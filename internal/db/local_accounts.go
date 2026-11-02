package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/jackc/pgx/v5/pgconn"
)

const AccountAuthorityLock int64 = 6361703472894002482
const accountColumns = `subject_id,username,role,disabled,needs_activation,revision`

var ErrAccountAuthorityUninitialized = errors.New("local account authority is not initialized")

type LocalAccountStore struct{ db *sql.DB }

func NewLocalAccountStore(conn *sql.DB) *LocalAccountStore  { return &LocalAccountStore{db: conn} }
func (s *WorkspaceStore) LocalAccounts() *LocalAccountStore { return NewLocalAccountStore(s.db) }
func OpenLocalAccountStore(ctx context.Context, dsn string) (*LocalAccountStore, error) {
	c, e := sql.Open("pgx", dsn)
	if e != nil {
		return nil, e
	}
	if e = c.PingContext(ctx); e != nil {
		c.Close()
		return nil, e
	}
	return NewLocalAccountStore(c), nil
}
func (s *LocalAccountStore) Close() error { return s.db.Close() }
func (s *LocalAccountStore) Initialized(ctx context.Context) (bool, error) {
	var yes bool
	e := s.db.QueryRowContext(ctx, `SELECT initialized FROM xinsight_accounts.authority WHERE singleton`).Scan(&yes)
	return yes, e
}

