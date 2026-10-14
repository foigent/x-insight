package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

// All queries use the caller's transaction and the outer workchain grant lock.
// No creation timestamp supplied by a caller is used to test a reading's TTL.
func resolveCreationExecutionContext(ctx context.Context, tx *sql.Tx, c workspace.ReviewCreationCommand, basis workspace.ReviewBasis) (workspace.ReviewBasis, error) {
	if c.ExecutionContext == nil {
		return basis, nil
	}
	a := workchain.FromContext(ctx)
	if a == nil || !a.Reviewer {
		return basis, workspace.ErrNotFound
	}
	input := *c.ExecutionContext
	var now time.Time
	if err := tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return basis, err
	}
	cost, err := loadValidWorkbenchReading(ctx, tx, a.Subject, input.WorkbenchReadingID, now)
	if err != nil {
		return basis, executionReadingError(err)
	}
	runtime, err := loadRuntimeReading(ctx, tx, a.Subject, input.RuntimeReadingID, now)
	if err != nil {
		return basis, executionReadingError(err)
	}
	if !cost.Cutoff.Equal(input.ExecutionAsOf) {
		return basis, workspace.ErrConflict
	}
	if _, err = scopedExecutionRequirement(ctx, tx, input.ExecutionID); err != nil {
		return basis, executionReadingError(err)
	}
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count(DISTINCT m.request_ref) FROM runtime_reading_economic_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE m.reading_id=$1 AND f.execution_id=$2`, input.RuntimeReadingID, input.ExecutionID).Scan(&count)
	if err != nil {
		return basis, err
	}
	if count != 1 {
		return basis, workspace.ErrConflict
	}
	var target workbench.ResourceScope
	var request string
	err = tx.QueryRowContext(ctx, `SELECT DISTINCT q.source_system_id::text,q.source_instance,q.org_id,q.project_id,q.request_id
 FROM runtime_reading_requests q JOIN runtime_reading_economic_members m ON m.reading_id=q.reading_id AND m.request_ref=q.request_ref
 JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
 JOIN workbench_reading_members cm ON cm.economic_fact_id=f.id AND cm.reading_id=$3
 JOIN evidence_events e ON e.id=f.evidence_event_id AND e.execution_id=f.execution_id AND e.event_hash=f.event_hash AND e.source_system_id=f.source_system_id AND e.request_id=f.request_id
 WHERE q.reading_id=$1 AND q.request_ref=$2 AND f.execution_id=$4
 AND f.source_system_id=q.source_system_id AND f.source_version='unirouter.completed-execution.v1'
 AND f.source_ref='unirouter:'||q.source_instance||':completed_execution:'||q.request_id
 AND f.request_id=q.request_id AND f.source_project_ref='project-'||q.project_id AND e.org_ref='org-'||q.org_id`, input.RuntimeReadingID, input.RuntimeRequestRef, input.WorkbenchReadingID, input.ExecutionID).Scan(&target.SourceSystemID, &target.SourceInstance, &target.OrgID, &target.ProjectID, &request)
	if errors.Is(err, sql.ErrNoRows) {
		return basis, workspace.ErrNotFound
	}
	if err != nil {
		return basis, err
	}
	for _, ref := range basis.Selected.Resources {
		if ref.Target != target || ref.RequestID != request {
			return basis, workspace.ErrConflict
		}
		if _, err = resourceRequirements(ctx, tx, ref); err != nil {
			return basis, err
		}
		if _, err = readResourceChoice(ctx, tx, ref); err != nil {
			return basis, err
		}
		var present bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_reading_resource_members m JOIN resource_access_records d USING(source_system_id,source_instance,org_id,project_id,record_ref,revision)
 WHERE m.reading_id=$1 AND m.request_ref=$2 AND m.source_system_id=$3::uuid AND m.source_instance=$4 AND m.org_id=$5 AND m.project_id=$6 AND m.record_ref=$7 AND m.revision=$8 AND d.record_hash=$9)`, input.RuntimeReadingID, input.RuntimeRequestRef, ref.Reporter.SourceSystemID, ref.Reporter.SourceInstance, ref.Reporter.OrgID, ref.Reporter.ProjectID, ref.RecordRef, ref.Revision, ref.RecordHash).Scan(&present)
		if err != nil {
			return basis, err
		}
		if !present {
			return basis, workspace.ErrConflict
		}
	}
	for _, choice := range basis.Selected.Contents {
		ref := choice.Reference
		if ref.SourceSystemID != target.SourceSystemID || ref.SourceInstance != target.SourceInstance || ref.OrgID != target.OrgID || ref.ProjectID != target.ProjectID || ref.RequestID != request {
			return basis, workspace.ErrConflict
		}
		if _, err = contentRequirements(ctx, tx, choice); err != nil {
			return basis, err
		}
		expires, e := time.Parse(sourcefacts.TimeFormat, ref.ExpiresAt)
		if e != nil || !now.Before(expires) {
			return basis, workbench.ErrContentExpired
		}
		var raw []byte
		err = tx.QueryRowContext(ctx, `SELECT frozen_ref FROM runtime_reading_content_refs WHERE reading_id=$1 AND request_ref=$2 AND source_system_id=$3::uuid AND source_fact_id=$4 AND revision=$5`, input.RuntimeReadingID, input.RuntimeRequestRef, ref.SourceSystemID, ref.SourceFactID, ref.Revision).Scan(&raw)
		if err != nil {
			return basis, workspaceError(err)
		}
		var frozen sourcefacts.FrozenManifestRef
		if json.Unmarshal(raw, &frozen) != nil || !workspace.SameAssessmentValue(frozen, ref) {
			return basis, workspace.ErrConflict
		}
	}
	basis.ResolvedExecutionContext = &workspace.ResolvedExecutionContext{Context: input, RuntimeCutoff: runtime.Cutoff, WorkbenchMembershipRef: cost.MembershipRef, RuntimeMembershipRef: runtime.MembershipRef, Target: target, RequestID: request, Resources: append([]workspace.ResourceChoice{}, basis.Selected.Resources...), Contents: append([]workspace.ContentChoice(nil), basis.Selected.Contents...)}
	hasContent := len(basis.ResolvedExecutionContext.Contents) > 0
	if !hasContent && c.CaseID != "" {
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM case_content_links WHERE case_id=$1)`, c.CaseID).Scan(&hasContent); err != nil {
			return basis, err
		}
	}
	if _, _, err = scopedCaseExecutionRequirement(ctx, tx, c.CaseID, input.ExecutionID, hasContent, basis.ResolvedExecutionContext); err != nil {
		return basis, executionReadingError(err)
	}
	return workspace.CanonicalReviewBasis(basis)
}
func executionReadingError(err error) error {
	if errors.Is(err, workbench.ErrNotFound) || errors.Is(err, workbench.ErrScopeForbidden) || errors.Is(err, workchain.ErrDenied) {
		return workspace.ErrNotFound
	}
	if errors.Is(err, workbench.ErrExpired) || errors.Is(err, workbench.ErrAccessChanged) || errors.Is(err, workbench.ErrUnsupportedVersion) || errors.Is(err, workbench.ErrConflict) {
		return workspace.ErrConflict
	}
	return err
}
func checkExecutionContextDeadline(ctx context.Context, tx *sql.Tx, c *workspace.ExecutionContext, contents ...workspace.ContentChoice) error {
	if c == nil {
		return nil
	}
	a := workchain.FromContext(ctx)
	if a == nil {
		return workspace.ErrNotFound
	}
	var live bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workbench_readings c CROSS JOIN runtime_readings r WHERE c.id=$1 AND r.id=$2 AND c.owner_subject=$3 AND r.owner_subject=$3 AND c.expires_at>clock_timestamp() AND r.expires_at>clock_timestamp())`, c.WorkbenchReadingID, c.RuntimeReadingID, a.Subject).Scan(&live); err != nil {
		return err
	}
	if !live {
		return workspace.ErrConflict
	}
	if len(contents) > 0 {
		var now time.Time
		if err := tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			return err
		}
		for _, content := range contents {
			expires, err := time.Parse(sourcefacts.TimeFormat, content.Reference.ExpiresAt)
			if err != nil || !now.Before(expires) {
				return workbench.ErrContentExpired
			}
		}
	}
	return nil
}
func insertReviewExecution(ctx context.Context, tx *sql.Tx, caseID string, actor workspace.Actor, b workspace.ReviewBasis, now time.Time) error {
	if b.ResolvedExecutionContext == nil {
		return nil
	}
	c := b.ResolvedExecutionContext.Context
	_, err := tx.ExecContext(ctx, `INSERT INTO case_executions(case_id,execution_id,as_of,added_by_subject,added_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(case_id,execution_id,as_of) DO NOTHING`, caseID, c.ExecutionID, c.ExecutionAsOf, actor.Ref, now)
	return err
}

