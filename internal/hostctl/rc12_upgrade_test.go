package hostctl

import (
	"context"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
)

const testLiteLLMWorkerUnit = "[Unit]\nDescription=rc.12 LiteLLM worker\n[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer litellm pull-spend-logs\n"

func TestRC12FreshInstallStagesLiteLLMWorkerDormant(t *testing.T) {
	fixture := newInstallFixture(t)
	fixture.artifact = packVersionedHostRelease(
		t,
		fixture,
		"rc12-fresh",
		"0.2.0-rc.12",
		"0.2.0-rc.12/manual-r12",
		"1212121212121212121212121212121212121212",
		nil,
		"[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed\n",
		withLiteLLMUnit(testLiteLLMWorkerUnit),
	)
	target, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify rc.12 target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: target.Identity},
	}
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install rc.12: %v", err)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	assertFileContents(
		t,
		fixture.paths.LiteLLMServiceUnit,
		[]byte(testLiteLLMWorkerUnit),
	)
	if !runner.liteLLMWorkerLoaded ||
		runner.liteLLMWorkerActive ||
		runner.liteLLMWorkerEnabled {
		t.Fatalf(
			"fresh LiteLLM worker loaded=%t active=%t enabled=%t",
			runner.liteLLMWorkerLoaded,
			runner.liteLLMWorkerActive,
			runner.liteLLMWorkerEnabled,
		)
	}
	if exists, err := fileExists(fixture.paths.LiteLLMEnvironment); err != nil || exists {
		t.Fatalf("fresh LiteLLM environment exists=%t err=%v", exists, err)
	}
	if exists, err := fileExists(fixture.paths.LiteLLMCredential); err != nil || exists {
		t.Fatalf("fresh LiteLLM credential exists=%t err=%v", exists, err)
	}
	if containsCommand(
		runner.commands,
		"systemctl",
		"start",
		liteLLMWorkerUnitName,
	) ||
		containsCommand(
			runner.commands,
			"systemctl",
			"restart",
			liteLLMWorkerUnitName,
		) ||
		containsCommand(
			runner.commands,
			"systemctl",
			"enable",
			liteLLMWorkerUnitName,
		) {
		t.Fatalf("fresh install started or enabled LiteLLM worker: %+v", runner.commands)
	}
}

func TestRC12UpgradeFromRC11AdvancesSchema13To14AndStagesWorkerDormant(
	t *testing.T,
) {
	fixture := newInstallFixture(t)
	feedUnit := "[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed\n"
	fixture.artifact = packVersionedHostRelease(
		t,
		fixture,
		"rc11-current-for-schema14",
		"0.2.0-rc.11",
		"0.2.0-rc.11/manual-r11",
		"1111111111111111111111111111111111111111",
		nil,
		feedUnit,
	)
	current, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify rc.11 current: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: current.Identity},
	}
	if _, err := fixture.engine.Install(
		context.Background(),
		fixture.artifact,
	); err != nil {
		t.Fatalf("install rc.11: %v", err)
	}
	rc12 := packVersionedHostRelease(
		t,
		fixture,
		"rc12-schema14-target",
		"0.2.0-rc.12",
		"0.2.0-rc.12/manual-r12",
		"1212121212121212121212121212121212121212",
		[]string{"0.2.0-rc.11"},
		feedUnit,
		withLiteLLMUnit(testLiteLLMWorkerUnit),
	)
	target, err := fixture.engine.verifyTarget(rc12)
	if err != nil {
		t.Fatalf("verify rc.12 target: %v", err)
	}
	if target.Identity.DatabaseSchema != "14" {
		t.Fatalf("rc.12 target schema=%s", target.Identity.DatabaseSchema)
	}
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: target.Identity},
	}
	if _, err := fixture.engine.Upgrade(context.Background(), rc12); err != nil {
		t.Fatalf("upgrade rc.11 to rc.12: %v", err)
	}
	state, err := fixture.engine.readInstallState()
	if err != nil ||
		state.Current.ProductVersion != "0.2.0-rc.12" ||
		state.DatabaseSchemaCurrent != "14" ||
		state.Previous == nil ||
		state.Previous.ProductVersion != "0.2.0-rc.11" ||
		len(fixture.database.facts.MigrationLedger) != 14 {
		t.Fatalf(
			"rc.12 schema14 state=%+v migrations=%d err=%v",
			state,
			len(fixture.database.facts.MigrationLedger),
			err,
		)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	if !runner.liteLLMWorkerLoaded ||
		runner.liteLLMWorkerActive ||
		runner.liteLLMWorkerEnabled {
		t.Fatalf(
			"upgraded LiteLLM worker loaded=%t active=%t enabled=%t",
			runner.liteLLMWorkerLoaded,
			runner.liteLLMWorkerActive,
			runner.liteLLMWorkerEnabled,
		)
	}
}

