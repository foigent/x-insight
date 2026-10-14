package hostctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/config"
)

const maxHTTPProofBytes = 1 << 20

const maxEnvironmentFileBytes = 64 << 10

const (
	legacyPhase1ServiceDropInName = "10-release-layout.conf"
	managedServiceDropInName      = "99-xinsight-managed-launcher.conf"
	legacyPhase1ServiceDropIn     = "[Service]\nExecStart=\nExecStart=/opt/x-insight/current/bin/xinsight-server serve 127.0.0.1:8080\n"
	managedServiceDropIn          = "[Service]\nExecStart=\nExecStart=/opt/x-insight/control/xinsight-launcher\n"
)

const (
	activationProofAttempts = 21
	activationProofInterval = 500 * time.Millisecond
)

var errLoopbackListenerNotFound = errors.New("X-Insight loopback listener was not found")

type HostFacts struct {
	OS             string `json:"os"`
	Architecture   string `json:"architecture"`
	DistributionID string `json:"distribution_id"`
	VersionID      string `json:"version_id"`
}

func detectSupportedHost(paths Paths) (HostFacts, error) {
	if paths.IsTestRoot {
		if os.Getenv("XINSIGHTCTL_TEST_HOST") != "ubuntu-22.04-amd64" {
			return HostFacts{}, fmt.Errorf("test host identity is not explicitly enabled")
		}
		return HostFacts{OS: "linux", Architecture: "amd64", DistributionID: "ubuntu", VersionID: "22.04"}, nil
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return HostFacts{}, fmt.Errorf("supported host requires Linux amd64")
	}
	values, err := readOSRelease("/etc/os-release")
	if err != nil {
		return HostFacts{}, err
	}
	facts := HostFacts{OS: runtime.GOOS, Architecture: runtime.GOARCH, DistributionID: values["ID"], VersionID: values["VERSION_ID"]}
	if facts.DistributionID != "ubuntu" || facts.VersionID != "22.04" {
		return facts, fmt.Errorf("supported host requires Ubuntu 22.04")
	}
	return facts, nil
}

func requireMutationAuthority(paths Paths) error {
	if paths.IsTestRoot {
		return nil
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("mutating xinsightctl commands require root")
	}
	return nil
}

func readOSRelease(path string) (map[string]string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(contents) == 0 || len(contents) > 64<<10 {
		return nil, fmt.Errorf("os-release size is invalid")
	}
	result := make(map[string]string)
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, present := strings.Cut(line, "=")
		if !present {
			continue
		}
		value = strings.Trim(value, `"'`)
		result[key] = value
	}
	return result, nil
}

func availableBytes(path string) (uint64, error) {
	var statistics syscall.Statfs_t
	if err := syscall.Statfs(path, &statistics); err != nil {
		return 0, err
	}
	return uint64(statistics.Bavail) * uint64(statistics.Bsize), nil
}

func copyFileExclusive(source string, destination string, mode os.FileMode, limit int64) error {
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !sourceInfo.Mode().IsRegular() || sourceInfo.Size() <= 0 || sourceInfo.Size() > limit {
		return fmt.Errorf("source file type or size is invalid")
	}
	if err := ensureDirectory(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, limit+1))
	if copyErr != nil || written != sourceInfo.Size() {
		_ = output.Close()
		return fmt.Errorf("copy did not preserve exact source bytes")
	}
	if err := output.Chmod(mode); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(destination))
}

