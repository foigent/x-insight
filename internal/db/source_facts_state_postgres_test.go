package db_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestSourceFactsStateRecoveryAndCurrentScope(t *testing.T) {
	f := newSourceFixture(t)
	f.allow()
	f.money()
	initial := f.reading()
	if !slices.Equal(initial.SourceCoverage.CollectionStates, []string{"active"}) {
		t.Fatal("active state missing")
	}
	p := sourcefacts.IdentityProfile
	original := f.documents[p]
	changed := original
	changed.Identity = copySourceMap(original.Identity)
	changed.Identity["owner_user_id"] = "8901"
	hash := f.hash(p)
	f.setPage(p, "conflicting-owner", changed)
	before, err := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[p])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.consumer.PullOnce(f.ctx, f.streams[p]); !errors.Is(err, sourcefacts.ErrConflict) {
		t.Fatal(err)
	}
	failed := f.reading()
	if !slices.Contains(failed.SourceCoverage.Gaps, "source_fact_conflict") || f.hash(p) != hash {
		t.Fatal("conflict state or retained bytes incorrect")
	}
	after, err := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[p])
	if err != nil || before.Cursor != after.Cursor || before.HighWatermark != after.HighWatermark {
		t.Fatal("bad page advanced", err)
	}
	f.exec(`UPDATE source_fact_streams SET revision=revision+1,status='paused' WHERE id=$1::uuid`, f.streams[p])
	paused := f.reading()
	if !slices.Equal(paused.SourceCoverage.CollectionStates, []string{"active", "paused"}) {
		t.Fatal("mixed registrations lost", paused.SourceCoverage)
	}
	f.exec(`UPDATE source_fact_streams SET revision=revision+1,status='active',requires_attention=false,last_error='' WHERE id=$1::uuid`, f.streams[p])
	f.setPage(p, "recovered-original", original)
	f.pull(p)
	recovered := f.reading()
	if !slices.Equal(recovered.SourceCoverage.CollectionStates, []string{"active"}) || slices.Contains(recovered.SourceCoverage.Gaps, "source_fact_conflict") {
		t.Fatal("successful receipt did not recover state")
	}
	var code string
	if err = f.admin.QueryRow(`SELECT last_error FROM source_fact_streams WHERE id=$1::uuid`, f.streams[p]).Scan(&code); err != nil || code != "" {
		t.Fatal("failure not cleared", err)
	}
	for _, r := range []workbench.Reading{initial, failed, paused, recovered} {
		current, e := f.workspace.GetWorkbenchReading(f.ctx, f.subject, r.ID, time.Now())
		if e != nil || current.SourceManifestRef != r.SourceManifestRef || !reflect.DeepEqual(current.SourceCoverage, r.SourceCoverage) {
			t.Fatal("old state backfilled", e)
		}
		if f.sum(r).CalculatedReferenceMicros != "30000000" || f.sum(r).RequestCount != 1 {
			t.Fatal("state changed economic amount")
		}
	}
	// Hidden profile states and errors cannot leak through aggregated coverage.
	f.exec(`UPDATE source_fact_streams SET revision=revision+1,status='revoked',last_error='source_fact_conflict' WHERE id=$1::uuid`, f.streams[p])
	f.exec(`UPDATE workbench_access_grants SET allow_source_identity=false WHERE subject_id=$1`, f.subject)
	denied := f.reading()
	if denied.SourceCoverage.KnownDocuments != 2 || !slices.Equal(denied.SourceCoverage.CollectionStates, []string{"active"}) || slices.Contains(denied.SourceCoverage.Gaps, "source_fact_conflict") {
		t.Fatal("profile status/count leak")
	}
	if _, err = f.workspace.GetWorkbenchReading(f.ctx, f.subject, failed.ID, time.Now()); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("person revocation not current", err)
	}
	// Another project is independently registered but outside the user's grant.
	f.exec(`INSERT INTO source_fact_streams(id,source_system_id,source_instance,org_id,project_id,profile,scope_generation,status,last_error) VALUES($1::uuid,$2::uuid,'ur-demo-01','17','999',$3,'1','revoked','cursor_expired')`, testUUID(t), f.source, sourcefacts.ContextProfile)
	f.exec(`UPDATE workbench_access_grants SET source_project_ref='project-101' WHERE subject_id=$1`, f.subject)
	req := f.request()
	req.Filters.SourceProjects = []string{"project-101"}
	scoped, e := f.workspace.CreateWorkbenchReading(f.ctx, f.subject, req, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if !slices.Equal(scoped.SourceCoverage.CollectionStates, []string{"active"}) || slices.Contains(scoped.SourceCoverage.Gaps, "cursor_expired") {
		t.Fatal("project grant status leak")
	}
	req.Filters.SourceProjects = []string{"project-999"}
	if _, e = f.workspace.CreateWorkbenchReading(f.ctx, f.subject, req, time.Now()); !errors.Is(e, workbench.ErrScopeForbidden) {
		t.Fatal("out-of-grant project allowed", e)
	}
	if err = db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal(err)
	}
}

