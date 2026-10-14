package unirouter

import (
	"context"

	"github.com/foigent/x-insight/internal/connector"
)

const (
	CompletedExecutionReferenceProfileID       = "xinsight.foigent-envelope-to-m0.unirouter-completed-execution.v1"
	CompletedExecutionReferenceProtocolVersion = "1.0.0"
	CompletedExecutionReferenceReleaseDigest   = "sha256:aaeb5b37b94a1fd603048d77b2180fa88be1ddbd6eeea6a1369edf0cea9c5e2b"
)

// CompletedExecutionReferenceProjector is the product-local Phase 2C runtime
// adapter for the single registered completed-execution Reference Connector.
// It builds and validates a shared Envelope in memory and never owns delivery,
// persistence, or Checkpoint state.
type CompletedExecutionReferenceProjector struct {
	binding   connector.ManifestBinding
	validator connector.EnvelopeValidator
	registry  *connector.ProfileRegistry
}

func NewCompletedExecutionReferenceProjector() (*CompletedExecutionReferenceProjector, error) {
	binding := completedExecutionReferenceBindingV1()
	if err := validateCompletedExecutionReferenceBinding(binding); err != nil {
		return nil, err
	}
	profile, err := NewCompletedExecutionProfile(binding)
	if err != nil {
		return nil, err
	}
	registry, err := connector.NewProfileRegistry(binding)
	if err != nil {
		return nil, err
	}
	if err := registry.Register(profile); err != nil {
		return nil, err
	}
	return &CompletedExecutionReferenceProjector{
		binding:   binding,
		validator: connector.StrictEnvelopeValidator{},
		registry:  registry,
	}, nil
}

func (projector *CompletedExecutionReferenceProjector) Binding() connector.ManifestBinding {
	if projector == nil {
		return connector.ManifestBinding{}
	}
	return projector.binding
}

func (projector *CompletedExecutionReferenceProjector) Project(
	ctx context.Context,
	sourceBytes []byte,
	options CompletedExecutionMapOptions,
) (connector.NormalizedItem, *connector.ConnectorError) {
	if projector == nil || projector.validator == nil || projector.registry == nil {
		return connector.NormalizedItem{}, completedReferenceProjectorError(
			"foigent.reference.uninitialized",
			"completed-execution Reference projector is unavailable",
		)
	}
	if err := ctx.Err(); err != nil {
		return connector.NormalizedItem{}, completedReferenceProjectorError(
			"foigent.reference.context_ended",
			"completed-execution Reference projection was interrupted",
		)
	}
	envelope, err := BuildFoigentReferenceEnvelopeV1(
		sourceBytes,
		options.SourceInstance,
	)
	if err != nil {
		return connector.NormalizedItem{}, completedReferenceProjectorError(
			"foigent.reference.source_projection_invalid",
			"completed-execution Source projection is invalid",
		)
	}
	validated, connectorErr := projector.validator.ValidateEnvelope(
		envelope.EnvelopeBytes,
		projector.binding,
	)
	if connectorErr != nil {
		return connector.NormalizedItem{}, connectorErr
	}
	profile, connectorErr := projector.registry.Resolve(
		validated,
		projector.binding.Projection.ProfileID,
	)
	if connectorErr != nil {
		return connector.NormalizedItem{}, connectorErr
	}
	return profile.Normalize(ctx, validated, connector.ProjectionContext{
		ObservedAt: options.ObservedAt,
		IngestedAt: options.IngestedAt,
	})
}

func completedReferenceProjectorError(code string, message string) *connector.ConnectorError {
	return connector.NewConnectorError(
		connector.ErrorContract,
		code,
		connector.PhaseNormalize,
		"normalize_item",
		false,
		message,
		connector.RecoveryOperatorRepair,
	)
}

func validateCompletedExecutionReferenceBinding(binding connector.ManifestBinding) error {
	return connector.ValidateExactRegistration(
		binding,
		completedExecutionReferenceBindingV1(),
	)
}

