package db_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/investigation"
)

func TestFreshValueRecoveredFallbackFixtureUsesUniqueCallLogOutcomeAndRetainsEvidence(t *testing.T) {
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

	snapshot, err := unirouter.LoadSnapshot(filepath.Join("..", "..", "fixtures", "unirouter_snapshot", "fresh_value_recovered_fallback.json"))
	if err != nil {
		t.Fatalf("load fresh-value fixture: %v", err)
	}
	sourceID := testUUID(t)
	adapterID := testUUID(t)
	events, err := unirouter.BuildSnapshotEvents(snapshot, unirouter.SnapshotImportOptions{
		SourceSystemID:    sourceID,
		AdapterManifestID: adapterID,
	})
	if err != nil {
		t.Fatalf("build fresh-value events: %v", err)
	}
	for _, event := range events {
		hash, hashErr := evidence.GenerateEventHash(event)
		if hashErr != nil {
			t.Fatalf("hash fresh-value event: %v", hashErr)
		}
		event["event_hash"] = hash
	}
	artifactHash := "sha256:" + repeatHex('f', 64)
	provenance := investigation.ImportProvenance{
		SourceSystemID:       sourceID,
		AdapterManifestID:    adapterID,
		IntegrationMode:      investigation.IntegrationFixture,
		ProofClass:           investigation.ProofSyntheticFixture,
		AssuranceMode:        investigation.AssuranceSyntheticFixture,
		SourceArtifactSHA256: artifactHash,
		SourceRevision:       "fresh-value-fixture-v1",
		ImportedRevision:     artifactHash,
		SourceWatermark:      artifactHash,
		ImportWatermark:      artifactHash,
		TimelineComplete:     false,
	}
	report, err := store.PersistEventsWithProvenance(ctx, events, provenance)
	if err != nil || report.Inserted != 5 || report.IdempotencyConflict != 0 {
		t.Fatalf("persist fresh-value fixture report=%+v err=%v", report, err)
	}
	replay, err := store.PersistEventsWithProvenance(ctx, events, provenance)
	if err != nil || replay.Inserted != 0 || replay.DuplicateNoop != 5 || replay.IdempotencyConflict != 0 {
		t.Fatalf("replay fresh-value fixture report=%+v err=%v", replay, err)
	}

	clockDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open clock connection: %v", err)
	}
	defer clockDB.Close()
	var asOf time.Time
	if err := clockDB.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&asOf); err != nil {
		t.Fatalf("read database clock: %v", err)
	}
	asOf = asOf.UTC()
	requestID := snapshot.RequestIDs[0]
	query := investigation.SearchQuery{
		AsOf:           asOf,
		From:           time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC),
		To:             asOf,
		RequestID:      requestID,
		SourceSystemID: sourceID,
		Limit:          10,
	}
	summaries, err := store.SearchExecutions(ctx, query)
	if err != nil || len(summaries) != 1 {
		t.Fatalf("search fresh-value fixture summaries=%+v err=%v", summaries, err)
	}
	alertID := fmt.Sprintf("xal_%x", sha256.Sum256([]byte("posture-alert:"+sourceID)))
	if _, err := clockDB.ExecContext(ctx, `
INSERT INTO investigation_alerts (
  id, source_system_id, source_alert_ref, title, severity, state, execution_id, observed_at
) VALUES ($1, $2::uuid, $3, $4, 'warning', 'open', $5, $6)`,
		alertID,
		sourceID,
		"posture-alert:"+sourceID,
		"Fresh-value posture warning",
		summaries[0].ExecutionID,
		asOf.Add(-time.Second),
	); err != nil {
		t.Fatalf("insert posture Alert fixture: %v", err)
	}
	posture, err := store.GetPosture(ctx, investigation.PostureQuery{
		AsOf:           asOf,
		From:           query.From,
		To:             query.To,
		SourceSystemID: sourceID,
	})
	if err != nil {
		t.Fatalf("aggregate fresh-value posture: %v", err)
	}
	if posture.Executions.Total != 1 || posture.Executions.Success != 1 ||
		posture.Executions.Failure != 0 || posture.Executions.WithGaps != 0 ||
		len(posture.Coverage) != 4 || len(posture.Sources) != 1 || len(posture.Trend.Items) == 0 {
		t.Fatalf("fresh-value posture=%+v", posture)
	}
	if posture.Sources[0].SourceSystemID != sourceID || posture.Sources[0].ExecutionTotal != 1 ||
		posture.Risk.OpenTotal != 1 || posture.Risk.Warning != 1 ||
		posture.Risk.AffectedExecutions != 1 || len(posture.Risk.Items) != 1 ||
		posture.Risk.Items[0].ID != alertID {
		t.Fatalf("fresh-value posture source/risk=%+v/%+v", posture.Sources, posture.Risk)
	}
	if summaries[0].Outcome != investigation.OutcomeSuccess {
		t.Fatalf("unique call-log outcome=%s want=%s", summaries[0].Outcome, investigation.OutcomeSuccess)
	}
	detail, err := store.GetExecution(ctx, summaries[0].ExecutionID, asOf)
	if err != nil || detail.Summary.Outcome != investigation.OutcomeSuccess {
		t.Fatalf("fresh-value detail=%+v err=%v", detail, err)
	}
	successQuery := query
	successQuery.Outcome = investigation.OutcomeSuccess
	successItems, err := store.SearchExecutions(ctx, successQuery)
	if err != nil || len(successItems) != 1 || successItems[0].ExecutionID != summaries[0].ExecutionID {
		t.Fatalf("success filter items=%+v err=%v", successItems, err)
	}
	failureQuery := query
	failureQuery.Outcome = investigation.OutcomeFailure
	failureItems, err := store.SearchExecutions(ctx, failureQuery)
	if err != nil || len(failureItems) != 0 {
		t.Fatalf("failure filter items=%+v err=%v", failureItems, err)
	}

	timeline, total, err := store.ListTimeline(ctx, investigation.TimelineQuery{
		ExecutionID: summaries[0].ExecutionID,
		AsOf:        asOf,
		Limit:       10,
	})
	if err != nil || total != 5 || len(timeline) != 5 {
		t.Fatalf("timeline=%+v total=%d err=%v", timeline, total, err)
	}
	wantDecisionKinds := map[string]string{
		"routing.route.selected":  "routing",
		"provider.attempt.failed": "provider",
		"retry.attempt.scheduled": "retry",
		"fallback.route.selected": "failover",
	}
	seenDecisionKinds := map[string]bool{}
	seenFailedAttempt := false
	seenSuccessfulCallLog := false
	for _, node := range timeline {
		if node.EventType == "provider.attempt.failed" && node.OutcomeStatus == investigation.OutcomeFailure {
			seenFailedAttempt = true
		}
		if node.EventType == "usage.tokens.recorded" && node.OutcomeStatus == investigation.OutcomeSuccess {
			seenSuccessfulCallLog = true
		}
		wantKind, inspect := wantDecisionKinds[node.EventType]
		if !inspect {
			continue
		}
		detail, detailErr := store.GetExecutionEvent(ctx, summaries[0].ExecutionID, node.EventID, asOf)
		if detailErr != nil {
			t.Fatalf("get %s detail: %v", node.EventType, detailErr)
		}
		if detail.Decision.DecisionKind != wantKind {
			t.Fatalf("event %s decision=%+v", node.EventType, detail.Decision)
		}
		body, marshalErr := json.Marshal(detail)
		if marshalErr != nil {
			t.Fatalf("marshal %s detail: %v", node.EventType, marshalErr)
		}
		for _, forbidden := range []string{"raw_source_excerpt", "is_stream", "route_index", "retry_number", "fallback_from_route_index", "\"metadata\""} {
			if strings.Contains(string(body), forbidden) {
				t.Fatalf("event %s exposed restricted key %q: %s", node.EventType, forbidden, body)
			}
		}
		seenDecisionKinds[node.EventType] = true
	}
	if !seenFailedAttempt || !seenSuccessfulCallLog || len(seenDecisionKinds) != len(wantDecisionKinds) {
		t.Fatalf("failed=%t success=%t decisions=%+v", seenFailedAttempt, seenSuccessfulCallLog, seenDecisionKinds)
	}
}

