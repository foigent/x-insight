package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type EvidenceStore struct {
	db *sql.DB
}

type PersistenceReport struct {
	EventsTotal          int                 `json:"events_total"`
	Inserted             int                 `json:"inserted"`
	DuplicateNoop        int                 `json:"duplicate_noop"`
	IdempotencyConflict  int                 `json:"idempotency_conflict"`
	Rejected             int                 `json:"rejected"`
	CaptureGapsInserted  int                 `json:"capture_gaps_inserted"`
	CaptureGapsDuplicate int                 `json:"capture_gaps_duplicate"`
	Results              []PersistenceResult `json:"results"`
}

type PersistenceResult struct {
	SourceRef      string `json:"source_ref"`
	EventType      string `json:"event_type"`
	RequestID      string `json:"request_id,omitempty"`
	Outcome        string `json:"outcome"`
	Reason         string `json:"reason"`
	EventHash      string `json:"event_hash,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

type EvidenceEventRecord struct {
	ID                 string         `json:"id"`
	SchemaVersion      string         `json:"schema_version"`
	SourceSystemID     string         `json:"source_system_id"`
	AdapterManifestID  string         `json:"adapter_manifest_id"`
	SourceType         string         `json:"source_type"`
	SourceVersion      string         `json:"source_version,omitempty"`
	SourceEventID      string         `json:"source_event_id,omitempty"`
	SourceEventType    string         `json:"source_event_type,omitempty"`
	SourceRef          string         `json:"source_ref"`
	EventType          string         `json:"event_type"`
	Stage              string         `json:"stage"`
	Severity           string         `json:"severity"`
	CaptureMode        string         `json:"capture_mode"`
	OccurredAt         *time.Time     `json:"occurred_at,omitempty"`
	ObservedAt         *time.Time     `json:"observed_at,omitempty"`
	IngestedAt         time.Time      `json:"ingested_at"`
	RequestID          string         `json:"request_id,omitempty"`
	TraceID            string         `json:"trace_id,omitempty"`
	SpanID             string         `json:"span_id,omitempty"`
	OrgRef             string         `json:"org_ref,omitempty"`
	ProjectRef         string         `json:"project_ref,omitempty"`
	PayloadCaptureMode string         `json:"payload_capture_mode"`
	EventHash          string         `json:"event_hash"`
	SourceEventHash    string         `json:"source_event_hash,omitempty"`
	Correlation        map[string]any `json:"correlation,omitempty"`
	Identity           map[string]any `json:"identity,omitempty"`
	Capability         map[string]any `json:"capability,omitempty"`
	Decision           map[string]any `json:"decision,omitempty"`
	Usage              map[string]any `json:"usage,omitempty"`
	Outcome            map[string]any `json:"outcome,omitempty"`
	PayloadPolicy      map[string]any `json:"payload_policy,omitempty"`
	FieldProvenance    map[string]any `json:"field_provenance,omitempty"`
	RawSourceExcerpt   map[string]any `json:"raw_source_excerpt,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
}

