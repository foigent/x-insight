package hostctl

import (
	"database/sql"
	"encoding/json"
	"github.com/foigent/x-insight/internal/db"
	"os"
	"path/filepath"
	"testing"
)

// This exporter is test-only and guarded by the package's recorded PG scope.
// It never changes an already migrated predecessor to bypass checksum drift.
func TestAdminFlowSchemaContractPostgres(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	out := os.Getenv("XINSIGHT_ADMIN_FLOW_SCHEMA_OUTPUT")
	if dsn == "" || out == "" {
		t.Skip("explicit synthetic schema export not selected")
	}
	if !filepath.IsAbs(out) || filepath.Clean(out) != out {
		t.Fatal("absolute schema output path required")
	}
	ctx, credentials := connectorDigestDatabase(t)
	dsn = credentials.AdminDSN
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	contract, err := GenerateCleanBaselineSchemaContract(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
}
