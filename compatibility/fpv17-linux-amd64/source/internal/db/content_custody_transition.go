package db

import (
	"context"
	"time"

	"github.com/foigent/x-insight/internal/reviewbody"
)

// Both selection and the locked final update use this predicate. The caller
// supplies the fixed e/c/s/i aliases and the observation time as $1. Capture
// health describes events the producer may never have observed; it is not a
// missing manifest page. Preserve that unknown claim while requiring every
// enumerated, unexpired representation to have an exact local copy.
const legacyCCustodyReadySQL = `e.legacy_c_copy_pending AND e.status='requires_migration'
 AND c.status='active' AND NOT c.requires_attention AND c.last_success_at IS NOT NULL
 AND c.last_sequence=c.high_watermark_sequence
 AND ((c.coverage='bounded' AND c.gaps='[]'::jsonb)
 OR (c.coverage='unknown' AND c.gaps='[{"reason":"capture_health_unknown","range_known":false}]'::jsonb))
 AND s.status='active' AND i.status='connected' AND i.desired_state='active'
 AND ROW(c.source_instance,c.org_id,c.project_id)=ROW(e.source_instance,e.org_id,e.project_id)
 AND NOT EXISTS(SELECT 1 FROM source_content_manifests d WHERE d.collection_id=c.id
 AND (d.source_json->>'expires_at')::timestamptz>$1
 AND d.source_json->'representation' IS NOT NULL AND d.source_json->'representation'<>'null'::jsonb
 AND NOT EXISTS(SELECT 1 FROM review_body_operations o JOIN review_body_objects b ON b.content_ref=o.content_ref
 WHERE o.content_collection_id=d.collection_id AND o.source_kind='ur_c' AND o.state='accepted' AND o.expires_at>$1
 AND ROW(o.source_system_id,o.source_instance,o.org_id,o.project_id,o.source_fact_id,o.source_revision,o.source_sha256,o.content_ref)
 =ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.source_fact_id,d.revision,d.source_sha256,d.source_json->>'content_ref')
 AND o.digest=d.source_json->'representation'->>'digest' AND o.plain_bytes=(d.source_json->'representation'->>'saved_bytes')::bigint
 AND o.byte_domain=d.source_json->'representation'->>'byte_domain'))`

// FinishLegacyCCustodyTransition shares the source lock with manifest commit
// and copy acceptance. An empty copy queue is insufficient: partial, policy,
// quota and integrity gaps must still prevent a premature custody switch.
func (s *WorkspaceStore) FinishLegacyCCustodyTransition(ctx context.Context, collectionID string, now time.Time) error {
	if s == nil || s.db == nil || !workEventUUID.MatchString(collectionID) || now.IsZero() {
		return reviewbody.ErrInvalid
	}
	// Read the latest committed collection state after sourceLock. A serializable
	// snapshot established before waiting for the lock can be stale afterwards.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = requireSourceAdministrator(ctx, tx); err != nil {
		return err
	}
	var sourceID string
	if err = tx.QueryRowContext(ctx, `SELECT source_system_id::text FROM source_content_collections WHERE id=$1::uuid`, collectionID).Scan(&sourceID); err != nil {
		return err
	}
	if err = sourceLock(ctx, tx, sourceID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE connector_source_enrollments e SET legacy_c_copy_pending=false,status='active',updated_at=$1
 FROM source_content_collections c,source_systems s,connector_installations i
 WHERE c.id=e.content_collection_id AND s.id=c.source_system_id AND i.id=e.installation_id
 AND c.id=$2::uuid AND `+legacyCCustodyReadySQL, now.UTC(), collectionID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// SweepLegacyCCustodyTransitions is run by the existing owner controller,
// independently of UR enrollment availability. Candidate selection is only
// an optimization: Finish repeats the complete-page and copy checks while
// holding the same source lock as manifest commit and copy acceptance.
func (s *WorkspaceStore) SweepLegacyCCustodyTransitions(ctx context.Context, now time.Time, limit int) (int, error) {
	if s == nil || s.db == nil || now.IsZero() || limit < 1 || limit > 100 {
		return 0, reviewbody.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.content_collection_id::text FROM connector_source_enrollments e
 JOIN source_content_collections c ON c.id=e.content_collection_id
 JOIN source_systems s ON s.id=c.source_system_id
 JOIN connector_installations i ON i.id=e.installation_id
 WHERE `+legacyCCustodyReadySQL+`
 ORDER BY e.content_collection_id LIMIT $2`, now.UTC(), limit)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, err
	}
	for index, id := range ids {
		if err = s.FinishLegacyCCustodyTransition(ctx, id, now); err != nil {
			return index, err
		}
	}
	return len(ids), nil
}
