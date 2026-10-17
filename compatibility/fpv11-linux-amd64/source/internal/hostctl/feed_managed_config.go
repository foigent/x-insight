package hostctl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

const (
	ManagedFeedConfigStatusSchema   = "xinsight.managed-feed-config-status.v1"
	managedFeedNetworkBindingSchema = "xinsight.managed-feed-network-binding.v1"
	managedFeedPollInterval         = "5s"
	managedFeedAllowPrivateHTTP     = "false"
	managedFeedMappingMode          = config.UniRouterFeedMappingLegacy
)

var hostctlSHA256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type ManagedFeedConfigStatus struct {
	SchemaVersion        string                                        `json:"schema_version"`
	Status               string                                        `json:"status"`
	TrustedOrigin        string                                        `json:"trusted_origin,omitempty"`
	Endpoint             string                                        `json:"endpoint,omitempty"`
	ConfigSHA256         string                                        `json:"config_sha256,omitempty"`
	NetworkBindingSHA256 string                                        `json:"network_binding_sha256,omitempty"`
	NetworkBinding       *connectorcontrol.FixedReceiverNetworkBinding `json:"network_binding,omitempty"`
	Active               bool                                          `json:"active"`
	Staged               bool                                          `json:"staged"`
	Replayed             bool                                          `json:"replayed"`
}

type managedFeedNetworkBindingAuthority struct {
	SchemaVersion string `json:"schema_version"`
	TrustedOrigin string `json:"trusted_origin"`
	Transport     string `json:"transport"`
	Route         string `json:"route"`
}

type managedFeedConfigReadback struct {
	contents       []byte
	trustedOrigin  string
	endpoint       string
	configSHA256   string
	networkBinding connectorcontrol.FixedReceiverNetworkBinding
}

// StageManagedFeedConfig creates the exact root-owned inactive feed runtime
// configuration. trustedOrigin is authority supplied by the local controller;
// callers cannot supply a route, polling interval, mapping mode, or HTTP
// exception.
func (engine *Engine) StageManagedFeedConfig(
	ctx context.Context,
	trustedOrigin string,
) (ManagedFeedConfigStatus, error) {
	_ = ctx
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return ManagedFeedConfigStatus{}, err
	}
	expected, err := buildManagedFeedConfig(trustedOrigin)
	if err != nil {
		return ManagedFeedConfigStatus{}, err
	}
	var report ManagedFeedConfigStatus
	err = engine.withOperationLock(func() error {
		if err := ensureDirectory(filepath.Dir(engine.Paths.FeedEnvironmentFile), 0o755); err != nil {
			return err
		}
		active, activePresent, err := engine.readManagedFeedConfigOptional(engine.Paths.FeedEnvironmentFile)
		if err != nil {
			return err
		}
		stagedPath := engine.managedFeedConfigStagedPath()
		staged, stagedPresent, err := engine.readManagedFeedConfigOptional(stagedPath)
		if err != nil {
			return err
		}
		if activePresent {
			if active.configSHA256 == expected.configSHA256 && !stagedPresent {
				report = managedFeedConfigReport(active, true, false, true)
				return nil
			}
			return fmt.Errorf("active managed feed configuration conflicts with requested authority")
		}
		if stagedPresent {
			if staged.configSHA256 != expected.configSHA256 {
				return fmt.Errorf("a different managed feed configuration is already staged")
			}
			report = managedFeedConfigReport(staged, false, true, true)
			return nil
		}
		if err := engine.writeManagedFeedConfigExclusive(stagedPath, expected.contents); err != nil {
			return fmt.Errorf("stage managed feed configuration: %w", err)
		}
		staged, stagedPresent, err = engine.readManagedFeedConfigOptional(stagedPath)
		if err != nil {
			return err
		}
		if !stagedPresent || staged.configSHA256 != expected.configSHA256 {
			return fmt.Errorf("staged managed feed configuration readback mismatch")
		}
		report = managedFeedConfigReport(staged, false, true, false)
		return nil
	})
	return report, err
}

