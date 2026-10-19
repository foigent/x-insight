package connectorsync_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/testpostgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestMain(m *testing.M) {
	code := testpostgres.Main(m.Run)
	if database != nil {
		database.Close()
	}
	if control != nil {
		if testDatabase != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = control.ExecContext(ctx, "DROP DATABASE "+pgx.Identifier{testDatabase}.Sanitize())
			cancel()
		}
		control.Close()
	}
	os.Exit(code)
}

var setupOnce sync.Once
var setupErr error
var database *sql.DB
var control *sql.DB
var testDatabase string

const oldID = "xci_0000000000000000000000"

func fixture(t *testing.T) *connectorsync.Store {
	t.Helper()
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires explicitly scoped synthetic PostgreSQL")
	}
	setupOnce.Do(func() {
		control, setupErr = sql.Open("pgx", dsn)
		if setupErr != nil {
			return
		}
		testDatabase = fmt.Sprintf("xi_sync_%d_%d", os.Getpid(), time.Now().UnixNano())
		if _, setupErr = control.Exec("CREATE DATABASE " + pgx.Identifier{testDatabase}.Sanitize()); setupErr != nil {
			return
		}
		config, e := pgx.ParseConfig(dsn)
		if e != nil {
			setupErr = e
			return
		}
		config.Database = testDatabase
		// ConnString preserves the originally parsed string, not edits to
		// config.Database. Register the actual config for both migration calls.
		dsn = stdlib.RegisterConnConfig(config)
		database, setupErr = sql.Open("pgx", dsn)
		if setupErr != nil {
			return
		}
		_, setupErr = database.Exec(`DO $$ DECLARE r text; BEGIN FOREACH r IN ARRAY ARRAY['xinsight_admin','xinsight_reader','xinsight_importer','xinsight_workspace'] LOOP IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=r) THEN EXECUTE format('CREATE ROLE %I LOGIN',r); END IF; END LOOP; END $$`)
		if setupErr != nil {
			return
		}
		dir, e := os.MkdirTemp("", "xi-sync-schema45-")
		if e != nil {
			setupErr = e
			return
		}
		// Go owns this new test-only directory; it contains only copied migrations.
		entries, e := os.ReadDir("../../migrations")
		if e != nil {
			setupErr = e
			return
		}
		for _, f := range entries {
			if !f.IsDir() && f.Name() < "000046" {
				raw, e := os.ReadFile(filepath.Join("../../migrations", f.Name()))
				if e != nil {
					setupErr = e
					return
				}
				if e = os.WriteFile(filepath.Join(dir, f.Name()), raw, 0600); e != nil {
					setupErr = e
					return
				}
			}
		}
		_, setupErr = db.RunMigrations(context.Background(), dsn, dir)
		if setupErr != nil {
			return
		}
		setupErr = insertInstallation(database, oldID)
		if setupErr != nil {
			return
		}
		_, setupErr = db.RunMigrations(context.Background(), dsn, "../../migrations")
		if setupErr != nil {
			return
		}
		_, setupErr = database.Exec(`CREATE TABLE sync_test_effects(id text PRIMARY KEY); GRANT INSERT,SELECT ON sync_test_effects TO xinsight_importer`)
	})
	if setupErr != nil {
		t.Fatal(setupErr)
	}
	return connectorsync.New(database)
}
func insertInstallation(d *sql.DB, id string) error {
	_, e := d.Exec(`INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,status,desired_state,observed_state,created_at,updated_at) VALUES($1,'xid_0000000000000000000000','unirouter',ARRAY['model.request.completed'],'pending','active','awaiting_consent',clock_timestamp(),clock_timestamp())`, id)
	return e
}
func install(t *testing.T, n int) (*connectorsync.Store, string) {
	t.Helper()
	s := fixture(t)
	id := fmt.Sprintf("xci_%022d", n)
	if e := insertInstallation(database, id); e != nil {
		t.Fatal(e)
	}
	if _, e := database.Exec(`INSERT INTO connector_installation_source_bindings(installation_id,source_binding_sha256)VALUES($1,'sha256:'||repeat('a',64)) ON CONFLICT DO NOTHING`, id); e != nil {
		t.Fatal(e)
	}
	return s, id
}
func request(id string, rev int64, n int, action string) connectorsync.Request {
	return connectorsync.Request{SchemaVersion: connectorsync.LocalSchema, Action: action, InstallationID: id, Actor: "xs_0000000000000000", ExpectedRevision: rev, RequestID: fmt.Sprintf("00000000-0000-4000-8000-%012d", n)}
}

