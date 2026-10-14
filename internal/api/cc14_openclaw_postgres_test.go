package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5"
)

const cc14XIRun = "cc14-openclaw-20260924-r9"
const cc14XIActions = "synthetic-db-redis,http,credentials,guardrail-shadow-policy,content-binding,full-content-policy,human-review-grant,secret-cleanup,openclaw-read-two-rounds"
const cc14XIRunDir = p3Root + "/evidence/" + cc14XIRun
const cc14XICredentials = p3Root + "/credentials/xi-product-test/" + cc14XIRun
const cc14XIOutput = cc14XIRunDir + "/xi"

const cc15XIRun = "cc15-openclaw-file-actions-20260925-r1"
const cc15XIActions = "synthetic-db-redis,http,credentials,guardrail-shadow-policy,content-binding,full-content-policy,human-review-grant,secret-cleanup,openclaw-read-edit-read-five-rounds"

type cc14XIScenario struct {
	run, actions, optIn, runKey, actionsKey, xiBinaryKey, urBinaryKey, url, upstream, testName string
	rounds                                                                                     int
	fileActions                                                                                bool
}

func cc14LegacyScenario() cc14XIScenario {
	return cc14XIScenario{cc14XIRun, cc14XIActions, "XI_CC14_INTEROP", "CC14_APPROVED_RUN", "CC14_APPROVED_ACTIONS", "CC14_XI_APPROVED_BINARY_SHA256", "CC14_UR_APPROVED_BINARY_SHA256", "http://127.0.0.1:19185", "http://127.0.0.1:19186", "TestCC14OpenClawWorkbenchHarness", 2, false}
}
func cc15FileScenario() cc14XIScenario {
	return cc14XIScenario{cc15XIRun, cc15XIActions, "XI_CC15_INTEROP", "CC15_APPROVED_RUN", "CC15_APPROVED_ACTIONS", "CC15_XI_APPROVED_BINARY_SHA256", "CC15_UR_APPROVED_BINARY_SHA256", "http://127.0.0.1:19285", "http://127.0.0.1:19286", "TestCC15OpenClawWorkbenchHarness", 5, true}
}
func cc14ScenarioFor(get func(string) string) cc14XIScenario {
	if get("XI_CC15_INTEROP") == "1" {
		return cc15FileScenario()
	}
	return cc14LegacyScenario()
}
func (s cc14XIScenario) runDir() string      { return p3Root + "/evidence/" + s.run }
func (s cc14XIScenario) credentials() string { return p3Root + "/credentials/xi-product-test/" + s.run }
func (s cc14XIScenario) output() string      { return s.runDir() + "/xi" }

func cc14Digest(s string) bool   { return len(s) == 64 && strings.Trim(s, "0123456789abcdef") == "" }
func cc14Positive(s string) bool { return sourcefacts.Positive(s) }

// Pure admission: no connection, file read, credential use, listener or child.
func cc14XIValidateEnvironment(get func(string) string) error {
	scenario := cc14ScenarioFor(get)
	if get("XI_FORMAL_ACCOUNTS") != "1" || p3FormalMode(get) != nil {
		return errors.New("CC14/15 requires explicit formal accounts and ctl identity")
	}
	other := "XI_CC15_INTEROP"
	otherRun, otherActions, otherXI, otherUR := "CC15_APPROVED_RUN", "CC15_APPROVED_ACTIONS", "CC15_XI_APPROVED_BINARY_SHA256", "CC15_UR_APPROVED_BINARY_SHA256"
	if scenario.fileActions {
		other = "XI_CC14_INTEROP"
		otherRun, otherActions, otherXI, otherUR = "CC14_APPROVED_RUN", "CC14_APPROVED_ACTIONS", "CC14_XI_APPROVED_BINARY_SHA256", "CC14_UR_APPROVED_BINARY_SHA256"
	}
	if get(scenario.optIn) != "1" || get(other) != "" || get(otherRun) != "" || get(otherActions) != "" || get(otherXI) != "" || get(otherUR) != "" ||
		get(scenario.runKey) != scenario.run || get(scenario.actionsKey) != scenario.actions || get("P3_CREDENTIAL_DIR") != scenario.credentials() || get("XINSIGHT_SCOPED_BROWSER_OUTPUT") != scenario.output() {
		return errors.New("CC14/15 exact opt-in/run/actions/paths required")
	}
	for _, key := range []string{"XI_J1_INTEROP", "XI_P3_INTEROP", "XI_NO_B_BROWSER", "XINSIGHT_SCOPED_BROWSER", "XINSIGHT_A0_BROWSER_HARNESS", "XINSIGHT_A1_BROWSER_HARNESS", "XINSIGHT_A_MANAGED_BROWSER", "XINSIGHT_PURPOSE_BROWSER", "XINSIGHT_A0_BROWSER_PAGING_FIXTURE", "XINSIGHT_RESPONSIBILITY_BROWSER", "UR_CC14_INTEROP", "UR_CC15_INTEROP", "UR_J1_INTEROP", "UR_P3_INTEROP"} {
		if get(key) != "" {
			return errors.New("CC14/15 excludes other runner modes")
		}
	}
	for _, key := range []string{scenario.xiBinaryKey, scenario.urBinaryKey, "P3_XI_IMPORTER_SHA256"} {
		if !cc14Digest(get(key)) {
			return errors.New("CC14/15 exact candidate digests required")
		}
	}
	binary := get("P3_XI_IMPORTER")
	if !strings.HasPrefix(binary, p3Root+"/") || filepath.Clean(binary) != binary {
		return errors.New("CC14/15 importer path boundary")
	}
	cfg, err := pgx.ParseConfig(get("XINSIGHT_POSTGRES_DSN"))
	if err != nil {
		return errors.New("CC14/15 database configuration")
	}
	base := get("XI_P3_BASE_DATABASE")
	if base == "" || base == "postgres" || base == "template0" || base == "template1" || cfg.Host != "127.0.0.1" || cfg.Port != 56134 || cfg.Database != base || len(cfg.Fallbacks) != 0 || len(cfg.RuntimeParams) != 0 {
		return errors.New("CC14/15 exact XI synthetic base required")
	}
	for _, key := range []string{"XI_P3_DATABASE_OID", "XI_P3_SYSTEM_ID", "UR_P3_DATABASE_OID", "UR_P3_SYSTEM_ID"} {
		if !cc14Positive(get(key)) {
			return errors.New("CC14/15 explicit database identity required")
		}
	}
	if get("UR_P3_BASE_DATABASE") == "" || get("UR_P3_BASE_DATABASE") == "postgres" || get("UR_P3_BASE_DATABASE") == "template0" || get("UR_P3_BASE_DATABASE") == "template1" {
		return errors.New("CC14/15 explicit UR base identity required")
	}
	return nil
}
func cc14RuntimeApproval(t *testing.T) {
	t.Helper()
	scenario := cc14ScenarioFor(os.Getenv)
	p3Check(t, cc14XIValidateEnvironment(os.Getenv) == nil, "CC14/15 exact admission")
	p3Check(t, t.Name() == scenario.testName && os.Geteuid() == 0, "CC14/15 exclusive runtime selector and root")
	selector := false
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-test.run=") {
			p3Check(t, arg == "-test.run=^"+scenario.testName+"$", "CC14/15 exact test selector")
			selector = true
		}
	}
	p3Check(t, selector, "CC14/15 explicit selector required")
	binary, err := os.Executable()
	p3Check(t, err == nil && strings.HasPrefix(binary, p3Root+"/") && filepath.Clean(binary) == binary, "CC14/15 precompiled candidate path")
	p3PrivateDirectory(t, filepath.Dir(binary), false)
	file, err := os.Open(binary)
	p3Check(t, err == nil, "CC14/15 candidate open")
	defer file.Close()
	info, err := file.Stat()
	p3Check(t, err == nil && info.Mode().IsRegular(), "CC14/15 candidate regular file")
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	p3Check(t, err == nil && hex.EncodeToString(hash.Sum(nil)) == os.Getenv(scenario.xiBinaryKey), "CC14/15 running candidate digest")
}
func cc14Decode(raw []byte, target any) error {
	if _, err := connector.DecodeStrictJSONObject(raw); err != nil {
		return errors.New("CC14 malformed receipt")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("CC14 receipt fields")
	}
	return nil
}

// Only the producer's private immutable receipts are accepted; never read keys.
func cc14ReadReceipt(path string, target any) error {
	raw, err := cc14ReadDocument(path)
	if err != nil {
		return err
	}
	return cc14Decode(raw, target)
}
func cc14ReadDocument(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 || st.Nlink != 1 || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 1<<20 {
		return nil, errors.New("CC14 receipt ownership/type/size")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("CC14 receipt open")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("CC14 receipt changed")
	}
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, errors.New("CC14 receipt read")
	}
	return raw, nil
}

// A native exclusive link publishes complete bytes atomically without replacing
// any existing bootstrap receipt. The temporary file belongs only to this call.
func cc14AtomicJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cc14-bootstrap-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Link(name, path)
}

type cc14XIIdentity struct {
	Run          string `json:"run"`
	Binary       string `json:"binary_sha256"`
	Database     string `json:"database"`
	OID          string `json:"database_oid"`
	System       string `json:"system_id"`
	Base         string `json:"base_database"`
	BaseOID      string `json:"base_oid"`
	GatewayKeyID int64  `json:"gateway_key_id"`
	Upstream     string `json:"upstream"`
}
type cc14XIDatabases struct {
	Run       string            `json:"run"`
	System    string            `json:"system_id"`
	Base      string            `json:"base_database"`
	BaseOID   string            `json:"base_oid"`
	Clone     string            `json:"clone"`
	Created   map[string]string `json:"created_databases"`
	Unchanged bool              `json:"existing_unchanged"`
	Cleanup   string            `json:"cleanup_owner"`
}

