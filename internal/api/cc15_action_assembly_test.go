package api_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"regexp"
)

var cc15ResourceRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]{0,127}$`)

// cc15ActionAssemblyInput contains exact source bytes supplied by a bounded CC15
// local qualification run. It is not a general live adapter or credential path.
type cc15ActionAssemblyInput struct {
	ManifestRaw, EventsRaw, RequestsReadyRaw, RelationshipRaw, CheckerBeforeRaw, CheckerAfterRaw []byte
	BeforeFiles, AfterFiles                                                                      map[string][]byte
	Reporter, Target                                                                             workbench.ResourceScope
	AllowedObjects                                                                               map[string]workbench.ActionObject        // exact native args.path -> stable object
	ContentLinks                                                                                 map[string][]workbench.ActionContentLink // toolCallId -> exact C refs
}

type cc15Ready struct {
	Run           string `json:"run"`
	RequestCount  int    `json:"request_count"`
	ContentPoints int    `json:"content_points"`
	Source        struct {
		SourceInstance string `json:"source_instance"`
		OrgID          string `json:"org_id"`
		ProjectID      string `json:"project_id"`
	} `json:"source"`
	Requests []struct {
		RequestID          string `json:"request_id"`
		ProviderResponseID string `json:"provider_response_id"`
		Round              int    `json:"round"`
		Status             int    `json:"status"`
		ProviderCalls      int    `json:"provider_calls"`
	} `json:"requests"`
}
type cc15Relation struct {
	SchemaVersion       string   `json:"schema_version"`
	Verified            bool     `json:"verified"`
	RunID               string   `json:"run_id"`
	SessionID           string   `json:"session_id"`
	TraceID             string   `json:"trace_id"`
	ManifestSHA256      string   `json:"manifest_sha256"`
	EventsSHA256        string   `json:"events_sha256"`
	RequestsReadySHA256 string   `json:"requests_ready_sha256"`
	RequestIDs          []string `json:"request_ids"`
	ProviderResponseIDs []string `json:"provider_response_ids"`
	Actions             []struct {
		ToolCallID       string `json:"tool_call_id"`
		ActionKind       string `json:"action_kind"`
		TriggerRequestID string `json:"trigger_request_id"`
		FinalRequestID   string `json:"final_request_id"`
		ResponseID       string `json:"response_id"`
		AssistantSeq     int    `json:"assistant_seq"`
		AssistantLineSHA string `json:"assistant_line_sha256"`
		CallSeq          int    `json:"call_seq"`
		CallLineSHA256   string `json:"call_line_sha256"`
		ResultSeq        int    `json:"result_seq"`
		ResultLineSHA256 string `json:"result_line_sha256"`
	} `json:"actions"`
}
type cc15FileCheck struct {
	SchemaVersion string    `json:"schema_version"`
	Phase         string    `json:"phase"`
	CheckerRef    string    `json:"checker_ref"`
	ObservedAt    time.Time `json:"observed_at"`
	Files         map[string]struct {
		SHA256 string `json:"sha256"`
		Size   int    `json:"size"`
	} `json:"files"`
	Checks            map[string]bool `json:"checks"`
	Passed            bool            `json:"passed"`
	WriterAttribution string          `json:"writer_attribution"`
}

// cc15AssembleActionWitnesses verifies source bytes before producing the
// metadata-only records. The native result and local file checker remain
// separate claims. UR remains authority only for its own request/C facts.
func cc15AssembleActionWitnesses(in cc15ActionAssemblyInput) ([]workbench.ActionWitness, error) {
	if in.Reporter.SourceSystemID == "" || in.Target.SourceSystemID == "" {
		return nil, workbench.ErrInvalid
	}
	tools, err := workbench.ExtractOpenClawRuntimeTools(in.ManifestRaw, in.EventsRaw)
	if err != nil || len(tools) != 4 {
		return nil, workbench.ErrInvalid
	}
	for _, raw := range [][]byte{in.RequestsReadyRaw, in.RelationshipRaw, in.CheckerBeforeRaw, in.CheckerAfterRaw} {
		if len(raw) == 0 || len(raw) > 1<<20 {
			return nil, workbench.ErrInvalid
		}
		if _, err = connector.DecodeStrictLosslessJSONObject(raw); err != nil {
			return nil, workbench.ErrInvalid
		}
	}
	var ready cc15Ready
	var relation cc15Relation
	var before, after cc15FileCheck
	if json.Unmarshal(in.RequestsReadyRaw, &ready) != nil || json.Unmarshal(in.RelationshipRaw, &relation) != nil || json.Unmarshal(in.CheckerBeforeRaw, &before) != nil || json.Unmarshal(in.CheckerAfterRaw, &after) != nil {
		return nil, workbench.ErrInvalid
	}
	if ready.Run != "cc15-openclaw-file-actions-20260925-r1" || ready.RequestCount != 5 || ready.ContentPoints != 20 || len(ready.Requests) != 5 ||
		ready.Source.SourceInstance != in.Target.SourceInstance || ready.Source.OrgID != in.Target.OrgID || ready.Source.ProjectID != in.Target.ProjectID ||
		relation.SchemaVersion != "cc15.test-relationship-receipt.v1" || !relation.Verified || len(relation.Actions) != 4 || len(relation.RequestIDs) != 5 || len(relation.ProviderResponseIDs) != 5 ||
		relation.ManifestSHA256 != sourcefacts.Hash(in.ManifestRaw) || relation.EventsSHA256 != sourcefacts.Hash(in.EventsRaw) || relation.RequestsReadySHA256 != sourcefacts.Hash(in.RequestsReadyRaw) ||
		before.SchemaVersion != "xi.test-file-check.v1" || before.Phase != "before" || after.SchemaVersion != "xi.test-file-check.v1" || after.Phase != "after" ||
		before.CheckerRef == "" || before.CheckerRef != after.CheckerRef || before.ObservedAt.IsZero() || after.ObservedAt.Before(before.ObservedAt) ||
		before.WriterAttribution != "not_established_by_file_readback" || after.WriterAttribution != "not_established_by_file_readback" ||
		len(before.Files) != 3 || len(after.Files) != 3 || len(in.BeforeFiles) != 3 || len(in.AfterFiles) != 3 {
		return nil, workbench.ErrInvalid
	}
	for i, r := range ready.Requests {
		if r.Round != i+1 || r.Status != 200 || r.ProviderCalls != 1 || !cc15ResourceRef.MatchString(r.RequestID) || !cc15ResourceRef.MatchString(r.ProviderResponseID) || r.RequestID != relation.RequestIDs[i] || r.ProviderResponseID != relation.ProviderResponseIDs[i] {
			return nil, workbench.ErrInvalid
		}
	}
	if !verifyCC15Transcript(in.EventsRaw, ready, relation, tools) {
		return nil, workbench.ErrInvalid
	}
	for _, name := range []string{"input.md", "report.md", "task.md"} {
		b, okB := in.BeforeFiles[name]
		a, okA := in.AfterFiles[name]
		br, okBR := before.Files[name]
		ar, okAR := after.Files[name]
		if !okB || !okA || !okBR || !okAR || len(b) != br.Size || len(a) != ar.Size || sourcefacts.Hash(b) != br.SHA256 || sourcefacts.Hash(a) != ar.SHA256 {
			return nil, workbench.ErrInvalid
		}
	}
	if !after.Passed || len(after.Checks) != 4 || !after.Checks["input_unchanged"] || !after.Checks["task_unchanged"] || !after.Checks["report_expected_bytes"] || !after.Checks["report_changed"] ||
		before.Files["input.md"].SHA256 != after.Files["input.md"].SHA256 || before.Files["task.md"].SHA256 != after.Files["task.md"].SHA256 || before.Files["report.md"].SHA256 == after.Files["report.md"].SHA256 {
		return nil, workbench.ErrInvalid
	}
	oldText := []byte("完成记录：待填")
	newText := []byte("完成记录：alpha 7，beta 11，合计 18")
	if sourcefacts.Hash(in.BeforeFiles["input.md"]) != "sha256:7b111340f24a443070929b260c54f62c8a9125eb3dbb9ff9df1f84d11ccac26c" ||
		sourcefacts.Hash(in.BeforeFiles["report.md"]) != "sha256:9d040c296af5085f9006590928b64fa8709af7886c5480dc2a52bb51914e7459" ||
		sourcefacts.Hash(in.BeforeFiles["task.md"]) != "sha256:3e3ec529ed680a312270f7d6be9c5581e9eaa32a20c308df62012b3a0a00f479" ||
		!bytes.Equal(in.BeforeFiles["input.md"], in.AfterFiles["input.md"]) || !bytes.Equal(in.BeforeFiles["task.md"], in.AfterFiles["task.md"]) ||
		bytes.Count(in.BeforeFiles["report.md"], oldText) != 1 || !bytes.Equal(in.AfterFiles["report.md"], bytes.Replace(in.BeforeFiles["report.md"], oldText, newText, 1)) {
		return nil, workbench.ErrInvalid
	}
	if relation.RunID != tools[0].RunID || relation.SessionID != tools[0].SessionID || relation.TraceID != tools[0].TraceID {
		return nil, workbench.ErrInvalid
	}
	result := make([]workbench.ActionWitness, 0, 4)
	for i, source := range relation.Actions {
		tool := tools[i]
		if source.ToolCallID != tool.ToolCallID || source.ActionKind != tool.Name || source.TriggerRequestID != ready.Requests[i].RequestID || source.FinalRequestID != ready.Requests[4].RequestID || source.ResponseID != ready.Requests[i].ProviderResponseID ||
			source.CallSeq != tool.CallSeq || source.CallLineSHA256 != tool.CallSHA256 || source.ResultSeq != tool.ResultSeq || source.ResultLineSHA256 != tool.ResultEventSHA256 || tool.State != "completed" || tool.RunID != relation.RunID || tool.SessionID != relation.SessionID || tool.TraceID != relation.TraceID {
			return nil, workbench.ErrInvalid
		}
		var object workbench.ActionObject
		matched := false
		for _, candidate := range in.AllowedObjects {
			if workbench.MatchTrajectoryObject(in.EventsRaw, tool, candidate, in.AllowedObjects) {
				object = candidate
				matched = true
				break
			}
		}
		if !matched {
			return nil, workbench.ErrInvalid
		}
		related := make([]string, 0, 4)
		for _, r := range ready.Requests {
			if r.RequestID != source.TriggerRequestID {
				related = append(related, r.RequestID)
			}
		}
		acceptance := workbench.ActionAcceptance{State: "not_checked", GoalState: "not_checked", HashScope: "unknown"}
		if source.ActionKind == "edit" {
			acceptance = workbench.ActionAcceptance{State: "confirmed", GoalState: "passed", HashScope: "full_file", BeforeSHA256: before.Files["report.md"].SHA256, AfterSHA256: after.Files["report.md"].SHA256, CheckedAt: &after.ObservedAt, CheckerRef: after.CheckerRef, BeforeReceiptSHA256: sourcefacts.Hash(in.CheckerBeforeRaw), AfterReceiptSHA256: sourcefacts.Hash(in.CheckerAfterRaw)}
		}
		links := in.ContentLinks[tool.ToolCallID]
		if links == nil {
			links = []workbench.ActionContentLink{}
		}
		if source.ActionKind == "read" {
			found := false
			for _, link := range links {
				found = found || (link.Purpose == "result" && link.Extent == "full_file" && link.RequestID == ready.Requests[i+1].RequestID)
			}
			if !found {
				return nil, workbench.ErrInvalid
			}
		} else {
			foundBefore, foundAfter := false, false
			for _, link := range links {
				foundBefore = foundBefore || (link.Purpose == "before" && link.Extent == "full_file" && link.RequestID == ready.Requests[2].RequestID)
				foundAfter = foundAfter || (link.Purpose == "after" && link.Extent == "full_file" && link.RequestID == ready.Requests[4].RequestID)
			}
			if !foundBefore || !foundAfter {
				return nil, workbench.ErrInvalid
			}
		}
		r := workbench.ActionWitness{Version: workbench.ActionWitnessVersion, Environment: "test", RecordRef: "cc15-action-" + strconv.Itoa(i+1) + "-" + tool.ToolCallID, Revision: 1, Reporter: in.Reporter, Target: in.Target, RequestID: source.TriggerRequestID, FinalRequestID: source.FinalRequestID, RelatedRequestIDs: related, OccurredAt: tool.CalledAt, RunID: tool.RunID, ToolCallID: tool.ToolCallID, ActionKind: source.ActionKind, Object: object, ToolResult: workbench.ActionToolResult{State: tool.State, ResultSHA256: tool.ResultSHA256}, NativeEvidence: workbench.ActionNativeEvidence{TraceID: tool.TraceID, SessionID: tool.SessionID, ManifestSHA256: tool.ManifestSHA256, EventsSHA256: tool.EventsSHA256, CallSeq: tool.CallSeq, CallSHA256: tool.CallSHA256, ResultSeq: tool.ResultSeq, ResultSHA256: tool.ResultEventSHA256}, Relationship: &workbench.ActionRelationship{ReceiptRef: "cc15-relationship-" + relation.RunID, ReceiptSHA256: sourcefacts.Hash(in.RelationshipRaw)}, Acceptance: acceptance, ContentLinks: links}
		if r.Validate() != nil || !r.MatchesTrajectory(tool) {
			return nil, workbench.ErrInvalid
		}
		result = append(result, r)
	}
	return result, nil
}

func verifyCC15Transcript(raw []byte, ready cc15Ready, relation cc15Relation, tools []workbench.TrajectoryToolReport) bool {
	type block struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var assistant []struct {
		Seq        int
		LineSHA256 string
		ResponseID string
		Blocks     []block
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		var event struct {
			Source    string          `json:"source"`
			Type      string          `json:"type"`
			Seq       int             `json:"seq"`
			TraceID   string          `json:"traceId"`
			SessionID string          `json:"sessionId"`
			Data      json.RawMessage `json:"data"`
		}
		if json.Unmarshal(line, &event) != nil {
			return false
		}
		if event.Source == "transcript" && event.Type == "assistant.message" {
			var assistantData struct {
				Message struct {
					ResponseID string  `json:"responseId"`
					Content    []block `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(event.Data, &assistantData) != nil {
				return false
			}
			if event.TraceID != relation.TraceID || event.SessionID != relation.SessionID {
				return false
			}
			assistant = append(assistant, struct {
				Seq        int
				LineSHA256 string
				ResponseID string
				Blocks     []block
			}{event.Seq, sourcefacts.Hash(line), assistantData.Message.ResponseID, assistantData.Message.Content})
		}
	}
	if scanner.Err() != nil || len(assistant) != 5 {
		return false
	}
	for i, event := range assistant {
		if event.ResponseID != ready.Requests[i].ProviderResponseID {
			return false
		}
		if i == 4 {
			foundText := false
			for _, b := range event.Blocks {
				if b.Type == "toolCall" {
					return false
				}
				if b.Type == "text" {
					foundText = true
				}
			}
			return foundText && tools[3].ResultSeq < event.Seq
		}
		action := relation.Actions[i]
		if action.AssistantSeq != event.Seq || action.AssistantLineSHA != event.LineSHA256 || !(event.Seq < tools[i].CallSeq && tools[i].CallSeq < tools[i].ResultSeq && tools[i].ResultSeq < assistant[i+1].Seq) {
			return false
		}
		callCount := 0
		for _, b := range event.Blocks {
			if b.Type == "toolCall" {
				callCount++
				if b.ID != tools[i].ToolCallID || b.Name != tools[i].Name {
					return false
				}
			}
		}
		if callCount != 1 {
			return false
		}
	}
	return true
}
