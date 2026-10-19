package hostctl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/testpostgres"
)

func TestPostgreSQL16AdminCredentialRotationQualification(t *testing.T) {
	if os.Getenv("XINSIGHT_ADMIN_ROTATION_DATABASE_TEST") != "1" {
		t.Skip("set XINSIGHT_ADMIN_ROTATION_DATABASE_TEST=1 for the isolated admin rotation qualification")
	}
	readerDSN := os.Getenv("XINSIGHT_HOSTCTL_READER_DSN")
	workspaceDSN := os.Getenv("XINSIGHT_HOSTCTL_WORKSPACE_DSN")
	importerDSN := os.Getenv("XINSIGHT_HOSTCTL_IMPORTER_DSN")
	adminDSN := os.Getenv("XINSIGHT_HOSTCTL_ADMIN_DSN")
	if readerDSN == "" || workspaceDSN == "" || importerDSN == "" || adminDSN == "" {
		t.Fatal("all four XINSIGHT_HOSTCTL_*_DSN values are required")
	}

	root := persistentHostTestDir(t)
	t.Setenv(AllowTestRootEnv, "1")
	t.Setenv(TestRootEnv, root)
	paths, err := PathsFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	setRef := filepath.Join(paths.DatabaseSetsDir, "admin-rotation-predecessor")
	if err := os.MkdirAll(setRef, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"postgres-reader.dsn": readerDSN, "postgres-workspace.dsn": workspaceDSN,
		"postgres-importer.dsn": importerDSN, "postgres-admin.dsn": adminDSN,
	} {
		writeHostFile(t, filepath.Join(setRef, name), []byte(value+"\n"), 0o600)
	}
	if err := atomicSymlink(setRef, paths.DatabaseCurrentLink); err != nil {
		t.Fatal(err)
	}
	predecessor, err := LoadDatabaseCredentials(paths)
	if err != nil {
		t.Fatal(err)
	}
	manager := PostgresManager{Runner: ExecRunner{}}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := manager.EnsureRoles(ctx, predecessor); err != nil {
		t.Fatalf("ensure role boundary: %v", err)
	}
	maintenance, err := manager.AcquireMaintenance(ctx, predecessor)
	if err != nil {
		t.Fatalf("acquire maintenance: %v", err)
	}
	migrations := filepath.Join("..", "..", "migrations")
	checksums, err := db.LoadMigrationChecksums(migrations)
	if err != nil {
		maintenance.Close()
		t.Fatal(err)
	}
	if applied, err := maintenance.RunMigrations(ctx, migrations); err != nil || applied != len(checksums) {
		maintenance.Close()
		t.Fatalf("apply current migrations count=%d err=%v", applied, err)
	}
	if err := manager.ApplyGrants(ctx, predecessor); err != nil {
		maintenance.Close()
		t.Fatalf("apply role grants: %v", err)
	}
	if err := maintenance.SetRoleLogin(ctx, "xinsight_importer", true); err != nil {
		maintenance.Close()
		t.Fatalf("enable importer role: %v", err)
	}
	if err := maintenance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, predecessor); err != nil {
		t.Fatalf("verify predecessor roles: %v", err)
	}
	beforeFacts, err := manager.Inspect(ctx, predecessor)
	if err != nil {
		t.Fatal(err)
	}
	beforePreview, err := manager.PreviewCleanBaseline(ctx, predecessor)
	if err != nil {
		t.Fatalf("preview predecessor product state: %v", err)
	}
	boundary, err := manager.InspectAdminBoundary(ctx, predecessor)
	if err != nil || boundary.AdminSessions != 0 {
		t.Fatalf("predecessor admin boundary sessions=%d err=%v", boundary.AdminSessions, err)
	}

	successorRef := filepath.Join(paths.DatabaseSetsDir, "admin-rotation-successor")
	successor, _, err := StageAdminCredentialSet(
		paths, predecessor, successorRef,
		bytes.NewReader(bytes.Repeat([]byte{0x93, 0x4f, 0x21, 0xc7}, 64)),
	)
	if err != nil {
		t.Fatalf("stage successor: %v", err)
	}
	beforeRaw, err := readAdminCredentialSetBytes(paths, predecessor.SetRef)
	if err != nil {
		t.Fatal(err)
	}
	afterRaw, err := readAdminCredentialSetBytes(paths, successor.SetRef)
	if err != nil {
		t.Fatal(err)
	}
	for _, inherited := range adminCredentialMemberSpecs[:3] {
		if !bytes.Equal(beforeRaw[inherited.filename], afterRaw[inherited.filename]) {
			t.Fatalf("inherited credential changed: %s", inherited.role)
		}
	}

	concurrent, err := sql.Open("pgx", predecessor.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	concurrent.SetMaxOpenConns(1)
	var concurrentPID int
	if err := concurrent.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&concurrentPID); err != nil {
		concurrent.Close()
		t.Fatalf("open concurrent admin session: %v", err)
	}
	if err := manager.ChangeAdminVerifier(ctx, predecessor, successor, bytes.NewReader(bytes.Repeat([]byte{0x31}, 32))); err == nil ||
		!strings.Contains(err.Error(), "zero concurrent admin sessions") {
		concurrent.Close()
		t.Fatalf("concurrent admin session was not rejected: %v", err)
	}
	if err := concurrent.Close(); err != nil {
		t.Fatal(err)
	}
	if transition, err := manager.ProbeAdminCredentialTransition(ctx, predecessor, successor); err != nil ||
		transition != AdminCredentialTransitionPredecessor {
		t.Fatalf("failed preflight changed verifier: transition=%s err=%v", transition, err)
	}

	if err := manager.ChangeAdminVerifier(ctx, predecessor, successor, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32))); err != nil {
		t.Fatalf("change admin verifier: %v", err)
	}
	if transition, err := manager.ProbeAdminCredentialTransition(ctx, predecessor, successor); err != nil ||
		transition != AdminCredentialTransitionSuccessor {
		t.Fatalf("successor did not become authoritative: transition=%s err=%v", transition, err)
	}
	if err := atomicSymlink(successor.SetRef, paths.DatabaseCurrentLink); err != nil {
		t.Fatal(err)
	}
	recoveryDatabase, err := sql.Open("pgx", successor.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	recoverySession, err := recoveryDatabase.Conn(ctx)
	if err != nil {
		recoveryDatabase.Close()
		t.Fatal(err)
	}
	var recoveryPID int
	if err := recoverySession.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&recoveryPID); err != nil {
		recoverySession.Close()
		recoveryDatabase.Close()
		t.Fatal(err)
	}
	terminated, err := manager.TerminateOtherAdminSessions(ctx, successor)
	if err != nil || terminated < 1 {
		recoverySession.Close()
		recoveryDatabase.Close()
		t.Fatalf("terminate race/recovery sessions: %v", err)
	}
	if err := recoverySession.QueryRowContext(ctx, `SELECT 1`).Scan(new(int)); err == nil {
		recoverySession.Close()
		recoveryDatabase.Close()
		t.Fatal("race/recovery admin session remained usable after containment")
	}
	_ = recoverySession.Close()
	_ = recoveryDatabase.Close()
	proof, err := manager.ValidateAdminCredentialTransition(ctx, predecessor, successor)
	if err != nil || !proof.SuccessorFreshConnection || !proof.PredecessorAuthRejected ||
		!proof.ValidationSessionClosed || proof.AdminSessionsRemaining != 0 {
		t.Fatalf("transition proof=%+v err=%v", proof, err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, successor); err != nil {
		t.Fatalf("verify successor roles: %v", err)
	}
	afterFacts, err := manager.Inspect(ctx, successor)
	if err != nil {
		t.Fatal(err)
	}
	afterPreview, err := manager.PreviewCleanBaseline(ctx, successor)
	if err != nil {
		t.Fatalf("preview successor product state: %v", err)
	}
	beforeFacts.DatabaseSizeBytes = 0
	afterFacts.DatabaseSizeBytes = 0
	if !reflect.DeepEqual(beforeFacts, afterFacts) || !reflect.DeepEqual(beforePreview, afterPreview) {
		t.Fatal("admin credential rotation changed schema, migration, product, connector, or checkpoint state")
	}
	active, err := readAbsoluteSymlink(paths.DatabaseCurrentLink)
	if err != nil || active != successor.SetRef {
		t.Fatalf("active successor=%q err=%v", active, err)
	}
}