func cc14ValidateProducerFor(ready p3ProducerReady, id cc14XIIdentity, databases cc14XIDatabases, get func(string) string, scenario cc14XIScenario) bool {
	if ready.URL != scenario.url || ready.SourceInstance != "synthetic-http" || !cc14Positive(ready.OrgID) || !cc14Positive(ready.ProjectID) || ready.CredentialDir != scenario.credentials() || ready.Content == nil {
		return false
	}
	c := ready.Content
	if c.CredentialID == "" || len(c.CredentialID) > 256 || !strings.HasPrefix(c.RegistrationHash, "sha256:") || !cc14Digest(strings.TrimPrefix(c.RegistrationHash, "sha256:")) || !strings.HasPrefix(c.CapabilitiesSHA256, "sha256:") || !cc14Digest(strings.TrimPrefix(c.CapabilitiesSHA256, "sha256:")) {
		return false
	}
	seenOIDs := map[string]bool{}
	for name, oid := range databases.Created {
		if name == "" || name == id.Base || name == "postgres" || name == "template0" || name == "template1" || !cc14Positive(oid) || seenOIDs[oid] {
			return false
		}
		seenOIDs[oid] = true
	}
	return id.Run == scenario.run && id.Binary == get(scenario.urBinaryKey) && id.Base == get("UR_P3_BASE_DATABASE") && id.BaseOID == get("UR_P3_DATABASE_OID") && id.System == get("UR_P3_SYSTEM_ID") && id.Database != "" && id.Database != id.Base && cc14Positive(id.OID) && id.GatewayKeyID > 0 && id.Upstream == scenario.upstream && databases.Run == scenario.run && databases.System == id.System && databases.Base == id.Base && databases.BaseOID == id.BaseOID && databases.Clone == id.Database && databases.Unchanged && len(databases.Created) == 2 && databases.Created[id.Database] == id.OID && databases.Created[id.Base] == "" && databases.Cleanup == "native SetupMigratedTestDB/TestMain; exact names only"
}

func cc14ValidateProducer(ready p3ProducerReady, id cc14XIIdentity, databases cc14XIDatabases, get func(string) string) bool {
	return cc14ValidateProducerFor(ready, id, databases, get, cc14LegacyScenario())
}

type cc14XIRound struct {
	Round              int       `json:"round"`
	RequestID          string    `json:"request_id"`
	ProviderResponseID string    `json:"provider_response_id,omitempty"`
	SHA                string    `json:"request_sha256"`
	Status             int       `json:"status"`
	Calls              int64     `json:"provider_calls"`
	Stale              bool      `json:"policy_snapshot_stale"`
	Started            time.Time `json:"started_at"`
	Ended              time.Time `json:"ended_at"`
}
type cc14XIRequests struct {
	Run      string          `json:"run"`
	Source   p3ProducerReady `json:"source"`
	Requests []cc14XIRound   `json:"requests"`
	Count    int             `json:"request_count"`
	Economic int             `json:"economic_records"`
	Points   int             `json:"content_points"`
	Calls    int             `json:"provider_calls"`
}

func cc14ValidateRequestsFor(v cc14XIRequests, source p3ProducerReady, scenario cc14XIScenario) bool {
	if v.Run != scenario.run || !reflect.DeepEqual(v.Source, source) || v.Count != scenario.rounds || v.Economic != scenario.rounds || v.Points != 4*scenario.rounds || v.Calls != scenario.rounds || len(v.Requests) != scenario.rounds {
		return false
	}
	seenIDs, seenResponses := map[string]bool{}, map[string]bool{}
	for i, r := range v.Requests {
		if seenIDs[r.RequestID] || r.Round != i+1 || r.RequestID == "" || len(r.RequestID) > 64 || strings.ContainsAny(r.RequestID, "\r\n\x00") || !cc14Digest(r.SHA) || r.Status != 200 || r.Calls != 1 || r.Stale || r.Started.IsZero() || r.Ended.Before(r.Started) {
			return false
		}
		seenIDs[r.RequestID] = true
		if scenario.fileActions {
			want := fmt.Sprintf("cc15-%s-%d", scenario.run, i+1)
			if r.ProviderResponseID != want || seenResponses[r.ProviderResponseID] {
				return false
			}
			seenResponses[r.ProviderResponseID] = true
		} else if r.ProviderResponseID != "" {
			return false
		}
		if i > 0 && r.Started.Before(v.Requests[i-1].Ended) {
			return false
		}
	}
	return true
}

func cc14ValidateRequests(v cc14XIRequests, source p3ProducerReady) bool {
	return cc14ValidateRequestsFor(v, source, cc14LegacyScenario())
}

type cc14XITerminal struct {
	Round       int      `json:"round"`
	RequestID   string   `json:"request_id"`
	Points      int      `json:"content_points"`
	Profiles    []string `json:"runtime_profiles"`
	Observation struct {
		RequestID string    `json:"request_id"`
		At        time.Time `json:"observed_at"`
		QueryOK   bool      `json:"query_ok"`
		Counts    *struct {
			Settled  int `json:"settled"`
			Economic int `json:"economic_records"`
			Context  int `json:"context_terminal_records"`
			Identity int `json:"identity_terminal_records"`
			Cost     int `json:"cost_terminal_records"`
			All      int `json:"all_profiles"`
		} `json:"counts"`
	} `json:"observation"`
}

func cc14ValidateTerminal(v cc14XITerminal, r cc14XIRound) bool {
	c := v.Observation.Counts
	if c == nil || v.Round != r.Round || v.RequestID != r.RequestID || v.Points != 4 || !v.Observation.QueryOK || v.Observation.RequestID != r.RequestID || v.Observation.At.IsZero() || v.Observation.At.Before(r.Ended) || c.Settled != 1 || c.Economic != 1 || c.Context < 1 || c.Identity < 1 || c.Cost < 1 || c.All != 8 || len(v.Profiles) != 5 {
		return false
	}
	seen := map[string]bool{}
	for _, p := range v.Profiles {
		if seen[p] {
			return false
		}
		seen[p] = true
	}
	for _, p := range []string{sourcefacts.RuntimeProfile, sourcefacts.AttemptProfile, sourcefacts.CapabilityProfile, sourcefacts.PolicyProfile, sourcefacts.DestinationProfile} {
		if !seen[p] {
			return false
		}
	}
	return true
}

