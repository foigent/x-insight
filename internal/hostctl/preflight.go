package hostctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/legacyrelease"
	"github.com/foigent/x-insight/internal/releasebundle"
)

const (
	postgresImageReference = "postgres:16.14-alpine3.24@sha256:57c72fd2a128e416c7fcc499958864df5301e940bca0a56f58fddf30ffc07777"
	minimumFreeHeadroom    = uint64(512 << 20)
)

var sha256ReferencePattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type installPlan struct {
	Target      verifiedTarget
	Credentials DatabaseCredentials
	Facts       DatabaseFacts
}

type upgradePlan struct {
	DirectSource            *DirectFPV9SourceProof
	DirectSourceProofSHA256 string
	State                   InstallState
	CurrentV2               *verifiedTarget
	CurrentLegacy           *legacyrelease.Verified
	LegacySource            *releasebundle.LegacySource
	Target                  verifiedTarget
	Credentials             DatabaseCredentials
	WorkspaceCredentialBase *DatabaseCredentials
	Facts                   DatabaseFacts
}

func (engine *Engine) CheckDirectFPV9Upgrade(ctx context.Context, artifact string, sourceProof string) (PreflightReport, error) {
	report, _, err := engine.prepareDirectFPV9Upgrade(ctx, artifact, sourceProof)
	return report, err
}

