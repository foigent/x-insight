package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workspace"
)

func loadContentLinks(ctx context.Context, q workspaceQueryer, ids []string) ([]workspace.ContentLink, error) {
	present, err := contentLinksPresent(ctx, q)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT case_id,COALESCE(finding_id,''),reference_snapshot,added_by_subject,added_at,choice_sha256 FROM case_content_links WHERE case_id=ANY($1::text[]) ORDER BY case_id,finding_id,choice_sha256`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workspace.ContentLink
	for rows.Next() {
		var link workspace.ContentLink
		var raw []byte
		var hash string
		if err = rows.Scan(&link.CaseID, &link.FindingID, &raw, &link.AddedBySubject, &link.AddedAt, &hash); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &link.Reference) != nil || sourcefacts.Hash([]byte(workspace.ContentChoiceKey(link.Reference))) != hash || (link.FindingID == "") != (link.Reference.Scope == "case") {
			return nil, workspace.ErrConflict
		}
		link.AddedAt = link.AddedAt.UTC()
		out = append(out, link)
	}
	return out, rows.Err()
}

func insertContentLinks(ctx context.Context, tx *sql.Tx, caseID, findingID string, actor workspace.Actor, choices []workspace.ContentChoice, now time.Time) error {
	for _, choice := range choices {
		if _, err := contentRequirements(ctx, tx, choice); err != nil {
			return err
		}
		finding := findingID
		if choice.Scope == "case" {
			finding = ""
		}
		raw, err := json.Marshal(choice)
		if err != nil {
			return err
		}
		r := choice.Reference
		if _, err = tx.ExecContext(ctx, `INSERT INTO case_content_links(case_id,finding_id,source_system_id,source_instance,org_id,project_id,request_id,source_fact_id,revision,source_sha256,relation,choice_sha256,reference_snapshot,added_by_subject,added_at) VALUES($1,NULLIF($2,''),$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14,$15)`, caseID, finding, r.SourceSystemID, r.SourceInstance, r.OrgID, r.ProjectID, r.RequestID, r.SourceFactID, r.Revision, r.SourceSHA256, choice.Relation, sourcefacts.Hash([]byte(workspace.ContentChoiceKey(choice))), raw, actor.Ref, now); err != nil {
			return err
		}
	}
	return nil
}

// This internal restore validator checks retained reference identities and
// immutable action membership only. It never authorizes an online read.
func validateContentAssessmentHistory(ctx context.Context, q workspaceQueryer, caseID string, snapshot workspace.AssessmentSnapshot) error {
	links, err := loadContentLinks(ctx, q, []string{caseID})
	if err != nil {
		return err
	}
	byKey := make(map[string]workspace.ContentChoice)
	for _, link := range links {
		if err = readContentChoice(ctx, q, link.Reference); err != nil {
			return err
		}
		byKey[queueLinkKey(caseID, link.FindingID, workspace.ContentChoiceKey(link.Reference))] = link.Reference
	}
	validate := func(finding string, b workspace.ReviewBasis) error {
		for _, c := range append(append([]workspace.ContentChoice{}, b.Known.Contents...), b.Selected.Contents...) {
			f := finding
			if c.Scope == "case" {
				f = ""
			}
			stored, ok := byKey[queueLinkKey(caseID, f, workspace.ContentChoiceKey(c))]
			if !ok || workspace.ContentChoiceKey(stored) != workspace.ContentChoiceKey(c) {
				return errAssessmentRestore
			}
		}
		return nil
	}
	for _, a := range snapshot.Actions {
		if err = validate(a.Current.FindingID, a.Basis); err != nil {
			return err
		}
	}
	for _, c := range snapshot.Creations {
		if err = validate(c.FindingID, c.Basis); err != nil {
			return err
		}
	}
	return nil
}

func validateContentPackLinks(ctx context.Context, q workspaceQueryer, p workspace.EvidencePack) error {
	links, err := loadContentLinks(ctx, q, []string{p.CaseID})
	if err != nil {
		return err
	}
	if len(links) != len(p.Contents) {
		return errAssessmentRestore
	}
	remaining := make(map[string]workspace.ContentLink)
	for _, link := range links {
		remaining[queueLinkKey(link.CaseID, link.FindingID, workspace.ContentChoiceKey(link.Reference))] = link
	}
	for _, link := range p.Contents {
		key := queueLinkKey(link.CaseID, link.FindingID, workspace.ContentChoiceKey(link.Reference))
		stored, ok := remaining[key]
		if !ok || !workspace.SameAssessmentValue(stored, link) {
			return errAssessmentRestore
		}
		delete(remaining, key)
	}
	return nil
}
