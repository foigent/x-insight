package api_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/investigation"
)

func TestGeneratedV2SnapshotPersistsReplaysAndReadsBackThroughAPI(t *testing.T) {
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

	sourceSystemID := randomTestUUID(t)
	requestID := "req-xi-v2-micro-7-synthetic"
	v1, err := unirouter.LoadSnapshot(filepath.Join("..", "..", "fixtures", "unirouter_snapshot", "minimal.json"))
	if err != nil {
		t.Fatalf("load v1 snapshot: %v", err)
	}
	v1.SourceInstance = "local-synthetic-v2"
	v1.RequestIDs = []string{requestID}
	v1.Scenarios = nil
	v1.TraceEvents = nil
	v1.CaptureGaps = nil
	v1.CallLogs[0].RequestID = requestID
	v1.CallLogs[0].SourceRef = "unirouter:local-synthetic-v2:call_logs:1"
	v1Events, err := unirouter.BuildSnapshotEvents(v1, unirouter.SnapshotImportOptions{SourceSystemID: sourceSystemID})
	if err != nil {
		t.Fatalf("build v1 events: %v", err)
	}
	for _, event := range v1Events {
		hash, hashErr := evidence.GenerateEventHash(event)
		if hashErr != nil {
			t.Fatalf("hash v1 event: %v", hashErr)
		}
		event["event_hash"] = hash
	}
	v1Provenance := syntheticProvenance(sourceSystemID, unirouter.DefaultAdapterManifestIDV1, unirouter.SnapshotSchemaV1, "sha256:"+strings.Repeat("1", 64))
	firstV1, err := store.PersistEventsWithProvenance(ctx, v1Events, v1Provenance)
	if err != nil || firstV1.Inserted != 1 || firstV1.IdempotencyConflict != 0 {
		t.Fatalf("first v1 persist=%+v err=%v", firstV1, err)
	}
	replayV1, err := store.PersistEventsWithProvenance(ctx, v1Events, v1Provenance)
	if err != nil || replayV1.DuplicateNoop != 1 || replayV1.IdempotencyConflict != 0 {
		t.Fatalf("replay v1 persist=%+v err=%v", replayV1, err)
	}

	v2Report, err := unirouter.ImportSnapshot(
		filepath.Join("..", "..", "fixtures", "unirouter_snapshot", "micro_money_v2_generated.json"),
		filepath.Join("..", "..", "schemas", "m0", "evidence-event.schema.json"),
		unirouter.SnapshotImportOptions{SourceSystemID: sourceSystemID, EmitEvents: true, RerunCheck: true},
	)
	if err != nil {
		t.Fatalf("import generated v2 snapshot: %v", err)
	}
	if v2Report.AdapterManifestID != unirouter.DefaultAdapterManifestIDV2 || v2Report.ArtifactSHA256 != "sha256:93bc4c8448f1f838668f9dab10cbba070f0df550665b16d73bb494066fe8d6a1" {
		t.Fatalf("v2 report identity=%+v", v2Report)
	}
	v2Provenance := syntheticProvenance(sourceSystemID, v2Report.AdapterManifestID, v2Report.SnapshotSchema, v2Report.ArtifactSHA256)
	firstV2, err := store.PersistEventsWithProvenance(ctx, v2Report.Events, v2Provenance)
	if err != nil || firstV2.Inserted != 2 || firstV2.CaptureGapsInserted != 1 || firstV2.IdempotencyConflict != 0 {
		t.Fatalf("first v2 persist=%+v err=%v", firstV2, err)
	}
	replayV2, err := store.PersistEventsWithProvenance(ctx, v2Report.Events, v2Provenance)
	if err != nil || replayV2.DuplicateNoop != 2 || replayV2.IdempotencyConflict != 0 {
		t.Fatalf("replay v2 persist=%+v err=%v", replayV2, err)
	}

	assertMapperRegistryUpgrade(t, ctx, dsn, sourceSystemID)
	lateV1Replay, err := store.PersistEventsWithProvenance(ctx, v1Events, v1Provenance)
	if err != nil || lateV1Replay.DuplicateNoop != 1 || lateV1Replay.IdempotencyConflict != 0 {
		t.Fatalf("late v1 replay persist=%+v err=%v", lateV1Replay, err)
	}
	assertMapperRegistryUpgrade(t, ctx, dsn, sourceSystemID)

	versionless := variantEvent(t, v1Events[0], "")
	versionlessPersist, err := store.PersistEvents(ctx, []map[string]any{versionless})
	if err != nil || versionlessPersist.Inserted != 1 || versionlessPersist.IdempotencyConflict != 0 {
		t.Fatalf("versionless historical persist=%+v err=%v", versionlessPersist, err)
	}
	assertMapperRegistryUpgrade(t, ctx, dsn, sourceSystemID)

	unknownTransition := variantEvent(t, v1Events[0], "unirouter.prd5.xinsight.snapshot.v3")
	if _, err := store.PersistEvents(ctx, []map[string]any{unknownTransition}); err == nil || !strings.Contains(err.Error(), "source-version transition") {
		t.Fatalf("unknown source-version transition err=%v", err)
	}
	assertMapperRegistryUpgrade(t, ctx, dsn, sourceSystemID)

	conflictingVersion := make(map[string]any, len(v2Report.Events[0]))
	for key, value := range v2Report.Events[0] {
		conflictingVersion[key] = value
	}
	conflictingVersion["source_version"] = unirouter.SnapshotSchemaV1
	if _, err := store.PersistEvents(ctx, []map[string]any{conflictingVersion}); err == nil || !strings.Contains(err.Error(), "reserved UniRouter mapper version") {
		t.Fatalf("known manifest-version mismatch err=%v", err)
	}

	handler := newAuthenticatedHandler(t, store)
	searchFrom, searchTo := mappedEventTimeRange(t, append(append([]map[string]any{}, v1Events...), v2Report.Events...))
	searchPath := "/api/v1/executions?request_id=" + url.QueryEscape(requestID) +
		"&source_system_id=" + url.QueryEscape(sourceSystemID) +
		"&from=" + url.QueryEscape(searchFrom.Format(time.RFC3339Nano)) +
		"&to=" + url.QueryEscape(searchTo.Format(time.RFC3339Nano))
	search := httptest.NewRecorder()
	handler.ServeHTTP(search, httptest.NewRequest(http.MethodGet, searchPath, nil))
	if search.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", search.Code, search.Body.String())
	}
	var searchResponse struct {
		Items []investigation.ExecutionSummary
		Page  map[string]json.RawMessage
	}
	decodePostgresResponse(t, search, &searchResponse)
	if len(searchResponse.Items) != 1 || searchResponse.Items[0].EventCount != 3 || searchResponse.Items[0].GapCount != 1 {
		t.Fatalf("search response=%+v", searchResponse)
	}
	var asOfTime time.Time
	if err := json.Unmarshal(searchResponse.Page["as_of"], &asOfTime); err != nil {
		t.Fatalf("decode search as_of: %v", err)
	}
	executionID := searchResponse.Items[0].ExecutionID
	asOf := url.QueryEscape(asOfTime.UTC().Format(time.RFC3339Nano))
	timeline := httptest.NewRecorder()
	handler.ServeHTTP(timeline, httptest.NewRequest(http.MethodGet, "/api/v1/executions/"+executionID+"/timeline?limit=10&as_of="+asOf, nil))
	if timeline.Code != http.StatusOK {
		t.Fatalf("timeline status=%d body=%s", timeline.Code, timeline.Body.String())
	}
	var timelineResponse struct {
		Items []investigation.TimelineNode
	}
	decodePostgresResponse(t, timeline, &timelineResponse)
	foundV2 := false
	for _, node := range timelineResponse.Items {
		if node.EventType != "usage.tokens.recorded" {
			continue
		}
		event := httptest.NewRecorder()
		path := "/api/v1/executions/" + executionID + "/events/" + node.EventID + "?as_of=" + asOf
		handler.ServeHTTP(event, httptest.NewRequest(http.MethodGet, path, nil))
		if event.Code != http.StatusOK {
			t.Fatalf("event status=%d body=%s", event.Code, event.Body.String())
		}
		var detail investigation.EvidenceEventDetail
		decodePostgresResponse(t, event, &detail)
		if detail.Usage.CostMicros == "" {
			continue
		}
		foundV2 = true
		if detail.Usage.CostMicros != "7" || detail.Usage.Currency != "CNY" || detail.Usage.AmountScale == nil || *detail.Usage.AmountScale != 1_000_000 || detail.Usage.MoneySchema != "micro-cny.v1" {
			t.Fatalf("v2 API money=%+v", detail.Usage)
		}
		if strings.Contains(event.Body.String(), "cost_cents") || !strings.Contains(event.Body.String(), "\"cost_micros\":\"7\"") {
			t.Fatalf("v2 API body=%s", event.Body.String())
		}
	}
	if !foundV2 {
		t.Fatalf("v2 usage event not found in timeline: %+v", timelineResponse.Items)
	}
}

