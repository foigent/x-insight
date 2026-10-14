package hostctl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	feedWorkerUnitName    = "xinsight-feed.service"
	liteLLMWorkerUnitName = "xinsight-litellm-feed.service"
)

type feedWorkerState struct {
	present bool
	active  bool
	enabled bool
}

func (engine *Engine) captureAndStopFeedWorker(ctx context.Context, journal *Journal) error {
	const capturePhase = "capture_feed_worker_state"
	if !containsString(journal.CompletedPhases, capturePhase) {
		if err := engine.beginPhase(journal, capturePhase); err != nil {
			return engine.operationFailed(journal, "journal_update_failed", err)
		}
		state, err := engine.inspectFeedWorkerState(ctx)
		if err != nil {
			return engine.operationFailed(journal, "feed_worker_state_inspection_failed", err)
		}
		journal.FeedWorkerStateCaptured = true
		journal.FeedWorkerWasPresent = state.present
		journal.FeedWorkerWasActive = state.active
		journal.FeedWorkerWasEnabled = state.enabled
		if err := engine.updateJournal(journal); err != nil {
			return engine.operationFailed(journal, "journal_update_failed", err)
		}
		if err := engine.completePhase(journal, capturePhase); err != nil {
			return engine.operationFailed(journal, "journal_update_failed", err)
		}
	}
	if !journal.FeedWorkerStateCaptured {
		return engine.operationFailed(journal, "feed_worker_state_missing", fmt.Errorf("captured feed worker state is missing"))
	}
	if err := engine.runPhase(journal, "stop_feed_worker", func() error {
		if !journal.FeedWorkerWasPresent {
			return nil
		}
		return runSystemctl(ctx, engine.Runner, "stop", feedWorkerUnitName)
	}); err != nil {
		return engine.operationFailed(journal, "feed_worker_stop_failed", err)
	}
	return nil
}

func (engine *Engine) captureAndStopLiteLLMWorker(ctx context.Context, journal *Journal) error {
	const capturePhase = "capture_litellm_worker_state"
	if !containsString(journal.CompletedPhases, capturePhase) {
		if err := engine.beginPhase(journal, capturePhase); err != nil {
			return engine.operationFailed(journal, "journal_update_failed", err)
		}
		state, err := engine.inspectLiteLLMWorkerState(ctx)
		if err != nil {
			return engine.operationFailed(journal, "litellm_worker_state_inspection_failed", err)
		}
		journal.LiteLLMWorkerStateCaptured = true
		journal.LiteLLMWorkerWasPresent = state.present
		journal.LiteLLMWorkerWasActive = state.active
		journal.LiteLLMWorkerWasEnabled = state.enabled
		if err := engine.updateJournal(journal); err != nil {
			return engine.operationFailed(journal, "journal_update_failed", err)
		}
		if err := engine.completePhase(journal, capturePhase); err != nil {
			return engine.operationFailed(journal, "journal_update_failed", err)
		}
	}
	if !journal.LiteLLMWorkerStateCaptured {
		return engine.operationFailed(
			journal,
			"litellm_worker_state_missing",
			fmt.Errorf("captured LiteLLM worker state is missing"),
		)
	}
	if err := engine.runPhase(journal, "stop_litellm_worker", func() error {
		if !journal.LiteLLMWorkerWasPresent {
			return nil
		}
		return runSystemctl(ctx, engine.Runner, "stop", liteLLMWorkerUnitName)
	}); err != nil {
		return engine.operationFailed(journal, "litellm_worker_stop_failed", err)
	}
	return nil
}

