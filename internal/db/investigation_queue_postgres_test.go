package db_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestQueueStorePersistentFiltersAndCompleteSelection(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("PG required")
	}
	ctx := context.Background()
	dsn := workspaceTestDSN(t, ctx, base)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	actor := workspace.Actor{Kind: "human", Ref: "xs_1111111111111111"}
	at := time.Now().UTC().Truncate(time.Microsecond)
	c, err := store.CreateCase(ctx, actor, workspace.CreateCaseInput{Title: "Queue store full candidate set"}, at, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err = store.CreateFinding(ctx, c.ID, actor, workspace.CreateFindingInput{Title: "Stored item", Statement: "Synthetic known scope", Classification: "hypothesis"}, at.Add(-time.Duration(i)*48*time.Hour), nil); err != nil {
			t.Fatal(err)
		}
	}
	scope := workspace.QueueAccess{Subject: actor.Ref, Revision: "reader"}
	req := workspace.QueueRequest{Limit: 1}
	first, err := store.ListInvestigationQueue(ctx, scope, req, at.Add(time.Second))
	if err != nil || first.Total != 3 || first.Statistics.Created != 1 {
		t.Fatal("full selection", first, err)
	}
	req.Cursor = first.NextCursor
	next, err := store.ListInvestigationQueue(ctx, scope, req, at.Add(time.Minute))
	if err != nil || len(next.Items) != 1 || next.Items[0].Finding.ID == first.Items[0].Finding.ID {
		t.Fatal("page", err)
	}
	for _, filter := range []workspace.QueueFilters{{Assignee: "me"}, {Assignee: "unassigned"}, {Assignee: "inactive"}, {Progress: "ended"}, {Conclusion: "supported"}, {Basis: "changed"}, {CaseStatus: "sealed"}, {Search: "absent"}, {Sort: "due_asc"}, {Mode: "created"}, {Mode: "reassessment"}} {
		if _, err = store.ListInvestigationQueue(ctx, scope, workspace.QueueRequest{Limit: 100, Filters: filter}, at.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.ListInvestigationQueue(ctx, scope, workspace.QueueRequest{Limit: 101}, at); !errors.Is(err, workspace.ErrInvalid) {
		t.Fatal("oversized page")
	}
	if _, err = store.ListInvestigationQueue(ctx, scope, workspace.QueueRequest{Limit: 1, Cursor: "bad"}, at); !errors.Is(err, workspace.ErrInvalid) {
		t.Fatal("malformed cursor")
	}
	req.Cursor = first.NextCursor
	if _, err = store.ListInvestigationQueue(ctx, scope, req, at.Add(time.Hour)); !errors.Is(err, workspace.ErrConflict) {
		t.Fatal("expired cursor")
	}
	if _, err = admin.Exec(`UPDATE case_findings SET title='Later' WHERE id=$1`, first.Items[0].Finding.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ListInvestigationQueue(ctx, scope, req, at.Add(time.Minute)); !errors.Is(err, workspace.ErrConflict) {
		t.Fatal("stale page")
	}
	cmd := workspace.QueueFilterCommand{OperationID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Schema: workspace.QueueFilterSchema, Name: "Stored filter", Filters: workspace.QueueFilters{Mode: "created", Period: "7d", Sort: "created_desc"}}
	saved, err := store.ApplyQueueFilter(ctx, actor.Ref, "", "create", cmd, at)
	if err != nil {
		t.Fatal(err)
	}
	same, err := store.ApplyQueueFilter(ctx, actor.Ref, "", "create", cmd, at.Add(time.Second))
	if err != nil || !workspace.SameQueueValue(saved, same) {
		t.Fatal("replay", err)
	}
	cmd.Name = "Changed command"
	if _, err = store.ApplyQueueFilter(ctx, actor.Ref, "", "create", cmd, at); !errors.Is(err, workspace.ErrConflict) {
		t.Fatal("conflicting operation")
	}
	cmd.OperationID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	cmd.ExpectedRevision = 1
	if _, err = store.ApplyQueueFilter(ctx, "xs_2222222222222222", saved.ID, "update", cmd, at); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatal("cross-owner")
	}
	current, err := store.ApplyQueueFilter(ctx, actor.Ref, saved.ID, "update", cmd, at.Add(time.Second))
	if err != nil || current.Revision != 2 {
		t.Fatal(err)
	}
	list, err := store.ListQueueFilters(ctx, actor.Ref)
	if err != nil || len(list) != 1 || !workspace.SameQueueValue(list[0], current) {
		t.Fatal("list", err)
	}
	other, err := store.ListQueueFilters(ctx, "xs_2222222222222222")
	if err != nil || len(other) != 0 {
		t.Fatal("owner leak")
	}
	if err = db.ValidateInvestigationQueueRestore(ctx, admin); err != nil {
		t.Fatal(err)
	}
	cmd.OperationID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	cmd.ExpectedRevision = 2
	deleted, err := store.ApplyQueueFilter(ctx, actor.Ref, saved.ID, "delete", cmd, at.Add(2*time.Second))
	if err != nil || !deleted.Deleted {
		t.Fatal(err)
	}
	if err = db.ValidateInvestigationQueueRestore(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(`UPDATE investigation_queue_filter_operations SET receipt=receipt||'{"unknown":true}'::jsonb WHERE filter_id=$1`, saved.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.ValidateInvestigationQueueRestore(ctx, admin); err == nil {
		t.Fatal("unknown receipt fields accepted")
	}
	// Read-only connections can list queue/filter state without writes.
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("default_transaction_read_only", "on")
	u.RawQuery = q.Encode()
	reader, err := db.OpenWorkspaceStore(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err = reader.ListInvestigationQueue(ctx, scope, workspace.QueueRequest{Limit: 10}, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.ListQueueFilters(ctx, actor.Ref); err != nil {
		t.Fatal(err)
	}
}
