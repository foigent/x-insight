package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// Internal identities from the original factory; no external fixture input.
type a0HotspotFixture struct {
	Database string    `json:"database"`
	Source   string    `json:"source_id"`
	Subject  string    `json:"subject"`
	Reading  string    `json:"reading_id"`
	Cutoff   time.Time `json:"cutoff"`
}

func TestWorkbenchA0ReadHotspotDiagnostic(t *testing.T) {
	if os.Getenv("XINSIGHT_A0_READ_HOTSPOT") != "1" {
		t.Skip("explicit single-pair diagnostic opt-in required")
	}
	if os.Getenv("XINSIGHT_A0_CHECKPOINT_MODE") != "" {
		t.Fatal("checkpoint mode cannot change diagnostic setup or cleanup")
	}
	if os.Getenv("XINSIGHT_POSTGRES_DSN") == "" {
		t.Fatal("XINSIGHT_POSTGRES_DSN required")
	}
	a0HotspotLog(t, "mode", map[string]any{"mode": "rebuild_original_setup", "qualification": false, "pair_calls": 2, "peak_calls": 0})
	runWorkbenchA0Performance(t, true)
}

type a0HotspotNotice struct {
	Stage   string `json:"stage"`
	PID     uint32 `json:"pid"`
	Message string `json:"message"`
}
type a0HotspotNotices struct {
	sync.Mutex
	stage string
	rows  []a0HotspotNotice
}

// The manager verified these exact settings on PG16.14 with xinsight_admin.
// Use the same callback/parser before seed, disable for setup, then enable for the pair.
const a0HotspotPlanSettings = `LOAD 'auto_explain';
SET client_min_messages=notice;
SET auto_explain.log_level=notice;
SET auto_explain.log_format=json;
SET auto_explain.log_analyze=on;
SET auto_explain.log_buffers=on;
SET auto_explain.log_wal=on;
SET auto_explain.log_settings=on;
SET auto_explain.log_timing=off;
SET auto_explain.log_nested_statements=off;
SET auto_explain.log_parameter_max_length=0;
SET auto_explain.sample_rate=1;
SET auto_explain.log_min_duration=0;`

func openA0HotspotStore(t *testing.T, ctx context.Context, dsn string) (*WorkspaceStore, *readModeTrace, *a0HotspotNotices, error) {
	t.Helper()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid diagnostic database DSN")
	}
	cfg.RuntimeParams["jit"] = "off"
	trace := &readModeTrace{}
	cfg.Tracer = trace
	notices := &a0HotspotNotices{stage: "setup_no_plans"}
	cfg.OnNotice = func(c *pgconn.PgConn, n *pgconn.Notice) {
		notices.Lock()
		row := a0HotspotNotice{notices.stage, c.PID(), n.Message}
		notices.rows = append(notices.rows, row)
		notices.Unlock()
		// Persist before a later query/test failure; never query or fail inside OnNotice.
		raw, _ := json.Marshal(row)
		t.Logf("A0 diagnostic stage=notice data=%s", raw)
	}
	pool := stdlib.OpenDB(*cfg)
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(30 * time.Minute)
	pool.SetConnMaxIdleTime(5 * time.Minute)
	if err := pool.PingContext(ctx); err != nil {
		pool.Close()
		return nil, nil, nil, err
	}
	a0HotspotLog(t, "plan_driver", map[string]any{"mode": cfg.DefaultQueryExecMode.String(), "statement_cache_capacity": cfg.StatementCacheCapacity, "plans_enabled": false})
	return NewWorkspaceStore(pool), trace, notices, nil
}

// The sequential factory retains its original pool and prepared statements.
// Fail if its existing connection cannot be bound; never resize or reopen it.
func a0HotspotConfigurePlans(t *testing.T, ctx context.Context, store *WorkspaceStore, notices *a0HotspotNotices, enabled bool) (uint32, error) {
	t.Helper()
	stats := store.db.Stats()
	if stats.OpenConnections != 1 || stats.InUse != 0 || stats.Idle != 1 {
		return 0, fmt.Errorf("cannot bind original Store connection: %+v", stats)
	}
	conn, err := store.db.Conn(ctx)
	if err != nil {
		return 0, err
	}
	settings, stage := a0HotspotPlanSettings, "plan_enable"
	if !enabled {
		settings = "SET auto_explain.log_min_duration=-1; SET auto_explain.log_analyze=off; RESET client_min_messages;"
		stage = "plan_disable"
	}
	var pid uint32
	notices.Lock()
	notices.stage = stage
	notices.Unlock()
	err = conn.Raw(func(dc any) error {
		c := dc.(*stdlib.Conn).Conn()
		pid = c.PgConn().PID()
		_, err := c.Exec(ctx, settings)
		return err
	})
	closeErr := conn.Close() // Release before ordinary Store methods acquire it.
	a0HotspotLog(t, stage, map[string]any{"pid": pid, "settings": settings, "pool": store.db.Stats(), "error": a0HotspotError(errors.Join(err, closeErr))})
	return pid, errors.Join(err, closeErr)
}

