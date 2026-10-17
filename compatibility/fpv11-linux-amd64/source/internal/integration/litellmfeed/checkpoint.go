package litellmfeed

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/adapters/litellm"
)

const (
	EligibilityDelay      = 60 * time.Second
	MaxWindowPages        = 20
	MaxWindowRows         = 2000
	MaxWindowBytes        = 8 << 20
	PageSize              = 100
	checkpointTimeLayout  = "2006-01-02T15:04:05.000000Z"
	MaxCheckpointBytes    = 512 << 10
	MaxBoundaryRequestIDs = 2000
)

type Checkpoint struct {
	LastStartTime     string   `json:"last_start_time"`
	BoundaryRequestID []string `json:"boundary_request_ids"`
}

func ActivationCheckpoint(now time.Time) Checkpoint {
	return Checkpoint{
		LastStartTime:     now.UTC().Add(-EligibilityDelay).Truncate(time.Microsecond).Format(checkpointTimeLayout),
		BoundaryRequestID: []string{},
	}
}

func DecodeCheckpoint(payload string) (Checkpoint, error) {
	if payload == "" || len(payload) > MaxCheckpointBytes || !utf8.ValidString(payload) {
		return Checkpoint{}, errors.New("LiteLLM Checkpoint bytes are invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(payload)))
	decoder.DisallowUnknownFields()
	var checkpoint Checkpoint
	if err := decoder.Decode(&checkpoint); err != nil {
		return Checkpoint{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Checkpoint{}, errors.New("LiteLLM Checkpoint contains trailing content")
	}
	if err := checkpoint.Validate(); err != nil {
		return Checkpoint{}, err
	}
	return checkpoint, nil
}

func (checkpoint Checkpoint) Encode() (string, error) {
	if err := checkpoint.Validate(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(checkpoint)
	if err != nil {
		return "", err
	}
	if len(payload) > MaxCheckpointBytes {
		return "", errors.New("LiteLLM Checkpoint exceeds the byte limit")
	}
	return string(payload), nil
}

func (checkpoint Checkpoint) Validate() error {
	if _, err := time.Parse(checkpointTimeLayout, checkpoint.LastStartTime); err != nil {
		return errors.New("LiteLLM Checkpoint last_start_time is invalid")
	}
	if checkpoint.BoundaryRequestID == nil ||
		len(checkpoint.BoundaryRequestID) > MaxBoundaryRequestIDs {
		return errors.New("LiteLLM Checkpoint boundary is invalid")
	}
	for index, requestID := range checkpoint.BoundaryRequestID {
		if !validRequestID(requestID) {
			return errors.New("LiteLLM Checkpoint request identity is invalid")
		}
		if index > 0 &&
			bytes.Compare(
				[]byte(checkpoint.BoundaryRequestID[index-1]),
				[]byte(requestID),
			) >= 0 {
			return errors.New("LiteLLM Checkpoint boundary is not strictly UTF-8-byte ordered")
		}
	}
	return nil
}

type WindowSelection struct {
	Accepted []litellm.SanitizedRecord
	Next     Checkpoint
	Advances bool
}

func SelectWindow(
	previous Checkpoint,
	rows []litellm.SanitizedRecord,
) (WindowSelection, error) {
	if err := previous.Validate(); err != nil {
		return WindowSelection{}, err
	}
	if len(rows) > MaxWindowRows {
		return WindowSelection{}, errors.New("LiteLLM complete window exceeds row limit")
	}
	ordered := append([]litellm.SanitizedRecord(nil), rows...)
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].Record.StartTime != ordered[right].Record.StartTime {
			return ordered[left].Record.StartTime < ordered[right].Record.StartTime
		}
		return bytes.Compare(
			[]byte(ordered[left].Record.RequestID),
			[]byte(ordered[right].Record.RequestID),
		) < 0
	})
	seen := make(map[string]struct{}, len(ordered))
	boundary := make(map[string]struct{}, len(previous.BoundaryRequestID))
	for _, requestID := range previous.BoundaryRequestID {
		boundary[requestID] = struct{}{}
	}
	accepted := make([]litellm.SanitizedRecord, 0, len(ordered))
	for _, row := range ordered {
		if _, duplicate := seen[row.Record.RequestID]; duplicate {
			return WindowSelection{}, errors.New("LiteLLM complete window contains duplicate request_id")
		}
		seen[row.Record.RequestID] = struct{}{}
		switch {
		case row.Record.StartTime > previous.LastStartTime:
			accepted = append(accepted, row)
		case row.Record.StartTime == previous.LastStartTime:
			if _, alreadyAccepted := boundary[row.Record.RequestID]; !alreadyAccepted {
				accepted = append(accepted, row)
			}
		}
	}
	if len(accepted) == 0 {
		return WindowSelection{
			Accepted: []litellm.SanitizedRecord{},
			Next:     previous,
			Advances: false,
		}, nil
	}
	maxTime := accepted[len(accepted)-1].Record.StartTime
	nextBoundary := make([]string, 0)
	if maxTime == previous.LastStartTime {
		nextBoundary = append(nextBoundary, previous.BoundaryRequestID...)
	}
	for _, row := range accepted {
		if row.Record.StartTime == maxTime {
			nextBoundary = append(nextBoundary, row.Record.RequestID)
		}
	}
	sort.Slice(nextBoundary, func(left, right int) bool {
		return bytes.Compare([]byte(nextBoundary[left]), []byte(nextBoundary[right])) < 0
	})
	nextBoundary = uniqueStrings(nextBoundary)
	next := Checkpoint{
		LastStartTime:     maxTime,
		BoundaryRequestID: nextBoundary,
	}
	if err := next.Validate(); err != nil {
		return WindowSelection{}, err
	}
	return WindowSelection{Accepted: accepted, Next: next, Advances: true}, nil
}

func validRequestID(value string) bool {
	if !utf8.ValidString(value) || len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character == 0 || character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
