package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/access"
)

func (s *LocalAccountStore) checkNativeHandoff(ctx context.Context, tx *sql.Tx, username, subject string, revision int64) (access.ManagedAccount, error) {
	var initialized bool
	if e := tx.QueryRowContext(ctx, `SELECT initialized FROM xinsight_accounts.authority WHERE singleton`).Scan(&initialized); e != nil || !initialized {
		return access.ManagedAccount{}, ErrAccountAuthorityUninitialized
	}
	if subject != "" {
		a, e := scanManaged(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM xinsight_accounts.accounts WHERE subject_id=$1`, subject))
		if errors.Is(e, sql.ErrNoRows) {
			return a, access.ErrAccountInvalid
		}
		if e != nil {
			return a, e
		}
		if a.Disabled || a.Revision != revision {
			return a, access.ErrAccountConflict
		}
		return a, nil
	}
	if username != access.NormalizeUsername(username) || len(username) < 3 || len(username) > 64 {
		return access.ManagedAccount{}, access.ErrAccountInvalid
	}
	var n int
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM xinsight_accounts.accounts WHERE role='platform_admin'`).Scan(&n); e != nil {
		return access.ManagedAccount{}, e
	}
	if n != 0 {
		return access.ManagedAccount{}, fmt.Errorf("an administrator identity already exists; use explicit recovery of that subject")
	}
	return access.ManagedAccount{Username: username, Role: access.RolePlatformAdmin, NeedsActivation: true, Revision: 1}, nil
}
func (s *LocalAccountStore) CheckNativeAccountHandoff(ctx context.Context, username, subject string, revision int64) (any, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, AccountAuthorityLock); e != nil {
		return nil, e
	}
	a, e := s.checkNativeHandoff(ctx, tx, username, subject, revision)
	if e != nil {
		return nil, e
	}
	return map[string]any{"check_only": true, "changed": false, "account": a, "password_input": "native_https_holder_page"}, nil
}
func (s *LocalAccountStore) NativeAccountHandoff(ctx context.Context, username, subject string, revision int64, digest string, publish func(access.ManagedAccount, time.Time) error) (any, error) {
	if len(digest) != 64 || publish == nil {
		return nil, access.ErrAccountInvalid
	}
	tx, e := beginAccountWrite(ctx, s.db)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	a, e := s.checkNativeHandoff(ctx, tx, username, subject, revision)
	if e != nil {
		return nil, e
	}
	if subject == "" {
		b := make([]byte, 16)
		if _, e = rand.Read(b); e != nil {
			return nil, e
		}
		a.SubjectID = "xs_" + hex.EncodeToString(b)
		a, e = scanManaged(tx.QueryRowContext(ctx, `INSERT INTO xinsight_accounts.accounts(subject_id,username,role,needs_activation) VALUES($1,$2,'platform_admin',true) RETURNING `+accountColumns, a.SubjectID, a.Username))
	} else {
		a, e = scanManaged(tx.QueryRowContext(ctx, `UPDATE xinsight_accounts.accounts SET needs_activation=true,revision=revision+1,updated_at=clock_timestamp() WHERE subject_id=$1 RETURNING `+accountColumns, subject))
	}
	if e != nil {
		return nil, e
	}
	var expires time.Time
	e = tx.QueryRowContext(ctx, `INSERT INTO xinsight_accounts.password_tickets(subject_id,verifier_sha256,purpose,account_revision,expires_at,created_by,ticket_generation) VALUES($1,$2,'activation',$3,clock_timestamp()+interval '15 minutes','root',(SELECT ticket_generation FROM xinsight_accounts.authority WHERE singleton)) ON CONFLICT(subject_id) DO UPDATE SET verifier_sha256=EXCLUDED.verifier_sha256,ticket_generation=EXCLUDED.ticket_generation,account_revision=EXCLUDED.account_revision,expires_at=EXCLUDED.expires_at,consumed_at=NULL,created_by='root',created_at=clock_timestamp() RETURNING expires_at`, a.SubjectID, digest, a.Revision).Scan(&expires)
	if e != nil {
		return nil, e
	}
	if e = accountAudit(ctx, tx, "root", "root", "access.account.native_handoff", a.SubjectID, map[string]any{"revision": a.Revision}); e != nil {
		return nil, e
	}
	// Publishing private output before commit prevents a successful mutation with
	// no holder handoff. A commit error is an unknown outcome: preserve the exact
	// private file and inspect state rather than generate another secret on retry.
	if e = publish(a, expires); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, fmt.Errorf("account handoff commit outcome unknown; retain private output and inspect exact subject")
	}
	return map[string]any{"changed": true, "account": a, "expires_at": expires, "private_handoff_written": true}, nil
}
