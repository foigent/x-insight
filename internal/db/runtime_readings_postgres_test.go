package db_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func runtimeGolden(t *testing.T) []sourcefacts.Observation {
	t.Helper()
	raw, err := os.ReadFile("../sourcefacts/contract/observations/golden-observations.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Valid []struct {
			Canonical string `json:"canonical_utf8"`
		}
	}
	if err = json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	var result []sourcefacts.Observation
	for _, row := range v.Valid {
		d, err := sourcefacts.DecodeObservation([]byte(row.Canonical))
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, d)
	}
	return result
}

func runtimeEnvelope(t *testing.T, d sourcefacts.Observation) json.RawMessage {
	t.Helper()
	d.SourceFactID = sourcefacts.SourceID(sourcefacts.ObservationVersion, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.SubjectKind, d.SubjectID, d.Kind)
	d.EventID = sourcefacts.SourceID(sourcefacts.ObservationVersion+".revision", d.SourceFactID, d.Revision)
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	value, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = connector.CanonicalizeJSONValue(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sourcefacts.DecodeObservation(raw); err != nil {
		t.Fatalf("decode fixture %s/%s revision=%s: %v", d.RequestID, d.Kind, d.Revision, err)
	}
	envelope, err := sourcefacts.ObservationEnvelope(raw)
	if err != nil {
		t.Fatalf("build fixture envelope %s/%s revision=%s: %v", d.RequestID, d.Kind, d.Revision, err)
	}
	return envelope
}

func putRuntimeDocuments(t *testing.T, f *sourceFixture, source string, documents ...sourcefacts.Observation) {
	t.Helper()
	groups := map[string][]sourcefacts.Observation{}
	for _, d := range documents {
		key := d.SourceInstance + ":" + d.Profile + ":" + d.OrgID + ":" + d.ProjectID
		groups[key] = append(groups[key], d)
	}
	for key, group := range groups {
		d := group[0]
		key = source + ":" + key
		streamID := f.streams[key]
		var stream sourcefacts.Stream
		var err error
		if streamID == "" {
			stream, err = f.consumerStore.RegisterSourceFactsStream(f.ctx, sourcefacts.Stream{ID: testUUID(t), SourceSystemID: source, SourceInstance: d.SourceInstance, OrgID: d.OrgID, ProjectID: d.ProjectID, Profile: d.Profile, ScopeGeneration: "1"})
			if err != nil {
				t.Fatal(err)
			}
			stream, err = f.consumerStore.ControlSourceFactsStream(f.ctx, stream.ID, "active", stream.Revision, false)
			if err != nil {
				t.Fatal(err)
			}
			page := sourcefacts.Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: sourcefacts.ConnectorID, ConnectorVersion: sourcefacts.ObservationConnectorVersion, ProducerInstance: d.SourceInstance, GeneratedAt: time.Now().UTC().Format(sourcefacts.TimeFormat), Events: []json.RawMessage{}, NextCursor: "start", HighWatermark: "start"}
			if err = f.consumerStore.CommitSourceFactsPage(f.ctx, stream, page, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			f.streams[key] = stream.ID
			streamID = stream.ID
		}
		stream, err = f.consumerStore.LoadSourceFactsStream(f.ctx, streamID)
		if err != nil {
			t.Fatal(err)
		}
		events := []json.RawMessage{}
		for _, doc := range group {
			events = append(events, runtimeEnvelope(t, doc))
		}
		page := sourcefacts.Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: sourcefacts.ConnectorID, ConnectorVersion: sourcefacts.ObservationConnectorVersion, ProducerInstance: d.SourceInstance, GeneratedAt: time.Now().UTC().Format(sourcefacts.TimeFormat), Events: events, NextCursor: stream.Cursor + "x", HighWatermark: stream.Cursor + "x"}
		if err = f.consumerStore.CommitSourceFactsPage(f.ctx, stream, page, time.Now().UTC()); err != nil {
			t.Fatalf("commit fixture page profile=%s events=%d: %v", d.Profile, len(events), err)
		}
	}
}

