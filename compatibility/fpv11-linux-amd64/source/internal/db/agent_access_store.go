package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/workspace"
)

func (s *WorkspaceStore) CreateServiceIdentity(
	ctx context.Context,
	actor workspace.Actor,
	input workspace.CreateServiceIdentityInput,
	now time.Time,
	random io.Reader,
) (workspace.ServiceCredential, error) {
	if s == nil || s.db == nil {
		return workspace.ServiceCredential{}, fmt.Errorf("workspace store is not initialized")
	}
	if actor.Kind != "root" || actor.Ref != "root" ||
		workspace.ValidateCreateServiceIdentity(input, now.UTC()) != nil {
		return workspace.ServiceCredential{}, workspace.ErrInvalid
	}
	if random == nil {
		random = rand.Reader
	}
	identityID, err := workspace.NewID("xsi_", random)
	if err != nil {
		return workspace.ServiceCredential{}, err
	}
	token, digest, err := workspace.NewServiceToken(random)
	if err != nil {
		return workspace.ServiceCredential{}, err
	}
	now = now.UTC()
	expiresAt := input.ExpiresAt.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.ServiceCredential{}, err
	}
	defer tx.Rollback()
	permissions := []string{"evidence.read", "evidence.cite"}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO service_identities (
  id, display_name, status, permissions, created_by_subject,
  created_at, updated_at, expires_at
) VALUES ($1, $2, 'active', $3, 'root', $4, $4, $5)`,
		identityID, input.DisplayName, permissions, now, expiresAt); err != nil {
		return workspace.ServiceCredential{}, err
	}
	prefix := token[:12]
	if _, err := tx.ExecContext(ctx, `
INSERT INTO service_identity_tokens (
  token_digest, service_identity_id, token_prefix, created_at, expires_at
) VALUES ($1, $2, $3, $4, $5)`,
		digest, identityID, prefix, now, expiresAt); err != nil {
		return workspace.ServiceCredential{}, err
	}
	if err := insertAccessAudit(
		ctx, tx, "root", actor.Ref, "service_identity.create",
		"service_identity", identityID, "accepted", now,
	); err != nil {
		return workspace.ServiceCredential{}, err
	}
	if err := tx.Commit(); err != nil {
		return workspace.ServiceCredential{}, err
	}
	expires := expiresAt
	return workspace.ServiceCredential{
		ServiceIdentity: workspace.ServiceIdentity{
			ID: identityID, DisplayName: input.DisplayName, Status: "active",
			Permissions: permissions, CreatedBySubject: "root", CreatedAt: now,
			UpdatedAt: now, ExpiresAt: &expires,
		},
		Token: token, TokenPrefix: prefix, ExpiresAt: expiresAt,
	}, nil
}

func (s *WorkspaceStore) AuthenticateServiceToken(
	ctx context.Context,
	token string,
	now time.Time,
) (workspace.ServiceAuthentication, error) {
	if s == nil || s.db == nil {
		return workspace.ServiceAuthentication{}, fmt.Errorf("workspace store is not initialized")
	}
	digest, err := workspace.ServiceTokenDigest(token)
	if err != nil {
		return workspace.ServiceAuthentication{}, workspace.ErrCredentialInvalid
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.ServiceAuthentication{}, err
	}
	defer tx.Rollback()
	identity, tokenExpiresAt, tokenRevokedAt, err := queryServiceIdentityByDigest(ctx, tx, digest)
	if errors.Is(err, sql.ErrNoRows) {
		return workspace.ServiceAuthentication{}, workspace.ErrCredentialInvalid
	}
	if err != nil {
		return workspace.ServiceAuthentication{}, err
	}
	switch {
	case identity.Status == "revoked" || tokenRevokedAt != nil:
		_ = insertAccessAudit(ctx, tx, "service", identity.ID, "service_identity.authenticate", "read_api", "evidence", "revoked", now)
		_ = tx.Commit()
		return workspace.ServiceAuthentication{}, workspace.ErrCredentialRevoked
	case !now.Before(tokenExpiresAt) || (identity.ExpiresAt != nil && !now.Before(*identity.ExpiresAt)):
		// A rotated token can expire while its identity and replacement token
		// remain active. Only the identity's own expiry is terminal.
		if identity.Status == "active" && identity.ExpiresAt != nil && !now.Before(*identity.ExpiresAt) {
			_, _ = tx.ExecContext(ctx, `
