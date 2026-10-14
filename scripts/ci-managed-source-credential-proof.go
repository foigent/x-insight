//go:build ignore

// An isolated Linux qualification probe over the production file/credential
// loaders. It never makes a network call or returns key contents/digests.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--check-loaded" || os.Args[1] == "--wait-loaded") {
		if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("XINSIGHT_CI_EPHEMERAL_HOST") != "1" || os.Geteuid() != 0 || os.Getenv("XINSIGHTCTL_TEST_ROOT") != "" {
			panic("isolated root loaded-key check required")
		}
		paths, err := hostctl.PathsFromEnvironment()
		if err != nil || paths.IsTestRoot {
			panic("real managed paths required")
		}
		e, err := hostctl.NewEngine(paths)
		if err != nil {
			fmt.Fprintln(os.Stderr, loadedProofFailure(os.Args[1], errors.New("CI loaded proof engine unavailable"), nil))
			os.Exit(1)
		}
		var waitContextErr error
		if os.Args[1] == "--wait-loaded" {
			// Type=simple may still be executing systemd's startup child. Wait only
			// for the original read-only current-consumer proof, never restart/load.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err = waitLoadedConsumers(ctx, e.ReviewBodyLoaded)
			waitContextErr = ctx.Err()
		} else {
			// Keep the post-probe check single-shot: never wait away stale evidence.
			err = e.ReviewBodyLoaded(context.Background())
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, loadedProofFailure(os.Args[1], err, waitContextErr))
			os.Exit(1)
		}
		fmt.Println("actual_reader_worker_original_custody=loaded_current_pid_and_invocation")
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "--check-config" {
		for _, path := range os.Args[2:] {
			raw, err := os.ReadFile(path)
			if err != nil {
				panic(err)
			}
			if _, err = sourcefacts.ParseManagedConfig(raw); err != nil {
				panic(err)
			}
		}
		fmt.Println("managed qualification configuration=verified_offline")
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "--publish-partition" {
		if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("XINSIGHT_CI_EPHEMERAL_HOST") != "1" || os.Geteuid() != 0 || os.Getenv("XINSIGHTCTL_TEST_ROOT") != "" {
			panic("isolated real Linux host publisher required")
		}
		path := os.Args[2]
		if !filepath.IsAbs(path) || filepath.Base(path) != "partition.json" {
			panic("exact public partition input required")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			panic("public partition input unavailable")
		}
		cfg, err := sourcefacts.ParseManagedConfig(raw)
		if err != nil {
			panic(err)
		}
		paths, err := hostctl.PathsFromEnvironment()
		if err != nil || paths.IsTestRoot {
			panic("real managed paths required")
		}
		engine, err := hostctl.NewEngine(paths)
		if err != nil {
			panic(err)
		}
		if err = engine.PublishManagedProjects(context.Background(), map[string]sourcefacts.ManagedConfig{"xci_AAAAAAAAAAAAAAAAAAAAAA-xise_1-project-87.json": cfg}); err != nil {
			panic(err)
		}
		fmt.Println("actual_managed_source_publisher=refreshed")
		return
	}
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("XINSIGHT_CI_EPHEMERAL_HOST") != "1" || os.Geteuid() == 0 {
		panic("credential proof requires isolated systemd non-root service")
	}
	dir := os.Getenv(config.CredentialsDirEnv)
	if !filepath.IsAbs(dir) {
		panic("missing actual systemd credentials directory")
	}
	if len(os.Args) != 2 || (os.Args[1] != "--worker" && os.Args[1] != "--reader") {
		panic("exact actual service consumer required")
	}
	consumer := "xinsight-source-facts.service"
	if os.Args[1] == "--reader" {
		consumer = "xinsight.service"
	}
	if dir != "/run/credentials/"+consumer {
		panic("actual consumer credential directory required")
	}
	keyPath := filepath.Join(dir, "review-body.key")
	keyMetadata := reviewKeyMetadata(keyPath)
	if _, err := config.LoadReviewBodyKeyFile(keyPath); err != nil {
		// Only metadata from this actual consumer context survives rejection.
		writeProof(map[string]any{"schema_version": "xinsight.ci-managed-credential-proof.v1", "classification": "ci_linux_synthetic_no_source_call", "consumer": consumer, "uid": os.Geteuid(), "review_body_key": "rejected", "review_body_credential": keyMetadata})
		panic(err)
	}
	if _, err := config.LoadPostgresDSNFile(filepath.Join(dir, config.WorkspaceCredential)); err != nil {
		panic("actual workspace credential rejected")
	}
	rootKey := os.Getenv("XINSIGHT_REVIEW_BODY_SOURCE_KEY_FILE")
	if rootKey != "/etc/x-insight/review-body.key" {
		panic("automatic original root binding required")
	}
	for _, root := range []string{rootKey, "/etc/x-insight/database/current/postgres-workspace.dsn"} {
		if _, err := os.ReadFile(root); err == nil {
			panic("consumer read root custody credential directly")
		}
	}
	proof := map[string]any{"schema_version": "xinsight.ci-managed-credential-proof.v1", "classification": "ci_linux_synthetic_no_source_call", "consumer": consumer, "uid": os.Geteuid(), "review_body_key": "loaded", "review_body_credential": keyMetadata, "workspace_credential": "loaded", "direct_root_key_access": "rejected", "systemd_credentials": "loaded"}
	if os.Args[1] == "--reader" {
		writeProof(proof)
		return
	}
	base, err := sourcefacts.LoadManagedConfig("/etc/x-insight/source-facts.json")
	if err != nil {
		panic(err)
	}
	partitions, err := sourcefacts.LoadManagedPartitions("/etc/x-insight/source-facts.json")
	if err != nil || len(partitions) != 1 {
		panic("expected exact root-owned managed partition")
	}
	baseKeys := base.CredentialFiles()
	for i, path := range baseKeys {
		if _, err = config.LoadSourceFactsServiceKey(filepath.Join(dir, fmt.Sprintf("source-profile-%d.key", i))); err != nil {
			panic(err)
		}
		// Non-root worker must not be able to read the original root secret.
		if _, err = os.ReadFile(path); err == nil {
			panic("worker read root source credential directly")
		}
	}
	count := 0
	for _, c := range partitions {
		for _, path := range c.CredentialFiles() {
			name, err := sourcefacts.ManagedCredentialName(path)
			if err != nil {
				panic(err)
			}
			if _, err = config.LoadSourceFactsServiceKey(filepath.Join(dir, name)); err != nil {
				panic(err)
			}
			if _, err = os.ReadFile(path); err == nil {
				panic("worker read root managed credential directly")
			}
			count++
		}
	}
	proof["root_owned_config"], proof["original_indexed_keys"], proof["managed_partition_keys"] = true, len(baseKeys), count
	writeProof(proof)
}

