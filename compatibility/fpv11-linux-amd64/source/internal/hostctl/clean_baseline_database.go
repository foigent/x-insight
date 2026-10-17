package hostctl

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/db"
	"github.com/jackc/pgx/v5"
)

const (
	cleanBaselineSourceCatalogSHA256  = "sha256:9c5c5142799bbff32758bc23064c62b0814587c6ba3593d32a7c5bd840fefd26"
	cleanBaselineAdapterCatalogSHA256 = "sha256:bef27697dbbe22b4f3d92bb58d9cc1c4dd16f1e1f4170dc052bd497238a7f879"

	cleanBaselineSourceSystemID    = "11111111-1111-1111-1111-111111111111"
	cleanBaselineAdapterManifestID = "22222222-2222-4222-8222-222222222224"
)

var cleanBaselineTables = []string{
	"access_audit_records",
	"adapter_import_states",
	"adapter_manifests",
	"authoritative_sessions",
	"budget_reading_members",
	"budget_readings",
	"capture_gap_resolutions",
	"capture_gaps",
	"case_annotations",
	"case_content_links",
	"case_evidence_links",
	"case_executions",
	"case_finding_actions",
	"case_finding_assessment_actions",
	"case_finding_assessments",
	"case_finding_followups",
	"case_findings",
	"case_gap_links",
	"case_resource_links",
	"connector_checkpoint_epoch_history",
	"connector_checkpoint_epoch_operations",
	"connector_control_operations",
	"connector_import_operation_bindings",
	"connector_installation_invitations",
	"connector_installations",
	"connector_operational_receipts",
	"discovery_analysis_runs",
	"discovery_hypotheses",
	"evaluation_feedback",
	"evidence_events",
	"evidence_refs",
	"execution_version_dimensions",
	"finding_review_creations",
	"import_provenance",
	"import_provenance_events",
	"ingestion_quarantine",
	"investigation_alert_case_links",
	"investigation_alerts",
	"investigation_case_scope_labels",
	"investigation_cases",
	"investigation_queue_filter_operations",
	"investigation_queue_filters",
	"payload_policies",
	"resource_access_records",
	"review_body_copy_attempts",
	"review_body_ingest_bindings",
	"review_body_objects",
	"review_body_operations",
	"review_body_project_usage",
	"review_capture_defaults",
	"review_capture_exceptions",
	"review_capture_project_bindings",
	"review_capture_projects",
	"review_capture_ur_operations",
	"runtime_reading_content_members",
	"runtime_reading_content_refs",
	"runtime_reading_economic_members",
	"runtime_reading_members",
	"runtime_reading_requests",
	"runtime_reading_resource_members",
	"runtime_reading_source_members",
	"runtime_readings",
	"saved_investigation_views",
	"schema_migrations",
	"service_identities",
	"service_identity_tokens",
	"session_execution_links",
	"source_budget_catalog_entries",
	"source_budget_catalogs",
	"source_budget_collections",
	"source_budget_offline_authorizations",
	"source_budget_offline_imports",
	"source_budget_readings",
	"source_budget_receipts",
	"source_content_collections",
	"source_content_manifests",
	"source_content_receipts",
	"source_fact_documents",
	"source_fact_management_events",
	"source_fact_offline_authorizations",
	"source_fact_streams",
	"source_observation_documents",
	"source_runtime_snapshot_pages",
	"source_runtime_snapshots",
	"source_systems",
	"work_event_collections",
	"work_event_documents",
	"work_event_ingest_bindings",
	"work_event_install_invitations",
	"work_event_relations",
	"work_event_ur_targets",
	"workbench_access_grants",
	"workbench_economic_facts",
	"workbench_purpose_manifests",
	"workbench_purpose_members",
	"workbench_purpose_revisions",
	"workbench_reading_members",
	"workbench_readings",
	"workbench_saved_filter_operations",
	"workbench_saved_filters",
	"workbench_source_manifests",
	"workbench_source_members",
}

type CleanBaselineTableState struct {
	Table          string `json:"table"`
	Disposition    string `json:"disposition"`
	CurrentRows    int64  `json:"current_rows"`
	ProtectedRows  int64  `json:"protected_rows"`
	RowsToZero     int64  `json:"rows_to_zero"`
	SemanticSHA256 string `json:"semantic_sha256"`
}

type CleanBaselineSchemaObjects struct {
	Namespaces  int `json:"namespaces"`
	Tables      int `json:"tables"`
	Columns     int `json:"columns"`
	Constraints int `json:"constraints"`
	Indexes     int `json:"indexes"`
	Triggers    int `json:"triggers"`
	Functions   int `json:"functions"`
	Extensions  int `json:"extensions"`
	Types       int `json:"types"`
	Relations   int `json:"relations"`
}

type CleanBaselineCatalogState struct {
	SourceRows        int64  `json:"release_source_rows"`
	SourceSHA256      string `json:"release_source_sha256"`
	AdapterRows       int64  `json:"release_adapter_rows"`
	AdapterSHA256     string `json:"release_adapter_sha256"`
	RuntimeRowsZeroed int64  `json:"runtime_rows_zeroed"`
}

// CleanBaselineCheckpointFloor is a sealed, raw-free summary of every
// checkpoint identity that existed in the rollback source. DATA_RESET carries
// it into its durable receipt; a later BOOTSTRAP derives max+1 and rejects old
// source/cursor identities without copying checkpoint history into the live DB.
type CleanBaselineCheckpointFloor struct {
	Binding                   checkpointepoch.Binding `json:"binding"`
	PriorIdentityRows         int64                   `json:"prior_identity_rows"`
	PriorIdentitySHA256       string                  `json:"prior_identity_sha256"`
	HistoricalMaxEpoch        int64                   `json:"historical_max_epoch"`
	PriorSourceInstanceSHA256 []string                `json:"prior_source_instance_sha256"`
	PriorCursorSHA256         []string                `json:"prior_cursor_sha256"`
	PriorHighWatermarkSHA256  []string                `json:"prior_high_watermark_sha256"`
	RollbackBackupSHA256      string                  `json:"rollback_backup_sha256"`
	SealedSHA256              string                  `json:"sealed_sha256"`
}