func (engine *Engine) restartFeedWorkerAfterMaintenance(ctx context.Context, journal *Journal, releaseDir string) error {
	if !journal.FeedWorkerStateCaptured {
		return engine.operationFailed(journal, "feed_worker_state_missing", fmt.Errorf("captured feed worker state is missing"))
	}
	if err := engine.runPhase(journal, "restore_feed_worker_state", func() error {
		supported, err := feedWorkerSupportedByRelease(releaseDir)
		if err != nil {
			return err
		}
		if !supported {
			if journal.FeedWorkerWasEnabled {
				return runSystemctl(ctx, engine.Runner, "disable", feedWorkerUnitName)
			}
			return nil
		}
		if err := runSystemctl(ctx, engine.Runner, "daemon-reload"); err != nil {
			return err
		}
		if journal.FeedWorkerWasActive {
			if err := runSystemctl(ctx, engine.Runner, "restart", feedWorkerUnitName); err != nil {
				return err
			}
			state, err := engine.inspectFeedWorkerState(ctx)
			if err != nil {
				return err
			}
			if !state.present || !state.active {
				return fmt.Errorf("feed worker did not become active after restart")
			}
		}
		return nil
	}); err != nil {
		return engine.operationFailed(journal, "feed_worker_restore_failed", err)
	}
	return nil
}

func (engine *Engine) restartLiteLLMWorkerAfterMaintenance(
	ctx context.Context,
	journal *Journal,
	releaseDir string,
) error {
	if !journal.LiteLLMWorkerStateCaptured {
		return engine.operationFailed(
			journal,
			"litellm_worker_state_missing",
			fmt.Errorf("captured LiteLLM worker state is missing"),
		)
	}
	if err := engine.runPhase(journal, "restore_litellm_worker_state", func() error {
		supported, err := liteLLMWorkerSupportedByRelease(releaseDir)
		if err != nil {
			return err
		}
		if !supported {
			return nil
		}
		if err := runSystemctl(ctx, engine.Runner, "daemon-reload"); err != nil {
			return err
		}
		if journal.LiteLLMWorkerWasActive {
			if err := runSystemctl(ctx, engine.Runner, "restart", liteLLMWorkerUnitName); err != nil {
				return err
			}
			state, err := engine.inspectLiteLLMWorkerState(ctx)
			if err != nil {
				return err
			}
			if !state.present || !state.active {
				return fmt.Errorf("LiteLLM worker did not become active after restart")
			}
		}
		return nil
	}); err != nil {
		return engine.operationFailed(journal, "litellm_worker_restore_failed", err)
	}
	return nil
}

func (engine *Engine) disableLiteLLMWorkerBeforeUnsupportedRuntime(
	ctx context.Context,
	journal *Journal,
	releaseDir string,
) error {
	if !journal.LiteLLMWorkerStateCaptured {
		return engine.operationFailed(
			journal,
			"litellm_worker_state_missing",
			fmt.Errorf("captured LiteLLM worker state is missing"),
		)
	}
	if err := engine.runPhase(
		journal,
		"disable_unsupported_litellm_worker",
		func() error {
			supported, err := liteLLMWorkerSupportedByRelease(releaseDir)
			if err != nil {
				return err
			}
			if supported || !journal.LiteLLMWorkerWasEnabled {
				return nil
			}
			return runSystemctl(
				ctx,
				engine.Runner,
				"disable",
				liteLLMWorkerUnitName,
			)
		},
	); err != nil {
		return engine.operationFailed(
			journal,
			"litellm_worker_disable_failed",
			err,
		)
	}
	return nil
}

func (engine *Engine) inspectFeedWorkerState(ctx context.Context) (feedWorkerState, error) {
	return engine.inspectManagedWorkerState(
		ctx,
		feedWorkerUnitName,
		engine.Paths.FeedServiceUnit,
		"feed worker",
	)
}

func (engine *Engine) inspectLiteLLMWorkerState(ctx context.Context) (feedWorkerState, error) {
	return engine.inspectManagedWorkerState(
		ctx,
		liteLLMWorkerUnitName,
		engine.Paths.LiteLLMServiceUnit,
		"LiteLLM worker",
	)
}

