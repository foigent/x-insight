package hostctl

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/legacyrelease"
	"github.com/foigent/x-insight/internal/releasebundle"
	"github.com/foigent/x-insight/internal/reviewbody"
)

func TestInstallWritesDurableStateReceiptAndRoleGate(t *testing.T) {
	fixture := newInstallFixture(t)
	fixture.engine.Random = bytes.NewReader(bytes.Repeat([]byte{0x42}, 16))

	var receipt OperationReceipt
	var err error
	func() {
		previousUmask := syscall.Umask(0o077)
		defer syscall.Umask(previousUmask)
		receipt, err = fixture.engine.Install(context.Background(), fixture.artifact)
	}()
	if err != nil {
		t.Fatalf("install under restrictive umask: %v", err)
	}
	if receipt.Status != "completed" || receipt.Kind != "install" || receipt.CompletedAt.IsZero() {
		t.Fatalf("receipt=%+v", receipt)
	}
	state, err := fixture.engine.readInstallState()
	if err != nil {
		t.Fatalf("read install state: %v", err)
	}
	if state.Current != *receipt.After || state.DatabaseSchemaCurrent != "1" {
		t.Fatalf("state=%+v receipt=%+v", state, receipt)
	}
	current, err := readAbsoluteSymlink(fixture.paths.CurrentLink)
	if err != nil || current != state.Current.ReleaseDir {
		t.Fatalf("current=%q err=%v", current, err)
	}
	journal, err := fixture.engine.readActiveJournal()
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if journal.Status != "completed" || journal.ReceiptAt == nil || !journal.ReceiptAt.Equal(receipt.CompletedAt) {
		t.Fatalf("journal=%+v", journal)
	}
	var persisted OperationReceipt
	if err := readJSONStrict(filepath.Join(fixture.paths.ReceiptsDir, receipt.OperationID+".json"), &persisted); err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	if !reflect.DeepEqual(persisted, receipt) {
		t.Fatalf("persisted receipt differs: persisted=%+v returned=%+v", persisted, receipt)
	}
	if !reflect.DeepEqual(fixture.database.loginTransitions, []bool{false, true, false, true}) {
		t.Fatalf("importer login transitions=%v", fixture.database.loginTransitions)
	}
	for path, expected := range map[string]os.FileMode{
		filepath.Dir(fixture.paths.ReleasesDir):                       0o755,
		fixture.paths.ReleasesDir:                                     0o755,
		fixture.paths.ControlDir:                                      0o755,
		fixture.paths.ServiceDropInDir:                                0o755,
		receipt.After.ReleaseDir:                                      0o755,
		filepath.Join(receipt.After.ReleaseDir, "bin"):                0o755,
		filepath.Join(receipt.After.ReleaseDir, "bin", "xinsightctl"): 0o755,
		fixture.paths.ServiceLauncher:                                 0o755,
		fixture.paths.ServiceUnit:                                     0o644,
		fixture.paths.ManagedServiceDropIn:                            0o644,
		fixture.paths.PostgresCompose:                                 0o644,
		fixture.paths.EnvironmentFile:                                 0o644,
		fixture.paths.ConnectorIdentityEnv:                            0o600,
	} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode().Perm() != expected {
			t.Fatalf("path %s mode=%v expected=%v err=%v", path, infoMode(info), expected, err)
		}
	}
	assertFileContents(t, fixture.paths.ManagedServiceDropIn, []byte(managedServiceDropIn))
	assertFileContents(
		t,
		fixture.paths.ConnectorIdentityEnv,
		[]byte("XINSIGHT_CONNECTOR_DEPLOYMENT_ID=xid_QkJCQkJCQkJCQkJCQkJCQg\n"),
	)
}

func infoMode(info os.FileInfo) os.FileMode {
	if info == nil {
		return 0
	}
	return info.Mode().Perm()
}

func checkStatus(checks []CheckResult, name string) string {
	for _, check := range checks {
		if check.Name == name {
			return check.Status
		}
	}
	return ""
}

func TestResumeAfterReceiptWriteUsesPersistedResultTimestamp(t *testing.T) {
	fixture := newInstallFixture(t)
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "write_receipt")

	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err == nil || !strings.Contains(err.Error(), "receipt_finalize_failed") {
		t.Fatalf("expected injected receipt finalization failure, got %v", err)
	}
	journalBefore, err := fixture.engine.readActiveJournal()
	if err != nil {
		t.Fatalf("read failed journal: %v", err)
	}
	if journalBefore.Status != "failed" || journalBefore.ReceiptAt == nil || !containsString(journalBefore.CompletedPhases, "write_receipt") {
		t.Fatalf("failed journal=%+v", journalBefore)
	}
	var receiptBefore OperationReceipt
	if err := readJSONStrict(filepath.Join(fixture.paths.ReceiptsDir, journalBefore.OperationID+".json"), &receiptBefore); err != nil {
		t.Fatalf("read pre-resume receipt: %v", err)
	}

	*fixture.now = fixture.now.Add(2 * time.Hour)
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	resumed, err := fixture.engine.Resume(context.Background())
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !reflect.DeepEqual(resumed, receiptBefore) {
		t.Fatalf("resume changed durable result: before=%+v after=%+v", receiptBefore, resumed)
	}
	journalAfter, err := fixture.engine.readActiveJournal()
	if err != nil {
		t.Fatalf("read completed journal: %v", err)
	}
	if journalAfter.Status != "completed" || journalAfter.ReceiptAt == nil || !journalAfter.ReceiptAt.Equal(receiptBefore.CompletedAt) {
		t.Fatalf("completed journal=%+v", journalAfter)
	}
}

func TestAbortBeforeMigrationPreservesStagedRelease(t *testing.T) {
	fixture := newInstallFixture(t)
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "stage_release")

	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err == nil || !strings.Contains(err.Error(), "release_staging_failed") {
		t.Fatalf("expected injected staging failure, got %v", err)
	}
	journal, err := fixture.engine.readActiveJournal()
	if err != nil || journal.Target == nil {
		t.Fatalf("failed journal=%+v err=%v", journal, err)
	}
	if _, err := os.Lstat(journal.Target.ReleaseDir); err != nil {
		t.Fatalf("staged release missing before abort: %v", err)
	}
	receipt, err := fixture.engine.Abort(context.Background())
	if err != nil {
		t.Fatalf("abort: %v", err)
	}
	if receipt.Status != "aborted" || !fixture.database.importerLogin {
		t.Fatalf("abort receipt=%+v importer_login=%v", receipt, fixture.database.importerLogin)
	}
	if _, err := os.Lstat(journal.Target.ReleaseDir); err != nil {
		t.Fatalf("abort must preserve staged release: %v", err)
	}
	if _, err := os.Lstat(fixture.artifact); err != nil {
		t.Fatalf("abort must preserve source artifact: %v", err)
	}
}

func TestExistingReceiptMustMatchEntireDurableResult(t *testing.T) {
	fixture := newInstallFixture(t)
	receipt := OperationReceipt{
		SchemaVersion: ReceiptSchema,
		OperationID:   "20260713T120000Z-deadbeef",
		Kind:          "install",
		Status:        "completed",
		Checks:        []CheckResult{{Name: "release_signature", Status: "pass", Detail: "key-a"}},
		CompletedAt:   time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
	}
	if err := fixture.engine.writeReceipt(receipt); err != nil {
		t.Fatalf("write receipt: %v", err)
	}
	receipt.Checks[0].Detail = "key-b"
	if err := fixture.engine.writeReceipt(receipt); err == nil {
		t.Fatal("expected mismatched existing receipt rejection")
	}
}

