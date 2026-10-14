package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

const ReviewBasisSchema = "xinsight.review-basis.v1"
const ResourceReviewBasisSchema = "xinsight.review-basis.v2"
const MaxSelectedReferences = 32
const MaxKnownReferences = 128

var assessmentCaseIDPattern = regexp.MustCompile(`^xc_[A-Za-z0-9_-]{22}$`)
var assessmentFindingIDPattern = regexp.MustCompile(`^xf_[A-Za-z0-9_-]{22}$`)

// Scope is either this Finding or an explicitly selected Case-level citation.
// Link metadata is checked against both the stored link and the source identity.
type EvidenceChoice struct {
	Scope           string `json:"scope"`
	EvidenceEventID string `json:"evidence_event_id"`
	Relation        string `json:"relation"`
	EventHash       string `json:"event_hash"`
	SourceRef       string `json:"source_ref"`
}
type GapChoice struct {
	Scope         string `json:"scope"`
	GapID         string `json:"gap_id"`
	Relation      string `json:"relation"`
	AffectedClaim string `json:"affected_claim"`
	GapType       string `json:"gap_type"`
}
type BasisSelection struct {
	Contents  []ContentChoice  `json:"contents,omitempty"`
	Resources []ResourceChoice `json:"resources,omitempty"`
	Evidence  []EvidenceChoice `json:"evidence"`
	Gaps      []GapChoice      `json:"gaps"`
}
type AdoptedGap struct {
	Reference          GapChoice  `json:"reference"`
	ResolutionRecorded bool       `json:"resolution_recorded"`
	ResolutionVersion  int64      `json:"resolution_version"`
	State              string     `json:"state"`
	Note               string     `json:"note"`
	UpdatedBySubject   string     `json:"updated_by_subject"`
	UpdatedAt          *time.Time `json:"updated_at"`
}
type ReviewBasis struct {
	ResolvedExecutionContext *ResolvedExecutionContext `json:"resolved_execution_context,omitempty"`
	ContentBasisVersion      string                    `json:"content_basis_version,omitempty"`
	AdoptedResources         []AdoptedResource         `json:"adopted_resources,omitempty"`
	Schema                   string                    `json:"schema"`
	Statement                string                    `json:"statement"`
	FindingVersion           int64                     `json:"finding_version"`
	Known                    BasisSelection            `json:"known"`
	Selected                 BasisSelection            `json:"selected"`
	AdoptedGaps              []AdoptedGap              `json:"adopted_gaps"`
	Digest                   string                    `json:"digest"`
}

func EvidenceChoiceKey(e EvidenceChoice) string {
	return e.Scope + ":" + e.EvidenceEventID + ":" + e.Relation
}
func GapChoiceKey(g GapChoice) string { return g.Scope + ":" + g.GapID + ":" + g.Relation }

func NormalizeBasisSelection(s BasisSelection, maximum int) (BasisSelection, error) {
	if len(s.Evidence)+len(s.Gaps)+len(s.Resources)+len(s.Contents) > maximum {
		return s, ErrInvalid
	}
	s.Evidence = append([]EvidenceChoice{}, s.Evidence...)
	s.Gaps = append([]GapChoice{}, s.Gaps...)
	seen := map[string]bool{}
	for _, e := range s.Evidence {
		if (e.Scope != "finding" && e.Scope != "case") || ValidateAddEvidence(AddEvidenceInput{EvidenceEventID: e.EvidenceEventID, Relation: e.Relation, EventHash: e.EventHash, SourceRef: e.SourceRef}) != nil || seen[EvidenceChoiceKey(e)] {
			return s, ErrInvalid
		}
		seen[EvidenceChoiceKey(e)] = true
	}
	seen = map[string]bool{}
	for _, g := range s.Gaps {
		if (g.Scope != "finding" && g.Scope != "case") || ValidateAddGap(AddGapInput{GapID: g.GapID, Relation: g.Relation, AffectedClaim: g.AffectedClaim, GapType: g.GapType}) != nil || seen[GapChoiceKey(g)] {
			return s, ErrInvalid
		}
		seen[GapChoiceKey(g)] = true
	}
	if err := normalizeContentChoices(&s); err != nil {
		return s, err
	}
	if err := normalizeResourceChoices(&s); err != nil {
		return s, err
	}
	sort.Slice(s.Evidence, func(i, j int) bool { return EvidenceChoiceKey(s.Evidence[i]) < EvidenceChoiceKey(s.Evidence[j]) })
	sort.Slice(s.Gaps, func(i, j int) bool { return GapChoiceKey(s.Gaps[i]) < GapChoiceKey(s.Gaps[j]) })
	return s, nil
}

