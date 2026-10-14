package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestWorkbenchReadModeTextArrayEncoding(t *testing.T) {
	// QueryExecModeExec infers text[] from []string and uses text format. Exercise
	// the actual pgx codec rather than assuming CacheStatement encoding is equal.
	m := pgtype.NewMap()
	for _, values := range [][]string{nil, {}, {"project-300k"}, {"quote'value", `a"b`, `a\b`, "comma,value", "{brace}", "中文"}} {
		normalized := pgTextArray(values)
		b, err := m.Encode(pgtype.TextArrayOID, pgtype.TextFormatCode, normalized, nil)
		if err != nil {
			t.Fatal(err)
		}
		var decoded []string
		if err = m.Scan(pgtype.TextArrayOID, pgtype.TextFormatCode, b, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(normalized, decoded) {
			t.Fatalf("array roundtrip %v -> %v", normalized, decoded)
		}
	}
}

type readModeTraceKey struct{}
type readModeTrace struct {
	mu      sync.Mutex
	entries []map[string]any
}

func (r *readModeTrace) TraceQueryStart(ctx context.Context, c *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	args := make([]any, len(d.Args))
	for i, a := range d.Args {
		if mode, ok := a.(pgx.QueryExecMode); ok {
			args[i] = map[string]string{"driver_query_mode": mode.String()}
		} else {
			args[i] = a
		}
	}
	index := len(r.entries)
	r.entries = append(r.entries, map[string]any{"pid": c.PgConn().PID(), "sql": d.SQL, "arguments": args, "started_at": time.Now().UTC()})
	return context.WithValue(ctx, readModeTraceKey{}, index)
}
func (r *readModeTrace) TraceQueryEnd(ctx context.Context, c *pgx.Conn, d pgx.TraceQueryEndData) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index, ok := ctx.Value(readModeTraceKey{}).(int)
	if !ok {
		return
	}
	e := r.entries[index]
	e["ended_at"] = time.Now().UTC()
	e["command_tag"] = d.CommandTag.String()
	if d.Err != nil {
		e["error"] = d.Err.Error()
		var pe *pgconn.PgError
		if errors.As(d.Err, &pe) {
			e["sqlstate"] = pe.Code
		}
	}
}
func (r *readModeTrace) snapshot() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]map[string]any, len(r.entries))
	for i, e := range r.entries {
		result[i] = map[string]any{}
		for k, v := range e {
			result[i][k] = v
		}
	}
	return result
}

func readModeSession(t *testing.T, ctx context.Context, db *sql.DB) map[string]any {
	t.Helper()
	var pid int
	var mode string
	if err := db.QueryRowContext(ctx, "SELECT pg_backend_pid(),current_setting('plan_cache_mode')").Scan(&pid, &mode); err != nil {
		t.Fatal(err)
	}
	return map[string]any{"pid": pid, "mode": mode}
}
func readModeSetSession(t *testing.T, ctx context.Context, db *sql.DB, mode string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, "SELECT set_config('plan_cache_mode',$1,false)", mode); err != nil {
		t.Fatal(err)
	}
}

