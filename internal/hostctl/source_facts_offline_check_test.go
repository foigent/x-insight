package hostctl

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/jackc/pgx/v5"
)

// Fingerprint every public table and sequence, including existing readings,
// management history and live cursors. Counts alone would miss in-place changes.
func managedOfflineDatabaseState(t *testing.T, admin *sql.DB) map[string]string {
	t.Helper()
	rows, err := admin.Query(`SELECT c.relname,c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p','S') ORDER BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	var names [][2]string
	for rows.Next() {
		var name, kind string
		if err := rows.Scan(&name, &kind); err != nil {
			t.Fatal(err)
		}
		names = append(names, [2]string{name, kind})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	state := map[string]string{}
	for _, item := range names {
		name := pgx.Identifier{"public", item[0]}.Sanitize()
		query := `SELECT md5(COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text) FROM ` + name + ` t`
		if item[1] == "S" {
			query = `SELECT md5(last_value::text || ':' || is_called::text) FROM ` + name
		}
		var digest string
		if err := admin.QueryRow(query).Scan(&digest); err != nil {
			t.Fatal(item[0], err)
		}
		state[item[0]] = digest
	}
	return state
}

func waitOfflineExpiry(t *testing.T, ctx context.Context, admin *sql.DB, expires string) {
	t.Helper()
	if _, err := admin.ExecContext(ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM $1::timestamptz-clock_timestamp()))+0.05)`, expires); err != nil {
		t.Fatal(err)
	}
}

