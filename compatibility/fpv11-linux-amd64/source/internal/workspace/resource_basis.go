package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/workbench"
)

// A citation names one immutable revision. It never points to an expiring
// reading or silently follows a later revision of the same resource report.
type ResourceChoice struct {
	Scope      string                  `json:"scope"`
	Reporter   workbench.ResourceScope `json:"reporter"`
	Target     workbench.ResourceScope `json:"target"`
	RequestID  string                  `json:"request_id"`
	RecordRef  string                  `json:"record_ref"`
	Revision   int64                   `json:"revision"`
	RecordHash string                  `json:"record_hash"`
	Relation   string                  `json:"relation"`
}

// Newly introduced citations use exact field names and reject duplicate keys,
// including nested identities; older Evidence/Gap serialization is untouched.
func (r *ResourceChoice) UnmarshalJSON(raw []byte) error {
	value, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return ErrInvalid
	}
	keys := []string{"scope", "reporter", "target", "request_id", "record_ref", "revision", "record_hash", "relation"}
	if len(value) != len(keys) {
		return ErrInvalid
	}
	for _, k := range keys {
		if _, ok := value[k]; !ok {
			return ErrInvalid
		}
	}
	for _, name := range []string{"reporter", "target"} {
		scope, ok := value[name].(map[string]any)
		if !ok || len(scope) != 4 {
			return ErrInvalid
		}
		for _, k := range []string{"source_system_id", "source_instance", "org_id", "project_id"} {
			if _, ok := scope[k]; !ok {
				return ErrInvalid
			}
		}
	}
	type plain ResourceChoice
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode((*plain)(r)); err != nil {
		return ErrInvalid
	}
	return nil
}

type AdoptedResource struct {
	Reference ResourceChoice     `json:"reference"`
	Record    ResourceRecordJSON `json:"record"`
}

// Keep the accepted JSON representation: omitempty and time.Time marshaling
// must never rewrite a source record before its original hash is checked.
type ResourceRecordJSON []byte

func (r ResourceRecordJSON) MarshalJSON() ([]byte, error) {
	v, err := connector.DecodeStrictJSONObject(r)
	if err != nil {
		return nil, err
	}
	return connector.CanonicalizeJSONValue(v)
}
func (r *ResourceRecordJSON) UnmarshalJSON(raw []byte) error {
	v, err := ResourceRecordJSON(raw).MarshalJSON()
	if err == nil {
		*r = v
	}
	return err
}
func (r AdoptedResource) DecodedRecord() workbench.ResourceAccessRecord {
	v, _ := workbench.DecodeResourceAccess(r.Record)
	return v
}

var resourceChoiceRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]{0,255}$`)
var resourceChoiceUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

func ResourceChoiceKey(r ResourceChoice) string {
	// JSON preserves component boundaries, including colons in record refs.
	b, _ := json.Marshal(struct {
		Scope    string
		Reporter workbench.ResourceScope
		Ref      string
		Revision int64
		Relation string
	}{r.Scope, r.Reporter, r.RecordRef, r.Revision, r.Relation})
	return string(b)
}

func ValidResourceChoice(r ResourceChoice) bool {
	if (r.Scope != "finding" && r.Scope != "case") || (r.Relation != "supports" && r.Relation != "contradicts" && r.Relation != "context") || r.Revision < 1 || r.Revision > 1000000 || !hashPattern.MatchString(r.RecordHash) || !resourceChoiceRef.MatchString(r.RecordRef) || !resourceChoiceRef.MatchString(r.RequestID) {
		return false
	}
	for _, s := range []workbench.ResourceScope{r.Reporter, r.Target} {
		if !resourceChoiceUUID.MatchString(s.SourceSystemID) || !resourceChoiceRef.MatchString(s.SourceInstance) || !resourceChoiceRef.MatchString(s.OrgID) || !resourceChoiceRef.MatchString(s.ProjectID) {
			return false
		}
	}
	return true
}

func normalizeResourceChoices(s *BasisSelection) error {
	if len(s.Resources) == 0 {
		s.Resources = nil
		return nil
	}
	s.Resources = append([]ResourceChoice{}, s.Resources...)
	seen := map[string]bool{}
	for _, r := range s.Resources {
		k := ResourceChoiceKey(r)
		if !ValidResourceChoice(r) || seen[k] {
			return ErrInvalid
		}
		seen[k] = true
	}
	sort.Slice(s.Resources, func(i, j int) bool { return ResourceChoiceKey(s.Resources[i]) < ResourceChoiceKey(s.Resources[j]) })
	return nil
}

func containsResource(rs []ResourceChoice, r ResourceChoice) bool {
	for _, v := range rs {
		if v == r {
			return true
		}
	}
	return false
}

func ResourceMatchesRecord(r ResourceChoice, raw []byte) bool {
	record, err := workbench.DecodeResourceAccess(raw)
	if err != nil || r.Reporter != record.Reporter || r.Target != record.Target || r.RequestID != record.RequestID || r.RecordRef != record.RecordRef || r.Revision != record.Revision {
		return false
	}
	canonical, err := ResourceRecordJSON(raw).MarshalJSON()
	if err != nil {
		return false
	}
	h := sha256.Sum256(canonical)
	return r.RecordHash == "sha256:"+hex.EncodeToString(h[:])
}

func canonicalResourceBasis(b *ReviewBasis) error {
	hasResources := len(b.Known.Resources)+len(b.Selected.Resources) > 0
	if b.Schema != ContentReviewBasisSchema && (b.Schema == ResourceReviewBasisSchema) != hasResources {
		return ErrInvalid
	}
	for _, r := range b.Known.Resources {
		if r.Scope != "finding" {
			return ErrInvalid
		}
	}
	for _, r := range b.Selected.Resources {
		if r.Scope == "finding" && !containsResource(b.Known.Resources, r) {
			return ErrInvalid
		}
	}
	if len(b.AdoptedResources) != len(b.Selected.Resources) {
		return ErrInvalid
	}
	if !hasResources {
		b.AdoptedResources = nil
		return nil
	}
	b.AdoptedResources = append([]AdoptedResource{}, b.AdoptedResources...)
	sort.Slice(b.AdoptedResources, func(i, j int) bool {
		return ResourceChoiceKey(b.AdoptedResources[i].Reference) < ResourceChoiceKey(b.AdoptedResources[j].Reference)
	})
	for i, r := range b.AdoptedResources {
		if r.Reference != b.Selected.Resources[i] || !ResourceMatchesRecord(r.Reference, r.Record) {
			return ErrInvalid
		}
	}
	return nil
}

// Current resource signals are outside the immutable adopted basis and pack.
type ResourceUpdate struct {
	Reference      ResourceChoice `json:"reference"`
	State          string         `json:"state"` // revised, withdrawn, unavailable
	LatestRevision int64          `json:"latest_revision,omitempty"`
	LatestHash     string         `json:"latest_hash,omitempty"`
}

func ApplyResourceUpdates(v BasisEvaluation, updates map[string]ResourceUpdate, selected []ResourceChoice) BasisEvaluation {
	if v.State == "unrecorded" || v.State == "unavailable" {
		return v
	}
	for _, r := range selected {
		if u, ok := updates[ResourceChoiceKey(r)]; ok && u.Reference == r {
			v.ResourceUpdates = append(v.ResourceUpdates, u)
			if u.State == "unavailable" {
				v.State = "unavailable"
				v.Current = nil
			} else if v.State != "unavailable" {
				v.State = "changed"
			}
		}
	}
	return v
}
