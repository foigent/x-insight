package unirouter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/investigation"
)

func TestPhase2BNeutralProfileThreePathDifferential(t *testing.T) {
	binding := readPhase2BBinding(t)
	profile, err := NewCompletedExecutionProfile(binding)
	if err != nil {
		t.Fatalf("new completed-execution profile: %v", err)
	}
	registry, err := connector.NewProfileRegistry(binding)
	if err != nil {
		t.Fatalf("new profile registry: %v", err)
	}
	if err := registry.Register(profile); err != nil {
		t.Fatalf("register completed-execution profile: %v", err)
	}
	if err := registry.Register(profile); err == nil {
		t.Fatal("duplicate completed-execution profile registration accepted")
	}

	testCases := []struct {
		fixture              string
		provenanceID         string
		mainSourceRef        string
		expectedSourceHash   string
		expectedEnvelopeHash string
		expectedSharedKey    string
	}{
		{
			fixture:              "valid-settled.json",
			provenanceID:         "bf802b1c-df9c-54a3-82e6-03b691e79afa",
			mainSourceRef:        "unirouter:unirouter-development:completed_execution:req_feed_v1_settled",
			expectedSourceHash:   "sha256:5e9eda0a2630d52c5589d38b8ec480604c47c2cc77391b75a57b2a159db6dd27",
			expectedEnvelopeHash: "sha256:0d11f98f981427b7bcf175e82294511ca42981c0ecea8e02f841c3777743d1d2",
			expectedSharedKey:    "sha256:c2822f0d940c01de77e2e06af1a5f6e1fd10fc66269be20fceb0939150c17b2e",
		},
		{
			fixture:              "valid-cancelled.json",
			provenanceID:         "bd6d6519-88d2-578e-8aff-1c66932cd4e2",
			mainSourceRef:        "unirouter:unirouter-development:completed_execution:req_feed_v1_cancelled",
			expectedSourceHash:   "sha256:31cbfeb23968c07d158b591c28efb3dd40cffba1fd6d1fd4202f43d63bfe9f2d",
			expectedEnvelopeHash: "sha256:c31445c779ffec9dae8cbc7f1f0344722a4e0fb696a366b2e321f58d0d07fbab",
			expectedSharedKey:    "sha256:d342a560ade558edc557f2cd645a526178958540a012ceabe9ca7047e916796c",
		},
	}
	totalEvents := 0
	totalGaps := 0
	for _, testCase := range testCases {
		t.Run(testCase.fixture, func(t *testing.T) {
			source := readCompletedExecutionContractFile(t, "fixtures/"+testCase.fixture)
			projectionContext := connector.ProjectionContext{
				ObservedAt: time.Date(2026, 7, 25, 5, 0, 0, 0, time.UTC),
				IngestedAt: time.Date(2026, 7, 25, 5, 0, 1, 0, time.UTC),
			}
			options := CompletedExecutionMapOptions{
				SourceInstance: "unirouter-development",
				ObservedAt:     projectionContext.ObservedAt,
				IngestedAt:     projectionContext.IngestedAt,
			}

			shared, err := buildFoigentReferenceEnvelopeV1(
				source,
				options.SourceInstance,
			)
			if err != nil {
				t.Fatalf("build Reference Envelope: %v", err)
			}
			if shared.SourceHash != testCase.expectedSourceHash ||
				shared.EnvelopeHash != testCase.expectedEnvelopeHash ||
				shared.IdempotencyKey != testCase.expectedSharedKey {
				t.Fatalf(
					"shared identities=%s/%s/%s",
					shared.SourceHash,
					shared.EnvelopeHash,
					shared.IdempotencyKey,
				)
			}
			legacy, err := MapCompletedExecutionV1(source, options)
			if err != nil {
				t.Fatalf("map legacy path: %v", err)
			}
			phase2A, err := MapFoigentReferenceEnvelopeV1(
				shared.EnvelopeBytes,
				options,
			)
			if err != nil {
				t.Fatalf("map Phase 2A bridge: %v", err)
			}
			validated, connectorErr := connector.ValidateEnvelope(
				shared.EnvelopeBytes,
				binding,
			)
			if connectorErr != nil {
				t.Fatalf("validate neutral Envelope: %v", connectorErr)
			}
			if !bytes.Equal(validated.OriginalBytes(), shared.EnvelopeBytes) ||
				!bytes.Equal(validated.CanonicalBytes(), shared.EnvelopeBytes) ||
				!bytes.Equal(validated.HashInputBytes(), shared.CanonicalBytes) ||
				validated.EnvelopeHash != shared.EnvelopeHash ||
				validated.IdempotencyKey != shared.IdempotencyKey {
				t.Fatal("neutral Envelope validation changed exact bytes or identity")
			}
			resolved, connectorErr := registry.Resolve(
				validated,
				binding.Projection.ProfileID,
			)
			if connectorErr != nil {
				t.Fatalf("resolve neutral profile: %v", connectorErr)
			}
			neutral, connectorErr := resolved.Normalize(
				context.Background(),
				validated,
				projectionContext,
			)
			if connectorErr != nil {
				t.Fatalf("normalize neutral path: %v", connectorErr)
			}

			if !reflect.DeepEqual(legacy, phase2A.CompletedExecution) {
				t.Fatal("legacy and Phase 2A M0 projections differ")
			}
			if !reflect.DeepEqual(legacy.Events, neutral.Events) {
				t.Fatalf(
					"legacy and Phase 2B complete M0 maps differ\nlegacy=%#v\nneutral=%#v",
					legacy.Events,
					neutral.Events,
				)
			}
			if len(neutral.Events) != 5 {
				t.Fatalf("neutral M0 events=%d want=5", len(neutral.Events))
			}
			totalEvents += len(neutral.Events)
			for index, event := range neutral.Events {
				for _, required := range []string{
					"id", "idempotency_key", "event_hash", "field_provenance",
				} {
					if _, exists := event[required]; !exists {
						t.Fatalf("event[%d] missing %s", index, required)
					}
				}
				sourceRef, _ := event["source_ref"].(string)
				if index == 0 {
					if sourceRef != testCase.mainSourceRef {
						t.Fatalf("main source_ref=%s want=%s", sourceRef, testCase.mainSourceRef)
					}
					continue
				}
				totalGaps++
				rawSource, _ := event["raw_source_excerpt"].(map[string]any)
				gapSource, _ := rawSource["gap_source"].(string)
				if event["event_type"] != "gap.capture.detected" ||
					sourceRef != testCase.mainSourceRef+":gap:"+gapSource {
					t.Fatalf("gap[%d] source identity=%#v", index, event)
				}
			}
			assertPhase2BProvenance(
				t,
				neutral.Provenance,
				testCase.provenanceID,
				testCase.expectedSourceHash,
			)
			assertBatchDeliveryIsolation(t, binding, shared.EnvelopeBytes)
		})
	}
	if totalEvents != 10 || totalGaps != 8 {
		t.Fatalf("three-path totals events/gaps=%d/%d want=10/8", totalEvents, totalGaps)
	}

	if _, connectorErr := registry.Resolve(
		connector.ValidatedEnvelope{
			EventType:     binding.Connector.EventType,
			SchemaRef:     binding.Connector.SchemaRef,
			SchemaVersion: binding.Connector.SchemaVersion,
		},
		"unregistered.profile.v1",
	); connectorErr == nil {
		t.Fatal("unregistered profile resolved through payload guessing")
	}
}

