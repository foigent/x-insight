package db_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func resourceForRuntime(source string, request, destination sourcefacts.Observation) workbench.ResourceAccessRecord {
	scope := workbench.ResourceScope{SourceSystemID: source, SourceInstance: request.SourceInstance, OrgID: request.OrgID, ProjectID: request.ProjectID}
	return workbench.ResourceAccessRecord{Version: workbench.ResourceAccessVersion, Synthetic: true, RecordRef: "enterprise-read", Revision: 1,
		Reporter: scope, Target: scope, RequestID: request.RequestID, ApplicationRef: "synthetic-app", ActorRef: "synthetic-user",
		OccurredAt: time.Date(2026, 9, 10, 0, 0, 0, 123456789, time.UTC), ReportKind: "source_audit",
		Resource: workbench.ResourceObject{Ref: "document-one", Version: "v2", Name: "合成企业知识库", Location: "enterprise_service", Ownership: "known", OwnerRef: "enterprise-one", Classification: "known", ClassificationRef: "internal-v1", DeviceManagement: "unknown"},
		Action:   "read", AccessOutcome: "read", AccessDecision: "allow", DecisionRef: "acl-one", PolicyVersion: "acl-v2",
		Input: &workbench.ResourceInputClaim{FragmentRef: "section-three", FragmentVersion: "v2.3", AttemptID: destination.SubjectID, AttemptKind: destination.SubjectKind, ReportedInputRef: destination.Destination.InputBindingRef,
			Transforms: []workbench.ResourceTransform{{Kind: "summarize", InputVersion: "v2.3", OutputVersion: "summary-v1", ReceiptRef: "transform-one"}}}}
}