// Only the native administrator calls this after schema migration. The marker
// and complete v1/v2/v3 set commit together. An initialized authority never reads
// or compares a subsequently changed legacy file, including on restore/resume.
func (s *LocalAccountStore) Initialize(ctx context.Context, config access.AccountConfig, legacySHA string) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, AccountAuthorityLock); e != nil {
		return e
	}
	var done bool
	if e = tx.QueryRowContext(ctx, `SELECT initialized FROM xinsight_accounts.authority WHERE singleton FOR UPDATE`).Scan(&done); e != nil {
		return e
	}
	if done {
		return tx.Commit()
	}
	var n int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM xinsight_accounts.accounts`).Scan(&n); e != nil {
		return e
	}
	if n != 0 {
		return fmt.Errorf("uninitialized account authority contains partial state")
	}
	for _, a := range config.Accounts {
		if _, e = tx.ExecContext(ctx, `INSERT INTO xinsight_accounts.accounts(subject_id,username,role,disabled,password_hash) VALUES($1,$2,$3,$4,$5)`, a.SubjectID, a.Username, a.Role, a.Disabled, a.PasswordHash); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE xinsight_accounts.authority SET initialized=true,legacy_sha256=$1,initialized_at=clock_timestamp() WHERE singleton`, legacySHA); e != nil {
		return e
	}
	if e = accountAudit(ctx, tx, "root", "root", "access.account_authority.initialize", "authority", map[string]any{"account_count": len(config.Accounts)}); e != nil {
		return e
	}
	return tx.Commit()
}
func scanManaged(row interface{ Scan(...any) error }) (a access.ManagedAccount, e error) {
	e = row.Scan(&a.SubjectID, &a.Username, &a.Role, &a.Disabled, &a.NeedsActivation, &a.Revision)
	return
}
func (s *LocalAccountStore) ready(ctx context.Context) error {
	done, e := s.Initialized(ctx)
	if e != nil {
		return access.ErrAccountUnavailable
	}
	if !done {
		return ErrAccountAuthorityUninitialized
	}
	return nil
}
func (s *LocalAccountStore) ManagedCredential(ctx context.Context, username string) (access.ManagedCredential, error) {
	var a access.ManagedCredential
	if e := s.ready(ctx); e != nil {
		return a, access.ErrAccountUnavailable
	}
	e := s.db.QueryRowContext(ctx, `SELECT `+accountColumns+`,COALESCE(password_hash,'') FROM xinsight_accounts.accounts WHERE username=$1`, username).Scan(&a.SubjectID, &a.Username, &a.Role, &a.Disabled, &a.NeedsActivation, &a.Revision, &a.PasswordHash)
	if errors.Is(e, sql.ErrNoRows) {
		return a, access.ErrInvalidCredentials
	}
	if e != nil {
		return a, access.ErrAccountUnavailable
	}
	return a, nil
}
func (s *LocalAccountStore) CurrentAccount(ctx context.Context, subject string) (access.ManagedAccount, error) {
	if e := s.ready(ctx); e != nil {
		return access.ManagedAccount{}, access.ErrAccountUnavailable
	}
	a, e := scanManaged(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM xinsight_accounts.accounts WHERE subject_id=$1 AND EXISTS(SELECT 1 FROM xinsight_accounts.authority WHERE singleton AND initialized)`, subject))
	if errors.Is(e, sql.ErrNoRows) {
		return a, access.ErrUnauthenticated
	}
	if e != nil {
		return a, access.ErrAccountUnavailable
	}
	return a, nil
}
func (s *LocalAccountStore) ListAccounts(ctx context.Context) ([]access.ManagedAccount, error) {
	if e := s.ready(ctx); e != nil {
		return nil, access.ErrAccountUnavailable
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM xinsight_accounts.accounts ORDER BY username LIMIT 101`)
	if e != nil {
		return nil, access.ErrAccountUnavailable
	}
	defer rows.Close()
	out := []access.ManagedAccount{}
	for rows.Next() {
		a, e := scanManaged(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	if len(out) > access.MaxAccounts {
		return nil, access.ErrAccountUnavailable
	}
	return out, rows.Err()
}
func accountAudit(ctx context.Context, tx *sql.Tx, kind, actor, action, target string, details any) error {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return e
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	raw, e := json.Marshal(details)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO access_audit_records(id,occurred_at,actor_kind,actor_ref,action,resource_kind,resource_ref,result,correlation_id,details) VALUES($1,clock_timestamp(),$2,$3,$4,'local_account',$5,'accepted',$6,$7::jsonb)`, "xaa_"+id, kind, actor, action, target, "xa_"+id, string(raw))
	return e
}
func accountWriteError(e error) error {
	var pg *pgconn.PgError
	if errors.As(e, &pg) {
		switch pg.Code {
		case "23505", "40001":
			return access.ErrAccountConflict
		case "22023", "23514":
			return access.ErrAccountInvalid
		case "42501":
			return access.ErrForbidden
		}
	}
	return access.ErrAccountUnavailable
}
func beginAccountWrite(ctx context.Context, conn *sql.DB) (*sql.Tx, error) {
	tx, e := conn.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, AccountAuthorityLock); e != nil {
		tx.Rollback()
		return nil, e
	}
	return tx, nil
}
func verifyAccountActor(ctx context.Context, tx *sql.Tx, actor access.Principal) error {
	a, e := scanManaged(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM xinsight_accounts.accounts WHERE subject_id=$1 AND EXISTS(SELECT 1 FROM xinsight_accounts.authority WHERE singleton AND initialized)`, actor.SubjectID))
	if e != nil || a.Role != access.RolePlatformAdmin || a.Disabled || a.NeedsActivation || a.Revision != actor.AuthRevision {
		return access.ErrForbidden
	}
	return nil
}
func (s *LocalAccountStore) ChangeAccount(ctx context.Context, actor access.Principal, c access.AccountChange, digest string) (access.AccountChangeResult, error) {
	var out access.AccountChangeResult
	tx, e := beginAccountWrite(ctx, s.db)
	if e != nil {
		return out, access.ErrAccountUnavailable
	}
	defer tx.Rollback()
	if e = verifyAccountActor(ctx, tx, actor); e != nil {
		return out, e
	}
	var a access.ManagedAccount
	if c.Action == "invite" {
		if c.ExpectedRevision != 0 || c.Username != access.NormalizeUsername(c.Username) || (c.Role != access.RoleScopedReader && c.Role != access.RoleScopedReviewer) || len(digest) != 64 {
			return out, access.ErrAccountInvalid
		}
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM xinsight_accounts.accounts`).Scan(&n); e != nil {
			return out, access.ErrAccountUnavailable
		}
		if n >= access.MaxAccounts {
			return out, access.ErrAccountInvalid
		}
		a, e = scanManaged(tx.QueryRowContext(ctx, `INSERT INTO xinsight_accounts.accounts(subject_id,username,role,needs_activation) VALUES($1,$2,$3,true) RETURNING `+accountColumns, c.SubjectID, c.Username, c.Role))
	} else {
		a, e = scanManaged(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM xinsight_accounts.accounts WHERE subject_id=$1 FOR UPDATE`, c.SubjectID))
		if errors.Is(e, sql.ErrNoRows) {
			return out, access.ErrAccountInvalid
		}
		if e != nil {
			return out, access.ErrAccountUnavailable
		}
		// The normal page cannot demote/disable/recover an administrator. Emergency
		// administrator recovery is an explicit native root operation.
		if a.Role != access.RoleScopedReader && a.Role != access.RoleScopedReviewer {
			return out, access.ErrForbidden
		}
		if a.Revision != c.ExpectedRevision {
			return out, access.ErrAccountConflict
		}
		switch c.Action {
		case "recover":
			if a.Disabled || len(digest) != 64 {
				return out, access.ErrAccountInvalid
			}
			a.NeedsActivation = true
		case "disable":
			a.Disabled = true
		case "enable":
			if !a.Disabled {
				return out, access.ErrAccountInvalid
			}
			a.Disabled = false
		default:
			return out, access.ErrAccountInvalid
		}
		a, e = scanManaged(tx.QueryRowContext(ctx, `UPDATE xinsight_accounts.accounts SET disabled=$2,needs_activation=$3,revision=revision+1,updated_at=clock_timestamp() WHERE subject_id=$1 RETURNING `+accountColumns, a.SubjectID, a.Disabled, a.NeedsActivation))
	}
	if e != nil {
		return out, accountWriteError(e)
	}
	if c.Action == "invite" || c.Action == "recover" {
		purpose := "recovery"
		if c.Action == "invite" {
			purpose = "activation"
		}
		var expires time.Time
		e = tx.QueryRowContext(ctx, `INSERT INTO xinsight_accounts.password_tickets(subject_id,verifier_sha256,purpose,account_revision,expires_at,created_by,ticket_generation) VALUES($1,$2,$3,$4,clock_timestamp()+interval '15 minutes',$5,(SELECT ticket_generation FROM xinsight_accounts.authority WHERE singleton)) ON CONFLICT(subject_id) DO UPDATE SET verifier_sha256=EXCLUDED.verifier_sha256,ticket_generation=EXCLUDED.ticket_generation,purpose=EXCLUDED.purpose,account_revision=EXCLUDED.account_revision,expires_at=EXCLUDED.expires_at,consumed_at=NULL,created_by=EXCLUDED.created_by,created_at=clock_timestamp() RETURNING expires_at`, a.SubjectID, digest, purpose, a.Revision, actor.SubjectID).Scan(&expires)
		if e != nil {
			return out, accountWriteError(e)
		}
		out.ExpiresAt = &expires
	} else {
		if _, e = tx.ExecContext(ctx, `UPDATE xinsight_accounts.password_tickets SET consumed_at=clock_timestamp() WHERE subject_id=$1 AND consumed_at IS NULL`, a.SubjectID); e != nil {
			return out, access.ErrAccountUnavailable
		}
	}
	if e = accountAudit(ctx, tx, "human", actor.SubjectID, "access.account."+c.Action, a.SubjectID, map[string]any{"revision": a.Revision, "disabled": a.Disabled, "needs_activation": a.NeedsActivation, "role": a.Role}); e != nil {
		return out, access.ErrAccountUnavailable
	}
	if e = tx.Commit(); e != nil {
		return out, access.ErrAccountUnavailable
	}
	out.Account = a
	return out, nil
}
func (s *LocalAccountStore) CompletePassword(ctx context.Context, digest, encoded string) (access.ManagedAccount, error) {
	var out access.ManagedAccount
	if _, e := access.ParsePasswordHash(encoded); e != nil || len(digest) != 64 {
		return out, access.ErrAccountInvalid
	}
	tx, e := beginAccountWrite(ctx, s.db)
	if e != nil {
		return out, access.ErrAccountUnavailable
	}
	defer tx.Rollback()
	var subject string
	var rev int64
	e = tx.QueryRowContext(ctx, `SELECT subject_id,account_revision FROM xinsight_accounts.password_tickets WHERE verifier_sha256=$1 AND consumed_at IS NULL AND expires_at>clock_timestamp() AND ticket_generation=(SELECT ticket_generation FROM xinsight_accounts.authority WHERE singleton AND initialized) FOR UPDATE`, digest).Scan(&subject, &rev)
	if errors.Is(e, sql.ErrNoRows) {
		return out, access.ErrAccountInvalid
	}
	if e != nil {
		return out, access.ErrAccountUnavailable
	}
	out, e = scanManaged(tx.QueryRowContext(ctx, `UPDATE xinsight_accounts.accounts SET password_hash=$3,needs_activation=false,revision=revision+1,updated_at=clock_timestamp() WHERE subject_id=$1 AND revision=$2 AND NOT disabled AND needs_activation RETURNING `+accountColumns, subject, rev, encoded))
	if errors.Is(e, sql.ErrNoRows) {
		return out, access.ErrAccountConflict
	}
	if e != nil {
		return out, access.ErrAccountUnavailable
	}
	if _, e = tx.ExecContext(ctx, `UPDATE xinsight_accounts.password_tickets SET consumed_at=clock_timestamp() WHERE subject_id=$1`, subject); e != nil {
		return out, access.ErrAccountUnavailable
	}
	if e = accountAudit(ctx, tx, "human", subject, "access.account.password_set", subject, map[string]any{"revision": out.Revision}); e != nil {
		return out, access.ErrAccountUnavailable
	}
	if e = tx.Commit(); e != nil {
		return out, access.ErrAccountUnavailable
	}
	return out, nil
}

