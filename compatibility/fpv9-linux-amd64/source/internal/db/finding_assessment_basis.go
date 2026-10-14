package db

import (
	"context"
	"database/sql"
	"strings"

	"github.com/foigent/x-insight/internal/workspace"
)

func findingBasisIdentity(ctx context.Context, q workspaceQueryer, caseID, findingID string) (string, int64, error) {
	if err := authorizeScopedCase(ctx, q, caseID); err != nil {
		return "", 0, err
	}

	var statement string
	var version int64
	err := q.QueryRowContext(ctx, `SELECT statement,version FROM case_findings WHERE case_id=$1 AND id=$2`, caseID, findingID).Scan(&statement, &version)
	return statement, version, workspaceError(err)
}

func knownFindingReferences(ctx context.Context, q workspaceQueryer, caseID, findingID string) (workspace.BasisSelection, error) {
	s := workspace.BasisSelection{Evidence: []workspace.EvidenceChoice{}, Gaps: []workspace.GapChoice{}}
	rows, err := q.QueryContext(ctx, `SELECT evidence_event_id::text,relation,event_hash_snapshot,source_ref_snapshot FROM case_evidence_links WHERE case_id=$1 AND finding_id=$2 ORDER BY evidence_event_id,relation LIMIT $3`, caseID, findingID, workspace.MaxKnownReferences+1)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		e := workspace.EvidenceChoice{Scope: "finding"}
		if err = rows.Scan(&e.EvidenceEventID, &e.Relation, &e.EventHash, &e.SourceRef); err != nil {
			rows.Close()
			return s, err
		}
		s.Evidence = append(s.Evidence, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	rows, err = q.QueryContext(ctx, `SELECT gap_id::text,relation,affected_claim_snapshot,gap_type_snapshot FROM case_gap_links WHERE case_id=$1 AND finding_id=$2 ORDER BY gap_id,relation LIMIT $3`, caseID, findingID, workspace.MaxKnownReferences+1)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		g := workspace.GapChoice{Scope: "finding"}
		if err = rows.Scan(&g.GapID, &g.Relation, &g.AffectedClaim, &g.GapType); err != nil {
			rows.Close()
			return s, err
		}
		s.Gaps = append(s.Gaps, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	links, err := loadResourceLinks(ctx, q, []string{caseID})
	if err != nil {
		return s, err
	}
	for _, l := range links {
		if l.FindingID == findingID {
			s.Resources = append(s.Resources, l.Reference)
		}
	}
	contents, err := loadContentLinks(ctx, q, []string{caseID})
	if err != nil {
		return s, err
	}
	for _, l := range contents {
		if l.FindingID == findingID {
			s.Contents = append(s.Contents, l.Reference)
		}
	}
	return workspace.NormalizeBasisSelection(s, workspace.MaxKnownReferences)
}

// Existing Finding reads and new-record previews use exactly this resolver.
// Each selected Gap joins its resolution once: version and contents come from
// the same MVCC row. The returned value, never a future JOIN, is persisted.
func resolveReviewBasis(ctx context.Context, q workspaceQueryer, caseID, findingID, statement string, version int64, selection, known workspace.BasisSelection, creating bool) (workspace.ReviewBasis, error) {
	selection = scopedSelectionInput(ctx, selection)
	var selectionErr error
	if creating {
		selectionErr = resolveScopedSelection(ctx, q, &selection)
	} else {
		selectionErr = resolveFindingSelection(ctx, q, caseID, findingID, &selection)
	}
	if selectionErr != nil {
		return workspace.ReviewBasis{}, selectionErr
	}
	if creating {
		known = selection
	}
	selection, err := workspace.NormalizeBasisSelection(selection, workspace.MaxSelectedReferences)
	if err != nil {
		return workspace.ReviewBasis{}, err
	}
	gapRows, err := readSelectedGapRows(ctx, q, selection.Gaps)
	if err != nil {
		return workspace.ReviewBasis{}, err
	}
	evidenceLookup := func(e workspace.EvidenceChoice) (workspace.EvidenceChoice, string, string, error) {
		link := e
		if !creating {
			linkFinding := findingID
			if e.Scope == "case" {
				linkFinding = ""
			}
			if err := q.QueryRowContext(ctx, `SELECT event_hash_snapshot,source_ref_snapshot FROM case_evidence_links WHERE case_id=$1 AND finding_id IS NOT DISTINCT FROM NULLIF($2,'') AND evidence_event_id=$3::uuid AND relation=$4`, caseID, linkFinding, e.EvidenceEventID, e.Relation).Scan(&link.EventHash, &link.SourceRef); err != nil {
				return link, "", "", workspaceError(err)
			}
		}
		var hash, ref string
		err := q.QueryRowContext(ctx, `SELECT event_hash,source_ref FROM evidence_events WHERE id=$1::uuid`, e.EvidenceEventID).Scan(&hash, &ref)
		return link, hash, ref, workspaceError(err)
	}
	gapLookup := func(g workspace.GapChoice) (workspace.GapChoice, selectedGapRow, error) {
		link := g
		if !creating {
			linkFinding := findingID
			if g.Scope == "case" {
				linkFinding = ""
			}
			if err := q.QueryRowContext(ctx, `SELECT affected_claim_snapshot,gap_type_snapshot FROM case_gap_links WHERE case_id=$1 AND finding_id IS NOT DISTINCT FROM NULLIF($2,'') AND gap_id=$3::uuid AND relation=$4`, caseID, linkFinding, g.GapID, g.Relation).Scan(&link.AffectedClaim, &link.GapType); err != nil {
				return link, selectedGapRow{}, workspaceError(err)
			}
		}
		row, ok := gapRows[g.GapID]
		if !ok {
			return link, row, workspace.ErrNotFound
		}
		return link, row, nil
	}
	return assembleReviewBasisWithContent(statement, version, selection, known, evidenceLookup, gapLookup, contentBasisLookup(ctx, q, caseID, findingID, creating), resourceBasisLookup(ctx, q, caseID, findingID, creating))
}

// All readers use the same identity checks and canonical basis assembly. The
// queue merely supplies prefetched rows, without changing assessment semantics.
func assembleReviewBasis(statement string, version int64, selection, known workspace.BasisSelection,
	evidenceLookup func(workspace.EvidenceChoice) (workspace.EvidenceChoice, string, string, error),
	gapLookup func(workspace.GapChoice) (workspace.GapChoice, selectedGapRow, error), resourceLookups ...func(workspace.ResourceChoice) (workspace.AdoptedResource, error)) (workspace.ReviewBasis, error) {
	return assembleReviewBasisWithContent(statement, version, selection, known, evidenceLookup, gapLookup, nil, resourceLookups...)
}

func assembleReviewBasisWithContent(statement string, version int64, selection, known workspace.BasisSelection,
	evidenceLookup func(workspace.EvidenceChoice) (workspace.EvidenceChoice, string, string, error),
	gapLookup func(workspace.GapChoice) (workspace.GapChoice, selectedGapRow, error), contentLookup func(workspace.ContentChoice) error, resourceLookups ...func(workspace.ResourceChoice) (workspace.AdoptedResource, error)) (workspace.ReviewBasis, error) {
	b := workspace.ReviewBasis{Schema: workspace.ReviewBasisSchema, Statement: statement, FindingVersion: version, Known: known, AdoptedGaps: []workspace.AdoptedGap{}}
	var err error
	b.Selected, err = workspace.NormalizeBasisSelection(selection, workspace.MaxSelectedReferences)
	if err != nil {
		return b, err
	}
	for _, e := range b.Selected.Evidence {
		link, hash, ref, err := evidenceLookup(e)
		if err != nil {
			return b, err
		}
		if link.EventHash != e.EventHash || link.SourceRef != e.SourceRef || hash != e.EventHash || ref != e.SourceRef {
			return b, workspace.ErrConflict
		}
	}
	for _, g := range b.Selected.Gaps {
		link, row, err := gapLookup(g)
		if err != nil {
			return b, err
		}
		if link.AffectedClaim != g.AffectedClaim || link.GapType != g.GapType || row.claim != g.AffectedClaim || row.kind != g.GapType {
			return b, workspace.ErrConflict
		}
		adopted := row.adopted
		adopted.Reference = g
		b.AdoptedGaps = append(b.AdoptedGaps, adopted)
	}
	if len(b.Known.Resources)+len(b.Selected.Resources) > 0 {
		b.Schema = workspace.ResourceReviewBasisSchema
	}
	for _, r := range b.Selected.Resources {
		if len(resourceLookups) != 1 {
			return b, workspace.ErrNotFound
		}
		adopted, e := resourceLookups[0](r)
		if e != nil {
			return b, e
		}
		b.AdoptedResources = append(b.AdoptedResources, adopted)
	}
	if len(b.Known.Contents)+len(b.Selected.Contents) > 0 {
		b.Schema = workspace.ContentReviewBasisSchema
		b.ContentBasisVersion = workspace.ContentBasisVersion
		if contentLookup == nil {
			return b, workspace.ErrNotFound
		}
		if err := validateContentBasisReferences(b, contentLookup); err != nil {
			return b, err
		}
	}
	return workspace.CanonicalReviewBasis(b)
}

type selectedGapRow struct {
	claim, kind string
	adopted     workspace.AdoptedGap
}

func readSelectedGapRows(ctx context.Context, q workspaceQueryer, selected []workspace.GapChoice) (map[string]selectedGapRow, error) {
	result := make(map[string]selectedGapRow, len(selected))
	if len(selected) == 0 {
		return result, nil
	}
	ids := make([]string, 0, len(selected))
	for _, g := range selected {
		ids = append(ids, g.GapID)
	}
	// One statement snapshot also keeps two selected relations to the same Gap
	// on the same resolution version under Read Committed write transactions.
	rows, err := q.QueryContext(ctx, `SELECT g.id::text,g.affected_claim,g.gap_type,r.version,r.state,r.note,r.updated_by_subject,r.updated_at FROM capture_gaps g LEFT JOIN capture_gap_resolutions r ON r.gap_id=g.id WHERE g.id=ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var item selectedGapRow
		var state, note, subject sql.NullString
		var at sql.NullTime
		var version sql.NullInt64
		if err := rows.Scan(&id, &item.claim, &item.kind, &version, &state, &note, &subject, &at); err != nil {
			return nil, err
		}
		if version.Valid {
			if !at.Valid || !state.Valid || !note.Valid || !subject.Valid {
				return nil, workspace.ErrConflict
			}
			t := at.Time.UTC()
			item.adopted = workspace.AdoptedGap{ResolutionRecorded: true, ResolutionVersion: version.Int64, State: state.String, Note: note.String, UpdatedBySubject: subject.String, UpdatedAt: &t}
		}
		result[id] = item
	}
	return result, rows.Err()
}

func resolveFindingBasis(ctx context.Context, q workspaceQueryer, caseID, findingID string, selection workspace.BasisSelection) (workspace.ReviewBasis, error) {
	statement, version, err := findingBasisIdentity(ctx, q, caseID, findingID)
	if err != nil {
		return workspace.ReviewBasis{}, err
	}
	known, err := knownFindingReferences(ctx, q, caseID, findingID)
	if err != nil {
		return workspace.ReviewBasis{}, err
	}
	basis, err := resolveReviewBasis(ctx, q, caseID, findingID, statement, version, selection, known, false)
	if err != nil {
		return basis, err
	}
	basis.ResolvedExecutionContext, err = persistedFindingExecutionContext(ctx, q, caseID, findingID)
	if err != nil {
		return basis, err
	}
	return workspace.CanonicalReviewBasis(basis)
}

func (s *WorkspaceStore) PreviewFindingBasis(ctx context.Context, caseID, findingID string, selection workspace.BasisSelection) (workspace.ReviewBasis, error) {
	if err := requireContentLocationProof(ctx, selection); err != nil {
		return workspace.ReviewBasis{}, err
	}
	var b workspace.ReviewBasis
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	b, err = resolveFindingBasis(ctx, tx, caseID, findingID, selection)
	if err != nil {
		return b, err
	}
	return b, commitWorkspaceTx(ctx, tx)
}

func validateCreationContext(ctx context.Context, q workspaceQueryer, c workspace.ReviewCreationCommand) error {
	if c.NewCase == nil {
		if err := authorizeScopedCase(ctx, q, c.CaseID); err != nil {
			return err
		}
		var status string
		var version int64
		if err := q.QueryRowContext(ctx, `SELECT status,version FROM investigation_cases WHERE id=$1`, c.CaseID).Scan(&status, &version); err != nil {
			return workspaceError(err)
		}
		if status != "open" {
			return workspace.ErrSealed
		}
		if version != c.ExpectedCaseVersion {
			return workspace.ErrConflict
		}
	}
	if c.From != nil {
		if err := authorizeScopedCase(ctx, q, c.From.CaseID); err != nil {
			return err
		}
		var status, digest string
		var revision sql.NullInt64
		err := q.QueryRowContext(ctx, `SELECT c.status,COALESCE(c.sealed_digest,''),c.sealed_revision FROM investigation_cases c JOIN case_findings f ON f.case_id=c.id WHERE c.id=$1 AND f.id=$2`, c.From.CaseID, c.From.FindingID).Scan(&status, &digest, &revision)
		if err != nil {
			return workspaceError(err)
		}
		if status != "sealed" || !revision.Valid || revision.Int64 != c.From.Revision || digest != c.From.Digest {
			return workspace.ErrConflict
		}
	}
	return nil
}

func (s *WorkspaceStore) PreviewReviewCreation(ctx context.Context, input workspace.ReviewCreationCommand) (workspace.ReviewBasis, error) {
	if err := requireContentLocationProof(ctx, input.Selection); err != nil {
		return workspace.ReviewBasis{}, err
	}
	var b workspace.ReviewBasis
	// Only the expected digest is absent before the first preview. All other
	// target/selection fields use the same bounded creation contract.
	input.ExpectedBasisDigest = "sha256:" + strings.Repeat("0", 64)
	input.Selection = scopedSelectionInput(ctx, input.Selection)
	c, err := workspace.NormalizeReviewCreation(input)
	if err != nil {
		return b, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	if err = validateCreationContext(ctx, tx, c); err != nil {
		return b, err
	}
	b, err = resolveReviewBasis(ctx, tx, "", "", c.Finding.Statement, 1, c.Selection, c.Selection, true)
	if err != nil {
		return b, err
	}
	b, err = resolveCreationExecutionContext(ctx, tx, c, b)
	if err != nil {
		return b, err
	}
	return b, commitWorkspaceTx(ctx, tx, func() error {
		return checkExecutionContextDeadline(ctx, tx, c.ExecutionContext, c.Selection.Contents...)
	})
}
