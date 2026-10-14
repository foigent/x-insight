package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

type autoExplainEntry struct {
	PID        int            `json:"pid"`
	DurationMS float64        `json:"duration_ms"`
	Document   map[string]any `json:"document"`
}

var autoExplainPrefix = regexp.MustCompile(`XI_AUTO\[([0-9]+)\] LOG:  duration: ([0-9.]+) ms  plan:\n`)

// PostgreSQL's auto_explain JSON is one object, with indented continuation lines.
func autoExplainParse(raw []byte, pid int) ([]autoExplainEntry, error) {
	entries := []autoExplainEntry{}
	for _, loc := range autoExplainPrefix.FindAllSubmatchIndex(raw, -1) {
		actual, _ := strconv.Atoi(string(raw[loc[2]:loc[3]]))
		if actual != pid {
			continue
		}
		duration, err := strconv.ParseFloat(string(raw[loc[4]:loc[5]]), 64)
		if err != nil {
			return nil, err
		}
		var doc map[string]any
		if err := json.NewDecoder(strings.NewReader(string(raw[loc[1]:]))).Decode(&doc); err != nil {
			return nil, err
		}
		if _, ok := doc["Query Text"].(string); !ok {
			return nil, fmt.Errorf("missing query text")
		}
		if _, ok := doc["Plan"].(map[string]any); !ok {
			return nil, fmt.Errorf("missing actual plan")
		}
		entries = append(entries, autoExplainEntry{actual, duration, doc})
	}
	return entries, nil
}
func autoExplainPreflight(entries []autoExplainEntry, query, parameters string) error {
	count := 0
	for _, e := range entries {
		if e.Document["Query Text"] != query {
			continue
		}
		count++
		if e.Document["Query Parameters"] != parameters {
			return fmt.Errorf("actual parameter log mismatch: %v", e.Document["Query Parameters"])
		}
		plan := e.Document["Plan"].(map[string]any)
		if plan["Actual Rows"] != float64(1) || plan["Actual Loops"] != float64(1) {
			return fmt.Errorf("not a one-row analyzed preflight")
		}
		for _, k := range []string{"Shared Hit Blocks", "WAL Records"} {
			if _, ok := plan[k]; !ok {
				return fmt.Errorf("missing %s", k)
			}
		}
	}
	if count != 1 {
		return fmt.Errorf("preflight plan count %d", count)
	}
	return nil
}
func TestWorkbenchAutoExplainLogContract(t *testing.T) {
	raw := []byte("XI_AUTO[12] LOG:  duration: 0.123 ms  plan:\n\t{\n\t\"Query Text\":\"SELECT $1::text\",\"Query Parameters\":\"$1 = 'bound'\",\"Plan\":{\"Actual Rows\":1,\"Actual Loops\":1,\"Shared Hit Blocks\":0,\"WAL Records\":0}}\nXI_AUTO[13] LOG:  duration: 1.000 ms  plan:\n{bad other process")
	entries, err := autoExplainParse(raw, 12)
	if err != nil || len(entries) != 1 {
		t.Fatalf("parse %v %v", entries, err)
	}
	if err := autoExplainPreflight(entries, "SELECT $1::text", "$1 = 'bound'"); err != nil {
		t.Fatal(err)
	}
	if autoExplainPreflight(entries, "SELECT $1::text", "$1 = 'wrong'") == nil {
		t.Fatal("wrong parameter accepted")
	}
	if autoExplainPreflight(entries, "SELECT $2::text", "$1 = 'bound'") == nil {
		t.Fatal("wrong SQL accepted")
	}
	entries[0].Document["Plan"].(map[string]any)["Actual Rows"] = float64(0)
	if autoExplainPreflight(entries, "SELECT $1::text", "$1 = 'bound'") == nil {
		t.Fatal("missing execution accepted")
	}
	if _, err := autoExplainParse(raw[:90], 12); err == nil {
		t.Fatal("truncated workspace plan accepted")
	}
}

const autoExplainCountsSQL = `SELECT jsonb_build_array((SELECT count(*) FROM workbench_readings),(SELECT count(*) FROM workbench_reading_members),(SELECT count(*) FROM workbench_saved_filters),(SELECT count(*) FROM workbench_saved_filter_operations),(SELECT count(*) FROM workbench_economic_facts),(SELECT count(*) FROM evidence_events))`
const autoExplainPreparedSQL = `SELECT COALESCE(jsonb_agg(jsonb_build_object('name',name,'statement',statement,'parameter_types',parameter_types::oid[],'prepare_time',prepare_time,'generic_plans',generic_plans,'custom_plans',custom_plans) ORDER BY name),'[]'::jsonb) FROM pg_prepared_statements`

