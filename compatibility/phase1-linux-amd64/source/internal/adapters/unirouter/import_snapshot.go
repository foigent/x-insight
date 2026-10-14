package unirouter

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/evidence"
)

const (
	DefaultSourceSystemID     = "11111111-1111-1111-1111-111111111111"
	DefaultAdapterManifestID  = "22222222-2222-2222-2222-222222222222"
	DefaultXInsightSchema     = "m0.1"
	DefaultSnapshotImportMode = "db_import"
)

var eventTypeTokenPattern = regexp.MustCompile(`[^a-z0-9_]+`)

type SnapshotFile struct {
	SchemaVersion          string               `json:"schema_version"`
	ExchangeID             string               `json:"exchange_id,omitempty"`
	ApprovalManifestSHA256 string               `json:"approval_manifest_sha256,omitempty"`
	GeneratedAt            string               `json:"generated_at"`
	Source                 string               `json:"source"`
	SourceInstance         string               `json:"source_instance"`
	RequestIDs             []string             `json:"request_ids"`
	Scenarios              []SnapshotScenario   `json:"scenarios"`
	Safety                 SnapshotSafety       `json:"safety"`
	Counts                 SnapshotCounts       `json:"counts"`
	CaptureGaps            []SnapshotCaptureGap `json:"capture_gaps"`
	CallLogs               []SnapshotCallLog    `json:"call_logs"`
	TraceEvents            []SnapshotTraceEvent `json:"request_trace_events"`
	Notes                  map[string]any       `json:"notes"`
}

type SnapshotScenario struct {
	Label     string `json:"label"`
	RequestID string `json:"request_id"`
}

type SnapshotSafety struct {
	SelectionMode        string `json:"selection_mode"`
	RawPromptsIncluded   bool   `json:"raw_prompts_included"`
	RawResponsesIncluded bool   `json:"raw_responses_included"`
	APIKeysIncluded      bool   `json:"api_keys_included"`
	IPHandling           string `json:"ip_handling"`
	ErrorHandling        string `json:"error_handling"`
	MetadataHandling     string `json:"metadata_handling"`
}

type SnapshotCounts struct {
	RequestedRequestIDs int `json:"requested_request_ids"`
	CallLogs            int `json:"call_logs"`
	TraceEvents         int `json:"request_trace_events"`
	CaptureGaps         int `json:"capture_gaps"`
}

type SnapshotCaptureGap struct {
	RequestID            string   `json:"request_id"`
	Source               string   `json:"source"`
	Gap                  string   `json:"capture_gap"`
	CandidateCaptureGaps []string `json:"candidate_capture_gaps"`
	Reason               string   `json:"reason"`
	Generated            bool     `json:"generated_by_snapshot_tool"`
}

