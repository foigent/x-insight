package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestWorkbenchA0LocalPerformanceQualification is intentionally opt-in. It is
// a repeatable local qualification, not a CI timing assertion and not Target
// or Production evidence.
func TestWorkbenchA0LocalPerformanceQualification(t *testing.T) {
	if os.Getenv("XINSIGHT_A0_PERFORMANCE") != "1" {
		t.Skip("set XINSIGHT_A0_PERFORMANCE=1 for the XI-A0 local qualification")
	}
	runWorkbenchA0Performance(t, false)
}

// The diagnostic reuses only setup; its branch returns before the peak/capacity workload.
func runWorkbenchA0Performance(t *testing.T, readHotspot bool) {
	t.Helper()
	runWorkbenchA0Fixture(t, readHotspot, false)
}

func runWorkbenchA0Fixture(t *testing.T, readHotspot, createHotspot bool) {
	t.Helper()
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Fatal("XINSIGHT_POSTGRES_DSN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	dsn := performanceTestDSN(t, ctx, baseDSN)
	if _, err := RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	admin := openPerformanceSQL(t, dsn)
	defer admin.Close()
	var store *WorkspaceStore
	var trace *readModeTrace
	var notices *a0HotspotNotices
	var err error
	if readHotspot || createHotspot {
		store, trace, notices, err = openA0HotspotStore(t, ctx, dsn)
	} else {
		store, err = OpenWorkspaceStore(ctx, dsn)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if readHotspot || createHotspot {
		a0HotspotPreseedProbe(t, ctx, store, trace, notices)
	}
	if createHotspot {
		a0CreateProbePreflight(t, ctx, admin, store)
	}
	evidenceStore, err := OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer evidenceStore.Close()

	subject := "xs_8899aabbccddeeff"
	visibilitySourceID := performanceTestUUID(t)
	visibilityDurations := make([]time.Duration, 0, 20)
	for index := 0; index < 20; index++ {
		mapped := mapPerformanceFixture(t, visibilitySourceID, index, "1")
		started := time.Now()
		if _, err := evidenceStore.PersistEvents(ctx, mapped.Events); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			if _, err := admin.ExecContext(ctx, `
INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences)
VALUES($1,$2::uuid,true,true,true,true)`, subject, visibilitySourceID); err != nil {
				t.Fatal(err)
			}
		}
		request := explicitPerformanceRequest(
			time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
			[]string{visibilitySourceID}, nil,
		)
		reading, err := store.CreateWorkbenchReading(ctx, subject, request, time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		summary, err := store.GetWorkbenchSummary(ctx, subject, reading.ID, reading.Cutoff)
		if err != nil || summary.RequestCount != int64(index+1) {
			t.Fatalf("visibility summary=%+v err=%v", summary, err)
		}
		visibilityDurations = append(visibilityDurations, time.Since(started))
		if _, err := admin.ExecContext(ctx, `DELETE FROM workbench_readings WHERE id=$1`, reading.ID); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("A0 phase=visibility duration=%+v", durationReport(visibilityDurations))
	if maximumDuration(visibilityDurations) > 60*time.Second {
		t.Fatalf("local Evidence visibility exceeded 60s: %s", maximumDuration(visibilityDurations))
	}

	performanceSourceID := performanceTestUUID(t)
	cutoff := time.Now().UTC().Truncate(time.Second)
	seedStarted := time.Now()
	if _, err := admin.ExecContext(ctx, `
INSERT INTO source_systems(id,source_key,source_type,display_name,source_version,status)
VALUES($1::uuid,$2,'unirouter','XI-A0 local performance fixture','unirouter.completed-execution.v1','active')`, performanceSourceID, "source-system:"+performanceSourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `
INSERT INTO evidence_events(
  id,schema_version,source_system_id,adapter_manifest_id,source_type,source_version,
  source_event_type,source_ref,idempotency_key,event_type,stage,severity,capture_mode,
  occurred_at,observed_at,ingested_at,request_id,project_ref,payload_capture_mode,
  event_hash,source_event_hash,execution_id
)
SELECT gen_random_uuid(),'m0.1',$1::uuid,$2::uuid,'unirouter','unirouter.completed-execution.v1',
 'completed_execution','perf:'||g,'perf:'||g,'billing.execution.settled','billing','info','api_import',
 $3::timestamptz - CASE WHEN g<=70000 THEN (g % 604700)*interval '1 second'
   ELSE interval '8 days' + ((g-70000) % 1814400)*interval '1 second' END,
 $3::timestamptz,$3::timestamptz,'req-perf-'||g,
 CASE WHEN g<=300000 THEN 'project-300k' ELSE 'project-200k' END,
 'metadata_only','sha256:'||encode(digest('event:'||g,'sha256'),'hex'),
 'sha256:'||encode(digest('source:'||g,'sha256'),'hex'),
 'xe1_'||encode(digest($1::text||':'||g,'sha256'),'hex')
FROM generate_series(1,500000) g`, performanceSourceID, unirouter.DefaultCompletedExecutionAdapterManifestID, cutoff); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `
INSERT INTO workbench_economic_facts(
 source_system_id,source_version,source_ref,source_event_hash,evidence_event_id,
 request_id,execution_id,model_ref,source_project_ref,cost_project_ref,
 terminal_status,occurred_at,gateway_charged_micros,calculated_reference_micros,
 currency,amount_scale,projected_at,event_hash
)
SELECT source_system_id,source_version,source_ref,source_event_hash,id,request_id,
 execution_id,'model-'||(row_number() OVER (ORDER BY id) % 100),project_ref,NULL,
 'settled',occurred_at,1,1,'CNY',1000000,$2::timestamptz,event_hash
FROM evidence_events WHERE source_system_id=$1::uuid`, performanceSourceID, cutoff); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `
INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences)
VALUES($1,$2::uuid,true,true,true,true)`, subject, performanceSourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `ANALYZE evidence_events; ANALYZE workbench_economic_facts; ANALYZE workbench_access_grants`); err != nil {
		t.Fatal(err)
	}
	seedDuration := time.Since(seedStarted)
	var total, sevenDay, thirtyDay int64
	if err := admin.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE occurred_at >= $2::timestamptz-interval '7 days'),count(*) FILTER(WHERE source_project_ref='project-300k') FROM workbench_economic_facts WHERE source_system_id=$1::uuid`, performanceSourceID, cutoff).Scan(&total, &sevenDay, &thirtyDay); err != nil {
		t.Fatal(err)
	}
	if total != 500000 || sevenDay != 70000 || thirtyDay != 300000 {
		t.Fatalf("fixture total=%d seven=%d thirty=%d", total, sevenDay, thirtyDay)
	}
	if os.Getenv("XINSIGHT_A0_CHECKPOINT_MODE") == "seed" {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		checkpointWriteJSON(t, os.Getenv("XINSIGHT_A0_CHECKPOINT_OUTPUT"), map[string]any{
			"database": strings.TrimPrefix(parsed.Path, "/"), "source_id": performanceSourceID,
			"subject": subject, "cutoff": cutoff, "seed_ms": seedDuration.Milliseconds(),
			"source_facts": total, "seven_day_members": sevenDay, "selected_members": thirtyDay,
		})
		return
	}

	t.Logf("A0 phase=seed elapsed_ms=%d total=%d seven_day=%d thirty_day=%d", seedDuration.Milliseconds(), total, sevenDay, thirtyDay)
	request30 := explicitPerformanceRequest(cutoff.Add(-30*24*time.Hour), cutoff, []string{performanceSourceID}, []string{"project-300k"})
	if createHotspot {
		runA0CreateProbe(t, ctx, admin, store, trace, notices, subject, request30, cutoff)
		return // No 20x300k growth, extra INSERT EXPLAIN, 70k readers, peak or capacity run.
	}
	reading30Durations := make([]time.Duration, 0, 20)
	reading30Phases := make(map[string]time.Duration)
	firstReading30ID := ""
	for sample := 0; sample < 20; sample++ {
		started := time.Now()
		var reading workbench.Reading
		phases := make(map[string]time.Duration)
		reading, err = store.createWorkbenchReading(ctx, subject, request30, cutoff, func(name string, duration time.Duration) {
			phases[name] = duration
		})
		if sample == 0 {
			reading30Phases = phases
			firstReading30ID = reading.ID
		}
		elapsed := time.Since(started)
		a0HotspotLog(t, "creation_sample", map[string]any{"index": sample, "elapsed_ms": elapsed.Milliseconds(), "phases_ms": durationMapReport(phases), "error": a0HotspotError(err)})
		if err != nil {
			t.Fatal(err)
		}
		reading30Durations = append(reading30Durations, elapsed)
		// Retain every sample until the isolated test database is dropped. Deleting
		// each reading here makes the next sample contend with cascade cleanup and
		// autovacuum, which measures the harness rather than reading creation.
		_ = reading
	}
	reading30Qualified := p95(reading30Durations) <= 5*time.Second
	t.Logf("A0 phase=reading_300k duration=%+v first_phases=%+v qualified=%t", durationReport(reading30Durations), durationMapReport(reading30Phases), reading30Qualified)
	explainStarted := time.Now()
	memberInsertExplain := explainMemberInsert(t, ctx, admin, firstReading30ID, subject, request30)
	a0HotspotLog(t, "member_insert_explain", memberInsertExplain)

	t.Logf("A0 phase=member_insert_explain elapsed_ms=%d", time.Since(explainStarted).Milliseconds())
	request7 := explicitPerformanceRequest(cutoff.Add(-7*24*time.Hour), cutoff, []string{performanceSourceID}, []string{"project-300k"})
	reading7, err := store.CreateWorkbenchReading(ctx, subject, request7, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	summaryDurations := make([]time.Duration, 0, 20)
	topDurations := make([]time.Duration, 0, 20)
	for sample := 0; !readHotspot && sample < 20; sample++ {
		started := time.Now()
		if _, err := store.GetWorkbenchSummary(ctx, subject, reading7.ID, cutoff); err != nil {
			t.Fatal(err)
		}
		summaryDurations = append(summaryDurations, time.Since(started))
		started = time.Now()
		if _, err := store.GetWorkbenchContributions(ctx, subject, reading7.ID, "model", 0, 10, "", cutoff); err != nil {
			t.Fatal(err)
		}
		topDurations = append(topDurations, time.Since(started))
	}
	summaryQualified, topQualified := false, false
	if !readHotspot {
		summaryQualified = p95(summaryDurations) <= 2*time.Second
		topQualified = p95(topDurations) <= 2*time.Second
		t.Logf("A0 phase=summary_70k duration=%+v qualified=%t", durationReport(summaryDurations), summaryQualified)
		t.Logf("A0 phase=top_70k duration=%+v qualified=%t", durationReport(topDurations), topQualified)
	}
	readersStarted := time.Now()
	peakSubjects := []string{subject}
	peakReadingIDs := []string{reading7.ID}
	for reader := 1; reader < 10; reader++ {
		readerSubject := fmt.Sprintf("xs_%016x", reader)
		if _, err := admin.ExecContext(ctx, `
INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary)
VALUES($1,$2::uuid,true)`, readerSubject, performanceSourceID); err != nil {
			t.Fatal(err)
		}
		readerReading, err := store.CreateWorkbenchReading(ctx, readerSubject, request7, cutoff)
		if err != nil {
			t.Fatal(err)
		}
		peakSubjects = append(peakSubjects, readerSubject)
		peakReadingIDs = append(peakReadingIDs, readerReading.ID)
	}
	deadline, _ := ctx.Deadline()
	t.Logf("A0 phase=peak_readers_setup elapsed_ms=%d remaining_deadline_ms=%d pool=%+v", time.Since(readersStarted).Milliseconds(), time.Until(deadline).Milliseconds(), store.db.Stats())
	if readHotspot {
		var database string
		if err := admin.QueryRowContext(ctx, "SELECT current_database()").Scan(&database); err != nil {
			t.Fatal(err)
		}
		fixture := a0HotspotFixture{Database: database, Source: performanceSourceID, Subject: peakSubjects[1], Reading: peakReadingIDs[1], Cutoff: cutoff}
		a0HotspotLog(t, "fixture", fixture)
		runA0HotspotPair(t, ctx, store, trace, notices, fixture)
		return
	}
	peak := runWorkbenchPeak(t, ctx, store, peakSubjects, peakReadingIDs, cutoff)
	if len(peak.requestDurations) != 600 {
		t.Fatalf("peak calls=%d", len(peak.requestDurations))
	}

	request500 := explicitPerformanceRequest(cutoff.Add(-30*24*time.Hour), cutoff, []string{performanceSourceID}, nil)
	started500 := time.Now()
	reading500, err := store.CreateWorkbenchReading(ctx, subject, request500, cutoff)
	if err != nil {
		t.Fatalf("500k reading: %v", err)
	}
	duration500 := time.Since(started500)
	var members500 int64
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM workbench_reading_members WHERE reading_id=$1`, reading500.ID).Scan(&members500); err != nil || members500 != 500000 {
		t.Fatalf("500k members=%d err=%v", members500, err)
	}
	if _, err := admin.ExecContext(ctx, `
WITH inserted AS (
 INSERT INTO evidence_events(
  id,schema_version,source_system_id,adapter_manifest_id,source_type,source_version,
  source_event_type,source_ref,idempotency_key,event_type,stage,severity,capture_mode,
  occurred_at,observed_at,ingested_at,request_id,project_ref,payload_capture_mode,
  event_hash,source_event_hash,execution_id
 ) VALUES(
  gen_random_uuid(),'m0.1',$1::uuid,$2::uuid,'unirouter','unirouter.completed-execution.v1',
  'completed_execution','perf:500001','perf:500001','billing.execution.settled','billing','info','api_import',
  $3::timestamptz-interval '1 day',$3::timestamptz,$3::timestamptz,'req-perf-500001','project-200k','metadata_only',
  'sha256:'||encode(digest('event:500001','sha256'),'hex'),
  'sha256:'||encode(digest('source:500001','sha256'),'hex'),
  'xe1_'||encode(digest($1::text||':500001','sha256'),'hex')
 ) RETURNING *
)
INSERT INTO workbench_economic_facts(
 source_system_id,source_version,source_ref,source_event_hash,evidence_event_id,request_id,
 execution_id,model_ref,source_project_ref,terminal_status,occurred_at,
 gateway_charged_micros,calculated_reference_micros,currency,amount_scale,projected_at,event_hash
)
SELECT source_system_id,source_version,source_ref,source_event_hash,id,request_id,execution_id,
 'model-1',project_ref,'settled',occurred_at,1,1,'CNY',1000000,$3::timestamptz,event_hash FROM inserted`, performanceSourceID, unirouter.DefaultCompletedExecutionAdapterManifestID, cutoff); err != nil {
		t.Fatal(err)
	}
	var readingsBefore, readingsAfter int64
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM workbench_readings`).Scan(&readingsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWorkbenchReading(ctx, subject, request500, cutoff); !errors.Is(err, workbench.ErrTooLarge) {
		t.Fatalf("500001 reading error=%v", err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM workbench_readings`).Scan(&readingsAfter); err != nil || readingsAfter != readingsBefore {
		t.Fatalf("oversize left partial reading before=%d after=%d err=%v", readingsBefore, readingsAfter, err)
	}

	countExplain := explainJSON(t, ctx, admin, `
SELECT count(*) FROM workbench_economic_facts f
WHERE f.occurred_at >= $2 AND f.occurred_at < $3 AND f.currency='CNY'
  AND f.source_system_id::text=ANY($4::text[])
  AND f.source_project_ref=ANY($5::text[])
  AND EXISTS (SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$1 AND g.active AND g.allow_summary
    AND g.source_system_id=f.source_system_id
    AND (g.source_project_ref IS NULL OR g.source_project_ref=f.source_project_ref)
    AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref))`, subject, cutoff.Add(-30*24*time.Hour), cutoff, []string{performanceSourceID}, []string{"project-300k"})
	groupExplain := explainJSON(t, ctx, admin, `
SELECT model_ref,count(*),sum(gateway_charged_micros)
FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE reading_id=$1 AND model_ref IS NOT NULL AND model_ref<>''
GROUP BY model_ref ORDER BY sum(gateway_charged_micros) DESC,model_ref LIMIT 10`, reading7.ID)

	report := map[string]any{
		"schema_version":                    "xinsight.xi-a0-local-performance.v1",
		"evidence_boundary":                 "local isolated PostgreSQL 16 only; not Target, External Integration, or Production evidence",
		"qualified":                         reading30Qualified && summaryQualified && topQualified && peak.qualified(),
		"percentile_method":                 "nearest_rank",
		"fixture":                           map[string]any{"total_members": total, "seven_day_members": sevenDay, "thirty_day_members": thirtyDay},
		"seed_duration_ms":                  seedDuration.Milliseconds(),
		"visibility":                        withQualification(durationReport(visibilityDurations), maximumDuration(visibilityDurations) <= 60*time.Second, "max_ms <= 60000"),
		"reading_30_day_300k":               withQualification(durationReport(reading30Durations), reading30Qualified, "p95_ms <= 5000"),
		"reading_30_day_300k_first_phases":  durationMapReport(reading30Phases),
		"summary_7_day_70k":                 withQualification(durationReport(summaryDurations), summaryQualified, "p95_ms <= 2000"),
		"top_n_7_day_70k":                   withQualification(durationReport(topDurations), topQualified, "p95_ms <= 2000"),
		"peak_10_readers_20_rps_30_seconds": peak.report(),
		"reading_500k":                      map[string]any{"members": members500, "duration_ms": duration500.Milliseconds()},
		"reading_500001":                    map[string]any{"status": "rejected_too_large", "partial_state": false},
		"storage_bytes":                     workbenchStorage(t, ctx, admin),
		"postgres":                          postgresPerformanceIdentity(t, ctx, admin),
		"process":                           map[string]any{"goos": runtime.GOOS, "goarch": runtime.GOARCH, "logical_cpus": runtime.NumCPU(), "gomaxprocs": runtime.GOMAXPROCS(0)},
		"explain":                           map[string]any{"eligible_count": countExplain, "member_insert": memberInsertExplain, "top_n": groupExplain},
	}
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	payload = append(payload, '\n')
	if output := os.Getenv("XINSIGHT_A0_PERFORMANCE_OUTPUT"); output != "" {
		if !filepath.IsAbs(output) || filepath.Clean(output) != output {
			t.Fatal("XINSIGHT_A0_PERFORMANCE_OUTPUT must be clean and absolute")
		}
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("XI-A0 local performance report: %s", payload)
	if !reading30Qualified || !summaryQualified || !topQualified || !peak.qualified() {
		t.Fatalf("XI-A0 performance qualification failed: 30-day=%v summary=%v top_n=%v peak=%+v", durationReport(reading30Durations), durationReport(summaryDurations), durationReport(topDurations), peak.report())
	}
}

