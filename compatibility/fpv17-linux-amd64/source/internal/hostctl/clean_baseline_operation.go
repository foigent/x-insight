package hostctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/checkpointepoch"
)

func (engine *Engine) ApplyCleanBaselineDataReset(
	ctx context.Context,
	backup string,
	backupSHA256 string,
	newDatabase string,
	expectedPreviewSHA256 string,
	reasonCode string,
) (CleanBaselineReceipt, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return CleanBaselineReceipt{}, err
	}
	if !sha256ReferencePattern.MatchString(expectedPreviewSHA256) {
		return CleanBaselineReceipt{}, fmt.Errorf("clean-baseline requires the exact preview SHA-256")
	}
	if reasonCode != cleanBaselineReasonCode {
		return CleanBaselineReceipt{}, fmt.Errorf("clean-baseline requires --reason-code %s", cleanBaselineReasonCode)
	}
	var receipt CleanBaselineReceipt
	err := engine.withOperationLock(func() error {
		preview, plan, err := engine.prepareCleanBaseline(ctx, backup, backupSHA256, newDatabase)
		if err != nil {
			return err
		}
		if preview.PreviewSHA256 != expectedPreviewSHA256 {
			return fmt.Errorf("clean-baseline preview digest changed; rerun preview and obtain a new authorization")
		}
		before := plan.State.Current
		journal, err := engine.startJournal("clean_baseline_data_reset", before.ArtifactPath, "", &before, &before)
		if err != nil {
			return err
		}
		journal.BackupPath = plan.Backup.Path
		journal.BackupSHA256 = plan.Backup.SHA256
		journal.CleanDatabase = plan.NewDatabase
		journal.CleanPredecessorDatabase = plan.Credentials.Admin.Database
		journal.CleanRestoreDatabase = cleanBaselineRestoreDatabaseName(journal.OperationID)
		journal.CleanPreviewSHA256 = preview.PreviewSHA256
		journal.CleanPriorAccountSHA256 = plan.AccountPreview.PredecessorConfigSHA256
		journal.CleanAccountSHA256 = plan.AccountPreview.SuccessorConfigSHA256
		credentialPreview := plan.Credential
		journal.CleanCredentialPreview = &credentialPreview
		journal.CleanCheckpointFloorSHA256 = plan.DatabasePreview.CheckpointFloor.SealedSHA256
		journal.ServicePIDBefore = plan.ServiceMainPID
		journal.DatabaseSetBefore = plan.Credentials.SetRef
		journal.ReasonCode = reasonCode
		if err := engine.updateJournal(journal); err != nil {
			return err
		}
		receipt, err = engine.executeCleanBaseline(ctx, journal, plan)
		return err
	})
	return receipt, err
}

