package unirouter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/evidence"
)

func TestImportSnapshotMinimal(t *testing.T) {
	report, err := ImportSnapshot(
		filepath.Join("..", "..", "..", "fixtures", "unirouter_snapshot", "minimal.json"),
		filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json"),
		SnapshotImportOptions{RerunCheck: true, EmitEvents: true},
	)
	if err != nil {
		t.Fatalf("import snapshot: %v", err)
	}
	if report.EventsTotal != 2 {
		t.Fatalf("events total=%d", report.EventsTotal)
	}
	if report.Accepted != 2 || report.Rejected != 0 || report.Quarantined != 0 {
		t.Fatalf("unexpected outcomes accepted=%d rejected=%d quarantined=%d", report.Accepted, report.Rejected, report.Quarantined)
	}
	if report.RerunDuplicateNoop != 2 {
		t.Fatalf("rerun duplicate noop=%d", report.RerunDuplicateNoop)
	}
	if !strings.HasPrefix(report.ArtifactSHA256, "sha256:") || report.Snapshot.SchemaVersion != report.SnapshotSchema {
		t.Fatalf("artifact identity missing from prepared import: hash=%s snapshot=%+v", report.ArtifactSHA256, report.Snapshot)
	}

	eventTypes := map[string]bool{}
	for _, result := range report.Results {
		if result.Outcome != evidence.OutcomeAccepted {
			t.Fatalf("event %s outcome=%s reason=%s", result.SourceRef, result.Outcome, result.Reason)
		}
		eventTypes[result.EventType] = true
	}
	if !eventTypes["gateway.request.accepted"] {
		t.Fatalf("missing gateway.request.accepted event: %v", eventTypes)
	}
	if !eventTypes["usage.tokens.recorded"] {
		t.Fatalf("missing usage.tokens.recorded event: %v", eventTypes)
	}
	for index, event := range report.Events {
		hash, err := evidence.GenerateEventHash(event)
		if err != nil {
			t.Fatalf("hash prepared event %d: %v", index, err)
		}
		if event["event_hash"] != hash {
			t.Fatalf("prepared event %d hash=%v expected=%s", index, event["event_hash"], hash)
		}
		if event["event_type"] == "usage.tokens.recorded" {
			usage := event["usage"].(map[string]any)
			if usage["cost_cents"] != 1 {
				t.Fatalf("v1 cost_cents=%#v", usage["cost_cents"])
			}
			if _, exists := usage["cost_micros"]; exists {
				t.Fatalf("v1 unexpectedly contains cost_micros: %#v", usage)
			}
		}
	}
}

func TestFreshValueRecoveredFallbackFixtureSeparatesNormalizedFactsFromRestrictedRawProjection(t *testing.T) {
	report, err := ImportSnapshot(
		filepath.Join("..", "..", "..", "fixtures", "unirouter_snapshot", "fresh_value_recovered_fallback.json"),
		filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json"),
		SnapshotImportOptions{RerunCheck: true, EmitEvents: true},
	)
	if err != nil {
		t.Fatalf("import fresh-value fixture: %v", err)
	}
	if report.EventsTotal != 5 || report.Accepted != 5 || report.RerunDuplicateNoop != 5 {
		t.Fatalf("fixture report=%+v", report)
	}

	wantDecisions := map[string]struct {
		kind   string
		result string
	}{
		"routing.route.selected":  {kind: "routing", result: "selected"},
		"provider.attempt.failed": {kind: "provider", result: "failed"},
		"retry.attempt.scheduled": {kind: "retry", result: "allowed"},
		"fallback.route.selected": {kind: "failover", result: "selected"},
	}
	seenDecisions := map[string]bool{}
	seenCallLog := false
	for _, event := range report.Events {
		eventType, _ := event["event_type"].(string)
		raw, _ := event["raw_source_excerpt"].(map[string]any)
		if eventType == "usage.tokens.recorded" {
			seenCallLog = true
			outcome, _ := event["outcome"].(map[string]any)
			if outcome["outcome_status"] != "success" {
				t.Fatalf("call-log outcome=%+v", outcome)
			}
			if raw["is_stream"] != true {
				t.Fatalf("restricted call-log stream fact=%#v", raw["is_stream"])
			}
			if _, exposed := event["is_stream"]; exposed {
				t.Fatalf("is_stream escaped restricted projection: %+v", event)
			}
			continue
		}

		want, relevant := wantDecisions[eventType]
		if !relevant {
			continue
		}
		decision, _ := event["decision"].(map[string]any)
		if decision["decision_kind"] != want.kind || decision["decision_result"] != want.result {
			t.Fatalf("event %s decision=%+v", eventType, decision)
		}
		if _, hasMetadata := raw["metadata"]; !hasMetadata {
			t.Fatalf("event %s missing restricted metadata fixture", eventType)
		}
		if _, exposed := event["metadata"]; exposed {
			t.Fatalf("event %s metadata escaped restricted projection", eventType)
		}
		seenDecisions[eventType] = true
	}
	if !seenCallLog || len(seenDecisions) != len(wantDecisions) {
		t.Fatalf("seen call log=%t decisions=%+v", seenCallLog, seenDecisions)
	}
}

