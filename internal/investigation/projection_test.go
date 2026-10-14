package investigation

import (
	"reflect"
	"testing"
	"time"
)

func TestBuildProjectionExplainsRecoveredFallbackWithExplicitEdges(t *testing.T) {
	asOf := time.Date(2026, 7, 21, 4, 5, 0, 0, time.UTC)
	nodes := []TimelineNode{
		projectionTimelineNode("route", "routing.route.selected", "routing", asOf.Add(-5*time.Second), "", false),
		projectionTimelineNode("failed", "provider.request.failed", "provider", asOf.Add(-4*time.Second), "route", true),
		projectionTimelineNode("retry", "retry.attempt.scheduled", "routing", asOf.Add(-3*time.Second), "route", true),
		projectionTimelineNode("fallback", "fallback.route.selected", "routing", asOf.Add(-2*time.Second), "retry", true),
		projectionTimelineNode("complete", "usage.tokens.recorded", "usage", asOf.Add(-time.Second), "", false),
	}
	events := []EvidenceEventDetail{
		projectionEvent("route", "routing.route.selected", "routing", 200, "primary", "", 0),
		projectionEvent("failed", "provider.request.failed", "provider", 502, "primary", "", 0),
		projectionEvent("retry", "retry.attempt.scheduled", "routing", 200, "fallback", "", 0),
		projectionEvent("fallback", "fallback.route.selected", "routing", 200, "fallback", "", 0),
		projectionEvent("complete", "usage.tokens.recorded", "usage", 200, "fallback", "7", 11),
	}
	events[len(events)-1].Source.SourceEventType = "call_log"

	input := ProjectionInput{
		Detail: ExecutionDetail{
			Summary: ExecutionSummary{
				ExecutionID: "xe1_fixture",
				RequestID:   "req-fallback",
				Outcome:     OutcomeSuccess,
				Freshness: Freshness{
					IntegrationMode:  IntegrationFixture,
					SyncState:        SyncStateCurrent,
					TimelineComplete: true,
				},
			},
			AsOf: asOf,
		},
		Timeline:        nodes,
		Events:          events,
		TotalEventCount: len(nodes),
		Gaps:            []CaptureGap{},
		TotalGapCount:   0,
	}

	first := BuildProjection(input)
	second := BuildProjection(input)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("projection must be deterministic for a fixed as_of")
	}
	if first.PresentationMode != PresentationCausalTrace {
		t.Fatalf("presentation_mode=%q", first.PresentationMode)
	}
	if len(first.Edges) != 3 {
		t.Fatalf("edges=%d want=3", len(first.Edges))
	}
	for _, edge := range first.Edges {
		if edge.EdgeBasis != "source_declared_parent_span" {
			t.Fatalf("invented edge basis: %+v", edge)
		}
	}
	if !hasClaim(first.SummaryClaims, "primary_provider_failed_then_fallback_succeeded") {
		t.Fatalf("claims=%+v", first.SummaryClaims)
	}
	if coverageStatus(first.StageCoverage, "retry_fallback") != CoveragePresent ||
		coverageStatus(first.StageCoverage, "billing") != CoveragePresent ||
		coverageStatus(first.StageCoverage, "logging") != CoveragePresent ||
		coverageStatus(first.StageCoverage, "identity") != CoveragePresent {
		t.Fatalf("coverage=%+v", first.StageCoverage)
	}
	if !first.Integrity.ProjectionComplete || first.Integrity.Truncated {
		t.Fatalf("integrity=%+v", first.Integrity)
	}
	if first.Gaps == nil {
		t.Fatal("empty projection gaps must encode as [] rather than null")
	}
}

