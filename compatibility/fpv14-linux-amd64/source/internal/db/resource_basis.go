package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

const resourceLinkJoin = `ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref,d.revision)=ROW(l.source_system_id,l.source_instance,l.org_id,l.project_id,l.record_ref,l.revision)`
const resourceIdentityWhere = `source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND record_ref=$5 AND revision=$6`

func resourceIdentityArgs(r workspace.ResourceChoice) []any {
	return []any{r.Reporter.SourceSystemID, r.Reporter.SourceInstance, r.Reporter.OrgID, r.Reporter.ProjectID, r.RecordRef, r.Revision}
}

// All online entry points resolve both sides under the existing grant lock.
// Historical restore deliberately uses the record resolver without this check.
func resourceRequirements(ctx context.Context, q workspaceQueryer, r workspace.ResourceChoice) ([]workchain.Requirement, error) {
	a := workchain.FromContext(ctx)
	if a == nil {
		return nil, workspace.ErrNotFound
	}
	// Match the same profile and optional economic project as runtime reading.
	var costProject string
	join := strings.ReplaceAll(sourceEconomicJoinSQL, "d.", "t.")
	profile := strings.ReplaceAll(runtimeProfileGrantSQL, "d.", "t.")
	err := q.QueryRowContext(ctx, `SELECT COALESCE(f.cost_project_ref,'') FROM source_observation_documents t LEFT JOIN workbench_economic_facts f ON `+join+` WHERE t.source_system_id=$1::uuid AND t.source_instance=$2 AND t.org_id=$3 AND t.project_id=$4 AND t.request_id=$5 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$6 AND g.active AND g.source_system_id=t.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||t.project_id) AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref) AND `+profile+`) ORDER BY f.id NULLS LAST LIMIT 1`, r.Target.SourceSystemID, r.Target.SourceInstance, r.Target.OrgID, r.Target.ProjectID, r.RequestID, a.Subject).Scan(&costProject)
	if err != nil {
		return nil, workspaceError(err)
	}
	reqs := []workchain.Requirement{
		{Source: r.Reporter.SourceSystemID, Project: "project-" + r.Reporter.ProjectID, Fields: workchain.Fields{Resource: true}},
		{Source: r.Target.SourceSystemID, Project: "project-" + r.Target.ProjectID, CostProject: costProject, Fields: workchain.Fields{Observation: true}},
	}
	for _, req := range reqs {
		if !a.Allows(req) {
			return nil, workspace.ErrNotFound
		}
	}
	return reqs, nil
}
func readResourceChoice(ctx context.Context, q workspaceQueryer, r workspace.ResourceChoice) (workspace.AdoptedResource, error) {
	out := workspace.AdoptedResource{Reference: r}
	if !workspace.ValidResourceChoice(r) {
		return out, workspace.ErrInvalid
	}
	var raw []byte
	var hash string
	if err := q.QueryRowContext(ctx, `SELECT record_bytes,record_hash FROM resource_access_records WHERE `+resourceIdentityWhere, resourceIdentityArgs(r)...).Scan(&raw, &hash); err != nil {
		return out, workspaceError(err)
	}
	if hash != r.RecordHash || !workspace.ResourceMatchesRecord(r, raw) {
		return out, workspace.ErrConflict
	}
	out.Record = raw
	return out, nil
}
func loadResourceLinks(ctx context.Context, q workspaceQueryer, ids []string) ([]workspace.ResourceLink, error) {
	var present bool
	if err := q.QueryRowContext(ctx, "SELECT to_regclass('public.case_resource_links') IS NOT NULL").Scan(&present); err != nil {
		return nil, err
	}
	if !present {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT case_id,COALESCE(finding_id,''),reference_snapshot,added_by_subject,added_at FROM case_resource_links WHERE case_id=ANY($1::text[]) ORDER BY case_id,finding_id,source_system_id,source_instance,org_id,project_id,record_ref,revision,relation`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workspace.ResourceLink
	for rows.Next() {
		var r workspace.ResourceLink
		var raw []byte
		if err = rows.Scan(&r.CaseID, &r.FindingID, &raw, &r.AddedBySubject, &r.AddedAt); err != nil {
			return nil, err
		}
		if err = decodeAssessmentRecord(raw, &r.Reference); err != nil || !workspace.ValidResourceChoice(r.Reference) {
			return nil, workspace.ErrConflict
		}
		if (r.FindingID == "" && r.Reference.Scope != "case") || (r.FindingID != "" && r.Reference.Scope != "finding") {
			return nil, workspace.ErrConflict
		}
		r.AddedAt = r.AddedAt.UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}
func insertResourceLinks(ctx context.Context, tx *sql.Tx, c, f string, actor workspace.Actor, rs []workspace.ResourceChoice, now time.Time) error {
	for _, r := range rs {
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO case_resource_links(case_id,finding_id,source_system_id,source_instance,org_id,project_id,record_ref,revision,relation,reference_snapshot,added_by_subject,added_at) VALUES($1,$2,$3::uuid,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12)`, c, f, r.Reporter.SourceSystemID, r.Reporter.SourceInstance, r.Reporter.OrgID, r.Reporter.ProjectID, r.RecordRef, r.Revision, r.Relation, raw, actor.Ref, now); err != nil {
			return err
		}
	}
	return nil
}
func resourceBasisLookup(ctx context.Context, q workspaceQueryer, c, f string, creating bool) func(workspace.ResourceChoice) (workspace.AdoptedResource, error) {
	return func(r workspace.ResourceChoice) (workspace.AdoptedResource, error) {
		if _, err := resourceRequirements(ctx, q, r); err != nil {
			return workspace.AdoptedResource{}, err
		}
		if !creating {
			finding := f
			if r.Scope == "case" {
				finding = ""
			}
			args := append(resourceIdentityArgs(r), c, finding, r.Relation)
			var raw []byte
			if err := q.QueryRowContext(ctx, `SELECT reference_snapshot FROM case_resource_links WHERE `+resourceIdentityWhere+` AND case_id=$7 AND finding_id IS NOT DISTINCT FROM NULLIF($8,'') AND relation=$9`, args...).Scan(&raw); err != nil {
				return workspace.AdoptedResource{}, workspaceError(err)
			}
			var link workspace.ResourceChoice
			if decodeAssessmentRecord(raw, &link) != nil || link != r {
				return workspace.AdoptedResource{}, workspace.ErrConflict
			}
		}
		return readResourceChoice(ctx, q, r)
	}
}

