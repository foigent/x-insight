package litellm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/investigation"
)

const succeededSourceFixture = `{
  "schema_version": "foigent.litellm.spend-log.v1",
  "request_id": "litellm_req_success_001",
  "status": "succeeded",
  "start_time": "2026-07-28T01:02:03.123456Z",
  "end_time": "2026-07-28T01:02:04.654321Z",
  "model": "gpt-4o-mini",
  "model_group": "general",
  "provider": "openai",
  "prompt_tokens": "12",
  "completion_tokens": "8",
  "total_tokens": "20",
  "reported_spend": "0.00001234",
  "currency": "USD",
  "cache_status": "miss"
}`

const failedSourceFixture = `{
  "schema_version": "foigent.litellm.spend-log.v1",
  "request_id": "litellm_req_failure_001",
  "status": "failed",
  "start_time": "2026-07-28T01:03:05.000001Z",
  "end_time": "2026-07-28T01:03:05.250001Z",
  "model": "gpt-4o-mini",
  "model_group": "general",
  "provider": "openai",
  "prompt_tokens": "0",
  "completion_tokens": "0",
  "total_tokens": "0",
  "reported_spend": "0",
  "currency": "USD",
  "cache_status": "unknown"
}`

const cacheHitSourceFixture = `{
  "schema_version": "foigent.litellm.spend-log.v1",
  "request_id": "litellm_req_cache_hit_001",
  "status": "succeeded",
  "start_time": "2026-07-28T01:04:06.000002Z",
  "end_time": "2026-07-28T01:04:06.125002Z",
  "model": "gpt-4o-mini",
  "model_group": "general",
  "provider": "openai",
  "prompt_tokens": "9",
  "completion_tokens": "3",
  "total_tokens": "12",
  "reported_spend": "0.0000012",
  "currency": "USD",
  "cache_status": "hit"
}`

func TestLiteLLMSourceGoldenEnvelopeAndM0Projection(t *testing.T) {
	source, err := DecodeSourceRecord([]byte(succeededSourceFixture))
	if err != nil {
		t.Fatalf("decode Source: %v", err)
	}
	if source.SourceHash != "sha256:7a9056fa6abfa1f44262bcba36734bcbdb6e3e5fd48ce79023c090b19e073b80" {
		t.Fatalf("unexpected Source hash: %s", source.SourceHash)
	}
	projector, err := NewProjector()
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	mapping := MappingContext{
		ProducerInstance: "litellm-development",
		OrgRef:           "101",
		ProjectRef:       "202",
	}
	envelopeBytes, envelope, connectorErr := BuildEnvelope(
		source,
		mapping,
		projector.Binding(),
	)
	if connectorErr != nil {
		t.Fatalf("build Envelope: %v", connectorErr)
	}
	if len(envelope.HashInputBytes()) != 4663 {
		t.Fatalf("unexpected Envelope hash-input bytes: %d", len(envelope.HashInputBytes()))
	}
	if len(envelopeBytes) != 4753 {
		t.Fatalf("unexpected complete Envelope bytes: %d", len(envelopeBytes))
	}
	if envelope.EnvelopeHash != "sha256:0c595fcb3066288b16d6fc12e375912d0bcb421ba27c7a442d6b22d898a5e5a7" {
		t.Fatalf("unexpected Envelope hash: %s", envelope.EnvelopeHash)
	}
	item, connectorErr := projector.Project(
		contextpkg(),
		source,
		mapping,
		connector.ProjectionContext{
			IngestedAt: time.Date(2026, 7, 28, 2, 0, 0, 0, time.UTC),
		},
	)
	if connectorErr != nil {
		t.Fatalf("project Source: %v", connectorErr)
	}
	if len(item.Events) != 5 {
		t.Fatalf("unexpected M0 event count: %d", len(item.Events))
	}
	if item.Provenance.IntegrationMode != investigation.IntegrationConnectorPull ||
		item.Provenance.AssuranceMode != investigation.AssuranceContractedPull ||
		item.Provenance.SourceWatermark != "" ||
		item.Provenance.ImportWatermark != "" {
		t.Fatalf("unexpected Connector provenance: %+v", item.Provenance)
	}
	if item.Events[0]["event_type"] != "model.request.succeeded" {
		t.Fatalf("unexpected main event type: %v", item.Events[0]["event_type"])
	}
	usage := item.Events[0]["usage"].(map[string]any)
	if _, present := usage["currency"]; present {
		t.Fatal("USD was projected into top-level M0 money fields")
	}
	extensions := usage["extensions"].(map[string]any)
	money := extensions["source_reported_money"].(map[string]any)
	if money["currency"] != "USD" || money["amount"] != "0.00001234" {
		t.Fatalf("unexpected source-reported money: %#v", money)
	}
	validator, err := evidence.NewSchemaValidator("../../../schemas/m0/evidence-event.schema.json")
	if err != nil {
		t.Fatalf("load M0 schema: %v", err)
	}
	for index, event := range item.Events {
		if err := validator.Validate(event); err != nil {
			raw, _ := json.Marshal(event)
			t.Fatalf("M0 event %d invalid: %v\n%s", index, err, raw)
		}
		if findings := evidence.DetectSensitive(event["raw_source_excerpt"]); len(findings) != 0 {
			t.Fatalf("M0 event %d contains sensitive excerpt: %#v", index, findings)
		}
	}
}

