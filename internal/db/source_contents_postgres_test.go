package db_test

import (
	"encoding/json"
	"errors"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"os"
	"testing"
	"time"
)

// Explicit PG verification only: this test is not included in the X1 lightweight
// run. It exercises the exact migration39 tables, locks, rollback and restore.
func TestContentIngestPostgresStaleBindingAndRestore(t *testing.T) {
	if os.Getenv("XINSIGHT_POSTGRES_DSN") == "" {
		t.Skip("explicit PostgreSQL fixture required")
	}
	f := newSourceFixture(t)
	store := f.consumerStore
	at := "2026-09-15T00:00:00.000000Z"
	c := sourcefacts.ContentCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: "content-source", OrgID: "7", ProjectID: "9", CredentialID: "c-one", RegistrationHash: sourcefacts.Hash([]byte("registration-one")), ContentScopeRevision: "scope-one", RecoveryEpoch: "1", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: sourcefacts.ContentBoundary{Time: at, AfterSequence: "0"}}
	var e error
	c, e = store.RegisterContentCollection(f.ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	newProjection := c
	newProjection.Revision = 0
	if _, err := store.ControlContentCollection(f.ctx, c.ID, "active", c.Revision, false, newProjection); !errors.Is(err, sourcefacts.ErrScopeChanged) {
		t.Fatal("source projection without persisted revision crossed activation CAS", err)
	}
	c, e = store.ControlContentCollection(f.ctx, c.ID, "active", c.Revision, false, c)
	if e != nil {
		t.Fatal(e)
	}
	doc := sourcefacts.ContentManifest{SchemaVersion: sourcefacts.ContentManifestVersion, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, RequestID: "content-only", Profile: sourcefacts.ContentProfile, Revision: "1", OccurredAt: at, RecordedAt: at, Protocol: "openai.responses", ObservationPoint: "client_request", ContentItemID: "body", Capture: sourcefacts.ContentCapture{Mode: "metadata_only", PolicyRef: "policy", PolicyVersion: "1", PolicyEffectiveAt: at, Completeness: "not_captured", Reason: "policy_metadata_only"}}
	doc.SourceFactID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion, doc.SourceInstance, doc.OrgID, doc.ProjectID, doc.Profile, doc.RequestID, doc.ObservationPoint, doc.AttemptID, doc.ContentItemID)
	doc.EventID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion+".revision", doc.SourceFactID, doc.Revision)
	canonical := func(v any) []byte {
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
	entry := sourcefacts.ContentManifestEntry{Sequence: "1", Document: doc, SourceSHA256: sourcefacts.Hash(canonical(doc)), Availability: sourcefacts.ContentAvailability{State: "not_captured", EvaluatedAt: at}}
	page := sourcefacts.ContentManifestPage{SchemaVersion: sourcefacts.ContentManifestPageVersion, Profile: sourcefacts.ContentProfile, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, GeneratedAt: at, ContentScopeRevision: c.ContentScopeRevision, RecoveryEpoch: c.RecoveryEpoch, CoverageFrom: c.CoverageFrom, RetentionFloor: c.CoverageFrom, AfterSequence: "0", LastSequence: "1", HighWatermarkSequence: "1", NextCursor: "next", HighWatermark: "high", Coverage: "bounded", Gaps: []sourcefacts.ContentGap{}, Items: []sourcefacts.ContentManifestEntry{entry}}
	raw := canonical(page)
	if e = store.CommitContentManifests(f.ctx, c, raw, time.Now()); e != nil {
		t.Fatal(e)
	}
	if e = store.CommitContentManifests(f.ctx, c, raw, time.Now()); !errors.Is(e, sourcefacts.ErrStreamChanged) {
		t.Fatal("old checkpoint replay", e)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_content=true,allowed_content_purposes='["human_review"]' WHERE subject_id=$1`, f.subject)
	instant, _ := time.Parse(sourcefacts.TimeFormat, at)
	start, end := instant.Add(-time.Minute), instant.Add(time.Minute)
	readingNow := time.Now().UTC().Add(2 * time.Minute)
	input := workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: workbench.PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "UTC"}}
	firstReading, e := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, readingNow)
	if e != nil {
		t.Fatal(e)
	}
	firstPage, e := f.workspace.GetRuntimeRequests(f.ctx, f.subject, firstReading.ID, "all", 10, "", readingNow)
	if e != nil || len(firstPage.Items) != 1 {
		t.Fatal("initial content membership", firstPage, e)
	}
	immutableSnapshot := func() string {
		t.Helper()
		var v string
		if e := f.admin.QueryRow(`SELECT jsonb_build_array((SELECT to_jsonb(d) FROM source_content_manifests d WHERE source_system_id=$1::uuid),(SELECT to_jsonb(r) FROM runtime_readings r WHERE id=$2),(SELECT jsonb_agg(to_jsonb(m) ORDER BY source_fact_id) FROM runtime_reading_content_refs m WHERE reading_id=$2))::text`, f.source, firstReading.ID).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	before := immutableSnapshot()
	current, e := store.LoadContentCollection(f.ctx, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	old := current
	current, e = store.ControlContentCollection(f.ctx, current.ID, "paused", current.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	next := current
	next.CredentialID = "c-two"
	next.RegistrationHash = sourcefacts.Hash([]byte("registration-two"))
	next.ContentScopeRevision = "scope-two"
	next.RecoveryEpoch = "2"
	next, e = store.RebindContentCollection(f.ctx, next, current.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.RecordContentFailure(f.ctx, old, "source_paused", time.Now()); !errors.Is(e, sourcefacts.ErrStreamChanged) {
		t.Fatal("old credential overwrote new state", e)
	}
	loaded, e := store.LoadContentCollection(f.ctx, c.ID)
	if e != nil || loaded.CredentialID != "c-two" || loaded.Cursor != "" || loaded.LastError != "scope_changed" {
		t.Fatal("bad rebind", e)
	}
	var n int
	f.admin.QueryRow(`SELECT count(*) FROM source_content_manifests WHERE source_system_id=$1::uuid`, f.source).Scan(&n)
	if n != 1 {
		t.Fatal("history changed", n)
	}
	next, e = store.ControlContentCollection(f.ctx, next.ID, "active", next.Revision, true, next)
	if e != nil {
		t.Fatal(e)
	}
	page.ContentScopeRevision, page.RecoveryEpoch = next.ContentScopeRevision, next.RecoveryEpoch
	received2 := readingNow.Add(time.Second)
	if e = store.CommitContentManifests(f.ctx, next, canonical(page), received2); e != nil {
		t.Fatal("same fact replay after rebind", e)
	}
	if immutableSnapshot() != before {
		t.Fatal("replay rewrote old fact or reading")
	}
	if _, e = f.workspace.GetRuntimeReading(f.ctx, f.subject, firstReading.ID, received2); !errors.Is(e, workbench.ErrAccessChanged) {
		t.Fatal("old reading revived", e)
	}
	preReceipt, e := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, readingNow)
	if e != nil {
		t.Fatal(e)
	}
	noLate, e := f.workspace.GetRuntimeRequests(f.ctx, f.subject, preReceipt.ID, "all", 10, "", readingNow)
	if e != nil || len(noLate.Items) != 0 {
		t.Fatal("new binding borrowed old received_at", noLate, e)
	}
	fresh, e := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, received2)
	if e != nil {
		t.Fatal(e)
	}
	freshPage, e := f.workspace.GetRuntimeRequests(f.ctx, f.subject, fresh.ID, "all", 10, "", received2)
	if e != nil || len(freshPage.Items) != 1 {
		t.Fatal("replay did not create new C-only qualification", freshPage, e)
	}
	freshContents, e := f.workspace.GetRuntimeContents(f.ctx, f.subject, fresh.ID, freshPage.Items[0].Ref, 10, "", received2)
	if e != nil || len(freshContents.Items) != 1 || freshContents.Items[0].Reference.SourceSHA256 != entry.SourceSHA256 {
		t.Fatal("replay changed frozen fact", freshContents, e)
	}
	receiptCount := func() int {
		t.Helper()
		var n int
		if e := f.admin.QueryRow(`SELECT count(*) FROM source_content_receipts WHERE source_system_id=$1::uuid`, f.source).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	if receiptCount() != 2 {
		t.Fatal("new binding receipt missing")
	}
	if e = store.CommitContentManifests(f.ctx, c, raw, received2); e == nil || receiptCount() != 2 {
		t.Fatal("late old binding accepted", e)
	}
	current, e = store.LoadContentCollection(f.ctx, next.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.ControlContentCollection(f.ctx, current.ID, "paused", current.Revision, false); e != nil {
		t.Fatal(e)
	}
	third := next
	third.ID = testUUID(t)
	third.CredentialID = "c-three"
	third.RegistrationHash = sourcefacts.Hash([]byte("registration-three"))
	third.ContentScopeRevision = "scope-three"
	third.RecoveryEpoch = "3"
	third, e = store.RegisterContentCollection(f.ctx, third)
	if e != nil {
		t.Fatal(e)
	}
	third, e = store.ControlContentCollection(f.ctx, third.ID, "active", third.Revision, false, third)
	if e != nil {
		t.Fatal(e)
	}
	page.ContentScopeRevision, page.RecoveryEpoch = third.ContentScopeRevision, third.RecoveryEpoch
	received3 := received2.Add(time.Second)
	if e = store.CommitContentManifests(f.ctx, third, canonical(page), received3); e != nil {
		t.Fatal("same fact replay in new collection", e)
	}
	newest, e := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, received3)
	if e != nil {
		t.Fatal(e)
	}
	newestPage, e := f.workspace.GetRuntimeRequests(f.ctx, f.subject, newest.ID, "all", 10, "", received3)
	if e != nil || len(newestPage.Items) != 1 || receiptCount() != 3 {
		t.Fatal("new collection lost deduplicated fact", newestPage, e)
	}
	if immutableSnapshot() != before {
		t.Fatal("new collection rewrote historical fact or reading")
	}
	current, e = store.LoadContentCollection(f.ctx, third.ID)
	if e != nil {
		t.Fatal(e)
	}
	bad := page
	bad.AfterSequence = "1"
	bad.LastSequence = "2"
	bad.HighWatermarkSequence = "2"
	bad.Items = append([]sourcefacts.ContentManifestEntry(nil), page.Items...)
	bad.Items[0].Sequence = "2"
	bad.Items[0].SourceSHA256 = sourcefacts.Hash([]byte("bad"))
	if e = store.CommitContentManifests(f.ctx, current, canonical(bad), received3); e == nil || receiptCount() != 3 {
		t.Fatal("wrong hash replay accepted", e)
	}
	bad.Items[0].SourceSHA256 = entry.SourceSHA256
	bad.Items[0].Document.ProjectID = "99"
	if e = store.CommitContentManifests(f.ctx, current, canonical(bad), received3); e == nil || receiptCount() != 3 {
		t.Fatal("wrong identity replay accepted", e)
	}
	current, e = store.ControlContentCollection(f.ctx, current.ID, "paused", current.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	limited := current
	limited.ContentScopeRevision = "scope-limited"
	limited.CoverageFrom.Time = instant.Add(time.Hour).Format(sourcefacts.TimeFormat)
	limited, e = store.RebindContentCollection(f.ctx, limited, current.Revision)
	if e != nil {
		t.Fatal(e)
	}
	limited, e = store.ControlContentCollection(f.ctx, limited.ID, "active", limited.Revision, true, limited)
	if e != nil {
		t.Fatal(e)
	}
	page.ContentScopeRevision = limited.ContentScopeRevision
	page.CoverageFrom = limited.CoverageFrom
	page.RetentionFloor = limited.CoverageFrom
	if e = store.CommitContentManifests(f.ctx, limited, canonical(page), received3); e == nil || receiptCount() != 3 {
		t.Fatal("out-of-range historical replay accepted", e)
	}
	if e = db.ValidateWorkbenchRestore(f.ctx, f.admin); e != nil {
		t.Fatal("historical reading/receipt restore", e)
	}
	if e = db.ValidateSourceContentsRestore(f.ctx, f.admin); e != nil {
		t.Fatal(e)
	}
}
