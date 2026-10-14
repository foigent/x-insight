package openinferenceoffline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/openinference"
	"github.com/foigent/x-insight/internal/connector"
)

var errInjectedReceiptCommit = errors.New("injected receipt commit failure")

type recordingRegistry struct {
	calls int
}

func (registry *recordingRegistry) EnsureRegistry(
	_ context.Context,
	_ string,
) error {
	registry.calls++
	return nil
}

type recordingCommitter struct {
	outcomes []connector.ItemConvergence
	items    []connector.NormalizedItem
}

func (committer *recordingCommitter) CommitItem(
	_ context.Context,
	item connector.NormalizedItem,
) (connector.ItemConvergence, *connector.ConnectorError) {
	committer.items = append(committer.items, item)
	index := len(committer.items) - 1
	if index >= len(committer.outcomes) {
		return "", connector.NewConnectorError(
			connector.ErrorInternal,
			"foigent.openinference.test_outcome_missing",
			connector.PhaseCommit,
			"commit_item",
			false,
			"Test convergence was not configured",
			connector.RecoveryOperatorRepair,
		)
	}
	return committer.outcomes[index], nil
}

type storedReceipt struct {
	binding OperationBinding
	receipt connector.SourceNeutralOperationalReceipt
}

type memoryReceiptStore struct {
	bindings           map[string]OperationBinding
	values             map[string]storedReceipt
	failAfterOperation int
}

func newMemoryReceiptStore() *memoryReceiptStore {
	return &memoryReceiptStore{
		bindings: make(map[string]OperationBinding),
		values:   make(map[string]storedReceipt),
	}
}

func (store *memoryReceiptStore) WithOperation(
	ctx context.Context,
	binding OperationBinding,
	operation func(
		context.Context,
		*connector.SourceNeutralOperationalReceipt,
	) (connector.SourceNeutralOperationalReceipt, error),
) (connector.SourceNeutralOperationalReceipt, error) {
	key := binding.ConnectorID + "\x00" + binding.OperationID
	claimed, claimedExists := store.bindings[key]
	if claimedExists && claimed != binding {
		return connector.SourceNeutralOperationalReceipt{}, ErrOperationConflict
	}
	store.bindings[key] = binding
	stored, exists := store.values[key]
	if exists && stored.binding != binding {
		return connector.SourceNeutralOperationalReceipt{}, ErrOperationConflict
	}
	var existing *connector.SourceNeutralOperationalReceipt
	if exists {
		receipt := stored.receipt
		existing = &receipt
	}
	result, err := operation(ctx, existing)
	if err != nil {
		return connector.SourceNeutralOperationalReceipt{}, err
	}
	if store.failAfterOperation > 0 {
		store.failAfterOperation--
		return connector.SourceNeutralOperationalReceipt{}, errInjectedReceiptCommit
	}
	if exists && result != stored.receipt {
		return connector.SourceNeutralOperationalReceipt{}, ErrOperationConflict
	}
	store.values[key] = storedReceipt{binding: binding, receipt: result}
	return result, nil
}

type sequenceClock struct {
	values []time.Time
	index  int
}

func (clock *sequenceClock) Now() time.Time {
	if clock.index >= len(clock.values) {
		panic("test clock exhausted")
	}
	value := clock.values[clock.index]
	clock.index++
	return value
}

func newTestImporter(
	t *testing.T,
	projector ItemProjector,
	registry RegistryPort,
	committer connector.EvidenceCommitPort,
	receipts ImportOperationStore,
) *Importer {
	t.Helper()
	clock := &sequenceClock{values: []time.Time{
		time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 1, 1, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 1, 1, 0, 2, 0, time.UTC),
		time.Date(2026, 8, 1, 1, 0, 3, 0, time.UTC),
		time.Date(2026, 8, 1, 1, 0, 4, 0, time.UTC),
		time.Date(2026, 8, 1, 1, 0, 5, 0, time.UTC),
	}}
	importer, err := NewImporter(projector, registry, committer, receipts, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	return importer
}

func referenceProjector(t *testing.T) *openinference.Projector {
	t.Helper()
	projector, err := openinference.NewProjector()
	if err != nil {
		t.Fatal(err)
	}
	return projector
}

func referenceBundle(t *testing.T) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "contracts", "openinference",
		"reference-otlp-export.v1.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func referenceRequest() ImportRequest {
	return ImportRequest{
		OperationID:      "operation-001",
		BundleID:         "bundle-001",
		ProducerInstance: "otel-collector-prod-a",
		OrgRef:           "1",
		ProjectRef:       "7",
	}
}

func eventIdentityHashPairs(item connector.NormalizedItem) []string {
	pairs := make([]string, 0, len(item.Events))
	for _, event := range item.Events {
		pairs = append(pairs, event["id"].(string)+" "+event["event_hash"].(string))
	}
	return pairs
}

func assertReferenceM0Compatibility(
	t *testing.T,
	item connector.NormalizedItem,
) {
	t.Helper()
	want := []string{
		"c70b878d-21a0-57fa-90c7-92c989907e37 sha256:dba2c158b78533ab174ece5bae4b7298968b3e9fa2d398d3ab6a2af134193a46",
		"3b54f6a2-f303-55d7-8a5c-8808a9a61ed3 sha256:62e94aba0f167b21deaa8d87def230a0df7b938b81ebcb398e20a757e5a8cfba",
		"d295a54e-bd67-572f-aadf-7b7ae3e2abba sha256:4e3f1e8964aa1119450af0c7842fc724be836efcadadf9258f6349d9df164116",
		"daa55d31-a996-53c9-8454-6851cf246cb1 sha256:a164079825e5ae1d5a70ef0b543f2766ddbd5c8069ac4d4bc4375b5b6cecafc0",
		"264babaf-3080-5fea-b391-d1c22cd50c33 sha256:00eac75af3083ddcad31c1d0f4abd217dd8d4bcf614dafd4d6174e61f958cd67",
	}
	if got := eventIdentityHashPairs(item); !slices.Equal(got, want) {
		t.Fatalf("OpenInference M0 compatibility changed\n got=%v\nwant=%v", got, want)
	}
	gapCount := 0
	for _, event := range item.Events {
		if event["event_type"] == "gap.capture.detected" {
			gapCount++
		}
	}
	if gapCount != 4 {
		t.Fatalf("Capture Gap rows=%d want=4", gapCount)
	}
}
