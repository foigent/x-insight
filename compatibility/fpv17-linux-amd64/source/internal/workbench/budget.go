package workbench

import (
	"encoding/json"
	"regexp"
	"slices"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

const BudgetContractVersion = "xinsight.budget-reading.v1"
const BudgetInterpretation = "current-authorized-source-snapshot;original-period-and-units;same-reading-comparison;no-local-detail-addition;independent-alert-status.v1"

var budgetReadingPattern = regexp.MustCompile(`^xbd_[A-Za-z0-9_-]{22}$`)
var budgetItemPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

func ValidBudgetReadingID(id string) bool { return budgetReadingPattern.MatchString(id) }
func ValidBudgetItemRef(id string) bool   { return budgetItemPattern.MatchString(id) }

type BudgetReadingRequest struct {
	ContractVersion string  `json:"contract_version"`
	Filters         Filters `json:"filters"`
}

func (r BudgetReadingRequest) Validate() error {
	if r.ContractVersion != BudgetContractVersion {
		return ErrUnsupportedVersion
	}
	if err := validateFilters(r.Filters); err != nil {
		return err
	}
	// Source snapshots have no per-request membership proving a cost-project
	// allocation. An economic filter must not silently narrow a project budget.
	if len(r.Filters.CostProjects) > 0 {
		return ErrInvalid
	}
	return nil
}

type BudgetReading struct {
	ID                            string    `json:"reading_id"`
	ContractVersion               string    `json:"contract_version"`
	Cutoff                        time.Time `json:"cutoff"`
	ExpiresAt                     time.Time `json:"expires_at"`
	AuthorizationRevision         string    `json:"authorization_revision"`
	InterpretationRevision        string    `json:"interpretation_revision"`
	MembershipRef                 string    `json:"membership_ref"`
	BudgetCount                   int64     `json:"budget_count"`
	CalculatedBudgetSourceState   string    `json:"calculated_budget_source_state"`
	IndependentInvoiceSourceState string    `json:"independent_invoice_source_state"`
}

type BudgetProfileView struct {
	State      string         `json:"state"`
	Body       map[string]any `json:"body,omitempty"`
	SourceHash string         `json:"source_hash,omitempty"`
}

type BudgetItem struct {
	Ref                   string            `json:"ref"`
	BudgetRef             string            `json:"budget_ref"`
	SourceSystemID        string            `json:"source_system_id"`
	SourceInstance        string            `json:"source_instance"`
	OrgID                 string            `json:"org_id"`
	ProjectID             string            `json:"project_id"`
	SourceReadingID       string            `json:"source_reading_id"`
	ReadingCutoff         string            `json:"source_reading_cutoff"`
	ObservedAt            string            `json:"source_observed_at"`
	ReceivedAt            time.Time         `json:"received_at"`
	DefinitionRevision    string            `json:"definition_revision"`
	BindingRevision       string            `json:"binding_revision,omitempty"`
	CurrentRetrievalState string            `json:"current_retrieval_state"`
	SourceState           string            `json:"source_state"`
	Definition            BudgetProfileView `json:"definition"`
	Summary               BudgetProfileView `json:"summary"`
	Alerts                BudgetProfileView `json:"alerts"`
	AlertsPage            string            `json:"alerts_page,omitempty"`
	DetailRelationship    string            `json:"detail_relationship"`
	RuntimeEffect         string            `json:"runtime_effect"`
}

type BudgetItems struct {
	ReadingID  string       `json:"reading_id"`
	Items      []BudgetItem `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

func BudgetGrantProfiles(g Grant) []string {
	var profiles []string
	if g.AllowBudgetDefinition {
		profiles = append(profiles, sourcefacts.BudgetDefinitionProfile)
	}
	if g.AllowBudgetSummary {
		profiles = append(profiles, sourcefacts.BudgetSummaryProfile)
	}
	if g.AllowBudgetAlert {
		profiles = append(profiles, sourcefacts.BudgetAlertProfile)
	}
	return profiles
}

// ProjectBudget never exposes an ungranted document or reconstructs its amount
// through the summary's denominator/remaining/threshold. A granted alert may
// independently expose its original trigger amounts, not the current budget.
func ProjectBudget(r sourcefacts.BudgetReading, viewerProfiles, sourceProfiles []string) BudgetItem {
	i := BudgetItem{BudgetRef: r.BudgetRef, SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, SourceReadingID: r.ReadingID, ReadingCutoff: r.ReadingCutoff, ObservedAt: r.ObservedAt, DefinitionRevision: r.DefinitionRevision, BindingRevision: r.BindingRevision, RuntimeEffect: "report_only", DetailRelationship: "request_membership_not_provided"}
	profile := func(name string) BudgetProfileView {
		if !slices.Contains(viewerProfiles, name) {
			return BudgetProfileView{State: "not_authorized"}
		}
		if !slices.Contains(sourceProfiles, name) {
			return BudgetProfileView{State: "source_profile_not_authorized"}
		}
		for _, d := range r.Documents {
			if d.Profile == name {
				// Projection changes must not mutate retained source bytes or another
				// caller's view of this same reading.
				raw, _ := json.Marshal(d.Document.Body)
				var body map[string]any
				_ = json.Unmarshal(raw, &body)
				return BudgetProfileView{State: "available", Body: body, SourceHash: d.SourceSHA256}
			}
		}
		return BudgetProfileView{State: "source_profile_not_authorized"}
	}
	i.Definition = profile(sourcefacts.BudgetDefinitionProfile)
	i.Summary = profile(sourcefacts.BudgetSummaryProfile)
	i.Alerts = profile(sourcefacts.BudgetAlertProfile)
	if i.Summary.Body != nil && i.Definition.State != "available" {
		state := "definition_not_authorized"
		if i.Definition.State == "source_profile_not_authorized" {
			state = "source_definition_not_authorized"
		}
		i.Summary.Body["comparison"] = map[string]any{"state": state}
		// This hash belongs to the complete source summary, which includes the
		// suppressed comparison. The API exposes only a qualified projection.
		i.Summary.SourceHash = ""
	}
	return i
}
