package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/foigent/x-insight/internal/db"
)

func TestConnectorPullMigrationAcceptsNewModeAndDownRefusesLiveRows(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dsn := connectorPullMigrationDatabaseDSN(t, ctx, baseDSN)
	migrationsDir := filepath.Join("..", "..", "migrations")
	applied, err := db.RunMigrations(ctx, dsn, migrationsDir)
	if err != nil {
		t.Fatalf("run current migrations: %v", err)
	}
	expected, err := currentUpMigrationCount(migrationsDir)
	if err != nil {
		t.Fatalf("count current migrations: %v", err)
	}
	if applied != expected {
		t.Fatalf("applied migrations=%d, want current migration count=%d", applied, expected)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	defer database.Close()

	provenanceID := testUUID(t)
	insertConnectorPull := `
INSERT INTO import_provenance (
  id,
  source_system_id,
  adapter_manifest_id,
  integration_mode,
  proof_class,
  assurance_mode,
  source_payload_sha256,
  source_revision,
  imported_revision,
  timeline_complete
) VALUES (
  $1,
  '11111111-1111-1111-1111-111111111111',
  '22222222-2222-4222-8222-222222222224',
  'connector_pull',
  'operator_approved_integration',
  'contracted_pull',
  $2,
  '1',
  '1',
  true
)`
	if _, err := database.ExecContext(
		ctx,
		insertConnectorPull,
		provenanceID,
		"sha256:"+strings.Repeat("a", 64),
	); err != nil {
		t.Fatalf("insert connector_pull provenance after up: %v", err)
	}

	downSQL, err := os.ReadFile(
		filepath.Join(
			migrationsDir,
			"000014_add_connector_pull_provenance.down.sql",
		),
	)
	if err != nil {
		t.Fatalf("read connector_pull down migration: %v", err)
	}
	if _, err := database.ExecContext(ctx, string(downSQL)); err == nil ||
		!strings.Contains(
			err.Error(),
			"cannot remove connector pull provenance while connector_pull rows exist",
		) {
		t.Fatalf("down with connector_pull row error=%v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		"DELETE FROM import_provenance WHERE id = $1",
		provenanceID,
	); err != nil {
		t.Fatalf("delete connector_pull test row: %v", err)
	}
	if _, err := database.ExecContext(ctx, string(downSQL)); err != nil {
		t.Fatalf("down after connector_pull cleanup: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		insertConnectorPull,
		testUUID(t),
		"sha256:"+strings.Repeat("b", 64),
	); err == nil {
		t.Fatal("down migration still accepted connector_pull provenance")
	}
}

func TestAnalysisExtensionMigrationAddsBoundedOfflineModeAndDownRefusesLiveRows(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dsn := connectorPullMigrationDatabaseDSN(t, ctx, baseDSN)
	migrationsDir := filepath.Join("..", "..", "migrations")
	if _, err := db.RunMigrations(ctx, dsn, migrationsDir); err != nil {
		t.Fatalf("run current migrations: %v", err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	defer database.Close()

	provenanceID := testUUID(t)
	insertBoundedOffline := `
INSERT INTO import_provenance (
  id,
  source_system_id,
  adapter_manifest_id,
  integration_mode,
  proof_class,
  assurance_mode,
  source_payload_sha256,
  source_revision,
  imported_revision,
  timeline_complete
) VALUES (
  $1,
  '11111111-1111-1111-1111-111111111111',
  '22222222-2222-4222-8222-222222222224',
  'connector_bounded_offline',
  'operator_approved_integration',
  'bounded_offline_import',
  $2,
  '1',
  '1',
  false
)`
	if _, err := database.ExecContext(
		ctx,
		insertBoundedOffline,
		provenanceID,
		"sha256:"+strings.Repeat("a", 64),
	); err != nil {
		t.Fatalf("insert bounded offline provenance after up: %v", err)
	}

	downSQL, err := os.ReadFile(filepath.Join(
		migrationsDir,
		"000018_create_analysis_extensions.down.sql",
	))
	if err != nil {
		t.Fatalf("read analysis extension down migration: %v", err)
	}
	if _, err := database.ExecContext(ctx, string(downSQL)); err == nil ||
		!strings.Contains(err.Error(), "cannot remove bounded offline provenance") {
		t.Fatalf("down with bounded offline row error=%v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		"DELETE FROM import_provenance WHERE id = $1",
		provenanceID,
	); err != nil {
		t.Fatalf("delete bounded offline test row: %v", err)
	}
	if _, err := database.ExecContext(ctx, string(downSQL)); err != nil {
		t.Fatalf("down after bounded offline cleanup: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		insertBoundedOffline,
		testUUID(t),
		"sha256:"+strings.Repeat("b", 64),
	); err == nil {
		t.Fatal("down migration still accepted bounded offline provenance")
	}
}

func currentUpMigrationCount(migrationsDir string) (int, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			count++
		}
	}
	return count, nil
}

func connectorPullMigrationDatabaseDSN(
	t *testing.T,
	ctx context.Context,
	baseDSN string,
) string {
	t.Helper()
	parsed, err := url.Parse(baseDSN)
	if err != nil || parsed.Scheme == "" {
		t.Fatalf("connector pull migration requires a URL PostgreSQL DSN: %v", err)
	}
	databaseName := "connector_pull_migration_" +
		strings.ReplaceAll(testUUID(t), "-", "")
	admin, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open connector pull migration admin connection: %v", err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(
		ctx,
		fmt.Sprintf("CREATE DATABASE %s", databaseName),
	); err != nil {
		t.Fatalf("create connector pull migration database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cleanupCancel()
		cleanup, cleanupErr := sql.Open("pgx", baseDSN)
		if cleanupErr != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.ExecContext(
			cleanupCtx,
			fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", databaseName),
		)
	})
	parsed.Path = "/" + databaseName
	return parsed.String()
}
