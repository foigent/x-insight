package hostctl

import (
	"errors"
	"reflect"
	"testing"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/testsourcefacts"
	"github.com/jackc/pgx/v5/pgconn"
)

// The accepted P1 predecessor is schema38. This requires the existing explicit
// synthetic PG fixture; an offline compile does not qualify the migration.
func TestContentSchema39UpgradePreservesLegacyGrants(t *testing.T) {
	ctx, credentials := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.EnsureRoles(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, credentials.AdminDSN, connectorDigestMigrations(t, 38)); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	admin := managedSQL(t, credentials.AdminDSN)
	source := testsourcefacts.RegisterSource(t, admin)
	// Retained non-default revisions and inactive rows must not acquire content
	// authority or reset the existing authorization generation on upgrade.
	for i, active := range []bool{true, false} {
		subject := []string{"xs_3939393939393939", "xs_3838383838383838"}[i]
		if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,revision,active) VALUES($1,$2::uuid,true,true,7,$3)`, subject, source, active); err != nil {
			t.Fatal(err)
		}
	}
	var before string
	if err := admin.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(g) ORDER BY id)::text FROM workbench_access_grants g`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if n, err := db.RunMigrations(ctx, credentials.AdminDSN, connectorDigestMigrations(t, 39)); err != nil || n != 1 {
		t.Fatal("schema38 to schema39 upgrade", n, err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	// Scope labels need version updates when a case adopts content. The writer
	// must still be unable to move a label to a different case. The HTTP suite
	// exercises the complete UPSERT with this same restricted role.
	writer := managedSQL(t, credentials.WorkspaceDSN)
	var currentUser string
	if err := writer.QueryRowContext(ctx, `SELECT current_user`).Scan(&currentUser); err != nil || currentUser != "xinsight_workspace" {
		t.Fatal("scope label test must use the restricted workspace role", currentUser, err)
	}
	result, err := writer.ExecContext(ctx, `UPDATE investigation_case_scope_labels SET schema_version=schema_version WHERE false`)
	if err != nil {
		t.Fatal("workspace cannot maintain the scope label version", err)
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 0 {
		t.Fatal("permission probe changed a scope label", rows, err)
	}
	_, err = writer.ExecContext(ctx, `UPDATE investigation_case_scope_labels SET case_id=case_id WHERE false`)
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatal("workspace must not acquire scope label identity updates", err)
	}
	var after string
	if err := admin.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(g)-ARRAY['allow_content','allowed_content_purposes','legacy_revision','content_revision'] ORDER BY id)::text FROM workbench_access_grants g`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("migration changed predecessor grant fields")
	}
	var preserved int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM workbench_access_grants WHERE NOT allow_content AND allowed_content_purposes='[]'::jsonb AND legacy_revision=revision AND revision=7 AND content_revision=0`).Scan(&preserved); err != nil || preserved != 2 {
		t.Fatal("legacy grant generations or explicit content defaults changed", preserved, err)
	}
	first, err := GenerateCleanBaselineSchemaContract(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateCleanBaselineSchemaContract(ctx, admin)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("schema39 generation was not deterministic", err)
	}
	if first.DatabaseSchema != "39" || first.MigrationCount != 39 {
		t.Fatal("generated contract does not describe actual schema39")
	}
	if n, err := db.RunMigrations(ctx, credentials.AdminDSN, connectorDigestMigrations(t, 39)); err != nil || n != 0 {
		t.Fatal("already applied migration was not idempotent", n, err)
	}
}