// Historical validation does not join either reading table. Online use also
// checks current resource/execution access, under the caller's held scope lock.
func validateDurableExecutionContext(ctx context.Context, q workspaceQueryer, caseID, findingID string, v *workspace.ResolvedExecutionContext, online bool) error {
	if v == nil {
		return nil
	}
	c := v.Context
	var present bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM case_executions WHERE case_id=$1 AND execution_id=$2 AND as_of=$3)`, caseID, c.ExecutionID, c.ExecutionAsOf).Scan(&present); err != nil {
		return err
	}
	if !present {
		return workspace.ErrConflict
	}
	if online {
		if _, err := scopedExecutionRequirement(ctx, q, c.ExecutionID); err != nil {
			return executionReadingError(err)
		}
	}
	for _, ref := range v.Resources {
		var raw []byte
		args := append(resourceIdentityArgs(ref), caseID, findingID, ref.Relation)
		if err := q.QueryRowContext(ctx, `SELECT reference_snapshot FROM case_resource_links WHERE `+resourceIdentityWhere+` AND case_id=$7 AND finding_id=$8 AND relation=$9`, args...).Scan(&raw); err != nil {
			return workspaceError(err)
		}
		var stored workspace.ResourceChoice
		if json.Unmarshal(raw, &stored) != nil || stored != ref {
			return workspace.ErrConflict
		}
		if online {
			if _, err := resourceRequirements(ctx, q, ref); err != nil {
				return err
			}
		}
		if _, err := readResourceChoice(ctx, q, ref); err != nil {
			return err
		}
	}
	for _, content := range v.Contents {
		var raw []byte
		if err := q.QueryRowContext(ctx, `SELECT reference_snapshot FROM case_content_links WHERE case_id=$1 AND finding_id=$2 AND choice_sha256=$3`, caseID, findingID, sourcefacts.Hash([]byte(workspace.ContentChoiceKey(content)))).Scan(&raw); err != nil {
			return workspaceError(err)
		}
		var stored workspace.ContentChoice
		if json.Unmarshal(raw, &stored) != nil || !workspace.SameAssessmentValue(stored, content) {
			return workspace.ErrConflict
		}
		if online {
			if _, err := contentRequirements(ctx, q, content); err != nil {
				return err
			}
		}
		if err := readContentChoice(ctx, q, content); err != nil {
			return err
		}
	}
	return nil
}
func persistedFindingExecutionContext(ctx context.Context, q workspaceQueryer, caseID, findingID string) (*workspace.ResolvedExecutionContext, error) {
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT receipt FROM finding_review_creations WHERE case_id=$1 AND finding_id=$2`, caseID, findingID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipt workspace.ReviewCreationReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	v := receipt.Basis.ResolvedExecutionContext
	if (receipt.Command.ExecutionContext == nil) != (v == nil) {
		return nil, workspace.ErrConflict
	}
	if v == nil {
		return nil, nil
	}
	if !workspace.ValidReviewBasis(receipt.Basis) || !workspace.SameAssessmentValue(receipt.Command.ExecutionContext, &v.Context) || !workspace.ExecutionContextSelectionMatches(v, receipt.Command.Selection) {
		return nil, workspace.ErrConflict
	}
	if err = validateDurableExecutionContext(ctx, q, caseID, findingID, v, true); err != nil {
		return nil, err
	}
	return v, nil
}

