package connectorcontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type controlFixtureIndex struct {
	SchemaVersion    string                `json:"schema_version"`
	OperationSchema  string                `json:"operation_schema"`
	ResultSchema     string                `json:"result_schema"`
	Canonicalization string                `json:"canonicalization"`
	Entries          []controlFixtureEntry `json:"entries"`
}

type controlFixtureEntry struct {
	Path            string `json:"path"`
	Expect          string `json:"expect"`
	ReasonCode      string `json:"reason_code,omitempty"`
	CanonicalSHA256 string `json:"canonical_sha256,omitempty"`
	RawSHA256       string `json:"raw_sha256,omitempty"`
}

func TestControlV2FrozenConformanceFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "contracts", "connector-control", "v2", "fixtures")
	indexPayload, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index controlFixtureIndex
	decoder := json.NewDecoder(bytes.NewReader(indexPayload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&index); err != nil {
		t.Fatal(err)
	}
	if index.SchemaVersion != "xinsight.connector-control-conformance-index.v1" ||
		index.OperationSchema != ControlOperationSchemaV2 || index.ResultSchema != ControlResultSchemaV2 ||
		index.Canonicalization != "RFC8259 compact JSON using the product typed field order" || len(index.Entries) != 11 {
		t.Fatalf("unexpected frozen fixture index: %+v", index)
	}

	validResults := map[string]ControlOperationResult{}
	validRequests := map[string]ControlOperationRequestV2{}
	now := time.Date(2030, 1, 1, 1, 0, 0, 0, time.UTC)
	for _, entry := range index.Entries {
		payload, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.Path)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if entry.RawSHA256 != "" {
			if got := fixtureSHA256(payload); got != entry.RawSHA256 {
				t.Fatalf("raw digest %s=%s expected=%s", entry.Path, got, entry.RawSHA256)
			}
		} else {
			canonical := bytes.TrimSuffix(payload, []byte{'\n'})
			if bytes.Contains(canonical, []byte{'\n'}) || fixtureSHA256(canonical) != entry.CanonicalSHA256 {
				t.Fatalf("canonical fixture bytes drifted: %s", entry.Path)
			}
		}

		switch {
		case strings.Contains(entry.Path, "operation") || strings.Contains(entry.Path, "phase") ||
			strings.Contains(entry.Path, "predecessor") || strings.Contains(entry.Path, "duplicate") ||
			strings.Contains(entry.Path, "arbitrary-url") || strings.Contains(entry.Path, "secret-token"):
			request, decodeErr := DecodeControlOperationRequestV2(payload)
			if entry.Expect == "valid" {
				if decodeErr != nil {
					t.Fatalf("valid operation %s rejected: %v", entry.Path, decodeErr)
				}
				digest, canonical, canonicalErr := CanonicalSHA256(request)
				if canonicalErr != nil || digest != entry.CanonicalSHA256 ||
					!bytes.Equal(canonical, bytes.TrimSuffix(payload, []byte{'\n'})) {
					t.Fatalf("operation canonicalization drift: %s digest=%s err=%v", entry.Path, digest, canonicalErr)
				}
				validRequests[entry.Path] = request
			} else if entry.Path == "invalid/04-resume-wrong-predecessor.json" {
				if decodeErr != nil {
					t.Fatalf("predecessor conflict fixture must be structurally typed: %v", decodeErr)
				}
				validRequests[entry.Path] = request
			} else if decodeErr == nil {
				t.Fatalf("invalid operation %s was accepted", entry.Path)
			}
		case strings.Contains(entry.Path, "result"):
			result, decodeErr := DecodeControlResultV2(payload, now)
			if entry.Expect != "valid" || decodeErr != nil {
				t.Fatalf("result fixture %s rejected: %v", entry.Path, decodeErr)
			}
			digest, canonical, canonicalErr := CanonicalControlResultV2(result)
			if canonicalErr != nil || digest != entry.CanonicalSHA256 ||
				!bytes.Equal(canonical, bytes.TrimSuffix(payload, []byte{'\n'})) {
				t.Fatalf("result canonicalization drift: %s digest=%s err=%v", entry.Path, digest, canonicalErr)
			}
			validResults[entry.Path] = result
		default:
			t.Fatalf("unrouted fixture %s", entry.Path)
		}
	}

	prepareResult := validResults["valid/02-prepare-paused-source-result.json"]
	registerResult := validResults["valid/04-register-receiver-result.json"]
	registerRequest := validRequests["valid/03-register-receiver-operation.json"]
	resumeRequest := validRequests["valid/05-resume-source-operation.json"]
	wrongPredecessor := validRequests["invalid/04-resume-wrong-predecessor.json"]
	if ValidateControlOperationV2Predecessor(registerRequest, prepareResult) != nil ||
		ValidateControlOperationV2Predecessor(resumeRequest, registerResult) != nil {
		t.Fatal("valid exact predecessor chain was rejected")
	}
	if ValidateControlOperationV2Predecessor(wrongPredecessor, registerResult) == nil {
		t.Fatal("wrong exact-case/phase predecessor was accepted")
	}

	for requestPath, resultPath := range map[string]string{
		"valid/01-prepare-paused-source-operation.json": "valid/02-prepare-paused-source-result.json",
		"valid/03-register-receiver-operation.json":     "valid/04-register-receiver-result.json",
		"valid/05-resume-source-operation.json":         "valid/06-resume-source-result.json",
	} {
		request := validRequests[requestPath]
		digest, _, digestErr := CanonicalSHA256(request)
		if digestErr != nil {
			t.Fatal(digestErr)
		}
		terminal := validResults[resultPath]
		live := pendingControlOperationV2(request, digest, terminal.ObservedAt.Add(-time.Second))
		if ValidateControlOperation(live) != nil ||
			ValidateControlResultV2ForOperation(live, terminal) != nil {
			t.Fatalf("live delivery binding rejected for %s", request.Phase)
		}
	}
}

