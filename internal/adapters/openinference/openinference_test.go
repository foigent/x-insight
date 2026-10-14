package openinference

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/evidence"
)

func TestOpenInferenceOTLPProjectionIsLosslessMetadataOnlyAndDeterministic(t *testing.T) {
	payload := readContractFixture(t, "reference-otlp-export.v1.json")
	batch, err := SanitizeOTLPExport(payload)
	if err != nil {
		t.Fatalf("sanitize OTLP export: %v", err)
	}
	if len(batch.Records) != 1 || batch.SkippedNonLLM != 0 {
		t.Fatalf("unexpected sanitized batch: %+v", batch)
	}
	record := batch.Records[0]
	if bytes.Contains(record.CanonicalBytes, []byte("MUST_NOT_SURVIVE_SANITIZATION")) {
		t.Fatal("raw input/output survived metadata allowlist")
	}
	if record.Record.TotalTokens != "11" ||
		record.Record.SessionID != "session-source-01" ||
		record.Record.Model != "model-a" {
		t.Fatalf("sanitized record lost allowlisted facts: %+v", record.Record)
	}
	projector, err := NewProjector()
	if err != nil {
		t.Fatalf("create projector: %v", err)
	}
	mapping := MappingContext{
		ProducerInstance: "otel-collector-prod-a",
		OrgRef:           "1",
		ProjectRef:       "7",
	}
	projectionContext := connector.ProjectionContext{
		IngestedAt: time.Date(2026, 7, 31, 2, 1, 2, 3000, time.UTC),
	}
	first, connectorErr := projector.Project(context.Background(), record, mapping, projectionContext)
	if connectorErr != nil {
		t.Fatalf("project OTLP record: %+v", connectorErr)
	}
	second, connectorErr := projector.Project(context.Background(), record, mapping, projectionContext)
	if connectorErr != nil {
		t.Fatalf("project OTLP record again: %+v", connectorErr)
	}
	firstBytes, _ := json.Marshal(first.Events)
	secondBytes, _ := json.Marshal(second.Events)
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("OpenInference M0 projection is not deterministic")
	}
	if len(first.Events) != 5 {
		t.Fatalf("main Event plus four explicit Gaps expected, got %d", len(first.Events))
	}
	validator, err := evidence.NewSchemaValidator(filepath.Join(repoRoot(t), "schemas/m0/evidence-event.schema.json"))
	if err != nil {
		t.Fatalf("load M0 schema: %v", err)
	}
	for index, event := range first.Events {
		if err := validator.Validate(event); err != nil {
			t.Fatalf("M0 event %d invalid: %v\n%v", index, err, event)
		}
		if event["source_type"] != "openinference" ||
			event["adapter_manifest_id"] != AdapterManifestID {
			t.Fatalf("M0 registry identity mismatch: %v", event)
		}
	}
	main := first.Events[0]
	correlation := main["correlation"].(map[string]any)
	if correlation["trace_id"] != record.Record.TraceID ||
		correlation["span_id"] != record.Record.SpanID ||
		correlation["session_id"] != record.Record.SessionID {
		t.Fatalf("trace/session correlation lost: %v", correlation)
	}
	if main["payload_capture_mode"] != "metadata_only" ||
		main["capture_mode"] != "openinference" {
		t.Fatalf("capture modes collapsed: %v", main)
	}
	if first.Provenance.TimelineComplete {
		t.Fatal("bounded OTLP page must not claim complete execution timeline")
	}
	if first.Provenance.IntegrationMode != "connector_bounded_offline" ||
		first.Provenance.AssuranceMode != "bounded_offline_import" ||
		first.Provenance.SourceWatermark != "" ||
		first.Provenance.ImportWatermark != "" {
		t.Fatalf("bounded-offline provenance boundary changed: %+v", first.Provenance)
	}
	expectedIdentity := [][2]string{
		{"c70b878d-21a0-57fa-90c7-92c989907e37", "sha256:dba2c158b78533ab174ece5bae4b7298968b3e9fa2d398d3ab6a2af134193a46"},
		{"3b54f6a2-f303-55d7-8a5c-8808a9a61ed3", "sha256:62e94aba0f167b21deaa8d87def230a0df7b938b81ebcb398e20a757e5a8cfba"},
		{"d295a54e-bd67-572f-aadf-7b7ae3e2abba", "sha256:4e3f1e8964aa1119450af0c7842fc724be836efcadadf9258f6349d9df164116"},
		{"daa55d31-a996-53c9-8454-6851cf246cb1", "sha256:a164079825e5ae1d5a70ef0b543f2766ddbd5c8069ac4d4bc4375b5b6cecafc0"},
		{"264babaf-3080-5fea-b391-d1c22cd50c33", "sha256:00eac75af3083ddcad31c1d0f4abd217dd8d4bcf614dafd4d6174e61f958cd67"},
	}
	for index, expected := range expectedIdentity {
		if first.Events[index]["id"] != expected[0] || first.Events[index]["event_hash"] != expected[1] {
			t.Fatalf("OpenInference compatibility identity %d changed: %v", index, first.Events[index])
		}
	}
}