func TestLoadSnapshotExchangeMetadata(t *testing.T) {
	path := writeTempSnapshot(t, `{
		"schema_version": "unirouter.prd5.xinsight.snapshot.v1",
		"exchange_id": "exchange-1",
		"approval_manifest_sha256": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"generated_at": "2026-07-10T00:00:00Z",
		"source": "unirouter",
		"source_instance": "dev"
	}`)

	snapshot, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot.ExchangeID != "exchange-1" {
		t.Fatalf("exchange_id=%s", snapshot.ExchangeID)
	}
	if snapshot.ApprovalManifestSHA256 != "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("approval_manifest_sha256=%s", snapshot.ApprovalManifestSHA256)
	}
}

func TestImportSnapshotV2PreservesSubCentCostMicros(t *testing.T) {
	path := writeTempSnapshot(t, `{
		"schema_version": "unirouter.prd5.xinsight.snapshot.v2",
		"exchange_id": "exchange-v2-micros",
		"generated_at": "2026-07-16T00:00:00Z",
		"source": "unirouter",
		"source_instance": "synthetic-v2",
		"request_ids": ["req-v2-micros-7"],
		"safety": {
			"selection_mode": "explicit_request_id_only",
			"raw_prompts_included": false,
			"raw_responses_included": false,
			"api_keys_included": false
		},
		"counts": {
			"requested_request_ids": 1,
			"call_logs": 1,
			"request_trace_events": 0,
			"capture_gaps": 0
		},
		"capture_gaps": [],
		"call_logs": [{
			"source_ref": "unirouter:synthetic-v2:call_logs:7",
			"id": 7,
			"request_id": "req-v2-micros-7",
			"org_id": 1,
			"model": "synthetic-model",
			"requested_model": "synthetic-model",
			"provider": "synthetic",
			"channel": "synthetic",
			"prompt_tokens": 5,
			"completion_tokens": 1,
			"total_tokens": 6,
			"estimated_tokens": 0,
			"usage_source": "upstream",
			"usage_confidence": "1.000",
			"cost_micros": "7",
			"currency": "CNY",
			"amount_scale": 1000000,
			"money_schema": "micro-cny.v1",
			"latency_ms": 1,
			"ttft_ms": 1,
			"status_code": 200,
			"created_at": "2026-07-16T00:00:00Z",
			"payload_capture_mode": "metadata_only"
		}],
		"request_trace_events": []
	}`)

	report, err := ImportSnapshot(
		path,
		filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json"),
		SnapshotImportOptions{EmitEvents: true, RerunCheck: true},
	)
	if err != nil {
		t.Fatalf("import v2 snapshot: %v", err)
	}
	if len(report.Events) != 1 {
		t.Fatalf("events=%d", len(report.Events))
	}
	if report.AdapterManifestID != DefaultAdapterManifestIDV2 || report.RerunDuplicateNoop != 1 {
		t.Fatalf("v2 manifest/rerun report=%+v", report)
	}
	usage, ok := report.Events[0]["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage missing: %#v", report.Events[0]["usage"])
	}
	if usage["cost_micros"] != "7" {
		t.Fatalf("cost_micros=%#v want exact decimal string 7; usage=%#v", usage["cost_micros"], usage)
	}
	if _, exists := usage["cost_cents"]; exists {
		t.Fatalf("v2 usage must not downgrade micros to cents: %#v", usage)
	}
	if usage["currency"] != "CNY" || usage["amount_scale"] != int64(1_000_000) || usage["money_schema"] != "micro-cny.v1" {
		t.Fatalf("v2 money metadata not preserved: %#v", usage)
	}
}

