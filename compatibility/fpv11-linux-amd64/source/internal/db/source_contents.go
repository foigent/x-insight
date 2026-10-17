package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"time"
)

func (s *SourceFactsStore) LoadContentCollection(ctx context.Context, id string) (sourcefacts.ContentCollection, error) {
	return loadContentCollection(ctx, s.db, id, false)
}
func loadContentCollection(ctx context.Context, q workspaceQueryer, id string, lock bool) (sourcefacts.ContentCollection, error) {
	var c sourcefacts.ContentCollection
	var purposes, coverage, retention, gaps []byte
	query := `SELECT id::text,source_system_id::text,source_instance,org_id,project_id,credential_id,registration_hash,content_scope_revision,recovery_epoch,allowed_purposes,coverage_from,cursor_value,last_sequence,high_watermark_sequence,high_watermark,status,revision,requires_attention,last_error,retention_floor,coverage,gaps FROM source_content_collections WHERE id=$1::uuid`
	if lock {
		query += " FOR UPDATE"
	}
	err := q.QueryRowContext(ctx, query, id).Scan(&c.ID, &c.SourceSystemID, &c.SourceInstance, &c.OrgID, &c.ProjectID, &c.CredentialID, &c.RegistrationHash, &c.ContentScopeRevision, &c.RecoveryEpoch, &purposes, &coverage, &c.Cursor, &c.LastSequence, &c.HighWatermarkSequence, &c.HighWatermark, &c.Status, &c.Revision, &c.RequiresAttention, &c.LastError, &retention, &c.Coverage, &gaps)
	if errors.Is(err, sql.ErrNoRows) {
		return c, sourcefacts.ErrUnavailable
	}
	if err != nil {
		return c, err
	}
	if json.Unmarshal(purposes, &c.AllowedPurposes) != nil || json.Unmarshal(coverage, &c.CoverageFrom) != nil || json.Unmarshal(retention, &c.RetentionFloor) != nil || json.Unmarshal(gaps, &c.Gaps) != nil || !sourcefacts.ValidContentCollection(c) {
		return c, sourcefacts.ErrInvalid
	}
	return c, nil
}
func (s *SourceFactsStore) RegisterContentCollection(ctx context.Context, c sourcefacts.ContentCollection) (sourcefacts.ContentCollection, error) {
	if !sourcefacts.ValidContentCollection(c) {
		return c, sourcefacts.ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return c, e
	}
	defer tx.Rollback()
	if e = requireSourceAdministrator(ctx, tx); e != nil {
		return c, e
	}
	if e = sourceLock(ctx, tx, c.SourceSystemID); e != nil {
		return c, e
	}
	if e = ensureManagedSource(ctx, tx, c.SourceSystemID, c.SourceInstance); e != nil {
		return c, e
	}
	purposes, _ := json.Marshal(c.AllowedPurposes)
	coverage, _ := json.Marshal(c.CoverageFrom)
	result, e := tx.ExecContext(ctx, `INSERT INTO source_content_collections(id,source_system_id,source_instance,org_id,project_id,credential_id,registration_hash,content_scope_revision,recovery_epoch,allowed_purposes,coverage_from,last_sequence,high_watermark_sequence) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb,$12,$12) ON CONFLICT DO NOTHING`, c.ID, c.SourceSystemID, c.SourceInstance, c.OrgID, c.ProjectID, c.CredentialID, c.RegistrationHash, c.ContentScopeRevision, c.RecoveryEpoch, string(purposes), string(coverage), c.CoverageFrom.AfterSequence)
	if e != nil {
		return c, e
	}
	live, e := loadContentCollection(ctx, tx, c.ID, true)
	if e != nil || !sourcefacts.SameContentCollection(live, c) {
		return c, sourcefacts.ErrConflict
	}
	if n, _ := result.RowsAffected(); n == 1 {
		if e = sourceManagementEvent(ctx, tx, "content", c.ID, "register", live.Status, live.Revision); e != nil {
			return c, e
		}
	}
	return live, tx.Commit()
}

