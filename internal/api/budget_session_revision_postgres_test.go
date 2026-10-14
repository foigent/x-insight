package api_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/foigent/x-insight/internal/workbench"
)

// Use the scope revision obtained by the browser's actual session endpoint.
// Omitting that header masks differences between session and budget identities.
func TestBudgetSessionRevisionMatchesReadingIdentity(t *testing.T) {
	t.Setenv("XINSIGHT_A0_RUNTIME_ROLES", "1")
	for _, username := range []string{"scoped.reviewer", "scoped.reader"} {
		for _, budgetOnly := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/budget_only_%t", username, budgetOnly), func(t *testing.T) {
				server, admin, _, source, _ := startWorkbenchProductionFixture(t, true)
				subject := "xs_8899aabbccddeeff"
				if username == "scoped.reader" {
					subject = "xs_6666666666666666"
				}
				if _, err := admin.Exec(`DELETE FROM workbench_access_grants WHERE subject_id=$1`, subject); err != nil {
					t.Fatal(err)
				}
				if _, err := admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_budget_summary) VALUES($1,$2,$3,$3,true)`, subject, source, !budgetOnly); err != nil {
					t.Fatal(err)
				}
				client := responsibilityLogin(t, &responsibilityFixture{server: server}, username)
				readRevision := func() string {
					t.Helper()
					status, raw := client.call(t, "GET", "/api/v1/access/session", nil, nil)
					var session struct {
						ScopeRevision string `json:"scope_revision"`
					}
					if status != 200 || json.Unmarshal(raw, &session) != nil || session.ScopeRevision == "" {
						t.Fatalf("session status=%d body=%s", status, raw)
					}
					return session.ScopeRevision
				}
				revision := readRevision()
				headers := map[string]string{"X-XInsight-Scope-Revision": revision}
				body := workbench.BudgetReadingRequest{ContractVersion: workbench.BudgetContractVersion}
				path := "/api/v1/workbench/budgets/readings"
				status, raw := client.call(t, "POST", path, body, headers)
				var reading workbench.BudgetReading
				if status != 201 || json.Unmarshal(raw, &reading) != nil || reading.ID == "" {
					t.Fatalf("current session revision rejected: status=%d body=%s", status, raw)
				}
				for _, suffix := range []string{"", "/items"} {
					status, raw = client.call(t, "GET", path+"/"+reading.ID+suffix, nil, headers)
					if status != 200 {
						t.Fatalf("budget read status=%d body=%s", status, raw)
					}
				}
				var before, after int
				if err := admin.QueryRow(`SELECT count(*) FROM budget_readings WHERE owner_subject=$1`, subject).Scan(&before); err != nil {
					t.Fatal(err)
				}
				status, raw = client.call(t, "POST", path, body, map[string]string{"X-XInsight-Scope-Revision": "stale"})
				if status != 409 {
					t.Fatalf("stale revision status=%d body=%s", status, raw)
				}
				if err := admin.QueryRow(`SELECT count(*) FROM budget_readings WHERE owner_subject=$1`, subject).Scan(&after); err != nil || before != after {
					t.Fatal("stale request created a reading", err)
				}
				// Another legal budget subset changes the shared identity. Old identity
				// must fail, and the freshly read identity must work without a retry loop.
				if _, err := admin.Exec(`UPDATE workbench_access_grants SET allow_budget_summary=false,allow_budget_alert=true WHERE subject_id=$1`, subject); err != nil {
					t.Fatal(err)
				}
				status, raw = client.call(t, "POST", path, body, headers)
				if status != 409 {
					t.Fatalf("changed grant accepted old revision: %d %s", status, raw)
				}
				next := readRevision()
				if next == revision {
					t.Fatal("grant change retained session revision")
				}
				status, raw = client.call(t, "POST", path, body, map[string]string{"X-XInsight-Scope-Revision": next})
				if status != 201 {
					t.Fatalf("fresh identity rejected: %d %s", status, raw)
				}
			})
		}
	}
}
