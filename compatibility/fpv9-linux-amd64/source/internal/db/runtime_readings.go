package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

const runtimeInterpretationBeforeResources = "xinsight.runtime-reading.v1:authorized-latest-revisions;trusted-request-start;unknown-entry-observed-in-period;independent-request-denominator;independent-runtime-grants-v1;" + runtimeMeasurementEligibility + ";" + workbench.RuntimeMeasurementVersion + ";" + workbench.RuntimeSummaryVersion + ";" + workbench.RuntimeDimensionsVersion + ";" + workbench.RuntimeAssociationsVersion
const runtimeInterpretation = runtimeInterpretationBeforeResources + ";" + workbench.RuntimeResourcesVersion

const runtimeProfileGrantSQL = `((d.profile='unirouter.runtime-observation.v1' AND g.allow_source_runtime)
 OR (d.profile='unirouter.attempt-observation.v1' AND g.allow_source_attempt)
 OR (d.profile='unirouter.capability-snapshot.v1' AND g.allow_source_capability)
 OR (d.profile='unirouter.policy-observation.v1' AND g.allow_source_policy)
 OR (d.profile='unirouter.destination-observation.v1' AND g.allow_source_destination))`

// The existing I-01 join verifies source instance, org, project and request.
// A cost-project constraint is satisfied only by that actual economic anchor;
// an unpriced request remains available to an authorized source-project reader.
const runtimeVisibleSQL = `WITH RECURSIVE latest AS (
 SELECT DISTINCT ON(d.source_system_id,d.source_fact_id)
 d.source_system_id,d.source_fact_id,d.event_id,d.revision,d.previous_hash,
 d.source_instance,d.org_id,d.project_id,d.request_id,d.occurred_at,
 d.profile,d.subject_kind,d.observation_kind FROM source_observation_documents d
 LEFT JOIN workbench_economic_facts f ON ` + sourceEconomicJoinSQL + `
 WHERE d.received_at<=$5
 AND (cardinality($6::text[])=0 OR d.source_system_id::text=ANY($6::text[]))
 AND (cardinality($7::text[])=0 OR 'project-'||d.project_id=ANY($7::text[]))
 AND (cardinality($8::text[])=0 OR f.cost_project_ref=ANY($8::text[]))
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$2 AND g.active
 AND (cardinality($8::text[])=0 OR (g.allow_summary AND g.allow_details)) AND g.source_system_id=d.source_system_id
 AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id)
 AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref) AND ` + runtimeProfileGrantSQL + `)
 ORDER BY d.source_system_id,d.source_fact_id,d.revision DESC
), lineage AS (
 SELECT source_system_id,source_fact_id,event_id root_event,revision,previous_hash FROM latest
 UNION ALL SELECT d.source_system_id,d.source_fact_id,l.root_event,d.revision,d.previous_hash
 FROM lineage l JOIN source_observation_documents d ON d.source_system_id=l.source_system_id
 AND d.source_fact_id=l.source_fact_id AND d.revision=l.revision-1 AND d.source_hash=l.previous_hash AND d.received_at<=$5
), visible AS (
 SELECT d.*,CASE WHEN EXISTS(SELECT 1 FROM lineage l WHERE l.source_system_id=d.source_system_id
 AND l.root_event=d.event_id AND l.revision=1) THEN 'complete' ELSE 'missing_predecessor' END chain_state FROM latest d
), grouped AS (
 SELECT source_system_id,source_instance,org_id,project_id,request_id,
 min(occurred_at) FILTER(WHERE profile='unirouter.runtime-observation.v1' AND subject_kind='request'
   AND observation_kind='request_started' AND chain_state='complete') entry_at,
 max(occurred_at) last_observed_at,count(*) observation_count,
 bool_or(occurred_at>=$3 AND occurred_at<$4) observed_in_period
 FROM visible GROUP BY source_system_id,source_instance,org_id,project_id,request_id
), selected AS (
 SELECT source_system_id,source_instance,org_id,project_id,request_id,entry_at,last_observed_at,observation_count,observed_in_period
 FROM grouped WHERE ` + runtimeSelectedPredicate + `
) `

