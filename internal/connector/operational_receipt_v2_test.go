package connector_test

import (
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/openinference"
	"github.com/foigent/x-insight/internal/connector"
)

func TestBoundedOfflineOperationalReceiptCanonicalRoundTrip(t *testing.T) {
	receipt, err := connector.ProjectBoundedOfflineOperationalReceipt(
		openinference.Binding(),
		connector.SourceNeutralOperationalReceiptInput{
			OperationID:      "operation-001",
			ProducerInstance: "openinference-prod-a",
			Operation:        connector.OperationImportBundle,
			Status:           "completed",
			Counts: connector.ReceiptCounts{
				Received: 1,
				Inserted: 1,
			},
			BundleID:     "bundle-001",
			BundleSHA256: "sha256:" + strings.Repeat("a", 64),
			ObservedAt: time.Date(
				2026, time.August, 1, 2, 3, 4, 5000, time.UTC,
			),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := receipt.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"connector_id":"foigent.openinference.otlp-spans","counts":{"conflict":0,"duplicate_noop":0,"inserted":1,"received":1,"rejected":0},"delivery_mode":"bounded_offline","delivery_state":{"bundle_id":"bundle-001","bundle_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","mode":"bounded_offline"},"observed_at":"2026-08-01T02:03:04.000005Z","operation":"import_bundle","operation_id":"operation-001","producer_instance":"openinference-prod-a","receipt_version":"foigent.operational-receipt.v2","status":"completed"}`
	if string(canonical) != want {
		t.Fatalf("canonical receipt mismatch\n got: %s\nwant: %s", canonical, want)
	}
	parsed, err := connector.ParseSourceNeutralOperationalReceipt(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != receipt {
		t.Fatalf("round-trip receipt mismatch: got=%+v want=%+v", parsed, receipt)
	}
	firstHash, err := receipt.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := parsed.SHA256()
	if err != nil || secondHash != firstHash {
		t.Fatalf("receipt hashes first=%q second=%q err=%v", firstHash, secondHash, err)
	}
}

func TestBoundedOfflineOperationalReceiptEnforcesStatusAndCounts(t *testing.T) {
	base := completedReceiptInput()
	tests := []struct {
		name   string
		mutate func(*connector.SourceNeutralOperationalReceiptInput)
	}{
		{
			name: "count not accounted",
			mutate: func(input *connector.SourceNeutralOperationalReceiptInput) {
				input.Counts.Inserted = 0
			},
		},
		{
			name: "completed conflict",
			mutate: func(input *connector.SourceNeutralOperationalReceiptInput) {
				input.Counts.Inserted = 0
				input.Counts.Conflict = 1
			},
		},
		{
			name: "accepted outcome",
			mutate: func(input *connector.SourceNeutralOperationalReceiptInput) {
				input.Status = "accepted"
			},
		},
		{
			name: "unsafe count",
			mutate: func(input *connector.SourceNeutralOperationalReceiptInput) {
				input.Counts.Received = 9007199254740992
				input.Counts.Inserted = 9007199254740992
			},
		},
		{
			name: "sub-microsecond receipt time",
			mutate: func(input *connector.SourceNeutralOperationalReceiptInput) {
				input.ObservedAt = input.ObservedAt.Add(time.Nanosecond)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			if _, err := connector.ProjectBoundedOfflineOperationalReceipt(
				openinference.Binding(), input,
			); err == nil {
				t.Fatal("invalid receipt was accepted")
			}
		})
	}

	base.Status = "accepted"
	base.Counts.Inserted = 0
	base.Counts.Received = 9007199254740991
	if _, err := connector.ProjectBoundedOfflineOperationalReceipt(
		openinference.Binding(), base,
	); err != nil {
		t.Fatalf("safe-integer accepted receipt was rejected: %v", err)
	}
}

func TestBoundedOfflineOperationalReceiptErrorContract(t *testing.T) {
	input := completedReceiptInput()
	input.Status = "retryable_error"
	input.Counts.Inserted = 0
	input.Counts.Rejected = 1
	input.Error = &connector.SourceNeutralConnectorError{
		ErrorVersion: connector.SourceNeutralErrorVersionV2,
		Category:     connector.ErrorSource,
		Code:         "foigent.openinference.bundle_unavailable",
		Phase:        "import",
		Operation:    connector.OperationImportBundle,
		Retryable:    true,
		SafeMessage:  "OpenInference bundle import can be retried",
		Recovery:     "reimport_bundle",
	}
	if _, err := connector.ProjectBoundedOfflineOperationalReceipt(
		openinference.Binding(), input,
	); err != nil {
		t.Fatalf("valid retryable receipt was rejected: %v", err)
	}
	input.Error.Operation = connector.OperationCommitBatch
	if _, err := connector.ProjectBoundedOfflineOperationalReceipt(
		openinference.Binding(), input,
	); err == nil {
		t.Fatal("mismatched nested error operation was accepted")
	}
	input.Error.Operation = connector.OperationImportBundle
	input.Error.SafeMessage = "cafe\u0301"
	if _, err := connector.ProjectBoundedOfflineOperationalReceipt(
		openinference.Binding(), input,
	); err == nil {
		t.Fatal("non-NFC protocol safe_message was accepted")
	}

	input = completedReceiptInput()
	input.Status = "permanent_error"
	input.Counts.Inserted = 0
	input.Counts.Conflict = 1
	input.Error = &connector.SourceNeutralConnectorError{
		ErrorVersion: connector.SourceNeutralErrorVersionV2,
		Category:     connector.ErrorConflict,
		Code:         "foigent.openinference.identity_conflict",
		Phase:        "commit",
		Operation:    connector.OperationImportBundle,
		Retryable:    false,
		SafeMessage:  "OpenInference bundle conflicts with durable Evidence",
		Recovery:     "operator_repair",
	}
	if _, err := connector.ProjectBoundedOfflineOperationalReceipt(
		openinference.Binding(), input,
	); err != nil {
		t.Fatalf("valid permanent conflict receipt was rejected: %v", err)
	}
}

func TestBoundedOfflineOperationalReceiptPreservesOpaqueNonNFCInputs(t *testing.T) {
	input := completedReceiptInput()
	input.OperationID = "operation-cafe\u0301"
	input.ProducerInstance = "producer-cafe\u0301"
	input.BundleID = "bundle-cafe\u0301"
	receipt, err := connector.ProjectBoundedOfflineOperationalReceipt(
		openinference.Binding(), input,
	)
	if err != nil {
		t.Fatalf("opaque non-NFC input was rejected: %v", err)
	}
	canonical, err := receipt.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(canonical), "cafe\u0301") ||
		strings.Contains(string(canonical), "caf\u00e9") {
		t.Fatalf("opaque source text was normalized: %s", canonical)
	}
	parsed, err := connector.ParseSourceNeutralOperationalReceipt(canonical)
	if err != nil || parsed != receipt {
		t.Fatalf("opaque source text did not round-trip: parsed=%+v err=%v", parsed, err)
	}
}

func TestParseSourceNeutralOperationalReceiptRejectsUnknownAndDuplicateKeys(t *testing.T) {
	unknown := []byte(`{"receipt_version":"foigent.operational-receipt.v2","unknown":true}`)
	if _, err := connector.ParseSourceNeutralOperationalReceipt(unknown); err == nil {
		t.Fatal("unknown receipt member was accepted")
	}
	duplicate := []byte(`{"receipt_version":"foigent.operational-receipt.v2","operation_id":"one","operation_id":"two"}`)
	if _, err := connector.ParseSourceNeutralOperationalReceipt(duplicate); err == nil {
		t.Fatal("duplicate receipt member was accepted")
	}
	nestedDuplicate := []byte(`{"counts":{"received":1,"received":1}}`)
	if _, err := connector.ParseSourceNeutralOperationalReceipt(nestedDuplicate); err == nil {
		t.Fatal("nested duplicate receipt member was accepted")
	}
}

func completedReceiptInput() connector.SourceNeutralOperationalReceiptInput {
	return connector.SourceNeutralOperationalReceiptInput{
		OperationID:      "operation-001",
		ProducerInstance: "openinference-prod-a",
		Operation:        connector.OperationImportBundle,
		Status:           "completed",
		Counts: connector.ReceiptCounts{
			Received: 1,
			Inserted: 1,
		},
		BundleID:     "bundle-001",
		BundleSHA256: "sha256:" + strings.Repeat("a", 64),
		ObservedAt: time.Date(
			2026, time.August, 1, 2, 3, 4, 5000, time.UTC,
		),
	}
}
