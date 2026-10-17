package hostctl

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/foigent/x-insight/internal/connectorcontrol"
)

// These files are installed by the root host CLI, not by the sandboxed
// controller. The origin remains the sole network authority.
const targetNetworkDropIn = "19-ur-target-network.conf"

func (e *Engine) targetNetworkFiles(origin string) (map[string]string, error) {
	prefix, err := machineEgressPrefix(origin)
	if err != nil {
		return nil, err
	}
	body := managedDirectEgressContent(prefix)
	return map[string]string{
		filepath.Join(e.Paths.ControllerServiceUnit+".d", targetNetworkDropIn): body,
		filepath.Join(e.Paths.FeedServiceUnit+".d", targetNetworkDropIn):       body,
	}, nil
}

// The two units may be half-published after interruption. Read-only checks
// accept that recoverable state but reject any changed byte or old rule whose
// effective address disagrees with the one pinned target.
func (e *Engine) validateTargetNetworkFiles(origin string) (int, error) {
	files, err := e.targetNetworkFiles(origin)
	if err != nil {
		return 0, err
	}
	count := 0
	for path, body := range files {
		if exists, err := fileExists(path); err != nil {
			return count, err
		} else if exists {
			if err := e.validateRootReference(path, 0644); err != nil {
				return count, fmt.Errorf("target network reference is invalid: %w", err)
			}
			if err := validateExactServiceDropIn(path, body); err != nil {
				return count, fmt.Errorf("target network drop-in changed: %w", err)
			}
			count++
		}
		if err := e.checkReferenceStaging(path, []byte(body), 0644); err != nil {
			return count, fmt.Errorf("target network staging changed: %w", err)
		}
	}
	prefix, _ := machineEgressPrefix(origin)
	for _, path := range []string{
		filepath.Join(e.Paths.ControllerServiceUnit+".d", machineDropIn),
		filepath.Join(e.Paths.FeedServiceUnit+".d", "40-xinsight-reviewed-source-egress.conf"),
	} {
		if exists, err := fileExists(path); err != nil {
			return count, err
		} else if exists {
			if err := validateExactServiceDropIn(path, managedDirectEgressContent(prefix)); err != nil {
				return count, fmt.Errorf("existing network rule conflicts with Connector target: %w", err)
			}
		}
	}
	return count, nil
}

func (e *Engine) validateTargetNetworkOptional() error {
	origin, present, err := readConnectorTargetEnvironmentFileIfPresent(e.Paths.ConnectorEnvironment)
	if err != nil {
		return err
	}
	if !present {
		for _, unit := range []string{e.Paths.ControllerServiceUnit, e.Paths.FeedServiceUnit} {
			path := filepath.Join(unit+".d", targetNetworkDropIn)
			for _, candidate := range []string{path, path + ".next"} {
				if exists, err := fileExists(candidate); err != nil {
					return err
				} else if exists {
					return fmt.Errorf("target network drop-in has no target authority")
				}
			}
		}
		return nil
	}
	if _, err := machineEgressPrefix(origin); err != nil {
		// Hostname targets remain legacy-only. They never qualify for the
		// precise managed route, and must not retain an old exact-IP rule.
		for _, unit := range []string{e.Paths.ControllerServiceUnit, e.Paths.FeedServiceUnit} {
			path := filepath.Join(unit+".d", targetNetworkDropIn)
			for _, candidate := range []string{path, path + ".next"} {
				if exists, err := fileExists(candidate); err != nil {
					return err
				} else if exists {
					return fmt.Errorf("hostname target cannot own an exact-IP network drop-in")
				}
			}
		}
		return nil
	}
	_, err = e.validateTargetNetworkFiles(origin)
	return err
}

