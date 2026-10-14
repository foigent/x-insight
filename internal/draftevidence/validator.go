// Package draftevidence validates the X-Insight consumer-side Execution
// Evidence incubation contract. It is CI/test tooling only and is not imported
// by runtime adapters, registries, database code, APIs, or UI code.
package draftevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/evidence"
)

const (
	ContractRootRelative = "contracts/execution-evidence/incubation/unirouter-consumer-candidate"
	DraftVersionDir      = "versions/0.1.0"
	ValidatorID          = "xinsight.execution-evidence-draft-validator"
	ValidatorVersion     = "1.0.0"
	ComparatorID         = "xinsight.completed-execution-v1-draft-differential"
	ComparatorVersion    = "1.0.0"
)

type SourceFileIdentity struct {
	Path          string `json:"path"`
	ContentSHA256 string `json:"content_sha256"`
}

type ToolIdentity struct {
	ID          string               `json:"id"`
	Version     string               `json:"version"`
	SourceFiles []SourceFileIdentity `json:"source_files"`
}

type ArtifactIdentity struct {
	Role          string `json:"role"`
	Path          string `json:"path"`
	ContentSHA256 string `json:"content_sha256"`
}

type RepositoryIdentity struct {
	Repository   string `json:"repository"`
	RepositoryID int64  `json:"repository_id"`
	Commit       string `json:"commit"`
	Tree         string `json:"tree"`
}

type SourceObservation struct {
	Path          string `json:"path"`
	Version       string `json:"version"`
	ContentSHA256 string `json:"content_sha256"`
}

type SourceContext struct {
	Repository         string              `json:"repository"`
	RepositoryID       int64               `json:"repository_id"`
	Commit             string              `json:"commit"`
	Tree               string              `json:"tree"`
	Version            string              `json:"version"`
	AuthorityMechanism string              `json:"authority_mechanism"`
	Observations       []SourceObservation `json:"observations"`
}

type V1Identity struct {
	SchemaPath          string               `json:"schema_path"`
	SchemaContentSHA256 string               `json:"schema_content_sha256"`
	Fixtures            []SourceFileIdentity `json:"fixtures"`
}

type FoigentIdentity struct {
	Version          string            `json:"version"`
	PinPath          string            `json:"pin_path"`
	PinContentSHA256 string            `json:"pin_content_sha256"`
	PublicComponents map[string]string `json:"public_components"`
}

type Manifest struct {
	ManifestVersion         string             `json:"manifest_version"`
	DraftVersion            string             `json:"draft_version"`
	Ownership               string             `json:"ownership"`
	PreviousManifestSHA     string             `json:"previous_manifest_sha256,omitempty"`
	SourceContext           SourceContext      `json:"source_context"`
	ConsumerBaseline        RepositoryIdentity `json:"consumer_baseline"`
	CompletedExecutionV1    V1Identity         `json:"completed_execution_v1"`
	FoigentProtocol         FoigentIdentity    `json:"foigent_protocol"`
	Artifacts               []ArtifactIdentity `json:"artifacts"`
	Validator               ToolIdentity       `json:"validator"`
	Comparator              ToolIdentity       `json:"comparator"`
	AuthorityStatus         string             `json:"authority_status"`
	ReleaseStatus           string             `json:"release_status"`
	RuntimeEligible         bool               `json:"runtime_eligible"`
	CompatibilityCommitment string             `json:"compatibility_commitment"`
}

type FixtureIndex struct {
	IndexVersion        string              `json:"index_version"`
	SchemaID            string              `json:"schema_id"`
	SchemaVersion       string              `json:"schema_version"`
	SchemaContentSHA256 string              `json:"schema_content_sha256"`
	Fixtures            []FixtureIndexEntry `json:"fixtures"`
}

type FixtureIndexEntry struct {
	FixtureID               string `json:"fixture_id"`
	FixtureVersion          string `json:"fixture_version"`
	Path                    string `json:"path"`
	ContentSHA256           string `json:"content_sha256"`
	OriginKind              string `json:"origin_kind"`
	Expectation             string `json:"expectation"`
	ExpectedValidationError string `json:"expected_validation_error,omitempty"`
	ScenarioID              string `json:"scenario_id"`
	CapabilityClaimBoundary string `json:"capability_claim_boundary"`
}

