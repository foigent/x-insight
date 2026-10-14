package workbench

import "time"

// RecentRequests is a page of economic reading members with current detail
// permission. Runtime facts, when requested, come from a separate frozen read.
const RecentRequestsVersion = "xinsight.overview.recent-requests.v1"

type RecentRuntime struct {
	MatchState string     `json:"match_state"`
	RequestRef string     `json:"request_ref,omitempty"`
	EntryAt    *time.Time `json:"entry_at,omitempty"`
	Outcome    string     `json:"outcome,omitempty"`
}

type RecentRequest struct {
	InvestigationAvailable    *bool         `json:"investigation_available,omitempty"`
	RequestID                 string        `json:"request_id"`
	ExecutionID               string        `json:"execution_id"`
	EvidenceEventID           string        `json:"evidence_event_id"`
	SourceSystemID            string        `json:"source_system_id"`
	Model                     string        `json:"model,omitempty"`
	SourceProject             string        `json:"source_project,omitempty"`
	CostProject               string        `json:"cost_project,omitempty"`
	OccurredAt                time.Time     `json:"occurred_at"`
	GatewayChargedMicros      string        `json:"gateway_charged_micros"`
	CalculatedReferenceMicros string        `json:"calculated_reference_micros"`
	PurposeState              string        `json:"purpose_state"`
	PurposeCategory           string        `json:"purpose_category,omitempty"`
	AccountState              string        `json:"account_state"`
	ApplicationState          string        `json:"application_state"`
	ApplicationID             string        `json:"application_id,omitempty"`
	Runtime                   RecentRuntime `json:"runtime"`
}

type RecentRequests struct {
	Version              string          `json:"version"`
	ScopeBasis           string          `json:"scope_basis"`
	DetailScope          string          `json:"detail_scope"`
	ReadingID            string          `json:"reading_id"`
	MembershipRef        string          `json:"membership_ref"`
	Cutoff               time.Time       `json:"cutoff"`
	RuntimeReadingID     string          `json:"runtime_reading_id,omitempty"`
	RuntimeMembershipRef string          `json:"runtime_membership_ref,omitempty"`
	RuntimeCutoff        *time.Time      `json:"runtime_cutoff,omitempty"`
	Items                []RecentRequest `json:"items"`
	NextCursor           string          `json:"next_cursor,omitempty"`
}
