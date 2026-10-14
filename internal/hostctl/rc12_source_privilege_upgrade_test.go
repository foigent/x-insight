package hostctl

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/db"
)

func TestRC12UpgradePreflightAcceptsExactRC10SourcePrivilegePolicyWithoutMutation(t *testing.T) {
	fixture, targetArtifact := newRC10SourcePrivilegeUpgradeFixture(t)
	runner := fixture.engine.Runner.(*fakeRunner)
	journalBefore, err := fixture.engine.readActiveJournal()
	if err != nil {
		t.Fatal(err)
	}
	commandsBefore := append([]Command(nil), runner.commands...)
	migrationsBefore := append([]db.MigrationRecord(nil), fixture.database.facts.MigrationLedger...)
	backupCountBefore := fixture.database.backupCount
	applyGrantsBefore := fixture.database.applyGrantsCount
	serviceActiveBefore := runner.mainServiceActive
	fixture.database.roleBoundaryChecks = 0
	fixture.database.roleBoundaryErr = errors.New("rc.12 target policy rejects legal rc.10 table UPDATE grants")

	report, err := fixture.engine.CheckUpgrade(context.Background(), targetArtifact)
	if err != nil || !report.Supported ||
		report.Current == nil || report.Current.ProductVersion != "0.3.0-rc.10" ||
		report.Target == nil || report.Target.ProductVersion != "0.3.0-rc.12" {
		t.Fatalf("rc.10 source-policy preflight=%+v err=%v", report, err)
	}
	if fixture.database.sourceRoleBoundaryChecks != 1 || fixture.database.roleBoundaryChecks != 0 ||
		!slices.Equal(fixture.database.sourceRoleVersions, []string{"0.3.0-rc.10"}) {
		t.Fatalf(
			"preflight source checks=%d versions=%v target checks=%d",
			fixture.database.sourceRoleBoundaryChecks,
			fixture.database.sourceRoleVersions,
			fixture.database.roleBoundaryChecks,
		)
	}

	journalAfter, journalErr := fixture.engine.readActiveJournal()
	if journalErr != nil || !reflect.DeepEqual(journalAfter, journalBefore) ||
		!reflect.DeepEqual(fixture.database.facts.MigrationLedger, migrationsBefore) ||
		fixture.database.backupCount != backupCountBefore ||
		fixture.database.applyGrantsCount != applyGrantsBefore ||
		runner.mainServiceActive != serviceActiveBefore {
		t.Fatalf(
			"check mutated state journal_equal=%t migrations_equal=%t backup=%d/%d grants=%d/%d service=%t/%t journal_err=%v",
			reflect.DeepEqual(journalAfter, journalBefore),
			reflect.DeepEqual(fixture.database.facts.MigrationLedger, migrationsBefore),
			fixture.database.backupCount,
			backupCountBefore,
			fixture.database.applyGrantsCount,
			applyGrantsBefore,
			runner.mainServiceActive,
			serviceActiveBefore,
			journalErr,
		)
	}
	assertNoMutatingHostCommands(t, runner.commands[len(commandsBefore):])
}

func TestRC12UpgradePreflightRejectsRC10SourcePrivilegeDriftWithoutMutation(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  string
	}{
		{name: "extra_grant", err: "unexpected DELETE grant"},
		{name: "overwide_grant", err: "unexpected table UPDATE grant"},
		{name: "unknown_grant", err: "unknown object privilege"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture, targetArtifact := newRC10SourcePrivilegeUpgradeFixture(t)
			runner := fixture.engine.Runner.(*fakeRunner)
			journalBefore, err := fixture.engine.readActiveJournal()
			if err != nil {
				t.Fatal(err)
			}
			commandsBefore := append([]Command(nil), runner.commands...)
			migrationsBefore := append([]db.MigrationRecord(nil), fixture.database.facts.MigrationLedger...)
			backupCountBefore := fixture.database.backupCount
			applyGrantsBefore := fixture.database.applyGrantsCount
			serviceActiveBefore := runner.mainServiceActive
			fixture.database.roleBoundaryChecks = 0
			fixture.database.sourceRoleBoundaryErr = errors.New(testCase.err)

			if _, err := fixture.engine.CheckUpgrade(context.Background(), targetArtifact); err == nil ||
				!strings.Contains(err.Error(), testCase.err) {
				t.Fatalf("source privilege drift error=%v", err)
			}
			if fixture.database.sourceRoleBoundaryChecks != 1 || fixture.database.roleBoundaryChecks != 0 {
				t.Fatalf(
					"rejected preflight source checks=%d target checks=%d",
					fixture.database.sourceRoleBoundaryChecks,
					fixture.database.roleBoundaryChecks,
				)
			}

			journalAfter, journalErr := fixture.engine.readActiveJournal()
			if journalErr != nil || !reflect.DeepEqual(journalAfter, journalBefore) ||
				!reflect.DeepEqual(fixture.database.facts.MigrationLedger, migrationsBefore) ||
				fixture.database.backupCount != backupCountBefore ||
				fixture.database.applyGrantsCount != applyGrantsBefore ||
				runner.mainServiceActive != serviceActiveBefore {
				t.Fatalf("rejected check mutated state journal_err=%v", journalErr)
			}
			assertNoMutatingHostCommands(t, runner.commands[len(commandsBefore):])
		})
	}
}

