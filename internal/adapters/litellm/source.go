package litellm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/connector"
)

const (
	SourceSchemaV1        = "foigent.litellm.spend-log.v1"
	ConnectorID           = "foigent.litellm.spend-logs"
	ConnectorVersion      = "1.0.0"
	ProjectionProfileID   = "xinsight.foigent-envelope-to-m0.litellm-spend-log.v1"
	ProtocolVersion       = "1.1.2"
	ProtocolCommit        = "fd82aeca9877bea8064b25c365a3cdcdd0bb3d16"
	ProtocolReleaseSHA    = "sha256:0b414d5d59e5a7ca5c05505cb58d7992c544537da1c84a57256ec205618d9cd1"
	SourceSystemID        = "71ee805a-997c-5265-badb-4e453f30d4e0"
	AdapterManifestID     = "ae560b8a-eb62-5756-b8d1-be164a82232b"
	CursorKey             = "litellm.spend-logs.v1"
	microsecondTimeLayout = "2006-01-02T15:04:05.000000Z"
)

var (
	rawAllowlist = map[string]struct{}{
		"request_id":          {},
		"status":              {},
		"startTime":           {},
		"endTime":             {},
		"model":               {},
		"model_group":         {},
		"custom_llm_provider": {},
		"prompt_tokens":       {},
		"completion_tokens":   {},
		"total_tokens":        {},
		"spend":               {},
		"cache_hit":           {},
	}
	prohibitedRawFields = map[string]struct{}{
		"api_key":              {},
		"user":                 {},
		"end_user":             {},
		"requester_ip_address": {},
		"metadata":             {},
		"messages":             {},
		"response":             {},
		"proxy_server_request": {},
		"cache_key":            {},
		"session_id":           {},
		"team_id":              {},
		"organization_id":      {},
	}
)

type SourceRecord struct {
	SchemaVersion    string `json:"schema_version"`
	RequestID        string `json:"request_id"`
	Status           string `json:"status"`
	StartTime        string `json:"start_time"`
	EndTime          string `json:"end_time"`
	Model            string `json:"model"`
	ModelGroup       string `json:"model_group,omitempty"`
	Provider         string `json:"provider,omitempty"`
	PromptTokens     string `json:"prompt_tokens"`
	CompletionTokens string `json:"completion_tokens"`
	TotalTokens      string `json:"total_tokens"`
	ReportedSpend    string `json:"reported_spend"`
	Currency         string `json:"currency"`
	CacheStatus      string `json:"cache_status"`
}

type MappingContext struct {
	ProducerInstance string
	TenantRef        *string
	OrgRef           string
	ProjectRef       string
}

type SanitizedRecord struct {
	Record         SourceRecord
	CanonicalBytes []byte
	SourceHash     string
}

func ValidateStrictJSON(payload []byte) error {
	if !utf8.Valid(payload) {
		return errors.New("LiteLLM JSON must be valid UTF-8")
	}
	if err := validateJSONUnicodeEscapes(payload); err != nil {
		return err
	}
	return scanStrictJSON(payload)
}

func SanitizeUpstreamRowBytes(payload []byte) (SanitizedRecord, error) {
	if len(payload) == 0 || len(payload) > 256<<10 || !utf8.Valid(payload) {
		return SanitizedRecord{}, errors.New("LiteLLM spend-log row bytes are invalid")
	}
	if err := ValidateStrictJSON(payload); err != nil {
		return SanitizedRecord{}, err
	}
	var row map[string]json.RawMessage
	if err := json.Unmarshal(payload, &row); err != nil || row == nil {
		return SanitizedRecord{}, errors.New("LiteLLM spend-log row must be an object")
	}
	return SanitizeUpstreamRow(row)
}

// SanitizeUpstreamRow performs the only raw LiteLLM-to-product transition.
// It copies the protocol allowlist, explicitly discards sensitive fields and
// never retains the upstream object.
func SanitizeUpstreamRow(row map[string]json.RawMessage) (SanitizedRecord, error) {
	if len(row) == 0 {
		return SanitizedRecord{}, errors.New("LiteLLM spend-log row is empty")
	}
	allowlisted := make(map[string]json.RawMessage, len(rawAllowlist))
	for field := range rawAllowlist {
		if raw, ok := row[field]; ok {
			allowlisted[field] = bytes.Clone(raw)
		}
	}
	record, err := sourceRecordFromRaw(allowlisted)
	if err != nil {
		return SanitizedRecord{}, err
	}
	return canonicalSourceRecord(record)
}

