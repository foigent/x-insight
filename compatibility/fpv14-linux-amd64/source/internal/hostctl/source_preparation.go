package hostctl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type SourcePreparation struct {
	KeyFile            string
	Registration       sourcefacts.SourceCredentialRegistration
	RegistrationSHA256 string
}

func ManagedCollectionID(installation, enrollment, project, kind string) string {
	return connectorcontrol.ManagedCollectionID(installation, enrollment, project, kind)
}
func validSourcePreparationProfiles(kind string, profiles []string) bool {
	if kind == "content" {
		return slices.Equal(profiles, []string{sourcefacts.ContentProfile})
	}
	if kind != "facts" || len(profiles) == 0 || len(profiles) > 12 || !slices.IsSorted(profiles) {
		return false
	}
	for i, p := range profiles {
		if i > 0 && p == profiles[i-1] {
			return false
		}
		if !sourcefacts.ValidProfile(p) && !sourcefacts.ValidObservationProfile(p) && !sourcefacts.ValidBudgetProfiles([]string{p}) {
			return false
		}
	}
	return true
}
func (e *Engine) PrepareSourceCredential(ctx context.Context, installation, enrollment, kind, source, org, project string, profiles []string) (SourcePreparation, error) {
	_ = ctx
	var out SourcePreparation
	if requireMutationAuthority(e.Paths) != nil || !connectorcontrol.IsInstallationID(installation) || !regexp.MustCompile(`^xise_[1-9][0-9]{0,18}$`).MatchString(enrollment) || !regexpPositiveID(org) || !regexpPositiveID(project) || source == "" || len(source) > 128 || strings.ContainsAny(source, "\r\n\x00") || !validSourcePreparationProfiles(kind, profiles) {
		return out, sourcefacts.ErrInvalid
	}
	path := filepath.Join(e.Paths.EtcDir, "connector-machines", installation, "sources", "source-managed-"+ManagedCollectionID(installation, enrollment, project, kind)+".key")
	err := e.withOperationLock(func() error {
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		if err := e.validateReferenceParent(path); err != nil {
			return err
		}
		present, err := fileExists(path)
		if err != nil {
			return err
		}
		if !present {
			secret := make([]byte, 32)
			if _, err = io.ReadFull(e.feedCredentialRandomReader(), secret); err != nil {
				return fmt.Errorf("source random source unavailable")
			}
			token := "ur-xf1-" + hex.EncodeToString(secret)
			clear(secret)
			if err = e.writeConnectorEnvironmentExclusive(path, []byte(token+"\n")); err != nil {
				return err
			}
		}
		if err = e.validateRootReference(path, 0600); err != nil {
			return err
		}
		token, err := config.LoadOwnerOnlySecretFile(path)
		if err != nil || !feedCredentialTokenPattern.MatchString(token) {
			return sourcefacts.ErrAuthorization
		}
		operation := sha256.Sum256([]byte("xinsight.managed-source-preparation.v1\x00" + installation + "\x00" + enrollment + "\x00" + project + "\x00" + kind))
		registration := sourcefacts.SourceCredentialRegistration{SchemaVersion: sourcefacts.SourceCredentialRegistrationVersion, TokenFormat: FeedCredentialTokenFormat, VerifierScheme: FeedCredentialVerifierScheme, KeyPrefix: token[:20], VerifierSHA256: feedCredentialVerifier(token), OperationID: hex.EncodeToString(operation[:16]), SourceInstance: source, OrgID: org, ProjectID: project, Profiles: append([]string(nil), profiles...)}
		raw, err := json.Marshal(registration)
		if err != nil {
			return err
		}
		state := path + ".prepared.json"
		if exists, err := fileExists(state); err != nil {
			return err
		} else if exists {
			if err = e.validateRootReference(state, 0600); err != nil {
				return err
			}
			prior, err := os.ReadFile(state)
			if err != nil || string(prior) != string(raw) {
				return fmt.Errorf("source scope requires explicit migration; credential is immutable")
			}
		}
		if err = e.publishReference(state, raw, 0600); err != nil {
			return err
		}
		out = SourcePreparation{KeyFile: path, Registration: registration, RegistrationSHA256: sourcefacts.Hash(raw)}
		return nil
	})
	return out, err
}

