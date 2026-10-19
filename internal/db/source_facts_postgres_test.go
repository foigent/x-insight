package db_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/testsourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

type sourceFixture struct {
	t                              *testing.T
	ctx                            context.Context
	dsn, source, subject           string
	admin                          *sql.DB
	consumerStore                  *db.SourceFactsStore
	workspace                      *db.WorkspaceStore
	evidence                       *db.EvidenceStore
	consumer                       sourcefacts.Consumer
	mu                             sync.Mutex
	pages                          map[string]sourcefacts.Page
	generation, status, credential string
	httpStatus                     int
	httpCalls                      int
	streams                        map[string]string
	documents                      map[string]sourcefacts.Document
	i01                            []byte
}

func newSourceFixture(t *testing.T) *sourceFixture {
	t.Helper()
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("PG required")
	}
	f := &sourceFixture{t: t, ctx: t.Context(), pages: map[string]sourcefacts.Page{}, generation: "1", status: "active", credential: "source-synthetic-one", streams: map[string]string{}, documents: map[string]sourcefacts.Document{}}
	f.dsn = workspaceTestDSN(t, f.ctx, base)
	if _, err := db.RunMigrations(f.ctx, f.dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	f.admin = openTestSQL(t, f.dsn)
	t.Cleanup(func() { f.admin.Close() })
	f.source = testUUID(t)
	f.subject = "xs_a1a1a1a1a1a1a1a1"
	f.exec(`INSERT INTO source_systems(id,source_key,source_type,display_name,source_version,status) VALUES($1::uuid,$2,'unirouter','Synthetic Source Facts','unirouter.completed-execution.v1','active')`, f.source, "source-system:"+f.source)
	f.exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences) VALUES($1,$2::uuid,true,true,true,true)`, f.subject, f.source)
	var err error
	f.consumerStore, err = db.OpenSourceFactsStore(f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.consumerStore.Close() })
	f.workspace, err = db.OpenWorkspaceStore(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.workspace.Close() })
	f.evidence, err = db.OpenEvidenceStore(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.evidence.Close() })
	loadSourceFixtureDocuments(t, f)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.httpCalls++
		if r.Header.Get("Authorization") != "Bearer "+f.credential {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/integrations/xinsight/source-facts/capabilities" {
			if r.URL.RawQuery != "" {
				w.WriteHeader(400)
				return
			}
			json.NewEncoder(w).Encode(sourcefacts.Capabilities{SchemaVersion: "unirouter.source-capabilities.v1", SourceInstance: "ur-demo-01", Profiles: []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile}, ProfileVersion: "1.0.0", Status: f.status, CredentialStatus: "feed_active", ScopeGeneration: f.generation, RetentionSeconds: 604800, Bootstrap: "forward_only", FieldAuthorization: "fixed_profiles"})
			return
		}
		if f.httpStatus != 0 {
			w.WriteHeader(f.httpStatus)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "cursor_expired"}})
			return
		}
		p, ok := f.pages[r.URL.Query().Get("profile")]
		if !ok {
			w.WriteHeader(403)
			return
		}
		if r.URL.Query().Get("cursor") == "" {
			p.Events = []json.RawMessage{}
			p.Items = nil
			p.NextCursor = "fixture-start"
			p.HighWatermark = p.NextCursor
			p.HasMore = false
		}
		json.NewEncoder(w).Encode(p)
	}))
	t.Cleanup(server.Close)
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{SyncRequired: true, Endpoint: server.URL + "/api/v1/integrations/xinsight/source-facts", ServiceKey: f.credential, AllowPrivateTunnelHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	f.consumer = sourcefacts.Consumer{Store: f.consumerStore, Reader: reader}
	f.bindSyncScope("ur-demo-01", "17", "101")
	for _, p := range []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile} {
		id := testUUID(t)
		f.streams[p] = id
		f.exec(`INSERT INTO source_fact_streams(id,source_system_id,source_instance,org_id,project_id,profile,scope_generation,status) VALUES($1::uuid,$2::uuid,'ur-demo-01','17','101',$3,'1','active')`, id, f.source, p)
		f.setPage(p, "fixture-next", f.documents[p])
		f.pull(p)
		f.pull(p)
	}
	return f
}

func (f *sourceFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.admin.ExecContext(f.ctx, query, args...); err != nil {
		f.t.Fatal(err)
	}
}
func (f *sourceFixture) setPage(profile, next string, documents ...sourcefacts.Document) {
	f.t.Helper()
	events := []json.RawMessage{}
	for _, d := range documents {
		d.SourceFactID = sourcefacts.SourceID(sourcefacts.Version, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.Stage)
		d.EventID = sourcefacts.SourceID(sourcefacts.Version+".revision", d.SourceFactID, d.Revision)
		raw, _ := json.Marshal(d)
		value, err := connector.DecodeStrictJSONObject(raw)
		if err != nil {
			f.t.Fatal(err)
		}
		canonical, err := connector.CanonicalizeJSONValue(value)
		if err != nil {
			f.t.Fatal(err)
		}
		wire, err := sourcefacts.Envelope(d, canonical)
		if err != nil {
			f.t.Fatal(err)
		}
		events = append(events, wire)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages[profile] = sourcefacts.Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: sourcefacts.ConnectorID, ConnectorVersion: sourcefacts.ConnectorVersion, ProducerInstance: "ur-demo-01", GeneratedAt: "2026-09-10T00:00:00.000000Z", Events: events, NextCursor: next, HighWatermark: next}
}
func (f *sourceFixture) pull(profile string) {
	f.t.Helper()
	if _, err := f.consumer.PullOnce(f.ctx, f.streams[profile]); err != nil {
		f.t.Fatal(err)
	}
}
func (f *sourceFixture) allow() {
	f.exec(`UPDATE workbench_access_grants SET allow_source_context=true,allow_source_identity=true,allow_source_cost=true WHERE subject_id=$1`, f.subject)
}

// Retained contract samples have fixed occurrence times. Anchor the test query
// to those samples so the passing window does not expire with the wall clock.
func (f *sourceFixture) request() workbench.ReadingRequest {
	f.t.Helper()
	var start, end time.Time
	if err := f.admin.QueryRowContext(f.ctx, `SELECT min(occurred_at),max(occurred_at) FROM source_fact_documents`).Scan(&start, &end); err != nil {
		f.t.Fatal(err)
	}
	start, end = start.Add(-time.Hour), end.Add(time.Hour)
	request := relativeWorkbenchRequest()
	request.Period = workbench.PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "UTC"}
	return request
}

func (f *sourceFixture) reading() workbench.Reading {
	f.t.Helper()
	request := f.request()
	r, err := f.workspace.CreateWorkbenchReading(f.ctx, f.subject, request, time.Now().UTC())
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}
func (f *sourceFixture) sum(r workbench.Reading) workbench.Summary {
	f.t.Helper()
	s, err := f.workspace.GetWorkbenchSummary(f.ctx, f.subject, r.ID, time.Now().UTC())
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}
func (f *sourceFixture) money() {
	f.t.Helper()
	mapped, err := unirouter.MapCompletedExecutionV1(f.i01, unirouter.CompletedExecutionMapOptions{SourceInstance: "ur-demo-01", SourceSystemID: f.source, AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID, ObservedAt: time.Now().UTC(), IngestedAt: time.Now().UTC()})
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err = f.evidence.PersistEvents(f.ctx, mapped.Events); err != nil {
		f.t.Fatal(err)
	}
}
func (f *sourceFixture) hash(profile string) string {
	f.t.Helper()
	var h string
	if err := f.admin.QueryRowContext(f.ctx, `SELECT source_hash FROM source_fact_documents WHERE source_system_id=$1::uuid AND profile=$2 ORDER BY revision DESC LIMIT 1`, f.source, profile).Scan(&h); err != nil {
		f.t.Fatal(err)
	}
	return h
}

func TestSourceFactsHTTPArrivalRevisionReadingAndCurrentPermission(t *testing.T) {
	f := newSourceFixture(t)
	legacy := f.reading()
	if legacy.SourceManifestRef != "" {
		t.Fatal("default fields acquired source access")
	}
	if old, err := f.workspace.GetWorkbenchReading(f.ctx, f.subject, legacy.ID, time.Now().UTC()); err != nil || old.InterpretationRevision != legacy.InterpretationRevision || old.SourceManifestRef != "" {
		t.Fatalf("original A0 reading unavailable: %+v %v", old, err)
	}
	var legacyStored string
	if err := f.admin.QueryRowContext(f.ctx, `SELECT to_jsonb(r)::text FROM workbench_readings r WHERE id=$1`, legacy.ID).Scan(&legacyStored); err != nil {
		t.Fatal(err)
	}
	f.allow()
	first := f.reading()
	s := f.sum(first)
	if s.RequestCount != 0 || s.SourceCoverage == nil || s.SourceCoverage.KnownDocuments != 3 || s.SourceCoverage.EconomicNotReceived != 3 {
		t.Fatalf("source-first state: %+v", s)
	}
	observed, err := f.workspace.GetWorkbenchSourceFacts(f.ctx, f.subject, first.ID, 10, "", time.Now().UTC())
	if err != nil || len(observed.Items) != 3 {
		t.Fatalf("independent source visibility: %+v %v", observed, err)
	}
	if _, err := f.workspace.GetWorkbenchReading(f.ctx, f.subject, legacy.ID, time.Now().UTC()); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatalf("changed authorization served an earlier reading: %v", err)
	}
	var legacyAfter string
	if err := f.admin.QueryRowContext(f.ctx, `SELECT to_jsonb(r)::text FROM workbench_readings r WHERE id=$1`, legacy.ID).Scan(&legacyAfter); err != nil || legacyAfter != legacyStored {
		t.Fatal("authorization change rewrote the earlier reading", err)
	}
	f.money()
	f.money()
	second := f.reading()
	s = f.sum(second)
	if s.RequestCount != 1 || s.CalculatedReferenceMicros != "30000000" || s.GatewayChargedMicros != "0" || s.SourceCoverage.EconomicNotReceived != 0 {
		t.Fatalf("money once: %+v", s)
	}
	if old := f.sum(first); old.RequestCount != 0 || old.SourceCoverage.EconomicNotReceived != 3 {
		t.Fatal("late money changed old reading")
	}
	for dimension, want := range map[string]string{"application": "9007199254740993", "account": "9007199254740995"} {
		groups, err := f.workspace.GetWorkbenchContributions(f.ctx, f.subject, second.ID, dimension, 0, 10, "", time.Now().UTC())
		if err != nil || len(groups.Rows) != 1 || groups.Rows[0].Label != want || groups.Rows[0].RequestCount != 1 {
			t.Fatalf("%s group %+v %v", dimension, groups, err)
		}
		details, err := f.workspace.GetWorkbenchDetails(f.ctx, f.subject, second.ID, groups.Rows[0].Ref, 50, "", time.Now().UTC())
		if err != nil || len(details.Rows) != 1 || len(details.Rows[0].SourceFacts) != 3 || details.Rows[0].AccountState != "known_account_owner" {
			t.Fatalf("same manifest details %+v %v", details, err)
		}
	}
	// Cost profile revision 2 continues to refer to economic revision 1.
	cost := f.documents[sourcefacts.CostProfile]
	cost.Revision = "2"
	cost.PreviousRevision = "1"
	cost.PreviousHash = f.hash(cost.Profile)
	cost.CorrectionReason = "basis source annotation corrected"
	f.setPage(cost.Profile, "cost-second", cost)
	f.pull(cost.Profile)
	third := f.reading()
	s = f.sum(third)
	if s.RequestCount != 1 || s.SourceCoverage.EconomicNotReceived != 0 || s.SourceCoverage.EconomicHashConflicts != 0 {
		t.Fatalf("profile/economic revision conflated: %+v", s)
	}
	if err := db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	// New fields have their own current permission digest; no legacy boolean
	// or Evidence/Case role can expose them after revocation.
	f.exec(`UPDATE workbench_access_grants SET allow_source_identity=false WHERE subject_id=$1`, f.subject)
	if _, err := f.workspace.GetWorkbenchSummary(f.ctx, f.subject, third.ID, time.Now().UTC()); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatalf("revoked saved reading: %v", err)
	}
	if _, err := f.workspace.GetWorkbenchSourceFacts(f.ctx, f.subject, third.ID, 10, "", time.Now().UTC()); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatalf("revoked source detail: %v", err)
	}
	current := f.reading()
	if current.SourceCoverage.KnownDocuments != 2 {
		t.Fatal("denied identity leaked through count")
	}
	if _, err := f.workspace.GetWorkbenchContributions(f.ctx, f.subject, current.ID, "account", 0, 10, "", time.Now().UTC()); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatalf("denied identity grouping: %v", err)
	}
	if _, err := f.workspace.GetWorkbenchReading(f.ctx, f.subject, legacy.ID, time.Now().UTC()); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("later revocation restored an earlier reading's access", err)
	}
	if err := f.admin.QueryRowContext(f.ctx, `SELECT to_jsonb(r)::text FROM workbench_readings r WHERE id=$1`, legacy.ID).Scan(&legacyAfter); err != nil || legacyAfter != legacyStored {
		t.Fatal("later revocation rewrote the earlier reading", err)
	}
}

func TestSourceFactsPendingPredecessorAdvancesAndConflictsAreAtomic(t *testing.T) {
	f := newSourceFixture(t)
	f.allow()
	f.money()
	profile := sourcefacts.IdentityProfile
	v2 := f.documents[profile]
	v2.Revision = "2"
	v2.PreviousRevision = "1"
	v2.PreviousHash = f.hash(profile)
	v2.CorrectionReason = "revision two"
	f.setPage(profile, "identity-two", v2)
	f.mu.Lock()
	item, err := sourcefacts.ValidateItem(f.pages[profile].Events[0], profile)
	f.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	v3 := v2
	v3.Revision = "3"
	v3.PreviousRevision = "2"
	v3.PreviousHash = item.SourceHash
	v3.CorrectionReason = "revision three"
	f.setPage(profile, "identity-three-first", v3)
	f.pull(profile)
	stream, err := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[profile])
	if err != nil || stream.Cursor != "identity-three-first" {
		t.Fatalf("pending blocked receipt progress: %+v %v", stream, err)
	}
	pending := f.reading()
	if pending.SourceCoverage.PendingPredecessor != 1 {
		t.Fatal("missing predecessor not visible")
	}
	groups, err := f.workspace.GetWorkbenchContributions(f.ctx, f.subject, pending.ID, "account", 0, 10, "", time.Now().UTC())
	if err != nil || len(groups.Rows) != 0 {
		t.Fatalf("pending identity adopted %+v %v", groups, err)
	}
	f.setPage(profile, "identity-two-later", v2)
	f.pull(profile)
	complete := f.reading()
	if complete.SourceCoverage.PendingPredecessor != 0 || f.sum(pending).SourceCoverage.PendingPredecessor != 1 {
		t.Fatal("late predecessor rewrote history")
	}
	if err := db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	// Same identity/revision with different valid canonical bytes is a conflict.
	v2.CorrectionReason = "conflicting revision two"
	f.setPage(profile, "must-not-advance", v2)
	before, _ := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[profile])
	if _, err := f.consumer.PullOnce(f.ctx, f.streams[profile]); !errors.Is(err, sourcefacts.ErrConflict) {
		t.Fatalf("conflict accepted: %v", err)
	}
	after, _ := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[profile])
	if before.Cursor != after.Cursor || before.HighWatermark != after.HighWatermark || after.Revision != before.Revision+1 {
		t.Fatal("conflict advanced checkpoint")
	}
	// A transaction-level checkpoint failure rolls back newly inserted documents.
	ctxDoc := f.documents[sourcefacts.ContextProfile]
	ctxDoc.RequestID = "checkpoint-failure"
	ctxDoc.Core = copySourceMap(ctxDoc.Core)
	ctxDoc.Core["request_id"] = ctxDoc.RequestID
	f.setPage(ctxDoc.Profile, "fail-checkpoint", ctxDoc)
	f.exec(`CREATE FUNCTION fail_source_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic checkpoint failure'; END $$; CREATE TRIGGER fail_source_checkpoint BEFORE UPDATE ON source_fact_streams FOR EACH ROW EXECUTE FUNCTION fail_source_checkpoint()`)
	if _, err := f.consumer.PullOnce(f.ctx, f.streams[ctxDoc.Profile]); err == nil {
		t.Fatal("checkpoint failure accepted")
	}
	var count int
	if err := f.admin.QueryRowContext(f.ctx, `SELECT count(*) FROM source_fact_documents WHERE request_id='checkpoint-failure'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("page partial writes: %d %v", count, err)
	}
	f.exec(`DROP TRIGGER fail_source_checkpoint ON source_fact_streams; DROP FUNCTION fail_source_checkpoint()`)
	f.pull(ctxDoc.Profile)
	for _, status := range []int{409, 410} {
		// Each independent fault starts after explicit administrator correction;
		// a restarted worker may not clear the previous persistent attention.
		current, _ := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[profile])
		if _, err := f.consumerStore.ControlSourceFactsStream(f.ctx, current.ID, "active", current.Revision, true); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		f.httpStatus = status
		f.mu.Unlock()
		before, _ := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[profile])
		if _, err := f.consumer.PullOnce(f.ctx, f.streams[profile]); err == nil {
			t.Fatal("HTTP gap ignored")
		}
		after, _ := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[profile])
		if after.Cursor != before.Cursor {
			t.Fatal("gap reset cursor")
		}
	}
	f.mu.Lock()
	f.httpStatus = 0
	f.generation = "2"
	f.mu.Unlock()
	current, _ := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[profile])
	if _, err := f.consumerStore.ControlSourceFactsStream(f.ctx, current.ID, "active", current.Revision, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.consumer.PullOnce(f.ctx, f.streams[profile]); !errors.Is(err, sourcefacts.ErrScopeChanged) {
		t.Fatalf("scope generation accepted: %v", err)
	}
	f.exec(`UPDATE source_fact_streams SET revision=revision+1,status='paused' WHERE id=$1::uuid`, f.streams[profile])
	if _, err := f.consumer.PullOnce(f.ctx, f.streams[profile]); !errors.Is(err, sourcefacts.ErrUnavailable) {
		t.Fatal("paused collection pulled")
	}
	for _, table := range []string{"source_fact_documents", "workbench_source_members", "workbench_source_manifests"} {
		if _, err := f.admin.ExecContext(f.ctx, `DELETE FROM `+table+` WHERE false`); err == nil {
			t.Fatal("immutable relation accepted delete", table)
		}
	}
	t.Logf("retained checkpoint after gaps; original economic amount=%s", strconv.Quote(f.sum(complete).CalculatedReferenceMicros))
}

func copySourceMap(m map[string]any) map[string]any {
	raw, _ := json.Marshal(m)
	var c map[string]any
	json.Unmarshal(raw, &c)
	return c
}

func TestSourceFactsAllRetainedHTTPExports(t *testing.T) {
	f := newSourceFixture(t)
	for _, name := range []string{testsourcefacts.Chat, testsourcefacts.Media,
		"TestSourceFactsMediaHTTPIntegration_grant_revoke_before_dispatch.json",
		"TestSourceFactsMediaHTTPIntegration_legacy_success_replay.json",
		"TestSourceFactsMediaHTTPIntegration_legacy_revoke_before_dispatch.json",
		"TestSourceFactsMediaHTTPIntegration_legacy_queued_cancel.json",
		"TestSourceFactsMediaHTTPIntegration_legacy_inflight_cancel.json",
		"TestSourceFactsMediaHTTPIntegration_grant_queued_cancel.json",
		"TestSourceFactsMediaHTTPIntegration_grant_inflight_cancel.json"} {
		t.Run(name, func(t *testing.T) {
			b := testsourcefacts.Load(t, name)
			source := testsourcefacts.RegisterSource(t, f.admin)
			testsourcefacts.Replay(t, f.admin, f.dsn, source, b, "1")
			f.exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_source_context,allow_source_identity,allow_source_cost) VALUES($1,$2::uuid,true,true,true,true,true)`, f.subject, source)
			req := f.request()
			req.Filters.SourceIDs = []string{source}
			before, e := f.workspace.CreateWorkbenchReading(f.ctx, f.subject, req, time.Now())
			if e != nil {
				t.Fatal(e)
			}
			observations, e := f.workspace.GetWorkbenchSourceFacts(f.ctx, f.subject, before.ID, 100, "", time.Now())
			if e != nil {
				t.Fatal(e)
			}
			if len(observations.Items) == 0 || int64(len(observations.Items)) != before.SourceCoverage.KnownDocuments {
				t.Fatal("retained source missing")
			}
			testsourcefacts.PersistMoney(t, f.ctx, f.dsn, source, b)
			after, e := f.workspace.CreateWorkbenchReading(f.ctx, f.subject, req, time.Now())
			if e != nil {
				t.Fatal(e)
			}
			summary, e := f.workspace.GetWorkbenchSummary(f.ctx, f.subject, after.ID, time.Now())
			if e != nil || summary.RequestCount != int64(len(b.I01)) {
				t.Fatal("source stage added calls", e)
			}
			if len(b.I01) == 0 && summary.SelectedAmountMicros != "0" {
				t.Fatal("media added money")
			}
		})
	}
	if e := db.ValidateWorkbenchRestore(f.ctx, f.admin); e != nil {
		t.Fatal(e)
	}
}

