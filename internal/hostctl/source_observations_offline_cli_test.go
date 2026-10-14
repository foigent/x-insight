package hostctl

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

// Exercises real CLI processes and systemd egress restrictions. It does not
// install a release or claim that a signed B artifact has been qualified.
func TestManagedObservationOfflineNativeCLI(t *testing.T) {
	root, bin := os.Getenv("XINSIGHT_B_OFFLINE_NATIVE_ROOT"), os.Getenv("XINSIGHT_B_OFFLINE_CLI_BIN")
	if root == "" || bin == "" {
		t.Skip("explicit isolated B native CLI fixture required")
	}
	if os.Geteuid() != 0 || !filepath.IsAbs(root) || filepath.Clean(root) != root || !strings.HasPrefix(root, "/opt/xi-validation/evidence/b-adaptation/") || !filepath.IsAbs(bin) || filepath.Clean(bin) != bin {
		t.Fatal("invalid B native fixture paths/executor")
	}
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	ctx, c := connectorDigestDatabase(t)
	m := PostgresManager{Runner: ExecRunner{}}
	if err := m.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err := m.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	admin := managedSQL(t, c.AdminDSN)
	t.Setenv(TestRootEnv, root)
	t.Setenv(AllowTestRootEnv, "1")
	paths, err := PathsFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	write := func(path string, raw []byte, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	set := filepath.Join(paths.DatabaseSetsDir, "native-b")
	for name, dsn := range map[string]string{"reader": c.ReaderDSN, "workspace": c.WorkspaceDSN, "importer": c.ImporterDSN, "admin": c.AdminDSN} {
		write(filepath.Join(set, "postgres-"+name+".dsn"), []byte(dsn+"\n"), 0600)
	}
	if err := os.Symlink(set, paths.DatabaseCurrentLink); err != nil {
		t.Fatal(err)
	}
	cli := func(binary string, args ...string) []byte {
		t.Helper()
		command := exec.CommandContext(ctx, filepath.Join(bin, binary), args...)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("native %s: %v: %s", binary, err, out)
		}
		return out
	}
	ctl := func(args ...string) []byte {
		t.Helper()
		return cli("xinsightctl", append([]string{"source-facts"}, args...)...)
	}
	var product buildinfo.Info
	if err := json.Unmarshal(cli("xinsight-importer", "version", "--json"), &product); err != nil || product.DatabaseSchemaMin != "34" || product.DatabaseSchemaMax != "34" {
		t.Fatal("B CLI target schema identity", err)
	}
	var source string
	if err := admin.QueryRow(`SELECT gen_random_uuid()::text`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	grants, bundles := managedObservationOfflineFixtures(t, source)
	a, raw := grants[0], bundles[0]
	machine, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		t.Fatal(err)
	}
	a.MachineIDSHA256 = sourcefacts.Hash([]byte(strings.TrimSpace(string(machine))))
	a.ProductVersion, a.ProductCommit = product.ProductVersion, product.Commit
	a.BundlePath = filepath.Join(root, "bundle.json")
	write(a.BundlePath, raw, 0600)
	authFile := filepath.Join(root, "authorization.json")
	authRaw, _ := json.Marshal(a)
	write(authFile, authRaw, 0600)
	var grant db.SourceOfflineRegistration
	if err := json.Unmarshal(ctl("offline-register", "--authorization-file", authFile, "--target-ref", a.TargetRef), &grant); err != nil || grant.Status != "disabled" {
		t.Fatal("native register/default disabled", err)
	}
	calls := 0
	offline := func(extra ...string) ([]byte, error) {
		calls++
		args := []string{"--unit=xi-b-offline-" + filepath.Base(filepath.Dir(root)) + "-" + strconv.Itoa(calls), "--wait", "--pipe", "--quiet", "--property=IPAddressDeny=any", "--property=IPAddressAllow=localhost", "--property=NoNewPrivileges=yes", "--setenv=" + config.ImporterDSNFileEnv + "=" + paths.ImporterDSNFile, filepath.Join(bin, "xinsight-importer"), "unirouter", "import-source-facts", "--authorization-file", authFile, "--target-ref", a.TargetRef}
		return exec.CommandContext(ctx, "systemd-run", append(args, extra...)...).CombinedOutput()
	}
	checks := []string{}
	check := func(name string, success bool, extra ...string) {
		t.Helper()
		before := managedOfflineDatabaseState(t, admin)
		out, err := offline(append([]string{"--check"}, extra...)...)
		if (err == nil) != success {
			t.Fatalf("B native check %s: success=%t: %v: %s", name, success, err, out)
		}
		if success {
			var result struct {
				Status      string `json:"status"`
				OperationID string `json:"operation_id"`
				Documents   int    `json:"documents"`
			}
			if json.Unmarshal(out, &result) != nil || result.Status != "checked" || result.OperationID != a.OperationID || result.Documents != 1 {
				t.Fatal("B check output/count", name)
			}
		}
		if !reflect.DeepEqual(before, managedOfflineDatabaseState(t, admin)) {
			t.Fatal("B CLI check changed table/sequence", name)
		}
		checks = append(checks, name)
	}
	check("disabled", false)
	ctl("offline-enable", "--authorization-file", authFile, "--target-ref", a.TargetRef, "--revision", strconv.FormatInt(grant.Revision, 10))
	check("active", true)
	check("repeat", true)
	check("wrong-target", false, "--target-ref", "different-target")
	for name, change := range map[string]func(*sourcefacts.OfflineAuthorization){
		"wrong-machine": func(v *sourcefacts.OfflineAuthorization) {
			v.MachineIDSHA256 = sourcefacts.Hash([]byte("different-machine"))
		},
		"wrong-product": func(v *sourcefacts.OfflineAuthorization) { v.ProductCommit = "different-build" },
		"wrong-schema":  func(v *sourcefacts.OfflineAuthorization) { v.TargetDatabaseSchema = "32" },
		"wrong-format": func(v *sourcefacts.OfflineAuthorization) {
			v.SchemaVersion = sourcefacts.OfflineAuthorizationVersion
			v.TargetDatabaseSchema = ""
		},
		"wrong-bundle": func(v *sourcefacts.OfflineAuthorization) {
			v.BundleSHA256 = sourcefacts.Hash([]byte("different-bundle"))
		},
		"expired": func(v *sourcefacts.OfflineAuthorization) {
			v.NotBefore = time.Now().Add(-time.Hour).UTC().Format(sourcefacts.TimeFormat)
			v.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(sourcefacts.TimeFormat)
		},
	} {
		bad := a
		change(&bad)
		bytes, _ := json.Marshal(bad)
		write(authFile, bytes, 0600)
		check(name, false)
	}
	write(authFile, authRaw, 0644)
	check("non-private-authorization", false)
	write(authFile, authRaw, 0600)
	write(a.BundlePath, raw, 0644)
	check("non-private-bundle", false)
	write(a.BundlePath, raw, 0600)
	out, err := offline()
	if err != nil {
		t.Fatalf("native B import: %v: %s", err, out)
	}
	var receipt connector.SourceNeutralOperationalReceipt
	if json.Unmarshal(out, &receipt) != nil || receipt.Status != "completed" || receipt.Counts.Inserted != 1 {
		t.Fatal("native B receipt")
	}
	check("completed", true)
	replayed, err := offline()
	if err != nil || string(out) != string(replayed) {
		t.Fatal("native B replay differs", err)
	}
	if err := json.Unmarshal(ctl("offline-status", "--authorization-id", a.AuthorizationID), &grant); err != nil {
		t.Fatal(err)
	}
	ctl("offline-revoke", "--authorization-id", grant.ID, "--revision", strconv.FormatInt(grant.Revision, 10))
	check("revoked", false)
	if _, err := offline(); err == nil {
		t.Fatal("revoked B native import accepted")
	}
	if managedCount(t, admin, "source_observation_documents") != 1 || managedCount(t, admin, "source_fact_documents") != 0 || managedCount(t, admin, "workbench_economic_facts") != 0 {
		t.Fatal("native B altered A/money facts")
	}
	result, _ := json.MarshalIndent(map[string]any{"checks": checks, "check_side_effects": false, "source_config_present": false, "egress": "systemd IPAddressDeny=any; IPAddressAllow=localhost", "product": product, "receipt": receipt, "signed_artifact_qualified": false}, "", "  ")
	write(filepath.Join(root, "result.json"), result, 0600)
}
