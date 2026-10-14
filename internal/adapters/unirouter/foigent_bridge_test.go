package unirouter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFoigentReferenceBridgeGoldenAndM0Compatibility(t *testing.T) {
	testCases := []struct {
		fixture        string
		canonicalBytes int
		envelopeHash   string
		idempotencyKey string
		m0Events       [][2]string
	}{
		{
			fixture:        "valid-settled.json",
			canonicalBytes: 4596,
			envelopeHash:   "sha256:0d11f98f981427b7bcf175e82294511ca42981c0ecea8e02f841c3777743d1d2",
			idempotencyKey: "sha256:c2822f0d940c01de77e2e06af1a5f6e1fd10fc66269be20fceb0939150c17b2e",
			m0Events: [][2]string{
				{"4805a534-a614-5d51-a116-1537e5ba48ba", "sha256:0318866b5d0ecdb6ac9f7fdd0e695f447bcdb9b1ed3dd57c225a1950a76f645a"},
				{"faa85223-b1c8-5fc8-be7f-5a5efef2fcb6", "sha256:64e14030ebe55b5c877230b2618ed47e80dc5a2494fd777ed312d6aea27a4c65"},
				{"ae69fd56-ee75-5bab-b5b3-f094239d4d4b", "sha256:d8ee3f703ebbd49e152eb14f36e253d050bdcbbab2eb64de9625dcdc13b318f3"},
				{"16b110d9-4240-51b3-8b59-6ebc2c4e692f", "sha256:ab5ff4c276c1d982e6037dbb9ad4343144481ef28bbbcba82b285be355e0f993"},
				{"2f86fbc5-ac47-5358-9834-ca52f4d91ed9", "sha256:584c92345473b37f972f745c2e8fe7f6ff2516ebebaefa56b22e2df3a0626ef4"},
			},
		},
		{
			fixture:        "valid-cancelled.json",
			canonicalBytes: 4629,
			envelopeHash:   "sha256:c31445c779ffec9dae8cbc7f1f0344722a4e0fb696a366b2e321f58d0d07fbab",
			idempotencyKey: "sha256:d342a560ade558edc557f2cd645a526178958540a012ceabe9ca7047e916796c",
			m0Events: [][2]string{
				{"b1bf7b19-a0d1-50cb-aa7f-3fe0b5d087a4", "sha256:5fbf54535f992d35f157f884c4b997f905b6f4351e54919927d1b087458c4230"},
				{"b65ee756-50ec-541e-a264-722226432e3f", "sha256:e4c8e0b7482f7c1dcc61fe3716eefd31d9258de966db333ab67170b68f6af29f"},
				{"cd0fc10f-d55d-55b5-aff3-aff2113f574f", "sha256:7e296d27a29310196b1dfa9b221621d91a13d60d04e5cd1e74492cd55220cd33"},
				{"f10aa91b-ea77-5898-85eb-89c070e3a3a5", "sha256:65661cda523749491642a1d12b7e1238ec0ba2f43619e8cff8cf530daa81189a"},
				{"f0594e43-4332-5ff0-8e2f-3ef1c1b54ee4", "sha256:c7fbc46f495bc7e0ed671c2efd9fe3f3b00aee153f3d9ad078092f38a685a994"},
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.fixture, func(t *testing.T) {
			source := readCompletedExecutionContractFile(t, "fixtures/"+testCase.fixture)
			options := CompletedExecutionMapOptions{
				SourceInstance: "unirouter-development",
				ObservedAt:     time.Date(2026, 7, 25, 5, 0, 0, 0, time.UTC),
				IngestedAt:     time.Date(2026, 7, 25, 5, 0, 1, 0, time.UTC),
			}
			envelope, err := buildFoigentReferenceEnvelopeV1(source, options.SourceInstance)
			if err != nil {
				t.Fatalf("build shared Envelope: %v", err)
			}
			if len(envelope.CanonicalBytes) != testCase.canonicalBytes ||
				envelope.EnvelopeHash != testCase.envelopeHash ||
				envelope.IdempotencyKey != testCase.idempotencyKey {
				t.Fatalf(
					"shared result bytes/hash/idempotency=%d/%s/%s",
					len(envelope.CanonicalBytes),
					envelope.EnvelopeHash,
					envelope.IdempotencyKey,
				)
			}
			digest := sha256.Sum256(envelope.CanonicalBytes)
			if got := "sha256:" + hex.EncodeToString(digest[:]); got != envelope.EnvelopeHash {
				t.Fatalf("canonical digest=%s envelope hash=%s", got, envelope.EnvelopeHash)
			}

			legacy, err := MapCompletedExecutionV1(source, options)
			if err != nil {
				t.Fatalf("map legacy path: %v", err)
			}
			bridged, err := MapFoigentReferenceEnvelopeV1(envelope.EnvelopeBytes, options)
			if err != nil {
				t.Fatalf("map shared path: %v", err)
			}
			if bridged.EnvelopeHash != testCase.envelopeHash ||
				bridged.SharedIdempotency != testCase.idempotencyKey {
				t.Fatalf("shared projection identity=%#v", bridged)
			}
			if !reflect.DeepEqual(legacy, bridged.CompletedExecution) {
				t.Fatalf("legacy and shared M0 projections differ\nlegacy=%#v\nshared=%#v", legacy, bridged.CompletedExecution)
			}
			if len(bridged.CompletedExecution.Events) != len(testCase.m0Events) {
				t.Fatalf("M0 events=%d want=%d", len(bridged.CompletedExecution.Events), len(testCase.m0Events))
			}
			for index, expected := range testCase.m0Events {
				event := bridged.CompletedExecution.Events[index]
				if event["id"] != expected[0] || event["event_hash"] != expected[1] {
					t.Fatalf(
						"M0 event[%d] identity/hash=%v/%v want=%s/%s",
						index,
						event["id"],
						event["event_hash"],
						expected[0],
						expected[1],
					)
				}
			}
		})
	}
}

func TestFoigentReferenceBridgeRejectsHashAndIdentityDrift(t *testing.T) {
	source := readCompletedExecutionContractFile(t, "fixtures/valid-settled.json")
	envelope, err := buildFoigentReferenceEnvelopeV1(source, "unirouter-development")
	if err != nil {
		t.Fatalf("build shared Envelope: %v", err)
	}
	testCases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "envelope hash",
			mutate: func(value map[string]any) {
				value["integrity"].(map[string]any)["envelope_hash"] = "sha256:" + strings.Repeat("0", 64)
			},
		},
		{
			name: "shared idempotency",
			mutate: func(value map[string]any) {
				value["identity"].(map[string]any)["idempotency_key"] = "sha256:" + strings.Repeat("0", 64)
			},
		},
		{
			name: "capture gap provenance",
			mutate: func(value map[string]any) {
				gaps := value["payload"].(map[string]any)["capture_gaps"].([]any)
				gaps[0].(map[string]any)["field_provenance"].(map[string]any)["gap_type"] = "direct"
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(envelope.EnvelopeBytes, &value); err != nil {
				t.Fatalf("decode Envelope: %v", err)
			}
			testCase.mutate(value)
			mutated, err := json.Marshal(value)
			if err != nil {
				t.Fatalf("marshal mutation: %v", err)
			}
			if _, err := MapFoigentReferenceEnvelopeV1(mutated, CompletedExecutionMapOptions{
				SourceInstance: "unirouter-development",
			}); err == nil {
				t.Fatal("mutated Envelope accepted")
			}
		})
	}
}

