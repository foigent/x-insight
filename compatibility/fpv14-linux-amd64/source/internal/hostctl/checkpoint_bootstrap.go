package hostctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/checkpointepoch"
)

const (
	CheckpointBootstrapPreviewSchema = "xinsight.checkpoint-bootstrap-preview.v1"
	CheckpointBootstrapReceiptSchema = "xinsight.checkpoint-bootstrap-receipt.v1"
	checkpointBootstrapReasonCode    = "second_integration_checkpoint_bootstrap"
)

var hostOperationIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}$`)

type CheckpointBootstrapPreviewReport struct {
	SchemaVersion           string                       `json:"schema_version"`
	Supported               bool                         `json:"supported"`
	Mutating                bool                         `json:"mutating"`
	Release                 CleanBaselineReleaseIdentity `json:"release"`
	DataResetOperationID    string                       `json:"data_reset_operation_id"`
	DataResetReceiptSHA256  string                       `json:"data_reset_receipt_sha256"`
	CheckpointFloorSHA256   string                       `json:"checkpoint_floor_sha256"`
	Binding                 checkpointepoch.Binding      `json:"binding"`
	WorkerUnit              string                       `json:"worker_unit"`
	NextEpoch               int64                        `json:"next_epoch"`
	NextSourceSHA256        string                       `json:"next_source_instance_sha256"`
	NextCursorSHA256        string                       `json:"next_cursor_sha256"`
	NextHighWatermarkSHA256 string                       `json:"next_high_watermark_sha256"`
	RequestSHA256           string                       `json:"checkpoint_request_sha256"`
	Workers                 []CleanBaselineWorkerPreview `json:"workers"`
	Checks                  []CheckResult                `json:"checks"`
	PreviewSHA256           string                       `json:"preview_sha256"`
	GeneratedAt             time.Time                    `json:"generated_at"`
}

type CheckpointBootstrapDurableEvidence struct {
	SchemaVersion           string                             `json:"schema_version"`
	DataResetOperationID    string                             `json:"data_reset_operation_id"`
	DataResetReceiptSHA256  string                             `json:"data_reset_receipt_sha256"`
	CheckpointFloorSHA256   string                             `json:"checkpoint_floor_sha256"`
	NextEpoch               int64                              `json:"next_epoch"`
	NextSourceSHA256        string                             `json:"next_source_instance_sha256"`
	NextCursorSHA256        string                             `json:"next_cursor_sha256"`
	NextHighWatermarkSHA256 string                             `json:"next_high_watermark_sha256"`
	RequestSHA256           string                             `json:"checkpoint_request_sha256"`
	Active                  checkpointepoch.ActiveStateSummary `json:"active"`
}

type CheckpointBootstrapReceipt struct {
	SchemaVersion          string                             `json:"schema_version"`
	OperationID            string                             `json:"operation_id"`
	Status                 string                             `json:"status"`
	Release                CleanBaselineReleaseIdentity       `json:"release"`
	PreviewSHA256          string                             `json:"preview_sha256"`
	DataResetOperationID   string                             `json:"data_reset_operation_id"`
	DataResetReceiptSHA256 string                             `json:"data_reset_receipt_sha256"`
	CheckpointFloorSHA256  string                             `json:"checkpoint_floor_sha256"`
	RequestSHA256          string                             `json:"checkpoint_request_sha256"`
	Active                 checkpointepoch.ActiveStateSummary `json:"active"`
	Workers                []CleanBaselineWorkerPreview       `json:"workers"`
	Checks                 []CheckResult                      `json:"checks"`
	DurableReceiptSHA256   string                             `json:"durable_receipt_sha256"`
	CompletedAt            time.Time                          `json:"completed_at"`
}

type checkpointBootstrapPlan struct {
	State                  InstallState
	Current                verifiedTarget
	Credentials            DatabaseCredentials
	DataResetReceipt       OperationReceipt
	DataResetReceiptSHA256 string
	Input                  checkpointepoch.PrepareInput
	NextEpoch              int64
	Workers                []CleanBaselineWorkerPreview
	Preview                CheckpointBootstrapPreviewReport
}

func (engine *Engine) PreviewCheckpointBootstrap(
	ctx context.Context,
	dataResetOperationID string,
	dataResetReceiptSHA256 string,
	input checkpointepoch.PrepareInput,
) (CheckpointBootstrapPreviewReport, error) {
	report, _, err := engine.prepareCheckpointBootstrap(
		ctx, dataResetOperationID, dataResetReceiptSHA256, input, "",
	)
	return report, err
}

func (engine *Engine) prepareCheckpointBootstrap(
	ctx context.Context,
	dataResetOperationID string,
	dataResetReceiptSHA256 string,
	input checkpointepoch.PrepareInput,
	allowedActiveOperationID string,
) (CheckpointBootstrapPreviewReport, checkpointBootstrapPlan, error) {
	empty := CheckpointBootstrapPreviewReport{
		SchemaVersion: CheckpointBootstrapPreviewSchema,
		Supported:     false,
		Mutating:      false,
		GeneratedAt:   engine.now().UTC().Truncate(time.Second),
	}
	if allowedActiveOperationID == "" {
		if err := engine.ensureNoOutstandingOperation(); err != nil {
			return empty, checkpointBootstrapPlan{}, err
		}
	} else {
		journal, err := engine.readActiveJournal()
		if err != nil || journal.OperationID != allowedActiveOperationID || journal.Kind != "checkpoint_bootstrap" {
			return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap active operation identity changed")
		}
	}
	if err := validateCleanBaselineBinding(input); err != nil {
		return empty, checkpointBootstrapPlan{}, err
	}
	if !hostOperationIDPattern.MatchString(dataResetOperationID) ||
		!sha256ReferencePattern.MatchString(dataResetReceiptSHA256) {
		return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap requires an exact DATA_RESET operation and receipt SHA-256")
	}
	if _, err := detectSupportedHost(engine.Paths); err != nil {
		return empty, checkpointBootstrapPlan{}, err
	}
	state, err := engine.readInstallState()
	if err != nil || state.Current.LegacyReleaseID != "" {
		return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap requires a managed SemVer release")
	}
	current, err := engine.verifyStateV2(state.Current)
	if err != nil {
		return empty, checkpointBootstrapPlan{}, err
	}
	if err := releasePayloadAndRuntimePreflight(current, engine.Paths); err != nil {
		return empty, checkpointBootstrapPlan{}, err
	}
	dataResetReceipt, exactSHA256, err := engine.readDataResetReceipt(
		dataResetOperationID, dataResetReceiptSHA256,
	)
	if err != nil {
		return empty, checkpointBootstrapPlan{}, err
	}
	if dataResetReceipt.After == nil || *dataResetReceipt.After != state.Current ||
		dataResetReceipt.DataReset.Release != cleanBaselineReleaseIdentity(state.Current) {
		return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap release differs from its DATA_RESET receipt")
	}
	activeSet, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
	if err != nil || activeSet != dataResetReceipt.DatabaseSetRef ||
		cleanBaselineDatabaseSetDigest(activeSet) != dataResetReceipt.DataReset.ActiveDatabaseSetSHA256 {
		return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap database set differs from its DATA_RESET receipt")
	}
	credentials, err := LoadDatabaseCredentials(engine.Paths)
	if err != nil || credentials.SetRef != activeSet {
		return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap database credentials differ from its DATA_RESET receipt")
	}
	floor := dataResetReceipt.DataReset.CheckpointFloor
	if err := verifyCleanBaselineCheckpointFloor(floor); err != nil {
		return empty, checkpointBootstrapPlan{}, err
	}
	if floor.RollbackBackupSHA256 != dataResetReceipt.BackupSHA256 ||
		floor.SealedSHA256 == "" || floor.HistoricalMaxEpoch == math.MaxInt64 {
		return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap DATA_RESET floor is invalid")
	}
	databaseProof, err := engine.CleanBaseline.VerifyCleanBaselineDatabase(
		ctx, credentials, current.Bundle.Manifest.MigrationChecksums,
	)
	if err != nil || databaseProof != dataResetReceipt.DataReset.Database {
		if allowedActiveOperationID == "" {
			return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap requires the exact strict-empty DATA_RESET database")
		}
		if _, activeErr := engine.CleanBaseline.VerifyEmptyCheckpoint(
			ctx, credentials, input, floor.HistoricalMaxEpoch+1,
		); activeErr != nil {
			return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap database is neither strict-empty nor the exact committed bootstrap state")
		}
	}
	if err := verifyCleanBaselineAccountEvidence(engine.Paths.AccessAccountsFile, dataResetReceipt.DataReset.Account); err != nil {
		return empty, checkpointBootstrapPlan{}, err
	}
	if err := proveCleanBaselineCredentialAbsent(engine.Paths, dataResetOperationID, true); err != nil {
		return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap requires zero connector credential/config state: %w", err)
	}
	if containsString(floor.PriorSourceInstanceSHA256, hashCleanBaselineText(input.NextSourceInstance)) {
		return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap source_instance was already used before DATA_RESET")
	}
	if containsString(floor.PriorCursorSHA256, hashCleanBaselineText(input.NextCursor)) ||
		containsString(floor.PriorHighWatermarkSHA256, hashCleanBaselineText(input.NextHighWatermark)) {
		return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap cursor identity was already used before DATA_RESET")
	}
	feedWorker, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.CompletedExecutionWorkerUnit)
	if err != nil {
		return empty, checkpointBootstrapPlan{}, err
	}
	liteLLMWorker, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.LiteLLMWorkerUnit)
	if err != nil {
		return empty, checkpointBootstrapPlan{}, err
	}
	serviceState, err := engine.systemdUnitProperty(ctx, "xinsight.service", "ActiveState")
	if err != nil || serviceState != "active" {
		return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap requires the main service to remain active")
	}
	mainPID, err := engine.systemdUnitProperty(ctx, "xinsight.service", "MainPID")
	pid, parseErr := strconv.ParseInt(mainPID, 10, 64)
	if err != nil || parseErr != nil || pid <= 0 {
		return empty, checkpointBootstrapPlan{}, fmt.Errorf("checkpoint bootstrap main service process identity is invalid")
	}
	workers := []CleanBaselineWorkerPreview{
		{Unit: feedWorker.Unit, Present: feedWorker.Present, Active: feedWorker.Active, Enabled: feedWorker.Enabled},
		{Unit: liteLLMWorker.Unit, Present: liteLLMWorker.Present, Active: liteLLMWorker.Active, Enabled: liteLLMWorker.Enabled},
	}
	report := CheckpointBootstrapPreviewReport{
		SchemaVersion:           CheckpointBootstrapPreviewSchema,
		Supported:               true,
		Mutating:                false,
		Release:                 cleanBaselineReleaseIdentity(state.Current),
		DataResetOperationID:    dataResetOperationID,
		DataResetReceiptSHA256:  exactSHA256,
		CheckpointFloorSHA256:   floor.SealedSHA256,
		Binding:                 input.Binding,
		WorkerUnit:              input.WorkerUnit,
		NextEpoch:               floor.HistoricalMaxEpoch + 1,
		NextSourceSHA256:        hashCleanBaselineText(input.NextSourceInstance),
		NextCursorSHA256:        hashCleanBaselineText(input.NextCursor),
		NextHighWatermarkSHA256: hashCleanBaselineText(input.NextHighWatermark),
		Workers:                 workers,
		Checks: []CheckResult{
			{Name: "data_reset_receipt", Status: "pass", Detail: exactSHA256},
			{Name: "strict_empty_checkpoint", Status: "pass", Detail: "state_zero_operations_zero_history_zero"},
			{Name: "checkpoint_epoch", Status: "pass", Detail: "sealed_historical_max_plus_one"},
			{Name: "old_identity", Status: "pass", Detail: "source_cursor_not_reused"},
			{Name: "connector_credential_config", Status: "pass", Detail: "unirouter_state_and_litellm_credential_environment_absent"},
			{Name: "workers", Status: "pass", Detail: "both_inactive_disabled_no_poll"},
		},
		GeneratedAt: empty.GeneratedAt,
	}
	report.RequestSHA256, err = checkpointBootstrapRequestSHA256(input)
	if err != nil {
		return empty, checkpointBootstrapPlan{}, err
	}
	report.PreviewSHA256, err = checkpointBootstrapPreviewDigest(report)
	if err != nil {
		return empty, checkpointBootstrapPlan{}, err
	}
	return report, checkpointBootstrapPlan{
		State:                  state,
		Current:                current,
		Credentials:            credentials,
		DataResetReceipt:       dataResetReceipt,
		DataResetReceiptSHA256: exactSHA256,
		Input:                  input,
		NextEpoch:              report.NextEpoch,
		Workers:                workers,
		Preview:                report,
	}, nil
}

func (engine *Engine) ApplyCheckpointBootstrap(
	ctx context.Context,
	dataResetOperationID string,
	dataResetReceiptSHA256 string,
	input checkpointepoch.PrepareInput,
	expectedPreviewSHA256 string,
	reasonCode string,
) (CheckpointBootstrapReceipt, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return CheckpointBootstrapReceipt{}, err
	}
	if !sha256ReferencePattern.MatchString(expectedPreviewSHA256) {
		return CheckpointBootstrapReceipt{}, fmt.Errorf("checkpoint bootstrap requires the exact preview SHA-256")
	}
	if reasonCode != checkpointBootstrapReasonCode {
		return CheckpointBootstrapReceipt{}, fmt.Errorf("checkpoint bootstrap requires --reason-code %s", checkpointBootstrapReasonCode)
	}
	var receipt CheckpointBootstrapReceipt
	err := engine.withOperationLock(func() error {
		preview, plan, err := engine.prepareCheckpointBootstrap(
			ctx, dataResetOperationID, dataResetReceiptSHA256, input, "",
		)
		if err != nil {
			return err
		}
		if preview.PreviewSHA256 != expectedPreviewSHA256 {
			return fmt.Errorf("checkpoint bootstrap preview digest changed; rerun preview and obtain a new authorization")
		}
		if err := engine.ensureControlDirectories(); err != nil {
			return err
		}
		before := plan.State.Current
		journal, err := engine.startJournal("checkpoint_bootstrap", before.ArtifactPath, "", &before, &before)
		if err != nil {
			return err
		}
		journal.DatabaseSetBefore = plan.Credentials.SetRef
		journal.DatabaseSetAfter = plan.Credentials.SetRef
		journal.DataResetOperationID = dataResetOperationID
		journal.DataResetReceiptSHA256 = plan.DataResetReceiptSHA256
		journal.CleanCheckpointFloorSHA256 = preview.CheckpointFloorSHA256
		journal.CheckpointPreviewSHA256 = preview.PreviewSHA256
		journal.CheckpointEpoch = preview.NextEpoch
		journal.ReasonCode = reasonCode
		if err := engine.updateJournal(journal); err != nil {
			return err
		}
		requestSHA256, err := stageCheckpointBootstrapRequest(engine.Paths, journal.OperationID, input)
		if err != nil {
			if failErr := engine.failJournal(
				journal,
				"checkpoint_bootstrap_request_staging_failed",
				"recover",
				"abort",
			); failErr != nil {
				return fmt.Errorf("stage checkpoint bootstrap request: %v; operation journal update failed: %w", err, failErr)
			}
			return fmt.Errorf("stage checkpoint bootstrap request: %w", err)
		}
		if requestSHA256 != preview.RequestSHA256 {
			if failErr := engine.failJournal(
				journal,
				"checkpoint_bootstrap_request_digest_changed",
				"recover",
				"abort",
			); failErr != nil {
				return fmt.Errorf("checkpoint bootstrap request changed after preview; operation journal update failed: %w", failErr)
			}
			return fmt.Errorf("checkpoint bootstrap request changed after preview")
		}
		journal.CheckpointRequestSHA256 = requestSHA256
		if err := engine.updateJournal(journal); err != nil {
			if failErr := engine.failJournal(
				journal,
				"checkpoint_bootstrap_request_journal_failed",
				"recover",
				"abort",
			); failErr != nil {
				return fmt.Errorf("record checkpoint bootstrap request: %v; operation journal update failed: %w", err, failErr)
			}
			return fmt.Errorf("record checkpoint bootstrap request: %w", err)
		}
		receipt, err = engine.executeCheckpointBootstrap(ctx, journal, plan)
		return err
	})
	return receipt, err
}

func checkpointBootstrapPreviewDigest(report CheckpointBootstrapPreviewReport) (string, error) {
	report.PreviewSHA256 = ""
	report.GeneratedAt = time.Time{}
	payload, err := marshalJSON(report)
	if err != nil {
		return "", err
	}
	return hashCleanBaselineBytes(payload), nil
}

func (engine *Engine) readDataResetReceipt(
	operationID string,
	expectedSHA256 string,
) (OperationReceipt, string, error) {
	if !hostOperationIDPattern.MatchString(operationID) || !sha256ReferencePattern.MatchString(expectedSHA256) {
		return OperationReceipt{}, "", fmt.Errorf("DATA_RESET receipt identity is invalid")
	}
	path := filepath.Join(engine.Paths.ReceiptsDir, operationID+".json")
	digest, _, err := fileSHA256(path, cleanBaselineReceiptMaxBytes)
	if err != nil || digest != expectedSHA256 {
		return OperationReceipt{}, "", fmt.Errorf("DATA_RESET durable receipt SHA-256 does not match")
	}
	var receipt OperationReceipt
	if err := readJSONStrict(path, &receipt); err != nil {
		return OperationReceipt{}, "", err
	}
	if receipt.SchemaVersion != ReceiptSchema || receipt.OperationID != operationID ||
		receipt.Kind != "clean_baseline_data_reset" || receipt.Status != "completed" ||
		receipt.ReasonCode != cleanBaselineReasonCode || receipt.DataReset == nil ||
		receipt.DataReset.SchemaVersion != CleanBaselineReceiptSchema ||
		receipt.DataReset.RollbackBackupSHA256 != receipt.BackupSHA256 {
		return OperationReceipt{}, "", fmt.Errorf("DATA_RESET durable receipt is invalid")
	}
	if err := validateCleanBaselineDurableEvidence(receipt.DataReset, receipt.BackupSHA256); err != nil {
		return OperationReceipt{}, "", err
	}
	return receipt, digest, nil
}

func verifyCleanBaselineAccountEvidence(path string, expected CleanBaselineAccountPreview) error {
	config, err := access.ReadAndValidateAccountFile(path)
	if err != nil || len(config.Accounts) != 1 {
		return fmt.Errorf("checkpoint bootstrap requires exactly the protected demo.operator account")
	}
	entry := config.Accounts[0]
	fileDigest, _, err := fileSHA256(path, access.MaxAccountFileBytes)
	if err != nil || entry.Username != expected.Username ||
		hashCleanBaselineText(entry.SubjectID) != expected.SubjectSHA256 ||
		entry.Role != expected.Role || entry.Disabled != expected.Disabled ||
		hashCleanBaselineText(entry.PasswordHash) != expected.PasswordVerifierSHA256 ||
		fileDigest != expected.SuccessorConfigSHA256 {
		return fmt.Errorf("protected demo.operator identity, role, or verifier differs from DATA_RESET receipt")
	}
	return nil
}

func stageCheckpointBootstrapRequest(
	paths Paths,
	operationID string,
	input checkpointepoch.PrepareInput,
) (string, error) {
	path := checkpointBootstrapRequestPath(paths, operationID)
	if err := writeJSONExclusive(path, checkpointBootstrapRequest(input), 0o600); err != nil {
		return "", err
	}
	digest, _, err := fileSHA256(path, cleanBaselineReceiptMaxBytes)
	return digest, err
}

func checkpointBootstrapRequestSHA256(input checkpointepoch.PrepareInput) (string, error) {
	payload, err := checkpointBootstrapRequestPayload(input)
	if err != nil {
		return "", err
	}
	return hashCleanBaselineBytes(payload), nil
}

func checkpointBootstrapRequestPayload(input checkpointepoch.PrepareInput) ([]byte, error) {
	return marshalJSON(checkpointBootstrapRequest(input))
}

func checkpointBootstrapRequest(input checkpointepoch.PrepareInput) checkpointepoch.PrepareRequest {
	return checkpointepoch.PrepareRequest{
		SchemaVersion:      checkpointepoch.PrepareRequestSchema,
		Binding:            input.Binding,
		WorkerUnit:         input.WorkerUnit,
		NextSourceInstance: input.NextSourceInstance,
		NextCursor:         input.NextCursor,
		NextHighWatermark:  input.NextHighWatermark,
		AuthorizationRef:   input.AuthorizationRef,
	}
}

func checkpointBootstrapRequestPath(paths Paths, operationID string) string {
	return filepath.Join(paths.OperationsDir, operationID+"-checkpoint-bootstrap-request.json")
}

func readCheckpointBootstrapRequest(
	paths Paths,
	operationID string,
	expectedSHA256 string,
) (checkpointepoch.PrepareInput, error) {
	path := checkpointBootstrapRequestPath(paths, operationID)
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 ||
		before.Size() <= 0 || before.Size() > cleanBaselineReceiptMaxBytes {
		return checkpointepoch.PrepareInput{}, fmt.Errorf("checkpoint bootstrap staged request metadata is invalid")
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() || uint64(stat.Nlink) != 1 {
		return checkpointepoch.PrepareInput{}, fmt.Errorf("checkpoint bootstrap staged request ownership is invalid")
	}
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return checkpointepoch.PrepareInput{}, fmt.Errorf("checkpoint bootstrap staged request cannot be opened safely")
	}
	file := os.NewFile(uintptr(descriptor), filepath.Base(path))
	if file == nil {
		_ = syscall.Close(descriptor)
		return checkpointepoch.PrepareInput{}, fmt.Errorf("checkpoint bootstrap staged request handle is invalid")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return checkpointepoch.PrepareInput{}, fmt.Errorf("checkpoint bootstrap staged request changed while opening")
	}
	payload, err := io.ReadAll(io.LimitReader(file, cleanBaselineReceiptMaxBytes+1))
	if err != nil || len(payload) == 0 || int64(len(payload)) > cleanBaselineReceiptMaxBytes {
		return checkpointepoch.PrepareInput{}, fmt.Errorf("checkpoint bootstrap staged request cannot be read")
	}
	if hashCleanBaselineBytes(payload) != expectedSHA256 {
		for index := range payload {
			payload[index] = 0
		}
		return checkpointepoch.PrepareInput{}, fmt.Errorf("checkpoint bootstrap staged request digest differs from journal")
	}
	input, err := checkpointepoch.DecodePrepareRequest(payload)
	for index := range payload {
		payload[index] = 0
	}
	return input, err
}

func retireCheckpointBootstrapRequest(paths Paths, operationID string, expectedSHA256 string) error {
	path := checkpointBootstrapRequestPath(paths, operationID)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if expectedSHA256 != "" {
		digest, _, err := fileSHA256(path, cleanBaselineReceiptMaxBytes)
		if err != nil || digest != expectedSHA256 {
			return fmt.Errorf("checkpoint bootstrap staged request digest differs from journal")
		}
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
