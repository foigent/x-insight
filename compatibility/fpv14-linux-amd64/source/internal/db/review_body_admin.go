package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/reviewbody"
)

// GrantReviewBodyIngestBinding is deliberately separate from metadata ingest.
// An existing work_event.ingest identity cannot upload bodies until an owner
// explicitly records this exact active identity+collection binding.
func (s *SourceFactsStore) GrantReviewBodyIngestBinding(ctx context.Context, serviceIdentityID, collectionID string, now time.Time) error {
	if s == nil || s.db == nil || serviceIdentityID == "" || !workEventUUID.MatchString(collectionID) || now.IsZero() {
		return reviewbody.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var admin bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_has_role(current_user,relowner,'USAGE') FROM pg_class WHERE oid='public.review_body_ingest_bindings'::regclass`).Scan(&admin); err != nil {
		return err
	}
	if !admin {
		return reviewbody.ErrScope
	}
	var sourceID, status string
	if err = tx.QueryRowContext(ctx, `SELECT c.source_system_id::text FROM work_event_collections c WHERE c.id=$1::uuid`, collectionID).Scan(&sourceID); err != nil {
		return err
	}
	if err = sourceLock(ctx, tx, sourceID); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT c.status FROM work_event_collections c JOIN work_event_ingest_bindings b ON b.collection_id=c.id
 WHERE b.service_identity_id=$1 AND c.id=$2::uuid AND c.source_system_id=$3::uuid AND b.status='active' FOR SHARE OF b,c`,
		serviceIdentityID, collectionID, sourceID).Scan(&status); err != nil {
		return err
	}
	if status != "active" {
		return reviewbody.ErrScope
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT status FROM review_body_ingest_bindings WHERE service_identity_id=$1 AND collection_id=$2::uuid FOR UPDATE`, serviceIdentityID, collectionID).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO review_body_ingest_bindings(service_identity_id,collection_id,status,revision) VALUES($1,$2::uuid,'active',1)`, serviceIdentityID, collectionID)
	} else if err == nil && existing == "active" {
		// Exact replay is an audit-safe no-op.
	} else if err == nil {
		return reviewbody.ErrConflict
	}
	if err != nil {
		return err
	}
	if err = insertAccessAudit(ctx, tx, "root", "root", "review_body.binding.grant", "work_event_collection", collectionID, "accepted", now.UTC()); err != nil {
		return err
	}
	return tx.Commit()
}
