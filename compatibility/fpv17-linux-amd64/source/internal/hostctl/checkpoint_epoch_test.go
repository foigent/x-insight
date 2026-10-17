package hostctl

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/checkpointepoch"
)

func TestCheckpointEpochPrepareRequiresDormantManagedWorker(t *testing.T) {
	fixture := newInstallFixture(t)
	runner := fixture.engine.Runner.(*fakeRunner)
	runner.feedWorkerLoaded = true
	repository := &checkpointEpochRepositoryStub{}
	fixture.engine.CheckpointEpochOpen = func(context.Context, string) (CheckpointEpochRepository, error) {
		return repository, nil
	}
	fixture.engine.Random = bytes.NewReader(bytes.Repeat([]byte{0x4a}, 16))
	input := checkpointepoch.PrepareInput{
		Binding: checkpointepoch.Binding{
			SourceSystemID:    "11111111-1111-4111-8111-111111111111",
			AdapterManifestID: "22222222-2222-4222-8222-222222222222",
			CursorKey:         "unirouter.completed-execution.v1",
		},
		WorkerUnit:         checkpointepoch.CompletedExecutionWorkerUnit,
		NextSourceInstance: "unirouter-campaign-2",
		NextCursor:         "cursor-2",
		NextHighWatermark:  "cursor-2",
		AuthorizationRef:   "decision://campaign-2",
	}
	report, err := fixture.engine.PrepareCheckpointEpoch(context.Background(), input)
	if err != nil {
		t.Fatalf("prepare checkpoint epoch: %v", err)
	}
	if repository.preparedOperationID != strings.Repeat("4a", 16) ||
		report.Worker.Active || report.Worker.Enabled || !report.Worker.Present ||
		report.Epoch.Operation == nil || report.Epoch.Operation.Status != checkpointepoch.StatusPrepared {
		t.Fatalf("operation=%q report=%+v", repository.preparedOperationID, report)
	}

	runner.feedWorkerEnabled = true
	repository.preparedOperationID = ""
	if _, err := fixture.engine.PrepareCheckpointEpoch(context.Background(), input); err == nil ||
		!strings.Contains(err.Error(), "inactive, disabled") {
		t.Fatalf("enabled worker error=%v", err)
	}
	if repository.preparedOperationID != "" {
		t.Fatal("repository mutated after worker gate failed")
	}
}

func TestCheckpointEpochActivateRechecksWorkerAndBindsPlan(t *testing.T) {
	fixture := newInstallFixture(t)
	runner := fixture.engine.Runner.(*fakeRunner)
	runner.feedWorkerLoaded = true
	repository := &checkpointEpochRepositoryStub{workerUnit: checkpointepoch.CompletedExecutionWorkerUnit}
	fixture.engine.CheckpointEpochOpen = func(context.Context, string) (CheckpointEpochRepository, error) {
		return repository, nil
	}
	operationID := strings.Repeat("a", 32)
	planSHA256 := "sha256:" + strings.Repeat("b", 64)
	if _, err := fixture.engine.ActivateCheckpointEpoch(context.Background(), operationID, planSHA256); err != nil {
		t.Fatalf("activate checkpoint epoch: %v", err)
	}
	if repository.activatedOperationID != operationID || repository.activatedPlanSHA256 != planSHA256 {
		t.Fatalf("activation binding=%q %q", repository.activatedOperationID, repository.activatedPlanSHA256)
	}
	runner.feedWorkerActive = true
	repository.activatedOperationID = ""
	repository.operationStatus = checkpointepoch.StatusPrepared
	if _, err := fixture.engine.ActivateCheckpointEpoch(context.Background(), operationID, planSHA256); err == nil {
		t.Fatal("prepared activation accepted an active worker")
	}
	if repository.activatedOperationID != "" {
		t.Fatal("prepared activation reached repository while worker was active")
	}
	repository.operationStatus = checkpointepoch.StatusActivated
	if _, err := fixture.engine.ActivateCheckpointEpoch(context.Background(), operationID, planSHA256); err != nil {
		t.Fatalf("idempotent activation with active worker: %v", err)
	}
	if repository.activatedOperationID != operationID {
		t.Fatal("idempotent activation did not revalidate repository state")
	}
}

type checkpointEpochRepositoryStub struct {
	preparedOperationID  string
	activatedOperationID string
	activatedPlanSHA256  string
	workerUnit           string
	operationStatus      string
}

func (stub *checkpointEpochRepositoryStub) Prepare(
	_ context.Context,
	input checkpointepoch.PrepareInput,
	operationID string,
	preparedAt time.Time,
) (checkpointepoch.Report, error) {
	stub.preparedOperationID = operationID
	return checkpointEpochTestReport(input.Binding, input.WorkerUnit, operationID, preparedAt, checkpointepoch.StatusPrepared), nil
}

