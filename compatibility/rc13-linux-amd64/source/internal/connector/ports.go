package connector

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/foigent/x-insight/internal/investigation"
)

type EnvelopeValidator interface {
	ValidateEnvelope([]byte, EnvelopeBinding) (ValidatedEnvelope, *ConnectorError)
}

type ProjectionContext struct {
	ObservedAt time.Time
	IngestedAt time.Time
}

type ProfileKey struct {
	ProfileID     string
	EventType     string
	SchemaRef     string
	SchemaVersion string
}

func (key ProfileKey) Validate() error {
	if key.ProfileID == "" ||
		!eventTypePattern.MatchString(key.EventType) ||
		!schemaRefPattern.MatchString(key.SchemaRef) ||
		!schemaVersionPattern.MatchString(key.SchemaVersion) {
		return errors.New("normalizer profile key is invalid")
	}
	return nil
}

type NormalizedItem struct {
	ProfileID  string
	Envelope   ValidatedEnvelope
	Events     []map[string]any
	Provenance investigation.ImportProvenance
}

type ItemNormalizer interface {
	ProfileKey() ProfileKey
	Normalize(
		context.Context,
		ValidatedEnvelope,
		ProjectionContext,
	) (NormalizedItem, *ConnectorError)
}

type ProfileRegistry struct {
	connector  PortableConnectorBinding
	projection ProjectionBinding
	mu         sync.RWMutex
	profiles   map[ProfileKey]ItemNormalizer
}

func NewProfileRegistry(binding EnvelopeBinding) (*ProfileRegistry, error) {
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	return &ProfileRegistry{
		connector:  binding.ConnectorRegistration(),
		projection: binding.ProjectionRegistration(),
		profiles:   make(map[ProfileKey]ItemNormalizer),
	}, nil
}