type SnapshotCallLog struct {
	SourceRef              string  `json:"source_ref"`
	SourceEventHash        string  `json:"source_event_hash,omitempty"`
	ID                     int64   `json:"id"`
	RequestID              string  `json:"request_id"`
	OrgID                  int64   `json:"org_id"`
	ProjectID              *int64  `json:"project_id"`
	APIKeyID               *int64  `json:"api_key_id"`
	OwnerUserID            *int64  `json:"owner_user_id"`
	KeyType                string  `json:"key_type"`
	Model                  string  `json:"model"`
	RequestedModel         string  `json:"requested_model"`
	Provider               string  `json:"provider"`
	Channel                string  `json:"channel"`
	PromptTokens           int64   `json:"prompt_tokens"`
	CompletionTokens       int64   `json:"completion_tokens"`
	TotalTokens            int64   `json:"total_tokens"`
	EstimatedTokens        int64   `json:"estimated_tokens"`
	UsageSource            string  `json:"usage_source"`
	UsageConfidence        string  `json:"usage_confidence"`
	CostCents              int64   `json:"cost_cents"`
	LatencyMS              int64   `json:"latency_ms"`
	TTFTMS                 int64   `json:"ttft_ms"`
	StatusCode             int64   `json:"status_code"`
	UpstreamStatusCode     *int64  `json:"upstream_status_code"`
	UpstreamErrorPresent   bool    `json:"upstream_error_present"`
	IPAddressWasPresent    bool    `json:"ip_address_was_present"`
	IPAddressRedacted      bool    `json:"ip_address_redacted"`
	IsStream               bool    `json:"is_stream"`
	CreatedAt              string  `json:"created_at"`
	TraceID                *string `json:"trace_id"`
	SessionID              *string `json:"session_id"`
	UpstreamRequestID      *string `json:"upstream_request_id"`
	EvidenceRef            *string `json:"evidence_ref"`
	ActorType              *string `json:"actor_type"`
	ActorRef               *string `json:"actor_ref"`
	OnBehalfOf             *string `json:"on_behalf_of"`
	PayloadRef             *string `json:"payload_ref"`
	PayloadCaptureMode     string  `json:"payload_capture_mode"`
	PayloadCaptureReason   string  `json:"payload_capture_reason"`
	DerivedCaptureMode     string  `json:"capture_mode"`
	DerivedSchemaVersion   string  `json:"schema_version"`
	DerivedSourceComponent string  `json:"source_component"`
}

type SnapshotTraceEvent struct {
	SourceRef              string         `json:"source_ref"`
	SourceEventHash        string         `json:"source_event_hash,omitempty"`
	ID                     int64          `json:"id"`
	RequestID              string         `json:"request_id"`
	OrgID                  int64          `json:"org_id"`
	ProjectID              *int64         `json:"project_id"`
	APIKeyID               *int64         `json:"api_key_id"`
	OwnerUserID            *int64         `json:"owner_user_id"`
	KeyType                string         `json:"key_type"`
	EventType              string         `json:"event_type"`
	Stage                  string         `json:"stage"`
	Severity               string         `json:"severity"`
	Model                  string         `json:"model"`
	RequestedModel         string         `json:"requested_model"`
	Provider               string         `json:"provider"`
	Channel                string         `json:"channel"`
	StatusCode             *int64         `json:"status_code"`
	LatencyMS              int64          `json:"latency_ms"`
	Metadata               map[string]any `json:"metadata"`
	MetadataKeys           []string       `json:"metadata_keys"`
	CreatedAt              string         `json:"created_at"`
	TraceID                *string        `json:"trace_id"`
	SpanID                 *string        `json:"span_id"`
	ParentSpanID           *string        `json:"parent_span_id"`
	CaptureGap             *string        `json:"capture_gap"`
	EvidenceRef            *string        `json:"evidence_ref"`
	UpstreamRequestID      *string        `json:"upstream_request_id"`
	DecisionKind           *string        `json:"decision_kind"`
	DecisionResult         *string        `json:"decision_result"`
	DecisionReason         *string        `json:"decision_reason"`
	ActorType              *string        `json:"actor_type"`
	ActorRef               *string        `json:"actor_ref"`
	OnBehalfOf             *string        `json:"on_behalf_of"`
	SessionID              *string        `json:"session_id"`
	PayloadRef             *string        `json:"payload_ref"`
	PayloadCaptureMode     string         `json:"payload_capture_mode"`
	PayloadCaptureReason   string         `json:"payload_capture_reason"`
	DerivedCaptureMode     string         `json:"capture_mode"`
	DerivedSchemaVersion   string         `json:"schema_version"`
	DerivedSourceComponent string         `json:"source_component"`
}

type SnapshotImportOptions struct {
	SourceSystemID    string
	AdapterManifestID string
	XInsightSchema    string
	SourceVersion     string
	ImportMode        string
	EmitEvents        bool
	RerunCheck        bool
}

