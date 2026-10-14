package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const workbenchInterpretationA0 = "source_project=direct:project_ref;cost_project=unavailable;account=unknown;application=unknown;purpose=unknown"

func (s *WorkspaceStore) CreateWorkbenchReading(ctx context.Context, subject string, request workbench.ReadingRequest, now time.Time) (workbench.Reading, error) {
	return s.createWorkbenchReading(ctx, subject, request, now, nil)
}

type workbenchReadingPhaseObserver func(string, time.Duration)

func (s *WorkspaceStore) createWorkbenchReading(
	ctx context.Context,
	subject string,
	request workbench.ReadingRequest,
	now time.Time,
	observer workbenchReadingPhaseObserver,
) (workbench.Reading, error) {
	return s.createWorkbenchReadingWithPlan(ctx, subject, request, now, observer, workbenchReadingUseGenericPlan)
}

// Keep controlled comparisons on the same creation path as the production selector.
func (s *WorkspaceStore) createWorkbenchReadingWithPlan(
	ctx context.Context,
	subject string,
	request workbench.ReadingRequest,
	now time.Time,
	observer workbenchReadingPhaseObserver,
	genericPlan func(int64) bool,
) (workbench.Reading, error) {
	if s == nil || s.db == nil {
		return workbench.Reading{}, fmt.Errorf("workspace store is not initialized")
	}
	period, err := request.Resolve(now)
	if err != nil {
		return workbench.Reading{}, err
	}
	// Match PostgreSQL timestamp precision in both the frozen values and the
	// initial POST response, as runtime reading creation already does.
	now = now.UTC().Truncate(time.Microsecond)
	period.StartInclusive = period.StartInclusive.UTC().Truncate(time.Microsecond)
	period.EndExclusive = period.EndExclusive.UTC().Truncate(time.Microsecond)
	if !period.EndExclusive.After(period.StartInclusive) {
		return workbench.Reading{}, workbench.ErrInvalid
	}
	phaseStarted := time.Now()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return workbench.Reading{}, err
	}
	observeWorkbenchReadingPhase(observer, "begin_transaction", phaseStarted)
	defer tx.Rollback()
	phaseStarted = time.Now()
	authorization, err := loadWorkbenchAuthorization(ctx, tx, subject)
	if err != nil {
		return workbench.Reading{}, err
	}
	if !authorizationAllows(authorization, request.Filters, func(grant workbench.Grant) bool { return grant.AllowSummary }) {
		return workbench.Reading{}, workbench.ErrScopeForbidden
	}
	readingID, err := workbench.NewID("xwr_")
	if err != nil {
		return workbench.Reading{}, err
	}
	filtersJSON, _ := json.Marshal(request.Filters)
	coverage := workbench.Coverage{
		Status:                       "period_completeness_unknown",
		PeriodCompleteness:           "unknown",
		ProjectionCompleteForMembers: true,
		KnownItems:                   0,
		Gaps:                         []string{"source_period_completeness_not_asserted"},
	}
	coverageJSON, _ := json.Marshal(coverage)
	interpretationRevision := digestText(workbenchInterpretationA0)
	if len(sourceProfiles(authorization, request.Filters)) > 0 {
		interpretationRevision = digestText(workbenchInterpretationA1)
	}
	placeholder := digestText("")
	if _, err := tx.ExecContext(ctx, `
INSERT INTO workbench_readings (
  id, owner_subject, contract_version, metric_version, period_start, period_end,
  display_timezone, cutoff, expires_at, money_basis, currency, filters,
  authorization_revision, interpretation_revision, membership_sha256, coverage, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13,$14,$15,$16::jsonb,$8)`,
		readingID, subject, workbench.ContractVersion, workbench.MetricVersion,
		period.StartInclusive, period.EndExclusive, period.Timezone, now, now.Add(workbench.ReadingTTL),
		request.MoneyBasis, request.Currency, string(filtersJSON), authorization.Revision,
		interpretationRevision, placeholder, string(coverageJSON)); err != nil {
		return workbench.Reading{}, err
	}
	observeWorkbenchReadingPhase(observer, "authorization_and_header", phaseStarted)
	var eligible int64
	phaseStarted = time.Now()
	if err := tx.QueryRowContext(ctx, workbenchEligibleCountSQL,
		pgx.QueryExecModeExec, subject, period.StartInclusive, period.EndExclusive,
		pgTextArray(request.Filters.SourceIDs), pgTextArray(request.Filters.SourceProjects), pgTextArray(request.Filters.CostProjects),
	).Scan(&eligible); err != nil {
		return workbench.Reading{}, err
	}
	if eligible > workbench.MaxMembers {
		return workbench.Reading{}, workbench.ErrTooLarge
	}
	observeWorkbenchReadingPhase(observer, "eligible_count", phaseStarted)
	phaseStarted = time.Now()
	memberResult, err := tx.ExecContext(ctx, workbenchMemberInsertSQL,
		readingID, subject, period.StartInclusive, period.EndExclusive,
		pgTextArray(request.Filters.SourceIDs), pgTextArray(request.Filters.SourceProjects), pgTextArray(request.Filters.CostProjects),
	)
	if err != nil {
		return workbench.Reading{}, err
	}
	insertedMembers, err := memberResult.RowsAffected()
	if err != nil {
		return workbench.Reading{}, err
	}
	if insertedMembers != eligible {
		return workbench.Reading{}, fmt.Errorf("workbench reading membership changed during repeatable-read transaction")
	}
	observeWorkbenchReadingPhase(observer, "member_insert", phaseStarted)
	var restorePlan func() error
	if genericPlan != nil && genericPlan(insertedMembers) {
		phaseStarted = time.Now()
		restorePlan, err = useWorkbenchGenericPlan(ctx, tx)
		if err != nil {
			return workbench.Reading{}, err
		}
		observeWorkbenchReadingPhase(observer, "reading_plan_mode", phaseStarted)
	}
	var membership sql.NullString
	phaseStarted = time.Now()
	if err := tx.QueryRowContext(ctx, `
SELECT 'sha256:' || encode(pg_catalog.sha256(convert_to(COALESCE(string_agg(
  economic_fact_id::text || ':' || source_event_hash || ':' || event_hash,
  E'\n' ORDER BY ordinal), ''), 'UTF8')), 'hex')
FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE reading_id=$1`, readingID).Scan(&membership); err != nil {
		return workbench.Reading{}, err
	}
	observeWorkbenchReadingPhase(observer, "membership_digest", phaseStarted)
	phaseStarted = time.Now()
	coverage, err = loadWorkbenchCoverage(ctx, tx, subject, readingID, request.Filters, eligible, period.StartInclusive)
	if err != nil {
		return workbench.Reading{}, err
	}
	observeWorkbenchReadingPhase(observer, "coverage", phaseStarted)
	sourceManifest, sourceCoverage, err := freezeWorkbenchSources(ctx, tx, readingID, subject, authorization, request.Filters, period, now)
	if err != nil {
		return workbench.Reading{}, err
	}
	purposeReading, err := freezeWorkbenchPurposes(ctx, tx, readingID, subject, authorization)
	if err != nil {
		return workbench.Reading{}, err
	}
	if restorePlan != nil {
		phaseStarted = time.Now()
		if err := restorePlan(); err != nil {
			return workbench.Reading{}, err
		}
		observeWorkbenchReadingPhase(observer, "restore_reading_plan_mode", phaseStarted)
	}
	coverageJSON, _ = json.Marshal(coverage)
	phaseStarted = time.Now()
	if _, err := tx.ExecContext(ctx, `
UPDATE workbench_readings SET membership_sha256=$2, coverage=$3::jsonb WHERE id=$1`,
		readingID, membership.String, string(coverageJSON)); err != nil {
		return workbench.Reading{}, err
	}
	observeWorkbenchReadingPhase(observer, "finalize_header", phaseStarted)
	phaseStarted = time.Now()
	if err := commitWorkspaceTx(ctx, tx); err != nil {
		return workbench.Reading{}, err
	}
	observeWorkbenchReadingPhase(observer, "commit", phaseStarted)
	reading := workbench.Reading{
		ID: readingID, ContractVersion: workbench.ContractVersion, MetricVersion: workbench.MetricVersion,
		Period: period, Cutoff: now, ExpiresAt: now.Add(workbench.ReadingTTL), MoneyBasis: request.MoneyBasis,
		MoneyTitle: workbench.MoneyTitle(request.MoneyBasis), Currency: request.Currency,
		MembershipRef: membership.String, InterpretationRevision: interpretationRevision,
		AuthorizationRevision: authorization.Revision, Dimensions: []string{"model", "source_project", "cost_project"}, Coverage: coverage,
		SourceManifestRef: sourceManifest, SourceCoverage: sourceCoverage, Purpose: purposeReading,
	}
	if purposeReading != nil && purposeReading.Status == "available" {
		reading.Dimensions = append(reading.Dimensions, "purpose")
	}
	if sourceCoverage != nil {
		for _, p := range sourceCoverage.Profiles {
			if p == "unirouter.execution-context.v1" {
				reading.Dimensions = append(reading.Dimensions, "application")
			}
			if p == "unirouter.execution-identity.v1" {
				reading.Dimensions = append(reading.Dimensions, "account")
			}
		}
	}
	return reading, nil
}

