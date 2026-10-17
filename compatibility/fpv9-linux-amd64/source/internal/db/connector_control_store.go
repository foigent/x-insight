package db

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/workspace"
)

var connectorServiceIdentityPattern = regexp.MustCompile(`^xsi_[A-Za-z0-9_-]{22}$`)

type controlOperationRequestWire struct {
	SchemaVersion  string   `json:"schema_version"`
	OperationID    string   `json:"operation_id"`
	InstallationID string   `json:"installation_id"`
	Kind           string   `json:"kind"`
	ProjectID      string   `json:"project_id"`
	EventScope     []string `json:"event_scope"`
	DesiredState   string   `json:"desired_state"`
}

func (s *WorkspaceStore) CreateInvitation(
	ctx context.Context,
	input connectorcontrol.CreateInvitationInput,
) (connectorcontrol.InvitationPackage, connectorcontrol.Installation, error) {
	// Production invitation issuance reaches this persistence method only in
	// the privileged local controller. The web process holds the restricted
	// workspace role, which has no INSERT authority for either table.
	if s == nil || s.db == nil || input.Config.Validate() != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, connectorcontrol.ErrInvalid
	}
	if _, err := workspace.HumanActor(input.ActorSubject); err != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, connectorcontrol.ErrInvalid
	}
	now := input.Now.UTC()
	if now.IsZero() {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, connectorcontrol.ErrInvalid
	}
	ttl := input.TTL
	if ttl == 0 {
		ttl = connectorcontrol.DefaultInvitationTTL
	}
	if ttl <= 0 || ttl > connectorcontrol.MaximumInvitationTTL {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, connectorcontrol.ErrInvalid
	}
	random := input.Random
	if random == nil {
		random = rand.Reader
	}
	installationID, err := connectorcontrol.NewID("xci_", random)
	if err != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, err
	}
	invitationID, err := connectorcontrol.NewID("xii_", random)
	if err != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, err
	}
	token, tokenDigest, err := connectorcontrol.NewInvitationToken(random)
	if err != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, err
	}
	installURL, err := connectorcontrol.BuildInstallURL(
		input.Config.UniRouterInstallOrigin,
		token,
		input.Config.XInsightOrigin,
	)
	if err != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, err
	}
	expiresAt := now.Add(ttl)
	requestedScope := []string{connectorcontrol.CompletedEventType}
	installation := connectorcontrol.Installation{
		SchemaVersion:  connectorcontrol.InstallationSchema,
		InstallationID: installationID, DeploymentID: input.Config.DeploymentID,
		ConnectorType:       connectorcontrol.ConnectorTypeUniRouter,
		RequestedEventScope: requestedScope,
		Status:              "pending", DesiredState: "active", ObservedState: "awaiting_consent",
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if err := connectorcontrol.ValidateInstallation(installation); err != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO connector_installations (
  id, deployment_id, connector_type, requested_event_scope,
  status, desired_state, observed_state, created_at, updated_at
) VALUES ($1, $2, $3, $4, 'pending', 'active', 'awaiting_consent', $5, $5)`,
		installationID, input.Config.DeploymentID, connectorcontrol.ConnectorTypeUniRouter,
		requestedScope, now); err != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO connector_installation_invitations (
  id, installation_id, nonce_digest, token_prefix, audience,
  xinsight_origin, unirouter_install_origin, requested_event_scope,
  status, created_by_subject, created_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active', $9, $10, $11)`,
		invitationID, installationID, tokenDigest, token[:12], connectorcontrol.ConnectorAudience,
		input.Config.XInsightOrigin, input.Config.UniRouterInstallOrigin, requestedScope,
		input.ActorSubject, now, expiresAt); err != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, err
	}
	if err := insertAccessAudit(
		ctx, tx, "human", input.ActorSubject, "connector_installation.invite",
		"connector_installation", installationID, "accepted", now,
	); err != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, err
	}
	if err := tx.Commit(); err != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, err
	}
	return connectorcontrol.InvitationPackage{
		SchemaVersion: connectorcontrol.InvitationSchema,
		InvitationID:  invitationID, InstallationID: installationID,
		DeploymentID: input.Config.DeploymentID, Audience: connectorcontrol.ConnectorAudience,
		XInsightOrigin:      input.Config.XInsightOrigin,
		RequestedEventScope: requestedScope, ExpiresAt: expiresAt,
		InstallURL: installURL, Token: token,
	}, installation, nil
}