// Called only from the root CLI or an existing root install/upgrade journal.
// Validate every existing path before the first publication; each publication
// is exclusive and can be completed by replaying the same origin.
func (e *Engine) publishTargetNetworkFiles(origin string) (bool, error) {
	count, err := e.validateTargetNetworkFiles(origin)
	if err != nil {
		return false, err
	}
	if count == 2 {
		return false, nil
	}
	files, _ := e.targetNetworkFiles(origin)
	for path, body := range files {
		if exists, err := fileExists(path); err != nil {
			return false, err
		} else if !exists {
			if err := e.publishReference(path, []byte(body), 0644); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

func (e *Engine) ProveTargetNetwork(ctx context.Context, unit string) error {
	origin, err := readConnectorTargetEnvironmentFile(e.Paths.ConnectorEnvironment)
	if err != nil {
		return err
	}
	if _, err := machineEgressPrefix(origin); err != nil {
		return fmt.Errorf("precise target network requires an IP origin: %w", err)
	}
	if count, err := e.validateTargetNetworkFiles(origin); err != nil {
		return err
	} else if count != 2 {
		return fmt.Errorf("target network publication is incomplete")
	}
	if unit != connectorControllerUnitName && unit != feedWorkerUnitName {
		return fmt.Errorf("unsupported target network unit")
	}
	if unit == connectorControllerUnitName {
		pid, err := e.systemdUnitProperty(ctx, unit, "MainPID")
		if err != nil || pid == "0" {
			return fmt.Errorf("controller process cannot be proved")
		}
		if _, err := e.systemdUnitProperty(ctx, unit, "InvocationID"); err != nil {
			return fmt.Errorf("controller invocation cannot be proved: %w", err)
		}
	}
	allowed, err := e.systemdUnitProperty(ctx, unit, "IPAddressAllow")
	if err != nil {
		return err
	}
	ready, err := machineEgressMatches(origin, allowed)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("%s effective target network is not exact", unit)
	}
	return nil
}

// SetConnectorTargetOriginWithNetwork is the operator-facing set path. A
// matching retry completes a partial drop-in publish and reload; it never
// widens or silently changes an existing network binding.
func (e *Engine) SetConnectorTargetOriginWithNetwork(ctx context.Context, origin string) error {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return err
	}
	if !connectorcontrol.IsCanonicalHTTPSOrigin(origin) {
		return fmt.Errorf("UniRouter install origin must be one canonical HTTPS origin")
	}
	return e.withOperationLock(func() error {
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		current, present, err := readConnectorTargetEnvironmentFileIfPresent(e.Paths.ConnectorEnvironment)
		if err != nil {
			return err
		}
		if !present || current != origin {
			if _, _, enabled, err := e.ReadURMachineConfig(); err != nil {
				return err
			} else if enabled {
				return fmt.Errorf("active machine configuration forbids implicit origin migration")
			}
			for _, path := range []string{e.Paths.FeedEnvironmentFile, e.Paths.FeedCredentialFile, e.Paths.FeedCredentialState} {
				if exists, err := fileExists(path); err != nil {
					return err
				} else if exists {
					return fmt.Errorf("managed feed binding forbids implicit origin migration")
				}
			}
		}
		if _, err := machineEgressPrefix(origin); err == nil {
			if _, err := e.validateTargetNetworkFiles(origin); err != nil {
				return err
			}
		} else if present && current != origin {
			return fmt.Errorf("hostname target migration needs separate network review")
		}
		if err := e.SetConnectorTargetOrigin(origin); err != nil {
			return err
		}
		if _, err := machineEgressPrefix(origin); err != nil {
			return nil
		}
		before, _ := e.systemdUnitProperty(ctx, connectorControllerUnitName, "InvocationID")
		_, err = e.publishTargetNetworkFiles(origin)
		if err != nil {
			return err
		}
		// Explicit set always crosses the reload/restart barrier. A retry
		// after an interrupted prior run must not infer that the old process
		// already loaded manager-side IPAddressAllow readback.
		if err := runSystemctl(ctx, e.Runner, "daemon-reload"); err != nil {
			return err
		}
		if err := runSystemctl(ctx, e.Runner, "restart", connectorControllerUnitName); err != nil {
			return err
		}
		if !e.Paths.IsTestRoot {
			if err := e.waitForConnectorControllerRuntime(ctx); err != nil {
				return err
			}
		}
		if err := e.ProveTargetNetwork(ctx, connectorControllerUnitName); err != nil {
			return err
		}
		if err := e.ProveTargetNetwork(ctx, feedWorkerUnitName); err != nil {
			return err
		}
		if before != "" {
			after, err := e.systemdUnitProperty(ctx, connectorControllerUnitName, "InvocationID")
			if err != nil || after == before {
				return fmt.Errorf("controller has not loaded the target network in a new invocation")
			}
		}
		return nil
	})
}

func (e *Engine) prepareExistingTargetNetwork() error {
	origin, present, err := readConnectorTargetEnvironmentFileIfPresent(e.Paths.ConnectorEnvironment)
	if err != nil || !present {
		return err
	}
	if _, err := machineEgressPrefix(origin); err != nil {
		return e.validateTargetNetworkOptional()
	}
	_, err = e.publishTargetNetworkFiles(origin)
	return err
}

func (e *Engine) targetNetworkRequired() (bool, error) {
	origin, present, err := readConnectorTargetEnvironmentFileIfPresent(e.Paths.ConnectorEnvironment)
	if err != nil || !present {
		return false, err
	}
	// These feed methods also serve existing legacy connections. Keep their
	// hostname behavior; machine enrollment separately requires a literal IP.
	_, err = machineEgressPrefix(origin)
	return err == nil, nil
}
