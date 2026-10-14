package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

// These are the persisted, case-owned records. Live evidence/gap projections
// are deliberately excluded: their authorization is checked from their anchors.
var scopedCaseTables = []string{"case_executions", "case_findings", "case_evidence_links", "case_gap_links", "case_resource_links", "case_content_links", "case_annotations", "investigation_alert_case_links", "case_finding_followups", "case_finding_actions", "case_finding_assessments", "case_finding_assessment_actions", "finding_review_creations"}

// Canonicalize native timestamp columns before hashing. JSON receipts are
// already immutable canonical documents. The binding must survive a different
// PostgreSQL session timezone during restore or a reader connection change.
func caseRowJSONSQL() string {
	return `(SELECT jsonb_object_agg(k,CASE WHEN k IN ('created_at','updated_at','added_at','sealed_at','as_of','due_at') AND jsonb_typeof(v)='string' THEN to_jsonb(to_char((v #>> '{}')::timestamptz AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) ELSE v END) FROM jsonb_each(to_jsonb(c)) AS fields(k,v))`
}
func caseDocumentsSQL(id string) string {
	parts := []string{fmt.Sprintf("SELECT 'investigation_cases' AS kind,"+caseRowJSONSQL()+" AS doc FROM investigation_cases c WHERE c.id=%s", id)}
	for _, t := range scopedCaseTables {
		parts = append(parts, fmt.Sprintf("SELECT '%s',"+caseRowJSONSQL()+" FROM %s c WHERE c.case_id=%s", t, t, id))
	}
	return strings.Join(parts, " UNION ALL ")
}
func caseDigestSQL(id string) string {
	return "(SELECT 'sha256:'||encode(sha256(convert_to(COALESCE(jsonb_agg(jsonb_build_array(d.kind,d.doc) ORDER BY d.kind,d.doc::text),'[]'::jsonb)::text,'UTF8')),'hex') FROM (" + caseDocumentsSQL(id) + ") d)"
}

// Scope labels classify data, never grant access. The predicate is evaluated
// before list limits and queue statistics, including all historical case links.
func scopedCaseSQL(id, subject string) string {
	return fmt.Sprintf(`EXISTS (WITH RECURSIVE related(id) AS (
 SELECT %[1]s::text UNION
 SELECT refs.id FROM related r CROSS JOIN LATERAL (
   SELECT DISTINCT ref #>> '{}' AS id FROM (%[3]s) docs
   CROSS JOIN LATERAL jsonb_path_query(docs.doc,'lax $.**.case_id') ref
   UNION SELECT c.case_id FROM finding_review_creations c WHERE c.source_case_id=r.id
 ) refs WHERE refs.id IS NOT NULL AND refs.id<>''
 ) SELECT 1 WHERE NOT EXISTS (
 SELECT 1 FROM related r LEFT JOIN investigation_cases c ON c.id=r.id
 LEFT JOIN investigation_case_scope_labels l ON l.case_id=r.id
 WHERE c.id IS NULL OR l.case_id IS NULL OR l.schema_version NOT IN ('xinsight.case-scope.v1','xinsight.case-scope.v2','xinsight.case-scope.v3')
 OR l.case_version<>c.version OR l.content_digest<>%[4]s
 OR EXISTS (SELECT 1 FROM (
   SELECT ce.execution_id FROM case_executions ce WHERE ce.case_id=r.id
   UNION SELECT e.execution_id FROM case_evidence_links ce LEFT JOIN evidence_events e ON e.id=ce.evidence_event_id WHERE ce.case_id=r.id
   UNION SELECT e.execution_id FROM case_gap_links cg LEFT JOIN capture_gaps g ON g.id=cg.gap_id LEFT JOIN evidence_events e ON e.id=g.evidence_event_id AND e.source_system_id=g.source_system_id AND e.request_id=g.request_id WHERE cg.case_id=r.id
 ) resources WHERE resources.execution_id IS NULL OR NOT (%[5]s))
 OR NOT (%[6]s)
 OR jsonb_typeof(l.requirements)<>'array'  OR jsonb_array_length(l.requirements)=0
 OR EXISTS (SELECT 1 FROM jsonb_array_elements(l.requirements) req WHERE
   jsonb_typeof(req)<>'object' OR COALESCE(req->>'source','')='' OR
   NOT (COALESCE(%[7]s,false)) OR (req->'fields' IN `+resourceRequirementFieldsSQL+` AND l.schema_version NOT IN ('xinsight.case-scope.v2','xinsight.case-scope.v3')) OR (req->'fields'='{"context":false,"identity":false,"cost":false,"content":true}'::jsonb AND l.schema_version<>'xinsight.case-scope.v3'))
 ))`, id, subject, caseDocumentsSQL("r.id"), caseDigestSQL("r.id"), scopedExecutionSQL("resources.execution_id", subject), scopedResourceCaseSQL("r.id", subject)+" AND "+scopedContentCaseSQL("r.id", subject), scopedRequirementSQL(subject))
}

