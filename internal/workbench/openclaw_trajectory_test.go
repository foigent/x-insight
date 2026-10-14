package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func trajectoryFixture() ([]byte, []byte) {
	manifest := []byte(`{"traceSchema":"openclaw-trajectory","schemaVersion":1,"traceId":"trace-1","sessionId":"session-1","eventCount":3,"runtimeEventCount":2,"transcriptEventCount":1}`)
	events := []byte(`{"traceSchema":"openclaw-trajectory","schemaVersion":1,"traceId":"trace-1","sessionId":"session-1","source":"runtime","type":"tool.call","ts":"2026-09-25T01:00:00Z","seq":1,"runId":"run-1","data":{"toolCallId":"call-1","name":"read","args":{"path":"sample.txt"}}}
{"traceSchema":"openclaw-trajectory","schemaVersion":1,"traceId":"trace-1","sessionId":"session-1","source":"transcript","type":"tool.result","ts":"2026-09-25T01:00:00Z","seq":2,"runId":"run-1","data":{"toolCallId":"call-1","name":"read","success":true,"result":"transcript must not count"}}
{"traceSchema":"openclaw-trajectory","schemaVersion":1,"traceId":"trace-1","sessionId":"session-1","source":"runtime","type":"tool.result","ts":"2026-09-25T01:00:01Z","seq":3,"runId":"run-1","data":{"toolCallId":"call-1","name":"read","success":true,"result":{"content":"secret source body"}}}
`)
	return manifest, events
}

func TestExtractOpenClawRuntimeToolsIgnoresTranscriptAndRawContent(t *testing.T) {
	manifest, events := trajectoryFixture()
	items, err := ExtractOpenClawRuntimeTools(manifest, events)
	if err != nil || len(items) != 1 || items[0].Name != "read" || items[0].State != "completed" || items[0].ResultAt == nil || items[0].ResultSHA256 == "" ||
		items[0].ManifestSHA256 != sourcefacts.Hash(manifest) || items[0].EventsSHA256 != sourcefacts.Hash(events) || items[0].CallSeq != 1 || items[0].ResultSeq != 3 ||
		items[0].CallSHA256 != sourcefacts.Hash([]byte(strings.Split(string(events), "\n")[0])) || items[0].ResultEventSHA256 != sourcefacts.Hash([]byte(strings.Split(string(events), "\n")[2])) {
		t.Fatalf("runtime tool pairing: %+v %v", items, err)
	}
	raw, _ := json.Marshal(items)
	if strings.Contains(string(raw), "secret source body") || strings.Contains(string(raw), "sample.txt") || strings.Contains(string(raw), "transcript must not count") {
		t.Fatal("raw arguments or result leaked into action projection")
	}
	object := ActionObject{Ref: "sample-file", Version: "v1", Name: "sample.txt"}
	if !MatchTrajectoryObject(events, items[0], object, map[string]ActionObject{"sample.txt": object}) ||
		MatchTrajectoryObject(events, items[0], object, map[string]ActionObject{"other.txt": object}) ||
		MatchTrajectoryObject(events, items[0], ActionObject{Ref: "other-file", Version: "v1", Name: "sample.txt"}, map[string]ActionObject{"sample.txt": object}) {
		t.Fatal("native path must resolve to one exact allowlisted object")
	}
	for name, changed := range map[string][]byte{
		"count mismatch":     bytesReplace(manifest, `"eventCount":3`, `"eventCount":4`),
		"duplicate result":   append(append([]byte{}, events...), events[strings.LastIndex(string(events), `{"traceSchema"`):]...),
		"result before call": bytesReplace(events, `"seq":3`, `"seq":1`),
		"run mismatch":       bytesReplace(events, `"seq":3,"runId":"run-1"`, `"seq":3,"runId":"other-run"`),
		"duplicate JSON key": bytesReplace(events, `"name":"read","args"`, `"name":"read","name":"read","args"`),
	} {
		t.Run(name, func(t *testing.T) {
			m, e := manifest, events
			if name == "count mismatch" {
				m = changed
			} else {
				e = changed
			}
			if _, err := ExtractOpenClawRuntimeTools(m, e); err == nil {
				t.Fatal("invalid trajectory accepted")
			}
		})
	}
}

func TestExtractOpenClawRuntimeToolsNativeR9(t *testing.T) {
	dir := os.Getenv("XINSIGHT_OPENCLAW_TRAJECTORY_DIR")
	if dir == "" {
		t.Skip("set XINSIGHT_OPENCLAW_TRAJECTORY_DIR to the dedicated synthetic export")
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	events, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	items, err := ExtractOpenClawRuntimeTools(manifest, events)
	if err != nil || len(items) != 1 || items[0].Name != "read" || items[0].State != "completed" || items[0].ToolCallID != "callcc14readsample" {
		t.Fatalf("native r9 tool evidence: %+v %v", items, err)
	}
}

func TestExtractOpenClawRuntimeToolsLargeIntegerIsLossless(t *testing.T) {
	manifest, events := trajectoryFixture()
	events = bytesReplace(events, `"path":"sample.txt"`, `"path":"sample.txt","size":9007199254740993`)
	items, err := ExtractOpenClawRuntimeTools(manifest, events)
	if err != nil || len(items) != 1 || items[0].ArgumentsSHA256 != sourcefacts.Hash([]byte(`{"path":"sample.txt","size":9007199254740993}`)) {
		t.Fatalf("native args hash must preserve large integer bytes: %+v %v", items, err)
	}
}

func bytesReplace(raw []byte, old, replacement string) []byte {
	return []byte(strings.Replace(string(raw), old, replacement, 1))
}