func TestHostArtifactVerificationRejectsWritableTrustStore(t *testing.T) {
	fixture := newInstallFixture(t)
	if err := os.Chmod(fixture.paths.TrustStore, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.VerifyArtifact(fixture.artifact); err == nil {
		t.Fatal("expected writable trust store rejection")
	}
}

func TestUpgradeRollbackAndReupgradePreserveConfigAndExpandedSchema(t *testing.T) {
	fixture := newInstallFixture(t)
	fixture.engine.Random = bytes.NewReader(bytes.Repeat([]byte{0x43}, 16))
	installed, err := fixture.engine.Install(context.Background(), fixture.artifact)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	originalEnvironment, err := os.ReadFile(fixture.paths.EnvironmentFile)
	if err != nil {
		t.Fatal(err)
	}
	customEnvironment := append(append([]byte(nil), originalEnvironment...), []byte("# operator-preserved-setting-boundary\n")...)
	if err := os.WriteFile(fixture.paths.EnvironmentFile, customEnvironment, 0o644); err != nil {
		t.Fatal(err)
	}
	generatedConnectorIdentity, err := os.ReadFile(fixture.paths.ConnectorIdentityEnv)
	if err != nil {
		t.Fatal(err)
	}
	customConnectorEnvironment := []byte("XINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://unirouter.example\n")
	if err := os.WriteFile(fixture.paths.ConnectorEnvironment, customConnectorEnvironment, 0o600); err != nil {
		t.Fatal(err)
	}
	writeHostFile(
		t,
		fixture.paths.TrustedTLSProxyEnv,
		[]byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xinsight.example\n"),
		0o600,
	)

	upgradeArtifact := packUpgradeRelease(t, fixture)
	upgradeTarget, err := fixture.engine.verifyTarget(upgradeArtifact)
	if err != nil {
		t.Fatalf("verify upgrade target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: upgradeTarget.Identity}}
	upgraded, err := fixture.engine.Upgrade(context.Background(), upgradeArtifact)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if upgraded.Before == nil || upgraded.After == nil || upgraded.Before.ProductVersion != "0.2.0-rc.1" || upgraded.After.ProductVersion != "0.2.0" {
		t.Fatalf("upgrade receipt=%+v", upgraded)
	}
	state, err := fixture.engine.readInstallState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Previous == nil || state.Current.ProductVersion != "0.2.0" || state.DatabaseSchemaCurrent != "2" || len(fixture.database.facts.MigrationLedger) != 2 {
		t.Fatalf("upgraded state=%+v facts=%+v", state, fixture.database.facts)
	}
	assertFileContents(t, fixture.paths.EnvironmentFile, customEnvironment)
	assertFileContents(t, fixture.paths.ConnectorIdentityEnv, generatedConnectorIdentity)
	assertFileContents(t, fixture.paths.ConnectorEnvironment, customConnectorEnvironment)
	if upgraded.BackupPath == "" || upgraded.BackupSHA256 == "" {
		t.Fatalf("upgrade backup identity missing: %+v", upgraded)
	}

	rollbackReadiness := installed.After
	rollbackReadiness.DatabaseSchema = "2"
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: *rollbackReadiness}}
	rolledBack, err := fixture.engine.Rollback(context.Background(), "field_validation")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolledBack.After == nil || rolledBack.After.ProductVersion != "0.2.0-rc.1" || len(fixture.database.facts.MigrationLedger) != 2 {
		t.Fatalf("rollback receipt=%+v facts=%+v", rolledBack, fixture.database.facts)
	}
	state, err = fixture.engine.readInstallState()
	if err != nil || state.DatabaseSchemaCurrent != "2" || state.Current.ProductVersion != "0.2.0-rc.1" {
		t.Fatalf("rolled-back state=%+v err=%v", state, err)
	}
	assertFileContents(t, fixture.paths.EnvironmentFile, customEnvironment)
	assertFileContents(t, fixture.paths.ConnectorIdentityEnv, generatedConnectorIdentity)
	assertFileContents(t, fixture.paths.ConnectorEnvironment, customConnectorEnvironment)

	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: upgradeTarget.Identity}}
	reupgraded, err := fixture.engine.Upgrade(context.Background(), upgradeArtifact)
	if err != nil {
		t.Fatalf("re-upgrade over already-expanded schema: %v", err)
	}
	if reupgraded.After == nil || reupgraded.After.ProductVersion != "0.2.0" || len(fixture.database.facts.MigrationLedger) != 2 {
		t.Fatalf("re-upgrade receipt=%+v facts=%+v", reupgraded, fixture.database.facts)
	}
	assertFileContents(t, fixture.paths.EnvironmentFile, customEnvironment)
	assertFileContents(t, fixture.paths.ConnectorIdentityEnv, generatedConnectorIdentity)
	assertFileContents(t, fixture.paths.ConnectorEnvironment, customConnectorEnvironment)
}

func TestLegacyTargetDropInAndExistingRestrictiveControlModeSurviveRollbackLoop(t *testing.T) {
	fixture, legacyArtifact := newLegacyAdoptFixture(t)
	if err := os.MkdirAll(fixture.paths.ControlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture.paths.ControlDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeHostFile(t, filepath.Join(fixture.paths.ServiceDropInDir, legacyPhase1ServiceDropInName), []byte(legacyPhase1ServiceDropIn), 0o644)

	adoptCheck, err := fixture.engine.CheckAdopt(context.Background(), fixture.artifact, legacyArtifact)
	if err != nil || !adoptCheck.Supported || checkStatus(adoptCheck.Checks, "service_dropins") != "pass" {
		t.Fatalf("legacy drop-in adopt check=%+v err=%v", adoptCheck, err)
	}
	if _, err := fixture.engine.Adopt(context.Background(), fixture.artifact, legacyArtifact); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	target, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: target.Identity}}
	upgraded, err := fixture.engine.Upgrade(context.Background(), fixture.artifact)
	if err != nil {
		t.Fatalf("upgrade with inherited target state: %v", err)
	}
	if checkStatus(upgraded.Checks, "service_launcher") != "pass" {
		t.Fatalf("upgrade launcher proof=%+v", upgraded.Checks)
	}
	for path, expected := range map[string]os.FileMode{
		fixture.paths.ControlDir:           0o755,
		fixture.paths.ServiceDropInDir:     0o755,
		fixture.paths.ManagedServiceDropIn: 0o644,
	} {
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode().Perm() != expected {
			t.Fatalf("path %s mode=%v expected=%v err=%v", path, infoMode(info), expected, statErr)
		}
	}
	assertFileContents(t, filepath.Join(fixture.paths.ServiceDropInDir, legacyPhase1ServiceDropInName), []byte(legacyPhase1ServiceDropIn))
	assertFileContents(t, fixture.paths.ManagedServiceDropIn, []byte(managedServiceDropIn))

	fixture.engine.HTTPClient = &http.Client{Transport: legacyReadyTransport{}}
	rolledBack, err := fixture.engine.Rollback(context.Background(), "field_validation")
	if err != nil {
		t.Fatalf("legacy application rollback: %v", err)
	}
	if rolledBack.After == nil || rolledBack.After.LegacyReleaseID == "" || checkStatus(rolledBack.Checks, "service_launcher") != "pass" {
		t.Fatalf("legacy rollback receipt=%+v", rolledBack)
	}
	assertFileContents(t, fixture.paths.ManagedServiceDropIn, []byte(managedServiceDropIn))

	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: target.Identity}}
	if _, err := fixture.engine.Upgrade(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("re-upgrade over inherited target state: %v", err)
	}
}

