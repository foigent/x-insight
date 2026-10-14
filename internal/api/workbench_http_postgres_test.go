package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

type workbenchHTTPClient struct {
	client     *http.Client
	base, csrf string
}

func loginWorkbenchHTTP(t *testing.T, server *httptest.Server, username, password string) workbenchHTTPClient {
	t.Helper()
	clientCopy := *server.Client()
	client := &clientCopy
	jar, _ := cookiejar.New(nil)
	client.Jar = jar
	response := postJSON(t, client, server.URL+"/api/v1/access/login", server.URL, fmt.Sprintf(`{"username":%q,"password":%q}`, username, password), "")
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("login status=%d", response.StatusCode)
	}
	var session sessionEnvelope
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	return workbenchHTTPClient{client, server.URL, session.CSRFToken}
}
func (c workbenchHTTPClient) request(t *testing.T, method, path string, value any, status int) []byte {
	t.Helper()
	var body io.Reader
	if value != nil {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = strings.NewReader(string(raw))
	}
	request, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", c.base)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", c.csrf)
	response, err := c.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.StatusCode, status, raw)
	}
	if status >= 400 {
		var problem map[string]any
		if json.Unmarshal(raw, &problem) != nil || problem["correlation_id"] == "" || problem["correlation_id"] == nil || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/problem+json") {
			t.Fatalf("invalid Problem: %s", raw)
		}
	}
	return raw
}
func httpReadingRequest(source string) workbench.ReadingRequest {
	return workbench.ReadingRequest{ContractVersion: workbench.ContractVersion, MetricVersion: workbench.MetricVersion, Page: "cost", MoneyBasis: workbench.MoneyGatewayCharged, Currency: "CNY", Period: workbench.PeriodInput{RelativeHours: 24, DisplayTimezone: "UTC"}, Filters: workbench.Filters{SourceIDs: []string{source}, SourceProjects: []string{}, CostProjects: []string{}}}
}
func TestWorkbenchRealHTTPMoneyPaginationAndRevocation(t *testing.T) {
	server, admin, _, source := startWorkbenchProduction(t)
	c := loginWorkbenchHTTP(t, server, "workbench.one", "workbench password")
	request := httpReadingRequest(source)
	invalid := request
	invalid.ContractVersion = "unsupported.v99"
	c.request(t, "POST", "/api/v1/workbench/readings", invalid, 400)
	noCSRF := c
	noCSRF.csrf = ""
	noCSRF.request(t, "POST", "/api/v1/workbench/readings", request, 403)
	for _, basis := range []workbench.MoneyBasis{workbench.MoneyGatewayCharged, workbench.MoneyCalculatedReference} {
		request.MoneyBasis = basis
		var reading workbench.Reading
		json.Unmarshal(c.request(t, "POST", "/api/v1/workbench/readings", request, 201), &reading)
		path := "/api/v1/workbench/readings/" + reading.ID
		var sum workbench.Summary
		json.Unmarshal(c.request(t, "GET", path+"/summary", nil, 200), &sum)
		want := "270"
		if basis == workbench.MoneyCalculatedReference {
			want = "342"
		}
		if sum.SelectedAmountMicros != want || sum.GatewayChargedMicros != "270" || sum.CalculatedReferenceMicros != "342" {
			t.Fatalf("money=%+v", sum)
		}
		var full workbench.Contributions
		cursor := ""
		total := 0
		rows := 0
		for {
			full = workbench.Contributions{}
			json.Unmarshal(c.request(t, "GET", path+"/contributions?dimension=source_project&limit=5&cursor="+cursor, nil, 200), &full)
			if full.DenominatorMicros != want || full.UnassignedScope != "reading_constant" || full.OtherKnownMicros != "0" {
				t.Fatalf("constants=%+v", full)
			}
			for _, row := range full.Rows {
				var amount int
				fmt.Sscan(row.SelectedAmountMicros, &amount)
				total += amount
				rows++
			}
			if full.NextCursor == "" {
				break
			}
			cursor = full.NextCursor
		}
		var wantInt int
		fmt.Sscan(want, &wantInt)
		if rows != 12 || total != wantInt {
			t.Fatalf("pages rows=%d amount=%d", rows, total)
		}
		c.request(t, "GET", path+"/summary?unknown=1", nil, 400)
		other := loginWorkbenchHTTP(t, server, "reader.0", "synthetic password")
		other.request(t, "GET", path+"/summary", nil, 404)
	}
	request.MoneyBasis = workbench.MoneyGatewayCharged
	request.Filters.SourceProjects = []string{"project-9000"}
	var reading workbench.Reading
	json.Unmarshal(c.request(t, "POST", "/api/v1/workbench/readings", request, 201), &reading)
	raw := c.request(t, "GET", "/api/v1/workbench/readings/"+reading.ID+"/summary", nil, 200)
	if !strings.Contains(string(raw), `"selected_amount_micros":"17"`) {
		t.Fatalf("17 micros lost: %s", raw)
	}
	for _, path := range []string{"/api/v1/executions", "/api/v1/cases", "/api/v1/saved-views"} {
		c.request(t, "GET", path, nil, 403)
	}
	if _, err := admin.Exec(`UPDATE workbench_access_grants SET active=false,revision=revision+1 WHERE subject_id='xs_8899aabbccddeeff'`); err != nil {
		t.Fatal(err)
	}
	c.request(t, "GET", "/api/v1/workbench/readings/"+reading.ID+"/summary", nil, 409)
}

