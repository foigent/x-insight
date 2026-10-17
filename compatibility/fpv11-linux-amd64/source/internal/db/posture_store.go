package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/foigent/x-insight/internal/investigation"
)

const postureExecutionCTE = `
WITH candidate_executions AS (
  SELECT DISTINCT e.source_system_id, e.request_id
  FROM evidence_events e
  WHERE e.request_id IS NOT NULL
    AND e.request_id <> ''
    AND e.execution_id IS NOT NULL
    AND e.ingested_at <= $1
    AND e.created_at <= $1
    AND COALESCE(e.occurred_at, e.observed_at, e.ingested_at) >= $2
    AND COALESCE(e.occurred_at, e.observed_at, e.ingested_at) <= $3
    AND ($4 = '' OR e.source_system_id = NULLIF($4, '')::uuid)
), event_rollup AS (
  SELECT
    e.source_system_id,
    e.request_id,
    min(e.execution_id) AS execution_id,
    min(e.source_type) AS source_type,
    min(COALESCE(e.occurred_at, e.observed_at, e.ingested_at)) AS first_effective_at,
    max(COALESCE(e.occurred_at, e.observed_at, e.ingested_at)) AS last_effective_at,
    bool_and(e.occurred_at IS NOT NULL OR e.observed_at IS NOT NULL) AS direct_time_complete,
    bool_or(e.identity <> '{}'::jsonb) AS identity_observed,
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
    END AS execution_outcome
  FROM evidence_events e
  JOIN candidate_executions candidate
    ON candidate.source_system_id = e.source_system_id
   AND candidate.request_id = e.request_id
  WHERE e.request_id IS NOT NULL
    AND e.ingested_at <= $1
    AND e.created_at <= $1
  GROUP BY e.source_system_id, e.request_id
), gap_rollup AS (
  SELECT g.source_system_id, g.request_id, count(*)::bigint AS gap_count
  FROM capture_gaps g
  JOIN candidate_executions candidate
    ON candidate.source_system_id = g.source_system_id
   AND candidate.request_id = g.request_id
  WHERE g.request_id IS NOT NULL
    AND g.created_at <= $1
  GROUP BY g.source_system_id, g.request_id
), executions AS (
  SELECT
    er.source_system_id,
    er.request_id,
    er.execution_id,
    er.source_type,
    source.display_name AS source_display_name,
    er.first_effective_at,
    er.last_effective_at,
    er.direct_time_complete,
    er.identity_observed,
    er.execution_outcome,
    COALESCE(gap.gap_count, 0)::bigint AS gap_count
  FROM event_rollup er
  JOIN source_systems source ON source.id = er.source_system_id
  LEFT JOIN gap_rollup gap
    ON gap.source_system_id = er.source_system_id
   AND gap.request_id = er.request_id
  WHERE er.last_effective_at >= $2
    AND er.last_effective_at <= $3
)
`

type postureQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *EvidenceStore) GetPosture(
	ctx context.Context,
	query investigation.PostureQuery,
) (investigation.PostureSnapshot, error) {
	snapshot := investigation.PostureSnapshot{
		SchemaVersion:  investigation.PostureSchema,
		AsOf:           query.AsOf.UTC(),
		From:           query.From.UTC(),
		To:             query.To.UTC(),
		SourceSystemID: query.SourceSystemID,
		Coverage:       []investigation.PostureCoverage{},
		Sources:        []investigation.PostureSourceSummary{},
		Trend: investigation.PostureTrend{
			Items:    []investigation.PostureTrendBucket{},
			Boundary: "execution buckets are fixed by as_of; Alert buckets use current non-resolved state for records observed in the window and do not reconstruct historical state; absence is zero, not proof of health",
		},
		Boundaries: []string{
			"aggregates_use_fixed_as_of_and_effective_time_window",
			"open_alerts_are_product_analysis_records_not_m0_events",
			"alert_observed_time_is_bounded_but_current_state_is_not_historical_state_reconstruction",
			"connector_delivery_state_is_operational_and_not_included_in_event_aggregates",
			"coverage_ratios_measure_observed_fields_or_declared_links_not_ground_truth_completeness",
		},
	}
	if s == nil || s.db == nil {
		return snapshot, fmt.Errorf("evidence store is not initialized")
	}
	if query.AsOf.IsZero() || query.From.IsZero() || query.To.IsZero() || !query.From.Before(query.To) {
		return snapshot, fmt.Errorf("posture query requires a valid fixed window")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return snapshot, err
	}
	defer func() { _ = tx.Rollback() }()

	args := []any{query.AsOf.UTC(), query.From.UTC(), query.To.UTC(), query.SourceSystemID}
	executionSummary, temporalComplete, identityObserved, err := queryPostureExecutionSummary(ctx, tx, args)
	if err != nil {
		return snapshot, err
	}
	snapshot.Executions = executionSummary

	risk, err := queryPostureRisk(ctx, tx, query)
	if err != nil {
		return snapshot, err
	}
	snapshot.Risk = risk

	declaredParents, resolvedParents, unresolvedParents, ambiguousParents, err := queryPostureCausalCoverage(ctx, tx, args)
	if err != nil {
		return snapshot, err
	}
	snapshot.Coverage = []investigation.PostureCoverage{
		postureCoverage(
			"temporal",
			"Direct time",
			temporalComplete,
			executionSummary.Total,
			"executions",
			"every visible Event in the execution has occurred_at or observed_at",
			"ingestion time is intentionally excluded from direct temporal coverage",
		),
		postureCoverage(
			"identity",
			"Identity",
			identityObserved,
			executionSummary.Total,
			"executions",
			"at least one visible Event in the execution carries a non-empty identity group",
			"presence does not prove end-to-end identity completeness",
		),
		postureCoverage(
			"outcome",
			"Outcome",
			executionSummary.Total-executionSummary.Unknown,
			executionSummary.Total,
			"executions",
			"the deterministic execution rollup resolves to success, failure or partial",
			"known outcome does not prove that every downstream terminal Event was captured",
		),
		postureCausalCoverage(declaredParents, resolvedParents, unresolvedParents, ambiguousParents),
	}

	sources, err := queryPostureSources(ctx, tx, args)
	if err != nil {
		return snapshot, err
	}
	snapshot.Sources = sources

	trend, err := queryPostureTrend(ctx, tx, query, args)
	if err != nil {
		return snapshot, err
	}
	snapshot.Trend = trend

	if err := tx.Commit(); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func queryPostureExecutionSummary(
	ctx context.Context,
	queryer postureQueryer,
	args []any,
) (investigation.PostureExecutionSummary, int64, int64, error) {
	var summary investigation.PostureExecutionSummary
	var temporalComplete, identityObserved int64
	err := queryer.QueryRowContext(ctx, postureExecutionCTE+`
SELECT
  count(*)::bigint,
  count(*) FILTER (WHERE execution_outcome = 'success')::bigint,
  count(*) FILTER (WHERE execution_outcome = 'failure')::bigint,
  count(*) FILTER (WHERE execution_outcome = 'partial')::bigint,
  count(*) FILTER (WHERE execution_outcome = 'unknown')::bigint,
  count(*) FILTER (WHERE gap_count > 0)::bigint,
  COALESCE(sum(gap_count), 0)::bigint,
  count(*) FILTER (WHERE direct_time_complete)::bigint,
  count(*) FILTER (WHERE identity_observed)::bigint
FROM executions`, args...).Scan(
		&summary.Total,
		&summary.Success,
		&summary.Failure,
		&summary.Partial,
		&summary.Unknown,
		&summary.WithGaps,
		&summary.TotalGaps,
		&temporalComplete,
		&identityObserved,
	)
	return summary, temporalComplete, identityObserved, err
}

func queryPostureRisk(
	ctx context.Context,
	queryer postureQueryer,
	query investigation.PostureQuery,
) (investigation.PostureRiskSummary, error) {
	args := []any{query.AsOf.UTC(), query.From.UTC(), query.To.UTC(), query.SourceSystemID}
	var risk investigation.PostureRiskSummary
	err := queryer.QueryRowContext(ctx, `
SELECT
  count(*)::bigint,
  count(*) FILTER (WHERE severity = 'critical')::bigint,
  count(*) FILTER (WHERE severity = 'error')::bigint,
  count(*) FILTER (WHERE severity = 'warning')::bigint,
  count(*) FILTER (WHERE severity = 'info')::bigint,
  count(DISTINCT NULLIF(execution_id, ''))::bigint,
  count(*) FILTER (WHERE execution_id IS NULL OR execution_id = '')::bigint
FROM investigation_alerts
WHERE state <> 'resolved'
  AND observed_at <= $1
  AND observed_at >= $2
  AND observed_at <= $3
  AND ($4 = '' OR source_system_id = NULLIF($4, '')::uuid)`, args...).Scan(
		&risk.OpenTotal,
		&risk.Critical,
		&risk.Error,
		&risk.Warning,
		&risk.Info,
		&risk.AffectedExecutions,
		&risk.Unlinked,
	)
	if err != nil {
		return risk, err
	}

	rows, err := queryer.QueryContext(ctx, `
SELECT alert.id, alert.source_system_id::text, alert.source_alert_ref,
       alert.title, alert.severity, alert.state, alert.execution_id,
       link.case_id, alert.observed_at
FROM investigation_alerts alert
LEFT JOIN investigation_alert_case_links link ON link.alert_id = alert.id
WHERE alert.state <> 'resolved'
  AND alert.observed_at <= $1
  AND alert.observed_at >= $2
  AND alert.observed_at <= $3
  AND ($4 = '' OR alert.source_system_id = NULLIF($4, '')::uuid)
ORDER BY
  CASE alert.severity WHEN 'critical' THEN 4 WHEN 'error' THEN 3 WHEN 'warning' THEN 2 ELSE 1 END DESC,
  alert.observed_at DESC, alert.id
LIMIT 20`, args...)
	if err != nil {
		return risk, err
	}
	defer rows.Close()
	risk.Items = make([]investigation.InvestigationAlert, 0)
	for rows.Next() {
		var item investigation.InvestigationAlert
		var executionID, caseID sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.SourceSystemID,
			&item.SourceAlertRef,
			&item.Title,
			&item.Severity,
			&item.State,
			&executionID,
			&caseID,
			&item.ObservedAt,
		); err != nil {
			return risk, err
		}
		item.ExecutionID = executionID.String
		item.CaseID = caseID.String
		item.ObservedAt = item.ObservedAt.UTC()
		risk.Items = append(risk.Items, item)
	}
	return risk, rows.Err()
}

