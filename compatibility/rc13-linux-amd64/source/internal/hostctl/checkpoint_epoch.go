package hostctl

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/foigent/x-insight/internal/checkpointepoch"
)

const CheckpointEpochHostReportSchema = "xinsight.connector-checkpoint-epoch-host-report.v1"

type CheckpointEpochRepository interface {
	Prepare(context.Context, checkpointepoch.PrepareInput, string, time.Time) (checkpointepoch.Report, error)
	Activate(context.Context, string, string, time.Time) (checkpointepoch.Report, error)
	Abort(context.Context, string, string, time.Time) (checkpointepoch.Report, error)
	StatusByBinding(context.Context, checkpointepoch.Binding, time.Time) (checkpointepoch.Report, error)
	StatusByOperation(context.Context, string, time.Time) (checkpointepoch.Report, error)
	OperationWorkerUnit(context.Context, string) (string, error)
	Close() error
}

type CheckpointEpochWorkerStatus struct {
	Unit    string `json:"unit"`
	Present bool   `json:"present"`
	Active  bool   `json:"active"`
	Enabled bool   `json:"enabled"`
}

type CheckpointEpochHostReport struct {
	SchemaVersion string                      `json:"schema_version"`
	Worker        CheckpointEpochWorkerStatus `json:"worker"`
	Epoch         checkpointepoch.Report      `json:"epoch"`
	GeneratedAt   time.Time                   `json:"generated_at"`
}

func (engine *Engine) PrepareCheckpointEpoch(
	ctx context.Context,
	input checkpointepoch.PrepareInput,
) (CheckpointEpochHostReport, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return CheckpointEpochHostReport{}, err
	}
	var report CheckpointEpochHostReport
	err := engine.withOperationLock(func() error {
		worker, err := engine.requireCheckpointWorkerDormant(ctx, input.WorkerUnit)
		if err != nil {
			return err
		}
		repository, err := engine.openCheckpointEpochRepository(ctx)
		if err != nil {
			return err
		}
		defer repository.Close()
		operationBytes := make([]byte, 16)
		if _, err := io.ReadFull(engine.feedCredentialRandomReader(), operationBytes); err != nil {
			return fmt.Errorf("generate checkpoint epoch operation identity: %w", err)
		}
		epoch, err := repository.Prepare(ctx, input, hex.EncodeToString(operationBytes), engine.now())
		if err != nil {
			return err
		}
		report = checkpointEpochHostReport(worker, epoch, engine.now())
		return nil
	})
	return report, err
}

func (engine *Engine) ActivateCheckpointEpoch(
	ctx context.Context,
	operationID string,
	planSHA256 string,
) (CheckpointEpochHostReport, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return CheckpointEpochHostReport{}, err
	}
	var report CheckpointEpochHostReport
	err := engine.withOperationLock(func() error {
		repository, err := engine.openCheckpointEpochRepository(ctx)
		if err != nil {
			return err
		}
		defer repository.Close()
		workerUnit, err := repository.OperationWorkerUnit(ctx, operationID)
		if err != nil {
			return err
		}
		current, err := repository.StatusByOperation(ctx, operationID, engine.now())
		if err != nil {
			return err
		}
		if current.Operation == nil {
			return fmt.Errorf("checkpoint epoch operation status is incomplete")
		}
		var worker CheckpointEpochWorkerStatus
		if current.Operation.Status == checkpointepoch.StatusActivated {
			worker, err = engine.inspectCheckpointWorkerState(ctx, workerUnit)
		} else {
			worker, err = engine.requireCheckpointWorkerDormant(ctx, workerUnit)
		}
		if err != nil {
			return err
		}
		epoch, err := repository.Activate(ctx, operationID, planSHA256, engine.now())
		if err != nil {
			return err
		}
		report = checkpointEpochHostReport(worker, epoch, engine.now())
		return nil
	})
	return report, err
}

