package hostctl

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/testsourcefacts"
)

// This opt-in proof runs only in the admitted synthetic XI guest. It executes
// actual binaries, TLS HTTP, restricted DB roles and systemd. The unit is
// relocated into a fresh retained fixture and uses the existing guest xi UID.
func TestManagedSourceNativeCLIAndSystemd(t *testing.T) {
	root := os.Getenv("XINSIGHT_A_MANAGED_NATIVE_ROOT")
	bin := os.Getenv("XINSIGHT_A_MANAGED_CLI_BIN")
	if root == "" || bin == "" {
		t.Skip("explicit isolated native CLI/systemd fixture required")
	}
	if os.Geteuid() != 0 || !filepath.IsAbs(root) || filepath.Clean(root) != root || !strings.HasPrefix(root, "/opt/xi-validation/evidence/a-managed-offline/native-") || !filepath.IsAbs(bin) {
		t.Fatal("exact synthetic root and root executor required")
	}
	if e := os.Mkdir(root, 0755); e != nil {
		t.Fatal(e)
	}
	globalUnit := "/etc/systemd/system/" + sourceFactsWorkerUnit
	for _, p := range []string{globalUnit, globalUnit + ".d"} {
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			t.Fatal("native fixture refuses existing unit", p)
		}
	}
	ctx, c := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if e := manager.EnsureRoles(ctx, c); e != nil {
		t.Fatal(e)
	}
	if _, e := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); e != nil {
		t.Fatal(e)
	}
	if e := manager.ApplyGrants(ctx, c); e != nil {
		t.Fatal(e)
	}
	admin := managedSQL(t, c.AdminDSN)
	t.Setenv(TestRootEnv, root)
	t.Setenv(AllowTestRootEnv, "1")
	paths, e := PathsFromEnvironment()
	if e != nil {
		t.Fatal(e)
	}
	write := func(path string, raw []byte, mode os.FileMode) {
		t.Helper()
		if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, raw, mode); e != nil {
			t.Fatal(e)
		}
		if e := os.Chmod(path, mode); e != nil {
			t.Fatal(e)
		}
	}
	set := filepath.Join(paths.DatabaseSetsDir, "native")
	for name, dsn := range map[string]string{"reader": c.ReaderDSN, "workspace": c.WorkspaceDSN, "importer": c.ImporterDSN, "admin": c.AdminDSN} {
		write(filepath.Join(set, "postgres-"+name+".dsn"), []byte(dsn+"\n"), 0600)
	}
	if e = os.Symlink(set, paths.DatabaseCurrentLink); e != nil {
		t.Fatal(e)
	}
	var source string
	if e = admin.QueryRow(`SELECT gen_random_uuid()::text`).Scan(&source); e != nil {
		t.Fatal(e)
	}
	a, offlineRaw, _ := managedOfflineFixture(t, source)
	bundle := testsourcefacts.Load(t, testsourcefacts.Chat)
	pages := map[string]sourcefacts.Page{}
	for _, p := range bundle.Pages {
		var v sourcefacts.Page
		if e = json.Unmarshal(p.Page, &v); e != nil {
			t.Fatal(e)
		}
		if len(v.Events) > 0 {
			if _, ok := pages[p.Profile]; !ok {
				pages[p.Profile] = v
			}
		}
	}
	if len(pages) != 3 {
		t.Fatal("three fixed profiles required")
	}
	var requests atomic.Int64
	var denyIdentity atomic.Bool
	var sourceKey atomic.Value
	sourceKey.Store("synthetic-managed-source-only")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+sourceKey.Load().(string) {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/api/v1/integrations/xinsight/source-facts/capabilities" {
			_ = json.NewEncoder(w).Encode(sourcefacts.Capabilities{SchemaVersion: "unirouter.source-capabilities.v1", SourceInstance: a.SourceInstance, Profiles: []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile}, ProfileVersion: "1.0.0", Status: "active", CredentialStatus: "feed_active", ScopeGeneration: "1", RetentionSeconds: 604800, Bootstrap: "forward_only", FieldAuthorization: "fixed_profiles"})
			return
		}
		profile := r.URL.Query().Get("profile")
		p, ok := pages[profile]
		if r.URL.Path != "/api/v1/integrations/xinsight/source-facts" || !ok || r.URL.Query().Get("limit") != "50" {
			w.WriteHeader(400)
			return
		}
		if profile == sourcefacts.IdentityProfile && denyIdentity.Load() {
			w.WriteHeader(403)
			return
		}
		cursor := r.URL.Query().Get("cursor")
		p.HasMore = false
		if cursor == "" {
			p.Events = []json.RawMessage{}
			p.NextCursor = profile + ":bootstrap"
		} else if cursor == profile+":bootstrap" {
			p.NextCursor = profile + ":received"
		} else if cursor == profile+":received" {
			p.Events = []json.RawMessage{}
			p.NextCursor = cursor
		} else {
			w.WriteHeader(409)
			return
		}
		p.HighWatermark = p.NextCursor
		_ = json.NewEncoder(w).Encode(p)
	}))
	defer server.Close()
	ca := filepath.Join(root, "source-fixture-ca.pem")
	write(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.TLS.Certificates[0].Certificate[0]}), 0644)
	key := filepath.Join(paths.EtcDir, "source.key")
	write(key, []byte("synthetic-managed-source-only\n"), 0600)
	cfg := sourcefacts.ManagedConfig{SchemaVersion: sourcefacts.ManagedConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 4}
	for _, profile := range []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile} {
		var id string
		if e = admin.QueryRow(`SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
			t.Fatal(e)
		}
		cfg.Profiles = append(cfg.Profiles, sourcefacts.ManagedProfileConfig{StreamID: id, SourceSystemID: source, SourceInstance: a.SourceInstance, OrgID: a.OrgID, ProjectID: a.ProjectID, Profile: profile, ScopeGeneration: "1", Endpoint: server.URL + "/api/v1/integrations/xinsight/source-facts", KeyFile: key})
	}
	input := filepath.Join(root, "managed-input.json")
	configRaw, _ := json.Marshal(cfg)
	write(input, configRaw, 0644)
	unit, e := os.ReadFile("../../deploy/xinsight-source-facts.service")
	if e != nil {
		t.Fatal(e)
	}
	text := strings.ReplaceAll(string(unit), "/opt/x-insight/current", bin)
	text = strings.ReplaceAll(text, "/etc/x-insight/source-facts.json", paths.SourceFactsConfig)
	text = strings.ReplaceAll(text, "/etc/x-insight/database/current/postgres-importer.dsn", paths.ImporterDSNFile)
	text = strings.ReplaceAll(text, "ConditionPathIsSymbolicLink="+bin, "ConditionPathIsDirectory="+bin)
	text = strings.ReplaceAll(text, "ConditionPathIsReadWrite=/var/lib/x-insight", "ConditionPathIsReadWrite="+root)
	text = strings.ReplaceAll(text, "User=xinsight", "User=xi")
	text = strings.ReplaceAll(text, "Group=xinsight", "Group=xi")
	text = strings.ReplaceAll(text, "unirouter run-source-facts", "unirouter run-source-facts --config "+paths.SourceFactsConfig)
	text = strings.Replace(text, "[Service]", "[Service]\nEnvironment=SSL_CERT_FILE="+ca+"\nMemoryMax=256M\nMemorySwapMax=0", 1)
	write(paths.SourceFactsServiceUnit, []byte(text), 0644)
	if e = os.MkdirAll(paths.SourceFactsServiceUnit+".d", 0755); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(paths.SourceFactsServiceUnit, globalUnit); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(paths.SourceFactsServiceUnit+".d", globalUnit+".d"); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if err := exec.Command("systemctl", "stop", sourceFactsWorkerUnit).Run(); err != nil {
			t.Error(err)
		}
		if err := exec.Command("systemctl", "disable", sourceFactsWorkerUnit).Run(); err != nil {
			t.Error(err)
		}
		for _, p := range []string{globalUnit, globalUnit + ".d"} {
			target, e := os.Readlink(p)
			if e == nil && strings.HasPrefix(target, root+"/") {
				if e = os.Remove(p); e != nil {
					t.Error(e)
				}
			} else if !os.IsNotExist(e) {
				t.Error("native fixture cleanup identity mismatch", p)
			}
		}
		if e := exec.Command("systemctl", "daemon-reload").Run(); e != nil {
			t.Error(e)
		}
	})
	if output, e := exec.Command("systemctl", "daemon-reload").CombinedOutput(); e != nil {
		t.Fatal(string(output), e)
	}
	env := append(os.Environ(), "SSL_CERT_FILE="+ca, "XINSIGHT_IMPORTER_POSTGRES_DSN_FILE="+paths.ImporterDSNFile)
	cli := func(binary string, args ...string) ([]byte, error) {
		t.Helper()
		command := exec.CommandContext(ctx, filepath.Join(bin, "bin", binary), args...)
		command.Env = env
		output, e := command.CombinedOutput()
		return output, e
	}
	ctl := func(args ...string) []byte {
		t.Helper()
		output, e := cli("xinsightctl", append([]string{"source-facts"}, args...)...)
		if e != nil {
			t.Fatalf("CLI %s: %s: %v", args[0], output, e)
		}
		return output
	}
	invalid := filepath.Join(root, "managed-invalid-path.json")
	write(invalid, []byte(strings.ReplaceAll(string(configRaw), "source.key", "source profile.key")), 0644)
	if _, err := cli("xinsightctl", "source-facts", "configure", "--file", invalid); err == nil {
		t.Fatal("configuration accepted a credential path unsupported by systemd")
	}
	ctl("configure", "--file", input)
	if requests.Load() != 0 {
		t.Fatal("configuration contacted source")
	}
	for _, p := range cfg.Profiles {
		var s sourcefacts.Stream
		if e = json.Unmarshal(ctl("register", "--stream-id", p.StreamID), &s); e != nil || s.Status != "paused" {
			t.Fatal(e)
		}
		ctl("enable", "--stream-id", p.StreamID, "--revision", strconv.FormatInt(s.Revision, 10))
	}
	ctl("worker-start")
	store := managedStore(t, c.AdminDSN)
	await := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		for _, p := range cfg.Profiles {
			s, err := store.LoadSourceFactsStream(ctx, p.StreamID)
			t.Logf("profile=%s status=%s cursor=%s attention=%t last_error=%s load_error=%v", p.Profile, s.Status, s.Cursor, s.RequiresAttention, s.LastError, err)
		}
		journal, _ := exec.Command("journalctl", "-u", sourceFactsWorkerUnit, "--no-pager", "-n", "30").CombinedOutput()
		write(filepath.Join(root, "failure-journal.log"), journal, 0600)
		t.Fatal("native managed outcome not observed; retained failure-journal.log")
	}
	await(func() bool {
		for _, p := range cfg.Profiles {
			s, e := store.LoadSourceFactsStream(ctx, p.StreamID)
			if e != nil || s.Cursor != p.Profile+":received" {
				return false
			}
		}
		return true
	})
	denyIdentity.Store(true)
	identity := cfg.Profiles[1]
	await(func() bool {
		s, e := store.LoadSourceFactsStream(ctx, identity.StreamID)
		return e == nil && s.RequiresAttention
	})
	ctl("worker-stop")
	before := requests.Load()
	if output, e := cli("xinsight-importer", "unirouter", "pull-source-facts", "--config", paths.SourceFactsConfig, "--stream-id", identity.StreamID); e == nil {
		t.Fatal("single-step bypassed attention", string(output))
	}
	if requests.Load() != before {
		t.Fatal("attention step contacted source")
	}
	// Restart retains attention; the other two profiles continue to be polled.
	ctl("worker-start")
	await(func() bool { return requests.Load() >= before+4 })
	ctl("worker-stop")
	s, e := store.LoadSourceFactsStream(ctx, identity.StreamID)
	if e != nil || !s.RequiresAttention {
		t.Fatal("restart cleared attention", e)
	}
	denyIdentity.Store(false)
	ctl("resume", "--stream-id", s.ID, "--revision", strconv.FormatInt(s.Revision, 10), "--acknowledge")
	sourceKey.Store("synthetic-managed-source-rotated")
	write(key, []byte(sourceKey.Load().(string)+"\n"), 0600)
	before = requests.Load()
	ctl("worker-start")
	await(func() bool { return requests.Load() >= before+6 })
	ctl("worker-stop")
	for _, p := range cfg.Profiles {
		s, e := store.LoadSourceFactsStream(ctx, p.StreamID)
		if e != nil || s.RequiresAttention || s.LastError != "" || s.Cursor != p.Profile+":received" {
			t.Fatal(e)
		}
		ctl("pause", "--stream-id", s.ID, "--revision", strconv.FormatInt(s.Revision, 10))
	}
	var product buildinfo.Info
	output, e := cli("xinsight-importer", "version", "--json")
	if e != nil || json.Unmarshal(output, &product) != nil {
		t.Fatal("native product identity", e)
	}
	machine, e := os.ReadFile("/etc/machine-id")
	if e != nil {
		t.Fatal(e)
	}
	a.MachineIDSHA256 = sourcefacts.Hash([]byte(strings.TrimSpace(string(machine))))
	a.ProductVersion = product.ProductVersion
	a.ProductCommit = product.Commit
	a.BundlePath = filepath.Join(root, "offline-bundle.json")
	write(a.BundlePath, offlineRaw, 0600)
	authFile := filepath.Join(root, "offline-authorization.json")
	authRaw, _ := json.Marshal(a)
	write(authFile, authRaw, 0600)
	var grant db.SourceOfflineRegistration
	if e = json.Unmarshal(ctl("offline-register", "--authorization-file", authFile, "--target-ref", a.TargetRef), &grant); e != nil {
		t.Fatal(e)
	}
	ctl("offline-enable", "--authorization-file", authFile, "--target-ref", a.TargetRef, "--revision", strconv.FormatInt(grant.Revision, 10))
	before = requests.Load()
	// Keep only the supported loopback database reachable. The actual TLS
	// source is closed and its original credential path is no longer present.
	// Non-loopback IP egress is denied by the isolated systemd unit.
	server.Close()
	if e = os.Rename(key, key+".retained-after-rotation"); e != nil {
		t.Fatal(e)
	}
	offlineCalls := 0
	offline := func(extra ...string) ([]byte, error) {
		offlineCalls++
		args := []string{"--unit=xi-a-offline-" + filepath.Base(root) + "-" + strconv.Itoa(offlineCalls), "--wait", "--pipe", "--quiet",
			"--property=IPAddressDeny=any", "--property=IPAddressAllow=localhost", "--property=NoNewPrivileges=yes",
			"--setenv=" + config.ImporterDSNFileEnv + "=" + paths.ImporterDSNFile,
			filepath.Join(bin, "bin", "xinsight-importer"), "unirouter", "import-source-facts", "--authorization-file", authFile, "--target-ref", a.TargetRef}
		return exec.CommandContext(ctx, "systemd-run", append(args, extra...)...).CombinedOutput()
	}
	checks := []string{}
	check := func(name string, success bool, extra ...string) {
		t.Helper()
		state := managedOfflineDatabaseState(t, admin)
		out, err := offline(append([]string{"--check"}, extra...)...)
		if (err == nil) != success {
			t.Fatalf("offline check %s: expected success=%t: %s: %v", name, success, out, err)
		}
		if success {
			var result struct {
				Status      string `json:"status"`
				OperationID string `json:"operation_id"`
				Documents   int    `json:"documents"`
			}
			if json.Unmarshal(out, &result) != nil || result.Status != "checked" || result.OperationID != a.OperationID || result.Documents == 0 {
				t.Fatal("check returned an import result or invalid input count", name)
			}
		}
		if !reflect.DeepEqual(state, managedOfflineDatabaseState(t, admin)) {
			t.Fatal("offline check changed a public table or sequence", name)
		}
		if requests.Load() != before {
			t.Fatal("offline check contacted source", name)
		}
		checks = append(checks, name)
	}
	check("active-before-import", true)
	check("active-repeat", true)
	check("wrong-target", false, "--target-ref", "different-target")
	for _, tc := range []struct {
		name   string
		change func(*sourcefacts.OfflineAuthorization)
	}{
		{"wrong-product", func(v *sourcefacts.OfflineAuthorization) { v.ProductCommit = "different-build" }},
		{"wrong-schema", func(v *sourcefacts.OfflineAuthorization) { v.SchemaVersion = "unsupported" }},
		{"wrong-batch-hash", func(v *sourcefacts.OfflineAuthorization) { v.BundleSHA256 = sourcefacts.Hash([]byte("different")) }},
		{"unregistered", func(v *sourcefacts.OfflineAuthorization) { v.AuthorizationID = "unregistered-authorization" }},
		{"expired", func(v *sourcefacts.OfflineAuthorization) {
			v.NotBefore = time.Now().Add(-time.Hour).UTC().Format(sourcefacts.TimeFormat)
			v.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(sourcefacts.TimeFormat)
		}},
		{"not-yet-valid", func(v *sourcefacts.OfflineAuthorization) {
			v.NotBefore = time.Now().Add(time.Minute).UTC().Format(sourcefacts.TimeFormat)
			v.ExpiresAt = time.Now().Add(time.Hour).UTC().Format(sourcefacts.TimeFormat)
		}},
	} {
		bad := a
		tc.change(&bad)
		raw, _ := json.Marshal(bad)
		write(authFile, raw, 0600)
		check(tc.name, false)
	}
	write(authFile, authRaw, 0644)
	check("non-private-file", false)
	write(authFile, authRaw, 0600)
	grant, e = store.LoadSourceOfflineRegistration(ctx, a.AuthorizationID)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(ctl("offline-disable", "--authorization-id", grant.ID, "--revision", strconv.FormatInt(grant.Revision, 10)), &grant); e != nil {
		t.Fatal(e)
	}
	check("currently-disabled", false)
	ctl("offline-enable", "--authorization-file", authFile, "--target-ref", a.TargetRef, "--revision", strconv.FormatInt(grant.Revision, 10))
	output, e = offline()
	if e != nil {
		t.Fatal("native offline", string(output), e)
	}
	var receipt connector.SourceNeutralOperationalReceipt
	if e = json.Unmarshal(output, &receipt); e != nil || receipt.Status != "completed" {
		t.Fatal(e)
	}
	check("already-completed", true)
	check("completed-repeat", true)
	conflict := a
	conflict.AuthorizationID = "valid-different-input-binding"
	conflict.BundleID = "different-bundle-id"
	conflictRaw, _ := json.Marshal(conflict)
	write(authFile, conflictRaw, 0600)
	var conflictingGrant db.SourceOfflineRegistration
	if e = json.Unmarshal(ctl("offline-register", "--authorization-file", authFile, "--target-ref", a.TargetRef), &conflictingGrant); e != nil {
		t.Fatal(e)
	}
	ctl("offline-enable", "--authorization-file", authFile, "--target-ref", a.TargetRef, "--revision", strconv.FormatInt(conflictingGrant.Revision, 10))
	check("existing-operation-different-binding", false)
	write(authFile, authRaw, 0600)
	duplicate, e := cli("xinsight-importer", "unirouter", "import-source-facts", "--authorization-file", authFile, "--target-ref", a.TargetRef)
	if e != nil || string(duplicate) != string(output) {
		t.Fatal("native offline exact replay", e)
	}
	if requests.Load() != before {
		t.Fatal("offline made source network requests")
	}
	write(authFile, authRaw, 0644)
	if _, e = cli("xinsight-importer", "unirouter", "import-source-facts", "--authorization-file", authFile, "--target-ref", a.TargetRef); e == nil {
		t.Fatal("native offline accepted non-private authorization")
	}
	write(authFile, authRaw, 0600)
	grant, e = store.LoadSourceOfflineRegistration(ctx, a.AuthorizationID)
	if e != nil {
		t.Fatal(e)
	}
	ctl("offline-revoke", "--authorization-id", grant.ID, "--revision", strconv.FormatInt(grant.Revision, 10))
	check("currently-revoked", false)
	if _, e = cli("xinsight-importer", "unirouter", "import-source-facts", "--authorization-file", authFile, "--target-ref", a.TargetRef); e == nil {
		t.Fatal("native revoked replay")
	}
	report := map[string]any{"product": product, "profiles": 3, "source_requests": requests.Load(), "offline_source_requests": int64(0), "offline_nonloopback_ip_denied": true, "offline_source_closed": true, "offline_source_key_absent": true, "same_scope_key_rotation": true, "retained_fact_count": managedCount(t, admin, "source_fact_documents"), "managed_worker_stopped": true, "native_cli": true, "tls_source": true, "systemd": true, "fixture_adaptations": []string{"installation paths relocated into exact fresh fixture", "existing xi operating-system user; actual restricted PostgreSQL importer role"}}
	report["offline_check_cases"] = checks
	report["offline_check_all_public_tables_and_sequences_unchanged"] = true
	raw, _ := json.MarshalIndent(report, "", "  ")
	write(filepath.Join(root, "result.json"), raw, 0644)
	t.Log(fmt.Sprintf("native CLI, TLS source, three-profile systemd collection, persistent attention across single-step/restart, explicit resume, independent offline exact replay/zero source requests/revoke; evidence %s", root))
}