func queryPostureCausalCoverage(
	ctx context.Context,
	queryer postureQueryer,
	args []any,
) (int64, int64, int64, int64, error) {
	var declared, resolved, unresolved, ambiguous int64
	err := queryer.QueryRowContext(ctx, postureExecutionCTE+`, child_links AS (
  SELECT child.id, count(parent.id)::bigint AS parent_count
  FROM executions execution
  JOIN evidence_events child
    ON child.source_system_id = execution.source_system_id
   AND child.request_id = execution.request_id
  LEFT JOIN evidence_events parent
    ON parent.source_system_id = child.source_system_id
   AND parent.trace_id = child.trace_id
   AND parent.span_id = child.correlation->>'parent_span_id'
   AND COALESCE(parent.org_ref, '') = COALESCE(child.org_ref, '')
   AND COALESCE(parent.project_ref, '') = COALESCE(child.project_ref, '')
   AND parent.ingested_at <= $1
   AND parent.created_at <= $1
   AND parent.stage <> 'gap'
   AND parent.event_type <> 'gap.capture.detected'
  WHERE child.ingested_at <= $1
    AND child.created_at <= $1
    AND child.trace_id IS NOT NULL
    AND child.trace_id <> ''
    AND child.span_id IS NOT NULL
    AND child.span_id <> ''
    AND COALESCE(child.correlation->>'parent_span_id', '') <> ''
    AND child.stage <> 'gap'
    AND child.event_type <> 'gap.capture.detected'
  GROUP BY child.id
)
SELECT
  count(*)::bigint,
  count(*) FILTER (WHERE parent_count = 1)::bigint,
  count(*) FILTER (WHERE parent_count = 0)::bigint,
  count(*) FILTER (WHERE parent_count > 1)::bigint
FROM child_links`, args...).Scan(&declared, &resolved, &unresolved, &ambiguous)
	return declared, resolved, unresolved, ambiguous, err
}