type FixtureResult struct {
	FixtureID                 string        `json:"fixture_id"`
	FixtureVersion            string        `json:"fixture_version"`
	Path                      string        `json:"path"`
	ContentSHA256             string        `json:"content_sha256"`
	SchemaID                  string        `json:"schema_id"`
	SchemaVersion             string        `json:"schema_version"`
	SchemaContentSHA256       string        `json:"schema_content_sha256"`
	Validator                 ToolIdentity  `json:"validator"`
	Expectation               string        `json:"expectation"`
	ExpectedValidationError   string        `json:"expected_validation_error,omitempty"`
	ActualResult              string        `json:"actual_result"`
	ObservedAt                string        `json:"observed_at"`
	OriginKind                string        `json:"origin_kind"`
	SourceIdentity            any           `json:"source_identity,omitempty"`
	ProjectorStatus           string        `json:"projector_status"`
	Comparator                *ToolIdentity `json:"comparator,omitempty"`
	Provenance                string        `json:"provenance"`
	CapabilitySupportNonclaim string        `json:"capability_support_nonclaim"`
	ValidationError           string        `json:"validation_error,omitempty"`
	Passed                    bool          `json:"passed"`
}

type Report struct {
	ReportVersion             string          `json:"report_version"`
	DraftGate                 string          `json:"draft_gate"`
	SchemaAuthority           string          `json:"schema_authority"`
	RuntimeEligible           bool            `json:"runtime_eligible"`
	CompatibilityCommitment   string          `json:"compatibility_commitment"`
	ProjectorStatus           string          `json:"projector_status"`
	ObservedAt                string          `json:"observed_at"`
	Results                   []FixtureResult `json:"results"`
	Accepted                  int             `json:"accepted"`
	Rejected                  int             `json:"rejected"`
	Passed                    int             `json:"passed"`
	Failed                    int             `json:"failed"`
	CapabilitySupportNonclaim string          `json:"capability_support_nonclaim"`
}

func Run(repositoryRoot string, observedAt time.Time) (Report, error) {
	if repositoryRoot == "" {
		return Report{}, errors.New("repository root is required")
	}
	if observedAt.IsZero() {
		return Report{}, errors.New("observed_at is required")
	}
	repositoryRoot, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return Report{}, fmt.Errorf("resolve repository root: %w", err)
	}
	versionDir := filepath.Join(repositoryRoot, ContractRootRelative, DraftVersionDir)

	var manifest Manifest
	if err := decodeFile(filepath.Join(versionDir, "draft-manifest.json"), &manifest); err != nil {
		return Report{}, err
	}
	if err := validateJSONSchema(filepath.Join(versionDir, "draft-manifest.schema.json"), filepath.Join(versionDir, "draft-manifest.json")); err != nil {
		return Report{}, err
	}
	if err := validateManifest(repositoryRoot, versionDir, manifest); err != nil {
		return Report{}, err
	}
	if err := validateMatrix(versionDir); err != nil {
		return Report{}, err
	}

	var index FixtureIndex
	indexPath := filepath.Join(versionDir, "fixture-index.json")
	if err := decodeFile(indexPath, &index); err != nil {
		return Report{}, err
	}
	if err := validateJSONSchema(filepath.Join(versionDir, "fixture-index.schema.json"), indexPath); err != nil {
		return Report{}, err
	}
	schemaPath := filepath.Join(versionDir, "draft-evidence.schema.json")
	if digest, err := fileSHA256(schemaPath); err != nil || digest != index.SchemaContentSHA256 {
		return Report{}, fmt.Errorf("fixture index schema identity mismatch: got=%q err=%v want=%q", digest, err, index.SchemaContentSHA256)
	}
	if err := validateFixtureInventory(versionDir, index); err != nil {
		return Report{}, err
	}

	report := Report{
		ReportVersion:             "xinsight.execution-evidence-draft-conformance-report.v1",
		DraftGate:                 "incubation_consumer_candidate",
		SchemaAuthority:           "x-insight-consumer-side-only",
		RuntimeEligible:           false,
		CompatibilityCommitment:   "none",
		ProjectorStatus:           "projector_not_applied",
		ObservedAt:                observedAt.UTC().Format(time.RFC3339),
		CapabilitySupportNonclaim: "Conformance results do not prove UniRouter release status, model capability support, External Integration, Target, or Production behavior.",
	}

	validator, err := evidence.NewSchemaValidator(schemaPath)
	if err != nil {
		return Report{}, fmt.Errorf("compile draft schema: %w", err)
	}
	seenFixtureIdentity := map[string]string{}
	for _, entry := range index.Fixtures {
		result := validateFixture(repositoryRoot, versionDir, index, manifest, entry, validator, seenFixtureIdentity, observedAt)
		report.Results = append(report.Results, result)
		if result.ActualResult == "accept" {
			report.Accepted++
		} else {
			report.Rejected++
		}
		if result.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}
	if report.Failed != 0 {
		return report, fmt.Errorf("draft evidence conformance failed: %d fixture expectations mismatched", report.Failed)
	}
	return report, nil
}