func TestExecutionOutcomeFallsBackWithoutExactlyOneUniRouterCallLog(t *testing.T) {
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
	clockDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open clock connection: %v", err)
	}
	defer clockDB.Close()

	tests := []struct {
		name       string
		sourceType string
		events     []struct {
			sourceEventType string
			outcome         string
		}
		wantOutcome string
	}{
		{
			name:       "unique failed call log remains failure",
			sourceType: "unirouter",
			events: []struct {
				sourceEventType string
				outcome         string
			}{
				{sourceEventType: "trace_event", outcome: investigation.OutcomeSuccess},
				{sourceEventType: "call_log", outcome: investigation.OutcomeFailure},
			},
			wantOutcome: investigation.OutcomeFailure,
		},
		{
			name:       "zero call logs retain worst event",
			sourceType: "unirouter",
			events: []struct {
				sourceEventType string
				outcome         string
			}{
				{sourceEventType: "trace_event", outcome: investigation.OutcomeSuccess},
				{sourceEventType: "trace_event", outcome: investigation.OutcomeFailure},
			},
			wantOutcome: investigation.OutcomeFailure,
		},
		{
			name:       "multiple call logs retain worst event",
			sourceType: "unirouter",
			events: []struct {
				sourceEventType string
				outcome         string
			}{
				{sourceEventType: "trace_event", outcome: investigation.OutcomeFailure},
				{sourceEventType: "call_log", outcome: investigation.OutcomeSuccess},
				{sourceEventType: "call_log", outcome: investigation.OutcomeSuccess},
			},
			wantOutcome: investigation.OutcomeFailure,
		},
		{
			name:       "registered non unirouter unique call log retains worst event",
			sourceType: "registered-test-source",
			events: []struct {
				sourceEventType string
				outcome         string
			}{
				{sourceEventType: "trace_event", outcome: investigation.OutcomeFailure},
				{sourceEventType: "call_log", outcome: investigation.OutcomeSuccess},
			},
			wantOutcome: investigation.OutcomeFailure,
		},
	}

	for caseIndex, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sourceID := testUUID(t)
			adapterID := testUUID(t)
			if test.sourceType != "unirouter" {
				registerInvestigationTestConnector(
					t,
					ctx,
					clockDB,
					sourceID,
					adapterID,
					test.sourceType,
				)
			}
			requestID := "req-outcome-" + sourceID[:8]
			baseTime := time.Date(2026, 7, 21, 4, 10+caseIndex, 0, 0, time.UTC)
			events := make([]map[string]any, 0, len(test.events))
			for eventIndex, spec := range test.events {
				eventID := testUUID(t)
				event := investigationOutcomeTestEvent(
					sourceID,
					adapterID,
					requestID,
					eventID,
					test.sourceType,
					spec.sourceEventType,
					spec.outcome,
					baseTime.Add(time.Duration(eventIndex)*time.Second),
				)
				hash, hashErr := evidence.GenerateEventHash(event)
				if hashErr != nil {
					t.Fatalf("hash event %d: %v", eventIndex, hashErr)
				}
				event["event_hash"] = hash
				events = append(events, event)
			}
			report, persistErr := store.PersistEvents(ctx, events)
			if persistErr != nil || report.Inserted != len(events) || report.IdempotencyConflict != 0 {
				t.Fatalf("persist events report=%+v err=%v", report, persistErr)
			}
			var asOf time.Time
			if clockErr := clockDB.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&asOf); clockErr != nil {
				t.Fatalf("read database clock: %v", clockErr)
			}
			query := investigation.SearchQuery{
				AsOf:           asOf.UTC(),
				From:           baseTime.Add(-time.Minute),
				To:             asOf.UTC(),
				RequestID:      requestID,
				SourceSystemID: sourceID,
				Limit:          10,
			}
			summaries, searchErr := store.SearchExecutions(ctx, query)
			if searchErr != nil || len(summaries) != 1 || summaries[0].Outcome != test.wantOutcome {
				t.Fatalf("summaries=%+v err=%v want=%s", summaries, searchErr, test.wantOutcome)
			}
			detail, detailErr := store.GetExecution(ctx, summaries[0].ExecutionID, asOf.UTC())
			if detailErr != nil || detail.Summary.Outcome != test.wantOutcome {
				t.Fatalf("detail=%+v err=%v want=%s", detail, detailErr, test.wantOutcome)
			}
		})
	}
}