func TestLegacyAdoptionStagesWorkspaceCredentialSuccessorWithoutChangingSourceSet(t *testing.T) {
	fixture, legacyArtifact := newLegacyAdoptFixture(t)
	legacySet := stageLegacyThreeRoleCredentialSet(t, fixture.paths, "legacy-adoption-three-role")
	if err := atomicSymlink(legacySet, fixture.paths.DatabaseCurrentLink); err != nil {
		t.Fatal(err)
	}
	fixture.database.facts.WorkspaceRoleExists = true
	if _, err := fixture.engine.CheckAdopt(context.Background(), fixture.artifact, legacyArtifact); err == nil || !strings.Contains(err.Error(), "workspace database role must be absent") {
		t.Fatalf("legacy adoption accepted a pre-existing workspace role: %v", err)
	}
	fixture.database.facts.WorkspaceRoleExists = false
	check, err := fixture.engine.CheckAdopt(context.Background(), fixture.artifact, legacyArtifact)
	if err != nil || !check.Supported {
		t.Fatalf("legacy three-role adoption preflight=%+v err=%v", check, err)
	}
	var roleDetail string
	for _, item := range check.Checks {
		if item.Name == "role_credentials" {
			roleDetail = item.Detail
		}
	}
	if !strings.Contains(roleDetail, "workspace_bridge_required") {
		t.Fatalf("legacy adoption did not declare workspace bridge: %q", roleDetail)
	}
	receipt, err := fixture.engine.Adopt(context.Background(), fixture.artifact, legacyArtifact)
	if err != nil || receipt.Status != "completed" || receipt.DatabaseSetRef == legacySet {
		t.Fatalf("legacy adoption receipt=%+v err=%v", receipt, err)
	}
	activeSet, err := readAbsoluteSymlink(fixture.paths.DatabaseCurrentLink)
	if err != nil || activeSet != receipt.DatabaseSetRef {
		t.Fatalf("legacy adoption active set=%q receipt=%q err=%v", activeSet, receipt.DatabaseSetRef, err)
	}
	if _, err := LoadDatabaseCredentialSet(fixture.paths, activeSet); err != nil {
		t.Fatalf("legacy adoption successor is invalid: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(legacySet, "postgres-workspace.dsn")); !os.IsNotExist(err) {
		t.Fatalf("legacy adoption source credential set changed: %v", err)
	}
}

func TestLegacyAdoptRejectsUnknownActiveSystemdDropIn(t *testing.T) {
	fixture, legacyArtifact := newLegacyAdoptFixture(t)
	writeHostFile(t, filepath.Join(fixture.paths.ServiceDropInDir, "50-unknown.conf"), []byte("[Service]\nExecStart=/tmp/unknown\n"), 0o644)

	if _, err := fixture.engine.CheckAdopt(context.Background(), fixture.artifact, legacyArtifact); err == nil || !strings.Contains(err.Error(), "unsupported active systemd service drop-in") {
		t.Fatalf("expected unknown active drop-in rejection, got %v", err)
	}
}

func TestManagedServiceDropInRuntimeMatchesReleaseGeneration(t *testing.T) {
	root := t.TempDir()
	t.Setenv(AllowTestRootEnv, "1")
	t.Setenv(TestRootEnv, root)
	paths, err := PathsFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	releaseDir := filepath.Join(root, "release")
	if err := os.MkdirAll(filepath.Join(releaseDir, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := validateManagedServiceDropInRuntime(releaseDir, paths); err != nil {
		t.Fatalf("pre-bridge release without an active managed drop-in: %v", err)
	}
	writeHostFile(t, paths.ManagedServiceDropIn, []byte(managedServiceDropIn), 0o644)
	if err := validateManagedServiceDropInRuntime(releaseDir, paths); err == nil || !strings.Contains(err.Error(), "presence does not match") {
		t.Fatalf("expected pre-bridge presence mismatch, got %v", err)
	}
	writeHostFile(t, filepath.Join(releaseDir, "deploy", managedServiceDropInName), []byte(managedServiceDropIn), 0o644)
	if err := validateManagedServiceDropInRuntime(releaseDir, paths); err != nil {
		t.Fatalf("managed release with exact active drop-in: %v", err)
	}
}

func TestRestoreProofFailureRevertsCredentialLinkAndResumeKeepsBothDatabases(t *testing.T) {
	fixture := newInstallFixture(t)
	installed, err := fixture.engine.Install(context.Background(), fixture.artifact)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	credentials, err := LoadDatabaseCredentials(fixture.paths)
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}
	backupPath := filepath.Join(fixture.paths.BackupsDir, "operator-selected.dump")
	backup, err := fixture.database.Backup(context.Background(), credentials, backupPath)
	if err != nil {
		t.Fatalf("create verified backup: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: statusTransport{status: http.StatusServiceUnavailable}}
	newDatabase := "xinsight_restore_field_trial"
	if _, err := fixture.engine.Restore(context.Background(), backup.Path, backup.SHA256, newDatabase, "field_validation"); err == nil || !strings.Contains(err.Error(), "restore_activation_proof_failed_database_link_restored") {
		t.Fatalf("expected restore proof failure, got %v", err)
	}
	activeAfterFailure, err := readAbsoluteSymlink(fixture.paths.DatabaseCurrentLink)
	if err != nil || activeAfterFailure != credentials.SetRef {
		t.Fatalf("failed restore did not revert credential link: active=%q expected=%q err=%v", activeAfterFailure, credentials.SetRef, err)
	}
	journal, err := fixture.engine.readActiveJournal()
	if err != nil || journal.Status != "failed" || journal.DatabaseSetAfter == "" || journal.DatabaseSetAfter == credentials.SetRef {
		t.Fatalf("failed restore journal=%+v err=%v", journal, err)
	}
	if !fixture.database.restoredDatabases[newDatabase] {
		t.Fatalf("restored database was not preserved: %v", fixture.database.restoredDatabases)
	}
	if _, err := os.Lstat(journal.DatabaseSetAfter); err != nil {
		t.Fatalf("staged restored credential set was not preserved: %v", err)
	}

	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: *installed.After}}
	resumed, err := fixture.engine.Resume(context.Background())
	if err != nil {
		t.Fatalf("resume restore: %v", err)
	}
	if resumed.Kind != "restore" || resumed.Status != "completed" || resumed.DatabaseSetRef != journal.DatabaseSetAfter {
		t.Fatalf("restore receipt=%+v", resumed)
	}
	activeAfterResume, err := readAbsoluteSymlink(fixture.paths.DatabaseCurrentLink)
	if err != nil || activeAfterResume != journal.DatabaseSetAfter {
		t.Fatalf("resumed restore did not activate new credential set: active=%q expected=%q err=%v", activeAfterResume, journal.DatabaseSetAfter, err)
	}
	if _, err := os.Lstat(credentials.SetRef); err != nil {
		t.Fatalf("old credential set must be preserved: %v", err)
	}
}

func TestLegacyConfigTransitionPreservesExactPriorFile(t *testing.T) {
	fixture := newInstallFixture(t)
	target, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify target: %v", err)
	}
	if err := fixture.engine.stageRelease(target); err != nil {
		t.Fatalf("stage target: %v", err)
	}
	legacy := []byte(
		"XINSIGHT_POSTGRES_DSN=postgres://legacy:legacy-password@127.0.0.1:5432/xinsight?sslmode=disable\n" +
			"XINSIGHT_MIGRATIONS_DIR=/opt/x-insight/current/migrations\n",
	)
	writeHostFile(t, fixture.paths.EnvironmentFile, legacy, 0o600)
	operationID := "20260714T000000Z-legacytransition"
	if err := fixture.engine.installRuntimeFiles(target.Identity.ReleaseDir, operationID, runtimeConfigReplaceLegacy); err != nil {
		t.Fatalf("replace legacy config: %v", err)
	}
	active, err := os.ReadFile(fixture.paths.EnvironmentFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(active), "POSTGRES_DSN") || validateManagedEnvironmentFile(fixture.paths.EnvironmentFile) != nil {
		t.Fatalf("active environment was not converted to managed no-secret form: %q", active)
	}
	backup := fixture.paths.EnvironmentFile + ".before-" + operationID
	assertFileContents(t, backup, legacy)
	info, err := os.Lstat(backup)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("preserved legacy environment mode=%v err=%v", info, err)
	}
}

func TestRolledBackLegacyBridgeAllowsReupgradeWithoutRestoringSecretEnvironment(t *testing.T) {
	fixture := newInstallFixture(t)
	installed, err := fixture.engine.Install(context.Background(), fixture.artifact)
	if err != nil {
		t.Fatalf("install managed bridge: %v", err)
	}
	if installed.After == nil {
		t.Fatal("installed release identity is missing")
	}
	state := InstallState{
		Current:  ReleaseIdentity{LegacyReleaseID: "legacy-test"},
		Previous: installed.After,
	}
	if err := fixture.engine.validateLegacyUpgradeRuntime(state); err != nil {
		t.Fatalf("validate managed legacy bridge: %v", err)
	}
	environment, err := os.ReadFile(fixture.paths.EnvironmentFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(environment), "POSTGRES_DSN") {
		t.Fatalf("managed bridge restored a secret environment: %q", environment)
	}
	state.Previous = nil
	if err := fixture.engine.validateLegacyUpgradeRuntime(state); err == nil {
		t.Fatal("expected managed environment without a verified bridge release to fail")
	}
}

func TestRolledBackLegacyBridgeAcceptsVerifiedHistoricalRC3WithoutManagedDropIn(t *testing.T) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install managed bridge: %v", err)
	}

	payload := filepath.Join(fixture.paths.Root, "artifacts", "payload-v0.2.0-rc.3")
	writeHostPayload(t, payload)
	if err := os.Remove(filepath.Join(payload, "deploy", managedServiceDropInName)); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(fixture.paths.Root, "artifacts", "release-config-v0.2.0-rc.3.json")
	writeHostJSON(t, configPath, releasebundle.ReleaseConfig{
		SchemaVersion:  releasebundle.ConfigSchemaVersion,
		ProductName:    "x-insight",
		ProductVersion: "0.2.0-rc.3",
		ReleaseChannel: "release_candidate",
		TrustReference: releasebundle.DefaultTrustReference,
		ApplicationCompatibility: releasebundle.ApplicationCompatibility{
			DatabaseSchemaMin: "1", DatabaseSchemaMax: "2",
			ConfigSchemaMin: "xinsight.config.v1", ConfigSchemaMax: "xinsight.config.v1",
		},
		DatabaseSchema:         releasebundle.SchemaCompatibility{Current: "1", Min: "1", Max: "2"},
		ConfigSchema:           releasebundle.SchemaCompatibility{Current: "xinsight.config.v1", Min: "xinsight.config.v1", Max: "xinsight.config.v1"},
		APIVersions:            []string{"xinsight.investigation.v1"},
		EvidenceSchemaVersions: []string{"m0.1"},
		ReceiptSchemaVersions:  []string{"xinsight.import-receipt.v1"},
		Manual:                 releasebundle.ManualIdentity{Language: "zh-CN", Version: "0.2.0-rc.3/manual-r3"},
		Rollback:               releasebundle.RollbackPolicy{ApplicationRollbackCompatible: true, Barriers: []string{"no_down_migration"}},
	}, 0o644)
	artifact := filepath.Join(fixture.paths.Root, "artifacts", "xinsight-v0.2.0-rc.3-linux-amd64.tar.gz")
	if _, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: fixture.privateKey, OutputPath: artifact,
		Commit: "3333333333333333333333333333333333333333", SourceDateEpoch: fixture.now.Add(-30 * time.Minute),
		BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
	}); err != nil {
		t.Fatalf("pack historical rc.3: %v", err)
	}
	previous, err := fixture.engine.verifyTarget(artifact)
	if err != nil {
		t.Fatalf("verify historical rc.3: %v", err)
	}
	if err := releasebundle.ExtractPayload(previous.Bundle, previous.Identity.ReleaseDir); err != nil {
		t.Fatalf("extract historical rc.3: %v", err)
	}
	if err := os.Remove(fixture.paths.ManagedServiceDropIn); err != nil {
		t.Fatal(err)
	}
	state := InstallState{
		Current:  ReleaseIdentity{LegacyReleaseID: "legacy-test"},
		Previous: &previous.Identity,
	}
	if err := fixture.engine.validateLegacyUpgradeRuntime(state); err != nil {
		t.Fatalf("validate historical rc.3 bridge: %v", err)
	}
}