// Project only fixed errors emitted by the original loaded-proof checker.
// Unknown errors, including their paths or sensitive payloads, never survive.
func loadedProofFailure(mode string, err, contextErr error) string {
	stage, termination, reason := "check", "failed", "unknown"
	if mode == "--wait-loaded" {
		stage = "wait"
	}
	if errors.Is(contextErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		termination = "deadline"
	} else if errors.Is(contextErr, context.Canceled) || errors.Is(err, context.Canceled) {
		termination = "cancelled"
	}
	if err != nil {
		reason = map[string]string{
			"body consumer is not active":               "inactive",
			"body consumer is not running":              "not_running",
			"body consumer proof is stale":              "stale_proof",
			"body consumer loaded a different key":      "key_mismatch",
			"reader body capability pending":            "reader_pending",
			"source body capability pending":            "worker_pending",
			"body consumer restarted during readback":   "restarted",
			"restore_original_review_body_key_required": "binding_missing",
			"CI loaded proof engine unavailable":        "engine_unavailable",
		}[err.Error()]
		if reason == "" {
			reason = "unknown"
		}
	}
	return fmt.Sprintf("actual_loaded_custody_proof=failed stage=%s termination=%s reason=%s", stage, termination, reason)
}

// The existing host activation boundary uses 500ms intervals within 10s.
// This CI helper waits for that same asynchronous proof without host mutation.
func waitLoadedConsumers(ctx context.Context, check func(context.Context) error) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = check(ctx)
		if lastErr == nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return lastErr
		case <-ticker.C:
		}
	}
}

// Permission metadata only, collected inside the actual unit. Successful
// strict loading supplies the final authority; a candidate form is not proof.
func reviewKeyMetadata(path string) map[string]any {
	result := map[string]any{"form": "unavailable"}
	info, err := os.Lstat(path)
	if err != nil {
		return result
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return result
	}
	result["regular"], result["mode"], result["uid"], result["gid"], result["links"] = info.Mode().IsRegular(), fmt.Sprintf("%04o", info.Mode().Perm()), stat.Uid, stat.Gid, stat.Nlink
	var fs syscall.Statfs_t
	if syscall.Statfs(path, &fs) == nil {
		result["read_only"] = uint64(fs.Flags)&1 != 0 // Linux ST_RDONLY.
	}
	form := "unsupported"
	if info.Mode().Perm() == 0440 && stat.Uid == 0 && stat.Gid == 0 {
		form = "root_0440_acl_candidate"
	} else if info.Mode().Perm() == 0400 && stat.Uid == uint32(os.Geteuid()) && stat.Gid == 0 {
		form = "service_owned_0400_native_candidate"
	}
	result["form"] = form
	return result
}

// The root orchestrator owns capture and projection; consumers need no writable
// host path. These maps contain only fixed verdicts and permission metadata.
func writeProof(proof map[string]any) {
	if err := json.NewEncoder(os.Stdout).Encode(proof); err != nil {
		panic("consumer receipt write failed")
	}
}
