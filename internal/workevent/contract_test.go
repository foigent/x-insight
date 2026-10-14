package workevent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
)

func canonical(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	object, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = connector.CanonicalizeJSONValue(object)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func exampleEvent() Event {
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	return Event{
		SchemaVersion: EventVersion,
		Reporter:      Scope{"11111111-1111-4111-8111-111111111111", "openclaw-managed-1", "7", "9"},
		EventRef:      "tool-call-1", Revision: 1, OccurredAt: now, RecordedAt: now,
		SubjectKind: "resource_action", LifecycleState: "completed", RunID: "run-1", ToolCallID: "call-1", Actor: Actor{"agent", "agent-1"},
		ActionKind: "resource.read", Resource: &Resource{"resource-1", "readable", "report.csv"},
		ContentState: "not_captured", ContentRefs: []ContentRef{},
	}
}

func TestWorkEventAcceptsUnlinkedTypedMetadata(t *testing.T) {
	raw := canonical(t, exampleEvent())
	event, err := DecodeEvent(raw)
	if err != nil || event.EventRef != "tool-call-1" || event.Reporter.SourceInstance != "openclaw-managed-1" {
		t.Fatalf("unlinked event rejected: %v", err)
	}
	for _, broken := range [][]byte{
		[]byte(strings.Replace(string(raw), `"action_kind":"resource.read"`, `"action_kind":"resource.read","action_kind":"resource.write"`, 1)),
		[]byte(strings.Replace(string(raw), `"action_kind":"resource.read"`, `"action_kind":"resource.read","tool_input":"secret"`, 1)),
		[]byte(strings.Replace(string(raw), `"name":"report.csv"`, `"name":"bad\u0000name"`, 1)),
		[]byte(strings.Replace(string(raw), `"content_state":"not_captured"`, `"content_state":"referenced"`, 1)),
	} {
		if _, err := DecodeEvent(broken); err == nil {
			t.Fatalf("unsafe or malformed event accepted: %s", broken)
		}
	}
	event.Resource.Name = "/srv/project/report.csv"
	if _, err := DecodeEvent(canonical(t, event)); err != nil {
		t.Fatalf("authorized path-like locator rejected: %v", err)
	}
}

func TestWorkEventRevisionAndNamePolicy(t *testing.T) {
	event := exampleEvent()
	event.Revision = 2
	if _, err := DecodeEvent(canonical(t, event)); err == nil {
		t.Fatal("revision without predecessor hash accepted")
	}
	event.PreviousHash = "sha256:" + strings.Repeat("a", 64)
	event.Resource.NameState, event.Resource.Name = "hidden", ""
	if _, err := DecodeEvent(canonical(t, event)); err != nil {
		t.Fatal(err)
	}
}

func TestWorkEventSubjectKindsAndTypedContent(t *testing.T) {
	for _, example := range []struct {
		kind, action, contentKind string
	}{
		{"task", "task.started", "user_input"},
		{"model_call", "model.requested", "model_input"},
		{"tool_call", "tool.invoked", "tool_input"},
		{"resource_action", "resource.read", "resource_before"},
	} {
		event := exampleEvent()
		event.SubjectKind, event.ActionKind, event.LifecycleState = example.kind, example.action, "started"
		event.TaskRef, event.ModelCallID, event.ToolCallID = "", "", ""
		event.Resource = nil
		switch example.kind {
		case "task":
			event.TaskRef = "task-1"
		case "model_call":
			event.ModelCallID = "model-1"
		case "tool_call":
			event.ToolCallID = "tool-1"
		case "resource_action":
			event.Resource = &Resource{"resource-1", "readable", "/project/report.csv"}
		}
		event.ContentState = "referenced"
		event.ContentRefs = []ContentRef{{Kind: example.contentKind, Ref: "content-1", SHA256: "sha256:" + strings.Repeat("c", 64)}}
		if _, err := DecodeEvent(canonical(t, event)); err != nil {
			t.Fatalf("%s starting event rejected: %v", example.kind, err)
		}
		event.ContentRefs[0].Kind = "tool_output"
		if example.kind != "tool_call" {
			if _, err := DecodeEvent(canonical(t, event)); err == nil {
				t.Fatalf("%s accepted unrelated content kind", example.kind)
			}
		}
	}
}

func TestWorkEventActionAndLifecycleAgree(t *testing.T) {
	for _, example := range []struct {
		action, state string
		valid         bool
	}{
		{"task.started", "started", true},
		{"task.completed", "completed", true},
		{"task.failed", "failed", true},
		{"task.ended_observed", "unknown", true},
		{"task.completed", "started", false},
		{"model.requested", "completed", false},
		{"tool.invoked", "failed", false},
		{"resource.read", "failed", true},
		{"resource.read.not_found", "failed", true},
		{"resource.read.unconfirmed", "unknown", true},
	} {
		event := exampleEvent()
		event.ActionKind, event.LifecycleState = example.action, example.state
		if strings.HasPrefix(example.action, "task.") {
			event.SubjectKind, event.TaskRef, event.Resource = "task", "task-1", nil
		} else if strings.HasPrefix(example.action, "model.") {
			event.SubjectKind, event.ModelCallID, event.Resource = "model_call", "model-1", nil
		} else if strings.HasPrefix(example.action, "tool.") {
			event.SubjectKind, event.ToolCallID, event.Resource = "tool_call", "tool-1", nil
		}
		_, err := DecodeEvent(canonical(t, event))
		if (err == nil) != example.valid {
			t.Errorf("action %s with state %s: valid=%t, err=%v", example.action, example.state, example.valid, err)
		}
	}
}

func TestIndependentMessageAndModelObservationsDoNotInventCallIdentity(t *testing.T) {
	event := exampleEvent()
	event.SubjectKind, event.ActionKind, event.LifecycleState = "message", "message.sent_observed", "unknown"
	event.TaskRef, event.ToolCallID, event.RunID, event.Resource = "", "", "", nil
	event.ContentState = "referenced"
	event.ContentRefs = []ContentRef{{Kind: "user_output", Ref: "body-1", SHA256: "sha256:" + strings.Repeat("a", 64)}}
	if _, err := DecodeEvent(canonical(t, event)); err != nil {
		t.Fatalf("independent message rejected: %v", err)
	}
	event.ActionKind, event.SubjectKind, event.ContentRefs[0].Kind = "model.input_observed", "model_call", "model_input"
	if _, err := DecodeEvent(canonical(t, event)); err != nil {
		t.Fatalf("call-less input observation rejected: %v", err)
	}
	event.ActionKind = "model.requested"
	if _, err := DecodeEvent(canonical(t, event)); err == nil {
		t.Fatal("model request without real call ID accepted")
	}
}

func TestRelationIsOnlySourceAssertion(t *testing.T) {
	event := exampleEvent()
	relation := RelationAssertion{
		SchemaVersion: RelationVersion, Reporter: event.Reporter, RelationRef: "rel-1", EventRef: event.EventRef,
		EventRevision: event.Revision, EventHash: "sha256:" + strings.Repeat("a", 64),
		URTarget:    Scope{"22222222-2222-4222-8222-222222222222", "ur-1", "7", "9"},
		URRequestID: "op_" + strings.Repeat("a", 32), ModelCallID: "run-1:model:1",
		EvidenceRef: "response-header-1", EvidenceHash: "sha256:" + strings.Repeat("b", 64), RecordedAt: event.RecordedAt,
	}
	if _, err := DecodeRelation(canonical(t, relation)); err != nil {
		t.Fatal(err)
	}
	relation.URRequestID = "client-request-1"
	if _, err := DecodeRelation(canonical(t, relation)); err == nil {
		t.Fatal("client request id accepted as UR server id")
	}
}
