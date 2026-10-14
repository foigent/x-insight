package hostctl

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestManagedFeedWorkerServiceKeepsWriterAndNetworkBoundaries(t *testing.T) {
	unitPath := filepath.Join("..", "..", "deploy", "xinsight-feed.service")
	contents, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read feed worker unit: %v", err)
	}
	unit := string(contents)
	for _, required := range []string{
		"User=xinsight",
		"Group=xinsight",
		"EnvironmentFile=/etc/x-insight/xinsight-feed.env",
		"LoadCredential=postgres-importer.dsn:/etc/x-insight/database/current/postgres-importer.dsn",
		"LoadCredential=unirouter-feed.key:/etc/x-insight/unirouter-feed.key",
		"ExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed",
		"TimeoutStopSec=60s",
		"KillSignal=SIGTERM",
		"CapabilityBoundingSet=",
		"IPAddressDeny=any",
		"IPAddressAllow=100.64.0.0/10",
		"ProtectSystem=strict",
	} {
		if !strings.Contains(unit, required) {
			t.Fatalf("feed worker unit is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"xinsight-server",
		"postgres-reader.dsn",
		"postgres-workspace.dsn",
		"XINSIGHT_READER_POSTGRES_DSN",
		"XINSIGHT_WORKSPACE_POSTGRES_DSN",
		"Environment=XINSIGHT_UNIROUTER_FEED",
	} {
		if strings.Contains(unit, forbidden) {
			t.Fatalf("feed worker unit contains forbidden boundary %q", forbidden)
		}
	}

	environmentPath := filepath.Join("..", "..", "deploy", "xinsight-feed.env.example")
	environment, err := os.ReadFile(environmentPath)
	if err != nil {
		t.Fatalf("read feed worker environment example: %v", err)
	}
	keys := make([]string, 0, 4)
	for _, line := range strings.Split(string(environment), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" || value == "" {
			t.Fatalf("invalid feed environment example line %q", line)
		}
		keys = append(keys, key)
	}
	expectedKeys := []string{
		"XINSIGHT_UNIROUTER_FEED_ENDPOINT",
		"XINSIGHT_UNIROUTER_FEED_POLL_INTERVAL",
		"XINSIGHT_UNIROUTER_FEED_ALLOW_PRIVATE_TUNNEL_HTTP",
		"XINSIGHT_UNIROUTER_FEED_MAPPING_MODE",
	}
	if !reflect.DeepEqual(keys, expectedKeys) {
		t.Fatalf("feed environment keys=%v expected=%v", keys, expectedKeys)
	}
}

func TestManagedInstallLeavesFeedWorkerDormant(t *testing.T) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install: %v", err)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	for _, command := range runner.commands {
		if command.Name != "systemctl" {
			continue
		}
		if reflect.DeepEqual(command.Args, []string{"start", feedWorkerUnitName}) ||
			reflect.DeepEqual(command.Args, []string{"restart", feedWorkerUnitName}) ||
			reflect.DeepEqual(command.Args, []string{"enable", "--now", feedWorkerUnitName}) {
			t.Fatalf("fresh install activated feed worker: %+v", command)
		}
	}
	if runner.feedWorkerActive || runner.feedWorkerEnabled {
		t.Fatalf("fresh install feed state active=%t enabled=%t", runner.feedWorkerActive, runner.feedWorkerEnabled)
	}
	info, err := os.Stat(fixture.paths.FeedServiceUnit)
	if err != nil {
		t.Fatalf("installed feed service unit: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("feed service mode=%o", info.Mode().Perm())
	}
}

func TestRuntimeInstallKeepsHistoricalReleaseWithoutFeedUnitCompatible(t *testing.T) {
	fixture := newInstallFixture(t)
	if err := fixture.engine.ensureControlDirectories(); err != nil {
		t.Fatalf("ensure control directories: %v", err)
	}
	releaseDir := filepath.Join(fixture.paths.Root, "historical-release")
	writeHostPayload(t, releaseDir)
	if err := os.Remove(filepath.Join(releaseDir, "deploy", "xinsight-feed.service")); err != nil {
		t.Fatalf("remove future feed unit from historical release: %v", err)
	}
	if err := fixture.engine.installRuntimeFiles(releaseDir, "historical-runtime-test", runtimeConfigInitialize); err != nil {
		t.Fatalf("install historical runtime files: %v", err)
	}
	if _, err := os.Stat(fixture.paths.FeedServiceUnit); !os.IsNotExist(err) {
		t.Fatalf("historical release unexpectedly installed feed unit: %v", err)
	}
}

