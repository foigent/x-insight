package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
)

type purposeFixture struct {
	ctx             context.Context
	admin           *sql.DB
	store           *db.WorkspaceStore
	scope           *db.ScopedWorkchainStore
	subject, source string
	targets         []purposeTarget
	request         workbench.ReadingRequest
}
type purposeTarget struct{ execution, event, hash string }

func newPurposeFixture(t *testing.T, amounts []string) *purposeFixture {
	t.Helper()
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	dsn := workspaceTestDSN(t, ctx, base)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	admin := openTestSQL(t, dsn)
	t.Cleanup(func() { admin.Close() })
	reader, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	store, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	f := &purposeFixture{ctx: ctx, admin: admin, store: store, scope: db.NewScopedWorkchainStore(reader, store), subject: "xs_purpose0000000001", source: testUUID(t)}
	for i, amount := range amounts {
		mapped := mapWorkbenchFixture(t, f.source, i, amount)
		if _, err = reader.PersistEvents(ctx, mapped.Events); err != nil {
			t.Fatal(err)
		}
		var target purposeTarget
		if err = admin.QueryRowContext(ctx, `SELECT execution_id,evidence_event_id::text,event_hash FROM workbench_economic_facts WHERE request_id=$1 AND source_system_id=$2`, mapped.RequestID, f.source).Scan(&target.execution, &target.event, &target.hash); err != nil {
			t.Fatal(err)
		}
		f.targets = append(f.targets, target)
	}
	if _, err = admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences,allow_purpose_read,allow_purpose_review) VALUES($1,$2,true,true,true,true,true,true)`, f.subject, f.source); err != nil {
		t.Fatal(err)
	}
	f.request = relativeWorkbenchRequest()
	f.request.Period = workbench.PeriodInput{StartInclusive: timePointer(time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)), EndExclusive: timePointer(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)), DisplayTimezone: "UTC"}
	return f
}

func (f *purposeFixture) run(c workbench.PurposeCommand, index int) (workbench.PurposeEvent, error) {
	ctx, finish, err := f.scope.Begin(f.ctx, f.subject, false, func(commit func() error) error { return commit() })
	if err != nil {
		return workbench.PurposeEvent{}, err
	}
	defer finish()
	workchain.FromContext(ctx).PurposeReviewer = true
	return f.store.ApplyWorkbenchPurpose(ctx, f.subject, f.targets[index].execution, c, time.Now().UTC())
}
func (f *purposeFixture) declare(t *testing.T, index int, category string, revision int64) workbench.PurposeEvent {
	t.Helper()
	c := workbench.PurposeCommand{Action: "declare", ExpectedRevision: revision, IdempotencyKey: fmt.Sprintf("declare-%d-%d", index, revision), Category: category, Vocabulary: workbench.PurposeVocabulary, Basis: []workbench.PurposeBasis{{EvidenceID: f.targets[index].event, EventHash: f.targets[index].hash}}, Reason: "Synthetic human assessment of the referenced request"}
	e, err := f.run(c, index)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func (f *purposeFixture) accept(t *testing.T, index int, e workbench.PurposeEvent) workbench.PurposeEvent {
	t.Helper()
	c := workbench.PurposeCommand{Action: "accept", ExpectedRevision: e.Revision, IdempotencyKey: fmt.Sprintf("accept-%d-%d", index, e.Revision), ClaimID: e.NewClaimID, Reason: "Explicit adoption"}
	next, err := f.run(c, index)
	if err != nil {
		t.Fatal(err)
	}
	return next
}
func (f *purposeFixture) reading(t *testing.T) workbench.Reading {
	t.Helper()
	r, err := f.store.CreateWorkbenchReading(f.ctx, f.subject, f.request, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPurposeNativeHundredRequestReadingAndRevision(t *testing.T) {
	amounts := make([]string, 100)
	for i := range amounts {
		switch {
		case i == 0:
			amounts[i] = "2000000"
		case i == 1:
			amounts[i] = "10000000"
		case i < 40:
			amounts[i] = "1000000"
		case i < 60:
			amounts[i] = "500000"
		case i < 70:
			amounts[i] = "1000000"
		case i < 90:
			amounts[i] = "1250000"
		default:
			amounts[i] = "500000"
		}
	}
	f := newPurposeFixture(t, amounts)
	var first workbench.PurposeEvent
	for i := 0; i < 70; i++ {
		category := "software_check"
		if i >= 40 {
			category = "content_transformation"
		}
		e := f.declare(t, i, category, 0)
		e = f.accept(t, i, e)
		if i == 0 {
			first = e
		}
	}
	for i := 90; i < 100; i++ {
		f.declare(t, i, "software_check", 0)
		f.declare(t, i, "content_transformation", 1)
	}
	old := f.reading(t)
	check := func(r workbench.Reading, codeCount, contentCount int64, codeMoney, contentMoney string) {
		t.Helper()
		var recentCode, recentContent, recentUnknown, recentConflict int64
		recentCursor := ""
		for {
			page, e := f.store.GetWorkbenchRecentRequests(f.ctx, f.subject, r.ID, "", 50, recentCursor, time.Now())
			if e != nil {
				t.Fatal(e)
			}
			for _, item := range page.Items {
				switch item.PurposeState {
				case "adopted":
					if item.PurposeCategory == "software_check" {
						recentCode++
					} else if item.PurposeCategory == "content_transformation" {
						recentContent++
					} else {
						t.Fatal("unexpected adopted category", item)
					}
				case "unknown":
					recentUnknown++
				case "conflict":
					recentConflict++
				default:
					t.Fatal("unexpected frozen purpose state", item)
				}
			}
			if page.NextCursor == "" {
				break
			}
			recentCursor = page.NextCursor
		}
		if recentCode != codeCount || recentContent != contentCount || recentUnknown != 20 || recentConflict != 10 {
			t.Fatal("recent purpose changed across readings", recentCode, recentContent, recentUnknown, recentConflict)
		}
		summary, err := f.store.GetWorkbenchSummary(f.ctx, f.subject, r.ID, time.Now())
		if err != nil || summary.RequestCount != 100 || summary.SelectedAmountMicros != "100000000" || summary.PurposeKnownRequests != 70 {
			t.Fatalf("summary %+v %v", summary, err)
		}
		groups, err := f.store.GetWorkbenchContributions(f.ctx, f.subject, r.ID, "purpose", 50, 0, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]struct {
			count int64
			money string
		}{"software_check": {codeCount, codeMoney}, "content_transformation": {contentCount, contentMoney}, "unknown": {20, "25000000"}, "conflict": {10, "5000000"}}
		if len(groups.Rows) != 4 || groups.DenominatorMicros != "100000000" || groups.UnassignedMicros != "0" {
			t.Fatalf("groups %+v", groups)
		}
		for _, g := range groups.Rows {
			w, ok := want[g.Label]
			if !ok || g.RequestCount != w.count || g.SelectedAmountMicros != w.money {
				t.Fatalf("group %+v", g)
			}
			details, err := f.store.GetWorkbenchDetails(f.ctx, f.subject, r.ID, g.Ref, 100, "", time.Now())
			if err != nil || int64(len(details.Rows)) != w.count {
				t.Fatalf("details %s %d %v", g.Label, len(details.Rows), err)
			}
			for _, row := range details.Rows {
				state := g.Label
				if state == "software_check" || state == "content_transformation" {
					state = "adopted"
				}
				if row.PurposeState != state {
					t.Fatalf("frozen detail purpose %s want %s", row.PurposeState, state)
				}
			}
		}
	}
	check(old, 40, 30, "50000000", "20000000")
	c := workbench.PurposeCommand{Action: "modify", ClaimID: first.State.AdoptedClaim, ExpectedRevision: 2, IdempotencyKey: "change-two-yuan", Category: "content_transformation", Vocabulary: workbench.PurposeVocabulary, Basis: []workbench.PurposeBasis{{EvidenceID: f.targets[0].event, EventHash: f.targets[0].hash}}, Reason: "Correct the two-yuan request", Tags: []string{"information_query"}}
	e, err := f.run(c, 0)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.run(c, 0)
	if err != nil || replay.Hash != e.Hash {
		t.Fatalf("replay %v", err)
	}
	newReading := f.reading(t)
	check(newReading, 39, 31, "48000000", "22000000")
	check(old, 40, 30, "50000000", "20000000")
	v, err := f.store.GetWorkbenchPurpose(f.ctx, f.subject, f.targets[0].execution, old.ID, 0, time.Now())
	if err != nil || v.State.Category != "software_check" || v.State.Revision != 2 || v.CanReview {
		t.Fatalf("frozen purpose %+v %v", v, err)
	}
	if err = db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal("native restoration invariants", err)
	}
	t.Log("100 requests/100 yuan conserved; 70 known, 20 unknown, 10 conflict; 2 yuan revision changes only new reading; duplicate command and auxiliary tag add no money")
}

func TestPurposeNativePartialScopeHistoryAndInterruptedCommit(t *testing.T) {
	f := newPurposeFixture(t, []string{"17", "23"})
	command := workbench.PurposeCommand{Action: "declare", ExpectedRevision: 0, IdempotencyKey: "interrupted-command", Category: "software_check", Vocabulary: workbench.PurposeVocabulary, Basis: []workbench.PurposeBasis{{EvidenceID: f.targets[0].event, EventHash: f.targets[0].hash}, {EvidenceID: f.targets[1].event, EventHash: f.targets[1].hash}}, Reason: "Two explicitly referenced requests"}
	local, finish, err := f.scope.Begin(f.ctx, f.subject, false, func(func() error) error { return workbench.ErrAccessChanged })
	if err != nil {
		t.Fatal(err)
	}
	workchain.FromContext(local).PurposeReviewer = true
	_, err = f.store.ApplyWorkbenchPurpose(local, f.subject, f.targets[0].execution, command, time.Now())
	finish()
	if !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("commit guard failure", err)
	}
	var count int
	f.admin.QueryRow(`SELECT count(*) FROM workbench_purpose_revisions`).Scan(&count)
	if count != 0 {
		t.Fatal("interrupted transaction persisted a revision")
	}
	e, err := f.run(command, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.accept(t, 0, e)
	for revision := int64(2); revision < 24; revision++ {
		_, err = f.run(workbench.PurposeCommand{Action: "accept", ExpectedRevision: revision, IdempotencyKey: fmt.Sprintf("repeat-explicit-%d", revision), ClaimID: e.NewClaimID, Reason: "Explicitly retain the still-valid adoption"}, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	view, err := f.store.GetWorkbenchPurpose(f.ctx, f.subject, f.targets[0].execution, "", 0, time.Now())
	if err != nil || len(view.History) != 20 || view.NextBefore != 5 {
		t.Fatalf("history page %+v %v", view, err)
	}
	page, err := f.store.GetWorkbenchPurpose(f.ctx, f.subject, f.targets[0].execution, "", view.NextBefore, time.Now())
	if err != nil || len(page.History) != 4 || page.NextBefore != 0 || page.History[0].Revision != 4 {
		t.Fatalf("history continuation %+v %v", page, err)
	}
	command.Reason = "Different command under identical key"
	if _, err = f.run(command, 0); !errors.Is(err, workbench.ErrConflict) {
		t.Fatal("different-content idempotency accepted", err)
	}
	if _, err = f.admin.Exec(`UPDATE workbench_access_grants SET allow_purpose_read=false,allow_purpose_review=false WHERE subject_id=$1`, f.subject); err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_summary,allow_details,allow_purpose_read,allow_purpose_review) SELECT $1,$2,source_project_ref,true,true,true,true FROM workbench_economic_facts WHERE execution_id=$3 LIMIT 1`, f.subject, f.source, f.targets[0].execution); err != nil {
		t.Fatal(err)
	}
	mixed := f.reading(t)
	if mixed.Purpose == nil || mixed.Purpose.Status != "unavailable" {
		t.Fatal("mixed scope disclosed a partial purpose denominator", mixed)
	}
	var project string
	if err = f.admin.QueryRow(`SELECT source_project_ref FROM workbench_economic_facts WHERE execution_id=$1`, f.targets[0].execution).Scan(&project); err != nil {
		t.Fatal(err)
	}
	f.request.Filters.SourceProjects = []string{project}
	onlyFirst := f.reading(t)
	if onlyFirst.Purpose == nil || onlyFirst.Purpose.Status != "unavailable" {
		t.Fatal("hidden basis was interpreted as unknown", onlyFirst)
	}
	if _, err = f.store.GetWorkbenchPurpose(f.ctx, f.subject, f.targets[0].execution, "", 0, time.Now()); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatal("hidden basis exposed history", err)
	}
	summary, err := f.store.GetWorkbenchSummary(f.ctx, f.subject, mixed.ID, time.Now())
	if err != nil || summary.SelectedAmountMicros != "40" {
		t.Fatal("purpose scope damaged economic read", err)
	}
}

