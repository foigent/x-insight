package hostctl

import "time"

const (
	InstallStateSchema = "xinsight.install-state.v1"
	JournalSchema      = "xinsight.operation-journal.v1"
	ReceiptSchema      = "xinsight.host-operation-receipt.v1"
	PreflightSchema    = "xinsight.host-preflight.v1"
	StatusSchema       = "xinsight.host-status.v1"
	OperationRefSchema = "xinsight.active-operation-ref.v1"
)

type OperationRef struct {
	SchemaVersion string `json:"schema_version"`
	OperationID   string `json:"operation_id"`
	JournalPath   string `json:"journal_path"`
}

type ReleaseIdentity struct {
	SourceKind      string `json:"source_kind,omitempty"`
	ProductVersion  string `json:"product_version"`
	LegacyReleaseID string `json:"legacy_release_id,omitempty"`
	Commit          string `json:"commit"`
	BundleSHA256    string `json:"bundle_sha256"`
	ManifestSHA256  string `json:"manifest_sha256"`
	PayloadSHA256   string `json:"payload_sha256"`
	KeyID           string `json:"key_id"`
	ArtifactPath    string `json:"artifact_path"`
	ReleaseDir      string `json:"release_dir"`
	DatabaseSchema  string `json:"database_schema"`
	ConfigSchema    string `json:"config_schema"`
	ManualVersion   string `json:"manual_version"`
	RollbackAllowed bool   `json:"rollback_allowed"`
}

type InstallState struct {
	SchemaVersion         string           `json:"schema_version"`
	Managed               bool             `json:"managed"`
	Current               ReleaseIdentity  `json:"current"`
	Previous              *ReleaseIdentity `json:"previous,omitempty"`
	DatabaseSchemaCurrent string           `json:"database_schema_current"`
	DatabaseSetLink       string           `json:"database_set_link"`
	ServiceUnit           string           `json:"service_unit"`
	LastOperationID       string           `json:"last_operation_id"`
	ManagedAt             time.Time        `json:"managed_at"`
	UpdatedAt             time.Time        `json:"updated_at"`
}

type Journal struct {
	BackupRuntimeStateCaptured      bool                             `json:"backup_runtime_state_captured,omitempty"`
	BackupServiceWasActive          bool                             `json:"backup_service_was_active,omitempty"`
	BackupControllerWasActive       bool                             `json:"backup_controller_was_active,omitempty"`
	BackupImporterWasLogin          *bool                            `json:"backup_importer_was_login,omitempty"`
	PrivateDumpTempPath             string                           `json:"private_dump_temp_path,omitempty"`
	PrivateFingerprint              *EvidenceFingerprint             `json:"private_fingerprint,omitempty"`
	PrivateDumpSHA256               string                           `json:"private_dump_sha256,omitempty"`
	BackupIsPrivate                 bool                             `json:"backup_is_private,omitempty"`
	CustodyCandidate                *ReviewBodyConfig                `json:"custody_candidate,omitempty"`
	RecoveryBundlePath              string                           `json:"recovery_bundle_path,omitempty"`
	RecoveryBundleSHA256            string                           `json:"recovery_bundle_sha256,omitempty"`
	BeforeEvidenceFingerprintSHA256 string                           `json:"before_evidence_fingerprint_sha256,omitempty"`
	SourceProofPath                 string                           `json:"source_proof_path,omitempty"`
	SourceProofSHA256               string                           `json:"source_proof_sha256,omitempty"`
	SourceFactsWorkerStateCaptured  bool                             `json:"source_facts_worker_state_captured,omitempty"`
	SourceFactsWorkerWasPresent     bool                             `json:"source_facts_worker_was_present,omitempty"`
	SourceFactsWorkerWasActive      bool                             `json:"source_facts_worker_was_active,omitempty"`
	SourceFactsWorkerWasEnabled     bool                             `json:"source_facts_worker_was_enabled,omitempty"`
	SchemaVersion                   string                           `json:"schema_version"`
	OperationID                     string                           `json:"operation_id"`
	Kind                            string                           `json:"kind"`
	Status                          string                           `json:"status"`
	Phase                           string                           `json:"phase"`
	ArtifactPath                    string                           `json:"artifact_path,omitempty"`
	LegacyArtifactPath              string                           `json:"legacy_artifact_path,omitempty"`
	BackupPath                      string                           `json:"backup_path,omitempty"`
	BackupSHA256                    string                           `json:"backup_sha256,omitempty"`
	RestoreDatabase                 string                           `json:"restore_database,omitempty"`
	CleanDatabase                   string                           `json:"clean_baseline_database,omitempty"`
	CleanPredecessorDatabase        string                           `json:"clean_baseline_predecessor_database,omitempty"`
	CleanRestoreDatabase            string                           `json:"clean_baseline_restore_verification_database,omitempty"`
	CleanRestoreDatabaseOwned       bool                             `json:"clean_baseline_restore_verification_database_owned,omitempty"`
	CleanBackupRestoreProof         *CleanBaselineBackupRestoreProof `json:"clean_baseline_backup_restore_proof,omitempty"`
	CleanPreviewSHA256              string                           `json:"clean_baseline_preview_sha256,omitempty"`
	CleanPriorAccountSHA256         string                           `json:"clean_baseline_account_before_sha256,omitempty"`
	CleanAccountSHA256              string                           `json:"clean_baseline_account_sha256,omitempty"`
	CleanCredentialPreview          *CleanBaselineCredentialPreview  `json:"clean_baseline_connector_credential_config,omitempty"`
	CleanCheckpointFloorSHA256      string                           `json:"clean_baseline_checkpoint_floor_sha256,omitempty"`
	DataResetOperationID            string                           `json:"data_reset_operation_id,omitempty"`
	DataResetReceiptSHA256          string                           `json:"data_reset_receipt_sha256,omitempty"`
	CheckpointPreviewSHA256         string                           `json:"checkpoint_bootstrap_preview_sha256,omitempty"`
	CheckpointRequestSHA256         string                           `json:"checkpoint_bootstrap_request_sha256,omitempty"`
	CheckpointEpoch                 int64                            `json:"checkpoint_bootstrap_epoch,omitempty"`
	AdminCredentialRotation         *AdminCredentialRotationJournal  `json:"admin_credential_rotation,omitempty"`
	ServicePIDBefore                string                           `json:"service_main_pid_before,omitempty"`
	ReasonCode                      string                           `json:"reason_code,omitempty"`
	DatabaseSetBefore               string                           `json:"database_set_before,omitempty"`
	DatabaseSetAfter                string                           `json:"database_set_after,omitempty"`
	FeedWorkerStateCaptured         bool                             `json:"feed_worker_state_captured,omitempty"`
	FeedWorkerWasPresent            bool                             `json:"feed_worker_was_present,omitempty"`
	FeedWorkerWasActive             bool                             `json:"feed_worker_was_active,omitempty"`
	FeedWorkerWasEnabled            bool                             `json:"feed_worker_was_enabled,omitempty"`
	LiteLLMWorkerStateCaptured      bool                             `json:"litellm_worker_state_captured,omitempty"`
	LiteLLMWorkerWasPresent         bool                             `json:"litellm_worker_was_present,omitempty"`
	LiteLLMWorkerWasActive          bool                             `json:"litellm_worker_was_active,omitempty"`
	LiteLLMWorkerWasEnabled         bool                             `json:"litellm_worker_was_enabled,omitempty"`
	Before                          *ReleaseIdentity                 `json:"before,omitempty"`
	Target                          *ReleaseIdentity                 `json:"target,omitempty"`
	CompletedPhases                 []string                         `json:"completed_phases"`
	ErrorCode                       string                           `json:"error_code,omitempty"`
	AllowedActions                  []string                         `json:"allowed_actions"`
	StartedAt                       time.Time                        `json:"started_at"`
	UpdatedAt                       time.Time                        `json:"updated_at"`
	ReceiptAt                       *time.Time                       `json:"receipt_at,omitempty"`
	CompletedAt                     *time.Time                       `json:"completed_at,omitempty"`
}

