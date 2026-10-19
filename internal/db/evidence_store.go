package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/foigent/x-insight/internal/connectorsync"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/investigation"
)

type EvidenceStore struct {
	db *sql.DB
}

const (
	MaxPersistEvents                  = 10000
	MaxNormalizedEventSize            = 256 << 10
	uniRouterSnapshotSourceVersionV1  = "unirouter.prd5.xinsight.snapshot.v1"
	uniRouterSnapshotSourceVersionV2  = "unirouter.prd5.xinsight.snapshot.v2"
	uniRouterCompletedSourceVersionV1 = "unirouter.completed-execution.v1"
	uniRouterEvidenceSourceVersionV2  = "unirouter.execution-evidence.bundle.v2"
	uniRouterAdapterManifestIDV1      = "22222222-2222-2222-2222-222222222222"
	uniRouterAdapterManifestIDV2      = "22222222-2222-4222-8222-222222222223"
	uniRouterCompletedAdapterIDV1     = "22222222-2222-4222-8222-222222222224"
	uniRouterEvidenceAdapterIDV2      = "22222222-2222-4222-8222-222222222225"
)

var sha256ReferencePattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type PersistenceReport struct {
	EventsTotal          int                 `json:"events_total"`
	Inserted             int                 `json:"inserted"`
	DuplicateNoop        int                 `json:"duplicate_noop"`
	IdempotencyConflict  int                 `json:"idempotency_conflict"`
	Rejected             int                 `json:"rejected"`
	CaptureGapsInserted  int                 `json:"capture_gaps_inserted"`
	CaptureGapsDuplicate int                 `json:"capture_gaps_duplicate"`
	ImportProvenanceID   string              `json:"import_provenance_id,omitempty"`
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
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(30 * time.Minute)
	conn.SetConnMaxIdleTime(5 * time.Minute)
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

func (s *EvidenceStore) Readiness(ctx context.Context, expected []MigrationChecksum) (ReadinessStatus, error) {
	if s == nil || s.db == nil {
		return ReadinessStatus{}, fmt.Errorf("evidence store is not initialized")
	}
	if err := validateExpectedMigrations(expected); err != nil {
		return ReadinessStatus{}, err
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return ReadinessStatus{}, err
	}
	defer conn.Close()
	records, err := loadMigrationRecords(ctx, conn)
	if err != nil {
		return ReadinessStatus{}, err
	}
	if len(records) != len(expected) {
		return ReadinessStatus{}, fmt.Errorf("%w: applied=%d expected=%d", ErrMigrationSetMismatch, len(records), len(expected))
	}
	if err := validateAppliedMigrations(expected, records); err != nil {
		return ReadinessStatus{}, err
	}
	migrationDigest, err := MigrationSetSHA256(expected)
	if err != nil {
		return ReadinessStatus{}, err
	}
	lastID := expected[len(expected)-1].ID
	separator := strings.IndexByte(lastID, '_')
	if separator < 1 {
		return ReadinessStatus{}, fmt.Errorf("invalid current migration identity")
	}
	schemaNumber, err := strconv.Atoi(lastID[:separator])
	if err != nil || schemaNumber < 1 {
		return ReadinessStatus{}, fmt.Errorf("invalid current migration schema version")
	}
	var versionNumber int
	if err := conn.QueryRowContext(ctx, `SELECT current_setting('server_version_num')::integer`).Scan(&versionNumber); err != nil {
		return ReadinessStatus{}, err
	}
	var (
		sourceRowsPresent          bool
		evidenceRowsPresent        bool
		captureGapRowsPresent      bool
		provenanceRowsPresent      bool
		provenanceEventRowsPresent bool
		operationalReceiptPresent  bool
	)
	if err := conn.QueryRowContext(ctx, `
SELECT
  EXISTS (SELECT 1 FROM source_systems LIMIT 1),
  EXISTS (SELECT 1 FROM evidence_events LIMIT 1),
  EXISTS (SELECT 1 FROM capture_gaps LIMIT 1),
  EXISTS (SELECT 1 FROM import_provenance LIMIT 1),
  EXISTS (SELECT 1 FROM import_provenance_events LIMIT 1),
  EXISTS (SELECT 1 FROM connector_operational_receipts LIMIT 1)
`).Scan(
		&sourceRowsPresent,
		&evidenceRowsPresent,
		&captureGapRowsPresent,
		&provenanceRowsPresent,
		&provenanceEventRowsPresent,
		&operationalReceiptPresent,
	); err != nil {
		return ReadinessStatus{}, err
	}
	_ = sourceRowsPresent
	_ = evidenceRowsPresent
	_ = captureGapRowsPresent
	_ = provenanceRowsPresent
	_ = provenanceEventRowsPresent
	_ = operationalReceiptPresent
	return ReadinessStatus{
		DatabaseSchema:     strconv.Itoa(schemaNumber),
		MigrationCount:     len(records),
		MigrationSetSHA256: migrationDigest,
		PostgresMajor:      versionNumber / 10000,
		RepresentativeRead: true,
	}, nil
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
	return s.persistEvents(ctx, events, nil)
}

func (s *EvidenceStore) PersistEventsWithProvenance(ctx context.Context, events []map[string]any, provenance investigation.ImportProvenance) (PersistenceReport, error) {
	return s.persistEvents(ctx, events, &provenance)
}

func (s *EvidenceStore) persistEvents(ctx context.Context, events []map[string]any, provenance *investigation.ImportProvenance) (PersistenceReport, error) {
	if s == nil || s.db == nil {
		return PersistenceReport{}, fmt.Errorf("evidence store is not initialized")
	}
	if len(events) == 0 {
		return PersistenceReport{}, fmt.Errorf("at least one evidence event is required")
	}
	if len(events) > MaxPersistEvents {
		return PersistenceReport{}, fmt.Errorf("event batch exceeds %d events", MaxPersistEvents)
	}
	for index, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return PersistenceReport{}, fmt.Errorf("encode event %d for size validation: %w", index, err)
		}
		if len(encoded) > MaxNormalizedEventSize {
			return PersistenceReport{}, fmt.Errorf("event %d exceeds %d bytes", index, MaxNormalizedEventSize)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PersistenceReport{}, err
	}
	defer tx.Rollback()
	if syncErr := connectorsync.CommitFence(ctx, tx); syncErr != nil {
		return PersistenceReport{}, syncErr
	}
	var writerLockAcquired bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock_shared($1)`, MaintenanceLockKey).Scan(&writerLockAcquired); err != nil {
		return PersistenceReport{}, err
	}
	if !writerLockAcquired {
		return PersistenceReport{}, fmt.Errorf("evidence writer is busy")
	}

	report := PersistenceReport{EventsTotal: len(events)}
	var provenanceID string
	linkLegacyProvenance := false
	provenanceProofClass := ""
	if provenance != nil {
		if err := ensureDependencies(ctx, tx, events[0]); err != nil {
			return report, err
		}
		provenanceID, err = persistImportProvenance(ctx, tx, events, *provenance)
		if err != nil {
			return report, err
		}
		report.ImportProvenanceID = provenanceID
		linkLegacyProvenance = provenance.ProofClass == investigation.ProofOperatorApproved
		provenanceProofClass = provenance.ProofClass
	}
	for _, event := range events {
		if err := ensureDependencies(ctx, tx, event); err != nil {
			return report, err
		}
		result, persistedEventID, associateProvenance, err := persistEvent(ctx, tx, event, provenanceID, provenanceProofClass, linkLegacyProvenance)
		if err != nil {
			return report, err
		}
		if result.Outcome == "inserted" || result.Outcome == "duplicate_noop" {
			if err := projectWorkbenchEconomicFact(ctx, tx, event, persistedEventID); err != nil {
				return report, err
			}
		}
		if provenanceID != "" && associateProvenance {
			if err := persistImportProvenanceEvent(ctx, tx, provenanceID, persistedEventID, event); err != nil {
				return report, err
			}
		}
		report.Results = append(report.Results, result)
		switch result.Outcome {
		case "inserted":
			report.Inserted++
			if hasCaptureGap(event) {
				inserted, err := persistCaptureGap(ctx, tx, event, persistedEventID)
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

func persistImportProvenance(ctx context.Context, tx *sql.Tx, events []map[string]any, provenance investigation.ImportProvenance) (string, error) {
	first := events[0]
	sourceSystemID := requiredString(first, "source_system_id")
	adapterManifestID := requiredString(first, "adapter_manifest_id")
	if provenance.SourceSystemID == "" {
		provenance.SourceSystemID = sourceSystemID
	}
	if provenance.AdapterManifestID == "" {
		provenance.AdapterManifestID = adapterManifestID
	}
	if provenance.SourceSystemID != sourceSystemID || provenance.AdapterManifestID != adapterManifestID {
		return "", fmt.Errorf("import provenance source or adapter does not match evidence events")
	}
	for _, event := range events[1:] {
		if requiredString(event, "source_system_id") != sourceSystemID || requiredString(event, "adapter_manifest_id") != adapterManifestID {
			return "", fmt.Errorf("one import provenance record cannot span sources or adapters")
		}
	}
	if err := validateImportProvenance(provenance); err != nil {
		return "", err
	}
	if provenance.ID == "" {
		provenance.ID = deterministicImportProvenanceID(provenance)
	}

	_, err := tx.ExecContext(ctx, `
INSERT INTO import_provenance (
  id, source_system_id, adapter_manifest_id, integration_mode, proof_class,
  assurance_mode, source_artifact_sha256, source_payload_sha256,
  approval_manifest_sha256, exchange_id, source_generated_at, source_terminal_at,
  source_revision, imported_revision, source_watermark, import_watermark,
  timeline_complete
) VALUES (
  $1, $2, $3, $4, $5,
  $6, $7, $8,
  $9, $10, $11, $12,
  $13, $14, $15, $16,
  $17
)
ON CONFLICT (id) DO NOTHING`,
		provenance.ID,
		provenance.SourceSystemID,
		provenance.AdapterManifestID,
		provenance.IntegrationMode,
		provenance.ProofClass,
		provenance.AssuranceMode,
		nullString(provenance.SourceArtifactSHA256),
		nullString(provenance.SourcePayloadSHA256),
		nullString(provenance.ApprovalManifestSHA256),
		nullString(provenance.ExchangeID),
		provenance.SourceGeneratedAt,
		provenance.SourceTerminalAt,
		nullString(provenance.SourceRevision),
		nullString(provenance.ImportedRevision),
		nullString(provenance.SourceWatermark),
		nullString(provenance.ImportWatermark),
		provenance.TimelineComplete,
	)
	if err != nil {
		return "", err
	}
	var matches bool
	err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM import_provenance
  WHERE id = $1
    AND source_system_id = $2
    AND adapter_manifest_id = $3
    AND integration_mode = $4
    AND proof_class = $5
    AND assurance_mode = $6
    AND source_artifact_sha256 IS NOT DISTINCT FROM $7
    AND source_payload_sha256 IS NOT DISTINCT FROM $8
    AND approval_manifest_sha256 IS NOT DISTINCT FROM $9
    AND exchange_id IS NOT DISTINCT FROM $10
    AND source_generated_at IS NOT DISTINCT FROM $11
    AND source_terminal_at IS NOT DISTINCT FROM $12
    AND source_revision IS NOT DISTINCT FROM $13
    AND imported_revision IS NOT DISTINCT FROM $14
    AND source_watermark IS NOT DISTINCT FROM $15
    AND import_watermark IS NOT DISTINCT FROM $16
    AND timeline_complete = $17
)`,
		provenance.ID,
		provenance.SourceSystemID,
		provenance.AdapterManifestID,
		provenance.IntegrationMode,
		provenance.ProofClass,
		provenance.AssuranceMode,
		nullString(provenance.SourceArtifactSHA256),
		nullString(provenance.SourcePayloadSHA256),
		nullString(provenance.ApprovalManifestSHA256),
		nullString(provenance.ExchangeID),
		provenance.SourceGeneratedAt,
		provenance.SourceTerminalAt,
		nullString(provenance.SourceRevision),
		nullString(provenance.ImportedRevision),
		nullString(provenance.SourceWatermark),
		nullString(provenance.ImportWatermark),
		provenance.TimelineComplete,
	).Scan(&matches)
	if err != nil {
		return "", err
	}
	if !matches {
		return "", fmt.Errorf("import provenance id conflicts with an existing immutable record")
	}
	return provenance.ID, nil
}

