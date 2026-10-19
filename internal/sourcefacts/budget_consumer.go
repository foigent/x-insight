package sourcefacts

import (
	"context"
	"errors"
	"github.com/foigent/x-insight/internal/connectorsync"
	"math/big"
	"regexp"
	"slices"
	"time"
)

const BudgetEndpointPath = "/api/v1/integrations/xinsight/source/v2/budgets"

var budgetPositivePattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

// A current-snapshot collection has no financial event cursor. Its catalog
// position only schedules independent atomic source readings.
type BudgetCollection struct {
	Transport         string   `json:"transport"`
	ID                string   `json:"id"`
	SourceSystemID    string   `json:"source_system_id"`
	SourceInstance    string   `json:"source_instance"`
	OrgID             string   `json:"org_id"`
	ProjectID         string   `json:"project_id"`
	ScopeGeneration   string   `json:"scope_generation"`
	Profiles          []string `json:"profiles"`
	Status            string   `json:"status"`
	Revision          int64    `json:"revision"`
	RequiresAttention bool     `json:"requires_attention"`
	LastError         string   `json:"last_error"`
	AccessGeneration  string   `json:"access_generation"`
	CatalogRevision   string   `json:"catalog_revision"`
	GrantedProfiles   []string `json:"granted_profiles"`
	CatalogRaw        []byte   `json:"-"`
	CatalogCursor     string   `json:"-"`
	EntryIndex        int      `json:"-"`
	AlertsCursor      string   `json:"-"`
	CatalogID         string   `json:"-"`
	Cycle             int64    `json:"cycle"`
	VisibleCycle      int64    `json:"visible_cycle"`
}

func ValidBudgetCollection(s BudgetCollection) bool {
	return (s.Transport == "" || s.Transport == "live" || s.Transport == "bounded_offline") && uuidPattern.MatchString(s.ID) && uuidPattern.MatchString(s.SourceSystemID) && boundedText(s.SourceInstance, 128) && budgetPositivePattern.MatchString(s.OrgID) && budgetPositivePattern.MatchString(s.ProjectID) && budgetPositivePattern.MatchString(s.ScopeGeneration) && ValidBudgetProfiles(s.Profiles)
}

func BudgetTransport(s BudgetCollection) string {
	if s.Transport == "" {
		return "live"
	}
	return s.Transport
}

func SameBudgetCollection(a, b BudgetCollection) bool {
	return BudgetTransport(a) == BudgetTransport(b) && a.ID == b.ID && a.SourceSystemID == b.SourceSystemID && a.SourceInstance == b.SourceInstance && a.OrgID == b.OrgID && a.ProjectID == b.ProjectID && a.ScopeGeneration == b.ScopeGeneration && SameBudgetProfiles(a.Profiles, b.Profiles)
}

type BudgetReader interface {
	FetchSourceCapabilities(context.Context) ([]byte, error)
	FetchBudgetCatalog(context.Context, string) ([]byte, error)
	FetchBudgetReading(context.Context, string, string) ([]byte, error)
}

type BudgetStore interface {
	LoadBudgetCollection(context.Context, string) (BudgetCollection, error)
	CommitBudgetCatalog(context.Context, BudgetCollection, []byte, time.Time) error
	CommitBudgetReading(context.Context, BudgetCollection, []byte, time.Time) error
	RecordBudgetFailure(context.Context, BudgetCollection, string, time.Time) error
}

type BudgetConsumer struct {
	Store    BudgetStore
	Reader   BudgetReader
	Expected BudgetCollection
	Now      func() time.Time
}

func PreflightBudget(ctx context.Context, reader BudgetReader, s BudgetCollection) (Capabilities, error) {
	var zero Capabilities
	if reader == nil || !ValidBudgetCollection(s) || BudgetTransport(s) != "live" {
		return zero, ErrInvalid
	}
	raw, err := reader.FetchSourceCapabilities(ctx)
	if err != nil {
		return zero, err
	}
	c, err := DecodeObservationCapabilities(raw)
	if err != nil {
		return zero, err
	}
	if c.SourceInstance != s.SourceInstance || c.ScopeGeneration != s.ScopeGeneration {
		return zero, ErrScopeChanged
	}
	if c.Status != "active" || c.CredentialStatus != "feed_active" {
		return zero, ErrUnavailable
	}
	var profiles []string
	for _, p := range c.Profiles {
		if p == BudgetDefinitionProfile || p == BudgetSummaryProfile || p == BudgetAlertProfile {
			if !slices.Contains(s.Profiles, p) {
				return zero, ErrScopeChanged
			}
			profiles = append(profiles, p)
		}
	}
	if len(profiles) == 0 {
		return zero, ErrUnavailable
	}
	return c, nil
}

