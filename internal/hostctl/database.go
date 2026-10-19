package hostctl

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewbody"
)

const maxBackupBytes = int64(1 << 40)

var restoreDatabasePattern = regexp.MustCompile(`^xinsight_restore_[a-z0-9_]{1,40}$`)
var cleanBaselineDatabasePattern = regexp.MustCompile(`^xinsight_clean_[a-z0-9_]{1,40}$`)

var workspaceInsertTables = []string{
	"investigation_cases", "case_executions", "case_findings",
	"case_evidence_links", "case_gap_links", "case_resource_links", "case_content_links", "case_annotations",
	"saved_investigation_views", "capture_gap_resolutions", "investigation_alert_case_links",
	"discovery_analysis_runs", "discovery_hypotheses", "access_audit_records",
	"workbench_readings", "workbench_reading_members", "workbench_saved_filters",
	"workbench_saved_filter_operations",
	"workbench_source_members", "workbench_source_manifests",
	"runtime_readings", "runtime_reading_requests", "runtime_reading_members",
	"runtime_reading_source_members", "runtime_reading_economic_members",
	"runtime_reading_resource_members", "runtime_reading_content_members", "runtime_reading_content_refs",
	"budget_readings", "budget_reading_members",
	"workbench_purpose_revisions", "workbench_purpose_members", "workbench_purpose_manifests",
	"case_finding_followups", "case_finding_actions",
	"case_finding_assessments", "case_finding_assessment_actions", "finding_review_creations",
	"investigation_queue_filters", "investigation_queue_filter_operations",
	"investigation_case_scope_labels",
	"work_event_documents", "work_event_relations",
	"review_body_project_usage", "review_body_operations", "review_body_objects", "review_body_copy_attempts",
}

var workspaceUpdateTables = []string{
	"investigation_queue_filters",
	"case_finding_followups",
	"case_finding_assessments",
	"investigation_cases",
	"capture_gap_resolutions",
	"connector_control_operations",
}

var importerInsertTables = []string{
	"connector_sync_slots",
	"adapter_import_states", "source_systems", "adapter_manifests",
	"import_provenance", "import_provenance_events", "evidence_events", "capture_gaps",
	"connector_import_operation_bindings", "connector_operational_receipts",
	"workbench_economic_facts",
	"source_fact_documents",
	"source_observation_documents",
	"resource_access_records", "source_content_manifests", "source_content_receipts",
	"work_event_documents", "work_event_relations",
	"source_runtime_snapshots", "source_runtime_snapshot_pages",
	"source_budget_catalogs", "source_budget_catalog_entries", "source_budget_readings", "source_budget_receipts", "source_budget_offline_imports",
}

type tableColumnGrant struct {
	table   string
	columns []string
}

type databaseRoleSpec struct {
	name        string
	password    string
	readOnly    bool
	timeout     string
	connections int
}

var importerUpdateColumnGrants = []tableColumnGrant{
	{table: "connector_sync_slots", columns: []string{"owner", "token", "revision", "busy", "started_at", "ended_at", "last_error"}},
	{table: "connector_sync_runs", columns: []string{"pages", "attempts", "state", "results", "observed_items"}},
	{table: "connector_sync_controls", columns: []string{"last_checked_at", "last_data_at", "last_error"}},
	{table: "source_content_collections", columns: []string{"revision", "cursor_value", "last_sequence", "high_watermark_sequence", "high_watermark", "coverage", "gaps", "retention_floor", "requires_attention", "last_error", "last_attempt_at", "last_success_at"}},
	{table: "source_budget_collections", columns: []string{"revision", "requires_attention", "last_error", "access_generation", "catalog_revision", "granted_profiles", "cycle", "visible_cycle", "catalog_id", "catalog_cursor", "entry_index", "alerts_cursor", "last_attempt_at", "last_success_at"}},
	{table: "source_runtime_snapshots", columns: []string{"revision", "generation", "page_ordinal", "cursor_value", "visible_generation", "requires_attention", "last_error", "last_attempt_at", "last_success_at"}},
	// PostgreSQL requires an UPDATE privilege for SELECT FOR SHARE. The ALWAYS
	// trigger denies importer mutations; this grant only permits the auth lock.
	{table: "source_fact_offline_authorizations", columns: []string{"revision"}},
	{table: "source_fact_streams", columns: []string{"cursor_value", "high_watermark", "revision", "last_error", "last_received_at", "updated_at", "requires_attention"}},
	{table: "work_event_collections", columns: []string{"cursor_value", "high_watermark", "revision", "last_error", "last_received_at", "updated_at", "requires_attention"}},
	{
		table: "adapter_import_states",
		columns: []string{
			"cursor_value", "high_watermark", "status", "worker_status",
			"last_import_started_at", "last_import_completed_at", "last_poll_failed_at",
			"last_error", "pages_committed", "items_received", "items_committed",
			"duplicate_noop", "consecutive_failures", "updated_at",
		},
	},
	{table: "source_systems", columns: []string{"source_version", "status"}},
	{table: "adapter_manifests", columns: []string{"adapter_version", "active"}},
	{table: "evidence_events", columns: []string{"import_provenance_id"}},
}

var workspaceUpdateColumnGrants = []tableColumnGrant{
	// Row locks for authenticated ingestion require PostgreSQL UPDATE privilege;
	// ALWAYS triggers deny any workspace-role mutation of authority rows.
	{table: "work_event_collections", columns: []string{"revision"}},
	{table: "work_event_ingest_bindings", columns: []string{"revision"}},
	{table: "work_event_ur_targets", columns: []string{"revision"}},
	{table: "review_capture_defaults", columns: []string{"desired_revision"}},
	{table: "review_capture_exceptions", columns: []string{"desired_revision"}},
	{table: "review_capture_project_bindings", columns: []string{"revision"}},
	{table: "review_body_ingest_bindings", columns: []string{"revision"}},
	{table: "review_body_project_usage", columns: []string{"stored_bytes", "reserved_bytes"}},
	{table: "review_body_operations", columns: []string{"state", "reason", "lease_expires_at"}},
	{table: "review_body_copy_attempts", columns: []string{"status", "reason", "attempts", "next_attempt_at", "last_attempt_at"}},
	{table: "source_systems", columns: []string{"status"}},
	{table: "budget_readings", columns: []string{"membership_sha256", "budget_count", "sealed"}},
	{table: "investigation_case_scope_labels", columns: []string{"schema_version", "case_version", "requirements", "content_digest", "labeled_at"}},
	{table: "workbench_readings", columns: []string{"membership_sha256", "coverage"}},
	{table: "runtime_readings", columns: []string{"membership_sha256", "coverage", "known_entry_requests", "unknown_entry_requests", "observation_count", "interpretation_revision", "content_authorization_revision"}},
	{table: "workbench_saved_filters", columns: []string{
		"name", "config", "content_sha256", "revision", "updated_at",
	}},
	{table: "workbench_saved_filter_operations", columns: []string{"result"}},
	{
		table: "service_identities",
		columns: []string{
			"status", "updated_at", "revoked_at", "revoke_reason", "last_used_at",
		},
	},
	{
		table:   "service_identity_tokens",
		columns: []string{"revoked_at", "last_used_at"},
	},
	{
		table: "connector_installations",
		columns: []string{
			"source_instance", "source_instance_sha256", "status", "observed_state",
			"status_reason_code", "last_agent_seen_at", "updated_at", "version",
		},
	},
}