// Calibration at 6M retained members found generic derived queries slower for
// 0/16 members and faster as a pair for 70k/300k. Use the smallest measured large
// size as a conservative boundary; this is not an estimated optimal crossover.
func workbenchReadingUseGenericPlan(members int64) bool {
	return members >= 70000
}

// The caller rolls the transaction back on any query or context error. SET LOCAL
// then restores the entry mode even when an aborted transaction cannot run SQL.
func useWorkbenchGenericPlan(ctx context.Context, tx *sql.Tx) (func() error, error) {
	var previous string
	if err := tx.QueryRowContext(ctx, "SELECT current_setting('plan_cache_mode')").Scan(&previous); err != nil {
		return nil, err
	}
	if previous == "force_generic_plan" {
		return func() error { return nil }, nil
	}
	if _, err := tx.ExecContext(ctx, "SELECT set_config('plan_cache_mode', 'force_generic_plan', true)"); err != nil {
		return nil, err
	}
	return func() error {
		_, err := tx.ExecContext(ctx, "SELECT set_config('plan_cache_mode', $1, true)", previous)
		return err
	}, nil
}

func observeWorkbenchReadingPhase(observer workbenchReadingPhaseObserver, name string, started time.Time) {
	if observer != nil {
		observer(name, time.Since(started))
	}
}

const workbenchEligibleCountSQL = `
SELECT count(*) FROM workbench_economic_facts f
WHERE f.occurred_at >= $2 AND f.occurred_at < $3 AND f.currency='CNY'
  AND (cardinality($4::text[])=0 OR f.source_system_id::text=ANY($4::text[]))
  AND (cardinality($5::text[])=0 OR f.source_project_ref=ANY($5::text[]))
  AND (cardinality($6::text[])=0 OR f.cost_project_ref=ANY($6::text[]))
  AND EXISTS (SELECT 1 FROM workbench_access_grants g
    WHERE g.subject_id=$1 AND g.active AND g.allow_summary
      AND g.source_system_id=f.source_system_id
      AND (g.source_project_ref IS NULL OR g.source_project_ref=f.source_project_ref)
      AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref))`

const workbenchMemberInsertSQL = `
INSERT INTO workbench_reading_members (reading_id, ordinal, economic_fact_id)
SELECT $1, row_number() OVER (ORDER BY f.occurred_at,f.id), f.id
FROM workbench_economic_facts f
WHERE f.occurred_at >= $3 AND f.occurred_at < $4 AND f.currency='CNY'
  AND (cardinality($5::text[])=0 OR f.source_system_id::text=ANY($5::text[]))
  AND (cardinality($6::text[])=0 OR f.source_project_ref=ANY($6::text[]))
  AND (cardinality($7::text[])=0 OR f.cost_project_ref=ANY($7::text[]))
  AND EXISTS (SELECT 1 FROM workbench_access_grants g
    WHERE g.subject_id=$2 AND g.active AND g.allow_summary
      AND g.source_system_id=f.source_system_id
      AND (g.source_project_ref IS NULL OR g.source_project_ref=f.source_project_ref)
      AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref))
-- Physical input order reduces random heap reads during fact FK checks. The
-- persisted ordinal above still uses exactly (occurred_at,id), as before.
ORDER BY f.ctid`

func (s *WorkspaceStore) GetWorkbenchReading(ctx context.Context, subject, id string, now time.Time) (workbench.Reading, error) {
	return s.loadValidWorkbenchReading(ctx, subject, id, now)
}