func TestOpenInferenceSourceNeutralBindingV2IsExactAndHasNoCursorIdentity(t *testing.T) {
	payload := readContractFixture(t, "xinsight-projection-binding.v2.json")
	parsed, err := connector.ParseSourceNeutralManifestBinding(payload)
	if err != nil {
		t.Fatalf("parse source-neutral binding: %v", err)
	}
	if err := connector.ValidateExactSourceNeutralRegistration(parsed, Binding()); err != nil {
		expected, _ := json.MarshalIndent(Binding(), "", "  ")
		t.Fatalf("exact source-neutral binding mismatch: %v\nparsed=%s\nexpected=%s", err, payload, expected)
	}
	if bytes.Contains(payload, []byte("cursor_key")) ||
		parsed.Protocol.ReleaseDigest != ProtocolReleaseSHA ||
		parsed.ReferenceManifest.SHA256 != "sha256:363123d5837741ed78c134e305593d3fd0fdf68040cebff068d3f76d5bc62a1b" {
		t.Fatalf("source-neutral binding contains a pull identity or wrong authority: %+v", parsed)
	}
}

func TestOpenInferenceMissingFactsProduceFirstClassGapsWithoutZeroSubstitution(t *testing.T) {
	payload := []byte(`{
	  "resourceSpans":[{
	    "resource":{"attributes":[]},
	    "scopeSpans":[{"spans":[{
	      "traceId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	      "spanId":"bbbbbbbbbbbbbbbb",
	      "parentSpanId":"cccccccccccccccc",
	      "name":"llm",
	      "startTimeUnixNano":"1785463200000000000",
	      "endTimeUnixNano":"1785463200001000000",
	      "attributes":[
	        {"key":"openinference.span.kind","value":{"stringValue":"LLM"}},
	        {"key":"llm.model_name","value":{"stringValue":"model-b"}}
	      ],
	      "status":{"code":2}
	    }]}]
	  }]
	}`)
	batch, err := SanitizeOTLPExport(payload)
	if err != nil {
		t.Fatalf("sanitize missing-fact fixture: %v", err)
	}
	projector, err := NewProjector()
	if err != nil {
		t.Fatalf("create projector: %v", err)
	}
	item, connectorErr := projector.Project(
		context.Background(),
		batch.Records[0],
		MappingContext{ProducerInstance: "collector-a", OrgRef: "1", ProjectRef: "1"},
		connector.ProjectionContext{IngestedAt: time.Date(2026, 7, 31, 2, 0, 0, 0, time.UTC)},
	)
	if connectorErr != nil {
		t.Fatalf("project missing-fact fixture: %+v", connectorErr)
	}
	main := item.Events[0]
	usage := main["usage"].(map[string]any)
	for _, key := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		if _, exists := usage[key]; exists {
			t.Fatalf("missing %s was substituted: %v", key, usage)
		}
	}
	claims := map[string]bool{}
	for _, event := range item.Events[1:] {
		excerpt := event["raw_source_excerpt"].(map[string]any)
		claims[excerpt["gap_source"].(string)] = true
	}
	for _, expected := range []string{"token_usage", "authoritative_session", "causal_parent"} {
		if !claims[expected] {
			t.Fatalf("missing first-class gap %q: %v", expected, claims)
		}
	}
	if main["event_type"] != "model.request.failed" || main["severity"] != "error" {
		t.Fatalf("source-declared failure was lost: %v", main)
	}
}