func DecodeSourceRecord(payload []byte) (SanitizedRecord, error) {
	if len(payload) == 0 || len(payload) > 64<<10 || !utf8.Valid(payload) {
		return SanitizedRecord{}, errors.New("LiteLLM Source record bytes are invalid")
	}
	if err := ValidateStrictJSON(payload); err != nil {
		return SanitizedRecord{}, err
	}
	var generic any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&generic); err != nil {
		return SanitizedRecord{}, fmt.Errorf("decode LiteLLM Source record: %w", err)
	}
	if field, found := findProhibitedField(generic); found {
		return SanitizedRecord{}, fmt.Errorf("LiteLLM Source record contains prohibited field %q", field)
	}
	decoder = json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var record SourceRecord
	if err := decoder.Decode(&record); err != nil {
		return SanitizedRecord{}, fmt.Errorf("decode LiteLLM Source record: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return SanitizedRecord{}, errors.New("LiteLLM Source record contains trailing content")
	}
	if err := validateSourceRecord(record); err != nil {
		return SanitizedRecord{}, err
	}
	return canonicalSourceRecord(record)
}

func BuildEnvelope(
	record SanitizedRecord,
	context MappingContext,
	binding connector.ManifestBinding,
) ([]byte, connector.ValidatedEnvelope, *connector.ConnectorError) {
	if err := validateBinding(binding); err != nil {
		return nil, connector.ValidatedEnvelope{}, sourceConnectorError(
			"foigent.litellm.binding_invalid",
			"LiteLLM projection binding is invalid",
		)
	}
	if err := validateMappingContext(context); err != nil {
		return nil, connector.ValidatedEnvelope{}, sourceConnectorError(
			"foigent.litellm.mapping_context_invalid",
			"LiteLLM mapping context is invalid",
		)
	}
	if err := validateSourceRecord(record.Record); err != nil {
		return nil, connector.ValidatedEnvelope{}, sourceConnectorError(
			"foigent.litellm.source_invalid",
			"LiteLLM Source record is invalid",
		)
	}
	recomputed, err := canonicalSourceRecord(record.Record)
	if err != nil ||
		!bytes.Equal(recomputed.CanonicalBytes, record.CanonicalBytes) ||
		recomputed.SourceHash != record.SourceHash {
		return nil, connector.ValidatedEnvelope{}, sourceConnectorError(
			"foigent.litellm.source_identity_mismatch",
			"LiteLLM Source record identity is invalid",
		)
	}
	tenant := ""
	if context.TenantRef != nil {
		tenant = *context.TenantRef
	}
	idempotency, err := connector.EventIdempotencyKey(
		"litellm",
		context.ProducerInstance,
		tenant,
		context.OrgRef,
		context.ProjectRef,
		"model.request.completed",
		record.Record.RequestID,
	)
	if err != nil {
		return nil, connector.ValidatedEnvelope{}, sourceConnectorError(
			"foigent.litellm.identity_invalid",
			"LiteLLM Event identity is invalid",
		)
	}
	scope := map[string]any{
		"org_ref":     context.OrgRef,
		"project_ref": context.ProjectRef,
	}
	if context.TenantRef != nil {
		scope["tenant_ref"] = *context.TenantRef
	}
	// Protocol v1.1.2 retains this legacy field value for immutable
	// cross-language vectors. It means "eligible after the configured
	// start-time delay"; it is not a terminal-finality assertion and the
	// product-local M0 projection deliberately does not emit it.
	usage := map[string]any{
		"model":             record.Record.Model,
		"prompt_tokens":     record.Record.PromptTokens,
		"completion_tokens": record.Record.CompletionTokens,
		"total_tokens":      record.Record.TotalTokens,
		"source":            "litellm_spend_log",
		"authority":         "source_reported",
		"finality":          "finalized_after_delay",
		"contract_version":  SourceSchemaV1,
		"cache_status":      record.Record.CacheStatus,
	}
	if record.Record.ModelGroup != "" {
		usage["model_group"] = record.Record.ModelGroup
	}
	if record.Record.Provider != "" {
		usage["provider"] = record.Record.Provider
	}
	hashInput := map[string]any{
		"spec_version":   connector.EventEnvelopeVersionV1,
		"event_id":       record.Record.RequestID,
		"event_type":     "model.request.completed",
		"schema_ref":     "foigent://events/model/request-completed/v2",
		"schema_version": "2",
		"producer": map[string]any{
			"product":           "litellm",
			"instance":          context.ProducerInstance,
			"connector_id":      ConnectorID,
			"connector_version": ConnectorVersion,
		},
		"scope":   scope,
		"subject": map[string]any{"type": "request", "ref": record.Record.RequestID},
		"actor":   map[string]any{"type": "system"},
		"time": map[string]any{
			"occurred_at": record.Record.EndTime,
			"terminal_at": record.Record.EndTime,
		},
		"correlation": map[string]any{"request_ref": record.Record.RequestID},
		"identity": map[string]any{
			"idempotency_key": idempotency,
			"source_revision": "1",
		},
		"integrity": map[string]any{"source_hash": record.SourceHash},
		"data_policy": map[string]any{
			"payload_capture_mode": "metadata_only",
			"raw_payload_present":  false,
		},
		"provenance": map[string]any{
			"adapter_fidelity": "F1",
			"field_groups": map[string]any{
				"actor":                     "declared",
				"correlation":               "direct",
				"data_policy":               "declared",
				"identity":                  "derived",
				"payload.capture_gaps":      "declared",
				"payload.reported_money":    "direct",
				"payload.terminal":          "direct",
				"payload.timeline_complete": "declared",
				"payload.usage":             "direct",
				"scope":                     "declared",
				"subject":                   "derived",
				"time.occurred_at":          "derived",
				"time.terminal_at":          "direct",
			},
		},
		"payload": map[string]any{
			"terminal": map[string]any{"status": record.Record.Status},
			"usage":    usage,
			"reported_money": map[string]any{
				"schema":    "decimal-currency.v1",
				"currency":  "USD",
				"amount":    record.Record.ReportedSpend,
				"source":    "litellm_spend_log",
				"authority": "source_reported",
			},
			"timeline_complete": true,
			"capture_gaps":      buildCaptureGaps(record.Record, scope),
		},
	}
	return connector.BuildCanonicalEnvelope(hashInput, binding)
}

