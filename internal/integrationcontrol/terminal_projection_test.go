package integrationcontrol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	unirouteradapter "github.com/foigent/x-insight/internal/adapters/unirouter"
)

func TestCampaignTerminalProjectionParserRejectsTerminalContradictions(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join(
		"testdata", "n2-terminal-projection", "terminal-projection.valid.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	canonical := bytes.TrimSuffix(payload, []byte("\n"))
	projection, err := ParseCampaignTerminalProjection(canonical)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*CampaignTerminalProjection){
		"outcome convergence": func(value *CampaignTerminalProjection) {
			value.Outcomes.Conflict = 1
		},
		"completeness": func(value *CampaignTerminalProjection) {
			value.Completeness.AllMemberSourceProfilesComplete = true
		},
		"gap-code order": func(value *CampaignTerminalProjection) {
			value.Completeness.GapCodes = []string{"z_gap", "a_gap"}
		},
		"completion time": func(value *CampaignTerminalProjection) {
			value.CompletedAt = "2030-01-01T00:59:59Z"
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			drifted := projection
			drifted.Completeness.GapCodes = append(
				[]string(nil), projection.Completeness.GapCodes...,
			)
			mutate(&drifted)
			driftedPayload, err := json.Marshal(drifted)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseCampaignTerminalProjection(driftedPayload); err == nil {
				t.Fatal("terminal contradiction was accepted")
			}
		})
	}
}

func TestSyntheticTerminalProjectionConformanceIndex(t *testing.T) {
	type fixtureEntry struct {
		Path          string `json:"path"`
		SHA256        string `json:"sha256"`
		Artifact      string `json:"artifact"`
		ExpectedValid bool   `json:"expected_valid"`
	}
	type fixtureIndex struct {
		SchemaVersion    string `json:"schema_version"`
		FixtureAuthority string `json:"fixture_authority"`
		AuthorityChain   struct {
			ProducerCommit                 string `json:"producer_commit"`
			ProducerTree                   string `json:"producer_tree"`
			ProducerConformanceIndexSHA256 string `json:"producer_conformance_index_sha256"`
			CampaignManifestSHA256         string `json:"campaign_manifest_sha256"`
			AuthorizationSHA256            string `json:"authorization_sha256"`
			AuthorizationSignatureSHA256   string `json:"authorization_signature_sha256"`
			SourceBindingSHA256            string `json:"source_binding_sha256"`
			SourceEvidenceTrustSetSHA256   string `json:"source_evidence_trust_set_sha256"`
			XInsightAcknowledgementSHA256  string `json:"xinsight_acknowledgement_sha256"`
			PackageSHA256                  string `json:"package_sha256"`
			PackageSizeBytes               int64  `json:"package_size_bytes"`
			PackageManifestSHA256          string `json:"package_manifest_sha256"`
		} `json:"authority_chain"`
		Fixtures []fixtureEntry `json:"fixtures"`
	}

	root := filepath.Join("testdata", "n2-terminal-projection")
	indexPayload, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index fixtureIndex
	if err := json.Unmarshal(indexPayload, &index); err != nil {
		t.Fatal(err)
	}
	expected := []fixtureEntry{
		{
			Path: "import-receipt.valid.json", SHA256: "sha256:f331b71f5c4db4de1f4674c515d5328af11a12cd18e89a56a3aed7d34afe7946",
			Artifact: "import_receipt", ExpectedValid: true,
		},
		{
			Path: "terminal-projection.invalid-outcome.json", SHA256: "sha256:ab22e9af0fec432a6f3a2588ff7e18d4847250013baf3353af6c53058b4bd0eb",
			Artifact: "terminal_projection", ExpectedValid: false,
		},
		{
			Path: "terminal-projection.valid.json", SHA256: "sha256:13acbcff9bc4b2c5c06880fb713f2e94292b8d493d401b08866bdd845c94a447",
			Artifact: "terminal_projection", ExpectedValid: true,
		},
	}
	if index.SchemaVersion != "xinsight.unirouter-campaign-terminal-projection-conformance-index.v1" ||
		index.FixtureAuthority != "synthetic_conformance_only" ||
		index.AuthorityChain.ProducerCommit != "d0ec4563d0596c614a5e3171d4d592725973328a" ||
		index.AuthorityChain.ProducerTree != "bebc4bb33568236ac987681dd6a55b4837833c48" ||
		index.AuthorityChain.ProducerConformanceIndexSHA256 != "sha256:7289e3e160221fd28f0e30397eb9b37dca4a914f5bfda7a0aca422825b552a2b" ||
		index.AuthorityChain.CampaignManifestSHA256 != "sha256:dbb526eb32c89b1f4771b6be265c28235ead650a1ab51557c48250cb04520a5c" ||
		index.AuthorityChain.AuthorizationSHA256 != "sha256:c05636524e3b5513a01d4705fc70c562b344ba28690aafa12c7c37dc5792d1a3" ||
		index.AuthorityChain.AuthorizationSignatureSHA256 != "sha256:8f550ed24bdf7a9d8b93c298ff45acc8a9bb2c0c53b389bbd25a1ef869b92ec3" ||
		index.AuthorityChain.SourceBindingSHA256 != "sha256:c8959a370ae9955df2d732897a9ed24ffff0a2b1d03844dd9ff3dbb8607f9020" ||
		index.AuthorityChain.SourceEvidenceTrustSetSHA256 != "sha256:9a32b63baca57f9eeef2047c74e52c6a759cc3e336d35abadcc09b4cc30f96d8" ||
		index.AuthorityChain.XInsightAcknowledgementSHA256 != "sha256:1ec9bc8601e8221bff01b343f4784af8cdd84a1a858facbe71edb9ccc65056d6" ||
		index.AuthorityChain.PackageSHA256 != "sha256:42466d9a588503ca9978e4659ceff7a817cdf2f94dd744083f14b0f4e6899c09" ||
		index.AuthorityChain.PackageSizeBytes != 2618 ||
		index.AuthorityChain.PackageManifestSHA256 != "sha256:52f87dc3d666a28662888541225a09440d7a767e443d5432b917084afd69fa81" ||
		!slices.Equal(index.Fixtures, expected) {
		t.Fatalf("fixture index=%+v", index)
	}
	for _, entry := range index.Fixtures {
		payload, err := os.ReadFile(filepath.Join(root, entry.Path))
		if err != nil {
			t.Fatal(err)
		}
		if bytesSHA256(payload) != entry.SHA256 {
			t.Fatalf("fixture digest drifted: %s", entry.Path)
		}
		canonical := bytes.TrimSuffix(payload, []byte("\n"))
		switch entry.Artifact {
		case "import_receipt":
			_, err = unirouteradapter.ParseCampaignPackageImportReceipt(canonical)
		case "terminal_projection":
			_, err = ParseCampaignTerminalProjection(canonical)
		default:
			t.Fatalf("unknown fixture artifact %q", entry.Artifact)
		}
		if (err == nil) != entry.ExpectedValid {
			t.Fatalf("fixture %s validity=%t error=%v", entry.Path, err == nil, err)
		}
	}
}
