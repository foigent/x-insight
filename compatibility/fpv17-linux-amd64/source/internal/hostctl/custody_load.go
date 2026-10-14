package hostctl

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Normal install preloads credentials. A later explicit full-content policy may
// resume the same bounded host operation when an original retired binding needs
// loading. Read-only preview/status never enters this path.
func (e *Engine) EnsureReviewBodyLoaded(ctx context.Context) error {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return err
	}
	return e.withOperationLock(func() error {
		if j, err := e.readActiveJournal(); err == nil && (j.Status == "active" || j.Status == "failed") {
			if j.Kind != "review_body_load" {
				return fmt.Errorf("another managed operation requires recovery")
			}
			_, err = e.executeCustodyLoad(ctx, &j)
			return err
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := e.proveReviewBodyLoaded(ctx); err == nil {
			return nil
		}
		c, _, present, err := e.custodyBinding()
		if err != nil || !present {
			return fmt.Errorf("restore_original_review_body_key_required")
		}
		if err = e.validateReviewBodyCandidate(c); err != nil {
			return err
		}
		for _, unit := range []string{e.Paths.ServiceUnit, e.Paths.SourceFactsServiceUnit} {
			if err = e.requireUnitDirective(unit, reviewBodyStartup); err != nil {
				return err
			}
		}
		if configured, err := e.hasCustodySourceConfiguration(); err != nil {
			return err
		} else if configured {
			active, err := e.systemdUnitProperty(ctx, sourceFactsWorkerUnit, "ActiveState")
			if err != nil || active != "active" {
				return fmt.Errorf("configured source worker must be active before loading custody")
			}
		}
		state, err := e.readInstallState()
		if err != nil {
			return err
		}
		current, err := e.verifyStateV2(state.Current)
		if err != nil {
			return err
		}
		j, err := e.startJournal("review_body_load", current.Identity.ArtifactPath, "", &current.Identity, &current.Identity)
		if err != nil {
			return err
		}
		j.CustodyCandidate = &c
		if err = e.updateJournal(j); err != nil {
			return err
		}
		_, err = e.executeCustodyLoad(ctx, j)
		return err
	})
}
func (e *Engine) executeCustodyLoad(ctx context.Context, j *Journal) (OperationReceipt, error) {
	fail := func(code string, err error) (OperationReceipt, error) {
		return OperationReceipt{}, e.operationFailed(j, code, err)
	}
	if j.Kind != "review_body_load" || j.Before == nil || j.Target == nil {
		return fail("custody_load_binding_invalid", fmt.Errorf("custody load journal incomplete"))
	}
	current, err := e.verifyStateV2(*j.Before)
	if err != nil {
		return fail("custody_load_release_invalid", err)
	}
	state, err := e.readInstallState()
	if err != nil || state.Current != *j.Before {
		return fail("custody_load_release_changed", fmt.Errorf("current release changed"))
	}
	c, _, present, err := e.custodyBinding()
	if err != nil || !present {
		return fail("restore_original_review_body_key_required", fmt.Errorf("original custody binding required"))
	}
	if j.CustodyCandidate == nil {
		j.CustodyCandidate = &c
		if err = e.updateJournal(j); err != nil {
			return OperationReceipt{}, err
		}
	}
	if c != *j.CustodyCandidate {
		return fail("custody_load_binding_changed", fmt.Errorf("original key binding changed"))
	}
	if err = e.runPhase(j, "load_original_custody_binding", func() error {
		for path, body := range e.reviewBodyFiles(c) {
			if err := e.publishReference(path, body, 0644); err != nil {
				return err
			}
		}
		env := []byte(reviewBodyIDEnv + "=" + c.KeyID + "\n" + reviewBodySourceEnv + "=" + c.KeyFile + "\n")
		if err := e.publishReference(e.reviewBodyEnvironment(), env, 0600); err != nil {
			return err
		}
		return e.ValidateReviewBodyConfig()
	}); err != nil {
		return fail("custody_load_configuration_failed", err)
	}
	// Completed restart phases are not repeated; proof is always freshly read.
	if !containsString(j.CompletedPhases, "reload_custody_units") {
		if err = e.beginPhase(j, "reload_custody_units"); err != nil {
			return OperationReceipt{}, err
		}
		if err = runSystemctl(ctx, e.Runner, "daemon-reload"); err != nil {
			return fail("custody_load_reload_failed", err)
		}
		if err = runSystemctl(ctx, e.Runner, "restart", "xinsight.service"); err != nil {
			return fail("custody_reader_restart_failed", err)
		}
		if configured, err := e.hasCustodySourceConfiguration(); err != nil {
			return fail("custody_source_configuration_failed", err)
		} else if configured {
			active, err := e.systemdUnitProperty(ctx, sourceFactsWorkerUnit, "ActiveState")
			if err != nil || active != "active" {
				return fail("custody_source_inactive", fmt.Errorf("configured source worker must be active before loading custody"))
			}
			if err = runSystemctl(ctx, e.Runner, "restart", sourceFactsWorkerUnit); err != nil {
				return fail("custody_source_restart_failed", err)
			}
		}
		if err = e.completePhase(j, "reload_custody_units"); err != nil {
			return OperationReceipt{}, err
		}
	}
	if err = e.waitForActivationProof(ctx, func() error { return e.proveReviewBodyLoaded(ctx) }); err != nil {
		return fail("custody_loaded_proof_pending", err)
	}
	receipt := OperationReceipt{SchemaVersion: ReceiptSchema, OperationID: j.OperationID, Kind: j.Kind, Status: "completed", Before: j.Before, After: &current.Identity, Checks: []CheckResult{{Name: "review_body_loaded", Status: "pass", Detail: "current_consumers_proved_original_key_bytes"}}, CompletedAt: e.now()}
	if err = e.finalizeOperation(j, &receipt); err != nil {
		return fail("custody_receipt_finalize_failed", err)
	}
	return receipt, nil
}
