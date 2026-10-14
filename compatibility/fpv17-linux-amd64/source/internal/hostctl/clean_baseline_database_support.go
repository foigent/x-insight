package hostctl

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// isolatedDatabaseCredentials rewrites only the in-memory database target. It
// is used for disposable restore verification and never stages or activates a
// credential set.
func isolatedDatabaseCredentials(current DatabaseCredentials, newDatabase string) (DatabaseCredentials, error) {
	values := []string{
		current.ReaderDSN,
		current.WorkspaceDSN,
		current.ImporterDSN,
		current.AdminDSN,
	}
	rewritten := make([]string, len(values))
	configs := make([]*pgx.ConnConfig, len(values))
	for index, value := range values {
		var err error
		rewritten[index], err = rewriteDatabaseDSN(value, newDatabase)
		if err != nil {
			return DatabaseCredentials{}, err
		}
		configs[index], err = pgx.ParseConfig(rewritten[index])
		if err != nil {
			return DatabaseCredentials{}, errors.New("database credential cannot be rewritten")
		}
	}
	return DatabaseCredentials{
		ReaderDSN: rewritten[0], WorkspaceDSN: rewritten[1],
		ImporterDSN: rewritten[2], AdminDSN: rewritten[3],
		Reader: configs[0], Workspace: configs[1],
		Importer: configs[2], Admin: configs[3],
	}, nil
}

// verifyWorkspaceRoleGrants verifies static grants without opening restricted
// role sessions. The clean-baseline restore proof calls it while importer login
// is deliberately disabled under exclusive maintenance.
func (manager PostgresManager) verifyWorkspaceRoleGrants(
	ctx context.Context,
	credentials DatabaseCredentials,
) error {
	if credentials.Workspace == nil {
		return fmt.Errorf("workspace database role is required")
	}
	return manager.verifyRoleGrantTables(ctx, credentials, workspaceUpdateTablesArraySQL, false)
}