func TestBuildEnvelopeRejectsStructurallyValidRegistrationDrift(t *testing.T) {
	source, err := DecodeSourceRecord([]byte(succeededSourceFixture))
	if err != nil {
		t.Fatalf("decode Source: %v", err)
	}
	binding := Binding()
	binding.Protocol.ReleaseDigest = "sha256:" + strings.Repeat("0", 64)
	if err := binding.Validate(); err != nil {
		t.Fatalf("test binding drift is not structurally valid: %v", err)
	}
	envelopeBytes, _, connectorErr := BuildEnvelope(
		source,
		MappingContext{
			ProducerInstance: "litellm-development",
			OrgRef:           "101",
			ProjectRef:       "202",
		},
		binding,
	)
	if connectorErr == nil ||
		connectorErr.Category != connector.ErrorContract ||
		connectorErr.Code != "foigent.litellm.binding_invalid" ||
		connectorErr.SafeMessage != "LiteLLM projection binding is invalid" ||
		len(envelopeBytes) != 0 {
		t.Fatalf("binding drift result bytes=%d err=%+v", len(envelopeBytes), connectorErr)
	}
}

func TestLiteLLMProtocolGoldenVectorsRemainExactThroughM0(t *testing.T) {
	tests := []struct {
		name           string
		sourceFixture  string
		sourceHash     string
		hashInputBytes int
		envelopeHash   string
		idempotencyKey string
		eventType      string
		outcomeStatus  string
		cacheStatus    string
		reportedSpend  string
		m0Identity     []string
	}{
		{
			name:           "succeeded",
			sourceFixture:  succeededSourceFixture,
			sourceHash:     "sha256:7a9056fa6abfa1f44262bcba36734bcbdb6e3e5fd48ce79023c090b19e073b80",
			hashInputBytes: 4663,
			envelopeHash:   "sha256:0c595fcb3066288b16d6fc12e375912d0bcb421ba27c7a442d6b22d898a5e5a7",
			idempotencyKey: "sha256:ba906ad95206034be58b9b8801fa8c3f176fa2f188c8443b31d37962f172f235",
			eventType:      "model.request.succeeded",
			outcomeStatus:  "success",
			cacheStatus:    "miss",
			reportedSpend:  "0.00001234",
			m0Identity: []string{
				"c8abb539-c452-5c30-908a-793ac371f9aa sha256:921810662346b32c056b67a5544f43960db09005a6baf50b3b6ab8a70abc4565",
				"7a792e45-dfb2-5a11-8727-884d71402ea1 sha256:5025786553b42ae82ed3d72d9f95f2305bc1748c979e63828abd7386d2dc8a6f",
				"a905a72a-8add-5c9c-86d1-2f0d1109f27a sha256:0b0daf025a27f99731722acfd873f59270ae795f5ab4d8ef59fca702f32ffe62",
				"3621dbe8-24c4-5b32-979f-3c108283043a sha256:ec35e832929bb691125f034c62b36b7aa228d19b2d5c8da41963a7097b6203ed",
				"64f2e891-b8de-5dc7-b992-0bd0aa28bedb sha256:42cf36953dd30e942640827f025812964c16c03b7e18d07f90825768c2c0a6f9",
			},
		},
		{
			name:           "failed",
			sourceFixture:  failedSourceFixture,
			sourceHash:     "sha256:2064e93871533369a2dc06bf484825b31decb87e0448b3f7ba58ce7f32f90b2b",
			hashInputBytes: 4652,
			envelopeHash:   "sha256:8c95fe0461c9372b033554f2828782eb136ebaa2ffa2e1e55111a4d2493df78a",
			idempotencyKey: "sha256:69e2b4ad8345ac51281ef50635a00d102e7691ba52c93c438304323873ec4ec6",
			eventType:      "model.request.failed",
			outcomeStatus:  "failure",
			cacheStatus:    "unknown",
			reportedSpend:  "0",
			m0Identity: []string{
				"955d1388-d65e-521b-a55c-32ad121a41ff sha256:9efbac2683f86a39051cb05084359ac6830a57e0a471c0885c843905f5eb7adb",
				"08c693bd-3077-5066-b7c9-c04cc27eea8e sha256:aadb337e413b84f353b973690121bee3eb3a5eee193ec6e308b40dd181257524",
				"fd90c262-1503-515f-b217-df19720df0fb sha256:5326c7113d3d5b7984c885c82992e55e6d36c9d07be113ce67aaf62cf204228d",
				"4dce0af3-9f0f-5760-ad04-e64e2b423da4 sha256:0603785b41f3db6d564a31e0b799ba9c19bb9f7f85d59a7bb00b222f04f4aa47",
				"bb705a8e-f68d-599d-97d0-26517f1b90c3 sha256:cf7bce766f16775b8531253719db9a713333d64d1b7a6b90d45f85b5fa989745",
			},
		},
		{
			name:           "cache_hit",
			sourceFixture:  cacheHitSourceFixture,
			sourceHash:     "sha256:fd51489ce4e67df17185df54dc76b50f66221437461cfbec0984391c0d96eddd",
			hashInputBytes: 4690,
			envelopeHash:   "sha256:38fa662294774e04d56977d4582a9824a07fafd99464fb55ebb737066d4ddfe0",
			idempotencyKey: "sha256:d2faccb46ffb9aca9804a27b7438224606026229dee5a1f8e9dc3ff0545a41c4",
			eventType:      "model.request.succeeded",
			outcomeStatus:  "success",
			cacheStatus:    "hit",
			reportedSpend:  "0.0000012",
			m0Identity: []string{
				"2a238b38-6af6-552e-af21-556fed5d103f sha256:267872008c8f141b8a86be3fbd075f0dcece8540868fa09ed6e72fa277b851f3",
				"e63639a6-9a45-5e61-8650-e93bfcb6522b sha256:20b1a3fad801b9780e5efb2eb5b4a538df465f4372ab60dc9a1074b3cdaf91b3",
				"da1c1459-9d06-5ae5-bb8a-760e1585ab21 sha256:016ff1cd5bb9b4b84befe1823a4f87cd9d5a185d80cc32f62d0401103693fd64",
				"b99a3662-13f4-5073-a0f0-7d50940591de sha256:58304fa739a15aad3cdba10e06ced1c2f8e4bfe7baebdc28bc43df1672ef203e",
				"9981dac5-450e-5a21-b4a8-5d633bca412a sha256:56d4b412a441bda040decc1b1007c08e4b1c4755d4654c76c3c78726671ba25d",
			},
		},
	}
	projector, err := NewProjector()
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	mapping := MappingContext{
		ProducerInstance: "litellm-development",
		OrgRef:           "101",
		ProjectRef:       "202",
	}
	ingestedAt := time.Date(2026, 7, 28, 2, 0, 0, 0, time.UTC)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, err := DecodeSourceRecord([]byte(test.sourceFixture))
			if err != nil {
				t.Fatalf("decode Source: %v", err)
			}
			if source.SourceHash != test.sourceHash {
				t.Fatalf("Source hash=%s want=%s", source.SourceHash, test.sourceHash)
			}
			_, envelope, connectorErr := BuildEnvelope(
				source,
				mapping,
				projector.Binding(),
			)
			if connectorErr != nil {
				t.Fatalf("build Envelope: %v", connectorErr)
			}
			if len(envelope.HashInputBytes()) != test.hashInputBytes ||
				envelope.EnvelopeHash != test.envelopeHash ||
				envelope.IdempotencyKey != test.idempotencyKey {
				t.Fatalf(
					"Envelope bytes/hash/idempotency=%d/%s/%s want=%d/%s/%s",
					len(envelope.HashInputBytes()),
					envelope.EnvelopeHash,
					envelope.IdempotencyKey,
					test.hashInputBytes,
					test.envelopeHash,
					test.idempotencyKey,
				)
			}
			item, connectorErr := projector.Project(
				context.Background(),
				source,
				mapping,
				connector.ProjectionContext{IngestedAt: ingestedAt},
			)
			if connectorErr != nil {
				t.Fatalf("project M0: %v", connectorErr)
			}
			if len(item.Events) != 5 ||
				item.Events[0]["event_type"] != test.eventType {
				t.Fatalf(
					"M0 count/type=%d/%v want=5/%s",
					len(item.Events),
					item.Events[0]["event_type"],
					test.eventType,
				)
			}
			m0Identity := make([]string, 0, len(item.Events))
			for _, event := range item.Events {
				m0Identity = append(
					m0Identity,
					event["id"].(string)+" "+event["event_hash"].(string),
				)
			}
			if !slices.Equal(m0Identity, test.m0Identity) {
				t.Fatalf("M0 identities=%q", m0Identity)
			}
			usage := item.Events[0]["usage"].(map[string]any)
			extensions := usage["extensions"].(map[string]any)
			money := extensions["source_reported_money"].(map[string]any)
			outcome := item.Events[0]["outcome"].(map[string]any)
			if extensions["cache_status"] != test.cacheStatus ||
				money["currency"] != "USD" ||
				money["amount"] != test.reportedSpend ||
				outcome["outcome_status"] != test.outcomeStatus {
				t.Fatalf(
					"M0 cache/money/outcome=%v/%#v/%#v",
					extensions["cache_status"],
					money,
					outcome,
				)
			}
			for index, event := range item.Events[1:] {
				if event["event_type"] != "gap.capture.detected" {
					t.Fatalf("gap %d event_type=%v", index, event["event_type"])
				}
			}
		})
	}
}

