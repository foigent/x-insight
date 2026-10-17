package workevent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/foigent/x-insight/internal/connector"
)

const EventVersion = "xinsight.work-event.v1"
const RelationVersion = "xinsight.work-event-relation.v1"
const CollectionProfile = "xinsight.work-event.v1"
const MaxEventBytes = 8192

var ErrInvalid = errors.New("invalid work event")
var ErrConflict = errors.New("conflicting work event")
var ErrScopeChanged = errors.New("work event source scope changed")
var ErrUnavailable = errors.New("work event source unavailable")

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var actionPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var positivePattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
var urRequestPattern = regexp.MustCompile(`^op_[a-f0-9]{32}$`)

type Scope struct {
	SourceSystemID string `json:"source_system_id"`
	SourceInstance string `json:"source_instance"`
	OrgID          string `json:"org_id"`
	ProjectID      string `json:"project_id"`
}

func (s Scope) Valid() bool {
	return uuidPattern.MatchString(s.SourceSystemID) && refPattern.MatchString(s.SourceInstance) &&
		positivePattern.MatchString(s.OrgID) && positivePattern.MatchString(s.ProjectID)
}

type Actor struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref,omitempty"`
}

type Resource struct {
	Ref       string `json:"ref"`
	NameState string `json:"name_state"`
	Name      string `json:"name,omitempty"`
}

type ContentRef struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	SHA256 string `json:"sha256"`
}

// Event is only typed metadata and source-reported content references.
// It never contains tool input/output bytes, SQL values or a body blob.
// A reference is not proof that XI holds a human-review copy or that a
// separate execution-temporary copy exists or is available for replay.
// A readable resource name may be a path-like locator within an authorized scope.
type Event struct {
	SchemaVersion  string       `json:"schema_version"`
	Reporter       Scope        `json:"reporter"`
	EventRef       string       `json:"event_ref"`
	Revision       int64        `json:"revision"`
	PreviousHash   string       `json:"previous_hash,omitempty"`
	OccurredAt     time.Time    `json:"occurred_at"`
	RecordedAt     time.Time    `json:"recorded_at"`
	SubjectKind    string       `json:"subject_kind"`
	LifecycleState string       `json:"lifecycle_state"`
	TaskRef        string       `json:"task_ref,omitempty"`
	RunID          string       `json:"run_id,omitempty"`
	ModelCallID    string       `json:"model_call_id,omitempty"`
	ToolCallID     string       `json:"tool_call_id,omitempty"`
	Actor          Actor        `json:"actor"`
	ActionKind     string       `json:"action_kind"`
	Resource       *Resource    `json:"resource,omitempty"`
	ContentState   string       `json:"content_state"`
	ContentRefs    []ContentRef `json:"content_refs"`
}

// RelationAssertion remains a source assertion. XI must independently match
// the exact UR request before the UI may describe an identifier match.
type RelationAssertion struct {
	SchemaVersion string    `json:"schema_version"`
	Reporter      Scope     `json:"reporter"`
	RelationRef   string    `json:"relation_ref"`
	EventRef      string    `json:"event_ref"`
	EventRevision int64     `json:"event_revision"`
	EventHash     string    `json:"event_hash"`
	URTarget      Scope     `json:"ur_target"`
	URRequestID   string    `json:"ur_request_id"`
	ModelCallID   string    `json:"model_call_id"`
	EvidenceRef   string    `json:"evidence_ref"`
	EvidenceHash  string    `json:"evidence_sha256"`
	RecordedAt    time.Time `json:"recorded_at"`
}

