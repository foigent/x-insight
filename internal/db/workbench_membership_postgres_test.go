package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkbenchMembershipReplaysAcrossUpgradeAndLateCommit(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("PostgreSQL is required")
	}
	ctx := t.Context()
	dsn := performanceTestDSN(t, ctx, base)
	const migrations = "../../migrations"
	const migration = "000049_replay_workbench_members"
	paths, err := filepath.Glob(filepath.Join(migrations, "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	predecessor := t.TempDir()
	for _, path := range paths {
		name := filepath.Base(path)
		if name >= migration {
			break
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(predecessor, name), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if applied, err := RunMigrations(ctx, dsn, predecessor); err != nil || applied != 48 {
		t.Fatalf("predecessor applied=%d err=%v", applied, err)
	}
	admin := openPerformanceSQL(t, dsn)
	defer admin.Close()
	evidence, err := OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer evidence.Close()
	workspace, err := OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	source := performanceTestUUID(t)
	initial := mapPerformanceFixture(t, source, 0, "17")
	if _, err := evidence.PersistEvents(ctx, initial.Events); err != nil {
		t.Fatal(err)
	}
	const subject = "xs_0000000000000010"
	if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details) VALUES($1,$2,true,true)`, subject, source); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 21, 0, 0, 0, time.UTC)
	request := explicitPerformanceRequest(now.Add(-24*time.Hour), now, []string{source}, nil)
	legacy, err := workspace.CreateWorkbenchReading(ctx, subject, request, now)
	if err != nil || legacy.Coverage.KnownItems != 1 {
		t.Fatalf("legacy reading: %+v err=%v", legacy, err)
	}
	if applied, err := RunMigrations(ctx, dsn, migrations); err != nil || applied != 1 {
		t.Fatalf("migration applied=%d err=%v", applied, err)
	}
	down, err := os.ReadFile(filepath.Join(migrations, migration+".down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, string(down)); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	block, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer block.Rollback()
	if _, err := block.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, evidencePublicationLock); err != nil {
		t.Fatal(err)
	}
	late := mapPerformanceFixture(t, source, 1, "23")
	done := make(chan error, 1)
	go func() {
		_, err := evidence.PersistEvents(ctx, late.Events)
		done <- err
	}()
	for deadline := time.Now().Add(5 * time.Second); ; {
		var waiting bool
		if err := admin.QueryRowContext(ctx, `SELECT EXISTS (
 SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
 AND wait_event='advisory' AND query LIKE 'SELECT pg_advisory_xact_lock(%'
 )`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("late import exited before its publication fence: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("late import did not reach its publication fence")
		}
		time.Sleep(10 * time.Millisecond)
	}
	pinned, err := workspace.CreateWorkbenchReading(ctx, subject, request, now)
	if err != nil || pinned.Coverage.KnownItems != 1 || pinned.MembershipRef != legacy.MembershipRef {
		t.Fatalf("reading during unpublished write: %+v err=%v", pinned, err)
	}
	if err := block.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	newer, err := workspace.CreateWorkbenchReading(ctx, subject, request, now)
	if err != nil || newer.Coverage.KnownItems != 2 || newer.MembershipRef == pinned.MembershipRef {
		t.Fatalf("reading after commit: %+v err=%v", newer, err)
	}
	for _, id := range []string{legacy.ID, pinned.ID} {
		summary, err := workspace.GetWorkbenchSummary(ctx, subject, id, now)
		if err != nil || summary.RequestCount != 1 || summary.GatewayChargedMicros != "17" {
			t.Fatalf("replayed summary id=%s: %+v err=%v", id, summary, err)
		}
	}
	var physicalRows int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM workbench_legacy_reading_members`).Scan(&physicalRows); err != nil || physicalRows != 1 {
		t.Fatalf("materialized rows=%d err=%v", physicalRows, err)
	}
	if err := ValidateWorkbenchRestore(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE workbench_readings SET gateway_total_micros='18' WHERE id=$1`, pinned.ID); err == nil {
		t.Fatal("updating frozen totals succeeded")
	}
	damaged, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer damaged.Rollback()
	if _, err := damaged.ExecContext(ctx, `
INSERT INTO workbench_readings
SELECT (jsonb_populate_record(NULL::workbench_readings, to_jsonb(r) ||
  jsonb_build_object('id','xwr_0000000000000000000001','gateway_total_micros','18'))).*
FROM workbench_readings r WHERE id=$1`, pinned.ID); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWorkbenchRestore(ctx, damaged); err == nil || !strings.Contains(err.Error(), "frozen totals") {
		t.Fatalf("restore accepted the wrong frozen total: %v", err)
	}
	if err := damaged.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, string(down)); err == nil || !strings.Contains(err.Error(), "versioned Workbench readings") {
		t.Fatalf("destructive downgrade should fail closed: %v", err)
	}
}

func TestWorkbenchReplayPublishesNewProjectionOfDuplicateEvidence(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("PostgreSQL is required")
	}
	ctx := t.Context()
	dsn := performanceTestDSN(t, ctx, base)
	if _, err := RunMigrations(ctx, dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	admin := openPerformanceSQL(t, dsn)
	defer admin.Close()
	store, err := OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	event := mapPerformanceFixture(t, performanceTestUUID(t), 0, "17").Events[0]
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	id, err := beginEvidencePublication(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureDependencies(ctx, tx, event); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := persistEvent(ctx, tx, event, "", "", false)
	if err != nil || got.Outcome != "inserted" {
		t.Fatalf("insert evidence without projection: %+v err=%v", got, err)
	}
	if err := publishEvidenceTransaction(ctx, tx, id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	report, err := store.PersistEvents(ctx, []map[string]any{event})
	if err != nil || report.Inserted != 0 || report.DuplicateNoop != 1 {
		t.Fatalf("late projection report: %+v err=%v", report, err)
	}
	var factRevision, eventRevision int
	if err := admin.QueryRowContext(ctx, `
SELECT pf.revision,pe.revision FROM workbench_economic_facts f
JOIN evidence_events e ON e.id=f.evidence_event_id
JOIN evidence_publications pf ON pf.snapshot_id=f.publication_id
JOIN evidence_publications pe ON pe.snapshot_id=e.publication_id`).Scan(&factRevision, &eventRevision); err != nil || factRevision != eventRevision+1 {
		t.Fatalf("fact revision=%d event revision=%d err=%v", factRevision, eventRevision, err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE evidence_publications SET revision=20 WHERE revision=1`); err == nil {
		t.Fatal("publication mutation was accepted")
	}
}
