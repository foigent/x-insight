package api_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/foigent/x-insight/internal/db"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/investigation"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestScopedPersonalFilterLifecycle(t *testing.T) {
	server, _, _, _, _ := startWorkbenchProductionFixture(t, true)
	f := &responsibilityFixture{server: server}
	clients := []*responsibilityClient{responsibilityLogin(t, f, "scoped.reviewer"), responsibilityLogin(t, f, "scoped.reader")}
	pace := time.NewTicker(60 * time.Millisecond)
	defer pace.Stop()
	call := func(t *testing.T, c *responsibilityClient, method, path string, body any, headers map[string]string, want int) []byte {
		t.Helper()
		<-pace.C
		status, raw := c.call(t, method, path, body, headers)
		if status != want {
			t.Fatalf("%s %s = %d want %d: %s", method, path, status, want, raw)
		}
		return raw
	}
	const collection = "/api/v1/investigation/queue-filters"
	for i, c := range clients {
		t.Run([]string{"reviewer", "reader"}[i], func(t *testing.T) {
			command := workspace.QueueFilterCommand{OperationID: randomTestUUID(t), Name: "Personal investigation", Schema: workspace.QueueFilterSchema, Filters: workspace.QueueFilters{Mode: "current", Period: "7d", Sort: "due_asc"}}
			decode := func(raw []byte) workspace.QueueFilter {
				t.Helper()
				var item workspace.QueueFilter
				if err := json.Unmarshal(raw, &item); err != nil {
					t.Fatal(err)
				}
				return item
			}
			created := decode(call(t, c, "POST", collection, command, nil, 201))
			if created.Revision != 1 || created.ID == "" || created.Deleted {
				t.Fatalf("create: %+v", created)
			}
			path := collection + "/" + created.ID
			command.OperationID, command.ExpectedRevision, command.Name = randomTestUUID(t), 1, "Updated personal investigation"
			for _, method := range []string{"PUT", "DELETE"} {
				call(t, clients[1-i], method, path, command, nil, 404)
				call(t, c, method, path, command, map[string]string{"X-CSRF-Token": ""}, 403)
				call(t, c, method, path, command, map[string]string{"Origin": "https://unrelated.invalid"}, 403)
			}
			updated := decode(call(t, c, "PUT", path, command, nil, 200))
			if updated.Revision != 2 || updated.Name != command.Name || updated.OwnerSubject != created.OwnerSubject {
				t.Fatalf("update: %+v", updated)
			}
			if replay := decode(call(t, c, "PUT", path, command, nil, 200)); !workspace.SameQueueValue(replay, updated) {
				t.Fatal("update replay changed the saved revision")
			}
			command.OperationID = randomTestUUID(t)
			call(t, c, "PUT", path, command, nil, 409)
			call(t, c, "DELETE", path, command, nil, 409)
			var list struct {
				Items []workspace.QueueFilter `json:"items"`
			}
			if err := json.Unmarshal(call(t, c, "GET", collection, nil, nil, 200), &list); err != nil || len(list.Items) != 1 || list.Items[0].ID != created.ID || list.Items[0].Revision != 2 {
				t.Fatalf("conflict recovery list: %+v %v", list, err)
			}
			command.OperationID, command.ExpectedRevision = randomTestUUID(t), list.Items[0].Revision
			recovered := decode(call(t, c, "PUT", path, command, nil, 200))
			if recovered.Revision != 3 {
				t.Fatal("update recovery revision", recovered.Revision)
			}
			command.OperationID, command.ExpectedRevision = randomTestUUID(t), recovered.Revision
			deleted := decode(call(t, c, "DELETE", path, command, nil, 200))
			if !deleted.Deleted || deleted.Revision != 4 {
				t.Fatalf("delete: %+v", deleted)
			}
			if replay := decode(call(t, c, "DELETE", path, command, nil, 200)); !workspace.SameQueueValue(replay, deleted) {
				t.Fatal("delete replay changed tombstone")
			}
			if err := json.Unmarshal(call(t, c, "GET", collection, nil, nil, 200), &list); err != nil || len(list.Items) != 0 {
				t.Fatalf("deleted preference still listed: %+v %v", list, err)
			}
		})
	}
	call(t, clients[1], "POST", "/api/v1/security/reviews", map[string]any{}, nil, 403)
	t.Log("formal scoped reviewer and reader: personal create/update/delete, ownership, CSRF/origin, conflicts, recovery, replay; reader review writes remain denied")
}

