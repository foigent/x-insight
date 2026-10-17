package connectorcontrol

import (
	"context"
	"time"
)

// ManagedBoundary is root-local readback, never a public adoption assertion.
type ManagedBoundary struct {
	Installation        Installation
	UniRouterOrigin     string
	SourceBindingSHA256 string
	Bootstrap           *ReceiverAdoptionBootstrap
	Descriptor          *SourceDescriptorV1
	Phases              []ActivationPhase
	LocalReceipt        *XIAdoptionReceipt
	Checkpoint          *ExistingCheckpoint
	FeedRead            *FeedReadReceipt
}

func (b ManagedBoundary) Phase(name, purpose string) *ActivationPhase {
	for i := range b.Phases {
		p := &b.Phases[i]
		if p.Operation.Phase != name {
			continue
		}
		if purpose != "" && (p.Operation.RequestV3 == nil || p.Operation.RequestV3.Registration == nil || p.Operation.RequestV3.Registration.Purpose != purpose) {
			continue
		}
		return p
	}
	return nil
}

type ManagedStore interface {
	InspectManagedConnector(context.Context, string) (ManagedBoundary, error)
	EnqueueManagedControlOperation(context.Context, ControlOperationRequestV3, time.Time) (ControlOperation, error)
	CompleteManagedControlOperation(context.Context, string, ControlResultV3, time.Time) (Installation, error)
	ListManagedConnectorIDs(context.Context, int) ([]string, error)
	SaveManagedSourceDescriptor(context.Context, SourceDescriptorV1, time.Time) error
}
type ManagedAcknowledger interface {
	AcknowledgeManaged(context.Context, string, ControlResultV3) (Installation, error)
}

const LocalManagedACKSchema = "xinsight.local-managed-connector-ack.v1"

type ManagedACKRequest struct {
	SchemaVersion     string          `json:"schema_version"`
	ServiceIdentityID string          `json:"service_identity_id"`
	Result            ControlResultV3 `json:"result"`
}
type ManagedACKResponse struct {
	SchemaVersion string        `json:"schema_version"`
	Installation  *Installation `json:"installation,omitempty"`
	// These fields are a root-local receipt only. The public Installation
	// deliberately excludes them; restoring them allows its full validation.
	ControlServiceIdentityID string `json:"control_service_identity_id,omitempty"`
	SourceInstance           string `json:"source_instance,omitempty"`
	Error                    string `json:"error,omitempty"`
}
