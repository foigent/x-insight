package workbench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

// TrajectoryToolReport is an allowlisted projection of OpenClaw's native
// runtime export. It never contains raw args, result, prompts or workspace
// paths. The export is a source report, not an independent resource audit.
type TrajectoryToolReport struct {
	TraceID           string     `json:"trace_id"`
	RunID             string     `json:"run_id"`
	SessionID         string     `json:"session_id"`
	ToolCallID        string     `json:"tool_call_id"`
	Name              string     `json:"name"`
	CalledAt          time.Time  `json:"called_at"`
	ResultAt          *time.Time `json:"result_at,omitempty"`
	State             string     `json:"state"`
	ArgumentsSHA256   string     `json:"arguments_sha256"`
	ResultSHA256      string     `json:"result_sha256,omitempty"`
	ManifestSHA256    string     `json:"manifest_sha256"`
	EventsSHA256      string     `json:"events_sha256"`
	CallSeq           int        `json:"call_seq"`
	CallSHA256        string     `json:"call_sha256"`
	ResultSeq         int        `json:"result_seq,omitempty"`
	ResultEventSHA256 string     `json:"result_event_sha256,omitempty"`
}

// MatchTrajectoryObject resolves only an exact allowlisted native tool path.
// Raw paths are inspected transiently and never returned in the projection.
func MatchTrajectoryObject(eventsRaw []byte, tool TrajectoryToolReport, object ActionObject, allowed map[string]ActionObject) bool {
	if len(eventsRaw) == 0 || len(eventsRaw) > maxTrajectoryBytes || sourcefacts.Hash(eventsRaw) != tool.EventsSHA256 {
		return false
	}
	scanner := bufio.NewScanner(bytes.NewReader(eventsRaw))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		raw := scanner.Bytes()
		var event struct {
			Seq    int    `json:"seq"`
			Source string `json:"source"`
			Type   string `json:"type"`
			RunID  string `json:"runId"`
			Data   struct {
				ToolCallID string `json:"toolCallId"`
				Name       string `json:"name"`
				Args       struct {
					Path string `json:"path"`
				} `json:"args"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &event) != nil || event.Seq != tool.CallSeq {
			continue
		}
		matched, ok := allowed[event.Data.Args.Path]
		return ok && event.Source == "runtime" && event.Type == "tool.call" && event.RunID == tool.RunID &&
			event.Data.ToolCallID == tool.ToolCallID && event.Data.Name == tool.Name && sourcefacts.Hash(raw) == tool.CallSHA256 && matched == object
	}
	return false
}

const maxTrajectoryBytes = 4 << 20
const maxTrajectoryEvents = 512
const maxTrajectoryTools = 50

func ExtractOpenClawRuntimeTools(manifestRaw, eventsRaw []byte) ([]TrajectoryToolReport, error) {
	if len(manifestRaw) == 0 || len(manifestRaw) > 16<<10 || len(eventsRaw) == 0 || len(eventsRaw) > maxTrajectoryBytes {
		return nil, ErrInvalid
	}
	manifestValue, err := connector.DecodeStrictLosslessJSONObject(manifestRaw)
	if err != nil {
		return nil, ErrInvalid
	}
	var manifest struct {
		TraceSchema          string `json:"traceSchema"`
		SchemaVersion        int    `json:"schemaVersion"`
		TraceID              string `json:"traceId"`
		SessionID            string `json:"sessionId"`
		EventCount           int    `json:"eventCount"`
		RuntimeEventCount    int    `json:"runtimeEventCount"`
		TranscriptEventCount int    `json:"transcriptEventCount"`
	}
	if err = json.Unmarshal(manifestRaw, &manifest); err != nil || manifestValue["traceSchema"] != "openclaw-trajectory" ||
		manifest.TraceSchema != "openclaw-trajectory" || manifest.SchemaVersion != 1 ||
		!resourceRef.MatchString(manifest.TraceID) || !resourceRef.MatchString(manifest.SessionID) ||
		manifest.EventCount < 1 || manifest.EventCount > maxTrajectoryEvents ||
		manifest.RuntimeEventCount < 0 || manifest.TranscriptEventCount < 0 ||
		manifest.RuntimeEventCount+manifest.TranscriptEventCount != manifest.EventCount {
		return nil, ErrInvalid
	}
	type nativeEvent struct {
		TraceSchema   string          `json:"traceSchema"`
		SchemaVersion int             `json:"schemaVersion"`
		TraceID       string          `json:"traceId"`
		Source        string          `json:"source"`
		Type          string          `json:"type"`
		TS            string          `json:"ts"`
		Seq           int             `json:"seq"`
		SessionID     string          `json:"sessionId"`
		RunID         string          `json:"runId"`
		Data          json.RawMessage `json:"data"`
	}
	type toolData struct {
		ToolCallID string          `json:"toolCallId"`
		Name       string          `json:"name"`
		Args       json.RawMessage `json:"args"`
		Success    *bool           `json:"success"`
		Result     json.RawMessage `json:"result"`
	}
	reports := make([]TrajectoryToolReport, 0)
	seen := map[string]int{}
	count, runtimeCount, transcriptCount, previousSeq := 0, 0, 0, 0
	scanner := bufio.NewScanner(bytes.NewReader(eventsRaw))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		count++
		if count > maxTrajectoryEvents {
			return nil, ErrInvalid
		}
		raw := scanner.Bytes()
		if _, err = connector.DecodeStrictLosslessJSONObject(raw); err != nil {
			return nil, ErrInvalid
		}
		var event nativeEvent
		if err = json.Unmarshal(raw, &event); err != nil || event.TraceSchema != manifest.TraceSchema || event.SchemaVersion != 1 ||
			event.TraceID != manifest.TraceID || event.SessionID != manifest.SessionID || event.Seq <= previousSeq {
			return nil, ErrInvalid
		}
		previousSeq = event.Seq
		switch event.Source {
		case "runtime":
			runtimeCount++
		case "transcript":
			transcriptCount++
			continue // transcript is never a second action
		default:
			return nil, ErrInvalid
		}
		if event.Type != "tool.call" && event.Type != "tool.result" {
			continue
		}
		if !resourceRef.MatchString(event.RunID) || len(event.Data) == 0 {
			return nil, ErrInvalid
		}
		if _, err = connector.DecodeStrictLosslessJSONObject(event.Data); err != nil {
			return nil, ErrInvalid
		}
		var data toolData
		if err = json.Unmarshal(event.Data, &data); err != nil || !resourceRef.MatchString(data.ToolCallID) || !resourceRef.MatchString(data.Name) {
			return nil, ErrInvalid
		}
		at, parseErr := time.Parse(time.RFC3339Nano, event.TS)
		if parseErr != nil {
			return nil, ErrInvalid
		}
		key := event.RunID + "\x00" + data.ToolCallID
		if event.Type == "tool.call" {
			if _, exists := seen[key]; exists || len(data.Args) == 0 || data.Success != nil || len(reports) >= maxTrajectoryTools {
				return nil, ErrInvalid
			}
			seen[key] = len(reports)
			reports = append(reports, TrajectoryToolReport{TraceID: event.TraceID, RunID: event.RunID, SessionID: event.SessionID, ToolCallID: data.ToolCallID, Name: data.Name, CalledAt: at.UTC(), State: "unknown", ArgumentsSHA256: sourcefacts.Hash(data.Args), ManifestSHA256: sourcefacts.Hash(manifestRaw), EventsSHA256: sourcefacts.Hash(eventsRaw), CallSeq: event.Seq, CallSHA256: sourcefacts.Hash(raw)})
			continue
		}
		index, exists := seen[key]
		if !exists || reports[index].Name != data.Name || reports[index].ResultAt != nil || data.Success == nil || at.Before(reports[index].CalledAt) {
			return nil, ErrInvalid
		}
		report := &reports[index]
		at = at.UTC()
		report.ResultAt = &at
		report.ResultSeq = event.Seq
		report.ResultEventSHA256 = sourcefacts.Hash(raw)
		if *data.Success {
			report.State = "completed"
		} else {
			report.State = "failed"
		}
		if len(data.Result) != 0 {
			report.ResultSHA256 = sourcefacts.Hash(data.Result)
		}
	}
	if err = scanner.Err(); err != nil || count != manifest.EventCount || runtimeCount != manifest.RuntimeEventCount || transcriptCount != manifest.TranscriptEventCount {
		return nil, ErrInvalid
	}
	return reports, nil
}
