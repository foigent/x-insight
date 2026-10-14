package openinference

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/connector"
)

const (
	SourceSchemaV1        = "xinsight.otlp-openinference-span.v1"
	ConnectorID           = "foigent.openinference.otlp-spans"
	ConnectorVersion      = "1.0.0"
	ProjectionProfileID   = "xinsight.foigent-envelope-to-m0.openinference-span.v1"
	IngestionCaptureMode  = "openinference"
	ProtocolVersion       = "2.0.2"
	ProtocolCommit        = "ad8fe7a38c9d7335657630baa8670dd07556b98c"
	ProtocolReleaseSHA    = "sha256:99f8a541b6d33b0a74d2e4c06aa312c6d6c88c35a4bfb1d4d34b98b4e4e2e8e8"
	SourceSystemID        = "d7fd8d6e-7b59-5cb9-9261-578655cc5ae9"
	AdapterManifestID     = "30b5d2c6-7df7-5c0b-87fc-e5b58a33a837"
	maxOTLPRequestBytes   = 8 << 20
	maxOTLPSpans          = 200
	microsecondTimeLayout = "2006-01-02T15:04:05.000000Z"
)

var (
	traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	spanIDPattern  = regexp.MustCompile(`^[0-9a-f]{16}$`)
	int64Pattern   = regexp.MustCompile(`^(0|[1-9][0-9]{0,18})$`)
)

type SourceRecord struct {
	SchemaVersion    string `json:"schema_version"`
	TraceID          string `json:"trace_id"`
	SpanID           string `json:"span_id"`
	ParentSpanID     string `json:"parent_span_id,omitempty"`
	SessionID        string `json:"session_id,omitempty"`
	SpanName         string `json:"span_name"`
	SpanKind         string `json:"span_kind"`
	StartTime        string `json:"start_time"`
	EndTime          string `json:"end_time"`
	Status           string `json:"status"`
	Model            string `json:"model"`
	Provider         string `json:"provider,omitempty"`
	PromptTokens     string `json:"prompt_tokens,omitempty"`
	CompletionTokens string `json:"completion_tokens,omitempty"`
	TotalTokens      string `json:"total_tokens,omitempty"`
	ActorRef         string `json:"actor_ref,omitempty"`
	ServiceName      string `json:"service_name,omitempty"`
	MissingParent    bool   `json:"missing_parent"`
}

type SanitizedRecord struct {
	Record         SourceRecord
	CanonicalBytes []byte
	SourceHash     string
}

type SanitizedBatch struct {
	Records       []SanitizedRecord
	SkippedNonLLM int
}

type MappingContext struct {
	ProducerInstance string
	TenantRef        *string
	OrgRef           string
	ProjectRef       string
}