func authorizeScopedCase(ctx context.Context, q workspaceQueryer, id string) error {
	a := workchain.FromContext(ctx)
	if a == nil {
		return authorizeUnscopedContentCase(ctx, q, id)
	}
	var ok bool
	if err := q.QueryRowContext(ctx, "SELECT "+scopedCaseSQL("$1", "$2"), id, a.Subject).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return workspace.ErrNotFound
	}
	return nil
}

// A transaction records its checked pre-state so a later version bump cannot
// cause an accidental reclassification of a previously unlabelled legacy case.
func prepareScopedCase(ctx context.Context, tx *sql.Tx, id string) error {
	a := workchain.FromContext(ctx)
	if a == nil {
		return authorizeUnscopedContentCase(ctx, tx, id)
	}
	if !a.Reviewer {
		return workspace.ErrNotFound
	}
	if _, ok := a.Cases[tx][id]; ok {
		return nil
	}
	// This repeated authorization/digest query spends far more time compiling
	// JIT code than checking the case. Keep the setting inside this transaction;
	// commit and rollback both restore the pooled connection's original value.
	if _, err := tx.ExecContext(ctx, "SET LOCAL jit=off"); err != nil {
		return err
	}
	var got string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM investigation_cases WHERE id=$1 FOR UPDATE", id).Scan(&got); err != nil {
		return workspaceError(err)
	}
	if err := authorizeScopedCase(ctx, tx, id); err != nil {
		return err
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, "SELECT requirements FROM investigation_case_scope_labels WHERE case_id=$1", id).Scan(&raw); err != nil {
		return err
	}
	var reqs []workchain.Requirement
	if err := json.Unmarshal(raw, &reqs); err != nil {
		return workspace.ErrNotFound
	}
	registerScopedCase(a, tx, id, reqs)
	return nil
}
func registerScopedCase(a *workchain.Access, tx *sql.Tx, id string, reqs []workchain.Requirement) {
	if a.Cases == nil {
		a.Cases = make(map[any]map[string][]workchain.Requirement)
	}
	if a.Cases[tx] == nil {
		a.Cases[tx] = make(map[string][]workchain.Requirement)
	}
	a.Cases[tx][id] = reqs
}

