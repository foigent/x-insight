package workbench

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	ContractVersion = "xinsight.workbench.v1"
	MetricVersion   = "xinsight.workbench.cost.v1"
	ReadingTTL      = 30 * time.Minute
	MaxMembers      = 500000
	MaxPageSize     = 100
)

var (
	ErrInvalid            = errors.New("invalid workbench request")
	ErrUnsupportedVersion = errors.New("unsupported workbench version")
	ErrScopeForbidden     = errors.New("workbench scope is forbidden")
	ErrNotFound           = errors.New("workbench record not found")
	ErrExpired            = errors.New("workbench reading expired")
	ErrAccessChanged      = errors.New("workbench reading access changed")
	ErrTooLarge           = errors.New("workbench reading is too large")
	ErrConflict           = errors.New("workbench record conflicts")
	ErrStaleRevision      = errors.New("workbench revision is stale")

	idPattern             = regexp.MustCompile(`^xwr_[A-Za-z0-9_-]{22}$`)
	savedFilterIDPattern  = regexp.MustCompile(`^xwf_[A-Za-z0-9_-]{22}$`)
	idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,159}$`)
)

type MoneyBasis string

const (
	MoneyGatewayCharged      MoneyBasis = "gateway_charged"
	MoneyCalculatedReference MoneyBasis = "calculated_reference"
)

type PeriodInput struct {
	RelativeHours   int        `json:"relative_hours,omitempty"`
	StartInclusive  *time.Time `json:"start_inclusive,omitempty"`
	EndExclusive    *time.Time `json:"end_exclusive,omitempty"`
	DisplayTimezone string     `json:"display_timezone"`
}

type Period struct {
	StartInclusive time.Time `json:"start_inclusive"`
	EndExclusive   time.Time `json:"end_exclusive"`
	Timezone       string    `json:"timezone"`
}

type Filters struct {
	SourceIDs      []string `json:"source_ids"`
	SourceProjects []string `json:"source_projects"`
	CostProjects   []string `json:"cost_projects"`
}

type ReadingRequest struct {
	ContractVersion string      `json:"contract_version"`
	Page            string      `json:"page"`
	MetricVersion   string      `json:"metric_version"`
	Period          PeriodInput `json:"period"`
	MoneyBasis      MoneyBasis  `json:"money_basis"`
	Currency        string      `json:"currency"`
	Filters         Filters     `json:"filters"`
}

type Coverage struct {
	Status                       string     `json:"status"`
	PeriodCompleteness           string     `json:"period_completeness"`
	ProjectionCompleteForMembers bool       `json:"projection_complete_for_members"`
	CaptureStartedAt             *time.Time `json:"capture_started_at,omitempty"`
	LastReceivedAt               *time.Time `json:"last_received_at,omitempty"`
	ReceivedWatermark            string     `json:"received_watermark,omitempty"`
	ProjectionWatermark          string     `json:"projection_watermark,omitempty"`
	KnownItems                   int64      `json:"known_items"`
	Gaps                         []string   `json:"gaps"`
}

type Reading struct {
	ID                     string          `json:"reading_id"`
	ContractVersion        string          `json:"contract_version"`
	MetricVersion          string          `json:"metric_version"`
	Period                 Period          `json:"period"`
	Cutoff                 time.Time       `json:"cutoff"`
	ExpiresAt              time.Time       `json:"expires_at"`
	MoneyBasis             MoneyBasis      `json:"money_basis"`
	MoneyTitle             string          `json:"money_title"`
	Currency               string          `json:"currency"`
	MembershipRef          string          `json:"membership_ref"`
	InterpretationRevision string          `json:"interpretation_revision"`
	AuthorizationRevision  string          `json:"authorization_revision"`
	Dimensions             []string        `json:"dimensions"`
	Coverage               Coverage        `json:"coverage"`
	Purpose                *PurposeReading `json:"purpose,omitempty"`
	SourceManifestRef      string          `json:"source_manifest_ref,omitempty"`
	SourceCoverage         *SourceCoverage `json:"source_coverage,omitempty"`
}

type Summary struct {
	ReadingID                 string          `json:"reading_id"`
	MoneyBasis                MoneyBasis      `json:"money_basis"`
	Currency                  string          `json:"currency"`
	RequestCount              int64           `json:"request_count"`
	RequestTotalComplete      bool            `json:"request_total_complete"`
	SelectedAmountMicros      string          `json:"selected_amount_micros"`
	GatewayChargedMicros      string          `json:"gateway_charged_micros"`
	CalculatedReferenceMicros string          `json:"calculated_reference_micros"`
	AmountComplete            bool            `json:"amount_complete"`
	PurposeKnownRequests      int64           `json:"purpose_known_requests"`
	ActorKnownRequests        int64           `json:"actor_known_requests"`
	Coverage                  Coverage        `json:"coverage"`
	SourceCoverage            *SourceCoverage `json:"source_coverage,omitempty"`
}

type Ratio struct {
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
	Unit        string `json:"unit"`
}

type Contribution struct {
	Ref                       string `json:"ref"`
	Label                     string `json:"label"`
	RequestCount              int64  `json:"request_count"`
	SelectedAmountMicros      string `json:"selected_amount_micros"`
	GatewayChargedMicros      string `json:"gateway_charged_micros"`
	CalculatedReferenceMicros string `json:"calculated_reference_micros"`
	Share                     Ratio  `json:"share"`
	DrilldownAllowed          bool   `json:"drilldown_allowed"`
}

type Contributions struct {
	OrderBy              string         `json:"order_by"`
	DenominatorRequests  int64          `json:"denominator_requests"`
	UnassignedRequests   int64          `json:"unassigned_requests"`
	OtherKnownRequests   int64          `json:"other_known_requests"`
	ReadingID            string         `json:"reading_id"`
	Dimension            string         `json:"dimension"`
	Mode                 string         `json:"mode"`
	Rows                 []Contribution `json:"rows"`
	OtherKnownMicros     string         `json:"other_known_micros"`
	UnassignedMicros     string         `json:"unassigned_micros"`
	UnassignedScope      string         `json:"unassigned_scope"`
	DenominatorMicros    string         `json:"denominator_micros"`
	NextCursor           string         `json:"next_cursor,omitempty"`
	IsLastPage           bool           `json:"is_last_page"`
	AuthorizedGroupCount int            `json:"authorized_group_count"`
}

type DetailRow struct {
	InvestigationAvailable    *bool               `json:"investigation_available,omitempty"`
	EconomicFactID            string              `json:"-"`
	RequestID                 string              `json:"request_id"`
	ExecutionID               string              `json:"execution_id"`
	EvidenceEventID           string              `json:"evidence_event_id"`
	EventHash                 string              `json:"event_hash"`
	SourceSystemID            string              `json:"source_system_id"`
	SourceVersion             string              `json:"source_version"`
	Model                     string              `json:"model,omitempty"`
	SourceProject             string              `json:"source_project,omitempty"`
	CostProject               string              `json:"cost_project,omitempty"`
	TerminalStatus            string              `json:"terminal_status"`
	OccurredAt                time.Time           `json:"occurred_at"`
	GatewayChargedMicros      string              `json:"gateway_charged_micros"`
	CalculatedReferenceMicros string              `json:"calculated_reference_micros"`
	AccountState              string              `json:"account_state"`
	ApplicationState          string              `json:"application_state"`
	PurposeState              string              `json:"purpose_state"`
	SourceFacts               []SourceObservation `json:"source_facts,omitempty"`
}

type Details struct {
	ReadingID       string      `json:"reading_id"`
	ContributionRef string      `json:"contribution_ref"`
	Rows            []DetailRow `json:"rows"`
	NextCursor      string      `json:"next_cursor,omitempty"`
}

type SourceStatus struct {
	SourceFacts         []SourceFactsStatus `json:"source_facts,omitempty"`
	SourceSystemID      string              `json:"source_system_id"`
	DisplayName         string              `json:"display_name"`
	Status              string              `json:"status"`
	CaptureStartedAt    *time.Time          `json:"capture_started_at"`
	LastAttemptAt       *time.Time          `json:"last_attempt_at"`
	LastReceivedAt      *time.Time          `json:"last_received_at"`
	ReceivedWatermark   string              `json:"received_watermark,omitempty"`
	ProjectionWatermark string              `json:"projection_watermark,omitempty"`
	ScopeLabel          string              `json:"scope_label"`
	Gaps                []string            `json:"gaps"`
}

type SourceFactsStatus struct {
	StreamID          string     `json:"stream_id"`
	Profile           string     `json:"profile"`
	SourceInstance    string     `json:"source_instance"`
	OrgID             string     `json:"org_id"`
	ProjectID         string     `json:"project_id"`
	ScopeGeneration   string     `json:"scope_generation"`
	Status            string     `json:"status"`
	RequiresAttention bool       `json:"requires_attention"`
	LastError         string     `json:"last_error,omitempty"`
	LastReceivedAt    *time.Time `json:"last_received_at"`
}

type SourceStatuses struct {
	ReadingID string         `json:"reading_id"`
	Items     []SourceStatus `json:"items"`
}

type SeriesPoint struct {
	StartInclusive time.Time `json:"start_inclusive"`
	EndExclusive   time.Time `json:"end_exclusive"`
	KnownValue     string    `json:"known_value"`
	Complete       bool      `json:"complete"`
}

type Series struct {
	ReadingID     string        `json:"reading_id"`
	Metric        string        `json:"metric"`
	Unit          string        `json:"unit"`
	BucketSeconds int64         `json:"bucket_seconds"`
	Points        []SeriesPoint `json:"points"`
	Coverage      Coverage      `json:"coverage"`
}

type SavedFilterCommand struct {
	Name             string         `json:"name"`
	Config           ReadingRequest `json:"config"`
	ExpectedRevision int64          `json:"expected_revision"`
	IdempotencyKey   string         `json:"idempotency_key"`
}

type SavedFilter struct {
	ID           string         `json:"id"`
	OwnerSubject string         `json:"owner_subject"`
	Name         string         `json:"name"`
	Config       ReadingRequest `json:"config"`
	Revision     int64          `json:"revision"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type SavedFilterList struct {
	Items []SavedFilter `json:"items"`
}

