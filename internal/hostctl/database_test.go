package hostctl

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestUpgradeCredentialLoaderRequiresExplicitThreeRoleSource(t *testing.T) {
	root := t.TempDir()
	t.Setenv(AllowTestRootEnv, "1")
	t.Setenv(TestRootEnv, root)
	paths, err := PathsFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	setRef := stageLegacyThreeRoleCredentialSet(t, paths, "legacy-rc15")
	if err := atomicSymlink(setRef, paths.DatabaseCurrentLink); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadUpgradeDatabaseCredentials(paths, false); err == nil || !strings.Contains(err.Error(), "workspace database credential is required") {
		t.Fatalf("expected fail-closed workspace requirement, got %v", err)
	}
	credentials, bridge, err := LoadUpgradeDatabaseCredentials(paths, true)
	if err != nil || !bridge || credentials.Workspace != nil || credentials.SetRef != setRef {
		t.Fatalf("legacy credential structure invalid bridge=%t err=%v", bridge, err)
	}
	writeHostFile(t, filepath.Join(setRef, "postgres-workspace.dsn"), []byte("invalid\n"), 0o600)
	if _, bridge, err := LoadUpgradeDatabaseCredentials(paths, true); err == nil || bridge {
		t.Fatalf("present invalid workspace credential fell back to legacy bridge=%t err=%v", bridge, err)
	}
}

func TestStageWorkspaceDatabaseCredentialSetPreservesInheritedCredentials(t *testing.T) {
	root := t.TempDir()
	t.Setenv(AllowTestRootEnv, "1")
	t.Setenv(TestRootEnv, root)
	paths, err := PathsFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	legacyRef := stageLegacyThreeRoleCredentialSet(t, paths, "legacy-rc15")
	if err := atomicSymlink(legacyRef, paths.DatabaseCurrentLink); err != nil {
		t.Fatal(err)
	}
	legacy, bridge, err := LoadUpgradeDatabaseCredentials(paths, true)
	if err != nil || !bridge {
		t.Fatalf("load legacy bridge=%t err=%v", bridge, err)
	}
	nextRef := filepath.Join(paths.DatabaseSetsDir, "workspace-bridge-test")
	expanded, err := StageWorkspaceDatabaseCredentialSet(paths, legacy, nextRef)
	if err != nil {
		t.Fatal(err)
	}
	if expanded.SetRef != nextRef || expanded.Workspace == nil || expanded.Workspace.User != "xinsight_workspace" {
		t.Fatal("expanded credential structure is invalid")
	}
	if expanded.ReaderDSN != legacy.ReaderDSN || expanded.ImporterDSN != legacy.ImporterDSN || expanded.AdminDSN != legacy.AdminDSN {
		t.Fatal("inherited credential changed")
	}
	if expanded.Workspace.Password == legacy.Reader.Password || expanded.Workspace.Password == legacy.Importer.Password || expanded.Workspace.Password == legacy.Admin.Password {
		t.Fatal("workspace password is not unique")
	}
	active, err := readAbsoluteSymlink(paths.DatabaseCurrentLink)
	if err != nil || active != legacyRef {
		t.Fatalf("staging activated credentials active=%q err=%v", active, err)
	}
	firstWorkspaceDSN := expanded.WorkspaceDSN
	reloaded, err := StageWorkspaceDatabaseCredentialSet(paths, legacy, nextRef)
	if err != nil || reloaded.WorkspaceDSN != firstWorkspaceDSN {
		t.Fatalf("idempotent stage changed workspace credential err=%v", err)
	}
	for _, name := range []string{"postgres-reader.dsn", "postgres-workspace.dsn", "postgres-importer.dsn", "postgres-admin.dsn"} {
		info, err := os.Lstat(filepath.Join(nextRef, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("credential %s mode=%v err=%v", name, infoMode(info), err)
		}
	}
	info, err := os.Lstat(nextRef)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("credential set mode=%v err=%v", infoMode(info), err)
	}
}

func stageLegacyThreeRoleCredentialSet(t *testing.T, paths Paths, name string) string {
	t.Helper()
	setRef := filepath.Join(paths.DatabaseSetsDir, name)
	if err := os.MkdirAll(setRef, 0o700); err != nil {
		t.Fatal(err)
	}
	for file, value := range map[string]string{
		"postgres-reader.dsn":   "postgres://xinsight_reader:reader-password@127.0.0.1:5432/xinsight?sslmode=disable",
		"postgres-importer.dsn": "postgres://xinsight_importer:importer-password@127.0.0.1:5432/xinsight?sslmode=disable",
		"postgres-admin.dsn":    "postgres://xinsight_admin:admin-password@127.0.0.1:5432/xinsight?sslmode=disable",
	} {
		writeHostFile(t, filepath.Join(setRef, file), []byte(value+"\n"), 0o600)
	}
	return setRef
}

