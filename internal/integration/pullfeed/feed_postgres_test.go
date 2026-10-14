package pullfeed_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/investigation"
)

func TestMockConsumerPersistsThenFindsExecutionWithinDiagnosticTarget(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "..", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	store, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	requestID := "req-pull-mock-" + randomHex(t, 6)
	sourceSystemID := randomUUID(t)
	adapterManifestID := randomUUID(t)
	snapshot, err := unirouter.LoadSnapshot(filepath.Join("..", "..", "..", "fixtures", "unirouter_snapshot", "missing_trace_gap.json"))
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	snapshot.SourceInstance = "unirouter-field-mock"
	snapshot.RequestIDs = []string{requestID}
	for index := range snapshot.Scenarios {
		snapshot.Scenarios[index].RequestID = requestID
	}
	for index := range snapshot.CallLogs {
		snapshot.CallLogs[index].RequestID = requestID
		snapshot.CallLogs[index].SourceRef = ""
	}
	for index := range snapshot.TraceEvents {
		snapshot.TraceEvents[index].RequestID = requestID
		snapshot.TraceEvents[index].SourceRef = ""
	}
	for index := range snapshot.CaptureGaps {
		snapshot.CaptureGaps[index].RequestID = requestID
	}
	snapshotBytes, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	terminal := time.Now().UTC().Add(-2 * time.Second)
	snapshotDigest := sha256.Sum256(snapshotBytes)
	feedItem := pullfeed.FeedItem{
		RequestID:           requestID,
		TerminalAvailableAt: terminal,
		Revision:            "1",
		TimelineComplete:    false,
		SnapshotSHA256:      "sha256:" + hex.EncodeToString(snapshotDigest[:]),
		Snapshot:            snapshotBytes,
	}
	payload := pageWithItems(t, terminal, "cursor-search-visible", "watermark-search-visible", false, feedItem)
	source := sourceFunc(func(context.Context, string, int) ([]byte, error) { return payload, nil })
	commitCalls := 0
	committer := pullfeed.CommitFunc(func(ctx context.Context, item pullfeed.FeedItem) (pullfeed.CommitResult, error) {
		commitCalls++
		var decoded unirouter.SnapshotFile
		if err := json.Unmarshal(item.Snapshot, &decoded); err != nil {
			return pullfeed.CommitResult{}, err
		}
		events, err := unirouter.BuildSnapshotEvents(decoded, unirouter.SnapshotImportOptions{
			SourceSystemID:    sourceSystemID,
			AdapterManifestID: adapterManifestID,
		})
		if err != nil {
			return pullfeed.CommitResult{}, err
		}
		for _, event := range events {
			hash, err := evidence.GenerateEventHash(event)
			if err != nil {
				return pullfeed.CommitResult{}, err
			}
			event["event_hash"] = hash
		}
		generatedAt, err := time.Parse(time.RFC3339Nano, decoded.GeneratedAt)
		if err != nil {
			return pullfeed.CommitResult{}, err
		}
		if _, err := store.PersistEventsWithProvenance(ctx, events, investigation.ImportProvenance{
			SourceSystemID:       sourceSystemID,
			AdapterManifestID:    adapterManifestID,
			IntegrationMode:      investigation.IntegrationFixture,
			ProofClass:           investigation.ProofSyntheticFixture,
			AssuranceMode:        investigation.AssuranceSyntheticFixture,
			SourceArtifactSHA256: item.SnapshotSHA256,
			SourceGeneratedAt:    &generatedAt,
			SourceTerminalAt:     &item.TerminalAvailableAt,
			SourceRevision:       item.Revision,
			ImportedRevision:     item.Revision,
			SourceWatermark:      "watermark-search-visible",
			ImportWatermark:      "watermark-search-visible",
			TimelineComplete:     item.TimelineComplete,
		}); err != nil {
			return pullfeed.CommitResult{}, err
		}
		searchVisibleAt := time.Now().UTC()
		results, err := store.SearchExecutions(ctx, investigation.SearchQuery{
			AsOf:           searchVisibleAt.Add(time.Second),
			From:           time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			To:             searchVisibleAt.Add(time.Second),
			RequestID:      requestID,
			SourceSystemID: sourceSystemID,
			Limit:          2,
		})
		if err != nil {
			return pullfeed.CommitResult{}, err
		}
		if len(results) != 1 ||
			results[0].RequestID != requestID ||
			results[0].SourceSystemID != sourceSystemID ||
			results[0].SourceType != "unirouter" ||
			results[0].Freshness.SyncState != investigation.SyncStateGap {
			t.Fatalf("search results=%+v", results)
		}
		freshness := results[0].Freshness
		if freshness.SourceRevision != "1" || freshness.SourceWatermark != "watermark-search-visible" || freshness.TimelineComplete {
			t.Fatalf("freshness=%+v", freshness)
		}
		return pullfeed.CommitResult{SearchVisibleAt: searchVisibleAt}, nil
	})
	consumer, err := pullfeed.NewConsumer(
		pullfeed.Config{Mode: pullfeed.ModeMock, Activation: activation(terminal, "activation-now")},
		source,
		committer,
	)
	if err != nil {
		t.Fatalf("new consumer: %v", err)
	}

	report, err := consumer.Poll(ctx)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if report.ItemsCommitted != 1 || report.IncompleteTimelines != 1 || !pullfeed.MeetsDiagnosticVisibilityTarget(report.VisibilityLatencies) {
		t.Fatalf("report=%+v p95=%s", report, pullfeed.VisibilityP95(report.VisibilityLatencies))
	}
	t.Logf("mock terminal-to-search p95=%s target=%s", pullfeed.VisibilityP95(report.VisibilityLatencies), pullfeed.DiagnosticVisibilityTarget)
	replay, err := consumer.Poll(ctx)
	if err != nil || replay.DuplicateNoop != 1 || replay.ItemsCommitted != 0 || commitCalls != 1 {
		t.Fatalf("replay=%+v commit_calls=%d err=%v", replay, commitCalls, err)
	}
}

func randomUUID(t *testing.T) string {
	t.Helper()
	value := randomHex(t, 16)
	return value[0:8] + "-" + value[8:12] + "-4" + value[13:16] + "-a" + value[17:20] + "-" + value[20:32]
}

func randomHex(t *testing.T, bytes int) string {
	t.Helper()
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		t.Fatalf("random bytes: %v", err)
	}
	return hex.EncodeToString(value)
}
