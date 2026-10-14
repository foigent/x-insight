package db

import (
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/investigation"
)

func TestFinalizeTraceJourneyUsesOnlyUniqueSourceDeclaredParents(t *testing.T) {
	journey := investigation.TraceJourney{
		Nodes: []investigation.TraceJourneyNode{
			{ExecutionID: "execution-a", EventID: "event-root", SpanID: "span-root"},
			{ExecutionID: "execution-b", EventID: "event-child", SpanID: "span-child", ParentSpanID: "span-root"},
			{ExecutionID: "execution-c", EventID: "event-unresolved", SpanID: "span-unresolved", ParentSpanID: "span-absent"},
		},
	}
	finalizeTraceJourney(&journey)

	if !journey.Available || len(journey.Edges) != 1 {
		t.Fatalf("journey=%+v", journey)
	}
	if journey.Edges[0].FromEventID != "event-root" ||
		journey.Edges[0].ToEventID != "event-child" ||
		journey.Edges[0].EdgeBasis != "source_declared_parent_span_across_execution" {
		t.Fatalf("edge=%+v", journey.Edges[0])
	}
	if journey.Integrity.NodeCount != 3 ||
		journey.Integrity.ExecutionCount != 3 ||
		journey.Integrity.DeclaredParentCount != 2 ||
		journey.Integrity.ResolvedParentCount != 1 ||
		journey.Integrity.UnresolvedParentCount != 1 ||
		journey.Integrity.VisibleParentResolutionComplete {
		t.Fatalf("integrity=%+v", journey.Integrity)
	}
	if journey.Integrity.SourceTimelineComplete {
		t.Fatal("bounded trace journey claimed source timeline completeness")
	}
}

func TestFinalizeTraceJourneyRejectsAmbiguousSpanIdentityAndTruncation(t *testing.T) {
	journey := investigation.TraceJourney{
		Nodes: []investigation.TraceJourneyNode{
			{ExecutionID: "execution-a", EventID: "event-root-a", SpanID: "span-root", EffectiveAt: time.Unix(1, 0)},
			{ExecutionID: "execution-b", EventID: "event-root-b", SpanID: "span-root", EffectiveAt: time.Unix(2, 0)},
			{ExecutionID: "execution-c", EventID: "event-child", SpanID: "span-child", ParentSpanID: "span-root", EffectiveAt: time.Unix(3, 0)},
		},
		Integrity: investigation.TraceJourneyIntegrity{Truncated: true},
	}
	finalizeTraceJourney(&journey)

	if len(journey.Edges) != 0 || journey.Integrity.AmbiguousParentCount != 1 ||
		journey.Integrity.ResolvedParentCount != 0 ||
		journey.Integrity.VisibleParentResolutionComplete {
		t.Fatalf("journey=%+v", journey)
	}
}

func TestFinalizeTraceJourneyMarksSourceDeclaredCycleIncomplete(t *testing.T) {
	journey := investigation.TraceJourney{
		Nodes: []investigation.TraceJourneyNode{
			{ExecutionID: "execution-a", EventID: "event-a", SpanID: "span-a", ParentSpanID: "span-b"},
			{ExecutionID: "execution-b", EventID: "event-b", SpanID: "span-b", ParentSpanID: "span-a"},
		},
	}
	finalizeTraceJourney(&journey)

	if len(journey.Edges) != 2 || !journey.Integrity.CycleDetected ||
		journey.Integrity.VisibleParentResolutionComplete {
		t.Fatalf("journey=%+v", journey)
	}
}