// Digest includes the actual adopted Gap contents. No observation time or
// generated Case/Finding identity is included, so a creation preview is stable.
func CanonicalReviewBasis(b ReviewBasis) (ReviewBasis, error) {
	var err error
	if (b.Schema != ReviewBasisSchema && b.Schema != ResourceReviewBasisSchema && b.Schema != ContentReviewBasisSchema) || b.FindingVersion < 1 || !validAssessmentText(b.Statement, MaxFindingStatementBytes, true) {
		return b, ErrInvalid
	}
	b.Known, err = NormalizeBasisSelection(b.Known, MaxKnownReferences)
	if err != nil {
		return b, err
	}
	b.Selected, err = NormalizeBasisSelection(b.Selected, MaxSelectedReferences)
	if err != nil {
		return b, err
	}
	for _, e := range b.Known.Evidence {
		if e.Scope != "finding" {
			return b, ErrInvalid
		}
	}
	for _, g := range b.Known.Gaps {
		if g.Scope != "finding" {
			return b, ErrInvalid
		}
	}
	for _, e := range b.Selected.Evidence {
		if e.Scope == "finding" && !containsEvidence(b.Known.Evidence, e) {
			return b, ErrInvalid
		}
	}
	for _, g := range b.Selected.Gaps {
		if g.Scope == "finding" && !containsGap(b.Known.Gaps, g) {
			return b, ErrInvalid
		}
	}
	if err := canonicalContentBasis(&b); err != nil {
		return b, err
	}
	if err := canonicalResourceBasis(&b); err != nil {
		return b, err
	}
	b.AdoptedGaps = append([]AdoptedGap{}, b.AdoptedGaps...)
	sort.Slice(b.AdoptedGaps, func(i, j int) bool {
		return GapChoiceKey(b.AdoptedGaps[i].Reference) < GapChoiceKey(b.AdoptedGaps[j].Reference)
	})
	if len(b.AdoptedGaps) != len(b.Selected.Gaps) {
		return b, ErrInvalid
	}
	resolved := make(map[string]AdoptedGap)
	for i, g := range b.AdoptedGaps {
		if g.Reference != b.Selected.Gaps[i] {
			return b, ErrInvalid
		}
		if !g.ResolutionRecorded {
			if g.ResolutionVersion != 0 || g.State != "" || g.Note != "" || g.UpdatedBySubject != "" || g.UpdatedAt != nil {
				return b, ErrInvalid
			}
		} else {
			if g.ResolutionVersion < 1 || !subjectPattern.MatchString(g.UpdatedBySubject) || g.UpdatedAt == nil || g.UpdatedAt.IsZero() || ValidateResolveGap(ResolveGapInput{State: g.State, Note: g.Note}) != nil {
				return b, ErrInvalid
			}
			at := g.UpdatedAt.UTC().Truncate(time.Microsecond)
			b.AdoptedGaps[i].UpdatedAt = &at
		}
		sameGap := b.AdoptedGaps[i]
		sameGap.Reference.Scope, sameGap.Reference.Relation = "", ""
		if earlier, exists := resolved[g.Reference.GapID]; exists && !SameAssessmentValue(earlier, sameGap) {
			return b, ErrInvalid
		}
		resolved[g.Reference.GapID] = sameGap
	}
	if err := canonicalExecutionContext(&b); err != nil {
		return b, err
	}
	b.Digest = ""
	raw, err := json.Marshal(b)
	if err != nil {
		return b, err
	}
	h := sha256.Sum256(raw)
	b.Digest = "sha256:" + hex.EncodeToString(h[:])
	return b, nil
}
func ValidReviewBasis(b ReviewBasis) bool {
	normalized, err := CanonicalReviewBasis(b)
	return err == nil && SameAssessmentValue(normalized, b)
}
func SameAssessmentValue(a, b any) bool {
	x, e1 := json.Marshal(a)
	y, e2 := json.Marshal(b)
	return e1 == nil && e2 == nil && bytes.Equal(x, y)
}
func containsEvidence(es []EvidenceChoice, e EvidenceChoice) bool {
	for _, v := range es {
		if v == e {
			return true
		}
	}
	return false
}
func containsGap(gs []GapChoice, g GapChoice) bool {
	for _, v := range gs {
		if v == g {
			return true
		}
	}
	return false
}
func validAssessmentText(s string, max int, required bool) bool {
	return (!required || strings.TrimSpace(s) != "") && len(s) <= max && !strings.ContainsRune(s, 0)
}