func (engine *Engine) executeCleanBaseline(
	ctx context.Context,
	journal *Journal,
	plan cleanBaselinePlan,
) (receipt CleanBaselineReceipt, resultErr error) {
	var maintenance Maintenance
	var targetCredentials DatabaseCredentials
	serviceStopped := false
	accountReplaced := false
	databaseSwitched := false
	operationFinalized := false
	failureCode := "clean_baseline_failed"

	defer func() {
		resultErr = engine.closeCleanBaselineExecution(
			journal, plan, maintenance, serviceStopped, accountReplaced,
			databaseSwitched, operationFinalized, failureCode, resultErr,
		)
	}()

	preparation, preparationFailureCode, err := engine.prepareCleanBaselineExecution(ctx, journal, plan)
	maintenance = preparation.maintenance
	serviceStopped = preparation.serviceStopped
	if err != nil {
		failureCode = preparationFailureCode
		return CleanBaselineReceipt{}, err
	}
	backupRestoreProof := preparation.backupRestoreProof

	if err := engine.beginPhase(journal, "stage_clean_database_credentials"); err != nil {
		failureCode = "journal_update_failed"
		return CleanBaselineReceipt{}, err
	}
	targetCredentials, err = StageCleanBaselineCredentialSet(engine.Paths, plan.Credentials, plan.NewDatabase)
	if err != nil {
		failureCode = "clean_database_credential_staging_failed"
		return CleanBaselineReceipt{}, err
	}
	journal.DatabaseSetAfter = targetCredentials.SetRef
	if err := engine.updateJournal(journal); err != nil {
		failureCode = "journal_update_failed"
		return CleanBaselineReceipt{}, err
	}
	if err := engine.completePhase(journal, "stage_clean_database_credentials"); err != nil {
		failureCode = "journal_update_failed"
		return CleanBaselineReceipt{}, err
	}

	var databaseProof CleanBaselineDatabaseProof
	if err := engine.runPhase(journal, "create_clean_baseline_database", func() error {
		var createErr error
		databaseProof, createErr = engine.CleanBaseline.CreateCleanBaselineDatabase(
			ctx,
			plan.Credentials,
			targetCredentials,
			filepath.Join(plan.Current.Identity.ReleaseDir, "migrations"),
			plan.Current.Bundle.Manifest.MigrationChecksums,
		)
		return createErr
	}); err != nil {
		failureCode = "clean_database_create_failed"
		return CleanBaselineReceipt{}, err
	}

	if err := engine.runPhase(journal, "replace_access_accounts", func() error {
		return engine.replaceWithDemoOperator(journal.OperationID, plan.SuccessorAccounts)
	}); err != nil {
		failureCode = "access_account_replacement_failed"
		return CleanBaselineReceipt{}, err
	}
	accountReplaced = true
	if err := validateCleanBaselineAccountPredecessor(
		engine.Paths,
		journal.OperationID,
		plan.AccountPreview.PredecessorConfigSHA256,
	); err != nil {
		failureCode = "access_account_predecessor_invalid"
		return CleanBaselineReceipt{}, err
	}
	if err := ensureCleanBaselineAccountReadback(engine.Paths.AccessAccountsFile, plan.AccountEntry); err != nil {
		failureCode = "access_account_readback_failed"
		return CleanBaselineReceipt{}, err
	}

	if err := engine.runPhase(journal, "switch_clean_database", func() error {
		return atomicSymlink(targetCredentials.SetRef, engine.Paths.DatabaseCurrentLink)
	}); err != nil {
		failureCode = "clean_database_switch_failed"
		return CleanBaselineReceipt{}, err
	}
	databaseSwitched = true
	if err := engine.runPhase(journal, "restart_service", func() error {
		return runSystemctl(ctx, engine.Runner, "restart", "xinsight.service")
	}); err != nil {
		failureCode = "service_restart_failed"
		return CleanBaselineReceipt{}, err
	}
	proofChecks, databaseProof, activationFailureCode, err := engine.verifyCleanBaselineActivation(
		ctx, journal, plan, targetCredentials,
	)
	if err != nil {
		failureCode = activationFailureCode
		return CleanBaselineReceipt{}, err
	}

	if err := engine.runPhase(journal, "enable_importer", func() error {
		return maintenance.SetRoleLogin(ctx, "xinsight_importer", true)
	}); err != nil {
		failureCode = "importer_enable_failed"
		return CleanBaselineReceipt{}, err
	}
	if err := engine.runPhase(journal, "verify_clean_database_role_boundary", func() error {
		return engine.Database.VerifyRoleBoundaries(ctx, targetCredentials)
	}); err != nil {
		failureCode = "clean_database_role_boundary_failed"
		return CleanBaselineReceipt{}, err
	}
	if err := engine.runPhase(journal, "release_source_maintenance", func() error {
		if err := maintenance.Close(); err != nil {
			return err
		}
		maintenance = nil
		return nil
	}); err != nil {
		failureCode = "maintenance_release_failed"
		return CleanBaselineReceipt{}, err
	}

	baseReceipt, credentialProof, checks := engine.prepareCleanBaselineCompletion(
		journal, plan, targetCredentials, backupRestoreProof, databaseProof, proofChecks,
	)
	if err := engine.finalizeCleanBaselineOperation(ctx, journal, &baseReceipt); err != nil {
		failureCode = "receipt_finalize_failed"
		return CleanBaselineReceipt{}, err
	}
	operationFinalized = true
	receiptPath := filepath.Join(engine.Paths.ReceiptsDir, journal.OperationID+".json")
	durableReceiptSHA256, _, err := fileSHA256(receiptPath, cleanBaselineReceiptMaxBytes)
	if err != nil {
		return CleanBaselineReceipt{}, err
	}
	credentialProof.QuarantineAbsent = true
	completedChecks := append([]CheckResult(nil), checks...)
	completedChecks = append(completedChecks, CheckResult{
		Name: "predecessor_database", Status: "pass", Detail: "absent_single_retained_dump_only",
	})
	return CleanBaselineReceipt{
		SchemaVersion:             CleanBaselineReceiptSchema,
		OperationID:               journal.OperationID,
		Status:                    "completed",
		Release:                   cleanBaselineReleaseIdentity(plan.State.Current),
		PreviewSHA256:             plan.Preview.PreviewSHA256,
		RollbackBackupSHA256:      plan.Backup.SHA256,
		RollbackDatabaseSetSHA256: cleanBaselineDatabaseSetDigest(plan.Credentials.SetRef),
		ActiveDatabaseSetSHA256:   cleanBaselineDatabaseSetDigest(targetCredentials.SetRef),
		PredecessorDatabaseSHA256: hashCleanBaselineText(plan.Credentials.Admin.Database),
		PredecessorDatabaseAbsent: true,
		BackupRestore:             backupRestoreProof,
		Account:                   plan.AccountPreview,
		CheckpointFloor:           plan.DatabasePreview.CheckpointFloor,
		Credential:                credentialProof,
		Database:                  databaseProof,
		Checks:                    completedChecks,
		DurableReceiptSHA256:      durableReceiptSHA256,
		CompletedAt:               baseReceipt.CompletedAt,
	}, nil
}

type cleanBaselinePreparation struct {
	maintenance        Maintenance
	serviceStopped     bool
	backupRestoreProof CleanBaselineBackupRestoreProof
}