type CleanBaselineDatabasePreview struct {
	SchemaContractVersion string                       `json:"schema_contract_version"`
	SchemaContractSHA256  string                       `json:"schema_contract_sha256"`
	SchemaLayoutSHA256    string                       `json:"schema_layout_sha256"`
	SchemaLayoutMatches   bool                         `json:"schema_layout_matches"`
	MigrationSetSHA256    string                       `json:"migration_set_sha256"`
	SchemaObjects         CleanBaselineSchemaObjects   `json:"schema_objects"`
	Tables                []CleanBaselineTableState    `json:"tables"`
	Catalog               CleanBaselineCatalogState    `json:"catalog"`
	CheckpointFloor       CleanBaselineCheckpointFloor `json:"checkpoint_floor"`
}

type CleanBaselineDatabaseProof struct {
	PostgresMajor         int    `json:"postgres_major"`
	MigrationCount        int    `json:"migration_count"`
	SchemaContractSHA256  string `json:"schema_contract_sha256"`
	SchemaContractVersion string `json:"schema_contract_version"`
	SchemaLayoutSHA256    string `json:"schema_layout_sha256"`
	SchemaLayoutMatches   bool   `json:"schema_layout_matches"`
	MigrationSetSHA256    string `json:"migration_set_sha256"`
	SourceCatalogRows     int64  `json:"source_catalog_rows"`
	AdapterCatalogRows    int64  `json:"adapter_catalog_rows"`
	HistoricalRows        int64  `json:"historical_rows"`
	CheckpointStateRows   int64  `json:"checkpoint_state_rows"`
	EpochOperationRows    int64  `json:"epoch_operation_rows"`
	EpochHistoryRows      int64  `json:"epoch_history_rows"`
}

type CleanBaselineDatabaseManager interface {
	PreviewCleanBaseline(context.Context, DatabaseCredentials) (CleanBaselineDatabasePreview, error)
	CleanBaselineDatabaseExists(context.Context, DatabaseCredentials, string) (bool, error)
	VerifyCleanBaselineBackupRestore(context.Context, DatabaseCredentials, string, string, string, string, []db.MigrationChecksum, CleanBaselineDatabasePreview) (CleanBaselineBackupRestoreProof, error)
	RetireCleanBaselineVerificationDatabase(context.Context, DatabaseCredentials, string) error
	RetireCleanBaselinePredecessorDatabase(context.Context, DatabaseCredentials, DatabaseCredentials) error
	CreateCleanBaselineDatabase(context.Context, DatabaseCredentials, DatabaseCredentials, string, []db.MigrationChecksum) (CleanBaselineDatabaseProof, error)
	VerifyCleanBaselineDatabase(context.Context, DatabaseCredentials, []db.MigrationChecksum) (CleanBaselineDatabaseProof, error)
	BootstrapEmptyCheckpoint(context.Context, DatabaseCredentials, checkpointepoch.PrepareInput, int64, time.Time) (checkpointepoch.ActiveStateSummary, error)
	VerifyEmptyCheckpoint(context.Context, DatabaseCredentials, checkpointepoch.PrepareInput, int64) (checkpointepoch.ActiveStateSummary, error)
}

func (manager PostgresManager) PreviewCleanBaseline(
	ctx context.Context,
	credentials DatabaseCredentials,
) (CleanBaselineDatabasePreview, error) {
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return CleanBaselineDatabasePreview{}, errors.New("clean-baseline database cannot be opened")
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return CleanBaselineDatabasePreview{}, errors.New("clean-baseline database cannot be reached")
	}

	contract, err := InspectCleanBaselineSchemaContract(ctx, database)
	if err != nil {
		return CleanBaselineDatabasePreview{}, err
	}
	if err := schemaContractDriftError(contract); err != nil {
		return CleanBaselineDatabasePreview{}, err
	}
	if contract.Objects.Tables != len(cleanBaselineTables) {
		return CleanBaselineDatabasePreview{}, fmt.Errorf("release schema table classification drift")
	}
	sourceRows, sourceDigest, adapterRows, adapterDigest, runtimeCatalogRows, err := inspectReleaseCatalog(ctx, database)
	if err != nil {
		return CleanBaselineDatabasePreview{}, err
	}
	if sourceRows != 1 || sourceDigest != cleanBaselineSourceCatalogSHA256 ||
		adapterRows != 2 || adapterDigest != cleanBaselineAdapterCatalogSHA256 {
		return CleanBaselineDatabasePreview{}, fmt.Errorf("release-defined Connector catalog drift")
	}

	tables := make([]CleanBaselineTableState, 0, len(cleanBaselineTables))
	for _, table := range cleanBaselineTables {
		rowCount, digest, err := digestTableRows(ctx, database, table)
		if err != nil {
			return CleanBaselineDatabasePreview{}, err
		}
		protected := int64(0)
		disposition := "zero"
		switch table {
		case "schema_migrations":
			protected = rowCount
			disposition = "release_rebuilt"
		case "source_systems":
			protected = sourceRows
			disposition = "release_catalog_rebuilt"
		case "adapter_manifests":
			protected = adapterRows
			disposition = "release_catalog_rebuilt"
		}
		if protected > rowCount {
			return CleanBaselineDatabasePreview{}, fmt.Errorf("table classification underflow: %s", table)
		}
		tables = append(tables, CleanBaselineTableState{
			Table: table, Disposition: disposition, CurrentRows: rowCount,
			ProtectedRows: protected, RowsToZero: rowCount - protected, SemanticSHA256: digest,
		})
	}
	floor, err := inspectCheckpointHistory(ctx, database, cleanBaselineBinding())
	if err != nil {
		return CleanBaselineDatabasePreview{}, err
	}
	if floor.HistoricalMaxEpoch == math.MaxInt64 {
		return CleanBaselineDatabasePreview{}, fmt.Errorf("checkpoint epoch cannot advance beyond int64")
	}
	return CleanBaselineDatabasePreview{
		SchemaContractVersion: contract.SchemaVersion,
		SchemaContractSHA256:  contract.ActualSHA256,
		SchemaLayoutSHA256:    contract.ActualLayoutSHA256,
		SchemaLayoutMatches:   contract.LayoutMatches,
		MigrationSetSHA256:    contract.Actual.MigrationSetSHA256,
		SchemaObjects:         contract.Objects,
		Tables:                tables,
		Catalog: CleanBaselineCatalogState{
			SourceRows: sourceRows, SourceSHA256: sourceDigest,
			AdapterRows: adapterRows, AdapterSHA256: adapterDigest,
			RuntimeRowsZeroed: runtimeCatalogRows,
		},
		CheckpointFloor: floor,
	}, nil
}

