package db

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

// The caller holds the existing source lock and current collection authority
// in this transaction. Out-of-order receipt is allowed, contradictory links
// are not. No observation writes an economic fact or changes an A document.
func insertSourceObservations(ctx context.Context, tx *sql.Tx, source string, items []sourcefacts.ObservationItem) (int64, error) {
	var inserted int64
	for _, item := range items {
		d := item.Document
		revision, _ := strconv.ParseInt(d.Revision, 10, 64)
		var existingSource, existingEnvelope []byte
		err := tx.QueryRowContext(ctx, `SELECT source_bytes,envelope_bytes FROM source_observation_documents WHERE source_system_id=$1::uuid AND source_fact_id=$2 AND revision=$3`, source, d.SourceFactID, revision).Scan(&existingSource, &existingEnvelope)
		if err == nil {
			if !bytes.Equal(existingSource, item.SourceBytes) || !bytes.Equal(existingEnvelope, item.EnvelopeBytes) {
				return 0, sourcefacts.ErrConflict
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		var conflict bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_observation_documents WHERE source_system_id=$1::uuid AND source_fact_id=$2 AND ((revision=$3::bigint-1 AND source_hash<>$4) OR (revision-1=$3::bigint AND previous_hash<>$5)))`, source, d.SourceFactID, revision, d.PreviousHash, item.SourceHash).Scan(&conflict)
		if err != nil {
			return 0, err
		}
		if conflict {
			return 0, sourcefacts.ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO source_observation_documents(source_system_id,source_instance,org_id,project_id,request_id,profile,subject_kind,subject_id,observation_kind,source_fact_id,revision,event_id,previous_hash,source_hash,envelope_hash,source_bytes,envelope_bytes,source_json,occurred_at,recorded_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18::jsonb,$19,$20)`, source, d.SourceInstance, d.OrgID, d.ProjectID, d.RequestID, d.Profile, d.SubjectKind, d.SubjectID, d.Kind, d.SourceFactID, revision, d.EventID, d.PreviousHash, item.SourceHash, item.EnvelopeHash, item.SourceBytes, item.EnvelopeBytes, string(item.SourceBytes), d.OccurredAt, d.RecordedAt)
		if err != nil {
			return 0, err
		}
		inserted++
	}
	return inserted, nil
}