type otlpExport struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource    `json:"resource"`
	ScopeSpans []otlpScopeSpan `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpKeyValue `json:"attributes"`
}

type otlpScopeSpan struct {
	Spans []otlpSpan `json:"spans"`
}

type otlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId,omitempty"`
	Name              string          `json:"name"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	EndTimeUnixNano   string          `json:"endTimeUnixNano"`
	Attributes        []otlpKeyValue  `json:"attributes"`
	Status            json.RawMessage `json:"status,omitempty"`
}

type otlpKeyValue struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

type otlpAnyValue struct {
	StringValue *string         `json:"stringValue,omitempty"`
	IntValue    json.RawMessage `json:"intValue,omitempty"`
}

func SanitizeOTLPExport(payload []byte) (SanitizedBatch, error) {
	if len(payload) == 0 || len(payload) > maxOTLPRequestBytes || !utf8.Valid(payload) {
		return SanitizedBatch{}, errors.New("OTLP export bytes are invalid")
	}
	if err := rejectDuplicateJSONKeys(payload); err != nil {
		return SanitizedBatch{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var export otlpExport
	if err := decoder.Decode(&export); err != nil {
		return SanitizedBatch{}, fmt.Errorf("decode OTLP export: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return SanitizedBatch{}, err
	}
	records := make([]SourceRecord, 0)
	skipped := 0
	for _, resourceGroup := range export.ResourceSpans {
		resource, err := attributesToStrings(resourceGroup.Resource.Attributes)
		if err != nil {
			return SanitizedBatch{}, err
		}
		for _, scopeGroup := range resourceGroup.ScopeSpans {
			for _, span := range scopeGroup.Spans {
				if len(records)+skipped >= maxOTLPSpans {
					return SanitizedBatch{}, errors.New("OTLP export exceeds the bounded span limit")
				}
				attributes, err := attributesToStrings(span.Attributes)
				if err != nil {
					return SanitizedBatch{}, err
				}
				kind := attributes["openinference.span.kind"]
				if kind != "LLM" {
					skipped++
					continue
				}
				record, err := sourceRecordFromSpan(span, attributes, resource)
				if err != nil {
					return SanitizedBatch{}, err
				}
				records = append(records, record)
			}
		}
	}
	if len(records) == 0 {
		return SanitizedBatch{}, errors.New("OTLP export contains no supported OpenInference LLM spans")
	}
	spanIDs := make(map[string]struct{}, len(records))
	for _, record := range records {
		spanIDs[record.TraceID+"\x00"+record.SpanID] = struct{}{}
	}
	output := SanitizedBatch{Records: make([]SanitizedRecord, 0, len(records)), SkippedNonLLM: skipped}
	for _, record := range records {
		if record.ParentSpanID != "" {
			_, parentPresent := spanIDs[record.TraceID+"\x00"+record.ParentSpanID]
			record.MissingParent = !parentPresent
		}
		sanitized, err := canonicalSourceRecord(record)
		if err != nil {
			return SanitizedBatch{}, err
		}
		output.Records = append(output.Records, sanitized)
	}
	sort.Slice(output.Records, func(left, right int) bool {
		a := output.Records[left].Record
		b := output.Records[right].Record
		if a.TraceID == b.TraceID {
			return a.SpanID < b.SpanID
		}
		return a.TraceID < b.TraceID
	})
	return output, nil
}

func sourceRecordFromSpan(
	span otlpSpan,
	attributes map[string]string,
	resource map[string]string,
) (SourceRecord, error) {
	start, err := exactMicrosecondFromUnixNano(span.StartTimeUnixNano)
	if err != nil {
		return SourceRecord{}, errors.New("OpenInference start timestamp is not exact microsecond time")
	}
	end, err := exactMicrosecondFromUnixNano(span.EndTimeUnixNano)
	if err != nil {
		return SourceRecord{}, errors.New("OpenInference end timestamp is not exact microsecond time")
	}
	status, err := spanStatus(span.Status)
	if err != nil {
		return SourceRecord{}, err
	}
	record := SourceRecord{
		SchemaVersion:    SourceSchemaV1,
		TraceID:          strings.ToLower(span.TraceID),
		SpanID:           strings.ToLower(span.SpanID),
		ParentSpanID:     strings.ToLower(span.ParentSpanID),
		SessionID:        attributes["session.id"],
		SpanName:         span.Name,
		SpanKind:         "LLM",
		StartTime:        start,
		EndTime:          end,
		Status:           status,
		Model:            attributes["llm.model_name"],
		Provider:         attributes["llm.provider"],
		PromptTokens:     attributes["llm.token_count.prompt"],
		CompletionTokens: attributes["llm.token_count.completion"],
		TotalTokens:      attributes["llm.token_count.total"],
		ActorRef:         attributes["user.id"],
		ServiceName:      resource["service.name"],
	}
	if err := validateSourceRecord(record); err != nil {
		return SourceRecord{}, err
	}
	return record, nil
}

func attributesToStrings(attributes []otlpKeyValue) (map[string]string, error) {
	result := make(map[string]string)
	for _, attribute := range attributes {
		if attribute.Key == "" || len(attribute.Key) > 240 {
			return nil, errors.New("OTLP attribute key is invalid")
		}
		if _, duplicate := result[attribute.Key]; duplicate {
			return nil, errors.New("OTLP attribute keys must be unique")
		}
		value, ok, err := attribute.Value.scalarString()
		if err != nil {
			return nil, err
		}
		if ok {
			result[attribute.Key] = value
		}
	}
	return result, nil
}

func (value otlpAnyValue) scalarString() (string, bool, error) {
	if value.StringValue != nil && len(value.IntValue) != 0 {
		return "", false, errors.New("OTLP AnyValue has multiple scalar values")
	}
	if value.StringValue != nil {
		if !validOpaque(*value.StringValue, 0, 512) {
			return "", false, errors.New("OTLP string attribute is invalid")
		}
		return *value.StringValue, true, nil
	}
	if len(value.IntValue) == 0 {
		return "", false, nil
	}
	var text string
	if err := json.Unmarshal(value.IntValue, &text); err != nil {
		return "", false, errors.New("OTLP int attribute must use protobuf JSON string encoding")
	}
	if _, err := parseCanonicalInt64(text); err != nil {
		return "", false, errors.New("OTLP int attribute is outside canonical int64")
	}
	return text, true, nil
}

func spanStatus(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("OTLP span status code is required")
	}
	var object struct {
		Code any `json:"code"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return "", errors.New("OTLP span status is invalid")
	}
	switch value := object.Code.(type) {
	case string:
		if value == "STATUS_CODE_ERROR" {
			return "failed", nil
		}
		if value == "STATUS_CODE_OK" {
			return "succeeded", nil
		}
	case json.Number:
		if value.String() == "2" {
			return "failed", nil
		}
		if value.String() == "1" {
			return "succeeded", nil
		}
	}
	return "", errors.New("OTLP span status code is invalid")
}

