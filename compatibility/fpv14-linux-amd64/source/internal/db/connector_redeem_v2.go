package db

import (
	"context"
	"database/sql"
	"errors"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"io"
	"time"
)

func (s *WorkspaceStore) RedeemAdoptionInvitation(ctx context.Context, r connectorcontrol.RedeemInvitationInputV2, now time.Time, _ io.Reader) (connectorcontrol.RedeemInvitationResultV2, error) {
	var out connectorcontrol.RedeemInvitationResultV2
	if s == nil || s.db == nil || connectorcontrol.ValidateRedeemInvitationV2(r, now.UTC()) != nil {
		return out, connectorcontrol.ErrInvalid
	}
	digest, _ := connectorcontrol.InvitationTokenDigest(r.InvitationToken)
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var id, deployment, status string
	var created, expires time.Time
	e = tx.QueryRowContext(ctx, `SELECT i.installation_id,c.deployment_id,i.status,i.created_at,i.expires_at
 FROM connector_installation_invitations i JOIN connector_installations c ON c.id=i.installation_id WHERE i.nonce_digest=$1`, digest).Scan(&id, &deployment, &status, &created, &expires)
	if errors.Is(e, sql.ErrNoRows) {
		return out, connectorcontrol.ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if s.connectorDeploymentID != "" && deployment != s.connectorDeploymentID {
		return out, connectorcontrol.ErrConflict
	}

	b := r.ReceiverAdoptionBootstrap
	sourceSHA, e := connectorcontrol.SourceBindingDigest(deployment, b)
	if e != nil || b.InstallationID != id || sourceSHA != r.ConsentSHA256 || r.ConfirmedAt.Before(created) || !r.ConfirmedAt.Before(expires) {
		return out, connectorcontrol.ErrConflict
	}
	if status != "active" && status != "consumed" {
		return out, connectorcontrol.ErrConsumed
	}
	if status == "active" && !now.Before(expires) {
		return out, connectorcontrol.ErrExpired
	}
	ack := connectorcontrol.InstallationAcknowledgement{SchemaVersion: connectorcontrol.AcknowledgementSchema, InstallationID: id, DeploymentID: deployment, ProjectID: r.ProjectID, FinalEventScope: r.FinalEventScope, ConsentSHA256: r.ConsentSHA256, ConfirmedAt: r.ConfirmedAt.UTC()}
	ackSHA, ackRaw, e := connectorcontrol.CanonicalSHA256(ack)
	if e != nil {
		return out, e
	}
	_, bootstrapRaw, e := connectorcontrol.CanonicalSHA256(b)
	if e != nil {
		return out, e
	}
	requestSHA, _, e := connectorcontrol.CanonicalSHA256(r)
	if e != nil {
		return out, e
	}
	var applied string
	e = tx.QueryRowContext(ctx, `SELECT installation_id FROM public.xinsight_redeem_connector_adoption_invitation($1,$2,$3,$4,$5,$6)`, r.InvitationToken, string(ackRaw), r.ControlToken, r.ControlTokenExpires.UTC(), string(bootstrapRaw), requestSHA).Scan(&applied)
	if e != nil {
		return out, mapConnectorRedemptionError(e, status)
	}
	if applied != id {
		return out, connectorcontrol.ErrConflict
	}
	i, e := loadConnectorInstallation(ctx, tx, id)
	if e != nil {
		return out, e
	}
	if i.ProjectID != r.ProjectID || i.ControlServiceIdentityID == "" || i.InstallationAcknowledgementSHA256 != ackSHA || !equalConnectorStrings(i.FinalEventScope, r.FinalEventScope) {
		return out, connectorcontrol.ErrConflict
	}
	out = connectorcontrol.RedeemInvitationResultV2{SchemaVersion: connectorcontrol.RedeemInvitationResultSchemaV2, Installation: i, ReceiverAdoptionBootstrapSHA256: r.ReceiverAdoptionBootstrapSHA256}
	return out, tx.Commit()
}