func TestInstallEveryDurablePhaseBoundaryCanResume(t *testing.T) {
	phases := []string{
		"exclusive_maintenance",
		"stage_release",
		"migrate_database",
		"apply_role_grants",
		"install_runtime_files",
		"activate_release",
		"start_service",
		"verify_service_launcher",
		"verify_listener",
		"verify_product_read",
		"write_install_state",
		"enable_importer",
		"write_receipt",
	}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			fixture := newInstallFixture(t)
			t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", phase)
			if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err == nil {
				t.Fatalf("expected injected failure after %s", phase)
			}
			journal, err := fixture.engine.readActiveJournal()
			if err != nil || journal.Status != "failed" || !containsString(journal.CompletedPhases, phase) {
				t.Fatalf("phase=%s journal=%+v err=%v", phase, journal, err)
			}
			if !containsString(journal.AllowedActions, "resume") || !containsString(journal.AllowedActions, "recover") {
				t.Fatalf("phase=%s allowed_actions=%v", phase, journal.AllowedActions)
			}
			crossedBarrier := containsString(journal.CompletedPhases, "migrate_database") || containsString(journal.CompletedPhases, "activate_release")
			if containsString(journal.AllowedActions, "abort") == crossedBarrier {
				t.Fatalf("phase=%s barrier=%t allowed_actions=%v", phase, crossedBarrier, journal.AllowedActions)
			}
			t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
			receipt, err := fixture.engine.Resume(context.Background())
			if err != nil || receipt.Status != "completed" || receipt.Kind != "install" {
				t.Fatalf("phase=%s resume receipt=%+v err=%v", phase, receipt, err)
			}
		})
	}
}

func TestUpgradeEveryDurablePhaseBoundaryCanResume(t *testing.T) {
	phases := []string{
		"capture_feed_worker_state",
		"stop_feed_worker",
		"capture_litellm_worker_state",
		"stop_litellm_worker",
		"stop_service",
		"exclusive_maintenance",
		"stage_release",
		"backup_database",
		"migrate_database",
		"apply_role_grants",
		"install_runtime_files",
		"activate_release",
		"restart_service",
		"verify_service_launcher",
		"verify_listener",
		"verify_product_read",
		"write_install_state",
		"enable_importer",
		"restore_feed_worker_state",
		"restore_litellm_worker_state",
		"write_receipt",
	}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			fixture := newInstallFixture(t)
			if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
				t.Fatalf("install prerequisite: %v", err)
			}
			artifact := packUpgradeRelease(t, fixture)
			target, err := fixture.engine.verifyTarget(artifact)
			if err != nil {
				t.Fatalf("verify upgrade target: %v", err)
			}
			fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: target.Identity}}
			assertPhaseFailureCanResume(t, fixture, phase, "upgrade", func() (OperationReceipt, error) {
				return fixture.engine.Upgrade(context.Background(), artifact)
			})
			runner := fixture.engine.Runner.(*fakeRunner)
			if !runner.liteLLMWorkerLoaded ||
				runner.liteLLMWorkerActive ||
				runner.liteLLMWorkerEnabled {
				t.Fatalf(
					"phase=%s resumed target LiteLLM state loaded=%t active=%t enabled=%t",
					phase,
					runner.liteLLMWorkerLoaded,
					runner.liteLLMWorkerActive,
					runner.liteLLMWorkerEnabled,
				)
			}
			if _, err := os.Lstat(fixture.paths.LiteLLMServiceUnit); err != nil {
				t.Fatalf("phase=%s resumed target LiteLLM unit: %v", phase, err)
			}
		})
	}
}

func TestAdoptEveryDurablePhaseBoundaryCanResume(t *testing.T) {
	phases := []string{
		"stop_legacy_service",
		"exclusive_maintenance",
		"adopt_migration_checksums",
		"apply_role_grants",
		"write_install_state",
		"enable_importer",
		"write_receipt",
	}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			fixture, legacyArtifact := newLegacyAdoptFixture(t)
			assertPhaseFailureCanResume(t, fixture, phase, "adopt", func() (OperationReceipt, error) {
				return fixture.engine.Adopt(context.Background(), fixture.artifact, legacyArtifact)
			})
		})
	}
}

func TestRollbackEveryDurablePhaseBoundaryCanResume(t *testing.T) {
	phases := []string{
		"capture_feed_worker_state",
		"stop_feed_worker",
		"capture_litellm_worker_state",
		"stop_litellm_worker",
		"stop_service",
		"exclusive_maintenance",
		"activate_release",
		"install_feed_worker_runtime",
		"disable_unsupported_litellm_worker",
		"install_litellm_worker_runtime",
		"restart_service",
		"verify_service_launcher",
		"verify_listener",
		"verify_product_read",
		"write_install_state",
		"enable_importer",
		"restore_feed_worker_state",
		"restore_litellm_worker_state",
		"write_receipt",
	}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			fixture := newInstallFixture(t)
			installed, err := fixture.engine.Install(context.Background(), fixture.artifact)
			if err != nil {
				t.Fatalf("install prerequisite: %v", err)
			}
			artifact := packUpgradeRelease(t, fixture)
			target, err := fixture.engine.verifyTarget(artifact)
			if err != nil {
				t.Fatalf("verify upgrade target: %v", err)
			}
			fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: target.Identity}}
			if _, err := fixture.engine.Upgrade(context.Background(), artifact); err != nil {
				t.Fatalf("upgrade prerequisite: %v", err)
			}
			runner := fixture.engine.Runner.(*fakeRunner)
			runner.liteLLMWorkerLoaded = true
			runner.liteLLMWorkerActive = true
			runner.liteLLMWorkerEnabled = false
			rollbackIdentity := *installed.After
			rollbackIdentity.DatabaseSchema = target.Identity.DatabaseSchema
			fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: rollbackIdentity}}
			assertPhaseFailureCanResume(t, fixture, phase, "rollback", func() (OperationReceipt, error) {
				return fixture.engine.Rollback(context.Background(), "field_validation")
			})
			if runner.liteLLMWorkerLoaded ||
				runner.liteLLMWorkerActive ||
				runner.liteLLMWorkerEnabled {
				t.Fatalf(
					"phase=%s resumed unsupported LiteLLM state loaded=%t active=%t enabled=%t",
					phase,
					runner.liteLLMWorkerLoaded,
					runner.liteLLMWorkerActive,
					runner.liteLLMWorkerEnabled,
				)
			}
			if _, err := os.Lstat(fixture.paths.LiteLLMServiceUnit); !os.IsNotExist(err) {
				t.Fatalf("phase=%s resumed unsupported LiteLLM unit: %v", phase, err)
			}
		})
	}
}

func TestRestoreEveryDurablePhaseBoundaryCanResume(t *testing.T) {
	phases := []string{
		"capture_feed_worker_state",
		"stop_feed_worker",
		"capture_litellm_worker_state",
		"stop_litellm_worker",
		"stop_service",
		"exclusive_maintenance",
		"stage_restore_credentials",
		"restore_database",
		"switch_database_credentials",
		"restart_service",
		"verify_service_launcher",
		"verify_listener",
		"verify_product_read",
		"write_install_state",
		"enable_importer",
		"restore_feed_worker_state",
		"restore_litellm_worker_state",
		"write_receipt",
	}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			fixture := newInstallFixture(t)
			installed, err := fixture.engine.Install(context.Background(), fixture.artifact)
			if err != nil {
				t.Fatalf("install prerequisite: %v", err)
			}
			credentials, err := LoadDatabaseCredentials(fixture.paths)
			if err != nil {
				t.Fatalf("load credentials: %v", err)
			}
			backupPath := filepath.Join(fixture.paths.BackupsDir, "phase-boundary-restore.dump")
			backup, err := fixture.database.Backup(context.Background(), credentials, backupPath)
			if err != nil {
				t.Fatalf("create restore prerequisite: %v", err)
			}
			fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: *installed.After}}
			assertPhaseFailureCanResume(t, fixture, phase, "restore", func() (OperationReceipt, error) {
				return fixture.engine.Restore(
					context.Background(),
					backup.Path,
					backup.SHA256,
					"xinsight_restore_phase_boundary",
					"field_validation",
				)
			})
		})
	}
}

func assertPhaseFailureCanResume(
	t *testing.T,
	fixture installFixture,
	phase string,
	kind string,
	start func() (OperationReceipt, error),
) {
	t.Helper()
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", phase)
	if _, err := start(); err == nil {
		t.Fatalf("expected injected failure after %s", phase)
	}
	journal, err := fixture.engine.readActiveJournal()
	if err != nil || journal.Kind != kind || journal.Status != "failed" || !containsString(journal.CompletedPhases, phase) {
		t.Fatalf("kind=%s phase=%s journal=%+v err=%v", kind, phase, journal, err)
	}
	if !containsString(journal.AllowedActions, "resume") || !containsString(journal.AllowedActions, "recover") {
		t.Fatalf("kind=%s phase=%s allowed_actions=%v", kind, phase, journal.AllowedActions)
	}
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	receipt, err := fixture.engine.Resume(context.Background())
	if err != nil || receipt.Status != "completed" || receipt.Kind != kind || receipt.OperationID != journal.OperationID {
		t.Fatalf("kind=%s phase=%s resume receipt=%+v err=%v", kind, phase, receipt, err)
	}
}

type installFixture struct {
	engine     *Engine
	paths      Paths
	artifact   string
	privateKey string
	database   *fakeDatabase
	now        *time.Time
}