func workbenchStorage(t *testing.T, ctx context.Context, database *sql.DB) map[string]any {
	t.Helper()
	result := make(map[string]any)
	for _, relation := range []string{"workbench_economic_facts", "workbench_readings", "workbench_reading_members"} {
		var table, indexes, total int64
		if err := database.QueryRowContext(ctx, `
SELECT pg_relation_size($1::regclass),pg_indexes_size($1::regclass),pg_total_relation_size($1::regclass)`, relation).
			Scan(&table, &indexes, &total); err != nil {
			t.Fatal(err)
		}
		result[relation] = map[string]any{"table": table, "indexes": indexes, "total": total}
	}
	return result
}

func explicitPerformanceRequest(start, end time.Time, sources, sourceProjects []string) workbench.ReadingRequest {
	return workbench.ReadingRequest{
		ContractVersion: workbench.ContractVersion,
		Page:            "cost",
		MetricVersion:   workbench.MetricVersion,
		Period: workbench.PeriodInput{
			StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "UTC",
		},
		MoneyBasis: workbench.MoneyGatewayCharged,
		Currency:   "CNY",
		Filters: workbench.Filters{
			SourceIDs: append([]string{}, sources...), SourceProjects: append([]string{}, sourceProjects...), CostProjects: []string{},
		},
	}
}

