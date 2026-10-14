package workbench

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func actionWitnessFixture() ActionWitness {
	openclaw := ResourceScope{SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "openclaw-test", OrgID: "17", ProjectID: "101"}
	ur := ResourceScope{SourceSystemID: "22222222-2222-4222-8222-222222222222", SourceInstance: "ur-test", OrgID: "17", ProjectID: "101"}
	at := time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)
	link := ActionContentLink{Extent: "full_file", SourceSystemID: ur.SourceSystemID, SourceInstance: ur.SourceInstance, OrgID: ur.OrgID, ProjectID: ur.ProjectID, RequestID: "ur-final", SourceFactID: "fact-1", Revision: "1", ContentRef: "content-1", SourceSHA256: "sha256:" + strings.Repeat("c", 64), Pointer: "/messages/2/content"}
	return ActionWitness{
		Version: ActionWitnessVersion, Environment: "test", RecordRef: "action-edit-1", Revision: 1,
		Reporter: openclaw, Target: ur, RequestID: "ur-edit", FinalRequestID: "ur-final", RelatedRequestIDs: []string{"ur-final"}, OccurredAt: at,
		RunID: "run-1", ToolCallID: "call-1", ActionKind: "edit", Object: ActionObject{Ref: "test-file-1", Version: "v2", Name: "report.md"},
		ToolResult:     ActionToolResult{State: "completed", ResultSHA256: "sha256:" + strings.Repeat("d", 64)},
		NativeEvidence: ActionNativeEvidence{TraceID: "trace-1", SessionID: "session-1", ManifestSHA256: "sha256:" + strings.Repeat("1", 64), EventsSHA256: "sha256:" + strings.Repeat("2", 64), CallSeq: 11, CallSHA256: "sha256:" + strings.Repeat("3", 64), ResultSeq: 12, ResultSHA256: "sha256:" + strings.Repeat("4", 64)},
		Relationship:   &ActionRelationship{ReceiptRef: "relationship-1", ReceiptSHA256: "sha256:" + strings.Repeat("5", 64)},
		Acceptance:     ActionAcceptance{State: "confirmed", GoalState: "passed", HashScope: "full_file", BeforeSHA256: "sha256:" + strings.Repeat("a", 64), AfterSHA256: "sha256:" + strings.Repeat("b", 64), CheckedAt: &at, CheckerRef: "test-checker-1", BeforeReceiptSHA256: "sha256:" + strings.Repeat("6", 64), AfterReceiptSHA256: "sha256:" + strings.Repeat("7", 64)},
		ContentLinks:   []ActionContentLink{{Purpose: "before", Extent: link.Extent, SourceSystemID: link.SourceSystemID, SourceInstance: link.SourceInstance, OrgID: link.OrgID, ProjectID: link.ProjectID, RequestID: link.RequestID, SourceFactID: link.SourceFactID, Revision: link.Revision, ContentRef: link.ContentRef, SourceSHA256: link.SourceSHA256, Pointer: link.Pointer}, {Purpose: "after", Extent: link.Extent, SourceSystemID: link.SourceSystemID, SourceInstance: link.SourceInstance, OrgID: link.OrgID, ProjectID: link.ProjectID, RequestID: link.RequestID, SourceFactID: "fact-2", Revision: link.Revision, ContentRef: "content-2", SourceSHA256: link.SourceSHA256, Pointer: "/messages/3/content"}},
	}
}