// Opt-in real HTTP peak and size rejection; bulk rows are synthetic capacity
// fixtures, separately from the decoder/EvidenceStore correctness test above.
func TestWorkbenchRealHTTPPeakAndOversize(t *testing.T) {
	if os.Getenv("XINSIGHT_A0_HTTP_PERFORMANCE") != "1" {
		t.Skip("opt-in HTTP capacity verification")
	}
	server, admin, store, source := startWorkbenchProduction(t)
	request7 := httpReadingRequest(source)
	request7.Period.RelativeHours = 168
	request30 := httpReadingRequest(source)
	request30.Period.RelativeHours = 720
	// Existing fixture rows supply valid FK references, with distinct business IDs.
	if _, err := admin.Exec(`INSERT INTO workbench_economic_facts
 SELECT (jsonb_populate_record(NULL::workbench_economic_facts,to_jsonb(f)||jsonb_build_object(
 'id',gen_random_uuid(),'source_ref','http-capacity:'||i,'request_id','http-capacity:'||i,
 'occurred_at',now()-((i%604700)+1)*interval '1 second','gateway_charged_micros',1,'calculated_reference_micros',2))).*
 FROM (SELECT * FROM workbench_economic_facts LIMIT 1) f CROSS JOIN generate_series(1,69988) i`); err != nil {
		t.Fatal(err)
	}
	clients := make([]workbenchHTTPClient, 10)
	ids := make([]string, 10)
	for i := 0; i < 10; i++ {
		subject := fmt.Sprintf("xs_%016x", i)
		if _, err := admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary) VALUES($1,$2::uuid,true)`, subject, source); err != nil {
			t.Fatal(err)
		}
		clients[i] = loginWorkbenchHTTP(t, server, fmt.Sprintf("reader.%d", i), "synthetic password")
		var reading workbench.Reading
		if err := json.Unmarshal(clients[i].request(t, "POST", "/api/v1/workbench/readings", request7, 201), &reading); err != nil {
			t.Fatal(err)
		}
		ids[i] = reading.ID
		if reading.Coverage.KnownItems != 70000 {
			t.Fatalf("seven-day members=%d", reading.Coverage.KnownItems)
		}
		if i == 0 {
			clients[i].request(t, "GET", "/api/v1/workbench/readings/"+reading.ID+"/series?metric=amount&bucket_seconds=3600", nil, 200)
		}
	}
	durations := make([]int64, 600)
	endToEnd := make([]int64, 600)
	statuses := make([]int, 600)
	failures := make([]error, 600)
	type job struct {
		index int
		at    time.Time
	}
	jobs := make(chan job, 600)
	var workers sync.WaitGroup
	for i := 0; i < 10; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := range jobs {
				reader := (j.index / 2) % 10
				path := "/summary"
				if j.index%2 == 1 {
					path = "/contributions?dimension=model&top_n=10"
				}
				start := time.Now()
				r, err := clients[reader].client.Get(server.URL + "/api/v1/workbench/readings/" + ids[reader] + path)
				failures[j.index] = err
				if err == nil {
					_, failures[j.index] = io.Copy(io.Discard, r.Body)
					r.Body.Close()
					statuses[j.index] = r.StatusCode
				}
				durations[j.index] = time.Since(start).Milliseconds()
				endToEnd[j.index] = time.Since(j.at).Milliseconds()
			}
		}()
	}
	start := time.Now()
	ticker := time.NewTicker(50 * time.Millisecond)
	for i := 0; i < 600; i++ {
		jobs <- job{i, <-ticker.C}
	}
	ticker.Stop()
	close(jobs)
	workers.Wait()
	elapsed := time.Since(start)
	for i := range statuses {
		if failures[i] != nil || statuses[i] != 200 {
			t.Fatalf("HTTP peak %d status=%d err=%v", i, statuses[i], failures[i])
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	sort.Slice(endToEnd, func(i, j int) bool { return endToEnd[i] < endToEnd[j] })

	createMeasurements := make([]map[string]any, 0, 21)
	measureCreate := func(size int) {
		raw, _ := json.Marshal(request30)
		req, err := http.NewRequest("POST", server.URL+"/api/v1/workbench/readings", strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", server.URL)
		req.Header.Set("X-CSRF-Token", clients[0].csrf)
		var before int
		if err := admin.QueryRow(`SELECT count(*) FROM workbench_readings`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		res, err := clients[0].client.Do(req)
		duration := time.Since(started).Milliseconds()
		if err != nil {
			t.Fatal(err)
		}
		payload, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		row := map[string]any{"members": size, "status": res.StatusCode, "duration_ms": duration}
		if res.StatusCode == 201 {
			var reading workbench.Reading
			if err := json.Unmarshal(payload, &reading); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := admin.QueryRow(`SELECT count(*) FROM workbench_reading_members WHERE reading_id=$1`, reading.ID).Scan(&count); err != nil || count != size {
				t.Fatalf("HTTP created count=%d expected=%d err=%v", count, size, err)
			}
		} else {
			var after int
			if err := admin.QueryRow(`SELECT count(*) FROM workbench_readings`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			row["zero_residual"] = before == after
			row["problem"] = string(payload)
			if before != after {
				t.Fatal("failed creation retained a header")
			}
		}
		createMeasurements = append(createMeasurements, row)
	}
	for _, stage := range []struct{ start, end, size, samples int }{{69989, 299988, 300000, 20}, {299989, 499988, 500000, 1}} {
		if _, err := admin.Exec(`INSERT INTO workbench_economic_facts
 SELECT (jsonb_populate_record(NULL::workbench_economic_facts,to_jsonb(f)||jsonb_build_object(
 'id',gen_random_uuid(),'source_ref','http-capacity:'||i,'request_id','http-capacity:'||i,
 'occurred_at',now()-interval '8 days'-((i-69989)%1814400)*interval '1 second','gateway_charged_micros',1,'calculated_reference_micros',2))).*
 FROM (SELECT * FROM workbench_economic_facts LIMIT 1) f CROSS JOIN generate_series($1::int,$2::int) i`, stage.start, stage.end); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(`ANALYZE workbench_economic_facts`); err != nil {
			t.Fatal(err)
		}
		for sample := 0; sample < stage.samples; sample++ {
			measureCreate(stage.size)
		}
	}
	if _, err := admin.Exec(`INSERT INTO workbench_economic_facts
 SELECT (jsonb_populate_record(NULL::workbench_economic_facts,to_jsonb(f)||jsonb_build_object(
 'id',gen_random_uuid(),'source_ref','http-capacity:'||i,'request_id','http-capacity:'||i,
 'occurred_at',now()-interval '8 days'-((i-69989)%1814400)*interval '1 second','gateway_charged_micros',1,'calculated_reference_micros',2))).*
 FROM (SELECT * FROM workbench_economic_facts LIMIT 1) f CROSS JOIN generate_series(499989,499989) i`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var s string
		if err := admin.QueryRow(`SELECT jsonb_build_array((SELECT count(*) FROM workbench_readings),(SELECT count(*) FROM workbench_reading_members),(SELECT count(*) FROM workbench_saved_filters),(SELECT count(*) FROM workbench_saved_filter_operations),(SELECT count(*) FROM workbench_economic_facts),(SELECT count(*) FROM evidence_events))::text`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := snapshot()
	_, err := store.CreateWorkbenchReading(t.Context(), "xs_0000000000000000", request30, time.Now())
	if err != workbench.ErrTooLarge {
		t.Fatalf("store oversize=%v", err)
	}
	raw := clients[0].request(t, "POST", "/api/v1/workbench/readings", request30, 422)
	if !strings.Contains(string(raw), `"code":"workbench_reading_too_large"`) || snapshot() != before {
		t.Fatalf("oversize residual or Problem: %s", raw)
	}
	report := map[string]any{"boundary": "Local TLS HTTP production handler + isolated PG16; synthetic capacity data", "reading_preparation": "production HTTP; actual runtime DB roles when XINSIGHT_A0_RUNTIME_ROLES=1", "creation_measurements": createMeasurements, "runtime_roles": os.Getenv("XINSIGHT_A0_RUNTIME_ROLES") == "1", "peak_reading_hours": 168, "creation_reading_hours": 720, "calls": 600, "readers": 10, "readings": 10, "errors": 0, "achieved_rps": 600 / elapsed.Seconds(), "request_p95_ms": durations[569], "end_to_end_p95_ms": endToEnd[569], "oversize_http": 422, "oversize_store": "ErrTooLarge", "zero_residual": true, "table_counts_before_after": before}
	payload, _ := json.MarshalIndent(report, "", "  ")
	t.Log(string(payload))
	if output := os.Getenv("XINSIGHT_A0_HTTP_OUTPUT"); output != "" {
		if err := os.WriteFile(output, payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	createMS := make([]int64, 0, 20)
	allCreated := true
	for i, row := range createMeasurements {
		if row["status"].(int) != 201 {
			allCreated = false
		}
		if i < 20 {
			createMS = append(createMS, row["duration_ms"].(int64))
		}
	}
	sort.Slice(createMS, func(i, j int) bool { return createMS[i] < createMS[j] })
	if !allCreated || len(createMS) != 20 || createMS[18] > 5000 {
		t.Errorf("HTTP creation unqualified: all_created=%t durations_ms=%v", allCreated, createMS)
	}
	if durations[569] > 2000 || endToEnd[569] > 2000 {
		t.Fatalf("HTTP p95=%d exceeds 2000ms", durations[569])
	}
}

func TestWorkbenchHTTPClientCancellationRollsBack(t *testing.T) {
	server, admin, _, source := startWorkbenchProduction(t)
	c := loginWorkbenchHTTP(t, server, "workbench.one", "workbench password")
	var before int
	if err := admin.QueryRow(`SELECT count(*) FROM workbench_readings`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	lock, err := admin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if _, err = lock.Exec(`LOCK workbench_reading_members IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(httpReadingRequest(source))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", server.URL+"/api/v1/workbench/readings", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", server.URL)
	req.Header.Set("X-CSRF-Token", c.csrf)
	finished := make(chan error, 1)
	go func() {
		r, err := c.client.Do(req)
		if r != nil {
			r.Body.Close()
		}
		finished <- err
	}()
	// Prove the handler reached the locked INSERT before cancelling its client.
	deadline := time.Now().Add(2 * time.Second)
	for {
		var waiting bool
		if err := admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%INSERT INTO workbench_reading_members%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("request did not reach member INSERT")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-finished; err == nil {
		t.Fatal("cancelled client unexpectedly succeeded")
	}
	lock.Rollback()
	// A database barrier waits for any server-side transaction to finish rollback.
	barrier, err := admin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = barrier.Exec(`SET LOCAL lock_timeout='2s'; LOCK workbench_readings IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := barrier.QueryRow(`SELECT count(*) FROM workbench_readings`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	barrier.Rollback()
	if before != after {
		t.Fatalf("cancelled HTTP creation left header: %d -> %d", before, after)
	}
}