func (engine *Engine) prepareCleanBaselineExecution(
	ctx context.Context,
	journal *Journal,
	plan cleanBaselinePlan,
) (cleanBaselinePreparation, string, error) {
	var preparation cleanBaselinePreparation
	if err := engine.ensureControlDirectories(); err != nil {
		return preparation, "directory_staging_failed", err
	}
	if _, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.CompletedExecutionWorkerUnit); err != nil {
		return preparation, "feed_worker_not_dormant", err
	}
	if _, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.LiteLLMWorkerUnit); err != nil {
		return preparation, "litellm_worker_not_dormant", err
	}
	if err := engine.runPhase(journal, "stop_service", func() error {
		return runSystemctl(ctx, engine.Runner, "stop", "xinsight.service")
	}); err != nil {
		return preparation, "service_stop_failed", err
	}
	preparation.serviceStopped = true
	if err := engine.beginPhase(journal, "exclusive_maintenance"); err != nil {
		return preparation, "journal_update_failed", err
	}
	maintenance, err := engine.Database.AcquireMaintenance(ctx, plan.Credentials)
	preparation.maintenance = maintenance
	if err != nil {
		return preparation, "maintenance_lock_failed", err
	}
	if err := maintenance.SetRoleLogin(ctx, "xinsight_importer", false); err != nil {
		return preparation, "importer_gate_failed", err
	}
	sessions, err := maintenance.CountOtherSessions(ctx, []string{
		"xinsight_reader", "xinsight_workspace", "xinsight_importer", "xinsight_admin",
	})
	if err != nil {
		return preparation, "database_sessions_present", err
	}
	if sessions != 0 {
		return preparation, "database_sessions_present", fmt.Errorf("database sessions remain after controlled service stop")
	}
	if err := engine.completePhase(journal, "exclusive_maintenance"); err != nil {
		return preparation, "journal_update_failed", err
	}
	if err := engine.runPhase(journal, "verify_restore_database_absent", func() error {
		exists, err := engine.Database.DatabaseExists(ctx, plan.Credentials, journal.CleanRestoreDatabase)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("isolated backup restore database already exists")
		}
		return nil
	}); err != nil {
		return preparation, "restore_database_precondition_failed", err
	}
	if err := engine.beginPhase(journal, "verify_rollback_backup_restore"); err != nil {
		return preparation, "journal_update_failed", err
	}
	journal.CleanRestoreDatabaseOwned = true
	if err := engine.updateJournal(journal); err != nil {
		return preparation, "journal_update_failed", err
	}
	backupRestoreProof, err := engine.CleanBaseline.VerifyCleanBaselineBackupRestore(
		ctx,
		plan.Credentials,
		journal.CleanRestoreDatabase,
		plan.Backup.Path,
		plan.Backup.SHA256,
		filepath.Join(plan.Current.Identity.ReleaseDir, "migrations"),
		plan.Current.Bundle.Manifest.MigrationChecksums,
		plan.DatabasePreview,
	)
	preparation.backupRestoreProof = backupRestoreProof
	if err != nil {
		return preparation, "rollback_backup_restore_proof_failed", err
	}
	journal.CleanBackupRestoreProof = &backupRestoreProof
	if err := engine.updateJournal(journal); err != nil {
		return preparation, "journal_update_failed", err
	}
	if err := engine.completePhase(journal, "verify_rollback_backup_restore"); err != nil {
		return preparation, "journal_update_failed", err
	}
	if err := engine.runPhase(journal, "quarantine_connector_campaign_state", func() error {
		return quarantineCleanBaselineCredentialState(engine.Paths, journal.OperationID, plan.Credential)
	}); err != nil {
		return preparation, "connector_campaign_state_retirement_failed", err
	}
	return preparation, "", nil
}

func (engine *Engine) verifyCleanBaselineActivation(
	ctx context.Context,
	journal *Journal,
	plan cleanBaselinePlan,
	targetCredentials DatabaseCredentials,
) ([]CheckResult, CleanBaselineDatabaseProof, string, error) {
	afterPID, err := engine.systemdUnitProperty(ctx, "xinsight.service", "MainPID")
	if err != nil || afterPID == journal.ServicePIDBefore {
		return nil, CleanBaselineDatabaseProof{}, "session_process_replacement_failed",
			fmt.Errorf("main service process was not replaced")
	}
	pid, err := strconv.ParseInt(afterPID, 10, 64)
	if err != nil || pid <= 0 {
		return nil, CleanBaselineDatabaseProof{}, "session_process_replacement_failed",
			fmt.Errorf("replacement main service process identity is invalid")
	}
	proofChecks, err := engine.proveActivatedRelease(
		ctx,
		journal,
		plan.State.Current,
		plan.State.DatabaseSchemaCurrent,
		true,
	)
	if err != nil {
		return nil, CleanBaselineDatabaseProof{}, "clean_baseline_activation_proof_failed", err
	}
	if err := ensureCleanBaselineAccountReadback(engine.Paths.AccessAccountsFile, plan.AccountEntry); err != nil {
		return nil, CleanBaselineDatabaseProof{}, "access_account_readback_failed", err
	}
	databaseProof, err := engine.CleanBaseline.VerifyCleanBaselineDatabase(
		ctx,
		targetCredentials,
		plan.Current.Bundle.Manifest.MigrationChecksums,
	)
	if err != nil {
		return nil, CleanBaselineDatabaseProof{}, "clean_database_readback_failed", err
	}
	if _, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.CompletedExecutionWorkerUnit); err != nil {
		return nil, CleanBaselineDatabaseProof{}, "feed_worker_changed_during_data_reset", err
	}
	if _, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.LiteLLMWorkerUnit); err != nil {
		return nil, CleanBaselineDatabaseProof{}, "litellm_worker_changed_during_data_reset", err
	}
	if err := proveCleanBaselineCredentialAbsent(engine.Paths, journal.OperationID, false); err != nil {
		return nil, CleanBaselineDatabaseProof{}, "connector_campaign_state_readback_failed", err
	}
	return proofChecks, databaseProof, "", nil
}

