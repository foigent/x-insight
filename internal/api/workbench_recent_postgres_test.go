package api_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

// The ordinary scoped reviewer reaches the new route through the production
// scoped router and receives only members authorized by the existing grant.
func TestScopedReviewerRecentRequestsRoute(t *testing.T) {
	server, admin, _, source, _ := startWorkbenchProductionFixture(t, true)
	client := responsibilityLogin(t, &responsibilityFixture{server: server}, "scoped.reviewer")
	other := responsibilityLogin(t, &responsibilityFixture{server: server}, "scoped.other")
	request := httpReadingRequest(source)
	end := time.Now().UTC().Add(-time.Minute)
	start := end.Add(-24 * time.Hour)
	request.Period = workbench.PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "UTC"}
	status, raw := client.call(t, "POST", "/api/v1/workbench/readings", request, nil)
	if status != 201 {
		t.Fatalf("create scoped reading: %d %s", status, raw)
	}
	var reading workbench.Reading
	if err := json.Unmarshal(raw, &reading); err != nil || reading.ID == "" {
		t.Fatalf("decode reading: %+v %v", reading, err)
	}
	path := "/api/v1/workbench/readings/" + reading.ID + "/recent-requests?limit=10"
	status, raw = client.call(t, "GET", path, nil, nil)
	if status != 200 {
		t.Fatalf("scoped recent route: %d %s", status, raw)
	}
	var page workbench.RecentRequests
	if err := json.Unmarshal(raw, &page); err != nil || len(page.Items) != 1 || page.Items[0].RequestID != "req-browser-workbench-01" || page.Items[0].InvestigationAvailable == nil || !*page.Items[0].InvestigationAvailable {
		t.Fatalf("scope, member, or execution navigation: %+v %v", page, err)
	}
	status, raw = other.call(t, "GET", path, nil, nil)
	if status != 404 {
		t.Fatalf("cross-owner recent route: %d %s", status, raw)
	}
	if _, err := admin.Exec(`UPDATE workbench_access_grants SET allow_details=false WHERE subject_id='xs_8899aabbccddeeff'`); err != nil {
		t.Fatal(err)
	}
	status, raw = client.call(t, "GET", path, nil, nil)
	if status != 409 {
		t.Fatalf("grant change should invalidate reading: %d %s", status, raw)
	}
}