func (s *WorkspaceStore) GetWorkbenchSummary(ctx context.Context, subject, id string, now time.Time) (workbench.Summary, error) {
	reading, err := s.loadValidWorkbenchReading(ctx, subject, id, now)
	if err != nil {
		return workbench.Summary{}, err
	}
	var summary workbench.Summary
	err = s.db.QueryRowContext(ctx, `
SELECT count(*), COALESCE(sum(gateway_charged_micros),0)::text,
       COALESCE(sum(calculated_reference_micros),0)::text
FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE reading_id=$1`, id).Scan(
		&summary.RequestCount, &summary.GatewayChargedMicros, &summary.CalculatedReferenceMicros)
	if err != nil {
		return workbench.Summary{}, err
	}
	if reading.Purpose != nil && reading.Purpose.Status == "available" {
		if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM workbench_purpose_members WHERE reading_id=$1 AND status='adopted'`, id).Scan(&summary.PurposeKnownRequests); err != nil {
			return workbench.Summary{}, err
		}
	}
	summary.ReadingID = id
	summary.MoneyBasis = reading.MoneyBasis
	summary.Currency = reading.Currency
	summary.SelectedAmountMicros = workbench.SelectedMicros(reading.MoneyBasis, summary.GatewayChargedMicros, summary.CalculatedReferenceMicros)
	summary.RequestTotalComplete = reading.Coverage.Status == "complete_to_cutoff"
	summary.AmountComplete = reading.Coverage.Status == "complete_to_cutoff"
	summary.Coverage = reading.Coverage
	summary.SourceCoverage = reading.SourceCoverage
	return summary, nil
}

func (s *WorkspaceStore) GetWorkbenchSeries(ctx context.Context, subject, id, metric string, bucketSeconds int64, now time.Time) (workbench.Series, error) {
	reading, err := s.loadValidWorkbenchReading(ctx, subject, id, now)
	if err != nil {
		return workbench.Series{}, err
	}
	if metric != "amount" && metric != "requests" {
		return workbench.Series{}, workbench.ErrInvalid
	}
	if bucketSeconds < 300 || bucketSeconds > 2678400 {
		return workbench.Series{}, workbench.ErrInvalid
	}
	seconds := int64(reading.Period.EndExclusive.Sub(reading.Period.StartInclusive).Seconds())
	buckets := (seconds + bucketSeconds - 1) / bucketSeconds
	if buckets > 720 {
		return workbench.Series{}, workbench.ErrTooLarge
	}
	amountColumn, unit := "gateway_charged_micros", "micros"
	if reading.MoneyBasis == workbench.MoneyCalculatedReference {
		amountColumn = "calculated_reference_micros"
	}
	valueExpression := "count(*)"
	if metric == "amount" {
		valueExpression = fmt.Sprintf("sum(f.%s)", amountColumn)
	} else {
		unit = "request"
	}
	query := fmt.Sprintf(`
WITH values_by_bucket AS (
 SELECT floor(extract(epoch FROM (f.occurred_at-$2::timestamptz))/$4)::bigint bucket, %s value
 FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
 WHERE m.reading_id=$1 GROUP BY bucket
)
SELECT bucket_start,bucket_start+($4::bigint*interval '1 second'),COALESCE(v.value,0)::text
FROM generate_series($2::timestamptz,$3::timestamptz-interval '1 microsecond',$4::bigint*interval '1 second') WITH ORDINALITY AS b(bucket_start,ordinal)
LEFT JOIN values_by_bucket v ON v.bucket=b.ordinal-1 ORDER BY bucket_start`, valueExpression)
	rows, err := s.db.QueryContext(ctx, query, id, reading.Period.StartInclusive, reading.Period.EndExclusive, bucketSeconds)
	if err != nil {
		return workbench.Series{}, err
	}
	defer rows.Close()
	points := make([]workbench.SeriesPoint, 0)
	complete := reading.Coverage.Status == "complete_to_cutoff"
	for rows.Next() {
		var point workbench.SeriesPoint
		if err := rows.Scan(&point.StartInclusive, &point.EndExclusive, &point.KnownValue); err != nil {
			return workbench.Series{}, err
		}
		if point.EndExclusive.After(reading.Period.EndExclusive) {
			point.EndExclusive = reading.Period.EndExclusive
		}
		point.Complete = complete
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return workbench.Series{}, err
	}
	return workbench.Series{ReadingID: id, Metric: metric, Unit: unit, BucketSeconds: bucketSeconds, Points: points, Coverage: reading.Coverage}, nil
}

func (s *WorkspaceStore) GetWorkbenchContributions(ctx context.Context, subject, id, dimension string, limit, topN int, cursor string, now time.Time) (workbench.Contributions, error) {
	return s.GetWorkbenchContributionsOrdered(ctx, subject, id, dimension, "amount", limit, topN, cursor, now)
}

// GetWorkbenchContributionsOrdered ranks the same authorized frozen members.
// Amount mode deliberately retains the existing cursor identity.
func (s *WorkspaceStore) GetWorkbenchContributionsOrdered(ctx context.Context, subject, id, dimension, orderBy string, limit, topN int, cursor string, now time.Time) (workbench.Contributions, error) {
	reading, err := s.loadValidWorkbenchReading(ctx, subject, id, now)
	if err != nil {
		return workbench.Contributions{}, err
	}
	if (orderBy != "amount" && orderBy != "requests") || workbenchDimensionSQL(dimension) == "" ||
		((topN < 1 || topN > 50) && (limit < 1 || limit > 50)) || (topN > 0 && limit > 0) || (topN > 0 && cursor != "") {
		return workbench.Contributions{}, workbench.ErrInvalid
	}
	if (dimension == "application" || dimension == "account" || dimension == "purpose") && !workbenchHasDimension(reading, dimension) {
		return workbench.Contributions{}, workbench.ErrScopeForbidden
	}
	pageSize := topN
	if limit > 0 {
		pageSize = limit
	}
	cursorDimension := dimension
	if orderBy == "requests" {
		cursorDimension += ":requests"
	}
	offset := 0
	if cursor != "" {
		var ok bool
		offset, ok = parsePageCursor(cursor, id, cursorDimension)
		if !ok {
			return workbench.Contributions{}, workbench.ErrInvalid
		}
	}
	column := workbenchDimensionSQL(dimension)
	selected := "gateway_charged_micros"
	if reading.MoneyBasis == workbench.MoneyCalculatedReference {
		selected = "calculated_reference_micros"
	}
	ordering := "sum(" + selected + ")"
	if orderBy == "requests" {
		ordering = "count(*)"
	}
	var denominatorRequests, unassignedRequests int64
	var denominatorText, unassignedText string
	var groupCount int
	totalQuery := fmt.Sprintf(`SELECT COALESCE(sum(%[1]s),0)::text,
 COALESCE(sum(%[1]s) FILTER(WHERE %[2]s IS NULL OR %[2]s=''),0)::text,
 count(DISTINCT %[2]s) FILTER(WHERE %[2]s IS NOT NULL AND %[2]s<>''),
 count(*), count(*) FILTER(WHERE %[2]s IS NULL OR %[2]s='')
 FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE reading_id=$1`, selected, column)
	if err := s.db.QueryRowContext(ctx, totalQuery, id).Scan(&denominatorText, &unassignedText, &groupCount, &denominatorRequests, &unassignedRequests); err != nil {
		return workbench.Contributions{}, err
	}
	query := fmt.Sprintf(`
SELECT %[1]s, count(*), COALESCE(sum(%[2]s),0)::text,
       COALESCE(sum(gateway_charged_micros),0)::text,
       COALESCE(sum(calculated_reference_micros),0)::text,
       bool_and(CASE WHEN EXISTS (
         SELECT 1 FROM workbench_access_grants detail_grant
         WHERE detail_grant.subject_id=$4 AND detail_grant.active AND detail_grant.allow_details
       ) THEN EXISTS (
         SELECT 1 FROM workbench_access_grants g
         WHERE g.subject_id=$4 AND g.active AND g.allow_details
           AND g.source_system_id=f.source_system_id
           AND (g.source_project_ref IS NULL OR g.source_project_ref=f.source_project_ref)
           AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref)
       ) ELSE false END)
FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE reading_id=$1 AND %[1]s IS NOT NULL AND %[1]s<>''
GROUP BY %[1]s ORDER BY %[3]s DESC, %[1]s LIMIT $2 OFFSET $3`, column, selected, ordering)
	rows, err := s.db.QueryContext(ctx, query, id, pageSize, offset, subject)
	if err != nil {
		return workbench.Contributions{}, err
	}
	defer rows.Close()
	visible := make([]workbench.Contribution, 0)
	var pageRequests int64
	pageSum := new(big.Int)
	for rows.Next() {
		var row workbench.Contribution
		if err := rows.Scan(&row.Label, &row.RequestCount, &row.SelectedAmountMicros, &row.GatewayChargedMicros, &row.CalculatedReferenceMicros, &row.DrilldownAllowed); err != nil {
			return workbench.Contributions{}, err
		}
		value, err := parseMicros(row.SelectedAmountMicros)
		if err != nil {
			return workbench.Contributions{}, err
		}
		pageRequests += row.RequestCount
		pageSum.Add(pageSum, value)
		row.Ref = contributionRef(dimension, row.Label)
		row.Share = workbench.Ratio{Numerator: row.SelectedAmountMicros, Denominator: denominatorText, Unit: "micros"}
		visible = append(visible, row)
	}
	if err := rows.Err(); err != nil {
		return workbench.Contributions{}, err
	}
	denominator, _ := parseMicros(denominatorText)
	unassigned, _ := parseMicros(unassignedText)
	var otherRequests int64
	other := new(big.Int)
	if topN > 0 {
		otherRequests = denominatorRequests - unassignedRequests - pageRequests
		other.Sub(new(big.Int).Sub(new(big.Int).Set(denominator), unassigned), pageSum)
	}
	next := ""
	if limit > 0 && offset+len(visible) < groupCount {
		next = pageCursor(id, cursorDimension, offset+len(visible))
	}
	mode := "top_n"
	if limit > 0 {
		mode = "full"
	}
	return workbench.Contributions{ReadingID: id, Dimension: dimension, OrderBy: orderBy, Mode: mode, Rows: visible,
		DenominatorRequests: denominatorRequests, UnassignedRequests: unassignedRequests, OtherKnownRequests: otherRequests,
		OtherKnownMicros: other.String(), UnassignedMicros: unassigned.String(), UnassignedScope: "reading_constant",
		DenominatorMicros: denominator.String(), NextCursor: next, IsLastPage: next == "", AuthorizedGroupCount: groupCount}, nil
}

func (s *WorkspaceStore) GetWorkbenchDetails(ctx context.Context, subject, id, ref string, limit int, cursor string, now time.Time) (workbench.Details, error) {
	reading, err := s.loadValidWorkbenchReading(ctx, subject, id, now)
	if err != nil {
		return workbench.Details{}, err
	}
	dimension, label, ok := parseContributionRef(ref)
	if !ok || limit < 1 || limit > workbench.MaxPageSize {
		return workbench.Details{}, workbench.ErrInvalid
	}
	column := workbenchDimensionSQL(dimension)
	if column == "" {
		return workbench.Details{}, workbench.ErrInvalid
	}
	if (dimension == "application" || dimension == "account" || dimension == "purpose") && !workbenchHasDimension(reading, dimension) {
		return workbench.Details{}, workbench.ErrScopeForbidden
	}
	after := int64(0)
	if cursor != "" {
		var cursorOK bool
		after, cursorOK = parseDetailCursor(cursor, id, ref)
		if !cursorOK {
			return workbench.Details{}, workbench.ErrInvalid
		}
	}
	allowed, err := s.workbenchGroupDetailsAllowed(ctx, subject, id, column, label)
	if err != nil {
		return workbench.Details{}, err
	}
	if !allowed {
		return workbench.Details{}, workbench.ErrScopeForbidden
	}
	operator := "="
	if label == "" {
		operator = "IS NOT DISTINCT FROM"
	}
	purposeState := "'unknown'"
	if reading.Purpose != nil {
		purposeState = "'unavailable'"
		if reading.Purpose.Status == "available" {
			purposeState = "(SELECT p.status FROM workbench_purpose_members p WHERE p.reading_id=m.reading_id AND p.economic_fact_id=f.id)"
		}
	}
	query := fmt.Sprintf(`
SELECT request_id,execution_id,evidence_event_id::text,event_hash,source_system_id::text,
       source_version,COALESCE(model_ref,''),COALESCE(source_project_ref,''),COALESCE(cost_project_ref,''),
       terminal_status,occurred_at,gateway_charged_micros::text,
       calculated_reference_micros::text,ordinal,f.id::text,%[3]s
FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE reading_id=$1 AND %[1]s %[2]s NULLIF($2,'')
  AND EXISTS (SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$3
    AND g.active AND g.allow_details AND g.source_system_id=f.source_system_id
    AND (g.source_project_ref IS NULL OR g.source_project_ref=f.source_project_ref)
    AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref))
  AND ordinal>$4 ORDER BY ordinal LIMIT $5`, column, operator, purposeState)
	rows, err := s.db.QueryContext(ctx, query, id, label, subject, after, limit+1)
	if err != nil {
		return workbench.Details{}, err
	}
	defer rows.Close()
	items := make([]workbench.DetailRow, 0)
	ordinals := make([]int64, 0)
	for rows.Next() {
		var item workbench.DetailRow
		var ordinal int64
		if err := rows.Scan(&item.RequestID, &item.ExecutionID, &item.EvidenceEventID, &item.EventHash,
			&item.SourceSystemID, &item.SourceVersion, &item.Model, &item.SourceProject, &item.CostProject,
			&item.TerminalStatus, &item.OccurredAt, &item.GatewayChargedMicros, &item.CalculatedReferenceMicros, &ordinal, &item.EconomicFactID, &item.PurposeState); err != nil {
			return workbench.Details{}, err
		}
		item.AccountState, item.ApplicationState = "unknown", "unknown"
		items = append(items, item)
		ordinals = append(ordinals, ordinal)
	}
	if err := rows.Err(); err != nil {
		return workbench.Details{}, err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		ordinals = ordinals[:limit]
		next = detailCursor(id, ref, ordinals[len(ordinals)-1])
	}
	if reading.SourceManifestRef != "" {
		if err := s.addWorkbenchDetailSources(ctx, subject, id, items); err != nil {
			return workbench.Details{}, err
		}
	}
	if workchain.FromContext(ctx) != nil {
		rows.Close()
		for i := range items {
			_, scopeErr := scopedExecutionRequirement(ctx, s.db, items[i].ExecutionID)
			available := scopeErr == nil
			if scopeErr != nil && !errors.Is(scopeErr, workchain.ErrDenied) {
				return workbench.Details{}, scopeErr
			}
			items[i].InvestigationAvailable = &available
		}
	}
	return workbench.Details{ReadingID: id, ContributionRef: ref, Rows: items, NextCursor: next}, nil
}

func (s *WorkspaceStore) GetWorkbenchSourceStatuses(ctx context.Context, subject, id string, now time.Time) (workbench.SourceStatuses, error) {
	if _, err := s.loadValidWorkbenchReading(ctx, subject, id, now); err != nil {
		return workbench.SourceStatuses{}, err
	}
	filters, err := s.loadWorkbenchFilters(ctx, subject, id)
	if err != nil {
		return workbench.SourceStatuses{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
WITH authorized_sources AS (
 SELECT s.id,s.display_name,s.status,
        bool_or(g.source_project_ref IS NULL AND g.cost_project_ref IS NULL) whole_source
 FROM source_systems s JOIN workbench_access_grants g ON g.source_system_id=s.id
 WHERE g.subject_id=$1 AND g.active AND g.allow_coverage
   AND (cardinality($3::text[])=0 OR s.id::text=ANY($3::text[]))
   AND (cardinality($4::text[])=0 OR g.source_project_ref IS NULL OR g.source_project_ref=ANY($4::text[]))
   AND (cardinality($5::text[])=0 OR g.cost_project_ref IS NULL OR g.cost_project_ref=ANY($5::text[]))
 GROUP BY s.id,s.display_name,s.status
)
SELECT s.id::text,s.display_name,s.status,state.capture_started,state.last_attempt,
 state.last_received,state.received_watermark,members.projection_watermark,
 s.whole_source,state.connected,COALESCE(gaps.gap_count,0)
FROM authorized_sources s
LEFT JOIN LATERAL (
 SELECT min(activated_at) capture_started,max(GREATEST(last_import_started_at,last_poll_failed_at)) last_attempt,
 max(last_import_completed_at) last_received,
 CASE WHEN count(*)=1 THEN max(high_watermark) ELSE '' END received_watermark,
 bool_and(status<>'error' AND worker_status NOT IN ('inactive','stopping','error')) connected
 FROM adapter_import_states
 WHERE source_system_id=s.id AND cursor_key='unirouter.completed-execution.v1' AND s.whole_source
) state ON true
LEFT JOIN LATERAL (
 SELECT max(f.projected_at) projection_watermark FROM workbench_reading_members m
 JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE m.reading_id=$2 AND f.source_system_id=s.id
) members ON true
LEFT JOIN LATERAL (
 SELECT count(*) gap_count FROM capture_gaps g
 WHERE g.source_system_id=s.id AND s.whole_source
   AND EXISTS (
     SELECT 1 FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
     WHERE m.reading_id=$2 AND f.source_system_id=s.id AND f.request_id=g.request_id
   )
) gaps ON true
ORDER BY s.display_name,s.id`, subject, id, pgTextArray(filters.SourceIDs), pgTextArray(filters.SourceProjects), pgTextArray(filters.CostProjects))
	if err != nil {
		return workbench.SourceStatuses{}, err
	}
	defer rows.Close()
	items := make([]workbench.SourceStatus, 0)
	for rows.Next() {
		var item workbench.SourceStatus
		var capture, lastAttempt, last, projection sql.NullTime
		var gaps int64
		var sourceState string
		var wholeSource bool
		var connected sql.NullBool
		if err := rows.Scan(&item.SourceSystemID, &item.DisplayName, &sourceState, &capture, &lastAttempt, &last,
			&item.ReceivedWatermark, &projection, &wholeSource, &connected, &gaps); err != nil {
			return workbench.SourceStatuses{}, err
		}
		if capture.Valid {
			value := capture.Time.UTC()
			item.CaptureStartedAt = &value
		}
		if last.Valid {
			value := last.Time.UTC()
			item.LastReceivedAt = &value
		}
		if lastAttempt.Valid {
			value := lastAttempt.Time.UTC()
			item.LastAttemptAt = &value
		}
		if projection.Valid {
			item.ProjectionWatermark = projection.Time.UTC().Format(time.RFC3339Nano)
		}
		item.ScopeLabel = "whole_source"
		item.Gaps = []string{}
		if gaps > 0 {
			item.Gaps = []string{"source_profile_capture_gaps_present"}
		}
		if !wholeSource {
			item.ScopeLabel = "project_restricted"
			item.Status = "scope_limited_unknown"
			item.Gaps = []string{"scope_limited_source_state_unknown"}
			item.CaptureStartedAt = nil
			item.LastAttemptAt = nil
			item.LastReceivedAt = nil
			item.ReceivedWatermark = ""
		} else if !capture.Valid && !lastAttempt.Valid {
			item.Status = "not_connected"
		} else if sourceState != "active" || !connected.Valid || !connected.Bool {
			item.Status = "disconnected"
		} else if gaps > 0 {
			item.Status = "observed_with_gaps"
		} else {
			item.Status = "observed"
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return workbench.SourceStatuses{}, err
	}
	rows.Close()
	profiles, err := s.loadSourceFactsStatuses(ctx, subject, filters)
	if err != nil {
		return workbench.SourceStatuses{}, err
	}
	for i := range items {
		items[i].SourceFacts = profiles[items[i].SourceSystemID]
	}
	return workbench.SourceStatuses{ReadingID: id, Items: items}, nil
}

