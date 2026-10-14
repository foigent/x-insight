package unirouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/investigation"
)

// CompletedExecutionProfile is the only Phase 2B normalizer profile. It wraps
// the existing completed-execution projection without registering a runtime
// caller or changing the live worker path.
type CompletedExecutionProfile struct {
	binding connector.ManifestBinding
}

func NewCompletedExecutionProfile(
	binding connector.ManifestBinding,
) (*CompletedExecutionProfile, error) {
	if err := validateCompletedExecutionReferenceBinding(binding); err != nil {
		return nil, err
	}
	if binding.Connector.ConnectorID != foigentConnectorID ||
		binding.Connector.ConnectorVersion != foigentConnectorVersion ||
		binding.Connector.SourceProduct != "unirouter" ||
		binding.Connector.EventType != foigentEventType ||
		binding.Connector.SchemaRef != foigentEventSchemaRef ||
		binding.Connector.SchemaVersion != "1" ||
		binding.Connector.SourceSchema != CompletedExecutionSchemaV1 ||
		binding.Connector.SourceSchemaSHA256 !=
			"sha256:"+AcceptedCompletedExecutionSchemaSHA256 ||
		binding.Projection.ProfileID !=
			"xinsight.foigent-envelope-to-m0.unirouter-completed-execution.v1" ||
		binding.Projection.SourceSystemID != DefaultSourceSystemID ||
		binding.Projection.AdapterManifestID !=
			DefaultCompletedExecutionAdapterManifestID ||
		binding.Projection.CursorKey !=
			"unirouter.completed-execution.v1" {
		return nil, errors.New("completed-execution profile binding is invalid")
	}
	return &CompletedExecutionProfile{binding: binding}, nil
}

func (profile *CompletedExecutionProfile) ProfileKey() connector.ProfileKey {
	if profile == nil {
		return connector.ProfileKey{}
	}
	return connector.ProfileKey{
		ProfileID:     profile.binding.Projection.ProfileID,
		EventType:     profile.binding.Connector.EventType,
		SchemaRef:     profile.binding.Connector.SchemaRef,
		SchemaVersion: profile.binding.Connector.SchemaVersion,
	}
}

func (profile *CompletedExecutionProfile) Normalize(
	ctx context.Context,
	validated connector.ValidatedEnvelope,
	projectionContext connector.ProjectionContext,
) (connector.NormalizedItem, *connector.ConnectorError) {
	if profile == nil {
		return connector.NormalizedItem{}, completedProfileError(
			connector.ErrorInternal,
			"foigent.normalizer.uninitialized",
			false,
			"completed-execution normalizer is unavailable",
		)
	}
	if err := ctx.Err(); err != nil {
		return connector.NormalizedItem{}, completedProfileError(
			connector.ErrorInternal,
			"foigent.normalizer.context_ended",
			true,
			"completed-execution normalization was interrupted",
		)
	}
	key := profile.ProfileKey()
	if validated.EventType != key.EventType ||
		validated.SchemaRef != key.SchemaRef ||
		validated.SchemaVersion != key.SchemaVersion ||
		validated.ConnectorID != profile.binding.Connector.ConnectorID ||
		validated.ConnectorVersion != profile.binding.Connector.ConnectorVersion ||
		validated.ProducerProduct != profile.binding.Connector.SourceProduct {
		return connector.NormalizedItem{}, completedProfileError(
			connector.ErrorContract,
			"foigent.normalizer.binding_mismatch",
			false,
			"Event Envelope does not match the completed-execution profile",
		)
	}

	decoder := json.NewDecoder(bytes.NewReader(validated.CanonicalBytes()))
	decoder.DisallowUnknownFields()
	var envelope foigentEnvelopeV1
	if err := decoder.Decode(&envelope); err != nil {
		return connector.NormalizedItem{}, completedProfileError(
			connector.ErrorContract,
			"foigent.normalizer.payload_invalid",
			false,
			"completed-execution Event payload is invalid",
		)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return connector.NormalizedItem{}, completedProfileError(
			connector.ErrorContract,
			"foigent.normalizer.payload_invalid",
			false,
			"completed-execution Event payload contains trailing content",
		)
	}
	if err := validateFoigentReferenceEnvelopeV1(envelope); err != nil {
		return connector.NormalizedItem{}, completedProfileError(
			connector.ErrorContract,
			"foigent.normalizer.reference_contract_invalid",
			false,
			"completed-execution Event semantics are invalid",
		)
	}
	if envelope.Integrity.EnvelopeHash != validated.EnvelopeHash ||
		envelope.Integrity.SourceHash != validated.SourceHash ||
		envelope.Identity.IdempotencyKey != validated.IdempotencyKey {
		return connector.NormalizedItem{}, completedProfileError(
			connector.ErrorConflict,
			"foigent.normalizer.validated_identity_mismatch",
			false,
			"validated Event identity changed before normalization",
		)
	}

	snapshot, err := completedExecutionFromFoigentEnvelope(envelope)
	if err != nil {
		return connector.NormalizedItem{}, completedProfileError(
			connector.ErrorContract,
			"foigent.normalizer.projection_invalid",
			false,
			"completed-execution Event cannot be projected",
		)
	}
	mapped, err := mapCompletedExecutionSnapshot(
		snapshot,
		validated.SourceHash,
		CompletedExecutionMapOptions{
			SourceInstance:    validated.ProducerInstance,
			SourceSystemID:    profile.binding.Projection.SourceSystemID,
			AdapterManifestID: profile.binding.Projection.AdapterManifestID,
			ObservedAt:        projectionContext.ObservedAt,
			IngestedAt:        projectionContext.IngestedAt,
		},
	)
	if err != nil {
		return connector.NormalizedItem{}, completedProfileError(
			connector.ErrorInternal,
			"foigent.normalizer.m0_projection_failed",
			false,
			"completed-execution M0 projection failed",
		)
	}

	terminalAt := mapped.TerminalAvailableAt.UTC()
	item := connector.NormalizedItem{
		ProfileID: profile.binding.Projection.ProfileID,
		Envelope:  validated,
		Events:    mapped.Events,
		Provenance: investigation.ImportProvenance{
			SourceSystemID:      mapped.SourceSystemID,
			AdapterManifestID:   mapped.AdapterManifestID,
			IntegrationMode:     investigation.IntegrationPull,
			ProofClass:          investigation.ProofOperatorApproved,
			AssuranceMode:       investigation.AssuranceContractedPull,
			SourcePayloadSHA256: mapped.SnapshotSHA256,
			SourceTerminalAt:    &terminalAt,
			SourceRevision:      envelope.Identity.SourceRevision,
			ImportedRevision:    CompletedExecutionRevisionV1,
			TimelineComplete:    true,
		},
	}
	if err := connector.ValidateNormalizedItem(item); err != nil {
		return connector.NormalizedItem{}, completedProfileError(
			connector.ErrorInternal,
			"foigent.normalizer.output_invalid",
			false,
			"completed-execution normalized output is invalid",
		)
	}
	return item, nil
}

func completedProfileError(
	category connector.ErrorCategory,
	code string,
	retryable bool,
	message string,
) *connector.ConnectorError {
	recovery := connector.RecoveryOperatorRepair
	if retryable {
		recovery = connector.RecoveryAutomaticRetry
	}
	return connector.NewConnectorError(
		category,
		code,
		connector.PhaseNormalize,
		"normalize_item",
		retryable,
		message,
		recovery,
	)
}
