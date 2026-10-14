package hostctl

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/foigent/x-insight/internal/checkpointepoch"
)

func (engine *Engine) executeCheckpointBootstrap(
	ctx context.Context,
	journal *Journal,
	plan checkpointBootstrapPlan,
) (receipt CheckpointBootstrapReceipt, resultErr error) {
	failureCode := "checkpoint_bootstrap_failed"
	operationFinalized := false
	defer func() {
		if resultErr == nil || operationFinalized {
			return
		}
		if _, exists, receiptErr := engine.readCheckpointBootstrapDurableReceipt(*journal); exists {
			if receiptErr != nil {
				_ = engine.failJournal(journal, "checkpoint_bootstrap_receipt_ambiguous", "recover")
				resultErr = fmt.Errorf("%s: %v; durable receipt is ambiguous: %w", failureCode, resultErr, receiptErr)
				return
			}
			_ = engine.failJournal(journal, failureCode, "recover", "resume")
			resultErr = fmt.Errorf("%s: %w", failureCode, resultErr)
			return
		}
		if _, verifyErr := engine.CleanBaseline.VerifyEmptyCheckpoint(
			ctx, plan.Credentials, plan.Input, plan.NextEpoch,
		); verifyErr == nil {
			_ = engine.failJournal(journal, failureCode, "recover", "resume")
			resultErr = fmt.Errorf("%s: %w", failureCode, resultErr)
			return
		}
		if _, resetErr := engine.CleanBaseline.VerifyCleanBaselineDatabase(
			ctx, plan.Credentials, plan.Current.Bundle.Manifest.MigrationChecksums,
		); resetErr == nil {
			_ = engine.failJournal(journal, failureCode, "recover", "resume", "abort")
			resultErr = fmt.Errorf("%s: %w", failureCode, resultErr)
			return
		}
		_ = engine.failJournal(journal, "checkpoint_bootstrap_state_ambiguous", "recover")
		resultErr = fmt.Errorf("checkpoint_bootstrap_state_ambiguous: %w", resultErr)
	}()

	if _, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.CompletedExecutionWorkerUnit); err != nil {
		failureCode = "feed_worker_not_dormant"
		return CheckpointBootstrapReceipt{}, err
	}
	if _, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.LiteLLMWorkerUnit); err != nil {
		failureCode = "litellm_worker_not_dormant"
		return CheckpointBootstrapReceipt{}, err
	}
	if err := proveCleanBaselineCredentialAbsent(engine.Paths, plan.Preview.DataResetOperationID, true); err != nil {
		failureCode = "connector_credential_config_state_present"
		return CheckpointBootstrapReceipt{}, err
	}

	var active checkpointepoch.ActiveStateSummary
	if err := engine.runPhase(journal, "bootstrap_empty_checkpoint", func() error {
		var bootstrapErr error
		active, bootstrapErr = engine.CleanBaseline.BootstrapEmptyCheckpoint(
			ctx, plan.Credentials, plan.Input, plan.NextEpoch, cleanBaselineNow(engine),
		)
		if bootstrapErr != nil && errors.Is(bootstrapErr, checkpointepoch.ErrConflict) {
			active, bootstrapErr = engine.CleanBaseline.VerifyEmptyCheckpoint(
				ctx, plan.Credentials, plan.Input, plan.NextEpoch,
			)
		}
		return bootstrapErr
	}); err != nil {
		failureCode = "empty_checkpoint_bootstrap_failed"
		return CheckpointBootstrapReceipt{}, err
	}
	verified, err := engine.CleanBaseline.VerifyEmptyCheckpoint(
		ctx, plan.Credentials, plan.Input, plan.NextEpoch,
	)
	if err != nil || verified.StateSHA256 != active.StateSHA256 {
		failureCode = "empty_checkpoint_readback_failed"
		if err != nil {
			return CheckpointBootstrapReceipt{}, err
		}
		return CheckpointBootstrapReceipt{}, fmt.Errorf("checkpoint bootstrap state changed after commit")
	}
	for _, unit := range []string{
		checkpointepoch.CompletedExecutionWorkerUnit,
		checkpointepoch.LiteLLMWorkerUnit,
	} {
		if _, err := engine.requireCheckpointWorkerDormant(ctx, unit); err != nil {
			failureCode = "worker_changed_during_checkpoint_bootstrap"
			return CheckpointBootstrapReceipt{}, err
		}
	}
	if err := proveCleanBaselineCredentialAbsent(engine.Paths, plan.Preview.DataResetOperationID, true); err != nil {
		failureCode = "connector_credential_config_changed_during_checkpoint_bootstrap"
		return CheckpointBootstrapReceipt{}, err
	}

	checks := []CheckResult{
		{Name: "data_reset_receipt", Status: "pass", Detail: plan.DataResetReceiptSHA256},
		{Name: "checkpoint_epoch", Status: "pass", Detail: "sealed_historical_max_plus_one"},
		{Name: "checkpoint_state", Status: "pass", Detail: "one_inactive_idle_zero_counter_state"},
		{Name: "epoch_history", Status: "pass", Detail: "operations_zero_history_zero"},
		{Name: "connector_credential_config", Status: "pass", Detail: "unirouter_state_and_litellm_credential_environment_absent_no_replacement"},
		{Name: "workers", Status: "pass", Detail: "both_inactive_disabled_no_poll"},
	}
	evidence := &CheckpointBootstrapDurableEvidence{
		SchemaVersion:           CheckpointBootstrapReceiptSchema,
		DataResetOperationID:    plan.Preview.DataResetOperationID,
		DataResetReceiptSHA256:  plan.DataResetReceiptSHA256,
		CheckpointFloorSHA256:   plan.Preview.CheckpointFloorSHA256,
		NextEpoch:               plan.NextEpoch,
		NextSourceSHA256:        plan.Preview.NextSourceSHA256,
		NextCursorSHA256:        plan.Preview.NextCursorSHA256,
		NextHighWatermarkSHA256: plan.Preview.NextHighWatermarkSHA256,
		RequestSHA256:           plan.Preview.RequestSHA256,
		Active:                  verified,
	}
	baseReceipt := OperationReceipt{
		SchemaVersion:       ReceiptSchema,
		OperationID:         journal.OperationID,
		Kind:                "checkpoint_bootstrap",
		Status:              "completed",
		Before:              &plan.State.Current,
		After:               &plan.State.Current,
		BackupPath:          plan.DataResetReceipt.BackupPath,
		BackupSHA256:        plan.DataResetReceipt.BackupSHA256,
		DatabaseSetRef:      plan.Credentials.SetRef,
		ReasonCode:          journal.ReasonCode,
		CheckpointBootstrap: evidence,
		Checks:              checks,
		CompletedAt:         cleanBaselineNow(engine),
	}
	if err := engine.finalizeCheckpointBootstrapOperation(journal, &baseReceipt); err != nil {
		failureCode = "checkpoint_bootstrap_receipt_finalize_failed"
		return CheckpointBootstrapReceipt{}, err
	}
	operationFinalized = true
	receiptPath := filepath.Join(engine.Paths.ReceiptsDir, journal.OperationID+".json")
	durableSHA256, _, err := fileSHA256(receiptPath, cleanBaselineReceiptMaxBytes)
	if err != nil {
		return CheckpointBootstrapReceipt{}, err
	}
	return CheckpointBootstrapReceipt{
		SchemaVersion:          CheckpointBootstrapReceiptSchema,
		OperationID:            journal.OperationID,
		Status:                 "completed",
		Release:                cleanBaselineReleaseIdentity(plan.State.Current),
		PreviewSHA256:          plan.Preview.PreviewSHA256,
		DataResetOperationID:   plan.Preview.DataResetOperationID,
		DataResetReceiptSHA256: plan.DataResetReceiptSHA256,
		CheckpointFloorSHA256:  plan.Preview.CheckpointFloorSHA256,
		RequestSHA256:          plan.Preview.RequestSHA256,
		Active:                 verified,
		Workers:                plan.Workers,
		Checks:                 checks,
		DurableReceiptSHA256:   durableSHA256,
		CompletedAt:            baseReceipt.CompletedAt,
	}, nil
}

