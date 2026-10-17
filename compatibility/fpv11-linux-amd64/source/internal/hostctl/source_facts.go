package hostctl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

const sourceFactsWorkerUnit = "xinsight-source-facts.service"

type SourceFactsRequest struct {
	Action            string
	ID                string
	Revision          int64
	Acknowledge       bool
	ConfigFile        string
	AuthorizationFile string
	TargetRef         string
}

// ManageSourceFacts uses host administrator credentials and the maintenance
// lock. Collection grants are independent of workspace reader grants.
func (e *Engine) ManageSourceFacts(ctx context.Context, r SourceFactsRequest) (any, error) {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return nil, err
	}
	var result any
	err := e.withOperationLock(func() error {
		if r.Action == "configure" {
			c, err := sourcefacts.LoadManagedConfig(r.ConfigFile)
			if err != nil {
				return err
			}
			for _, keyFile := range c.CredentialFiles() {
				if _, err := config.LoadSourceFactsServiceKey(keyFile); err != nil {
					return err
				}
			}
			state, err := e.inspectManagedWorkerState(ctx, sourceFactsWorkerUnit, e.Paths.SourceFactsServiceUnit, "source facts worker")
			if err != nil {
				return err
			}
			if state.present {
				if err := runSystemctl(ctx, e.Runner, "stop", sourceFactsWorkerUnit); err != nil {
					return err
				}
			}
			if err := ensureDirectory(e.Paths.EtcDir, 0o755); err != nil {
				return err
			}
			raw, err := json.MarshalIndent(c, "", "  ")
			if err != nil {
				return err
			}
			if err := writeSourceFactsConfiguration(e.Paths.SourceFactsConfig, append(raw, '\n')); err != nil {
				return err
			}
			if err := e.installSourceFactsCredentials(c); err != nil {
				return err
			}
			if err := runSystemctl(ctx, e.Runner, "daemon-reload"); err != nil {
				return err
			}
			result = map[string]any{"status": "configured", "profiles": len(c.Profiles), "budgets": len(c.Budgets), "contents": len(c.Contents), "worker_started": false, "server_restart_required": contentServerCredentialsConfigured(e.Paths)}
			return nil
		}
		if r.Action == "worker-start" || r.Action == "worker-stop" || r.Action == "worker-status" {
			if r.Action == "worker-start" {
				c, err := sourcefacts.LoadManagedConfig(e.Paths.SourceFactsConfig)
				if err != nil {
					return err
				}
				if err := e.installSourceFactsCredentials(c); err != nil {
					return err
				}
				if err := runSystemctl(ctx, e.Runner, "daemon-reload"); err != nil {
					return err
				}
				if err := runSystemctl(ctx, e.Runner, "enable", sourceFactsWorkerUnit); err != nil {
					return err
				}
				if err := runSystemctl(ctx, e.Runner, "start", sourceFactsWorkerUnit); err != nil {
					return err
				}
			} else if r.Action == "worker-stop" {
				if err := runSystemctl(ctx, e.Runner, "stop", sourceFactsWorkerUnit); err != nil {
					return err
				}
			}
			s, err := e.inspectManagedWorkerState(ctx, sourceFactsWorkerUnit, e.Paths.SourceFactsServiceUnit, "source facts worker")
			result = map[string]bool{"present": s.present, "active": s.active, "enabled": s.enabled}
			return err
		}
		credentials, err := LoadDatabaseCredentials(e.Paths)
		if err != nil {
			return err
		}
		store, err := db.OpenSourceFactsStore(credentials.AdminDSN)
		if err != nil {
			return err
		}
		defer store.Close()
		if strings.HasPrefix(r.Action, "content-") {
			result, err = e.manageContentCollection(ctx, store, r)
			return err
		}
		if strings.HasPrefix(r.Action, "budget-") {
			result, err = e.manageBudgetCollection(ctx, store, r)
			return err
		}
		if strings.HasPrefix(r.Action, "offline-") {
			switch r.Action {
			case "offline-status":
				result, err = store.LoadSourceOfflineRegistration(ctx, r.ID)
			case "offline-disable", "offline-revoke":
				status := "disabled"
				if r.Action == "offline-revoke" {
					status = "revoked"
				}
				result, err = store.ControlSourceOfflineAuthorization(ctx, r.ID, status, r.Revision, "")
			case "offline-register", "offline-enable":
				product := buildinfo.Current()
				a, loadErr := sourcefacts.LoadOfflineAuthorization(r.AuthorizationFile, r.TargetRef, product.ProductVersion, product.Commit, time.Now().UTC())
				if loadErr != nil {
					return loadErr
				}
				if r.ID != "" && r.ID != a.AuthorizationID {
					return sourcefacts.ErrAuthorization
				}
				if r.Action == "offline-register" {
					result, err = store.RegisterSourceOfflineAuthorization(ctx, a)
				} else {
					result, err = store.ControlSourceOfflineAuthorization(ctx, a.AuthorizationID, "active", r.Revision, a.Digest(), a)
				}
			default:
				return sourcefacts.ErrInvalid
			}
			return err
		}
		switch r.Action {
		case "status":
			result, err = store.LoadSourceFactsStream(ctx, r.ID)
			return err
		case "pause", "revoke":
			status := "paused"
			if r.Action == "revoke" {
				status = "revoked"
			}
			result, err = store.ControlSourceFactsStream(ctx, r.ID, status, r.Revision, false)
			return err
		case "register", "preflight", "enable", "resume":
		default:
			return sourcefacts.ErrInvalid
		}
		c, err := sourcefacts.LoadManagedConfig(e.Paths.SourceFactsConfig)
		if err != nil {
			return err
		}
		var selected *sourcefacts.ManagedProfileConfig
		for _, p := range c.Profiles {
			if p.StreamID == r.ID {
				selected = &p
				break
			}
		}
		if selected == nil {
			return sourcefacts.ErrInvalid
		}
		if r.Action == "register" {
			result, err = store.RegisterSourceFactsStream(ctx, selected.Registration())
			return err
		}
		s, err := store.LoadSourceFactsStream(ctx, r.ID)
		if err != nil {
			return err
		}
		if !selected.Matches(s) {
			return sourcefacts.ErrScopeChanged
		}
		key, err := config.LoadSourceFactsServiceKey(selected.KeyFile)
		if err != nil {
			return err
		}
		reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: selected.Endpoint, ServiceKey: key, AllowPrivateTunnelHTTP: selected.AllowPrivateTunnelHTTP, RequestTimeout: 5 * time.Second})
		if err != nil {
			return err
		}
		if err = sourcefacts.Preflight(ctx, reader, s); err != nil {
			return err
		}
		if r.Action == "preflight" {
			result = map[string]string{"stream_id": s.ID, "status": "preflight_passed"}
			return nil
		}
		result, err = store.ControlSourceFactsStream(ctx, r.ID, "active", r.Revision, r.Acknowledge)
		return err
	})
	return result, err
}