func TestRC12RollbackToRC11RemovesUnsupportedLiteLLMUnitAndPreservesIndependentStates(
	t *testing.T,
) {
	fixture := newInstallFixture(t)
	feedUnit := "[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed\n"
	fixture.artifact = packVersionedHostRelease(
		t,
		fixture,
		"rc11-current-for-rc12",
		"0.2.0-rc.11",
		"0.2.0-rc.11/manual-r11",
		"1111111111111111111111111111111111111111",
		nil,
		feedUnit,
	)
	current, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify rc.11 current: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: current.Identity},
	}
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install rc.11: %v", err)
	}

	liteEnvironment := []byte(
		"XINSIGHT_LITELLM_FEED_ENDPOINT=https://private.example.invalid/spend/logs/v2\n" +
			"XINSIGHT_LITELLM_FEED_POLL_INTERVAL=30s\n" +
			"XINSIGHT_LITELLM_FEED_SOURCE_INSTANCE=litellm-development\n" +
			"XINSIGHT_LITELLM_FEED_TENANT_REF=\n" +
			"XINSIGHT_LITELLM_FEED_ORG_REF=1\n" +
			"XINSIGHT_LITELLM_FEED_PROJECT_REF=2\n",
	)
	liteCredential := []byte("sk-" + strings.Repeat("1", 32))
	writeHostFile(t, fixture.paths.LiteLLMEnvironment, liteEnvironment, 0o600)
	writeHostFile(t, fixture.paths.LiteLLMCredential, liteCredential, 0o600)
	fixture.database.durableCursor = "litellm-scope-bound-cursor"

	rc12 := packVersionedHostRelease(
		t,
		fixture,
		"rc12-litellm-target",
		"0.2.0-rc.12",
		"0.2.0-rc.12/manual-r12",
		"1212121212121212121212121212121212121212",
		[]string{"0.2.0-rc.11"},
		feedUnit,
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withApplicationRollbackCompatible(),
		withDatabaseSchema(13),
	)
	target, err := fixture.engine.verifyTarget(rc12)
	if err != nil {
		t.Fatalf("verify rc.12 target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: target.Identity},
	}
	if _, err := fixture.engine.Upgrade(context.Background(), rc12); err != nil {
		t.Fatalf("upgrade rc.11 to rc.12: %v", err)
	}
	assertFileContents(
		t,
		fixture.paths.LiteLLMServiceUnit,
		[]byte(testLiteLLMWorkerUnit),
	)
	assertFileContents(t, fixture.paths.LiteLLMEnvironment, liteEnvironment)
	assertFileContents(t, fixture.paths.LiteLLMCredential, liteCredential)

	runner := fixture.engine.Runner.(*fakeRunner)
	runner.feedWorkerLoaded = true
	runner.feedWorkerActive = false
	runner.feedWorkerEnabled = true
	runner.liteLLMWorkerLoaded = true
	runner.liteLLMWorkerActive = true
	runner.liteLLMWorkerEnabled = false
	runner.commands = nil
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: current.Identity},
	}

	if _, err := fixture.engine.Rollback(
		context.Background(),
		"operator_request",
	); err != nil {
		t.Fatalf("rollback rc.12 to rc.11: %v", err)
	}
	state, err := fixture.engine.readInstallState()
	if err != nil ||
		state.Current.ProductVersion != "0.2.0-rc.11" ||
		state.Previous == nil ||
		state.Previous.ProductVersion != "0.2.0-rc.12" {
		t.Fatalf("rollback install state=%+v err=%v", state, err)
	}
	if _, err := os.Lstat(fixture.paths.LiteLLMServiceUnit); !os.IsNotExist(err) {
		t.Fatalf("unsupported LiteLLM unit remained after rollback: %v", err)
	}
	if runner.liteLLMWorkerLoaded ||
		runner.liteLLMWorkerActive ||
		runner.liteLLMWorkerEnabled {
		t.Fatalf(
			"unsupported LiteLLM state loaded=%t active=%t enabled=%t",
			runner.liteLLMWorkerLoaded,
			runner.liteLLMWorkerActive,
			runner.liteLLMWorkerEnabled,
		)
	}
	if runner.feedWorkerActive || !runner.feedWorkerEnabled {
		t.Fatalf(
			"UniRouter worker state changed active=%t enabled=%t",
			runner.feedWorkerActive,
			runner.feedWorkerEnabled,
		)
	}
	if !containsCommand(
		runner.commands,
		"systemctl",
		"stop",
		liteLLMWorkerUnitName,
	) ||
		!containsCommand(runner.commands, "systemctl", "daemon-reload") ||
		containsCommand(
			runner.commands,
			"systemctl",
			"restart",
			liteLLMWorkerUnitName,
		) ||
		containsCommand(
			runner.commands,
			"systemctl",
			"enable",
			liteLLMWorkerUnitName,
		) {
		t.Fatalf("LiteLLM rollback lifecycle commands=%+v", runner.commands)
	}
	assertFileContents(t, fixture.paths.LiteLLMEnvironment, liteEnvironment)
	assertFileContents(t, fixture.paths.LiteLLMCredential, liteCredential)
	if fixture.database.durableCursor != "litellm-scope-bound-cursor" {
		t.Fatalf("durable cursor changed: %q", fixture.database.durableCursor)
	}
}

