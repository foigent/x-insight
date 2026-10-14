package openinferenceoffline

import (
	"context"
	"errors"

	"github.com/foigent/x-insight/internal/connector"
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
}

func NewEvidenceCommitter(store EvidencePersister) (*EvidenceCommitter, error) {
	if store == nil {
		return nil, errors.New("OpenInference Evidence persister is required")
	}
	return &EvidenceCommitter{store: store}, nil
}

func (committer *EvidenceCommitter) CommitItem(
	ctx context.Context,
	item connector.NormalizedItem,
) (connector.ItemConvergence, *connector.ConnectorError) {
	if committer == nil || committer.store == nil {
		return "", persistenceError(
			"foigent.openinference.evidence_committer_unavailable",
			false,
			"OpenInference Evidence committer is unavailable",
		)
	}
	if err := connector.ValidateNormalizedItem(item); err != nil {
		return "", persistenceError(
			"foigent.openinference.normalized_item_invalid",
			false,
			"OpenInference normalized Evidence item is invalid",
		)
	}
	report, err := committer.store.PersistEventsWithProvenance(
		ctx,
		item.Events,
		item.Provenance,
	)
	if err != nil {
		return "", persistenceError(
			"foigent.openinference.evidence_persist_failed",
			true,
			"OpenInference Evidence persistence failed",
		)
	}
	return connector.ClassifyItemConvergence(
		len(item.Events),
		connector.EvidenceCommitReport{
			EventsTotal:         report.EventsTotal,
			Inserted:            report.Inserted,
			DuplicateNoop:       report.DuplicateNoop,
			IdempotencyConflict: report.IdempotencyConflict,
			Rejected:            report.Rejected,
		},
	)
}

func persistenceError(
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
