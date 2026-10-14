package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func contentBasisChoice() ContentChoice {
	zero, length := int64(0), int64(5)
	return ContentChoice{Scope: "finding", Reference: sourcefacts.FrozenManifestRef{SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "synthetic-source", OrgID: "17", ProjectID: "101", RequestID: "request:one", SourceFactID: "uf1_" + strings.Repeat("a", 64), Revision: "1", SourceSHA256: "sha256:" + strings.Repeat("b", 64), CaptureStatus: "complete", ContentRef: "uc1_" + strings.Repeat("c", 64), Representation: &sourcefacts.ContentRepresentation{MediaType: "text/plain", Encoding: "identity", ByteDomain: "stored_representation_before_http_content_encoding", SavedBytes: 5, Digest: sourcefacts.Hash([]byte("hello"))}, ExpiresAt: "2026-09-16T00:00:00.000000Z"}, Location: sourcefacts.ContentLocation{Kind: "byte_range", Offset: &zero, Length: &length}, Relation: "supports"}
}

func contentBasisFixture(t *testing.T) ReviewBasis {
	t.Helper()
	choice := contentBasisChoice()
	s := BasisSelection{Contents: []ContentChoice{choice}}
	b, err := CanonicalReviewBasis(ReviewBasis{Schema: ContentReviewBasisSchema, ContentBasisVersion: ContentBasisVersion, Statement: "A bounded content assertion", FindingVersion: 1, Known: s, Selected: s})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func cloneContentBasis(t *testing.T, b ReviewBasis) ReviewBasis {
	t.Helper()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var copied ReviewBasis
	if err = json.Unmarshal(raw, &copied); err != nil {
		t.Fatal(err)
	}
	return copied
}

func TestContentBasisIdentityOrderingAndNoRawPayload(t *testing.T) {
	b := contentBasisFixture(t)
	for name, change := range map[string]func(*ContentChoice){
		"source":         func(c *ContentChoice) { c.Reference.SourceSystemID = "22222222-2222-4222-8222-222222222222" },
		"instance":       func(c *ContentChoice) { c.Reference.SourceInstance = "another-source" },
		"org":            func(c *ContentChoice) { c.Reference.OrgID = "18" },
		"project":        func(c *ContentChoice) { c.Reference.ProjectID = "102" },
		"revision":       func(c *ContentChoice) { c.Reference.Revision = "2" },
		"representation": func(c *ContentChoice) { c.Reference.Representation.Digest = sourcefacts.Hash([]byte("other")) },
		"location":       func(c *ContentChoice) { *c.Location.Length = 4 },
		"relation":       func(c *ContentChoice) { c.Relation = "contradicts" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := cloneContentBasis(t, b)
			change(&changed.Selected.Contents[0])
			changed.Known.Contents = changed.Selected.Contents
			next, err := CanonicalReviewBasis(changed)
			if err != nil || next.Digest == b.Digest {
				t.Fatal("content identity not bound", err)
			}
		})
	}
	second := contentBasisChoice()
	second.Reference.RequestID = "request:two"
	b.Known.Contents = append(b.Known.Contents, second)
	b.Selected.Contents = b.Known.Contents
	x, err := CanonicalReviewBasis(b)
	if err != nil {
		t.Fatal(err)
	}
	b.Known.Contents = []ContentChoice{second, b.Known.Contents[0]}
	b.Selected.Contents = b.Known.Contents
	y, err := CanonicalReviewBasis(b)
	if err != nil || x.Digest != y.Digest {
		t.Fatal("reordering changed content digest", err)
	}
	a, c := contentBasisChoice(), contentBasisChoice()
	a.Reference.SourceInstance = "a:b"
	a.Reference.RequestID = "c"
	c.Reference.SourceInstance = "a"
	c.Reference.RequestID = "b:c"
	if ContentChoiceKey(a) == ContentChoiceKey(c) {
		t.Fatal("opaque tuple collision")
	}
	raw, _ := json.Marshal(x)
	for _, forbidden := range []string{`"record":`, `"payload":`, `"reading_id":`, `"hello"`} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatal("basis copied content or transient reading", forbidden)
		}
	}
}

