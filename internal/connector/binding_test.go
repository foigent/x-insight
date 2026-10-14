package connector

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestManifestBindingParserIsStrictAndStructural(t *testing.T) {
	expected := sampleManifestBinding()
	payload, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("marshal binding: %v", err)
	}
	parsed, err := ParseManifestBinding(payload)
	if err != nil {
		t.Fatalf("parse binding: %v", err)
	}
	if parsed != expected {
		t.Fatal("structurally valid binding changed during parsing")
	}

	duplicate := append([]byte(`{"binding_version":"xinsight.foigent-projection-binding.v1",`), bytes.TrimSpace(payload)[1:]...)
	if _, err := ParseManifestBinding(duplicate); err == nil ||
		!strings.Contains(err.Error(), "duplicate field") {
		t.Fatalf("duplicate binding error=%v", err)
	}

	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatalf("decode binding: %v", err)
	}
	value["unexpected"] = true
	unknown, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal binding: %v", err)
	}
	if _, err := ParseManifestBinding(unknown); err == nil {
		t.Fatal("binding with unknown field accepted")
	}

	delete(value, "unexpected")
	value["protocol"].(map[string]any)["release_digest"] = "sha256:" + strings.Repeat("0", 64)
	mutated, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal binding mutation: %v", err)
	}
	structurallyValid, err := ParseManifestBinding(mutated)
	if err != nil {
		t.Fatalf("structurally valid registration rejected: %v", err)
	}
	if err := ValidateExactRegistration(structurallyValid, expected); err == nil {
		t.Fatal("altered registration accepted as the injected exact binding")
	}

	value["protocol"].(map[string]any)["release_digest"] = "sha256:invalid"
	invalid, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal invalid binding: %v", err)
	}
	if _, err := ParseManifestBinding(invalid); err == nil {
		t.Fatal("structurally invalid registration accepted")
	}
}

func TestExactRegistrationSeparatesNeutralShapeFromProductRegistry(t *testing.T) {
	expected := sampleManifestBinding()
	if err := ValidateExactRegistration(expected, expected); err != nil {
		t.Fatalf("exact injected registration rejected: %v", err)
	}
	binding := expected
	binding.ReferenceManifest = ArtifactPin{
		Path:   "manifests/foigent.other.completed-1.0.0.json",
		SHA256: "sha256:" + strings.Repeat("1", 64),
	}
	binding.Connector = PortableConnectorBinding{
		ConnectorID:        "foigent.other.completed",
		ConnectorVersion:   "1.0.0",
		SourceProduct:      "other",
		EventType:          "model.request.completed",
		SchemaRef:          "foigent://events/model/request-completed/v1",
		SchemaVersion:      "1",
		SourceSchema:       "other.completed.v1",
		SourceSchemaSHA256: "sha256:" + strings.Repeat("2", 64),
	}
	binding.Projection = ProjectionBinding{
		ProfileID:         "xinsight.foigent-envelope-to-m0.other-completed.v1",
		SourceSystemID:    "33333333-3333-4333-8333-333333333333",
		AdapterManifestID: "44444444-4444-4444-8444-444444444444",
		CursorKey:         "other.completed.v1",
	}
	if err := binding.Validate(); err != nil {
		t.Fatalf("source-neutral binding shape rejected: %v", err)
	}
	if err := ValidateExactRegistration(binding, expected); err == nil {
		t.Fatal("unregistered structurally valid profile accepted as exact registration")
	}
}

func TestPhase2BNeutralKernelDependencyBoundary(t *testing.T) {
	blocked := []string{
		"github.com/foigent/x-insight/cmd/",
		"github.com/foigent/x-insight/internal/adapters/",
		"github.com/foigent/x-insight/internal/api",
		"github.com/foigent/x-insight/internal/config",
		"github.com/foigent/x-insight/internal/db",
		"github.com/foigent/x-insight/internal/integration/",
		"github.com/foigent/x-insight/internal/middleware",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read connector package: %v", err)
	}
	fileset := token.NewFileSet()
	forbiddenRegistrations := []string{
		"unirouter",
		"litellm",
		"completed-execution",
		"completed_execution",
		"spend-log",
		"spend_log",
	}
	for _, entry := range entries {
		if entry.IsDir() ||
			!strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileset, entry.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		source, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		lowerSource := strings.ToLower(string(source))
		for _, registration := range forbiddenRegistrations {
			if strings.Contains(lowerSource, registration) {
				t.Errorf("%s contains product registration name %q", entry.Name(), registration)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			importSpec, ok := node.(*ast.ImportSpec)
			if !ok {
				return true
			}
			path, unquoteErr := strconv.Unquote(importSpec.Path.Value)
			if unquoteErr != nil {
				t.Errorf("%s import: %v", entry.Name(), unquoteErr)
				return false
			}
			for _, prefix := range blocked {
				if strings.HasPrefix(path, prefix) {
					t.Errorf("%s imports blocked runtime package %s", entry.Name(), path)
				}
			}
			return false
		})
	}
}

func sampleManifestBinding() ManifestBinding {
	digest := "sha256:" + strings.Repeat("1", 64)
	return ManifestBinding{
		BindingVersion: ProjectionBindingVersionV1,
		Protocol: ProtocolBinding{
			Version:       "1.2.3",
			Tag:           "v1.2.3",
			Commit:        strings.Repeat("a", 40),
			ReleaseDigest: digest,
		},
		Schemas: SchemaBindings{
			EventEnvelope:         ArtifactPin{Path: "schemas/event-envelope.v1.json", SHA256: digest},
			ModelRequestCompleted: ArtifactPin{Path: "schemas/model-request-completed.v1.json", SHA256: digest},
			ConnectorManifest:     ArtifactPin{Path: "schemas/connector-manifest.v1.json", SHA256: digest},
			ConnectorLifecycle:    ArtifactPin{Path: "schemas/connector-lifecycle.v1.json", SHA256: digest},
			EventBatch:            ArtifactPin{Path: "schemas/event-batch.v1.json", SHA256: digest},
			Checkpoint:            ArtifactPin{Path: "schemas/checkpoint.v1.json", SHA256: digest},
			OperationalReceipt:    ArtifactPin{Path: "schemas/operational-receipt.v1.json", SHA256: digest},
			ConnectorError:        ArtifactPin{Path: "schemas/connector-error.v1.json", SHA256: digest},
		},
		ReferenceManifest: ArtifactPin{
			Path:   "manifests/foigent.source.completed-1.0.0.json",
			SHA256: digest,
		},
		Compatibility: CompatibilityBindings{
			ProjectionContractSHA256:  digest,
			LifecycleMappingSHA256:    digest,
			IdentityMappingSHA256:     digest,
			ReferenceVectorSHA256:     digest,
			CanonicalVectorSHA256:     digest,
			ConformanceContractSHA256: digest,
		},
		Connector: PortableConnectorBinding{
			ConnectorID:        "foigent.source.completed",
			ConnectorVersion:   "1.0.0",
			SourceProduct:      "source",
			EventType:          "model.request.completed",
			SchemaRef:          "foigent://events/model/request-completed/v1",
			SchemaVersion:      "1",
			SourceSchema:       "source.completed.v1",
			SourceSchemaSHA256: digest,
		},
		Projection: ProjectionBinding{
			ProfileID:         "xinsight.foigent-envelope-to-m0.source-completed.v1",
			SourceSystemID:    "33333333-3333-4333-8333-333333333333",
			AdapterManifestID: "44444444-4444-4444-8444-444444444444",
			CursorKey:         "source.completed.v1",
		},
	}
}