var workspaceInsertTablesArraySQL = textArraySQL(workspaceInsertTables)

const connectorInvitationRedemptionFunction = `public.xinsight_redeem_connector_invitation(text,text,text,timestamp with time zone)`
const managedWorkInstallIdentityFunction = `public.xinsight_managed_work_install_id(text)`

var workspaceUpdateTablesArraySQL = textArraySQL(workspaceUpdateTables)

// rc.10 shipped before workspace service-identity writes were narrowed from
// table UPDATE to explicit columns. Preflight may validate this exact source
// policy; the upgrade journal still applies and verifies the target policy.
const rc10WorkspaceUpdateTablesArraySQL = `ARRAY[
'investigation_cases', 'service_identities', 'service_identity_tokens',
'capture_gap_resolutions'
]::text[]`

var importerInsertTablesArraySQL = textArraySQL(importerInsertTables)

// Only replaceable lease inventory can be pruned by the importer. Source facts,
// observations, receipts and event checkpoints never receive DELETE authority.
var importerDeleteTables = []string{"source_runtime_snapshot_pages"}
var importerDeleteTablesArraySQL = textArraySQL(importerDeleteTables)

func textArraySQL(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, quotePostgresLiteral(value))
	}
	return "ARRAY[" + strings.Join(quoted, ",") + "]::text[]"
}

type DatabaseCredentials struct {
	SetRef       string
	ReaderDSN    string
	WorkspaceDSN string
	ImporterDSN  string
	AdminDSN     string
	Reader       *pgx.ConnConfig
	Workspace    *pgx.ConnConfig
	Importer     *pgx.ConnConfig
	Admin        *pgx.ConnConfig
}

type DatabaseFacts struct {
	PostgresMajor        int                  `json:"postgres_major"`
	SystemIdentifier     string               `json:"system_identifier,omitempty"`
	DatabaseName         string               `json:"database_name"`
	DatabaseSizeBytes    int64                `json:"database_size_bytes"`
	PublicTableCount     int                  `json:"public_table_count"`
	MigrationLedger      []db.MigrationRecord `json:"migration_ledger"`
	ReaderRoleExists     bool                 `json:"reader_role_exists"`
	WorkspaceRoleExists  bool                 `json:"workspace_role_exists"`
	ImporterRoleExists   bool                 `json:"importer_role_exists"`
	AdminRoleIsSuperuser bool                 `json:"admin_role_is_superuser"`
}

type BackupResult struct {
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	SizeBytes      int64  `json:"size_bytes"`
	PGDumpMajor    int    `json:"pg_dump_major"`
	PGRestoreMajor int    `json:"pg_restore_major"`
}

type EvidenceFingerprint struct {
	EventCount  int64  `json:"event_count"`
	SampleCount int    `json:"sample_count"`
	SHA256      string `json:"sha256"`
}

type Maintenance interface {
	RunMigrations(context.Context, string) (int, error)
	AdoptLegacyMigrationChecksums(context.Context, string, []db.MigrationChecksum) (int, error)
	CountOtherSessions(context.Context, []string) (int, error)
	SetRoleLogin(context.Context, string, bool) error
	Close() error
}

type DatabaseManager interface {
	Inspect(context.Context, DatabaseCredentials) (DatabaseFacts, error)
	EnsureRoles(context.Context, DatabaseCredentials) error
	ApplyGrants(context.Context, DatabaseCredentials) error
	VerifyUpgradeSourceRoleBoundaries(context.Context, DatabaseCredentials, string) error
	VerifyRoleBoundaries(context.Context, DatabaseCredentials) error
	EvidenceFingerprint(context.Context, DatabaseCredentials) (EvidenceFingerprint, error)
	DatabaseExists(context.Context, DatabaseCredentials, string) (bool, error)
	AcquireMaintenance(context.Context, DatabaseCredentials) (Maintenance, error)
	Backup(context.Context, DatabaseCredentials, string) (BackupResult, error)
	VerifyBackup(context.Context, string, string) (BackupResult, error)
	RestoreToNewDatabase(context.Context, DatabaseCredentials, DatabaseCredentials, string, string, []db.MigrationChecksum) error
	VerifyReviewBodyRestore(context.Context, DatabaseCredentials, reviewbody.KeyRing, time.Time) error
	VerifyReviewBodyInventory(context.Context, DatabaseCredentials) error
	SuspendForeignRestoredAuthority(context.Context, DatabaseCredentials, string, string, time.Time) error
}