func TestImportSnapshotRejectsInvalidVersionedMoney(t *testing.T) {
	testCases := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{name: "unknown version", mutate: func(snapshot map[string]any) { snapshot["schema_version"] = "unirouter.prd5.xinsight.snapshot.v3" }, wantErr: "unsupported UniRouter snapshot schema_version"},
		{name: "missing micros", mutate: deleteMoneyField("cost_micros"), wantErr: "are required"},
		{name: "missing currency", mutate: deleteMoneyField("currency"), wantErr: "are required"},
		{name: "wrong currency", mutate: setMoneyField("currency", "USD"), wantErr: "currency must be CNY"},
		{name: "wrong scale", mutate: setMoneyField("amount_scale", float64(100)), wantErr: "amount_scale must be 1000000"},
		{name: "wrong schema", mutate: setMoneyField("money_schema", "micro-cny.v2"), wantErr: "money_schema must be micro-cny.v1"},
		{name: "negative", mutate: setMoneyField("cost_micros", "-1"), wantErr: "canonical non-negative decimal string"},
		{name: "fractional", mutate: setMoneyField("cost_micros", "0.7"), wantErr: "canonical non-negative decimal string"},
		{name: "leading zero", mutate: setMoneyField("cost_micros", "07"), wantErr: "canonical non-negative decimal string"},
		{name: "plus sign", mutate: setMoneyField("cost_micros", "+7"), wantErr: "canonical non-negative decimal string"},
		{name: "too many digits", mutate: setMoneyField("cost_micros", "10000000000000000000"), wantErr: "at most 19 decimal digits"},
		{name: "ambiguous cents", mutate: setMoneyField("cost_cents", float64(0)), wantErr: "cost_cents conflicts with cost_micros"},
		{name: "null cents still conflicts", mutate: setMoneyField("cost_cents", nil), wantErr: "cost_cents conflicts with cost_micros"},
		{name: "null micros is not a value", mutate: setMoneyField("cost_micros", nil), wantErr: "are required"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			snapshot := validV2SnapshotMap()
			testCase.mutate(snapshot)
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatalf("marshal snapshot: %v", err)
			}
			path := writeTempSnapshot(t, string(encoded))
			_, err = ImportSnapshot(path, filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json"), SnapshotImportOptions{})
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("error=%v want substring %q", err, testCase.wantErr)
			}
		})
	}
}

func TestImportGeneratedSnapshotV2GoldenFixture(t *testing.T) {
	report, err := ImportSnapshot(
		filepath.Join("..", "..", "..", "fixtures", "unirouter_snapshot", "micro_money_v2_generated.json"),
		filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json"),
		SnapshotImportOptions{EmitEvents: true, RerunCheck: true},
	)
	if err != nil {
		t.Fatalf("import generated v2 fixture: %v", err)
	}
	if report.ArtifactSHA256 != "sha256:93bc4c8448f1f838668f9dab10cbba070f0df550665b16d73bb494066fe8d6a1" {
		t.Fatalf("artifact hash=%s", report.ArtifactSHA256)
	}
	if report.AdapterManifestID != DefaultAdapterManifestIDV2 || report.Accepted != 2 || report.RerunDuplicateNoop != 2 {
		t.Fatalf("generated fixture report=%+v", report)
	}
	var found bool
	for _, event := range report.Events {
		if event["event_type"] != "usage.tokens.recorded" {
			continue
		}
		found = true
		usage := event["usage"].(map[string]any)
		if usage["cost_micros"] != "7" || usage["currency"] != "CNY" || usage["amount_scale"] != int64(1_000_000) || usage["money_schema"] != "micro-cny.v1" {
			t.Fatalf("generated fixture money=%#v", usage)
		}
	}
	if !found {
		t.Fatal("generated fixture has no usage event")
	}
}