func (engine *Engine) closeCleanBaselineExecution(
	journal *Journal,
	plan cleanBaselinePlan,
	maintenance Maintenance,
	serviceStopped bool,
	accountReplaced bool,
	databaseSwitched bool,
	operationFinalized bool,
	failureCode string,
	resultErr error,
) error {
	if maintenance != nil {
		defer maintenance.Close()
	}
	if resultErr == nil || operationFinalized {
		return resultErr
	}
	_, receiptExists, receiptErr := engine.readCleanBaselineDurableReceipt(*journal)
	if receiptExists {
		if receiptErr != nil {
			_ = engine.failJournal(journal, "clean_baseline_receipt_ambiguous", "recover")
			return fmt.Errorf("%s: %v; durable receipt is ambiguous: %w", failureCode, resultErr, receiptErr)
		}
		if failErr := engine.failJournal(journal, failureCode, "recover", "resume"); failErr != nil {
			return fmt.Errorf("%s: %v; operation journal update failed: %w", failureCode, resultErr, failErr)
		}
		return fmt.Errorf("%s: %w", failureCode, resultErr)
	}
	rollbackErr := engine.rollbackFailedCleanBaseline(
		journal,
		plan,
		maintenance,
		serviceStopped,
		accountReplaced,
		databaseSwitched,
	)
	if rollbackErr != nil {
		_ = engine.failJournal(journal, "clean_baseline_automatic_rollback_failed", "recover")
		return fmt.Errorf("%s: %v; automatic rollback failed: %w", failureCode, resultErr, rollbackErr)
	}
	if failErr := engine.failJournal(journal, failureCode, "recover", "abort"); failErr != nil {
		return fmt.Errorf("%s: %v; operation journal update failed: %w", failureCode, resultErr, failErr)
	}
	return fmt.Errorf("%s: %w", failureCode, resultErr)
}

func (engine *Engine) prepareCleanBaselineCompletion(
	journal *Journal,
	plan cleanBaselinePlan,
	targetCredentials DatabaseCredentials,
	backupRestoreProof CleanBaselineBackupRestoreProof,
	databaseProof CleanBaselineDatabaseProof,
	proofChecks []CheckResult,
) (OperationReceipt, CleanBaselineCredentialProof, []CheckResult) {
	checks := append([]CheckResult{
		{Name: "fresh_database", Status: "pass", Detail: "migrations_only_no_restore_or_delete"},
		{Name: "rollback_backup_restore", Status: "pass", Detail: backupRestoreProof.SealedSHA256},
		{Name: "rollback_source", Status: "pass", Detail: "predecessor_bound_for_post_receipt_retirement"},
		{Name: "historical_rows", Status: "pass", Detail: "zero"},
		{Name: "release_catalog", Status: "pass", Detail: "one_source_two_adapters"},
		{Name: "checkpoint_floor", Status: "pass", Detail: plan.DatabasePreview.CheckpointFloor.SealedSHA256},
		{Name: "strict_empty_checkpoint", Status: "pass", Detail: "state_zero_operations_zero_history_zero"},
		{Name: "connector_credential_config", Status: "pass", Detail: "unirouter_state_and_litellm_credential_environment_absent_no_replacement"},
		{Name: "demo_operator", Status: "pass", Detail: "subject_username_role_verifier_unchanged"},
		{Name: "sessions", Status: "pass", Detail: "database_zero_and_main_process_replaced"},
		{Name: "workers", Status: "pass", Detail: "both_inactive_disabled_no_poll"},
	}, proofChecks...)
	credentialProof := CleanBaselineCredentialProof{
		Before:                   plan.Credential,
		ActiveAbsent:             true,
		StagedAbsent:             true,
		OperationStateAbsent:     true,
		LiteLLMCredentialAbsent:  true,
		LiteLLMEnvironmentAbsent: true,
		ReplacementGenerated:     false,
	}
	durableEvidence := &CleanBaselineDurableEvidence{
		SchemaVersion:             CleanBaselineReceiptSchema,
		Release:                   cleanBaselineReleaseIdentity(plan.State.Current),
		PreviewSHA256:             plan.Preview.PreviewSHA256,
		RollbackBackupSHA256:      plan.Backup.SHA256,
		RollbackDatabaseSetSHA256: cleanBaselineDatabaseSetDigest(plan.Credentials.SetRef),
		ActiveDatabaseSetSHA256:   cleanBaselineDatabaseSetDigest(targetCredentials.SetRef),
		PredecessorDatabaseSHA256: hashCleanBaselineText(plan.Credentials.Admin.Database),
		BackupRestore:             backupRestoreProof,
		Account:                   plan.AccountPreview,
		CheckpointFloor:           plan.DatabasePreview.CheckpointFloor,
		Credential:                credentialProof,
		Database:                  databaseProof,
	}
	return OperationReceipt{
		SchemaVersion:  ReceiptSchema,
		OperationID:    journal.OperationID,
		Kind:           "clean_baseline_data_reset",
		Status:         "completed",
		Before:         &plan.State.Current,
		After:          &plan.State.Current,
		BackupPath:     plan.Backup.Path,
		BackupSHA256:   plan.Backup.SHA256,
		MigrationIDs:   migrationIDs(plan.Current.Bundle.Manifest.MigrationChecksums),
		DatabaseSetRef: targetCredentials.SetRef,
		ReasonCode:     journal.ReasonCode,
		DataReset:      durableEvidence,
		Checks:         checks,
		CompletedAt:    cleanBaselineNow(engine),
	}, credentialProof, checks
}

