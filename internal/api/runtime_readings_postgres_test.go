package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/workbench"
)

func TestRuntimeReadingHTTPGrantCSRFAndActualRoles(t *testing.T) {
	t.Setenv("XINSIGHT_A0_RUNTIME_ROLES", "1")
	server, admin, _, source := startWorkbenchProduction(t)
	c := loginWorkbenchHTTP(t, server, "workbench.one", "workbench password")
	legacy := httpReadingRequest(source)
	input := workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: legacy.Period, Filters: legacy.Filters}
	c.request(t, "POST", "/api/v1/runtime/readings", input, 403)
	if _, err := admin.Exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id='xs_8899aabbccddeeff'`); err != nil {
		t.Fatal(err)
	}
	noCSRF := c
	noCSRF.csrf = ""
	noCSRF.request(t, "POST", "/api/v1/runtime/readings", input, 403)
	// Money input is not a tolerated runtime extension.
	raw, _ := json.Marshal(input)
	var widened map[string]any
	json.Unmarshal(raw, &widened)
	widened["money_basis"] = "gateway_charged"
	c.request(t, "POST", "/api/v1/runtime/readings", widened, 400)
	var reading workbench.RuntimeReading
	if err := json.Unmarshal(c.request(t, "POST", "/api/v1/runtime/readings", input, 201), &reading); err != nil {
		t.Fatal(err)
	}
	if reading.ObservationCount != 0 || reading.RequestsWithEntryTime != 0 || reading.RequestsWithUnknownEntryTime != 0 {
		t.Fatal("I-01-only fixtures became B requests", reading)
	}
	path := "/api/v1/runtime/readings/" + reading.ID
	associationPath := path + "/requests/xrr_" + strings.Repeat("a", 64) + "/associations"
	c.request(t, "GET", associationPath, nil, 404)
	c.request(t, "GET", associationPath+"?limit=101", nil, 400)
	c.request(t, "GET", associationPath+"?entry_time=all", nil, 400)
	resourcePath := strings.TrimSuffix(associationPath, "associations") + "resources"
	c.request(t, "GET", resourcePath, nil, 404)
	c.request(t, "GET", resourcePath+"?limit=101", nil, 400)
	c.request(t, "GET", resourcePath+"?entry_time=all", nil, 400)
	c.request(t, "POST", resourcePath, map[string]any{}, 403)
	var executionID string
	if err := admin.QueryRow(`SELECT execution_id FROM workbench_economic_facts WHERE source_system_id=$1 LIMIT 1`, source).Scan(&executionID); err != nil {
		t.Fatal(err)
	}
	executionPath := path + "/executions/" + executionID
	c.request(t, "GET", executionPath, nil, 404)
	c.request(t, "GET", executionPath+"?limit=1", nil, 400)
	c.request(t, "POST", executionPath, map[string]any{}, 403)

	c.request(t, "GET", path, nil, 200)
	c.request(t, "GET", path+"/requests?entry_time=unknown&limit=1", nil, 200)
	c.request(t, "GET", path+"/attempts", nil, 200)
	c.request(t, "GET", path+"/sources", nil, 200)
	c.request(t, "GET", path+"/measurements", nil, 200)
	c.request(t, "GET", path+"/measurements?entry_time=invalid", nil, 400)
	var dimensions workbench.RuntimeDimensions
	if err := json.Unmarshal(c.request(t, "GET", path+"/dimensions/selected_model", nil, 200), &dimensions); err != nil {
		t.Fatal(err)
	}
	if dimensions.ReadingID != reading.ID || dimensions.Requests != 0 || dimensions.Items == nil || dimensions.DimensionsVersion != workbench.RuntimeDimensionsVersion {
		t.Fatal("I-01-only fixture manufactured selected models", dimensions)
	}
	c.request(t, "GET", path+"/dimensions/operation?entry_time=invalid", nil, 400)
	c.request(t, "GET", path+"/dimensions/operation?limit=101", nil, 400)
	c.request(t, "GET", path+"/dimensions/operation?currency=USD", nil, 400)
	c.request(t, "GET", path+"/dimensions/money", nil, 403)
	var summary workbench.RuntimeSummary
	if err := json.Unmarshal(c.request(t, "GET", path+"/summary", nil, 200), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.ReadingID != reading.ID || summary.EntryTime != "known" || summary.SummaryVersion != workbench.RuntimeSummaryVersion || summary.Requests != 0 || summary.Outcomes == nil || len(summary.Outcomes) != 0 || summary.DiagnosticFailure.State != "not_computable" || summary.DiagnosticFailure.Denominator != 0 {
		t.Fatal("I-01-only fixture manufactured runtime outcomes", summary)
	}
	c.request(t, "GET", path+"/summary?entry_time=unknown", nil, 200)
	c.request(t, "GET", path+"/summary?entry_time=invalid", nil, 400)
	c.request(t, "GET", path+"/summary?limit=1", nil, 400)
	c.request(t, "GET", path+"/summary?entry_time=known&entry_time=all", nil, 400)
	c.request(t, "GET", path+"/requests?entry_time=invalid", nil, 400)
	c.request(t, "GET", path+"/budget", nil, 403)
	if _, err := admin.Exec(`UPDATE workbench_access_grants SET allow_source_runtime=false WHERE subject_id='xs_8899aabbccddeeff'`); err != nil {
		t.Fatal(err)
	}
	c.request(t, "GET", path, nil, 409)
	c.request(t, "GET", associationPath, nil, 409)
	c.request(t, "GET", resourcePath, nil, 409)
	c.request(t, "GET", path+"/summary", nil, 409)
	c.request(t, "GET", path+"/dimensions/selected_model", nil, 409)
}

func TestRuntimeHTTPWithoutEconomicPermission(t *testing.T) {
	t.Setenv("XINSIGHT_A0_RUNTIME_ROLES", "1")
	server, admin, _, source := startWorkbenchProduction(t)
	c := loginWorkbenchHTTP(t, server, "workbench.one", "workbench password")
	if _, err := admin.Exec(`UPDATE workbench_access_grants SET allow_summary=false,allow_details=false,allow_source_runtime=true WHERE subject_id='xs_8899aabbccddeeff'`); err != nil {
		t.Fatal(err)
	}
	legacy := httpReadingRequest(source)
	input := workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: legacy.Period, Filters: legacy.Filters}
	c.request(t, "POST", "/api/v1/workbench/readings", legacy, 403)
	c.request(t, "POST", "/api/v1/runtime/readings", input, 201)
	c.request(t, "GET", "/api/v1/runtime/current", nil, 200)
	input.Filters.CostProjects = []string{"hidden-cost-project"}
	c.request(t, "POST", "/api/v1/runtime/readings", input, 403)
	c.request(t, "GET", "/api/v1/runtime/current?cost_projects=hidden-cost-project", nil, 403)
}
