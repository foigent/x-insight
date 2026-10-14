package api_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestCC15TranscriptBindsEachAssistantToTheNativeCallAndURResponse(t *testing.T) {
	var ready cc15Ready
	var relation cc15Relation
	if err := json.Unmarshal([]byte(`{"requests":[{},{},{},{},{}]}`), &ready); err != nil {
		t.Fatal(err)
	}
	tools := make([]workbench.TrajectoryToolReport, 4)
	// Native user messages can carry a string content body; only assistant
	// messages have typed tool-call blocks.
	lines := []string{`{"source":"transcript","type":"user.message","seq":0,"traceId":"trace-cc15","sessionId":"session-cc15","data":{"message":{"content":"请读取文件"}}}`}
	relation.Actions = make([]struct {
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
	}, 4)
	relation.TraceID, relation.SessionID = "trace-cc15", "session-cc15"
	for i := range ready.Requests {
		ready.Requests[i].ProviderResponseID = fmt.Sprintf("response-%d", i+1)
		seq := 1 + i*3
		block := `{"type":"text","text":"done"}`
		if i < 4 {
			id := fmt.Sprintf("call-%d", i+1)
			name := "read"
			if i == 2 {
				name = "edit"
			}
			block = fmt.Sprintf(`{"type":"toolCall","id":%q,"name":%q}`, id, name)
			tools[i] = workbench.TrajectoryToolReport{ToolCallID: id, Name: name, CallSeq: seq + 1, ResultSeq: seq + 2}
			relation.Actions[i].AssistantSeq = seq
		}
		line := fmt.Sprintf(`{"source":"transcript","type":"assistant.message","seq":%d,"traceId":"trace-cc15","sessionId":"session-cc15","data":{"message":{"responseId":%q,"content":[%s]}}}`, seq, ready.Requests[i].ProviderResponseID, block)
		lines = append(lines, line)
		if i < 4 {
			relation.Actions[i].AssistantLineSHA = sourcefacts.Hash([]byte(line))
		}
	}
	raw := []byte(strings.Join(lines, "\n") + "\n")
	if !verifyCC15Transcript(raw, ready, relation, tools) {
		t.Fatal("exact five-round relation rejected")
	}
	bad := ready
	bad.Requests[2].ProviderResponseID = "different-response"
	if verifyCC15Transcript(raw, bad, relation, tools) {
		t.Fatal("UR response identity mismatch accepted")
	}
	broken := relation
	broken.Actions = append(relation.Actions[:0:0], relation.Actions...)
	broken.Actions[1].AssistantLineSHA = sourcefacts.Hash([]byte("wrong"))
	if verifyCC15Transcript(raw, ready, broken, tools) {
		t.Fatal("assistant event hash mismatch accepted")
	}
	badOrder := append([]workbench.TrajectoryToolReport{}, tools...)
	badOrder[0].CallSeq = 4
	if verifyCC15Transcript(raw, ready, relation, badOrder) {
		t.Fatal("runtime call outside assistant interval accepted")
	}
}

func TestCC15PreparationCheckNeverCreatesBundle(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "assembly.json")
	if err := os.WriteFile(spec, []byte(`{"schema_version":"xinsight.cc15-action-assembly-input.v1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "bundle.json")
	if _, err := cc15PrepareBundle(spec, out, true); err == nil {
		t.Fatal("incomplete input accepted")
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatal("check wrote a bundle", err)
	}
}