type Grant struct {
	ID                     string
	SubjectID              string
	SourceSystemID         string
	SourceProject          string
	CostProject            string
	AllowSummary           bool
	AllowDetails           bool
	AllowCoverage          bool
	AllowPreferences       bool
	AllowSourceContext     bool
	AllowSourceIdentity    bool
	AllowSourceCost        bool
	AllowSourceRuntime     bool
	AllowSourceAttempt     bool
	AllowSourceCapability  bool
	AllowSourcePolicy      bool
	AllowSourceDestination bool
	AllowResource          bool
	AllowBudgetDefinition  bool
	AllowBudgetSummary     bool
	AllowBudgetAlert       bool
	AllowPurposeRead       bool
	AllowPurposeReview     bool
	AllowContent           bool
	AllowedContentPurposes []string
	LegacyRevision         int64
	ContentRevision        int64
	Revision               int64
}

type Authorization struct {
	Revision            string
	SourceRevision      string
	PurposeRevision     string
	ObservationRevision string
	BudgetRevision      string
	ContentRevision     string
	Grants              []Grant
}

// Counts describe authorized source documents, never extra calls or charges.
type SourceCoverage struct {
	RequiresAttention     bool     `json:"requires_attention,omitempty"`
	KnownDocuments        int64    `json:"known_documents"`
	PendingPredecessor    int64    `json:"pending_predecessor"`
	EconomicNotReceived   int64    `json:"economic_not_received"`
	EconomicHashConflicts int64    `json:"economic_hash_conflicts"`
	MediaPending          int64    `json:"media_pending"`
	NotCaptured           int64    `json:"not_captured"`
	Profiles              []string `json:"profiles"`
	Gaps                  []string `json:"gaps"`
	CollectionStates      []string `json:"collection_states,omitempty"`
}