func cleanText(s string, max int) bool {
	if s == "" || len(s) > max || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validTime(t time.Time) bool {
	return !t.IsZero() && t.Location() == time.UTC && t.Year() >= 2020 && t.Year() <= 9999
}

func (e Event) Validate() error {
	if e.SchemaVersion != EventVersion || !e.Reporter.Valid() || !refPattern.MatchString(e.EventRef) ||
		e.Revision < 1 || e.Revision > 1000000 || (e.Revision == 1 && e.PreviousHash != "") ||
		(e.Revision > 1 && !digestPattern.MatchString(e.PreviousHash)) || !validTime(e.OccurredAt) ||
		!validTime(e.RecordedAt) || !oneOf(e.SubjectKind, "task", "message", "model_call", "tool_call", "resource_action") ||
		!oneOf(e.LifecycleState, "started", "completed", "failed", "unknown") ||
		!optionalRef(e.TaskRef) || !optionalRef(e.RunID) || !optionalRef(e.ModelCallID) || !optionalRef(e.ToolCallID) ||
		(e.SubjectKind == "task" && e.TaskRef == "") ||
		(e.SubjectKind == "model_call" && e.ModelCallID == "" && !oneOf(e.ActionKind, "model.input_observed", "model.output_observed")) ||
		(e.SubjectKind == "message" && (e.TaskRef != "" || e.ModelCallID != "" || e.ToolCallID != "")) ||
		(e.SubjectKind == "tool_call" && e.ToolCallID == "") || (e.SubjectKind == "resource_action" && e.Resource == nil) ||
		!actionPattern.MatchString(e.ActionKind) ||
		!oneOf(e.Actor.Kind, "human", "agent", "service", "unknown") ||
		(e.Actor.Kind == "unknown" && e.Actor.Ref != "") ||
		(e.Actor.Kind != "unknown" && !refPattern.MatchString(e.Actor.Ref)) ||
		!oneOf(e.ContentState, "not_captured", "pending", "referenced") ||
		len(e.ContentRefs) > 8 || (e.ContentState == "referenced") != (len(e.ContentRefs) > 0) {
		return ErrInvalid
	}
	prefix := map[string]string{"task": "task.", "message": "message.", "model_call": "model.", "tool_call": "tool.", "resource_action": "resource."}[e.SubjectKind]
	if !strings.HasPrefix(e.ActionKind, prefix) || len(e.ActionKind) == len(prefix) || !actionLifecycleConsistent(e.ActionKind, e.LifecycleState) {
		return ErrInvalid
	}
	if e.Resource != nil && (!refPattern.MatchString(e.Resource.Ref) || !oneOf(e.Resource.NameState, "readable", "hidden") ||
		(e.Resource.NameState == "hidden" && e.Resource.Name != "") ||
		(e.Resource.NameState == "readable" && !cleanText(e.Resource.Name, 512))) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, ref := range e.ContentRefs {
		if !oneOf(ref.Kind, "user_input", "model_input", "model_output", "tool_input", "tool_output", "user_output", "resource_before", "resource_after") ||
			!contentKindForSubject(e.SubjectKind, ref.Kind) || !refPattern.MatchString(ref.Ref) ||
			!digestPattern.MatchString(ref.SHA256) || seen[ref.Kind+"\x00"+ref.Ref] {
			return ErrInvalid
		}
		seen[ref.Kind+"\x00"+ref.Ref] = true
	}
	return nil
}

func (r RelationAssertion) Validate() error {
	if r.SchemaVersion != RelationVersion || !r.Reporter.Valid() || !r.URTarget.Valid() ||
		!refPattern.MatchString(r.RelationRef) || !refPattern.MatchString(r.EventRef) ||
		r.EventRevision < 1 || r.EventRevision > 1000000 || !digestPattern.MatchString(r.EventHash) ||
		!urRequestPattern.MatchString(r.URRequestID) ||
		!refPattern.MatchString(r.ModelCallID) || !refPattern.MatchString(r.EvidenceRef) ||
		!digestPattern.MatchString(r.EvidenceHash) || !validTime(r.RecordedAt) {
		return ErrInvalid
	}
	return nil
}

func optionalRef(value string) bool { return value == "" || refPattern.MatchString(value) }

func actionLifecycleConsistent(action, state string) bool {
	for _, terminal := range []string{"started", "completed", "failed"} {
		if strings.HasSuffix(action, "."+terminal) {
			return state == terminal
		}
	}
	if action == "model.requested" || action == "tool.invoked" {
		return state == "started"
	}
	return true
}

func contentKindForSubject(subjectKind, contentKind string) bool {
	switch subjectKind {
	case "task":
		return oneOf(contentKind, "user_input", "user_output")
	case "message":
		return oneOf(contentKind, "user_input", "user_output")
	case "model_call":
		return oneOf(contentKind, "model_input", "model_output")
	case "tool_call":
		return oneOf(contentKind, "tool_input", "tool_output")
	case "resource_action":
		return oneOf(contentKind, "resource_before", "resource_after")
	}
	return false
}

func oneOf(s string, values ...string) bool {
	for _, value := range values {
		if s == value {
			return true
		}
	}
	return false
}

func decode(raw []byte, dst any) error {
	if len(raw) == 0 || len(raw) > MaxEventBytes {
		return ErrInvalid
	}
	value, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return ErrInvalid
	}
	canonical, err := connector.CanonicalizeJSONValue(value)
	if err != nil || !bytes.Equal(raw, canonical) {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}

func DecodeEvent(raw []byte) (Event, error) {
	var event Event
	if err := decode(raw, &event); err != nil {
		return Event{}, err
	}
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func DecodeRelation(raw []byte) (RelationAssertion, error) {
	var relation RelationAssertion
	if err := decode(raw, &relation); err != nil {
		return RelationAssertion{}, err
	}
	if err := relation.Validate(); err != nil {
		return RelationAssertion{}, err
	}
	return relation, nil
}
