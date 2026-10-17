package db

import (
	"context"
	"database/sql"
	"github.com/foigent/x-insight/internal/workbench"
)

func (s *WorkspaceStore) loadSourceFactsStatuses(ctx context.Context, subject string, filters workbench.Filters) (map[string][]workbench.SourceFactsStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.source_system_id::text,s.id::text,s.profile,s.source_instance,s.org_id,s.project_id,s.scope_generation,s.status,s.requires_attention,s.last_error,s.last_received_at
 FROM source_fact_streams s WHERE
 (cardinality($2::text[])=0 OR s.source_system_id::text=ANY($2::text[]))
 AND (cardinality($3::text[])=0 OR 'project-'||s.project_id=ANY($3::text[]))
 AND cardinality($4::text[])=0
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$1 AND g.active AND g.allow_coverage
  AND g.source_system_id=s.source_system_id AND g.cost_project_ref IS NULL
  AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||s.project_id)
  AND ((s.profile='unirouter.execution-context.v1' AND g.allow_source_context)
    OR (s.profile='unirouter.execution-identity.v1' AND g.allow_source_identity)
    OR (s.profile='unirouter.cost-basis.v1' AND g.allow_source_cost)))
 ORDER BY s.source_system_id,s.profile,s.scope_generation::bigint,s.id`, subject, pgTextArray(filters.SourceIDs), pgTextArray(filters.SourceProjects), pgTextArray(filters.CostProjects))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]workbench.SourceFactsStatus{}
	for rows.Next() {
		var id string
		var v workbench.SourceFactsStatus
		var received sql.NullTime
		if err = rows.Scan(&id, &v.StreamID, &v.Profile, &v.SourceInstance, &v.OrgID, &v.ProjectID, &v.ScopeGeneration, &v.Status, &v.RequiresAttention, &v.LastError, &received); err != nil {
			return nil, err
		}
		if received.Valid {
			at := received.Time.UTC()
			v.LastReceivedAt = &at
		}
		result[id] = append(result[id], v)
	}
	return result, rows.Err()
}