func TestFreshValueRecoveredFallbackFixtureUsesUniqueCallLogOutcomeThroughAPI(t *testing.T) {
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

	sourceSystemID := randomTestUUID(t)
	report, err := unirouter.ImportSnapshot(
		filepath.Join("..", "..", "fixtures", "unirouter_snapshot", "fresh_value_recovered_fallback.json"),
		filepath.Join("..", "..", "schemas", "m0", "evidence-event.schema.json"),
		unirouter.SnapshotImportOptions{SourceSystemID: sourceSystemID, EmitEvents: true, RerunCheck: true},
	)
	if err != nil {
		t.Fatalf("import fresh-value fixture: %v", err)
	}
	provenance := syntheticProvenance(sourceSystemID, report.AdapterManifestID, report.SnapshotSchema, report.ArtifactSHA256)
	provenance.TimelineComplete = true
	persisted, err := store.PersistEventsWithProvenance(ctx, report.Events, provenance)
	if err != nil || persisted.Inserted != 5 || persisted.IdempotencyConflict != 0 {
		t.Fatalf("persist fresh-value fixture=%+v err=%v", persisted, err)
	}
	replayed, err := store.PersistEventsWithProvenance(ctx, report.Events, provenance)
	if err != nil || replayed.Inserted != 0 || replayed.DuplicateNoop != 5 || replayed.IdempotencyConflict != 0 {
		t.Fatalf("replay fresh-value fixture=%+v err=%v", replayed, err)
	}

	handler := newAuthenticatedHandler(t, store)
	requestID := report.Snapshot.RequestIDs[0]
	searchPath := "/api/v1/executions?request_id=" + url.QueryEscape(requestID) +
		"&source_system_id=" + url.QueryEscape(sourceSystemID) +
		"&from=" + url.QueryEscape("2026-07-21T00:00:00Z") +
		"&to=" + url.QueryEscape("2026-07-22T00:00:00Z")
	search := httptest.NewRecorder()
	handler.ServeHTTP(search, httptest.NewRequest(http.MethodGet, searchPath, nil))
	if search.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", search.Code, search.Body.String())
	}
	var searchResponse struct {
		Items []investigation.ExecutionSummary
		Page  map[string]json.RawMessage
	}
	decodePostgresResponse(t, search, &searchResponse)
	if len(searchResponse.Items) != 1 || searchResponse.Items[0].Outcome != investigation.OutcomeSuccess {
		t.Fatalf("unique call-log API outcome=%+v", searchResponse.Items)
	}
	for outcome, wantCount := range map[string]int{
		investigation.OutcomeSuccess: 1,
		investigation.OutcomeFailure: 0,
	} {
		filtered := httptest.NewRecorder()
		handler.ServeHTTP(filtered, httptest.NewRequest(http.MethodGet, searchPath+"&outcome="+outcome, nil))
		if filtered.Code != http.StatusOK {
			t.Fatalf("%s filter status=%d body=%s", outcome, filtered.Code, filtered.Body.String())
		}
		var filteredResponse struct {
			Items []investigation.ExecutionSummary
		}
		decodePostgresResponse(t, filtered, &filteredResponse)
		if len(filteredResponse.Items) != wantCount {
			t.Fatalf("%s filter items=%+v want_count=%d", outcome, filteredResponse.Items, wantCount)
		}
	}
	var asOfTime time.Time
	if err := json.Unmarshal(searchResponse.Page["as_of"], &asOfTime); err != nil {
		t.Fatalf("decode search as_of: %v", err)
	}
	executionID := searchResponse.Items[0].ExecutionID
	asOf := url.QueryEscape(asOfTime.UTC().Format(time.RFC3339Nano))
	detailResponse := httptest.NewRecorder()
	handler.ServeHTTP(detailResponse, httptest.NewRequest(http.MethodGet, "/api/v1/executions/"+executionID+"?as_of="+asOf, nil))
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detailResponse.Code, detailResponse.Body.String())
	}
	var executionDetail investigation.ExecutionDetail
	decodePostgresResponse(t, detailResponse, &executionDetail)
	if executionDetail.Summary.Outcome != investigation.OutcomeSuccess {
		t.Fatalf("detail outcome=%+v", executionDetail.Summary)
	}
	timeline := httptest.NewRecorder()
	handler.ServeHTTP(timeline, httptest.NewRequest(http.MethodGet, "/api/v1/executions/"+executionID+"/timeline?limit=10&as_of="+asOf, nil))
	if timeline.Code != http.StatusOK {
		t.Fatalf("timeline status=%d body=%s", timeline.Code, timeline.Body.String())
	}
	var timelineResponse struct {
		Items []investigation.TimelineNode
	}
	decodePostgresResponse(t, timeline, &timelineResponse)
	if len(timelineResponse.Items) != 5 {
		t.Fatalf("timeline=%+v", timelineResponse.Items)
	}

	wantDecisionKinds := map[string]string{
		"routing.route.selected":  "routing",
		"provider.attempt.failed": "provider",
		"retry.attempt.scheduled": "retry",
		"fallback.route.selected": "failover",
	}
	seen := map[string]bool{}
	seenFailedAttempt := false
	seenSuccessfulCallLog := false
	for _, node := range timelineResponse.Items {
		if node.EventType == "provider.attempt.failed" && node.OutcomeStatus == investigation.OutcomeFailure {
			seenFailedAttempt = true
		}
		if node.EventType == "usage.tokens.recorded" && node.OutcomeStatus == investigation.OutcomeSuccess {
			seenSuccessfulCallLog = true
		}
		event := httptest.NewRecorder()
		path := "/api/v1/executions/" + executionID + "/events/" + node.EventID + "?as_of=" + asOf
		handler.ServeHTTP(event, httptest.NewRequest(http.MethodGet, path, nil))
		if event.Code != http.StatusOK {
			t.Fatalf("event %s status=%d body=%s", node.EventType, event.Code, event.Body.String())
		}
		for _, forbidden := range []string{"raw_source_excerpt", "is_stream", "route_index", "retry_number", "fallback_from_route_index", "\"metadata\""} {
			if strings.Contains(event.Body.String(), forbidden) {
				t.Fatalf("event %s exposed restricted key %q: %s", node.EventType, forbidden, event.Body.String())
			}
		}
		wantKind, inspect := wantDecisionKinds[node.EventType]
		if !inspect {
			continue
		}
		var detail investigation.EvidenceEventDetail
		decodePostgresResponse(t, event, &detail)
		if detail.Decision.DecisionKind != wantKind {
			t.Fatalf("event %s decision=%+v", node.EventType, detail.Decision)
		}
		seen[node.EventType] = true
	}
	if !seenFailedAttempt || !seenSuccessfulCallLog || len(seen) != len(wantDecisionKinds) {
		t.Fatalf("failed=%t success=%t normalized decisions=%+v", seenFailedAttempt, seenSuccessfulCallLog, seen)
	}

	projectionResponse := httptest.NewRecorder()
	handler.ServeHTTP(
		projectionResponse,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/executions/"+executionID+"/investigation?as_of="+asOf,
			nil,
		),
	)
	if projectionResponse.Code != http.StatusOK {
		t.Fatalf("investigation status=%d body=%s", projectionResponse.Code, projectionResponse.Body.String())
	}
	var projection investigation.InvestigationProjection
	decodePostgresResponse(t, projectionResponse, &projection)
	if projection.PresentationMode != investigation.PresentationCausalTrace ||
		!projection.Integrity.SourceProfileComplete ||
		!projection.Integrity.ProjectionComplete {
		t.Fatalf("investigation integrity=%+v mode=%s", projection.Integrity, projection.PresentationMode)
	}
	claimCodes := make(map[string]bool)
	for _, claim := range projection.SummaryClaims {
		claimCodes[claim.Code] = true
		if len(claim.EvidenceEventIDs) == 0 {
			t.Fatalf("claim lacks Evidence citations: %+v", claim)
		}
	}
	if !claimCodes["primary_provider_failed_then_fallback_succeeded"] ||
		!claimCodes["execution_completed_successfully"] {
		t.Fatalf("investigation claims=%+v", projection.SummaryClaims)
	}
	nodeKinds := make(map[string]bool)
	for _, node := range projection.Nodes {
		nodeKinds[node.Kind] = true
		if node.Citation.EvidenceEventID == "" || node.Citation.SourceRef == "" || node.Citation.EventHash == "" {
			t.Fatalf("node lacks a complete Evidence citation: %+v", node)
		}
	}
	for _, kind := range []string{"route_selected", "provider_failed", "retry_scheduled", "fallback_selected", "execution_completed"} {
		if !nodeKinds[kind] {
			t.Fatalf("investigation missing node kind %s: %+v", kind, nodeKinds)
		}
	}
	if len(projection.Edges) == 0 || len(projection.StageCoverage) != 7 {
		t.Fatalf("investigation edges=%+v coverage=%+v", projection.Edges, projection.StageCoverage)
	}
	if strings.Contains(projectionResponse.Body.String(), "raw_source_excerpt") ||
		strings.Contains(projectionResponse.Body.String(), "\"metadata\"") {
		t.Fatalf("investigation exposed restricted source projection: %s", projectionResponse.Body.String())
	}
	if strings.Contains(projectionResponse.Body.String(), `"gaps":null`) {
		t.Fatalf("investigation encoded empty gaps as null: %s", projectionResponse.Body.String())
	}
}