func sourceRecordFromRaw(fields map[string]json.RawMessage) (SourceRecord, error) {
	requestID, err := rawRequiredString(fields, "request_id")
	if err != nil {
		return SourceRecord{}, err
	}
	status, err := rawStatus(fields["status"])
	if err != nil {
		return SourceRecord{}, err
	}
	startTime, err := rawMicrosecondTime(fields, "startTime")
	if err != nil {
		return SourceRecord{}, err
	}
	endTime, err := rawMicrosecondTime(fields, "endTime")
	if err != nil {
		return SourceRecord{}, err
	}
	model, err := rawRequiredString(fields, "model")
	if err != nil {
		return SourceRecord{}, err
	}
	modelGroup, err := rawOptionalString(fields, "model_group")
	if err != nil {
		return SourceRecord{}, err
	}
	provider, err := rawOptionalString(fields, "custom_llm_provider")
	if err != nil {
		return SourceRecord{}, err
	}
	prompt, err := rawNonnegativeInt64(fields, "prompt_tokens")
	if err != nil {
		return SourceRecord{}, err
	}
	completion, err := rawNonnegativeInt64(fields, "completion_tokens")
	if err != nil {
		return SourceRecord{}, err
	}
	total, err := rawNonnegativeInt64(fields, "total_tokens")
	if err != nil {
		return SourceRecord{}, err
	}
	spendRaw, exists := fields["spend"]
	if !exists {
		return SourceRecord{}, errors.New("LiteLLM spend-log row is missing spend")
	}
	spend, _, valid := CanonicalReportedDecimal(string(spendRaw))
	if !valid {
		return SourceRecord{}, errors.New("LiteLLM spend-log spend is invalid")
	}
	cacheStatus, err := rawCacheStatus(fields["cache_hit"])
	if err != nil {
		return SourceRecord{}, err
	}
	record := SourceRecord{
		SchemaVersion:    SourceSchemaV1,
		RequestID:        requestID,
		Status:           status,
		StartTime:        startTime,
		EndTime:          endTime,
		Model:            model,
		ModelGroup:       modelGroup,
		Provider:         provider,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
		ReportedSpend:    spend,
		Currency:         "USD",
		CacheStatus:      cacheStatus,
	}
	if err := validateSourceRecord(record); err != nil {
		return SourceRecord{}, err
	}
	return record, nil
}

