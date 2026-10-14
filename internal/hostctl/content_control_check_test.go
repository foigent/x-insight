package hostctl

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestContentControlOfflineReceiptDoesNotAuthorizeBinding(t *testing.T) {
	var vectors struct {
		Actions []struct {
			Action  string          `json:"action"`
			Request json.RawMessage `json:"request"`
		} `json:"actions"`
	}
	raw, err := os.ReadFile("../sourcefacts/contract/content-control/u1-request.json")
	if err != nil || json.Unmarshal(raw, &vectors) != nil {
		t.Fatal(err)
	}
	request := vectors.Actions[0].Request
	var value struct {
		OperationID  string                                   `json:"operation_id"`
		Registration sourcefacts.SourceCredentialRegistration `json:"registration"`
	}
	if json.Unmarshal(request, &value) != nil {
		t.Fatal("fixture")
	}
	_, hash, err := value.Registration.MarshalRegistration()
	if err != nil {
		t.Fatal(err)
	}
	receipt := sourcefacts.ContentControlReceipt{Action: "prepare", OperationID: value.OperationID, StateVersion: "1", ContentScopeRevision: "1", CredentialID: "42", RegistrationHash: hash, AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: &sourcefacts.ContentBoundary{Time: "2026-09-15T00:00:00.000000Z", AfterSequence: "100"}}
	raw, err = json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	in := ContentControlCheckInput{Action: "prepare", Request: request, Receipt: raw}
	before := bytes.Clone(request)
	out, err := CheckContentControl(in)
	if err != nil || !out.CheckOnly || !out.ReceiptMatched || out.BindingCandidate || out.CapabilitiesMatched || out.RequestHash != sourcefacts.Hash(request) {
		t.Fatal("receipt check conferred current authority", out, err)
	}
	if !bytes.Equal(before, request) {
		t.Fatal("request rewritten")
	}
	in.GET = []byte(`{}`)
	if _, err := CheckContentControl(in); err == nil {
		t.Fatal("mixed POST/GET check accepted")
	}
	in.GET = nil
	in.Capabilities = []byte(`{}`)
	if _, err := CheckContentControl(in); err == nil {
		t.Fatal("receipt used as capabilities candidate")
	}
}