func TestOpenInferenceUsesSharedLifecycleAndErrorSemanticsWithoutInventingPullCheckpoint(t *testing.T) {
	lifecycle, err := connector.ProjectLifecycle(connector.LocalLifecycleState{
		SourceStatus: "active", AdapterActive: true, Activated: true,
		AuthorizationRef: "systemd-credential", WorkerStatus: "idle",
		PagesCommitted: 1,
	})
	if err != nil || lifecycle.Health != "healthy" || lifecycle.Checkpoint != "steady" {
		t.Fatalf("shared lifecycle mismatch: %+v err=%v", lifecycle, err)
	}
	privacy := connector.NewConnectorError(
		connector.ErrorPrivacy,
		"foigent.openinference.raw_payload_rejected",
		connector.PhaseDecode,
		"sanitize_otlp",
		false,
		"OTLP payload violates the metadata allowlist",
		connector.RecoveryOperatorRepair,
	)
	if err := privacy.Validate(); err != nil {
		t.Fatalf("shared error contract mismatch: %v", err)
	}
}

func TestOpenInferenceRejectsSubMicrosecondDuplicateAndUnsafeCounterInputs(t *testing.T) {
	base := string(readContractFixture(t, "reference-otlp-export.v1.json"))
	cases := []string{
		strings.Replace(base, `"endTimeUnixNano": "1785463200123456000"`, `"endTimeUnixNano": "1785463200123456001"`, 1),
		strings.Replace(base, `"traceId": "0123456789abcdef0123456789abcdef"`, `"traceId": "0123456789abcdef0123456789abcdef", "traceId": "0123456789abcdef0123456789abcdef"`, 1),
		strings.Replace(base, `"intValue": "11"`, `"intValue": 11`, 1),
		strings.Replace(base, `"status": { "code": "STATUS_CODE_OK" }`, `"status": { "code": "STATUS_CODE_UNSET" }`, 1),
		strings.Replace(base, `"status": { "code": "STATUS_CODE_OK" }`, `"status": {}`, 1),
	}
	for index, payload := range cases {
		if _, err := SanitizeOTLPExport([]byte(payload)); err == nil {
			t.Fatalf("unsafe case %d unexpectedly accepted", index)
		}
	}
}

func TestOpenInferenceParentPresenceIsScopedToTrace(t *testing.T) {
	payload := []byte(`{
	  "resourceSpans":[{
	    "resource":{"attributes":[]},
	    "scopeSpans":[{"spans":[
	      {
	        "traceId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	        "spanId":"aaaaaaaaaaaaaaaa",
	        "parentSpanId":"1111111111111111",
	        "name":"trace-a-child",
	        "startTimeUnixNano":"1785463200000000000",
	        "endTimeUnixNano":"1785463200001000000",
	        "attributes":[
	          {"key":"openinference.span.kind","value":{"stringValue":"LLM"}},
	          {"key":"llm.model_name","value":{"stringValue":"model-a"}}
	        ],
	        "status":{"code":1}
	      },
	      {
	        "traceId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	        "spanId":"1111111111111111",
	        "name":"trace-b-root",
	        "startTimeUnixNano":"1785463200000000000",
	        "endTimeUnixNano":"1785463200001000000",
	        "attributes":[
	          {"key":"openinference.span.kind","value":{"stringValue":"LLM"}},
	          {"key":"llm.model_name","value":{"stringValue":"model-b"}}
	        ],
	        "status":{"code":1}
	      }
	    ]}]
	  }]
	}`)
	batch, err := SanitizeOTLPExport(payload)
	if err != nil {
		t.Fatalf("sanitize cross-trace parent fixture: %v", err)
	}
	if len(batch.Records) != 2 {
		t.Fatalf("expected two records, got %d", len(batch.Records))
	}
	if !batch.Records[0].Record.MissingParent {
		t.Fatalf("span from another trace satisfied parent reference: %+v", batch.Records[0].Record)
	}
	if batch.Records[1].Record.MissingParent {
		t.Fatalf("root span unexpectedly marked missing parent: %+v", batch.Records[1].Record)
	}
}

