package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/foigent/x-insight/internal/db"
)

func TestMigrationLedgerChecksumsRerunDriftAndDirtyState(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dsn := migrationTestDSN(t, ctx, baseDSN)
	dir := migrationTestDir(t)
	writeMigration(t, dir, "000001_create_alpha", `
CREATE TABLE alpha (id bigint PRIMARY KEY);
CREATE TABLE source_systems (id bigint PRIMARY KEY);
CREATE TABLE evidence_events (id bigint PRIMARY KEY);
CREATE TABLE capture_gaps (id bigint PRIMARY KEY);
CREATE TABLE import_provenance (id bigint PRIMARY KEY);
CREATE TABLE import_provenance_events (id bigint PRIMARY KEY);
CREATE TABLE connector_operational_receipts (id bigint PRIMARY KEY);
`)
	writeMigration(t, dir, "000002_create_beta", "CREATE TABLE beta (id bigint PRIMARY KEY);\n")

	applied, err := db.RunMigrations(ctx, dsn, dir)
	if err != nil || applied != 2 {
		t.Fatalf("first migration applied=%d err=%v", applied, err)
	}
	records, err := db.ReadMigrationLedger(ctx, dsn)
	if err != nil || len(records) != 2 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	expected, err := db.LoadMigrationChecksums(dir)
	if err != nil {
		t.Fatalf("load migration identity: %v", err)
	}
	store, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatalf("open evidence store: %v", err)
	}
	readiness, err := store.Readiness(ctx, expected)
	if closeErr := store.Close(); closeErr != nil {
		t.Fatalf("close evidence store: %v", closeErr)
	}
	if err != nil || readiness.DatabaseSchema != "2" || readiness.MigrationCount != 2 || readiness.PostgresMajor < 10 || !strings.HasPrefix(readiness.MigrationSetSHA256, "sha256:") {
		t.Fatalf("readiness=%+v err=%v", readiness, err)
	}
	for _, record := range records {
		if record.Dirty || !strings.HasPrefix(record.SHA256, "sha256:") {
			t.Fatalf("record=%+v", record)
		}
	}
	if applied, err := db.RunMigrations(ctx, dsn, dir); err != nil || applied != 0 {
		t.Fatalf("rerun applied=%d err=%v", applied, err)
	}

	writeMigration(t, dir, "000001_create_alpha", "CREATE TABLE alpha (id text PRIMARY KEY);\n")
	if _, err := db.RunMigrations(ctx, dsn, dir); !errors.Is(err, db.ErrMigrationChecksumDrift) {
		t.Fatalf("drift error=%v", err)
	}

	dirtyDSN := migrationTestDSN(t, ctx, baseDSN)
	dirtyDir := migrationTestDir(t)
	writeMigration(t, dirtyDir, "000001_invalid", "THIS IS NOT SQL;\n")
	if _, err := db.RunMigrations(ctx, dirtyDSN, dirtyDir); err == nil {
		t.Fatal("expected invalid migration failure")
	}
	dirtyRecords, err := db.ReadMigrationLedger(ctx, dirtyDSN)
	if err != nil || len(dirtyRecords) != 1 || !dirtyRecords[0].Dirty {
		t.Fatalf("dirty records=%+v err=%v", dirtyRecords, err)
	}
	writeMigration(t, dirtyDir, "000001_invalid", "CREATE TABLE repaired (id bigint PRIMARY KEY);\n")
	if _, err := db.RunMigrations(ctx, dirtyDSN, dirtyDir); !errors.Is(err, db.ErrMigrationDirty) {
		t.Fatalf("dirty rerun error=%v", err)
	}
}

