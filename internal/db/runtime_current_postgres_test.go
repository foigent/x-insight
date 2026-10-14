package db_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func currentRuntimeWire(t *testing.T, s sourcefacts.Stream, at time.Time, next string, entries ...sourcefacts.CurrentRuntimeEntry) []byte {
	t.Helper()
	p := sourcefacts.CurrentRuntimePage{SchemaVersion: sourcefacts.CurrentRuntimeVersion, SourceInstance: s.SourceInstance, OrgID: s.OrgID, ProjectID: s.ProjectID, Profile: sourcefacts.RuntimeProfile, ObservedAt: at.Format(sourcefacts.TimeFormat), Coverage: "lease_backed_inventory", Uncovered: []string{"synchronous_requests_without_execution_lease", "progress_between_lease_observations"}, Entries: entries, HasMore: next != "", NextCursor: next}
	if p.Entries == nil {
		p.Entries = []sourcefacts.CurrentRuntimeEntry{}
	}
	raw, _ := json.Marshal(p)
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := connector.CanonicalizeJSONValue(m)
	if err != nil {
		t.Fatal(err)
	}
	p.SourceSHA256 = sourcefacts.Hash(unsigned)
	raw, _ = json.Marshal(p)
	if _, err = sourcefacts.DecodeCurrentRuntimePage(raw, s.SourceInstance, s.OrgID, s.ProjectID); err != nil {
		t.Fatal("snapshot fixture", err)
	}
	return raw
}

func TestRuntimeCurrentEnumerationScopeFreshnessAndRestore(t *testing.T) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	putRuntimeDocuments(t, f, f.source, gold[0])
	var id string
	if err := f.admin.QueryRow(`SELECT id::text FROM source_fact_streams WHERE source_system_id=$1::uuid AND profile=$2`, f.source, sourcefacts.RuntimeProfile).Scan(&id); err != nil {
		t.Fatal(err)
	}
	s, err := f.consumerStore.LoadSourceFactsStream(f.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	observed := now.Add(-time.Second)
	active := sourcefacts.CurrentRuntimeEntry{RequestID: "current-active", Family: "stream", SubjectKind: "provider_attempt", SubjectID: "attempt-1", Operation: "openai.chat_completions.v1", Freshness: "lease_valid_execution", OriginalState: "streaming", LastObservedAt: observed.Format(sourcefacts.TimeFormat), ValidUntil: now.Add(time.Minute).Format(sourcefacts.TimeFormat), LeaseGeneration: "1", EvidenceRef: "uf1_" + strings.Repeat("a", 64)}
	pending := active
	pending.RequestID = "current-pending"
	pending.Family = "media"
	pending.SubjectKind = "media_job"
	pending.SubjectID = "media-1"
	pending.Freshness = "leased_pending"
	pending.OriginalState = "pending"
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id=$1`, f.subject)
	old, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, runtimeRequestInput(t, gold[0]), now)
	if err != nil {
		t.Fatal(err)
	}
	// Compare the persisted representation before and after inventory writes.
	// time.Time's internal location representation is not an API equality rule.
	baseline, err := f.workspace.GetRuntimeReading(f.ctx, f.subject, old.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.consumerStore.LoadCurrentRuntimeCursor(f.ctx, s)
	if !old.Period.StartInclusive.Equal(baseline.Period.StartInclusive) || !old.Period.EndExclusive.Equal(baseline.Period.EndExclusive) || !old.Cutoff.Equal(baseline.Cutoff) || !old.ExpiresAt.Equal(baseline.ExpiresAt) {
		t.Fatal("persisted reading changed an instant before inventory writes")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.CommitCurrentRuntimePage(f.ctx, s, first, currentRuntimeWire(t, s, observed, "second", active), now); err != nil {
		t.Fatal(err)
	}
	read := func(at time.Time) workbench.RuntimeCurrentInventory {
		t.Helper()
		v, err := f.workspace.GetRuntimeCurrent(f.ctx, f.subject, workbench.Filters{}, 100, "", at)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if len(read(now).Items) != 0 {
		t.Fatal("partial inventory presented as complete enumeration")
	}
	second, err := f.consumerStore.LoadCurrentRuntimeCursor(f.ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.CommitCurrentRuntimePage(f.ctx, s, first, currentRuntimeWire(t, s, observed, "", pending), now); !errors.Is(err, sourcefacts.ErrStreamChanged) {
		t.Fatal("stale page overwrote progress", err)
	}
	if err = f.consumerStore.CommitCurrentRuntimePage(f.ctx, s, second, currentRuntimeWire(t, s, observed, "", pending), now); err != nil {
		t.Fatal(err)
	}
	got := read(now)
	if len(got.Items) != 2 {
		t.Fatal(got)
	}
	states := map[string]string{}
	for _, item := range got.Items {
		states[item.Entry.RequestID] = item.CurrentState
	}
	if states["current-active"] != "lease_valid_execution" || states["current-pending"] != "leased_pending" {
		t.Fatal(states)
	}
	page, err := f.workspace.GetRuntimeCurrent(f.ctx, f.subject, workbench.Filters{}, 1, "", now)
	if err != nil || page.NextCursor == "" {
		t.Fatal(page, err)
	}
	next, err := f.workspace.GetRuntimeCurrent(f.ctx, f.subject, workbench.Filters{}, 1, page.NextCursor, now)
	if err != nil || len(next.Items) != 1 || next.Items[0].Ref == page.Items[0].Ref || next.NextCursor != "" {
		t.Fatal(next, err)
	}
	if _, err = f.workspace.GetRuntimeCurrent(f.ctx, f.subject, workbench.Filters{SourceProjects: []string{"project-" + s.ProjectID}}, 1, page.NextCursor, now); !errors.Is(err, workbench.ErrInvalid) {
		t.Fatal("cross-filter current cursor", err)
	}
	unpriced, err := f.workspace.GetRuntimeCurrent(f.ctx, f.subject, workbench.Filters{CostProjects: []string{"cost-project-without-anchor"}}, 100, "", now)
	if err != nil || len(unpriced.Items) != 0 {
		t.Fatal("invented economic join", unpriced, err)
	}
	for _, item := range read(now.Add(time.Minute)).Items {
		if item.CurrentState != "not_current" {
			t.Fatal("expired lease promoted", item)
		}
	}
	current, err := f.consumerStore.LoadCurrentRuntimeCursor(f.ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.RecordCurrentRuntimeFailure(f.ctx, s, current, "source_unavailable", now); err != nil {
		t.Fatal(err)
	}
	for _, item := range read(now).Items {
		if item.CurrentState != "not_current" {
			t.Fatal("known source failure ignored")
		}
	}
	after, err := f.consumerStore.LoadSourceFactsStream(f.ctx, id)
	if err != nil || after != s {
		t.Fatal("snapshot advanced/reset event stream", after, err)
	}
	frozen, err := f.workspace.GetRuntimeReading(f.ctx, f.subject, old.ID, now)
	if err != nil || !reflect.DeepEqual(frozen, baseline) {
		t.Fatal("current cache changed historical reading", err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=false WHERE subject_id=$1`, f.subject)
	if _, err = f.workspace.GetRuntimeCurrent(f.ctx, f.subject, workbench.Filters{}, 100, "", now); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatal("current grant revocation", err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true,source_project_ref='project-999' WHERE subject_id=$1`, f.subject)
	if len(read(now).Items) != 0 {
		t.Fatal("source scope leak")
	}
	f.exec(`UPDATE workbench_access_grants SET source_project_ref=NULL WHERE subject_id=$1`, f.subject)
	if err = db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE source_runtime_snapshot_pages SET source_sha256='sha256:'||repeat('b',64)`)
	if err = db.ValidateWorkbenchRestore(f.ctx, f.admin); err == nil {
		t.Fatal("corrupt cache accepted")
	}
	f.exec(`UPDATE source_runtime_snapshot_pages SET source_sha256=source_json->>'source_sha256'`)
	if err = db.SuspendRestoredSourceFacts(f.ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	if len(read(now).Items) != 0 {
		t.Fatal("restored cache retained current eligibility")
	}
	var pages, money int
	if err = f.admin.QueryRow(`SELECT (SELECT count(*) FROM source_runtime_snapshot_pages),(SELECT count(*) FROM workbench_economic_facts)`).Scan(&pages, &money); err != nil || pages != 0 || money != 0 {
		t.Fatal(pages, money, err)
	}
}

