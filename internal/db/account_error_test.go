package db

import (
	"errors"
	"github.com/foigent/x-insight/internal/access"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

func TestAccountWriteErrorSeparatesExistingUsernameFromRevisionConflict(t *testing.T) {
	tests := []struct {
		e    error
		want error
	}{
		{&pgconn.PgError{Code: "23505", SchemaName: "xinsight_accounts", TableName: "accounts", ConstraintName: "accounts_username_key"}, access.ErrAccountExists},
		{&pgconn.PgError{Code: "23505", SchemaName: "xinsight_accounts", TableName: "accounts", ConstraintName: "accounts_pkey"}, access.ErrAccountConflict},
		{&pgconn.PgError{Code: "40001"}, access.ErrAccountConflict},
	}
	for _, tt := range tests {
		if got := accountWriteError(tt.e); !errors.Is(got, tt.want) {
			t.Fatalf("got%v want%v", got, tt.want)
		}
	}
}
