package hostctl

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/releasebundle"
)

const rc13AccessFeedUnit = "[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed\n"

func TestRC13FreshInstallRequiresExternallyProvisionedAccessCredentialBeforeMutation(
	t *testing.T,
) {
	fixture := newInstallFixture(t)
	target := packRC13AccessTarget(t, fixture, nil, nil)

	if _, err := fixture.engine.Install(context.Background(), target); err == nil ||
		!strings.Contains(err.Error(), "access account credential is required and invalid") {
		t.Fatalf("fresh install without access credential error=%v", err)
	}
	for _, path := range []string{
		fixture.paths.AccessAccountsFile,
		fixture.paths.CurrentLink,
		fixture.paths.ServiceUnit,
		fixture.paths.InstallState,
		fixture.paths.ActiveOperation,
	} {
		if exists, err := fileExists(path); err != nil || exists {
			t.Fatalf("preflight created %s exists=%t err=%v", path, exists, err)
		}
	}
}

func TestRC13UpgradeFailsClosedBeforeMutationAndPreservesExternalCredential(
	t *testing.T,
) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install current release: %v", err)
	}
	target := packRC13AccessTarget(
		t,
		fixture,
		[]string{"0.2.0-rc.1"},
		nil,
	)
	beforeCurrent, err := readAbsoluteSymlink(fixture.paths.CurrentLink)
	if err != nil {
		t.Fatal(err)
	}
	beforeState, err := os.ReadFile(fixture.paths.InstallState)
	if err != nil {
		t.Fatal(err)
	}
	beforeUnit, err := os.ReadFile(fixture.paths.ServiceUnit)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(fixture.paths.ActiveOperation)
	if err != nil {
		t.Fatal(err)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	beforeCommandCount := len(runner.commands)

	if _, err := fixture.engine.Upgrade(context.Background(), target); err == nil ||
		!strings.Contains(err.Error(), "access account credential is required and invalid") {
		t.Fatalf("upgrade without access credential error=%v", err)
	}
	assertPreflightDidNotMutateManagedRuntime(
		t,
		fixture,
		beforeCurrent,
		beforeState,
		beforeUnit,
		beforeJournal,
		beforeCommandCount,
	)

	writeHostFile(t, fixture.paths.AccessAccountsFile, []byte("{}\n"), 0o600)
	if _, err := fixture.engine.Upgrade(context.Background(), target); err == nil ||
		!strings.Contains(err.Error(), "access account credential is required and invalid") {
		t.Fatalf("upgrade with invalid access credential error=%v", err)
	}
	assertPreflightDidNotMutateManagedRuntime(
		t,
		fixture,
		beforeCurrent,
		beforeState,
		beforeUnit,
		beforeJournal,
		beforeCommandCount,
	)

	credential := writeSyntheticAccessCredential(t, fixture.paths.AccessAccountsFile)
	report, err := fixture.engine.CheckUpgrade(context.Background(), target)
	if err != nil || !report.Supported || !containsCheck(report.Checks, "access_account_credential") {
		t.Fatalf("valid access credential preflight report=%+v err=%v", report, err)
	}
	verifiedTarget, err := fixture.engine.verifyTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity:        verifiedTarget.Identity,
		accessProtected: true,
	}}
	if _, err := fixture.engine.Upgrade(context.Background(), target); err != nil {
		t.Fatalf("upgrade with valid access credential: %v", err)
	}
	afterCredential, err := os.ReadFile(fixture.paths.AccessAccountsFile)
	if err != nil || !bytes.Equal(afterCredential, credential) {
		t.Fatalf("externally provisioned credential changed err=%v", err)
	}
	state, err := fixture.engine.readInstallState()
	if err != nil || state.Current.ProductVersion != "0.2.0-rc.13" {
		t.Fatalf("upgraded state=%+v err=%v", state, err)
	}
}

