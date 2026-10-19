package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/integration/reviewcopy"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

// The existing importer has a bounded, restartable source-profile entrypoint.
// It does not register scopes, generate credentials or reset retained cursors.
func runSourceFacts(args []string) error { return runManagedSourceFacts(args, false) }

func runManagedSourceFacts(args []string, continuous bool) error {
	fs := flag.NewFlagSet("unirouter pull-source-facts", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("stream-id", "", "explicit existing collection registration")
	contentID := fs.String("content-collection-id", "", "explicit existing content collection")
	budgetID := fs.String("collection-id", "", "explicit existing budget snapshot collection")
	endpoint := fs.String("endpoint", "", "exact Source Facts endpoint")
	keyFile := fs.String("key-file", "", "explicit source-only credential file")
	allowHTTP := fs.Bool("allow-private-tunnel-http", false, "explicit private tunnel HTTP")
	configPath := fs.String("config", "/etc/x-insight/source-facts.json", "administrator-owned managed configuration")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("invalid source facts arguments")
	}
	if fs.NArg() != 0 || (!continuous && *id == "" && *budgetID == "" && *contentID == "") || ((*id != "" && *budgetID != "") || (*contentID != "" && (*id != "" || *budgetID != ""))) || (continuous && (*id != "" || *budgetID != "" || *contentID != "" || *endpoint != "" || *keyFile != "" || *allowHTTP)) {
		return fmt.Errorf("select one configured stream for a single step, or the complete configuration for the managed worker")
	}
	basePresent := true
	if _, err := os.Lstat(*configPath); continuous && os.IsNotExist(err) {
		basePresent = false
	}
	configs := []sourcefacts.ManagedConfig{}
	if basePresent {
		cfg, err := sourcefacts.LoadManagedConfig(*configPath)
		if err != nil {
			return err
		}
		configs = append(configs, cfg)
	}
	if continuous {
		extra, err := sourcefacts.LoadManagedPartitions(*configPath)
		if err != nil {
			return err
		}
		configs = append(configs, extra...)
	}
	if len(configs) == 0 {
		return sourcefacts.ErrInvalid
	}
	dsn, err := config.LoadImporterPostgresDSN()
	if err != nil {
		return err
	}
	store, err := db.OpenSourceFactsStore(dsn)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reviewKeys, err := reviewbody.LoadKeyRingFromFile(os.Getenv("XINSIGHT_REVIEW_BODY_KEY_ID"), os.Getenv("XINSIGHT_REVIEW_BODY_KEY_FILE"))
	if err != nil {
		return err
	}
	var reviewStore *db.WorkspaceStore
	if reviewKeys.ActiveID != "" {
		workspaceDSN, err := config.LoadWorkspacePostgresDSN()
		if err != nil {
			return err
		}
		reviewStore, err = db.OpenWorkspaceStore(ctx, workspaceDSN)
		if err != nil {
			return err
		}
		defer reviewStore.Close()
		if err = reviewKeys.WriteWorkerLoadedProof(); err != nil {
			return err
		}
	}
	var profiles []sourcefacts.ManagedProfile
	var budgets []sourcefacts.BudgetConsumer
	var contents []sourcefacts.ContentConsumer
	roots, err := config.LoadManagedSourceCAPool(os.Getenv(config.ManagedSourceCAEnv))
	if err != nil {
		return err
	}
	workers := []sourcefacts.ManagedWorker{}
	for cfgIndex, cfg := range configs {
		profiles = make([]sourcefacts.ManagedProfile, 0, len(cfg.Profiles))
		for i, p := range cfg.Profiles {
			if !continuous && p.StreamID != *id {
				continue
			}
			// Historical one-step flags remain assertions, never configuration overrides.
			if (*endpoint != "" && *endpoint != p.Endpoint) || (*keyFile != "" && *keyFile != p.KeyFile) || (*allowHTTP && !p.AllowPrivateTunnelHTTP) {
				return sourcefacts.ErrScopeChanged
			}
			keyPath := p.KeyFile
			if dir := os.Getenv(config.CredentialsDirEnv); dir != "" {
				if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
					return sourcefacts.ErrAuthorization
				}
				if cfgIndex == 0 && basePresent {
					keyPath = filepath.Join(dir, fmt.Sprintf("source-profile-%d.key", i))
				} else {
					name, err := sourcefacts.ManagedCredentialName(p.KeyFile)
					if err != nil {
						return err
					}
					keyPath = filepath.Join(dir, name)
				}
			}
			key, err := config.LoadSourceFactsServiceKey(keyPath)
			if err != nil {
				return err
			}
			reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{SyncRequired: true, Endpoint: p.Endpoint, ServiceKey: key, TLSRootCAs: roots, AllowPrivateTunnelHTTP: p.AllowPrivateTunnelHTTP, RequestTimeout: 5 * time.Second})
			if err != nil {
				return err
			}
			expected := p.Registration()
			profiles = append(profiles, sourcefacts.ManagedProfile{StreamID: p.StreamID, Consumer: sourcefacts.Consumer{Store: store, Reader: reader, ExpectedStream: &expected}, CollectCurrentRuntime: p.Profile == sourcefacts.RuntimeProfile})
		}
		budgets = make([]sourcefacts.BudgetConsumer, 0, len(cfg.Budgets))
		for i, p := range cfg.Budgets {
			if !continuous && p.CollectionID != *budgetID {
				continue
			}
			if (*endpoint != "" && *endpoint != p.Endpoint) || (*keyFile != "" && *keyFile != p.KeyFile) || (*allowHTTP && !p.AllowPrivateTunnelHTTP) {
				return sourcefacts.ErrScopeChanged
			}
			keyPath := p.KeyFile
			if dir := os.Getenv(config.CredentialsDirEnv); dir != "" {
				if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
					return sourcefacts.ErrAuthorization
				}
				if cfgIndex == 0 && basePresent {
					keyPath = filepath.Join(dir, fmt.Sprintf("source-profile-%d.key", len(cfg.Profiles)+i))
				} else {
					name, err := sourcefacts.ManagedCredentialName(p.KeyFile)
					if err != nil {
						return err
					}
					keyPath = filepath.Join(dir, name)
				}
			}
			key, err := config.LoadSourceFactsServiceKey(keyPath)
			if err != nil {
				return err
			}
			reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{SyncRequired: true, Endpoint: p.Endpoint, ServiceKey: key, TLSRootCAs: roots, AllowPrivateTunnelHTTP: p.AllowPrivateTunnelHTTP, RequestTimeout: 5 * time.Second})
			if err != nil {
				return err
			}
			budgets = append(budgets, sourcefacts.BudgetConsumer{Store: store, Reader: reader, Expected: p.Registration()})
		}

		contents = make([]sourcefacts.ContentConsumer, 0, len(cfg.Contents))
		for i, configured := range cfg.Contents {
			if !continuous && configured.CollectionID != *contentID {
				continue
			}
			p, err := cfg.ResolveContent(configured)
			if err != nil {
				return err
			}
			if (*endpoint != "" && *endpoint != p.Endpoint) || (*keyFile != "" && *keyFile != p.KeyFile) || (*allowHTTP && !p.AllowPrivateTunnelHTTP) {
				return sourcefacts.ErrScopeChanged
			}
			keyPath := p.KeyFile
			if dir := os.Getenv(config.CredentialsDirEnv); dir != "" {
				if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
					return sourcefacts.ErrAuthorization
				}
				if cfgIndex == 0 && basePresent {
					keyPath = filepath.Join(dir, fmt.Sprintf("source-profile-%d.key", len(cfg.Profiles)+len(cfg.Budgets)+i))
				} else {
					name, err := sourcefacts.ManagedCredentialName(p.KeyFile)
					if err != nil {
						return err
					}
					keyPath = filepath.Join(dir, name)
				}
			}
			key, err := config.LoadSourceFactsServiceKey(keyPath)
			if err != nil {
				return err
			}
			reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{SyncRequired: true, Endpoint: p.Endpoint, ServiceKey: key, TLSRootCAs: roots, AllowPrivateTunnelHTTP: p.AllowPrivateTunnelHTTP, RequestTimeout: 5 * time.Second})
			if err != nil {
				return err
			}
			consumer := sourcefacts.ContentConsumer{Store: store, Reader: reader, Expected: p.Registration()}
			if reviewStore != nil {
				consumer.AfterCommit = func(ctx context.Context, current sourcefacts.ContentCollection, limits sourcefacts.ContentLimits) error {
					return reviewcopy.PollURCCopies(ctx, reader, reviewStore, current.ID, limits, reviewKeys, time.Now)
				}
			}
			contents = append(contents, consumer)
		}
		workers = append(workers, sourcefacts.ManagedWorker{ConfigVersion: cfg.SchemaVersion, Profiles: profiles, Budgets: budgets, Contents: contents, PollInterval: time.Duration(cfg.PollIntervalSeconds) * time.Second, RetryMaximum: time.Duration(cfg.RetryMaximumSeconds) * time.Second})
	}
	if len(profiles)+len(budgets)+len(contents) == 0 {
		return sourcefacts.ErrInvalid
	}
	if continuous {
		last := map[string]string{}
		worker := workers[0]
		worker.Partitions = workers[1:]
		worker.Observe = func(id string, err error) {
			state := "persisted"
			if err != nil {
				state = "waiting_or_failed"
			}
			if last[id] != state {
				_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"stream_id": id, "state": state})
				last[id] = state
			}
		}
		err := worker.Run(ctx)
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	if len(contents) > 0 {
		more, err := contents[0].PullOnce(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "persisted", "has_more": more})
	}
	if len(budgets) > 0 {
		more, err := budgets[0].PullOnce(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "persisted", "has_more": more})
	}
	page, err := profiles[0].Consumer.PullOnce(ctx, profiles[0].StreamID)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "persisted", "documents": len(page.Items) + len(page.Observations), "has_more": page.HasMore})
}

