package db

import (
	"context"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

// Only these closed SQL projections may enter the query. In particular,
// attempt.provider is a response state, never a selected provider identity.
func runtimeDimensionProjection(dimension string) (profile, projection string) {
	switch dimension {
	case "operation":
		return `d.profile IN ('unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1')`, `SELECT COALESCE(d.source_json#>>'{runtime,operation}',d.source_json#>>'{attempt,operation}',d.source_json#>>'{capability,operation}') value,'not_applicable'::text evidence_state`
	case "selected_model", "selected_provider":
		field := "model_ref"
		if dimension == "selected_provider" {
			field = "provider"
		}
		return `d.profile='unirouter.capability-snapshot.v1'`, `SELECT d.source_json#>>'{capability,` + field + `}' value,d.source_json#>>'{capability,evidence_state}' evidence_state`
	case "required_capability":
		return `d.profile='unirouter.capability-snapshot.v1'`, `SELECT value,'not_applicable'::text evidence_state FROM jsonb_array_elements_text(d.source_json#>'{capability,required_capabilities}')`
	case "enabled_capability":
		return `d.profile='unirouter.capability-snapshot.v1'`, `SELECT c->>'capability_id' value,c->>'evidence_state' evidence_state FROM jsonb_array_elements(d.source_json#>'{capability,capabilities}') c WHERE c->>'support_state'='enabled'`
	case "destination_provider", "destination_origin":
		field := "provider"
		if dimension == "destination_origin" {
			field = "origin"
		}
		return `d.profile='unirouter.destination-observation.v1'`, `SELECT d.source_json#>>'{destination,` + field + `}' value,d.source_json#>>'{destination,dispatch}' evidence_state`
	}
	return "false", `SELECT NULL::text value,NULL::text evidence_state`
}

func (s *WorkspaceStore) GetRuntimeDimensions(ctx context.Context, subject, id, dimension, entry string, limit int, cursor string, now time.Time) (workbench.RuntimeDimensions, error) {
	result := workbench.RuntimeDimensions{ReadingID: id, DimensionsVersion: workbench.RuntimeDimensionsVersion, Dimension: dimension, EntryTime: entry, GroupsMayOverlap: true, Items: []workbench.RuntimeDimensionGroup{}}
	r, err := s.GetRuntimeReading(ctx, subject, id, now)
	if err != nil {
		return result, err
	}
	if !workbench.ValidRuntimeDimension(dimension) || limit < 1 || limit > workbench.MaxPageSize || (entry != "known" && entry != "unknown" && entry != "all") {
		return result, workbench.ErrInvalid
	}
	result.InterpretationRevision = r.InterpretationRevision
	offset := 0
	pageKey := "runtime.dimensions:" + dimension + ":" + entry
	if cursor != "" {
		var ok bool
		offset, ok = parsePageCursor(cursor, id, pageKey)
		if !ok {
			return result, workbench.ErrInvalid
		}
	}
	profile, projection := runtimeDimensionProjection(dimension)
	query := `WITH selected_requests AS (
 SELECT request_ref FROM runtime_reading_requests WHERE reading_id=$1 AND observation_count>0
 AND ($2='all' OR ($2='known' AND entry_at IS NOT NULL) OR ($2='unknown' AND entry_at IS NULL))
), facts AS (
 SELECT m.request_ref,m.chain_state,d.* FROM selected_requests q
 JOIN runtime_reading_members m ON m.reading_id=$1 AND m.request_ref=q.request_ref
 JOIN source_observation_documents d USING(source_system_id,event_id) WHERE ` + profile + `
), dimension_values AS (
 SELECT DISTINCT d.request_ref,d.source_system_id,d.event_id,d.subject_kind,d.subject_id,v.value,v.evidence_state
 FROM facts d CROSS JOIN LATERAL (` + projection + `) v
 WHERE d.chain_state='complete' AND v.value IS NOT NULL AND v.value<>'' AND v.value<>'not_available'
) `
	err = s.db.QueryRowContext(ctx, query+`SELECT (SELECT count(*) FROM selected_requests),
 (SELECT count(DISTINCT request_ref) FROM dimension_values),
 (SELECT count(*) FROM facts WHERE chain_state<>'complete')`, id, entry).Scan(&result.Requests, &result.RequestsWithValues, &result.MissingPredecessors)
	if err != nil {
		return result, err
	}
	result.RequestsWithoutValues = result.Requests - result.RequestsWithValues
	rows, err := s.db.QueryContext(ctx, query+`SELECT value,evidence_state,count(DISTINCT request_ref),
 count(DISTINCT (request_ref,subject_kind,subject_id)),count(*) FROM dimension_values
 GROUP BY value,evidence_state ORDER BY value COLLATE "C",evidence_state COLLATE "C" LIMIT $3 OFFSET $4`, id, entry, limit+1, offset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item workbench.RuntimeDimensionGroup
		if err := rows.Scan(&item.Value, &item.EvidenceState, &item.Requests, &item.Subjects, &item.ObservationCount); err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = pageCursor(id, pageKey, offset+limit)
	}
	return result, nil
}
