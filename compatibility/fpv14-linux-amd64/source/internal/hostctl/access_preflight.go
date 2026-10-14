package hostctl

import (
	"fmt"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/buildinfo"
)

func targetRequiresAccessAccountCredential(target verifiedTarget) bool {
	return containsString(
		target.Bundle.Manifest.APIVersions,
		buildinfo.AccessAPIVersion,
	)
}

func (engine *Engine) validateAccessAccountCredential(
	target verifiedTarget,
) (bool, error) {
	if !targetRequiresAccessAccountCredential(target) {
		return false, nil
	}
	if _, err := access.ReadAndValidateAccountFile(engine.Paths.AccessAccountsFile); err != nil {
		return true, fmt.Errorf(
			"target access account credential is required and invalid: %w",
			err,
		)
	}
	return true, nil
}

func accessAccountCredentialCheck() CheckResult {
	return CheckResult{
		Name:   "access_account_credential",
		Status: "pass",
		Detail: "root_provisioned_strict_local_accounts_v1",
	}
}

func (engine *Engine) validateResumeAccessAccountCredential(journal Journal) error {
	switch journal.Kind {
	case "install", "adopt", "upgrade", "upgrade_direct_fpv9", "restore":
		target, err := engine.verifyTarget(journal.ArtifactPath)
		if err != nil {
			return err
		}
		_, err = engine.validateAccessAccountCredential(target)
		return err
	case "rollback":
		if journal.Target == nil || journal.Target.LegacyReleaseID != "" {
			return nil
		}
		target, err := engine.verifyTarget(journal.ArtifactPath)
		if err != nil {
			return err
		}
		_, err = engine.validateAccessAccountCredential(target)
		return err
	default:
		return nil
	}
}

func (engine *Engine) validateAbortAccessAccountCredential(journal Journal) error {
	if journal.Kind == "upgrade_direct_fpv9" {
		// The direct-run predecessor is explicitly unsigned. Its exact source
		// proof is checked by the direct abort path before restoring overrides.
		return nil
	}
	if journal.Before == nil || journal.Before.LegacyReleaseID != "" {
		return nil
	}
	target, err := engine.verifyTarget(journal.Before.ArtifactPath)
	if err != nil || target.Identity != *journal.Before {
		return fmt.Errorf("abort source release identity changed")
	}
	_, err = engine.validateAccessAccountCredential(target)
	return err
}