func TestActionWitnessStrictMetadataAndSeparatedClaims(t *testing.T) {
	good := actionWitnessFixture()
	raw, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeActionWitnessForImport(raw)
	if err != nil || got.ActionKind != "edit" || got.Acceptance.State != "confirmed" || got.ToolResult.State != "completed" || got.Object.Name != "report.md" {
		t.Fatalf("valid action witness: %+v %v", got, err)
	}
	independent := actionWitnessFixture()
	independent.ToolResult.State = "failed"
	independent.Object.Name = "测试报告..md"
	if err := independent.Validate(); err != nil {
		t.Fatalf("checker pass must stay independent of tool report; UTF-8 display name valid: %v", err)
	}
	unmatched := actionWitnessFixture()
	unmatched.FinalRequestID = ""
	unmatched.RelatedRequestIDs = nil
	unmatched.Relationship = nil
	for i := range unmatched.ContentLinks {
		unmatched.ContentLinks[i].RequestID = unmatched.RequestID
	}
	if err := unmatched.Validate(); err != nil {
		t.Fatalf("unmatched action must remain visible under its trigger: %v", err)
	}
	for name, mutate := range map[string]func(*ActionWitness){
		"production admission":       func(r *ActionWitness) { r.Environment = "production" },
		"synthetic v1":               func(r *ActionWitness) { r.Version = ResourceAccessVersion },
		"unsupported action":         func(r *ActionWitness) { r.ActionKind = "shell" },
		"path as name":               func(r *ActionWitness) { r.Object.Name = "../report.md" },
		"unchanged file confirmed":   func(r *ActionWitness) { r.Acceptance.AfterSHA256 = r.Acceptance.BeforeSHA256 },
		"fragment called full file":  func(r *ActionWitness) { r.Acceptance.HashScope = "fragment" },
		"cross source content":       func(r *ActionWitness) { r.ContentLinks[0].SourceSystemID = r.Reporter.SourceSystemID },
		"unrelated content request":  func(r *ActionWitness) { r.ContentLinks[0].RequestID = "other-run" },
		"duplicate content role":     func(r *ActionWitness) { r.ContentLinks[1].Purpose = "before" },
		"invalid JSON pointer":       func(r *ActionWitness) { r.ContentLinks[0].Pointer = "/bad~escape" },
		"missing final relationship": func(r *ActionWitness) { r.RelatedRequestIDs = []string{} },
		"missing native event":       func(r *ActionWitness) { r.NativeEvidence.CallSHA256 = "" },
		"missing relationship":       func(r *ActionWitness) { r.Relationship.ReceiptRef = "" },
		"missing checker receipt":    func(r *ActionWitness) { r.Acceptance.AfterReceiptSHA256 = "" },
		"reversed native events":     func(r *ActionWitness) { r.NativeEvidence.ResultSeq = 10 },
		"withdrawn confirmation":     func(r *ActionWitness) { r.Withdrawn = true },
	} {
		t.Run(name, func(t *testing.T) {
			bad := actionWitnessFixture()
			mutate(&bad)
			encoded, _ := json.Marshal(bad)
			if _, err := DecodeActionWitnessForImport(encoded); err == nil {
				t.Fatal("invalid action witness accepted")
			}
		})
	}
	for _, inject := range []string{`"prompt":"raw content"`, `"schema_version":"duplicate"`, `"RESULT":"alias"`} {
		bad := append(append([]byte{}, raw[:len(raw)-1]...), []byte(","+inject+"}")...)
		if _, err := DecodeActionWitnessForImport(bad); err == nil {
			t.Fatal("unknown, duplicate, or raw field accepted", inject)
		}
	}
}

func TestActionWitnessMatchesExactNativeActionKind(t *testing.T) {
	manifest, events := trajectoryFixture()
	tools, err := ExtractOpenClawRuntimeTools(manifest, events)
	if err != nil || len(tools) != 1 {
		t.Fatal(err)
	}
	r := actionWitnessFixture()
	tool := tools[0]
	r.RunID, r.ToolCallID = tool.RunID, tool.ToolCallID
	r.NativeEvidence = ActionNativeEvidence{TraceID: tool.TraceID, SessionID: tool.SessionID, ManifestSHA256: tool.ManifestSHA256, EventsSHA256: tool.EventsSHA256, CallSeq: tool.CallSeq, CallSHA256: tool.CallSHA256, ResultSeq: tool.ResultSeq, ResultSHA256: tool.ResultEventSHA256}
	r.ToolResult = ActionToolResult{State: tool.State, ResultSHA256: tool.ResultSHA256}
	if r.MatchesTrajectory(tool) {
		t.Fatal("read runtime event accepted as edit")
	}
	r.ActionKind = "read"
	if !r.MatchesTrajectory(tool) {
		t.Fatal("exact read runtime event did not match")
	}
}