func TestLiteLLMM0OpaqueIdentityUsesInjectiveSourceReferenceAndEventIdentity(
	t *testing.T,
) {
	const (
		instance1 = "b:spend_logs:x:spend_logs:a:model.request.succeeded:a:model.request.succeeded:x"
		request1  = "a"
		instance2 = "b"
		request2  = "x:spend_logs:a:model.request.succeeded:a"
	)
	legacyKey := func(instance, requestID string) string {
		sourceRef := fmt.Sprintf("litellm:%s:spend_logs:%s", instance, requestID)
		return fmt.Sprintf(
			"adapter-litellm:%s:%s:%s:%s",
			SourceSystemID,
			sourceRef,
			"model.request.succeeded",
			requestID,
		)
	}
	if legacyKey(instance1, request1) != legacyKey(instance2, request2) {
		t.Fatal("adversarial opaque tuple no longer reproduces the legacy delimiter collision")
	}

	project := func(instance, requestID string) []map[string]any {
		var raw SourceRecord
		if err := json.Unmarshal([]byte(succeededSourceFixture), &raw); err != nil {
			t.Fatal(err)
		}
		raw.RequestID = requestID
		source, err := canonicalSourceRecord(raw)
		if err != nil {
			t.Fatal(err)
		}
		projector, err := NewProjector()
		if err != nil {
			t.Fatal(err)
		}
		item, connectorErr := projector.Project(
			context.Background(),
			source,
			MappingContext{
				ProducerInstance: instance,
				OrgRef:           "101",
				ProjectRef:       "202",
			},
			connector.ProjectionContext{
				IngestedAt: time.Date(2026, 7, 28, 2, 0, 0, 0, time.UTC),
			},
		)
		if connectorErr != nil {
			t.Fatal(connectorErr)
		}
		return item.Events
	}
	first := project(instance1, request1)
	firstReplay := project(instance1, request1)
	second := project(instance2, request2)
	unicode := project("实例:α", "请求:β")
	for index := range first {
		for _, field := range []string{"source_ref", "id", "idempotency_key"} {
			if first[index][field] != firstReplay[index][field] {
				t.Fatalf("same tuple event %d %s is unstable", index, field)
			}
			if first[index][field] == second[index][field] {
				t.Fatalf("adversarial tuple event %d %s collided", index, field)
			}
		}
	}
	for _, event := range append(first, unicode...) {
		sourceRef := event["source_ref"].(string)
		parentRef := strings.SplitN(sourceRef, ":gap:", 2)[0]
		segments := strings.Split(parentRef, ":")
		if len(segments) != 6 ||
			segments[0] != "litellm" ||
			segments[1] != "v1" ||
			segments[2] != "instance" ||
			segments[4] != "request" {
			t.Fatalf("source_ref is not unambiguously segmented: %q", sourceRef)
		}
		if _, err := base64.RawURLEncoding.DecodeString(segments[3]); err != nil {
			t.Fatalf("source_ref producer segment is not reversible: %v", err)
		}
		if _, err := base64.RawURLEncoding.DecodeString(segments[5]); err != nil {
			t.Fatalf("source_ref request segment is not reversible: %v", err)
		}
	}
}