func (manager PostgresManager) verifyRoleGrantTables(ctx context.Context, credentials DatabaseCredentials, workspaceUpdateTablesSQL string, allowLegacyScopeLabelVersion bool) error {
	admin, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return errors.New("admin database connection cannot be opened")
	}
	defer admin.Close()
	var (
		readerMissingSelect        int
		readerMutation             int
		workspaceMissingSelect     int
		workspaceForbiddenMutation int
		workspaceMissingInsert     int
		workspaceMissingUpdate     int
		workspaceForbiddenColumn   int
		workspaceMissingColumn     int
		importerMissingSelect      int
		importerForbiddenMutation  int
		importerMissingInsert      int
		importerMissingDelete      int
		importerForbiddenColumn    int
		importerMissingColumn      int
	)
	if err := admin.QueryRowContext(ctx, `
SELECT
  count(*) FILTER (WHERE tablename <> 'work_event_install_invitations' AND NOT has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'SELECT')),
  count(*) FILTER (
    WHERE has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'INSERT')
       OR has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'UPDATE')
       OR has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'DELETE')
       OR has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'TRUNCATE')
       OR has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'REFERENCES')
       OR has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'TRIGGER')
  ),
  count(*) FILTER (WHERE tablename <> 'work_event_install_invitations' AND NOT has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'SELECT')),
  count(*) FILTER (
    WHERE has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'DELETE')
       OR has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'TRUNCATE')
       OR has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'REFERENCES')
       OR has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'TRIGGER')
       OR (
         NOT (tablename = ANY(`+workspaceInsertTablesArraySQL+`))
         AND has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'INSERT')
       )
       OR (
         NOT (tablename = ANY(`+workspaceUpdateTablesSQL+`))
         AND has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'UPDATE')
       )
  ),
  count(*) FILTER (
    WHERE tablename = ANY(`+workspaceInsertTablesArraySQL+`)
      AND NOT has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'INSERT')
  ),
  count(*) FILTER (
    WHERE tablename = ANY(`+workspaceUpdateTablesSQL+`)
      AND NOT has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'UPDATE')
  ),
  count(*) FILTER (WHERE tablename <> 'work_event_install_invitations' AND NOT has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'SELECT')),
  count(*) FILTER (
    WHERE (NOT (tablename = ANY(`+importerDeleteTablesArraySQL+`)) AND has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'DELETE'))
       OR has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'TRUNCATE')
       OR has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'REFERENCES')
       OR has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'TRIGGER')
       OR has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'UPDATE')
       OR (
         NOT (tablename = ANY(`+importerInsertTablesArraySQL+`))
         AND has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'INSERT')
       )
  ),
  count(*) FILTER (
    WHERE tablename = ANY(`+importerInsertTablesArraySQL+`)
      AND NOT has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'INSERT')
  ),
  count(*) FILTER (
    WHERE tablename = ANY(`+importerDeleteTablesArraySQL+`)
      AND NOT has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'DELETE')
  )
FROM pg_tables
WHERE schemaname = 'public'`).Scan(
		&readerMissingSelect,
		&readerMutation,
		&workspaceMissingSelect,
		&workspaceForbiddenMutation,
		&workspaceMissingInsert,
		&workspaceMissingUpdate,
		&importerMissingSelect,
		&importerForbiddenMutation,
		&importerMissingInsert,
		&importerMissingDelete,
	); err != nil {
		return errors.New("database role privilege verification failed")
	}
	workspaceAllowedColumnValues := allowedColumnValuesSQL(workspaceUpdateColumnGrants)
	if err := admin.QueryRowContext(ctx, `
WITH allowed (table_name, column_name) AS (`+workspaceAllowedColumnValues+`)
SELECT
  count(*) FILTER (
    WHERE has_column_privilege(
      'xinsight_workspace',
      format('%I.%I', columns.table_schema, columns.table_name),
      columns.column_name,
      'UPDATE'
    )
      AND allowed.column_name IS NULL
  ),
  count(*) FILTER (
    WHERE allowed.column_name IS NOT NULL
      AND NOT ($1 AND columns.table_name='investigation_case_scope_labels' AND columns.column_name='schema_version')
      AND NOT has_column_privilege(
        'xinsight_workspace',
        format('%I.%I', columns.table_schema, columns.table_name),
        columns.column_name,
        'UPDATE'
      )
  )
FROM information_schema.columns AS columns
JOIN pg_tables AS tables
  ON tables.schemaname = columns.table_schema
 AND tables.tablename = columns.table_name
LEFT JOIN allowed
  ON allowed.table_name = columns.table_name
 AND allowed.column_name = columns.column_name
WHERE columns.table_schema = 'public'
  AND NOT (columns.table_name = ANY(`+workspaceUpdateTablesSQL+`))`, allowLegacyScopeLabelVersion).Scan(
		&workspaceForbiddenColumn,
		&workspaceMissingColumn,
	); err != nil {
		return errors.New("database role column privilege verification failed")
	}
	allowedColumnValues := allowedColumnValuesSQL(importerUpdateColumnGrants)
	if err := admin.QueryRowContext(ctx, `
WITH allowed (table_name, column_name) AS (`+allowedColumnValues+`)
SELECT
  count(*) FILTER (
    WHERE has_column_privilege(
      'xinsight_importer',
      format('%I.%I', columns.table_schema, columns.table_name),
      columns.column_name,
      'UPDATE'
    )
      AND allowed.column_name IS NULL
  ),
  count(*) FILTER (
    WHERE allowed.column_name IS NOT NULL
      AND NOT has_column_privilege(
        'xinsight_importer',
        format('%I.%I', columns.table_schema, columns.table_name),
        columns.column_name,
        'UPDATE'
      )
  )
FROM information_schema.columns AS columns
JOIN pg_tables AS tables
  ON tables.schemaname = columns.table_schema
 AND tables.tablename = columns.table_name
LEFT JOIN allowed
  ON allowed.table_name = columns.table_name
 AND allowed.column_name = columns.column_name
WHERE columns.table_schema = 'public'`).Scan(
		&importerForbiddenColumn,
		&importerMissingColumn,
	); err != nil {
		return errors.New("database role column privilege verification failed")
	}
	if readerMissingSelect != 0 ||
		readerMutation != 0 ||
		workspaceMissingSelect != 0 ||
		workspaceForbiddenMutation != 0 ||
		workspaceMissingInsert != 0 ||
		workspaceMissingUpdate != 0 ||
		workspaceForbiddenColumn != 0 ||
		workspaceMissingColumn != 0 ||
		importerMissingSelect != 0 ||
		importerForbiddenMutation != 0 ||
		importerMissingInsert != 0 ||
		importerMissingDelete != 0 ||
		importerForbiddenColumn != 0 ||
		importerMissingColumn != 0 {
		return fmt.Errorf("database role privilege boundary is invalid")
	}
	var connectorTableExists, connectorFunctionExists bool
	if err := admin.QueryRowContext(ctx, `
SELECT
  to_regclass('public.connector_installations') IS NOT NULL,
	  to_regprocedure($1) IS NOT NULL`, connectorInvitationRedemptionFunction).Scan(
		&connectorTableExists,
		&connectorFunctionExists,
	); err != nil {
		return errors.New("database connector function boundary verification failed")
	}
	if connectorTableExists != connectorFunctionExists {
		return fmt.Errorf("database connector function boundary is invalid")
	}
	if connectorFunctionExists {
		var workspaceExecute, readerExecute, importerExecute bool
		if err := admin.QueryRowContext(ctx, `
SELECT
  has_function_privilege('xinsight_workspace', $1, 'EXECUTE'),
  has_function_privilege('xinsight_reader', $1, 'EXECUTE'),
  has_function_privilege('xinsight_importer', $1, 'EXECUTE')`, connectorInvitationRedemptionFunction).Scan(
			&workspaceExecute,
			&readerExecute,
			&importerExecute,
		); err != nil {
			return errors.New("database connector function privilege verification failed")
		}
		// Reader/importer EXECUTE checks include privileges inherited from PUBLIC,
		// so both remaining false proves the function is not publicly executable.
		if !workspaceExecute || readerExecute || importerExecute {
			return fmt.Errorf("database connector function privilege boundary is invalid")
		}
	}
	var workInstallTableExists, workInstallFunctionExists bool
	if err := admin.QueryRowContext(ctx, `SELECT to_regclass('public.work_event_install_invitations') IS NOT NULL,
to_regprocedure($1) IS NOT NULL`, managedWorkInstallIdentityFunction).Scan(
		&workInstallTableExists, &workInstallFunctionExists,
	); err != nil || workInstallTableExists != workInstallFunctionExists {
		return errors.New("managed work install boundary verification failed")
	}
	if workInstallTableExists {
		var allowed bool
		if err := admin.QueryRowContext(ctx, `SELECT
  NOT has_table_privilege('xinsight_reader','public.work_event_install_invitations','SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
  AND NOT has_table_privilege('xinsight_workspace','public.work_event_install_invitations','SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
  AND NOT has_table_privilege('xinsight_importer','public.work_event_install_invitations','SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
  AND has_function_privilege('xinsight_workspace',$1,'EXECUTE')
  AND NOT has_function_privilege('xinsight_reader',$1,'EXECUTE')
  AND NOT has_function_privilege('xinsight_importer',$1,'EXECUTE')
  AND NOT has_function_privilege('xinsight_workspace',$1,'EXECUTE WITH GRANT OPTION')`, managedWorkInstallIdentityFunction).Scan(&allowed); err != nil || !allowed {
			return errors.New("managed work install privilege boundary invalid")
		}
	}
	var resourceFunctionExists bool
	if err := admin.QueryRowContext(ctx, `SELECT to_regprocedure('public.synthetic_resource_cluster_id()') IS NOT NULL`).Scan(&resourceFunctionExists); err != nil {
		return errors.New("resource identity function inspection failed")
	}
	if resourceFunctionExists {
		var allowed bool
		if err := admin.QueryRowContext(ctx, `SELECT has_function_privilege('xinsight_importer','public.synthetic_resource_cluster_id()','EXECUTE')
 AND NOT has_function_privilege('xinsight_reader','public.synthetic_resource_cluster_id()','EXECUTE')
 AND NOT has_function_privilege('xinsight_workspace','public.synthetic_resource_cluster_id()','EXECUTE')
 AND NOT has_function_privilege('xinsight_importer','public.synthetic_resource_cluster_id()','EXECUTE WITH GRANT OPTION')`).Scan(&allowed); err != nil || !allowed {
			return errors.New("resource identity function privilege boundary invalid")
		}
	}
	return nil
}
