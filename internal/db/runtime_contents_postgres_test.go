package db_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

// This fixture is compiled but not run in X1's pure-Go local verification.
func TestRuntimeContentPostgresFreezeDomainsAndZeroObservationGuard(t *testing.T) {
	f := newSourceFixture(t)
	economic := f.documents[sourcefacts.ContextProfile]
	b := runtimeGolden(t)[0]
	b.SourceInstance, b.OrgID, b.ProjectID = economic.SourceInstance, economic.OrgID, economic.ProjectID
	b.RequestID, b.SubjectID = "b-content-intersection", "b-content-intersection"
	b.OccurredAt, b.RecordedAt = economic.OccurredAt, economic.RecordedAt
	putRuntimeDocuments(t, f, f.source, b)
	f.money()
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id=$1`, f.subject)
	now := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	input := runtimeRequestInput(t, b)
	old, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil {
		t.Fatal(err)
	}
	c := sourcefacts.ContentCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: b.SourceInstance, OrgID: b.OrgID, ProjectID: b.ProjectID, CredentialID: "runtime-content", RegistrationHash: sourcefacts.Hash([]byte("runtime-content-registration")), ContentScopeRevision: "scope-one", RecoveryEpoch: "1", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: sourcefacts.ContentBoundary{Time: b.OccurredAt, AfterSequence: "0"}}
	c, err = f.consumerStore.RegisterContentCollection(f.ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	c, err = f.consumerStore.ControlContentCollection(f.ctx, c.ID, "active", c.Revision, false, c)
	if err != nil {
		t.Fatal(err)
	}
	canonical := func(v any) []byte {
		t.Helper()
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		obj, e := connector.DecodeStrictJSONObject(raw)
		if e != nil {
			t.Fatal(e)
		}
		raw, e = connector.CanonicalizeJSONValue(obj)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	manifestTime, _ := time.Parse(sourcefacts.TimeFormat, b.OccurredAt)
	manifestAt := manifestTime.Add(time.Hour).Format(sourcefacts.TimeFormat)
	manifest := func(request, letter string) sourcefacts.ContentManifest {
		d := sourcefacts.ContentManifest{SchemaVersion: sourcefacts.ContentManifestVersion, SourceInstance: b.SourceInstance, OrgID: b.OrgID, ProjectID: b.ProjectID, RequestID: request, Profile: sourcefacts.ContentProfile, Revision: "1", OccurredAt: manifestAt, RecordedAt: manifestAt, Protocol: "openai.responses", ObservationPoint: "client_request", ContentItemID: "body", Capture: sourcefacts.ContentCapture{Mode: "full_content", PolicyRef: "runtime-policy", PolicyVersion: "1", PolicyEffectiveAt: manifestAt, Completeness: "complete", Reason: "none"}, ContentRef: "uc1_" + strings.Repeat(letter, 64), Representation: &sourcefacts.ContentRepresentation{MediaType: "application/octet-stream", Encoding: "identity", ByteDomain: "stored_representation_before_http_content_encoding", SavedBytes: 0, Digest: sourcefacts.Hash(nil)}, CapturedAt: manifestAt, ExpiresAt: now.Add(time.Minute).Format(sourcefacts.TimeFormat)}
		d.SourceFactID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.ObservationPoint, d.AttemptID, d.ContentItemID)
		d.EventID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion+".revision", d.SourceFactID, d.Revision)
		return d
	}
	one, two := manifest(b.RequestID, "a"), manifest("content-only", "b")
	entries := []sourcefacts.ContentManifestEntry{{Sequence: "1", Document: one, SourceSHA256: sourcefacts.Hash(canonical(one)), Availability: sourcefacts.ContentAvailability{State: "available", EvaluatedAt: b.OccurredAt}}, {Sequence: "2", Document: two, SourceSHA256: sourcefacts.Hash(canonical(two)), Availability: sourcefacts.ContentAvailability{State: "available", EvaluatedAt: b.OccurredAt}}}
	page := sourcefacts.ContentManifestPage{SchemaVersion: sourcefacts.ContentManifestPageVersion, Profile: sourcefacts.ContentProfile, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, GeneratedAt: now.Format(sourcefacts.TimeFormat), ContentScopeRevision: c.ContentScopeRevision, RecoveryEpoch: c.RecoveryEpoch, CoverageFrom: c.CoverageFrom, RetentionFloor: c.CoverageFrom, AfterSequence: "0", LastSequence: "2", HighWatermarkSequence: "2", NextCursor: "next-two", HighWatermark: "high-two", Coverage: "bounded", Gaps: []sourcefacts.ContentGap{}, Items: entries}
	if err = f.consumerStore.CommitContentManifests(f.ctx, c, canonical(page), now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_content=true,allowed_content_purposes='["human_review"]' WHERE subject_id=$1`, f.subject)
	// Adding C to the old grant cannot invalidate the existing B-only reading.
	unchanged, err := f.workspace.GetRuntimeReading(f.ctx, f.subject, old.ID, now)
	if err != nil || unchanged.MembershipRef != old.MembershipRef || unchanged.AuthorizationRevision != old.AuthorizationRevision || unchanged.InterpretationRevision != old.InterpretationRevision {
		t.Fatal("old B changed", err)
	}
	r, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.ContentInterpretationRevision != workbench.ContentInterpretationRevision || r.ContentAuthorizationRevision == "" || r.RequestsWithEntryTime != 1 || r.RequestsWithUnknownEntryTime != 0 || r.ObservationCount != 1 {
		t.Fatalf("wrong mixed reading %+v", r)
	}
	requests, err := f.workspace.GetRuntimeRequests(f.ctx, f.subject, r.ID, "all", 100, "", now)
	if err != nil || len(requests.Items) != 3 {
		t.Fatalf("three independent requests %+v %v", requests, err)
	}
	refs := map[string]string{}
	for _, q := range requests.Items {
		refs[q.RequestID] = q.Ref
		if q.RequestID != b.RequestID && (q.ObservationCount != 0 || q.EntryAt != nil || !q.LastObservedAt.IsZero() || q.ContentEligibleAt == nil) {
			t.Fatal("content fabricated B time", q)
		}
	}
	summary, err := f.workspace.GetRuntimeSummary(f.ctx, f.subject, r.ID, "all", now)
	if err != nil || summary.Requests != 1 {
		t.Fatal("content widened summary denominator", summary, err)
	}
	dimensions, err := f.workspace.GetRuntimeDimensions(f.ctx, f.subject, r.ID, "operation", "all", 100, "", now)
	if err != nil || dimensions.Requests != 1 {
		t.Fatal("content widened dimension denominator", dimensions, err)
	}
	econ, err := f.workspace.GetRuntimeContents(f.ctx, f.subject, r.ID, refs[economic.RequestID], 100, "", now)
	if err != nil || econ.MemberBasis != "exact_execution_association" || len(econ.Items) != 0 {
		t.Fatal("I01 empty manifest qualification absent", econ, err)
	}
	contents, err := f.workspace.GetRuntimeContents(f.ctx, f.subject, r.ID, refs["content-only"], 100, "", now)
	if err != nil || len(contents.Items) != 1 {
		t.Fatal(contents, err)
	}
	selected := contents.Items[0].Reference
	observation := contents.Items[0].Observation
	if observation == nil || observation.Protocol != two.Protocol || observation.ObservationPoint != two.ObservationPoint || observation.ContentItemID != two.ContentItemID || observation.AttemptID != "" || observation.OccurredAt != two.OccurredAt || observation.CaptureReason != "none" {
		t.Fatal("wrong frozen observation", observation)
	}
	admitted, err := f.workspace.ResolveRuntimeContent(f.ctx, f.subject, r.ID, refs["content-only"], sourcefacts.FrozenManifestRef{SourceFactID: selected.SourceFactID, Revision: selected.Revision, ContentRef: selected.ContentRef}, now)
	if err != nil || admitted.Reference.SourceSHA256 != selected.SourceSHA256 || admitted.Collection.ID != c.ID {
		t.Fatal("short admission failed", admitted, err)
	}
	if _, err = f.workspace.ResolveRuntimeContent(f.ctx, f.subject, r.ID, refs["content-only"], selected, now.Add(2*time.Minute)); !errors.Is(err, workbench.ErrContentExpired) {
		t.Fatal("body expiry confused with reading expiry", err)
	}
	if _, err = f.workspace.ResolveRuntimeContent(f.ctx, f.subject, r.ID, refs["content-only"], selected, now.Add(workbench.ReadingTTL+time.Second)); !errors.Is(err, workbench.ErrExpired) {
		t.Fatal("reading TTL lost", err)
	}
	// Reordering physical inserts must leave the fixed SQL tuple digest stable.
	tx, err := f.admin.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`CREATE TEMP TABLE content_refs_reorder AS SELECT * FROM runtime_reading_content_refs WHERE reading_id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`DELETE FROM runtime_reading_content_refs WHERE reading_id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO runtime_reading_content_refs SELECT * FROM content_refs_reorder ORDER BY source_fact_id DESC`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = f.workspace.GetRuntimeReading(f.ctx, f.subject, r.ID, now); err != nil {
		t.Fatal("reordering changed hash", err)
	}
	if _, err = f.admin.Exec(`DELETE FROM runtime_reading_content_refs WHERE reading_id=$1 AND request_ref=$2`, r.ID, refs["content-only"]); err == nil {
		t.Fatal("last qualifying manifest removed from zero-B request")
	}
	// A manifest-free I01-only reading has the nonempty content version too.
	contentSubject := "xs_3333333333333333"
	f.exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_content,allowed_content_purposes) VALUES($1,$2::uuid,$3,true,'["human_review"]')`, contentSubject, f.source, "project-"+b.ProjectID)
	pureInput := input
	pureStart, pureEnd := manifestTime.Add(-time.Minute), manifestTime.Add(time.Minute)
	pureInput.Period.StartInclusive, pureInput.Period.EndExclusive = &pureStart, &pureEnd
	pure, err := f.workspace.CreateRuntimeReading(f.ctx, contentSubject, pureInput, now)
	if err != nil || pure.ObservationCount != 0 || pure.RequestsWithUnknownEntryTime != 0 || pure.ContentInterpretationRevision == "" {
		t.Fatal("C-only grant denied or counted B", pure, err)
	}
	var pureRefCount int
	if err = f.admin.QueryRow(`SELECT count(*) FROM runtime_reading_content_refs WHERE reading_id=$1`, pure.ID).Scan(&pureRefCount); err != nil || pureRefCount != 0 {
		t.Fatal("I01-only reading unexpectedly captured manifests", pureRefCount, err)
	}
	// A newer manifest is reported separately and never mutates the frozen ref.
	next := two
	next.Revision = "2"
	next.Protocol = "openai.responses.v1"
	next.PreviousRevision = "1"
	next.PreviousHash = entries[1].SourceSHA256
	next.CorrectionReason = "corrected_capture"
	next.ContentRef = "uc1_" + strings.Repeat("c", 64)
	next.EventID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion+".revision", next.SourceFactID, next.Revision)
	current, e := f.consumerStore.LoadContentCollection(f.ctx, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	page.AfterSequence = "2"
	page.LastSequence = "3"
	page.HighWatermarkSequence = "3"
	page.NextCursor = "next-three"
	page.HighWatermark = "high-three"
	page.Items = []sourcefacts.ContentManifestEntry{{Sequence: "3", Document: next, SourceSHA256: sourcefacts.Hash(canonical(next)), Availability: sourcefacts.ContentAvailability{State: "available", EvaluatedAt: manifestAt}}}
	if err = f.consumerStore.CommitContentManifests(f.ctx, current, canonical(page), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	frozen, err := f.workspace.GetRuntimeContents(f.ctx, f.subject, r.ID, refs["content-only"], 100, "", now.Add(time.Second))
	if err != nil || len(frozen.Items) != 1 || frozen.Items[0].Reference.Revision != "1" || !frozen.Items[0].NewerRevision {
		t.Fatal("late revision rewrote frozen reference", frozen, err)
	}
	if frozen.Items[0].Observation == nil || *frozen.Items[0].Observation != *observation {
		t.Fatal("late revision changed frozen observation", frozen)
	}
	if reread, err := f.workspace.GetRuntimeReading(f.ctx, f.subject, r.ID, now.Add(time.Second)); err != nil || reread.MembershipRef != r.MembershipRef {
		t.Fatal("display metadata changed membership", err)
	}
	newer, err := f.workspace.CreateRuntimeReading(f.ctx, contentSubject, input, now.Add(2*time.Second))
	if err != nil || newer.ObservationCount != 0 {
		t.Fatal("C-only reading denied or expanded B", err)
	}
	newerRequests, err := f.workspace.GetRuntimeRequests(f.ctx, contentSubject, newer.ID, "all", 100, "", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var newerRef string
	for _, q := range newerRequests.Items {
		if q.RequestID == "content-only" {
			newerRef = q.Ref
		}
	}
	newerContents, err := f.workspace.GetRuntimeContents(f.ctx, contentSubject, newer.ID, newerRef, 100, "", now.Add(2*time.Second))
	if err != nil || len(newerContents.Items) != 1 || newerContents.Items[0].Reference.Revision != "2" || newerContents.Items[0].Observation == nil || newerContents.Items[0].Observation.Protocol != next.Protocol {
		t.Fatal("C-only new reading missing revised observation", newerContents, err)
	}
	// A source binding change also fences manifest-free I01 readings.
	current, e = f.consumerStore.LoadContentCollection(f.ctx, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, err = f.consumerStore.ControlContentCollection(f.ctx, c.ID, "paused", current.Revision, false); err != nil {
		t.Fatal(err)
	}
	if _, err = f.workspace.GetRuntimeReading(f.ctx, contentSubject, pure.ID, now.Add(time.Second)); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("I01-only reading ignored source C revocation", err)
	}
	if _, err = f.workspace.GetRuntimeContents(f.ctx, f.subject, r.ID, refs["content-only"], 100, "", now.Add(time.Second)); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("paused C exposed observation", err)
	}
	// Single C revoke leaves the historical B-only reading but fences C members.
	f.exec(`UPDATE workbench_access_grants SET allow_content=false,allowed_content_purposes='[]' WHERE subject_id=$1`, f.subject)
	if _, err = f.workspace.GetRuntimeReading(f.ctx, f.subject, old.ID, now); err != nil {
		t.Fatal("C revoke invalidated old B", err)
	}
	if _, err = f.workspace.ResolveRuntimeContent(f.ctx, f.subject, r.ID, refs["content-only"], selected, now); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("revoked C still readable", err)
	}
	if err = db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal("historical restore checked current grants", err)
	}
}