func (manager PostgresManager) CleanBaselineDatabaseExists(
	ctx context.Context,
	credentials DatabaseCredentials,
	name string,
) (bool, error) {
	if !cleanBaselineDatabasePattern.MatchString(name) {
		return false, fmt.Errorf("clean-baseline database name is invalid")
	}
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return false, errors.New("admin database connection cannot be opened")
	}
	defer database.Close()
	var exists bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&exists); err != nil {
		return false, errors.New("clean-baseline target database inspection failed")
	}
	return exists, nil
}

func (manager PostgresManager) VerifyCleanBaselineBackupRestore(
	ctx context.Context,
	source DatabaseCredentials,
	verificationDatabase string,
	backupPath string,
	backupSHA256 string,
	migrationsDir string,
	expected []db.MigrationChecksum,
	expectedSource CleanBaselineDatabasePreview,
) (CleanBaselineBackupRestoreProof, error) {
	if !restoreDatabasePattern.MatchString(verificationDatabase) ||
		!sha256ReferencePattern.MatchString(backupSHA256) || len(expected) == 0 {
		return CleanBaselineBackupRestoreProof{}, fmt.Errorf("isolated backup restore request is invalid")
	}
	if exists, err := manager.DatabaseExists(ctx, source, verificationDatabase); err != nil {
		return CleanBaselineBackupRestoreProof{}, err
	} else if exists {
		return CleanBaselineBackupRestoreProof{}, fmt.Errorf("isolated backup restore database already exists")
	}
	if expectedSource.CheckpointFloor.RollbackBackupSHA256 != backupSHA256 ||
		verifyCleanBaselineCheckpointFloor(expectedSource.CheckpointFloor) != nil {
		return CleanBaselineBackupRestoreProof{}, fmt.Errorf("isolated backup restore source preview is invalid")
	}
	if _, err := manager.VerifyBackup(ctx, backupPath, backupSHA256); err != nil {
		return CleanBaselineBackupRestoreProof{}, err
	}

	liveSource, err := manager.PreviewCleanBaseline(ctx, source)
	if err != nil {
		return CleanBaselineBackupRestoreProof{}, err
	}
	liveSource, err = bindCleanBaselinePreviewToBackup(liveSource, backupSHA256)
	if err != nil {
		return CleanBaselineBackupRestoreProof{}, err
	}
	expectedDigest, err := cleanBaselineDatabasePreviewDigest(expectedSource)
	if err != nil {
		return CleanBaselineBackupRestoreProof{}, err
	}
	sourceDigest, err := cleanBaselineDatabasePreviewDigest(liveSource)
	if err != nil {
		return CleanBaselineBackupRestoreProof{}, err
	}
	if sourceDigest != expectedDigest {
		return CleanBaselineBackupRestoreProof{}, fmt.Errorf("clean-baseline source changed after authorized preview")
	}

	verificationCredentials, err := isolatedDatabaseCredentials(source, verificationDatabase)
	if err != nil {
		return CleanBaselineBackupRestoreProof{}, err
	}
	if err := manager.RestoreToNewDatabase(
		ctx,
		source,
		verificationCredentials,
		backupPath,
		migrationsDir,
		expected,
	); err != nil {
		return CleanBaselineBackupRestoreProof{}, manager.cleanBaselineRestoreFailure(
			source,
			verificationDatabase,
			err,
		)
	}
	cleanupFailure := func(primary error) (CleanBaselineBackupRestoreProof, error) {
		return CleanBaselineBackupRestoreProof{}, manager.cleanBaselineRestoreFailure(
			source,
			verificationDatabase,
			primary,
		)
	}

	facts, err := manager.Inspect(ctx, verificationCredentials)
	if err != nil || facts.PostgresMajor != 16 || len(facts.MigrationLedger) != len(expected) {
		return cleanupFailure(fmt.Errorf("isolated backup restore release schema readback failed"))
	}
	if err := manager.verifyWorkspaceRoleGrants(ctx, verificationCredentials); err != nil {
		return cleanupFailure(err)
	}
	restoredPreview, err := manager.PreviewCleanBaseline(ctx, verificationCredentials)
	if err != nil {
		return cleanupFailure(err)
	}
	restoredPreview, err = bindCleanBaselinePreviewToBackup(restoredPreview, backupSHA256)
	if err != nil {
		return cleanupFailure(err)
	}
	restoredDigest, err := cleanBaselineDatabasePreviewDigest(restoredPreview)
	if err != nil {
		return cleanupFailure(err)
	}
	if restoredDigest != sourceDigest {
		return cleanupFailure(fmt.Errorf("isolated backup restore differs from the quiesced source"))
	}
	if err := manager.RetireCleanBaselineVerificationDatabase(ctx, source, verificationDatabase); err != nil {
		return CleanBaselineBackupRestoreProof{}, err
	}
	if _, err := manager.VerifyBackup(ctx, backupPath, backupSHA256); err != nil {
		return CleanBaselineBackupRestoreProof{}, err
	}
	return sealCleanBaselineBackupRestoreProof(CleanBaselineBackupRestoreProof{
		BackupSHA256:               backupSHA256,
		VerificationDatabaseSHA256: hashCleanBaselineText(verificationDatabase),
		SourceSemanticSHA256:       sourceDigest,
		RestoredSemanticSHA256:     restoredDigest,
		PostgresMajor:              facts.PostgresMajor,
		MigrationCount:             len(facts.MigrationLedger),
		VerificationDatabaseAbsent: true,
	})
}

func bindCleanBaselinePreviewToBackup(
	preview CleanBaselineDatabasePreview,
	backupSHA256 string,
) (CleanBaselineDatabasePreview, error) {
	preview.CheckpointFloor.RollbackBackupSHA256 = backupSHA256
	floor, err := sealCleanBaselineCheckpointFloor(preview.CheckpointFloor)
	if err != nil {
		return CleanBaselineDatabasePreview{}, err
	}
	preview.CheckpointFloor = floor
	return preview, nil
}

func (manager PostgresManager) cleanBaselineRestoreFailure(
	control DatabaseCredentials,
	verificationDatabase string,
	primary error,
) error {
	cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cleanupErr := manager.RetireCleanBaselineVerificationDatabase(
		cleanupContext,
		control,
		verificationDatabase,
	)
	if cleanupErr != nil {
		return errors.Join(primary, fmt.Errorf("retire isolated restore database: %w", cleanupErr))
	}
	return primary
}