func TestPhase2BNeutralProfileFailsClosed(t *testing.T) {
	binding := readPhase2BBinding(t)
	profile, err := NewCompletedExecutionProfile(binding)
	if err != nil {
		t.Fatalf("new completed-execution profile: %v", err)
	}
	source := readCompletedExecutionContractFile(t, "fixtures/valid-settled.json")
	shared, err := buildFoigentReferenceEnvelopeV1(source, "unirouter-development")
	if err != nil {
		t.Fatalf("build Reference Envelope: %v", err)
	}

	testCases := []struct {
		name          string
		payload       func() []byte
		validateFails bool
		profileFails  bool
	}{
		{
			name: "envelope hash",
			payload: func() []byte {
				var value map[string]any
				mustJSONUnmarshal(t, shared.EnvelopeBytes, &value)
				value["integrity"].(map[string]any)["envelope_hash"] =
					"sha256:" + strings.Repeat("0", 64)
				return mustCanonicalFoigentJSON(t, value)
			},
			validateFails: true,
		},
		{
			name: "shared idempotency",
			payload: func() []byte {
				return mutateAndRehashEnvelope(t, shared.EnvelopeBytes, func(value map[string]any) {
					value["identity"].(map[string]any)["idempotency_key"] =
						"sha256:" + strings.Repeat("0", 64)
				})
			},
			validateFails: true,
		},
		{
			name: "required false field",
			payload: func() []byte {
				return mutateAndRehashEnvelope(t, shared.EnvelopeBytes, func(value map[string]any) {
					delete(value["data_policy"].(map[string]any), "raw_payload_present")
				})
			},
			validateFails: true,
		},
		{
			name: "capture gap provenance",
			payload: func() []byte {
				return mutateAndRehashEnvelope(t, shared.EnvelopeBytes, func(value map[string]any) {
					gaps := value["payload"].(map[string]any)["capture_gaps"].([]any)
					gaps[0].(map[string]any)["field_provenance"].(map[string]any)["gap_type"] =
						"direct"
				})
			},
			profileFails: true,
		},
		{
			name: "field group provenance",
			payload: func() []byte {
				return mutateAndRehashEnvelope(t, shared.EnvelopeBytes, func(value map[string]any) {
					value["provenance"].(map[string]any)["field_groups"].(map[string]any)["payload.money"] =
						"derived"
				})
			},
			profileFails: true,
		},
		{
			name: "noncanonical number",
			payload: func() []byte {
				return bytes.Replace(
					shared.EnvelopeBytes,
					[]byte(`"amount_scale":1000000`),
					[]byte(`"amount_scale":1000000.0`),
					1,
				)
			},
			validateFails: true,
		},
		{
			name: "nested duplicate key",
			payload: func() []byte {
				return bytes.Replace(
					shared.EnvelopeBytes,
					[]byte(`"terminal":{"status":"settled"}`),
					[]byte(`"terminal":{"status":"settled","status":"settled"}`),
					1,
				)
			},
			validateFails: true,
		},
		{
			name: "unpaired surrogate",
			payload: func() []byte {
				return bytes.Replace(
					shared.EnvelopeBytes,
					[]byte(`"event_id":"req_feed_v1_settled"`),
					[]byte(`"event_id":"req_\ud800"`),
					1,
				)
			},
			validateFails: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			validated, connectorErr := connector.ValidateEnvelope(
				testCase.payload(),
				binding,
			)
			if testCase.validateFails {
				if connectorErr == nil ||
					connectorErr.Category != connector.ErrorContract ||
					connectorErr.Retryable {
					t.Fatalf("validation error=%+v", connectorErr)
				}
				return
			}
			if connectorErr != nil {
				t.Fatalf("generic validation failed before profile: %v", connectorErr)
			}
			if !testCase.profileFails {
				t.Fatal("negative case has no expected failure stage")
			}
			if _, connectorErr := profile.Normalize(
				context.Background(),
				validated,
				connector.ProjectionContext{},
			); connectorErr == nil ||
				connectorErr.Category != connector.ErrorContract ||
				connectorErr.Retryable {
				t.Fatalf("profile error=%+v", connectorErr)
			}
		})
	}
}