func TestExecutionEvidenceV2BundlePersistsAndProjectsThroughAPI(t *testing.T) {
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
	migrationChecksums, err := db.LoadMigrationChecksums(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("load migration checksums: %v", err)
	}
	readiness := httptest.NewRecorder()
	api.NewHandlerWithMigrations(store, migrationChecksums).ServeHTTP(
		readiness,
		httptest.NewRequest(http.MethodGet, "/readyz", nil),
	)
	if readiness.Code != http.StatusOK ||
		!strings.Contains(readiness.Body.String(), `"database_schema":"`+buildinfo.DatabaseSchemaMax+`"`) {
		t.Fatalf("schema %s readiness status=%d body=%s", buildinfo.DatabaseSchemaMax, readiness.Code, readiness.Body.String())
	}

	sourceSystemID := randomTestUUID(t)
	report, err := unirouter.ImportExecutionEvidenceBundle(
		filepath.Join("..", "..", "fixtures", "unirouter_execution_evidence", "recovered_fallback.v2.json"),
		filepath.Join("..", "..", "schemas", "m0", "evidence-event.schema.json"),
		unirouter.SnapshotImportOptions{
			SourceSystemID: sourceSystemID,
			EmitEvents:     true,
			RerunCheck:     true,
		},
	)
	if err != nil {
		t.Fatalf("import execution-evidence V2 bundle: %v", err)
	}
	if report.Accepted != 6 ||
		report.RerunDuplicateNoop != 6 ||
		report.AdapterManifestID != unirouter.DefaultExecutionEvidenceAdapterID ||
		!report.Bundle.Completeness.SourceProfileComplete {
		t.Fatalf("unexpected execution-evidence import report: %+v", report.SnapshotImportReport)
	}
	closedAt, err := time.Parse(time.RFC3339Nano, report.Bundle.ClosedAt)
	if err != nil {
		t.Fatalf("parse bundle closed_at: %v", err)
	}
	provenance := syntheticProvenance(
		sourceSystemID,
		report.AdapterManifestID,
		unirouter.ExecutionEvidenceProfileID+"@"+unirouter.ExecutionEvidenceProfileManifestSHA256,
		report.ArtifactSHA256,
	)
	provenance.SourceGeneratedAt = &closedAt
	provenance.SourceTerminalAt = &closedAt
	provenance.TimelineComplete = report.Bundle.Completeness.SourceProfileComplete
	persisted, err := store.PersistEventsWithProvenance(ctx, report.Events, provenance)
	if err != nil ||
		persisted.Inserted != 6 ||
		persisted.CaptureGapsInserted != 0 ||
		persisted.IdempotencyConflict != 0 {
		t.Fatalf("persist execution-evidence report=%+v err=%v", persisted, err)
	}
	replayed, err := store.PersistEventsWithProvenance(ctx, report.Events, provenance)
	if err != nil || replayed.DuplicateNoop != 6 || replayed.IdempotencyConflict != 0 {
		t.Fatalf("replay execution-evidence report=%+v err=%v", replayed, err)
	}

	executionID, err := executionid.New(sourceSystemID, report.Bundle.RequestID)
	if err != nil {
		t.Fatalf("execution ID: %v", err)
	}
	asOf := url.QueryEscape(time.Now().UTC().Format(time.RFC3339Nano))
	response := httptest.NewRecorder()
	newAuthenticatedHandler(t, store).ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/executions/"+executionID+"/investigation?as_of="+asOf,
			nil,
		),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("investigation status=%d body=%s", response.Code, response.Body.String())
	}
	var projection investigation.InvestigationProjection
	decodePostgresResponse(t, response, &projection)
	if projection.SchemaVersion != investigation.InvestigationProjectionSchema ||
		projection.PresentationMode != investigation.PresentationCausalTrace ||
		projection.Execution.ProofClass != investigation.ProofSyntheticFixture ||
		projection.Execution.Outcome != investigation.OutcomeSuccess ||
		!projection.Integrity.SourceProfileComplete ||
		!projection.Integrity.ImportComplete ||
		!projection.Integrity.ProjectionComplete ||
		projection.Integrity.Truncated ||
		len(projection.Nodes) != 6 ||
		len(projection.Edges) != 4 ||
		len(projection.Gaps) != 0 {
		t.Fatalf("unexpected execution-evidence projection: %+v", projection)
	}
	for _, stage := range projection.StageCoverage {
		if stage.Status != investigation.CoveragePresent {
			t.Fatalf("stage coverage is not demo-complete: %+v", projection.StageCoverage)
		}
	}
	claims := make(map[string]bool)
	for _, claim := range projection.SummaryClaims {
		claims[claim.Code] = true
	}
	for _, required := range []string{
		"provider_failed_then_retry_scheduled",
		"primary_provider_failed_then_fallback_succeeded",
		"execution_completed_successfully",
	} {
		if !claims[required] {
			t.Fatalf("missing claim %q: claims=%+v nodes=%+v", required, projection.SummaryClaims, projection.Nodes)
		}
	}
	if strings.Contains(response.Body.String(), "raw_source_excerpt") ||
		strings.Contains(response.Body.String(), "\"metadata\"") ||
		!strings.Contains(response.Body.String(), "\"cost_micros\":\"17\"") ||
		!strings.Contains(response.Body.String(), "\"total_tokens\":11") {
		t.Fatalf("unsafe or incomplete investigation projection: %s", response.Body.String())
	}
}