func (manager PostgresManager) RetireCleanBaselineVerificationDatabase(
	ctx context.Context,
	control DatabaseCredentials,
	verificationDatabase string,
) error {
	if !restoreDatabasePattern.MatchString(verificationDatabase) {
		return fmt.Errorf("isolated restore database name is invalid")
	}
	target, err := isolatedDatabaseCredentials(control, verificationDatabase)
	if err != nil {
		return err
	}
	return manager.retireCleanBaselineDatabase(ctx, control, target, false)
}

func (manager PostgresManager) RetireCleanBaselinePredecessorDatabase(
	ctx context.Context,
	active DatabaseCredentials,
	predecessor DatabaseCredentials,
) error {
	return manager.retireCleanBaselineDatabase(ctx, active, predecessor, true)
}

func (manager PostgresManager) retireCleanBaselineDatabase(
	ctx context.Context,
	control DatabaseCredentials,
	target DatabaseCredentials,
	predecessor bool,
) error {
	if control.Admin == nil || target.Admin == nil ||
		!sameServer(control.Admin, target.Admin) ||
		control.Admin.User != target.Admin.User ||
		control.Admin.Password != target.Admin.Password ||
		control.Admin.Database == target.Admin.Database {
		return fmt.Errorf("database retirement boundary is invalid")
	}
	targetDatabase := target.Admin.Database
	if predecessor {
		if !cleanBaselineDatabasePattern.MatchString(control.Admin.Database) ||
			!validCleanBaselinePredecessorDatabase(targetDatabase) {
			return fmt.Errorf("predecessor database retirement boundary is invalid")
		}
	} else if !restoreDatabasePattern.MatchString(targetDatabase) {
		return fmt.Errorf("restore verification database retirement boundary is invalid")
	}
	database, err := sql.Open("pgx", control.AdminDSN)
	if err != nil {
		return errors.New("database retirement connection cannot be opened")
	}
	defer database.Close()
	var connectedDatabase string
	if err := database.QueryRowContext(ctx, `SELECT current_database()`).Scan(&connectedDatabase); err != nil ||
		connectedDatabase != control.Admin.Database {
		return fmt.Errorf("database retirement control connection is invalid")
	}
	var exists bool
	if err := database.QueryRowContext(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`,
		targetDatabase,
	).Scan(&exists); err != nil {
		return errors.New("database retirement target inspection failed")
	}
	if !exists {
		return nil
	}
	var sessions int
	if err := database.QueryRowContext(
		ctx,
		`SELECT count(*) FROM pg_stat_activity WHERE datname = $1`,
		targetDatabase,
	).Scan(&sessions); err != nil {
		return errors.New("database retirement session inspection failed")
	}
	if sessions != 0 {
		return fmt.Errorf("database retirement target still has active sessions")
	}
	if _, err := database.ExecContext(
		ctx,
		`DROP DATABASE `+pgx.Identifier{targetDatabase}.Sanitize(),
	); err != nil {
		return fmt.Errorf("database retirement failed: %w", err)
	}
	if err := database.QueryRowContext(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`,
		targetDatabase,
	).Scan(&exists); err != nil || exists {
		return fmt.Errorf("database retirement absence proof failed")
	}
	return nil
}

func validCleanBaselinePredecessorDatabase(name string) bool {
	if name == "" || len([]byte(name)) > 63 || strings.TrimSpace(name) != name || strings.ContainsRune(name, 0) {
		return false
	}
	switch strings.ToLower(name) {
	case "postgres", "template0", "template1":
		return false
	default:
		return true
	}
}

func (manager PostgresManager) CreateCleanBaselineDatabase(
	ctx context.Context,
	current DatabaseCredentials,
	target DatabaseCredentials,
	migrationsDir string,
	expected []db.MigrationChecksum,
) (CleanBaselineDatabaseProof, error) {
	if manager.Runner == nil {
		return CleanBaselineDatabaseProof{}, fmt.Errorf("database command runner is not configured")
	}
	newDatabase := target.Admin.Database
	if !cleanBaselineDatabasePattern.MatchString(newDatabase) ||
		!sameServer(current.Admin, target.Admin) ||
		current.Admin.User != target.Admin.User ||
		current.Admin.Password != target.Admin.Password {
		return CleanBaselineDatabaseProof{}, fmt.Errorf("clean-baseline target database credential is invalid")
	}
	exists, err := manager.CleanBaselineDatabaseExists(ctx, current, newDatabase)
	if err != nil {
		return CleanBaselineDatabaseProof{}, err
	}
	if exists {
		return CleanBaselineDatabaseProof{}, fmt.Errorf("clean-baseline target database already exists")
	}
	arguments := []string{
		"--host=" + current.Admin.Host,
		"--port=" + strconv.Itoa(int(current.Admin.Port)),
		"--username=" + current.Admin.User,
		"--maintenance-db=" + current.Admin.Database,
		"--no-password",
		"--owner=xinsight_admin",
		"--template=template0",
		"--encoding=UTF8",
		newDatabase,
	}
	if _, err := manager.Runner.Run(ctx, Command{
		Name: "createdb", Args: arguments, Env: map[string]string{"PGPASSWORD": current.Admin.Password},
	}); err != nil {
		return CleanBaselineDatabaseProof{}, err
	}
	if _, err := db.RunMigrations(ctx, target.AdminDSN, migrationsDir); err != nil {
		return CleanBaselineDatabaseProof{}, fmt.Errorf("clean-baseline migration failed: %w", err)
	}
	if err := manager.ApplyGrants(ctx, target); err != nil {
		return CleanBaselineDatabaseProof{}, err
	}
	return manager.VerifyCleanBaselineDatabase(ctx, target, expected)
}