func TestBuildProjectionKeepsTerminalOnlyEvidenceOutOfTraceMode(t *testing.T) {
	asOf := time.Date(2026, 7, 21, 4, 5, 0, 0, time.UTC)
	timeline := projectionTimelineNode(
		"terminal",
		"model.request.completed",
		"usage",
		asOf.Add(-time.Second),
		"",
		false,
	)
	event := projectionEvent(
		"terminal",
		"model.request.completed",
		"usage",
		200,
		"",
		"17",
		4,
	)
	gaps := []CaptureGap{
		{GapID: "gap-trace", GapType: "not_available_from_source", AffectedClaim: "request_trace"},
		{GapID: "gap-log", GapType: "not_available_from_source", AffectedClaim: "call_log"},
	}
	projection := BuildProjection(ProjectionInput{
		Detail: ExecutionDetail{
			Summary: ExecutionSummary{
				ExecutionID: "xe1_terminal",
				RequestID:   "req-terminal",
				Outcome:     OutcomeSuccess,
				Freshness: Freshness{
					IntegrationMode:  IntegrationPull,
					SyncState:        SyncStateGap,
					TimelineComplete: false,
				},
			},
			AsOf: asOf,
		},
		Timeline:        []TimelineNode{timeline},
		Events:          []EvidenceEventDetail{event},
		TotalEventCount: 1,
		Gaps:            gaps,
		TotalGapCount:   len(gaps),
	})

	if projection.PresentationMode != PresentationTerminalSnapshot {
		t.Fatalf("presentation_mode=%q", projection.PresentationMode)
	}
	if len(projection.Edges) != 0 {
		t.Fatalf("terminal snapshot acquired invented edges: %+v", projection.Edges)
	}
	if projection.Integrity.SourceProfileComplete {
		t.Fatalf("terminal snapshot incorrectly complete: %+v", projection.Integrity)
	}
	if len(projection.SummaryClaims) != 0 {
		t.Fatalf("incomplete source must not produce terminal claims: %+v", projection.SummaryClaims)
	}
	if coverageStatus(projection.StageCoverage, "logging") != CoverageGap {
		t.Fatalf("logging coverage=%+v", projection.StageCoverage)
	}
}

func TestBuildProjectionDoesNotInferRecoveredFallbackFromUnrelatedSuccess(t *testing.T) {
	asOf := time.Date(2026, 7, 21, 4, 5, 0, 0, time.UTC)
	timeline := []TimelineNode{
		projectionTimelineNode("failed", "provider.attempt.failed", "provider", asOf.Add(-4*time.Second), "", false),
		projectionTimelineNode("retry", "retry.attempt.scheduled", "routing", asOf.Add(-3*time.Second), "failed", true),
		projectionTimelineNode("fallback", "fallback.route.selected", "routing", asOf.Add(-2*time.Second), "retry", true),
		projectionTimelineNode("complete", "usage.tokens.recorded", "usage", asOf.Add(-time.Second), "", false),
	}
	events := []EvidenceEventDetail{
		projectionEvent("failed", "provider.attempt.failed", "provider", 502, "primary", "", 0),
		projectionEvent("retry", "retry.attempt.scheduled", "routing", 200, "fallback", "", 0),
		projectionEvent("fallback", "fallback.route.selected", "routing", 200, "fallback", "", 0),
		projectionEvent("complete", "usage.tokens.recorded", "usage", 200, "unrelated", "", 1),
	}
	events[len(events)-1].Source.SourceEventType = "call_log"
	projection := BuildProjection(ProjectionInput{
		Detail: ExecutionDetail{
			Summary: ExecutionSummary{
				Freshness: Freshness{TimelineComplete: true, SyncState: SyncStateCurrent},
			},
			AsOf: asOf,
		},
		Timeline:        timeline,
		Events:          events,
		TotalEventCount: len(timeline),
	})
	if hasClaim(projection.SummaryClaims, "primary_provider_failed_then_fallback_succeeded") {
		t.Fatalf("unrelated terminal provider produced a recovered fallback claim: %+v", projection.SummaryClaims)
	}
	if !hasClaim(projection.SummaryClaims, "execution_completed_successfully") {
		t.Fatalf("genuine terminal success was not retained: %+v", projection.SummaryClaims)
	}
}