func canonicalSourceRecord(record SourceRecord) (SanitizedRecord, error) {
	if err := validateSourceRecord(record); err != nil {
		return SanitizedRecord{}, err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return SanitizedRecord{}, err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return SanitizedRecord{}, err
	}
	canonical, err := connector.CanonicalizeJSONValue(value)
	if err != nil {
		return SanitizedRecord{}, err
	}
	digest := sha256.Sum256(canonical)
	return SanitizedRecord{
		Record:         record,
		CanonicalBytes: canonical,
		SourceHash:     "sha256:" + hex.EncodeToString(digest[:]),
	}, nil
}

func validateSourceRecord(record SourceRecord) error {
	if record.SchemaVersion != SourceSchemaV1 ||
		(record.Status != "succeeded" && record.Status != "failed") ||
		record.Currency != "USD" ||
		(record.CacheStatus != "hit" &&
			record.CacheStatus != "miss" &&
			record.CacheStatus != "unknown") {
		return errors.New("LiteLLM Source record controlled field is invalid")
	}
	if !validOpaque(record.RequestID, 1, 128) ||
		!validOpaque(record.Model, 1, 200) ||
		(record.ModelGroup != "" && !validOpaque(record.ModelGroup, 1, 200)) ||
		(record.Provider != "" && !validOpaque(record.Provider, 1, 100)) {
		return errors.New("LiteLLM Source record opaque field is invalid")
	}
	start, err := time.Parse(microsecondTimeLayout, record.StartTime)
	if err != nil {
		return errors.New("LiteLLM Source record start_time is invalid")
	}
	end, err := time.Parse(microsecondTimeLayout, record.EndTime)
	if err != nil || end.Before(start) {
		return errors.New("LiteLLM Source record end_time is invalid")
	}
	prompt, err := parseCanonicalInt64(record.PromptTokens)
	if err != nil {
		return errors.New("LiteLLM Source record prompt_tokens is invalid")
	}
	completion, err := parseCanonicalInt64(record.CompletionTokens)
	if err != nil {
		return errors.New("LiteLLM Source record completion_tokens is invalid")
	}
	total, err := parseCanonicalInt64(record.TotalTokens)
	if err != nil || prompt > total || completion > total-prompt ||
		prompt+completion != total {
		return errors.New("LiteLLM Source token counters are inconsistent")
	}
	canonical, _, valid := CanonicalReportedDecimal(record.ReportedSpend)
	if !valid || canonical != record.ReportedSpend {
		return errors.New("LiteLLM Source reported_spend is not canonical")
	}
	return nil
}

func validateMappingContext(context MappingContext) error {
	if !validOpaque(context.ProducerInstance, 1, 128) ||
		!isPositiveDecimal(context.OrgRef) ||
		!isPositiveDecimal(context.ProjectRef) {
		return errors.New("LiteLLM mapping context is invalid")
	}
	if context.TenantRef != nil && !validOpaque(*context.TenantRef, 1, 128) {
		return errors.New("LiteLLM mapping context tenant is invalid")
	}
	return nil
}

func buildCaptureGaps(record SourceRecord, scope map[string]any) []any {
	claims := []string{
		"billing_authority",
		"business_outcome",
		"downstream_side_effects",
		"routing_decision",
	}
	gaps := make([]any, 0, len(claims))
	for _, claim := range claims {
		gaps = append(gaps, map[string]any{
			"gap_id":               record.RequestID + ":gap:" + claim,
			"parent_event_id":      record.RequestID,
			"scope":                cloneMap(scope),
			"correlation":          map[string]any{"request_ref": record.RequestID},
			"affected_claim":       claim,
			"gap_type":             "not_available_from_source",
			"reason_code":          "not_exposed_by_litellm_spend_logs_v1",
			"severity":             "warning",
			"observed_at":          record.EndTime,
			"payload_capture_mode": "none",
			"field_provenance": map[string]any{
				"affected_claim":       "declared",
				"reason_code":          "declared",
				"gap_id":               "derived",
				"parent_event_id":      "derived",
				"scope":                "derived",
				"correlation":          "derived",
				"observed_at":          "derived",
				"gap_type":             "declared",
				"severity":             "declared",
				"payload_capture_mode": "declared",
			},
		})
	}
	return gaps
}

