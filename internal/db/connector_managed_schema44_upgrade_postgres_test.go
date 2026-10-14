package db_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/db"
)

// Qualify the real connected original v2 and used checkpoint before its first
// descriptor/pair: M44 must preserve those authorities, including dump restore.
func qualifyManagedSchema44Upgrade(t *testing.T, ctx context.Context, baseDSN, dsn string, database *sql.DB, store *db.WorkspaceStore, id, migrations string) {
	t.Helper()
	snapshot := func(pool *sql.DB) map[string]string {
		t.Helper()
		rows, err := pool.QueryContext(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename<>'schema_migrations' ORDER BY tablename`)
		if err != nil {
			t.Fatal(err)
		}
		var tables []string
		for rows.Next() {
			var table string
			if err = rows.Scan(&table); err != nil {
				t.Fatal(err)
			}
			tables = append(tables, table)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		out := map[string]string{}
		for _, table := range tables {
			var raw string
			quoted := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
			projection := "to_jsonb(t)"
			if table == "connector_installations" {
				projection = "(to_jsonb(t)-ARRAY['setup_request_id','setup_actor_subject','setup_request_sha256','setup_consent','setup_reuses_policy','setup_policy_revision','setup_policy_applied_at','setup_error_code'])"
			}
			if err = pool.QueryRowContext(ctx, "SELECT coalesce(jsonb_agg("+projection+" ORDER BY "+projection+"::text),'[]'::jsonb)::text FROM public."+quoted+" t").Scan(&raw); err != nil {
				t.Fatal(err)
			}
			out[table] = fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
		}
		return out
	}
	before := snapshot(database)
	boundary, err := store.InspectActivationBoundary(ctx, id)
	if err != nil || boundary.Installation.Status != "connected" {
		t.Fatal("schema43 original authority", err)
	}
	baseline, _ := json.Marshal(boundary)
	runPG := func(databaseDSN, tool string, args ...string) {
		t.Helper()
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("schema44 restore qualifier requires PostgreSQL16 %s", tool)
		}
		version, err := exec.CommandContext(ctx, path, "--version").Output()
		if err != nil || !bytes.Contains(version, []byte(" 16.")) {
			t.Fatal("PostgreSQL16 client required")
		}
		parsed, err := url.Parse(databaseDSN)
		if err != nil {
			t.Fatal(err)
		}
		env := append([]string{}, os.Environ()...)
		password, _ := parsed.User.Password()
		env = append(env, "PGHOST="+parsed.Hostname(), "PGPORT="+parsed.Port(), "PGUSER="+parsed.User.Username(), "PGPASSWORD="+password, "PGDATABASE="+strings.TrimPrefix(parsed.Path, "/"), "PGSSLMODE=disable")
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Env = env
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("synthetic schema44 %s failed: %v: %s", tool, err, output)
		}
	}
	dump := filepath.Join(t.TempDir(), "original-schema43.dump")
	runPG(dsn, "pg_dump", "--format=custom", "--file", dump)
	if err = os.Chmod(dump, 0600); err != nil {
		t.Fatal(err)
	}
	verify := func(pool *sql.DB, receiver *db.WorkspaceStore) {
		t.Helper()
		after := snapshot(pool)
		for table, want := range before {
			if after[table] != want {
				t.Fatalf("M44 changed original table %s", table)
			}
		}
		got, err := receiver.InspectActivationBoundary(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		actual, _ := json.Marshal(got)
		if !bytes.Equal(actual, baseline) {
			t.Fatal("M44 changed original connected v2 authority/checkpoint")
		}
	}
	schema44 := connectorMigrationPrefixDir(t, migrations, "000044")
	if applied, err := db.RunMigrations(ctx, dsn, schema44); err != nil || applied != 1 {
		t.Fatalf("43 to44 applied=%d err=%v", applied, err)
	}
	verify(database, store)
	// Before any managed authority exists, the exact down/up pair is legal.
	down, err := os.ReadFile(filepath.Join(migrations, "000044_add_managed_connector_adoption.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, string(down)); err != nil {
		tx.Rollback()
		t.Fatal("unused M44 down", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version='000044_add_managed_connector_adoption'`); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	verify(database, store)
	if applied, err := db.RunMigrations(ctx, dsn, schema44); err != nil || applied != 1 {
		t.Fatalf("44 reapply applied=%d err=%v", applied, err)
	}
	verify(database, store)
	restoredDSN := connectorPullMigrationDatabaseDSN(t, ctx, baseDSN)
	parsed, _ := url.Parse(restoredDSN)
	runPG(restoredDSN, "pg_restore", "--exit-on-error", "--single-transaction", "--no-owner", "--dbname", strings.TrimPrefix(parsed.Path, "/"), dump)
	restored, err := sql.Open("pgx", restoredDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if applied, err := db.RunMigrations(ctx, restoredDSN, schema44); err != nil || applied != 1 {
		t.Fatalf("restored43 forward44 applied=%d err=%v", applied, err)
	}
	receiver, err := db.OpenWorkspaceStore(ctx, restoredDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	verify(restored, receiver)
	t.Log("actual connected schema43 v2/checkpoint survives44, unused44 down/up, PG16 dump restore and forward44 with every original public table unchanged")
}
