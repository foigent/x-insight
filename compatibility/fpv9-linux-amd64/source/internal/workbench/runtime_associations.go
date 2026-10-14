package workbench

const RuntimeAssociationsVersion = "xinsight.runtime.associations.v1"
const RuntimeExecutionRequestVersion = "xinsight.runtime.execution-request.v1"

type RuntimeExecutionRequest struct {
	Version       string         `json:"version"`
	ReadingID     string         `json:"reading_id"`
	MembershipRef string         `json:"membership_ref"`
	ExecutionID   string         `json:"execution_id"`
	Request       RuntimeRequest `json:"request"`
}

// An exact source request identity is a join, not an inferred causal edge,
// input payload, route proof, or a second economic fact.
type RuntimeEconomicLink struct {
	EconomicFactID  string `json:"economic_fact_id"`
	ExecutionID     string `json:"execution_id"`
	SourceEventHash string `json:"source_event_hash"`
}
type RuntimeAssociation struct {
	Kind     string               `json:"kind"`
	Source   *SourceObservation   `json:"source,omitempty"`
	Economic *RuntimeEconomicLink `json:"economic,omitempty"`
}
type RuntimeAssociations struct {
	ReadingID           string               `json:"reading_id"`
	RequestRef          string               `json:"request_ref"`
	AssociationsVersion string               `json:"associations_version"`
	MembershipRef       string               `json:"membership_ref"`
	Basis               string               `json:"basis"`
	Items               []RuntimeAssociation `json:"items"`
	NextCursor          string               `json:"next_cursor,omitempty"`
}
