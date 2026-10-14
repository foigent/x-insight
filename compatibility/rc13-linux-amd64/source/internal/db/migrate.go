package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	MaintenanceLockKey int64 = 6308648141524501081
	MaxMigrationBytes        = 1 << 20
)

var (
	ErrMaintenanceBusy        = errors.New("exclusive database maintenance lock is busy")
	ErrMigrationDirty         = errors.New("database migration ledger is dirty")
	ErrMigrationChecksumDrift = errors.New("database migration checksum drift detected")
	ErrLegacyChecksumMissing  = errors.New("legacy migration checksum adoption is required")
	ErrMigrationSetMismatch   = errors.New("database migration set does not match the expected legacy source")
	migrationIDPattern        = regexp.MustCompile(`^[0-9]{6}_[a-z0-9_]+$`)
	sha256Pattern             = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type MigrationChecksum struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

type MigrationRecord struct {
	MigrationChecksum
	Dirty     bool      `json:"dirty"`
	AppliedAt time.Time `json:"applied_at"`
}

type ReadinessStatus struct {
	DatabaseSchema     string `json:"database_schema"`
	MigrationCount     int    `json:"migration_count"`
	MigrationSetSHA256 string `json:"migration_set_sha256"`
	PostgresMajor      int    `json:"postgres_major"`
	RepresentativeRead bool   `json:"representative_read"`
}

type MaintenanceSession struct {
	conn      *sql.Conn
	closeConn func()
	closed    bool
}

func RunMigrations(ctx context.Context, dsn string, dir string) (int, error) {
	session, err := AcquireMaintenanceSession(ctx, dsn)
	if err != nil {
		return 0, err
	}
	defer session.Close()
	return session.RunMigrations(ctx, dir)
}

func AcquireMaintenanceSession(ctx context.Context, dsn string) (*MaintenanceSession, error) {
	conn, closeConn, err := openPinnedConnection(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := acquireExclusiveMaintenanceLock(ctx, conn); err != nil {
		closeConn()
		return nil, err
	}
	return &MaintenanceSession{conn: conn, closeConn: closeConn}, nil
}

func (session *MaintenanceSession) Close() error {
	if session == nil || session.closed {
		return nil
	}
	session.closed = true
	releaseExclusiveMaintenanceLock(session.conn)
	session.closeConn()
	return nil
}

func (session *MaintenanceSession) RunMigrations(ctx context.Context, dir string) (int, error) {
	if session == nil || session.closed || session.conn == nil {
		return 0, fmt.Errorf("database maintenance session is not active")
	}
	conn := session.conn

	if err := ensureMigrationLedger(ctx, conn); err != nil {
		return 0, err
	}
	if err := verifyMigrationDurability(ctx, conn); err != nil {
		return 0, err
	}
	files, err := LoadMigrationChecksums(dir)
	if err != nil {
		return 0, err
	}
	applied, err := loadMigrationRecords(ctx, conn)
	if err != nil {
		return 0, err
	}
	if err := validateAppliedMigrations(files, applied); err != nil {
		return 0, err
	}

	fileByID := make(map[string]MigrationChecksum, len(files))
	for _, file := range files {
		fileByID[file.ID] = file
	}
	appliedByID := make(map[string]MigrationRecord, len(applied))
	for _, record := range applied {
		appliedByID[record.ID] = record
	}

	appliedCount := 0
	for _, migration := range files {
		if _, exists := appliedByID[migration.ID]; exists {
			continue
		}
		path := filepath.Join(dir, migration.ID+".up.sql")
		statement, err := readMigration(path)
		if err != nil {
			return appliedCount, err
		}
		if actual := sha256Reference(statement); actual != fileByID[migration.ID].SHA256 {
			return appliedCount, fmt.Errorf("%w: migration %s changed after preflight", ErrMigrationChecksumDrift, migration.ID)
		}
		if err := applyMigration(ctx, conn, migration, string(statement)); err != nil {
			return appliedCount, fmt.Errorf("apply %s: %w", filepath.Base(path), err)
		}
		appliedCount++
	}
	return appliedCount, nil
}

func (session *MaintenanceSession) CountOtherSessions(ctx context.Context, roles []string) (int, error) {
	if session == nil || session.closed || session.conn == nil {
		return 0, fmt.Errorf("database maintenance session is not active")
	}
	if len(roles) == 0 || len(roles) > 16 {
		return 0, fmt.Errorf("one to sixteen database roles are required")
	}
	for _, role := range roles {
		if role == "" || len(role) > 63 {
			return 0, fmt.Errorf("invalid database role identity")
		}
	}
	var count int
	if err := session.conn.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_stat_activity
WHERE datname = current_database()
  AND pid <> pg_backend_pid()
  AND NOT (usename = 'xinsight_admin' AND application_name = 'xinsight_healthcheck')
  AND usename = ANY($1)`, roles).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (session *MaintenanceSession) SetRoleLogin(ctx context.Context, role string, enabled bool) error {
	if session == nil || session.closed || session.conn == nil {
		return fmt.Errorf("database maintenance session is not active")
	}
	if role != "xinsight_importer" {
		return fmt.Errorf("maintenance session may only gate the importer role")
	}
	login := "NOLOGIN"
	if enabled {
		login = "LOGIN"
	}
	_, err := session.conn.ExecContext(ctx, `ALTER ROLE xinsight_importer `+login)
	return err
}

func LoadMigrationChecksums(dir string) ([]MigrationChecksum, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return nil, fmt.Errorf("migration directory contains no up migrations: %s", dir)
	}
	records := make([]MigrationChecksum, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, path := range matches {
		id := migrationVersion(path)
		if !migrationIDPattern.MatchString(id) {
			return nil, fmt.Errorf("invalid migration filename: %s", path)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("duplicate migration ID: %s", id)
		}
		seen[id] = struct{}{}
		contents, err := readMigration(path)
		if err != nil {
			return nil, err
		}
		records = append(records, MigrationChecksum{ID: id, SHA256: sha256Reference(contents)})
	}
	return records, nil
}

func AdoptLegacyMigrationChecksums(ctx context.Context, dsn string, dir string, expected []MigrationChecksum) (int, error) {
	session, err := AcquireMaintenanceSession(ctx, dsn)
	if err != nil {
		return 0, err
	}
	defer session.Close()
	return session.AdoptLegacyMigrationChecksums(ctx, dir, expected)
}

func (session *MaintenanceSession) AdoptLegacyMigrationChecksums(ctx context.Context, dir string, expected []MigrationChecksum) (int, error) {
	if session == nil || session.closed || session.conn == nil {
		return 0, fmt.Errorf("database maintenance session is not active")
	}
	if err := validateExpectedMigrations(expected); err != nil {
		return 0, err
	}
	conn := session.conn
	if err := ensureMigrationLedger(ctx, conn); err != nil {
		return 0, err
	}
	if err := verifyMigrationDurability(ctx, conn); err != nil {
		return 0, err
	}

	available, err := LoadMigrationChecksums(dir)
	if err != nil {
		return 0, err
	}
	availableByID := make(map[string]string, len(available))
	for _, migration := range available {
		availableByID[migration.ID] = migration.SHA256
	}
	for _, migration := range expected {
		if availableByID[migration.ID] != migration.SHA256 {
			return 0, fmt.Errorf("%w: signed migration %s does not match local release files", ErrMigrationChecksumDrift, migration.ID)
		}
	}

	applied, err := loadMigrationRecords(ctx, conn)
	if err != nil {
		return 0, err
	}
	if len(applied) != len(expected) {
		return 0, fmt.Errorf("%w: applied=%d expected=%d", ErrMigrationSetMismatch, len(applied), len(expected))
	}
	for index, record := range applied {
		want := expected[index]
		if record.ID != want.ID {
			return 0, fmt.Errorf("%w: applied=%s expected=%s", ErrMigrationSetMismatch, record.ID, want.ID)
		}
		if record.Dirty {
			return 0, fmt.Errorf("%w: migration %s", ErrMigrationDirty, record.ID)
		}
		if record.SHA256 != "" && record.SHA256 != want.SHA256 {
			return 0, fmt.Errorf("%w: migration %s", ErrMigrationChecksumDrift, record.ID)
		}
	}

	updated := 0
	for _, migration := range expected {
		result, err := conn.ExecContext(ctx, `
UPDATE schema_migrations
SET checksum_sha256 = $2
WHERE version = $1 AND checksum_sha256 IS NULL`, migration.ID, migration.SHA256)
		if err != nil {
			return updated, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return updated, err
		}
		updated += int(rows)
	}
	return updated, nil
}

func ReadMigrationLedger(ctx context.Context, dsn string) ([]MigrationRecord, error) {
	conn, closeConn, err := openPinnedConnection(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer closeConn()
	return loadMigrationRecords(ctx, conn)
}

func MigrationSetSHA256(values []MigrationChecksum) (string, error) {
	if err := validateExpectedMigrations(values); err != nil {
		return "", err
	}
	digest := sha256.New()
	for _, value := range values {
		_, _ = digest.Write([]byte(value.ID))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(value.SHA256))
		_, _ = digest.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}

func openPinnedConnection(ctx context.Context, dsn string) (*sql.Conn, func(), error) {
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, err
	}
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, nil, err
	}
	conn, err := database.Conn(ctx)
	if err != nil {
		_ = database.Close()
		return nil, nil, err
	}
	closeFn := func() {
		_ = conn.Close()
		_ = database.Close()
	}
	return conn, closeFn, nil
}

func acquireExclusiveMaintenanceLock(ctx context.Context, conn *sql.Conn) error {
	var acquired bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, MaintenanceLockKey).Scan(&acquired); err != nil {
		return err
	}
	if !acquired {
		return ErrMaintenanceBusy
	}
	return nil
}

func releaseExclusiveMaintenanceLock(conn *sql.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, MaintenanceLockKey)
}

func ensureMigrationLedger(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version text PRIMARY KEY,
  checksum_sha256 text,
  dirty boolean NOT NULL DEFAULT false,
  applied_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum_sha256 text;
ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS dirty boolean NOT NULL DEFAULT false;
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'schema_migrations_checksum_sha256_check'
      AND conrelid = 'schema_migrations'::regclass
  ) THEN
    ALTER TABLE schema_migrations ADD CONSTRAINT schema_migrations_checksum_sha256_check CHECK (
      checksum_sha256 IS NULL OR checksum_sha256 ~ '^sha256:[a-f0-9]{64}$'
    );
  END IF;
END $$;`)
	return err
}

func loadMigrationRecords(ctx context.Context, conn *sql.Conn) ([]MigrationRecord, error) {
	var exists bool
	if err := conn.QueryRowContext(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	var hasChecksum, hasDirty bool
	if err := conn.QueryRowContext(ctx, `
SELECT
  EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'schema_migrations' AND column_name = 'checksum_sha256'),
  EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'schema_migrations' AND column_name = 'dirty')`).Scan(&hasChecksum, &hasDirty); err != nil {
		return nil, err
	}
	checksumExpression := "NULL::text"
	if hasChecksum {
		checksumExpression = "checksum_sha256"
	}
	dirtyExpression := "false"
	if hasDirty {
		dirtyExpression = "dirty"
	}
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
SELECT version, %s, %s, applied_at
FROM schema_migrations
ORDER BY version`, checksumExpression, dirtyExpression))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]MigrationRecord, 0)
	for rows.Next() {
		var record MigrationRecord
		var checksum sql.NullString
		if err := rows.Scan(&record.ID, &checksum, &record.Dirty, &record.AppliedAt); err != nil {
			return nil, err
		}
		record.SHA256 = checksum.String
		records = append(records, record)
	}
	return records, rows.Err()
}

func validateAppliedMigrations(files []MigrationChecksum, applied []MigrationRecord) error {
	if len(applied) > len(files) {
		return fmt.Errorf("%w: applied migration count exceeds release", ErrMigrationChecksumDrift)
	}
	fileByID := make(map[string]string, len(files))
	for _, file := range files {
		fileByID[file.ID] = file.SHA256
	}
	for index, record := range applied {
		if record.ID != files[index].ID {
			return fmt.Errorf("%w: applied migrations are not a release prefix", ErrMigrationChecksumDrift)
		}
		if record.Dirty {
			return fmt.Errorf("%w: migration %s", ErrMigrationDirty, record.ID)
		}
		expected, exists := fileByID[record.ID]
		if !exists {
			return fmt.Errorf("%w: applied migration %s is absent from release", ErrMigrationChecksumDrift, record.ID)
		}
		if record.SHA256 == "" {
			return fmt.Errorf("%w: migration %s", ErrLegacyChecksumMissing, record.ID)
		}
		if record.SHA256 != expected {
			return fmt.Errorf("%w: migration %s", ErrMigrationChecksumDrift, record.ID)
		}
	}
	return nil
}

func validateExpectedMigrations(expected []MigrationChecksum) error {
	if len(expected) == 0 {
		return fmt.Errorf("expected legacy migration set is empty")
	}
	previous := ""
	for _, migration := range expected {
		if !migrationIDPattern.MatchString(migration.ID) || !sha256Pattern.MatchString(migration.SHA256) {
			return fmt.Errorf("invalid expected migration identity")
		}
		if previous != "" && strings.Compare(previous, migration.ID) >= 0 {
			return fmt.Errorf("expected migrations are not strictly ordered")
		}
		previous = migration.ID
	}
	return nil
}

func applyMigration(ctx context.Context, conn *sql.Conn, migration MigrationChecksum, statement string) error {
	if _, err := conn.ExecContext(ctx, `
INSERT INTO schema_migrations (version, checksum_sha256, dirty)
VALUES ($1, $2, true)`, migration.ID, migration.SHA256); err != nil {
		return err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, statement); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE schema_migrations
SET dirty = false, applied_at = now()
WHERE version = $1 AND checksum_sha256 = $2 AND dirty = true`, migration.ID, migration.SHA256)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("migration ledger clean transition affected %d rows", rows)
	}
	return tx.Commit()
}

func verifyMigrationDurability(ctx context.Context, conn *sql.Conn) error {
	var synchronousCommit string
	if err := conn.QueryRowContext(ctx, `SHOW synchronous_commit`).Scan(&synchronousCommit); err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(synchronousCommit), "off") {
		return fmt.Errorf("synchronous_commit must not be off during migration")
	}
	return nil
}

func readMigration(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("migration is not a regular file: %s", path)
	}
	if info.Size() <= 0 || info.Size() > MaxMigrationBytes {
		return nil, fmt.Errorf("migration size must be between 1 and %d bytes: %s", MaxMigrationBytes, path)
	}
	return os.ReadFile(path)
}

func migrationVersion(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".up.sql")
}

func sha256Reference(contents []byte) string {
	digest := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(digest[:])
}
