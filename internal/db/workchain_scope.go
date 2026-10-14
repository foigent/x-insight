package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
)

// WorkchainGrantLock is distinct from migration and Finding graph locks.
// The migration's statement trigger uses the same key after its table lock.
const WorkchainGrantLock int64 = 6361703472894002481

const noAuthorizedScopeRevision = "no_authorized_scope"

// Existing scope digests retain their bytes when no B capability is granted.
func observationScopeSuffix(auth workbench.Authorization) string {
	for _, g := range auth.Grants {
		if g.AllowSourceRuntime || g.AllowSourceAttempt || g.AllowSourceCapability || g.AllowSourcePolicy || g.AllowSourceDestination || g.AllowResource {
			return "\n" + auth.ObservationRevision
		}
	}
	return ""
}

// Empty content has zero contribution to historical scope digest bytes.
func contentScopeSuffix(auth workbench.Authorization) string {
	for _, g := range auth.Grants {
		if g.AllowContent && len(g.AllowedContentPurposes) == 1 && g.AllowedContentPurposes[0] == "human_review" {
			return "\nxinsight.content-authorization.v1\n" + auth.ContentRevision
		}
	}
	return ""
}

// ScopedWorkchainStore reuses the two existing production pools in one order:
// reader authorization -> workspace business. Business reads have the existing
// workspace SELECT authority; this object exposes no M0 import capability.
type ScopedWorkchainStore struct {
	reader    *sql.DB
	workspace *WorkspaceStore
	evidence  *EvidenceStore
}

func NewScopedWorkchainStore(reader *EvidenceStore, workspace *WorkspaceStore) *ScopedWorkchainStore {
	if reader == nil || workspace == nil {
		return nil
	}
	return &ScopedWorkchainStore{reader: reader.db, workspace: workspace, evidence: NewEvidenceStore(workspace.db)}
}

func (s *ScopedWorkchainStore) Evidence() *EvidenceStore { return s.evidence }

// Begin keeps only an authorization connection. No business operation borrows
// from that pool, and an aliased pool is rejected before borrowing anything.
// finish must run after business commit / buffered response construction.
func (s *ScopedWorkchainStore) Begin(ctx context.Context, subject string, reviewer bool, commitCheck func(func() error) error) (context.Context, func() error, error) {
	return s.begin(ctx, subject, reviewer, commitCheck, false, false)
}

// Legacy cost readers must retain their own-record lookup and preference
// semantics even with no grants. Their handlers enforce each capability while
// this same lock prevents a concurrent grant change during the response.
func (s *ScopedWorkchainStore) BeginWorkbenchReading(ctx context.Context, subject string, commitCheck func(func() error) error) (context.Context, func() error, error) {
	return s.begin(ctx, subject, false, commitCheck, true, false)
}

// Budget reads retain the session role in their revision identity. Route and
// capability checks still determine the allowed operation.
func (s *ScopedWorkchainStore) BeginBudgetReading(ctx context.Context, subject string, reviewer bool, commitCheck func(func() error) error) (context.Context, func() error, error) {
	return s.begin(ctx, subject, reviewer, commitCheck, true, true)
}

func (s *ScopedWorkchainStore) begin(ctx context.Context, subject string, reviewer bool, commitCheck func(func() error) error, allowEmpty, budgets bool) (context.Context, func() error, error) {
	if s == nil || s.reader == nil || s.workspace == nil || s.reader == s.workspace.db {
		return ctx, nil, workchain.ErrConfiguration
	}
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return ctx, nil, err
	}
	fail := func(err error) (context.Context, func() error, error) { _ = tx.Rollback(); return ctx, nil, err }
	// TRUNCATE owns AccessExclusive before its trigger. Never hold an advisory
	// lock while waiting to acquire this table lock in the opposite order.
	if _, err = tx.ExecContext(ctx, `LOCK TABLE workbench_access_grants IN ACCESS SHARE MODE`); err != nil {
		return fail(err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, WorkchainGrantLock); err != nil {
		return fail(err)
	}
	// A separate READ COMMITTED statement sees any writer that completed while
	// the prior lock statement waited. Business RR snapshots start later.
	auth, err := loadWorkbenchAuthorization(ctx, tx, subject)
	if err != nil {
		return fail(err)
	}
	if len(auth.Grants) == 0 && !allowEmpty {
		return fail(workchain.ErrDenied)
	}
	if budgets {
		// Hold the same source locks as collection admission/control until the
		// buffered response is checked. Revocation cannot race an amount response.
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended('source-facts:'||source_id,0)) FROM (
 SELECT DISTINCT source_system_id::text source_id FROM workbench_access_grants WHERE subject_id=$1 AND active
 AND cost_project_ref IS NULL AND (allow_budget_definition OR allow_budget_summary OR allow_budget_alert) ORDER BY source_id) allowed`, subject); err != nil {
			return fail(err)
		}
	}
	a := &workchain.Access{Subject: subject, Reviewer: reviewer, Authorization: auth, CommitCheck: commitCheck,
		Revision: digestText(fmt.Sprintf("%s\n%t\n%s\n%s\n%s", subject, reviewer, auth.Revision, auth.SourceRevision, workchain.ProjectionVersion) + purposeScopeSuffix(auth) + observationScopeSuffix(auth) + contentScopeSuffix(auth))}
	if len(auth.Grants) == 0 {
		a.Revision = noAuthorizedScopeRevision
	}
	return workchain.WithAccess(ctx, a), tx.Rollback, nil
}

func commitWorkspaceTx(ctx context.Context, tx *sql.Tx, beforeCommit ...func() error) error {
	commit := func() error {
		for _, check := range beforeCommit {
			if err := check(); err != nil {
				return err
			}
		}
		return tx.Commit()
	}
	if a := workchain.FromContext(ctx); a != nil {
		if a.CommitCheck == nil {
			return workchain.ErrDenied
		}
		if err := refreshScopedCases(ctx, tx); err != nil {
			return err
		}
		return a.CommitCheck(commit)
	}
	return commit()
}

func (s *ScopedWorkchainStore) CurrentRevision(ctx context.Context, subject string, reviewer bool) (string, error) {
	scoped, finish, err := s.Begin(ctx, subject, reviewer, nil)
	if errors.Is(err, workchain.ErrDenied) {
		return noAuthorizedScopeRevision, nil
	}
	if err != nil {
		return "", err
	}
	revision := workchain.FromContext(scoped).Revision
	return revision, finish()
}
