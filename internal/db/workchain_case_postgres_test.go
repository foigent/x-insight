package db_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/investigation"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestScopedCaseCreationVisibilityHistoryAndInvalidation(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("isolated PostgreSQL required")
	}
	// The 500 real writes and later lifecycle assertions share the package test
	// deadline; setup time must not expire the context of a subsequent request.
	ctx := t.Context()
	dsn := workspaceTestDSN(t, ctx, base)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	admin := openTestSQL(t, dsn)
	defer admin.Close()
	reader := openTestSQL(t, dsn)
	defer reader.Close()
	reader.SetMaxOpenConns(1)
	business := openTestSQL(t, dsn)
	defer business.Close()
	business.SetMaxOpenConns(1)
	// Force the expensive server default on this single reused connection.
	// The product's transactions must not leak their local overrides.
	if _, err := business.ExecContext(ctx, "SET jit=on"); err != nil {
		t.Fatal(err)
	}
	assertJITRestored := func() {
		t.Helper()
		var setting string
		if err := business.QueryRowContext(ctx, "SHOW jit").Scan(&setting); err != nil || setting != "on" {
			t.Fatal("case transaction leaked JIT setting", setting, err)
		}
	}
	evidence := db.NewEvidenceStore(reader)
	store := db.NewWorkspaceStore(business)
	scope := db.NewScopedWorkchainStore(evidence, store)
	source := testUUID(t)
	mapped := mapWorkbenchFixture(t, source, 0, "17")
	if _, err := evidence.PersistEvents(ctx, mapped.Events); err != nil {
		t.Fatal(err)
	}
	other := mapWorkbenchFixture(t, source, 1, "29")
	if _, err := evidence.PersistEvents(ctx, other.Events); err != nil {
		t.Fatal(err)
	}
	const subject = "xs_0011223344556677"
	var eventID, hash, ref, execution, project string
	if err := admin.QueryRowContext(ctx, `SELECT evidence_event_id::text,event_hash,source_ref,execution_id,source_project_ref FROM workbench_economic_facts WHERE source_system_id=$1 AND request_id='req-workbench-1'`, source).Scan(&eventID, &hash, &ref, &execution, &project); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_summary,allow_details,allow_source_context,allow_source_identity,allow_source_cost) VALUES($1,$2,$3,true,true,true,true,true)`, subject, source, project); err != nil {
		t.Fatal(err)
	}
	actor, _ := workspace.HumanActor(subject)
	now := time.Now().UTC()
	legacy, err := store.CreateCase(ctx, actor, workspace.CreateCaseInput{Title: "private legacy text"}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	command := workspace.ReviewCreationCommand{Operation: "start_review", OperationID: testUUID(t), NewCase: &workspace.CreateCaseInput{Title: "Scoped investigation"}, Finding: workspace.CreateFindingInput{Title: "Review", Statement: "A scoped assertion", Classification: "hypothesis"}, Selection: workspace.BasisSelection{Evidence: []workspace.EvidenceChoice{{Scope: "finding", EvidenceEventID: eventID, EventHash: hash, SourceRef: ref, Relation: "supports"}}}, Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Reviewed the selected source"}, Reason: "Start review"}
	local, finish, err := scope.Begin(ctx, subject, true, func(f func() error) error { return f() })
	if err != nil {
		t.Fatal(err)
	}
	preview, err := store.PreviewReviewCreation(local, command)
	if err != nil {
		finish()
		t.Fatal("preview", err)
	}
	command.ExpectedBasisDigest = preview.Digest
	result, err := store.CreateReview(local, actor, command, now, nil)
	if err != nil {
		finish()
		t.Fatal("create", err)
	}
	if _, err = store.GetCase(local, result.CaseID); err != nil {
		finish()
		t.Fatal("get", err)
	}
	assertJITRestored()
	if _, err = business.ExecContext(ctx, "SET TIME ZONE 'Asia/Shanghai'"); err != nil {
		finish()
		t.Fatal(err)
	}
	if _, err = store.GetCase(local, result.CaseID); err != nil {
		finish()
		t.Fatal("case binding depends on connection timezone", err)
	}
	if _, err = business.ExecContext(ctx, "SET TIME ZONE 'UTC'"); err != nil {
		finish()
		t.Fatal(err)
	}
	if _, err = store.GetCase(local, legacy.ID); !errors.Is(err, workspace.ErrNotFound) {
		finish()
		t.Fatalf("legacy disclosed: %v", err)
	}
	assertJITRestored()
	cases, err := store.ListCases(local, 50)
	if err != nil || len(cases) != 1 || cases[0].ID != result.CaseID {
		finish()
		t.Fatalf("case filtering: %v %+v", err, cases)
	}
	executions, err := scope.Evidence().SearchExecutions(local, investigation.SearchQuery{AsOf: now, From: now.AddDate(-1, 0, 0), To: now, Limit: 50})
	if err != nil || len(executions) != 1 || executions[0].ExecutionID != execution {
		finish()
		t.Fatalf("execution filtering: %v %+v", err, executions)
	}
	queue, err := store.ListInvestigationQueue(local, workspace.QueueAccess{Subject: subject, Revision: "test"}, workspace.QueueRequest{Limit: 50}, now)
	if err != nil || len(queue.Items) != 1 {
		finish()
		t.Fatalf("queue: %v %+v", err, queue)
	}
	review, err := store.GetFindingAssessment(local, result.CaseID, result.FindingID, now)
	if err != nil || review.Assessment.Version != 1 {
		finish()
		t.Fatalf("assessment: %v", err)
	}
	replay, err := store.CreateReview(local, actor, command, now, nil)
	if err != nil || replay.CaseID != result.CaseID {
		finish()
		t.Fatalf("replay: %v", err)
	}
	// One scope request may contain several local operations in this test. Each
	// transaction's label pre-state is separate, even with a single business slot.
	annotation, err := store.CreateAnnotation(local, result.CaseID, actor, workspace.CreateAnnotationInput{TargetKind: "case", TargetRef: result.CaseID, Body: "Scoped note"}, now, nil)
	if err != nil {
		finish()
		t.Fatalf("annotation: %+v %v", annotation, err)
	}
	if _, err = store.GetCase(local, result.CaseID); err != nil {
		finish()
		t.Fatal("label refresh", err)
	}
	// Mixed authorized/legacy queue, with the scope predicate before counts
	// and pagination. Grow the authorized case through the actual write path.
	writeStarted := time.Now()
	for i := 1; i < 500; i++ {
		if _, err = store.CreateFinding(local, result.CaseID, actor, workspace.CreateFindingInput{Title: fmt.Sprintf("Scoped %d", i), Statement: "Authorized synthetic assertion", Classification: "hypothesis"}, now, nil); err != nil {
			finish()
			t.Fatalf("500 finding setup %d: %v", i, err)
		}
	}
	assertJITRestored()
	t.Logf("499 authorized finding writes in %s; connection JIT restored", time.Since(writeStarted))
	if _, err = admin.ExecContext(ctx, `INSERT INTO case_findings(id,case_id,title,statement,classification,status,created_by_subject,created_at,updated_at) SELECT 'xf_'||lpad(i::text,22,'0'),$1,'private hidden finding','private hidden assertion','hypothesis','open',$2,$3,$3 FROM generate_series(1,2001) i`, legacy.ID, subject, now); err != nil {
		finish()
		t.Fatal(err)
	}
	seen := map[string]bool{}
	cursor := ""
	started := time.Now()
	for {
		page, e := store.ListInvestigationQueue(local, workspace.QueueAccess{Subject: subject, Revision: "test"}, workspace.QueueRequest{Limit: 50, Cursor: cursor}, now)
		if e != nil {
			finish()
			t.Fatal(e)
		}
		if page.Total != 500 {
			finish()
			t.Fatalf("hidden queue count %d", page.Total)
		}
		for _, row := range page.Items {
			if row.Finding.CaseID != result.CaseID || seen[row.Finding.ID] {
				finish()
				t.Fatal("hidden/duplicate queue row")
			}
			seen[row.Finding.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 500 {
		finish()
		t.Fatalf("authorized queue total %d", len(seen))
	}
	t.Logf("scoped queue 500 visible / 2001 hidden, all pages in %s", time.Since(started))
	if err = db.ValidateWorkbenchRestore(ctx, admin); err != nil {
		finish()
		t.Fatal("restore validator", err)
	}
	if err = finish(); err != nil {
		t.Fatal(err)
	}
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "000030_add_scoped_workchain.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	tx, e := admin.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	_, e = tx.ExecContext(ctx, string(down))
	tx.Rollback()
	if e == nil {
		t.Fatal("populated scope labels allowed destructive downgrade")
	}
	tx, e = admin.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.ExecContext(ctx, "ALTER TABLE workbench_access_grants DISABLE TRIGGER workchain_grants_synchronize"); e != nil {
		tx.Rollback()
		t.Fatal(e)
	}
	if e = db.ValidateWorkbenchRestore(ctx, tx); e == nil {
		tx.Rollback()
		t.Fatal("missing grant guard accepted")
	}
	tx.Rollback()
	// Global legacy mutation cannot silently renew a scoped classification.
	if _, err = admin.ExecContext(ctx, `UPDATE investigation_cases SET title='unclassified legacy edit' WHERE id=$1`, result.CaseID); err != nil {
		t.Fatal(err)
	}
	local, finish, err = scope.Begin(ctx, subject, true, func(f func() error) error { return f() })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetCase(local, result.CaseID); !errors.Is(err, workspace.ErrNotFound) {
		finish()
		t.Fatalf("digest mismatch disclosed: %v", err)
	}
	if _, err = store.CreateFinding(local, result.CaseID, actor, workspace.CreateFindingInput{Title: "Rejected", Statement: "Invalidated scope", Classification: "hypothesis"}, now, nil); !errors.Is(err, workspace.ErrNotFound) {
		finish()
		t.Fatalf("invalidated case write allowed: %v", err)
	}
	assertJITRestored()
	if _, err = store.CreateReview(local, actor, command, now, nil); !errors.Is(err, workspace.ErrNotFound) {
		finish()
		t.Fatalf("stale receipt disclosed: %v", err)
	}
	if err = finish(); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.ExecContext(ctx, `UPDATE workbench_access_grants SET active=false WHERE subject_id=$1`, subject); err != nil {
		t.Fatal(err)
	}
	if reader.Stats().InUse != 0 || business.Stats().InUse != 0 {
		t.Fatal("connection retained")
	}
}
