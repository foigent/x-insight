package hostctl

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/db"
)

func TestRC15ThreeRoleTargetUpgradesThroughJournaledWorkspaceCredentialBridge(t *testing.T) {
	for _, testCase := range []struct {
		name                   string
		failAfterBridge        bool
		expectResumeCompletion bool
	}{
		{name: "uninterrupted"},
		{name: "resume_after_boundary_expansion", failAfterBridge: true, expectResumeCompletion: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newInstallFixture(t)
			if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
				t.Fatalf("install baseline release: %v", err)
			}
			credential := writeSyntheticAccessCredential(t, fixture.paths.AccessAccountsFile)
			rc15 := packVersionedHostRelease(
				t,
				fixture,
				"rc15-real-three-role-source",
				"0.2.0-rc.15",
				"0.2.0-rc.15/manual-r15",
				"1515151515151515151515151515151515151515",
				[]string{"0.2.0-rc.1"},
				rc13AccessFeedUnit,
				withLiteLLMUnit(testLiteLLMWorkerUnit),
				withAccessPlane(),
			)
			rc15Target, err := fixture.engine.verifyTarget(rc15)
			if err != nil {
				t.Fatal(err)
			}
			fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
				identity:        rc15Target.Identity,
				accessProtected: true,
			}}
			if _, err := fixture.engine.Upgrade(context.Background(), rc15); err != nil {
				t.Fatalf("upgrade to rc15: %v", err)
			}

			legacyRef := stageLegacyThreeRoleCredentialSet(t, fixture.paths, "real-rc15-three-role")
			if err := atomicSymlink(legacyRef, fixture.paths.DatabaseCurrentLink); err != nil {
				t.Fatal(err)
			}
			fixture.database.facts.WorkspaceRoleExists = false

			targetArtifact := packVersionedHostRelease(
				t,
				fixture,
				"rc3-workspace-bridge-target",
				"0.3.0-rc.3",
				"0.3.0-rc.3/manual-r3",
				"2222222222222222222222222222222222222222",
				[]string{"0.2.0-rc.15"},
				rc13AccessFeedUnit,
				withLiteLLMUnit(testLiteLLMWorkerUnit),
				withAccessPlane(),
				withDatabaseSchema(19),
			)
			target, err := fixture.engine.verifyTarget(targetArtifact)
			if err != nil {
				t.Fatal(err)
			}
			fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
				identity:        target.Identity,
				accessProtected: true,
			}}
			journalBeforeCheck, err := fixture.engine.readActiveJournal()
			if err != nil {
				t.Fatal(err)
			}
			check, err := fixture.engine.CheckUpgrade(context.Background(), targetArtifact)
			if err != nil || !check.Supported || checkStatus(check.Checks, "database_roles") != "pass" {
				t.Fatalf("three-role rc15 preflight=%+v err=%v", check, err)
			}
			var bridgeDetail string
			for _, item := range check.Checks {
				if item.Name == "database_roles" {
					bridgeDetail = item.Detail
				}
			}
			if !strings.Contains(bridgeDetail, "workspace_bridge_required") {
				t.Fatalf("preflight did not expose bridge detail %q", bridgeDetail)
			}
			backupAction, bridgeAction := -1, -1
			for index, action := range check.RequiredActions {
				if strings.Contains(action, "backup") {
					backupAction = index
				}
				if strings.Contains(action, "credential successor") {
					bridgeAction = index
				}
			}
			if backupAction < 0 || bridgeAction < 0 || backupAction >= bridgeAction {
				t.Fatalf("preflight action order does not preserve backup-before-bridge: %v", check.RequiredActions)
			}
			activeAfterCheck, err := readAbsoluteSymlink(fixture.paths.DatabaseCurrentLink)
			journalAfterCheck, journalErr := fixture.engine.readActiveJournal()
			if err != nil || journalErr != nil || activeAfterCheck != legacyRef || journalAfterCheck.OperationID != journalBeforeCheck.OperationID || fixture.database.facts.WorkspaceRoleExists {
				t.Fatalf("preflight mutated state active=%q journal_before=%q journal_after=%q workspace_role=%t err=%v journal_err=%v", activeAfterCheck, journalBeforeCheck.OperationID, journalAfterCheck.OperationID, fixture.database.facts.WorkspaceRoleExists, err, journalErr)
			}
			if _, err := os.Lstat(filepath.Join(legacyRef, "postgres-workspace.dsn")); !os.IsNotExist(err) {
				t.Fatalf("preflight changed legacy credential set: %v", err)
			}
			fixture.database.facts.WorkspaceRoleExists = true
			if _, err := fixture.engine.CheckUpgrade(context.Background(), targetArtifact); err == nil || !strings.Contains(err.Error(), "workspace database role must be absent") {
				t.Fatalf("pre-existing workspace role did not fail closed: %v", err)
			}
			fixture.database.facts.WorkspaceRoleExists = false
			if testCase.failAfterBridge {
				t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "expand_workspace_database_boundary")
			}
			receipt, upgradeErr := fixture.engine.Upgrade(context.Background(), targetArtifact)
			if testCase.failAfterBridge {
				if upgradeErr == nil {
					t.Fatal("expected injected post-bridge interruption")
				}
				journal, err := fixture.engine.readActiveJournal()
				if err != nil || !containsString(journal.CompletedPhases, "expand_workspace_database_boundary") || journal.DatabaseSetAfter == "" || journal.BackupPath == "" || journal.BackupSHA256 == "" {
					t.Fatalf("bridge journal=%+v err=%v", journal, err)
				}
				if containsString(journal.AllowedActions, "abort") {
					t.Fatalf("post-bridge journal incorrectly allows abort: %v", journal.AllowedActions)
				}
				if _, err := fixture.engine.Abort(context.Background()); err == nil || !strings.Contains(err.Error(), "crossed a database or activation barrier") {
					t.Fatalf("post-bridge abort did not fail closed: %v", err)
				}
				t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
				receipt, upgradeErr = fixture.engine.Resume(context.Background())
			}
			if upgradeErr != nil || receipt.Status != "completed" || receipt.After == nil || receipt.After.ProductVersion != "0.3.0-rc.3" {
				t.Fatalf("bridge upgrade receipt=%+v err=%v", receipt, upgradeErr)
			}
			if !fixture.database.facts.WorkspaceRoleExists {
				t.Fatal("workspace role was not created")
			}
			activeRef, err := readAbsoluteSymlink(fixture.paths.DatabaseCurrentLink)
			if err != nil || activeRef == legacyRef || receipt.DatabaseSetRef != activeRef {
				t.Fatalf("active credential set=%q legacy=%q receipt=%q err=%v", activeRef, legacyRef, receipt.DatabaseSetRef, err)
			}
			expanded, err := LoadDatabaseCredentialSet(fixture.paths, activeRef)
			if err != nil || expanded.Workspace == nil {
				t.Fatalf("expanded credential set err=%v", err)
			}
			if _, err := os.Lstat(filepath.Join(legacyRef, "postgres-workspace.dsn")); !os.IsNotExist(err) {
				t.Fatalf("legacy credential set was modified: %v", err)
			}
			afterCredential, err := os.ReadFile(fixture.paths.AccessAccountsFile)
			if err != nil || !bytes.Equal(afterCredential, credential) {
				t.Fatalf("access credential changed err=%v", err)
			}
		})
	}
}

