package litellmfeed

import (
	"reflect"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/litellm"
)

func TestActivationCheckpointStartsAtEligibilityForwardBoundary(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 987654321, time.FixedZone("test", 8*60*60))
	checkpoint := ActivationCheckpoint(now)
	if checkpoint.LastStartTime != "2026-07-28T01:59:00.987654Z" {
		t.Fatalf("activation checkpoint=%q", checkpoint.LastStartTime)
	}
	if checkpoint.BoundaryRequestID == nil || len(checkpoint.BoundaryRequestID) != 0 {
		t.Fatalf("activation boundary=%v", checkpoint.BoundaryRequestID)
	}
}

func TestSelectWindowUsesUTF8ByteOrderingAndSameTimestampBoundary(t *testing.T) {
	previous := Checkpoint{
		LastStartTime:     "2026-07-28T02:00:00.500000Z",
		BoundaryRequestID: []string{"request-a", "请求-b"},
	}
	rows := []litellm.SanitizedRecord{
		checkpointRecord("request-z", "2026-07-28T02:00:01.000000Z"),
		checkpointRecord("请求-b", "2026-07-28T02:00:00.500000Z"),
		checkpointRecord("request-new", "2026-07-28T02:00:00.500000Z"),
		checkpointRecord("request-old", "2026-07-28T02:00:00.499999Z"),
	}
	selection, err := SelectWindow(previous, rows)
	if err != nil {
		t.Fatalf("select window: %v", err)
	}
	if !selection.Advances || len(selection.Accepted) != 2 {
		t.Fatalf("selection=%+v", selection)
	}
	gotIDs := []string{
		selection.Accepted[0].Record.RequestID,
		selection.Accepted[1].Record.RequestID,
	}
	if want := []string{"request-new", "request-z"}; !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("accepted ids=%v want=%v", gotIDs, want)
	}
	if selection.Next.LastStartTime != "2026-07-28T02:00:01.000000Z" ||
		!reflect.DeepEqual(selection.Next.BoundaryRequestID, []string{"request-z"}) {
		t.Fatalf("next=%+v", selection.Next)
	}
}

func TestSelectWindowDoesNotAdvanceForOnlySeenSameTimestampRows(t *testing.T) {
	previous := Checkpoint{
		LastStartTime:     "2026-07-28T02:00:00.500000Z",
		BoundaryRequestID: []string{"request-a"},
	}
	selection, err := SelectWindow(previous, []litellm.SanitizedRecord{
		checkpointRecord("request-a", previous.LastStartTime),
	})
	if err != nil {
		t.Fatalf("select window: %v", err)
	}
	if selection.Advances || len(selection.Accepted) != 0 ||
		!reflect.DeepEqual(selection.Next, previous) {
		t.Fatalf("selection=%+v", selection)
	}
}

func checkpointRecord(requestID, startTime string) litellm.SanitizedRecord {
	return litellm.SanitizedRecord{
		Record: litellm.SourceRecord{
			RequestID: requestID,
			StartTime: startTime,
		},
	}
}
