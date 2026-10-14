package boundedoffline

import (
	"testing"

	"github.com/foigent/x-insight/internal/connector"
)

func TestOperationLockAndBindingValidationAreDeterministic(t *testing.T) {
	connectorID := "foigent.openinference.otlp-spans"
	if first, second := receiptLockKey(connectorID, "operation-001"),
		receiptLockKey(connectorID, "operation-001"); first != second {
		t.Fatalf("receipt lock key changed: %d != %d", first, second)
	}
	if receiptLockKey(connectorID, "operation-001") ==
		receiptLockKey(connectorID, "operation-002") {
		t.Fatal("distinct operation identities shared a lock key")
	}
	if !validSHA256Ref(
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	) {
		t.Fatal("valid request-binding digest was rejected")
	}
	for _, invalid := range []string{
		"",
		"sha256:AAaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"sha256:aaaaaaaa",
		"md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	} {
		if validSHA256Ref(invalid) {
			t.Fatalf("invalid request-binding digest accepted: %q", invalid)
		}
	}
	for _, valid := range []string{
		"foigent.openinference.otlp-spans",
		"foigent.unirouter.execution-evidence",
	} {
		if !validConnectorID(valid) {
			t.Fatalf("valid connector identity rejected: %q", valid)
		}
	}
	for _, invalid := range []string{
		"",
		"openinference",
		"Foigent.openinference",
		"foigent..openinference",
		"foigent.openinference/unsafe",
	} {
		if validConnectorID(invalid) {
			t.Fatalf("invalid connector identity accepted: %q", invalid)
		}
	}
}

func TestStoredReceiptIdentityIsConnectorNeutralAndCompletedOnly(t *testing.T) {
	for _, connectorID := range []string{
		"foigent.openinference.otlp-spans",
		"foigent.unirouter.execution-evidence",
	} {
		receipt := completedReceipt(connectorID)
		if err := validateStoredReceiptIdentity(
			receipt,
			connectorID,
			receipt.OperationID,
		); err != nil {
			t.Fatalf("connector %q receipt rejected: %v", connectorID, err)
		}

		changed := receipt
		changed.Status = "accepted"
		changed.Counts = connector.ReceiptCounts{}
		if err := validateStoredReceiptIdentity(
			changed,
			connectorID,
			changed.OperationID,
		); err == nil {
			t.Fatalf("connector %q accepted receipt became durable", connectorID)
		}

		if err := validateStoredReceiptIdentity(
			receipt,
			connectorID,
			"different-operation",
		); err == nil {
			t.Fatalf("connector %q mismatched operation became durable", connectorID)
		}
	}
}

func TestNewPostgresOperationStoreRejectsNilDatabase(t *testing.T) {
	if _, err := NewPostgresOperationStore(nil); err == nil {
		t.Fatal("nil operational database was accepted")
	}
}

func completedReceipt(connectorID string) connector.SourceNeutralOperationalReceipt {
	return connector.SourceNeutralOperationalReceipt{
		ReceiptVersion:   connector.SourceNeutralReceiptVersionV2,
		OperationID:      "operation-001",
		ConnectorID:      connectorID,
		ProducerInstance: "producer-001",
		DeliveryMode:     connector.DeliveryModeBoundedOffline,
		Operation:        connector.OperationImportBundle,
		Status:           "completed",
		Counts: connector.ReceiptCounts{
			Received: 1,
			Inserted: 1,
		},
		DeliveryState: connector.BoundedOfflineDeliveryState{
			Mode:         connector.DeliveryModeBoundedOffline,
			BundleID:     "bundle-001",
			BundleSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		ObservedAt: "2026-08-10T00:00:00.000000Z",
	}
}
