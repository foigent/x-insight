package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/investigation"
)

var (
	ErrExecutionNotFound = errors.New("execution not found")
	ErrEventNotFound     = errors.New("event not found")
)

func (s *EvidenceStore) SearchExecutions(ctx context.Context, query investigation.SearchQuery) ([]investigation.ExecutionSummary, error) {
	return s.queryExecutions(ctx, query, "")
}

func (s *EvidenceStore) GetExecution(ctx context.Context, executionID string, asOf time.Time) (investigation.ExecutionDetail, error) {
	query := investigation.SearchQuery{
		AsOf:  asOf,
		From:  time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
		Limit: 1,
	}
	summaries, err := s.queryExecutions(ctx, query, executionID)
	if err != nil {
		return investigation.ExecutionDetail{}, err
	}
	if len(summaries) == 0 {
		return investigation.ExecutionDetail{}, ErrExecutionNotFound
	}
	traceIDs, err := s.listExecutionTraceIDs(ctx, executionID, asOf, 100)
	if err != nil {
		return investigation.ExecutionDetail{}, err
	}
	return investigation.ExecutionDetail{Summary: summaries[0], AsOf: asOf.UTC(), TraceIDs: traceIDs}, nil
}

func (s *EvidenceStore) queryExecutions(ctx context.Context, query investigation.SearchQuery, executionID string) ([]investigation.ExecutionSummary, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("evidence store is not initialized")
	}
	if query.Limit < 1 {
		return nil, fmt.Errorf("execution query limit must be positive")
	}
	args := make([]any, 0, 12)
	bind := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	asOfParameter := bind(query.AsOf.UTC())
	fromParameter := bind(query.From.UTC())
	toParameter := bind(query.To.UTC())
	candidateConditions := []string{
		"e.request_id IS NOT NULL",
		"e.request_id <> ''",
		"e.execution_id IS NOT NULL",
		"e.ingested_at <= " + asOfParameter,
		"e.created_at <= " + asOfParameter,
		"COALESCE(e.occurred_at, e.observed_at, e.ingested_at) >= " + fromParameter,
		"COALESCE(e.occurred_at, e.observed_at, e.ingested_at) <= " + toParameter,
	}
	if query.RequestID != "" {
		candidateConditions = append(candidateConditions, "e.request_id = "+bind(query.RequestID))
	}
	if query.RequestPrefix != "" {
		candidateConditions = append(candidateConditions, "starts_with(e.request_id, "+bind(query.RequestPrefix)+")")
	}
	if query.SourceSystemID != "" {
		candidateConditions = append(candidateConditions, "e.source_system_id = "+bind(query.SourceSystemID)+"::uuid")
	}
	if executionID != "" {
		candidateConditions = append(candidateConditions, "e.execution_id = "+bind(executionID))
	}
	finalConditions := []string{
		"last_effective_at >= " + fromParameter,
		"last_effective_at <= " + toParameter,
	}
	if query.Outcome != "" {
		finalConditions = append(finalConditions, "execution_outcome = "+bind(query.Outcome))
	}
	if query.HasGaps != nil {
		finalConditions = append(finalConditions, "(gap_count > 0) = "+bind(*query.HasGaps))
	}
	if query.After != nil {
		finalConditions = append(finalConditions,
			"(last_effective_at, execution_id) < ("+bind(query.After.LastEffectiveAt.UTC())+", "+bind(query.After.ExecutionID)+")",
		)
	}
	limitParameter := bind(query.Limit)

	statement := fmt.Sprintf(`
WITH candidate_executions AS (
  SELECT DISTINCT e.source_system_id, e.request_id, e.execution_id
  FROM evidence_events e
  WHERE %s
), event_rollup AS (
  SELECT
    e.source_system_id,
    e.request_id,
    min(e.execution_id) AS execution_id,
    min(e.source_type) AS source_type,
    min(COALESCE(e.occurred_at, e.observed_at, e.ingested_at)) AS first_effective_at,
    max(COALESCE(e.occurred_at, e.observed_at, e.ingested_at)) AS last_effective_at,
    max(e.ingested_at) AS last_imported_at,
    count(*)::bigint AS event_count,
    CASE
      WHEN bool_and(e.source_type = 'unirouter')
        AND count(*) FILTER (WHERE e.source_event_type = 'call_log') = 1
      THEN CASE max(CASE WHEN e.source_event_type = 'call_log' THEN
        CASE e.outcome->>'outcome_status'
          WHEN 'failure' THEN 4
          WHEN 'partial' THEN 3
          WHEN 'success' THEN 2
          ELSE 1
        END
      END)
        WHEN 4 THEN 'failure'
        WHEN 3 THEN 'partial'
        WHEN 2 THEN 'success'
        ELSE 'unknown'
      END
      ELSE CASE max(CASE e.outcome->>'outcome_status'
        WHEN 'failure' THEN 4
        WHEN 'partial' THEN 3
        WHEN 'success' THEN 2
        ELSE 1
      END)
        WHEN 4 THEN 'failure'
        WHEN 3 THEN 'partial'
        WHEN 2 THEN 'success'
        ELSE 'unknown'
      END
    END AS execution_outcome,
    array_to_json(array_agg(DISTINCT e.payload_capture_mode ORDER BY e.payload_capture_mode))::text AS payload_modes
  FROM evidence_events e
  JOIN candidate_executions candidate
    ON candidate.source_system_id = e.source_system_id
    AND candidate.request_id = e.request_id
  WHERE e.request_id IS NOT NULL
    AND e.ingested_at <= %s
    AND e.created_at <= %s
  GROUP BY e.source_system_id, e.request_id
), gap_rollup AS (
  SELECT g.source_system_id, g.request_id, count(*)::bigint AS gap_count
  FROM capture_gaps g
  JOIN candidate_executions candidate
    ON candidate.source_system_id = g.source_system_id
    AND candidate.request_id = g.request_id
  WHERE g.request_id IS NOT NULL
    AND g.created_at <= %s
  GROUP BY g.source_system_id, g.request_id
), executions AS (
  SELECT
    er.execution_id,
    er.request_id,
    er.source_system_id::text,
    er.source_type,
    ss.display_name,
    er.first_effective_at,
    er.last_effective_at,
    er.last_imported_at,
    er.event_count,
    er.execution_outcome,
    er.payload_modes,
    COALESCE(gr.gap_count, 0)::bigint AS gap_count,
    ip.integration_mode,
    classification.proof_class,
    classification.assurance_mode,
    ip.source_generated_at,
    ip.source_terminal_at,
    ip.source_revision,
    ip.imported_revision,
    ip.source_watermark,
    ip.import_watermark,
    ip.timeline_complete,
    ip.imported_at,
    ip.search_visible_at
  FROM event_rollup er
  JOIN source_systems ss ON ss.id = er.source_system_id
  LEFT JOIN gap_rollup gr
    ON gr.source_system_id = er.source_system_id AND gr.request_id = er.request_id
  LEFT JOIN LATERAL (
    SELECT p.*
    FROM import_provenance_events ipe
    JOIN import_provenance p ON p.id = ipe.import_provenance_id
    WHERE ipe.execution_id = er.execution_id
      AND ipe.created_at <= %s
      AND p.created_at <= %s
      AND p.imported_at <= %s
    ORDER BY p.imported_at DESC, p.id DESC
    LIMIT 1
  ) ip ON true
  LEFT JOIN LATERAL (
    SELECT
      CASE
        WHEN coverage.approved_events = er.event_count THEN 'operator_approved_integration'
        WHEN coverage.synthetic_events = er.event_count THEN 'synthetic_fixture'
      END AS proof_class,
      CASE
        WHEN coverage.approved_events = er.event_count
          AND coverage.approved_assurance_count = 1
        THEN coverage.approved_assurance
        WHEN coverage.synthetic_events = er.event_count
          AND coverage.synthetic_assurance_count = 1
        THEN coverage.synthetic_assurance
      END AS assurance_mode
    FROM (
      SELECT
        count(DISTINCT linked.evidence_event_id) FILTER (
          WHERE p.proof_class = 'operator_approved_integration'
        ) AS approved_events,
        count(DISTINCT linked.evidence_event_id) FILTER (
          WHERE p.proof_class = 'synthetic_fixture'
        ) AS synthetic_events,
        count(DISTINCT p.assurance_mode) FILTER (
          WHERE p.proof_class = 'operator_approved_integration'
        ) AS approved_assurance_count,
        min(p.assurance_mode) FILTER (
          WHERE p.proof_class = 'operator_approved_integration'
        ) AS approved_assurance,
        count(DISTINCT p.assurance_mode) FILTER (
          WHERE p.proof_class = 'synthetic_fixture'
        ) AS synthetic_assurance_count,
        min(p.assurance_mode) FILTER (
          WHERE p.proof_class = 'synthetic_fixture'
        ) AS synthetic_assurance
      FROM import_provenance_events linked
      JOIN import_provenance p ON p.id = linked.import_provenance_id
      JOIN evidence_events linked_event ON linked_event.id = linked.evidence_event_id
      WHERE linked.execution_id = er.execution_id
        AND linked.created_at <= %s
        AND p.created_at <= %s
        AND p.imported_at <= %s
        AND linked_event.ingested_at <= %s
        AND linked_event.created_at <= %s
    ) coverage
  ) classification ON true
)
SELECT
  execution_id, request_id, source_system_id, source_type, display_name,
  execution_outcome, first_effective_at, last_effective_at, last_imported_at,
  event_count, gap_count, payload_modes,
  integration_mode, proof_class, assurance_mode,
  source_generated_at, source_terminal_at, source_revision, imported_revision,
  source_watermark, import_watermark, timeline_complete, imported_at,
  search_visible_at
FROM executions
WHERE %s
ORDER BY last_effective_at DESC, execution_id DESC
LIMIT %s`,
		strings.Join(candidateConditions, "\n    AND "),
		asOfParameter,
		asOfParameter,
		asOfParameter,
		asOfParameter,
		asOfParameter,
		asOfParameter,
		asOfParameter,
		asOfParameter,
		asOfParameter,
		asOfParameter,
		asOfParameter,
		strings.Join(finalConditions, "\n  AND "),
		limitParameter,
	)

	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := make([]investigation.ExecutionSummary, 0, query.Limit)
	for rows.Next() {
		summary, err := scanExecutionSummary(rows)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

type rowScanner interface {
	Scan(...any) error
}

func scanExecutionSummary(row rowScanner) (investigation.ExecutionSummary, error) {
	var summary investigation.ExecutionSummary
	var eventCount, gapCount int64
	var payloadModes string
	var integrationMode, proofClass, assuranceMode sql.NullString
	var sourceGeneratedAt, sourceTerminalAt, importedAt, searchVisibleAt sql.NullTime
	var sourceRevision, importedRevision, sourceWatermark, importWatermark sql.NullString
	var timelineComplete sql.NullBool
	var lastImportedAt time.Time
	if err := row.Scan(
		&summary.ExecutionID,
		&summary.RequestID,
		&summary.SourceSystemID,
		&summary.SourceType,
		&summary.SourceDisplayName,
		&summary.Outcome,
		&summary.FirstEffectiveAt,
		&summary.LastEffectiveAt,
		&lastImportedAt,
		&eventCount,
		&gapCount,
		&payloadModes,
		&integrationMode,
		&proofClass,
		&assuranceMode,
		&sourceGeneratedAt,
		&sourceTerminalAt,
		&sourceRevision,
		&importedRevision,
		&sourceWatermark,
		&importWatermark,
		&timelineComplete,
		&importedAt,
		&searchVisibleAt,
	); err != nil {
		return investigation.ExecutionSummary{}, err
	}
	summary.EventCount = int(eventCount)
	summary.FirstEffectiveAt = summary.FirstEffectiveAt.UTC()
	summary.LastEffectiveAt = summary.LastEffectiveAt.UTC()
	lastImportedAt = lastImportedAt.UTC()
	summary.GapCount = int(gapCount)
	summary.HasGaps = gapCount > 0
	summary.PayloadCaptureModes = decodeJSONStringSlice(payloadModes)
	summary.ProofClass = investigation.ProofUnclassified
	if proofClass.Valid {
		summary.ProofClass = proofClass.String
	}
	summary.AssuranceMode = investigation.AssuranceUnclassified
	if assuranceMode.Valid {
		summary.AssuranceMode = assuranceMode.String
	}
	summary.Freshness = buildFreshness(
		integrationMode,
		gapCount,
		lastImportedAt,
		timelineComplete,
		sourceGeneratedAt,
		sourceTerminalAt,
		sourceRevision,
		importedRevision,
		sourceWatermark,
		importWatermark,
		importedAt,
		searchVisibleAt,
	)
	return summary, nil
}

func buildFreshness(
	integrationMode sql.NullString,
	gapCount int64,
	lastImportedAt time.Time,
	timelineComplete sql.NullBool,
	sourceGeneratedAt sql.NullTime,
	sourceTerminalAt sql.NullTime,
	sourceRevision sql.NullString,
	importedRevision sql.NullString,
	sourceWatermark sql.NullString,
	importWatermark sql.NullString,
	importedAt sql.NullTime,
	searchVisibleAt sql.NullTime,
) investigation.Freshness {
	freshness := investigation.Freshness{
		IntegrationMode:  investigation.IntegrationSidecar,
		SyncState:        investigation.SyncStateStale,
		TimelineComplete: timelineComplete.Valid && timelineComplete.Bool,
		ImportedAt:       timePointer(lastImportedAt),
	}
	if integrationMode.Valid {
		freshness.IntegrationMode = integrationMode.String
	}
	if gapCount > 0 {
		freshness.SyncState = investigation.SyncStateGap
	} else if integrationMode.Valid && !freshness.TimelineComplete {
		freshness.SyncState = investigation.SyncStateSyncing
	} else if integrationMode.Valid && freshness.IntegrationMode != investigation.IntegrationSidecar {
		freshness.SyncState = investigation.SyncStateCurrent
	}
	if sourceGeneratedAt.Valid {
		freshness.SourceGeneratedAt = timePointer(sourceGeneratedAt.Time)
	}
	if sourceTerminalAt.Valid {
		freshness.SourceTerminalAt = timePointer(sourceTerminalAt.Time)
	}
	if importedAt.Valid {
		freshness.ImportedAt = timePointer(importedAt.Time)
		freshness.LastSuccessfulSyncAt = timePointer(importedAt.Time)
	}
	if searchVisibleAt.Valid {
		freshness.SearchVisibleAt = timePointer(searchVisibleAt.Time)
	}
	freshness.SourceRevision = nullStringValue(sourceRevision)
	freshness.ImportedRevision = nullStringValue(importedRevision)
	freshness.SourceWatermark = nullStringValue(sourceWatermark)
	freshness.ImportWatermark = nullStringValue(importWatermark)
	return freshness
}

func (s *EvidenceStore) listExecutionTraceIDs(ctx context.Context, executionID string, asOf time.Time, limit int) ([]string, error) {
	statement := `
SELECT DISTINCT trace_id
FROM evidence_events e
WHERE e.request_id IS NOT NULL
  AND e.trace_id IS NOT NULL
  AND e.trace_id <> ''
  AND e.ingested_at <= $2
  AND e.created_at <= $2
  AND e.execution_id = $1
ORDER BY trace_id
LIMIT $3`
	rows, err := s.db.QueryContext(ctx, statement, executionID, asOf.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var traceIDs []string
	for rows.Next() {
		var traceID string
		if err := rows.Scan(&traceID); err != nil {
			return nil, err
		}
		traceIDs = append(traceIDs, traceID)
	}
	return traceIDs, rows.Err()
}

func (s *EvidenceStore) ListTimeline(ctx context.Context, query investigation.TimelineQuery) ([]investigation.TimelineNode, int, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("evidence store is not initialized")
	}
	if query.Limit < 1 {
		return nil, 0, fmt.Errorf("timeline query limit must be positive")
	}
	executionPredicate := `
e.request_id IS NOT NULL
  AND e.ingested_at <= $2
  AND e.created_at <= $2
  AND e.execution_id = $1`
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM evidence_events e WHERE "+executionPredicate, query.ExecutionID, query.AsOf.UTC()).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, ErrExecutionNotFound
	}
	afterSet := query.After != nil
	afterEffectiveAt := query.AsOf
	afterIngestedAt := query.AsOf
	afterEventID := "00000000-0000-0000-0000-000000000000"
	if query.After != nil {
		afterEffectiveAt = query.After.EffectiveAt
		afterIngestedAt = query.After.IngestedAt
		afterEventID = query.After.EventID
	}
	statement := fmt.Sprintf(`
SELECT
  e.id::text, e.event_type, e.stage, e.severity,
  COALESCE(e.occurred_at, e.observed_at, e.ingested_at) AS effective_at,
  e.occurred_at, e.observed_at, e.ingested_at,
  e.correlation->>'parent_span_id' AS parent_span_id,
  parent.parent_event_id,
  e.source_ref, e.source_type,
  e.decision->>'decision_result' AS decision_result,
  e.outcome->>'outcome_status' AS outcome_status,
  e.field_provenance::text
FROM evidence_events e
LEFT JOIN LATERAL (
  SELECT CASE WHEN count(*) = 1 THEN min(candidate.id::text) END AS parent_event_id
  FROM evidence_events candidate
  WHERE NULLIF(e.correlation->>'parent_span_id', '') IS NOT NULL
    AND candidate.source_system_id = e.source_system_id
    AND candidate.request_id = e.request_id
    AND candidate.span_id = e.correlation->>'parent_span_id'
    AND candidate.ingested_at <= $2
    AND candidate.created_at <= $2
) parent ON true
WHERE %s
  AND (NOT $3 OR (
    COALESCE(e.occurred_at, e.observed_at, e.ingested_at), e.ingested_at, e.id
  ) > ($4, $5, $6::uuid))
ORDER BY COALESCE(e.occurred_at, e.observed_at, e.ingested_at), e.ingested_at, e.id
LIMIT $7`, executionPredicate)
	rows, err := s.db.QueryContext(ctx, statement,
		query.ExecutionID,
		query.AsOf.UTC(),
		afterSet,
		afterEffectiveAt.UTC(),
		afterIngestedAt.UTC(),
		afterEventID,
		query.Limit,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	nodes := make([]investigation.TimelineNode, 0, query.Limit)
	for rows.Next() {
		var node investigation.TimelineNode
		var occurredAt, observedAt sql.NullTime
		var parentSpanID, parentEventID, decisionResult, outcomeStatus sql.NullString
		var fieldProvenance string
		if err := rows.Scan(
			&node.EventID,
			&node.EventType,
			&node.Stage,
			&node.Severity,
			&node.EffectiveAt,
			&occurredAt,
			&observedAt,
			&node.IngestedAt,
			&parentSpanID,
			&parentEventID,
			&node.SourceRef,
			&node.SourceType,
			&decisionResult,
			&outcomeStatus,
			&fieldProvenance,
		); err != nil {
			return nil, 0, err
		}
		node.OccurredAt = nullTimeValue(occurredAt)
		node.ObservedAt = nullTimeValue(observedAt)
		node.EffectiveAt = node.EffectiveAt.UTC()
		node.IngestedAt = node.IngestedAt.UTC()
		node.DecisionResult = nullStringValue(decisionResult)
		node.OutcomeStatus = nullStringValue(outcomeStatus)
		if err := decodeJSONStruct(fieldProvenance, &node.FieldProvenance); err != nil {
			return nil, 0, err
		}
		if parentSpanID.Valid && parentSpanID.String != "" {
			resolved := parentEventID.Valid
			node.DeclaredParentResolved = &resolved
			node.DeclaredParentEventID = nullStringValue(parentEventID)
		}
		nodes = append(nodes, node)
	}
	return nodes, total, rows.Err()
}

const evidenceEventDetailColumns = `
  e.id::text, e.event_type, e.stage, e.severity, e.capture_mode,
  e.occurred_at, e.observed_at, e.ingested_at, e.payload_capture_mode,
  e.correlation::text, e.identity::text, e.capability::text, e.decision::text,
  e.usage_data::text, e.outcome::text, e.payload_policy::text,
  e.field_provenance::text,
  e.source_system_id::text, e.adapter_manifest_id::text, e.schema_version,
  e.source_type, e.source_version, e.source_event_id, e.source_event_type,
  e.source_ref, e.event_hash, e.source_event_hash, am.fidelity_level`

func (s *EvidenceStore) ListExecutionEvents(
	ctx context.Context,
	executionID string,
	asOf time.Time,
	limit int,
) ([]investigation.EvidenceEventDetail, int, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("evidence store is not initialized")
	}
	if limit < 1 {
		return nil, 0, fmt.Errorf("event detail query limit must be positive")
	}
	const predicate = `
e.request_id IS NOT NULL
  AND e.ingested_at <= $2
  AND e.created_at <= $2
  AND e.execution_id = $1`
	var total int
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT count(*) FROM evidence_events e WHERE "+predicate,
		executionID,
		asOf.UTC(),
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, ErrExecutionNotFound
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT`+evidenceEventDetailColumns+`
FROM evidence_events e
JOIN adapter_manifests am ON am.id = e.adapter_manifest_id
WHERE `+predicate+`
ORDER BY COALESCE(e.occurred_at, e.observed_at, e.ingested_at), e.ingested_at, e.id
LIMIT $3`, executionID, asOf.UTC(), limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]investigation.EvidenceEventDetail, 0, limit)
	for rows.Next() {
		detail, err := scanEvidenceEventDetail(rows, executionID)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, detail)
	}
	return items, total, rows.Err()
}

func (s *EvidenceStore) GetExecutionEvent(ctx context.Context, executionID string, eventID string, asOf time.Time) (investigation.EvidenceEventDetail, error) {
	if s == nil || s.db == nil {
		return investigation.EvidenceEventDetail{}, fmt.Errorf("evidence store is not initialized")
	}
	statement := `SELECT` + evidenceEventDetailColumns + `
FROM evidence_events e
JOIN adapter_manifests am ON am.id = e.adapter_manifest_id
WHERE e.id = $2::uuid
  AND e.request_id IS NOT NULL
  AND e.ingested_at <= $3
  AND e.created_at <= $3
  AND e.execution_id = $1`
	row := s.db.QueryRowContext(ctx, statement, executionID, eventID, asOf.UTC())
	detail, err := scanEvidenceEventDetail(row, executionID)
	if errors.Is(err, sql.ErrNoRows) {
		if exists, existsErr := s.executionExists(ctx, executionID, asOf); existsErr != nil {
			return investigation.EvidenceEventDetail{}, existsErr
		} else if !exists {
			return investigation.EvidenceEventDetail{}, ErrExecutionNotFound
		}
		return investigation.EvidenceEventDetail{}, ErrEventNotFound
	}
	return detail, err
}

func scanEvidenceEventDetail(row rowScanner, executionID string) (investigation.EvidenceEventDetail, error) {
	var detail investigation.EvidenceEventDetail
	var occurredAt, observedAt sql.NullTime
	var correlation, identity, capability, decision, usage, outcome, payloadPolicy, fieldProvenance string
	var sourceVersion, sourceEventID, sourceEventType, sourceEventHash sql.NullString
	err := row.Scan(
		&detail.EventID,
		&detail.EventType,
		&detail.Stage,
		&detail.Severity,
		&detail.CaptureMode,
		&occurredAt,
		&observedAt,
		&detail.IngestedAt,
		&detail.PayloadCaptureMode,
		&correlation,
		&identity,
		&capability,
		&decision,
		&usage,
		&outcome,
		&payloadPolicy,
		&fieldProvenance,
		&detail.Source.SourceSystemID,
		&detail.Source.AdapterManifestID,
		&detail.Source.SchemaVersion,
		&detail.Source.SourceType,
		&sourceVersion,
		&sourceEventID,
		&sourceEventType,
		&detail.Source.SourceRef,
		&detail.Source.EventHash,
		&sourceEventHash,
		&detail.Source.FidelityLevel,
	)
	if err != nil {
		return investigation.EvidenceEventDetail{}, err
	}
	detail.ExecutionID = executionID
	detail.OccurredAt = nullTimeValue(occurredAt)
	detail.ObservedAt = nullTimeValue(observedAt)
	detail.IngestedAt = detail.IngestedAt.UTC()
	detail.Source.SourceVersion = nullStringValue(sourceVersion)
	detail.Source.SourceEventID = nullStringValue(sourceEventID)
	detail.Source.SourceEventType = nullStringValue(sourceEventType)
	detail.Source.SourceEventHash = nullStringValue(sourceEventHash)
	for _, decode := range []struct {
		raw    string
		target any
	}{
		{correlation, &detail.Correlation},
		{identity, &detail.Identity},
		{capability, &detail.Capability},
		{decision, &detail.Decision},
		{outcome, &detail.Outcome},
		{payloadPolicy, &detail.PayloadPolicy},
		{fieldProvenance, &detail.FieldProvenance},
	} {
		if err := decodeJSONStruct(decode.raw, decode.target); err != nil {
			return investigation.EvidenceEventDetail{}, err
		}
	}
	if err := decodeUsageGroup(usage, &detail.Usage); err != nil {
		return investigation.EvidenceEventDetail{}, err
	}
	return detail, nil
}

func (s *EvidenceStore) ListExecutionGaps(ctx context.Context, query investigation.GapQuery) ([]investigation.CaptureGap, int, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("evidence store is not initialized")
	}
	if query.Limit < 1 {
		return nil, 0, fmt.Errorf("gap query limit must be positive")
	}
	sourceSystemID, requestID, err := s.resolveExecutionScope(ctx, query.ExecutionID, query.AsOf)
	if err != nil {
		return nil, 0, err
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `
SELECT count(*)
FROM capture_gaps
WHERE source_system_id = $1::uuid
  AND request_id = $2
  AND created_at <= $3`, sourceSystemID, requestID, query.AsOf.UTC()).Scan(&total); err != nil {
		return nil, 0, err
	}
	afterSet := query.After != nil
	afterCreatedAt := query.AsOf
	afterGapID := "00000000-0000-0000-0000-000000000000"
	if query.After != nil {
		afterCreatedAt = query.After.CreatedAt
		afterGapID = query.After.GapID
	}
	statement := `
SELECT
  g.id::text, g.evidence_event_id::text, g.gap_type,
  array_to_json(g.candidate_gap_types)::text, g.affected_claim, g.gap_reason,
  g.severity, g.recommendation, g.created_at,
  COALESCE(resolution.state, 'open'), resolution.note, resolution.updated_at
FROM capture_gaps g
LEFT JOIN capture_gap_resolutions resolution ON resolution.gap_id = g.id
WHERE g.source_system_id = $1::uuid
  AND g.request_id = $2
  AND g.created_at <= $3
  AND (NOT $4 OR (g.created_at, g.id) > ($5, $6::uuid))
ORDER BY g.created_at, g.id
LIMIT $7`
	rows, err := s.db.QueryContext(ctx, statement,
		sourceSystemID,
		requestID,
		query.AsOf.UTC(),
		afterSet,
		afterCreatedAt.UTC(),
		afterGapID,
		query.Limit,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	gaps := make([]investigation.CaptureGap, 0, query.Limit)
	for rows.Next() {
		var gap investigation.CaptureGap
		var eventID, recommendation, resolutionNote sql.NullString
		var resolutionUpdatedAt sql.NullTime
		var candidates string
		if err := rows.Scan(
			&gap.GapID,
			&eventID,
			&gap.GapType,
			&candidates,
			&gap.AffectedClaim,
			&gap.GapReason,
			&gap.Severity,
			&recommendation,
			&gap.CreatedAt,
			&gap.ResolutionState,
			&resolutionNote,
			&resolutionUpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		gap.EvidenceEventID = nullStringValue(eventID)
		gap.Recommendation = nullStringValue(recommendation)
		gap.ResolutionNote = nullStringValue(resolutionNote)
		gap.ResolutionUpdatedAt = nullTimePointer(resolutionUpdatedAt)
		gap.CandidateGapTypes = decodeJSONStringSlice(candidates)
		gap.CreatedAt = gap.CreatedAt.UTC()
		gaps = append(gaps, gap)
	}
	return gaps, total, rows.Err()
}

func (s *EvidenceStore) resolveExecutionScope(ctx context.Context, executionID string, asOf time.Time) (string, string, error) {
	var sourceSystemID, requestID string
	err := s.db.QueryRowContext(ctx, `
SELECT source_system_id::text, request_id
FROM evidence_events
WHERE execution_id = $1
  AND request_id IS NOT NULL
  AND ingested_at <= $2
  AND created_at <= $2
ORDER BY ingested_at, id
LIMIT 1`, executionID, asOf.UTC()).Scan(&sourceSystemID, &requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrExecutionNotFound
	}
	if err != nil {
		return "", "", err
	}
	return sourceSystemID, requestID, nil
}

func (s *EvidenceStore) ListSources(ctx context.Context, limit int) ([]investigation.SourceOption, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("evidence store is not initialized")
	}
	if limit < 1 {
		return nil, fmt.Errorf("source query limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, source_key, source_type, display_name, source_version
FROM source_systems
WHERE status = 'active'
ORDER BY display_name, id
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := make([]investigation.SourceOption, 0, limit)
	for rows.Next() {
		var source investigation.SourceOption
		var sourceVersion sql.NullString
		if err := rows.Scan(&source.SourceSystemID, &source.SourceKey, &source.SourceType, &source.DisplayName, &sourceVersion); err != nil {
			return nil, err
		}
		source.SourceVersion = nullStringValue(sourceVersion)
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (s *EvidenceStore) executionExists(ctx context.Context, executionID string, asOf time.Time) (bool, error) {
	statement := `
SELECT EXISTS (
  SELECT 1 FROM evidence_events e
  WHERE e.request_id IS NOT NULL
    AND e.ingested_at <= $2
    AND e.created_at <= $2
    AND e.execution_id = $1
)`
	var exists bool
	if err := s.db.QueryRowContext(ctx, statement, executionID, asOf.UTC()).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func decodeJSONStruct(raw string, target any) error {
	if raw == "" || raw == "{}" || raw == "null" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("decode persisted JSON projection: %w", err)
	}
	return nil
}

func decodeUsageGroup(raw string, target *investigation.UsageGroup) error {
	var persisted struct {
		PromptTokens     *int64 `json:"prompt_tokens"`
		CompletionTokens *int64 `json:"completion_tokens"`
		TotalTokens      *int64 `json:"total_tokens"`
		CostCents        *int64 `json:"cost_cents"`
		CostMicros       string `json:"cost_micros"`
		Currency         string `json:"currency"`
		AmountScale      *int64 `json:"amount_scale"`
		MoneySchema      string `json:"money_schema"`
		LatencyMS        *int64 `json:"latency_ms"`
		TTFTMS           *int64 `json:"ttft_ms"`
		UsageSource      string `json:"usage_source"`
		UsageConfidence  string `json:"usage_confidence"`
		Extensions       struct {
			EstimatedTokens *int64 `json:"estimated_tokens"`
		} `json:"extensions"`
	}
	if err := decodeJSONStruct(raw, &persisted); err != nil {
		return err
	}
	*target = investigation.UsageGroup{
		PromptTokens:     persisted.PromptTokens,
		CompletionTokens: persisted.CompletionTokens,
		TotalTokens:      persisted.TotalTokens,
		EstimatedTokens:  persisted.Extensions.EstimatedTokens,
		CostCents:        persisted.CostCents,
		CostMicros:       persisted.CostMicros,
		Currency:         persisted.Currency,
		AmountScale:      persisted.AmountScale,
		MoneySchema:      persisted.MoneySchema,
		LatencyMS:        persisted.LatencyMS,
		TTFTMS:           persisted.TTFTMS,
		UsageSource:      persisted.UsageSource,
		UsageConfidence:  persisted.UsageConfidence,
	}
	return nil
}

func timePointer(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}