func TestFoigentReferenceBridgeRejectsMissingRequiredFalseField(t *testing.T) {
	source := readCompletedExecutionContractFile(t, "fixtures/valid-settled.json")
	envelope, err := buildFoigentReferenceEnvelopeV1(source, "unirouter-development")
	if err != nil {
		t.Fatalf("build shared Envelope: %v", err)
	}
	value := cloneFoigentMap(envelope.Envelope)
	delete(value["data_policy"].(map[string]any), "raw_payload_present")
	delete(value["integrity"].(map[string]any), "envelope_hash")
	canonical, err := canonicalFoigentJSON(value)
	if err != nil {
		t.Fatalf("canonicalize modified Envelope: %v", err)
	}
	digest := sha256.Sum256(canonical)
	value["integrity"].(map[string]any)["envelope_hash"] = "sha256:" + hex.EncodeToString(digest[:])
	modified, err := canonicalFoigentJSON(value)
	if err != nil {
		t.Fatalf("encode modified Envelope: %v", err)
	}
	if _, err := MapFoigentReferenceEnvelopeV1(modified, CompletedExecutionMapOptions{
		SourceInstance: "unirouter-development",
	}); err == nil || !strings.Contains(err.Error(), "raw_payload_present is required") {
		t.Fatalf("error=%v want missing required raw_payload_present", err)
	}
}

func TestFoigentReferenceBridgeExcludesDeliveryStateFromM0Identity(t *testing.T) {
	source := readCompletedExecutionContractFile(t, "fixtures/valid-settled.json")
	first, err := mapCompletedExecutionViaFoigentBridgeV1(source, CompletedExecutionMapOptions{
		SourceInstance: "unirouter-development",
		ObservedAt:     time.Date(2026, 7, 25, 5, 0, 0, 0, time.UTC),
		IngestedAt:     time.Date(2026, 7, 25, 5, 0, 1, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("first bridge: %v", err)
	}
	second, err := mapCompletedExecutionViaFoigentBridgeV1(source, CompletedExecutionMapOptions{
		SourceInstance: "unirouter-development",
		ObservedAt:     time.Date(2026, 7, 25, 6, 0, 0, 0, time.UTC),
		IngestedAt:     time.Date(2026, 7, 25, 6, 0, 1, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("second bridge: %v", err)
	}
	for index := range first.CompletedExecution.Events {
		left := first.CompletedExecution.Events[index]
		right := second.CompletedExecution.Events[index]
		if left["id"] != right["id"] ||
			left["idempotency_key"] != right["idempotency_key"] ||
			left["event_hash"] != right["event_hash"] {
			t.Fatalf("event[%d] identity changed across operational times", index)
		}
	}
}

func TestFoigentReferenceBridgePreservesOpaqueNonNFCIdentity(t *testing.T) {
	idempotency, err := foigentIdempotencyKey(
		"unirouter",
		"unirouter-development",
		"",
		"101",
		"202",
		foigentEventType,
		"req_e\u0301",
	)
	if err != nil {
		t.Fatalf("build opaque identity: %v", err)
	}
	if idempotency != "sha256:fd256a506369a0fa05dad3ee68f2e577918b97810778e1802d11950112275447" {
		t.Fatalf("idempotency=%s", idempotency)
	}
}
