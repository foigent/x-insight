package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestAssessmentStrictHTTPNegativesPreserveState(t *testing.T) {
	f := startResponsibilityProduction(t)
	writer := responsibilityLogin(t, f, "reviewer.one")
	ctx := context.Background()
	c := previewAssessmentCreation(t, f, newAssessmentCreation(t, workspace.BasisSelection{}))
	created, err := f.store.CreateReview(ctx, assessmentActor(), c, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	path := assessmentPath(created.CaseID, created.FindingID)
	valid := assessmentCommand(t, f, created.CaseID, created.FindingID, workspace.BasisSelection{}, &workspace.ReviewPayload{Conclusion: "pending_review", Reason: "Valid but not committed"}, nil)
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	badBodies := [][]byte{
		append([]byte(`{"actor_subject_id":"xs_2222222222222222",`), raw[1:]...),
		append([]byte(`{"operation":"record_progress",`), raw[1:]...),
		[]byte(`{"operation":"record_review","operation_id":"` + valid.OperationID + `"}`),
		[]byte(strings.Replace(string(raw), `"pending_review"`, `"confirmed_automatically"`, 1)),
		[]byte(strings.Replace(string(raw), `"pending_review"`, `"supported"`, 1)),
	}
	for i, body := range badBodies {
		if status, _ := writer.call(t, "POST", path+"-actions", body, nil); status != 400 {
			t.Fatalf("invalid body %d status=%d", i, status)
		}
	}
	if status, _ := writer.call(t, "POST", strings.TrimSuffix(path, "assessment")+"review-actions", valid, nil); status != 400 {
		t.Fatalf("new command reinterpreted by old URL: %d", status)
	}
	for _, query := range []string{"", "?through_version=-1", "?through_version=2", "?through_version=1&before_version=3", "?through_version=1&limit=101", "?through_version=1&limit=0", "?through_version=1&actor=any", "?through_version=1&through_version=1"} {
		if status, _ := writer.call(t, "GET", path+"/history"+query, nil, nil); status != 400 {
			t.Fatalf("invalid history %s status=%d", query, status)
		}
	}
	if status, _ := writer.call(t, "GET", assessmentPath(f.caseItem.ID, created.FindingID), nil, nil); status != 404 {
		t.Fatalf("cross-Case Finding read: %d", status)
	}
	for _, headers := range []map[string]string{{"Origin": "https://unrelated.invalid"}, {"X-CSRF-Token": "invalid"}} {
		if status, _ := writer.call(t, "POST", path+"/preview", workspace.BasisSelection{}, headers); status != 403 {
			t.Fatalf("readonly preview bypassed CSRF/origin: %d", status)
		}
	}
	view := assessmentCurrent(t, f, created.CaseID, created.FindingID)
	if view.Assessment.Version != 1 || view.CaseVersion != created.CaseVersion || len(view.History.Items) != 1 {
		t.Fatal("negative requests changed projection/history/Case version")
	}
	t.Log("Real HTTPS rejects actor spoofing, duplicate/unknown/missing JSON, unsupported verdict and missing support, old URL reinterpretation, invalid/future/duplicate history bounds, cross-Case identities and preview CSRF/origin without writes")
}

type lostCreationResponse struct {
	base    http.RoundTripper
	dropped bool
	body    []byte
}

func (x *lostCreationResponse) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := x.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if !x.dropped && r.Method == http.MethodPost && r.URL.Path == "/api/v1/security/reviews" && response.StatusCode == 200 {
		x.body, err = io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return nil, err
		}
		x.dropped = true
		return nil, errors.New("synthetic loss after creation commit")
	}
	return response, nil
}

