package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/foigent/x-insight/internal/workchain"
)

// Both arguments are internally generated SQL expressions/placeholders, never
// request text. This predicate precedes LIMIT and aggregate construction.
func scopedExecutionSQL(execution, subject string) string {
	return fmt.Sprintf(`EXISTS (
 SELECT 1 FROM workbench_economic_facts sf
 JOIN evidence_events anchor ON anchor.id=sf.evidence_event_id
 WHERE sf.execution_id=%[1]s AND anchor.execution_id=sf.execution_id
 AND anchor.source_system_id=sf.source_system_id AND anchor.event_hash=sf.event_hash
 AND COALESCE(anchor.project_ref,'')=COALESCE(sf.source_project_ref,'')
 AND EXISTS (SELECT 1 FROM workbench_access_grants sg
   WHERE sg.subject_id=%[2]s AND sg.active AND sg.allow_details
   AND sg.source_system_id=sf.source_system_id
   AND (sg.source_project_ref IS NULL OR sg.source_project_ref=sf.source_project_ref)
   AND (sg.cost_project_ref IS NULL OR sg.cost_project_ref=sf.cost_project_ref))
 AND NOT EXISTS (SELECT 1 FROM evidence_events member WHERE member.execution_id=sf.execution_id
   AND (member.source_system_id<>sf.source_system_id OR COALESCE(member.project_ref,'')<>COALESCE(sf.source_project_ref,'')))
 AND NOT EXISTS (SELECT 1 FROM workbench_economic_facts other WHERE other.execution_id=sf.execution_id
   AND (other.source_system_id<>sf.source_system_id OR COALESCE(other.source_project_ref,'')<>COALESCE(sf.source_project_ref,'')
        OR COALESCE(other.cost_project_ref,'')<>COALESCE(sf.cost_project_ref,'')))
 AND NOT EXISTS (SELECT 1 FROM capture_gaps cg WHERE cg.source_system_id=sf.source_system_id AND cg.request_id=sf.request_id
   AND NOT EXISTS (SELECT 1 FROM evidence_events ge WHERE ge.id=cg.evidence_event_id AND ge.execution_id=sf.execution_id)
   AND NOT EXISTS (SELECT 1 FROM workbench_access_grants whole_source WHERE whole_source.subject_id=%[2]s
     AND whole_source.active AND whole_source.allow_details AND whole_source.source_system_id=sf.source_system_id
     AND whole_source.source_project_ref IS NULL AND whole_source.cost_project_ref IS NULL))
)`, execution, subject)
}

func scopedExecutionRequirement(ctx context.Context, q workspaceQueryer, executionID string) (workchain.Requirement, error) {
	a := workchain.FromContext(ctx)
	if a == nil {
		return workchain.Requirement{}, workchain.ErrDenied
	}
	var r workchain.Requirement
	err := q.QueryRowContext(ctx, `SELECT f.source_system_id::text,COALESCE(f.source_project_ref,''),COALESCE(f.cost_project_ref,'') FROM workbench_economic_facts f WHERE f.execution_id=$1 AND `+scopedExecutionSQL("f.execution_id", "$2")+` ORDER BY f.id LIMIT 1`, executionID, a.Subject).Scan(&r.Source, &r.Project, &r.CostProject)
	if errors.Is(err, sql.ErrNoRows) {
		return r, workchain.ErrDenied
	}
	if err != nil {
		return r, err
	}
	var allowed bool
	r.Fields, allowed = a.FieldsFor(r.Source, r.Project, r.CostProject)
	if !allowed {
		return r, workchain.ErrDenied
	}
	return r, nil
}

func (s *EvidenceStore) AuthorizeScopedExecution(ctx context.Context, executionID string) (workchain.Requirement, error) {
	return scopedExecutionRequirement(ctx, s.db, executionID)
}

func (s *EvidenceStore) checkScopedExecution(ctx context.Context, executionID string) error {
	if workchain.FromContext(ctx) == nil {
		return nil
	}
	_, err := s.AuthorizeScopedExecution(ctx, executionID)
	if errors.Is(err, workchain.ErrDenied) {
		return ErrExecutionNotFound
	}
	return err
}
