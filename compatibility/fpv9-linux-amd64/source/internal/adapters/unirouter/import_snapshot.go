package unirouter

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/evidence"
)

const (
	SnapshotSchemaV1                 = "unirouter.prd5.xinsight.snapshot.v1"
	SnapshotSchemaV2                 = "unirouter.prd5.xinsight.snapshot.v2"
	MoneyCurrencyCNY                 = "CNY"
	MoneyAmountScale           int64 = 1_000_000
	MoneySchemaMicroCNY              = "micro-cny.v1"
	DefaultSourceSystemID            = "11111111-1111-1111-1111-111111111111"
	DefaultAdapterManifestIDV1       = "22222222-2222-2222-2222-222222222222"
	DefaultAdapterManifestIDV2       = "22222222-2222-4222-8222-222222222223"
	DefaultAdapterManifestID         = DefaultAdapterManifestIDV1
	DefaultXInsightSchema            = "m0.1"
	DefaultSnapshotImportMode        = "db_import"
	MaxSnapshotBytes                 = 64 << 20
	MaxSnapshotRequestIDs            = 1000
	MaxSnapshotEvents                = 10000
	MaxSnapshotCaptureGaps           = 10000
	MaxNormalizedEventBytes          = 256 << 10
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
	CostCents              *int64  `json:"cost_cents,omitempty"`
	CostMicros             *string `json:"cost_micros,omitempty"`
	Currency               *string `json:"currency,omitempty"`
	AmountScale            *int64  `json:"amount_scale,omitempty"`
	MoneySchema            *string `json:"money_schema,omitempty"`
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
	costCentsPresent       bool
	costMicrosPresent      bool
	currencyPresent        bool
	amountScalePresent     bool
	moneySchemaPresent     bool
}

func (row *SnapshotCallLog) UnmarshalJSON(data []byte) error {
	type snapshotCallLogAlias SnapshotCallLog
	var decoded snapshotCallLogAlias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("call log contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing call log data: %w", err)
	}
	*row = SnapshotCallLog(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	_, row.costCentsPresent = fields["cost_cents"]
	_, row.costMicrosPresent = fields["cost_micros"]
	_, row.currencyPresent = fields["currency"]
	_, row.amountScalePresent = fields["amount_scale"]
	_, row.moneySchemaPresent = fields["money_schema"]
	return nil
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
	Snapshot           SnapshotFile          `json:"-"`
	ArtifactSHA256     string                `json:"-"`
	SourceSystemID     string                `json:"source_system_id"`
	AdapterManifestID  string                `json:"adapter_manifest_id"`
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
	snapshot, artifactSHA256, err := loadSnapshotArtifact(path)
	if err != nil {
		return SnapshotImportReport{}, err
	}
	if err := validateSnapshotSafe(snapshot); err != nil {
		return SnapshotImportReport{}, err
	}

	options = normalizeOptions(options, snapshot.SchemaVersion)
	events, err := BuildSnapshotEvents(snapshot, options)
	if err != nil {
		return SnapshotImportReport{}, err
	}
	if err := validateNormalizedEventBounds(events); err != nil {
		return SnapshotImportReport{}, err
	}
	validator, err := evidence.NewSchemaValidator(schemaPath)
	if err != nil {
		return SnapshotImportReport{}, err
	}
	ingester := evidence.NewInMemoryIngester(validator)

	report := SnapshotImportReport{
		Source:            valueOr(snapshot.Source, "unirouter"),
		SourceInstance:    snapshot.SourceInstance,
		SnapshotSchema:    snapshot.SchemaVersion,
		SnapshotPath:      path,
		GeneratedAt:       snapshot.GeneratedAt,
		EventsTotal:       len(events),
		CaptureGapEvents:  len(snapshot.CaptureGaps),
		Snapshot:          snapshot,
		ArtifactSHA256:    artifactSHA256,
		SourceSystemID:    options.SourceSystemID,
		AdapterManifestID: options.AdapterManifestID,
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

func validateNormalizedEventBounds(events []map[string]any) error {
	for index, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("encode normalized event %d: %w", index, err)
		}
		if len(encoded) > MaxNormalizedEventBytes {
			return fmt.Errorf("normalized event %d exceeds %d bytes", index, MaxNormalizedEventBytes)
		}
	}
	return nil
}