UPDATE service_identities
SET status = 'expired', updated_at = $2
WHERE id = $1 AND status = 'active'`, identity.ID, now)
		}
		_ = insertAccessAudit(ctx, tx, "service", identity.ID, "service_identity.authenticate", "read_api", "evidence", "expired", now)
		_ = tx.Commit()
		return workspace.ServiceAuthentication{}, workspace.ErrCredentialExpired
	case identity.Status != "active":
		return workspace.ServiceAuthentication{}, workspace.ErrCredentialInvalid
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE service_identity_tokens SET last_used_at = $2 WHERE token_digest = $1`,
		digest, now); err != nil {
		return workspace.ServiceAuthentication{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE service_identities SET last_used_at = $2, updated_at = $2 WHERE id = $1`,
		identity.ID, now); err != nil {
		return workspace.ServiceAuthentication{}, err
	}
	if err := insertAccessAudit(
		ctx, tx, "service", identity.ID, "service_identity.authenticate",
		"read_api", "evidence", "accepted", now,
	); err != nil {
		return workspace.ServiceAuthentication{}, err
	}
	if err := tx.Commit(); err != nil {
		return workspace.ServiceAuthentication{}, err
	}
	identity.LastUsedAt = timePointer(now)
	identity.UpdatedAt = now
	return workspace.ServiceAuthentication{Identity: identity, TokenDigest: digest}, nil
}

func (s *WorkspaceStore) ListServiceIdentities(ctx context.Context, limit int) ([]workspace.ServiceIdentity, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("workspace store is not initialized")
	}
	if limit < 1 || limit > 100 {
		return nil, workspace.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, display_name, status, array_to_json(permissions)::text,
       created_by_subject, created_at,
       updated_at, expires_at, revoked_at, revoke_reason, last_used_at
FROM service_identities
ORDER BY created_at DESC, id
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]workspace.ServiceIdentity, 0)
	for rows.Next() {
		item, err := scanServiceIdentity(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *WorkspaceStore) RevokeServiceIdentity(
	ctx context.Context,
	id string,
	actor workspace.Actor,
	reason string,
	now time.Time,
) (workspace.ServiceIdentity, error) {
	if s == nil || s.db == nil {
		return workspace.ServiceIdentity{}, fmt.Errorf("workspace store is not initialized")
	}
	if (actor.Kind != "root" && actor.Kind != "human") ||
		len(strings.TrimSpace(reason)) == 0 || len(reason) > 512 {
		return workspace.ServiceIdentity{}, workspace.ErrInvalid
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.ServiceIdentity{}, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `
UPDATE service_identities
SET status = 'revoked', updated_at = $2, revoked_at = $2, revoke_reason = $3
WHERE id = $1 AND status = 'active'
RETURNING id, display_name, status, array_to_json(permissions)::text,
          created_by_subject, created_at,
          updated_at, expires_at, revoked_at, revoke_reason, last_used_at`,
		id, now, reason)
	item, err := scanServiceIdentity(row)
	if errors.Is(err, sql.ErrNoRows) {
		var status string
		lookupErr := tx.QueryRowContext(ctx, `SELECT status FROM service_identities WHERE id = $1`, id).Scan(&status)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return workspace.ServiceIdentity{}, workspace.ErrNotFound
		}
		if lookupErr != nil {
			return workspace.ServiceIdentity{}, lookupErr
		}
		if status == "revoked" {
			return workspace.ServiceIdentity{}, workspace.ErrCredentialRevoked
		}
		return workspace.ServiceIdentity{}, workspace.ErrCredentialExpired
	}
	if err != nil {
		return workspace.ServiceIdentity{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE service_identity_tokens
SET revoked_at = $2
WHERE service_identity_id = $1 AND revoked_at IS NULL`,
		id, now); err != nil {
		return workspace.ServiceIdentity{}, err
	}
	if err := insertAccessAudit(
		ctx, tx, actor.Kind, actor.Ref, "service_identity.revoke",
		"service_identity", id, "revoked", now,
	); err != nil {
		return workspace.ServiceIdentity{}, err
	}
	if err := tx.Commit(); err != nil {
		return workspace.ServiceIdentity{}, err
	}
	return item, nil
}

