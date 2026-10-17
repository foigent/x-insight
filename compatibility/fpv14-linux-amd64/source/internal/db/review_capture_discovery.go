package db

import "context"

type ReviewCaptureProjectChoice struct {
	SourceSystemID string `json:"source_system_id"`
	SourceInstance string `json:"source_instance"`
	OrgID          string `json:"org_id"`
	ProjectID      string `json:"project_id"`
	SourceName     string `json:"source_name"`
	SourceType     string `json:"source_type"`
	XIProjectID    string `json:"xi_project_id,omitempty"`
	XIProjectName  string `json:"xi_project_name,omitempty"`
}

type ReviewCaptureXIProject struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type ReviewCaptureProjectCatalog struct {
	Items    []ReviewCaptureProjectChoice `json:"items"`
	Projects []ReviewCaptureXIProject     `json:"projects"`
}

// Only already-registered active source scopes appear as choices. The exact
// source tuple and existing XI binding are read together; a name or number is
// never offered as an implicit cross-source project match.
func (s *WorkspaceStore) ListReviewCaptureProjects(ctx context.Context) (ReviewCaptureProjectCatalog, error) {
	result := ReviewCaptureProjectCatalog{Items: []ReviewCaptureProjectChoice{}, Projects: []ReviewCaptureXIProject{}}
	rows, err := s.db.QueryContext(ctx, `WITH scopes AS (
 SELECT source_system_id,source_instance,org_id,project_id FROM work_event_collections WHERE status='active'
 UNION SELECT source_system_id,source_instance,org_id,project_id FROM source_content_collections WHERE status='active'
 ) SELECT q.source_system_id::text,q.source_instance,q.org_id,q.project_id,s.display_name,s.source_type,
 COALESCE(b.xi_project_id::text,''),COALESCE(p.display_name,'')
 FROM scopes q JOIN source_systems s ON s.id=q.source_system_id AND s.status='active'
 LEFT JOIN review_capture_project_bindings b ON b.source_system_id=q.source_system_id AND
 b.source_instance=q.source_instance AND b.org_id=q.org_id AND b.project_id=q.project_id
 LEFT JOIN review_capture_projects p ON p.xi_project_id=b.xi_project_id
 ORDER BY s.display_name,q.source_system_id,q.source_instance,q.org_id,q.project_id`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ReviewCaptureProjectChoice
		if err = rows.Scan(&item.SourceSystemID, &item.SourceInstance, &item.OrgID, &item.ProjectID,
			&item.SourceName, &item.SourceType, &item.XIProjectID, &item.XIProjectName); err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	projects, err := s.db.QueryContext(ctx, `SELECT xi_project_id::text,display_name FROM review_capture_projects
 ORDER BY display_name,xi_project_id`)
	if err != nil {
		return result, err
	}
	defer projects.Close()
	for projects.Next() {
		var item ReviewCaptureXIProject
		if err = projects.Scan(&item.ID, &item.DisplayName); err != nil {
			return result, err
		}
		result.Projects = append(result.Projects, item)
	}
	return result, projects.Err()
}
