package db

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/workbench"
)

type ReviewBodyRead struct {
	ContentRef string
	Kind       string
	SHA256     string
	Bytes      []byte
	ExpiresAt  time.Time
}

// AuditReviewBodyRead records the attempted human content handoff without
// storing plaintext. A successful read must be audited before bytes leave.
func (s *WorkspaceStore) AuditReviewBodyRead(ctx context.Context, subject, contentRef, result string, now time.Time) error {
	if s == nil || s.db == nil || subject == "" || !reviewBodyRefPattern.MatchString(contentRef) ||
		(result != "accepted" && result != "denied") || now.IsZero() {
		return reviewbody.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertAccessAudit(ctx, tx, "human", subject, "review_body.read", "review_body", contentRef, result, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *WorkspaceStore) ReadReviewBody(ctx context.Context, subject, contentRef string, keys reviewbody.KeyRing, now time.Time) (ReviewBodyRead, error) {
	var result ReviewBodyRead
	if s == nil || s.db == nil || subject == "" || !reviewBodyRefPattern.MatchString(contentRef) || now.IsZero() {
		return result, reviewbody.ErrInvalid
	}
	var owner reviewbody.Identity
	var eventRef, eventHash, factID, factRevision, sourceHash sql.NullString
	var eventRevision sql.NullInt64
	var sealed reviewbody.Sealed
	err := s.db.QueryRowContext(ctx, `SELECT o.source_kind,o.source_system_id::text,o.source_instance,o.org_id,o.project_id,
 o.event_ref,o.event_revision,o.event_hash,o.source_fact_id,o.source_revision::text,o.source_sha256,
 o.content_kind,o.content_ref,o.byte_domain,o.policy_version,o.expires_at,o.digest,o.plain_bytes,
 b.key_id,b.nonce,b.ciphertext
 FROM review_body_operations o JOIN review_body_objects b ON b.content_ref=o.content_ref
 WHERE o.content_ref=$1 AND o.state='accepted' AND o.expires_at>$3 AND
 EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$2 AND g.active AND g.allow_content AND
   g.allowed_content_purposes='["human_review"]'::jsonb AND g.source_system_id=o.source_system_id AND
   g.cost_project_ref IS NULL AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||o.project_id))`,
		contentRef, subject, now.UTC()).Scan(&owner.SourceKind, &owner.SourceSystemID, &owner.SourceInstance,
		&owner.OrgID, &owner.ProjectID, &eventRef, &eventRevision, &eventHash, &factID, &factRevision, &sourceHash,
		&owner.ContentKind, &owner.ContentRef, &owner.ByteDomain, &owner.PolicyVersion, &owner.ExpiresAt,
		&sealed.SHA256, &sealed.PlainBytes, &sealed.KeyID, &sealed.Nonce, &sealed.Ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return result, workbench.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	owner.EventRef, owner.EventRevision, owner.EventHash = eventRef.String, eventRevision.Int64, eventHash.String
	owner.SourceFactID, owner.SourceRevision, owner.SourceSHA256 = factID.String, factRevision.String, sourceHash.String
	plain, err := keys.Open(owner, sealed)
	if err != nil {
		return result, err
	}
	if err = s.ReauthorizeReviewBodyRead(ctx, subject, contentRef, now); err != nil {
		return result, err
	}
	return ReviewBodyRead{ContentRef: contentRef, Kind: owner.ContentKind, SHA256: sealed.SHA256,
		Bytes: plain, ExpiresAt: owner.ExpiresAt}, nil
}

func (s *WorkspaceStore) ReauthorizeReviewBodyRead(ctx context.Context, subject, contentRef string, now time.Time) error {
	if s == nil || s.db == nil || subject == "" || !reviewBodyRefPattern.MatchString(contentRef) || now.IsZero() {
		return reviewbody.ErrInvalid
	}
	var allowed bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM review_body_operations o WHERE o.content_ref=$1 AND
 o.state='accepted' AND o.expires_at>$3 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE
 g.subject_id=$2 AND g.active AND g.allow_content AND g.allowed_content_purposes='["human_review"]'::jsonb AND
 g.source_system_id=o.source_system_id AND g.cost_project_ref IS NULL AND
 (g.source_project_ref IS NULL OR g.source_project_ref='project-'||o.project_id)))`, contentRef, subject, now.UTC()).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return workbench.ErrNotFound
	}
	return nil
}

// ExpireReviewBodies is owner-only maintenance. Reads stop at the fixed
// deadline even if this bounded sweep has not run yet.
func (s *SourceFactsStore) ReviewBodyExpiryAvailable(ctx context.Context) (bool, error) {
	if s == nil || s.db == nil {
		return false, reviewbody.ErrInvalid
	}
	var available bool
	err := s.db.QueryRowContext(ctx, `SELECT to_regclass('public.review_body_operations') IS NOT NULL AND
 to_regclass('public.review_body_objects') IS NOT NULL AND to_regclass('public.review_body_project_usage') IS NOT NULL`).Scan(&available)
	return available, err
}

func (s *SourceFactsStore) ExpireReviewBodies(ctx context.Context, now time.Time, limit int) (int, error) {
	if s == nil || s.db == nil || now.IsZero() || limit < 1 || limit > 100 {
		return 0, reviewbody.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var admin bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_has_role(current_user,relowner,'USAGE') FROM pg_class WHERE oid='public.review_body_objects'::regclass`).Scan(&admin); err != nil {
		return 0, err
	}
	if !admin {
		return 0, reviewbody.ErrScope
	}
	rows, err := tx.QueryContext(ctx, `SELECT content_ref,quota_scope
 FROM review_body_operations WHERE (state='accepted' AND expires_at<=$1) OR
 (state='reserved' AND lease_expires_at<=$1) ORDER BY expires_at,content_ref LIMIT $2`, now.UTC(), limit)
	if err != nil {
		return 0, err
	}
	type candidate struct{ ref, quotaScope string }
	items := []candidate{}
	for rows.Next() {
		var item candidate
		if err = rows.Scan(&item.ref, &item.quotaScope); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	// Ingress locks usage before operation. Keep the same order here, and
	// lock project usage keys in a stable order across concurrent sweepers.
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		ak, bk := a.quotaScope, b.quotaScope
		if ak == bk {
			return a.ref < b.ref
		}
		return ak < bk
	})
	processed := 0
	for _, item := range items {
		var held int
		if err = tx.QueryRowContext(ctx, `SELECT 1 FROM review_body_project_usage WHERE
 quota_scope=$1 FOR UPDATE`, item.quotaScope).Scan(&held); err != nil {
			return 0, err
		}
		var state string
		var charge int64
		var expires, lease sql.NullTime
		err = tx.QueryRowContext(ctx, `SELECT state,charged_bytes,expires_at,lease_expires_at FROM review_body_operations
 WHERE content_ref=$1 FOR UPDATE SKIP LOCKED`, item.ref).Scan(&state, &charge, &expires, &lease)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if state != "accepted" && state != "reserved" {
			continue
		}
		if state == "accepted" && (!expires.Valid || expires.Time.After(now)) {
			continue
		}
		if state == "reserved" && (!lease.Valid || lease.Time.After(now)) {
			continue
		}
		var update sql.Result
		if state == "accepted" {
			if _, err = tx.ExecContext(ctx, `DELETE FROM review_body_objects WHERE content_ref=$1`, item.ref); err != nil {
				return 0, err
			}
			if update, err = tx.ExecContext(ctx, `UPDATE review_body_project_usage SET stored_bytes=stored_bytes-$2 WHERE
 quota_scope=$1 AND stored_bytes>=$2`, item.quotaScope, charge); err != nil {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE review_body_operations SET state='expired',reason='retention_expired' WHERE content_ref=$1`, item.ref); err != nil {
				return 0, err
			}
		} else {
			if update, err = tx.ExecContext(ctx, `UPDATE review_body_project_usage SET reserved_bytes=reserved_bytes-$2 WHERE
 quota_scope=$1 AND reserved_bytes>=$2`, item.quotaScope, charge); err != nil {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE review_body_operations SET state='gap',reason='reservation_expired' WHERE content_ref=$1`, item.ref); err != nil {
				return 0, err
			}
		}
		updated, err := update.RowsAffected()
		if err != nil {
			return 0, err
		}
		if updated != 1 {
			return 0, reviewbody.ErrIntegrity
		}
		processed++
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return processed, nil
}
