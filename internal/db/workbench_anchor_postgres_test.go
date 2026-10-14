package db

import (
	"os"
	"strings"
	"testing"
)

// Exercise the projection's selected-Evidence rule directly. The second row is
// synthetic; this does not grant another adapter runtime qualification.
func TestWorkbenchFactKeepsSelectedEvidenceAcrossProjectionReplay(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("PG required")
	}
	ctx := t.Context()
	dsn := performanceTestDSN(t, ctx, base)
	if _, err := RunMigrations(ctx, dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	a := openPerformanceSQL(t, dsn)
	defer a.Close()
	store, err := OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mapped := mapPerformanceFixture(t, performanceTestUUID(t), 0, "17")
	if _, err := store.PersistEvents(ctx, mapped.Events); err != nil {
		t.Fatal(err)
	}
	var before, after string
	if err := a.QueryRowContext(ctx, `SELECT to_jsonb(f)::text FROM workbench_economic_facts f`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, conflict := range []bool{false, true} {
		tx, err := a.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		id := performanceTestUUID(t)
		if _, err := tx.ExecContext(ctx, `INSERT INTO evidence_events SELECT (jsonb_populate_record(NULL::evidence_events,to_jsonb(e)||jsonb_build_object('id',$1::uuid,'idempotency_key',$1::text,'event_hash','sha256:'||repeat('a',64)))).* FROM evidence_events e WHERE id=(SELECT evidence_event_id FROM workbench_economic_facts LIMIT 1)`, id); err != nil {
			t.Fatal(err)
		}
		if conflict {
			mapped.Events[0]["usage"].(map[string]any)["cost_micros"] = "18"
		}
		err = projectWorkbenchEconomicFact(ctx, tx, mapped.Events[0], id)
		if conflict {
			if err == nil || !strings.Contains(err.Error(), "identity conflicts") {
				t.Fatalf("changed money accepted: %v", err)
			}
			tx.Rollback()
			var exists bool
			if err := a.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM evidence_events WHERE id=$1::uuid)`, id).Scan(&exists); err != nil || exists {
				t.Fatalf("conflict Evidence escaped rollback: %v", err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
		if err := a.QueryRowContext(ctx, `SELECT to_jsonb(f)::text FROM workbench_economic_facts f`).Scan(&after); err != nil || before != after {
			t.Fatalf("fact or selected Evidence changed: %v", err)
		}
	}
}