// Every ciphertext counts, including expired objects awaiting physical expiry.
func (manager PostgresManager) VerifyReviewBodyInventory(ctx context.Context, credentials DatabaseCredentials) error {
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return fmt.Errorf("custody inventory unavailable")
	}
	defer database.Close()
	var present bool
	if err = database.QueryRowContext(ctx, `SELECT to_regclass('public.review_body_objects') IS NOT NULL`).Scan(&present); err != nil {
		return fmt.Errorf("custody inventory unavailable")
	}
	if !present {
		return nil
	}
	if err = database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM review_body_objects)`).Scan(&present); err != nil {
		return fmt.Errorf("custody inventory unavailable")
	}
	if present {
		return fmt.Errorf("restore_original_review_body_key_required: existing ciphertext")
	}
	return nil
}

func (manager PostgresManager) VerifyReviewBodyRestore(ctx context.Context, credentials DatabaseCredentials, keys reviewbody.KeyRing, now time.Time) error {
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return fmt.Errorf("restored database cannot be opened for custody validation")
	}
	defer database.Close()
	return db.ValidateReviewBodyRestore(ctx, database, keys, now)
}

type PostgresManager struct {
	Runner CommandRunner
}

func LoadDatabaseCredentials(paths Paths) (DatabaseCredentials, error) {
	setRef, err := readAbsoluteSymlink(paths.DatabaseCurrentLink)
	if err != nil {
		return DatabaseCredentials{}, fmt.Errorf("database credential set link is invalid: %w", err)
	}
	if !pathWithin(paths.DatabaseSetsDir, setRef) || setRef == paths.DatabaseSetsDir {
		return DatabaseCredentials{}, fmt.Errorf("database credential set is outside the managed sets directory")
	}
	return LoadDatabaseCredentialSet(paths, setRef)
}

// LoadUpgradeDatabaseCredentials accepts the exact pre-Workspace three-role
// credential layout only when the caller has already proved that the active
// release is an explicitly supported legacy source. A present-but-invalid
// workspace credential never falls back to the legacy layout.
func LoadUpgradeDatabaseCredentials(paths Paths, allowThreeRole bool) (DatabaseCredentials, bool, error) {
	setRef, err := readAbsoluteSymlink(paths.DatabaseCurrentLink)
	if err != nil {
		return DatabaseCredentials{}, false, fmt.Errorf("database credential set link is invalid: %w", err)
	}
	if !pathWithin(paths.DatabaseSetsDir, setRef) || setRef == paths.DatabaseSetsDir {
		return DatabaseCredentials{}, false, fmt.Errorf("database credential set is outside the managed sets directory")
	}
	return LoadUpgradeDatabaseCredentialSet(paths, setRef, allowThreeRole)
}

func LoadUpgradeDatabaseCredentialSet(paths Paths, setRef string, allowThreeRole bool) (DatabaseCredentials, bool, error) {
	if !filepath.IsAbs(setRef) || filepath.Clean(setRef) != setRef || !pathWithin(paths.DatabaseSetsDir, setRef) || setRef == paths.DatabaseSetsDir {
		return DatabaseCredentials{}, false, fmt.Errorf("database credential set path is invalid")
	}
	workspacePath := filepath.Join(setRef, "postgres-workspace.dsn")
	if _, err := os.Lstat(workspacePath); err == nil {
		credentials, loadErr := LoadDatabaseCredentialSet(paths, setRef)
		return credentials, false, loadErr
	} else if !os.IsNotExist(err) {
		return DatabaseCredentials{}, false, fmt.Errorf("workspace database credential cannot be inspected")
	}
	if !allowThreeRole {
		return DatabaseCredentials{}, false, fmt.Errorf("workspace database credential is required")
	}
	credentials, err := LoadLegacyDatabaseCredentialSet(paths, setRef)
	if err != nil {
		return DatabaseCredentials{}, false, err
	}
	return credentials, true, nil
}

func LoadLegacyDatabaseCredentialSet(paths Paths, setRef string) (DatabaseCredentials, error) {
	if !filepath.IsAbs(setRef) || filepath.Clean(setRef) != setRef || !pathWithin(paths.DatabaseSetsDir, setRef) || setRef == paths.DatabaseSetsDir {
		return DatabaseCredentials{}, fmt.Errorf("database credential set path is invalid")
	}
	info, err := os.Lstat(setRef)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return DatabaseCredentials{}, fmt.Errorf("database credential set target is invalid")
	}
	if _, err := os.Lstat(filepath.Join(setRef, "postgres-workspace.dsn")); err == nil || !os.IsNotExist(err) {
		return DatabaseCredentials{}, fmt.Errorf("legacy database credential set must not contain a workspace credential")
	}
	readerDSN, err := config.LoadPostgresDSNFile(filepath.Join(setRef, "postgres-reader.dsn"))
	if err != nil {
		return DatabaseCredentials{}, fmt.Errorf("reader database credential is invalid: %w", err)
	}
	importerDSN, err := config.LoadPostgresDSNFile(filepath.Join(setRef, "postgres-importer.dsn"))
	if err != nil {
		return DatabaseCredentials{}, fmt.Errorf("importer database credential is invalid: %w", err)
	}
	adminDSN, err := config.LoadPostgresDSNFile(filepath.Join(setRef, "postgres-admin.dsn"))
	if err != nil {
		return DatabaseCredentials{}, fmt.Errorf("admin database credential is invalid: %w", err)
	}
	reader, err := pgx.ParseConfig(readerDSN)
	if err != nil {
		return DatabaseCredentials{}, errors.New("reader database credential cannot be parsed")
	}
	importer, err := pgx.ParseConfig(importerDSN)
	if err != nil {
		return DatabaseCredentials{}, errors.New("importer database credential cannot be parsed")
	}
	admin, err := pgx.ParseConfig(adminDSN)
	if err != nil {
		return DatabaseCredentials{}, errors.New("admin database credential cannot be parsed")
	}
	if reader.User != "xinsight_reader" || importer.User != "xinsight_importer" || admin.User != "xinsight_admin" {
		return DatabaseCredentials{}, fmt.Errorf("legacy database credentials must use xinsight_reader, xinsight_importer, and xinsight_admin")
	}
	if !sameDatabaseEndpoint(reader, importer) || !sameDatabaseEndpoint(reader, admin) {
		return DatabaseCredentials{}, fmt.Errorf("database credentials must reference one loopback database")
	}
	if reader.Password == importer.Password || reader.Password == admin.Password || importer.Password == admin.Password {
		return DatabaseCredentials{}, fmt.Errorf("database roles must not share passwords")
	}
	return DatabaseCredentials{
		SetRef: setRef, ReaderDSN: readerDSN, ImporterDSN: importerDSN, AdminDSN: adminDSN,
		Reader: reader, Importer: importer, Admin: admin,
	}, nil
}

func LoadDatabaseCredentialSet(paths Paths, setRef string) (DatabaseCredentials, error) {
	if !filepath.IsAbs(setRef) || filepath.Clean(setRef) != setRef || !pathWithin(paths.DatabaseSetsDir, setRef) || setRef == paths.DatabaseSetsDir {
		return DatabaseCredentials{}, fmt.Errorf("database credential set path is invalid")
	}
	info, err := os.Lstat(setRef)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return DatabaseCredentials{}, fmt.Errorf("database credential set target is invalid")
	}
	readerDSN, err := config.LoadPostgresDSNFile(filepath.Join(setRef, "postgres-reader.dsn"))
	if err != nil {
		return DatabaseCredentials{}, fmt.Errorf("reader database credential is invalid: %w", err)
	}
	workspaceDSN, err := config.LoadPostgresDSNFile(filepath.Join(setRef, "postgres-workspace.dsn"))
	if err != nil {
		return DatabaseCredentials{}, fmt.Errorf("workspace database credential is invalid: %w", err)
	}
	importerDSN, err := config.LoadPostgresDSNFile(filepath.Join(setRef, "postgres-importer.dsn"))
	if err != nil {
		return DatabaseCredentials{}, fmt.Errorf("importer database credential is invalid: %w", err)
	}
	adminDSN, err := config.LoadPostgresDSNFile(filepath.Join(setRef, "postgres-admin.dsn"))
	if err != nil {
		return DatabaseCredentials{}, fmt.Errorf("admin database credential is invalid: %w", err)
	}
	reader, err := pgx.ParseConfig(readerDSN)
	if err != nil {
		return DatabaseCredentials{}, errors.New("reader database credential cannot be parsed")
	}
	workspaceConnection, err := pgx.ParseConfig(workspaceDSN)
	if err != nil {
		return DatabaseCredentials{}, errors.New("workspace database credential cannot be parsed")
	}
	importer, err := pgx.ParseConfig(importerDSN)
	if err != nil {
		return DatabaseCredentials{}, errors.New("importer database credential cannot be parsed")
	}
	admin, err := pgx.ParseConfig(adminDSN)
	if err != nil {
		return DatabaseCredentials{}, errors.New("admin database credential cannot be parsed")
	}
	if reader.User != "xinsight_reader" ||
		workspaceConnection.User != "xinsight_workspace" ||
		importer.User != "xinsight_importer" ||
		admin.User != "xinsight_admin" {
		return DatabaseCredentials{}, fmt.Errorf("database credentials must use xinsight_reader, xinsight_workspace, xinsight_importer, and xinsight_admin")
	}
	if !sameDatabaseEndpoint(reader, workspaceConnection) ||
		!sameDatabaseEndpoint(reader, importer) ||
		!sameDatabaseEndpoint(reader, admin) {
		return DatabaseCredentials{}, fmt.Errorf("database credentials must reference one loopback database")
	}
	passwords := []string{reader.Password, workspaceConnection.Password, importer.Password, admin.Password}
	for left := range passwords {
		for right := left + 1; right < len(passwords); right++ {
			if passwords[left] == passwords[right] {
				return DatabaseCredentials{}, fmt.Errorf("database roles must not share passwords")
			}
		}
	}
	return DatabaseCredentials{
		SetRef: setRef, ReaderDSN: readerDSN, WorkspaceDSN: workspaceDSN,
		ImporterDSN: importerDSN, AdminDSN: adminDSN,
		Reader: reader, Workspace: workspaceConnection, Importer: importer, Admin: admin,
	}, nil
}

// StageWorkspaceDatabaseCredentialSet creates an immutable four-role successor
// to an exact three-role set. It preserves all inherited DSNs byte-for-byte and
// creates only the new workspace credential. Activation remains a separate,
// journaled operation phase.
func StageWorkspaceDatabaseCredentialSet(paths Paths, current DatabaseCredentials, setRef string) (DatabaseCredentials, error) {
	if current.Workspace != nil || current.WorkspaceDSN != "" {
		return DatabaseCredentials{}, fmt.Errorf("workspace database credential bridge requires a three-role source set")
	}
	if filepath.Dir(setRef) != paths.DatabaseSetsDir || !filepath.IsAbs(setRef) || filepath.Clean(setRef) != setRef {
		return DatabaseCredentials{}, fmt.Errorf("workspace database credential set path is invalid")
	}
	if info, err := os.Lstat(setRef); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return DatabaseCredentials{}, fmt.Errorf("workspace database credential set target is invalid")
		}
		credentials, loadErr := LoadDatabaseCredentialSet(paths, setRef)
		if loadErr != nil {
			return DatabaseCredentials{}, loadErr
		}
		if credentials.ReaderDSN != current.ReaderDSN || credentials.ImporterDSN != current.ImporterDSN || credentials.AdminDSN != current.AdminDSN {
			return DatabaseCredentials{}, fmt.Errorf("workspace database credential bridge changed an inherited credential")
		}
		return credentials, nil
	} else if !os.IsNotExist(err) {
		return DatabaseCredentials{}, fmt.Errorf("workspace database credential set cannot be inspected")
	}
	if err := ensureDirectory(paths.DatabaseSetsDir, 0o700); err != nil {
		return DatabaseCredentials{}, err
	}
	suffix, err := randomToken(8)
	if err != nil {
		return DatabaseCredentials{}, err
	}
	stagingRef := setRef + ".staging-" + suffix
	if err := os.Mkdir(stagingRef, 0o700); err != nil {
		return DatabaseCredentials{}, err
	}
	var password string
	for attempt := 0; attempt < 4; attempt++ {
		password, err = randomToken(32)
		if err != nil {
			return DatabaseCredentials{}, err
		}
		if password != current.Reader.Password && password != current.Importer.Password && password != current.Admin.Password {
			break
		}
		password = ""
	}
	if password == "" {
		return DatabaseCredentials{}, fmt.Errorf("unique workspace database credential cannot be generated")
	}
	workspaceURL, err := url.Parse(current.ReaderDSN)
	if err != nil {
		return DatabaseCredentials{}, fmt.Errorf("reader database credential cannot seed workspace credential")
	}
	workspaceURL.User = url.UserPassword("xinsight_workspace", password)
	workspaceDSN, err := config.ValidatePostgresDSN(workspaceURL.String())
	if err != nil {
		return DatabaseCredentials{}, fmt.Errorf("generated workspace database credential is invalid")
	}
	values := []struct {
		name string
		dsn  string
	}{
		{name: "postgres-reader.dsn", dsn: current.ReaderDSN},
		{name: "postgres-workspace.dsn", dsn: workspaceDSN},
		{name: "postgres-importer.dsn", dsn: current.ImporterDSN},
		{name: "postgres-admin.dsn", dsn: current.AdminDSN},
	}
	for _, value := range values {
		file, err := os.OpenFile(filepath.Join(stagingRef, value.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return DatabaseCredentials{}, err
		}
		if err := writeSyncClose(file, []byte(value.dsn+"\n")); err != nil {
			return DatabaseCredentials{}, err
		}
	}
	if err := syncDirectory(stagingRef); err != nil {
		return DatabaseCredentials{}, err
	}
	if err := os.Rename(stagingRef, setRef); err != nil {
		return DatabaseCredentials{}, err
	}
	if err := syncDirectory(paths.DatabaseSetsDir); err != nil {
		return DatabaseCredentials{}, err
	}
	credentials, err := LoadDatabaseCredentialSet(paths, setRef)
	if err != nil {
		return DatabaseCredentials{}, err
	}
	if credentials.ReaderDSN != current.ReaderDSN || credentials.ImporterDSN != current.ImporterDSN || credentials.AdminDSN != current.AdminDSN {
		return DatabaseCredentials{}, fmt.Errorf("workspace database credential bridge changed an inherited credential")
	}
	return credentials, nil
}

func (manager PostgresManager) Inspect(ctx context.Context, credentials DatabaseCredentials) (DatabaseFacts, error) {
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return DatabaseFacts{}, errors.New("admin database connection cannot be opened")
	}
	defer database.Close()
	var facts DatabaseFacts
	var versionNumber int
	if err := database.QueryRowContext(ctx, `