func TestScopedWorkbenchDocumentRoutes(t *testing.T) {
	server, _, _, _, _ := startWorkbenchProductionFixture(t, true)
	f := &responsibilityFixture{server: server}
	pace := time.NewTicker(60 * time.Millisecond)
	defer pace.Stop()
	for _, username := range []string{"scoped.reviewer", "scoped.reader"} {
		t.Run(username, func(t *testing.T) {
			client := responsibilityLogin(t, f, username)
			for _, method := range []string{"GET", "HEAD"} {
				for _, route := range []string{"/workbench/cost", "/workbench/cost?view=usage", "/workbench/unknown", "/workbench/cost/unknown", "/api/v1/workbench/unknown"} {
					<-pace.C
					status, raw := client.call(t, method, route, nil, nil)
					want := 404
					if route == "/workbench/cost" || route == "/workbench/cost?view=usage" {
						want = 200
					}
					if strings.HasPrefix(route, "/api/") {
						want = 403 // Scoped production authorization rejects unknown API paths before dispatch.
					}
					if status != want {
						t.Fatalf("%s %s status=%d want=%d body=%s", method, route, status, want, raw)
					}
					if method == "HEAD" && len(raw) != 0 {
						t.Fatalf("HEAD %s returned a body", route)
					}
					if method == "GET" && want == 200 && !strings.Contains(string(raw), `<html lang="en-US">`) {
						t.Fatalf("GET %s did not return the product shell", route)
					}
				}
			}
		})
	}
}

