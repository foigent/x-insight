package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/foigent/x-insight/internal/connectorsync"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func currentRuntimeCursor(ctx context.Context, q workspaceQueryer, id string, lock bool) (sourcefacts.CurrentRuntimeCursor, error) {
	c := sourcefacts.CurrentRuntimeCursor{Generation: 1, PageOrdinal: 1}
	query := `SELECT generation,page_ordinal,revision,cursor_value,requires_attention FROM source_runtime_snapshots WHERE stream_id=$1::uuid`
	if lock {
		query += " FOR UPDATE"
	}
	err := q.QueryRowContext(ctx, query, id).Scan(&c.Generation, &c.PageOrdinal, &c.Revision, &c.Cursor, &c.RequiresAttention)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	return c, err
}

func (s *SourceFactsStore) LoadCurrentRuntimeCursor(ctx context.Context, stream sourcefacts.Stream) (sourcefacts.CurrentRuntimeCursor, error) {
	if !sourcefacts.ValidRegistration(stream) || stream.Profile != sourcefacts.RuntimeProfile {
		return sourcefacts.CurrentRuntimeCursor{}, sourcefacts.ErrInvalid
	}
	c, err := currentRuntimeCursor(ctx, s.db, stream.ID, false)
	if err == nil && c.RequiresAttention {
		return c, sourcefacts.ErrUnavailable
	}
	return c, err
}

