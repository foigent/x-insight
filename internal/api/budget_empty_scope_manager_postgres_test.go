package api_test

import (
	"encoding/json"
	"testing"

	"github.com/foigent/x-insight/internal/workbench"
)

// Independent acceptance scenario: a real logged-in user loses their last
// grant, refreshes their session, and opens a new or previously saved budget.
// Fixture SQL only prepares test state; this is not the product preview path.
func TestManagerBudgetRevokedScopeFreshSession(t *testing.T) {
	t.Setenv("XINSIGHT_A0_RUNTIME_ROLES", "1")
	for _, username := range []string{"scoped.reviewer", "scoped.reader"} {
		t.Run(username, func(t *testing.T) {
			server, admin, _, source, _ := startWorkbenchProductionFixture(t, true)
			subject := "xs_8899aabbccddeeff"
			if username == "scoped.reader" {
				subject = "xs_6666666666666666"
			}
			if _, err := admin.Exec(`DELETE FROM workbench_access_grants WHERE subject_id=$1`, subject); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_budget_summary) VALUES($1,$2,true)`, subject, source); err != nil {
				t.Fatal(err)
			}
			client := responsibilityLogin(t, &responsibilityFixture{server: server}, username)
			freshHeaders := func() map[string]string {
				t.Helper()
				status, raw := client.call(t, "GET", "/api/v1/access/session", nil, nil)
				var session struct {
					ScopeRevision string `json:"scope_revision"`
				}
				if status != 200 || json.Unmarshal(raw, &session) != nil || session.ScopeRevision == "" {
					t.Fatalf("session failed: %d %s", status, raw)
				}
				return map[string]string{"X-XInsight-Scope-Revision": session.ScopeRevision}
			}
			path := "/api/v1/workbench/budgets/readings"
			body := workbench.BudgetReadingRequest{ContractVersion: workbench.BudgetContractVersion}
			status, raw := client.call(t, "POST", path, body, freshHeaders())
			var reading workbench.BudgetReading
			if status != 201 || json.Unmarshal(raw, &reading) != nil || reading.ID == "" {
				t.Fatalf("valid control failed: %d %s", status, raw)
			}
			if _, err := admin.Exec(`UPDATE workbench_access_grants SET active=false WHERE subject_id=$1`, subject); err != nil {
				t.Fatal(err)
			}
			for _, suffix := range []string{"", "/" + reading.ID, "/" + reading.ID + "/items"} {
				t.Run("fresh_session"+suffix, func(t *testing.T) {
					method := "GET"
					var request any
					if suffix == "" {
						method, request = "POST", body
					}
					status, raw := client.call(t, method, path+suffix, request, freshHeaders())
					if status == 409 {
						t.Fatalf("fresh session still asks user to refresh after all grants were revoked: %d %s", status, raw)
					}
					if status >= 500 {
						t.Fatalf("revocation failed to settle: %d %s", status, raw)
					}
				})
			}
		})
	}
}