type sourceFailureInterleave struct {
	*db.SourceFactsStore
	before func()
}

func (s sourceFailureInterleave) RecordSourceFactsFailure(ctx context.Context, expected sourcefacts.Stream, code string, now time.Time) error {
	s.before()
	return s.SourceFactsStore.RecordSourceFactsFailure(ctx, expected, code, now)
}

func TestSourceFactsStaleRequestsDoNotBecomeSourceFailures(t *testing.T) {
	f := newSourceFixture(t)
	f.allow()
	p := sourcefacts.IdentityProfile
	f.setPage(p, "converged", f.documents[p])
	page := f.pages[p]
	expected, e := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[p])
	if e != nil {
		t.Fatal(e)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() { <-start; results <- f.consumerStore.CommitSourceFactsPage(f.ctx, expected, page, time.Now()) }()
	}
	close(start)
	a, b := <-results, <-results
	if !((a == nil && errors.Is(b, sourcefacts.ErrStreamChanged)) || (b == nil && errors.Is(a, sourcefacts.ErrStreamChanged))) {
		t.Fatalf("converged concurrent requests: %v / %v", a, b)
	}
	for _, code := range []string{"source_fact_conflict", "invalid_page"} {
		if e = f.consumerStore.RecordSourceFactsFailure(f.ctx, expected, code, time.Now()); !errors.Is(e, sourcefacts.ErrStreamChanged) {
			t.Fatal("stale failure overwritten progress", e)
		}
	}
	if slices.Contains(f.reading().SourceCoverage.Gaps, "source_fact_conflict") {
		t.Fatal("normal concurrency became source conflict")
	}
	// A second consumer succeeds between the rejected transaction and the
	// error-state transaction. The old failure must lose its compare-and-swap.
	original := f.documents[p]
	f.setPage(p, "newer-success", original)
	good := f.pages[p]
	expected, e = f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[p])
	if e != nil {
		t.Fatal(e)
	}
	changed := original
	changed.Identity = copySourceMap(original.Identity)
	changed.Identity["owner_user_id"] = "8902"
	f.setPage(p, "bad-page", changed)
	c := f.consumer
	c.Store = sourceFailureInterleave{SourceFactsStore: f.consumerStore, before: func() {
		if err := f.consumerStore.CommitSourceFactsPage(f.ctx, expected, good, time.Now()); err != nil {
			t.Fatal(err)
		}
	}}
	if _, e = c.PullOnce(f.ctx, f.streams[p]); !errors.Is(e, sourcefacts.ErrStreamChanged) {
		t.Fatal("newer progress lost to old failure", e)
	}
	after, e := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[p])
	if e != nil || after.Cursor != "newer-success" {
		t.Fatal("new progress lost", e)
	}
	var last string
	if e = f.admin.QueryRow(`SELECT last_error FROM source_fact_streams WHERE id=$1::uuid`, f.streams[p]).Scan(&last); e != nil || last != "" {
		t.Fatal("stale conflict persisted", e)
	}
	// Pausing after a request starts is likewise a local state change, not a
	// contradictory source. No further document or failure write is admitted.
	f.exec(`UPDATE source_fact_streams SET revision=revision+1,status='paused' WHERE id=$1::uuid`, f.streams[p])
	f.setPage(p, "after-pause", original)
	good = f.pages[p]
	if e = f.consumerStore.CommitSourceFactsPage(f.ctx, after, good, time.Now()); !errors.Is(e, sourcefacts.ErrStreamChanged) {
		t.Fatal("paused stale request committed", e)
	}
	if e = f.consumerStore.RecordSourceFactsFailure(f.ctx, after, "source_fact_conflict", time.Now()); !errors.Is(e, sourcefacts.ErrStreamChanged) {
		t.Fatal("paused stale failure recorded", e)
	}
}
