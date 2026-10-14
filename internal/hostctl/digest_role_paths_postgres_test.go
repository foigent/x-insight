package hostctl

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/jackc/pgx/v5"
)

func TestConnectorDigestUsableRolePaths(t *testing.T) {
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
	var created []string
	defer func() {
		for _, role := range created {
			if _, err := admin.Exec(`REVOKE ALL ON ALL FUNCTIONS IN SCHEMA public FROM ` + role + ` CASCADE`); err != nil {
				t.Error(err)
			}
		}
		for i := len(created) - 1; i >= 0; i-- {
			if _, err := admin.Exec(`DROP ROLE ` + created[i]); err != nil {
				t.Error(err)
			}
		}
	}()
	token := time.Now().UnixNano()
	names := make([]string, 3)
	for i, label := range []string{"helper", "bridge", "recipient"} {
		names[i] = fmt.Sprintf("xi_digest_path_%s_%d", label, token)
		quoted := pgx.Identifier{names[i]}.Sanitize()
		if _, err := admin.Exec(`CREATE ROLE ` + quoted + ` NOLOGIN`); err != nil {
			t.Fatal(err)
		}
		created = append(created, quoted)
	}
	helper, bridge, recipient := created[0], created[1], created[2]
	const reader = "xinsight_reader"
	edge := func(role, member, options string) string {
		return "GRANT " + role + " TO " + member + " WITH " + options
	}
	const (
		unreachable = "INHERIT FALSE, SET FALSE, ADMIN FALSE"
		inheritOnly = "INHERIT TRUE, SET FALSE, ADMIN FALSE"
		setOnly     = "INHERIT FALSE, SET TRUE, ADMIN FALSE"
		adminOnly   = "INHERIT FALSE, SET FALSE, ADMIN TRUE"
	)
	grantFunction := `GRANT EXECUTE ON FUNCTION public.digest(text,text) TO ` + recipient
	grantRole := `GRANT ` + helper + ` TO ` + recipient + ` WITH ` + inheritOnly
	reset := func(t *testing.T) {
		t.Helper()
		_, err := admin.Exec(`REVOKE ` + helper + ` FROM xinsight_reader,xinsight_workspace,xinsight_importer,` + bridge + `,` + recipient + ` CASCADE;
REVOKE ` + bridge + ` FROM xinsight_reader CASCADE;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA public FROM ` + strings.Join(created, ",") + ` CASCADE`)
		if err != nil {
			t.Fatal(err)
		}
	}
	memberships := func(t *testing.T) string {
		t.Helper()
		var value string
		if err := admin.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY m.oid),'[]'::jsonb)::text
