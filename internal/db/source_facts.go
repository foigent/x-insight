package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

// SourceFactsStore uses the existing importer role. Collection registration
// and scope changes remain an explicit admin operation, not a side effect of pull.
type SourceFactsStore struct{ db *sql.DB }

func OpenSourceFactsStore(dsn string) (*SourceFactsStore, error) {
	d, e := sql.Open("pgx", dsn)
	if e != nil {
		return nil, e
	}
	return &SourceFactsStore{db: d}, nil
}
func (s *SourceFactsStore) Close() error { return s.db.Close() }

func (s *SourceFactsStore) LoadSourceFactsStream(ctx context.Context, id string) (sourcefacts.Stream, error) {
	return loadSourceStream(ctx, s.db, id, false)
}
func loadSourceStream(ctx context.Context, q workspaceQueryer, id string, lock bool) (sourcefacts.Stream, error) {
	var s sourcefacts.Stream
	query := `SELECT id::text,source_system_id::text,source_instance,org_id,project_id,profile,scope_generation,status,cursor_value,high_watermark,revision,requires_attention,last_error FROM source_fact_streams WHERE id=$1::uuid`
	if lock {
		query += " FOR UPDATE"
	}
	err := q.QueryRowContext(ctx, query, id).Scan(&s.ID, &s.SourceSystemID, &s.SourceInstance, &s.OrgID, &s.ProjectID, &s.Profile, &s.ScopeGeneration, &s.Status, &s.Cursor, &s.HighWatermark, &s.Revision, &s.RequiresAttention, &s.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return s, sourcefacts.ErrUnavailable
	}
	return s, err
}

func (s *SourceFactsStore) CommitSourceFactsPage(ctx context.Context, expected sourcefacts.Stream, page sourcefacts.Page, now time.Time) error {
	// Revalidate the wire page at the store boundary; exported structs are not
	// evidence of prior validation and cannot bypass scope or bootstrap checks.
	raw, err := json.Marshal(page)
	if err != nil {
		return sourcefacts.ErrInvalid
	}
	decode := sourcefacts.DecodePage
	if sourcefacts.ValidObservationProfile(expected.Profile) {
		decode = sourcefacts.DecodeObservationPage
	}
	p, err := decode(raw, expected.SourceInstance, expected.OrgID, expected.ProjectID, expected.Profile, expected.Cursor, 50)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Different scope generations can replay the same immutable documents.
	// Serialize their adjacency checks and inserts, including out-of-order
	// revisions, so two transactions cannot retain contradictory links.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "source-facts:"+expected.SourceSystemID); err != nil {
		return err
	}
	live, err := loadSourceStream(ctx, tx, expected.ID, true)
	if err != nil {
		return err
	}
	if live != expected || live.Status != "active" || live.RequiresAttention {
		return sourcefacts.ErrStreamChanged
	}
	if _, err := insertSourceFactItems(ctx, tx, live.SourceSystemID, p.Items); err != nil {
		return err
	}
	if _, err := insertSourceObservations(ctx, tx, live.SourceSystemID, p.Observations); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE source_fact_streams SET cursor_value=$2,high_watermark=$3,revision=revision+1,last_error='',last_received_at=$4,updated_at=$4 WHERE id=$1::uuid`, live.ID, p.NextCursor, p.HighWatermark, now.UTC())
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Both transports call this function inside their own authorized transaction,
// after taking the same source lock. It never changes a live checkpoint.
func insertSourceFactItems(ctx context.Context, tx *sql.Tx, sourceSystemID string, items []sourcefacts.Item) (int64, error) {
	var inserted int64
	for _, item := range items {
		d := item.Document
		revision, _ := strconv.ParseInt(d.Revision, 10, 64)
		var existingSource, existingEnvelope []byte
		err := tx.QueryRowContext(ctx, `SELECT source_bytes,envelope_bytes FROM source_fact_documents WHERE source_system_id=$1::uuid AND source_fact_id=$2 AND revision=$3`, sourceSystemID, d.SourceFactID, revision).Scan(&existingSource, &existingEnvelope)
		if err == nil {
			if !bytes.Equal(existingSource, item.SourceBytes) || !bytes.Equal(existingEnvelope, item.EnvelopeBytes) {
				return 0, sourcefacts.ErrConflict
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		// A missing predecessor is allowed. A present contradictory link is a
		// conflict, including a later-arriving predecessor of a retained child.
		var conflict bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_fact_documents WHERE source_system_id=$1::uuid AND source_fact_id=$2 AND ((revision=$3::bigint-1 AND source_hash<>$4) OR (revision-1=$3::bigint AND previous_hash<>$5)))`, sourceSystemID, d.SourceFactID, revision, d.PreviousHash, item.SourceHash).Scan(&conflict); err != nil {
			return 0, err
		}
		if conflict {
			return 0, sourcefacts.ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO source_fact_documents(source_system_id,source_instance,org_id,project_id,request_id,profile,stage,source_fact_id,revision,event_id,previous_hash,source_hash,envelope_hash,source_bytes,envelope_bytes,source_json,occurred_at,recorded_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16::jsonb,$17,$18)`, sourceSystemID, d.SourceInstance, d.OrgID, d.ProjectID, d.RequestID, d.Profile, d.Stage, d.SourceFactID, revision, d.EventID, d.PreviousHash, item.SourceHash, item.EnvelopeHash, item.SourceBytes, item.EnvelopeBytes, string(item.SourceBytes), d.OccurredAt, d.RecordedAt)
		if err != nil {
			return 0, err
		}
		inserted++
	}
	return inserted, nil
}

func (s *SourceFactsStore) RecordSourceFactsFailure(ctx context.Context, expected sourcefacts.Stream, code string, now time.Time) error {
	switch code {
	case "invalid_capabilities", "scope_changed", "source_paused", "invalid_page", "cursor_or_epoch_conflict", "cursor_expired", "source_authorization_denied", "source_unavailable", "source_fact_conflict":
	default:
		return sourcefacts.ErrInvalid
	}
	// This separate transaction records the failed attempt only after the bad
	// page rolled back, and only if no successful/administrative progress won.
	result, err := s.db.ExecContext(ctx, `UPDATE source_fact_streams SET last_error=$3,updated_at=$4,revision=revision+1,requires_attention=$8
	 WHERE id=$1::uuid AND revision=$2 AND status='active' AND status=$5
	 AND NOT requires_attention AND cursor_value=$6 AND high_watermark=$7`, expected.ID, expected.Revision, code, now.UTC(), expected.Status, expected.Cursor, expected.HighWatermark, sourcefacts.NeedsAttention(code))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sourcefacts.ErrStreamChanged
	}
	return nil
}