// Each pass publishes all changed project configurations, then reloads and
// refreshes only the source worker once. Original indexed/server copies remain.
func (e *Engine) PublishManagedProjects(ctx context.Context, changes map[string]sourcefacts.ManagedConfig) error {
	if requireMutationAuthority(e.Paths) != nil {
		return connectorcontrol.ErrInvalid
	}
	if len(changes) == 0 {
		return nil
	}
	return e.withOperationLock(func() error {
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		dir := sourcefacts.ManagedPartitionsDirectory(e.Paths.SourceFactsConfig)
		if err := e.validateReferenceParent(filepath.Join(dir, "probe")); err != nil {
			return err
		}
		for name, cfg := range changes {
			if !sourcefacts.ValidManagedPartitionName(name) || cfg.Validate() != nil {
				return sourcefacts.ErrInvalid
			}
			for _, path := range cfg.CredentialFiles() {
				if _, err := sourcefacts.ManagedCredentialName(path); err != nil {
					return err
				}
				if err := e.validateRootReference(path, 0600); err != nil {
					return err
				}
				if _, err := config.LoadSourceFactsServiceKey(path); err != nil {
					return err
				}
			}
		}
		if err := ensureDirectory(dir, 0755); err != nil {
			return err
		}
		for name, cfg := range changes {
			raw, err := json.Marshal(cfg)
			if err != nil {
				return err
			}
			path := filepath.Join(dir, name)
			if prior, err := os.ReadFile(path); err == nil && string(prior) == string(raw) {
				continue
			} else if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err = e.publishReference(path, raw, 0644); err != nil {
				return err
			}
		}
		partitions, err := e.loadManagedProjectPartitions()
		if err != nil {
			return err
		}
		body := "[Service]\n"
		machine, _, _, err := e.ReadURMachineConfig()
		if err != nil {
			return err
		}
		if machine.CAFile != "" {
			body += "LoadCredential=source-managed-ca.pem:" + machine.CAFile + "\nEnvironment=XINSIGHT_SOURCE_MANAGED_CA_FILE=/run/credentials/xinsight-source-facts.service/source-managed-ca.pem\n"
		}
		seen := map[string]string{}
		for _, cfg := range partitions {
			for _, path := range cfg.CredentialFiles() {
				name, err := sourcefacts.ManagedCredentialName(path)
				if err != nil {
					return err
				}
				if old, ok := seen[name]; ok {
					if old != path {
						return sourcefacts.ErrConflict
					}
					continue
				}
				seen[name] = path
				body += "LoadCredential=" + name + ":" + path + "\n"
			}
		}
		dropin := filepath.Join(e.Paths.SourceFactsServiceUnit+".d", "21-managed-source-credentials.conf")
		if prior, err := os.ReadFile(dropin); err == nil && string(prior) == body {
		} else {
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err = e.publishReference(dropin, []byte(body), 0644); err != nil {
				return err
			}
		}
		// A nonempty pass is pending DB authority until actual refresh succeeds.
		// Equal files may be recovery from a failed reload/restart; refresh them.
		if err = runSystemctl(ctx, e.Runner, "daemon-reload"); err != nil {
			return err
		}
		if err = runSystemctl(ctx, e.Runner, "enable", sourceFactsWorkerUnit); err != nil {
			return err
		}
		if err = runSystemctl(ctx, e.Runner, "restart", sourceFactsWorkerUnit); err != nil {
			return err
		}
		state, err := e.inspectManagedWorkerState(ctx, sourceFactsWorkerUnit, e.Paths.SourceFactsServiceUnit, "source facts worker")
		if err != nil {
			return err
		}
		if !state.present || !state.active || !state.enabled {
			return fmt.Errorf("managed source worker refresh is not active and enabled")
		}
		return nil
	})
}

