package litellmfeed

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/foigent/x-insight/internal/adapters/litellm"
	"github.com/foigent/x-insight/internal/db"
)

// TestLiteLLMComponentIntegration is intentionally environment-gated. CI runs
// it against the pinned LiteLLM v1.93.0 image, that image's real PostgreSQL
// spend-log store and the local OpenAI-compatible upstream in scripts/ci. Its
// outcomes are deterministic while successful response identities are unique.
// It is not an httptest substitute for the component boundary.
func TestLiteLLMComponentIntegration(t *testing.T) {
	endpoint := os.Getenv("XINSIGHT_LITELLM_COMPONENT_ENDPOINT")
	apiKey := os.Getenv("XINSIGHT_LITELLM_COMPONENT_API_KEY")
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	sourceDatabase := os.Getenv("XINSIGHT_LITELLM_COMPONENT_SOURCE_DATABASE")
	consumerDatabase := os.Getenv("XINSIGHT_LITELLM_COMPONENT_CONSUMER_DATABASE")
	sourceAccess := os.Getenv("XINSIGHT_LITELLM_COMPONENT_SOURCE_ACCESS")
	privacySentinels := []string{
		os.Getenv("XINSIGHT_LITELLM_COMPONENT_SUCCESS_PROMPT"),
		os.Getenv("XINSIGHT_LITELLM_COMPONENT_SECOND_SUCCESS_PROMPT"),
		os.Getenv("XINSIGHT_LITELLM_COMPONENT_FAILURE_PROMPT"),
		os.Getenv("XINSIGHT_LITELLM_COMPONENT_UNAUTHORIZED_TEAM_PROMPT"),
		os.Getenv("XINSIGHT_LITELLM_COMPONENT_OBSERVER_DENIED_PROMPT"),
		os.Getenv("XINSIGHT_LITELLM_COMPONENT_SUCCESS_RESPONSE"),
		os.Getenv("XINSIGHT_LITELLM_COMPONENT_AUTHORIZED_TRAFFIC_KEY_SENTINEL"),
		os.Getenv("XINSIGHT_LITELLM_COMPONENT_OBSERVER_KEY_SENTINEL"),
		os.Getenv("XINSIGHT_LITELLM_COMPONENT_UNAUTHORIZED_TRAFFIC_KEY_SENTINEL"),
	}
	if endpoint == "" ||
		apiKey == "" ||
		dsn == "" ||
		sourceDatabase == "" ||
		consumerDatabase == "" ||
		sourceAccess == "" {
		t.Skip("LiteLLM component integration environment is not bound")
	}
	seenPrivacySentinels := make(map[string]struct{}, len(privacySentinels))
	for _, sentinel := range privacySentinels {
		if sentinel == "" {
			t.Fatal("LiteLLM component privacy sentinels are not bound")
		}
		if _, duplicate := seenPrivacySentinels[sentinel]; duplicate {
			t.Fatal("LiteLLM component privacy sentinels must be distinct")
		}
		seenPrivacySentinels[sentinel] = struct{}{}
	}
	if _, leaked := os.LookupEnv("LITELLM_COMPONENT_SOURCE_POSTGRES_DSN"); leaked {
		t.Fatal("Source PostgreSQL DSN leaked into the X-Insight component test process")
	}
	if sourceDatabase == consumerDatabase ||
		sourceDatabase != "litellm_component_source" ||
		consumerDatabase != "xinsight_litellm_component" {
		t.Fatalf(
			"component database identities source=%q consumer=%q",
			sourceDatabase,
			consumerDatabase,
		)
	}
	if sourceAccess != "http_spend_logs_v2_only" {
		t.Fatalf("Source access contract=%q", sourceAccess)
	}
	endpointURL, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("parse component endpoint: %v", err)
	}
	if endpointURL.Scheme != "https" ||
		endpointURL.Path != "/spend/logs/v2" ||
		endpointURL.RawQuery != "" ||
		endpointURL.Fragment != "" {
		t.Fatalf("component Source endpoint is not exact HTTPS /spend/logs/v2")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	migrationsDir := filepath.Join("..", "..", "..", "migrations")
	expectedMigrations, err := db.LoadMigrationChecksums(migrationsDir)
	if err != nil {
		t.Fatalf("load X-Insight migration identities: %v", err)
	}
	applied, err := db.RunMigrations(
		ctx,
		dsn,
		migrationsDir,
	)
	if err != nil {
		t.Fatalf("run X-Insight migrations: %v", err)
	}
	if applied != len(expectedMigrations) {
		t.Fatalf(
			"X-Insight component migrations applied=%d, want current migration count=%d",
			applied,
			len(expectedMigrations),
		)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open component PostgreSQL: %v", err)
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("ping component PostgreSQL: %v", err)
	}
	var actualConsumerDatabase string
	if err := database.QueryRowContext(
		ctx,
		"SELECT current_database()",
	).Scan(&actualConsumerDatabase); err != nil {
		t.Fatalf("read Consumer database identity: %v", err)
	}
	if actualConsumerDatabase != consumerDatabase {
		t.Fatalf(
			"Consumer DSN resolved to %q, want %q",
			actualConsumerDatabase,
			consumerDatabase,
		)
	}
	t.Logf(
		"component database isolation: source=%s consumer=%s source_access=%s",
		sourceDatabase,
		actualConsumerDatabase,
		sourceAccess,
	)

	stateStore, err := NewStateStore(database)
	if err != nil {
		t.Fatalf("new LiteLLM StateStore: %v", err)
	}
	if _, err := stateStore.Load(ctx); !errors.Is(err, ErrStateNotFound) {
		if err == nil {
			t.Fatal("component integration requires a fresh LiteLLM durable state")
		}
		t.Fatalf("inspect initial LiteLLM durable state: %v", err)
	}
	source, err := NewHTTPSource(HTTPSourceConfig{
		Endpoint: endpoint,
		APIKey:   apiKey,
	})
	if err != nil {
		t.Fatalf("new real LiteLLM HTTPSource: %v", err)
	}
	sourceInstance := "litellm-component-ci-v1"
	scope := ScopeBinding{OrgRef: "101", ProjectRef: "202"}
	activationTime := time.Now().UTC().Truncate(time.Microsecond)
	activation := Checkpoint{
		LastStartTime: activationTime.
			Add(-10 * time.Minute).
			Format(checkpointTimeLayout),
		BoundaryRequestID: []string{},
	}
	activated, err := stateStore.Activate(
		ctx,
		sourceInstance,
		scope,
		activation,
		activationTime,
	)
	if err != nil {
		t.Fatalf("activate real LiteLLM StateStore: %v", err)
	}
	initialCursor := activated.Cursor
	if initialCursor == "" ||
		activated.Cursor != activated.HighWatermark ||
		activated.ActivationCursor != activated.Cursor ||
		activated.ActivationHighWatermark != activated.HighWatermark {
		t.Fatalf("initial durable binding=%+v", activated)
	}

	pollNow := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Microsecond)
	eligibleThrough := pollNow.Add(-EligibilityDelay)
	queryStartTime := checkpointQueryStart(activation)
	queryEndTime := eligibleThrough.UTC().Truncate(time.Second)
	var page sourcePage
	for attempt := 0; attempt < 45; attempt++ {
		var payload []byte
		payload, err = source.fetchPage(ctx, queryStartTime, queryEndTime, 1)
		if err != nil {
			t.Fatalf("fetch real LiteLLM spend-log proof page: %v", err)
		}
		page, err = decodePage(payload)
		if err != nil {
			t.Fatalf("decode real LiteLLM spend-log proof page: %v", err)
		}
		if page.Page != 1 ||
			page.PageSize != PageSize ||
			page.TotalIsCapped ||
			page.Total < 0 ||
			page.Total > 3 ||
			len(page.Data) != page.Total {
			t.Fatalf(
				"real LiteLLM spend-log proof page shape=%d/%d/%d/%d/%t/%d",
				page.Page,
				page.PageSize,
				page.Total,
				page.TotalPages,
				page.TotalIsCapped,
				len(page.Data),
			)
		}
		if page.Total == 3 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for real LiteLLM spend-log rows")
		case <-time.After(time.Second):
		}
	}
	if page.Total != 3 || page.TotalPages != 1 || len(page.Data) != 3 {
		t.Fatalf(
			"real LiteLLM spend-log proof page=%d/%d/%d, want 3/1/3",
			page.Total,
			page.TotalPages,
			len(page.Data),
		)
	}
	statuses := map[string]int{}
	seenRequestIDs := map[string]struct{}{}
	for _, rawRow := range page.Data {
		rawRequestID, expectedCacheStatus, err := componentRawCacheStatusProof(rawRow)
		if err != nil {
			t.Fatal(err)
		}
		row, err := litellm.SanitizeUpstreamRowBytes(rawRow)
		if err != nil {
			t.Fatalf("sanitize real LiteLLM spend-log proof row: %v", err)
		}
		if row.Record.RequestID != rawRequestID {
			t.Fatal("sanitized Source request identity differs from its raw proof row")
		}
		if _, duplicate := seenRequestIDs[rawRequestID]; duplicate {
			t.Fatal("real LiteLLM spend-log proof page contains a duplicate request_id")
		}
		seenRequestIDs[rawRequestID] = struct{}{}
		statuses[row.Record.Status]++
		assertSanitizedComponentRecord(t, row, expectedCacheStatus)
	}
	if statuses["succeeded"] != 2 || statuses["failed"] != 1 || len(statuses) != 2 {
		t.Fatalf("real LiteLLM terminal statuses=%v", statuses)
	}

	projector, err := litellm.NewProjector()
	if err != nil {
		t.Fatalf("new LiteLLM projector: %v", err)
	}
	evidenceStore, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatalf("open X-Insight EvidenceStore: %v", err)
	}
	defer evidenceStore.Close()
	committer, err := NewEvidenceCommitter(evidenceStore)
	if err != nil {
		t.Fatalf("new LiteLLM Evidence committer: %v", err)
	}
	worker, err := NewWorker(
		source,
		stateStore,
		projector,
		committer,
		WorkerConfig{
			SourceInstance: sourceInstance,
			OrgRef:         scope.OrgRef,
			ProjectRef:     scope.ProjectRef,
			PollInterval:   time.Second,
			Now:            func() time.Time { return pollNow },
		},
	)
	if err != nil {
		t.Fatalf("new real LiteLLM worker: %v", err)
	}
	report, err := worker.PollOnce(ctx)
	if err != nil {
		t.Fatalf("commit real LiteLLM spend-log window: %v", err)
	}
	if report.WindowRows != 3 ||
		report.Accepted != 3 ||
		report.Inserted != 3 ||
		report.DuplicateNoop != 0 ||
		!report.CheckpointAdvanced {
		t.Fatalf("real LiteLLM poll report=%+v", report)
	}
	state, err := stateStore.Load(ctx)
	if err != nil {
		t.Fatalf("load converged LiteLLM state: %v", err)
	}
	if state.Cursor == initialCursor ||
		state.Cursor != state.HighWatermark ||
		state.PagesCommitted != 1 ||
		state.ItemsReceived != 3 ||
		state.ItemsCommitted != 3 ||
		state.DuplicateNoop != 0 ||
		state.WorkerStatus != "idle" ||
		state.ConsecutiveFailures != 0 ||
		state.LastErrorCode != "" {
		t.Fatalf("converged LiteLLM state=%+v", state)
	}
	assertComponentPersistence(t, ctx, database, privacySentinels)
}

