package db_test

import (
	"context"
	"errors"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/investigation"
	"github.com/foigent/x-insight/internal/workspace"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScopedEvidenceAmbiguityAndMixedCaseRequirements(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dsn := workspaceTestDSN(t, ctx, base)
	if _, e := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); e != nil {
		t.Fatal(e)
	}
	admin := openTestSQL(t, dsn)
	defer admin.Close()
	reader := openTestSQL(t, dsn)
	defer reader.Close()
	business := openTestSQL(t, dsn)
	defer business.Close()
	evidence := db.NewEvidenceStore(reader)
	store := db.NewWorkspaceStore(business)
	scope := db.NewScopedWorkchainStore(evidence, store)
	source, otherSource := testUUID(t), testUUID(t)
	for _, pair := range []struct {
		source string
		index  int
	}{{source, 0}, {source, 1}, {otherSource, 0}} {
		mapped := mapWorkbenchFixture(t, pair.source, pair.index, "17")
		if _, e := evidence.PersistEvents(ctx, mapped.Events); e != nil {
			t.Fatal(e)
		}
	}
	const subject = "xs_1020304050607080"
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := admin.ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_details,allow_source_context,allow_source_identity,allow_source_cost) VALUES($1,$2,'project-202',true,true,true,true)`, subject, source)
	type fact struct{ event, hash, execution, project string }
	facts := make([]fact, 3)
	for i, pair := range []struct{ source, request string }{{source, "req-workbench-1"}, {source, "req-workbench-2"}, {otherSource, "req-workbench-1"}} {
		if e := admin.QueryRowContext(ctx, `SELECT evidence_event_id::text,event_hash,execution_id,source_project_ref FROM workbench_economic_facts WHERE source_system_id=$1 AND request_id=$2`, pair.source, pair.request).Scan(&facts[i].event, &facts[i].hash, &facts[i].execution, &facts[i].project); e != nil {
			t.Fatal(e)
		}
	}
	run := func(fn func(context.Context)) {
		t.Helper()
		local, finish, e := scope.Begin(ctx, subject, true, func(f func() error) error { return f() })
		if e != nil {
			t.Fatal(e)
		}
		defer finish()
		fn(local)
	}
	check := func(id string, allowed bool) {
		t.Helper()
		run(func(local context.Context) {
			_, e := scope.Evidence().GetExecution(local, id, time.Now().UTC())
			if allowed && e != nil {
				t.Fatal(e)
			}
			if !allowed && !errors.Is(e, db.ErrExecutionNotFound) {
				t.Fatalf("hidden execution %v", e)
			}
		})
	}
	check(facts[0].execution, true)
	check(facts[1].execution, false)
	check(facts[2].execution, false)
	// These isolated admin fixtures model corrupted or ambiguous provenance; no production M0 mutation is involved.
	exec(`UPDATE evidence_events SET event_hash=$1 WHERE id=$2`, facts[1].hash, facts[0].event)
	check(facts[0].execution, false)
	exec(`UPDATE evidence_events SET event_hash=$1 WHERE id=$2`, facts[0].hash, facts[0].event)
	exec(`UPDATE evidence_events SET execution_id=$1 WHERE id=$2`, facts[0].execution, facts[1].event)
	check(facts[0].execution, false)
	exec(`UPDATE evidence_events SET execution_id=$1 WHERE id=$2`, facts[1].execution, facts[1].event)
	exec(`UPDATE workbench_access_grants SET cost_project_ref='unknown-cost-project' WHERE subject_id=$1`, subject)
	check(facts[0].execution, false)
	exec(`UPDATE workbench_access_grants SET cost_project_ref=NULL WHERE subject_id=$1`, subject)
	unknown := mapWorkbenchFixture(t, source, 2, "17")
	for _, event := range unknown.Events {
		delete(event, "project_ref")
	}
	if _, e := evidence.PersistEvents(ctx, unknown.Events); e != nil {
		t.Fatal(e)
	}
	var unknownID string
	if e := admin.QueryRowContext(ctx, `SELECT execution_id FROM workbench_economic_facts WHERE source_system_id=$1 AND request_id='req-workbench-3'`, source).Scan(&unknownID); e != nil {
		t.Fatal(e)
	}
	check(unknownID, false)
	actor, _ := workspace.HumanActor(subject)
	now := time.Now().UTC()
	command := workspace.ReviewCreationCommand{Operation: "start_review", OperationID: testUUID(t), NewCase: &workspace.CreateCaseInput{Title: "Mixed-scope classified case"}, Finding: workspace.CreateFindingInput{Title: "Scoped", Statement: "Synthetic assertion", Classification: "hypothesis"}, Selection: workspace.BasisSelection{Evidence: []workspace.EvidenceChoice{{Scope: "finding", EvidenceEventID: facts[0].event, EventHash: facts[0].hash, Relation: "supports"}}}, Reason: "Start scoped review", Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Reviewed authorized evidence"}}
	var receipt workspace.ReviewCreationReceipt
	run(func(local context.Context) {
		bad := command
		bad.Selection = workspace.BasisSelection{Evidence: []workspace.EvidenceChoice{{Scope: "finding", EvidenceEventID: facts[0].event, EventHash: facts[1].hash, Relation: "supports"}}}
		if _, e := store.PreviewReviewCreation(local, bad); e == nil {
			t.Fatal("wrong hash accepted")
		}
		basis, e := store.PreviewReviewCreation(local, command)
		if e != nil {
			t.Fatal(e)
		}
		command.ExpectedBasisDigest = basis.Digest
		receipt, e = store.CreateReview(local, actor, command, now, nil)
		if e != nil {
			t.Fatal(e)
		}
		before, e := store.GetCase(local, receipt.CaseID)
		if e != nil {
			t.Fatal(e)
		}
		_, e = store.AddEvidenceLink(local, receipt.CaseID, actor, workspace.AddEvidenceInput{EvidenceEventID: facts[1].event, EventHash: facts[1].hash, Relation: "context"}, now)
		if !errors.Is(e, workspace.ErrNotFound) {
			t.Fatalf("unauthorized case link: %v", e)
		}
		after, e := store.GetCase(local, receipt.CaseID)
		if e != nil || after.Case.Version != before.Case.Version || len(after.Evidence) != len(before.Evidence) {
			t.Fatalf("rejected link mutated case: %v", e)
		}
	})
	exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_details,allow_source_context,allow_source_identity,allow_source_cost) VALUES($1,$2,'project-203',true,true,true,true)`, subject, source)
	var cursor string
	run(func(local context.Context) {
		if _, e := store.AddEvidenceLink(local, receipt.CaseID, actor, workspace.AddEvidenceInput{EvidenceEventID: facts[1].event, EventHash: facts[1].hash, Relation: "context"}, now); e != nil {
			t.Fatal(e)
		}
		if _, e := store.CreateFinding(local, receipt.CaseID, actor, workspace.CreateFindingInput{Title: "Second", Statement: "Second assertion", Classification: "hypothesis"}, now, nil); e != nil {
			t.Fatal(e)
		}
		page, e := store.ListInvestigationQueue(local, workspace.QueueAccess{Subject: subject, Revision: "scope-test"}, workspace.QueueRequest{Limit: 1}, now)
		if e != nil || page.Total != 2 || page.NextCursor == "" {
			t.Fatalf("queue before revocation: %+v %v", page, e)
		}
		cursor = page.NextCursor
	})
	exec(`UPDATE workbench_access_grants SET active=false WHERE subject_id=$1 AND source_project_ref='project-203'`, subject)
	run(func(local context.Context) {
		if _, e := store.GetCase(local, receipt.CaseID); !errors.Is(e, workspace.ErrNotFound) {
			t.Fatalf("mixed case leaked after partial revoke: %v", e)
		}
		if _, e := store.CreateReview(local, actor, command, now, nil); !errors.Is(e, workspace.ErrNotFound) {
			t.Fatalf("mixed receipt replay leaked: %v", e)
		}
		page, e := store.ListInvestigationQueue(local, workspace.QueueAccess{Subject: subject, Revision: "scope-test"}, workspace.QueueRequest{Limit: 1}, now)
		if e != nil || page.Total != 0 {
			t.Fatalf("hidden mixed case counted: %+v %v", page, e)
		}
		if _, e = store.ListInvestigationQueue(local, workspace.QueueAccess{Subject: subject, Revision: "scope-test"}, workspace.QueueRequest{Limit: 1, Cursor: cursor}, now); e == nil {
			t.Fatal("old-scope cursor accepted")
		}
		rows, e := scope.Evidence().SearchExecutions(local, investigation.SearchQuery{From: now.AddDate(-1, 0, 0), To: now, AsOf: now, Limit: 1})
		if e != nil || len(rows) != 1 || rows[0].ExecutionID != facts[0].execution {
			t.Fatalf("filter before limit: %+v %v", rows, e)
		}
	})
}