func deterministicImportProvenanceID(provenance investigation.ImportProvenance) string {
	identityParts := []string{
		"import_provenance",
		provenance.SourceSystemID,
		provenance.AdapterManifestID,
		provenance.IntegrationMode,
		provenance.ProofClass,
		provenance.AssuranceMode,
		provenance.SourceArtifactSHA256,
		provenance.ApprovalManifestSHA256,
		provenance.ExchangeID,
		provenance.SourceRevision,
		provenance.ImportedRevision,
		provenance.SourceWatermark,
		provenance.ImportWatermark,
		strconv.FormatBool(provenance.TimelineComplete),
		timeValue(provenance.SourceGeneratedAt),
		timeValue(provenance.SourceTerminalAt),
	}
	if provenance.SourcePayloadSHA256 != "" {
		identityParts = append(identityParts, "source_payload_sha256", provenance.SourcePayloadSHA256)
	}
	return deterministicUUID(strings.Join(identityParts, "\x00"))
}

func validateImportProvenance(provenance investigation.ImportProvenance) error {
	switch provenance.ProofClass {
	case investigation.ProofOperatorApproved:
		switch provenance.IntegrationMode {
		case investigation.IntegrationSidecar:
			if provenance.AssuranceMode != investigation.AssuranceOperatorAttested {
				return fmt.Errorf("sidecar integration requires operator-attested assurance")
			}
			if provenance.SourceArtifactSHA256 == "" || provenance.ApprovalManifestSHA256 == "" {
				return fmt.Errorf("operator-approved sidecar proof requires artifact and approval manifest hashes")
			}
			if strings.TrimSpace(provenance.ExchangeID) == "" {
				return fmt.Errorf("operator-approved sidecar proof requires an exchange ID")
			}
			if provenance.SourcePayloadSHA256 != "" {
				return fmt.Errorf("operator-approved sidecar proof must not include a source payload hash")
			}
		case investigation.IntegrationPull, investigation.IntegrationConnectorPull:
			if provenance.AssuranceMode != investigation.AssuranceContractedPull {
				return fmt.Errorf("contracted pull requires contracted-pull assurance")
			}
			if provenance.SourcePayloadSHA256 == "" {
				return fmt.Errorf("contracted pull requires a source payload hash")
			}
			if provenance.SourceArtifactSHA256 != "" || provenance.ApprovalManifestSHA256 != "" || provenance.ExchangeID != "" {
				return fmt.Errorf("contracted pull must not include sidecar artifact, approval manifest, or exchange fields")
			}
			if provenance.SourceWatermark != "" || provenance.ImportWatermark != "" {
				return fmt.Errorf("contracted pull checkpoint and watermark belong only to durable worker state")
			}
		case investigation.IntegrationBoundedOffline:
			if provenance.AssuranceMode != investigation.AssuranceBoundedOffline {
				return fmt.Errorf("bounded offline import requires bounded-offline assurance")
			}
			if provenance.SourcePayloadSHA256 == "" {
				return fmt.Errorf("bounded offline import requires a source payload hash")
			}
			if provenance.SourceArtifactSHA256 != "" || provenance.ApprovalManifestSHA256 != "" || provenance.ExchangeID != "" {
				return fmt.Errorf("bounded offline import must not include sidecar artifact, approval manifest, or exchange fields")
			}
			if provenance.SourceWatermark != "" || provenance.ImportWatermark != "" {
				return fmt.Errorf("bounded offline delivery state belongs only to an operational receipt")
			}
		default:
			return fmt.Errorf("operator-approved proof requires sidecar, contracted pull, or bounded offline integration")
		}
	case investigation.ProofSyntheticFixture:
		if provenance.IntegrationMode != investigation.IntegrationFixture || provenance.AssuranceMode != investigation.AssuranceSyntheticFixture {
			return fmt.Errorf("synthetic proof requires synthetic integration and assurance modes")
		}
		if provenance.SourceArtifactSHA256 == "" {
			return fmt.Errorf("synthetic proof requires an artifact hash")
		}
		if provenance.SourcePayloadSHA256 != "" || provenance.ApprovalManifestSHA256 != "" || provenance.ExchangeID != "" {
			return fmt.Errorf("synthetic proof must not include live payload or sidecar approval fields")
		}
	default:
		return fmt.Errorf("unsupported import proof class")
	}
	switch provenance.AssuranceMode {
	case investigation.AssuranceOperatorAttested, investigation.AssuranceContractedPull, investigation.AssuranceBoundedOffline, investigation.AssuranceSyntheticFixture:
	default:
		return fmt.Errorf("unsupported import assurance mode")
	}
	if provenance.SourceArtifactSHA256 != "" && !sha256ReferencePattern.MatchString(provenance.SourceArtifactSHA256) {
		return fmt.Errorf("source artifact hash must be a SHA-256 reference")
	}
	if provenance.SourcePayloadSHA256 != "" && !sha256ReferencePattern.MatchString(provenance.SourcePayloadSHA256) {
		return fmt.Errorf("source payload hash must be a SHA-256 reference")
	}
	if provenance.ApprovalManifestSHA256 != "" && !sha256ReferencePattern.MatchString(provenance.ApprovalManifestSHA256) {
		return fmt.Errorf("approval manifest hash must be a SHA-256 reference")
	}
	return nil
}