type CaptureGapRecord struct {
	ID                string    `json:"id"`
	SourceSystemID    string    `json:"source_system_id"`
	AdapterManifestID string    `json:"adapter_manifest_id"`
	EvidenceEventID   string    `json:"evidence_event_id,omitempty"`
	RequestID         string    `json:"request_id,omitempty"`
	GapType           string    `json:"gap_type"`
	CandidateGapTypes []string  `json:"candidate_gap_types,omitempty"`
	AffectedClaim     string    `json:"affected_claim"`
	GapReason         string    `json:"gap_reason"`
	Severity          string    `json:"severity"`
	Recommendation    string    `json:"recommendation,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

func OpenEvidenceStore(ctx context.Context, dsn string) (*EvidenceStore, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &EvidenceStore{db: conn}, nil
}

func NewEvidenceStore(conn *sql.DB) *EvidenceStore {
	return &EvidenceStore{db: conn}
}

func (s *EvidenceStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *EvidenceStore) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("evidence store is not initialized")
	}
	return s.db.PingContext(ctx)
}

func (s *EvidenceStore) ListEvidenceEventsByRequestID(ctx context.Context, requestID string, limit int) ([]EvidenceEventRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("evidence store is not initialized")
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	limit = normalizeLimit(limit)

	rows, err := s.db.QueryContext(ctx, `
SELECT
  id::text, schema_version, source_system_id::text, adapter_manifest_id::text, source_type,
  source_version, source_event_id, source_event_type, source_ref, event_type, stage,
  severity, capture_mode, occurred_at, observed_at, ingested_at, request_id, trace_id,
  span_id, org_ref, project_ref, payload_capture_mode, event_hash, source_event_hash,
  correlation::text, identity::text, capability::text, decision::text, usage_data::text,
  outcome::text, payload_policy::text, field_provenance::text, raw_source_excerpt::text,
  created_at
FROM evidence_events
WHERE request_id = $1
ORDER BY COALESCE(occurred_at, observed_at, ingested_at), created_at, id
LIMIT $2`,
		requestID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []EvidenceEventRecord
	for rows.Next() {
		var record EvidenceEventRecord
		var sourceVersion, sourceEventID, sourceEventType, request, traceID, spanID, orgRef, projectRef, sourceEventHash sql.NullString
		var occurredAt, observedAt sql.NullTime
		var correlation, identity, capability, decision, usage, outcome, payloadPolicy, fieldProvenance, rawSourceExcerpt string
		if err := rows.Scan(
			&record.ID,
			&record.SchemaVersion,
			&record.SourceSystemID,
			&record.AdapterManifestID,
			&record.SourceType,
			&sourceVersion,
			&sourceEventID,
			&sourceEventType,
			&record.SourceRef,
			&record.EventType,
			&record.Stage,
			&record.Severity,
			&record.CaptureMode,
			&occurredAt,
			&observedAt,
			&record.IngestedAt,
			&request,
			&traceID,
			&spanID,
			&orgRef,
			&projectRef,
			&record.PayloadCaptureMode,
			&record.EventHash,
			&sourceEventHash,
			&correlation,
			&identity,
			&capability,
			&decision,
			&usage,
			&outcome,
			&payloadPolicy,
			&fieldProvenance,
			&rawSourceExcerpt,
			&record.CreatedAt,
		); err != nil {
			return nil, err
		}

		record.SourceVersion = nullStringValue(sourceVersion)
		record.SourceEventID = nullStringValue(sourceEventID)
		record.SourceEventType = nullStringValue(sourceEventType)
		record.RequestID = nullStringValue(request)
		record.TraceID = nullStringValue(traceID)
		record.SpanID = nullStringValue(spanID)
		record.OrgRef = nullStringValue(orgRef)
		record.ProjectRef = nullStringValue(projectRef)
		record.SourceEventHash = nullStringValue(sourceEventHash)
		record.OccurredAt = nullTimeValue(occurredAt)
		record.ObservedAt = nullTimeValue(observedAt)
		record.Correlation = decodeJSONMap(correlation)
		record.Identity = decodeJSONMap(identity)
		record.Capability = decodeJSONMap(capability)
		record.Decision = decodeJSONMap(decision)
		record.Usage = decodeJSONMap(usage)
		record.Outcome = decodeJSONMap(outcome)
		record.PayloadPolicy = decodeJSONMap(payloadPolicy)
		record.FieldProvenance = decodeJSONMap(fieldProvenance)
		record.RawSourceExcerpt = decodeJSONMap(rawSourceExcerpt)
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *EvidenceStore) ListCaptureGapsByRequestID(ctx context.Context, requestID string, limit int) ([]CaptureGapRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("evidence store is not initialized")
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	limit = normalizeLimit(limit)

	rows, err := s.db.QueryContext(ctx, `
SELECT
  id::text, source_system_id::text, adapter_manifest_id::text,
  evidence_event_id::text, request_id, gap_type, array_to_json(candidate_gap_types)::text, affected_claim, gap_reason,
  severity, recommendation, created_at
FROM capture_gaps
WHERE request_id = $1
ORDER BY created_at, id
LIMIT $2`,
		requestID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []CaptureGapRecord
	for rows.Next() {
		var record CaptureGapRecord
		var evidenceEventID, request, recommendation sql.NullString
		var candidateGapTypes string
		if err := rows.Scan(
			&record.ID,
			&record.SourceSystemID,
			&record.AdapterManifestID,
			&evidenceEventID,
			&request,
			&record.GapType,
			&candidateGapTypes,
			&record.AffectedClaim,
			&record.GapReason,
			&record.Severity,
			&recommendation,
			&record.CreatedAt,
		); err != nil {
			return nil, err
		}
		record.EvidenceEventID = nullStringValue(evidenceEventID)
		record.RequestID = nullStringValue(request)
		record.CandidateGapTypes = decodeJSONStringSlice(candidateGapTypes)
		record.Recommendation = nullStringValue(recommendation)
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *EvidenceStore) PersistEvents(ctx context.Context, events []map[string]any) (PersistenceReport, error) {
	if s == nil || s.db == nil {
		return PersistenceReport{}, fmt.Errorf("evidence store is not initialized")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PersistenceReport{}, err
	}
	defer tx.Rollback()

	report := PersistenceReport{EventsTotal: len(events)}
	for _, event := range events {
		if err := ensureDependencies(ctx, tx, event); err != nil {
			return report, err
		}
		result, insertedEventID, err := persistEvent(ctx, tx, event)
		if err != nil {
			return report, err
		}
		report.Results = append(report.Results, result)
		switch result.Outcome {
		case "inserted":
			report.Inserted++
			if hasCaptureGap(event) {
				inserted, err := persistCaptureGap(ctx, tx, event, insertedEventID)
				if err != nil {
					return report, err
				}
				if inserted {
					report.CaptureGapsInserted++
				} else {
					report.CaptureGapsDuplicate++
				}
			}
		case "duplicate_noop":
			report.DuplicateNoop++
		case "idempotency_conflict":
			report.IdempotencyConflict++
		case "rejected":
			report.Rejected++
		}
	}
	if err := tx.Commit(); err != nil {
		return report, err
	}
	return report, nil
}

func ensureDependencies(ctx context.Context, tx *sql.Tx, event map[string]any) error {
	sourceSystemID := requiredString(event, "source_system_id")
	adapterManifestID := requiredString(event, "adapter_manifest_id")
	sourceType := valueOr(requiredString(event, "source_type"), "unknown")
	sourceVersion := stringField(event, "source_version")

	if sourceSystemID == "" || adapterManifestID == "" {
		return fmt.Errorf("source_system_id and adapter_manifest_id are required")
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO source_systems (
  id, source_key, source_type, display_name, source_version, status, metadata
) VALUES (
  $1, $2, $3, $4, $5, 'active', '{}'::jsonb
)
ON CONFLICT (id) DO NOTHING`,
		sourceSystemID,
		"source-system:"+sourceSystemID,
		sourceType,
		"Imported "+sourceType+" source",
		nullString(sourceVersion),
	); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO adapter_manifests (
  id, adapter_id, adapter_version, source_type, fidelity_level, ingestion_modes,
  direct_field_groups, derived_field_groups, known_capture_gaps, payload_behavior,
  idempotency_strategy, cursor_strategy, declared_event_types, manifest_json, active
) VALUES (
  $1, $2, $3, $4, 'F2', ARRAY['db_import']::text[],
  ARRAY['correlation','identity','capability','usage','payload_policy']::text[],
  ARRAY['outcome']::text[], ARRAY['not_available_from_source','provider_not_returned','redacted','sampled_out','retention_expired','not_instrumented','permission_denied','decision_not_emitted']::text[],
  'metadata_only_default', 'source_ref_event_type_source_event_id', 'source_ref', ARRAY[]::text[], '{}'::jsonb, true
)
ON CONFLICT (id) DO NOTHING`,
		adapterManifestID,
		"adapter-unirouter:"+adapterManifestID,
		valueOr(sourceVersion, "m0"),
		sourceType,
	); err != nil {
		return err
	}

	return nil
}

func persistEvent(ctx context.Context, tx *sql.Tx, event map[string]any) (PersistenceResult, string, error) {
	result := PersistenceResult{
		SourceRef:      requiredString(event, "source_ref"),
		EventType:      requiredString(event, "event_type"),
		RequestID:      stringField(event, "request_id"),
		EventHash:      requiredString(event, "event_hash"),
		IdempotencyKey: requiredString(event, "idempotency_key"),
	}
	eventID := requiredString(event, "id")
	if eventID == "" || result.SourceRef == "" || result.IdempotencyKey == "" || result.EventHash == "" {
		result.Outcome = "rejected"
		result.Reason = "missing_required_persistence_field"
		return result, "", nil
	}

	inserted, err := insertEvidenceEvent(ctx, tx, event)
	if err != nil {
		return result, "", err
	}
	if inserted {
		result.Outcome = "inserted"
		result.Reason = "inserted"
		return result, eventID, nil
	}

	var existingHash string
	err = tx.QueryRowContext(ctx, `
