package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/workbench"
	"strings"
	"testing"
	"time"
)

func executionContextFixture() ExecutionContext {
	return ExecutionContext{
		WorkbenchReadingID: "xwr_" + strings.Repeat("a", 22), ExecutionID: "xe1_" + strings.Repeat("b", 64),
		ExecutionAsOf: time.Date(2026, 9, 20, 1, 2, 3, 123456000, time.UTC), RuntimeReadingID: "xrt_" + strings.Repeat("c", 22), RuntimeRequestRef: "xrr_" + strings.Repeat("d", 64),
	}
}
func contextBasis(t *testing.T) ReviewBasis {
	t.Helper()
	b := resourceBasisFixture(t)
	c := executionContextFixture()
	ref := b.Selected.Resources[0]
	c.ExecutionID, _ = executionid.New(ref.Target.SourceSystemID, ref.RequestID)
	b.ResolvedExecutionContext = &ResolvedExecutionContext{Context: c, RuntimeCutoff: c.ExecutionAsOf.Add(time.Minute), WorkbenchMembershipRef: "sha256:" + strings.Repeat("a", 64), RuntimeMembershipRef: "sha256:" + strings.Repeat("b", 64), Target: ref.Target, RequestID: ref.RequestID, Resources: b.Selected.Resources}
	b, err := CanonicalReviewBasis(b)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func contextCommand(t *testing.T, b ReviewBasis) ReviewCreationCommand {
	t.Helper()
	c := ReviewCreationCommand{Operation: "start_review", OperationID: "22222222-2222-4222-8222-222222222222", NewCase: &CreateCaseInput{Title: "Bounded review"}, Finding: CreateFindingInput{Title: "Claim", Statement: b.Statement, Classification: "hypothesis"}, Selection: b.Selected, ExpectedBasisDigest: b.Digest, Review: &ReviewPayload{Conclusion: "supported", Reason: "Read exact revision"}, Reason: "Start"}
	if b.ResolvedExecutionContext != nil {
		v := b.ResolvedExecutionContext.Context
		c.ExecutionContext = &v
	}
	c, err := NormalizeReviewCreation(c)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestExecutionContextStrictInputAndNormalization(t *testing.T) {
	c := executionContextFixture()
	raw, _ := json.Marshal(c)
	for _, bad := range []string{strings.Replace(string(raw), `"execution_id":`, `"Execution_ID":`, 1), strings.Replace(string(raw), `"execution_id":`, `"execution_id":"bad","execution_id":`, 1), strings.Replace(string(raw), `"execution_id":`, `"extra":true,"execution_id":`, 1), `{}`, `null`} {
		var got ExecutionContext
		if json.Unmarshal([]byte(bad), &got) == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	for _, change := range []func(*ExecutionContext){func(c *ExecutionContext) { c.ExecutionID = "other" }, func(c *ExecutionContext) { c.WorkbenchReadingID = " xwr_" + strings.Repeat("a", 22) }, func(c *ExecutionContext) { c.RuntimeReadingID = c.WorkbenchReadingID }, func(c *ExecutionContext) { c.RuntimeRequestRef = "xrr_bad" }, func(c *ExecutionContext) { c.ExecutionAsOf = c.ExecutionAsOf.Add(time.Nanosecond) }, func(c *ExecutionContext) { c.ExecutionAsOf = time.Time{} }} {
		bad := c
		change(&bad)
		if _, e := NormalizeExecutionContext(bad); e == nil {
			t.Fatal("accepted bad context", bad)
		}
	}
	c.ExecutionAsOf = c.ExecutionAsOf.In(time.FixedZone("offset", 8*3600))
	norm, e := NormalizeExecutionContext(c)
	if e != nil || norm.ExecutionAsOf.Location() != time.UTC {
		t.Fatal(norm, e)
	}
	command := contextCommand(t, contextBasis(t))
	wire, _ := json.Marshal(command)
	for _, bad := range []string{strings.Replace(string(wire), `"execution_context":`, `"Execution_Context":`, 1), strings.Replace(string(wire), `"execution_context":`, `"execution_context":null,"execution_context":`, 1)} {
		var got ReviewCreationCommand
		if json.Unmarshal([]byte(bad), &got) == nil {
			t.Fatal("ambiguous outer context accepted")
		}
	}
	command.Selection = BasisSelection{}
	command.Review = &ReviewPayload{Conclusion: "pending_review", Reason: "Pending"}
	if _, e = NormalizeReviewCreation(command); e == nil {
		t.Fatal("context without resources accepted")
	}
}
func TestExecutionContextBindsBothCutoffsAndOriginAfterSelectionChanges(t *testing.T) {
	b := contextBasis(t)
	for _, change := range []func(*ResolvedExecutionContext){func(v *ResolvedExecutionContext) {
		v.Context.ExecutionAsOf = v.Context.ExecutionAsOf.Add(time.Microsecond)
	}, func(v *ResolvedExecutionContext) { v.RuntimeCutoff = v.RuntimeCutoff.Add(time.Microsecond) }, func(v *ResolvedExecutionContext) { v.Context.RuntimeReadingID = "xrt_" + strings.Repeat("z", 22) }, func(v *ResolvedExecutionContext) { v.WorkbenchMembershipRef = "sha256:" + strings.Repeat("c", 64) }} {
		changed := b
		v := *b.ResolvedExecutionContext
		change(&v)
		changed.ResolvedExecutionContext = &v
		got, e := CanonicalReviewBasis(changed)
		if e != nil || got.Digest == b.Digest {
			t.Fatal("context not bound", e)
		}
	}
	changed := b
	changed.Selected = BasisSelection{}
	changed.AdoptedResources = nil
	got, e := CanonicalReviewBasis(changed)
	if e != nil || !SameAssessmentValue(got.ResolvedExecutionContext, b.ResolvedExecutionContext) {
		t.Fatal("origin lost when selection changed", e)
	}
	changed.Known.Resources = nil
	if _, e = CanonicalReviewBasis(changed); e == nil {
		t.Fatal("origin detached from immutable resource links")
	}
}

// Exact pre-extension field layout, independently specified instead of deleting
// a field from the new JSON. Includes the earlier content extension.
type preExecutionBasis struct {
	ContentBasisVersion string            `json:"content_basis_version,omitempty"`
	AdoptedResources    []AdoptedResource `json:"adopted_resources,omitempty"`
	Schema              string            `json:"schema"`
	Statement           string            `json:"statement"`
	FindingVersion      int64             `json:"finding_version"`
	Known               BasisSelection    `json:"known"`
	Selected            BasisSelection    `json:"selected"`
	AdoptedGaps         []AdoptedGap      `json:"adopted_gaps"`
	Digest              string            `json:"digest"`
}
type preExecutionCommand struct {
	Operation           string                  `json:"operation"`
	OperationID         string                  `json:"operation_id"`
	CaseID              string                  `json:"case_id"`
	ExpectedCaseVersion int64                   `json:"expected_case_version"`
	NewCase             *CreateCaseInput        `json:"new_case"`
	Finding             CreateFindingInput      `json:"finding"`
	Selection           BasisSelection          `json:"selection"`
	ExpectedBasisDigest string                  `json:"expected_basis_digest"`
	Review              *ReviewPayload          `json:"review"`
	From                *SealedFindingReference `json:"from"`
	Reason              string                  `json:"reason"`
}

func TestExecutionContextOmittedPreservesHistoricalBytesAndHashes(t *testing.T) {
	for _, b := range []ReviewBasis{assessmentBasisFixture(t), resourceBasisFixture(t), contentBasisFixture(t)} {
		old := preExecutionBasis{b.ContentBasisVersion, b.AdoptedResources, b.Schema, b.Statement, b.FindingVersion, b.Known, b.Selected, b.AdoptedGaps, b.Digest}
		old.Digest = ""
		hashInput, _ := json.Marshal(old)
		digest := sha256.Sum256(hashInput)
		old.Digest = "sha256:" + hex.EncodeToString(digest[:])
		if old.Digest != b.Digest {
			t.Fatal("historical canonical digest changed")
		}
		x, _ := json.Marshal(old)
		y, _ := json.Marshal(b)
		if !bytes.Equal(x, y) {
			t.Fatal("historical basis bytes changed")
		}
		// Use a legal pending review for the independent selection fixtures.
		c := ReviewCreationCommand{Operation: "start_review", OperationID: "22222222-2222-4222-8222-222222222222", NewCase: &CreateCaseInput{Title: "Legacy"}, Finding: CreateFindingInput{Title: "Claim", Statement: b.Statement, Classification: "hypothesis"}, Selection: b.Selected, ExpectedBasisDigest: b.Digest, Review: &ReviewPayload{Conclusion: "pending_review", Reason: "Legacy"}, Reason: "Legacy"}
		c, e := NormalizeReviewCreation(c)
		if e != nil {
			t.Fatal(e)
		}
		legacy := preExecutionCommand{c.Operation, c.OperationID, c.CaseID, c.ExpectedCaseVersion, c.NewCase, c.Finding, c.Selection, c.ExpectedBasisDigest, c.Review, c.From, c.Reason}
		x, _ = json.Marshal(legacy)
		y, _ = json.Marshal(c)
		if !bytes.Equal(x, y) || sha256.Sum256(x) != sha256.Sum256(y) {
			t.Fatal("legacy request bytes/hash changed")
		}
		wire := strings.TrimSuffix(string(x), "}") + `,"execution_context":null}`
		var round ReviewCreationCommand
		if json.Unmarshal([]byte(wire), &round) != nil {
			t.Fatal("optional null")
		}
		y, _ = json.Marshal(round)
		if !bytes.Equal(x, y) {
			t.Fatal("null changed legacy bytes")
		}
		oldReceipt := struct {
			Command preExecutionCommand `json:"command"`
			Basis   preExecutionBasis   `json:"basis"`
		}{legacy, old}
		newReceipt := struct {
			Command ReviewCreationCommand `json:"command"`
			Basis   ReviewBasis           `json:"basis"`
		}{c, b}
		x, _ = json.Marshal(oldReceipt)
		y, _ = json.Marshal(newReceipt)
		if !bytes.Equal(x, y) {
			t.Fatal("receipt nesting changed legacy bytes")
		}
	}
}
func TestExecutionContextPackRequiresDurableLinksAndActionContinuity(t *testing.T) {
	b := contextBasis(t)
	c := contextCommand(t, b)
	at := c.ExecutionContext.ExecutionAsOf
	p := EvidencePack{Executions: []CaseExecution{{ExecutionID: c.ExecutionContext.ExecutionID, AsOf: at}}, Resources: []ResourceLink{{FindingID: "finding", Reference: b.Selected.Resources[0]}}, Assessments: &AssessmentSnapshot{Creations: []ReviewCreationReceipt{{FindingID: "finding", Command: c, Basis: b, Action: &AssessmentAction{Basis: b, Current: Assessment{FindingID: "finding", Review: &ReviewRecord{Basis: b}}}}}, Assessments: []Assessment{{FindingID: "finding", Review: &ReviewRecord{Basis: b}}}}}
	if e := ValidateExecutionContextPack(p); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*EvidencePack){func(p *EvidencePack) { p.Executions = nil }, func(p *EvidencePack) { p.Executions[0].AsOf = p.Executions[0].AsOf.Add(time.Microsecond) }, func(p *EvidencePack) { p.Resources = nil }, func(p *EvidencePack) { p.Assessments.Assessments[0].Review.Basis.ResolvedExecutionContext = nil }, func(p *EvidencePack) { p.Assessments.Creations[0].Command.ExecutionContext = nil }} {
		raw, _ := json.Marshal(p)
		var bad EvidencePack
		_ = json.Unmarshal(raw, &bad)
		mutate(&bad)
		if ValidateExecutionContextPack(bad) == nil {
			t.Fatal("accepted detached/tampered context")
		}
	}
}

func contentExecutionBasis(t *testing.T) ReviewBasis {
	t.Helper()
	b := contentBasisFixture(t)
	ref := b.Selected.Contents[0].Reference
	c := executionContextFixture()
	c.ExecutionID, _ = executionid.New(ref.SourceSystemID, ref.RequestID)
	b.ResolvedExecutionContext = &ResolvedExecutionContext{Context: c, RuntimeCutoff: c.ExecutionAsOf.Add(time.Minute), WorkbenchMembershipRef: "sha256:" + strings.Repeat("a", 64), RuntimeMembershipRef: "sha256:" + strings.Repeat("b", 64), Target: workbench.ResourceScope{SourceSystemID: ref.SourceSystemID, SourceInstance: ref.SourceInstance, OrgID: ref.OrgID, ProjectID: ref.ProjectID}, RequestID: ref.RequestID, Resources: []ResourceChoice{}, Contents: b.Selected.Contents}
	b, err := CanonicalReviewBasis(b)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestContentExecutionContextCanonicalIdentityAndOrigin(t *testing.T) {
	b := contentExecutionBasis(t)
	c := contextCommand(t, b)
	if !ExecutionContextSelectionMatches(b.ResolvedExecutionContext, c.Selection) {
		t.Fatal("content selection mismatch")
	}
	for name, mutate := range map[string]func(*ContentChoice){"request": func(c *ContentChoice) { c.Reference.RequestID = "other" }, "project": func(c *ContentChoice) { c.Reference.ProjectID = "other" }, "instance": func(c *ContentChoice) { c.Reference.SourceInstance = "other" }, "relation": func(c *ContentChoice) { c.Relation = "context" }, "location": func(c *ContentChoice) { *c.Location.Length = 1 }, "hash": func(c *ContentChoice) { c.Reference.SourceSHA256 = "sha256:" + strings.Repeat("d", 64) }} {
		t.Run(name, func(t *testing.T) {
			bad := cloneContentBasis(t, b)
			mutate(&bad.ResolvedExecutionContext.Contents[0])
			if _, err := CanonicalReviewBasis(bad); err == nil {
				t.Fatal("accepted unbound original content")
			}
		})
	}
	later := cloneContentBasis(t, b)
	later.Selected = BasisSelection{}
	later, err := CanonicalReviewBasis(later)
	if err != nil || !SameAssessmentValue(later.ResolvedExecutionContext, b.ResolvedExecutionContext) {
		t.Fatal("later selection lost immutable origin", err)
	}
	empty := cloneContentBasis(t, b)
	empty.ResolvedExecutionContext.Contents = nil
	if _, err = CanonicalReviewBasis(empty); err == nil {
		t.Fatal("empty origin accepted")
	}
	// Relation changes are legitimate only when both known and original agree, and change the digest.
	changed := cloneContentBasis(t, b)
	changed.Selected.Contents[0].Relation = "context"
	changed.Known.Contents[0].Relation = "context"
	changed.ResolvedExecutionContext.Contents[0].Relation = "context"
	changed, err = CanonicalReviewBasis(changed)
	if err != nil || changed.Digest == b.Digest {
		t.Fatal("relation not digest-bound", err)
	}
}
func TestContentExecutionContextPackContinuity(t *testing.T) {
	b := contentExecutionBasis(t)
	c := contextCommand(t, b)
	p := EvidencePack{Executions: []CaseExecution{{ExecutionID: c.ExecutionContext.ExecutionID, AsOf: c.ExecutionContext.ExecutionAsOf}}, Contents: []ContentLink{{FindingID: "finding", Reference: b.Selected.Contents[0]}}, Assessments: &AssessmentSnapshot{Creations: []ReviewCreationReceipt{{FindingID: "finding", Command: c, Basis: b, Action: &AssessmentAction{Basis: b, Current: Assessment{FindingID: "finding", Review: &ReviewRecord{Basis: b}}}}}, Assessments: []Assessment{{FindingID: "finding", Review: &ReviewRecord{Basis: b}}}}}
	if err := ValidateExecutionContextPack(p); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*EvidencePack){func(p *EvidencePack) { p.Contents = nil }, func(p *EvidencePack) { p.Executions = nil }, func(p *EvidencePack) { p.Contents[0].FindingID = "other" }, func(p *EvidencePack) { p.Contents[0].Reference.Relation = "context" }, func(p *EvidencePack) { p.Assessments.Assessments[0].Review.Basis.ResolvedExecutionContext = nil }, func(p *EvidencePack) { p.Assessments.Creations[0].Command.Selection.Contents = nil }} {
		raw, _ := json.Marshal(p)
		var bad EvidencePack
		_ = json.Unmarshal(raw, &bad)
		mutate(&bad)
		if ValidateExecutionContextPack(bad) == nil {
			t.Fatal("detached content context accepted")
		}
	}
}
func TestResourceExecutionContextPreservesPreContentBytes(t *testing.T) {
	b := contextBasis(t)
	v := b.ResolvedExecutionContext
	old := struct {
		Context                ExecutionContext        `json:"context"`
		RuntimeCutoff          time.Time               `json:"runtime_cutoff"`
		WorkbenchMembershipRef string                  `json:"workbench_membership_ref"`
		RuntimeMembershipRef   string                  `json:"runtime_membership_ref"`
		Target                 workbench.ResourceScope `json:"target"`
		RequestID              string                  `json:"request_id"`
		Resources              []ResourceChoice        `json:"resources"`
	}{v.Context, v.RuntimeCutoff, v.WorkbenchMembershipRef, v.RuntimeMembershipRef, v.Target, v.RequestID, v.Resources}
	x, _ := json.Marshal(old)
	y, _ := json.Marshal(v)
	if !bytes.Equal(x, y) {
		t.Fatal("resource context bytes changed")
	}
	oldBasis := struct {
		Resolved any `json:"resolved_execution_context,omitempty"`
		preExecutionBasis
	}{old, preExecutionBasis{b.ContentBasisVersion, b.AdoptedResources, b.Schema, b.Statement, b.FindingVersion, b.Known, b.Selected, b.AdoptedGaps, ""}}
	raw, _ := json.Marshal(oldBasis)
	digest := sha256.Sum256(raw)
	if "sha256:"+hex.EncodeToString(digest[:]) != b.Digest {
		t.Fatal("resource basis hash changed")
	}
	// Previously legal mixed reviews retain a resource-only origin when Contents is omitted.
	selection := b.Selected
	selection.Contents = []ContentChoice{contentBasisChoice()}
	if !ExecutionContextSelectionMatches(v, selection) {
		t.Fatal("legacy mixed review invalidated")
	}
}
