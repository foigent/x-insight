package hostctl

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRC10ToRC11TargetControllerRecoveryPreservesReferenceRuntimeState(t *testing.T) {
	fixture := newInstallFixture(t)
	fixture.artifact = packVersionedHostRelease(
		t,
		fixture,
		"rc10-current-for-rc11",
		"0.2.0-rc.10",
		"0.2.0-rc.10/manual-r10",
		"1010101010101010101010101010101010101010",
		nil,
		"",
	)
	current, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify rc.10 current: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: current.Identity},
	}
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install rc.10 current: %v", err)
	}

	feedEnvironment := []byte(
		"XINSIGHT_UNIROUTER_FEED_ENDPOINT=https://private.example.invalid/api/v1/integrations/xinsight/completed-executions\n" +
			"XINSIGHT_UNIROUTER_FEED_POLL_INTERVAL=5s\n" +
			"XINSIGHT_UNIROUTER_FEED_ALLOW_PRIVATE_TUNNEL_HTTP=false\n" +
			"XINSIGHT_UNIROUTER_FEED_MAPPING_MODE=shadow\n",
	)
	token := "ur-xf1-" + strings.Repeat("11", 32)
	registration := FeedCredentialRegistration{
		SchemaVersion:  FeedCredentialRegistrationSchema,
		TokenFormat:    FeedCredentialTokenFormat,
		VerifierScheme: FeedCredentialVerifierScheme,
		KeyPrefix:      token[:20],
		VerifierSHA256: feedCredentialVerifier(token),
		OperationID:    strings.Repeat("2", 32),
	}
	completedAt := fixture.now.Add(-time.Minute)
	credentialState := feedCredentialOperationState{
		SchemaVersion: FeedCredentialStateSchema,
		Active:        &registration,
		LastOperation: &feedCredentialTerminal{
			Status:       "activated",
			Registration: registration,
			CompletedAt:  completedAt,
		},
		UpdatedAt: completedAt,
	}
	writeHostFile(t, fixture.paths.FeedEnvironmentFile, feedEnvironment, 0o600)
	writeHostFile(t, fixture.paths.FeedCredentialFile, []byte(token), 0o600)
	writeHostJSON(t, fixture.paths.FeedCredentialState, credentialState, 0o600)
	credentialStateBytes, err := os.ReadFile(fixture.paths.FeedCredentialState)
	if err != nil {
		t.Fatalf("read active credential state: %v", err)
	}
	databaseSetBefore, err := readAbsoluteSymlink(fixture.paths.DatabaseCurrentLink)
	if err != nil {
		t.Fatalf("read database set before upgrade: %v", err)
	}
	fixture.database.durableCursor = "phase2c-reference-cursor-preserved-across-rc10-rc11"
	runner := fixture.engine.Runner.(*fakeRunner)
	runner.feedWorkerLoaded = true
	runner.feedWorkerActive = true
	runner.feedWorkerEnabled = false
	runner.commands = nil

	targetFeedUnit := "[Unit]\nDescription=rc.11 target payload feed worker\n[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed\n"
	rc11 := packVersionedHostRelease(
		t,
		fixture,
		"rc11-reference-runtime-target",
		"0.2.0-rc.11",
		"0.2.0-rc.11/manual-r11",
		"1111111111111111111111111111111111111111",
		[]string{"0.2.0-rc.10"},
		targetFeedUnit,
	)
	target, err := fixture.engine.verifyTarget(rc11)
	if err != nil {
		t.Fatalf("verify rc.11 target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: target.Identity},
	}
	check, err := fixture.engine.CheckUpgrade(context.Background(), rc11)
	if err != nil || !check.Supported ||
		check.Current == nil || check.Current.ProductVersion != "0.2.0-rc.10" ||
		check.Target == nil || check.Target.ProductVersion != "0.2.0-rc.11" {
		t.Fatalf("rc.10 to rc.11 preflight=%+v err=%v", check, err)
	}

	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "install_runtime_files")
	if _, err := fixture.engine.Upgrade(context.Background(), rc11); err == nil {
		t.Fatal("expected injected rc.11 upgrade interruption")
	}
	if runner.feedWorkerActive {
		t.Fatal("interrupted upgrade left feed worker active in maintenance")
	}
	journal, err := fixture.engine.readActiveJournal()
	if err != nil {
		t.Fatalf("read interrupted rc.11 journal: %v", err)
	}
	if !journal.FeedWorkerStateCaptured ||
		!journal.FeedWorkerWasPresent ||
		!journal.FeedWorkerWasActive ||
		journal.FeedWorkerWasEnabled {
		t.Fatalf("captured active+disabled feed worker state=%+v", journal)
	}

	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	receipt, err := fixture.engine.Resume(context.Background())
	if err != nil || receipt.Status != "completed" || receipt.Kind != "upgrade" {
		t.Fatalf("resume rc.11 upgrade receipt=%+v err=%v", receipt, err)
	}
	state, err := fixture.engine.readInstallState()
	if err != nil ||
		state.Current.ProductVersion != "0.2.0-rc.11" ||
		state.Current.Commit != "1111111111111111111111111111111111111111" ||
		state.Previous == nil ||
		state.Previous.ProductVersion != "0.2.0-rc.10" ||
		state.DatabaseSchemaCurrent != "13" {
		t.Fatalf("rc.11 install state=%+v err=%v", state, err)
	}
	assertFileContents(t, fixture.paths.FeedServiceUnit, []byte(targetFeedUnit))
	assertFileContents(t, fixture.paths.FeedEnvironmentFile, feedEnvironment)
	assertFileContents(t, fixture.paths.FeedCredentialFile, []byte(token))
	assertFileContents(t, fixture.paths.FeedCredentialState, credentialStateBytes)
	databaseSetAfter, err := readAbsoluteSymlink(fixture.paths.DatabaseCurrentLink)
	if err != nil || databaseSetAfter != databaseSetBefore {
		t.Fatalf(
			"database set after upgrade=%q before=%q err=%v",
			databaseSetAfter,
			databaseSetBefore,
			err,
		)
	}
	if fixture.database.durableCursor !=
		"phase2c-reference-cursor-preserved-across-rc10-rc11" {
		t.Fatalf("durable cursor changed: %q", fixture.database.durableCursor)
	}
	if !runner.feedWorkerActive || runner.feedWorkerEnabled {
		t.Fatalf(
			"feed worker state not independently restored active=%t enabled=%t",
			runner.feedWorkerActive,
			runner.feedWorkerEnabled,
		)
	}
	if !containsCommand(runner.commands, "systemctl", "stop", feedWorkerUnitName) ||
		!containsCommand(runner.commands, "systemctl", "restart", feedWorkerUnitName) ||
		containsCommand(runner.commands, "systemctl", "enable", feedWorkerUnitName) ||
		containsCommand(runner.commands, "systemctl", "enable", "--now", feedWorkerUnitName) {
		t.Fatalf("feed worker lifecycle commands=%+v", runner.commands)
	}
}
