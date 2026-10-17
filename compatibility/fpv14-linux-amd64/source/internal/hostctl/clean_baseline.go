package hostctl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/releasebundle"
)

const (
	CleanBaselinePreviewSchema   = "xinsight.data-reset-preview.v1"
	CleanBaselineReceiptSchema   = "xinsight.data-reset-receipt.v1"
	cleanBaselineReasonCode      = "second_integration_data_reset"
	cleanBaselineReceiptMaxBytes = int64(1 << 20)
)

type CleanBaselineReleaseIdentity struct {
	ProductVersion string `json:"product_version"`
	Commit         string `json:"commit"`
	BundleSHA256   string `json:"bundle_sha256"`
	ManifestSHA256 string `json:"manifest_sha256"`
	PayloadSHA256  string `json:"payload_sha256"`
	DatabaseSchema string `json:"database_schema"`
	ManualVersion  string `json:"manual_version"`
}

type CleanBaselineAccountPreview struct {
	Username                string      `json:"username"`
	SubjectSHA256           string      `json:"subject_sha256"`
	Role                    access.Role `json:"role"`
	Disabled                bool        `json:"disabled"`
	CurrentAccountCount     int         `json:"current_account_count"`
	AccountsRemoved         int         `json:"accounts_removed"`
	PredecessorConfigSHA256 string      `json:"predecessor_config_sha256"`
	PasswordVerifierSHA256  string      `json:"password_verifier_sha256"`
	SuccessorConfigSHA256   string      `json:"successor_config_sha256"`
}

type CleanBaselineWorkerPreview struct {
	Unit    string `json:"unit"`
	Present bool   `json:"present"`
	Active  bool   `json:"active"`
	Enabled bool   `json:"enabled"`
}

type CleanBaselineCredentialPreview struct {
	ActivePresent             bool `json:"unirouter_active_present"`
	StagedPresent             bool `json:"unirouter_staged_present"`
	OperationStatePresent     bool `json:"unirouter_operation_state_present"`
	LiteLLMCredentialPresent  bool `json:"litellm_credential_present"`
	LiteLLMEnvironmentPresent bool `json:"litellm_environment_present"`
	CanonicalFilesToRetire    int  `json:"canonical_files_to_retire"`
}

type CleanBaselineCredentialProof struct {
	Before                   CleanBaselineCredentialPreview `json:"before"`
	ActiveAbsent             bool                           `json:"active_absent"`
	StagedAbsent             bool                           `json:"staged_absent"`
	OperationStateAbsent     bool                           `json:"operation_state_absent"`
	LiteLLMCredentialAbsent  bool                           `json:"litellm_credential_absent"`
	LiteLLMEnvironmentAbsent bool                           `json:"litellm_environment_absent"`
	QuarantineAbsent         bool                           `json:"quarantine_absent,omitempty"`
	ReplacementGenerated     bool                           `json:"replacement_generated"`
}

// CleanBaselineBackupRestoreProof seals the exact retained backup to a
// successful isolated restore of the complete source preview. The temporary
// verification database must already be absent before this proof can be
// written into the DATA_RESET receipt.
type CleanBaselineBackupRestoreProof struct {
	BackupSHA256               string `json:"backup_sha256"`
	VerificationDatabaseSHA256 string `json:"verification_database_sha256"`
	SourceSemanticSHA256       string `json:"source_semantic_sha256"`
	RestoredSemanticSHA256     string `json:"restored_semantic_sha256"`
	PostgresMajor              int    `json:"postgres_major"`
	MigrationCount             int    `json:"migration_count"`
	VerificationDatabaseAbsent bool   `json:"verification_database_absent"`
	SealedSHA256               string `json:"sealed_sha256"`
}

