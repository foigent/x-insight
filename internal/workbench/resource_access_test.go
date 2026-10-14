package workbench

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func resourceFixture() ResourceAccessRecord {
	scope := ResourceScope{SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "synthetic-application", OrgID: "17", ProjectID: "101"}
	return ResourceAccessRecord{
		Version: ResourceAccessVersion, Synthetic: true, RecordRef: "resource-read-1", Revision: 1,
		Reporter: scope, Target: scope, RequestID: "request-1", ApplicationRef: "app-1", ActorRef: "actor-1",
		OccurredAt: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC), ReportKind: "source_audit",
		Resource: ResourceObject{Ref: "document-1", Version: "v2", Name: "合成企业知识库", Location: "enterprise_service", Ownership: "known", OwnerRef: "owner-1", Classification: "known", ClassificationRef: "internal-v1", DeviceManagement: "unknown"},
		Action:   "read", AccessOutcome: "read", AccessDecision: "allow", DecisionRef: "acl-receipt-1", PolicyVersion: "acl-v2",
		Input: &ResourceInputClaim{FragmentRef: "section-3", FragmentVersion: "v2.3", AttemptID: "attempt-1", AttemptKind: "provider_attempt", ReportedInputRef: "input-1", Transforms: []ResourceTransform{{Kind: "summarize", InputVersion: "v2.3", OutputVersion: "summary-v1", ReceiptRef: "transform-1"}}},
	}
}

func TestResourceDomainKeepsClaimsUnknownsAndInputMismatch(t *testing.T) {
	r := resourceFixture()
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := DecodeResourceAccess(raw)
	if err != nil || ResourceInputState(parsed, "input-1") != "reference_matched" {
		t.Fatal("synthetic positive reference chain", err)
	}
	if ResourceInputState(parsed, "input-2") != "reference_mismatch" || ResourceInputState(parsed, "") != "not_confirmed" {
		t.Fatal("missing or different input was promoted")
	}
	pc := resourceFixture()
	pc.ReportKind, pc.Resource.Location = "client_assertion", "pc"
	pc.Resource.Ownership, pc.Resource.OwnerRef = "unknown", ""
	pc.Resource.Classification, pc.Resource.ClassificationRef = "unknown", ""
	if err = pc.Validate(); err != nil || pc.Resource.Ownership != "unknown" || pc.Resource.DeviceManagement != "unknown" {
		t.Fatal("PC location inferred ownership or management", err)
	}
	pc.AccessOutcome, pc.AccessDecision, pc.DecisionRef, pc.PolicyVersion = "denied", "unknown", "", ""
	if err = pc.Validate(); err != nil || ResourceInputState(pc, "input-1") != "not_confirmed" {
		t.Fatal("a denied read proved content use", err)
	}
	r.Withdrawn = true
	if ResourceInputState(r, "input-1") != "withdrawn" {
		t.Fatal("withdrawn declaration retained the association")
	}
}

func TestResourceDomainRejectsUnqualifiedInputAndMalformedRevisions(t *testing.T) {
	for name, change := range map[string]func(*ResourceAccessRecord){
		"production":                  func(r *ResourceAccessRecord) { r.Synthetic = false },
		"unknown-version":             func(r *ResourceAccessRecord) { r.Version = "unqualified.resource.v1" },
		"missing-full-target":         func(r *ResourceAccessRecord) { r.Target.OrgID = "" },
		"missing-predecessor":         func(r *ResourceAccessRecord) { r.Revision = 2 },
		"wrong-initial-predecessor":   func(r *ResourceAccessRecord) { r.PreviousHash = "sha256:" + strings.Repeat("a", 64) },
		"unknown-owner-with-identity": func(r *ResourceAccessRecord) { r.Resource.Ownership = "unknown" },
		"missing-ACL-version":         func(r *ResourceAccessRecord) { r.PolicyVersion = "" },
		"missing-attempt-kind":        func(r *ResourceAccessRecord) { r.Input.AttemptKind = "" },
		"broken-transform":            func(r *ResourceAccessRecord) { r.Input.Transforms[0].InputVersion = "other-version" },
		"oversized-transform-chain":   func(r *ResourceAccessRecord) { r.Input.Transforms = make([]ResourceTransform, 9) },
		"control-in-name":             func(r *ResourceAccessRecord) { r.Resource.Name = "name\ncontent" },
	} {
		t.Run(name, func(t *testing.T) {
			r := resourceFixture()
			change(&r)
			raw, _ := json.Marshal(r)
			if _, err := DecodeResourceAccess(raw); err == nil {
				t.Fatal("invalid resource record accepted")
			}
		})
	}
	raw, _ := json.Marshal(resourceFixture())
	for _, extra := range []string{`"prompt":"unwanted raw content"`, `"schema_version":"duplicate"`} {
		bad := append(append([]byte{}, raw[:len(raw)-1]...), []byte(","+extra+"}")...)
		if _, err := DecodeResourceAccess(bad); err == nil {
			t.Fatal("unknown/raw/duplicate field accepted")
		}
	}
	if _, err := DecodeResourceAccess([]byte(strings.Repeat(" ", MaxResourceRecordBytes+1))); err == nil {
		t.Fatal("oversized resource record accepted")
	}
}

func TestResourceDomainRejectsFieldAliasesAtEveryBoundary(t *testing.T) {
	raw, _ := json.Marshal(resourceFixture())
	for _, key := range []string{"synthetic", "reporter", "target", "source_system_id", "source_instance", "org_id", "project_id", "resource", "name", "input", "transforms", "receipt_ref", "withdrawn"} {
		t.Run(key, func(t *testing.T) {
			alias := strings.ToUpper(key)
			changed := strings.Replace(string(raw), `"`+key+`":`, `"`+alias+`":`, 1)
			if changed == string(raw) {
				t.Fatal("fixture field missing")
			}
			if _, err := DecodeResourceAccessForImport([]byte(changed)); err == nil {
				t.Fatal("case alias accepted")
			}
		})
	}
	for _, prefix := range []string{`"synthetic":true`, `"reporter":{"source_system_id":"11111111-1111-4111-8111-111111111111"`, `"target":{"source_system_id":"11111111-1111-4111-8111-111111111111"`} {
		replacement := prefix + `,"SYNTHETIC":true`
		if strings.Contains(prefix, "source_system_id") {
			replacement = prefix + `,"SOURCE_SYSTEM_ID":"22222222-2222-4222-8222-222222222222"`
		}
		changed := strings.Replace(string(raw), prefix, replacement, 1)
		if _, err := DecodeResourceAccessForImport([]byte(changed)); err == nil {
			t.Fatal("semantic duplicate accepted", prefix)
		}
	}
	if _, err := DecodeResourceAccessForImport(raw); err != nil {
		t.Fatal("canonical supported record rejected", err)
	}
}

func TestResourceLegacySpellingRemainsReadableButCannotBeNewInput(t *testing.T) {
	raw, _ := json.Marshal(resourceFixture())
	legacy := []byte(strings.Replace(string(raw), `"synthetic":true`, `"SYNTHETIC":true`, 1))
	if _, err := DecodeResourceAccess(legacy); err != nil {
		t.Fatal("historically accepted stored record lost restore compatibility", err)
	}
	if _, err := DecodeResourceAccessForImport(legacy); err == nil {
		t.Fatal("historical decoder must not permit new noncanonical input")
	}
}