func TestScopedProductionAccountsReadReviewRevokeAndRoutes(t *testing.T) {
	server, admin, _, _, closeProduction := startWorkbenchProductionFixture(t, true)
	f := &responsibilityFixture{server: server}
	reviewer := responsibilityLogin(t, f, "scoped.reviewer")
	reader := responsibilityLogin(t, f, "scoped.reader")
	partial := responsibilityLogin(t, f, "scoped.partial")
	other := responsibilityLogin(t, f, "scoped.other")
	// Production admits 20 requests/sec; pace this sequential route matrix below that rate.
	pace := time.NewTicker(60 * time.Millisecond)
	defer pace.Stop()
	must := func(c *responsibilityClient, method, path string, body any, want int) []byte {
		t.Helper()
		<-pace.C
		status, raw := c.call(t, method, path, body, nil)
		if status != want {
			t.Fatalf("%s %s = %d want %d: %s", method, path, status, want, raw)
		}
		return raw
	}
	var session struct {
		ScopeRevision string `json:"scope_revision"`
	}
	if err := json.Unmarshal(must(reviewer, "GET", "/api/v1/access/session", nil, 200), &session); err != nil || session.ScopeRevision == "" {
		t.Fatal("missing scope revision", err)
	}
	var event, execution, hash string
	if err := admin.QueryRow(`SELECT evidence_event_id::text,execution_id,event_hash FROM workbench_economic_facts WHERE request_id='req-browser-workbench-01' ORDER BY occurred_at LIMIT 1`).Scan(&event, &execution, &hash); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Items []investigation.ExecutionSummary `json:"items"`
	}
	if err := json.Unmarshal(must(reviewer, "GET", "/api/v1/executions", nil, 200), &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("scope search: %+v %v", list, err)
	}
	detailPath := "/api/v1/executions/" + execution
	must(reviewer, "GET", detailPath, nil, 200)
	for _, suffix := range []string{"/timeline", "/investigation", "/gaps", "/trace-journey", "/events/" + event} {
		must(reviewer, "GET", detailPath+suffix, nil, 200)
	}
	var projected investigation.EvidenceEventDetail
	if err := json.Unmarshal(must(partial, "GET", detailPath+"/events/"+event, nil, 200), &projected); err != nil {
		t.Fatal(err)
	}
	if projected.ViewBoundary == nil || projected.Identity.ActorRef != "" || projected.Identity.ProjectID != "" || projected.Usage.CostMicros != "" || projected.Source.SourceRef != "" || projected.Source.EventHash != hash {
		t.Fatalf("field projection leaked or changed original hash: %+v", projected)
	}
	must(other, "GET", detailPath, nil, 404)
	for _, path := range []string{"/api/v1/posture", "/api/v1/connectors", "/api/v1/sessions", "/api/v1/alerts", "/api/v1/discovery/hypotheses", "/api/v1/saved-views", "/api/v1/service-identities", "/api/v1/executions/req-browser-workbench-01", "/api/v1/executions/" + execution + "/feedback", "/api/v1/cases/fake/evidence-pack.json", "/api/v1/future-route", "/api/v1/executions%2f" + execution} {
		must(reviewer, "GET", path, nil, 403)
	}
	must(reviewer, "POST", "/api/v1/cases", workspace.CreateCaseInput{Title: "empty"}, 403)
	command := workspace.ReviewCreationCommand{Operation: "start_review", OperationID: randomTestUUID(t), NewCase: &workspace.CreateCaseInput{Title: "Scoped production review"}, Finding: workspace.CreateFindingInput{Title: "Scoped basis", Statement: "Synthetic event review", Classification: "hypothesis"}, Selection: workspace.BasisSelection{Evidence: []workspace.EvidenceChoice{{Scope: "finding", EvidenceEventID: event, EventHash: hash, Relation: "supports"}}}, Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Reviewed the authorized event"}, Reason: "Start scoped review"}
	var basis workspace.ReviewBasis
	if err := json.Unmarshal(must(reviewer, "POST", "/api/v1/security/reviews/preview", command, 200), &basis); err != nil {
		t.Fatal(err)
	}
	command.ExpectedBasisDigest = basis.Digest
	for _, headers := range []map[string]string{{"X-CSRF-Token": ""}, {"X-CSRF-Token": "invalid"}, {"Origin": "https://unrelated.invalid"}} {
		<-pace.C
		status, _ := reviewer.call(t, "POST", "/api/v1/security/reviews", command, headers)
		if status != 403 {
			t.Fatal("scoped CSRF/origin boundary", status)
		}
	}
	must(reader, "POST", "/api/v1/security/reviews", command, 403)
	must(partial, "POST", "/api/v1/security/reviews/preview", command, 404)
	var receipt workspace.ReviewCreationReceipt
	if err := json.Unmarshal(must(reviewer, "POST", "/api/v1/security/reviews", command, 200), &receipt); err != nil || receipt.CaseID == "" {
		t.Fatalf("review: %v %+v", err, receipt)
	}
	casePath := "/api/v1/cases/" + receipt.CaseID
	must(reader, "GET", casePath, nil, 200)
	must(other, "GET", casePath, nil, 404)
	findingPath := casePath + "/findings/" + receipt.FindingID
	must(reader, "GET", findingPath+"/assessment", nil, 200)
	must(reviewer, "GET", findingPath+"/assessment/history?through_version=1", nil, 200)
	must(reader, "POST", findingPath+"/review-actions", map[string]any{}, 403)
	for _, suffix := range []string{"/seal", "/evidence-pack.json", "/evidence-pack.html"} {
		method := "GET"
		if suffix == "/seal" {
			method = "POST"
		}
		must(reviewer, method, casePath+suffix, nil, 403)
	}
	candidates := must(reviewer, "GET", fmt.Sprintf("/api/v1/security/assignee-candidates?case_id=%s&finding_id=%s", receipt.CaseID, receipt.FindingID), nil, 200)
	if strings.Contains(string(candidates), "scoped.other") || strings.Contains(string(candidates), "scoped.reader") || !strings.Contains(string(candidates), "scoped.reviewer") {
		t.Fatalf("directory scope: %s", candidates)
	}
	if _, err := admin.Exec(`UPDATE workbench_access_grants SET active=false WHERE subject_id='xs_8899aabbccddeeff'`); err != nil {
		t.Fatal(err)
	}
	must(reviewer, "GET", casePath, nil, 403)
	must(reviewer, "POST", "/api/v1/security/reviews", command, 403)
	must(reader, "GET", casePath, nil, 200)
	if _, err := admin.Exec(`UPDATE workbench_access_grants SET active=true WHERE subject_id='xs_8899aabbccddeeff'`); err != nil {
		t.Fatal(err)
	}
	must(reviewer, "POST", "/api/v1/security/reviews", command, 200)
	// A no-longer-authorized client revision cannot obtain or commit new data.
	status, _ := reviewer.call(t, "GET", detailPath, nil, map[string]string{"X-XInsight-Scope-Revision": "stale"})
	if status != 409 {
		t.Fatal("stale scope header", status)
	}
	closeProduction()
	// Exercise the actual native pools directly: production HTTP admission
	// deliberately rejects excess simultaneous reads before pool acquisition.
	var databaseName string
	if err := admin.QueryRow("SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{1, 2, 10} {
		u, err := url.Parse(os.Getenv("XINSIGHT_POSTGRES_DSN"))
		if err != nil {
			t.Fatal(err)
		}
		u.Path = "/" + databaseName
		u.User = workbenchRuntimeRoleUser(t, "reader")
		nativeReader, err := sql.Open("pgx", u.String())
		if err != nil {
			t.Fatal(err)
		}
		nativeReader.SetMaxOpenConns(size)
		u.User = workbenchRuntimeRoleUser(t, "workspace")
		nativeBusiness, err := sql.Open("pgx", u.String())
		if err != nil {
			t.Fatal(err)
		}
		nativeBusiness.SetMaxOpenConns(size)
		nativeStore := db.NewWorkspaceStore(nativeBusiness)
		scope := db.NewScopedWorkchainStore(db.NewEvidenceStore(nativeReader), nativeStore)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var group sync.WaitGroup
		results := make(chan error, 24)
		for range 24 {
			group.Add(1)
			go func() {
				defer group.Done()
				scoped, finish, err := scope.Begin(ctx, "xs_8899aabbccddeeff", true, func(f func() error) error { return f() })
				if err != nil {
					results <- err
					return
				}
				defer finish()
				_, err = nativeStore.GetCase(scoped, receipt.CaseID)
				results <- err
			}()
		}
		group.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatalf("native pools %d: %v", size, err)
			}
		}
		if nativeReader.Stats().InUse != 0 || nativeBusiness.Stats().InUse != 0 {
			t.Fatal("native pool leak")
		}
		cancel()
		nativeReader.Close()
		nativeBusiness.Close()
	}
	t.Logf("Formal v3 scoped reader/reviewer; native pools; source/project and field denials; atomic review/history/replay; revoke/restore; at %s", time.Now().UTC().Format(time.RFC3339))
}
