package hostctl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/foigent/x-insight/internal/legacyrelease"
	"github.com/foigent/x-insight/internal/releasebundle"
)

func (engine *Engine) Install(ctx context.Context, artifact string) (OperationReceipt, error) {
	return engine.InstallWithRecovery(ctx, artifact, "", "")
}
func (engine *Engine) InstallWithRecovery(ctx context.Context, artifact, recoveryPath, recoverySHA string) (OperationReceipt, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return OperationReceipt{}, err
	}
	var receipt OperationReceipt
	err := engine.withOperationLock(func() error {
		_, plan, err := engine.prepareInstall(ctx, artifact)
		if err != nil {
			return err
		}
		var recovery *PrivateBackup
		if recoveryPath != "" {
			recovery, err = engine.inspectPrivateBackup(ctx, recoveryPath, recoverySHA)
			if err != nil {
				return err
			}
			defer recovery.Clear()
			if err = validateBackupRelease(recovery, plan.Target); err != nil {
				return err
			}
		}
		if err = engine.checkCustodyPreparation(ctx, plan.Credentials, plan.Target.Identity, recovery); err != nil {
			return err
		}
		journal, err := engine.startJournal("install", artifact, "", nil, &plan.Target.Identity)
		if err != nil {
			return err
		}
		if recoveryPath != "" {
			bundle, err := engine.inspectPrivateBackup(ctx, recoveryPath, recoverySHA)
			if err != nil {
				return err
			}
			defer bundle.Clear()
			if !custodySupported(plan.Target.Identity) {
				return fmt.Errorf("release does not support recovery custody preparation")
			}
			if err = validateBackupRelease(bundle, plan.Target); err != nil {
				return err
			}
			journal.RecoveryBundlePath, journal.RecoveryBundleSHA256 = recoveryPath, recoverySHA
			if bundle.Manifest.Custody != nil {
				if existing, _, present, err := engine.custodyBinding(); err != nil {
					return err
				} else if present {
					journal.CustodyCandidate = &existing
				} else {
					journal.CustodyCandidate = &ReviewBodyConfig{bundle.Manifest.Custody.KeyID, filepath.Join(engine.Paths.EtcDir, "review-body.key")}
				}
			}
		}
		journal.DatabaseSetBefore = plan.Credentials.SetRef
		if err := engine.updateJournal(journal); err != nil {
			return err
		}
		receipt, err = engine.executeInstall(ctx, journal, plan)
		return err
	})
	return receipt, err
}

func (engine *Engine) Adopt(ctx context.Context, targetArtifact string, legacyArtifact string) (OperationReceipt, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return OperationReceipt{}, err
	}
	var receipt OperationReceipt
	err := engine.withOperationLock(func() error {
		_, plan, err := engine.prepareAdopt(ctx, targetArtifact, legacyArtifact)
		if err != nil {
			return err
		}
		journal, err := engine.startJournal("adopt", targetArtifact, legacyArtifact, nil, &plan.Identity)
		if err != nil {
			return err
		}
		journal.DatabaseSetBefore = plan.Credentials.SetRef
		if err := engine.updateJournal(journal); err != nil {
			return err
		}
		receipt, err = engine.executeAdopt(ctx, journal, plan)
		return err
	})
	return receipt, err
}

func (engine *Engine) Upgrade(ctx context.Context, artifact string) (OperationReceipt, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return OperationReceipt{}, err
	}
	var receipt OperationReceipt
	err := engine.withOperationLock(func() error {
		_, plan, err := engine.prepareUpgrade(ctx, artifact)
		if err != nil {
			return err
		}
		before := plan.State.Current
		journal, err := engine.startJournal("upgrade", artifact, "", &before, &plan.Target.Identity)
		if err != nil {
			return err
		}
		journal.DatabaseSetBefore = plan.Credentials.SetRef
		if err := engine.updateJournal(journal); err != nil {
			return err
		}
		receipt, err = engine.executeUpgrade(ctx, journal, plan)
		return err
	})
	return receipt, err
}

func (engine *Engine) DirectFPV9Upgrade(ctx context.Context, artifact string, sourceProof string) (OperationReceipt, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return OperationReceipt{}, err
	}
	var receipt OperationReceipt
	err := engine.withOperationLock(func() error {
		_, plan, err := engine.prepareDirectFPV9Upgrade(ctx, artifact, sourceProof)
		if err != nil {
			return err
		}
		_, proofDigest, err := readDirectFPV9Proof(sourceProof)
		if err != nil || proofDigest != plan.DirectSourceProofSHA256 {
			return fmt.Errorf("direct FPV9 source proof changed after preflight")
		}
		before := plan.State.Current
		journal, err := engine.startJournal("upgrade_direct_fpv9", artifact, "", &before, &plan.Target.Identity)
		if err != nil {
			return err
		}
		plan.State.ManagedAt = journal.StartedAt
		journal.SourceProofPath, journal.SourceProofSHA256 = sourceProof, proofDigest
		journal.DatabaseSetBefore = plan.Credentials.SetRef
		if err := engine.updateJournal(journal); err != nil {
			return err
		}
		receipt, err = engine.executeUpgrade(ctx, journal, plan)
		return err
	})
	return receipt, err
}

func (engine *Engine) Rollback(ctx context.Context, reasonCode string) (OperationReceipt, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return OperationReceipt{}, err
	}
	if reasonCode != "failed_readiness" && reasonCode != "field_validation" && reasonCode != "operator_request" {
		return OperationReceipt{}, fmt.Errorf("rollback requires --reason-code failed_readiness|field_validation|operator_request")
	}
	var receipt OperationReceipt
	err := engine.withOperationLock(func() error {
		_, plan, err := engine.prepareRollback(ctx)
		if err != nil {
			return err
		}
		before := plan.State.Current
		target := *plan.State.Previous
		journal, err := engine.startJournal("rollback", target.ArtifactPath, "", &before, &target)
		if err != nil {
			return err
		}
		journal.DatabaseSetBefore = plan.Credentials.SetRef
		journal.ReasonCode = reasonCode
		if err := engine.updateJournal(journal); err != nil {
			return err
		}
		receipt, err = engine.executeRollback(ctx, journal, plan)
		return err
	})
	return receipt, err
}

func (engine *Engine) Restore(ctx context.Context, backup string, backupSHA256 string, newDatabase string, reasonCode string) (OperationReceipt, error) {
	return engine.restoreMode(ctx, backup, backupSHA256, newDatabase, reasonCode, false)
}
func (engine *Engine) RestoreBundle(ctx context.Context, backup, backupSHA256, newDatabase, reasonCode string) (OperationReceipt, error) {
	return engine.restoreMode(ctx, backup, backupSHA256, newDatabase, reasonCode, true)
}
func (engine *Engine) restoreMode(ctx context.Context, backup string, backupSHA256 string, newDatabase string, reasonCode string, private bool) (OperationReceipt, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return OperationReceipt{}, err
	}
	if reasonCode != "database_recovery" && reasonCode != "field_validation" && reasonCode != "operator_request" {
		return OperationReceipt{}, fmt.Errorf("restore requires --reason-code database_recovery|field_validation|operator_request")
	}
	var receipt OperationReceipt
	err := engine.withOperationLock(func() error {
		_, plan, err := engine.prepareRestoreMode(ctx, backup, backupSHA256, newDatabase, private)
		if err != nil {
			return err
		}
		before := plan.State.Current
		journal, err := engine.startJournal("restore", before.ArtifactPath, "", &before, &before)
		if err != nil {
			return err
		}
		journal.BackupIsPrivate = private
		if plan.Private != nil {
			defer plan.Private.Clear()
		}
		journal.BackupPath = backup
		journal.BackupSHA256 = backupSHA256
		journal.RestoreDatabase = newDatabase
		journal.DatabaseSetBefore = plan.Credentials.SetRef
		journal.ReasonCode = reasonCode
		if err := engine.updateJournal(journal); err != nil {
			return err
		}
		receipt, err = engine.executeRestore(ctx, journal, plan)
		return err
	})
	return receipt, err
}

func (engine *Engine) executeInstall(ctx context.Context, journal *Journal, plan installPlan) (OperationReceipt, error) {
	if err := engine.ensureControlDirectories(); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "directory_staging_failed", err)
	}
	maintenance, err := engine.acquireAndGateMaintenance(ctx, journal, plan.Credentials, true, false)
	if err != nil {
		return OperationReceipt{}, err
	}
	defer maintenance.Close()

	if err := engine.runPhase(journal, "stage_release", func() error {
		return engine.stageRelease(plan.Target)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "release_staging_failed", err)
	}
	if err := engine.runPhase(journal, "migrate_database", func() error {
		_, err := maintenance.RunMigrations(ctx, filepath.Join(plan.Target.Identity.ReleaseDir, "migrations"))
		return err
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "database_migration_failed", err)
	}
	if err := engine.runPhase(journal, "apply_role_grants", func() error {
		if err := engine.Database.ApplyGrants(ctx, plan.Credentials); err != nil {
			return err
		}
		return engine.verifyRoleBoundariesAndRegate(ctx, maintenance, plan.Credentials)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "database_role_boundary_failed", err)
	}
	if err := engine.runPhase(journal, "install_runtime_files", func() error {
		return engine.installRuntimeFiles(plan.Target.Identity.ReleaseDir, journal.OperationID, runtimeConfigInitialize)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "runtime_file_install_failed", err)
	}
	if err := engine.prepareCustody(ctx, journal, plan.Credentials, plan.Target.Identity); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "custody_preparation_failed", err)
	}
	if err := engine.runPhase(journal, "activate_release", func() error {
		return atomicSymlink(plan.Target.Identity.ReleaseDir, engine.Paths.CurrentLink)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "release_activation_failed", err)
	}
	if err := engine.runPhase(journal, "start_service", func() error {
		if err := runSystemctl(ctx, engine.Runner, "daemon-reload"); err != nil {
			return err
		}
		if err := engine.startConnectorControllerForRelease(ctx, plan.Target.Identity.ReleaseDir); err != nil {
			return err
		}
		return runSystemctl(ctx, engine.Runner, "enable", "--now", "xinsight.service")
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "service_start_failed", err)
	}
	proofChecks, err := engine.proveActivatedRelease(
		ctx,
		journal,
		plan.Target.Identity,
		plan.Target.Identity.DatabaseSchema,
		targetRequiresAccessAccountCredential(plan.Target),
	)
	if err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "activation_proof_failed", err)
	}
	now := engine.now()
	state := InstallState{
		Current: plan.Target.Identity, LastOperationID: journal.OperationID,
		DatabaseSchemaCurrent: plan.Target.Identity.DatabaseSchema,
		ManagedAt:             journal.StartedAt, UpdatedAt: now,
	}
	if err := engine.runPhase(journal, "write_install_state", func() error {
		return engine.writeInstallState(state)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "install_state_write_failed", err)
	}
	if err := engine.runPhase(journal, "enable_importer", func() error {
		return maintenance.SetRoleLogin(ctx, "xinsight_importer", true)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "importer_enable_failed", err)
	}
	receipt := OperationReceipt{
		SchemaVersion: ReceiptSchema, OperationID: journal.OperationID, Kind: "install", Status: "completed",
		After: &plan.Target.Identity, MigrationIDs: migrationIDs(plan.Target.Bundle.Manifest.MigrationChecksums),
		DatabaseSetRef: plan.Credentials.SetRef,
		Checks: append([]CheckResult{
			{Name: "release_signature", Status: "pass", Detail: plan.Target.Identity.KeyID},
			{Name: "database_roles", Status: "pass", Detail: "reader_importer_admin_separated"},
		}, proofChecks...),
		CompletedAt: now,
	}
	if err := engine.finalizeOperation(journal, &receipt); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "receipt_finalize_failed", err)
	}
	return receipt, nil
}