func TestRC13AdoptRequiresAccessCredentialBeforeLegacyMutation(t *testing.T) {
	fixture, legacyArtifact := newLegacyAdoptFixture(t)
	existingTarget, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	target := packRC13AccessTarget(
		t,
		fixture,
		nil,
		existingTarget.Bundle.Manifest.SupportedLegacySources,
	)
	beforeCurrent, err := readAbsoluteSymlink(fixture.paths.CurrentLink)
	if err != nil {
		t.Fatal(err)
	}
	beforeUnit, err := os.ReadFile(fixture.paths.ServiceUnit)
	if err != nil {
		t.Fatal(err)
	}
	beforeEnvironment, err := os.ReadFile(fixture.paths.EnvironmentFile)
	if err != nil {
		t.Fatal(err)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	beforeCommandCount := len(runner.commands)

	if _, err := fixture.engine.Adopt(context.Background(), target, legacyArtifact); err == nil ||
		!strings.Contains(err.Error(), "access account credential is required and invalid") {
		t.Fatalf("adopt without access credential error=%v", err)
	}
	afterCurrent, err := readAbsoluteSymlink(fixture.paths.CurrentLink)
	if err != nil || afterCurrent != beforeCurrent {
		t.Fatalf("legacy current changed before=%q after=%q err=%v", beforeCurrent, afterCurrent, err)
	}
	assertFileContents(t, fixture.paths.ServiceUnit, beforeUnit)
	assertFileContents(t, fixture.paths.EnvironmentFile, beforeEnvironment)
	if len(runner.commands) != beforeCommandCount {
		t.Fatalf("adopt preflight ran host commands: %+v", runner.commands[beforeCommandCount:])
	}
	for _, path := range []string{
		fixture.paths.AccessAccountsFile,
		fixture.paths.InstallState,
		fixture.paths.ActiveOperation,
	} {
		if exists, err := fileExists(path); err != nil || exists {
			t.Fatalf("adopt preflight created %s exists=%t err=%v", path, exists, err)
		}
	}

	writeSyntheticAccessCredential(t, fixture.paths.AccessAccountsFile)
	report, err := fixture.engine.CheckAdopt(context.Background(), target, legacyArtifact)
	if err != nil || !report.Supported || !containsCheck(report.Checks, "access_account_credential") {
		t.Fatalf("valid adopt preflight report=%+v err=%v", report, err)
	}
}

func TestRC13ResumeRevalidatesCredentialBeforeJournalMutationAndRecoverIsReadOnly(
	t *testing.T,
) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install current release: %v", err)
	}
	target := packRC13AccessTarget(
		t,
		fixture,
		[]string{"0.2.0-rc.1"},
		nil,
	)
	writeSyntheticAccessCredential(t, fixture.paths.AccessAccountsFile)
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "exclusive_maintenance")
	if _, err := fixture.engine.Upgrade(context.Background(), target); err == nil {
		t.Fatal("expected injected upgrade failure")
	}
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	movedCredential := fixture.paths.AccessAccountsFile + ".unavailable"
	if err := os.Rename(fixture.paths.AccessAccountsFile, movedCredential); err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(fixture.paths.ActiveOperation)
	if err != nil {
		t.Fatal(err)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	beforeCommandCount := len(runner.commands)

	if _, err := fixture.engine.Recover(context.Background()); err != nil {
		t.Fatalf("read-only recover/status: %v", err)
	}
	afterRecover, err := os.ReadFile(fixture.paths.ActiveOperation)
	if err != nil || !bytes.Equal(afterRecover, beforeJournal) {
		t.Fatalf("recover mutated active journal err=%v", err)
	}
	if _, err := fixture.engine.Resume(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "access account credential is required and invalid") {
		t.Fatalf("resume without access credential error=%v", err)
	}
	afterResume, err := os.ReadFile(fixture.paths.ActiveOperation)
	if err != nil || !bytes.Equal(afterResume, beforeJournal) {
		t.Fatalf("resume preflight mutated active journal err=%v", err)
	}
	if len(runner.commands) != beforeCommandCount {
		t.Fatalf("recover/resume preflight ran host commands: %+v", runner.commands[beforeCommandCount:])
	}
}

