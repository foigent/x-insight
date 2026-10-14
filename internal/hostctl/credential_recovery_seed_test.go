package hostctl

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCredentialRecoverySeedRealEngineReplayAndFailureFences(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	const actor = "xs_0123456789abcdef"
	const installation = "xci_AAAAAAAAAAAAAAAAAAAAAA"
	const request = "12345678-1234-4234-8234-123456789abc"
	accounts := access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{{SubjectID: actor, Username: "setup-admin", Role: access.RolePlatformAdmin, PasswordHash: cleanBaselineTestPasswordHash}}}
	writeHostJSON(t, e.Paths.AccessAccountsFile, accounts, 0600)
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	e.Now = func() time.Time { return now }
	e.Random = bytes.NewReader(bytes.Repeat([]byte{0x71}, 48))
	callbackFailure := errors.New("private store failure")
	var first CredentialRecoverySeed
	if err := e.WithCredentialRecoverySeed(t.Context(), actor, installation, request, func(seed CredentialRecoverySeed) error {
		first = seed
		return callbackFailure
	}); !errors.Is(err, callbackFailure) {
		t.Fatal("store callback failure was not preserved", err)
	}
	if first.RequestID != request || first.ActorSubject != actor || first.InstallationID != installation ||
		!connectorcontrol.IsRecoveryID(first.RecoveryID) || !connectorcontrol.IsRecoveryTicket(first.Ticket) ||
		first.ExpiresAt.Sub(first.CreatedAt) != connectorcontrol.ControlRecoveryTTL {
		t.Fatal("root-owned recovery seed is invalid")
	}
	seedFile := filepath.Join(e.Paths.EtcDir, "connector-credential-recoveries", request+".seed.json")
	before, err := os.ReadFile(seedFile)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(seedFile); err != nil || info.Mode().Perm() != 0600 || !info.Mode().IsRegular() {
		t.Fatal("seed file permissions changed", err)
	}
	// A lost response or callback failure must reuse the exact seed without new
	// randomness. This is the real Engine path, including its root-owned file.
	e.Random = bytes.NewReader(nil)
	if err := e.WithCredentialRecoverySeed(t.Context(), actor, installation, request, func(seed CredentialRecoverySeed) error {
		if seed != first {
			t.Fatal("same request regenerated recovery material")
		}
		return nil
	}); err != nil {
		t.Fatal("same-request replay", err)
	}
	if after, err := os.ReadFile(seedFile); err != nil || !bytes.Equal(after, before) {
		t.Fatal("replay changed root seed", err)
	}
	if err := e.WithCredentialRecoverySeed(t.Context(), actor, "xci_BBBBBBBBBBBBBBBBBBBBBB", request, func(CredentialRecoverySeed) error {
		t.Fatal("conflicting request reached store callback")
		return nil
	}); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("conflicting installation reused seed", err)
	}
	lock, err := acquireOperationLock(e.Paths.OperationLock)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.WithCredentialRecoverySeed(t.Context(), actor, installation, request, func(CredentialRecoverySeed) error {
		t.Fatal("busy host reached store callback")
		return nil
	}); !errors.Is(err, ErrOperationBusy) {
		t.Fatal("busy host operation was not fenced", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	now = first.ExpiresAt
	if err := e.WithCredentialRecoverySeed(t.Context(), actor, installation, request, func(CredentialRecoverySeed) error {
		t.Fatal("expired seed reached store callback")
		return nil
	}); !errors.Is(err, connectorcontrol.ErrExpired) {
		t.Fatal("expired seed was reused", err)
	}
	if after, err := os.ReadFile(seedFile); err != nil || !bytes.Equal(after, before) {
		t.Fatal("failed replay changed original seed", err)
	}
}

func TestCredentialRecoveryFailureClassesContainOnlyFixedDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		err      error
		category string
		sqlstate string
	}{
		{ErrOperationBusy, "operation_busy", "none"},
		{context.DeadlineExceeded, "context_deadline", "none"},
		{&pgconn.PgError{Code: "23505", Message: "private SQL parameter"}, "database_sqlstate", "23505"},
		{&pgconn.PgError{Code: "XX000", Message: "private SQL parameter"}, "internal", "none"},
		{errors.New("private path and ticket"), "internal", "none"},
	} {
		category, sqlstate := credentialRecoveryFailureClass(tc.err)
		if category != tc.category || sqlstate != tc.sqlstate {
			t.Fatalf("unexpected safe failure class %q %q", category, sqlstate)
		}
	}
}