type CleanBaselinePreviewReport struct {
	SchemaVersion        string                         `json:"schema_version"`
	Supported            bool                           `json:"supported"`
	Mutating             bool                           `json:"mutating"`
	Release              CleanBaselineReleaseIdentity   `json:"release"`
	RollbackBackupSHA256 string                         `json:"rollback_backup_sha256"`
	TargetDatabaseSHA256 string                         `json:"target_database_sha256"`
	Account              CleanBaselineAccountPreview    `json:"account"`
	Workers              []CleanBaselineWorkerPreview   `json:"workers"`
	Credential           CleanBaselineCredentialPreview `json:"connector_credential_config"`
	Database             CleanBaselineDatabasePreview   `json:"database"`
	Checks               []CheckResult                  `json:"checks"`
	PreviewSHA256        string                         `json:"preview_sha256"`
	GeneratedAt          time.Time                      `json:"generated_at"`
}

type CleanBaselineReceipt struct {
	SchemaVersion             string                          `json:"schema_version"`
	OperationID               string                          `json:"operation_id"`
	Status                    string                          `json:"status"`
	Release                   CleanBaselineReleaseIdentity    `json:"release"`
	PreviewSHA256             string                          `json:"preview_sha256"`
	RollbackBackupSHA256      string                          `json:"rollback_backup_sha256"`
	RollbackDatabaseSetSHA256 string                          `json:"rollback_database_set_sha256"`
	ActiveDatabaseSetSHA256   string                          `json:"active_database_set_sha256"`
	PredecessorDatabaseSHA256 string                          `json:"predecessor_database_sha256"`
	PredecessorDatabaseAbsent bool                            `json:"predecessor_database_absent"`
	BackupRestore             CleanBaselineBackupRestoreProof `json:"backup_restore"`
	Account                   CleanBaselineAccountPreview     `json:"account"`
	CheckpointFloor           CleanBaselineCheckpointFloor    `json:"checkpoint_floor"`
	Credential                CleanBaselineCredentialProof    `json:"connector_credential_config"`
	Database                  CleanBaselineDatabaseProof      `json:"database"`
	Checks                    []CheckResult                   `json:"checks"`
	DurableReceiptSHA256      string                          `json:"durable_receipt_sha256"`
	CompletedAt               time.Time                       `json:"completed_at"`
}

type CleanBaselineDurableEvidence struct {
	SchemaVersion             string                          `json:"schema_version"`
	Release                   CleanBaselineReleaseIdentity    `json:"release"`
	PreviewSHA256             string                          `json:"preview_sha256"`
	RollbackBackupSHA256      string                          `json:"rollback_backup_sha256"`
	RollbackDatabaseSetSHA256 string                          `json:"rollback_database_set_sha256"`
	ActiveDatabaseSetSHA256   string                          `json:"active_database_set_sha256"`
	PredecessorDatabaseSHA256 string                          `json:"predecessor_database_sha256"`
	BackupRestore             CleanBaselineBackupRestoreProof `json:"backup_restore"`
	Account                   CleanBaselineAccountPreview     `json:"account"`
	CheckpointFloor           CleanBaselineCheckpointFloor    `json:"checkpoint_floor"`
	Credential                CleanBaselineCredentialProof    `json:"connector_credential_config"`
	Database                  CleanBaselineDatabaseProof      `json:"database"`
}

type cleanBaselinePlan struct {
	State             InstallState
	Current           verifiedTarget
	Credentials       DatabaseCredentials
	Facts             DatabaseFacts
	Backup            BackupResult
	NewDatabase       string
	DatabasePreview   CleanBaselineDatabasePreview
	AccountEntry      access.AccountConfigEntry
	SuccessorAccounts access.AccountConfig
	AccountPreview    CleanBaselineAccountPreview
	Workers           []CleanBaselineWorkerPreview
	Credential        CleanBaselineCredentialPreview
	ServiceMainPID    string
	Preview           CleanBaselinePreviewReport
}