func TestRuntimeResourcesFullIdentityFrozenInputAndRevocation(t *testing.T) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	known := gold[0]
	var destination sourcefacts.Observation
	for _, d := range gold {
		if d.Profile == sourcefacts.DestinationProfile {
			destination = d
			break
		}
	}
	if destination.Destination == nil {
		t.Fatal("destination fixture absent")
	}
	destination.SourceInstance, destination.OrgID, destination.ProjectID, destination.RequestID = known.SourceInstance, known.OrgID, known.ProjectID, known.RequestID
	destination.Destination.InputBindingState = "known"
	destination.Destination.InputBindingRef = "input-one"
	putRuntimeDocuments(t, f, f.source, known, destination)
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true,allow_source_destination=true WHERE subject_id=$1`, f.subject)
	now := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	input := runtimeRequestInput(t, known)
	makeReading := func(at time.Time) workbench.RuntimeReading {
		t.Helper()
		r, e := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, at)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	requestRef := func(r workbench.RuntimeReading) string {
		t.Helper()
		p, e := f.workspace.GetRuntimeRequests(f.ctx, f.subject, r.ID, "all", 10, "", now)
		if e != nil || len(p.Items) != 1 {
			t.Fatal(p, e)
		}
		return p.Items[0].Ref
	}
	read := func(r workbench.RuntimeReading, limit int, cursor string) workbench.RuntimeResources {
		t.Helper()
		p, e := f.workspace.GetRuntimeResources(f.ctx, f.subject, r.ID, requestRef(r), limit, cursor, now)
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	put := func(record workbench.ResourceAccessRecord, at time.Time) {
		t.Helper()
		inserted, e := f.consumerStore.StoreSyntheticResourceRecord(f.ctx, record, at)
		if e != nil || !inserted {
			t.Fatal("insert resource", inserted, e)
		}
	}
	r1 := resourceForRuntime(f.source, known, destination)
	put(r1, now.Add(-time.Second))
	if inserted, e := f.consumerStore.StoreSyntheticResourceRecord(f.ctx, r1, now); e != nil || inserted {
		t.Fatal("idempotent replay", inserted, e)
	}
	changed := r1
	changed.ActorRef = "other-user"
	if _, e := f.consumerStore.StoreSyntheticResourceRecord(f.ctx, changed, now); !errors.Is(e, workbench.ErrConflict) {
		t.Fatal("same revision overwrite", e)
	}
	without := makeReading(now)
	if len(read(without, 10, "").Items) != 0 {
		t.Fatal("economic and runtime grants leaked resource")
	}
	f.exec(`UPDATE workbench_access_grants SET allow_resource=true WHERE subject_id=$1`, f.subject)
	if _, e := f.workspace.GetRuntimeResources(f.ctx, f.subject, without.ID, requestRefUnchecked(t, f, without), 10, "", now); !errors.Is(e, workbench.ErrAccessChanged) {
		t.Fatal("resource grant failed to invalidate old reading", e)
	}
	r2 := resourceForRuntime(f.source, known, destination)
	r2.RecordRef = "pc-read"
	r2.ReportKind = "client_assertion"
	r2.Resource.Name = "合成个人电脑资料"
	r2.Resource.Location = "pc"
	r2.Resource.Ownership, r2.Resource.OwnerRef, r2.Resource.Classification, r2.Resource.ClassificationRef = "unknown", "", "unknown", ""
	r2.Input.ReportedInputRef = "different-input"
	put(r2, now.Add(-time.Second))
	r3 := resourceForRuntime(f.source, known, destination)
	r3.RecordRef = "other-attempt-kind"
	if r3.Input.AttemptKind == "provider_attempt" {
		r3.Input.AttemptKind = "media_attempt"
	} else {
		r3.Input.AttemptKind = "provider_attempt"
	}
	put(r3, now.Add(-time.Second))
	r4 := r1
	r4.Reporter.SourceInstance = "second-reporter-instance"
	r4.Reporter.OrgID = "different-org"
	r4.Input = nil
	put(r4, now.Add(-time.Second))
	hidden := r1
	hidden.RecordRef = "hidden-target"
	hidden.Target.OrgID = "other-target-org"
	put(hidden, now.Add(-time.Second))
	r := makeReading(now)
	page := read(r, 2, "")
	if len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatal("resource page", page)
	}
	next := read(r, 2, page.NextCursor)
	if len(next.Items) != 2 || next.NextCursor != "" {
		t.Fatal("resource next page", next)
	}
	items := append(page.Items, next.Items...)
	seen := map[string]bool{}
	var firstHash string
	for _, item := range items {
		key := item.Record.Reporter.SourceInstance + ":" + item.Record.RecordRef
		if seen[key] {
			t.Fatal("duplicate resource member", key)
		}
		seen[key] = true
		if item.ContentVerification != "not_performed" || item.InputEvidenceBasis != "frozen_reference_witnesses" || len(item.CapturedInputRefs) > 2 {
			t.Fatal("unbounded or promoted evidence", item)
		}
		if item.Record.Reporter.SourceInstance == r1.Reporter.SourceInstance && item.Record.RecordRef == r1.RecordRef {
			firstHash = item.RecordHash
			if item.InputState != "reference_matched" || len(item.CapturedInputRefs) != 1 {
				t.Fatal("reference agreement", item)
			}
		}
		if item.Record.RecordRef == r2.RecordRef && (item.InputState != "reference_mismatch" || item.Record.Resource.Ownership != "unknown") {
			t.Fatal("PC unknown/mismatch", item)
		}
		if item.Record.RecordRef == r3.RecordRef && item.InputState != "not_confirmed" {
			t.Fatal("attempt kinds merged", item)
		}
	}
	if firstHash == "" {
		t.Fatal("full reporter identity overwrote first record")
	}
	if _, e := f.workspace.GetRuntimeObservations(f.ctx, f.subject, r.ID, requestRef(r), "", 2, page.NextCursor, now); !errors.Is(e, workbench.ErrInvalid) {
		t.Fatal("cross-collection cursor accepted", e)
	}
	late := r1
	late.RecordRef = "late-record"
	put(late, now.Add(time.Second))
	withdrawn := r1
	withdrawn.Revision = 2
	withdrawn.PreviousHash = firstHash
	withdrawn.Withdrawn = true
	put(withdrawn, now.Add(time.Second))
	old := read(r, 10, "")
	if len(old.Items) != 4 || old.MembershipRef != r.MembershipRef {
		t.Fatal("late data changed frozen membership", old)
	}
	for _, item := range old.Items {
		if item.Record.RecordRef == r1.RecordRef && item.Record.Reporter == r1.Reporter && (!item.ChangedSinceReading || item.InputState != "reassessment_required" || item.Record.Withdrawn) {
			t.Fatal("old association not reassessed", item)
		}
	}
	newReading := makeReading(now.Add(2 * time.Second))
	latest := read(newReading, 10, "")
	if len(latest.Items) != 5 {
		t.Fatal("new reading missing late record", latest)
	}
	for _, item := range latest.Items {
		if item.Record.RecordRef == r1.RecordRef && item.Record.Reporter == r1.Reporter && item.InputState != "withdrawn" {
			t.Fatal("withdrawal lost", item)
		}
	}
	if err := db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal("populated resources restore", err)
	}
	var money int
	if err := f.admin.QueryRow(`SELECT count(*) FROM workbench_economic_facts`).Scan(&money); err != nil || money != 0 {
		t.Fatal("resource created money", money, err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_resource=false WHERE subject_id=$1`, f.subject)
	if _, e := f.workspace.GetRuntimeResources(f.ctx, f.subject, newReading.ID, requestRefUnchecked(t, f, newReading), 10, "", now); !errors.Is(e, workbench.ErrAccessChanged) {
		t.Fatal("revoked resource remained readable", e)
	}
	if len(read(makeReading(now.Add(3*time.Second)), 10, "").Items) != 0 {
		t.Fatal("new reading leaked revoked resources")
	}
	if err := db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal("historical restore depended on present grant", err)
	}
	f.exec(`ALTER TABLE resource_access_records DISABLE TRIGGER resource_access_records_immutable`)
	if err := db.ValidateWorkbenchRestore(f.ctx, f.admin); err == nil || !strings.Contains(err.Error(), "resource") {
		t.Fatal("restore accepted disabled resource guard", err)
	}
}

// Used only after revocation; it does not bypass a product read.
func requestRefUnchecked(t *testing.T, f *sourceFixture, r workbench.RuntimeReading) string {
	t.Helper()
	var ref string
	if err := f.admin.QueryRow(`SELECT request_ref FROM runtime_reading_requests WHERE reading_id=$1 LIMIT 1`, r.ID).Scan(&ref); err != nil {
		t.Fatal(err)
	}
	return ref
}
