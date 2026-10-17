package hostctl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const cleanBaselineCredentialFileMaxBytes = int64(1 << 20)

type cleanBaselineCredentialPath struct {
	name   string
	source string
}

func cleanBaselineCredentialPaths(paths Paths) []cleanBaselineCredentialPath {
	return []cleanBaselineCredentialPath{
		{name: "active", source: paths.FeedCredentialFile},
		{name: "staged", source: paths.FeedCredentialNext},
		{name: "operation_state", source: paths.FeedCredentialState},
		{name: "litellm_credential", source: paths.LiteLLMCredential},
		{name: "litellm_environment", source: paths.LiteLLMEnvironment},
	}
}

func inspectCleanBaselineCredentialPreview(paths Paths) (CleanBaselineCredentialPreview, error) {
	if err := ensureNoForeignCleanBaselineCredentialQuarantine(paths, ""); err != nil {
		return CleanBaselineCredentialPreview{}, err
	}
	present := make(map[string]bool, len(cleanBaselineCredentialPaths(paths)))
	for _, item := range cleanBaselineCredentialPaths(paths) {
		exists, err := inspectCleanBaselineCredentialFile(item.source)
		if err != nil {
			return CleanBaselineCredentialPreview{}, fmt.Errorf("connector credential/config %s metadata is invalid: %w", item.name, err)
		}
		present[item.name] = exists
	}
	preview := CleanBaselineCredentialPreview{
		ActivePresent:             present["active"],
		StagedPresent:             present["staged"],
		OperationStatePresent:     present["operation_state"],
		LiteLLMCredentialPresent:  present["litellm_credential"],
		LiteLLMEnvironmentPresent: present["litellm_environment"],
	}
	for _, value := range []bool{
		preview.ActivePresent,
		preview.StagedPresent,
		preview.OperationStatePresent,
		preview.LiteLLMCredentialPresent,
		preview.LiteLLMEnvironmentPresent,
	} {
		if value {
			preview.CanonicalFilesToRetire++
		}
	}
	return preview, nil
}