// A bounded publication shares this lock with account mutation, fencing a
// disabled/recovered user at the existing read/commit publication boundary.
func (s *LocalAccountStore) WithAccount(ctx context.Context, p access.Principal, fn func() error) error {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return access.ErrAccountUnavailable
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, AccountAuthorityLock); e != nil {
		return access.ErrAccountUnavailable
	}
	a, e := scanManaged(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM xinsight_accounts.accounts WHERE subject_id=$1 AND EXISTS(SELECT 1 FROM xinsight_accounts.authority WHERE singleton AND initialized)`, p.SubjectID))
	if e != nil || a.Disabled || a.NeedsActivation || a.Revision != p.AuthRevision || a.Role != p.Role {
		return access.ErrUnauthenticated
	}
	if e = fn(); e != nil {
		return e
	}
	return tx.Commit()
}

func (s *LocalAccountStore) ProjectReadOptions(ctx context.Context, actor access.Principal) (any, error) {
	a, e := s.CurrentAccount(ctx, actor.SubjectID)
	if e != nil || a.Role != access.RolePlatformAdmin || a.Disabled || a.NeedsActivation || a.Revision != actor.AuthRevision {
		return nil, access.ErrForbidden
	}
	// Only project identifiers, registered source names and existing grant state.
	var raw []byte
	e = s.db.QueryRowContext(ctx, `SELECT jsonb_build_object('sources',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',id,'name',display_name) ORDER BY id) FROM source_systems),'[]'::jsonb),'projects',COALESCE((SELECT jsonb_agg(jsonb_build_object('source_system_id',source_system_id,'source_project_ref',project) ORDER BY source_system_id,project) FROM (SELECT DISTINCT source_system_id,'project-'||project_id AS project FROM source_fact_streams UNION SELECT DISTINCT source_system_id,source_project_ref FROM workbench_access_grants WHERE source_project_ref IS NOT NULL UNION SELECT DISTINCT source_system_id,source_project_ref FROM workbench_economic_facts WHERE source_project_ref IS NOT NULL UNION SELECT DISTINCT source_system_id,'project-'||project_id FROM source_content_collections) p),'[]'::jsonb),'grants',COALESCE((SELECT jsonb_agg(to_jsonb(g) ORDER BY subject_id,id) FROM workbench_access_grants g),'[]'::jsonb))`).Scan(&raw)
	if e != nil {
		return nil, access.ErrAccountUnavailable
	}
	var out any
	e = json.Unmarshal(raw, &out)
	return out, e
}
func (s *LocalAccountStore) ChangeProjectRead(ctx context.Context, actor access.Principal, c access.ProjectReadChange) (any, error) {
	if strings.TrimSpace(c.Project) != c.Project || len(c.Project) > 128 {
		return nil, access.ErrAccountInvalid
	}
	var raw []byte
	// The SQL function uses the original table -> grant-lock ordering and source/
	// project constraints. Check mode is a read-only transaction, no audit/write.
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: c.CheckOnly})
	if e != nil {
		return nil, access.ErrAccountUnavailable
	}
	defer tx.Rollback()
	e = tx.QueryRowContext(ctx, `SELECT xinsight_accounts.set_project_read_grant($1,$2,$3,$4::uuid,$5,$6::uuid,$7,$8::text[],$9,$10)`, actor.SubjectID, actor.AuthRevision, c.SubjectID, c.SourceID, c.Project, c.GrantID, c.ExpectedRevision, pgTextArray(c.Capabilities), c.Revoke, c.CheckOnly).Scan(&raw)
	if e != nil {
		return nil, accountWriteError(e)
	}
	if e = tx.Commit(); e != nil {
		return nil, accountWriteError(e)
	}
	var out any
	e = json.Unmarshal(raw, &out)
	return out, e
}