func TestImportSnapshotRejectsMismatchedReservedIdentityBeforeFirstWrite(t *testing.T) {
	fixture := filepath.Join("..", "..", "..", "fixtures", "unirouter_snapshot", "micro_money_v2_generated.json")
	schema := filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json")
	testCases := []struct {
		name    string
		options SnapshotImportOptions
		wantErr string
	}{
		{
			name:    "source version disagrees with artifact schema",
			options: SnapshotImportOptions{SourceVersion: SnapshotSchemaV1},
			wantErr: "must match snapshot schema_version",
		},
		{
			name:    "reserved v1 manifest used for v2 artifact",
			options: SnapshotImportOptions{AdapterManifestID: DefaultAdapterManifestIDV1},
			wantErr: "is reserved for snapshot schema_version",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := ImportSnapshot(fixture, schema, testCase.options); err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("error=%v want substring %q", err, testCase.wantErr)
			}
		})
	}

	matching, err := ImportSnapshot(fixture, schema, SnapshotImportOptions{
		SourceVersion:     SnapshotSchemaV2,
		AdapterManifestID: DefaultAdapterManifestIDV2,
	})
	if err != nil {
		t.Fatalf("matching explicit v2 identity: %v", err)
	}
	if matching.SnapshotSchema != SnapshotSchemaV2 || matching.AdapterManifestID != DefaultAdapterManifestIDV2 {
		t.Fatalf("matching report=%+v", matching)
	}

	customManifestID := "33333333-3333-4333-8333-333333333333"
	custom, err := ImportSnapshot(fixture, schema, SnapshotImportOptions{AdapterManifestID: customManifestID})
	if err != nil {
		t.Fatalf("custom non-reserved manifest: %v", err)
	}
	if custom.AdapterManifestID != customManifestID {
		t.Fatalf("custom manifest=%q", custom.AdapterManifestID)
	}
}

func TestBuildSnapshotEventsRejectsUnvalidatedSnapshot(t *testing.T) {
	if events, err := BuildSnapshotEvents(SnapshotFile{SchemaVersion: "unknown"}, SnapshotImportOptions{}); err == nil || events != nil {
		t.Fatalf("unknown schema events=%#v err=%v", events, err)
	}
	partial := SnapshotFile{
		SchemaVersion: SnapshotSchemaV2,
		CallLogs:      []SnapshotCallLog{{costMicrosPresent: true}},
	}
	if events, err := BuildSnapshotEvents(partial, SnapshotImportOptions{}); err == nil || events != nil {
		t.Fatalf("partial v2 events=%#v err=%v", events, err)
	}
}

func TestImportSnapshotRejectsNegativeV1CostAndUnknownFields(t *testing.T) {
	v1 := validV2SnapshotMap()
	v1["schema_version"] = SnapshotSchemaV1
	callLog := firstCallLog(v1)
	delete(callLog, "cost_micros")
	delete(callLog, "currency")
	delete(callLog, "amount_scale")
	delete(callLog, "money_schema")
	callLog["cost_cents"] = float64(-1)
	encoded, _ := json.Marshal(v1)
	_, err := LoadSnapshot(writeTempSnapshot(t, string(encoded)))
	if err == nil || !strings.Contains(err.Error(), "cost_cents must be non-negative") {
		t.Fatalf("negative v1 error=%v", err)
	}

	v2 := validV2SnapshotMap()
	firstCallLog(v2)["unexpected_money_unit"] = "cents"
	encoded, _ = json.Marshal(v2)
	_, err = LoadSnapshot(writeTempSnapshot(t, string(encoded)))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error=%v", err)
	}
}

func TestLoadSnapshotRejectsRawIPAddressField(t *testing.T) {
	const rawIPSentinel = "198.51.100.77"
	for _, field := range []string{"ip_address", "client_ip", "remote_ip", "source_ip", "remote_addr"} {
		t.Run(field, func(t *testing.T) {
			snapshot := validV2SnapshotMap()
			firstCallLog(snapshot)[field] = rawIPSentinel
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatalf("marshal snapshot: %v", err)
			}
			_, err = LoadSnapshot(writeTempSnapshot(t, string(encoded)))
			if err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("raw IP field %q error=%v", field, err)
			}
			if strings.Contains(err.Error(), rawIPSentinel) {
				t.Fatalf("raw IP sentinel echoed for field %q: %v", field, err)
			}
		})
	}
}

