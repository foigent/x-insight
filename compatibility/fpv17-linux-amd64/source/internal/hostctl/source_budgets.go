package hostctl

import (
	"context"
	"slices"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func (e *Engine) manageBudgetCollection(ctx context.Context, store *db.SourceFactsStore, r SourceFactsRequest) (any, error) {
	if r.Action == "budget-offline-register" || r.Action == "budget-offline-enable" {
		product := buildinfo.Current()
		a, err := sourcefacts.LoadBudgetOfflineAuthorization(r.AuthorizationFile, r.TargetRef, product.ProductVersion, product.Commit, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		if r.Action == "budget-offline-register" {
			return store.RegisterBudgetOffline(ctx, a)
		}
		return store.ControlBudgetOffline(ctx, a, r.Revision, r.Acknowledge)
	}
	switch r.Action {
	case "budget-status":
		return store.LoadBudgetCollection(ctx, r.ID)
	case "budget-pause":
		return store.ControlBudgetCollection(ctx, r.ID, "paused", r.Revision, false)
	case "budget-revoke":
		return store.ControlBudgetCollection(ctx, r.ID, "revoked", r.Revision, false)
	case "budget-register", "budget-preflight", "budget-enable", "budget-resume":
	default:
		return nil, sourcefacts.ErrInvalid
	}
	c, err := sourcefacts.LoadManagedConfig(e.Paths.SourceFactsConfig)
	if err != nil {
		return nil, err
	}
	var selected *sourcefacts.ManagedBudgetConfig
	for _, p := range c.Budgets {
		if p.CollectionID == r.ID {
			selected = &p
			break
		}
	}
	if selected == nil {
		return nil, sourcefacts.ErrInvalid
	}
	if r.Action == "budget-register" {
		return store.RegisterBudgetCollection(ctx, selected.Registration())
	}
	live, err := store.LoadBudgetCollection(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if !sourcefacts.SameBudgetCollection(live, selected.Registration()) {
		return nil, sourcefacts.ErrScopeChanged
	}
	key, err := config.LoadSourceFactsServiceKey(selected.KeyFile)
	if err != nil {
		return nil, err
	}
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: selected.Endpoint, ServiceKey: key, AllowPrivateTunnelHTTP: selected.AllowPrivateTunnelHTTP, RequestTimeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	cap, err := sourcefacts.PreflightBudget(ctx, reader, live)
	if err != nil {
		return nil, err
	}
	raw, err := reader.FetchBudgetCatalog(ctx, "")
	if err != nil {
		return nil, err
	}
	catalog, err := sourcefacts.DecodeBudgetCatalog(raw)
	if err != nil {
		return nil, err
	}
	if catalog.SourceInstance != live.SourceInstance || catalog.OrgID != live.OrgID || catalog.ProjectID != live.ProjectID {
		return nil, sourcefacts.ErrScopeChanged
	}
	for _, p := range catalog.Profiles {
		if !slices.Contains(live.Profiles, p) || !slices.Contains(cap.Profiles, p) {
			return nil, sourcefacts.ErrScopeChanged
		}
	}
	if r.Action == "budget-preflight" {
		return map[string]any{"collection_id": live.ID, "status": "preflight_passed", "profiles": catalog.Profiles}, nil
	}
	return store.ControlBudgetCollection(ctx, r.ID, "active", r.Revision, r.Acknowledge)
}