func runOfflineSourceFacts(args []string) error {
	fs := flag.NewFlagSet("unirouter import-source-facts", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("authorization-file", "", "root-owned current offline authorization")
	target := fs.String("target-ref", "", "exact target reference")
	check := fs.Bool("check", false, "validate offline input and current authorization without importing")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *file == "" || *target == "" {
		return sourcefacts.ErrInvalid
	}
	product := buildinfo.Current()
	now := time.Now().UTC()
	a, err := sourcefacts.LoadOfflineAuthorization(*file, *target, product.ProductVersion, product.Commit, now)
	if err != nil {
		return err
	}
	raw, err := sourcefacts.ReadOwnedFile(a.BundlePath, sourcefacts.MaxPageBytes-1, 0)
	if err != nil {
		return err
	}
	batch, err := sourcefacts.ValidateOfflineBatch(raw, a, now)
	if err != nil {
		return err
	}
	dsn, err := config.LoadImporterPostgresDSN()
	if err != nil {
		return err
	}
	store, err := db.OpenSourceFactsStore(dsn)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *check {
		if err := store.CheckSourceFactsOffline(ctx, a, raw, now); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "checked", "operation_id": a.OperationID, "documents": batch.Count()})
	}
	receipt, err := store.ImportSourceFactsOffline(ctx, a, raw, now)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(receipt)
}