type SnapshotImportReport struct {
	Source             string                `json:"source"`
	SourceInstance     string                `json:"source_instance"`
	SnapshotSchema     string                `json:"snapshot_schema"`
	SnapshotPath       string                `json:"snapshot_path"`
	GeneratedAt        string                `json:"generated_at"`
	EventsTotal        int                   `json:"events_total"`
	CaptureGapEvents   int                   `json:"capture_gap_events"`
	Accepted           int                   `json:"accepted"`
	DuplicateNoop      int                   `json:"duplicate_noop"`
	Quarantined        int                   `json:"quarantined"`
	Rejected           int                   `json:"rejected"`
	Results            []SnapshotEventResult `json:"results"`
	RerunDuplicateNoop int                   `json:"rerun_duplicate_noop,omitempty"`
	RerunRejected      int                   `json:"rerun_rejected,omitempty"`
	RerunQuarantined   int                   `json:"rerun_quarantined,omitempty"`
	Persistence        any                   `json:"persistence,omitempty"`
	Events             []map[string]any      `json:"events,omitempty"`
}

type SnapshotEventResult struct {
	SourceRef      string           `json:"source_ref"`
	EventType      string           `json:"event_type"`
	RequestID      string           `json:"request_id,omitempty"`
	Outcome        evidence.Outcome `json:"outcome"`
	Reason         string           `json:"reason"`
	EventHash      string           `json:"event_hash,omitempty"`
	CaptureGap     bool             `json:"capture_gap,omitempty"`
	IdempotencyKey string           `json:"idempotency_key"`
}

func ImportSnapshot(path string, schemaPath string, options SnapshotImportOptions) (SnapshotImportReport, error) {
	snapshot, err := LoadSnapshot(path)
	if err != nil {
		return SnapshotImportReport{}, err
	}
	if err := validateSnapshotSafe(snapshot); err != nil {
		return SnapshotImportReport{}, err
	}

	events := BuildSnapshotEvents(snapshot, normalizeOptions(options))
	validator, err := evidence.NewSchemaValidator(schemaPath)
	if err != nil {
		return SnapshotImportReport{}, err
	}
	ingester := evidence.NewInMemoryIngester(validator)

	report := SnapshotImportReport{
		Source:           valueOr(snapshot.Source, "unirouter"),
		SourceInstance:   snapshot.SourceInstance,
		SnapshotSchema:   snapshot.SchemaVersion,
		SnapshotPath:     path,
		GeneratedAt:      snapshot.GeneratedAt,
		EventsTotal:      len(events),
		CaptureGapEvents: len(snapshot.CaptureGaps),
	}
	for _, event := range events {
		result := ingester.Ingest(event)
		eventResult := resultFromIngest(event, result)
		report.Results = append(report.Results, eventResult)
		countOutcome(&report, result.Outcome)
	}
	if options.RerunCheck {
		for _, event := range events {
			result := ingester.Ingest(event)
			switch result.Outcome {
			case evidence.OutcomeDuplicate:
				report.RerunDuplicateNoop++
			case evidence.OutcomeRejected:
				report.RerunRejected++
			case evidence.OutcomeQuarantined:
				report.RerunQuarantined++
			}
		}
	}
	if options.EmitEvents {
		report.Events = events
	}
	return report, nil
}

func LoadSnapshot(path string) (SnapshotFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SnapshotFile{}, err
	}
	var snapshot SnapshotFile
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return SnapshotFile{}, err
	}
	if snapshot.Source == "" {
		snapshot.Source = "unirouter"
	}
	if snapshot.SourceInstance == "" {
		snapshot.SourceInstance = "unknown"
	}
	return snapshot, nil
}

func BuildSnapshotEvents(snapshot SnapshotFile, options SnapshotImportOptions) []map[string]any {
	options = normalizeOptions(options)
	ingestedAt := time.Now().UTC().Format(time.RFC3339Nano)

	events := make([]map[string]any, 0, len(snapshot.TraceEvents)+len(snapshot.CallLogs)+len(snapshot.CaptureGaps))
	for _, row := range snapshot.TraceEvents {
		events = append(events, mapTraceEvent(snapshot, row, options, ingestedAt))
	}
	for _, row := range snapshot.CallLogs {
		events = append(events, mapCallLog(snapshot, row, options, ingestedAt))
	}
	for _, gap := range snapshot.CaptureGaps {
		events = append(events, mapCaptureGap(snapshot, gap, options, ingestedAt))
	}
	sort.SliceStable(events, func(i, j int) bool {
		left := stringValue(events[i]["source_ref"])
		right := stringValue(events[j]["source_ref"])
		return left < right
	})
	return events
}

