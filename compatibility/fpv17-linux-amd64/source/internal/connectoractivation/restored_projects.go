package connectoractivation

import (
	"context"
	"errors"
	"reflect"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

var ErrOriginalReadinessPending = errors.New("reauthorized original consumer custody readiness pending")

func originalRegistrationUsable(status string, attention bool, reason string) bool {
	return status == "active" && !attention || status == "paused" && attention && reason == "restore_requires_authorization"
}

type originalResume struct {
	Streams  []sourcefacts.Stream
	Budgets  []sourcefacts.BudgetCollection
	Contents []sourcefacts.ContentCollection
}

func (r originalResume) restored() bool {
	for _, s := range r.Streams {
		if s.Status == "paused" {
			return true
		}
	}
	for _, s := range r.Budgets {
		if s.Status == "paused" {
			return true
		}
	}
	for _, s := range r.Contents {
		if s.Status == "paused" {
			return true
		}
	}
	return false
}

type originalResumeStore interface {
	ControlSourceFactsStream(context.Context, string, string, int64, bool) (sourcefacts.Stream, error)
	ControlBudgetCollection(context.Context, string, string, int64, bool) (sourcefacts.BudgetCollection, error)
	ControlContentCollection(context.Context, string, string, int64, bool, ...sourcefacts.ContentCollection) (sourcefacts.ContentCollection, error)
}

// All selected registrations have already matched the exact original config
// and authenticated preflight. Start does not acknowledge paused DB rows. A
// later pass may finish custody readiness before any resume is allowed.
func resumeOriginalRegistrations(ctx context.Context, store originalResumeStore, r originalResume, authorize, start, ready func() error) error {
	for _, s := range r.Streams {
		if !originalRegistrationUsable(s.Status, s.RequiresAttention, s.LastError) {
			return sourcefacts.ErrScopeChanged
		}
	}
	for _, s := range r.Budgets {
		if !originalRegistrationUsable(s.Status, s.RequiresAttention, s.LastError) {
			return sourcefacts.ErrScopeChanged
		}
	}
	for _, s := range r.Contents {
		if !originalRegistrationUsable(s.Status, s.RequiresAttention, s.LastError) {
			return sourcefacts.ErrScopeChanged
		}
	}
	if err := authorize(); err != nil {
		return err
	}
	if err := start(); err != nil {
		return err
	}
	if err := ready(); err != nil {
		return err
	}
	if err := authorize(); err != nil {
		return err
	}
	for _, s := range r.Streams {
		if s.Status == "paused" {
			if _, err := store.ControlSourceFactsStream(ctx, s.ID, "active", s.Revision, true); err != nil {
				return err
			}
		}
	}
	for _, s := range r.Budgets {
		if s.Status == "paused" {
			if _, err := store.ControlBudgetCollection(ctx, s.ID, "active", s.Revision, true); err != nil {
				return err
			}
		}
	}
	for _, s := range r.Contents {
		if s.Status == "paused" {
			if _, err := store.ControlContentCollection(ctx, s.ID, "active", s.Revision, true, s); err != nil {
				return err
			}
		}
	}
	return nil
}

// The setup record is the current explicit XI human consent, not permission
// inferred from an old source config or a restore attention reason.
func (p *ManagedProjects) originalSetupAuthority(ctx context.Context, expected connectorcontrol.ManagedBoundary) (db.ConnectorSetupRecord, error) {
	var zero db.ConnectorSetupRecord
	current, err := p.Store.InspectManagedConnector(ctx, expected.Installation.InstallationID)
	if err != nil {
		return zero, err
	}
	if current.SourceBindingSHA256 != expected.SourceBindingSHA256 || current.UniRouterOrigin != expected.UniRouterOrigin ||
		current.Installation.DeploymentID != expected.Installation.DeploymentID || current.Installation.ControlServiceIdentityID != expected.Installation.ControlServiceIdentityID ||
		current.Installation.InstallationAcknowledgementSHA256 == "" || current.Installation.InstallationAcknowledgementSHA256 != expected.Installation.InstallationAcknowledgementSHA256 {
		return zero, connectorcontrol.ErrConflict
	}
	if err := p.Store.CheckConnectorSetupControlAccess(ctx, current.Installation.InstallationID, current.Installation.ControlServiceIdentityID, p.now()); err != nil {
		return zero, err
	}
	if err := p.qualifyRoot(ctx, current); err != nil {
		return zero, err
	}
	r, err := p.Store.ReadConnectorSetup(ctx, current.Installation.InstallationID)
	if err != nil {
		return zero, err
	}
	if !connectorcontrol.ValidSetupConsentView(r.Consent) || r.Consent.InstallationID != current.Installation.InstallationID ||
		r.Consent.DeploymentID != current.Installation.DeploymentID || r.Consent.UROrigin != current.UniRouterOrigin ||
		r.Installation.InstallationAcknowledgementSHA256 != current.Installation.InstallationAcknowledgementSHA256 ||
		(r.ErrorCode != "" && r.ErrorCode != "custody_loading") {
		return zero, connectorcontrol.ErrConflict
	}
	if err := p.Host.AuthorizeSetupActor(r.Actor, !r.ReusesPolicy); err != nil {
		return zero, err
	}
	peer, err := p.Host.SetupPeerConfig()
	if err != nil || peer.DeploymentID != r.Consent.DeploymentID || peer.XInsightOrigin != r.Consent.XIOrigin || peer.UniRouterInstallOrigin != r.Consent.UROrigin {
		return zero, connectorcontrol.ErrConflict
	}
	policy, err := p.Store.ReadReviewCaptureDefault(ctx)
	if err != nil {
		return zero, err
	}
	if r.PolicyRevision < 1 || policy.DesiredRevision != r.PolicyRevision || !sameSetupCustody(policy.Desired, r.Consent.Custody) {
		return zero, ErrOriginalReadinessPending
	}
	return r, nil
}
func sameSetupCustody(p db.ReviewCaptureSettings, c connectorcontrol.SetupCustody) bool {
	return p.Mode == c.Mode && p.RetentionSeconds == c.RetentionSeconds && p.ProjectQuotaBytes == c.ProjectQuotaBytes && p.MaxItemBytes == c.MaxItemBytes
}
func (p *ManagedProjects) restoreOriginalProject(ctx context.Context, b connectorcontrol.ManagedBoundary, base sourcefacts.ManagedConfig, project string, enrollment urmachine.Enrollment, r originalResume) error {
	if len(r.Streams)+len(r.Budgets) > 0 && enrollment.FactsStatus != "active" || len(r.Contents) > 0 && enrollment.ContentStatus != "active" {
		return sourcefacts.ErrScopeChanged
	}
	authorize := func() error {
		if _, err := p.originalSetupAuthority(ctx, b); err != nil {
			return err
		}
		fresh, err := p.Store.InspectManagedConnector(ctx, b.Installation.InstallationID)
		if err != nil {
			return err
		}
		client, err := p.EnrollmentClient(ctx, fresh)
		if err != nil {
			return err
		}
		projects, err := allEnrollmentProjects(ctx, client)
		if err != nil {
			return err
		}
		for _, current := range projects {
			if current.ProjectID != project {
				continue
			}
			for _, grant := range current.Enrollments {
				if grant.EnrollmentID == enrollment.EnrollmentID && reflect.DeepEqual(grant, enrollment) {
					return nil
				}
			}
		}
		return sourcefacts.ErrScopeChanged
	}
	ready := func() error {
		setup, err := p.originalSetupAuthority(ctx, b)
		if err != nil {
			return err
		}
		policy, err := p.Store.ReadReviewCaptureDefault(ctx)
		if err != nil {
			return err
		}
		if policy.EffectiveRevision != setup.PolicyRevision || policy.Effective == nil || !sameSetupCustody(*policy.Effective, setup.Consent.Custody) {
			return ErrOriginalReadinessPending
		}
		if policy.Effective.Mode == "full_content" && p.Host.ReviewBodyLoaded(ctx) != nil {
			return ErrOriginalReadinessPending
		}
		return nil
	}
	return resumeOriginalRegistrations(ctx, p.Sources, r, authorize, func() error { return p.Host.EnsureOriginalSourceWorker(ctx, base) }, ready)
}
