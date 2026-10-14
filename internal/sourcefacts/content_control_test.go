package sourcefacts

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

type contentControlVectors struct {
	Registrations []struct {
		Registration json.RawMessage `json:"registration"`
		Wire         string          `json:"registration_wire"`
		Hash         string          `json:"registration_hash"`
	} `json:"registrations"`
	Actions []struct {
		Action  string          `json:"action"`
		Request json.RawMessage `json:"request"`
	} `json:"actions"`
}
type contentControlResponses struct {
	GET  map[string]json.RawMessage `json:"get"`
	POST map[string]json.RawMessage `json:"post_examples"`
}

func contentControlFixtures(t *testing.T) (contentControlVectors, contentControlResponses) {
	t.Helper()
	var requests contentControlVectors
	var responses contentControlResponses
	for _, f := range []struct {
		path, hash string
		out        any
	}{
		{"contract/content-control/u1-request.json", "sha256:3ef52efb929d795b0249f07747af582163c5af5741d2bfe29271fbbc68e4c235", &requests},
		{"contract/content-control/u1-response-v2.json", "sha256:64b26542165fb90434a73646ba4f04ca3ed4e14280776b2f217d320431a37bce", &responses},
	} {
		raw, err := os.ReadFile(f.path)
		if err != nil || Hash(raw) != f.hash || json.Unmarshal(raw, f.out) != nil {
			t.Fatal("frozen public wire changed", f.path, err)
		}
	}
	return requests, responses
}
func contentControlJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestContentControlAcceptedRegistrationAndRequestBytes(t *testing.T) {
	v, _ := contentControlFixtures(t)
	for _, gold := range v.Registrations {
		r, err := DecodeSourceCredentialRegistration(gold.Registration)
		if err != nil {
			t.Fatal(err)
		}
		raw, hash, err := r.MarshalRegistration()
		if err != nil || string(raw) != gold.Wire || hash != gold.Hash {
			t.Fatal("UR struct-order hash changed", err)
		}
		var legacy map[string]any
		json.Unmarshal(raw, &legacy)
		for _, k := range []string{"source_instance", "org_id", "project_id", "profiles"} {
			delete(legacy, k)
		}
		legacy["schema_version"] = "unirouter.xinsight.feed-credential-registration.v1"
		if _, err := DecodeSourceCredentialRegistration(contentControlJSON(t, legacy)); err == nil {
			t.Fatal("legacy six-field registration accepted as C")
		}
	}
	for i, a := range v.Actions {
		t.Run(strconv.Itoa(i)+"_"+a.Action, func(t *testing.T) {
			original := bytes.Clone(a.Request)
			r, err := DecodeContentControlRequest(a.Action, a.Request)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := r.RetryBytes(r.value.OperationID, Hash(original))
			if err != nil || !bytes.Equal(wire, original) {
				t.Fatal("retry changed original CAS/operation bytes", err)
			}
			wire[0] = '!'
			a.Request[0] = '!'
			if !bytes.Equal(r.Bytes(), original) {
				t.Fatal("mutable request identity")
			}
			if _, err := r.RetryBytes("ffffffffffffffffffffffffffffffff", r.Hash()); err == nil {
				t.Fatal("changed retry operation accepted")
			}
			if _, err := r.RetryBytes(r.value.OperationID, Hash(nil)); err == nil {
				t.Fatal("changed retry hash accepted")
			}
		})
	}
}

func TestContentControlAcceptedResponseShapes(t *testing.T) {
	_, v := contentControlFixtures(t)
	for name, raw := range v.GET {
		t.Run("get_"+name, func(t *testing.T) {
			if _, err := DecodeContentControlGET(raw); err != nil {
				t.Fatal(err)
			}
		})
	}
	for name, raw := range v.POST {
		t.Run("post_"+name, func(t *testing.T) {
			if _, err := DecodeContentControlReceipt(raw); err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeContentControlGET(raw); err == nil {
				t.Fatal("POST receipt substituted for current GET")
			}
		})
	}
}