func mapTraceEvent(snapshot SnapshotFile, row SnapshotTraceEvent, options SnapshotImportOptions, ingestedAt string) map[string]any {
	sourceRef := valueOr(row.SourceRef, sourceRef(snapshot.SourceInstance, "request_trace_events", row.ID))
	sourceEventID := strconv.FormatInt(row.ID, 10)
	eventType := normalizeEventType(row.EventType)
	stage := normalizeStage(row.Stage, eventType)
	severity := normalizeSeverity(row.Severity, row.StatusCode)
	payloadMode := normalizePayloadCaptureMode(row.PayloadCaptureMode)

	event := baseEvidenceEvent(options, sourceRef, sourceEventID, row.EventType, eventType, stage, severity, row.CreatedAt, ingestedAt)
	event["source_version"] = valueOr(options.SourceVersion, snapshot.SchemaVersion)
	event["source_event_hash"] = row.SourceEventHash
	event["request_id"] = row.RequestID
	event["trace_id"] = ptrString(row.TraceID)
	event["span_id"] = ptrString(row.SpanID)
	event["org_ref"] = orgRef(row.OrgID)
	event["project_ref"] = projectRef(row.ProjectID)
	event["payload_capture_mode"] = payloadMode

	event["correlation"] = compactMap(map[string]any{
		"request_id":          row.RequestID,
		"trace_id":            ptrString(row.TraceID),
		"span_id":             ptrString(row.SpanID),
		"parent_span_id":      ptrString(row.ParentSpanID),
		"session_id":          ptrString(row.SessionID),
		"upstream_request_id": ptrString(row.UpstreamRequestID),
	})
	event["identity"] = identityGroup(row.OrgID, row.ProjectID, row.APIKeyID, row.OwnerUserID, row.KeyType, row.ActorType, row.ActorRef, row.OnBehalfOf)
	event["capability"] = capabilityGroup(row.Model, row.RequestedModel, row.Provider, row.Channel)
	if decision := decisionGroup(row.DecisionKind, row.DecisionResult, row.DecisionReason); len(decision) > 0 {
		event["decision"] = decision
	}
	event["usage"] = compactMap(map[string]any{
		"latency_ms": nonNegativeInt(row.LatencyMS),
	})
	event["outcome"] = outcomeGroup(row.StatusCode)
	event["payload_policy"] = payloadPolicyGroup(payloadMode, ptrString(row.PayloadRef), row.PayloadCaptureReason)
	event["field_provenance"] = fieldProvenance(row.DecisionKind != nil, true, hasUsage(event["usage"]), row.StatusCode != nil)
	event["raw_source_excerpt"] = compactMap(map[string]any{
		"status_code":            int64PtrValue(row.StatusCode),
		"latency_ms":             nonNegativeInt(row.LatencyMS),
		"metadata":               row.Metadata,
		"metadata_keys":          row.MetadataKeys,
		"source_component":       row.DerivedSourceComponent,
		"source_capture_mode":    row.DerivedCaptureMode,
		"source_schema_version":  row.DerivedSchemaVersion,
		"capture_gap":            ptrString(row.CaptureGap),
		"evidence_ref":           ptrString(row.EvidenceRef),
		"payload_capture_reason": row.PayloadCaptureReason,
	})
	return event
}

