package workspace

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/workbench"
)

// ExecutionContext names two separately authorized readings. ExecutionAsOf is
// bound to the cost reading cutoff; it is not the resource observation cutoff.
type ExecutionContext struct {
	WorkbenchReadingID string    `json:"workbench_reading_id"`
	ExecutionID        string    `json:"execution_id"`
	ExecutionAsOf      time.Time `json:"execution_as_of"`
	RuntimeReadingID   string    `json:"runtime_reading_id"`
	RuntimeRequestRef  string    `json:"runtime_request_ref"`
}

func (c *ExecutionContext) UnmarshalJSON(raw []byte) error {
	value, err := connector.DecodeStrictJSONObject(raw)
	if err != nil || len(value) != 5 {
		return ErrInvalid
	}
	for _, key := range []string{"workbench_reading_id", "execution_id", "execution_as_of", "runtime_reading_id", "runtime_request_ref"} {
		if _, ok := value[key]; !ok {
			return ErrInvalid
		}
	}
	type plain ExecutionContext
	return json.Unmarshal(raw, (*plain)(c))
}
func NormalizeExecutionContext(c ExecutionContext) (ExecutionContext, error) {
	if !workbench.ValidReadingID(c.WorkbenchReadingID) || !workbench.ValidRuntimeReadingID(c.RuntimeReadingID) || !workbench.ValidRuntimeRequestRef(c.RuntimeRequestRef) || !executionid.Valid(c.ExecutionID) || c.ExecutionAsOf.IsZero() {
		return c, ErrInvalid
	}
	// Do not silently round a client timestamp to a different stored reading.
	if !c.ExecutionAsOf.Equal(c.ExecutionAsOf.Truncate(time.Microsecond)) {
		return c, ErrInvalid
	}
	c.ExecutionAsOf = c.ExecutionAsOf.UTC()
	return c, nil
}

// This immutable resolution lives with the creation receipt and basis. It can
// be revalidated after the navigation readings have expired or been cleaned.
type ResolvedExecutionContext struct {
	Context                ExecutionContext        `json:"context"`
	RuntimeCutoff          time.Time               `json:"runtime_cutoff"`
	WorkbenchMembershipRef string                  `json:"workbench_membership_ref"`
	RuntimeMembershipRef   string                  `json:"runtime_membership_ref"`
	Target                 workbench.ResourceScope `json:"target"`
	RequestID              string                  `json:"request_id"`
	Resources              []ResourceChoice        `json:"resources"`
	Contents               []ContentChoice         `json:"contents,omitempty"`
}