func (engine *Engine) executeAdopt(ctx context.Context, journal *Journal, plan adoptPlan) (OperationReceipt, error) {
	if err := engine.ensureControlDirectories(); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "directory_staging_failed", err)
	}
	if err := engine.runPhase(journal, "stop_legacy_service", func() error {
		return runSystemctl(ctx, engine.Runner, "stop", "xinsight.service")
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "legacy_service_stop_failed", err)
	}
	maintenance, err := engine.acquireAndGateMaintenance(ctx, journal, plan.Credentials, true, true)
	if err != nil {
		return OperationReceipt{}, err
	}
	defer maintenance.Close()
	effectiveCredentials := plan.Credentials
	if plan.WorkspaceCredentialBase != nil {
		effectiveCredentials, err = engine.expandWorkspaceDatabaseBoundary(
			ctx,
			journal,
			maintenance,
			*plan.WorkspaceCredentialBase,
		)
		if err != nil {
			return OperationReceipt{}, err
		}
	}
	if err := engine.runPhase(journal, "adopt_migration_checksums", func() error {
		_, err := maintenance.AdoptLegacyMigrationChecksums(ctx, filepath.Join(plan.Identity.ReleaseDir, "migrations"), plan.LegacySource.MigrationChecksums)
		return err
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "legacy_checksum_adoption_failed", err)
	}
	if err := engine.runPhase(journal, "apply_role_grants", func() error {
		if err := engine.Database.ApplyGrants(ctx, effectiveCredentials); err != nil {
			return err
		}
		return engine.verifyRoleBoundariesAndRegate(ctx, maintenance, effectiveCredentials)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "database_role_boundary_failed", err)
	}
	now := engine.now()
	state := InstallState{
		Current: plan.Identity, DatabaseSchemaCurrent: plan.Identity.DatabaseSchema,
		LastOperationID: journal.OperationID, ManagedAt: journal.StartedAt, UpdatedAt: now,
	}
	if err := engine.runPhase(journal, "write_install_state", func() error {
		return engine.writeInstallState(state)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "install_state_write_failed", err)
	}
	if err := engine.runPhase(journal, "enable_importer", func() error {
		return maintenance.SetRoleLogin(ctx, "xinsight_importer", true)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "importer_enable_failed", err)
	}
	receipt := OperationReceipt{
		SchemaVersion: ReceiptSchema, OperationID: journal.OperationID, Kind: "adopt", Status: "completed",
		After: &plan.Identity, MigrationIDs: migrationIDs(plan.LegacySource.MigrationChecksums),
		DatabaseSetRef: effectiveCredentials.SetRef,
		Checks: []CheckResult{
			{Name: "legacy_artifact", Status: "pass", Detail: plan.LegacySource.ReleaseID},
			{Name: "installed_payload", Status: "pass", Detail: "exact_file_set_match"},
			{Name: "migration_checksums", Status: "pass", Detail: "legacy_ledger_adopted"},
			{Name: "service_state", Status: "pass", Detail: "stopped_pending_managed_upgrade"},
		},
		CompletedAt: now,
	}
	if err := engine.finalizeOperation(journal, &receipt); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "receipt_finalize_failed", err)
	}
	return receipt, nil
}

func (engine *Engine) executeUpgrade(ctx context.Context, journal *Journal, plan upgradePlan) (OperationReceipt, error) {
	if err := engine.ensureControlDirectories(); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "directory_staging_failed", err)
	}
	if err := engine.captureAndStopFeedWorker(ctx, journal); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.captureAndStopSourceFactsWorker(ctx, journal); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.captureAndStopLiteLLMWorker(ctx, journal); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.stopConnectorControllerForMaintenance(ctx, journal); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.runPhase(journal, "stop_service", func() error {
		return runSystemctl(ctx, engine.Runner, "stop", "xinsight.service")
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "service_stop_failed", err)
	}
	maintenance, err := engine.acquireAndGateMaintenance(ctx, journal, plan.Credentials, false, true)
	if err != nil {
		return OperationReceipt{}, err
	}
	defer maintenance.Close()
	effectiveCredentials := plan.Credentials
	beforeFingerprint, err := engine.Database.EvidenceFingerprint(ctx, effectiveCredentials)
	if err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "evidence_fingerprint_failed", err)
	}
	if plan.DirectSource != nil && !containsString(journal.CompletedPhases, "write_install_state") {
		if journal.BeforeEvidenceFingerprintSHA256 == "" {
			if journal.Phase == "migrate_database" || containsString(journal.CompletedPhases, "migrate_database") {
				return OperationReceipt{}, engine.operationFailed(journal, "source_evidence_baseline_missing", fmt.Errorf("direct FPV9 migration began without an evidence baseline"))
			}
			journal.BeforeEvidenceFingerprintSHA256 = beforeFingerprint.SHA256
			if err := engine.updateJournal(journal); err != nil {
				return OperationReceipt{}, engine.operationFailed(journal, "journal_update_failed", err)
			}
		} else if beforeFingerprint.SHA256 != journal.BeforeEvidenceFingerprintSHA256 {
			return OperationReceipt{}, engine.operationFailed(journal, "source_evidence_changed", fmt.Errorf("direct FPV9 evidence differs from the pre-migration baseline"))
		}
	}
	if err := engine.runPhase(journal, "stage_release", func() error {
		return engine.stageRelease(plan.Target)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "release_staging_failed", err)
	}
	if !containsString(journal.CompletedPhases, "backup_database") {
		if err := engine.beginPhase(journal, "backup_database"); err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "journal_update_failed", err)
		}
		if journal.BackupPath == "" {
			journal.BackupPath = backupPath(engine.Paths, journal.OperationID, engine.now())
			journal.BackupIsPrivate = custodySupported(plan.Target.Identity) && plan.CurrentV2 != nil
			if journal.BackupIsPrivate {
				journal.BackupPath = journal.BackupPath + ".tar"
			}
			if err := engine.updateJournal(journal); err != nil {
				return OperationReceipt{}, engine.operationFailed(journal, "journal_update_failed", err)
			}
		}
		var backup BackupResult
		var err error
		if journal.BackupIsPrivate {
			backup, err = engine.writePrivateBackup(ctx, journal.BackupPath, journal, effectiveCredentials, *plan.CurrentV2)
		} else {
			backup, err = engine.Database.Backup(ctx, effectiveCredentials, journal.BackupPath)
		}
		if err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "database_backup_failed", err)
		}
		journal.BackupSHA256 = backup.SHA256
		if err := engine.completePhase(journal, "backup_database"); err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "backup_phase_finalize_failed", err)
		}
	}
	if plan.WorkspaceCredentialBase != nil {
		effectiveCredentials, err = engine.expandWorkspaceDatabaseBoundary(
			ctx,
			journal,
			maintenance,
			*plan.WorkspaceCredentialBase,
		)
		if err != nil {
			return OperationReceipt{}, err
		}
	}
	if plan.DirectSource != nil {
		if err := engine.runPhase(journal, "retire_direct_dropins", func() error {
			return engine.retireDirectFPV9DropIns(journal.OperationID, *plan.DirectSource)
		}); err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "direct_dropin_retirement_failed", err)
		}
	}
	if err := engine.runPhase(journal, "migrate_database", func() error {
		_, err := maintenance.RunMigrations(ctx, filepath.Join(plan.Target.Identity.ReleaseDir, "migrations"))
		return err
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "database_migration_failed", err)
	}
	if err := engine.runPhase(journal, "apply_role_grants", func() error {
		if err := engine.Database.ApplyGrants(ctx, effectiveCredentials); err != nil {
			return err
		}
		return engine.verifyRoleBoundariesAndRegate(ctx, maintenance, effectiveCredentials)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "database_role_boundary_failed", err)
	}
	configMode := runtimeConfigPreserve
	if plan.CurrentLegacy != nil {
		configMode = runtimeConfigReplaceLegacy
	} else if plan.DirectSource != nil {
		configMode = runtimeConfigInitialize
	}
	if err := engine.runPhase(journal, "install_runtime_files", func() error {
		return engine.installRuntimeFiles(plan.Target.Identity.ReleaseDir, journal.OperationID, configMode)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "runtime_file_install_failed", err)
	}
	if err := engine.prepareCustody(ctx, journal, effectiveCredentials, plan.Target.Identity); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "custody_preparation_failed", err)
	}
	if err := engine.runPhase(journal, "activate_release", func() error {
		return atomicSymlink(plan.Target.Identity.ReleaseDir, engine.Paths.CurrentLink)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "release_activation_failed", err)
	}
	if err := engine.runPhase(journal, "restart_service", func() error {
		if err := runSystemctl(ctx, engine.Runner, "daemon-reload"); err != nil {
			return err
		}
		if err := engine.startConnectorControllerForRelease(ctx, plan.Target.Identity.ReleaseDir); err != nil {
			return err
		}
		return runSystemctl(ctx, engine.Runner, "restart", "xinsight.service")
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "service_restart_failed", err)
	}
	proofChecks, err := engine.proveActivatedRelease(
		ctx,
		journal,
		plan.Target.Identity,
		plan.Target.Identity.DatabaseSchema,
		targetRequiresAccessAccountCredential(plan.Target),
	)
	if err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "activation_proof_failed", err)
	}
	if !(plan.DirectSource != nil && containsString(journal.CompletedPhases, "write_install_state")) {
		afterFingerprint, err := engine.Database.EvidenceFingerprint(ctx, effectiveCredentials)
		if err != nil || afterFingerprint != beforeFingerprint {
			return OperationReceipt{}, engine.operationFailed(journal, "evidence_integrity_changed", fmt.Errorf("representative evidence fingerprint changed during upgrade"))
		}
	}
	now := engine.now()
	previous := plan.State.Current
	state := InstallState{
		Current: plan.Target.Identity, Previous: &previous, LastOperationID: journal.OperationID,
		DatabaseSchemaCurrent: plan.Target.Identity.DatabaseSchema,
		ManagedAt:             plan.State.ManagedAt, UpdatedAt: now,
	}
	if plan.DirectSource != nil {
		state.Previous = nil // An unsigned direct-run source is never an application rollback target.
	}
	if err := engine.runPhase(journal, "write_install_state", func() error {
		return engine.writeInstallState(state)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "install_state_write_failed", err)
	}
	if err := engine.runPhase(journal, "enable_importer", func() error {
		return maintenance.SetRoleLogin(ctx, "xinsight_importer", true)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "importer_enable_failed", err)
	}
	if err := engine.restartFeedWorkerAfterMaintenance(ctx, journal, plan.Target.Identity.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.restartSourceFactsWorkerAfterMaintenance(ctx, journal, plan.Target.Identity.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.restartLiteLLMWorkerAfterMaintenance(ctx, journal, plan.Target.Identity.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	evidenceDetail := beforeFingerprint.SHA256
	if plan.DirectSource != nil {
		evidenceDetail = journal.BeforeEvidenceFingerprintSHA256
	}
	receipt := OperationReceipt{
		SchemaVersion: ReceiptSchema, OperationID: journal.OperationID, Kind: "upgrade", Status: "completed",
		Before: &previous, After: &plan.Target.Identity,
		BackupPath: journal.BackupPath, BackupSHA256: journal.BackupSHA256,
		MigrationIDs: migrationIDs(plan.Target.Bundle.Manifest.MigrationChecksums), DatabaseSetRef: effectiveCredentials.SetRef,
		Checks: append([]CheckResult{
			{Name: "release_signature", Status: "pass", Detail: plan.Target.Identity.KeyID},
			{Name: "backup", Status: "pass", Detail: journal.BackupSHA256},
			{Name: "migration_ledger", Status: "pass", Detail: "exact_checksum_set"},
			{Name: "evidence_integrity", Status: "pass", Detail: evidenceDetail},
		}, proofChecks...),
		CompletedAt: now,
	}
	if plan.DirectSource != nil {
		receipt.Kind = "upgrade_direct_fpv9"
		receipt.Checks = append(receipt.Checks,
			CheckResult{Name: "source_kind", Status: "pass", Detail: directFPV9Kind},
			CheckResult{Name: "source_proof", Status: "pass", Detail: journal.SourceProofSHA256},
			CheckResult{Name: "direct_dropins", Status: "pass", Detail: "three_program_overrides_retired_egress_rebound_credentials_preserved"})
	}
	if err := engine.finalizeOperation(journal, &receipt); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "receipt_finalize_failed", err)
	}
	return receipt, nil
}