func exactMicrosecondFromUnixNano(value string) (string, error) {
	if !int64Pattern.MatchString(value) {
		return "", errors.New("timestamp is not canonical uint64 decimal")
	}
	nanos := new(big.Int)
	if _, ok := nanos.SetString(value, 10); !ok || !nanos.IsInt64() {
		return "", errors.New("timestamp is outside int64")
	}
	remainder := new(big.Int)
	seconds := new(big.Int)
	seconds.QuoRem(nanos, big.NewInt(1_000_000_000), remainder)
	if new(big.Int).Mod(remainder, big.NewInt(1000)).Sign() != 0 {
		return "", errors.New("timestamp has sub-microsecond precision")
	}
	instant := time.Unix(seconds.Int64(), remainder.Int64()).UTC()
	return instant.Format(microsecondTimeLayout), nil
}

func canonicalSourceRecord(record SourceRecord) (SanitizedRecord, error) {
	if err := validateSourceRecord(record); err != nil {
		return SanitizedRecord{}, err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return SanitizedRecord{}, err
	}
	var generic any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&generic); err != nil {
		return SanitizedRecord{}, err
	}
	canonical, err := connector.CanonicalizeJSONValue(generic)
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
		record.SpanKind != "LLM" ||
		(record.Status != "succeeded" && record.Status != "failed") ||
		!traceIDPattern.MatchString(record.TraceID) ||
		!spanIDPattern.MatchString(record.SpanID) ||
		(record.ParentSpanID != "" && !spanIDPattern.MatchString(record.ParentSpanID)) ||
		!validOpaque(record.SpanName, 1, 240) ||
		!validOpaque(record.Model, 1, 240) ||
		(record.Provider != "" && !validOpaque(record.Provider, 1, 160)) ||
		(record.SessionID != "" && !validOpaque(record.SessionID, 1, 256)) ||
		(record.ActorRef != "" && !validOpaque(record.ActorRef, 1, 256)) ||
		(record.ServiceName != "" && !validOpaque(record.ServiceName, 1, 240)) {
		return errors.New("OpenInference Source record is invalid")
	}
	start, err := time.Parse(microsecondTimeLayout, record.StartTime)
	if err != nil {
		return errors.New("OpenInference Source start time is invalid")
	}
	end, err := time.Parse(microsecondTimeLayout, record.EndTime)
	if err != nil || end.Before(start) {
		return errors.New("OpenInference Source end time is invalid")
	}
	for _, counter := range []string{record.PromptTokens, record.CompletionTokens, record.TotalTokens} {
		if counter != "" {
			if _, err := parseCanonicalInt64(counter); err != nil {
				return errors.New("OpenInference token counter is invalid")
			}
		}
	}
	if record.PromptTokens != "" && record.CompletionTokens != "" && record.TotalTokens != "" {
		prompt, _ := parseCanonicalInt64(record.PromptTokens)
		completion, _ := parseCanonicalInt64(record.CompletionTokens)
		total, _ := parseCanonicalInt64(record.TotalTokens)
		if prompt > total || completion > total-prompt || prompt+completion != total {
			return errors.New("OpenInference token counters are inconsistent")
		}
	}
	return nil
}

func parseCanonicalInt64(value string) (int64, error) {
	if !int64Pattern.MatchString(value) {
		return 0, errors.New("not a canonical nonnegative int64")
	}
	integer := new(big.Int)
	integer.SetString(value, 10)
	if !integer.IsInt64() {
		return 0, errors.New("outside int64")
	}
	return integer.Int64(), nil
}

func validOpaque(value string, minimum, maximum int) bool {
	if !utf8.ValidString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("OTLP export contains trailing content")
	}
	return nil
}

func rejectDuplicateJSONKeys(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := scanUniqueJSONValue(decoder); err != nil {
		return errors.New("OTLP export contains duplicate or invalid JSON fields")
	}
	return ensureJSONEOF(decoder)
}

func scanUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			rawKey, err := decoder.Token()
			key, ok := rawKey.(string)
			if err != nil || !ok {
				return errors.New("JSON object key is invalid")
			}
			if _, duplicate := seen[key]; duplicate {
				return errors.New("JSON object key is duplicated")
			}
			seen[key] = struct{}{}
			if err := scanUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := scanUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("JSON delimiter is invalid")
	}
}
