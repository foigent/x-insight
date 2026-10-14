package investigation

import "time"

const (
	ProofOperatorApproved = "operator_approved_integration"
	ProofSyntheticFixture = "synthetic_fixture"
	ProofUnclassified     = "unclassified"

	AssuranceOperatorAttested = "operator_attested_sidecar"
	AssuranceContractedPull   = "contracted_pull"
	AssuranceBoundedOffline   = "bounded_offline_import"
	AssuranceSyntheticFixture = "synthetic_fixture"
	AssuranceUnclassified     = "unclassified"

	IntegrationSidecar        = "sidecar"
	IntegrationPull           = "completed_execution_pull"
	IntegrationConnectorPull  = "connector_pull"
	IntegrationBoundedOffline = "connector_bounded_offline"
	IntegrationFixture        = "synthetic_fixture"
	SyncStateSyncing          = "syncing"
	SyncStateCurrent          = "current"
	SyncStateStale            = "stale"
	SyncStateGap              = "gap"
	OutcomeSuccess            = "success"
	OutcomeFailure            = "failure"
	OutcomePartial            = "partial"
	OutcomeUnknown            = "unknown"
	TraceJourneySchema        = "xinsight.trace-journey.v1"
)

type SearchQuery struct {
	AsOf           time.Time
	From           time.Time
	To             time.Time
	RequestID      string
	RequestPrefix  string
	SourceSystemID string
	Outcome        string
	HasGaps        *bool
	After          *SearchKey
	Limit          int
}

type SearchKey struct {
	LastEffectiveAt time.Time
	ExecutionID     string
}

type TimelineQuery struct {
	ExecutionID string
	AsOf        time.Time
	After       *TimelineKey
	Limit       int
}

type TimelineKey struct {
	EffectiveAt time.Time
	IngestedAt  time.Time
	EventID     string
}

type GapQuery struct {
	ExecutionID string
	AsOf        time.Time
	After       *GapKey
	Limit       int
}

type GapKey struct {
	CreatedAt time.Time
	GapID     string
}

type Freshness struct {
	IntegrationMode      string     `json:"integration_mode"`
	SyncState            string     `json:"sync_state"`
	TimelineComplete     bool       `json:"timeline_complete"`
	SourceTerminalAt     *time.Time `json:"source_terminal_at,omitempty"`
	SourceGeneratedAt    *time.Time `json:"source_generated_at,omitempty"`
	ImportedAt           *time.Time `json:"imported_at,omitempty"`
	SearchVisibleAt      *time.Time `json:"search_visible_at,omitempty"`
	SourceRevision       string     `json:"source_revision,omitempty"`
	ImportedRevision     string     `json:"imported_revision,omitempty"`
	SourceWatermark      string     `json:"source_watermark,omitempty"`
	ImportWatermark      string     `json:"import_watermark,omitempty"`
	LastSuccessfulSyncAt *time.Time `json:"last_successful_sync_at,omitempty"`
}

type ExecutionSummary struct {
	ExecutionID         string    `json:"execution_id"`
	RequestID           string    `json:"request_id"`
	SourceSystemID      string    `json:"source_system_id"`
	SourceType          string    `json:"source_type"`
	SourceDisplayName   string    `json:"source_display_name,omitempty"`
	Outcome             string    `json:"outcome"`
	FirstEffectiveAt    time.Time `json:"first_effective_at"`
	LastEffectiveAt     time.Time `json:"last_effective_at"`
	EventCount          int       `json:"event_count"`
	GapCount            int       `json:"gap_count"`
	HasGaps             bool      `json:"has_gaps"`
	PayloadCaptureModes []string  `json:"payload_capture_modes,omitempty"`
	ProofClass          string    `json:"proof_class"`
	AssuranceMode       string    `json:"assurance_mode"`
	Freshness           Freshness `json:"freshness"`
}

type ExecutionDetail struct {
	Summary  ExecutionSummary `json:"summary"`
	AsOf     time.Time        `json:"as_of"`
	TraceIDs []string         `json:"trace_ids,omitempty"`
}

