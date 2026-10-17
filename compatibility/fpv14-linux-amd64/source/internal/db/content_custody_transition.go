package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/foigent/x-insight/internal/reviewbody"
)

// FinishLegacyCCustodyTransition shares the source lock with manifest commit
// and copy acceptance. An empty copy queue is insufficient: partial, policy,
// quota and integrity gaps must still prevent a premature custody switch.
func (s *WorkspaceStore) FinishLegacyCCustodyTransition(ctx context.Context, collectionID string, now time.Time) error {
	if s == nil || s.db == nil || !workEventUUID.MatchString(collectionID) || now.IsZero() {
		return reviewbody.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var sourceID string
	if err = tx.QueryRowContext(ctx, `SELECT source_system_id::text FROM source_content_collections WHERE id=$1::uuid`, collectionID).Scan(&sourceID); err != nil {
		return err
	}
	if err = sourceLock(ctx, tx, sourceID); err != nil {
		return err
	}
	var missing bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM source_content_manifests d WHERE d.collection_id=$1::uuid
 AND d.availability->>'state'='available' AND (d.source_json->>'expires_at')::timestamptz>$2
 AND d.source_json->'representation' IS NOT NULL AND d.source_json->'representation'<>'null'::jsonb
 AND NOT EXISTS(SELECT 1 FROM review_body_operations o JOIN review_body_objects b ON b.content_ref=o.content_ref
 WHERE o.content_collection_id=d.collection_id AND o.source_kind='ur_c' AND o.state='accepted' AND o.expires_at>$2
 AND ROW(o.source_system_id,o.source_instance,o.org_id,o.project_id,o.source_fact_id,o.source_revision,o.source_sha256,o.content_ref)
 =ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.source_fact_id,d.revision,d.source_sha256,d.source_json->>'content_ref')
 AND o.digest=d.source_json->'representation'->>'digest' AND o.plain_bytes=(d.source_json->'representation'->>'saved_bytes')::bigint
 AND o.byte_domain=d.source_json->'representation'->>'byte_domain'))`, collectionID, now.UTC()).Scan(&missing)
	if err != nil {
		return err
	}
	if missing {
		return tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `UPDATE connector_source_enrollments e SET legacy_c_copy_pending=false,status='active',updated_at=$2
 FROM source_content_collections c,source_systems s,connector_installations i
 WHERE e.content_collection_id=$1::uuid AND e.legacy_c_copy_pending AND e.status='requires_migration'
 AND c.id=e.content_collection_id AND s.id=c.source_system_id AND i.id=e.installation_id
 AND c.status='active' AND NOT c.requires_attention AND s.status='active'
 AND i.status='connected' AND i.desired_state='active'
 AND ROW(c.source_instance,c.org_id,c.project_id)=ROW(e.source_instance,e.org_id,e.project_id)`, collectionID, now.UTC())
	if err != nil {
		return err
	}
	return tx.Commit()
}
