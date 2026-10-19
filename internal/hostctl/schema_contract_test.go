package hostctl

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSchemaContractComparisonIgnoresPhysicalLayoutOnly(t *testing.T) {
	expected := SchemaContractManifest{
		SchemaVersion:  CleanBaselineSchemaContractVersion,
		DatabaseSchema: "20", PostgresMajor: 16, MigrationCount: 20,
		MigrationSetSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SemanticSHA256:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ColumnLayoutSHA256: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Entries: []SchemaContractObject{{
			Kind: "C", Namespace: "public", Owner: "schema_migrations", Name: "dirty",
			SemanticSHA256: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		}},
	}
	actual := expected
	actual.ColumnLayoutSHA256 = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	if mismatches := compareSchemaContracts(expected, actual); len(mismatches) != 0 {
		t.Fatalf("physical layout changed authorization: %+v", mismatches)
	}
	actual.Entries = append([]SchemaContractObject(nil), expected.Entries...)
	actual.Entries[0].SemanticSHA256 = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	mismatches := compareSchemaContracts(expected, actual)
	if len(mismatches) != 1 || mismatches[0].Reason != "semantic_property_drift" ||
		mismatches[0].Namespace != "public" || mismatches[0].Owner != "schema_migrations" ||
		mismatches[0].Name != "dirty" {
		t.Fatalf("semantic drift diagnostics=%+v", mismatches)
	}
}

func TestSchemaContractComparisonRejectsDuplicateObjectIdentity(t *testing.T) {
	base := SchemaContractManifest{
		SchemaVersion:  CleanBaselineSchemaContractVersion,
		DatabaseSchema: "20", PostgresMajor: 16, MigrationCount: 20,
		MigrationSetSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SemanticSHA256:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ColumnLayoutSHA256: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Entries: []SchemaContractObject{{
			Kind: "C", Namespace: "public", Owner: "schema_migrations", Name: "dirty",
			SemanticSHA256: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		}},
	}
	for _, contractSide := range []string{"expected", "actual"} {
		t.Run(contractSide, func(t *testing.T) {
			expected := base
			expected.Entries = append([]SchemaContractObject(nil), base.Entries...)
			actual := base
			actual.Entries = append([]SchemaContractObject(nil), base.Entries...)
			if contractSide == "expected" {
				expected.Entries = append(expected.Entries, expected.Entries[0])
			} else {
				actual.Entries = append(actual.Entries, actual.Entries[0])
			}
			mismatches := compareSchemaContracts(expected, actual)
			if len(mismatches) != 1 || mismatches[0].Reason != "duplicate_object_identity" ||
				mismatches[0].ContractSide != contractSide || mismatches[0].Kind != "C" ||
				mismatches[0].Namespace != "public" || mismatches[0].Owner != "schema_migrations" ||
				mismatches[0].Name != "dirty" {
				t.Fatalf("duplicate %s identity yielded valid or ambiguous result: %+v", contractSide, mismatches)
			}
		})
	}
}

func TestGeneratedSchemaContractIsSortedAndSelfConsistent(t *testing.T) {
	manifest := ExpectedCleanBaselineSchemaContract()
	if manifest.SchemaVersion != CleanBaselineSchemaContractVersion ||
		manifest.DatabaseSchema != "46" || manifest.PostgresMajor != 16 ||
		manifest.MigrationCount != 46 || manifest.SemanticSHA256 != expectedCleanBaselineSchemaContract.SemanticSHA256 ||
		manifest.Objects.Tables != len(cleanBaselineTables) || len(manifest.Entries) == 0 {
		t.Fatalf("generated contract identity: version=%s schema=%s postgres=%d migrations=%d tables=%d entries=%d semantic=%s", manifest.SchemaVersion, manifest.DatabaseSchema, manifest.PostgresMajor, manifest.MigrationCount, manifest.Objects.Tables, len(manifest.Entries), manifest.SemanticSHA256)
	}
	for index := 1; index < len(manifest.Entries); index++ {
		if schemaContractObjectKey(manifest.Entries[index-1]) >= schemaContractObjectKey(manifest.Entries[index]) {
			t.Fatalf("generated contract entries are not strictly sorted at %d", index)
		}
	}
	manifest.Entries[0].SemanticSHA256 = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if ExpectedCleanBaselineSchemaContract().Entries[0].SemanticSHA256 == manifest.Entries[0].SemanticSHA256 {
		t.Fatal("expected schema contract readback returned mutable generated state")
	}
}

