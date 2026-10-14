package api_test

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workevent"
)

func TestWorkActivityScopedHTTPAndRuntimeRoles(t *testing.T) {
	server, admin, _, _, _ := startWorkbenchProductionFixture(t, true)
	var databaseName string
	if err := admin.QueryRow(`SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(strings.TrimSpace(os.Getenv("XINSIGHT_POSTGRES_DSN")))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + databaseName
	owner, err := db.OpenSourceFactsStore(u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	reporterID, collectionID := randomTestUUID(t), randomTestUUID(t)
	if _, err := admin.Exec(`INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1::uuid,$2,'agent_runtime','Synthetic agent')`, reporterID, "agent-runtime:"+reporterID); err != nil {
		t.Fatal(err)
	}
	scope := workevent.Scope{SourceSystemID: reporterID, SourceInstance: "agent-local", OrgID: "7", ProjectID: "9"}
	collection := db.WorkEventCollection{ID: collectionID, Scope: scope, Profile: workevent.CollectionProfile, ScopeGeneration: "1", Status: "staged", Revision: 1}
	if _, err := owner.RegisterWorkEventCollection(t.Context(), collection); err != nil {
		t.Fatal(err)
	}
	collection, err = owner.ActivateWorkEventCollection(t.Context(), collectionID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.BindReviewCaptureProject(t.Context(), reporterID, scope.SourceInstance, scope.OrgID, scope.ProjectID,
		randomTestUUID(t), "Finance review", "xs_admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	u.User = workbenchRuntimeRoleUser(t, "importer")
	importer, err := db.OpenSourceFactsStore(u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer importer.Close()
	now := time.Now().UTC().Truncate(time.Microsecond)
	event := workevent.Event{SchemaVersion: workevent.EventVersion, Reporter: scope, EventRef: "tool-observation-1", Revision: 1,
		OccurredAt: now.Add(-time.Minute), RecordedAt: now, SubjectKind: "resource_action", LifecycleState: "completed", RunID: "run-1", ToolCallID: "tool-1",
		Actor: workevent.Actor{Kind: "agent", Ref: "agent-1"}, ActionKind: "resource.read",
		Resource:     &workevent.Resource{Ref: "resource-1", NameState: "readable", Name: "report.csv"},
		ContentState: "not_captured", ContentRefs: []workevent.ContentRef{}}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	object, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = connector.CanonicalizeJSONValue(object)
	if err != nil {
		t.Fatal(err)
	}
	if inserted, err := importer.CommitWorkEvent(t.Context(), collection, raw, now); err != nil || !inserted {
		t.Fatalf("restricted importer commit: %v %v", inserted, err)
	}
	if _, err := admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_resource) VALUES('xs_8899aabbccddeeff',$1::uuid,'project-9',true)`, reporterID); err != nil {
		t.Fatal(err)
	}
	client := responsibilityLogin(t, &responsibilityFixture{server: server}, "scoped.reviewer")
	other := responsibilityLogin(t, &responsibilityFixture{server: server}, "scoped.other")
	status, body := client.call(t, "GET", "/api/v1/work-activity/scopes", nil, nil)
	var choices db.WorkEventScopeChoice
	var scopeList struct {
		Items []db.WorkEventScopeChoice `json:"items"`
	}
	if err := json.Unmarshal(body, &scopeList); status != 200 || err != nil || len(scopeList.Items) != 1 || scopeList.Items[0].SourceID != reporterID {
		t.Fatalf("authorized scope discovery: %d %+v %v", status, scopeList, err)
	}
	choices = scopeList.Items[0]
	if choices.ProjectID != "9" || choices.SourceInstance != "agent-local" || choices.ProjectLabel != "Finance review" {
		t.Fatalf("scope discovery lost project context: %+v", choices)
	}
	status, body = other.call(t, "GET", "/api/v1/work-activity/scopes", nil, nil)
	if err := json.Unmarshal(body, &scopeList); status != 200 || err != nil || len(scopeList.Items) != 0 {
		t.Fatalf("unauthorized scope discovery: %d %+v %v", status, scopeList, err)
	}
	query := url.Values{"source_id": {reporterID}, "project_id": {"9"}, "from": {now.Add(-time.Hour).Format(time.RFC3339)}, "to": {now.Add(time.Second).Format(time.RFC3339)}}
	query.Set("resource_name", "report")
	listPath := "/api/v1/work-activity?" + query.Encode()
	status, body = client.call(t, "GET", listPath, nil, nil)
	if status != 200 {
		t.Fatalf("authorized list: %d %s", status, body)
	}
	var page db.WorkEventPage
	if err := json.Unmarshal(body, &page); err != nil || len(page.Items) != 1 || page.Items[0].Event.EventRef != event.EventRef {
		t.Fatalf("authorized list: %+v %v", page, err)
	}
	detailPath := "/api/v1/work-activity/events/" + reporterID + "/agent-local/7/9/" + event.EventRef
	status, body = client.call(t, "GET", detailPath, nil, nil)
	var detail db.WorkEventDetail
	if err := json.Unmarshal(body, &detail); status != 200 || err != nil || detail.Item.Event.EventRef != event.EventRef {
		t.Fatalf("authorized detail: %d %+v %v", status, detail, err)
	}
	var legacy map[string]json.RawMessage
	if err := json.Unmarshal(body, &legacy); err != nil || legacy["run_summary"] != nil || legacy["run_cutoff"] != nil || legacy["run_items_next_cursor"] != nil || detail.Version != "xinsight.work-activity-detail.v1" {
		t.Fatalf("default detail changed the closed v1 response: %d %s %v", status, body, err)
	}
	status, body = client.call(t, "GET", detailPath+"?view=reading_v2", nil, nil)
	if err := json.Unmarshal(body, &detail); status != 200 || err != nil || detail.Version != "xinsight.work-activity-detail.v2" ||
		detail.RunSummary == nil || detail.RunSummary.ResourceReadOutcomes.Completed != 1 || detail.RunSummary.DistinctResources != 1 || detail.RunCutoff == nil {
		t.Fatalf("explicit reading detail: %d %+v %v", status, detail, err)
	}
	summaryQuery := url.Values{"from": query["from"], "to": query["to"], "source_ids": {reporterID}, "project_ids": {"9"}}
	status, body = client.call(t, "GET", "/api/v1/work-activity/summary?"+summaryQuery.Encode(), nil, nil)
	if err := json.Unmarshal(body, &legacy); status != 200 || err != nil || legacy["resource_read_outcomes"] != nil || legacy["resource_change_outcomes"] != nil {
		t.Fatalf("default summary changed the closed v1 response: %d %s %v", status, body, err)
	}
	summaryQuery.Set("view", "reading_v2")
	status, body = client.call(t, "GET", "/api/v1/work-activity/summary?"+summaryQuery.Encode(), nil, nil)
	var reading db.WorkActivitySummary
	if err := json.Unmarshal(body, &reading); status != 200 || err != nil || reading.Version != "xinsight.work-activity-summary.v2" ||
		reading.ResourceReadOutcomes.Completed != 1 {
		t.Fatalf("explicit reading summary: %d %+v %v", status, reading, err)
	}
	if status, _ = client.call(t, "GET", detailPath+"?run_cursor=invalid", nil, nil); status != 400 {
		t.Fatalf("run cursor without reading view: %d", status)
	}
	if status, _ = client.call(t, "GET", detailPath+"?view=unknown", nil, nil); status != 400 {
		t.Fatalf("unknown reading view: %d", status)
	}
	if status, _ = other.call(t, "GET", listPath, nil, nil); status != 403 {
		t.Fatalf("unauthorized list: %d", status)
	}
	if status, _ = other.call(t, "GET", detailPath, nil, nil); status != 404 {
		t.Fatalf("unauthorized detail: %d", status)
	}
	if _, err := admin.Exec(`UPDATE workbench_access_grants SET active=false WHERE subject_id='xs_8899aabbccddeeff' AND source_system_id=$1::uuid`, reporterID); err != nil {
		t.Fatal(err)
	}
	if status, _ = client.call(t, "GET", detailPath, nil, nil); status != 404 {
		t.Fatalf("revoked detail: %d", status)
	}
}