func TestWorkspacePrivilegeTableSetsPreserveCreationAndEvidenceBoundaries(t *testing.T) {
	assertUniqueTableSet(t, "insert", workspaceInsertTables)
	assertUniqueTableSet(t, "update", workspaceUpdateTables)
	assertUniqueColumnGrants(t, "workspace update", workspaceUpdateColumnGrants)
	assertUniqueTableSet(t, "importer insert", importerInsertTables)
	assertUniqueColumnGrants(t, "importer update", importerUpdateColumnGrants)
	for _, forbidden := range []string{
		"evidence_events",
		"capture_gaps",
		"import_provenance",
		"investigation_alerts",
		"connector_checkpoint_epoch_operations",
		"connector_checkpoint_epoch_history",
		"service_identities",
		"service_identity_tokens",
	} {
		if containsTable(workspaceInsertTables, forbidden) {
			t.Fatalf("workspace INSERT unexpectedly includes %q", forbidden)
		}
	}
	if containsTable(workspaceUpdateTables, "investigation_alerts") {
		t.Fatal("workspace UPDATE unexpectedly includes source-owned investigation_alerts")
	}
	for _, authorityTable := range []string{"connector_installations", "connector_installation_invitations"} {
		if containsTable(workspaceInsertTables, authorityTable) {
			t.Fatalf("workspace INSERT unexpectedly includes connector authority table %q", authorityTable)
		}
		if containsTable(workspaceUpdateTables, authorityTable) {
			t.Fatalf("workspace table UPDATE unexpectedly includes connector authority table %q", authorityTable)
		}
	}
	for _, operationalColumn := range []string{
		"source_instance", "source_instance_sha256", "status", "observed_state",
		"status_reason_code", "last_agent_seen_at", "updated_at", "version",
	} {
		if !containsColumnGrant(workspaceUpdateColumnGrants, "connector_installations", operationalColumn) {
			t.Fatalf("workspace operational UPDATE is missing connector_installations.%s", operationalColumn)
		}
	}
	for _, authorityColumn := range []string{
		"project_id", "final_event_scope", "control_service_identity_id",
		"installation_acknowledgement_sha256",
	} {
		if containsColumnGrant(workspaceUpdateColumnGrants, "connector_installations", authorityColumn) {
			t.Fatalf("workspace UPDATE unexpectedly includes connector authority column %q", authorityColumn)
		}
	}
	for table, expectedColumns := range map[string][]string{
		"service_identities":      {"status", "updated_at", "revoked_at", "revoke_reason", "last_used_at"},
		"service_identity_tokens": {"revoked_at", "last_used_at"},
	} {
		if containsTable(workspaceUpdateTables, table) {
			t.Fatalf("workspace table UPDATE unexpectedly includes %q", table)
		}
		if columns := updateColumnsFor(workspaceUpdateColumnGrants, table); !slices.Equal(columns, expectedColumns) {
			t.Fatalf("workspace UPDATE columns for %s=%v expected=%v", table, columns, expectedColumns)
		}
	}
	for table, forbiddenColumns := range map[string][]string{
		"service_identities": {
			"permissions", "created_by_subject", "expires_at", "created_at", "display_name",
		},
		"service_identity_tokens": {
			"token_digest", "service_identity_id", "token_prefix", "created_at", "expires_at",
		},
	} {
		for _, column := range forbiddenColumns {
			if containsColumnGrant(workspaceUpdateColumnGrants, table, column) {
				t.Fatalf("workspace UPDATE unexpectedly includes %s.%s", table, column)
			}
		}
	}
	for _, expected := range []string{
		"investigation_cases",
		"access_audit_records",
		"investigation_alert_case_links",
		"discovery_analysis_runs",
		"discovery_hypotheses",
	} {
		if !containsTable(workspaceInsertTables, expected) {
			t.Fatalf("workspace INSERT is missing %q", expected)
		}
	}
	if containsTable(workspaceInsertTables, "connector_control_operations") {
		t.Fatal("workspace INSERT must not bypass the root-local connector phase controller")
	}
	for _, expected := range []string{
		"adapter_import_states",
		"source_systems",
		"adapter_manifests",
		"import_provenance",
		"import_provenance_events",
		"evidence_events",
		"capture_gaps",
		"connector_import_operation_bindings",
		"connector_operational_receipts",
	} {
		if !containsTable(importerInsertTables, expected) {
			t.Fatalf("importer INSERT allowlist is missing %q", expected)
		}
	}
	for table, expectedColumns := range map[string][]string{
		"adapter_import_states": {"cursor_value", "worker_status", "last_error", "updated_at"},
		"source_systems":        {"source_version", "status"},
		"adapter_manifests":     {"adapter_version", "active"},
		"evidence_events":       {"import_provenance_id"},
	} {
		if !containsTable(importerInsertTables, table) {
			t.Fatalf("importer UPDATE table %q must also be INSERT-authorized", table)
		}
		for _, column := range expectedColumns {
			if !containsColumnGrant(importerUpdateColumnGrants, table, column) {
				t.Fatalf("importer UPDATE allowlist is missing %s.%s", table, column)
			}
		}
	}
	if columns := updateColumnsFor(importerUpdateColumnGrants, "evidence_events"); len(columns) != 1 || columns[0] != "import_provenance_id" {
		t.Fatalf("Evidence UPDATE must be provenance-link-only, got %v", columns)
	}
	if columns := updateColumnsFor(importerUpdateColumnGrants, "connector_operational_receipts"); len(columns) != 0 {
		t.Fatalf("completed operational receipts must be immutable, got UPDATE columns %v", columns)
	}
	if columns := updateColumnsFor(importerUpdateColumnGrants, "connector_import_operation_bindings"); len(columns) != 0 {
		t.Fatalf("import operation bindings must be immutable, got UPDATE columns %v", columns)
	}
	for _, forbidden := range []string{
		"investigation_cases",
		"investigation_alerts",
		"authoritative_sessions",
		"evaluation_feedback",
		"execution_version_dimensions",
		"payload_policies",
		"connector_checkpoint_epoch_operations",
		"connector_checkpoint_epoch_history",
	} {
		if containsTable(importerInsertTables, forbidden) || len(updateColumnsFor(importerUpdateColumnGrants, forbidden)) != 0 {
			t.Fatalf("importer mutation allowlist unexpectedly includes %q", forbidden)
		}
	}
}

