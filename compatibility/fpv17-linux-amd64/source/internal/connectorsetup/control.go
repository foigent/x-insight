package connectorsetup

import (
	"context"
	"database/sql"
	"errors"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"io"
	"log"
	"time"
)

type Store interface {
	ConnectorSetupRequestExists(context.Context, string) (bool, error)
	ReadReviewCaptureDefault(context.Context) (db.ReviewCaptureDefault, error)
	CreateSetupInvitation(context.Context, connectorcontrol.CreateInvitationInput, connectorcontrol.SetupConsentPut, string) (connectorcontrol.SetupCreated, error)
	ReadConnectorSetup(context.Context, string) (db.ConnectorSetupRecord, error)
	PrepareConnectorSetupPolicy(context.Context, string, string, time.Time) (int64, error)
	RecordConnectorSetupResult(context.Context, string, bool, string) error
	ListPendingConnectorSetups(context.Context) ([]string, error)
	ReadConnectorSetupReceiving(context.Context, string, time.Time) (connectorcontrol.SetupReceiving, error)
	ReadConnectorSetupReceivingWithOriginalConfig(context.Context, string, time.Time, sourcefacts.ManagedConfig) (connectorcontrol.SetupReceiving, error)
	CheckConnectorSetupControlAccess(context.Context, string, string, time.Time) error
}
type Host interface {
	SetupPeerConfig() (connectorcontrol.InvitationConfig, error)
	AuthorizeSetupActor(string, bool) error
	WithSetupInvitationSeed(context.Context, string, string, connectorcontrol.SetupConsentPut, bool, func(hostctl.SetupInvitationSeed, io.Reader) error) error
	EnsureReviewBodyLoaded(context.Context) error
	ReviewBodyLoaded(context.Context) error
	ReadOriginalSourceConfig(context.Context) (sourcefacts.ManagedConfig, bool, error)
}
type Manager struct {
	Store  Store
	Host   Host
	Policy reviewpolicycontrol.Manager
	Now    func() time.Time
}

func setupStatusFailureClass(err error) (string, string) {
	switch {
	case errors.Is(err, connectorcontrol.ErrNotFound):
		return "not_found", "none"
	case errors.Is(err, connectorcontrol.ErrConflict), errors.Is(err, reviewbody.ErrConflict):
		return "conflict", "none"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline", "none"
	case errors.Is(err, context.Canceled):
		return "context_cancelled", "none"
	}
	var sqlError interface{ SQLState() string }
	if errors.As(err, &sqlError) {
		switch sqlError.SQLState() {
		case "23505", "40001", "42501", "55P03", "57014":
			return "database_sqlstate", sqlError.SQLState()
		}
	}
	return "internal", "none"
}