func TestManagedOfflineAuthorizationTimeAfterRowLocks(t *testing.T) {
	for _, stage := range []string{"reservation", "completion", "replay", "admitted_completion"} {
		t.Run(stage, func(t *testing.T) {
			ctx, c := connectorDigestDatabase(t)
			m := PostgresManager{Runner: ExecRunner{}}
			if err := m.EnsureRoles(ctx, c); err != nil {
				t.Fatal(err)
			}
			if _, err := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil {
				t.Fatal(err)
			}
			if err := m.ApplyGrants(ctx, c); err != nil {
				t.Fatal(err)
			}
			admin := managedSQL(t, c.AdminDSN)
			owner, importer := managedStore(t, c.AdminDSN), managedStore(t, c.ImporterDSN)
			var source string
			if err := admin.QueryRow(`SELECT gen_random_uuid()::text`).Scan(&source); err != nil {
				t.Fatal(err)
			}
			a, raw, page := managedOfflineFixture(t, source)
			lifetime := 3 * time.Second
			if stage == "replay" {
				// Replay first imports a full batch and fingerprints the
				// database, so leave time for those prerequisites before the
				// controlled row-lock wait begins.
				lifetime = 10 * time.Second
			}
			a.ExpiresAt = time.Now().Add(lifetime).UTC().Format(sourcefacts.TimeFormat)
			g, err := owner.RegisterSourceOfflineAuthorization(ctx, a)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := owner.ControlSourceOfflineAuthorization(ctx, g.ID, "active", g.Revision, a.Digest()); err != nil {
				t.Fatal(err)
			}
			if stage == "replay" {
				if _, err := importer.ImportSourceFactsOffline(ctx, a, raw, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			before := managedOfflineDatabaseState(t, admin)
			row, err := admin.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer row.Rollback()
			lockRow := func() error {
				var id string
				return row.QueryRowContext(ctx, `SELECT id FROM source_fact_offline_authorizations WHERE id=$1 FOR UPDATE`, a.AuthorizationID).Scan(&id)
			}
			done := make(chan error, 1)
			start := func() {
				go func() { _, err := importer.ImportSourceFactsOffline(ctx, a, raw, time.Now()); done <- err }()
			}
			if stage == "reservation" {
				if err := lockRow(); err != nil {
					t.Fatal(err)
				}
				start()
				awaitManagedWaiter(t, ctx, admin, "FOR SHARE")
			} else {
				// Queue a row lock before the reservation commits, ensuring the
				// completion transaction really waits for authorization.
				table := "connector_import_operation_bindings"
				if stage == "admitted_completion" {
					table = "connector_operational_receipts"
				}
				managedExec(t, admin, `CREATE FUNCTION synthetic_offline_time_gate() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(91927364); RETURN NEW; END $$`)
				managedExec(t, admin, `CREATE TRIGGER synthetic_offline_time_gate BEFORE INSERT ON `+table+` FOR EACH ROW EXECUTE FUNCTION synthetic_offline_time_gate()`)
				gate, err := admin.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer gate.Rollback()
				if _, err := gate.ExecContext(ctx, `SELECT pg_advisory_xact_lock(91927364)`); err != nil {
					t.Fatal(err)
				}
				start()
				awaitManagedWaiter(t, ctx, admin, "INSERT INTO "+table)
				var sourceGate *sql.Tx
				var sourceHeld chan error
				if stage == "replay" {
					// Queue behind the reservation's source lock before releasing
					// its INSERT gate. This keeps replay's receipt transaction
					// from overtaking the waiting authorization-row FOR UPDATE.
					sourceGate, err = admin.BeginTx(ctx, nil)
					if err != nil {
						t.Fatal(err)
					}
					defer sourceGate.Rollback()
					sourceHeld = make(chan error, 1)
					go func() {
						_, e := sourceGate.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "source-facts:"+a.SourceSystemID)
						sourceHeld <- e
					}()
					awaitManagedWaiter(t, ctx, admin, "hashtextextended")
				}
				if stage == "admitted_completion" {
					waitOfflineExpiry(t, ctx, admin, a.ExpiresAt)
					if err := gate.Commit(); err != nil {
						t.Fatal(err)
					}
				} else {
					locked := make(chan error, 1)
					go func() { locked <- lockRow() }()
					awaitManagedWaiter(t, ctx, admin, "FOR UPDATE")
					if err := gate.Commit(); err != nil {
						t.Fatal(err)
					}
					if stage == "replay" {
						if err := <-sourceHeld; err != nil {
							t.Fatal(err)
						}
					}
					if err := <-locked; err != nil {
						t.Fatal(err)
					}
					if stage == "replay" {
						if err := sourceGate.Commit(); err != nil {
							t.Fatal(err)
						}
					}
					awaitManagedWaiter(t, ctx, admin, "FOR SHARE")
				}
			}
			if stage != "admitted_completion" {
				waitOfflineExpiry(t, ctx, admin, a.ExpiresAt)
				if err := row.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			err = <-done
			wantBindings, wantFacts, wantReceipts := int64(1), int64(0), int64(0)
			if stage == "admitted_completion" {
				if err != nil {
					t.Fatal("already admitted completion must finish", err)
				}
			} else if !errors.Is(err, sourcefacts.ErrAuthorization) {
				t.Fatal("authorization expired while waiting for row lock", err)
			}
			if stage == "reservation" {
				wantBindings = 0
			}
			if stage == "replay" || stage == "admitted_completion" {
				wantFacts, wantReceipts = int64(len(page.Events)), 1
			}
			if managedCount(t, admin, "connector_import_operation_bindings") != wantBindings || managedCount(t, admin, "source_fact_documents") != wantFacts || managedCount(t, admin, "connector_operational_receipts") != wantReceipts {
				t.Fatal("unexpected reservation/fact/receipt outcome")
			}
			if stage != "admitted_completion" {
				after := managedOfflineDatabaseState(t, admin)
				if stage == "completion" {
					// This reservation was valid when committed and must survive.
					after["connector_import_operation_bindings"] = before["connector_import_operation_bindings"]
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatal("refused authorization changed state beyond a valid reservation")
				}
			}
			t.Logf("row-lock expiry stage=%s bindings=%d facts=%d receipts=%d; authorization semantics preserved", stage, wantBindings, wantFacts, wantReceipts)
		})
	}
}