func (s *WorkspaceStore) CreateWorkbenchSavedFilter(ctx context.Context, subject string, command workbench.SavedFilterCommand, now time.Time) (workbench.SavedFilter, error) {
	if err := workbench.ValidateSavedFilterAt(command, now); err != nil {
		return workbench.SavedFilter{}, err
	}
	authorization, err := loadWorkbenchAuthorization(ctx, s.db, subject)
	if err != nil {
		return workbench.SavedFilter{}, err
	}
	if !authorizationAllows(authorization, command.Config.Filters, func(grant workbench.Grant) bool {
		return grant.AllowPreferences && grant.AllowSummary
	}) {
		return workbench.SavedFilter{}, workbench.ErrScopeForbidden
	}
	id, err := workbench.NewID("xwf_")
	if err != nil {
		return workbench.SavedFilter{}, err
	}
	config, _ := json.Marshal(command.Config)
	contentDigest := savedFilterContentDigest(command.Name, config)
	digest := savedOperationDigest("create", "", command.ExpectedRevision, command.Name, config)
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workbench.SavedFilter{}, err
	}
	defer tx.Rollback()
	if replay, found, err := reserveSavedOperation(ctx, tx, subject, "create", id, command.IdempotencyKey, digest, now); err != nil {
		return workbench.SavedFilter{}, err
	} else if found {
		return replay, nil
	}
	var item workbench.SavedFilter
	var raw []byte
	err = tx.QueryRowContext(ctx, `
	INSERT INTO workbench_saved_filters(id,owner_subject,name,config_version,config,content_sha256,created_at,updated_at)
	VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$7)
	RETURNING id,owner_subject,name,config,revision,created_at,updated_at`, id, subject, command.Name, workbench.ContractVersion, string(config), contentDigest, now).
		Scan(&item.ID, &item.OwnerSubject, &item.Name, &raw, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return workbench.SavedFilter{}, mapWorkbenchDBError(err)
	}
	if err := json.Unmarshal(raw, &item.Config); err != nil {
		return workbench.SavedFilter{}, err
	}
	if err := completeSavedOperation(ctx, tx, subject, "create", command.IdempotencyKey, item); err != nil {
		return workbench.SavedFilter{}, err
	}
	if err := commitWorkspaceTx(ctx, tx); err != nil {
		return workbench.SavedFilter{}, err
	}
	return item, nil
}

