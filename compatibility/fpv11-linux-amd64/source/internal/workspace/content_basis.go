package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

const ContentReviewBasisSchema = "xinsight.review-basis.v3"
const ContentBasisVersion = "xinsight.review-content-basis.v1"
const ContentEvidencePackSchema = "xinsight.evidence-pack.v5"

// A new adoption needs request-local byte proof. Existing authorized receipts
// are resolved before this signal; it is never persisted as an operation result.
var ErrContentLocationProofRequired = errors.New("content location verification required")

type contentLocationProofKey struct{}
type contentLocationProof struct {
	subject string
	choices map[string]bool
}

// The API adds this only after verifying the actual saved representation in
// this request. It is not serializable, persisted, or accepted from clients.
func WithVerifiedContentLocations(ctx context.Context, subject string, choices []ContentChoice) context.Context {
	p := contentLocationProof{subject: subject, choices: map[string]bool{}}
	for _, c := range choices {
		if ValidContentChoice(c) {
			p.choices[ContentChoiceKey(c)] = true
		}
	}
	return context.WithValue(ctx, contentLocationProofKey{}, p)
}

func ContentLocationVerified(ctx context.Context, subject string, c ContentChoice) bool {
	p, ok := ctx.Value(contentLocationProofKey{}).(contentLocationProof)
	return ok && p.subject == subject && p.choices[ContentChoiceKey(c)]
}

// ContentChoice holds only an immutable reference and a location, never a
// payload, excerpt, arbitrary URL or expiring runtime reading identity.
type ContentChoice sourcefacts.ContentChoice

func (c *ContentChoice) UnmarshalJSON(raw []byte) error {
	v, err := sourcefacts.DecodeContentChoice(raw)
	if err != nil {
		return ErrInvalid
	}
	*c = ContentChoice(v)
	return nil
}

type ContentLink struct {
	CaseID         string        `json:"case_id"`
	FindingID      string        `json:"finding_id,omitempty"`
	Reference      ContentChoice `json:"reference"`
	AddedBySubject string        `json:"added_by_subject"`
	AddedAt        time.Time     `json:"added_at"`
}

func ContentChoiceKey(c ContentChoice) string {
	// The tuple includes the full frozen identity/representation, location and
	// relation. Encoding boundaries prevent collisions in opaque source values.
	raw, _ := json.Marshal([]any{c.Scope, c.Reference, c.Location, c.Relation})
	return string(raw)
}

func ValidContentChoice(c ContentChoice) bool {
	raw, err := json.Marshal(c)
	if err != nil {
		return false
	}
	_, err = sourcefacts.DecodeContentChoice(raw)
	return err == nil
}

func normalizeContentChoices(s *BasisSelection) error {
	if len(s.Contents) == 0 {
		s.Contents = nil
		return nil
	}
	contents := make([]ContentChoice, len(s.Contents))
	seen := make(map[string]bool)
	for i, choice := range s.Contents {
		raw, err := json.Marshal(choice)
		if err != nil || json.Unmarshal(raw, &contents[i]) != nil {
			return ErrInvalid
		}
		key := ContentChoiceKey(contents[i])
		if seen[key] {
			return ErrInvalid
		}
		seen[key] = true
	}
	sort.Slice(contents, func(i, j int) bool { return ContentChoiceKey(contents[i]) < ContentChoiceKey(contents[j]) })
	s.Contents = contents
	return nil
}

func containsContent(choices []ContentChoice, c ContentChoice) bool {
	key := ContentChoiceKey(c)
	for _, candidate := range choices {
		if ContentChoiceKey(candidate) == key {
			return true
		}
	}
	return false
}

func canonicalContentBasis(b *ReviewBasis) error {
	hasContent := len(b.Known.Contents)+len(b.Selected.Contents) > 0
	if !hasContent {
		if b.Schema == ContentReviewBasisSchema || b.ContentBasisVersion != "" {
			return ErrInvalid
		}
		return nil
	}
	if b.Schema != ContentReviewBasisSchema || b.ContentBasisVersion != ContentBasisVersion {
		return ErrInvalid
	}
	for _, c := range b.Known.Contents {
		if c.Scope != "finding" {
			return ErrInvalid
		}
	}
	for _, c := range b.Selected.Contents {
		if c.Scope == "finding" && !containsContent(b.Known.Contents, c) {
			return ErrInvalid
		}
	}
	return nil
}