// CanonicalReportedDecimal implements the exact protocol decimal profile.
func CanonicalReportedDecimal(input string) (string, string, bool) {
	if input == "" || len(input) > 1024 {
		return "", "invalid_reported_spend", false
	}
	if strings.HasPrefix(input, "-") {
		return "", "negative_reported_spend", false
	}
	mantissa := input
	exponent := int64(0)
	if position := strings.IndexAny(mantissa, "eE"); position >= 0 {
		exponentRaw := mantissa[position+1:]
		mantissa = mantissa[:position]
		parsed, err := strconv.ParseInt(exponentRaw, 10, 32)
		if err != nil {
			return "", "invalid_reported_spend", false
		}
		exponent = parsed
	}
	if mantissa == "" || strings.Count(mantissa, ".") > 1 {
		return "", "invalid_reported_spend", false
	}
	parts := strings.SplitN(mantissa, ".", 2)
	if parts[0] == "" || !allDigits(parts[0]) ||
		(len(parts) == 2 && (parts[1] == "" || !allDigits(parts[1]))) {
		return "", "invalid_reported_spend", false
	}
	fractionLength := int64(0)
	digits := parts[0]
	if len(parts) == 2 {
		fractionLength = int64(len(parts[1]))
		digits += parts[1]
	}
	integer := new(big.Int)
	if _, ok := integer.SetString(digits, 10); !ok {
		return "", "invalid_reported_spend", false
	}
	if integer.Sign() == 0 {
		return "0", "", true
	}
	scale := exponent - fractionLength
	var rendered string
	if scale >= 0 {
		integerDigits := integer.String()
		if scale > 80 || int64(len(integerDigits))+scale > 80 {
			return "", "reported_spend_too_large", false
		}
		rendered = integerDigits + strings.Repeat("0", int(scale))
	} else {
		fractionDigits := int(-scale)
		digits = integer.String()
		if fractionDigits >= len(digits) {
			// The canonical rendering is "0." plus exactly fractionDigits
			// fractional digits. Bound it before allocation so an extreme
			// exponent cannot drive an unbounded strings.Repeat.
			if fractionDigits > 78 {
				return "", "reported_spend_too_large", false
			}
			rendered = "0." + strings.Repeat("0", fractionDigits-len(digits)) + digits
		} else {
			position := len(digits) - fractionDigits
			rendered = digits[:position] + "." + digits[position:]
		}
		rendered = strings.TrimRight(rendered, "0")
		rendered = strings.TrimRight(rendered, ".")
	}
	if len(rendered) > 80 {
		return "", "reported_spend_too_large", false
	}
	return rendered, "", true
}

func rawRequiredString(fields map[string]json.RawMessage, name string) (string, error) {
	raw, exists := fields[name]
	if !exists {
		return "", fmt.Errorf("LiteLLM spend-log row is missing %s", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		return "", fmt.Errorf("LiteLLM spend-log %s is invalid", name)
	}
	return value, nil
}

func rawOptionalString(fields map[string]json.RawMessage, name string) (string, error) {
	raw, exists := fields[name]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("LiteLLM spend-log %s is invalid", name)
	}
	return value, nil
}

func rawStatus(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("LiteLLM spend-log status is missing")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "succeeded", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errors.New("LiteLLM spend-log status is invalid")
	}
	switch value {
	case "success":
		return "succeeded", nil
	case "failure":
		return "failed", nil
	default:
		return "", errors.New("LiteLLM spend-log status is invalid")
	}
}

func rawCacheStatus(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("LiteLLM spend-log cache_hit is missing")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "unknown", nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errors.New("LiteLLM spend-log cache_hit is invalid")
	}
	switch typed := value.(type) {
	case bool:
		if typed {
			return "hit", nil
		}
		return "miss", nil
	case string:
		switch typed {
		case "True":
			return "hit", nil
		case "False":
			return "miss", nil
		case "", "None":
			return "unknown", nil
		}
	}
	return "", errors.New("LiteLLM spend-log cache_hit is invalid")
}