func (engine *Engine) finalizeCleanBaselineOperation(
	ctx context.Context,
	journal *Journal,
	receipt *OperationReceipt,
) error {
	if receipt == nil {
		return fmt.Errorf("operation receipt is required")
	}
	if journal.ReceiptAt == nil {
		completedAt := engine.now()
		journal.ReceiptAt = &completedAt
		if err := engine.updateJournal(journal); err != nil {
			return err
		}
	}
	receipt.CompletedAt = *journal.ReceiptAt
	if err := engine.runPhase(journal, "write_receipt", func() error {
		return engine.writeReceipt(*receipt)
	}); err != nil {
		return err
	}
	if err := engine.runPhase(journal, "retire_predecessor_database", func() error {
		return engine.retireCleanBaselinePredecessorDatabase(ctx, journal, receipt)
	}); err != nil {
		return err
	}
	if err := engine.runPhase(journal, "retire_access_account_predecessor", func() error {
		return retireCleanBaselineAccountPredecessor(
			engine.Paths,
			journal.OperationID,
			journal.CleanPriorAccountSHA256,
		)
	}); err != nil {
		return err
	}
	if err := engine.runPhase(journal, "retire_connector_campaign_state_quarantine", func() error {
		if journal.CleanCredentialPreview == nil {
			return fmt.Errorf("connector credential/config preview is missing from DATA_RESET journal")
		}
		return retireCleanBaselineCredentialQuarantine(
			engine.Paths,
			journal.OperationID,
			*journal.CleanCredentialPreview,
		)
	}); err != nil {
		return err
	}
	return engine.completeJournal(journal)
}

func (engine *Engine) retireCleanBaselinePredecessorDatabase(
	ctx context.Context,
	journal *Journal,
	receipt *OperationReceipt,
) error {
	if journal == nil || receipt == nil || receipt.DataReset == nil ||
		journal.CleanBackupRestoreProof == nil || !journal.CleanRestoreDatabaseOwned ||
		receipt.DataReset.BackupRestore != *journal.CleanBackupRestoreProof {
		return fmt.Errorf("isolated backup restore proof is missing from DATA_RESET receipt")
	}
	if err := verifyCleanBaselineBackupRestoreProof(
		receipt.DataReset.BackupRestore,
		journal.BackupSHA256,
	); err != nil {
		return err
	}
	if journal.CleanPredecessorDatabase == "" || journal.CleanRestoreDatabase == "" ||
		hashCleanBaselineText(journal.CleanPredecessorDatabase) != receipt.DataReset.PredecessorDatabaseSHA256 ||
		hashCleanBaselineText(journal.CleanRestoreDatabase) != receipt.DataReset.BackupRestore.VerificationDatabaseSHA256 {
		return fmt.Errorf("database retirement identities differ from DATA_RESET receipt")
	}
	activeSet, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
	if err != nil || activeSet != journal.DatabaseSetAfter {
		return fmt.Errorf("clean-baseline active database set differs before predecessor retirement")
	}
	active, err := LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetAfter)
	if err != nil || active.Admin.Database != journal.CleanDatabase ||
		cleanBaselineDatabaseSetDigest(active.SetRef) != receipt.DataReset.ActiveDatabaseSetSHA256 {
		return fmt.Errorf("clean-baseline active database identity differs before predecessor retirement")
	}
	state, err := engine.readInstallState()
	if err != nil {
		return err
	}
	current, err := engine.verifyStateV2(state.Current)
	if err != nil || cleanBaselineReleaseIdentity(state.Current) != receipt.DataReset.Release {
		return fmt.Errorf("clean-baseline release differs before predecessor retirement")
	}
	strictEmpty, err := engine.CleanBaseline.VerifyCleanBaselineDatabase(
		ctx,
		active,
		current.Bundle.Manifest.MigrationChecksums,
	)
	if err != nil || strictEmpty != receipt.DataReset.Database {
		return fmt.Errorf("clean-baseline strict-empty readback differs before predecessor retirement")
	}
	predecessor, err := LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetBefore)
	if err != nil || predecessor.Admin.Database != journal.CleanPredecessorDatabase ||
		cleanBaselineDatabaseSetDigest(predecessor.SetRef) != receipt.DataReset.RollbackDatabaseSetSHA256 {
		return fmt.Errorf("clean-baseline predecessor database identity differs from DATA_RESET receipt")
	}
	if _, err := engine.Database.VerifyBackup(ctx, journal.BackupPath, journal.BackupSHA256); err != nil {
		return fmt.Errorf("retained rollback backup is unavailable before predecessor retirement: %w", err)
	}
	if err := engine.CleanBaseline.RetireCleanBaselineVerificationDatabase(
		ctx,
		active,
		journal.CleanRestoreDatabase,
	); err != nil {
		return fmt.Errorf("retire isolated restore database before predecessor: %w", err)
	}
	if err := engine.CleanBaseline.RetireCleanBaselinePredecessorDatabase(
		ctx,
		active,
		predecessor,
	); err != nil {
		return err
	}
	if _, err := engine.Database.VerifyBackup(ctx, journal.BackupPath, journal.BackupSHA256); err != nil {
		return fmt.Errorf("retained rollback backup changed during predecessor retirement: %w", err)
	}
	return nil
}