func componentCacheStatus(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	switch {
	case bytes.Equal(trimmed, []byte("null")):
		return "unknown", nil
	case bytes.Equal(trimmed, []byte("true")):
		return "hit", nil
	case bytes.Equal(trimmed, []byte("false")):
		return "miss", nil
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return "", errors.New("raw LiteLLM cache_hit does not match the frozen v1.1.2 mapping")
	}
	switch value {
	case "True":
		return "hit", nil
	case "False":
		return "miss", nil
	case "", "None":
		return "unknown", nil
	default:
		return "", errors.New("raw LiteLLM cache_hit does not match the frozen v1.1.2 mapping")
	}
}

func componentRawCacheStatusProof(rawRow json.RawMessage) (string, string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawRow, &fields); err != nil {
		return "", "", errors.New("raw LiteLLM cache-status proof row is invalid")
	}
	rawRequestID, exists := fields["request_id"]
	if !exists {
		return "", "", errors.New("raw LiteLLM cache-status proof row is missing request_id")
	}
	var requestID string
	if err := json.Unmarshal(rawRequestID, &requestID); err != nil || requestID == "" {
		return "", "", errors.New("raw LiteLLM cache-status proof request_id is invalid")
	}
	rawCacheHit, exists := fields["cache_hit"]
	if !exists {
		return "", "", errors.New("raw LiteLLM cache-status proof row is missing cache_hit")
	}
	cacheStatus, err := componentCacheStatus(rawCacheHit)
	if err != nil {
		return "", "", err
	}
	return requestID, cacheStatus, nil
}