func TestImportSnapshotPersistsOnlyIPRedactionProofBooleans(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	snapshot.Safety.IPHandling = testStrictIPHandling
	snapshot.CallLogs[0].IPAddressWasPresent = true
	snapshot.CallLogs[0].IPAddressRedacted = true
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)
	preflight, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	if preflight.Status != "pass" {
		t.Fatalf("preflight report=%+v", preflight)
	}
	report, err := ImportSnapshot(
		path,
		filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json"),
		SnapshotImportOptions{EmitEvents: true},
	)
	if err != nil {
		t.Fatalf("import snapshot: %v", err)
	}
	var callLogEvent map[string]any
	for _, event := range report.Events {
		if event["event_type"] == "usage.tokens.recorded" {
			callLogEvent = event
			break
		}
	}
	if callLogEvent == nil {
		t.Fatalf("call-log event missing from %#v", report.Events)
	}
	raw, ok := callLogEvent["raw_source_excerpt"].(map[string]any)
	if !ok {
		t.Fatalf("raw_source_excerpt=%#v", callLogEvent["raw_source_excerpt"])
	}
	if raw["ip_address_was_present"] != true || raw["ip_address_redacted"] != true {
		t.Fatalf("IP proof=%#v", raw)
	}
	allowed := map[string]bool{
		"ip_address_was_present": true,
		"ip_address_redacted":    true,
	}
	for key := range raw {
		if strings.Contains(key, "ip_address") && !allowed[key] {
			t.Fatalf("unapproved IP-related persistence key %q in %#v", key, raw)
		}
	}
	for _, forbidden := range []string{"ip_address", "client_ip", "remote_ip", "source_ip", "remote_addr"} {
		if _, exists := raw[forbidden]; exists {
			t.Fatalf("raw IP key %q persisted in %#v", forbidden, raw)
		}
	}
}

func validV2SnapshotMap() map[string]any {
	return map[string]any{
		"schema_version":  SnapshotSchemaV2,
		"generated_at":    "2026-07-16T00:00:00Z",
		"source":          "unirouter",
		"source_instance": "synthetic-v2",
		"request_ids":     []any{"req-v2-micros-7"},
		"safety":          map[string]any{"selection_mode": "explicit_request_id_only", "raw_prompts_included": false, "raw_responses_included": false, "api_keys_included": false},
		"counts":          map[string]any{"requested_request_ids": float64(1), "call_logs": float64(1), "request_trace_events": float64(0), "capture_gaps": float64(0)},
		"capture_gaps":    []any{},
		"call_logs": []any{map[string]any{
			"source_ref": "unirouter:synthetic-v2:call_logs:7", "id": float64(7), "request_id": "req-v2-micros-7", "org_id": float64(1),
			"model": "synthetic-model", "requested_model": "synthetic-model", "provider": "synthetic", "channel": "synthetic",
			"prompt_tokens": float64(5), "completion_tokens": float64(1), "total_tokens": float64(6), "estimated_tokens": float64(0),
			"usage_source": "upstream", "usage_confidence": "1.000", "cost_micros": "7", "currency": "CNY", "amount_scale": float64(1_000_000), "money_schema": "micro-cny.v1",
			"latency_ms": float64(1), "ttft_ms": float64(1), "status_code": float64(200), "created_at": "2026-07-16T00:00:00Z", "payload_capture_mode": "metadata_only",
		}},
		"request_trace_events": []any{},
	}
}

func firstCallLog(snapshot map[string]any) map[string]any {
	return snapshot["call_logs"].([]any)[0].(map[string]any)
}

func deleteMoneyField(field string) func(map[string]any) {
	return func(snapshot map[string]any) { delete(firstCallLog(snapshot), field) }
}

func setMoneyField(field string, value any) func(map[string]any) {
	return func(snapshot map[string]any) { firstCallLog(snapshot)[field] = value }
}

func TestValidateSnapshotBoundsRejectsUnboundedCollections(t *testing.T) {
	testCases := []SnapshotFile{
		{RequestIDs: make([]string, MaxSnapshotRequestIDs+1)},
		{CaptureGaps: make([]SnapshotCaptureGap, MaxSnapshotCaptureGaps+1)},
		{TraceEvents: make([]SnapshotTraceEvent, MaxSnapshotEvents), CallLogs: make([]SnapshotCallLog, 1)},
	}
	for index, snapshot := range testCases {
		if err := validateSnapshotBounds(snapshot); err == nil {
			t.Fatalf("case %d unexpectedly accepted", index)
		}
	}
}

func TestSnapshotReadersRejectOversizedFileBeforeDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create oversized fixture: %v", err)
	}
	if err := file.Truncate(MaxSnapshotBytes + 1); err != nil {
		_ = file.Close()
		t.Fatalf("truncate oversized fixture: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close oversized fixture: %v", err)
	}
	if _, err := LoadSnapshot(path); err == nil {
		t.Fatal("LoadSnapshot accepted oversized file")
	}
	if _, err := DiscoverSnapshot(path, "test"); err == nil {
		t.Fatal("DiscoverSnapshot accepted oversized file")
	}
}