func TestRC2FourRoleSourceUpgradesToRC3WithoutCredentialBridge(t *testing.T) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install baseline release: %v", err)
	}
	writeSyntheticAccessCredential(t, fixture.paths.AccessAccountsFile)
	rc2 := packVersionedHostRelease(
		t, fixture, "rc2-four-role-source", "0.3.0-rc.2", "0.3.0-rc.2/manual-r2",
		"2222222222222222222222222222222222222222", []string{"0.2.0-rc.1"},
		rc13AccessFeedUnit, withLiteLLMUnit(testLiteLLMWorkerUnit), withAccessPlane(),
		withDatabaseSchema(19),
	)
	rc2Target, err := fixture.engine.verifyTarget(rc2)
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: rc2Target.Identity, accessProtected: true}}
	if _, err := fixture.engine.Upgrade(context.Background(), rc2); err != nil {
		t.Fatalf("upgrade to rc2: %v", err)
	}
	databaseSetBefore, err := readAbsoluteSymlink(fixture.paths.DatabaseCurrentLink)
	if err != nil {
		t.Fatal(err)
	}
	rc3 := packVersionedHostRelease(
		t, fixture, "rc3-four-role-target", "0.3.0-rc.3", "0.3.0-rc.3/manual-r3",
		"3333333333333333333333333333333333333333", []string{"0.3.0-rc.2"},
		rc13AccessFeedUnit, withLiteLLMUnit(testLiteLLMWorkerUnit), withAccessPlane(),
		withDatabaseSchema(19),
	)
	rc3Target, err := fixture.engine.verifyTarget(rc3)
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: rc3Target.Identity, accessProtected: true}}
	invalidRC2Set := stageLegacyThreeRoleCredentialSet(t, fixture.paths, "invalid-rc2-three-role")
	if err := atomicSymlink(invalidRC2Set, fixture.paths.DatabaseCurrentLink); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.CheckUpgrade(context.Background(), rc3); err == nil || !strings.Contains(err.Error(), "workspace database credential is required") {
		t.Fatalf("rc2 source without workspace credential did not fail closed: %v", err)
	}
	if err := atomicSymlink(databaseSetBefore, fixture.paths.DatabaseCurrentLink); err != nil {
		t.Fatal(err)
	}
	check, err := fixture.engine.CheckUpgrade(context.Background(), rc3)
	if err != nil || !check.Supported {
		t.Fatalf("rc2 to rc3 preflight=%+v err=%v", check, err)
	}
	receipt, err := fixture.engine.Upgrade(context.Background(), rc3)
	if err != nil || receipt.Status != "completed" {
		t.Fatalf("rc2 to rc3 receipt=%+v err=%v", receipt, err)
	}
	databaseSetAfter, err := readAbsoluteSymlink(fixture.paths.DatabaseCurrentLink)
	if err != nil || databaseSetAfter != databaseSetBefore || receipt.DatabaseSetRef != databaseSetBefore {
		t.Fatalf("four-role credential set changed before=%q after=%q receipt=%q err=%v", databaseSetBefore, databaseSetAfter, receipt.DatabaseSetRef, err)
	}
	journal, err := fixture.engine.readActiveJournal()
	if err != nil || journal.DatabaseSetAfter != "" || containsString(journal.CompletedPhases, "expand_workspace_database_boundary") {
		t.Fatalf("rc2 source unexpectedly used workspace bridge journal=%+v err=%v", journal, err)
	}
}