func TestSourceFactsConcurrentGenerationConflictAndAtomicPage(t *testing.T) {
	f := newSourceFixture(t)
	p := sourcefacts.IdentityProfile
	d := f.documents[p]
	d.Revision = "2"
	d.PreviousRevision = "1"
	d.PreviousHash = f.hash(p)
	d.CorrectionReason = "first correction"
	f.setPage(p, "candidate-a", d)
	pageA := f.pages[p]
	d.CorrectionReason = "different same revision"
	f.setPage(p, "candidate-b", d)
	pageB := f.pages[p]
	streams := []sourcefacts.Stream{}
	for _, gen := range []string{"2", "3"} {
		id := testUUID(t)
		f.exec(`INSERT INTO source_fact_streams(id,source_system_id,source_instance,org_id,project_id,profile,scope_generation,status,cursor_value) VALUES($1::uuid,$2::uuid,'ur-demo-01','17','101',$3,$4,'active','before')`, id, f.source, p, gen)
		s, e := f.consumerStore.LoadSourceFactsStream(f.ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		streams = append(streams, s)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i, page := range []sourcefacts.Page{pageA, pageB} {
		go func(s sourcefacts.Stream, p sourcefacts.Page) {
			<-start
			results <- f.consumerStore.CommitSourceFactsPage(f.ctx, s, p, time.Now())
		}(streams[i], page)
	}
	close(start)
	a, b := <-results, <-results
	if !((a == nil && errors.Is(b, sourcefacts.ErrConflict)) || (b == nil && errors.Is(a, sourcefacts.ErrConflict))) {
		t.Fatalf("concurrent identity conflict %v / %v", a, b)
	}
	var count int
	if e := f.admin.QueryRow(`SELECT count(*) FROM source_fact_documents WHERE source_system_id=$1::uuid AND profile=$2 AND revision=2`, f.source, p).Scan(&count); e != nil || count != 1 {
		t.Fatal("two immutable candidates survived", e)
	}
	// A valid first document followed by a broken second document cannot leave
	// a partial page or advance the durable consumer position.
	ctxDoc := f.documents[sourcefacts.ContextProfile]
	ctxDoc.RequestID = "partial-page"
	ctxDoc.Core = copySourceMap(ctxDoc.Core)
	ctxDoc.Core["request_id"] = ctxDoc.RequestID
	f.setPage(ctxDoc.Profile, "partial-must-not-advance", ctxDoc)
	f.mu.Lock()
	bad := f.pages[ctxDoc.Profile]
	bad.Events = append(bad.Events, json.RawMessage(`{}`))
	f.pages[ctxDoc.Profile] = bad
	f.mu.Unlock()
	before, _ := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[ctxDoc.Profile])
	if _, e := f.consumer.PullOnce(f.ctx, f.streams[ctxDoc.Profile]); !errors.Is(e, sourcefacts.ErrInvalid) {
		t.Fatal("partial invalid page accepted", e)
	}
	after, _ := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[ctxDoc.Profile])
	if after.Cursor != before.Cursor {
		t.Fatal("partial page cursor advanced")
	}
	if e := f.admin.QueryRow(`SELECT count(*) FROM source_fact_documents WHERE request_id='partial-page'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial page inserted bytes", e)
	}
	// Upper int64 revision remains a valid durable pending document.
	d = f.documents[sourcefacts.CostProfile]
	d.Revision = "9223372036854775807"
	d.PreviousRevision = "9223372036854775806"
	d.PreviousHash = "sha256:" + strings.Repeat("a", 64)
	d.CorrectionReason = "retained missing predecessor"
	f.setPage(d.Profile, "max-revision", d)
	f.pull(d.Profile)
}

func TestSourceFactsManifestRejectsStaleSnapshotAppend(t *testing.T) {
	f := newSourceFixture(t)
	// A header already exists before either append/finalize transaction.
	r := f.reading()
	tx, e := f.admin.BeginTx(f.ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRowContext(f.ctx, `SELECT count(*) FROM workbench_source_manifests`).Scan(&n); e != nil {
		t.Fatal(e)
	}
	// The payload is deliberately irrelevant to this direct-writer guard test;
	// restore validation separately rejects an A0 header carrying any manifest.
	f.exec(`INSERT INTO workbench_source_manifests(reading_id,rule_version,authorization_sha256,coverage,manifest_sha256) VALUES($1,'xinsight.source-reading.v1','sha256:'||repeat('a',64),'{}','sha256:'||repeat('b',64))`, r.ID)
	_, e = tx.ExecContext(f.ctx, `INSERT INTO workbench_source_members(reading_id,source_system_id,event_id,chain_state,economic_state) SELECT $1,source_system_id,event_id,'complete','not_received' FROM source_fact_documents LIMIT 1`, r.ID)
	if e == nil {
		t.Fatal("a repeatable-read writer appended after manifest finalization")
	}
	tx.Rollback()
	if _, e = f.admin.Exec(`INSERT INTO workbench_source_members(reading_id,source_system_id,event_id,chain_state,economic_state) SELECT $1,source_system_id,event_id,'complete','not_received' FROM source_fact_documents LIMIT 1`, r.ID); e == nil {
		t.Fatal("current snapshot appended after manifest")
	}
}

func TestSourceFactsOriginalR1ThroughR5Amounts(t *testing.T) {
	f := newSourceFixture(t)
	raw, e := os.ReadFile("../sourcefacts/contract/golden-pages.json")
	if e != nil {
		t.Fatal(e)
	}
	var original struct {
		Valid []json.RawMessage
		I01   []json.RawMessage
	}
	if e = json.Unmarshal(raw, &original); e != nil {
		t.Fatal(e)
	}
	var total int64
	for i, economic := range original.I01 {
		label := "original_R" + strconv.Itoa(i+1)
		for _, page := range original.Valid {
			var name struct{ Name string }
			json.Unmarshal(page, &name)
			if name.Name != label {
				continue
			}
			raw, e = json.Marshal(map[string]any{"pages": []json.RawMessage{page}, "i01": []json.RawMessage{economic}})
			if e != nil {
				t.Fatal(e)
			}
			var b testsourcefacts.Bundle
			if e = json.Unmarshal(raw, &b); e != nil {
				t.Fatal(e)
			}
			source := testsourcefacts.RegisterSource(t, f.admin)
			f.exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_source_cost) VALUES($1,$2::uuid,true,true,true)`, f.subject, source)
			testsourcefacts.Replay(t, f.admin, f.dsn, source, b, "1")
			testsourcefacts.PersistMoney(t, f.ctx, f.dsn, source, b)
			testsourcefacts.Replay(t, f.admin, f.dsn, source, b, "2")
			req := f.request()
			req.Filters.SourceIDs = []string{source}
			r, e := f.workspace.CreateWorkbenchReading(f.ctx, f.subject, req, time.Now())
			if e != nil {
				t.Fatal(e)
			}
			s, e := f.workspace.GetWorkbenchSummary(f.ctx, f.subject, r.ID, time.Now())
			if e != nil {
				t.Fatal(e)
			}
			if s.RequestCount != 1 || s.GatewayChargedMicros != "0" || s.SourceCoverage.KnownDocuments != 1 || s.SourceCoverage.EconomicNotReceived != 0 || s.SourceCoverage.EconomicHashConflicts != 0 {
				t.Fatalf("%s association: %+v", label, s)
			}
			n, e := strconv.ParseInt(s.CalculatedReferenceMicros, 10, 64)
			if e != nil {
				t.Fatal(e)
			}
			total += n
		}
	}
	if total != 105000000 {
		t.Fatalf("original five examples changed: %d", total)
	}
}

