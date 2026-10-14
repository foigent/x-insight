package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
)

type controlCredentialBoundary struct {
	installation     connectorcontrol.Installation
	identityID       string
	identityStatus   string
	identityExpires  sql.NullTime
	permissionsOK    bool
	currentDigest    string
	sourceBindingSHA string
	xiOrigin         string
	urOrigin         string
}

// The installation row is always the first lock in credential mutations.
// Authentication locks the same identity/token rows before expiry marking.
func (s *WorkspaceStore) lockControlCredentialBoundary(ctx context.Context, tx *sql.Tx, id string) (controlCredentialBoundary, error) {
	return s.controlCredentialBoundaryTx(ctx, tx, id, true)
}

func (s *WorkspaceStore) readControlCredentialBoundary(ctx context.Context, tx *sql.Tx, id string) (controlCredentialBoundary, error) {
	return s.controlCredentialBoundaryTx(ctx, tx, id, false)
}

func (s *WorkspaceStore) controlCredentialBoundaryTx(ctx context.Context, tx *sql.Tx, id string, lock bool) (controlCredentialBoundary, error) {
	var b controlCredentialBoundary
	if !connectorcontrol.IsInstallationID(id) {
		return b, connectorcontrol.ErrInvalid
	}
	var locked string
	installationQuery := `SELECT id FROM connector_installations WHERE id=$1`
	if lock {
		installationQuery += ` FOR UPDATE`
	}
	if err := tx.QueryRowContext(ctx, installationQuery, id).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return b, connectorcontrol.ErrNotFound
		}
		return b, err
	}
	i, err := loadConnectorInstallation(ctx, tx, id)
	if err != nil {
		return b, err
	}
	if s.connectorDeploymentID != "" && i.DeploymentID != s.connectorDeploymentID {
		return b, connectorcontrol.ErrConflict
	}
	b.installation = i
	b.identityID = i.ControlServiceIdentityID
	if b.identityID == "" || i.InstallationAcknowledgementSHA256 == "" || i.ProjectID == "" ||
		len(i.FinalEventScope) != 1 || i.FinalEventScope[0] != connectorcontrol.CompletedEventType ||
		i.DesiredState != "active" || i.Status == "paused" || i.Status == "disconnected" {
		return b, connectorcontrol.ErrConflict
	}
	err = tx.QueryRowContext(ctx, `SELECT b.source_binding_sha256,inv.xinsight_origin,inv.unirouter_install_origin
FROM connector_installation_source_bindings b
JOIN connector_installation_invitations inv ON inv.installation_id=b.installation_id
 AND inv.status='consumed' AND inv.acknowledgement_sha256=$2
WHERE b.installation_id=$1`, id, i.InstallationAcknowledgementSHA256).Scan(&b.sourceBindingSHA, &b.xiOrigin, &b.urOrigin)
	if err != nil || !connectorcontrol.IsCanonicalHTTPSOrigin(b.xiOrigin) || !connectorcontrol.IsCanonicalHTTPSOrigin(b.urOrigin) {
		return b, connectorcontrol.ErrConflict
	}
	identityQuery := `SELECT status,expires_at,permissions @> ARRAY['connector.control.poll','connector.control.ack']::text[]
FROM service_identities WHERE id=$1`
	if lock {
		identityQuery += ` FOR UPDATE`
	}
	err = tx.QueryRowContext(ctx, identityQuery, b.identityID).Scan(&b.identityStatus, &b.identityExpires, &b.permissionsOK)
	if err != nil || !b.permissionsOK || b.identityStatus == "revoked" {
		return b, connectorcontrol.ErrConflict
	}
	currentQuery := `SELECT current_token_digest FROM connector_control_credentials
WHERE installation_id=$1 AND service_identity_id=$2`
	if lock {
		currentQuery += ` FOR UPDATE`
	}
	err = tx.QueryRowContext(ctx, currentQuery, id, b.identityID).Scan(&b.currentDigest)
	if err != nil {
		return b, connectorcontrol.ErrConflict
	}
	return b, nil
}

func (b controlCredentialBoundary) matchesRequest(r connectorcontrol.CredentialRequest) bool {
	return r.InstallationID == b.installation.InstallationID &&
		r.DeploymentID == b.installation.DeploymentID &&
		r.ProjectID == b.installation.ProjectID &&
		len(r.FinalEventScope) == 1 && len(b.installation.FinalEventScope) == 1 &&
		r.FinalEventScope[0] == b.installation.FinalEventScope[0] &&
		r.SourceBindingSHA256 == b.sourceBindingSHA &&
		r.XInsightOrigin == b.xiOrigin && r.UniRouterOrigin == b.urOrigin
}

