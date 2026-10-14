package db_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestRuntimeDimensionsFrozenIdentityAndProfileBoundaries(t *testing.T) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	start, selection, attempt := gold[0], gold[2], gold[1]
	clone := func(org, request, subject, model, provider string) sourcefacts.Observation {
		d := selection
		c := *d.Capability
		d.Capability = &c
		d.OrgID, d.RequestID, d.SubjectID = org, request, subject
		c.ModelRef, c.Provider = model, provider
		return d
	}
	a := clone("2", "request-1", "attempt-1", "model-a", "provider-a")
	b := clone("2", "request-1", "attempt-2", "model-a", "provider-b")
	otherOrg := clone("4", "request-1", "attempt-1", "model-a", "provider-a")
	otherStart := start
	otherStart.OrgID = "4"
	unknown := clone("2", "unknown-entry", "attempt-1", "model-b", "provider-a")
	brokenStart := start
	brokenStart.RequestID, brokenStart.SubjectID = "broken", "broken"
	broken := clone("2", "broken", "attempt-1", "untrusted-model", "untrusted-provider")
	broken.Revision, broken.PreviousRevision = "2", "1"
	broken.PreviousHash, broken.CorrectionReason = "sha256:"+strings.Repeat("a", 64), "late source revision"
	hidden := clone("2", "hidden", "attempt-1", "hidden-model", "hidden-provider")
	hidden.ProjectID = "999"
	putRuntimeDocuments(t, f, f.source, start, a, b, otherStart, otherOrg, unknown, brokenStart, broken, hidden, attempt)
	f.exec(`UPDATE workbench_access_grants SET source_project_ref='project-3',allow_source_runtime=true,allow_source_attempt=true,allow_source_capability=true WHERE subject_id=$1`, f.subject)
	input := runtimeRequestInput(t, start)
	now := time.Now().UTC()
	r, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil {
		t.Fatal(err)
	}
	get := func(dimension, entry string, limit int, cursor string) workbench.RuntimeDimensions {
		t.Helper()
		x, err := f.workspace.GetRuntimeDimensions(f.ctx, f.subject, r.ID, dimension, entry, limit, cursor, now)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	models := get("selected_model", "all", 100, "")
	if models.Requests != 4 || models.RequestsWithValues != 3 || models.RequestsWithoutValues != 1 || models.MissingPredecessors != 1 || !models.GroupsMayOverlap || len(models.Items) != 2 {
		t.Fatal("wrong scoped dimension population", models)
	}
	if models.Items[0] != (workbench.RuntimeDimensionGroup{Value: "model-a", EvidenceState: "partial", Requests: 2, Subjects: 3, ObservationCount: 3}) || models.Items[1].Value != "model-b" || models.Items[1].Requests != 1 {
		t.Fatal("fallback or org identity conflated", models.Items)
	}
	known := get("selected_model", "known", 100, "")
	un := get("selected_model", "unknown", 100, "")
	if known.Requests != 3 || known.RequestsWithValues != 2 || len(known.Items) != 1 || un.Requests != 1 || un.Items[0].Value != "model-b" {
		t.Fatal("unknown entry mixed into trusted-start cohort", known, un)
	}
	providers := get("selected_provider", "all", 100, "")
	if len(providers.Items) != 2 || providers.Items[0].Value != "provider-a" || providers.Items[0].Requests != 3 || providers.Items[1].Value != "provider-b" || providers.Items[1].Requests != 1 {
		t.Fatal("provider state became identity or overlapping requests were flattened", providers)
	}
	operations := get("operation", "all", 100, "")
	if len(operations.Items) != 1 || operations.Items[0].Requests != 4 || operations.Items[0].Subjects != 7 || operations.Items[0].ObservationCount != 8 {
		t.Fatal("multiple profiles became extra operations or requests", operations)
	}
	for _, dimension := range []string{"required_capability", "enabled_capability"} {
		x := get(dimension, "all", 100, "")
		if len(x.Items) != 1 || x.Items[0].Value != "tools" || x.Items[0].Requests != 3 || x.Items[0].Subjects != 4 {
			t.Fatal("capability membership was not subject scoped", dimension, x)
		}
		if dimension == "enabled_capability" && x.Items[0].EvidenceState != "verified" {
			t.Fatal("source evidence qualification lost", x)
		}
	}
	page := get("selected_model", "all", 1, "")
	if page.NextCursor == "" || get("selected_model", "all", 1, page.NextCursor).Items[0].Value != "model-b" {
		t.Fatal("pagination lost model group", page)
	}
	for _, pair := range [][2]string{{"selected_provider", "all"}, {"selected_model", "known"}} {
		if _, err := f.workspace.GetRuntimeDimensions(f.ctx, f.subject, r.ID, pair[0], pair[1], 1, page.NextCursor, now); !errors.Is(err, workbench.ErrInvalid) {
			t.Fatal("cross-dimension/cohort cursor admitted", pair, err)
		}
	}
	if _, err := f.workspace.GetRuntimeDimensions(f.ctx, f.subject, r.ID, "money", "all", 100, "", now); !errors.Is(err, workbench.ErrInvalid) {
		t.Fatal("unknown dimension admitted", err)
	}
	late := clone("2", "request-1", "attempt-late", "model-late", "provider-late")
	putRuntimeDocuments(t, f, f.source, late)
	if after := get("selected_model", "all", 100, ""); !reflect.DeepEqual(models, after) {
		t.Fatal("late source mutation changed frozen dimensions", models, after)
	}
	if _, err := f.workspace.GetRuntimeDimensions(f.ctx, f.subject, r.ID, "selected_model", "all", 100, "", r.ExpiresAt); !errors.Is(err, workbench.ErrExpired) {
		t.Fatal("expired reading admitted", err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_source_capability=false WHERE subject_id=$1`, f.subject)
	if _, err := f.workspace.GetRuntimeDimensions(f.ctx, f.subject, r.ID, "selected_model", "all", 100, "", now); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("revoked capability remained visible", err)
	}
	r, err = f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	restricted := get("selected_model", "all", 100, "")
	if restricted.Requests != 3 || restricted.RequestsWithValues != 0 || restricted.MissingPredecessors != 0 || len(restricted.Items) != 0 {
		t.Fatal("ungranted capability presence leaked", restricted)
	}
	var money int64
	if err := f.admin.QueryRow(`SELECT count(*) FROM workbench_economic_facts`).Scan(&money); err != nil || money != 0 {
		t.Fatal("dimension observations created money", money, err)
	}
}

func TestRuntimeDimensionsDestinationRegistrationIsNotDelivery(t *testing.T) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	var destination sourcefacts.Observation
	for _, d := range gold {
		if d.Destination != nil {
			destination = d
			break
		}
	}
	if destination.Destination == nil {
		t.Fatal("missing frozen destination vector")
	}
	putRuntimeDocuments(t, f, f.source, destination)
	f.exec(`UPDATE workbench_access_grants SET allow_source_destination=true WHERE subject_id=$1`, f.subject)
	now := time.Now().UTC()
	r, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, runtimeRequestInput(t, destination), now)
	if err != nil {
		t.Fatal(err)
	}
	for dimension, value := range map[string]string{"destination_provider": "synthetic", "destination_origin": "https://provider.invalid"} {
		x, err := f.workspace.GetRuntimeDimensions(f.ctx, f.subject, r.ID, dimension, "unknown", 100, "", now)
		if err != nil || len(x.Items) != 1 || x.Items[0].Value != value || x.Items[0].EvidenceState != "registered" {
			t.Fatal("destination registration lost source semantics", x, err)
		}
	}
}