type OperationReceipt struct {
	SchemaVersion           string                                  `json:"schema_version"`
	OperationID             string                                  `json:"operation_id"`
	Kind                    string                                  `json:"kind"`
	Status                  string                                  `json:"status"`
	Before                  *ReleaseIdentity                        `json:"before,omitempty"`
	After                   *ReleaseIdentity                        `json:"after,omitempty"`
	BackupPath              string                                  `json:"backup_path,omitempty"`
	BackupSHA256            string                                  `json:"backup_sha256,omitempty"`
	MigrationIDs            []string                                `json:"migration_ids,omitempty"`
	DatabaseSetRef          string                                  `json:"database_set_ref"`
	ReasonCode              string                                  `json:"reason_code,omitempty"`
	DataReset               *CleanBaselineDurableEvidence           `json:"data_reset,omitempty"`
	CheckpointBootstrap     *CheckpointBootstrapDurableEvidence     `json:"checkpoint_bootstrap,omitempty"`
	AdminCredentialRotation *AdminCredentialRotationDurableEvidence `json:"admin_credential_rotation,omitempty"`
	Checks                  []CheckResult                           `json:"checks"`
	CompletedAt             time.Time                               `json:"completed_at"`
}

type CheckResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type PreflightReport struct {
	SchemaVersion      string                    `json:"schema_version"`
	Operation          string                    `json:"operation"`
	Mutating           bool                      `json:"mutating"`
	Supported          bool                      `json:"supported"`
	Current            *ReleaseIdentity          `json:"current,omitempty"`
	Target             *ReleaseIdentity          `json:"target,omitempty"`
	Checks             []CheckResult             `json:"checks"`
	ControlCredentials []ControlCredentialStatus `json:"control_credentials,omitempty"`
	RequiredActions    []string                  `json:"required_actions,omitempty"`
	GeneratedAt        time.Time                 `json:"generated_at"`
}

type StatusReport struct {
	SchemaVersion  string        `json:"schema_version"`
	Managed        bool          `json:"managed"`
	InstallState   *InstallState `json:"install_state,omitempty"`
	ActiveJournal  *Journal      `json:"active_journal,omitempty"`
	CurrentLink    string        `json:"current_link,omitempty"`
	DatabaseSetRef string        `json:"database_set_ref,omitempty"`
	GeneratedAt    time.Time     `json:"generated_at"`
}