func TestComponentCacheStatusMatchesFrozenV112Mapping(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "null", raw: `null`, want: "unknown"},
		{name: "true boolean", raw: `true`, want: "hit"},
		{name: "false boolean", raw: `false`, want: "miss"},
		{name: "true string", raw: `"True"`, want: "hit"},
		{name: "false string", raw: `"False"`, want: "miss"},
		{name: "empty string", raw: `""`, want: "unknown"},
		{name: "none string", raw: `"None"`, want: "unknown"},
		{name: "lowercase none rejects", raw: `"none"`, wantErr: true},
		{name: "null string rejects", raw: `"null"`, wantErr: true},
		{name: "number rejects", raw: `0`, wantErr: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := componentCacheStatus(json.RawMessage(test.raw))
			if test.wantErr {
				if err == nil {
					t.Fatal("invalid raw cache_hit was accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("map raw cache_hit: %v", err)
			}
			if got != test.want {
				t.Fatalf("cache status=%q, want %q", got, test.want)
			}
		})
	}
}

func assertSanitizedComponentRecord(
	t *testing.T,
	row litellm.SanitizedRecord,
	expectedCacheStatus string,
) {
	t.Helper()
	var canonical map[string]any
	if err := json.Unmarshal(row.CanonicalBytes, &canonical); err != nil {
		t.Fatalf("decode sanitized Source record: %v", err)
	}
	for _, prohibited := range []string{
		"api_key",
		"user",
		"end_user",
		"requester_ip_address",
		"metadata",
		"messages",
		"response",
		"proxy_server_request",
		"cache_key",
		"session_id",
		"team_id",
		"organization_id",
	} {
		if _, present := canonical[prohibited]; present {
			t.Fatalf("sanitized Source record retained prohibited field %q", prohibited)
		}
	}
	if row.Record.RequestID == "" ||
		row.SourceHash == "" ||
		!strings.HasPrefix(row.SourceHash, "sha256:") {
		t.Fatalf("sanitized Source identity is incomplete: %+v", row)
	}
	if row.Record.CacheStatus != expectedCacheStatus {
		t.Fatalf(
			"sanitized Source cache status=%q, want %q from raw v1.1.2 mapping",
			row.Record.CacheStatus,
			expectedCacheStatus,
		)
	}
	switch row.Record.Status {
	case "succeeded":
		if row.Record.PromptTokens != "3" ||
			row.Record.CompletionTokens != "2" ||
			row.Record.TotalTokens != "5" ||
			row.Record.ReportedSpend != "0.000007" {
			t.Fatalf("successful Source usage/spend=%+v", row.Record)
		}
	case "failed":
		if row.Record.PromptTokens != "0" ||
			row.Record.CompletionTokens != "0" ||
			row.Record.TotalTokens != "0" ||
			row.Record.ReportedSpend != "0" {
			t.Fatalf("failed Source usage/spend=%+v", row.Record)
		}
	default:
		t.Fatalf("unexpected sanitized Source status=%q", row.Record.Status)
	}
}