type SourceObservation struct {
	EconomicFactID string         `json:"-"`
	SourceSystemID string         `json:"source_system_id"`
	RequestID      string         `json:"request_id"`
	Profile        string         `json:"profile"`
	Stage          string         `json:"stage"`
	SourceFactID   string         `json:"source_fact_id"`
	Revision       string         `json:"revision"`
	SourceHash     string         `json:"source_hash"`
	EnvelopeHash   string         `json:"envelope_hash"`
	ChainState     string         `json:"chain_state"`
	EconomicState  string         `json:"economic_state"`
	CaptureStatus  string         `json:"capture_status"`
	OccurredAt     time.Time      `json:"occurred_at"`
	RecordedAt     time.Time      `json:"recorded_at"`
	Context        map[string]any `json:"context,omitempty"`
	Identity       map[string]any `json:"identity,omitempty"`
	CostBasis      map[string]any `json:"cost_basis,omitempty"`
}

type SourceObservations struct {
	ReadingID   string              `json:"reading_id"`
	ManifestRef string              `json:"manifest_ref"`
	Items       []SourceObservation `json:"items"`
	NextCursor  string              `json:"next_cursor,omitempty"`
}

func (request ReadingRequest) Resolve(now time.Time) (Period, error) {
	if request.ContractVersion != ContractVersion || request.MetricVersion != MetricVersion {
		return Period{}, ErrUnsupportedVersion
	}
	if request.Page != "cost" || request.Currency != "CNY" ||
		(request.MoneyBasis != MoneyGatewayCharged && request.MoneyBasis != MoneyCalculatedReference) {
		return Period{}, ErrInvalid
	}
	if err := validateFilters(request.Filters); err != nil {
		return Period{}, err
	}
	return ResolvePeriod(request.Period, now)
}

