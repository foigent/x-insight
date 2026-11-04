package connectoractivation

import (
	"context"
	"errors"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
)

func syncFailure(stage string, err error, unit ...string) error {
	category := "unknown"
	var postgres *pgconn.PgError
	switch {
	case errors.Is(err, context.Canceled):
		category = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		category = "deadline"
	// This means remote 401/403, never an inferred bad password or credential.
	case errors.Is(err, urmachine.ErrUnauthorized):
		category = "authorization"
	case errors.Is(err, urmachine.ErrUnavailable):
		category = "unavailable"
	case errors.Is(err, urmachine.ErrUnconfirmed):
		category = "unconfirmed"
	case errors.Is(err, urmachine.ErrContract), errors.Is(err, connectorcontrol.ErrInvalid):
		category = "contract"
	case errors.Is(err, urmachine.ErrNotFound), errors.Is(err, connectorcontrol.ErrNotFound):
		category = "not_found"
	case errors.Is(err, urmachine.ErrConflict), errors.Is(err, connectorcontrol.ErrConflict), errors.Is(err, sourcefacts.ErrConflict), errors.Is(err, sourcefacts.ErrScopeChanged):
		category = "conflict"
	case errors.Is(err, hostctl.ErrOperationBusy):
		category = "host_busy"
	case os.IsPermission(err):
		category = "local_permission"
	case errors.As(err, &postgres):
		category = "local_store"
	}
	return connectorsync.WithFailure(stage, category, err, unit...)
}
