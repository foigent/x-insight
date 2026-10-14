package connectorcontrol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestControlV3SharedGolden(t *testing.T) {
	raw, e := os.ReadFile("testdata/control-v3/index.json")
	if e != nil {
		t.Fatal(e)
	}
	var index struct {
		Entries []struct {
			Path, Expect    string
			CanonicalSHA256 string `json:"canonical_sha256"`
		} `json:"entries"`
	}
	if json.Unmarshal(raw, &index) != nil {
		t.Fatal("index")
	}
	now := time.Date(2026, 10, 3, 0, 0, 10, 0, time.UTC)
	operations := map[string]ControlOperationRequestV3{}
	for _, entry := range index.Entries {
		t.Run(entry.Path, func(t *testing.T) {
			b, e := os.ReadFile("testdata/control-v3/" + entry.Path)
			if e != nil {
				t.Fatal(e)
			}
			var value any
			if strings.Contains(entry.Path, "operation") || strings.Contains(entry.Path, "prefix-conflict") {
				v, e := DecodeControlOperationRequestV3(b)
				if entry.Expect == "invalid" {
					if e == nil {
						t.Fatal("invalid accepted")
					}
					return
				}
				if e != nil {
					t.Fatal(e)
				}
				value = v
				operations[v.OperationID] = v
			} else {
				v, e := DecodeControlResultV3(b, now)
				if entry.Expect == "invalid" {
					if e == nil {
						t.Fatal("invalid accepted")
					}
					return
				}
				if e != nil {
					t.Fatal(e)
				}
				value = v
				if e := ValidateControlResultV3ForOperation(operations[v.OperationID], v); e != nil {
					t.Fatal("result binding", e)
				}
				var envelope ControlOperationResult
				if json.Unmarshal(b, &envelope) != nil || ValidateControlResult(envelope, now) != nil {
					t.Fatal("production result dispatch")
				}
				wire, e := json.Marshal(envelope)
				if e != nil || !MatchesCanonicalDigest(json.RawMessage(wire), entry.CanonicalSHA256) {
					t.Fatal("result envelope changed canonical identity")
				}
			}
			if !MatchesCanonicalDigest(value, entry.CanonicalSHA256) {
				t.Fatal("canonical bytes differ across products")
			}
		})
	}
}
func TestControlV3RejectsNonCanonicalShapeAndChangedAuthority(t *testing.T) {
	raw, e := os.ReadFile("testdata/control-v3/valid/03-confirm-existing-operation.json")
	if e != nil {
		t.Fatal(e)
	}
	for _, b := range []string{
		strings.Replace(string(raw), `,"pages_committed_before":86628`, "", 1),
		strings.Replace(string(raw), `"pages_committed_before":86628`, `"pages_committed_before":8.6628e4`, 1),
		strings.Replace(string(raw), `"response_status":200`, `"response_status":null`, 1),
		strings.Replace(string(raw), `"response_status":200`, `"response_status":200,"response_status":200`, 1),
		strings.Replace(string(raw), `"completed_at":"2026-10-03T00:00:02Z"`, `"completed_at":"2026-10-03T00:00:00Z"`, 1),
		strings.Replace(string(raw), `"consumer_epoch":2`, `"consumer_epoch":1`, 1),
		strings.Replace(string(raw), `"request":{`, `"request":{"token":"secret",`, 1),
	} {
		if b == string(raw) {
			t.Fatal("fixture mutation did not change input")
		}
		if _, e := DecodeControlOperationRequestV3([]byte(b)); e == nil {
			t.Fatal("invalid receipt/shape accepted")
		}
	}
}
