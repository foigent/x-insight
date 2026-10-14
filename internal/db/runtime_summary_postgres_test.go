package db_test

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func runtimeSummaryDocument(gold sourcefacts.Observation, request, kind, delivery string) sourcefacts.Observation {
	d := gold
	runtime := *gold.Runtime
	d.Runtime = &runtime
	d.RequestID, d.SubjectID, d.Kind = request, request, kind
	d.Runtime.LocalDelivery = delivery
	if kind == "admission_rejected" {
		d.Runtime.Admission = "rejected"
	}
	return d
}

func TestRuntimeSummaryT06FrozenRequestDenominator(t *testing.T) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	// Use the actual frozen T06 membership, including running and unknown.
	// Its policy-rejected case can only map to generic admission rejection
	// because the source runtime schema does not attest the rejection reason.
	raw, err := os.ReadFile("../sourcefacts/contract/observations/measurement-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Cases []struct {
			ID       string
			Requests []struct{ ID, State string }
			Expected struct{ Requests, Numerator, Denominator, Excluded int64 }
		}
	}
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	var states []struct{ ID, State string }
	var expected struct{ Requests, Numerator, Denominator, Excluded int64 }
	for _, v := range vectors.Cases {
		if v.ID == "T06" {
			states = v.Requests
			expected = v.Expected
		}
	}
	if len(states) != 8 {
		t.Fatal("missing frozen T06")
	}
	var documents []sourcefacts.Observation
	for _, state := range states {
		request := "t06-" + state.ID
		start := runtimeSummaryDocument(gold[0], request, "request_started", "not_started")
		documents = append(documents, start)
		kind, delivery := "local_delivery", "unknown"
		switch state.State {
		case "completed":
			delivery = "completed"
		case "technical_failure":
			delivery = "write_failed"
		case "policy_rejected":
			kind, delivery = "admission_rejected", "not_started"
		case "interrupted":
			delivery = "interrupted"
		case "running", "unknown":
			continue
		default:
			t.Fatal("unmapped frozen state", state.State)
		}
		terminal := runtimeSummaryDocument(gold[0], request, kind, delivery)
		documents = append(documents, terminal)
	}
	// A failed provider attempt before a successful request must not inflate
	// the request failure numerator. Financial settlement is not delivery proof.
	attempt := gold[1]
	a := *attempt.Attempt
	attempt.Attempt = &a
	attempt.RequestID, attempt.Kind = "t06-r1", "provider_rejected"
	attempt.Attempt.Provider = "rejected"
	documents = append(documents, attempt)
	unknown := runtimeSummaryDocument(gold[0], "entry-unknown", "local_delivery", "completed")
	hidden := runtimeSummaryDocument(gold[0], "hidden-request", "request_started", "completed")
	hidden.ProjectID = "999"
	documents = append(documents, unknown, hidden)
	putRuntimeDocuments(t, f, f.source, documents...)
	f.exec(`UPDATE workbench_access_grants SET source_project_ref='project-3',allow_source_runtime=true,allow_source_attempt=true WHERE subject_id=$1`, f.subject)
	now := time.Now().UTC().Add(time.Minute)
	input := runtimeRequestInput(t, gold[0])
	r, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := f.workspace.GetRuntimeSummary(f.ctx, f.subject, r.ID, "known", now)
	if err != nil {
		t.Fatal(err)
	}
	want := []workbench.RuntimeOutcomeCount{{Outcome: "admission_rejected", Requests: 1}, {Outcome: "interrupted", Requests: 1}, {Outcome: "local_delivery_completed", Requests: 3}, {Outcome: "technical_failure", Requests: 1}, {Outcome: "unresolved", Requests: 2}}
	if summary.Requests != expected.Requests || !reflect.DeepEqual(summary.Outcomes, want) || summary.DiagnosticFailure.Numerator != expected.Numerator || summary.DiagnosticFailure.Denominator != expected.Denominator || summary.DiagnosticFailure.ExcludedRequests != expected.Excluded || summary.DiagnosticFailure.State != "computed" {
		t.Fatal("T06 composition/1/(3+1)", summary)
	}
	page, err := f.workspace.GetRuntimeRequests(f.ctx, f.subject, r.ID, "known", 10, "", now)
	if err != nil || len(page.Items) != 8 {
		t.Fatal("request-level projection must use the same eight frozen requests", page, err)
	}
	pageOutcomes := map[string]string{}
	for _, item := range page.Items {
		if item.OutcomeBasis != "request_level_observation" {
			t.Fatal("request outcome lacks request-level evidence", item)
		}
		pageOutcomes[item.RequestID] = item.Outcome
	}
	if pageOutcomes["t06-r5"] != "technical_failure" || pageOutcomes["t06-r7"] != "unresolved" || pageOutcomes["t06-r8"] != "unresolved" || pageOutcomes["hidden-request"] != "" {
		t.Fatal("page outcome differs from the frozen request classifier or widened scope", pageOutcomes)
	}
	separate, err := f.workspace.GetRuntimeSummary(f.ctx, f.subject, r.ID, "unknown", now)
	if err != nil || separate.Requests != 1 || separate.DiagnosticFailure.Denominator != 1 {
		t.Fatal("unknown entry merged or hidden scope leaked", separate, err)
	}
	if _, err := f.workspace.GetRuntimeSummary(f.ctx, f.subject, r.ID, "invalid", now); !errors.Is(err, workbench.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := f.workspace.GetRuntimeSummary(f.ctx, "other-subject", r.ID, "known", now); !errors.Is(err, workbench.ErrNotFound) {
		t.Fatal("another owner read summary", err)
	}
	if _, err := f.workspace.GetRuntimeSummary(f.ctx, f.subject, r.ID, "known", r.ExpiresAt.Add(time.Second)); !errors.Is(err, workbench.ErrExpired) {
		t.Fatal("expired summary", err)
	}
	late := runtimeSummaryDocument(gold[0], "t06-r8", "local_delivery", "completed")
	putRuntimeDocuments(t, f, f.source, late)
	again, err := f.workspace.GetRuntimeSummary(f.ctx, f.subject, r.ID, "known", now)
	if err != nil || !reflect.DeepEqual(again, summary) {
		t.Fatal("late observation changed frozen summary", err)
	}
	againPage, err := f.workspace.GetRuntimeRequests(f.ctx, f.subject, r.ID, "known", 10, "", now)
	if err != nil || !reflect.DeepEqual(againPage, page) {
		t.Fatal("late observation changed frozen request outcomes", err)
	}
	var money int
	if err = f.admin.QueryRow(`SELECT count(*) FROM workbench_economic_facts`).Scan(&money); err != nil || money != 0 {
		t.Fatal("runtime summary created money", money, err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=false WHERE subject_id=$1`, f.subject)
	if _, err := f.workspace.GetRuntimeSummary(f.ctx, f.subject, r.ID, "known", now); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("revoked old summary", err)
	}
	r, err = f.workspace.CreateRuntimeReading(f.ctx, f.subject, input, now)
	if err != nil {
		t.Fatal(err)
	}
	limited, err := f.workspace.GetRuntimeSummary(f.ctx, f.subject, r.ID, "all", now)
	if err != nil || limited.Requests != 1 || limited.DiagnosticFailure.Denominator != 0 || limited.DiagnosticFailure.State != "not_computable" {
		t.Fatal("attempt-only authority inferred request outcome", limited, err)
	}
}

func TestRuntimeSummaryIncompleteAndConflictingEvidence(t *testing.T) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	for _, request := range []string{"broken", "conflict", "context", "held", "terminal-rejection", "settled-without-delivery", "completed-context"} {
		start := runtimeSummaryDocument(gold[0], request, "request_started", "not_started")
		putRuntimeDocuments(t, f, f.source, start)
	}
	broken := runtimeSummaryDocument(gold[0], "broken", "local_delivery", "completed")
	broken.Revision, broken.PreviousRevision, broken.PreviousHash, broken.CorrectionReason = "2", "1", "sha256:"+strings.Repeat("b", 64), "synthetic corrected terminal"
	complete := runtimeSummaryDocument(gold[0], "conflict", "local_delivery", "completed")
	conflict := runtimeSummaryDocument(gold[0], "conflict", "request_terminal", "write_failed")
	context := gold[9]
	context.RequestID, context.SubjectID = "context", "context"
	held := runtimeSummaryDocument(gold[0], "held", "output_held", "not_started")
	rejected := runtimeSummaryDocument(gold[0], "terminal-rejection", "request_terminal", "not_started")
	rejected.Runtime.Provider = "rejected"
	settled := runtimeSummaryDocument(gold[0], "settled-without-delivery", "request_terminal", "unknown")
	settled.Runtime.Financial = "settled"
	completed := runtimeSummaryDocument(gold[0], "completed-context", "local_delivery", "completed")
	ended := gold[9]
	ended.RequestID, ended.SubjectID = "completed-context", "completed-context"
	putRuntimeDocuments(t, f, f.source, broken, complete, conflict, context, held, rejected, settled, completed, ended)
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id=$1`, f.subject)
	now := time.Now().UTC().Add(time.Minute)
	r, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, runtimeRequestInput(t, gold[0]), now)
	if err != nil {
		t.Fatal(err)
	}
	s, err := f.workspace.GetRuntimeSummary(f.ctx, f.subject, r.ID, "known", now)
	if err != nil {
		t.Fatal(err)
	}
	want := []workbench.RuntimeOutcomeCount{{Outcome: "conflicting", Requests: 1}, {Outcome: "context_done_unresolved", Requests: 1}, {Outcome: "local_delivery_completed", Requests: 1}, {Outcome: "missing_predecessor", Requests: 1}, {Outcome: "output_held", Requests: 1}, {Outcome: "technical_failure", Requests: 1}, {Outcome: "unresolved", Requests: 1}}
	if s.Requests != 7 || !reflect.DeepEqual(s.Outcomes, want) || s.DiagnosticFailure.Numerator != 1 || s.DiagnosticFailure.Denominator != 2 || s.DiagnosticFailure.ExcludedRequests != 5 {
		t.Fatal(s)
	}
}