type workbenchPeakRun struct {
	requestDurations []time.Duration
	queueDelays      []time.Duration
	endToEnd         []time.Duration
	elapsed          time.Duration
	readers          int
	readings         int
}

// The accepted A0 plan applies the same two-second Summary/TopN target
// under the original peak load, including queue delay. Serial success alone
// cannot mark this full qualification successful.
func (run workbenchPeakRun) qualified() bool {
	if len(run.endToEnd) != 600 {
		return false
	}
	summary, top := []time.Duration{}, []time.Duration{}
	for index, duration := range run.endToEnd {
		if index%2 == 0 {
			summary = append(summary, duration)
		} else {
			top = append(top, duration)
		}
	}
	return p95(summary) <= 2*time.Second && p95(top) <= 2*time.Second
}

func (run workbenchPeakRun) report() map[string]any {
	report := map[string]any{
		"qualified":         run.qualified(),
		"threshold":         "summary and top_n arrival-to-complete p95_ms <= 2000",
		"calls":             len(run.requestDurations),
		"summary_calls":     len(run.requestDurations) / 2,
		"top_n_calls":       len(run.requestDurations) / 2,
		"readers":           run.readers,
		"readings":          run.readings,
		"target_rps":        20,
		"target_seconds":    30,
		"actual_elapsed_ms": run.elapsed.Milliseconds(),
		"errors":            0,
		"request_latency":   durationReport(run.requestDurations),
		"queue_delay":       durationReport(run.queueDelays),
		"end_to_end":        durationReport(run.endToEnd),
	}
	if run.elapsed > 0 {
		report["achieved_rps"] = float64(len(run.requestDurations)) / run.elapsed.Seconds()
	}
	return report
}