func newCC14XI(t *testing.T) *p3BrowserFixture {
	t.Helper()
	cc14RuntimeApproval(t)
	scenario := cc14ScenarioFor(os.Getenv)
	p3PrivateDirectory(t, scenario.runDir(), true)
	p3PrivateDirectory(t, scenario.runDir()+"/ur", true)
	p3PrivateDirectory(t, scenario.credentials(), true)
	var ready p3ProducerReady
	var id cc14XIIdentity
	var databases cc14XIDatabases
	p3Check(t, cc14ReadReceipt(scenario.runDir()+"/ur/ready.json", &ready) == nil && cc14ReadReceipt(scenario.runDir()+"/ur/identity.json", &id) == nil && cc14ReadReceipt(scenario.runDir()+"/ur/databases.json", &databases) == nil && cc14ValidateProducerFor(ready, id, databases, os.Getenv, scenario), "CC14 producer candidate and exact source/database identity")
	_, err := os.Lstat(scenario.runDir() + "/bootstrap.json")
	p3Check(t, os.IsNotExist(err), "CC14 bootstrap must be absent")
	names := []string{"requests-ready.json", "extra-request.json", "closed.json"}
	for i := 1; i <= scenario.rounds; i++ {
		names = append(names, fmt.Sprintf("request-%d.json", i))
	}
	for _, name := range names {
		_, err = os.Lstat(scenario.runDir() + "/ur/" + name)
		p3Check(t, os.IsNotExist(err), "CC14 empty production before bootstrap")
	}
	importer := p3Binary(t, "P3_XI_IMPORTER")
	cfg, err := pgx.ParseConfig(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	p3Check(t, err == nil, "CC14 XI config")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	control, err := pgx.ConnectConfig(ctx, cfg)
	p3Check(t, err == nil, "CC14 XI identity connection")
	defer control.Close(context.Background())
	var oid, system string
	var tables int
	err = control.QueryRow(ctx, `SELECT (SELECT oid::text FROM pg_database WHERE datname=current_database()),system_identifier::text,(SELECT count(*) FROM information_schema.tables WHERE table_schema='public') FROM pg_control_system()`).Scan(&oid, &system, &tables)
	// The native factory creates a separate database; the approved control base may contain prior test tables.
	p3Check(t, err == nil && oid == os.Getenv("XI_P3_DATABASE_OID") && system == os.Getenv("XI_P3_SYSTEM_ID"), "CC14 exact XI base identity")
	p3Check(t, os.Mkdir(scenario.output(), 0700) == nil, "CC14 fresh XI evidence directory")
	runCtx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	if deadline, ok := t.Deadline(); ok {
		bounded, finish := context.WithDeadline(runCtx, deadline.Add(-45*time.Second))
		original := stop
		stop = func() { finish(); original() }
		runCtx = bounded
	}
	t.Cleanup(stop)
	f := &p3BrowserFixture{cc14: true, cc14Scenario: scenario, ctx: runCtx, cancel: stop, output: scenario.output(), runDir: scenario.runDir(), credentials: scenario.credentials(), importer: importer, source: ready}
	p3JSON(t, filepath.Join(f.output, "admission.json"), map[string]any{"run": scenario.run, "xi_binary_sha256": os.Getenv(scenario.xiBinaryKey), "ur_binary_sha256": id.Binary, "importer_sha256": os.Getenv("P3_XI_IMPORTER_SHA256"), "source": ready, "producer_identity": id, "producer_databases": databases, "xi_base_database": cfg.Database, "xi_base_oid": oid, "xi_system_id": system, "xi_base_public_tables": tables})
	f.prepareFormalAccounts(t)
	return f
}

func cc14StopRequested(t *testing.T, f *p3BrowserFixture) bool {
	t.Helper()
	info, err := os.Lstat(f.output + "/stop")
	if os.IsNotExist(err) {
		return false
	}
	p3Check(t, err == nil, "CC14 stop marker lookup")
	st, ok := info.Sys().(*syscall.Stat_t)
	p3Check(t, ok && st.Uid == 0 && st.Nlink == 1 && info.Mode().IsRegular() && info.Mode().Perm() == 0600 && info.Size() == 0, "CC14 exact root-owned empty stop marker")
	return true
}
func cc14JoinChild(t *testing.T, f *p3BrowserFixture, p *p3Child, name string) {
	t.Helper()
	p.cancel()
	<-p.done
	p3JSON(t, filepath.Join(f.output, name), p3ProcessReceipt("cc14_consumer_stop", p.cmd, p.ctx, true))
	if p.cmd.ProcessState == nil || p.cmd.ProcessState.ExitCode() != 0 {
		t.Error("CC14 importer did not exit cleanly; inspect bounded process receipt")
	}
}
func cc14StopConsumers(t *testing.T, f *p3BrowserFixture, complete *bool) {
	// Cancel HTTP request contexts first, then Close joins admitted handlers.
	// The producer is external: only the coordinator may stop UR after this file.
	f.cancel()
	if f.stopContentHTTP != nil {
		f.stopContentHTTP()
	}
	// Joining every owner precedes all evidence-writing assertions, including Fatal.
	for i := len(f.children) - 1; i >= 0; i-- {
		p := f.children[i]
		p.cancel()
		<-p.done
		if p.cmd.ProcessState == nil || p.cmd.ProcessState.ExitCode() != 0 {
			t.Error("CC14 importer close failed")
		}
	}
	for i, p := range f.children {
		p3JSON(t, filepath.Join(f.output, fmt.Sprintf("consumer-stop-%d.json", i)), p3ProcessReceipt("cc14_consumer_stop", p.cmd, p.ctx, true))
	}
	p3JSON(t, filepath.Join(f.output, "consumers-stopped.json"), map[string]any{"run": f.cc14Scenario.run, "http_joined": true, "consumers_joined": true, "consumer_count": len(f.children), "ordinary_verification_complete": *complete, "successful": *complete && !t.Failed(), "producer_owner": "coordinator; UR not signalled by XI", "final_acceptance": "requires all UR economic records, closed, exit and cleanup evidence"})
}
func cc14Bootstrap(t *testing.T, f *p3BrowserFixture, admin *sql.DB) string {
	t.Helper()
	owner, err := db.OpenSourceFactsStore(f.adminDSN)
	p3Check(t, err == nil, "CC14 source administration")
	defer owner.Close()
	store, err := db.OpenSourceFactsStore(f.importerDSN)
	p3Check(t, err == nil, "CC14 source importer")
	defer store.Close()
	config := f.bootstrapSourceProfiles(t, owner, store)
	child := f.child(t, f.importer, "", "completed-bootstrap.jsonl", f.importerEnv(), "unirouter", "pull-feed")
	state, err := pullfeed.NewPostgresStateStore(admin, unirouter.DefaultSourceSystemID, unirouter.DefaultCompletedExecutionAdapterManifestID)
	p3Check(t, err == nil, "CC14 completed checkpoint")
	f.wait(t, "CC14 empty completed bootstrap", func(ctx context.Context) bool {
		s, e := state.Load(ctx)
		return e == nil && s.Cursor != "" && s.SourceInstance == f.source.SourceInstance && s.PagesCommitted > 0 && s.ItemsCommitted == 0
	})
	cc14JoinChild(t, f, child, "completed-bootstrap-stopped.json")
	p3Check(t, !t.Failed(), "CC14 bootstrap owner stopped")
	// This exact owner has already joined; healthy() monitors only live consumers.
	p3Check(t, len(f.children) == 1 && f.children[0] == child, "CC14 bootstrap owner identity")
	f.children = nil
	f.bootstrapContent(t, owner, store)
	var count int
	err = admin.QueryRowContext(f.ctx, `SELECT (SELECT count(*) FROM workbench_economic_facts)+(SELECT count(*) FROM source_fact_documents)+(SELECT count(*) FROM source_observation_documents)+(SELECT count(*) FROM source_content_manifests)+(SELECT count(*) FROM resource_access_records)`).Scan(&count)
	p3Check(t, err == nil && count == 0, "CC14 truly empty A/B/C bootstrap")
	p3JSON(t, filepath.Join(f.output, "empty-bootstrap.json"), map[string]any{"run": f.cc14Scenario.run, "source": f.source, "business_rows": count, "completed_bootstrap_owner_joined": true, "source_profiles": len(f.managed.Profiles), "content_collection": f.collection.ID})
	p3Check(t, cc14AtomicJSON(filepath.Join(f.runDir, "bootstrap.json"), f.source) == nil, "CC14 atomic same-source bootstrap publication")
	return config
}
func cc14WaitRequests(t *testing.T, f *p3BrowserFixture) cc14XIRequests {
	t.Helper()
	var v cc14XIRequests
	err := p3Poll(f.ctx, func() bool { _, err := os.Lstat(f.output + "/stop"); return !os.IsNotExist(err) }, func(context.Context) bool {
		cc14ProducerRunning(t, f)
		err := cc14ReadReceipt(filepath.Join(f.runDir, "ur", "requests-ready.json"), &v)
		if os.IsNotExist(err) {
			return false
		}
		p3Check(t, err == nil && cc14ValidateRequestsFor(v, f.source, f.cc14Scenario), "CC14/15 exact producer requests")
		for i, r := range v.Requests {
			var saved cc14XIRound
			var terminal cc14XITerminal
			p3Check(t, cc14ReadReceipt(filepath.Join(f.runDir, "ur", fmt.Sprintf("request-%d.json", i+1)), &saved) == nil && reflect.DeepEqual(saved, r), "CC14 individual HTTP receipt equals aggregate")
			p3Check(t, cc14ReadReceipt(filepath.Join(f.runDir, "ur", fmt.Sprintf("request-%d-ready.json", i+1)), &terminal) == nil && cc14ValidateTerminal(terminal, r), "CC14 complete A/B/C terminal for each actual request")
		}
		return true
	})
	p3Check(t, err == nil, "CC14 waiting for externally produced OpenClaw rounds")
	p3JSON(t, filepath.Join(f.output, "producer-requests-accepted.json"), v)
	return v
}
func cc14Consume(t *testing.T, f *p3BrowserFixture, admin *sql.DB, config string, v cc14XIRequests) {
	t.Helper()
	f.child(t, f.importer, "", "completed-feed.jsonl", f.importerEnv(), "unirouter", "pull-feed")
	f.child(t, f.importer, "", "source-worker.jsonl", f.importerEnv(), "unirouter", "run-source-facts", "--config", config)
	state, err := pullfeed.NewPostgresStateStore(admin, unirouter.DefaultSourceSystemID, unirouter.DefaultCompletedExecutionAdapterManifestID)
	p3Check(t, err == nil, "CC14 completed checkpoint")
	f.wait(t, "CC14/15 exact request imports", func(ctx context.Context) bool {
		cc14ProducerRunning(t, f)
		s, err := state.Load(ctx)
		if err != nil || s.ItemsCommitted != int64(f.cc14Scenario.rounds) || s.SourceInstance != f.source.SourceInstance {
			return false
		}
		var total int
		if admin.QueryRowContext(ctx, `SELECT count(*) FROM workbench_economic_facts`).Scan(&total) != nil || total != f.cc14Scenario.rounds {
			return false
		}
		for _, r := range v.Requests {
			var economic, core, runtime int
			err = admin.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM workbench_economic_facts WHERE source_system_id=$1::uuid AND request_id=$5 AND source_project_ref='project-'||$4 AND terminal_status='settled'),(SELECT count(DISTINCT profile) FROM source_fact_documents WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND request_id=$5 AND stage='terminal'),(SELECT count(DISTINCT profile) FROM source_observation_documents WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND request_id=$5)`, unirouter.DefaultSourceSystemID, f.source.SourceInstance, f.source.OrgID, f.source.ProjectID, r.RequestID).Scan(&economic, &core, &runtime)
			if err != nil || economic != 1 || core != 3 || runtime != 5 {
				return false
			}
		}
		return true
	})
	f.cli(t, "content-consume", "unirouter", "pull-source-facts", "--config", f.contentConfig, "--content-collection-id", f.collection.ID)
	count := func() []int {
		n := make([]int, 2+len(v.Requests))
		err := admin.QueryRowContext(f.ctx, `SELECT (SELECT count(*) FROM source_content_manifests WHERE collection_id=$1::uuid),(SELECT count(*) FROM source_content_receipts WHERE collection_id=$1::uuid)`, f.collection.ID).Scan(&n[0], &n[1])
		p3Check(t, err == nil, "CC14/15 C retained revision counts")
		for i, round := range v.Requests {
			err = admin.QueryRowContext(f.ctx, `SELECT count(*) FROM (SELECT DISTINCT ON(source_fact_id) source_json FROM source_content_manifests WHERE collection_id=$1::uuid AND request_id=$2 ORDER BY source_fact_id,revision DESC) latest WHERE source_json->'capture'->>'completeness'='complete'`, f.collection.ID, round.RequestID).Scan(&n[2+i])
			p3Check(t, err == nil, "CC14/15 C per-request counts")
		}
		return n
	}
	before := count()
	p3Check(t, before[0] >= 4*f.cc14Scenario.rounds && before[0] == before[1], "CC14/15 complete observations and revisions preserved")
	for _, n := range before[2:] {
		p3Check(t, n == 4, "CC14/15 four complete points per request")
	}
	f.cli(t, "content-replay", "unirouter", "pull-source-facts", "--config", f.contentConfig, "--content-collection-id", f.collection.ID)
	after := count()
	p3Check(t, reflect.DeepEqual(before, after), "CC14/15 C replay changes no manifest/receipt")
	store, err := db.OpenSourceFactsStore(f.importerDSN)
	p3Check(t, err == nil, "CC14 checkpoint inspection")
	defer store.Close()
	for _, p := range f.managed.Profiles {
		s, e := store.LoadSourceFactsStream(f.ctx, p.StreamID)
		p3Check(t, e == nil && s.Status == "active" && !s.RequiresAttention && s.LastError == "", "CC14 B current checkpoint")
	}
	c, err := store.LoadContentCollection(f.ctx, f.collection.ID)
	p3Check(t, err == nil && c.Cursor != "" && c.Status == "active" && !c.RequiresAttention && c.LastError == "" && sourcefacts.SameContentCollection(c, f.collection), "CC14 C current checkpoint")
	p3JSON(t, filepath.Join(f.output, "content-imported.json"), map[string]any{"run": f.cc14Scenario.run, "counts_before": before, "counts_after": after, "collection": c, "cursor_sha256": sourcefacts.Hash([]byte(c.Cursor))})
}

type cc14Point struct {
	Manifest         sourcefacts.ContentManifest `json:"manifest"`
	Hash             string                      `json:"source_sha256"`
	LegacyBodyDenied bool                        `json:"legacy_body_denied"`
}

func cc14DecodePoint(raw []byte) (cc14Point, error) {
	var point cc14Point
	if err := cc14Decode(raw, &point); err != nil {
		return cc14Point{}, err
	}
	if !point.LegacyBodyDenied {
		return cc14Point{}, errors.New("CC14 legacy body denial required")
	}
	return point, nil
}

func cc14ReadPoints(t *testing.T, path string) []cc14Point {
	t.Helper()
	raw, err := cc14ReadDocument(path)
	p3Check(t, err == nil, "CC14 producer point receipt")
	var items []json.RawMessage
	p3Check(t, json.Unmarshal(raw, &items) == nil && len(items) == 4, "CC14 four producer observations")
	points := make([]cc14Point, 4)
	for i, item := range items {
		points[i], err = cc14DecodePoint(item)
		p3Check(t, err == nil, "CC14 strict point receipt and legacy body denial")
	}
	return points
}

// Select only a location actually present in captured client bytes. The second
// round proves a client-reported tool result; it is not an independent read audit.
func cc14ReviewPointer(raw []byte, round int) (string, error) {
	if _, err := connector.DecodeStrictJSONObject(raw); err != nil {
		return "", errors.New("CC14 client request document")
	}
	var v struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
			CallID  string          `json:"tool_call_id"`
			Calls   []struct {
				ID       string `json:"id"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return "", errors.New("CC14 client request messages")
	}
	pointer := ""
	tools, calls := 0, 0
	for i, m := range v.Messages {
		if round == 1 && m.Role == "user" && len(m.Content) > 0 {
			pointer = fmt.Sprintf("/messages/%d/content", i)
		}
		if m.Role == "assistant" {
			for _, call := range m.Calls {
				if call.ID == "callcc14readsample" && call.Function.Name == "read" {
					calls++
				}
			}
		}
		if m.Role == "tool" && m.CallID == "callcc14readsample" && len(m.Content) > 0 {
			tools++
			if round == 2 {
				pointer = fmt.Sprintf("/messages/%d/content", i)
			}
		}
	}
	if pointer == "" || round != 1 && round != 2 || round == 1 && (tools != 0 || calls != 0) || round == 2 && (tools != 1 || calls != 1) {
		return "", errors.New("CC14 client tool chronology")
	}
	return pointer, nil
}

// CC15 selects one client-reported result from the actual captured request.
// Runtime execution and file readback are verified by separate owners.
func cc15ReviewPointer(raw []byte, round int) (string, error) {
	if round < 1 || round > 5 {
		return "", errors.New("CC15 round")
	}
	if _, err := connector.DecodeStrictJSONObject(raw); err != nil {
		return "", errors.New("CC15 client request document")
	}
	var v struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
			CallID  string          `json:"tool_call_id"`
			Calls   []struct {
				ID       string `json:"id"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return "", errors.New("CC15 client request messages")
	}
	if round == 1 {
		pointer := ""
		for i, m := range v.Messages {
			if m.Role == "tool" || len(m.Calls) != 0 {
				return "", errors.New("CC15 unexpected initial tool history")
			}
			if m.Role == "user" && len(m.Content) > 0 {
				pointer = fmt.Sprintf("/messages/%d/content", i)
			}
		}
		if pointer == "" {
			return "", errors.New("CC15 initial user content missing")
		}
		return pointer, nil
	}
	ids := []string{"callcc15readinput", "callcc15readreport", "callcc15editreport", "callcc15readupdated"}
	names := []string{"read", "read", "edit", "read"}
	id, name := ids[round-2], names[round-2]
	calls, results, pointer, callIndex, resultIndex := 0, 0, "", -1, -1
	for i, m := range v.Messages {
		if m.Role == "assistant" {
			for _, call := range m.Calls {
				if call.ID == id && call.Function.Name == name {
					calls++
					callIndex = i
				}
			}
		}
		if m.Role == "tool" && m.CallID == id && len(m.Content) > 0 {
			results++
			resultIndex = i
			pointer = fmt.Sprintf("/messages/%d/content", i)
		}
	}
	if calls != 1 || results != 1 || callIndex >= resultIndex {
		return "", errors.New("CC15 exact prior client tool result required")
	}
	return pointer, nil
}

func cc14OrdinaryProof(t *testing.T, f *p3BrowserFixture, server *httptest.Server, v cc14XIRequests) {
	t.Helper()
	client := f.ordinaryLogin(t, server, "reviewer")
	client.client.Timeout = 15 * time.Second
	get := func(method, path string, body any, status int, target any) {
		code, raw, _ := f.contentHTTP(t, client, method, path, body)
		p3Check(t, code == status && (target == nil || json.Unmarshal(raw, target) == nil), "CC14 ordinary HTTP proof: "+path)
	}
	input := httpReadingRequest(unirouter.DefaultSourceSystemID)
	input.Filters.SourceProjects = []string{"project-" + f.source.ProjectID}
	input.MoneyBasis = workbench.MoneyCalculatedReference
	var cost workbench.Reading
	get("POST", "/api/v1/workbench/readings", input, 201, &cost)
	var contributions workbench.Contributions
	get("GET", "/api/v1/workbench/readings/"+cost.ID+"/contributions?dimension=source_project", nil, 200, &contributions)
	p3Check(t, len(contributions.Rows) == 1 && contributions.Rows[0].DrilldownAllowed, "CC14 same project contribution")
	var details workbench.Details
	get("GET", "/api/v1/workbench/readings/"+cost.ID+"/details?contribution_ref="+url.QueryEscape(contributions.Rows[0].Ref), nil, 200, &details)
	p3Check(t, len(details.Rows) == f.cc14Scenario.rounds && details.NextCursor == "", "CC14/15 exact ordinary economic rows")
	p3JSON(t, filepath.Join(f.output, "ordinary-economic.json"), details)
	var runtime workbench.RuntimeReading
	runtimeInput := workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: input.Period, Filters: input.Filters}
	get("POST", "/api/v1/runtime/readings", runtimeInput, 201, &runtime)
	var requests workbench.RuntimeRequests
	base := "/api/v1/runtime/readings/" + runtime.ID
	get("GET", base+"/requests?entry_time=all", nil, 200, &requests)
	p3Check(t, len(requests.Items) == f.cc14Scenario.rounds && requests.NextCursor == "", "CC14/15 exact runtime requests")
	p3JSON(t, filepath.Join(f.output, "ordinary-readings.json"), map[string]any{"cost": cost, "runtime": runtime, "requests": requests})
	denied := f.ordinaryLogin(t, server, "reader")
	denied.client.Timeout = 15 * time.Second
	var deniedReading workbench.RuntimeReading
	status, raw, _ := f.contentHTTP(t, denied, "POST", "/api/v1/runtime/readings", runtimeInput)
	p3Check(t, status == 201 && json.Unmarshal(raw, &deniedReading) == nil, "CC14 no-C user owns its reading")
	deniedBase := "/api/v1/runtime/readings/" + deniedReading.ID + "/requests"
	var deniedRequests workbench.RuntimeRequests
	status, raw, _ = f.contentHTTP(t, denied, "GET", deniedBase+"?entry_time=all", nil)
	p3Check(t, status == 200 && json.Unmarshal(raw, &deniedRequests) == nil && len(deniedRequests.Items) == f.cc14Scenario.rounds, "CC14 no-C user sees actual B requests")
	var formalCases []workspace.ReviewCreationReceipt
	seen := map[string]bool{}
	for i, round := range v.Requests {
		var row workbench.DetailRow
		for _, d := range details.Rows {
			if d.RequestID == round.RequestID {
				p3Check(t, !seen[d.RequestID], "CC14 unique economic request")
				row = d
				seen[d.RequestID] = true
			}
		}
		p3Check(t, row.ExecutionID != "" && row.SourceSystemID == unirouter.DefaultSourceSystemID && row.SourceProject == "project-"+f.source.ProjectID && row.TerminalStatus == "settled" && row.GatewayChargedMicros == "0" && row.CalculatedReferenceMicros != "" && row.CalculatedReferenceMicros != "0", "CC14 exact economic identity and synthetic cost")
		var binding workbench.RuntimeExecutionRequest
		get("GET", base+"/executions/"+row.ExecutionID, nil, 200, &binding)
		request := binding.Request
		p3Check(t, binding.ReadingID == runtime.ID && binding.ExecutionID == row.ExecutionID && request.RequestID == round.RequestID && request.SourceSystemID == unirouter.DefaultSourceSystemID && request.SourceInstance == f.source.SourceInstance && request.OrgID == f.source.OrgID && request.ProjectID == f.source.ProjectID, "CC14 exact I01/runtime association")
		found := false
		for _, r := range requests.Items {
			found = found || (r.Ref == request.Ref && r.RequestID == round.RequestID)
		}
		p3Check(t, found, "CC14 runtime list/detail identity")
		path := base + "/requests/" + request.Ref + "/contents"
		var contents workbench.RuntimeContents
		get("GET", path, nil, 200, &contents)
		p3Check(t, len(contents.Items) == 4 && contents.NextCursor == "", "CC14 four content refs per actual round")
		points := cc14ReadPoints(t, filepath.Join(f.runDir, "ur", fmt.Sprintf("content-points-%d.json", i)))
		observations := map[string]bool{}
		var choice workspace.ContentChoice
		for _, item := range contents.Items {
			ref := item.Reference
			p3Check(t, ref.SourceSystemID == unirouter.DefaultSourceSystemID && ref.SourceInstance == f.source.SourceInstance && ref.OrgID == f.source.OrgID && ref.ProjectID == f.source.ProjectID && ref.RequestID == round.RequestID && ref.CaptureStatus == "complete" && ref.Representation != nil && item.Observation != nil, "CC14 frozen complete source/request content identity")
			var point *cc14Point
			for j := range points {
				p := &points[j]
				if p.Manifest.SourceFactID == ref.SourceFactID && p.Manifest.Revision == ref.Revision && p.Hash == ref.SourceSHA256 {
					point = p
				}
			}
			p3Check(t, point != nil && sourcefacts.MatchContentManifestReference(ref, sourcefacts.ContentManifestEntry{Document: point.Manifest, SourceSHA256: point.Hash}) == nil && point.Manifest.ObservationPoint == item.Observation.ObservationPoint && !observations[item.Observation.ObservationPoint], "CC14 UR/XI exact four-point manifest/hash match")
			observations[item.Observation.ObservationPoint] = true
			bodyPath := path + "/" + ref.ContentRef + "?source_fact_id=" + url.QueryEscape(ref.SourceFactID) + "&revision=" + url.QueryEscape(ref.Revision)
			status, body, headers := f.contentHTTP(t, client, "GET", bodyPath, nil)
			metadataRaw, err := base64.RawURLEncoding.DecodeString(headers.Get(pullfeed.ContentMetadataHeader))
			var metadata sourcefacts.ContentReadMetadata
			p3Check(t, status == 200 && int64(len(body)) == ref.Representation.SavedBytes && sourcefacts.Hash(body) == ref.Representation.Digest && err == nil && json.Unmarshal(metadataRaw, &metadata) == nil && metadata.SourceFactID == ref.SourceFactID && metadata.SourceSHA256 == ref.SourceSHA256 && metadata.ContentRef == ref.ContentRef && reflect.DeepEqual(metadata.Representation, *ref.Representation), "CC14 actual byte/hash verification")
			p3JSON(t, filepath.Join(f.output, fmt.Sprintf("round-%d-content-%s.json", i+1, item.Observation.ObservationPoint)), map[string]any{"reference": ref, "metadata": metadata, "downloaded_bytes": len(body), "downloaded_sha256": sourcefacts.Hash(body)})
			if item.Observation.ObservationPoint == "client_request" {
				var pointer string
				var err error
				if f.cc14Scenario.fileActions {
					pointer, err = cc15ReviewPointer(body, round.Round)
				} else {
					pointer, err = cc14ReviewPointer(body, round.Round)
				}
				p3Check(t, err == nil, "CC14 actual client message chronology")
				choice = workspace.ContentChoice{Scope: "finding", Relation: "supports", Reference: ref, Location: sourcefacts.ContentLocation{Kind: "json_pointer", Pointer: &pointer}}
			}
		}
		p3Check(t, observations["client_request"] && observations["provider_request"] && observations["provider_response"] && observations["client_response"] && choice.Reference.ContentRef != "", "CC14 complete four-point set")
		deniedRequestRef := ""
		for _, item := range deniedRequests.Items {
			if item.RequestID == round.RequestID && item.SourceSystemID == unirouter.DefaultSourceSystemID && item.SourceInstance == f.source.SourceInstance && item.OrgID == f.source.OrgID && item.ProjectID == f.source.ProjectID {
				deniedRequestRef = item.Ref
			}
		}
		p3Check(t, deniedRequestRef != "", "CC14 no-C user's exact request")
		ref := choice.Reference
		status, _, _ = f.contentHTTP(t, denied, "GET", deniedBase+"/"+deniedRequestRef+"/contents/"+ref.ContentRef+"?source_fact_id="+url.QueryEscape(ref.SourceFactID)+"&revision="+url.QueryEscape(ref.Revision), nil)
		p3Check(t, status == 403 || status == 404, "CC14 same-project reader without content authority denied")
		p3JSON(t, filepath.Join(f.output, fmt.Sprintf("round-%d-no-content-grant.json", i+1)), map[string]any{"status": status, "request_id": round.RequestID, "username": f.ordinaryUsername("reader"), "owns_reading": true})
		command := workspace.ReviewCreationCommand{Operation: "start_review", OperationID: randomTestUUID(t), NewCase: &workspace.CreateCaseInput{Title: fmt.Sprintf("%s round %d captured request", f.cc14Scenario.run, i+1)}, Finding: workspace.CreateFindingInput{Title: "Captured client evidence", Statement: "The captured client request contains the selected message; tool results are client-reported", Classification: "hypothesis"}, Selection: workspace.BasisSelection{Contents: []workspace.ContentChoice{choice}}, Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Verified the stored representation and selected actual JSON location"}, Reason: "CC14 same-user execution content review", ExecutionContext: &workspace.ExecutionContext{WorkbenchReadingID: cost.ID, ExecutionID: row.ExecutionID, ExecutionAsOf: cost.Cutoff, RuntimeReadingID: runtime.ID, RuntimeRequestRef: request.Ref}}
		var basis workspace.ReviewBasis
		get("POST", "/api/v1/security/reviews/preview", command, 200, &basis)
		p3Check(t, basis.ResolvedExecutionContext != nil && workspace.SameAssessmentValue(basis.ResolvedExecutionContext.Contents, command.Selection.Contents), "CC14 preview bound exact content context")
		command.ExpectedBasisDigest = basis.Digest
		var created workspace.ReviewCreationReceipt
		get("POST", "/api/v1/security/reviews", command, 200, &created)
		var reopened workspace.AssessmentView
		get("GET", "/api/v1/cases/"+created.CaseID+"/findings/"+created.FindingID+"/assessment", nil, 200, &reopened)
		p3Check(t, reopened.Assessment.Review != nil && workspace.SameAssessmentValue(reopened.Assessment.Review.Basis.ResolvedExecutionContext, created.Basis.ResolvedExecutionContext), "CC14 reopened durable content origin")
		var record workspace.CaseRecord
		get("GET", "/api/v1/cases/"+created.CaseID, nil, 200, &record)
		p3Check(t, len(record.Executions) == 1 && record.Executions[0].ExecutionID == row.ExecutionID && record.Executions[0].AsOf.Equal(cost.Cutoff) && len(record.Contents) == 1 && workspace.SameAssessmentValue(record.Contents[0].Reference, choice) && len(record.Resources) == 0 && len(record.Evidence) == 0 && len(record.Gaps) == 0, "CC14 atomic origin and selected content, no fabricated resource audit")
		formalCases = append(formalCases, created)
		p3JSON(t, filepath.Join(f.output, fmt.Sprintf("round-%d-review.json", i+1)), map[string]any{"round": round, "economic": row, "runtime_request": request, "contents": contents, "receipt": created, "case": record, "reopened": reopened, "username": f.ordinaryUsername("reviewer"), "tool_result_evidence": "client report, not independent resource audit"})
	}
	if f.formal != nil {
		// Keep both no-C round proofs above. Then explicitly grant only the same
		// source/project human_review domain; role remains ordinary reader.
		f.formalGrant(t, "reader-content", "reader", unirouter.DefaultSourceSystemID, "project-"+f.source.ProjectID, false, true)
		reader := f.ordinaryLogin(t, server, "reader")
		for _, created := range formalCases {
			var record workspace.CaseRecord
			status, raw, _ := f.contentHTTP(t, reader, "GET", "/api/v1/cases/"+created.CaseID, nil)
			p3Check(t, status == 200 && json.Unmarshal(raw, &record) == nil && len(record.Executions) == 1 && len(record.Contents) == 1, "formal reader sees persisted execution/content case")
			status, raw, _ = f.contentHTTP(t, reader, "GET", "/api/v1/cases/"+created.CaseID+"/findings/"+created.FindingID+"/assessment", nil)
			var reopened workspace.AssessmentView
			p3Check(t, status == 200 && json.Unmarshal(raw, &reopened) == nil && reopened.Assessment.Review != nil && workspace.SameAssessmentValue(reopened.Assessment.Review.Basis.ResolvedExecutionContext, created.Basis.ResolvedExecutionContext), "formal reader sees same persisted review")
		}
		status, _, _ := f.contentHTTP(t, reader, "POST", "/api/v1/security/reviews", map[string]any{})
		p3Check(t, status == 403, "formal content reader cannot write review")
		p3JSON(t, filepath.Join(f.output, "formal-reader-content-fence.json"), map[string]any{"case_count": len(formalCases), "case_get_status": 200, "assessment_get_status": 200, "review_write_status": 403, "service_restart_verified": false})
	}

}

func TestCC14OpenClawWorkbenchHarness(t *testing.T) {
	if os.Getenv("XI_CC14_INTEROP") != "1" {
		t.Skip("CC14 exact runtime opt-in required")
	}
	cc14RunWorkbenchHarness(t)
}
func TestCC15OpenClawWorkbenchHarness(t *testing.T) {
	if os.Getenv("XI_CC15_INTEROP") != "1" {
		t.Skip("CC15 exact runtime opt-in required")
	}
	cc14RunWorkbenchHarness(t)
}
func cc14RunWorkbenchHarness(t *testing.T) {
	t.Helper()
	f := newCC14XI(t)
	complete := false
	defer cc14StopConsumers(t, f, &complete)
	server, admin, _, source, _ := startWorkbenchProductionFixture(t, true, f)
	p3Check(t, source == unirouter.DefaultSourceSystemID, "CC14 native source identity")
	var name, oid, system string
	err := admin.QueryRowContext(f.ctx, `SELECT current_database(),(SELECT oid::text FROM pg_database WHERE datname=current_database()),system_identifier::text FROM pg_control_system()`).Scan(&name, &oid, &system)
	p3Check(t, err == nil && name != os.Getenv("XI_P3_BASE_DATABASE") && cc14Positive(oid) && system == os.Getenv("XI_P3_SYSTEM_ID"), "CC14 native disposable XI database")
	p3JSON(t, filepath.Join(f.output, "database.json"), map[string]any{"run": f.cc14Scenario.run, "database": name, "oid": oid, "system_id": system, "cleanup_owner": "existing startWorkbenchProductionFixture exact-name native cleanup"})
	config := cc14Bootstrap(t, f, admin)
	requests := cc14WaitRequests(t, f)
	cc14Consume(t, f, admin, config, requests)
	cc14OrdinaryProof(t, f, server, requests)
	if f.cc14Scenario.fileActions {
		cc15ActionProof(t, f, server, admin, requests)
	}
	p3JSON(t, filepath.Join(f.output, "ready.json"), map[string]any{"run": f.cc14Scenario.run, "base_url": server.URL, "database_name": name, "source": f.source, "username": f.ordinaryUsername("reviewer"), "requests": requests.Requests, "ordinary_verification_complete": true, "evidence_boundary": "component production HTTPS/PG/importers; provider synthetic; OpenClaw and final UR closure owned by coordinator"})
	err = p3Poll(f.ctx, func() bool { return false }, func(context.Context) bool { f.healthy(t); cc14ProducerRunning(t, f); return cc14StopRequested(t, f) })
	p3Check(t, err == nil, "CC14 coordinator stop after OpenClaw/browser owners joined")
	complete = true
}

func cc14ProducerRunning(t *testing.T, f *p3BrowserFixture) {
	t.Helper()
	for _, name := range []string{"closed.json", "extra-request.json"} {
		_, err := os.Lstat(filepath.Join(f.runDir, "ur", name))
		p3Check(t, os.IsNotExist(err), "CC14 producer closed or unexpected request before consumers stopped")
	}
}

func cc15GuardEnvironment() map[string]string {
	env := cc14GuardEnvironment()
	for _, key := range []string{"XI_CC14_INTEROP", "CC14_APPROVED_RUN", "CC14_APPROVED_ACTIONS", "CC14_XI_APPROVED_BINARY_SHA256", "CC14_UR_APPROVED_BINARY_SHA256"} {
		delete(env, key)
	}
	env["XI_CC15_INTEROP"] = "1"
	env["CC15_APPROVED_RUN"] = cc15XIRun
	env["CC15_APPROVED_ACTIONS"] = cc15XIActions
	env["CC15_XI_APPROVED_BINARY_SHA256"] = strings.Repeat("a", 64)
	env["CC15_UR_APPROVED_BINARY_SHA256"] = strings.Repeat("b", 64)
	env["P3_CREDENTIAL_DIR"] = cc15FileScenario().credentials()
	env["XINSIGHT_SCOPED_BROWSER_OUTPUT"] = cc15FileScenario().output()
	return env
}
func TestCC15XIAdmissionGuardPure(t *testing.T) {
	env := cc15GuardEnvironment()
	get := func(k string) string { return env[k] }
	if err := cc14XIValidateEnvironment(get); err != nil {
		t.Fatal("CC15 exact inert admission rejected")
	}
	for _, tc := range []struct{ name, key, value string }{
		{"missing-mode", "XI_CC15_INTEROP", ""}, {"shared-mode", "XI_CC14_INTEROP", "1"},
		{"wrong-run", "CC15_APPROVED_RUN", cc15XIRun + "-copy"}, {"old-actions", "CC15_APPROVED_ACTIONS", cc14XIActions},
		{"old-binary", "CC14_XI_APPROVED_BINARY_SHA256", strings.Repeat("a", 64)},
		{"wrong-credentials", "P3_CREDENTIAL_DIR", cc14XICredentials},
		{"wrong-output", "XINSIGHT_SCOPED_BROWSER_OUTPUT", cc14XIOutput},
		{"UR-mode", "UR_CC15_INTEROP", "1"}, {"XI-digest", "CC15_XI_APPROVED_BINARY_SHA256", strings.Repeat("A", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := env[tc.key]
			env[tc.key] = tc.value
			defer func() { env[tc.key] = old }()
			if cc14XIValidateEnvironment(get) == nil {
				t.Fatal("unsafe CC15 admission")
			}
		})
	}
}
func TestCC15XIClientPointerPure(t *testing.T) {
	ids := []string{"callcc15readinput", "callcc15readreport", "callcc15editreport", "callcc15readupdated"}
	names := []string{"read", "read", "edit", "read"}
	for round := 2; round <= 5; round++ {
		round := round
		t.Run(fmt.Sprint(round), func(t *testing.T) {
			wire, _ := json.Marshal(map[string]any{"messages": []any{
				map[string]any{"role": "user", "content": "task"},
				map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": ids[round-2], "function": map[string]any{"name": names[round-2]}}}},
				map[string]any{"role": "tool", "tool_call_id": ids[round-2], "content": "result"},
			}})
			pointer, err := cc15ReviewPointer(wire, round)
			if err != nil || pointer != "/messages/2/content" {
				t.Fatal("exact CC15 client result rejected")
			}
			wrong := bytes.ReplaceAll(wire, []byte(ids[round-2]), []byte("wrong"))
			wrong = bytes.Replace(wrong, []byte(`"tool_call_id":"wrong"`), []byte(`"tool_call_id":"other"`), 1)
			if _, err = cc15ReviewPointer(wrong, round); err == nil {
				t.Fatal("unmatched client result accepted")
			}
			reversed, _ := json.Marshal(map[string]any{"messages": []any{
				map[string]any{"role": "user", "content": "task"},
				map[string]any{"role": "tool", "tool_call_id": ids[round-2], "content": "result"},
				map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": ids[round-2], "function": map[string]any{"name": names[round-2]}}}},
			}})
			if _, err = cc15ReviewPointer(reversed, round); err == nil {
				t.Fatal("result preceding call accepted")
			}
		})
	}
	first, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "user", "content": "task"}}})
	if pointer, err := cc15ReviewPointer(first, 1); err != nil || pointer != "/messages/0/content" {
		t.Fatal("first-round user pointer rejected")
	}
	firstWithTool, _ := json.Marshal(map[string]any{"messages": []any{
		map[string]any{"role": "user", "content": "task"},
		map[string]any{"role": "tool", "tool_call_id": ids[0], "content": "unexpected"},
	}})
	if _, err := cc15ReviewPointer(firstWithTool, 1); err == nil {
		t.Fatal("first-round tool history accepted")
	}
}

func TestCC15XIReceiptGuardPure(t *testing.T) {
	env := cc15GuardEnvironment()
	get := func(k string) string { return env[k] }
	ready, id, databases, requests := cc14ReceiptFixture()
	scenario := cc15FileScenario()
	ready.URL, ready.CredentialDir = scenario.url, scenario.credentials()
	id.Run, id.Binary, id.Upstream = scenario.run, env[scenario.urBinaryKey], scenario.upstream
	databases.Run = scenario.run
	requests.Run, requests.Source = scenario.run, ready
	requests.Count, requests.Economic, requests.Points, requests.Calls = 5, 5, 20, 5
	requests.Requests = nil
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= scenario.rounds; i++ {
		requests.Requests = append(requests.Requests, cc14XIRound{Round: i, RequestID: fmt.Sprintf("real-%d", i), ProviderResponseID: fmt.Sprintf("cc15-%s-%d", scenario.run, i), SHA: strings.Repeat("a", 64), Status: 200, Calls: 1, Started: now.Add(time.Duration(2*i) * time.Second), Ended: now.Add(time.Duration(2*i+1) * time.Second)})
	}
	if !cc14ValidateProducerFor(ready, id, databases, get, scenario) || !cc14ValidateRequestsFor(requests, ready, scenario) {
		t.Fatal("exact CC15 producer receipts rejected")
	}
	for _, tc := range []struct {
		name string
		edit func(*cc14XIRequests)
	}{
		{"missing-fifth-round", func(v *cc14XIRequests) { v.Requests = v.Requests[:4] }},
		{"wrong-point-count", func(v *cc14XIRequests) { v.Points = 8 }},
		{"missing-response-id", func(v *cc14XIRequests) { v.Requests[2].ProviderResponseID = "" }},
		{"wrong-response-id", func(v *cc14XIRequests) { v.Requests[2].ProviderResponseID = "cc15-other-3" }},
		{"duplicate-request-id", func(v *cc14XIRequests) { v.Requests[2].RequestID = v.Requests[1].RequestID }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := requests
			bad.Requests = append([]cc14XIRound(nil), requests.Requests...)
			tc.edit(&bad)
			if cc14ValidateRequestsFor(bad, ready, scenario) {
				t.Fatal("invalid CC15 receipt accepted")
			}
		})
	}
}

func cc14GuardEnvironment() map[string]string {
	return map[string]string{"XI_FORMAL_ACCOUNTS": "1", "P3_XI_CTL": p3Root + "/candidate/bin/xinsightctl", "P3_XI_CTL_SHA256": strings.Repeat("d", 64), "XI_CC14_INTEROP": "1", "CC14_APPROVED_RUN": cc14XIRun, "CC14_APPROVED_ACTIONS": cc14XIActions, "P3_CREDENTIAL_DIR": cc14XICredentials, "XINSIGHT_SCOPED_BROWSER_OUTPUT": cc14XIOutput, "CC14_XI_APPROVED_BINARY_SHA256": strings.Repeat("a", 64), "CC14_UR_APPROVED_BINARY_SHA256": strings.Repeat("b", 64), "P3_XI_IMPORTER_SHA256": strings.Repeat("c", 64), "P3_XI_IMPORTER": p3Root + "/candidate/bin/xinsight-importer", "XINSIGHT_POSTGRES_DSN": "postgres://synthetic@127.0.0.1:56134/xi_cc14_base?sslmode=disable", "XI_P3_BASE_DATABASE": "xi_cc14_base", "XI_P3_DATABASE_OID": "11", "XI_P3_SYSTEM_ID": "12", "UR_P3_BASE_DATABASE": "ur_cc14_base", "UR_P3_DATABASE_OID": "21", "UR_P3_SYSTEM_ID": "22"}
}
func TestCC14XIAdmissionGuardPure(t *testing.T) {
	env := cc14GuardEnvironment()
	get := func(k string) string { return env[k] }
	if cc14XIValidateEnvironment(get) != nil {
		t.Fatal("exact admission rejected")
	}
	for _, tc := range []struct{ name, key, value string }{
		{"formal-mode", "XI_FORMAL_ACCOUNTS", ""}, {"previous-r8-run", "CC14_APPROVED_RUN", "cc14-openclaw-20260924-r8"}, {"previous-r8-credentials", "P3_CREDENTIAL_DIR", p3Root + "/credentials/xi-product-test/cc14-openclaw-20260924-r8"}, {"previous-r8-output", "XINSIGHT_SCOPED_BROWSER_OUTPUT", p3Root + "/evidence/cc14-openclaw-20260924-r8/xi"}, {"previous-r7-run", "CC14_APPROVED_RUN", "cc14-openclaw-20260923-r7"}, {"previous-r7-credentials", "P3_CREDENTIAL_DIR", p3Root + "/credentials/xi-product-test/cc14-openclaw-20260923-r7"}, {"previous-r7-output", "XINSIGHT_SCOPED_BROWSER_OUTPUT", p3Root + "/evidence/cc14-openclaw-20260923-r7/xi"}, {"opt-in", "XI_CC14_INTEROP", ""}, {"old-run", "CC14_APPROVED_RUN", "p3-j1-content-20260922-r2"}, {"previous-cc14-run", "CC14_APPROVED_RUN", "cc14-openclaw-20260922-r1"}, {"previous-cc14-credential-path", "P3_CREDENTIAL_DIR", p3Root + "/credentials/xi-product-test/cc14-openclaw-20260922-r1"}, {"previous-cc14-output-path", "XINSIGHT_SCOPED_BROWSER_OUTPUT", p3Root + "/evidence/cc14-openclaw-20260922-r1/xi"}, {"previous-r2-run", "CC14_APPROVED_RUN", "cc14-openclaw-20260923-r2"}, {"previous-r2-credential-path", "P3_CREDENTIAL_DIR", p3Root + "/credentials/xi-product-test/cc14-openclaw-20260923-r2"}, {"previous-r2-output-path", "XINSIGHT_SCOPED_BROWSER_OUTPUT", p3Root + "/evidence/cc14-openclaw-20260923-r2/xi"}, {"previous-r3-run", "CC14_APPROVED_RUN", "cc14-openclaw-20260923-r3"}, {"previous-r3-credential-path", "P3_CREDENTIAL_DIR", p3Root + "/credentials/xi-product-test/cc14-openclaw-20260923-r3"}, {"previous-r3-output-path", "XINSIGHT_SCOPED_BROWSER_OUTPUT", p3Root + "/evidence/cc14-openclaw-20260923-r3/xi"}, {"previous-r4-run", "CC14_APPROVED_RUN", "cc14-openclaw-20260923-r4"}, {"previous-r4-credential-path", "P3_CREDENTIAL_DIR", p3Root + "/credentials/xi-product-test/cc14-openclaw-20260923-r4"}, {"previous-r4-output-path", "XINSIGHT_SCOPED_BROWSER_OUTPUT", p3Root + "/evidence/cc14-openclaw-20260923-r4/xi"}, {"previous-r5-run", "CC14_APPROVED_RUN", "cc14-openclaw-20260923-r5"}, {"previous-r5-credential-path", "P3_CREDENTIAL_DIR", p3Root + "/credentials/xi-product-test/cc14-openclaw-20260923-r5"}, {"previous-r5-output-path", "XINSIGHT_SCOPED_BROWSER_OUTPUT", p3Root + "/evidence/cc14-openclaw-20260923-r5/xi"}, {"previous-r6-run", "CC14_APPROVED_RUN", "cc14-openclaw-20260923-r6"}, {"previous-r6-credential-path", "P3_CREDENTIAL_DIR", p3Root + "/credentials/xi-product-test/cc14-openclaw-20260923-r6"}, {"previous-r6-output-path", "XINSIGHT_SCOPED_BROWSER_OUTPUT", p3Root + "/evidence/cc14-openclaw-20260923-r6/xi"}, {"actions", "CC14_APPROVED_ACTIONS", "http"}, {"old-actions", "CC14_APPROVED_ACTIONS", "synthetic-db-redis,http,credentials,content-binding,full-content-policy,human-review-grant,secret-cleanup,openclaw-read-two-rounds"}, {"credential-path", "P3_CREDENTIAL_DIR", cc14XICredentials + "/"}, {"output-path", "XINSIGHT_SCOPED_BROWSER_OUTPUT", cc14XIOutput + "-other"}, {"J1-mode", "XI_J1_INTEROP", "1"}, {"UR-mode", "UR_CC14_INTEROP", "1"}, {"no-B-mode", "XI_NO_B_BROWSER", "1"}, {"binary-digest", "CC14_XI_APPROVED_BINARY_SHA256", strings.Repeat("A", 64)}, {"UR-digest", "CC14_UR_APPROVED_BINARY_SHA256", ""}, {"importer-digest", "P3_XI_IMPORTER_SHA256", "short"}, {"importer-path", "P3_XI_IMPORTER", "/tmp/importer"}, {"system-id", "XI_P3_SYSTEM_ID", "0"}, {"UR-oid", "UR_P3_DATABASE_OID", ""}, {"wrong-port", "XINSIGHT_POSTGRES_DSN", "postgres://synthetic@127.0.0.1:56132/xi_cc14_base?sslmode=disable"}, {"remote-host", "XINSIGHT_POSTGRES_DSN", "postgres://synthetic@remote.invalid:56134/xi_cc14_base?sslmode=disable"}, {"wrong-base", "XI_P3_BASE_DATABASE", "postgres"}, {"runtime-option", "XINSIGHT_POSTGRES_DSN", "postgres://synthetic@127.0.0.1:56134/xi_cc14_base?sslmode=disable&search_path=evil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := cc14GuardEnvironment()
			e[tc.key] = tc.value
			if cc14XIValidateEnvironment(func(k string) string { return e[k] }) == nil {
				t.Fatal("invalid admission accepted")
			}
		})
	}
}
func cc14ReceiptFixture() (p3ProducerReady, cc14XIIdentity, cc14XIDatabases, cc14XIRequests) {
	env := cc14GuardEnvironment()
	ready := p3ProducerReady{URL: "http://127.0.0.1:19185", SourceInstance: "synthetic-http", OrgID: "17", ProjectID: "101", CredentialDir: cc14XICredentials, Content: &j1ProducerContent{CredentialID: "synthetic-c", RegistrationHash: "sha256:" + strings.Repeat("a", 64), CapabilitiesSHA256: "sha256:" + strings.Repeat("b", 64)}}
	id := cc14XIIdentity{Run: cc14XIRun, Binary: env["CC14_UR_APPROVED_BINARY_SHA256"], Database: "ur_clone", OID: "23", System: "22", Base: "ur_cc14_base", BaseOID: "21", GatewayKeyID: 1, Upstream: "http://127.0.0.1:19186"}
	databases := cc14XIDatabases{Run: cc14XIRun, System: "22", Base: "ur_cc14_base", BaseOID: "21", Clone: "ur_clone", Created: map[string]string{"ur_template": "24", "ur_clone": "23"}, Unchanged: true, Cleanup: "native SetupMigratedTestDB/TestMain; exact names only"}
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	requests := cc14XIRequests{Run: cc14XIRun, Source: ready, Count: 2, Economic: 2, Points: 8, Calls: 2, Requests: []cc14XIRound{{Round: 1, RequestID: "real-1", SHA: strings.Repeat("a", 64), Status: 200, Calls: 1, Started: now, Ended: now.Add(time.Second)}, {Round: 2, RequestID: "real-2", SHA: strings.Repeat("b", 64), Status: 200, Calls: 1, Started: now.Add(2 * time.Second), Ended: now.Add(3 * time.Second)}}}
	return ready, id, databases, requests
}
func TestCC14XIReceiptGuardPure(t *testing.T) {
	env := cc14GuardEnvironment()
	get := func(k string) string { return env[k] }
	ready, id, databases, requests := cc14ReceiptFixture()
	if !cc14ValidateProducer(ready, id, databases, get) || !cc14ValidateRequests(requests, ready) {
		t.Fatal("exact producer receipts rejected")
	}
	for name, mutate := range map[string]func(*p3ProducerReady, *cc14XIIdentity, *cc14XIDatabases){"wrong-source": func(r *p3ProducerReady, _ *cc14XIIdentity, _ *cc14XIDatabases) { r.SourceInstance = "other" }, "missing-content": func(r *p3ProducerReady, _ *cc14XIIdentity, _ *cc14XIDatabases) { r.Content = nil }, "binary": func(_ *p3ProducerReady, i *cc14XIIdentity, _ *cc14XIDatabases) { i.Binary = strings.Repeat("f", 64) }, "base-is-clone": func(_ *p3ProducerReady, i *cc14XIIdentity, _ *cc14XIDatabases) { i.Database = i.Base }, "cluster": func(_ *p3ProducerReady, i *cc14XIIdentity, _ *cc14XIDatabases) { i.System = "999" }, "old-database-changed": func(_ *p3ProducerReady, _ *cc14XIIdentity, d *cc14XIDatabases) { d.Unchanged = false }, "wrong-clone-oid": func(_ *p3ProducerReady, _ *cc14XIIdentity, d *cc14XIDatabases) { d.Created[d.Clone] = "999" }} {
		t.Run(name, func(t *testing.T) {
			r, i, d, _ := cc14ReceiptFixture()
			mutate(&r, &i, &d)
			if cc14ValidateProducer(r, i, d, get) {
				t.Fatal("unbound producer accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*cc14XIRequests){"single": func(v *cc14XIRequests) { v.Count = 1 }, "duplicate-request": func(v *cc14XIRequests) { v.Requests[1].RequestID = v.Requests[0].RequestID }, "wrong-round": func(v *cc14XIRequests) { v.Requests[0].Round = 2 }, "extra-call": func(v *cc14XIRequests) { v.Calls = 3 }, "stale": func(v *cc14XIRequests) { v.Requests[0].Stale = true }, "failed-status": func(v *cc14XIRequests) { v.Requests[1].Status = 500 }, "missing-points": func(v *cc14XIRequests) { v.Points = 7 }, "other-source": func(v *cc14XIRequests) { v.Source.ProjectID = "202" }, "reversed-time": func(v *cc14XIRequests) { v.Requests[1].Started = v.Requests[0].Started.Add(-time.Second) }} {
		t.Run(name, func(t *testing.T) {
			_, _, _, v := cc14ReceiptFixture()
			mutate(&v)
			if cc14ValidateRequests(v, ready) {
				t.Fatal("incomplete two-round receipt accepted")
			}
		})
	}
	var terminal cc14XITerminal
	raw := []byte(`{"round":1,"request_id":"real-1","content_points":4,"runtime_profiles":["unirouter.runtime-context.v1","unirouter.attempt-evidence.v1","unirouter.capability-decision.v1","unirouter.policy-decision.v1","unirouter.destination-evidence.v1"],"observation":{"request_id":"real-1","observed_at":"2026-09-22T00:00:02Z","query_ok":true,"counts":{"settled":1,"economic_records":1,"context_terminal_records":1,"identity_terminal_records":1,"cost_terminal_records":1,"all_profiles":8}}}`)
	if cc14Decode(raw, &terminal) != nil {
		t.Fatal("terminal shape")
	}
	terminal.Profiles = []string{sourcefacts.RuntimeProfile, sourcefacts.AttemptProfile, sourcefacts.CapabilityProfile, sourcefacts.PolicyProfile, sourcefacts.DestinationProfile}
	if !cc14ValidateTerminal(terminal, requests.Requests[0]) {
		t.Fatal("terminal rejected")
	}
	terminal.Observation.Counts.Settled = 0
	if cc14ValidateTerminal(terminal, requests.Requests[0]) {
		t.Fatal("unsettled accepted")
	}
	terminal.Observation.Counts.Settled = 1
	terminal.Profiles[1] = terminal.Profiles[0]
	if cc14ValidateTerminal(terminal, requests.Requests[0]) {
		t.Fatal("duplicate runtime profile accepted")
	}
	for _, raw := range []string{`{"run":"one","run":"two"}`, `{"unknown":true}`, `{} {}`} {
		var v cc14XIRequests
		if cc14Decode([]byte(raw), &v) == nil {
			t.Fatal("ambiguous receipt accepted")
		}
	}
}
func TestCC14XIClientEvidencePointerPure(t *testing.T) {
	round1 := `{"messages":[{"role":"system","content":"synthetic"},{"role":"user","content":"Read sample.txt"}]}`
	round2 := `{"messages":[{"role":"user","content":"Read sample.txt"},{"role":"assistant","tool_calls":[{"id":"callcc14readsample","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"callcc14readsample","content":"client-reported sample content"}]}`
	if p, e := cc14ReviewPointer([]byte(round1), 1); e != nil || p != "/messages/1/content" {
		t.Fatal("round1 location", e)
	}
	if p, e := cc14ReviewPointer([]byte(round2), 2); e != nil || p != "/messages/2/content" {
		t.Fatal("round2 location", e)
	}
	for _, tc := range []struct {
		body  string
		round int
	}{{round1, 2}, {round2, 1}, {round2, 3}, {strings.ReplaceAll(round2, "callcc14readsample", "other-call"), 2}, {strings.Replace(round2, "callcc14readsample", "call_cc14_read_sample", 1), 2}, {strings.Replace(round2, `"tool_call_id":"callcc14readsample"`, `"tool_call_id":"call_cc14_read_sample"`, 1), 2}, {strings.ReplaceAll(round2, "callcc14readsample", "call_cc14_read_sample"), 2}, {strings.ReplaceAll(round2, `"role":"tool"`, `"role":"assistant"`), 2}, {`{"messages":[],"messages":[]}`, 1}} {
		if _, err := cc14ReviewPointer([]byte(tc.body), tc.round); err == nil {
			t.Fatal("invented tool sequence accepted")
		}
	}
}

// Diagnostics belong in a separate receipt, never in the strict producer identity.
func TestCC14XIIdentityDiagnosticsRejectedPure(t *testing.T) {
	_, identity, _, _ := cc14ReceiptFixture()
	raw, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	var decoded cc14XIIdentity
	if cc14Decode(raw, &decoded) != nil || !reflect.DeepEqual(decoded, identity) {
		t.Fatal("exact identity rejected")
	}
	for name, extra := range map[string]map[string]any{
		"policy-id":  {"guardrail_policy_id": 1},
		"policy-ref": {"guardrail_policy_ref": "guardrail:1"},
		"both":       {"guardrail_policy_id": 1, "guardrail_policy_ref": "guardrail:1"},
	} {
		t.Run(name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			for key, item := range extra {
				value[key] = item
			}
			changed, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var got cc14XIIdentity
			if cc14Decode(changed, &got) == nil {
				t.Fatal("identity accepted extra diagnostics")
			}
		})
	}
}

func TestCC14XIPointReceiptGuardPure(t *testing.T) {
	valid := `{"manifest":{},"source_sha256":"sha256:fixture","legacy_body_denied":true}`
	point, err := cc14DecodePoint([]byte(valid))
	if err != nil || !point.LegacyBodyDenied {
		t.Fatal("explicit legacy body denial rejected", err)
	}
	for name, raw := range map[string]string{
		"false":      strings.Replace(valid, "true", "false", 1),
		"missing":    strings.Replace(valid, `,"legacy_body_denied":true`, "", 1),
		"null":       strings.Replace(valid, "true", "null", 1),
		"wrong-type": strings.Replace(valid, "true", `"true"`, 1),
		"extra":      strings.TrimSuffix(valid, "}") + `,"extra":true}`,
		"duplicate":  strings.TrimSuffix(valid, "}") + `,"legacy_body_denied":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := cc14DecodePoint([]byte(raw)); err == nil {
				t.Fatal("invalid point receipt accepted")
			}
		})
	}
}
