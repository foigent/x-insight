package connectoractivation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

const (
	activationInstallationID = "xci_AAAAAAAAAAAAAAAAAAAAAA"
	activationDeploymentID   = "xid_AAAAAAAAAAAAAAAAAAAAAA"
	activationIdentityID     = "xsi_AAAAAAAAAAAAAAAAAAAAAA"
	activationPrepareID      = "xco_AAAAAAAAAAAAAAAAAAAAAA"
	activationRegisterID     = "xco_BBBBBBBBBBBBBBBBBBBBBB"
	activationResumeID       = "xco_CCCCCCCCCCCCCCCCCC_CCC"
	activationProjectID      = "Project-ExactCase"
	activationSourceInstance = "ecs.OneClick.Source.v1"
)

var activationNow = time.Date(2030, 1, 1, 0, 15, 0, 0, time.UTC)

func TestProductReconcilerConvergesThreePhasesAndStartsWorkerLast(t *testing.T) {
	store, epochPort, host, reconciler := newActivationHarness(t)
	ctx := context.Background()

	outcome, err := reconciler.Reconcile(ctx, activationInstallationID, activationPrepareID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State != StateDeferred || epochPort.commitCalls != 1 || store.phaseCreateCalls != 1 ||
		store.claimCalls != 1 ||
		store.boundary.RegisterReceiver == nil || store.boundary.ResumeSource != nil ||
		host.stageCalls != 1 || host.prepareCalls != 1 || host.promoteCalls != 0 ||
		host.dormantCalls != 0 || host.workerCalls != 0 || store.finalizeCalls != 0 {
		t.Fatalf("phase1 outcome=%+v epoch=%+v store=%+v host=%+v", outcome, epochPort, store, host)
	}
	registerRequest := store.boundary.RegisterReceiver.Operation.Request
	if registerRequest == nil || registerRequest.ConsumerEpoch != 1 ||
		registerRequest.SourceInstance != activationSourceInstance ||
		registerRequest.CheckpointAuthoritySHA256 == "" || registerRequest.ReceiverRegistration == nil ||
		registerRequest.NetworkBinding == nil {
		t.Fatalf("register request=%+v", registerRequest)
	}

	// A lost phase-2 enqueue response converges to the same durable operation;
	// it never creates a second epoch or starts the worker.
	outcome, err = reconciler.Reconcile(ctx, activationInstallationID, activationPrepareID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State != StateDeferred || epochPort.commitCalls != 1 || store.phaseCreateCalls != 1 ||
		store.claimCalls != 1 || store.enqueueCalls != 2 || host.workerCalls != 0 {
		t.Fatalf("phase2 replay outcome=%+v epoch=%+v store=%+v host=%+v", outcome, epochPort, store, host)
	}

	completeActivationPhase(t, store.boundary.RegisterReceiver, connectorcontrol.ControlPhaseResultV2{
		SourceInstance:             activationSourceInstance,
		ReceiverRegistrationSHA256: canonicalRegistrationSHA(t, *registerRequest.ReceiverRegistration),
		NetworkBindingSHA256:       registerRequest.NetworkBinding.BindingSHA256,
		SourceState:                "paused",
		ReceiverState:              "feed_staged",
		Readiness:                  connectorcontrol.ReadinessPausedReceiverRegistered,
	}, activationNow.Add(2*time.Minute))

	outcome, err = reconciler.Reconcile(ctx, activationInstallationID, activationRegisterID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State != StateDeferred || store.phaseCreateCalls != 2 || store.boundary.ResumeSource == nil ||
		host.promoteCalls != 1 || host.dormantCalls != 1 || host.workerCalls != 0 || store.finalizeCalls != 0 {
		t.Fatalf("phase2 completion outcome=%+v store=%+v host=%+v", outcome, store, host)
	}
	resumeRequest := store.boundary.ResumeSource.Operation.Request
	if resumeRequest == nil ||
		resumeRequest.PausedAuthReceiptSHA256 != host.pausedAuthSHA256 ||
		resumeRequest.DormantCredentialReceiptSHA256 != host.dormantCredentialSHA256 ||
		resumeRequest.ReceiverRegistrationSHA256 != canonicalRegistrationSHA(t, *registerRequest.ReceiverRegistration) ||
		resumeRequest.NetworkBindingSHA256 != registerRequest.NetworkBinding.BindingSHA256 {
		t.Fatalf("resume request=%+v", resumeRequest)
	}

	// A pending resume is read-only with respect to worker activation.
	outcome, err = reconciler.Reconcile(ctx, activationInstallationID, activationResumeID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State != StateDeferred || host.workerCalls != 0 || store.finalizeCalls != 0 {
		t.Fatalf("pending resume outcome=%+v store=%+v host=%+v", outcome, store, host)
	}

	completeActivationPhase(t, store.boundary.ResumeSource, connectorcontrol.ControlPhaseResultV2{
		SourceInstance: activationSourceInstance,
		SourceState:    "active",
		ReceiverState:  "active",
		Readiness:      connectorcontrol.ReadinessSourceResumedReceiverActive,
	}, activationNow.Add(4*time.Minute))
	outcome, err = reconciler.Reconcile(ctx, activationInstallationID, activationResumeID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State != StateReady || host.workerCalls != 1 || host.actualWorkerStarts != 1 ||
		store.finalizeCalls != 1 || store.boundary.Installation.Status != "connected" {
		t.Fatalf("activation outcome=%+v store=%+v host=%+v", outcome, store, host)
	}

	// A connected replay exits before epoch/config/credential/worker mutation.
	counts := activationCallCounts{epochPort.inspectCalls, epochPort.commitCalls, store.claimCalls, store.enqueueCalls,
		host.stageCalls, host.prepareCalls, host.promoteCalls, host.dormantCalls, host.workerCalls, store.finalizeCalls}
	outcome, err = reconciler.Reconcile(ctx, activationInstallationID, activationResumeID)
	if err != nil {
		t.Fatal(err)
	}
	after := activationCallCounts{epochPort.inspectCalls, epochPort.commitCalls, store.claimCalls, store.enqueueCalls,
		host.stageCalls, host.prepareCalls, host.promoteCalls, host.dormantCalls, host.workerCalls, store.finalizeCalls}
	if outcome.State != StateReady || after != counts {
		t.Fatalf("connected replay outcome=%+v before=%+v after=%+v", outcome, counts, after)
	}
}

func TestProductReconcilerRecoversAmbiguousWorkerStartWithoutFalseConnected(t *testing.T) {
	store, _, host, reconciler := newActivationHarness(t)
	advanceActivationToCompletedResume(t, store, host, reconciler)
	host.ambiguousWorkerOnce = true

	outcome, err := reconciler.Reconcile(context.Background(), activationInstallationID, activationResumeID)
	if err == nil || outcome.State != "" {
		t.Fatalf("ambiguous worker outcome=%+v error=%v", outcome, err)
	}
	if !host.workerActivated || host.actualWorkerStarts != 1 || store.finalizeCalls != 0 ||
		store.boundary.Installation.Status == "connected" {
		t.Fatalf("ambiguous state store=%+v host=%+v", store, host)
	}

	outcome, err = reconciler.Reconcile(context.Background(), activationInstallationID, activationResumeID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State != StateReady || host.workerCalls != 2 || host.actualWorkerStarts != 1 ||
		store.finalizeCalls != 1 || store.boundary.Installation.Status != "connected" {
		t.Fatalf("recovery outcome=%+v store=%+v host=%+v", outcome, store, host)
	}
}

func TestProductReconcilerRecoversAfterWorkerReadbackBeforeDatabaseFinalize(t *testing.T) {
	store, _, host, reconciler := newActivationHarness(t)
	advanceActivationToCompletedResume(t, store, host, reconciler)
	store.finalizeErrOnce = true

	if _, err := reconciler.Reconcile(context.Background(), activationInstallationID, activationResumeID); err == nil {
		t.Fatal("expected simulated lost database finalize response")
	}
	if host.actualWorkerStarts != 1 || store.boundary.Installation.Status == "connected" {
		t.Fatalf("pre-recovery store=%+v host=%+v", store, host)
	}
	outcome, err := reconciler.Reconcile(context.Background(), activationInstallationID, activationResumeID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State != StateReady || host.actualWorkerStarts != 1 || store.finalizeCalls != 2 ||
		store.boundary.Installation.Status != "connected" {
		t.Fatalf("recovery outcome=%+v store=%+v host=%+v", outcome, store, host)
	}
}

func TestProductReconcilerPendingAndFailedPhasesCannotStartWorker(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*fakeActivationStore)
		wantState State
	}{
		{
			name: "prepare pending",
			mutate: func(store *fakeActivationStore) {
				setActivationPhasePending(store.boundary.PreparePausedSource)
			},
			wantState: StateDeferred,
		},
		{
			name: "prepare failed",
			mutate: func(store *fakeActivationStore) {
				failActivationPhase(t, store.boundary.PreparePausedSource, activationNow.Add(time.Minute))
			},
			wantState: StateDrift,
		},
		{
			name: "register failed",
			mutate: func(store *fakeActivationStore) {
				_, _, host, reconciler := existingActivationHarness(t, store)
				if _, err := reconciler.Reconcile(context.Background(), activationInstallationID, activationPrepareID); err != nil {
					t.Fatal(err)
				}
				_ = host
				failActivationPhase(t, store.boundary.RegisterReceiver, activationNow.Add(2*time.Minute))
			},
			wantState: StateDrift,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, epochPort, host, reconciler := newActivationHarness(t)
			test.mutate(store)
			outcome, err := reconciler.Reconcile(context.Background(), activationInstallationID, activationPrepareID)
			if err != nil {
				t.Fatal(err)
			}
			if outcome.State != test.wantState || host.workerCalls != 0 || store.finalizeCalls != 0 {
				t.Fatalf("outcome=%+v epoch=%+v store=%+v host=%+v", outcome, epochPort, store, host)
			}
		})
	}
}

func TestProductReconcilerRejectsInactiveInstallationBeforeAnyMutation(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		desired  string
		observed string
	}{
		{name: "paused", status: "paused", desired: "paused", observed: "paused"},
		{name: "disconnected", status: "disconnected", desired: "disconnected", observed: "disconnected"},
		{name: "pending agent registered", status: "pending", desired: "active", observed: "agent_registered"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, epochPort, host, reconciler := newActivationHarness(t)
			store.boundary.Installation.Status = test.status
			store.boundary.Installation.DesiredState = test.desired
			store.boundary.Installation.ObservedState = test.observed
			store.boundary.Installation.Version++
			if err := connectorcontrol.ValidateActivationBoundary(store.boundary); err != nil {
				t.Fatalf("test boundary: %v", err)
			}

			outcome, err := reconciler.Reconcile(context.Background(), activationInstallationID, activationPrepareID)
			if !errors.Is(err, connectorcontrol.ErrConflict) || outcome.State != "" {
				t.Fatalf("outcome=%+v error=%v", outcome, err)
			}
			if store.claimCalls != 0 || epochPort.inspectCalls != 0 || epochPort.commitCalls != 0 || store.enqueueCalls != 0 ||
				host.stageCalls != 0 || host.prepareCalls != 0 || host.promoteCalls != 0 ||
				host.dormantCalls != 0 || host.workerCalls != 0 || store.finalizeCalls != 0 ||
				store.boundary.Installation.Status == "connected" {
				t.Fatalf("inactive installation mutated epoch=%+v store=%+v host=%+v", epochPort, store, host)
			}
		})
	}
}

func TestProductReconcilerRevalidatesAuthorityBeforeWorkerActivation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *connectorcontrol.ActivationBoundary)
	}{
		{
			name: "version drift",
			mutate: func(_ *testing.T, boundary *connectorcontrol.ActivationBoundary) {
				boundary.Installation.Version++
			},
		},
		{
			name: "paused lifecycle drift",
			mutate: func(_ *testing.T, boundary *connectorcontrol.ActivationBoundary) {
				boundary.Installation.Status = "paused"
				boundary.Installation.DesiredState = "paused"
				boundary.Installation.ObservedState = "paused"
				boundary.Installation.Version++
			},
		},
		{
			name: "disconnected lifecycle drift",
			mutate: func(_ *testing.T, boundary *connectorcontrol.ActivationBoundary) {
				boundary.Installation.Status = "disconnected"
				boundary.Installation.DesiredState = "disconnected"
				boundary.Installation.ObservedState = "disconnected"
				boundary.Installation.Version++
			},
		},
		{
			name: "observed state drift",
			mutate: func(_ *testing.T, boundary *connectorcontrol.ActivationBoundary) {
				boundary.Installation.ObservedState = "source_ready"
				boundary.Installation.Version++
			},
		},
		{
			name: "completed resume authority drift",
			mutate: func(t *testing.T, boundary *connectorcontrol.ActivationBoundary) {
				completeActivationPhase(t, boundary.ResumeSource, connectorcontrol.ControlPhaseResultV2{
					SourceInstance: activationSourceInstance, SourceState: "active", ReceiverState: "active",
					Readiness: connectorcontrol.ReadinessSourceResumedReceiverActive,
				}, activationNow.Add(4*time.Minute+time.Second))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, _, host, reconciler := newActivationHarness(t)
			advanceActivationToCompletedResume(t, store, host, reconciler)
			driftAtInspect := store.inspectCalls + 2
			store.inspectHook = func(call int, boundary *connectorcontrol.ActivationBoundary) {
				if call == driftAtInspect {
					test.mutate(t, boundary)
				}
			}
			beforeWorkerCalls := host.workerCalls
			beforeFinalizes := store.finalizeCalls

			outcome, err := reconciler.Reconcile(context.Background(), activationInstallationID, activationResumeID)
			if !errors.Is(err, connectorcontrol.ErrConflict) || outcome.State != "" {
				t.Fatalf("outcome=%+v error=%v", outcome, err)
			}
			if host.workerCalls != beforeWorkerCalls || host.actualWorkerStarts != 0 ||
				store.finalizeCalls != beforeFinalizes || store.boundary.Installation.Status == "connected" {
				t.Fatalf("stale authority crossed worker boundary store=%+v host=%+v", store, host)
			}
		})
	}
}