func logSetupStatusFailure(phase string, err error) {
	category, sqlState := setupStatusFailureClass(err)
	log.Printf("connector setup status action=status phase=%s category=%s sqlstate=%s", phase, category, sqlState)
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}
func custody(p db.ReviewCaptureSettings) *connectorcontrol.SetupCustody {
	return &connectorcontrol.SetupCustody{Mode: p.Mode, RetentionSeconds: p.RetentionSeconds, ProjectQuotaBytes: p.ProjectQuotaBytes, MaxItemBytes: p.MaxItemBytes}
}
func (m *Manager) policy(ctx context.Context) (connectorcontrol.SetupCustodyStatus, error) {
	p, err := m.Store.ReadReviewCaptureDefault(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return connectorcontrol.SetupCustodyStatus{}, nil
	}
	if err != nil {
		return connectorcontrol.SetupCustodyStatus{}, err
	}
	out := connectorcontrol.SetupCustodyStatus{DesiredRevision: p.DesiredRevision, EffectiveRevision: p.EffectiveRevision}
	if p.Effective != nil {
		out.Settings = custody(*p.Effective)
		out.Loaded = p.Effective.Mode == "disabled" || m.Host.ReviewBodyLoaded(ctx) == nil
	}
	return out, nil
}
func (m *Manager) ExecuteSetup(ctx context.Context, r connectorcontrol.SetupLocalRequest) (connectorcontrol.SetupLocalResponse, error) {
	out := connectorcontrol.SetupLocalResponse{SchemaVersion: connectorcontrol.LocalSetupResponseSchema, Action: r.Action}
	if m == nil || m.Store == nil || m.Host == nil || !r.Valid() {
		return out, connectorcontrol.ErrInvalid
	}
	switch r.Action {
	case "preview":
		if err := m.Host.AuthorizeSetupActor(r.ActorSubject, false); err != nil {
			return out, connectorcontrol.ErrSetupUnauthorized
		}
		peer, err := m.Host.SetupPeerConfig()
		if err != nil {
			return out, err
		}
		can := m.Host.AuthorizeSetupActor(r.ActorSubject, true) == nil
		p, err := m.policy(ctx)
		if err != nil {
			return out, err
		}
		if !can {
			p.Settings = nil
			p.Loaded = false
		}
		out.Preview = &connectorcontrol.SetupPreview{SchemaVersion: connectorcontrol.SetupPreviewSchema, DeploymentID: peer.DeploymentID, UROrigin: peer.UniRouterInstallOrigin, Custody: connectorcontrol.SetupPreviewCustody{DesiredRevision: p.DesiredRevision, EffectiveRevision: p.EffectiveRevision, Settings: p.Settings}, CanChangeCustody: can}
		return out, nil
	case "create":
		peer, err := m.Host.SetupPeerConfig()
		if err != nil {
			return out, err
		}
		// Hash includes both static peers and the real XI subject, never caller URLs.
		hash, _, err := connectorcontrol.CanonicalSHA256(struct {
			Actor   string                            `json:"actor"`
			Peer    connectorcontrol.InvitationConfig `json:"peer"`
			Consent connectorcontrol.SetupConsentPut  `json:"consent"`
		}{r.ActorSubject, peer, *r.Consent})
		if err != nil {
			return out, err
		}
		var created connectorcontrol.SetupCreated
		existing, err := m.Store.ConnectorSetupRequestExists(ctx, r.Consent.RequestID)
		if err != nil {
			return out, err
		}
		err = m.Host.WithSetupInvitationSeed(ctx, r.ActorSubject, hash, *r.Consent, existing, func(seed hostctl.SetupInvitationSeed, random io.Reader) error {
			var err error
			created, err = m.Store.CreateSetupInvitation(ctx, connectorcontrol.CreateInvitationInput{ActorSubject: r.ActorSubject, Config: peer, Now: seed.CreatedAt, TTL: seed.ExpiresAt.Sub(seed.CreatedAt), Random: random}, *r.Consent, hash)
			return err
		})
		if err != nil {
			return out, err
		}
		out.Created = &created
		return out, nil
	case "status":
		if r.ActorSubject != "" {
			if err := m.Host.AuthorizeSetupActor(r.ActorSubject, false); err != nil {
				return out, connectorcontrol.ErrSetupUnauthorized
			}
		} else if err := m.Store.CheckConnectorSetupControlAccess(ctx, r.InstallationID, r.ControlIdentityID, m.now()); err != nil {
			return out, err
		}
		record, err := m.Store.ReadConnectorSetup(ctx, r.InstallationID)
		if err != nil {
			logSetupStatusFailure("read_record", err)
			return out, err
		}
		status, err := m.status(ctx, record)
		if err != nil {
			return out, err
		}
		if r.ActorSubject != "" && m.Host.AuthorizeSetupActor(r.ActorSubject, true) != nil {
			status.Custody.Settings = nil
			status.Custody.Loaded = false
		}
		out.Status = &status
		return out, nil
	}
	return out, connectorcontrol.ErrInvalid
}
func (m *Manager) status(ctx context.Context, r db.ConnectorSetupRecord) (connectorcontrol.SetupStatus, error) {
	out := connectorcontrol.SetupStatus{SchemaVersion: connectorcontrol.SetupStatusSchema, InstallationID: r.Installation.InstallationID, DeploymentID: r.Installation.DeploymentID, ConsentRevision: r.Consent.ConsentRevision, State: "waiting_consent", Receiving: connectorcontrol.SetupReceiving{ProfilesReceived: []string{}}, Issues: []connectorcontrol.SetupIssue{}}
	p, err := m.policy(ctx)
	if err != nil {
		logSetupStatusFailure("read_policy", err)
		return out, err
	}
	out.Custody = p
	if r.Installation.DesiredState != "active" || r.Installation.Status == "paused" || r.Installation.Status == "disconnected" {
		out.State = "needs_action"
		out.Issues = append(out.Issues, connectorcontrol.SetupIssue{Code: "installation_inactive", RequiresChoice: true})
		return out, nil
	}
	if r.Installation.InstallationAcknowledgementSHA256 == "" {
		if !m.now().Before(r.Consent.ExpiresAt) {
			out.State = "needs_action"
			out.Issues = append(out.Issues, connectorcontrol.SetupIssue{Code: "invitation_expired", RequiresChoice: true})
		}
		return out, nil
	}
	if r.ControlIdentityIssue == "control_identity_revoked" || r.ControlIdentityIssue == "control_identity_unavailable" {
		out.State = "needs_action"
		out.Issues = append(out.Issues, connectorcontrol.SetupIssue{Code: r.ControlIdentityIssue, RequiresChoice: true})
		return out, nil
	}
	if r.ControlCredentialIssue != "" {
		out.State = "needs_action"
		if r.ControlCredentialIssue == "control_credential_pending" {
			out.State = "provisioning"
		}
		out.Issues = append(out.Issues, connectorcontrol.SetupIssue{Code: r.ControlCredentialIssue, RequiresChoice: r.ControlCredentialIssue != "control_credential_pending"})
		return out, nil
	}
	if r.ControlIdentityIssue != "" {
		out.State = "needs_action"
		out.Issues = append(out.Issues, connectorcontrol.SetupIssue{Code: r.ControlIdentityIssue, RequiresChoice: true})
		return out, nil
	}
	out.State = "provisioning"
	if r.ErrorCode != "" {
		out.State = "needs_action"
		out.Issues = append(out.Issues, connectorcontrol.SetupIssue{Code: r.ErrorCode, RequiresChoice: r.ErrorCode != "custody_loading"})
		return out, nil
	}
	if r.AppliedAt == nil {
		return out, nil
	}
	// The original consent/CAS binds initial application only. Once applied,
	// ordinary authorized policy changes are current state, not setup conflict.
	if p.EffectiveRevision < 1 || p.Settings == nil {
		out.State = "needs_action"
		out.Issues = append(out.Issues, connectorcontrol.SetupIssue{Code: "custody_unavailable", RequiresChoice: true})
		return out, nil
	}
	if p.DesiredRevision != p.EffectiveRevision {
		out.Issues = append(out.Issues, connectorcontrol.SetupIssue{Code: "current_custody_pending"})
		return out, nil
	}

	if !p.Loaded {
		out.Issues = append(out.Issues, connectorcontrol.SetupIssue{Code: "custody_loading"})
		return out, nil
	}
	if r.Installation.Status != "connected" {
		return out, nil
	}
	original, present, err := m.Host.ReadOriginalSourceConfig(ctx)
	if err != nil {
		return out, err
	}
	var receiving connectorcontrol.SetupReceiving
	if present {
		receiving, err = m.Store.ReadConnectorSetupReceivingWithOriginalConfig(ctx, r.Installation.InstallationID, r.Installation.CreatedAt, original)
	} else {
		receiving, err = m.Store.ReadConnectorSetupReceiving(ctx, r.Installation.InstallationID, r.Installation.CreatedAt)
	}
	if err != nil {
		return out, err
	}
	out.Receiving = receiving
	out.State = "waiting_data"
	if receiving.LastSuccessAt != nil {
		out.State = "ready"
	}
	return out, nil
}