func replaceFilePreservingPrevious(source string, destination string, mode os.FileMode, operationID string, limit int64) error {
	sourceDigest, _, err := fileSHA256(source, limit)
	if err != nil {
		return err
	}
	if exists, err := fileExists(destination); err != nil {
		return err
	} else if exists {
		destinationDigest, _, err := fileSHA256(destination, limit)
		if err != nil {
			return err
		}
		if destinationDigest == sourceDigest {
			return os.Chmod(destination, mode)
		}
		backup := destination + ".before-" + operationID
		if backupExists, err := fileExists(backup); err != nil {
			return err
		} else if !backupExists {
			if err := copyFileExclusive(destination, backup, 0o600, limit); err != nil {
				return err
			}
		}
	}
	if err := ensureDirectory(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	contents, err := os.ReadFile(source)
	if err != nil || int64(len(contents)) > limit {
		return fmt.Errorf("managed file source cannot be read")
	}
	suffix, err := randomToken(8)
	if err != nil {
		return err
	}
	temporary := destination + ".next-" + suffix
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if err := writeSyncClose(file, contents); err != nil {
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(destination))
}

func verifyLoopbackListener(ctx context.Context, runner CommandRunner) error {
	result, err := runner.Run(ctx, Command{Name: "ss", Args: []string{"-ltnH"}})
	if err != nil {
		return err
	}
	found := false
	for _, line := range strings.Split(result.Output, "\n") {
		fields := strings.Fields(line)
		for _, field := range fields {
			if !strings.HasSuffix(field, ":8080") {
				continue
			}
			if field != "127.0.0.1:8080" {
				return fmt.Errorf("X-Insight listener is not restricted to 127.0.0.1:8080")
			}
			found = true
		}
	}
	if !found {
		return errLoopbackListenerNotFound
	}
	return nil
}

func (engine *Engine) waitForLoopbackListener(ctx context.Context) error {
	var lastErr error
	for attempt := 0; attempt < activationProofAttempts; attempt++ {
		lastErr = verifyLoopbackListener(ctx, engine.Runner)
		if lastErr == nil {
			return nil
		}
		if !errors.Is(lastErr, errLoopbackListenerNotFound) {
			return lastErr
		}
		if attempt+1 < activationProofAttempts {
			if err := engine.wait(ctx, activationProofInterval); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("X-Insight listener did not become ready within 10 seconds: %w", lastErr)
}

func (engine *Engine) waitForActivationProof(ctx context.Context, proof func() error) error {
	var lastErr error
	for attempt := 0; attempt < activationProofAttempts; attempt++ {
		lastErr = proof()
		if lastErr == nil {
			return nil
		}
		if attempt+1 < activationProofAttempts {
			if err := engine.wait(ctx, activationProofInterval); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("X-Insight readiness did not stabilize within 10 seconds: %w", lastErr)
}

func proveHTTPReadiness(
	ctx context.Context,
	client *http.Client,
	expected ReleaseIdentity,
	accessProtected bool,
) ([]CheckResult, error) {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	readyBody, err := boundedGET(ctx, client, "http://127.0.0.1:8080/readyz")
	if err != nil {
		return nil, err
	}
	var ready struct {
		Status             string `json:"status"`
		ProductVersion     string `json:"product_version"`
		DatabaseSchema     string `json:"database_schema"`
		PostgresMajor      int    `json:"postgres_major"`
		ManualVersion      string `json:"manual_version"`
		RepresentativeRead string `json:"representative_read"`
	}
	if err := json.Unmarshal(readyBody, &ready); err != nil {
		return nil, fmt.Errorf("readiness response is invalid")
	}
	if ready.Status != "ready" || ready.ProductVersion != expected.ProductVersion || ready.DatabaseSchema != expected.DatabaseSchema || ready.PostgresMajor != 16 || ready.ManualVersion != expected.ManualVersion {
		return nil, fmt.Errorf(
			"readiness identity does not match the activated release: got status=%q product=%q schema=%q postgres=%d manual=%q; expected product=%q schema=%q postgres=16 manual=%q",
			ready.Status,
			ready.ProductVersion,
			ready.DatabaseSchema,
			ready.PostgresMajor,
			ready.ManualVersion,
			expected.ProductVersion,
			expected.DatabaseSchema,
			expected.ManualVersion,
		)
	}
	checks := []CheckResult{{
		Name:   "compatibility_readiness",
		Status: "pass",
		Detail: "version_schema_postgres_manual_match",
	}}
	if accessProtected {
		if ready.RepresentativeRead != "ok" {
			return nil, fmt.Errorf("readiness did not prove a bounded reader query")
		}
		body, status, contentType, err := boundedGETResponse(
			ctx,
			client,
			"http://127.0.0.1:8080/api/v1/executions?limit=1",
		)
		if err != nil {
			return nil, err
		}
		var problem map[string]any
		if status != http.StatusUnauthorized ||
			contentType != "application/problem+json" ||
			json.Unmarshal(body, &problem) != nil ||
			len(problem) != 6 ||
			problem["code"] != "authentication_required" ||
			problem["status"] != float64(http.StatusUnauthorized) {
			return nil, fmt.Errorf("anonymous investigation API boundary is invalid")
		}
		return append(
			checks,
			CheckResult{
				Name:   "representative_read",
				Status: "pass",
				Detail: "readyz_bounded_reader_query_ok",
			},
			CheckResult{
				Name:   "anonymous_access_boundary",
				Status: "pass",
				Detail: "authentication_required_no_evidence",
			},
		), nil
	}
	if _, err := boundedGET(
		ctx,
		client,
		"http://127.0.0.1:8080/api/v1/executions?limit=1",
	); err != nil {
		return nil, err
	}
	return append(checks, CheckResult{
		Name:   "representative_read",
		Status: "pass",
		Detail: "bounded_execution_search_ok",
	}), nil
}

func boundedGET(ctx context.Context, client *http.Client, address string) ([]byte, error) {
	contents, status, _, err := boundedGETResponse(ctx, client, address)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("loopback HTTP proof returned status %d", status)
	}
	return contents, nil
}

func boundedGETResponse(
	ctx context.Context,
	client *http.Client,
	address string,
) ([]byte, int, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, 0, "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, "", fmt.Errorf("loopback HTTP proof failed")
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, maxHTTPProofBytes+1))
	if err != nil || len(contents) > maxHTTPProofBytes {
		return nil, 0, "", fmt.Errorf("loopback HTTP proof response is invalid")
	}
	return contents, response.StatusCode, response.Header.Get("Content-Type"), nil
}

func parseMajorVersion(output string, product string) (int, error) {
	for _, field := range strings.Fields(output) {
		parts := strings.Split(strings.TrimSpace(field), ".")
		if len(parts) == 0 {
			continue
		}
		major, err := strconv.Atoi(parts[0])
		if err == nil && major >= 10 {
			return major, nil
		}
	}
	return 0, fmt.Errorf("cannot determine %s major version", product)
}

func validateManagedEnvironmentFile(path string) error {
	values, err := readEnvironmentFile(path, false)
	if err != nil {
		return err
	}
	if len(values) != 1 || values["XINSIGHT_MIGRATIONS_DIR"] != "/opt/x-insight/current/migrations" {
		return fmt.Errorf("managed environment file does not match config schema xinsight.config.v1")
	}
	return nil
}

func validateLegacyEnvironmentFile(path string) error {
	values, err := readEnvironmentFile(path, true)
	if err != nil {
		return err
	}
	if len(values) != 2 || values["XINSIGHT_MIGRATIONS_DIR"] != "/opt/x-insight/current/migrations" || values[config.PostgresDSNEnv] == "" {
		return fmt.Errorf("legacy environment file does not match the allowlisted config layout")
	}
	if _, err := config.ValidatePostgresDSN(values[config.PostgresDSNEnv]); err != nil {
		return fmt.Errorf("legacy environment database credential is invalid")
	}
	return nil
}

func readEnvironmentFile(path string, secret bool) (map[string]string, error) {
	allowedKeys := map[string]struct{}{
		"XINSIGHT_MIGRATIONS_DIR": {},
	}
	if secret {
		allowedKeys[config.PostgresDSNEnv] = struct{}{}
	}
	return readAllowlistedEnvironmentFile(path, secret, allowedKeys)
}

func readAllowlistedEnvironmentFile(path string, secret bool, allowedKeys map[string]struct{}) (map[string]string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("environment file path must be absolute and clean")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maxEnvironmentFileBytes {
		return nil, fmt.Errorf("environment file type or size is invalid")
	}
	permissions := before.Mode().Perm()
	if permissions&0o200 == 0 || permissions&0o022 != 0 || before.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || (secret && permissions&0o077 != 0) {
		return nil, fmt.Errorf("environment file permissions are invalid")
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || (int(stat.Uid) != 0 && int(stat.Uid) != os.Geteuid()) {
		return nil, fmt.Errorf("environment file ownership is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("environment file cannot be opened")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, fmt.Errorf("environment file changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxEnvironmentFileBytes+1))
	if err != nil || len(contents) > maxEnvironmentFileBytes {
		return nil, fmt.Errorf("environment file cannot be read safely")
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, present := strings.Cut(line, "=")
		if !present || key == "" || value == "" || strings.TrimSpace(key) != key || strings.TrimSpace(value) != value {
			return nil, fmt.Errorf("environment file contains an invalid assignment")
		}
		if _, allowed := allowedKeys[key]; !allowed {
			return nil, fmt.Errorf("environment file contains an unsupported key")
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("environment file contains a duplicate key")
		}
		values[key] = value
	}
	return values, nil
}

func validateTrustStoreFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 {
		return fmt.Errorf("release trust store type or size is invalid")
	}
	if info.Mode().Perm()&0o022 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("release trust store permissions are invalid")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (int(stat.Uid) != 0 && int(stat.Uid) != os.Geteuid()) {
		return fmt.Errorf("release trust store ownership is invalid")
	}
	return nil
}

func runSystemctl(ctx context.Context, runner CommandRunner, args ...string) error {
	_, err := runner.Run(ctx, Command{Name: "systemctl", Args: args})
	return err
}

func validateServiceDropIns(paths Paths) error {
	info, err := os.Lstat(paths.ServiceDropInDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("systemd service drop-in path is not a real directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (int(stat.Uid) != 0 && int(stat.Uid) != os.Geteuid()) {
		return fmt.Errorf("systemd service drop-in directory ownership is invalid")
	}
	entries, err := os.ReadDir(paths.ServiceDropInDir)
	if err != nil || len(entries) > 64 {
		return fmt.Errorf("systemd service drop-in directory cannot be inspected safely")
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		var expected string
		switch entry.Name() {
		case legacyPhase1ServiceDropInName:
			expected = legacyPhase1ServiceDropIn
		case managedServiceDropInName:
			expected = managedServiceDropIn
		default:
			return fmt.Errorf("unsupported active systemd service drop-in: %s", entry.Name())
		}
		if err := validateExactServiceDropIn(filepath.Join(paths.ServiceDropInDir, entry.Name()), expected); err != nil {
			return err
		}
	}
	return nil
}

func validateManagedServiceDropInSource(path string) error {
	return validateExactServiceDropIn(path, managedServiceDropIn)
}

func validateManagedServiceDropInRuntime(releaseDir string, paths Paths) error {
	source := filepath.Join(releaseDir, "deploy", managedServiceDropInName)
	sourceExists, err := fileExists(source)
	if err != nil {
		return err
	}
	destinationExists, err := fileExists(paths.ManagedServiceDropIn)
	if err != nil {
		return err
	}
	if sourceExists != destinationExists {
		return fmt.Errorf("managed systemd service drop-in presence does not match the active release")
	}
	if !sourceExists {
		return nil
	}
	if err := validateManagedServiceDropInSource(source); err != nil {
		return err
	}
	if err := requireMatchingFiles(source, paths.ManagedServiceDropIn, maxEnvironmentFileBytes); err != nil {
		return fmt.Errorf("managed systemd service drop-in runtime drift: %w", err)
	}
	return nil
}

func validateExactServiceDropIn(path string, expected string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 || info.Size() != int64(len(expected)) {
		return fmt.Errorf("systemd service drop-in metadata is invalid: %s", filepath.Base(path))
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (int(stat.Uid) != 0 && int(stat.Uid) != os.Geteuid()) {
		return fmt.Errorf("systemd service drop-in ownership is invalid: %s", filepath.Base(path))
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != expected {
		return fmt.Errorf("systemd service drop-in contents are invalid: %s", filepath.Base(path))
	}
	return nil
}

func verifyEffectiveServiceLauncher(ctx context.Context, runner CommandRunner, paths Paths) error {
	result, err := runner.Run(ctx, Command{
		Name: "systemctl",
		Args: []string{"show", "xinsight.service", "--property", "ExecStart", "--value"},
	})
	if err != nil {
		return err
	}
	expected := "path=" + paths.ServiceLauncher
	if !strings.Contains(result.Output, expected) || strings.Contains(result.Output, paths.CurrentLink+"/bin/xinsight-server") {
		return fmt.Errorf("effective systemd ExecStart does not use the managed launcher")
	}
	return nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
