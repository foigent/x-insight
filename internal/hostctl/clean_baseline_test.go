package hostctl

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/db"
)

func TestCleanBaselineTableContractIsExactAndSorted(t *testing.T) {
	if len(cleanBaselineTables) != 108 || !sort.StringsAreSorted(cleanBaselineTables) {
		t.Fatalf("clean-baseline tables=%v", cleanBaselineTables)
	}
	var contractTables []string
	for _, entry := range ExpectedCleanBaselineSchemaContract().Entries {
		if entry.Kind == "T" && entry.Namespace == "public" {
			contractTables = append(contractTables, entry.Owner)
		}
	}
	sort.Strings(contractTables)
	if !slices.Equal(cleanBaselineTables, contractTables) {
		t.Fatalf("clean-baseline inventory differs from exact schema contract: inventory=%v contract=%v", cleanBaselineTables, contractTables)
	}
	expected := map[string]bool{
		"source_budget_collections": true, "source_budget_catalogs": true, "source_budget_catalog_entries": true, "source_budget_readings": true, "source_budget_receipts": true, "source_budget_offline_authorizations": true, "source_budget_offline_imports": true, "budget_readings": true, "budget_reading_members": true,
		"workbench_purpose_revisions": true, "workbench_purpose_members": true, "workbench_purpose_manifests": true,
		"investigation_case_scope_labels": true,
		"source_fact_management_events":   true, "source_fact_offline_authorizations": true,
		"source_fact_documents": true, "source_fact_streams": true,
		"work_event_collections": true, "work_event_documents": true, "work_event_relations": true,
		"work_event_ingest_bindings": true, "work_event_install_invitations": true, "work_event_ur_targets": true,
		"review_capture_defaults": true, "review_capture_exceptions": true,
		"review_capture_project_bindings": true, "review_capture_projects": true,
		"review_capture_ur_operations": true,
		"review_body_ingest_bindings":  true, "review_body_project_usage": true,
		"review_body_operations": true, "review_body_objects": true, "review_body_copy_attempts": true,
		"workbench_source_manifests": true, "workbench_source_members": true,
		"case_finding_actions": true, "case_finding_followups": true,
		"case_finding_assessments": true, "case_finding_assessment_actions": true, "finding_review_creations": true,
		"workbench_access_grants": true, "workbench_economic_facts": true, "workbench_reading_members": true, "workbench_readings": true, "workbench_saved_filter_operations": true, "workbench_saved_filters": true,
		"schema_migrations":                       true,
		"source_systems":                          true,
		"adapter_manifests":                       true,
		"adapter_import_states":                   true,
		"evidence_events":                         true,
		"capture_gaps":                            true,
		"import_provenance":                       true,
		"ingestion_quarantine":                    true,
		"authoritative_sessions":                  true,
		"connector_checkpoint_epoch_operations":   true,
		"connector_checkpoint_epoch_history":      true,
		"connector_installations":                 true,
		"connector_installation_source_bindings":  true,
		"connector_receiver_adoptions":            true,
		"connector_source_enrollments":            true,
		"connector_installation_invitations":      true,
		"connector_control_operations":            true,
		"connector_control_credentials":           true,
		"connector_control_credential_operations": true,
		"connector_control_recovery_tickets":      true,
	}
	for table := range expected {
		index := sort.SearchStrings(cleanBaselineTables, table)
		if index == len(cleanBaselineTables) || cleanBaselineTables[index] != table {
			t.Fatalf("required table is unclassified: %s", table)
		}
	}
}

