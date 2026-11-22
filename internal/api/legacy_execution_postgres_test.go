package api_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/executionid"
)

func TestLegacyExecutionRoutesRejectRequestIDSharedAcrossSources(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sourceID, restrictedSourceID := randomTestUUID(t), randomTestUUID(t)
	requestID := "req-legacy-" + sourceID
	fixture, err := unirouter.LoadSnapshot(filepath.Join("..", "..", "fixtures", "unirouter_snapshot", "minimal.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.SourceInstance = "legacy-execution-source"
	fixture.RequestIDs = []string{requestID}
	fixture.Scenarios, fixture.TraceEvents, fixture.CaptureGaps = nil, nil, nil
	fixture.CallLogs[0].RequestID = requestID
	fixture.CallLogs[0].SourceRef = ""
	events, err := unirouter.BuildSnapshotEvents(fixture, unirouter.SnapshotImportOptions{SourceSystemID: sourceID})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		hash, err := evidence.GenerateEventHash(event)
		if err != nil {
			t.Fatal(err)
		}
		event["event_hash"] = hash
	}
	if report, err := store.PersistEvents(ctx, events); err != nil || report.Inserted != 1 {
		t.Fatalf("persist legacy fixture: %+v %v", report, err)
	}
	handler := newAuthenticatedHandler(t, store)
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}
	path := "/api/v1/executions/" + requestID
	for _, route := range []struct {
		suffix string
		count  string
		want   int
	}{{"", "event_count", 1}, {"/events", "event_count", 1}, {"/gaps", "gap_count", 0}} {
		response := get(path + route.suffix + "?limit=1")
		var counts map[string]json.RawMessage
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &counts) != nil ||
			string(counts["request_id"]) != `"`+requestID+`"` ||
			string(counts[route.count]) != strconv.Itoa(route.want) {
			t.Fatalf("unique %s: %d %s", route.suffix, response.Code, response.Body.String())
		}
	}

	empty := get(path + "-absent")
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"event_count":0`) ||
		!strings.Contains(empty.Body.String(), `"gap_count":0`) {
		t.Fatalf("empty legacy detail: %d %s", empty.Code, empty.Body.String())
	}

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, `
INSERT INTO source_systems (id, source_key, source_type, display_name)
VALUES ($1, $2, 'unirouter', 'restricted fixture source')`, restrictedSourceID, "legacy:"+restrictedSourceID); err != nil {
		t.Fatal(err)
	}
	// A gap-only source also makes the raw ID ambiguous. Each table alone
	// contains just one source for this ID.
	if _, err := admin.ExecContext(ctx, `
INSERT INTO capture_gaps (source_system_id, adapter_manifest_id, request_id, gap_type, affected_claim, gap_reason)
VALUES ($1, $2, $3, 'missing', 'restricted fixture', 'synthetic gap')`,
		restrictedSourceID, unirouter.DefaultAdapterManifestIDV1, requestID); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "/events", "/gaps"} {
		response := get(path + suffix + "?limit=1")
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"ambiguous_request_id"`) ||
			strings.Contains(response.Body.String(), restrictedSourceID) ||
			strings.Contains(response.Body.String(), "restricted fixture") {
			t.Errorf("ambiguous %s: %d %s", suffix, response.Code, response.Body.String())
		}
	}
	// The importer has a precise source identity and can still verify its own
	// persisted evidence when another source used the same request ID.
	for _, want := range []struct {
		source string
		events int
		gaps   int
	}{{sourceID, 1, 0}, {restrictedSourceID, 0, 1}} {
		result, err := store.ReadSourceRequestEvidence(ctx, want.source, requestID, 1)
		if err != nil || len(result.Events) != want.events || len(result.Gaps) != want.gaps {
			t.Fatalf("source-specific readback counts: events=%d gaps=%d err=%v", len(result.Events), len(result.Gaps), err)
		}
	}
	executionID, err := executionid.New(sourceID, requestID)
	if err != nil {
		t.Fatal(err)
	}
	response := get("/api/v1/executions/" + executionID)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), restrictedSourceID) {
		t.Fatalf("qualified execution: %d %s", response.Code, response.Body.String())
	}
}
