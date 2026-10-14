package db_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestRuntimeReaderWithoutEconomicPermission(t *testing.T) {
	f := newSourceFixture(t)
	a := f.documents[sourcefacts.ContextProfile]
	b := runtimeGolden(t)[0]
	b.SourceInstance, b.OrgID, b.ProjectID, b.RequestID, b.SubjectID = a.SourceInstance, a.OrgID, a.ProjectID, a.RequestID, a.RequestID
	b.OccurredAt, b.RecordedAt = a.OccurredAt, a.RecordedAt
	putRuntimeDocuments(t, f, f.source, b)
	f.money()
	f.allow()
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id=$1`, f.subject)
	now := time.Now().UTC().Add(time.Minute)
	input := runtimeRequestInput(t, b)
	full, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_summary=false,allow_details=false WHERE subject_id=$1`, f.subject)
	if _, err = f.workspace.GetRuntimeReading(f.ctx, f.subject, full.ID, now); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("economic revocation preserved old reading", err)
	}
	r, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil || r.ObservationCount != 1 || r.RequestsWithEntryTime != 1 {
		t.Fatal("B-only reader lost authorized request", r, err)
	}
	requests, err := f.workspace.GetRuntimeRequests(f.ctx, f.subject, r.ID, "all", 100, "", now)
	if err != nil || len(requests.Items) != 1 {
		t.Fatal(requests, err)
	}
	page, err := f.workspace.GetRuntimeAssociations(f.ctx, f.subject, r.ID, requests.Items[0].Ref, 100, "", now)
	if err != nil || len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatal("economic or A existence disclosed", page, err)
	}
	var executionID string
	if e := f.admin.QueryRow(`SELECT execution_id FROM workbench_economic_facts WHERE source_system_id=$1`, f.source).Scan(&executionID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.workspace.GetRuntimeExecutionRequest(f.ctx, f.subject, r.ID, executionID, now); !errors.Is(e, workbench.ErrNotFound) {
		t.Fatal("execution bridge disclosed hidden economics", e)
	}
	if _, err = f.workspace.CreateWorkbenchReading(f.ctx, f.subject, relativeWorkbenchRequest(), now); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatal("B-only permission granted cost reading", err)
	}
	input.Filters.CostProjects = []string{"hidden-cost-project"}
	if _, err = f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatal("cost filter became an existence oracle", err)
	}
	if _, err = f.workspace.GetRuntimeCurrent(f.ctx, f.subject, input.Filters, 100, "", now); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatal("current cost filter became an existence oracle", err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=false WHERE subject_id=$1`, f.subject)
	if _, err = f.workspace.GetRuntimeReading(f.ctx, f.subject, r.ID, now); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("B revoke retained reading", err)
	}
}

func TestRuntimeAssociationsFreezeIndependentGrantsAndFullIdentity(t *testing.T) {
	f := newSourceFixture(t)
	a := f.documents[sourcefacts.ContextProfile]
	b := runtimeGolden(t)[0]
	b.SourceInstance, b.OrgID, b.ProjectID, b.RequestID, b.SubjectID = a.SourceInstance, a.OrgID, a.ProjectID, a.RequestID, a.RequestID
	b.OccurredAt, b.RecordedAt = a.OccurredAt, a.RecordedAt
	collision := b
	collision.OrgID = "999"
	otherInstance := b
	otherInstance.SourceInstance = "other-instance"
	putRuntimeDocuments(t, f, f.source, b, collision, otherInstance)
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id=$1`, f.subject)
	now := time.Now().UTC().Add(time.Minute) // late receipts below still predate cutoff
	input := runtimeRequestInput(t, b)
	create := func() workbench.RuntimeReading {
		t.Helper()
		r, e := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	r := create()
	requests, e := f.workspace.GetRuntimeRequests(f.ctx, f.subject, r.ID, "all", 100, "", now)
	if e != nil {
		t.Fatal(e)
	}
	if len(requests.Items) != 3 {
		t.Fatal("full request identities collapsed", requests)
	}
	ref := ""
	for _, q := range requests.Items {
		if q.OrgID == a.OrgID && q.SourceInstance == a.SourceInstance {
			ref = q.Ref
		}
	}
	if ref == "" {
		t.Fatal("request not found")
	}
	page, e := f.workspace.GetRuntimeAssociations(f.ctx, f.subject, r.ID, ref, 100, "", now)
	if e != nil || len(page.Items) != 0 {
		t.Fatal("B permission granted A fields", page, e)
	}
	f.allow()
	if _, e = f.workspace.GetRuntimeAssociations(f.ctx, f.subject, r.ID, ref, 100, "", now); !errors.Is(e, workbench.ErrAccessChanged) {
		t.Fatal("A grant change did not invalidate B", e)
	}
	r = create()
	page, e = f.workspace.GetRuntimeAssociations(f.ctx, f.subject, r.ID, ref, 1, "", now)
	if e != nil || len(page.Items) != 1 || page.NextCursor == "" || page.Basis != "same_source_request_identity" || page.MembershipRef != r.MembershipRef {
		t.Fatal("source page", page, e)
	}
	for _, q := range requests.Items {
		if q.Ref != ref {
			empty, e := f.workspace.GetRuntimeAssociations(f.ctx, f.subject, r.ID, q.Ref, 100, "", now)
			if e != nil || len(empty.Items) != 0 {
				t.Fatal("cross-organization or instance association", empty, e)
			}
			if _, e = f.workspace.GetRuntimeAssociations(f.ctx, f.subject, r.ID, q.Ref, 1, page.NextCursor, now); !errors.Is(e, workbench.ErrInvalid) {
				t.Fatal("cursor crossed request", e)
			}
		}
	}
	if _, e = f.workspace.GetRuntimeAssociations(f.ctx, f.subject, r.ID, "xrr_"+strings.Repeat("a", 64), 100, "", now); !errors.Is(e, workbench.ErrNotFound) {
		t.Fatal("unknown request disclosed", e)
	}
	old, e := f.workspace.GetRuntimeAssociations(f.ctx, f.subject, r.ID, ref, 100, "", now)
	if e != nil || len(old.Items) != 3 {
		t.Fatal(old, e)
	}
	// Persist a new source revision without its predecessor, and a late I-01 fact.
	corrected := a
	corrected.Revision = "3"
	corrected.PreviousRevision = "2"
	corrected.PreviousHash = "sha256:" + strings.Repeat("a", 64)
	corrected.CorrectionReason = "fixture missing predecessor"
	f.setPage(sourcefacts.ContextProfile, "late-context", corrected)
	f.pull(sourcefacts.ContextProfile)
	f.money()
	frozen, e := f.workspace.GetRuntimeAssociations(f.ctx, f.subject, r.ID, ref, 100, "", now)
	x, _ := json.Marshal(old)
	y, _ := json.Marshal(frozen)
	if e != nil || string(x) != string(y) {
		t.Fatal("late records changed old reading", e)
	}
	next := create()
	fresh, e := f.workspace.GetRuntimeAssociations(f.ctx, f.subject, next.ID, ref, 100, "", now)
	if e != nil || len(fresh.Items) != 4 {
		t.Fatal("new reading missed associated records", fresh, e)
	}
	economics, sources, missing := 0, 0, 0
	for _, item := range fresh.Items {
		if item.Kind == "economic" {
			economics++
			if item.Economic == nil || item.Economic.ExecutionID == "" || item.Economic.SourceEventHash == "" {
				t.Fatal(item)
			}
			linked, err := f.workspace.GetRuntimeExecutionRequest(f.ctx, f.subject, next.ID, item.Economic.ExecutionID, now)
			if err != nil || linked.Request.Ref != ref || linked.Request.OrgID != a.OrgID || linked.Request.SourceInstance != a.SourceInstance || linked.MembershipRef != next.MembershipRef {
				t.Fatal("execution bridge lost full identity", linked, err)
			}
			if _, err = f.workspace.GetRuntimeExecutionRequest(f.ctx, f.subject, r.ID, item.Economic.ExecutionID, now); !errors.Is(err, workbench.ErrNotFound) {
				t.Fatal("late economic fact entered old execution bridge", err)
			}

		} else {
			sources++
			if item.Source == nil || item.Source.EconomicState != "not_evaluated" {
				t.Fatal(item)
			}
			if item.Source.Profile == sourcefacts.ContextProfile {
				missing++
				if item.Source.ChainState != "missing_predecessor" || item.Source.Context != nil || item.Source.Revision != "3" {
					t.Fatal("missing lineage leaked fields", item)
				}
			}
		}
	}
	if economics != 1 || sources != 3 || missing != 1 {
		t.Fatal(fresh)
	}
	var count int
	if e = f.admin.QueryRow(`SELECT count(*) FROM workbench_economic_facts`).Scan(&count); e != nil || count != 1 {
		t.Fatal("runtime association created money", count, e)
	}
	if e = db.ValidateWorkbenchRestore(f.ctx, f.admin); e != nil {
		t.Fatal(e)
	}
	f.exec(`UPDATE runtime_reading_source_members SET chain_state='complete' WHERE reading_id=$1 AND chain_state='missing_predecessor'`, next.ID)
	if e = db.ValidateWorkbenchRestore(f.ctx, f.admin); e == nil {
		t.Fatal("tampered association accepted in restore")
	}
	f.exec(`UPDATE runtime_reading_source_members m SET chain_state='missing_predecessor' FROM source_fact_documents d WHERE m.reading_id=$1 AND m.source_system_id=d.source_system_id AND m.event_id=d.event_id AND d.revision=3`, next.ID)
	if e = db.ValidateWorkbenchRestore(f.ctx, f.admin); e != nil {
		t.Fatal(e)
	}
	if _, e = f.workspace.GetRuntimeAssociations(f.ctx, f.subject, next.ID, ref, 1, page.NextCursor, now); !errors.Is(e, workbench.ErrInvalid) {
		t.Fatal("cursor crossed reading", e)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_source_identity=false,allow_source_cost=false WHERE subject_id=$1`, f.subject)
	if _, e = f.workspace.GetRuntimeAssociations(f.ctx, f.subject, next.ID, ref, 100, "", now); !errors.Is(e, workbench.ErrAccessChanged) {
		t.Fatal("A revoke retained frozen fields", e)
	}
	restricted := create()
	narrow, e := f.workspace.GetRuntimeAssociations(f.ctx, f.subject, restricted.ID, ref, 100, "", now)
	if e != nil || len(narrow.Items) != 2 {
		t.Fatal("independent field permissions", narrow, e)
	}
	for _, item := range narrow.Items {
		if item.Source != nil && item.Source.Profile != sourcefacts.ContextProfile {
			t.Fatal("ungranted source profile", item)
		}
	}
	// Cost project restrictions need the exact economic anchor for each request.
	f.exec(`UPDATE workbench_access_grants SET cost_project_ref='unrelated-project' WHERE subject_id=$1`, f.subject)
	restricted = create()
	q, e := f.workspace.GetRuntimeRequests(f.ctx, f.subject, restricted.ID, "all", 100, "", now)
	if e != nil || len(q.Items) != 0 {
		t.Fatal("unmatched cost project widened runtime scope", q, e)
	}
}