func (manager PostgresManager) VerifyCleanBaselineDatabase(
	ctx context.Context,
	target DatabaseCredentials,
	expected []db.MigrationChecksum,
) (CleanBaselineDatabaseProof, error) {
	facts, err := manager.Inspect(ctx, target)
	if err != nil || facts.PostgresMajor != 16 {
		return CleanBaselineDatabaseProof{}, fmt.Errorf("clean-baseline target requires PostgreSQL 16")
	}
	if err := validateLedgerPrefix(facts.MigrationLedger, expected, false); err != nil {
		return CleanBaselineDatabaseProof{}, err
	}
	database, err := sql.Open("pgx", target.AdminDSN)
	if err != nil {
		return CleanBaselineDatabaseProof{}, errors.New("clean-baseline target cannot be opened")
	}
	defer database.Close()
	contract, err := InspectCleanBaselineSchemaContract(ctx, database)
	if err != nil {
		return CleanBaselineDatabaseProof{}, err
	}
	if err := schemaContractDriftError(contract); err != nil || contract.Objects.Tables != len(cleanBaselineTables) {
		if err != nil {
			return CleanBaselineDatabaseProof{}, err
		}
		return CleanBaselineDatabaseProof{}, fmt.Errorf("clean-baseline target schema table classification is invalid")
	}
	sourceRows, sourceDigest, adapterRows, adapterDigest, runtimeRows, err := inspectReleaseCatalog(ctx, database)
	if err != nil || sourceRows != 1 || sourceDigest != cleanBaselineSourceCatalogSHA256 ||
		adapterRows != 2 || adapterDigest != cleanBaselineAdapterCatalogSHA256 || runtimeRows != 0 {
		return CleanBaselineDatabaseProof{}, fmt.Errorf("clean-baseline target release catalog is invalid")
	}
	var historicalRows int64
	for _, table := range cleanBaselineTables {
		if table == "schema_migrations" || table == "source_systems" ||
			table == "adapter_manifests" {
			continue
		}
		var count int64
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`"`).Scan(&count); err != nil {
			return CleanBaselineDatabaseProof{}, fmt.Errorf("clean-baseline target count failed: %s", table)
		}
		historicalRows += count
	}
	var stateRows, operationRows, historyRows int64
	if err := database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM adapter_import_states),
  (SELECT count(*) FROM connector_checkpoint_epoch_operations),
  (SELECT count(*) FROM connector_checkpoint_epoch_history)`).Scan(
		&stateRows,
		&operationRows,
		&historyRows,
	); err != nil {
		return CleanBaselineDatabaseProof{}, err
	}
	if historicalRows != 0 || stateRows != 0 || operationRows != 0 || historyRows != 0 {
		return CleanBaselineDatabaseProof{}, fmt.Errorf("clean-baseline target contains historical rows")
	}
	if err := manager.verifyWorkspaceRoleGrants(ctx, target); err != nil {
		return CleanBaselineDatabaseProof{}, err
	}
	return CleanBaselineDatabaseProof{
		PostgresMajor: facts.PostgresMajor, MigrationCount: len(facts.MigrationLedger),
		SchemaContractVersion: contract.SchemaVersion,
		SchemaContractSHA256:  contract.ActualSHA256,
		SchemaLayoutSHA256:    contract.ActualLayoutSHA256,
		SchemaLayoutMatches:   contract.LayoutMatches,
		MigrationSetSHA256:    contract.Actual.MigrationSetSHA256,
		SourceCatalogRows:     sourceRows,
		AdapterCatalogRows:    adapterRows, HistoricalRows: historicalRows,
		CheckpointStateRows: stateRows, EpochOperationRows: operationRows, EpochHistoryRows: historyRows,
	}, nil
}

func (manager PostgresManager) BootstrapEmptyCheckpoint(
	ctx context.Context,
	target DatabaseCredentials,
	input checkpointepoch.PrepareInput,
	nextEpoch int64,
	activatedAt time.Time,
) (checkpointepoch.ActiveStateSummary, error) {
	if err := validateCleanBaselineBinding(input); err != nil {
		return checkpointepoch.ActiveStateSummary{}, err
	}
	database, err := sql.Open("pgx", target.AdminDSN)
	if err != nil {
		return checkpointepoch.ActiveStateSummary{}, errors.New("checkpoint bootstrap target cannot be opened")
	}
	defer database.Close()
	return checkpointepoch.BootstrapEmpty(ctx, database, checkpointepoch.EmptyBootstrapInput{
		PrepareInput: input, CheckpointEpoch: nextEpoch,
	}, activatedAt)
}

func (manager PostgresManager) VerifyEmptyCheckpoint(
	ctx context.Context,
	target DatabaseCredentials,
	input checkpointepoch.PrepareInput,
	nextEpoch int64,
) (checkpointepoch.ActiveStateSummary, error) {
	if err := validateCleanBaselineBinding(input); err != nil {
		return checkpointepoch.ActiveStateSummary{}, err
	}
	database, err := sql.Open("pgx", target.AdminDSN)
	if err != nil {
		return checkpointepoch.ActiveStateSummary{}, errors.New("checkpoint bootstrap target cannot be opened")
	}
	defer database.Close()
	var stateRows, operationRows, historyRows int64
	if err := database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM adapter_import_states),
  (SELECT count(*) FROM connector_checkpoint_epoch_operations),
  (SELECT count(*) FROM connector_checkpoint_epoch_history)`).Scan(
		&stateRows, &operationRows, &historyRows,
	); err != nil {
		return checkpointepoch.ActiveStateSummary{}, err
	}
	if stateRows != 1 || operationRows != 0 || historyRows != 0 {
		return checkpointepoch.ActiveStateSummary{}, fmt.Errorf("checkpoint bootstrap state/history readback is invalid")
	}
	store, err := checkpointepoch.NewStore(database)
	if err != nil {
		return checkpointepoch.ActiveStateSummary{}, err
	}
	report, err := store.StatusByBinding(ctx, input.Binding, time.Now().UTC())
	if err != nil {
		return checkpointepoch.ActiveStateSummary{}, err
	}
	active := report.Active
	if active.Epoch != nextEpoch || active.EpochOperationID != "" ||
		active.SourceInstance != input.NextSourceInstance ||
		active.CursorSHA256 != hashCleanBaselineText(input.NextCursor) ||
		active.HighWatermarkSHA256 != hashCleanBaselineText(input.NextHighWatermark) ||
		active.WorkerStatus != "inactive" || active.PagesCommitted != 0 ||
		active.ItemsReceived != 0 || active.ItemsCommitted != 0 ||
		active.DuplicateNoop != 0 || active.ConsecutiveFailures != 0 {
		return checkpointepoch.ActiveStateSummary{}, fmt.Errorf("checkpoint bootstrap readback is invalid")
	}
	return active, nil
}

