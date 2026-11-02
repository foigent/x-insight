package hostctl

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/db"
)

func TestSchemaContractFunctionACLIdentityOrderPostgres(t *testing.T) {
	ctx, credentials := connectorDigestDatabase(t)
	manager := PostgresManager{}
	if err := manager.EnsureRoles(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, credentials.AdminDSN, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	// Reproduce the exact pre-rotation entry that failed in native CI, without
	// rotating credentials when only schema comparison changed.
	if _, err := manager.PreviewCleanBaseline(ctx, credentials); err != nil {
		t.Fatalf("native pre-rotation schema preview: %v", err)
	}
	roleIDs := func() string {
		t.Helper()
		var value string
		if err := admin.QueryRowContext(ctx, `SELECT jsonb_object_agg(rolname,oid)::text FROM pg_roles WHERE rolname IN ('xinsight_importer','xinsight_workspace','xi_contract_acl_swap')`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	originalRoleIDs := roleIDs()
	definitions := func(q cleanBaselineSchemaQuerier) map[string]string {
		t.Helper()
		rows, err := querySchemaContractRows(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, row := range rows {
			if row.Kind == "F" && strings.HasPrefix(row.Name, "xinsight_sync_") {
				out[row.Name] = row.Definition
			}
		}
		if len(out) != 3 {
			t.Fatal("expected all three dual-role projection functions")
		}
		return out
	}
	before := definitions(admin)
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Only this transaction sees the swapped name/OID ordering. Both grantees
	// retain identical EXECUTE sets on these three functions; rollback restores
	// the original cluster roles and all other privileges before publication.
	defer tx.Rollback()
	for _, statement := range []string{
		`ALTER ROLE xinsight_importer RENAME TO xi_contract_acl_swap`,
		`ALTER ROLE xinsight_workspace RENAME TO xinsight_importer`,
		`ALTER ROLE xi_contract_acl_swap RENAME TO xinsight_workspace`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(before, definitions(tx)) {
		t.Fatal("equivalent function ACL depends on role OID creation order")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, definitions(admin)) {
		t.Fatal("role identity rollback changed function ACL")
	}
	if roleIDs() != originalRoleIDs {
		t.Fatal("role name/OID mapping was not restored exactly")
	}
	for _, mutation := range []string{
		`REVOKE EXECUTE ON FUNCTION public.xinsight_sync_receiver(text) FROM xinsight_workspace`,
		`GRANT EXECUTE ON FUNCTION public.xinsight_sync_receiver(text) TO PUBLIC`,
		`GRANT EXECUTE ON FUNCTION public.xinsight_sync_receiver(text) TO xinsight_workspace WITH GRANT OPTION`,
	} {
		tx, err := admin.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(ctx, mutation); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		inspection, err := InspectCleanBaselineSchemaContract(ctx, tx)
		if err != nil || inspection.Valid {
			tx.Rollback()
			t.Fatalf("real function ACL drift was not rejected: valid=%t err=%v", inspection.Valid, err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, definitions(admin)) {
			t.Fatal("ACL negative case did not restore original privileges")
		}
	}
}