// PromoteManagedFeedConfig atomically promotes only the exact staged digest.
// A lost response is recovered by readback of the already-active exact bytes.
func (engine *Engine) PromoteManagedFeedConfig(
	ctx context.Context,
	expectedConfigSHA256 string,
) (ManagedFeedConfigStatus, error) {
	_ = ctx
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return ManagedFeedConfigStatus{}, err
	}
	if !hostctlSHA256Pattern.MatchString(expectedConfigSHA256) {
		return ManagedFeedConfigStatus{}, fmt.Errorf("managed feed configuration digest is invalid")
	}
	var report ManagedFeedConfigStatus
	err := engine.withOperationLock(func() error {
		stagedPath := engine.managedFeedConfigStagedPath()
		staged, stagedPresent, err := engine.readManagedFeedConfigOptional(stagedPath)
		if err != nil {
			return err
		}
		active, activePresent, err := engine.readManagedFeedConfigOptional(engine.Paths.FeedEnvironmentFile)
		if err != nil {
			return err
		}
		if !stagedPresent {
			if activePresent && active.configSHA256 == expectedConfigSHA256 {
				report = managedFeedConfigReport(active, true, false, true)
				return nil
			}
			return fmt.Errorf("exact staged managed feed configuration is missing")
		}
		if staged.configSHA256 != expectedConfigSHA256 {
			return fmt.Errorf("staged managed feed configuration digest mismatch")
		}
		if activePresent && active.configSHA256 == expectedConfigSHA256 {
			return fmt.Errorf("duplicate active and staged managed feed configuration exists")
		}
		if err := os.Rename(stagedPath, engine.Paths.FeedEnvironmentFile); err != nil {
			return fmt.Errorf("atomically promote managed feed configuration: %w", err)
		}
		if err := syncDirectory(filepath.Dir(engine.Paths.FeedEnvironmentFile)); err != nil {
			return fmt.Errorf("sync managed feed configuration directory: %w", err)
		}
		active, activePresent, err = engine.readManagedFeedConfigOptional(engine.Paths.FeedEnvironmentFile)
		if err != nil {
			return err
		}
		if !activePresent || active.configSHA256 != expectedConfigSHA256 {
			return fmt.Errorf("active managed feed configuration readback mismatch")
		}
		report = managedFeedConfigReport(active, true, false, false)
		return nil
	})
	return report, err
}

func (engine *Engine) ManagedFeedConfigStatus(ctx context.Context) (ManagedFeedConfigStatus, error) {
	_ = ctx
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return ManagedFeedConfigStatus{}, err
	}
	var report ManagedFeedConfigStatus
	err := engine.withOperationLock(func() error {
		active, activePresent, err := engine.readManagedFeedConfigOptional(engine.Paths.FeedEnvironmentFile)
		if err != nil {
			return err
		}
		staged, stagedPresent, err := engine.readManagedFeedConfigOptional(engine.managedFeedConfigStagedPath())
		if err != nil {
			return err
		}
		switch {
		case activePresent && stagedPresent:
			return fmt.Errorf("active and staged managed feed configurations conflict")
		case activePresent:
			report = managedFeedConfigReport(active, true, false, false)
		case stagedPresent:
			report = managedFeedConfigReport(staged, false, true, false)
		default:
			report = ManagedFeedConfigStatus{SchemaVersion: ManagedFeedConfigStatusSchema, Status: "absent"}
		}
		return nil
	})
	return report, err
}

func (engine *Engine) managedFeedConfigStagedPath() string {
	return engine.Paths.FeedEnvironmentFile + ".next"
}

func buildManagedFeedConfig(trustedOrigin string) (managedFeedConfigReadback, error) {
	if err := validateManagedFeedTrustedOrigin(trustedOrigin); err != nil {
		return managedFeedConfigReadback{}, err
	}
	endpoint := trustedOrigin + config.UniRouterCompletedExecutionsPath
	contents := []byte(strings.Join([]string{
		config.UniRouterFeedEndpointEnv + "=" + endpoint,
		config.UniRouterFeedPollIntervalEnv + "=" + managedFeedPollInterval,
		config.UniRouterFeedAllowHTTPEnv + "=" + managedFeedAllowPrivateHTTP,
		config.UniRouterFeedMappingModeEnv + "=" + managedFeedMappingMode,
		"",
	}, "\n"))
	configSHA256 := managedFeedSHA256Reference(contents)
	bindingAuthority := managedFeedNetworkBindingAuthority{
		SchemaVersion: managedFeedNetworkBindingSchema,
		TrustedOrigin: trustedOrigin,
		Transport:     connectorcontrol.ReceiverNetworkTransport,
		Route:         connectorcontrol.ReceiverFeedRoute,
	}
	bindingSHA256, _, err := connectorcontrol.CanonicalSHA256(bindingAuthority)
	if err != nil {
		return managedFeedConfigReadback{}, err
	}
	return managedFeedConfigReadback{
		contents:      contents,
		trustedOrigin: trustedOrigin,
		endpoint:      endpoint,
		configSHA256:  configSHA256,
		networkBinding: connectorcontrol.FixedReceiverNetworkBinding{
			Transport:     connectorcontrol.ReceiverNetworkTransport,
			Route:         connectorcontrol.ReceiverFeedRoute,
			BindingSHA256: bindingSHA256,
		},
	}, nil
}