type ReviewPayload struct {
	Conclusion      string `json:"conclusion"`
	Reason          string `json:"reason"`
	MissingEvidence string `json:"missing_evidence"`
}
type FindingReference struct {
	CaseID    string `json:"case_id"`
	FindingID string `json:"finding_id"`
}
type Closure struct {
	VerificationContents  []ContentChoice   `json:"verification_contents,omitempty"`
	VerificationResources []ResourceChoice  `json:"verification_resources,omitempty"`
	Kind                  string            `json:"kind"`
	Explanation           string            `json:"explanation"`
	Scope                 string            `json:"scope"`
	VerifiedAt            *time.Time        `json:"verified_at"`
	VerificationEvidence  []EvidenceChoice  `json:"verification_evidence"`
	Duplicate             *FindingReference `json:"duplicate"`
	RemainingUnknowns     string            `json:"remaining_unknowns"`
}
type ProgressPayload struct {
	State   string   `json:"state"`
	Reason  string   `json:"reason"`
	Closure *Closure `json:"closure"`
}
type ReviewRecord struct {
	ReviewPayload
	Basis          ReviewBasis `json:"basis"`
	ActorSubjectID string      `json:"actor_subject_id"`
	At             time.Time   `json:"at"`
}
type ProgressRecord struct {
	ProgressPayload
	Basis          ReviewBasis `json:"basis"`
	ActorSubjectID string      `json:"actor_subject_id"`
	At             time.Time   `json:"at"`
}
type Assessment struct {
	CaseID           string          `json:"case_id"`
	FindingID        string          `json:"finding_id"`
	Recorded         bool            `json:"recorded"`
	Version          int64           `json:"version"`
	Review           *ReviewRecord   `json:"review"`
	Progress         *ProgressRecord `json:"progress"`
	UpdatedAt        *time.Time      `json:"updated_at"`
	UpdatedBySubject string          `json:"updated_by_subject"`
}
type AssessmentCommand struct {
	Operation           string           `json:"operation"`
	OperationID         string           `json:"operation_id"`
	ExpectedVersion     int64            `json:"expected_version"`
	ExpectedCaseVersion int64            `json:"expected_case_version"`
	ExpectedBasisDigest string           `json:"expected_basis_digest"`
	Selection           BasisSelection   `json:"selection"`
	Review              *ReviewPayload   `json:"review"`
	Progress            *ProgressPayload `json:"progress"`
}

