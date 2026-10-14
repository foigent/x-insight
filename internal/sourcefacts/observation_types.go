// Source observation wire types follow the frozen UR 59f7712d contract.
// B has its own identity domain; no B field is stored as an A document stage.
package sourcefacts

type Observation struct {
	SchemaVersion     string                  `json:"schema_version"`
	SourceInstance    string                  `json:"source_instance"`
	OrgID             string                  `json:"org_id"`
	ProjectID         string                  `json:"project_id"`
	RequestID         string                  `json:"request_id"`
	Profile           string                  `json:"profile"`
	SubjectKind       string                  `json:"subject_kind"`
	SubjectID         string                  `json:"subject_id"`
	Kind              string                  `json:"observation_kind"`
	SourceFactID      string                  `json:"source_fact_id"`
	Revision          string                  `json:"revision"`
	EventID           string                  `json:"event_id"`
	PreviousRevision  string                  `json:"previous_revision,omitempty"`
	PreviousHash      string                  `json:"previous_hash,omitempty"`
	CorrectionReason  string                  `json:"correction_reason,omitempty"`
	OccurredAt        string                  `json:"occurred_at"`
	RecordedAt        string                  `json:"recorded_at"`
	Coverage          string                  `json:"coverage"`
	ContextRef        string                  `json:"context_ref,omitempty"`
	RouteEvidenceHash string                  `json:"route_evidence_hash,omitempty"`
	CaptureExpected   []string                `json:"capture_expected"`
	Runtime           *RuntimeObservation     `json:"runtime,omitempty"`
	Attempt           *AttemptObservation     `json:"attempt,omitempty"`
	Capability        *CapabilityObservation  `json:"capability,omitempty"`
	Policy            *PolicyObservation      `json:"policy,omitempty"`
	Destination       *DestinationObservation `json:"destination,omitempty"`
	Measurement       *Measurement            `json:"measurement,omitempty"`
}

type RuntimeObservation struct {
	Operation         string `json:"operation"`
	StreamMode        string `json:"stream_mode"`
	Admission         string `json:"admission"`
	Provider          string `json:"provider"`
	LocalDelivery     string `json:"local_delivery"`
	Financial         string `json:"financial"`
	Capture           string `json:"capture"`
	EvidenceRef       string `json:"evidence_ref,omitempty"`
	ContextDoneReason string `json:"context_done_reason,omitempty"`
}

type AttemptObservation struct {
	AttemptID       string `json:"attempt_id"`
	Operation       string `json:"operation"`
	RouteSequence   string `json:"route_sequence,omitempty"`
	RouteTotal      string `json:"route_total,omitempty"`
	Provider        string `json:"provider"`
	LocalDelivery   string `json:"local_delivery"`
	Financial       string `json:"financial"`
	RetryAuthorized bool   `json:"retry_authorized"`
	HTTPStatus      string `json:"http_status,omitempty"`
	EvidenceRef     string `json:"evidence_ref"`
}

type ProfileReference struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256,omitempty"`
}

// Only selected, committed authority belongs here. None of these references is
// read from mutable current configuration when a terminal record is replayed.
type CapabilityObservation struct {
	ModelRef               string                `json:"model_ref"`
	Provider               string                `json:"provider"`
	Operation              string                `json:"operation"`
	Surface                string                `json:"surface"`
	Protocol               ProfileReference      `json:"protocol"`
	ExecutionProfile       ProfileReference      `json:"execution_profile"`
	ReleaseRef             string                `json:"release_ref,omitempty"`
	SlotVersion            string                `json:"slot_version,omitempty"`
	BindingRef             string                `json:"binding_ref,omitempty"`
	ProbeRef               string                `json:"probe_ref,omitempty"`
	ApprovalRef            string                `json:"approval_ref,omitempty"`
	EvidenceState          string                `json:"evidence_state"`
	RequiredCapabilities   []string              `json:"required_capabilities"`
	Capabilities           []CapabilityReference `json:"capabilities"`
	CapabilityEvidenceHash string                `json:"capability_evidence_hash,omitempty"`
}

// EvidenceRefSHA256 hashes the exact authority's reference text. It is not a
// digest of the referenced artifact, and discloses no path, URL or credential.
type CapabilityReference struct {
	CapabilityID      string `json:"capability_id"`
	SupportState      string `json:"support_state"`
	EvidenceState     string `json:"evidence_state"`
	EvidenceRefSHA256 string `json:"evidence_ref_sha256"`
	VerifiedAt        string `json:"verified_at"`
	Revision          string `json:"revision"`
}

type PolicyObservation struct {
	DetectorOutcomes []string `json:"detector_outcomes"`
	StageDecisions   []string `json:"stage_decisions"`
	PolicyRef        string   `json:"policy_ref"`
	PolicyVersion    string   `json:"policy_version"`
	DetectorRef      string   `json:"detector_ref"`
	DetectorVersion  string   `json:"detector_version"`
	Stage            string   `json:"stage"`
	Mode             string   `json:"mode"`
	ConfiguredAction string   `json:"configured_action"`
	Outcome          string   `json:"outcome"`
	FailureMode      string   `json:"failure_mode"`
	Degraded         bool     `json:"degraded"`
	Mandatory        bool     `json:"mandatory"`
	Effects          []string `json:"effects"`
	EvidenceState    string   `json:"evidence_state"`
}

type DestinationObservation struct {
	Provider          string `json:"provider"`
	Origin            string `json:"origin"`
	Region            string `json:"region,omitempty"`
	RegionSource      string `json:"region_source"`
	Dispatch          string `json:"dispatch"`
	PolicyEvidence    string `json:"policy_evidence"`
	InputBindingState string `json:"input_binding_state"`
	InputBindingRef   string `json:"input_binding_ref,omitempty"`
}

type Measurement struct {
	Kind          string  `json:"kind"`
	Version       string  `json:"version"`
	Value         *string `json:"value,omitempty"`
	Unit          string  `json:"unit"`
	State         string  `json:"state"`
	StartBoundary string  `json:"start_boundary"`
	EndBoundary   string  `json:"end_boundary"`
	Resolution    string  `json:"resolution"`
	Source        string  `json:"source"`
	EvidenceRef   string  `json:"evidence_ref,omitempty"`
}

type CurrentRuntimeEntry struct {
	RequestID       string `json:"request_id"`
	Family          string `json:"family"`
	SubjectKind     string `json:"subject_kind"`
	SubjectID       string `json:"subject_id"`
	Operation       string `json:"operation"`
	Freshness       string `json:"freshness"`
	OriginalState   string `json:"original_state"`
	LastObservedAt  string `json:"last_observed_at"`
	ValidUntil      string `json:"valid_until,omitempty"`
	LeaseGeneration string `json:"lease_generation,omitempty"`
	EvidenceRef     string `json:"evidence_ref"`
	CheckpointRef   string `json:"checkpoint_ref,omitempty"`
}

type CurrentRuntimePage struct {
	SchemaVersion  string                `json:"schema_version"`
	SourceInstance string                `json:"source_instance"`
	OrgID          string                `json:"org_id"`
	ProjectID      string                `json:"project_id"`
	Profile        string                `json:"profile"`
	ObservedAt     string                `json:"observed_at"`
	Coverage       string                `json:"coverage"`
	Uncovered      []string              `json:"uncovered"`
	Entries        []CurrentRuntimeEntry `json:"entries"`
	HasMore        bool                  `json:"has_more"`
	NextCursor     string                `json:"next_cursor,omitempty"`
	SourceSHA256   string                `json:"source_sha256,omitempty"`
}
