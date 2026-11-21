package db_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/investigation"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestEvidenceRevisionFencesLateCommitAcrossInvestigationReads(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("explicit PostgreSQL fixture required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	dsn := workspaceTestDSN(t, ctx, baseDSN)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	reader, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer, err := db.OpenEvidenceStore(ctx, dsn+"&application_name=snapshot_review_writer")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	clock, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer clock.Close()

	sourceID, adapterID, requestID := testUUID(t), testUUID(t), "review-"+testUUID(t)
	executionID, err := executionid.New(sourceID, requestID)
	if err != nil {
		t.Fatal(err)
	}
	backdated := time.Now().UTC().Add(-time.Hour)
	makeEvent := func(spanID string, effectiveAt time.Time) map[string]any {
		event := investigationTestEvent(sourceID, adapterID, requestID, testUUID(t), spanID, "", effectiveAt, backdated)
		hash, err := evidence.GenerateEventHash(event)
		if err != nil {
			t.Fatal(err)
		}
		event["event_hash"] = hash
		return event
	}
	first, second := makeEvent("seed", backdated), makeEvent("next", backdated.Add(time.Minute))
	if report, err := writer.PersistEvents(ctx, []map[string]any{first}); err != nil || report.Inserted != 1 {
		t.Fatalf("Seed: inserted=%d err=%v", report.Inserted, err)
	}

	blocker, err := clock.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err := blocker.ExecContext(ctx, "LOCK TABLE source_systems IN SHARE MODE"); err != nil {
		t.Fatal(err)
	}
	written := make(chan error, 1)
	go func() {
		_, err := writer.PersistEvents(ctx, []map[string]any{second})
		written <- err
	}()
	for {
		var waiting bool
		if err := clock.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name = 'snapshot_review_writer' AND wait_event_type = 'Lock')").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("writer did not wait on the fixture lock")
		case <-time.After(5 * time.Millisecond):
		}
	}
	var asOf time.Time
	if err := clock.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&asOf); err != nil {
		t.Fatal(err)
	}
	old, err := reader.ResolveEvidenceRevision(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	pinned := investigation.WithEvidenceRevision(ctx, old)
	query := investigation.SearchQuery{AsOf: asOf, From: asOf.Add(-2 * time.Hour), To: asOf, SourceSystemID: sourceID, Limit: 10}
	check := func(readCtx context.Context, wantCount int, wantLast time.Time) {
		t.Helper()
		search, err := reader.SearchExecutions(readCtx, query)
		if err != nil || len(search) != 1 {
			t.Fatalf("search items=%d err=%v", len(search), err)
		}
		if search[0].EventCount != wantCount || search[0].LastEffectiveAt.Sub(wantLast).Abs() >= time.Microsecond {
			t.Fatalf("search count=%d last=%s, want count=%d last=%s", search[0].EventCount, search[0].LastEffectiveAt, wantCount, wantLast)
		}
		projection, err := reader.GetInvestigationProjection(readCtx, executionID, asOf)
		if err != nil || projection.Integrity.TotalEventCount != wantCount || len(projection.Nodes) != wantCount {
			t.Fatalf("projection count=%d nodes=%d err=%v", projection.Integrity.TotalEventCount, len(projection.Nodes), err)
		}
		if revision, ok := investigation.EvidenceRevisionFromContext(readCtx); ok {
			if projection.EvidenceRevision != revision.ID || !strings.Contains(projection.Nodes[0].Citation.DeepLink, "evidence_revision="+revision.ID) {
				t.Fatal("projection omitted the requested revision from its citation")
			}
		}
		timeline, total, err := reader.ListTimeline(readCtx, investigation.TimelineQuery{ExecutionID: executionID, AsOf: asOf, Limit: 10})
		if err != nil || len(timeline) != wantCount || total != wantCount {
			t.Fatalf("timeline count=%d total=%d err=%v", len(timeline), total, err)
		}
	}
	check(pinned, 1, backdated)
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}

	check(pinned, 1, backdated)
	check(ctx, 2, backdated.Add(time.Minute))
	if _, err := reader.GetExecutionEvent(pinned, executionID, second["id"].(string), asOf); !errors.Is(err, db.ErrEventNotFound) {
		t.Fatalf("late event in the old revision: %v", err)
	}
	latest, err := reader.ResolveEvidenceRevision(ctx, "")
	if err != nil || latest.Order != old.Order+1 || latest.ID == old.ID {
		t.Fatalf("publication head old=%+v latest=%+v err=%v", old, latest, err)
	}
	if replay, err := writer.PersistEvents(ctx, []map[string]any{second}); err != nil || replay.DuplicateNoop != 1 {
		t.Fatalf("replay %+v: %v", replay, err)
	}
	afterReplay, err := reader.ResolveEvidenceRevision(ctx, "")
	if err != nil || afterReplay != latest {
		t.Fatalf("replay moved publication head: %+v err=%v", afterReplay, err)
	}

	third := makeEvent("third", backdated.Add(2*time.Minute))
	invalid := makeEvent("malformed", backdated.Add(3*time.Minute))
	invalid["ingested_at"] = "invalid-timestamp"
	if _, err := writer.PersistEvents(ctx, []map[string]any{third, invalid}); err == nil {
		t.Fatal("invalid batch committed")
	}
	afterFailure, err := reader.ResolveEvidenceRevision(ctx, "")
	if err != nil || afterFailure != latest {
		t.Fatalf("failed batch moved publication head: %+v err=%v", afterFailure, err)
	}
	if _, err := reader.GetExecutionEvent(ctx, executionID, third["id"].(string), asOf); !errors.Is(err, db.ErrEventNotFound) {
		t.Fatalf("partially persisted event after failed batch: %v", err)
	}

	cases := db.NewWorkspaceStore(clock)
	owner, err := workspace.HumanActor("xs_1234567890abcdef")
	if err != nil {
		t.Fatal(err)
	}
	caseItem, err := cases.CreateCase(ctx, owner, workspace.CreateCaseInput{Title: "Versioned evidence"}, asOf, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	initial := workspace.AddExecutionInput{ExecutionID: executionID, AsOf: asOf.UTC(), EvidenceRevision: old.ID, ReuseTimestampReference: true}
	firstReference, err := cases.AddCaseExecution(ctx, caseItem.ID, owner, initial, asOf)
	if err != nil || firstReference.EvidenceRevision != old.ID {
		t.Fatalf("first Case reference %+v: %v", firstReference, err)
	}
	initial.EvidenceRevision = latest.ID
	replayedReference, err := cases.AddCaseExecution(ctx, caseItem.ID, owner, initial, asOf.Add(time.Minute))
	if err != nil || replayedReference != firstReference {
		t.Fatalf("time-only retry changed the Case reference %+v: %v", replayedReference, err)
	}
	initial.ReuseTimestampReference = false
	secondReference, err := cases.AddCaseExecution(ctx, caseItem.ID, owner, initial, asOf.Add(time.Minute))
	if err != nil || secondReference.EvidenceRevision != latest.ID {
		t.Fatalf("explicit distinct Case reference %+v: %v", secondReference, err)
	}
	record, err := cases.GetCase(ctx, caseItem.ID)
	if err != nil || len(record.Executions) != 2 {
		t.Fatalf("Case persisted %d references: %v", len(record.Executions), err)
	}

}