func TestRC14ToRC15UpgradePreservesAccessCredentialAndSchema15(t *testing.T) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install baseline release: %v", err)
	}

	credential := writeSyntheticAccessCredential(t, fixture.paths.AccessAccountsFile)
	rc14 := packVersionedHostRelease(
		t,
		fixture,
		"rc14-access-trace",
		"0.2.0-rc.14",
		"0.2.0-rc.14/manual-r14",
		"1414141414141414141414141414141414141414",
		[]string{"0.2.0-rc.1"},
		rc13AccessFeedUnit,
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withAccessPlane(),
	)
	rc14Target, err := fixture.engine.verifyTarget(rc14)
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity:        rc14Target.Identity,
		accessProtected: true,
	}}
	if _, err := fixture.engine.Upgrade(context.Background(), rc14); err != nil {
		t.Fatalf("upgrade to rc14: %v", err)
	}

	rc15 := packVersionedHostRelease(
		t,
		fixture,
		"rc15-demo-refinement",
		"0.2.0-rc.15",
		"0.2.0-rc.15/manual-r15",
		"1515151515151515151515151515151515151515",
		[]string{"0.2.0-rc.14"},
		rc13AccessFeedUnit,
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withAccessPlane(),
	)
	rc15Target, err := fixture.engine.verifyTarget(rc15)
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity:        rc15Target.Identity,
		accessProtected: true,
	}}
	stateBeforeCheck, err := fixture.engine.readInstallState()
	if err != nil {
		t.Fatal(err)
	}
	journalBeforeCheck, err := fixture.engine.readActiveJournal()
	if err != nil {
		t.Fatal(err)
	}
	currentBeforeCheck, err := readAbsoluteSymlink(fixture.paths.CurrentLink)
	if err != nil {
		t.Fatal(err)
	}
	ledgerBeforeCheck := append([]db.MigrationRecord(nil), fixture.database.facts.MigrationLedger...)
	check, err := fixture.engine.CheckUpgrade(context.Background(), rc15)
	if err != nil || !check.Supported ||
		check.Current == nil || check.Current.ProductVersion != "0.2.0-rc.14" ||
		check.Target == nil || check.Target.ProductVersion != "0.2.0-rc.15" {
		t.Fatalf("rc14 to rc15 preflight=%+v err=%v", check, err)
	}
	stateAfterCheck, stateErr := fixture.engine.readInstallState()
	journalAfterCheck, journalErr := fixture.engine.readActiveJournal()
	currentAfterCheck, currentErr := readAbsoluteSymlink(fixture.paths.CurrentLink)
	if stateErr != nil || journalErr != nil || currentErr != nil ||
		!reflect.DeepEqual(stateAfterCheck, stateBeforeCheck) ||
		!reflect.DeepEqual(journalAfterCheck, journalBeforeCheck) ||
		currentAfterCheck != currentBeforeCheck ||
		!reflect.DeepEqual(fixture.database.facts.MigrationLedger, ledgerBeforeCheck) {
		t.Fatalf("same-schema preflight mutated state: state=%v journal=%v current=%v ledger=%v", stateErr, journalErr, currentErr, fixture.database.facts.MigrationLedger)
	}
	if _, err := fixture.engine.Upgrade(context.Background(), rc15); err != nil {
		t.Fatalf("upgrade to rc15: %v", err)
	}

	state, err := fixture.engine.readInstallState()
	if err != nil ||
		state.Current.ProductVersion != "0.2.0-rc.15" ||
		state.Previous == nil ||
		state.Previous.ProductVersion != "0.2.0-rc.14" ||
		state.DatabaseSchemaCurrent != "15" {
		t.Fatalf("rc15 install state=%+v err=%v", state, err)
	}
	afterCredential, err := os.ReadFile(fixture.paths.AccessAccountsFile)
	if err != nil || !bytes.Equal(afterCredential, credential) {
		t.Fatalf("externally provisioned access credential changed err=%v", err)
	}
}