func (engine *Engine) inspectManagedWorkerState(
	ctx context.Context,
	unitName string,
	unitPath string,
	label string,
) (feedWorkerState, error) {
	loadState, err := engine.systemdUnitProperty(ctx, unitName, "LoadState")
	if err != nil {
		exists, inspectErr := fileExists(unitPath)
		if inspectErr != nil {
			return feedWorkerState{}, inspectErr
		}
		if !exists {
			return feedWorkerState{}, nil
		}
		return feedWorkerState{}, err
	}
	switch loadState {
	case "not-found":
		return feedWorkerState{}, nil
	case "loaded":
	case "masked":
		return feedWorkerState{}, nil
	default:
		return feedWorkerState{}, fmt.Errorf("%s load state %q is unsupported", label, loadState)
	}

	activeState, err := engine.systemdUnitProperty(ctx, unitName, "ActiveState")
	if err != nil {
		return feedWorkerState{}, err
	}
	state := feedWorkerState{present: true}
	switch activeState {
	case "active", "activating", "reloading":
		state.active = true
	case "inactive", "failed", "deactivating":
	default:
		return feedWorkerState{}, fmt.Errorf("%s active state %q is unsupported", label, activeState)
	}

	unitFileState, err := engine.systemdUnitProperty(ctx, unitName, "UnitFileState")
	if err != nil {
		return feedWorkerState{}, err
	}
	switch unitFileState {
	case "enabled", "enabled-runtime":
		state.enabled = true
	case "disabled", "static", "indirect", "generated", "transient", "linked", "linked-runtime", "alias", "masked", "masked-runtime":
	default:
		return feedWorkerState{}, fmt.Errorf("%s unit-file state %q is unsupported", label, unitFileState)
	}
	return state, nil
}

func (engine *Engine) systemdUnitProperty(ctx context.Context, unit string, property string) (string, error) {
	result, err := engine.Runner.Run(ctx, Command{
		Name: "systemctl",
		Args: []string{"show", unit, "--property", property, "--value"},
	})
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(result.Output)
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("systemd %s property is invalid", property)
	}
	return value, nil
}

func feedWorkerSupportedByRelease(releaseDir string) (bool, error) {
	return managedWorkerSupportedByRelease(releaseDir, "xinsight-feed.service")
}

func liteLLMWorkerSupportedByRelease(releaseDir string) (bool, error) {
	return managedWorkerSupportedByRelease(releaseDir, "xinsight-litellm-feed.service")
}

func managedWorkerSupportedByRelease(releaseDir string, serviceName string) (bool, error) {
	for _, path := range []string{
		filepath.Join(releaseDir, "bin", "xinsight-importer"),
		filepath.Join(releaseDir, "deploy", serviceName),
	} {
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("feed worker release path is not a regular file: %s", filepath.Base(path))
		}
	}
	return true, nil
}

func (engine *Engine) installFeedWorkerUnitForRelease(releaseDir string, operationID string) error {
	return engine.installManagedWorkerUnitForRelease(
		releaseDir,
		operationID,
		"xinsight-feed.service",
		engine.Paths.FeedServiceUnit,
		"feed worker",
		false,
	)
}

func (engine *Engine) installLiteLLMWorkerUnitForRelease(
	releaseDir string,
	operationID string,
) error {
	return engine.installManagedWorkerUnitForRelease(
		releaseDir,
		operationID,
		"xinsight-litellm-feed.service",
		engine.Paths.LiteLLMServiceUnit,
		"LiteLLM worker",
		true,
	)
}

func (engine *Engine) installManagedWorkerUnitForRelease(
	releaseDir string,
	operationID string,
	serviceName string,
	destination string,
	label string,
	removeWhenUnsupported bool,
) error {
	source := filepath.Join(releaseDir, "deploy", serviceName)
	info, err := os.Lstat(source)
	if err != nil {
		if os.IsNotExist(err) {
			if removeWhenUnsupported {
				return removeStaleManagedWorkerUnit(destination, label)
			}
			return nil
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s service source is not a regular file", label)
	}
	return replaceFilePreservingPrevious(source, destination, 0o644, operationID, 1<<20)
}

func removeStaleManagedWorkerUnit(destination string, label string) error {
	info, err := os.Lstat(destination)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s service destination is not a regular file", label)
	}
	if err := os.Remove(destination); err != nil {
		return fmt.Errorf("remove unsupported %s service unit: %w", label, err)
	}
	return nil
}