func (engine *Engine) finalizeCheckpointBootstrapOperation(
	journal *Journal,
	receipt *OperationReceipt,
) error {
	if receipt == nil {
		return fmt.Errorf("checkpoint bootstrap receipt is required")
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
	if err := engine.runPhase(journal, "retire_checkpoint_bootstrap_request", func() error {
		return retireCheckpointBootstrapRequest(
			engine.Paths, journal.OperationID, journal.CheckpointRequestSHA256,
		)
	}); err != nil {
		return err
	}
	return engine.completeJournal(journal)
}

func (engine *Engine) readCheckpointBootstrapDurableReceipt(
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
	if receipt.SchemaVersion != ReceiptSchema || receipt.OperationID != journal.OperationID ||
		receipt.Kind != "checkpoint_bootstrap" || receipt.Status != "completed" ||
		journal.ReceiptAt == nil || !receipt.CompletedAt.Equal(*journal.ReceiptAt) ||
		receipt.DatabaseSetRef != journal.DatabaseSetAfter || receipt.ReasonCode != journal.ReasonCode ||
		receipt.CheckpointBootstrap == nil ||
		receipt.CheckpointBootstrap.SchemaVersion != CheckpointBootstrapReceiptSchema ||
		receipt.CheckpointBootstrap.DataResetOperationID != journal.DataResetOperationID ||
		receipt.CheckpointBootstrap.DataResetReceiptSHA256 != journal.DataResetReceiptSHA256 ||
		receipt.CheckpointBootstrap.CheckpointFloorSHA256 != journal.CleanCheckpointFloorSHA256 ||
		receipt.CheckpointBootstrap.RequestSHA256 != journal.CheckpointRequestSHA256 ||
		receipt.CheckpointBootstrap.NextEpoch != journal.CheckpointEpoch ||
		receipt.Before == nil || journal.Before == nil || *receipt.Before != *journal.Before ||
		receipt.After == nil || journal.Target == nil || *receipt.After != *journal.Target {
		return OperationReceipt{}, true, fmt.Errorf("checkpoint bootstrap durable receipt does not match its journal")
	}
	return receipt, true, nil
}

func (engine *Engine) resumeCheckpointBootstrapOperation(
	ctx context.Context,
	journal *Journal,
) (OperationReceipt, error) {
	input, err := readCheckpointBootstrapRequest(
		engine.Paths, journal.OperationID, journal.CheckpointRequestSHA256,
	)
	if err != nil {
		if receipt, exists, receiptErr := engine.readCheckpointBootstrapDurableReceipt(*journal); exists && receiptErr == nil {
			if verifyErr := engine.verifyCheckpointBootstrapSummary(
				ctx, receipt.CheckpointBootstrap.Active,
			); verifyErr != nil {
				return OperationReceipt{}, verifyErr
			}
			if verifyErr := engine.verifyCheckpointBootstrapDormantBoundary(
				ctx, journal.DataResetOperationID,
			); verifyErr != nil {
				return OperationReceipt{}, verifyErr
			}
			if err := retireCheckpointBootstrapRequest(engine.Paths, journal.OperationID, ""); err != nil {
				return OperationReceipt{}, err
			}
			if err := engine.completeJournal(journal); err != nil {
				return OperationReceipt{}, err
			}
			return receipt, nil
		}
		return OperationReceipt{}, err
	}
	preview, plan, err := engine.prepareCheckpointBootstrap(
		ctx,
		journal.DataResetOperationID,
		journal.DataResetReceiptSHA256,
		input,
		journal.OperationID,
	)
	if err != nil || preview.PreviewSHA256 != journal.CheckpointPreviewSHA256 ||
		preview.NextEpoch != journal.CheckpointEpoch ||
		preview.CheckpointFloorSHA256 != journal.CleanCheckpointFloorSHA256 {
		if err != nil {
			return OperationReceipt{}, err
		}
		return OperationReceipt{}, fmt.Errorf("checkpoint bootstrap resume plan differs from its journal")
	}
	if receipt, exists, err := engine.readCheckpointBootstrapDurableReceipt(*journal); err != nil {
		return OperationReceipt{}, err
	} else if exists {
		verified, verifyErr := engine.CleanBaseline.VerifyEmptyCheckpoint(
			ctx, plan.Credentials, plan.Input, plan.NextEpoch,
		)
		if verifyErr != nil || verified.StateSHA256 != receipt.CheckpointBootstrap.Active.StateSHA256 {
			return OperationReceipt{}, fmt.Errorf("checkpoint bootstrap state differs from durable receipt")
		}
		if verifyErr := engine.verifyCheckpointBootstrapDormantBoundary(
			ctx, journal.DataResetOperationID,
		); verifyErr != nil {
			return OperationReceipt{}, verifyErr
		}
		if err := retireCheckpointBootstrapRequest(
			engine.Paths, journal.OperationID, journal.CheckpointRequestSHA256,
		); err != nil {
			return OperationReceipt{}, err
		}
		if err := engine.completeJournal(journal); err != nil {
			return OperationReceipt{}, err
		}
		return receipt, nil
	}
	_, err = engine.executeCheckpointBootstrap(ctx, journal, plan)
	if err != nil {
		return OperationReceipt{}, err
	}
	receipt, _, err := engine.readCheckpointBootstrapDurableReceipt(*journal)
	return receipt, err
}

func (engine *Engine) verifyCheckpointBootstrapSummary(
	ctx context.Context,
	expected checkpointepoch.ActiveStateSummary,
) error {
	repository, err := engine.openCheckpointEpochRepository(ctx)
	if err != nil {
		return err
	}
	defer repository.Close()
	report, err := repository.StatusByBinding(ctx, expected.Binding, engine.now())
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(report.Active, expected) || report.Operation != nil {
		return fmt.Errorf("checkpoint bootstrap state differs from durable receipt")
	}
	return nil
}

func (engine *Engine) verifyCheckpointBootstrapDormantBoundary(
	ctx context.Context,
	dataResetOperationID string,
) error {
	for _, unit := range []string{
		checkpointepoch.CompletedExecutionWorkerUnit,
		checkpointepoch.LiteLLMWorkerUnit,
	} {
		if _, err := engine.requireCheckpointWorkerDormant(ctx, unit); err != nil {
			return err
		}
	}
	return proveCleanBaselineCredentialAbsent(engine.Paths, dataResetOperationID, true)
}

func (engine *Engine) abortCheckpointBootstrapOperation(
	ctx context.Context,
	journal *Journal,
) (OperationReceipt, error) {
	if _, exists, err := engine.readCheckpointBootstrapDurableReceipt(*journal); err != nil {
		return OperationReceipt{}, err
	} else if exists {
		return OperationReceipt{}, fmt.Errorf("completed checkpoint bootstrap cannot be aborted")
	}
	credentials, err := LoadDatabaseCredentials(engine.Paths)
	if err != nil || credentials.SetRef != journal.DatabaseSetBefore || journal.DatabaseSetAfter != journal.DatabaseSetBefore {
		return OperationReceipt{}, fmt.Errorf("checkpoint bootstrap database set changed")
	}
	state, err := engine.readInstallState()
	if err != nil {
		return OperationReceipt{}, err
	}
	current, err := engine.verifyStateV2(state.Current)
	if err != nil {
		return OperationReceipt{}, err
	}
	if journal.CheckpointRequestSHA256 != "" {
		input, requestErr := readCheckpointBootstrapRequest(
			engine.Paths, journal.OperationID, journal.CheckpointRequestSHA256,
		)
		if requestErr != nil {
			return OperationReceipt{}, requestErr
		}
		if _, activeErr := engine.CleanBaseline.VerifyEmptyCheckpoint(
			ctx, credentials, input, journal.CheckpointEpoch,
		); activeErr == nil {
			return OperationReceipt{}, fmt.Errorf("checkpoint bootstrap crossed the monotonic epoch barrier and cannot be aborted")
		}
	}
	if _, err := engine.CleanBaseline.VerifyCleanBaselineDatabase(
		ctx, credentials, current.Bundle.Manifest.MigrationChecksums,
	); err != nil {
		return OperationReceipt{}, fmt.Errorf("checkpoint bootstrap strict-empty rollback boundary is unavailable")
	}
	for _, unit := range []string{
		checkpointepoch.CompletedExecutionWorkerUnit,
		checkpointepoch.LiteLLMWorkerUnit,
	} {
		if _, err := engine.requireCheckpointWorkerDormant(ctx, unit); err != nil {
			return OperationReceipt{}, err
		}
	}
	if err := proveCleanBaselineCredentialAbsent(engine.Paths, journal.DataResetOperationID, true); err != nil {
		return OperationReceipt{}, err
	}
	if err := retireCheckpointBootstrapRequest(
		engine.Paths, journal.OperationID, journal.CheckpointRequestSHA256,
	); err != nil {
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
		SchemaVersion:  ReceiptSchema,
		OperationID:    journal.OperationID,
		Kind:           "checkpoint_bootstrap",
		Status:         "aborted",
		Before:         journal.Before,
		After:          journal.Before,
		DatabaseSetRef: journal.DatabaseSetBefore,
		ReasonCode:     journal.ReasonCode,
		Checks: []CheckResult{
			{Name: "strict_empty_checkpoint", Status: "pass", Detail: "state_zero_operations_zero_history_zero"},
			{Name: "workers", Status: "pass", Detail: "both_inactive_disabled"},
		},
		CompletedAt: now,
	}
	if err := engine.writeReceipt(receipt); err != nil {
		return OperationReceipt{}, err
	}
	return receipt, nil
}
