package hostctl

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsolatedVerificationRetirementNativeSessionFencePostgres(t *testing.T) {
	ctx, credentials := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.EnsureRoles(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	control, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	control.SetMaxOpenConns(1)
	create := func(t *testing.T, prefix string) DatabaseCredentials {
		t.Helper()
		token, err := randomToken(6)
		if err != nil {
			t.Fatal(err)
		}
		name := prefix + token
		if _, err := control.ExecContext(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := control.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+pgx.Identifier{name}.Sanitize()); err != nil {
				t.Error(err)
			}
		})
		target, err := isolatedDatabaseCredentials(credentials, name)
		if err != nil {
			t.Fatal(err)
		}
		return target
	}
	exists := func(t *testing.T, name string, want bool) {
		t.Helper()
		var actual bool
		err := control.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)`, name).Scan(&actual)
		if err != nil || actual != want {
			t.Fatalf("database presence=%t want=%t error=%v", actual, want, err)
		}
	}
	for _, role := range []string{"admin", "foreign workspace"} {
		t.Run(role+" persistent session refused", func(t *testing.T) {
			target := create(t, "xinsight_restore_fence_")
			dsn := target.AdminDSN
			if role != "admin" {
				dsn = target.WorkspaceDSN
			}
			held, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close(context.Background())
			pid := held.PgConn().PID()
			err = manager.RetireCleanBaselineVerificationDatabase(ctx, credentials, target.Admin.Database)
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != "55006" {
				t.Fatalf("persistent connection did not retain native session refusal: %v", err)
			}
			var after uint32
			if err := held.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&after); err != nil || after != pid {
				t.Fatal("retirement terminated or replaced a persistent client")
			}
			exists(t, target.Admin.Database, true)
			if err := held.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if err := manager.RetireCleanBaselineVerificationDatabase(ctx, credentials, target.Admin.Database); err != nil {
				t.Fatalf("closed client prevented native verification cleanup: %v", err)
			}
			exists(t, target.Admin.Database, false)
		})
	}
	t.Run("cancel retains database", func(t *testing.T) {
		target := create(t, "xinsight_restore_cancel_")
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if err := manager.RetireCleanBaselineVerificationDatabase(cancelled, credentials, target.Admin.Database); err == nil {
			t.Fatal("cancelled retirement reported success")
		}
		exists(t, target.Admin.Database, true)
	})
	t.Run("deadline with live client retains database and session", func(t *testing.T) {
		target := create(t, "xinsight_restore_deadline_")
		held, err := pgx.Connect(ctx, target.WorkspaceDSN)
		if err != nil {
			t.Fatal(err)
		}
		defer held.Close(context.Background())
		pid := held.PgConn().PID()
		bounded, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		err = manager.RetireCleanBaselineVerificationDatabase(bounded, credentials, target.Admin.Database)
		if err == nil || bounded.Err() != context.DeadlineExceeded || !strings.HasPrefix(err.Error(), "database retirement failed:") {
			t.Fatalf("deadline did not enter the native DROP error path: %v", err)
		}
		var after uint32
		if err := held.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&after); err != nil || after != pid {
			t.Fatal("cancelled retirement terminated the live client")
		}
		exists(t, target.Admin.Database, true)
		t.Log("deadline_fence=native_nonforce_drop_entered deadline_reached=true original_client_pid_preserved=true database_retained=true")
	})
	t.Run("permission error retains database", func(t *testing.T) {
		target := create(t, "xinsight_restore_denied_")
		restricted := credentials
		restricted.Admin = credentials.Workspace.Copy()
		restricted.AdminDSN = credentials.WorkspaceDSN
		err := manager.RetireCleanBaselineVerificationDatabase(ctx, restricted, target.Admin.Database)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "42501" {
			t.Fatalf("unowned verification database did not retain permission refusal: %v", err)
		}
		exists(t, target.Admin.Database, true)
	})
	t.Run("target and credential boundaries retained", func(t *testing.T) {
		target := create(t, "xinsight_restore_boundary_")
		for _, change := range []func(*DatabaseCredentials){
			func(c *DatabaseCredentials) { c.Admin.User = "other_owner" },
			func(c *DatabaseCredentials) { c.Admin.Password += "different" },
			func(c *DatabaseCredentials) { c.Admin.Port++ },
		} {
			wrong := target
			wrong.Admin = target.Admin.Copy()
			change(&wrong)
			if err := manager.retireCleanBaselineDatabase(ctx, credentials, wrong, false); err == nil {
				t.Fatal("mismatched retirement credentials accepted")
			}
		}
		if err := manager.retireCleanBaselineDatabase(ctx, credentials, credentials, false); err == nil {
			t.Fatal("control database retirement accepted")
		}
		if err := manager.RetireCleanBaselineVerificationDatabase(ctx, credentials, "not_a_verification_database"); err == nil {
			t.Fatal("unqualified target accepted")
		}
		exists(t, target.Admin.Database, true)
		exists(t, credentials.Admin.Database, true)
	})
	t.Run("predecessor keeps immediate session precheck", func(t *testing.T) {
		active := create(t, "xinsight_clean_fence_")
		var pid, after int
		if err := control.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			t.Fatal(err)
		}
		err := manager.RetireCleanBaselinePredecessorDatabase(ctx, active, credentials)
		if err == nil || !strings.Contains(err.Error(), "target still has active sessions") {
			t.Fatalf("predecessor immediate guard changed: %v", err)
		}
		if err := control.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&after); err != nil || after != pid {
			t.Fatal("predecessor session was terminated")
		}
		exists(t, credentials.Admin.Database, true)
	})
}