func TestBuildProjectionKeepsGapEvidenceOutOfExecutionSteps(t *testing.T) {
	asOf := time.Date(2026, 7, 21, 4, 5, 0, 0, time.UTC)
	timeline := []TimelineNode{
		projectionTimelineNode("terminal", "model.request.completed", "usage", asOf.Add(-time.Second), "", false),
		projectionTimelineNode("gap-event", "gap.capture.detected", "gap", asOf, "terminal", true),
	}
	events := []EvidenceEventDetail{
		projectionEvent("terminal", "model.request.completed", "usage", 200, "provider-a", "4", 8),
		projectionEvent("gap-event", "gap.capture.detected", "gap", 200, "", "", 0),
	}
	projection := BuildProjection(ProjectionInput{
		Detail: ExecutionDetail{
			Summary: ExecutionSummary{
				ExecutionID: "xe1_gap",
				Freshness: Freshness{
					TimelineComplete: true,
					SyncState:        SyncStateCurrent,
				},
			},
			AsOf: asOf,
		},
		Timeline:        timeline,
		Events:          events,
		TotalEventCount: len(events),
		Gaps: []CaptureGap{{
			GapID:         "gap-row",
			GapType:       "not_available_from_source",
			AffectedClaim: "request_trace",
		}},
		TotalGapCount: 1,
	})

	if len(projection.Nodes) != 1 || projection.Nodes[0].EventID != "terminal" {
		t.Fatalf("capture Gap was rendered as an execution step: %+v", projection.Nodes)
	}
	if len(projection.Edges) != 0 {
		t.Fatalf("capture Gap acquired an execution edge: %+v", projection.Edges)
	}
	if !projection.Integrity.ProjectionComplete {
		t.Fatalf("excluding a first-class Gap event from execution steps must not make the evidence projection inconsistent: %+v", projection.Integrity)
	}
	if len(projection.Gaps) != 1 {
		t.Fatalf("first-class Gap row was lost: %+v", projection.Gaps)
	}
}

func TestBuildProjectionDoesNotEmitDanglingDeclaredParentEdge(t *testing.T) {
	asOf := time.Date(2026, 7, 21, 4, 5, 0, 0, time.UTC)
	timeline := []TimelineNode{
		projectionTimelineNode("child", "provider.request_succeeded", "provider", asOf, "missing-parent", true),
	}
	events := []EvidenceEventDetail{
		projectionEvent("child", "provider.request_succeeded", "provider", 200, "provider-a", "", 0),
	}
	projection := BuildProjection(ProjectionInput{
		Detail: ExecutionDetail{
			Summary: ExecutionSummary{
				Freshness: Freshness{
					TimelineComplete: true,
					SyncState:        SyncStateCurrent,
				},
			},
			AsOf: asOf,
		},
		Timeline:        timeline,
		Events:          events,
		TotalEventCount: 1,
	})

	if len(projection.Edges) != 0 {
		t.Fatalf("projection emitted an edge to an absent parent: %+v", projection.Edges)
	}
	if projection.Integrity.UnresolvedParentCount != 1 ||
		projection.Integrity.CausalComplete {
		t.Fatalf("missing visible parent was not preserved as an unresolved causal boundary: %+v", projection.Integrity)
	}
}

func TestBuildProjectionRejectsMismatchedAndDuplicateEventViews(t *testing.T) {
	asOf := time.Date(2026, 7, 21, 4, 5, 0, 0, time.UTC)
	tests := []struct {
		name     string
		timeline []TimelineNode
		events   []EvidenceEventDetail
	}{
		{
			name: "same count different identities",
			timeline: []TimelineNode{
				projectionTimelineNode("timeline-only", "external.event.observed", "external", asOf, "", false),
			},
			events: []EvidenceEventDetail{
				projectionEvent("event-only", "external.event.observed", "external", 200, "", "", 0),
			},
		},
		{
			name: "duplicate identities",
			timeline: []TimelineNode{
				projectionTimelineNode("duplicate", "external.event.observed", "external", asOf, "", false),
				projectionTimelineNode("duplicate", "external.event.observed", "external", asOf, "", false),
			},
			events: []EvidenceEventDetail{
				projectionEvent("duplicate", "external.event.observed", "external", 200, "", "", 0),
				projectionEvent("duplicate", "external.event.observed", "external", 200, "", "", 0),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projection := BuildProjection(ProjectionInput{
				Detail:          ExecutionDetail{Summary: ExecutionSummary{Freshness: Freshness{TimelineComplete: true, SyncState: SyncStateCurrent}}, AsOf: asOf},
				Timeline:        test.timeline,
				Events:          test.events,
				TotalEventCount: len(test.timeline),
			})
			if projection.Integrity.ProjectionComplete {
				t.Fatalf("inconsistent evidence views were accepted: %+v", projection.Integrity)
			}
		})
	}
}

