package api_test

import (
	"bytes"
	"encoding/json"
	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"net/url"
	"os"
	"testing"
	"time"
)

func seedCapturedRequestRuntimeObservation(t *testing.T, f *scopedContentFixture) {
	t.Helper()
	raw, e := os.ReadFile("../sourcefacts/contract/observations/golden-observations.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixture struct {
		Valid []struct {
			Canonical string `json:"canonical_utf8"`
		}
	}
	if e = json.Unmarshal(raw, &fixture); e != nil {
		t.Fatal(e)
	}
	for _, v := range fixture.Valid {
		d, e := sourcefacts.DecodeObservation([]byte(v.Canonical))
		if e != nil {
			t.Fatal(e)
		}
		if d.Profile != sourcefacts.RuntimeProfile || d.Kind != "request_started" {
			continue
		}
		d.SourceInstance, d.OrgID, d.ProjectID, d.RequestID = f.collection.SourceInstance, f.collection.OrgID, f.collection.ProjectID, f.choice.Reference.RequestID
		d.SubjectID = d.RequestID
		d.OccurredAt = f.base.Add(-time.Minute).Format(sourcefacts.TimeFormat)
		d.RecordedAt = d.OccurredAt
		d.SourceFactID = sourcefacts.SourceID(d.SchemaVersion, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.SubjectKind, d.SubjectID, d.Kind)
		d.EventID = sourcefacts.SourceID(d.SchemaVersion+".revision", d.SourceFactID, d.Revision)
		envelope, e := sourcefacts.ObservationEnvelope(scopedContentCanonical(t, d))
		if e != nil {
			t.Fatal(e)
		}
		stream, e := f.consumer.RegisterSourceFactsStream(f.ctx, sourcefacts.Stream{ID: randomTestUUID(t), SourceSystemID: f.sourceID, SourceInstance: d.SourceInstance, OrgID: d.OrgID, ProjectID: d.ProjectID, Profile: d.Profile, ScopeGeneration: "1"})
		if e != nil {
			t.Fatal(e)
		}
		stream, e = f.consumer.ControlSourceFactsStream(f.ctx, stream.ID, "active", stream.Revision, false)
		if e != nil {
			t.Fatal(e)
		}
		importer, e := db.OpenSourceFactsStore(f.importerDSN)
		if e != nil {
			t.Fatal(e)
		}
		defer importer.Close()
		page := sourcefacts.Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: sourcefacts.ConnectorID, ConnectorVersion: sourcefacts.ObservationConnectorVersion, ProducerInstance: d.SourceInstance, GeneratedAt: d.RecordedAt, Events: []json.RawMessage{}, NextCursor: "runtime-bootstrap", HighWatermark: "runtime-bootstrap"}
		if e = importer.CommitSourceFactsPage(f.ctx, stream, page, f.base); e != nil {
			t.Fatal(e)
		}
		stream, e = importer.LoadSourceFactsStream(f.ctx, stream.ID)
		if e != nil {
			t.Fatal(e)
		}
		page.Events = []json.RawMessage{envelope}
		page.NextCursor = "runtime-one"
		page.HighWatermark = "runtime-one"
		if e = importer.CommitSourceFactsPage(f.ctx, stream, page, f.base); e != nil {
			t.Fatal(e)
		}
		return
	}
	t.Fatal("canonical request_started vector not found")
}

