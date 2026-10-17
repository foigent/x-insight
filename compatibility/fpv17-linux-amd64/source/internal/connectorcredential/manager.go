package connectorcredential

import (
	"context"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/hostctl"
)

type Store interface {
	IssueControlCredentialRecovery(context.Context, string, string, string, string, string, time.Time, func(string) error) (connectorcontrol.RecoveryMaterial, error)
	ResolveControlCredentialRecovery(context.Context, string, time.Time) (connectorcontrol.RecoveryMaterial, error)
	PrepareControlCredential(context.Context, connectorcontrol.CredentialRequest, string, string, time.Time, func(string) error) (connectorcontrol.CredentialReceipt, error)
	ReadControlCredentialOperation(context.Context, string, string, string, string, time.Time) (connectorcontrol.CredentialReceipt, error)
	ConfirmControlCredential(context.Context, string, connectorcontrol.CredentialConfirm, string, string, time.Time) (connectorcontrol.CredentialReceipt, error)
}

type Host interface {
	AuthorizeSetupActor(string, bool) error
	WithCredentialRecoverySeed(context.Context, string, string, string, func(hostctl.CredentialRecoverySeed) error) error
}

type Manager struct {
	Store Store
	Host  Host
	Now   func() time.Time
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m *Manager) ExecuteCredential(ctx context.Context, r connectorcontrol.CredentialLocalRequest) (connectorcontrol.CredentialLocalResponse, error) {
	out := connectorcontrol.CredentialLocalResponse{SchemaVersion: connectorcontrol.LocalCredentialResponseSchema, Action: r.Action}
	if m == nil || m.Store == nil || m.Host == nil || !r.Valid() {
		return out, connectorcontrol.ErrInvalid
	}
	var err error
	switch r.Action {
	case "issue":
		var material connectorcontrol.RecoveryMaterial
		err = m.Host.WithCredentialRecoverySeed(ctx, r.ActorSubject, r.InstallationID, r.RequestID, func(seed hostctl.CredentialRecoverySeed) error {
			var e error
			material, e = m.Store.IssueControlCredentialRecovery(ctx, r.InstallationID, r.RequestID, r.ActorSubject, seed.RecoveryID, seed.Ticket, m.now(), func(actor string) error { return m.Host.AuthorizeSetupActor(actor, false) })
			return e
		})
		if err == nil {
			out.Recovery = &material
		}
	case "resolve":
		var material connectorcontrol.RecoveryMaterial
		material, err = m.Store.ResolveControlCredentialRecovery(ctx, r.Ticket, m.now())
		if err == nil {
			out.Recovery = &material
		}
	case "rotate", "recover":
		var receipt connectorcontrol.CredentialReceipt
		receipt, err = m.Store.PrepareControlCredential(ctx, *r.Credential, r.IdentityID, r.BearerDigest, m.now(), func(actor string) error { return m.Host.AuthorizeSetupActor(actor, false) })
		if err == nil {
			out.Receipt = &receipt
		}
	case "query":
		var receipt connectorcontrol.CredentialReceipt
		receipt, err = m.Store.ReadControlCredentialOperation(ctx, r.InstallationID, r.OperationID, r.IdentityID, r.BearerDigest, m.now())
		if err == nil {
			out.Receipt = &receipt
		}
	case "confirm":
		var receipt connectorcontrol.CredentialReceipt
		receipt, err = m.Store.ConfirmControlCredential(ctx, r.InstallationID, *r.Confirm, r.IdentityID, r.BearerDigest, m.now())
		if err == nil {
			out.Receipt = &receipt
		}
	}
	if err != nil {
		return out, err
	}
	if !connectorcontrol.ValidCredentialLocalResponse(r, out) {
		return connectorcontrol.CredentialLocalResponse{}, connectorcontrol.ErrInvalid
	}
	return out, nil
}
