package api_test

import (
	"encoding/json"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workspace"
	"strings"
	"testing"
	"time"
)

// Real TLS/session/CSRF/scope middleware and native runtime roles. The existing
// fixture owns its isolated PG database; no edits to the browser harness.
func TestReviewExecutionContextHTTPRejectsInvalidAndUnauthorizedWithoutWrites(t *testing.T) {
	server, admin, _, source, _ := startWorkbenchProductionFixture(t, true)
	f := &responsibilityFixture{server: server}
	reviewer := responsibilityLogin(t, f, "scoped.reviewer")
	reader := responsibilityLogin(t, f, "scoped.reader")
	var event, execution, hash, sourceRef string
	if e := admin.QueryRow(`SELECT evidence_event_id::text,execution_id,event_hash,source_ref FROM workbench_economic_facts WHERE request_id='req-browser-workbench-01' ORDER BY occurred_at LIMIT 1`).Scan(&event, &execution, &hash, &sourceRef); e != nil {
		t.Fatal(e)
	}
	scope := workbench.ResourceScope{SourceSystemID: source, SourceInstance: "synthetic", OrgID: "17", ProjectID: "101"}
	c := workspace.ReviewCreationCommand{Operation: "start_review", OperationID: randomTestUUID(t), NewCase: &workspace.CreateCaseInput{Title: "Context HTTP boundary"}, Finding: workspace.CreateFindingInput{Title: "Claim", Statement: "Bounded claim", Classification: "hypothesis"}, Selection: workspace.BasisSelection{Evidence: []workspace.EvidenceChoice{{Scope: "finding", EvidenceEventID: event, EventHash: hash, SourceRef: sourceRef, Relation: "supports"}}}, Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Read selected evidence"}, Reason: "Start", ExpectedBasisDigest: "sha256:" + strings.Repeat("a", 64)}
	context := workspace.ExecutionContext{WorkbenchReadingID: "xwr_" + strings.Repeat("a", 22), ExecutionID: execution, ExecutionAsOf: time.Now().UTC().Truncate(time.Microsecond), RuntimeReadingID: "xrt_" + strings.Repeat("b", 22), RuntimeRequestRef: "xrr_" + strings.Repeat("c", 64)}
	c.ExecutionContext = &context
	snapshot := func() string {
		t.Helper()
		var parts []string
		for _, table := range []string{"investigation_cases", "case_findings", "case_executions", "case_resource_links", "case_evidence_links", "case_gap_links", "case_finding_assessments", "case_finding_assessment_actions", "finding_review_creations", "investigation_case_scope_labels"} {
			var s string
			if e := admin.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM ` + table + ` t`).Scan(&s); e != nil {
				t.Fatal(e)
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, "\n")
	}
	before := snapshot()
	pace := time.NewTicker(60 * time.Millisecond)
	defer pace.Stop()
	must := func(client *responsibilityClient, path string, body any, headers map[string]string, want int) []byte {
		t.Helper()
		<-pace.C
		status, raw := client.call(t, "POST", path, body, headers)
		if status != want {
			t.Fatalf("HTTP status=%d want=%d response=%s", status, want, raw)
		}
		if want >= 400 && snapshot() != before {
			t.Fatal("negative HTTP request wrote partial state")
		}
		return raw
	}
	for _, path := range []string{"/api/v1/security/reviews/preview", "/api/v1/security/reviews"} {
		must(reviewer, path, c, nil, 400) // context requires explicitly selected resources
		wire, _ := json.Marshal(c)
		must(reviewer, path, []byte(strings.Replace(string(wire), `"execution_context":`, `"execution_context":null,"execution_context":`, 1)), nil, 400)
		must(reviewer, path, []byte(strings.Replace(string(wire), `"runtime_request_ref":`, `"unknown_context_field":true,"runtime_request_ref":`, 1)), nil, 400)
		must(reviewer, path, c, map[string]string{"X-CSRF-Token": ""}, 403)
	}
	missing := c
	missing.Selection = workspace.BasisSelection{Resources: []workspace.ResourceChoice{{Scope: "finding", Reporter: scope, Target: scope, RequestID: "unavailable", RecordRef: "unavailable", Revision: 1, RecordHash: "sha256:" + strings.Repeat("d", 64), Relation: "supports"}}}
	must(reviewer, "/api/v1/security/reviews/preview", missing, nil, 404)
	must(reviewer, "/api/v1/security/reviews", missing, nil, 404)
	must(reader, "/api/v1/security/reviews", missing, nil, 403)
	// The existing no-context command remains valid through the same handler.
	c.ExecutionContext = nil
	c.ExpectedBasisDigest = ""
	var b workspace.ReviewBasis
	if e := json.Unmarshal(must(reviewer, "/api/v1/security/reviews/preview", c, nil, 200), &b); e != nil {
		t.Fatal(e)
	}
	if b.ResolvedExecutionContext != nil {
		t.Fatal("legacy preview invented context")
	}
	c.ExpectedBasisDigest = b.Digest
	var receipt workspace.ReviewCreationReceipt
	if e := json.Unmarshal(must(reviewer, "/api/v1/security/reviews", c, nil, 200), &receipt); e != nil {
		t.Fatal(e)
	}
	if receipt.Command.ExecutionContext != nil || receipt.Basis.ResolvedExecutionContext != nil {
		t.Fatal("omitted context not retained")
	}
	var links int
	if e := admin.QueryRow(`SELECT count(*) FROM case_executions WHERE case_id=$1`, receipt.CaseID).Scan(&links); e != nil || links != 0 {
		t.Fatal("implicit execution relation", links, e)
	}
}

// Use the production TLS/session/CSRF handler and native database roles without
// modifying the shared browser harness or replacing its real clock.
func TestReviewExecutionContextCostReadingHTTPPrecision(t *testing.T) {
	server, admin, _, source, _ := startWorkbenchProductionFixture(t, true)
	client := responsibilityLogin(t, &responsibilityFixture{server: server}, "scoped.reviewer")
	var execution string
	if err := admin.QueryRow(`SELECT execution_id FROM workbench_economic_facts WHERE source_system_id=$1 ORDER BY occurred_at LIMIT 1`, source).Scan(&execution); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"relative", "absolute"} {
		t.Run(mode, func(t *testing.T) {
			request := httpReadingRequest(source)
			start := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second).Add(123456789 * time.Nanosecond)
			end := start.Add(time.Hour + 500*time.Nanosecond)
			if mode == "absolute" {
				request.Period = workbench.PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "Asia/Shanghai"}
			}
			status, body := client.call(t, "POST", "/api/v1/workbench/readings", request, nil)
			if status != 201 {
				t.Fatalf("POST status=%d body=%s", status, body)
			}
			var created, read workbench.Reading
			if err := json.Unmarshal(body, &created); err != nil {
				t.Fatal(err)
			}
			status, body = client.call(t, "GET", "/api/v1/workbench/readings/"+created.ID, nil, nil)
			if status != 200 {
				t.Fatalf("GET status=%d body=%s", status, body)
			}
			if err := json.Unmarshal(body, &read); err != nil {
				t.Fatal(err)
			}
			for _, pair := range [][2]time.Time{{created.Cutoff, read.Cutoff}, {created.ExpiresAt, read.ExpiresAt}, {created.Period.StartInclusive, read.Period.StartInclusive}, {created.Period.EndExclusive, read.Period.EndExclusive}} {
				if !pair[0].Equal(pair[1]) || !pair[0].Equal(pair[0].Truncate(time.Microsecond)) {
					t.Fatalf("POST/GET precision mismatch: %v", pair)
				}
			}
			if created.ExpiresAt.Sub(created.Cutoff) != workbench.ReadingTTL || created.Period.Timezone != read.Period.Timezone || created.MembershipRef != read.MembershipRef {
				t.Fatal("frozen reading changed", created, read)
			}
			if mode == "absolute" && (!created.Period.StartInclusive.Equal(start.Truncate(time.Microsecond)) || !created.Period.EndExclusive.Equal(end.Truncate(time.Microsecond)) || created.Period.Timezone != "Asia/Shanghai") {
				t.Fatal("absolute period changed", created.Period)
			}
			if mode == "relative" && (!created.Period.EndExclusive.Equal(created.Cutoff) || created.Period.EndExclusive.Sub(created.Period.StartInclusive) != 24*time.Hour) {
				t.Fatal("relative period changed", created.Period)
			}
			if _, err := workspace.NormalizeExecutionContext(workspace.ExecutionContext{WorkbenchReadingID: created.ID, ExecutionID: execution, ExecutionAsOf: created.Cutoff, RuntimeReadingID: "xrt_" + strings.Repeat("b", 22), RuntimeRequestRef: "xrr_" + strings.Repeat("c", 64)}); err != nil {
				t.Fatal("POST cutoff invalid for context", err)
			}
		})
	}
	t.Run("collapsed_period_without_writes", func(t *testing.T) {
		snapshot := func() string {
			t.Helper()
			var state string
			if err := admin.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text FROM workbench_readings r`).Scan(&state); err != nil {
				t.Fatal(err)
			}
			return state
		}
		before := snapshot()
		request := httpReadingRequest(source)
		start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second).Add(100 * time.Nanosecond)
		end := start.Add(500 * time.Nanosecond)
		request.Period = workbench.PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "UTC"}
		if _, err := request.Resolve(time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		status, body := client.call(t, "POST", "/api/v1/workbench/readings", request, nil)
		if status != 400 {
			t.Fatalf("collapsed POST status=%d body=%s", status, body)
		}
		if snapshot() != before {
			t.Fatal("collapsed POST wrote reading")
		}
	})
}
