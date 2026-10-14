package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

func contentReviewTestChoice() workspace.ContentChoice {
	zero := int64(0)
	return workspace.ContentChoice{Scope: "finding", Relation: "supports", Reference: sourcefacts.FrozenManifestRef{SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "synthetic-source", OrgID: "17", ProjectID: "101", RequestID: "request-one", SourceFactID: "uf1_" + strings.Repeat("a", 64), Revision: "1", SourceSHA256: "sha256:" + strings.Repeat("b", 64), CaptureStatus: "complete", ContentRef: "uc1_" + strings.Repeat("c", 64), Representation: &sourcefacts.ContentRepresentation{MediaType: "text/plain", Encoding: "identity", ByteDomain: "stored_representation_before_http_content_encoding", Digest: sourcefacts.Hash(nil)}, ExpiresAt: "2026-09-16T00:00:00.000000Z"}, Location: sourcefacts.ContentLocation{Kind: "byte_range", Offset: &zero, Length: &zero}}
}

func TestContentReviewAssemblyRequiresResolverAndExactChoice(t *testing.T) {
	c := contentReviewTestChoice()
	s := workspace.BasisSelection{Contents: []workspace.ContentChoice{c}}
	if _, err := assembleReviewBasis("Human assertion", 1, s, s, nil, nil); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatal("content assembled without retained source lookup", err)
	}
	called := 0
	lookup := func(got workspace.ContentChoice) error {
		called++
		if workspace.ContentChoiceKey(c) != workspace.ContentChoiceKey(got) {
			return workspace.ErrConflict
		}
		return nil
	}
	b, err := assembleReviewBasisWithContent("Human assertion", 1, s, s, nil, nil, lookup)
	if err != nil || called != 1 || b.Schema != workspace.ContentReviewBasisSchema || b.ContentBasisVersion != workspace.ContentBasisVersion {
		t.Fatal("content resolver not adopted", err)
	}
	lookup = func(workspace.ContentChoice) error { return workspace.ErrConflict }
	if _, err = assembleReviewBasisWithContent("Human assertion", 1, s, s, nil, nil, lookup); !errors.Is(err, workspace.ErrConflict) {
		t.Fatal("source mismatch was ignored", err)
	}
	if _, err = contentRequirements(context.Background(), nil, c); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatal("unscoped input reached lookup", err)
	}
}

func TestContentReviewQueueUsesFrozenReferences(t *testing.T) {
	c := contentReviewTestChoice()
	s := workspace.BasisSelection{Contents: []workspace.ContentChoice{c}}
	b, err := assembleReviewBasisWithContent("Human assertion", 1, s, s, nil, nil, func(workspace.ContentChoice) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	f := workspace.Finding{CaseID: "case-one", ID: "finding-one", Statement: b.Statement, Version: 1}
	rows := queueBasisRows{known: map[string]workspace.BasisSelection{f.ID: b.Known}, contents: map[string]workspace.ContentChoice{queueLinkKey(f.CaseID, f.ID, workspace.ContentChoiceKey(c)): c}}
	v, err := rows.evaluate(f, &b)
	if err != nil || v.State != "unchanged" {
		t.Fatal("stable frozen content changed queue basis", v.State, err)
	}
	delete(rows.contents, queueLinkKey(f.CaseID, f.ID, workspace.ContentChoiceKey(c)))
	v, err = rows.evaluate(f, &b)
	if err != nil || v.State != "unavailable" {
		t.Fatal("missing retained reference hidden", v.State, err)
	}
}

func TestContentReviewScopeRequiresContentPurposeAndRealEconomicProject(t *testing.T) {
	r := workchain.Requirement{Source: "source-one", Project: "project-101", Fields: workchain.Fields{Content: true}}
	a := workchain.Access{Authorization: workbench.Authorization{Grants: []workbench.Grant{{SourceSystemID: r.Source, SourceProject: r.Project, AllowDetails: true, AllowSourceRuntime: true}}}}
	if a.Allows(r) {
		t.Fatal("old details/runtime grants conferred content")
	}
	a.Authorization.Grants[0].AllowContent = true
	a.Authorization.Grants[0].AllowedContentPurposes = []string{sourcefacts.ContentPurposeHumanReview}
	a.Authorization.Grants[0].CostProject = "real-cost"
	if a.Allows(r) {
		t.Fatal("unassociated content matched restricted cost project")
	}
	r.CostProject = "real-cost"
	if !a.Allows(r) {
		t.Fatal("exact content grant rejected")
	}
	a.Authorization.Grants[0].AllowedContentPurposes = nil
	if a.Allows(r) {
		t.Fatal("missing purpose accepted")
	}
	query := scopedCaseSQL("c.id", "$9")
	for _, fragment := range []string{"case_content_links", "source_content_manifests", "xinsight.case-scope.v3", "g.allow_content", "g.allowed_content_purposes", "f.evidence_event_id", "e.org_ref"} {
		if !strings.Contains(query, fragment) {
			t.Fatal("case scope omitted content qualification", fragment)
		}
	}
	if !strings.Contains(unscopedContentCaseSQL("c.id"), "WITH RECURSIVE related") {
		t.Fatal("continuation source omitted from unscoped exclusion")
	}
}

func TestContentReviewEmptySelectionRetainsCreationAndCaseBoundaries(t *testing.T) {
	store, state, ctx, _, _, _ := reviewReplayFixture(t)
	empty := workspace.BasisSelection{}
	if err := resolveScopedSelection(ctx, store.db, &empty); !errors.Is(err, workspace.ErrInvalid) {
		t.Fatal("empty selection admitted to creation", err)
	}
	state.caseAllowed = false
	if err := resolveFindingSelection(ctx, store.db, "hidden-case", "hidden-finding", &empty); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatal("empty follow-up skipped current case authorization", err)
	}
	state.caseAllowed = true
	state.findings = map[string]string{"visible-case:visible-finding": "Retained finding"}
	if err := resolveFindingSelection(ctx, store.db, "visible-case", "visible-finding", &empty); err != nil {
		t.Fatal("authorized existing finding required creation history", err)
	}
	for _, pair := range [][2]string{{"visible-case", "missing-finding"}, {"other-case", "visible-finding"}} {
		if err := resolveFindingSelection(ctx, store.db, pair[0], pair[1], &empty); !errors.Is(err, workspace.ErrNotFound) {
			t.Fatal("empty selection ignored exact finding ownership", err)
		}
	}
	if state.dml != 0 || state.commits != 0 {
		t.Fatal("empty selection admission performed a write")
	}
}