// A content-only operation never updates source_fact_streams or budget rows.
// Activation requires a just-preflighted exact registration snapshot, closing
// the local preflight/control race without holding a transaction across HTTP.
func (s *SourceFactsStore) ControlContentCollection(ctx context.Context, id, status string, revision int64, acknowledge bool, expected ...sourcefacts.ContentCollection) (sourcefacts.ContentCollection, error) {
	var zero sourcefacts.ContentCollection
	if revision < 1 || (status != "active" && status != "paused" && status != "revoked") {
		return zero, sourcefacts.ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return zero, e
	}
	defer tx.Rollback()
	if e = requireSourceAdministrator(ctx, tx); e != nil {
		return zero, e
	}
	before, e := loadContentCollection(ctx, tx, id, false)
	if e != nil {
		return zero, e
	}
	if e = sourceLock(ctx, tx, before.SourceSystemID); e != nil {
		return zero, e
	}
	live, e := loadContentCollection(ctx, tx, id, true)
	if e != nil {
		return zero, e
	}
	if live.Revision != revision || live.Status == "revoked" && status != "revoked" {
		return zero, sourcefacts.ErrStreamChanged
	}
	if status == "active" && (len(expected) != 1 || !sourcefacts.SameContentCollection(live, expected[0]) || expected[0].Revision != live.Revision) {
		return zero, sourcefacts.ErrScopeChanged
	}
	if status == "active" && live.RequiresAttention && !acknowledge {
		return zero, sourcefacts.ErrUnavailable
	}
	if live.Status != status || status == "active" && live.RequiresAttention {
		_, e = tx.ExecContext(ctx, `UPDATE source_content_collections SET status=$2,revision=revision+1,requires_attention=CASE WHEN $2='active' THEN false ELSE requires_attention END,last_error=CASE WHEN $2='active' THEN '' ELSE last_error END WHERE id=$1::uuid`, id, status)
		if e != nil {
			return zero, e
		}
		if e = sourceManagementEvent(ctx, tx, "content", id, "state_change", status, live.Revision+1); e != nil {
			return zero, e
		}
	}
	live, e = loadContentCollection(ctx, tx, id, false)
	if e != nil {
		return zero, e
	}
	return live, tx.Commit()
}
func lockContentCollection(ctx context.Context, tx *sql.Tx, expected sourcefacts.ContentCollection) (sourcefacts.ContentCollection, error) {
	if !sourcefacts.ValidContentCollection(expected) {
		return expected, sourcefacts.ErrInvalid
	}
	if e := sourceLock(ctx, tx, expected.SourceSystemID); e != nil {
		return expected, e
	}
	live, e := loadContentCollection(ctx, tx, expected.ID, true)
	if e != nil {
		return live, e
	}
	if !sourcefacts.SameContentCollection(live, expected) || live.Revision != expected.Revision || live.Status != "active" || live.RequiresAttention || live.Cursor != expected.Cursor || live.LastSequence != expected.LastSequence || live.HighWatermark != expected.HighWatermark || live.HighWatermarkSequence != expected.HighWatermarkSequence {
		return live, sourcefacts.ErrStreamChanged
	}
	return live, nil
}
func (s *SourceFactsStore) CommitContentManifests(ctx context.Context, expected sourcefacts.ContentCollection, raw []byte, now time.Time) error {
	p, e := sourcefacts.DecodeContentManifestPage(raw)
	if e != nil {
		return e
	}
	if e = sourcefacts.MatchContentPage(expected, p); e != nil {
		return e
	}
	if now.IsZero() {
		return sourcefacts.ErrInvalid
	}
	// Preserve canonical bytes from the actual source object, not a reconstructed
	// projection. They are the only bytes allowed to establish source_sha256.
	obj, e := connector.DecodeStrictJSONObject(raw)
	if e != nil {
		return e
	}
	items := obj["items"].([]any)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	live, e := lockContentCollection(ctx, tx, expected)
	if e != nil {
		return e
	}
	for i, entry := range p.Items {
		d := entry.Document
		canonical, e := connector.CanonicalizeJSONValue(items[i].(map[string]any)["document"])
		if e != nil || sourcefacts.Hash(canonical) != entry.SourceSHA256 {
			return sourcefacts.ErrInvalid
		}
		var previous []byte
		var hash string
		if d.PreviousRevision != "" {
			e = tx.QueryRowContext(ctx, `SELECT source_bytes,source_sha256 FROM source_content_manifests WHERE source_system_id=$1::uuid AND source_fact_id=$2 AND revision=$3::numeric`, live.SourceSystemID, d.SourceFactID, d.PreviousRevision).Scan(&previous, &hash)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if e == nil {
				old, e := sourcefacts.DecodeContentManifest(previous)
				if e != nil {
					return e
				}
				if e = sourcefacts.ValidateContentManifestSuccessor(sourcefacts.ContentManifestEntry{Document: old, SourceSHA256: hash}, entry); e != nil {
					return e
				}
			}
		}
		availability, _ := json.Marshal(entry.Availability)
		_, e = tx.ExecContext(ctx, `INSERT INTO source_content_manifests(collection_id,sequence,source_system_id,source_instance,org_id,project_id,request_id,source_fact_id,revision,event_id,previous_revision,previous_hash,source_sha256,source_bytes,source_json,content_scope_revision,recovery_epoch,availability,occurred_at,recorded_at,received_at) VALUES($1::uuid,$2::numeric,$3::uuid,$4,$5,$6,$7,$8,$9::numeric,$10,NULLIF($11,'')::numeric,$12,$13,$14,$15::jsonb,$16,$17,$18::jsonb,$19::timestamptz,$20::timestamptz,$21) ON CONFLICT DO NOTHING`, live.ID, entry.Sequence, live.SourceSystemID, live.SourceInstance, live.OrgID, live.ProjectID, d.RequestID, d.SourceFactID, d.Revision, d.EventID, d.PreviousRevision, d.PreviousHash, entry.SourceSHA256, canonical, string(canonical), live.ContentScopeRevision, live.RecoveryEpoch, string(availability), d.OccurredAt, d.RecordedAt, now.UTC())
		if e != nil {
			return e
		}
		var stored []byte
		e = tx.QueryRowContext(ctx, `SELECT source_bytes FROM source_content_manifests WHERE source_system_id=$1::uuid AND source_fact_id=$2 AND revision=$3::numeric AND event_id=$4 AND source_sha256=$5`, live.SourceSystemID, d.SourceFactID, d.Revision, d.EventID, entry.SourceSHA256).Scan(&stored)
		if e != nil || string(stored) != string(canonical) {
			return sourcefacts.ErrConflict
		}
		coverage, _ := json.Marshal(live.CoverageFrom)
		_, e = tx.ExecContext(ctx, `INSERT INTO source_content_receipts(collection_id,credential_id,registration_hash,content_scope_revision,recovery_epoch,coverage_from,sequence,source_system_id,source_fact_id,revision,source_sha256,availability,received_at)
 VALUES($1::uuid,$2,$3,$4,$5,$6::jsonb,$7::numeric,$8::uuid,$9,$10::numeric,$11,$12::jsonb,$13) ON CONFLICT DO NOTHING`, live.ID, live.CredentialID, live.RegistrationHash, live.ContentScopeRevision, live.RecoveryEpoch, string(coverage), entry.Sequence, live.SourceSystemID, d.SourceFactID, d.Revision, entry.SourceSHA256, string(availability), now.UTC())
		if e != nil {
			return e
		}
		var accepted bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_content_receipts WHERE collection_id=$1::uuid AND credential_id=$2 AND registration_hash=$3 AND content_scope_revision=$4 AND recovery_epoch=$5 AND coverage_from=$6::jsonb AND sequence=$7::numeric AND source_system_id=$8::uuid AND source_fact_id=$9 AND revision=$10::numeric AND source_sha256=$11)`, live.ID, live.CredentialID, live.RegistrationHash, live.ContentScopeRevision, live.RecoveryEpoch, string(coverage), entry.Sequence, live.SourceSystemID, d.SourceFactID, d.Revision, entry.SourceSHA256).Scan(&accepted)
		if e != nil {
			return e
		}
		if !accepted {
			return sourcefacts.ErrConflict
		}

	}
	retention, _ := json.Marshal(p.RetentionFloor)
	gaps, _ := json.Marshal(p.Gaps)
	_, e = tx.ExecContext(ctx, `UPDATE source_content_collections SET retention_floor=$7::jsonb,coverage=$8,gaps=$9::jsonb,cursor_value=$2,last_sequence=$3,high_watermark_sequence=$4,high_watermark=$5,revision=revision+1,last_error='',last_attempt_at=$6,last_success_at=$6 WHERE id=$1::uuid`, live.ID, p.NextCursor, p.LastSequence, p.HighWatermarkSequence, p.HighWatermark, now.UTC(), string(retention), p.Coverage, string(gaps))
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *SourceFactsStore) RecordContentFailure(ctx context.Context, expected sourcefacts.ContentCollection, code string, now time.Time) error {
	if !validContentFailureCode(code) {
		return sourcefacts.ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	live, e := lockContentCollection(ctx, tx, expected)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE source_content_collections SET revision=revision+1,last_attempt_at=$2,last_error=$3,requires_attention=$4 WHERE id=$1::uuid`, live.ID, now.UTC(), code, sourcefacts.ContentNeedsAttention(code))
	if e != nil {
		return e
	}
	return tx.Commit()
}
func validContentFailureCode(c string) bool {
	switch c {
	case "profile_not_authorized", "source_paused", "staged_credential_requires_pause", "cursor_or_epoch_conflict", "cursor_expired", "source_authorization_denied", "scope_changed", "invalid_content_page", "source_fact_conflict", "source_unavailable":
		return true
	}
	return false
}