const runtimeSelectedPredicate = `(entry_at>=$3 AND entry_at<$4) OR (entry_at IS NULL AND observed_in_period)`

// Carry eligibility on each authorized observation. This avoids joining to a
// just-inserted membership table whose statistics cannot describe this reading.
const runtimeMemberInsertSQL = runtimeVisibleSQL + `, eligible AS (
 SELECT d.*,min(occurred_at) FILTER(WHERE profile='unirouter.runtime-observation.v1'
 AND subject_kind='request' AND observation_kind='request_started' AND chain_state='complete') OVER request_group entry_at,
 bool_or(occurred_at>=$3 AND occurred_at<$4) OVER request_group observed_in_period
 FROM visible d WINDOW request_group AS (PARTITION BY source_system_id,source_instance,org_id,project_id,request_id)
) INSERT INTO runtime_reading_members(reading_id,request_ref,source_system_id,event_id,chain_state)
 SELECT $1,'xrr_'||encode(sha256(convert_to(jsonb_build_array(source_system_id::text,source_instance,org_id,project_id,request_id)::text,'UTF8')),'hex'),
 source_system_id,event_id,chain_state FROM eligible WHERE ` + runtimeSelectedPredicate

const runtimeManifestDigestPrefixSQL = `SELECT 'sha256:'||encode(sha256(convert_to(
 jsonb_build_array(r.contract_version,r.metric_version,r.period_start AT TIME ZONE 'UTC',r.period_end AT TIME ZONE 'UTC',r.display_timezone,
 r.cutoff AT TIME ZONE 'UTC',r.expires_at AT TIME ZONE 'UTC',r.filters,r.authorization_revision,r.interpretation_revision,
 r.known_entry_requests,r.unknown_entry_requests,r.observation_count,r.coverage)::text||E'\n'||
 COALESCE((SELECT string_agg(jsonb_build_array(q.ordinal,q.request_ref,q.source_system_id,q.source_instance,
 q.org_id,q.project_id,q.request_id,q.entry_at AT TIME ZONE 'UTC',q.last_observed_at AT TIME ZONE 'UTC',q.observation_count)::text,E'\n' ORDER BY q.ordinal)
 FROM runtime_reading_requests q WHERE q.reading_id=r.id),'')||E'\n'||
 COALESCE((SELECT string_agg(jsonb_build_array(m.request_ref,m.source_system_id,m.event_id,m.chain_state,
 d.source_hash,d.envelope_hash)::text,E'\n' ORDER BY m.source_system_id,m.event_id)
 FROM runtime_reading_members m JOIN source_observation_documents d USING(source_system_id,event_id)
 WHERE m.reading_id=r.id),'')||E'\n'||
 COALESCE((SELECT string_agg(jsonb_build_array(m.request_ref,m.source_system_id,m.event_id,m.chain_state,
 d.source_hash,d.envelope_hash)::text,E'\n' ORDER BY m.source_system_id,m.event_id)
 FROM runtime_reading_source_members m JOIN source_fact_documents d USING(source_system_id,event_id)
 WHERE m.reading_id=r.id),'')||E'\n'||
 COALESCE((SELECT string_agg(jsonb_build_array(m.request_ref,m.economic_fact_id,f.execution_id,f.source_event_hash)::text,E'\n' ORDER BY m.economic_fact_id)
 FROM runtime_reading_economic_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
 WHERE m.reading_id=r.id),'')`
const runtimeManifestDigestSuffixSQL = `,'UTF8')),'hex') FROM runtime_readings r WHERE r.id=$1`
const runtimeManifestDigestBeforeResourcesSQL = runtimeManifestDigestPrefixSQL + runtimeManifestDigestSuffixSQL
const runtimeManifestDigestSQL = runtimeManifestDigestPrefixSQL + `||` + runtimeResourceManifestSQL + runtimeManifestDigestSuffixSQL