func ensureDependencies(ctx context.Context, tx *sql.Tx, event map[string]any) error {
	sourceSystemID := requiredString(event, "source_system_id")
	adapterManifestID := requiredString(event, "adapter_manifest_id")
	sourceType := valueOr(requiredString(event, "source_type"), "unknown")
	sourceVersion := stringField(event, "source_version")
	eventType := requiredString(event, "event_type")
	captureMode := requiredString(event, "capture_mode")

	if sourceSystemID == "" || adapterManifestID == "" {
		return fmt.Errorf("source_system_id and adapter_manifest_id are required")
	}
	registered, err := useProductManagedRegistry(
		ctx,
		tx,
		sourceSystemID,
		adapterManifestID,
		sourceType,
		sourceVersion,
		eventType,
		captureMode,
	)
	if err != nil {
		return err
	}
	if registered {
		return nil
	}
	if sourceType != "unirouter" {
		return fmt.Errorf(
			"source %s and adapter %s require an exact active product-managed Connector registry",
			sourceSystemID,
			adapterManifestID,
		)
	}
	if sourceType == "unirouter" &&
		sourceVersion != "" &&
		sourceVersion != uniRouterSnapshotSourceVersionV1 &&
		sourceVersion != uniRouterSnapshotSourceVersionV2 &&
		sourceVersion != uniRouterCompletedSourceVersionV1 &&
		sourceVersion != uniRouterEvidenceSourceVersionV2 {
		return fmt.Errorf("source-version transition to %q is unsupported for UniRouter", sourceVersion)
	}

	sourceResult, err := tx.ExecContext(ctx, `
INSERT INTO source_systems (
  id, source_key, source_type, display_name, source_version, status, metadata
) VALUES (
  $1, $2, $3, $4, $5, 'active', '{}'::jsonb
)
ON CONFLICT (id) DO UPDATE SET
  source_version = CASE
    WHEN EXCLUDED.source_version IS NULL THEN source_systems.source_version
    WHEN source_systems.source_version IS NULL THEN EXCLUDED.source_version
    WHEN source_systems.source_version = EXCLUDED.source_version THEN source_systems.source_version
    WHEN source_systems.source_type = 'unirouter'
      AND source_systems.source_version IN ($6, $7, $8, $9)
      AND EXCLUDED.source_version IN ($6, $7, $8, $9)
      THEN CASE
        WHEN source_systems.source_version = $9 OR EXCLUDED.source_version = $9 THEN $9
        WHEN source_systems.source_version = $8 OR EXCLUDED.source_version = $8 THEN $8
        WHEN source_systems.source_version = $7 OR EXCLUDED.source_version = $7 THEN $7
        ELSE $6
      END
    ELSE source_systems.source_version
  END
WHERE source_systems.source_type = EXCLUDED.source_type
  AND (
    EXCLUDED.source_version IS NULL
    OR source_systems.source_version IS NULL
    OR source_systems.source_version = EXCLUDED.source_version
    OR (
      source_systems.source_type = 'unirouter'
      AND source_systems.source_version IN ($6, $7, $8, $9)
      AND EXCLUDED.source_version IN ($6, $7, $8, $9)
    )
  )`,
		sourceSystemID,
		"source-system:"+sourceSystemID,
		sourceType,
		"Imported "+sourceType+" source",
		nullString(sourceVersion),
		uniRouterSnapshotSourceVersionV1,
		uniRouterSnapshotSourceVersionV2,
		uniRouterCompletedSourceVersionV1,
		uniRouterEvidenceSourceVersionV2,
	)
	if err != nil {
		return err
	}
	sourceRowsAffected, err := sourceResult.RowsAffected()
	if err != nil {
		return err
	}
	if sourceRowsAffected != 1 {
		return fmt.Errorf("source system %s conflicts with an existing source type or source-version transition", sourceSystemID)
	}
	if err := validateReservedAdapterManifestBinding(sourceType, sourceVersion, adapterManifestID); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
INSERT INTO adapter_manifests (
  id, adapter_id, adapter_version, source_type, fidelity_level, ingestion_modes,
  direct_field_groups, derived_field_groups, known_capture_gaps, payload_behavior,
  idempotency_strategy, cursor_strategy, declared_event_types, manifest_json, active
) VALUES (
  $1, $2, $3, $4, 'F2',
  CASE WHEN $3 = $5 THEN ARRAY['api_import']::text[] ELSE ARRAY['db_import']::text[] END,
  CASE WHEN $3 = $5
    THEN ARRAY['correlation','identity','capability','usage','outcome']::text[]
    WHEN $3 = $6
    THEN ARRAY['correlation','identity','capability','decision','usage','outcome']::text[]
    ELSE ARRAY['correlation','identity','capability','usage','payload_policy']::text[]
  END,
  CASE WHEN $3 = $5
    THEN ARRAY['payload_policy']::text[]
    WHEN $3 = $6
    THEN ARRAY['payload_policy']::text[]
    ELSE ARRAY['outcome']::text[]
  END,
  ARRAY['not_available_from_source','provider_not_returned','redacted','sampled_out','retention_expired','not_instrumented','permission_denied','decision_not_emitted']::text[],
  'metadata_only_default', 'source_ref_event_type_source_event_id', 'source_ref',
  CASE WHEN $3 = $5
    THEN ARRAY['billing.execution.cancelled','billing.execution.settled','gap.capture.detected']::text[]
    ELSE ARRAY[]::text[]
  END,
  '{}'::jsonb, true
)
ON CONFLICT (id) DO UPDATE SET
  adapter_version = CASE
    WHEN adapter_manifests.adapter_version = 'm0' AND EXCLUDED.adapter_version <> 'm0'
      THEN EXCLUDED.adapter_version
    ELSE adapter_manifests.adapter_version
  END,
  active = adapter_manifests.active
WHERE adapter_manifests.adapter_id = EXCLUDED.adapter_id
  AND adapter_manifests.source_type = EXCLUDED.source_type
  AND (
    EXCLUDED.adapter_version NOT IN ($5, $6)
    OR (
      adapter_manifests.ingestion_modes = EXCLUDED.ingestion_modes
      AND adapter_manifests.direct_field_groups = EXCLUDED.direct_field_groups
      AND adapter_manifests.derived_field_groups = EXCLUDED.derived_field_groups
      AND adapter_manifests.known_capture_gaps = EXCLUDED.known_capture_gaps
      AND adapter_manifests.payload_behavior = EXCLUDED.payload_behavior
      AND adapter_manifests.idempotency_strategy = EXCLUDED.idempotency_strategy
      AND adapter_manifests.cursor_strategy IS NOT DISTINCT FROM EXCLUDED.cursor_strategy
      AND adapter_manifests.declared_event_types = EXCLUDED.declared_event_types
      AND adapter_manifests.manifest_json = EXCLUDED.manifest_json
      AND adapter_manifests.active
    )
  )
  AND (
    adapter_manifests.adapter_version = EXCLUDED.adapter_version
    OR adapter_manifests.adapter_version = 'm0'
    OR EXCLUDED.adapter_version = 'm0'
  )`,
		adapterManifestID,
		"adapter-unirouter:"+adapterManifestID,
		valueOr(sourceVersion, "m0"),
		sourceType,
		uniRouterCompletedSourceVersionV1,
		uniRouterEvidenceSourceVersionV2,
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return fmt.Errorf("adapter manifest %s conflicts with an existing mapper version", adapterManifestID)
	}

	return nil
}

// useProductManagedRegistry lets an isolated Connector pre-register an exact
// source/adapter identity without teaching EvidenceStore about that product.
// Legacy adapters do not carry this marker and retain the historical seeding
// path below.
func useProductManagedRegistry(
	ctx context.Context,
	tx *sql.Tx,
	sourceSystemID string,
	adapterManifestID string,
	sourceType string,
	sourceVersion string,
	eventType string,
	captureMode string,
) (bool, error) {
	var registeredSourceType string
	var registeredSourceVersion sql.NullString
	var adapterSourceType string
	var sourceConnectorID sql.NullString
	var sourceConnectorVersion sql.NullString
	var sourceProtocolVersion sql.NullString
	var adapterConnectorID sql.NullString
	var adapterConnectorVersion sql.NullString
	var adapterProtocolVersion sql.NullString
	var sourceActive bool
	var adapterActive bool
	var sourceVersionDeclared bool
	var captureModeDeclared bool
	var eventTypeDeclared bool
	err := tx.QueryRowContext(ctx, `
SELECT
  source.source_type,
  source.source_version,
  adapter.source_type,
  source.metadata ->> 'connector_id',
  source.metadata ->> 'connector_version',
  source.metadata ->> 'protocol_version',
  adapter.manifest_json ->> 'connector_id',
  adapter.manifest_json ->> 'connector_version',
  adapter.manifest_json ->> 'protocol_version',
  source.status = 'active',
  adapter.active,
  $3::text = ANY(COALESCE(adapter.supported_source_versions, ARRAY[]::text[])),
  $4::text = ANY(COALESCE(adapter.ingestion_modes, ARRAY[]::text[])),
  $5::text = ANY(COALESCE(adapter.declared_event_types, ARRAY[]::text[]))
FROM source_systems AS source
JOIN adapter_manifests AS adapter ON adapter.id = $2::uuid
WHERE source.id = $1::uuid
  AND COALESCE((adapter.manifest_json ->> 'product_managed_registry')::boolean, false)`,
		sourceSystemID,
		adapterManifestID,
		sourceVersion,
		captureMode,
		eventType,
	).Scan(
		&registeredSourceType,
		&registeredSourceVersion,
		&adapterSourceType,
		&sourceConnectorID,
		&sourceConnectorVersion,
		&sourceProtocolVersion,
		&adapterConnectorID,
		&adapterConnectorVersion,
		&adapterProtocolVersion,
		&sourceActive,
		&adapterActive,
		&sourceVersionDeclared,
		&captureModeDeclared,
		&eventTypeDeclared,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read product-managed Connector registry: %w", err)
	}
	if !sourceActive || !adapterActive ||
		!sourceVersionDeclared ||
		!captureModeDeclared ||
		!eventTypeDeclared ||
		registeredSourceType != sourceType ||
		adapterSourceType != sourceType ||
		registeredSourceVersion.String != sourceVersion ||
		registeredSourceVersion.Valid != (sourceVersion != "") ||
		!sourceConnectorID.Valid ||
		!sourceConnectorVersion.Valid ||
		!sourceProtocolVersion.Valid ||
		!adapterConnectorID.Valid ||
		!adapterConnectorVersion.Valid ||
		!adapterProtocolVersion.Valid ||
		sourceConnectorID.String == "" ||
		sourceConnectorVersion.String == "" ||
		sourceProtocolVersion.String == "" ||
		sourceConnectorID.String != adapterConnectorID.String ||
		sourceConnectorVersion.String != adapterConnectorVersion.String ||
		sourceProtocolVersion.String != adapterProtocolVersion.String {
		return false, fmt.Errorf(
			"source %s and adapter %s conflict with product-managed Connector registry",
			sourceSystemID,
			adapterManifestID,
		)
	}
	return true, nil
}

func validateReservedAdapterManifestBinding(sourceType string, sourceVersion string, adapterManifestID string) error {
	reservedVersion := ""
	switch adapterManifestID {
	case uniRouterAdapterManifestIDV1:
		reservedVersion = uniRouterSnapshotSourceVersionV1
	case uniRouterAdapterManifestIDV2:
		reservedVersion = uniRouterSnapshotSourceVersionV2
	case uniRouterCompletedAdapterIDV1:
		reservedVersion = uniRouterCompletedSourceVersionV1
	case uniRouterEvidenceAdapterIDV2:
		reservedVersion = uniRouterEvidenceSourceVersionV2
	default:
		if sourceType == "unirouter" &&
			(sourceVersion == uniRouterCompletedSourceVersionV1 ||
				sourceVersion == uniRouterEvidenceSourceVersionV2) {
			reservedID := uniRouterCompletedAdapterIDV1
			if sourceVersion == uniRouterEvidenceSourceVersionV2 {
				reservedID = uniRouterEvidenceAdapterIDV2
			}
			return fmt.Errorf("adapter manifest %s conflicts with reserved UniRouter mapper version %s at %s", adapterManifestID, sourceVersion, reservedID)
		}
		return nil
	}
	if sourceType != "unirouter" ||
		(sourceVersion != "" && sourceVersion != reservedVersion) ||
		((reservedVersion == uniRouterCompletedSourceVersionV1 ||
			reservedVersion == uniRouterEvidenceSourceVersionV2) && sourceVersion == "") {
		return fmt.Errorf("adapter manifest %s conflicts with its reserved UniRouter mapper version %s", adapterManifestID, reservedVersion)
	}
	return nil
}

func persistEvent(ctx context.Context, tx *sql.Tx, event map[string]any, provenanceID string, provenanceProofClass string, linkLegacyProvenance bool) (PersistenceResult, string, bool, error) {
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
		return result, "", false, nil
	}

	inserted, err := insertEvidenceEvent(ctx, tx, event, provenanceID)
	if err != nil {
		return result, "", false, err
	}
	if inserted {
		result.Outcome = "inserted"
		result.Reason = "inserted"
		return result, eventID, true, nil
	}

	var existingEventID, existingHash, existingProofClass string
	err = tx.QueryRowContext(ctx, `
SELECT e.id::text, e.event_hash, COALESCE(p.proof_class, '')
FROM evidence_events e
LEFT JOIN import_provenance p ON p.id = e.import_provenance_id
WHERE e.source_system_id = $1 AND e.adapter_manifest_id = $2 AND e.idempotency_key = $3`,
		requiredString(event, "source_system_id"),
		requiredString(event, "adapter_manifest_id"),
		result.IdempotencyKey,
	).Scan(&existingEventID, &existingHash, &existingProofClass)
	if err != nil {
		return result, "", false, err
	}
	if existingHash == result.EventHash {
		if provenanceID != "" && linkLegacyProvenance {
			if _, err := tx.ExecContext(ctx, `
UPDATE evidence_events
SET import_provenance_id = $1
WHERE source_system_id = $2
  AND adapter_manifest_id = $3
  AND idempotency_key = $4
  AND import_provenance_id IS NULL`,
				provenanceID,
				requiredString(event, "source_system_id"),
				requiredString(event, "adapter_manifest_id"),
				result.IdempotencyKey,
			); err != nil {
				return result, "", false, err
			}
		}
		result.Outcome = "duplicate_noop"
		result.Reason = "duplicate_noop"
		associate := linkLegacyProvenance || (provenanceProofClass != "" && existingProofClass == provenanceProofClass)
		return result, existingEventID, associate, nil
	}
	result.Outcome = "idempotency_conflict"
	result.Reason = "same_idempotency_key_different_hash"
	return result, "", false, nil
}

func persistImportProvenanceEvent(ctx context.Context, tx *sql.Tx, provenanceID string, eventID string, event map[string]any) error {
	executionID := ""
	requestID := stringField(event, "request_id")
	if requestID != "" {
		var err error
		executionID, err = executionid.New(requiredString(event, "source_system_id"), requestID)
		if err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO import_provenance_events (
  import_provenance_id, evidence_event_id, execution_id
) VALUES ($1, $2, $3)
ON CONFLICT (import_provenance_id, evidence_event_id) DO NOTHING`,
		provenanceID,
		eventID,
		nullString(executionID),
	)
	return err
}