func TestOpenInferenceV202ProductPinKeepsRuntimeActivationFailClosed(t *testing.T) {
	if err := ValidateSharedProtocolActivation(); err == nil {
		t.Fatal("non-authorized OpenInference runtime was activation-eligible")
	}
	var assessment struct {
		Status   string `json:"status"`
		Protocol struct {
			Version            string `json:"version"`
			TagObject          string `json:"tag_object"`
			TagCommit          string `json:"tag_commit"`
			Tree               string `json:"tree"`
			ReleaseDigest      string `json:"release_digest"`
			ContentDigest      string `json:"content_digest"`
			ReleaseManifestSHA string `json:"release_manifest_file_sha256"`
		} `json:"protocol"`
		ProductPin struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"product_pin"`
		ProjectionBinding struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"projection_binding"`
		ProductMigration struct {
			IntegrationMode string `json:"integration_mode"`
			AssuranceMode   string `json:"assurance_mode"`
			Checkpoint      string `json:"checkpoint"`
		} `json:"product_migration"`
		AuthorityConflict struct {
			ManifestProjection string `json:"manifest_embedded_projection_sha256"`
			ReleaseProjection  string `json:"release_component_projection_sha256"`
			Classification     string `json:"classification"`
		} `json:"authority_reference_conflict"`
		Runtime struct {
			ActivationEligible  bool   `json:"activation_eligible"`
			BoundedOneOperation bool   `json:"bounded_offline_one_operation_eligible"`
			ReceiptImplemented  bool   `json:"durable_bounded_import_receipt_implemented"`
			RuntimeAuthorized   bool   `json:"runtime_authorized"`
			AuthorizationMode   string `json:"authorization_mode"`
			AuthorizationSchema string `json:"authorization_schema"`
			NetworkListener     bool   `json:"network_listener"`
			FeedActivation      bool   `json:"feed_activation"`
			CursorCheckpoint    bool   `json:"cursor_checkpoint"`
		} `json:"runtime_gate"`
	}
	if err := json.Unmarshal(
		readContractFixture(t, "shared-protocol-compatibility.v2.json"),
		&assessment,
	); err != nil {
		t.Fatalf("decode compatibility assessment: %v", err)
	}
	if assessment.Status != SharedProtocolCompatibilityStatus ||
		assessment.Protocol.Version != ProtocolVersion ||
		assessment.Protocol.TagObject != "1fece056b251239b8a557264c5d95043c4b5bdd2" ||
		assessment.Protocol.TagCommit != ProtocolCommit ||
		assessment.Protocol.Tree != "288fa23b0591b6ed2953e14bc4a9d3571e2839ac" ||
		assessment.Protocol.ReleaseDigest != ProtocolReleaseSHA ||
		assessment.Protocol.ContentDigest != "sha256:9dce6081e15be0028441513291808f8d58becb25f00887ae66299031bc79c1c0" ||
		assessment.Protocol.ReleaseManifestSHA != "sha256:3c5489c9931255cce5b565046deee3fd082d524e9a80945cbbd88e413ec073b0" ||
		assessment.ProductPin.SHA256 != fileSHA256(t, assessment.ProductPin.Path) ||
		assessment.ProjectionBinding.SHA256 != fileSHA256(t, assessment.ProjectionBinding.Path) ||
		assessment.ProductMigration.IntegrationMode != "connector_bounded_offline" ||
		assessment.ProductMigration.AssuranceMode != "bounded_offline_import" ||
		assessment.ProductMigration.Checkpoint != "not_applicable" ||
		assessment.AuthorityConflict.ManifestProjection != AuthorityManifestProjectionSHA256 ||
		assessment.AuthorityConflict.ReleaseProjection != AuthorityReleaseProjectionSHA256 ||
		assessment.AuthorityConflict.ManifestProjection != assessment.AuthorityConflict.ReleaseProjection ||
		assessment.AuthorityConflict.Classification != "resolved_in_v2.0.1" ||
		assessment.Runtime.ActivationEligible ||
		!assessment.Runtime.BoundedOneOperation ||
		!assessment.Runtime.ReceiptImplemented ||
		assessment.Runtime.RuntimeAuthorized ||
		assessment.Runtime.AuthorizationMode != "exact_root_owned_expiring_nonproduction_manifest" ||
		assessment.Runtime.AuthorizationSchema != "schemas/integration/openinference-bounded-offline-authorization-v1.schema.json" ||
		assessment.Runtime.NetworkListener ||
		assessment.Runtime.FeedActivation ||
		assessment.Runtime.CursorCheckpoint {
		t.Fatalf("compatibility assessment is incomplete: %+v", assessment)
	}
}

func fileSHA256(t *testing.T, relativePath string) string {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(repoRoot(t), relativePath))
	if err != nil {
		t.Fatalf("read digest input %s: %v", relativePath, err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func readContractFixture(t *testing.T, name string) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(repoRoot(t), "contracts/openinference", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return payload
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}