// TraceJourney is a read-model projection over source-declared trace/span
// correlation. It never changes M0 Event identity, request_id or event_hash.
// The anchor execution only establishes the authorized source/scope/trace
// tuple; nodes may belong to multiple executions in that same tuple.
type TraceJourney struct {
	SchemaVersion     string                `json:"schema_version"`
	Available         bool                  `json:"available"`
	UnavailableReason string                `json:"unavailable_reason,omitempty"`
	AnchorExecutionID string                `json:"anchor_execution_id"`
	SourceSystemID    string                `json:"source_system_id,omitempty"`
	TraceID           string                `json:"trace_id,omitempty"`
	AsOf              time.Time             `json:"as_of"`
	Nodes             []TraceJourneyNode    `json:"nodes"`
	Edges             []TraceJourneyEdge    `json:"edges"`
	Integrity         TraceJourneyIntegrity `json:"integrity"`
	EvidenceBoundary  string                `json:"evidence_boundary"`
}

type TraceJourneyNode struct {
	ExecutionID  string    `json:"execution_id"`
	EventID      string    `json:"event_id"`
	RequestID    string    `json:"request_id"`
	SourceType   string    `json:"source_type"`
	SourceRef    string    `json:"source_ref"`
	EventHash    string    `json:"event_hash"`
	TraceID      string    `json:"trace_id"`
	SpanID       string    `json:"span_id"`
	ParentSpanID string    `json:"parent_span_id,omitempty"`
	ActorType    string    `json:"actor_type,omitempty"`
	ActorRef     string    `json:"actor_ref,omitempty"`
	OrgID        string    `json:"org_id,omitempty"`
	ProjectID    string    `json:"project_id,omitempty"`
	Action       string    `json:"action,omitempty"`
	ServiceName  string    `json:"service_name,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	Model        string    `json:"model,omitempty"`
	Outcome      string    `json:"outcome,omitempty"`
	EffectiveAt  time.Time `json:"effective_at"`
}

type TraceJourneyEdge struct {
	FromEventID string `json:"from_event_id"`
	ToEventID   string `json:"to_event_id"`
	EdgeBasis   string `json:"edge_basis"`
}

type TraceJourneyIntegrity struct {
	NodeCount                       int  `json:"node_count"`
	ExecutionCount                  int  `json:"execution_count"`
	DeclaredParentCount             int  `json:"declared_parent_count"`
	ResolvedParentCount             int  `json:"resolved_parent_count"`
	UnresolvedParentCount           int  `json:"unresolved_parent_count"`
	AmbiguousParentCount            int  `json:"ambiguous_parent_count"`
	CycleDetected                   bool `json:"cycle_detected"`
	Truncated                       bool `json:"truncated"`
	SourceTimelineComplete          bool `json:"source_timeline_complete"`
	VisibleParentResolutionComplete bool `json:"visible_parent_resolution_complete"`
}

type FieldProvenance struct {
	Correlation   string `json:"correlation,omitempty"`
	Identity      string `json:"identity,omitempty"`
	Capability    string `json:"capability,omitempty"`
	Decision      string `json:"decision,omitempty"`
	Usage         string `json:"usage,omitempty"`
	Outcome       string `json:"outcome,omitempty"`
	PayloadPolicy string `json:"payload_policy,omitempty"`
}

type TimelineNode struct {
	EventID                string          `json:"event_id"`
	EventType              string          `json:"event_type"`
	Stage                  string          `json:"stage"`
	Severity               string          `json:"severity"`
	EffectiveAt            time.Time       `json:"effective_at"`
	OccurredAt             *time.Time      `json:"occurred_at,omitempty"`
	ObservedAt             *time.Time      `json:"observed_at,omitempty"`
	IngestedAt             time.Time       `json:"ingested_at"`
	DeclaredParentEventID  string          `json:"declared_parent_event_id,omitempty"`
	DeclaredParentResolved *bool           `json:"declared_parent_resolved,omitempty"`
	SourceRef              string          `json:"source_ref"`
	SourceType             string          `json:"source_type"`
	DecisionResult         string          `json:"decision_result,omitempty"`
	OutcomeStatus          string          `json:"outcome_status,omitempty"`
	FieldProvenance        FieldProvenance `json:"field_provenance"`
}

type CorrelationGroup struct {
	RequestID         string `json:"request_id,omitempty"`
	TraceID           string `json:"trace_id,omitempty"`
	SpanID            string `json:"span_id,omitempty"`
	ParentSpanID      string `json:"parent_span_id,omitempty"`
	RunID             string `json:"run_id,omitempty"`
	SessionID         string `json:"session_id,omitempty"`
	UpstreamRequestID string `json:"upstream_request_id,omitempty"`
}

type IdentityGroup struct {
	ActorType  string `json:"actor_type,omitempty"`
	ActorRef   string `json:"actor_ref,omitempty"`
	OwnerRef   string `json:"owner_ref,omitempty"`
	OrgID      string `json:"org_id,omitempty"`
	ProjectID  string `json:"project_id,omitempty"`
	TeamRef    string `json:"team_ref,omitempty"`
	APIKeyRef  string `json:"api_key_ref,omitempty"`
	EndUserRef string `json:"end_user_ref,omitempty"`
	KeyType    string `json:"key_type,omitempty"`
}

type CapabilityGroup struct {
	CapabilityType string `json:"capability_type,omitempty"`
	TargetRef      string `json:"target_ref,omitempty"`
	Model          string `json:"model,omitempty"`
	RequestedModel string `json:"requested_model,omitempty"`
	Provider       string `json:"provider,omitempty"`
	Channel        string `json:"channel,omitempty"`
}

type DecisionGroup struct {
	DecisionKind   string `json:"decision_kind,omitempty"`
	DecisionResult string `json:"decision_result,omitempty"`
	DecisionReason string `json:"decision_reason,omitempty"`
	PolicyRef      string `json:"policy_ref,omitempty"`
	RuleRef        string `json:"rule_ref,omitempty"`
}

type UsageGroup struct {
	PromptTokens     *int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens *int64 `json:"completion_tokens,omitempty"`
	TotalTokens      *int64 `json:"total_tokens,omitempty"`
	EstimatedTokens  *int64 `json:"estimated_tokens,omitempty"`
	CostCents        *int64 `json:"cost_cents,omitempty"`
	CostMicros       string `json:"cost_micros,omitempty"`
	Currency         string `json:"currency,omitempty"`
	AmountScale      *int64 `json:"amount_scale,omitempty"`
	MoneySchema      string `json:"money_schema,omitempty"`
	LatencyMS        *int64 `json:"latency_ms,omitempty"`
	TTFTMS           *int64 `json:"ttft_ms,omitempty"`
	UsageSource      string `json:"usage_source,omitempty"`
	UsageConfidence  string `json:"usage_confidence,omitempty"`
}

type OutcomeGroup struct {
	OutcomeStatus string `json:"outcome_status,omitempty"`
	StatusCode    *int64 `json:"status_code,omitempty"`
	ErrorCode     string `json:"error_code,omitempty"`
	FailureKind   string `json:"failure_kind,omitempty"`
}

type PayloadPolicyGroup struct {
	PayloadCaptureMode string `json:"payload_capture_mode,omitempty"`
	RedactionState     string `json:"redaction_state,omitempty"`
	PayloadRef         string `json:"payload_ref,omitempty"`
	RetentionPolicyRef string `json:"retention_policy_ref,omitempty"`
}

type EventSource struct {
	SourceSystemID    string `json:"source_system_id"`
	AdapterManifestID string `json:"adapter_manifest_id"`
	SchemaVersion     string `json:"schema_version"`
	SourceType        string `json:"source_type"`
	SourceVersion     string `json:"source_version,omitempty"`
	SourceEventID     string `json:"source_event_id,omitempty"`
	SourceEventType   string `json:"source_event_type,omitempty"`
	SourceRef         string `json:"source_ref"`
	EventHash         string `json:"event_hash"`
	SourceEventHash   string `json:"source_event_hash,omitempty"`
	FidelityLevel     string `json:"fidelity_level"`
}

type EvidenceEventDetail struct {
	ExecutionID        string             `json:"execution_id"`
	EventID            string             `json:"event_id"`
	EventType          string             `json:"event_type"`
	Stage              string             `json:"stage"`
	Severity           string             `json:"severity"`
	CaptureMode        string             `json:"capture_mode"`
	OccurredAt         *time.Time         `json:"occurred_at,omitempty"`
	ObservedAt         *time.Time         `json:"observed_at,omitempty"`
	IngestedAt         time.Time          `json:"ingested_at"`
	PayloadCaptureMode string             `json:"payload_capture_mode,omitempty"`
	Correlation        CorrelationGroup   `json:"correlation,omitempty"`
	Identity           IdentityGroup      `json:"identity,omitempty"`
	Capability         CapabilityGroup    `json:"capability,omitempty"`
	Decision           DecisionGroup      `json:"decision,omitempty"`
	Usage              UsageGroup         `json:"usage,omitempty"`
	Outcome            OutcomeGroup       `json:"outcome,omitempty"`
	PayloadPolicy      PayloadPolicyGroup `json:"payload_policy,omitempty"`
	FieldProvenance    FieldProvenance    `json:"field_provenance"`
	Source             EventSource        `json:"source"`
}

type CaptureGap struct {
	GapID               string     `json:"gap_id"`
	EvidenceEventID     string     `json:"evidence_event_id,omitempty"`
	GapType             string     `json:"gap_type"`
	CandidateGapTypes   []string   `json:"candidate_gap_types,omitempty"`
	AffectedClaim       string     `json:"affected_claim"`
	GapReason           string     `json:"gap_reason"`
	Severity            string     `json:"severity"`
	Recommendation      string     `json:"recommendation,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	ResolutionState     string     `json:"resolution_state"`
	ResolutionNote      string     `json:"resolution_note,omitempty"`
	ResolutionUpdatedAt *time.Time `json:"resolution_updated_at,omitempty"`
}