// The existing controller's maintenance loop calls Tick. No browser session or
// additional scheduler owns continuation, and every step rechecks authority.
func (m *Manager) Tick(ctx context.Context) error {
	if m == nil || m.Store == nil || m.Host == nil {
		return connectorcontrol.ErrInvalid
	}
	ids, err := m.Store.ListPendingConnectorSetups(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = ctx.Err(); err != nil {
			return err
		}
		record, err := m.Store.ReadConnectorSetup(ctx, id)
		if err != nil {
			return err
		}
		if record.ControlIdentityIssue != "" || record.ControlCredentialIssue != "" {
			continue
		}
		code := ""
		if m.Host.AuthorizeSetupActor(record.Actor, !record.ReusesPolicy) != nil {
			code = "xi_actor_permission_changed"
		}
		var revision int64
		if code == "" {
			revision, err = m.Store.PrepareConnectorSetupPolicy(ctx, id, record.Actor, m.now())
			if err != nil {
				code = "custody_revision_changed"
				if !errors.Is(err, reviewbody.ErrConflict) && !errors.Is(err, connectorcontrol.ErrConflict) {
					return err
				}
			}
		}
		if code == "" {
			current, err := m.Store.ReadReviewCaptureDefault(ctx)
			if err != nil {
				return err
			}
			if current.DesiredRevision != revision {
				code = "custody_revision_changed"
			} else if m.Host.AuthorizeSetupActor(record.Actor, !record.ReusesPolicy) != nil {
				code = "xi_actor_permission_changed"
			} else if current.EffectiveRevision != revision {
				_, err = m.Policy.Execute(ctx, reviewpolicycontrol.Request{SchemaVersion: reviewpolicycontrol.RequestSchema, Action: reviewpolicycontrol.ActionConfirmDefault, ActorSubject: record.Actor, ExpectedRevision: revision, RequestedAt: m.now()})
				if err != nil {
					code = "custody_loading"
					if errors.Is(err, reviewbody.ErrConflict) {
						code = "custody_revision_changed"
					}
				}
			}
			if code == "" {
				current, err = m.Store.ReadReviewCaptureDefault(ctx)
				if err != nil {
					return err
				}
				if current.DesiredRevision != revision || current.EffectiveRevision != revision || current.Effective == nil || *custody(*current.Effective) != record.Consent.Custody {
					code = "custody_revision_changed"
				} else if m.Host.AuthorizeSetupActor(record.Actor, !record.ReusesPolicy) != nil {
					code = "xi_actor_permission_changed"
				} else if current.Effective.Mode == "full_content" && m.Host.EnsureReviewBodyLoaded(ctx) != nil {
					code = "custody_loading"
				}
			}
		}
		if err = m.Store.RecordConnectorSetupResult(ctx, id, code == "", code); err != nil {
			return err
		}
	}
	return nil
}
