package hostctl

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const CleanBaselineSchemaContractVersion = "xinsight.schema-contract.v2"

// cleanBaselineSchemaContractJSON is generated only from a migrated PostgreSQL
// 16 database by cmd/xinsight-schema-contract. It describes the current
// development migration set. Historical signed releases keep their immutable
// contract file and release metadata unchanged.
//
//go:embed development_schema_contract.json
var cleanBaselineSchemaContractJSON []byte

type SchemaContractObject struct {
	Kind           string `json:"kind"`
	Namespace      string `json:"namespace"`
	Owner          string `json:"owner,omitempty"`
	Name           string `json:"name"`
	SemanticSHA256 string `json:"semantic_sha256"`
}

type SchemaContractManifest struct {
	SchemaVersion      string                     `json:"schema_version"`
	DatabaseSchema     string                     `json:"database_schema"`
	PostgresMajor      int                        `json:"postgres_major"`
	MigrationCount     int                        `json:"migration_count"`
	MigrationSetSHA256 string                     `json:"migration_set_sha256"`
	SemanticSHA256     string                     `json:"semantic_sha256"`
	ColumnLayoutSHA256 string                     `json:"column_layout_sha256"`
	Objects            CleanBaselineSchemaObjects `json:"objects"`
	Entries            []SchemaContractObject     `json:"entries"`
}