func (engine *Engine) replaceWithDemoOperator(
	operationID string,
	successor access.AccountConfig,
) error {
	staging := cleanBaselineAccountStagingPath(engine.Paths, operationID)
	expected, err := marshalJSON(successor)
	if err != nil {
		return err
	}
	expectedSHA256 := hashCleanBaselineBytes(expected)
	if exists, err := fileExists(staging); err != nil {
		return err
	} else if !exists {
		if err := writeJSONExclusive(staging, successor, 0o600); err != nil {
			return err
		}
	}
	stagedSHA256, _, err := fileSHA256(staging, access.MaxAccountFileBytes)
	if err != nil || stagedSHA256 != expectedSHA256 {
		return fmt.Errorf("demo.operator staging file differs from authorized successor")
	}
	defer os.Remove(staging)
	return replaceFilePreservingPrevious(
		staging,
		engine.Paths.AccessAccountsFile,
		0o600,
		operationID,
		access.MaxAccountFileBytes,
	)
}

func (engine *Engine) rollbackFailedCleanBaseline(
	journal *Journal,
	plan cleanBaselinePlan,
	maintenance Maintenance,
	serviceStopped bool,
	accountReplaced bool,
	databaseSwitched bool,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := engine.beginPhase(journal, "automatic_rollback_to_source"); err != nil {
		return err
	}
	var rollbackErrors []error
	if databaseSwitched || accountReplaced {
		if err := runSystemctl(ctx, engine.Runner, "stop", "xinsight.service"); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("stop replacement service: %w", err))
		}
	}
	if journal.CleanRestoreDatabaseOwned && journal.CleanRestoreDatabase == "" {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("isolated restore database identity is missing"))
	} else if journal.CleanRestoreDatabaseOwned {
		if err := engine.CleanBaseline.RetireCleanBaselineVerificationDatabase(
			ctx,
			plan.Credentials,
			journal.CleanRestoreDatabase,
		); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("retire isolated restore database: %w", err))
		}
	}
	if databaseSwitched {
		if err := atomicSymlink(plan.Credentials.SetRef, engine.Paths.DatabaseCurrentLink); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore database set link: %w", err))
		}
	}
	if accountReplaced {
		backup := cleanBaselineAccountBackupPath(engine.Paths, journal.OperationID)
		if !cleanBaselinePathExists(backup) {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("protected access account predecessor is missing"))
		} else if err := replaceFilePreservingPrevious(
			backup,
			engine.Paths.AccessAccountsFile,
			0o600,
			journal.OperationID+"-rollback",
			access.MaxAccountFileBytes,
		); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore access accounts: %w", err))
		}
	}
	if err := restoreCleanBaselineCredentialState(
		engine.Paths,
		journal.OperationID,
		plan.Credential,
	); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("restore connector credential/config state: %w", err))
	}
	if maintenance != nil {
		if err := maintenance.SetRoleLogin(ctx, "xinsight_importer", true); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore importer login: %w", err))
		}
	} else if err := engine.Database.EnsureRoles(ctx, plan.Credentials); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("restore database roles: %w", err))
	}
	if serviceStopped {
		if err := runSystemctl(ctx, engine.Runner, "start", "xinsight.service"); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restart source service: %w", err))
		}
	}
	if _, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.CompletedExecutionWorkerUnit); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	if _, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.LiteLLMWorkerUnit); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	if len(rollbackErrors) != 0 {
		return errors.Join(rollbackErrors...)
	}
	return engine.completePhase(journal, "automatic_rollback_to_source")
}

func (engine *Engine) readCleanBaselineDurableReceipt(
	journal Journal,
) (OperationReceipt, bool, error) {
	path := filepath.Join(engine.Paths.ReceiptsDir, journal.OperationID+".json")
	exists, err := fileExists(path)
	if err != nil || !exists {
		return OperationReceipt{}, exists, err
	}
	var receipt OperationReceipt
	if err := readJSONStrict(path, &receipt); err != nil {
		return OperationReceipt{}, true, err
	}
	if receipt.SchemaVersion != ReceiptSchema ||
		receipt.OperationID != journal.OperationID ||
		receipt.Kind != "clean_baseline_data_reset" ||
		receipt.Status != "completed" ||
		journal.ReceiptAt == nil ||
		!receipt.CompletedAt.Equal(*journal.ReceiptAt) ||
		receipt.DatabaseSetRef != journal.DatabaseSetAfter ||
		receipt.BackupPath != journal.BackupPath ||
		receipt.BackupSHA256 != journal.BackupSHA256 ||
		receipt.ReasonCode != journal.ReasonCode ||
		receipt.Before == nil || journal.Before == nil || *receipt.Before != *journal.Before ||
		receipt.After == nil || journal.Target == nil || *receipt.After != *journal.Target ||
		receipt.DataReset == nil ||
		receipt.DataReset.SchemaVersion != CleanBaselineReceiptSchema ||
		journal.CleanCredentialPreview == nil ||
		receipt.DataReset.Credential.Before != *journal.CleanCredentialPreview ||
		receipt.DataReset.CheckpointFloor.SealedSHA256 != journal.CleanCheckpointFloorSHA256 ||
		journal.CleanBackupRestoreProof == nil ||
		receipt.DataReset.BackupRestore != *journal.CleanBackupRestoreProof ||
		!journal.CleanRestoreDatabaseOwned ||
		journal.CleanPredecessorDatabase == "" || journal.CleanRestoreDatabase == "" ||
		hashCleanBaselineText(journal.CleanPredecessorDatabase) != receipt.DataReset.PredecessorDatabaseSHA256 ||
		hashCleanBaselineText(journal.CleanRestoreDatabase) != receipt.DataReset.BackupRestore.VerificationDatabaseSHA256 {
		return OperationReceipt{}, true, fmt.Errorf("clean-baseline durable receipt does not match the operation journal")
	}
	if err := validateCleanBaselineDurableEvidence(receipt.DataReset, receipt.BackupSHA256); err != nil {
		return OperationReceipt{}, true, err
	}
	return receipt, true, nil
}

