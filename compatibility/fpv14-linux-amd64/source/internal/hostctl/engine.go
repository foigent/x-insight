package hostctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/legacyrelease"
	"github.com/foigent/x-insight/internal/releasebundle"
)

type Engine struct {
	Paths                    Paths
	Runner                   CommandRunner
	Database                 DatabaseManager
	CleanBaseline            CleanBaselineDatabaseManager
	AdminCredentialRotation  AdminCredentialRotationDatabase
	HTTPClient               *http.Client
	Now                      func() time.Time
	Wait                     func(context.Context, time.Duration) error
	Random                   io.Reader
	FeedCredentialAuthCheck  func(context.Context, string) error
	FeedCredentialPhaseHook  func(string) error
	CheckpointEpochOpen      func(context.Context, string) (CheckpointEpochRepository, error)
	PrivateBackupCopy        func(io.Writer, io.Reader, int64) (int64, error)
	PrivateBackupPublishHook func() error
}

type verifiedTarget struct {
	Bundle   releasebundle.VerifiedBundle
	Identity ReleaseIdentity
}

func NewEngine(paths Paths) (*Engine, error) {
	if err := paths.Validate(); err != nil {
		return nil, err
	}
	runner := ExecRunner{}
	return &Engine{
		Paths:                   paths,
		Runner:                  runner,
		Database:                PostgresManager{Runner: runner},
		CleanBaseline:           PostgresManager{Runner: runner},
		AdminCredentialRotation: PostgresManager{Runner: runner},
		HTTPClient:              &http.Client{Timeout: 5 * time.Second},
		Now:                     time.Now,
		Wait:                    waitForContext,
		Random:                  nil,
		FeedCredentialAuthCheck: nil,
		FeedCredentialPhaseHook: nil,
		CheckpointEpochOpen:     nil,
	}, nil
}

func waitForContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (engine *Engine) wait(ctx context.Context, duration time.Duration) error {
	if engine.Wait != nil {
		return engine.Wait(ctx, duration)
	}
	return waitForContext(ctx, duration)
}

func (engine *Engine) Status(ctx context.Context) (StatusReport, error) {
	_ = ctx
	report := StatusReport{SchemaVersion: StatusSchema, GeneratedAt: engine.now()}
	state, err := engine.readInstallState()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return StatusReport{}, err
	}
	if err == nil {
		report.Managed = true
		report.InstallState = &state
	}
	if current, err := readAbsoluteSymlink(engine.Paths.CurrentLink); err == nil {
		report.CurrentLink = current
	} else if !errors.Is(err, os.ErrNotExist) {
		return StatusReport{}, err
	}
	if databaseSet, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink); err == nil {
		report.DatabaseSetRef = databaseSet
	} else if !errors.Is(err, os.ErrNotExist) {
		return StatusReport{}, err
	}
	journal, err := engine.readActiveJournal()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return StatusReport{}, err
	}
	if err == nil && (journal.Status == "active" || journal.Status == "failed") {
		report.ActiveJournal = &journal
	}
	return report, nil
}

func (engine *Engine) VerifyArtifact(artifact string) (PreflightReport, error) {
	target, err := engine.verifyTarget(artifact)
	if err != nil {
		return PreflightReport{}, err
	}
	return PreflightReport{
		SchemaVersion: PreflightSchema,
		Operation:     "verify",
		Mutating:      false,
		Supported:     true,
		Target:        &target.Identity,
		Checks: []CheckResult{
			{Name: "artifact_digest", Status: "pass", Detail: target.Bundle.BundleSHA256},
			{Name: "publisher_trust", Status: "pass", Detail: target.Bundle.Manifest.Signature.KeyID},
			{Name: "target_platform", Status: "pass", Detail: "linux_amd64_cgo_disabled"},
			{Name: "manual_identity", Status: "pass", Detail: target.Bundle.Manifest.Manual.Version},
		},
		GeneratedAt: engine.now(),
	}, nil
}

