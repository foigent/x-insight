package litellm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/connector"
)

func TestBindingMatchesStrictExternalRegistryMirror(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "contracts", "foigent",
		"xinsight-litellm-projection-binding.v1.json",
	))
	if err != nil {
		t.Fatalf("read LiteLLM projection binding: %v", err)
	}
	parsed, err := connector.ParseManifestBinding(payload)
	if err != nil {
		t.Fatalf("strictly parse LiteLLM projection binding: %v", err)
	}
	if err := validateBinding(parsed); err != nil {
		t.Fatalf("validate exact LiteLLM registration: %v", err)
	}
	if parsed != Binding() {
		t.Fatal("compiled LiteLLM binding drifted from the external registry mirror")
	}
}

func TestBindingRejectsStructurallyValidRegistrationDrift(t *testing.T) {
	mutated := Binding()
	mutated.Protocol.ReleaseDigest = "sha256:" + strings.Repeat("0", 64)
	if err := mutated.Validate(); err != nil {
		t.Fatalf("test mutation is not structurally valid: %v", err)
	}
	if err := validateBinding(mutated); err == nil {
		t.Fatal("structurally valid LiteLLM registration drift was accepted")
	}
}
