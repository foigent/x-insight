package connectorcontrol

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestActivationBoundaryRequiresExactDurablePhaseChain(t *testing.T) {
	boundary := activationFixtureBoundary(t)
	if err := ValidateActivationBoundary(boundary); err != nil {
		t.Fatalf("valid activation boundary: %v", err)
	}

	missingPredecessor := boundary
	missingPredecessor.RegisterReceiver = nil
	if err := ValidateActivationBoundary(missingPredecessor); err == nil {
		t.Fatal("resume phase accepted without register predecessor")
	}

	untrustedOrigin := boundary
	untrustedOrigin.UniRouterOrigin = "https://unirouter.example/path"
	if err := ValidateActivationBoundary(untrustedOrigin); err == nil {
		t.Fatal("activation boundary accepted a non-origin UniRouter URL")
	}

	tamperedResult := boundary
	tamperedPhase := *boundary.ResumeSource
	tamperedOperation := tamperedPhase.Operation
	tamperedOperation.ResultSHA256 = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tamperedPhase.Operation = tamperedOperation
	tamperedResult.ResumeSource = &tamperedPhase
	if err := ValidateActivationBoundary(tamperedResult); err == nil {
		t.Fatal("activation boundary accepted a result digest mismatch")
	}

	pending := boundary
	pendingPhase := *boundary.ResumeSource
	pendingOperation := pendingPhase.Operation
	pendingOperation.Status = "pending"
	pendingOperation.AttemptCount = 0
	pendingOperation.CompletedAt = nil
	pendingOperation.ResultSHA256 = ""
	pendingOperation.UpdatedAt = pendingOperation.CreatedAt
	pendingPhase.Operation = pendingOperation
	pendingPhase.Result = nil
	pending.ResumeSource = &pendingPhase
	if err := ValidateActivationBoundary(pending); err != nil {
		t.Fatalf("pending phase without result should remain inspectable: %v", err)
	}
	falseConnected := pending
	falseConnected.Installation.Status = "connected"
	falseConnected.Installation.ObservedState = "connected"
	if err := ValidateActivationBoundary(falseConnected); err == nil {
		t.Fatal("connected installation accepted without a completed resume result")
	}
}

func activationFixtureBoundary(t *testing.T) ActivationBoundary {
	t.Helper()
	root := filepath.Join("..", "..", "contracts", "connector-control", "v2", "fixtures", "valid")
	prepare := activationFixturePhase(t, root, "01-prepare-paused-source-operation.json", "02-prepare-paused-source-result.json")
	register := activationFixturePhase(t, root, "03-register-receiver-operation.json", "04-register-receiver-result.json")
	resume := activationFixturePhase(t, root, "05-resume-source-operation.json", "06-resume-source-result.json")
	createdAt := time.Date(2029, 12, 31, 23, 0, 0, 0, time.UTC)
	return ActivationBoundary{
		Installation: Installation{
			SchemaVersion:  InstallationSchema,
			InstallationID: prepare.Operation.InstallationID,
			DeploymentID:   "xid_AAAAAAAAAAAAAAAAAAAAAA",
			ConnectorType:  ConnectorTypeUniRouter,
			RequestedEventScope: []string{
				CompletedEventType,
			},
			FinalEventScope: []string{
				CompletedEventType,
			},
			ProjectID:                         prepare.Operation.ProjectID,
			SourceInstance:                    resume.Result.Result.SourceInstance,
			SourceInstanceSHA256:              RawSHA256(resume.Result.Result.SourceInstance),
			ControlServiceIdentityID:          "xsi_AAAAAAAAAAAAAAAAAAAAAA",
			InstallationAcknowledgementSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Status:                            "pending", DesiredState: "active", ObservedState: "controller_pending",
			CreatedAt: createdAt, UpdatedAt: resume.Operation.UpdatedAt, Version: 4,
		},
		UniRouterOrigin:     "https://unirouter.example",
		PreparePausedSource: prepare,
		RegisterReceiver:    register,
		ResumeSource:        resume,
	}
}

func activationFixturePhase(t *testing.T, root, operationName, resultName string) *ActivationPhase {
	t.Helper()
	operationPayload, err := os.ReadFile(filepath.Join(root, operationName))
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeControlOperationRequestV2(operationPayload)
	if err != nil {
		t.Fatalf("decode operation fixture %s: %v", operationName, err)
	}
	resultPayload, err := os.ReadFile(filepath.Join(root, resultName))
	if err != nil {
		t.Fatal(err)
	}
	result, err := DecodeControlResultV2(resultPayload, time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("decode result fixture %s: %v", resultName, err)
	}
	createdAt := result.ObservedAt.Add(-time.Minute)
	phaseRequest := request.Request
	operation := ControlOperation{
		SchemaVersion: request.SchemaVersion,
		OperationID:   request.OperationID, InstallationID: request.InstallationID,
		Kind: request.Kind, Phase: request.Phase, ProjectID: request.ProjectID,
		EventScope: request.EventScope, DesiredState: request.DesiredState,
		PredecessorOperationID:  request.PredecessorOperationID,
		PredecessorResultSHA256: request.PredecessorResultSHA256,
		Request:                 &phaseRequest,
		Status:                  "completed", AttemptCount: 1,
		AvailableAt: createdAt, CreatedAt: createdAt, UpdatedAt: result.ObservedAt,
		CompletedAt: &result.ObservedAt,
	}
	_, requestSHA256, _, err := CanonicalControlOperationRequestV2(operation)
	if err != nil {
		t.Fatal(err)
	}
	operation.RequestSHA256 = requestSHA256
	resultSHA256, _, err := CanonicalControlResultV2(result)
	if err != nil {
		t.Fatal(err)
	}
	operation.ResultSHA256 = resultSHA256
	return &ActivationPhase{Operation: operation, Result: &result}
}