func validateExecutionContextHistory(ctx context.Context, q workspaceQueryer, caseID string, snapshot *workspace.AssessmentSnapshot) error {
	// Each origin is validated against the retained database links. The pure pack
	// validator additionally enforces continuity across all actions/projections.
	pack := workspace.EvidencePack{Assessments: snapshot}
	links, err := loadResourceLinks(ctx, q, []string{caseID})
	if err != nil {
		return err
	}
	pack.Resources = links
	pack.Contents, err = loadContentLinks(ctx, q, []string{caseID})
	if err != nil {
		return err
	}
	for _, r := range snapshot.Creations {
		v := r.Basis.ResolvedExecutionContext
		if v == nil {
			continue
		}
		if err := validateDurableExecutionContext(ctx, q, caseID, r.FindingID, v, false); err != nil {
			return err
		}
		pack.Executions = append(pack.Executions, workspace.CaseExecution{ExecutionID: v.Context.ExecutionID, AsOf: v.Context.ExecutionAsOf})
	}
	return workspace.ValidateExecutionContextPack(pack)
}

// Preview and the final case refresh apply the same execution classification.
// pending is supplied only after its reading, selected references and grants
// have been validated; it models the exact execution link that Create will add.
func scopedCaseExecutionRequirement(ctx context.Context, q workspaceQueryer, caseID, executionID string, hasContent bool, pending *workspace.ResolvedExecutionContext) (workchain.Requirement, bool, error) {
	req, err := scopedExecutionRequirement(ctx, q, executionID)
	if err != nil {
		return req, false, err
	}
	if hasContent {
		covered, err := contentContextCoversExecutionLinks(ctx, q, caseID, executionID, pending)
		if err != nil || covered {
			return req, covered, err
		}
	}
	if !req.Fields.Complete() {
		return req, false, workspace.ErrNotFound
	}
	return req, false, nil
}