func TestControlV2RejectsCrossPhaseAndNormalizationWithoutChangingV1(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	prepare := ControlOperationRequestV2{
		SchemaVersion: ControlOperationSchemaV2,
		OperationID:   "xco_BBBBBBBBBBBBBBBBBBBBBB", InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		ProjectID: "project.second-integration", EventScope: []string{CompletedEventType},
		DesiredState: "active", Kind: "connect", Phase: ControlPhasePreparePausedSource,
		Request: ControlPhaseRequestV2{FeedSchema: ControlFeedSchema, CursorKey: ControlCursorKey, RequiredSourceState: "paused"},
	}
	for name, mutate := range map[string]func(*ControlOperationRequestV2){
		"phase whitespace":            func(value *ControlOperationRequestV2) { value.Phase += " " },
		"cross phase":                 func(value *ControlOperationRequestV2) { value.Request.SourceInstance = "source.unexpected" },
		"desired state normalization": func(value *ControlOperationRequestV2) { value.DesiredState = "Active" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := prepare
			mutate(&invalid)
			if ValidateControlOperationRequestV2(invalid) == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}

	v1 := ControlOperationResult{
		SchemaVersion: ControlResultSchema, OperationID: "xco_AAAAAAAAAAAAAAAAAAAAAA",
		InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA", Status: "completed",
		SourceInstance: "source.v1", ObservedAt: now,
	}
	if ValidateControlResult(v1, now) != nil {
		t.Fatal("v1 result compatibility regressed")
	}
	v1.Phase = ControlPhasePreparePausedSource
	if ValidateControlResult(v1, now) == nil {
		t.Fatal("v1 result accepted a v2-only field")
	}
}

func TestControlV2RejectsNonProductSourceIdentityAndInvalidObservationTime(t *testing.T) {
	root := filepath.Join("..", "..", "contracts", "connector-control", "v2", "fixtures", "valid")
	now := time.Date(2030, 1, 1, 1, 0, 0, 0, time.UTC)
	registerPayload, err := os.ReadFile(filepath.Join(root, "03-register-receiver-operation.json"))
	if err != nil {
		t.Fatal(err)
	}
	register, err := DecodeControlOperationRequestV2(registerPayload)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"source/loose", "source loose", "source.\u4e2d\u6587"} {
		invalid := register
		invalid.Request.SourceInstance = source
		if !validOpaqueControlText(source, 128) {
			t.Fatalf("test source %q does not exercise the looser text grammar", source)
		}
		if ValidateControlOperationRequestV2(invalid) == nil {
			t.Fatalf("non-product Source identity %q was accepted", source)
		}
	}

	resultPayload, err := os.ReadFile(filepath.Join(root, "04-register-receiver-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := DecodeControlResultV2(resultPayload, now)
	if err != nil {
		t.Fatal(err)
	}
	invalidResult := result
	invalidResult.Result = &ControlPhaseResultV2{
		SourceInstance: "source/loose", ReceiverRegistrationSHA256: result.Result.ReceiverRegistrationSHA256,
		NetworkBindingSHA256: result.Result.NetworkBindingSHA256, SourceState: result.Result.SourceState,
		ReceiverState: result.Result.ReceiverState, Readiness: result.Result.Readiness,
	}
	if ValidateControlResultV2(invalidResult, now) == nil {
		t.Fatal("terminal result accepted a non-product Source identity")
	}

	digest, _, err := CanonicalSHA256(register)
	if err != nil {
		t.Fatal(err)
	}
	live := pendingControlOperationV2(register, digest, result.ObservedAt.Add(time.Second))
	if ValidateControlResultV2ForOperation(live, result) == nil {
		t.Fatal("terminal observation before operation creation was accepted")
	}
	live.CreatedAt = result.ObservedAt
	live.UpdatedAt = result.ObservedAt
	live.AvailableAt = result.ObservedAt
	if ValidateControlResultV2ForOperation(live, result) != nil {
		t.Fatal("terminal observation at operation creation was rejected")
	}
	future := result
	future.ObservedAt = now.Add(time.Minute + time.Nanosecond)
	if ValidateControlResult(future, now) == nil {
		t.Fatal("store-level now+1m future observation boundary regressed")
	}
}

func pendingControlOperationV2(request ControlOperationRequestV2, requestSHA string, now time.Time) ControlOperation {
	phaseRequest := request.Request
	return ControlOperation{
		SchemaVersion: request.SchemaVersion, OperationID: request.OperationID,
		InstallationID: request.InstallationID, Kind: request.Kind, Phase: request.Phase,
		ProjectID: request.ProjectID, EventScope: request.EventScope, DesiredState: request.DesiredState,
		PredecessorOperationID:  request.PredecessorOperationID,
		PredecessorResultSHA256: request.PredecessorResultSHA256,
		Request:                 &phaseRequest, RequestSHA256: requestSHA, Status: "pending",
		AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
}

func fixtureSHA256(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}