func validateManagedFeedTrustedOrigin(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || parsed.String() != value || strings.Contains(parsed.Host, "@") {
		return fmt.Errorf("managed feed trusted origin must be one canonical HTTPS origin")
	}
	return nil
}

func (engine *Engine) readManagedFeedConfigOptional(path string) (managedFeedConfigReadback, bool, error) {
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return managedFeedConfigReadback{}, false, nil
	}
	if err != nil {
		return managedFeedConfigReadback{}, false, err
	}
	if !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 || before.Size() <= 0 ||
		before.Size() > maxEnvironmentFileBytes || before.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return managedFeedConfigReadback{}, false, fmt.Errorf("managed feed configuration metadata is invalid")
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	expectedUID, expectedGID := engine.feedCredentialExpectedOwner()
	if !ok || uint32(stat.Uid) != expectedUID || uint32(stat.Gid) != expectedGID || stat.Nlink != 1 {
		return managedFeedConfigReadback{}, false, fmt.Errorf("managed feed configuration ownership or link count is invalid")
	}
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return managedFeedConfigReadback{}, false, fmt.Errorf("open managed feed configuration safely")
	}
	file := os.NewFile(uintptr(descriptor), filepath.Base(path))
	if file == nil {
		_ = syscall.Close(descriptor)
		return managedFeedConfigReadback{}, false, fmt.Errorf("open managed feed configuration handle")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return managedFeedConfigReadback{}, false, fmt.Errorf("managed feed configuration changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxEnvironmentFileBytes+1))
	if err != nil || len(contents) > maxEnvironmentFileBytes {
		return managedFeedConfigReadback{}, false, fmt.Errorf("managed feed configuration cannot be read safely")
	}
	endpointLine := config.UniRouterFeedEndpointEnv + "="
	firstLine, _, found := strings.Cut(string(contents), "\n")
	if !found || !strings.HasPrefix(firstLine, endpointLine) {
		return managedFeedConfigReadback{}, false, fmt.Errorf("managed feed configuration endpoint is invalid")
	}
	endpoint := strings.TrimPrefix(firstLine, endpointLine)
	if !strings.HasSuffix(endpoint, config.UniRouterCompletedExecutionsPath) {
		return managedFeedConfigReadback{}, false, fmt.Errorf("managed feed configuration route is invalid")
	}
	trustedOrigin := strings.TrimSuffix(endpoint, config.UniRouterCompletedExecutionsPath)
	expected, err := buildManagedFeedConfig(trustedOrigin)
	if err != nil || !bytes.Equal(contents, expected.contents) {
		return managedFeedConfigReadback{}, false, fmt.Errorf("managed feed configuration bytes are not canonical")
	}
	return expected, true, nil
}

func (engine *Engine) writeManagedFeedConfigExclusive(path string, contents []byte) error {
	suffix, err := randomToken(8)
	if err != nil {
		return err
	}
	temporary := path + ".write-" + suffix
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if err := writeSyncClose(file, contents); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func managedFeedConfigReport(
	readback managedFeedConfigReadback,
	active bool,
	staged bool,
	replayed bool,
) ManagedFeedConfigStatus {
	binding := readback.networkBinding
	status := "staged"
	if active {
		status = "active"
	}
	return ManagedFeedConfigStatus{
		SchemaVersion:        ManagedFeedConfigStatusSchema,
		Status:               status,
		TrustedOrigin:        readback.trustedOrigin,
		Endpoint:             readback.endpoint,
		ConfigSHA256:         readback.configSHA256,
		NetworkBindingSHA256: readback.networkBinding.BindingSHA256,
		NetworkBinding:       &binding,
		Active:               active,
		Staged:               staged,
		Replayed:             replayed,
	}
}

func managedFeedSHA256Reference(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}