const (
	InvestigationProjectionSchema = "xinsight.investigation-projection.v1"
	PresentationCausalTrace       = "causal_trace"
	PresentationChronology        = "chronology"
	PresentationTerminalSnapshot  = "terminal_snapshot"
	CoveragePresent               = "present"
	CoverageGap                   = "gap"
	CoverageNotApplicable         = "not_applicable"
	CoverageUnknown               = "unknown"
)

type EvidenceCitation struct {
	EvidenceEventID string `json:"evidence_event_id"`
	SourceRef       string `json:"source_ref"`
	EventHash       string `json:"event_hash"`
	DeepLink        string `json:"deep_link"`
}

type InvestigationClaim struct {
	Code             string   `json:"code"`
	EvidenceEventIDs []string `json:"evidence_event_ids"`
	DerivationBasis  string   `json:"derivation_basis"`
}

type StageCoverage struct {
	Stage            string   `json:"stage"`
	Status           string   `json:"status"`
	EvidenceEventIDs []string `json:"evidence_event_ids,omitempty"`
	GapIDs           []string `json:"gap_ids,omitempty"`
}

type InvestigationNode struct {
	EventID               string           `json:"event_id"`
	EventType             string           `json:"event_type"`
	SourceEventType       string           `json:"source_event_type,omitempty"`
	Kind                  string           `json:"kind"`
	ProofBasis            string           `json:"proof_basis"`
	Stage                 string           `json:"stage"`
	Severity              string           `json:"severity"`
	EffectiveAt           time.Time        `json:"effective_at"`
	DeclaredParentEventID string           `json:"declared_parent_event_id,omitempty"`
	ActorType             string           `json:"actor_type,omitempty"`
	ActorRef              string           `json:"actor_ref,omitempty"`
	OrgID                 string           `json:"org_id,omitempty"`
	ProjectID             string           `json:"project_id,omitempty"`
	Provider              string           `json:"provider,omitempty"`
	Model                 string           `json:"model,omitempty"`
	RequestedModel        string           `json:"requested_model,omitempty"`
	DecisionKind          string           `json:"decision_kind,omitempty"`
	DecisionResult        string           `json:"decision_result,omitempty"`
	DecisionReason        string           `json:"decision_reason,omitempty"`
	OutcomeStatus         string           `json:"outcome_status,omitempty"`
	StatusCode            *int64           `json:"status_code,omitempty"`
	LatencyMS             *int64           `json:"latency_ms,omitempty"`
	TotalTokens           *int64           `json:"total_tokens,omitempty"`
	CostMicros            string           `json:"cost_micros,omitempty"`
	Citation              EvidenceCitation `json:"citation"`
}

type InvestigationEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	EdgeBasis string `json:"edge_basis"`
}

type InvestigationIntegrity struct {
	SourceProfileComplete bool `json:"source_profile_complete"`
	ImportComplete        bool `json:"import_complete"`
	ProjectionComplete    bool `json:"projection_complete"`
	NodeCount             int  `json:"node_count"`
	TotalEventCount       int  `json:"total_event_count"`
	ResolvedParentCount   int  `json:"resolved_parent_count"`
	ParentEligibleCount   int  `json:"parent_eligible_count"`
	UnresolvedParentCount int  `json:"unresolved_parent_count"`
	CausalComplete        bool `json:"causal_complete"`
	Truncated             bool `json:"truncated"`
}

type CompletenessAxis struct {
	Axis   string `json:"axis"`
	Status string `json:"status"`
	Basis  string `json:"basis"`
}

type UnprovableAction struct {
	Code      string   `json:"code"`
	Statement string   `json:"statement"`
	GapIDs    []string `json:"gap_ids,omitempty"`
}

type InvestigationProjection struct {
	SchemaVersion     string                 `json:"schema_version"`
	Execution         ExecutionSummary       `json:"execution"`
	AsOf              time.Time              `json:"as_of"`
	PresentationMode  string                 `json:"presentation_mode"`
	SummaryClaims     []InvestigationClaim   `json:"summary_claims"`
	StageCoverage     []StageCoverage        `json:"stage_coverage"`
	Nodes             []InvestigationNode    `json:"nodes"`
	Edges             []InvestigationEdge    `json:"edges"`
	Gaps              []CaptureGap           `json:"gaps"`
	Freshness         Freshness              `json:"freshness"`
	Integrity         InvestigationIntegrity `json:"integrity"`
	Completeness      []CompletenessAxis     `json:"completeness"`
	UnprovableActions []UnprovableAction     `json:"unprovable_actions"`
}