// Shared fixture document loading does not open or mutate a database.
func loadSourceFixtureDocuments(t *testing.T, f *sourceFixture) {
	t.Helper()
	var bundle struct {
		Valid []struct {
			Name string
			Page sourcefacts.Page
		}
		I01 []struct {
			Label string
			Wire  string `json:"snapshot_wire"`
		}
	}
	raw, err := os.ReadFile("../sourcefacts/contract/golden-pages.json")
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &bundle) != nil {
		t.Fatal("fixture JSON")
	}
	for _, v := range bundle.I01 {
		if v.Label == "R1" {
			f.i01 = []byte(v.Wire)
		}
	}
	for _, v := range bundle.Valid {
		if v.Name == "original_R1" {
			item, err := sourcefacts.ValidateItem(v.Page.Events[0], sourcefacts.CostProfile)
			if err != nil {
				t.Fatal(err)
			}
			f.documents[sourcefacts.CostProfile] = item.Document
		}
	}
	for _, v := range bundle.Valid {
		if v.Name == "context" || v.Name == "identity" {
			p := sourcefacts.ContextProfile
			if v.Name == "identity" {
				p = sourcefacts.IdentityProfile
			}
			item, err := sourcefacts.ValidateItem(v.Page.Events[0], p)
			if err != nil {
				t.Fatal(err)
			}
			d := item.Document
			cost := f.documents[sourcefacts.CostProfile]
			d.SourceInstance, d.OrgID, d.ProjectID, d.RequestID, d.OccurredAt, d.RecordedAt = cost.SourceInstance, cost.OrgID, cost.ProjectID, cost.RequestID, cost.OccurredAt, cost.RecordedAt
			if d.Core != nil {
				d.Core["org_id"] = d.OrgID
				d.Core["project_id"] = d.ProjectID
				d.Core["request_id"] = d.RequestID
				d.Core["workload_application_id"] = "9007199254740993"
				d.Core["captured_at"] = d.OccurredAt
			}
			if d.Identity != nil {
				d.Identity["owner_user_id"] = "9007199254740995"
			}
			f.documents[p] = d
		}
	}
}

