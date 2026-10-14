package legacyrelease_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/foigent/x-insight/internal/legacyrelease"
	"github.com/foigent/x-insight/internal/releasebundle"
)

func TestPhase1FinalArtifactMatchesDeclaredLegacySource(t *testing.T) {
	artifact := os.Getenv("XINSIGHT_LEGACY_TEST_ARTIFACT")
	if artifact == "" {
		t.Skip("set XINSIGHT_LEGACY_TEST_ARTIFACT to the exact Phase 1 final artifact")
	}
	configBytes, err := os.ReadFile(filepath.Join("..", "..", "release", "release-config.json"))
	if err != nil {
		t.Fatalf("read release config: %v", err)
	}
	var config releasebundle.ReleaseConfig
	if err := json.Unmarshal(configBytes, &config); err != nil {
		t.Fatalf("decode release config: %v", err)
	}
	var source *releasebundle.LegacySource
	for index := range config.SupportedLegacySources {
		if config.SupportedLegacySources[index].ReleaseID == "legacy-b9e5811-phase1-final" {
			source = &config.SupportedLegacySources[index]
			break
		}
	}
	if source == nil {
		t.Fatal("Phase 1 final legacy source is not declared")
	}
	verified, err := legacyrelease.Verify(artifact, *source)
	if err != nil {
		t.Fatalf("verify exact Phase 1 artifact: %v", err)
	}
	if verified.ArtifactSHA256 != source.ArtifactSHA256 || verified.ManifestSHA256 != source.ManifestSHA256 || verified.Manifest.Commit != source.Commit {
		t.Fatalf("verified identity=%+v source=%+v", verified, *source)
	}
}