func (c *AssessmentCommand) UnmarshalJSON(raw []byte) error {
	type plain AssessmentCommand
	return decodeAssessmentFields(raw, (*plain)(c), "operation", "operation_id", "expected_version", "expected_case_version", "expected_basis_digest", "selection")
}
func decodeAssessmentFields(raw []byte, target any, required ...string) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ErrInvalid
	}
	for _, key := range required {
		v, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return ErrInvalid
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(target)
}
func NormalizeAssessmentCommand(c AssessmentCommand) (AssessmentCommand, error) {
	if !uuidPattern.MatchString(c.OperationID) || c.ExpectedVersion < 0 || c.ExpectedCaseVersion < 1 || !hashPattern.MatchString(c.ExpectedBasisDigest) {
		return c, ErrInvalid
	}
	var err error
	c.Selection, err = NormalizeBasisSelection(c.Selection, MaxSelectedReferences)
	if err != nil {
		return c, err
	}
	switch c.Operation {
	case "record_review":
		if c.Review == nil || c.Progress != nil {
			return c, ErrInvalid
		}
		r := *c.Review
		c.Review = &r
		r.Reason = strings.TrimSpace(r.Reason)
		r.MissingEvidence = strings.TrimSpace(r.MissingEvidence)
		c.Review = &r
		if !validAssessmentText(r.Reason, 4000, true) || !validAssessmentText(r.MissingEvidence, 4000, false) {
			return c, ErrInvalid
		}
		switch r.Conclusion {
		case "pending_review", "supported", "not_supported", "insufficient_evidence":
		default:
			return c, ErrInvalid
		}
	case "record_progress":
		if c.Progress == nil || c.Review != nil {
			return c, ErrInvalid
		}
		p := *c.Progress
		p.Reason = strings.TrimSpace(p.Reason)
		c.Progress = &p
		if !validAssessmentText(p.Reason, 4000, true) {
			return c, ErrInvalid
		}
		switch p.State {
		case "not_started", "in_progress":
			if p.Closure != nil {
				return c, ErrInvalid
			}
		case "ended":
			if p.Closure == nil {
				return c, ErrInvalid
			}
		default:
			return c, ErrInvalid
		}
		if p.Closure != nil {
			cl := *p.Closure
			c.Progress.Closure = &cl
			cl.Explanation = strings.TrimSpace(cl.Explanation)
			if !validAssessmentText(cl.Explanation, 4000, true) || !validAssessmentText(cl.Scope, 2000, false) || !validAssessmentText(cl.RemainingUnknowns, 4000, false) {
				return c, ErrInvalid
			}
			s, err := NormalizeBasisSelection(BasisSelection{Evidence: cl.VerificationEvidence, Resources: cl.VerificationResources, Contents: cl.VerificationContents}, MaxSelectedReferences)
			if err != nil {
				return c, err
			}
			cl.VerificationEvidence = s.Evidence
			cl.VerificationResources = s.Resources
			cl.VerificationContents = s.Contents
			if cl.VerifiedAt != nil {
				at := cl.VerifiedAt.UTC().Truncate(time.Microsecond)
				cl.VerifiedAt = &at
			}
			switch cl.Kind {
			case "verified_remediation":
				if !validAssessmentText(cl.Scope, 2000, true) || cl.VerifiedAt == nil || cl.VerifiedAt.IsZero() || (len(cl.VerificationEvidence)+len(cl.VerificationResources)+len(cl.VerificationContents)) == 0 || cl.Duplicate != nil || cl.RemainingUnknowns != "" {
					return c, ErrInvalid
				}
			case "ruled_out":
				if cl.Scope != "" || cl.VerifiedAt != nil || (len(cl.VerificationEvidence)+len(cl.VerificationResources)+len(cl.VerificationContents)) != 0 || cl.Duplicate != nil || cl.RemainingUnknowns != "" {
					return c, ErrInvalid
				}
			case "duplicate":
				if cl.Duplicate == nil || !validFindingReference(*cl.Duplicate) || cl.Scope != "" || cl.VerifiedAt != nil || (len(cl.VerificationEvidence)+len(cl.VerificationResources)+len(cl.VerificationContents)) != 0 || cl.RemainingUnknowns != "" {
					return c, ErrInvalid
				}
			case "end_followup":
				if !validAssessmentText(cl.RemainingUnknowns, 4000, true) || cl.Scope != "" || cl.VerifiedAt != nil || (len(cl.VerificationEvidence)+len(cl.VerificationResources)+len(cl.VerificationContents)) != 0 || cl.Duplicate != nil {
					return c, ErrInvalid
				}
			default:
				return c, ErrInvalid
			}
		}
	default:
		return c, ErrInvalid
	}
	return c, nil
}
func validFindingReference(r FindingReference) bool {
	return assessmentCaseIDPattern.MatchString(r.CaseID) && assessmentFindingIDPattern.MatchString(r.FindingID)
}