func registerInvestigationTestConnector(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	sourceID string,
	adapterID string,
	sourceType string,
) {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin investigation test registry: %v", err)
	}
	defer tx.Rollback()
	connectorID := "xinsight.test.investigation-outcome"
	if _, err := tx.ExecContext(ctx, `
INSERT INTO source_systems (
  id, source_key, source_type, display_name, source_version, status, metadata
) VALUES (
  $1::uuid, $2, $3, 'Investigation outcome test source',
  'xinsight.test.investigation-outcome.v1', 'active',
  jsonb_build_object(
    'connector_id', $4::text,
    'connector_version', '1.0.0',
    'protocol_version', '1.1.1'
  )
)`,
		sourceID,
		"investigation-outcome-"+sourceID,
		sourceType,
		connectorID,
	); err != nil {
		t.Fatalf("register investigation test source: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO adapter_manifests (
  id, adapter_id, adapter_version, source_type, fidelity_level,
  supported_source_versions, ingestion_modes, declared_event_types,
  payload_behavior, idempotency_strategy, manifest_json, active
) VALUES (
  $1::uuid, $2, '1.0.0', $3, 'F1',
  ARRAY['xinsight.test.investigation-outcome.v1']::text[],
  ARRAY['db_import']::text[],
  ARRAY['usage.tokens.recorded','provider.attempt.observed']::text[],
  'metadata_only_default', 'source_ref_event_type_source_event_id',
  jsonb_build_object(
    'product_managed_registry', true,
    'connector_id', $4::text,
    'connector_version', '1.0.0',
    'protocol_version', '1.1.1'
  ),
  true
)`,
		adapterID,
		"investigation-outcome-"+adapterID,
		sourceType,
		connectorID,
	); err != nil {
		t.Fatalf("register investigation test adapter: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit investigation test registry: %v", err)
	}
}

func TestInvestigationStoreKeepsSourcesSeparateAndPagesAtFixedSnapshot(t *testing.T) {
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

	requestID := "req-investigation-" + testUUID(t)[:8]
	type importedExecution struct {
		sourceID  string
		adapterID string
		events    []map[string]any
	}
	imports := make([]importedExecution, 0, 2)
	for index := 0; index < 2; index++ {
		sourceID := testUUID(t)
		adapterID := testUUID(t)
		snapshot, err := unirouter.LoadSnapshot(filepath.Join("..", "..", "fixtures", "unirouter_snapshot", "missing_trace_gap.json"))
		if err != nil {
			t.Fatalf("load snapshot: %v", err)
		}
		snapshot.SourceInstance = "investigation-shared"
		snapshot.RequestIDs = []string{requestID}
		for row := range snapshot.Scenarios {
			snapshot.Scenarios[row].RequestID = requestID
		}
		for row := range snapshot.CallLogs {
			snapshot.CallLogs[row].SourceRef = ""
			snapshot.CallLogs[row].RequestID = requestID
		}
		for row := range snapshot.TraceEvents {
			snapshot.TraceEvents[row].SourceRef = ""
			snapshot.TraceEvents[row].RequestID = requestID
		}
		for row := range snapshot.CaptureGaps {
			snapshot.CaptureGaps[row].RequestID = requestID
		}
		events, err := unirouter.BuildSnapshotEvents(snapshot, unirouter.SnapshotImportOptions{
			SourceSystemID:    sourceID,
			AdapterManifestID: adapterID,
		})
		if err != nil {
			t.Fatalf("build snapshot events: %v", err)
		}
		for _, event := range events {
			hash, err := evidence.GenerateEventHash(event)
			if err != nil {
				t.Fatalf("hash event: %v", err)
			}
			event["event_hash"] = hash
		}
		artifactHash := "sha256:" + repeatHex(byte('a'+index), 64)
		report, err := store.PersistEventsWithProvenance(ctx, events, investigation.ImportProvenance{
			SourceSystemID:       sourceID,
			AdapterManifestID:    adapterID,
			IntegrationMode:      investigation.IntegrationFixture,
			ProofClass:           investigation.ProofSyntheticFixture,
			AssuranceMode:        investigation.AssuranceSyntheticFixture,
			SourceArtifactSHA256: artifactHash,
			SourceRevision:       "fixture-1",
			ImportedRevision:     artifactHash,
			SourceWatermark:      artifactHash,
			ImportWatermark:      artifactHash,
			TimelineComplete:     true,
		})
		if err != nil {
			t.Fatalf("persist source %d: %v", index, err)
		}
		if report.Inserted != 2 || report.CaptureGapsInserted != 1 || report.ImportProvenanceID == "" {
			t.Fatalf("source %d report=%+v", index, report)
		}
		imports = append(imports, importedExecution{sourceID: sourceID, adapterID: adapterID, events: events})
	}

	clockDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open clock connection: %v", err)
	}
	defer clockDB.Close()
	var asOf time.Time
	if err := clockDB.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&asOf); err != nil {
		t.Fatalf("read database clock: %v", err)
	}
	asOf = asOf.UTC()
	query := investigation.SearchQuery{
		AsOf:      asOf,
		From:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		To:        asOf,
		RequestID: requestID,
		Limit:     10,
	}
	summaries, err := store.SearchExecutions(ctx, query)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(summaries) != 2 || summaries[0].ExecutionID == summaries[1].ExecutionID {
		t.Fatalf("summaries=%+v", summaries)
	}
	for _, summary := range summaries {
		expectedID, err := executionid.New(summary.SourceSystemID, requestID)
		if err != nil {
			t.Fatalf("expected execution ID: %v", err)
		}
		if summary.ExecutionID != expectedID || summary.ProofClass != investigation.ProofSyntheticFixture {
			t.Fatalf("summary=%+v expected_id=%s", summary, expectedID)
		}
		if summary.EventCount != 2 || summary.GapCount != 1 || summary.Freshness.SyncState != investigation.SyncStateGap {
			t.Fatalf("summary counts/freshness=%+v", summary)
		}
	}
	sourceFiltered := query
	sourceFiltered.SourceSystemID = summaries[0].SourceSystemID
	sourceItems, err := store.SearchExecutions(ctx, sourceFiltered)
	if err != nil || len(sourceItems) != 1 || sourceItems[0].SourceSystemID != summaries[0].SourceSystemID {
		t.Fatalf("source filter items=%+v err=%v", sourceItems, err)
	}
	prefixFiltered := query
	prefixFiltered.RequestID = ""
	prefixFiltered.RequestPrefix = requestID[:len(requestID)-1]
	prefixItems, err := store.SearchExecutions(ctx, prefixFiltered)
	if err != nil || len(prefixItems) != 2 {
		t.Fatalf("prefix filter items=%+v err=%v", prefixItems, err)
	}
	outcomeFiltered := query
	outcomeFiltered.Outcome = investigation.OutcomeSuccess
	outcomeItems, err := store.SearchExecutions(ctx, outcomeFiltered)
	if err != nil || len(outcomeItems) != 2 {
		t.Fatalf("outcome filter items=%+v err=%v", outcomeItems, err)
	}
	withoutGaps := false
	gapFiltered := query
	gapFiltered.HasGaps = &withoutGaps
	gapItems, err := store.SearchExecutions(ctx, gapFiltered)
	if err != nil || len(gapItems) != 0 {
		t.Fatalf("gap filter items=%+v err=%v", gapItems, err)
	}

	firstPageQuery := query
	firstPageQuery.Limit = 1
	firstPage, err := store.SearchExecutions(ctx, firstPageQuery)
	if err != nil || len(firstPage) != 1 {
		t.Fatalf("first page=%+v err=%v", firstPage, err)
	}
	secondPageQuery := firstPageQuery
	secondPageQuery.After = &investigation.SearchKey{
		LastEffectiveAt: firstPage[0].LastEffectiveAt,
		ExecutionID:     firstPage[0].ExecutionID,
	}
	secondPage, err := store.SearchExecutions(ctx, secondPageQuery)
	if err != nil || len(secondPage) != 1 || secondPage[0].ExecutionID == firstPage[0].ExecutionID {
		t.Fatalf("second page=%+v err=%v", secondPage, err)
	}

	executionID := summaries[0].ExecutionID
	detail, err := store.GetExecution(ctx, executionID, asOf)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Summary.ExecutionID != executionID || detail.Summary.EventCount != 2 {
		t.Fatalf("detail=%+v", detail)
	}

	timeline, totalEvents, err := store.ListTimeline(ctx, investigation.TimelineQuery{
		ExecutionID: executionID,
		AsOf:        asOf,
		Limit:       1,
	})
	if err != nil || totalEvents != 2 || len(timeline) != 1 {
		t.Fatalf("timeline=%+v total=%d err=%v", timeline, totalEvents, err)
	}
	nextTimeline, nextTotal, err := store.ListTimeline(ctx, investigation.TimelineQuery{
		ExecutionID: executionID,
		AsOf:        asOf,
		After: &investigation.TimelineKey{
			EffectiveAt: timeline[0].EffectiveAt,
			IngestedAt:  timeline[0].IngestedAt,
			EventID:     timeline[0].EventID,
		},
		Limit: 1,
	})
	if err != nil || nextTotal != 2 || len(nextTimeline) != 1 || nextTimeline[0].EventID == timeline[0].EventID {
		t.Fatalf("next timeline=%+v total=%d err=%v", nextTimeline, nextTotal, err)
	}

	event, err := store.GetExecutionEvent(ctx, executionID, timeline[0].EventID, asOf)
	if err != nil {
		t.Fatalf("event detail: %v", err)
	}
	if event.ExecutionID != executionID || event.EventID != timeline[0].EventID || event.Source.EventHash == "" {
		t.Fatalf("event=%+v", event)
	}

	gaps, totalGaps, err := store.ListExecutionGaps(ctx, investigation.GapQuery{
		ExecutionID: executionID,
		AsOf:        asOf,
		Limit:       1,
	})
	if err != nil || totalGaps != 1 || len(gaps) != 1 || len(gaps[0].CandidateGapTypes) != 3 {
		t.Fatalf("gaps=%+v total=%d err=%v", gaps, totalGaps, err)
	}

	sources, err := store.ListSources(ctx, 100)
	if err != nil {
		t.Fatalf("sources: %v", err)
	}
	found := map[string]bool{}
	for _, source := range sources {
		found[source.SourceSystemID] = true
	}
	for _, imported := range imports {
		if !found[imported.sourceID] {
			t.Fatalf("source %s absent from %+v", imported.sourceID, sources)
		}
	}
	if _, err := clockDB.ExecContext(ctx, `
INSERT INTO import_provenance (
  id, source_system_id, adapter_manifest_id, integration_mode, proof_class,
  assurance_mode, source_artifact_sha256, approval_manifest_sha256,
  timeline_complete
) VALUES ($1, $2, $3, 'sidecar', 'operator_approved_integration',
  'operator_attested_sidecar', $4, $5, false)`,
		testUUID(t),
		imports[0].sourceID,
		imports[0].adapterID,
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	); err == nil {
		t.Fatal("database accepted operator-approved provenance without exchange ID")
	}
	lateExecutionID, err := executionid.New(imports[0].sourceID, requestID)
	if err != nil {
		t.Fatalf("late execution ID: %v", err)
	}
	approvedArtifactHash := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	approvedReport, err := store.PersistEventsWithProvenance(ctx, imports[0].events, investigation.ImportProvenance{
		SourceSystemID:         imports[0].sourceID,
		AdapterManifestID:      imports[0].adapterID,
		IntegrationMode:        investigation.IntegrationSidecar,
		ProofClass:             investigation.ProofOperatorApproved,
		AssuranceMode:          investigation.AssuranceOperatorAttested,
		SourceArtifactSHA256:   approvedArtifactHash,
		ApprovalManifestSHA256: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		ExchangeID:             "exchange-approved-duplicate",
		ImportedRevision:       approvedArtifactHash,
		ImportWatermark:        approvedArtifactHash,
		TimelineComplete:       false,
	})
	if err != nil || approvedReport.DuplicateNoop != 2 || approvedReport.ImportProvenanceID == "" {
		t.Fatalf("approved duplicate report=%+v err=%v", approvedReport, err)
	}
	var approvedAsOf time.Time
	if err := clockDB.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&approvedAsOf); err != nil {
		t.Fatalf("read approved database clock: %v", err)
	}
	approvedDetail, err := store.GetExecution(ctx, lateExecutionID, approvedAsOf.UTC())
	if err != nil {
		t.Fatalf("approved duplicate detail: %v", err)
	}
	if approvedDetail.Summary.ProofClass != investigation.ProofOperatorApproved || approvedDetail.Summary.AssuranceMode != investigation.AssuranceOperatorAttested || approvedDetail.Summary.Freshness.ImportWatermark != approvedArtifactHash {
		t.Fatalf("approved duplicate provenance not projected: %+v", approvedDetail)
	}
	_, err = store.PersistEventsWithProvenance(ctx, imports[0].events, investigation.ImportProvenance{
		ID:                     approvedReport.ImportProvenanceID,
		SourceSystemID:         imports[0].sourceID,
		AdapterManifestID:      imports[0].adapterID,
		IntegrationMode:        investigation.IntegrationSidecar,
		ProofClass:             investigation.ProofOperatorApproved,
		AssuranceMode:          investigation.AssuranceOperatorAttested,
		SourceArtifactSHA256:   "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		ApprovalManifestSHA256: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		ExchangeID:             "exchange-approved-duplicate",
		ImportedRevision:       approvedArtifactHash,
		ImportWatermark:        approvedArtifactHash,
		TimelineComplete:       false,
	})
	if err == nil {
		t.Fatal("conflicting immutable import provenance ID was accepted")
	}

	lateEvent := investigationTestEvent(
		imports[0].sourceID,
		imports[0].adapterID,
		requestID,
		testUUID(t),
		"late-span",
		"",
		asOf.Add(-time.Minute),
		asOf.Add(-time.Minute),
	)
	lateHash, err := evidence.GenerateEventHash(lateEvent)
	if err != nil {
		t.Fatalf("hash late event: %v", err)
	}
	lateEvent["event_hash"] = lateHash
	lateReport, err := store.PersistEvents(ctx, []map[string]any{lateEvent})
	if err != nil || lateReport.Inserted != 1 {
		t.Fatalf("persist late event report=%+v err=%v", lateReport, err)
	}
	fixedDetail, err := store.GetExecution(ctx, lateExecutionID, asOf)
	if err != nil {
		t.Fatalf("fixed snapshot detail: %v", err)
	}
	if fixedDetail.Summary.EventCount != 2 || fixedDetail.Summary.ProofClass != investigation.ProofSyntheticFixture {
		t.Fatalf("late backdated event leaked into fixed snapshot: %+v", fixedDetail)
	}
	var afterInsert time.Time
	if err := clockDB.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&afterInsert); err != nil {
		t.Fatalf("read post-insert database clock: %v", err)
	}
	currentDetail, err := store.GetExecution(ctx, lateExecutionID, afterInsert.UTC())
	if err != nil {
		t.Fatalf("current snapshot detail: %v", err)
	}
	if currentDetail.Summary.EventCount != 3 || currentDetail.Summary.ProofClass != investigation.ProofUnclassified {
		t.Fatalf("current snapshot omitted late event or retained false proof: %+v", currentDetail)
	}
}

func TestInvestigationTimelineResolvesOnlyDeclaredParent(t *testing.T) {
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
	sourceID := testUUID(t)
	adapterID := testUUID(t)
	requestID := "req-parent-" + sourceID[:8]
	parentEventID := testUUID(t)
	childEventID := testUUID(t)
	siblingEventID := testUUID(t)
	baseTime := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	parent := investigationTestEvent(sourceID, adapterID, requestID, parentEventID, "parent-span", "", baseTime.Add(2*time.Second), baseTime.Add(4*time.Second))
	child := investigationTestEvent(sourceID, adapterID, requestID, childEventID, "child-span", "parent-span", baseTime, baseTime.Add(3*time.Second))
	sibling := investigationTestEvent(sourceID, adapterID, requestID, siblingEventID, "sibling-span", "", baseTime.Add(time.Second), baseTime.Add(3*time.Second))
	events := []map[string]any{parent, child, sibling}
	for _, event := range events {
		hash, err := evidence.GenerateEventHash(event)
		if err != nil {
			t.Fatalf("hash event: %v", err)
		}
		event["event_hash"] = hash
	}
	_, err = store.PersistEventsWithProvenance(ctx, events, investigation.ImportProvenance{
		SourceSystemID:       sourceID,
		AdapterManifestID:    adapterID,
		IntegrationMode:      investigation.IntegrationFixture,
		ProofClass:           investigation.ProofSyntheticFixture,
		AssuranceMode:        investigation.AssuranceSyntheticFixture,
		SourceArtifactSHA256: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		TimelineComplete:     true,
	})
	if err != nil {
		t.Fatalf("persist timeline events: %v", err)
	}
	executionID, err := executionid.New(sourceID, requestID)
	if err != nil {
		t.Fatalf("execution ID: %v", err)
	}
	nodes, total, err := store.ListTimeline(ctx, investigation.TimelineQuery{
		ExecutionID: executionID,
		AsOf:        time.Now().UTC(),
		Limit:       10,
	})
	if err != nil || total != 3 || len(nodes) != 3 {
		t.Fatalf("nodes=%+v total=%d err=%v", nodes, total, err)
	}
	if nodes[0].EventID != childEventID || nodes[0].DeclaredParentEventID != parentEventID || nodes[0].DeclaredParentResolved == nil || !*nodes[0].DeclaredParentResolved {
		t.Fatalf("declared child=%+v", nodes[0])
	}
	if nodes[1].EventID != siblingEventID || nodes[1].DeclaredParentEventID != "" || nodes[1].DeclaredParentResolved != nil {
		t.Fatalf("chronological sibling acquired false causality: %+v", nodes[1])
	}
	if nodes[2].EventID != parentEventID {
		t.Fatalf("reversed-clock parent order=%+v", nodes)
	}
}

func investigationTestEvent(sourceID string, adapterID string, requestID string, eventID string, spanID string, parentSpanID string, occurredAt time.Time, ingestedAt time.Time) map[string]any {
	correlation := map[string]any{"request_id": requestID, "span_id": spanID}
	if parentSpanID != "" {
		correlation["parent_span_id"] = parentSpanID
	}
	return map[string]any{
		"id":                   eventID,
		"schema_version":       "m0.1",
		"source_system_id":     sourceID,
		"adapter_manifest_id":  adapterID,
		"source_type":          "unirouter",
		"source_ref":           "timeline-test:" + eventID,
		"idempotency_key":      "timeline-test:" + eventID,
		"event_type":           "agent.step.observed",
		"stage":                "gateway",
		"severity":             "info",
		"capture_mode":         "db_import",
		"occurred_at":          occurredAt.UTC().Format(time.RFC3339Nano),
		"observed_at":          occurredAt.UTC().Format(time.RFC3339Nano),
		"ingested_at":          ingestedAt.UTC().Format(time.RFC3339Nano),
		"request_id":           requestID,
		"span_id":              spanID,
		"payload_capture_mode": "metadata_only",
		"correlation":          correlation,
		"outcome":              map[string]any{"outcome_status": "success"},
		"payload_policy":       map[string]any{"payload_capture_mode": "metadata_only"},
		"field_provenance":     map[string]any{"correlation": "direct", "outcome": "derived"},
	}
}

func investigationOutcomeTestEvent(sourceID string, adapterID string, requestID string, eventID string, sourceType string, sourceEventType string, outcome string, occurredAt time.Time) map[string]any {
	event := investigationTestEvent(
		sourceID,
		adapterID,
		requestID,
		eventID,
		"span-"+eventID[:8],
		"",
		occurredAt,
		occurredAt.Add(time.Second),
	)
	event["source_type"] = sourceType
	if sourceType != "unirouter" {
		event["source_version"] = "xinsight.test.investigation-outcome.v1"
	}
	event["source_event_id"] = eventID
	event["source_event_type"] = sourceEventType
	event["outcome"] = map[string]any{"outcome_status": outcome}
	if sourceEventType == "call_log" {
		event["event_type"] = "usage.tokens.recorded"
	} else {
		event["event_type"] = "provider.attempt.observed"
	}
	return event
}

func repeatHex(value byte, count int) string {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return string(result)
}