func cleanBaselineBinding() checkpointepoch.Binding {
	return checkpointepoch.Binding{
		SourceSystemID:    cleanBaselineSourceSystemID,
		AdapterManifestID: cleanBaselineAdapterManifestID,
		CursorKey:         checkpointepoch.CompletedExecutionCursorKey,
	}
}

func validateCleanBaselineBinding(input checkpointepoch.PrepareInput) error {
	if input.SourceSystemID != cleanBaselineSourceSystemID ||
		input.AdapterManifestID != cleanBaselineAdapterManifestID ||
		input.CursorKey != checkpointepoch.CompletedExecutionCursorKey ||
		input.WorkerUnit != checkpointepoch.CompletedExecutionWorkerUnit {
		return fmt.Errorf("clean-baseline supports only the release-defined UniRouter completed-execution binding")
	}
	request := checkpointepoch.PrepareRequest{
		SchemaVersion: checkpointepoch.PrepareRequestSchema,
		Binding:       input.Binding, WorkerUnit: input.WorkerUnit,
		NextSourceInstance: input.NextSourceInstance, NextCursor: input.NextCursor,
		NextHighWatermark: input.NextHighWatermark, AuthorizationRef: input.AuthorizationRef,
	}
	payload, err := marshalJSON(request)
	if err != nil {
		return err
	}
	_, err = checkpointepoch.DecodePrepareRequest(payload)
	return err
}

const cleanBaselineSchemaQuery = `
WITH objects AS (
  SELECT 'N'::text AS kind, n.nspname::text AS namespace, ''::text AS owner,
         n.nspname::text AS name, ''::text AS definition, ''::text AS layout
  FROM pg_namespace AS n
  WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'
  UNION ALL
  SELECT 'T', n.nspname, class.relname, '',
         jsonb_build_object('persistence', class.relpersistence, 'kind', class.relkind)::text, ''
  FROM pg_class AS class
  JOIN pg_namespace AS n ON n.oid = class.relnamespace
  WHERE n.nspname = 'public' AND class.relkind IN ('r', 'p')
  UNION ALL
  SELECT 'C', n.nspname, class.relname, attribute.attname,
         jsonb_build_object(
           'formatted_type', format_type(attribute.atttypid, attribute.atttypmod),
           'type_namespace', type_namespace.nspname,
           'type_name', type.typname,
           'type_kind', type.typtype,
           'domain_base_type', CASE WHEN type.typtype = 'd' THEN format_type(type.typbasetype, type.typtypmod) ELSE '' END,
           'nullable', NOT attribute.attnotnull,
           'default_expression', COALESCE(pg_get_expr(attribute_default.adbin, attribute_default.adrelid, true), ''),
           'identity_state', attribute.attidentity,
           'identity_generation', COALESCE(info.identity_generation, ''),
           'identity_start', COALESCE(info.identity_start, ''),
           'identity_increment', COALESCE(info.identity_increment, ''),
           'identity_maximum', COALESCE(info.identity_maximum, ''),
           'identity_minimum', COALESCE(info.identity_minimum, ''),
           'identity_cycle', COALESCE(info.identity_cycle, ''),
           'generated_state', attribute.attgenerated,
           'generation_expression', COALESCE(info.generation_expression, ''),
           'collation_namespace', COALESCE(collation_namespace.nspname, ''),
           'collation_name', COALESCE(column_collation.collname, '')
         )::text,
         attribute.attnum::text
  FROM pg_attribute AS attribute
  JOIN pg_class AS class ON class.oid = attribute.attrelid
  JOIN pg_namespace AS n ON n.oid = class.relnamespace
  JOIN pg_type AS type ON type.oid = attribute.atttypid
  JOIN pg_namespace AS type_namespace ON type_namespace.oid = type.typnamespace
  LEFT JOIN pg_attrdef AS attribute_default
    ON attribute_default.adrelid = attribute.attrelid AND attribute_default.adnum = attribute.attnum
  LEFT JOIN pg_collation AS column_collation ON column_collation.oid = NULLIF(attribute.attcollation, 0)
  LEFT JOIN pg_namespace AS collation_namespace ON collation_namespace.oid = column_collation.collnamespace
  LEFT JOIN information_schema.columns AS info
    ON info.table_schema = n.nspname AND info.table_name = class.relname AND info.column_name = attribute.attname
  WHERE n.nspname = 'public' AND class.relkind IN ('r', 'p')
    AND attribute.attnum > 0 AND NOT attribute.attisdropped
  UNION ALL
  SELECT 'K', namespace.nspname, relation.relname, con.conname,
         con.contype::text || '|' || pg_get_constraintdef(con.oid, true), ''
  FROM pg_constraint con
  JOIN pg_namespace AS namespace ON namespace.oid = con.connamespace
  JOIN pg_class AS relation ON relation.oid = con.conrelid
  WHERE namespace.nspname = 'public' AND con.contype IN ('p','u','f','c','x')
  UNION ALL
  SELECT 'I', idx.schemaname, idx.tablename, idx.indexname, idx.indexdef, ''
  FROM pg_indexes idx WHERE idx.schemaname = 'public'
  UNION ALL
  SELECT 'G', namespace.nspname, relation.relname, trg.tgname, pg_get_triggerdef(trg.oid, true), ''
  FROM pg_trigger trg
  JOIN pg_class AS relation ON relation.oid = trg.tgrelid
  JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
  WHERE NOT trg.tgisinternal AND namespace.nspname = 'public'
  UNION ALL
  -- Function semantics include every execution-affecting pg_proc property.
  -- The managed connector and work-install EXECUTE grants are enforced by
  -- the role boundary verifier and omitted here. The synthetic resource identity
  -- importer grant remains contract-visible, so generation must follow
  -- ApplyGrants. Every other non-owner ACL entry, including PUBLIC or
  -- grant-option access, remains contract-visible.
  SELECT 'F', 'public', pg_get_userbyid(p.proowner),
         p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')',
         jsonb_build_object(
           'kind', p.prokind,
           'language', language.lanname,
           'returns', pg_get_function_result(p.oid),
           'arguments', pg_get_function_arguments(p.oid),
           'argument_defaults', COALESCE(pg_get_expr(p.proargdefaults, 0, true), ''),
           'volatility', p.provolatile,
           'strict', p.proisstrict,
           'security_definer', p.prosecdef,
           'leakproof', p.proleakproof,
           'parallel', p.proparallel,
           'cost', p.procost,
           'rows', p.prorows,
           'config', COALESCE(to_jsonb(p.proconfig), '[]'::jsonb),
           'body', p.prosrc,
           'unexpected_acl', COALESCE((
             SELECT jsonb_agg(
               jsonb_build_object(
                 'grantor', pg_get_userbyid(privilege.grantor),
                 'grantee', CASE WHEN privilege.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(privilege.grantee) END,
                 'privilege', privilege.privilege_type,
                 'grantable', privilege.is_grantable
               ) ORDER BY privilege.grantor, privilege.grantee, privilege.privilege_type, privilege.is_grantable
             )
             FROM aclexplode(COALESCE(p.proacl, acldefault('f', p.proowner))) AS privilege
             WHERE privilege.grantee <> p.proowner
               AND NOT (
                 p.proname = 'xinsight_redeem_connector_invitation'
                 AND oidvectortypes(p.proargtypes) = 'text, text, text, timestamp with time zone'
                 AND pg_get_userbyid(privilege.grantor) = pg_get_userbyid(p.proowner)
                 AND pg_get_userbyid(privilege.grantee) = 'xinsight_workspace'
                 AND privilege.privilege_type = 'EXECUTE'
                 AND NOT privilege.is_grantable
               )
               AND NOT (
                 p.proname = 'xinsight_managed_work_install_id'
                 AND oidvectortypes(p.proargtypes) = 'text'
                 AND pg_get_userbyid(privilege.grantor) = pg_get_userbyid(p.proowner)
                 AND pg_get_userbyid(privilege.grantee) = 'xinsight_workspace'
                 AND privilege.privilege_type = 'EXECUTE'
                 AND NOT privilege.is_grantable
               )
           ), '[]'::jsonb)
         )::text, ''
  FROM pg_proc AS p
  JOIN pg_language AS language ON language.oid = p.prolang
  WHERE p.pronamespace = 'public'::regnamespace
    AND NOT EXISTS (
      SELECT 1 FROM pg_depend AS dependency
      WHERE dependency.classid = 'pg_proc'::regclass
        AND dependency.objid = p.oid
        AND dependency.deptype = 'e'
    )
  UNION ALL
  SELECT 'E', extension_namespace.nspname, '', extension.extname, extension.extversion, ''
  FROM pg_extension AS extension
  JOIN pg_namespace AS extension_namespace ON extension_namespace.oid = extension.extnamespace
  UNION ALL
  SELECT 'Y', 'public', '', type.typname,
         concat_ws('|', type.typtype::text, type.typcategory::text,
                   type.typnotnull::text, type.typbasetype::regtype::text), ''
  FROM pg_type AS type
  WHERE type.typnamespace = 'public'::regnamespace
    AND type.typrelid = 0
    AND type.typelem = 0
    AND NOT EXISTS (
      SELECT 1 FROM pg_depend AS dependency
      WHERE dependency.classid = 'pg_type'::regclass
        AND dependency.objid = type.oid
        AND dependency.deptype = 'e'
    )
  UNION ALL
  SELECT 'R', 'public', '', class.relname, class.relkind::text, ''
  FROM pg_class AS class
  WHERE class.relnamespace = 'public'::regnamespace
    AND class.relkind NOT IN ('r', 'i')
)
SELECT kind, namespace, owner, name, definition, layout
FROM objects ORDER BY kind, namespace, owner, name`

