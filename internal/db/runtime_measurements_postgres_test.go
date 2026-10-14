package db_test

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func runtimeMeasurementPair(gold []sourcefacts.Observation, request, state string, value *string) []sourcefacts.Observation {
	start, measurement := gold[0], gold[5]
	start.RequestID, start.SubjectID = request, request
	runtime := *start.Runtime
	runtime.StreamMode = "non_streaming"
	start.Runtime = &runtime
	measurement.RequestID, measurement.SubjectID = request, request
	m := *measurement.Measurement
	m.State, m.Value = state, value
	measurement.Measurement = &m
	return []sourcefacts.Observation{start, measurement}
}

func TestRuntimeMeasurementsFrozenT05MembershipAndExclusions(t *testing.T) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	raw, err := os.ReadFile("../sourcefacts/contract/observations/measurement-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	type expected struct {
		N   int
		P50 string
		P95 string
	}
	var vectors struct {
		Unit  string
		Cases []struct {
			ID       string
			Values   []int64 `json:"completed_observed"`
			Expected expected
			WithZero expected `json:"with_observed_zero"`
		}
	}
	if err := json.Unmarshal(raw, &vectors); err != nil || vectors.Unit != "milliseconds" {
		t.Fatal("frozen measurement fixture", err)
	}
	var values []int64
	var want, withZero expected
	for _, v := range vectors.Cases {
		if v.ID == "T05" {
			values, want, withZero = v.Values, v.Expected, v.WithZero
		}
	}
	if len(values) == 0 {
		t.Fatal("missing frozen T05")
	}
	documents := []sourcefacts.Observation{}
	for i, ms := range values {
		ns := strconv.FormatInt(ms*1_000_000, 10)
		documents = append(documents, runtimeMeasurementPair(gold, "t05-"+strconv.Itoa(i), "observed", &ns)...)
	}
	for _, state := range []string{"not_applicable", "not_observed", "legacy_unknown", "invalid"} {
		documents = append(documents, runtimeMeasurementPair(gold, state, state, nil)...)
	}
	zero := "0"
	unknown := runtimeMeasurementPair(gold, "unknown-entry", "observed", &zero)[1]
	documents = append(documents, unknown)
	hidden := runtimeMeasurementPair(gold, "hidden-project", "observed", &zero)
	for i := range hidden {
		hidden[i].ProjectID = "999"
	}
	documents = append(documents, hidden...)
	putRuntimeDocuments(t, f, f.source, documents...)
	f.exec(`UPDATE workbench_access_grants SET source_project_ref='project-3',allow_source_runtime=true WHERE subject_id=$1`, f.subject)
	now := time.Now().UTC().Add(time.Minute)
	input := runtimeRequestInput(t, gold[0])
	r, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := f.workspace.GetRuntimeMeasurements(f.ctx, f.subject, r.ID, "known", 100, "", now)
	if err != nil || len(groups.Items) != 1 {
		t.Fatal(groups, err)
	}
	g := groups.Items[0]
	if g.ObservationCount != int64(len(values)+4) || g.Percentiles.SampleCount != want.N || g.Percentiles.P50 != want.P50 || g.Percentiles.P95 != want.P95 ||
		g.Excluded.NotApplicable != 1 || g.Excluded.NotObserved != 1 || g.Excluded.LegacyUnknown != 1 || g.Excluded.Invalid != 1 {
		t.Fatalf("T05 or exclusions changed: %+v", g)
	}
	u, err := f.workspace.GetRuntimeMeasurements(f.ctx, f.subject, r.ID, "unknown", 100, "", now)
	if err != nil || len(u.Items) != 1 || u.Items[0].EntryTimeState != "unknown" || u.Items[0].Excluded.MissingContext != 1 || u.Items[0].Percentiles.State != "not_computable" || u.Items[0].Percentiles.P50 != "" {
		t.Fatal("unknown entry/context manufactured a comparable value", u, err)
	}
	first, err := f.workspace.GetRuntimeMeasurements(f.ctx, f.subject, r.ID, "all", 1, "", now)
	if err != nil || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	second, err := f.workspace.GetRuntimeMeasurements(f.ctx, f.subject, r.ID, "all", 1, first.NextCursor, now)
	if err != nil || len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].EntryTimeState == first.Items[0].EntryTimeState {
		t.Fatal("group paging changed denominator", second, err)
	}
	if _, err := f.workspace.GetRuntimeMeasurements(f.ctx, f.subject, r.ID, "known", 1, first.NextCursor, now); !errors.Is(err, workbench.ErrInvalid) {
		t.Fatal("measurement cursor crossed collection", err)
	}
	putRuntimeDocuments(t, f, f.source, runtimeMeasurementPair(gold, "observed-zero", "observed", &zero)...)
	old, err := f.workspace.GetRuntimeMeasurements(f.ctx, f.subject, r.ID, "known", 100, "", now)
	if err != nil || old.Items[0].Percentiles.SampleCount != want.N {
		t.Fatal("late zero altered frozen membership", old, err)
	}
	updated, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil {
		t.Fatal(err)
	}
	newGroups, err := f.workspace.GetRuntimeMeasurements(f.ctx, f.subject, updated.ID, "known", 100, "", now)
	if err != nil || len(newGroups.Items) != 1 || newGroups.Items[0].Percentiles.SampleCount != withZero.N || newGroups.Items[0].Percentiles.P50 != withZero.P50 || newGroups.Items[0].Percentiles.P95 != withZero.P95 {
		t.Fatal("explicit observed zero lost", newGroups, err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=false WHERE subject_id=$1`, f.subject)
	if _, err := f.workspace.GetRuntimeMeasurements(f.ctx, f.subject, r.ID, "known", 100, "", now); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("revoked measurement reading survived", err)
	}
}

func TestRuntimeMeasurementsDoNotInferContextOrCompleteBrokenEvidence(t *testing.T) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	ns := "300000000"
	gap := runtimeMeasurementPair(gold, "missing-predecessor", "observed", &ns)
	gap[1].Revision, gap[1].PreviousRevision = "2", "1"
	gap[1].PreviousHash = "sha256:" + strings.Repeat("a", 64)
	gap[1].CorrectionReason = "source correction"
	boundary := runtimeMeasurementPair(gold, "incompatible-mode", "observed", &ns)
	boundary[0].Runtime.StreamMode = "streaming"
	conflict := runtimeMeasurementPair(gold, "conflicting-mode", "observed", &ns)
	other := conflict[0]
	runtime := *other.Runtime
	runtime.StreamMode = "streaming"
	other.Runtime, other.Kind = &runtime, "local_delivery"
	documents := append(append(gap, boundary...), conflict...)
	documents = append(documents, other)
	unavailable := runtimeMeasurementPair(gold, "unavailable-operation", "observed", &ns)
	unavailable[0].Runtime.Operation = "not_available"
	documents = append(documents, unavailable...)
	putRuntimeDocuments(t, f, f.source, documents...)
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id=$1`, f.subject)
	now := time.Now().UTC().Add(time.Minute)
	r, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, runtimeRequestInput(t, gold[0]), now)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := f.workspace.GetRuntimeMeasurements(f.ctx, f.subject, r.ID, "known", 100, "", now)
	if err != nil {
		t.Fatal(err)
	}
	var missing, mismatch, conflicting, noContext int64
	for _, g := range groups.Items {
		if g.Percentiles.SampleCount != 0 || g.Percentiles.State != "not_computable" || g.Percentiles.P95 != "" {
			t.Fatal("incomplete or conflicting evidence entered percentiles", g)
		}
		missing += g.Excluded.MissingPredecessor
		mismatch += g.Excluded.IncompatibleContext
		conflicting += g.Excluded.ConflictingContext
		noContext += g.Excluded.MissingContext
	}
	if missing != 1 || mismatch != 1 || conflicting != 1 || noContext != 1 {
		t.Fatal("incorrect exclusion categories", groups)
	}
}