func newInstallFixture(t *testing.T) installFixture {
	t.Helper()
	root := persistentHostTestDir(t)
	t.Setenv(AllowTestRootEnv, "1")
	t.Setenv(TestRootEnv, root)
	t.Setenv("XINSIGHTCTL_TEST_HOST", "ubuntu-22.04-amd64")
	paths, err := PathsFromEnvironment()
	if err != nil {
		t.Fatalf("paths: %v", err)
	}
	stageDatabaseCredentials(t, paths)

	privateKey := filepath.Join(root, "artifacts", "signing-key.json")
	createdAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, paths.TrustStore, createdAt.Add(-time.Hour)); err != nil {
		t.Fatalf("generate release key: %v", err)
	}
	payload := filepath.Join(root, "artifacts", "payload")
	writeHostPayload(t, payload)
	configPath := filepath.Join(root, "artifacts", "release-config.json")
	writeHostJSON(t, configPath, releasebundle.ReleaseConfig{
		SchemaVersion:  releasebundle.ConfigSchemaVersion,
		ProductName:    "x-insight",
		ProductVersion: "0.2.0-rc.1",
		ReleaseChannel: "release_candidate",
		TrustReference: releasebundle.DefaultTrustReference,
		ApplicationCompatibility: releasebundle.ApplicationCompatibility{
			DatabaseSchemaMin: "1", DatabaseSchemaMax: "2",
			ConfigSchemaMin: "xinsight.config.v1", ConfigSchemaMax: "xinsight.config.v1",
		},
		DatabaseSchema:         releasebundle.SchemaCompatibility{Current: "1", Min: "1", Max: "2"},
		ConfigSchema:           releasebundle.SchemaCompatibility{Current: "xinsight.config.v1", Min: "xinsight.config.v1", Max: "xinsight.config.v1"},
		APIVersions:            []string{"xinsight.investigation.v1"},
		EvidenceSchemaVersions: []string{"m0.1"},
		ReceiptSchemaVersions:  []string{"xinsight.import-receipt.v1"},
		Manual:                 releasebundle.ManualIdentity{Language: "zh-CN", Version: "0.2.0-rc.1/manual-r1"},
		Rollback:               releasebundle.RollbackPolicy{ApplicationRollbackCompatible: true, Barriers: []string{"no_down_migration"}},
	}, 0o644)
	artifact := filepath.Join(root, "artifacts", "xinsight-v0.2.0-rc.1-linux-amd64.tar.gz")
	if _, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey, OutputPath: artifact,
		Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SourceDateEpoch: createdAt,
		BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
	}); err != nil {
		t.Fatalf("pack release: %v", err)
	}

	engine, err := NewEngine(paths)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	database := &fakeDatabase{facts: DatabaseFacts{PostgresMajor: 16, DatabaseName: "xinsight", AdminRoleIsSuperuser: true}}
	runner := &fakeRunner{}
	engine.Runner = runner
	engine.Database = database
	now := createdAt.Add(time.Hour)
	engine.Now = func() time.Time { return now }
	engine.Wait = func(context.Context, time.Duration) error { return nil }
	target, err := engine.verifyTarget(artifact)
	if err != nil {
		t.Fatalf("verify target: %v", err)
	}
	engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: target.Identity}}
	return installFixture{engine: engine, paths: paths, artifact: artifact, privateKey: privateKey, database: database, now: &now}
}

func packUpgradeRelease(t *testing.T, fixture installFixture) string {
	t.Helper()
	payload := filepath.Join(fixture.paths.Root, "artifacts", "payload-v0.2.0")
	writeHostPayload(t, payload)
	writeLiteLLMWorkerPayload(t, payload)
	writeHostFile(t, filepath.Join(payload, "migrations", "000002_expand_test.up.sql"), []byte("ALTER TABLE release_test ADD COLUMN note text;\n"), 0o644)
	configPath := filepath.Join(fixture.paths.Root, "artifacts", "release-config-v0.2.0.json")
	writeHostJSON(t, configPath, releasebundle.ReleaseConfig{
		SchemaVersion:                releasebundle.ConfigSchemaVersion,
		ProductName:                  "x-insight",
		ProductVersion:               "0.2.0",
		ReleaseChannel:               "release_candidate",
		TrustReference:               releasebundle.DefaultTrustReference,
		SupportedUpgradeFromVersions: []string{"0.2.0-rc.1"},
		ApplicationCompatibility: releasebundle.ApplicationCompatibility{
			DatabaseSchemaMin: "1", DatabaseSchemaMax: "2",
			ConfigSchemaMin: "xinsight.config.v1", ConfigSchemaMax: "xinsight.config.v1",
		},
		DatabaseSchema:         releasebundle.SchemaCompatibility{Current: "2", Min: "1", Max: "2"},
		ConfigSchema:           releasebundle.SchemaCompatibility{Current: "xinsight.config.v1", Min: "xinsight.config.v1", Max: "xinsight.config.v1"},
		APIVersions:            []string{"xinsight.investigation.v1"},
		EvidenceSchemaVersions: []string{"m0.1"},
		ReceiptSchemaVersions:  []string{"xinsight.import-receipt.v1"},
		Manual:                 releasebundle.ManualIdentity{Language: "zh-CN", Version: "0.2.0/manual-r1"},
		Rollback:               releasebundle.RollbackPolicy{ApplicationRollbackCompatible: true, Barriers: []string{"no_down_migration"}},
	}, 0o644)
	artifact := filepath.Join(fixture.paths.Root, "artifacts", "xinsight-v0.2.0-linux-amd64.tar.gz")
	if _, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: fixture.privateKey, OutputPath: artifact,
		Commit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SourceDateEpoch: fixture.now.Add(-30 * time.Minute),
		BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
	}); err != nil {
		t.Fatalf("pack upgrade release: %v", err)
	}
	return artifact
}

func newLegacyAdoptFixture(t *testing.T) (installFixture, string) {
	t.Helper()
	fixture := newInstallFixture(t)
	legacyArtifact, legacyFiles, legacySource := packLegacyRelease(t, fixture)

	legacyDirectory := filepath.Join(fixture.paths.ReleasesDir, "xinsight-release-linux-amd64-"+legacySource.Commit)
	for name, contents := range legacyFiles {
		mode := os.FileMode(0o644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0o755
		}
		writeHostFile(t, filepath.Join(legacyDirectory, filepath.FromSlash(name)), contents, mode)
	}
	if err := atomicSymlink(legacyDirectory, fixture.paths.CurrentLink); err != nil {
		t.Fatalf("legacy current link: %v", err)
	}
	writeHostFile(t, fixture.paths.ServiceUnit, []byte("[Service]\n"), 0o644)
	writeHostFile(t, fixture.paths.EnvironmentFile, []byte(
		"XINSIGHT_POSTGRES_DSN=postgres://legacy:legacy-password@127.0.0.1:5432/xinsight?sslmode=disable\n"+
			"XINSIGHT_MIGRATIONS_DIR=/opt/x-insight/current/migrations\n",
	), 0o600)
	fixture.database.facts.MigrationLedger = []db.MigrationRecord{{
		MigrationChecksum: db.MigrationChecksum{ID: legacySource.MigrationChecksums[0].ID},
	}}

	payload := filepath.Join(fixture.paths.Root, "artifacts", "payload")
	configPath := filepath.Join(fixture.paths.Root, "artifacts", "release-config-legacy-adopt.json")
	writeHostJSON(t, configPath, releasebundle.ReleaseConfig{
		SchemaVersion:          releasebundle.ConfigSchemaVersion,
		ProductName:            "x-insight",
		ProductVersion:         "0.2.0-rc.1",
		ReleaseChannel:         "release_candidate",
		TrustReference:         releasebundle.DefaultTrustReference,
		SupportedLegacySources: []releasebundle.LegacySource{legacySource},
		ApplicationCompatibility: releasebundle.ApplicationCompatibility{
			DatabaseSchemaMin: "1", DatabaseSchemaMax: "2",
			ConfigSchemaMin: "xinsight.config.v1", ConfigSchemaMax: "xinsight.config.v1",
		},
		DatabaseSchema:         releasebundle.SchemaCompatibility{Current: "1", Min: "1", Max: "2"},
		ConfigSchema:           releasebundle.SchemaCompatibility{Current: "xinsight.config.v1", Min: "xinsight.config.v1", Max: "xinsight.config.v1"},
		APIVersions:            []string{"xinsight.investigation.v1"},
		EvidenceSchemaVersions: []string{"m0.1"},
		ReceiptSchemaVersions:  []string{"xinsight.import-receipt.v1"},
		Manual:                 releasebundle.ManualIdentity{Language: "zh-CN", Version: "0.2.0-rc.1/manual-r1"},
		Rollback:               releasebundle.RollbackPolicy{ApplicationRollbackCompatible: true, Barriers: []string{"no_down_migration"}},
	}, 0o644)
	artifact := filepath.Join(fixture.paths.Root, "artifacts", "xinsight-v0.2.0-rc.1-legacy-adopt-linux-amd64.tar.gz")
	if _, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: fixture.privateKey, OutputPath: artifact,
		Commit: "dddddddddddddddddddddddddddddddddddddddd", SourceDateEpoch: fixture.now.Add(-15 * time.Minute),
		BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
	}); err != nil {
		t.Fatalf("pack legacy-adopt target: %v", err)
	}
	fixture.artifact = artifact
	return fixture, legacyArtifact
}