func (s *SourceFactsStore) currentRuntimeTransaction(ctx context.Context, expected sourcefacts.Stream, cursor sourcefacts.CurrentRuntimeCursor) (*sql.Tx, error) {
	if !sourcefacts.ValidRegistration(expected) || expected.Profile != sourcefacts.RuntimeProfile || cursor.RequiresAttention {
		return nil, sourcefacts.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*sql.Tx, error) { tx.Rollback(); return nil, err }
	if err = connectorsync.Fence(ctx, tx); err != nil {
		return fail(err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "source-facts:"+expected.SourceSystemID); err != nil {
		return fail(err)
	}
	live, err := loadSourceStream(ctx, tx, expected.ID, true)
	if err != nil {
		return fail(err)
	}
	if live != expected || !sourcefacts.CurrentRuntimeCollectionAllowed(live) {
		return fail(sourcefacts.ErrStreamChanged)
	}
	current, err := currentRuntimeCursor(ctx, tx, expected.ID, true)
	if err != nil {
		return fail(err)
	}
	if current != cursor {
		return fail(sourcefacts.ErrStreamChanged)
	}
	return tx, nil
}

func (s *SourceFactsStore) CommitCurrentRuntimePage(ctx context.Context, expected sourcefacts.Stream, cursor sourcefacts.CurrentRuntimeCursor, raw []byte, now time.Time) error {
	page, err := sourcefacts.DecodeCurrentRuntimePage(raw, expected.SourceInstance, expected.OrgID, expected.ProjectID)
	if err != nil || page.Coverage != "lease_backed_inventory" || page.HasMore && page.NextCursor == cursor.Cursor {
		return sourcefacts.ErrInvalid
	}
	// The existing runtime reading limit is 500,000 observations. Keep this
	// temporary inventory bounded to at most 10,000 source pages of 50 entries.
	if cursor.PageOrdinal > 10000 {
		return sourcefacts.ErrInvalid
	}
	tx, err := s.currentRuntimeTransaction(ctx, expected, cursor)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if syncErr := connectorsync.CommitFence(ctx, tx); syncErr != nil {
		return syncErr
	}
	if cursor.Revision == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO source_runtime_snapshots(stream_id,revision,generation,page_ordinal,last_attempt_at) VALUES($1::uuid,1,1,1,$2)`, expected.ID, now.UTC())
		if err != nil {
			return err
		}
	}
	var repeated bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_runtime_snapshot_pages WHERE stream_id=$1::uuid AND generation=$2 AND request_cursor_sha256=$3)`, expected.ID, cursor.Generation, sourcefacts.Hash([]byte(cursor.Cursor))).Scan(&repeated); err != nil {
		return err
	}
	if repeated {
		return sourcefacts.ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO source_runtime_snapshot_pages(stream_id,generation,ordinal,request_cursor_sha256,source_sha256,source_bytes,source_json,observed_at,received_at)
 VALUES($1::uuid,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)`, expected.ID, cursor.Generation, cursor.PageOrdinal, sourcefacts.Hash([]byte(cursor.Cursor)), page.SourceSHA256, raw, string(raw), page.ObservedAt, now.UTC())
	if err != nil {
		return err
	}
	if page.HasMore {
		_, err = tx.ExecContext(ctx, `UPDATE source_runtime_snapshots SET revision=$2,generation=$3,page_ordinal=$4,cursor_value=$5,last_error='',last_attempt_at=$6,last_success_at=$6 WHERE stream_id=$1::uuid`, expected.ID, cursor.Revision+1, cursor.Generation, cursor.PageOrdinal+1, page.NextCursor, now.UTC())
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE source_runtime_snapshots SET revision=$2,generation=$3+1,page_ordinal=1,cursor_value='',visible_generation=$3,last_error='',last_attempt_at=$4,last_success_at=$4 WHERE stream_id=$1::uuid`, expected.ID, cursor.Revision+1, cursor.Generation, now.UTC())
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM source_runtime_snapshot_pages WHERE stream_id=$1::uuid AND generation<>$2`, expected.ID, cursor.Generation)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SourceFactsStore) RecordCurrentRuntimeFailure(ctx context.Context, expected sourcefacts.Stream, cursor sourcefacts.CurrentRuntimeCursor, code string, now time.Time) error {
	switch code {
	case "invalid_capabilities", "scope_changed", "source_paused", "invalid_page", "cursor_or_epoch_conflict", "cursor_expired", "source_authorization_denied", "source_unavailable":
	default:
		return sourcefacts.ErrInvalid
	}
	tx, err := s.currentRuntimeTransaction(ctx, expected, cursor)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if syncErr := connectorsync.Fence(ctx, tx); syncErr != nil {
		return syncErr
	}
	attention := sourcefacts.NeedsAttention(code) && code != "cursor_expired" && code != "cursor_or_epoch_conflict"
	if cursor.Revision == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO source_runtime_snapshots(stream_id,revision,generation,page_ordinal,last_error,last_attempt_at,requires_attention) VALUES($1::uuid,1,2,1,$2,$3,$4)`, expected.ID, code, now.UTC(), attention)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM source_runtime_snapshot_pages WHERE stream_id=$1::uuid AND generation=$2`, expected.ID, cursor.Generation)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE source_runtime_snapshots SET revision=revision+1,generation=generation+1,page_ordinal=1,cursor_value='',last_error=$2,last_attempt_at=$3,requires_attention=$4 WHERE stream_id=$1::uuid`, expected.ID, code, now.UTC(), attention)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Cache validation is read-only. Restore invalidates display eligibility even
// for unexpired leases; restoring bytes cannot re-establish a connection.
func validateRuntimeCurrentRestore(ctx context.Context, q workspaceQueryer) error {
	rows, err := q.QueryContext(ctx, `SELECT p.source_bytes,p.source_json::text,p.source_sha256,s.source_instance,s.org_id,s.project_id
 FROM source_runtime_snapshot_pages p JOIN source_fact_streams s ON s.id=p.stream_id ORDER BY p.stream_id,p.generation,p.ordinal`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var projection, hash, instance, org, project string
		if err = rows.Scan(&raw, &projection, &hash, &instance, &org, &project); err != nil {
			return err
		}
		page, err := sourcefacts.DecodeCurrentRuntimePage(raw, instance, org, project)
		if err != nil || page.SourceSHA256 != hash || page.Coverage != "lease_backed_inventory" {
			return sourcefacts.ErrInvalid
		}
		var a, b any
		if json.Unmarshal(raw, &a) != nil || json.Unmarshal([]byte(projection), &b) != nil {
			return sourcefacts.ErrInvalid
		}
		normalizedA, _ := json.Marshal(a)
		normalizedB, _ := json.Marshal(b)
		if string(normalizedA) != string(normalizedB) {
			return sourcefacts.ErrInvalid
		}
	}
	return rows.Err()
}
