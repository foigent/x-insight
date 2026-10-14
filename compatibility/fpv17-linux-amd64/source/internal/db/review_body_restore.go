package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/reviewbody"
)

// ValidateReviewBodyRestore verifies every live accepted object with its stored
// authenticated identity before the restored database is activated. It neither
// publishes plaintext nor rewrites objects/keys. Expired bodies remain expired.
func ValidateReviewBodyRestore(ctx context.Context, database *sql.DB, keys reviewbody.KeyRing, now time.Time) error {
	if database == nil || now.IsZero() {
		return reviewbody.ErrInvalid
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return fmt.Errorf("review custody restore inspection is unavailable")
	}
	defer tx.Rollback()
	var present bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('public.review_body_operations') IS NOT NULL`).Scan(&present); err != nil {
		return fmt.Errorf("review custody restore inspection is unavailable")
	}
	if !present {
		return tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT o.source_kind,o.source_system_id::text,o.source_instance,o.org_id,o.project_id,
 o.event_ref,o.event_revision,o.event_hash,o.source_fact_id,o.source_revision::text,o.source_sha256,
 o.content_kind,o.content_ref,o.byte_domain,o.policy_version,o.expires_at,o.digest,o.plain_bytes,
 b.key_id,b.nonce,b.ciphertext FROM review_body_operations o
 LEFT JOIN review_body_objects b ON b.content_ref=o.content_ref
 WHERE o.state='accepted' AND o.expires_at>$1 ORDER BY o.content_ref`, now.UTC())
	if err != nil {
		return fmt.Errorf("review custody restore objects are unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var owner reviewbody.Identity
		var eventRef, eventHash, factID, factRevision, sourceHash sql.NullString
		var eventRevision sql.NullInt64
		var sealed reviewbody.Sealed
		if err := rows.Scan(&owner.SourceKind, &owner.SourceSystemID, &owner.SourceInstance, &owner.OrgID, &owner.ProjectID,
			&eventRef, &eventRevision, &eventHash, &factID, &factRevision, &sourceHash,
			&owner.ContentKind, &owner.ContentRef, &owner.ByteDomain, &owner.PolicyVersion, &owner.ExpiresAt,
			&sealed.SHA256, &sealed.PlainBytes, &sealed.KeyID, &sealed.Nonce, &sealed.Ciphertext); err != nil {
			return fmt.Errorf("review custody restore object metadata or ciphertext is invalid")
		}
		owner.EventRef, owner.EventRevision, owner.EventHash = eventRef.String, eventRevision.Int64, eventHash.String
		owner.SourceFactID, owner.SourceRevision, owner.SourceSHA256 = factID.String, factRevision.String, sourceHash.String
		plain, err := keys.Open(owner, sealed)
		if err != nil {
			return fmt.Errorf("review custody restore decryption failed: %w", err)
		}
		clear(plain)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("review custody restore scan is incomplete")
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("review custody restore scan is incomplete")
	}
	return tx.Commit()
}