func TestAssessmentCreationAfterActualCommittedResponseLoss(t *testing.T) {
	f := startResponsibilityProduction(t)
	client := responsibilityLogin(t, f, "reviewer.one")
	c := previewAssessmentCreation(t, f, newAssessmentCreation(t, workspace.BasisSelection{}))
	transport := &lostCreationResponse{base: client.client.Transport}
	client.client.Transport = transport
	raw, _ := json.Marshal(c)
	request, err := http.NewRequest("POST", client.base+"/api/v1/security/reviews", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", client.base)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", client.csrf)
	if response, err := client.client.Do(request); err == nil {
		response.Body.Close()
		t.Fatal("response loss was not injected")
	}
	var count int
	if err = f.admin.QueryRow(`SELECT count(*) FROM finding_review_creations WHERE operation_id=$1`, c.OperationID).Scan(&count); err != nil || count != 1 {
		t.Fatal("commit not established before lost response", count, err)
	}
	status, replay := client.call(t, "POST", "/api/v1/security/reviews", c, nil)
	if status != 200 || !bytes.Equal(replay, transport.body) {
		t.Fatalf("original creation receipt did not replay: %d", status)
	}
	if err = f.admin.QueryRow(`SELECT count(*) FROM finding_review_creations WHERE operation_id=$1`, c.OperationID).Scan(&count); err != nil || count != 1 {
		t.Fatal("lost result created another finding", count, err)
	}
	if status, _ = client.call(t, "POST", "/api/v1/access/logout", map[string]any{}, nil); status != 204 {
		t.Fatalf("logout: %d", status)
	}
	if status, _ = client.call(t, "POST", "/api/v1/security/reviews", c, nil); status != 401 {
		t.Fatalf("receipt replay bypassed current authentication: %d", status)
	}
	t.Log("Actual production HTTP creation committed exactly once before transport response loss; original unknown IDs and exact receipt recovered by the same actor/operation key")
}