SELECT current_setting('server_version_num')::integer,
       current_database(),
       pg_database_size(current_database()),
       (SELECT count(*) FROM pg_tables WHERE schemaname = 'public'),
       EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'xinsight_reader'),
       EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'xinsight_workspace'),
       EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'xinsight_importer'),
       (SELECT rolsuper FROM pg_roles WHERE rolname = current_user)`).Scan(
		&versionNumber, &facts.DatabaseName, &facts.DatabaseSizeBytes, &facts.PublicTableCount,
		&facts.ReaderRoleExists, &facts.WorkspaceRoleExists,
		&facts.ImporterRoleExists, &facts.AdminRoleIsSuperuser,
	); err != nil {
		return DatabaseFacts{}, errors.New("admin database inspection failed")
	}
	facts.PostgresMajor = versionNumber / 10000
	if err := database.QueryRowContext(ctx, `SELECT system_identifier::text FROM pg_control_system()`).Scan(&facts.SystemIdentifier); err != nil {
		return DatabaseFacts{}, errors.New("database system identity inspection failed")
	}
	ledger, err := db.ReadMigrationLedger(ctx, credentials.AdminDSN)
	if err != nil {
		return DatabaseFacts{}, errors.New("database migration ledger inspection failed")
	}
	facts.MigrationLedger = ledger
	return facts, nil
}

func (manager PostgresManager) EnsureRoles(ctx context.Context, credentials DatabaseCredentials) error {
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return errors.New("admin database connection cannot be opened")
	}
	defer database.Close()
	var currentUser string
	var superuser bool
	if err := database.QueryRowContext(ctx, `SELECT current_user, rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&currentUser, &superuser); err != nil || currentUser != "xinsight_admin" || !superuser {
		return fmt.Errorf("xinsight_admin must be the connected PostgreSQL superuser")
	}
	roles := []databaseRoleSpec{
		{name: "xinsight_reader", password: credentials.Reader.Password, readOnly: true, timeout: "10s", connections: 20},
		{name: "xinsight_importer", password: credentials.Importer.Password, readOnly: false, timeout: "30s", connections: 5},
	}
	if credentials.Workspace != nil {
		roles = append(roles, databaseRoleSpec{
			name: "xinsight_workspace", password: credentials.Workspace.Password,
			readOnly: false, timeout: "10s", connections: 10,
		})
	}
	for _, role := range roles {
		var exists bool
		if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, role.name).Scan(&exists); err != nil {
			return errors.New("database role inspection failed")
		}
		identifier := pgx.Identifier{role.name}.Sanitize()
		password := quotePostgresLiteral(role.password)
		if !exists {
			statement := fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD %s`, identifier, password)
			if _, err := database.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("create restricted database role failed")
			}
		}
		statement := fmt.Sprintf(`ALTER ROLE %s WITH LOGIN PASSWORD %s NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS CONNECTION LIMIT %d`, identifier, password, role.connections)
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("harden restricted database role failed")
		}
		readOnly := "off"
		if role.readOnly {
			readOnly = "on"
		}
		for setting, value := range map[string]string{
			"default_transaction_read_only": readOnly,
			"statement_timeout":             role.timeout,
			"search_path":                   "public",
		} {
			statement := fmt.Sprintf(`ALTER ROLE %s SET %s = %s`, identifier, setting, quotePostgresLiteral(value))
			if _, err := database.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("set restricted database role defaults failed")
			}
		}
	}
	return nil
}

func (manager PostgresManager) ApplyGrants(ctx context.Context, credentials DatabaseCredentials) error {
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return errors.New("admin database connection cannot be opened")
	}
	defer database.Close()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("database grant transaction cannot start")
	}
	defer tx.Rollback()
	if err := verifyConnectorDigestIdentity(ctx, tx); err != nil {
		return err
	}
	insertTables, err := existingPublicTableList(ctx, tx, workspaceInsertTables)
	if err != nil {
		return errors.New("database least-privilege table inspection failed")
	}
	updateTables, err := existingPublicTableList(ctx, tx, workspaceUpdateTables)
	if err != nil {
		return errors.New("database least-privilege table inspection failed")
	}
	importerInsert, err := existingPublicTableList(ctx, tx, importerInsertTables)
	if err != nil {
		return errors.New("database least-privilege table inspection failed")
	}
	importerDelete, err := existingPublicTableList(ctx, tx, importerDeleteTables)
	if err != nil {
		return errors.New("database least-privilege table inspection failed")
	}
	var connectorInvitationRedemptionFunctionExists bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regprocedure($1) IS NOT NULL`, connectorInvitationRedemptionFunction).Scan(&connectorInvitationRedemptionFunctionExists); err != nil {
		return errors.New("database least-privilege function inspection failed")
	}
	var managedWorkInstallFunctionExists, managedWorkInstallTableExists bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regprocedure($1) IS NOT NULL,