func TestContentSchema39UpgradeAcceptsExactPredecessorRolePolicy(t *testing.T) {
	ctx, credentials := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.EnsureRoles(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, credentials.AdminDSN, connectorDigestMigrations(t, 38)); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	admin := managedSQL(t, credentials.AdminDSN)
	exec := func(statement string) {
		t.Helper()
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	grantSnapshot := func() string {
		t.Helper()
		var snapshot string
		if err := admin.QueryRowContext(ctx, `SELECT jsonb_agg(jsonb_build_array(c.relname,c.relacl,a.attname,a.attacl) ORDER BY c.relname,a.attnum)::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped WHERE n.nspname='public' AND c.relkind IN ('r','p')`).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	// Reproduce the actual schema38 predecessor privilege, before the target
	// ApplyGrants has repaired it. No target privilege is required of that source.
	exec(`REVOKE UPDATE(schema_version) ON investigation_case_scope_labels FROM xinsight_workspace`)
	before := grantSnapshot()
	for _, version := range []string{"0.2.0-dev.b2", "0.2.0-dev.d1", "0.2.0-dev.d2", "0.2.0-dev.fpv1", "0.2.0-dev.fpv2", "0.2.0-dev.fpv3", "0.2.0-dev.fpv4", "0.2.0-dev.fpv5", "0.2.0-dev.fpv6"} {
		if err := manager.VerifyUpgradeSourceRoleBoundaries(ctx, credentials, version); err != nil {
			t.Fatal("retained four-column source role policy", version, err)
		}
	}
	if err := manager.VerifyRoleBoundaries(ctx, credentials); err == nil {
		t.Fatal("target accepted the missing scope label version grant")
	}
	if err := manager.VerifyUpgradeSourceRoleBoundaries(ctx, credentials, "0.2.0-dev.unknown"); err == nil {
		t.Fatal("unknown source received a historical permission exception")
	}
	if before != grantSnapshot() {
		t.Fatal("source or target permission inspection changed grants")
	}
	if _, err := db.RunMigrations(ctx, credentials.AdminDSN, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, credentials); err != nil {
		t.Fatal("target privilege convergence", err)
	}
	if err := manager.VerifyUpgradeSourceRoleBoundaries(ctx, credentials, "0.2.0-dev.fpv6"); err != nil {
		t.Fatal("source identity rejected already-converged privileges", err)
	}
	for _, probe := range []struct{ name, apply, undo string }{
		{"missing_required_column", `REVOKE UPDATE(case_version) ON investigation_case_scope_labels FROM xinsight_workspace`, `GRANT UPDATE(case_version) ON investigation_case_scope_labels TO xinsight_workspace`},
		{"label_identity_update", `GRANT UPDATE(case_id) ON investigation_case_scope_labels TO xinsight_workspace`, `REVOKE UPDATE(case_id) ON investigation_case_scope_labels FROM xinsight_workspace`},
		{"table_update", `GRANT UPDATE ON investigation_case_scope_labels TO xinsight_workspace`, `REVOKE UPDATE ON investigation_case_scope_labels FROM xinsight_workspace`},
		{"table_delete", `GRANT DELETE ON investigation_case_scope_labels TO xinsight_workspace`, `REVOKE DELETE ON investigation_case_scope_labels FROM xinsight_workspace`},
	} {
		t.Run(probe.name, func(t *testing.T) {
			exec(probe.apply)
			defer exec(probe.undo)
			before := grantSnapshot()
			if err := manager.VerifyUpgradeSourceRoleBoundaries(ctx, credentials, "0.2.0-dev.fpv6"); err == nil {
				t.Fatal("source accepted permission drift")
			}
			if err := manager.VerifyRoleBoundaries(ctx, credentials); err == nil {
				t.Fatal("target accepted permission drift")
			}
			if before != grantSnapshot() {
				t.Fatal("permission inspection repaired or changed the source")
			}
		})
		// Revoking a table-level test grant may also remove its column grants.
		// Reapply the product policy before testing the next independent drift.
		if err := manager.ApplyGrants(ctx, credentials); err != nil {
			t.Fatal("test privilege convergence", probe.name, err)
		}
		if err := manager.VerifyRoleBoundaries(ctx, credentials); err != nil {
			t.Fatal("test privilege restoration", probe.name, err)
		}
	}
}
