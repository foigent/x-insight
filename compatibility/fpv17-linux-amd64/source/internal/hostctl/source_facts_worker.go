package hostctl

import (
	"context"
	"fmt"
)

func (engine *Engine) captureAndStopSourceFactsWorker(ctx context.Context, journal *Journal) error {
	const capturePhase = "capture_source_facts_worker_state"
	if !containsString(journal.CompletedPhases, capturePhase) {
		if err := engine.beginPhase(journal, capturePhase); err != nil {
			return engine.operationFailed(journal, "journal_update_failed", err)
		}
		state, err := engine.inspectManagedWorkerState(ctx, sourceFactsWorkerUnit, engine.Paths.SourceFactsServiceUnit, "source facts worker")
		if err != nil {
			return engine.operationFailed(journal, "source_facts_worker_state_inspection_failed", err)
		}
		journal.SourceFactsWorkerStateCaptured = true
		journal.SourceFactsWorkerWasPresent = state.present
		journal.SourceFactsWorkerWasActive = state.active
		journal.SourceFactsWorkerWasEnabled = state.enabled
		if err := engine.updateJournal(journal); err != nil {
			return engine.operationFailed(journal, "journal_update_failed", err)
		}
		if err := engine.completePhase(journal, capturePhase); err != nil {
			return engine.operationFailed(journal, "journal_update_failed", err)
		}
	}
	if !journal.SourceFactsWorkerStateCaptured {
		return engine.operationFailed(journal, "source_facts_worker_state_missing", fmt.Errorf("captured source facts worker state is missing"))
	}
	if err := engine.runPhase(journal, "stop_source_facts_worker", func() error {
		if !journal.SourceFactsWorkerWasPresent {
			return nil
		}
		return runSystemctl(ctx, engine.Runner, "stop", sourceFactsWorkerUnit)
	}); err != nil {
		return engine.operationFailed(journal, "source_facts_worker_stop_failed", err)
	}
	return nil
}

func (engine *Engine) restartSourceFactsWorkerAfterMaintenance(ctx context.Context, journal *Journal, releaseDir string) error {
	if !journal.SourceFactsWorkerStateCaptured {
		return engine.operationFailed(journal, "source_facts_worker_state_missing", fmt.Errorf("captured source facts worker state is missing"))
	}
	err := engine.runPhase(journal, "restore_source_facts_worker_state", func() error {
		supported, err := managedWorkerSupportedByRelease(releaseDir, sourceFactsWorkerUnit)
		if err != nil {
			return err
		}
		if !supported || journal.Kind == "restore" {
			if journal.SourceFactsWorkerWasEnabled {
				return runSystemctl(ctx, engine.Runner, "disable", sourceFactsWorkerUnit)
			}
			return nil
		}
		if err := runSystemctl(ctx, engine.Runner, "daemon-reload"); err != nil {
			return err
		}
		if !journal.SourceFactsWorkerWasActive {
			return nil
		}
		if err := runSystemctl(ctx, engine.Runner, "restart", sourceFactsWorkerUnit); err != nil {
			return err
		}
		state, err := engine.inspectManagedWorkerState(ctx, sourceFactsWorkerUnit, engine.Paths.SourceFactsServiceUnit, "source facts worker")
		if err != nil {
			return err
		}
		if !state.active {
			return fmt.Errorf("source facts worker did not restart")
		}
		return nil
	})
	if err != nil {
		return engine.operationFailed(journal, "source_facts_worker_restore_failed", err)
	}
	if journal.Kind != "restore" && journal.SourceFactsWorkerWasActive {
		_, active, _, err := engine.custodyBinding()
		if err != nil {
			return engine.operationFailed(journal, "source_facts_worker_body_readiness_failed", err)
		}
		if active {
			if err = engine.waitForActivationProof(ctx, func() error { return engine.proveReviewBodyLoaded(ctx) }); err != nil {
				return engine.operationFailed(journal, "source_facts_worker_body_readiness_failed", err)
			}
		}
	}
	return nil
}