func assertComponentPersistence(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	privacySentinels []string,
) {
	t.Helper()
	var (
		eventCount         int
		requestCount       int
		succeededCount     int
		failedCount        int
		gapEventCount      int
		idempotencyCount   int
		eventHashCount     int
		sourceHashCount    int
		metadataEventCount int
		noneEventCount     int
	)
	if err := database.QueryRowContext(ctx, `
SELECT
  count(*),
  count(DISTINCT request_id),
  count(*) FILTER (WHERE event_type = 'model.request.succeeded'),
  count(*) FILTER (WHERE event_type = 'model.request.failed'),
  count(*) FILTER (WHERE event_type = 'gap.capture.detected'),
  count(DISTINCT idempotency_key),
  count(DISTINCT event_hash),
  count(DISTINCT source_event_hash),
  count(*) FILTER (WHERE payload_capture_mode = 'metadata_only'),
  count(*) FILTER (WHERE payload_capture_mode = 'none')
FROM evidence_events
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
	).Scan(
		&eventCount,
		&requestCount,
		&succeededCount,
		&failedCount,
		&gapEventCount,
		&idempotencyCount,
		&eventHashCount,
		&sourceHashCount,
		&metadataEventCount,
		&noneEventCount,
	); err != nil {
		t.Fatalf("read component Evidence summary: %v", err)
	}
	if eventCount != 15 ||
		requestCount != 3 ||
		succeededCount != 2 ||
		failedCount != 1 ||
		gapEventCount != 12 ||
		idempotencyCount != 15 ||
		eventHashCount != 15 ||
		sourceHashCount != 3 ||
		metadataEventCount != 3 ||
		noneEventCount != 12 {
		t.Fatalf(
			"component Evidence summary=%d/%d/%d/%d/%d/%d/%d/%d/%d/%d",
			eventCount,
			requestCount,
			succeededCount,
			failedCount,
			gapEventCount,
			idempotencyCount,
			eventHashCount,
			sourceHashCount,
			metadataEventCount,
			noneEventCount,
		)
	}

	usageRows, err := database.QueryContext(ctx, `
SELECT
  event_type,
  usage_data->>'prompt_tokens',
  usage_data->>'completion_tokens',
  usage_data->>'total_tokens',
  usage_data#>>'{extensions,source_reported_money,amount}'
FROM evidence_events
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND event_type IN ('model.request.succeeded', 'model.request.failed')
ORDER BY event_type`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
	)
	if err != nil {
		t.Fatalf("read component M0 usage/spend: %v", err)
	}
	defer usageRows.Close()
	usageByEvent := map[string][4]string{}
	usageRowCount := 0
	for usageRows.Next() {
		var (
			eventType  string
			prompt     string
			completion string
			total      string
			spend      string
		)
		if err := usageRows.Scan(
			&eventType,
			&prompt,
			&completion,
			&total,
			&spend,
		); err != nil {
			t.Fatalf("scan component M0 usage/spend: %v", err)
		}
		usageByEvent[eventType] = [4]string{prompt, completion, total, spend}
		usageRowCount++
	}
	if err := usageRows.Err(); err != nil {
		t.Fatalf("iterate component M0 usage/spend: %v", err)
	}
	if got := usageByEvent["model.request.succeeded"]; got != [4]string{"3", "2", "5", "0.000007"} {
		t.Fatalf("successful M0 usage/spend=%v", got)
	}
	if got := usageByEvent["model.request.failed"]; got != [4]string{"0", "0", "0", "0"} {
		t.Fatalf("failed M0 usage/spend=%v", got)
	}
	if len(usageByEvent) != 2 {
		t.Fatalf("component M0 usage/spend event types=%d, want 2", len(usageByEvent))
	}
	if usageRowCount != 3 {
		t.Fatalf("component M0 usage/spend rows=%d, want 3", usageRowCount)
	}

	var gapRows int
	var gapClaims int
	if err := database.QueryRowContext(ctx, `
SELECT count(*), count(DISTINCT affected_claim)
FROM capture_gaps
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
	).Scan(&gapRows, &gapClaims); err != nil {
		t.Fatalf("read component Gap summary: %v", err)
	}
	if gapRows != 12 || gapClaims != 4 {
		t.Fatalf("component Gap summary=%d/%d", gapRows, gapClaims)
	}

	var provenanceRows int
	var provenanceLinks int
	var volatileWatermarks int
	if err := database.QueryRowContext(ctx, `
SELECT
  count(DISTINCT provenance.id),
  count(links.evidence_event_id),
  count(*) FILTER (
    WHERE provenance.source_watermark IS NOT NULL
       OR provenance.import_watermark IS NOT NULL
  )
FROM import_provenance AS provenance
JOIN import_provenance_events AS links
  ON links.import_provenance_id = provenance.id
WHERE provenance.source_system_id = $1::uuid
  AND provenance.adapter_manifest_id = $2::uuid
  AND provenance.integration_mode = 'connector_pull'
  AND provenance.assurance_mode = 'contracted_pull'
  AND provenance.timeline_complete`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
	).Scan(
		&provenanceRows,
		&provenanceLinks,
		&volatileWatermarks,
	); err != nil {
		t.Fatalf("read component provenance summary: %v", err)
	}
	if provenanceRows != 3 || provenanceLinks != 15 || volatileWatermarks != 0 {
		t.Fatalf(
			"component provenance summary=%d/%d/%d",
			provenanceRows,
			provenanceLinks,
			volatileWatermarks,
		)
	}

	rows, err := database.QueryContext(ctx, `
SELECT raw_source_excerpt::text
FROM evidence_events
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
	)
	if err != nil {
		t.Fatalf("read persisted source excerpts: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var excerpt string
		if err := rows.Scan(&excerpt); err != nil {
			t.Fatalf("scan persisted source excerpt: %v", err)
		}
		for _, prohibited := range []string{
			`"api_key"`,
			`"messages"`,
			`"response"`,
			`"proxy_server_request"`,
			`"requester_ip_address"`,
		} {
			if strings.Contains(excerpt, prohibited) {
				t.Fatalf("persisted source excerpt contains prohibited key %s", prohibited)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate persisted source excerpts: %v", err)
	}

	for _, sentinel := range privacySentinels {
		var sentinelMatches int
		if err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM evidence_events AS evidence
WHERE evidence.source_system_id = $1::uuid
  AND evidence.adapter_manifest_id = $2::uuid
  AND strpos(row_to_json(evidence)::text, $3) > 0`,
			litellm.SourceSystemID,
			litellm.AdapterManifestID,
			sentinel,
		).Scan(&sentinelMatches); err != nil {
			t.Fatalf("scan X Evidence for privacy sentinel: %v", err)
		}
		if sentinelMatches != 0 {
			t.Fatalf(
				"X Evidence contains a privacy sentinel in %d rows",
				sentinelMatches,
			)
		}
	}
}