func (e *Engine) installSourceFactsCredentials(c sourcefacts.ManagedConfig) error {
	if err := c.Validate(); err != nil {
		return err
	}
	contents := "[Service]\nLoadCredential=\nLoadCredential=postgres-importer.dsn:" + e.Paths.ImporterDSNFile + "\n"
	for i, keyFile := range c.CredentialFiles() {
		if strings.ContainsAny(keyFile, "\\\"%\r\n") {
			return sourcefacts.ErrInvalid
		}
		// LoadCredential consumes one id:path value; quotes become literal bytes.
		contents += fmt.Sprintf("LoadCredential=source-profile-%d.key:%s\n", i, keyFile)
	}
	dir := e.Paths.SourceFactsServiceUnit + ".d"
	if err := ensureDirectory(dir, 0o755); err != nil {
		return err
	}
	// Write the small generated drop-in before daemon-reload, preserving the last
	// complete configuration until rename. No credential bytes enter this file.
	if err := writeSourceFactsConfiguration(filepath.Join(dir, "10-source-credentials.conf"), []byte(contents)); err != nil {
		return err
	}
	return e.installContentServerCredentials(c)
}

func writeSourceFactsConfiguration(path string, contents []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".source-facts-configuration-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if err = writeSyncClose(f, contents); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncDirectory(dir)
}