func TestRC12RollbackDisablesEnabledLiteLLMUnitBeforeRemovingIt(t *testing.T) {
	fixture := newInstallFixture(t)
	feedUnit := "[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed\n"
	fixture.artifact = packVersionedHostRelease(
		t,
		fixture,
		"rc11-current-enabled-lite-rollback",
		"0.2.0-rc.11",
		"0.2.0-rc.11/manual-r11",
		"1111111111111111111111111111111111111111",
		nil,
		feedUnit,
	)
	current, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify rc.11 current: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: current.Identity},
	}
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install rc.11: %v", err)
	}

	liteEnvironment := []byte(
		"XINSIGHT_LITELLM_FEED_ENDPOINT=https://private.example.invalid/spend/logs/v2\n" +
			"XINSIGHT_LITELLM_FEED_POLL_INTERVAL=30s\n" +
			"XINSIGHT_LITELLM_FEED_SOURCE_INSTANCE=litellm-development\n" +
			"XINSIGHT_LITELLM_FEED_TENANT_REF=\n" +
			"XINSIGHT_LITELLM_FEED_ORG_REF=1\n" +
			"XINSIGHT_LITELLM_FEED_PROJECT_REF=2\n",
	)
	liteCredential := []byte("sk-" + strings.Repeat("2", 32))
	writeHostFile(t, fixture.paths.LiteLLMEnvironment, liteEnvironment, 0o600)
	writeHostFile(t, fixture.paths.LiteLLMCredential, liteCredential, 0o600)
	fixture.database.durableCursor = "litellm-enabled-downgrade-cursor"

	rc12 := packVersionedHostRelease(
		t,
		fixture,
		"rc12-enabled-lite-target",
		"0.2.0-rc.12",
		"0.2.0-rc.12/manual-r12",
		"1212121212121212121212121212121212121212",
		[]string{"0.2.0-rc.11"},
		feedUnit,
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withApplicationRollbackCompatible(),
		withDatabaseSchema(13),
	)
	target, err := fixture.engine.verifyTarget(rc12)
	if err != nil {
		t.Fatalf("verify rc.12 target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: target.Identity},
	}
	if _, err := fixture.engine.Upgrade(context.Background(), rc12); err != nil {
		t.Fatalf("upgrade rc.11 to rc.12: %v", err)
	}

	runner := fixture.engine.Runner.(*fakeRunner)
	runner.liteLLMWorkerLoaded = true
	runner.liteLLMWorkerActive = false
	runner.liteLLMWorkerEnabled = true
	runner.commands = nil
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: current.Identity},
	}
	if _, err := fixture.engine.Rollback(
		context.Background(),
		"operator_request",
	); err != nil {
		t.Fatalf("rollback rc.12 to rc.11: %v", err)
	}
	if runner.liteLLMWorkerLoaded ||
		runner.liteLLMWorkerActive ||
		runner.liteLLMWorkerEnabled {
		t.Fatalf(
			"unsupported LiteLLM state loaded=%t active=%t enabled=%t",
			runner.liteLLMWorkerLoaded,
			runner.liteLLMWorkerActive,
			runner.liteLLMWorkerEnabled,
		)
	}
	disableIndex := commandIndex(
		runner.commands,
		"systemctl",
		"disable",
		liteLLMWorkerUnitName,
	)
	reloadIndex := commandIndex(
		runner.commands,
		"systemctl",
		"daemon-reload",
	)
	if disableIndex < 0 || reloadIndex < 0 || disableIndex >= reloadIndex {
		t.Fatalf(
			"LiteLLM disable/reload ordering disable=%d reload=%d commands=%+v",
			disableIndex,
			reloadIndex,
			runner.commands,
		)
	}
	journal, err := fixture.engine.readActiveJournal()
	if err != nil {
		t.Fatalf("read rollback journal: %v", err)
	}
	assertPhaseOrder(
		t,
		journal.CompletedPhases,
		"capture_litellm_worker_state",
		"stop_litellm_worker",
		"disable_unsupported_litellm_worker",
		"install_litellm_worker_runtime",
		"restart_service",
		"restore_litellm_worker_state",
	)
	if _, err := os.Lstat(fixture.paths.LiteLLMServiceUnit); !os.IsNotExist(err) {
		t.Fatalf("unsupported LiteLLM unit remained after rollback: %v", err)
	}
	assertFileContents(t, fixture.paths.LiteLLMEnvironment, liteEnvironment)
	assertFileContents(t, fixture.paths.LiteLLMCredential, liteCredential)
	if fixture.database.durableCursor != "litellm-enabled-downgrade-cursor" {
		t.Fatalf("durable cursor changed: %q", fixture.database.durableCursor)
	}
}

func commandIndex(commands []Command, name string, args ...string) int {
	for index, command := range commands {
		if command.Name == name && slices.Equal(command.Args, args) {
			return index
		}
	}
	return -1
}
