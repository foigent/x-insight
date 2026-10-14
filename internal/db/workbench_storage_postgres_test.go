package db_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workbench"
)

// Start from the actual immutable schema24 scripts, including their UUID-based
// Evidence selection, rather than constructing a lookalike wide table.
func TestWorkbenchSchema25PreservesAndRejectsWideSnapshots(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("PG required")
	}
	ctx := t.Context()
	dsn := workspaceTestDSN(t, ctx, base)
	dir := t.TempDir()
	entries, err := os.ReadDir("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".up.sql") || entry.Name() >= "000024_" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("../../migrations", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, entry.Name()), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.RunMigrations(ctx, dsn, dir); err != nil {
		t.Fatal(err)
	}
	admin := openTestSQL(t, dsn)
	defer admin.Close()
	source := testUUID(t)
	if _, err := admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name,source_version,status) VALUES($1::uuid,$2,'unirouter','storage migration','unirouter.completed-execution.v1','active')`, source, "source-system:"+source); err != nil {
		t.Fatal(err)
	}
	for i, amount := range []string{"17", "9007199254740993", "0"} {
		mapped := mapWorkbenchFixture(t, source, i, amount)
		insertWorkbenchHistoricalEvidence(t, ctx, admin, mapped.Events[0], source, mapped.RequestID)
	}
	raw, err := os.ReadFile("../../migrations/000024_create_workbench_reading_chain.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "000024_create_workbench_reading_chain.up.sql"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, dsn, dir); err != nil {
		t.Fatal(err)
	}
	subject := "xs_8899aabbccddeeff"
	const readingID = "xwr_abcdefghijklmnopqrstuv"
	// Header authorization uses the same persisted grant serialization as A0.
	if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences) VALUES($1,$2::uuid,true,true,true,true);
 `, subject, source); err != nil {
		t.Fatal(err)
	}
	var grantID string
	if err := admin.QueryRowContext(ctx, `SELECT id::text FROM workbench_access_grants WHERE subject_id=$1`, subject).Scan(&grantID); err != nil {
		t.Fatal(err)
	}
	auth := "sha256:" + fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%t\x00%t\x00%t\x00%t\x00%d", grantID, source, "", "", true, true, true, true, 1))))
	if _, err := admin.ExecContext(ctx, `
 INSERT INTO workbench_readings SELECT $1,$2,'xinsight.workbench.v1','xinsight.workbench.cost.v1','2026-07-25','2026-07-26','UTC','2026-07-26','2026-07-27','gateway_charged','CNY','{}',
 $3,
 'sha256:'||encode(digest('source_project=direct:project_ref;cost_project=unavailable;account=unknown;application=unknown;purpose=unknown','sha256'),'hex'),
 'sha256:'||repeat('0',64),'{"known_items":3,"status":"period_completeness_unknown"}','2026-07-26'
 FROM workbench_access_grants WHERE subject_id=$2;
 `, readingID, subject, auth); err != nil {
		t.Fatal(err)
	}
	// Exactly the schema24 member projection and ordering.
	if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_reading_members SELECT $1,row_number() OVER (ORDER BY f.occurred_at,f.id),f.id,f.evidence_event_id,f.source_system_id,f.source_version,f.source_event_hash,e.event_hash,f.request_id,f.execution_id,f.model_ref,f.source_project_ref,f.cost_project_ref,f.terminal_status,f.occurred_at,f.gateway_charged_micros,f.calculated_reference_micros,f.currency FROM workbench_economic_facts f JOIN evidence_events e ON e.id=f.evidence_event_id`, readingID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE workbench_readings SET membership_sha256=(SELECT 'sha256:'||encode(digest(convert_to(string_agg(economic_fact_id::text||':'||source_event_hash||':'||event_hash,E'\n' ORDER BY ordinal),'UTF8'),'sha256'),'hex') FROM workbench_reading_members)`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/000025_freeze_workbench_facts.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Every removed column must independently prevent the destructive conversion.
	secondSource := testUUID(t)
	if _, err := admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name,source_version,status) VALUES($1::uuid,$2,'unirouter','other source','unirouter.completed-execution.v1','active')`, secondSource, "source-system:"+secondSource); err != nil {
		t.Fatal(err)
	}
	changes := map[string]string{
		"evidence_event_id": "(SELECT evidence_event_id FROM workbench_reading_members WHERE ordinal=2)",
		"source_system_id":  "'" + secondSource + "'::uuid", "source_version": "'other'", "source_event_hash": "'sha256:'||repeat('a',64)",
		"event_hash": "'sha256:'||repeat('b',64)", "request_id": "'other'", "execution_id": "'xe1_'||repeat('c',64)",
		"model_ref": "NULL", "source_project_ref": "NULL", "cost_project_ref": "'new-attribution'",
		"terminal_status": "'cancelled'", "occurred_at": "occurred_at+interval '1 second'",
		"gateway_charged_micros": "18", "calculated_reference_micros": "19", "currency": "'USD'",
	}
	for column, value := range changes {
		t.Run(column, func(t *testing.T) {
			tx, err := admin.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			// For values prohibited by schema24 itself, corruption is still rejected
			// before schema25 can touch data (no disabled constraints or triggers).
			_, err = tx.ExecContext(ctx, "UPDATE workbench_reading_members SET "+column+"="+value+" WHERE ordinal=1")
			if err == nil {
				_, err = tx.ExecContext(ctx, string(migration))
			}
			if err == nil {
				t.Fatal("inconsistent snapshot accepted")
			}
		})
	}
	for _, statement := range []string{
		`UPDATE workbench_readings SET interpretation_revision='sha256:'||repeat('f',64)`,
		`UPDATE workbench_readings SET membership_sha256='sha256:'||repeat('f',64)`,
		`UPDATE workbench_readings SET coverage='{"known_items":2}'`,
		`UPDATE workbench_reading_members SET ordinal=99 WHERE ordinal=1`,
	} {
		tx, _ := admin.BeginTx(ctx, nil)
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, string(migration)); err == nil {
			t.Fatal("corrupt header/order accepted")
		}
		tx.Rollback()
	}
	var before, after string
	snapshot := `SELECT jsonb_agg(to_jsonb(m) ORDER BY ordinal)::text FROM workbench_reading_members m`
	if err := admin.QueryRowContext(ctx, snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	joined := `SELECT jsonb_agg(to_jsonb(f)-'id'-'source_ref'-'amount_scale'-'projected_at'||to_jsonb(m) ORDER BY ordinal)::text FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id`
	if err := admin.QueryRowContext(ctx, joined).Scan(&after); err != nil || before != after {
		t.Fatalf("all-field conversion changed: %v\nbefore=%s\nafter=%s", err, before, after)
	}
	assertWorkbenchFactGuards(t, ctx, admin)
	// Current-role live authorization is still checked before version dispatch;
	// bind the real A0 revision through a fresh reading, then inject an unknown one.
	store, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	old, err := store.GetWorkbenchSummary(ctx, subject, readingID, now)
	if err != nil || old.RequestCount != 3 || old.SelectedAmountMicros != "9007199254741010" {
		t.Fatalf("migrated old reading drift: %+v %v", old, err)
	}
	reading, err := store.CreateWorkbenchReading(ctx, subject, relativeWorkbenchRequest(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE workbench_readings SET interpretation_revision='sha256:'||repeat('e',64) WHERE id=$1`, reading.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetWorkbenchSummary(ctx, subject, reading.ID, now); !errors.Is(err, workbench.ErrUnsupportedVersion) {
		t.Fatalf("unknown interpretation=%v", err)
	}
}

func assertWorkbenchFactGuards(t *testing.T, ctx context.Context, admin *sql.DB) {
	t.Helper()
	for _, mode := range []string{"origin", "replica"} {
		for _, statement := range []string{
			`UPDATE workbench_economic_facts SET model_ref='changed'`,
			`DELETE FROM workbench_economic_facts`,
			`TRUNCATE workbench_economic_facts CASCADE`,
		} {
			tx, err := admin.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, "SET LOCAL session_replication_role="+mode); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, statement); err == nil || !strings.Contains(err.Error(), "facts are immutable") {
				t.Fatalf("guard %s %s: %v", mode, statement, err)
			}
			tx.Rollback()
		}
	}
}
