package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestAssessmentProductionBrowserHarness(t *testing.T) {
	if os.Getenv("XINSIGHT_ASSESSMENT_BROWSER") != "1" {
		t.Skip("explicit local assessment browser harness required")
	}
	output := os.Getenv("XINSIGHT_ASSESSMENT_BROWSER_OUTPUT")
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		t.Fatal("absolute clean output required")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	f := startResponsibilityProduction(t)
	selection := assessmentFixtureSources(t, f)
	config := f.config
	config.Accounts = append(append([]access.AccountConfigEntry{}, config.Accounts...), access.AccountConfigEntry{SubjectID: "xs_5555555555555555", Username: "investigation.reader", Role: access.RoleInvestigationReader, PasswordHash: config.Accounts[0].PasswordHash})
	f.reloadAccounts(t, config)
	if _, err := f.store.ResolveGap(context.Background(), assessmentActor(), selection.Gaps[0].GapID, workspace.ResolveGapInput{State: "investigating", Note: "Browser original adopted Gap content"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// A bounded preexisting review also permits theme/asset checks after a
	// stylesheet-only rebuild without replaying the already verified workflow.
	themeCommand := newAssessmentCreation(t, selection)
	themeCommand.NewCase.Title = "主题与依据显示核验"
	themeCommand = previewAssessmentCreation(t, f, themeCommand)
	themeReceipt, err := f.store.CreateReview(context.Background(), assessmentActor(), themeCommand, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var databaseName string
	if err := f.admin.QueryRow("SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	var executionID string
	if err := f.admin.QueryRow(`SELECT execution_id FROM evidence_events WHERE id=$1::uuid`, selection.Evidence[0].EvidenceEventID).Scan(&executionID); err != nil {
		t.Fatal(err)
	}
	ready := map[string]any{"base_url": f.server.URL, "database_name": databaseName, "execution_id": executionID, "existing_case_id": f.caseItem.ID, "selection": selection, "usernames": []string{"reviewer.one", "investigation.reader"}, "evidence_boundary": "local synthetic evidence / native PG16 / production TLS human sessions and embedded UI"}
	ready["theme_case_id"], ready["theme_finding_id"] = themeReceipt.CaseID, themeReceipt.FindingID
	raw, err := json.MarshalIndent(ready, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(output, "ready.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-t.Context().Done():
			t.Fatal("browser harness cancelled")
		case <-ticker.C:
			if _, err = os.Stat(filepath.Join(output, "stop")); err == nil {
				return
			}
		}
	}
}

func assessmentFixtureSources(t *testing.T, f *responsibilityFixture) workspace.BasisSelection {
	t.Helper()
	ctx := context.Background()
	store, err := db.OpenEvidenceStore(ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot, err := unirouter.LoadSnapshot(filepath.Join("..", "..", "fixtures", "unirouter_snapshot", "missing_trace_gap.json"))
	if err != nil {
		t.Fatal(err)
	}
	sourceID, manifestID := randomTestUUID(t), randomTestUUID(t)
	snapshot.SourceInstance = "assessment-local-" + sourceID[:8]
	requestID := "req-assessment-" + sourceID[:8]
	snapshot.RequestIDs = []string{requestID}
	for i := range snapshot.Scenarios {
		snapshot.Scenarios[i].RequestID = requestID
	}
	for i := range snapshot.CallLogs {
		snapshot.CallLogs[i].RequestID = requestID
		snapshot.CallLogs[i].SourceRef = ""
	}
	for i := range snapshot.TraceEvents {
		snapshot.TraceEvents[i].RequestID = requestID
		snapshot.TraceEvents[i].SourceRef = ""
	}
	for i := range snapshot.CaptureGaps {
		snapshot.CaptureGaps[i].RequestID = requestID
	}
	events, err := unirouter.BuildSnapshotEvents(snapshot, unirouter.SnapshotImportOptions{SourceSystemID: sourceID, AdapterManifestID: manifestID})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		hash, e := evidence.GenerateEventHash(event)
		if e != nil {
			t.Fatal(e)
		}
		event["event_hash"] = hash
	}
	if _, err = store.PersistEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	e := workspace.EvidenceChoice{Scope: "finding", Relation: "supports"}
	g := workspace.GapChoice{Scope: "finding", Relation: "limits"}
	if err = f.admin.QueryRow(`SELECT id::text,event_hash,source_ref FROM evidence_events WHERE source_system_id=$1::uuid ORDER BY id LIMIT 1`, sourceID).Scan(&e.EvidenceEventID, &e.EventHash, &e.SourceRef); err != nil {
		t.Fatal(err)
	}
	if err = f.admin.QueryRow(`SELECT id::text,affected_claim,gap_type FROM capture_gaps WHERE source_system_id=$1::uuid ORDER BY id LIMIT 1`, sourceID).Scan(&g.GapID, &g.AffectedClaim, &g.GapType); err != nil {
		t.Fatal(err)
	}
	return workspace.BasisSelection{Evidence: []workspace.EvidenceChoice{e}, Gaps: []workspace.GapChoice{g}}
}

func assessmentPath(caseID, findingID string) string {
	return "/api/v1/cases/" + caseID + "/findings/" + findingID + "/assessment"
}

func newAssessmentCreation(t *testing.T, selection workspace.BasisSelection) workspace.ReviewCreationCommand {
	t.Helper()
	return workspace.ReviewCreationCommand{Operation: "start_review", OperationID: randomTestUUID(t), NewCase: &workspace.CreateCaseInput{Title: "Synthetic review Case", Description: "Local human lifecycle verification"}, Finding: workspace.CreateFindingInput{Title: "A bounded human assertion", Statement: "The selected trace supports only this execution", Classification: "hypothesis"}, Selection: selection, Review: &workspace.ReviewPayload{Conclusion: "pending_review", Reason: "Begin a human review"}, Reason: "Start from explicitly selected references"}
}

func previewAssessmentCreation(t *testing.T, f *responsibilityFixture, c workspace.ReviewCreationCommand) workspace.ReviewCreationCommand {
	t.Helper()
	basis, err := f.store.PreviewReviewCreation(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	c.ExpectedBasisDigest = basis.Digest
	return c
}

func assessmentCurrent(t *testing.T, f *responsibilityFixture, caseID, findingID string) workspace.AssessmentView {
	t.Helper()
	v, err := f.store.GetFindingAssessment(context.Background(), caseID, findingID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func assessmentCommand(t *testing.T, f *responsibilityFixture, caseID, findingID string, selection workspace.BasisSelection, r *workspace.ReviewPayload, p *workspace.ProgressPayload) workspace.AssessmentCommand {
	t.Helper()
	v := assessmentCurrent(t, f, caseID, findingID)
	b, err := f.store.PreviewFindingBasis(context.Background(), caseID, findingID, selection)
	if err != nil {
		t.Fatal(err)
	}
	op := "record_review"
	if p != nil {
		op = "record_progress"
	}
	return workspace.AssessmentCommand{Operation: op, OperationID: randomTestUUID(t), ExpectedVersion: v.Assessment.Version, ExpectedCaseVersion: v.CaseVersion, ExpectedBasisDigest: b.Digest, Selection: b.Selected, Review: r, Progress: p}
}

func assessmentActor() workspace.Actor {
	return workspace.Actor{Kind: "human", Ref: "xs_1111111111111111"}
}

func applyAssessment(t *testing.T, f *responsibilityFixture, caseID, findingID string, c workspace.AssessmentCommand) workspace.AssessmentAction {
	t.Helper()
	a, err := f.store.ApplyFindingAssessment(context.Background(), caseID, findingID, assessmentActor(), c, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAssessmentProductionLifecycleAndPermissions(t *testing.T) {
	f := startResponsibilityProduction(t)
	ctx := context.Background()
	selection := assessmentFixtureSources(t, f)
	config := f.config
	config.Accounts = append(append([]access.AccountConfigEntry{}, config.Accounts...), access.AccountConfigEntry{SubjectID: "xs_5555555555555555", Username: "investigation.reader", Role: access.RoleInvestigationReader, PasswordHash: config.Accounts[0].PasswordHash})
	f.reloadAccounts(t, config)
	writer := responsibilityLogin(t, f, "reviewer.one")
	reader := responsibilityLogin(t, f, "investigation.reader")
	workbench := responsibilityLogin(t, f, "assignee.reader")
	gap := selection.Gaps[0]
	firstResolution, err := f.store.ResolveGap(ctx, assessmentActor(), gap.GapID, workspace.ResolveGapInput{State: "investigating", Note: "FIRST: examined the initial trace only"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c := newAssessmentCreation(t, selection)
	c.Review = &workspace.ReviewPayload{Conclusion: "supported", Reason: "Human reviewed this support and limitation"}
	var before int
	if err = f.admin.QueryRow(`SELECT count(*) FROM investigation_cases`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	status, raw := reader.call(t, "POST", "/api/v1/security/reviews/preview", c, nil)
	var basis workspace.ReviewBasis
	if status != 200 || json.Unmarshal(raw, &basis) != nil || basis.AdoptedGaps[0].Note != firstResolution.Note {
		t.Fatalf("readonly preview status=%d body=%s", status, raw)
	}
	c.ExpectedBasisDigest = basis.Digest
	var after int
	if err = f.admin.QueryRow(`SELECT count(*) FROM investigation_cases`).Scan(&after); err != nil || after != before {
		t.Fatal("preview wrote a Case")
	}
	for _, client := range []*responsibilityClient{reader, workbench} {
		if status, _ = client.call(t, "POST", "/api/v1/security/reviews", c, nil); status != 403 {
			t.Fatalf("unauthorized create=%d", status)
		}
	}
	for _, headers := range []map[string]string{{"X-CSRF-Token": "wrong"}, {"Origin": "https://elsewhere.invalid"}, {"Authorization": "Bearer synthetic-not-a-real-credential"}} {
		if status, _ = writer.call(t, "POST", "/api/v1/security/reviews", c, headers); status != 403 && status != 401 {
			t.Fatalf("creation auth bypass=%d", status)
		}
	}
	status, first := writer.call(t, "POST", "/api/v1/security/reviews", c, nil)
	var created workspace.ReviewCreationReceipt
	if status != 200 || json.Unmarshal(first, &created) != nil || created.Action == nil || created.Action.Current.Progress.State != "not_started" {
		t.Fatalf("atomic start=%d %s", status, first)
	}
	path := assessmentPath(created.CaseID, created.FindingID)
	status, raw = reader.call(t, "GET", path, nil, nil)
	var view workspace.AssessmentView
	if status != 200 || json.Unmarshal(raw, &view) != nil || len(view.AllowedActions) != 0 || view.Assessment.Review.Basis.AdoptedGaps[0].ResolutionVersion != 1 {
		t.Fatalf("readonly read=%d", status)
	}
	responsibility, err := f.store.GetFindingReview(ctx, created.CaseID, created.FindingID)
	if err != nil || responsibility.Followup.Recorded {
		t.Fatal("creation inferred responsibility")
	}
	start := assessmentCommand(t, f, created.CaseID, created.FindingID, selection, nil, &workspace.ProgressPayload{State: "in_progress", Reason: "Begin follow-up"})
	if status, _ = reader.call(t, "POST", path+"-actions", start, nil); status != 403 {
		t.Fatalf("reader mutation=%d", status)
	}
	if status, _ = writer.call(t, "POST", path+"-actions", start, nil); status != 200 {
		t.Fatalf("progress=%d", status)
	}
	due := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	zone := "UTC"
	v := assessmentCurrent(t, f, created.CaseID, created.FindingID)
	oldCommand := workspace.FollowupCommand{Operation: "update_followup", OperationID: randomTestUUID(t), ExpectedCaseVersion: v.CaseVersion, Payload: workspace.FollowupPayload{DueAt: &due, DueTimezone: &zone, Reason: "Explicit overdue test"}}
	oldPath := strings.TrimSuffix(path, "assessment") + "review-actions"
	if status, raw = writer.call(t, "POST", oldPath, oldCommand, nil); status != 200 {
		t.Fatalf("old responsibility=%d %s", status, raw)
	}
	v = assessmentCurrent(t, f, created.CaseID, created.FindingID)
	if !v.Overdue || v.Assessment.Review.Conclusion != "supported" {
		t.Fatal("overdue/state separation")
	}
	stale := assessmentCommand(t, f, created.CaseID, created.FindingID, selection, &workspace.ReviewPayload{Conclusion: "supported", Reason: "Stale preview must fail"}, nil)
	secondResolution, err := f.store.ResolveGap(ctx, assessmentActor(), gap.GapID, workspace.ResolveGapInput{State: "accepted_limitation", Note: "SECOND: later note does not replace the adopted first note"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ApplyFindingAssessment(ctx, created.CaseID, created.FindingID, assessmentActor(), stale, time.Now()); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("stale Gap preview=%v", err)
	}
	v = assessmentCurrent(t, f, created.CaseID, created.FindingID)
	if v.ReviewBasis.State != "changed" || v.Assessment.Review.Basis.AdoptedGaps[0].Note != firstResolution.Note || v.ReviewBasis.Current.AdoptedGaps[0].Note != secondResolution.Note || v.Assessment.Progress.State != "in_progress" {
		t.Fatal("past/current Gap contents or no-auto-transition boundary")
	}
	cl := &workspace.Closure{Kind: "end_followup", Explanation: "Stop this bounded attempt", RemainingUnknowns: "Unobserved executions remain unknown"}
	end := assessmentCommand(t, f, created.CaseID, created.FindingID, selection, nil, &workspace.ProgressPayload{State: "ended", Reason: "End current follow-up", Closure: cl})
	ended := applyAssessment(t, f, created.CaseID, created.FindingID, end)
	if ended.Basis.AdoptedGaps[0].Note != secondResolution.Note || ended.Current.Review.Basis.AdoptedGaps[0].Note != firstResolution.Note {
		t.Fatal("progress basis overwrote review basis")
	}
	v = assessmentCurrent(t, f, created.CaseID, created.FindingID)
	if v.Overdue || v.Assessment.Review.Conclusion != "supported" {
		t.Fatal("ended follow-up still overdue or altered review")
	}
	status, replay := writer.call(t, "POST", "/api/v1/security/reviews", c, nil)
	if status != 200 || !bytes.Equal(first, replay) {
		t.Fatal("creation retry after later actions changed receipt")
	}
	changed := c
	changed.Reason = "Different command"
	if status, _ = writer.call(t, "POST", "/api/v1/security/reviews", changed, nil); status != 409 {
		t.Fatal("same creation key changed content accepted")
	}
	status, raw = reader.call(t, "GET", path+"/history?through_version=3&limit=1", nil, nil)
	var history workspace.AssessmentHistory
	if status != 200 || json.Unmarshal(raw, &history) != nil || history.NextBefore == nil || history.Items[0].Current.Version != 3 {
		t.Fatal("history fixed upper page")
	}
	applyAssessment(t, f, created.CaseID, created.FindingID, assessmentCommand(t, f, created.CaseID, created.FindingID, selection, nil, &workspace.ProgressPayload{State: "in_progress", Reason: "Reopen for explicit new work"}))
	status, raw = reader.call(t, "GET", path+"/history?through_version=3&before_version=3&limit=10", nil, nil)
	if status != 200 || json.Unmarshal(raw, &history) != nil || len(history.Items) != 2 || history.Items[0].Current.Version != 2 {
		t.Fatal("history moved upper bound")
	}
	if status, _ = writer.call(t, "POST", "/api/v1/cases/"+created.CaseID+"/seal", map[string]any{}, nil); status != 200 {
		t.Fatalf("seal=%d", status)
	}
	status, sealedBytes := reader.call(t, "GET", "/api/v1/cases/"+created.CaseID+"/evidence-pack.json", nil, nil)
	var pack workspace.EvidencePack
	if status != 200 || json.Unmarshal(sealedBytes, &pack) != nil || pack.SchemaVersion != workspace.EvidencePackSchema || pack.Assessments.Actions[0].Basis.AdoptedGaps[0].Note != firstResolution.Note {
		t.Fatal("v3 omitted adopted Gap history")
	}
	if err = db.ValidateFindingAssessmentRestore(ctx, f.admin); err != nil {
		t.Fatal("valid historical contents rejected after current Gap changed", err)
	}
	continuation := newAssessmentCreation(t, workspace.BasisSelection{})
	continuation.Operation = "continue_finding"
	continuation.Review = nil
	continuation.From = &workspace.SealedFindingReference{FindingReference: workspace.FindingReference{CaseID: created.CaseID, FindingID: created.FindingID}, Revision: pack.Revision, Digest: responsibilityDigest(sealedBytes)}
	continuation = previewAssessmentCreation(t, f, continuation)
	continuationPath := strings.TrimSuffix(path, "assessment") + "continuations"
	if status, _ = reader.call(t, "POST", continuationPath, continuation, nil); status != 403 {
		t.Fatal("reader continued sealed finding")
	}
	status, raw = writer.call(t, "POST", continuationPath, continuation, nil)
	var next workspace.ReviewCreationReceipt
	if status != 200 || json.Unmarshal(raw, &next) != nil || next.Action != nil {
		t.Fatalf("continuation=%d %s", status, raw)
	}
	nextState := assessmentCurrent(t, f, next.CaseID, next.FindingID)
	if nextState.Assessment.Recorded || nextState.Assessment.Review != nil || nextState.Assessment.Progress != nil {
		t.Fatal("continuation copied historical judgment")
	}
	status, raw = reader.call(t, "GET", "/api/v1/cases/"+created.CaseID+"/evidence-pack.json", nil, nil)
	if status != 200 || !bytes.Equal(sealedBytes, raw) {
		t.Fatal("continuation changed exact sealed bytes")
	}
	if status, _ = reader.call(t, "GET", assessmentPath(f.caseItem.ID, created.FindingID), nil, nil); status != 404 {
		t.Fatal("cross-Case Finding escaped")
	}
	if err = db.ValidateFindingResponsibilityRestore(ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	if err = db.ValidateFindingAssessmentRestore(ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	// Preserve the original row and deliberately damage only a finite historical
	// note, as a corrupt backup could. No current Gap JOIN can repair this.
	var original []byte
	if err = f.admin.QueryRow(`SELECT receipt FROM case_finding_assessment_actions WHERE case_id=$1 AND finding_id=$2 AND result_version=1`, created.CaseID, created.FindingID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.Exec(`UPDATE case_finding_assessment_actions SET receipt=jsonb_set(receipt,'{basis,adopted_gaps,0,note}','"CORRUPT"'::jsonb) WHERE case_id=$1 AND finding_id=$2 AND result_version=1`, created.CaseID, created.FindingID); err != nil {
		t.Fatal(err)
	}
	if err = db.ValidateFindingAssessmentRestore(ctx, f.admin); err == nil {
		t.Fatal("corrupt adopted content accepted")
	}
	if _, err = f.admin.Exec(`UPDATE case_finding_assessment_actions SET receipt=$3 WHERE case_id=$1 AND finding_id=$2 AND result_version=1`, created.CaseID, created.FindingID, original); err != nil {
		t.Fatal(err)
	}
	if err = db.ValidateFindingAssessmentRestore(ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	t.Log("Production role/HTTP: readonly preview and reads; atomic start, old responsibility, review/progress separation, exact v1/v2 Gap contents, fixed history, v3 seal and unreviewed atomic continuation; corrupted adopted snapshot rejected")
}

func TestAssessmentAtomicCreationRetryRollbackAndOldCommandConflict(t *testing.T) {
	f := startResponsibilityProduction(t)
	ctx := context.Background()
	c := previewAssessmentCreation(t, f, newAssessmentCreation(t, workspace.BasisSelection{}))
	start := make(chan struct{})
	var wg sync.WaitGroup
	type answer struct {
		r   workspace.ReviewCreationReceipt
		err error
	}
	out := make(chan answer, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, e := f.store.CreateReview(ctx, assessmentActor(), c, time.Now(), nil)
			out <- answer{r, e}
		}()
	}
	close(start)
	wg.Wait()
	close(out)
	var first workspace.ReviewCreationReceipt
	for a := range out {
		if a.err != nil {
			t.Fatal(a.err)
		}
		if first.CaseID == "" {
			first = a.r
		} else if !workspace.SameAssessmentValue(first, a.r) {
			t.Fatal("concurrent creation produced different identities")
		}
	}
	var count int
	if err := f.admin.QueryRow(`SELECT count(*) FROM finding_review_creations WHERE actor_subject_id=$1 AND operation_id=$2`, assessmentActor().Ref, c.OperationID).Scan(&count); err != nil || count != 1 {
		t.Fatal("creation receipt count", count, err)
	}
	// The second path creates inside an existing open Case, with no new Case.
	v := assessmentCurrent(t, f, first.CaseID, first.FindingID)
	existing := newAssessmentCreation(t, workspace.BasisSelection{})
	existing.NewCase = nil
	existing.CaseID = first.CaseID
	existing.ExpectedCaseVersion = v.CaseVersion
	existing = previewAssessmentCreation(t, f, existing)
	r, err := f.store.CreateReview(ctx, assessmentActor(), existing, time.Now(), nil)
	if err != nil || r.CaseID != first.CaseID || r.FindingID == first.FindingID {
		t.Fatal("existing-Case atomic creation", err)
	}
	newCommand := assessmentCommand(t, f, first.CaseID, first.FindingID, workspace.BasisSelection{}, &workspace.ReviewPayload{Conclusion: "pending_review", Reason: "Concurrent new review"}, nil)
	oldCommand := workspace.FollowupCommand{Operation: "update_followup", OperationID: randomTestUUID(t), ExpectedCaseVersion: newCommand.ExpectedCaseVersion, Payload: workspace.FollowupPayload{Reason: "Concurrent responsibility"}}
	barrier := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-barrier
		_, e := f.store.ApplyFindingAssessment(ctx, first.CaseID, first.FindingID, assessmentActor(), newCommand, time.Now())
		results <- e
	}()
	go func() {
		<-barrier
		_, e := f.store.ApplyFindingFollowup(ctx, first.CaseID, first.FindingID, assessmentActor(), oldCommand, workspace.AssigneeLookup{}, time.Now())
		results <- e
	}()
	close(barrier)
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		e := <-results
		if e == nil {
			success++
		} else if errors.Is(e, workspace.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("cross protocol conflict=%d/%d", success, conflict)
	}
	_, err = f.admin.Exec(`CREATE FUNCTION xi_assessment_commit_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic deferred commit failure'; END $$;
CREATE CONSTRAINT TRIGGER xi_assessment_commit_failure AFTER INSERT ON finding_review_creations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION xi_assessment_commit_failure()`)
	if err != nil {
		t.Fatal(err)
	}
	failing := previewAssessmentCreation(t, f, newAssessmentCreation(t, workspace.BasisSelection{}))
	counts := func() string {
		var value string
		if e := f.admin.QueryRow(`SELECT concat((SELECT count(*) FROM investigation_cases),':',(SELECT count(*) FROM case_findings),':',(SELECT count(*) FROM case_finding_assessments),':',(SELECT count(*) FROM case_finding_assessment_actions),':',(SELECT count(*) FROM finding_review_creations))`).Scan(&value); e != nil {
			t.Fatal(e)
		}
		return value
	}
	before := counts()
	if _, err = f.store.CreateReview(ctx, assessmentActor(), failing, time.Now(), nil); err == nil {
		t.Fatal("deferred failure ignored")
	}
	if after := counts(); after != before {
		t.Fatalf("failed commit left partial rows: %s -> %s", before, after)
	}
	if _, err = f.admin.Exec(`DROP TRIGGER xi_assessment_commit_failure ON finding_review_creations; DROP FUNCTION xi_assessment_commit_failure()`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.CreateReview(ctx, assessmentActor(), failing, time.Now(), nil); err != nil {
		t.Fatal("same command could not recover after rollback", err)
	}
	if err = db.ValidateFindingAssessmentRestore(ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	t.Log("Concurrent first-use creation returned one identity and receipt; existing-Case path and cross-old/new version conflict passed; deferred commit failure left zero partial Case/Finding/projection/action/receipt rows")
}

func TestAssessmentDuplicateDisjointConcurrentCycle(t *testing.T) {
	f := startResponsibilityProduction(t)
	ctx := context.Background()
	records := make([]workspace.ReviewCreationReceipt, 4)
	for i := range records {
		c := previewAssessmentCreation(t, f, newAssessmentCreation(t, workspace.BasisSelection{}))
		r, err := f.store.CreateReview(ctx, assessmentActor(), c, time.Now(), nil)
		if err != nil {
			t.Fatal(err)
		}
		records[i] = r
	}
	duplicate := func(from, to int) workspace.AssessmentCommand {
		r, target := records[from], records[to]
		return assessmentCommand(t, f, r.CaseID, r.FindingID, workspace.BasisSelection{}, nil, &workspace.ProgressPayload{State: "ended", Reason: fmt.Sprintf("Same bounded claim as %d", to), Closure: &workspace.Closure{Kind: "duplicate", Explanation: "Human compared the identical statement", Duplicate: &workspace.FindingReference{CaseID: target.CaseID, FindingID: target.FindingID}}})
	}
	applyAssessment(t, f, records[1].CaseID, records[1].FindingID, duplicate(1, 2))
	applyAssessment(t, f, records[3].CaseID, records[3].FindingID, duplicate(3, 0))
	// Existing B→C and D→A. Concurrent A→B and C→D have disjoint endpoint
	// Case pairs; both passing a snapshot check would form A→B→C→D→A.
	commands := []workspace.AssessmentCommand{duplicate(0, 1), duplicate(2, 3)}
	start := make(chan struct{})
	out := make(chan error, 2)
	for i, source := range []int{0, 2} {
		go func(i, source int) {
			<-start
			r := records[source]
			_, err := f.store.ApplyFindingAssessment(ctx, r.CaseID, r.FindingID, assessmentActor(), commands[i], time.Now())
			out <- err
		}(i, source)
	}
	close(start)
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		err := <-out
		if err == nil {
			success++
		} else if errors.Is(err, workspace.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("disjoint cycle outcomes=%d/%d", success, conflict)
	}
	if err := db.ValidateFindingAssessmentRestore(ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	t.Log("Native PostgreSQL concurrent disjoint endpoint changes: exactly one commit and one cycle conflict; persisted duplicate graph remains acyclic")
}
