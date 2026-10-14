package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
)

func (b controlCredentialBoundary) recoveryMaterial(id, previous, supersededID, supersededDigest string, expiry time.Time) connectorcontrol.RecoveryMaterial {
	return connectorcontrol.RecoveryMaterial{
		SchemaVersion:  connectorcontrol.RecoveryMaterialSchema,
		InstallationID: b.installation.InstallationID, DeploymentID: b.installation.DeploymentID,
		ProjectID: b.installation.ProjectID, FinalEventScope: append([]string(nil), b.installation.FinalEventScope...),
		SourceBindingSHA256: b.sourceBindingSHA, XInsightOrigin: b.xiOrigin, UniRouterOrigin: b.urOrigin,
		RecoveryID: id, PreviousTokenSHA256: previous, SupersededOperationID: supersededID,
		SupersededReplacementTokenSHA256: supersededDigest, ExpiresAt: expiry.UTC(),
	}
}

// IssueControlCredentialRecovery keeps only the ticket digest in PostgreSQL.
// The root-owned host boundary freezes the exact ticket before calling this.
func (s *WorkspaceStore) IssueControlCredentialRecovery(ctx context.Context, installationID, requestID, actor, recoveryID, ticket string,
	now time.Time, reauthorize func(string) error) (connectorcontrol.RecoveryMaterial, error) {
	var out connectorcontrol.RecoveryMaterial
	if s == nil || s.db == nil || !connectorcontrol.IsInstallationID(installationID) ||
		!connectorcontrol.SetupRequestIDPattern.MatchString(requestID) || !connectorcontrol.IsRecoveryID(recoveryID) ||
		!connectorcontrol.IsRecoveryTicket(ticket) || now.IsZero() || reauthorize == nil {
		return out, connectorcontrol.ErrInvalid
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	b, err := s.lockControlCredentialBoundary(ctx, tx, installationID)
	if err == nil {
		now, err = credentialLockedNow(ctx, tx, now)
	}
	if err != nil || reauthorize(actor) != nil {
		return out, connectorcontrol.ErrConflict
	}
	currentExpiry, revoked, err := lockCredentialToken(ctx, tx, b.currentDigest, b.identityID)
	if err == nil {
		now, err = credentialLockedNow(ctx, tx, now)
	}
	if err != nil || revoked || currentExpiry.After(now) {
		return out, connectorcontrol.ErrConflict
	}
	var pendingID, pendingDigest string
	var pendingExpiry time.Time
	err = tx.QueryRowContext(ctx, `SELECT operation_id,replacement_token_sha256,replacement_expires_at
FROM connector_control_credential_operations WHERE installation_id=$1 AND state='prepared' FOR UPDATE`, installationID).Scan(&pendingID, &pendingDigest, &pendingExpiry)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if pendingID != "" {
		pendingTokenExpiry, pendingRevoked, lockErr := lockCredentialToken(ctx, tx, pendingDigest, b.identityID)
		if lockErr == nil {
			now, lockErr = credentialLockedNow(ctx, tx, now)
		}
		if lockErr != nil || pendingRevoked || pendingExpiry.After(now) || !pendingTokenExpiry.Equal(pendingExpiry) {
			return out, connectorcontrol.ErrConflict
		}
	}
	var savedID, savedActor, savedDigest, savedPrevious, savedState string
	var savedExpiry time.Time
	var savedSupersededID, savedSupersededSHA sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT recovery_id,actor_subject,ticket_digest,previous_token_sha256,state,expires_at,
superseded_operation_id,superseded_replacement_token_sha256 FROM connector_control_recovery_tickets
WHERE installation_id=$1 AND request_id=$2::uuid FOR UPDATE`, installationID, requestID).Scan(
		&savedID, &savedActor, &savedDigest, &savedPrevious, &savedState, &savedExpiry, &savedSupersededID, &savedSupersededSHA)
	if err == nil {
		now, err = credentialLockedNow(ctx, tx, now)
		if err != nil {
			return out, err
		}
		if savedState == "issued" && !savedExpiry.After(now) {
			return out, connectorcontrol.ErrExpired
		}
		if savedID != recoveryID || savedActor != actor || savedDigest != connectorcontrol.RawSHA256(ticket) ||
			savedPrevious != b.currentDigest || savedSupersededID.String != pendingID || savedSupersededSHA.String != pendingDigest ||
			savedState != "issued" || !savedExpiry.After(now) {
			return out, connectorcontrol.ErrConflict
		}
		out = b.recoveryMaterial(savedID, savedPrevious, savedSupersededID.String, savedSupersededSHA.String, savedExpiry)
		out.Ticket = ticket
		out.RecoverURL, err = connectorcontrol.BuildCredentialRecoveryURL(out)
		if err != nil {
			return connectorcontrol.RecoveryMaterial{}, err
		}
		return out, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	now, err = credentialLockedNow(ctx, tx, now)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE connector_control_recovery_tickets SET state='expired',updated_at=$2
WHERE installation_id=$1 AND state='issued' AND expires_at<=$2`, installationID, now); err != nil {
		return out, err
	}
	var existingID string
	err = tx.QueryRowContext(ctx, `SELECT recovery_id FROM connector_control_recovery_tickets
WHERE installation_id=$1 AND state='issued' FOR UPDATE`, installationID).Scan(&existingID)
	if err == nil {
		return out, connectorcontrol.ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	now, err = credentialLockedNow(ctx, tx, now)
	if err != nil {
		return out, err
	}
	if currentExpiry.After(now) || revoked || (pendingID != "" && pendingExpiry.After(now)) {
		return out, connectorcontrol.ErrConflict
	}
	expiry := now.Add(connectorcontrol.ControlRecoveryTTL)
	if _, err = tx.ExecContext(ctx, `INSERT INTO connector_control_recovery_tickets
(recovery_id,installation_id,request_id,actor_subject,ticket_digest,previous_token_sha256,
superseded_operation_id,superseded_replacement_token_sha256,state,expires_at,created_at,updated_at)
VALUES($1,$2,$3::uuid,$4,$5,$6,$7,$8,'issued',$9,$10,$10)`,
		recoveryID, installationID, requestID, actor, connectorcontrol.RawSHA256(ticket), b.currentDigest,
		nullableString(pendingID), nullableString(pendingDigest), expiry, now); err != nil {
		return out, connectorcontrol.ErrConflict
	}
	if err = insertAccessAudit(ctx, tx, "human", actor, "connector_control_credential.recovery_issue", "connector_installation", installationID, "accepted", now); err != nil {
		return out, err
	}
	out = b.recoveryMaterial(recoveryID, b.currentDigest, pendingID, pendingDigest, expiry)
	out.Ticket = ticket
	out.RecoverURL, err = connectorcontrol.BuildCredentialRecoveryURL(out)
	if err != nil {
		return connectorcontrol.RecoveryMaterial{}, err
	}
	return out, tx.Commit()
}

func (s *WorkspaceStore) ResolveControlCredentialRecovery(ctx context.Context, ticket string, now time.Time) (connectorcontrol.RecoveryMaterial, error) {
	var out connectorcontrol.RecoveryMaterial
	if s == nil || s.db == nil || !connectorcontrol.IsRecoveryTicket(ticket) || now.IsZero() {
		return out, connectorcontrol.ErrInvalid
	}
	var id, installationID, previous, supersededID, supersededSHA, state string
	var expiry time.Time
	err := s.db.QueryRowContext(ctx, `SELECT recovery_id,installation_id,previous_token_sha256,
COALESCE(superseded_operation_id,''),COALESCE(superseded_replacement_token_sha256,''),state,expires_at
FROM connector_control_recovery_tickets WHERE ticket_digest=$1`, connectorcontrol.RawSHA256(ticket)).Scan(
		&id, &installationID, &previous, &supersededID, &supersededSHA, &state, &expiry)
	if err != nil || state != "issued" || !expiry.After(now.UTC()) {
		return out, connectorcontrol.ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	b, err := s.readControlCredentialBoundary(ctx, tx, installationID)
	if err == nil {
		now, err = credentialLockedNow(ctx, tx, now)
	}
	if err != nil || b.currentDigest != previous || !expiry.After(now) {
		return out, connectorcontrol.ErrConflict
	}
	currentExpiry, currentRevoked, err := readCredentialToken(ctx, tx, previous, b.identityID)
	if err != nil || currentRevoked || currentExpiry.After(now) {
		return out, connectorcontrol.ErrConflict
	}
	if supersededID != "" {
		pending, readErr := readCredentialOperation(ctx, tx, supersededID, false)
		if readErr != nil || pending.installationID != installationID || pending.state != "prepared" || pending.previousDigest != previous ||
			pending.replacementDigest != supersededSHA || pending.expires.After(now) {
			return out, connectorcontrol.ErrConflict
		}
		pendingExpiry, pendingRevoked, tokenErr := readCredentialToken(ctx, tx, supersededSHA, b.identityID)
		if tokenErr != nil || pendingRevoked || pendingExpiry.After(now) || !pendingExpiry.Equal(pending.expires) {
			return out, connectorcontrol.ErrConflict
		}
	}
	out = b.recoveryMaterial(id, previous, supersededID, supersededSHA, expiry)
	return out, tx.Commit()
}