func (s *WorkspaceStore) UpdateWorkbenchSavedFilter(ctx context.Context, subject, id string, command workbench.SavedFilterCommand, now time.Time) (workbench.SavedFilter, error) {
	if !workbench.ValidSavedFilterID(id) {
		return workbench.SavedFilter{}, workbench.ErrNotFound
	}
	if err := workbench.ValidateSavedFilterAt(command, now); err != nil || command.ExpectedRevision < 1 {
		return workbench.SavedFilter{}, workbench.ErrInvalid
	}
	authorization, err := loadWorkbenchAuthorization(ctx, s.db, subject)
	if err != nil {
		return workbench.SavedFilter{}, err
	}
	if !authorizationAllows(authorization, command.Config.Filters, func(grant workbench.Grant) bool {
		return grant.AllowPreferences && grant.AllowSummary
	}) {
		return workbench.SavedFilter{}, workbench.ErrScopeForbidden
	}
	config, _ := json.Marshal(command.Config)
	contentDigest := savedFilterContentDigest(command.Name, config)
	digest := savedOperationDigest("update", id, command.ExpectedRevision, command.Name, config)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workbench.SavedFilter{}, err
	}
	defer tx.Rollback()
	if replay, found, err := reserveSavedOperation(ctx, tx, subject, "update", id, command.IdempotencyKey, digest, now.UTC()); err != nil {
		return workbench.SavedFilter{}, err
	} else if found {
		return replay, nil
	}
	var item workbench.SavedFilter
	var raw []byte
	err = tx.QueryRowContext(ctx, `
	UPDATE workbench_saved_filters SET name=$3,config=$4::jsonb,content_sha256=$5,
	 revision=revision+1,updated_at=$6 WHERE id=$1 AND owner_subject=$2 AND revision=$7
	RETURNING id,owner_subject,name,config,revision,created_at,updated_at`, id, subject, command.Name, string(config), contentDigest, now.UTC(), command.ExpectedRevision).
		Scan(&item.ID, &item.OwnerSubject, &item.Name, &raw, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		_ = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workbench_saved_filters WHERE id=$1 AND owner_subject=$2)`, id, subject).Scan(&exists)
		if exists {
			return workbench.SavedFilter{}, workbench.ErrStaleRevision
		}
		return workbench.SavedFilter{}, workbench.ErrNotFound
	}
	if err != nil {
		return workbench.SavedFilter{}, mapWorkbenchDBError(err)
	}
	if err := json.Unmarshal(raw, &item.Config); err != nil {
		return workbench.SavedFilter{}, err
	}
	if err := completeSavedOperation(ctx, tx, subject, "update", command.IdempotencyKey, item); err != nil {
		return workbench.SavedFilter{}, err
	}
	if err := commitWorkspaceTx(ctx, tx); err != nil {
		return workbench.SavedFilter{}, err
	}
	return item, nil
}

func (s *WorkspaceStore) ListWorkbenchSavedFilters(ctx context.Context, subject string) (workbench.SavedFilterList, error) {
	if ok, err := s.workbenchCapabilityAllowed(ctx, subject, "allow_preferences"); err != nil {
		return workbench.SavedFilterList{}, err
	} else if !ok {
		return workbench.SavedFilterList{}, workbench.ErrScopeForbidden
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,owner_subject,name,config,revision,created_at,updated_at FROM workbench_saved_filters WHERE owner_subject=$1 ORDER BY updated_at DESC,id`, subject)
	if err != nil {
		return workbench.SavedFilterList{}, err
	}
	defer rows.Close()
	items := make([]workbench.SavedFilter, 0)
	for rows.Next() {
		var item workbench.SavedFilter
		var raw []byte
		if err := rows.Scan(&item.ID, &item.OwnerSubject, &item.Name, &raw, &item.Revision, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return workbench.SavedFilterList{}, err
		}
		if err := json.Unmarshal(raw, &item.Config); err != nil {
			return workbench.SavedFilterList{}, err
		}
		items = append(items, item)
	}
	return workbench.SavedFilterList{Items: items}, rows.Err()
}