func TestMigrationPreservesExistingAndDefaultsNewManual(t *testing.T) {
	s := fixture(t)
	old, e := s.Status(t.Context(), oldID)
	if e != nil || !old.Auto || old.Cadence != nil {
		t.Fatalf("old behavior drift: %+v %v", old, e)
	}
	_, id := install(t, 1)
	fresh, e := s.Status(t.Context(), id)
	if e != nil || fresh.Auto || fresh.Revision != 1 {
		t.Fatalf("new default: %+v %v", fresh, e)
	}
	r := request(id, 1, 1, "set")
	v := 4
	r.Cadence = &v
	if _, e = s.Change(t.Context(), r); !errors.Is(e, connectorsync.ErrInvalid) {
		t.Fatal("accepted invalid cadence", e)
	}
	r.Cadence = nil
	r.Auto = true
	fresh, e = s.Change(t.Context(), r)
	if e != nil || fresh.Cadence == nil || *fresh.Cadence != 60 {
		t.Fatalf("new low traffic default: %+v %v", fresh, e)
	}
}

func TestManualRunBoundsAndIdempotentReceipt(t *testing.T) {
	s, id := install(t, 2)
	if _, _, e := s.Begin(t.Context(), id, "projects", time.Minute); !errors.Is(e, connectorsync.ErrPaused) {
		t.Fatal(e)
	}
	r := request(id, 1, 2, "once")
	one, e := s.Change(t.Context(), r)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Change(t.Context(), r)
	if e != nil || again.Revision != one.Revision {
		t.Fatal("idempotency changed run", e)
	}
	if _, e = s.Change(t.Context(), request(id, one.Revision, 3, "once")); !errors.Is(e, connectorsync.ErrConflict) {
		t.Fatal("concurrent once accepted", e)
	}
	ctx, finish, e := s.Begin(t.Context(), id, "projects", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 64; i++ {
		if e = connectorsync.BeforeHTTP(ctx, true); e != nil {
			t.Fatalf("attempt %d: %v", i, e)
		}
	}
	if e = connectorsync.BeforeHTTP(ctx, true); !errors.Is(e, connectorsync.ErrPaused) {
		t.Fatal("attempt 65", e)
	}
	finish(nil)
	v, e := s.Status(t.Context(), id)
	if e != nil || v.Run.Attempts != 64 || v.Run.State != "partial" {
		t.Fatalf("budget: %+v %v", v.Run, e)
	}
	if _, _, e = s.Begin(t.Context(), id, "projects", time.Minute); !errors.Is(e, connectorsync.ErrPaused) {
		t.Fatal("exhausted run resumed", e)
	}
}

func TestOnceEmptyUnitsStopAndApplicableWorkers(t *testing.T) {
	s, id := install(t, 3)
	if _, e := s.Change(t.Context(), request(id, 1, 4, "once")); e != nil {
		t.Fatal(e)
	}
	for _, w := range []string{"activation", "projects"} {
		ctx, finish, e := s.Begin(t.Context(), id, w, time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		if e = connectorsync.BeforeHTTP(ctx, true); e != nil {
			t.Fatal(e)
		}
		connectorsync.Progress(ctx, false, 0)
		finish(nil)
	}
	v, e := s.Status(t.Context(), id)
	if e != nil || v.Run.State != "empty" || len(v.Required) != 2 {
		t.Fatalf("applicability/empty: %+v %v", v, e)
	}
	before := v.Run.Attempts
	if _, _, e = s.Begin(t.Context(), id, "projects", time.Minute); !errors.Is(e, connectorsync.ErrPaused) {
		t.Fatal("empty category polled again", e)
	}
	v, _ = s.Status(t.Context(), id)
	if v.Run.Attempts != before {
		t.Fatal("empty repoll spent requests")
	}
	off := request(id, v.Revision, 5, "set")
	if _, e = s.Change(t.Context(), off); e != nil {
		t.Fatal(e)
	}
	for _, w := range []string{"activation", "projects"} {
		_, _, _ = s.Begin(t.Context(), id, w, time.Minute)
	}
	v, e = s.Status(t.Context(), id)
	if e != nil || v.Effective != "quiet" {
		t.Fatalf("no Feed exists, should not require it: %+v %v", v, e)
	}
}

func TestPauseAndBusinessCommitSerializeInBothOrders(t *testing.T) {
	s, id := install(t, 4)
	v, e := s.Change(t.Context(), request(id, 1, 6, "once"))
	if e != nil {
		t.Fatal(e)
	}
	ctx, finish, e := s.Begin(t.Context(), id, "source", 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Change(t.Context(), request(id, v.Revision, 7, "set")); e != nil {
		t.Fatal(e)
	}
	tx, e := database.BeginTx(t.Context(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = connectorsync.CommitFence(ctx, tx); !errors.Is(e, connectorsync.ErrChanged) {
		t.Fatal("stale commit admitted", e)
	}
	tx.Rollback()
	finish(connectorsync.ErrChanged)
	v, _ = s.Status(t.Context(), id)
	v, e = s.Change(t.Context(), request(id, v.Revision, 8, "once"))
	if e != nil {
		t.Fatal(e)
	}
	ctx, finish, e = s.Begin(t.Context(), id, "source", 0)
	if e != nil {
		t.Fatal(e)
	}
	tx, e = database.BeginTx(t.Context(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = connectorsync.CommitFence(ctx, tx); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO sync_test_effects VALUES('before_off')`); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := s.Change(context.Background(), request(id, v.Revision, 9, "set")); done <- e }()
	select {
	case e := <-done:
		t.Fatal("pause bypassed business transaction", e)
	case <-time.After(40 * time.Millisecond):
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	finish(connectorsync.ErrChanged)
	var count int
	if e = database.QueryRow(`SELECT count(*) FROM sync_test_effects WHERE id='before_off'`).Scan(&count); e != nil || count != 1 {
		t.Fatal("already committed evidence lost", e)
	}
}

func TestImporterCannotCreateAuthorityOrChangePreference(t *testing.T) {
	fixture(t)
	for _, q := range []string{`UPDATE connector_sync_controls SET auto_sync=false`, `UPDATE connector_sync_controls SET revision=revision+1`, `INSERT INTO connector_sync_runs(installation_id,request_id,revision,started_at,deadline,state,updated_by)VALUES('` + oldID + `','forged',1,statement_timestamp(),statement_timestamp()+interval '60 seconds','running','forged')`} {
		tx, e := database.BeginTx(t.Context(), nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.ExecContext(t.Context(), `SET LOCAL ROLE xinsight_importer`); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
		if _, e = tx.ExecContext(t.Context(), q); e == nil {
			tx.Rollback()
			t.Fatal("importer gained authority", q)
		}
		tx.Rollback()
	}
	tx, e := database.BeginTx(t.Context(), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(t.Context(), `SET LOCAL ROLE xinsight_importer`); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.ExecContext(t.Context(), `SELECT * FROM public.xinsight_sync_units('`+oldID+`')`); e != nil {
		t.Fatal("narrow ownership projection inaccessible", e)
	}
}

func TestOnceStatusAndPostResponseLocalPublicationFence(t *testing.T) {
	s, id := install(t, 5)
	v, e := s.Change(t.Context(), request(id, 1, 10, "once"))
	if e != nil {
		t.Fatal(e)
	}
	ctx, finish, e := s.Begin(t.Context(), id, "projects", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	status, e := s.Status(t.Context(), id)
	if e != nil || status.Effective != "manual_running" {
		t.Fatal("active once is not draining", status.Effective, e)
	}
	// Models PublishManagedProjects after its real enrollment response has been
	// received: the native file callback must not run if pause wins this boundary.
	if _, e = s.Change(t.Context(), request(id, v.Revision, 11, "set")); e != nil {
		t.Fatal(e)
	}
	status, e = s.Status(t.Context(), id)
	if e != nil || status.Effective != "stopping" {
		t.Fatal("pause must expose inflight drain", status.Effective, e)
	}
	called := false
	if e = connectorsync.LocalCommit(ctx, func() error { called = true; return nil }); !errors.Is(e, connectorsync.ErrChanged) || called {
		t.Fatal("paused enrollment result reached publication", called, e)
	}
	finish(connectorsync.ErrChanged)
}

func TestUnknownBindingAndRevokedInstallationFailClosed(t *testing.T) {
	s := fixture(t)
	id := "xci_0000000000000000000006"
	if e := insertInstallation(database, id); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Change(t.Context(), request(id, 1, 12, "once")); !errors.Is(e, connectorsync.ErrBinding) {
		t.Fatal("unbound once accepted", e)
	}
	if _, _, e := s.Begin(t.Context(), id, "projects", time.Minute); !errors.Is(e, connectorsync.ErrBinding) {
		t.Fatal("unbound network permit accepted", e)
	}
	_, id = install(t, 7)
	if _, e := s.Change(t.Context(), request(id, 1, 13, "once")); e != nil {
		t.Fatal(e)
	}
	ctx, finish, e := s.Begin(t.Context(), id, "source", 0)
	if e != nil {
		t.Fatal(e)
	}
	defer finish(connectorsync.ErrBinding)
	if _, e = database.Exec(`UPDATE connector_installations SET desired_state='paused' WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	if e = connectorsync.BeforeHTTP(ctx, true); !errors.Is(e, connectorsync.ErrBinding) {
		t.Fatal("revoked installation emitted next request", e)
	}
	tx, e := database.BeginTx(t.Context(), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if e = connectorsync.CommitFence(ctx, tx); !errors.Is(e, connectorsync.ErrBinding) {
		t.Fatal("revoked installation committed received data", e)
	}
}
