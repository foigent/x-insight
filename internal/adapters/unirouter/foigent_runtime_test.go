package unirouter

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
)

func TestCompletedExecutionReferenceRuntimeBindingMatchesEditableRegistry(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "contracts", "foigent",
		"xinsight-projection-binding.v1.json",
	))
	if err != nil {
		t.Fatalf("read projection binding: %v", err)
	}
	expected, err := connector.ParseManifestBinding(payload)
	if err != nil {
		t.Fatalf("parse projection binding: %v", err)
	}
	if err := validateCompletedExecutionReferenceBinding(expected); err != nil {
		t.Fatalf("validate exact completed-execution registration: %v", err)
	}
	projector, err := NewCompletedExecutionReferenceProjector()
	if err != nil {
		t.Fatalf("new Reference projector: %v", err)
	}
	if actual := projector.Binding(); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("compiled binding drifted from editable registry")
	}
}

func TestCompletedExecutionReferenceRuntimePreservesLegacyProjection(t *testing.T) {
	projector, err := NewCompletedExecutionReferenceProjector()
	if err != nil {
		t.Fatalf("new Reference projector: %v", err)
	}
	for _, fixture := range []string{"valid-settled.json", "valid-cancelled.json"} {
		t.Run(fixture, func(t *testing.T) {
			source := readCompletedExecutionContractFile(t, "fixtures/"+fixture)
			options := CompletedExecutionMapOptions{
				SourceInstance: "unirouter-development",
				ObservedAt:     time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC),
				IngestedAt:     time.Date(2026, 7, 28, 10, 0, 1, 0, time.UTC),
			}
			legacy, err := MapCompletedExecutionV1(source, options)
			if err != nil {
				t.Fatalf("map legacy item: %v", err)
			}
			neutral, connectorErr := projector.Project(context.Background(), source, options)
			if connectorErr != nil {
				t.Fatalf("project Reference item: %v", connectorErr)
			}
			terminalAt := legacy.TerminalAvailableAt.UTC()
			if !reflect.DeepEqual(legacy.Events, neutral.Events) ||
				neutral.Provenance.SourcePayloadSHA256 != legacy.SnapshotSHA256 ||
				neutral.Provenance.SourceTerminalAt == nil ||
				!neutral.Provenance.SourceTerminalAt.Equal(terminalAt) ||
				neutral.Provenance.SourceWatermark != "" ||
				neutral.Provenance.ImportWatermark != "" {
				t.Fatalf("legacy/Reference projection drift")
			}
		})
	}
}