// RebindContentCollection changes only a paused C authorization projection.
// The host preflights the explicitly configured replacement; no UR management
// wire is inferred here. Retained manifests and legacy collections are untouched.
func (s *SourceFactsStore) RebindContentCollection(ctx context.Context, next sourcefacts.ContentCollection, revision int64) (sourcefacts.ContentCollection, error) {
	if !sourcefacts.ValidContentCollection(next) || revision < 1 {
		return next, sourcefacts.ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return next, e
	}
	defer tx.Rollback()
	if e = requireSourceAdministrator(ctx, tx); e != nil {
		return next, e
	}
	if e = sourceLock(ctx, tx, next.SourceSystemID); e != nil {
		return next, e
	}
	live, e := loadContentCollection(ctx, tx, next.ID, true)
	if e != nil {
		return next, e
	}
	if live.Status != "paused" || live.Revision != revision {
		return live, sourcefacts.ErrStreamChanged
	}
	if live.SourceSystemID != next.SourceSystemID || live.SourceInstance != next.SourceInstance || live.OrgID != next.OrgID || live.ProjectID != next.ProjectID {
		return live, sourcefacts.ErrScopeChanged
	}
	if sourcefacts.SameContentCollection(live, next) {
		return live, nil
	}
	purposes, _ := json.Marshal(next.AllowedPurposes)
	coverage, _ := json.Marshal(next.CoverageFrom)
	_, e = tx.ExecContext(ctx, `UPDATE source_content_collections SET credential_id=$2,registration_hash=$3,content_scope_revision=$4,recovery_epoch=$5,allowed_purposes=$6::jsonb,coverage_from=$7::jsonb,cursor_value='',last_sequence=$8,high_watermark_sequence=$8,high_watermark='',retention_floor='{}'::jsonb,coverage='unknown',gaps='[]'::jsonb,revision=revision+1,requires_attention=true,last_error='scope_changed',last_success_at=NULL WHERE id=$1::uuid`, next.ID, next.CredentialID, next.RegistrationHash, next.ContentScopeRevision, next.RecoveryEpoch, string(purposes), string(coverage), next.CoverageFrom.AfterSequence)
	if e != nil {
		return next, e
	}
	if e = sourceManagementEvent(ctx, tx, "content", next.ID, "state_change", "paused", live.Revision+1); e != nil {
		return next, e
	}
	next, e = loadContentCollection(ctx, tx, next.ID, false)
	if e != nil {
		return next, e
	}
	return next, tx.Commit()
}