// A running target must support the currently enabled host configuration.
func (engine *Engine) verifyTarget(artifact string) (verifiedTarget, error) {
	target, err := engine.verifySignedArtifact(artifact)
	if err != nil {
		return verifiedTarget{}, err
	}
	if err := engine.validateManagedConfigurationTarget(func(name string) ([]byte, error) { return releasebundle.ReadRuntimeUnit(target.Bundle, name) }); err != nil {
		return verifiedTarget{}, err
	}
	return target, nil
}

// Artifact identity is independent of mutable host configuration. All original
// path, publisher trust, signature, platform and manifest checks remain here.
func (engine *Engine) verifySignedArtifact(artifact string) (verifiedTarget, error) {
	if !filepath.IsAbs(artifact) || filepath.Clean(artifact) != artifact {
		return verifiedTarget{}, fmt.Errorf("release artifact path must be absolute and clean")
	}
	info, err := os.Lstat(artifact)
	if err != nil {
		return verifiedTarget{}, err
	}
	if !info.Mode().IsRegular() {
		return verifiedTarget{}, fmt.Errorf("release artifact must be a regular file, not a link")
	}
	if err := validateTrustStoreFile(engine.Paths.TrustStore); err != nil {
		return verifiedTarget{}, err
	}
	verified, err := releasebundle.Verify(artifact, releasebundle.VerifyOptions{
		TrustStorePath: engine.Paths.TrustStore,
		ExpectedOS:     "linux",
		ExpectedArch:   "amd64",
		Now:            engine.now(),
	})
	if err != nil {
		return verifiedTarget{}, err
	}
	payloadDigest := strings.TrimPrefix(verified.Manifest.ArtifactDigest, "sha256:")
	if len(payloadDigest) < 12 || len(verified.Manifest.Commit) < 12 {
		return verifiedTarget{}, fmt.Errorf("release identity is incomplete")
	}
	releaseDir := filepath.Join(
		engine.Paths.ReleasesDir,
		"v"+verified.Manifest.ProductVersion+"-"+verified.Manifest.Commit[:12]+"-"+payloadDigest[:12],
	)
	identity := ReleaseIdentity{
		ProductVersion:  verified.Manifest.ProductVersion,
		Commit:          verified.Manifest.Commit,
		BundleSHA256:    verified.BundleSHA256,
		ManifestSHA256:  verified.ManifestSHA256,
		PayloadSHA256:   verified.Manifest.ArtifactDigest,
		KeyID:           verified.Manifest.Signature.KeyID,
		ArtifactPath:    artifact,
		ReleaseDir:      releaseDir,
		DatabaseSchema:  verified.Manifest.DatabaseSchema.Current,
		ConfigSchema:    verified.Manifest.ConfigSchema.Current,
		ManualVersion:   verified.Manifest.Manual.Version,
		RollbackAllowed: verified.Manifest.Rollback.ApplicationRollbackCompatible,
	}
	return verifiedTarget{Bundle: verified, Identity: identity}, nil
}

func (engine *Engine) verifyStateV2(identity ReleaseIdentity) (verifiedTarget, error) {
	if identity.LegacyReleaseID != "" {
		return verifiedTarget{}, fmt.Errorf("managed release is a legacy source")
	}
	target, err := engine.verifyTarget(identity.ArtifactPath)
	if err != nil {
		return verifiedTarget{}, err
	}
	if target.Identity != identity {
		return verifiedTarget{}, fmt.Errorf("managed release identity drift")
	}
	return target, nil
}

// Only the historical Before of an upgrade resume uses this verifier: the
// upgrade may already have published configuration unsupported by that release.
// It still requires every recorded identity field to match the signed artifact.
func (engine *Engine) verifyHistoricalStateV2(identity ReleaseIdentity) (verifiedTarget, error) {
	if identity.LegacyReleaseID != "" {
		return verifiedTarget{}, fmt.Errorf("managed release is a legacy source")
	}
	target, err := engine.verifySignedArtifact(identity.ArtifactPath)
	if err != nil {
		return verifiedTarget{}, err
	}
	if target.Identity != identity {
		return verifiedTarget{}, fmt.Errorf("managed release identity drift")
	}
	return target, nil
}