func TestRC13AbortRevalidatesSourceCredentialBeforeAnyRecoveryMutation(t *testing.T) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install current release: %v", err)
	}
	writeSyntheticAccessCredential(t, fixture.paths.AccessAccountsFile)
	rc13 := packRC13AccessTarget(
		t,
		fixture,
		[]string{"0.2.0-rc.1"},
		nil,
	)
	rc13Target, err := fixture.engine.verifyTarget(rc13)
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity:        rc13Target.Identity,
		accessProtected: true,
	}}
	if _, err := fixture.engine.Upgrade(context.Background(), rc13); err != nil {
		t.Fatalf("upgrade to rc13: %v", err)
	}

	target := packVersionedHostRelease(
		t,
		fixture,
		"post-rc13-abort",
		"0.2.0",
		"0.2.0/manual-r1",
		"1414141414141414141414141414141414141414",
		[]string{"0.2.0-rc.13"},
		rc13AccessFeedUnit,
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withAccessPlane(),
		withDatabaseSchema(14),
	)
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "stage_release")
	if _, err := fixture.engine.Upgrade(context.Background(), target); err == nil {
		t.Fatal("expected injected post-rc13 upgrade failure")
	}
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	beforeCurrent, err := readAbsoluteSymlink(fixture.paths.CurrentLink)
	if err != nil {
		t.Fatal(err)
	}
	beforeState, err := os.ReadFile(fixture.paths.InstallState)
	if err != nil {
		t.Fatal(err)
	}
	beforeUnit, err := os.ReadFile(fixture.paths.ServiceUnit)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(fixture.paths.ActiveOperation)
	if err != nil {
		t.Fatal(err)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	beforeCommandCount := len(runner.commands)
	movedCredential := fixture.paths.AccessAccountsFile + ".unavailable"
	if err := os.Rename(fixture.paths.AccessAccountsFile, movedCredential); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.engine.Abort(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "access account credential is required and invalid") {
		t.Fatalf("abort without rc13 source credential error=%v", err)
	}
	assertPreflightDidNotMutateManagedRuntime(
		t,
		fixture,
		beforeCurrent,
		beforeState,
		beforeUnit,
		beforeJournal,
		beforeCommandCount,
	)
}

func TestRC13AbortBarrierPrecedesCredentialValidationAndRecoveryMutation(t *testing.T) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install current release: %v", err)
	}
	writeSyntheticAccessCredential(t, fixture.paths.AccessAccountsFile)
	rc13 := packRC13AccessTarget(
		t,
		fixture,
		[]string{"0.2.0-rc.1"},
		nil,
	)
	rc13Target, err := fixture.engine.verifyTarget(rc13)
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity:        rc13Target.Identity,
		accessProtected: true,
	}}
	if _, err := fixture.engine.Upgrade(context.Background(), rc13); err != nil {
		t.Fatalf("upgrade to rc13: %v", err)
	}

	target := packVersionedHostRelease(
		t,
		fixture,
		"post-rc13-abort-barrier",
		"0.2.0",
		"0.2.0/manual-r1",
		"1515151515151515151515151515151515151515",
		[]string{"0.2.0-rc.13"},
		rc13AccessFeedUnit,
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withAccessPlane(),
		withDatabaseSchema(14),
	)
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "migrate_database")
	if _, err := fixture.engine.Upgrade(context.Background(), target); err == nil {
		t.Fatal("expected injected post-migration upgrade failure")
	}
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	beforeJournal, err := os.ReadFile(fixture.paths.ActiveOperation)
	if err != nil {
		t.Fatal(err)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	beforeCommandCount := len(runner.commands)
	movedCredential := fixture.paths.AccessAccountsFile + ".unavailable"
	if err := os.Rename(fixture.paths.AccessAccountsFile, movedCredential); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.engine.Abort(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "crossed a database or activation barrier") {
		t.Fatalf("abort after migration barrier error=%v", err)
	}
	afterJournal, err := os.ReadFile(fixture.paths.ActiveOperation)
	if err != nil || !bytes.Equal(afterJournal, beforeJournal) {
		t.Fatalf("barrier rejection mutated active journal err=%v", err)
	}
	if len(runner.commands) != beforeCommandCount {
		t.Fatalf(
			"barrier rejection ran recovery commands: %+v",
			runner.commands[beforeCommandCount:],
		)
	}
}