func runOfflineBudgets(args []string) error {
	fs := flag.NewFlagSet("unirouter import-budgets", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("authorization-file", "", "root-owned current offline authorization")
	target := fs.String("target-ref", "", "exact target reference")
	check := fs.Bool("check", false, "validate offline input and current authorization without importing")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *file == "" || *target == "" {
		return sourcefacts.ErrInvalid
	}
	product := buildinfo.Current()
	now := time.Now().UTC()
	a, err := sourcefacts.LoadBudgetOfflineAuthorization(*file, *target, product.ProductVersion, product.Commit, now)
	if err != nil {
		return err
	}
	raw, err := sourcefacts.ReadOwnedFile(a.BundlePath, sourcefacts.MaxBudgetOfflineBytes, 0)
	if err != nil {
		return err
	}
	batch, err := sourcefacts.ValidateBudgetOfflineBundle(raw, a, now)
	if err != nil {
		return err
	}
	dsn, err := config.LoadImporterPostgresDSN()
	if err != nil {
		return err
	}
	store, err := db.OpenSourceFactsStore(dsn)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *check {
		if err := store.CheckBudgetOffline(ctx, a, raw, now); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "checked", "collection_id": a.CollectionID, "source_readings": len(batch.Readings)})
	}
	receipt, err := store.ImportBudgetOffline(ctx, a, raw, now)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(receipt)
}
func runSyntheticResources(args []string) error {
	fs := flag.NewFlagSet("import-synthetic-resources", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("authorization-file", "", "root-owned current offline authorization")
	target := fs.String("target-ref", "", "exact target reference")
	check := fs.Bool("check", false, "validate offline input and current authorization without importing")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *file == "" || *target == "" {
		return sourcefacts.ErrInvalid
	}
	product := buildinfo.Current()
	now := time.Now().UTC()
	a, err := sourcefacts.LoadOfflineAuthorization(*file, *target, product.ProductVersion, product.Commit, now)
	if err != nil {
		return err
	}
	raw, err := sourcefacts.ReadOwnedFile(a.BundlePath, sourcefacts.MaxPageBytes-1, 0)
	if err != nil {
		return err
	}
	if a.SchemaVersion != sourcefacts.ResourceOfflineAuthorizationVersion {
		return sourcefacts.ErrAuthorization
	}
	dsn, err := config.LoadImporterPostgresDSN()
	if err != nil {
		return err
	}
	store, err := db.OpenSourceFactsStore(dsn)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *check {
		count, err := store.CheckSyntheticResources(ctx, a, raw, now)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "checked", "operation_id": a.OperationID, "documents": count})
	}
	receipt, err := store.ImportSyntheticResources(ctx, a, raw, now)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(receipt)
}