func mapCallLog(snapshot SnapshotFile, row SnapshotCallLog, options SnapshotImportOptions, ingestedAt string) map[string]any {
	sourceRef := valueOr(row.SourceRef, sourceRef(snapshot.SourceInstance, "call_logs", row.ID))
	sourceEventID := strconv.FormatInt(row.ID, 10)
	eventType := "usage.tokens.recorded"
	statusCode := &row.StatusCode
	payloadMode := normalizePayloadCaptureMode(row.PayloadCaptureMode)

	event := baseEvidenceEvent(options, sourceRef, sourceEventID, "call_log", eventType, "usage", normalizeSeverity("", statusCode), row.CreatedAt, ingestedAt)
	event["source_version"] = valueOr(options.SourceVersion, snapshot.SchemaVersion)
	event["source_event_hash"] = row.SourceEventHash
	event["request_id"] = row.RequestID
	event["trace_id"] = ptrString(row.TraceID)
	event["org_ref"] = orgRef(row.OrgID)
	event["project_ref"] = projectRef(row.ProjectID)
	event["payload_capture_mode"] = payloadMode

	event["correlation"] = compactMap(map[string]any{
		"request_id":          row.RequestID,
		"trace_id":            ptrString(row.TraceID),
		"session_id":          ptrString(row.SessionID),
		"upstream_request_id": ptrString(row.UpstreamRequestID),
	})
	event["identity"] = identityGroup(row.OrgID, row.ProjectID, row.APIKeyID, row.OwnerUserID, row.KeyType, row.ActorType, row.ActorRef, row.OnBehalfOf)
	event["capability"] = capabilityGroup(row.Model, row.RequestedModel, row.Provider, row.Channel)
	event["usage"] = compactMap(map[string]any{
		"prompt_tokens":     nonNegativeInt(row.PromptTokens),
		"completion_tokens": nonNegativeInt(row.CompletionTokens),
		"total_tokens":      nonNegativeInt(row.TotalTokens),
		"cost_cents":        nonNegativeInt(row.CostCents),
		"latency_ms":        nonNegativeInt(row.LatencyMS),
		"ttft_ms":           nonNegativeInt(row.TTFTMS),
		"usage_source":      row.UsageSource,
		"usage_confidence":  row.UsageConfidence,
		"extensions": compactMap(map[string]any{
			"estimated_tokens": nonNegativeInt(row.EstimatedTokens),
		}),
	})
	event["outcome"] = outcomeGroup(statusCode)
	event["payload_policy"] = payloadPolicyGroup(payloadMode, ptrString(row.PayloadRef), row.PayloadCaptureReason)
	event["field_provenance"] = fieldProvenance(false, true, true, true)
	event["raw_source_excerpt"] = compactMap(map[string]any{
		"upstream_status_code":   int64PtrValue(row.UpstreamStatusCode),
		"upstream_error_present": row.UpstreamErrorPresent,
		"ip_address_was_present": row.IPAddressWasPresent,
		"ip_address_redacted":    row.IPAddressRedacted,
		"is_stream":              row.IsStream,
		"source_component":       row.DerivedSourceComponent,
		"source_capture_mode":    row.DerivedCaptureMode,
		"source_schema_version":  row.DerivedSchemaVersion,
		"evidence_ref":           ptrString(row.EvidenceRef),
		"payload_capture_reason": row.PayloadCaptureReason,
	})
	return event
}