func packLegacyRelease(t *testing.T, fixture installFixture, migrationDirectory ...string) (string, map[string][]byte, releasebundle.LegacySource) {
	t.Helper()
	commit := "cccccccccccccccccccccccccccccccccccccccc"
	migrationID := "000001_create_test"
	migration := []byte("CREATE TABLE release_test (id bigint PRIMARY KEY);\n")
	migrationSHA := testSHA256Reference(migration)
	payload := map[string][]byte{
		"bin/xinsight-server":                  []byte("legacy-server\n"),
		"bin/xinsight-importer":                []byte("legacy-importer\n"),
		"migrations/000001_create_test.up.sql": migration,
	}
	migrationChecksums := []db.MigrationChecksum{{ID: migrationID, SHA256: migrationSHA}}
	schema := "1"
	if len(migrationDirectory) != 0 {
		var err error
		migrationChecksums, err = db.LoadMigrationChecksums(migrationDirectory[0])
		if err != nil {
			t.Fatal(err)
		}
		delete(payload, "migrations/000001_create_test.up.sql")
		for _, checksum := range migrationChecksums {
			name := checksum.ID + ".up.sql"
			raw, err := os.ReadFile(filepath.Join(migrationDirectory[0], name))
			if err != nil {
				t.Fatal(err)
			}
			payload["migrations/"+name] = raw
		}
		schema = strconv.Itoa(len(migrationChecksums))
	}

	fileNames := make([]string, 0, len(payload))
	for name := range payload {
		fileNames = append(fileNames, name)
	}
	sort.Strings(fileNames)
	manifestFiles := make([]legacyrelease.ManifestFile, 0, len(fileNames))
	for _, name := range fileNames {
		manifestFiles = append(manifestFiles, legacyrelease.ManifestFile{
			Path: name, SHA256: testSHA256Reference(payload[name]), SizeBytes: int64(len(payload[name])),
		})
	}
	manifest := legacyrelease.Manifest{
		SchemaVersion:   "xinsight.release-manifest.v1",
		Project:         "x-insight",
		Commit:          commit,
		SourceDateEpoch: fixture.now.Unix(),
		Toolchain:       legacyrelease.Toolchain{GoVersion: "go1.25.11"},
		Target:          legacyrelease.Target{OS: "linux", Arch: "amd64", CGOEnabled: false},
		Builder:         legacyrelease.Builder{OS: "test", Arch: "test"},
		Layout:          legacyrelease.Layout{TopLevel: []string{"bin", "migrations"}},
		Files:           manifestFiles,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes = append(manifestBytes, '\n')
	files := make(map[string][]byte, len(payload)+2)
	for name, contents := range payload {
		files[name] = contents
	}
	files["release-manifest.json"] = manifestBytes

	checksumNames := append(append([]string(nil), fileNames...), "release-manifest.json")
	sort.Strings(checksumNames)
	var checksums strings.Builder
	for _, name := range checksumNames {
		checksums.WriteString(strings.TrimPrefix(testSHA256Reference(files[name]), "sha256:"))
		checksums.WriteString("  ")
		checksums.WriteString(name)
		checksums.WriteByte('\n')
	}
	files["SHA256SUMS"] = []byte(checksums.String())

	root := "xinsight-release-linux-amd64-" + commit
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: root + "/", Typeflag: tar.TypeDir, Mode: 0o755,
		Uid: 0, Gid: 0, Uname: "root", Gname: "root",
	}); err != nil {
		t.Fatal(err)
	}
	archiveNames := make([]string, 0, len(files))
	for name := range files {
		archiveNames = append(archiveNames, name)
	}
	sort.Strings(archiveNames)
	for _, name := range archiveNames {
		mode := int64(0o644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0o755
		}
		contents := files[name]
		if err := tarWriter.WriteHeader(&tar.Header{
			Name: root + "/" + name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(contents)),
			Uid: 0, Gid: 0, Uname: "root", Gname: "root",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(fixture.paths.Root, "artifacts", "legacy-test.tar.gz")
	writeHostFile(t, artifact, archive.Bytes(), 0o644)
	source := releasebundle.LegacySource{
		ReleaseID:                    "legacy-phase-boundary-test",
		Commit:                       commit,
		ArtifactSHA256:               testSHA256Reference(archive.Bytes()),
		ManifestSHA256:               testSHA256Reference(manifestBytes),
		ReleaseLayout:                "xinsight.release-manifest.v1",
		DatabaseSchema:               schema,
		ConfigSchema:                 "xinsight.config.legacy-env.v1",
		ServiceLayout:                "phase1-immutable-current-symlink.v1",
		ApplicationDatabaseSchemaMin: schema,
		ApplicationDatabaseSchemaMax: "2",
		CompatibleConfigSchemas:      []string{"xinsight.config.v1"},
		MigrationChecksums:           migrationChecksums,
	}
	if len(migrationDirectory) != 0 {
		source.ApplicationDatabaseSchemaMax = schema
	}
	return artifact, files, source
}

func testSHA256Reference(contents []byte) string {
	digest := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func stageDatabaseCredentials(t *testing.T, paths Paths) {
	t.Helper()
	setRef := filepath.Join(paths.DatabaseSetsDir, "initial")
	if err := os.MkdirAll(setRef, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"postgres-reader.dsn":    "postgres://xinsight_reader:reader-password@127.0.0.1:5432/xinsight?sslmode=disable",
		"postgres-workspace.dsn": "postgres://xinsight_workspace:workspace-password@127.0.0.1:5432/xinsight?sslmode=disable",
		"postgres-importer.dsn":  "postgres://xinsight_importer:importer-password@127.0.0.1:5432/xinsight?sslmode=disable",
		"postgres-admin.dsn":     "postgres://xinsight_admin:admin-password@127.0.0.1:5432/xinsight?sslmode=disable",
	} {
		writeHostFile(t, filepath.Join(setRef, name), []byte(value+"\n"), 0o600)
	}
	if err := atomicSymlink(setRef, paths.DatabaseCurrentLink); err != nil {
		t.Fatalf("database current link: %v", err)
	}
}

func writeHostPayload(t *testing.T, root string) {
	t.Helper()
	for name, value := range map[string]string{
		"bin/xinsight-server":                          "server\n",
		"bin/xinsight-importer":                        "importer\n",
		"bin/xinsightctl":                              "ctl\n",
		"bin/xinsight-launcher":                        "launcher\n",
		"migrations/000001_create_test.up.sql":         "CREATE TABLE release_test (id bigint PRIMARY KEY);\n",
		"schemas/test.json":                            "{}\n",
		"schemas/access/local-accounts-v1.schema.json": "{}\n",
		"schemas/integration/feed-credential-registration-v1.schema.json": "{}\n",
		"fixtures/test.json":                       "{}\n",
		"manual/zh-CN/index.html":                  "<!doctype html><html lang=\"zh-CN\"></html>\n",
		"manual/zh-CN/manual.css":                  "body { color: #123; }\n",
		"manual/zh-CN/manual-icon.svg":             "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>\n",
		"release/compatibility-matrix.json":        "{}\n",
		"release/release-notes.md":                 "# Release notes\n",
		"deploy/xinsight.service":                  "[Service]\n",
		"deploy/xinsight-feed.service":             "[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed\n",
		"deploy/99-xinsight-managed-launcher.conf": managedServiceDropIn,
		"deploy/xinsight.env.example":              "XINSIGHT_MIGRATIONS_DIR=/opt/x-insight/current/migrations\n",
		"deploy/xinsight-connector.env.example":    "XINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://REPLACE_WITH_APPROVED_UNIROUTER_ORIGIN\n",
		"deploy/trusted-tls-proxy.env.example":     "XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://REPLACE_WITH_APPROVED_HOST_OR_IP\n",
		"deploy/xinsight-nginx-ip.conf.example":    "server_name REPLACE_WITH_PUBLIC_IP;\nproxy_pass http://127.0.0.1:8080;\n",
		"deploy/xinsight-feed.env.example":         "XINSIGHT_UNIROUTER_FEED_ENDPOINT=https://private.example.invalid/api/v1/integrations/xinsight/completed-executions\nXINSIGHT_UNIROUTER_FEED_POLL_INTERVAL=5s\nXINSIGHT_UNIROUTER_FEED_ALLOW_PRIVATE_TUNNEL_HTTP=false\nXINSIGHT_UNIROUTER_FEED_MAPPING_MODE=legacy\n",
		"deploy/postgres.compose.yml":              "services: {}\n",
		"deploy/postgres.env.example":              "POSTGRES_PASSWORD=REPLACE_WITH_FIELD_SECRET\n",
	} {
		writeHostFile(t, filepath.Join(root, filepath.FromSlash(name)), []byte(value), 0o644)
	}
}

func writeLiteLLMWorkerPayload(t *testing.T, root string) {
	t.Helper()
	for name, value := range map[string]string{
		"deploy/xinsight-litellm-feed.service": "[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer litellm pull-spend-logs\n",
		"deploy/xinsight-litellm-feed.env.example": "XINSIGHT_LITELLM_FEED_ENDPOINT=https://private.example.invalid/spend/logs/v2\n" +
			"XINSIGHT_LITELLM_FEED_POLL_INTERVAL=30s\n" +
			"XINSIGHT_LITELLM_FEED_SOURCE_INSTANCE=litellm-test\n" +
			"XINSIGHT_LITELLM_FEED_TENANT_REF=\n" +
			"XINSIGHT_LITELLM_FEED_ORG_REF=1\n" +
			"XINSIGHT_LITELLM_FEED_PROJECT_REF=2\n",
	} {
		writeHostFile(
			t,
			filepath.Join(root, filepath.FromSlash(name)),
			[]byte(value),
			0o644,
		)
	}
}

type fakeRunner struct {
	sourceFactsWorkerLoaded  bool
	sourceFactsWorkerActive  bool
	sourceFactsWorkerEnabled bool
	commands                 []Command
	mainServiceActive        bool
	mainServicePID           int64
	controllerLoaded         bool
	controllerActive         bool
	controllerEnabled        bool
	feedWorkerLoaded         bool
	feedWorkerActive         bool
	feedWorkerEnabled        bool
	liteLLMWorkerLoaded      bool
	liteLLMWorkerActive      bool
	liteLLMWorkerEnabled     bool
}

func (runner *fakeRunner) Run(_ context.Context, command Command) (CommandResult, error) {
	runner.commands = append(runner.commands, command)
	switch command.Name {
	case "systemctl":
		if reflect.DeepEqual(command.Args, []string{"show", "xinsight.service", "--property", "ActiveState", "--value"}) {
			if runner.mainServiceActive {
				return CommandResult{Output: "active\n"}, nil
			}
			return CommandResult{Output: "inactive\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", "xinsight.service", "--property", "MainPID", "--value"}) {
			return CommandResult{Output: strconv.FormatInt(runner.mainServicePID, 10) + "\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"stop", "xinsight.service"}) {
			runner.mainServiceActive = false
			runner.mainServicePID = 0
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", connectorControllerUnitName, "--property", "LoadState", "--value"}) {
			if runner.controllerLoaded {
				return CommandResult{Output: "loaded\n"}, nil
			}
			return CommandResult{Output: "not-found\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", connectorControllerUnitName, "--property", "ActiveState", "--value"}) {
			if runner.controllerActive {
				return CommandResult{Output: "active\n"}, nil
			}
			return CommandResult{Output: "inactive\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", connectorControllerUnitName, "--property", "UnitFileState", "--value"}) {
			if runner.controllerEnabled {
				return CommandResult{Output: "enabled\n"}, nil
			}
			return CommandResult{Output: "disabled\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"stop", connectorControllerUnitName}) {
			runner.controllerActive = false
			removeFakeControllerSocket()
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"start", connectorControllerUnitName}) {
			runner.controllerActive = true
			if err := createFakeControllerSocket(); err != nil {
				return CommandResult{}, err
			}
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"enable", "--now", connectorControllerUnitName}) {
			runner.controllerLoaded = true
			runner.controllerActive = true
			runner.controllerEnabled = true
			if err := createFakeControllerSocket(); err != nil {
				return CommandResult{}, err
			}
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"disable", "--now", connectorControllerUnitName}) {
			runner.controllerActive = false
			runner.controllerEnabled = false
			removeFakeControllerSocket()
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"enable", "--now", "xinsight.service"}) ||
			reflect.DeepEqual(command.Args, []string{"start", "xinsight.service"}) ||
			reflect.DeepEqual(command.Args, []string{"restart", "xinsight.service"}) {
			runner.mainServiceActive = true
			if runner.mainServicePID < 1000 {
				runner.mainServicePID = 1000
			} else {
				runner.mainServicePID++
			}
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"daemon-reload"}) {
			if paths, err := PathsFromEnvironment(); err == nil {
				_, controllerErr := os.Lstat(paths.ControllerServiceUnit)
				runner.controllerLoaded = controllerErr == nil
				_, feedErr := os.Lstat(paths.FeedServiceUnit)
				runner.feedWorkerLoaded = feedErr == nil
				_, sourceFactsErr := os.Lstat(paths.SourceFactsServiceUnit)
				runner.sourceFactsWorkerLoaded = sourceFactsErr == nil
				_, liteLLMErr := os.Lstat(paths.LiteLLMServiceUnit)
				runner.liteLLMWorkerLoaded = liteLLMErr == nil
			}
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", "xinsight.service", "--property", "ExecStart", "--value"}) {
			return CommandResult{Output: "{ path=" + runnerServiceLauncher() + " ; argv[]=" + runnerServiceLauncher() + " ; }\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", sourceFactsWorkerUnit, "--property", "LoadState", "--value"}) {
			if runner.sourceFactsWorkerLoaded {
				return CommandResult{Output: "loaded\n"}, nil
			}
			return CommandResult{Output: "not-found\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", sourceFactsWorkerUnit, "--property", "ActiveState", "--value"}) {
			if runner.sourceFactsWorkerActive {
				return CommandResult{Output: "active\n"}, nil
			}
			return CommandResult{Output: "inactive\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", sourceFactsWorkerUnit, "--property", "UnitFileState", "--value"}) {
			if runner.sourceFactsWorkerEnabled {
				return CommandResult{Output: "enabled\n"}, nil
			}
			return CommandResult{Output: "disabled\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"stop", sourceFactsWorkerUnit}) {
			runner.sourceFactsWorkerActive = false
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"start", sourceFactsWorkerUnit}) ||
			reflect.DeepEqual(command.Args, []string{"restart", sourceFactsWorkerUnit}) {
			runner.sourceFactsWorkerLoaded = true
			runner.sourceFactsWorkerActive = true
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"disable", sourceFactsWorkerUnit}) {
			runner.sourceFactsWorkerActive = false
			runner.sourceFactsWorkerEnabled = false
			return CommandResult{}, nil
		}

		if reflect.DeepEqual(command.Args, []string{"enable", sourceFactsWorkerUnit}) {
			runner.sourceFactsWorkerEnabled = true
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", feedWorkerUnitName, "--property", "LoadState", "--value"}) {
			if runner.feedWorkerLoaded {
				return CommandResult{Output: "loaded\n"}, nil
			}
			return CommandResult{Output: "not-found\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", feedWorkerUnitName, "--property", "ActiveState", "--value"}) {
			if runner.feedWorkerActive {
				return CommandResult{Output: "active\n"}, nil
			}
			return CommandResult{Output: "inactive\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", feedWorkerUnitName, "--property", "UnitFileState", "--value"}) {
			if runner.feedWorkerEnabled {
				return CommandResult{Output: "enabled\n"}, nil
			}
			return CommandResult{Output: "disabled\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"stop", feedWorkerUnitName}) {
			runner.feedWorkerActive = false
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"start", feedWorkerUnitName}) ||
			reflect.DeepEqual(command.Args, []string{"restart", feedWorkerUnitName}) {
			runner.feedWorkerLoaded = true
			runner.feedWorkerActive = true
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"disable", feedWorkerUnitName}) {
			runner.feedWorkerActive = false
			runner.feedWorkerEnabled = false
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", liteLLMWorkerUnitName, "--property", "LoadState", "--value"}) {
			if runner.liteLLMWorkerLoaded {
				return CommandResult{Output: "loaded\n"}, nil
			}
			return CommandResult{Output: "not-found\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", liteLLMWorkerUnitName, "--property", "ActiveState", "--value"}) {
			if runner.liteLLMWorkerActive {
				return CommandResult{Output: "active\n"}, nil
			}
			return CommandResult{Output: "inactive\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"show", liteLLMWorkerUnitName, "--property", "UnitFileState", "--value"}) {
			if runner.liteLLMWorkerEnabled {
				return CommandResult{Output: "enabled\n"}, nil
			}
			return CommandResult{Output: "disabled\n"}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"stop", liteLLMWorkerUnitName}) {
			runner.liteLLMWorkerActive = false
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"start", liteLLMWorkerUnitName}) ||
			reflect.DeepEqual(command.Args, []string{"restart", liteLLMWorkerUnitName}) {
			runner.liteLLMWorkerLoaded = true
			runner.liteLLMWorkerActive = true
			return CommandResult{}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"disable", liteLLMWorkerUnitName}) {
			paths, err := PathsFromEnvironment()
			if err != nil {
				return CommandResult{}, err
			}
			info, err := os.Lstat(paths.LiteLLMServiceUnit)
			if err != nil || !info.Mode().IsRegular() {
				return CommandResult{}, fmt.Errorf(
					"LiteLLM unit must exist as a regular file before disable",
				)
			}
			runner.liteLLMWorkerActive = false
			runner.liteLLMWorkerEnabled = false
			return CommandResult{}, nil
		}
		return CommandResult{Output: "systemd 249\n"}, nil
	case "ss":
		if reflect.DeepEqual(command.Args, []string{"-ltnH"}) {
			return CommandResult{Output: "LISTEN 0 4096 127.0.0.1:8080 0.0.0.0:*\n"}, nil
		}
		return CommandResult{Output: "ss utility, iproute2\n"}, nil
	case "docker":
		return CommandResult{Output: "Docker Compose version v2.39.1\n"}, nil
	case "pg_dump", "pg_restore", "createdb":
		return CommandResult{Output: command.Name + " (PostgreSQL) 16.14\n"}, nil
	default:
		return CommandResult{}, fmt.Errorf("unexpected command %s", command.Name)
	}
}

