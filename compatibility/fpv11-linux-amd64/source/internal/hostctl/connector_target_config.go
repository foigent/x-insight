package hostctl

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

var connectorIdentityEnvironmentKeys = map[string]struct{}{
	connectorcontrol.DeploymentIDEnv: {},
}

var connectorTargetEnvironmentKeys = map[string]struct{}{
	connectorcontrol.UniRouterInstallOriginEnv: {},
}

var trustedTLSProxyEnvironmentKeys = map[string]struct{}{
	api.TrustedTLSProxyModeEnv: {},
	api.ExternalOriginEnv:      {},
}

func readConnectorIdentityEnvironmentFile(path string) (string, error) {
	values, err := readAllowlistedEnvironmentFile(path, true, connectorIdentityEnvironmentKeys)
	if err != nil {
		return "", fmt.Errorf("managed Connector identity environment is invalid: %w", err)
	}
	deploymentID := values[connectorcontrol.DeploymentIDEnv]
	if len(values) != 1 || !connectorcontrol.IsDeploymentID(deploymentID) {
		return "", fmt.Errorf("managed Connector environment has an invalid deployment identity")
	}
	return deploymentID, nil
}

func readConnectorIdentityEnvironmentFileIfPresent(path string) (string, bool, error) {
	present, err := fileExists(path)
	if err != nil || !present {
		return "", false, err
	}
	deploymentID, err := readConnectorIdentityEnvironmentFile(path)
	return deploymentID, true, err
}

func readConnectorTargetEnvironmentFile(path string) (string, error) {
	values, err := readAllowlistedEnvironmentFile(path, true, connectorTargetEnvironmentKeys)
	if err != nil {
		return "", fmt.Errorf("managed Connector target environment is invalid: %w", err)
	}
	origin := values[connectorcontrol.UniRouterInstallOriginEnv]
	if len(values) != 1 || !connectorcontrol.IsCanonicalHTTPSOrigin(origin) {
		return "", fmt.Errorf("managed Connector target environment has an invalid UniRouter origin")
	}
	return origin, nil
}

func readConnectorTargetEnvironmentFileIfPresent(path string) (string, bool, error) {
	present, err := fileExists(path)
	if err != nil || !present {
		return "", false, err
	}
	origin, err := readConnectorTargetEnvironmentFile(path)
	return origin, true, err
}

func validateTrustedTLSProxyEnvironmentFileIfPresent(path string) (bool, error) {
	present, err := fileExists(path)
	if err != nil || !present {
		return false, err
	}
	values, err := readAllowlistedEnvironmentFile(path, false, trustedTLSProxyEnvironmentKeys)
	if err != nil || len(values) != 2 {
		return false, fmt.Errorf("managed trusted TLS proxy environment is invalid")
	}
	if _, err := api.NewTransportSecurity(
		values[api.TrustedTLSProxyModeEnv],
		values[api.ExternalOriginEnv],
	); err != nil {
		return false, fmt.Errorf("managed trusted TLS proxy environment is invalid")
	}
	return true, nil
}

func (engine *Engine) validateConnectorConfiguration(requiredIdentity bool) error {
	_, identityPresent, err := readConnectorIdentityEnvironmentFileIfPresent(engine.Paths.ConnectorIdentityEnv)
	if err != nil {
		return err
	}
	if requiredIdentity && !identityPresent {
		return fmt.Errorf("managed Connector deployment identity is required")
	}
	_, targetPresent, err := readConnectorTargetEnvironmentFileIfPresent(engine.Paths.ConnectorEnvironment)
	if err != nil {
		return err
	}
	if targetPresent && !identityPresent {
		return fmt.Errorf("Connector target requires the managed deployment identity")
	}
	tlsConfigured, err := validateTrustedTLSProxyEnvironmentFileIfPresent(engine.Paths.TrustedTLSProxyEnv)
	if err != nil {
		return err
	}
	if targetPresent && !tlsConfigured {
		return fmt.Errorf("Connector target requires the managed trusted TLS external origin")
	}
	return nil
}

func (engine *Engine) ValidateConnectorTargetConfig() error {
	return engine.validateConnectorConfiguration(true)
}

func (engine *Engine) SetConnectorTargetOrigin(origin string) error {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return err
	}
	if !connectorcontrol.IsCanonicalHTTPSOrigin(origin) {
		return fmt.Errorf("UniRouter install origin must be one canonical HTTPS origin")
	}
	if err := engine.ValidateConnectorTargetConfig(); err != nil {
		return err
	}
	tlsConfigured, err := validateTrustedTLSProxyEnvironmentFileIfPresent(engine.Paths.TrustedTLSProxyEnv)
	if err != nil {
		return err
	}
	if !tlsConfigured {
		return fmt.Errorf("Connector target requires the managed trusted TLS external origin")
	}
	if err := engine.writeConnectorTargetEnvironmentAtomic(connectorTargetEnvironmentContents(origin)); err != nil {
		return err
	}
	return engine.ValidateConnectorTargetConfig()
}

