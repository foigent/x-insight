package db

import (
	"github.com/foigent/x-insight/internal/sourcefacts"
	"testing"
)

func TestContentRestoreProgressBoundaries(t *testing.T) {
	boundary := sourcefacts.ContentBoundary{Time: "2026-09-15T00:00:00.000000Z", AfterSequence: "0"}
	c := sourcefacts.ContentCollection{CoverageFrom: boundary, LastSequence: "0", HighWatermarkSequence: "0", Coverage: "unknown"}
	if !validRestoredContentProgress(c) {
		t.Fatal("initial boundary")
	}
	c.Cursor = "opaque"
	c.HighWatermark = "high"
	c.LastSequence = "3"
	c.HighWatermarkSequence = "3"
	c.RetentionFloor = boundary
	c.Coverage = "partial"
	if !validRestoredContentProgress(c) {
		t.Fatal("partial coverage")
	}
	c.RetentionFloor.AfterSequence = "2"
	if validRestoredContentProgress(c) {
		t.Fatal("missing retention gap accepted")
	}
	c.Gaps = []sourcefacts.ContentGap{{Reason: "retention_floor_ahead"}}
	if !validRestoredContentProgress(c) {
		t.Fatal("retention gap")
	}
	c.HighWatermarkSequence = "2"
	if validRestoredContentProgress(c) {
		t.Fatal("rewound high accepted")
	}
}
func TestContentRestoreAvailabilityDoesNotRewriteCapture(t *testing.T) {
	d := sourcefacts.ContentManifest{Representation: &sourcefacts.ContentRepresentation{}, ExpiresAt: "2026-09-16T00:00:00.000000Z", Capture: sourcefacts.ContentCapture{Completeness: "complete"}}
	a := sourcefacts.ContentAvailability{State: "expired", EvaluatedAt: "2026-09-17T00:00:00.000000Z"}
	if !validRestoredContentAvailability(d, a) {
		t.Fatal("expired historical complete")
	}
	a.State = "available"
	if validRestoredContentAvailability(d, a) {
		t.Fatal("expired bytes called available")
	}
	a.State = "missing"
	if !validRestoredContentAvailability(d, a) {
		t.Fatal("missing historical bytes")
	}
	a.Retryable = true
	if validRestoredContentAvailability(d, a) {
		t.Fatal("invalid missing retry")
	}
}