// AdvanceAssessment enforces human-state rules only. The store resolves source
// identities and duplicate visibility/cycles in the transaction before commit.
func AdvanceAssessment(previous Assessment, c AssessmentCommand, basis ReviewBasis, actor Actor, at time.Time, initial bool) (Assessment, error) {
	if actor.Kind != "human" || !subjectPattern.MatchString(actor.Ref) || at.IsZero() || !ValidReviewBasis(basis) || basis.Digest != c.ExpectedBasisDigest || !SameAssessmentValue(c.Selection, basis.Selected) || previous.Version != c.ExpectedVersion {
		return previous, ErrInvalid
	}
	current := previous
	current.Recorded = true
	current.Version++
	now := at.UTC().Truncate(time.Microsecond)
	current.UpdatedAt = &now
	current.UpdatedBySubject = actor.Ref
	if c.Operation == "record_review" {
		r := c.Review
		if r == nil {
			return previous, ErrInvalid
		}
		support, contradiction := false, false
		for _, e := range basis.Selected.Evidence {
			support = support || e.Relation == "supports"
			contradiction = contradiction || e.Relation == "contradicts"
		}
		for _, content := range basis.Selected.Contents {
			support = support || content.Relation == "supports"
			contradiction = contradiction || content.Relation == "contradicts"
		}
		for _, resource := range basis.Selected.Resources {
			support = support || resource.Relation == "supports"
			contradiction = contradiction || resource.Relation == "contradicts"
		}
		if (r.Conclusion == "supported" && !support) || (r.Conclusion == "not_supported" && !contradiction) || (r.Conclusion == "insufficient_evidence" && len(basis.Selected.Gaps) == 0 && r.MissingEvidence == "") {
			return previous, ErrInvalid
		}
		current.Review = &ReviewRecord{ReviewPayload: *r, Basis: basis, ActorSubjectID: actor.Ref, At: now}
		if initial {
			if previous.Recorded {
				return previous, ErrInvalid
			}
			current.Progress = &ProgressRecord{ProgressPayload: ProgressPayload{State: "not_started", Reason: r.Reason}, Basis: basis, ActorSubjectID: actor.Ref, At: now}
		}
	} else if c.Operation == "record_progress" && c.Progress != nil && !initial {
		p := *c.Progress
		if previous.Progress == nil {
			if p.State != "not_started" {
				return previous, ErrConflict
			}
		} else {
			old := previous.Progress.State
			if !((old == "not_started" && (p.State == "in_progress" || p.State == "ended")) || (old == "in_progress" && p.State == "ended") || (old == "ended" && p.State == "in_progress")) {
				return previous, ErrConflict
			}
		}
		if p.Closure != nil {
			switch p.Closure.Kind {
			case "ruled_out":
				if previous.Review == nil || previous.Review.Conclusion != "not_supported" || previous.Review.Basis.Digest != basis.Digest {
					return previous, ErrConflict
				}
			case "verified_remediation":
				if p.Closure.VerifiedAt.After(now) {
					return previous, ErrInvalid
				}
				for _, e := range p.Closure.VerificationEvidence {
					if !containsEvidence(basis.Selected.Evidence, e) {
						return previous, ErrInvalid
					}
				}
				for _, content := range p.Closure.VerificationContents {
					if !containsContent(basis.Selected.Contents, content) {
						return previous, ErrInvalid
					}
				}
				for _, resource := range p.Closure.VerificationResources {
					if !containsResource(basis.Selected.Resources, resource) {
						return previous, ErrInvalid
					}
				}
			case "duplicate":
				if p.Closure.Duplicate.CaseID == previous.CaseID && p.Closure.Duplicate.FindingID == previous.FindingID {
					return previous, ErrInvalid
				}
			}
		}
		current.Progress = &ProgressRecord{ProgressPayload: p, Basis: basis, ActorSubjectID: actor.Ref, At: now}
	} else {
		return previous, ErrInvalid
	}
	return current, nil
}

type AssessmentAction struct {
	OperationID    string            `json:"operation_id"`
	Operation      string            `json:"operation"`
	ActorSubjectID string            `json:"actor_subject_id"`
	At             time.Time         `json:"at"`
	CaseVersion    int64             `json:"case_version"`
	Initial        bool              `json:"initial"`
	Command        AssessmentCommand `json:"command"`
	Basis          ReviewBasis       `json:"basis"`
	Previous       Assessment        `json:"previous"`
	Current        Assessment        `json:"current"`
}
type AssessmentHistory struct {
	Items          []AssessmentAction `json:"items"`
	ThroughVersion int64              `json:"through_version"`
	NextBefore     *int64             `json:"next_before_version"`
}
type BasisEvaluation struct {
	ResourceUpdates []ResourceUpdate `json:"resource_updates,omitempty"`
	State           string           `json:"state"` // unrecorded, unchanged, changed, unavailable
	Current         *ReviewBasis     `json:"current"`
}
type AssessmentView struct {
	EvaluatedAt    time.Time               `json:"evaluated_at"`
	Assessment     Assessment              `json:"assessment"`
	CaseVersion    int64                   `json:"case_version"`
	CaseStatus     string                  `json:"case_status"`
	ReviewBasis    BasisEvaluation         `json:"review_basis"`
	ProgressBasis  BasisEvaluation         `json:"progress_basis"`
	Overdue        bool                    `json:"overdue"`
	History        AssessmentHistory       `json:"history"`
	Continuations  []ReviewCreationReceipt `json:"continuations"`
	AllowedActions []string                `json:"allowed_actions"`
}