type SourceOption struct {
	SourceSystemID string `json:"source_system_id"`
	SourceKey      string `json:"source_key"`
	SourceType     string `json:"source_type"`
	DisplayName    string `json:"display_name"`
	SourceVersion  string `json:"source_version,omitempty"`
}

type ConnectorCheckpointStatus struct {
	Epoch                 int64      `json:"epoch"`
	EpochOperationID      string     `json:"epoch_operation_id,omitempty"`
	CursorKey             string     `json:"cursor_key"`
	CursorPresent         bool       `json:"cursor_present"`
	CursorDigest          string     `json:"cursor_digest,omitempty"`
	HighWatermarkPresent  bool       `json:"high_watermark_present"`
	HighWatermarkDigest   string     `json:"high_watermark_digest,omitempty"`
	WorkerStatus          string     `json:"worker_status"`
	ImportStatus          string     `json:"import_status"`
	LastImportStartedAt   *time.Time `json:"last_import_started_at,omitempty"`
	LastImportCompletedAt *time.Time `json:"last_import_completed_at,omitempty"`
	LastPollFailedAt      *time.Time `json:"last_poll_failed_at,omitempty"`
	ConsecutiveFailures   int        `json:"consecutive_failures"`
	PagesCommitted        int64      `json:"pages_committed"`
	ItemsReceived         int64      `json:"items_received"`
	ItemsCommitted        int64      `json:"items_committed"`
	DuplicateNoop         int64      `json:"duplicate_noop"`
	ErrorPresent          bool       `json:"error_present"`
	UpdatedAt             *time.Time `json:"updated_at,omitempty"`
}

type ConnectorTrust struct {
	ConnectorKey          string                    `json:"connector_key"`
	SourceSystemID        string                    `json:"source_system_id"`
	SourceKey             string                    `json:"source_key"`
	SourceType            string                    `json:"source_type"`
	SourceVersion         string                    `json:"source_version,omitempty"`
	SourceDisplayName     string                    `json:"source_display_name"`
	SourceStatus          string                    `json:"source_status"`
	AdapterManifestID     string                    `json:"adapter_manifest_id"`
	AdapterID             string                    `json:"adapter_id"`
	AdapterVersion        string                    `json:"adapter_version"`
	ManifestActive        bool                      `json:"manifest_active"`
	FidelityLevel         string                    `json:"fidelity_level"`
	IngestionModes        []string                  `json:"ingestion_modes"`
	PayloadBehavior       string                    `json:"payload_behavior"`
	IdempotencyStrategy   string                    `json:"idempotency_strategy"`
	CursorStrategy        string                    `json:"cursor_strategy,omitempty"`
	TrustState            string                    `json:"trust_state"`
	Checkpoint            ConnectorCheckpointStatus `json:"checkpoint"`
	DeliveryStateBoundary string                    `json:"delivery_state_boundary"`
}