func TestWorkbenchAutoExplainCreateDiagnostic(t *testing.T) {
	if os.Getenv("XINSIGHT_A0_AUTO_CREATE") != "1" {
		t.Skip("one explicitly authorized synthetic Store creation only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	report := map[string]any{"started_at": time.Now().UTC(), "attempted": 0, "complete": false, "qualification": false, "scope": "One instrumented normal Store creation at retained 5.7M; fresh session, not original qualification replay"}
	output := os.Getenv("XINSIGHT_A0_AUTO_OUTPUT")
	defer func() { report["finished_at"] = time.Now().UTC(); checkpointWriteJSON(t, output, report) }()
	var original struct {
		Fixture checkpointFixture `json:"fixture"`
		Samples []struct {
			Success bool              `json:"success"`
			Reading workbench.Reading `json:"reading"`
		} `json:"samples"`
	}
	raw, err := os.ReadFile(os.Getenv("XINSIGHT_A0_PLAN_ORIGINAL"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	fixture := original.Fixture
	report["fixture"] = fixture
	var baseline workbench.Reading
	for _, s := range original.Samples {
		if s.Success {
			baseline = s.Reading
			break
		}
	}
	if baseline.Coverage.KnownItems != 300000 || baseline.MembershipRef == "" {
		t.Fatal("invalid original semantic baseline")
	}
	admin := openPerformanceSQL(t, os.Getenv("XINSIGHT_POSTGRES_DSN"))
	defer admin.Close()
	u, err := url.Parse(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User("xinsight_workspace")
	store, err := OpenWorkspaceStore(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.db.SetMaxOpenConns(1)
	store.db.SetMaxIdleConns(1)
	conn, err := store.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	if err = conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if err = conn.Raw(func(dc any) error {
		p := dc.(*stdlib.Conn).Conn()
		c := p.Config()
		report["driver"] = map[string]any{"pid": p.PgConn().PID(), "statement_cache_capacity": c.StatementCacheCapacity, "mode": c.DefaultQueryExecMode.String()}
		if int(p.PgConn().PID()) != pid || c.StatementCacheCapacity != 512 || c.DefaultQueryExecMode != pgx.QueryExecModeCacheStatement {
			return fmt.Errorf("unexpected driver")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	} // Release before product Store acquires its sole connection.
	identity := planCacheJSON(t, ctx, store.db, `SELECT jsonb_build_object('pid',pg_backend_pid(),'session_user',session_user,'current_user',current_user,'superuser',(SELECT rolsuper FROM pg_roles WHERE rolname=current_user),'statement_timeout',current_setting('statement_timeout'),'plan_cache_mode',current_setting('plan_cache_mode'),'facts_update',has_table_privilege(current_user,'workbench_economic_facts','UPDATE'),'members_insert',has_table_privilege(current_user,'workbench_reading_members','INSERT'))`)
	report["identity"] = identity
	var role struct {
		PID     int    `json:"pid"`
		Session string `json:"session_user"`
		User    string `json:"current_user"`
		Super   bool   `json:"superuser"`
		Timeout string `json:"statement_timeout"`
		Mode    string `json:"plan_cache_mode"`
		Facts   bool   `json:"facts_update"`
		Members bool   `json:"members_insert"`
	}
	if err = json.Unmarshal(identity, &role); err != nil {
		t.Fatal(err)
	}
	if role.PID != pid || role.Session != "xinsight_workspace" || role.User != role.Session || role.Super || role.Timeout != "10s" || role.Mode != "auto" || role.Facts || !role.Members {
		t.Fatalf("unexpected role %s", identity)
	}
	settings := planCacheJSON(t, ctx, store.db, `SELECT jsonb_object_agg(name,setting) FROM pg_settings WHERE name=ANY(ARRAY['server_version','shared_buffers','work_mem','max_wal_size','wal_buffers','fsync','synchronous_commit','full_page_writes','autovacuum','max_parallel_workers_per_gather','session_preload_libraries','auto_explain.log_min_duration','auto_explain.log_parameter_max_length','auto_explain.log_analyze','auto_explain.log_buffers','auto_explain.log_wal','auto_explain.log_settings','auto_explain.log_triggers','auto_explain.log_timing','auto_explain.log_nested_statements','auto_explain.log_format','auto_explain.sample_rate'])`)
	report["settings"] = settings
	var settingMap map[string]string
	if err = json.Unmarshal(settings, &settingMap); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"shared_buffers": "16384", "work_mem": "4096", "max_wal_size": "1024", "wal_buffers": "512", "fsync": "on", "synchronous_commit": "on", "full_page_writes": "on", "autovacuum": "on", "max_parallel_workers_per_gather": "2", "auto_explain.log_min_duration": "0", "auto_explain.log_parameter_max_length": "-1", "auto_explain.log_analyze": "on", "auto_explain.log_buffers": "on", "auto_explain.log_wal": "on", "auto_explain.log_settings": "on", "auto_explain.log_triggers": "on", "auto_explain.log_timing": "off", "auto_explain.log_nested_statements": "off", "auto_explain.log_format": "json", "auto_explain.sample_rate": "1"} {
		if settingMap[name] != want {
			t.Fatalf("setting %s=%s want %s", name, settingMap[name], want)
		}
	}
	// PostgreSQL hides GUC_SUPERUSER_ONLY rows from the restricted pg_settings view.
	// Read this startup option via the existing diagnostic admin; actual loading is
	// still independently required by the workspace's analyzed parameter log below.
	var preload string
	if err = admin.QueryRowContext(ctx, "SHOW session_preload_libraries").Scan(&preload); err != nil {
		t.Fatal(err)
	}
	report["admin_session_preload_libraries"] = preload
	if preload != "auto_explain" {
		t.Fatal("unexpected startup preload setting")
	}
	logPath := os.Getenv("XINSIGHT_A0_AUTO_LOG")
	const preflightSQL = `SELECT jsonb_build_object('pid',pg_backend_pid(),'marker',$1::text,'projects',$2::text[],'cutoff',$3::timestamptz::text)`
	const marker = "xi_auto_preflight_20260909"
	preflightResult := planCacheJSON(t, ctx, store.db, preflightSQL, marker, pgTextArray([]string{"project-300k"}), fixture.Cutoff)
	var light struct {
		PID      int      `json:"pid"`
		Marker   string   `json:"marker"`
		Projects []string `json:"projects"`
		Cutoff   string   `json:"cutoff"`
	}
	if err = json.Unmarshal(preflightResult, &light); err != nil {
		t.Fatal(err)
	}
	if light.PID != pid || light.Marker != marker || len(light.Projects) != 1 || light.Projects[0] != "project-300k" {
		t.Fatal("light result mismatch")
	}
	raw, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := autoExplainParse(raw, pid)
	if err != nil {
		t.Fatal(err)
	}
	params := fmt.Sprintf("$1 = '%s', $2 = '{project-300k}', $3 = '%s'", marker, light.Cutoff)
	if err = autoExplainPreflight(entries, preflightSQL, params); err != nil {
		t.Fatal(err)
	}
	report["preflight"] = map[string]any{"passed": true, "result": preflightResult, "sql": preflightSQL, "expected_parameters": params, "entries": entries}
	report["constraints_before"] = checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_agg(to_jsonb(c) ORDER BY oid) FROM pg_constraint c WHERE conrelid IN ('workbench_readings'::regclass,'workbench_reading_members'::regclass,'workbench_economic_facts'::regclass)`)
	report["triggers_before"] = checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_agg(to_jsonb(c) ORDER BY oid) FROM pg_trigger c WHERE tgrelid IN ('workbench_readings'::regclass,'workbench_reading_members'::regclass,'workbench_economic_facts'::regclass)`)
	before := checkpointQueryJSON(t, ctx, admin, autoExplainCountsSQL)
	report["counts_before"] = before
	var countsBefore []int64
	if err = json.Unmarshal(before, &countsBefore); err != nil || len(countsBefore) != 6 || countsBefore[0] != 19 || countsBefore[1] != 5700000 {
		t.Fatalf("wrong retained prestate %s %v", before, err)
	}
	report["headers_before"] = checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_agg(id ORDER BY id) FROM workbench_readings`)
	report["stats_before"] = planCacheStats(t, ctx, store.db)
	report["storage_before"] = checkpointSnapshot(t, ctx, admin)
	report["prepared_before"] = planCacheJSON(t, ctx, store.db, autoExplainPreparedSQL)
	res := planCacheResource(t, ctx)
	report["resources_before_create"] = res
	var resource struct {
		New    int64 `json:"new_bytes"`
		Free   int64 `json:"free_bytes"`
		Within bool  `json:"within_limits"`
	}
	if err = json.Unmarshal(res, &resource); err != nil || !resource.Within || 4480*1024*1024-resource.New < 512*1024*1024 || resource.Free < 43*1024*1024*1024/2 {
		t.Fatalf("first creation reserve failed: %s %v", res, err)
	}
	request := explicitPerformanceRequest(fixture.Cutoff.Add(-30*24*time.Hour), fixture.Cutoff, []string{fixture.Source}, []string{"project-300k"})
	report["request"] = request
	raw, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	offset := len(raw)
	report["attempted"] = 1
	report["creation_log_start_byte"] = offset
	checkpointWriteJSON(t, output, report)
	phases := []map[string]any{}
	started := time.Now()
	reading, createErr := store.createWorkbenchReading(ctx, fixture.Subject, request, fixture.Cutoff, func(name string, d time.Duration) {
		phases = append(phases, map[string]any{"name": name, "duration_ns": d.Nanoseconds(), "completed_at": time.Now().UTC()})
	})
	report["creation"] = map[string]any{"started_at": started.UTC(), "ended_at": time.Now().UTC(), "duration_ns": time.Since(started).Nanoseconds(), "phases": phases, "success": createErr == nil, "reading": reading}
	if createErr != nil {
		report["error"] = createErr.Error()
		var pe *pgconn.PgError
		if errors.As(createErr, &pe) {
			report["sqlstate"] = pe.Code
		}
	}
	checkpointWriteJSON(t, output, report)
	raw, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err = autoExplainParse(raw[offset:], pid)
	if err != nil {
		t.Fatal(err)
	}
	report["creation_log_end_byte"] = len(raw)
	report["creation_plans"] = entries
	var pidAfter int
	if err = store.db.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pidAfter); err != nil {
		t.Fatal(err)
	}
	report["pid_after"] = pidAfter
	if pidAfter != pid {
		t.Fatal("workspace connection changed")
	}
	report["prepared_after"] = planCacheJSON(t, ctx, store.db, autoExplainPreparedSQL)
	report["stats_after"] = planCacheStats(t, ctx, store.db)
	report["storage_after"] = checkpointSnapshot(t, ctx, admin)
	after := checkpointQueryJSON(t, ctx, admin, autoExplainCountsSQL)
	report["counts_after"] = after
	var countsAfter []int64
	if err = json.Unmarshal(after, &countsAfter); err != nil || len(countsAfter) != 6 {
		t.Fatal("invalid after counts")
	}
	report["headers_after"] = checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_agg(id ORDER BY id) FROM workbench_readings`)
	for i := 2; i < 6; i++ {
		if countsAfter[i] != countsBefore[i] {
			t.Fatalf("unexpected table count change index %d", i)
		}
	}
	if createErr != nil {
		report["zero_residual"] = planCacheSameJSON(countsAfter, before) && planCacheSameJSON(report["headers_after"], report["headers_before"].(json.RawMessage))
		if report["zero_residual"] != true {
			t.Fatal("failed creation has residual state")
		}
	} else {
		if countsAfter[0] != 20 || countsAfter[1] != 6000000 || reading.Coverage.KnownItems != 300000 || reading.MembershipRef != baseline.MembershipRef {
			t.Fatal("committed count/ref mismatch")
		}
		coverageRaw, _ := json.Marshal(baseline.Coverage)
		if !planCacheSameJSON(reading.Coverage, coverageRaw) {
			t.Fatal("coverage baseline mismatch")
		}
		got, err := store.GetWorkbenchReading(ctx, fixture.Subject, reading.ID, fixture.Cutoff.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		report["reading_readback"] = got
		if got.ID != reading.ID || got.MembershipRef != reading.MembershipRef || !planCacheSameJSON(got.Coverage, coverageRaw) {
			t.Fatal("stored reading mismatch")
		}
		var members int64
		if err = store.db.QueryRowContext(ctx, `SELECT count(*) FROM workbench_reading_members WHERE reading_id=$1`, reading.ID).Scan(&members); err != nil || members != 300000 {
			t.Fatalf("new members %d %v", members, err)
		}
		report["new_reading_members"] = members
		report["semantic_baseline"] = baseline
	}
	report["mode_after"] = planCacheJSON(t, ctx, store.db, `SELECT to_jsonb(current_setting('plan_cache_mode'))`)
	if string(report["mode_after"].(json.RawMessage)) != `"auto"` {
		t.Fatal("mode changed")
	}
	report["resources_after_verification"] = planCacheResource(t, ctx)
	report["complete"] = true
}
