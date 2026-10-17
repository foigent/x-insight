package hostctl

import (
	"context"
	"fmt"
	"path/filepath"
)

func (e *Engine) prepareBackup(ctx context.Context, path string) (PreflightReport, verifiedTarget, DatabaseCredentials, error) {
	report := PreflightReport{SchemaVersion: PreflightSchema, Operation: "backup", Mutating: false, GeneratedAt: e.now()}
	var empty verifiedTarget
	var creds DatabaseCredentials
	if err := e.ensureNoOutstandingOperation(); err != nil {
		return report, empty, creds, err
	}
	if _, err := detectSupportedHost(e.Paths); err != nil {
		return report, empty, creds, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return report, empty, creds, fmt.Errorf("backup output must be an exact absolute path")
	}
	if present, err := fileExists(path); err != nil || present {
		return report, empty, creds, fmt.Errorf("backup output already exists")
	}
	if err := e.validateReferenceParent(path); err != nil {
		return report, empty, creds, err
	}
	state, err := e.readInstallState()
	if err != nil {
		return report, empty, creds, err
	}
	current, err := e.verifyStateV2(state.Current)
	if err != nil {
		return report, empty, creds, err
	}
	link, err := readAbsoluteSymlink(e.Paths.CurrentLink)
	if err != nil || link != current.Identity.ReleaseDir {
		return report, empty, creds, fmt.Errorf("current release pointer drift")
	}
	creds, err = LoadDatabaseCredentials(e.Paths)
	if err != nil {
		return report, empty, creds, err
	}
	facts, err := e.Database.Inspect(ctx, creds)
	if err != nil || facts.PostgresMajor != 16 {
		return report, empty, creds, fmt.Errorf("backup requires compatible PostgreSQL16")
	}
	if err = validateLedgerPrefix(facts.MigrationLedger, current.Bundle.Manifest.MigrationChecksums, false); err != nil {
		return report, empty, creds, err
	}
	if _, _, present, err := e.custodyBinding(); err != nil {
		return report, empty, creds, err
	} else if !present {
		if err = e.Database.VerifyReviewBodyInventory(ctx, creds); err != nil {
			return report, empty, creds, err
		}
	}
	if err = e.verifyRestoredReviewBodyKeys(ctx, creds); err != nil {
		return report, empty, creds, err
	}
	if err = e.checkHostTools(ctx, false, true); err != nil {
		return report, empty, creds, err
	}
	if err = e.checkDisk(current, facts); err != nil {
		return report, empty, creds, err
	}
	report.Current, report.Target = &current.Identity, &current.Identity
	report.Checks = []CheckResult{{Name: "custody", Status: "pass", Detail: "original_key_verified_or_no_ciphertext"}, {Name: "backup_output", Status: "pass", Detail: "new_owner_only_private_bundle"}}
	report.RequiredActions = []string{"pause managed writers within existing maintenance boundary", "create private PG16 dump plus required original body key", "verify complete bundle and restore prior service state"}
	report.Supported = true
	return report, current, creds, nil
}
func (e *Engine) CheckBackup(ctx context.Context, path string) (PreflightReport, error) {
	r, _, _, err := e.prepareBackup(ctx, path)
	return r, err
}
func (e *Engine) Backup(ctx context.Context, path string) (OperationReceipt, error) {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return OperationReceipt{}, err
	}
	var receipt OperationReceipt
	err := e.withOperationLock(func() error {
		_, current, creds, err := e.prepareBackup(ctx, path)
		if err != nil {
			return err
		}
		j, err := e.startJournal("backup", current.Identity.ArtifactPath, "", &current.Identity, &current.Identity)
		if err != nil {
			return err
		}
		j.BackupPath = path
		j.BackupIsPrivate = true
		j.DatabaseSetBefore = creds.SetRef
		if err = e.updateJournal(j); err != nil {
			return err
		}
		receipt, err = e.executeBackup(ctx, j, current, creds)
		return err
	})
	return receipt, err
}
func (e *Engine) executeBackup(ctx context.Context, j *Journal, current verifiedTarget, creds DatabaseCredentials) (OperationReceipt, error) {
	fail := func(code string, err error) (OperationReceipt, error) {
		return OperationReceipt{}, e.operationFailed(j, code, err)
	}
	if err := e.ensureControlDirectories(); err != nil {
		return fail("directory_staging_failed", err)
	}
	if err := e.captureBackupServices(ctx, j); err != nil {
		return fail("backup_runtime_capture_failed", err)
	}
	if err := e.captureAndStopFeedWorker(ctx, j); err != nil {
		return OperationReceipt{}, err
	}
	if err := e.captureAndStopSourceFactsWorker(ctx, j); err != nil {
		return OperationReceipt{}, err
	}
	if err := e.captureAndStopLiteLLMWorker(ctx, j); err != nil {
		return OperationReceipt{}, err
	}
	if err := e.stopConnectorControllerForMaintenance(ctx, j); err != nil {
		return OperationReceipt{}, err
	}
	if err := e.runPhase(j, "stop_service", func() error { return runSystemctl(ctx, e.Runner, "stop", "xinsight.service") }); err != nil {
		return fail("service_stop_failed", err)
	}
	m, err := e.acquireAndGateMaintenance(ctx, j, creds, false, true)
	if err != nil {
		return OperationReceipt{}, err
	}
	defer m.Close()
	var b BackupResult
	if j.BackupSHA256 != "" {
		bundle, err := e.inspectPrivateBackup(ctx, j.BackupPath, j.BackupSHA256)
		if err != nil {
			return fail("private_backup_revalidation_failed", err)
		}
		bundle.Clear()
		b = BackupResult{Path: j.BackupPath, SHA256: j.BackupSHA256}
	} else {
		b, err = e.writePrivateBackup(ctx, j.BackupPath, j, creds, current)
		if err != nil {
			return fail("private_backup_failed", err)
		}
		j.BackupSHA256 = b.SHA256
		if err = e.completePhase(j, "backup_database"); err != nil {
			return fail("backup_phase_finalize_failed", err)
		}
	}
	if j.BackupImporterWasLogin == nil {
		return fail("backup_importer_state_missing", fmt.Errorf("original importer state is missing"))
	}
	if err = m.SetRoleLogin(ctx, "xinsight_importer", *j.BackupImporterWasLogin); err != nil {
		return fail("importer_restore_failed", err)
	}
	if err = e.restoreBackupServices(ctx, j); err != nil {
		return fail("backup_service_restore_failed", err)
	}
	if j.BackupServiceWasActive {
		if _, err = e.proveActivatedRelease(ctx, j, current.Identity, current.Identity.DatabaseSchema, targetRequiresAccessAccountCredential(current)); err != nil {
			return fail("backup_service_restore_proof_failed", err)
		}
	}
	if err = e.restartFeedWorkerAfterMaintenance(ctx, j, current.Identity.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	if err = e.restartSourceFactsWorkerAfterMaintenance(ctx, j, current.Identity.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	if err = e.restartLiteLLMWorkerAfterMaintenance(ctx, j, current.Identity.ReleaseDir); err != nil {
		return OperationReceipt{}, err
	}
	receipt := OperationReceipt{SchemaVersion: ReceiptSchema, OperationID: j.OperationID, Kind: "backup", Status: "completed", Before: j.Before, After: j.Before, BackupPath: b.Path, BackupSHA256: b.SHA256, DatabaseSetRef: creds.SetRef, Checks: []CheckResult{{Name: "private_backup", Status: "pass", Detail: "verified_dump_and_required_original_custody"}}, CompletedAt: e.now()}
	if err = e.finalizeOperation(j, &receipt); err != nil {
		return fail("receipt_finalize_failed", err)
	}
	return receipt, nil
}

func (e *Engine) captureBackupServices(ctx context.Context, j *Journal) error {
	if j.BackupRuntimeStateCaptured {
		return nil
	}
	// Capture before stopping any writer; the journal remains the only recovery state.
	active, err := e.systemdUnitProperty(ctx, "xinsight.service", "ActiveState")
	if err != nil {
		return err
	}
	switch active {
	case "active", "inactive", "failed":
	default:
		return fmt.Errorf("backup requires stable main service state")
	}
	controller, err := e.inspectManagedWorkerState(ctx, connectorControllerUnitName, e.Paths.ControllerServiceUnit, "connector controller")
	if err != nil {
		return err
	}
	j.BackupServiceWasActive = active == "active"
	j.BackupControllerWasActive = controller.active
	j.BackupRuntimeStateCaptured = true
	return e.updateJournal(j)
}
func (e *Engine) restoreBackupServices(ctx context.Context, j *Journal) error {
	if !j.BackupRuntimeStateCaptured {
		return fmt.Errorf("original backup runtime state is missing")
	}
	// start preserves enablement; normal release installation's enable --now
	// is inappropriate for a backup of an intentionally stopped/disabled unit.
	if j.BackupControllerWasActive {
		if err := runSystemctl(ctx, e.Runner, "start", connectorControllerUnitName); err != nil {
			return err
		}
		if err := e.waitForBackupConnectorControllerRuntime(ctx); err != nil {
			return err
		}
	}
	if j.BackupServiceWasActive {
		return runSystemctl(ctx, e.Runner, "start", "xinsight.service")
	}
	return nil
}
