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
		importerMissingSelect      int
		importerForbiddenMutation  int
		importerMissingInsert      int
		importerForbiddenColumn    int
		importerMissingColumn      int
	)
	if err := admin.QueryRowContext(ctx, `
SELECT
  count(*) FILTER (WHERE NOT has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'SELECT')),
  count(*) FILTER (
    WHERE has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'INSERT')
       OR has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'UPDATE')
       OR has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'DELETE')
       OR has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'TRUNCATE')
       OR has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'REFERENCES')
       OR has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'TRIGGER')
  ),
  count(*) FILTER (WHERE NOT has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'SELECT')),
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
         NOT (tablename = ANY(`+workspaceUpdateTablesArraySQL+`))
         AND has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'UPDATE')
       )
  ),
  count(*) FILTER (
    WHERE tablename = ANY(`+workspaceInsertTablesArraySQL+`)
      AND NOT has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'INSERT')
  ),
  count(*) FILTER (
    WHERE tablename = ANY(`+workspaceUpdateTablesArraySQL+`)
      AND NOT has_table_privilege('xinsight_workspace', format('%I.%I', schemaname, tablename), 'UPDATE')
  ),
  count(*) FILTER (WHERE NOT has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'SELECT')),
  count(*) FILTER (
    WHERE has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'DELETE')
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
	); err != nil {
		return errors.New("database role privilege verification failed")
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
		importerMissingSelect != 0 ||
		importerForbiddenMutation != 0 ||
		importerMissingInsert != 0 ||
		importerForbiddenColumn != 0 ||
		importerMissingColumn != 0 {
		return fmt.Errorf("database role privilege boundary is invalid")
	}
	return nil
}
