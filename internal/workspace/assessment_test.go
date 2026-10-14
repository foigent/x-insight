package workspace

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func assessmentBasisFixture(t *testing.T) ReviewBasis {
	t.Helper()
	e := EvidenceChoice{Scope: "finding", EvidenceEventID: "11111111-1111-4111-8111-111111111111", Relation: "supports", EventHash: "sha256:" + strings.Repeat("a", 64), SourceRef: "fixture:one"}
	g := GapChoice{Scope: "finding", GapID: "22222222-2222-4222-8222-222222222222", Relation: "limits", AffectedClaim: "Tool effect is unknown", GapType: "missing_trace"}
	s := BasisSelection{Evidence: []EvidenceChoice{e}, Gaps: []GapChoice{g}}
	b, err := CanonicalReviewBasis(ReviewBasis{Schema: ReviewBasisSchema, Statement: "A human assertion", FindingVersion: 1, Known: s, Selected: s, AdoptedGaps: []AdoptedGap{{Reference: g}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReviewBasisKeepsKnownAndAdoptedContentsDistinct(t *testing.T) {
	b := assessmentBasisFixture(t)
	original := b.Digest
	unknown := b
	unknown.AdoptedGaps = append([]AdoptedGap{}, b.AdoptedGaps...)
	unknown.AdoptedGaps[0].State = "open"
	if _, err := CanonicalReviewBasis(unknown); err == nil {
		t.Fatal("absent resolution manufactured a state")
	}
	at := time.Date(2026, 9, 1, 2, 3, 4, 123456000, time.UTC)
	adopted := AdoptedGap{Reference: b.Selected.Gaps[0], ResolutionRecorded: true, ResolutionVersion: 1, State: "investigating", Note: "Only reviewed the first trace", UpdatedBySubject: "xs_1111111111111111", UpdatedAt: &at}
	b.AdoptedGaps = []AdoptedGap{adopted}
	b, err := CanonicalReviewBasis(b)
	if err != nil {
		t.Fatal(err)
	}
	if b.Digest == original {
		t.Fatal("resolution contents not bound")
	}
	for _, change := range []func(*AdoptedGap){func(g *AdoptedGap) { g.Note = "A later account" }, func(g *AdoptedGap) { g.ResolutionVersion++ }, func(g *AdoptedGap) { g.State = "source_fixed" }, func(g *AdoptedGap) { g.UpdatedBySubject = "xs_2222222222222222" }, func(g *AdoptedGap) { v := g.UpdatedAt.Add(time.Second); g.UpdatedAt = &v }} {
		changed := b
		changed.AdoptedGaps = append([]AdoptedGap{}, b.AdoptedGaps...)
		change(&changed.AdoptedGaps[0])
		if ValidReviewBasis(changed) {
			t.Fatal("tampered adopted content accepted")
		}
		next, e := CanonicalReviewBasis(changed)
		if e != nil || next.Digest == b.Digest {
			t.Fatalf("change not detected: %v", e)
		}
	}
	partial := b
	partial.Selected = BasisSelection{Evidence: []EvidenceChoice{}, Gaps: b.Selected.Gaps}
	partial, err = CanonicalReviewBasis(partial)
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.Known.Evidence) != 1 || len(partial.Selected.Evidence) != 0 {
		t.Fatal("known but unselected evidence lost")
	}
	rebuilt, err := CanonicalReviewBasis(partial)
	if err != nil || rebuilt.Digest != partial.Digest {
		t.Fatal("same known references changed the digest")
	}
	extra := b.Known.Evidence[0]
	extra.EvidenceEventID = "33333333-3333-4333-8333-333333333333"
	partial.Known.Evidence = append(append([]EvidenceChoice{}, partial.Known.Evidence...), extra)
	next, err := CanonicalReviewBasis(partial)
	if err != nil || next.Digest == rebuilt.Digest {
		t.Fatal("new relevant reference not detected")
	}
}

func TestAssessmentConclusionsProgressAndClosures(t *testing.T) {
	basis := assessmentBasisFixture(t)
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	actor := Actor{Kind: "human", Ref: "xs_1111111111111111"}
	empty := Assessment{CaseID: "xc_1111111111111111111111", FindingID: "xf_1111111111111111111111"}
	command := func(a Assessment, b ReviewBasis, r *ReviewPayload, p *ProgressPayload) AssessmentCommand {
		op := "record_review"
		if p != nil {
			op = "record_progress"
		}
		c, err := NormalizeAssessmentCommand(AssessmentCommand{Operation: op, OperationID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", ExpectedVersion: a.Version, ExpectedCaseVersion: a.Version + 2, ExpectedBasisDigest: b.Digest, Selection: b.Selected, Review: r, Progress: p})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	for _, conclusion := range []string{"pending_review", "supported", "insufficient_evidence"} {
		c := command(empty, basis, &ReviewPayload{Conclusion: conclusion, Reason: "Human explanation"}, nil)
		a, err := AdvanceAssessment(empty, c, basis, actor, now, true)
		if err != nil || a.Review.Conclusion != conclusion || a.Progress.State != "not_started" || empty.Recorded {
			t.Fatalf("initial state %s: %v", conclusion, err)
		}
	}
	c := command(empty, basis, &ReviewPayload{Conclusion: "not_supported", Reason: "No counterevidence selected"}, nil)
	if _, err := AdvanceAssessment(empty, c, basis, actor, now, false); err == nil {
		t.Fatal("not-supported without counterevidence accepted")
	}
	noSources, err := CanonicalReviewBasis(ReviewBasis{Schema: ReviewBasisSchema, Statement: basis.Statement, FindingVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, conclusion := range []string{"supported", "not_supported", "insufficient_evidence"} {
		c := command(empty, noSources, &ReviewPayload{Conclusion: conclusion, Reason: "Needs sources"}, nil)
		if _, err := AdvanceAssessment(empty, c, noSources, actor, now, false); err == nil {
			t.Fatalf("empty basis accepted %s", conclusion)
		}
	}
	c = command(empty, noSources, &ReviewPayload{Conclusion: "insufficient_evidence", Reason: "Source not onboarded", MissingEvidence: "Post-action tool result"}, nil)
	if _, err = AdvanceAssessment(empty, c, noSources, actor, now, false); err != nil {
		t.Fatal("explicit missing evidence rejected", err)
	}
	c = command(empty, basis, &ReviewPayload{Conclusion: "supported", Reason: "Reviewed support"}, nil)
	a, err := AdvanceAssessment(empty, c, basis, actor, now, false)
	if err != nil || a.Progress != nil {
		t.Fatal("review silently created progress")
	}
	c = command(a, basis, nil, &ProgressPayload{State: "in_progress", Reason: "Cannot skip unrecorded progress"})
	if _, err = AdvanceAssessment(a, c, basis, actor, now, false); err == nil {
		t.Fatal("unrecorded progress silently inferred")
	}
	c = command(a, basis, nil, &ProgressPayload{State: "not_started", Reason: "Explicit start status"})
	a, err = AdvanceAssessment(a, c, basis, actor, now, false)
	if err != nil {
		t.Fatal(err)
	}
	verified := now.Add(-time.Hour)
	closures := []Closure{
		{Kind: "verified_remediation", Explanation: "Human inspected post-fix result", Scope: "One synthetic execution on September 9", VerifiedAt: &verified, VerificationEvidence: basis.Selected.Evidence},
		{Kind: "duplicate", Explanation: "Same human claim and source", Duplicate: &FindingReference{CaseID: "xc_2222222222222222222222", FindingID: "xf_2222222222222222222222"}},
		{Kind: "end_followup", Explanation: "End this investigation attempt", RemainingUnknowns: "Other executions have not been observed"},
	}
	for _, cl := range closures {
		c := command(a, basis, nil, &ProgressPayload{State: "ended", Reason: "Finite closure", Closure: &cl})
		ended, e := AdvanceAssessment(a, c, basis, actor, now, false)
		if e != nil || ended.Progress.State != "ended" || !SameAssessmentValue(ended.Review, a.Review) {
			t.Fatalf("closure %s: %v", cl.Kind, e)
		}
		c = command(ended, basis, nil, &ProgressPayload{State: "in_progress", Reason: "New follow-up warranted"})
		reopened, e := AdvanceAssessment(ended, c, basis, actor, now, false)
		if e != nil || reopened.Progress.Closure != nil || !SameAssessmentValue(reopened.Review, a.Review) {
			t.Fatal("reopen altered review or retained active duplicate", e)
		}
		if ended.Progress.Closure == nil {
			t.Fatal("reopen mutated historical closure")
		}
	}
	cl := Closure{Kind: "ruled_out", Explanation: "Review ruled this out"}
	c = command(a, basis, nil, &ProgressPayload{State: "ended", Reason: "Rule out", Closure: &cl})
	if _, err = AdvanceAssessment(a, c, basis, actor, now, false); err == nil {
		t.Fatal("ruled out without negative review")
	}
	negative := basis
	negative.Known.Evidence = append([]EvidenceChoice{}, basis.Known.Evidence...)
	negative.Selected.Evidence = append([]EvidenceChoice{}, basis.Selected.Evidence...)
	negative.Known.Evidence[0].Relation = "contradicts"
	negative.Selected.Evidence[0].Relation = "contradicts"
	negative, err = CanonicalReviewBasis(negative)
	if err != nil {
		t.Fatal(err)
	}
	c = command(a, negative, &ReviewPayload{Conclusion: "not_supported", Reason: "Counterevidence reviewed"}, nil)
	a, err = AdvanceAssessment(a, c, negative, actor, now, false)
	if err != nil {
		t.Fatal(err)
	}
	c = command(a, negative, nil, &ProgressPayload{State: "ended", Reason: "Rule out", Closure: &cl})
	if _, err = AdvanceAssessment(a, c, negative, actor, now, false); err != nil {
		t.Fatal(err)
	}
	c = command(a, basis, nil, &ProgressPayload{State: "ended", Reason: "Rule out on changed basis", Closure: &cl})
	if _, err = AdvanceAssessment(a, c, basis, actor, now, false); err == nil {
		t.Fatal("ruled out on unreviewed basis")
	}
}

func TestAssessmentStrictFiniteCommands(t *testing.T) {
	for _, raw := range []string{
		`{"operation":"record_review"}`, `null`,
		`{"operation":"record_review","operation_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","expected_version":0,"expected_case_version":1,"expected_basis_digest":"x","selection":{},"review":{"conclusion":"supported","reason":"r","actor":"forged"}}`,
	} {
		var c AssessmentCommand
		if json.Unmarshal([]byte(raw), &c) == nil {
			if _, err := NormalizeAssessmentCommand(c); err == nil {
				t.Fatal("malformed command accepted", raw)
			}
		}
	}
	b := assessmentBasisFixture(t)
	base := AssessmentCommand{Operation: "record_progress", OperationID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", ExpectedCaseVersion: 1, ExpectedBasisDigest: b.Digest, Selection: b.Selected}
	for _, cl := range []Closure{
		{Kind: "verified_remediation", Explanation: "No verification"}, {Kind: "ruled_out", Explanation: "r", RemainingUnknowns: "unexpected field"}, {Kind: "end_followup", Explanation: "No unknowns"}, {Kind: "duplicate", Explanation: "No target"}, {Kind: "accept_risk", Explanation: "Not a supported closure"},
	} {
		c := base
		c.Progress = &ProgressPayload{State: "ended", Reason: "r", Closure: &cl}
		if _, err := NormalizeAssessmentCommand(c); err == nil {
			t.Fatal("incomplete closure accepted", cl.Kind)
		}
	}
	oversized := b.Selected
	oversized.Evidence = make([]EvidenceChoice, MaxSelectedReferences+1)
	if _, err := NormalizeBasisSelection(oversized, MaxSelectedReferences); err == nil {
		t.Fatal("unbounded selection accepted")
	}
}
