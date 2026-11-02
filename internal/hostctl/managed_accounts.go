package hostctl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/db"
)

type accountAuthorityDatabase interface {
	AccountAuthorityState(context.Context, DatabaseCredentials) (bool, bool, error)
	InitializeAccountAuthority(context.Context, DatabaseCredentials, access.AccountConfig, string) error
	RetireRestoredAccountLinks(context.Context, DatabaseCredentials, string) error
}

func (m PostgresManager) AccountAuthorityState(ctx context.Context, c DatabaseCredentials) (bool, bool, error) {
	conn, e := sql.Open("pgx", c.AdminDSN)
	if e != nil {
		return false, false, e
	}
	defer conn.Close()
	var present bool
	if e = conn.QueryRowContext(ctx, `SELECT to_regclass('xinsight_accounts.authority') IS NOT NULL`).Scan(&present); e != nil || !present {
		return present, false, e
	}
	var initialized bool
	e = conn.QueryRowContext(ctx, `SELECT initialized FROM xinsight_accounts.authority WHERE singleton`).Scan(&initialized)
	return true, initialized, e
}
func (m PostgresManager) InitializeAccountAuthority(ctx context.Context, c DatabaseCredentials, config access.AccountConfig, sha string) error {
	store, e := db.OpenLocalAccountStore(ctx, c.AdminDSN)
	if e != nil {
		return e
	}
	defer store.Close()
	return store.Initialize(ctx, config, sha)
}
func targetUsesDurableAccounts(t verifiedTarget) bool {
	return containsString(t.Bundle.Manifest.APIVersions, buildinfo.AccountsAPIVersion)
}
func (e *Engine) prepareAccountAuthority(ctx context.Context, j *Journal, c DatabaseCredentials, identity ReleaseIdentity) error {
	if identity.DatabaseSchema != "47" {
		return nil
	}
	manager, ok := e.Database.(accountAuthorityDatabase)
	if !ok {
		return fmt.Errorf("database backend does not support durable local accounts")
	}
	err := e.runPhase(j, "initialize_account_authority", func() error {
		present, initialized, err := manager.AccountAuthorityState(ctx, c)
		if err != nil || !present {
			return fmt.Errorf("account authority migration is unavailable")
		}
		if initialized {
			return nil
		}
		if j.Kind == "restore" && j.Before != nil && j.Before.DatabaseSchema == "47" {
			return fmt.Errorf("legacy backup lacks current durable account authority; refusing stale-file credential restoration")
		}
		config := access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3}
		digest := "bootstrap_required"
		if _, err = os.Lstat(e.Paths.AccessAccountsFile); err == nil {
			config, err = access.ReadAndValidateAccountFile(e.Paths.AccessAccountsFile)
			if err != nil {
				return err
			}
			raw, err := json.Marshal(config)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			digest = hex.EncodeToString(sum[:])
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return manager.InitializeAccountAuthority(ctx, c, config, digest)
	})
	if err != nil {
		return err
	}
	if j.Kind == "restore" {
		return e.runPhase(j, "retire_restored_activation_links", func() error { return manager.RetireRestoredAccountLinks(ctx, c, j.OperationID) })
	}
	return nil
}
func (e *Engine) validateDurableAccountPreflight(t verifiedTarget) (bool, error) {
	if !targetUsesDurableAccounts(t) {
		return false, nil
	}
	credentials, err := LoadDatabaseCredentials(e.Paths)
	if err != nil {
		return true, err
	}
	manager, ok := e.Database.(accountAuthorityDatabase)
	if !ok {
		return true, fmt.Errorf("database backend does not support durable local accounts")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, initialized, err := manager.AccountAuthorityState(ctx, credentials)
	if err != nil {
		return true, fmt.Errorf("durable account authority cannot be inspected")
	}
	if initialized {
		return true, nil
	}
	if _, err = os.Lstat(e.Paths.AccessAccountsFile); errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	_, err = access.ReadAndValidateAccountFile(e.Paths.AccessAccountsFile)
	return true, err
}

type AccountBootstrapRequest struct {
	Username         string
	SubjectID        string
	ExpectedRevision int64
	Output           string
	CheckOnly        bool
}

// Root-only first-owner handoff/emergency recovery. Passwords are entered only
// by the holder on the native HTTPS page. stdout contains metadata/path, never
// the one-time bearer. There is no old-file authentication fallback.
func (e *Engine) AccountBootstrap(ctx context.Context, r AccountBootstrapRequest) (any, error) {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return nil, err
	}
	lock, err := acquireOperationLock(e.Paths.OperationLock)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if !filepath.IsAbs(r.Output) || filepath.Clean(r.Output) != r.Output {
		return nil, fmt.Errorf("activation output must be an absolute private path")
	}
	parent, err := os.Lstat(filepath.Dir(r.Output))
	if err != nil || !parent.IsDir() || parent.Mode().Perm() != 0700 {
		return nil, fmt.Errorf("activation output requires an existing private0700 directory")
	}
	uid := uint32(0)
	if e.Paths.IsTestRoot {
		uid = uint32(os.Geteuid())
	}
	if st, ok := parent.Sys().(*syscall.Stat_t); !ok || st.Uid != uid {
		return nil, fmt.Errorf("activation output directory owner invalid")
	}
	if _, err = os.Lstat(r.Output); !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("activation output already exists or cannot be inspected")
	}
	tls, err := readTrustedTLSProxyEnvironmentFile(e.Paths.TrustedTLSProxyEnv)
	if err != nil {
		return nil, fmt.Errorf("activation handoff requires the configured HTTPS origin")
	}
	origin := tls["XINSIGHT_EXTERNAL_ORIGIN"]
	credentials, err := LoadDatabaseCredentials(e.Paths)
	if err != nil {
		return nil, err
	}
	store, err := db.OpenLocalAccountStore(ctx, credentials.AdminDSN)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	if r.CheckOnly {
		return store.CheckNativeAccountHandoff(ctx, r.Username, r.SubjectID, r.ExpectedRevision)
	}
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return nil, err
	}
	token := "xi-ac1_" + base64.RawURLEncoding.EncodeToString(b)
	return store.NativeAccountHandoff(ctx, r.Username, r.SubjectID, r.ExpectedRevision, access.TicketDigest(token), func(a access.ManagedAccount, expires time.Time) error {
		f, err := os.OpenFile(r.Output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
		page, err := template.New("private-account-handoff").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="referrer" content="no-referrer"><title>X-Insight private account activation</title><h1>Activate {{.Username}}</h1><p>This private one-time link authorizes setting this account password. Share only with its holder.</p><p>Expires: {{.Expires}}</p><a href="{{.Link}}">Set your password in X-Insight</a></html>`)
		if err != nil {
			return err
		}
		if err = page.Execute(f, map[string]string{"Username": a.Username, "Expires": expires.UTC().Format(time.RFC3339), "Link": origin + "/access/activate#" + token}); err != nil {
			return err
		}
		if err = f.Sync(); err != nil {
			return err
		}
		return nil
	})
}

func (e *Engine) ManagedAccountDirectory(ctx context.Context) (any, error) {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return nil, err
	}
	c, err := LoadDatabaseCredentials(e.Paths)
	if err != nil {
		return nil, err
	}
	store, err := db.OpenLocalAccountStore(ctx, c.AdminDSN)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListAccounts(ctx)
}

func (m PostgresManager) RetireRestoredAccountLinks(ctx context.Context, c DatabaseCredentials, operation string) error {
	store, e := db.OpenLocalAccountStore(ctx, c.AdminDSN)
	if e != nil {
		return e
	}
	defer store.Close()
	return store.RetireRestoredActivationLinks(ctx, operation)
}