func (b controlCredentialBoundary) receipt(op credentialOperation) connectorcontrol.CredentialReceipt {
	return connectorcontrol.CredentialReceipt{
		SchemaVersion:  connectorcontrol.CredentialReceiptSchema,
		InstallationID: b.installation.InstallationID, DeploymentID: b.installation.DeploymentID,
		ProjectID: b.installation.ProjectID, FinalEventScope: append([]string(nil), b.installation.FinalEventScope...),
		SourceBindingSHA256: b.sourceBindingSHA, XInsightOrigin: b.xiOrigin, UniRouterOrigin: b.urOrigin,
		OperationID: op.id, RequestSHA256: op.requestSHA, ControlIdentityID: b.identityID,
		PreviousTokenSHA256: op.previousDigest, ReplacementTokenSHA256: op.replacementDigest,
		ReplacementExpiresAt: op.expires.UTC(), State: op.state,
	}
}

type credentialOperation struct {
	id, installationID, kind, state, requestSHA, previousDigest, replacementDigest string
	expires                                                                        time.Time
	recoveryID                                                                     sql.NullString
}

func readCredentialOperation(ctx context.Context, q workspaceQueryer, id string, lock bool) (credentialOperation, error) {
	var op credentialOperation
	query := `SELECT operation_id,installation_id,kind,state,request_sha256,previous_token_sha256,replacement_token_sha256,replacement_expires_at,recovery_id
FROM connector_control_credential_operations WHERE operation_id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	err := q.QueryRowContext(ctx, query, id).Scan(&op.id, &op.installationID, &op.kind, &op.state, &op.requestSHA, &op.previousDigest, &op.replacementDigest, &op.expires, &op.recoveryID)
	if errors.Is(err, sql.ErrNoRows) {
		return op, connectorcontrol.ErrNotFound
	}
	return op, err
}

func lockCredentialToken(ctx context.Context, tx *sql.Tx, digest, identity string) (time.Time, bool, error) {
	return credentialToken(ctx, tx, digest, identity, true)
}

func readCredentialToken(ctx context.Context, tx *sql.Tx, digest, identity string) (time.Time, bool, error) {
	return credentialToken(ctx, tx, digest, identity, false)
}

func credentialToken(ctx context.Context, tx *sql.Tx, digest, identity string, lock bool) (time.Time, bool, error) {
	var expiry time.Time
	var revoked sql.NullTime
	query := `SELECT expires_at,revoked_at FROM service_identity_tokens WHERE token_digest=$1 AND service_identity_id=$2`
	if lock {
		query += ` FOR UPDATE`
	}
	err := tx.QueryRowContext(ctx, query, digest, identity).Scan(&expiry, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, connectorcontrol.ErrConflict
	}
	return expiry.UTC(), revoked.Valid, err
}

// A request can wait on the installation or identity lock past expiry. Use a
// post-lock database clock, retaining a caller-supplied later time for tests
// and clock-skew fail-closed behavior.
func credentialLockedNow(ctx context.Context, tx *sql.Tx, requested time.Time) (time.Time, error) {
	var observed time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&observed); err != nil {
		return time.Time{}, err
	}
	if observed.After(requested) {
		return observed.UTC().Truncate(time.Microsecond), nil
	}
	return requested.UTC().Truncate(time.Microsecond), nil
}

func (s *WorkspaceStore) PrepareControlCredential(ctx context.Context, r connectorcontrol.CredentialRequest, authenticatedIdentity, authenticatedDigest string, now time.Time, reauthorizeRecovery func(string) error) (connectorcontrol.CredentialReceipt, error) {
	var receipt connectorcontrol.CredentialReceipt
	if s == nil || s.db == nil || !r.Valid() || now.IsZero() ||
		(r.Kind == "rotate" && (authenticatedIdentity == "" || authenticatedDigest == "")) ||
		(r.Kind == "recover" && (authenticatedIdentity != "" || authenticatedDigest != "" || reauthorizeRecovery == nil)) {
		return receipt, connectorcontrol.ErrInvalid
	}
	requestSHA, err := r.Digest()
	if err != nil {
		return receipt, err
	}
	newDigest := connectorcontrol.RawSHA256(r.ReplacementToken)
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return receipt, err
	}
	defer tx.Rollback()
	b, err := s.lockControlCredentialBoundary(ctx, tx, r.InstallationID)
	if err != nil || !b.matchesRequest(r) || r.PreviousTokenSHA256 != b.currentDigest {
		return receipt, connectorcontrol.ErrConflict
	}
	now, err = credentialLockedNow(ctx, tx, now)
	if err != nil {
		return receipt, err
	}
	if r.Kind == "rotate" && (authenticatedIdentity != b.identityID || authenticatedDigest != b.currentDigest || b.identityStatus != "active" || !b.identityExpires.Valid || !b.identityExpires.Time.After(now)) {
		return receipt, connectorcontrol.ErrConflict
	}
	oldExpiry, oldRevoked, err := lockCredentialToken(ctx, tx, b.currentDigest, b.identityID)
	if err != nil || oldRevoked {
		return receipt, connectorcontrol.ErrConflict
	}
	now, err = credentialLockedNow(ctx, tx, now)
	if err != nil {
		return receipt, err
	}
	if r.Kind == "rotate" && (!oldExpiry.After(now) || oldExpiry.After(now.Add(connectorcontrol.ControlRotationWindow))) {
		return receipt, connectorcontrol.ErrConflict
	}
	if r.Kind == "recover" && oldExpiry.After(now) {
		return receipt, connectorcontrol.ErrConflict
	}
	if prior, readErr := readCredentialOperation(ctx, tx, r.OperationID, true); readErr == nil {
		now, err = credentialLockedNow(ctx, tx, now)
		if err != nil {
			return receipt, err
		}
		if prior.installationID != r.InstallationID || prior.requestSHA != requestSHA || prior.kind != r.Kind ||
			prior.previousDigest != b.currentDigest || prior.replacementDigest != newDigest || prior.state != "prepared" {
			return receipt, connectorcontrol.ErrConflict
		}
		if !prior.expires.After(now) || !b.identityExpires.Valid || !b.identityExpires.Time.After(now) || (r.Kind == "rotate" && !oldExpiry.After(now)) {
			return receipt, connectorcontrol.ErrConflict
		}
		return b.receipt(prior), tx.Commit()
	} else if !errors.Is(readErr, connectorcontrol.ErrNotFound) {
		return receipt, readErr
	}
	var ticketID string
	var recoveryTicketExpiry time.Time
	if r.Kind == "recover" {
		if connectorcontrol.RawSHA256(r.PreviousToken) != b.currentDigest {
			return receipt, connectorcontrol.ErrConflict
		}
		var actor, state string
		var ticketExpiry time.Time
		var ticketPrev string
		var supersededOp, supersededSHA sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT actor_subject,state,expires_at,previous_token_sha256,
superseded_operation_id,superseded_replacement_token_sha256 FROM connector_control_recovery_tickets
WHERE recovery_id=$1 AND installation_id=$2 AND ticket_digest=$3 FOR UPDATE`,
			r.RecoveryID, r.InstallationID, connectorcontrol.RawSHA256(r.RecoveryTicket)).Scan(&actor, &state, &ticketExpiry, &ticketPrev, &supersededOp, &supersededSHA)
		queryErr := err
		now, err = credentialLockedNow(ctx, tx, now)
		if err != nil {
			return receipt, err
		}
		err = queryErr
		if err != nil || state != "issued" || !ticketExpiry.After(now) || ticketPrev != b.currentDigest ||
			!supersededMatch(r, supersededOp, supersededSHA) || reauthorizeRecovery(actor) != nil {
			return receipt, connectorcontrol.ErrConflict
		}
		ticketID = r.RecoveryID
		recoveryTicketExpiry = ticketExpiry
	}
	var pendingID string
	err = tx.QueryRowContext(ctx, `SELECT operation_id FROM connector_control_credential_operations
WHERE installation_id=$1 AND state='prepared' FOR UPDATE`, r.InstallationID).Scan(&pendingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return receipt, err
	}
	now, err = credentialLockedNow(ctx, tx, now)
	if err != nil {
		return receipt, err
	}
	if r.Kind == "rotate" && pendingID != "" {
		return receipt, connectorcontrol.ErrConflict
	}
	if r.Kind == "recover" {
		if *r.SupersededOperationID != pendingID {
			return receipt, connectorcontrol.ErrConflict
		}
		if pendingID != "" {
			pending, readErr := readCredentialOperation(ctx, tx, pendingID, false)
			if readErr != nil || pending.previousDigest != b.currentDigest ||
				pending.replacementDigest != *r.SupersededReplacementTokenSHA256 || pending.expires.After(now) {
				return receipt, connectorcontrol.ErrConflict
			}
			pendingTokenExpiry, pendingRevoked, lockErr := lockCredentialToken(ctx, tx, pending.replacementDigest, b.identityID)
			now, err = credentialLockedNow(ctx, tx, now)
			if err != nil {
				return receipt, err
			}
			if lockErr != nil || pendingRevoked || pendingTokenExpiry.After(now) || !pendingTokenExpiry.Equal(pending.expires) {
				return receipt, connectorcontrol.ErrConflict
			}
			if _, err = tx.ExecContext(ctx, `UPDATE connector_control_credential_operations
SET state='superseded',superseded_by_operation_id=$2,updated_at=$3 WHERE operation_id=$1 AND state='prepared'`, pendingID, r.OperationID, now); err != nil {
				return receipt, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE service_identity_tokens SET revoked_at=$2
WHERE token_digest=$1 AND revoked_at IS NULL AND expires_at<=$2`, pending.replacementDigest, now); err != nil {
				return receipt, err
			}
		}
	}
	now, err = credentialLockedNow(ctx, tx, now)
	if err != nil {
		return receipt, err
	}
	if r.Kind == "rotate" && (!oldExpiry.After(now) || !b.identityExpires.Valid || !b.identityExpires.Time.After(now)) {
		return receipt, connectorcontrol.ErrConflict
	}
	if r.Kind == "recover" && (!recoveryTicketExpiry.After(now) || oldExpiry.After(now)) {
		return receipt, connectorcontrol.ErrConflict
	}
	expiry := now.Add(connectorcontrol.ControlCredentialTTL).UTC().Truncate(time.Microsecond)
	identityUpdate, err := tx.ExecContext(ctx, `UPDATE service_identities SET status='active',expires_at=$2,updated_at=$3
WHERE id=$1 AND status IN ('active','expired') AND revoked_at IS NULL`, b.identityID, expiry, now)
	if err != nil {
		return receipt, err
	}
	if n, e := identityUpdate.RowsAffected(); e != nil || n != 1 {
		return receipt, connectorcontrol.ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO service_identity_tokens(token_digest,service_identity_id,token_prefix,created_at,expires_at)
VALUES($1,$2,$3,$4,$5)`, newDigest, b.identityID, r.ReplacementToken[:12], now, expiry); err != nil {
		return receipt, connectorcontrol.ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO connector_control_credential_operations
(operation_id,installation_id,kind,state,request_sha256,previous_token_sha256,replacement_token_sha256,replacement_expires_at,recovery_id,created_at,updated_at)
VALUES($1,$2,$3,'prepared',$4,$5,$6,$7,$8,$9,$9)`, r.OperationID, r.InstallationID, r.Kind, requestSHA, b.currentDigest, newDigest, expiry, nullableString(ticketID), now); err != nil {
		return receipt, connectorcontrol.ErrConflict
	}
	if r.Kind == "recover" {
		if _, err = tx.ExecContext(ctx, `UPDATE connector_control_recovery_tickets SET state='consumed',consumed_operation_id=$2,updated_at=$3
WHERE recovery_id=$1 AND state='issued'`, r.RecoveryID, r.OperationID, now); err != nil {
			return receipt, err
		}
	}
	if err = insertAccessAudit(ctx, tx, "service", b.identityID, "connector_control_credential.prepare", "connector_installation", r.InstallationID, "accepted", now); err != nil {
		return receipt, err
	}
	op := credentialOperation{id: r.OperationID, installationID: r.InstallationID, kind: r.Kind, state: "prepared", requestSHA: requestSHA,
		previousDigest: b.currentDigest, replacementDigest: newDigest, expires: expiry}
	receipt = b.receipt(op)
	return receipt, tx.Commit()
}

func supersededMatch(r connectorcontrol.CredentialRequest, id, digest sql.NullString) bool {
	return r.SupersededOperationID != nil && r.SupersededReplacementTokenSHA256 != nil &&
		*r.SupersededOperationID == id.String && *r.SupersededReplacementTokenSHA256 == digest.String
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (s *WorkspaceStore) ReadControlCredentialOperation(ctx context.Context, installationID, operationID, identityID, bearerDigest string, now time.Time) (connectorcontrol.CredentialReceipt, error) {
	return s.controlCredentialOperation(ctx, installationID, operationID, identityID, bearerDigest, "", false, now)
}

func (s *WorkspaceStore) ConfirmControlCredential(ctx context.Context, installationID string, confirm connectorcontrol.CredentialConfirm, identityID, bearerDigest string, now time.Time) (connectorcontrol.CredentialReceipt, error) {
	if confirm.SchemaVersion != connectorcontrol.CredentialRequestSchema || !connectorcontrol.IsCredentialOperationID(confirm.OperationID) {
		return connectorcontrol.CredentialReceipt{}, connectorcontrol.ErrInvalid
	}
	return s.controlCredentialOperation(ctx, installationID, confirm.OperationID, identityID, bearerDigest, confirm.RequestSHA256, true, now)
}

func (s *WorkspaceStore) controlCredentialOperation(ctx context.Context, installationID, operationID, identityID, bearerDigest, requestSHA string, confirm bool, now time.Time) (connectorcontrol.CredentialReceipt, error) {
	var receipt connectorcontrol.CredentialReceipt
	if s == nil || s.db == nil || now.IsZero() || !connectorcontrol.IsInstallationID(installationID) ||
		!connectorcontrol.IsCredentialOperationID(operationID) {
		return receipt, connectorcontrol.ErrInvalid
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return receipt, err
	}
	defer tx.Rollback()
	b, err := s.lockControlCredentialBoundary(ctx, tx, installationID)
	if err == nil {
		now, err = credentialLockedNow(ctx, tx, now)
	}
	if err != nil || b.identityID != identityID || b.identityStatus != "active" || !b.identityExpires.Valid || !b.identityExpires.Time.After(now) {
		return receipt, connectorcontrol.ErrConflict
	}
	op, err := readCredentialOperation(ctx, tx, operationID, true)
	if err != nil || op.installationID != installationID || op.state == "superseded" || op.replacementDigest != bearerDigest ||
		(confirm && op.requestSHA != requestSHA) {
		return receipt, connectorcontrol.ErrConflict
	}
	newExpiry, newRevoked, err := lockCredentialToken(ctx, tx, op.replacementDigest, b.identityID)
	if err == nil {
		now, err = credentialLockedNow(ctx, tx, now)
	}
	if err != nil || newRevoked || !newExpiry.After(now) || !newExpiry.Equal(op.expires) || !b.identityExpires.Time.After(now) {
		return receipt, connectorcontrol.ErrConflict
	}
	if op.state == "prepared" {
		if b.currentDigest != op.previousDigest {
			return receipt, connectorcontrol.ErrConflict
		}
		if confirm {
			_, oldRevoked, lockErr := lockCredentialToken(ctx, tx, op.previousDigest, b.identityID)
			if lockErr == nil {
				now, lockErr = credentialLockedNow(ctx, tx, now)
			}
			if lockErr != nil || oldRevoked {
				return receipt, connectorcontrol.ErrConflict
			}
			if !newExpiry.After(now) || !b.identityExpires.Time.After(now) {
				return receipt, connectorcontrol.ErrConflict
			}
			updated, updateErr := tx.ExecContext(ctx, `UPDATE connector_control_credentials SET current_token_digest=$2,updated_at=$3
WHERE installation_id=$1 AND current_token_digest=$4`, installationID, op.replacementDigest, now, op.previousDigest)
			if updateErr != nil {
				return receipt, updateErr
			}
			if n, e := updated.RowsAffected(); e != nil || n != 1 {
				return receipt, connectorcontrol.ErrConflict
			}
			if _, err = tx.ExecContext(ctx, `UPDATE service_identity_tokens SET revoked_at=$2 WHERE token_digest=$1 AND revoked_at IS NULL`, op.previousDigest, now); err != nil {
				return receipt, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE connector_control_credential_operations SET state='committed',updated_at=$2
WHERE operation_id=$1 AND state='prepared'`, op.id, now); err != nil {
				return receipt, err
			}
			op.state = "committed"
		}
	} else if op.state != "committed" || b.currentDigest != op.replacementDigest {
		return receipt, connectorcontrol.ErrConflict
	}
	if confirm {
		if err = insertAccessAudit(ctx, tx, "service", b.identityID, "connector_control_credential.confirm", "connector_installation", installationID, "accepted", now); err != nil {
			return receipt, err
		}
	}
	receipt = b.receipt(op)
	return receipt, tx.Commit()
}