func runtimeRequestInput(t *testing.T, d sourcefacts.Observation) workbench.RuntimeReadingRequest {
	t.Helper()
	start, err := time.Parse(time.RFC3339Nano, d.OccurredAt)
	if err != nil {
		t.Fatal(err)
	}
	start = start.Add(-time.Hour)
	end := start.Add(24 * time.Hour)
	return workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: workbench.PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "Asia/Shanghai"}, Filters: workbench.Filters{}}
}

func TestRuntimeReadingIndependentMembershipAndRevocation(t *testing.T) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	known := gold[0]
	known.ContextRef = "uf1_" + strings.Repeat("c", 64)
	unknown := gold[9]
	unknown.RequestID = "request-unknown-entry"
	unknown.SubjectID = unknown.RequestID
	hidden := gold[0]
	hidden.ProjectID = "999"
	hidden.RequestID = "hidden-other-project"
	hidden.SubjectID = hidden.RequestID
	putRuntimeDocuments(t, f, f.source, known, gold[1], gold[4], unknown, hidden)
	now := time.Now().UTC().Add(time.Minute)
	input := runtimeRequestInput(t, known)
	if _, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatal("default A grants admitted B", err)
	}
	f.exec(`UPDATE workbench_access_grants SET source_project_ref='project-3',allow_source_runtime=true,allow_source_attempt=true WHERE subject_id=$1`, f.subject)
	r, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.RequestsWithEntryTime != 1 || r.RequestsWithUnknownEntryTime != 1 || r.ObservationCount != 3 {
		t.Fatalf("wrong independent denominator: %+v", r)
	}
	page, err := f.workspace.GetRuntimeRequests(f.ctx, f.subject, r.ID, "all", 1, "", now)
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatal(page, err)
	}
	second, err := f.workspace.GetRuntimeRequests(f.ctx, f.subject, r.ID, "all", 1, page.NextCursor, now)
	if err != nil || len(second.Items) != 1 || second.Items[0].Ref == page.Items[0].Ref {
		t.Fatal(second, err)
	}
	if _, err = f.workspace.GetRuntimeRequests(f.ctx, f.subject, r.ID, "unknown", 1, page.NextCursor, now); !errors.Is(err, workbench.ErrInvalid) {
		t.Fatal("cross-collection cursor accepted", err)
	}
	observations, err := f.workspace.GetRuntimeObservations(f.ctx, f.subject, r.ID, "", "", 100, "", now)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(observations)
	if strings.Contains(string(encoded), known.ContextRef) || strings.Contains(string(encoded), "hidden-other-project") || strings.Contains(string(encoded), sourcefacts.DestinationProfile) {
		t.Fatal("field/scope disclosure")
	}
	if len(observations.Items) != 3 {
		t.Fatal("wrong visible observations", len(observations.Items))
	}
	putRuntimeDocuments(t, f, f.source, gold[9])
	old, err := f.workspace.GetRuntimeReading(f.ctx, f.subject, r.ID, now)
	if err != nil || old.MembershipRef != r.MembershipRef || old.ObservationCount != 3 {
		t.Fatal("late receipt reinterpreted old reading", old, err)
	}
	newReading, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil || newReading.ObservationCount != 4 {
		t.Fatal("new reading missed late document", newReading, err)
	}
	putRuntimeDocuments(t, f, f.source, gold[9])
	again, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil || again.ObservationCount != 4 {
		t.Fatal("replay enlarged denominator", again, err)
	}
	var money int
	if err = f.admin.QueryRow(`SELECT count(*) FROM workbench_economic_facts`).Scan(&money); err != nil || money != 0 {
		t.Fatal("B created economic facts", money, err)
	}
	if err = db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE runtime_readings SET observation_count=observation_count+1 WHERE id=$1`, r.ID)
	if err = db.ValidateWorkbenchRestore(f.ctx, f.admin); err == nil {
		t.Fatal("corrupt reading accepted on restore")
	}
	f.exec(`UPDATE runtime_readings SET observation_count=observation_count-1 WHERE id=$1`, r.ID)
	if err = db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=false WHERE subject_id=$1`, f.subject)
	if _, err = f.workspace.GetRuntimeReading(f.ctx, f.subject, r.ID, now); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("old reading survived B revocation", err)
	}
	restricted, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil || restricted.RequestsWithEntryTime != 0 || restricted.RequestsWithUnknownEntryTime != 1 || restricted.ObservationCount != 1 {
		t.Fatal("hidden runtime leaked entry time", restricted, err)
	}
}