func (engine *Engine) PreviewCleanBaseline(
	ctx context.Context,
	backup string,
	backupSHA256 string,
	newDatabase string,
) (CleanBaselinePreviewReport, error) {
	report, _, err := engine.prepareCleanBaseline(ctx, backup, backupSHA256, newDatabase)
	return report, err
}

func (engine *Engine) prepareCleanBaseline(
	ctx context.Context,
	backup string,
	backupSHA256 string,
	newDatabase string,
) (CleanBaselinePreviewReport, cleanBaselinePlan, error) {
	emptyReport := CleanBaselinePreviewReport{
		SchemaVersion: CleanBaselinePreviewSchema,
		Supported:     false,
		Mutating:      false,
		GeneratedAt:   engine.now().UTC().Truncate(time.Second),
	}
	if err := engine.ensureNoOutstandingOperation(); err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	if _, err := detectSupportedHost(engine.Paths); err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	if !filepath.IsAbs(backup) || filepath.Clean(backup) != backup ||
		!sha256ReferencePattern.MatchString(backupSHA256) ||
		!cleanBaselineDatabasePattern.MatchString(newDatabase) {
		return emptyReport, cleanBaselinePlan{}, fmt.Errorf("clean-baseline requires an absolute rollback backup, exact SHA-256, and xinsight_clean_* target")
	}
	state, err := engine.readInstallState()
	if err != nil || state.Current.LegacyReleaseID != "" {
		return emptyReport, cleanBaselinePlan{}, fmt.Errorf("clean-baseline requires a managed SemVer release")
	}
	currentLink, err := readAbsoluteSymlink(engine.Paths.CurrentLink)
	if err != nil || currentLink != state.Current.ReleaseDir {
		return emptyReport, cleanBaselinePlan{}, fmt.Errorf("managed current release pointer drift")
	}
	current, err := engine.verifyStateV2(state.Current)
	if err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	accessRequired, err := engine.validateAccessAccountCredential(current)
	if err != nil || !accessRequired {
		return emptyReport, cleanBaselinePlan{}, fmt.Errorf("clean-baseline requires the release-defined local account boundary")
	}
	if err := releasePayloadAndRuntimePreflight(current, engine.Paths); err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	credentials, err := LoadDatabaseCredentials(engine.Paths)
	if err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil || facts.PostgresMajor != 16 ||
		validateLedgerPrefix(facts.MigrationLedger, current.Bundle.Manifest.MigrationChecksums, false) != nil {
		return emptyReport, cleanBaselinePlan{}, fmt.Errorf("clean-baseline source is not the exact PostgreSQL 16 release schema")
	}
	if engine.CleanBaseline == nil {
		return emptyReport, cleanBaselinePlan{}, fmt.Errorf("clean-baseline database manager is unavailable")
	}
	if exists, err := engine.CleanBaseline.CleanBaselineDatabaseExists(ctx, credentials, newDatabase); err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	} else if exists {
		return emptyReport, cleanBaselinePlan{}, fmt.Errorf("clean-baseline target already exists; target reuse is forbidden")
	}
	backupResult, err := engine.Database.VerifyBackup(ctx, backup, backupSHA256)
	if err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	if err := engine.checkHostTools(ctx, false, true); err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	availablePath, err := nearestExistingPath(engine.Paths.DatabaseSetsDir)
	if err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	available, err := availableBytes(availablePath)
	if err != nil || available < uint64(maxInt64(facts.DatabaseSizeBytes, 0))+uint64(backupResult.SizeBytes)+minimumFreeHeadroom {
		return emptyReport, cleanBaselinePlan{}, fmt.Errorf("insufficient disk for clean-baseline target and isolated backup restore")
	}

	feedWorker, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.CompletedExecutionWorkerUnit)
	if err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	liteLLMWorker, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.LiteLLMWorkerUnit)
	if err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	serviceState, err := engine.systemdUnitProperty(ctx, "xinsight.service", "ActiveState")
	if err != nil || serviceState != "active" {
		return emptyReport, cleanBaselinePlan{}, fmt.Errorf("clean-baseline requires the main service to be active before controlled replacement")
	}
	mainPID, err := engine.systemdUnitProperty(ctx, "xinsight.service", "MainPID")
	if err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	pidNumber, err := strconv.ParseInt(mainPID, 10, 64)
	if err != nil || pidNumber <= 0 {
		return emptyReport, cleanBaselinePlan{}, fmt.Errorf("clean-baseline main service process identity is invalid")
	}

	databasePreview, err := engine.CleanBaseline.PreviewCleanBaseline(ctx, credentials)
	if err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	databasePreview.CheckpointFloor.RollbackBackupSHA256 = backupResult.SHA256
	sealedFloor, err := sealCleanBaselineCheckpointFloor(databasePreview.CheckpointFloor)
	if err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	databasePreview.CheckpointFloor = sealedFloor
	accountEntry, successor, accountPreview, err := cleanBaselineDemoOperator(engine.Paths.AccessAccountsFile)
	if err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	credentialPreview, err := inspectCleanBaselineCredentialPreview(engine.Paths)
	if err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	workers := []CleanBaselineWorkerPreview{
		{Unit: feedWorker.Unit, Present: feedWorker.Present, Active: feedWorker.Active, Enabled: feedWorker.Enabled},
		{Unit: liteLLMWorker.Unit, Present: liteLLMWorker.Present, Active: liteLLMWorker.Active, Enabled: liteLLMWorker.Enabled},
	}
	release := cleanBaselineReleaseIdentity(current.Identity)
	report := CleanBaselinePreviewReport{
		SchemaVersion:        CleanBaselinePreviewSchema,
		Supported:            true,
		Mutating:             false,
		Release:              release,
		RollbackBackupSHA256: backupResult.SHA256,
		TargetDatabaseSHA256: hashCleanBaselineText(newDatabase),
		Account:              accountPreview,
		Workers:              workers,
		Credential:           credentialPreview,
		Database:             databasePreview,
		Checks: []CheckResult{
			{Name: "release", Status: "pass", Detail: "exact_signed_active_release"},
			{Name: "rollback_backup", Status: "pass", Detail: "sha256_and_pg_restore_16_list_verified_apply_requires_isolated_restore"},
			{Name: "schema_contract", Status: "pass", Detail: databasePreview.SchemaContractSHA256},
			{Name: "table_classification", Status: "pass", Detail: fmt.Sprintf("all_%d_public_tables_classified", len(cleanBaselineTables))},
			{Name: "demo_operator", Status: "pass", Detail: "identity_role_and_password_verifier_preserved"},
			{Name: "workers", Status: "pass", Detail: "both_inactive_disabled"},
			{Name: "checkpoint_floor", Status: "pass", Detail: databasePreview.CheckpointFloor.SealedSHA256},
			{Name: "strict_empty_checkpoint", Status: "pass", Detail: "data_reset_creates_no_state_operation_or_history"},
			{Name: "connector_credential_config", Status: "pass", Detail: "existing_unirouter_and_litellm_campaign_state_will_be_retired_without_replacement"},
		},
		GeneratedAt: emptyReport.GeneratedAt,
	}
	previewSHA, err := cleanBaselinePreviewDigest(report)
	if err != nil {
		return emptyReport, cleanBaselinePlan{}, err
	}
	report.PreviewSHA256 = previewSHA
	return report, cleanBaselinePlan{
		State: state, Current: current, Credentials: credentials, Facts: facts,
		Backup: backupResult, NewDatabase: newDatabase,
		DatabasePreview: databasePreview, AccountEntry: accountEntry,
		SuccessorAccounts: successor, AccountPreview: accountPreview,
		Workers: workers, Credential: credentialPreview, ServiceMainPID: mainPID, Preview: report,
	}, nil
}