func TestSuccessfulNodeMustBeTerminalOrProviderEvidence(t *testing.T) {
	status := int64(200)
	if isSuccessfulNode(InvestigationNode{
		Kind:          "fallback_selected",
		OutcomeStatus: OutcomeSuccess,
		StatusCode:    &status,
	}) {
		t.Fatal("route selection was treated as execution success")
	}
}

func TestBuildProjectionMarksBoundedPartialProjection(t *testing.T) {
	asOf := time.Now().UTC()
	timeline := make([]TimelineNode, maxProjectionNodes+1)
	events := make([]EvidenceEventDetail, maxProjectionNodes+1)
	for index := range timeline {
		eventID := time.Unix(int64(index+1), 0).UTC().Format(time.RFC3339Nano)
		timeline[index] = projectionTimelineNode(eventID, "external.event.observed", "external", asOf, "", false)
		events[index] = projectionEvent(eventID, "external.event.observed", "external", 200, "", "", 0)
	}
	projection := BuildProjection(ProjectionInput{
		Detail:          ExecutionDetail{Summary: ExecutionSummary{Freshness: Freshness{TimelineComplete: true}}, AsOf: asOf},
		Timeline:        timeline,
		Events:          events,
		TotalEventCount: len(timeline),
	})
	if len(projection.Nodes) != maxProjectionNodes ||
		!projection.Integrity.Truncated ||
		projection.Integrity.ProjectionComplete ||
		len(projection.SummaryClaims) != 0 {
		t.Fatalf("bounded projection=%+v claims=%+v", projection.Integrity, projection.SummaryClaims)
	}
}

func projectionTimelineNode(
	eventID string,
	eventType string,
	stage string,
	effectiveAt time.Time,
	parentEventID string,
	parentResolved bool,
) TimelineNode {
	node := TimelineNode{
		EventID:     eventID,
		EventType:   eventType,
		Stage:       stage,
		Severity:    "info",
		EffectiveAt: effectiveAt,
		IngestedAt:  effectiveAt,
		SourceRef:   "fixture://" + eventID,
		SourceType:  "unirouter",
	}
	if parentEventID != "" {
		node.DeclaredParentEventID = parentEventID
		node.DeclaredParentResolved = &parentResolved
	}
	return node
}

func projectionEvent(
	eventID string,
	eventType string,
	stage string,
	statusCode int64,
	provider string,
	costMicros string,
	totalTokens int64,
) EvidenceEventDetail {
	event := EvidenceEventDetail{
		EventID:   eventID,
		EventType: eventType,
		Stage:     stage,
		Severity:  "info",
		Identity: IdentityGroup{
			ActorType: "api_key",
			ActorRef:  "api_key:1",
			OrgID:     "org:1",
			ProjectID: "project:1",
		},
		Capability: CapabilityGroup{
			Provider:       provider,
			Model:          provider + "-model",
			RequestedModel: "requested-model",
		},
		Outcome: OutcomeGroup{StatusCode: &statusCode},
		Source: EventSource{
			SourceRef: "fixture://" + eventID,
			EventHash: "sha256:" + eventID,
		},
	}
	if statusCode >= 400 {
		event.Severity = "error"
		event.Outcome.OutcomeStatus = OutcomeFailure
	} else {
		event.Outcome.OutcomeStatus = OutcomeSuccess
	}
	if costMicros != "" {
		event.Usage.CostMicros = costMicros
	}
	if totalTokens > 0 {
		event.Usage.TotalTokens = &totalTokens
	}
	return event
}

func hasClaim(claims []InvestigationClaim, code string) bool {
	for _, claim := range claims {
		if claim.Code == code {
			return true
		}
	}
	return false
}

func coverageStatus(coverage []StageCoverage, stage string) string {
	for _, item := range coverage {
		if item.Stage == stage {
			return item.Status
		}
	}
	return ""
}
