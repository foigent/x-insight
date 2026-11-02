package db_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/db"
)

const accountOwner = "xs_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const accountReader = "xs_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const accountReviewer = "xs_cccccccccccccccccccccccccccccccc"
const accountPassword = "synthetic account fixture password"

type accountFixture struct {
	dsn      string
	ctx      context.Context
	sql      *sql.DB
	store    *db.LocalAccountStore
	provider *access.DurableProvider
	service  *access.Service
	owner    access.Principal
	config   access.AccountConfig
}

func newAccountFixture(t *testing.T, empty ...bool) *accountFixture {
	t.Helper()
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("real PostgreSQL required")
	}
	ctx := t.Context()
	dsn := workspaceTestDSN(t, ctx, base)
	conn := openTestSQL(t, dsn)
	t.Cleanup(func() { conn.Close() })
	for _, role := range []string{"xinsight_reader", "xinsight_workspace", "xinsight_importer"} {
		if _, e := conn.Exec(`DO $$ BEGIN IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='` + role + `') THEN CREATE ROLE ` + role + `; END IF; END $$`); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := db.RunMigrations(ctx, dsn, "../../migrations"); e != nil {
		t.Fatal(e)
	}
	encoded, e := access.DerivePasswordHash([]byte(accountPassword))
	if e != nil {
		t.Fatal(e)
	}
	cfg := access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{{SubjectID: accountOwner, Username: "owner", Role: access.RolePlatformAdmin, PasswordHash: encoded}, {SubjectID: accountReader, Username: "reader", Role: access.RoleScopedReader, PasswordHash: encoded}, {SubjectID: accountReviewer, Username: "reviewer", Role: access.RoleScopedReviewer, PasswordHash: encoded}}}
	if len(empty) > 0 && empty[0] {
		cfg.Accounts = nil
	}
	store := db.NewLocalAccountStore(conn)
	if e = store.Initialize(ctx, cfg, "synthetic-v3-predecessor"); e != nil {
		t.Fatal(e)
	}
	p, e := access.NewDurableProvider(store)
	if e != nil {
		t.Fatal(e)
	}
	svc, e := access.NewService(p, nil, access.ServiceOptions{})
	if e != nil {
		t.Fatal(e)
	}
	return &accountFixture{dsn: dsn, ctx: ctx, sql: conn, store: store, provider: p, service: svc, owner: access.Principal{SubjectID: accountOwner, Role: access.RolePlatformAdmin, AuthRevision: 1}, config: cfg}
}
func TestLocalAccountV3MigrationAndSingleAuthorityPostgres(t *testing.T) {
	f := newAccountFixture(t)
	a, e := f.store.ListAccounts(f.ctx)
	if e != nil || len(a) != 3 {
		t.Fatalf("account directory %d %v", len(a), e)
	}
	// A stale/broken legacy file must not overwrite a committed authority.
	stale := f.config
	stale.Accounts = nil
	if e = f.store.Initialize(f.ctx, stale, "wrong-old-file"); e != nil {
		t.Fatal(e)
	}
	for _, old := range f.config.Accounts {
		got, e := f.store.ManagedCredential(f.ctx, old.Username)
		if e != nil || got.SubjectID != old.SubjectID || got.Role != old.Role || got.PasswordHash != old.PasswordHash {
			t.Fatal("stable v3 identity/credential changed")
		}
	}
	p, e := access.NewDurableProvider(db.NewLocalAccountStore(f.sql))
	if e != nil {
		t.Fatal(e)
	}
	if a, ok := p.Authenticate("reader", []byte(accountPassword)); !ok || a.SubjectID != accountReader {
		t.Fatal("restarted provider lost durable account")
	}
	for _, role := range []string{"xinsight_reader", "xinsight_importer"} {
		tx, e := f.sql.BeginTx(f.ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(`SET LOCAL ROLE ` + role); e != nil {
			t.Fatal(e)
		}
		var n int
		e = tx.QueryRow(`SELECT count(*) FROM xinsight_accounts.accounts`).Scan(&n)
		tx.Rollback()
		if e == nil {
			t.Fatalf("%s accessed account credentials", role)
		}
	}
}
func TestLocalAccountRecoveryReplayExpiryDisableAndOtherSessionsPostgres(t *testing.T) {
	f := newAccountFixture(t)
	ownerToken, _, e := f.service.Login("owner", []byte(accountPassword), "")
	if e != nil {
		t.Fatal(e)
	}
	readerToken, _, e := f.service.Login("reader", []byte(accountPassword), "")
	if e != nil {
		t.Fatal(e)
	}
	reviewerToken, _, e := f.service.Login("reviewer", []byte(accountPassword), "")
	if e != nil {
		t.Fatal(e)
	}
	result, e := f.provider.Change(f.ctx, f.owner, access.AccountChange{Action: "recover", SubjectID: accountReader, ExpectedRevision: 1})
	if e != nil || result.Token == "" {
		t.Fatalf("recover %v", e)
	}
	if _, e = f.service.Authenticate(readerToken); !errors.Is(e, access.ErrUnauthenticated) {
		t.Fatal("target old session remains")
	}
	if _, e = f.service.Authenticate(ownerToken); e != nil {
		t.Fatal("owner interrupted")
	}
	if _, e = f.service.Authenticate(reviewerToken); e != nil {
		t.Fatal("other reader interrupted")
	}
	if _, ok := f.provider.Authenticate("reader", []byte(accountPassword)); ok {
		t.Fatal("old password remains active during recovery")
	}
	const replacement = "synthetic replacement account password"
	changed, e := f.provider.SetPassword(f.ctx, result.Token, []byte(replacement), []byte(replacement))
	if e != nil || changed.SubjectID != accountReader {
		t.Fatal(e)
	}
	if _, e = f.provider.SetPassword(f.ctx, result.Token, []byte(replacement), []byte(replacement)); !errors.Is(e, access.ErrAccountInvalid) {
		t.Fatal("ticket replay accepted")
	}
	token, _, e := f.service.Login("reader", []byte(replacement), "")
	if e != nil {
		t.Fatal(e)
	}
	disabled, e := f.provider.Change(f.ctx, f.owner, access.AccountChange{Action: "disable", SubjectID: accountReader, ExpectedRevision: changed.Revision})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.Authenticate(token); !errors.Is(e, access.ErrUnauthenticated) {
		t.Fatal("disabled session accepted")
	}
	if _, ok := f.provider.Authenticate("reader", []byte(replacement)); ok {
		t.Fatal("disabled new login accepted")
	}
	enabled, e := f.provider.Change(f.ctx, f.owner, access.AccountChange{Action: "enable", SubjectID: accountReader, ExpectedRevision: disabled.Account.Revision})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.Authenticate(token); !errors.Is(e, access.ErrUnauthenticated) {
		t.Fatal("enable resurrected old session")
	}
	recovery, e := f.provider.Change(f.ctx, f.owner, access.AccountChange{Action: "recover", SubjectID: accountReader, ExpectedRevision: enabled.Account.Revision})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.sql.Exec(`UPDATE xinsight_accounts.password_tickets SET expires_at=clock_timestamp()-interval '1 second' WHERE subject_id=$1`, accountReader); e != nil {
		t.Fatal(e)
	}
	if _, e = f.provider.SetPassword(f.ctx, recovery.Token, []byte(replacement), []byte(replacement)); !errors.Is(e, access.ErrAccountInvalid) {
		t.Fatal("expired ticket accepted")
	}
	if _, e = f.service.Authenticate(ownerToken); e != nil {
		t.Fatal("owner lost session after ordinary account lifecycle")
	}
}
func TestLocalAccountAuditRollbackCASAndConcurrentCompletionPostgres(t *testing.T) {
	f := newAccountFixture(t)
	if _, e := f.sql.Exec(`CREATE FUNCTION reject_account_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit failure'; END $$; CREATE TRIGGER reject_account_audit BEFORE INSERT ON access_audit_records FOR EACH ROW EXECUTE FUNCTION reject_account_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.provider.Change(f.ctx, f.owner, access.AccountChange{Action: "recover", SubjectID: accountReader, ExpectedRevision: 1}); e == nil {
		t.Fatal("audit failure accepted")
	}
	a, e := f.store.CurrentAccount(f.ctx, accountReader)
	if e != nil || a.Revision != 1 || a.NeedsActivation {
		t.Fatal("failed transaction partially mutated account")
	}
	if _, e = f.sql.Exec(`DROP TRIGGER reject_account_audit ON access_audit_records; DROP FUNCTION reject_account_audit()`); e != nil {
		t.Fatal(e)
	}
	r, e := f.provider.Change(f.ctx, f.owner, access.AccountChange{Action: "recover", SubjectID: accountReader, ExpectedRevision: 1})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.provider.Change(f.ctx, f.owner, access.AccountChange{Action: "disable", SubjectID: accountReader, ExpectedRevision: 1}); !errors.Is(e, access.ErrAccountConflict) {
		t.Fatal("stale CAS accepted")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := f.provider.SetPassword(f.ctx, r.Token, []byte(accountPassword), []byte(accountPassword))
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("one-use completion successes %d", success)
	}
	if _, e = f.provider.Change(f.ctx, access.Principal{SubjectID: accountReviewer, Role: access.RoleScopedReviewer, AuthRevision: 1}, access.AccountChange{Action: "disable", SubjectID: accountReader, ExpectedRevision: 3}); !errors.Is(e, access.ErrForbidden) {
		t.Fatal("reader escalated management")
	}
	if _, e = f.provider.Change(f.ctx, f.owner, access.AccountChange{Action: "disable", SubjectID: accountOwner, ExpectedRevision: 1}); !errors.Is(e, access.ErrForbidden) {
		t.Fatal("normal page disabled last owner")
	}
}
func TestLocalAccountPublicationFenceAndUnavailableAuthorityPostgres(t *testing.T) {
	f := newAccountFixture(t)
	token, auth, e := f.service.Login("reader", []byte(accountPassword), "")
	if e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- f.service.WithCurrentAuthentication(auth, func() error { close(entered); <-release; return nil })
	}()
	<-entered
	ctx, cancel := context.WithTimeout(f.ctx, 120*time.Millisecond)
	defer cancel()
	_, e = f.provider.Change(ctx, f.owner, access.AccountChange{Action: "disable", SubjectID: accountReader, ExpectedRevision: 1})
	if e == nil {
		t.Fatal("disable crossed active publication fence")
	}
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if _, e = f.provider.Change(f.ctx, f.owner, access.AccountChange{Action: "disable", SubjectID: accountReader, ExpectedRevision: 1}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.Authenticate(token); !errors.Is(e, access.ErrUnauthenticated) {
		t.Fatal("disabled target accepted")
	}
	// No file fallback if the durable authority is unavailable/uninitialized.
	if _, e = f.sql.Exec(`UPDATE xinsight_accounts.authority SET initialized=false`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.provider.AuthenticateCurrent("owner", []byte(accountPassword)); !errors.Is(e, access.ErrAccountUnavailable) {
		t.Fatal("unavailable authority did not fail closed")
	}
}

func TestLocalAccountFreshOwnerCheckHandoffRollbackAndRateLimitPostgres(t *testing.T) {
	f := newAccountFixture(t, true)
	before, err := f.store.Inventory(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.CheckNativeAccountHandoff(f.ctx, "owner", "", 0); err != nil {
		t.Fatal(err)
	}
	after, err := f.store.Inventory(f.ctx)
	if err != nil || before != after {
		t.Fatal("bootstrap check wrote authority", err)
	}
	if _, _, err = f.service.Login("owner", []byte(accountPassword), ""); !errors.Is(err, access.ErrInvalidCredentials) {
		t.Fatal("fresh default password accepted", err)
	}
	token := "xi-ac1_" + strings.Repeat("D", 43)
	publish := func(access.ManagedAccount, time.Time) error { return errors.New("synthetic private output failure") }
	if _, err = f.store.NativeAccountHandoff(f.ctx, "owner", "", 0, access.TicketDigest(token), publish); err == nil {
		t.Fatal("failed handoff committed")
	}
	after, err = f.store.Inventory(f.ctx)
	if err != nil || before != after {
		t.Fatal("failed private handoff changed authority", err)
	}
	var subject string
	if _, err = f.store.NativeAccountHandoff(f.ctx, "owner", "", 0, access.TicketDigest(token), func(a access.ManagedAccount, _ time.Time) error { subject = a.SubjectID; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.CheckNativeAccountHandoff(f.ctx, "second.owner", "", 0); err == nil {
		t.Fatal("second bootstrap owner allowed")
	}
	if _, _, err = f.service.Login("owner", []byte(accountPassword), ""); !errors.Is(err, access.ErrInvalidCredentials) {
		t.Fatal("pending owner authenticated", err)
	}
	if _, err = f.service.SetManagedPassword(f.ctx, token, []byte(accountPassword), []byte(accountPassword)); err != nil {
		t.Fatal(err)
	}
	_, auth, err := f.service.Login("owner", []byte(accountPassword), "")
	if err != nil || auth.Session.SubjectID != subject || auth.Session.Role != access.RolePlatformAdmin {
		t.Fatal("holder activated owner unavailable", err)
	}
	if _, err = f.service.SetManagedPassword(f.ctx, token, []byte(accountPassword), []byte(accountPassword)); !errors.Is(err, access.ErrAccountInvalid) {
		t.Fatal("activation replay accepted", err)
	}
	var grants int
	if err = f.sql.QueryRow(`SELECT count(*) FROM workbench_access_grants WHERE subject_id=$1`, subject).Scan(&grants); err != nil || grants != 0 {
		t.Fatal("bootstrap implicitly granted data", err)
	}
	limited, err := access.NewService(f.provider, nil, access.ServiceOptions{PrincipalFailureLimit: 2, GlobalFailureLimit: 3})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = limited.SetManagedPassword(f.ctx, "invalid", []byte(accountPassword), []byte(accountPassword)); !errors.Is(err, access.ErrAccountInvalid) {
			t.Fatal("invalid link accepted", err)
		}
	}
	if _, err = limited.SetManagedPassword(f.ctx, "invalid", []byte(accountPassword), []byte(accountPassword)); !errors.Is(err, access.ErrRateLimited) {
		t.Fatal("per-ticket limiter not enforced", err)
	}
	if _, err = limited.SetManagedPassword(f.ctx, "xi-ac1_"+strings.Repeat("E", 43), []byte(accountPassword), []byte(accountPassword)); !errors.Is(err, access.ErrAccountInvalid) {
		t.Fatal(err)
	}
	if _, err = limited.SetManagedPassword(f.ctx, "xi-ac1_"+strings.Repeat("F", 43), []byte(accountPassword), []byte(accountPassword)); !errors.Is(err, access.ErrRateLimited) {
		t.Fatal("global limiter not enforced", err)
	}
}

func TestLocalAccountGrantWaitDoesNotBlockOtherAccountOrPublicationPostgres(t *testing.T) {
	for _, kind := range []string{"web", "native"} {
		t.Run(kind, func(t *testing.T) {
			f := newAccountFixture(t)
			ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
			defer cancel()
			const source = "11111111-1111-4111-8111-111111111111"
			if _, e := f.sql.Exec(`INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1,'account-lock-fixture','unirouter','Synthetic lock source')`, source); e != nil {
				t.Fatal(e)
			}
			if _, e := f.sql.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_summary,allow_details,allow_coverage) VALUES($1,$2,'project-2',true,true,true)`, accountReviewer, source); e != nil {
				t.Fatal(e)
			}
			evidence, e := db.OpenEvidenceStore(ctx, f.dsn)
			if e != nil {
				t.Fatal(e)
			}
			defer evidence.Close()
			writer, e := db.OpenWorkspaceStore(ctx, f.dsn)
			if e != nil {
				t.Fatal(e)
			}
			defer writer.Close()
			_, auth, e := f.service.Login("reviewer", []byte(accountPassword), "")
			if e != nil {
				t.Fatal(e)
			}
			scoped := db.NewScopedWorkchainStore(evidence, writer)
			_, finish, e := scoped.Begin(ctx, accountReviewer, true, func(fn func() error) error { return f.service.WithCurrentAuthentication(auth, fn) })
			if e != nil {
				t.Fatal(e)
			}
			defer finish()
			grantDone := make(chan error, 1)
			go func() {
				if kind == "web" {
					_, e := f.store.ChangeProjectRead(ctx, f.owner, access.ProjectReadChange{GrantID: "22222222-2222-4222-8222-222222222222", SubjectID: accountOwner, SourceID: source, Project: "project-2", Capabilities: []string{"summary", "details", "coverage"}})
					grantDone <- e
					return
				}
				native, e := db.OpenSourceFactsStore(f.dsn)
				if e != nil {
					grantDone <- e
					return
				}
				defer native.Close()
				project, cost, revision := "project-2", "*", int64(0)
				request := db.WorkbenchGrantChange{SchemaVersion: "xinsight.workbench-grant-change.v1", Action: "create", ID: "22222222-2222-4222-8222-222222222222", Subject: accountOwner, Source: source, SourceProject: &project, CostProject: &cost, ExpectedRevision: &revision, Capabilities: []string{"summary", "details", "coverage"}}
				_, e = native.ChangeManagedWorkbenchGrant(ctx, request, false)
				grantDone <- e
			}()
			waitScopeLock(t, ctx, f.sql, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_database d ON d.oid=l.database WHERE d.datname=current_database() AND l.locktype='advisory' AND NOT l.granted AND l.classid::bigint*4294967296+l.objid::bigint=6361703472894002481)`)
			mutationDone := make(chan error, 1)
			go func() {
				_, e := f.store.ChangeAccount(ctx, f.owner, access.AccountChange{Action: "disable", SubjectID: accountReader, ExpectedRevision: 1}, "")
				mutationDone <- e
			}()
			select {
			case e = <-mutationDone:
				if e != nil {
					finish()
					<-grantDone
					t.Fatal(e)
				}
			case <-time.After(2 * time.Second):
				finish()
				<-grantDone
				<-mutationDone
				t.Fatal("account mutation blocked behind grant waiting on an unrelated scoped read")
			}
			if e = f.service.WithCurrentAuthentication(auth, func() error { return nil }); e != nil {
				t.Fatal("unrelated reader publication lost authentication", e)
			}
			finish()
			if e = <-grantDone; e != nil {
				t.Fatal("grant after scoped read", e)
			}
			if _, e = f.store.ChangeAccount(ctx, f.owner, access.AccountChange{Action: "disable", SubjectID: accountReviewer, ExpectedRevision: 1}, ""); e != nil {
				t.Fatal(e)
			}
			if e = f.service.WithCurrentAuthentication(auth, func() error { return nil }); !errors.Is(e, access.ErrUnauthenticated) {
				t.Fatal("disabled reader still published", e)
			}
			if kind == "web" {
				if _, e = f.store.ChangeProjectRead(ctx, f.owner, access.ProjectReadChange{GrantID: "33333333-3333-4333-8333-333333333333", SubjectID: accountReviewer, SourceID: source, Project: "project-2", Capabilities: []string{"details", "coverage"}, CheckOnly: true}); e == nil {
					t.Fatal("web grant admitted disabled target")
				}
			} else {
				native, e := db.OpenSourceFactsStore(f.dsn)
				if e != nil {
					t.Fatal(e)
				}
				defer native.Close()
				project, cost, revision := "project-2", "*", int64(0)
				_, e = native.ChangeManagedWorkbenchGrant(ctx, db.WorkbenchGrantChange{SchemaVersion: "xinsight.workbench-grant-change.v1", Action: "create", ID: "33333333-3333-4333-8333-333333333333", Subject: accountReviewer, Source: source, SourceProject: &project, CostProject: &cost, ExpectedRevision: &revision, Capabilities: []string{"summary", "details"}}, true)
				if !errors.Is(e, access.ErrForbidden) {
					t.Fatal("native same-transaction guard admitted disabled target", e)
				}
			}
		})
	}
}
