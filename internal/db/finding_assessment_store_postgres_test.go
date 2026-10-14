package db_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workspace"
)

// Reuse the real migrated Case fixture and its one Evidence/Gap pair. This
// checks the Store's persisted history and recovery contract directly, without
// changing HTTP tests or treating their execution as uncollected DB coverage.
func testAssessmentStoreHistory(t *testing.T, ctx context.Context, store *db.WorkspaceStore, database *sql.DB, actor workspace.Actor, now time.Time, selection workspace.BasisSelection) {
	t.Helper()
	countCases := func() int {
		t.Helper()
		var count int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM investigation_cases`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	command := workspace.ReviewCreationCommand{Operation: "start_review", OperationID: testUUID(t),
		NewCase:   &workspace.CreateCaseInput{Title: "Direct Store review"},
		Finding:   workspace.CreateFindingInput{Title: "Bounded assertion", Statement: "One selected event with an explicit source limitation", Classification: "hypothesis"},
		Selection: selection, Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Human inspected this exact event"}, Reason: "Begin a bounded review"}
	before := countCases()
	basis, err := store.PreviewReviewCreation(ctx, command)
	if err != nil || countCases() != before || len(basis.AdoptedGaps) != 1 || basis.AdoptedGaps[0].ResolutionVersion != 2 {
		t.Fatalf("read-only creation preview: basis=%+v err=%v", basis, err)
	}
	command.ExpectedBasisDigest = "sha256:" + strings.Repeat("f", 64)
	if _, err := store.CreateReview(ctx, actor, command, now, nil); !errors.Is(err, workspace.ErrConflict) || countCases() != before {
		t.Fatalf("stale basis left a partial Case: %v", err)
	}
	command.ExpectedBasisDigest = basis.Digest
	created, err := store.CreateReview(ctx, actor, command, now, nil)
	if err != nil || created.Action == nil || created.Action.Current.Version != 1 || countCases() != before+1 {
		t.Fatalf("atomic initial review: %+v err=%v", created, err)
	}
	replayed, err := store.CreateReview(ctx, actor, command, now.Add(time.Minute), nil)
	if err != nil || !workspace.SameAssessmentValue(created, replayed) || countCases() != before+1 {
		t.Fatalf("creation receipt replay changed identity or contents: %v", err)
	}
	changed := command
	changed.Reason = "A different command with the same identity"
	if _, err := store.CreateReview(ctx, actor, changed, now, nil); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("same identity accepted changed command: %v", err)
	}
	view, err := store.GetFindingAssessment(ctx, created.CaseID, created.FindingID, now)
	if err != nil || view.ReviewBasis.State != "unchanged" || view.Assessment.Progress.State != "not_started" {
		t.Fatalf("initial current state: %+v err=%v", view, err)
	}
	// Responsibility shares the Case version but neither changes the verdict
	// nor starts progress. A later disabled account keeps its historical name.
	due, zone, subject := now.Add(-time.Hour).UTC(), "UTC", "xs_3333333333333333"
	duty := workspace.FollowupCommand{Operation: "update_followup", OperationID: testUUID(t), ExpectedCaseVersion: view.CaseVersion,
		Payload: workspace.FollowupPayload{AssigneeSubjectID: &subject, DueAt: &due, DueTimezone: &zone, Reason: "Assign this bounded follow-up"}}
	assigned, err := store.ApplyFindingFollowup(ctx, created.CaseID, created.FindingID, actor, duty,
		workspace.AssigneeLookup{SubjectID: subject, Name: "Historical owner", Active: true}, now)
	if err != nil || assigned.Current.Version != 1 {
		t.Fatalf("assign responsibility: %+v err=%v", assigned, err)
	}
	if replay, err := store.ApplyFindingFollowup(ctx, created.CaseID, created.FindingID, actor, duty, workspace.AssigneeLookup{}, now); err != nil || !workspace.SameAssessmentValue(replay, assigned) {
		t.Fatalf("responsibility receipt depended on current account availability: %v", err)
	}
	changedDuty := duty
	changedDuty.Payload.Reason = "Changed contents under the same identity"
	if _, err := store.ApplyFindingFollowup(ctx, created.CaseID, created.FindingID, actor, changedDuty, workspace.AssigneeLookup{}, now); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("responsibility replay accepted changed contents: %v", err)
	}
	changedDuty.OperationID, changedDuty.ExpectedVersion, changedDuty.ExpectedCaseVersion = testUUID(t), 1, assigned.CaseVersion
	updated, err := store.ApplyFindingFollowup(ctx, created.CaseID, created.FindingID, actor, changedDuty, workspace.AssigneeLookup{}, now)
	if err != nil || updated.Current.AssigneeName != assigned.Current.AssigneeName {
		t.Fatalf("unchanged assignee lost historical name: %+v err=%v", updated, err)
	}
	dutyView, err := store.GetFindingReview(ctx, created.CaseID, created.FindingID)
	if err != nil || dutyView.CaseVersion != updated.CaseVersion || dutyView.Followup.Version != 2 || len(dutyView.History.Items) != 2 {
		t.Fatalf("responsibility current/history diverged: %+v err=%v", dutyView, err)
	}
	dutyPage, err := store.GetFindingHistory(ctx, created.CaseID, created.FindingID, 2, 0, 1)
	if err != nil || len(dutyPage.Items) != 1 || dutyPage.Items[0].Current.Version != 2 || dutyPage.NextBefore == nil {
		t.Fatalf("responsibility history first page: %+v err=%v", dutyPage, err)
	}
	dutyPage, err = store.GetFindingHistory(ctx, created.CaseID, created.FindingID, 2, *dutyPage.NextBefore, 1)
	if err != nil || len(dutyPage.Items) != 1 || dutyPage.Items[0].Current.Version != 1 || dutyPage.NextBefore != nil {
		t.Fatalf("responsibility history second page: %+v err=%v", dutyPage, err)
	}
	if _, err := store.GetFindingHistory(ctx, created.CaseID, created.FindingID, 3, 0, 1); !errors.Is(err, workspace.ErrInvalid) {
		t.Fatalf("future responsibility history bound accepted: %v", err)
	}
	view, err = store.GetFindingAssessment(ctx, created.CaseID, created.FindingID, now)
	if err != nil || !view.Overdue || view.Assessment.Version != 1 || view.Assessment.Progress.State != "not_started" || view.Assessment.Review.Conclusion != "supported" {
		t.Fatalf("responsibility changed assessment or hid overdue state: %+v err=%v", view, err)
	}
	end := workspace.AssessmentCommand{Operation: "record_progress", OperationID: testUUID(t), ExpectedVersion: 1,
		ExpectedCaseVersion: view.CaseVersion, ExpectedBasisDigest: basis.Digest, Selection: selection,
		Progress: &workspace.ProgressPayload{State: "ended", Reason: "End this bounded attempt", Closure: &workspace.Closure{Kind: "end_followup", Explanation: "No further local source is available", RemainingUnknowns: "Other executions have not been observed"}}}
	ended, err := store.ApplyFindingAssessment(ctx, created.CaseID, created.FindingID, actor, end, now.Add(time.Minute))
	if err != nil || ended.Current.Progress.State != "ended" || ended.Current.Review.Conclusion != "supported" {
		t.Fatalf("progress changed the conclusion: %+v err=%v", ended, err)
	}
	if replay, err := store.ApplyFindingAssessment(ctx, created.CaseID, created.FindingID, actor, end, now.Add(2*time.Minute)); err != nil || !workspace.SameAssessmentValue(replay, ended) {
		t.Fatalf("action receipt replay changed contents: %v", err)
	}
	oldGap := ended.Basis.AdoptedGaps[0]
	resolution, err := store.ResolveGap(ctx, actor, selection.Gaps[0].GapID, workspace.ResolveGapInput{State: "investigating", Note: "A later source account"}, now.Add(2*time.Minute))
	if err != nil || resolution.Version != oldGap.ResolutionVersion+1 {
		t.Fatalf("advance current Gap: %+v err=%v", resolution, err)
	}
	view, err = store.GetFindingAssessment(ctx, created.CaseID, created.FindingID, now.Add(3*time.Minute))
	if err != nil || view.ReviewBasis.State != "changed" || view.ProgressBasis.State != "changed" || view.Overdue ||
		view.Assessment.Review.Basis.AdoptedGaps[0].Note != oldGap.Note || view.Assessment.Progress.Basis.AdoptedGaps[0].ResolutionVersion != oldGap.ResolutionVersion {
		t.Fatalf("current Gap replaced historical adopted content: %+v err=%v", view, err)
	}
	fresh, err := store.PreviewFindingBasis(ctx, created.CaseID, created.FindingID, selection)
	if err != nil || fresh.AdoptedGaps[0].Note != resolution.Note {
		t.Fatalf("fresh basis: %+v err=%v", fresh, err)
	}
	review := workspace.AssessmentCommand{Operation: "record_review", OperationID: testUUID(t), ExpectedVersion: view.Assessment.Version,
		ExpectedCaseVersion: view.CaseVersion, ExpectedBasisDigest: fresh.Digest, Selection: selection,
		Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Explicitly reviewed the later account"}}
	reviewed, err := store.ApplyFindingAssessment(ctx, created.CaseID, created.FindingID, actor, review, now.Add(3*time.Minute))
	if err != nil || reviewed.Current.Progress.State != "ended" || reviewed.Current.Review.Basis.Digest != fresh.Digest {
		t.Fatalf("new review silently reopened progress: %+v err=%v", reviewed, err)
	}
	page, err := store.GetAssessmentHistory(ctx, created.CaseID, created.FindingID, 2, 0, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Current.Version != 2 || page.NextBefore == nil || page.Items[0].Basis.AdoptedGaps[0].Note != oldGap.Note {
		t.Fatalf("fixed-upper history included later review: %+v err=%v", page, err)
	}
	page, err = store.GetAssessmentHistory(ctx, created.CaseID, created.FindingID, 2, *page.NextBefore, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Current.Version != 1 || page.NextBefore != nil {
		t.Fatalf("second fixed history page: %+v err=%v", page, err)
	}
	if _, err := store.GetAssessmentHistory(ctx, created.CaseID, created.FindingID, 4, 0, 1); !errors.Is(err, workspace.ErrInvalid) {
		t.Fatalf("future history bound accepted: %v", err)
	}
	sealed, err := store.SealCase(ctx, created.CaseID, actor, now.Add(4*time.Minute))
	if err != nil || sealed.SealedRevision == nil {
		t.Fatalf("seal current history: %v", err)
	}
	if replay, err := store.CreateReview(ctx, actor, command, now.Add(5*time.Minute), nil); err != nil || !workspace.SameAssessmentValue(replay, created) {
		t.Fatalf("seal invalidated original creation receipt: %v", err)
	}
	target, err := store.CreateCase(ctx, actor, workspace.CreateCaseInput{Title: "Continue in an existing open Case"}, now.Add(5*time.Minute), nil)
	if err != nil {
		t.Fatal(err)
	}
	continuation := workspace.ReviewCreationCommand{Operation: "continue_finding", OperationID: testUUID(t), CaseID: target.ID, ExpectedCaseVersion: target.Version,
		Finding: command.Finding, Selection: selection, Reason: "Continue after the immutable seal",
		From: &workspace.SealedFindingReference{FindingReference: workspace.FindingReference{CaseID: created.CaseID, FindingID: created.FindingID}, Revision: *sealed.SealedRevision, Digest: sealed.SealedDigest}}
	preview, err := store.PreviewReviewCreation(ctx, continuation)
	if err != nil {
		t.Fatal(err)
	}
	continuation.ExpectedBasisDigest = preview.Digest
	continued, err := store.CreateReview(ctx, actor, continuation, now.Add(6*time.Minute), nil)
	if err != nil || continued.Action != nil {
		t.Fatalf("continue into open Case: %+v err=%v", continued, err)
	}
	view, err = store.GetFindingAssessment(ctx, continued.CaseID, continued.FindingID, now.Add(7*time.Minute))
	if err != nil || view.Assessment.Recorded || view.Assessment.Review != nil || view.Assessment.Progress != nil || len(view.Continuations) != 1 {
		t.Fatalf("continuation manufactured a current review: %+v err=%v", view, err)
	}
	parent := mustCaseRecord(t, ctx, store, created.CaseID)
	if !bytes.Equal(parent.Case.SealedPack, sealed.SealedPack) || parent.Case.SealedDigest != sealed.SealedDigest {
		t.Fatal("continuation rewrote the old sealed pack")
	}
	if err := db.ValidateFindingAssessmentRestore(ctx, database); err != nil {
		t.Fatalf("valid history with later Gap content rejected on restore: %v", err)
	}
	if err := db.ValidateFindingResponsibilityRestore(ctx, database); err != nil {
		t.Fatalf("valid responsibility history and sealed contents rejected: %v", err)
	}
	var original []byte
	if err := database.QueryRowContext(ctx, `SELECT record FROM case_finding_assessments WHERE case_id=$1 AND finding_id=$2`, created.CaseID, created.FindingID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE case_finding_assessments SET record=jsonb_set(record,'{review,basis,adopted_gaps,0,note}','"forged adopted note"') WHERE case_id=$1 AND finding_id=$2`, created.CaseID, created.FindingID); err != nil {
		t.Fatal(err)
	}
	if err := db.ValidateFindingAssessmentRestore(ctx, database); err == nil {
		t.Fatal("restore accepted forged adopted contents")
	}
	if _, err := database.ExecContext(ctx, `UPDATE case_finding_assessments SET record=$3 WHERE case_id=$1 AND finding_id=$2`, created.CaseID, created.FindingID, original); err != nil {
		t.Fatal(err)
	}
	if err := db.ValidateFindingAssessmentRestore(ctx, database); err != nil {
		t.Fatalf("exact restored bytes rejected: %v", err)
	}
}
