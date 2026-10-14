package hostctl

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConnectorControllerUnitKeepsRootAndCredentialBoundary(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "deploy", connectorControllerUnitName))
	if err != nil {
		t.Fatalf("read connector controller unit: %v", err)
	}
	unit := string(contents)
	for _, required := range []string{
		"Before=xinsight.service",
		"User=root",
		"Group=xinsight",
		"LoadCredential=postgres-admin.dsn:/etc/x-insight/database/current/postgres-admin.dsn",
		"ExecStart=/opt/x-insight/current/bin/xinsightctl connector-controller serve",
		"RuntimeDirectory=x-insight",
		"RuntimeDirectoryMode=0750",
		"CapabilityBoundingSet=",
		"AmbientCapabilities=",
		"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
		"IPAddressDeny=any",
		"IPAddressAllow=100.64.0.0/10",
		"ReadWritePaths=/etc/x-insight /var/lib/x-insight /run/x-insight /etc/systemd/system/xinsight-connector-controller.service.d /etc/systemd/system/xinsight-source-facts.service.d /etc/systemd/system/xinsight.service.d",
	} {
		if !strings.Contains(unit, required) {
			t.Fatalf("connector controller unit is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"postgres-reader.dsn",
		"postgres-workspace.dsn",
		"postgres-importer.dsn",
		"unirouter-feed.key",
		"litellm-feed.key",
		"--socket",
		"--dsn",
		"Environment=XINSIGHT",
	} {
		if strings.Contains(unit, forbidden) {
			t.Fatalf("connector controller unit crosses boundary %q", forbidden)
		}
	}
}

func TestAbortRestoresControllerStoppedBeforeWebMaintenance(t *testing.T) {
	fixture := newInstallFixture(t)
	controllerUnit := string(mustReadTestFile(
		t,
		filepath.Join("..", "..", "deploy", connectorControllerUnitName),
	))
	fixture.artifact = packVersionedHostRelease(
		t,
		fixture,
		"connector-controller-abort-current",
		"0.2.0-rc.14",
		"0.2.0-rc.14/manual-r14",
		"1414141414141414141414141414141414141414",
		nil,
		"",
		withDatabaseSchema(1),
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withConnectorControllerUnit(controllerUnit),
	)
	current, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify controller current: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: current.Identity}}
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install controller current: %v", err)
	}
	targetArtifact := packVersionedHostRelease(
		t,
		fixture,
		"connector-controller-abort-target",
		"0.2.0-rc.15",
		"0.2.0-rc.15/manual-r15",
		"1515151515151515151515151515151515151515",
		[]string{"0.2.0-rc.14"},
		"",
		withDatabaseSchema(1),
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withConnectorControllerUnit(controllerUnit),
	)
	target, err := fixture.engine.verifyTarget(targetArtifact)
	if err != nil {
		t.Fatalf("verify controller target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: target.Identity}}
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "stop_connector_controller")
	if _, err := fixture.engine.Upgrade(context.Background(), targetArtifact); err == nil {
		t.Fatal("expected interruption after connector controller stop")
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	if runner.controllerActive || !runner.mainServiceActive {
		t.Fatalf(
			"interrupted state controller_active=%t main_active=%t",
			runner.controllerActive,
			runner.mainServiceActive,
		)
	}
	runner.commands = nil
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	receipt, err := fixture.engine.Abort(context.Background())
	if err != nil || receipt.Status != "aborted" {
		t.Fatalf("abort receipt=%+v err=%v", receipt, err)
	}
	if !runner.controllerActive || !runner.mainServiceActive {
		t.Fatalf(
			"abort state controller_active=%t main_active=%t",
			runner.controllerActive,
			runner.mainServiceActive,
		)
	}
	if got := commandIndex(runner.commands, "systemctl", "enable", "--now", connectorControllerUnitName); got < 0 {
		t.Fatalf("abort did not restore connector controller: %+v", runner.commands)
	}
	if got := commandIndex(runner.commands, "systemctl", "start", "xinsight.service"); got >= 0 {
		t.Fatalf("abort restarted web that maintenance never stopped: %+v", runner.commands)
	}
}

func TestManagedInstallStartsConnectorControllerBeforeWebAndReadsBackSocket(t *testing.T) {
	fixture := newInstallFixture(t)
	fixture.artifact = packVersionedHostRelease(
		t,
		fixture,
		"connector-controller-fresh",
		"0.2.0-rc.15",
		"0.2.0-rc.15/manual-r15",
		"1515151515151515151515151515151515151515",
		nil,
		"",
		withDatabaseSchema(1),
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withConnectorControllerUnit(string(mustReadTestFile(t, filepath.Join("..", "..", "deploy", connectorControllerUnitName)))),
	)
	target, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify controller target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: target.Identity}}
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install connector controller release: %v", err)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	controllerIndex := commandIndex(runner.commands, "systemctl", "enable", "--now", connectorControllerUnitName)
	webIndex := commandIndex(runner.commands, "systemctl", "enable", "--now", "xinsight.service")
	if controllerIndex < 0 || webIndex < 0 || controllerIndex >= webIndex {
		t.Fatalf("controller/web start order controller=%d web=%d commands=%+v", controllerIndex, webIndex, runner.commands)
	}
	if !runner.controllerLoaded || !runner.controllerActive || !runner.controllerEnabled {
		t.Fatalf("controller state loaded=%t active=%t enabled=%t", runner.controllerLoaded, runner.controllerActive, runner.controllerEnabled)
	}
	if err := validateConnectorControllerSocket(fixture.paths); err != nil {
		t.Fatalf("connector socket readback: %v", err)
	}
	journal, err := fixture.engine.readActiveJournal()
	if err != nil || !containsString(journal.CompletedPhases, "verify_connector_controller") {
		t.Fatalf("controller proof phase journal=%+v err=%v", journal, err)
	}
}