func scopedReferenceRequirement(ctx context.Context, q workspaceQueryer, kind, id string) (workchain.Requirement, error) {
	var execution string
	query := "SELECT execution_id FROM evidence_events WHERE id=$1::uuid"
	if kind == "gap" {
		query = `SELECT e.execution_id FROM capture_gaps g JOIN evidence_events e ON e.id=g.evidence_event_id AND e.source_system_id=g.source_system_id AND e.request_id=g.request_id WHERE g.id=$1::uuid`
	}
	if err := q.QueryRowContext(ctx, query, id).Scan(&execution); err != nil {
		return workchain.Requirement{}, workspaceError(err)
	}
	req, err := scopedExecutionRequirement(ctx, q, execution)
	if errors.Is(err, workchain.ErrDenied) {
		err = workspace.ErrNotFound
	}
	if err == nil && !req.Fields.Complete() {
		err = workspace.ErrNotFound
	}
	req.Fields = workchain.Fields{Context: true, Identity: true, Cost: true}
	return req, err
}
func authorizeScopedSelection(ctx context.Context, q workspaceQueryer, selection workspace.BasisSelection) error {
	if workchain.FromContext(ctx) == nil {
		return nil
	}
	if _, err := workspace.NormalizeBasisSelection(scopedSelectionInput(ctx, selection), workspace.MaxSelectedReferences); err != nil {
		return err
	}
	if len(selection.Evidence)+len(selection.Gaps)+len(selection.Resources)+len(selection.Contents) == 0 {
		return workspace.ErrInvalid
	}
	for _, e := range selection.Evidence {
		if _, err := scopedReferenceRequirement(ctx, q, "event", e.EvidenceEventID); err != nil {
			return err
		}
	}
	for _, g := range selection.Gaps {
		if _, err := scopedReferenceRequirement(ctx, q, "gap", g.GapID); err != nil {
			return err
		}
	}
	for _, c := range selection.Contents {
		if _, err := contentRequirements(ctx, q, c); err != nil {
			return err
		}
	}
	for _, r := range selection.Resources {
		if _, err := resourceRequirements(ctx, q, r); err != nil {
			return err
		}
	}
	return nil
}

