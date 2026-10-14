package api_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

func TestPurposeProductionRolesAndAuthorization(t *testing.T) {
	server, admin, _, _, _ := startWorkbenchProductionFixture(t, true)
	f := &responsibilityFixture{server: server}
	purpose := responsibilityLogin(t, f, "purpose.reviewer")
	caseReviewer := responsibilityLogin(t, f, "scoped.reviewer")
	reader := responsibilityLogin(t, f, "scoped.reader")
	money := responsibilityLogin(t, f, "legacy.money")
	if _, err := admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_summary,allow_details,allow_coverage,allow_preferences,allow_source_context,allow_source_identity,allow_source_cost,allow_purpose_read,allow_purpose_review)
SELECT s,source_system_id,source_project_ref,true,true,true,true,true,true,true,true,true FROM workbench_access_grants CROSS JOIN (VALUES ('xs_3333333333333333'),('xs_2222222222222222')) v(s) WHERE subject_id='xs_8899aabbccddeeff';
UPDATE workbench_access_grants SET allow_purpose_read=true,allow_purpose_review=true WHERE subject_id IN ('xs_8899aabbccddeeff','xs_6666666666666666')`); err != nil {
		t.Fatal(err)
	}
	pace := time.NewTicker(60 * time.Millisecond)
	defer pace.Stop()
	call := func(c *responsibilityClient, method, path string, body any, headers map[string]string, want int) []byte {
		t.Helper()
		<-pace.C
		status, raw := c.call(t, method, path, body, headers)
		if status != want {
			t.Fatalf("%s %s = %d want %d: %s", method, path, status, want, raw)
		}
		return raw
	}
	var execution, event, hash, otherEvent, otherHash string
	if err := admin.QueryRow(`SELECT execution_id,evidence_event_id::text,event_hash FROM workbench_economic_facts WHERE request_id='req-browser-workbench-01'`).Scan(&execution, &event, &hash); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(`SELECT evidence_event_id::text,event_hash FROM workbench_economic_facts WHERE request_id='req-browser-workbench-02'`).Scan(&otherEvent, &otherHash); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/workbench/executions/" + execution + "/purpose"
	decode := func(raw []byte) workbench.PurposeView {
		t.Helper()
		var v workbench.PurposeView
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := decode(call(purpose, "GET", path, nil, nil, 200)); !v.CanReview || v.State.Status != "unknown" {
		t.Fatalf("purpose role: %+v", v)
	}
	command := workbench.PurposeCommand{Action: "declare", ExpectedRevision: 0, IdempotencyKey: "native-api-declare", Reason: "Synthetic human request purpose", Category: "software_check", Vocabulary: workbench.PurposeVocabulary, Basis: []workbench.PurposeBasis{{EvidenceID: event, EventHash: hash}}}
	for _, c := range []*responsibilityClient{caseReviewer, reader, money} {
		if v := decode(call(c, "GET", path, nil, nil, 200)); v.CanReview {
			t.Fatal("existing role inherited purpose review")
		}
		call(c, "POST", path, command, nil, 403)
	}
	for _, headers := range []map[string]string{{"X-CSRF-Token": ""}, {"Origin": "https://unrelated.invalid"}} {
		call(purpose, "POST", path, command, headers, 403)
	}
	for _, target := range []string{"/api/v1/saved-views", "/api/v1/cases", "/api/v1/security/reviews"} {
		call(purpose, "POST", target, map[string]any{}, nil, 403)
	}
	for _, change := range []func(map[string]any){func(v map[string]any) { delete(v, "expected_revision") }, func(v map[string]any) { v["expected_revision"] = nil }, func(v map[string]any) { v["actual_actor"] = "forged" }, func(v map[string]any) { v["category"] = "customer_service" }, func(v map[string]any) { v["claim_id"] = "" }, func(v map[string]any) { v["reject_claim_ids"] = []string{} }} {
		b, _ := json.Marshal(command)
		var v map[string]any
		json.Unmarshal(b, &v)
		change(v)
		call(purpose, "POST", path, v, nil, 400)
	}
	bad := command
	bad.IdempotencyKey = "hidden-basis"
	bad.Basis = []workbench.PurposeBasis{{EvidenceID: otherEvent, EventHash: otherHash}}
	call(purpose, "POST", path, bad, nil, 403)
	var created workbench.PurposeEvent
	if err := json.Unmarshal(call(purpose, "POST", path, command, nil, 201), &created); err != nil {
		t.Fatal(err)
	}
	var replay workbench.PurposeEvent
	json.Unmarshal(call(purpose, "POST", path, command, nil, 201), &replay)
	if created.Hash != replay.Hash || created.State.Claims[0].DeclaredBy != "xs_3333333333333333" || created.State.Claims[0].Method != "human_declared" {
		t.Fatal("replay or human attribution changed")
	}
	command = workbench.PurposeCommand{Action: "accept", ExpectedRevision: 1, ClaimID: created.NewClaimID, IdempotencyKey: "native-api-accept", Reason: "Explicit review"}
	call(purpose, "POST", path, command, nil, 201)
	readRequest := json.RawMessage(`{"contract_version":"xinsight.workbench.v1","page":"cost","metric_version":"xinsight.workbench.cost.v1","period":{"relative_hours":24,"display_timezone":"UTC"},"money_basis":"gateway_charged","currency":"CNY","filters":{"source_ids":[],"source_projects":[],"cost_projects":[]}}`)
	var reading workbench.Reading
	json.Unmarshal(call(money, "POST", "/api/v1/workbench/readings", readRequest, nil, 201), &reading)
	if reading.ID == "" || reading.Purpose == nil || reading.Purpose.Status != "available" {
		t.Fatalf("legacy economic reader: %+v", reading)
	}
	frozen := decode(call(money, "GET", path+"?reading_id="+reading.ID, nil, nil, 200))
	if frozen.CanReview || frozen.State.Revision != 2 {
		t.Fatal("frozen purpose interpretation", frozen)
	}
	var revision int64
	if err := admin.QueryRow(`SELECT revision FROM workbench_access_grants WHERE subject_id='xs_2222222222222222'`).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`UPDATE workbench_access_grants SET allow_purpose_read=false,allow_purpose_review=false WHERE subject_id='xs_2222222222222222'`); err != nil {
		t.Fatal(err)
	}
	var next int64
	admin.QueryRow(`SELECT revision FROM workbench_access_grants WHERE subject_id='xs_2222222222222222'`).Scan(&next)
	if next != revision+1 {
		t.Fatal("grant bits did not advance revision")
	}
	if raw := call(money, "GET", "/api/v1/workbench/readings/"+reading.ID+"/summary", nil, nil, 409); !strings.Contains(string(raw), "workbench_access_changed") {
		t.Fatal(string(raw))
	}
	call(money, "GET", path, nil, nil, 403)
	var current workbench.Reading
	json.Unmarshal(call(money, "POST", "/api/v1/workbench/readings", readRequest, nil, 201), &current)
	if current.Purpose != nil {
		t.Fatal("purpose available after revocation")
	}
	call(money, "GET", "/api/v1/workbench/readings/"+current.ID+"/summary", nil, nil, 200)
	t.Log("formal purpose role, independent Case rights, actual CSRF/origin, strict command fields, hidden basis, immutable retry, economic reader and automatic grant invalidation verified")
}
