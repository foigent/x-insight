package openinference

import (
	"context"

	"github.com/foigent/x-insight/internal/connector"
)

type Projector struct {
	binding   connector.SourceNeutralManifestBinding
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
		binding: binding, validator: connector.StrictEnvelopeValidator{}, registry: registry,
	}, nil
}

func (projector *Projector) Binding() connector.SourceNeutralManifestBinding {
	if projector == nil {
		return connector.SourceNeutralManifestBinding{}
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
		return connector.NormalizedItem{}, adapterError(
			connector.ErrorInternal,
			"foigent.openinference.projector_uninitialized",
			connector.PhaseNormalize,
			"normalize_item",
			"OpenInference projector is unavailable",
		)
	}
	envelopeBytes, _, connectorErr := BuildEnvelope(source, mapping, projector.binding)
	if connectorErr != nil {
		return connector.NormalizedItem{}, connectorErr
	}
	envelope, connectorErr := projector.validator.ValidateEnvelope(envelopeBytes, projector.binding)
	if connectorErr != nil {
		return connector.NormalizedItem{}, connectorErr
	}
	profile, connectorErr := projector.registry.Resolve(envelope, projector.binding.Projection.ProfileID)
	if connectorErr != nil {
		return connector.NormalizedItem{}, connectorErr
	}
	return profile.Normalize(ctx, envelope, projectionContext)
}
