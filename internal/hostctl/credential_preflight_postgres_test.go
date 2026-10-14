package hostctl

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/jackc/pgx/v5"
)

func TestControlCredentialReadOnlyPreflightAcrossSchema43To45(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		if os.Getenv("XINSIGHT_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatal("PostgreSQL 16 preflight fixture is required")
		}
		t.Skip("PostgreSQL fixture not selected")
	}
	u, err := url.Parse(base)
	if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") || u.User == nil || u.User.Username() != "xinsight_admin" {
		t.Fatal("local synthetic PostgreSQL administrator required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	control, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("xi_credential_preflight_%d", time.Now().UnixNano())
	if _, err = control.ExecContext(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer control.Close()
		if _, cleanupErr := control.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	u.Path = "/" + name
	credentials := DatabaseCredentials{AdminDSN: u.String()}
	credentials.Admin, err = pgx.ParseConfig(credentials.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"reader", "workspace", "importer"} {
		roleURL := *u
		roleURL.User = url.UserPassword("xinsight_"+role, "synthetic credential preflight "+role)
		roleConfig, parseErr := pgx.ParseConfig(roleURL.String())
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		switch role {
		case "reader":
			credentials.Reader = roleConfig
		case "workspace":
			credentials.Workspace = roleConfig
		case "importer":
			credentials.Importer = roleConfig
		}
	}
	manager := PostgresManager{Runner: ExecRunner{}}
	var roleCount int
	if err := control.QueryRowContext(ctx, `SELECT count(*) FROM pg_roles WHERE rolname IN ('xinsight_reader','xinsight_workspace','xinsight_importer')`).Scan(&roleCount); err != nil {
		t.Fatal(err)
	}
	if roleCount != 3 {
		if roleCount != 0 {
			t.Fatal("incomplete synthetic restricted roles")
		}
		if err := manager.EnsureRoles(ctx, credentials); err != nil {
			t.Fatal(err)
		}
	}
	for _, version := range []int{43, 44, 45} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			if _, err := db.RunMigrations(ctx, credentials.AdminDSN, connectorDigestMigrations(t, version)); err != nil {
				t.Fatal(err)
			}
			database, err := sql.Open("pgx", credentials.AdminDSN)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			var before, after int
			if err = database.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if err = manager.VerifyControlCredentialLifecycle(ctx, credentials); err != nil {
				t.Fatal("upgrade compatibility preflight", err)
			}
			statuses, err := manager.InspectControlCredentialLifecycle(ctx, credentials, time.Now().UTC())
			if err != nil || len(statuses) != 0 {
				t.Fatal("read-only runtime inspection", statuses, err)
			}
			if err = database.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&after); err != nil || before != after || after != version {
				t.Fatal("inspection changed migration ledger", before, after, err)
			}
		})
	}
}