func (engine *Engine) expandWorkspaceDatabaseBoundary(
	ctx context.Context,
	journal *Journal,
	maintenance Maintenance,
	base DatabaseCredentials,
) (DatabaseCredentials, error) {
	if base.Workspace != nil || !workspaceCredentialBridgeAllowed(*journal) {
		return DatabaseCredentials{}, engine.operationFailed(
			journal,
			"workspace_credential_bridge_not_allowed",
			fmt.Errorf("workspace database credential bridge source is invalid"),
		)
	}
	if journal.DatabaseSetAfter == "" {
		if filepath.Base(journal.OperationID) != journal.OperationID {
			return DatabaseCredentials{}, engine.operationFailed(
				journal,
				"workspace_credential_bridge_path_invalid",
				fmt.Errorf("operation identity cannot name a credential set"),
			)
		}
		journal.DatabaseSetAfter = filepath.Join(
			engine.Paths.DatabaseSetsDir,
			"workspace-bridge-"+journal.OperationID,
		)
		if err := engine.updateJournal(journal); err != nil {
			return DatabaseCredentials{}, engine.operationFailed(journal, "journal_update_failed", err)
		}
	}
	var expanded DatabaseCredentials
	if err := engine.runPhase(journal, "stage_workspace_database_credentials", func() error {
		var stageErr error
		expanded, stageErr = StageWorkspaceDatabaseCredentialSet(
			engine.Paths,
			base,
			journal.DatabaseSetAfter,
		)
		return stageErr
	}); err != nil {
		return DatabaseCredentials{}, engine.operationFailed(journal, "workspace_credential_staging_failed", err)
	}
	if expanded.SetRef == "" {
		var loadErr error
		expanded, loadErr = LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetAfter)
		if loadErr != nil {
			return DatabaseCredentials{}, engine.operationFailed(journal, "workspace_credential_reload_failed", loadErr)
		}
	}
	if expanded.ReaderDSN != base.ReaderDSN || expanded.ImporterDSN != base.ImporterDSN || expanded.AdminDSN != base.AdminDSN {
		return DatabaseCredentials{}, engine.operationFailed(
			journal,
			"workspace_credential_inherited_identity_changed",
			fmt.Errorf("workspace database credential bridge changed an inherited credential"),
		)
	}
	if err := engine.runPhase(journal, "expand_workspace_database_boundary", func() error {
		if err := engine.Database.EnsureRoles(ctx, expanded); err != nil {
			return err
		}
		if err := engine.Database.ApplyGrants(ctx, expanded); err != nil {
			return err
		}
		if err := engine.verifyRoleBoundariesAndRegate(ctx, maintenance, expanded); err != nil {
			return err
		}
		return atomicSymlink(expanded.SetRef, engine.Paths.DatabaseCurrentLink)
	}); err != nil {
		return DatabaseCredentials{}, engine.operationFailed(journal, "workspace_database_boundary_expansion_failed", err)
	}
	active, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
	if err != nil || active != expanded.SetRef {
		return DatabaseCredentials{}, engine.operationFailed(
			journal,
			"workspace_credential_activation_drift",
			fmt.Errorf("workspace database credential successor is not active"),
		)
	}
	return expanded, nil
}

func workspaceCredentialBridgeAllowed(journal Journal) bool {
	if journal.Kind == "upgrade" && journal.Before != nil {
		return journal.Before.LegacyReleaseID != "" ||
			isThreeRoleCredentialUpgradeSource(journal.Before.ProductVersion)
	}
	return journal.Kind == "adopt" && journal.Target != nil && journal.Target.LegacyReleaseID != ""
}