func (engine *Engine) resumeCleanBaselineOperation(
	ctx context.Context,
	journal *Journal,
) (OperationReceipt, error) {
	receipt, exists, err := engine.readCleanBaselineDurableReceipt(*journal)
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("clean-baseline can resume only after an exact durable receipt: %w", err)
	}
	if !exists {
		return OperationReceipt{}, fmt.Errorf("clean-baseline can resume only after an exact durable receipt")
	}
	activeSet, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
	if err != nil || activeSet != journal.DatabaseSetAfter {
		return OperationReceipt{}, fmt.Errorf("clean-baseline active database set differs from its durable receipt")
	}
	accountSHA256, _, err := fileSHA256(engine.Paths.AccessAccountsFile, access.MaxAccountFileBytes)
	if err != nil || accountSHA256 != journal.CleanAccountSHA256 {
		return OperationReceipt{}, fmt.Errorf("clean-baseline protected account readback differs from its durable receipt")
	}
	credentials, err := LoadDatabaseCredentials(engine.Paths)
	if err != nil || credentials.SetRef != journal.DatabaseSetAfter {
		return OperationReceipt{}, fmt.Errorf("clean-baseline active database credentials differ from its durable receipt")
	}
	state, err := engine.readInstallState()
	if err != nil {
		return OperationReceipt{}, err
	}
	current, err := engine.verifyStateV2(state.Current)
	if err != nil {
		return OperationReceipt{}, err
	}
	databaseProof, err := engine.CleanBaseline.VerifyCleanBaselineDatabase(
		ctx, credentials, current.Bundle.Manifest.MigrationChecksums,
	)
	if err != nil || databaseProof != receipt.DataReset.Database {
		return OperationReceipt{}, fmt.Errorf("clean-baseline strict-empty database proof differs from its durable receipt")
	}
	if err := proveCleanBaselineCredentialAbsent(engine.Paths, journal.OperationID, false); err != nil {
		return OperationReceipt{}, err
	}
	if _, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.CompletedExecutionWorkerUnit); err != nil {
		return OperationReceipt{}, err
	}
	if _, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.LiteLLMWorkerUnit); err != nil {
		return OperationReceipt{}, err
	}
	serviceState, err := engine.systemdUnitProperty(ctx, "xinsight.service", "ActiveState")
	if err != nil || serviceState != "active" {
		return OperationReceipt{}, fmt.Errorf("clean-baseline replacement service is not active")
	}
	if err := engine.runPhase(journal, "retire_predecessor_database", func() error {
		return engine.retireCleanBaselinePredecessorDatabase(ctx, journal, &receipt)
	}); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.runPhase(journal, "retire_access_account_predecessor", func() error {
		return retireCleanBaselineAccountPredecessor(
			engine.Paths,
			journal.OperationID,
			journal.CleanPriorAccountSHA256,
		)
	}); err != nil {
		return OperationReceipt{}, err
	}
	if journal.CleanCredentialPreview == nil {
		return OperationReceipt{}, fmt.Errorf("connector credential/config preview is missing from DATA_RESET journal")
	}
	if err := engine.runPhase(journal, "retire_connector_campaign_state_quarantine", func() error {
		return retireCleanBaselineCredentialQuarantine(
			engine.Paths,
			journal.OperationID,
			*journal.CleanCredentialPreview,
		)
	}); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.completeJournal(journal); err != nil {
		return OperationReceipt{}, err
	}
	return receipt, nil
}

func validateCleanBaselineAccountPredecessor(
	paths Paths,
	operationID string,
	expectedSHA256 string,
) error {
	if !sha256ReferencePattern.MatchString(expectedSHA256) {
		return fmt.Errorf("protected access account predecessor identity is invalid")
	}
	path := cleanBaselineAccountBackupPath(paths, operationID)
	digest, _, err := fileSHA256(path, access.MaxAccountFileBytes)
	if err != nil || digest != expectedSHA256 {
		return fmt.Errorf("protected access account predecessor differs from authorized preview")
	}
	if _, err := access.ReadAndValidateAccountFile(path); err != nil {
		return fmt.Errorf("protected access account predecessor metadata is invalid")
	}
	return nil
}