func (s *WorkspaceStore) loadValidWorkbenchReading(ctx context.Context, subject, id string, now time.Time) (workbench.Reading, error) {
	return loadValidWorkbenchReading(ctx, s.db, subject, id, now)
}

func loadValidWorkbenchReading(ctx context.Context, q workspaceQueryer, subject, id string, now time.Time) (workbench.Reading, error) {
	if !workbench.ValidReadingID(id) {
		return workbench.Reading{}, workbench.ErrNotFound
	}
	var item workbench.Reading
	var filtersRaw, coverageRaw []byte
	var timezone string
	err := q.QueryRowContext(ctx, `SELECT id,contract_version,metric_version,period_start,period_end,display_timezone,cutoff,expires_at,money_basis,currency,membership_sha256,interpretation_revision,authorization_revision,filters,coverage FROM workbench_readings WHERE id=$1 AND owner_subject=$2`, id, subject).Scan(&item.ID, &item.ContractVersion, &item.MetricVersion, &item.Period.StartInclusive, &item.Period.EndExclusive, &timezone, &item.Cutoff, &item.ExpiresAt, &item.MoneyBasis, &item.Currency, &item.MembershipRef, &item.InterpretationRevision, &item.AuthorizationRevision, &filtersRaw, &coverageRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return workbench.Reading{}, workbench.ErrNotFound
	}
	if err != nil {
		return workbench.Reading{}, err
	}
	// Database drivers may scan timestamptz in the process/session location.
	// Public reading instants use UTC, preserving their full stored precision.
	item.Period.StartInclusive = item.Period.StartInclusive.UTC()
	item.Period.EndExclusive = item.Period.EndExclusive.UTC()
	item.Cutoff = item.Cutoff.UTC()
	item.ExpiresAt = item.ExpiresAt.UTC()
	if !now.UTC().Before(item.ExpiresAt) {
		return workbench.Reading{}, workbench.ErrExpired
	}
	auth, err := loadWorkbenchAuthorization(ctx, q, subject)
	if err != nil {
		return workbench.Reading{}, err
	}
	if auth.Revision != item.AuthorizationRevision {
		return workbench.Reading{}, workbench.ErrAccessChanged
	}
	// A0 binds these stored F0 fields and unknown/unavailable dimensions. Future
	// revisions must dispatch explicitly; never reinterpret an old reading.
	item.Dimensions = []string{"model", "source_project", "cost_project"}
	switch item.InterpretationRevision {
	case digestText(workbenchInterpretationA0):
	case digestText(workbenchInterpretationA1):
		if err := loadWorkbenchSourceManifest(ctx, q, &item, auth); err != nil {
			return workbench.Reading{}, err
		}
	default:
		return workbench.Reading{}, workbench.ErrUnsupportedVersion
	}
	if err := loadWorkbenchPurposeManifest(ctx, q, &item, auth); err != nil {
		return workbench.Reading{}, err
	}
	item.Period.Timezone = timezone
	item.MoneyTitle = workbench.MoneyTitle(item.MoneyBasis)
	if err := json.Unmarshal(coverageRaw, &item.Coverage); err != nil {
		return workbench.Reading{}, err
	}
	return item, nil
}

func loadWorkbenchAuthorization(ctx context.Context, q workspaceQueryer, subject string) (workbench.Authorization, error) {
	rows, err := q.QueryContext(ctx, `SELECT id::text,subject_id,source_system_id::text,COALESCE(source_project_ref,''),COALESCE(cost_project_ref,''),allow_summary,allow_details,allow_coverage,allow_preferences,revision,COALESCE((to_jsonb(workbench_access_grants)->>'allow_source_context')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_source_identity')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_source_cost')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_purpose_read')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_purpose_review')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_source_runtime')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_source_attempt')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_source_capability')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_source_policy')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_source_destination')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_resource')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_budget_definition')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_budget_summary')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_budget_alert')::boolean,false),COALESCE((to_jsonb(workbench_access_grants)->>'allow_content')::boolean,false),COALESCE(to_jsonb(workbench_access_grants)->'allowed_content_purposes','[]'::jsonb),COALESCE((to_jsonb(workbench_access_grants)->>'legacy_revision')::bigint,revision),COALESCE((to_jsonb(workbench_access_grants)->>'content_revision')::bigint,0) FROM workbench_access_grants WHERE subject_id=$1 AND active ORDER BY source_system_id,source_project_ref NULLS FIRST,cost_project_ref NULLS FIRST,id`, subject)
	if err != nil {
		return workbench.Authorization{}, err
	}
	defer rows.Close()
	grants := make([]workbench.Grant, 0)
	for rows.Next() {
		var g workbench.Grant
		var purposes []byte
		if err := rows.Scan(&g.ID, &g.SubjectID, &g.SourceSystemID, &g.SourceProject, &g.CostProject, &g.AllowSummary, &g.AllowDetails, &g.AllowCoverage, &g.AllowPreferences, &g.Revision, &g.AllowSourceContext, &g.AllowSourceIdentity, &g.AllowSourceCost, &g.AllowPurposeRead, &g.AllowPurposeReview, &g.AllowSourceRuntime, &g.AllowSourceAttempt, &g.AllowSourceCapability, &g.AllowSourcePolicy, &g.AllowSourceDestination, &g.AllowResource, &g.AllowBudgetDefinition, &g.AllowBudgetSummary, &g.AllowBudgetAlert, &g.AllowContent, &purposes, &g.LegacyRevision, &g.ContentRevision); err != nil {
			return workbench.Authorization{}, err
		}
		if err := json.Unmarshal(purposes, &g.AllowedContentPurposes); err != nil {
			return workbench.Authorization{}, err
		}
		grants = append(grants, g)

	}
	if err := rows.Err(); err != nil {
		return workbench.Authorization{}, err
	}
	return workbenchAuthorizationFromGrants(grants), nil
}

