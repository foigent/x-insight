package sourcefacts

import (
	"context"
	"errors"
	"github.com/foigent/x-insight/internal/connectorsync"
	"time"
)

// Inventory pages have their own position. They are not event checkpoints,
// request terminal evidence, or one simultaneous snapshot of the whole source.
type CurrentRuntimeCursor struct {
	Generation        int64
	PageOrdinal       int64
	Revision          int64
	Cursor            string
	RequiresAttention bool
}

type CurrentRuntimeStore interface {
	LoadCurrentRuntimeCursor(context.Context, Stream) (CurrentRuntimeCursor, error)
	CommitCurrentRuntimePage(context.Context, Stream, CurrentRuntimeCursor, []byte, time.Time) error
	RecordCurrentRuntimeFailure(context.Context, Stream, CurrentRuntimeCursor, string, time.Time) error
}

type CurrentRuntimeReader interface {
	FetchCurrentRuntime(context.Context, string, int) ([]byte, error)
}

// An expired historical cursor is not a revoked inventory grant. Current
// collection still preflights the exact scope and credential independently.
// Pause, revocation and other durable trust failures remain blocking.
func CurrentRuntimeCollectionAllowed(s Stream) bool {
	return s.Status == "active" && (!s.RequiresAttention || s.LastError == "cursor_expired" || s.LastError == "cursor_or_epoch_conflict")
}

// PullCurrentRuntimeOnce advances at most one inventory page so a large
// inventory cannot monopolize the existing per-profile worker. A failed
// inventory refresh never resets or advances the historical event position.
func (c Consumer) PullCurrentRuntimeOnce(ctx context.Context, streamID string) (out CurrentRuntimePage, resultErr error) {
	store, ok := c.Store.(CurrentRuntimeStore)
	reader, readable := c.Reader.(CurrentRuntimeReader)
	if !ok || !readable {
		return CurrentRuntimePage{}, ErrUnavailable
	}
	s, err := c.Store.LoadSourceFactsStream(ctx, streamID)
	if err != nil {
		return CurrentRuntimePage{}, err
	}
	ctx, finish, gateErr := beginSync(ctx, c.Store, s.SourceInstance, s.OrgID, s.ProjectID, "runtime:"+s.ID)
	if gateErr != nil {
		return CurrentRuntimePage{}, gateErr
	}
	defer func() { finish(resultErr) }()
	if s.Profile != RuntimeProfile || !CurrentRuntimeCollectionAllowed(s) {
		return CurrentRuntimePage{}, ErrUnavailable
	}
	if c.ExpectedStream != nil && !SameRegistration(*c.ExpectedStream, s) {
		return CurrentRuntimePage{}, ErrScopeChanged
	}
	cursor, err := store.LoadCurrentRuntimeCursor(ctx, s)
	if err != nil {
		return CurrentRuntimePage{}, err
	}
	now := func() time.Time {
		if c.Now != nil {
			return c.Now().UTC()
		}
		return time.Now().UTC()
	}
	fail := func(code string, cause error) (CurrentRuntimePage, error) {
		if connectorsync.IsControlStop(cause) {
			return CurrentRuntimePage{}, cause
		}
		if err := store.RecordCurrentRuntimeFailure(ctx, s, cursor, code, now()); err != nil {
			return CurrentRuntimePage{}, err
		}
		return CurrentRuntimePage{}, cause
	}
	if err = Preflight(ctx, c.Reader, s); err != nil {
		code := sourceFailure(err)
		if err == ErrInvalid {
			code = "invalid_capabilities"
		} else if err == ErrScopeChanged {
			code = "scope_changed"
		} else if err == ErrUnavailable {
			code = "source_paused"
		}
		return fail(code, err)
	}
	raw, err := reader.FetchCurrentRuntime(ctx, cursor.Cursor, 50)
	if err != nil {
		return fail(sourceFailure(err), err)
	}
	page, err := DecodeCurrentRuntimePage(raw, s.SourceInstance, s.OrgID, s.ProjectID)
	if err != nil || page.Coverage != "lease_backed_inventory" || page.HasMore && page.NextCursor == cursor.Cursor {
		return fail("invalid_page", ErrInvalid)
	}
	if err = store.CommitCurrentRuntimePage(ctx, s, cursor, raw, now()); err != nil {
		if errors.Is(err, ErrInvalid) {
			return fail("invalid_page", err)
		}
		return CurrentRuntimePage{}, err
	}
	connectorsync.Progress(ctx, page.HasMore, len(page.Entries))
	return page, nil
}