func TestRuntimeCurrentPermanentFailureNeedsAcknowledgmentWithoutBlockingEvents(t *testing.T) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	putRuntimeDocuments(t, f, f.source, gold[0])
	var id string
	if err := f.admin.QueryRow(`SELECT id::text FROM source_fact_streams WHERE source_system_id=$1::uuid AND profile=$2`, f.source, sourcefacts.RuntimeProfile).Scan(&id); err != nil {
		t.Fatal(err)
	}
	s, err := f.consumerStore.LoadSourceFactsStream(f.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.consumerStore.LoadCurrentRuntimeCursor(f.ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = f.consumerStore.RecordCurrentRuntimeFailure(f.ctx, s, c, "invalid_page", now); err != nil {
		t.Fatal(err)
	}
	if _, err = f.consumerStore.LoadCurrentRuntimeCursor(f.ctx, s); !errors.Is(err, sourcefacts.ErrUnavailable) {
		t.Fatal("permanent failure retried automatically", err)
	}
	after, err := f.consumerStore.LoadSourceFactsStream(f.ctx, id)
	if err != nil || after != s {
		t.Fatal("current failure blocked event collection", err)
	}
	if _, err = f.consumerStore.ControlSourceFactsStream(f.ctx, id, "active", s.Revision, false); !errors.Is(err, sourcefacts.ErrUnavailable) {
		t.Fatal("current attention silently cleared", err)
	}
	resumed, err := f.consumerStore.ControlSourceFactsStream(f.ctx, id, "active", s.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Cursor != s.Cursor || resumed.HighWatermark != s.HighWatermark || resumed.Revision != s.Revision+1 {
		t.Fatal("acknowledgment reset history", resumed)
	}
	next, err := f.consumerStore.LoadCurrentRuntimeCursor(f.ctx, resumed)
	if err != nil || next.RequiresAttention || next.Cursor != "" {
		t.Fatal(next, err)
	}
	if err = f.consumerStore.CommitCurrentRuntimePage(f.ctx, s, next, currentRuntimeWire(t, s, now.Add(-time.Second), ""), now); !errors.Is(err, sourcefacts.ErrStreamChanged) {
		t.Fatal("stale registration wrote after acknowledge", err)
	}
	if err = f.consumerStore.CommitCurrentRuntimePage(f.ctx, resumed, next, currentRuntimeWire(t, resumed, now.Add(-time.Second), ""), now); err != nil {
		t.Fatal(err)
	}
}
