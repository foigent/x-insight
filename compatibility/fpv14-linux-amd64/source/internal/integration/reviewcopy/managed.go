package reviewcopy

import (
	"context"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

type ManagedStore interface {
	CustodyStore
	ListURCCopyCandidates(context.Context, string, time.Time, int) ([]db.URCCopyCandidate, error)
	RecordURCCopyAttempt(context.Context, db.URCCopyCandidate, string, string, time.Time) error
	FinishLegacyCCustodyTransition(context.Context, string, time.Time) error
}

// PollURCCopies is called by the existing managed C polling job after each
// committed manifest page. It scans durable manifests, so a process crash
// between manifest commit and byte copy does not lose the work.
func PollURCCopies(ctx context.Context, source URContentSource, store ManagedStore,
	collectionID string, limits sourcefacts.ContentLimits, keys reviewbody.KeyRing, now func() time.Time) error {
	if source == nil || store == nil || now == nil || keys.Validate() != nil {
		return reviewbody.ErrInvalid
	}
	candidates, err := store.ListURCCopyCandidates(ctx, collectionID, now().UTC(), 16)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		at := now().UTC()
		expiry, parseErr := time.Parse(sourcefacts.TimeFormat, candidate.Ref.ExpiresAt)
		if parseErr != nil || !at.Before(expiry) {
			err = reviewbody.ErrExpired
		} else {
			_, err = CopyURCReviewBody(ctx, source, store, collectionID, candidate.Ref, limits, keys, now)
		}
		status, reason := copyAttemptOutcome(err)
		if recordErr := store.RecordURCCopyAttempt(ctx, candidate, status, reason, now().UTC()); recordErr != nil {
			return recordErr
		}
	}
	// Per-item failures already have durable state. They must not turn an
	// independently committed C manifest page into a failed metadata pull.
	return store.FinishLegacyCCustodyTransition(ctx, collectionID, now().UTC())
}

func copyAttemptOutcome(err error) (string, string) {
	switch {
	case err == nil:
		return "accepted", "copy_accepted"
	case errors.Is(err, reviewbody.ErrIdentityPending):
		return "retry", "account_identity_pending"
	case errors.Is(err, reviewbody.ErrProjectBindingPending):
		return "retry", "project_binding_missing"
	case errors.Is(err, reviewbody.ErrPolicy):
		return "excluded_policy", "effective_policy_disabled"
	case errors.Is(err, reviewbody.ErrScope):
		return "scope_lost", "managed_source_scope_lost"
	case errors.Is(err, reviewbody.ErrExpired):
		return "source_expired", "source_content_expired"
	case errors.Is(err, reviewbody.ErrIntegrity), errors.Is(err, reviewbody.ErrConflict), errors.Is(err, reviewbody.ErrInvalid):
		return "integrity_gap", "source_content_integrity_gap"
	default:
		return "retry", "source_copy_unavailable"
	}
}