func WriteReport(path string, report Report) error {
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	body = append(body, '\n')
	if path == "-" {
		_, err = os.Stdout.Write(body)
		return err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write report %s: %w", path, err)
	}
	return nil
}

func validateManifest(repositoryRoot, versionDir string, manifest Manifest) error {
	if manifest.RuntimeEligible || manifest.CompatibilityCommitment != "none" ||
		manifest.AuthorityStatus != "undefined" || manifest.ReleaseStatus != "no_eligible_release" ||
		manifest.SourceContext.AuthorityMechanism != "undefined" {
		return errors.New("draft manifest crossed the frozen authority/runtime/compatibility boundary")
	}
	if manifest.PreviousManifestSHA != "" {
		return errors.New("first draft manifest must not declare previous_manifest_sha256")
	}
	requiredRoles := map[string]int{
		"draft_schema": 0, "matrix_schema": 0, "matrix": 0,
		"fixture_index_schema": 0, "fixture_index": 0, "manifest_schema": 0,
	}
	artifactPaths := map[string]struct{}{}
	compatibilityInputs := 0
	for _, artifact := range manifest.Artifacts {
		if _, duplicate := artifactPaths[artifact.Path]; duplicate {
			return fmt.Errorf("duplicate manifest artifact path %s", artifact.Path)
		}
		artifactPaths[artifact.Path] = struct{}{}
		if _, required := requiredRoles[artifact.Role]; required {
			requiredRoles[artifact.Role]++
		}
		if artifact.Role == "compatibility_input" {
			compatibilityInputs++
		}
		if err := verifyIdentity(repositoryRoot, artifact.Path, artifact.ContentSHA256); err != nil {
			return fmt.Errorf("artifact %s: %w", artifact.Role, err)
		}
	}
	for role, count := range requiredRoles {
		if count != 1 {
			return fmt.Errorf("manifest artifact role %s count=%d want=1", role, count)
		}
	}
	if compatibilityInputs == 0 {
		return errors.New("manifest has no compatibility input identity")
	}
	for _, tool := range []ToolIdentity{manifest.Validator, manifest.Comparator} {
		if len(tool.SourceFiles) == 0 {
			return fmt.Errorf("tool %s has no source identity", tool.ID)
		}
		paths := make([]string, 0, len(tool.SourceFiles))
		seenPaths := map[string]struct{}{}
		for _, source := range tool.SourceFiles {
			if _, duplicate := seenPaths[source.Path]; duplicate {
				return fmt.Errorf("tool %s has duplicate source path %s", tool.ID, source.Path)
			}
			seenPaths[source.Path] = struct{}{}
			paths = append(paths, source.Path)
			if err := verifyIdentity(repositoryRoot, source.Path, source.ContentSHA256); err != nil {
				return fmt.Errorf("tool %s: %w", tool.ID, err)
			}
		}
		if !sort.StringsAreSorted(paths) {
			return fmt.Errorf("tool %s source paths are not sorted", tool.ID)
		}
	}
	if manifest.Validator.ID != ValidatorID || manifest.Validator.Version != ValidatorVersion ||
		manifest.Comparator.ID != ComparatorID || manifest.Comparator.Version != ComparatorVersion {
		return errors.New("validator or comparator identity mismatch")
	}
	if err := verifyIdentity(repositoryRoot, manifest.CompletedExecutionV1.SchemaPath, manifest.CompletedExecutionV1.SchemaContentSHA256); err != nil {
		return fmt.Errorf("completed-execution V1 schema: %w", err)
	}
	for _, fixture := range manifest.CompletedExecutionV1.Fixtures {
		if err := verifyIdentity(repositoryRoot, fixture.Path, fixture.ContentSHA256); err != nil {
			return fmt.Errorf("completed-execution V1 fixture: %w", err)
		}
	}
	if err := verifyIdentity(repositoryRoot, manifest.FoigentProtocol.PinPath, manifest.FoigentProtocol.PinContentSHA256); err != nil {
		return fmt.Errorf("Foigent pin: %w", err)
	}
	var pin map[string]any
	if err := decodeFile(filepath.Join(repositoryRoot, filepath.FromSlash(manifest.FoigentProtocol.PinPath)), &pin); err != nil {
		return err
	}
	if pin["protocol_version"] != manifest.FoigentProtocol.Version {
		return errors.New("Foigent protocol version drift")
	}
	components, ok := pin["components"].(map[string]any)
	if !ok {
		return errors.New("Foigent pin components are missing")
	}
	for component, digest := range manifest.FoigentProtocol.PublicComponents {
		if components[component] != digest {
			return fmt.Errorf("Foigent public component %s drift", component)
		}
	}
	_ = versionDir
	return nil
}