func TestReadinessFailsWhenAnyRepresentativeReaderGrantDrifts(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dsn, schema := migrationTestDSNAndSchema(t, ctx, baseDSN)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run production migrations: %v", err)
	}
	expected, err := db.LoadMigrationChecksums(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	role := "xir_" + strings.ReplaceAll(testUUID(t), "-", "")
	password := "readiness-test-password"
	roleIdentifier := pgx.Identifier{role}.Sanitize()
	schemaIdentifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.ExecContext(
		ctx,
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s'", roleIdentifier, password),
	); err != nil {
		t.Fatalf("create reader role: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cleanupErr := sql.Open("pgx", dsn)
		if cleanupErr != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.ExecContext(context.Background(), "DROP OWNED BY "+roleIdentifier)
		_, _ = cleanup.ExecContext(context.Background(), "DROP ROLE "+roleIdentifier)
	})
	var databaseName string
	if err := admin.QueryRowContext(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"GRANT CONNECT ON DATABASE " + pgx.Identifier{databaseName}.Sanitize() + " TO " + roleIdentifier,
		"GRANT USAGE ON SCHEMA " + schemaIdentifier + " TO " + roleIdentifier,
		"GRANT SELECT ON ALL TABLES IN SCHEMA " + schemaIdentifier + " TO " + roleIdentifier,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("grant representative reader access: %v", err)
		}
	}
	readerURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	readerURL.User = url.UserPassword(role, password)
	reader, err := db.OpenEvidenceStore(ctx, readerURL.String())
	if err != nil {
		t.Fatalf("open representative reader: %v", err)
	}
	defer reader.Close()
	if readiness, err := reader.Readiness(ctx, expected); err != nil || !readiness.RepresentativeRead {
		t.Fatalf("complete representative grants readiness=%+v err=%v", readiness, err)
	}
	for _, table := range []string{
		"source_systems",
		"evidence_events",
		"capture_gaps",
		"import_provenance",
		"import_provenance_events",
		"connector_operational_receipts",
	} {
		qualified := schemaIdentifier + "." + pgx.Identifier{table}.Sanitize()
		if _, err := admin.ExecContext(
			ctx,
			"REVOKE SELECT ON TABLE "+qualified+" FROM "+roleIdentifier,
		); err != nil {
			t.Fatalf("revoke %s: %v", table, err)
		}
		if readiness, err := reader.Readiness(ctx, expected); err == nil {
			t.Fatalf("readiness accepted missing %s grant: %+v", table, readiness)
		}
		if _, err := admin.ExecContext(
			ctx,
			"GRANT SELECT ON TABLE "+qualified+" TO "+roleIdentifier,
		); err != nil {
			t.Fatalf("restore %s: %v", table, err)
		}
	}
}

func TestMigrationSetSHA256IsOrderedAndDeterministic(t *testing.T) {
	values := []db.MigrationChecksum{
		{ID: "000001_alpha", SHA256: "sha256:" + strings.Repeat("a", 64)},
		{ID: "000002_beta", SHA256: "sha256:" + strings.Repeat("b", 64)},
	}
	first, err := db.MigrationSetSHA256(values)
	if err != nil || !strings.HasPrefix(first, "sha256:") {
		t.Fatalf("digest=%q err=%v", first, err)
	}
	second, err := db.MigrationSetSHA256(values)
	if err != nil || second != first {
		t.Fatalf("second=%q err=%v", second, err)
	}
	reversed := []db.MigrationChecksum{values[1], values[0]}
	if _, err := db.MigrationSetSHA256(reversed); err == nil {
		t.Fatal("unordered migration set was accepted")
	}
}

