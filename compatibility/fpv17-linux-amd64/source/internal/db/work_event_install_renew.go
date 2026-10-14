package db

import (
	"context"
	"database/sql"
	"regexp"
	"time"

	"github.com/foigent/x-insight/internal/workevent"
	"github.com/foigent/x-insight/internal/workspace"
)

var serviceIdentityPattern = regexp.MustCompile(`^xsi_[A-Za-z0-9_-]{22}$`)
var serviceTokenDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// The host owns the replacement bearer before requesting renewal. The owner
// stores only its digest and one old-to-new mapping, making lost responses
// repeatable without minting additional credentials.
func (s *SourceFactsStore) RenewWorkInstallCredential(ctx context.Context, identityID, oldDigest, replacementToken string,
	now time.Time) (time.Time, error) {
	if s == nil || s.db == nil || !serviceIdentityPattern.MatchString(identityID) ||
		!serviceTokenDigestPattern.MatchString(oldDigest) || now.IsZero() {
		return time.Time{}, workevent.ErrInvalid
	}
	newDigest, err := workspace.ServiceTokenDigest(replacementToken)
	if err != nil || newDigest == oldDigest {
		return time.Time{}, workevent.ErrInvalid
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback()
	var tokenExpiry time.Time
	var identityExpiry sql.NullTime
	var identityStatus string
	var priorOld, priorNew sql.NullString
	var priorExpiry sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT t.expires_at,i.expires_at,i.status,w.renewal_old_digest,w.renewal_new_digest,w.renewal_expires_at
FROM service_identity_tokens t JOIN service_identities i ON i.id=t.service_identity_id
JOIN work_event_install_invitations w ON w.approved_service_identity_id=i.id AND w.state='approved'
WHERE t.token_digest=$1 AND i.id=$2 AND t.revoked_at IS NULL AND i.permissions=ARRAY['work_event.ingest']::text[]
FOR UPDATE OF i,t,w`, oldDigest, identityID).Scan(&tokenExpiry, &identityExpiry, &identityStatus, &priorOld, &priorNew, &priorExpiry)
	if err != nil {
		return time.Time{}, workevent.ErrScopeChanged
	}
	if identityStatus != "active" || !now.Before(tokenExpiry) || !identityExpiry.Valid || !now.Before(identityExpiry.Time) {
		return time.Time{}, workevent.ErrScopeChanged
	}
	if priorOld.Valid && priorOld.String == oldDigest {
		if priorNew.String != newDigest || !priorExpiry.Valid {
			return time.Time{}, workevent.ErrConflict
		}
		return priorExpiry.Time.UTC(), nil
	}
	if tokenExpiry.After(now.Add(31 * 24 * time.Hour)) {
		return time.Time{}, workevent.ErrScopeChanged
	}
	expiry := now.Add(90 * 24 * time.Hour)
	if _, err = tx.ExecContext(ctx, `UPDATE service_identities SET expires_at=$2,updated_at=$3 WHERE id=$1 AND status='active'`,
		identityID, expiry, now); err != nil {
		return time.Time{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO service_identity_tokens(token_digest,service_identity_id,token_prefix,created_at,expires_at)
VALUES($1,$2,$3,$4,$5)`, newDigest, identityID, replacementToken[:12], now, expiry); err != nil {
		return time.Time{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE work_event_install_invitations SET renewal_old_digest=$2,
renewal_new_digest=$3,renewal_expires_at=$4,updated_at=$5 WHERE approved_service_identity_id=$1`,
		identityID, oldDigest, newDigest, expiry, now); err != nil {
		return time.Time{}, err
	}
	if err = insertAccessAudit(ctx, tx, "service", identityID, "work_install_credential.renew", "service_identity", identityID,
		"accepted", now); err != nil {
		return time.Time{}, err
	}
	if err = tx.Commit(); err != nil {
		return time.Time{}, err
	}
	return expiry, nil
}
