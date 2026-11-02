package hostctl

import (
	"context"
	"fmt"

	"github.com/foigent/x-insight/internal/db"
)

type accountRestoreDatabase interface {
	AccountInventory(context.Context, DatabaseCredentials) (*db.LocalAccountInventory, error)
	CarryAccountAuthority(context.Context, DatabaseCredentials, DatabaseCredentials) (db.LocalAccountInventory, error)
}

func (m PostgresManager) AccountInventory(ctx context.Context, c DatabaseCredentials) (*db.LocalAccountInventory, error) {
	present, _, e := m.AccountAuthorityState(ctx, c)
	if e != nil || !present {
		return nil, e
	}
	s, e := db.OpenLocalAccountStore(ctx, c.AdminDSN)
	if e != nil {
		return nil, e
	}
	defer s.Close()
	v, e := s.Inventory(ctx)
	return &v, e
}
func (m PostgresManager) CarryAccountAuthority(ctx context.Context, current, restored DatabaseCredentials) (db.LocalAccountInventory, error) {
	s, e := db.OpenLocalAccountStore(ctx, current.AdminDSN)
	if e != nil {
		return db.LocalAccountInventory{}, e
	}
	defer s.Close()
	t, e := db.OpenLocalAccountStore(ctx, restored.AdminDSN)
	if e != nil {
		return db.LocalAccountInventory{}, e
	}
	defer t.Close()
	return s.CarryIntoUninitialized(ctx, t)
}

func (e *Engine) recordRestoreAccountSource(ctx context.Context, j *Journal, c DatabaseCredentials) error {
	manager, ok := e.Database.(accountRestoreDatabase)
	if !ok {
		return nil
	}
	v, err := manager.AccountInventory(ctx, c)
	if err != nil {
		return err
	}
	if j.BeforeLocalAccounts == nil {
		j.BeforeLocalAccounts = v
		return e.updateJournal(j)
	}
	if v == nil || *v != *j.BeforeLocalAccounts {
		return fmt.Errorf("original target local account authority changed")
	}
	return nil
}
func (e *Engine) prepareRestoredAccounts(ctx context.Context, j *Journal, plan restorePlan, restored DatabaseCredentials, backup BackupResult) error {
	if plan.Current.Identity.DatabaseSchema != "47" {
		return nil
	}
	manager, ok := e.Database.(accountRestoreDatabase)
	if !ok {
		return fmt.Errorf("database backend lacks account restore inventory")
	}
	if j.RestoredLocalAccounts == nil {
		expected := backup.LocalAccounts
		if expected == nil && j.BeforeLocalAccounts != nil && j.BeforeLocalAccounts.Initialized {
			var carried db.LocalAccountInventory
			if err := e.runPhase(j, "carry_current_account_authority", func() error {
				var err error
				carried, err = manager.CarryAccountAuthority(ctx, plan.Credentials, restored)
				return err
			}); err != nil {
				return err
			}
			// The completed phase may be resumed after its DB commit; the admitted
			// source inventory is already durable in the original restore journal.
			expected = &carried
			if carried.AccountsSHA256 == "" {
				expected = j.BeforeLocalAccounts
			}
		}
		if expected != nil {
			copy := *expected
			j.RestoredLocalAccounts = &copy
			if err := e.updateJournal(j); err != nil {
				return err
			}
		}
	}
	if j.RestoredLocalAccounts != nil {
		actual, err := manager.AccountInventory(ctx, restored)
		if err != nil || actual == nil {
			return fmt.Errorf("restored local account inventory unavailable")
		}
		expected := *j.RestoredLocalAccounts
		recorded := containsString(j.CompletedPhases, "retire_restored_activation_links")
		// The native DB receipt, bound to this journal's exact operation, also
		// covers a crash after commit but before completePhase is durable.
		retired := actual.LastRestoreOperation == j.OperationID && actual.TicketGeneration != expected.TicketGeneration && actual.AuthorityStateSHA256 == expected.AuthorityStateSHA256
		if actual.AccountsSHA256 != expected.AccountsSHA256 || actual.TicketsSHA256 != expected.TicketsSHA256 || actual.AccountCount != expected.AccountCount || actual.Initialized != expected.Initialized || actual.AuthorityStateSHA256 != expected.AuthorityStateSHA256 || (!retired && actual.AuthoritySHA256 != expected.AuthoritySHA256) || (recorded && !retired) {
			return fmt.Errorf("restored account authority integrity mismatch")
		}
	}
	// Reached by executeRestore itself on both the first attempt and resume,
	// before credential switch/service activation. This also initializes genuinely
	// legacy authorities and journals retirement of all pre-recovery links.
	if err := e.prepareAccountAuthority(ctx, j, restored, plan.Current.Identity); err != nil {
		return err
	}
	return e.recordRestoreAccountSource(ctx, j, plan.Credentials)
}
