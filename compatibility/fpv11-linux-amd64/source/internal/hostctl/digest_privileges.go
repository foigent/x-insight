package hostctl

import (
	"context"
	"database/sql"
	"errors"
)

const connectorDigestFunction = "public.digest(text,text)"

// Check the extension-owned implementation before granting execution, not just
// a function with a matching SQL signature. These are the installed PG16
// pgcrypto digest properties; no user-authored wrapper is accepted.
func verifyConnectorDigestIdentity(ctx context.Context, q cleanBaselineSchemaQuerier) error {
	var valid bool
	err := q.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM pg_proc p
  JOIN pg_language l ON l.oid=p.prolang
  JOIN pg_depend d ON d.classid='pg_proc'::regclass AND d.objid=p.oid AND d.deptype='e'
  JOIN pg_extension e ON e.oid=d.refobjid AND d.refclassid='pg_extension'::regclass
  WHERE p.oid=to_regprocedure($1)
    AND e.extname='pgcrypto' AND e.extnamespace='public'::regnamespace
    AND e.extowner=(SELECT oid FROM pg_roles WHERE rolname='xinsight_admin')
    AND p.proowner=e.extowner AND p.prokind='f' AND NOT p.prosecdef
    AND l.lanname='c' AND p.probin='$libdir/pgcrypto' AND p.prosrc='pg_digest'
    AND p.prorettype='bytea'::regtype AND p.proisstrict
    AND p.provolatile='i' AND p.proparallel='s'
)`, connectorDigestFunction).Scan(&valid)
	if err != nil || !valid {
		return errors.New("database connector digest function identity is invalid")
	}
	return nil
}

func verifyConnectorDigestPrivileges(ctx context.Context, credentials DatabaseCredentials, requireReader bool) error {
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return errors.New("database connector digest boundary cannot be opened")
	}
	defer database.Close()
	if err := verifyConnectorDigestIdentity(ctx, database); err != nil {
		return err
	}
	// Runtime logins must not administer roles: ADMIN can transfer a role's
	// ordinary EXECUTE without any object grant option, or make it SET-able.
	// An actionable ADMIN grantor must be inherited by an identity the login
	// can SET to. pg_has_role(..., 'MEMBER WITH ADMIN OPTION') alone also
	// traverses unreachable memberships; it is not a usable-grantor check.
	var roleAdministration bool
	if err := database.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM pg_roles runtime
  JOIN pg_roles identity ON pg_has_role(runtime.oid,identity.oid,'SET')
  WHERE runtime.rolname IN ('xinsight_reader','xinsight_workspace','xinsight_importer')
    AND (identity.rolsuper OR identity.rolcreaterole
      OR EXISTS (SELECT 1 FROM pg_auth_members membership
        JOIN pg_roles target ON target.oid=membership.roleid
        WHERE membership.admin_option AND NOT target.rolsuper
          AND pg_has_role(identity.oid,membership.member,'USAGE')))
)`).Scan(&roleAdministration); err != nil || roleAdministration {
		return errors.New("database connector digest privilege boundary is invalid")
	}
	if !requireReader {
		// Upgrade preflight has already checked the source's bound migration
		// ledger. Pre-23 sources predate the PUBLIC-function hardening contract;
		// retain their old role checks instead of imposing target grants early.
		var hardened bool
		if err := database.QueryRowContext(ctx, `SELECT EXISTS (
SELECT 1 FROM schema_migrations WHERE version='000023_harden_function_execute_privileges'
)`).Scan(&hardened); err != nil {
			return errors.New("database source function contract cannot be inspected")
		}
		if !hardened {
			return nil
		}
	}
	// Each login can act as itself or any SET-reachable identity. The effective
	// rights of each identity include its automatic inheritance, so mixed
	// SET/INHERIT paths are covered without treating mere membership as access.
	var readerExecute bool
	var forbidden int
	err = database.QueryRowContext(ctx, `
WITH runtime AS (
  SELECT oid,rolname FROM pg_roles
  WHERE rolname IN ('xinsight_reader','xinsight_workspace','xinsight_importer')
), usable AS (
  SELECT runtime.rolname,identity.oid
  FROM runtime JOIN pg_roles identity ON pg_has_role(runtime.oid,identity.oid,'SET')
)
SELECT has_function_privilege('xinsight_reader', $1, 'EXECUTE'),
  (SELECT count(*) FROM pg_proc p WHERE p.pronamespace='public'::regnamespace AND (
    EXISTS (SELECT 1 FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) a
            WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
    OR EXISTS (SELECT 1 FROM usable r
      WHERE has_function_privilege(r.oid,p.oid,'EXECUTE')
        AND NOT ((r.rolname='xinsight_reader' AND p.oid=to_regprocedure($1))
          OR (r.rolname='xinsight_workspace' AND (
            COALESCE(p.oid=to_regprocedure($2),false) OR COALESCE(p.oid=to_regprocedure($3),false)))
          OR (r.rolname='xinsight_importer' AND COALESCE(p.oid=to_regprocedure('public.synthetic_resource_cluster_id()'),false))))
    OR EXISTS (SELECT 1 FROM usable r
      WHERE has_function_privilege(r.oid,p.oid,'EXECUTE WITH GRANT OPTION'))
  ))`, connectorDigestFunction, connectorInvitationRedemptionFunction, managedWorkInstallIdentityFunction).Scan(&readerExecute, &forbidden)
	if err != nil || forbidden != 0 || (requireReader && !readerExecute) {
		return errors.New("database connector digest privilege boundary is invalid")
	}
	return nil
}