// completedExecutionReferenceBindingV1 is the compiled product-local registry
// pin. Its equality with the sole editable JSON binding is enforced in tests.
func completedExecutionReferenceBindingV1() connector.ManifestBinding {
	return connector.ManifestBinding{
		BindingVersion: connector.ProjectionBindingVersionV1,
		Protocol: connector.ProtocolBinding{
			Version:       CompletedExecutionReferenceProtocolVersion,
			Tag:           "v1.0.0",
			Commit:        "c09bc93cc7750e2333752655d6b4e69802e6769d",
			ReleaseDigest: CompletedExecutionReferenceReleaseDigest,
		},
		Schemas: connector.SchemaBindings{
			EventEnvelope: connector.ArtifactPin{
				Path:   "schemas/event-envelope.v1.schema.json",
				SHA256: "sha256:6942e85d9b5d8b08ff9828b970c317596394184ef29908cbf2b8fc792187cf26",
			},
			ModelRequestCompleted: connector.ArtifactPin{
				Path:   "schemas/model-request-completed.v1.schema.json",
				SHA256: "sha256:ea0ea7354f3d9f435bb86a34eb28284e4297e5a2d1a3d22a23058c7dcc514dc6",
			},
			ConnectorManifest: connector.ArtifactPin{
				Path:   "schemas/connector-manifest.v1.schema.json",
				SHA256: "sha256:a6f43b37477002ab836a4d9ea7c4e462675cd4313f0637c0733cd98675fc0adf",
			},
			ConnectorLifecycle: connector.ArtifactPin{
				Path:   "schemas/connector-lifecycle.v1.schema.json",
				SHA256: "sha256:bd5c730587041ddc7ad6f66bca4deca35edf5f9b5b92b761ac53701f37e18d5e",
			},
			EventBatch: connector.ArtifactPin{
				Path:   "schemas/event-batch.v1.schema.json",
				SHA256: "sha256:8e2133922bcb77cf032309290593665df1bf56cfa47e433dd573d0e42f36eef9",
			},
			Checkpoint: connector.ArtifactPin{
				Path:   "schemas/connector-checkpoint.v1.schema.json",
				SHA256: "sha256:8be2fd290d6db8d288a9b10ac6d9aa0b55a66ff21ea0fb55ac0af2d1b66bf800",
			},
			OperationalReceipt: connector.ArtifactPin{
				Path:   "schemas/operational-receipt.v1.schema.json",
				SHA256: "sha256:3c3b99e009e208456039d3bfe26d84af7e3f1af23589e7fa62a1e1a61e0fb3c6",
			},
			ConnectorError: connector.ArtifactPin{
				Path:   "schemas/connector-error.v1.schema.json",
				SHA256: "sha256:f449ba6314f1f730be5a0b188dc6d2cc2704e8f5d84a4eebecac576221e4ca56",
			},
		},
		ReferenceManifest: connector.ArtifactPin{
			Path:   "manifests/foigent.unirouter.completed-execution-1.0.0.json",
			SHA256: "sha256:a3bc97bd5d5c7aab6452de9518be0b00f62776877deb431a91c175da5287de79",
		},
		Compatibility: connector.CompatibilityBindings{
			ProjectionContractSHA256:  "sha256:c812a9c70d55ad0413650218eab6f968fd44974e1e7d77a147d58f62e18e8b03",
			LifecycleMappingSHA256:    "sha256:f1980ad31b1b3be978b39dee854f9e9e1accf99fa266615fe5a43f10dc115503",
			IdentityMappingSHA256:     "sha256:a14c7ed4fe5475cf4ad50da32731824eb8b85fb249dcd5b4bc5b7556d6b33305",
			ReferenceVectorSHA256:     "sha256:5cd40aa6c0256a70ad7317c05e078759685e0469be77a3e960a976adc3158e8e",
			CanonicalVectorSHA256:     "sha256:50fc8e8be08c37381611d6423e29780894741fb108fc4569df7c895452e5d4db",
			ConformanceContractSHA256: "sha256:f3ae834265170ff22088943283fd23dcb562bc2308571ccc722414c2c896dc23",
		},
		Connector: connector.PortableConnectorBinding{
			ConnectorID:        foigentConnectorID,
			ConnectorVersion:   foigentConnectorVersion,
			SourceProduct:      "unirouter",
			EventType:          foigentEventType,
			SchemaRef:          foigentEventSchemaRef,
			SchemaVersion:      "1",
			SourceSchema:       CompletedExecutionSchemaV1,
			SourceSchemaSHA256: "sha256:" + AcceptedCompletedExecutionSchemaSHA256,
		},
		Projection: connector.ProjectionBinding{
			ProfileID:         CompletedExecutionReferenceProfileID,
			SourceSystemID:    DefaultSourceSystemID,
			AdapterManifestID: DefaultCompletedExecutionAdapterManifestID,
			CursorKey:         "unirouter.completed-execution.v1",
		},
	}
}
