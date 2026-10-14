package hostctl

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

type interruptedSourceRefresh struct {
	delegate CommandRunner
	fail     bool
	restarts int
}

func (r *interruptedSourceRefresh) Run(ctx context.Context, c Command) (CommandResult, error) {
	if reflect.DeepEqual(c.Args, []string{"restart", sourceFactsWorkerUnit}) {
		r.restarts++
		if r.fail {
			r.fail = false
			return CommandResult{}, errors.New("fixture interrupted worker refresh")
		}
	}
	return r.delegate.Run(ctx, c)
}

func TestManagedProjectsRefreshRetriesPublishedFilesAndValidatesWholeBatch(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	e.Runner = &fakeRunner{}
	ctx := context.Background()
	id := "xci_AAAAAAAAAAAAAAAAAAAAAA"
	if err := e.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatal(err)
	}
	putManagedFile(t, e.Paths.TrustedTLSProxyEnv, []byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xinsight.example\n"), 0600)
	if err := e.SetConnectorTargetOrigin("https://198.18.0.2:9444"); err != nil {
		t.Fatal(err)
	}
	machine := URMachineConfig{InstallationID: id, SourceInstance: "fixture-source", OrgID: "7", EnrollFile: filepath.Join(e.Paths.EtcDir, "ur-enroll.key")}
	putManagedFile(t, machine.EnrollFile, []byte("ur-xse1-"+strings.Repeat("b", 64)), 0600)
	if err := e.SetURMachineConfig(machine, false); err != nil {
		t.Fatal(err)
	}
	originalEgress := filepath.Join(e.Paths.SourceFactsServiceUnit+".d", "20-original-egress.conf")
	putManagedFile(t, originalEgress, []byte("[Service]\nIPAddressAllow=203.0.113.7/32\n"), 0644)
	prepared, err := e.PrepareSourceCredential(ctx, id, "xise_1", "facts", "fixture-source", "7", "9", []string{sourcefacts.CostProfile})
	if err != nil {
		t.Fatal(err)
	}
	other, err := e.PrepareSourceCredential(ctx, id, "xise_1", "facts", "fixture-source", "7", "10", []string{sourcefacts.CostProfile})
	if err != nil || other.KeyFile == prepared.KeyFile || other.Registration.OperationID == prepared.Registration.OperationID {
		t.Fatal("same grant reused project custody or operation", err)
	}
	replay, err := e.PrepareSourceCredential(ctx, id, "xise_1", "facts", "fixture-source", "7", "9", []string{sourcefacts.CostProfile})
	if err != nil || !reflect.DeepEqual(replay, prepared) {
		t.Fatal("original project preparation changed", err)
	}
	cfg := sourcefacts.ManagedConfig{SchemaVersion: sourcefacts.ManagedConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 30, Profiles: []sourcefacts.ManagedProfileConfig{{StreamID: ManagedCollectionID(id, "xise_1", "9", "facts"), SourceSystemID: unirouter.DefaultSourceSystemID, SourceInstance: "fixture-source", OrgID: "7", ProjectID: "9", Profile: sourcefacts.CostProfile, ScopeGeneration: "1", Endpoint: "https://source.example/api/v1/integrations/xinsight/source-facts", KeyFile: prepared.KeyFile}}}
	name := id + "-xise_1-project-9.json"
	before := snapshotManagedFiles(t, e.Paths.Root)
	if err := e.PublishManagedProjects(ctx, map[string]sourcefacts.ManagedConfig{name: cfg, id + "-xise_2-project-9.json": {}}); err == nil {
		t.Fatal("invalid later candidate accepted")
	}
	if string(before) != string(snapshotManagedFiles(t, e.Paths.Root)) {
		t.Fatal("batch validation published an earlier candidate")
	}
	r := &interruptedSourceRefresh{delegate: e.Runner, fail: true}
	e.Runner = r
	if err := e.PublishManagedProjects(ctx, map[string]sourcefacts.ManagedConfig{name: cfg}); err == nil {
		t.Fatal("interrupted refresh succeeded")
	}
	path := filepath.Join(sourcefacts.ManagedPartitionsDirectory(e.Paths.SourceFactsConfig), name)
	published, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.PublishManagedProjects(ctx, map[string]sourcefacts.ManagedConfig{name: cfg}); err != nil {
		t.Fatal("equal-file recovery", err)
	}
	if r.restarts != 2 {
		t.Fatalf("restarts=%d", r.restarts)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(published) {
		t.Fatal("recovery changed candidate", err)
	}
	state, err := e.inspectManagedWorkerState(ctx, sourceFactsWorkerUnit, e.Paths.SourceFactsServiceUnit, "source worker")
	if err != nil || !state.present || !state.active || !state.enabled {
		t.Fatal("worker not refreshed", err)
	}
	dropin, err := os.ReadFile(filepath.Join(e.Paths.SourceFactsServiceUnit+".d", "21-managed-source-credentials.conf"))
	if err != nil || strings.Count(string(dropin), "IPAddressAllow=") != 1 || !strings.Contains(string(dropin), "IPAddressAllow=198.18.0.2/32\n") {
		t.Fatal("managed consumer did not receive the exact accepted UR destination", err)
	}
	if prior, err := os.ReadFile(originalEgress); err != nil || string(prior) != "[Service]\nIPAddressAllow=203.0.113.7/32\n" {
		t.Fatal("managed publication changed original egress", err)
	}
}

func TestOriginalConfigInspectionDoesNotStartWorkerAndExplicitPreparationRechecksConfig(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	r := &fakeRunner{sourceFactsWorkerLoaded: true}
	e.Runner = r
	prepared, err := e.PrepareSourceCredential(t.Context(), "xci_AAAAAAAAAAAAAAAAAAAAAA", "xise_1", "facts", "original-source", "2", "86", []string{sourcefacts.CostProfile})
	if err != nil {
		t.Fatal(err)
	}
	cfg := sourcefacts.ManagedConfig{SchemaVersion: sourcefacts.ManagedConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 30, Profiles: []sourcefacts.ManagedProfileConfig{{StreamID: ManagedCollectionID("xci_AAAAAAAAAAAAAAAAAAAAAA", "xise_1", "86", "facts"), SourceSystemID: unirouter.DefaultSourceSystemID, SourceInstance: "original-source", OrgID: "2", ProjectID: "86", Profile: sourcefacts.CostProfile, ScopeGeneration: "1", Endpoint: "https://source.example/api/v1/integrations/xinsight/source-facts", KeyFile: prepared.KeyFile}}}
	raw, _ := json.Marshal(cfg)
	putManagedFile(t, e.Paths.SourceFactsConfig, raw, 0644)
	before := snapshotManagedFiles(t, e.Paths.Root)
	inspected, present, err := e.InspectOriginalSourceConfig(t.Context())
	if err != nil || !present || !reflect.DeepEqual(cfg, inspected) {
		t.Fatal("paused original config unavailable", err)
	}
	if !reflect.DeepEqual(before, snapshotManagedFiles(t, e.Paths.Root)) || len(r.commands) != 0 {
		t.Fatal("read-only inspection had effects")
	}
	if _, _, err := e.ReadOriginalSourceConfig(t.Context()); err == nil {
		t.Fatal("receiving read accepted inactive consumer")
	}
	bad := cfg
	bad.PollIntervalSeconds++
	commandCount := len(r.commands)
	if err := e.EnsureOriginalSourceWorker(t.Context(), bad); !errors.Is(err, sourcefacts.ErrScopeChanged) {
		t.Fatal("config drift accepted", err)
	}
	if len(r.commands) != commandCount || !reflect.DeepEqual(before, snapshotManagedFiles(t, e.Paths.Root)) {
		t.Fatal("rejected config changed worker/files")
	}
	if err := e.EnsureOriginalSourceWorker(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if !r.sourceFactsWorkerActive || !r.sourceFactsWorkerEnabled {
		t.Fatal("original consumer not started")
	}
	starts := 0
	for _, c := range r.commands {
		if reflect.DeepEqual(c.Args, []string{"start", sourceFactsWorkerUnit}) {
			starts++
		}
	}
	if err := e.EnsureOriginalSourceWorker(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	afterStarts := 0
	for _, c := range r.commands {
		if reflect.DeepEqual(c.Args, []string{"start", sourceFactsWorkerUnit}) {
			afterStarts++
		}
	}
	if starts != 1 || afterStarts != 1 {
		t.Fatal("active replay started consumer again")
	}
	credentialDropIn := e.Paths.SourceFactsServiceUnit + ".d/10-source-credentials.conf"
	oldInfo, err := os.Lstat(credentialDropIn)
	if err != nil {
		t.Fatal(err)
	}
	r.sourceFactsWorkerActive = false
	r.sourceFactsWorkerEnabled = false
	if err := e.EnsureOriginalSourceWorker(t.Context(), cfg); err != nil {
		t.Fatal("resume original paused worker", err)
	}
	newInfo, err := os.Lstat(credentialDropIn)
	if err != nil || !os.SameFile(oldInfo, newInfo) {
		t.Fatal("unchanged original credential drop-in was replaced", err)
	}
	current, err := os.ReadFile(e.Paths.SourceFactsConfig)
	if err != nil || string(current) != string(raw) {
		t.Fatal("original indexed config replaced", err)
	}
}
