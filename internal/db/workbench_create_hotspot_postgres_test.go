package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

// Uses the existing factory, seed and exact cleanup, then ONE large Create.
// The 20 tiny visibility checks remain setup. This is not retained-growth/p95
// qualification, nor a comparison to an uninstrumented call's latency.
func TestWorkbenchA0CreateHotspotDiagnostic(t *testing.T) {
	if os.Getenv("XINSIGHT_A0_CREATE_HOTSPOT") != "1" {
		t.Skip("explicit one-create diagnostic opt-in required")
	}
	if os.Getenv("XINSIGHT_A0_CHECKPOINT_MODE") != "" || os.Getenv("XINSIGHT_A0_PERFORMANCE") != "" || os.Getenv("XINSIGHT_A0_READ_HOTSPOT") != "" {
		t.Fatal("create diagnostic cannot combine checkpoint, qualification or read-hotspot modes")
	}
	a0HotspotLog(t, "create_mode", map[string]any{"qualification": false, "large_create_calls": 1, "retained_growth": false, "peak_calls": 0, "wait_sample_interval_ms": 500})
	runWorkbenchA0Fixture(t, false, true)
}

// Read only the bound backend in this disposable database. No query text,
// parameters, other-session SQL, credentials or cluster settings are captured.
const a0CreateWaitSQL = `SELECT json_build_object(
 'observed_at',clock_timestamp(),'pid',pid,'backend_start',backend_start,
 'state',state,'wait_event_type',wait_event_type,'wait_event',wait_event,
 'query_start',query_start,'xact_start',xact_start,'blocking_pids',pg_blocking_pids(pid))
 FROM pg_stat_activity WHERE pid=$1 AND datname=current_database()`

func a0CreateProbePreflight(t *testing.T, ctx context.Context, admin *sql.DB, store *WorkspaceStore) {
	t.Helper()
	// Fail before the expensive seed if the already-supported session setting or
	// same-PID wait readback is unavailable. No installation, GRANT or global SET.
	if _, err := store.db.ExecContext(ctx, "SET auto_explain.log_triggers=on"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "SET auto_explain.log_timing=on"); err != nil {
		t.Fatal(err)
	}
	var pid uint32
	var version, triggers, timing string
	if err := store.db.QueryRowContext(ctx, "SELECT pg_backend_pid(),current_setting('server_version_num'),current_setting('auto_explain.log_triggers'),current_setting('auto_explain.log_timing')").Scan(&pid, &version, &triggers, &timing); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, "16") || triggers != "on" || timing != "on" {
		t.Fatal("PG16 trigger logging unavailable")
	}
	if _, err := store.db.ExecContext(ctx, "SET auto_explain.log_timing=off"); err != nil {
		t.Fatal(err)
	}
	var raw json.RawMessage
	if err := admin.QueryRowContext(ctx, a0CreateWaitSQL, pid).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	a0HotspotLog(t, "create_preseed_support", map[string]any{"pg_version_num": version, "log_triggers": triggers, "log_timing_supported": timing, "backend": raw, "scope": "session only"})
}

// Reuse the existing exact SQL/PID/NOTICE binder for the one member INSERT.
// BEGIN/COMMIT and other utility commands correctly have no execution plan.
func a0CreateMemberPlans(entries []map[string]any, rows []a0HotspotNotice, pid uint32) ([]map[string]any, error) {
	var memberEntries []map[string]any
	for _, e := range entries {
		if e["pid"] != pid {
			return nil, fmt.Errorf("creation trace changed backend PID")
		}
		if e["sql"] == workbenchMemberInsertSQL {
			memberEntries = append(memberEntries, e)
		}
	}
	if len(memberEntries) != 1 {
		return nil, fmt.Errorf("expected exactly one actual member INSERT, got %d", len(memberEntries))
	}
	var memberRows []a0HotspotNotice
	for _, n := range rows {
		if n.PID != pid || n.Stage != "create" {
			return nil, fmt.Errorf("creation NOTICE changed PID/stage")
		}
		at := strings.Index(n.Message, "plan:\n")
		if at < 0 {
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(n.Message[at+len("plan:\n"):]), &doc); err != nil {
			return nil, err
		}
		if doc["Query Text"] == workbenchMemberInsertSQL {
			memberRows = append(memberRows, n)
		}
	}
	plans, err := a0HotspotBindPlans(memberEntries, memberRows, "create", pid)
	if err != nil {
		return plans, err
	}
	doc := plans[0]["document"].(map[string]any)
	triggers, _ := doc["Triggers"].([]any)
	seen := map[string]bool{}
	for _, raw := range triggers {
		trigger, _ := raw.(map[string]any)
		name, _ := trigger["Constraint Name"].(string)
		if name != "workbench_reading_members_reading_id_fkey" && name != "workbench_reading_members_economic_fact_id_fkey" {
			continue
		}
		calls, _ := trigger["Calls"].(float64)
		duration, timed := trigger["Time"].(float64)
		if seen[name] || calls != 300000 || !timed || duration < 0 {
			return plans, fmt.Errorf("invalid member FK timing/call count")
		}
		seen[name] = true
	}
	if len(seen) != 2 {
		return plans, fmt.Errorf("actual INSERT lacks both FK timings; raw plan retained")
	}
	return plans, nil
}