func TestCleanBaselineCredentialRetirementIsMetadataOnlyCrashSafeAndFinal(t *testing.T) {
	fixture := newInstallFixture(t)
	values := map[string][]byte{
		fixture.paths.FeedCredentialFile:  []byte("synthetic-active-receiver-credential"),
		fixture.paths.FeedCredentialNext:  []byte("synthetic-staged-receiver-credential"),
		fixture.paths.FeedCredentialState: []byte(`{"schema_version":"synthetic-operation-state"}`),
		fixture.paths.LiteLLMCredential:   []byte("synthetic-litellm-source-credential"),
		fixture.paths.LiteLLMEnvironment: []byte(
			"XINSIGHT_LITELLM_FEED_SOURCE_INSTANCE=synthetic-old-campaign\n" +
				"XINSIGHT_LITELLM_FEED_ORG_REF=synthetic-org\n" +
				"XINSIGHT_LITELLM_FEED_PROJECT_REF=synthetic-project\n",
		),
	}
	for path, value := range values {
		writeHostFile(t, path, value, 0o600)
	}
	preview, err := inspectCleanBaselineCredentialPreview(fixture.paths)
	if err != nil || !preview.ActivePresent || !preview.StagedPresent ||
		!preview.OperationStatePresent || !preview.LiteLLMCredentialPresent ||
		!preview.LiteLLMEnvironmentPresent || preview.CanonicalFilesToRetire != 5 {
		t.Fatalf("credential preview=%+v err=%v", preview, err)
	}
	payload, err := marshalJSON(preview)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if strings.Contains(string(payload), string(value)) {
			t.Fatal("credential preview exposed credential bytes")
		}
	}
	const operationID = "20260805T030000Z-abcdef123456"
	if err := quarantineCleanBaselineCredentialState(fixture.paths, operationID, preview); err != nil {
		t.Fatal(err)
	}
	if err := proveCleanBaselineCredentialAbsent(fixture.paths, operationID, false); err != nil {
		t.Fatal(err)
	}
	if err := restoreCleanBaselineCredentialState(fixture.paths, operationID, preview); err != nil {
		t.Fatal(err)
	}
	for path, expected := range values {
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(expected) {
			t.Fatalf("credential rollback path=%s err=%v", filepath.Base(path), err)
		}
	}
	if err := quarantineCleanBaselineCredentialState(fixture.paths, operationID, preview); err != nil {
		t.Fatal(err)
	}
	if err := retireCleanBaselineCredentialQuarantine(fixture.paths, operationID, preview); err != nil {
		t.Fatal(err)
	}
	if err := proveCleanBaselineCredentialAbsent(fixture.paths, operationID, true); err != nil {
		t.Fatal(err)
	}
}

