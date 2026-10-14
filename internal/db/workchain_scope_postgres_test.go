package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workchain"
)

func TestScopedGrantPoolAndLockOrdering(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dsn := workspaceTestDSN(t, ctx, base)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	admin := openTestSQL(t, dsn)
	defer admin.Close()
	source := testUUID(t)
	const subject = "xs_0011223344556677"
	if _, err := admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name,source_version,status) VALUES($1,$2,'unirouter','scope fixture','unirouter.completed-execution.v1','active')`, source, "scope:"+source); err != nil {
		t.Fatal(err)
	}
	seed := func() {
		if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details) VALUES($1,$2,true,true) ON CONFLICT(subject_id,source_system_id,source_project_ref,cost_project_ref) DO UPDATE SET active=true`, subject, source); err != nil {
			t.Fatal(err)
		}
	}
	seed()
	for _, size := range []int{1, 2, 10} {
		t.Run(fmt.Sprintf("pool-%d", size), func(t *testing.T) {
			reader := openTestSQL(t, dsn)
			defer reader.Close()
			reader.SetMaxOpenConns(size)
			business := openTestSQL(t, dsn)
			defer business.Close()
			business.SetMaxOpenConns(size)
			s := db.NewScopedWorkchainStore(db.NewEvidenceStore(reader), db.NewWorkspaceStore(business))
			start := make(chan struct{})
			errs := make(chan error, 24)
			var group sync.WaitGroup
			for range 24 {
				group.Add(1)
				go func() {
					defer group.Done()
					<-start
					local, stop, err := s.Begin(ctx, subject, true, func(f func() error) error { return f() })
					if err != nil {
						errs <- err
						return
					}
					defer stop()
					var value int
					err = business.QueryRowContext(local, `SELECT 1`).Scan(&value)
					if err == nil && value != 1 {
						err = fmt.Errorf("business result %d", value)
					}
					errs <- err
				}()
			}
			close(start)
			group.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			if reader.Stats().InUse != 0 || business.Stats().InUse != 0 {
				t.Fatal("pool connection retained")
			}
			aliased := db.NewScopedWorkchainStore(db.NewEvidenceStore(reader), db.NewWorkspaceStore(reader))
			if _, _, err := aliased.Begin(ctx, subject, true, nil); !errors.Is(err, workchain.ErrConfiguration) {
				t.Fatalf("aliased pool: %v", err)
			}
			if reader.Stats().InUse != 0 {
				t.Fatal("aliased rejection borrowed connection")
			}
		})
	}
	reader := openTestSQL(t, dsn)
	defer reader.Close()
	reader.SetMaxOpenConns(2)
	business := openTestSQL(t, dsn)
	defer business.Close()
	business.SetMaxOpenConns(1)
	s := db.NewScopedWorkchainStore(db.NewEvidenceStore(reader), db.NewWorkspaceStore(business))

	t.Run("truncate-ahead-of-reader", func(t *testing.T) {
		seed()
		writer, err := admin.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Rollback()
		if _, err = writer.ExecContext(ctx, `LOCK TABLE workbench_access_grants IN ACCESS EXCLUSIVE MODE`); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() {
			_, finish, e := s.Begin(ctx, subject, true, nil)
			if finish != nil {
				finish()
			}
			result <- e
		}()
		waitScopeTableWait(t, ctx, admin)
		if _, err = writer.ExecContext(ctx, `TRUNCATE workbench_access_grants`); err != nil {
			t.Fatal(err)
		}
		if err = writer.Commit(); err != nil {
			t.Fatal(err)
		}
		if err = <-result; !errors.Is(err, workchain.ErrDenied) {
			t.Fatalf("post-truncate scope: %v", err)
		}
	})
	t.Run("reader-ahead-of-truncate", func(t *testing.T) {
		seed()
		_, finish, err := s.Begin(ctx, subject, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() { _, e := admin.ExecContext(ctx, `TRUNCATE workbench_access_grants`); result <- e }()
		waitScopeTableWait(t, ctx, admin)
		finish()
		if err = <-result; err != nil {
			t.Fatal(err)
		}
		if _, stop, e := s.Begin(ctx, subject, true, nil); !errors.Is(e, workchain.ErrDenied) {
			if stop != nil {
				stop()
			}
			t.Fatalf("post-revoke: %v", e)
		}
	})
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("dml-rollback-%t", rollback), func(t *testing.T) {
			seed()
			writer, err := admin.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Rollback()
			if _, err = writer.ExecContext(ctx, `UPDATE workbench_access_grants SET active=false,revision=revision+1 WHERE subject_id=$1`, subject); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, finish, e := s.Begin(ctx, subject, true, nil)
				if finish != nil {
					finish()
				}
				result <- e
			}()
			waitScopeAdvisoryWait(t, ctx, admin)
			if rollback {
				err = writer.Rollback()
			} else {
				err = writer.Commit()
			}
			if err != nil {
				t.Fatal(err)
			}
			err = <-result
			if rollback && err != nil {
				t.Fatal(err)
			}
			if !rollback && !errors.Is(err, workchain.ErrDenied) {
				t.Fatalf("stale authorization: %v", err)
			}
		})
	}
	t.Run("canceled-and-failed-business-release", func(t *testing.T) {
		seed()
		busy, err := business.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		request, cancelRequest := context.WithCancel(ctx)
		local, finish, err := s.Begin(request, subject, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		cancelRequest()
		var value int
		if err = business.QueryRowContext(local, `SELECT 1`).Scan(&value); err == nil {
			t.Fatal("canceled borrow succeeded")
		}
		finish()
		busy.Close()
		local, finish, err = s.Begin(ctx, subject, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = business.ExecContext(local, `SELECT * FROM intentionally_missing_scope_fixture_table`); err == nil {
			t.Fatal("expected business error")
		}
		finish()
		// A fresh write must acquire the exclusive advisory lock, proving no
		// canceled request leaked its grant lock or its only pool connection.
		if _, err = admin.ExecContext(ctx, `UPDATE workbench_access_grants SET revision=revision+1`); err != nil {
			t.Fatal(err)
		}
		if reader.Stats().InUse != 0 || business.Stats().InUse != 0 {
			t.Fatal("canceled connection retained")
		}
	})
}

func waitScopeTableWait(t *testing.T, ctx context.Context, admin *sql.DB) {
	t.Helper()
	waitScopeLock(t, ctx, admin, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='workbench_access_grants'::regclass AND NOT granted)`)
}
func waitScopeAdvisoryWait(t *testing.T, ctx context.Context, admin *sql.DB) {
	t.Helper()
	waitScopeLock(t, ctx, admin, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted)`)
}
func waitScopeLock(t *testing.T, ctx context.Context, admin *sql.DB, query string) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := admin.QueryRowContext(ctx, query).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}
