package connectoractivation

import (
	"context"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"slices"
	"strconv"
)

// UR's active status after bind-existing is rechecked using the exact original
// root configuration and authenticated capability reads. No new key or cursor.
func (p *ManagedProjects) qualifyOriginalProject(ctx context.Context, b connectorcontrol.ManagedBoundary, project string, enrollment urmachine.Enrollment) (sourcefacts.ManagedConfig, bool, error) {
	base, present, err := p.Host.InspectOriginalSourceConfig(ctx)
	if err != nil || !present {
		return base, false, err
	}
	verified, ok := b.VerifiedSource()
	if !ok {
		return base, false, connectorcontrol.ErrConflict
	}
	source, org := verified.SourceInstance, strconv.FormatInt(verified.OrgID, 10)
	cfg := base
	cfg.Profiles = nil
	cfg.Budgets = nil
	cfg.Contents = nil
	cfg.Bindings = nil
	found := false
	resume := originalResume{}
	for _, profile := range base.Profiles {
		if profile.SourceInstance != source || profile.OrgID != org || profile.ProjectID != project {
			continue
		}
		found = true
		if enrollment.FactsStatus == "revoked" || enrollment.FactsStatus == "not_requested" {
			continue
		}
		if !slices.Contains(enrollment.Profiles, profile.Profile) {
			return cfg, true, sourcefacts.ErrScopeChanged
		}
		live, err := p.Sources.LoadSourceFactsStream(ctx, profile.StreamID)
		if err != nil || !profile.Matches(live) || !originalRegistrationUsable(live.Status, live.RequiresAttention, live.LastError) {
			return cfg, true, sourcefacts.ErrScopeChanged
		}
		reader, err := p.sourceReader(profile.Endpoint, profile.KeyFile)
		if err != nil {
			return cfg, true, err
		}
		err = sourcefacts.Preflight(ctx, reader, live)
		reader.CloseContentConnections()
		if err != nil {
			return cfg, true, err
		}
		resume.Streams = append(resume.Streams, live)
		cfg.Profiles = append(cfg.Profiles, profile)
	}
	for _, budget := range base.Budgets {
		if budget.SourceInstance != source || budget.OrgID != org || budget.ProjectID != project {
			continue
		}
		found = true
		if enrollment.FactsStatus == "revoked" || enrollment.FactsStatus == "not_requested" {
			continue
		}
		for _, profile := range budget.Profiles {
			if !slices.Contains(enrollment.Profiles, profile) {
				return cfg, true, sourcefacts.ErrScopeChanged
			}
		}
		live, err := p.Sources.LoadBudgetCollection(ctx, budget.CollectionID)
		if err != nil || !sourcefacts.SameBudgetCollection(live, budget.Registration()) || !originalRegistrationUsable(live.Status, live.RequiresAttention, live.LastError) {
			return cfg, true, sourcefacts.ErrScopeChanged
		}
		reader, err := p.sourceReader(budget.Endpoint, budget.KeyFile)
		if err != nil {
			return cfg, true, err
		}
		_, err = sourcefacts.PreflightBudget(ctx, reader, budget.Registration())
		reader.CloseContentConnections()
		if err != nil {
			return cfg, true, err
		}
		resume.Budgets = append(resume.Budgets, live)
		cfg.Budgets = append(cfg.Budgets, budget)
	}
	for _, content := range base.Contents {
		if content.SourceInstance != source || content.OrgID != org || content.ProjectID != project {
			continue
		}
		found = true
		if !enrollment.IncludeContent || enrollment.ContentStatus == "revoked" || enrollment.ContentStatus == "not_requested" {
			continue
		}
		resolved, err := base.ResolveContent(content)
		if err != nil {
			return cfg, true, err
		}
		live, err := p.Sources.LoadContentCollection(ctx, content.CollectionID)
		if err != nil || !sourcefacts.SameContentCollection(live, resolved.Registration()) || !originalRegistrationUsable(live.Status, live.RequiresAttention, live.LastError) {
			return cfg, true, sourcefacts.ErrScopeChanged
		}
		reader, err := p.sourceReader(content.Endpoint, resolved.KeyFile)
		if err != nil {
			return cfg, true, err
		}
		_, err = sourcefacts.PreflightContent(ctx, reader, live)
		reader.CloseContentConnections()
		if err != nil {
			return cfg, true, err
		}
		resume.Contents = append(resume.Contents, live)
		cfg.Contents = append(cfg.Contents, content)
	}
	if !found {
		return cfg, false, nil
	}
	actual := []string{}
	for _, profile := range cfg.Profiles {
		actual = append(actual, profile.Profile)
	}
	for _, budget := range cfg.Budgets {
		actual = append(actual, budget.Profiles...)
	}
	slices.Sort(actual)
	if enrollment.FactsStatus != "revoked" && enrollment.FactsStatus != "not_requested" && !slices.Equal(actual, enrollment.Profiles) {
		return cfg, true, sourcefacts.ErrScopeChanged
	}
	if enrollment.IncludeContent && enrollment.ContentStatus != "revoked" && enrollment.ContentStatus != "not_requested" && len(cfg.Contents) != 1 {
		return cfg, true, sourcefacts.ErrScopeChanged
	}
	cfg = sourcefacts.ManagedProjectConfig(base, source, org, project,
		enrollment.FactsStatus != "revoked" && enrollment.FactsStatus != "not_requested",
		enrollment.IncludeContent && enrollment.ContentStatus != "revoked" && enrollment.ContentStatus != "not_requested")
	if err := cfg.Validate(); err != nil {
		return cfg, true, err
	}
	_, _, running := p.Host.ReadOriginalSourceConfig(ctx)
	if resume.restored() || running != nil {
		if err := p.restoreOriginalProject(ctx, b, base, project, enrollment, resume); err != nil {
			return cfg, true, err
		}
	}
	// Restoration owns the authorization and active-state transition. Bind only
	// after it succeeds, including unchanged active enrollments on a later pass.
	if err := p.ensureManagedContentProject(ctx, b, cfg); err != nil {
		return cfg, true, err
	}
	return cfg, true, nil
}