func TestLiteLLMGapScopeMustExactlyMatchParentTenantScope(t *testing.T) {
	source, err := DecodeSourceRecord([]byte(succeededSourceFixture))
	if err != nil {
		t.Fatal(err)
	}
	projector, err := NewProjector()
	if err != nil {
		t.Fatal(err)
	}
	tenant := "tenant-development"
	mapping := MappingContext{
		ProducerInstance: "litellm-development",
		TenantRef:        &tenant,
		OrgRef:           "101",
		ProjectRef:       "202",
	}
	envelopeBytes, _, connectorErr := BuildEnvelope(
		source,
		mapping,
		projector.Binding(),
	)
	if connectorErr != nil {
		t.Fatal(connectorErr)
	}
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "missing tenant",
			mutate: func(scope map[string]any) {
				delete(scope, "tenant_ref")
			},
		},
		{
			name: "wrong tenant",
			mutate: func(scope map[string]any) {
				scope["tenant_ref"] = "other-tenant"
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var raw map[string]any
			decoder := json.NewDecoder(bytes.NewReader(envelopeBytes))
			decoder.UseNumber()
			if err := decoder.Decode(&raw); err != nil {
				t.Fatal(err)
			}
			payload := raw["payload"].(map[string]any)
			gaps := payload["capture_gaps"].([]any)
			scope := gaps[0].(map[string]any)["scope"].(map[string]any)
			test.mutate(scope)
			delete(raw["integrity"].(map[string]any), "envelope_hash")
			_, envelope, connectorErr := connector.BuildCanonicalEnvelope(
				raw,
				projector.Binding(),
			)
			if connectorErr != nil {
				t.Fatalf("rebuild mutated Envelope: %v", connectorErr)
			}
			profile, err := NewProfile(projector.Binding())
			if err != nil {
				t.Fatal(err)
			}
			if _, connectorErr := profile.Normalize(
				context.Background(),
				envelope,
				connector.ProjectionContext{
					IngestedAt: time.Date(
						2026,
						7,
						28,
						2,
						0,
						0,
						0,
						time.UTC,
					),
				},
			); connectorErr == nil ||
				connectorErr.Code != "foigent.litellm.envelope_semantics_invalid" {
				t.Fatalf("gap scope mismatch error=%v", connectorErr)
			}
		})
	}
}

