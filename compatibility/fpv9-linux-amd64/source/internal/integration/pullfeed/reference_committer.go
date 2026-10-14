package pullfeed

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connector"
)

type ReferenceMappingMode string

const (
	ReferenceMappingLegacy        ReferenceMappingMode = "legacy"
	ReferenceMappingShadow        ReferenceMappingMode = "shadow"
	ReferenceMappingNeutralCanary ReferenceMappingMode = "neutral_canary"
)

type ReferenceItemProjector interface {
	Project(
		context.Context,
		[]byte,
		unirouter.CompletedExecutionMapOptions,
	) (connector.NormalizedItem, *connector.ConnectorError)
}

type ModeAwareEvidenceCommitter struct {
	mode              ReferenceMappingMode
	store             EvidencePersister
	neutral           connector.EvidenceCommitPort
	projector         ReferenceItemProjector
	now               func() time.Time
	logger            LiveWorkerLogFunc
	promotionEligible atomic.Bool
}

func NewModeAwareEvidenceCommitter(
	store EvidencePersister,
	mode ReferenceMappingMode,
	projector ReferenceItemProjector,
	now func() time.Time,
	logger LiveWorkerLogFunc,
) (*ModeAwareEvidenceCommitter, error) {
	if store == nil {
		return nil, fmt.Errorf("evidence persister is required")
	}
	switch mode {
	case ReferenceMappingLegacy:
		if projector != nil {
			return nil, fmt.Errorf("legacy mapping mode must not initialize the neutral projector")
		}
	case ReferenceMappingShadow, ReferenceMappingNeutralCanary:
		if projector == nil {
			return nil, fmt.Errorf("%s mapping mode requires the neutral projector", mode)
		}
	default:
		return nil, fmt.Errorf("reference mapping mode is invalid")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	committer := &ModeAwareEvidenceCommitter{
		mode:      mode,
		store:     store,
		projector: projector,
		now:       now,
		logger:    logger,
	}
	if mode != ReferenceMappingLegacy {
		committer.neutral = &evidenceStoreCommitPort{store: store}
		committer.promotionEligible.Store(true)
	}
	return committer, nil
}

func (committer *ModeAwareEvidenceCommitter) CommitLive(
	ctx context.Context,
	page FeedPage,
	item FeedItem,
) (LiveItemCommitResult, error) {
	if committer == nil || committer.store == nil {
		return LiveItemCommitResult{}, NewLiveWorkerError(
			"evidence_committer_uninitialized",
			false,
			0,
			nil,
		)
	}
	ingestedAt := committer.now().UTC()
	legacy, workerErr := prepareLegacyEvidenceItem(page, item, ingestedAt)
	if workerErr != nil {
		if committer.mode != ReferenceMappingLegacy {
			committer.promotionEligible.Store(false)
			committer.logComparison("legacy_error", "")
		}
		return LiveItemCommitResult{}, workerErr
	}
	if committer.mode == ReferenceMappingLegacy {
		return commitPreparedEvidenceItem(ctx, committer.store, legacy)
	}

	neutral, connectorErr := projectReferenceSafely(
		committer.projector,
		ctx,
		bytes.Clone(item.Snapshot),
		unirouter.CompletedExecutionMapOptions{
			SourceInstance: page.SourceInstance,
			ObservedAt:     page.GeneratedAt,
			IngestedAt:     ingestedAt,
		},
	)
	if connectorErr != nil {
		committer.promotionEligible.Store(false)
		committer.logComparison("neutral_error", "")
		if committer.mode == ReferenceMappingShadow {
			result, err := commitPreparedEvidenceItem(ctx, committer.store, legacy)
			result.CompatibilityCompared = true
			result.CompatibilityError = true
			result.CompatibilityEligible = false
			return result, err
		}
		return LiveItemCommitResult{}, NewLiveWorkerError(
			"reference_projection_invalid",
			false,
			0,
			connectorErr,
		)
	}

	mismatch := compareReferenceProjection(legacy, neutral)
	if mismatch != "" {
		committer.promotionEligible.Store(false)
		committer.logComparison("projection_mismatch", mismatch)
		if committer.mode == ReferenceMappingShadow {
			result, err := commitPreparedEvidenceItem(ctx, committer.store, legacy)
			result.CompatibilityCompared = true
			result.CompatibilityMismatch = true
			result.CompatibilityEligible = false
			return result, err
		}
		return LiveItemCommitResult{}, NewLiveWorkerError(
			"reference_projection_mismatch",
			false,
			0,
			nil,
		)
	}

	committer.logComparison("match", "")
	if committer.mode == ReferenceMappingShadow {
		result, err := commitPreparedEvidenceItem(ctx, committer.store, legacy)
		result.CompatibilityCompared = true
		result.CompatibilityMatched = true
		result.CompatibilityEligible = committer.promotionEligible.Load()
		return result, err
	}
	convergence, connectorErr := committer.neutral.CommitItem(ctx, neutral)
	if connectorErr != nil {
		return LiveItemCommitResult{}, NewLiveWorkerError(
			connectorErr.Code,
			connectorErr.Retryable,
			connectorRetryAfter(connectorErr),
			connectorErr,
		)
	}
	return LiveItemCommitResult{
		DuplicateNoop:         convergence == connector.ConvergenceDuplicateNoop,
		CompatibilityCompared: true,
		CompatibilityMatched:  true,
		CompatibilityEligible: committer.promotionEligible.Load(),
	}, nil
}

func projectReferenceSafely(
	projector ReferenceItemProjector,
	ctx context.Context,
	source []byte,
	options unirouter.CompletedExecutionMapOptions,
) (
	item connector.NormalizedItem,
	connectorErr *connector.ConnectorError,
) {
	defer func() {
		if recover() != nil {
			item = connector.NormalizedItem{}
			connectorErr = connector.NewConnectorError(
				connector.ErrorInternal,
				"foigent.reference.projector_panicked",
				connector.PhaseNormalize,
				"normalize_item",
				false,
				"Reference projection failed unexpectedly",
				connector.RecoveryOperatorRepair,
			)
		}
	}()
	return projector.Project(ctx, source, options)
}

func (committer *ModeAwareEvidenceCommitter) logComparison(
	result string,
	mismatchClass string,
) {
	fields := map[string]any{
		"mode":                    string(committer.mode),
		"profile":                 unirouter.CompletedExecutionReferenceProfileID,
		"protocol_version":        unirouter.CompletedExecutionReferenceProtocolVersion,
		"protocol_release_digest": unirouter.CompletedExecutionReferenceReleaseDigest,
		"result":                  result,
		"promotion_eligible":      committer.promotionEligible.Load(),
	}
	if mismatchClass != "" {
		fields["mismatch_class"] = mismatchClass
	}
	logCompatibilityTelemetryFailOpen(
		committer.logger,
		"reference_projection_comparison",
		fields,
	)
}

func logCompatibilityTelemetryFailOpen(
	logger LiveWorkerLogFunc,
	event string,
	fields map[string]any,
) {
	defer func() {
		_ = recover()
	}()
	logger.Log(event, fields)
}

func compareReferenceProjection(
	legacy preparedEvidenceItem,
	neutral connector.NormalizedItem,
) string {
	if neutral.ProfileID != unirouter.CompletedExecutionReferenceProfileID ||
		neutral.Envelope.EventID != legacy.mapped.RequestID ||
		neutral.Envelope.ProducerInstance != legacy.mapped.SourceInstance ||
		neutral.Envelope.SourceHash != legacy.mapped.SnapshotSHA256 {
		return "envelope_identity"
	}
	if len(legacy.events) != len(neutral.Events) {
		return "event_count"
	}
	for index := range legacy.events {
		left := legacy.events[index]
		right := neutral.Events[index]
		if !equalEventFields(left, right, []string{
			"id", "source_ref", "idempotency_key", "event_hash",
		}) {
			return "event_identity"
		}
		if !equalEventFields(left, right, []string{
			"event_type", "stage", "severity", "capture_mode",
			"payload_capture_mode",
		}) {
			return "event_shape"
		}
		if !equalEventFields(left, right, []string{
			"payload_policy", "field_provenance",
		}) {
			return "field_provenance"
		}
		if !reflect.DeepEqual(left, right) {
			return "event_payload"
		}
	}
	if !reflect.DeepEqual(legacy.provenance, neutral.Provenance) {
		return "provenance"
	}
	return ""
}

func equalEventFields(
	left map[string]any,
	right map[string]any,
	fields []string,
) bool {
	for _, field := range fields {
		if !reflect.DeepEqual(left[field], right[field]) {
			return false
		}
	}
	return true
}

type evidenceStoreCommitPort struct {
	store EvidencePersister
}

func (port *evidenceStoreCommitPort) CommitItem(
	ctx context.Context,
	item connector.NormalizedItem,
) (connector.ItemConvergence, *connector.ConnectorError) {
	if port == nil || port.store == nil {
		return "", connectorCommitError(
			"evidence_committer_uninitialized",
			false,
			"Evidence committer is unavailable",
		)
	}
	if err := connector.ValidateNormalizedItem(item); err != nil {
		return "", connectorCommitError(
			"reference_projection_invalid",
			false,
			"Normalized Evidence item is invalid",
		)
	}
	report, err := port.store.PersistEventsWithProvenance(
		ctx,
		item.Events,
		item.Provenance,
	)
	if err != nil {
		workerErr := classifyEvidencePersistenceError(err)
		return "", connectorCommitError(
			workerErr.Code,
			workerErr.Retryable,
			"Evidence persistence failed",
		)
	}
	convergence, connectorErr := connector.ClassifyItemConvergence(
		len(item.Events),
		connector.EvidenceCommitReport{
			EventsTotal:         report.EventsTotal,
			Inserted:            report.Inserted,
			DuplicateNoop:       report.DuplicateNoop,
			IdempotencyConflict: report.IdempotencyConflict,
			Rejected:            report.Rejected,
		},
	)
	if connectorErr == nil {
		return convergence, nil
	}
	if report.EventsTotal != len(item.Events) ||
		report.IdempotencyConflict != 0 ||
		report.Rejected != 0 {
		return "", connectorCommitError(
			"evidence_conflict",
			false,
			"Evidence item did not converge",
		)
	}
	return "", connectorCommitError(
		"evidence_partial_convergence",
		false,
		"Evidence item partially converged",
	)
}

func connectorCommitError(
	code string,
	retryable bool,
	message string,
) *connector.ConnectorError {
	recovery := connector.RecoveryOperatorRepair
	if retryable {
		recovery = connector.RecoveryAutomaticRetry
	}
	return connector.NewConnectorError(
		connector.ErrorPersistence,
		code,
		connector.PhaseCommit,
		"commit_item",
		retryable,
		message,
		recovery,
	)
}

func connectorRetryAfter(err *connector.ConnectorError) time.Duration {
	if err == nil || err.RetryAfterSeconds == nil {
		return 0
	}
	return time.Duration(*err.RetryAfterSeconds) * time.Second
}

var _ connector.EvidenceCommitPort = (*evidenceStoreCommitPort)(nil)