func runWorkbenchPeak(
	t *testing.T,
	ctx context.Context,
	store *WorkspaceStore,
	subjects []string,
	readingIDs []string,
	now time.Time,
) workbenchPeakRun {
	t.Helper()
	if len(subjects) != 10 || len(readingIDs) != len(subjects) {
		t.Fatalf("peak readers=%d readings=%d", len(subjects), len(readingIDs))
	}
	type job struct {
		index       int
		scheduledAt time.Time
	}
	// Keep the original 600 calls / 10 workers / 20 arrivals per second.
	// A bounded arrival queue prevents slow workers from blocking the ticker
	// producer and silently moving the intended arrival times forward.
	jobs := make(chan job, 600)
	durations := make([]time.Duration, 600)
	queueDelays := make([]time.Duration, 600)
	endToEnd := make([]time.Duration, 600)
	errorsByIndex := make([]error, 600)
	var workers sync.WaitGroup
	for worker := 0; worker < 10; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				queueDelays[item.index] = time.Since(item.scheduledAt)
				started := time.Now()
				reader := (item.index / 2) % len(subjects)
				if item.index%2 == 0 {
					_, errorsByIndex[item.index] = store.GetWorkbenchSummary(ctx, subjects[reader], readingIDs[reader], now)
				} else {
					_, errorsByIndex[item.index] = store.GetWorkbenchContributions(ctx, subjects[reader], readingIDs[reader], "model", 0, 10, "", now)
				}
				durations[item.index] = time.Since(started)
				endToEnd[item.index] = time.Since(item.scheduledAt)
			}
		}()
	}
	peakStarted := time.Now()
	ticker := time.NewTicker(50 * time.Millisecond)
	for index := 0; index < 600; index++ {
		<-ticker.C
		scheduledAt := peakStarted.Add(time.Duration(index+1) * 50 * time.Millisecond)
		jobs <- job{index: index, scheduledAt: scheduledAt}
	}
	ticker.Stop()
	close(jobs)
	workers.Wait()
	// Persist timing diagnostics in the normal test log before any original
	// failure assertion. A deadline must not erase all preceding measurements.
	samples := make([]map[string]any, len(durations))
	errors := 0
	for index := range durations {
		errorText := ""
		if errorsByIndex[index] != nil {
			errors++
			errorText = errorsByIndex[index].Error()
		}
		samples[index] = map[string]any{"index": index, "request_ms": durations[index].Milliseconds(), "queue_ms": queueDelays[index].Milliseconds(), "arrival_to_complete_ms": endToEnd[index].Milliseconds(), "error": errorText}
	}
	raw, marshalErr := json.Marshal(map[string]any{"elapsed_ms": time.Since(peakStarted).Milliseconds(), "errors": errors, "samples": samples})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	t.Logf("A0 phase=peak raw=%s pool=%+v", raw, store.db.Stats())
	for index, err := range errorsByIndex {
		if err != nil {
			t.Fatalf("peak request %d: %v", index, err)
		}
	}
	return workbenchPeakRun{
		requestDurations: durations,
		queueDelays:      queueDelays,
		endToEnd:         endToEnd,
		elapsed:          time.Since(peakStarted),
		readers:          len(subjects),
		readings:         len(readingIDs),
	}
}