func runA0CreateProbe(t *testing.T, ctx context.Context, admin *sql.DB, store *WorkspaceStore, trace *readModeTrace, notices *a0HotspotNotices, subject string, request workbench.ReadingRequest, cutoff time.Time) {
	t.Helper()
	var headers, members int64
	if err := admin.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM workbench_readings),(SELECT count(*) FROM workbench_reading_members)").Scan(&headers, &members); err != nil {
		t.Fatal(err)
	}
	if headers != 0 || members != 0 {
		t.Fatal("one-create diagnostic requires the factory's empty reading set")
	}
	pid, err := a0HotspotConfigurePlans(t, ctx, store, notices, true)
	if err != nil {
		t.Fatal(err)
	}
	// PG16 omits trigger Time when timing is off. Override the read-hotspot
	// helper only for this one Create; measured latency includes instrumentation.
	if _, err = store.db.ExecContext(ctx, "SET auto_explain.log_timing=on"); err != nil {
		t.Fatal(err)
	}
	// Preseed's log_triggers setting stays on this same original Store connection.
	var actualPID uint32
	var triggers, timing string
	if err = store.db.QueryRowContext(ctx, "SELECT pg_backend_pid(),current_setting('auto_explain.log_triggers'),current_setting('auto_explain.log_timing')").Scan(&actualPID, &triggers, &timing); err != nil || actualPID != pid || triggers != "on" || timing != "on" {
		t.Fatal("create connection/settings drift", err)
	}
	a0HotspotLog(t, "create_timing", map[string]any{"pid": pid, "log_timing": timing, "log_triggers": triggers, "qualification": false})
	observer, err := admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	var initial json.RawMessage
	if err = observer.QueryRowContext(ctx, a0CreateWaitSQL, pid).Scan(&initial); err != nil {
		t.Fatal(err)
	}
	a0HotspotLog(t, "create_wait_preflight", initial)
	// Exactly one read-only observer connection, bounded by the original fixture
	// deadline; no retries, backend cancellation or durable sampler state.
	waitCtx, stopWait := context.WithCancel(ctx)
	defer stopWait()
	waitDone := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-waitCtx.Done():
				waitDone <- nil
				return
			case <-ticker.C:
				queryCtx, cancel := context.WithTimeout(waitCtx, 2*time.Second)
				var raw json.RawMessage
				err := observer.QueryRowContext(queryCtx, a0CreateWaitSQL, pid).Scan(&raw)
				cancel()
				if err != nil {
					if waitCtx.Err() != nil {
						waitDone <- nil
						return
					}
					a0HotspotLog(t, "create_wait_error", map[string]any{"pid": pid, "error": a0HotspotError(err)})
					waitDone <- err
					return
				}
				a0HotspotLog(t, "create_wait", raw)
			}
		}
	}()
	traceStart := len(trace.snapshot())
	notices.Lock()
	notices.stage = "create"
	noticeStart := len(notices.rows)
	notices.Unlock()
	started := time.Now()
	phases := map[string]int64{}
	reading, callErr := store.createWorkbenchReading(ctx, subject, request, cutoff, func(phase string, duration time.Duration) {
		phases[phase] = duration.Nanoseconds()
		a0HotspotLog(t, "create_phase", map[string]any{"pid": pid, "phase": phase, "elapsed_ns": duration.Nanoseconds(), "ended_at": time.Now().UTC()})
	})
	elapsed := time.Since(started)
	stopWait()
	waitErr := <-waitDone // Join before pool/fixture cleanup; never leave a sampler running.
	entries := trace.snapshot()[traceStart:]
	notices.Lock()
	rows := append([]a0HotspotNotice(nil), notices.rows[noticeStart:]...)
	notices.stage = "between_calls"
	notices.Unlock()
	plans, bindErr := a0CreateMemberPlans(entries, rows, pid)
	// Raw NOTICE and phase/wait events are already durable in go test -json even
	// if this final record or the subsequent consistency check fails.
	a0HotspotLog(t, "create_complete", map[string]any{"qualification": false, "large_create_calls": 1, "pid": pid, "elapsed_ns": elapsed.Nanoseconds(), "phases_ns": phases, "trace": entries, "plans": plans, "create_error": a0HotspotError(callErr), "wait_error": a0HotspotError(waitErr), "binding_error": a0HotspotError(bindErr), "reading_id": reading.ID, "known_items": reading.Coverage.KnownItems, "pool": store.db.Stats()})
	disabledPID, disableErr := a0HotspotConfigurePlans(t, ctx, store, notices, false)
	if disableErr == nil && disabledPID != pid {
		disableErr = fmt.Errorf("disable plan PID changed")
	}
	if err := errors.Join(callErr, waitErr, bindErr, disableErr); err != nil {
		t.Fatal(err)
	}
	if reading.Coverage.KnownItems != 300000 {
		t.Fatal("selected member count changed")
	}
	var stored int64
	if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM workbench_reading_members WHERE reading_id=$1", reading.ID).Scan(&stored); err != nil || stored != 300000 {
		t.Fatal("committed membership mismatch", err)
	}
}