func (engine *Engine) verifyLegacyArtifact(path string, target verifiedTarget) (legacyrelease.Verified, releasebundle.LegacySource, error) {
	var firstError error
	for _, source := range target.Bundle.Manifest.SupportedLegacySources {
		verified, err := legacyrelease.Verify(path, source)
		if err == nil {
			return verified, source, nil
		}
		if firstError == nil {
			firstError = err
		}
	}
	if firstError == nil {
		firstError = fmt.Errorf("target release does not declare a legacy source")
	}
	return legacyrelease.Verified{}, releasebundle.LegacySource{}, firstError
}

func legacyIdentity(path string, directory string, verified legacyrelease.Verified, source releasebundle.LegacySource, rollbackAllowed bool) ReleaseIdentity {
	return ReleaseIdentity{
		ProductVersion:  "legacy",
		LegacyReleaseID: source.ReleaseID,
		Commit:          source.Commit,
		BundleSHA256:    verified.ArtifactSHA256,
		ManifestSHA256:  verified.ManifestSHA256,
		ArtifactPath:    path,
		ReleaseDir:      directory,
		DatabaseSchema:  source.DatabaseSchema,
		ConfigSchema:    source.ConfigSchema,
		RollbackAllowed: rollbackAllowed,
	}
}

func (engine *Engine) readInstallState() (InstallState, error) {
	var state InstallState
	if err := readJSONStrict(engine.Paths.InstallState, &state); err != nil {
		return InstallState{}, err
	}
	if state.SchemaVersion != InstallStateSchema || !state.Managed || state.Current.ReleaseDir == "" ||
		state.DatabaseSetLink != engine.Paths.DatabaseCurrentLink || state.ServiceUnit != engine.Paths.ServiceUnit ||
		state.DatabaseSchemaCurrent == "" || state.ManagedAt.IsZero() || state.UpdatedAt.IsZero() {
		return InstallState{}, fmt.Errorf("managed install state is invalid")
	}
	if !pathWithin(engine.Paths.ReleasesDir, state.Current.ReleaseDir) && state.Current.LegacyReleaseID == "" {
		return InstallState{}, fmt.Errorf("managed current release is outside the release directory")
	}
	return state, nil
}

func (engine *Engine) writeInstallState(state InstallState) error {
	state.SchemaVersion = InstallStateSchema
	state.Managed = true
	state.DatabaseSetLink = engine.Paths.DatabaseCurrentLink
	state.ServiceUnit = engine.Paths.ServiceUnit
	return writeJSONAtomic(engine.Paths.InstallState, state, 0o600)
}

func (engine *Engine) readActiveJournal() (Journal, error) {
	var reference OperationRef
	if err := readJSONStrict(engine.Paths.ActiveOperation, &reference); err != nil {
		return Journal{}, err
	}
	expectedPath := filepath.Join(engine.Paths.OperationsDir, reference.OperationID+".json")
	if reference.SchemaVersion != OperationRefSchema || reference.OperationID == "" || reference.JournalPath != expectedPath {
		return Journal{}, fmt.Errorf("active operation reference is invalid")
	}
	var journal Journal
	if err := readJSONStrict(reference.JournalPath, &journal); err != nil {
		return Journal{}, err
	}
	if journal.SchemaVersion != JournalSchema || journal.OperationID != reference.OperationID || journal.Kind == "" || journal.StartedAt.IsZero() || journal.UpdatedAt.IsZero() {
		return Journal{}, fmt.Errorf("operation journal is invalid")
	}
	return journal, nil
}