func TestConnectorActivationProductionFilesDoNotAddBusinessTransport(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	entries, err := os.ReadDir(filepath.Dir(filename))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(filepath.Dir(filename), entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{`"net/http"`, `"google.golang.org/grpc"`, `"github.com/gorilla/websocket"`} {
			if strings.Contains(string(payload), forbidden) {
				t.Fatalf("%s imports business transport %s", entry.Name(), forbidden)
			}
		}
	}
}

func TestOriginalActivationConcurrentExactConnectedFinalizerIsReadOnly(t *testing.T) {
	for _, scenario := range []string{"same-chain", "wrong-control-identity", "wrong-version", "changed-resume-digest", "paused"} {
		t.Run(scenario, func(t *testing.T) {
			store, _, host, reconciler := newActivationHarness(t)
			advanceActivationToCompletedResume(t, store, host, reconciler)
			freshRead := store.inspectCalls + 2
			store.inspectHook = func(call int, b *connectorcontrol.ActivationBoundary) {
				if call != freshRead {
					return
				}
				if _, err := store.FinalizeInstallationConnected(context.Background(), b.Installation.InstallationID, b.Installation.Version, b.ResumeSource.Operation.OperationID, b.ResumeSource.Operation.ResultSHA256, activationNow.Add(5*time.Minute)); err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "wrong-control-identity":
					b.Installation.ControlServiceIdentityID = "xsi_BBBBBBBBBBBBBBBBBBBBBB"
				case "wrong-version":
					b.Installation.Version++
				case "changed-resume-digest":
					completeActivationPhase(t, b.ResumeSource, connectorcontrol.ControlPhaseResultV2{SourceInstance: activationSourceInstance, SourceState: "active", ReceiverState: "active", Readiness: connectorcontrol.ReadinessSourceResumedReceiverActive}, activationNow.Add(4*time.Minute+time.Second))
				case "paused":
					b.Installation.Status = "paused"
					b.Installation.DesiredState = "paused"
					b.Installation.ObservedState = "paused"
				}
			}
			beforeWorker, beforeFinalizes := host.workerCalls, store.finalizeCalls
			outcome, err := reconciler.Reconcile(context.Background(), activationInstallationID, activationResumeID)
			if scenario == "same-chain" {
				if err != nil || outcome.State != StateReady {
					t.Fatalf("same chain advancement rejected: outcome=%s error=%v", outcome.State, err)
				}
			} else if !errors.Is(err, connectorcontrol.ErrConflict) || outcome.State != "" {
				t.Fatalf("changed authority accepted: outcome=%s error=%v", outcome.State, err)
			}
			if host.workerCalls != beforeWorker || store.finalizeCalls != beforeFinalizes+1 {
				t.Fatal("stale pass mutated host or repeated finalization")
			}
		})
	}
}