to_regclass('public.work_event_install_invitations') IS NOT NULL`, managedWorkInstallIdentityFunction).Scan(
		&managedWorkInstallFunctionExists, &managedWorkInstallTableExists,
	); err != nil || managedWorkInstallFunctionExists != managedWorkInstallTableExists {
		return errors.New("managed work install database boundary inspection failed")
	}
	workspaceColumnRevokes, workspaceColumnGrants, err := existingPublicColumnUpdateStatements(
		ctx,
		tx,
		"xinsight_workspace",
		workspaceUpdateColumnGrants,
	)
	if err != nil {
		return errors.New("database least-privilege column inspection failed")
	}
	importerColumnRevokes, importerColumnGrants, err := existingPublicColumnUpdateStatements(
		ctx,
		tx,
		"xinsight_importer",
		importerUpdateColumnGrants,
	)
	if err != nil {
		return errors.New("database least-privilege column inspection failed")
	}
	statements := []string{
		`REVOKE CREATE ON SCHEMA public FROM PUBLIC`,
		`REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public FROM PUBLIC, xinsight_reader, xinsight_workspace, xinsight_importer`,
		`REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM xinsight_reader`,
		`REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM xinsight_workspace`,
		`REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM xinsight_importer`,
	}
	statements = append(statements, workspaceColumnRevokes...)
	statements = append(statements, importerColumnRevokes...)
	statements = append(statements,
		`GRANT CONNECT ON DATABASE `+pgx.Identifier{credentials.Admin.Database}.Sanitize()+` TO xinsight_reader, xinsight_workspace, xinsight_importer`,
		`GRANT USAGE ON SCHEMA public TO xinsight_reader, xinsight_workspace, xinsight_importer`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA public TO xinsight_reader`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA public TO xinsight_workspace`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA public TO xinsight_importer`,
		`REVOKE INSERT, UPDATE ON TABLE schema_migrations FROM xinsight_workspace`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE xinsight_admin IN SCHEMA public GRANT SELECT ON TABLES TO xinsight_reader`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE xinsight_admin IN SCHEMA public GRANT SELECT ON TABLES TO xinsight_workspace`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE xinsight_admin IN SCHEMA public REVOKE INSERT, UPDATE ON TABLES FROM xinsight_importer`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE xinsight_admin IN SCHEMA public GRANT SELECT ON TABLES TO xinsight_importer`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE xinsight_admin IN SCHEMA public REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC`,
	)
	if managedWorkInstallTableExists {
		statements = append(statements,
			`REVOKE ALL PRIVILEGES ON TABLE public.work_event_install_invitations FROM PUBLIC, xinsight_reader, xinsight_workspace, xinsight_importer`,
		)
	}
	if insertTables != "" {
		statements = append(statements,
			`GRANT INSERT ON TABLE `+insertTables+` TO xinsight_workspace`,
		)
	}
	if updateTables != "" {
		statements = append(statements,
			`GRANT UPDATE ON TABLE `+updateTables+` TO xinsight_workspace`,
		)
	}
	if importerInsert != "" {
		statements = append(statements,
			`GRANT INSERT ON TABLE `+importerInsert+` TO xinsight_importer`,
		)
	}
	if importerDelete != "" {
		statements = append(statements, `GRANT DELETE ON TABLE `+importerDelete+` TO xinsight_importer`)
	}
	for _, fn := range workspaceConnectorFunctions {
		var present bool
		if err := tx.QueryRowContext(ctx, `SELECT to_regprocedure($1) IS NOT NULL`, fn).Scan(&present); err != nil {
			return err
		}
		if present {
			statements = append(statements, `GRANT EXECUTE ON FUNCTION `+fn+` TO xinsight_workspace`)
		}
	}
	if connectorInvitationRedemptionFunctionExists {
		statements = append(statements,
			`GRANT EXECUTE ON FUNCTION `+connectorInvitationRedemptionFunction+` TO xinsight_workspace`,
		)
	}
	if managedWorkInstallFunctionExists {
		statements = append(statements,
			`GRANT EXECUTE ON FUNCTION `+managedWorkInstallIdentityFunction+` TO xinsight_workspace`,
		)
	}
	var resourceIdentityExists bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regprocedure('public.synthetic_resource_cluster_id()') IS NOT NULL`).Scan(&resourceIdentityExists); err != nil {
		return errors.New("resource database identity inspection failed")
	}
	if resourceIdentityExists {
		statements = append(statements, `GRANT EXECUTE ON FUNCTION public.synthetic_resource_cluster_id() TO xinsight_importer`)
	}
	statements = append(statements, workspaceColumnGrants...)
	statements = append(statements, importerColumnGrants...)
	for _, fn := range syncProjectionFunctions {
		var present bool
		if err := tx.QueryRowContext(ctx, `SELECT to_regprocedure($1) IS NOT NULL`, fn).Scan(&present); err != nil {
			return err
		}
		if present {
			statements = append(statements, `GRANT EXECUTE ON FUNCTION `+fn+` TO xinsight_workspace,xinsight_importer`)
		}
	}
	statements = append(statements, `GRANT EXECUTE ON FUNCTION public.digest(text,text) TO xinsight_reader`)
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return errors.New("database least-privilege grants failed")
		}
	}
	return tx.Commit()
}