// The accepted response examples are independent shapes, not responses to the
// accepted request sequence. Derive an explicit matching synthetic fixture.
func contentControlBindingFixture(t *testing.T) (SourceCredentialRegistration, ManagedContentConfig, ContentControlGET, ContentCapabilities) {
	t.Helper()
	v, responses := contentControlFixtures(t)
	reg, err := DecodeSourceCredentialRegistration(v.Registrations[0].Registration)
	if err != nil {
		t.Fatal(err)
	}
	g, err := DecodeContentControlGET(responses.GET["active"])
	if err != nil {
		t.Fatal(err)
	}
	_, hash, _ := reg.MarshalRegistration()
	g.Active.RegistrationHash = hash
	g.ContentAccess.RegistrationHash = hash
	cfg := contentConfigFixture()
	p, err := cfg.ResolveContent(cfg.Contents[0])
	if err != nil {
		t.Fatal(err)
	}
	p.SourceInstance, p.OrgID, p.ProjectID = reg.SourceInstance, reg.OrgID, reg.ProjectID
	p.CredentialID, p.RegistrationHash, p.ContentScopeRevision = g.Active.CredentialID, hash, g.ContentScopeRevision
	p.CoverageFrom = *g.ContentAccess.CoverageFrom
	p.RecoveryEpoch = "17" // Deliberately unrelated to GET state_version "2".
	_, _, reader := contentConsumerFixture(t)
	caps, err := DecodeContentCapabilities(reader.cap)
	if err != nil {
		t.Fatal(err)
	}
	caps.SourceInstance, caps.OrgID, caps.ProjectID = p.SourceInstance, p.OrgID, p.ProjectID
	caps.ContentScopeRevision, caps.RecoveryEpoch, caps.CoverageFrom, caps.RetentionFloor = p.ContentScopeRevision, p.RecoveryEpoch, p.CoverageFrom, p.CoverageFrom
	return reg, p, g, caps
}

func TestContentControlCurrentBindingAndIndependentCapabilities(t *testing.T) {
	reg, p, g, caps := contentControlBindingFixture(t)
	// Staged rotation must not replace the existing active access projection.
	g.Staged = &ContentControlCredential{"43", Hash([]byte("staged"))}
	candidate, err := ValidateContentControlBinding(contentControlJSON(t, g), reg, p)
	if err != nil || candidate.StateVersion() != "2" {
		t.Fatal(err)
	}
	if _, err := candidate.ValidateCapabilities(contentControlJSON(t, caps)); err != nil {
		t.Fatal(err)
	}
	caps.RecoveryEpoch = candidate.StateVersion()
	if _, err := candidate.ValidateCapabilities(contentControlJSON(t, caps)); err == nil {
		t.Fatal("state_version substituted for recovery_epoch")
	}
	for name, mutate := range map[string]func(*ManagedContentConfig){
		"credential": func(p *ManagedContentConfig) { p.CredentialID = "43" }, "hash": func(p *ManagedContentConfig) { p.RegistrationHash = Hash(nil) },
		"source": func(p *ManagedContentConfig) { p.SourceInstance = "other" }, "org": func(p *ManagedContentConfig) { p.OrgID = "9" }, "project": func(p *ManagedContentConfig) { p.ProjectID = "9" },
		"scope": func(p *ManagedContentConfig) { p.ContentScopeRevision = "3" }, "coverage": func(p *ManagedContentConfig) { p.CoverageFrom.AfterSequence = "99" },
	} {
		t.Run(name, func(t *testing.T) {
			next := p
			mutate(&next)
			if _, err := ValidateContentControlBinding(contentControlJSON(t, g), reg, next); err == nil {
				t.Fatal("mismatched managed binding admitted")
			}
		})
	}
	_, responses := contentControlFixtures(t)
	for name, raw := range responses.GET {
		if name == "active" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateContentControlBinding(raw, reg, p); err == nil {
				t.Fatal("non-active projection admitted")
			}
		})
	}
	g.ContentAccess.Enabled = false
	g.ContentAccess.AllowedPurposes = []string{}
	if _, err := ValidateContentControlBinding(contentControlJSON(t, g), reg, p); err == nil {
		t.Fatal("disabled active binding admitted")
	}
}

