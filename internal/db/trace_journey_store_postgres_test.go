package db_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/openinference"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/integration/openinferenceoffline"
	"github.com/foigent/x-insight/internal/investigation"
)

func TestTraceJourneyCrossesExecutionBoundaryWithoutChangingM0Identity(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	store, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	registryDatabase, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open registry database: %v", err)
	}
	defer registryDatabase.Close()
	registry, err := openinferenceoffline.NewPostgresStore(registryDatabase)
	if err != nil {
		t.Fatalf("open OpenInference registry: %v", err)
	}
	if err := registry.EnsureRegistry(ctx, "xinsight-trace-journey-postgres-test"); err != nil {
		t.Fatalf("ensure OpenInference registry: %v", err)
	}

	sourceID := openinference.SourceSystemID
	adapterID := openinference.AdapterManifestID
	testRunID := testUUID(t)
	traceID := strings.ReplaceAll(testRunID, "-", "")
	baseTime := time.Date(2026, 8, 2, 2, 0, 0, 0, time.UTC)
	requests := []string{"trace-root-" + testRunID[:8], "trace-middle-" + testRunID[:8], "trace-leaf-" + testRunID[:8]}
	spanIDs := []string{"1111111111111111", "2222222222222222", "3333333333333333"}
	parents := []string{"", spanIDs[0], spanIDs[1]}
	services := []string{"agent-orchestrator", "policy-router", "model-provider"}
	events := make([]map[string]any, 0, 3)
	expectedHashes := make(map[string]string, 3)
	for index := range requests {
		event := investigationTestEvent(
			sourceID,
			adapterID,
			requests[index],
			testUUID(t),
			spanIDs[index],
			parents[index],
			baseTime.Add(time.Duration(index)*time.Second),
			baseTime.Add(time.Duration(index)*time.Second),
		)
		event["source_type"] = "openinference"
		event["source_version"] = openinference.SourceSchemaV1
		event["source_ref"] = "openinference:test:" + spanIDs[index]
		event["source_event_type"] = "openinference_llm_span"
		event["event_type"] = "model.request.succeeded"
		event["stage"] = "provider"
		event["capture_mode"] = "openinference"
		event["trace_id"] = traceID
		event["org_ref"] = "org-1"
		event["project_ref"] = "project-7"
		correlation := event["correlation"].(map[string]any)
		correlation["trace_id"] = traceID
		event["identity"] = map[string]any{
			"actor_type": "human",
			"actor_ref":  "study-user",
			"org_id":     "org-1",
			"project_id": "project-7",
		}
		event["capability"] = map[string]any{
			"capability_type": "model_call",
			"target_ref":      "stage-" + services[index],
			"model":           "study-model",
			"extensions": map[string]any{
				"provider":     "study-provider",
				"service_name": services[index],
			},
		}
		hash, hashErr := evidence.GenerateEventHash(event)
		if hashErr != nil {
			t.Fatalf("hash event %d: %v", index, hashErr)
		}
		event["event_hash"] = hash
		expectedHashes[event["id"].(string)] = hash
		events = append(events, event)
	}
	for index, event := range events {
		terminalTime := baseTime.Add(time.Duration(index) * time.Second)
		sourcePayloadHash := sha256.Sum256([]byte(
			"trace-journey-source-payload:" + traceID + ":" + spanIDs[index],
		))
		if _, err := store.PersistEventsWithProvenance(ctx, []map[string]any{event}, investigation.ImportProvenance{
			SourceSystemID:      sourceID,
			AdapterManifestID:   adapterID,
			IntegrationMode:     investigation.IntegrationBoundedOffline,
			ProofClass:          investigation.ProofOperatorApproved,
			AssuranceMode:       investigation.AssuranceBoundedOffline,
			SourcePayloadSHA256: fmt.Sprintf("sha256:%x", sourcePayloadHash),
			SourceTerminalAt:    &terminalTime,
			SourceRevision:      "1",
			ImportedRevision:    "1",
			TimelineComplete:    false,
		}); err != nil {
			t.Fatalf("persist trace event %d: %v", index, err)
		}
	}
	anchorExecutionID, err := executionid.New(sourceID, requests[1])
	if err != nil {
		t.Fatalf("anchor execution ID: %v", err)
	}
	journey, err := store.GetTraceJourney(ctx, anchorExecutionID, time.Now().UTC(), 200)
	if err != nil {
		t.Fatalf("get trace journey: %v", err)
	}
	if !journey.Available || journey.AnchorExecutionID != anchorExecutionID ||
		journey.TraceID != traceID || len(journey.Nodes) != 3 || len(journey.Edges) != 2 {
		t.Fatalf("journey=%+v", journey)
	}
	if journey.Integrity.ExecutionCount != 3 ||
		!journey.Integrity.VisibleParentResolutionComplete ||
		journey.Integrity.SourceTimelineComplete {
		t.Fatalf("integrity=%+v", journey.Integrity)
	}
	if journey.Nodes[0].ExecutionID == journey.Nodes[1].ExecutionID ||
		journey.Nodes[1].ExecutionID == journey.Nodes[2].ExecutionID {
		t.Fatalf("trace journey collapsed distinct execution identities: %+v", journey.Nodes)
	}
	for _, node := range journey.Nodes {
		if expectedHashes[node.EventID] != node.EventHash {
			t.Fatalf("trace journey changed M0 identity for %s: %s", node.EventID, node.EventHash)
		}
	}
}