func TestValidateNormalizedEventBoundsRejectsOversizedEvent(t *testing.T) {
	events := []map[string]any{{"metadata": strings.Repeat("a", MaxNormalizedEventBytes)}}
	if err := validateNormalizedEventBounds(events); err == nil {
		t.Fatal("oversized normalized event unexpectedly accepted")
	}
}

func TestMappedEventIDIsSourceAndAdapterQualifiedOutsideLegacyPair(t *testing.T) {
	sourceRef := "unirouter:shared:call_logs:1"
	legacy := mappedEventID(SnapshotImportOptions{
		SourceSystemID:    DefaultSourceSystemID,
		AdapterManifestID: DefaultAdapterManifestID,
	}, sourceRef, "usage.tokens.recorded", "1")
	if legacy != deterministicUUID(sourceRef+"|usage.tokens.recorded|1") {
		t.Fatalf("legacy pair changed event ID: %s", legacy)
	}

	first := mappedEventID(SnapshotImportOptions{
		SourceSystemID:    "33333333-3333-4333-8333-333333333333",
		AdapterManifestID: "44444444-4444-4444-8444-444444444444",
	}, sourceRef, "usage.tokens.recorded", "1")
	secondSource := mappedEventID(SnapshotImportOptions{
		SourceSystemID:    "55555555-5555-4555-8555-555555555555",
		AdapterManifestID: "44444444-4444-4444-8444-444444444444",
	}, sourceRef, "usage.tokens.recorded", "1")
	secondAdapter := mappedEventID(SnapshotImportOptions{
		SourceSystemID:    "33333333-3333-4333-8333-333333333333",
		AdapterManifestID: "66666666-6666-4666-8666-666666666666",
	}, sourceRef, "usage.tokens.recorded", "1")
	if first == legacy || first == secondSource || first == secondAdapter || secondSource == secondAdapter {
		t.Fatalf("event IDs collided: legacy=%s first=%s source=%s adapter=%s", legacy, first, secondSource, secondAdapter)
	}
}