func rawMicrosecondTime(fields map[string]json.RawMessage, name string) (string, error) {
	value, err := rawRequiredString(fields, name)
	if err != nil {
		return "", err
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Nanosecond()%1000 != 0 {
		return "", fmt.Errorf("LiteLLM spend-log %s is not exact microsecond time", name)
	}
	return parsed.UTC().Format(microsecondTimeLayout), nil
}

func rawNonnegativeInt64(fields map[string]json.RawMessage, name string) (string, error) {
	raw, exists := fields[name]
	if !exists || len(raw) == 0 {
		return "", fmt.Errorf("LiteLLM spend-log row is missing %s", name)
	}
	value := string(bytes.TrimSpace(raw))
	parsed, err := parseCanonicalInt64(value)
	if err != nil {
		return "", fmt.Errorf("LiteLLM spend-log %s is invalid", name)
	}
	return strconv.FormatInt(parsed, 10), nil
}

func parseCanonicalInt64(value string) (int64, error) {
	if value == "" || (len(value) > 1 && value[0] == '0') || !allDigits(value) {
		return 0, errors.New("noncanonical decimal")
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, errors.New("out of int64 range")
	}
	return parsed, nil
}

func isPositiveDecimal(value string) bool {
	if value == "" || value == "0" || (len(value) > 1 && value[0] == '0') ||
		!allDigits(value) {
		return false
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}

func validOpaque(value string, min, max int) bool {
	if !utf8.ValidString(value) || len(value) < min || len(value) > max {
		return false
	}
	for _, r := range value {
		if r == 0 || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func allDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func scanStrictJSON(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, "$"); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("LiteLLM JSON contains trailing content")
	}
	return nil
}

// validateJSONUnicodeEscapes rejects the lossy corner of Go's encoding/json
// decoder: an escaped unpaired UTF-16 surrogate is otherwise replaced with
// U+FFFD. LiteLLM request IDs and model labels are opaque exact-byte fields, so
// silently changing those bytes would corrupt Source identity.
func validateJSONUnicodeEscapes(payload []byte) error {
	inString := false
	for index := 0; index < len(payload); index++ {
		switch payload[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString {
				continue
			}
			if index+1 >= len(payload) {
				return errors.New("LiteLLM JSON contains an incomplete escape")
			}
			if payload[index+1] != 'u' {
				index++
				continue
			}
			code, ok := decodeHexQuad(payload, index+2)
			if !ok {
				return errors.New("LiteLLM JSON contains an invalid Unicode escape")
			}
			switch {
			case code >= 0xd800 && code <= 0xdbff:
				if index+11 >= len(payload) ||
					payload[index+6] != '\\' ||
					payload[index+7] != 'u' {
					return errors.New("LiteLLM JSON contains an unpaired UTF-16 high surrogate")
				}
				low, lowOK := decodeHexQuad(payload, index+8)
				if !lowOK || low < 0xdc00 || low > 0xdfff {
					return errors.New("LiteLLM JSON contains an unpaired UTF-16 high surrogate")
				}
				index += 11
			case code >= 0xdc00 && code <= 0xdfff:
				return errors.New("LiteLLM JSON contains an unpaired UTF-16 low surrogate")
			default:
				index += 5
			}
		}
	}
	return nil
}

func decodeHexQuad(payload []byte, offset int) (uint16, bool) {
	if offset+4 > len(payload) {
		return 0, false
	}
	var value uint16
	for _, raw := range payload[offset : offset+4] {
		value <<= 4
		switch {
		case raw >= '0' && raw <= '9':
			value += uint16(raw - '0')
		case raw >= 'a' && raw <= 'f':
			value += uint16(raw-'a') + 10
		case raw >= 'A' && raw <= 'F':
			value += uint16(raw-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func scanJSONValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode LiteLLM JSON: %w", err)
	}
	delim, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("LiteLLM JSON object key is invalid")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("LiteLLM JSON contains duplicate field %s.%s", path, key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, path+"."+key); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := scanJSONValue(decoder, path+"["+strconv.Itoa(index)+"]"); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("LiteLLM JSON delimiter is invalid")
	}
}

func findProhibitedField(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, prohibited := prohibitedRawFields[key]; prohibited {
				return key, true
			}
			if found, ok := findProhibitedField(typed[key]); ok {
				return found, true
			}
		}
	case []any:
		for _, item := range typed {
			if found, ok := findProhibitedField(item); ok {
				return found, true
			}
		}
	}
	return "", false
}

func cloneMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func sourceConnectorError(code, message string) *connector.ConnectorError {
	return connector.NewConnectorError(
		connector.ErrorContract,
		code,
		connector.PhaseNormalize,
		"map_source",
		false,
		message,
		connector.RecoveryOperatorRepair,
	)
}