func TestUpgradeDrainsActiveFeedWorkerAndRestoresItAfterImporterLogin(t *testing.T) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install prerequisite: %v", err)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	runner.feedWorkerLoaded = true
	runner.feedWorkerActive = true
	runner.feedWorkerEnabled = true
	runner.commands = nil
	fixture.database.loginTransitions = nil

	artifact := packUpgradeRelease(t, fixture)
	target, err := fixture.engine.verifyTarget(artifact)
	if err != nil {
		t.Fatalf("verify upgrade target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: target.Identity}}
	if _, err := fixture.engine.Upgrade(context.Background(), artifact); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	journal, err := fixture.engine.readActiveJournal()
	if err != nil {
		t.Fatalf("read upgrade journal: %v", err)
	}
	if !journal.FeedWorkerStateCaptured || !journal.FeedWorkerWasPresent || !journal.FeedWorkerWasActive || !journal.FeedWorkerWasEnabled {
		t.Fatalf("captured feed worker state=%+v", journal)
	}
	assertPhaseOrder(t, journal.CompletedPhases,
		"capture_feed_worker_state",
		"stop_feed_worker",
		"stop_service",
		"exclusive_maintenance",
		"enable_importer",
		"restore_feed_worker_state",
		"write_receipt",
	)
	if !runner.feedWorkerActive || !runner.feedWorkerEnabled {
		t.Fatalf("restored feed state active=%t enabled=%t", runner.feedWorkerActive, runner.feedWorkerEnabled)
	}
	if !containsCommand(runner.commands, "systemctl", "stop", feedWorkerUnitName) {
		t.Fatal("upgrade did not stop the feed worker")
	}
	if !containsCommand(runner.commands, "systemctl", "restart", feedWorkerUnitName) {
		t.Fatal("upgrade did not restart the previously active feed worker")
	}
	if len(fixture.database.loginTransitions) == 0 || !fixture.database.loginTransitions[len(fixture.database.loginTransitions)-1] {
		t.Fatalf("importer login transitions=%v", fixture.database.loginTransitions)
	}
}

func TestAbortRestoresFeedWorkerStoppedBeforeMaintenance(t *testing.T) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install prerequisite: %v", err)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	runner.feedWorkerLoaded = true
	runner.feedWorkerActive = true
	runner.feedWorkerEnabled = true
	artifact := packUpgradeRelease(t, fixture)

	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "stop_feed_worker")
	if _, err := fixture.engine.Upgrade(context.Background(), artifact); err == nil {
		t.Fatal("expected injected upgrade failure")
	}
	if runner.feedWorkerActive {
		t.Fatal("feed worker remained active after stop phase")
	}
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	if _, err := fixture.engine.Abort(context.Background()); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if !runner.feedWorkerActive || !runner.feedWorkerEnabled {
		t.Fatalf("abort did not restore feed worker active=%t enabled=%t", runner.feedWorkerActive, runner.feedWorkerEnabled)
	}
}

func TestRollbackTargetWithoutFeedContractIsDisabledInsteadOfRestarted(t *testing.T) {
	fixture := newInstallFixture(t)
	if err := fixture.engine.ensureControlDirectories(); err != nil {
		t.Fatalf("ensure control directories: %v", err)
	}
	journal, err := fixture.engine.startJournal("test-feed-rollback", "", "", nil, nil)
	if err != nil {
		t.Fatalf("start journal: %v", err)
	}
	journal.FeedWorkerStateCaptured = true
	journal.FeedWorkerWasPresent = true
	journal.FeedWorkerWasActive = true
	journal.FeedWorkerWasEnabled = true
	if err := fixture.engine.updateJournal(journal); err != nil {
		t.Fatalf("persist feed state: %v", err)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	runner.feedWorkerLoaded = true
	runner.feedWorkerActive = false
	runner.feedWorkerEnabled = true

	legacyRelease := filepath.Join(fixture.paths.Root, "legacy-release-without-feed")
	if err := os.MkdirAll(filepath.Join(legacyRelease, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyRelease, "bin", "xinsight-importer"), []byte("legacy\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fixture.engine.restartFeedWorkerAfterMaintenance(context.Background(), journal, legacyRelease); err != nil {
		t.Fatalf("restore unsupported feed worker state: %v", err)
	}
	if runner.feedWorkerActive || runner.feedWorkerEnabled {
		t.Fatalf("unsupported rollback target feed state active=%t enabled=%t", runner.feedWorkerActive, runner.feedWorkerEnabled)
	}
	if !containsCommand(runner.commands, "systemctl", "disable", feedWorkerUnitName) {
		t.Fatal("unsupported rollback target did not disable the feed worker")
	}
	if containsCommand(runner.commands, "systemctl", "restart", feedWorkerUnitName) {
		t.Fatal("unsupported rollback target restarted the feed worker")
	}
}

func assertPhaseOrder(t *testing.T, completed []string, expected ...string) {
	t.Helper()
	last := -1
	for _, phase := range expected {
		index := -1
		for candidateIndex, candidate := range completed {
			if candidate == phase {
				index = candidateIndex
				break
			}
		}
		if index <= last {
			t.Fatalf("phase %q index=%d after=%d completed=%v", phase, index, last, completed)
		}
		last = index
	}
}

func containsCommand(commands []Command, name string, args ...string) bool {
	for _, command := range commands {
		if command.Name == name && reflect.DeepEqual(command.Args, args) {
			return true
		}
	}
	return false
}