func releasePayloadAndRuntimePreflight(current verifiedTarget, paths Paths) error {
	if err := releasebundleVerifyExtracted(current); err != nil {
		return err
	}
	if err := validateServiceDropIns(paths); err != nil {
		return err
	}
	if err := validateManagedServiceDropInRuntime(current.Identity.ReleaseDir, paths); err != nil {
		return err
	}
	return validateManagedEnvironmentFile(paths.EnvironmentFile)
}

func releasebundleVerifyExtracted(current verifiedTarget) error {
	return releasebundle.VerifyExtractedPayload(current.Bundle, current.Identity.ReleaseDir)
}

func cleanBaselineDemoOperator(path string) (access.AccountConfigEntry, access.AccountConfig, CleanBaselineAccountPreview, error) {
	config, err := access.ReadAndValidateAccountFile(path)
	if err != nil {
		return access.AccountConfigEntry{}, access.AccountConfig{}, CleanBaselineAccountPreview{}, err
	}
	var selected *access.AccountConfigEntry
	for index := range config.Accounts {
		if config.Accounts[index].Username == "demo.operator" {
			entry := config.Accounts[index]
			selected = &entry
			break
		}
	}
	if selected == nil || selected.Disabled {
		return access.AccountConfigEntry{}, access.AccountConfig{}, CleanBaselineAccountPreview{}, fmt.Errorf("enabled demo.operator account is required")
	}
	successor := access.AccountConfig{
		SchemaVersion: access.AccountConfigSchema,
		Accounts:      []access.AccountConfigEntry{*selected},
	}
	contents, err := marshalJSON(successor)
	if err != nil {
		return access.AccountConfigEntry{}, access.AccountConfig{}, CleanBaselineAccountPreview{}, err
	}
	predecessorContents, err := marshalJSON(config)
	if err != nil {
		return access.AccountConfigEntry{}, access.AccountConfig{}, CleanBaselineAccountPreview{}, err
	}
	if _, err := access.ParseAccountConfig(contents); err != nil {
		return access.AccountConfigEntry{}, access.AccountConfig{}, CleanBaselineAccountPreview{}, err
	}
	return *selected, successor, CleanBaselineAccountPreview{
		Username:                "demo.operator",
		SubjectSHA256:           hashCleanBaselineText(selected.SubjectID),
		Role:                    selected.Role,
		Disabled:                selected.Disabled,
		CurrentAccountCount:     len(config.Accounts),
		AccountsRemoved:         len(config.Accounts) - 1,
		PredecessorConfigSHA256: hashCleanBaselineBytes(predecessorContents),
		PasswordVerifierSHA256:  hashCleanBaselineText(selected.PasswordHash),
		SuccessorConfigSHA256:   hashCleanBaselineBytes(contents),
	}, nil
}

