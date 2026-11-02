// ci-account-initialize prepares only the disposable backend smoke database.
// It uses the native one-time import; the server never reads an account file.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
)

func main() {
	if err := initialize(); err != nil {
		// Neither database errors nor credential input belongs in CI logs.
		fmt.Fprintln(os.Stderr, "disposable account initialization failed")
		os.Exit(1)
	}
}

func initialize() error {
	if len(os.Args) != 2 || os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("XINSIGHT_CI_ACCOUNT_INITIALIZE") != "1" {
		return fmt.Errorf("explicit CI fixture required")
	}
	dsn, err := config.LoadAdminPostgresDSN()
	if err != nil {
		return err
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") ||
		(u.Path != "/xinsight_test" && !strings.HasPrefix(u.Path, "/xi_accounts_ci_")) {
		return fmt.Errorf("disposable loopback database required")
	}
	accounts, err := access.ReadAndValidateAccountFile(os.Args[1])
	if err != nil {
		return err
	}
	raw, err := json.Marshal(accounts)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = (hostctl.PostgresManager{}).InitializeAccountAuthority(ctx, hostctl.DatabaseCredentials{AdminDSN: dsn}, accounts, hex.EncodeToString(digest[:])); err != nil {
		return err
	}
	store, err := db.OpenLocalAccountStore(ctx, dsn)
	if err != nil {
		return err
	}
	defer store.Close()
	listed, err := store.ListAccounts(ctx)
	if err != nil || len(listed) != len(accounts.Accounts) {
		return fmt.Errorf("account inventory differs")
	}
	for _, want := range accounts.Accounts {
		got, err := store.ManagedCredential(ctx, want.Username)
		if err != nil || got.SubjectID != want.SubjectID || got.Role != want.Role || got.Disabled != want.Disabled || got.PasswordHash != want.PasswordHash {
			return fmt.Errorf("account import differs")
		}
	}
	fmt.Printf("native_account_import=verified account_count=%d\n", len(listed))
	return nil
}
