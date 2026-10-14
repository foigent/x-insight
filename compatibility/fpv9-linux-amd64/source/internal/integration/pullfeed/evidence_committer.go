package pullfeed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/investigation"
)

type EvidencePersister interface {
	PersistEventsWithProvenance(
		context.Context,
		[]map[string]any,
		investigation.ImportProvenance,
	) (db.PersistenceReport, error)
}

type EvidenceCommitter struct {
	store EvidencePersister
	now   func() time.Time
}

type preparedEvidenceItem struct {
	mapped     unirouter.CompletedExecutionMapResult
	events     []map[string]any
	provenance investigation.ImportProvenance
}

func NewEvidenceCommitter(store EvidencePersister, now func() time.Time) (*EvidenceCommitter, error) {
	if store == nil {
		return nil, fmt.Errorf("evidence persister is required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &EvidenceCommitter{store: store, now: now}, nil
}

func (c *EvidenceCommitter) CommitLive(
	ctx context.Context,
	page FeedPage,
	item FeedItem,
) (LiveItemCommitResult, error) {
	if c == nil || c.store == nil {
		return LiveItemCommitResult{}, NewLiveWorkerError(
			"evidence_committer_uninitialized",
			false,
			0,
			nil,
		)
	}

	prepared, workerErr := prepareLegacyEvidenceItem(page, item, c.now().UTC())
	if workerErr != nil {
		return LiveItemCommitResult{}, workerErr
	}
	return commitPreparedEvidenceItem(ctx, c.store, prepared)
}

func prepareLegacyEvidenceItem(
	page FeedPage,
	item FeedItem,
	ingestedAt time.Time,
) (preparedEvidenceItem, *LiveWorkerError) {
	mapped, err := unirouter.MapCompletedExecutionV1(item.Snapshot, unirouter.CompletedExecutionMapOptions{
		SourceInstance: page.SourceInstance,
		ObservedAt:     page.GeneratedAt,
		IngestedAt:     ingestedAt.UTC(),
	})
	if err != nil {
		return preparedEvidenceItem{}, NewLiveWorkerError("contract_snapshot_invalid", false, 0, err)
	}
	if mapped.SnapshotSHA256 != item.SnapshotSHA256 {
		return preparedEvidenceItem{}, NewLiveWorkerError("snapshot_hash_mismatch", false, 0, nil)
	}
	if mapped.RequestID != item.RequestID {
		return preparedEvidenceItem{}, NewLiveWorkerError("request_identity_mismatch", false, 0, nil)
	}
	if !terminalTimesEqualAtPostgresPrecision(
		mapped.TerminalAvailableAt,
		item.TerminalAvailableAt,
	) {
		return preparedEvidenceItem{}, NewLiveWorkerError("terminal_time_mismatch", false, 0, nil)
	}
	if item.Revision != unirouter.CompletedExecutionRevisionV1 || !item.TimelineComplete {
		return preparedEvidenceItem{}, NewLiveWorkerError("immutable_revision_mismatch", false, 0, nil)
	}

	terminalAt := mapped.TerminalAvailableAt.UTC()
	provenance := investigation.ImportProvenance{
		SourceSystemID:      mapped.SourceSystemID,
		AdapterManifestID:   mapped.AdapterManifestID,
		IntegrationMode:     investigation.IntegrationPull,
		ProofClass:          investigation.ProofOperatorApproved,
		AssuranceMode:       investigation.AssuranceContractedPull,
		SourcePayloadSHA256: mapped.SnapshotSHA256,
		SourceTerminalAt:    &terminalAt,
		SourceRevision:      item.Revision,
		ImportedRevision:    unirouter.CompletedExecutionRevisionV1,
		TimelineComplete:    true,
	}
	return preparedEvidenceItem{
		mapped:     mapped,
		events:     mapped.Events,
		provenance: provenance,
	}, nil
}

func commitPreparedEvidenceItem(
	ctx context.Context,
	store EvidencePersister,
	prepared preparedEvidenceItem,
) (LiveItemCommitResult, error) {
	report, err := store.PersistEventsWithProvenance(
		ctx,
		prepared.events,
		prepared.provenance,
	)
	if err != nil {
		return LiveItemCommitResult{}, classifyEvidencePersistenceError(err)
	}
	if report.EventsTotal != len(prepared.events) ||
		report.IdempotencyConflict != 0 ||
		report.Rejected != 0 {
		return LiveItemCommitResult{}, NewLiveWorkerError("evidence_conflict", false, 0, nil)
	}
	switch {
	case report.Inserted == len(prepared.events) && report.DuplicateNoop == 0:
		return LiveItemCommitResult{}, nil
	case report.DuplicateNoop == len(prepared.events) && report.Inserted == 0:
		return LiveItemCommitResult{DuplicateNoop: true}, nil
	default:
		return LiveItemCommitResult{}, NewLiveWorkerError("evidence_partial_convergence", false, 0, nil)
	}
}

func terminalTimesEqualAtPostgresPrecision(left, right time.Time) bool {
	delta := left.UTC().Sub(right.UTC())
	return delta > -time.Microsecond && delta < time.Microsecond
}

func classifyEvidencePersistenceError(err error) *LiveWorkerError {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return NewLiveWorkerError("evidence_context_ended", true, 0, err)
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		class := postgresError.Code
		if len(class) >= 2 {
			class = class[:2]
		}
		switch class {
		case "08", "40", "53", "55", "57", "58":
			return NewLiveWorkerError("evidence_database_unavailable", true, 0, err)
		default:
			return NewLiveWorkerError("evidence_database_rejected", false, 0, err)
		}
	}
	return NewLiveWorkerError("evidence_persist_failed", true, 0, err)
}