// Historical schema36-38 CHECK requires a legacy primary capability. Filtering
// C-only grants here therefore preserves every valid old row and its bytes.
func workbenchAuthorizationFromGrants(grants []workbench.Grant) workbench.Authorization {
	parts, sourceParts, purposeParts, observationParts, budgetParts, contentParts := []string{}, []string{}, []string{}, []string{}, []string{}, []string{}
	for _, g := range grants {
		if g.AllowContent && len(g.AllowedContentPurposes) == 1 && g.AllowedContentPurposes[0] == "human_review" {
			raw, _ := json.Marshal([]any{g.ID, g.SubjectID, g.SourceSystemID, g.SourceProject, g.CostProject, g.AllowContent, g.AllowedContentPurposes, g.ContentRevision})
			contentParts = append(contentParts, string(raw))
		}
		if !(g.AllowSummary || g.AllowDetails || g.AllowCoverage || g.AllowPreferences || g.AllowSourceRuntime || g.AllowSourceAttempt || g.AllowSourceCapability || g.AllowSourcePolicy || g.AllowSourceDestination || g.AllowResource || g.AllowBudgetDefinition || g.AllowBudgetSummary || g.AllowBudgetAlert) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%t\x00%t\x00%t\x00%t\x00%d", g.ID, g.SourceSystemID, g.SourceProject, g.CostProject, g.AllowSummary, g.AllowDetails, g.AllowCoverage, g.AllowPreferences, g.LegacyRevision))
		purposeParts = append(purposeParts, fmt.Sprintf("%s\x00%t\x00%t", parts[len(parts)-1], g.AllowPurposeRead, g.AllowPurposeReview))
		sourceParts = append(sourceParts, fmt.Sprintf("%s\x00%t\x00%t\x00%t", parts[len(parts)-1], g.AllowSourceContext, g.AllowSourceIdentity, g.AllowSourceCost))
		observationParts = append(observationParts, fmt.Sprintf("%s\x00%t\x00%t\x00%t\x00%t\x00%t", sourceParts[len(sourceParts)-1], g.AllowSourceRuntime, g.AllowSourceAttempt, g.AllowSourceCapability, g.AllowSourcePolicy, g.AllowSourceDestination))
		budgetParts = append(budgetParts, fmt.Sprintf("%s\x00%t\x00%t\x00%t", parts[len(parts)-1], g.AllowBudgetDefinition, g.AllowBudgetSummary, g.AllowBudgetAlert))
		if g.AllowResource {
			observationParts[len(observationParts)-1] += "\x00resource:1"
		}
	}
	contentRevision := ""
	if len(contentParts) > 0 {
		contentRevision = digestText("xinsight.content-authorization.v1\n" + strings.Join(contentParts, "\n"))
	}
	return workbench.Authorization{Revision: digestText(strings.Join(parts, "\n")), SourceRevision: digestText(strings.Join(sourceParts, "\n")), PurposeRevision: digestText(strings.Join(purposeParts, "\n")), ObservationRevision: digestText(strings.Join(observationParts, "\n")), BudgetRevision: digestText(strings.Join(budgetParts, "\n")), ContentRevision: contentRevision, Grants: grants}
}

func authorizationAllows(auth workbench.Authorization, filters workbench.Filters, capability func(workbench.Grant) bool) bool {
	eligible := make([]workbench.Grant, 0)
	for _, g := range auth.Grants {
		if capability(g) {
			eligible = append(eligible, g)
		}
	}
	if len(eligible) == 0 {
		return false
	}
	sources := filters.SourceIDs
	if len(sources) == 0 {
		sources = []string{""}
	}
	sourceProjects := filters.SourceProjects
	if len(sourceProjects) == 0 {
		sourceProjects = []string{""}
	}
	costProjects := filters.CostProjects
	if len(costProjects) == 0 {
		costProjects = []string{""}
	}
	for _, source := range sources {
		for _, sourceProject := range sourceProjects {
			for _, costProject := range costProjects {
				covered := false
				for _, grant := range eligible {
					if source != "" && grant.SourceSystemID != source {
						continue
					}
					if sourceProject != "" && grant.SourceProject != "" && grant.SourceProject != sourceProject {
						continue
					}
					if costProject != "" && grant.CostProject != "" && grant.CostProject != costProject {
						continue
					}
					covered = true
					break
				}
				if !covered {
					return false
				}
			}
		}
	}
	return true
}

func loadWorkbenchCoverage(ctx context.Context, q workspaceQueryer, subject, readingID string, filters workbench.Filters, known int64, periodStart time.Time) (workbench.Coverage, error) {
	var capture, last, projection sql.NullTime
	var received string
	var gaps, sources int64
	var allWhole, allActivated, allConnected bool
	err := q.QueryRowContext(ctx, `
WITH authorized_sources AS (
 SELECT g.source_system_id,bool_or(g.source_project_ref IS NULL AND g.cost_project_ref IS NULL) whole_source,
        bool_and(s.status='active') source_active
 FROM workbench_access_grants g
 JOIN source_systems s ON s.id=g.source_system_id
 WHERE g.subject_id=$1 AND g.active AND g.allow_summary
   AND (cardinality($3::text[])=0 OR g.source_system_id::text=ANY($3::text[]))
   AND (cardinality($4::text[])=0 OR g.source_project_ref IS NULL OR g.source_project_ref=ANY($4::text[]))
   AND (cardinality($5::text[])=0 OR g.cost_project_ref IS NULL OR g.cost_project_ref=ANY($5::text[]))
 GROUP BY g.source_system_id
), per_source_state AS (
 SELECT s.source_system_id,s.whole_source,min(a.activated_at) capture_started,max(a.last_import_completed_at) last_received,
   CASE WHEN count(a.*)=1 THEN max(a.high_watermark) ELSE '' END received_watermark,
   s.source_active AND count(a.*)>0 AND bool_and(a.status<>'error' AND a.worker_status NOT IN ('inactive','stopping','error')) connected
 FROM authorized_sources s LEFT JOIN adapter_import_states a
   ON a.source_system_id=s.source_system_id
  AND a.cursor_key='unirouter.completed-execution.v1'
  AND s.whole_source
 GROUP BY s.source_system_id,s.whole_source,s.source_active
), state AS (
 SELECT min(capture_started) capture_started,min(last_received) weakest_received,
  CASE WHEN count(*)=1 AND bool_and(whole_source) THEN max(received_watermark) ELSE '' END received_watermark,
  bool_and(whole_source) all_whole,
  bool_and(capture_started IS NOT NULL AND capture_started<=$6) all_activated,
  bool_and(connected) all_connected
 FROM per_source_state
), member_state AS (
 SELECT max(f.projected_at) projection_watermark,max(e.ingested_at) last_durable FROM workbench_reading_members m
 JOIN workbench_economic_facts f ON f.id=m.economic_fact_id JOIN evidence_events e ON e.id=f.evidence_event_id WHERE m.reading_id=$2
), gap_state AS (
 SELECT count(*) gap_count FROM capture_gaps g
 JOIN authorized_sources s ON s.source_system_id=g.source_system_id AND s.whole_source
 WHERE EXISTS (
   SELECT 1 FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
   WHERE m.reading_id=$2 AND f.source_system_id=g.source_system_id AND f.request_id=g.request_id
 )
)
SELECT (SELECT count(*) FROM authorized_sources),state.capture_started,member_state.last_durable,
 state.received_watermark,member_state.projection_watermark,gap_state.gap_count,
 COALESCE(state.all_whole,false),COALESCE(state.all_activated,false),COALESCE(state.all_connected,false)
FROM state,member_state,gap_state`, subject, readingID, pgTextArray(filters.SourceIDs), pgTextArray(filters.SourceProjects), pgTextArray(filters.CostProjects), periodStart).Scan(&sources, &capture, &last, &received, &projection, &gaps, &allWhole, &allActivated, &allConnected)
	if err != nil {
		return workbench.Coverage{}, err
	}
	item := workbench.Coverage{
		Status:                       "period_completeness_unknown",
		PeriodCompleteness:           "unknown",
		ProjectionCompleteForMembers: true,
		ReceivedWatermark:            received,
		KnownItems:                   known,
		Gaps:                         []string{"source_period_completeness_not_asserted"},
	}
	if capture.Valid {
		v := capture.Time.UTC()
		item.CaptureStartedAt = &v
	}
	if last.Valid {
		v := last.Time.UTC()
		item.LastReceivedAt = &v
	}
	if projection.Valid {
		item.ProjectionWatermark = projection.Time.UTC().Format(time.RFC3339Nano)
	}
	if sources == 0 || (allWhole && !capture.Valid) {
		item.Status = "not_connected"
	} else if !allWhole {
		item.Status = "scope_limited_unknown"
		item.CaptureStartedAt = nil
		item.LastReceivedAt = nil
		item.ReceivedWatermark = ""
		item.Gaps = append(item.Gaps, "scope_limited_source_state_unknown")
	} else if !allConnected {
		item.Status = "disconnected"
		item.Gaps = append(item.Gaps, "source_not_currently_connected")
	} else if gaps > 0 {
		item.Status = "observed_with_gaps"
		item.Gaps = append(item.Gaps, "source_profile_capture_gaps_present")
	} else {
		item.Status = "observed"
	}
	// History coverage is independent of profile gaps and current connectivity.
	if allWhole && capture.Valid && !allActivated {
		item.Gaps = append(item.Gaps, "period_precedes_capture_start")
	}
	return item, nil
}