type delayedBackupControllerRunner struct{ base *fakeRunner }

func (r delayedBackupControllerRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	result, err := r.base.Run(ctx, command)
	if err == nil && command.Name == "systemctl" && len(command.Args) == 2 &&
		command.Args[0] == "start" && command.Args[1] == connectorControllerUnitName {
		// Type=simple reports start before the process has bound its socket.
		removeFakeControllerSocket()
	}
	return result, err
}

func TestBackupRestoreWaitsForControllerSocketWithoutEnablingUnit(t *testing.T) {
	for _, ready := range []bool{true, false} {
		t.Run(map[bool]string{true: "delayed_ready", false: "never_ready"}[ready], func(t *testing.T) {
			e, _ := managedCustodyFixture(t)
			runner := &fakeRunner{controllerLoaded: true, controllerEnabled: false}
			e.Runner = delayedBackupControllerRunner{base: runner}
			waits := 0
			e.Wait = func(context.Context, time.Duration) error {
				waits++
				if ready && waits == 2 {
					return createFakeControllerSocket()
				}
				return nil
			}
			journal := Journal{BackupRuntimeStateCaptured: true, BackupControllerWasActive: true}
			err := e.restoreBackupServices(t.Context(), &journal)
			if ready && err != nil {
				t.Fatal("delayed controller socket not accepted", err)
			}
			if !ready && err == nil {
				t.Fatal("controller without socket accepted")
			}
			if waits == 0 {
				t.Fatal("backup restoration did not wait for controller readiness")
			}
			if !runner.controllerActive || runner.controllerEnabled {
				t.Fatal("backup changed active-but-disabled controller state")
			}
			for _, command := range runner.commands {
				if command.Name == "systemctl" && len(command.Args) > 0 && command.Args[0] == "enable" {
					t.Fatal("backup enabled controller")
				}
			}
		})
	}
}

func TestRollbackStopsAndRemovesUnsupportedConnectorControllerBeforeWeb(t *testing.T) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install historical current: %v", err)
	}
	targetArtifact := packVersionedHostRelease(
		t,
		fixture,
		"connector-controller-upgrade",
		"0.2.0-rc.15",
		"0.2.0-rc.15/manual-r15",
		"1515151515151515151515151515151515151515",
		[]string{"0.2.0-rc.1"},
		"",
		withDatabaseSchema(1),
		withApplicationRollbackCompatible(),
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withConnectorControllerUnit(string(mustReadTestFile(t, filepath.Join("..", "..", "deploy", connectorControllerUnitName)))),
	)
	target, err := fixture.engine.verifyTarget(targetArtifact)
	if err != nil {
		t.Fatalf("verify controller upgrade: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: target.Identity}}
	if _, err := fixture.engine.Upgrade(context.Background(), targetArtifact); err != nil {
		t.Fatalf("upgrade to controller release: %v", err)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	runner.commands = nil
	current, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify rollback target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: current.Identity}}
	if _, err := fixture.engine.Rollback(context.Background(), "operator_request"); err != nil {
		t.Fatalf("rollback without controller support: %v", err)
	}
	stopIndex := commandIndex(runner.commands, "systemctl", "stop", connectorControllerUnitName)
	disableIndex := commandIndex(runner.commands, "systemctl", "disable", "--now", connectorControllerUnitName)
	webIndex := commandIndex(runner.commands, "systemctl", "restart", "xinsight.service")
	if stopIndex < 0 || disableIndex < 0 || webIndex < 0 || stopIndex >= disableIndex || disableIndex >= webIndex {
		t.Fatalf("rollback controller order stop=%d disable=%d web=%d commands=%+v", stopIndex, disableIndex, webIndex, runner.commands)
	}
	if runner.controllerLoaded || runner.controllerActive || runner.controllerEnabled {
		t.Fatalf("unsupported controller state loaded=%t active=%t enabled=%t", runner.controllerLoaded, runner.controllerActive, runner.controllerEnabled)
	}
	if _, err := os.Lstat(fixture.paths.ControllerServiceUnit); !os.IsNotExist(err) {
		t.Fatalf("unsupported controller unit remains: %v", err)
	}
	if _, err := os.Lstat(fixture.paths.ControllerSocket); !os.IsNotExist(err) {
		t.Fatalf("unsupported controller socket remains: %v", err)
	}
}

func mustReadTestFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}