func TestContentControlStrictNegativeShapes(t *testing.T) {
	v, responses := contentControlFixtures(t)
	for name, mutate := range map[string]func(map[string]any){
		"numeric_version": func(m map[string]any) { m["expected_state_version"] = 0 }, "leading_zero": func(m map[string]any) { m["expected_state_version"] = "01" },
		"overflow": func(m map[string]any) { m["expected_state_version"] = "9223372036854775808" }, "missing_cas": func(m map[string]any) { delete(m, "expected_state_version") },
		"null": func(m map[string]any) { m["operation_id"] = nil }, "unknown": func(m map[string]any) { m["key"] = "forbidden" },
		"op_binding": func(m map[string]any) { m["operation_id"] = "ffffffffffffffffffffffffffffffff" }, "ip_without_cidr": func(m map[string]any) { m["allowed_ip"] = "127.0.0.1" },
	} {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			json.Unmarshal(v.Actions[0].Request, &m)
			mutate(m)
			if _, err := DecodeContentControlRequest("prepare", contentControlJSON(t, m)); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
	duplicate := append([]byte(`{"operation_id":"00000000000000000000000000000001",`), v.Actions[0].Request[1:]...)
	if _, err := DecodeContentControlRequest("prepare", duplicate); err == nil {
		t.Fatal("duplicate accepted")
	}
	for name, mutate := range map[string]func(map[string]any){
		"active_extra":      func(m map[string]any) { m["active"].(map[string]any)["token"] = "forbidden" },
		"active_null":       func(m map[string]any) { m["active"] = nil },
		"mismatched_access": func(m map[string]any) { m["content_access"].(map[string]any)["registration_hash"] = Hash(nil) },
		"paused_enabled":    func(m map[string]any) { m["connection_status"] = "paused" },
		"staged_enabled":    func(m map[string]any) { m["staged"] = m["active"]; delete(m, "active") },
		"disabled_purpose":  func(m map[string]any) { m["content_access"].(map[string]any)["enabled"] = false },
		"revoked_key":       func(m map[string]any) { m["content_status"] = "revoked" },
	} {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			json.Unmarshal(responses.GET["active"], &m)
			mutate(m)
			if _, err := DecodeContentControlGET(contentControlJSON(t, m)); err == nil {
				t.Fatal("invalid GET accepted")
			}
		})
	}
}

func TestContentControlReceiptMatchesOriginalRequest(t *testing.T) {
	v, _ := contentControlFixtures(t)
	for i, a := range v.Actions {
		t.Run(strconv.Itoa(i)+"_"+a.Action, func(t *testing.T) {
			r, err := DecodeContentControlRequest(a.Action, a.Request)
			if err != nil {
				t.Fatal(err)
			}
			version, _ := strconv.Atoi(r.value.ExpectedStateVersion)
			receipt := ContentControlReceipt{Action: a.Action, OperationID: r.value.OperationID, StateVersion: strconv.Itoa(version + 1), ContentScopeRevision: "1", CredentialID: r.value.CredentialID, RegistrationHash: r.value.RegistrationHash, AllowedPurposes: []string{ContentPurposeHumanReview}, CoverageFrom: &ContentBoundary{contentTestTime, "100"}}
			if r.value.Registration != nil {
				receipt.CredentialID = "49"
				_, receipt.RegistrationHash, _ = r.value.Registration.MarshalRegistration()
			}
			if _, err := r.MatchReceipt(contentControlJSON(t, receipt)); err != nil {
				t.Fatal(err)
			}
			for _, which := range []string{"op", "hash", "cas", "action"} {
				next := receipt
				switch which {
				case "op":
					next.OperationID = "ffffffffffffffffffffffffffffffff"
				case "hash":
					next.RegistrationHash = Hash(nil)
				case "cas":
					next.StateVersion = r.value.ExpectedStateVersion
				case "action":
					next.Action = "unknown"
				}
				if _, err := r.MatchReceipt(contentControlJSON(t, next)); err == nil {
					t.Fatal("changed receipt accepted", which)
				}
			}
		})
	}
}