func TestContentBasisRejectsUnsavedInvalidAndDowngradedReferences(t *testing.T) {
	for _, status := range []string{"pending", "not_captured"} {
		c := contentBasisChoice()
		c.Reference.CaptureStatus = status
		c.Reference.ContentRef = ""
		c.Reference.Representation = nil
		c.Reference.ExpiresAt = ""
		if ValidContentChoice(c) {
			t.Fatal("unsaved choice accepted")
		}
	}
	b := contentBasisFixture(t)
	for _, schema := range []string{ReviewBasisSchema, ResourceReviewBasisSchema} {
		bad := b
		bad.Schema = schema
		if _, err := CanonicalReviewBasis(bad); err == nil {
			t.Fatal("content basis downgrade accepted")
		}
	}
	bad := b
	bad.ContentBasisVersion = ""
	if _, err := CanonicalReviewBasis(bad); err == nil {
		t.Fatal("missing content basis domain")
	}
	bad = cloneContentBasis(t, b)
	bad.Selected.Contents[0].Relation = "context"
	if _, err := CanonicalReviewBasis(bad); err == nil {
		t.Fatal("unlinked finding choice")
	}
	if _, err := NormalizeBasisSelection(BasisSelection{Contents: []ContentChoice{contentBasisChoice(), contentBasisChoice()}}, 32); err == nil {
		t.Fatal("duplicate choice")
	}
	raw, _ := json.Marshal(contentBasisChoice())
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"scope":"finding"`), []byte(`"scope":"finding","scope":"case"`), 1),
		bytes.Replace(raw, []byte(`"scope":"finding"`), []byte(`"scope":"finding","raw_content":"secret"`), 1),
		bytes.Replace(raw, []byte(`"location":`), []byte(`"Location":`), 1),
	} {
		var c ContentChoice
		if json.Unmarshal(bad, &c) == nil {
			t.Fatal("non-strict content choice")
		}
	}
}

func TestContentBasisHumanReviewClosureAndMixedResources(t *testing.T) {
	b := contentBasisFixture(t)
	at := time.Date(2026, 9, 15, 1, 0, 0, 0, time.UTC)
	actor := Actor{Kind: "human", Ref: "xs_1111111111111111"}
	c := AssessmentCommand{Operation: "record_review", OperationID: "22222222-2222-4222-8222-222222222222", ExpectedCaseVersion: 1, ExpectedBasisDigest: b.Digest, Selection: b.Selected, Review: &ReviewPayload{Conclusion: "supported", Reason: "Inspected the bounded saved representation"}}
	current, err := AdvanceAssessment(Assessment{}, c, b, actor, at, true)
	if err != nil {
		t.Fatal(err)
	}
	c.Operation = "record_progress"
	c.Review = nil
	c.ExpectedVersion = current.Version
	c.Progress = &ProgressPayload{State: "ended", Reason: "Human verification", Closure: &Closure{Kind: "verified_remediation", Explanation: "Inspected selected bytes", Scope: "the selected representation", VerifiedAt: &at, VerificationContents: b.Selected.Contents}}
	c, err = NormalizeAssessmentCommand(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = AdvanceAssessment(current, c, b, actor, at, false); err != nil {
		t.Fatal("content-only closure", err)
	}
	c.Progress.Closure.VerificationContents = append([]ContentChoice{}, c.Progress.Closure.VerificationContents...)
	c.Progress.Closure.VerificationContents[0].Relation = "context"
	if _, err = AdvanceAssessment(current, c, b, actor, at, false); err == nil {
		t.Fatal("unselected closure content accepted")
	}
	r := resourceBasisFixture(t)
	b.Known.Resources = r.Known.Resources
	b.Selected.Resources = r.Selected.Resources
	b.AdoptedResources = r.AdoptedResources
	if _, err = CanonicalReviewBasis(b); err != nil {
		t.Fatal("resource/content mixed basis", err)
	}
	b = contentBasisFixture(t)
	b.Selected.Contents[0].Relation = "contradicts"
	b.Known.Contents = b.Selected.Contents
	b, err = CanonicalReviewBasis(b)
	if err != nil {
		t.Fatal(err)
	}
	c = AssessmentCommand{Operation: "record_review", ExpectedBasisDigest: b.Digest, Selection: b.Selected, Review: &ReviewPayload{Conclusion: "not_supported", Reason: "Saved bytes contradict the claim"}}
	if _, err = AdvanceAssessment(Assessment{}, c, b, actor, at, true); err != nil {
		t.Fatal("content-only contradiction", err)
	}
}

// These declarations preserve the pre-content layout and JSON field order.
type preContentSelection struct {
	Resources []ResourceChoice `json:"resources,omitempty"`
	Evidence  []EvidenceChoice `json:"evidence"`
	Gaps      []GapChoice      `json:"gaps"`
}
type preContentBasis struct {
	AdoptedResources []AdoptedResource   `json:"adopted_resources,omitempty"`
	Schema           string              `json:"schema"`
	Statement        string              `json:"statement"`
	FindingVersion   int64               `json:"finding_version"`
	Known            preContentSelection `json:"known"`
	Selected         preContentSelection `json:"selected"`
	AdoptedGaps      []AdoptedGap        `json:"adopted_gaps"`
	Digest           string              `json:"digest"`
}

func TestContentBasisEmptyPreservesV1V2BytesAndDigests(t *testing.T) {
	for _, b := range []ReviewBasis{assessmentBasisFixture(t), resourceBasisFixture(t)} {
		legacy := preContentBasis{b.AdoptedResources, b.Schema, b.Statement, b.FindingVersion, preContentSelection{b.Known.Resources, b.Known.Evidence, b.Known.Gaps}, preContentSelection{b.Selected.Resources, b.Selected.Evidence, b.Selected.Gaps}, b.AdoptedGaps, ""}
		before, _ := json.Marshal(legacy)
		hash := sha256.Sum256(before)
		legacy.Digest = "sha256:" + hex.EncodeToString(hash[:])
		before, _ = json.Marshal(legacy)
		b.Known.Contents = []ContentChoice{}
		b.Selected.Contents = []ContentChoice{}
		normalized, err := CanonicalReviewBasis(b)
		if err != nil {
			t.Fatal(err)
		}
		after, _ := json.Marshal(normalized)
		if !bytes.Equal(before, after) || legacy.Digest != normalized.Digest {
			t.Fatal("empty extension changed legacy basis bytes")
		}
		command := AssessmentCommand{Operation: "record_review", OperationID: "22222222-2222-4222-8222-222222222222", ExpectedCaseVersion: 1, ExpectedBasisDigest: b.Digest, Selection: b.Selected, Review: &ReviewPayload{Conclusion: "supported", Reason: "legacy"}}
		x, err := NormalizeAssessmentCommand(command)
		if err != nil {
			t.Fatal(err)
		}
		before, _ = json.Marshal(x)
		command.Selection.Contents = nil
		y, err := NormalizeAssessmentCommand(command)
		if err != nil {
			t.Fatal(err)
		}
		after, _ = json.Marshal(y)
		if !bytes.Equal(before, after) {
			t.Fatal("empty extension changed old command hash input")
		}
	}
}

func TestContentBasisPackV5AndLegacyVersions(t *testing.T) {
	at := time.Date(2026, 9, 15, 1, 0, 0, 0, time.UTC)
	caseID, findingID := "xc_AAAAAAAAAAAAAAAAAAAAAA", "xf_AAAAAAAAAAAAAAAAAAAAAA"
	record := CaseRecord{Case: Case{ID: caseID, Title: "Content review"}, Findings: []Finding{{ID: findingID, CaseID: caseID}}, Contents: []ContentLink{{CaseID: caseID, FindingID: findingID, Reference: contentBasisChoice(), AddedBySubject: "xs_1111111111111111", AddedAt: at}}}
	pack, raw, hash, err := BuildEvidencePack(record, at, "xs_1111111111111111", 1)
	if err != nil || pack.SchemaVersion != ContentEvidencePackSchema || !SupportedEvidencePack(pack) {
		t.Fatal("v5 pack", err)
	}
	var reopened EvidencePack
	if json.Unmarshal(raw, &reopened) != nil || !SupportedEvidencePack(reopened) {
		t.Fatal("v5 reopen")
	}
	if sourcefacts.Hash(raw) != hash {
		t.Fatal("pack hash")
	}
	pack.SchemaVersion = ResourceEvidencePackSchema
	if SupportedEvidencePack(pack) {
		t.Fatal("v5 content downgraded to v4")
	}
	for _, version := range []string{EvidencePackSchemaV1, EvidencePackSchemaV2, EvidencePackSchema, ResourceEvidencePackSchema} {
		old := EvidencePack{SchemaVersion: version, CaseID: caseID}
		if version != EvidencePackSchemaV1 {
			old.Responsibility = &FollowupSnapshot{Followups: []Followup{}, Actions: []FollowupAction{}}
		}
		if version == EvidencePackSchema || version == ResourceEvidencePackSchema {
			old.Assessments = &AssessmentSnapshot{Assessments: []Assessment{}, Actions: []AssessmentAction{}, Creations: []ReviewCreationReceipt{}}
		}
		if !SupportedEvidencePack(old) {
			t.Fatal("old pack rejected", version)
		}
		before, _ := json.Marshal(old)
		old.Contents = []ContentLink{}
		after, _ := json.Marshal(old)
		if !bytes.Equal(before, after) {
			t.Fatal("empty content changed old pack bytes", version)
		}
		var restored EvidencePack
		if json.Unmarshal(before, &restored) != nil || !SupportedEvidencePack(restored) {
			t.Fatal("old pack restore", version)
		}
		after, _ = json.Marshal(restored)
		if !bytes.Equal(before, after) {
			t.Fatal("old pack byte roundtrip", version)
		}
	}
}

func TestContentBasisLocationProofBindsSubjectAndExactChoice(t *testing.T) {
	c := contentBasisChoice()
	ctx := WithVerifiedContentLocations(context.Background(), "alice", []ContentChoice{c})
	if !ContentLocationVerified(ctx, "alice", c) || ContentLocationVerified(ctx, "bob", c) || ContentLocationVerified(context.Background(), "alice", c) {
		t.Fatal("proof identity boundary")
	}
	changed := c
	changed.Relation = "contradicts"
	if ContentLocationVerified(ctx, "alice", changed) {
		t.Fatal("proof reused for different relation")
	}
	next := WithVerifiedContentLocations(ctx, "alice", []ContentChoice{changed})
	if ContentLocationVerified(ctx, "alice", changed) || !ContentLocationVerified(next, "alice", changed) {
		t.Fatal("context proof mutated")
	}
}