func runtimeGrant(g workbench.Grant) bool {
	return g.AllowSourceRuntime || g.AllowSourceAttempt || g.AllowSourceCapability || g.AllowSourcePolicy || g.AllowSourceDestination
}

// B fields have their own authority. Filtering by an economic attribute still
// requires the matching economic grant, even when no amount would be returned.
func runtimeAuthorizationAllows(auth workbench.Authorization, filters workbench.Filters, field func(workbench.Grant) bool) bool {
	return authorizationAllows(auth, filters, func(g workbench.Grant) bool {
		return field(g) && (len(filters.CostProjects) == 0 || (g.AllowSummary && g.AllowDetails))
	})
}

func (s *WorkspaceStore) CreateRuntimeReading(ctx context.Context, subject string, request workbench.RuntimeReadingRequest, now time.Time) (workbench.RuntimeReading, error) {
	// PostgreSQL timestamps persist microseconds. Return the same UTC instants
	// that are frozen in the database, including on the initial POST response.
	period, err := request.Resolve(now)
	if err != nil {
		return workbench.RuntimeReading{}, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	period.StartInclusive = period.StartInclusive.UTC().Truncate(time.Microsecond)
	period.EndExclusive = period.EndExclusive.UTC().Truncate(time.Microsecond)
	if !period.EndExclusive.After(period.StartInclusive) {
		return workbench.RuntimeReading{}, workbench.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return workbench.RuntimeReading{}, err
	}
	defer tx.Rollback()
	auth, err := loadWorkbenchAuthorization(ctx, tx, subject)
	if err != nil {
		return workbench.RuntimeReading{}, err
	}
	if !runtimeAuthorizationAllows(auth, request.Filters, runtimeGrant) && !authorizationAllows(auth, request.Filters, contentGrant) {
		return workbench.RuntimeReading{}, workbench.ErrScopeForbidden
	}
	id, err := workbench.NewID("xrt_")
	if err != nil {
		return workbench.RuntimeReading{}, err
	}
	filters, _ := json.Marshal(request.Filters)
	coverage := workbench.RuntimeCoverage{PeriodCompleteness: "unknown", TimeBasis: "trusted_request_started", UnknownEntryTimeBasis: "authorized_observation_in_period", Streams: []workbench.RuntimeStreamObservation{}, Gaps: []string{"forward_only_capture", "timeline_completeness_unknown", "synchronous_requests_not_covered_by_lease_inventory"}}
	raw, _ := json.Marshal(coverage)
	_, err = tx.ExecContext(ctx, `INSERT INTO runtime_readings(id,owner_subject,contract_version,metric_version,period_start,period_end,display_timezone,cutoff,expires_at,filters,authorization_revision,interpretation_revision,membership_sha256,coverage)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14::jsonb)`, id, subject, workbench.RuntimeContractVersion, workbench.RuntimeMetricVersion, period.StartInclusive, period.EndExclusive, period.Timezone, now, now.Add(workbench.ReadingTTL), string(filters), auth.ObservationRevision, digestText(runtimeInterpretation), digestText(""), string(raw))
	if err != nil {
		return workbench.RuntimeReading{}, err
	}
	args := []any{id, subject, period.StartInclusive, period.EndExclusive, now, pgTextArray(request.Filters.SourceIDs), pgTextArray(request.Filters.SourceProjects), pgTextArray(request.Filters.CostProjects)}
	_, err = tx.ExecContext(ctx, runtimeVisibleSQL+`INSERT INTO runtime_reading_requests(reading_id,request_ref,ordinal,source_system_id,source_instance,org_id,project_id,request_id,entry_at,last_observed_at,observation_count)
 SELECT $1,'xrr_'||encode(sha256(convert_to(jsonb_build_array(source_system_id::text,source_instance,org_id,project_id,request_id)::text,'UTF8')),'hex'),
 row_number() OVER(ORDER BY COALESCE(entry_at,last_observed_at) DESC,source_system_id,source_instance,org_id,project_id,request_id),
 source_system_id,source_instance,org_id,project_id,request_id,entry_at,last_observed_at,observation_count FROM selected`, args...)
	if err != nil {
		return workbench.RuntimeReading{}, fmt.Errorf("freeze runtime request members: %w", err)
	}
	var known, unknown, documents int64
	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE entry_at IS NOT NULL),count(*) FILTER(WHERE entry_at IS NULL),COALESCE(sum(observation_count),0) FROM runtime_reading_requests WHERE reading_id=$1 AND observation_count>0`, id).Scan(&known, &unknown, &documents)
	if err != nil {
		return workbench.RuntimeReading{}, err
	}
	if known+unknown > workbench.MaxMembers || documents > workbench.MaxMembers {
		return workbench.RuntimeReading{}, workbench.ErrTooLarge
	}
	_, err = tx.ExecContext(ctx, runtimeMemberInsertSQL, args...)
	if err != nil {
		return workbench.RuntimeReading{}, fmt.Errorf("freeze runtime observation members: %w", err)
	}
	contentCount, err := freezeRuntimeContents(ctx, tx, args, auth)
	if err != nil {
		return workbench.RuntimeReading{}, fmt.Errorf("freeze runtime content: %w", err)
	}
	if err := freezeRuntimeAssociations(ctx, tx, id, subject, request.Filters, now); err != nil {
		return workbench.RuntimeReading{}, fmt.Errorf("freeze runtime associations: %w", err)
	}
	if err := freezeRuntimeResources(ctx, tx, id, subject, now); err != nil {
		return workbench.RuntimeReading{}, fmt.Errorf("freeze runtime resources: %w", err)
	}
	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE chain_state='missing_predecessor') FROM runtime_reading_members WHERE reading_id=$1`, id).Scan(&coverage.MissingPredecessors)
	if err != nil {
		return workbench.RuntimeReading{}, err
	}
	// Collection status is frozen separately from immutable event membership.
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT d.source_system_id::text,d.source_instance,d.org_id,d.project_id,d.profile,d.status,d.scope_generation,d.high_watermark,d.requires_attention
 FROM source_fact_streams d WHERE (cardinality($2::text[])=0 OR d.source_system_id::text=ANY($2::text[]))
 AND (cardinality($3::text[])=0 OR 'project-'||d.project_id=ANY($3::text[])) AND cardinality($4::text[])=0
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$1 AND g.active AND g.allow_coverage
 AND g.source_system_id=d.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id)
 AND g.cost_project_ref IS NULL AND `+runtimeProfileGrantSQL+`) ORDER BY 1,2,3,4,5`, subject, pgTextArray(request.Filters.SourceIDs), pgTextArray(request.Filters.SourceProjects), pgTextArray(request.Filters.CostProjects))
	if err != nil {
		return workbench.RuntimeReading{}, err
	}
	for rows.Next() {
		var stream workbench.RuntimeStreamObservation
		err = rows.Scan(&stream.SourceSystemID, &stream.SourceInstance, &stream.OrgID, &stream.ProjectID, &stream.Profile, &stream.State, &stream.ScopeGeneration, &stream.ReceivedWatermark, &stream.RequiresAttention)
		if err != nil {
			rows.Close()
			return workbench.RuntimeReading{}, err
		}
		coverage.Streams = append(coverage.Streams, stream)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return workbench.RuntimeReading{}, err
	}
	raw, _ = json.Marshal(coverage)
	_, err = tx.ExecContext(ctx, `UPDATE runtime_readings SET known_entry_requests=$2,unknown_entry_requests=$3,observation_count=$4,coverage=$5::jsonb WHERE id=$1`, id, known, unknown, documents, string(raw))
	if err != nil {
		return workbench.RuntimeReading{}, err
	}
	var digest string
	digestSQL := runtimeManifestDigestSQL
	interpretation := digestText(runtimeInterpretation)
	contentRevision, contentInterpretation := "", ""
	if contentCount > 0 {
		digestSQL = runtimeManifestDigestWithContentReceiptsSQL
		var hasReceipts bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_reading_content_refs WHERE reading_id=$1 AND receipt_id IS NOT NULL)`, id).Scan(&hasReceipts); err != nil {
			return workbench.RuntimeReading{}, err
		}
		interpretation = runtimeContentInterpretation(hasReceipts)
		contentRevision = auth.ContentRevision
		contentInterpretation = workbench.ContentInterpretationRevision
	}
	if err = tx.QueryRowContext(ctx, digestSQL, id).Scan(&digest); err != nil {
		return workbench.RuntimeReading{}, fmt.Errorf("digest runtime membership: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE runtime_readings SET membership_sha256=$2 WHERE id=$1`, id, digest); err != nil {
		return workbench.RuntimeReading{}, err
	}
	if err = commitWorkspaceTx(ctx, tx); err != nil {
		return workbench.RuntimeReading{}, err
	}
	return workbench.RuntimeReading{ID: id, ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: period, Cutoff: now, ExpiresAt: now.Add(workbench.ReadingTTL), AuthorizationRevision: auth.ObservationRevision, InterpretationRevision: interpretation, ContentAuthorizationRevision: contentRevision, ContentInterpretationRevision: contentInterpretation, MembershipRef: digest, RequestsWithEntryTime: known, RequestsWithUnknownEntryTime: unknown, ObservationCount: documents, Coverage: coverage}, nil
}