// Local C management intentionally has no upstream credential-management DTO.
func (e *Engine) manageContentCollection(ctx context.Context, store *db.SourceFactsStore, r SourceFactsRequest) (any, error) {
	switch r.Action {
	case "content-status":
		return store.LoadContentCollection(ctx, r.ID)
	case "content-pause":
		return store.ControlContentCollection(ctx, r.ID, "paused", r.Revision, false)
	case "content-revoke":
		return store.ControlContentCollection(ctx, r.ID, "revoked", r.Revision, false)
	case "content-register", "content-preflight", "content-enable", "content-resume", "content-rebind":
	default:
		return nil, sourcefacts.ErrInvalid
	}
	c, err := sourcefacts.LoadManagedConfig(e.Paths.SourceFactsConfig)
	if err != nil {
		return nil, err
	}
	var selected *sourcefacts.ManagedContentConfig
	for _, p := range c.Contents {
		if p.CollectionID == r.ID {
			v, e := c.ResolveContent(p)
			if e != nil {
				return nil, e
			}
			selected = &v
			break
		}
	}
	if selected == nil {
		return nil, sourcefacts.ErrInvalid
	}
	if r.Action == "content-register" {
		return store.RegisterContentCollection(ctx, selected.Registration())
	}
	live, err := store.LoadContentCollection(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if r.Action != "content-rebind" && !sourcefacts.SameContentCollection(live, selected.Registration()) {
		return nil, sourcefacts.ErrScopeChanged
	}
	key, err := config.LoadSourceFactsServiceKey(selected.KeyFile)
	if err != nil {
		return nil, err
	}
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: selected.Endpoint, ServiceKey: key, AllowPrivateTunnelHTTP: selected.AllowPrivateTunnelHTTP, RequestTimeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	preflight := live
	if r.Action == "content-rebind" {
		preflight = selected.Registration()
	}
	if _, err = sourcefacts.PreflightContent(ctx, reader, preflight); err != nil {
		return nil, err
	}
	if r.Action == "content-rebind" {
		return store.RebindContentCollection(ctx, preflight, r.Revision)
	}
	if r.Action == "content-preflight" {
		return map[string]string{"collection_id": live.ID, "status": "preflight_passed"}, nil
	}
	return store.ControlContentCollection(ctx, r.ID, "active", r.Revision, r.Acknowledge, live)
}

// Installation generates only systemd references, never copies secret bytes.
// A service restart is required to refresh systemd's credential snapshot; the
// configure action does not silently restart the API service.
func (e *Engine) installContentServerCredentials(c sourcefacts.ManagedConfig) error {
	if len(c.Contents) == 0 {
		if _, err := os.Lstat(filepath.Join(e.Paths.ServiceUnit+".d", "30-content-credentials.conf")); os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}
	}
	contents := "[Service]\nEnvironment=XINSIGHT_CONTENT_CONFIG=" + e.Paths.SourceFactsConfig + "\n"
	for _, p := range c.Contents {
		resolved, err := c.ResolveContent(p)
		if err != nil {
			return err
		}
		contents += "LoadCredential=source-content-" + p.CollectionID + ".key:" + resolved.KeyFile + "\n"
	}
	dir := e.Paths.ServiceUnit + ".d"
	if err := ensureDirectory(dir, 0755); err != nil {
		return err
	}
	return writeSourceFactsConfiguration(filepath.Join(dir, "30-content-credentials.conf"), []byte(contents))
}

func contentServerCredentialsConfigured(paths Paths) bool {
	_, err := os.Lstat(filepath.Join(paths.ServiceUnit+".d", "30-content-credentials.conf"))
	return err == nil
}