func (engine *Engine) executeRollback(ctx context.Context, journal *Journal, plan rollbackPlan) (OperationReceipt, error) {
	if err := engine.validateManagedConfigurationRelease(plan.State.Previous.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.captureAndStopFeedWorker(ctx, journal); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.captureAndStopSourceFactsWorker(ctx, journal); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.captureAndStopLiteLLMWorker(ctx, journal); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.stopConnectorControllerForMaintenance(ctx, journal); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.runPhase(journal, "stop_service", func() error {
		return runSystemctl(ctx, engine.Runner, "stop", "xinsight.service")
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "service_stop_failed", err)
	}
	maintenance, err := engine.acquireAndGateMaintenance(ctx, journal, plan.Credentials, false, true)
	if err != nil {
		return OperationReceipt{}, err
	}
	defer maintenance.Close()
	beforeFingerprint, err := engine.Database.EvidenceFingerprint(ctx, plan.Credentials)
	if err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "evidence_fingerprint_failed", err)
	}
	previous := *plan.State.Previous
	if err := engine.runPhase(journal, "activate_release", func() error {
		return atomicSymlink(previous.ReleaseDir, engine.Paths.CurrentLink)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "release_activation_failed", err)
	}
	if err := engine.runPhase(journal, "install_connector_controller_runtime", func() error {
		return engine.reconcileConnectorControllerUnitForRelease(ctx, previous.ReleaseDir, journal.OperationID)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "connector_controller_runtime_install_failed", err)
	}
	if err := engine.runPhase(journal, "install_feed_worker_runtime", func() error {
		return engine.installFeedWorkerUnitForRelease(previous.ReleaseDir, journal.OperationID)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "feed_worker_runtime_install_failed", err)
	}
	if err := engine.runPhase(journal, "install_source_facts_worker_runtime", func() error {
		return engine.installManagedWorkerUnitForRelease(previous.ReleaseDir, journal.OperationID, sourceFactsWorkerUnit, engine.Paths.SourceFactsServiceUnit, "source facts worker", false)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "source_facts_runtime_install_failed", err)
	}
	if err := engine.disableLiteLLMWorkerBeforeUnsupportedRuntime(
		ctx,
		journal,
		previous.ReleaseDir,
	); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.runPhase(journal, "install_litellm_worker_runtime", func() error {
		return engine.installLiteLLMWorkerUnitForRelease(previous.ReleaseDir, journal.OperationID)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "litellm_worker_runtime_install_failed", err)
	}
	if err := engine.runPhase(journal, "restart_service", func() error {
		if err := runSystemctl(ctx, engine.Runner, "daemon-reload"); err != nil {
			return err
		}
		if err := engine.startConnectorControllerForRelease(ctx, previous.ReleaseDir); err != nil {
			return err
		}
		return runSystemctl(ctx, engine.Runner, "restart", "xinsight.service")
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "service_restart_failed", err)
	}
	var proofChecks []CheckResult
	if previous.LegacyReleaseID == "" {
		proofChecks, err = engine.proveActivatedRelease(
			ctx,
			journal,
			previous,
			plan.State.DatabaseSchemaCurrent,
			plan.PreviousV2 != nil &&
				targetRequiresAccessAccountCredential(*plan.PreviousV2),
		)
	} else {
		proofChecks, err = engine.proveLegacyRollback(ctx, journal)
	}
	if err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "rollback_proof_failed", err)
	}
	afterFingerprint, err := engine.Database.EvidenceFingerprint(ctx, plan.Credentials)
	if err != nil || afterFingerprint != beforeFingerprint {
		return OperationReceipt{}, engine.operationFailed(journal, "evidence_integrity_changed", fmt.Errorf("representative evidence fingerprint changed during application rollback"))
	}
	now := engine.now()
	formerCurrent := plan.State.Current
	state := InstallState{
		Current: previous, Previous: &formerCurrent,
		DatabaseSchemaCurrent: plan.State.DatabaseSchemaCurrent,
		LastOperationID:       journal.OperationID, ManagedAt: plan.State.ManagedAt, UpdatedAt: now,
	}
	if err := engine.runPhase(journal, "write_install_state", func() error {
		return engine.writeInstallState(state)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "install_state_write_failed", err)
	}
	if err := engine.runPhase(journal, "enable_importer", func() error {
		return maintenance.SetRoleLogin(ctx, "xinsight_importer", true)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "importer_enable_failed", err)
	}
	if err := engine.restartFeedWorkerAfterMaintenance(ctx, journal, previous.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.restartSourceFactsWorkerAfterMaintenance(ctx, journal, previous.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.restartLiteLLMWorkerAfterMaintenance(ctx, journal, previous.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	receipt := OperationReceipt{
		SchemaVersion: ReceiptSchema, OperationID: journal.OperationID, Kind: "rollback", Status: "completed",
		Before: &formerCurrent, After: &previous, DatabaseSetRef: plan.Credentials.SetRef, ReasonCode: journal.ReasonCode,
		Checks: append([]CheckResult{
			{Name: "down_migration", Status: "pass", Detail: "not_executed"},
			{Name: "evidence_integrity", Status: "pass", Detail: beforeFingerprint.SHA256},
		}, proofChecks...),
		CompletedAt: now,
	}
	if err := engine.finalizeOperation(journal, &receipt); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "receipt_finalize_failed", err)
	}
	return receipt, nil
}