func TestPurposeNativeRejectsDamagedRestoreAndMutation(t *testing.T) {
	f := newPurposeFixture(t, []string{"17"})
	f.accept(t, 0, f.declare(t, 0, "software_check", 0))
	f.reading(t)
	for _, statement := range []string{`UPDATE workbench_purpose_revisions SET actor_subject=actor_subject`, `DELETE FROM workbench_purpose_members`, `TRUNCATE workbench_purpose_manifests`} {
		if _, err := f.admin.Exec(statement); err == nil {
			t.Fatal("immutable state accepted mutation", statement)
		}
	}
	for name, damage := range map[string]string{
		"missing_predecessor":    `ALTER TABLE workbench_purpose_revisions DISABLE TRIGGER workbench_purpose_revisions_immutable; DELETE FROM workbench_purpose_revisions WHERE revision=1; ALTER TABLE workbench_purpose_revisions ENABLE ALWAYS TRIGGER workbench_purpose_revisions_immutable`,
		"wrong_member_reference": `ALTER TABLE workbench_purpose_members DISABLE TRIGGER workbench_purpose_members_immutable; UPDATE workbench_purpose_members SET purpose_revision=1; ALTER TABLE workbench_purpose_members ENABLE ALWAYS TRIGGER workbench_purpose_members_immutable`,
		"altered_manifest":       `ALTER TABLE workbench_purpose_manifests DISABLE TRIGGER workbench_purpose_manifests_immutable; UPDATE workbench_purpose_manifests SET manifest_sha256='sha256:'||repeat('0',64); ALTER TABLE workbench_purpose_manifests ENABLE ALWAYS TRIGGER workbench_purpose_manifests_immutable`,
	} {
		t.Run(name, func(t *testing.T) {
			tx, err := f.admin.BeginTx(f.ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.Exec(damage); err != nil {
				t.Fatal(err)
			}
			if err = db.ValidateWorkbenchRestore(f.ctx, tx); err == nil {
				t.Fatal("restore accepted damaged purpose history")
			}
		})
	}
	if err := db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal("damage checks changed original state", err)
	}
}