func TestLegacyMigrationChecksumAdoptionRequiresExactSignedSet(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dsn := migrationTestDSN(t, ctx, baseDSN)
	dir := migrationTestDir(t)
	writeMigration(t, dir, "000001_create_alpha", "CREATE TABLE alpha (id bigint PRIMARY KEY);\n")
	writeMigration(t, dir, "000002_create_beta", "CREATE TABLE beta (id bigint PRIMARY KEY);\n")

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `
CREATE TABLE schema_migrations (
  version text PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO schema_migrations (version) VALUES
  ('000001_create_alpha'),
  ('000002_create_beta')`); err != nil {
		t.Fatalf("create legacy ledger: %v", err)
	}
	if _, err := db.RunMigrations(ctx, dsn, dir); !errors.Is(err, db.ErrLegacyChecksumMissing) {
		t.Fatalf("legacy run error=%v", err)
	}
	expected, err := db.LoadMigrationChecksums(dir)
	if err != nil {
		t.Fatalf("load expected: %v", err)
	}
	wrong := append([]db.MigrationChecksum(nil), expected...)
	wrong[0].SHA256 = "sha256:" + strings.Repeat("a", 64)
	if _, err := db.AdoptLegacyMigrationChecksums(ctx, dsn, dir, wrong); !errors.Is(err, db.ErrMigrationChecksumDrift) {
		t.Fatalf("wrong adoption error=%v", err)
	}
	updated, err := db.AdoptLegacyMigrationChecksums(ctx, dsn, dir, expected)
	if err != nil || updated != 2 {
		t.Fatalf("adoption updated=%d err=%v", updated, err)
	}
	if applied, err := db.RunMigrations(ctx, dsn, dir); err != nil || applied != 0 {
		t.Fatalf("post-adoption applied=%d err=%v", applied, err)
	}
}

func TestMigrationRejectsContendedExclusiveMaintenanceLock(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dsn := migrationTestDSN(t, ctx, baseDSN)
	dir := migrationTestDir(t)
	writeMigration(t, dir, "000001_create_alpha", "CREATE TABLE alpha (id bigint PRIMARY KEY);\n")

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, db.MaintenanceLockKey); err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, db.MaintenanceLockKey)
	if _, err := db.RunMigrations(ctx, dsn, dir); !errors.Is(err, db.ErrMaintenanceBusy) {
		t.Fatalf("migration lock error=%v", err)
	}
}

func migrationTestDSN(t *testing.T, ctx context.Context, baseDSN string) string {
	t.Helper()
	dsn, _ := migrationTestDSNAndSchema(t, ctx, baseDSN)
	return dsn
}

func migrationTestDSNAndSchema(
	t *testing.T,
	ctx context.Context,
	baseDSN string,
) (string, string) {
	t.Helper()
	parsed, err := url.Parse(baseDSN)
	if err != nil || parsed.Scheme == "" {
		t.Fatalf("migration tests require a URL PostgreSQL DSN: %v", err)
	}
	databaseName := "migration_test_" + strings.ReplaceAll(testUUID(t), "-", "")
	admin, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	defer admin.Close()
	databaseIdentifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+databaseIdentifier); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanup, cleanupErr := sql.Open("pgx", baseDSN)
		if cleanupErr != nil {
			t.Errorf("open test database cleanup connection: %v", cleanupErr)
			return
		}
		defer cleanup.Close()
		if _, cleanupErr := cleanup.ExecContext(
			cleanupCtx,
			"DROP DATABASE "+databaseIdentifier+" WITH (FORCE)",
		); cleanupErr != nil {
			t.Errorf("drop test database %s: %v", databaseName, cleanupErr)
		}
	})
	parsed.Path = "/" + databaseName
	dsn := parsed.String()
	testDatabase, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer testDatabase.Close()
	if _, err := testDatabase.ExecContext(ctx, `CREATE EXTENSION pgcrypto WITH SCHEMA public`); err != nil {
		t.Fatalf("create test pgcrypto extension: %v", err)
	}
	return dsn, "public"
}

func migrationTestDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "xinsight-migration-test-"+strings.ReplaceAll(testUUID(t), "-", ""))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("create migration test directory: %v", err)
	}
	return dir
}

func writeMigration(t *testing.T, dir string, id string, statement string) {
	t.Helper()
	path := filepath.Join(dir, id+".up.sql")
	if err := os.WriteFile(path, []byte(statement), 0o600); err != nil {
		t.Fatalf("write migration %s: %v", id, err)
	}
}