func syntheticProvenance(sourceSystemID string, adapterManifestID string, revision string, artifactHash string) investigation.ImportProvenance {
	return investigation.ImportProvenance{
		SourceSystemID:       sourceSystemID,
		AdapterManifestID:    adapterManifestID,
		IntegrationMode:      investigation.IntegrationFixture,
		ProofClass:           investigation.ProofSyntheticFixture,
		AssuranceMode:        investigation.AssuranceSyntheticFixture,
		SourceArtifactSHA256: artifactHash,
		SourceRevision:       revision,
		ImportedRevision:     artifactHash,
		SourceWatermark:      artifactHash,
		ImportWatermark:      artifactHash,
		TimelineComplete:     false,
	}
}

func variantEvent(t *testing.T, original map[string]any, sourceVersion string) map[string]any {
	t.Helper()
	variant := make(map[string]any, len(original))
	for key, value := range original {
		variant[key] = value
	}
	suffix := randomTestUUID(t)
	variant["id"] = suffix
	variant["source_ref"] = "unirouter:local-synthetic-v2:registry-variant:" + suffix
	variant["source_event_id"] = suffix
	variant["idempotency_key"] = "adapter-unirouter:registry-variant:" + suffix
	variant["import_cursor"] = "registry-variant:" + suffix
	variantRequestID := "req-registry-variant-" + suffix
	variant["request_id"] = variantRequestID
	if correlation, ok := original["correlation"].(map[string]any); ok {
		correlationCopy := make(map[string]any, len(correlation))
		for key, value := range correlation {
			correlationCopy[key] = value
		}
		correlationCopy["request_id"] = variantRequestID
		variant["correlation"] = correlationCopy
	}
	if sourceVersion == "" {
		delete(variant, "source_version")
	} else {
		variant["source_version"] = sourceVersion
	}
	delete(variant, "event_hash")
	hash, err := evidence.GenerateEventHash(variant)
	if err != nil {
		t.Fatalf("hash registry variant: %v", err)
	}
	variant["event_hash"] = hash
	return variant
}