type SchemaContractMismatch struct {
	Reason         string `json:"reason"`
	ContractSide   string `json:"contract_side,omitempty"`
	Kind           string `json:"kind,omitempty"`
	Namespace      string `json:"namespace,omitempty"`
	Owner          string `json:"owner,omitempty"`
	Name           string `json:"name,omitempty"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	ActualSHA256   string `json:"actual_sha256,omitempty"`
}

type SchemaContractInspection struct {
	SchemaVersion        string                     `json:"schema_version"`
	Valid                bool                       `json:"valid"`
	ExpectedSHA256       string                     `json:"expected_sha256"`
	ActualSHA256         string                     `json:"actual_sha256"`
	ExpectedLayoutSHA256 string                     `json:"expected_column_layout_sha256"`
	ActualLayoutSHA256   string                     `json:"actual_column_layout_sha256"`
	LayoutMatches        bool                       `json:"column_layout_matches"`
	Expected             SchemaContractIdentity     `json:"expected"`
	Actual               SchemaContractIdentity     `json:"actual"`
	Objects              CleanBaselineSchemaObjects `json:"objects"`
	Mismatches           []SchemaContractMismatch   `json:"mismatches"`
}

type SchemaContractIdentity struct {
	DatabaseSchema     string `json:"database_schema"`
	PostgresMajor      int    `json:"postgres_major"`
	MigrationCount     int    `json:"migration_count"`
	MigrationSetSHA256 string `json:"migration_set_sha256"`
}

type schemaContractRow struct {
	Kind       string
	Namespace  string
	Owner      string
	Name       string
	Definition string
	Layout     string
}

var expectedCleanBaselineSchemaContract = mustLoadCleanBaselineSchemaContract()

func ExpectedCleanBaselineSchemaContract() SchemaContractManifest {
	manifest := expectedCleanBaselineSchemaContract
	manifest.Entries = append([]SchemaContractObject(nil), expectedCleanBaselineSchemaContract.Entries...)
	return manifest
}

func CleanBaselineSchemaContractBytes() []byte {
	return append([]byte(nil), cleanBaselineSchemaContractJSON...)
}

func InspectCleanBaselineSchemaContractDatabase(
	ctx context.Context,
	credentials DatabaseCredentials,
) (SchemaContractInspection, error) {
	database, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		return SchemaContractInspection{}, fmt.Errorf("schema contract database cannot be opened")
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return SchemaContractInspection{}, fmt.Errorf("schema contract database cannot be reached")
	}
	return InspectCleanBaselineSchemaContract(ctx, database)
}

func mustLoadCleanBaselineSchemaContract() SchemaContractManifest {
	var manifest SchemaContractManifest
	if err := json.Unmarshal(cleanBaselineSchemaContractJSON, &manifest); err != nil {
		panic(fmt.Sprintf("decode generated clean-baseline schema contract: %v", err))
	}
	if manifest.SchemaVersion != CleanBaselineSchemaContractVersion ||
		manifest.DatabaseSchema == "" || manifest.PostgresMajor != 16 ||
		manifest.MigrationCount <= 0 || !sha256ReferencePattern.MatchString(manifest.MigrationSetSHA256) ||
		!sha256ReferencePattern.MatchString(manifest.SemanticSHA256) ||
		!sha256ReferencePattern.MatchString(manifest.ColumnLayoutSHA256) {
		panic("generated clean-baseline schema contract identity is invalid")
	}
	if duplicates := schemaContractDuplicateMismatches("expected", manifest.Entries); len(duplicates) != 0 {
		panic("generated clean-baseline schema contract contains duplicate_object_identity")
	}
	digest := sha256.New()
	objects := CleanBaselineSchemaObjects{}
	previous := ""
	for _, entry := range manifest.Entries {
		key := schemaContractObjectKey(entry)
		if previous != "" && previous >= key || !sha256ReferencePattern.MatchString(entry.SemanticSHA256) {
			panic("generated clean-baseline schema contract is not strictly sorted")
		}
		previous = key
		incrementSchemaObjectCount(&objects, entry.Kind)
		encoded, _ := json.Marshal(entry)
		_, _ = digest.Write(encoded)
		_, _ = digest.Write([]byte{'\n'})
	}
	computed := "sha256:" + hex.EncodeToString(digest.Sum(nil))
	if len(manifest.Entries) == 0 || computed != manifest.SemanticSHA256 || objects != manifest.Objects {
		panic("generated clean-baseline schema contract contents are invalid")
	}
	return manifest
}

// GenerateCleanBaselineSchemaContract is a qualification-only operation. The
// caller must supply a fresh or supported-upgrade PostgreSQL 16 database after
// the repository migration set and product function privileges have converged.
func GenerateCleanBaselineSchemaContract(
	ctx context.Context,
	querier cleanBaselineSchemaQuerier,
) (SchemaContractManifest, error) {
	if err := validateSchemaContractGenerationPrivileges(ctx, querier); err != nil {
		return SchemaContractManifest{}, err
	}
	return buildSchemaContractManifest(ctx, querier)
}

func validateSchemaContractGenerationPrivileges(
	ctx context.Context,
	querier cleanBaselineSchemaQuerier,
) error {
	var unexpected, managed int
	if err := querier.QueryRowContext(ctx, cleanBaselineFunctionPrivilegeQuery).Scan(&unexpected, &managed); err != nil {
		return fmt.Errorf("schema contract function privileges cannot be inspected")
	}
	if unexpected != 0 || managed > 12 {
		return fmt.Errorf("schema contract generation requires converged function privileges")
	}
	return nil
}

func InspectCleanBaselineSchemaContract(
	ctx context.Context,
	querier cleanBaselineSchemaQuerier,
) (SchemaContractInspection, error) {
	actual, err := buildSchemaContractManifest(ctx, querier)
	if err != nil {
		return SchemaContractInspection{}, err
	}
	expected := expectedCleanBaselineSchemaContract
	inspection := SchemaContractInspection{
		SchemaVersion:        CleanBaselineSchemaContractVersion,
		ExpectedSHA256:       expected.SemanticSHA256,
		ActualSHA256:         actual.SemanticSHA256,
		ExpectedLayoutSHA256: expected.ColumnLayoutSHA256,
		ActualLayoutSHA256:   actual.ColumnLayoutSHA256,
		LayoutMatches:        expected.ColumnLayoutSHA256 == actual.ColumnLayoutSHA256,
		Expected: SchemaContractIdentity{
			DatabaseSchema: expected.DatabaseSchema, PostgresMajor: expected.PostgresMajor,
			MigrationCount: expected.MigrationCount, MigrationSetSHA256: expected.MigrationSetSHA256,
		},
		Actual: SchemaContractIdentity{
			DatabaseSchema: actual.DatabaseSchema, PostgresMajor: actual.PostgresMajor,
			MigrationCount: actual.MigrationCount, MigrationSetSHA256: actual.MigrationSetSHA256,
		},
		Objects: actual.Objects,
	}
	inspection.Mismatches = compareSchemaContracts(expected, actual)
	inspection.Valid = len(inspection.Mismatches) == 0
	return inspection, nil
}

func schemaContractDriftError(inspection SchemaContractInspection) error {
	if inspection.Valid {
		return nil
	}
	reason := "schema_contract_mismatch"
	object := "release"
	if len(inspection.Mismatches) != 0 {
		mismatch := inspection.Mismatches[0]
		reason = mismatch.Reason
		object = strings.Join([]string{mismatch.Namespace, mismatch.Owner, mismatch.Name}, ".")
		object = strings.Trim(object, ".")
		if object == "" {
			object = "release"
		}
	}
	return fmt.Errorf(
		"release schema contract drift: expected=%s actual=%s mismatches=%d first_reason=%s first_object=%s",
		inspection.ExpectedSHA256,
		inspection.ActualSHA256,
		len(inspection.Mismatches),
		reason,
		object,
	)
}

func buildSchemaContractManifest(
	ctx context.Context,
	querier cleanBaselineSchemaQuerier,
) (SchemaContractManifest, error) {
	identity, err := inspectSchemaContractIdentity(ctx, querier)
	if err != nil {
		return SchemaContractManifest{}, err
	}
	rows, err := querySchemaContractRows(ctx, querier)
	if err != nil {
		return SchemaContractManifest{}, err
	}
	entries := make([]SchemaContractObject, 0, len(rows))
	objects := CleanBaselineSchemaObjects{}
	layoutDigest := sha256.New()
	for _, row := range rows {
		definitionDigest := sha256.Sum256([]byte(row.Definition))
		entry := SchemaContractObject{
			Kind: row.Kind, Namespace: row.Namespace, Owner: row.Owner, Name: row.Name,
			SemanticSHA256: "sha256:" + hex.EncodeToString(definitionDigest[:]),
		}
		entries = append(entries, entry)
		if row.Kind == "C" {
			encoded, _ := json.Marshal(struct {
				Namespace string `json:"namespace"`
				Table     string `json:"table"`
				Column    string `json:"column"`
				Ordinal   string `json:"ordinal"`
			}{row.Namespace, row.Owner, row.Name, row.Layout})
			_, _ = layoutDigest.Write(encoded)
			_, _ = layoutDigest.Write([]byte{'\n'})
		}
		incrementSchemaObjectCount(&objects, row.Kind)
	}
	sort.Slice(entries, func(i, j int) bool {
		return schemaContractObjectKey(entries[i]) < schemaContractObjectKey(entries[j])
	})
	if duplicates := schemaContractDuplicateMismatches("actual", entries); len(duplicates) != 0 {
		return SchemaContractManifest{}, schemaContractDuplicateIdentityError(duplicates[0])
	}
	digest := sha256.New()
	for _, entry := range entries {
		encoded, _ := json.Marshal(entry)
		_, _ = digest.Write(encoded)
		_, _ = digest.Write([]byte{'\n'})
	}
	return SchemaContractManifest{
		SchemaVersion:  CleanBaselineSchemaContractVersion,
		DatabaseSchema: identity.DatabaseSchema, PostgresMajor: identity.PostgresMajor,
		MigrationCount: identity.MigrationCount, MigrationSetSHA256: identity.MigrationSetSHA256,
		SemanticSHA256:     "sha256:" + hex.EncodeToString(digest.Sum(nil)),
		ColumnLayoutSHA256: "sha256:" + hex.EncodeToString(layoutDigest.Sum(nil)),
		Objects:            objects, Entries: entries,
	}, nil
}

func querySchemaContractRows(
	ctx context.Context,
	querier cleanBaselineSchemaQuerier,
) ([]schemaContractRow, error) {
	rows, err := querier.QueryContext(ctx, cleanBaselineSchemaQuery)
	if err != nil {
		return nil, fmt.Errorf("inspect release schema contract: %w", err)
	}
	defer rows.Close()
	result := make([]schemaContractRow, 0, 768)
	for rows.Next() {
		var row schemaContractRow
		if err := rows.Scan(&row.Kind, &row.Namespace, &row.Owner, &row.Name, &row.Definition, &row.Layout); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool {
		left := result[i]
		right := result[j]
		return strings.Join([]string{left.Kind, left.Namespace, left.Owner, left.Name}, "\x00") <
			strings.Join([]string{right.Kind, right.Namespace, right.Owner, right.Name}, "\x00")
	})
	return result, nil
}

func inspectSchemaContractIdentity(
	ctx context.Context,
	querier cleanBaselineSchemaQuerier,
) (SchemaContractIdentity, error) {
	var versionNumber int
	if err := querier.QueryRowContext(ctx, `SELECT current_setting('server_version_num')::integer`).Scan(&versionNumber); err != nil {
		return SchemaContractIdentity{}, fmt.Errorf("inspect schema contract PostgreSQL identity: %w", err)
	}
	rows, err := querier.QueryContext(ctx, `
SELECT version, checksum_sha256, dirty
FROM schema_migrations
ORDER BY version`)
	if err != nil {
		return SchemaContractIdentity{}, fmt.Errorf("inspect schema contract migration identity: %w", err)
	}
	defer rows.Close()
	digest := sha256.New()
	count := 0
	lastSequence := 0
	for rows.Next() {
		var version string
		var checksum *string
		var dirty bool
		if err := rows.Scan(&version, &checksum, &dirty); err != nil {
			return SchemaContractIdentity{}, err
		}
		if dirty || checksum == nil || !sha256ReferencePattern.MatchString(*checksum) {
			return SchemaContractIdentity{}, fmt.Errorf("schema contract migration ledger is incomplete or dirty")
		}
		parts := strings.SplitN(version, "_", 2)
		sequence, err := strconv.Atoi(strings.TrimLeft(parts[0], "0"))
		if err != nil || sequence <= lastSequence {
			return SchemaContractIdentity{}, fmt.Errorf("schema contract migration ledger order is invalid")
		}
		lastSequence = sequence
		_, _ = fmt.Fprintf(digest, "%s\t%s\n", version, *checksum)
		count++
	}
	if err := rows.Err(); err != nil {
		return SchemaContractIdentity{}, err
	}
	if count == 0 {
		return SchemaContractIdentity{}, fmt.Errorf("schema contract migration ledger is empty")
	}
	return SchemaContractIdentity{
		DatabaseSchema:     strconv.Itoa(lastSequence),
		PostgresMajor:      versionNumber / 10000,
		MigrationCount:     count,
		MigrationSetSHA256: "sha256:" + hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func compareSchemaContracts(expected, actual SchemaContractManifest) []SchemaContractMismatch {
	duplicateMismatches := append(
		schemaContractDuplicateMismatches("expected", expected.Entries),
		schemaContractDuplicateMismatches("actual", actual.Entries)...,
	)
	if len(duplicateMismatches) != 0 {
		return duplicateMismatches
	}
	mismatches := make([]SchemaContractMismatch, 0)
	if expected.SchemaVersion != actual.SchemaVersion {
		mismatches = append(mismatches, SchemaContractMismatch{Reason: "canonical_version_mismatch"})
	}
	if expected.DatabaseSchema != actual.DatabaseSchema {
		mismatches = append(mismatches, SchemaContractMismatch{Reason: "database_schema_mismatch", ExpectedSHA256: hashCleanBaselineText(expected.DatabaseSchema), ActualSHA256: hashCleanBaselineText(actual.DatabaseSchema)})
	}
	if expected.PostgresMajor != actual.PostgresMajor {
		mismatches = append(mismatches, SchemaContractMismatch{Reason: "postgres_major_mismatch"})
	}
	if expected.MigrationCount != actual.MigrationCount || expected.MigrationSetSHA256 != actual.MigrationSetSHA256 {
		mismatches = append(mismatches, SchemaContractMismatch{Reason: "migration_set_mismatch", ExpectedSHA256: expected.MigrationSetSHA256, ActualSHA256: actual.MigrationSetSHA256})
	}
	expectedByKey := make(map[string]SchemaContractObject, len(expected.Entries))
	actualByKey := make(map[string]SchemaContractObject, len(actual.Entries))
	keys := make([]string, 0, len(expected.Entries)+len(actual.Entries))
	for _, entry := range expected.Entries {
		key := schemaContractObjectKey(entry)
		expectedByKey[key] = entry
		keys = append(keys, key)
	}
	for _, entry := range actual.Entries {
		key := schemaContractObjectKey(entry)
		actualByKey[key] = entry
		if _, exists := expectedByKey[key]; !exists {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		expectedEntry, expectedOK := expectedByKey[key]
		actualEntry, actualOK := actualByKey[key]
		switch {
		case !actualOK:
			mismatches = append(mismatches, mismatchForObject("missing_object", expectedEntry, expectedEntry.SemanticSHA256, ""))
		case !expectedOK:
			mismatches = append(mismatches, mismatchForObject("unexpected_object", actualEntry, "", actualEntry.SemanticSHA256))
		case expectedEntry.SemanticSHA256 != actualEntry.SemanticSHA256:
			mismatches = append(mismatches, mismatchForObject("semantic_property_drift", actualEntry, expectedEntry.SemanticSHA256, actualEntry.SemanticSHA256))
		}
	}
	return mismatches
}

func schemaContractDuplicateMismatches(
	contractSide string,
	entries []SchemaContractObject,
) []SchemaContractMismatch {
	counts := make(map[string]int, len(entries))
	objects := make(map[string]SchemaContractObject, len(entries))
	for _, entry := range entries {
		key := schemaContractObjectKey(entry)
		counts[key]++
		if counts[key] == 1 {
			objects[key] = entry
		}
	}
	keys := make([]string, 0)
	for key, count := range counts {
		if count > 1 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	mismatches := make([]SchemaContractMismatch, 0, len(keys))
	for _, key := range keys {
		object := objects[key]
		mismatches = append(mismatches, SchemaContractMismatch{
			Reason: "duplicate_object_identity", ContractSide: contractSide,
			Kind: object.Kind, Namespace: object.Namespace, Owner: object.Owner, Name: object.Name,
		})
	}
	return mismatches
}

func schemaContractDuplicateIdentityError(mismatch SchemaContractMismatch) error {
	object := SchemaContractObject{
		Kind: mismatch.Kind, Namespace: mismatch.Namespace, Owner: mismatch.Owner, Name: mismatch.Name,
	}
	return fmt.Errorf(
		"inspect release schema contract: duplicate_object_identity contract_side=%s object_key_sha256=%s",
		mismatch.ContractSide,
		hashCleanBaselineText(schemaContractObjectKey(object)),
	)
}

func mismatchForObject(reason string, object SchemaContractObject, expected, actual string) SchemaContractMismatch {
	return SchemaContractMismatch{
		Reason: reason, Kind: object.Kind, Namespace: object.Namespace,
		Owner: object.Owner, Name: object.Name,
		ExpectedSHA256: expected, ActualSHA256: actual,
	}
}

func schemaContractObjectKey(entry SchemaContractObject) string {
	return strings.Join([]string{entry.Kind, entry.Namespace, entry.Owner, entry.Name}, "\x00")
}

func incrementSchemaObjectCount(objects *CleanBaselineSchemaObjects, kind string) {
	switch kind {
	case "N":
		objects.Namespaces++
	case "T":
		objects.Tables++
	case "C":
		objects.Columns++
	case "K":
		objects.Constraints++
	case "I":
		objects.Indexes++
	case "G":
		objects.Triggers++
	case "F":
		objects.Functions++
	case "E":
		objects.Extensions++
	case "Y":
		objects.Types++
	case "R":
		objects.Relations++
	default:
		panic("unknown schema contract object kind: " + kind)
	}
}
