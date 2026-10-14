package hostctl

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type ControlCredentialStatus struct {
	InstallationID        string     `json:"installation_id"`
	CheckedAt             time.Time  `json:"checked_at"`
	SourceBindingSHA256   string     `json:"source_binding_sha256"`
	IdentityStatus        string     `json:"identity_status"`
	IdentityExpiresAt     *time.Time `json:"identity_expires_at,omitempty"`
	CurrentTokenExpiresAt *time.Time `json:"current_token_expires_at,omitempty"`
	PendingOperationID    string     `json:"pending_operation_id,omitempty"`
	PendingExpiresAt      *time.Time `json:"pending_expires_at,omitempty"`
	IngressState          string     `json:"ingress_state"`
	Reason                string     `json:"reason,omitempty"`
}

// InspectControlCredentialLifecycle is the status half of the upgrade check.
// Expiry is an operational recovery need, not a release blocker.
func (manager PostgresManager) InspectControlCredentialLifecycle(ctx context.Context, credentials DatabaseCredentials, now time.Time) ([]ControlCredentialStatus, error) {
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	var managed, sourceBinding, lifecycle bool
	if err = database.QueryRowContext(ctx, `SELECT to_regclass('public.connector_installations') IS NOT NULL,
to_regclass('public.connector_installation_source_bindings') IS NOT NULL,
to_regclass('public.connector_control_credentials') IS NOT NULL`).Scan(&managed, &sourceBinding, &lifecycle); err != nil {
		return nil, err
	}
	if !managed || !sourceBinding {
		return nil, nil
	}
	query := `SELECT c.id,b.source_binding_sha256,i.status,i.expires_at,t.expires_at,t.revoked_at IS NOT NULL,
''::text,NULL::timestamptz
FROM connector_installations c JOIN service_identities i ON i.id=c.control_service_identity_id
JOIN connector_installation_source_bindings b ON b.installation_id=c.id
LEFT JOIN LATERAL (SELECT min(token_digest) AS digest FROM service_identity_tokens WHERE service_identity_id=i.id) first ON true
LEFT JOIN service_identity_tokens t ON t.token_digest=first.digest
WHERE c.setup_request_id IS NOT NULL AND c.control_service_identity_id IS NOT NULL ORDER BY c.id`
	if lifecycle {
		query = `SELECT c.id,b.source_binding_sha256,i.status,i.expires_at,t.expires_at,t.revoked_at IS NOT NULL,
COALESCE(op.operation_id,''),op.replacement_expires_at
FROM connector_installations c JOIN service_identities i ON i.id=c.control_service_identity_id
JOIN connector_installation_source_bindings b ON b.installation_id=c.id
LEFT JOIN connector_control_credentials cc ON cc.installation_id=c.id
LEFT JOIN service_identity_tokens t ON t.token_digest=cc.current_token_digest
LEFT JOIN connector_control_credential_operations op ON op.installation_id=c.id AND op.state='prepared'
WHERE c.setup_request_id IS NOT NULL AND c.control_service_identity_id IS NOT NULL ORDER BY c.id`
	}
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	statuses := []ControlCredentialStatus{}
	for rows.Next() {
		var status ControlCredentialStatus
		var identityExpiry, tokenExpiry, pendingExpiry sql.NullTime
		var revoked sql.NullBool
		if err = rows.Scan(&status.InstallationID, &status.SourceBindingSHA256, &status.IdentityStatus, &identityExpiry, &tokenExpiry, &revoked, &status.PendingOperationID, &pendingExpiry); err != nil {
			return nil, err
		}
		status.CheckedAt = now.UTC().Truncate(time.Microsecond)
		if identityExpiry.Valid {
			v := identityExpiry.Time.UTC()
			status.IdentityExpiresAt = &v
		}
		if tokenExpiry.Valid {
			v := tokenExpiry.Time.UTC()
			status.CurrentTokenExpiresAt = &v
		}
		if pendingExpiry.Valid {
			v := pendingExpiry.Time.UTC()
			status.PendingExpiresAt = &v
		}
		switch {
		case status.IdentityStatus == "revoked" || revoked.Valid && revoked.Bool:
			status.IngressState = "blocked"
			status.Reason = "control_credential_revoked"
		case !tokenExpiry.Valid || !identityExpiry.Valid:
			status.IngressState = "blocked"
			status.Reason = "control_credential_unavailable"
		case !tokenExpiry.Time.After(now) || !identityExpiry.Time.After(now) || status.IdentityStatus == "expired":
			if status.PendingOperationID != "" && pendingExpiry.Valid && pendingExpiry.Time.After(now) && identityExpiry.Time.After(now) {
				status.IngressState = "pending_continuable"
				status.Reason = "current_expired_pending_valid"
			} else {
				status.IngressState = "recovery_required"
				status.Reason = "control_credential_expired"
			}
		case status.PendingOperationID != "":
			status.IngressState = "rotation_pending"
		default:
			status.IngressState = "ready"
		}
		statuses = append(statuses, status)
	}
	return statuses, rows.Err()
}

