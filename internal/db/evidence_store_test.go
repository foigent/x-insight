package db_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/evidence"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestEvidenceStorePersistsSnapshotEvents(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	sourceSystemID := testUUID(t)
	adapterManifestID := testUUID(t)
	sourceInstance := "store-test-" + sourceSystemID[:8]
	requestID := "req-gap-" + sourceSystemID[:8]

	snapshot, err := unirouter.LoadSnapshot(filepath.Join("..", "..", "fixtures", "unirouter_snapshot", "missing_trace_gap.json"))
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	snapshot.SourceInstance = sourceInstance
	snapshot.RequestIDs = []string{requestID}
	for i := range snapshot.Scenarios {
		snapshot.Scenarios[i].RequestID = requestID
	}
	for i := range snapshot.CallLogs {
		snapshot.CallLogs[i].SourceRef = ""
		snapshot.CallLogs[i].RequestID = requestID
	}
	for i := range snapshot.TraceEvents {
		snapshot.TraceEvents[i].SourceRef = ""
		snapshot.TraceEvents[i].RequestID = requestID
	}
	for i := range snapshot.CaptureGaps {
		snapshot.CaptureGaps[i].RequestID = requestID
	}

	events, err := unirouter.BuildSnapshotEvents(snapshot, unirouter.SnapshotImportOptions{
		SourceSystemID:    sourceSystemID,
		AdapterManifestID: adapterManifestID,
	})
	if err != nil {
		t.Fatalf("build snapshot events: %v", err)
	}
	for _, event := range events {
		hash, err := evidence.GenerateEventHash(event)
		if err != nil {
			t.Fatalf("hash event: %v", err)
		}
		event["event_hash"] = hash
	}

	store, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	first, err := store.PersistEvents(ctx, events)
	if err != nil {
		t.Fatalf("first persist: %v", err)
	}
	if first.Inserted != 2 || first.CaptureGapsInserted != 1 || first.IdempotencyConflict != 0 {
		t.Fatalf("unexpected first report: %+v", first)
	}

	second, err := store.PersistEvents(ctx, events)
	if err != nil {
		t.Fatalf("second persist: %v", err)
	}
	if second.DuplicateNoop != 2 || second.Inserted != 0 || second.IdempotencyConflict != 0 {
		t.Fatalf("unexpected second report: %+v", second)
	}

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open sql: %v", err)
	}
	defer conn.Close()

	var eventCount int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM evidence_events WHERE source_system_id = $1`, sourceSystemID).Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount != 2 {
		t.Fatalf("event count=%d", eventCount)
	}

	var gapCount int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM capture_gaps WHERE source_system_id = $1`, sourceSystemID).Scan(&gapCount); err != nil {
		t.Fatalf("count gaps: %v", err)
	}
	if gapCount != 1 {
		t.Fatalf("gap count=%d", gapCount)
	}

	eventsByRequest, err := store.ListEvidenceEventsByRequestID(ctx, requestID, 10)
	if err != nil {
		t.Fatalf("list events by request: %v", err)
	}
	if len(eventsByRequest) != 2 {
		t.Fatalf("events by request=%d", len(eventsByRequest))
	}

	limitedEvents, err := store.ListEvidenceEventsByRequestID(ctx, requestID, 1)
	if err != nil {
		t.Fatalf("list limited events by request: %v", err)
	}
	if len(limitedEvents) != 1 {
		t.Fatalf("limited events=%d", len(limitedEvents))
	}

	gapsByRequest, err := store.ListCaptureGapsByRequestID(ctx, requestID, 10)
	if err != nil {
		t.Fatalf("list gaps by request: %v", err)
	}
	if len(gapsByRequest) != 1 {
		t.Fatalf("gaps by request=%d", len(gapsByRequest))
	}
	if len(gapsByRequest[0].CandidateGapTypes) != 3 || gapsByRequest[0].CandidateGapTypes[0] != "sampled_out" {
		t.Fatalf("candidate gap types=%v", gapsByRequest[0].CandidateGapTypes)
	}
}