// PullOnce performs at most one catalog or current-reading request. Each alert
// continuation is retained as its own source reading; it cannot fill an older
// reading or change its financial comparison.
func (c BudgetConsumer) PullOnce(ctx context.Context) (more bool, err error) {
	if c.Store == nil || c.Reader == nil || !ValidBudgetCollection(c.Expected) {
		return false, ErrInvalid
	}
	s, err := c.Store.LoadBudgetCollection(ctx, c.Expected.ID)
	if err != nil {
		return false, err
	}
	if !SameBudgetCollection(s, c.Expected) {
		return false, ErrScopeChanged
	}
	ctx, finish, gateErr := beginSync(ctx, c.Store, s.SourceInstance, s.OrgID, s.ProjectID, "budget:"+s.ID)
	if gateErr != nil {
		return false, gateErr
	}
	defer func() { finish(err) }()
	if s.Status != "active" || s.RequiresAttention {
		return false, ErrUnavailable
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	fail := func(code string, cause error) (bool, error) {
		if connectorsync.IsControlStop(cause) {
			return false, cause
		}
		if err := c.Store.RecordBudgetFailure(ctx, s, code, now()); err != nil {
			return false, err
		}
		return false, cause
	}
	cap, err := PreflightBudget(ctx, c.Reader, s)
	if err != nil {
		return fail(budgetFailure(err), err)
	}
	if len(s.CatalogRaw) == 0 {
		raw, err := c.Reader.FetchBudgetCatalog(ctx, s.CatalogCursor)
		if err != nil {
			return fail(budgetFailure(err), err)
		}
		catalog, err := DecodeBudgetCatalog(raw)
		if err != nil {
			return fail("invalid_budget_catalog", err)
		}
		if catalog.SourceInstance != s.SourceInstance || catalog.OrgID != s.OrgID || catalog.ProjectID != s.ProjectID {
			return fail("scope_changed", ErrScopeChanged)
		}
		for _, p := range catalog.Profiles {
			if !slices.Contains(s.Profiles, p) || !slices.Contains(cap.Profiles, p) {
				return fail("scope_changed", ErrScopeChanged)
			}
		}
		if err = c.Store.CommitBudgetCatalog(ctx, s, raw, now()); err != nil {
			if errors.Is(err, ErrScopeChanged) {
				return fail("budget_catalog_changed", err)
			}
			if errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalid) {
				return fail("invalid_budget_catalog", err)
			}
			return false, err
		}
		more = len(catalog.Entries) > 0 || catalog.HasMore
		connectorsync.Progress(ctx, more, len(catalog.Entries))
		return more, nil
	}
	catalog, err := DecodeBudgetCatalog(s.CatalogRaw)
	if err != nil || s.EntryIndex < 0 || s.EntryIndex >= len(catalog.Entries) {
		return fail("invalid_budget_position", ErrInvalid)
	}
	entry := catalog.Entries[s.EntryIndex]
	raw, err := c.Reader.FetchBudgetReading(ctx, entry.BudgetRef, s.AlertsCursor)
	if err != nil {
		return fail(budgetFailure(err), err)
	}
	r, err := DecodeBudgetReading(raw)
	if err != nil {
		return fail("invalid_budget_reading", err)
	}
	if err = MatchBudgetReading(r, catalog, entry, s.Profiles); err != nil {
		// A definition/access change between the two source transactions restarts
		// catalog enumeration. It never activates a previously unconfigured grant.
		if r.SourceInstance == s.SourceInstance && r.OrgID == s.OrgID && r.ProjectID == s.ProjectID && (budgetRevisionAfter(r.AccessGeneration, catalog.AccessGeneration) || budgetRevisionAfter(r.CatalogRevision, catalog.CatalogRevision)) {
			return fail("budget_catalog_changed", err)
		}
		return fail("invalid_budget_reading", err)
	}
	if err = c.Store.CommitBudgetReading(ctx, s, raw, now()); err != nil {
		if errors.Is(err, ErrConflict) {
			return fail("source_fact_conflict", err)
		}
		if errors.Is(err, ErrInvalid) {
			return fail("invalid_budget_reading", err)
		}
		return false, err
	}
	more = Text(r.Body(BudgetAlertProfile), "next_cursor") != "" || s.EntryIndex+1 < len(catalog.Entries) || catalog.HasMore
	connectorsync.Progress(ctx, more, 1)
	return more, nil
}

func budgetRevisionAfter(a, b string) bool {
	x, ok := new(big.Int).SetString(a, 10)
	y, other := new(big.Int).SetString(b, 10)
	return ok && other && x.Cmp(y) > 0
}

func budgetFailure(err error) string {
	switch {
	case errors.Is(err, ErrScopeChanged):
		return "scope_changed"
	case errors.Is(err, ErrUnavailable):
		return "source_paused"
	case errors.Is(err, ErrInvalid):
		return "invalid_capabilities"
	default:
		return sourceFailure(err)
	}
}
