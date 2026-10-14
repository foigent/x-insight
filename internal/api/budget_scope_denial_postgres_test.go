package api_test

import (
	"encoding/json"
	"testing"

	"github.com/foigent/x-insight/internal/workbench"
)

func TestBudgetRevocationSettlesWithoutReadOrWrite(t *testing.T) {
	t.Setenv("XINSIGHT_A0_RUNTIME_ROLES", "1")
	for _, username := range []string{"scoped.reviewer", "scoped.reader"} {
		for _, keepCost := range []bool{false, true} {
			name := username + "/empty"
			if keepCost {
				name = username + "/cost_only"
			}
			t.Run(name, func(t *testing.T) {
				server, admin, _, source, _ := startWorkbenchProductionFixture(t, true)
				subject := "xs_8899aabbccddeeff"
				if username == "scoped.reader" {
					subject = "xs_6666666666666666"
				}
				if _, err := admin.Exec(`DELETE FROM workbench_access_grants WHERE subject_id=$1`, subject); err != nil {
					t.Fatal(err)
				}
				if _, err := admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_budget_summary) VALUES($1,$2,$3,$3,true)`, subject, source, keepCost); err != nil {
					t.Fatal(err)
				}
				client := responsibilityLogin(t, &responsibilityFixture{server: server}, username)
				fresh := func() map[string]string {
					t.Helper()
					status, raw := client.call(t, "GET", "/api/v1/access/session", nil, nil)
					var session struct {
						Revision string `json:"scope_revision"`
					}
					if status != 200 || json.Unmarshal(raw, &session) != nil || session.Revision == "" {
						t.Fatalf("session: %d %s", status, raw)
					}
					return map[string]string{"X-XInsight-Scope-Revision": session.Revision}
				}
				path := "/api/v1/workbench/budgets/readings"
				body := workbench.BudgetReadingRequest{ContractVersion: workbench.BudgetContractVersion}
				old := fresh()
				status, raw := client.call(t, "POST", path, body, old)
				var reading workbench.BudgetReading
				if status != 201 || json.Unmarshal(raw, &reading) != nil {
					t.Fatalf("positive: %d %s", status, raw)
				}
				if _, err := admin.Exec(`UPDATE workbench_access_grants SET active=$2,allow_budget_summary=NOT $2 WHERE subject_id=$1`, subject, keepCost); err != nil {
					t.Fatal(err)
				}
				status, _ = client.call(t, "POST", path, body, old)
				if status != 409 {
					t.Fatalf("old identity accepted: %d", status)
				}
				current := fresh()
				if !keepCost && current["X-XInsight-Scope-Revision"] != "no_authorized_scope" {
					t.Fatal("empty identity changed contract")
				}
				var before, after int
				if err := admin.QueryRow(`SELECT count(*) FROM budget_readings WHERE owner_subject=$1`, subject).Scan(&before); err != nil {
					t.Fatal(err)
				}
				for _, suffix := range []string{"", "/" + reading.ID, "/" + reading.ID + "/items"} {
					method := "GET"
					var request any
					if suffix == "" {
						method, request = "POST", body
					}
					status, raw = client.call(t, method, path+suffix, request, current)
					var problem map[string]any
					if status != 403 || json.Unmarshal(raw, &problem) != nil || problem["code"] != "workbench_scope_denied" {
						t.Fatalf("denial did not settle: %d %s", status, raw)
					}
				}
				if err := admin.QueryRow(`SELECT count(*) FROM budget_readings WHERE owner_subject=$1`, subject).Scan(&after); err != nil || after != before {
					t.Fatal("denial created a reading", err)
				}
				if _, err := admin.Exec(`UPDATE workbench_access_grants SET active=true,allow_budget_summary=true WHERE subject_id=$1`, subject); err != nil {
					t.Fatal(err)
				}
				status, raw = client.call(t, "POST", path, body, fresh())
				if status != 201 {
					t.Fatalf("restored current scope rejected: %d %s", status, raw)
				}
			})
		}
	}
}