func (s *WorkspaceStore) ListInstallations(
	ctx context.Context,
	limit int,
) ([]connectorcontrol.Installation, error) {
	if s == nil || s.db == nil || limit < 1 || limit > connectorcontrol.MaximumConnectorListSize {
		return nil, connectorcontrol.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, deployment_id, connector_type, array_to_json(requested_event_scope)::text,
       COALESCE(array_to_json(final_event_scope)::text, ''), COALESCE(project_id, ''),
       COALESCE(source_instance, ''), COALESCE(source_instance_sha256, ''),
       COALESCE(control_service_identity_id, ''), COALESCE(installation_acknowledgement_sha256, ''),
       status, desired_state, observed_state, COALESCE(status_reason_code, ''),
       last_agent_seen_at, created_at, updated_at, version
FROM connector_installations
ORDER BY created_at DESC, id
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]connectorcontrol.Installation, 0)
	for rows.Next() {
		item, err := scanConnectorInstallation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// InspectActivationBoundary returns the root-controller-only durable authority
// for one installation. The UniRouter origin is read only from the exact
// consumed invitation and is never accepted from a phase request or caller.
func (s *WorkspaceStore) InspectActivationBoundary(
	ctx context.Context,
	installationID string,
) (connectorcontrol.ActivationBoundary, error) {
	if s == nil || s.db == nil || !connectorcontrol.IsInstallationID(installationID) {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	defer tx.Rollback()
	if err := requireConnectorActivationAuthority(ctx, tx); err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	boundary, err := loadActivationBoundary(ctx, tx, installationID)
	if err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	if err := tx.Commit(); err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	return boundary, nil
}

// ClaimInstallationActivation atomically establishes the controller_pending
// mutation authority after the exact paused Source result. It is the only
// product-local bridge from CP2A source_ready into host-side activation work.
func (s *WorkspaceStore) ClaimInstallationActivation(
	ctx context.Context,
	installationID string,
	expectedVersion int64,
	prepareOperationID string,
	prepareResultSHA256 string,
	now time.Time,
) (connectorcontrol.ActivationBoundary, error) {
	if s == nil || s.db == nil || !connectorcontrol.IsInstallationID(installationID) ||
		expectedVersion < 1 || !connectorcontrol.IsOperationID(prepareOperationID) ||
		!connectorcontrol.IsSHA256Reference(prepareResultSHA256) || now.IsZero() {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrInvalid
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	defer tx.Rollback()
	if err := requireConnectorActivationAuthority(ctx, tx); err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	var lockedID string
	if err := tx.QueryRowContext(ctx, `
SELECT id
FROM connector_installations
WHERE id = $1
FOR UPDATE`, installationID).Scan(&lockedID); errors.Is(err, sql.ErrNoRows) {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrNotFound
	} else if err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	boundary, err := loadActivationBoundary(ctx, tx, lockedID)
	if err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	prepare := boundary.PreparePausedSource
	if prepare == nil || prepare.Result == nil || prepare.Operation.Status != "completed" ||
		prepare.Result.Status != "completed" || prepare.Operation.OperationID != prepareOperationID ||
		prepare.Operation.ResultSHA256 != prepareResultSHA256 || prepare.Result.Result == nil ||
		prepare.Result.Result.SourceInstance != boundary.Installation.SourceInstance ||
		prepare.Result.Result.SourceState != "paused" ||
		prepare.Result.Result.Readiness != connectorcontrol.ReadinessPausedSourceCursorReady ||
		prepare.Operation.CompletedAt == nil || now.Before(*prepare.Operation.CompletedAt) ||
		now.Before(boundary.Installation.UpdatedAt) || boundary.RegisterReceiver != nil || boundary.ResumeSource != nil {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
	}
	installation := boundary.Installation
	if installation.Status == "pending" && installation.DesiredState == "active" &&
		installation.ObservedState == "controller_pending" {
		if installation.Version != expectedVersion+1 {
			return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return connectorcontrol.ActivationBoundary{}, err
		}
		return boundary, nil
	}
	if installation.Version != expectedVersion || installation.Status != "pending" ||
		installation.DesiredState != "active" || installation.ObservedState != "source_ready" {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
	}
	update, err := tx.ExecContext(ctx, `
UPDATE connector_installations
SET observed_state = 'controller_pending', updated_at = $3, version = version + 1
WHERE id = $1 AND version = $2 AND status = 'pending'
  AND desired_state = 'active' AND observed_state = 'source_ready'`,
		installationID, expectedVersion, now)
	if err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	if err := requireOneConnectorRow(update); err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	if err := insertAccessAudit(
		ctx, tx, "system", "system", "connector_installation.activation_claimed",
		"connector_installation", installationID, "accepted", now,
	); err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	claimed, err := loadActivationBoundary(ctx, tx, installationID)
	if err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	if claimed.Installation.Version != expectedVersion+1 ||
		claimed.Installation.Status != "pending" || claimed.Installation.DesiredState != "active" ||
		claimed.Installation.ObservedState != "controller_pending" {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	return claimed, nil
}

// FinalizeInstallationConnected performs the final product-local CAS only
// after the exact completed resume result has been durably read back. Host
// worker readiness is deliberately a caller-owned precondition; ambiguous host
// effects must be resolved before this method is called.
func (s *WorkspaceStore) FinalizeInstallationConnected(
	ctx context.Context,
	installationID string,
	expectedVersion int64,
	resumeOperationID string,
	resumeResultSHA256 string,
	now time.Time,
) (connectorcontrol.Installation, error) {
	if s == nil || s.db == nil || !connectorcontrol.IsInstallationID(installationID) ||
		expectedVersion < 1 || !connectorcontrol.IsOperationID(resumeOperationID) ||
		!connectorcontrol.IsSHA256Reference(resumeResultSHA256) || now.IsZero() {
		return connectorcontrol.Installation{}, connectorcontrol.ErrInvalid
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return connectorcontrol.Installation{}, err
	}
	defer tx.Rollback()
	if err := requireConnectorActivationAuthority(ctx, tx); err != nil {
		return connectorcontrol.Installation{}, err
	}
	var lockedID string
	if err := tx.QueryRowContext(ctx, `
SELECT id
FROM connector_installations
WHERE id = $1
FOR UPDATE`, installationID).Scan(&lockedID); errors.Is(err, sql.ErrNoRows) {
		return connectorcontrol.Installation{}, connectorcontrol.ErrNotFound
	} else if err != nil {
		return connectorcontrol.Installation{}, err
	}
	boundary, err := loadActivationBoundary(ctx, tx, lockedID)
	if err != nil {
		return connectorcontrol.Installation{}, err
	}
	resume := boundary.ResumeSource
	if resume == nil || resume.Result == nil || resume.Operation.Status != "completed" ||
		resume.Result.Status != "completed" || resume.Operation.OperationID != resumeOperationID ||
		resume.Operation.ResultSHA256 != resumeResultSHA256 ||
		resume.Result.Result == nil || resume.Result.Result.SourceInstance != boundary.Installation.SourceInstance ||
		resume.Result.Result.SourceState != "active" || resume.Result.Result.ReceiverState != "active" ||
		resume.Result.Result.Readiness != connectorcontrol.ReadinessSourceResumedReceiverActive ||
		resume.Operation.CompletedAt == nil || now.Before(*resume.Operation.CompletedAt) ||
		now.Before(boundary.Installation.UpdatedAt) {
		return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
	}
	installation := boundary.Installation
	if installation.Status == "connected" {
		if installation.DesiredState != "active" || installation.ObservedState != "connected" ||
			installation.Version != expectedVersion+1 {
			return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return connectorcontrol.Installation{}, err
		}
		return installation, nil
	}
	if installation.Version != expectedVersion || installation.Status != "pending" ||
		installation.DesiredState != "active" || installation.ObservedState != "controller_pending" {
		return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
	}
	update, err := tx.ExecContext(ctx, `
UPDATE connector_installations
SET status = 'connected', observed_state = 'connected', status_reason_code = NULL,
    updated_at = $3, version = version + 1
WHERE id = $1 AND version = $2 AND status = 'pending'
  AND desired_state = 'active' AND observed_state = 'controller_pending'`,
		installationID, expectedVersion, now)
	if err != nil {
		return connectorcontrol.Installation{}, err
	}
	if err := requireOneConnectorRow(update); err != nil {
		return connectorcontrol.Installation{}, err
	}
	if err := insertAccessAudit(
		ctx, tx, "system", "system", "connector_installation.connected",
		"connector_installation", installationID, "accepted", now,
	); err != nil {
		return connectorcontrol.Installation{}, err
	}
	connected, err := loadConnectorInstallation(ctx, tx, installationID)
	if err != nil {
		return connectorcontrol.Installation{}, err
	}
	if err := tx.Commit(); err != nil {
		return connectorcontrol.Installation{}, err
	}
	return connected, nil
}

func requireConnectorActivationAuthority(ctx context.Context, tx *sql.Tx) error {
	var authorized bool
	if err := tx.QueryRowContext(ctx, `
SELECT current_user = 'xinsight_admin'
    OR current_setting('is_superuser') = 'on'`).Scan(&authorized); err != nil {
		return err
	}
	if !authorized {
		return connectorcontrol.ErrConflict
	}
	return nil
}

func loadActivationBoundary(
	ctx context.Context,
	tx *sql.Tx,
	installationID string,
) (connectorcontrol.ActivationBoundary, error) {
	installation, err := loadConnectorInstallation(ctx, tx, installationID)
	if errors.Is(err, sql.ErrNoRows) {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrNotFound
	}
	if err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	if installation.InstallationAcknowledgementSHA256 == "" {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
	}
	rows, err := tx.QueryContext(ctx, `
SELECT unirouter_install_origin
FROM connector_installation_invitations
WHERE installation_id = $1 AND status = 'consumed'
  AND acknowledgement_sha256 = $2
ORDER BY consumed_at, id`, installationID, installation.InstallationAcknowledgementSHA256)
	if err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	var origins []string
	for rows.Next() {
		var origin string
		if err := rows.Scan(&origin); err != nil {
			rows.Close()
			return connectorcontrol.ActivationBoundary{}, err
		}
		origins = append(origins, origin)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return connectorcontrol.ActivationBoundary{}, rowsErr
	}
	if len(origins) != 1 {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
	}
	boundary := connectorcontrol.ActivationBoundary{
		Installation:    installation,
		UniRouterOrigin: origins[0],
	}
	phaseRows, err := tx.QueryContext(ctx, `
SELECT operation_id, phase
FROM connector_control_operations
WHERE installation_id = $1 AND phase IS NOT NULL
ORDER BY CASE phase
  WHEN 'prepare_paused_source' THEN 1
  WHEN 'register_receiver' THEN 2
  WHEN 'resume_source' THEN 3
  ELSE 4
END`, installationID)
	if err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	type activationPhaseReference struct {
		operationID string
		phaseName   string
	}
	references := make([]activationPhaseReference, 0, 3)
	for phaseRows.Next() {
		var operationID, phaseName string
		if err := phaseRows.Scan(&operationID, &phaseName); err != nil {
			phaseRows.Close()
			return connectorcontrol.ActivationBoundary{}, err
		}
		references = append(references, activationPhaseReference{operationID: operationID, phaseName: phaseName})
	}
	phaseRowsErr := phaseRows.Err()
	phaseRows.Close()
	if phaseRowsErr != nil {
		return connectorcontrol.ActivationBoundary{}, phaseRowsErr
	}
	for _, reference := range references {
		operationID, phaseName := reference.operationID, reference.phaseName
		operation, err := loadControlOperation(ctx, tx, operationID)
		if err != nil {
			return connectorcontrol.ActivationBoundary{}, err
		}
		phase := &connectorcontrol.ActivationPhase{Operation: operation}
		if operation.Status == "completed" || operation.Status == "failed" {
			result, err := loadTerminalControlOperationResult(ctx, tx, operation)
			if err != nil {
				return connectorcontrol.ActivationBoundary{}, err
			}
			phase.Result = &result
		}
		switch phaseName {
		case connectorcontrol.ControlPhasePreparePausedSource:
			boundary.PreparePausedSource = phase
		case connectorcontrol.ControlPhaseRegisterReceiver:
			boundary.RegisterReceiver = phase
		case connectorcontrol.ControlPhaseResumeSource:
			boundary.ResumeSource = phase
		default:
			return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
		}
	}
	if err := connectorcontrol.ValidateActivationBoundary(boundary); err != nil {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
	}
	return boundary, nil
}

func (s *WorkspaceStore) RedeemInvitation(
	ctx context.Context,
	input connectorcontrol.RedeemInvitationInput,
	now time.Time,
	_ io.Reader,
) (connectorcontrol.RegistrationResult, error) {
	if s == nil || s.db == nil || connectorcontrol.ValidateRedeemInvitation(input, now.UTC()) != nil {
		return connectorcontrol.RegistrationResult{}, connectorcontrol.ErrInvalid
	}
	digest, err := connectorcontrol.InvitationTokenDigest(input.InvitationToken)
	if err != nil {
		return connectorcontrol.RegistrationResult{}, connectorcontrol.ErrInvalid
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return connectorcontrol.RegistrationResult{}, err
	}
	defer tx.Rollback()
	var (
		installationID     string
		deploymentID       string
		status             string
		expiresAt          time.Time
		createdAt          time.Time
		requestedScopeJSON string
	)
	err = tx.QueryRowContext(ctx, `
SELECT invitation.installation_id, invitation.status, invitation.created_at,
       invitation.expires_at,
       installation.deployment_id, array_to_json(invitation.requested_event_scope)::text
FROM connector_installation_invitations invitation
JOIN connector_installations installation ON installation.id = invitation.installation_id
WHERE invitation.nonce_digest = $1`, digest).Scan(
		&installationID, &status, &createdAt, &expiresAt, &deploymentID, &requestedScopeJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return connectorcontrol.RegistrationResult{}, connectorcontrol.ErrNotFound
	}
	if err != nil {
		return connectorcontrol.RegistrationResult{}, err
	}
	if input.ConfirmedAt.Before(createdAt.UTC()) || !input.ConfirmedAt.Before(expiresAt.UTC()) {
		return connectorcontrol.RegistrationResult{}, connectorcontrol.ErrConflict
	}
	var requestedScope []string
	if json.Unmarshal([]byte(requestedScopeJSON), &requestedScope) != nil ||
		len(requestedScope) != 1 || requestedScope[0] != connectorcontrol.CompletedEventType {
		return connectorcontrol.RegistrationResult{}, connectorcontrol.ErrConflict
	}
	acknowledgement := connectorcontrol.InstallationAcknowledgement{
		SchemaVersion:  connectorcontrol.AcknowledgementSchema,
		InstallationID: installationID, DeploymentID: deploymentID,
		ProjectID: input.ProjectID, FinalEventScope: append([]string(nil), input.FinalEventScope...),
		ConsentSHA256: input.ConsentSHA256, ConfirmedAt: input.ConfirmedAt.UTC(),
	}
	acknowledgementSHA256, acknowledgementPayload, err := connectorcontrol.CanonicalSHA256(acknowledgement)
	if err != nil {
		return connectorcontrol.RegistrationResult{}, err
	}
	if status != "active" && status != "consumed" {
		return connectorcontrol.RegistrationResult{}, connectorcontrol.ErrConsumed
	}
	if status == "active" && !now.Before(expiresAt.UTC()) {
		return connectorcontrol.RegistrationResult{}, connectorcontrol.ErrExpired
	}
	var appliedInstallationID, operationID string
	err = tx.QueryRowContext(ctx, `
SELECT installation_id, operation_id
FROM public.xinsight_redeem_connector_invitation($1, $2, $3, $4)`,
		input.InvitationToken, string(acknowledgementPayload), input.ControlToken,
		input.ControlTokenExpires.UTC(),
	).Scan(&appliedInstallationID, &operationID)
	if err != nil {
		return connectorcontrol.RegistrationResult{}, mapConnectorRedemptionError(err, status)
	}
	if appliedInstallationID != installationID {
		return connectorcontrol.RegistrationResult{}, connectorcontrol.ErrConflict
	}
	installation, err := loadConnectorInstallation(ctx, tx, installationID)
	if err != nil {
		return connectorcontrol.RegistrationResult{}, err
	}
	operation, err := loadControlOperation(ctx, tx, operationID)
	if err != nil {
		return connectorcontrol.RegistrationResult{}, err
	}
	legacyInitialOperation := operation.SchemaVersion == connectorcontrol.ControlOperationSchema &&
		operation.Phase == ""
	v2InitialOperation := operation.SchemaVersion == connectorcontrol.ControlOperationSchemaV2 &&
		operation.Phase == connectorcontrol.ControlPhasePreparePausedSource
	if installation.ProjectID != input.ProjectID ||
		!equalConnectorStrings(installation.FinalEventScope, input.FinalEventScope) ||
		installation.InstallationAcknowledgementSHA256 != acknowledgementSHA256 ||
		installation.ControlServiceIdentityID == "" ||
		operation.Kind != "connect" || operation.ProjectID != input.ProjectID ||
		!equalConnectorStrings(operation.EventScope, input.FinalEventScope) ||
		operation.DesiredState != "active" || (!legacyInitialOperation && !v2InitialOperation) {
		return connectorcontrol.RegistrationResult{}, connectorcontrol.ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return connectorcontrol.RegistrationResult{}, err
	}
	return connectorcontrol.RegistrationResult{Installation: installation, Operation: operation}, nil
}

func mapConnectorRedemptionError(err error, invitationStatus string) error {
	var postgresError interface{ SQLState() string }
	if errors.As(err, &postgresError) && postgresError.SQLState() == "23514" {
		if invitationStatus == "consumed" {
			return connectorcontrol.ErrConsumed
		}
		return connectorcontrol.ErrConflict
	}
	return err
}

func equalConnectorStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *WorkspaceStore) EnqueueControlPhase(
	ctx context.Context,
	input connectorcontrol.EnqueueControlPhaseInput,
	now time.Time,
	random io.Reader,
) (connectorcontrol.ControlOperation, error) {
	if s == nil || s.db == nil || !connectorcontrol.IsInstallationID(input.InstallationID) || now.IsZero() {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrInvalid
	}
	if input.Phase == connectorcontrol.ControlPhasePreparePausedSource {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
	}
	now = now.UTC()
	if random == nil {
		random = rand.Reader
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	defer tx.Rollback()
	var lockedInstallationID string
	if err := tx.QueryRowContext(ctx, `
SELECT id
FROM connector_installations
WHERE id = $1
FOR UPDATE`, input.InstallationID).Scan(&lockedInstallationID); errors.Is(err, sql.ErrNoRows) {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrNotFound
	} else if err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	installation, err := loadConnectorInstallation(ctx, tx, lockedInstallationID)
	if err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	if installation.DesiredState != "active" || installation.ProjectID == "" ||
		len(installation.FinalEventScope) != 1 ||
		installation.FinalEventScope[0] != connectorcontrol.CompletedEventType ||
		installation.ControlServiceIdentityID == "" || input.Request.SourceInstance != installation.SourceInstance {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
	}

	var existingOperationID string
	err = tx.QueryRowContext(ctx, `
SELECT operation_id
FROM connector_control_operations
WHERE installation_id = $1 AND phase = $2
FOR UPDATE`, input.InstallationID, input.Phase).Scan(&existingOperationID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return connectorcontrol.ControlOperation{}, err
	}
	if err == nil {
		existing, loadErr := loadControlOperation(ctx, tx, existingOperationID)
		if loadErr != nil {
			return connectorcontrol.ControlOperation{}, loadErr
		}
		expected := controlPhaseOperation(
			existing.OperationID,
			installation,
			input,
			existing.AvailableAt,
		)
		_, expectedSHA, expectedPayload, canonicalErr := connectorcontrol.CanonicalControlOperationRequestV2(expected)
		_, actualSHA, actualPayload, actualErr := connectorcontrol.CanonicalControlOperationRequestV2(existing)
		if canonicalErr != nil || actualErr != nil || expectedSHA != actualSHA ||
			!bytes.Equal(expectedPayload, actualPayload) {
			return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return connectorcontrol.ControlOperation{}, err
		}
		return existing, nil
	}
	if installation.Status != "pending" {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
	}
	expectedObservedState := "controller_pending"
	if installation.ObservedState != expectedObservedState {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
	}

	predecessor, predecessorResult, err := loadCompletedControlPredecessor(
		ctx,
		tx,
		input.InstallationID,
		input.PredecessorOperationID,
		input.PredecessorResultSHA256,
	)
	if err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	if predecessor.CompletedAt == nil || now.Before(*predecessor.CompletedAt) {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
	}
	operationID, err := connectorcontrol.NewID("xco_", random)
	if err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	operation := controlPhaseOperation(operationID, installation, input, now)
	request, requestSHA256, requestPayload, err := connectorcontrol.CanonicalControlOperationRequestV2(operation)
	if err != nil || connectorcontrol.ValidateControlOperationV2Predecessor(request, predecessorResult) != nil ||
		predecessor.OperationID != input.PredecessorOperationID {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO connector_control_operations (
  operation_id, installation_id, operation_kind, phase, desired_state,
  predecessor_operation_id, predecessor_result_sha256,
  request_sha256, request_payload, status, available_at, created_at, updated_at
) VALUES ($1, $2, 'connect', $3, 'active', $4, $5, $6, $7, 'pending', $8, $8, $8)`,
		operation.OperationID,
		operation.InstallationID,
		operation.Phase,
		operation.PredecessorOperationID,
		operation.PredecessorResultSHA256,
		requestSHA256,
		string(requestPayload),
		now,
	); err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	if err := insertAccessAudit(
		ctx,
		tx,
		"system",
		"system",
		"connector_control.enqueue_phase",
		"connector_installation",
		installation.InstallationID,
		"accepted",
		now,
	); err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	created, err := loadControlOperation(ctx, tx, operation.OperationID)
	if err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	if err := tx.Commit(); err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	return created, nil
}

func controlPhaseOperation(
	operationID string,
	installation connectorcontrol.Installation,
	input connectorcontrol.EnqueueControlPhaseInput,
	availableAt time.Time,
) connectorcontrol.ControlOperation {
	return connectorcontrol.ControlOperation{
		SchemaVersion: connectorcontrol.ControlOperationSchemaV2,
		OperationID:   operationID, InstallationID: installation.InstallationID,
		Kind: "connect", Phase: input.Phase,
		ProjectID:               installation.ProjectID,
		EventScope:              append([]string(nil), installation.FinalEventScope...),
		DesiredState:            "active",
		PredecessorOperationID:  input.PredecessorOperationID,
		PredecessorResultSHA256: input.PredecessorResultSHA256,
		Request:                 &input.Request,
		Status:                  "pending", AvailableAt: availableAt, CreatedAt: availableAt, UpdatedAt: availableAt,
	}
}

func loadCompletedControlPredecessor(
	ctx context.Context,
	tx *sql.Tx,
	installationID string,
	operationID string,
	resultSHA256 string,
) (connectorcontrol.ControlOperation, connectorcontrol.ControlOperationResult, error) {
	operation, err := loadControlOperation(ctx, tx, operationID)
	if errors.Is(err, sql.ErrNoRows) {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ControlOperationResult{}, connectorcontrol.ErrConflict
	}
	if err != nil {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ControlOperationResult{}, err
	}
	if operation.InstallationID != installationID || operation.Status != "completed" ||
		operation.ResultSHA256 != resultSHA256 {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ControlOperationResult{}, connectorcontrol.ErrConflict
	}
	result, err := loadControlOperationResult(ctx, tx, operation)
	if err != nil {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ControlOperationResult{}, err
	}
	return operation, result, nil
}

func (s *WorkspaceStore) PollControlOperation(
	ctx context.Context,
	identityID string,
	installationID string,
	now time.Time,
) (connectorcontrol.ControlOperation, error) {
	if s == nil || s.db == nil || !connectorServiceIdentityPattern.MatchString(identityID) ||
		!connectorcontrol.IsInstallationID(installationID) || now.IsZero() {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrInvalid
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	defer tx.Rollback()
	var installationExists bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM connector_installations
  WHERE id = $1 AND control_service_identity_id = $2
)`, installationID, identityID).Scan(&installationExists); err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	if !installationExists {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrNotFound
	}
	var operationID string
	err = tx.QueryRowContext(ctx, `
SELECT operation_id
FROM connector_control_operations
WHERE installation_id = $1
  AND (
    status = 'pending'
    OR (status = 'running' AND lease_expires_at <= $2)
  )
  AND available_at <= $2
ORDER BY created_at, operation_id
LIMIT 1
FOR UPDATE SKIP LOCKED`, installationID, now).Scan(&operationID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `
SELECT operation_id
FROM connector_control_operations
WHERE installation_id = $1 AND status = 'running' AND lease_expires_at > $2
ORDER BY created_at, operation_id
LIMIT 1`, installationID, now).Scan(&operationID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrNotFound
	}
	if err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	leaseExpiresAt := now.Add(30 * time.Second)
	if _, err := tx.ExecContext(ctx, `
UPDATE connector_control_operations
SET status = 'running', attempt_count = attempt_count + 1,
    lease_expires_at = $2, updated_at = $3
WHERE operation_id = $1 AND (status = 'pending' OR lease_expires_at <= $3)`,
		operationID, leaseExpiresAt, now); err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE connector_installations
SET last_agent_seen_at = $2, updated_at = GREATEST(updated_at, $2)
WHERE id = $1`, installationID, now); err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	operation, err := loadControlOperation(ctx, tx, operationID)
	if err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	if err := tx.Commit(); err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	return operation, nil
}

func (s *WorkspaceStore) CompleteControlOperation(
	ctx context.Context,
	identityID string,
	result connectorcontrol.ControlOperationResult,
	now time.Time,
) (connectorcontrol.Installation, error) {
	if s == nil || s.db == nil || !connectorServiceIdentityPattern.MatchString(identityID) || now.IsZero() ||
		connectorcontrol.ValidateControlResult(result, now.UTC()) != nil {
		return connectorcontrol.Installation{}, connectorcontrol.ErrInvalid
	}
	now = now.UTC()
	result.ObservedAt = result.ObservedAt.UTC()
	resultSHA256, resultPayload, err := connectorcontrol.CanonicalSHA256(result)
	if err != nil {
		return connectorcontrol.Installation{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return connectorcontrol.Installation{}, err
	}
	defer tx.Rollback()
	var storedStatus, storedResultSHA, installationID, storedOperationID string
	var storedResultPayload []byte
	err = tx.QueryRowContext(ctx, `
SELECT op.operation_id, op.status, COALESCE(op.result_sha256, ''), op.installation_id,
       COALESCE(op.result_payload::text, '')
FROM connector_control_operations op
JOIN connector_installations installation ON installation.id = op.installation_id
WHERE op.operation_id = $1 AND installation.control_service_identity_id = $2
FOR UPDATE OF op, installation`, result.OperationID, identityID).Scan(
		&storedOperationID, &storedStatus, &storedResultSHA, &installationID, &storedResultPayload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return connectorcontrol.Installation{}, connectorcontrol.ErrNotFound
	}
	if err != nil {
		return connectorcontrol.Installation{}, err
	}
	if installationID != result.InstallationID {
		return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
	}
	operation, err := loadControlOperation(ctx, tx, storedOperationID)
	if err != nil || validateControlResultForOperation(operation, result, now) != nil {
		return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
	}
	if storedStatus == "completed" || storedStatus == "failed" {
		var storedResult connectorcontrol.ControlOperationResult
		if decodeStrictConnectorJSON(storedResultPayload, &storedResult) != nil {
			return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
		}
		storedCanonicalSHA, _, canonicalErr := connectorcontrol.CanonicalSHA256(storedResult)
		if canonicalErr != nil || validateControlResultForOperation(operation, storedResult, now) != nil ||
			storedCanonicalSHA != storedResultSHA || storedResultSHA != resultSHA256 {
			return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
		}
		installation, err := loadConnectorInstallation(ctx, tx, installationID)
		if err != nil {
			return connectorcontrol.Installation{}, err
		}
		if err := tx.Commit(); err != nil {
			return connectorcontrol.Installation{}, err
		}
		return installation, nil
	}
	if storedStatus != "running" {
		return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
	}
	terminalStatus := result.Status
	var reason any
	if result.ReasonCode != "" {
		reason = result.ReasonCode
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE connector_control_operations
SET status = $2, lease_expires_at = NULL, result_sha256 = $3,
    result_payload = $4, reason_code = $5, completed_at = $6, updated_at = $6
WHERE operation_id = $1`, result.OperationID, terminalStatus, resultSHA256,
		string(resultPayload), reason, now); err != nil {
		return connectorcontrol.Installation{}, err
	}
	if result.Status == "completed" {
		sourceInstance := result.SourceInstance
		observedState := "source_ready"
		if operation.SchemaVersion == connectorcontrol.ControlOperationSchemaV2 {
			sourceInstance = result.Result.SourceInstance
			if operation.Phase != connectorcontrol.ControlPhasePreparePausedSource {
				observedState = "controller_pending"
			}
		}
		sourceSHA256 := connectorcontrol.RawSHA256(sourceInstance)
		update, err := tx.ExecContext(ctx, `
UPDATE connector_installations
SET source_instance = $2, source_instance_sha256 = $3,
    status = 'pending', observed_state = $4, status_reason_code = NULL,
    last_agent_seen_at = $5, updated_at = $5, version = version + 1
WHERE id = $1
  AND (source_instance IS NULL OR source_instance = $2)`,
			installationID, sourceInstance, sourceSHA256, observedState, now)
		if err != nil {
			return connectorcontrol.Installation{}, err
		}
		if err := requireOneConnectorRow(update); err != nil {
			return connectorcontrol.Installation{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
UPDATE connector_installations
SET status = 'abnormal', observed_state = 'abnormal', status_reason_code = $2,
    last_agent_seen_at = $3, updated_at = $3, version = version + 1
WHERE id = $1`, installationID, result.ReasonCode, now); err != nil {
			return connectorcontrol.Installation{}, err
		}
	}
	if err := insertAccessAudit(
		ctx, tx, "service", identityID, "connector_control.result",
		"connector_installation", installationID, "accepted", now,
	); err != nil {
		return connectorcontrol.Installation{}, err
	}
	installation, err := loadConnectorInstallation(ctx, tx, installationID)
	if err != nil {
		return connectorcontrol.Installation{}, err
	}
	if err := tx.Commit(); err != nil {
		return connectorcontrol.Installation{}, err
	}
	return installation, nil
}

func validateControlResultForOperation(
	operation connectorcontrol.ControlOperation,
	result connectorcontrol.ControlOperationResult,
	now time.Time,
) error {
	if connectorcontrol.ValidateControlResult(result, now) != nil {
		return connectorcontrol.ErrInvalid
	}
	if operation.SchemaVersion == connectorcontrol.ControlOperationSchemaV2 {
		return connectorcontrol.ValidateControlResultV2ForOperation(operation, result)
	}
	if result.SchemaVersion != connectorcontrol.ControlResultSchema || result.OperationID != operation.OperationID ||
		result.InstallationID != operation.InstallationID || result.ObservedAt.Before(operation.CreatedAt) {
		return connectorcontrol.ErrInvalid
	}
	return nil
}

func loadConnectorInstallation(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	id string,
) (connectorcontrol.Installation, error) {
	return scanConnectorInstallation(queryer.QueryRowContext(ctx, `
SELECT id, deployment_id, connector_type, array_to_json(requested_event_scope)::text,
       COALESCE(array_to_json(final_event_scope)::text, ''), COALESCE(project_id, ''),
       COALESCE(source_instance, ''), COALESCE(source_instance_sha256, ''),
       COALESCE(control_service_identity_id, ''), COALESCE(installation_acknowledgement_sha256, ''),
       status, desired_state, observed_state, COALESCE(status_reason_code, ''),
       last_agent_seen_at, created_at, updated_at, version
FROM connector_installations WHERE id = $1`, id))
}

type connectorRowScanner interface {
	Scan(...any) error
}

func scanConnectorInstallation(scanner connectorRowScanner) (connectorcontrol.Installation, error) {
	var (
		item               connectorcontrol.Installation
		requestedScopeJSON string
		finalScopeJSON     string
		lastAgentSeenAt    sql.NullTime
	)
	err := scanner.Scan(
		&item.InstallationID, &item.DeploymentID, &item.ConnectorType,
		&requestedScopeJSON, &finalScopeJSON, &item.ProjectID,
		&item.SourceInstance, &item.SourceInstanceSHA256,
		&item.ControlServiceIdentityID, &item.InstallationAcknowledgementSHA256,
		&item.Status, &item.DesiredState, &item.ObservedState, &item.StatusReasonCode,
		&lastAgentSeenAt, &item.CreatedAt, &item.UpdatedAt, &item.Version,
	)
	if err != nil {
		return connectorcontrol.Installation{}, err
	}
	item.SchemaVersion = connectorcontrol.InstallationSchema
	if json.Unmarshal([]byte(requestedScopeJSON), &item.RequestedEventScope) != nil {
		return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
	}
	if finalScopeJSON != "" && json.Unmarshal([]byte(finalScopeJSON), &item.FinalEventScope) != nil {
		return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
	}
	if lastAgentSeenAt.Valid {
		value := lastAgentSeenAt.Time.UTC()
		item.LastAgentSeenAt = &value
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	if err := connectorcontrol.ValidateInstallation(item); err != nil {
		return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
	}
	return item, nil
}

func requireOneConnectorRow(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return connectorcontrol.ErrConflict
	}
	return nil
}

func loadControlOperation(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	operationID string,
) (connectorcontrol.ControlOperation, error) {
	var (
		item                    connectorcontrol.ControlOperation
		requestJSON             []byte
		phase                   sql.NullString
		predecessorOperationID  sql.NullString
		predecessorResultSHA256 sql.NullString
		lease                   sql.NullTime
		completed               sql.NullTime
	)
	err := queryer.QueryRowContext(ctx, `
SELECT operation_id, installation_id, operation_kind, phase, desired_state,
       predecessor_operation_id, predecessor_result_sha256,
       request_sha256, request_payload, status, attempt_count, available_at,
       lease_expires_at, completed_at, COALESCE(result_sha256, ''),
       COALESCE(reason_code, ''), created_at, updated_at
FROM connector_control_operations WHERE operation_id = $1`, operationID).Scan(
		&item.OperationID, &item.InstallationID, &item.Kind, &phase, &item.DesiredState,
		&predecessorOperationID, &predecessorResultSHA256,
		&item.RequestSHA256, &requestJSON, &item.Status, &item.AttemptCount, &item.AvailableAt,
		&lease, &completed, &item.ResultSHA256, &item.ReasonCode, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return connectorcontrol.ControlOperation{}, err
	}
	var header struct {
		SchemaVersion string `json:"schema_version"`
	}
	if json.Unmarshal(requestJSON, &header) != nil {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
	}
	switch header.SchemaVersion {
	case connectorcontrol.ControlOperationSchema:
		var request controlOperationRequestWire
		if decodeStrictConnectorJSON(requestJSON, &request) != nil || phase.Valid ||
			predecessorOperationID.Valid || predecessorResultSHA256.Valid ||
			request.OperationID != item.OperationID || request.InstallationID != item.InstallationID ||
			request.Kind != item.Kind || request.DesiredState != item.DesiredState {
			return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
		}
		requestSHA256, _, canonicalErr := connectorcontrol.CanonicalSHA256(request)
		if canonicalErr != nil || requestSHA256 != item.RequestSHA256 {
			return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
		}
		item.SchemaVersion = connectorcontrol.ControlOperationSchema
		item.ProjectID = request.ProjectID
		item.EventScope = append([]string(nil), request.EventScope...)
	case connectorcontrol.ControlOperationSchemaV2:
		request, decodeErr := connectorcontrol.DecodeControlOperationRequestV2(requestJSON)
		if decodeErr != nil || !phase.Valid || request.Phase != phase.String ||
			request.OperationID != item.OperationID || request.InstallationID != item.InstallationID ||
			request.Kind != item.Kind || request.DesiredState != item.DesiredState ||
			request.PredecessorOperationID != predecessorOperationID.String ||
			request.PredecessorResultSHA256 != predecessorResultSHA256.String {
			return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
		}
		requestSHA256, _, canonicalErr := connectorcontrol.CanonicalSHA256(request)
		if canonicalErr != nil || requestSHA256 != item.RequestSHA256 {
			return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
		}
		item.SchemaVersion = connectorcontrol.ControlOperationSchemaV2
		item.Phase = request.Phase
		item.ProjectID = request.ProjectID
		item.EventScope = append([]string(nil), request.EventScope...)
		item.PredecessorOperationID = request.PredecessorOperationID
		item.PredecessorResultSHA256 = request.PredecessorResultSHA256
		phaseRequest := request.Request
		item.Request = &phaseRequest
	default:
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
	}
	item.AvailableAt = item.AvailableAt.UTC()
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	if lease.Valid {
		value := lease.Time.UTC()
		item.LeaseExpiresAt = &value
	}
	if completed.Valid {
		value := completed.Time.UTC()
		item.CompletedAt = &value
	}
	if err := connectorcontrol.ValidateControlOperation(item); err != nil {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
	}
	return item, nil
}

func loadControlOperationResult(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	operation connectorcontrol.ControlOperation,
) (connectorcontrol.ControlOperationResult, error) {
	if operation.Status != "completed" {
		return connectorcontrol.ControlOperationResult{}, connectorcontrol.ErrConflict
	}
	return loadTerminalControlOperationResult(ctx, queryer, operation)
}

func loadTerminalControlOperationResult(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	operation connectorcontrol.ControlOperation,
) (connectorcontrol.ControlOperationResult, error) {
	if operation.Status != "completed" && operation.Status != "failed" {
		return connectorcontrol.ControlOperationResult{}, connectorcontrol.ErrConflict
	}
	var resultJSON []byte
	var resultSHA256 string
	if err := queryer.QueryRowContext(ctx, `
SELECT COALESCE(result_payload::text, ''), COALESCE(result_sha256, '')
FROM connector_control_operations
WHERE operation_id = $1 AND status = $2`, operation.OperationID, operation.Status).Scan(
		&resultJSON,
		&resultSHA256,
	); err != nil {
		return connectorcontrol.ControlOperationResult{}, err
	}
	var result connectorcontrol.ControlOperationResult
	if decodeStrictConnectorJSON(resultJSON, &result) != nil ||
		validateControlResultForOperation(operation, result, operation.UpdatedAt) != nil {
		return connectorcontrol.ControlOperationResult{}, connectorcontrol.ErrConflict
	}
	digest, _, err := connectorcontrol.CanonicalSHA256(result)
	if err != nil || digest != resultSHA256 {
		return connectorcontrol.ControlOperationResult{}, connectorcontrol.ErrConflict
	}
	return result, nil
}

func decodeStrictConnectorJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return connectorcontrol.ErrConflict
	}
	return nil
}