func mapCaptureGap(snapshot SnapshotFile, gap SnapshotCaptureGap, options SnapshotImportOptions, ingestedAt string) map[string]any {
	sourceEventID := strings.Join([]string{gap.RequestID, gap.Source, gap.Gap}, ":")
	sourceRef := fmt.Sprintf("unirouter:%s:capture_gaps:%s:%s", snapshot.SourceInstance, gap.RequestID, gap.Source)
	eventType := "gap.capture.detected"

	event := baseEvidenceEvent(options, sourceRef, sourceEventID, "capture_gap", eventType, "gap", "warning", snapshot.GeneratedAt, ingestedAt)
	event["source_version"] = valueOr(options.SourceVersion, snapshot.SchemaVersion)
	event["request_id"] = gap.RequestID
	event["payload_capture_mode"] = "none"
	event["correlation"] = compactMap(map[string]any{"request_id": gap.RequestID})
	event["payload_policy"] = map[string]any{"payload_capture_mode": "none", "redaction_state": "not_applicable"}
	event["field_provenance"] = map[string]any{
		"correlation":    "direct",
		"payload_policy": "not_applicable",
		"extensions": map[string]any{
			"capture_gap": "direct",
		},
	}
	event["raw_source_excerpt"] = compactMap(map[string]any{
		"gap_source":                 gap.Source,
		"capture_gap":                gap.Gap,
		"candidate_capture_gaps":     gap.CandidateCaptureGaps,
		"reason":                     gap.Reason,
		"generated_by_snapshot_tool": gap.Generated,
	})
	return event
}

func baseEvidenceEvent(options SnapshotImportOptions, sourceRef string, sourceEventID string, sourceEventType string, eventType string, stage string, severity string, occurredAt string, ingestedAt string) map[string]any {
	return map[string]any{
		"id":                  deterministicUUID(sourceRef + "|" + eventType + "|" + sourceEventID),
		"schema_version":      options.XInsightSchema,
		"source_system_id":    options.SourceSystemID,
		"adapter_manifest_id": options.AdapterManifestID,
		"source_type":         "unirouter",
		"source_event_id":     sourceEventID,
		"source_event_type":   sourceEventType,
		"source_ref":          sourceRef,
		"idempotency_key":     fmt.Sprintf("adapter-unirouter:%s:%s:%s:%s", options.SourceSystemID, sourceRef, eventType, sourceEventID),
		"import_cursor":       importCursor(sourceRef),
		"event_type":          eventType,
		"stage":               stage,
		"severity":            severity,
		"capture_mode":        options.ImportMode,
		"occurred_at":         occurredAt,
		"observed_at":         occurredAt,
		"ingested_at":         ingestedAt,
	}
}

func validateSnapshotSafe(snapshot SnapshotFile) error {
	if snapshot.Safety.RawPromptsIncluded || snapshot.Safety.RawResponsesIncluded || snapshot.Safety.APIKeysIncluded {
		return fmt.Errorf("unsafe UniRouter snapshot refused: safety flags indicate raw prompt/response or API key inclusion")
	}
	bytes, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(bytes, &raw); err != nil {
		return err
	}
	if keys := unsafeKeys(raw); len(keys) > 0 {
		return fmt.Errorf("unsafe UniRouter snapshot refused: unsafe-looking keys observed: %s", strings.Join(keys, ", "))
	}
	return nil
}

func normalizeOptions(options SnapshotImportOptions) SnapshotImportOptions {
	if options.SourceSystemID == "" {
		options.SourceSystemID = DefaultSourceSystemID
	}
	if options.AdapterManifestID == "" {
		options.AdapterManifestID = DefaultAdapterManifestID
	}
	if options.XInsightSchema == "" {
		options.XInsightSchema = DefaultXInsightSchema
	}
	if options.ImportMode == "" {
		options.ImportMode = DefaultSnapshotImportMode
	}
	if !validCaptureMode(options.ImportMode) {
		options.ImportMode = DefaultSnapshotImportMode
	}
	return options
}

func normalizeEventType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, "/", "_")
	switch value {
	case "auth.model_not_allowed":
		return "auth.model.rejected"
	}
	parts := strings.Split(value, ".")
	clean := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		part = strings.Trim(eventTypeTokenPattern.ReplaceAllString(part, "_"), "_")
		if part == "" {
			continue
		}
		if strings.Contains(part, "_") && len(clean) == 1 {
			clean = append(clean, splitNonEmpty(part, "_")...)
			continue
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "external.event.observed"
	}
	if len(clean) == 1 {
		return clean[0] + ".event.observed"
	}
	if len(clean) == 2 {
		return clean[0] + "." + clean[1] + ".observed"
	}
	return strings.Join(clean[:3], ".")
}

