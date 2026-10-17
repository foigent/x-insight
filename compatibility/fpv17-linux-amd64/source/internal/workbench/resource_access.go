package workbench

import (
	"bytes"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/connector"
)

// This internal domain contract is deliberately limited to qualification data.
// It is not an I-03 producer profile or an enabled production import format.
const ResourceAccessVersion = "xinsight.resource-access.internal.v1"
const RuntimeResourcesVersion = "xinsight.runtime.resources.v1"

const MaxResourceRecordBytes = 16 << 10

type RuntimeResourceItem struct {
	Record              ResourceAccessRecord `json:"record"`
	RecordHash          string               `json:"record_hash"`
	ChainState          string               `json:"chain_state"`
	InputState          string               `json:"input_state"`
	ContentVerification string               `json:"content_verification"`
	InputEvidenceBasis  string               `json:"input_evidence_basis"`
	ChangedSinceReading bool                 `json:"changed_since_reading"`
	CapturedInputRefs   []string             `json:"captured_input_evidence_refs"`
}

type RuntimeResources struct {
	Version       string                `json:"resources_version"`
	ReadingID     string                `json:"reading_id"`
	RequestRef    string                `json:"request_ref"`
	MembershipRef string                `json:"membership_ref"`
	Coverage      string                `json:"coverage"`
	Items         []RuntimeResourceItem `json:"items"`
	NextCursor    string                `json:"next_cursor,omitempty"`
}

type ResourceScope struct {
	SourceSystemID string `json:"source_system_id"`
	SourceInstance string `json:"source_instance"`
	OrgID          string `json:"org_id"`
	ProjectID      string `json:"project_id"`
}

type ResourceObject struct {
	Ref               string `json:"ref"`
	Version           string `json:"version"`
	Name              string `json:"name"`
	Location          string `json:"location"`
	Ownership         string `json:"ownership"`
	OwnerRef          string `json:"owner_ref,omitempty"`
	Classification    string `json:"classification"`
	ClassificationRef string `json:"classification_ref,omitempty"`
	DeviceManagement  string `json:"device_management"`
}

type ResourceTransform struct {
	Kind          string `json:"kind"`
	InputVersion  string `json:"input_version"`
	OutputVersion string `json:"output_version"`
	ReceiptRef    string `json:"receipt_ref"`
}

type ResourceInputClaim struct {
	FragmentRef      string              `json:"fragment_ref"`
	FragmentVersion  string              `json:"fragment_version"`
	AttemptID        string              `json:"attempt_id"`
	AttemptKind      string              `json:"attempt_kind"`
	ReportedInputRef string              `json:"reported_input_ref"`
	Transforms       []ResourceTransform `json:"transforms"`
}

type ResourceAccessRecord struct {
	Version        string              `json:"schema_version"`
	Synthetic      bool                `json:"synthetic"`
	RecordRef      string              `json:"record_ref"`
	Revision       int64               `json:"revision"`
	PreviousHash   string              `json:"previous_hash,omitempty"`
	Reporter       ResourceScope       `json:"reporter"`
	Target         ResourceScope       `json:"target"`
	RequestID      string              `json:"request_id"`
	ApplicationRef string              `json:"application_ref"`
	ActorRef       string              `json:"actor_ref"`
	OccurredAt     time.Time           `json:"occurred_at"`
	ReportKind     string              `json:"report_kind"`
	Resource       ResourceObject      `json:"resource"`
	Action         string              `json:"action"`
	AccessOutcome  string              `json:"access_outcome"`
	AccessDecision string              `json:"access_decision"`
	DecisionRef    string              `json:"decision_ref,omitempty"`
	PolicyVersion  string              `json:"policy_version,omitempty"`
	Input          *ResourceInputClaim `json:"input,omitempty"`
	Withdrawn      bool                `json:"withdrawn"`
}

var resourceUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var resourceRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]{0,255}$`)
var resourceHash = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func resourceOneOf(value string, choices ...string) bool {
	for _, candidate := range choices {
		if candidate == value {
			return true
		}
	}
	return false
}

func validResourceScope(s ResourceScope) bool {
	return resourceUUID.MatchString(s.SourceSystemID) && resourceRef.MatchString(s.SourceInstance) && resourceRef.MatchString(s.OrgID) && resourceRef.MatchString(s.ProjectID)
}

// DecodeResourceAccess rejects unknown/duplicate fields, including raw-content
// fields. Bounded names still require resource grants; this is not content DLP.
func DecodeResourceAccess(raw []byte) (ResourceAccessRecord, error) {
	var r ResourceAccessRecord
	if len(raw) == 0 || len(raw) > MaxResourceRecordBytes {
		return r, ErrInvalid
	}
	if _, err := connector.DecodeStrictJSONObject(raw); err != nil {
		return r, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil {
		return ResourceAccessRecord{}, ErrInvalid
	}
	if err := r.Validate(); err != nil {
		return ResourceAccessRecord{}, err
	}
	return r, nil
}

// DecodeResourceAccessForImport applies exact field spelling to new writes.
// Historical stored records keep the original decoder so a spelling accepted
// by an older writer cannot make an otherwise valid backup unrestorable.
func DecodeResourceAccessForImport(raw []byte) (ResourceAccessRecord, error) {
	if len(raw) == 0 || len(raw) > MaxResourceRecordBytes {
		return ResourceAccessRecord{}, ErrInvalid
	}
	value, err := connector.DecodeStrictJSONObject(raw)
	if err != nil || !resourceExactFields(value, reflect.TypeOf(ResourceAccessRecord{})) {
		return ResourceAccessRecord{}, ErrInvalid
	}
	return DecodeResourceAccess(raw)
}

// encoding/json accepts case-insensitive field aliases. Reject them before
// typed decoding, including nested scopes, input claims and transforms. Keep
// explicit required fields in the new-import contract. Historical decoding is separate.
func resourceExactFields(value any, typ reflect.Type) bool {
	if value == nil {
		return false
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return true
	}
	switch typ.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				fields[name] = f.Type
				if !strings.Contains(f.Tag.Get("json"), ",omitempty") {
					if _, exists := object[name]; !exists {
						return false
					}
				}
			}
		}
		for name, child := range object {
			ft, ok := fields[name]
			if !ok || !resourceExactFields(child, ft) {
				return false
			}
		}
	case reflect.Slice:
		values, ok := value.([]any)
		if !ok {
			return false
		}
		for _, child := range values {
			if !resourceExactFields(child, typ.Elem()) {
				return false
			}
		}
	}
	return true
}

func (r ResourceAccessRecord) Validate() error {
	if r.Version != ResourceAccessVersion || !r.Synthetic {
		return ErrUnsupportedVersion
	}
	if !validResourceScope(r.Reporter) || !validResourceScope(r.Target) || r.Revision < 1 || r.Revision > 1_000_000 ||
		(r.Revision == 1 && r.PreviousHash != "") || (r.Revision > 1 && !resourceHash.MatchString(r.PreviousHash)) || r.OccurredAt.IsZero() {
		return ErrInvalid
	}
	for _, value := range []string{r.RecordRef, r.RequestID, r.ApplicationRef, r.ActorRef, r.Resource.Ref, r.Resource.Version} {
		if !resourceRef.MatchString(value) {
			return ErrInvalid
		}
	}
	object := r.Resource
	if !utf8.ValidString(object.Name) || strings.TrimSpace(object.Name) == "" || utf8.RuneCountInString(object.Name) > 160 || strings.IndexFunc(object.Name, unicode.IsControl) >= 0 ||
		!resourceOneOf(object.Location, "enterprise_service", "pc") || !resourceOneOf(object.Ownership, "known", "unknown") ||
		!resourceOneOf(object.Classification, "known", "unknown") || !resourceOneOf(object.DeviceManagement, "managed", "unmanaged", "unknown") {
		return ErrInvalid
	}
	for _, pair := range [][2]string{{object.Ownership, object.OwnerRef}, {object.Classification, object.ClassificationRef}} {
		if (pair[0] == "known" && !resourceRef.MatchString(pair[1])) || (pair[0] == "unknown" && pair[1] != "") {
			return ErrInvalid
		}
	}
	if !resourceOneOf(r.ReportKind, "source_audit", "client_assertion") || !resourceOneOf(r.Action, "read", "search", "download") ||
		!resourceOneOf(r.AccessOutcome, "read", "denied", "failed", "unknown") || !resourceOneOf(r.AccessDecision, "allow", "deny", "unknown") {
		return ErrInvalid
	}
	if r.AccessDecision == "unknown" {
		if r.DecisionRef != "" || r.PolicyVersion != "" {
			return ErrInvalid
		}
	} else if !resourceRef.MatchString(r.DecisionRef) || !resourceRef.MatchString(r.PolicyVersion) {
		return ErrInvalid
	}
	if r.Input != nil {
		if !resourceOneOf(r.Input.AttemptKind, "provider_attempt", "media_attempt") {
			return ErrInvalid
		}
		for _, value := range []string{r.Input.FragmentRef, r.Input.FragmentVersion, r.Input.AttemptID, r.Input.ReportedInputRef} {
			if !resourceRef.MatchString(value) {
				return ErrInvalid
			}
		}
		if r.Input.Transforms == nil || len(r.Input.Transforms) > 8 {
			return ErrInvalid
		}
		prior := r.Input.FragmentVersion
		for _, step := range r.Input.Transforms {
			if !resourceOneOf(step.Kind, "extract", "summarize", "redact", "translate", "truncate") || step.InputVersion != prior ||
				!resourceRef.MatchString(step.OutputVersion) || !resourceRef.MatchString(step.ReceiptRef) {
				return ErrInvalid
			}
			prior = step.OutputVersion
		}
	}
	return nil
}

// Matching opaque references demonstrates agreement between the two supplied
// records. It does not independently verify content, redaction, or dispatch.
func ResourceInputState(record ResourceAccessRecord, capturedInputRef string) string {
	if record.Withdrawn {
		return "withdrawn"
	}
	if record.Input == nil || capturedInputRef == "" || record.AccessOutcome != "read" {
		return "not_confirmed"
	}
	if record.Input.ReportedInputRef != capturedInputRef {
		return "reference_mismatch"
	}
	return "reference_matched"
}
