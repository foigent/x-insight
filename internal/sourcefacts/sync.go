package sourcefacts

import (
	"context"
	"time"
)

type syncStore interface {
	BeginSync(context.Context, string, string, string, string) (context.Context, func(error), error)
}
type scheduleStore interface {
	SyncSchedule(context.Context, string, string, time.Duration) (time.Duration, bool, bool, error)
}

// Production stores implement this port. Fixtures and offline stores do not
// acquire a live permit; they retain their original explicit authorization.
func beginSync(ctx context.Context, store any, source, org, project, unit string) (context.Context, func(error), error) {
	if gate, ok := store.(syncStore); ok {
		return gate.BeginSync(ctx, source, org, project, unit)
	}
	return ctx, func(error) {}, nil
}
func jobSchedule(ctx context.Context, store any, kind, id string, base time.Duration) (time.Duration, bool, bool, error) {
	if s, ok := store.(scheduleStore); ok {
		return s.SyncSchedule(ctx, kind, id, base)
	}
	return base, false, false, nil
}