func existingPublicTableList(ctx context.Context, tx *sql.Tx, candidates []string) (string, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT tablename
FROM pg_tables
WHERE schemaname = 'public' AND tablename = ANY($1)
ORDER BY tablename`, candidates)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	identifiers := make([]string, 0, len(candidates))
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return "", err
		}
		identifiers = append(identifiers, pgx.Identifier{"public", table}.Sanitize())
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.Join(identifiers, ", "), nil
}

func existingPublicColumnUpdateStatements(
	ctx context.Context,
	tx *sql.Tx,
	role string,
	grants []tableColumnGrant,
) ([]string, []string, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT columns.table_name, columns.column_name
FROM information_schema.columns AS columns
JOIN pg_tables AS tables
  ON tables.schemaname = columns.table_schema
 AND tables.tablename = columns.table_name
WHERE columns.table_schema = 'public'
ORDER BY columns.table_name, columns.ordinal_position`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	tableOrder := make([]string, 0)
	columnsByTable := make(map[string][]string)
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			return nil, nil, err
		}
		if _, seen := columnsByTable[table]; !seen {
			tableOrder = append(tableOrder, table)
		}
		columnsByTable[table] = append(columnsByTable[table], column)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	roleIdentifier := pgx.Identifier{role}.Sanitize()
	revokes := make([]string, 0, len(tableOrder))
	for _, table := range tableOrder {
		revokes = append(revokes, fmt.Sprintf(
			`REVOKE UPDATE (%s) ON TABLE %s FROM %s`,
			columnIdentifierList(columnsByTable[table]),
			pgx.Identifier{"public", table}.Sanitize(),
			roleIdentifier,
		))
	}
	grantStatements := make([]string, 0, len(grants))
	for _, grant := range grants {
		existing := make(map[string]struct{}, len(columnsByTable[grant.table]))
		for _, column := range columnsByTable[grant.table] {
			existing[column] = struct{}{}
		}
		columns := make([]string, 0, len(grant.columns))
		for _, column := range grant.columns {
			if _, exists := existing[column]; exists {
				columns = append(columns, column)
			}
		}
		if len(columns) == 0 {
			continue
		}
		grantStatements = append(grantStatements, fmt.Sprintf(
			`GRANT UPDATE (%s) ON TABLE %s TO %s`,
			columnIdentifierList(columns),
			pgx.Identifier{"public", grant.table}.Sanitize(),
			roleIdentifier,
		))
	}
	return revokes, grantStatements, nil
}

func columnIdentifierList(columns []string) string {
	identifiers := make([]string, 0, len(columns))
	for _, column := range columns {
		identifiers = append(identifiers, pgx.Identifier{column}.Sanitize())
	}
	return strings.Join(identifiers, ", ")
}

func allowedColumnValuesSQL(grants []tableColumnGrant) string {
	values := make([]string, 0)
	for _, grant := range grants {
		for _, column := range grant.columns {
			values = append(values, "("+quotePostgresLiteral(grant.table)+", "+quotePostgresLiteral(column)+")")
		}
	}
	return "VALUES " + strings.Join(values, ", ")
}

func (manager PostgresManager) VerifyRoleBoundaries(ctx context.Context, credentials DatabaseCredentials) error {
	if err := manager.verifyRoleBoundaries(ctx, credentials, workspaceUpdateTablesArraySQL, false); err != nil {
		return err
	}
	return verifyConnectorDigestPrivileges(ctx, credentials, true)
}

func (manager PostgresManager) VerifyUpgradeSourceRoleBoundaries(
	ctx context.Context,
	credentials DatabaseCredentials,
	productVersion string,
) error {
	updateTables := workspaceUpdateTablesArraySQL
	if productVersion == "0.3.0-rc.10" {
		updateTables = rc10WorkspaceUpdateTablesArraySQL
	}
	if err := manager.verifyRoleBoundaries(ctx, credentials, updateTables, hasLegacyScopeLabelVersionGrant(productVersion)); err != nil {
		return err
	}
	return verifyConnectorDigestPrivileges(ctx, credentials, false)
}

// These retained development sources used the four-column label policy. This
// only recognizes their role layout; the target manifest still decides whether
// a particular source release is eligible for upgrade.
func hasLegacyScopeLabelVersionGrant(productVersion string) bool {
	switch productVersion {
	case "0.2.0-dev.b2", "0.2.0-dev.d1", "0.2.0-dev.d2",
		"0.2.0-dev.fpv1", "0.2.0-dev.fpv2", "0.2.0-dev.fpv3",
		"0.2.0-dev.fpv4", "0.2.0-dev.fpv5", "0.2.0-dev.fpv6":
		return true
	default:
		return false
	}
}

func (manager PostgresManager) verifyRoleBoundaries(
	ctx context.Context,
	credentials DatabaseCredentials,
	workspaceUpdateTablesSQL string,
	allowLegacyScopeLabelVersion bool,
) error {
	if credentials.Workspace == nil {
		return manager.verifyLegacyRoleBoundaries(ctx, credentials)
	}
	for _, role := range []struct {
		name string
		dsn  string
	}{
		{name: "xinsight_reader", dsn: credentials.ReaderDSN},
		{name: "xinsight_workspace", dsn: credentials.WorkspaceDSN},
		{name: "xinsight_importer", dsn: credentials.ImporterDSN},
	} {
		database, err := sql.Open("pgx", role.dsn)
		if err != nil {
			return fmt.Errorf("restricted database role connection cannot be opened")
		}
		var currentUser, readOnly string
		if err := database.QueryRowContext(ctx, `SELECT current_user, current_setting('default_transaction_read_only')`).Scan(&currentUser, &readOnly); err != nil {
			_ = database.Close()
			return fmt.Errorf("restricted database role cannot connect")
		}
		_ = database.Close()
		expectedReadOnly := "off"
		if role.name == "xinsight_reader" {
			expectedReadOnly = "on"
		}
		if currentUser != role.name || readOnly != expectedReadOnly {
			return fmt.Errorf("restricted database role defaults are invalid")
		}
	}
	return manager.verifyRoleGrantTables(ctx, credentials, workspaceUpdateTablesSQL, allowLegacyScopeLabelVersion)
}

func (manager PostgresManager) verifyLegacyRoleBoundaries(ctx context.Context, credentials DatabaseCredentials) error {
	for _, role := range []struct {
		name string
		dsn  string
	}{
		{name: "xinsight_reader", dsn: credentials.ReaderDSN},
		{name: "xinsight_importer", dsn: credentials.ImporterDSN},
	} {
		database, err := sql.Open("pgx", role.dsn)
		if err != nil {
			return fmt.Errorf("restricted database role connection cannot be opened")
		}
		var currentUser, readOnly string
		if err := database.QueryRowContext(ctx, `SELECT current_user, current_setting('default_transaction_read_only')`).Scan(&currentUser, &readOnly); err != nil {
			_ = database.Close()
			return fmt.Errorf("restricted database role cannot connect")
		}
		_ = database.Close()
		expectedReadOnly := "off"
		if role.name == "xinsight_reader" {
			expectedReadOnly = "on"
		}
		if currentUser != role.name || readOnly != expectedReadOnly {
			return fmt.Errorf("restricted database role defaults are invalid")
		}
	}
	admin, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return errors.New("admin database connection cannot be opened")
	}
	defer admin.Close()
	var readerMissingSelect, readerMutation, importerMissingAccess, importerForbidden int
	if err := admin.QueryRowContext(ctx, `
SELECT
  count(*) FILTER (WHERE NOT has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'SELECT')),
  count(*) FILTER (WHERE has_table_privilege('xinsight_reader', format('%I.%I', schemaname, tablename), 'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')),
  count(*) FILTER (WHERE tablename <> 'schema_migrations' AND NOT has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'SELECT,INSERT,UPDATE')),
  count(*) FILTER (WHERE has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'DELETE,TRUNCATE,REFERENCES,TRIGGER')
                  OR (tablename = 'schema_migrations' AND has_table_privilege('xinsight_importer', format('%I.%I', schemaname, tablename), 'INSERT,UPDATE')))
FROM pg_tables
WHERE schemaname = 'public'`).Scan(&readerMissingSelect, &readerMutation, &importerMissingAccess, &importerForbidden); err != nil {
		return errors.New("database role privilege verification failed")
	}
	if readerMissingSelect != 0 || readerMutation != 0 || importerMissingAccess != 0 || importerForbidden != 0 {
		return fmt.Errorf("database role privilege boundary is invalid")
	}
	return nil
}

