package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/investigation"
)

func (s *EvidenceStore) ListEvaluationFeedback(
	ctx context.Context,
	executionID string,
	asOf time.Time,
	limit int,
) ([]investigation.EvaluationFeedback, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("evidence store is not initialized")
	}
	if limit < 1 || limit > 200 {
		return nil, fmt.Errorf("feedback limit must be between 1 and 200")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, execution_id, source_system_id::text, evaluator_ref, metric_name,
       metric_value_decimal, label, explanation, observed_at,
       array_to_json(evidence_event_ids)::text
FROM evaluation_feedback
WHERE execution_id = $1 AND observed_at <= $2
ORDER BY observed_at, id
LIMIT $3`, executionID, asOf.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]investigation.EvaluationFeedback, 0)
	for rows.Next() {
		var item investigation.EvaluationFeedback
		var metricValue, label sql.NullString
		var evidenceIDs string
		if err := rows.Scan(
			&item.ID, &item.ExecutionID, &item.SourceSystemID, &item.EvaluatorRef,
			&item.MetricName, &metricValue, &label, &item.Explanation,
			&item.ObservedAt, &evidenceIDs,
		); err != nil {
			return nil, err
		}
		item.MetricValueDecimal = metricValue.String
		item.Label = label.String
		item.ObservedAt = item.ObservedAt.UTC()
		item.EvidenceEventIDs = decodeNonNilJSONStringSlice(evidenceIDs)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *EvidenceStore) ListExecutionVersionDimensions(
	ctx context.Context,
	executionID string,
	asOf time.Time,
	limit int,
) ([]investigation.ExecutionVersionDimension, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("evidence store is not initialized")
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("version dimension limit must be between 1 and 100")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT execution_id, dimension, version_ref, source_system_id::text,
       evidence_event_id::text, observed_at
FROM execution_version_dimensions
WHERE execution_id = $1 AND observed_at <= $2
ORDER BY dimension, observed_at, version_ref
LIMIT $3`, executionID, asOf.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]investigation.ExecutionVersionDimension, 0)
	for rows.Next() {
		var item investigation.ExecutionVersionDimension
		var eventID sql.NullString
		if err := rows.Scan(
			&item.ExecutionID, &item.Dimension, &item.VersionRef, &item.SourceSystemID,
			&eventID, &item.ObservedAt,
		); err != nil {
			return nil, err
		}
		item.EvidenceEventID = eventID.String
		item.ObservedAt = item.ObservedAt.UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *EvidenceStore) ListInvestigationAlerts(
	ctx context.Context,
	asOf time.Time,
	limit int,
) ([]investigation.InvestigationAlert, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("evidence store is not initialized")
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("alert limit must be between 1 and 100")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT a.id, a.source_system_id::text, a.source_alert_ref, a.title, a.severity, a.state,
       a.execution_id, l.case_id, a.observed_at
FROM investigation_alerts a
LEFT JOIN investigation_alert_case_links l ON l.alert_id = a.id
WHERE a.observed_at <= $1
ORDER BY
  CASE a.severity WHEN 'critical' THEN 4 WHEN 'error' THEN 3 WHEN 'warning' THEN 2 ELSE 1 END DESC,
  a.observed_at DESC, a.id
LIMIT $2`, asOf.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]investigation.InvestigationAlert, 0)
	for rows.Next() {
		var item investigation.InvestigationAlert
		var executionID, caseID sql.NullString
		if err := rows.Scan(
			&item.ID, &item.SourceSystemID, &item.SourceAlertRef, &item.Title,
			&item.Severity, &item.State, &executionID, &caseID, &item.ObservedAt,
		); err != nil {
			return nil, err
		}
		item.ExecutionID = executionID.String
		item.CaseID = caseID.String
		item.ObservedAt = item.ObservedAt.UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *EvidenceStore) ListDiscoveryHypotheses(
	ctx context.Context,
	asOf time.Time,
	limit int,
) ([]investigation.DiscoveryHypothesis, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("evidence store is not initialized")
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("hypothesis limit must be between 1 and 100")
	}
	rows, err := s.db.QueryContext(ctx, `
WITH selected_run AS (
  SELECT id
  FROM discovery_analysis_runs
  WHERE analysis_as_of <= $1
  ORDER BY analysis_as_of DESC, generated_at DESC, id DESC
  LIMIT 1
)
SELECT h.id, h.analysis_run_id, run.analysis_as_of, run.sample_size,
       run.input_digest, h.hypothesis_type, h.title, h.statement,
	       h.support_score_decimal, h.status,
       array_to_json(h.evidence_event_ids)::text,
       array_to_json(h.execution_ids)::text,
       array_to_json(h.limitations)::text,
       h.abstention_reason, h.method_version, h.generated_at
FROM discovery_hypotheses h
JOIN discovery_analysis_runs run ON run.id = h.analysis_run_id
JOIN selected_run ON selected_run.id = run.id
ORDER BY h.hypothesis_type, h.id
LIMIT $2`, asOf.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]investigation.DiscoveryHypothesis, 0)
	for rows.Next() {
		var item investigation.DiscoveryHypothesis
		var supportScore, abstention sql.NullString
		var evidenceIDs, executionIDs, limitations string
		if err := rows.Scan(
			&item.ID, &item.AnalysisRunID, &item.AnalysisAsOf, &item.SampleSize,
			&item.InputDigest, &item.HypothesisType, &item.Title, &item.Statement,
			&supportScore, &item.Status, &evidenceIDs, &executionIDs,
			&limitations, &abstention, &item.MethodVersion, &item.GeneratedAt,
		); err != nil {
			return nil, err
		}
		item.SupportScoreDecimal = supportScore.String
		item.EvidenceEventIDs = decodeNonNilJSONStringSlice(evidenceIDs)
		item.ExecutionIDs = decodeNonNilJSONStringSlice(executionIDs)
		item.Limitations = decodeNonNilJSONStringSlice(limitations)
		item.AbstentionReason = abstention.String
		item.AnalysisAsOf = item.AnalysisAsOf.UTC()
		item.GeneratedAt = item.GeneratedAt.UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}