type SealedFindingReference struct {
	FindingReference
	Revision int64  `json:"revision"`
	Digest   string `json:"digest"`
}
type ReviewCreationCommand struct {
	ExecutionContext    *ExecutionContext       `json:"execution_context,omitempty"`
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

func (c *ReviewCreationCommand) UnmarshalJSON(raw []byte) error {
	// Preserve legacy decoding, but never accept ambiguous spellings or duplicate
	// keys for the new authorization-bearing optional context.
	if err := strictExecutionContextField(raw); err != nil {
		return err
	}
	type plain ReviewCreationCommand
	return decodeAssessmentFields(raw, (*plain)(c), "operation", "operation_id", "case_id", "expected_case_version", "finding", "selection", "expected_basis_digest", "reason")
}
func NormalizeReviewCreation(c ReviewCreationCommand) (ReviewCreationCommand, error) {
	if !uuidPattern.MatchString(c.OperationID) || ValidateCreateFinding(c.Finding) != nil || !hashPattern.MatchString(c.ExpectedBasisDigest) {
		return c, ErrInvalid
	}
	if c.NewCase != nil {
		if c.CaseID != "" || c.ExpectedCaseVersion != 0 || ValidateCreateCase(*c.NewCase) != nil {
			return c, ErrInvalid
		}
	} else if !assessmentCaseIDPattern.MatchString(c.CaseID) || c.ExpectedCaseVersion < 1 {
		return c, ErrInvalid
	}
	c.Reason = strings.TrimSpace(c.Reason)
	if !validAssessmentText(c.Reason, 4000, true) {
		return c, ErrInvalid
	}
	var err error
	c.Selection, err = NormalizeBasisSelection(c.Selection, MaxSelectedReferences)
	if err != nil {
		return c, err
	}
	for _, e := range c.Selection.Evidence {
		if e.Scope != "finding" {
			return c, ErrInvalid
		}
	}
	for _, g := range c.Selection.Gaps {
		if g.Scope != "finding" {
			return c, ErrInvalid
		}
	}
	for _, content := range c.Selection.Contents {
		if content.Scope != "finding" {
			return c, ErrInvalid
		}
	}
	for _, resource := range c.Selection.Resources {
		if resource.Scope != "finding" {
			return c, ErrInvalid
		}
	}
	if c.ExecutionContext != nil {
		if c.Operation != "start_review" || len(c.Selection.Resources)+len(c.Selection.Contents) == 0 {
			return c, ErrInvalid
		}
		normalized, err := NormalizeExecutionContext(*c.ExecutionContext)
		if err != nil {
			return c, err
		}
		c.ExecutionContext = &normalized
	}
	switch c.Operation {
	case "start_review":
		if c.From != nil || c.Review == nil {
			return c, ErrInvalid
		}
		command, e := NormalizeAssessmentCommand(AssessmentCommand{Operation: "record_review", OperationID: c.OperationID, ExpectedCaseVersion: 1, ExpectedBasisDigest: c.ExpectedBasisDigest, Selection: c.Selection, Review: c.Review})
		if e != nil {
			return c, e
		}
		c.Review = command.Review
	case "continue_finding":
		if c.Review != nil || c.From == nil || !validFindingReference(c.From.FindingReference) || c.From.Revision < 1 || !hashPattern.MatchString(c.From.Digest) {
			return c, ErrInvalid
		}
	default:
		return c, ErrInvalid
	}
	return c, nil
}

type ReviewCreationReceipt struct {
	OperationID    string                `json:"operation_id"`
	Operation      string                `json:"operation"`
	ActorSubjectID string                `json:"actor_subject_id"`
	At             time.Time             `json:"at"`
	CaseID         string                `json:"case_id"`
	FindingID      string                `json:"finding_id"`
	CaseVersion    int64                 `json:"case_version"`
	Command        ReviewCreationCommand `json:"command"`
	Basis          ReviewBasis           `json:"basis"`
	Action         *AssessmentAction     `json:"action"`
}
type AssessmentSnapshot struct {
	Assessments []Assessment            `json:"assessments"`
	Actions     []AssessmentAction      `json:"actions"`
	Creations   []ReviewCreationReceipt `json:"creations"`
}
type AssessmentStore interface {
	GetFindingAssessment(context.Context, string, string, time.Time) (AssessmentView, error)
	GetAssessmentHistory(context.Context, string, string, int64, int64, int) (AssessmentHistory, error)
	PreviewFindingBasis(context.Context, string, string, BasisSelection) (ReviewBasis, error)
	PreviewReviewCreation(context.Context, ReviewCreationCommand) (ReviewBasis, error)
	ApplyFindingAssessment(context.Context, string, string, Actor, AssessmentCommand, time.Time) (AssessmentAction, error)
	CreateReview(context.Context, Actor, ReviewCreationCommand, time.Time, io.Reader) (ReviewCreationReceipt, error)
}