func (manager PostgresManager) EvidenceFingerprint(ctx context.Context, credentials DatabaseCredentials) (EvidenceFingerprint, error) {
	database, err := sql.Open("pgx", credentials.ReaderDSN)
	if err != nil {
		return EvidenceFingerprint{}, errors.New("reader database connection cannot be opened")
	}
	defer database.Close()
	var result EvidenceFingerprint
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM evidence_events`).Scan(&result.EventCount); err != nil {
		return EvidenceFingerprint{}, errors.New("evidence count proof failed")
	}
	rows, err := database.QueryContext(ctx, `SELECT id::text, event_hash FROM evidence_events ORDER BY id LIMIT 1000`)
	if err != nil {
		return EvidenceFingerprint{}, errors.New("evidence hash sample proof failed")
	}
	defer rows.Close()
	digest := sha256.New()
	for rows.Next() {
		var id, eventHash string
		if err := rows.Scan(&id, &eventHash); err != nil {
			return EvidenceFingerprint{}, errors.New("evidence hash sample proof failed")
		}
		_, _ = digest.Write([]byte(id))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(eventHash))
		_, _ = digest.Write([]byte{'\n'})
		result.SampleCount++
	}
	if err := rows.Err(); err != nil {
		return EvidenceFingerprint{}, errors.New("evidence hash sample proof failed")
	}
	_, _ = digest.Write([]byte(strconv.FormatInt(result.EventCount, 10)))
	result.SHA256 = "sha256:" + hex.EncodeToString(digest.Sum(nil))
	return result, nil
}

func (manager PostgresManager) DatabaseExists(ctx context.Context, credentials DatabaseCredentials, name string) (bool, error) {
	if !restoreDatabasePattern.MatchString(name) {
		return false, fmt.Errorf("restore database name is invalid")
	}
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return false, errors.New("admin database connection cannot be opened")
	}
	defer database.Close()
	var exists bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&exists); err != nil {
		return false, errors.New("restore target database inspection failed")
	}
	return exists, nil
}

func (manager PostgresManager) AcquireMaintenance(ctx context.Context, credentials DatabaseCredentials) (Maintenance, error) {
	return db.AcquireMaintenanceSession(ctx, credentials.AdminDSN)
}

func (manager PostgresManager) Backup(ctx context.Context, credentials DatabaseCredentials, path string) (BackupResult, error) {
	if manager.Runner == nil {
		return BackupResult{}, fmt.Errorf("database command runner is not configured")
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return BackupResult{}, fmt.Errorf("backup path must be absolute and clean")
	}
	if exists, err := fileExists(path); err != nil || exists {
		if err != nil {
			return BackupResult{}, err
		}
		return BackupResult{}, fmt.Errorf("backup path already exists")
	}
	if err := ensureDirectory(filepath.Dir(path), 0o700); err != nil {
		return BackupResult{}, err
	}
	pgDumpVersion, err := manager.Runner.Run(ctx, Command{Name: "pg_dump", Args: []string{"--version"}})
	if err != nil {
		return BackupResult{}, err
	}
	pgDumpMajor, err := parseMajorVersion(pgDumpVersion.Output, "pg_dump")
	if err != nil || pgDumpMajor != 16 {
		return BackupResult{}, fmt.Errorf("pg_dump 16 is required")
	}
	arguments, environment := postgresCommandConnection(credentials.Admin)
	arguments = append(arguments,
		"--format=custom", "--compress=9", "--no-password", "--file="+path,
	)
	if _, err := manager.Runner.Run(ctx, Command{Name: "pg_dump", Args: arguments, Env: environment}); err != nil {
		return BackupResult{}, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return BackupResult{}, err
	}
	digest, size, err := fileSHA256(path, maxBackupBytes)
	if err != nil || size == 0 {
		return BackupResult{}, fmt.Errorf("backup output is invalid")
	}
	verified, err := manager.VerifyBackup(ctx, path, digest)
	if err != nil {
		return BackupResult{}, err
	}
	verified.PGDumpMajor = pgDumpMajor
	return verified, nil
}

func (manager PostgresManager) VerifyBackup(ctx context.Context, path string, expectedSHA256 string) (BackupResult, error) {
	if manager.Runner == nil {
		return BackupResult{}, fmt.Errorf("database command runner is not configured")
	}
	digest, size, err := fileSHA256(path, maxBackupBytes)
	if err != nil || size == 0 || (expectedSHA256 != "" && digest != expectedSHA256) {
		return BackupResult{}, fmt.Errorf("backup hash verification failed")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		return BackupResult{}, fmt.Errorf("backup must be owner-only")
	}
	version, err := manager.Runner.Run(ctx, Command{Name: "pg_restore", Args: []string{"--version"}})
	if err != nil {
		return BackupResult{}, err
	}
	major, err := parseMajorVersion(version.Output, "pg_restore")
	if err != nil || major != 16 {
		return BackupResult{}, fmt.Errorf("pg_restore 16 is required")
	}
	listing, err := manager.Runner.Run(ctx, Command{Name: "pg_restore", Args: []string{"--list", path}})
	if err != nil || strings.TrimSpace(listing.Output) == "" {
		return BackupResult{}, fmt.Errorf("backup archive structure verification failed")
	}
	return BackupResult{Path: path, SHA256: digest, SizeBytes: size, PGRestoreMajor: major}, nil
}

func (manager PostgresManager) RestoreToNewDatabase(ctx context.Context, current DatabaseCredentials, restored DatabaseCredentials, backupPath string, migrationsDir string, expected []db.MigrationChecksum) error {
	if manager.Runner == nil {
		return fmt.Errorf("database command runner is not configured")
	}
	newDatabase := restored.Admin.Database
	if !restoreDatabasePattern.MatchString(newDatabase) || !sameServer(current.Admin, restored.Admin) || current.Admin.User != restored.Admin.User || current.Admin.Password != restored.Admin.Password {
		return fmt.Errorf("restore target database credential is invalid")
	}
	database, err := sql.Open("pgx", current.AdminDSN)
	if err != nil {
		return errors.New("admin database connection cannot be opened")
	}
	defer database.Close()
	var exists bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, newDatabase).Scan(&exists); err != nil {
		return errors.New("restore target database inspection failed")
	}
	if exists {
		return fmt.Errorf("restore target database already exists")
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
	environment := map[string]string{"PGPASSWORD": current.Admin.Password}
	if _, err := manager.Runner.Run(ctx, Command{Name: "createdb", Args: arguments, Env: environment}); err != nil {
		return err
	}
	restoreArgs, restoreEnvironment := postgresCommandConnection(restored.Admin)
	restoreArgs = append(restoreArgs, "--no-password", "--exit-on-error", "--single-transaction", "--no-owner", "--no-privileges", backupPath)
	if _, err := manager.Runner.Run(ctx, Command{Name: "pg_restore", Args: restoreArgs, Env: restoreEnvironment}); err != nil {
		return err
	}
	if _, err := db.RunMigrations(ctx, restored.AdminDSN, migrationsDir); err != nil {
		return fmt.Errorf("restored database forward migration failed: %w", err)
	}
	if err := manager.ApplyGrants(ctx, restored); err != nil {
		return err
	}
	store, err := db.OpenEvidenceStore(ctx, restored.AdminDSN)
	if err != nil {
		return errors.New("restored database cannot be opened")
	}
	readiness, readinessErr := store.Readiness(ctx, expected)
	closeErr := store.Close()
	if readinessErr != nil {
		return fmt.Errorf("restored database compatibility verification failed: %w", readinessErr)
	}
	if closeErr != nil {
		return fmt.Errorf("restored database compatibility verification failed: close restored database: %w", closeErr)
	}
	if readiness.PostgresMajor != 16 {
		return fmt.Errorf("restored database compatibility verification failed: PostgreSQL 16 is required")
	}
	restoredDB, err := sql.Open("pgx", restored.AdminDSN)
	if err != nil {
		return errors.New("restored database cannot be opened for binding validation")
	}
	defer restoredDB.Close()
	if err := db.ValidateWorkbenchRestore(ctx, restoredDB); err != nil {
		return err
	}
	if err := db.ValidateFindingResponsibilityRestore(ctx, restoredDB); err != nil {
		return err
	}
	if err := db.ValidateFindingAssessmentRestore(ctx, restoredDB); err != nil {
		return err
	}
	if err := db.ValidateInvestigationQueueRestore(ctx, restoredDB); err != nil {
		return err
	}
	return db.SuspendRestoredSourceFacts(ctx, restoredDB)
}

func StageRestoredCredentialSet(paths Paths, current DatabaseCredentials, newDatabase string) (DatabaseCredentials, error) {
	if !restoreDatabasePattern.MatchString(newDatabase) {
		return DatabaseCredentials{}, fmt.Errorf("restore database name is invalid")
	}
	return stageIsolatedDatabaseCredentialSet(paths, current, newDatabase)
}

func StageCleanBaselineCredentialSet(paths Paths, current DatabaseCredentials, newDatabase string) (DatabaseCredentials, error) {
	if !cleanBaselineDatabasePattern.MatchString(newDatabase) {
		return DatabaseCredentials{}, fmt.Errorf("clean-baseline database name is invalid")
	}
	return stageIsolatedDatabaseCredentialSet(paths, current, newDatabase)
}

func stageIsolatedDatabaseCredentialSet(paths Paths, current DatabaseCredentials, newDatabase string) (DatabaseCredentials, error) {
	token, err := randomToken(8)
	if err != nil {
		return DatabaseCredentials{}, err
	}
	setRef := filepath.Join(paths.DatabaseSetsDir, newDatabase+"-"+token)
	if err := ensureDirectory(paths.DatabaseSetsDir, 0o700); err != nil {
		return DatabaseCredentials{}, err
	}
	if err := os.Mkdir(setRef, 0o700); err != nil {
		return DatabaseCredentials{}, err
	}
	values := []struct {
		name string
		dsn  string
	}{
		{name: "postgres-reader.dsn", dsn: current.ReaderDSN},
		{name: "postgres-workspace.dsn", dsn: current.WorkspaceDSN},
		{name: "postgres-importer.dsn", dsn: current.ImporterDSN},
		{name: "postgres-admin.dsn", dsn: current.AdminDSN},
	}
	for _, value := range values {
		rewritten, err := rewriteDatabaseDSN(value.dsn, newDatabase)
		if err != nil {
			return DatabaseCredentials{}, err
		}
		file, err := os.OpenFile(filepath.Join(setRef, value.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return DatabaseCredentials{}, err
		}
		if err := writeSyncClose(file, []byte(rewritten+"\n")); err != nil {
			return DatabaseCredentials{}, err
		}
	}
	if err := syncDirectory(setRef); err != nil {
		return DatabaseCredentials{}, err
	}
	reader, _ := rewriteDatabaseDSN(current.ReaderDSN, newDatabase)
	workspaceDSN, _ := rewriteDatabaseDSN(current.WorkspaceDSN, newDatabase)
	importer, _ := rewriteDatabaseDSN(current.ImporterDSN, newDatabase)
	admin, _ := rewriteDatabaseDSN(current.AdminDSN, newDatabase)
	readerConfig, _ := pgx.ParseConfig(reader)
	workspaceConfig, _ := pgx.ParseConfig(workspaceDSN)
	importerConfig, _ := pgx.ParseConfig(importer)
	adminConfig, _ := pgx.ParseConfig(admin)
	return DatabaseCredentials{
		SetRef: setRef, ReaderDSN: reader, WorkspaceDSN: workspaceDSN,
		ImporterDSN: importer, AdminDSN: admin,
		Reader: readerConfig, Workspace: workspaceConfig,
		Importer: importerConfig, Admin: adminConfig,
	}, nil
}

func sameDatabaseEndpoint(left *pgx.ConnConfig, right *pgx.ConnConfig) bool {
	return sameServer(left, right) && left.Database == right.Database
}

func sameServer(left *pgx.ConnConfig, right *pgx.ConnConfig) bool {
	return strings.EqualFold(left.Host, right.Host) && left.Port == right.Port
}

func pathWithin(root string, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func quotePostgresLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func postgresCommandConnection(connection *pgx.ConnConfig) ([]string, map[string]string) {
	arguments := []string{
		"--host=" + connection.Host,
		"--port=" + strconv.Itoa(int(connection.Port)),
		"--username=" + connection.User,
		"--dbname=" + connection.Database,
	}
	return arguments, map[string]string{"PGPASSWORD": connection.Password}
}

func rewriteDatabaseDSN(value string, database string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", errors.New("database credential cannot be rewritten")
	}
	parsed.Path = "/" + database
	parsed.RawPath = ""
	rewritten := parsed.String()
	if _, err := config.ValidatePostgresDSN(rewritten); err != nil {
		return "", errors.New("rewritten database credential is invalid")
	}
	return rewritten, nil
}

func backupPath(paths Paths, operationID string, now time.Time) string {
	return filepath.Join(paths.BackupsDir, now.UTC().Format("20060102T150405Z")+"-"+operationID+".dump")
}

func (manager PostgresManager) SuspendForeignRestoredAuthority(ctx context.Context, c DatabaseCredentials, source, target string, now time.Time) error {
	database, err := sql.Open("pgx", c.AdminDSN)
	if err != nil {
		return fmt.Errorf("restored authority database unavailable")
	}
	defer database.Close()
	return db.SuspendForeignRestoredAuthority(ctx, database, source, target, now)
}

// ImporterRoleLogin observes the existing maintenance gate without changing it.
func (manager PostgresManager) ImporterRoleLogin(ctx context.Context, credentials DatabaseCredentials) (bool, error) {
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return false, errors.New("admin database connection cannot be opened")
	}
	defer database.Close()
	var enabled bool
	if err = database.QueryRowContext(ctx, `SELECT rolcanlogin FROM pg_roles WHERE rolname='xinsight_importer'`).Scan(&enabled); err != nil {
		return false, errors.New("importer login inspection failed")
	}
	return enabled, nil
}