type activationCallCounts struct {
	epochInspects, epochCommits, claims, enqueues, stages, prepares, promotes, dormants, workers, finalizes int
}

type fakeActivationStore struct {
	boundary         connectorcontrol.ActivationBoundary
	inspectCalls     int
	claimCalls       int
	enqueueCalls     int
	phaseCreateCalls int
	finalizeCalls    int
	finalizeErrOnce  bool
	inspectHook      func(int, *connectorcontrol.ActivationBoundary)
}

func (store *fakeActivationStore) ClaimInstallationActivation(
	_ context.Context,
	installationID string,
	expectedVersion int64,
	prepareOperationID string,
	prepareResultSHA256 string,
	now time.Time,
) (connectorcontrol.ActivationBoundary, error) {
	store.claimCalls++
	prepare := store.boundary.PreparePausedSource
	if installationID != store.boundary.Installation.InstallationID || prepare == nil || prepare.Result == nil ||
		prepare.Operation.Status != "completed" || prepare.Operation.OperationID != prepareOperationID ||
		prepare.Operation.ResultSHA256 != prepareResultSHA256 {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
	}
	installation := &store.boundary.Installation
	if installation.Status == "pending" && installation.DesiredState == "active" &&
		installation.ObservedState == "controller_pending" && installation.Version == expectedVersion+1 {
		return store.boundary, nil
	}
	if installation.Version != expectedVersion || installation.Status != "pending" ||
		installation.DesiredState != "active" || installation.ObservedState != "source_ready" {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
	}
	installation.ObservedState = "controller_pending"
	installation.UpdatedAt = now
	installation.Version++
	return store.boundary, nil
}