func assertPhase2BProvenance(
	t *testing.T,
	provenance investigation.ImportProvenance,
	expectedID string,
	expectedSourceHash string,
) {
	t.Helper()
	if provenance.ID != "" ||
		provenance.SourceSystemID != DefaultSourceSystemID ||
		provenance.AdapterManifestID != DefaultCompletedExecutionAdapterManifestID ||
		provenance.IntegrationMode != investigation.IntegrationPull ||
		provenance.ProofClass != investigation.ProofOperatorApproved ||
		provenance.AssuranceMode != investigation.AssuranceContractedPull ||
		provenance.SourcePayloadSHA256 != expectedSourceHash ||
		provenance.SourceArtifactSHA256 != "" ||
		provenance.ApprovalManifestSHA256 != "" ||
		provenance.ExchangeID != "" ||
		provenance.SourceGeneratedAt != nil ||
		provenance.SourceTerminalAt == nil ||
		provenance.SourceRevision != CompletedExecutionRevisionV1 ||
		provenance.ImportedRevision != CompletedExecutionRevisionV1 ||
		provenance.SourceWatermark != "" ||
		provenance.ImportWatermark != "" ||
		!provenance.TimelineComplete {
		t.Fatalf("contracted-pull provenance=%+v", provenance)
	}
	if actual := phase2BDeterministicProvenanceID(provenance); actual != expectedID {
		t.Fatalf("provenance ID=%s want=%s", actual, expectedID)
	}
}