func validateFixtureInventory(versionDir string, index FixtureIndex) error {
	expected := make(map[string]struct{}, len(index.Fixtures))
	for _, entry := range index.Fixtures {
		path := filepath.ToSlash(entry.Path)
		if _, duplicate := expected[path]; duplicate {
			return fmt.Errorf("duplicate fixture index path %s", path)
		}
		expected[path] = struct{}{}
	}
	actual := map[string]struct{}{}
	fixturesDir := filepath.Join(versionDir, "fixtures")
	err := filepath.WalkDir(fixturesDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		relative, err := filepath.Rel(versionDir, path)
		if err != nil {
			return err
		}
		actual[filepath.ToSlash(relative)] = struct{}{}
		return nil
	})
	if err != nil {
		return fmt.Errorf("enumerate fixture inventory: %w", err)
	}
	for path := range expected {
		if _, exists := actual[path]; !exists {
			return fmt.Errorf("indexed fixture is missing from inventory: %s", path)
		}
	}
	for path := range actual {
		if _, exists := expected[path]; !exists {
			return fmt.Errorf("fixture is not bound by the index: %s", path)
		}
	}
	return nil
}

func validateMatrix(versionDir string) error {
	matrixPath := filepath.Join(versionDir, "field-matrix.json")
	if err := validateJSONSchema(filepath.Join(versionDir, "field-matrix.schema.json"), matrixPath); err != nil {
		return err
	}
	var matrix struct {
		Fields []struct {
			Identity       string `json:"field_identity"`
			Classification string `json:"classification"`
		} `json:"fields"`
		Entries []struct {
			OperationFamily         string   `json:"operation_family"`
			ProtocolProfile         string   `json:"protocol_profile"`
			ProtocolVersion         string   `json:"protocol_version"`
			CapabilityType          string   `json:"capability_type"`
			ExecutionProfile        string   `json:"execution_profile"`
			ExecutionProfileVersion string   `json:"execution_profile_version"`
			FieldBindings           []string `json:"field_bindings"`
		} `json:"entries"`
	}
	if err := decodeProjectionFile(matrixPath, &matrix); err != nil {
		return err
	}
	fieldIDs := map[string]struct{}{}
	for _, field := range matrix.Fields {
		if field.Classification == "frozen_public" {
			return fmt.Errorf("matrix field %s illegally claims frozen_public", field.Identity)
		}
		if _, duplicate := fieldIDs[field.Identity]; duplicate {
			return fmt.Errorf("duplicate matrix field identity %s", field.Identity)
		}
		fieldIDs[field.Identity] = struct{}{}
	}
	entryKeys := map[string]struct{}{}
	for _, entry := range matrix.Entries {
		key := strings.Join([]string{entry.OperationFamily, entry.ProtocolProfile, entry.ProtocolVersion, entry.CapabilityType, entry.ExecutionProfile, entry.ExecutionProfileVersion}, "\x00")
		if _, duplicate := entryKeys[key]; duplicate {
			return errors.New("duplicate matrix primary key")
		}
		entryKeys[key] = struct{}{}
		for _, binding := range entry.FieldBindings {
			if _, exists := fieldIDs[binding]; !exists {
				return fmt.Errorf("matrix entry references unknown field %s", binding)
			}
		}
	}
	return nil
}