func normalizeStage(stage string, eventType string) string {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "gateway", "auth", "identity", "routing", "provider", "billing", "usage", "logging", "policy", "payload", "gap", "simulation", "external", "unknown":
		return strings.ToLower(strings.TrimSpace(stage))
	}
	prefix := strings.Split(eventType, ".")[0]
	switch prefix {
	case "gateway", "auth", "identity", "routing", "provider", "billing", "usage", "policy", "payload", "gap", "simulation":
		return prefix
	default:
		return "external"
	}
}

func normalizeSeverity(severity string, statusCode *int64) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "info", "warning", "error":
		return strings.ToLower(strings.TrimSpace(severity))
	}
	if statusCode != nil && *statusCode >= 500 {
		return "error"
	}
	if statusCode != nil && *statusCode >= 400 {
		return "warning"
	}
	return "info"
}

func normalizePayloadCaptureMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none", "metadata_only", "hash_only", "redacted", "reference_only", "full_content":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "metadata_only"
	}
}

func validCaptureMode(value string) bool {
	switch value {
	case "native", "callback", "db_import", "api_import", "otel", "openinference", "sdk", "mcp", "derived", "manual":
		return true
	default:
		return false
	}
}

func identityGroup(orgID int64, projectID *int64, apiKeyID *int64, ownerUserID *int64, keyType string, actorType *string, actorRef *string, onBehalfOf *string) map[string]any {
	resolvedActorType := ptrString(actorType)
	if resolvedActorType == "" && apiKeyID != nil {
		resolvedActorType = "api_key"
	}
	if !validActorType(resolvedActorType) {
		resolvedActorType = "unknown"
	}
	resolvedActorRef := ptrString(actorRef)
	if resolvedActorRef == "" && apiKeyID != nil {
		resolvedActorRef = "api_key:" + strconv.FormatInt(*apiKeyID, 10)
	}

	return compactMap(map[string]any{
		"org_id":      orgRef(orgID),
		"project_id":  projectRef(projectID),
		"actor_type":  resolvedActorType,
		"actor_ref":   resolvedActorRef,
		"owner_ref":   userRef(ownerUserID),
		"api_key_ref": apiKeyRef(apiKeyID),
		"key_type":    keyType,
		"extensions": compactMap(map[string]any{
			"on_behalf_of": ptrString(onBehalfOf),
		}),
	})
}

func capabilityGroup(model string, requestedModel string, provider string, channel string) map[string]any {
	return compactMap(map[string]any{
		"capability_type": "model_call",
		"model":           model,
		"requested_model": requestedModel,
		"provider":        provider,
		"channel":         channel,
	})
}

func decisionGroup(kind *string, result *string, reason *string) map[string]any {
	k := normalizeDecisionKind(ptrString(kind))
	r := normalizeDecisionResult(ptrString(result))
	if k == "" && r == "" && ptrString(reason) == "" {
		return map[string]any{}
	}
	return compactMap(map[string]any{
		"decision_kind":   valueOr(k, "unknown"),
		"decision_result": valueOr(r, "unknown"),
		"decision_reason": ptrString(reason),
	})
}

func outcomeGroup(statusCode *int64) map[string]any {
	status := "unknown"
	if statusCode != nil {
		switch {
		case *statusCode >= 200 && *statusCode < 400:
			status = "success"
		case *statusCode >= 400:
			status = "failure"
		}
	}
	return compactMap(map[string]any{
		"outcome_status": status,
		"status_code":    int64PtrValue(statusCode),
	})
}

func payloadPolicyGroup(mode string, payloadRef string, reason string) map[string]any {
	redactionState := "not_applicable"
	if mode == "redacted" {
		redactionState = "redacted"
	}
	return compactMap(map[string]any{
		"payload_capture_mode": mode,
		"redaction_state":      redactionState,
		"payload_ref":          payloadRef,
		"extensions": compactMap(map[string]any{
			"capture_reason": reason,
		}),
	})
}