// The exact Runtime byte endpoint is exercised on the same captured object,
// same reader identity and a fresh reading owned by that metadata-only reader.
// The prior Work Activity body test remains a separate endpoint proof.
func TestRuntimeMetadataReaderOwnReadingRejectsKnownCapturedBodyPostgres(t *testing.T) {
	body := []byte(`{"value":1,"other":2}`)
	f := newScopedContentFixtureWithCustody(t, body, reviewbody.KeyRing{}, access.RoleScopedReader)
	seedCapturedRequestRuntimeObservation(t, f)
	if _, e := f.admin.ExecContext(f.ctx, `UPDATE workbench_access_grants SET allow_source_runtime=true,allow_coverage=true WHERE subject_id=$1`, scopedContentSubject); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	f.apiOffset = time.Minute
	f.mu.Unlock()
	refresh := func() {
		t.Helper()
		var s struct {
			Scope   string             `json:"scope_revision"`
			Session access.SessionView `json:"session"`
		}
		if e := json.Unmarshal(f.must(t, "GET", "/api/v1/access/session", nil, 200, nil), &s); e != nil || s.Scope == "" || s.Session.SubjectID != scopedContentSubject || s.Session.Role != access.RoleScopedReader {
			t.Fatal("wrong reader identity/scope", e)
		}
		f.scope = s.Scope
	}
	refresh()
	input := workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: workbench.PeriodInput{StartInclusive: func() *time.Time { v := f.base.Add(-time.Hour); return &v }(), EndExclusive: func() *time.Time { v := f.base.Add(time.Minute); return &v }(), DisplayTimezone: "UTC"}, Filters: workbench.Filters{SourceIDs: []string{f.sourceID}, SourceProjects: []string{"project-" + f.collection.ProjectID}, CostProjects: []string{}}}
	create := func() (workbench.RuntimeReading, workbench.RuntimeRequest) {
		t.Helper()
		var r workbench.RuntimeReading
		if e := json.Unmarshal(f.must(t, "POST", "/api/v1/runtime/readings", input, 201, nil), &r); e != nil || r.ObservationCount != 1 {
			t.Fatal("actual runtime observation missing", e)
		}
		var page workbench.RuntimeRequests
		if e := json.Unmarshal(f.must(t, "GET", "/api/v1/runtime/readings/"+r.ID+"/requests?entry_time=all", nil, 200, nil), &page); e != nil || len(page.Items) != 1 || page.Items[0].RequestID != f.choice.Reference.RequestID || page.Items[0].ObservationCount != 1 {
			t.Fatal("captured request not in own runtime cohort", e)
		}
		return r, page.Items[0]
	}
	positive, request := create()
	pathFor := func(r workbench.RuntimeReading, q workbench.RuntimeRequest) string {
		return "/api/v1/runtime/readings/" + r.ID + "/requests/" + q.Ref + "/contents/" + url.PathEscape(f.choice.Reference.ContentRef) + "?source_fact_id=" + url.QueryEscape(f.choice.Reference.SourceFactID) + "&revision=" + f.choice.Reference.Revision
	}
	f.resetSource()
	actual := f.must(t, "GET", pathFor(positive, request), nil, 200, nil)
	if !bytes.Equal(actual, body) || sourcefacts.Hash(actual) != f.choice.Reference.Representation.Digest {
		t.Fatal("positive captured body bytes not verified")
	}
	// The complete native preflight/binding/publication path checks capabilities four times.
	f.assertSource(t, 4, 1)
	if _, e := f.admin.ExecContext(f.ctx, `UPDATE workbench_access_grants SET allow_content=false,allowed_content_purposes='[]'::jsonb,allow_summary=false,allow_details=true,allow_coverage=true,allow_source_runtime=true WHERE subject_id=$1`, scopedContentSubject); e != nil {
		t.Fatal(e)
	}
	refresh()
	metadata, member := create()
	if metadata.ID == positive.ID || member.Ref != request.Ref {
		t.Fatal("negative test must use new owned reading and same request")
	}
	f.must(t, "GET", "/api/v1/runtime/readings/"+metadata.ID, nil, 200, nil)
	f.resetSource()
	status, denied, e := f.do("GET", pathFor(metadata, member), nil, nil)
	if e != nil || status != 404 || bytes.Contains(denied, body) {
		t.Fatalf("exact metadata-reader Runtime byte refusal status%d error%v", status, e)
	}
	f.assertSource(t, 0, 0)
	f.must(t, "GET", "/api/v1/runtime/readings/"+metadata.ID+"/summary?entry_time=all", nil, 200, nil)
	f.must(t, "GET", "/api/v1/runtime/readings/"+metadata.ID+"/requests?entry_time=all", nil, 200, nil)
	f.assertSource(t, 0, 0)
	t.Log("Same known captured non-sensitive body: authorized Runtime byte read matches digest; scoped-reader metadata-only new owned reading includes same observed request; exact Runtime body404 releases no body and performs zero source reads; own Runtime metadata remains readable. No production capture/grant changed.")
}
