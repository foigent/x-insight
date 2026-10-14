// Package testpostgres protects opt-in local synthetic PostgreSQL test runs.
// It is imported only by tests, never by a product executable.
package testpostgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

const ScopeEnv = "XINSIGHT_LOCAL_TEST_DATABASE_SCOPE"

// Scope is a separately recorded target, not derived from the candidate DSN.
// Database OID and cluster identity also reject a replaced database or server.
type Scope struct {
	Host        string `json:"host"`
	Port        uint16 `json:"port"`
	Database    string `json:"database"`
	User        string `json:"user"`
	DatabaseOID string `json:"database_oid"`
	SystemID    string `json:"system_id"`
}

func validate(raw string, scope Scope) (*pgx.ConnConfig, error) {
	if scope.Host != "127.0.0.1" || scope.Port == 0 || scope.User == "" || scope.DatabaseOID == "" || scope.SystemID == "" || scope.Database == "" || scope.Database == "postgres" || scope.Database == "template0" || scope.Database == "template1" {
		return nil, errors.New("invalid local test database scope")
	}
	c, err := pgx.ParseConfig(raw)
	if err != nil {
		return nil, errors.New("invalid local test database DSN")
	}
	if c.Host != scope.Host || c.Port != scope.Port || c.Database != scope.Database || c.User != scope.User || len(c.Fallbacks) != 0 || len(c.RuntimeParams) != 0 {
		return nil, errors.New("local test database target differs from recorded scope")
	}
	return c, nil
}

func probe(c *pgx.ConnConfig, scope Scope) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, c)
	if err != nil {
		return errors.New("local test target read-only identity connection failed")
	}
	defer conn.Close(ctx)
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return errors.New("local test target read-only transaction failed")
	}
	defer tx.Rollback(ctx)
	var database, user, oid, systemID string
	err = tx.QueryRow(ctx, `SELECT current_database(), current_user,
		(SELECT oid::text FROM pg_database WHERE datname=current_database()),
		system_identifier::text FROM pg_control_system()`).Scan(&database, &user, &oid, &systemID)
	if err != nil || database != scope.Database || user != scope.User || oid != scope.DatabaseOID || systemID != scope.SystemID {
		return errors.New("local test target live identity differs from recorded scope")
	}
	return nil
}

func check(getenv func(string) string, readFile func(string) ([]byte, error), verify func(*pgx.ConnConfig, Scope) error) error {
	// Required database qualification must fail before m.Run; individual test
	// skips remain available only to an explicitly optional/offline run.
	if getenv("XINSIGHT_REQUIRE_POSTGRES_TESTS") == "1" && getenv("XINSIGHT_POSTGRES_DSN") == "" {
		return errors.New("mandatory PostgreSQL tests require XINSIGHT_POSTGRES_DSN")
	}
	if getenv("GITHUB_ACTIONS") == "true" && getenv(ScopeEnv) == "" {
		return nil // The existing CI service creates its own fixed databases and role DSNs.
	}
	if getenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST") != "1" && getenv(ScopeEnv) == "" {
		return nil // Existing CI and other separately authorized runners retain their contracts.
	}
	// Native qualification must derive its role DSNs inside the checked factory.
	for _, role := range []string{"ADMIN", "READER", "WORKSPACE", "IMPORTER"} {
		if getenv("XINSIGHT_HOSTCTL_"+role+"_DSN") != "" {
			return errors.New("inherited hostctl DSNs forbidden in scoped local tests")
		}
	}
	if getenv("XINSIGHT_POSTGRES_DSN") == "" {
		return nil // Offline tests do not acquire a database target.
	}
	raw, err := readFile(getenv(ScopeEnv))
	if err != nil {
		return errors.New("recorded local test database scope required before running tests")
	}
	var scope Scope
	if json.Unmarshal(raw, &scope) != nil {
		return errors.New("invalid recorded local test database scope")
	}
	c, err := validate(getenv("XINSIGHT_POSTGRES_DSN"), scope)
	if err != nil {
		return err
	}
	return verify(c, scope)
}

// Main runs before m.Run, covering both legacy direct-DSN tests and factories.
// Rejection occurs before any fixture, migration, CREATE DATABASE or test body.
func Main(run func() int) int {
	if err := check(os.Getenv, os.ReadFile, probe); err != nil {
		fmt.Fprintln(os.Stderr, "PostgreSQL tests refused before m.Run:", err)
		return 2
	}
	return run()
}