const cleanBaselineFunctionPrivilegeQuery = `
WITH function_privileges AS (
  SELECT p.proname,
         oidvectortypes(p.proargtypes) AS argument_types,
         pg_get_userbyid(p.proowner) AS owner,
         pg_get_userbyid(privilege.grantor) AS grantor,
         CASE WHEN privilege.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(privilege.grantee) END AS grantee,
         privilege.privilege_type,
         privilege.is_grantable
  FROM pg_proc AS p
  CROSS JOIN LATERAL aclexplode(COALESCE(p.proacl, acldefault('f', p.proowner))) AS privilege
  WHERE p.pronamespace = 'public'::regnamespace
    AND privilege.grantee <> p.proowner
    AND NOT EXISTS (
      SELECT 1 FROM pg_depend AS dependency
      WHERE dependency.classid = 'pg_proc'::regclass
        AND dependency.objid = p.oid
        AND dependency.deptype = 'e'
    )
), classified AS (
  SELECT *,
         ((proname = 'xinsight_redeem_connector_invitation'
           AND argument_types = 'text, text, text, timestamp with time zone'
           AND grantee = 'xinsight_workspace')
          OR (proname = 'synthetic_resource_cluster_id' AND argument_types = ''
           AND grantee = 'xinsight_importer')
          OR (proname = 'xinsight_managed_work_install_id' AND argument_types = 'text'
           AND grantee = 'xinsight_workspace'))
           AND grantor = owner
           AND privilege_type = 'EXECUTE'
           AND NOT is_grantable AS managed
  FROM function_privileges
)
SELECT count(*) FILTER (WHERE NOT managed),
       count(*) FILTER (WHERE managed)
FROM classified`

type cleanBaselineSchemaQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func inspectCleanBaselineSchema(ctx context.Context, database cleanBaselineSchemaQuerier) (string, CleanBaselineSchemaObjects, error) {
	manifest, err := buildSchemaContractManifest(ctx, database)
	if err != nil {
		return "", CleanBaselineSchemaObjects{}, err
	}
	return manifest.SemanticSHA256, manifest.Objects, nil
}