func TestPostgreSQL16RoleBackupAndIsolatedRestoreQualification(t *testing.T) {
	if os.Getenv("XINSIGHT_HOSTCTL_DATABASE_TEST") != "1" {
		t.Skip("set XINSIGHT_HOSTCTL_DATABASE_TEST=1 for the destructive dedicated-database qualification")
	}
	readerDSN := os.Getenv("XINSIGHT_HOSTCTL_READER_DSN")
	workspaceDSN := os.Getenv("XINSIGHT_HOSTCTL_WORKSPACE_DSN")
	importerDSN := os.Getenv("XINSIGHT_HOSTCTL_IMPORTER_DSN")
	adminDSN := os.Getenv("XINSIGHT_HOSTCTL_ADMIN_DSN")
	if os.Getenv(testpostgres.ScopeEnv) != "" {
		// TestMain has already verified the recorded cluster/database scope.
		// Derive all roles in a fresh fixture; never inherit host role DSNs or
		// weaken the native credential/backup/restore qualification below.
		_, scoped := connectorDigestDatabase(t)
		readerDSN, workspaceDSN = scoped.ReaderDSN, scoped.WorkspaceDSN
		importerDSN, adminDSN = scoped.ImporterDSN, scoped.AdminDSN
	}
	if readerDSN == "" || workspaceDSN == "" || importerDSN == "" || adminDSN == "" {
		t.Fatal("all four XINSIGHT_HOSTCTL_*_DSN values are required")
	}

	root := persistentHostTestDir(t)
	t.Setenv(AllowTestRootEnv, "1")
	t.Setenv(TestRootEnv, root)
	paths, err := PathsFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	setRef := filepath.Join(paths.DatabaseSetsDir, "qualification")
	if err := os.MkdirAll(setRef, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"postgres-reader.dsn": readerDSN, "postgres-workspace.dsn": workspaceDSN,
		"postgres-importer.dsn": importerDSN, "postgres-admin.dsn": adminDSN,
	} {
		writeHostFile(t, filepath.Join(setRef, name), []byte(value+"\n"), 0o600)
	}
	if err := atomicSymlink(setRef, paths.DatabaseCurrentLink); err != nil {
		t.Fatal(err)
	}
	credentials, err := LoadDatabaseCredentials(paths)
	if err != nil {
		t.Fatalf("load role credentials: %v", err)
	}
	manager := PostgresManager{Runner: ExecRunner{}}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	migrations := filepath.Join("..", "..", "migrations")
	checksums, err := db.LoadMigrationChecksums(migrations)
	if err != nil {
		t.Fatal(err)
	}
	const (
		upgradeBackupMigrationCount = 9  // Accepted legacy-b9e5811 Phase 1 source schema.
		rc10MigrationCount          = 20 // Exact 0.3.0-rc.10 source schema.
	)
	if len(checksums) <= rc10MigrationCount {
		t.Fatalf("forward-restore regression requires migrations after schema %d, found %d", rc10MigrationCount, len(checksums))
	}
	upgradeBackupMigrations := filepath.Join(root, "upgrade-backup-migrations")
	if err := os.Mkdir(upgradeBackupMigrations, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, checksum := range checksums[:upgradeBackupMigrationCount] {
		contents, err := os.ReadFile(filepath.Join(migrations, checksum.ID+".up.sql"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(upgradeBackupMigrations, checksum.ID+".up.sql"), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	facts, err := manager.Inspect(ctx, credentials)
	if err != nil {
		t.Fatalf("inspect dedicated database: %v", err)
	}
	if facts.PostgresMajor != 16 || facts.PublicTableCount != 0 || len(facts.MigrationLedger) != 0 || !facts.AdminRoleIsSuperuser {
		t.Fatalf("dedicated database is not an empty PostgreSQL 16 admin boundary: %+v", facts)
	}
	maintenance, err := manager.AcquireMaintenance(ctx, credentials)
	if err != nil {
		t.Fatalf("acquire exclusive maintenance: %v", err)
	}
	if err := manager.EnsureRoles(ctx, credentials); err != nil {
		maintenance.Close()
		t.Fatalf("ensure roles: %v", err)
	}
	if err := maintenance.SetRoleLogin(ctx, "xinsight_importer", false); err != nil {
		maintenance.Close()
		t.Fatalf("gate importer: %v", err)
	}
	if sessions, err := maintenance.CountOtherSessions(ctx, []string{"xinsight_reader", "xinsight_workspace", "xinsight_importer", "xinsight_admin"}); err != nil || sessions != 0 {
		maintenance.Close()
		t.Fatalf("unexpected role sessions count=%d err=%v", sessions, err)
	}
	applied, err := maintenance.RunMigrations(ctx, upgradeBackupMigrations)
	if err != nil || applied != upgradeBackupMigrationCount {
		maintenance.Close()
		t.Fatalf("apply upgrade-backup migration prefix count=%d err=%v", applied, err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		maintenance.Close()
		t.Fatalf("apply least-privilege grants: %v", err)
	}
	if err := maintenance.SetRoleLogin(ctx, "xinsight_importer", true); err != nil {
		maintenance.Close()
		t.Fatalf("enable importer for boundary proof: %v", err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, credentials); err != nil {
		maintenance.Close()
		t.Fatalf("verify role boundaries: %v", err)
	}
	if err := maintenance.Close(); err != nil {
		t.Fatalf("close maintenance: %v", err)
	}
	backupSourceFacts, err := manager.Inspect(ctx, credentials)
	if err != nil || len(backupSourceFacts.MigrationLedger) != upgradeBackupMigrationCount {
		t.Fatalf("upgrade backup source ledger=%d expected=%d err=%v", len(backupSourceFacts.MigrationLedger), upgradeBackupMigrationCount, err)
	}

	backupPath := filepath.Join(paths.BackupsDir, "postgresql16-qualification.dump")
	backup, err := manager.Backup(ctx, credentials, backupPath)
	if err != nil {
		t.Fatalf("backup dedicated database: %v", err)
	}
	if backup.PGDumpMajor != 16 || backup.PGRestoreMajor != 16 || !strings.HasPrefix(backup.SHA256, "sha256:") {
		t.Fatalf("backup identity=%+v", backup)
	}
	maintenance, err = manager.AcquireMaintenance(ctx, credentials)
	if err != nil {
		t.Fatalf("reacquire exclusive maintenance: %v", err)
	}
	if err := maintenance.SetRoleLogin(ctx, "xinsight_importer", false); err != nil {
		maintenance.Close()
		t.Fatalf("regate importer: %v", err)
	}
	for _, checksum := range checksums[upgradeBackupMigrationCount:rc10MigrationCount] {
		contents, err := os.ReadFile(filepath.Join(migrations, checksum.ID+".up.sql"))
		if err != nil {
			maintenance.Close()
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(upgradeBackupMigrations, checksum.ID+".up.sql"), contents, 0o600); err != nil {
			maintenance.Close()
			t.Fatal(err)
		}
	}
	applied, err = maintenance.RunMigrations(ctx, upgradeBackupMigrations)
	if err != nil || applied != rc10MigrationCount-upgradeBackupMigrationCount {
		maintenance.Close()
		t.Fatalf("apply rc.10 migration suffix count=%d err=%v", applied, err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		maintenance.Close()
		t.Fatalf("apply schema-20 target grants before rc.10 source fixture: %v", err)
	}
	if err := maintenance.SetRoleLogin(ctx, "xinsight_importer", true); err != nil {
		maintenance.Close()
		t.Fatalf("enable importer for rc.10 source-policy proof: %v", err)
	}
	qualifyRC10SourcePrivilegeUpgrade(t, ctx, manager, credentials)
	if err := maintenance.SetRoleLogin(ctx, "xinsight_importer", false); err != nil {
		maintenance.Close()
		t.Fatalf("regate importer after rc.10 source-policy proof: %v", err)
	}
	applied, err = maintenance.RunMigrations(ctx, migrations)
	if err != nil || applied != len(checksums)-rc10MigrationCount {
		maintenance.Close()
		t.Fatalf("apply forward migration suffix count=%d err=%v", applied, err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		maintenance.Close()
		t.Fatalf("reapply least-privilege grants: %v", err)
	}
	if err := maintenance.SetRoleLogin(ctx, "xinsight_importer", true); err != nil {
		maintenance.Close()
		t.Fatalf("reenable importer: %v", err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, credentials); err != nil {
		maintenance.Close()
		t.Fatalf("reverify role boundaries: %v", err)
	}
	if err := maintenance.Close(); err != nil {
		t.Fatalf("close forward-migration maintenance: %v", err)
	}
	currentFacts, err := manager.Inspect(ctx, credentials)
	if err != nil || len(currentFacts.MigrationLedger) != len(checksums) {
		t.Fatalf("current source ledger=%d expected=%d err=%v", len(currentFacts.MigrationLedger), len(checksums), err)
	}
	schemaDatabase, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	freshContract, err := InspectCleanBaselineSchemaContract(ctx, schemaDatabase)
	if err != nil || !freshContract.Valid || !freshContract.LayoutMatches {
		t.Fatalf("fresh schema contract=%+v err=%v", freshContract, err)
	}
	if output := os.Getenv("XINSIGHT_A0_SCHEMA_OUTPUT"); output != "" {
		manifest, err := GenerateCleanBaselineSchemaContract(ctx, schemaDatabase)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	secondFresh, err := InspectCleanBaselineSchemaContract(ctx, schemaDatabase)
	if err != nil || secondFresh.ActualSHA256 != freshContract.ActualSHA256 ||
		secondFresh.ActualLayoutSHA256 != freshContract.ActualLayoutSHA256 {
		t.Fatalf("schema contract is not deterministic: first=%+v second=%+v err=%v", freshContract, secondFresh, err)
	}
	t.Run("legacy_rc7_to_rc8_ledger_order_is_semantically_equal", func(t *testing.T) {
		tx, err := schemaDatabase.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `
CREATE TABLE schema_migrations_reordered (
  version text NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT now(),
  checksum_sha256 text,
  dirty boolean NOT NULL DEFAULT false
);
INSERT INTO schema_migrations_reordered (version, applied_at, checksum_sha256, dirty)
SELECT version, applied_at, checksum_sha256, dirty FROM schema_migrations;
DROP TABLE schema_migrations;
ALTER TABLE schema_migrations_reordered RENAME TO schema_migrations;
ALTER TABLE schema_migrations ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);
ALTER TABLE schema_migrations ADD CONSTRAINT schema_migrations_checksum_sha256_check CHECK (
  checksum_sha256 IS NULL OR checksum_sha256 ~ '^sha256:[a-f0-9]{64}$'
)`); err != nil {
			t.Fatalf("stage legacy ledger order: %v", err)
		}
		inspection, err := InspectCleanBaselineSchemaContract(ctx, tx)
		if err != nil || !inspection.Valid || inspection.LayoutMatches ||
			inspection.ActualSHA256 != freshContract.ActualSHA256 ||
			inspection.ActualLayoutSHA256 == freshContract.ActualLayoutSHA256 {
			t.Fatalf("legacy order contract=%+v err=%v", inspection, err)
		}
	})
	for name, fixture := range map[string]struct {
		statement string
		owner     string
		object    string
		reason    string
	}{
		"type": {
			statement: `ALTER TABLE source_systems ALTER COLUMN display_name TYPE varchar(255)`,
			owner:     "source_systems", object: "display_name", reason: "semantic_property_drift",
		},
		"default": {
			statement: `ALTER TABLE source_systems ALTER COLUMN status SET DEFAULT 'qualification'`,
			owner:     "source_systems", object: "status", reason: "semantic_property_drift",
		},
		"nullability": {
			statement: `ALTER TABLE source_systems ALTER COLUMN display_name DROP NOT NULL`,
			owner:     "source_systems", object: "display_name", reason: "semantic_property_drift",
		},
		"collation": {
			statement: `ALTER TABLE source_systems ALTER COLUMN display_name TYPE text COLLATE "C"`,
			owner:     "source_systems", object: "display_name", reason: "semantic_property_drift",
		},
		"identity": {
			statement: `ALTER TABLE adapter_import_states ALTER COLUMN pages_committed DROP DEFAULT;
ALTER TABLE adapter_import_states ALTER COLUMN pages_committed ADD GENERATED BY DEFAULT AS IDENTITY`,
			owner: "adapter_import_states", object: "pages_committed", reason: "semantic_property_drift",
		},
		"generated": {
			statement: `ALTER TABLE source_systems DROP COLUMN deployment_ref;
ALTER TABLE source_systems ADD COLUMN deployment_ref text
GENERATED ALWAYS AS (source_type || ':' || source_key) STORED`,
			owner: "source_systems", object: "deployment_ref", reason: "semantic_property_drift",
		},
		"constraint": {
			statement: `ALTER TABLE source_systems ADD CONSTRAINT qualification_status_check CHECK (status <> '')`,
			owner:     "source_systems", object: "qualification_status_check", reason: "unexpected_object",
		},
	} {
		t.Run("clean_baseline_rejects_property_drift_"+name, func(t *testing.T) {
			assertCleanBaselineObjectDriftRejected(
				t, ctx, schemaDatabase, fixture.statement, fixture.reason, fixture.owner, fixture.object,
			)
		})
	}
	functionIdentity := "xinsight_redeem_connector_invitation(p_invitation_token text, p_acknowledgement_payload text, p_control_token text, p_control_token_expires_at timestamp with time zone)"
	for name, fixture := range map[string]struct {
		statement string
		reason    string
	}{
		"strictness": {
			statement: `ALTER FUNCTION public.xinsight_redeem_connector_invitation(text, text, text, timestamptz) STRICT`,
			reason:    "semantic_property_drift",
		},
		"parallel_safety": {
			statement: `ALTER FUNCTION public.xinsight_redeem_connector_invitation(text, text, text, timestamptz) PARALLEL SAFE`,
			reason:    "semantic_property_drift",
		},
		"cost": {
			statement: `ALTER FUNCTION public.xinsight_redeem_connector_invitation(text, text, text, timestamptz) COST 2`,
			reason:    "semantic_property_drift",
		},
		"rows": {
			statement: `ALTER FUNCTION public.xinsight_redeem_connector_invitation(text, text, text, timestamptz) ROWS 2`,
			reason:    "semantic_property_drift",
		},
		"configuration": {
			statement: `ALTER FUNCTION public.xinsight_redeem_connector_invitation(text, text, text, timestamptz) SET work_mem TO '4MB'`,
			reason:    "semantic_property_drift",
		},
		"public_acl": {
			statement: `GRANT EXECUTE ON FUNCTION public.xinsight_redeem_connector_invitation(text, text, text, timestamptz) TO PUBLIC`,
			reason:    "semantic_property_drift",
		},
		"owner": {
			statement: `ALTER FUNCTION public.xinsight_redeem_connector_invitation(text, text, text, timestamptz) OWNER TO xinsight_workspace`,
			reason:    "missing_object",
		},
	} {
		t.Run("clean_baseline_rejects_function_drift_"+name, func(t *testing.T) {
			assertCleanBaselineObjectDriftRejected(
				t, ctx, schemaDatabase, fixture.statement, fixture.reason, "xinsight_admin", functionIdentity,
			)
		})
	}
	for name, statement := range map[string]string{
		"namespace": `CREATE SCHEMA qualification_unknown_schema`,
		"foreign_key": `ALTER TABLE adapter_import_states
ADD CONSTRAINT qualification_unexpected_fk
FOREIGN KEY (source_system_id) REFERENCES source_systems(id)`,
		"trigger": `CREATE TRIGGER qualification_unexpected_trigger
BEFORE UPDATE ON source_systems
FOR EACH ROW EXECUTE FUNCTION reject_connector_checkpoint_epoch_history_mutation()`,
		"view": `CREATE VIEW qualification_unknown_view AS SELECT 1 AS value`,
	} {
		t.Run("clean_baseline_rejects_unknown_"+name, func(t *testing.T) {
			assertCleanBaselineObjectDriftRejected(t, ctx, schemaDatabase, statement, "unexpected_object", "", "")
		})
	}
	if err := schemaDatabase.Close(); err != nil {
		t.Fatal(err)
	}

	assertSQLRejected(t, credentials.ReaderDSN, `
INSERT INTO source_systems (source_key, source_type, display_name)
VALUES ('reader-forbidden', 'qualification', 'reader forbidden')`)
	assertSQLRejected(t, credentials.ReaderDSN, `UPDATE source_systems SET display_name = display_name`)
	assertSQLRejected(t, credentials.ReaderDSN, `DELETE FROM evidence_events`)
	assertSQLRejected(t, credentials.ReaderDSN, `CREATE TABLE reader_forbidden_ddl (id integer)`)
	assertSQLRejected(t, credentials.WorkspaceDSN, `
INSERT INTO service_identities (
  id, display_name, permissions, created_by_subject, created_at, updated_at
) VALUES (
  'xsi_0123456789abcdefghijkl', 'forbidden workspace create',
  ARRAY['evidence.read'], 'root', now(), now()
)`)
	assertSQLRejected(t, credentials.WorkspaceDSN, `
INSERT INTO service_identity_tokens (
  token_digest, service_identity_id, token_prefix, created_at, expires_at
) VALUES (
  'sha256:0000000000000000000000000000000000000000000000000000000000000000',
  'xsi_0123456789abcdefghijkl', 'xib_01234567', now(), now() + interval '1 hour'
)`)
	assertSQLRejected(t, credentials.ImporterDSN, `DELETE FROM evidence_events`)
	assertSQLRejected(t, credentials.ImporterDSN, `UPDATE schema_migrations SET dirty = dirty`)
	assertSQLRejected(t, credentials.ImporterDSN, `
INSERT INTO service_identities (
  id, display_name, permissions, created_by_subject, created_at, updated_at
) VALUES (
  'xsi_0123456789abcdefghijkl', 'forbidden importer create',
  ARRAY['evidence.read'], 'root', now(), now()
)`)
	assertSQLRejected(t, credentials.ImporterDSN, `
UPDATE investigation_cases SET title = title`)
	assertSQLAccepted(t, credentials.ImporterDSN, `
UPDATE evidence_events SET import_provenance_id = import_provenance_id`)
	assertSQLRejected(t, credentials.ImporterDSN, `
UPDATE evidence_events SET event_hash = event_hash`)
	assertSQLAccepted(t, credentials.ImporterDSN, `
UPDATE source_systems SET source_version = source_version, status = status`)
	assertSQLRejected(t, credentials.ImporterDSN, `
UPDATE source_systems SET display_name = display_name`)
	assertSQLAccepted(t, credentials.ImporterDSN, `
UPDATE adapter_manifests SET adapter_version = adapter_version, active = active`)
	assertSQLRejected(t, credentials.ImporterDSN, `
UPDATE adapter_manifests SET manifest_json = manifest_json`)
	assertSQLAccepted(t, credentials.ImporterDSN, `
UPDATE adapter_import_states SET status = status, updated_at = updated_at`)
	assertSQLRejected(t, credentials.ImporterDSN, `
UPDATE adapter_import_states SET authorization_ref = authorization_ref`)
	assertSQLCount(t, credentials.ReaderDSN, `SELECT count(*) FROM evidence_events`, 0)

	before, err := manager.EvidenceFingerprint(ctx, credentials)
	if err != nil {
		t.Fatalf("source evidence fingerprint: %v", err)
	}
	token, err := randomToken(6)
	if err != nil {
		t.Fatal(err)
	}
	newDatabase := "xinsight_restore_qualification_" + token
	restored, err := StageRestoredCredentialSet(paths, credentials, newDatabase)
	if err != nil {
		t.Fatalf("stage restored credentials: %v", err)
	}
	if err := manager.RestoreToNewDatabase(ctx, credentials, restored, backup.Path, migrations, checksums); err != nil {
		t.Fatalf("restore to isolated database: %v", err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, restored); err != nil {
		t.Fatalf("verify restored role boundaries: %v", err)
	}
	after, err := manager.EvidenceFingerprint(ctx, restored)
	if err != nil {
		t.Fatalf("restored evidence fingerprint: %v", err)
	}
	if after != before {
		t.Fatalf("restored fingerprint differs: before=%+v after=%+v", before, after)
	}
	restoredFacts, err := manager.Inspect(ctx, restored)
	if err != nil || restoredFacts.PostgresMajor != 16 || len(restoredFacts.MigrationLedger) != len(checksums) {
		t.Fatalf("restored facts=%+v err=%v", restoredFacts, err)
	}
	if _, err := manager.VerifyCleanBaselineBackupRestore(
		ctx,
		credentials,
		newDatabase,
		backup.Path,
		backup.SHA256,
		migrations,
		checksums,
		CleanBaselineDatabasePreview{},
	); err == nil {
		t.Fatal("clean-baseline restore accepted a pre-existing verification database")
	}
	if exists, err := manager.DatabaseExists(ctx, credentials, newDatabase); err != nil || !exists {
		t.Fatalf("pre-existing verification database was modified: exists=%t err=%v", exists, err)
	}
	if err := manager.RetireCleanBaselineVerificationDatabase(ctx, credentials, newDatabase); err != nil {
		t.Fatalf("retire generic isolated qualification database: %v", err)
	}
	if exists, err := manager.DatabaseExists(ctx, credentials, newDatabase); err != nil || exists {
		t.Fatalf("isolated qualification database remains: exists=%t err=%v", exists, err)
	}

	adminDatabase, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapInput := checkpointepoch.PrepareInput{
		Binding: checkpointepoch.Binding{
			SourceSystemID:    cleanBaselineSourceSystemID,
			AdapterManifestID: cleanBaselineAdapterManifestID,
			CursorKey:         checkpointepoch.CompletedExecutionCursorKey,
		},
		WorkerUnit:         checkpointepoch.CompletedExecutionWorkerUnit,
		NextSourceInstance: "qualification-old-source",
		NextCursor:         "qualification-old-cursor",
		NextHighWatermark:  "qualification-old-cursor",
		AuthorizationRef:   "decision://qualification-old",
	}
	if _, err := checkpointepoch.BootstrapEmpty(ctx, adminDatabase, checkpointepoch.EmptyBootstrapInput{
		PrepareInput: bootstrapInput, CheckpointEpoch: 5,
	}, time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)); err != nil {
		adminDatabase.Close()
		t.Fatalf("seed rollback-source epoch: %v", err)
	}
	if err := adminDatabase.Close(); err != nil {
		t.Fatal(err)
	}
	newInput := bootstrapInput
	newInput.NextSourceInstance = "qualification-new-source"
	newInput.NextCursor = "qualification-empty-cursor"
	newInput.NextHighWatermark = "qualification-empty-cursor"
	newInput.AuthorizationRef = "decision://qualification-new"
	preview, err := manager.PreviewCleanBaseline(ctx, credentials)
	if err != nil {
		t.Fatalf("preview clean baseline: %v", err)
	}
	if preview.SchemaContractSHA256 != expectedCleanBaselineSchemaContract.SemanticSHA256 ||
		preview.CheckpointFloor.HistoricalMaxEpoch != 5 ||
		preview.CheckpointFloor.PriorIdentityRows != 1 || len(preview.Tables) != len(cleanBaselineTables) ||
		!containsString(preview.CheckpointFloor.PriorSourceInstanceSHA256, hashCleanBaselineText("qualification-old-source")) ||
		containsString(preview.CheckpointFloor.PriorSourceInstanceSHA256, hashCleanBaselineText(newInput.NextSourceInstance)) {
		t.Fatalf("clean-baseline preview=%+v", preview)
	}
	dataResetBackupPath := filepath.Join(paths.BackupsDir, "data-reset-retained.dump")
	dataResetBackup, err := manager.Backup(ctx, credentials, dataResetBackupPath)
	if err != nil {
		t.Fatalf("create fresh DATA_RESET backup: %v", err)
	}
	if _, err := os.Stat(backup.Path); err != nil {
		t.Fatalf("historical release/upgrade backup was removed: %v", err)
	}
	preview, err = bindCleanBaselinePreviewToBackup(preview, dataResetBackup.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	verificationDatabase := "xinsight_restore_data_reset_qualification"
	backupRestoreProof, err := manager.VerifyCleanBaselineBackupRestore(
		ctx,
		credentials,
		verificationDatabase,
		dataResetBackup.Path,
		dataResetBackup.SHA256,
		migrations,
		checksums,
		preview,
	)
	if err != nil {
		t.Fatalf("verify fresh DATA_RESET backup by isolated restore: %v", err)
	}
	if err := verifyCleanBaselineBackupRestoreProof(backupRestoreProof, dataResetBackup.SHA256); err != nil {
		t.Fatalf("isolated DATA_RESET restore proof: %v", err)
	}
	if exists, err := manager.DatabaseExists(ctx, credentials, verificationDatabase); err != nil || exists {
		t.Fatalf("DATA_RESET verification database remains: exists=%t err=%v", exists, err)
	}
	cleanToken, err := randomToken(6)
	if err != nil {
		t.Fatal(err)
	}
	cleanDatabase := "xinsight_clean_qualification_" + cleanToken
	cleanCredentials, err := StageCleanBaselineCredentialSet(paths, credentials, cleanDatabase)
	if err != nil {
		t.Fatal(err)
	}
	cleanProof, err := manager.CreateCleanBaselineDatabase(
		ctx,
		credentials,
		cleanCredentials,
		migrations,
		checksums,
	)
	if err != nil {
		t.Fatalf("create clean baseline: %v", err)
	}
	if cleanProof.PostgresMajor != 16 || cleanProof.MigrationCount != len(checksums) ||
		cleanProof.SchemaContractSHA256 != expectedCleanBaselineSchemaContract.SemanticSHA256 ||
		cleanProof.HistoricalRows != 0 || cleanProof.EpochOperationRows != 0 ||
		cleanProof.EpochHistoryRows != 0 || cleanProof.CheckpointStateRows != 0 {
		t.Fatalf("clean-baseline proof=%+v", cleanProof)
	}
	active, err := manager.BootstrapEmptyCheckpoint(
		ctx,
		cleanCredentials,
		newInput,
		preview.CheckpointFloor.HistoricalMaxEpoch+1,
		time.Date(2026, 8, 5, 2, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("bootstrap empty checkpoint after DATA_RESET: %v", err)
	}
	verifiedActive, err := manager.VerifyEmptyCheckpoint(
		ctx, cleanCredentials, newInput, preview.CheckpointFloor.HistoricalMaxEpoch+1,
	)
	if err != nil || verifiedActive.StateSHA256 != active.StateSHA256 || active.Epoch != 6 ||
		active.SourceInstance != newInput.NextSourceInstance || active.WorkerStatus != "inactive" {
		t.Fatalf("checkpoint bootstrap active=%+v verified=%+v err=%v", active, verifiedActive, err)
	}
	var oldStateCount int
	oldDatabase, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := oldDatabase.QueryRowContext(ctx, `
SELECT count(*) FROM adapter_import_states
WHERE activation_source_instance = 'qualification-old-source'`).Scan(&oldStateCount); err != nil {
		oldDatabase.Close()
		t.Fatal(err)
	}
	if err := oldDatabase.Close(); err != nil {
		t.Fatal(err)
	}
	if oldStateCount != 1 {
		t.Fatalf("rollback source was mutated, old state count=%d", oldStateCount)
	}
	if err := manager.RetireCleanBaselinePredecessorDatabase(
		ctx,
		cleanCredentials,
		credentials,
	); err != nil {
		t.Fatalf("retire predecessor after durable-boundary qualification: %v", err)
	}
	if err := manager.RetireCleanBaselinePredecessorDatabase(
		ctx,
		cleanCredentials,
		credentials,
	); err != nil {
		t.Fatalf("idempotent predecessor absence readback: %v", err)
	}
	if _, err := manager.VerifyBackup(ctx, dataResetBackup.Path, dataResetBackup.SHA256); err != nil {
		t.Fatalf("sole retained DATA_RESET dump changed after predecessor retirement: %v", err)
	}
	if _, err := os.Stat(backup.Path); err != nil {
		t.Fatalf("historical release/upgrade backup was treated as live-state contamination: %v", err)
	}
}

func qualifyRC10SourcePrivilegeUpgrade(
	t *testing.T,
	ctx context.Context,
	manager PostgresManager,
	credentials DatabaseCredentials,
) {
	t.Helper()
	admin, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, `
GRANT UPDATE ON TABLE service_identities, service_identity_tokens
TO xinsight_workspace`); err != nil {
		t.Fatalf("stage exact rc.10 workspace UPDATE policy: %v", err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, credentials); err == nil {
		t.Fatal("rc.12 target policy accepted the rc.10 table UPDATE source policy")
	}
	if err := manager.VerifyUpgradeSourceRoleBoundaries(
		ctx,
		credentials,
		"0.3.0-rc.10",
	); err != nil {
		t.Fatalf("exact rc.10 source policy was rejected: %v", err)
	}
	if err := manager.VerifyUpgradeSourceRoleBoundaries(
		ctx,
		credentials,
		"0.3.0-rc.99",
	); err == nil {
		t.Fatal("rc.10 source policy was accepted under an unknown source identity")
	}
	for _, testCase := range []struct {
		name    string
		grant   string
		revoke  string
		restore string
	}{
		{
			name:   "extra_delete",
			grant:  `GRANT DELETE ON TABLE service_identities TO xinsight_workspace`,
			revoke: `REVOKE DELETE ON TABLE service_identities FROM xinsight_workspace`,
		},
		{
			name:    "overwide_update",
			grant:   `GRANT UPDATE ON TABLE source_systems TO xinsight_workspace`,
			revoke:  `REVOKE UPDATE ON TABLE source_systems FROM xinsight_workspace`,
			restore: `GRANT UPDATE(status) ON TABLE source_systems TO xinsight_workspace`,
		},
		{
			name:   "unknown_trigger",
			grant:  `GRANT TRIGGER ON TABLE service_identity_tokens TO xinsight_workspace`,
			revoke: `REVOKE TRIGGER ON TABLE service_identity_tokens FROM xinsight_workspace`,
		},
	} {
		if _, err := admin.ExecContext(ctx, testCase.grant); err != nil {
			t.Fatalf("stage %s privilege drift: %v", testCase.name, err)
		}
		verifyErr := manager.VerifyUpgradeSourceRoleBoundaries(
			ctx,
			credentials,
			"0.3.0-rc.10",
		)
		if _, err := admin.ExecContext(ctx, testCase.revoke); err != nil {
			t.Fatalf("remove %s privilege drift: %v", testCase.name, err)
		}
		if testCase.restore != "" {
			if _, err := admin.ExecContext(ctx, testCase.restore); err != nil {
				t.Fatalf("restore %s source column grant: %v", testCase.name, err)
			}
		}
		if verifyErr == nil {
			t.Fatalf("rc.10 source policy accepted %s privilege drift", testCase.name)
		}
		if err := manager.VerifyUpgradeSourceRoleBoundaries(
			ctx,
			credentials,
			"0.3.0-rc.10",
		); err != nil {
			t.Fatalf("exact rc.10 source policy did not recover after %s: %v", testCase.name, err)
		}
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		t.Fatalf("converge rc.10 grants to the rc.12 target policy: %v", err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, credentials); err != nil {
		t.Fatalf("converged rc.12 target policy was rejected: %v", err)
	}
	if err := manager.VerifyUpgradeSourceRoleBoundaries(
		ctx,
		credentials,
		"0.3.0-rc.10",
	); err == nil {
		t.Fatal("rc.10 source verifier accepted the converged rc.12 target policy")
	}
}

func assertCleanBaselineObjectDriftRejected(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	statement string,
	expectedReason string,
	expectedOwner string,
	expectedName string,
) {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, statement); err != nil {
		_ = tx.Rollback()
		t.Fatalf("stage schema drift: %v", err)
	}
	inspection, inspectErr := InspectCleanBaselineSchemaContract(ctx, tx)
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		t.Fatalf("rollback schema drift: %v", rollbackErr)
	}
	if inspectErr != nil {
		t.Fatalf("inspect schema drift: %v", inspectErr)
	}
	if inspection.Valid || len(inspection.Mismatches) == 0 {
		t.Fatalf("schema drift was accepted: %s", statement)
	}
	matched := false
	for _, mismatch := range inspection.Mismatches {
		if mismatch.Reason == expectedReason &&
			(expectedOwner == "" || mismatch.Owner == expectedOwner) &&
			(expectedName == "" || mismatch.Name == expectedName) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("schema drift diagnostics=%+v", inspection.Mismatches)
	}
}

func assertSQLRejected(t *testing.T, dsn string, statement string) {
	t.Helper()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := database.ExecContext(ctx, statement); err == nil {
		t.Fatalf("database role unexpectedly accepted forbidden statement")
	}
}

func assertSQLAccepted(t *testing.T, dsn string, statement string) {
	t.Helper()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := database.ExecContext(ctx, statement); err != nil {
		t.Fatalf("database role rejected required statement: %v", err)
	}
}

func assertSQLCount(t *testing.T, dsn string, statement string, expected int) {
	t.Helper()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var count int
	if err := database.QueryRowContext(ctx, statement).Scan(&count); err != nil || count != expected {
		t.Fatalf("bounded database read count=%d expected=%d err=%v", count, expected, err)
	}
}
