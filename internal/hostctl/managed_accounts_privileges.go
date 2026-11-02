package hostctl

import (
	"context"
	"database/sql"
	"fmt"
)

func applyAccountPrivileges(ctx context.Context, tx *sql.Tx) error {
	var present bool
	if e := tx.QueryRowContext(ctx, `SELECT to_regclass('xinsight_accounts.authority') IS NOT NULL`).Scan(&present); e != nil {
		return e
	}
	if !present {
		return nil
	}
	for _, q := range []string{
		`REVOKE ALL ON SCHEMA xinsight_accounts FROM PUBLIC,xinsight_reader,xinsight_importer`,
		`REVOKE ALL ON ALL TABLES IN SCHEMA xinsight_accounts FROM PUBLIC,xinsight_reader,xinsight_importer`,
		`REVOKE ALL ON ALL FUNCTIONS IN SCHEMA xinsight_accounts FROM PUBLIC,xinsight_reader,xinsight_importer`,
		`GRANT USAGE ON SCHEMA xinsight_accounts TO xinsight_workspace`,
		`GRANT SELECT ON xinsight_accounts.authority TO xinsight_workspace`,
		`GRANT SELECT,INSERT,UPDATE ON xinsight_accounts.accounts,xinsight_accounts.password_tickets TO xinsight_workspace`,
		`GRANT EXECUTE ON FUNCTION xinsight_accounts.set_project_read_grant(text,bigint,text,uuid,text,uuid,bigint,text[],boolean,boolean) TO xinsight_workspace`,
	} {
		if _, e := tx.ExecContext(ctx, q); e != nil {
			return fmt.Errorf("local account least-privilege reconciliation failed")
		}
	}
	return nil
}
func verifyAccountPrivileges(ctx context.Context, c DatabaseCredentials) error {
	conn, e := sql.Open("pgx", c.AdminDSN)
	if e != nil {
		return e
	}
	defer conn.Close()
	var present bool
	if e = conn.QueryRowContext(ctx, `SELECT to_regclass('xinsight_accounts.authority') IS NOT NULL`).Scan(&present); e != nil || !present {
		return e
	}
	var good bool
	e = conn.QueryRowContext(ctx, `SELECT
 NOT has_schema_privilege('xinsight_reader','xinsight_accounts','USAGE')
 AND NOT has_schema_privilege('xinsight_importer','xinsight_accounts','USAGE')
 AND NOT EXISTS(SELECT 1 FROM pg_tables WHERE schemaname='xinsight_accounts' AND (has_table_privilege('xinsight_reader',format('%I.%I',schemaname,tablename),'SELECT,INSERT,UPDATE,DELETE') OR has_table_privilege('xinsight_importer',format('%I.%I',schemaname,tablename),'SELECT,INSERT,UPDATE,DELETE')))
 AND has_schema_privilege('xinsight_workspace','xinsight_accounts','USAGE')
 AND NOT has_schema_privilege('xinsight_workspace','xinsight_accounts','CREATE')
 AND has_table_privilege('xinsight_workspace','xinsight_accounts.accounts','SELECT') AND has_table_privilege('xinsight_workspace','xinsight_accounts.accounts','INSERT') AND has_table_privilege('xinsight_workspace','xinsight_accounts.accounts','UPDATE')
 AND has_table_privilege('xinsight_workspace','xinsight_accounts.password_tickets','SELECT') AND has_table_privilege('xinsight_workspace','xinsight_accounts.password_tickets','INSERT') AND has_table_privilege('xinsight_workspace','xinsight_accounts.password_tickets','UPDATE')
 AND NOT has_table_privilege('xinsight_workspace','xinsight_accounts.accounts','DELETE,TRUNCATE,TRIGGER,REFERENCES')
 AND NOT has_table_privilege('xinsight_workspace','xinsight_accounts.authority','INSERT,UPDATE,DELETE,TRUNCATE')
 AND NOT has_table_privilege('xinsight_workspace','public.workbench_access_grants','INSERT,UPDATE,DELETE')`).Scan(&good)
	if e != nil || !good {
		return fmt.Errorf("local account private-schema boundary mismatch")
	}
	return nil
}