func TestConnectorInvitationRedemptionUsesOnlyAtomicDatabaseFunction(t *testing.T) {
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000021_create_connector_installations.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	definition := string(migration)
	for _, required := range []string{
		"CREATE FUNCTION public.xinsight_redeem_connector_invitation(",
		"SECURITY DEFINER",
		"SET search_path = pg_catalog",
		"p_control_token_expires_at > v_now + interval '365 days'",
		"ARRAY['connector.control.poll', 'connector.control.ack']::text[]",
		"UPDATE public.connector_installations",
		"UPDATE public.connector_installation_invitations",
		"INSERT INTO public.connector_control_operations",
		"INSERT INTO public.access_audit_records",
		"REVOKE ALL PRIVILEGES ON FUNCTION public.xinsight_redeem_connector_invitation(",
		"CREATE FUNCTION public.xinsight_guard_service_credential_terminal_update()",
		"CREATE TRIGGER service_identities_terminal_update_guard",
		"CREATE TRIGGER service_identity_tokens_terminal_update_guard",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("connector identity function lost boundary %q", required)
		}
	}
	phaseMigration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000022_add_connector_control_phases.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	phaseDefinition := string(phaseMigration)
	for _, required := range []string{
		"CREATE OR REPLACE FUNCTION public.xinsight_redeem_connector_invitation(",
		`'"phase":"prepare_paused_source"`,
		"'prepare_paused_source', 'active'",
		"AND (operation.phase IS NULL OR operation.phase = 'prepare_paused_source')",
		"CREATE TRIGGER connector_control_operations_phase_guard",
	} {
		if !strings.Contains(phaseDefinition, required) {
			t.Fatalf("connector phase migration lost boundary %q", required)
		}
	}
	store, err := os.ReadFile(filepath.Join("..", "db", "connector_control_store.go"))
	if err != nil {
		t.Fatal(err)
	}
	storeSource := string(store)
	if !strings.Contains(storeSource, "FROM public.xinsight_redeem_connector_invitation(") ||
		strings.Contains(storeSource, "INSERT INTO service_identities") ||
		strings.Contains(storeSource, "INSERT INTO service_identity_tokens") ||
		strings.Contains(storeSource, "UPDATE connector_installation_invitations") {
		t.Fatal("workspace connector store bypasses atomic invitation redemption")
	}
}

