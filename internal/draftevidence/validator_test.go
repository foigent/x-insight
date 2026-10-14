package draftevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDraftGateContractAndFixtures(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	report, err := Run(repositoryRoot, time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if report.Accepted != 17 || report.Rejected != 13 || report.Passed != 30 || report.Failed != 0 {
		t.Fatalf("unexpected report counts: %+v", report)
	}
	if report.ProjectorStatus != "projector_not_applied" || report.RuntimeEligible ||
		report.CompatibilityCommitment != "none" {
		t.Fatalf("Draft boundary drifted: %+v", report)
	}

	wantRejected := map[string]bool{
		"compatibility-commitment":        false,
		"cursor-as-revision":              false,
		"delivery-retry-new-fact":         false,
		"duplicate-usage-cardinality":     false,
		"fact-ordering":                   false,
		"refund-supersedes-terminal":      false,
		"request-id-caused-by":            false,
		"reused-fixture-identity":         false,
		"runtime-eligible":                false,
		"sensitive-raw-prompt":            false,
		"source-derived-missing-identity": false,
		"synthetic-masquerades-source":    false,
		"time-order-caused-by":            false,
	}
	comparators := 0
	for _, result := range report.Results {
		if result.Expectation == "reject" {
			name := strings.TrimSuffix(filepath.Base(result.Path), ".json")
			if _, exists := wantRejected[name]; !exists {
				t.Fatalf("unexpected rejected scenario %q", name)
			}
			if result.ValidationError == "" {
				t.Fatalf("rejected scenario %q has no validation evidence", name)
			}
			wantRejected[name] = true
		}
		if result.Comparator != nil {
			comparators++
			if result.Comparator.ID != ComparatorID || result.ProjectorStatus != "projector_not_applied" {
				t.Fatalf("comparator/projector identity drift: %+v", result)
			}
		}
	}
	for scenario, covered := range wantRejected {
		if !covered {
			t.Fatalf("negative scenario %q was not executed", scenario)
		}
	}
	if comparators != 2 {
		t.Fatalf("V1 differential comparator runs=%d want=2", comparators)
	}
}

func TestCompletedExecutionV1IdentityRemainsImmutable(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	expected := map[string]string{
		"contracts/completed-execution/v1/unirouter.completed-execution.v1.schema.json": "4ae2897d4f8470c38624dfe21c54d72eaf6d995d2c9578c4420c94d244424fb2",
		"contracts/completed-execution/v1/fixtures/error-cursor-expired.json":           "9f85ec830e0e61d0ac75656a980793d6d5c4020265d42ed192693659e1540437",
		"contracts/completed-execution/v1/fixtures/redaction-rejected.json":             "c23fd255c139f94e729ecca70622d6e6341cf7a257c21fbccf592964b0e3853e",
		"contracts/completed-execution/v1/fixtures/valid-cancelled.json":                "31cbfeb23968c07d158b591c28efb3dd40cffba1fd6d1fd4202f43d63bfe9f2d",
		"contracts/completed-execution/v1/fixtures/valid-settled.json":                  "5e9eda0a2630d52c5589d38b8ec480604c47c2cc77391b75a57b2a159db6dd27",
	}
	for path, want := range expected {
		body, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(body)
		if got := hex.EncodeToString(digest[:]); got != want {
			t.Fatalf("%s SHA-256=%s want=%s", path, got, want)
		}
	}
}

func TestDraftPackageIsNotImportedByRuntime(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	wantImport := "github.com/foigent/x-insight/internal/draftevidence"
	err := filepath.WalkDir(repositoryRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.Contains(path, string(filepath.Separator)+"internal"+string(filepath.Separator)+"draftevidence"+string(filepath.Separator)) ||
			strings.Contains(path, string(filepath.Separator)+"cmd"+string(filepath.Separator)+"xinsight-draft-evidence-validator"+string(filepath.Separator)) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), wantImport) {
			t.Fatalf("Draft validator imported outside CI/test tool boundary: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSourceDerivedFixtureIdentityAndAuthorityAreFixed(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	versionDir := filepath.Join(repositoryRoot, ContractRootRelative, DraftVersionDir)
	var manifest Manifest
	if err := decodeFile(filepath.Join(versionDir, "draft-manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(versionDir, "fixtures", "accept", "single-attempt-success.json")
	loadFixture := func() map[string]any {
		body, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		var fixture map[string]any
		if err := json.Unmarshal(body, &fixture); err != nil {
			t.Fatal(err)
		}
		return fixture
	}

	for name, mutate := range map[string]func(map[string]any){
		"authority": func(fixture map[string]any) {
			fixture["evidence"].(map[string]any)["source_authority_status"] = "resolved"
		},
		"ref": func(fixture map[string]any) {
			fixture["origin"].(map[string]any)["source"].(map[string]any)["ref"] = "main"
		},
		"version": func(fixture map[string]any) {
			fixture["origin"].(map[string]any)["source"].(map[string]any)["version"] = "wrong"
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := loadFixture()
			mutate(fixture)
			if err := validateFixtureSemantics(repositoryRoot, manifest, fixture); err == nil {
				t.Fatal("mutated Source identity unexpectedly passed")
			}
		})
	}
}

func TestV1DifferentialRejectsChangedPricingAndIdentity(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	fixturePath := filepath.Join(repositoryRoot, ContractRootRelative, DraftVersionDir, "fixtures", "accept", "single-attempt-success.json")
	loadFixture := func() map[string]any {
		body, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		var fixture map[string]any
		if err := json.Unmarshal(body, &fixture); err != nil {
			t.Fatal(err)
		}
		return fixture
	}

	pricing := loadFixture()
	facts := pricing["evidence"].(map[string]any)["facts"].([]any)
	facts[2].(map[string]any)["value"].(map[string]any)["pricing_version"] = "wrong"
	if err := validateV1Differential(repositoryRoot, pricing); err == nil {
		t.Fatal("changed V1 pricing version unexpectedly passed")
	}

	identity := loadFixture()
	identity["evidence"].(map[string]any)["execution_identity"].(map[string]any)["idempotency_key"] = "new-fact"
	if err := validateV1Differential(repositoryRoot, identity); err == nil {
		t.Fatal("changed V1 idempotency identity unexpectedly passed")
	}
}

func TestFixtureInventoryRequiresExactIndexCoverage(t *testing.T) {
	versionDir := t.TempDir()
	fixturesDir := filepath.Join(versionDir, "fixtures", "accept")
	if err := os.MkdirAll(fixturesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(fixturesDir, "covered.json")
	if err := os.WriteFile(fixturePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	index := FixtureIndex{Fixtures: []FixtureIndexEntry{{Path: "fixtures/accept/covered.json"}}}
	if err := validateFixtureInventory(versionDir, index); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixturesDir, "unindexed.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateFixtureInventory(versionDir, index); err == nil {
		t.Fatal("unindexed fixture unexpectedly passed inventory validation")
	}
}