func (engine *Engine) ensureConnectorIdentityEnvironmentFile() error {
	finalPath := engine.Paths.ConnectorIdentityEnv
	stagingPath := finalPath + ".staged"
	if _, present, err := readConnectorIdentityEnvironmentFileIfPresent(finalPath); err != nil {
		return err
	} else if present {
		return engine.reconcilePublishedConnectorIdentity(stagingPath, finalPath)
	}

	if _, present, err := readConnectorIdentityEnvironmentFileIfPresent(stagingPath); err != nil {
		if discardErr := engine.discardRecoverableConnectorStagingFile(stagingPath); discardErr != nil {
			return fmt.Errorf("recover Connector deployment identity staging: %v; cleanup failed: %w", err, discardErr)
		}
		if err := engine.writeGeneratedConnectorIdentityStaging(stagingPath); err != nil {
			return err
		}
	} else if !present {
		if err := engine.writeGeneratedConnectorIdentityStaging(stagingPath); err != nil {
			return err
		}
	}

	if err := os.Link(stagingPath, finalPath); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("publish Connector deployment identity: %w", err)
	}
	if err := syncDirectory(filepath.Dir(finalPath)); err != nil {
		return err
	}
	if err := requireMatchingFiles(stagingPath, finalPath, maxEnvironmentFileBytes); err != nil {
		return fmt.Errorf("published Connector deployment identity drift: %w", err)
	}
	if _, err := readConnectorIdentityEnvironmentFile(finalPath); err != nil {
		return err
	}
	if err := os.Remove(stagingPath); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(finalPath))
}

func (engine *Engine) reconcilePublishedConnectorIdentity(stagingPath string, finalPath string) error {
	if _, present, err := readConnectorIdentityEnvironmentFileIfPresent(stagingPath); err != nil {
		if discardErr := engine.discardRecoverableConnectorStagingFile(stagingPath); discardErr != nil {
			return fmt.Errorf("recover published Connector identity staging: %v; cleanup failed: %w", err, discardErr)
		}
		return nil
	} else if !present {
		return nil
	}
	if err := requireMatchingFiles(stagingPath, finalPath, maxEnvironmentFileBytes); err != nil {
		return fmt.Errorf("published Connector identity staging drift: %w", err)
	}
	if err := os.Remove(stagingPath); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(finalPath))
}

func (engine *Engine) writeGeneratedConnectorIdentityStaging(path string) error {
	randomBytes := make([]byte, 16)
	reader := io.Reader(rand.Reader)
	if engine.Random != nil {
		reader = engine.Random
	}
	if _, err := io.ReadFull(reader, randomBytes); err != nil {
		return fmt.Errorf("generate Connector deployment identity: %w", err)
	}
	deploymentID := "xid_" + base64.RawURLEncoding.EncodeToString(randomBytes)
	if !connectorcontrol.IsDeploymentID(deploymentID) {
		return fmt.Errorf("generated Connector deployment identity is invalid")
	}
	return engine.writeConnectorEnvironmentExclusive(path, connectorIdentityEnvironmentContents(deploymentID))
}

func connectorIdentityEnvironmentContents(deploymentID string) []byte {
	return []byte(connectorcontrol.DeploymentIDEnv + "=" + deploymentID + "\n")
}

func connectorTargetEnvironmentContents(origin string) []byte {
	return []byte(connectorcontrol.UniRouterInstallOriginEnv + "=" + origin + "\n")
}

func (engine *Engine) writeConnectorEnvironmentExclusive(path string, contents []byte) error {
	if err := ensureDirectory(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	fail := func(cause error) error {
		_ = file.Close()
		if cleanupErr := engine.discardRecoverableConnectorStagingFile(path); cleanupErr != nil {
			return fmt.Errorf("%v; cleanup failed: %w", cause, cleanupErr)
		}
		return cause
	}
	if !engine.Paths.IsTestRoot {
		if err := file.Chown(0, 0); err != nil {
			return fail(err)
		}
	}
	if err := file.Chmod(0o600); err != nil {
		return fail(err)
	}
	if err := writeSyncClose(file, contents); err != nil {
		return fail(err)
	}
	return syncDirectory(filepath.Dir(path))
}

func (engine *Engine) discardRecoverableConnectorStagingFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("Connector environment staging path is not a recoverable regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || (int(stat.Uid) != 0 && int(stat.Uid) != os.Geteuid()) {
		return fmt.Errorf("Connector environment staging ownership is not recoverable")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("Connector environment staging permissions are not recoverable")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func (engine *Engine) writeConnectorTargetEnvironmentAtomic(contents []byte) error {
	nextPath := engine.Paths.ConnectorEnvironment + ".next"
	if _, present, err := readConnectorTargetEnvironmentFileIfPresent(nextPath); err != nil {
		if discardErr := engine.discardRecoverableConnectorStagingFile(nextPath); discardErr != nil {
			return fmt.Errorf("recover Connector target staging: %v; cleanup failed: %w", err, discardErr)
		}
	} else if present {
		existing, readErr := os.ReadFile(nextPath)
		if readErr != nil || !bytes.Equal(existing, contents) {
			if err := engine.discardRecoverableConnectorStagingFile(nextPath); err != nil {
				return err
			}
		}
	}
	if exists, err := fileExists(nextPath); err != nil {
		return err
	} else if !exists {
		if err := engine.writeConnectorEnvironmentExclusive(nextPath, contents); err != nil {
			return err
		}
	}
	if err := os.Rename(nextPath, engine.Paths.ConnectorEnvironment); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(engine.Paths.ConnectorEnvironment))
}