// Only exact validated content origins may avoid the legacy full-B execution
// classification. Every existing and proposed as_of link must be covered.
// Durable origins consult current grants, not navigation readings or source TTL.
func contentContextCoversExecutionLinks(ctx context.Context, q workspaceQueryer, caseID, executionID string, pending *workspace.ResolvedExecutionContext) (bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT finding_id FROM finding_review_creations WHERE case_id=$1 AND receipt #>> '{basis,resolved_execution_context,context,execution_id}'=$2 AND receipt #> '{basis,resolved_execution_context,contents,0}' IS NOT NULL`, caseID, executionID)
	if err != nil {
		return false, err
	}
	var findings []string
	for rows.Next() {
		var finding string
		if err = rows.Scan(&finding); err != nil {
			rows.Close()
			return false, err
		}
		findings = append(findings, finding)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || (len(findings) == 0 && (pending == nil || len(pending.Contents) == 0)) {
		return false, err
	}
	var origins []time.Time
	for _, finding := range findings {
		v, err := persistedFindingExecutionContext(ctx, q, caseID, finding)
		if err != nil {
			return false, err
		}
		if v == nil || len(v.Contents) == 0 || v.Context.ExecutionID != executionID {
			return false, workspace.ErrConflict
		}
		origins = append(origins, v.Context.ExecutionAsOf)
	}
	rows, err = q.QueryContext(ctx, `SELECT as_of FROM case_executions WHERE case_id=$1 AND execution_id=$2`, caseID, executionID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var links []time.Time
	for rows.Next() {
		var asOf time.Time
		if err = rows.Scan(&asOf); err != nil {
			return false, err
		}
		links = append(links, asOf)
	}
	if err = rows.Err(); err != nil {
		return false, err
	}
	return executionContentOriginsCover(executionID, links, origins, pending), nil
}

func executionContentOriginsCover(executionID string, links, origins []time.Time, pending *workspace.ResolvedExecutionContext) bool {
	if pending != nil && pending.Context.ExecutionID != executionID {
		return false
	}
	covered := func(asOf time.Time) bool {
		if pending != nil && len(pending.Contents) > 0 && asOf.Equal(pending.Context.ExecutionAsOf) {
			return true
		}
		for _, origin := range origins {
			if asOf.Equal(origin) {
				return true
			}
		}
		return false
	}
	for _, asOf := range links {
		if !covered(asOf) {
			return false
		}
	}
	if pending != nil {
		return covered(pending.Context.ExecutionAsOf)
	}
	return len(links) > 0
}

// Empty selection adds no citation. Existing findings use their current case
// classification and exact finding membership, independent of creation history.
// Creation and all nonempty selections keep the original reference admission.
func resolveFindingSelection(ctx context.Context, q workspaceQueryer, caseID, findingID string, selection *workspace.BasisSelection) error {
	if workchain.FromContext(ctx) == nil || len(selection.Evidence)+len(selection.Gaps)+len(selection.Resources)+len(selection.Contents) > 0 {
		return resolveScopedSelection(ctx, q, selection)
	}
	_, _, err := findingBasisIdentity(ctx, q, caseID, findingID)
	return err
}