func validateFixture(repositoryRoot, versionDir string, index FixtureIndex, manifest Manifest, entry FixtureIndexEntry, validator *evidence.SchemaValidator, seen map[string]string, observedAt time.Time) FixtureResult {
	result := FixtureResult{
		FixtureID: entry.FixtureID, FixtureVersion: entry.FixtureVersion, Path: entry.Path,
		ContentSHA256: entry.ContentSHA256, SchemaID: index.SchemaID, SchemaVersion: index.SchemaVersion,
		SchemaContentSHA256: index.SchemaContentSHA256, Validator: manifest.Validator,
		Expectation: entry.Expectation, ExpectedValidationError: entry.ExpectedValidationError,
		ObservedAt: observedAt.UTC().Format(time.RFC3339),
		OriginKind: entry.OriginKind, ProjectorStatus: "projector_not_applied",
		Provenance:                "fixture raw bytes plus versioned index identity",
		CapabilitySupportNonclaim: "This fixture result is conformance evidence only and does not prove UniRouter or model capability support.",
	}
	path := filepath.Join(versionDir, filepath.FromSlash(entry.Path))
	var validationErr error
	digest, err := fileSHA256(path)
	if err != nil {
		validationErr = err
	} else if digest != entry.ContentSHA256 {
		validationErr = fmt.Errorf("fixture digest=%s want=%s", digest, entry.ContentSHA256)
	}
	var fixture map[string]any
	if err := decodeFile(path, &fixture); err != nil && validationErr == nil {
		validationErr = err
	}
	if validationErr == nil {
		if err := validateFixtureIndexMetadata(entry, fixture); err != nil {
			validationErr = err
		}
	}
	if validationErr == nil {
		if err := validator.Validate(fixture); err != nil {
			validationErr = fmt.Errorf("schema: %w", err)
		}
	}
	if validationErr == nil {
		if previous, duplicate := seen[entry.FixtureID]; duplicate && previous != entry.ContentSHA256 {
			validationErr = fmt.Errorf("fixture identity %s reused with different content", entry.FixtureID)
		}
	}
	if _, exists := seen[entry.FixtureID]; !exists {
		seen[entry.FixtureID] = entry.ContentSHA256
	}
	if validationErr == nil {
		if source, ok := fixture["origin"].(map[string]any)["source"]; ok {
			result.SourceIdentity = source
		}
		if err := validateFixtureSemantics(repositoryRoot, manifest, fixture); err != nil {
			validationErr = err
		}
	}
	if validationErr == nil {
		if _, exists := fixture["v1_differential"]; exists {
			comparator := manifest.Comparator
			result.Comparator = &comparator
			if err := validateV1Differential(repositoryRoot, fixture); err != nil {
				validationErr = err
			}
		}
	}
	if validationErr == nil {
		result.ActualResult = "accept"
	} else {
		result.ActualResult = "reject"
		result.ValidationError = validationErr.Error()
	}
	result.Passed = result.ActualResult == result.Expectation
	if result.Passed && result.Expectation == "reject" &&
		!strings.Contains(result.ValidationError, entry.ExpectedValidationError) {
		result.Passed = false
		result.ValidationError = fmt.Sprintf(
			"%s; expected rejection evidence containing %q",
			result.ValidationError,
			entry.ExpectedValidationError,
		)
	}
	return result
}

func validateFixtureIndexMetadata(entry FixtureIndexEntry, fixture map[string]any) error {
	stringValue := func(path string) string {
		switch path {
		case "fixture_id", "fixture_version", "scenario_id", "capability_claim_boundary":
			value, _ := fixture[path].(string)
			return value
		case "origin_kind":
			origin, _ := fixture["origin"].(map[string]any)
			value, _ := origin["kind"].(string)
			return value
		case "expectation":
			expectation, _ := fixture["expectation"].(map[string]any)
			value, _ := expectation["result"].(string)
			return value
		}
		return ""
	}
	checks := map[string]string{
		"fixture_id": entry.FixtureID, "fixture_version": entry.FixtureVersion,
		"origin_kind": entry.OriginKind, "expectation": entry.Expectation,
		"scenario_id": entry.ScenarioID, "capability_claim_boundary": entry.CapabilityClaimBoundary,
	}
	for field, expected := range checks {
		if actual := stringValue(field); actual != expected {
			return fmt.Errorf("fixture index %s=%q payload=%q", field, expected, actual)
		}
	}
	return nil
}

func validateJSONSchema(schemaPath, documentPath string) error {
	validator, err := evidence.NewSchemaValidator(schemaPath)
	if err != nil {
		return fmt.Errorf("compile schema %s: %w", schemaPath, err)
	}
	var document map[string]any
	if err := decodeFile(documentPath, &document); err != nil {
		return err
	}
	if err := validator.Validate(document); err != nil {
		return fmt.Errorf("validate %s: %w", documentPath, err)
	}
	return nil
}

func decodeFile(path string, target any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode %s: trailing content", path)
	}
	return nil
}

func decodeProjectionFile(path string, target any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func verifyIdentity(repositoryRoot, relativePath, expected string) error {
	if filepath.IsAbs(relativePath) || strings.Contains(filepath.ToSlash(relativePath), "../") {
		return fmt.Errorf("unsafe identity path %q", relativePath)
	}
	actual, err := fileSHA256(filepath.Join(repositoryRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("%s content_sha256=%s want=%s", relativePath, actual, expected)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
