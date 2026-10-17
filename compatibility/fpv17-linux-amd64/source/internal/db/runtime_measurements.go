package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

const runtimeMeasurementEligibility = "xinsight.runtime.measurement-eligibility.v1"

// Context comes only from the same authorized, frozen request and subject.
// Missing field grants leave context unknown; kind names do not invent it.
const runtimeMeasurementGroupsSQL = `WITH members AS MATERIALIZED (
 SELECT m.request_ref,m.chain_state,d.subject_kind,d.subject_id,d.source_json,
 CASE WHEN q.entry_at IS NULL THEN 'unknown' ELSE 'observed' END entry_time_state
 FROM runtime_reading_members m JOIN source_observation_documents d USING(source_system_id,event_id)
 JOIN runtime_reading_requests q ON q.reading_id=m.reading_id AND q.request_ref=m.request_ref
 WHERE m.reading_id=$1 AND ($2='all' OR ($2='known' AND q.entry_at IS NOT NULL) OR ($2='unknown' AND q.entry_at IS NULL))
), subjects AS (
 SELECT request_ref,subject_kind,subject_id,
 array_remove(array_agg(DISTINCT NULLIF(COALESCE(source_json#>>'{runtime,operation}',source_json#>>'{attempt,operation}'),'not_available')),NULL) operations
 FROM members WHERE chain_state='complete' GROUP BY request_ref,subject_kind,subject_id
), requests AS (
 SELECT request_ref,array_remove(array_agg(DISTINCT NULLIF(source_json#>>'{runtime,stream_mode}','not_observed')),NULL) modes
 FROM members WHERE chain_state='complete' AND subject_kind='request' GROUP BY request_ref
), measurements AS (
 SELECT m.chain_state,m.entry_time_state,m.source_json->'measurement' measurement,
 CASE COALESCE(cardinality(s.operations),0) WHEN 0 THEN 'not_observed' WHEN 1 THEN s.operations[1] ELSE 'conflicting' END operation,
 CASE COALESCE(cardinality(r.modes),0) WHEN 0 THEN 'not_observed' WHEN 1 THEN r.modes[1] ELSE 'conflicting' END stream_mode
 FROM members m LEFT JOIN subjects s USING(request_ref,subject_kind,subject_id)
 LEFT JOIN requests r USING(request_ref) WHERE m.source_json ? 'measurement'
), classified AS (
 SELECT *,measurement->>'kind' kind,measurement->>'version' version,measurement->>'end_boundary' completion_evidence,
 CASE WHEN chain_state<>'complete' THEN 'missing_predecessor'
 WHEN measurement->>'state'<>'observed' THEN measurement->>'state'
 WHEN operation='conflicting' OR stream_mode='conflicting' THEN 'conflicting_context'
 WHEN operation='not_observed' OR stream_mode='not_observed' THEN 'missing_context'
 WHEN (measurement->>'kind'='request_response_complete' AND stream_mode<>'non_streaming')
 OR (measurement->>'kind' IN ('stream_first_valid_output','stream_complete') AND stream_mode<>'streaming')
 THEN 'incompatible_context' ELSE '' END exclusion
 FROM measurements
)
SELECT operation,stream_mode,kind,version,completion_evidence,entry_time_state,count(*),
 count(*) FILTER(WHERE exclusion='not_applicable'),count(*) FILTER(WHERE exclusion='not_observed'),
 count(*) FILTER(WHERE exclusion='legacy_unknown'),count(*) FILTER(WHERE exclusion='invalid'),
 count(*) FILTER(WHERE exclusion='missing_predecessor'),count(*) FILTER(WHERE exclusion='missing_context'),
 count(*) FILTER(WHERE exclusion='conflicting_context'),count(*) FILTER(WHERE exclusion='incompatible_context'),
 COALESCE(json_agg(measurement->>'value') FILTER(WHERE exclusion=''),'[]'::json)
FROM classified GROUP BY operation,stream_mode,kind,version,completion_evidence,entry_time_state
ORDER BY operation,stream_mode,kind,version,completion_evidence,entry_time_state LIMIT $3 OFFSET $4`

func (s *WorkspaceStore) GetRuntimeMeasurements(ctx context.Context, subject, id, entry string, limit int, cursor string, now time.Time) (workbench.RuntimeMeasurements, error) {
	result := workbench.RuntimeMeasurements{ReadingID: id, EligibilityVersion: runtimeMeasurementEligibility, Items: []workbench.RuntimeMeasurementGroup{}}
	r, err := s.GetRuntimeReading(ctx, subject, id, now)
	if err != nil {
		return result, err
	}
	result.InterpretationRevision = r.InterpretationRevision
	if limit < 1 || limit > workbench.MaxPageSize || (entry != "all" && entry != "known" && entry != "unknown") {
		return result, workbench.ErrInvalid
	}
	offset := 0
	if cursor != "" {
		var ok bool
		offset, ok = parsePageCursor(cursor, id, "runtime.measurements:"+entry)
		if !ok {
			return result, workbench.ErrInvalid
		}
	}
	rows, err := s.db.QueryContext(ctx, runtimeMeasurementGroupsSQL, id, entry, limit+1, offset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var group workbench.RuntimeMeasurementGroup
		var raw []byte
		e := &group.Excluded
		if err := rows.Scan(&group.Operation, &group.StreamMode, &group.Kind, &group.SourceVersion, &group.CompletionEvidence, &group.EntryTimeState, &group.ObservationCount,
			&e.NotApplicable, &e.NotObserved, &e.LegacyUnknown, &e.Invalid, &e.MissingPredecessor, &e.MissingContext, &e.ConflictingContext, &e.IncompatibleContext, &raw); err != nil {
			return result, err
		}
		var values []string
		if err := json.Unmarshal(raw, &values); err != nil {
			return result, err
		}
		group.Percentiles, err = workbench.ComputeRuntimePercentiles(values)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, group)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = pageCursor(id, "runtime.measurements:"+entry, offset+limit)
	}
	return result, nil
}
