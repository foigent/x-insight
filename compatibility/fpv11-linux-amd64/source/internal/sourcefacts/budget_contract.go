package sourcefacts

import (
	"embed"
	"encoding/json"
	"math/big"
	"slices"
	"sync"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	BudgetVersion           = "unirouter.budget-snapshot.v1"
	BudgetCatalogVersion    = "unirouter.budget-catalog.v1"
	BudgetDefinitionProfile = "unirouter.budget-definition.v1"
	BudgetSummaryProfile    = "unirouter.budget-period-summary.v1"
	BudgetAlertProfile      = "unirouter.budget-alert.v1"
	// Includes three bounded documents and the reading envelope, not an event page.
	MaxBudgetReadingBytes = 3*MaxDocumentBytes + 16<<10
	MaxBudgetCatalogBytes = 64 << 10
)

//go:embed contract/budgets/*.json
var budgetAssets embed.FS

var budgetSchemas = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	result := map[string]*jsonschema.Schema{}
	for _, name := range []string{"budget-reading.schema.json", "budget-catalog.schema.json"} {
		b, err := budgetAssets.ReadFile("contract/budgets/" + name)
		if err != nil {
			return nil, err
		}
		var raw map[string]any
		if err = json.Unmarshal(b, &raw); err != nil {
			return nil, err
		}
		id := Text(raw, "$id")
		if err = compiler.AddResource(id, raw); err != nil {
			return nil, err
		}
		s, err := compiler.Compile(id)
		if err != nil {
			return nil, err
		}
		result[name] = s
	}
	return result, nil
})

type BudgetCatalogEntry struct {
	BudgetRef          string `json:"budget_ref"`
	Origin             string `json:"origin"`
	DefinitionRevision string `json:"definition_revision"`
	BindingRevision    string `json:"binding_revision,omitempty"`
}

type BudgetCatalog struct {
	SchemaVersion    string               `json:"schema_version"`
	SourceInstance   string               `json:"source_instance"`
	OrgID            string               `json:"org_id"`
	ProjectID        string               `json:"project_id"`
	ReadingCutoff    string               `json:"reading_cutoff"`
	AccessGeneration string               `json:"access_generation"`
	CatalogRevision  string               `json:"catalog_revision"`
	Profiles         []string             `json:"profiles"`
	EnterpriseScope  string               `json:"enterprise_scope"`
	Entries          []BudgetCatalogEntry `json:"entries"`
	HasMore          bool                 `json:"has_more"`
	NextCursor       string               `json:"next_cursor"`
}

type BudgetEnvelope struct {
	SchemaVersion      string         `json:"schema_version"`
	SourceInstance     string         `json:"source_instance"`
	OrgID              string         `json:"org_id"`
	ProjectID          string         `json:"project_id"`
	ReadingID          string         `json:"reading_id"`
	ReadingCutoff      string         `json:"reading_cutoff"`
	ObservedAt         string         `json:"observed_at"`
	BudgetRef          string         `json:"budget_ref"`
	DefinitionRevision string         `json:"definition_revision"`
	BindingRevision    string         `json:"binding_revision"`
	Profile            string         `json:"profile"`
	Body               map[string]any `json:"body"`
}

type BudgetDocument struct {
	Profile      string         `json:"profile"`
	Document     BudgetEnvelope `json:"document"`
	SourceSHA256 string         `json:"source_sha256"`
}

type BudgetReading struct {
	SchemaVersion      string           `json:"schema_version"`
	SourceInstance     string           `json:"source_instance"`
	OrgID              string           `json:"org_id"`
	ProjectID          string           `json:"project_id"`
	ReadingID          string           `json:"reading_id"`
	ReadingCutoff      string           `json:"reading_cutoff"`
	ObservedAt         string           `json:"observed_at"`
	BudgetRef          string           `json:"budget_ref"`
	DefinitionRevision string           `json:"definition_revision"`
	BindingRevision    string           `json:"binding_revision,omitempty"`
	AccessGeneration   string           `json:"access_generation"`
	CatalogRevision    string           `json:"catalog_revision"`
	Documents          []BudgetDocument `json:"documents"`
	Canonical          []byte           `json:"-"`
	SHA256             string           `json:"-"`
}

func ValidBudgetProfiles(profiles []string) bool {
	if len(profiles) < 1 || len(profiles) > 3 {
		return false
	}
	seen := map[string]bool{}
	for _, p := range profiles {
		if (p != BudgetDefinitionProfile && p != BudgetSummaryProfile && p != BudgetAlertProfile) || seen[p] {
			return false
		}
		seen[p] = true
	}
	return true
}

func SameBudgetProfiles(a, b []string) bool {
	if !ValidBudgetProfiles(a) || !ValidBudgetProfiles(b) || len(a) != len(b) {
		return false
	}
	for _, p := range a {
		if !slices.Contains(b, p) {
			return false
		}
	}
	return true
}

func decodeBudgetObject(raw []byte, name string, limit int) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > limit {
		return nil, ErrInvalid
	}
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return nil, ErrInvalid
	}
	schemas, err := budgetSchemas()
	if err != nil {
		return nil, err
	}
	if schemas[name].Validate(m) != nil {
		return nil, ErrInvalid
	}
	return m, nil
}

