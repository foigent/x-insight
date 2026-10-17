package connectorcontrol

import (
	"context"
	"io"
	"time"
)

// ActivationPhase is a product-local durable phase readback. Result is nil
// while the operation is pending or running and is required once terminal.
type ActivationPhase struct {
	Operation ControlOperation
	Result    *ControlOperationResult
}

// ActivationBoundary is the root-controller authority used to reconcile one
// installation. UniRouterOrigin comes only from the exact consumed invitation;
// it is never accepted as an operator or phase payload input.
type ActivationBoundary struct {
	Installation        Installation
	UniRouterOrigin     string
	PreparePausedSource *ActivationPhase
	RegisterReceiver    *ActivationPhase
	ResumeSource        *ActivationPhase
}

// ActivationStore is the narrow persistence boundary used by the root-local
// connector composer. The existing phase enqueue path remains the sole writer
// of phase operations.
type ActivationStore interface {
	InspectActivationBoundary(context.Context, string) (ActivationBoundary, error)
	ClaimInstallationActivation(context.Context, string, int64, string, string, time.Time) (ActivationBoundary, error)
	EnqueueControlPhase(context.Context, EnqueueControlPhaseInput, time.Time, io.Reader) (ControlOperation, error)
	FinalizeInstallationConnected(context.Context, string, int64, string, string, time.Time) (Installation, error)
}

// IsSHA256Reference reports whether value is a canonical sha256: reference.
func IsSHA256Reference(value string) bool {
	return hashPattern.MatchString(value)
}

// ValidateActivationBoundary validates the durable installation, consumed
// origin and exact prefix of the frozen three-phase chain.
func ValidateActivationBoundary(value ActivationBoundary) error {
	if ValidateInstallation(value.Installation) != nil ||
		validateCanonicalHTTPSOrigin(value.UniRouterOrigin) != nil ||
		value.Installation.ProjectID == "" || value.PreparePausedSource == nil {
		return ErrInvalid
	}
	installationID := value.Installation.InstallationID
	projectID := value.Installation.ProjectID
	eventScope := value.Installation.FinalEventScope
	if validateActivationPhase(value.PreparePausedSource, installationID, projectID, eventScope, ControlPhasePreparePausedSource) != nil {
		return ErrInvalid
	}
	if value.RegisterReceiver == nil {
		if value.ResumeSource != nil {
			return ErrInvalid
		}
		return nil
	}
	if validateActivationPhase(value.RegisterReceiver, installationID, projectID, eventScope, ControlPhaseRegisterReceiver) != nil ||
		validateActivationPredecessor(value.RegisterReceiver, value.PreparePausedSource) != nil {
		return ErrInvalid
	}
	if value.ResumeSource == nil {
		if value.Installation.Status == "connected" {
			return ErrInvalid
		}
		return nil
	}
	if validateActivationPhase(value.ResumeSource, installationID, projectID, eventScope, ControlPhaseResumeSource) != nil ||
		validateActivationPredecessor(value.ResumeSource, value.RegisterReceiver) != nil {
		return ErrInvalid
	}
	if value.Installation.Status == "connected" &&
		(value.Installation.DesiredState != "active" || value.ResumeSource.Operation.Status != "completed" ||
			value.ResumeSource.Result == nil || value.ResumeSource.Result.Status != "completed") {
		return ErrInvalid
	}
	return nil
}

func validateActivationPhase(
	phase *ActivationPhase,
	installationID string,
	projectID string,
	eventScope []string,
	expectedPhase string,
) error {
	if phase == nil || ValidateControlOperationV2(phase.Operation) != nil ||
		phase.Operation.InstallationID != installationID || phase.Operation.ProjectID != projectID ||
		!equalEventScope(phase.Operation.EventScope, eventScope) || phase.Operation.Phase != expectedPhase {
		return ErrInvalid
	}
	terminal := phase.Operation.Status == "completed" || phase.Operation.Status == "failed"
	if !terminal {
		if phase.Result != nil {
			return ErrInvalid
		}
		return nil
	}
	if phase.Result == nil || phase.Result.Status != phase.Operation.Status ||
		ValidateControlResultV2ForOperation(phase.Operation, *phase.Result) != nil {
		return ErrInvalid
	}
	digest, _, err := CanonicalControlResultV2(*phase.Result)
	if err != nil || digest != phase.Operation.ResultSHA256 {
		return ErrInvalid
	}
	return nil
}

func validateActivationPredecessor(current *ActivationPhase, predecessor *ActivationPhase) error {
	if current == nil || predecessor == nil || predecessor.Operation.Status != "completed" || predecessor.Result == nil {
		return ErrInvalid
	}
	request, _, _, err := CanonicalControlOperationRequestV2(current.Operation)
	if err != nil || ValidateControlOperationV2Predecessor(request, *predecessor.Result) != nil {
		return ErrInvalid
	}
	return nil
}