func cleanBaselineReleaseIdentity(identity ReleaseIdentity) CleanBaselineReleaseIdentity {
	return CleanBaselineReleaseIdentity{
		ProductVersion: identity.ProductVersion,
		Commit:         identity.Commit,
		BundleSHA256:   identity.BundleSHA256,
		ManifestSHA256: identity.ManifestSHA256,
		PayloadSHA256:  identity.PayloadSHA256,
		DatabaseSchema: identity.DatabaseSchema,
		ManualVersion:  identity.ManualVersion,
	}
}

func cleanBaselinePreviewDigest(report CleanBaselinePreviewReport) (string, error) {
	report.PreviewSHA256 = ""
	report.GeneratedAt = time.Time{}
	payload, err := json.Marshal(report)
	if err != nil {
		return "", err
	}
	return hashCleanBaselineBytes(payload), nil
}

func cleanBaselineDatabasePreviewDigest(preview CleanBaselineDatabasePreview) (string, error) {
	payload, err := json.Marshal(preview)
	if err != nil {
		return "", err
	}
	return hashCleanBaselineBytes(payload), nil
}

func sealCleanBaselineBackupRestoreProof(
	proof CleanBaselineBackupRestoreProof,
) (CleanBaselineBackupRestoreProof, error) {
	if !sha256ReferencePattern.MatchString(proof.BackupSHA256) ||
		!sha256ReferencePattern.MatchString(proof.VerificationDatabaseSHA256) ||
		!sha256ReferencePattern.MatchString(proof.SourceSemanticSHA256) ||
		proof.SourceSemanticSHA256 != proof.RestoredSemanticSHA256 ||
		proof.PostgresMajor != 16 || proof.MigrationCount <= 0 ||
		!proof.VerificationDatabaseAbsent {
		return CleanBaselineBackupRestoreProof{}, fmt.Errorf("isolated backup restore proof is invalid")
	}
	proof.SealedSHA256 = ""
	payload, err := json.Marshal(proof)
	if err != nil {
		return CleanBaselineBackupRestoreProof{}, err
	}
	proof.SealedSHA256 = hashCleanBaselineBytes(payload)
	return proof, nil
}