func TestRuntimeReadingRequestIdentityAndPeriod(t *testing.T) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	source2 := testUUID(t)
	f.exec(`INSERT INTO source_systems(id,source_key,source_type,display_name,source_version,status) VALUES($1,$2,'unirouter','Other runtime source','unirouter.completed-execution.v1','active')`, source2, "other:"+source2)
	f.exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_source_runtime) VALUES($1,$2,true,true,true)`, f.subject, source2)
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id=$1`, f.subject)
	putRuntimeDocuments(t, f, f.source, gold[0])
	putRuntimeDocuments(t, f, source2, gold[0])
	now := time.Now().UTC().Add(time.Minute)
	input := runtimeRequestInput(t, gold[0])
	r, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil || r.RequestsWithEntryTime != 2 {
		t.Fatal(r, err)
	}
	page, err := f.workspace.GetRuntimeRequests(f.ctx, f.subject, r.ID, "known", 100, "", now)
	if err != nil || len(page.Items) != 2 || page.Items[0].Ref == page.Items[1].Ref {
		t.Fatal("same request strings merged across sources", page, err)
	}
	if _, err = f.workspace.GetRuntimeReading(f.ctx, "xs_bbbbbbbbbbbbbbbb", r.ID, now); !errors.Is(err, workbench.ErrNotFound) {
		t.Fatal("foreign reading disclosed", err)
	}
	if _, err = f.workspace.GetRuntimeReading(f.ctx, f.subject, r.ID, r.ExpiresAt); !errors.Is(err, workbench.ErrExpired) {
		t.Fatal("expired reading accepted", err)
	}
	start, _ := time.Parse(time.RFC3339Nano, gold[0].OccurredAt)
	end := start
	start = start.Add(-time.Hour)
	input.Period.StartInclusive = &start
	input.Period.EndExclusive = &end
	empty, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil || empty.RequestsWithEntryTime != 0 || empty.RequestsWithUnknownEntryTime != 0 {
		t.Fatal("exclusive period end included request", empty, err)
	}
	input.Filters.CostProjects = []string{"unbound-project"}
	unbound, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil || unbound.ObservationCount != 0 {
		t.Fatal("cost constraint matched unpriced request", unbound, err)
	}
}

func TestRuntimeReadingCanonicalTimesRoundTrip(t *testing.T) {
	f := newSourceFixture(t)
	d := runtimeGolden(t)[0]
	now := time.Now().UTC().Add(time.Minute).Truncate(time.Second).Add(123456789 * time.Nanosecond)
	d.OccurredAt = now.Add(-time.Hour).Truncate(time.Second).Format(sourcefacts.TimeFormat)
	d.RecordedAt = d.OccurredAt
	putRuntimeDocuments(t, f, f.source, d)
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id=$1`, f.subject)
	start := now.Add(-2 * time.Hour).In(time.FixedZone("source-offset", 8*3600))
	end := now.In(time.FixedZone("source-offset", 8*3600))
	input := workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: workbench.PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "Asia/Shanghai"}}
	created, e := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if e != nil {
		t.Fatal(e)
	}
	loaded, e := f.workspace.GetRuntimeReading(f.ctx, f.subject, created.ID, now)
	if e != nil {
		t.Fatal(e)
	}
	timeView := func(r workbench.RuntimeReading) string {
		raw, e := json.Marshal(struct {
			Period  workbench.Period `json:"period"`
			Cutoff  time.Time        `json:"cutoff"`
			Expires time.Time        `json:"expires_at"`
		}{r.Period, r.Cutoff, r.ExpiresAt})
		if e != nil {
			t.Fatal(e)
		}
		return string(raw)
	}
	if timeView(created) != timeView(loaded) {
		t.Fatalf("POST/GET reading timestamps changed: created=%s loaded=%s", timeView(created), timeView(loaded))
	}
	if created.Cutoff.Nanosecond()%1000 != 0 || created.RequestsWithEntryTime != 1 {
		t.Fatal("unexpected stored precision or membership", created)
	}
}