func (s *WorkspaceStore) loadWorkbenchFilters(ctx context.Context, subject, id string) (workbench.Filters, error) {
	var raw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT filters FROM workbench_readings WHERE id=$1 AND owner_subject=$2`, id, subject).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return workbench.Filters{}, workbench.ErrNotFound
	} else if err != nil {
		return workbench.Filters{}, err
	}
	var filters workbench.Filters
	if err := json.Unmarshal(raw, &filters); err != nil {
		return workbench.Filters{}, err
	}
	return filters, nil
}

func (s *WorkspaceStore) workbenchCapabilityAllowed(ctx context.Context, subject, column string) (bool, error) {
	query := fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM workbench_access_grants WHERE subject_id=$1 AND active AND %s)`, column)
	var ok bool
	err := s.db.QueryRowContext(ctx, query, subject).Scan(&ok)
	return ok, err
}
func (s *WorkspaceStore) workbenchGroupDetailsAllowed(ctx context.Context, subject, readingID, column, label string) (bool, error) {
	operator := "="
	if label == "" {
		operator = "IS NOT DISTINCT FROM"
	}
	query := fmt.Sprintf(`SELECT NOT EXISTS(SELECT 1 FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE m.reading_id=$1 AND %s %s NULLIF($2,'') AND NOT EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$3 AND g.active AND g.allow_details AND g.source_system_id=f.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref=f.source_project_ref) AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref))) AND EXISTS(SELECT 1 FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE reading_id=$1 AND %s %s NULLIF($2,''))`, column, operator, column, operator)
	var ok bool
	err := s.db.QueryRowContext(ctx, query, readingID, label, subject).Scan(&ok)
	return ok, err
}

func contributionRef(dimension, label string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(dimension + "\x00" + label))
}
func parseContributionRef(value string) (string, string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(raw), "\x00", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
func digestText(value string) string { return digestBytes([]byte(value)) }
func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func savedOperationDigest(operation, target string, expected int64, name string, config []byte) string {
	return digestText(operation + "\x00" + target + "\x00" + strconv.FormatInt(expected, 10) + "\x00" + name + "\x00" + string(config))
}
func savedFilterContentDigest(name string, config []byte) string {
	return digestText(name + "\x00" + string(config))
}
func reserveSavedOperation(ctx context.Context, tx *sql.Tx, subject, operation, target, key, digest string, now time.Time) (workbench.SavedFilter, bool, error) {
	result, err := tx.ExecContext(ctx, `INSERT INTO workbench_saved_filter_operations(subject_id,operation_type,target_id,idempotency_key,content_sha256,created_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(subject_id,operation_type,idempotency_key) DO NOTHING`, subject, operation, target, key, digest, now)
	if err != nil {
		return workbench.SavedFilter{}, false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return workbench.SavedFilter{}, false, err
	}
	if inserted == 1 {
		return workbench.SavedFilter{}, false, nil
	}
	var existingDigest string
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT content_sha256,result FROM workbench_saved_filter_operations WHERE subject_id=$1 AND operation_type=$2 AND idempotency_key=$3`, subject, operation, key).Scan(&existingDigest, &raw); err != nil {
		return workbench.SavedFilter{}, false, err
	}
	if existingDigest != digest || len(raw) == 0 {
		return workbench.SavedFilter{}, false, workbench.ErrConflict
	}
	var item workbench.SavedFilter
	if err := json.Unmarshal(raw, &item); err != nil {
		return workbench.SavedFilter{}, false, err
	}
	return item, true, nil
}
func completeSavedOperation(ctx context.Context, tx *sql.Tx, subject, operation, key string, item workbench.SavedFilter) error {
	raw, err := json.Marshal(item)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE workbench_saved_filter_operations SET result=$4::jsonb WHERE subject_id=$1 AND operation_type=$2 AND idempotency_key=$3 AND result IS NULL`, subject, operation, key, string(raw))
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return workbench.ErrConflict
	}
	return nil
}
func parseMicros(value string) (*big.Int, error) {
	out, ok := new(big.Int).SetString(value, 10)
	if !ok || out.Sign() < 0 {
		return nil, fmt.Errorf("invalid micros")
	}
	return out, nil
}
func mapWorkbenchDBError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return workbench.ErrConflict
	}
	return err
}

func pgTextArray(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func pageCursor(readingID, dimension string, offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(readingID + "\x00" + dimension + "\x00" + strconv.Itoa(offset)))
}
func parsePageCursor(value, readingID, dimension string) (int, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, false
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 || parts[0] != readingID || parts[1] != dimension {
		return 0, false
	}
	offset, err := strconv.Atoi(parts[2])
	return offset, err == nil && offset >= 0
}
func detailCursor(readingID, ref string, ordinal int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(readingID + "\x00" + ref + "\x00" + strconv.FormatInt(ordinal, 10)))
}
func parseDetailCursor(value, readingID, ref string) (int64, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, false
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 || parts[0] != readingID || parts[1] != ref {
		return 0, false
	}
	ordinal, err := strconv.ParseInt(parts[2], 10, 64)
	return ordinal, err == nil && ordinal >= 0
}

var _ = strconv.Itoa