func (engine *Engine) startJournal(kind string, artifact string, legacyArtifact string, before *ReleaseIdentity, target *ReleaseIdentity) (*Journal, error) {
	if active, err := engine.readActiveJournal(); err == nil && (active.Status == "active" || active.Status == "failed") {
		return nil, fmt.Errorf("operation %s requires recover, resume, or abort", active.OperationID)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	token, err := randomToken(8)
	if err != nil {
		return nil, err
	}
	now := engine.now()
	operationID := now.Format("20060102T150405Z") + "-" + token
	journal := &Journal{
		SchemaVersion:      JournalSchema,
		OperationID:        operationID,
		Kind:               kind,
		Status:             "active",
		Phase:              "planned",
		ArtifactPath:       artifact,
		LegacyArtifactPath: legacyArtifact,
		Before:             before,
		Target:             target,
		CompletedPhases:    []string{},
		AllowedActions:     []string{"resume", "recover", "abort"},
		StartedAt:          now,
		UpdatedAt:          now,
	}
	path := engine.journalPath(operationID)
	if err := writeJSONExclusive(path, journal, 0o600); err != nil {
		return nil, err
	}
	reference := OperationRef{SchemaVersion: OperationRefSchema, OperationID: operationID, JournalPath: path}
	if err := writeJSONAtomic(engine.Paths.ActiveOperation, reference, 0o600); err != nil {
		return nil, err
	}
	return journal, nil
}

func (engine *Engine) updateJournal(journal *Journal) error {
	journal.UpdatedAt = engine.now()
	if err := writeJSONAtomic(engine.journalPath(journal.OperationID), journal, 0o600); err != nil {
		return err
	}
	reference := OperationRef{SchemaVersion: OperationRefSchema, OperationID: journal.OperationID, JournalPath: engine.journalPath(journal.OperationID)}
	return writeJSONAtomic(engine.Paths.ActiveOperation, reference, 0o600)
}

func (engine *Engine) beginPhase(journal *Journal, phase string) error {
	journal.Status = "active"
	journal.Phase = phase
	journal.ErrorCode = ""
	journal.AllowedActions = []string{"recover", "resume"}
	return engine.updateJournal(journal)
}

func (engine *Engine) completePhase(journal *Journal, phase string) error {
	if !containsString(journal.CompletedPhases, phase) {
		journal.CompletedPhases = append(journal.CompletedPhases, phase)
	}
	if err := engine.updateJournal(journal); err != nil {
		return err
	}
	if engine.Paths.IsTestRoot && os.Getenv("XINSIGHTCTL_FAIL_AFTER_PHASE") == phase {
		return fmt.Errorf("injected failure after phase %s", phase)
	}
	return nil
}

func (engine *Engine) failJournal(journal *Journal, code string, allowed ...string) error {
	journal.Status = "failed"
	journal.ErrorCode = code
	journal.AllowedActions = append([]string(nil), allowed...)
	if len(journal.AllowedActions) == 0 {
		journal.AllowedActions = []string{"recover"}
	}
	return engine.updateJournal(journal)
}

func (engine *Engine) completeJournal(journal *Journal) error {
	now := engine.now()
	journal.Status = "completed"
	journal.Phase = "completed"
	journal.ErrorCode = ""
	journal.AllowedActions = []string{}
	journal.CompletedAt = &now
	return engine.updateJournal(journal)
}

func (engine *Engine) journalPath(operationID string) string {
	return filepath.Join(engine.Paths.OperationsDir, operationID+".json")
}

func (engine *Engine) now() time.Time {
	if engine.Now == nil {
		return time.Now().UTC()
	}
	return engine.Now().UTC().Truncate(time.Second)
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func containsVersion(values []string, expected string) bool {
	return containsString(values, expected)
}

func (engine *Engine) withOperationLock(run func() error) error {
	lock, err := acquireOperationLock(engine.Paths.OperationLock)
	if err != nil {
		return err
	}
	defer lock.Close()
	return run()
}

func (engine *Engine) writeReceipt(receipt OperationReceipt) error {
	path := filepath.Join(engine.Paths.ReceiptsDir, receipt.OperationID+".json")
	if exists, err := fileExists(path); err != nil {
		return err
	} else if exists {
		var current OperationReceipt
		if err := readJSONStrict(path, &current); err != nil {
			return err
		}
		if !reflect.DeepEqual(current, receipt) {
			return fmt.Errorf("existing operation receipt does not match the durable operation result")
		}
		return nil
	}
	return writeJSONExclusive(path, receipt, 0o600)
}