func canonicalExecutionContext(b *ReviewBasis) error {
	if b.ResolvedExecutionContext == nil {
		return nil
	}
	v := *b.ResolvedExecutionContext
	c, err := NormalizeExecutionContext(v.Context)
	if err != nil || v.RuntimeCutoff.IsZero() || !v.RuntimeCutoff.Equal(v.RuntimeCutoff.Truncate(time.Microsecond)) || !hashPattern.MatchString(v.WorkbenchMembershipRef) || !hashPattern.MatchString(v.RuntimeMembershipRef) || len(v.Resources)+len(v.Contents) == 0 {
		return ErrInvalid
	}
	execution, err := executionid.New(v.Target.SourceSystemID, v.RequestID)
	if err != nil || execution != c.ExecutionID {
		return ErrInvalid
	}
	v.Context = c
	v.RuntimeCutoff = v.RuntimeCutoff.UTC()
	selection, err := NormalizeBasisSelection(BasisSelection{Resources: v.Resources, Contents: v.Contents}, MaxSelectedReferences)
	if err != nil {
		return err
	}
	v.Resources = append([]ResourceChoice{}, selection.Resources...)
	v.Contents = selection.Contents
	for _, resource := range v.Resources {
		if resource.Scope != "finding" || resource.Target != v.Target || resource.RequestID != v.RequestID {
			return ErrInvalid
		}
		found := false
		for _, known := range b.Known.Resources {
			if known == resource {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalid
		}
	}
	for _, content := range v.Contents {
		r := content.Reference
		if content.Scope != "finding" || r.SourceSystemID != v.Target.SourceSystemID || r.SourceInstance != v.Target.SourceInstance || r.OrgID != v.Target.OrgID || r.ProjectID != v.Target.ProjectID || r.RequestID != v.RequestID || !containsContent(b.Known.Contents, content) {
			return ErrInvalid
		}
	}
	b.ResolvedExecutionContext = &v
	return nil
}

// Old resource-only resolutions omit Contents and retain their original bytes
// and meaning. Every new creation resolves all selected content; a content-only
// resolution cannot omit its content because canonicalization requires a basis.
func ExecutionContextSelectionMatches(v *ResolvedExecutionContext, s BasisSelection) bool {
	if v == nil {
		return false
	}
	resources := len(s.Resources) == 0 && len(v.Resources) == 0 || SameAssessmentValue(s.Resources, v.Resources)
	return resources && (len(v.Contents) == 0 && len(v.Resources) > 0 || SameAssessmentValue(s.Contents, v.Contents))
}

// ValidateExecutionContextPack checks durable relationships, never live grants
// or ephemeral reading tables. Packs without the optional field are unchanged.
func ValidateExecutionContextPack(pack EvidencePack) error {
	if pack.Assessments == nil {
		return nil
	}
	contexts := map[string]*ResolvedExecutionContext{}
	for _, creation := range pack.Assessments.Creations {
		c := creation.Command.ExecutionContext
		resolved := creation.Basis.ResolvedExecutionContext
		if (c == nil) != (resolved == nil) {
			return ErrInvalid
		}
		if c == nil {
			continue
		}
		normalized, err := NormalizeReviewCreation(creation.Command)
		if err != nil || !SameAssessmentValue(normalized, creation.Command) || creation.Command.ExpectedBasisDigest != creation.Basis.Digest || creation.Action == nil || !SameAssessmentValue(creation.Action.Basis, creation.Basis) {
			return ErrInvalid
		}
		if !ValidReviewBasis(creation.Basis) || !SameAssessmentValue(*c, resolved.Context) || !ExecutionContextSelectionMatches(resolved, creation.Command.Selection) {
			return ErrInvalid
		}
		found := false
		for _, link := range pack.Executions {
			if link.ExecutionID == c.ExecutionID && link.AsOf.Equal(c.ExecutionAsOf) {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalid
		}
		for _, resource := range resolved.Resources {
			found = false
			for _, link := range pack.Resources {
				if link.FindingID == creation.FindingID && link.Reference == resource {
					found = true
					break
				}
			}
			if !found {
				return ErrInvalid
			}
		}
		for _, content := range resolved.Contents {
			found := false
			for _, link := range pack.Contents {
				if link.FindingID == creation.FindingID && SameAssessmentValue(link.Reference, content) {
					found = true
					break
				}
			}
			if !found {
				return ErrInvalid
			}
		}
		if _, exists := contexts[creation.FindingID]; exists {
			return ErrInvalid
		}
		contexts[creation.FindingID] = resolved
	}
	check := func(id string, b ReviewBasis) bool {
		return SameAssessmentValue(b.ResolvedExecutionContext, contexts[id])
	}
	checkAssessment := func(a Assessment) bool {
		return (a.Review == nil || check(a.FindingID, a.Review.Basis)) && (a.Progress == nil || check(a.FindingID, a.Progress.Basis))
	}
	for _, a := range pack.Assessments.Assessments {
		if !checkAssessment(a) {
			return ErrInvalid
		}
	}
	for _, creation := range pack.Assessments.Creations {
		if a := creation.Action; a != nil && (!check(creation.FindingID, a.Basis) || !checkAssessment(a.Previous) || !checkAssessment(a.Current)) {
			return ErrInvalid
		}
	}
	for _, a := range pack.Assessments.Actions {
		if !check(a.Current.FindingID, a.Basis) || !checkAssessment(a.Previous) || !checkAssessment(a.Current) {
			return ErrInvalid
		}
	}
	return nil
}

func strictExecutionContextField(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return ErrInvalid
	}
	seen := false
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return ErrInvalid
		}
		name, ok := key.(string)
		if !ok {
			return ErrInvalid
		}
		if strings.EqualFold(name, "execution_context") {
			if seen || name != "execution_context" {
				return ErrInvalid
			}
			seen = true
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return ErrInvalid
		}
	}
	return nil
}