func LoadSnapshot(path string) (SnapshotFile, error) {
	snapshot, _, err := loadSnapshotArtifact(path)
	return snapshot, err
}

func loadSnapshotArtifact(path string) (SnapshotFile, string, error) {
	data, err := readSnapshotBytes(path)
	if err != nil {
		return SnapshotFile{}, "", err
	}
	return loadSnapshotArtifactBytes(data)
}

func loadSnapshotArtifactBytes(data []byte) (SnapshotFile, string, error) {
	var snapshot SnapshotFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return SnapshotFile{}, "", err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return SnapshotFile{}, "", fmt.Errorf("UniRouter snapshot contains multiple JSON values")
		}
		return SnapshotFile{}, "", fmt.Errorf("decode trailing UniRouter snapshot data: %w", err)
	}
	if snapshot.Source == "" {
		snapshot.Source = "unirouter"
	}
	if snapshot.SourceInstance == "" {
		snapshot.SourceInstance = "unknown"
	}
	if err := validateSnapshotContract(snapshot); err != nil {
		return SnapshotFile{}, "", err
	}
	if err := validateSnapshotBounds(snapshot); err != nil {
		return SnapshotFile{}, "", err
	}
	digest := sha256.Sum256(data)
	return snapshot, fmt.Sprintf("sha256:%x", digest), nil
}