func assertNoMutatingHostCommands(t *testing.T, commands []Command) {
	t.Helper()
	for _, command := range commands {
		if command.Name != "systemctl" || len(command.Args) == 0 {
			continue
		}
		switch command.Args[0] {
		case "start", "stop", "restart", "enable", "disable", "daemon-reload":
			t.Fatalf("check mode issued mutating host command: %+v", command)
		}
	}
}

func TestRC12UpgradeConvergesRC10SourcePrivilegesInsideJournal(t *testing.T) {
	fixture, targetArtifact := newRC10SourcePrivilegeUpgradeFixture(t)
	fixture.database.roleBoundaryChecks = 0
	fixture.database.roleBoundaryErr = errors.New("rc.12 target policy rejects legal rc.10 table UPDATE grants")
	fixture.database.convergeRoleBoundary = true
	fixture.database.applyGrantsCount = 0

	receipt, err := fixture.engine.Upgrade(context.Background(), targetArtifact)
	if err != nil || receipt.Status != "completed" || receipt.Before == nil ||
		receipt.Before.ProductVersion != "0.3.0-rc.10" || receipt.After == nil ||
		receipt.After.ProductVersion != "0.3.0-rc.12" {
		t.Fatalf("rc.10 to rc.12 upgrade receipt=%+v err=%v", receipt, err)
	}
	if fixture.database.applyGrantsCount != 1 || fixture.database.roleBoundaryErr != nil ||
		fixture.database.sourceRoleBoundaryChecks != 1 || fixture.database.roleBoundaryChecks != 1 {
		t.Fatalf(
			"target policy convergence grants=%d source_checks=%d target_checks=%d residual_error=%v",
			fixture.database.applyGrantsCount,
			fixture.database.sourceRoleBoundaryChecks,
			fixture.database.roleBoundaryChecks,
			fixture.database.roleBoundaryErr,
		)
	}
	journal, err := fixture.engine.readActiveJournal()
	if err != nil || journal.Status != "completed" ||
		!slices.Contains(journal.CompletedPhases, "migrate_database") ||
		!slices.Contains(journal.CompletedPhases, "apply_role_grants") ||
		slices.Index(journal.CompletedPhases, "migrate_database") >= slices.Index(journal.CompletedPhases, "apply_role_grants") {
		t.Fatalf("upgrade journal=%+v err=%v", journal, err)
	}
}

func newRC10SourcePrivilegeUpgradeFixture(t *testing.T) (installFixture, string) {
	t.Helper()
	fixture := newInstallFixture(t)
	writeSyntheticAccessCredential(t, fixture.paths.AccessAccountsFile)
	fixture.artifact = packVersionedHostRelease(
		t,
		fixture,
		"rc10-source-privilege-current",
		"0.3.0-rc.10",
		"0.3.0-rc.10/manual-r10",
		"1010101010101010101010101010101010101010",
		nil,
		"[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed\n",
		withDatabaseSchema(20),
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withAccessPlane(),
	)
	current, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify rc.10 source: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity:        current.Identity,
		accessProtected: true,
	}}
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install rc.10 source: %v", err)
	}
	controllerUnit := string(mustReadTestFile(
		t,
		filepath.Join("..", "..", "deploy", connectorControllerUnitName),
	))
	targetArtifact := packVersionedHostRelease(
		t,
		fixture,
		"rc12-source-privilege-target",
		"0.3.0-rc.12",
		"0.3.0-rc.12/manual-r12",
		"1212121212121212121212121212121212121212",
		[]string{"0.3.0-rc.10"},
		"[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed\n",
		withDatabaseSchema(22),
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withAccessPlane(),
		withConnectorControllerUnit(controllerUnit),
	)
	target, err := fixture.engine.verifyTarget(targetArtifact)
	if err != nil {
		t.Fatalf("verify rc.12 target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity:        target.Identity,
		accessProtected: true,
	}}
	return fixture, targetArtifact
}