// VerifyControlCredentialLifecycle checks that every managed installation can
// acquire exactly one original current token in schema 45. It is read-only and
// runs before either --check success or the upgrade journal starts.
func (manager PostgresManager) VerifyControlCredentialLifecycle(ctx context.Context, credentials DatabaseCredentials) error {
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return fmt.Errorf("control credential preflight unavailable")
	}
	defer database.Close()
	var managed, sourceBinding bool
	if err = database.QueryRowContext(ctx, `SELECT to_regclass('public.connector_installations') IS NOT NULL,
to_regclass('public.connector_installation_source_bindings') IS NOT NULL`).Scan(&managed, &sourceBinding); err != nil {
		return err
	}
	if !managed || !sourceBinding {
		return nil
	}
	var lifecycle bool
	if err = database.QueryRowContext(ctx, `SELECT to_regclass('public.connector_control_credentials') IS NOT NULL`).Scan(&lifecycle); err != nil {
		return err
	}
	var unsafe int
	predecessors := `SELECT count(*) FROM (
 SELECT c.id FROM connector_installations c
 LEFT JOIN connector_installation_source_bindings b ON b.installation_id=c.id
 LEFT JOIN connector_installation_invitations inv ON inv.installation_id=c.id
  AND inv.status='consumed' AND inv.acknowledgement_sha256=c.installation_acknowledgement_sha256
 LEFT JOIN service_identity_tokens t ON t.service_identity_id=c.control_service_identity_id
 WHERE c.setup_request_id IS NOT NULL AND c.control_service_identity_id IS NOT NULL`
	if lifecycle {
		predecessors += ` AND NOT EXISTS(SELECT 1 FROM connector_control_credentials cc WHERE cc.installation_id=c.id)`
	}
	predecessors += `
 GROUP BY c.id,b.installation_id,inv.id
 HAVING b.installation_id IS NULL OR inv.id IS NULL OR count(DISTINCT t.token_digest)<>1
) unsafe_installations`
	err = database.QueryRowContext(ctx, predecessors).Scan(&unsafe)
	if err != nil {
		return fmt.Errorf("control credential preflight unavailable: %w", err)
	}
	if unsafe != 0 {
		return fmt.Errorf("control credential preflight found %d managed installations without a unique original token/binding", unsafe)
	}
	if lifecycle {
		err = database.QueryRowContext(ctx, `SELECT count(*) FROM connector_control_credentials cc
 JOIN connector_installations c ON c.id=cc.installation_id
 LEFT JOIN service_identity_tokens t ON t.token_digest=cc.current_token_digest AND t.service_identity_id=cc.service_identity_id
 WHERE cc.service_identity_id<>c.control_service_identity_id OR t.token_digest IS NULL`).Scan(&unsafe)
		if err != nil {
			return err
		}
		if unsafe != 0 {
			return fmt.Errorf("control credential preflight found %d invalid current pointers", unsafe)
		}
		err = database.QueryRowContext(ctx, `SELECT count(*) FROM connector_control_credential_operations op
 JOIN connector_control_credentials cc ON cc.installation_id=op.installation_id
 LEFT JOIN service_identity_tokens t ON t.token_digest=op.replacement_token_sha256 AND t.service_identity_id=cc.service_identity_id
 WHERE op.state='prepared' AND (op.previous_token_sha256<>cc.current_token_digest OR t.token_digest IS NULL OR t.revoked_at IS NOT NULL OR t.expires_at<>op.replacement_expires_at)`).Scan(&unsafe)
		if err != nil {
			return err
		}
		if unsafe != 0 {
			return fmt.Errorf("control credential preflight found %d invalid pending operations", unsafe)
		}
	}
	return nil
}