func inspectReleaseCatalog(ctx context.Context, database *sql.DB) (int64, string, int64, string, int64, error) {
	sourceRows, sourceDigest, err := digestQueryRows(ctx, database, `
SELECT (to_jsonb(source_systems) - 'created_at' - 'updated_at')::text
FROM source_systems
WHERE id = '11111111-1111-1111-1111-111111111111'::uuid
ORDER BY id`)
	if err != nil {
		return 0, "", 0, "", 0, err
	}
	adapterRows, adapterDigest, err := digestQueryRows(ctx, database, `
SELECT (to_jsonb(adapter_manifests) - 'created_at' - 'updated_at')::text
FROM adapter_manifests
WHERE id IN (
  '22222222-2222-4222-8222-222222222224'::uuid,
  '22222222-2222-4222-8222-222222222225'::uuid
)
ORDER BY id`)
	if err != nil {
		return 0, "", 0, "", 0, err
	}
	var totalSources, totalAdapters int64
	if err := database.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM source_systems), (SELECT count(*) FROM adapter_manifests)`).Scan(
		&totalSources,
		&totalAdapters,
	); err != nil {
		return 0, "", 0, "", 0, err
	}
	runtimeRows := (totalSources - sourceRows) + (totalAdapters - adapterRows)
	if runtimeRows < 0 {
		return 0, "", 0, "", 0, fmt.Errorf("release catalog row count underflow")
	}
	return sourceRows, sourceDigest, adapterRows, adapterDigest, runtimeRows, nil
}

func digestTableRows(ctx context.Context, database *sql.DB, table string) (int64, string, error) {
	index := sort.SearchStrings(cleanBaselineTables, table)
	if index == len(cleanBaselineTables) || cleanBaselineTables[index] != table {
		return 0, "", fmt.Errorf("unknown clean-baseline table")
	}
	query := `SELECT to_jsonb(row_value)::text FROM "` + table + `" AS row_value ORDER BY to_jsonb(row_value)::text`
	count, digest, err := digestQueryRows(ctx, database, query)
	if err != nil {
		return 0, "", fmt.Errorf("digest table %s: %w", table, err)
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(table + "\n" + digest + "\n"))
	return count, "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func digestQueryRows(ctx context.Context, database *sql.DB, query string, args ...any) (int64, string, error) {
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, "", err
	}
	defer rows.Close()
	digest := sha256.New()
	var count int64
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return 0, "", err
		}
		_, _ = digest.Write([]byte(value))
		_, _ = digest.Write([]byte{'\n'})
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, "", err
	}
	return count, "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}

func inspectCheckpointHistory(
	ctx context.Context,
	database *sql.DB,
	binding checkpointepoch.Binding,
) (CleanBaselineCheckpointFloor, error) {
	rows, digest, err := digestQueryRows(ctx, database, `
WITH identities AS (
  SELECT 'state'::text AS kind, checkpoint_epoch AS epoch,
         COALESCE(activation_source_instance, '') AS source_instance,
         encode(digest(COALESCE(cursor_value, ''), 'sha256'), 'hex') AS cursor_sha256,
         encode(digest(COALESCE(high_watermark, ''), 'sha256'), 'hex') AS high_watermark_sha256
  FROM adapter_import_states
  WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3
  UNION ALL
  SELECT 'history', checkpoint_epoch, source_instance,
         encode(digest(final_cursor, 'sha256'), 'hex'),
         encode(digest(final_high_watermark, 'sha256'), 'hex')
  FROM connector_checkpoint_epoch_history
  WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3
  UNION ALL
  SELECT 'operation', expected_epoch + 1, next_source_instance,
         encode(digest(next_cursor, 'sha256'), 'hex'),
         encode(digest(next_high_watermark, 'sha256'), 'hex')
  FROM connector_checkpoint_epoch_operations
  WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3
)
SELECT concat_ws('|', kind, epoch::text, source_instance, cursor_sha256, high_watermark_sha256)
FROM identities ORDER BY kind, epoch, source_instance, cursor_sha256, high_watermark_sha256`,
		binding.SourceSystemID,
		binding.AdapterManifestID,
		binding.CursorKey,
	)
	if err != nil {
		return CleanBaselineCheckpointFloor{}, err
	}
	var maximum int64
	if err := database.QueryRowContext(ctx, `
WITH epochs AS (
  SELECT checkpoint_epoch AS epoch FROM adapter_import_states
  WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3
  UNION ALL
  SELECT checkpoint_epoch FROM connector_checkpoint_epoch_history
  WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3
  UNION ALL
  SELECT expected_epoch + 1 FROM connector_checkpoint_epoch_operations
  WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3
)
SELECT COALESCE(max(epoch), 0) FROM epochs`, binding.SourceSystemID, binding.AdapterManifestID, binding.CursorKey).Scan(&maximum); err != nil {
		return CleanBaselineCheckpointFloor{}, err
	}
	identityHashes, err := inspectCheckpointIdentityHashes(ctx, database, binding)
	if err != nil {
		return CleanBaselineCheckpointFloor{}, err
	}
	return CleanBaselineCheckpointFloor{
		Binding:                   binding,
		PriorIdentityRows:         rows,
		PriorIdentitySHA256:       digest,
		HistoricalMaxEpoch:        maximum,
		PriorSourceInstanceSHA256: identityHashes.sources,
		PriorCursorSHA256:         identityHashes.cursors,
		PriorHighWatermarkSHA256:  identityHashes.highWatermarks,
	}, nil
}

type checkpointIdentityHashes struct {
	sources        []string
	cursors        []string
	highWatermarks []string
}

func inspectCheckpointIdentityHashes(
	ctx context.Context,
	database *sql.DB,
	binding checkpointepoch.Binding,
) (checkpointIdentityHashes, error) {
	const identityCTE = `
WITH identities AS (
  SELECT activation_source_instance AS source_instance,
         cursor_value AS cursor_value,
         high_watermark AS high_watermark
  FROM adapter_import_states
  WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3
  UNION ALL
  SELECT source_instance, final_cursor, final_high_watermark
  FROM connector_checkpoint_epoch_history
  WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3
  UNION ALL
  SELECT previous_source_instance, NULL::text, NULL::text
  FROM connector_checkpoint_epoch_operations
  WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3
  UNION ALL
  SELECT next_source_instance, next_cursor, next_high_watermark
  FROM connector_checkpoint_epoch_operations
  WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3
)`
	queryHashes := func(column string) ([]string, error) {
		rows, err := database.QueryContext(ctx, identityCTE+`
SELECT DISTINCT 'sha256:' || encode(digest(`+column+`, 'sha256'), 'hex')
FROM identities WHERE `+column+` IS NOT NULL AND `+column+` <> '' ORDER BY 1`,
			binding.SourceSystemID, binding.AdapterManifestID, binding.CursorKey,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var result []string
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return result, nil
	}
	sources, err := queryHashes("source_instance")
	if err != nil {
		return checkpointIdentityHashes{}, err
	}
	cursors, err := queryHashes("cursor_value")
	if err != nil {
		return checkpointIdentityHashes{}, err
	}
	highWatermarks, err := queryHashes("high_watermark")
	if err != nil {
		return checkpointIdentityHashes{}, err
	}
	return checkpointIdentityHashes{sources: sources, cursors: cursors, highWatermarks: highWatermarks}, nil
}

func hashCleanBaselineText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