func (engine *Engine) executeRestore(ctx context.Context, journal *Journal, plan restorePlan) (OperationReceipt, error) {
	if err := engine.ensureControlDirectories(); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "directory_staging_failed", err)
	}
	if err := engine.captureAndStopFeedWorker(ctx, journal); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.captureAndStopSourceFactsWorker(ctx, journal); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.captureAndStopLiteLLMWorker(ctx, journal); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.stopConnectorControllerForMaintenance(ctx, journal); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.runPhase(journal, "stop_service", func() error {
		return runSystemctl(ctx, engine.Runner, "stop", "xinsight.service")
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "service_stop_failed", err)
	}
	maintenance, err := engine.acquireAndGateMaintenance(ctx, journal, plan.Credentials, false, true)
	if err != nil {
		return OperationReceipt{}, err
	}
	defer maintenance.Close()
	beforeFingerprint, err := engine.Database.EvidenceFingerprint(ctx, plan.Credentials)
	if err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "evidence_fingerprint_failed", err)
	}
	if journal.BeforeEvidenceFingerprintSHA256 == "" {
		journal.BeforeEvidenceFingerprintSHA256 = beforeFingerprint.SHA256
		if err = engine.updateJournal(journal); err != nil {
			return OperationReceipt{}, err
		}
	} else if journal.BeforeEvidenceFingerprintSHA256 != beforeFingerprint.SHA256 {
		return OperationReceipt{}, engine.operationFailed(journal, "original_target_evidence_changed", fmt.Errorf("original target representative evidence changed"))
	}
	backupForRestore := plan.Backup
	if plan.Private != nil {
		if err = engine.checkBackupKey(plan.Private); err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "restored_review_body_key_validation_failed", err)
		}
		backupForRestore, err = engine.extractPrivateDump(ctx, plan.Private, journal)
		if err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "private_backup_extract_failed", err)
		}
		beforeFingerprint = plan.Private.Manifest.Fingerprint
	}
	var restoredCredentials DatabaseCredentials
	if !containsString(journal.CompletedPhases, "stage_restore_credentials") {
		if err := engine.beginPhase(journal, "stage_restore_credentials"); err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "journal_update_failed", err)
		}
		restoredCredentials, err = StageRestoredCredentialSet(engine.Paths, plan.Credentials, plan.NewDatabase)
		if err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "restore_credential_staging_failed", err)
		}
		journal.DatabaseSetAfter = restoredCredentials.SetRef
		if err := engine.updateJournal(journal); err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "journal_update_failed", err)
		}
		if err := engine.completePhase(journal, "stage_restore_credentials"); err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "restore_credential_phase_finalize_failed", err)
		}
	} else {
		restoredCredentials, err = LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetAfter)
		if err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "restore_credential_revalidation_failed", err)
		}
	}
	if !containsString(journal.CompletedPhases, "restore_database") {
		if err := engine.beginPhase(journal, "restore_database"); err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "journal_update_failed", err)
		}
		if err := engine.Database.RestoreToNewDatabase(
			ctx,
			plan.Credentials,
			restoredCredentials,
			backupForRestore.Path,
			filepath.Join(plan.Current.Identity.ReleaseDir, "migrations"),
			plan.Current.Bundle.Manifest.MigrationChecksums,
		); err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "database_restore_failed", err)
		}
		if err := engine.completePhase(journal, "restore_database"); err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "restore_phase_finalize_failed", err)
		}
	} else if err := engine.verifyRestoredDatabase(ctx, restoredCredentials, plan.Current); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "restored_database_revalidation_failed", err)
	}
	if err := engine.verifyRoleBoundariesAndRegate(ctx, maintenance, restoredCredentials); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "restored_database_role_boundary_failed", err)
	}
	afterFingerprint, err := engine.Database.EvidenceFingerprint(ctx, restoredCredentials)
	if err != nil || afterFingerprint != beforeFingerprint {
		return OperationReceipt{}, engine.operationFailed(journal, "restored_evidence_integrity_mismatch", fmt.Errorf("restored evidence fingerprint does not match backup source"))
	}
	// Always recheck, including a resumed operation after restore_database was
	// finalized. A previous successful check cannot authorize a changed key.
	if err := engine.verifyRestoredReviewBodyKeys(ctx, restoredCredentials); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "restored_review_body_key_validation_failed", err)
	}
	if plan.Private != nil {
		targetDeployment, err := engine.ConnectorDeploymentID()
		if err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "restored_deployment_binding_unavailable", err)
		}
		if err = engine.Database.SuspendForeignRestoredAuthority(ctx, restoredCredentials, plan.Private.Manifest.DeploymentID, targetDeployment, engine.now()); err != nil {
			return OperationReceipt{}, engine.operationFailed(journal, "foreign_restored_authority_suspend_failed", err)
		}
	}

	if err := engine.runPhase(journal, "switch_database_credentials", func() error {
		return atomicSymlink(restoredCredentials.SetRef, engine.Paths.DatabaseCurrentLink)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "database_credential_switch_failed", err)
	}
	if err := engine.runPhase(journal, "restart_service", func() error {
		if err := engine.startConnectorControllerForRelease(ctx, plan.State.Current.ReleaseDir); err != nil {
			return err
		}
		return runSystemctl(ctx, engine.Runner, "restart", "xinsight.service")
	}); err != nil {
		_ = atomicSymlink(plan.Credentials.SetRef, engine.Paths.DatabaseCurrentLink)
		return OperationReceipt{}, engine.operationFailed(journal, "service_restart_failed_database_link_restored", err)
	}
	proofChecks, err := engine.proveActivatedRelease(
		ctx,
		journal,
		plan.State.Current,
		plan.State.DatabaseSchemaCurrent,
		targetRequiresAccessAccountCredential(plan.Current),
	)
	if err != nil {
		_ = runSystemctl(context.Background(), engine.Runner, "stop", "xinsight.service")
		_ = atomicSymlink(plan.Credentials.SetRef, engine.Paths.DatabaseCurrentLink)
		return OperationReceipt{}, engine.operationFailed(journal, "restore_activation_proof_failed_database_link_restored", err)
	}
	now := engine.now()
	state := plan.State
	state.DatabaseSchemaCurrent = plan.State.DatabaseSchemaCurrent
	state.LastOperationID = journal.OperationID
	state.UpdatedAt = now
	if err := engine.runPhase(journal, "write_install_state", func() error {
		return engine.writeInstallState(state)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "install_state_write_failed", err)
	}
	if err := engine.runPhase(journal, "enable_importer", func() error {
		return maintenance.SetRoleLogin(ctx, "xinsight_importer", true)
	}); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "importer_enable_failed", err)
	}
	if err := engine.restartFeedWorkerAfterMaintenance(ctx, journal, plan.State.Current.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.restartSourceFactsWorkerAfterMaintenance(ctx, journal, plan.State.Current.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	if err := engine.restartLiteLLMWorkerAfterMaintenance(ctx, journal, plan.State.Current.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	receipt := OperationReceipt{
		SchemaVersion: ReceiptSchema, OperationID: journal.OperationID, Kind: "restore", Status: "completed",
		Before: &plan.State.Current, After: &plan.State.Current,
		BackupPath: plan.Backup.Path, BackupSHA256: plan.Backup.SHA256,
		MigrationIDs:   migrationIDs(plan.Current.Bundle.Manifest.MigrationChecksums),
		DatabaseSetRef: restoredCredentials.SetRef, ReasonCode: journal.ReasonCode,
		Checks: append([]CheckResult{
			{Name: "restore_mode", Status: "pass", Detail: "new_database_no_in_place_overwrite"},
			{Name: "evidence_integrity", Status: "pass", Detail: beforeFingerprint.SHA256},
			{Name: "credential_switch", Status: "pass", Detail: "atomic_versioned_set_link"},
		}, proofChecks...),
		CompletedAt: now,
	}
	if err := engine.finalizeOperation(journal, &receipt); err != nil {
		return OperationReceipt{}, engine.operationFailed(journal, "receipt_finalize_failed", err)
	}
	return receipt, nil
}

func (engine *Engine) verifyRestoredDatabase(ctx context.Context, credentials DatabaseCredentials, current verifiedTarget) error {
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil || facts.PostgresMajor != 16 {
		return fmt.Errorf("restored database profile is invalid")
	}
	if err := validateLedgerPrefix(facts.MigrationLedger, current.Bundle.Manifest.MigrationChecksums, false); err != nil {
		return err
	}
	return nil
}

func (engine *Engine) proveLegacyRollback(ctx context.Context, journal *Journal) ([]CheckResult, error) {
	if err := engine.runPhase(journal, "verify_service_launcher", func() error {
		return verifyEffectiveServiceLauncher(ctx, engine.Runner, engine.Paths)
	}); err != nil {
		return nil, err
	}
	if err := engine.runPhase(journal, "verify_listener", func() error {
		return engine.waitForLoopbackListener(ctx)
	}); err != nil {
		return nil, err
	}
	if err := engine.runPhase(journal, "verify_product_read", func() error {
		return engine.waitForActivationProof(ctx, func() error {
			contents, err := boundedGET(ctx, engine.HTTPClient, "http://127.0.0.1:8080/readyz")
			if err != nil {
				return err
			}
			var body struct {
				Status string `json:"status"`
				DB     string `json:"db"`
			}
			if err := json.Unmarshal(contents, &body); err != nil || body.Status != "ready" || body.DB != "ok" {
				return fmt.Errorf("legacy readiness proof failed")
			}
			return nil
		})
	}); err != nil {
		return nil, err
	}
	return []CheckResult{
		{Name: "service_launcher", Status: "pass", Detail: "effective_control_launcher"},
		{Name: "listener", Status: "pass", Detail: "127.0.0.1:8080_only"},
		{Name: "legacy_readiness", Status: "pass", Detail: "database_ready"},
	}, nil
}

func (engine *Engine) acquireAndGateMaintenance(ctx context.Context, journal *Journal, credentials DatabaseCredentials, ensureRoles bool, includeAdminSessions bool) (Maintenance, error) {
	if err := engine.beginPhase(journal, "exclusive_maintenance"); err != nil {
		return nil, engine.operationFailed(journal, "journal_update_failed", err)
	}
	maintenance, err := engine.Database.AcquireMaintenance(ctx, credentials)
	if err != nil {
		return nil, engine.operationFailed(journal, "maintenance_lock_failed", err)
	}
	if ensureRoles {
		if err := engine.Database.EnsureRoles(ctx, credentials); err != nil {
			_ = maintenance.Close()
			return nil, engine.operationFailed(journal, "database_role_setup_failed", err)
		}
	}
	if journal.Kind == "backup" && journal.BackupImporterWasLogin == nil {
		reader, ok := engine.Database.(interface {
			ImporterRoleLogin(context.Context, DatabaseCredentials) (bool, error)
		})
		if !ok {
			_ = maintenance.Close()
			return nil, engine.operationFailed(journal, "importer_state_unavailable", fmt.Errorf("backup requires original importer login state"))
		}
		login, err := reader.ImporterRoleLogin(ctx, credentials)
		if err != nil {
			_ = maintenance.Close()
			return nil, engine.operationFailed(journal, "importer_state_inspection_failed", err)
		}
		journal.BackupImporterWasLogin = &login
		if err = engine.updateJournal(journal); err != nil {
			_ = maintenance.Close()
			return nil, engine.operationFailed(journal, "journal_update_failed", err)
		}
	}
	priorLogin := true
	if journal.Kind == "backup" && journal.BackupImporterWasLogin != nil {
		priorLogin = *journal.BackupImporterWasLogin
	}
	if err := maintenance.SetRoleLogin(ctx, "xinsight_importer", false); err != nil {
		_ = maintenance.Close()
		return nil, engine.operationFailed(journal, "importer_gate_failed", err)
	}
	roles := []string{"xinsight_reader", "xinsight_workspace", "xinsight_importer"}
	if includeAdminSessions {
		roles = append(roles, "xinsight_admin")
	}
	sessions, err := maintenance.CountOtherSessions(ctx, roles)
	if err != nil || sessions != 0 {
		_ = maintenance.SetRoleLogin(context.Background(), "xinsight_importer", priorLogin)
		_ = maintenance.Close()
		if err != nil {
			return nil, engine.operationFailed(journal, "writer_session_check_failed", err)
		}
		return nil, engine.operationFailed(journal, "writer_sessions_present", fmt.Errorf("database sessions remain after service stop"))
	}
	if err := engine.completePhase(journal, "exclusive_maintenance"); err != nil {
		_ = maintenance.SetRoleLogin(context.Background(), "xinsight_importer", priorLogin)
		_ = maintenance.Close()
		return nil, engine.operationFailed(journal, "maintenance_phase_finalize_failed", err)
	}
	return maintenance, nil
}

func (engine *Engine) verifyRoleBoundariesAndRegate(ctx context.Context, maintenance Maintenance, credentials DatabaseCredentials) error {
	if err := maintenance.SetRoleLogin(ctx, "xinsight_importer", true); err != nil {
		return err
	}
	if err := engine.Database.VerifyRoleBoundaries(ctx, credentials); err != nil {
		_ = maintenance.SetRoleLogin(context.Background(), "xinsight_importer", false)
		return err
	}
	return maintenance.SetRoleLogin(ctx, "xinsight_importer", false)
}

func (engine *Engine) stageRelease(target verifiedTarget) error {
	if exists, err := fileExists(target.Identity.ReleaseDir); err != nil {
		return err
	} else if !exists {
		if err := releasebundle.ExtractPayload(target.Bundle, target.Identity.ReleaseDir); err != nil {
			return err
		}
	}
	return releasebundle.VerifyExtractedPayload(target.Bundle, target.Identity.ReleaseDir)
}

type runtimeConfigMode string

const (
	runtimeConfigInitialize    runtimeConfigMode = "initialize"
	runtimeConfigReplaceLegacy runtimeConfigMode = "replace_legacy"
	runtimeConfigPreserve      runtimeConfigMode = "preserve"
)

func (engine *Engine) installRuntimeFiles(releaseDir string, operationID string, configMode runtimeConfigMode) error {
	if err := engine.validateManagedConfigurationRelease(releaseDir); err != nil {
		return err
	}
	managedDropInSource := filepath.Join(releaseDir, "deploy", managedServiceDropInName)
	if err := validateManagedServiceDropInSource(managedDropInSource); err != nil {
		return err
	}
	files := []struct {
		source      string
		destination string
		mode        os.FileMode
	}{
		{source: filepath.Join(releaseDir, "bin", "xinsight-launcher"), destination: engine.Paths.ServiceLauncher, mode: 0o755},
		{source: filepath.Join(releaseDir, "deploy", "xinsight.service"), destination: engine.Paths.ServiceUnit, mode: 0o644},
		{source: managedDropInSource, destination: engine.Paths.ManagedServiceDropIn, mode: 0o644},
		{source: filepath.Join(releaseDir, "deploy", "postgres.compose.yml"), destination: engine.Paths.PostgresCompose, mode: 0o644},
	}
	for _, file := range files {
		payloadPath, err := filepath.Rel(releaseDir, file.source)
		if err != nil {
			return err
		}
		if err := replaceFilePreservingPrevious(file.source, file.destination, file.mode, operationID, releasebundle.RuntimeFileLimit(filepath.ToSlash(payloadPath))); err != nil {
			return err
		}
	}
	if err := engine.installFeedWorkerUnitForRelease(releaseDir, operationID); err != nil {
		return err
	}
	if err := engine.installManagedWorkerUnitForRelease(releaseDir, operationID, sourceFactsWorkerUnit, engine.Paths.SourceFactsServiceUnit, "source facts worker", false); err != nil {
		return err
	}
	if err := engine.installLiteLLMWorkerUnitForRelease(releaseDir, operationID); err != nil {
		return err
	}
	if err := engine.installConnectorControllerUnitForRelease(releaseDir, operationID); err != nil {
		return err
	}
	environmentSource := filepath.Join(releaseDir, "deploy", "xinsight.env.example")
	if err := validateManagedEnvironmentFile(environmentSource); err != nil {
		return err
	}
	var environmentErr error
	switch configMode {
	case runtimeConfigInitialize:
		if exists, err := fileExists(engine.Paths.EnvironmentFile); err != nil {
			return err
		} else if exists {
			if err := validateManagedEnvironmentFile(engine.Paths.EnvironmentFile); err != nil {
				return fmt.Errorf("fresh install environment staging drift: %w", err)
			}
			environmentErr = requireMatchingFiles(environmentSource, engine.Paths.EnvironmentFile, maxEnvironmentFileBytes)
		} else {
			environmentErr = copyFileExclusive(environmentSource, engine.Paths.EnvironmentFile, 0o644, maxEnvironmentFileBytes)
		}
	case runtimeConfigReplaceLegacy:
		if err := validateManagedEnvironmentFile(engine.Paths.EnvironmentFile); err == nil {
			environmentErr = requireMatchingFiles(environmentSource, engine.Paths.EnvironmentFile, maxEnvironmentFileBytes)
		} else {
			if err := validateLegacyEnvironmentFile(engine.Paths.EnvironmentFile); err != nil {
				return err
			}
			environmentErr = replaceFilePreservingPrevious(environmentSource, engine.Paths.EnvironmentFile, 0o644, operationID, maxEnvironmentFileBytes)
		}
	case runtimeConfigPreserve:
		environmentErr = validateManagedEnvironmentFile(engine.Paths.EnvironmentFile)
	default:
		return fmt.Errorf("runtime config installation mode is invalid")
	}
	if environmentErr != nil {
		return environmentErr
	}
	return engine.ensureConnectorIdentityEnvironmentFile()
}

func requireMatchingFiles(left string, right string, limit int64) error {
	leftDigest, leftSize, err := fileSHA256(left, limit)
	if err != nil {
		return err
	}
	rightDigest, rightSize, err := fileSHA256(right, limit)
	if err != nil {
		return err
	}
	if leftDigest != rightDigest || leftSize != rightSize {
		return fmt.Errorf("managed runtime file staging drift")
	}
	return nil
}

func (engine *Engine) proveActivatedRelease(
	ctx context.Context,
	journal *Journal,
	identity ReleaseIdentity,
	databaseSchema string,
	accessProtected bool,
) ([]CheckResult, error) {
	var checks []CheckResult
	controllerSupported, err := connectorControllerSupportedByRelease(identity.ReleaseDir)
	if err != nil {
		return nil, err
	}
	if controllerSupported {
		if err := engine.runPhase(journal, "verify_connector_controller", func() error {
			return engine.proveConnectorControllerRuntime(ctx)
		}); err != nil {
			return nil, err
		}
		checks = append(checks, CheckResult{Name: "connector_controller", Status: "pass", Detail: "root_service_socket_ready"})
	}
	if err := engine.runPhase(journal, "verify_service_launcher", func() error {
		return verifyEffectiveServiceLauncher(ctx, engine.Runner, engine.Paths)
	}); err != nil {
		return nil, err
	}
	checks = append(checks, CheckResult{Name: "service_launcher", Status: "pass", Detail: "effective_control_launcher"})
	if err := engine.runPhase(journal, "verify_listener", func() error {
		return engine.waitForLoopbackListener(ctx)
	}); err != nil {
		return nil, err
	}
	checks = append(checks, CheckResult{Name: "listener", Status: "pass", Detail: "127.0.0.1:8080_only"})
	var httpChecks []CheckResult
	if err := engine.runPhase(journal, "verify_product_read", func() error {
		return engine.waitForActivationProof(ctx, func() error {
			var err error
			expected := identity
			expected.DatabaseSchema = databaseSchema
			httpChecks, err = proveHTTPReadiness(
				ctx,
				engine.HTTPClient,
				expected,
				accessProtected,
			)
			return err
		})
	}); err != nil {
		return nil, err
	}
	checks = append(checks, httpChecks...)
	if custodySupported(identity) {
		_, active, _, err := engine.custodyBinding()
		if err != nil {
			return nil, err
		}
		if active {
			if err = engine.runPhase(journal, "verify_review_body_reader_loaded", func() error {
				return engine.waitForActivationProof(ctx, func() error { return engine.proveReviewBodyLoadedConsumers(ctx, false) })
			}); err != nil {
				return nil, err
			}
			checks = append(checks, CheckResult{Name: "review_body_reader_loaded", Status: "pass", Detail: "current_invocation_proved_original_key_bytes"})
		}
	}
	return checks, nil
}

func (engine *Engine) runPhase(journal *Journal, phase string, action func() error) error {
	if containsString(journal.CompletedPhases, phase) {
		return action()
	}
	if err := engine.beginPhase(journal, phase); err != nil {
		return err
	}
	if err := action(); err != nil {
		return err
	}
	return engine.completePhase(journal, phase)
}

func (engine *Engine) operationFailed(journal *Journal, code string, cause error) error {
	allowed := []string{"recover", "resume"}
	if !(journal.Kind == "upgrade_direct_fpv9" && journal.Phase == "migrate_database") &&
		!containsString(journal.CompletedPhases, "migrate_database") && !containsString(journal.CompletedPhases, "activate_release") && !containsString(journal.CompletedPhases, "restore_database") && journal.Phase != "restore_database" && !containsString(journal.CompletedPhases, "expand_workspace_database_boundary") && journal.Phase != "expand_workspace_database_boundary" {
		allowed = append(allowed, "abort")
	}
	if err := engine.failJournal(journal, code, allowed...); err != nil {
		return fmt.Errorf("%s; operation journal update also failed", code)
	}
	return fmt.Errorf("%s: %w", code, cause)
}

func (engine *Engine) finalizeOperation(journal *Journal, receipt *OperationReceipt) error {
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
	return engine.completeJournal(journal)
}

func (engine *Engine) ensureControlDirectories() error {
	for _, directory := range []struct {
		path string
		mode os.FileMode
	}{
		{filepath.Dir(engine.Paths.ReleasesDir), 0o755},
		{engine.Paths.ReleasesDir, 0o755},
		{engine.Paths.ControlDir, 0o755},
		{engine.Paths.ServiceDropInDir, 0o755},
		{engine.Paths.StateDir, 0o700},
		{engine.Paths.OperationsDir, 0o700},
		{engine.Paths.ReceiptsDir, 0o700},
		{engine.Paths.BackupsDir, 0o700},
	} {
		if err := ensureDirectory(directory.path, directory.mode); err != nil {
			return err
		}
	}
	return nil
}

func (engine *Engine) Abort(ctx context.Context) (OperationReceipt, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return OperationReceipt{}, err
	}
	var receipt OperationReceipt
	err := engine.withOperationLock(func() error {
		journal, err := engine.readActiveJournal()
		if err != nil {
			return err
		}
		if journal.Status != "active" && journal.Status != "failed" {
			return fmt.Errorf("there is no incomplete operation to abort")
		}
		if journal.Kind == "clean_baseline_data_reset" {
			receipt, err = engine.abortCleanBaselineOperation(ctx, &journal)
			return err
		}
		if journal.Kind == "checkpoint_bootstrap" {
			receipt, err = engine.abortCheckpointBootstrapOperation(ctx, &journal)
			return err
		}
		if journal.Kind == adminCredentialRotationKind {
			receipt, err = engine.abortAdminCredentialRotationOperation(ctx, &journal)
			return err
		}
		if (journal.Kind == "upgrade_direct_fpv9" && journal.Phase == "migrate_database") ||
			containsString(journal.CompletedPhases, "migrate_database") || containsString(journal.CompletedPhases, "activate_release") || containsString(journal.CompletedPhases, "restore_database") || journal.Phase == "restore_database" || containsString(journal.CompletedPhases, "expand_workspace_database_boundary") || journal.Phase == "expand_workspace_database_boundary" {
			return fmt.Errorf("operation crossed a database or activation barrier and cannot be marked aborted")
		}
		if err := engine.validateAbortAccessAccountCredential(journal); err != nil {
			return err
		}
		if journal.Kind == "upgrade_direct_fpv9" &&
			(containsString(journal.CompletedPhases, "retire_direct_dropins") || journal.Phase == "retire_direct_dropins") {
			proof, digest, err := readDirectFPV9Proof(journal.SourceProofPath)
			if err != nil || digest != journal.SourceProofSHA256 {
				return fmt.Errorf("direct FPV9 source proof changed before abort")
			}
			if err := engine.restoreDirectFPV9DropIns(journal.OperationID, proof); err != nil {
				return err
			}
			if err := runSystemctl(ctx, engine.Runner, "daemon-reload"); err != nil {
				return err
			}
		}
		if journal.Kind == "backup" && journal.BackupImporterWasLogin != nil {
			credentials, err := LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetBefore)
			if err != nil {
				return err
			}
			m, err := engine.Database.AcquireMaintenance(ctx, credentials)
			if err != nil {
				return err
			}
			err = m.SetRoleLogin(ctx, "xinsight_importer", *journal.BackupImporterWasLogin)
			_ = m.Close()
			if err != nil {
				return err
			}
		} else if containsString(journal.CompletedPhases, "exclusive_maintenance") {
			allowThreeRole := workspaceCredentialBridgeAllowed(journal)
			credentials, _, err := LoadUpgradeDatabaseCredentials(engine.Paths, allowThreeRole)
			if err != nil {
				return err
			}
			if err := engine.Database.EnsureRoles(ctx, credentials); err != nil {
				return fmt.Errorf("cannot restore importer login before abort: %w", err)
			}
		}
		if journal.Kind == "backup" {
			if journal.BackupRuntimeStateCaptured {
				if err := engine.restoreBackupServices(ctx, &journal); err != nil {
					return err
				}
			}
		} else if containsString(journal.CompletedPhases, "stop_connector_controller") && journal.Before != nil {
			if err := engine.startConnectorControllerForRelease(ctx, journal.Before.ReleaseDir); err != nil {
				return fmt.Errorf("cannot restore pre-operation connector controller before abort: %w", err)
			}
		}
		if journal.Kind != "backup" && (containsString(journal.CompletedPhases, "stop_service") || containsString(journal.CompletedPhases, "stop_legacy_service")) {
			if err := runSystemctl(ctx, engine.Runner, "start", "xinsight.service"); err != nil {
				return fmt.Errorf("cannot restore pre-operation service before abort: %w", err)
			}
		}
		if containsString(journal.CompletedPhases, "stop_source_facts_worker") && journal.SourceFactsWorkerWasActive {
			if err := runSystemctl(ctx, engine.Runner, "start", sourceFactsWorkerUnit); err != nil {
				return err
			}
		}
		if containsString(journal.CompletedPhases, "stop_feed_worker") && journal.FeedWorkerWasActive {
			if err := runSystemctl(ctx, engine.Runner, "start", feedWorkerUnitName); err != nil {
				return fmt.Errorf("cannot restore pre-operation feed worker before abort: %w", err)
			}
		}
		if containsString(journal.CompletedPhases, "stop_litellm_worker") &&
			journal.LiteLLMWorkerWasActive {
			if err := runSystemctl(ctx, engine.Runner, "start", liteLLMWorkerUnitName); err != nil {
				return fmt.Errorf("cannot restore pre-operation LiteLLM worker before abort: %w", err)
			}
		}
		now := engine.now()
		journal.Status = "aborted"
		journal.Phase = "aborted"
		journal.ErrorCode = ""
		journal.AllowedActions = []string{}
		journal.CompletedAt = &now
		if err := engine.updateJournal(&journal); err != nil {
			return err
		}
		receipt = OperationReceipt{
			SchemaVersion: ReceiptSchema, OperationID: journal.OperationID, Kind: journal.Kind, Status: "aborted",
			Before: journal.Before, After: journal.Before, BackupPath: journal.BackupPath, BackupSHA256: journal.BackupSHA256,
			DatabaseSetRef: journal.DatabaseSetBefore,
			Checks:         []CheckResult{{Name: "automatic_cleanup", Status: "pass", Detail: "no_files_databases_backups_or_evidence_deleted"}},
			CompletedAt:    now,
		}
		return engine.writeReceipt(receipt)
	})
	return receipt, err
}