func TestEvidenceStorePersistsSourceCaptureGap(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	sourceSystemID := testUUID(t)
	adapterManifestID := testUUID(t)
	requestID := "req-auth-denied-" + sourceSystemID[:8]

	snapshot, err := unirouter.LoadSnapshot(filepath.Join("..", "..", "fixtures", "unirouter_snapshot", "model_permission_denied.json"))
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	snapshot.SourceInstance = "store-test-" + sourceSystemID[:8]
	snapshot.RequestIDs = []string{requestID}
	for i := range snapshot.Scenarios {
		snapshot.Scenarios[i].RequestID = requestID
	}
	for i := range snapshot.TraceEvents {
		snapshot.TraceEvents[i].SourceRef = ""
		snapshot.TraceEvents[i].RequestID = requestID
	}
	for i := range snapshot.CaptureGaps {
		snapshot.CaptureGaps[i].RequestID = requestID
	}

	events, err := unirouter.BuildSnapshotEvents(snapshot, unirouter.SnapshotImportOptions{
		SourceSystemID:    sourceSystemID,
		AdapterManifestID: adapterManifestID,
	})
	if err != nil {
		t.Fatalf("build snapshot events: %v", err)
	}
	for _, event := range events {
		hash, err := evidence.GenerateEventHash(event)
		if err != nil {
			t.Fatalf("hash event: %v", err)
		}
		event["event_hash"] = hash
	}

	store, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	report, err := store.PersistEvents(ctx, events)
	if err != nil {
		t.Fatalf("persist events: %v", err)
	}
	if report.Inserted != 2 || report.CaptureGapsInserted != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}

	gaps, err := store.ListCaptureGapsByRequestID(ctx, requestID, 10)
	if err != nil {
		t.Fatalf("list gaps: %v", err)
	}
	if len(gaps) != 2 {
		t.Fatalf("gaps=%d", len(gaps))
	}
	gapTypes := map[string]bool{}
	for _, gap := range gaps {
		gapTypes[gap.GapType] = true
	}
	if !gapTypes["permission_denied"] || !gapTypes["not_available_from_source"] {
		t.Fatalf("gap types=%v", gapTypes)
	}
}

func TestEvidenceStoreWriterLockFailsBeforePersistence(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	lockDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open lock connection: %v", err)
	}
	defer lockDB.Close()
	lockTx, err := lockDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin lock transaction: %v", err)
	}
	defer lockTx.Rollback()
	if _, err := lockTx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, db.MaintenanceLockKey); err != nil {
		t.Fatalf("acquire writer lock: %v", err)
	}

	sourceSystemID := testUUID(t)
	adapterManifestID := testUUID(t)
	snapshot, err := unirouter.LoadSnapshot(filepath.Join("..", "..", "fixtures", "unirouter_snapshot", "minimal.json"))
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	events, err := unirouter.BuildSnapshotEvents(snapshot, unirouter.SnapshotImportOptions{
		SourceSystemID:    sourceSystemID,
		AdapterManifestID: adapterManifestID,
	})
	if err != nil {
		t.Fatalf("build snapshot events: %v", err)
	}
	for _, event := range events {
		hash, hashErr := evidence.GenerateEventHash(event)
		if hashErr != nil {
			t.Fatalf("hash event: %v", hashErr)
		}
		event["event_hash"] = hash
	}
	store, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if _, err := store.PersistEvents(ctx, events); err == nil || !strings.Contains(err.Error(), "writer is busy") {
		t.Fatalf("contending persistence error=%v", err)
	}

	var sourceCount int
	if err := lockDB.QueryRowContext(ctx, `SELECT count(*) FROM source_systems WHERE id = $1`, sourceSystemID).Scan(&sourceCount); err != nil {
		t.Fatalf("count source rows: %v", err)
	}
	if sourceCount != 0 {
		t.Fatalf("writer contention partially persisted %d source rows", sourceCount)
	}
}

func testUUID(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand uuid: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