func TestHistoricalRC13SchemaContractRemainsSchema23(t *testing.T) {
	payload, err := os.ReadFile("clean_baseline_schema_contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var historical SchemaContractManifest
	if err := json.Unmarshal(payload, &historical); err != nil {
		t.Fatal(err)
	}
	if historical.DatabaseSchema != "23" || historical.MigrationCount != 23 {
		t.Fatalf("historical rc.13 contract identity changed: schema=%s migrations=%d", historical.DatabaseSchema, historical.MigrationCount)
	}
	if expectedCleanBaselineSchemaContract.DatabaseSchema != "46" ||
		expectedCleanBaselineSchemaContract.MigrationCount != 46 {
		t.Fatalf("development contract identity: schema=%s migrations=%d", expectedCleanBaselineSchemaContract.DatabaseSchema, expectedCleanBaselineSchemaContract.MigrationCount)
	}
}

func TestSchemaContractProtectsCompleteFunctionSemantics(t *testing.T) {
	for _, property := range []string{
		"pg_get_userbyid(p.proowner)",
		"pg_get_function_result(p.oid)",
		"pg_get_function_arguments(p.oid)",
		"pg_get_expr(p.proargdefaults, 0, true)",
		"p.proisstrict",
		"p.proparallel",
		"p.procost",
		"p.prorows",
		"unexpected_acl",
		"aclexplode",
		"oidvectortypes(p.proargtypes)",
	} {
		if !strings.Contains(cleanBaselineSchemaQuery, property) {
			t.Fatalf("function schema contract does not protect %q", property)
		}
	}
}

func TestSchemaContractGenerationRequiresConvergedFunctionPrivileges(t *testing.T) {
	for _, boundary := range []string{
		"aclexplode(COALESCE(p.proacl, acldefault('f', p.proowner)))",
		"grantee = 'xinsight_workspace'",
		"privilege_type = 'EXECUTE'",
		"count(*) FILTER (WHERE NOT managed)",
		"count(*) FILTER (WHERE managed)",
	} {
		if !strings.Contains(cleanBaselineFunctionPrivilegeQuery, boundary) {
			t.Fatalf("schema contract generation privilege gate lost %q", boundary)
		}
	}
}

func TestSchema23HardensFreshFunctionExecutePrivilegesWithoutRewritingHistory(t *testing.T) {
	up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000023_harden_function_execute_privileges.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000023_harden_function_execute_privileges.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public FROM PUBLIC",
		"ALTER DEFAULT PRIVILEGES FOR ROLE xinsight_admin IN SCHEMA public",
		"REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC",
	} {
		if !strings.Contains(string(up), required) {
			t.Fatalf("schema23 up migration lost %q", required)
		}
	}
	if !strings.Contains(string(down), "GRANT EXECUTE ON FUNCTION public.reject_connector_checkpoint_epoch_history_mutation()") {
		t.Fatal("schema23 down migration does not restore the only historical PUBLIC function grant")
	}
}

func TestMigrationLedgerSQLDoesNotDependOnPhysicalColumnOrder(t *testing.T) {
	path := filepath.Join("..", "db", "migrate.go")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ToLower(string(payload))
	if strings.Contains(source, "select *") {
		t.Fatal("migration ledger uses SELECT *")
	}
	for _, statement := range []string{
		"select version, %s, %s, applied_at\nfrom schema_migrations",
		"insert into schema_migrations (version, checksum_sha256, dirty)",
		"where version = $1 and checksum_sha256 = $2 and dirty = true",
	} {
		if !strings.Contains(source, statement) {
			t.Fatalf("migration ledger lost explicit-column SQL: %q", statement)
		}
	}
}

func TestProductSQLDoesNotDependOnPhysicalColumnOrder(t *testing.T) {
	t.Parallel()
	selectAll := regexp.MustCompile(`(?is)\bselect\s+\*\s+from\b`)
	insertWithoutColumns := regexp.MustCompile(`(?is)\binsert\s+into\s+(?:"?[a-z_][a-z0-9_]*"?\.)?"?[a-z_][a-z0-9_]*"?\s+(?:values|select)\b`)
	err := filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != "../.." && (entry.Name() == ".git" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		extension := filepath.Ext(path)
		if (extension != ".go" && extension != ".sql") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if selectAll.Match(payload) {
			t.Errorf("product SQL uses SELECT *: %s", path)
		}
		if insertWithoutColumns.Match(payload) {
			t.Errorf("product SQL uses INSERT without an explicit column list: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