func (engine *Engine) Recover(ctx context.Context) (StatusReport, error) {
	return engine.Status(ctx)
}

func (engine *Engine) Resume(ctx context.Context) (OperationReceipt, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return OperationReceipt{}, err
	}
	var receipt OperationReceipt
	err := engine.withOperationLock(func() error {
		journal, err := engine.readActiveJournal()
		if err != nil {
			return err
		}
		if journal.Status != "active" && journal.Status != "failed" {
			return fmt.Errorf("there is no incomplete operation to resume")
		}
		if journal.Kind == adminCredentialRotationKind {
			var resumeErr error
			receipt, resumeErr = engine.resumeAdminCredentialRotationOperation(ctx, &journal)
			return resumeErr
		}
		if err := engine.validateResumeAccessAccountCredential(journal); err != nil {
			return err
		}
		journal.Status = "active"
		journal.ErrorCode = ""
		journal.AllowedActions = []string{"recover", "resume"}
		if err := engine.updateJournal(&journal); err != nil {
			return err
		}
		switch journal.Kind {
		case "review_body_load":
			receipt, err = engine.executeCustodyLoad(ctx, &journal)
			return err
		case "backup":
			if journal.Before == nil {
				return fmt.Errorf("backup journal incomplete")
			}
			current, err := engine.verifyStateV2(*journal.Before)
			if err != nil {
				return err
			}
			creds, err := LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetBefore)
			if err != nil {
				return err
			}
			receipt, err = engine.executeBackup(ctx, &journal, current, creds)
			return err
		case "install":
			plan, err := engine.resumeInstallPlan(ctx, journal)
			if err != nil {
				return engine.operationFailed(&journal, "resume_preflight_failed", err)
			}
			receipt, err = engine.executeInstall(ctx, &journal, plan)
			return err
		case "adopt":
			plan, err := engine.resumeAdoptPlan(ctx, journal)
			if err != nil {
				return engine.operationFailed(&journal, "resume_preflight_failed", err)
			}
			receipt, err = engine.executeAdopt(ctx, &journal, plan)
			return err
		case "upgrade":
			plan, err := engine.resumeUpgradePlan(ctx, journal)
			if err != nil {
				return engine.operationFailed(&journal, "resume_preflight_failed", err)
			}
			receipt, err = engine.executeUpgrade(ctx, &journal, plan)
			return err
		case "upgrade_direct_fpv9":
			plan, err := engine.resumeDirectFPV9UpgradePlan(ctx, journal)
			if err != nil {
				return engine.operationFailed(&journal, "resume_preflight_failed", err)
			}
			receipt, err = engine.executeUpgrade(ctx, &journal, plan)
			return err
		case "rollback":
			plan, err := engine.resumeRollbackPlan(ctx, journal)
			if err != nil {
				return engine.operationFailed(&journal, "resume_preflight_failed", err)
			}
			receipt, err = engine.executeRollback(ctx, &journal, plan)
			return err
		case "restore":
			plan, err := engine.resumeRestorePlan(ctx, &journal)
			if err != nil {
				return engine.operationFailed(&journal, "resume_preflight_failed", err)
			}
			if plan.Private != nil {
				defer plan.Private.Clear()
			}
			receipt, err = engine.executeRestore(ctx, &journal, plan)
			return err
		case "clean_baseline_data_reset":
			var resumeErr error
			receipt, resumeErr = engine.resumeCleanBaselineOperation(ctx, &journal)
			if resumeErr != nil {
				if failErr := engine.failJournal(&journal, "resume_preflight_failed", "recover", "resume"); failErr != nil {
					return fmt.Errorf("resume_preflight_failed: %v; operation journal update failed: %w", resumeErr, failErr)
				}
				return fmt.Errorf("resume_preflight_failed: %w", resumeErr)
			}
			return nil
		case "checkpoint_bootstrap":
			var resumeErr error
			receipt, resumeErr = engine.resumeCheckpointBootstrapOperation(ctx, &journal)
			if resumeErr != nil {
				if failErr := engine.failJournal(&journal, "resume_preflight_failed", "recover", "resume"); failErr != nil {
					return fmt.Errorf("resume_preflight_failed: %v; operation journal update failed: %w", resumeErr, failErr)
				}
				return fmt.Errorf("resume_preflight_failed: %w", resumeErr)
			}
			return nil
		default:
			return engine.operationFailed(&journal, "resume_not_supported", fmt.Errorf("operation kind requires an explicit recovery decision"))
		}
	})
	return receipt, err
}