func TestWorkbenchA0CreateMemberPlanBinding(t *testing.T) {
	doc := map[string]any{"Query Text": workbenchMemberInsertSQL, "Plan": map[string]any{"Actual Rows": 0, "Actual Loops": 1, "Shared Hit Blocks": 0, "WAL Records": 1}, "Triggers": []any{
		map[string]any{"Constraint Name": "workbench_reading_members_reading_id_fkey", "Calls": 300000, "Time": 2.0},
		map[string]any{"Constraint Name": "workbench_reading_members_economic_fact_id_fkey", "Calls": 300000, "Time": 7.0},
	}}
	raw, _ := json.Marshal(doc)
	entry := map[string]any{"pid": uint32(17), "sql": workbenchMemberInsertSQL}
	row := a0HotspotNotice{Stage: "create", PID: 17, Message: "duration: 1 ms plan:\n" + string(raw)}
	utility := map[string]any{"pid": uint32(17), "sql": "commit"}
	if plans, err := a0CreateMemberPlans([]map[string]any{entry, utility}, []a0HotspotNotice{row}, 17); err != nil || len(plans) != 1 {
		t.Fatal(plans, err)
	}
	withoutTriggers := map[string]any{"Query Text": workbenchMemberInsertSQL, "Plan": doc["Plan"]}
	missingRaw, _ := json.Marshal(withoutTriggers)
	if plans, err := a0CreateMemberPlans([]map[string]any{entry}, []a0HotspotNotice{{Stage: "create", PID: 17, Message: "plan:\n" + string(missingRaw)}}, 17); err == nil || len(plans) != 1 {
		t.Fatal("missing FK timing must fail while retaining raw plan")
	}

	// PG16 TIMING OFF still reports Calls but omits Time. Reject that shape.
	for _, trigger := range doc["Triggers"].([]any) {
		delete(trigger.(map[string]any), "Time")
	}
	callsOnly, _ := json.Marshal(doc)
	if plans, err := a0CreateMemberPlans([]map[string]any{entry}, []a0HotspotNotice{{Stage: "create", PID: 17, Message: "plan:\n" + string(callsOnly)}}, 17); err == nil || len(plans) != 1 {
		t.Fatal("calls without timing must fail while retaining raw plan")
	}

	for _, tc := range []struct {
		name    string
		entries []map[string]any
		rows    []a0HotspotNotice
	}{
		{"missing_insert", []map[string]any{utility}, nil},
		{"repeated_insert", []map[string]any{entry, entry}, []a0HotspotNotice{row}},
		{"missing_plan", []map[string]any{entry}, nil},
		{"duplicate_plan", []map[string]any{entry}, []a0HotspotNotice{row, row}},
		{"wrong_pid", []map[string]any{entry}, []a0HotspotNotice{{Stage: "create", PID: 18, Message: row.Message}}},
		{"wrong_stage", []map[string]any{entry}, []a0HotspotNotice{{Stage: "summary", PID: 17, Message: row.Message}}},
		{"trace_pid_drift", []map[string]any{entry, {"pid": uint32(18), "sql": "commit"}}, []a0HotspotNotice{row}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := a0CreateMemberPlans(tc.entries, tc.rows, 17); err == nil {
				t.Fatal("bad binding accepted")
			}
		})
	}
}