func refreshScopedCases(ctx context.Context, tx *sql.Tx) error {
	a := workchain.FromContext(ctx)
	if a == nil {
		return nil
	}
	ids := make([]string, 0, len(a.Cases[tx]))
	for id := range a.Cases[tx] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		reqs := append([]workchain.Requirement{}, a.Cases[tx][id]...)
		rows, err := tx.QueryContext(ctx, `SELECT 'event',evidence_event_id::text FROM case_evidence_links WHERE case_id=$1 UNION SELECT 'gap',gap_id::text FROM case_gap_links WHERE case_id=$1 UNION SELECT 'execution',execution_id FROM case_executions WHERE case_id=$1`, id)
		if err != nil {
			return err
		}
		var refs [][2]string
		for rows.Next() {
			var r [2]string
			if err = rows.Scan(&r[0], &r[1]); err != nil {
				rows.Close()
				return err
			}
			refs = append(refs, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		resourceLinks, err := loadResourceLinks(ctx, tx, []string{id})
		if err != nil {
			return err
		}
		for _, link := range resourceLinks {
			required, e := resourceRequirements(ctx, tx, link.Reference)
			if e != nil {
				return e
			}
			reqs = append(reqs, required...)
		}
		contentLinks, err := loadContentLinks(ctx, tx, []string{id})
		if err != nil {
			return err
		}
		for _, link := range contentLinks {
			required, e := contentRequirements(ctx, tx, link.Reference)
			if e != nil {
				return e
			}
			reqs = append(reqs, required...)
		}
		if len(refs)+len(resourceLinks)+len(contentLinks) == 0 {
			return workspace.ErrInvalid
		}
		for _, ref := range refs {
			var req workchain.Requirement
			if ref[0] == "execution" {
				var originOnly bool
				req, originOnly, err = scopedCaseExecutionRequirement(ctx, tx, id, ref[1], len(contentLinks) > 0, nil)
				if err == nil && originOnly {
					continue
				}
			} else {
				req, err = scopedReferenceRequirement(ctx, tx, ref[0], ref[1])
			}
			if err != nil {
				return err
			}
			req.Fields = workchain.Fields{Context: true, Identity: true, Cost: true}
			reqs = append(reqs, req)
		}
		// References embedded in immutable receipts include removed duplicate edges.
		related, err := tx.QueryContext(ctx, "SELECT DISTINCT ref #>> '{}' FROM ("+caseDocumentsSQL("$1")+") docs CROSS JOIN LATERAL jsonb_path_query(docs.doc,'lax $.**.case_id') ref WHERE ref #>> '{}'<>$1 AND COALESCE(ref #>> '{}','')<>''", id)
		if err != nil {
			return err
		}
		var peers []string
		for related.Next() {
			var peer string
			if err = related.Scan(&peer); err != nil {
				related.Close()
				return err
			}
			peers = append(peers, peer)
		}
		err = related.Err()
		related.Close()
		if err != nil {
			return err
		}
		for _, peer := range peers {
			if _, ok := a.Cases[tx][peer]; !ok {
				if err = authorizeScopedCase(ctx, tx, peer); err != nil {
					return err
				}
			}
			var raw []byte
			if err = tx.QueryRowContext(ctx, "SELECT requirements FROM investigation_case_scope_labels WHERE case_id=$1", peer).Scan(&raw); err != nil {
				return err
			}
			var inherited []workchain.Requirement
			if err = json.Unmarshal(raw, &inherited); err != nil {
				return err
			}
			reqs = append(reqs, inherited...)
		}
		unique := map[string]workchain.Requirement{}
		for _, r := range reqs {
			if !a.Allows(r) {
				return workspace.ErrNotFound
			}
			b, _ := json.Marshal(r)
			unique[string(b)] = r
		}
		keys := make([]string, 0, len(unique))
		for k := range unique {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		reqs = nil
		for _, k := range keys {
			reqs = append(reqs, unique[k])
		}
		raw, err := json.Marshal(reqs)
		if err != nil {
			return err
		}
		labelVersion := workchain.LabelVersion
		for _, r := range reqs {
			if r.Fields.Resource || r.Fields.Observation {
				labelVersion = "xinsight.case-scope.v2"
			}
		}
		for _, r := range reqs {
			if r.Fields.Content {
				labelVersion = "xinsight.case-scope.v3"
				break
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO investigation_case_scope_labels(case_id,schema_version,case_version,requirements,content_digest,labeled_at) SELECT id,$2,version,$3::jsonb,`+caseDigestSQL("$1")+`,clock_timestamp() FROM investigation_cases WHERE id=$1 ON CONFLICT(case_id) DO UPDATE SET schema_version=EXCLUDED.schema_version,case_version=EXCLUDED.case_version,requirements=EXCLUDED.requirements,content_digest=EXCLUDED.content_digest,labeled_at=EXCLUDED.labeled_at`, id, labelVersion, raw)
		if err != nil {
			return err
		}
	}
	return nil
}

// The caller has already established its own case authorization. Candidate
// checks reuse the held grant lock without borrowing another reader connection.
func (s *WorkspaceStore) ScopedCaseCandidate(ctx context.Context, id, subject string) (bool, error) {
	if workchain.FromContext(ctx) == nil {
		return false, workchain.ErrDenied
	}
	var ok bool
	err := s.db.QueryRowContext(ctx, "SELECT "+scopedCaseSQL("$1", "$2"), id, subject).Scan(&ok)
	return ok, err
}

const scopedSourceRefPlaceholder = "xinsight:resolve-scoped-reference"

func scopedSelectionInput(ctx context.Context, selection workspace.BasisSelection) workspace.BasisSelection {
	if workchain.FromContext(ctx) == nil {
		return selection
	}
	selection.Evidence = append([]workspace.EvidenceChoice{}, selection.Evidence...)
	for i := range selection.Evidence {
		if selection.Evidence[i].SourceRef == "" {
			selection.Evidence[i].SourceRef = scopedSourceRefPlaceholder
		}
	}
	return selection
}
func resolveScopedSelection(ctx context.Context, q workspaceQueryer, selection *workspace.BasisSelection) error {
	if len(selection.Resources)+len(selection.Contents) > 0 && workchain.FromContext(ctx) == nil {
		return workspace.ErrNotFound
	}
	if workchain.FromContext(ctx) == nil {
		return nil
	}
	if err := authorizeScopedSelection(ctx, q, *selection); err != nil {
		return err
	}
	for i := range selection.Evidence {
		e := &selection.Evidence[i]
		var hash, ref string
		if err := q.QueryRowContext(ctx, "SELECT event_hash,source_ref FROM evidence_events WHERE id=$1::uuid", e.EvidenceEventID).Scan(&hash, &ref); err != nil {
			return workspaceError(err)
		}
		if e.EventHash != hash {
			return workspace.ErrConflict
		}
		if e.SourceRef != "" && e.SourceRef != scopedSourceRefPlaceholder && e.SourceRef != ref {
			return workspace.ErrConflict
		}
		e.SourceRef = ref
	}
	return nil
}