func (engine *Engine) resumeInstallPlan(ctx context.Context, journal Journal) (installPlan, error) {
	target, err := engine.verifyTarget(journal.ArtifactPath)
	if err != nil || journal.Target == nil || target.Identity != *journal.Target {
		return installPlan{}, fmt.Errorf("resume target release identity changed")
	}
	credentials, err := LoadDatabaseCredentials(engine.Paths)
	if err != nil || credentials.SetRef != journal.DatabaseSetBefore {
		return installPlan{}, fmt.Errorf("resume database credential set changed")
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil || facts.PostgresMajor != 16 {
		return installPlan{}, fmt.Errorf("resume database profile changed")
	}
	if containsString(journal.CompletedPhases, "stage_release") {
		if err := releasebundle.VerifyExtractedPayload(target.Bundle, target.Identity.ReleaseDir); err != nil {
			return installPlan{}, err
		}
	}
	return installPlan{Target: target, Credentials: credentials, Facts: facts}, nil
}

func (engine *Engine) resumeAdoptPlan(ctx context.Context, journal Journal) (adoptPlan, error) {
	target, err := engine.verifyTarget(journal.ArtifactPath)
	if err != nil {
		return adoptPlan{}, err
	}
	legacy, source, err := engine.verifyLegacyArtifact(journal.LegacyArtifactPath, target)
	if err != nil {
		return adoptPlan{}, err
	}
	if journal.Target == nil {
		return adoptPlan{}, fmt.Errorf("resume legacy target identity is missing")
	}
	identity := legacyIdentity(journal.LegacyArtifactPath, journal.Target.ReleaseDir, legacy, source, target.Bundle.Manifest.Rollback.ApplicationRollbackCompatible)
	if identity != *journal.Target {
		return adoptPlan{}, fmt.Errorf("resume legacy identity changed")
	}
	if err := legacyrelease.VerifyInstalled(legacy, identity.ReleaseDir); err != nil {
		return adoptPlan{}, err
	}
	beforeCredentials, needsWorkspaceCredentialBridge, err := LoadUpgradeDatabaseCredentialSet(
		engine.Paths,
		journal.DatabaseSetBefore,
		true,
	)
	if err != nil {
		return adoptPlan{}, fmt.Errorf("resume database credential set changed")
	}
	activeSet, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
	if err != nil {
		return adoptPlan{}, fmt.Errorf("resume database credential set changed")
	}
	credentials := beforeCredentials
	switch activeSet {
	case journal.DatabaseSetBefore:
	case journal.DatabaseSetAfter:
		if journal.DatabaseSetAfter == "" || !needsWorkspaceCredentialBridge {
			return adoptPlan{}, fmt.Errorf("resume database credential set changed")
		}
		credentials, err = LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetAfter)
		if err != nil {
			return adoptPlan{}, fmt.Errorf("resume database credential set changed")
		}
	default:
		return adoptPlan{}, fmt.Errorf("resume database credential set changed")
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil || facts.PostgresMajor != 16 {
		return adoptPlan{}, fmt.Errorf("resume database profile changed")
	}
	plan := adoptPlan{Target: target, Legacy: legacy, LegacySource: source, Identity: identity, Credentials: credentials, Facts: facts}
	if needsWorkspaceCredentialBridge {
		base := beforeCredentials
		plan.WorkspaceCredentialBase = &base
	} else if journal.DatabaseSetAfter != "" {
		return adoptPlan{}, fmt.Errorf("resume database credential bridge state is invalid")
	}
	return plan, nil
}

func (engine *Engine) resumeUpgradePlan(ctx context.Context, journal Journal) (upgradePlan, error) {
	if journal.Before == nil || journal.Target == nil {
		return upgradePlan{}, fmt.Errorf("resume upgrade identities are missing")
	}
	target, err := engine.verifyTarget(journal.ArtifactPath)
	if err != nil || target.Identity != *journal.Target {
		return upgradePlan{}, fmt.Errorf("resume target release identity changed")
	}
	plan := upgradePlan{Target: target}
	if journal.Before.LegacyReleaseID != "" {
		legacy, source, err := engine.verifyLegacyArtifact(journal.Before.ArtifactPath, target)
		if err != nil || source.ReleaseID != journal.Before.LegacyReleaseID || legacy.ArtifactSHA256 != journal.Before.BundleSHA256 || legacy.ManifestSHA256 != journal.Before.ManifestSHA256 {
			return upgradePlan{}, fmt.Errorf("resume legacy release identity changed")
		}
		if err := legacyrelease.VerifyInstalled(legacy, journal.Before.ReleaseDir); err != nil {
			return upgradePlan{}, err
		}
		plan.CurrentLegacy = &legacy
		plan.LegacySource = &source
	} else {
		current, err := engine.verifyHistoricalStateV2(*journal.Before)
		if err != nil || releasebundle.VerifyExtractedPayload(current.Bundle, current.Identity.ReleaseDir) != nil {
			return upgradePlan{}, fmt.Errorf("resume current release identity changed")
		}
		plan.CurrentV2 = &current
	}
	allowThreeRole := journal.Before.LegacyReleaseID != "" ||
		isThreeRoleCredentialUpgradeSource(journal.Before.ProductVersion)
	beforeCredentials, needsWorkspaceCredentialBridge, err := LoadUpgradeDatabaseCredentialSet(
		engine.Paths,
		journal.DatabaseSetBefore,
		allowThreeRole,
	)
	if err != nil {
		return upgradePlan{}, fmt.Errorf("resume database credential set changed")
	}
	activeSet, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
	if err != nil {
		return upgradePlan{}, fmt.Errorf("resume database credential set changed")
	}
	credentials := beforeCredentials
	switch activeSet {
	case journal.DatabaseSetBefore:
	case journal.DatabaseSetAfter:
		if journal.DatabaseSetAfter == "" || !needsWorkspaceCredentialBridge {
			return upgradePlan{}, fmt.Errorf("resume database credential set changed")
		}
		credentials, err = LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetAfter)
		if err != nil {
			return upgradePlan{}, fmt.Errorf("resume database credential set changed")
		}
	default:
		return upgradePlan{}, fmt.Errorf("resume database credential set changed")
	}
	if needsWorkspaceCredentialBridge {
		base := beforeCredentials
		plan.WorkspaceCredentialBase = &base
	} else if journal.DatabaseSetAfter != "" {
		return upgradePlan{}, fmt.Errorf("resume database credential bridge state is invalid")
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil || facts.PostgresMajor != 16 {
		return upgradePlan{}, fmt.Errorf("resume database profile changed")
	}
	if containsString(journal.CompletedPhases, "stage_release") {
		if err := releasebundle.VerifyExtractedPayload(target.Bundle, target.Identity.ReleaseDir); err != nil {
			return upgradePlan{}, err
		}
	}
	if containsString(journal.CompletedPhases, "backup_database") {
		if journal.BackupPath == "" || journal.BackupSHA256 == "" {
			return upgradePlan{}, fmt.Errorf("completed backup phase is missing identity")
		}
		if journal.BackupIsPrivate {
			if plan.CurrentV2 == nil || journal.PrivateDumpSHA256 == "" || journal.PrivateFingerprint == nil {
				return upgradePlan{}, fmt.Errorf("private upgrade backup identity is incomplete")
			}
			backup, err := engine.inspectPrivateBackup(ctx, journal.BackupPath, journal.BackupSHA256)
			if err != nil {
				return upgradePlan{}, err
			}
			defer backup.Clear()
			if backup.Manifest.OperationID != journal.OperationID || backup.Manifest.Release != *journal.Before ||
				!slices.Equal(backup.Manifest.Migrations, plan.CurrentV2.Bundle.Manifest.MigrationChecksums) ||
				backup.Manifest.Dump.SHA256 != journal.PrivateDumpSHA256 || backup.Manifest.Fingerprint != *journal.PrivateFingerprint {
				return upgradePlan{}, fmt.Errorf("private upgrade backup is not the original snapshot")
			}
			if err := validateBackupRelease(backup, *plan.CurrentV2); err != nil {
				return upgradePlan{}, err
			}
			if err := engine.checkBackupKey(backup); err != nil {
				return upgradePlan{}, err
			}
		} else if _, err := engine.Database.VerifyBackup(ctx, journal.BackupPath, journal.BackupSHA256); err != nil {
			return upgradePlan{}, err
		}
	}
	if containsString(journal.CompletedPhases, "migrate_database") {
		if err := validateLedgerPrefix(facts.MigrationLedger, target.Bundle.Manifest.MigrationChecksums, false); err != nil {
			return upgradePlan{}, err
		}
	}
	managedAt := journal.StartedAt
	if state, err := engine.readInstallState(); err == nil {
		managedAt = state.ManagedAt
	}
	state := InstallState{Current: *journal.Before, DatabaseSchemaCurrent: target.Identity.DatabaseSchema, ManagedAt: managedAt}
	plan.State = state
	plan.Credentials = credentials
	plan.Facts = facts
	return plan, nil
}

func (engine *Engine) resumeRollbackPlan(ctx context.Context, journal Journal) (rollbackPlan, error) {
	if journal.Before == nil || journal.Target == nil {
		return rollbackPlan{}, fmt.Errorf("resume rollback identities are missing")
	}
	current, err := engine.verifyStateV2(*journal.Before)
	if err != nil || !current.Bundle.Manifest.Rollback.ApplicationRollbackCompatible {
		return rollbackPlan{}, fmt.Errorf("resume current release identity changed")
	}
	target := *journal.Target
	plan := rollbackPlan{Current: current}
	if target.LegacyReleaseID != "" {
		legacy, source, err := engine.verifyLegacyArtifact(target.ArtifactPath, current)
		if err != nil {
			return rollbackPlan{}, err
		}
		if source.ReleaseID != target.LegacyReleaseID || legacy.ArtifactSHA256 != target.BundleSHA256 || legacy.ManifestSHA256 != target.ManifestSHA256 {
			return rollbackPlan{}, fmt.Errorf("resume previous legacy identity changed")
		}
		if err := legacyrelease.VerifyInstalled(legacy, target.ReleaseDir); err != nil {
			return rollbackPlan{}, err
		}
		if err := verifyLegacyRollbackCompatibility(source, current.Identity.DatabaseSchema, current.Identity.ConfigSchema); err != nil {
			return rollbackPlan{}, err
		}
		plan.PreviousLegacy = &legacy
		plan.LegacySource = &source
	} else {
		previous, err := engine.verifyStateV2(target)
		if err != nil {
			return rollbackPlan{}, err
		}
		if err := releasebundle.VerifyExtractedPayload(previous.Bundle, target.ReleaseDir); err != nil {
			return rollbackPlan{}, err
		}
		plan.PreviousV2 = &previous
	}
	credentials, err := LoadDatabaseCredentials(engine.Paths)
	if err != nil || credentials.SetRef != journal.DatabaseSetBefore {
		return rollbackPlan{}, fmt.Errorf("resume database credential set changed")
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil || facts.PostgresMajor != 16 || validateLedgerPrefix(facts.MigrationLedger, current.Bundle.Manifest.MigrationChecksums, false) != nil {
		return rollbackPlan{}, fmt.Errorf("resume database state changed")
	}
	managedAt := journal.StartedAt
	databaseSchema := current.Identity.DatabaseSchema
	if state, err := engine.readInstallState(); err == nil {
		managedAt = state.ManagedAt
		databaseSchema = state.DatabaseSchemaCurrent
	}
	previous := target
	state := InstallState{Current: *journal.Before, Previous: &previous, DatabaseSchemaCurrent: databaseSchema, ManagedAt: managedAt}
	plan.State = state
	plan.Credentials = credentials
	plan.Facts = facts
	return plan, nil
}

func (engine *Engine) resumeRestorePlan(ctx context.Context, journal *Journal) (restorePlan, error) {
	if journal.Before == nil || journal.Target == nil || *journal.Before != *journal.Target ||
		journal.BackupPath == "" || journal.BackupSHA256 == "" || !restoreDatabasePattern.MatchString(journal.RestoreDatabase) ||
		journal.DatabaseSetBefore == "" {
		return restorePlan{}, fmt.Errorf("resume restore identity is incomplete")
	}
	current, err := engine.verifyStateV2(*journal.Before)
	if err != nil {
		return restorePlan{}, err
	}
	credentials, err := LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetBefore)
	if err != nil {
		return restorePlan{}, err
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil || facts.PostgresMajor != 16 || validateLedgerPrefix(facts.MigrationLedger, current.Bundle.Manifest.MigrationChecksums, false) != nil {
		return restorePlan{}, fmt.Errorf("resume restore source database changed")
	}
	var bundle *PrivateBackup
	var backup BackupResult
	if journal.BackupIsPrivate {
		bundle, err = engine.inspectPrivateBackup(ctx, journal.BackupPath, journal.BackupSHA256)
		if err == nil {
			err = validateBackupRelease(bundle, current)
		}
		if err == nil {
			err = engine.checkBackupKey(bundle)
		}
		if err == nil {
			backup = BackupResult{Path: bundle.Path, SHA256: bundle.SHA256, SizeBytes: bundle.Size, PGRestoreMajor: 16}
		}
	} else {
		backup, err = engine.Database.VerifyBackup(ctx, journal.BackupPath, journal.BackupSHA256)
	}
	if err != nil {
		return restorePlan{}, err
	}
	if journal.DatabaseSetAfter != "" {
		restored, err := LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetAfter)
		if err != nil {
			return restorePlan{}, err
		}
		exists, err := engine.Database.DatabaseExists(ctx, credentials, journal.RestoreDatabase)
		if err != nil {
			return restorePlan{}, err
		}
		if exists && !containsString(journal.CompletedPhases, "restore_database") {
			if err := engine.verifyRestoredDatabase(ctx, restored, current); err != nil {
				return restorePlan{}, fmt.Errorf("partial restore database exists and requires an explicit new restore target")
			}
			if err := engine.completePhase(journal, "restore_database"); err != nil {
				return restorePlan{}, err
			}
		}
	}
	managedAt := journal.StartedAt
	databaseSchema := current.Identity.DatabaseSchema
	if state, err := engine.readInstallState(); err == nil {
		managedAt = state.ManagedAt
		databaseSchema = state.DatabaseSchemaCurrent
	}
	state := InstallState{Current: *journal.Before, DatabaseSchemaCurrent: databaseSchema, ManagedAt: managedAt}
	return restorePlan{State: state, Current: current, Credentials: credentials, Facts: facts, Backup: backup, NewDatabase: journal.RestoreDatabase, Private: bundle}, nil
}