func TestSanitizeUpstreamRowDropsSensitiveAndRejectsInvalidPrecision(t *testing.T) {
	row := map[string]json.RawMessage{
		"request_id":           json.RawMessage(`"request-1"`),
		"status":               json.RawMessage(`null`),
		"startTime":            json.RawMessage(`"2026-07-28T01:02:03.123456Z"`),
		"endTime":              json.RawMessage(`"2026-07-28T01:02:04.654321Z"`),
		"model":                json.RawMessage(`"model-a"`),
		"prompt_tokens":        json.RawMessage(`12`),
		"completion_tokens":    json.RawMessage(`8`),
		"total_tokens":         json.RawMessage(`20`),
		"spend":                json.RawMessage(`1.2300`),
		"cache_hit":            json.RawMessage(`"False"`),
		"api_key":              json.RawMessage(`"never-retain"`),
		"metadata":             json.RawMessage(`{"prompt":"never-retain"}`),
		"unknown_future_field": json.RawMessage(`"ignored"`),
	}
	sanitized, err := SanitizeUpstreamRow(row)
	if err != nil {
		t.Fatalf("sanitize row: %v", err)
	}
	if sanitized.Record.ReportedSpend != "1.23" ||
		sanitized.Record.Status != "succeeded" ||
		sanitized.Record.CacheStatus != "miss" {
		t.Fatalf("unexpected sanitized record: %#v", sanitized.Record)
	}
	if strings.Contains(string(sanitized.CanonicalBytes), "never-retain") {
		t.Fatal("sensitive upstream bytes survived sanitization")
	}
	if _, retained := row["api_key"]; !retained {
		t.Fatal("sanitization mutated the caller-owned raw row")
	}
	row["endTime"] = json.RawMessage(`"2026-07-28T01:02:04.654321999Z"`)
	if _, err := SanitizeUpstreamRow(row); err == nil {
		t.Fatal("sub-microsecond precision loss accepted")
	}
}

