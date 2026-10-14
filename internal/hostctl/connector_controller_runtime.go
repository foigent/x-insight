package hostctl

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const connectorControllerUnitName = "xinsight-connector-controller.service"

func connectorControllerSupportedByRelease(releaseDir string) (bool, error) {
	for _, path := range []string{
		filepath.Join(releaseDir, "bin", "xinsightctl"),
		filepath.Join(releaseDir, "deploy", connectorControllerUnitName),
	} {
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("connector controller release path is not a regular file: %s", filepath.Base(path))
		}
	}
	return true, nil
}

func (engine *Engine) installConnectorControllerUnitForRelease(releaseDir string, operationID string) error {
	return engine.installManagedWorkerUnitForRelease(
		releaseDir,
		operationID,
		connectorControllerUnitName,
		engine.Paths.ControllerServiceUnit,
		"connector controller",
		true,
	)
}

func (engine *Engine) reconcileConnectorControllerUnitForRelease(
	ctx context.Context,
	releaseDir string,
	operationID string,
) error {
	supported, err := connectorControllerSupportedByRelease(releaseDir)
	if err != nil {
		return err
	}
	if !supported {
		state, err := engine.inspectManagedWorkerState(
			ctx,
			connectorControllerUnitName,
			engine.Paths.ControllerServiceUnit,
			"connector controller",
		)
		if err != nil {
			return err
		}
		if state.present {
			if err := runSystemctl(ctx, engine.Runner, "disable", "--now", connectorControllerUnitName); err != nil {
				return err
			}
		}
	}
	return engine.installConnectorControllerUnitForRelease(releaseDir, operationID)
}

func (engine *Engine) stopConnectorControllerForMaintenance(ctx context.Context, journal *Journal) error {
	state, err := engine.inspectManagedWorkerState(
		ctx,
		connectorControllerUnitName,
		engine.Paths.ControllerServiceUnit,
		"connector controller",
	)
	if err != nil {
		return engine.operationFailed(journal, "connector_controller_state_inspection_failed", err)
	}
	if err := engine.runPhase(journal, "stop_connector_controller", func() error {
		if !state.present || !state.active {
			return nil
		}
		return runSystemctl(ctx, engine.Runner, "stop", connectorControllerUnitName)
	}); err != nil {
		return engine.operationFailed(journal, "connector_controller_stop_failed", err)
	}
	return nil
}

func (engine *Engine) startConnectorControllerForRelease(ctx context.Context, releaseDir string) error {
	supported, err := connectorControllerSupportedByRelease(releaseDir)
	if err != nil {
		return err
	}
	if !supported {
		return nil
	}
	if err := runSystemctl(ctx, engine.Runner, "enable", "--now", connectorControllerUnitName); err != nil {
		return err
	}
	return engine.waitForConnectorControllerRuntime(ctx)
}

func (engine *Engine) waitForConnectorControllerRuntime(ctx context.Context) error {
	return engine.waitForConnectorControllerRuntimeState(ctx, true)
}

// Backup restores the recorded active state without changing whether the
// unit was enabled. It still requires the same live socket proof.
func (engine *Engine) waitForBackupConnectorControllerRuntime(ctx context.Context) error {
	return engine.waitForConnectorControllerRuntimeState(ctx, false)
}

func (engine *Engine) waitForConnectorControllerRuntimeState(ctx context.Context, requireEnabled bool) error {
	var lastErr error
	for attempt := 0; attempt < activationProofAttempts; attempt++ {
		if err := engine.proveConnectorControllerRuntimeState(ctx, requireEnabled); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt+1 < activationProofAttempts {
			if err := engine.wait(ctx, 100*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("connector controller runtime did not become ready: %w", lastErr)
}

func (engine *Engine) proveConnectorControllerRuntime(ctx context.Context) error {
	return engine.proveConnectorControllerRuntimeState(ctx, true)
}

func (engine *Engine) proveConnectorControllerRuntimeState(ctx context.Context, requireEnabled bool) error {
	state, err := engine.inspectManagedWorkerState(
		ctx,
		connectorControllerUnitName,
		engine.Paths.ControllerServiceUnit,
		"connector controller",
	)
	if err != nil {
		return err
	}
	if !state.present || !state.active || requireEnabled && !state.enabled {
		return fmt.Errorf("connector controller systemd state is not ready")
	}
	return validateConnectorControllerSocket(engine.Paths)
}

func validateConnectorControllerSocket(paths Paths) error {
	info, err := os.Lstat(paths.ControllerSocket)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o660 {
		return fmt.Errorf("connector controller socket type or mode is invalid")
	}
	if paths.IsTestRoot {
		return nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return fmt.Errorf("connector controller socket owner is invalid")
	}
	group, err := user.LookupGroup("xinsight")
	if err != nil {
		return fmt.Errorf("resolve xinsight group: %w", err)
	}
	expectedGID, err := strconv.ParseUint(group.Gid, 10, 32)
	if err != nil || uint64(stat.Gid) != expectedGID {
		return fmt.Errorf("connector controller socket group is invalid")
	}
	return nil
}