func (registry *ProfileRegistry) Register(profile ItemNormalizer) error {
	if registry == nil || profile == nil {
		return errors.New("normalizer profile registry and profile are required")
	}
	key := profile.ProfileKey()
	if err := key.Validate(); err != nil {
		return err
	}
	if key.ProfileID != registry.projection.ProfileID ||
		key.EventType != registry.connector.EventType ||
		key.SchemaRef != registry.connector.SchemaRef ||
		key.SchemaVersion != registry.connector.SchemaVersion {
		return errors.New("normalizer profile does not match the projection binding")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.profiles[key]; exists {
		return errors.New("normalizer profile is already registered")
	}
	registry.profiles[key] = profile
	return nil
}

func (registry *ProfileRegistry) Resolve(
	envelope ValidatedEnvelope,
	profileID string,
) (ItemNormalizer, *ConnectorError) {
	if registry == nil {
		return nil, registryError("normalizer profile registry is unavailable")
	}
	key := ProfileKey{
		ProfileID:     profileID,
		EventType:     envelope.EventType,
		SchemaRef:     envelope.SchemaRef,
		SchemaVersion: envelope.SchemaVersion,
	}
	registry.mu.RLock()
	profile, exists := registry.profiles[key]
	registry.mu.RUnlock()
	if !exists {
		return nil, registryError("no exact normalizer profile is registered")
	}
	return profile, nil
}

type ItemConvergence string

const (
	ConvergenceInserted      ItemConvergence = "inserted"
	ConvergenceDuplicateNoop ItemConvergence = "duplicate_noop"
)

type EvidenceCommitReport struct {
	EventsTotal         int
	Inserted            int
	DuplicateNoop       int
	IdempotencyConflict int
	Rejected            int
}

func ClassifyItemConvergence(
	expectedEvents int,
	report EvidenceCommitReport,
) (ItemConvergence, *ConnectorError) {
	if expectedEvents <= 0 ||
		report.EventsTotal != expectedEvents ||
		report.Inserted < 0 ||
		report.DuplicateNoop < 0 ||
		report.IdempotencyConflict < 0 ||
		report.Rejected < 0 {
		return "", commitError(
			ErrorPersistence,
			"foigent.commit.report_invalid",
			"Evidence commit report is invalid",
		)
	}
	if report.IdempotencyConflict != 0 || report.Rejected != 0 {
		return "", commitError(
			ErrorConflict,
			"foigent.commit.evidence_conflict",
			"Evidence item did not converge",
		)
	}
	switch {
	case report.Inserted == expectedEvents && report.DuplicateNoop == 0:
		return ConvergenceInserted, nil
	case report.DuplicateNoop == expectedEvents && report.Inserted == 0:
		return ConvergenceDuplicateNoop, nil
	default:
		return "", commitError(
			ErrorConflict,
			"foigent.commit.partial_convergence",
			"Evidence item partially converged",
		)
	}
}

type EvidenceCommitPort interface {
	CommitItem(
		context.Context,
		NormalizedItem,
	) (ItemConvergence, *ConnectorError)
}

type BatchConvergence struct {
	ProducerInstance string
	NextCursor       string
	HighWatermark    string
	HasMore          bool
	Received         int
	Inserted         int
	DuplicateNoop    int
	Conflict         int
	Rejected         int
}

func (outcome BatchConvergence) Validate() error {
	if !validOpaqueText(outcome.ProducerInstance, 1, 128) ||
		!validateBoundedOperationalText(outcome.NextCursor, 512) ||
		!validateBoundedOperationalText(outcome.HighWatermark, 512) ||
		outcome.Received < 0 ||
		outcome.Inserted < 0 ||
		outcome.DuplicateNoop < 0 ||
		outcome.Conflict < 0 ||
		outcome.Rejected < 0 {
		return errors.New("batch convergence outcome is invalid")
	}
	if outcome.Conflict != 0 || outcome.Rejected != 0 ||
		outcome.Inserted+outcome.DuplicateNoop != outcome.Received {
		return errors.New("batch did not fully converge")
	}
	return nil
}

type CheckpointCommitPort interface {
	CommitCheckpoint(
		context.Context,
		Checkpoint,
		BatchConvergence,
	) (Checkpoint, *ConnectorError)
}

func ProjectConvergedCheckpoint(
	binding ManifestBinding,
	expected Checkpoint,
	outcome BatchConvergence,
	updatedAt time.Time,
) (Checkpoint, *ConnectorError) {
	if err := binding.Validate(); err != nil {
		return expected, checkpointError(
			ErrorConfiguration,
			"foigent.checkpoint.binding_invalid",
			"Checkpoint projection binding is invalid",
		)
	}
	if err := expected.Validate(); err != nil ||
		expected.ConnectorID != binding.Connector.ConnectorID ||
		expected.ConnectorVersion != binding.Connector.ConnectorVersion {
		return expected, checkpointError(
			ErrorConflict,
			"foigent.checkpoint.expected_invalid",
			"Expected Checkpoint is invalid",
		)
	}
	if err := outcome.Validate(); err != nil {
		return expected, checkpointError(
			ErrorConflict,
			"foigent.checkpoint.batch_not_converged",
			"Checkpoint cannot advance before every item converges",
		)
	}
	if outcome.ProducerInstance != expected.ProducerInstance {
		return expected, checkpointError(
			ErrorConflict,
			"foigent.checkpoint.instance_mismatch",
			"Checkpoint producer instance does not match",
		)
	}
	if outcome.Received == 0 && outcome.NextCursor != expected.Cursor {
		return expected, checkpointError(
			ErrorCursor,
			"foigent.checkpoint.empty_batch_advance",
			"An empty Batch cannot advance the Checkpoint",
		)
	}
	if (outcome.HasMore || outcome.Received > 0) &&
		outcome.NextCursor == expected.Cursor {
		return expected, checkpointError(
			ErrorCursor,
			"foigent.checkpoint.no_progress",
			"Checkpoint did not make required cursor progress",
		)
	}
	next, err := ProjectCheckpoint(binding, LocalCheckpointState{
		ProducerInstance: outcome.ProducerInstance,
		Cursor:           outcome.NextCursor,
		HighWatermark:    outcome.HighWatermark,
		Phase:            "steady",
		UpdatedAt:        updatedAt,
	})
	if err != nil {
		return expected, checkpointError(
			ErrorInternal,
			"foigent.checkpoint.projection_failed",
			"Checkpoint projection failed",
		)
	}
	return next, nil
}

func registryError(message string) *ConnectorError {
	return NewConnectorError(
		ErrorContract,
		"foigent.normalizer.profile_not_registered",
		PhaseNormalize,
		"normalize_item",
		false,
		message,
		RecoveryOperatorRepair,
	)
}

func commitError(
	category ErrorCategory,
	code string,
	message string,
) *ConnectorError {
	return NewConnectorError(
		category,
		code,
		PhaseCommit,
		"commit_item",
		false,
		message,
		RecoveryOperatorRepair,
	)
}

func checkpointError(
	category ErrorCategory,
	code string,
	message string,
) *ConnectorError {
	return NewConnectorError(
		category,
		code,
		PhaseCheckpoint,
		"commit_checkpoint",
		false,
		message,
		RecoveryOperatorRepair,
	)
}

func ValidateNormalizedItem(item NormalizedItem) error {
	if item.ProfileID == "" ||
		item.Envelope.EventType == "" ||
		item.Envelope.EnvelopeHash == "" ||
		len(item.Events) == 0 {
		return errors.New("normalized item identity is invalid")
	}
	provenance := item.Provenance
	if provenance.SourceSystemID == "" ||
		provenance.AdapterManifestID == "" ||
		provenance.SourcePayloadSHA256 == "" ||
		provenance.SourceTerminalAt == nil {
		return errors.New("normalized item provenance is incomplete")
	}
	if provenance.SourcePayloadSHA256 != item.Envelope.SourceHash {
		return errors.New("normalized item source integrity does not match provenance")
	}
	for _, event := range item.Events {
		sourceSystemID, sourceOK := event["source_system_id"].(string)
		adapterManifestID, adapterOK := event["adapter_manifest_id"].(string)
		if !sourceOK || !adapterOK ||
			sourceSystemID != provenance.SourceSystemID ||
			adapterManifestID != provenance.AdapterManifestID {
			return errors.New("normalized event registry identity does not match provenance")
		}
	}
	return nil
}