func DecodeBudgetCatalog(raw []byte) (BudgetCatalog, error) {
	var c BudgetCatalog
	if _, err := decodeBudgetObject(raw, "budget-catalog.schema.json", MaxBudgetCatalogBytes); err != nil {
		return c, err
	}
	if json.Unmarshal(raw, &c) != nil {
		return c, ErrInvalid
	}
	if c.HasMore != (c.NextCursor != "") || len(c.NextCursor) > 4096 || c.HasMore && len(c.Entries) == 0 {
		return c, ErrInvalid
	}
	for i, e := range c.Entries {
		if i > 0 && c.Entries[i-1].BudgetRef >= e.BudgetRef {
			return c, ErrInvalid
		}
	}
	return c, nil
}

// DecodeBudgetReading validates the wire contract without inventing absent
// profiles. Admission additionally calls MatchBudgetReading with the current
// catalog and explicit registration. It never infers a grant from the payload.
func DecodeBudgetReading(raw []byte) (BudgetReading, error) {
	var r BudgetReading
	m, err := decodeBudgetObject(raw, "budget-reading.schema.json", MaxBudgetReadingBytes)
	if err != nil {
		return r, err
	}
	if json.Unmarshal(raw, &r) != nil {
		return r, ErrInvalid
	}
	seen := map[string]bool{}
	for i, w := range r.Documents {
		d := w.Document
		if seen[w.Profile] || w.Profile != d.Profile {
			return r, ErrInvalid
		}
		seen[w.Profile] = true
		if d.SchemaVersion != r.SchemaVersion || d.SourceInstance != r.SourceInstance || d.OrgID != r.OrgID || d.ProjectID != r.ProjectID || d.ReadingID != r.ReadingID || d.ReadingCutoff != r.ReadingCutoff || d.ObservedAt != r.ObservedAt || d.BudgetRef != r.BudgetRef || d.DefinitionRevision != r.DefinitionRevision || d.BindingRevision != r.BindingRevision {
			return r, ErrInvalid
		}
		original := m["documents"].([]any)[i].(map[string]any)["document"]
		canonical, err := connector.CanonicalizeJSONValue(original)
		if err != nil || len(canonical) > MaxDocumentBytes || Hash(canonical) != w.SourceSHA256 {
			return r, ErrInvalid
		}
		b := d.Body
		switch w.Profile {
		case BudgetDefinitionProfile:
			if Text(b, "revision") != r.DefinitionRevision || !validBudgetDefinition(b) {
				return r, ErrInvalid
			}
		case BudgetSummaryProfile:
			if !validBudgetSummary(b, r.ReadingCutoff) {
				return r, ErrInvalid
			}
		case BudgetAlertProfile:
			more, _ := b["has_more"].(bool)
			if more != (Text(b, "next_cursor") != "") || len(Text(b, "next_cursor")) > 4096 {
				return r, ErrInvalid
			}
			ids := map[string]bool{}
			for _, raw := range b["entries"].([]any) {
				a := raw.(map[string]any)
				id := Text(a, "event_ref")
				if ids[id] || Text(a, "status_cutoff") != r.ReadingCutoff || Text(a, "delivery_cutoff") != r.ReadingCutoff {
					return r, ErrInvalid
				}
				ids[id] = true
			}
		}
	}
	// A source may legitimately authorize the summary alone. Only compare the
	// definition when both documents are in this same atomic reading.
	d, s := r.Body(BudgetDefinitionProfile), r.Body(BudgetSummaryProfile)
	if d != nil && s != nil {
		if !budgetAuthorityMatchesOrigin(Text(d, "origin"), Text(s, "authority")) {
			return r, ErrInvalid
		}
		if Text(d, "origin") == "gateway_project" && Text(s, "period_state") == "original_utc_period" {
			cutoff, _ := time.Parse(time.RFC3339Nano, r.ReadingCutoff)
			cutoff = cutoff.UTC()
			var start, end time.Time
			switch Text(d, "period_kind") {
			case "monthly":
				start = time.Date(cutoff.Year(), cutoff.Month(), 1, 0, 0, 0, 0, time.UTC)
				end = start.AddDate(0, 1, 0)
			case "daily":
				start = time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, time.UTC)
				end = start.AddDate(0, 0, 1)
			default:
				return r, ErrInvalid
			}
			a, ae := time.Parse(time.RFC3339Nano, Text(s, "period_start"))
			b, be := time.Parse(time.RFC3339Nano, Text(s, "period_end"))
			if ae != nil || be != nil || !a.Equal(start) || !b.Equal(end) {
				return r, ErrInvalid
			}
		}
	}
	if d != nil && s != nil && Text(Object(s, "comparison"), "state") == "comparable" {
		amount, used := Object(d, "amount"), Object(s, "known_subtotal")
		if Text(d, "setting_state") != "active" || amount == nil || !budgetUnitsEqual(amount, used) || Text(amount, "value_minor") != Text(Object(s, "comparison"), "utilization_denominator") {
			return r, ErrInvalid
		}
	}
	r.Canonical, err = connector.CanonicalizeJSONValue(m)
	if err != nil {
		return r, ErrInvalid
	}
	r.SHA256 = Hash(r.Canonical)
	return r, nil
}

