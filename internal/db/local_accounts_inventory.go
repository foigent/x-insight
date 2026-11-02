package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Server-side aggregate identities, never account PHC or activation bearers.
type LocalAccountInventory struct {
	Initialized          bool   `json:"initialized"`
	AccountsSHA256       string `json:"accounts_sha256"`
	TicketsSHA256        string `json:"tickets_sha256"`
	AuthoritySHA256      string `json:"authority_sha256"`
	AuthorityStateSHA256 string `json:"authority_state_sha256"`
	TicketGeneration     string `json:"ticket_generation"`
	LastRestoreOperation string `json:"last_restore_operation"`
	AccountCount         int    `json:"account_count"`
}

func readLocalAccountInventory(ctx context.Context, q workspaceQueryer) (LocalAccountInventory, error) {
	var out LocalAccountInventory
	var raw []byte
	e := q.QueryRowContext(ctx, `SELECT jsonb_build_object(
 'initialized',(SELECT initialized FROM xinsight_accounts.authority WHERE singleton),
 'authority_state_sha256',(SELECT encode(sha256(convert_to((to_jsonb(a)-'ticket_generation'-'last_restore_operation')::text,'UTF8')),'hex') FROM xinsight_accounts.authority a WHERE singleton),
 'ticket_generation',(SELECT ticket_generation::text FROM xinsight_accounts.authority WHERE singleton),
 'last_restore_operation',(SELECT last_restore_operation FROM xinsight_accounts.authority WHERE singleton),
 'account_count',(SELECT count(*) FROM xinsight_accounts.accounts),
 'accounts_sha256',(SELECT encode(sha256(convert_to(COALESCE(jsonb_agg(to_jsonb(a) ORDER BY subject_id),'[]')::text,'UTF8')),'hex') FROM xinsight_accounts.accounts a),
 'tickets_sha256',(SELECT encode(sha256(convert_to(COALESCE(jsonb_agg(to_jsonb(t) ORDER BY subject_id),'[]')::text,'UTF8')),'hex') FROM xinsight_accounts.password_tickets t),
 'authority_sha256',(SELECT encode(sha256(convert_to(to_jsonb(a)::text,'UTF8')),'hex') FROM xinsight_accounts.authority a WHERE singleton))`).Scan(&raw)
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(raw, &out)
	return out, e
}

// Complete-database recovery preserves account hashes/revisions, ticket rows,
// grants and audit references. Retire outstanding links without erasing their
// evidence: a restored unconsumed snapshot must not resurrect a spent bearer.
func (s *LocalAccountStore) RetireRestoredActivationLinks(ctx context.Context, operation string) error {
	if len(operation) < 8 || len(operation) > 128 {
		return fmt.Errorf("restore operation identity required")
	}
	tx, e := beginAccountWrite(ctx, s.db)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `UPDATE xinsight_accounts.authority SET ticket_generation=gen_random_uuid(),last_restore_operation=$1 WHERE singleton AND initialized AND last_restore_operation<>$1`, operation)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n > 0 {
		if e = accountAudit(ctx, tx, "root", "root", "access.account_authority.restore", "authority", map[string]any{"operation_id": operation, "outstanding_links": "retired", "account_credentials": "preserved"}); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (s *LocalAccountStore) Inventory(ctx context.Context) (LocalAccountInventory, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return LocalAccountInventory{}, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return LocalAccountInventory{}, e
	}
	return readLocalAccountInventory(ctx, tx)
}

// An old business-data backup has no person credentials. Preserve the current
// initialized database authority into the empty forward-migrated restore; never
// read the obsolete host file. This is a bounded compatibility phase inside the
// existing native restore, not a new recovery transport or login fallback.
func (s *LocalAccountStore) CarryIntoUninitialized(ctx context.Context, target *LocalAccountStore) (LocalAccountInventory, error) {
	var empty LocalAccountInventory
	src, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return empty, e
	}
	defer src.Rollback()
	if _, e = src.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, AccountAuthorityLock); e != nil {
		return empty, e
	}
	if _, e = src.ExecContext(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return empty, e
	}
	expected, e := readLocalAccountInventory(ctx, src)
	if e != nil || !expected.Initialized {
		return empty, fmt.Errorf("current durable account authority is not initialized")
	}
	var accounts, tickets, authority []byte
	e = src.QueryRowContext(ctx, `SELECT (SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY subject_id),'[]') FROM xinsight_accounts.accounts a),(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY subject_id),'[]') FROM xinsight_accounts.password_tickets t),(SELECT to_jsonb(a) FROM xinsight_accounts.authority a WHERE singleton)`).Scan(&accounts, &tickets, &authority)
	if e != nil {
		return empty, e
	}
	if len(accounts) > 128<<10 || len(tickets) > 128<<10 || len(authority) > 8192 {
		return empty, fmt.Errorf("account compatibility snapshot exceeds native bounds")
	}
	dst, e := beginAccountWrite(ctx, target.db)
	if e != nil {
		return empty, e
	}
	defer dst.Rollback()
	if _, e = dst.ExecContext(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return empty, e
	}
	actual, e := readLocalAccountInventory(ctx, dst)
	if e != nil {
		return empty, e
	}
	if actual.Initialized {
		if actual != expected {
			return empty, fmt.Errorf("restored account authority differs from admitted current authority")
		}
		return actual, nil
	}
	var count int
	if e = dst.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM xinsight_accounts.accounts)+(SELECT count(*) FROM xinsight_accounts.password_tickets)`).Scan(&count); e != nil || count != 0 {
		return empty, fmt.Errorf("restored account authority contains partial state")
	}
	if _, e = dst.ExecContext(ctx, `DELETE FROM xinsight_accounts.authority`); e != nil {
		return empty, e
	}
	if _, e = dst.ExecContext(ctx, `INSERT INTO xinsight_accounts.authority(singleton,initialized,legacy_sha256,initialized_at,ticket_generation,last_restore_operation) SELECT r.singleton,r.initialized,r.legacy_sha256,r.initialized_at,r.ticket_generation,r.last_restore_operation FROM jsonb_populate_record(NULL::xinsight_accounts.authority,$1::jsonb) r`, string(authority)); e != nil {
		return empty, e
	}
	if _, e = dst.ExecContext(ctx, `INSERT INTO xinsight_accounts.accounts(subject_id,username,role,disabled,needs_activation,password_hash,revision,created_at,updated_at) SELECT r.subject_id,r.username,r.role,r.disabled,r.needs_activation,r.password_hash,r.revision,r.created_at,r.updated_at FROM jsonb_populate_recordset(NULL::xinsight_accounts.accounts,$1::jsonb) r`, string(accounts)); e != nil {
		return empty, e
	}
	if _, e = dst.ExecContext(ctx, `INSERT INTO xinsight_accounts.password_tickets(subject_id,verifier_sha256,purpose,account_revision,expires_at,consumed_at,created_by,ticket_generation,created_at) SELECT r.subject_id,r.verifier_sha256,r.purpose,r.account_revision,r.expires_at,r.consumed_at,r.created_by,r.ticket_generation,r.created_at FROM jsonb_populate_recordset(NULL::xinsight_accounts.password_tickets,$1::jsonb) r`, string(tickets)); e != nil {
		return empty, e
	}
	got, e := readLocalAccountInventory(ctx, dst)
	if e != nil || got != expected {
		return empty, fmt.Errorf("account compatibility snapshot verification failed")
	}
	if e = dst.Commit(); e != nil {
		return empty, e
	}
	return got, nil
}