func runResourceActions(args []string) error {
	fs := flag.NewFlagSet("import-resource-actions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("authorization-file", "", "root-owned current offline authorization")
	target := fs.String("target-ref", "", "exact target reference")
	check := fs.Bool("check", false, "validate offline input and current authorization without importing")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *file == "" || *target == "" {
		return sourcefacts.ErrInvalid
	}
	product := buildinfo.Current()
	now := time.Now().UTC()
	a, err := sourcefacts.LoadOfflineAuthorization(*file, *target, product.ProductVersion, product.Commit, now)
	if err != nil {
		return err
	}
	raw, err := sourcefacts.ReadOwnedFile(a.BundlePath, sourcefacts.MaxPageBytes-1, 0)
	if err != nil {
		return err
	}
	if a.SchemaVersion != sourcefacts.ResourceOfflineAuthorizationVersion || a.Profile != sourcefacts.ActionOfflineProfile {
		return sourcefacts.ErrAuthorization
	}
	dsn, err := config.LoadImporterPostgresDSN()
	if err != nil {
		return err
	}
	store, err := db.OpenSourceFactsStore(dsn)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *check {
		count, err := store.CheckResourceActions(ctx, a, raw, now)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "checked", "operation_id": a.OperationID, "documents": count})
	}
	receipt, err := store.ImportResourceActions(ctx, a, raw, now)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(receipt)
}