// ResolvePeriod keeps the same bounded period contract for separately typed
// cost and runtime readings; runtime does not inherit money configuration.
func ResolvePeriod(period PeriodInput, now time.Time) (Period, error) {
	timezone := strings.TrimSpace(period.DisplayTimezone)
	if timezone == "" || len(timezone) > 128 {
		return Period{}, ErrInvalid
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Period{}, ErrInvalid
	}
	now = now.UTC()
	if period.RelativeHours != 0 {
		if period.StartInclusive != nil || period.EndExclusive != nil {
			return Period{}, ErrInvalid
		}
		switch period.RelativeHours {
		case 24, 72, 168, 720:
		default:
			return Period{}, ErrInvalid
		}
		return Period{StartInclusive: now.Add(-time.Duration(period.RelativeHours) * time.Hour), EndExclusive: now, Timezone: timezone}, nil
	}
	if period.StartInclusive == nil || period.EndExclusive == nil {
		return Period{}, ErrInvalid
	}
	start := period.StartInclusive.UTC()
	end := period.EndExclusive.UTC()
	// A reading can only freeze evidence that exists at the server cutoff. Do
	// not accept a future end and let the database turn it into an internal
	// error; callers can retry with a current cutoff.
	if !start.Before(end) || end.Sub(start) > 31*24*time.Hour || end.After(now) {
		return Period{}, ErrInvalid
	}
	return Period{StartInclusive: start, EndExclusive: end, Timezone: timezone}, nil
}

func ValidateSavedFilter(command SavedFilterCommand) error {
	return ValidateSavedFilterAt(command, time.Now().UTC())
}

func ValidateSavedFilterAt(command SavedFilterCommand, now time.Time) error {
	name := strings.TrimSpace(command.Name)
	if name == "" || name != command.Name || len(name) > 120 ||
		!idempotencyKeyPattern.MatchString(command.IdempotencyKey) || command.ExpectedRevision < 0 {
		return ErrInvalid
	}
	_, err := command.Config.Resolve(now.UTC())
	return err
}

func ValidReadingID(value string) bool { return idPattern.MatchString(value) }

func ValidSavedFilterID(value string) bool { return savedFilterIDPattern.MatchString(value) }

func NewID(prefix string) (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate workbench id: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(entropy[:]), nil
}

func MoneyTitle(basis MoneyBasis) string {
	if basis == MoneyGatewayCharged {
		return "网关扣费"
	}
	return "网关计算参考值"
}

func SelectedMicros(basis MoneyBasis, charged, calculated string) string {
	if basis == MoneyGatewayCharged {
		return charged
	}
	return calculated
}

func SortStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func validateFilters(filters Filters) error {
	for _, values := range [][]string{filters.SourceIDs, filters.SourceProjects, filters.CostProjects} {
		if len(values) > 100 {
			return ErrInvalid
		}
		seen := map[string]struct{}{}
		for _, value := range values {
			if value == "" || strings.TrimSpace(value) != value || len(value) > 512 {
				return ErrInvalid
			}
			if _, exists := seen[value]; exists {
				return ErrInvalid
			}
			seen[value] = struct{}{}
		}
	}
	return nil
}
