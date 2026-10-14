package hostctl

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/db"
	"github.com/jackc/pgx/v5"
)

func currentMigrationSchema(t *testing.T) int {
	t.Helper()
	current, err := strconv.Atoi(buildinfo.DatabaseSchemaMax)
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func connectorDigestDatabase(t *testing.T) (context.Context, DatabaseCredentials) {
	t.Helper()
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("dedicated PostgreSQL required")
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() != "127.0.0.1" || os.Getenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST") != "1" {
		t.Fatal("dedicated synthetic instance required")
	}
	control, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("xi_digest_%d", time.Now().UnixNano())
	if _, err = control.Exec("CREATE DATABASE " + pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer control.Close()
		if _, err := control.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	u.Path = "/" + name
	// Preserve the approved test-cluster administrator authentication.
	// Replacing it with a fixture password only works under trust auth.
	c := DatabaseCredentials{AdminDSN: u.String()}
	c.Admin, err = pgx.ParseConfig(c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"reader", "workspace", "importer"} {
		v := *u
		v.User = url.UserPassword("xinsight_"+role, "synthetic digest qualification "+role)
		config, err := pgx.ParseConfig(v.String())
		if err != nil {
			t.Fatal(err)
		}
		switch role {
		case "reader":
			c.ReaderDSN, c.Reader = v.String(), config
		case "workspace":
			c.WorkspaceDSN, c.Workspace = v.String(), config
		case "importer":
			c.ImporterDSN, c.Importer = v.String(), config
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx, c
}

func connectorDigestMigrations(t *testing.T, limit int) string {
	t.Helper()
	path := t.TempDir()
	entries, err := os.ReadDir("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") && entry.Name()[:6] <= fmt.Sprintf("%06d", limit) {
			raw, err := os.ReadFile(filepath.Join("../../migrations", entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(path, entry.Name()), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return path
}

func TestConnectorDigestSourceTargetAndNegativePrivileges(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, 25)); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	// Stage exactly the missing privilege observed on the old implementation.
	if _, err := admin.Exec(`REVOKE EXECUTE ON FUNCTION public.digest(text,text) FROM xinsight_reader`); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyUpgradeSourceRoleBoundaries(ctx, c, "0.2.0-dev"); err != nil {
		t.Fatalf("schema25 source must reach repair: %v", err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, c); err == nil {
		t.Fatal("target accepted missing required digest")
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := manager.ApplyGrants(ctx, c); err != nil {
			t.Fatal(err)
		}
		if err := manager.VerifyRoleBoundaries(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	for _, probe := range []struct{ name, sql string }{
		{"reader_bytea", `GRANT EXECUTE ON FUNCTION public.digest(bytea,text) TO xinsight_reader`},
		{"reader_hmac", `GRANT EXECUTE ON FUNCTION public.hmac(text,text,text) TO xinsight_reader`},
		{"workspace_digest", `GRANT EXECUTE ON FUNCTION public.digest(text,text) TO xinsight_workspace`},
		{"importer_digest", `GRANT EXECUTE ON FUNCTION public.digest(text,text) TO xinsight_importer`},
		{"public_digest", `GRANT EXECUTE ON FUNCTION public.digest(text,text) TO PUBLIC`},
		{"delegable_digest", `GRANT EXECUTE ON FUNCTION public.digest(text,text) TO xinsight_reader WITH GRANT OPTION`},
		{"reader_credential_initializer", `GRANT EXECUTE ON FUNCTION public.xinsight_initialize_connector_control_credential(text,text) TO xinsight_reader`},
		{"public_credential_initializer", `GRANT EXECUTE ON FUNCTION public.xinsight_initialize_connector_control_credential(text,text) TO PUBLIC`},
		{"delegable_credential_initializer", `GRANT EXECUTE ON FUNCTION public.xinsight_initialize_connector_control_credential(text,text) TO xinsight_workspace WITH GRANT OPTION`},
		{"reader_fact_write", `GRANT UPDATE ON evidence_events TO xinsight_reader`},
	} {
		t.Run(probe.name, func(t *testing.T) {
			if _, err := admin.Exec(probe.sql); err != nil {
				t.Fatal(err)
			}
			if err := manager.VerifyRoleBoundaries(ctx, c); err == nil {
				t.Fatal("target accepted privilege drift")
			}
			if err := manager.VerifyUpgradeSourceRoleBoundaries(ctx, c, "0.2.0-dev"); err == nil {
				t.Fatal("source accepted privilege drift")
			}
			if err := manager.ApplyGrants(ctx, c); err != nil {
				t.Fatal(err)
			}
			if err := manager.VerifyRoleBoundaries(ctx, c); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, probe := range []struct{ name, apply, undo string }{
		{"security_definer", `ALTER FUNCTION public.digest(text,text) SECURITY DEFINER`, `ALTER FUNCTION public.digest(text,text) SECURITY INVOKER`},
		{"not_extension_member", `ALTER EXTENSION pgcrypto DROP FUNCTION public.digest(text,text)`, `ALTER EXTENSION pgcrypto ADD FUNCTION public.digest(text,text)`},
		{"wrong_owner", `ALTER FUNCTION public.digest(text,text) OWNER TO xinsight_workspace`, `ALTER FUNCTION public.digest(text,text) OWNER TO xinsight_admin`},
	} {
		t.Run(probe.name, func(t *testing.T) {
			if _, err := admin.Exec(probe.apply); err != nil {
				t.Fatal(err)
			}
			for _, verify := range []func() error{func() error { return manager.VerifyRoleBoundaries(ctx, c) }, func() error { return manager.VerifyUpgradeSourceRoleBoundaries(ctx, c, "0.2.0-dev") }, func() error { return manager.ApplyGrants(ctx, c) }} {
				if verify() == nil {
					t.Fatal("accepted abnormal digest identity")
				}
			}
			if _, err := admin.Exec(probe.undo); err != nil {
				t.Fatal(err)
			}
			if err := manager.ApplyGrants(ctx, c); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Actual restricted connections, not only grants introspection.
	for _, role := range []struct{ name, dsn string }{{"reader", c.ReaderDSN}, {"workspace", c.WorkspaceDSN}, {"importer", c.ImporterDSN}} {
		connection, err := sql.Open("pgx", role.dsn)
		if err != nil {
			t.Fatal(err)
		}
		var digest string
		err = connection.QueryRow(`SELECT encode(public.digest('synthetic','sha256'),'hex')`).Scan(&digest)
		if (role.name == "reader") != (err == nil) {
			t.Fatalf("%s text execution boundary", role.name)
		}
		if err = connection.QueryRow(`SELECT encode(public.digest('synthetic'::bytea,'sha256'),'hex')`).Scan(&digest); err == nil {
			t.Fatalf("%s bytea execution unexpectedly allowed", role.name)
		}
		connection.Close()
	}
	var adminDigest string
	if err := admin.QueryRow(`SELECT encode(public.digest('synthetic'::bytea,'sha256'),'hex')`).Scan(&adminDigest); err != nil {
		t.Fatal("admin restore digest unavailable")
	}
	t.Log("Local schema25→26 source missing reader permission admitted; target requires exact text digest; formal ApplyGrants converges idempotently; runtime execution and source/target privilege and identity negatives passed; admin bytea retained")
}

func TestConnectorDigestInheritedGrantOption(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	helper := fmt.Sprintf("xi_digest_helper_%d", time.Now().UnixNano())
	recipient := fmt.Sprintf("xi_digest_recipient_%d", time.Now().UnixNano())
	var created []string
	defer func() {
		// Only these freshly created roles are involved. Revoke the upstream
		// grant first so its delegated ACL dependencies disappear before DROP.
		if len(created) != 0 {
			if _, err := admin.Exec(`REVOKE EXECUTE ON FUNCTION public.digest(text,text) FROM ` + pgx.Identifier{helper}.Sanitize() + ` CASCADE`); err != nil {
				t.Error(err)
			}
			if _, err := admin.Exec(`REVOKE ` + pgx.Identifier{helper}.Sanitize() + ` FROM xinsight_reader`); err != nil {
				t.Error(err)
			}
		}
		for i := len(created) - 1; i >= 0; i-- {
			if _, err := admin.Exec(`DROP ROLE ` + pgx.Identifier{created[i]}.Sanitize()); err != nil {
				t.Error(err)
			}
		}
	}()
	for _, role := range []string{helper, recipient} {
		if _, err := admin.Exec(`CREATE ROLE ` + pgx.Identifier{role}.Sanitize() + ` NOLOGIN`); err != nil {
			t.Fatal(err)
		}
		created = append(created, role)
	}
	if _, err := admin.Exec(`GRANT EXECUTE ON FUNCTION public.digest(text,text) TO ` + pgx.Identifier{helper}.Sanitize() + ` WITH GRANT OPTION`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`GRANT ` + pgx.Identifier{helper}.Sanitize() + ` TO xinsight_reader WITH INHERIT FALSE, SET FALSE, ADMIN FALSE`); err != nil {
		t.Fatal(err)
	}
	check := func(delegable bool) {
		t.Helper()
		var effective bool
		if err := admin.QueryRow(`SELECT has_function_privilege('xinsight_reader','public.digest(text,text)','EXECUTE WITH GRANT OPTION')`).Scan(&effective); err != nil || effective != delegable {
			t.Fatal("effective grant-option fixture mismatch")
		}
		for name, verify := range map[string]func() error{
			"target": func() error { return manager.VerifyRoleBoundaries(ctx, c) },
			"source": func() error { return manager.VerifyUpgradeSourceRoleBoundaries(ctx, c, "0.3.0-rc.13") },
		} {
			err := verify()
			if delegable && (err == nil || !strings.Contains(err.Error(), "connector digest privilege boundary is invalid")) {
				t.Errorf("%s accepted inherited effective grant option or failed for another reason: %v", name, err)
			}
			if !delegable && err != nil {
				t.Errorf("%s rejected ordinary reader rights: %v", name, err)
			}
		}
	}
	check(false) // This membership is neither inherited, SET-able, nor administrable.
	if _, err := admin.Exec(`GRANT ` + pgx.Identifier{helper}.Sanitize() + ` TO xinsight_reader WITH INHERIT TRUE`); err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("pgx", c.ReaderDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	connection, err := reader.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, `SET default_transaction_read_only=off`); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(ctx, `GRANT EXECUTE ON FUNCTION public.digest(text,text) TO `+pgx.Identifier{recipient}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	var delegated bool
	if err := admin.QueryRow(`SELECT has_function_privilege($1,'public.digest(text,text)','EXECUTE')`, recipient).Scan(&delegated); err != nil || !delegated {
		t.Fatal("reader did not actually delegate")
	}
	t.Log("Actual synthetic reader inherited grant option and delegated EXECUTE; baseline NOINHERIT alone did not grant that capability")
	check(true)
	if err := manager.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	check(true) // Formal grants must not silently rewrite user memberships.
	var inherits bool
	if err := admin.QueryRow(`SELECT inherit_option FROM pg_auth_members WHERE roleid=(SELECT oid FROM pg_roles WHERE rolname=$1) AND member=(SELECT oid FROM pg_roles WHERE rolname='xinsight_reader')`, helper).Scan(&inherits); err != nil || !inherits {
		t.Fatal("ApplyGrants changed synthetic membership")
	}
	if _, err := admin.Exec(`REVOKE ` + pgx.Identifier{helper}.Sanitize() + ` FROM xinsight_reader`); err != nil {
		t.Fatal(err)
	}
	check(false)
	if _, err := admin.Exec(`REVOKE EXECUTE ON FUNCTION public.digest(text,text) FROM xinsight_reader`); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyUpgradeSourceRoleBoundaries(ctx, c, "0.3.0-rc.13"); err != nil {
		t.Fatal("legal missing ordinary source grant cannot reach repair")
	}
	if err := manager.VerifyRoleBoundaries(ctx, c); err == nil {
		t.Fatal("target accepted missing ordinary grant")
	}
	if err := manager.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	check(false)
}