func TestImportSnapshotCaptureGap(t *testing.T) {
	path := writeTempSnapshot(t, `{
		"schema_version": "unirouter.prd5.xinsight.snapshot.v1",
		"generated_at": "2026-07-09T00:00:00Z",
		"source": "unirouter",
		"source_instance": "gap-test",
		"request_ids": ["req-gap-001"],
		"safety": {
			"selection_mode": "explicit_request_id_only",
			"raw_prompts_included": false,
			"raw_responses_included": false,
			"api_keys_included": false
		},
		"counts": {
			"requested_request_ids": 1,
			"call_logs": 1,
			"request_trace_events": 0,
			"capture_gaps": 1
		},
		"capture_gaps": [
			{
				"request_id": "req-gap-001",
				"source": "request_trace_events",
				"capture_gap": "not_available_from_source",
				"candidate_capture_gaps": ["sampled_out", "retention_expired", "not_instrumented"],
				"reason": "no request_trace_events row matched this request_id",
				"generated_by_snapshot_tool": true
			}
		],
		"call_logs": [
			{
				"source_ref": "unirouter:gap-test:call_logs:10",
				"id": 10,
				"request_id": "req-gap-001",
				"org_id": 1,
				"project_id": 2,
				"api_key_id": 3,
				"owner_user_id": 4,
				"key_type": "service",
				"model": "gpt-4o-mini",
				"requested_model": "gpt-4o-mini",
				"provider": "openai",
				"channel": "default",
				"prompt_tokens": 1,
				"completion_tokens": 1,
				"total_tokens": 2,
				"estimated_tokens": 0,
				"usage_source": "upstream",
				"usage_confidence": "1.000",
				"cost_cents": 1,
				"latency_ms": 100,
				"ttft_ms": 20,
				"status_code": 200,
				"upstream_error_present": false,
				"ip_address_was_present": false,
				"ip_address_redacted": true,
				"is_stream": false,
				"created_at": "2026-07-09T00:00:00Z",
				"payload_capture_mode": "metadata_only",
				"payload_capture_reason": "raw prompt/response export is disabled",
				"capture_mode": "native",
				"schema_version": "unirouter.evidence.v1",
				"source_component": "logging"
			}
		],
		"request_trace_events": []
	}`)

	report, err := ImportSnapshot(path, filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json"), SnapshotImportOptions{RerunCheck: true, EmitEvents: true})
	if err != nil {
		t.Fatalf("import snapshot: %v", err)
	}
	if report.EventsTotal != 2 || report.CaptureGapEvents != 1 {
		t.Fatalf("events=%d capture_gap_events=%d", report.EventsTotal, report.CaptureGapEvents)
	}
	if report.Accepted != 2 || report.RerunDuplicateNoop != 2 {
		t.Fatalf("accepted=%d rerun_duplicate=%d", report.Accepted, report.RerunDuplicateNoop)
	}
	foundGap := false
	for _, result := range report.Results {
		if result.CaptureGap && result.EventType == "gap.capture.detected" {
			foundGap = true
		}
	}
	if !foundGap {
		t.Fatalf("missing gap event: %+v", report.Results)
	}
	for _, event := range report.Events {
		if event["event_type"] != "gap.capture.detected" {
			continue
		}
		raw, ok := event["raw_source_excerpt"].(map[string]any)
		if !ok {
			t.Fatalf("gap event raw source excerpt missing: %+v", event)
		}
		candidates, ok := raw["candidate_capture_gaps"].([]string)
		if !ok || len(candidates) != 3 || candidates[0] != "sampled_out" {
			t.Fatalf("unexpected candidate capture gaps: %#v", raw["candidate_capture_gaps"])
		}
	}
}

func TestImportSnapshotModelPermissionDenied(t *testing.T) {
	report, err := ImportSnapshot(
		filepath.Join("..", "..", "..", "fixtures", "unirouter_snapshot", "model_permission_denied.json"),
		filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json"),
		SnapshotImportOptions{RerunCheck: true, EmitEvents: true},
	)
	if err != nil {
		t.Fatalf("import snapshot: %v", err)
	}
	if report.EventsTotal != 2 || report.CaptureGapEvents != 1 {
		t.Fatalf("events=%d capture_gap_events=%d", report.EventsTotal, report.CaptureGapEvents)
	}
	if report.Accepted != 2 || report.RerunDuplicateNoop != 2 {
		t.Fatalf("accepted=%d rerun_duplicate=%d", report.Accepted, report.RerunDuplicateNoop)
	}

	foundAuthDecision := false
	for _, event := range report.Events {
		if event["source_event_type"] != "auth.model_not_allowed" {
			continue
		}
		foundAuthDecision = true
		if event["event_type"] != "auth.model.rejected" {
			t.Fatalf("event_type=%v", event["event_type"])
		}
		if event["stage"] != "auth" {
			t.Fatalf("stage=%v", event["stage"])
		}
		decision, ok := event["decision"].(map[string]any)
		if !ok {
			t.Fatalf("decision missing: %+v", event)
		}
		if decision["decision_kind"] != "auth" || decision["decision_result"] != "rejected" || decision["decision_reason"] != "auth_error.model_not_allowed" {
			t.Fatalf("decision=%+v", decision)
		}
		raw, ok := event["raw_source_excerpt"].(map[string]any)
		if !ok {
			t.Fatalf("raw source excerpt missing: %+v", event)
		}
		if raw["capture_gap"] != "permission_denied" {
			t.Fatalf("capture_gap=%v", raw["capture_gap"])
		}
	}
	if !foundAuthDecision {
		t.Fatalf("missing auth decision event: %+v", report.Events)
	}
}

func TestImportSnapshotRefusesUnsafeSnapshot(t *testing.T) {
	path := writeTempSnapshot(t, `{
		"schema_version": "unirouter.prd5.xinsight.snapshot.v1",
		"source": "unirouter",
		"source_instance": "unsafe",
		"safety": {
			"raw_prompts_included": false,
			"raw_responses_included": false,
			"api_keys_included": false
		},
		"call_logs": [],
		"request_trace_events": [
			{
				"id": 1,
				"request_id": "req-unsafe",
				"org_id": 1,
				"event_type": "gateway.request.accepted",
				"stage": "gateway",
				"severity": "info",
				"metadata": {"prompt": "blocked"},
				"created_at": "2026-07-09T00:00:00Z",
				"payload_capture_mode": "metadata_only"
			}
		]
	}`)

	_, err := ImportSnapshot(path, filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json"), SnapshotImportOptions{})
	if err == nil {
		t.Fatalf("expected unsafe snapshot error")
	}
}

func writeTempSnapshot(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	return path
}