func createFakeControllerSocket() error {
	paths, err := PathsFromEnvironment()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(paths.ControllerSocket), 0o755); err != nil {
		return err
	}
	removeFakeControllerSocket()
	bindPath := paths.ControllerSocket
	if len(bindPath) > 90 {
		digest := sha256.Sum256([]byte(bindPath))
		bindPath = fmt.Sprintf("/tmp/xic-%x.sock", digest[:8])
		_ = os.Remove(bindPath)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: bindPath, Net: "unix"})
	if err != nil {
		return err
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(bindPath, 0o660); err != nil {
		_ = listener.Close()
		return err
	}
	if err := listener.Close(); err != nil {
		return err
	}
	if bindPath != paths.ControllerSocket {
		if err := os.Link(bindPath, paths.ControllerSocket); err != nil {
			_ = os.Remove(bindPath)
			return err
		}
		if err := os.Remove(bindPath); err != nil {
			return err
		}
	}
	return nil
}

func removeFakeControllerSocket() {
	paths, err := PathsFromEnvironment()
	if err != nil {
		return
	}
	_ = os.Remove(paths.ControllerSocket)
}

func runnerServiceLauncher() string {
	paths, err := PathsFromEnvironment()
	if err != nil {
		return "/invalid/xinsight-launcher"
	}
	return paths.ServiceLauncher
}

