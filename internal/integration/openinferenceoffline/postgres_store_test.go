package openinferenceoffline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/integration/boundedoffline"
)

func TestOperationalReceiptMigrationPreservesOfflineAndImmutableBoundary(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	up, err := os.ReadFile(filepath.Join(
		root,
		"migrations",
		"000019_create_connector_operational_receipts.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile(filepath.Join(
		root,
		"migrations",
		"000019_create_connector_operational_receipts.down.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	text := string(up)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS connector_import_operation_bindings",
		"PRIMARY KEY (connector_id, operation_id)",
		"CREATE TABLE IF NOT EXISTS connector_operational_receipts",
		"connector_operational_receipts_operation_binding_fk",
		"REFERENCES connector_import_operation_bindings",
		"request_binding_sha256 text NOT NULL",
		"request_binding_sha256 ~ '^sha256:[a-f0-9]{64}$'",
		"delivery_mode = 'bounded_offline'",
		"operation IN ('import_bundle', 'commit_batch')",
		"status = 'completed'",
		"received = inserted + duplicate_noop",
		"conflict = 0",
		"rejected = 0",
		"canonical_receipt bytea NOT NULL",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration omitted %q", required)
		}
	}
	for _, forbidden := range []string{
		"cursor_value",
		"high_watermark",
		"checkpoint_before",
		"checkpoint_after",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("bounded-offline receipt persisted %q", forbidden)
		}
	}
	if count := strings.Count(
		text,
		"PRIMARY KEY (connector_id, operation_id)",
	); count != 2 {
		t.Fatalf("operation binding and completed receipt require two immutable identities; got %d", count)
	}
	if !strings.Contains(
		string(down),
		"cannot remove Connector import operation evidence while rows exist",
	) {
		t.Fatal("down migration does not fail closed over durable receipts")
	}
}

func TestOperationStoreCompatibilityAliasesRemainExact(t *testing.T) {
	var generic boundedoffline.OperationBinding = OperationBinding{
		ConnectorID:   "foigent.openinference.otlp-spans",
		OperationID:   "operation-001",
		RequestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	var legacy OperationBinding = generic
	if legacy != generic {
		t.Fatalf("OpenInference OperationBinding alias changed: %+v", legacy)
	}
	if !errors.Is(ErrOperationConflict, boundedoffline.ErrOperationConflict) ||
		!errors.Is(boundedoffline.ErrOperationConflict, ErrOperationConflict) {
		t.Fatal("OpenInference operation-conflict error identity changed")
	}
}