type AuthoritativeSession struct {
	SessionID         string         `json:"session_id"`
	SourceSystemID    string         `json:"source_system_id"`
	SourceType        string         `json:"source_type"`
	SourceDisplayName string         `json:"source_display_name"`
	SourceSessionRef  string         `json:"source_session_ref"`
	SourceSessionType string         `json:"source_session_type"`
	ObservedAt        time.Time      `json:"observed_at"`
	ExecutionCount    int            `json:"execution_count"`
	Metadata          map[string]any `json:"metadata"`
}

type SessionExecutionLink struct {
	ExecutionID   string    `json:"execution_id"`
	SourceLinkRef string    `json:"source_link_ref"`
	LinkedAt      time.Time `json:"linked_at"`
}

type AuthoritativeSessionDetail struct {
	Session    AuthoritativeSession   `json:"session"`
	Executions []SessionExecutionLink `json:"executions"`
}

type EvaluationFeedback struct {
	ID                 string    `json:"id"`
	ExecutionID        string    `json:"execution_id"`
	SourceSystemID     string    `json:"source_system_id"`
	EvaluatorRef       string    `json:"evaluator_ref"`
	MetricName         string    `json:"metric_name"`
	MetricValueDecimal string    `json:"metric_value_decimal,omitempty"`
	Label              string    `json:"label,omitempty"`
	Explanation        string    `json:"explanation,omitempty"`
	ObservedAt         time.Time `json:"observed_at"`
	EvidenceEventIDs   []string  `json:"evidence_event_ids"`
}

type ExecutionVersionDimension struct {
	ExecutionID     string    `json:"execution_id"`
	Dimension       string    `json:"dimension"`
	VersionRef      string    `json:"version_ref"`
	SourceSystemID  string    `json:"source_system_id"`
	EvidenceEventID string    `json:"evidence_event_id,omitempty"`
	ObservedAt      time.Time `json:"observed_at"`
}

type InvestigationAlert struct {
	ID             string    `json:"id"`
	SourceSystemID string    `json:"source_system_id"`
	SourceAlertRef string    `json:"source_alert_ref"`
	Title          string    `json:"title"`
	Severity       string    `json:"severity"`
	State          string    `json:"state"`
	ExecutionID    string    `json:"execution_id,omitempty"`
	CaseID         string    `json:"case_id,omitempty"`
	ObservedAt     time.Time `json:"observed_at"`
}

type DiscoveryHypothesis struct {
	ID                  string    `json:"id"`
	AnalysisRunID       string    `json:"analysis_run_id"`
	AnalysisAsOf        time.Time `json:"analysis_as_of"`
	SampleSize          int       `json:"sample_size"`
	InputDigest         string    `json:"input_digest"`
	HypothesisType      string    `json:"hypothesis_type"`
	Title               string    `json:"title"`
	Statement           string    `json:"statement"`
	SupportScoreDecimal string    `json:"support_score_decimal,omitempty"`
	Status              string    `json:"status"`
	EvidenceEventIDs    []string  `json:"evidence_event_ids"`
	ExecutionIDs        []string  `json:"execution_ids"`
	Limitations         []string  `json:"limitations"`
	AbstentionReason    string    `json:"abstention_reason,omitempty"`
	MethodVersion       string    `json:"method_version"`
	GeneratedAt         time.Time `json:"generated_at"`
}

type ImportProvenance struct {
	ID                     string
	SourceSystemID         string
	AdapterManifestID      string
	IntegrationMode        string
	ProofClass             string
	AssuranceMode          string
	SourceArtifactSHA256   string
	SourcePayloadSHA256    string
	ApprovalManifestSHA256 string
	ExchangeID             string
	SourceGeneratedAt      *time.Time
	SourceTerminalAt       *time.Time
	SourceRevision         string
	ImportedRevision       string
	SourceWatermark        string
	ImportWatermark        string
	TimelineComplete       bool
}