func TestCleanBaselineCredentialRollbackFailsClosedWhenPredecessorIsMissing(t *testing.T) {
	fixture := newInstallFixture(t)
	writeHostFile(t, fixture.paths.FeedCredentialFile, []byte("synthetic-active-predecessor"), 0o600)
	preview, err := inspectCleanBaselineCredentialPreview(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	const operationID = "20260805T030001Z-abcdef123456"
	if err := quarantineCleanBaselineCredentialState(fixture.paths, operationID, preview); err != nil {
		t.Fatal(err)
	}
	quarantine := cleanBaselineCredentialQuarantinePath(fixture.paths.FeedCredentialFile, operationID)
	if err := os.Remove(quarantine); err != nil {
		t.Fatal(err)
	}
	if err := restoreCleanBaselineCredentialState(fixture.paths, operationID, preview); err == nil {
		t.Fatal("credential rollback accepted a missing authorized predecessor")
	}
}

func TestCleanBaselineCredentialPreviewRejectsForeignQuarantineState(t *testing.T) {
	fixture := newInstallFixture(t)
	foreign := cleanBaselineCredentialQuarantinePath(
		fixture.paths.LiteLLMEnvironment,
		"20260805T020000Z-abcdef123456",
	)
	writeHostFile(t, foreign, []byte("synthetic-foreign-quarantine"), 0o600)
	if _, err := inspectCleanBaselineCredentialPreview(fixture.paths); err == nil {
		t.Fatal("credential preview accepted foreign quarantine state")
	}
}

func TestCleanBaselineDurableEvidenceRequiresStrictEmptyCheckpointAndCredential(t *testing.T) {
	const digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	backupRestore := testCleanBaselineBackupRestoreProof(
		t,
		digest,
		"xinsight_restore_data_reset_durable",
	)
	floor, err := sealCleanBaselineCheckpointFloor(CleanBaselineCheckpointFloor{
		Binding:              cleanBaselineBinding(),
		PriorIdentitySHA256:  digest,
		RollbackBackupSHA256: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence := &CleanBaselineDurableEvidence{
		SchemaVersion:             CleanBaselineReceiptSchema,
		PreviewSHA256:             digest,
		RollbackBackupSHA256:      digest,
		RollbackDatabaseSetSHA256: digest,
		ActiveDatabaseSetSHA256:   digest,
		PredecessorDatabaseSHA256: hashCleanBaselineText("xinsight"),
		BackupRestore:             backupRestore,
		CheckpointFloor:           floor,
		Credential: CleanBaselineCredentialProof{
			ActiveAbsent: true, StagedAbsent: true, OperationStateAbsent: true,
			LiteLLMCredentialAbsent: true, LiteLLMEnvironmentAbsent: true,
		},
		Database: CleanBaselineDatabaseProof{
			PostgresMajor:         16,
			MigrationCount:        expectedCleanBaselineSchemaContract.MigrationCount,
			SchemaContractVersion: CleanBaselineSchemaContractVersion,
			SchemaContractSHA256:  expectedCleanBaselineSchemaContract.SemanticSHA256,
			SchemaLayoutSHA256:    expectedCleanBaselineSchemaContract.ColumnLayoutSHA256,
			SchemaLayoutMatches:   true,
			MigrationSetSHA256:    expectedCleanBaselineSchemaContract.MigrationSetSHA256,
			SourceCatalogRows:     1,
			AdapterCatalogRows:    2,
		},
	}
	if err := validateCleanBaselineDurableEvidence(evidence, digest); err != nil {
		t.Fatal(err)
	}
	withState := *evidence
	withState.Database.CheckpointStateRows = 1
	if err := validateCleanBaselineDurableEvidence(&withState, digest); err == nil {
		t.Fatal("DATA_RESET receipt accepted checkpoint state")
	}
	withCredential := *evidence
	withCredential.Credential.ActiveAbsent = false
	if err := validateCleanBaselineDurableEvidence(&withCredential, digest); err == nil {
		t.Fatal("DATA_RESET receipt accepted active UniRouter credential state")
	}
	withLiteLLMConfig := *evidence
	withLiteLLMConfig.Credential.LiteLLMEnvironmentAbsent = false
	if err := validateCleanBaselineDurableEvidence(&withLiteLLMConfig, digest); err == nil {
		t.Fatal("DATA_RESET receipt accepted a LiteLLM campaign environment")
	}
	tamperedFloor := *evidence
	tamperedFloor.CheckpointFloor.HistoricalMaxEpoch++
	if err := validateCleanBaselineDurableEvidence(&tamperedFloor, digest); err == nil {
		t.Fatal("DATA_RESET receipt accepted a checkpoint floor not sealed to the backup")
	}
	missingRestore := *evidence
	missingRestore.BackupRestore = CleanBaselineBackupRestoreProof{}
	if err := validateCleanBaselineDurableEvidence(&missingRestore, digest); err == nil {
		t.Fatal("DATA_RESET receipt accepted a missing isolated backup restore proof")
	}
	tamperedRestore := *evidence
	tamperedRestore.BackupRestore.MigrationCount++
	if err := validateCleanBaselineDurableEvidence(&tamperedRestore, digest); err == nil {
		t.Fatal("DATA_RESET receipt accepted a tampered isolated backup restore proof")
	}
}

func TestCleanBaselineAbortRestoresDatabaseAccountAndDormantWorkers(t *testing.T) {
	fixture, originalAccounts, successor, preview := newCleanBaselineRecoveryFixture(t)
	currentCredentials, err := LoadDatabaseCredentials(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	targetCredentials, err := StageCleanBaselineCredentialSet(
		fixture.paths,
		currentCredentials,
		"xinsight_clean_abort_test",
	)
	if err != nil {
		t.Fatal(err)
	}
	state, err := fixture.engine.readInstallState()
	if err != nil {
		t.Fatal(err)
	}
	before := state.Current
	journal, err := fixture.engine.startJournal(
		"clean_baseline_data_reset",
		before.ArtifactPath,
		"",
		&before,
		&before,
	)
	if err != nil {
		t.Fatal(err)
	}
	journal.DatabaseSetBefore = currentCredentials.SetRef
	journal.DatabaseSetAfter = targetCredentials.SetRef
	journal.CleanDatabase = targetCredentials.Admin.Database
	journal.CleanPredecessorDatabase = currentCredentials.Admin.Database
	journal.CleanRestoreDatabase = cleanBaselineRestoreDatabaseName(journal.OperationID)
	journal.CleanRestoreDatabaseOwned = true
	journal.CleanPriorAccountSHA256 = preview.PredecessorConfigSHA256
	journal.CleanAccountSHA256 = preview.SuccessorConfigSHA256
	backupSHA256 := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	floor, err := sealCleanBaselineCheckpointFloor(CleanBaselineCheckpointFloor{
		Binding:              cleanBaselineBinding(),
		PriorIdentitySHA256:  backupSHA256,
		RollbackBackupSHA256: backupSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	databaseProof := CleanBaselineDatabaseProof{
		PostgresMajor:         16,
		MigrationCount:        expectedCleanBaselineSchemaContract.MigrationCount,
		SchemaContractVersion: CleanBaselineSchemaContractVersion,
		SchemaContractSHA256:  expectedCleanBaselineSchemaContract.SemanticSHA256,
		SchemaLayoutSHA256:    expectedCleanBaselineSchemaContract.ColumnLayoutSHA256,
		SchemaLayoutMatches:   true,
		MigrationSetSHA256:    expectedCleanBaselineSchemaContract.MigrationSetSHA256,
		SourceCatalogRows:     1,
		AdapterCatalogRows:    2,
	}
	verificationPresent := true
	fixture.engine.CleanBaseline = cleanBaselineManagerStub{
		databaseProof:       databaseProof,
		verificationPresent: &verificationPresent,
	}
	journal.BackupPath = "/var/backups/x-insight/test.dump"
	journal.BackupSHA256 = backupSHA256
	journal.CleanCheckpointFloorSHA256 = floor.SealedSHA256
	journal.ReasonCode = cleanBaselineReasonCode
	journal.Phase = "clean_database_readback"
	credentialValues := map[string][]byte{
		fixture.paths.FeedCredentialFile:  []byte("synthetic-active-before-abort"),
		fixture.paths.FeedCredentialNext:  []byte("synthetic-staged-before-abort"),
		fixture.paths.FeedCredentialState: []byte(`{"schema_version":"synthetic-before-abort"}`),
		fixture.paths.LiteLLMCredential:   []byte("synthetic-litellm-credential-before-abort"),
		fixture.paths.LiteLLMEnvironment:  []byte("XINSIGHT_LITELLM_FEED_SOURCE_INSTANCE=synthetic-before-abort\n"),
	}
	for path, value := range credentialValues {
		writeHostFile(t, path, value, 0o600)
	}
	credentialPreview, err := inspectCleanBaselineCredentialPreview(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := quarantineCleanBaselineCredentialState(fixture.paths, journal.OperationID, credentialPreview); err != nil {
		t.Fatal(err)
	}
	journal.CleanCredentialPreview = &credentialPreview
	journal.CompletedPhases = []string{
		"stop_service",
		"exclusive_maintenance",
		"quarantine_connector_campaign_state",
		"stage_clean_database_credentials",
		"create_clean_baseline_database",
		"replace_access_accounts",
		"switch_clean_database",
		"restart_service",
	}
	if err := fixture.engine.updateJournal(journal); err != nil {
		t.Fatal(err)
	}
	if err := runSystemctl(context.Background(), fixture.engine.Runner, "stop", "xinsight.service"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.engine.replaceWithDemoOperator(journal.OperationID, successor); err != nil {
		t.Fatal(err)
	}
	if err := atomicSymlink(targetCredentials.SetRef, fixture.paths.DatabaseCurrentLink); err != nil {
		t.Fatal(err)
	}
	if err := runSystemctl(context.Background(), fixture.engine.Runner, "start", "xinsight.service"); err != nil {
		t.Fatal(err)
	}

	receipt, err := fixture.engine.Abort(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	activeSet, linkErr := readAbsoluteSymlink(fixture.paths.DatabaseCurrentLink)
	if linkErr != nil || activeSet != currentCredentials.SetRef {
		t.Fatalf("active database set=%q err=%v", activeSet, linkErr)
	}
	accounts, readErr := os.ReadFile(fixture.paths.AccessAccountsFile)
	if readErr != nil || string(accounts) != string(originalAccounts) {
		t.Fatalf("rollback account readback differs err=%v", readErr)
	}
	runner := fixture.engine.Runner.(*fakeRunner)
	if receipt.Status != "aborted" || !runner.mainServiceActive ||
		runner.feedWorkerActive || runner.feedWorkerEnabled ||
		runner.liteLLMWorkerActive || runner.liteLLMWorkerEnabled {
		t.Fatalf("receipt=%+v runner=%+v", receipt, runner)
	}
	if _, err := os.Stat(targetCredentials.SetRef); err != nil {
		t.Fatalf("clean target must remain preserved after abort: %v", err)
	}
	if verificationPresent {
		t.Fatal("isolated restore database remains after pre-receipt abort")
	}
	for path, expected := range credentialValues {
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(expected) {
			t.Fatalf("connector campaign state was not restored at %s: %v", filepath.Base(path), err)
		}
	}
}

func TestCleanBaselineResumeCompletesOnlyExactDurableCutoverReceipt(t *testing.T) {
	fixture, _, successor, preview := newCleanBaselineRecoveryFixture(t)
	currentCredentials, err := LoadDatabaseCredentials(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	targetCredentials, err := StageCleanBaselineCredentialSet(
		fixture.paths,
		currentCredentials,
		"xinsight_clean_resume_test",
	)
	if err != nil {
		t.Fatal(err)
	}
	state, err := fixture.engine.readInstallState()
	if err != nil {
		t.Fatal(err)
	}
	before := state.Current
	journal, err := fixture.engine.startJournal(
		"clean_baseline_data_reset",
		before.ArtifactPath,
		"",
		&before,
		&before,
	)
	if err != nil {
		t.Fatal(err)
	}
	journal.DatabaseSetBefore = currentCredentials.SetRef
	journal.DatabaseSetAfter = targetCredentials.SetRef
	journal.CleanDatabase = targetCredentials.Admin.Database
	journal.CleanPredecessorDatabase = currentCredentials.Admin.Database
	journal.CleanRestoreDatabase = cleanBaselineRestoreDatabaseName(journal.OperationID)
	journal.CleanRestoreDatabaseOwned = true
	journal.CleanPriorAccountSHA256 = preview.PredecessorConfigSHA256
	journal.CleanAccountSHA256 = preview.SuccessorConfigSHA256
	backupPath := filepath.Join(fixture.paths.BackupsDir, "data-reset-retained.dump")
	backup, err := fixture.database.Backup(
		context.Background(),
		currentCredentials,
		backupPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	backupSHA256 := backup.SHA256
	floor, err := sealCleanBaselineCheckpointFloor(CleanBaselineCheckpointFloor{
		Binding:              cleanBaselineBinding(),
		PriorIdentitySHA256:  backupSHA256,
		RollbackBackupSHA256: backupSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	databaseProof := CleanBaselineDatabaseProof{
		PostgresMajor:         16,
		MigrationCount:        expectedCleanBaselineSchemaContract.MigrationCount,
		SchemaContractVersion: CleanBaselineSchemaContractVersion,
		SchemaContractSHA256:  expectedCleanBaselineSchemaContract.SemanticSHA256,
		SchemaLayoutSHA256:    expectedCleanBaselineSchemaContract.ColumnLayoutSHA256,
		SchemaLayoutMatches:   true,
		MigrationSetSHA256:    expectedCleanBaselineSchemaContract.MigrationSetSHA256,
		SourceCatalogRows:     1,
		AdapterCatalogRows:    2,
	}
	backupRestore := testCleanBaselineBackupRestoreProof(
		t,
		backupSHA256,
		journal.CleanRestoreDatabase,
	)
	predecessorPresent := true
	verificationPresent := false
	predecessorRetireCalls := 0
	fixture.engine.CleanBaseline = cleanBaselineManagerStub{
		databaseProof:          databaseProof,
		backupRestoreProof:     backupRestore,
		verificationPresent:    &verificationPresent,
		predecessorPresent:     &predecessorPresent,
		predecessorRetireCalls: &predecessorRetireCalls,
	}
	journal.BackupPath = backupPath
	journal.BackupSHA256 = backupSHA256
	journal.CleanBackupRestoreProof = &backupRestore
	journal.CleanCheckpointFloorSHA256 = floor.SealedSHA256
	credentialValues := map[string][]byte{
		fixture.paths.FeedCredentialFile:  []byte("synthetic-active-before-receipt"),
		fixture.paths.FeedCredentialNext:  []byte("synthetic-staged-before-receipt"),
		fixture.paths.FeedCredentialState: []byte(`{"schema_version":"synthetic-before-receipt"}`),
		fixture.paths.LiteLLMCredential:   []byte("synthetic-litellm-credential-before-receipt"),
		fixture.paths.LiteLLMEnvironment:  []byte("XINSIGHT_LITELLM_FEED_SOURCE_INSTANCE=synthetic-before-receipt\n"),
	}
	for path, value := range credentialValues {
		writeHostFile(t, path, value, 0o600)
	}
	credentialPreview, err := inspectCleanBaselineCredentialPreview(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := quarantineCleanBaselineCredentialState(fixture.paths, journal.OperationID, credentialPreview); err != nil {
		t.Fatal(err)
	}
	journal.CleanCredentialPreview = &credentialPreview
	journal.ReasonCode = cleanBaselineReasonCode
	journal.Phase = "write_receipt"
	journal.CompletedPhases = []string{"write_receipt"}
	receiptAt := time.Date(2026, 8, 5, 3, 0, 0, 0, time.UTC)
	journal.ReceiptAt = &receiptAt
	journal.Status = "failed"
	journal.ErrorCode = "receipt_finalize_failed"
	journal.AllowedActions = []string{"recover", "resume"}
	if err := fixture.engine.updateJournal(journal); err != nil {
		t.Fatal(err)
	}
	if err := fixture.engine.replaceWithDemoOperator(journal.OperationID, successor); err != nil {
		t.Fatal(err)
	}
	accountPredecessor := cleanBaselineAccountBackupPath(fixture.paths, journal.OperationID)
	if _, err := os.Stat(accountPredecessor); err != nil {
		t.Fatalf("expected account predecessor before durable resume: %v", err)
	}
	if err := atomicSymlink(targetCredentials.SetRef, fixture.paths.DatabaseCurrentLink); err != nil {
		t.Fatal(err)
	}
	if err := runSystemctl(context.Background(), fixture.engine.Runner, "start", "xinsight.service"); err != nil {
		t.Fatal(err)
	}
	durable := OperationReceipt{
		SchemaVersion: ReceiptSchema, OperationID: journal.OperationID,
		Kind: "clean_baseline_data_reset", Status: "completed", Before: &before, After: &before,
		BackupPath: journal.BackupPath, BackupSHA256: journal.BackupSHA256,
		DatabaseSetRef: targetCredentials.SetRef, ReasonCode: cleanBaselineReasonCode,
		DataReset: &CleanBaselineDurableEvidence{
			SchemaVersion:             CleanBaselineReceiptSchema,
			Release:                   cleanBaselineReleaseIdentity(before),
			PreviewSHA256:             backupSHA256,
			RollbackBackupSHA256:      backupSHA256,
			RollbackDatabaseSetSHA256: cleanBaselineDatabaseSetDigest(currentCredentials.SetRef),
			ActiveDatabaseSetSHA256:   cleanBaselineDatabaseSetDigest(targetCredentials.SetRef),
			PredecessorDatabaseSHA256: hashCleanBaselineText(currentCredentials.Admin.Database),
			BackupRestore:             backupRestore,
			Account:                   preview,
			CheckpointFloor:           floor,
			Credential: CleanBaselineCredentialProof{
				Before:                   credentialPreview,
				ActiveAbsent:             true,
				StagedAbsent:             true,
				OperationStateAbsent:     true,
				LiteLLMCredentialAbsent:  true,
				LiteLLMEnvironmentAbsent: true,
			},
			Database: databaseProof,
		},
		Checks:      []CheckResult{{Name: "fresh_database", Status: "pass", Detail: "exact"}},
		CompletedAt: receiptAt,
	}
	if err := fixture.engine.writeReceipt(durable); err != nil {
		t.Fatal(err)
	}

	resumed, err := fixture.engine.Resume(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	completed, err := fixture.engine.readActiveJournal()
	if err != nil || completed.Status != "completed" || resumed.OperationID != durable.OperationID {
		t.Fatalf("resumed=%+v journal=%+v err=%v", resumed, completed, err)
	}
	if !containsString(completed.CompletedPhases, "retire_connector_campaign_state_quarantine") {
		t.Fatalf("completed journal lacks durable connector campaign cleanup phase: %+v", completed.CompletedPhases)
	}
	if !containsString(completed.CompletedPhases, "retire_predecessor_database") ||
		predecessorPresent || verificationPresent || predecessorRetireCalls != 1 {
		t.Fatalf(
			"database retirement phases=%v predecessor=%t verification=%t calls=%d",
			completed.CompletedPhases,
			predecessorPresent,
			verificationPresent,
			predecessorRetireCalls,
		)
	}
	if _, err := fixture.database.VerifyBackup(
		context.Background(),
		backupPath,
		backupSHA256,
	); err != nil {
		t.Fatalf("single retained rollback dump changed: %v", err)
	}
	if _, err := os.Lstat(accountPredecessor); !os.IsNotExist(err) {
		t.Fatalf("account predecessor remains after completed cutover: %v", err)
	}
	if err := proveCleanBaselineCredentialAbsent(fixture.paths, journal.OperationID, true); err != nil {
		t.Fatalf("connector campaign state quarantine was not retired after durable receipt: %v", err)
	}
	if _, err := fixture.engine.Abort(context.Background()); err == nil {
		t.Fatal("completed clean-baseline cutover was abortable")
	}
}

func TestCleanBaselinePredecessorRetirementFailsClosedWithoutBackupOrRestoreProof(t *testing.T) {
	for _, test := range []struct {
		name         string
		includeProof bool
	}{
		{name: "missing_restore_proof", includeProof: false},
		{name: "missing_retained_backup", includeProof: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, _, _, _ := newCleanBaselineRecoveryFixture(t)
			predecessor, err := LoadDatabaseCredentials(fixture.paths)
			if err != nil {
				t.Fatal(err)
			}
			active, err := StageCleanBaselineCredentialSet(
				fixture.paths,
				predecessor,
				"xinsight_clean_retirement_guard",
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := atomicSymlink(active.SetRef, fixture.paths.DatabaseCurrentLink); err != nil {
				t.Fatal(err)
			}
			state, err := fixture.engine.readInstallState()
			if err != nil {
				t.Fatal(err)
			}
			const backupSHA256 = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
			journal := Journal{
				BackupPath:                filepath.Join(fixture.paths.BackupsDir, "missing.dump"),
				BackupSHA256:              backupSHA256,
				DatabaseSetBefore:         predecessor.SetRef,
				DatabaseSetAfter:          active.SetRef,
				CleanDatabase:             active.Admin.Database,
				CleanPredecessorDatabase:  predecessor.Admin.Database,
				CleanRestoreDatabase:      "xinsight_restore_data_reset_guard",
				CleanRestoreDatabaseOwned: true,
			}
			receipt := OperationReceipt{DataReset: &CleanBaselineDurableEvidence{
				Release:                   cleanBaselineReleaseIdentity(state.Current),
				RollbackDatabaseSetSHA256: cleanBaselineDatabaseSetDigest(predecessor.SetRef),
				ActiveDatabaseSetSHA256:   cleanBaselineDatabaseSetDigest(active.SetRef),
				PredecessorDatabaseSHA256: hashCleanBaselineText(predecessor.Admin.Database),
			}}
			if test.includeProof {
				proof := testCleanBaselineBackupRestoreProof(
					t,
					backupSHA256,
					journal.CleanRestoreDatabase,
				)
				journal.CleanBackupRestoreProof = &proof
				receipt.DataReset.BackupRestore = proof
			}
			predecessorPresent := true
			retireCalls := 0
			fixture.engine.CleanBaseline = cleanBaselineManagerStub{
				databaseProof:          receipt.DataReset.Database,
				predecessorPresent:     &predecessorPresent,
				predecessorRetireCalls: &retireCalls,
			}
			if err := fixture.engine.retireCleanBaselinePredecessorDatabase(
				context.Background(),
				&journal,
				&receipt,
			); err == nil {
				t.Fatal("predecessor retirement accepted incomplete rollback evidence")
			}
			if !predecessorPresent || retireCalls != 0 {
				t.Fatalf("predecessor was retired before proof: present=%t calls=%d", predecessorPresent, retireCalls)
			}
		})
	}
}

func newCleanBaselineRecoveryFixture(
	t *testing.T,
) (installFixture, []byte, access.AccountConfig, CleanBaselineAccountPreview) {
	t.Helper()
	fixture := newInstallFixture(t)
	target := packRC13AccessTarget(t, fixture, nil, nil)
	config := access.AccountConfig{
		SchemaVersion: access.AccountConfigSchema,
		Accounts: []access.AccountConfigEntry{
			{
				SubjectID: "xs_2222222222222222", Username: "remove.after.reset",
				Role: access.RoleInvestigator, PasswordHash: cleanBaselineTestPasswordHash,
			},
			{
				SubjectID: "xs_1111111111111111", Username: "demo.operator",
				Role: access.RoleOperator, PasswordHash: cleanBaselineTestPasswordHash,
			},
		},
	}
	original, err := marshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	writeHostFile(t, fixture.paths.AccessAccountsFile, original, 0o600)
	verified, err := fixture.engine.verifyTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity: verified.Identity, accessProtected: true,
	}}
	if _, err := fixture.engine.Install(context.Background(), target); err != nil {
		t.Fatalf("install access release: %v", err)
	}
	_, successor, preview, err := cleanBaselineDemoOperator(fixture.paths.AccessAccountsFile)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, original, successor, preview
}

const cleanBaselineTestPasswordHash = "$argon2id$v=19$m=19456,t=2,p=1$l/Jq1AZhEtyhc18PB2oOXg$v2CbNPksEVetOCbGuBIIOeKNHdF/Z+JBU9YuhqQ7piY"

func testCleanBaselineBackupRestoreProof(
	t *testing.T,
	backupSHA256 string,
	verificationDatabase string,
) CleanBaselineBackupRestoreProof {
	t.Helper()
	semanticSHA256 := hashCleanBaselineText("synthetic-complete-database-preview")
	proof, err := sealCleanBaselineBackupRestoreProof(CleanBaselineBackupRestoreProof{
		BackupSHA256:               backupSHA256,
		VerificationDatabaseSHA256: hashCleanBaselineText(verificationDatabase),
		SourceSemanticSHA256:       semanticSHA256,
		RestoredSemanticSHA256:     semanticSHA256,
		PostgresMajor:              16,
		MigrationCount:             expectedCleanBaselineSchemaContract.MigrationCount,
		VerificationDatabaseAbsent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return proof
}

type cleanBaselineManagerStub struct {
	databaseProof          CleanBaselineDatabaseProof
	backupRestoreProof     CleanBaselineBackupRestoreProof
	active                 checkpointepoch.ActiveStateSummary
	verificationPresent    *bool
	predecessorPresent     *bool
	predecessorRetireCalls *int
	retirementError        error
}

func (stub cleanBaselineManagerStub) PreviewCleanBaseline(
	context.Context,
	DatabaseCredentials,
) (CleanBaselineDatabasePreview, error) {
	return CleanBaselineDatabasePreview{}, nil
}

func (stub cleanBaselineManagerStub) CleanBaselineDatabaseExists(
	context.Context,
	DatabaseCredentials,
	string,
) (bool, error) {
	return false, nil
}

func (stub cleanBaselineManagerStub) VerifyCleanBaselineBackupRestore(
	context.Context,
	DatabaseCredentials,
	string,
	string,
	string,
	string,
	[]db.MigrationChecksum,
	CleanBaselineDatabasePreview,
) (CleanBaselineBackupRestoreProof, error) {
	return stub.backupRestoreProof, nil
}

func (stub cleanBaselineManagerStub) RetireCleanBaselineVerificationDatabase(
	context.Context,
	DatabaseCredentials,
	string,
) error {
	if stub.retirementError != nil {
		return stub.retirementError
	}
	if stub.verificationPresent != nil {
		*stub.verificationPresent = false
	}
	return nil
}

func (stub cleanBaselineManagerStub) RetireCleanBaselinePredecessorDatabase(
	context.Context,
	DatabaseCredentials,
	DatabaseCredentials,
) error {
	if stub.predecessorRetireCalls != nil {
		*stub.predecessorRetireCalls++
	}
	if stub.retirementError != nil {
		return stub.retirementError
	}
	if stub.predecessorPresent != nil {
		*stub.predecessorPresent = false
	}
	return nil
}

func (stub cleanBaselineManagerStub) CreateCleanBaselineDatabase(
	context.Context,
	DatabaseCredentials,
	DatabaseCredentials,
	string,
	[]db.MigrationChecksum,
) (CleanBaselineDatabaseProof, error) {
	return stub.databaseProof, nil
}

func (stub cleanBaselineManagerStub) VerifyCleanBaselineDatabase(
	context.Context,
	DatabaseCredentials,
	[]db.MigrationChecksum,
) (CleanBaselineDatabaseProof, error) {
	return stub.databaseProof, nil
}

func (stub cleanBaselineManagerStub) BootstrapEmptyCheckpoint(
	context.Context,
	DatabaseCredentials,
	checkpointepoch.PrepareInput,
	int64,
	time.Time,
) (checkpointepoch.ActiveStateSummary, error) {
	return stub.active, nil
}

func (stub cleanBaselineManagerStub) VerifyEmptyCheckpoint(
	context.Context,
	DatabaseCredentials,
	checkpointepoch.PrepareInput,
	int64,
) (checkpointepoch.ActiveStateSummary, error) {
	return stub.active, nil
}

func TestCleanBaselineDemoOperatorPreservesExactIdentityRoleAndVerifier(t *testing.T) {
	const firstHash = "$argon2id$v=19$m=19456,t=2,p=1$l/Jq1AZhEtyhc18PB2oOXg$v2CbNPksEVetOCbGuBIIOeKNHdF/Z+JBU9YuhqQ7piY"
	const secondHash = firstHash
	config := access.AccountConfig{
		SchemaVersion: access.AccountConfigSchema,
		Accounts: []access.AccountConfigEntry{
			{
				SubjectID: "xs_0000000000000000", Username: "other.user",
				Role: access.RoleInvestigator, Disabled: false, PasswordHash: firstHash,
			},
			{
				SubjectID: "xs_1111111111111111", Username: "demo.operator",
				Role: access.RoleOperator, Disabled: false, PasswordHash: secondHash,
			},
		},
	}
	payload, err := marshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "access-accounts.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	entry, successor, preview, err := cleanBaselineDemoOperator(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(successor.Accounts) != 1 || successor.Accounts[0] != entry ||
		entry.SubjectID != "xs_1111111111111111" || entry.Role != access.RoleOperator ||
		entry.PasswordHash != secondHash || preview.AccountsRemoved != 1 ||
		preview.PasswordVerifierSHA256 != hashCleanBaselineText(secondHash) {
		t.Fatalf("entry=%+v successor=%+v preview=%+v", entry, successor, preview)
	}
}