func a0HotspotPreseedProbe(t *testing.T, ctx context.Context, store *WorkspaceStore, trace *readModeTrace, notices *a0HotspotNotices) {
	t.Helper()
	pid, err := a0HotspotConfigurePlans(t, ctx, store, notices, true)
	if err != nil {
		t.Fatal(err)
	}
	var one int
	probeErr := a0HotspotCall(t, trace, notices, "preseed_select1", pid, func() error {
		return store.db.QueryRowContext(ctx, "SELECT 1").Scan(&one)
	})
	disabledPID, disableErr := a0HotspotConfigurePlans(t, ctx, store, notices, false)
	if probeErr != nil || disableErr != nil {
		t.Fatal(errors.Join(probeErr, disableErr))
	}
	var duration, analyze string
	var readPID uint32
	if err := store.db.QueryRowContext(ctx, "SELECT pg_backend_pid(),current_setting('auto_explain.log_min_duration'),current_setting('auto_explain.log_analyze')").Scan(&readPID, &duration, &analyze); err != nil {
		t.Fatal(err)
	}
	if one != 1 || pid != disabledPID || pid != readPID || duration != "-1" || analyze != "off" {
		t.Fatalf("preseed probe/disable mismatch: value=%d enabled_pid=%d disabled_pid=%d read_pid=%d duration=%q analyze=%q", one, pid, disabledPID, readPID, duration, analyze)
	}
	notices.Lock()
	notices.stage = "setup_no_plans"
	notices.Unlock()
	a0HotspotLog(t, "preseed_probe_complete", map[string]any{"pid": pid, "value": one, "log_min_duration": duration, "log_analyze": analyze, "qualification": false})
}

