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

// ActionWitnessVersion names the resource-action semantics. Its first offline
// admission remains limited to exact test/development authorization.
const ActionWitnessVersion = "xinsight.resource-action.v1"

type ActionObject struct {
	Ref     string `json:"ref"`
	Version string `json:"version"`
	Name    string `json:"name"`
}

type ActionToolResult struct {
	State        string `json:"state"`
	ResultSHA256 string `json:"result_sha256,omitempty"`
}

// NativeEvidence binds this metadata to exact bytes in a native trajectory
// export. The export remains a source report and is retained separately.
type ActionNativeEvidence struct {
	TraceID        string `json:"trace_id"`
	SessionID      string `json:"session_id"`
	ManifestSHA256 string `json:"manifest_sha256"`
	EventsSHA256   string `json:"events_sha256"`
	CallSeq        int    `json:"call_seq"`
	CallSHA256     string `json:"call_sha256"`
	ResultSeq      int    `json:"result_seq,omitempty"`
	ResultSHA256   string `json:"result_sha256,omitempty"`
}

// ActionRelationship is the producer's retained receipt that binds this
// OpenClaw action to an exact UR trigger and final request. IDs alone do not
// establish a cross-source relationship.
type ActionRelationship struct {
	ReceiptRef    string `json:"receipt_ref"`
	ReceiptSHA256 string `json:"receipt_sha256"`
}

// Acceptance is a test checker's readback, not a resource-system audit.
type ActionAcceptance struct {
	State               string     `json:"state"`
	GoalState           string     `json:"goal_state"`
	HashScope           string     `json:"hash_scope"`
	BeforeSHA256        string     `json:"before_sha256,omitempty"`
	AfterSHA256         string     `json:"after_sha256,omitempty"`
	CheckedAt           *time.Time `json:"checked_at,omitempty"`
	CheckerRef          string     `json:"checker_ref,omitempty"`
	BeforeReceiptSHA256 string     `json:"before_receipt_sha256,omitempty"`
	AfterReceiptSHA256  string     `json:"after_receipt_sha256,omitempty"`
}

// ActionContentLink is metadata pointing to an existing C content member.
// The body/diff is never stored in this record and remains separately gated.
type ActionContentLink struct {
	Purpose        string `json:"purpose"`
	Extent         string `json:"extent"`
	SourceSystemID string `json:"source_system_id"`
	SourceInstance string `json:"source_instance"`
	OrgID          string `json:"org_id"`
	ProjectID      string `json:"project_id"`
	RequestID      string `json:"request_id"`
	SourceFactID   string `json:"source_fact_id"`
	Revision       string `json:"revision"`
	ContentRef     string `json:"content_ref"`
	SourceSHA256   string `json:"source_sha256"`
	Pointer        string `json:"json_pointer"`
}

type ActionWitness struct {
	Version           string               `json:"schema_version"`
	Environment       string               `json:"environment"`
	RecordRef         string               `json:"record_ref"`
	Revision          int64                `json:"revision"`
	PreviousHash      string               `json:"previous_hash,omitempty"`
	Reporter          ResourceScope        `json:"reporter"`
	Target            ResourceScope        `json:"target"`
	RequestID         string               `json:"request_id"`
	FinalRequestID    string               `json:"final_request_id,omitempty"`
	RelatedRequestIDs []string             `json:"related_request_ids,omitempty"`
	OccurredAt        time.Time            `json:"occurred_at"`
	RunID             string               `json:"run_id"`
	ToolCallID        string               `json:"tool_call_id"`
	ActionKind        string               `json:"action_kind"`
	Object            ActionObject         `json:"object"`
	ToolResult        ActionToolResult     `json:"tool_result"`
	NativeEvidence    ActionNativeEvidence `json:"native_evidence"`
	Relationship      *ActionRelationship  `json:"relationship,omitempty"`
	Acceptance        ActionAcceptance     `json:"acceptance"`
	ContentLinks      []ActionContentLink  `json:"content_links"`
	Withdrawn         bool                 `json:"withdrawn"`
}

