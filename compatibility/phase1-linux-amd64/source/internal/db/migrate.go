package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func RunMigrations(ctx context.Context, dsn string, dir string) (int, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	if err := conn.PingContext(ctx); err != nil {
		return 0, err
	}
	if _, err := conn.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version text PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
)`); err != nil {
		return 0, err
	}

	files, err := migrationFiles(dir)
	if err != nil {
		return 0, err
	}

	applied := 0
	for _, file := range files {
		version := migrationVersion(file)
		if version == "" {
			return applied, fmt.Errorf("invalid migration filename: %s", file)
		}

		alreadyApplied, err := isMigrationApplied(ctx, conn, version)
		if err != nil {
			return applied, err
		}
		if alreadyApplied {
			continue
		}

		sqlBytes, err := os.ReadFile(file)
		if err != nil {
			return applied, err
		}
		if err := applyMigration(ctx, conn, version, string(sqlBytes)); err != nil {
			return applied, fmt.Errorf("apply %s: %w", filepath.Base(file), err)
		}
		applied++
	}

	return applied, nil
}

func migrationFiles(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

func migrationVersion(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".up.sql")
}

func isMigrationApplied(ctx context.Context, conn *sql.DB, version string) (bool, error) {
	var exists bool
	err := conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&exists)
	return exists, err
}

func applyMigration(ctx context.Context, conn *sql.DB, version string, stmt string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, stmt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return err
	}
	return tx.Commit()
}