// Bind the actual NOTICE documents to completed pgx calls, without SQL replay.
// Parameters stay in the synthetic driver trace; server parameter logging is off.
func a0HotspotBindPlans(entries []map[string]any, notices []a0HotspotNotice, stage string, pid uint32) ([]map[string]any, error) {
	plans := []map[string]any{}
	for _, n := range notices {
		if n.Stage != stage || (pid != 0 && n.PID != pid) {
			return plans, fmt.Errorf("notice stage/PID changed")
		}
		at := strings.Index(n.Message, "plan:\n")
		if at < 0 {
			continue
		} // Retained as raw notice, not claimed as a plan.
		var doc map[string]any
		if err := json.Unmarshal([]byte(n.Message[at+len("plan:\n"):]), &doc); err != nil {
			return plans, fmt.Errorf("malformed plan JSON: %w", err)
		}
		sql, ok := doc["Query Text"].(string)
		node, okNode := doc["Plan"].(map[string]any)
		if !ok || !okNode {
			return plans, fmt.Errorf("missing plan/query text")
		}
		for _, key := range []string{"Actual Rows", "Actual Loops", "Shared Hit Blocks", "WAL Records"} {
			if _, ok := node[key]; !ok {
				return plans, fmt.Errorf("actual plan missing %s", key)
			}
		}
		plans = append(plans, map[string]any{"pid": n.PID, "sql": sql, "phase": a0HotspotPhase(sql), "document": doc})
	}
	used := make([]bool, len(plans))
	for _, e := range entries {
		actualPID, ok := e["pid"].(uint32)
		if !ok || (pid != 0 && actualPID != pid) {
			return plans, fmt.Errorf("trace PID changed")
		}
		if e["error"] != nil {
			continue
		} // A failed statement need not emit a completed plan.
		match := -1
		for i, p := range plans {
			if !used[i] && p["pid"] == actualPID && p["sql"] == e["sql"] {
				match = i
				break
			}
		}
		if match < 0 {
			return plans, fmt.Errorf("completed SQL lacks actual plan: %s", e["sql"])
		}
		used[match] = true
	}
	for _, ok := range used {
		if !ok {
			return plans, fmt.Errorf("unbound or duplicate plan")
		}
	}
	if len(plans) == 0 {
		return plans, fmt.Errorf("no actual execution plans captured")
	}
	return plans, nil
}
func a0HotspotError(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
func a0HotspotLog(t *testing.T, stage string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	// Normal go test -json output is the durable evidence channel. Its Output
	// fragments must be concatenated; no final report is needed to recover this.
	t.Logf("A0 diagnostic stage=%s data=%s", stage, raw)
}

func a0HotspotPhase(sql string) string {
	switch {
	case strings.Contains(sql, "count(DISTINCT") && strings.Contains(sql, "workbench_reading_members"):
		return "totals"
	case strings.Contains(sql, "bool_and(") && strings.Contains(sql, "GROUP BY") && strings.Contains(sql, "workbench_reading_members"):
		return "groups"
	case strings.Contains(sql, "workbench_reading_members"):
		return "summary"
	case strings.Contains(sql, "workbench_readings") || strings.Contains(sql, "workbench_access_grants") || strings.Contains(sql, "workbench_purpose_manifests") || strings.Contains(sql, "workbench_source_manifests"):
		return "reading_authorization"
	default:
		return "other"
	}
}

func a0HotspotCall(t *testing.T, trace *readModeTrace, notices *a0HotspotNotices, name string, pid uint32, call func() error) error {
	t.Helper()
	before := len(trace.snapshot())
	notices.Lock()
	notices.stage = name
	noticeStart := len(notices.rows)
	notices.Unlock()
	started := time.Now()
	callErr := call()
	elapsed := time.Since(started)
	entries := trace.snapshot()[before:]
	notices.Lock()
	rows := append([]a0HotspotNotice(nil), notices.rows[noticeStart:]...)
	notices.stage = "between_calls"
	notices.Unlock()
	phases := map[string]int64{}
	for _, e := range entries {
		sql, _ := e["sql"].(string)
		phase := a0HotspotPhase(sql)
		e["phase"] = phase
		from, ok1 := e["started_at"].(time.Time)
		to, ok2 := e["ended_at"].(time.Time)
		if ok1 && ok2 {
			ns := to.Sub(from).Nanoseconds()
			e["elapsed_ns"] = ns
			phases[phase] += ns
		}
	}
	plans, bindErr := a0HotspotBindPlans(entries, rows, name, pid)
	a0HotspotLog(t, name, map[string]any{"elapsed_ns": elapsed.Nanoseconds(), "phases_ns": phases, "error": a0HotspotError(callErr), "plan_binding_error": a0HotspotError(bindErr), "trace": entries, "plans": plans, "qualification": false})
	return errors.Join(callErr, bindErr)
}
func runA0HotspotPair(t *testing.T, ctx context.Context, store *WorkspaceStore, trace *readModeTrace, notices *a0HotspotNotices, f a0HotspotFixture) {
	t.Helper()
	started := time.Now()
	var database, user, timeout, jit, planMode string
	var super bool
	if err := store.db.QueryRowContext(ctx, `SELECT current_database(),current_user,current_setting('statement_timeout'),current_setting('jit'),current_setting('plan_cache_mode'),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)`).Scan(&database, &user, &timeout, &jit, &planMode, &super); err != nil {
		t.Fatal(err)
	}
	a0HotspotLog(t, "session", map[string]any{"database": database, "user": user, "superuser": super, "statement_timeout": timeout, "jit": jit, "plan_cache_mode": planMode})
	if database != f.Database {
		t.Fatal("internal fixture and connected database differ")
	}
	// Check the internally constructed full growth, not merely one small reading.
	var facts, evidence, headers, members, big, small, sourceFacts, seven, thirty, grants int64
	err := store.db.QueryRowContext(ctx, `WITH sizes AS (
 SELECT r.id,count(m.ordinal) n FROM workbench_readings r LEFT JOIN workbench_reading_members m ON m.reading_id=r.id GROUP BY r.id)
 SELECT (SELECT count(*) FROM workbench_economic_facts),(SELECT count(*) FROM evidence_events),
 count(*),coalesce(sum(n),0),count(*) FILTER(WHERE n=300000),count(*) FILTER(WHERE n=70000),
 (SELECT count(*) FROM workbench_economic_facts WHERE source_system_id=$1),
 (SELECT count(*) FROM workbench_economic_facts WHERE source_system_id=$1 AND occurred_at >= $2::timestamptz-interval '7 days' AND occurred_at < $2),
 (SELECT count(*) FROM workbench_economic_facts WHERE source_system_id=$1 AND source_project_ref='project-300k'),
 (SELECT count(*) FROM workbench_access_grants WHERE source_system_id=$1 AND active AND allow_summary)
 FROM sizes`, f.Source, f.Cutoff).Scan(&facts, &evidence, &headers, &members, &big, &small, &sourceFacts, &seven, &thirty, &grants)
	if err != nil {
		t.Fatal(err)
	}
	a0HotspotLog(t, "preflight", map[string]any{"facts": facts, "evidence": evidence, "headers": headers, "members": members, "readings_300k": big, "readings_70k": small, "source_facts": sourceFacts, "source_7d": seven, "source_300k_project": thirty, "summary_grants": grants, "elapsed_ms": time.Since(started).Milliseconds()})
	if facts != 500020 || evidence != 500100 || headers != 30 || members != 6700000 || big != 20 || small != 10 || sourceFacts != 500000 || seven != 70000 || thirty != 300000 || grants != 10 {
		t.Fatal("fixture does not match original pre-peak scale")
	}
	var count int64
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM workbench_reading_members WHERE reading_id=$1`, f.Reading).Scan(&count); err != nil || count != 70000 {
		t.Fatalf("target member count=%d err=%v", count, err)
	}
	reading, err := store.GetWorkbenchReading(ctx, f.Subject, f.Reading, f.Cutoff)
	if err != nil || !reading.Cutoff.Equal(f.Cutoff) || !reading.Period.StartInclusive.Equal(f.Cutoff.Add(-7*24*time.Hour)) || !reading.Period.EndExclusive.Equal(f.Cutoff) || reading.Coverage.KnownItems != 70000 {
		t.Fatalf("target reading identity/period/scope mismatch: %v", err)
	}
	pid, err := a0HotspotConfigurePlans(t, ctx, store, notices, true)
	if err != nil {
		t.Fatal(err)
	}
	var readPID uint32
	var pairDB, pairUser, pairTimeout, pairJIT, pairMode string
	if err := a0HotspotCall(t, trace, notices, "plan_preflight", pid, func() error {
		return store.db.QueryRowContext(ctx, `SELECT pg_backend_pid(),current_database(),current_user,current_setting('statement_timeout'),current_setting('jit'),current_setting('plan_cache_mode')`).Scan(&readPID, &pairDB, &pairUser, &pairTimeout, &pairJIT, &pairMode)
	}); err != nil {
		t.Fatal(err)
	}
	a0HotspotLog(t, "plan_session", map[string]any{"pid": pid, "database": pairDB, "user": pairUser, "statement_timeout": pairTimeout, "jit": pairJIT, "plan_cache_mode": pairMode})
	if readPID != pid || pairDB != database || pairUser != user || pairTimeout != timeout || pairJIT != jit || pairMode != planMode {
		t.Fatal("plan connection identity/settings differ from original Store")
	}
	// Exactly one actual Store summary and one actual Store top call, in this order.
	// Preflight warms metadata/pages; this diagnostic makes no cold-cache claim.
	var summary workbench.Summary
	if err := a0HotspotCall(t, trace, notices, "summary", pid, func() error {
		var e error
		summary, e = store.GetWorkbenchSummary(ctx, f.Subject, f.Reading, f.Cutoff)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if summary.RequestCount != 70000 || summary.GatewayChargedMicros != "70000" || summary.CalculatedReferenceMicros != "70000" {
		t.Fatal("summary changed synthetic totals")
	}
	var top workbench.Contributions
	if err := a0HotspotCall(t, trace, notices, "top", pid, func() error {
		var e error
		top, e = store.GetWorkbenchContributions(ctx, f.Subject, f.Reading, "model", 0, 10, "", f.Cutoff)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if top.DenominatorRequests != 70000 || top.DenominatorMicros != "70000" || len(top.Rows) != 10 {
		t.Fatal("top changed synthetic denominator/row count")
	}
	a0HotspotLog(t, "pair_complete", map[string]any{"qualification": false, "summary_calls": 1, "top_calls": 1, "peak_calls": 0, "pool": store.db.Stats(), "reading_id": f.Reading})
}

func TestWorkbenchA0HotspotNoticeBinding(t *testing.T) {
	const sql = "SELECT $1::int AS synthetic_notice_probe"
	const msg = "duration: 0.1 ms plan:\n" + `{"Query Text":"SELECT $1::int AS synthetic_notice_probe","Plan":{"Actual Rows":1,"Actual Loops":1,"Shared Hit Blocks":0,"WAL Records":0},"Settings":{"jit":"off"}}`
	entry := map[string]any{"pid": uint32(17), "sql": sql, "arguments": []any{1}}
	notice := a0HotspotNotice{"summary", 17, msg}
	if plans, err := a0HotspotBindPlans([]map[string]any{entry}, []a0HotspotNotice{notice}, "summary", 17); err != nil || len(plans) != 1 {
		t.Fatalf("valid binding: %v %v", plans, err)
	}
	for _, tc := range []struct {
		name string
		rows []a0HotspotNotice
	}{
		{"missing", nil},
		{"wrong_pid", []a0HotspotNotice{{"summary", 18, msg}}},
		{"wrong_stage", []a0HotspotNotice{{"top", 17, msg}}},
		{"wrong_sql", []a0HotspotNotice{{"summary", 17, strings.ReplaceAll(msg, "synthetic_notice_probe", "other")}}},
		{"malformed", []a0HotspotNotice{{"summary", 17, "plan:\n{"}}},
		{"not_analyzed", []a0HotspotNotice{{"summary", 17, strings.ReplaceAll(msg, `"Actual Rows":1,`, "")}}},
		{"duplicate", []a0HotspotNotice{notice, notice}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := a0HotspotBindPlans([]map[string]any{entry}, tc.rows, "summary", 17); err == nil {
				t.Fatal("invalid plan binding accepted")
			}
		})
	}
	t.Run("partial_failure_retains_plan", func(t *testing.T) {
		entries := []map[string]any{entry, {"pid": uint32(17), "sql": "SELECT cancelled_query", "error": "context deadline exceeded"}}
		plans, err := a0HotspotBindPlans(entries, []a0HotspotNotice{notice}, "summary", 17)
		if err != nil || len(plans) != 1 {
			t.Fatal("completed plan lost when later query failed", plans, err)
		}
	})
}

// Real groups SQL shapes: the captured A0 plan and the details short-circuit query.
func TestWorkbenchA0HotspotGroupsPhase(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"legacy_exists", `
SELECT f.model_ref, count(*), COALESCE(sum(gateway_charged_micros),0)::text,
       COALESCE(sum(gateway_charged_micros),0)::text,
       COALESCE(sum(calculated_reference_micros),0)::text,
       bool_and(EXISTS (
         SELECT 1 FROM workbench_access_grants g
         WHERE g.subject_id=$4 AND g.active AND g.allow_details
           AND g.source_system_id=f.source_system_id
           AND (g.source_project_ref IS NULL OR g.source_project_ref=f.source_project_ref)
           AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref)
       ))
FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE reading_id=$1 AND f.model_ref IS NOT NULL AND f.model_ref<>''
GROUP BY f.model_ref ORDER BY sum(gateway_charged_micros) DESC, f.model_ref LIMIT $2 OFFSET $3`},
		{"details_short_circuit", `
SELECT f.model_ref, count(*), COALESCE(sum(gateway_charged_micros),0)::text,
       COALESCE(sum(gateway_charged_micros),0)::text,
       COALESCE(sum(calculated_reference_micros),0)::text,
       bool_and(CASE WHEN EXISTS (
         SELECT 1 FROM workbench_access_grants detail_grant
         WHERE detail_grant.subject_id=$4 AND detail_grant.active AND detail_grant.allow_details
       ) THEN EXISTS (
         SELECT 1 FROM workbench_access_grants g
         WHERE g.subject_id=$4 AND g.active AND g.allow_details
           AND g.source_system_id=f.source_system_id
           AND (g.source_project_ref IS NULL OR g.source_project_ref=f.source_project_ref)
           AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref)
       ) ELSE false END)
FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE reading_id=$1 AND f.model_ref IS NOT NULL AND f.model_ref<>''
GROUP BY f.model_ref ORDER BY sum(gateway_charged_micros) DESC, f.model_ref LIMIT $2 OFFSET $3`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := a0HotspotPhase(tc.sql); got != "groups" {
				t.Fatalf("actual groups SQL classified as %q", got)
			}
		})
	}
}