func TestWorkbenchReadModeCalibration(t *testing.T) {
	if os.Getenv("XINSIGHT_A0_READ_MODE") != "1" {
		t.Skip("bounded synthetic read-mode calibration only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	report := map[string]any{"started_at": time.Now().UTC(), "complete": false, "qualification": false, "samples": []map[string]any{}, "scope": "Four representative actual-creation pairs plus two empty restoration creates and one cancelled small create; no 500k, new copy or qualification"}
	out := os.Getenv("XINSIGHT_A0_READ_MODE_OUTPUT")
	trace := &readModeTrace{}
	defer func() {
		report["finished_at"] = time.Now().UTC()
		report["trace"] = trace.snapshot()
		checkpointWriteJSON(t, out, report)
	}()
	var original struct {
		Fixture checkpointFixture `json:"fixture"`
		Samples []struct {
			Success bool              `json:"success"`
			Reading workbench.Reading `json:"reading"`
		} `json:"samples"`
	}
	b, err := os.ReadFile(os.Getenv("XINSIGHT_A0_PLAN_ORIGINAL"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &original); err != nil {
		t.Fatal(err)
	}
	fixture := original.Fixture
	report["fixture"] = fixture
	admin := openPerformanceSQL(t, os.Getenv("XINSIGHT_POSTGRES_DSN"))
	defer admin.Close()
	u, err := url.Parse(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User("xinsight_workspace")
	config, err := pgx.ParseConfig(u.String())
	if err != nil {
		t.Fatal(err)
	}
	if config.DefaultQueryExecMode != pgx.QueryExecModeCacheStatement || config.StatementCacheCapacity != 512 {
		t.Fatal("unexpected pgx defaults")
	}
	config.Tracer = trace
	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	store := NewWorkspaceStore(db)
	session := readModeSession(t, ctx, db)
	report["session_before"] = session
	if session["mode"] != "auto" {
		t.Fatal("unexpected entry mode")
	}
	identity := planCacheJSON(t, ctx, db, `SELECT jsonb_build_object('session',session_user,'user',current_user,'superuser',(SELECT rolsuper FROM pg_roles WHERE rolname=current_user),'timeout',current_setting('statement_timeout'),'facts_update',has_table_privilege(current_user,'workbench_economic_facts','UPDATE'),'members_insert',has_table_privilege(current_user,'workbench_reading_members','INSERT'))`)
	report["identity"] = identity
	var role struct {
		Session string `json:"session"`
		User    string `json:"user"`
		Super   bool   `json:"superuser"`
		Timeout string `json:"timeout"`
		Facts   bool   `json:"facts_update"`
		Members bool   `json:"members_insert"`
	}
	if err = json.Unmarshal(identity, &role); err != nil {
		t.Fatal(err)
	}
	if role.Session != "xinsight_workspace" || role.User != role.Session || role.Super || role.Timeout != "10s" || role.Facts || !role.Members {
		t.Fatal("wrong workspace role")
	}
	settings := checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_object_agg(name,setting) FROM pg_settings WHERE name=ANY(ARRAY['session_preload_libraries','shared_buffers','work_mem','wal_buffers','max_wal_size','fsync','full_page_writes','synchronous_commit','autovacuum'])`)
	report["settings"] = settings
	var values map[string]string
	if err = json.Unmarshal(settings, &values); err != nil {
		t.Fatal(err)
	}
	for k, w := range map[string]string{"session_preload_libraries": "", "shared_buffers": "16384", "work_mem": "4096", "wal_buffers": "512", "max_wal_size": "1024", "fsync": "on", "full_page_writes": "on", "synchronous_commit": "on", "autovacuum": "on"} {
		if values[k] != w {
			t.Fatalf("setting %s=%s", k, values[k])
		}
	}
	counts := func() json.RawMessage { return checkpointQueryJSON(t, ctx, admin, autoExplainCountsSQL) }
	before := counts()
	report["counts_before"] = before
	var startCounts []int64
	if err = json.Unmarshal(before, &startCounts); err != nil || !reflect.DeepEqual(startCounts, []int64{20, 6000000, 0, 0, 500020, 500100}) {
		t.Fatalf("wrong 6M prestate %s", before)
	}
	report["headers_before"] = checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_agg(id ORDER BY id) FROM workbench_readings`)
	report["stats_before"] = planCacheStats(t, ctx, db)
	report["storage_before"] = checkpointSnapshot(t, ctx, admin)
	report["prepared_before"] = planCacheJSON(t, ctx, db, autoExplainPreparedSQL)
	requestFor := func(n int64) workbench.ReadingRequest {
		from, to := fixture.Cutoff.Add(-30*24*time.Hour), fixture.Cutoff
		switch n {
		case 0:
			from = fixture.Cutoff.Add(-40 * 24 * time.Hour)
			to = from.Add(24 * time.Hour)
		case 16:
			from = fixture.Cutoff.Add(-16 * time.Second)
		case 70000:
			from = fixture.Cutoff.Add(-7 * 24 * time.Hour)
		}
		return explicitPerformanceRequest(from, to, []string{fixture.Source}, []string{"project-300k"})
	}
	type arm struct {
		N       int64
		Generic bool
	}
	arms := []arm{{0, false}, {0, true}, {16, true}, {16, false}, {70000, false}, {70000, true}, {300000, true}, {300000, false}}
	refs := map[int64]string{}
	coverages := map[int64]json.RawMessage{}
	samples := []map[string]any{}
	largeStarted := false
	for i, arm := range arms {
		res := planCacheResource(t, ctx)
		if arm.N >= 70000 && !largeStarted {
			var x struct {
				New  int64 `json:"new_bytes"`
				Free int64 `json:"free_bytes"`
			}
			if err = json.Unmarshal(res, &x); err != nil || 4480*1024*1024-x.New < 512*1024*1024 || x.Free < 43*1024*1024*1024/2 {
				t.Fatalf("first large reserve: %s", res)
			}
			largeStarted = true
		}
		request := requestFor(arm.N)
		phases := []map[string]any{}
		sample := map[string]any{"index": i, "known_expected": arm.N, "generic_requested": arm.Generic, "request": request, "resources_before": res, "session_before": readModeSession(t, ctx, db), "started_at": time.Now().UTC(), "trace_start": len(trace.snapshot())}
		samples = append(samples, sample)
		report["samples"] = samples
		checkpointWriteJSON(t, out, report)
		started := time.Now()
		reading, createErr := store.createWorkbenchReadingWithPlan(ctx, fixture.Subject, request, fixture.Cutoff, func(name string, d time.Duration) {
			phases = append(phases, map[string]any{"name": name, "duration_ns": d.Nanoseconds()})
		}, func(n int64) bool {
			if n != arm.N {
				t.Errorf("actual members %d expected %d", n, arm.N)
			}
			return arm.Generic
		})
		sample["duration_ns"] = time.Since(started).Nanoseconds()
		sample["phases"] = phases
		sample["reading"] = reading
		sample["success"] = createErr == nil
		sample["trace_end"] = len(trace.snapshot())
		if createErr != nil {
			sample["error"] = createErr.Error()
			sample["counts_after_failure"] = counts()
			t.Fatalf("arm %d failed without replacement: %v", i, createErr)
		}
		after := readModeSession(t, ctx, db)
		sample["session_after"] = after
		if after["pid"] != session["pid"] || after["mode"] != "auto" {
			t.Fatal("connection/mode leaked after creation")
		}
		if reading.Coverage.KnownItems != arm.N {
			t.Fatal("wrong known items")
		}
		if old, ok := refs[arm.N]; ok {
			if old != reading.MembershipRef {
				t.Fatal("mode changed membership ref")
			}
			c, _ := json.Marshal(reading.Coverage)
			if !planCacheSameJSON(reading.Coverage, coverages[arm.N]) {
				t.Fatalf("mode changed coverage %s", c)
			}
		} else {
			refs[arm.N] = reading.MembershipRef
			coverages[arm.N], _ = json.Marshal(reading.Coverage)
		}
		got, err := store.GetWorkbenchReading(ctx, fixture.Subject, reading.ID, fixture.Cutoff.Add(time.Second))
		if err != nil || got.MembershipRef != reading.MembershipRef || !reflect.DeepEqual(got.Coverage, reading.Coverage) {
			t.Fatalf("readback %v", err)
		}
		sample["readback"] = got
		summary, err := store.GetWorkbenchSummary(ctx, fixture.Subject, reading.ID, fixture.Cutoff.Add(time.Second))
		if err != nil || summary.RequestCount != arm.N || summary.GatewayChargedMicros != fmt.Sprint(arm.N) || summary.CalculatedReferenceMicros != fmt.Sprint(arm.N) {
			t.Fatalf("money/count summary %v %+v", err, summary)
		}
		sample["summary"] = summary
		sample["ordinal_check"] = planCacheJSON(t, ctx, db, `SELECT jsonb_build_object('members',count(*),'min',min(ordinal),'max',max(ordinal),'mismatches',count(*) FILTER(WHERE ordinal<>expected)) FROM (SELECT m.ordinal,row_number() OVER(ORDER BY f.occurred_at,f.id) expected FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE m.reading_id=$1) q`, reading.ID)
		var ord struct {
			Members    int64 `json:"members"`
			Mismatches int64 `json:"mismatches"`
		}
		if err = json.Unmarshal(sample["ordinal_check"].(json.RawMessage), &ord); err != nil || ord.Members != arm.N || ord.Mismatches != 0 {
			t.Fatal("ordinal mismatch")
		}
		sample["prepared_after"] = planCacheJSON(t, ctx, db, autoExplainPreparedSQL)
		sample["resources_after"] = planCacheResource(t, ctx)
		checkpointWriteJSON(t, out, report)
	}
	// Independent expected membership from immutable F0 and time/project predicates,
	// with no member-table dependency or generic/custom mode variation.
	for n, ref := range refs {
		request := requestFor(n)
		period, err := request.Resolve(fixture.Cutoff)
		if err != nil {
			t.Fatal(err)
		}
		var expected string
		if err = db.QueryRowContext(ctx, `SELECT 'sha256:'||encode(pg_catalog.sha256(convert_to(COALESCE(string_agg(id::text||':'||source_event_hash||':'||event_hash,E'\n' ORDER BY occurred_at,id),''),'UTF8')),'hex') FROM workbench_economic_facts WHERE source_system_id=$1::uuid AND source_project_ref='project-300k' AND occurred_at >= $2 AND occurred_at < $3`, fixture.Source, period.StartInclusive, period.EndExclusive).Scan(&expected); err != nil || expected != ref {
			t.Fatalf("independent reference n=%d got=%s expected=%s err=%v", n, ref, expected, err)
		}
	}
	report["independent_refs"] = refs
	// Count's text[] codec, empty filter and half-open time boundaries on real pgx.
	countChecks := []map[string]any{}
	for _, tc := range []struct {
		Name                     string
		Sources, Projects, Costs []string
		From, To                 time.Time
		Want                     int64
	}{
		{"empty_sources", nil, []string{"project-300k"}, nil, fixture.Cutoff.Add(-30 * 24 * time.Hour), fixture.Cutoff, 300000},
		{"exclusive_end", []string{fixture.Source}, []string{"project-300k"}, nil, fixture.Cutoff.Add(-16 * time.Second), fixture.Cutoff.Add(-time.Second), 15},
		{"quoted_array", []string{fixture.Source}, []string{"quote'value", `a"b`, `a\b`, "comma,value", "中文"}, nil, fixture.Cutoff.Add(-30 * 24 * time.Hour), fixture.Cutoff, 0},
		{"cost_filter", []string{fixture.Source}, nil, []string{"missing-cost"}, fixture.Cutoff.Add(-30 * 24 * time.Hour), fixture.Cutoff, 0},
	} {
		var got int64
		if err = db.QueryRowContext(ctx, workbenchEligibleCountSQL, pgx.QueryExecModeExec, fixture.Subject, tc.From, tc.To, pgTextArray(tc.Sources), pgTextArray(tc.Projects), pgTextArray(tc.Costs)).Scan(&got); err != nil || got != tc.Want {
			t.Fatalf("count %s=%d want %d %v", tc.Name, got, tc.Want, err)
		}
		countChecks = append(countChecks, map[string]any{"case": tc.Name, "got": got, "expected": tc.Want})
	}
	report["count_contract_checks"] = countChecks
	// Two empty Store creates cover restoring pre-existing custom/generic session
	// modes on the same physical connection. These are explicit additional samples.
	restoreChecks := []map[string]any{}
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		readModeSetSession(t, ctx, db, mode)
		state := readModeSession(t, ctx, db)
		reading, err := store.createWorkbenchReadingWithPlan(ctx, fixture.Subject, requestFor(0), fixture.Cutoff, nil, func(int64) bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		after := readModeSession(t, ctx, db)
		if !reflect.DeepEqual(state, after) {
			t.Fatal("entry mode not restored")
		}
		restoreChecks = append(restoreChecks, map[string]any{"entry": state, "after": after, "reading": reading})
	}
	readModeSetSession(t, ctx, db, "auto")
	report["mode_restore_success"] = restoreChecks
	// Context cancellation after the digest has completed, while generic is active.
	cancelBefore := counts()
	cancelCtx, stop := context.WithCancel(ctx)
	cancelled, createErr := store.createWorkbenchReadingWithPlan(cancelCtx, fixture.Subject, requestFor(16), fixture.Cutoff, func(name string, _ time.Duration) {
		if name == "membership_digest" {
			stop()
		}
	}, func(int64) bool { return true })
	stop()
	cancelAfter := counts()
	report["store_cancel"] = map[string]any{"reading": cancelled, "error": fmt.Sprint(createErr), "before": cancelBefore, "after": cancelAfter, "session_after": readModeSession(t, ctx, db)}
	if !errors.Is(createErr, context.Canceled) || string(cancelBefore) != string(cancelAfter) || readModeSession(t, ctx, db)["mode"] != "auto" {
		t.Fatal("cancel did not rollback/restore")
	}
	// Server-side cancellation preserves the physical connection and proves SET
	// LOCAL restoration after an aborted transaction, separately from driver cleanup.
	pinned, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var pinnedPID int
	if err = pinned.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pinnedPID); err != nil {
		t.Fatal(err)
	}
	tx, err := pinned.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = useWorkbenchGenericPlan(ctx, tx); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := tx.ExecContext(ctx, "SELECT pg_sleep(5) /* xi_read_mode_cancel */"); done <- err }()
	deadline := time.Now().Add(3 * time.Second)
	active := false
	for time.Now().Before(deadline) {
		var yes bool
		if err = admin.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND state='active' AND wait_event='PgSleep')", pinnedPID).Scan(&yes); err != nil {
			t.Fatal(err)
		}
		if yes {
			active = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !active {
		t.Fatal("cancel target did not become active")
	}
	var sent bool
	if err = admin.QueryRowContext(ctx, "SELECT pg_cancel_backend($1)", pinnedPID).Scan(&sent); err != nil || !sent {
		t.Fatal("cancel request failed")
	}
	cancelledSQL := <-done
	var pe *pgconn.PgError
	if !errors.As(cancelledSQL, &pe) || pe.Code != "57014" {
		t.Fatalf("wrong server cancellation %v", cancelledSQL)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var afterPID int
	var afterMode string
	if err = pinned.QueryRowContext(ctx, "SELECT pg_backend_pid(),current_setting('plan_cache_mode')").Scan(&afterPID, &afterMode); err != nil || afterPID != pinnedPID || afterMode != "auto" {
		t.Fatal("cancel connection/mode not restored")
	}
	if err = pinned.Close(); err != nil {
		t.Fatal(err)
	}
	report["server_cancel"] = map[string]any{"pid_before": pinnedPID, "pid_after": afterPID, "sqlstate": pe.Code, "mode_after": afterMode}
	deniedBefore := counts()
	_, err = store.CreateWorkbenchReading(ctx, "xs_read_mode_no_grant", requestFor(16), fixture.Cutoff)
	if !errors.Is(err, workbench.ErrScopeForbidden) || string(counts()) != string(deniedBefore) {
		t.Fatal("authorization boundary changed")
	}
	report["denied_subject_no_residual"] = true
	prepared := planCacheJSON(t, ctx, db, autoExplainPreparedSQL)
	report["prepared_after"] = prepared
	var statements []struct {
		SQL string `json:"statement"`
	}
	if err = json.Unmarshal(prepared, &statements); err != nil {
		t.Fatal(err)
	}
	for _, s := range statements {
		if s.SQL == workbenchEligibleCountSQL {
			t.Fatal("count unexpectedly uses named prepared cache")
		}
	}
	report["counts_after"] = counts()
	var finalCounts []int64
	if err = json.Unmarshal(report["counts_after"].(json.RawMessage), &finalCounts); err != nil || !reflect.DeepEqual(finalCounts, []int64{30, 6740032, 0, 0, 500020, 500100}) {
		t.Fatalf("unexpected final state %v %v", finalCounts, err)
	}
	report["headers_after"] = checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_agg(id ORDER BY id) FROM workbench_readings`)
	report["stats_after"] = planCacheStats(t, ctx, db)
	report["storage_after"] = checkpointSnapshot(t, ctx, admin)
	report["session_after"] = readModeSession(t, ctx, db)
	report["resources_after"] = planCacheResource(t, ctx)
	report["complete"] = true
}

func TestWorkbenchReadModePolicyBoundary(t *testing.T) {
	for _, tc := range []struct {
		Members int64
		Generic bool
	}{{0, false}, {16, false}, {69999, false}, {70000, true}, {70001, true}, {300000, true}, {500000, true}} {
		if got := workbenchReadingUseGenericPlan(tc.Members); got != tc.Generic {
			t.Fatalf("members=%d generic=%v", tc.Members, got)
		}
	}
}

func TestWorkbenchReadModeFinalCandidate(t *testing.T) {
	if os.Getenv("XINSIGHT_A0_READ_MODE_FINAL") != "1" {
		t.Skip("final ordinary-entry candidate verification only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	trace := &readModeTrace{}
	report := map[string]any{"started_at": time.Now().UTC(), "complete": false, "samples": []map[string]any{}, "qualification": false, "threshold_members": 70000}
	out := os.Getenv("XINSIGHT_A0_READ_MODE_OUTPUT")
	defer func() {
		report["trace"] = trace.snapshot()
		report["finished_at"] = time.Now().UTC()
		checkpointWriteJSON(t, out, report)
	}()
	var original struct {
		Fixture checkpointFixture `json:"fixture"`
	}
	b, err := os.ReadFile(os.Getenv("XINSIGHT_A0_PLAN_ORIGINAL"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &original); err != nil {
		t.Fatal(err)
	}
	fixture := original.Fixture
	report["fixture"] = fixture
	admin := openPerformanceSQL(t, os.Getenv("XINSIGHT_POSTGRES_DSN"))
	defer admin.Close()
	u, err := url.Parse(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User("xinsight_workspace")
	cfg, err := pgx.ParseConfig(u.String())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StatementCacheCapacity != 512 || cfg.DefaultQueryExecMode != pgx.QueryExecModeCacheStatement {
		t.Fatal("wrong pgx defaults")
	}
	cfg.Tracer = trace
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	store := NewWorkspaceStore(db)
	session := readModeSession(t, ctx, db)
	report["session_before"] = session
	if session["mode"] != "auto" {
		t.Fatal("wrong entry mode")
	}
	var user, timeout string
	var super bool
	if err = db.QueryRowContext(ctx, `SELECT current_user,current_setting('statement_timeout'),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)`).Scan(&user, &timeout, &super); err != nil || user != "xinsight_workspace" || timeout != "10s" || super {
		t.Fatal("wrong workspace identity")
	}
	settings := checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_object_agg(name,setting) FROM pg_settings WHERE name=ANY(ARRAY['session_preload_libraries','shared_buffers','work_mem','wal_buffers','max_wal_size','fsync','full_page_writes','synchronous_commit','autovacuum'])`)
	report["settings"] = settings
	var values map[string]string
	if err = json.Unmarshal(settings, &values); err != nil {
		t.Fatal(err)
	}
	for k, w := range map[string]string{"session_preload_libraries": "", "shared_buffers": "16384", "work_mem": "4096", "wal_buffers": "512", "max_wal_size": "1024", "fsync": "on", "full_page_writes": "on", "synchronous_commit": "on", "autovacuum": "on"} {
		if values[k] != w {
			t.Fatalf("setting %s=%s", k, values[k])
		}
	}
	before := checkpointQueryJSON(t, ctx, admin, autoExplainCountsSQL)
	report["counts_before"] = before
	var beforeCounts []int64
	if err = json.Unmarshal(before, &beforeCounts); err != nil || !reflect.DeepEqual(beforeCounts, []int64{30, 6740032, 0, 0, 500020, 500100}) {
		t.Fatalf("wrong final verification prestate %s", before)
	}
	report["stats_before"] = planCacheStats(t, ctx, db)
	report["storage_before"] = checkpointSnapshot(t, ctx, admin)
	samples := []map[string]any{}
	largeStarted := false
	for _, n := range []int64{0, 16, 69999, 70000, 300000} {
		res := planCacheResource(t, ctx)
		if n >= 69999 && !largeStarted {
			var v struct {
				New  int64 `json:"new_bytes"`
				Free int64 `json:"free_bytes"`
			}
			if err = json.Unmarshal(res, &v); err != nil || 4480*1024*1024-v.New < 512*1024*1024 || v.Free < 43*1024*1024*1024/2 {
				t.Fatalf("first large reserve %s", res)
			}
			largeStarted = true
		}
		from, to := fixture.Cutoff.Add(-time.Duration(n)*time.Second), fixture.Cutoff
		if n == 0 {
			from = fixture.Cutoff.Add(-40 * 24 * time.Hour)
			to = from.Add(24 * time.Hour)
		}
		if n == 300000 {
			from = fixture.Cutoff.Add(-30 * 24 * time.Hour)
		}
		request := explicitPerformanceRequest(from, to, []string{fixture.Source}, []string{"project-300k"})
		phases := []map[string]any{}
		sample := map[string]any{"expected_members": n, "expected_generic": workbenchReadingUseGenericPlan(n), "request": request, "resources_before": res, "trace_start": len(trace.snapshot()), "started_at": time.Now().UTC()}
		samples = append(samples, sample)
		report["samples"] = samples
		checkpointWriteJSON(t, out, report)
		started := time.Now()
		// This is the normal private entry used by CreateWorkbenchReading, with only
		// its existing duration observer. No test selector overrides the final policy.
		reading, err := store.createWorkbenchReading(ctx, fixture.Subject, request, fixture.Cutoff, func(name string, d time.Duration) {
			phases = append(phases, map[string]any{"name": name, "duration_ns": d.Nanoseconds()})
		})
		sample["duration_ns"] = time.Since(started).Nanoseconds()
		sample["phases"] = phases
		sample["reading"] = reading
		sample["success"] = err == nil
		sample["trace_end"] = len(trace.snapshot())
		if err != nil {
			sample["error"] = err.Error()
			sample["counts_after_failure"] = checkpointQueryJSON(t, ctx, admin, autoExplainCountsSQL)
			t.Fatalf("final n=%d failed without replacement: %v", n, err)
		}
		if reading.Coverage.KnownItems != n {
			t.Fatal("wrong members")
		}
		changed := false
		restored := false
		for _, p := range phases {
			changed = changed || p["name"] == "reading_plan_mode"
			restored = restored || p["name"] == "restore_reading_plan_mode"
		}
		if changed != workbenchReadingUseGenericPlan(n) || changed != restored {
			t.Fatal("wrong policy branch")
		}
		after := readModeSession(t, ctx, db)
		sample["session_after"] = after
		if !reflect.DeepEqual(after, session) {
			t.Fatal("mode/PID changed")
		}
		got, err := store.GetWorkbenchReading(ctx, fixture.Subject, reading.ID, fixture.Cutoff.Add(time.Second))
		if err != nil || got.MembershipRef != reading.MembershipRef || !reflect.DeepEqual(got.Coverage, reading.Coverage) {
			t.Fatal("stored result mismatch")
		}
		sample["readback"] = got
		summary, err := store.GetWorkbenchSummary(ctx, fixture.Subject, reading.ID, fixture.Cutoff.Add(time.Second))
		if err != nil || summary.RequestCount != n || summary.GatewayChargedMicros != fmt.Sprint(n) || summary.CalculatedReferenceMicros != fmt.Sprint(n) {
			t.Fatal("summary/money mismatch")
		}
		sample["summary"] = summary
		var expected string
		if err = db.QueryRowContext(ctx, `SELECT 'sha256:'||encode(pg_catalog.sha256(convert_to(COALESCE(string_agg(id::text||':'||source_event_hash||':'||event_hash,E'\n' ORDER BY occurred_at,id),''),'UTF8')),'hex') FROM workbench_economic_facts WHERE source_system_id=$1::uuid AND source_project_ref='project-300k' AND occurred_at >= $2 AND occurred_at < $3`, fixture.Source, from, to).Scan(&expected); err != nil || expected != reading.MembershipRef {
			t.Fatalf("independent ref mismatch %v", err)
		}
		sample["independent_ref"] = expected
		sample["prepared_after"] = planCacheJSON(t, ctx, db, autoExplainPreparedSQL)
		sample["resources_after"] = planCacheResource(t, ctx)
		checkpointWriteJSON(t, out, report)
	}
	report["counts_after"] = checkpointQueryJSON(t, ctx, admin, autoExplainCountsSQL)
	var finalCounts []int64
	if err = json.Unmarshal(report["counts_after"].(json.RawMessage), &finalCounts); err != nil || !reflect.DeepEqual(finalCounts, []int64{35, 7180047, 0, 0, 500020, 500100}) {
		t.Fatalf("wrong final counts %v %v", finalCounts, err)
	}
	report["stats_after"] = planCacheStats(t, ctx, db)
	report["storage_after"] = checkpointSnapshot(t, ctx, admin)
	report["resources_after"] = planCacheResource(t, ctx)
	report["complete"] = true
}