func queryPostureSources(
	ctx context.Context,
	queryer postureQueryer,
	args []any,
) ([]investigation.PostureSourceSummary, error) {
	rows, err := queryer.QueryContext(ctx, postureExecutionCTE+`
SELECT source_system_id::text, source_type, source_display_name,
       count(*)::bigint,
       count(*) FILTER (WHERE execution_outcome = 'failure')::bigint,
       count(*) FILTER (WHERE gap_count > 0)::bigint,
       max(last_effective_at)
FROM executions
GROUP BY source_system_id, source_type, source_display_name
ORDER BY source_display_name, source_system_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]investigation.PostureSourceSummary, 0)
	for rows.Next() {
		var item investigation.PostureSourceSummary
		if err := rows.Scan(
			&item.SourceSystemID,
			&item.SourceType,
			&item.SourceDisplayName,
			&item.ExecutionTotal,
			&item.FailureTotal,
			&item.ExecutionsWithGaps,
			&item.LastEffectiveAt,
		); err != nil {
			return nil, err
		}
		item.LastEffectiveAt = item.LastEffectiveAt.UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}

func queryPostureTrend(
	ctx context.Context,
	queryer postureQueryer,
	query investigation.PostureQuery,
	args []any,
) (investigation.PostureTrend, error) {
	bucketUnit := "hour"
	if query.To.Sub(query.From) > 48*time.Hour {
		bucketUnit = "day"
	}
	trend := investigation.PostureTrend{
		BucketUnit: bucketUnit,
		Items:      []investigation.PostureTrendBucket{},
		Boundary:   "execution buckets are fixed by as_of; Alert buckets use current non-resolved state for records observed in the window and do not reconstruct historical state; absence is zero, not proof of health",
	}
	bucketExpression := fmt.Sprintf("date_trunc('%s', last_effective_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'", bucketUnit)
	rows, err := queryer.QueryContext(ctx, postureExecutionCTE+fmt.Sprintf(`
SELECT %s AS bucket_start,
       count(*)::bigint,
       count(*) FILTER (WHERE execution_outcome = 'failure')::bigint,
       count(*) FILTER (WHERE execution_outcome = 'partial')::bigint,
       count(*) FILTER (WHERE gap_count > 0)::bigint
FROM executions
GROUP BY bucket_start
ORDER BY bucket_start`, bucketExpression), args...)
	if err != nil {
		return trend, err
	}
	buckets := make(map[time.Time]investigation.PostureTrendBucket)
	for rows.Next() {
		var item investigation.PostureTrendBucket
		if err := rows.Scan(
			&item.BucketStart,
			&item.ExecutionTotal,
			&item.FailureTotal,
			&item.PartialTotal,
			&item.ExecutionsWithGaps,
		); err != nil {
			rows.Close()
			return trend, err
		}
		item.BucketStart = item.BucketStart.UTC()
		buckets[item.BucketStart] = item
	}
	if err := rows.Close(); err != nil {
		return trend, err
	}

	alertBucketExpression := fmt.Sprintf("date_trunc('%s', observed_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'", bucketUnit)
	alertRows, err := queryer.QueryContext(ctx, fmt.Sprintf(`
SELECT %s AS bucket_start, count(*)::bigint
FROM investigation_alerts
WHERE state <> 'resolved'
  AND observed_at <= $1
  AND observed_at >= $2
  AND observed_at <= $3
  AND ($4 = '' OR source_system_id = NULLIF($4, '')::uuid)
GROUP BY bucket_start
ORDER BY bucket_start`, alertBucketExpression), args...)
	if err != nil {
		return trend, err
	}
	defer alertRows.Close()
	for alertRows.Next() {
		var bucketStart time.Time
		var openAlerts int64
		if err := alertRows.Scan(&bucketStart, &openAlerts); err != nil {
			return trend, err
		}
		bucketStart = bucketStart.UTC()
		item := buckets[bucketStart]
		item.BucketStart = bucketStart
		item.OpenAlerts = openAlerts
		buckets[bucketStart] = item
	}
	if err := alertRows.Err(); err != nil {
		return trend, err
	}

	starts := make([]time.Time, 0, len(buckets))
	for start := range buckets {
		starts = append(starts, start)
	}
	sort.Slice(starts, func(left, right int) bool { return starts[left].Before(starts[right]) })
	for _, start := range starts {
		trend.Items = append(trend.Items, buckets[start])
	}
	return trend, nil
}

func postureCoverage(
	dimension string,
	label string,
	numerator int64,
	denominator int64,
	unit string,
	basis string,
	limitation string,
) investigation.PostureCoverage {
	coverage := investigation.PostureCoverage{
		Dimension:   dimension,
		Label:       label,
		Numerator:   numerator,
		Denominator: denominator,
		Unit:        unit,
		Basis:       basis,
		Limitation:  limitation,
	}
	if denominator > 0 {
		coverage.Available = true
		coverage.RatioDecimal = strconv.FormatFloat(float64(numerator)/float64(denominator), 'f', 4, 64)
	}
	return coverage
}

func postureCausalCoverage(
	declared int64,
	resolved int64,
	unresolved int64,
	ambiguous int64,
) investigation.PostureCoverage {
	coverage := postureCoverage(
		"causal",
		"Causal links",
		resolved,
		declared,
		"declared_parent_links",
		"source-declared parent_span_id links resolved within the same source, trace, organization and project",
		"chronology and cross-source time proximity never create a causal edge",
	)
	coverage.UnresolvedCount = unresolved
	coverage.AmbiguousCount = ambiguous
	return coverage
}