func inspectCleanBaselineCredentialFile(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		(info.Mode().Perm() != 0o400 && info.Mode().Perm() != 0o600) ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 ||
		info.Size() <= 0 || info.Size() > cleanBaselineCredentialFileMaxBytes {
		return false, fmt.Errorf("file type, mode, or size")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() || uint64(stat.Nlink) != 1 {
		return false, fmt.Errorf("file ownership or link count")
	}
	return true, nil
}

func cleanBaselineCredentialQuarantinePath(source string, operationID string) string {
	return source + ".data-reset-" + operationID + ".quarantine"
}

func quarantineCleanBaselineCredentialState(
	paths Paths,
	operationID string,
	expected CleanBaselineCredentialPreview,
) error {
	if !validCleanBaselineCredentialPreview(expected) {
		return fmt.Errorf("authorized connector credential/config preview is invalid")
	}
	if err := ensureNoForeignCleanBaselineCredentialQuarantine(paths, operationID); err != nil {
		return err
	}
	observed := CleanBaselineCredentialPreview{}
	for _, item := range cleanBaselineCredentialPaths(paths) {
		quarantine := cleanBaselineCredentialQuarantinePath(item.source, operationID)
		sourcePresent, err := inspectCleanBaselineCredentialFile(item.source)
		if err != nil {
			return fmt.Errorf("inspect connector credential/config %s: %w", item.name, err)
		}
		quarantinePresent, err := inspectCleanBaselineCredentialFile(quarantine)
		if err != nil {
			return fmt.Errorf("inspect quarantined connector credential/config %s: %w", item.name, err)
		}
		if sourcePresent && quarantinePresent {
			return fmt.Errorf("connector credential/config %s exists at both canonical and quarantine paths", item.name)
		}
		wasPresent := sourcePresent || quarantinePresent
		switch item.name {
		case "active":
			observed.ActivePresent = wasPresent
		case "staged":
			observed.StagedPresent = wasPresent
		case "operation_state":
			observed.OperationStatePresent = wasPresent
		case "litellm_credential":
			observed.LiteLLMCredentialPresent = wasPresent
		case "litellm_environment":
			observed.LiteLLMEnvironmentPresent = wasPresent
		}
		if wasPresent {
			observed.CanonicalFilesToRetire++
		}
	}
	if observed != expected {
		return fmt.Errorf("connector credential/config presence changed after authorized preview")
	}
	for _, item := range cleanBaselineCredentialPaths(paths) {
		quarantine := cleanBaselineCredentialQuarantinePath(item.source, operationID)
		sourcePresent, err := inspectCleanBaselineCredentialFile(item.source)
		if err != nil {
			return err
		}
		if !sourcePresent {
			continue
		}
		if err := os.Rename(item.source, quarantine); err != nil {
			return fmt.Errorf("quarantine connector credential/config %s: %w", item.name, err)
		}
		if err := syncDirectory(filepath.Dir(item.source)); err != nil {
			return err
		}
	}
	return proveCleanBaselineCredentialAbsent(paths, operationID, false)
}

func restoreCleanBaselineCredentialState(
	paths Paths,
	operationID string,
	expected CleanBaselineCredentialPreview,
) error {
	if !validCleanBaselineCredentialPreview(expected) {
		return fmt.Errorf("authorized connector credential/config preview is invalid")
	}
	observed := CleanBaselineCredentialPreview{}
	for _, item := range cleanBaselineCredentialPaths(paths) {
		quarantine := cleanBaselineCredentialQuarantinePath(item.source, operationID)
		sourcePresent, err := inspectCleanBaselineCredentialFile(item.source)
		if err != nil {
			return err
		}
		quarantinePresent, err := inspectCleanBaselineCredentialFile(quarantine)
		if err != nil {
			return err
		}
		if sourcePresent && quarantinePresent {
			return fmt.Errorf("connector credential/config %s rollback is ambiguous", item.name)
		}
		wasPresent := sourcePresent || quarantinePresent
		switch item.name {
		case "active":
			observed.ActivePresent = wasPresent
		case "staged":
			observed.StagedPresent = wasPresent
		case "operation_state":
			observed.OperationStatePresent = wasPresent
		case "litellm_credential":
			observed.LiteLLMCredentialPresent = wasPresent
		case "litellm_environment":
			observed.LiteLLMEnvironmentPresent = wasPresent
		}
		if wasPresent {
			observed.CanonicalFilesToRetire++
		}
	}
	if observed != expected {
		return fmt.Errorf("connector credential/config rollback state differs from the authorized preview")
	}
	for _, item := range cleanBaselineCredentialPaths(paths) {
		quarantine := cleanBaselineCredentialQuarantinePath(item.source, operationID)
		quarantinePresent, err := inspectCleanBaselineCredentialFile(quarantine)
		if err != nil {
			return err
		}
		if !quarantinePresent {
			continue
		}
		if err := os.Rename(quarantine, item.source); err != nil {
			return fmt.Errorf("restore connector credential/config %s: %w", item.name, err)
		}
		if err := syncDirectory(filepath.Dir(item.source)); err != nil {
			return err
		}
	}
	readback, err := inspectCleanBaselineCredentialPreview(paths)
	if err != nil {
		return err
	}
	if readback != expected {
		return fmt.Errorf("connector credential/config rollback does not match the authorized pre-reset state")
	}
	return nil
}

func retireCleanBaselineCredentialQuarantine(
	paths Paths,
	operationID string,
	expected CleanBaselineCredentialPreview,
) error {
	if !validCleanBaselineCredentialPreview(expected) {
		return fmt.Errorf("authorized connector credential/config preview is invalid")
	}
	if err := proveCleanBaselineCredentialAbsent(paths, operationID, false); err != nil {
		return err
	}
	for _, item := range cleanBaselineCredentialPaths(paths) {
		quarantine := cleanBaselineCredentialQuarantinePath(item.source, operationID)
		present, err := inspectCleanBaselineCredentialFile(quarantine)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		if !cleanBaselineCredentialExpectedPresent(expected, item.name) {
			return fmt.Errorf("unexpected connector credential/config %s quarantine", item.name)
		}
		if err := os.Remove(quarantine); err != nil {
			return fmt.Errorf("retire quarantined connector credential/config %s: %w", item.name, err)
		}
		if err := syncDirectory(filepath.Dir(quarantine)); err != nil {
			return err
		}
	}
	return proveCleanBaselineCredentialAbsent(paths, operationID, true)
}

func cleanBaselineCredentialExpectedPresent(
	expected CleanBaselineCredentialPreview,
	name string,
) bool {
	switch name {
	case "active":
		return expected.ActivePresent
	case "staged":
		return expected.StagedPresent
	case "operation_state":
		return expected.OperationStatePresent
	case "litellm_credential":
		return expected.LiteLLMCredentialPresent
	case "litellm_environment":
		return expected.LiteLLMEnvironmentPresent
	default:
		return false
	}
}

func validCleanBaselineCredentialPreview(preview CleanBaselineCredentialPreview) bool {
	expectedCount := 0
	for _, present := range []bool{
		preview.ActivePresent,
		preview.StagedPresent,
		preview.OperationStatePresent,
		preview.LiteLLMCredentialPresent,
		preview.LiteLLMEnvironmentPresent,
	} {
		if present {
			expectedCount++
		}
	}
	return preview.CanonicalFilesToRetire == expectedCount
}

func proveCleanBaselineCredentialAbsent(paths Paths, operationID string, requireQuarantineAbsent bool) error {
	allowedOperationID := operationID
	if requireQuarantineAbsent {
		allowedOperationID = ""
	}
	if err := ensureNoForeignCleanBaselineCredentialQuarantine(paths, allowedOperationID); err != nil {
		return err
	}
	for _, item := range cleanBaselineCredentialPaths(paths) {
		present, err := inspectCleanBaselineCredentialFile(item.source)
		if err != nil {
			return err
		}
		if present {
			return fmt.Errorf("connector credential/config %s remains at its canonical path", item.name)
		}
		if requireQuarantineAbsent {
			quarantine := cleanBaselineCredentialQuarantinePath(item.source, operationID)
			present, err = inspectCleanBaselineCredentialFile(quarantine)
			if err != nil {
				return err
			}
			if present {
				return fmt.Errorf("connector credential/config %s quarantine remains", item.name)
			}
		}
	}
	return nil
}

func ensureNoForeignCleanBaselineCredentialQuarantine(paths Paths, allowedOperationID string) error {
	seenDirectories := map[string]struct{}{}
	for _, item := range cleanBaselineCredentialPaths(paths) {
		directory := filepath.Dir(item.source)
		if _, seen := seenDirectories[directory]; seen {
			continue
		}
		seenDirectories[directory] = struct{}{}
		entries, err := os.ReadDir(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			for _, candidate := range cleanBaselineCredentialPaths(paths) {
				if filepath.Dir(candidate.source) != directory {
					continue
				}
				prefix := filepath.Base(candidate.source) + ".data-reset-"
				if !strings.HasPrefix(entry.Name(), prefix) ||
					!strings.HasSuffix(entry.Name(), ".quarantine") {
					continue
				}
				allowedName := ""
				if allowedOperationID != "" {
					allowedName = filepath.Base(cleanBaselineCredentialQuarantinePath(candidate.source, allowedOperationID))
				}
				if entry.Name() != allowedName {
					return fmt.Errorf("foreign connector credential/config quarantine state is present")
				}
			}
		}
	}
	return nil
}