type fakeDatabase struct {
	custodyInventoryErr      error
	facts                    DatabaseFacts
	importerLogin            bool
	loginTransitions         []bool
	backupCount              int
	restoredDatabases        map[string]bool
	durableCursor            string
	applyGrantsCount         int
	roleBoundaryChecks       int
	roleBoundaryErr          error
	sourceRoleBoundaryChecks int
	sourceRoleBoundaryErr    error
	sourceRoleVersions       []string
	convergeRoleBoundary     bool
	reviewBodyRestoreErr     error
	reviewBodyRestoreChecks  int
}

func (database *fakeDatabase) VerifyReviewBodyInventory(context.Context, DatabaseCredentials) error {
	return database.custodyInventoryErr
}

func (database *fakeDatabase) Inspect(context.Context, DatabaseCredentials) (DatabaseFacts, error) {
	copy := database.facts
	copy.MigrationLedger = append([]db.MigrationRecord(nil), database.facts.MigrationLedger...)
	return copy, nil
}

func (database *fakeDatabase) EnsureRoles(_ context.Context, credentials DatabaseCredentials) error {
	database.facts.ReaderRoleExists = true
	if credentials.Workspace != nil {
		database.facts.WorkspaceRoleExists = true
	}
	database.facts.ImporterRoleExists = true
	database.importerLogin = true
	return nil
}

func (database *fakeDatabase) ApplyGrants(context.Context, DatabaseCredentials) error {
	database.applyGrantsCount++
	if database.convergeRoleBoundary {
		database.roleBoundaryErr = nil
	}
	return nil
}
func (database *fakeDatabase) VerifyRoleBoundaries(context.Context, DatabaseCredentials) error {
	database.roleBoundaryChecks++
	return database.roleBoundaryErr
}
func (database *fakeDatabase) VerifyUpgradeSourceRoleBoundaries(
	_ context.Context,
	_ DatabaseCredentials,
	productVersion string,
) error {
	database.sourceRoleBoundaryChecks++
	database.sourceRoleVersions = append(database.sourceRoleVersions, productVersion)
	return database.sourceRoleBoundaryErr
}
func (*fakeDatabase) EvidenceFingerprint(context.Context, DatabaseCredentials) (EvidenceFingerprint, error) {
	return EvidenceFingerprint{SHA256: "sha256:" + strings.Repeat("0", 64)}, nil
}
func (database *fakeDatabase) DatabaseExists(_ context.Context, _ DatabaseCredentials, name string) (bool, error) {
	return database.restoredDatabases[name], nil
}
func (database *fakeDatabase) AcquireMaintenance(context.Context, DatabaseCredentials) (Maintenance, error) {
	return &fakeMaintenance{database: database}, nil
}
func (database *fakeDatabase) Backup(_ context.Context, _ DatabaseCredentials, path string) (BackupResult, error) {
	if err := ensureDirectory(filepath.Dir(path), 0o700); err != nil {
		return BackupResult{}, err
	}
	database.backupCount++
	contents := []byte(fmt.Sprintf("fake-postgresql-16-backup-%d\n", database.backupCount))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return BackupResult{}, err
	}
	if err := writeSyncClose(file, contents); err != nil {
		return BackupResult{}, err
	}
	digest, size, err := fileSHA256(path, maxBackupBytes)
	if err != nil {
		return BackupResult{}, err
	}
	return BackupResult{Path: path, SHA256: digest, SizeBytes: size, PGDumpMajor: 16, PGRestoreMajor: 16}, nil
}
func (*fakeDatabase) VerifyBackup(_ context.Context, path string, expected string) (BackupResult, error) {
	digest, size, err := fileSHA256(path, maxBackupBytes)
	if err != nil || digest != expected {
		return BackupResult{}, fmt.Errorf("fake backup verification failed")
	}
	return BackupResult{Path: path, SHA256: digest, SizeBytes: size, PGRestoreMajor: 16}, nil
}
func (database *fakeDatabase) RestoreToNewDatabase(_ context.Context, _ DatabaseCredentials, restored DatabaseCredentials, _ string, _ string, _ []db.MigrationChecksum) error {
	if database.restoredDatabases == nil {
		database.restoredDatabases = make(map[string]bool)
	}
	if database.restoredDatabases[restored.Admin.Database] {
		return fmt.Errorf("restore target already exists")
	}
	database.restoredDatabases[restored.Admin.Database] = true
	return nil
}

func (database *fakeDatabase) VerifyReviewBodyRestore(context.Context, DatabaseCredentials, reviewbody.KeyRing, time.Time) error {
	database.reviewBodyRestoreChecks++
	return database.reviewBodyRestoreErr
}

type fakeMaintenance struct {
	database *fakeDatabase
}

func (maintenance *fakeMaintenance) RunMigrations(_ context.Context, directory string) (int, error) {
	checksums, err := db.LoadMigrationChecksums(directory)
	if err != nil {
		return 0, err
	}
	maintenance.database.facts.MigrationLedger = make([]db.MigrationRecord, len(checksums))
	for index, checksum := range checksums {
		maintenance.database.facts.MigrationLedger[index] = db.MigrationRecord{MigrationChecksum: checksum}
	}
	maintenance.database.facts.PublicTableCount = len(checksums) + 1
	return len(checksums), nil
}

func (maintenance *fakeMaintenance) AdoptLegacyMigrationChecksums(_ context.Context, directory string, expected []db.MigrationChecksum) (int, error) {
	available, err := db.LoadMigrationChecksums(directory)
	if err != nil || !reflect.DeepEqual(available, expected) || len(maintenance.database.facts.MigrationLedger) != len(expected) {
		return 0, fmt.Errorf("legacy migration adoption mismatch")
	}
	for index, migration := range expected {
		if maintenance.database.facts.MigrationLedger[index].ID != migration.ID {
			return 0, fmt.Errorf("legacy migration adoption order mismatch")
		}
		maintenance.database.facts.MigrationLedger[index].SHA256 = migration.SHA256
	}
	return len(expected), nil
}
func (*fakeMaintenance) CountOtherSessions(context.Context, []string) (int, error) { return 0, nil }
func (maintenance *fakeMaintenance) SetRoleLogin(_ context.Context, _ string, enabled bool) error {
	maintenance.database.importerLogin = enabled
	maintenance.database.loginTransitions = append(maintenance.database.loginTransitions, enabled)
	return nil
}
func (*fakeMaintenance) Close() error { return nil }

type fixedProductTransport struct {
	identity        ReleaseIdentity
	accessProtected bool
}

type statusTransport struct {
	status int
}

type legacyReadyTransport struct{}

func (legacyReadyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path != "/readyz" {
		return nil, fmt.Errorf("unexpected legacy proof URL %s", request.URL.String())
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"status":"ready","db":"ok"}`)),
		Request:    request,
	}, nil
}

func (transport statusTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: transport.status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"error":"injected"}`)),
		Request:    request,
	}, nil
}

func (transport fixedProductTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body []byte
	switch request.URL.Path {
	case "/readyz":
		body, _ = json.Marshal(map[string]any{
			"status": "ready", "product_version": transport.identity.ProductVersion,
			"database_schema": transport.identity.DatabaseSchema, "postgres_major": 16,
			"manual_version":      transport.identity.ManualVersion,
			"representative_read": "ok",
		})
	case "/api/v1/executions":
		if transport.accessProtected {
			body = []byte(
				`{"type":"urn:xinsight:error:authentication_required",` +
					`"title":"Authentication is required","status":401,` +
					`"code":"authentication_required","correlation_id":"xc_test",` +
					`"retryable":false}`,
			)
		} else {
			body = []byte(`{"items":[]}`)
		}
	default:
		return nil, fmt.Errorf("unexpected proof URL %s", request.URL.String())
	}
	header := make(http.Header)
	status := http.StatusOK
	if request.URL.Path == "/api/v1/executions" && transport.accessProtected {
		status = http.StatusUnauthorized
		header.Set("Content-Type", "application/problem+json")
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    request,
	}, nil
}

func persistentHostTestDir(t *testing.T) string {
	t.Helper()
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.TempDir(), "xinsight-hostctl-test-"+hex.EncodeToString(random))
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeHostFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
}

func writeHostJSON(t *testing.T, path string, value any, mode os.FileMode) {
	t.Helper()
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeHostFile(t, path, append(contents, '\n'), mode)
}

func assertFileContents(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("file %s changed: actual=%q expected=%q", path, string(actual), string(expected))
	}
}

func (database *fakeDatabase) SuspendForeignRestoredAuthority(context.Context, DatabaseCredentials, string, string, time.Time) error {
	return nil
}

func (database *fakeDatabase) ImporterRoleLogin(context.Context, DatabaseCredentials) (bool, error) {
	return database.importerLogin, nil
}