func (s *WorkspaceStore) GetRuntimeReading(ctx context.Context, subject, id string, now time.Time) (workbench.RuntimeReading, error) {
	return loadRuntimeReading(ctx, s.db, subject, id, now)
}

func loadRuntimeReading(ctx context.Context, q workspaceQueryer, subject, id string, now time.Time) (workbench.RuntimeReading, error) {
	if !workbench.ValidRuntimeReadingID(id) {
		return workbench.RuntimeReading{}, workbench.ErrNotFound
	}
	var r workbench.RuntimeReading
	var coverage []byte
	err := q.QueryRowContext(ctx, `SELECT id,contract_version,metric_version,period_start,period_end,display_timezone,cutoff,expires_at,authorization_revision,interpretation_revision,membership_sha256,known_entry_requests,unknown_entry_requests,observation_count,coverage,COALESCE(to_jsonb(runtime_readings)->>'content_authorization_revision','') FROM runtime_readings WHERE id=$1 AND owner_subject=$2`, id, subject).Scan(&r.ID, &r.ContractVersion, &r.MetricVersion, &r.Period.StartInclusive, &r.Period.EndExclusive, &r.Period.Timezone, &r.Cutoff, &r.ExpiresAt, &r.AuthorizationRevision, &r.InterpretationRevision, &r.MembershipRef, &r.RequestsWithEntryTime, &r.RequestsWithUnknownEntryTime, &r.ObservationCount, &coverage, &r.ContentAuthorizationRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return r, workbench.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.Period.StartInclusive = r.Period.StartInclusive.UTC()
	r.Period.EndExclusive = r.Period.EndExclusive.UTC()
	r.Cutoff = r.Cutoff.UTC()
	r.ExpiresAt = r.ExpiresAt.UTC()
	if !now.UTC().Before(r.ExpiresAt) {
		return workbench.RuntimeReading{}, workbench.ErrExpired
	}
	auth, err := loadWorkbenchAuthorization(ctx, q, subject)
	if err != nil {
		return workbench.RuntimeReading{}, err
	}
	if auth.ObservationRevision != r.AuthorizationRevision {
		return workbench.RuntimeReading{}, workbench.ErrAccessChanged
	}
	if r.ContentAuthorizationRevision != "" {
		if auth.ContentRevision != r.ContentAuthorizationRevision {
			return workbench.RuntimeReading{}, workbench.ErrAccessChanged
		}
		if err := validateCurrentRuntimeContentBindings(ctx, q, id); err != nil {
			return workbench.RuntimeReading{}, err
		}
		r.ContentInterpretationRevision = workbench.ContentInterpretationRevision
	}
	if r.ContractVersion != workbench.RuntimeContractVersion || r.MetricVersion != workbench.RuntimeMetricVersion ||
		(r.InterpretationRevision != digestText(runtimeInterpretation) && r.InterpretationRevision != digestText(runtimeInterpretationBeforeResources) && r.InterpretationRevision != digestText(runtimeInterpretationWithContent) && r.InterpretationRevision != digestText(runtimeInterpretationWithContentReceipts)) {
		return workbench.RuntimeReading{}, workbench.ErrUnsupportedVersion
	}
	if (r.ContentAuthorizationRevision != "") != (r.InterpretationRevision == digestText(runtimeInterpretationWithContent) || r.InterpretationRevision == digestText(runtimeInterpretationWithContentReceipts)) {
		return workbench.RuntimeReading{}, workbench.ErrUnsupportedVersion
	}
	var digest string
	digestSQL := runtimeManifestDigestSQL
	if r.ContentAuthorizationRevision != "" {
		digestSQL = runtimeManifestDigestWithContentReceiptsSQL
	}
	if err = q.QueryRowContext(ctx, digestSQL, id).Scan(&digest); err != nil {
		return workbench.RuntimeReading{}, err
	}
	if digest != r.MembershipRef {
		return workbench.RuntimeReading{}, fmt.Errorf("runtime reading membership is invalid")
	}
	if err = json.Unmarshal(coverage, &r.Coverage); err != nil {
		return workbench.RuntimeReading{}, err
	}
	return r, nil
}

func (s *WorkspaceStore) GetRuntimeRequests(ctx context.Context, subject, id, entry string, limit int, cursor string, now time.Time) (workbench.RuntimeRequests, error) {
	result := workbench.RuntimeRequests{ReadingID: id, Items: []workbench.RuntimeRequest{}}
	if _, err := s.GetRuntimeReading(ctx, subject, id, now); err != nil {
		return result, err
	}
	if limit < 1 || limit > workbench.MaxPageSize || (entry != "all" && entry != "known" && entry != "unknown") {
		return result, workbench.ErrInvalid
	}
	offset := 0
	if cursor != "" {
		var ok bool
		offset, ok = parsePageCursor(cursor, id, "runtime.requests:"+entry)
		if !ok {
			return result, workbench.ErrInvalid
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT request_ref,source_system_id::text,source_instance,org_id,project_id,request_id,entry_at,last_observed_at,observation_count,(to_jsonb(runtime_reading_requests)->>'content_eligible_at')::timestamptz FROM runtime_reading_requests WHERE reading_id=$1
 AND ($2='all' OR ($2='known' AND entry_at IS NOT NULL) OR ($2='unknown' AND entry_at IS NULL)) ORDER BY ordinal LIMIT $3 OFFSET $4`, id, entry, limit+1, offset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item workbench.RuntimeRequest
		var at, last, eligible sql.NullTime
		if err = rows.Scan(&item.Ref, &item.SourceSystemID, &item.SourceInstance, &item.OrgID, &item.ProjectID, &item.RequestID, &at, &last, &item.ObservationCount, &eligible); err != nil {
			return result, err
		}
		if last.Valid {
			item.LastObservedAt = last.Time.UTC()
		}
		if eligible.Valid {
			value := eligible.Time.UTC()
			item.ContentEligibleAt = &value
		}
		if item.ObservationCount == 0 {
			item.MemberBasis = "content_eligible"
		}
		item.EntryTimeState = "unknown"
		if at.Valid {
			item.EntryAt = &at.Time
			item.EntryTimeState = "observed"
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = pageCursor(id, "runtime.requests:"+entry, offset+limit)
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	refs := make([]string, len(result.Items))
	index := make(map[string]int, len(result.Items))
	for i := range result.Items {
		result.Items[i].OutcomeBasis = "no_request_observation"
		refs[i] = result.Items[i].Ref
		index[refs[i]] = i
	}
	if len(refs) == 0 {
		return result, nil
	}
	// Use the same request-level evidence and classifier as the frozen summary.
	// The projection is limited to this authorized page, never to all source rows.
	signals, err := s.db.QueryContext(ctx, runtimeRequestSignalsSQL, id, entry, pgTextArray(refs))
	if err != nil {
		return result, err
	}
	defer signals.Close()
	for signals.Next() {
		var ref string
		var evidence workbench.RuntimeRequestSignals
		var requestObservation bool
		if err = signals.Scan(&ref, &evidence.Completed, &evidence.WriteFailed, &evidence.TerminalProviderRejected, &evidence.AdmissionRejected, &evidence.OutputHeld, &evidence.Interrupted, &evidence.ContextDone, &evidence.MissingPredecessor, &requestObservation); err != nil {
			return result, err
		}
		if i, ok := index[ref]; ok && requestObservation {
			result.Items[i].Outcome = workbench.ClassifyRuntimeRequest(evidence)
			result.Items[i].OutcomeBasis = "request_level_observation"
		}
	}
	if err = signals.Err(); err != nil {
		return result, err
	}
	return result, nil
}

func (s *WorkspaceStore) GetRuntimeObservations(ctx context.Context, subject, id, requestRef, profile string, limit int, cursor string, now time.Time) (workbench.RuntimeObservations, error) {
	result := workbench.RuntimeObservations{ReadingID: id, Items: []workbench.RuntimeObservation{}}
	if _, err := s.GetRuntimeReading(ctx, subject, id, now); err != nil {
		return result, err
	}
	if limit < 1 || limit > workbench.MaxPageSize || (profile != "" && !sourcefacts.ValidObservationProfile(profile)) {
		return result, workbench.ErrInvalid
	}
	if requestRef != "" {
		if !workbench.ValidRuntimeRequestRef(requestRef) {
			return result, workbench.ErrNotFound
		}
		var present bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_reading_requests WHERE reading_id=$1 AND request_ref=$2)`, id, requestRef).Scan(&present); err != nil {
			return result, err
		}
		if !present {
			return result, workbench.ErrNotFound
		}
	}
	dimension := "runtime.observations:" + requestRef + ":" + profile
	offset := 0
	if cursor != "" {
		var ok bool
		offset, ok = parsePageCursor(cursor, id, dimension)
		if !ok {
			return result, workbench.ErrInvalid
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.request_ref,m.source_system_id::text,d.source_hash,d.envelope_hash,m.chain_state,d.source_bytes
 FROM runtime_reading_members m JOIN source_observation_documents d USING(source_system_id,event_id)
 WHERE m.reading_id=$1 AND ($2='' OR m.request_ref=$2) AND ($3='' OR d.profile=$3)
 ORDER BY m.source_system_id,m.event_id LIMIT $4 OFFSET $5`, id, requestRef, profile, limit+1, offset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item workbench.RuntimeObservation
		var raw []byte
		if err = rows.Scan(&item.RequestRef, &item.SourceSystemID, &item.SourceHash, &item.EnvelopeHash, &item.ChainState, &raw); err != nil {
			return result, err
		}
		if err = json.Unmarshal(raw, &item.Document); err != nil {
			return result, err
		}
		item.Representation = "authorized_projection"
		item.Document.ContextRef = ""
		item.Document.RouteEvidenceHash = ""
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = pageCursor(id, dimension, offset+limit)
	}
	return result, nil
}