// Include every resource link in case filtering before limits and queue totals.
func resourceRecordAccessSQL(subject string) string {
	reporter := strings.ReplaceAll(runtimeResourceGrantSQL, "$2", subject)
	join := strings.ReplaceAll(sourceEconomicJoinSQL, "d.", "t.")
	profile := strings.ReplaceAll(runtimeProfileGrantSQL, "d.", "t.")
	target := `EXISTS(SELECT 1 FROM source_observation_documents t LEFT JOIN workbench_economic_facts f ON ` + join + ` WHERE ROW(t.source_system_id,t.source_instance,t.org_id,t.project_id,t.request_id)=ROW(d.target_source_system_id,d.target_source_instance,d.target_org_id,d.target_project_id,d.request_id) AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=` + subject + ` AND g.active AND g.source_system_id=t.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||t.project_id) AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref) AND ` + profile + `))`
	return `(` + reporter + `) AND (` + target + `)`
}
func scopedResourceCaseSQL(caseID, subject string) string {
	return `NOT EXISTS(SELECT 1 FROM case_resource_links l LEFT JOIN resource_access_records d ON ` + resourceLinkJoin + ` WHERE l.case_id=` + caseID + ` AND (d.record_ref IS NULL OR NOT (` + resourceRecordAccessSQL(subject) + `)))`
}

const resourceRequirementFieldsSQL = `('{"context":false,"identity":false,"cost":false,"resource":true}'::jsonb,'{"context":false,"identity":false,"cost":false,"observation":true}'::jsonb)`

func scopedRequirementSQL(subject string) string {
	base := `SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=` + subject + ` AND g.active AND g.source_system_id::text=req->>'source' AND (g.source_project_ref IS NULL OR g.source_project_ref=NULLIF(req->>'project','')) AND (g.cost_project_ref IS NULL OR g.cost_project_ref=NULLIF(req->>'cost_project',''))`
	complete := `req->'fields'='{"context":true,"identity":true,"cost":true}'::jsonb`
	for _, cap := range []string{"allow_source_context", "allow_source_identity", "allow_source_cost"} {
		complete += ` AND EXISTS(` + base + ` AND g.allow_details AND g.` + cap + `)`
	}
	return `((req->'fields'='{"context":false,"identity":false,"cost":false,"content":true}'::jsonb AND EXISTS(` + base + ` AND g.allow_content AND g.allowed_content_purposes='["human_review"]'::jsonb)) OR (` + complete + `) OR (req->'fields'='{"context":false,"identity":false,"cost":false,"resource":true}'::jsonb AND EXISTS(` + base + ` AND g.allow_resource)) OR (req->'fields'='{"context":false,"identity":false,"cost":false,"observation":true}'::jsonb AND EXISTS(` + base + ` AND (g.allow_source_runtime OR g.allow_source_attempt OR g.allow_source_capability OR g.allow_source_policy OR g.allow_source_destination))))`
}

// The immutable action chain is validated separately. Here each historical
// reference is checked against its retained link and original record, without
// asking whether the historical actor still has permission today.
func validateResourceAssessmentHistory(ctx context.Context, q workspaceQueryer, caseID string, s workspace.AssessmentSnapshot) error {
	links, err := loadResourceLinks(ctx, q, []string{caseID})
	if err != nil {
		return err
	}
	byKey := map[string]workspace.ResourceChoice{}
	for _, link := range links {
		if _, err = readResourceChoice(ctx, q, link.Reference); err != nil {
			return err
		}
		byKey[queueLinkKey(caseID, link.FindingID, workspace.ResourceChoiceKey(link.Reference))] = link.Reference
	}
	validate := func(finding string, b workspace.ReviewBasis) error {
		for _, r := range append(append([]workspace.ResourceChoice{}, b.Known.Resources...), b.Selected.Resources...) {
			f := finding
			if r.Scope == "case" {
				f = ""
			}
			if byKey[queueLinkKey(caseID, f, workspace.ResourceChoiceKey(r))] != r {
				return errAssessmentRestore
			}
		}
		for _, r := range b.AdoptedResources {
			got, e := readResourceChoice(ctx, q, r.Reference)
			if e != nil {
				return e
			}
			if !workspace.SameAssessmentValue(got, r) {
				return errAssessmentRestore
			}
		}
		return nil
	}
	for _, a := range s.Actions {
		if err = validate(a.Current.FindingID, a.Basis); err != nil {
			return err
		}
	}
	for _, c := range s.Creations {
		if err = validate(c.FindingID, c.Basis); err != nil {
			return err
		}
	}
	return nil
}