func TestApplyGrantsConvergesFunctionPrivilegesAfterNoACLRestore(t *testing.T) {
	store, err := os.ReadFile("database.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(store)
	for _, required := range []string{
		"REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public FROM PUBLIC, xinsight_reader, xinsight_workspace, xinsight_importer",
		"ALTER DEFAULT PRIVILEGES FOR ROLE xinsight_admin IN SCHEMA public REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC",
		"GRANT EXECUTE ON FUNCTION `+connectorInvitationRedemptionFunction+` TO xinsight_workspace",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("database privilege convergence lost %q", required)
		}
	}
}

func TestCurrentRoleBoundarySQLIsDerivedFromAuthoritativeLists(t *testing.T) {
	if workspaceInsertTablesArraySQL != textArraySQL(workspaceInsertTables) ||
		workspaceUpdateTablesArraySQL != textArraySQL(workspaceUpdateTables) ||
		importerInsertTablesArraySQL != textArraySQL(importerInsertTables) {
		t.Fatal("current role-boundary SQL diverged from its authoritative Go lists")
	}
	for _, table := range []string{"workbench_readings", "workbench_reading_members", "workbench_saved_filters", "workbench_saved_filter_operations", "case_finding_actions", "case_finding_followups", "case_finding_assessments", "case_finding_assessment_actions", "finding_review_creations"} {
		if !strings.Contains(workspaceInsertTablesArraySQL, "'"+table+"'") {
			t.Fatalf("workspace INSERT boundary lost %s", table)
		}
	}
	if !strings.Contains(importerInsertTablesArraySQL, "'workbench_economic_facts'") {
		t.Fatal("importer INSERT boundary lost workbench_economic_facts")
	}
	if !strings.Contains(workspaceUpdateTablesArraySQL, "'case_finding_followups'") || strings.Contains(workspaceUpdateTablesArraySQL, "'case_finding_actions'") || strings.Contains(rc10WorkspaceUpdateTablesArraySQL, "case_finding_") {
		t.Fatal("responsibility current/history or historical role boundary changed incorrectly")
	}
	if !strings.Contains(workspaceUpdateTablesArraySQL, "'case_finding_assessments'") || strings.Contains(workspaceUpdateTablesArraySQL, "'case_finding_assessment_actions'") || strings.Contains(workspaceUpdateTablesArraySQL, "'finding_review_creations'") {
		t.Fatal("assessment current/history grants are not separated")
	}
	if !strings.Contains(rc10WorkspaceUpdateTablesArraySQL, "'service_identities'") ||
		strings.Contains(rc10WorkspaceUpdateTablesArraySQL, "workbench_") {
		t.Fatal("historical rc.10 role-boundary constant was rewritten")
	}
}

func assertUniqueColumnGrants(t *testing.T, label string, grants []tableColumnGrant) {
	t.Helper()
	tables := make(map[string]struct{}, len(grants))
	for _, grant := range grants {
		if grant.table == "" || len(grant.columns) == 0 {
			t.Fatalf("%s contains an empty table or column set", label)
		}
		if _, duplicate := tables[grant.table]; duplicate {
			t.Fatalf("%s duplicates table %q", label, grant.table)
		}
		tables[grant.table] = struct{}{}
		columns := make(map[string]struct{}, len(grant.columns))
		for _, column := range grant.columns {
			if column == "" {
				t.Fatalf("%s table %q contains an empty column", label, grant.table)
			}
			if _, duplicate := columns[column]; duplicate {
				t.Fatalf("%s table %q duplicates column %q", label, grant.table, column)
			}
			columns[column] = struct{}{}
		}
	}
}

func updateColumnsFor(grants []tableColumnGrant, table string) []string {
	for _, grant := range grants {
		if grant.table == table {
			return grant.columns
		}
	}
	return nil
}

func containsColumnGrant(grants []tableColumnGrant, table string, column string) bool {
	for _, candidate := range updateColumnsFor(grants, table) {
		if candidate == column {
			return true
		}
	}
	return false
}

func assertUniqueTableSet(t *testing.T, label string, tables []string) {
	t.Helper()
	seen := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		if table == "" {
			t.Fatalf("%s table set contains an empty name", label)
		}
		if _, duplicate := seen[table]; duplicate {
			t.Fatalf("%s table set duplicates %q", label, table)
		}
		seen[table] = struct{}{}
	}
}

func containsTable(tables []string, target string) bool {
	for _, table := range tables {
		if table == target {
			return true
		}
	}
	return false
}
