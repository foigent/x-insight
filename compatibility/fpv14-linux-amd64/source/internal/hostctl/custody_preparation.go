package hostctl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"strconv"

	"github.com/foigent/x-insight/internal/config"
)

// Retired bindings remain authoritative: disabling capture never loses the key.
func (e *Engine) custodyBinding() (ReviewBodyConfig, bool, bool, error) {
	c, active, err := e.readReviewBodyConfig()
	if active || err != nil {
		return c, active, active, err
	}
	path := e.reviewBodyEnvironment() + ".disabled"
	present, err := fileExists(path)
	if err != nil || !present {
		return c, false, false, err
	}
	if err = e.validateRootReference(path, 0600); err != nil {
		return c, false, true, err
	}
	v, err := readAllowlistedEnvironmentFile(path, true, map[string]struct{}{reviewBodyIDEnv: {}, reviewBodySourceEnv: {}})
	c = ReviewBodyConfig{v[reviewBodyIDEnv], v[reviewBodySourceEnv]}
	if err != nil || len(v) != 2 {
		return c, false, true, fmt.Errorf("retired custody binding is invalid")
	}
	return c, false, true, e.validateReviewBodyCandidate(c)
}

func custodySupported(identity ReleaseIdentity) bool {
	n, err := strconv.Atoi(identity.DatabaseSchema)
	return err == nil && n >= 44
}

// This runs inside the existing install/upgrade maintenance lock and journal.
// A candidate is journaled before any secret is created; resume never overwrites.
func (e *Engine) prepareCustody(ctx context.Context, j *Journal, credentials DatabaseCredentials, identity ReleaseIdentity) error {
	if !custodySupported(identity) {
		return nil
	}
	c, active, present, err := e.custodyBinding()
	if err != nil {
		return fmt.Errorf("restore_original_review_body_key_required: %w", err)
	}
	var recovery *PrivateBackup
	if j.RecoveryBundlePath != "" {
		recovery, err = e.inspectPrivateBackup(ctx, j.RecoveryBundlePath, j.RecoveryBundleSHA256)
		if err != nil {
			return err
		}
		defer recovery.Clear()
	}
	if present {
		if recovery != nil {
			if err = e.checkCustodyPreparation(ctx, credentials, identity, recovery); err != nil {
				return err
			}
		}

		if j.CustodyCandidate != nil && *j.CustodyCandidate != c {
			return fmt.Errorf("custody preparation binding changed")
		}
		if active {
			return e.ValidateReviewBodyConfig()
		}
		return nil
	}
	if err = e.Database.VerifyReviewBodyInventory(ctx, credentials); err != nil {
		return err
	}
	keyPath := filepath.Join(e.Paths.EtcDir, "review-body.key")
	if j.CustodyCandidate == nil {
		if exists, err := fileExists(keyPath); err != nil || exists {
			return fmt.Errorf("restore_original_review_body_key_required: unbound key exists")
		}
		j.CustodyCandidate = &ReviewBodyConfig{KeyID: "xirk_" + j.OperationID, KeyFile: keyPath}
		if err = e.updateJournal(j); err != nil {
			return err
		}
	}
	c = *j.CustodyCandidate
	if c.KeyFile != keyPath {
		return fmt.Errorf("custody preparation candidate path changed")
	}
	if err = e.beginPhase(j, "prepare_review_body_custody"); err != nil {
		return err
	}
	if recovery != nil && recovery.Manifest.Custody != nil && c.KeyID != recovery.Manifest.Custody.KeyID {
		return fmt.Errorf("recovery key candidate changed")
	}

	if exists, err := fileExists(keyPath); err != nil {
		return err
	} else if !exists {
		raw := make([]byte, 32)
		if recovery != nil && recovery.Manifest.Custody != nil {
			copy(raw, recovery.Key)
		} else {
			reader := e.Random
			if reader == nil {
				reader = rand.Reader
			}
			if _, err = io.ReadFull(reader, raw); err != nil {
				return fmt.Errorf("custody entropy unavailable")
			}
		}
		defer clear(raw)
		encoded := []byte(hex.EncodeToString(raw) + "\n")
		defer clear(encoded)
		if err = e.validateReferenceParent(keyPath); err != nil {
			return err
		}
		if err = e.writeCustodyKey(keyPath, encoded); err != nil {
			return err
		}
	}
	if recovery != nil {
		if recovery.Manifest.Custody != nil {
			actual, err := config.LoadReviewBodyKeyFile(keyPath)
			if err != nil {
				return err
			}
			defer clear(actual)
			if !equalSecret(actual, recovery.Key) {
				return fmt.Errorf("recovery key conflicts with existing key")
			}
		}
	}
	if err = e.validateReviewBodyCandidate(c); err != nil {
		return err
	}
	for _, unit := range []string{e.Paths.ServiceUnit, e.Paths.SourceFactsServiceUnit} {
		if err = e.requireUnitDirective(unit, reviewBodyStartup); err != nil {
			return err
		}
	}
	if recovery != nil && recovery.Manifest.Custody != nil && !recovery.Manifest.Custody.Enabled {
		env := []byte(reviewBodyIDEnv + "=" + c.KeyID + "\n" + reviewBodySourceEnv + "=" + c.KeyFile + "\n")
		if err = e.publishReference(e.reviewBodyEnvironment()+".disabled", env, 0600); err != nil {
			return err
		}
		return e.completePhase(j, "prepare_review_body_custody")
	}
	for path, body := range e.reviewBodyFiles(c) {
		if err = e.publishReference(path, body, 0644); err != nil {
			return err
		}
	}
	env := []byte(reviewBodyIDEnv + "=" + c.KeyID + "\n" + reviewBodySourceEnv + "=" + c.KeyFile + "\n")
	if err = e.publishReference(e.reviewBodyEnvironment(), env, 0600); err != nil {
		return err
	}
	if err = e.ValidateReviewBodyConfig(); err != nil {
		return err
	}
	return e.completePhase(j, "prepare_review_body_custody")
}