func (stub *checkpointEpochRepositoryStub) Activate(
	_ context.Context,
	operationID string,
	planSHA256 string,
	activatedAt time.Time,
) (checkpointepoch.Report, error) {
	stub.activatedOperationID = operationID
	stub.activatedPlanSHA256 = planSHA256
	return checkpointEpochTestReport(checkpointepoch.Binding{}, stub.workerUnit, operationID, activatedAt, checkpointepoch.StatusActivated), nil
}

func (stub *checkpointEpochRepositoryStub) Abort(
	_ context.Context,
	operationID string,
	_ string,
	abortedAt time.Time,
) (checkpointepoch.Report, error) {
	return checkpointEpochTestReport(checkpointepoch.Binding{}, stub.workerUnit, operationID, abortedAt, checkpointepoch.StatusAborted), nil
}

func (stub *checkpointEpochRepositoryStub) StatusByBinding(
	_ context.Context,
	binding checkpointepoch.Binding,
	now time.Time,
) (checkpointepoch.Report, error) {
	return checkpointEpochTestReport(binding, stub.workerUnit, "", now, ""), nil
}

func (stub *checkpointEpochRepositoryStub) StatusByOperation(
	_ context.Context,
	operationID string,
	now time.Time,
) (checkpointepoch.Report, error) {
	status := stub.operationStatus
	if status == "" {
		status = checkpointepoch.StatusPrepared
	}
	return checkpointEpochTestReport(checkpointepoch.Binding{}, stub.workerUnit, operationID, now, status), nil
}

func (stub *checkpointEpochRepositoryStub) OperationWorkerUnit(context.Context, string) (string, error) {
	if stub.workerUnit == "" {
		return checkpointepoch.CompletedExecutionWorkerUnit, nil
	}
	return stub.workerUnit, nil
}

func (stub *checkpointEpochRepositoryStub) Close() error { return nil }

func checkpointEpochTestReport(
	binding checkpointepoch.Binding,
	workerUnit string,
	operationID string,
	now time.Time,
	status string,
) checkpointepoch.Report {
	report := checkpointepoch.Report{
		SchemaVersion: checkpointepoch.ReportSchema,
		Active:        checkpointepoch.ActiveStateSummary{Binding: binding, Epoch: 1, WorkerStatus: "inactive"},
		GeneratedAt:   now,
	}
	if operationID != "" {
		report.Operation = &checkpointepoch.OperationSummary{
			OperationID: operationID,
			Status:      status,
			Plan: checkpointepoch.Plan{
				SchemaVersion: checkpointepoch.PlanSchema,
				OperationID:   operationID,
				Binding:       binding,
				WorkerUnit:    workerUnit,
				PlanSHA256:    "sha256:" + strings.Repeat("b", 64),
			},
		}
	}
	return report
}

func TestCheckpointEpochStatusAndAbortKeepWorkerDormant(t *testing.T) {
	engine, paths := newConnectorTargetTestEngine(t)
	stageDatabaseCredentials(t, paths)
	runner := &fakeRunner{feedWorkerLoaded: true}
	engine.Runner = runner
	repository := &checkpointEpochRepositoryStub{workerUnit: checkpointepoch.CompletedExecutionWorkerUnit}
	engine.CheckpointEpochOpen = func(context.Context, string) (CheckpointEpochRepository, error) {
		return repository, nil
	}
	ctx := context.Background()
	operationID := strings.Repeat("c", 32)
	binding := checkpointepoch.Binding{
		SourceSystemID:    "11111111-1111-4111-8111-111111111111",
		AdapterManifestID: "22222222-2222-4222-8222-222222222222",
		CursorKey:         "unirouter.completed-execution.v1",
	}
	byOperation, err := engine.CheckpointEpochStatus(ctx, nil, operationID, "")
	if err != nil || byOperation.Epoch.Operation == nil || byOperation.Epoch.Operation.Status != checkpointepoch.StatusPrepared || !byOperation.Worker.Present || byOperation.Worker.Active {
		t.Fatalf("operation status=%+v err=%v", byOperation, err)
	}
	byBinding, err := engine.CheckpointEpochStatus(ctx, &binding, "", checkpointepoch.CompletedExecutionWorkerUnit)
	if err != nil || byBinding.Epoch.Active.Binding != binding || byBinding.Epoch.Operation != nil {
		t.Fatalf("binding status=%+v err=%v", byBinding, err)
	}
	if _, err := engine.CheckpointEpochStatus(ctx, &binding, operationID, ""); err == nil {
		t.Fatal("mixed operation and binding status accepted")
	}
	if _, err := engine.CheckpointEpochStatus(ctx, nil, "", ""); err == nil {
		t.Fatal("unbound status request accepted")
	}
	aborted, err := engine.AbortCheckpointEpoch(ctx, operationID, "sha256:"+strings.Repeat("b", 64))
	if err != nil || aborted.Epoch.Operation == nil || aborted.Epoch.Operation.Status != checkpointepoch.StatusAborted || aborted.Worker.Active {
		t.Fatalf("abort report=%+v err=%v", aborted, err)
	}
	if runner.feedWorkerActive || runner.feedWorkerEnabled {
		t.Fatal("status or abort started the source worker")
	}
}