func (engine *Engine) AbortCheckpointEpoch(
	ctx context.Context,
	operationID string,
	planSHA256 string,
) (CheckpointEpochHostReport, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return CheckpointEpochHostReport{}, err
	}
	var report CheckpointEpochHostReport
	err := engine.withOperationLock(func() error {
		repository, err := engine.openCheckpointEpochRepository(ctx)
		if err != nil {
			return err
		}
		defer repository.Close()
		workerUnit, err := repository.OperationWorkerUnit(ctx, operationID)
		if err != nil {
			return err
		}
		worker, err := engine.inspectCheckpointWorkerState(ctx, workerUnit)
		if err != nil {
			return err
		}
		epoch, err := repository.Abort(ctx, operationID, planSHA256, engine.now())
		if err != nil {
			return err
		}
		report = checkpointEpochHostReport(worker, epoch, engine.now())
		return nil
	})
	return report, err
}

func (engine *Engine) CheckpointEpochStatus(
	ctx context.Context,
	binding *checkpointepoch.Binding,
	operationID string,
	workerUnit string,
) (CheckpointEpochHostReport, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return CheckpointEpochHostReport{}, err
	}
	var report CheckpointEpochHostReport
	err := engine.withOperationLock(func() error {
		repository, err := engine.openCheckpointEpochRepository(ctx)
		if err != nil {
			return err
		}
		defer repository.Close()
		var epoch checkpointepoch.Report
		if operationID != "" {
			if binding != nil || workerUnit != "" {
				return fmt.Errorf("checkpoint epoch status accepts either operation_id or binding fields")
			}
			workerUnit, err = repository.OperationWorkerUnit(ctx, operationID)
			if err != nil {
				return err
			}
			epoch, err = repository.StatusByOperation(ctx, operationID, engine.now())
		} else {
			if binding == nil || workerUnit == "" {
				return fmt.Errorf("checkpoint epoch status requires operation_id or complete binding and worker_unit")
			}
			epoch, err = repository.StatusByBinding(ctx, *binding, engine.now())
		}
		if err != nil {
			return err
		}
		worker, err := engine.inspectCheckpointWorkerState(ctx, workerUnit)
		if err != nil {
			return err
		}
		report = checkpointEpochHostReport(worker, epoch, engine.now())
		return nil
	})
	return report, err
}

func (engine *Engine) openCheckpointEpochRepository(ctx context.Context) (CheckpointEpochRepository, error) {
	credentials, err := LoadDatabaseCredentials(engine.Paths)
	if err != nil {
		return nil, err
	}
	if engine.CheckpointEpochOpen != nil {
		return engine.CheckpointEpochOpen(ctx, credentials.AdminDSN)
	}
	return checkpointepoch.Open(ctx, credentials.AdminDSN)
}

func (engine *Engine) inspectCheckpointWorkerState(
	ctx context.Context,
	workerUnit string,
) (CheckpointEpochWorkerStatus, error) {
	var unitPath string
	switch workerUnit {
	case checkpointepoch.CompletedExecutionWorkerUnit:
		unitPath = engine.Paths.FeedServiceUnit
	case checkpointepoch.LiteLLMWorkerUnit:
		unitPath = engine.Paths.LiteLLMServiceUnit
	default:
		return CheckpointEpochWorkerStatus{}, fmt.Errorf("unsupported checkpoint worker unit %q", workerUnit)
	}
	state, err := engine.inspectManagedWorkerState(ctx, workerUnit, unitPath, "checkpoint epoch worker")
	if err != nil {
		return CheckpointEpochWorkerStatus{}, err
	}
	return CheckpointEpochWorkerStatus{
		Unit: workerUnit, Present: state.present, Active: state.active, Enabled: state.enabled,
	}, nil
}

func (engine *Engine) requireCheckpointWorkerDormant(
	ctx context.Context,
	workerUnit string,
) (CheckpointEpochWorkerStatus, error) {
	status, err := engine.inspectCheckpointWorkerState(ctx, workerUnit)
	if err != nil {
		return CheckpointEpochWorkerStatus{}, err
	}
	if !status.Present || status.Active || status.Enabled {
		return CheckpointEpochWorkerStatus{}, fmt.Errorf(
			"checkpoint epoch transition requires a present, inactive, disabled worker unit",
		)
	}
	return status, nil
}

func checkpointEpochHostReport(
	worker CheckpointEpochWorkerStatus,
	epoch checkpointepoch.Report,
	now time.Time,
) CheckpointEpochHostReport {
	return CheckpointEpochHostReport{
		SchemaVersion: CheckpointEpochHostReportSchema,
		Worker:        worker,
		Epoch:         epoch,
		GeneratedAt:   now.UTC().Truncate(time.Second),
	}
}