func TestPurposeNativePermissionsConflictAndWithdrawal(t *testing.T) {
	f := newPurposeFixture(t, []string{"17", "23"})
	e := f.accept(t, 0, f.declare(t, 0, "software_check", 0))
	old := f.reading(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := f.run(workbench.PurposeCommand{Action: "withdraw_adoption", ClaimID: e.State.AdoptedClaim, ExpectedRevision: 2, IdempotencyKey: fmt.Sprintf("concurrent-%d", i), Reason: "Withdraw current adoption"}, 0)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success, stale := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, workbench.ErrStaleRevision) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatalf("concurrent %d/%d", success, stale)
	}
	v, err := f.store.GetWorkbenchPurpose(f.ctx, f.subject, f.targets[0].execution, "", 0, time.Now())
	if err != nil || v.State.Status != "unknown" || v.State.AdoptedClaim != "" {
		t.Fatalf("withdraw %+v %v", v, err)
	}
	if _, err = f.admin.ExecContext(f.ctx, `UPDATE workbench_access_grants SET allow_purpose_review=false,revision=revision+1 WHERE subject_id=$1`, f.subject); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.GetWorkbenchSummary(f.ctx, f.subject, old.ID, time.Now()); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatalf("old reading survived grant revision: %v", err)
	}
	if _, err = f.run(workbench.PurposeCommand{Action: "accept", ClaimID: e.State.AdoptedClaim, ExpectedRevision: 3, IdempotencyKey: "after-write-revoke", Reason: "Must be denied"}, 0); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatalf("write revoked: %v", err)
	}
	current := f.reading(t)
	if current.Purpose == nil || current.Purpose.Status != "available" {
		t.Fatal("read disappeared with write-only revocation")
	}
	if _, err = f.admin.ExecContext(f.ctx, `UPDATE workbench_access_grants SET allow_purpose_read=false,revision=revision+1 WHERE subject_id=$1`, f.subject); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.GetWorkbenchPurpose(f.ctx, f.subject, f.targets[0].execution, "", 0, time.Now()); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatalf("read revoked: %v", err)
	}
	onlyMoney := f.reading(t)
	summary, err := f.store.GetWorkbenchSummary(f.ctx, f.subject, onlyMoney.ID, time.Now())
	if err != nil || summary.SelectedAmountMicros != "40" || onlyMoney.Purpose != nil {
		t.Fatalf("economic read %+v %+v %v", onlyMoney, summary, err)
	}
	if _, err = f.store.GetWorkbenchContributions(f.ctx, f.subject, onlyMoney.ID, "purpose", 50, 0, "", time.Now()); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatalf("no-purpose aggregation %v", err)
	}
}
