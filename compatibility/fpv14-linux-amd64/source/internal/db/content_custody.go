package db

import (
	"context"
	"database/sql"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func managedContentCustody(ctx context.Context, q workspaceQueryer, collectionID string) (bool, error) {
	var present bool
	if e := q.QueryRowContext(ctx, `SELECT to_regclass('public.connector_source_enrollments') IS NOT NULL`).Scan(&present); e != nil {
		return false, e
	}
	if !present {
		return false, nil
	}
	var managed bool
	e := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM connector_source_enrollments WHERE content_collection_id=$1::uuid AND NOT legacy_c_copy_pending)`, collectionID).Scan(&managed)
	return managed, e
}
func contentCustodyDeadline(ctx context.Context, q workspaceQueryer, collectionID string, r sourcefacts.FrozenManifestRef) (sql.NullTime, error) {
	var expiry sql.NullTime
	e := q.QueryRowContext(ctx, `SELECT max(expires_at) FROM review_body_operations WHERE content_collection_id=$1::uuid AND source_kind='ur_c' AND state='accepted'
 AND source_system_id=$2::uuid AND source_instance=$3 AND org_id=$4 AND project_id=$5 AND source_fact_id=$6 AND source_revision::text=$7 AND source_sha256=$8 AND content_ref=$9`, collectionID, r.SourceSystemID, r.SourceInstance, r.OrgID, r.ProjectID, r.SourceFactID, r.Revision, r.SourceSHA256, r.ContentRef).Scan(&expiry)
	return expiry, e
}
