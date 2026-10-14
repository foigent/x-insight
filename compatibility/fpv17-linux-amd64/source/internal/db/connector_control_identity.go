package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// managedControlIdentityIssue is the shared poll/ack eligibility rule for
// managed control operations and read-only setup diagnostics. It deliberately
// does not infer which bearer token a remote host currently holds.
func managedControlIdentityIssue(ctx context.Context, q workspaceQueryer, identityID string, now time.Time) (string, error) {
	var status string
	var expires sql.NullTime
	var permitted bool
	err := q.QueryRowContext(ctx, `SELECT status,expires_at,permissions @> ARRAY['connector.control.poll','connector.control.ack']::text[] FROM service_identities WHERE id=$1`, identityID).Scan(&status, &expires, &permitted)
	if errors.Is(err, sql.ErrNoRows) {
		return "control_identity_unavailable", nil
	}
	if err != nil {
		return "", err
	}
	if status == "revoked" {
		return "control_identity_revoked", nil
	}
	if status == "expired" || (expires.Valid && !expires.Time.After(now.UTC())) {
		return "control_identity_expired", nil
	}
	if status != "active" || !expires.Valid || !permitted {
		return "control_identity_unavailable", nil
	}
	return "", nil
}

func managedControlCredentialIssue(ctx context.Context, q workspaceQueryer, installationID, identityID string, now time.Time) (string, error) {
	var available bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('public.connector_control_credentials') IS NOT NULL`).Scan(&available); err != nil {
		return "", err
	}
	if !available {
		return "", nil
	}
	var currentExpiry sql.NullTime
	var currentRevoked sql.NullTime
	err := q.QueryRowContext(ctx, `SELECT t.expires_at,t.revoked_at FROM connector_control_credentials cc
JOIN service_identity_tokens t ON t.token_digest=cc.current_token_digest AND t.service_identity_id=cc.service_identity_id
WHERE cc.installation_id=$1 AND cc.service_identity_id=$2`, installationID, identityID).Scan(&currentExpiry, &currentRevoked)
	if errors.Is(err, sql.ErrNoRows) {
		return "control_credential_unavailable", nil
	}
	if err != nil {
		return "", err
	}
	if currentRevoked.Valid {
		return "control_credential_revoked", nil
	}
	var pendingExpiry sql.NullTime
	var pendingRevoked sql.NullTime
	err = q.QueryRowContext(ctx, `SELECT t.expires_at,t.revoked_at FROM connector_control_credential_operations op
JOIN service_identity_tokens t ON t.token_digest=op.replacement_token_sha256
WHERE op.installation_id=$1 AND op.state='prepared'`, installationID).Scan(&pendingExpiry, &pendingRevoked)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	pending := err == nil
	if !currentExpiry.Valid || !currentExpiry.Time.After(now) {
		if pending && pendingExpiry.Valid && pendingExpiry.Time.After(now) && !pendingRevoked.Valid {
			return "control_credential_pending", nil
		}
		return "control_credential_expired", nil
	}
	if pending {
		if pendingRevoked.Valid {
			return "control_credential_unavailable", nil
		}
		return "control_credential_pending", nil
	}
	return "", nil
}