func durationMapReport(values map[string]time.Duration) map[string]int64 {
	result := make(map[string]int64, len(values))
	for name, duration := range values {
		result[name+"_ms"] = duration.Milliseconds()
	}
	return result
}

func durationReport(values []time.Duration) map[string]any {
	return map[string]any{
		"samples": len(values),
		"p50_ms":  percentile(values, 50).Milliseconds(),
		"p95_ms":  percentile(values, 95).Milliseconds(),
		"max_ms":  maximumDuration(values).Milliseconds(),
	}
}

func withQualification(report map[string]any, qualified bool, threshold string) map[string]any {
	report["qualified"] = qualified
	report["threshold"] = threshold
	return report
}

func p95(values []time.Duration) time.Duration { return percentile(values, 95) }

func percentile(values []time.Duration, value int) time.Duration {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]time.Duration{}, values...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
	index := (len(ordered)*value + 99) / 100
	if index < 1 {
		index = 1
	}
	return ordered[index-1]
}

func maximumDuration(values []time.Duration) time.Duration {
	var maximum time.Duration
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func explainJSON(t *testing.T, ctx context.Context, database interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, query string, args ...any) json.RawMessage {
	t.Helper()
	var payload []byte
	if err := database.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, WAL, SETTINGS, FORMAT JSON) "+query, args...).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	return json.RawMessage(payload)
}

