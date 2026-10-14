package litellm

import (
	"context"

	"github.com/foigent/x-insight/internal/connector"
)

// Projector owns only the LiteLLM SourceRecord -> shared Envelope -> M0
// projection. Delivery, credentials, Checkpoint and persistence remain ports.
type Projector struct {
	binding   connector.ManifestBinding
	validator connector.EnvelopeValidator
	registry  *connector.ProfileRegistry
}

func NewProjector() (*Projector, error) {
	binding := Binding()
	if err := validateBinding(binding); err != nil {
		return nil, err
	}
	profile, err := NewProfile(binding)
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
	return &Projector{
		binding:   binding,
		validator: connector.StrictEnvelopeValidator{},
		registry:  registry,
	}, nil
}

func (projector *Projector) Binding() connector.ManifestBinding {
	if projector == nil {
		return connector.ManifestBinding{}
	}
	return projector.binding
}

func (projector *Projector) Project(
	ctx context.Context,
	source SanitizedRecord,
	mapping MappingContext,
	projectionContext connector.ProjectionContext,
) (connector.NormalizedItem, *connector.ConnectorError) {
	if projector == nil || projector.validator == nil || projector.registry == nil {
		return connector.NormalizedItem{}, profileError(
			"foigent.litellm.projector_uninitialized",
			"LiteLLM projector is unavailable",
		)
	}
	envelopeBytes, _, connectorErr := BuildEnvelope(
		source,
		mapping,
		projector.binding,
	)
	if connectorErr != nil {
		return connector.NormalizedItem{}, connectorErr
	}
	validated, connectorErr := projector.validator.ValidateEnvelope(
		envelopeBytes,
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
	return profile.Normalize(ctx, validated, projectionContext)
}