var actionPointer = regexp.MustCompile(`^(?:|/(?:[^~]|~[01])*)$`)

func DecodeActionWitnessForImport(raw []byte) (ActionWitness, error) {
	if len(raw) == 0 || len(raw) > MaxResourceRecordBytes {
		return ActionWitness{}, ErrInvalid
	}
	value, err := connector.DecodeStrictJSONObject(raw)
	if err != nil || !resourceExactFields(value, reflect.TypeOf(ActionWitness{})) {
		return ActionWitness{}, ErrInvalid
	}
	var record ActionWitness
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&record); err != nil {
		return ActionWitness{}, ErrInvalid
	}
	if err = record.Validate(); err != nil {
		return ActionWitness{}, err
	}
	return record, nil
}

func (r ActionWitness) Validate() error {
	if r.Version != ActionWitnessVersion || (r.Environment != "test" && r.Environment != "development") ||
		!validResourceScope(r.Reporter) || !validResourceScope(r.Target) ||
		r.Revision < 1 || r.Revision > 1_000_000 ||
		(r.Revision == 1 && r.PreviousHash != "") || (r.Revision > 1 && !resourceHash.MatchString(r.PreviousHash)) ||
		r.OccurredAt.IsZero() || !resourceRef.MatchString(r.RecordRef) || !resourceRef.MatchString(r.RequestID) ||
		(r.FinalRequestID != "" && !resourceRef.MatchString(r.FinalRequestID)) || !resourceRef.MatchString(r.RunID) || !resourceRef.MatchString(r.ToolCallID) ||
		!resourceOneOf(r.ActionKind, "read", "edit") ||
		!resourceRef.MatchString(r.Object.Ref) || !resourceRef.MatchString(r.Object.Version) ||
		!validActionDisplayName(r.Object.Name) ||
		!resourceOneOf(r.ToolResult.State, "completed", "failed", "unknown") ||
		(r.ToolResult.ResultSHA256 != "" && !resourceHash.MatchString(r.ToolResult.ResultSHA256)) ||
		!resourceRef.MatchString(r.NativeEvidence.TraceID) || !resourceRef.MatchString(r.NativeEvidence.SessionID) ||
		!resourceHash.MatchString(r.NativeEvidence.ManifestSHA256) || !resourceHash.MatchString(r.NativeEvidence.EventsSHA256) ||
		r.NativeEvidence.CallSeq < 1 || !resourceHash.MatchString(r.NativeEvidence.CallSHA256) ||
		(r.NativeEvidence.ResultSeq == 0) != (r.NativeEvidence.ResultSHA256 == "") ||
		(r.NativeEvidence.ResultSeq != 0 && (r.NativeEvidence.ResultSeq <= r.NativeEvidence.CallSeq || !resourceHash.MatchString(r.NativeEvidence.ResultSHA256))) {
		return ErrInvalid
	}
	if len(r.RelatedRequestIDs) > 4 || len(r.ContentLinks) > 4 || r.ContentLinks == nil {
		return ErrInvalid
	}
	seen := map[string]bool{r.RequestID: true}
	for _, id := range r.RelatedRequestIDs {
		if !resourceRef.MatchString(id) || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	if r.FinalRequestID == "" {
		if r.Relationship != nil || len(r.RelatedRequestIDs) != 0 {
			return ErrInvalid
		}
	} else {
		if r.Relationship == nil || !resourceRef.MatchString(r.Relationship.ReceiptRef) || !resourceHash.MatchString(r.Relationship.ReceiptSHA256) || !seen[r.FinalRequestID] {
			return ErrInvalid
		}
	}
	if !resourceOneOf(r.Acceptance.State, "confirmed", "failed", "not_checked") ||
		!resourceOneOf(r.Acceptance.GoalState, "passed", "failed", "not_checked") ||
		!resourceOneOf(r.Acceptance.HashScope, "full_file", "fragment", "unknown") ||
		(r.Acceptance.BeforeSHA256 != "" && !resourceHash.MatchString(r.Acceptance.BeforeSHA256)) ||
		(r.Acceptance.AfterSHA256 != "" && !resourceHash.MatchString(r.Acceptance.AfterSHA256)) ||
		(r.Acceptance.BeforeReceiptSHA256 != "" && !resourceHash.MatchString(r.Acceptance.BeforeReceiptSHA256)) ||
		(r.Acceptance.AfterReceiptSHA256 != "" && !resourceHash.MatchString(r.Acceptance.AfterReceiptSHA256)) {
		return ErrInvalid
	}
	if r.Acceptance.State == "not_checked" {
		if r.Acceptance.CheckedAt != nil || r.Acceptance.CheckerRef != "" || r.Acceptance.BeforeSHA256 != "" || r.Acceptance.AfterSHA256 != "" || r.Acceptance.BeforeReceiptSHA256 != "" || r.Acceptance.AfterReceiptSHA256 != "" || r.Acceptance.GoalState != "not_checked" {
			return ErrInvalid
		}
	} else if r.Acceptance.CheckedAt == nil || r.Acceptance.CheckedAt.IsZero() || !resourceRef.MatchString(r.Acceptance.CheckerRef) ||
		!resourceHash.MatchString(r.Acceptance.BeforeReceiptSHA256) || !resourceHash.MatchString(r.Acceptance.AfterReceiptSHA256) {
		return ErrInvalid
	}
	if r.ActionKind == "edit" && r.Acceptance.State == "confirmed" &&
		(r.Acceptance.HashScope != "full_file" || r.Acceptance.BeforeSHA256 == "" || r.Acceptance.AfterSHA256 == "" || r.Acceptance.BeforeSHA256 == r.Acceptance.AfterSHA256) {
		return ErrInvalid
	}
	links := map[string]bool{}
	for _, link := range r.ContentLinks {
		if !resourceOneOf(link.Purpose, "call", "result", "before", "after") || !resourceOneOf(link.Extent, "full_file", "fragment", "unknown") || links[link.Purpose] ||
			link.SourceSystemID != r.Target.SourceSystemID || link.SourceInstance != r.Target.SourceInstance ||
			link.OrgID != r.Target.OrgID || link.ProjectID != r.Target.ProjectID || !seen[link.RequestID] ||
			!resourceRef.MatchString(link.SourceFactID) || !resourceRef.MatchString(link.Revision) ||
			!resourceRef.MatchString(link.ContentRef) || !resourceHash.MatchString(link.SourceSHA256) ||
			len(link.Pointer) > 512 || !actionPointer.MatchString(link.Pointer) {
			return ErrInvalid
		}
		links[link.Purpose] = true
	}
	if r.Withdrawn && (r.Acceptance.State == "confirmed" || r.Acceptance.GoalState == "passed") {
		return ErrInvalid
	}
	return nil
}

// MatchesTrajectory verifies the exact native event projection. It does not
// verify the separate cross-source relationship or test checker receipt.
func (r ActionWitness) MatchesTrajectory(tool TrajectoryToolReport) bool {
	return r.RunID == tool.RunID && r.ToolCallID == tool.ToolCallID && r.ActionKind == tool.Name &&
		r.NativeEvidence.TraceID == tool.TraceID && r.NativeEvidence.SessionID == tool.SessionID &&
		r.NativeEvidence.ManifestSHA256 == tool.ManifestSHA256 && r.NativeEvidence.EventsSHA256 == tool.EventsSHA256 &&
		r.NativeEvidence.CallSeq == tool.CallSeq && r.NativeEvidence.CallSHA256 == tool.CallSHA256 &&
		r.NativeEvidence.ResultSeq == tool.ResultSeq && r.NativeEvidence.ResultSHA256 == tool.ResultEventSHA256 &&
		r.ToolResult.State == tool.State && r.ToolResult.ResultSHA256 == tool.ResultSHA256
}

func validActionDisplayName(name string) bool {
	return utf8.ValidString(name) && strings.TrimSpace(name) != "" && utf8.RuneCountInString(name) <= 160 &&
		strings.IndexFunc(name, unicode.IsControl) < 0 && name != "." && name != ".." &&
		!strings.ContainsAny(name, `/\`)
}