func explainMemberInsert(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	templateReadingID string,
	subject string,
	request workbench.ReadingRequest,
) json.RawMessage {
	t.Helper()
	period, err := request.Resolve(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	readingID, err := workbench.NewID("xwr_")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO workbench_readings(
 id,owner_subject,contract_version,metric_version,period_start,period_end,
 display_timezone,cutoff,expires_at,money_basis,currency,filters,
 authorization_revision,interpretation_revision,membership_sha256,coverage,created_at
)
SELECT $1,owner_subject,contract_version,metric_version,period_start,period_end,
 display_timezone,cutoff,expires_at,money_basis,currency,filters,
 authorization_revision,interpretation_revision,membership_sha256,coverage,created_at
FROM workbench_readings WHERE id=$2`, readingID, templateReadingID); err != nil {
		t.Fatal(err)
	}
	return explainJSON(t, ctx, tx, workbenchMemberInsertSQL,
		readingID, subject, period.StartInclusive, period.EndExclusive,
		pgTextArray(request.Filters.SourceIDs), pgTextArray(request.Filters.SourceProjects), pgTextArray(request.Filters.CostProjects),
	)
}

func postgresPerformanceIdentity(t *testing.T, ctx context.Context, database *sql.DB) map[string]any {
	t.Helper()
	var version, versionNumber, fsync, synchronousCommit, fullPageWrites, walLevel, sharedBuffers, workMem string
	if err := database.QueryRowContext(ctx, `
SELECT current_setting('server_version'),current_setting('server_version_num'),
 current_setting('fsync'),current_setting('synchronous_commit'),
 current_setting('full_page_writes'),current_setting('wal_level'),
 current_setting('shared_buffers'),current_setting('work_mem')`).Scan(
		&version, &versionNumber, &fsync, &synchronousCommit,
		&fullPageWrites, &walLevel, &sharedBuffers, &workMem,
	); err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"server_version": version, "server_version_num": versionNumber,
		"fsync": fsync, "synchronous_commit": synchronousCommit,
		"full_page_writes": fullPageWrites, "wal_level": walLevel,
		"shared_buffers": sharedBuffers, "work_mem": workMem,
	}
}

func performanceTestDSN(t *testing.T, ctx context.Context, baseDSN string) string {
	t.Helper()
	parsed, err := url.Parse(baseDSN)
	if err != nil || parsed.Scheme == "" {
		t.Fatalf("performance test requires a URL PostgreSQL DSN: %v", err)
	}
	databaseName := "workbench_perf_" + strings.ReplaceAll(performanceTestUUID(t), "-", "")
	admin, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	databaseIdentifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+databaseIdentifier); err != nil {
		t.Fatalf("create performance database: %v", err)
	}
	t.Cleanup(func() {
		// The checkpoint experiment owns the entire disposable cluster and needs
		// this exact stopped database as the common physical template.
		if os.Getenv("XINSIGHT_A0_CHECKPOINT_MODE") == "seed" {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cleanup, cleanupErr := sql.Open("pgx", baseDSN)
		if cleanupErr != nil {
			t.Errorf("open performance database cleanup: %v", cleanupErr)
			return
		}
		defer cleanup.Close()
		if _, cleanupErr := cleanup.ExecContext(cleanupCtx, "DROP DATABASE "+databaseIdentifier+" WITH (FORCE)"); cleanupErr != nil {
			t.Errorf("drop performance database: %v", cleanupErr)
		}
	})
	parsed.Path = "/" + databaseName
	query := parsed.Query()
	query.Set("options", strings.TrimSpace(query.Get("options")+" -cTimeZone=Asia/Shanghai"))
	parsed.RawQuery = query.Encode()
	dsn := parsed.String()
	testDatabase, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer testDatabase.Close()
	if _, err := testDatabase.ExecContext(ctx, `CREATE EXTENSION pgcrypto WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	return dsn
}

func performanceTestUUID(t *testing.T) string {
	t.Helper()
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal(err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

func openPerformanceSQL(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func mapPerformanceFixture(t *testing.T, sourceSystemID string, index int, amount string) unirouter.CompletedExecutionMapResult {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("..", "..", "contracts", "completed-execution", "v1", "fixtures", "valid-settled.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	value["request_id"] = "req-workbench-performance-" + big.NewInt(int64(index+1)).String()
	value["project_id"] = float64(1202 + index)
	value["terminal"].(map[string]any)["available_at"] = time.Date(2026, 7, 25, 4+index, 5, 6, 0, time.UTC).Format(time.RFC3339)
	value["usage"].(map[string]any)["model"] = "performance-model-" + big.NewInt(int64(index+1)).String()
	value["money"].(map[string]any)["charged_cost_micros"] = amount
	value["money"].(map[string]any)["calculated_cost_micros"] = amount
	payload, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	result, err := unirouter.MapCompletedExecutionV1(payload, unirouter.CompletedExecutionMapOptions{
		SourceInstance:    "xi-a0-performance-visibility",
		SourceSystemID:    sourceSystemID,
		AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID,
		ObservedAt:        time.Date(2026, 7, 25, 20, 0, 0, 0, time.UTC),
		IngestedAt:        time.Date(2026, 7, 25, 20, 0, 1, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