FROM pg_auth_members m WHERE m.roleid IN (SELECT oid FROM pg_roles WHERE rolname IN ($1,$2))`, names[0], names[1]).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, tc := range []struct {
		name, runtime, function, action string
		edges                           []string
		grantOption, transfer, rejected bool
	}{
		{"unreachable", reader, connectorDigestFunction, `SET ROLE ` + helper + `; ` + grantFunction,
			[]string{edge(helper, reader, unreachable)}, true, false, false},
		{"inherit", reader, connectorDigestFunction, grantFunction,
			[]string{edge(helper, reader, inheritOnly)}, true, true, true},
		{"set", reader, connectorDigestFunction, `SET ROLE ` + helper + `; ` + grantFunction,
			[]string{edge(helper, reader, setOnly)}, true, true, true},
		{"transitive_set", reader, connectorDigestFunction, `SET ROLE ` + helper + `; ` + grantFunction,
			[]string{edge(helper, bridge, setOnly), edge(bridge, reader, setOnly)}, true, true, true},
		{"set_then_inherit", reader, connectorDigestFunction, `SET ROLE ` + bridge + `; ` + grantFunction,
			[]string{edge(helper, bridge, inheritOnly), edge(bridge, reader, setOnly)}, true, true, true},
		{"inherit_does_not_enable_set", reader, connectorDigestFunction, `SET ROLE ` + helper + `; ` + grantFunction,
			[]string{edge(helper, bridge, setOnly), edge(bridge, reader, inheritOnly)}, true, false, false},
		{"admin_ordinary_execute", reader, connectorDigestFunction, grantRole,
			[]string{edge(helper, reader, adminOnly)}, false, true, true},
		{"set_then_admin", reader, connectorDigestFunction, `SET ROLE ` + bridge + `; ` + grantRole,
			[]string{edge(helper, bridge, adminOnly), edge(bridge, reader, setOnly)}, false, true, true},
		{"inherited_admin", reader, connectorDigestFunction, grantRole,
			[]string{edge(helper, bridge, adminOnly), edge(bridge, reader, inheritOnly)}, false, true, true},
		{"inherited_admin_explicit_grantor", reader, connectorDigestFunction, grantRole + ` GRANTED BY ` + bridge,
			[]string{edge(helper, bridge, adminOnly), edge(bridge, reader, inheritOnly)}, false, true, true},
		{"unreachable_admin", reader, connectorDigestFunction, grantRole + ` GRANTED BY ` + bridge,
			[]string{edge(helper, bridge, adminOnly), edge(bridge, reader, unreachable)}, false, false, false},
		{"benign_reader_set", reader, connectorDigestFunction, `SET ROLE ` + helper + `; ` + grantFunction,
			[]string{edge(helper, reader, setOnly)}, false, false, false},
		{"reader_bytea_via_set", reader, "public.digest(bytea,text)", `SET ROLE ` + helper + `; SELECT public.digest('synthetic'::bytea,'sha256')`,
			[]string{edge(helper, reader, setOnly)}, false, false, true},
		{"workspace_text_via_set", "xinsight_workspace", connectorDigestFunction, `SET ROLE ` + helper + `; SELECT public.digest('synthetic'::text,'sha256')`,
			[]string{edge(helper, "xinsight_workspace", setOnly)}, false, false, true},
		{"importer_text_via_set", "xinsight_importer", connectorDigestFunction, `SET ROLE ` + helper + `; SELECT public.digest('synthetic'::text,'sha256')`,
			[]string{edge(helper, "xinsight_importer", setOnly)}, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reset(t)
			defer reset(t)
			grant := `GRANT EXECUTE ON FUNCTION ` + tc.function + ` TO ` + helper
			if tc.grantOption {
				grant += ` WITH GRANT OPTION`
			}
			if _, err := admin.Exec(strings.Join(append([]string{grant}, tc.edges...), ";")); err != nil {
				t.Fatal(err)
			}
			check := func() {
				t.Helper()
				for name, verify := range map[string]func() error{
					"source": func() error { return manager.VerifyUpgradeSourceRoleBoundaries(ctx, c, "0.3.0-rc.13") },
					"target": func() error { return manager.VerifyRoleBoundaries(ctx, c) },
				} {
					err := verify()
					if tc.rejected && (err == nil || !strings.Contains(err.Error(), "connector digest privilege boundary is invalid")) {
						t.Errorf("%s did not reject usable privilege drift at digest boundary: %v", name, err)
					}
					if !tc.rejected && err != nil {
						t.Errorf("%s rejected ordinary or unreachable rights: %v", name, err)
					}
				}
			}
			check()
			dsn := c.ReaderDSN
			if tc.runtime == "xinsight_workspace" {
				dsn = c.WorkspaceDSN
			} else if tc.runtime == "xinsight_importer" {
				dsn = c.ImporterDSN
			}
			login, err := sql.Open("pgx", dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer login.Close()
			conn, err := login.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if _, err := conn.ExecContext(ctx, `SET default_transaction_read_only=off`); err != nil {
				t.Fatal(err)
			}
			_, actionErr := conn.ExecContext(ctx, tc.action)
			if (tc.transfer || strings.Contains(tc.action, "; SELECT")) && actionErr != nil {
				t.Fatalf("actual runtime path did not execute: %v", actionErr)
			}
			var transferred bool
			if err := admin.QueryRow(`SELECT has_function_privilege($1,$2,'EXECUTE')`, names[2], tc.function).Scan(&transferred); err != nil || transferred != tc.transfer {
				t.Fatalf("actual transfer=%v, expected=%v: %v", transferred, tc.transfer, err)
			}
			before := memberships(t)
			if err := manager.ApplyGrants(ctx, c); err != nil {
				t.Fatal(err)
			}
			if after := memberships(t); before != after {
				t.Fatal("ApplyGrants silently changed role memberships")
			}
			check()
			t.Logf("runtime=%s actual_transfer=%v source_and_target_reject=%v; memberships unchanged by ApplyGrants", tc.runtime, transferred, tc.rejected)
		})
	}
}