SELECT event_hash
FROM evidence_events
WHERE source_system_id = $1 AND adapter_manifest_id = $2 AND idempotency_key = $3`,
		requiredString(event, "source_system_id"),
		requiredString(event, "adapter_manifest_id"),
		result.IdempotencyKey,
	).Scan(&existingHash)
	if err != nil {
		return result, "", err
	}
	if existingHash == result.EventHash {
		result.Outcome = "duplicate_noop"
		result.Reason = "duplicate_noop"
		return result, "", nil
	}
	result.Outcome = "idempotency_conflict"
	result.Reason = "same_idempotency_key_different_hash"
	return result, "", nil
}

func insertEvidenceEvent(ctx context.Context, tx *sql.Tx, event map[string]any) (bool, error) {
	rows, err := tx.QueryContext(ctx, `
INSERT INTO evidence_events (
  id, schema_version, source_system_id, adapter_manifest_id, source_type,
  source_version, source_event_id, source_event_type, source_ref, idempotency_key,
  import_cursor, event_type, stage, severity, capture_mode, occurred_at,
  observed_at, ingested_at, request_id, trace_id, span_id, org_ref, project_ref,
  payload_capture_mode, event_hash, source_event_hash, correlation, identity,
  capability, decision, usage_data, outcome, payload_policy, field_provenance,
  raw_source_excerpt
) VALUES (
  $1, $2, $3, $4, $5,
  $6, $7, $8, $9, $10,
  $11, $12, $13, $14, $15, $16,
  $17, $18, $19, $20, $21, $22, $23,
  $24, $25, $26, $27::jsonb, $28::jsonb,
  $29::jsonb, $30::jsonb, $31::jsonb, $32::jsonb, $33::jsonb, $34::jsonb,
  $35::jsonb
)
ON CONFLICT (source_system_id, adapter_manifest_id, idempotency_key) DO NOTHING
RETURNING id`,
		requiredString(event, "id"),
		requiredString(event, "schema_version"),
		requiredString(event, "source_system_id"),
		requiredString(event, "adapter_manifest_id"),
		requiredString(event, "source_type"),
		nullString(stringField(event, "source_version")),
		nullString(stringField(event, "source_event_id")),
		nullString(stringField(event, "source_event_type")),
		requiredString(event, "source_ref"),
		requiredString(event, "idempotency_key"),
		nullString(stringField(event, "import_cursor")),
		requiredString(event, "event_type"),
		requiredString(event, "stage"),
		requiredString(event, "severity"),
		requiredString(event, "capture_mode"),
		nullString(stringField(event, "occurred_at")),
		nullString(stringField(event, "observed_at")),
		requiredString(event, "ingested_at"),
		nullString(stringField(event, "request_id")),
		nullString(stringField(event, "trace_id")),
		nullString(stringField(event, "span_id")),
		nullString(stringField(event, "org_ref")),
		nullString(stringField(event, "project_ref")),
		requiredString(event, "payload_capture_mode"),
		requiredString(event, "event_hash"),
		nullString(stringField(event, "source_event_hash")),
		jsonString(event["correlation"]),
		jsonString(event["identity"]),
		jsonString(event["capability"]),
		jsonString(event["decision"]),
		jsonString(event["usage"]),
		jsonString(event["outcome"]),
		jsonString(event["payload_policy"]),
		jsonString(event["field_provenance"]),
		jsonString(event["raw_source_excerpt"]),
	)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

func persistCaptureGap(ctx context.Context, tx *sql.Tx, event map[string]any, eventID string) (bool, error) {
	raw, _ := event["raw_source_excerpt"].(map[string]any)
	gapType := valueOr(stringField(raw, "capture_gap"), "unknown")
	candidateGapTypes := stringSliceField(raw, "candidate_capture_gaps")
	affectedClaim := valueOr(stringField(raw, "gap_source"), stringField(event, "event_type"))
	gapReason := valueOr(stringField(raw, "reason"), "capture gap imported from evidence event")
	gapID := deterministicUUID("capture_gap:" + eventID)

	rows, err := tx.QueryContext(ctx, `