func (r BudgetReading) Body(profile string) map[string]any {
	for _, w := range r.Documents {
		if w.Profile == profile {
			return w.Document.Body
		}
	}
	return nil
}

func (r BudgetReading) Profiles() []string {
	p := make([]string, 0, len(r.Documents))
	for _, w := range r.Documents {
		p = append(p, w.Profile)
	}
	return p
}

// MatchBudgetReading rejects omissions and surplus grants alike, and binds
// every admitted reading to its source identity, access generation and exact
// catalog entry. A new catalog may legitimately narrow the configured set.
func MatchBudgetReading(r BudgetReading, c BudgetCatalog, e BudgetCatalogEntry, configured []string) error {
	if !ValidBudgetProfiles(configured) || !ValidBudgetProfiles(c.Profiles) {
		return ErrInvalid
	}
	for _, p := range c.Profiles {
		if !slices.Contains(configured, p) {
			return ErrScopeChanged
		}
	}
	if !SameBudgetProfiles(r.Profiles(), c.Profiles) {
		return ErrScopeChanged
	}
	if r.SourceInstance != c.SourceInstance || r.OrgID != c.OrgID || r.ProjectID != c.ProjectID || r.AccessGeneration != c.AccessGeneration || r.CatalogRevision != c.CatalogRevision || r.BudgetRef != e.BudgetRef || r.DefinitionRevision != e.DefinitionRevision || r.BindingRevision != e.BindingRevision {
		return ErrScopeChanged
	}
	if d := r.Body(BudgetDefinitionProfile); d != nil && Text(d, "origin") != e.Origin {
		return ErrScopeChanged
	}
	// The catalog supplies origin even when definition access is not granted.
	if s := r.Body(BudgetSummaryProfile); s != nil && !budgetAuthorityMatchesOrigin(e.Origin, Text(s, "authority")) {
		return ErrScopeChanged
	}
	if !slices.Contains(c.Entries, e) {
		return ErrInvalid
	}
	return nil
}

func budgetAuthorityMatchesOrigin(origin, authority string) bool {
	switch origin {
	case "gateway_project":
		return authority == "usage_finalization_journal.settled.charged_cost_micros"
	case "enterprise_project":
		return authority == "enterprise_finance_allocation_results.project_slice"
	default:
		return false
	}
}

func budgetUnitsEqual(a, b map[string]any) bool {
	return a != nil && b != nil && Text(a, "currency") == Text(b, "currency") && Text(a, "scale") == Text(b, "scale")
}

func validBudgetDefinition(d map[string]any) bool {
	a := Object(d, "amount")
	switch Text(d, "setting_state") {
	case "active":
		return a != nil && Text(a, "value_minor") != "0" && slices.Contains([]string{"daily", "monthly", "fiscal_period"}, Text(d, "period_kind"))
	case "unset":
		return a == nil
	case "inactive_zero":
		return a != nil && Text(a, "value_minor") == "0"
	case "inactive_period_none":
		return Text(d, "period_kind") == "none"
	case "not_approved":
		return Text(d, "approval_state") != "approved"
	default:
		return true
	}
}

func validBudgetSummary(s map[string]any, cutoff string) bool {
	if Text(s, "amount_cutoff") != cutoff {
		return false
	}
	c := Object(s, "coverage")
	if Text(c, "state") == "complete_settled_as_of" {
		start, e1 := time.Parse(time.RFC3339Nano, Text(s, "period_start"))
		certified, e2 := time.Parse(time.RFC3339Nano, Text(c, "certified_from"))
		if e1 != nil || e2 != nil || start.Before(certified) || len(c["reasons"].([]any)) != 0 || Text(c, "materialization_pending") != "0" || Text(c, "coverage_epoch") == "" {
			return false
		}
	}
	comparison := Object(s, "comparison")
	if Text(comparison, "state") != "comparable" {
		return true
	}
	if Text(c, "state") != "complete_settled_as_of" && Text(c, "state") != "exact" {
		return false
	}
	used, remaining := Object(s, "known_subtotal"), Object(comparison, "remaining")
	if !budgetUnitsEqual(used, remaining) || Text(used, "value_minor") != Text(comparison, "utilization_numerator") {
		return false
	}
	n, nOK := new(big.Int).SetString(Text(comparison, "utilization_numerator"), 10)
	d, dOK := new(big.Int).SetString(Text(comparison, "utilization_denominator"), 10)
	if !nOK || !dOK || d.Sign() <= 0 {
		return false
	}
	if new(big.Int).Sub(d, n).String() != Text(remaining, "value_minor") {
		return false
	}
	state := "at"
	if n.Cmp(d) < 0 {
		state = "below"
	} else if n.Cmp(d) > 0 {
		state = "above"
	}
	return Text(comparison, "threshold_state") == state
}
