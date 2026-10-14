package workspace

import (
	"encoding/json"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"strings"
	"testing"
	"time"
)

func resourceBasisFixture(t *testing.T) ReviewBasis {
	t.Helper()
	scope := workbench.ResourceScope{SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "synthetic-app", OrgID: "17", ProjectID: "101"}
	record := workbench.ResourceAccessRecord{Version: workbench.ResourceAccessVersion, Synthetic: true, RecordRef: "resource-1", Revision: 1, Reporter: scope, Target: scope, RequestID: "request-1", ApplicationRef: "app-1", ActorRef: "actor-1", OccurredAt: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), ReportKind: "source_audit", Resource: workbench.ResourceObject{Ref: "doc-1", Version: "v1", Name: "Synthetic", Location: "enterprise_service", Ownership: "unknown", Classification: "unknown", DeviceManagement: "unknown"}, Action: "read", AccessOutcome: "unknown", AccessDecision: "unknown"}
	raw, _ := json.Marshal(record)
	v, _ := connector.DecodeStrictJSONObject(raw)
	raw, _ = connector.CanonicalizeJSONValue(v)
	r := ResourceChoice{Scope: "finding", Reporter: scope, Target: scope, RequestID: record.RequestID, RecordRef: record.RecordRef, Revision: 1, RecordHash: sourcefacts.Hash(raw), Relation: "supports"}
	s := BasisSelection{Resources: []ResourceChoice{r}}
	b, err := CanonicalReviewBasis(ReviewBasis{Schema: ResourceReviewBasisSchema, Statement: "Review this synthetic report", FindingVersion: 1, Known: s, Selected: s, AdoptedResources: []AdoptedResource{{Reference: r, Record: raw}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestResourceBasisVersionIdentityAndHumanProgress(t *testing.T) {
	b := resourceBasisFixture(t)
	r := b.Selected.Resources[0]
	wire, _ := json.Marshal(r)
	for _, bad := range []string{strings.Replace(string(wire), `"record_ref":`, `"Record_Ref":`, 1), strings.Replace(string(wire), `"source_instance":`, `"Source_Instance":`, 1), strings.Replace(string(wire), `"scope":"finding"`, `"scope":"case","scope":"finding"`, 1)} {
		var decoded ResourceChoice
		if json.Unmarshal([]byte(bad), &decoded) == nil {
			t.Fatal("noncanonical resource reference accepted")
		}
	}

	for _, change := range []func(*ReviewBasis){func(x *ReviewBasis) { x.Schema = ReviewBasisSchema }, func(x *ReviewBasis) {
		x.AdoptedResources[0].Record = []byte(strings.Replace(string(x.AdoptedResources[0].Record), `"withdrawn":false`, `"withdrawn":true`, 1))
	}, func(x *ReviewBasis) { x.AdoptedResources[0].Reference.Target.ProjectID = "999" }, func(x *ReviewBasis) { x.AdoptedResources = nil }} {
		raw, _ := json.Marshal(b)
		var changed ReviewBasis
		_ = json.Unmarshal(raw, &changed)
		change(&changed)
		if _, err := CanonicalReviewBasis(changed); err == nil {
			t.Fatal("inconsistent frozen resource accepted")
		}
	}
	if _, err := NormalizeBasisSelection(BasisSelection{Resources: []ResourceChoice{r, r}}, 32); err == nil {
		t.Fatal("duplicate reference accepted")
	}
	at := time.Date(2026, 9, 14, 1, 0, 0, 0, time.UTC)
	actor := Actor{Kind: "human", Ref: "xs_1111111111111111"}
	command := AssessmentCommand{Operation: "record_review", OperationID: "22222222-2222-4222-8222-222222222222", ExpectedCaseVersion: 1, ExpectedBasisDigest: b.Digest, Selection: b.Selected, Review: &ReviewPayload{Conclusion: "supported", Reason: "Reviewed exact synthetic revision"}}
	current, err := AdvanceAssessment(Assessment{}, command, b, actor, at, true)
	if err != nil || current.Review.Conclusion != "supported" {
		t.Fatal("resource-only support", err)
	}
	command.Operation = "record_progress"
	command.Review = nil
	command.ExpectedVersion = current.Version
	command.Progress = &ProgressPayload{State: "ended", Reason: "Bounded human verification", Closure: &Closure{Kind: "verified_remediation", Explanation: "Human checked this revision", Scope: "synthetic resource only", VerifiedAt: &at, VerificationResources: []ResourceChoice{r}}}
	command, err = NormalizeAssessmentCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = AdvanceAssessment(current, command, b, actor, at, false); err != nil {
		t.Fatal("resource-only closure", err)
	}
	bad := command
	bad.Progress = &ProgressPayload{State: "ended", Reason: "test", Closure: &Closure{Kind: "verified_remediation", Explanation: "test", Scope: "test", VerifiedAt: &at, VerificationResources: []ResourceChoice{r}}}
	bad.Progress.Closure.VerificationResources[0].RecordHash = "sha256:" + strings.Repeat("a", 64)
	if _, err = AdvanceAssessment(current, bad, b, actor, at, false); err == nil {
		t.Fatal("unselected verification resource accepted")
	}
	legacy := assessmentBasisFixture(t)
	raw, _ := json.Marshal(legacy)
	if strings.Contains(string(raw), "resources") {
		t.Fatal("legacy basis serialization changed")
	}
}

func TestResourceBasisCanMixWithEvidenceAndGaps(t *testing.T) {
	resource := resourceBasisFixture(t)
	mixed := assessmentBasisFixture(t)
	mixed.Schema = ResourceReviewBasisSchema
	mixed.Known.Resources = resource.Known.Resources
	mixed.Selected.Resources = resource.Selected.Resources
	mixed.AdoptedResources = resource.AdoptedResources
	b, err := CanonicalReviewBasis(mixed)
	if err != nil || len(b.Selected.Evidence) != 1 || len(b.Selected.Gaps) != 1 || len(b.Selected.Resources) != 1 {
		t.Fatal("mixed basis", err)
	}
	if b.Digest == resource.Digest || b.Digest == assessmentBasisFixture(t).Digest {
		t.Fatal("mixed basis omitted a reference class")
	}
	partial := b
	partial.Selected.Resources = nil
	partial.AdoptedResources = nil
	partial, err = CanonicalReviewBasis(partial)
	if err != nil || partial.Schema != ResourceReviewBasisSchema || len(partial.Known.Resources) != 1 {
		t.Fatal("known unselected resource lost", err)
	}
	r := resource.Selected.Resources[0]
	r.Relation = "contradicts"
	resource.Known.Resources = []ResourceChoice{r}
	resource.Selected.Resources = []ResourceChoice{r}
	resource.AdoptedResources[0].Reference = r
	resource, err = CanonicalReviewBasis(resource)
	if err != nil {
		t.Fatal(err)
	}
	c := AssessmentCommand{Operation: "record_review", ExpectedBasisDigest: resource.Digest, Selection: resource.Selected, Review: &ReviewPayload{Conclusion: "not_supported", Reason: "The report contradicts the assertion"}}
	if _, err = AdvanceAssessment(Assessment{}, c, resource, Actor{Kind: "human", Ref: "xs_1111111111111111"}, time.Now(), true); err != nil {
		t.Fatal("resource-only contradiction", err)
	}
}

func TestResourceBasisPreservesAcceptedJSONSpellings(t *testing.T) {
	for _, spelling := range []string{"previous_hash", "occurred_at", "owner_ref"} {
		t.Run(spelling, func(t *testing.T) {
			b := resourceBasisFixture(t)
			obj, _ := connector.DecodeStrictJSONObject(b.AdoptedResources[0].Record)
			switch spelling {
			case "previous_hash":
				obj[spelling] = ""
			case "occurred_at":
				obj[spelling] = "2026-09-14T00:00:00+00:00"
			case "owner_ref":
				obj["resource"].(map[string]any)[spelling] = ""
			}
			raw, _ := connector.CanonicalizeJSONValue(obj)
			if _, err := workbench.DecodeResourceAccessForImport(raw); err != nil {
				t.Fatal(err)
			}
			r := b.Selected.Resources[0]
			r.RecordHash = sourcefacts.Hash(raw)
			b.Known.Resources[0] = r
			b.Selected.Resources[0] = r
			b.AdoptedResources = []AdoptedResource{{Reference: r, Record: raw}}
			canonical, err := CanonicalReviewBasis(b)
			if err != nil {
				t.Fatal("accepted input rejected as basis", err)
			}
			wire, err := json.Marshal(canonical)
			if err != nil {
				t.Fatal(err)
			}
			var reopened ReviewBasis
			if json.Unmarshal(wire, &reopened) != nil {
				t.Fatal("reopen")
			}
			again, err := CanonicalReviewBasis(reopened)
			if err != nil || again.Digest != canonical.Digest || !ResourceMatchesRecord(r, again.AdoptedResources[0].Record) {
				t.Fatal("raw representation/hash lost", err)
			}
			r.RecordHash = "sha256:" + strings.Repeat("f", 64)
			if ResourceMatchesRecord(r, raw) {
				t.Fatal("wrong original hash accepted")
			}
		})
	}
}