INSERT INTO capture_gaps (
  id, source_system_id, adapter_manifest_id, evidence_event_id, request_id,
  gap_type, candidate_gap_types, affected_claim, gap_reason, severity, recommendation
) VALUES (
  $1, $2, $3, $4, $5,
  $6, $7::text[], $8, $9, $10, $11
)
ON CONFLICT (id) DO NOTHING
RETURNING id`,
		gapID,
		requiredString(event, "source_system_id"),
		requiredString(event, "adapter_manifest_id"),
		eventID,
		nullString(stringField(event, "request_id")),
		gapType,
		postgresTextArray(candidateGapTypes),
		affectedClaim,
		gapReason,
		valueOr(stringField(event, "severity"), "warning"),
		nullString("review source visibility and adapter coverage"),
	)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

func isGapEvent(event map[string]any) bool {
	return stringField(event, "stage") == "gap" || stringField(event, "event_type") == "gap.capture.detected"
}

func hasCaptureGap(event map[string]any) bool {
	if isGapEvent(event) {
		return true
	}
	raw, _ := event["raw_source_excerpt"].(map[string]any)
	return stringField(raw, "capture_gap") != ""
}

func jsonString(value any) string {
	if value == nil {
		return "{}"
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	if string(bytes) == "null" {
		return "{}"
	}
	return string(bytes)
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func requiredString(event map[string]any, key string) string {
	return stringField(event, key)
}

func stringField(event map[string]any, key string) string {
	if event == nil {
		return ""
	}
	value, _ := event[key].(string)
	return value
}

func valueOr(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 200
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func nullTimeValue(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func decodeJSONMap(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" || raw == "null" || raw == "{}" {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return map[string]any{"_decode_error": "invalid_json"}
	}
	if len(decoded) == 0 {
		return nil
	}
	return decoded
}

func decodeJSONStringSlice(raw string) []string {
	if strings.TrimSpace(raw) == "" || raw == "null" || raw == "[]" {
		return nil
	}
	var decoded []string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	if len(decoded) == 0 {
		return nil
	}
	return decoded
}

func stringSliceField(event map[string]any, key string) []string {
	if event == nil {
		return nil
	}
	switch value := event[key].(type) {
	case []string:
		return cleanStrings(value)
	case []any:
		values := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return cleanStrings(values)
	default:
		return nil
	}
}

func cleanStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func postgresTextArray(values []string) string {
	if len(values) == 0 {
		return "{}"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ReplaceAll(value, `\`, `\\`)
		value = strings.ReplaceAll(value, `"`, `\"`)
		quoted = append(quoted, `"`+value+`"`)
	}
	return "{" + strings.Join(quoted, ",") + "}"
}

func deterministicUUID(input string) string {
	sum := sha256String(input)
	return fmt.Sprintf("%s-%s-%s-%s-%s", sum[0:8], sum[8:12], "5"+sum[13:16], "8"+sum[17:20], sum[20:32])
}

func sha256String(input string) string {
	sum := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", sum[:])
}