func (engine *Engine) prepareDirectFPV9Upgrade(ctx context.Context, artifact string, sourceProofPath string) (PreflightReport, upgradePlan, error) {
	report := PreflightReport{SchemaVersion: PreflightSchema, Operation: "upgrade_direct_fpv9", Mutating: false, GeneratedAt: engine.now()}
	if err := engine.ensureNoOutstandingOperation(); err != nil {
		return report, upgradePlan{}, err
	}
	host, err := detectSupportedHost(engine.Paths)
	if err != nil {
		return report, upgradePlan{}, err
	}
	for _, path := range []string{engine.Paths.InstallState, engine.Paths.CurrentLink, engine.Paths.ManagedServiceDropIn, engine.Paths.ControllerServiceUnit} {
		if exists, err := fileExists(path); err != nil || exists {
			return report, upgradePlan{}, fmt.Errorf("unmanaged FPV9 bridge refuses existing managed state")
		}
	}
	proof, proofDigest, err := readDirectFPV9Proof(sourceProofPath)
	if err != nil {
		return report, upgradePlan{}, err
	}
	target, err := engine.verifyTarget(artifact)
	if err != nil {
		return report, upgradePlan{}, err
	}
	if target.Identity.DatabaseSchema != "43" || target.Identity.ProductVersion != "0.2.0-dev.fpv11" ||
		target.Bundle.Manifest.ReleaseChannel != "release_candidate" || !targetRequiresAccessAccountCredential(target) {
		return report, upgradePlan{}, fmt.Errorf("signed target is not the exact FPV11/schema43 development release")
	}
	if _, err := engine.validateAccessAccountCredential(target); err != nil {
		return report, upgradePlan{}, err
	}
	if len(target.Bundle.Manifest.MigrationChecksums) != 43 {
		return report, upgradePlan{}, fmt.Errorf("signed target migration set is not schema43")
	}
	prefix := make([]db.MigrationRecord, 40)
	for i, checksum := range target.Bundle.Manifest.MigrationChecksums[:40] {
		prefix[i].MigrationChecksum = checksum
	}
	prefixDigest, err := directFPV9LedgerDigest(prefix)
	if err != nil || prefixDigest != directFPV9LedgerSHA256 {
		return report, upgradePlan{}, fmt.Errorf("signed target does not preserve the closed FPV9 migration prefix")
	}
	credentials, needsBridge, err := LoadUpgradeDatabaseCredentials(engine.Paths, false)
	if err != nil || needsBridge {
		return report, upgradePlan{}, fmt.Errorf("unmanaged FPV9 requires the existing four-role credential set")
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil {
		return report, upgradePlan{}, err
	}
	if err := engine.verifyDirectFPV9Source(ctx, proof, facts, true); err != nil {
		return report, upgradePlan{}, err
	}
	if err := engine.verifyDirectFPV9ProxyEnvironment(); err != nil {
		return report, upgradePlan{}, err
	}
	if err := engine.validateConnectorConfiguration(false); err != nil {
		return report, upgradePlan{}, err
	}
	if exists, err := fileExists(engine.Paths.EnvironmentFile); err != nil || exists {
		return report, upgradePlan{}, fmt.Errorf("direct FPV9 bridge requires absent managed environment file")
	}
	if err := engine.verifyStageCollision(target); err != nil {
		return report, upgradePlan{}, err
	}
	if err := engine.checkHostTools(ctx, false, true); err != nil {
		return report, upgradePlan{}, err
	}
	if err := engine.checkDisk(target, facts); err != nil {
		return report, upgradePlan{}, err
	}
	before := ReleaseIdentity{SourceKind: directFPV9Kind, ProductVersion: proof.ProductVersion, Commit: proof.Commit,
		ReleaseDir: filepath.Dir(proof.ServerBinary), DatabaseSchema: "40", ConfigSchema: "xinsight.config.v1"}
	plan := upgradePlan{State: InstallState{Current: before, ManagedAt: engine.now()}, DirectSource: &proof,
		DirectSourceProofSHA256: proofDigest,
		Target:                  target, Credentials: credentials, Facts: facts}
	report.Current, report.Target = &before, &target.Identity
	report.Checks = []CheckResult{
		{Name: "host_profile", Status: "pass", Detail: host.profile()},
		{Name: "source_kind", Status: "pass", Detail: directFPV9Kind},
		{Name: "source_files", Status: "pass", Detail: "exact_reviewed_digests_live_processes_and_egress"},
		{Name: "database", Status: "pass", Detail: "exact_system_id_name_roles_and_40_checksums"},
		{Name: "target_signature", Status: "pass", Detail: target.Identity.KeyID},
		{Name: "capacity", Status: "pass", Detail: "stage_backup_and_headroom_available"},
	}
	report.RequiredActions = []string{"journal and stop three writers", "gate importer and verify owner-only backup",
		"retire three program overrides, carry forward only their exact reviewed egress IP, and preserve two validated credential drop-ins", "apply migrations 41-43 and role grants",
		"activate signed release and prove listener, identity, database, and preserved evidence"}
	report.Supported = true
	return report, plan, nil
}

type adoptPlan struct {
	Target                  verifiedTarget
	Legacy                  legacyrelease.Verified
	LegacySource            releasebundle.LegacySource
	Identity                ReleaseIdentity
	Credentials             DatabaseCredentials
	WorkspaceCredentialBase *DatabaseCredentials
	Facts                   DatabaseFacts
}

type rollbackPlan struct {
	State          InstallState
	Current        verifiedTarget
	PreviousV2     *verifiedTarget
	PreviousLegacy *legacyrelease.Verified
	LegacySource   *releasebundle.LegacySource
	Credentials    DatabaseCredentials
	Facts          DatabaseFacts
}

type restorePlan struct {
	Private     *PrivateBackup
	State       InstallState
	Current     verifiedTarget
	Credentials DatabaseCredentials
	Facts       DatabaseFacts
	Backup      BackupResult
	NewDatabase string
}

func (engine *Engine) CheckInstall(ctx context.Context, artifact string) (PreflightReport, error) {
	return engine.CheckInstallWithRecovery(ctx, artifact, "", "")
}

func (engine *Engine) CheckUpgrade(ctx context.Context, artifact string) (PreflightReport, error) {
	report, plan, err := engine.prepareUpgrade(ctx, artifact)
	if err != nil {
		return report, err
	}
	return report, engine.checkCustodyPreparation(ctx, plan.Credentials, plan.Target.Identity, nil)
}

func (engine *Engine) CheckAdopt(ctx context.Context, targetArtifact string, legacyArtifact string) (PreflightReport, error) {
	report, _, err := engine.prepareAdopt(ctx, targetArtifact, legacyArtifact)
	return report, err
}

func (engine *Engine) CheckRollback(ctx context.Context) (PreflightReport, error) {
	report, _, err := engine.prepareRollback(ctx)
	return report, err
}

func (engine *Engine) CheckRestore(ctx context.Context, backup string, backupSHA256 string, newDatabase string) (PreflightReport, error) {
	report, _, err := engine.prepareRestore(ctx, backup, backupSHA256, newDatabase)
	return report, err
}

func (engine *Engine) prepareInstall(ctx context.Context, artifact string) (PreflightReport, installPlan, error) {
	report := PreflightReport{SchemaVersion: PreflightSchema, Operation: "install", Mutating: false, GeneratedAt: engine.now()}
	if err := engine.ensureNoOutstandingOperation(); err != nil {
		return report, installPlan{}, err
	}
	hostFacts, err := detectSupportedHost(engine.Paths)
	if err != nil {
		return report, installPlan{}, err
	}
	report.Checks = append(report.Checks, CheckResult{Name: "host_profile", Status: "pass", Detail: hostFacts.profile()})
	for _, path := range []string{
		engine.Paths.InstallState,
		engine.Paths.CurrentLink,
		engine.Paths.ServiceUnit,
		engine.Paths.ControllerServiceUnit,
		engine.Paths.ControllerSocket,
		engine.Paths.FeedServiceUnit,
		engine.Paths.ManagedServiceDropIn,
		engine.Paths.EnvironmentFile,
		engine.Paths.ConnectorIdentityEnv,
		engine.Paths.ConnectorEnvironment,
		engine.Paths.ServiceLauncher,
	} {
		if exists, err := fileExists(path); err != nil {
			return report, installPlan{}, err
		} else if exists {
			return report, installPlan{}, fmt.Errorf("fresh install refuses existing managed or unmanaged installation state")
		}
	}
	report.Checks = append(report.Checks, CheckResult{Name: "fresh_install_state", Status: "pass", Detail: "no_existing_state_link_or_service"})
	if err := validateServiceDropIns(engine.Paths); err != nil {
		return report, installPlan{}, err
	}
	report.Checks = append(report.Checks, CheckResult{Name: "service_dropins", Status: "pass", Detail: "none_or_exact_known_contracts"})
	target, err := engine.verifyTarget(artifact)
	if err != nil {
		return report, installPlan{}, err
	}
	accessCredentialRequired, err := engine.validateAccessAccountCredential(target)
	if err != nil {
		return report, installPlan{}, err
	}
	report.Target = &target.Identity
	report.Checks = append(report.Checks,
		CheckResult{Name: "release_trust", Status: "pass", Detail: target.Bundle.Manifest.Signature.KeyID},
		CheckResult{Name: "upgrade_path", Status: "pass", Detail: "fresh_install"},
	)
	if accessCredentialRequired {
		report.Checks = append(report.Checks, accessAccountCredentialCheck())
	}
	if err := engine.verifyStageCollision(target); err != nil {
		return report, installPlan{}, err
	}
	credentials, err := LoadDatabaseCredentials(engine.Paths)
	if err != nil {
		return report, installPlan{}, err
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil {
		return report, installPlan{}, err
	}
	if facts.PostgresMajor != 16 || facts.PublicTableCount != 0 || len(facts.MigrationLedger) != 0 {
		return report, installPlan{}, fmt.Errorf("fresh install requires an empty dedicated PostgreSQL 16 database")
	}
	report.Checks = append(report.Checks,
		CheckResult{Name: "database_profile", Status: "pass", Detail: "postgresql_16_dedicated_empty"},
		CheckResult{Name: "database_role_credentials", Status: "pass", Detail: "reader_importer_admin_distinct"},
	)
	if err := engine.checkHostTools(ctx, true, false); err != nil {
		return report, installPlan{}, err
	}
	report.Checks = append(report.Checks, CheckResult{Name: "host_tools", Status: "pass", Detail: "systemd_ss_docker_compose_postgresql16_clients"})
	if err := engine.checkDisk(target, facts); err != nil {
		return report, installPlan{}, err
	}
	report.Checks = append(report.Checks, CheckResult{Name: "disk_capacity", Status: "pass", Detail: "release_and_database_headroom_available"})
	report.RequiredActions = []string{
		"create durable operation journal",
		"create and verify restricted database roles",
		"stage immutable release and apply checksum-verified migrations",
		"install loopback-only systemd files and activate release",
		"prove listener, compatibility readiness, and bounded search",
	}
	report.Supported = true
	return report, installPlan{Target: target, Credentials: credentials, Facts: facts}, nil
}

func (engine *Engine) prepareUpgrade(ctx context.Context, artifact string) (PreflightReport, upgradePlan, error) {
	report := PreflightReport{SchemaVersion: PreflightSchema, Operation: "upgrade", Mutating: false, GeneratedAt: engine.now()}
	if err := engine.ensureNoOutstandingOperation(); err != nil {
		return report, upgradePlan{}, err
	}
	hostFacts, err := detectSupportedHost(engine.Paths)
	if err != nil {
		return report, upgradePlan{}, err
	}
	state, err := engine.readInstallState()
	if err != nil {
		return report, upgradePlan{}, fmt.Errorf("managed install state is required: %w", err)
	}
	currentLink, err := readAbsoluteSymlink(engine.Paths.CurrentLink)
	if err != nil || currentLink != state.Current.ReleaseDir {
		return report, upgradePlan{}, fmt.Errorf("managed current release pointer drift")
	}
	report.Current = &state.Current
	target, err := engine.verifyTarget(artifact)
	if err != nil {
		return report, upgradePlan{}, err
	}
	accessCredentialRequired, err := engine.validateAccessAccountCredential(target)
	if err != nil {
		return report, upgradePlan{}, err
	}
	if err := validateServiceDropIns(engine.Paths); err != nil {
		return report, upgradePlan{}, err
	}
	if target.Identity.ProductVersion == state.Current.ProductVersion && state.Current.LegacyReleaseID == "" {
		return report, upgradePlan{}, fmt.Errorf("target release is already active")
	}
	report.Target = &target.Identity
	plan := upgradePlan{State: state, Target: target}
	if state.Current.LegacyReleaseID != "" {
		legacy, source, err := engine.verifyLegacyArtifact(state.Current.ArtifactPath, target)
		if err != nil {
			return report, upgradePlan{}, err
		}
		if source.ReleaseID != state.Current.LegacyReleaseID || source.Commit != state.Current.Commit ||
			legacy.ArtifactSHA256 != state.Current.BundleSHA256 || legacy.ManifestSHA256 != state.Current.ManifestSHA256 {
			return report, upgradePlan{}, fmt.Errorf("managed legacy identity is not accepted by target release")
		}
		if err := legacyrelease.VerifyInstalled(legacy, state.Current.ReleaseDir); err != nil {
			return report, upgradePlan{}, err
		}
		plan.CurrentLegacy = &legacy
		plan.LegacySource = &source
		if err := engine.validateLegacyUpgradeRuntime(state); err != nil {
			return report, upgradePlan{}, err
		}
	} else {
		if !containsVersion(target.Bundle.Manifest.SupportedUpgradeFromVersions, state.Current.ProductVersion) {
			return report, upgradePlan{}, fmt.Errorf("unsupported upgrade path; an intermediate release is required")
		}
		current, err := engine.verifyStateV2(state.Current)
		if err != nil {
			return report, upgradePlan{}, err
		}
		if err := releasebundle.VerifyExtractedPayload(current.Bundle, current.Identity.ReleaseDir); err != nil {
			return report, upgradePlan{}, fmt.Errorf("current release payload drift: %w", err)
		}
		if err := validateManagedServiceDropInRuntime(current.Identity.ReleaseDir, engine.Paths); err != nil {
			return report, upgradePlan{}, err
		}
		plan.CurrentV2 = &current
		if err := validateManagedEnvironmentFile(engine.Paths.EnvironmentFile); err != nil {
			return report, upgradePlan{}, err
		}
	}
	if err := engine.validateConnectorConfiguration(false); err != nil {
		return report, upgradePlan{}, err
	}
	if err := engine.verifyStageCollision(target); err != nil {
		return report, upgradePlan{}, err
	}
	allowThreeRoleCredentialBridge := state.Current.LegacyReleaseID != "" ||
		isThreeRoleCredentialUpgradeSource(state.Current.ProductVersion)
	credentials, needsWorkspaceCredentialBridge, err := LoadUpgradeDatabaseCredentials(
		engine.Paths,
		allowThreeRoleCredentialBridge,
	)
	if err != nil {
		return report, upgradePlan{}, err
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil {
		return report, upgradePlan{}, err
	}
	if needsWorkspaceCredentialBridge && facts.WorkspaceRoleExists {
		return report, upgradePlan{}, fmt.Errorf("workspace database role must be absent before credential bridge")
	}
	if facts.PostgresMajor != 16 || !facts.AdminRoleIsSuperuser || !facts.ReaderRoleExists || !facts.ImporterRoleExists ||
		(!needsWorkspaceCredentialBridge && !facts.WorkspaceRoleExists) {
		return report, upgradePlan{}, fmt.Errorf("upgrade requires PostgreSQL 16 and the declared database role boundary")
	}
	var currentMigrations []db.MigrationChecksum
	if plan.LegacySource != nil {
		currentMigrations = plan.LegacySource.MigrationChecksums
	} else {
		currentMigrations = plan.CurrentV2.Bundle.Manifest.MigrationChecksums
	}
	if err := validateLedgerPrefix(facts.MigrationLedger, currentMigrations, false); err != nil {
		if state.DatabaseSchemaCurrent != target.Identity.DatabaseSchema || validateLedgerPrefix(facts.MigrationLedger, target.Bundle.Manifest.MigrationChecksums, false) != nil {
			return report, upgradePlan{}, err
		}
	}
	if err := engine.Database.VerifyUpgradeSourceRoleBoundaries(
		ctx,
		credentials,
		state.Current.ProductVersion,
	); err != nil {
		return report, upgradePlan{}, err
	}
	if targetSchema, parseErr := strconv.Atoi(target.Identity.DatabaseSchema); parseErr == nil && targetSchema >= 45 {
		preflight, ok := engine.Database.(interface {
			VerifyControlCredentialLifecycle(context.Context, DatabaseCredentials) error
		})
		if !ok {
			return report, upgradePlan{}, fmt.Errorf("control credential preflight is unavailable")
		}
		if err := preflight.VerifyControlCredentialLifecycle(ctx, credentials); err != nil {
			return report, upgradePlan{}, err
		}
		statusReader, ok := engine.Database.(interface {
			InspectControlCredentialLifecycle(context.Context, DatabaseCredentials, time.Time) ([]ControlCredentialStatus, error)
		})
		if !ok {
			return report, upgradePlan{}, fmt.Errorf("control credential runtime inspection is unavailable")
		}
		report.ControlCredentials, err = statusReader.InspectControlCredentialLifecycle(ctx, credentials, engine.now())
		if err != nil {
			return report, upgradePlan{}, err
		}
		report.Checks = append(report.Checks, CheckResult{Name: "control_credential_lifecycle", Status: "pass", Detail: "unique_original_lineage_and_current_pointer_verified"})
	}
	if needsWorkspaceCredentialBridge {
		base := credentials
		plan.WorkspaceCredentialBase = &base
	}
	plan.Credentials = credentials
	plan.Facts = facts
	if err := engine.checkHostTools(ctx, false, true); err != nil {
		return report, upgradePlan{}, err
	}
	if err := engine.checkDisk(target, facts); err != nil {
		return report, upgradePlan{}, err
	}
	databaseRoleDetail := "reader_workspace_importer_admin_boundary_verified"
	if needsWorkspaceCredentialBridge {
		databaseRoleDetail = "legacy_reader_importer_admin_boundary_verified_workspace_bridge_required"
	}
	report.Checks = append(report.Checks,
		CheckResult{Name: "host_profile", Status: "pass", Detail: hostFacts.profile()},
		CheckResult{Name: "managed_state", Status: "pass", Detail: "state_pointer_artifact_match"},
		CheckResult{Name: "service_dropins", Status: "pass", Detail: "none_or_exact_known_contracts"},
		CheckResult{Name: "release_trust", Status: "pass", Detail: target.Bundle.Manifest.Signature.KeyID},
		CheckResult{Name: "upgrade_path", Status: "pass", Detail: state.Current.ProductVersion + "_to_" + target.Identity.ProductVersion},
		CheckResult{Name: "database_profile", Status: "pass", Detail: "postgresql_16_checksum_ledger_clean"},
		CheckResult{Name: "database_roles", Status: "pass", Detail: databaseRoleDetail},
		CheckResult{Name: "backup_tools", Status: "pass", Detail: "pg_dump_16_pg_restore_16"},
		CheckResult{Name: "disk_capacity", Status: "pass", Detail: "immutable_stage_backup_and_headroom_available"},
	)
	if accessCredentialRequired {
		report.Checks = append(report.Checks, accessAccountCredentialCheck())
	}
	report.RequiredActions = []string{
		"stop service and gate importer login",
		"hold the exclusive database maintenance session",
		"stage release, create and verify owner-only backup",
	}
	if needsWorkspaceCredentialBridge {
		report.RequiredActions = append(report.RequiredActions,
			"stage an immutable four-role database credential successor and create the least-privilege workspace role",
		)
	}
	report.RequiredActions = append(report.RequiredActions,
		"apply forward-only checksum migrations and activate atomically",
		"prove listener, release/schema/manual identity, and bounded read",
	)
	report.Supported = true
	return report, plan, nil
}

func isThreeRoleCredentialUpgradeSource(productVersion string) bool {
	switch productVersion {
	case "0.2.0-rc.3", "0.2.0-rc.4", "0.2.0-rc.5", "0.2.0-rc.6",
		"0.2.0-rc.7", "0.2.0-rc.8", "0.2.0-rc.9", "0.2.0-rc.10",
		"0.2.0-rc.11", "0.2.0-rc.12", "0.2.0-rc.13", "0.2.0-rc.14",
		"0.2.0-rc.15":
		return true
	default:
		return false
	}
}

func (engine *Engine) validateLegacyUpgradeRuntime(state InstallState) error {
	if err := validateLegacyEnvironmentFile(engine.Paths.EnvironmentFile); err == nil {
		return nil
	}
	if state.Previous == nil || state.Previous.LegacyReleaseID != "" {
		return fmt.Errorf("legacy runtime configuration is not accepted for managed re-upgrade")
	}
	previous, err := engine.verifyStateV2(*state.Previous)
	if err != nil {
		return fmt.Errorf("managed legacy bridge release is invalid: %w", err)
	}
	if err := validateManagedEnvironmentFile(engine.Paths.EnvironmentFile); err != nil {
		return fmt.Errorf("managed legacy bridge environment is invalid: %w", err)
	}
	for _, pair := range [][2]string{
		{filepath.Join(previous.Identity.ReleaseDir, "bin", "xinsight-launcher"), engine.Paths.ServiceLauncher},
		{filepath.Join(previous.Identity.ReleaseDir, "deploy", "xinsight.service"), engine.Paths.ServiceUnit},
		{filepath.Join(previous.Identity.ReleaseDir, "deploy", "postgres.compose.yml"), engine.Paths.PostgresCompose},
	} {
		payloadPath, err := filepath.Rel(previous.Identity.ReleaseDir, pair[0])
		if err != nil {
			return err
		}
		if err := requireMatchingFiles(pair[0], pair[1], releasebundle.RuntimeFileLimit(filepath.ToSlash(payloadPath))); err != nil {
			return fmt.Errorf("managed legacy bridge runtime drift: %w", err)
		}
	}
	return validateManagedServiceDropInRuntime(previous.Identity.ReleaseDir, engine.Paths)
}

func (engine *Engine) prepareAdopt(ctx context.Context, targetArtifact string, legacyArtifact string) (PreflightReport, adoptPlan, error) {
	report := PreflightReport{SchemaVersion: PreflightSchema, Operation: "adopt", Mutating: false, GeneratedAt: engine.now()}
	if err := engine.ensureNoOutstandingOperation(); err != nil {
		return report, adoptPlan{}, err
	}
	hostFacts, err := detectSupportedHost(engine.Paths)
	if err != nil {
		return report, adoptPlan{}, err
	}
	if exists, err := fileExists(engine.Paths.InstallState); err != nil {
		return report, adoptPlan{}, err
	} else if exists {
		return report, adoptPlan{}, fmt.Errorf("installation is already managed")
	}
	currentDir, err := readAbsoluteSymlink(engine.Paths.CurrentLink)
	if err != nil || !pathWithin(engine.Paths.ReleasesDir, currentDir) {
		return report, adoptPlan{}, fmt.Errorf("legacy current release pointer is invalid")
	}
	if exists, err := fileExists(engine.Paths.ServiceUnit); err != nil || !exists {
		return report, adoptPlan{}, fmt.Errorf("legacy systemd service unit is required")
	}
	if err := validateLegacyEnvironmentFile(engine.Paths.EnvironmentFile); err != nil {
		return report, adoptPlan{}, err
	}
	if err := engine.validateConnectorConfiguration(false); err != nil {
		return report, adoptPlan{}, err
	}
	if err := validateServiceDropIns(engine.Paths); err != nil {
		return report, adoptPlan{}, err
	}
	if exists, err := fileExists(engine.Paths.ManagedServiceDropIn); err != nil {
		return report, adoptPlan{}, err
	} else if exists {
		return report, adoptPlan{}, fmt.Errorf("legacy adoption cannot inherit a managed systemd service drop-in")
	}
	target, err := engine.verifyTarget(targetArtifact)
	if err != nil {
		return report, adoptPlan{}, err
	}
	accessCredentialRequired, err := engine.validateAccessAccountCredential(target)
	if err != nil {
		return report, adoptPlan{}, err
	}
	legacy, source, err := engine.verifyLegacyArtifact(legacyArtifact, target)
	if err != nil {
		return report, adoptPlan{}, err
	}
	if err := legacyrelease.VerifyInstalled(legacy, currentDir); err != nil {
		return report, adoptPlan{}, err
	}
	identity := legacyIdentity(legacyArtifact, currentDir, legacy, source, target.Bundle.Manifest.Rollback.ApplicationRollbackCompatible)
	report.Current = &identity
	report.Target = &target.Identity
	credentials, needsWorkspaceCredentialBridge, err := LoadUpgradeDatabaseCredentials(engine.Paths, true)
	if err != nil {
		return report, adoptPlan{}, err
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil {
		return report, adoptPlan{}, err
	}
	if needsWorkspaceCredentialBridge && facts.WorkspaceRoleExists {
		return report, adoptPlan{}, fmt.Errorf("workspace database role must be absent before credential bridge")
	}
	if facts.PostgresMajor != 16 || !facts.AdminRoleIsSuperuser {
		return report, adoptPlan{}, fmt.Errorf("legacy adoption requires PostgreSQL 16 and an xinsight_admin superuser")
	}
	if err := validateLedgerPrefix(facts.MigrationLedger, source.MigrationChecksums, true); err != nil {
		return report, adoptPlan{}, err
	}
	if err := engine.checkHostTools(ctx, false, false); err != nil {
		return report, adoptPlan{}, err
	}
	roleCredentialDetail := "reader_workspace_importer_admin_distinct"
	if needsWorkspaceCredentialBridge {
		roleCredentialDetail = "legacy_reader_importer_admin_distinct_workspace_bridge_required"
	}
	report.Checks = append(report.Checks,
		CheckResult{Name: "host_profile", Status: "pass", Detail: hostFacts.profile()},
		CheckResult{Name: "service_dropins", Status: "pass", Detail: "none_or_exact_known_contracts"},
		CheckResult{Name: "legacy_artifact", Status: "pass", Detail: source.ReleaseID},
		CheckResult{Name: "installed_payload", Status: "pass", Detail: "exact_legacy_file_set_match"},
		CheckResult{Name: "migration_prefix", Status: "pass", Detail: "exact_allowlisted_legacy_schema"},
		CheckResult{Name: "role_credentials", Status: "pass", Detail: roleCredentialDetail},
	)
	if accessCredentialRequired {
		report.Checks = append(report.Checks, accessAccountCredentialCheck())
	}
	report.RequiredActions = []string{
		"stop the legacy service and prove all legacy writer sessions absent",
		"create/harden restricted reader, workspace, and importer roles",
		"adopt exact signed legacy migration checksums",
		"write managed state and keep the legacy service stopped until upgrade",
	}
	report.Supported = true
	plan := adoptPlan{Target: target, Legacy: legacy, LegacySource: source, Identity: identity, Credentials: credentials, Facts: facts}
	if needsWorkspaceCredentialBridge {
		base := credentials
		plan.WorkspaceCredentialBase = &base
	}
	return report, plan, nil
}

func (engine *Engine) prepareRollback(ctx context.Context) (PreflightReport, rollbackPlan, error) {
	report := PreflightReport{SchemaVersion: PreflightSchema, Operation: "rollback", Mutating: false, GeneratedAt: engine.now()}
	if err := engine.ensureNoOutstandingOperation(); err != nil {
		return report, rollbackPlan{}, err
	}
	if _, err := detectSupportedHost(engine.Paths); err != nil {
		return report, rollbackPlan{}, err
	}
	state, err := engine.readInstallState()
	if err != nil || state.Previous == nil {
		return report, rollbackPlan{}, fmt.Errorf("managed previous release is required for application rollback")
	}
	currentLink, err := readAbsoluteSymlink(engine.Paths.CurrentLink)
	if err != nil || currentLink != state.Current.ReleaseDir {
		return report, rollbackPlan{}, fmt.Errorf("managed current release pointer drift")
	}
	current, err := engine.verifyStateV2(state.Current)
	if err != nil || !current.Bundle.Manifest.Rollback.ApplicationRollbackCompatible {
		return report, rollbackPlan{}, fmt.Errorf("current release does not declare application rollback compatibility")
	}
	if err := releasebundle.VerifyExtractedPayload(current.Bundle, state.Current.ReleaseDir); err != nil {
		return report, rollbackPlan{}, err
	}
	if err := validateServiceDropIns(engine.Paths); err != nil {
		return report, rollbackPlan{}, err
	}
	if err := validateManagedServiceDropInRuntime(current.Identity.ReleaseDir, engine.Paths); err != nil {
		return report, rollbackPlan{}, err
	}
	if err := validateManagedEnvironmentFile(engine.Paths.EnvironmentFile); err != nil {
		return report, rollbackPlan{}, err
	}
	if err := engine.validateConnectorConfiguration(false); err != nil {
		return report, rollbackPlan{}, err
	}
	plan := rollbackPlan{State: state, Current: current}
	accessCredentialRequired := false
	previous := *state.Previous
	if err := engine.validateManagedConfigurationRelease(previous.ReleaseDir); err != nil {
		return report, rollbackPlan{}, err
	}
	if previous.LegacyReleaseID != "" {
		legacy, source, err := engine.verifyLegacyArtifact(previous.ArtifactPath, current)
		if err != nil {
			return report, rollbackPlan{}, err
		}
		if source.ReleaseID != previous.LegacyReleaseID || source.Commit != previous.Commit || legacy.ArtifactSHA256 != previous.BundleSHA256 || legacy.ManifestSHA256 != previous.ManifestSHA256 {
			return report, rollbackPlan{}, fmt.Errorf("previous legacy release identity drift")
		}
		if err := legacyrelease.VerifyInstalled(legacy, previous.ReleaseDir); err != nil {
			return report, rollbackPlan{}, err
		}
		if err := verifyLegacyRollbackCompatibility(source, state.DatabaseSchemaCurrent, state.Current.ConfigSchema); err != nil {
			return report, rollbackPlan{}, err
		}
		plan.PreviousLegacy = &legacy
		plan.LegacySource = &source
	} else {
		verifiedPrevious, err := engine.verifyStateV2(previous)
		if err != nil {
			return report, rollbackPlan{}, err
		}
		if err := releasebundle.VerifyExtractedPayload(verifiedPrevious.Bundle, previous.ReleaseDir); err != nil {
			return report, rollbackPlan{}, err
		}
		accessCredentialRequired, err = engine.validateAccessAccountCredential(verifiedPrevious)
		if err != nil {
			return report, rollbackPlan{}, err
		}
		currentSchema, err := strconv.Atoi(state.DatabaseSchemaCurrent)
		minimum, minErr := strconv.Atoi(verifiedPrevious.Bundle.Manifest.ApplicationCompatibility.DatabaseSchemaMin)
		maximum, maxErr := strconv.Atoi(verifiedPrevious.Bundle.Manifest.ApplicationCompatibility.DatabaseSchemaMax)
		if err != nil || minErr != nil || maxErr != nil || currentSchema < minimum || currentSchema > maximum {
			return report, rollbackPlan{}, fmt.Errorf("previous application does not support the current database schema")
		}
		plan.PreviousV2 = &verifiedPrevious
	}
	credentials, err := LoadDatabaseCredentials(engine.Paths)
	if err != nil {
		return report, rollbackPlan{}, err
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil || facts.PostgresMajor != 16 {
		return report, rollbackPlan{}, fmt.Errorf("rollback requires the supported PostgreSQL 16 database")
	}
	if err := validateLedgerPrefix(facts.MigrationLedger, current.Bundle.Manifest.MigrationChecksums, false); err != nil {
		return report, rollbackPlan{}, err
	}
	if err := engine.Database.VerifyRoleBoundaries(ctx, credentials); err != nil {
		return report, rollbackPlan{}, err
	}
	if err := engine.checkHostTools(ctx, false, false); err != nil {
		return report, rollbackPlan{}, err
	}
	plan.Credentials = credentials
	plan.Facts = facts
	report.Current = &state.Current
	report.Target = state.Previous
	report.Checks = append(report.Checks,
		CheckResult{Name: "current_release", Status: "pass", Detail: "signed_active_release_verified"},
		CheckResult{Name: "previous_release", Status: "pass", Detail: "exact_immutable_previous_verified"},
		CheckResult{Name: "service_dropins", Status: "pass", Detail: "none_or_exact_known_contracts"},
		CheckResult{Name: "database_schema", Status: "pass", Detail: "forward_schema_declared_compatible"},
		CheckResult{Name: "rollback_mode", Status: "pass", Detail: "application_pointer_only_no_down_migration"},
	)
	if accessCredentialRequired {
		report.Checks = append(report.Checks, accessAccountCredentialCheck())
	}
	report.RequiredActions = []string{
		"stop service and gate importer login",
		"hold exclusive maintenance session",
		"atomically reactivate exact previous release without down migration",
		"restart and prove loopback listener and representative evidence integrity",
	}
	report.Supported = true
	return report, plan, nil
}

func (engine *Engine) prepareRestore(ctx context.Context, backup string, backupSHA256 string, newDatabase string) (PreflightReport, restorePlan, error) {
	return engine.prepareRestoreMode(ctx, backup, backupSHA256, newDatabase, false)
}

func (engine *Engine) prepareRestoreMode(ctx context.Context, backup string, backupSHA256 string, newDatabase string, private bool) (PreflightReport, restorePlan, error) {
	report := PreflightReport{SchemaVersion: PreflightSchema, Operation: "restore", Mutating: false, GeneratedAt: engine.now()}
	if err := engine.ensureNoOutstandingOperation(); err != nil {
		return report, restorePlan{}, err
	}
	if _, err := detectSupportedHost(engine.Paths); err != nil {
		return report, restorePlan{}, err
	}
	if !filepath.IsAbs(backup) || filepath.Clean(backup) != backup || !sha256ReferencePattern.MatchString(backupSHA256) || !restoreDatabasePattern.MatchString(newDatabase) {
		return report, restorePlan{}, fmt.Errorf("restore requires an absolute backup path, exact SHA-256, and xinsight_restore_* database name")
	}
	state, err := engine.readInstallState()
	if err != nil || state.Current.LegacyReleaseID != "" {
		return report, restorePlan{}, fmt.Errorf("database restore requires a managed SemVer application release")
	}
	currentLink, err := readAbsoluteSymlink(engine.Paths.CurrentLink)
	if err != nil || currentLink != state.Current.ReleaseDir {
		return report, restorePlan{}, fmt.Errorf("managed current release pointer drift")
	}
	current, err := engine.verifyStateV2(state.Current)
	if err != nil {
		return report, restorePlan{}, err
	}
	accessCredentialRequired, err := engine.validateAccessAccountCredential(current)
	if err != nil {
		return report, restorePlan{}, err
	}
	if err := releasebundle.VerifyExtractedPayload(current.Bundle, current.Identity.ReleaseDir); err != nil {
		return report, restorePlan{}, err
	}
	if err := validateServiceDropIns(engine.Paths); err != nil {
		return report, restorePlan{}, err
	}
	if err := validateManagedServiceDropInRuntime(current.Identity.ReleaseDir, engine.Paths); err != nil {
		return report, restorePlan{}, err
	}
	if err := validateManagedEnvironmentFile(engine.Paths.EnvironmentFile); err != nil {
		return report, restorePlan{}, err
	}
	if err := engine.validateConnectorConfiguration(false); err != nil {
		return report, restorePlan{}, err
	}
	credentials, err := LoadDatabaseCredentials(engine.Paths)
	if err != nil {
		return report, restorePlan{}, err
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil || facts.PostgresMajor != 16 || validateLedgerPrefix(facts.MigrationLedger, current.Bundle.Manifest.MigrationChecksums, false) != nil {
		return report, restorePlan{}, fmt.Errorf("restore source database is not a compatible PostgreSQL 16 release state")
	}
	if exists, err := engine.Database.DatabaseExists(ctx, credentials, newDatabase); err != nil {
		return report, restorePlan{}, err
	} else if exists {
		return report, restorePlan{}, fmt.Errorf("restore target database already exists; in-place overwrite is forbidden")
	}
	var bundle *PrivateBackup
	var backupResult BackupResult
	if private {
		bundle, err = engine.inspectPrivateBackup(ctx, backup, backupSHA256)
		if err == nil {
			err = validateBackupRelease(bundle, current)
		}
		if err == nil {
			err = engine.checkBackupKey(bundle)
		}
		if err == nil {
			backupResult = BackupResult{Path: backup, SHA256: backupSHA256, SizeBytes: bundle.Size, PGRestoreMajor: 16}
		}
	} else {
		backupResult, err = engine.Database.VerifyBackup(ctx, backup, backupSHA256)
	}
	if err != nil {
		return report, restorePlan{}, err
	}
	if err := engine.checkHostTools(ctx, false, true); err != nil {
		return report, restorePlan{}, err
	}
	path, err := nearestExistingPath(engine.Paths.DatabaseSetsDir)
	if err != nil {
		return report, restorePlan{}, err
	}
	available, err := availableBytes(path)
	if err != nil || available < uint64(maxInt64(facts.DatabaseSizeBytes, 0))*2+uint64(backupResult.SizeBytes)+minimumFreeHeadroom {
		return report, restorePlan{}, fmt.Errorf("insufficient disk for isolated restore and safety headroom")
	}
	structure := "pg_restore_16_list_ok"
	if private {
		structure = "private_members_and_hashes_verified_inner_pg_restore_list_checked_before_restore"
	}
	report.Current = &state.Current
	report.Target = &state.Current
	report.Checks = append(report.Checks,
		CheckResult{Name: "backup_hash", Status: "pass", Detail: backupResult.SHA256},
		CheckResult{Name: "service_dropins", Status: "pass", Detail: "none_or_exact_known_contracts"},
		CheckResult{Name: "backup_structure", Status: "pass", Detail: structure},
		CheckResult{Name: "restore_target", Status: "pass", Detail: "new_empty_database_name_available"},
		CheckResult{Name: "restore_mode", Status: "pass", Detail: "restore_to_new_database_atomic_credential_set_switch"},
		CheckResult{Name: "disk_capacity", Status: "pass", Detail: "isolated_restore_and_headroom_available"},
	)
	if accessCredentialRequired {
		report.Checks = append(report.Checks, accessAccountCredentialCheck())
	}
	report.RequiredActions = []string{
		"stop service and gate importer login",
		"create a new versioned credential set without changing the active link",
		"create and restore one new empty PostgreSQL database",
		"verify schema, roles, and representative evidence fingerprint",
		"atomically switch database credential set and prove the unchanged application release",
	}
	report.Supported = true
	return report, restorePlan{State: state, Current: current, Credentials: credentials, Facts: facts, Backup: backupResult, NewDatabase: newDatabase, Private: bundle}, nil
}

func (engine *Engine) ensureNoOutstandingOperation() error {
	journal, err := engine.readActiveJournal()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if journal.Status == "active" || journal.Status == "failed" {
		return fmt.Errorf("operation %s requires recover, resume, or abort", journal.OperationID)
	}
	return nil
}

func (engine *Engine) verifyStageCollision(target verifiedTarget) error {
	exists, err := fileExists(target.Identity.ReleaseDir)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := releasebundle.VerifyExtractedPayload(target.Bundle, target.Identity.ReleaseDir); err != nil {
		return fmt.Errorf("target release directory collision or drift: %w", err)
	}
	return nil
}

func (engine *Engine) checkHostTools(ctx context.Context, requireDocker bool, requireBackup bool) error {
	for _, command := range []Command{
		{Name: "systemctl", Args: []string{"--version"}},
		{Name: "ss", Args: []string{"--version"}},
	} {
		if _, err := engine.Runner.Run(ctx, command); err != nil {
			return err
		}
	}
	if requireDocker {
		if _, err := engine.Runner.Run(ctx, Command{Name: "docker", Args: []string{"compose", "version"}}); err != nil {
			return err
		}
		if _, err := engine.Runner.Run(ctx, Command{Name: "docker", Args: []string{"image", "inspect", postgresImageReference}}); err != nil {
			return fmt.Errorf("pinned PostgreSQL image is not available locally")
		}
	}
	if requireBackup {
		for _, name := range []string{"pg_dump", "pg_restore", "createdb"} {
			result, err := engine.Runner.Run(ctx, Command{Name: name, Args: []string{"--version"}})
			if err != nil {
				return err
			}
			major, err := parseMajorVersion(result.Output, name)
			if err != nil || major != 16 {
				return fmt.Errorf("%s 16 is required", name)
			}
		}
	}
	return nil
}

func (engine *Engine) checkDisk(target verifiedTarget, facts DatabaseFacts) error {
	path, err := nearestExistingPath(engine.Paths.ReleasesDir)
	if err != nil {
		return err
	}
	available, err := availableBytes(path)
	if err != nil {
		return err
	}
	required := uint64(target.Bundle.Manifest.ArtifactSizeBytes) + uint64(maxInt64(facts.DatabaseSizeBytes, 0))*2 + minimumFreeHeadroom
	if available < required {
		return fmt.Errorf("insufficient disk for release staging, verified backup, and safety headroom")
	}
	return nil
}

func nearestExistingPath(path string) (string, error) {
	for {
		info, err := os.Lstat(path)
		if err == nil {
			if info.IsDir() {
				return path, nil
			}
			return filepath.Dir(path), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", fmt.Errorf("no existing parent for disk inspection")
		}
		path = parent
	}
}

func validateLedgerPrefix(records []db.MigrationRecord, expected []db.MigrationChecksum, allowMissingChecksum bool) error {
	if len(records) != len(expected) {
		return fmt.Errorf("database migration ledger is not the exact expected prefix")
	}
	for index, record := range records {
		if record.ID != expected[index].ID || record.Dirty {
			return fmt.Errorf("database migration ledger is dirty or out of order")
		}
		if record.SHA256 == "" && allowMissingChecksum {
			continue
		}
		if record.SHA256 != expected[index].SHA256 {
			return fmt.Errorf("database migration checksum drift")
		}
	}
	return nil
}

func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func databaseSchemaNumber(identity ReleaseIdentity) (int, error) {
	value, err := strconv.Atoi(identity.DatabaseSchema)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("release database schema identity is invalid")
	}
	return value, nil
}

func migrationIDs(values []db.MigrationChecksum) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.ID
	}
	return result
}

func safeDetail(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 256 {
		return value[:256]
	}
	return value
}

func verifyLegacyRollbackCompatibility(source releasebundle.LegacySource, databaseSchema string, configSchema string) error {
	current, currentErr := strconv.Atoi(databaseSchema)
	minimum, minErr := strconv.Atoi(source.ApplicationDatabaseSchemaMin)
	maximum, maxErr := strconv.Atoi(source.ApplicationDatabaseSchemaMax)
	if currentErr != nil || minErr != nil || maxErr != nil || current < minimum || current > maximum || !containsString(source.CompatibleConfigSchemas, configSchema) {
		return fmt.Errorf("legacy application is not signed-compatible with the current database and runtime config schema")
	}
	return nil
}
