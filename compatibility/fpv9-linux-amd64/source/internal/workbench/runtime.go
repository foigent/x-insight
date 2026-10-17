package workbench

import (
	"regexp"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

const RuntimeContractVersion = "xinsight.runtime-reading.v1"
const RuntimeMetricVersion = "xinsight.runtime.request-observations.v1"

var runtimeReadingPattern = regexp.MustCompile(`^xrt_[A-Za-z0-9_-]{22}$`)
var runtimeRequestPattern = regexp.MustCompile(`^xrr_[a-f0-9]{64}$`)

func ValidRuntimeReadingID(id string) bool  { return runtimeReadingPattern.MatchString(id) }
func ValidRuntimeRequestRef(id string) bool { return runtimeRequestPattern.MatchString(id) }

type RuntimeReadingRequest struct {
	ContractVersion string      `json:"contract_version"`
	MetricVersion   string      `json:"metric_version"`
	Period          PeriodInput `json:"period"`
	Filters         Filters     `json:"filters"`
}

func (r RuntimeReadingRequest) Resolve(now time.Time) (Period, error) {
	if r.ContractVersion != RuntimeContractVersion || r.MetricVersion != RuntimeMetricVersion {
		return Period{}, ErrUnsupportedVersion
	}
	if err := validateFilters(r.Filters); err != nil {
		return Period{}, err
	}
	return ResolvePeriod(r.Period, now)
}

type RuntimeReading struct {
	ContentInterpretationRevision string          `json:"content_interpretation_revision,omitempty"`
	ContentAuthorizationRevision  string          `json:"content_authorization_revision,omitempty"`
	ID                            string          `json:"reading_id"`
	ContractVersion               string          `json:"contract_version"`
	MetricVersion                 string          `json:"metric_version"`
	Period                        Period          `json:"period"`
	Cutoff                        time.Time       `json:"cutoff"`
	ExpiresAt                     time.Time       `json:"expires_at"`
	AuthorizationRevision         string          `json:"authorization_revision"`
	InterpretationRevision        string          `json:"interpretation_revision"`
	MembershipRef                 string          `json:"membership_ref"`
	RequestsWithEntryTime         int64           `json:"requests_with_entry_time"`
	RequestsWithUnknownEntryTime  int64           `json:"requests_with_unknown_entry_time"`
	ObservationCount              int64           `json:"observation_count"`
	Coverage                      RuntimeCoverage `json:"coverage"`
}

type RuntimeCoverage struct {
	PeriodCompleteness    string                     `json:"period_completeness"`
	TimelineComplete      bool                       `json:"timeline_complete"`
	TimeBasis             string                     `json:"time_basis"`
	UnknownEntryTimeBasis string                     `json:"unknown_entry_time_basis"`
	MissingPredecessors   int64                      `json:"missing_predecessors"`
	Streams               []RuntimeStreamObservation `json:"streams"`
	Gaps                  []string                   `json:"gaps"`
}

type RuntimeStreamObservation struct {
	SourceSystemID    string `json:"source_system_id"`
	SourceInstance    string `json:"source_instance"`
	OrgID             string `json:"org_id"`
	ProjectID         string `json:"project_id"`
	Profile           string `json:"profile"`
	State             string `json:"state"`
	ScopeGeneration   string `json:"scope_generation"`
	ReceivedWatermark string `json:"received_watermark,omitempty"`
	RequiresAttention bool   `json:"requires_attention"`
}

type RuntimeRequest struct {
	MemberBasis       string     `json:"member_basis,omitempty"`
	ContentEligibleAt *time.Time `json:"content_eligible_at,omitempty"`
	Ref               string     `json:"request_ref"`
	SourceSystemID    string     `json:"source_system_id"`
	SourceInstance    string     `json:"source_instance"`
	OrgID             string     `json:"org_id"`
	ProjectID         string     `json:"project_id"`
	RequestID         string     `json:"request_id"`
	EntryAt           *time.Time `json:"entry_at,omitempty"`
	EntryTimeState    string     `json:"entry_time_state"`
	LastObservedAt    time.Time  `json:"last_observed_at,omitzero"`
	ObservationCount  int64      `json:"observation_count"`
	Outcome           string     `json:"outcome,omitempty"`
	OutcomeBasis      string     `json:"outcome_basis,omitempty"`
}

type RuntimeRequests struct {
	ReadingID  string           `json:"reading_id"`
	Items      []RuntimeRequest `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// The document contains only one explicitly authorized B profile. A context
// reference and route reference are omitted here; their separate authorized
// association must supply them rather than widening a B field grant.
type RuntimeObservation struct {
	Representation string                  `json:"representation"`
	RequestRef     string                  `json:"request_ref"`
	SourceSystemID string                  `json:"source_system_id"`
	SourceHash     string                  `json:"source_hash"`
	EnvelopeHash   string                  `json:"envelope_hash"`
	ChainState     string                  `json:"chain_state"`
	Document       sourcefacts.Observation `json:"document"`
}

type RuntimeObservations struct {
	ReadingID  string               `json:"reading_id"`
	Items      []RuntimeObservation `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

type RuntimeMeasurementExclusions struct {
	NotApplicable       int64 `json:"not_applicable"`
	NotObserved         int64 `json:"not_observed"`
	LegacyUnknown       int64 `json:"legacy_unknown"`
	Invalid             int64 `json:"invalid"`
	MissingPredecessor  int64 `json:"missing_predecessor"`
	MissingContext      int64 `json:"missing_context"`
	ConflictingContext  int64 `json:"conflicting_context"`
	IncompatibleContext int64 `json:"incompatible_context"`
}

type RuntimeMeasurementGroup struct {
	Operation          string                       `json:"operation"`
	StreamMode         string                       `json:"stream_mode"`
	Kind               string                       `json:"measurement_kind"`
	SourceVersion      string                       `json:"measurement_version"`
	CompletionEvidence string                       `json:"completion_evidence"`
	EntryTimeState     string                       `json:"entry_time_state"`
	ObservationCount   int64                        `json:"observation_count"`
	Excluded           RuntimeMeasurementExclusions `json:"excluded"`
	Percentiles        RuntimePercentiles           `json:"percentiles"`
}

type RuntimeMeasurements struct {
	ReadingID              string                    `json:"reading_id"`
	EligibilityVersion     string                    `json:"eligibility_version"`
	InterpretationRevision string                    `json:"interpretation_revision"`
	Items                  []RuntimeMeasurementGroup `json:"items"`
	NextCursor             string                    `json:"next_cursor,omitempty"`
}

func RuntimeCollectionProfile(collection string) (string, bool) {
	profiles := map[string]string{"observations": "", "runtime": sourcefacts.RuntimeProfile, "attempts": sourcefacts.AttemptProfile, "capabilities": sourcefacts.CapabilityProfile, "policies": sourcefacts.PolicyProfile, "destinations": sourcefacts.DestinationProfile}
	profile, ok := profiles[collection]
	return profile, ok
}
