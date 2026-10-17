package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

// Each page is a live authorized keyset view. It is deliberately independent
// of historical period membership and is never a global simultaneous count.
const runtimeCurrentSQL = `WITH current_entries AS (
 SELECT s.source_system_id,s.source_instance,s.org_id,s.project_id,e.value->>'request_id' request_id,
 e.value entry,p.source_sha256,p.observed_at,p.received_at,
 (s.status='active' AND (NOT s.requires_attention OR s.last_error IN ('cursor_expired','cursor_or_epoch_conflict'))
  AND NOT c.requires_attention AND c.last_error='') ready
 FROM source_fact_streams s JOIN source_runtime_snapshots c ON c.stream_id=s.id
 JOIN source_runtime_snapshot_pages p ON p.stream_id=s.id AND p.generation=c.visible_generation
 CROSS JOIN LATERAL jsonb_array_elements(p.source_json->'entries') e
 WHERE s.profile='unirouter.runtime-observation.v1'
), visible AS (
 SELECT d.*,'xrc_'||encode(sha256(convert_to(jsonb_build_array(d.source_system_id::text,d.source_instance,d.org_id,d.project_id,d.request_id,d.entry->>'family',d.entry->>'subject_kind',d.entry->>'subject_id')::text,'UTF8')),'hex') ref
 FROM current_entries d LEFT JOIN workbench_economic_facts f ON ` + sourceEconomicJoinSQL + `
 WHERE (cardinality($2::text[])=0 OR d.source_system_id::text=ANY($2::text[]))
 AND (cardinality($3::text[])=0 OR 'project-'||d.project_id=ANY($3::text[]))
 AND (cardinality($4::text[])=0 OR f.cost_project_ref=ANY($4::text[]))
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$1 AND g.active AND g.allow_source_runtime
 AND (cardinality($4::text[])=0 OR (g.allow_summary AND g.allow_details))
 AND g.source_system_id=d.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id)
 AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref))
), latest AS (
 SELECT DISTINCT ON(ref) * FROM visible ORDER BY ref,ready DESC,observed_at DESC,received_at DESC,entry::text
) SELECT ref,source_system_id::text,source_instance,org_id,project_id,observed_at,received_at,source_sha256,ready,entry
 FROM latest WHERE ref>$5 ORDER BY ref LIMIT $6`

type runtimeCurrentCursor struct {
	Scope string `json:"scope"`
	After string `json:"after"`
}

func (s *WorkspaceStore) GetRuntimeCurrent(ctx context.Context, subject string, filters workbench.Filters, limit int, cursor string, now time.Time) (workbench.RuntimeCurrentInventory, error) {
	result := workbench.RuntimeCurrentInventory{Version: workbench.RuntimeCurrentVersion, EvaluatedAt: now.UTC(), Coverage: "lease_backed_inventory", Uncovered: []string{"synchronous_requests_without_execution_lease", "progress_between_lease_observations", "pages_are_independently_observed"}, Items: []workbench.RuntimeCurrentItem{}}
	if err := workbench.ValidateRuntimeCurrentQuery(filters, limit); err != nil {
		return result, err
	}
	auth, err := loadWorkbenchAuthorization(ctx, s.db, subject)
	if err != nil {
		return result, err
	}
	if !runtimeAuthorizationAllows(auth, filters, func(g workbench.Grant) bool { return g.AllowSourceRuntime }) {
		return result, workbench.ErrScopeForbidden
	}
	binding, _ := json.Marshal([]any{workbench.RuntimeCurrentVersion, subject, auth.ObservationRevision, filters})
	position := runtimeCurrentCursor{Scope: digestText(string(binding))}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		var previous runtimeCurrentCursor
		if err != nil || len(raw) > 1024 || json.Unmarshal(raw, &previous) != nil || previous.Scope != position.Scope || len(previous.After) != 68 || previous.After[:4] != "xrc_" {
			return result, workbench.ErrInvalid
		}
		position = previous
	}
	rows, err := s.db.QueryContext(ctx, runtimeCurrentSQL, subject, pgTextArray(filters.SourceIDs), pgTextArray(filters.SourceProjects), pgTextArray(filters.CostProjects), position.After, limit+1)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item workbench.RuntimeCurrentItem
		var raw []byte
		var ready bool
		if err = rows.Scan(&item.Ref, &item.SourceSystemID, &item.SourceInstance, &item.OrgID, &item.ProjectID, &item.SnapshotObservedAt, &item.ReceivedAt, &item.SourceSHA256, &ready, &raw); err != nil {
			return result, err
		}
		if err = json.Unmarshal(raw, &item.Entry); err != nil {
			return result, err
		}
		item.CurrentState = workbench.RuntimeCurrentState(item.Entry, item.SnapshotObservedAt, item.ReceivedAt, now, ready)
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		position.After = result.Items[limit-1].Ref
		raw, _ := json.Marshal(position)
		result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return result, nil
}