// These older consumer fixtures explicitly exercise automatic collection with
// a verified synthetic connection; production no longer accepts unbound live I/O.
func (f *sourceFixture) bindSyncScope(source, org, project string) {
	f.t.Helper()
	f.bindSyncScopeWithAuto(source, org, project, true)
}

func (f *sourceFixture) bindSyncScopeWithAuto(source, org, project string, automatic bool) {
	f.t.Helper()
	var bound bool
	if e := f.admin.QueryRowContext(f.ctx, `SELECT EXISTS(SELECT 1 FROM public.xinsight_sync_scope($1,$2,$3,false))`, source, org, project).Scan(&bound); e != nil {
		f.t.Fatal(e)
	}
	if bound {
		return
	}
	suffix := strings.ReplaceAll(testUUID(f.t), "-", "")[:22]
	id := "xci_" + suffix
	identity := "xsi_" + suffix
	f.exec(`INSERT INTO service_identities(id,display_name,status,permissions,created_by_subject,created_at,updated_at)VALUES($1,'synthetic sync fixture','active',ARRAY['connector.control.poll','connector.control.ack'],'system',clock_timestamp(),clock_timestamp())`, identity)
	f.exec(`INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,final_event_scope,project_id,source_instance,source_instance_sha256,control_service_identity_id,installation_acknowledgement_sha256,status,desired_state,observed_state,created_at,updated_at)VALUES($1,'xid_0000000000000000000000','unirouter',ARRAY['model.request.completed'],ARRAY['model.request.completed'],$2,$3,'sha256:'||repeat('a',64),$4,'sha256:'||repeat('b',64),'connected','active','connected',clock_timestamp(),clock_timestamp())`, id, project, source, identity)
	f.exec(`INSERT INTO connector_installation_source_bindings(installation_id,source_binding_sha256)VALUES($1,'sha256:'||repeat('c',64))`, id)
	f.exec(`INSERT INTO connector_source_enrollments(installation_id,enrollment_id,source_instance,org_id,project_id,revision,status,updated_at)VALUES($1,'xise_1',$2,$3,$4,'1','active',clock_timestamp())`, id, source, org, project)
	if automatic {
		f.exec(`UPDATE connector_sync_controls SET auto_sync=true WHERE installation_id=$1`, id)
	}
}