func basisHasContent(b ReviewBasis) bool {
	return b.ContentBasisVersion != "" || len(b.Known.Contents)+len(b.Selected.Contents) > 0
}

func assessmentHasContent(a Assessment) bool {
	return a.Review != nil && basisHasContent(a.Review.Basis) || a.Progress != nil && (basisHasContent(a.Progress.Basis) || a.Progress.Closure != nil && len(a.Progress.Closure.VerificationContents) > 0)
}

func actionHasContent(a AssessmentAction) bool {
	return basisHasContent(a.Basis) || len(a.Command.Selection.Contents) > 0 || a.Command.Progress != nil && a.Command.Progress.Closure != nil && len(a.Command.Progress.Closure.VerificationContents) > 0 || assessmentHasContent(a.Previous) || assessmentHasContent(a.Current)
}

func packHasContent(pack EvidencePack) bool {
	if len(pack.Contents) > 0 {
		return true
	}
	if pack.Assessments == nil {
		return false
	}
	for _, a := range pack.Assessments.Assessments {
		if assessmentHasContent(a) {
			return true
		}
	}
	for _, a := range pack.Assessments.Actions {
		if actionHasContent(a) {
			return true
		}
	}
	for _, c := range pack.Assessments.Creations {
		if basisHasContent(c.Basis) || len(c.Command.Selection.Contents) > 0 || c.Action != nil && actionHasContent(*c.Action) {
			return true
		}
	}
	return false
}

func validatePackContentLinks(pack EvidencePack) error {
	seen := make(map[string]bool)
	linkKey := func(finding string, c ContentChoice) string {
		raw, _ := json.Marshal([]any{pack.CaseID, finding, ContentChoiceKey(c)})
		return string(raw)
	}
	for _, link := range pack.Contents {
		if link.CaseID != pack.CaseID || !ValidContentChoice(link.Reference) || !subjectPattern.MatchString(link.AddedBySubject) || link.AddedAt.IsZero() || link.AddedAt.After(pack.SealedAt) || (link.FindingID == "") != (link.Reference.Scope == "case") {
			return ErrInvalid
		}
		if link.FindingID != "" {
			found := false
			for _, f := range pack.Findings {
				if f.ID == link.FindingID {
					found = true
					break
				}
			}
			if !found {
				return ErrInvalid
			}
		}
		key := linkKey(link.FindingID, link.Reference)
		if seen[key] {
			return ErrInvalid
		}
		seen[key] = true
	}
	if !packHasContent(pack) || pack.Assessments == nil {
		return nil
	}
	checkChoices := func(finding string, choices []ContentChoice) bool {
		for _, c := range choices {
			f := finding
			if c.Scope == "case" {
				f = ""
			}
			if !ValidContentChoice(c) || !seen[linkKey(f, c)] {
				return false
			}
		}
		return true
	}
	checkBasis := func(finding string, b ReviewBasis) bool {
		return !basisHasContent(b) || ValidReviewBasis(b) && checkChoices(finding, b.Known.Contents) && checkChoices(finding, b.Selected.Contents)
	}
	checkAssessment := func(a Assessment) bool {
		if a.Review != nil && !checkBasis(a.FindingID, a.Review.Basis) {
			return false
		}
		if a.Progress != nil {
			if !checkBasis(a.FindingID, a.Progress.Basis) {
				return false
			}
			if a.Progress.Closure != nil && !checkChoices(a.FindingID, a.Progress.Closure.VerificationContents) {
				return false
			}
		}
		return true
	}
	checkAction := func(a AssessmentAction) bool {
		if !checkBasis(a.Current.FindingID, a.Basis) || !checkChoices(a.Current.FindingID, a.Command.Selection.Contents) || !checkAssessment(a.Previous) || !checkAssessment(a.Current) {
			return false
		}
		return a.Command.Progress == nil || a.Command.Progress.Closure == nil || checkChoices(a.Current.FindingID, a.Command.Progress.Closure.VerificationContents)
	}
	for _, a := range pack.Assessments.Assessments {
		if !checkAssessment(a) {
			return ErrInvalid
		}
	}
	for _, a := range pack.Assessments.Actions {
		if !checkAction(a) {
			return ErrInvalid
		}
	}
	for _, c := range pack.Assessments.Creations {
		if !checkBasis(c.FindingID, c.Basis) || !checkChoices(c.FindingID, c.Command.Selection.Contents) || c.Action != nil && !checkAction(*c.Action) {
			return ErrInvalid
		}
	}
	return nil
}
