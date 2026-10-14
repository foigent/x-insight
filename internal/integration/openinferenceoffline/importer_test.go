package openinferenceoffline

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/openinference"
	"github.com/foigent/x-insight/internal/connector"
)

func TestImportBundlePersistsExactReceiptAndReplaysWithoutEvidenceWrite(t *testing.T) {
	payload := referenceBundle(t)
	registry := &recordingRegistry{}
	committer := &recordingCommitter{
		outcomes: []connector.ItemConvergence{connector.ConvergenceInserted},
	}
	receipts := newMemoryReceiptStore()
	importer := newTestImporter(
		t,
		referenceProjector(t),
		registry,
		committer,
		receipts,
	)
	request := referenceRequest()

	first, err := importer.ImportBundle(context.Background(), payload, request)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	second, err := importer.ImportBundle(context.Background(), payload, request)
	if err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if first != second {
		t.Fatalf("replay receipt changed\nfirst=%+v\nsecond=%+v", first, second)
	}
	if first.Status != "completed" || first.Counts.Received != 1 ||
		first.Counts.Inserted != 1 || first.Counts.DuplicateNoop != 0 {
		t.Fatalf("unexpected completed receipt: %+v", first)
	}
	if len(committer.items) != 1 {
		t.Fatalf("exact receipt replay wrote Evidence %d times", len(committer.items))
	}
	if registry.calls != 1 {
		t.Fatalf("exact replay should bypass registry; calls=%d want=1", registry.calls)
	}
	assertReferenceM0Compatibility(t, committer.items[0])
}

func TestImportBundleRejectsOperationReuseAcrossBundleOrScope(t *testing.T) {
	payload := referenceBundle(t)
	committer := &recordingCommitter{
		outcomes: []connector.ItemConvergence{connector.ConvergenceInserted},
	}
	registry := &recordingRegistry{}
	importer := newTestImporter(
		t,
		referenceProjector(t),
		registry,
		committer,
		newMemoryReceiptStore(),
	)
	request := referenceRequest()
	if _, err := importer.ImportBundle(context.Background(), payload, request); err != nil {
		t.Fatalf("seed import: %v", err)
	}

	changedScope := request
	changedScope.ProjectRef = "8"
	if _, err := importer.ImportBundle(
		context.Background(),
		payload,
		changedScope,
	); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("changed scope error=%v", err)
	}

	changedBytes := append(append([]byte(nil), payload...), '\n')
	if _, err := importer.ImportBundle(
		context.Background(),
		changedBytes,
		request,
	); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("changed bundle bytes error=%v", err)
	}
	if _, err := importer.ImportBundle(
		context.Background(),
		[]byte("{"),
		request,
	); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("invalid conflicting bundle should fail on operation identity: %v", err)
	}
	if len(committer.items) != 1 {
		t.Fatalf("conflicting replay reached Evidence commit: %d writes", len(committer.items))
	}
	if registry.calls != 1 {
		t.Fatalf("conflicting replay reached registry: %d calls", registry.calls)
	}
}

func TestImportBundleCrashBeforeReceiptConvergesOnRetry(t *testing.T) {
	committer := &recordingCommitter{
		outcomes: []connector.ItemConvergence{
			connector.ConvergenceInserted,
			connector.ConvergenceDuplicateNoop,
		},
	}
	receipts := newMemoryReceiptStore()
	receipts.failAfterOperation = 1
	importer := newTestImporter(
		t,
		referenceProjector(t),
		&recordingRegistry{},
		committer,
		receipts,
	)
	request := referenceRequest()
	payload := referenceBundle(t)

	if _, err := importer.ImportBundle(
		context.Background(),
		payload,
		request,
	); !errors.Is(err, errInjectedReceiptCommit) {
		t.Fatalf("injected crash error=%v", err)
	}
	if len(receipts.values) != 0 {
		t.Fatal("receipt survived the injected pre-commit crash")
	}
	if len(receipts.bindings) != 1 {
		t.Fatal("operation binding did not survive the injected pre-commit crash")
	}
	changed := request
	changed.BundleID = "different-bundle"
	if _, err := importer.ImportBundle(
		context.Background(),
		payload,
		changed,
	); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("crashed operation accepted a different request: %v", err)
	}
	receipt, err := importer.ImportBundle(context.Background(), payload, request)
	if err != nil {
		t.Fatalf("retry after receipt crash: %v", err)
	}
	if receipt.Counts.Received != 1 || receipt.Counts.Inserted != 0 ||
		receipt.Counts.DuplicateNoop != 1 {
		t.Fatalf("retry did not report duplicate convergence: %+v", receipt)
	}
	if len(committer.items) != 2 || len(receipts.values) != 1 {
		t.Fatalf(
			"retry writes=%d durable receipts=%d",
			len(committer.items),
			len(receipts.values),
		)
	}
}

func TestMutableOperationStateDoesNotChangeM0IdentityHashOrGaps(t *testing.T) {
	clock := &sequenceClock{values: []time.Time{
		time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 1, 1, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 2, 2, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 2, 2, 0, 1, 0, time.UTC),
	}}
	committer := &recordingCommitter{
		outcomes: []connector.ItemConvergence{
			connector.ConvergenceInserted,
			connector.ConvergenceDuplicateNoop,
		},
	}
	importer, err := NewImporter(
		referenceProjector(t),
		&recordingRegistry{},
		committer,
		newMemoryReceiptStore(),
		clock.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	payload := referenceBundle(t)
	firstRequest := referenceRequest()
	first, err := importer.ImportBundle(context.Background(), payload, firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := firstRequest
	secondRequest.OperationID = "operation-002"
	secondRequest.BundleID = "bundle-002"
	second, err := importer.ImportBundle(context.Background(), payload, secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if first.OperationID == second.OperationID ||
		first.DeliveryState.BundleID == second.DeliveryState.BundleID ||
		first.ObservedAt == second.ObservedAt {
		t.Fatalf("test operation state did not vary: first=%+v second=%+v", first, second)
	}
	if len(committer.items) != 2 {
		t.Fatalf("committed item count=%d", len(committer.items))
	}
	firstPairs := eventIdentityHashPairs(committer.items[0])
	secondPairs := eventIdentityHashPairs(committer.items[1])
	if !slices.Equal(firstPairs, secondPairs) {
		t.Fatalf(
			"mutable operation state changed M0 identity/hash\nfirst=%v\nsecond=%v",
			firstPairs,
			secondPairs,
		)
	}
	if committer.items[0].Events[0]["ingested_at"] ==
		committer.items[1].Events[0]["ingested_at"] {
		t.Fatal("test ingestion times did not vary")
	}
	assertReferenceM0Compatibility(t, committer.items[0])
	assertReferenceM0Compatibility(t, committer.items[1])
}

func TestImportOperationBindingIncludesScopeAndProjectionIdentity(t *testing.T) {
	binding := openinference.Binding()
	request := referenceRequest()
	bundleSHA := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	base := bindImportOperation(binding, request, bundleSHA)

	emptyTenant := ""
	request.TenantRef = &emptyTenant
	withTenantPresence := bindImportOperation(binding, request, bundleSHA)
	if base.RequestSHA256 == withTenantPresence.RequestSHA256 {
		t.Fatal("tenant_ref presence was not bound")
	}

	request = referenceRequest()
	binding.Mappings.EventToM0.SHA256 =
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	changedProjection := bindImportOperation(binding, request, bundleSHA)
	if base.RequestSHA256 == changedProjection.RequestSHA256 {
		t.Fatal("projection identity was not bound")
	}
}