func (e *Engine) checkCustodyPreparation(ctx context.Context, creds DatabaseCredentials, identity ReleaseIdentity, recovery *PrivateBackup) error {
	if !custodySupported(identity) {
		if recovery != nil {
			return fmt.Errorf("target does not support recovery custody")
		}
		return nil
	}
	c, _, present, err := e.custodyBinding()
	if err != nil {
		return fmt.Errorf("restore_original_review_body_key_required: %w", err)
	}
	if present {
		if recovery != nil && recovery.Manifest.Custody != nil {
			raw, err := config.LoadReviewBodyKeyFile(c.KeyFile)
			if err != nil {
				return err
			}
			defer clear(raw)
			if c.KeyID != recovery.Manifest.Custody.KeyID || !equalSecret(raw, recovery.Key) {
				return fmt.Errorf("recovery key conflicts with target binding")
			}
		}
		return nil
	}
	if exists, err := fileExists(filepath.Join(e.Paths.EtcDir, "review-body.key")); err != nil || exists {
		return fmt.Errorf("restore_original_review_body_key_required: unbound key exists")
	}
	return e.Database.VerifyReviewBodyInventory(ctx, creds)
}
func (e *Engine) CheckInstallWithRecovery(ctx context.Context, artifact, path, hash string) (PreflightReport, error) {
	report, plan, err := e.prepareInstall(ctx, artifact)
	if err != nil {
		return report, err
	}
	var bundle *PrivateBackup
	if path != "" {
		bundle, err = e.inspectPrivateBackup(ctx, path, hash)
		if err != nil {
			return report, err
		}
		defer bundle.Clear()
		if err = validateBackupRelease(bundle, plan.Target); err != nil {
			return report, err
		}
	}
	return report, e.checkCustodyPreparation(ctx, plan.Credentials, plan.Target.Identity, bundle)
}
func (e *Engine) CheckRestoreBundle(ctx context.Context, path, hash, database string) (PreflightReport, error) {
	report, plan, err := e.prepareRestoreMode(ctx, path, hash, database, true)
	if plan.Private != nil {
		plan.Private.Clear()
	}
	return report, err
}

func (e *Engine) writeCustodyKey(path string, encoded []byte) error {
	return writeBytesAtomicExclusive(path, encoded, 0600)
}
