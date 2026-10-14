package api_test

import (
	"bytes"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

// This test is not parallel. Its temporary process location is restored after
// fixture servers/pools stop; PGTZ applies only to connections opened by this
// test process, never ALTER DATABASE, server configuration or the host timezone.
func TestReadingUTCPostGetExecutionNavigation(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	oldLocal := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = oldLocal })
	t.Setenv("PGTZ", "Asia/Shanghai")
	server, admin, _, source, _ := startWorkbenchProductionFixture(t, true)
	var timezone string
	var driverNow time.Time
	if err = admin.QueryRow(`SELECT current_setting('TimeZone'),clock_timestamp()`).Scan(&timezone, &driverNow); err != nil {
		t.Fatal(err)
	}
	_, offset := driverNow.Zone()
	if timezone != "Asia/Shanghai" || offset != 8*3600 {
		t.Fatalf("non-UTC regression precondition missing: DB=%s scanned_offset=%d", timezone, offset)
	}
	// Reuse the exact execution opened by TestScopedProductionAccountsReadReviewRevokeAndRoutes.
	// Other fixture requests belong to other projects outside this reviewer's scope.
	var execution, project string
	var occurredAt time.Time
	if err = admin.QueryRow(`SELECT execution_id,source_project_ref,occurred_at
 FROM workbench_economic_facts WHERE source_system_id=$1 AND request_id='req-browser-workbench-01'`, source).Scan(&execution, &project, &occurredAt); err != nil {
		t.Fatal(err)
	}
	// Like TestRuntimeReadingHTTPGrantCSRFAndActualRoles, explicitly prepare the
	// runtime grant in this disposable fixture DB, limited to this subject/source/project.
	// Keep its existing source/economic scope; do not grant another project or role.
	result, err := admin.Exec(`UPDATE workbench_access_grants SET allow_source_runtime=true
 WHERE subject_id='xs_8899aabbccddeeff' AND source_system_id=$1 AND source_project_ref=$2
 AND allow_summary AND allow_details AND allow_source_context AND allow_source_identity AND allow_source_cost`, source, project)
	if err != nil {
		t.Fatal(err)
	}
	if changed, e := result.RowsAffected(); e != nil || changed != 1 {
		t.Fatalf("exact scoped runtime grant precondition: changed=%d err=%v", changed, e)
	}
	client := responsibilityLogin(t, &responsibilityFixture{server: server}, "scoped.reviewer")
	// Explicit non-millisecond microseconds prevent a JavaScript-style
	// millisecond round trip from satisfying the public DTO assertions.
	end := time.Now().Add(-time.Minute).Truncate(time.Second).Add(410194 * time.Microsecond)
	start := end.Add(-24 * time.Hour)
	if occurredAt.Before(start) || !occurredAt.Before(end) {
		t.Fatal("scoped execution is outside the regression reading period", occurredAt, start, end)
	}
	request := httpReadingRequest(source)
	request.Period = workbench.PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "Asia/Shanghai"}
	call := func(t *testing.T, method, path string, body any, want int) []byte {
		t.Helper()
		status, raw := client.call(t, method, path, body, nil)
		if status != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, status, want, raw)
		}
		return raw
	}
	// Prove the production detail route is open before either reading lifecycle.
	call(t, "GET", "/api/v1/executions/"+execution, nil, 200)
	t.Logf("UTC fixture: scoped request=req-browser-workbench-01 project=%s execution=%s; exact runtime grant=1; execution detail=200", project, execution)
	type readingWire struct {
		ID     string `json:"reading_id"`
		Period struct {
			Start string `json:"start_inclusive"`
			End   string `json:"end_exclusive"`
			Zone  string `json:"timezone"`
		} `json:"period"`
		Cutoff  string `json:"cutoff"`
		Expires string `json:"expires_at"`
	}
	decode := func(t *testing.T, raw []byte) readingWire {
		t.Helper()
		var r readingWire
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{r.Period.Start, r.Period.End, r.Cutoff, r.Expires} {
			v, e := time.Parse(time.RFC3339Nano, text)
			if e != nil || text != v.UTC().Format(time.RFC3339Nano) || v.Nanosecond()%1000 != 0 {
				t.Fatal("public reading instant is not precise UTC", text, e)
			}
		}
		if r.Period.Start != start.UTC().Format(time.RFC3339Nano) || r.Period.End != end.UTC().Format(time.RFC3339Nano) || r.Period.Zone != "Asia/Shanghai" {
			t.Fatal("period or display timezone changed")
		}
		return r
	}
	for _, kind := range []string{"cost", "runtime"} {
		t.Run(kind, func(t *testing.T) {
			path := "/api/v1/workbench/readings"
			var input any = request
			if kind == "runtime" {
				path = "/api/v1/runtime/readings"
				input = workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: request.Period, Filters: request.Filters}
			}
			post := call(t, "POST", path, input, 201)
			created := decode(t, post)
			// Use the literal DTO string just as navigation does; never normalize it
			// in the test client before passing it into strict must_be_utc parsing.
			executionPath := "/api/v1/executions/" + execution + "?as_of="
			first := call(t, "GET", executionPath+url.QueryEscape(created.Cutoff), nil, 200)
			read := call(t, "GET", path+"/"+created.ID, nil, 200)
			returned := decode(t, read)
			var a, b any
			if json.Unmarshal(post, &a) != nil || json.Unmarshal(read, &b) != nil {
				t.Fatal("invalid reading DTO")
			}
			left, _ := json.Marshal(a)
			right, _ := json.Marshal(b)
			if !bytes.Equal(left, right) {
				t.Fatalf("POST/GET changed frozen reading DTO: POST=%s GET=%s", post, read)
			}
			again := call(t, "GET", executionPath+url.QueryEscape(returned.Cutoff), nil, 200)
			if !bytes.Equal(first, again) {
				t.Fatal("reopened execution snapshot changed")
			}
			cut, e := time.Parse(time.RFC3339Nano, returned.Cutoff)
			if e != nil {
				t.Fatal(e)
			}
			denied := call(t, "GET", executionPath+url.QueryEscape(cut.In(location).Format(time.RFC3339Nano)), nil, 400)
			var problem struct {
				Code   string                          `json:"code"`
				Fields []struct{ Name, Reason string } `json:"fields"`
			}
			if json.Unmarshal(denied, &problem) != nil || problem.Code != "invalid_argument" || len(problem.Fields) != 1 || problem.Fields[0].Name != "as_of" || problem.Fields[0].Reason != "must_be_utc" {
				t.Fatal("non-UTC input guard weakened", string(denied))
			}
		})
	}
}
