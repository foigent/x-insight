package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
	"github.com/jackc/pgx/v5/pgconn"
)

type checkpointFixture struct {
	Database string    `json:"database"`
	Source   string    `json:"source_id"`
	Subject  string    `json:"subject"`
	Cutoff   time.Time `json:"cutoff"`
}

func checkpointWriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func checkpointQueryJSON(t *testing.T, ctx context.Context, database *sql.DB, query string) json.RawMessage {
	t.Helper()
	var b []byte
	if err := database.QueryRowContext(ctx, query).Scan(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

func checkpointSnapshot(t *testing.T, ctx context.Context, database *sql.DB) json.RawMessage {
	return checkpointQueryJSON(t, ctx, database, `SELECT jsonb_build_object(
 'at',clock_timestamp(),'bgwriter',(SELECT to_jsonb(s) FROM pg_stat_bgwriter s),
 'wal',(SELECT to_jsonb(s) FROM pg_stat_wal s),
 'io',(SELECT jsonb_agg(to_jsonb(s)) FROM pg_stat_io s),
 'database',(SELECT to_jsonb(s) FROM pg_stat_database s WHERE datname=current_database()),
 'wal_directory_bytes',(SELECT sum(size) FROM pg_ls_waldir()),
 'storage',(SELECT jsonb_object_agg(n,jsonb_build_object('table',pg_table_size(n::regclass),'indexes',pg_indexes_size(n::regclass),'total',pg_total_relation_size(n::regclass))) FROM unnest(ARRAY['workbench_economic_facts','workbench_reading_members','workbench_readings']) n))`)
}

func TestWorkbenchCheckpointDiagnostic(t *testing.T) {
	mode := os.Getenv("XINSIGHT_A0_CHECKPOINT_MODE")
	if mode != "measure" && mode != "explain" && mode != "capacity" && mode != "qualify" {
		t.Skip("checkpoint experiment only")
	}
	limit := 5 * time.Minute
	if mode == "qualify" {
		limit = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	var fixture checkpointFixture
	b, err := os.ReadFile(os.Getenv("XINSIGHT_A0_CHECKPOINT_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	admin := openPerformanceSQL(t, os.Getenv("XINSIGHT_POSTGRES_DSN"))
	defer admin.Close()
	u, err := url.Parse(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User("xinsight_workspace") // Dedicated loopback trust fixture, actual restricted login.
	store, err := OpenWorkspaceStore(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.db.SetMaxOpenConns(1)
	store.db.SetMaxIdleConns(1)
	report := map[string]any{"mode": mode, "fixture": fixture, "qualification": false, "samples": []map[string]any{}}
	if mode == "qualify" {
		report["scope"] = "Store creation and capacity evidence only; not HTTP or A0 qualification"
	}
	output := os.Getenv("XINSIGHT_A0_CHECKPOINT_OUTPUT")
	defer func() { checkpointWriteJSON(t, output, report) }()
	identity := checkpointQueryJSON(t, ctx, store.db, `SELECT jsonb_build_object('session_user',session_user,'current_user',current_user,'statement_timeout',current_setting('statement_timeout'),'superuser',rolsuper,'connection_limit',rolconnlimit,'readonly',current_setting('default_transaction_read_only'),'facts_update',has_table_privilege(current_user,'workbench_economic_facts','UPDATE'),'members_insert',has_table_privilege(current_user,'workbench_reading_members','INSERT'),'search_path',current_setting('search_path')) FROM pg_roles WHERE rolname=current_user`)
	report["identity"] = identity
	var id struct {
		User          string `json:"current_user"`
		Session       string `json:"session_user"`
		Timeout       string `json:"statement_timeout"`
		Superuser     bool   `json:"superuser"`
		Limit         int    `json:"connection_limit"`
		FactsUpdate   bool   `json:"facts_update"`
		MembersInsert bool   `json:"members_insert"`
	}
	if err := json.Unmarshal(identity, &id); err != nil {
		t.Fatal(err)
	}
	if id.User != "xinsight_workspace" || id.Session != id.User || id.Timeout != "10s" || id.Superuser || id.Limit != 10 || id.FactsUpdate || !id.MembersInsert {
		t.Fatalf("wrong role: %s", identity)
	}
	settings := checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_object_agg(name,jsonb_build_object('setting',setting,'unit',unit,'source',source,'sourcefile',sourcefile,'pending_restart',pending_restart)) FROM pg_settings WHERE name=ANY(ARRAY['server_version','shared_buffers','work_mem','max_wal_size','min_wal_size','checkpoint_timeout','checkpoint_completion_target','wal_buffers','fsync','synchronous_commit','full_page_writes','wal_level','autovacuum','track_io_timing','track_wal_io_timing','log_checkpoints','wal_sync_method','wal_compression','checkpoint_flush_after','max_connections'])`)
	report["settings"] = settings
	var settingsMap map[string]struct {
		Setting string `json:"setting"`
	}
	if err := json.Unmarshal(settings, &settingsMap); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"shared_buffers": "16384", "work_mem": "4096", "max_wal_size": os.Getenv("XINSIGHT_A0_EXPECT_WAL_MB"), "checkpoint_timeout": "300", "checkpoint_completion_target": "0.9", "fsync": "on", "synchronous_commit": "on", "full_page_writes": "on", "wal_level": "replica", "autovacuum": "on", "track_io_timing": "on", "track_wal_io_timing": "on"} {
		if settingsMap[name].Setting != want {
			t.Fatalf("setting %s=%s want %s", name, settingsMap[name].Setting, want)
		}
	}
	report["constraints"] = checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_agg(jsonb_build_object('name',conname,'validated',convalidated,'definition',pg_get_constraintdef(oid)) ORDER BY conname) FROM pg_constraint WHERE conrelid IN ('workbench_economic_facts'::regclass,'workbench_reading_members'::regclass)`)
	report["triggers"] = checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_agg(jsonb_build_object('name',tgname,'enabled',tgenabled,'internal',tgisinternal,'definition',pg_get_triggerdef(oid)) ORDER BY tgname) FROM pg_trigger WHERE tgrelid IN ('workbench_economic_facts'::regclass,'workbench_reading_members'::regclass)`)
	request := explicitPerformanceRequest(fixture.Cutoff.Add(-30*24*time.Hour), fixture.Cutoff, []string{fixture.Source}, []string{"project-300k"})
	if mode == "explain" {
		var readingID string
		if err := admin.QueryRowContext(ctx, `SELECT id FROM workbench_readings ORDER BY created_at,id LIMIT 1`).Scan(&readingID); err != nil {
			t.Fatal(err)
		}
		report["before"] = checkpointSnapshot(t, ctx, admin)
		report["insert_explain"] = explainMemberInsert(t, ctx, store.db, readingID, fixture.Subject, request)
		report["after"] = checkpointSnapshot(t, ctx, admin)
		return
	}
	count, expected := 6, int64(300000)
	if mode == "qualify" {
		count = 20
	}
	if mode == "capacity" {
		count, expected = 1, 500000
		request.Filters.SourceProjects = nil
	}
	samples := make([]map[string]any, 0, count)
	durations := make([]time.Duration, 0, count)
	firstMembership := ""
	succeeded := 0
	var last workbench.Reading
	report["before"] = checkpointSnapshot(t, ctx, admin)
	cycleStart := time.Now()
	report["cycle_started_at"] = cycleStart.UTC()
	for i := 0; i < count; i++ {
		phases := make([]map[string]any, 0, 8)
		started := time.Now()
		reading, createErr := store.createWorkbenchReading(ctx, fixture.Subject, request, fixture.Cutoff, func(name string, d time.Duration) {
			phases = append(phases, map[string]any{"name": name, "duration_ns": d.Nanoseconds(), "completed_at": time.Now().UTC()})
		})
		ended := time.Now()
		sample := map[string]any{"index": i, "started_at": started.UTC(), "ended_at": ended.UTC(), "duration_ns": ended.Sub(started).Nanoseconds(), "phases": phases, "success": createErr == nil, "reading": reading}
		samples = append(samples, sample)
		report["samples"] = samples
		if createErr != nil {
			sample["error"] = createErr.Error()
			var e *pgconn.PgError
			if errors.As(createErr, &e) {
				sample["sqlstate"] = e.Code
			}
			sample["state_after_rollback"] = checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_build_object('headers',(SELECT count(*) FROM workbench_readings),'members',(SELECT count(*) FROM workbench_reading_members),'orphans',(SELECT count(*) FROM workbench_reading_members m LEFT JOIN workbench_readings r ON r.id=m.reading_id WHERE r.id IS NULL))`)
			report["after_failure"] = checkpointSnapshot(t, ctx, admin)
			checkpointWriteJSON(t, output, report)
			// These are the remaining predeclared attempts, never replacements for
			// failed samples. Only a cancelled SQL with verified rollback may continue.
			if mode == "qualify" && e != nil && e.Code == "57014" {
				var headers, members int64
				if err := admin.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM workbench_readings),(SELECT count(*) FROM workbench_reading_members)`).Scan(&headers, &members); err != nil || headers != int64(succeeded) || members != int64(succeeded)*expected {
					t.Fatalf("rollback state headers=%d members=%d successes=%d err=%v", headers, members, succeeded, err)
				}
				t.Logf("sample=%d failed duration_ms=%d SQLSTATE=%s; retained, no replacement", i, ended.Sub(started).Milliseconds(), e.Code)
				continue
			}
			t.Fatalf("sample %d failed: %v", i, createErr)
		}
		if reading.Coverage.KnownItems != expected {
			t.Fatalf("members %d != %d", reading.Coverage.KnownItems, expected)
		}
		last = reading
		succeeded++
		durations = append(durations, ended.Sub(started))
		if firstMembership == "" {
			firstMembership = reading.MembershipRef
		}
		if reading.MembershipRef != firstMembership {
			t.Fatal("membership changed across fixed-cutoff samples")
		}
		// Relation sizes are cheap metadata calls; no full-table validation or sleeps between samples.
		sample["storage"] = checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_build_object('members_table',pg_table_size('workbench_reading_members'),'members_indexes',pg_indexes_size('workbench_reading_members'),'wal_directory_bytes',(SELECT sum(size) FROM pg_ls_waldir()))`)
		checkpointWriteJSON(t, output, report)
		t.Logf("sample=%d success duration_ms=%d reading=%s", i, ended.Sub(started).Milliseconds(), reading.ID)
	}
	report["before_tail_checkpoint"] = checkpointSnapshot(t, ctx, admin)
	cpStart := time.Now()
	if _, err := admin.ExecContext(ctx, "CHECKPOINT"); err != nil {
		t.Fatal(err)
	}
	end := time.Now()
	report["tail_checkpoint"] = map[string]any{"started_at": cpStart.UTC(), "ended_at": end.UTC(), "duration_ns": end.Sub(cpStart).Nanoseconds()}
	report["cycle_duration_ns"] = end.Sub(cycleStart).Nanoseconds()
	report["after_tail_checkpoint"] = checkpointSnapshot(t, ctx, admin)
	if mode == "qualify" {
		report["creation_300k"] = map[string]any{"planned_attempts": 20, "attempted": len(samples), "successes": succeeded, "failures": len(samples) - succeeded, "success_latency": durationReport(durations), "qualified": succeeded == 20 && p95(durations) <= 5*time.Second, "percentile_method": "nearest_rank", "threshold_ms": 5000}
		if succeeded != 20 {
			report["capacity_skipped"] = "6M successfully committed members were not reached; failed attempts were not replaced"
			t.Fatalf("20-attempt workload retained %d successful readings; capacity precondition failed", succeeded)
		}
		before := checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_build_array((SELECT count(*) FROM workbench_readings),(SELECT count(*) FROM workbench_reading_members),(SELECT count(*) FROM workbench_saved_filters),(SELECT count(*) FROM workbench_saved_filter_operations),(SELECT count(*) FROM workbench_economic_facts),(SELECT count(*) FROM evidence_events))`)
		var counts []int64
		if err := json.Unmarshal(before, &counts); err != nil || counts[0] != 20 || counts[1] != 6000000 {
			t.Fatalf("capacity baseline %s err=%v", before, err)
		}
		capacity := map[string]any{"before_counts": before}
		report["capacity_500k"] = capacity
		request500 := request
		request500.Filters.SourceProjects = nil
		phases := make([]map[string]any, 0, 8)
		started := time.Now()
		reading500, capacityErr := store.createWorkbenchReading(ctx, fixture.Subject, request500, fixture.Cutoff, func(name string, d time.Duration) {
			phases = append(phases, map[string]any{"name": name, "duration_ns": d.Nanoseconds(), "completed_at": time.Now().UTC()})
		})
		ended := time.Now()
		capacity["started_at"], capacity["ended_at"], capacity["duration_ns"], capacity["phases"], capacity["reading"], capacity["success"] = started.UTC(), ended.UTC(), ended.Sub(started).Nanoseconds(), phases, reading500, capacityErr == nil
		capacity["after_counts"] = checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_build_array((SELECT count(*) FROM workbench_readings),(SELECT count(*) FROM workbench_reading_members),(SELECT count(*) FROM workbench_saved_filters),(SELECT count(*) FROM workbench_saved_filter_operations),(SELECT count(*) FROM workbench_economic_facts),(SELECT count(*) FROM evidence_events))`)
		if capacityErr != nil {
			capacity["error"] = capacityErr.Error()
			var e *pgconn.PgError
			if errors.As(capacityErr, &e) {
				capacity["sqlstate"] = e.Code
			}
			capacity["zero_residual"] = string(capacity["after_counts"].(json.RawMessage)) == string(before)
			if capacity["zero_residual"] != true {
				t.Fatal("500k failure retained partial state")
			}
		} else if reading500.Coverage.KnownItems != 500000 {
			t.Fatal("500k coverage does not match")
		}
		capacity["before_tail_checkpoint"] = checkpointSnapshot(t, ctx, admin)
		cpStarted := time.Now()
		if _, err := admin.ExecContext(ctx, "CHECKPOINT"); err != nil {
			t.Fatal(err)
		}
		capacity["tail_checkpoint"] = map[string]any{"started_at": cpStarted.UTC(), "ended_at": time.Now().UTC(), "duration_ns": time.Since(cpStarted).Nanoseconds()}
		capacity["after_tail_checkpoint"] = checkpointSnapshot(t, ctx, admin)
		// All stored readings retain contiguous ordinals and exact committed sizes.
		members := checkpointQueryJSON(t, ctx, admin, `SELECT jsonb_agg(to_jsonb(s) ORDER BY reading_id) FROM (SELECT reading_id,count(*) AS members,min(ordinal) AS first_ordinal,max(ordinal) AS last_ordinal FROM workbench_reading_members GROUP BY reading_id) s`)
		report["all_member_counts"] = members
		var rows []struct {
			ID      string `json:"reading_id"`
			Members int64  `json:"members"`
			First   int64  `json:"first_ordinal"`
			Last    int64  `json:"last_ordinal"`
		}
		if err := json.Unmarshal(members, &rows); err != nil {
			t.Fatal(err)
		}
		wantRows := 20
		if capacityErr == nil {
			wantRows++
		}
		if len(rows) != wantRows {
			t.Fatalf("stored readings %d != %d", len(rows), wantRows)
		}
		for _, r := range rows {
			n := int64(300000)
			if r.ID == reading500.ID {
				n = 500000
			}
			if r.Members != n || r.First != 1 || r.Last != n {
				t.Fatalf("noncontiguous reading %+v", r)
			}
		}
		if capacityErr != nil {
			t.Errorf("500k capacity failed without residual: %v", capacityErr)
		}
		if capacityErr == nil {
			started := time.Now()
			summary500, err := store.GetWorkbenchSummary(ctx, fixture.Subject, reading500.ID, fixture.Cutoff)
			capacity["summary"] = map[string]any{"duration_ns": time.Since(started).Nanoseconds(), "result": summary500}
			if err != nil || summary500.RequestCount != 500000 || summary500.SelectedAmountMicros != "500000" {
				t.Fatalf("500k summary: %+v err=%v", summary500, err)
			}
		}
		if p95(durations) > 5*time.Second {
			t.Errorf("300k nearest-rank p95 %s exceeds 5s", p95(durations))
		}
		checkpointWriteJSON(t, output, report)
	}
	var actual int64
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM workbench_reading_members WHERE reading_id=$1`, last.ID).Scan(&actual); err != nil || actual != expected {
		t.Fatalf("actual members %d expected %d err %v", actual, expected, err)
	}
	probe := map[string]any{}
	report["read_probe"] = probe
	started := time.Now()
	summary, err := store.GetWorkbenchSummary(ctx, fixture.Subject, last.ID, fixture.Cutoff)
	probe["summary"] = map[string]any{"duration_ns": time.Since(started).Nanoseconds(), "result": summary}
	if err != nil || summary.RequestCount != expected || summary.SelectedAmountMicros != fmt.Sprint(expected) {
		t.Fatalf("summary: %+v %v", summary, err)
	}
	started = time.Now()
	groups, err := store.GetWorkbenchContributions(ctx, fixture.Subject, last.ID, "model", 0, 10, "", fixture.Cutoff)
	probe["contributions"] = map[string]any{"duration_ns": time.Since(started).Nanoseconds(), "result": groups}
	if err != nil || groups.DenominatorMicros != fmt.Sprint(expected) || len(groups.Rows) == 0 {
		t.Fatalf("contributions: %v", err)
	}
	total := new(big.Int)
	for _, g := range groups.Rows {
		v, ok := new(big.Int).SetString(g.SelectedAmountMicros, 10)
		if !ok {
			t.Fatal("invalid contribution money")
		}
		total.Add(total, v)
	}
	for _, s := range []string{groups.OtherKnownMicros, groups.UnassignedMicros} {
		v, ok := new(big.Int).SetString(s, 10)
		if !ok {
			t.Fatal("invalid remainder money")
		}
		total.Add(total, v)
	}
	if total.String() != fmt.Sprint(expected) {
		t.Fatal("contribution conservation failed")
	}
	started = time.Now()
	series, err := store.GetWorkbenchSeries(ctx, fixture.Subject, last.ID, "amount", 3600, fixture.Cutoff)
	probe["series"] = map[string]any{"duration_ns": time.Since(started).Nanoseconds(), "result": series}
	if err != nil || len(series.Points) != 720 {
		t.Fatalf("series buckets=%d err=%v", len(series.Points), err)
	}
	total.SetInt64(0)
	nonempty := 0
	for _, p := range series.Points {
		v, ok := new(big.Int).SetString(p.KnownValue, 10)
		if !ok {
			t.Fatal("invalid series money")
		}
		total.Add(total, v)
		if v.Sign() != 0 {
			nonempty++
		}
	}
	if total.String() != fmt.Sprint(expected) {
		t.Fatal("series conservation failed")
	}
	probe["nonempty_hour_buckets"] = nonempty
	// Two actual cursor pages are a matched diagnostic probe, not full paging qualification.
	pages := make([]workbench.Details, 0, 2)
	cursor := ""
	seen := map[string]bool{}
	type expectedDetail struct {
		evidenceID, eventHash string
		ordinal               int64
		occurred              time.Time
	}
	detailExpected := map[string]expectedDetail{}
	if mode == "qualify" {
		_, label, ok := parseContributionRef(groups.Rows[0].Ref)
		if !ok {
			t.Fatal("invalid contribution ref")
		}
		rows, err := admin.QueryContext(ctx, `SELECT f.request_id,f.evidence_event_id::text,f.event_hash,m.ordinal,f.occurred_at,e.event_hash FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id JOIN evidence_events e ON e.id=f.evidence_event_id WHERE m.reading_id=$1 AND f.model_ref=$2 ORDER BY m.ordinal`, last.ID, label)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var req, anchor string
			var item expectedDetail
			if err := rows.Scan(&req, &item.evidenceID, &item.eventHash, &item.ordinal, &item.occurred, &anchor); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if item.eventHash != anchor {
				rows.Close()
				t.Fatal("anchored Evidence hash changed")
			}
			detailExpected[req] = item
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
	}
	lastOrdinal := int64(0)
	started = time.Now()
	for i := 0; i < 2 || mode == "qualify"; i++ {
		page, err := store.GetWorkbenchDetails(ctx, fixture.Subject, last.ID, groups.Rows[0].Ref, 100, cursor, fixture.Cutoff)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range page.Rows {
			if seen[row.RequestID] || row.GatewayChargedMicros != "1" || row.EventHash == "" {
				t.Fatal("detail invariant failed")
			}
			seen[row.RequestID] = true
			if mode == "qualify" {
				if row.AccountState != "unknown" || row.ApplicationState != "unknown" || row.PurposeState != "unknown" {
					t.Fatal("A0 interpretation changed")
				}
				item, ok := detailExpected[row.RequestID]
				if !ok || item.evidenceID != row.EvidenceEventID || item.eventHash != row.EventHash || !item.occurred.Equal(row.OccurredAt) || item.ordinal <= lastOrdinal {
					t.Fatal("detail reference/time/order mismatch")
				}
				lastOrdinal = item.ordinal
			}
		}
		pages = append(pages, page)
		cursor = page.NextCursor
		if mode == "qualify" && cursor != "" {
			if ordinal, ok := parseDetailCursor(cursor, last.ID, groups.Rows[0].Ref); !ok || ordinal != lastOrdinal {
				t.Fatal("invalid detail cursor")
			}
		}
		if cursor == "" {
			break
		}
	}
	key := "details_two_pages"
	if mode == "qualify" {
		key = "details_full_selected_group"
		if int64(len(seen)) != groups.Rows[0].RequestCount {
			t.Fatalf("detail group count %d expected %d", len(seen), groups.Rows[0].RequestCount)
		}
	}
	probe[key] = map[string]any{"duration_ns": time.Since(started).Nanoseconds(), "results": pages, "unique_rows": len(seen), "more_pages": cursor != ""}
	report["after_reads"] = checkpointSnapshot(t, ctx, admin)
	report["measurement_complete"] = true
	checkpointWriteJSON(t, filepath.Clean(output), report)
}