func fieldProvenance(hasDecision bool, hasCapability bool, hasUsage bool, hasOutcome bool) map[string]any {
	provenance := map[string]any{
		"correlation":    "direct",
		"identity":       "direct",
		"payload_policy": "declared",
	}
	if hasCapability {
		provenance["capability"] = "direct"
	}
	if hasDecision {
		provenance["decision"] = "direct"
	}
	if hasUsage {
		provenance["usage"] = "direct"
	}
	if hasOutcome {
		provenance["outcome"] = "derived"
	}
	return provenance
}

func normalizeDecisionKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "admission", "auth", "quota", "budget", "routing", "provider", "billing", "access", "payload_capture", "trace_sampling", "retry", "failover", "policy", "policy_simulation", "unknown":
		return value
	default:
		return ""
	}
}

func normalizeDecisionResult(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "allowed", "rejected", "selected", "failed", "skipped", "simulated_match", "simulated_no_match", "unknown":
		return value
	default:
		return ""
	}
}

func validActorType(value string) bool {
	switch value {
	case "human", "service", "agent", "api_key", "system", "unknown":
		return true
	default:
		return false
	}
}

func resultFromIngest(event map[string]any, result evidence.IngestResult) SnapshotEventResult {
	return SnapshotEventResult{
		SourceRef:      stringValue(event["source_ref"]),
		EventType:      stringValue(event["event_type"]),
		RequestID:      stringValue(event["request_id"]),
		Outcome:        result.Outcome,
		Reason:         result.Reason,
		EventHash:      result.Hash,
		CaptureGap:     stringValue(event["stage"]) == "gap",
		IdempotencyKey: stringValue(event["idempotency_key"]),
	}
}

func countOutcome(report *SnapshotImportReport, outcome evidence.Outcome) {
	switch outcome {
	case evidence.OutcomeAccepted:
		report.Accepted++
	case evidence.OutcomeDuplicate:
		report.DuplicateNoop++
	case evidence.OutcomeQuarantined:
		report.Quarantined++
	case evidence.OutcomeRejected:
		report.Rejected++
	}
}

func deterministicUUID(input string) string {
	sum := sha256.Sum256([]byte(input))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func importCursor(sourceRef string) string {
	parts := strings.Split(sourceRef, ":")
	if len(parts) >= 4 {
		return parts[len(parts)-2] + ":" + parts[len(parts)-1]
	}
	return sourceRef
}

func sourceRef(sourceInstance string, table string, id int64) string {
	return fmt.Sprintf("unirouter:%s:%s:%d", sourceInstance, table, id)
}

func orgRef(id int64) string {
	if id <= 0 {
		return ""
	}
	return "org-" + strconv.FormatInt(id, 10)
}

func projectRef(id *int64) string {
	if id == nil || *id <= 0 {
		return ""
	}
	return "project-" + strconv.FormatInt(*id, 10)
}

func userRef(id *int64) string {
	if id == nil || *id <= 0 {
		return ""
	}
	return "user:" + strconv.FormatInt(*id, 10)
}

func apiKeyRef(id *int64) string {
	if id == nil || *id <= 0 {
		return ""
	}
	return "api_key:" + strconv.FormatInt(*id, 10)
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int64PtrValue(value *int64) any {
	if value == nil {
		return nil
	}
	return int(*value)
}

func nonNegativeInt(value int64) any {
	if value < 0 {
		return nil
	}
	return int(value)
}

func hasUsage(value any) bool {
	m, ok := value.(map[string]any)
	return ok && len(m) > 0
}

func compactMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		switch v := value.(type) {
		case nil:
			continue
		case string:
			if v == "" {
				continue
			}
		case map[string]any:
			if len(v) == 0 {
				continue
			}
		case []string:
			if len(v) == 0 {
				continue
			}
		}
		out[key] = value
	}
	return out
}

func splitNonEmpty(value string, sep string) []string {
	parts := strings.Split(value, sep)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func valueOr(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