func readSnapshotBytes(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxSnapshotBytes {
		return nil, fmt.Errorf("UniRouter snapshot exceeds %d bytes", MaxSnapshotBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxSnapshotBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSnapshotBytes {
		return nil, fmt.Errorf("UniRouter snapshot exceeds %d bytes", MaxSnapshotBytes)
	}
	return data, nil
}

func validateSnapshotBounds(snapshot SnapshotFile) error {
	if len(snapshot.RequestIDs) > MaxSnapshotRequestIDs {
		return fmt.Errorf("UniRouter snapshot exceeds %d request IDs", MaxSnapshotRequestIDs)
	}
	if len(snapshot.CaptureGaps) > MaxSnapshotCaptureGaps {
		return fmt.Errorf("UniRouter snapshot exceeds %d capture gaps", MaxSnapshotCaptureGaps)
	}
	if len(snapshot.TraceEvents)+len(snapshot.CallLogs)+len(snapshot.CaptureGaps) > MaxSnapshotEvents {
		return fmt.Errorf("UniRouter snapshot exceeds %d normalized events", MaxSnapshotEvents)
	}
	return nil
}

func validateSnapshotContract(snapshot SnapshotFile) error {
	switch snapshot.SchemaVersion {
	case SnapshotSchemaV1:
		for index, row := range snapshot.CallLogs {
			if !row.costCentsPresent || row.CostCents == nil {
				return fmt.Errorf("call_logs[%d]: v1 cost_cents is required", index)
			}
			if *row.CostCents < 0 {
				return fmt.Errorf("call_logs[%d]: v1 cost_cents must be non-negative", index)
			}
			if row.costMicrosPresent || row.currencyPresent || row.amountScalePresent || row.moneySchemaPresent {
				return fmt.Errorf("call_logs[%d]: v1 must not contain v2 money fields", index)
			}
		}
	case SnapshotSchemaV2:
		for index, row := range snapshot.CallLogs {
			if row.costCentsPresent {
				return fmt.Errorf("call_logs[%d]: v2 cost_cents conflicts with cost_micros", index)
			}
			if !row.costMicrosPresent || !row.currencyPresent || !row.amountScalePresent || !row.moneySchemaPresent || row.CostMicros == nil || row.Currency == nil || row.AmountScale == nil || row.MoneySchema == nil {
				return fmt.Errorf("call_logs[%d]: v2 cost_micros, currency, amount_scale, and money_schema are required", index)
			}
			if err := validateCostMicros(*row.CostMicros); err != nil {
				return fmt.Errorf("call_logs[%d]: %w", index, err)
			}
			if *row.Currency != MoneyCurrencyCNY {
				return fmt.Errorf("call_logs[%d]: v2 currency must be %s", index, MoneyCurrencyCNY)
			}
			if *row.AmountScale != MoneyAmountScale {
				return fmt.Errorf("call_logs[%d]: v2 amount_scale must be %d", index, MoneyAmountScale)
			}
			if *row.MoneySchema != MoneySchemaMicroCNY {
				return fmt.Errorf("call_logs[%d]: v2 money_schema must be %s", index, MoneySchemaMicroCNY)
			}
		}
	default:
		return fmt.Errorf("unsupported UniRouter snapshot schema_version %q", snapshot.SchemaVersion)
	}
	return nil
}

func validateCostMicros(value string) error {
	if value == "" || (value != "0" && (value[0] < '1' || value[0] > '9')) {
		return fmt.Errorf("v2 cost_micros must be a canonical non-negative decimal string")
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return fmt.Errorf("v2 cost_micros must be a canonical non-negative decimal string")
		}
	}
	if len(value) > 19 {
		return fmt.Errorf("v2 cost_micros must contain at most 19 decimal digits")
	}
	return nil
}

func BuildSnapshotEvents(snapshot SnapshotFile, options SnapshotImportOptions) ([]map[string]any, error) {
	if err := validateSnapshotContract(snapshot); err != nil {
		return nil, err
	}
	options = normalizeOptions(options, snapshot.SchemaVersion)
	if err := validateSnapshotImportOptions(snapshot.SchemaVersion, options); err != nil {
		return nil, err
	}
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
	return events, nil
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
	usage := map[string]any{
		"prompt_tokens":     nonNegativeInt(row.PromptTokens),
		"completion_tokens": nonNegativeInt(row.CompletionTokens),
		"total_tokens":      nonNegativeInt(row.TotalTokens),
		"latency_ms":        nonNegativeInt(row.LatencyMS),
		"ttft_ms":           nonNegativeInt(row.TTFTMS),
		"usage_source":      row.UsageSource,
		"usage_confidence":  row.UsageConfidence,
		"extensions": compactMap(map[string]any{
			"estimated_tokens": nonNegativeInt(row.EstimatedTokens),
		}),
	}
	if snapshot.SchemaVersion == SnapshotSchemaV1 {
		usage["cost_cents"] = nonNegativeInt(*row.CostCents)
	} else {
		usage["cost_micros"] = *row.CostMicros
		usage["currency"] = *row.Currency
		usage["amount_scale"] = *row.AmountScale
		usage["money_schema"] = *row.MoneySchema
	}
	event["usage"] = compactMap(usage)
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
		"id":                  mappedEventID(options, sourceRef, eventType, sourceEventID),
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

func mappedEventID(options SnapshotImportOptions, sourceRef string, eventType string, sourceEventID string) string {
	legacyInput := sourceRef + "|" + eventType + "|" + sourceEventID
	if options.SourceSystemID == DefaultSourceSystemID && options.AdapterManifestID == DefaultAdapterManifestIDV1 {
		return deterministicUUID(legacyInput)
	}
	return deterministicUUID(strings.Join([]string{
		"xinsight-unirouter-event-v2",
		options.SourceSystemID,
		options.AdapterManifestID,
		sourceRef,
		eventType,
		sourceEventID,
	}, "\x00"))
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

func normalizeOptions(options SnapshotImportOptions, snapshotSchema string) SnapshotImportOptions {
	if options.SourceSystemID == "" {
		options.SourceSystemID = DefaultSourceSystemID
	}
	if options.AdapterManifestID == "" {
		options.AdapterManifestID = DefaultAdapterManifestIDForSchema(snapshotSchema)
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

func validateSnapshotImportOptions(snapshotSchema string, options SnapshotImportOptions) error {
	if options.SourceVersion != "" && options.SourceVersion != snapshotSchema {
		return fmt.Errorf("source_version %q must match snapshot schema_version %q", options.SourceVersion, snapshotSchema)
	}
	reservedSchema := ""
	switch options.AdapterManifestID {
	case DefaultAdapterManifestIDV1:
		reservedSchema = SnapshotSchemaV1
	case DefaultAdapterManifestIDV2:
		reservedSchema = SnapshotSchemaV2
	}
	if reservedSchema != "" && reservedSchema != snapshotSchema {
		return fmt.Errorf("adapter_manifest_id %s is reserved for snapshot schema_version %s", options.AdapterManifestID, reservedSchema)
	}
	return nil
}

func DefaultAdapterManifestIDForSchema(snapshotSchema string) string {
	if snapshotSchema == SnapshotSchemaV2 {
		return DefaultAdapterManifestIDV2
	}
	return DefaultAdapterManifestIDV1
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