func timeValue(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func insertEvidenceEvent(ctx context.Context, tx *sql.Tx, event map[string]any, provenanceID string) (bool, error) {
	executionID := ""
	requestID := stringField(event, "request_id")
	if requestID != "" {
		var err error
		executionID, err = executionid.New(requiredString(event, "source_system_id"), requestID)
		if err != nil {
			return false, err
		}
	}
	rows, err := tx.QueryContext(ctx, `
INSERT INTO evidence_events (
  id, schema_version, source_system_id, adapter_manifest_id, source_type,
  source_version, source_event_id, source_event_type, source_ref, idempotency_key,
  import_cursor, event_type, stage, severity, capture_mode, occurred_at,
  observed_at, ingested_at, request_id, execution_id, trace_id, span_id, org_ref, project_ref,
  payload_capture_mode, event_hash, source_event_hash, correlation, identity,
  capability, decision, usage_data, outcome, payload_policy, field_provenance,
  raw_source_excerpt, import_provenance_id
) VALUES (
  $1, $2, $3, $4, $5,
  $6, $7, $8, $9, $10,
  $11, $12, $13, $14, $15, $16,
  $17, $18, $19, $20, $21, $22, $23, $24,
  $25, $26, $27, $28::jsonb, $29::jsonb,
  $30::jsonb, $31::jsonb, $32::jsonb, $33::jsonb, $34::jsonb, $35::jsonb,
  $36::jsonb, $37
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
		nullString(requestID),
		nullString(executionID),
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
		nullString(provenanceID),
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
	utc := value.Time.UTC()
	return &utc
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