func (e *Engine) loadManagedProjectPartitions() ([]sourcefacts.ManagedConfig, error) {
	if !e.Paths.IsTestRoot {
		return sourcefacts.LoadManagedPartitions(e.Paths.SourceFactsConfig)
	}
	dir := sourcefacts.ManagedPartitionsDirectory(e.Paths.SourceFactsConfig)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []sourcefacts.ManagedConfig
	for _, entry := range entries {
		if !sourcefacts.ValidManagedPartitionName(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err = e.validateRootReference(path, 0644); err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		cfg, err := sourcefacts.ParseManagedConfig(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	return out, nil
}

// ReadOriginalSourceConfig preserves the original worker's indexed custody and
// collections. A migration may qualify them, but cannot prepare replacements.
func (e *Engine) ReadOriginalSourceConfig(ctx context.Context) (sourcefacts.ManagedConfig, bool, error) {
	return e.readOriginalSourceConfig(ctx, true)
}

// InspectOriginalSourceConfig validates the original indexed config and keys
// without starting a paused consumer or asserting that it is receiving.
func (e *Engine) InspectOriginalSourceConfig(ctx context.Context) (sourcefacts.ManagedConfig, bool, error) {
	return e.readOriginalSourceConfig(ctx, false)
}

func (e *Engine) readOriginalSourceConfig(ctx context.Context, requireRunning bool) (sourcefacts.ManagedConfig, bool, error) {
	var cfg sourcefacts.ManagedConfig
	present := false
	err := e.withOperationLock(func() error {
		exists, err := fileExists(e.Paths.SourceFactsConfig)
		if err != nil || !exists {
			return err
		}
		if err = e.validateRootReference(e.Paths.SourceFactsConfig, 0600, 0644); err != nil {
			return err
		}
		raw, err := os.ReadFile(e.Paths.SourceFactsConfig)
		if err != nil {
			return err
		}
		cfg, err = sourcefacts.ParseManagedConfig(raw)
		if err != nil {
			return err
		}
		for _, path := range cfg.CredentialFiles() {
			if err = e.validateRootReference(path, 0600); err != nil {
				return err
			}
			if _, err = config.LoadSourceFactsServiceKey(path); err != nil {
				return err
			}
		}
		if !requireRunning {
			present = true
			return nil
		}
		state, err := e.inspectManagedWorkerState(ctx, sourceFactsWorkerUnit, e.Paths.SourceFactsServiceUnit, "original source worker")
		if err != nil {
			return err
		}
		if !state.present || !state.active || !state.enabled {
			return fmt.Errorf("original source worker is not active and enabled")
		}
		present = true
		return nil
	})
	return cfg, present, err
}

// Only the managed flow calls this after requalifying a current explicit scope.
// It prepares the existing consumer while restored DB collections remain paused;
// it neither acknowledges a restore nor changes collection authority.
func (e *Engine) EnsureOriginalSourceWorker(ctx context.Context, expected sourcefacts.ManagedConfig) error {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return err
	}
	if expected.Validate() != nil {
		return sourcefacts.ErrInvalid
	}
	return e.withOperationLock(func() error {
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		if err := e.validateRootReference(e.Paths.SourceFactsConfig, 0600, 0644); err != nil {
			return err
		}
		raw, err := os.ReadFile(e.Paths.SourceFactsConfig)
		if err != nil {
			return err
		}
		current, err := sourcefacts.ParseManagedConfig(raw)
		if err != nil {
			return err
		}
		before, _ := json.Marshal(expected)
		after, _ := json.Marshal(current)
		if !slices.Equal(before, after) {
			return sourcefacts.ErrScopeChanged
		}
		for _, path := range current.CredentialFiles() {
			if err := e.validateRootReference(path, 0600); err != nil {
				return err
			}
			if _, err := config.LoadSourceFactsServiceKey(path); err != nil {
				return err
			}
		}
		state, err := e.inspectManagedWorkerState(ctx, sourceFactsWorkerUnit, e.Paths.SourceFactsServiceUnit, "original source worker")
		if err != nil {
			return err
		}
		if !state.present {
			return fmt.Errorf("original source worker is not installed")
		}
		if state.active && state.enabled {
			return nil
		}
		if err := e.installSourceFactsCredentials(current); err != nil {
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
		state, err = e.inspectManagedWorkerState(ctx, sourceFactsWorkerUnit, e.Paths.SourceFactsServiceUnit, "original source worker")
		if err != nil {
			return err
		}
		if !state.present || !state.active || !state.enabled {
			return fmt.Errorf("original source worker readiness pending")
		}
		return nil
	})
}