func TestRC13RollbackResumeToRC12UsesHistoricalAnonymousReadProof(t *testing.T) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install current release: %v", err)
	}
	rc12 := packVersionedHostRelease(
		t,
		fixture,
		"rc12-before-access",
		"0.2.0-rc.12",
		"0.2.0-rc.12/manual-r12",
		"1212121212121212121212121212121212121212",
		[]string{"0.2.0-rc.1"},
		rc13AccessFeedUnit,
		withLiteLLMUnit(testLiteLLMWorkerUnit),
	)
	rc12Target, err := fixture.engine.verifyTarget(rc12)
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity: rc12Target.Identity,
	}}
	if _, err := fixture.engine.Upgrade(context.Background(), rc12); err != nil {
		t.Fatalf("upgrade to rc12: %v", err)
	}

	writeSyntheticAccessCredential(t, fixture.paths.AccessAccountsFile)
	rc13 := packVersionedHostRelease(
		t,
		fixture,
		"rc13-compatible-rollback",
		"0.2.0-rc.13",
		"0.2.0-rc.13/manual-r13",
		"1313131313131313131313131313131313131313",
		[]string{"0.2.0-rc.12"},
		rc13AccessFeedUnit,
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withAccessPlane(),
		withApplicationRollbackCompatible(),
		withDatabaseSchema(14),
	)
	rc13Target, err := fixture.engine.verifyTarget(rc13)
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity:        rc13Target.Identity,
		accessProtected: true,
	}}
	if _, err := fixture.engine.Upgrade(context.Background(), rc13); err != nil {
		t.Fatalf("upgrade to rc13: %v", err)
	}

	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity: rc12Target.Identity,
	}}
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "restart_service")
	if _, err := fixture.engine.Rollback(context.Background(), "operator_request"); err == nil {
		t.Fatal("expected injected rc13 to rc12 rollback failure")
	}
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	receipt, err := fixture.engine.Resume(context.Background())
	if err != nil || receipt.Status != "completed" ||
		receipt.After == nil ||
		receipt.After.ProductVersion != "0.2.0-rc.12" ||
		!containsCheck(receipt.Checks, "representative_read") ||
		containsCheck(receipt.Checks, "anonymous_access_boundary") {
		t.Fatalf("rc13 to rc12 resumed rollback receipt=%+v err=%v", receipt, err)
	}
}

func packRC13AccessTarget(
	t *testing.T,
	fixture installFixture,
	upgradeSources []string,
	legacySources []releasebundle.LegacySource,
) string {
	t.Helper()
	return packVersionedHostRelease(
		t,
		fixture,
		"rc13-access",
		"0.2.0-rc.13",
		"0.2.0-rc.13/manual-r13",
		"1313131313131313131313131313131313131313",
		upgradeSources,
		rc13AccessFeedUnit,
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withAccessPlane(),
		withSupportedLegacySources(legacySources),
	)
}

func writeSyntheticAccessCredential(t *testing.T, path string) []byte {
	t.Helper()
	hash, err := access.DerivePasswordHash([]byte("synthetic local test password"))
	if err != nil {
		t.Fatal(err)
	}
	config := access.AccountConfig{
		SchemaVersion: access.AccountConfigSchema,
		Accounts: []access.AccountConfigEntry{{
			SubjectID:    "xs_0123456789abcdef",
			Username:     "operator.synthetic",
			Role:         access.RoleOperator,
			PasswordHash: hash,
		}},
	}
	contents, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	contents = append(contents, '\n')
	writeHostFile(t, path, contents, 0o600)
	return contents
}

func assertPreflightDidNotMutateManagedRuntime(
	t *testing.T,
	fixture installFixture,
	expectedCurrent string,
	expectedState []byte,
	expectedUnit []byte,
	expectedJournal []byte,
	expectedCommandCount int,
) {
	t.Helper()
	current, err := readAbsoluteSymlink(fixture.paths.CurrentLink)
	if err != nil || current != expectedCurrent {
		t.Fatalf("current changed before=%q after=%q err=%v", expectedCurrent, current, err)
	}
	assertFileContents(t, fixture.paths.InstallState, expectedState)
	assertFileContents(t, fixture.paths.ServiceUnit, expectedUnit)
	assertFileContents(t, fixture.paths.ActiveOperation, expectedJournal)
	runner := fixture.engine.Runner.(*fakeRunner)
	if len(runner.commands) != expectedCommandCount {
		t.Fatalf("preflight ran host commands: %+v", runner.commands[expectedCommandCount:])
	}
}

func containsCheck(checks []CheckResult, name string) bool {
	for _, check := range checks {
		if check.Name == name && check.Status == "pass" {
			return true
		}
	}
	return false
}