func queryServiceIdentityByDigest(
	ctx context.Context,
	tx *sql.Tx,
	digest string,
) (workspace.ServiceIdentity, time.Time, *time.Time, error) {
	return queryServiceIdentityByDigestLock(ctx, tx, digest, false)
}

func queryServiceIdentityByDigestLock(
	ctx context.Context,
	tx *sql.Tx,
	digest string,
	shared bool,
) (workspace.ServiceIdentity, time.Time, *time.Time, error) {
	query := `
SELECT i.id, i.display_name, i.status, array_to_json(i.permissions)::text,
       i.created_by_subject,
       i.created_at, i.updated_at, i.expires_at, i.revoked_at, i.revoke_reason,
       i.last_used_at, t.expires_at, t.revoked_at
FROM service_identity_tokens t
JOIN service_identities i ON i.id = t.service_identity_id
WHERE t.token_digest = $1`
	if shared {
		query += ` FOR SHARE OF i, t`
	} else {
		query += ` FOR UPDATE OF i, t`
	}
	row := tx.QueryRowContext(ctx, query, digest)
	var (
		item            workspace.ServiceIdentity
		identityExpires sql.NullTime
		revokedAt       sql.NullTime
		revokeReason    sql.NullString
		lastUsedAt      sql.NullTime
		tokenExpires    time.Time
		tokenRevoked    sql.NullTime
		permissions     string
	)
	err := row.Scan(
		&item.ID, &item.DisplayName, &item.Status, &permissions,
		&item.CreatedBySubject, &item.CreatedAt, &item.UpdatedAt,
		&identityExpires, &revokedAt, &revokeReason, &lastUsedAt,
		&tokenExpires, &tokenRevoked,
	)
	if err != nil {
		return workspace.ServiceIdentity{}, time.Time{}, nil, err
	}
	item.ExpiresAt = nullTimePointer(identityExpires)
	item.RevokedAt = nullTimePointer(revokedAt)
	item.RevokeReason = revokeReason.String
	item.LastUsedAt = nullTimePointer(lastUsedAt)
	item.Permissions = decodeJSONStringSlice(permissions)
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, tokenExpires.UTC(), nullTimePointer(tokenRevoked), nil
}

func scanServiceIdentity(scanner rowScanner) (workspace.ServiceIdentity, error) {
	var (
		item         workspace.ServiceIdentity
		expiresAt    sql.NullTime
		revokedAt    sql.NullTime
		revokeReason sql.NullString
		lastUsedAt   sql.NullTime
		permissions  string
	)
	err := scanner.Scan(
		&item.ID, &item.DisplayName, &item.Status, &permissions,
		&item.CreatedBySubject, &item.CreatedAt, &item.UpdatedAt,
		&expiresAt, &revokedAt, &revokeReason, &lastUsedAt,
	)
	if err != nil {
		return workspace.ServiceIdentity{}, err
	}
	item.ExpiresAt = nullTimePointer(expiresAt)
	item.RevokedAt = nullTimePointer(revokedAt)
	item.RevokeReason = revokeReason.String
	item.LastUsedAt = nullTimePointer(lastUsedAt)
	item.Permissions = decodeJSONStringSlice(permissions)
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, nil
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func insertAccessAudit(
	ctx context.Context,
	tx *sql.Tx,
	actorKind string,
	actorRef string,
	action string,
	resourceKind string,
	resourceRef string,
	result string,
	now time.Time,
) error {
	id, err := workspace.NewID("xaa_", rand.Reader)
	if err != nil {
		return err
	}
	correlation := "xa_" + strings.TrimPrefix(id, "xaa_")
	_, err = tx.ExecContext(ctx, `
INSERT INTO access_audit_records (
  id, occurred_at, actor_kind, actor_ref, action, resource_kind,
  resource_ref, result, correlation_id, details
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '{}'::jsonb)`,
		id, now.UTC(), actorKind, actorRef, action, resourceKind,
		resourceRef, result, correlation)
	return err
}