func assertMapperRegistryUpgrade(t *testing.T, ctx context.Context, dsn string, sourceSystemID string) {
	t.Helper()
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open registry connection: %v", err)
	}
	defer conn.Close()
	var sourceVersion string
	if err := conn.QueryRowContext(ctx, "SELECT source_version FROM source_systems WHERE id = $1", sourceSystemID).Scan(&sourceVersion); err != nil {
		t.Fatalf("read source version: %v", err)
	}
	if sourceVersion != unirouter.SnapshotSchemaV2 {
		t.Fatalf("source registry version=%q", sourceVersion)
	}
	for manifestID, wantVersion := range map[string]string{
		unirouter.DefaultAdapterManifestIDV1: unirouter.SnapshotSchemaV1,
		unirouter.DefaultAdapterManifestIDV2: unirouter.SnapshotSchemaV2,
	} {
		var adapterVersion string
		if err := conn.QueryRowContext(ctx, "SELECT adapter_version FROM adapter_manifests WHERE id = $1", manifestID).Scan(&adapterVersion); err != nil {
			t.Fatalf("read adapter version %s: %v", manifestID, err)
		}
		if adapterVersion != wantVersion {
			t.Fatalf("adapter %s version=%q want=%q", manifestID, adapterVersion, wantVersion)
		}
	}
}

func decodePostgresResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v body=%s", err, response.Body.String())
	}
}

func mappedEventTimeRange(t *testing.T, events []map[string]any) (time.Time, time.Time) {
	t.Helper()
	var from time.Time
	var to time.Time
	for _, event := range events {
		raw, ok := event["occurred_at"].(string)
		if !ok || raw == "" {
			t.Fatalf("mapped event occurred_at=%v", event["occurred_at"])
		}
		occurredAt, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			t.Fatalf("parse mapped event occurred_at %q: %v", raw, err)
		}
		if from.IsZero() || occurredAt.Before(from) {
			from = occurredAt
		}
		if to.IsZero() || occurredAt.After(to) {
			to = occurredAt
		}
	}
	if from.IsZero() || !from.Before(to) {
		t.Fatalf("mapped event time range from=%s to=%s", from, to)
	}
	return from.UTC(), to.UTC()
}

func randomTestUUID(t *testing.T) string {
	t.Helper()
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("random UUID: %v", err)
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(data)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}