func retireCleanBaselineAccountPredecessor(
	paths Paths,
	operationID string,
	expectedSHA256 string,
) error {
	path := cleanBaselineAccountBackupPath(paths, operationID)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := validateCleanBaselineAccountPredecessor(paths, operationID, expectedSHA256); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func (engine *Engine) abortCleanBaselineOperation(
	ctx context.Context,
	journal *Journal,
) (OperationReceipt, error) {
	if _, exists, err := engine.readCleanBaselineDurableReceipt(*journal); err != nil {
		return OperationReceipt{}, err
	} else if exists {
		return OperationReceipt{}, fmt.Errorf("completed clean-baseline cutover cannot be aborted")
	}
	credentials, err := LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetBefore)
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("load clean-baseline rollback database set: %w", err)
	}
	if journal.CleanRestoreDatabaseOwned && journal.CleanRestoreDatabase == "" {
		return OperationReceipt{}, fmt.Errorf("isolated restore database identity is missing")
	}
	if journal.CleanRestoreDatabaseOwned {
		if err := engine.CleanBaseline.RetireCleanBaselineVerificationDatabase(
			ctx,
			credentials,
			journal.CleanRestoreDatabase,
		); err != nil {
			return OperationReceipt{}, fmt.Errorf("retire isolated restore database before abort: %w", err)
		}
	}
	if journal.Phase != "planned" || len(journal.CompletedPhases) != 0 {
		if err := runSystemctl(ctx, engine.Runner, "stop", "xinsight.service"); err != nil {
			return OperationReceipt{}, fmt.Errorf("stop service for clean-baseline abort: %w", err)
		}
	}
	activeSet, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
	if err != nil {
		return OperationReceipt{}, err
	}
	if activeSet != journal.DatabaseSetBefore {
		if activeSet != journal.DatabaseSetAfter {
			return OperationReceipt{}, fmt.Errorf("clean-baseline database set drift blocks abort")
		}
		if err := atomicSymlink(journal.DatabaseSetBefore, engine.Paths.DatabaseCurrentLink); err != nil {
			return OperationReceipt{}, err
		}
	}
	accountBackup := cleanBaselineAccountBackupPath(engine.Paths, journal.OperationID)
	if backupExists, err := fileExists(accountBackup); err != nil {
		return OperationReceipt{}, err
	} else if containsString(journal.CompletedPhases, "replace_access_accounts") && !backupExists {
		return OperationReceipt{}, fmt.Errorf("protected access account predecessor is missing")
	} else if backupExists {
		if err := replaceFilePreservingPrevious(
			accountBackup,
			engine.Paths.AccessAccountsFile,
			0o600,
			journal.OperationID+"-abort",
			access.MaxAccountFileBytes,
		); err != nil {
			return OperationReceipt{}, err
		}
		if _, err := access.ReadAndValidateAccountFile(engine.Paths.AccessAccountsFile); err != nil {
			return OperationReceipt{}, err
		}
	}
	if journal.CleanCredentialPreview == nil {
		return OperationReceipt{}, fmt.Errorf("connector credential/config preview is missing from DATA_RESET journal")
	}
	if err := restoreCleanBaselineCredentialState(
		engine.Paths,
		journal.OperationID,
		*journal.CleanCredentialPreview,
	); err != nil {
		return OperationReceipt{}, fmt.Errorf("restore connector credential/config state: %w", err)
	}
	if err := engine.Database.EnsureRoles(ctx, credentials); err != nil {
		return OperationReceipt{}, fmt.Errorf("restore rollback database roles: %w", err)
	}
	if journal.Phase != "planned" || len(journal.CompletedPhases) != 0 {
		if err := runSystemctl(ctx, engine.Runner, "start", "xinsight.service"); err != nil {
			return OperationReceipt{}, fmt.Errorf("restart rollback source service: %w", err)
		}
	}
	if _, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.CompletedExecutionWorkerUnit); err != nil {
		return OperationReceipt{}, err
	}
	if _, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.LiteLLMWorkerUnit); err != nil {
		return OperationReceipt{}, err
	}
	now := cleanBaselineNow(engine)
	journal.Status = "aborted"
	journal.Phase = "aborted"
	journal.ErrorCode = ""
	journal.AllowedActions = []string{}
	journal.CompletedAt = &now
	if err := engine.updateJournal(journal); err != nil {
		return OperationReceipt{}, err
	}
	receipt := OperationReceipt{
		SchemaVersion: ReceiptSchema, OperationID: journal.OperationID,
		Kind: "clean_baseline_data_reset", Status: "aborted", Before: journal.Before, After: journal.Before,
		BackupPath: journal.BackupPath, BackupSHA256: journal.BackupSHA256,
		DatabaseSetRef: journal.DatabaseSetBefore, ReasonCode: journal.ReasonCode,
		Checks: []CheckResult{
			{Name: "rollback_source", Status: "pass", Detail: "database_link_account_and_connector_credential_config_restored"},
			{Name: "clean_target", Status: "pass", Detail: "preserved_for_forensic_review_no_reuse"},
			{Name: "workers", Status: "pass", Detail: "both_inactive_disabled"},
		},
		CompletedAt: now,
	}
	if err := engine.writeReceipt(receipt); err != nil {
		return OperationReceipt{}, err
	}
	return receipt, nil
}