func TestAssessmentGapUpdateAndSealRaces(t *testing.T) {
	for _, operation := range []string{"record_review", "record_progress"} {
		t.Run(operation, func(t *testing.T) {
			f := startResponsibilityProduction(t)
			ctx := context.Background()
			selected := assessmentFixtureSources(t, f)
			g := selected.Gaps[0]
			// The same Gap is intentionally adopted under two distinct relations.
			other := g
			other.Relation = "blocks"
			selected.Gaps = append(selected.Gaps, other)
			first, err := f.store.ResolveGap(ctx, assessmentActor(), g.GapID, workspace.ResolveGapInput{State: "investigating", Note: "ADOPTED BEFORE CONCURRENT UPDATE"}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			creation := previewAssessmentCreation(t, f, newAssessmentCreation(t, selected))
			created, err := f.store.CreateReview(ctx, assessmentActor(), creation, time.Now(), nil)
			if err != nil {
				t.Fatal(err)
			}
			var c workspace.AssessmentCommand
			if operation == "record_review" {
				c = assessmentCommand(t, f, created.CaseID, created.FindingID, selected, &workspace.ReviewPayload{Conclusion: "supported", Reason: "Reviewed the prior visible resolution"}, nil)
			} else {
				c = assessmentCommand(t, f, created.CaseID, created.FindingID, selected, nil, &workspace.ProgressPayload{State: "ended", Reason: "End this attempt on the reviewed basis", Closure: &workspace.Closure{Kind: "end_followup", Explanation: "Only this attempt ends", RemainingUnknowns: "Other executions remain unknown"}})
			}
			release, wait := responsibilityLockGate(t, f, `CREATE TRIGGER xi_c_test_gate AFTER INSERT ON case_finding_assessment_actions FOR EACH ROW EXECUTE FUNCTION xi_c_test_gate()`)
			type result struct {
				a   workspace.AssessmentAction
				err error
			}
			out := make(chan result, 1)
			go func() {
				a, e := f.store.ApplyFindingAssessment(ctx, created.CaseID, created.FindingID, assessmentActor(), c, time.Now())
				out <- result{a, e}
			}()
			wait(1)
			second, err := f.store.ResolveGap(ctx, assessmentActor(), g.GapID, workspace.ResolveGapInput{State: "accepted_limitation", Note: "CURRENT AFTER CONCURRENT UPDATE"}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			type sealedResult struct {
				c   workspace.Case
				err error
			}
			seals := make(chan sealedResult, 1)
			go func() {
				value, e := f.store.SealCase(ctx, created.CaseID, assessmentActor(), time.Now())
				seals <- sealedResult{value, e}
			}()
			wait(2)
			release()
			committed := <-out
			if committed.err != nil {
				t.Fatal(committed.err)
			}
			for _, adopted := range committed.a.Basis.AdoptedGaps {
				if adopted.ResolutionVersion != first.Version || adopted.Note != first.Note || !adopted.UpdatedAt.Equal(first.UpdatedAt) {
					t.Fatal("mixed prior version with current Gap content")
				}
			}
			sealed := <-seals
			if errors.Is(sealed.err, workspace.ErrConflict) {
				readback, e := f.store.GetCase(ctx, created.CaseID)
				if e != nil || readback.Case.Status != "open" || len(readback.Case.SealedPack) != 0 {
					t.Fatal("serialization conflict left partial seal", e)
				}
				sealed.c, sealed.err = f.store.SealCase(ctx, created.CaseID, assessmentActor(), time.Now())
			}
			if sealed.err != nil {
				t.Fatal(sealed.err)
			}
			var pack workspace.EvidencePack
			if json.Unmarshal(sealed.c.SealedPack, &pack) != nil || len(pack.Assessments.Actions) != 2 {
				t.Fatal("seal omitted the complete action")
			}
			for _, adopted := range pack.Assessments.Actions[1].Basis.AdoptedGaps {
				if adopted.Note != first.Note || adopted.ResolutionVersion != first.Version {
					t.Fatal("seal replaced historical Gap contents")
				}
			}
			v := assessmentCurrent(t, f, created.CaseID, created.FindingID)
			evaluation := v.ReviewBasis
			if operation == "record_progress" {
				evaluation = v.ProgressBasis
			}
			if evaluation.State != "changed" || evaluation.Current.AdoptedGaps[0].Note != second.Note {
				t.Fatal("next read failed to report concurrent Gap change")
			}
			if err = db.ValidateFindingAssessmentRestore(ctx, f.admin); err != nil {
				t.Fatal(err)
			}
			t.Log("PostgreSQL gate established adopted-before-update order; duplicate relations retain one exact old row version; concurrent seal commits complete history or cleanly rejects serialization; read-time evaluation exposes the new Gap contents")
		})
	}
}

func TestAssessmentSealWinsWithoutPartialAction(t *testing.T) {
	f := startResponsibilityProduction(t)
	ctx := context.Background()
	c := previewAssessmentCreation(t, f, newAssessmentCreation(t, workspace.BasisSelection{}))
	created, err := f.store.CreateReview(ctx, assessmentActor(), c, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	command := assessmentCommand(t, f, created.CaseID, created.FindingID, workspace.BasisSelection{}, &workspace.ReviewPayload{Conclusion: "pending_review", Reason: "Must not enter already sealed Case"}, nil)
	release, wait := responsibilityLockGate(t, f, `CREATE TRIGGER xi_c_test_gate AFTER UPDATE ON investigation_cases FOR EACH ROW WHEN (NEW.status='sealed') EXECUTE FUNCTION xi_c_test_gate()`)
	seals := make(chan error, 1)
	writes := make(chan error, 1)
	go func() { _, e := f.store.SealCase(ctx, created.CaseID, assessmentActor(), time.Now()); seals <- e }()
	wait(1)
	go func() {
		_, e := f.store.ApplyFindingAssessment(ctx, created.CaseID, created.FindingID, assessmentActor(), command, time.Now())
		writes <- e
	}()
	wait(2)
	release()
	if e := <-seals; e != nil {
		t.Fatal(e)
	}
	if e := <-writes; !errors.Is(e, workspace.ErrSealed) {
		t.Fatal("write after seal", e)
	}
	v := assessmentCurrent(t, f, created.CaseID, created.FindingID)
	if v.Assessment.Version != 1 || len(v.History.Items) != 1 || v.CaseStatus != "sealed" {
		t.Fatal("sealed rejection left a partial action")
	}
	if err = db.ValidateFindingAssessmentRestore(ctx, f.admin); err != nil {
		t.Fatal(err)
	}
}

func TestAssessmentRelevantReferencesAndFiniteClosures(t *testing.T) {
	f := startResponsibilityProduction(t)
	ctx := context.Background()
	selected := assessmentFixtureSources(t, f)
	c := newAssessmentCreation(t, selected)
	c.Review = &workspace.ReviewPayload{Conclusion: "supported", Reason: "Human selected the support"}
	c = previewAssessmentCreation(t, f, c)
	r, err := f.store.CreateReview(ctx, assessmentActor(), c, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	e := selected.Evidence[0]
	link := workspace.AddEvidenceInput{FindingID: r.FindingID, EvidenceEventID: e.EvidenceEventID, Relation: e.Relation, EventHash: e.EventHash, SourceRef: e.SourceRef}
	if _, err = f.store.AddEvidenceLink(ctx, r.CaseID, assessmentActor(), link, time.Now()); err != nil {
		t.Fatal(err)
	}
	if v := assessmentCurrent(t, f, r.CaseID, r.FindingID); v.ReviewBasis.State != "unchanged" {
		t.Fatal("same citation replay looked new")
	}
	if _, err = f.store.CreateAnnotation(ctx, r.CaseID, assessmentActor(), workspace.CreateAnnotationInput{TargetKind: "case", TargetRef: r.CaseID, Body: "Unrelated graph/Case commentary"}, time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if v := assessmentCurrent(t, f, r.CaseID, r.FindingID); v.ReviewBasis.State != "unchanged" {
		t.Fatal("unrelated Case version changed basis")
	}
	link.Relation = "contradicts"
	if _, err = f.store.AddEvidenceLink(ctx, r.CaseID, assessmentActor(), link, time.Now()); err != nil {
		t.Fatal(err)
	}
	if v := assessmentCurrent(t, f, r.CaseID, r.FindingID); v.ReviewBasis.State != "changed" {
		t.Fatal("new Finding citation missed")
	}
	// Reviewer knows the contradiction exists, but explicitly does not use it.
	applyAssessment(t, f, r.CaseID, r.FindingID, assessmentCommand(t, f, r.CaseID, r.FindingID, selected, &workspace.ReviewPayload{Conclusion: "supported", Reason: "Still select this support; counter citation acknowledged but not adopted"}, nil))
	if v := assessmentCurrent(t, f, r.CaseID, r.FindingID); v.ReviewBasis.State != "unchanged" || len(v.Assessment.Review.Basis.Known.Evidence) != 2 || len(v.Assessment.Review.Basis.Selected.Evidence) != 1 {
		t.Fatal("known unselected citation incorrectly treated as newly arriving")
	}
	verified := time.Now().Add(-time.Minute)
	verifiedClosure := &workspace.Closure{Kind: "verified_remediation", Explanation: "Human checked post-fix execution result", Scope: "Exactly this synthetic execution", VerifiedAt: &verified, VerificationEvidence: selected.Evidence}
	applyAssessment(t, f, r.CaseID, r.FindingID, assessmentCommand(t, f, r.CaseID, r.FindingID, selected, nil, &workspace.ProgressPayload{State: "ended", Reason: "Human verification complete", Closure: verifiedClosure}))
	applyAssessment(t, f, r.CaseID, r.FindingID, assessmentCommand(t, f, r.CaseID, r.FindingID, selected, nil, &workspace.ProgressPayload{State: "in_progress", Reason: "Reopen to examine counterevidence"}))
	counter := selected
	counter.Evidence = append([]workspace.EvidenceChoice{}, selected.Evidence...)
	counter.Evidence[0].Relation = "contradicts"
	applyAssessment(t, f, r.CaseID, r.FindingID, assessmentCommand(t, f, r.CaseID, r.FindingID, counter, &workspace.ReviewPayload{Conclusion: "not_supported", Reason: "Counterevidence reviewed on this basis"}, nil))
	applyAssessment(t, f, r.CaseID, r.FindingID, assessmentCommand(t, f, r.CaseID, r.FindingID, counter, nil, &workspace.ProgressPayload{State: "ended", Reason: "Human ruled out this judgment", Closure: &workspace.Closure{Kind: "ruled_out", Explanation: "Current selected counterevidence rules out this claim"}}))
	if err = db.ValidateFindingAssessmentRestore(ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	t.Log("Real registered references: idempotent replay and unrelated Case versions do not prompt reassessment; new relevant links do; known-unselected links remain known. Human verified remediation, reopen and evidence-backed rule-out preserve separate verdict/progress history")
}