func phase2BDeterministicProvenanceID(
	provenance investigation.ImportProvenance,
) string {
	timeValue := func(value *time.Time) string {
		if value == nil {
			return ""
		}
		return value.UTC().Format(time.RFC3339Nano)
	}
	identityParts := []string{
		"import_provenance",
		provenance.SourceSystemID,
		provenance.AdapterManifestID,
		provenance.IntegrationMode,
		provenance.ProofClass,
		provenance.AssuranceMode,
		provenance.SourceArtifactSHA256,
		provenance.ApprovalManifestSHA256,
		provenance.ExchangeID,
		provenance.SourceRevision,
		provenance.ImportedRevision,
		provenance.SourceWatermark,
		provenance.ImportWatermark,
		strconv.FormatBool(provenance.TimelineComplete),
		timeValue(provenance.SourceGeneratedAt),
		timeValue(provenance.SourceTerminalAt),
	}
	if provenance.SourcePayloadSHA256 != "" {
		identityParts = append(
			identityParts,
			"source_payload_sha256",
			provenance.SourcePayloadSHA256,
		)
	}
	digest := sha256.Sum256([]byte(strings.Join(identityParts, "\x00")))
	sum := hex.EncodeToString(digest[:])
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		sum[0:8],
		sum[8:12],
		"5"+sum[13:16],
		"8"+sum[17:20],
		sum[20:32],
	)
}

func assertBatchDeliveryIsolation(
	t *testing.T,
	binding connector.ManifestBinding,
	envelope []byte,
) {
	t.Helper()
	makeBatch := func(generatedAt, cursor, watermark string) []byte {
		payload, err := json.Marshal(map[string]any{
			"batch_version":     connector.EventBatchVersionV1,
			"connector_id":      binding.Connector.ConnectorID,
			"connector_version": binding.Connector.ConnectorVersion,
			"producer_instance": "unirouter-development",
			"generated_at":      generatedAt,
			"events":            []json.RawMessage{json.RawMessage(envelope)},
			"next_cursor":       cursor,
			"high_watermark":    watermark,
			"has_more":          false,
		})
		if err != nil {
			t.Fatalf("marshal Event Batch: %v", err)
		}
		return payload
	}
	first, connectorErr := connector.ValidateEventBatch(
		makeBatch("2026-07-28T08:00:00.000000Z", "cursor-a", "watermark-a"),
		binding,
	)
	if connectorErr != nil {
		t.Fatalf("validate first Event Batch: %v", connectorErr)
	}
	second, connectorErr := connector.ValidateEventBatch(
		makeBatch("2026-07-28T09:00:00.000000Z", "cursor-b", "watermark-b"),
		binding,
	)
	if connectorErr != nil {
		t.Fatalf("validate second Event Batch: %v", connectorErr)
	}
	if len(first.Events) != 1 ||
		len(second.Events) != 1 ||
		!bytes.Equal(first.Events[0].CanonicalBytes(), second.Events[0].CanonicalBytes()) ||
		first.Events[0].EnvelopeHash != second.Events[0].EnvelopeHash ||
		first.Events[0].IdempotencyKey != second.Events[0].IdempotencyKey {
		t.Fatal("Batch delivery fields changed Event bytes or identity")
	}
}

func readPhase2BBinding(t *testing.T) connector.ManifestBinding {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "contracts", "foigent", "xinsight-projection-binding.v1.json",
	))
	if err != nil {
		t.Fatalf("read Phase 2B binding: %v", err)
	}
	binding, err := connector.ParseManifestBinding(payload)
	if err != nil {
		t.Fatalf("parse Phase 2B binding: %v", err)
	}
	return binding
}

func mutateAndRehashEnvelope(
	t *testing.T,
	payload []byte,
	mutate func(map[string]any),
) []byte {
	t.Helper()
	var value map[string]any
	mustJSONUnmarshal(t, payload, &value)
	mutate(value)
	integrity := value["integrity"].(map[string]any)
	delete(integrity, "envelope_hash")
	hashInput := mustCanonicalFoigentJSON(t, value)
	digest := sha256.Sum256(hashInput)
	integrity["envelope_hash"] = "sha256:" + hex.EncodeToString(digest[:])
	return mustCanonicalFoigentJSON(t, value)
}

func mustJSONUnmarshal(t *testing.T, payload []byte, target any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

func mustCanonicalFoigentJSON(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := canonicalFoigentJSON(value)
	if err != nil {
		t.Fatalf("canonicalize Foigent JSON: %v", err)
	}
	return payload
}
