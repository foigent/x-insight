package unirouter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/evidence"
)

const (
	CompletedExecutionSchemaV1                  = "unirouter.completed-execution.v1"
	CompletedExecutionRevisionV1                = "1"
	CompletedExecutionMaxBytes                  = 64 << 10
	DefaultCompletedExecutionAdapterManifestID  = "22222222-2222-4222-8222-222222222224"
	AcceptedCompletedExecutionSchemaSHA256      = "4ae2897d4f8470c38624dfe21c54d72eaf6d995d2c9578c4420c94d244424fb2"
	completedExecutionCaptureGapReason          = "not_available_at_billing_terminal"
	completedExecutionUnavailableCaptureGapType = "not_available_from_source"
	completedExecutionImportMode                = "api_import"
)

var (
	completedExecutionUUIDPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	completedExecutionAmountPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,18})$`)
	completedExecutionRequiredGaps  = [...]string{
		"call_log",
		"request_trace",
		"client_ip_presence",
		"upstream_error_presence",
	}
	completedExecutionProhibitedKeys = map[string]struct{}{
		"prompt":         {},
		"response":       {},
		"authorization":  {},
		"api_key":        {},
		"credential":     {},
		"ip_address":     {},
		"upstream_error": {},
	}
)

// CompletedExecutionV1 is the strict wire DTO owned by the X-Insight
// UniRouter adapter. It is decoded into the source-neutral evidence model
// before persistence.
type CompletedExecutionV1 struct {
	SchemaVersion string                         `json:"schema_version"`
	RequestID     string                         `json:"request_id"`
	OrgID         int64                          `json:"org_id"`
	ProjectID     int64                          `json:"project_id"`
	Terminal      CompletedExecutionTerminalV1   `json:"terminal"`
	Usage         CompletedExecutionUsageV1      `json:"usage"`
	Money         CompletedExecutionMoneyV1      `json:"money"`
	CaptureGaps   []CompletedExecutionCaptureGap `json:"capture_gaps"`
}

type CompletedExecutionTerminalStatus string

const (
	CompletedExecutionTerminalSettled   CompletedExecutionTerminalStatus = "settled"
	CompletedExecutionTerminalCancelled CompletedExecutionTerminalStatus = "cancelled"
)

type CompletedExecutionTerminalV1 struct {
	Status      CompletedExecutionTerminalStatus `json:"status"`
	AvailableAt time.Time                        `json:"available_at"`
}

type CompletedExecutionUsageV1 struct {
	Model                string `json:"model"`
	PromptTokens         int64  `json:"prompt_tokens"`
	UncachedPromptTokens int64  `json:"uncached_prompt_tokens"`
	CacheReadTokens      int64  `json:"cache_read_tokens"`
	CacheCreationTokens  int64  `json:"cache_creation_tokens"`
	CompletionTokens     int64  `json:"completion_tokens"`
	Source               string `json:"source"`
	Authority            string `json:"authority"`
	Purpose              string `json:"purpose"`
	Finality             string `json:"finality"`
	ContractVersion      string `json:"contract_version"`
	EstimateVersion      string `json:"estimate_version,omitempty"`
	CacheUsageSchema     string `json:"cache_usage_schema"`
	CacheUsageStatus     string `json:"cache_usage_status"`
}

type CompletedExecutionMoneyV1 struct {
	Schema               string `json:"schema"`
	Currency             string `json:"currency"`
	AmountScale          int64  `json:"amount_scale"`
	CalculatedCostMicros string `json:"calculated_cost_micros"`
	ChargedCostMicros    string `json:"charged_cost_micros"`
	PricingVersion       string `json:"pricing_version"`
}

type CompletedExecutionCaptureGap struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

type CompletedExecutionMapOptions struct {
	SourceInstance    string
	SourceSystemID    string
	AdapterManifestID string
	XInsightSchema    string
	ObservedAt        time.Time
	IngestedAt        time.Time
}

// CompletedExecutionMapResult contains only accepted typed facts, the identity
// of the exact received bytes, and persistence-ready normalized events.
// ObservedAt is page provenance and is deliberately not hashed into events.
type CompletedExecutionMapResult struct {
	Snapshot            CompletedExecutionV1
	SnapshotSHA256      string
	SourceInstance      string
	SourceSystemID      string
	AdapterManifestID   string
	SourceVersion       string
	RequestID           string
	OrgID               int64
	ProjectID           int64
	TerminalStatus      CompletedExecutionTerminalStatus
	TerminalAvailableAt time.Time
	ObservedAt          time.Time
	IngestedAt          time.Time
	Events              []map[string]any
}

// DecodeCompletedExecutionV1 rejects oversized, ambiguous, prohibited,
// incomplete, and contract-invalid source bytes before returning typed facts.
func DecodeCompletedExecutionV1(payload []byte) (CompletedExecutionV1, string, error) {
	if len(payload) == 0 {
		return CompletedExecutionV1{}, "", errors.New("completed-execution snapshot is empty")
	}
	if len(payload) > CompletedExecutionMaxBytes {
		return CompletedExecutionV1{}, "", fmt.Errorf("completed-execution snapshot exceeds %d bytes", CompletedExecutionMaxBytes)
	}
	if !utf8.Valid(payload) {
		return CompletedExecutionV1{}, "", errors.New("completed-execution snapshot must be valid UTF-8")
	}
	if err := scanCompletedExecutionJSON(payload); err != nil {
		return CompletedExecutionV1{}, "", err
	}
	if err := validateCompletedExecutionRequiredShape(payload); err != nil {
		return CompletedExecutionV1{}, "", err
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var snapshot CompletedExecutionV1
	if err := decoder.Decode(&snapshot); err != nil {
		return CompletedExecutionV1{}, "", fmt.Errorf("decode completed-execution snapshot: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return CompletedExecutionV1{}, "", errors.New("completed-execution snapshot contains trailing content")
	}
	if err := ValidateCompletedExecutionV1(snapshot); err != nil {
		return CompletedExecutionV1{}, "", err
	}

	digest := sha256.Sum256(payload)
	return snapshot, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func ValidateCompletedExecutionV1(snapshot CompletedExecutionV1) error {
	if snapshot.SchemaVersion != CompletedExecutionSchemaV1 {
		return fmt.Errorf("completed-execution schema_version must be %q", CompletedExecutionSchemaV1)
	}
	if err := validateCompletedExecutionText("request_id", snapshot.RequestID, 1, 64); err != nil {
		return err
	}
	if snapshot.OrgID <= 0 || snapshot.ProjectID <= 0 {
		return errors.New("completed-execution org_id and project_id must be positive")
	}
	switch snapshot.Terminal.Status {
	case CompletedExecutionTerminalSettled, CompletedExecutionTerminalCancelled:
	default:
		return errors.New("completed-execution terminal status is unsupported")
	}
	if snapshot.Terminal.AvailableAt.IsZero() {
		return errors.New("completed-execution terminal available_at is required")
	}
	_, offset := snapshot.Terminal.AvailableAt.Zone()
	if offset != 0 {
		return errors.New("completed-execution terminal available_at must use UTC")
	}
	if err := validateCompletedExecutionText("usage.model", snapshot.Usage.Model, 1, 100); err != nil {
		return err
	}

	tokenCounts := map[string]int64{
		"usage.prompt_tokens":          snapshot.Usage.PromptTokens,
		"usage.uncached_prompt_tokens": snapshot.Usage.UncachedPromptTokens,
		"usage.cache_read_tokens":      snapshot.Usage.CacheReadTokens,
		"usage.cache_creation_tokens":  snapshot.Usage.CacheCreationTokens,
		"usage.completion_tokens":      snapshot.Usage.CompletionTokens,
	}
	for name, count := range tokenCounts {
		if count < 0 {
			return fmt.Errorf("%s must be nonnegative", name)
		}
	}
	if snapshot.Usage.UncachedPromptTokens > snapshot.Usage.PromptTokens ||
		snapshot.Usage.CacheReadTokens > snapshot.Usage.PromptTokens-snapshot.Usage.UncachedPromptTokens ||
		snapshot.Usage.CacheCreationTokens != snapshot.Usage.PromptTokens-snapshot.Usage.UncachedPromptTokens-snapshot.Usage.CacheReadTokens {
		return errors.New("completed-execution prompt token partition is invalid")
	}
	if snapshot.Usage.CompletionTokens > int64(^uint64(0)>>1)-snapshot.Usage.PromptTokens {
		return errors.New("completed-execution total token count overflows int64")
	}

	boundedFields := []struct {
		name  string
		value string
		max   int
	}{
		{"usage.source", snapshot.Usage.Source, 32},
		{"usage.authority", snapshot.Usage.Authority, 64},
		{"usage.purpose", snapshot.Usage.Purpose, 64},
		{"usage.finality", snapshot.Usage.Finality, 64},
		{"usage.contract_version", snapshot.Usage.ContractVersion, 80},
		{"usage.estimate_version", snapshot.Usage.EstimateVersion, 80},
		{"usage.cache_usage_schema", snapshot.Usage.CacheUsageSchema, 80},
		{"usage.cache_usage_status", snapshot.Usage.CacheUsageStatus, 40},
		{"money.pricing_version", snapshot.Money.PricingVersion, 80},
	}
	for _, field := range boundedFields {
		if err := validateCompletedExecutionText(field.name, field.value, 0, field.max); err != nil {
			return err
		}
	}

	if snapshot.Money.Schema != MoneySchemaMicroCNY ||
		snapshot.Money.Currency != MoneyCurrencyCNY ||
		snapshot.Money.AmountScale != MoneyAmountScale {
		return errors.New("completed-execution money identity is invalid")
	}
	if err := validateCompletedExecutionAmount("money.calculated_cost_micros", snapshot.Money.CalculatedCostMicros); err != nil {
		return err
	}
	if err := validateCompletedExecutionAmount("money.charged_cost_micros", snapshot.Money.ChargedCostMicros); err != nil {
		return err
	}

	if len(snapshot.CaptureGaps) != len(completedExecutionRequiredGaps) {
		return errors.New("completed-execution capture_gaps must contain the fixed V1 set")
	}
	seen := make(map[string]struct{}, len(snapshot.CaptureGaps))
	for _, gap := range snapshot.CaptureGaps {
		if gap.Reason != completedExecutionCaptureGapReason {
			return errors.New("completed-execution capture gap reason is unsupported")
		}
		if _, duplicate := seen[gap.Source]; duplicate {
			return errors.New("completed-execution capture_gaps contains a duplicate or unsupported source")
		}
		seen[gap.Source] = struct{}{}
	}
	for _, source := range completedExecutionRequiredGaps {
		if _, exists := seen[source]; !exists {
			return errors.New("completed-execution capture_gaps is missing a required source")
		}
	}
	return nil
}

func MapCompletedExecutionV1(payload []byte, options CompletedExecutionMapOptions) (CompletedExecutionMapResult, error) {
	snapshot, snapshotSHA256, err := DecodeCompletedExecutionV1(payload)
	if err != nil {
		return CompletedExecutionMapResult{}, err
	}
	return mapCompletedExecutionSnapshot(snapshot, snapshotSHA256, options)
}

func mapCompletedExecutionSnapshot(
	snapshot CompletedExecutionV1,
	snapshotSHA256 string,
	options CompletedExecutionMapOptions,
) (CompletedExecutionMapResult, error) {
	normalizedOptions, err := normalizeCompletedExecutionMapOptions(options, snapshot.Terminal.AvailableAt)
	if err != nil {
		return CompletedExecutionMapResult{}, err
	}
	options = normalizedOptions

	events, err := buildCompletedExecutionEvents(snapshot, snapshotSHA256, options)
	if err != nil {
		return CompletedExecutionMapResult{}, err
	}
	if err := validateNormalizedEventBounds(events); err != nil {
		return CompletedExecutionMapResult{}, err
	}
	for index, event := range events {
		hash, hashErr := evidence.GenerateEventHash(event)
		if hashErr != nil {
			return CompletedExecutionMapResult{}, fmt.Errorf("hash completed-execution event %d: %w", index, hashErr)
		}
		event["event_hash"] = hash
	}

	return CompletedExecutionMapResult{
		Snapshot:            snapshot,
		SnapshotSHA256:      snapshotSHA256,
		SourceInstance:      options.SourceInstance,
		SourceSystemID:      options.SourceSystemID,
		AdapterManifestID:   options.AdapterManifestID,
		SourceVersion:       CompletedExecutionSchemaV1,
		RequestID:           snapshot.RequestID,
		OrgID:               snapshot.OrgID,
		ProjectID:           snapshot.ProjectID,
		TerminalStatus:      snapshot.Terminal.Status,
		TerminalAvailableAt: snapshot.Terminal.AvailableAt,
		ObservedAt:          options.ObservedAt,
		IngestedAt:          options.IngestedAt,
		Events:              events,
	}, nil
}

func buildCompletedExecutionEvents(snapshot CompletedExecutionV1, snapshotSHA256 string, options CompletedExecutionMapOptions) ([]map[string]any, error) {
	importOptions := SnapshotImportOptions{
		SourceSystemID:    options.SourceSystemID,
		AdapterManifestID: options.AdapterManifestID,
		XInsightSchema:    options.XInsightSchema,
		SourceVersion:     CompletedExecutionSchemaV1,
		ImportMode:        completedExecutionImportMode,
	}
	terminalAt := snapshot.Terminal.AvailableAt.UTC().Format(time.RFC3339Nano)
	ingestedAt := options.IngestedAt.UTC().Format(time.RFC3339Nano)
	mainSourceRef := fmt.Sprintf("unirouter:%s:completed_execution:%s", options.SourceInstance, snapshot.RequestID)
	eventType := "billing.execution.settled"
	severity := "info"
	outcomeStatus := "success"
	if snapshot.Terminal.Status == CompletedExecutionTerminalCancelled {
		eventType = "billing.execution.cancelled"
		severity = "warning"
		outcomeStatus = "failure"
	}

	mainEvent := baseEvidenceEvent(
		importOptions,
		mainSourceRef,
		snapshot.RequestID,
		"completed_execution",
		eventType,
		"billing",
		severity,
		terminalAt,
		ingestedAt,
	)
	mainEvent["source_version"] = CompletedExecutionSchemaV1
	mainEvent["source_event_hash"] = snapshotSHA256
	mainEvent["request_id"] = snapshot.RequestID
	mainEvent["org_ref"] = orgRef(snapshot.OrgID)
	projectID := snapshot.ProjectID
	mainEvent["project_ref"] = projectRef(&projectID)
	mainEvent["payload_capture_mode"] = "metadata_only"
	mainEvent["correlation"] = map[string]any{"request_id": snapshot.RequestID}
	mainEvent["identity"] = map[string]any{
		"org_id":     orgRef(snapshot.OrgID),
		"project_id": projectRef(&projectID),
		"actor_type": "system",
	}
	mainEvent["capability"] = compactMap(map[string]any{
		"capability_type": "model_call",
		"model":           snapshot.Usage.Model,
		"extensions": compactMap(map[string]any{
			"usage_authority": snapshot.Usage.Authority,
		}),
	})
	mainEvent["usage"] = compactMap(map[string]any{
		"prompt_tokens":     snapshot.Usage.PromptTokens,
		"completion_tokens": snapshot.Usage.CompletionTokens,
		"total_tokens":      snapshot.Usage.PromptTokens + snapshot.Usage.CompletionTokens,
		"cost_micros":       snapshot.Money.ChargedCostMicros,
		"currency":          snapshot.Money.Currency,
		"amount_scale":      snapshot.Money.AmountScale,
		"money_schema":      snapshot.Money.Schema,
		"usage_source":      snapshot.Usage.Source,
		"usage_confidence":  snapshot.Usage.Finality,
		"extensions": compactMap(map[string]any{
			"uncached_prompt_tokens": snapshot.Usage.UncachedPromptTokens,
			"cache_read_tokens":      snapshot.Usage.CacheReadTokens,
			"cache_creation_tokens":  snapshot.Usage.CacheCreationTokens,
			"usage_purpose":          snapshot.Usage.Purpose,
			"usage_contract_version": snapshot.Usage.ContractVersion,
			"usage_estimate_version": snapshot.Usage.EstimateVersion,
			"cache_usage_schema":     snapshot.Usage.CacheUsageSchema,
			"cache_usage_status":     snapshot.Usage.CacheUsageStatus,
			"calculated_cost_micros": snapshot.Money.CalculatedCostMicros,
			"pricing_version":        snapshot.Money.PricingVersion,
		}),
	})
	mainEvent["outcome"] = map[string]any{
		"outcome_status": outcomeStatus,
		"extensions": map[string]any{
			"terminal_status": string(snapshot.Terminal.Status),
		},
	}
	mainEvent["payload_policy"] = map[string]any{
		"payload_capture_mode": "metadata_only",
		"redaction_state":      "not_applicable",
	}
	mainEvent["field_provenance"] = map[string]any{
		"correlation":    "direct",
		"identity":       "direct",
		"capability":     "direct",
		"usage":          "direct",
		"outcome":        "direct",
		"payload_policy": "declared",
	}
	mainEvent["raw_source_excerpt"] = compactMap(map[string]any{
		"terminal_status":        string(snapshot.Terminal.Status),
		"usage_source":           snapshot.Usage.Source,
		"usage_authority":        snapshot.Usage.Authority,
		"usage_purpose":          snapshot.Usage.Purpose,
		"usage_finality":         snapshot.Usage.Finality,
		"usage_contract_version": snapshot.Usage.ContractVersion,
		"usage_estimate_version": snapshot.Usage.EstimateVersion,
		"cache_usage_schema":     snapshot.Usage.CacheUsageSchema,
		"cache_usage_status":     snapshot.Usage.CacheUsageStatus,
		"calculated_cost_micros": snapshot.Money.CalculatedCostMicros,
		"pricing_version":        snapshot.Money.PricingVersion,
	})

	sortedGaps := append([]CompletedExecutionCaptureGap(nil), snapshot.CaptureGaps...)
	sort.Slice(sortedGaps, func(left, right int) bool {
		return sortedGaps[left].Source < sortedGaps[right].Source
	})
	events := make([]map[string]any, 0, 1+len(sortedGaps))
	events = append(events, mainEvent)
	for _, gap := range sortedGaps {
		sourceEventID := strings.Join([]string{snapshot.RequestID, "gap", gap.Source}, ":")
		sourceRef := fmt.Sprintf(
			"unirouter:%s:completed_execution:%s:gap:%s",
			options.SourceInstance,
			snapshot.RequestID,
			gap.Source,
		)
		event := baseEvidenceEvent(
			importOptions,
			sourceRef,
			sourceEventID,
			"completed_execution_capture_gap",
			"gap.capture.detected",
			"gap",
			"warning",
			terminalAt,
			ingestedAt,
		)
		event["source_version"] = CompletedExecutionSchemaV1
		event["source_event_hash"] = snapshotSHA256
		event["request_id"] = snapshot.RequestID
		event["org_ref"] = orgRef(snapshot.OrgID)
		event["project_ref"] = projectRef(&projectID)
		event["payload_capture_mode"] = "none"
		event["correlation"] = map[string]any{"request_id": snapshot.RequestID}
		event["identity"] = map[string]any{
			"org_id":     orgRef(snapshot.OrgID),
			"project_id": projectRef(&projectID),
			"actor_type": "system",
		}
		event["payload_policy"] = map[string]any{
			"payload_capture_mode": "none",
			"redaction_state":      "not_applicable",
		}
		event["field_provenance"] = map[string]any{
			"correlation":    "direct",
			"identity":       "direct",
			"payload_policy": "not_applicable",
			"extensions": map[string]any{
				"capture_gap": "direct",
			},
		}
		event["raw_source_excerpt"] = map[string]any{
			"capture_gap": completedExecutionUnavailableCaptureGapType,
			"gap_source":  gap.Source,
			"reason":      gap.Reason,
		}
		events = append(events, event)
	}
	return events, nil
}

func normalizeCompletedExecutionMapOptions(options CompletedExecutionMapOptions, terminalAt time.Time) (CompletedExecutionMapOptions, error) {
	if options.SourceSystemID == "" {
		options.SourceSystemID = DefaultSourceSystemID
	}
	if options.AdapterManifestID == "" {
		options.AdapterManifestID = DefaultCompletedExecutionAdapterManifestID
	}
	if options.XInsightSchema == "" {
		options.XInsightSchema = DefaultXInsightSchema
	}
	if err := validateCompletedExecutionText("source_instance", options.SourceInstance, 1, 128); err != nil {
		return CompletedExecutionMapOptions{}, err
	}
	if !completedExecutionUUIDPattern.MatchString(options.SourceSystemID) {
		return CompletedExecutionMapOptions{}, errors.New("completed-execution source_system_id must be a UUID")
	}
	if options.AdapterManifestID != DefaultCompletedExecutionAdapterManifestID {
		return CompletedExecutionMapOptions{}, fmt.Errorf(
			"completed-execution adapter_manifest_id must be reserved V1 identity %s",
			DefaultCompletedExecutionAdapterManifestID,
		)
	}
	if err := validateCompletedExecutionText("xinsight_schema", options.XInsightSchema, 1, 64); err != nil {
		return CompletedExecutionMapOptions{}, err
	}
	if options.ObservedAt.IsZero() {
		options.ObservedAt = terminalAt
	} else {
		options.ObservedAt = options.ObservedAt.UTC()
	}
	if options.IngestedAt.IsZero() {
		options.IngestedAt = time.Now().UTC()
	} else {
		options.IngestedAt = options.IngestedAt.UTC()
	}
	return options, nil
}

func validateCompletedExecutionAmount(name string, value string) error {
	if !completedExecutionAmountPattern.MatchString(value) {
		return fmt.Errorf("%s must be a canonical nonnegative decimal string of at most 19 digits", name)
	}
	return nil
}

func validateCompletedExecutionText(name string, value string, minimum int, maximum int) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", name)
	}
	if len(value) < minimum || len(value) > maximum {
		return fmt.Errorf("%s must contain between %d and %d bytes", name, minimum, maximum)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s must not contain control characters", name)
		}
	}
	return nil
}

func validateCompletedExecutionRequiredShape(payload []byte) error {
	top, err := completedExecutionRawObject(payload, "completed-execution snapshot")
	if err != nil {
		return err
	}
	if err := validateCompletedExecutionObjectFields(
		top,
		"completed-execution snapshot",
		[]string{"schema_version", "request_id", "org_id", "project_id", "terminal", "usage", "money", "capture_gaps"},
		nil,
	); err != nil {
		return err
	}
	terminal, err := completedExecutionRawObject(top["terminal"], "terminal")
	if err != nil {
		return err
	}
	if err := validateCompletedExecutionObjectFields(terminal, "terminal", []string{"status", "available_at"}, nil); err != nil {
		return err
	}
	usage, err := completedExecutionRawObject(top["usage"], "usage")
	if err != nil {
		return err
	}
	if err := validateCompletedExecutionObjectFields(
		usage,
		"usage",
		[]string{
			"model", "prompt_tokens", "uncached_prompt_tokens", "cache_read_tokens",
			"cache_creation_tokens", "completion_tokens", "source", "authority", "purpose",
			"finality", "contract_version", "cache_usage_schema", "cache_usage_status",
		},
		[]string{"estimate_version"},
	); err != nil {
		return err
	}
	money, err := completedExecutionRawObject(top["money"], "money")
	if err != nil {
		return err
	}
	if err := validateCompletedExecutionObjectFields(
		money,
		"money",
		[]string{
			"schema", "currency", "amount_scale", "calculated_cost_micros",
			"charged_cost_micros", "pricing_version",
		},
		nil,
	); err != nil {
		return err
	}
	var gaps []json.RawMessage
	if err := json.Unmarshal(top["capture_gaps"], &gaps); err != nil {
		return errors.New("capture_gaps must be an array")
	}
	for index, rawGap := range gaps {
		gap, gapErr := completedExecutionRawObject(rawGap, fmt.Sprintf("capture_gaps[%d]", index))
		if gapErr != nil {
			return gapErr
		}
		if err := validateCompletedExecutionObjectFields(
			gap,
			fmt.Sprintf("capture_gaps[%d]", index),
			[]string{"source", "reason"},
			nil,
		); err != nil {
			return err
		}
	}
	return nil
}

func completedExecutionRawObject(payload []byte, name string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%s must be a JSON object", name)
	}
	return object, nil
}

func validateCompletedExecutionObjectFields(
	object map[string]json.RawMessage,
	name string,
	required []string,
	optional []string,
) error {
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, field := range required {
		allowed[field] = struct{}{}
		raw, exists := object[field]
		if !exists || len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("%s.%s is required", name, field)
		}
	}
	for _, field := range optional {
		allowed[field] = struct{}{}
	}
	for field, raw := range object {
		if _, exists := allowed[field]; !exists {
			return fmt.Errorf("%s contains unknown field %q", name, field)
		}
		if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("%s.%s must not be null", name, field)
		}
	}
	return nil
}

func scanCompletedExecutionJSON(payload []byte) error {
	return scanStrictJSONDocument(payload, "completed-execution snapshot", completedExecutionProhibitedKeys)
}

func scanStrictJSONDocument(payload []byte, documentName string, prohibitedKeys map[string]struct{}) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := scanStrictJSONValue(decoder, "$", documentName, prohibitedKeys); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s contains trailing content", documentName)
	}
	return nil
}

func scanStrictJSONValue(
	decoder *json.Decoder,
	path string,
	documentName string,
	prohibitedKeys map[string]struct{},
) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode %s: %w", documentName, err)
	}
	delim, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return fmt.Errorf("decode %s: %w", documentName, keyErr)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("%s object key is not a string", documentName)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("%s contains duplicate field at %s.%s", documentName, path, key)
			}
			seen[key] = struct{}{}
			if _, prohibited := prohibitedKeys[strings.ToLower(key)]; prohibited {
				return fmt.Errorf("%s contains a prohibited raw field at %s.%s", documentName, path, key)
			}
			if err := scanStrictJSONValue(decoder, path+"."+key, documentName, prohibitedKeys); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("decode %s: %w", documentName, err)
		}
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := scanStrictJSONValue(
				decoder,
				path+"["+strconv.Itoa(index)+"]",
				documentName,
				prohibitedKeys,
			); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("decode %s: %w", documentName, err)
		}
	default:
		return fmt.Errorf("%s contains invalid JSON delimiter", documentName)
	}
	return nil
}