func (store *fakeActivationStore) InspectActivationBoundary(context.Context, string) (connectorcontrol.ActivationBoundary, error) {
	store.inspectCalls++
	if store.inspectHook != nil {
		store.inspectHook(store.inspectCalls, &store.boundary)
	}
	return store.boundary, nil
}

func (store *fakeActivationStore) EnqueueControlPhase(
	_ context.Context,
	input connectorcontrol.EnqueueControlPhaseInput,
	now time.Time,
	_ io.Reader,
) (connectorcontrol.ControlOperation, error) {
	store.enqueueCalls++
	var existing **connectorcontrol.ActivationPhase
	operationID := ""
	switch input.Phase {
	case connectorcontrol.ControlPhaseRegisterReceiver:
		existing = &store.boundary.RegisterReceiver
		operationID = activationRegisterID
	case connectorcontrol.ControlPhaseResumeSource:
		existing = &store.boundary.ResumeSource
		operationID = activationResumeID
	default:
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrInvalid
	}
	if *existing != nil {
		operation := (*existing).Operation
		if operation.InstallationID != input.InstallationID || operation.Phase != input.Phase ||
			operation.PredecessorOperationID != input.PredecessorOperationID ||
			operation.PredecessorResultSHA256 != input.PredecessorResultSHA256 ||
			operation.Request == nil || !reflect.DeepEqual(*operation.Request, input.Request) {
			return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
		}
		return operation, nil
	}
	request := input.Request
	operation := connectorcontrol.ControlOperation{
		SchemaVersion:           connectorcontrol.ControlOperationSchemaV2,
		OperationID:             operationID,
		InstallationID:          input.InstallationID,
		Kind:                    "connect",
		Phase:                   input.Phase,
		ProjectID:               store.boundary.Installation.ProjectID,
		EventScope:              append([]string(nil), store.boundary.Installation.FinalEventScope...),
		DesiredState:            "active",
		PredecessorOperationID:  input.PredecessorOperationID,
		PredecessorResultSHA256: input.PredecessorResultSHA256,
		Request:                 &request,
		Status:                  "pending",
		AvailableAt:             now,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	_, digest, _, err := connectorcontrol.CanonicalControlOperationRequestV2(operation)
	if err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	operation.RequestSHA256 = digest
	phase := &connectorcontrol.ActivationPhase{Operation: operation}
	*existing = phase
	store.phaseCreateCalls++
	if err := connectorcontrol.ValidateActivationBoundary(store.boundary); err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	return operation, nil
}

func (store *fakeActivationStore) FinalizeInstallationConnected(
	_ context.Context,
	installationID string,
	expectedVersion int64,
	operationID string,
	resultSHA256 string,
	now time.Time,
) (connectorcontrol.Installation, error) {
	store.finalizeCalls++
	if store.finalizeErrOnce {
		store.finalizeErrOnce = false
		return connectorcontrol.Installation{}, errors.New("database finalize response was lost")
	}
	if store.boundary.Installation.Status == "connected" {
		return store.boundary.Installation, nil
	}
	resume := store.boundary.ResumeSource
	if installationID != store.boundary.Installation.InstallationID ||
		expectedVersion != store.boundary.Installation.Version || resume == nil || resume.Result == nil ||
		resume.Operation.OperationID != operationID || resume.Operation.ResultSHA256 != resultSHA256 ||
		resume.Operation.Status != "completed" {
		return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
	}
	store.boundary.Installation.Status = "connected"
	store.boundary.Installation.ObservedState = "connected"
	store.boundary.Installation.UpdatedAt = now
	store.boundary.Installation.Version++
	if err := connectorcontrol.ValidateActivationBoundary(store.boundary); err != nil {
		return connectorcontrol.Installation{}, err
	}
	return store.boundary.Installation, nil
}

type fakeActivationEpochPort struct {
	boundary     checkpointepoch.InstallationEpochBoundary
	inspectCalls int
	commitCalls  int
}

func (port *fakeActivationEpochPort) InspectInstallationEpochBoundary(
	_ context.Context,
	binding checkpointepoch.Binding,
) (checkpointepoch.InstallationEpochBoundary, error) {
	port.inspectCalls++
	if port.boundary.SchemaVersion == "" {
		port.boundary = checkpointepoch.InstallationEpochBoundary{
			SchemaVersion: checkpointepoch.InstallationEpochBoundarySchema,
			Binding:       binding,
		}
	}
	return port.boundary, nil
}

func (port *fakeActivationEpochPort) CommitInstallationEpoch(
	_ context.Context,
	commit checkpointepoch.InstallationEpochCommit,
	_ time.Time,
) (checkpointepoch.ActiveStateSummary, error) {
	port.commitCalls++
	cursorSHA := activationSHA256(commit.NextCursor)
	highWatermarkSHA := activationSHA256(commit.NextHighWatermark)
	active := checkpointepoch.ActiveStateSummary{
		StateID:             "connector-installation-state",
		Binding:             commit.Binding,
		Epoch:               commit.Epoch,
		SourceInstance:      commit.NextSourceInstance,
		CursorSHA256:        cursorSHA,
		HighWatermarkSHA256: highWatermarkSHA,
		WorkerStatus:        "inactive",
		StateSHA256:         activationSHA256("connector-installation-state"),
	}
	port.boundary = checkpointepoch.InstallationEpochBoundary{
		SchemaVersion: checkpointepoch.InstallationEpochBoundarySchema,
		Binding:       commit.Binding,
		Records: []checkpointepoch.InstallationEpochRecord{{
			RecordID: "connector-installation-state", Kind: checkpointepoch.InstallationEpochRecordActive,
			Binding: commit.Binding, Epoch: commit.Epoch, AuthorizationRef: commit.AuthorizationRef,
			SourceInstance: commit.NextSourceInstance, CursorSHA256: cursorSHA, HighWatermarkSHA256: highWatermarkSHA,
		}},
		Active: &active,
	}
	return active, nil
}

type fakeActivationHost struct {
	configSHA256            string
	networkBinding          connectorcontrol.FixedReceiverNetworkBinding
	registration            connectorcontrol.PublicReceiverRegistrationV1
	pausedAuthSHA256        string
	dormantCredentialSHA256 string
	stageCalls              int
	prepareCalls            int
	promoteCalls            int
	dormantCalls            int
	workerCalls             int
	actualWorkerStarts      int
	workerActivated         bool
	ambiguousWorkerOnce     bool
}

func (host *fakeActivationHost) StageManagedFeedConfig(context.Context, string) (ManagedConfigReadback, error) {
	host.stageCalls++
	return ManagedConfigReadback{ConfigSHA256: host.configSHA256, NetworkBinding: host.networkBinding}, nil
}

func (host *fakeActivationHost) PromoteManagedFeedConfig(context.Context, string) (ManagedConfigReadback, error) {
	host.promoteCalls++
	return ManagedConfigReadback{ConfigSHA256: host.configSHA256, NetworkBinding: host.networkBinding}, nil
}

func (host *fakeActivationHost) PrepareFeedCredential(context.Context, string, string) (connectorcontrol.PublicReceiverRegistrationV1, error) {
	host.prepareCalls++
	return host.registration, nil
}

func (host *fakeActivationHost) PromoteFeedCredentialDormant(context.Context, string, string) (DormantReceiverReadback, error) {
	host.dormantCalls++
	return DormantReceiverReadback{
		PausedAuthSHA256: host.pausedAuthSHA256, DormantCredentialSHA256: host.dormantCredentialSHA256,
	}, nil
}

func (host *fakeActivationHost) ActivateFeedWorkerLast(context.Context, string, string) (WorkerReadback, error) {
	host.workerCalls++
	if !host.workerActivated {
		host.workerActivated = true
		host.actualWorkerStarts++
		if host.ambiguousWorkerOnce {
			host.ambiguousWorkerOnce = false
			return WorkerReadback{}, errors.New("worker start response was lost")
		}
	}
	return WorkerReadback{Present: true, Active: true, Enabled: true}, nil
}

func newActivationHarness(t *testing.T) (*fakeActivationStore, *fakeActivationEpochPort, *fakeActivationHost, *ProductReconciler) {
	t.Helper()
	prepare := newCompletedPreparePhase(t)
	installation := connectorcontrol.Installation{
		SchemaVersion: connectorcontrol.InstallationSchema, InstallationID: activationInstallationID,
		DeploymentID: activationDeploymentID, ConnectorType: connectorcontrol.ConnectorTypeUniRouter,
		RequestedEventScope: []string{connectorcontrol.CompletedEventType},
		FinalEventScope:     []string{connectorcontrol.CompletedEventType}, ProjectID: activationProjectID,
		SourceInstance: activationSourceInstance, SourceInstanceSHA256: connectorcontrol.RawSHA256(activationSourceInstance),
		ControlServiceIdentityID:          activationIdentityID,
		InstallationAcknowledgementSHA256: activationSHA256("installation-ack"),
		Status:                            "pending", DesiredState: "active", ObservedState: "source_ready",
		CreatedAt: activationNow.Add(-time.Hour), UpdatedAt: activationNow, Version: 3,
	}
	store := &fakeActivationStore{boundary: connectorcontrol.ActivationBoundary{
		Installation: installation, UniRouterOrigin: "https://unirouter.example", PreparePausedSource: prepare,
	}}
	epochPort := &fakeActivationEpochPort{}
	host := &fakeActivationHost{
		configSHA256: activationSHA256("managed-config"),
		networkBinding: connectorcontrol.FixedReceiverNetworkBinding{
			Transport: connectorcontrol.ReceiverNetworkTransport, Route: connectorcontrol.ReceiverFeedRoute,
			BindingSHA256: activationSHA256("network-binding"),
		},
		registration: connectorcontrol.PublicReceiverRegistrationV1{
			SchemaVersion: connectorcontrol.ReceiverRegistrationSchema,
			TokenFormat:   connectorcontrol.ReceiverTokenFormat, VerifierScheme: connectorcontrol.ReceiverVerifierScheme,
			KeyPrefix: "ur-xf1-0123456789abc", VerifierSHA256: strings.Repeat("a", 64),
			OperationID: strings.Repeat("b", 32),
		},
		pausedAuthSHA256: activationSHA256("paused-auth"), dormantCredentialSHA256: activationSHA256("dormant-credential"),
	}
	reconciler, err := NewProductReconciler(store, epochPort, host, func() time.Time { return activationNow }, strings.NewReader(strings.Repeat("r", 128)))
	if err != nil {
		t.Fatal(err)
	}
	if err := connectorcontrol.ValidateActivationBoundary(store.boundary); err != nil {
		t.Fatalf("initial boundary: %v", err)
	}
	return store, epochPort, host, reconciler
}

func existingActivationHarness(t *testing.T, store *fakeActivationStore) (*fakeActivationEpochPort, *fakeActivationStore, *fakeActivationHost, *ProductReconciler) {
	t.Helper()
	epochPort := &fakeActivationEpochPort{}
	host := &fakeActivationHost{
		configSHA256: activationSHA256("managed-config"),
		networkBinding: connectorcontrol.FixedReceiverNetworkBinding{
			Transport: connectorcontrol.ReceiverNetworkTransport, Route: connectorcontrol.ReceiverFeedRoute,
			BindingSHA256: activationSHA256("network-binding"),
		},
		registration: connectorcontrol.PublicReceiverRegistrationV1{
			SchemaVersion: connectorcontrol.ReceiverRegistrationSchema,
			TokenFormat:   connectorcontrol.ReceiverTokenFormat, VerifierScheme: connectorcontrol.ReceiverVerifierScheme,
			KeyPrefix: "ur-xf1-0123456789abc", VerifierSHA256: strings.Repeat("a", 64), OperationID: strings.Repeat("b", 32),
		},
		pausedAuthSHA256: activationSHA256("paused-auth"), dormantCredentialSHA256: activationSHA256("dormant-credential"),
	}
	reconciler, err := NewProductReconciler(store, epochPort, host, func() time.Time { return activationNow }, strings.NewReader(strings.Repeat("r", 128)))
	if err != nil {
		t.Fatal(err)
	}
	return epochPort, store, host, reconciler
}

func advanceActivationToCompletedResume(t *testing.T, store *fakeActivationStore, host *fakeActivationHost, reconciler *ProductReconciler) {
	t.Helper()
	ctx := context.Background()
	if _, err := reconciler.Reconcile(ctx, activationInstallationID, activationPrepareID); err != nil {
		t.Fatal(err)
	}
	register := store.boundary.RegisterReceiver
	completeActivationPhase(t, register, connectorcontrol.ControlPhaseResultV2{
		SourceInstance:             activationSourceInstance,
		ReceiverRegistrationSHA256: canonicalRegistrationSHA(t, *register.Operation.Request.ReceiverRegistration),
		NetworkBindingSHA256:       register.Operation.Request.NetworkBinding.BindingSHA256,
		SourceState:                "paused", ReceiverState: "feed_staged", Readiness: connectorcontrol.ReadinessPausedReceiverRegistered,
	}, activationNow.Add(2*time.Minute))
	if _, err := reconciler.Reconcile(ctx, activationInstallationID, activationRegisterID); err != nil {
		t.Fatal(err)
	}
	completeActivationPhase(t, store.boundary.ResumeSource, connectorcontrol.ControlPhaseResultV2{
		SourceInstance: activationSourceInstance, SourceState: "active", ReceiverState: "active",
		Readiness: connectorcontrol.ReadinessSourceResumedReceiverActive,
	}, activationNow.Add(4*time.Minute))
	if host.workerCalls != 0 {
		t.Fatal("test setup activated worker before completed resume reconciliation")
	}
}

func newCompletedPreparePhase(t *testing.T) *connectorcontrol.ActivationPhase {
	t.Helper()
	request := connectorcontrol.ControlPhaseRequestV2{
		FeedSchema: connectorcontrol.ControlFeedSchema, CursorKey: connectorcontrol.ControlCursorKey,
		RequiredSourceState: "paused",
	}
	createdAt := activationNow.Add(-10 * time.Minute)
	operation := connectorcontrol.ControlOperation{
		SchemaVersion: connectorcontrol.ControlOperationSchemaV2, OperationID: activationPrepareID,
		InstallationID: activationInstallationID, Kind: "connect", Phase: connectorcontrol.ControlPhasePreparePausedSource,
		ProjectID: activationProjectID, EventScope: []string{connectorcontrol.CompletedEventType}, DesiredState: "active",
		Request: &request, Status: "pending", AvailableAt: createdAt, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	_, digest, _, err := connectorcontrol.CanonicalControlOperationRequestV2(operation)
	if err != nil {
		t.Fatal(err)
	}
	operation.RequestSHA256 = digest
	phase := &connectorcontrol.ActivationPhase{Operation: operation}
	completeActivationPhase(t, phase, connectorcontrol.ControlPhaseResultV2{
		SourceInstance: activationSourceInstance, Cursor: "cursor:initial", HighWatermark: "cursor:initial",
		SourceState: "paused", Readiness: connectorcontrol.ReadinessPausedSourceCursorReady,
	}, activationNow.Add(-8*time.Minute))
	return phase
}

func completeActivationPhase(t *testing.T, phase *connectorcontrol.ActivationPhase, result connectorcontrol.ControlPhaseResultV2, observedAt time.Time) {
	t.Helper()
	value := connectorcontrol.ControlOperationResult{
		SchemaVersion: connectorcontrol.ControlResultSchemaV2, OperationID: phase.Operation.OperationID,
		InstallationID: phase.Operation.InstallationID, Phase: phase.Operation.Phase,
		Status: "completed", Result: &result, ObservedAt: observedAt,
	}
	if err := connectorcontrol.ValidateControlResultV2ForOperation(phase.Operation, value); err != nil {
		t.Fatalf("complete %s: %v", phase.Operation.Phase, err)
	}
	digest, _, err := connectorcontrol.CanonicalControlResultV2(value)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := observedAt
	phase.Operation.Status = "completed"
	phase.Operation.AttemptCount = 1
	phase.Operation.CompletedAt = &completedAt
	phase.Operation.UpdatedAt = observedAt
	phase.Operation.ResultSHA256 = digest
	phase.Result = &value
}

func failActivationPhase(t *testing.T, phase *connectorcontrol.ActivationPhase, observedAt time.Time) {
	t.Helper()
	value := connectorcontrol.ControlOperationResult{
		SchemaVersion: connectorcontrol.ControlResultSchemaV2, OperationID: phase.Operation.OperationID,
		InstallationID: phase.Operation.InstallationID, Phase: phase.Operation.Phase,
		Status: "failed", ReasonCode: "contract_invalid", ObservedAt: observedAt,
	}
	if err := connectorcontrol.ValidateControlResultV2ForOperation(phase.Operation, value); err != nil {
		t.Fatalf("fail %s: %v", phase.Operation.Phase, err)
	}
	digest, _, err := connectorcontrol.CanonicalControlResultV2(value)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := observedAt
	phase.Operation.Status = "failed"
	phase.Operation.AttemptCount = 1
	phase.Operation.CompletedAt = &completedAt
	phase.Operation.UpdatedAt = observedAt
	phase.Operation.ResultSHA256 = digest
	phase.Operation.ReasonCode = value.ReasonCode
	phase.Result = &value
}

func setActivationPhasePending(phase *connectorcontrol.ActivationPhase) {
	phase.Operation.Status = "pending"
	phase.Operation.AttemptCount = 0
	phase.Operation.CompletedAt = nil
	phase.Operation.ResultSHA256 = ""
	phase.Operation.ReasonCode = ""
	phase.Operation.UpdatedAt = phase.Operation.CreatedAt
	phase.Result = nil
}

func canonicalRegistrationSHA(t *testing.T, value connectorcontrol.PublicReceiverRegistrationV1) string {
	t.Helper()
	digest, _, err := connectorcontrol.CanonicalSHA256(value)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func activationSHA256(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}