func verifyCleanBaselineBackupRestoreProof(
	proof CleanBaselineBackupRestoreProof,
	expectedBackupSHA256 string,
) error {
	if proof.BackupSHA256 != expectedBackupSHA256 ||
		!sha256ReferencePattern.MatchString(proof.SealedSHA256) {
		return fmt.Errorf("isolated backup restore proof is not bound to the retained backup")
	}
	expectedSeal := proof.SealedSHA256
	sealed, err := sealCleanBaselineBackupRestoreProof(proof)
	if err != nil {
		return err
	}
	if sealed.SealedSHA256 != expectedSeal {
		return fmt.Errorf("isolated backup restore proof seal does not match its contents")
	}
	return nil
}

func hashCleanBaselineBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (engine *Engine) cleanBaselineDatabaseManager() (CleanBaselineDatabaseManager, error) {
	if engine.CleanBaseline == nil {
		return nil, errors.New("clean-baseline database manager is unavailable")
	}
	return engine.CleanBaseline, nil
}

func cleanBaselineAccountStagingPath(paths Paths, operationID string) string {
	return filepath.Join(paths.OperationsDir, operationID+"-demo-operator.json")
}

func cleanBaselineDatabaseSetDigest(setRef string) string {
	return hashCleanBaselineText(setRef)
}

func cleanBaselineRestoreDatabaseName(operationID string) string {
	digest := sha256.Sum256([]byte(operationID))
	return "xinsight_restore_data_reset_" + hex.EncodeToString(digest[:8])
}

func cleanBaselineNow(engine *Engine) time.Time {
	return engine.now().UTC().Truncate(time.Microsecond)
}

func ensureCleanBaselineAccountReadback(path string, expected access.AccountConfigEntry) error {
	config, err := access.ReadAndValidateAccountFile(path)
	if err != nil {
		return err
	}
	if len(config.Accounts) != 1 || config.Accounts[0] != expected {
		return fmt.Errorf("demo.operator account readback differs from protected identity/verifier")
	}
	return nil
}

func cleanBaselineAccountBackupPath(paths Paths, operationID string) string {
	return paths.AccessAccountsFile + ".before-" + operationID
}

func cleanBaselinePathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func sealCleanBaselineCheckpointFloor(
	floor CleanBaselineCheckpointFloor,
) (CleanBaselineCheckpointFloor, error) {
	if floor.Binding != cleanBaselineBinding() || floor.HistoricalMaxEpoch < 0 ||
		!sha256ReferencePattern.MatchString(floor.PriorIdentitySHA256) ||
		!sha256ReferencePattern.MatchString(floor.RollbackBackupSHA256) ||
		!sortedUniqueSHA256(floor.PriorSourceInstanceSHA256) ||
		!sortedUniqueSHA256(floor.PriorCursorSHA256) ||
		!sortedUniqueSHA256(floor.PriorHighWatermarkSHA256) {
		return CleanBaselineCheckpointFloor{}, fmt.Errorf("checkpoint floor is invalid")
	}
	floor.SealedSHA256 = ""
	payload, err := json.Marshal(floor)
	if err != nil {
		return CleanBaselineCheckpointFloor{}, err
	}
	floor.SealedSHA256 = hashCleanBaselineBytes(payload)
	return floor, nil
}