func TestSanitizeUpstreamRowRejectsMissingControlledFields(t *testing.T) {
	base := map[string]json.RawMessage{
		"request_id":        json.RawMessage(`"request-1"`),
		"status":            json.RawMessage(`null`),
		"startTime":         json.RawMessage(`"2026-07-28T01:02:03.123456Z"`),
		"endTime":           json.RawMessage(`"2026-07-28T01:02:04.654321Z"`),
		"model":             json.RawMessage(`"model-a"`),
		"prompt_tokens":     json.RawMessage(`12`),
		"completion_tokens": json.RawMessage(`8`),
		"total_tokens":      json.RawMessage(`20`),
		"spend":             json.RawMessage(`0`),
		"cache_hit":         json.RawMessage(`null`),
	}
	for _, missing := range []string{"status", "cache_hit"} {
		row := make(map[string]json.RawMessage, len(base))
		for key, value := range base {
			row[key] = value
		}
		delete(row, missing)
		if _, err := SanitizeUpstreamRow(row); err == nil {
			t.Fatalf("missing %s accepted", missing)
		}
	}
}

func TestSanitizeUpstreamCacheHitCompatibility(t *testing.T) {
	base := map[string]json.RawMessage{
		"request_id":        json.RawMessage(`"request-1"`),
		"status":            json.RawMessage(`"failure"`),
		"startTime":         json.RawMessage(`"2026-07-28T01:02:03.123456Z"`),
		"endTime":           json.RawMessage(`"2026-07-28T01:02:04.654321Z"`),
		"model":             json.RawMessage(`"model-a"`),
		"prompt_tokens":     json.RawMessage(`0`),
		"completion_tokens": json.RawMessage(`0`),
		"total_tokens":      json.RawMessage(`0`),
		"spend":             json.RawMessage(`0`),
	}
	tests := []struct {
		name        string
		raw         string
		want        string
		wantInvalid bool
	}{
		{name: "null", raw: `null`, want: "unknown"},
		{name: "empty", raw: `""`, want: "unknown"},
		{name: "pinned LiteLLM none sentinel", raw: `"None"`, want: "unknown"},
		{name: "boolean true", raw: `true`, want: "hit"},
		{name: "boolean false", raw: `false`, want: "miss"},
		{name: "string true", raw: `"True"`, want: "hit"},
		{name: "string false", raw: `"False"`, want: "miss"},
		{name: "lowercase none", raw: `"none"`, wantInvalid: true},
		{name: "string null", raw: `"null"`, wantInvalid: true},
		{name: "uppercase none", raw: `"NONE"`, wantInvalid: true},
		{name: "other string", raw: `"unknown"`, wantInvalid: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			row := make(map[string]json.RawMessage, len(base)+1)
			for key, value := range base {
				row[key] = value
			}
			row["cache_hit"] = json.RawMessage(test.raw)
			sanitized, err := SanitizeUpstreamRow(row)
			if test.wantInvalid {
				if err == nil {
					t.Fatal("invalid cache_hit lexical form was accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("sanitize cache_hit compatibility vector: %v", err)
			}
			if sanitized.Record.CacheStatus != test.want {
				t.Fatalf(
					"cache_status=%q, want %q",
					sanitized.Record.CacheStatus,
					test.want,
				)
			}
		})
	}
}

func TestSanitizeUpstreamCacheHitNoneHasStableUnknownSourceIdentity(t *testing.T) {
	raw := func(cacheHit string) []byte {
		return []byte(strings.Replace(
			`{
			  "request_id": "request-1",
			  "status": "failure",
			  "startTime": "2026-07-28T01:02:03.123456Z",
			  "endTime": "2026-07-28T01:02:04.654321Z",
			  "model": "model-a",
			  "prompt_tokens": 0,
			  "completion_tokens": 0,
			  "total_tokens": 0,
			  "spend": 0,
			  "cache_hit": CACHE_HIT
			}`,
			"CACHE_HIT",
			cacheHit,
			1,
		))
	}
	none, err := SanitizeUpstreamRowBytes(raw(`"None"`))
	if err != nil {
		t.Fatalf("sanitize pinned LiteLLM None sentinel: %v", err)
	}
	replay, err := SanitizeUpstreamRowBytes(raw(`"None"`))
	if err != nil {
		t.Fatalf("replay pinned LiteLLM None sentinel: %v", err)
	}
	nullEquivalent, err := SanitizeUpstreamRowBytes(raw(`null`))
	if err != nil {
		t.Fatalf("sanitize null cache state: %v", err)
	}
	if none.Record.CacheStatus != "unknown" ||
		!bytes.Equal(none.CanonicalBytes, replay.CanonicalBytes) ||
		none.SourceHash != replay.SourceHash ||
		!bytes.Equal(none.CanonicalBytes, nullEquivalent.CanonicalBytes) ||
		none.SourceHash != nullEquivalent.SourceHash {
		t.Fatal("None cache sentinel did not converge to stable unknown Source identity")
	}
}

func TestDecodeSourceRecordRejectsProhibitedAndUnknownFields(t *testing.T) {
	for _, mutation := range []string{
		strings.Replace(succeededSourceFixture, `"cache_status": "miss"`, `"cache_status": "miss", "api_key": "secret"`, 1),
		strings.Replace(succeededSourceFixture, `"cache_status": "miss"`, `"cache_status": "miss", "future": true`, 1),
	} {
		if _, err := DecodeSourceRecord([]byte(mutation)); err == nil {
			t.Fatal("unsafe or unknown Source field accepted")
		}
	}
}

func TestLiteLLMJSONRejectsUnpairedUTF16SurrogatesAtRawAndSourceBoundaries(
	t *testing.T,
) {
	rawTemplate := `{
	  "request_id": "REQUEST_ID",
	  "status": null,
	  "startTime": "2026-07-28T01:02:03.123456Z",
	  "endTime": "2026-07-28T01:02:04.654321Z",
	  "model": "model-a",
	  "prompt_tokens": 12,
	  "completion_tokens": 8,
	  "total_tokens": 20,
	  "spend": 0.000001,
	  "cache_hit": false
	}`
	for _, surrogate := range []string{`\ud800`, `\udfff`} {
		t.Run(surrogate, func(t *testing.T) {
			raw := strings.Replace(rawTemplate, "REQUEST_ID", "bad-"+surrogate, 1)
			if _, err := SanitizeUpstreamRowBytes([]byte(raw)); err == nil {
				t.Fatal("unpaired surrogate reached raw Source sanitization")
			}
			source := strings.Replace(
				succeededSourceFixture,
				"litellm_req_success_001",
				"bad-"+surrogate,
				1,
			)
			if _, err := DecodeSourceRecord([]byte(source)); err == nil {
				t.Fatal("unpaired surrogate reached Source record decoding")
			}
		})
	}
}

func TestLiteLLMOpaqueNonNFCBytesRemainExact(t *testing.T) {
	const requestID = "request-e\u0301"
	raw := strings.Replace(
		`{
		  "request_id": "REQUEST_ID",
		  "status": null,
		  "startTime": "2026-07-28T01:02:03.123456Z",
		  "endTime": "2026-07-28T01:02:04.654321Z",
		  "model": "model-e\u0301",
		  "prompt_tokens": 12,
		  "completion_tokens": 8,
		  "total_tokens": 20,
		  "spend": 0.000001,
		  "cache_hit": false
		}`,
		"REQUEST_ID",
		requestID,
		1,
	)
	sanitized, err := SanitizeUpstreamRowBytes([]byte(raw))
	if err != nil {
		t.Fatalf("sanitize non-NFC opaque bytes: %v", err)
	}
	if sanitized.Record.RequestID != requestID ||
		sanitized.Record.Model != "model-e\u0301" ||
		strings.Contains(sanitized.Record.RequestID, "\u00e9") {
		t.Fatalf(
			"opaque bytes were normalized: request=%q model=%q",
			sanitized.Record.RequestID,
			sanitized.Record.Model,
		)
	}
	decoded, err := DecodeSourceRecord(sanitized.CanonicalBytes)
	if err != nil {
		t.Fatalf("decode canonical non-NFC Source: %v", err)
	}
	if decoded.Record.RequestID != requestID ||
		!bytes.Equal(decoded.CanonicalBytes, sanitized.CanonicalBytes) ||
		decoded.SourceHash != sanitized.SourceHash {
		t.Fatal("non-NFC Source bytes/hash did not survive exact round trip")
	}
}

func TestCanonicalReportedDecimalVectors(t *testing.T) {
	cases := []struct {
		input     string
		canonical string
		code      string
		valid     bool
	}{
		{"0", "0", "", true},
		{"1.2300", "1.23", "", true},
		{"1.2e-7", "0.00000012", "", true},
		{"-0.1", "", "negative_reported_spend", false},
		{"NaN", "", "invalid_reported_spend", false},
	}
	for _, item := range cases {
		canonical, code, valid := CanonicalReportedDecimal(item.input)
		if canonical != item.canonical || code != item.code || valid != item.valid {
			t.Fatalf("%q => %q/%q/%v", item.input, canonical, code, valid)
		}
	}
}

func TestCanonicalReportedDecimalBoundsExponentBeforeAllocation(t *testing.T) {
	cases := []struct {
		input     string
		canonical string
		code      string
		valid     bool
	}{
		{"9e79", "9" + strings.Repeat("0", 79), "", true},
		{"9e80", "", "reported_spend_too_large", false},
		{"1e-78", "0." + strings.Repeat("0", 77) + "1", "", true},
		{"1e-79", "", "reported_spend_too_large", false},
		{"1e2147483647", "", "reported_spend_too_large", false},
		{"1e-2147483648", "", "reported_spend_too_large", false},
	}
	for _, item := range cases {
		t.Run(item.input, func(t *testing.T) {
			canonical, code, valid := CanonicalReportedDecimal(item.input)
			if canonical != item.canonical || code != item.code || valid != item.valid {
				t.Fatalf(
					"%q => %q/%q/%v, want %q/%q/%v",
					item.input,
					canonical,
					code,
					valid,
					item.canonical,
					item.code,
					item.valid,
				)
			}
		})
	}
}

func contextpkg() context.Context {
	return context.Background()
}