func verifyCleanBaselineCheckpointFloor(floor CleanBaselineCheckpointFloor) error {
	expected := floor.SealedSHA256
	if !sha256ReferencePattern.MatchString(expected) {
		return fmt.Errorf("checkpoint floor seal is invalid")
	}
	sealed, err := sealCleanBaselineCheckpointFloor(floor)
	if err != nil {
		return err
	}
	if sealed.SealedSHA256 != expected {
		return fmt.Errorf("checkpoint floor seal does not match its contents")
	}
	return nil
}

func sortedUniqueSHA256(values []string) bool {
	if !sort.StringsAreSorted(values) {
		return false
	}
	for index, value := range values {
		if !sha256ReferencePattern.MatchString(value) || (index > 0 && values[index-1] == value) {
			return false
		}
	}
	return true
}

func validateCleanBaselineDurableEvidence(
	evidence *CleanBaselineDurableEvidence,
	backupSHA256 string,
) error {
	if evidence == nil || evidence.SchemaVersion != CleanBaselineReceiptSchema ||
		!sha256ReferencePattern.MatchString(evidence.PreviewSHA256) ||
		evidence.RollbackBackupSHA256 != backupSHA256 ||
		!sha256ReferencePattern.MatchString(evidence.RollbackDatabaseSetSHA256) ||
		!sha256ReferencePattern.MatchString(evidence.ActiveDatabaseSetSHA256) ||
		!sha256ReferencePattern.MatchString(evidence.PredecessorDatabaseSHA256) ||
		evidence.Database.PostgresMajor != 16 ||
		evidence.Database.MigrationCount != expectedCleanBaselineSchemaContract.MigrationCount ||
		evidence.Database.SchemaContractVersion != CleanBaselineSchemaContractVersion ||
		evidence.Database.SchemaContractSHA256 != expectedCleanBaselineSchemaContract.SemanticSHA256 ||
		evidence.Database.MigrationSetSHA256 != expectedCleanBaselineSchemaContract.MigrationSetSHA256 ||
		!sha256ReferencePattern.MatchString(evidence.Database.SchemaLayoutSHA256) ||
		evidence.Database.SourceCatalogRows != 1 || evidence.Database.AdapterCatalogRows != 2 ||
		evidence.Database.HistoricalRows != 0 || evidence.Database.CheckpointStateRows != 0 ||
		evidence.Database.EpochOperationRows != 0 || evidence.Database.EpochHistoryRows != 0 ||
		!validCleanBaselineCredentialPreview(evidence.Credential.Before) ||
		!evidence.Credential.ActiveAbsent || !evidence.Credential.StagedAbsent ||
		!evidence.Credential.OperationStateAbsent ||
		!evidence.Credential.LiteLLMCredentialAbsent ||
		!evidence.Credential.LiteLLMEnvironmentAbsent ||
		evidence.Credential.ReplacementGenerated {
		return fmt.Errorf("DATA_RESET durable evidence is not a strict-empty baseline")
	}
	if err := verifyCleanBaselineBackupRestoreProof(evidence.BackupRestore, backupSHA256); err != nil {
		return err
	}
	if err := verifyCleanBaselineCheckpointFloor(evidence.CheckpointFloor); err != nil {
		return err
	}
	if evidence.CheckpointFloor.RollbackBackupSHA256 != backupSHA256 {
		return fmt.Errorf("DATA_RESET checkpoint floor is not bound to the rollback backup")
	}
	return nil
}
