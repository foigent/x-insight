package investigation

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	maxProjectionNodes = 200
	maxProjectionGaps  = 100
)

var investigationStages = [...]string{
	"identity",
	"routing",
	"provider",
	"retry_fallback",
	"billing",
	"usage",
	"logging",
}

type ProjectionInput struct {
	Detail          ExecutionDetail
	Timeline        []TimelineNode
	Events          []EvidenceEventDetail
	TotalEventCount int
	Gaps            []CaptureGap
	TotalGapCount   int
}

func BuildProjection(input ProjectionInput) InvestigationProjection {
	timeline := append([]TimelineNode(nil), input.Timeline...)
	events := append([]EvidenceEventDetail(nil), input.Events...)
	gaps := append(make([]CaptureGap, 0, len(input.Gaps)), input.Gaps...)
	truncated := input.TotalEventCount > maxProjectionNodes || input.TotalGapCount > maxProjectionGaps
	if len(timeline) > maxProjectionNodes {
		timeline = timeline[:maxProjectionNodes]
		truncated = true
	}
	if len(events) > maxProjectionNodes {
		events = events[:maxProjectionNodes]
		truncated = true
	}
	if len(gaps) > maxProjectionGaps {
		gaps = gaps[:maxProjectionGaps]
		truncated = true
	}

	eventByID := make(map[string]EvidenceEventDetail, len(events))
	eventViewsMatch := len(timeline) == len(events)
	for _, event := range events {
		if _, duplicate := eventByID[event.EventID]; duplicate {
			eventViewsMatch = false
		}
		eventByID[event.EventID] = event
	}

	displayIDs := make(map[string]struct{}, len(timeline))
	timelineIDs := make(map[string]struct{}, len(timeline))
	for _, timelineNode := range timeline {
		if _, duplicate := timelineIDs[timelineNode.EventID]; duplicate {
			eventViewsMatch = false
		}
		timelineIDs[timelineNode.EventID] = struct{}{}
		event, exists := eventByID[timelineNode.EventID]
		if !exists {
			eventViewsMatch = false
			continue
		}
		if isCaptureGapEvent(timelineNode, event) {
			continue
		}
		displayIDs[timelineNode.EventID] = struct{}{}
	}
	if eventViewsMatch {
		for _, event := range events {
			if _, exists := timelineIDs[event.EventID]; !exists {
				eventViewsMatch = false
				break
			}
		}
	}

	nodes := make([]InvestigationNode, 0, len(timeline))
	edges := make([]InvestigationEdge, 0, len(timeline))
	parentEligible := 0
	for _, timelineNode := range timeline {
		event, exists := eventByID[timelineNode.EventID]
		if !exists || isCaptureGapEvent(timelineNode, event) {
			continue
		}
		node := projectionNode(
			timelineNode,
			event,
			input.Detail.Summary.ExecutionID,
			input.Detail.AsOf,
		)
		nodes = append(nodes, node)
		if timelineNode.DeclaredParentResolved != nil {
			parentEligible++
		}
		if timelineNode.DeclaredParentResolved != nil &&
			*timelineNode.DeclaredParentResolved &&
			timelineNode.DeclaredParentEventID != "" {
			if _, parentVisible := displayIDs[timelineNode.DeclaredParentEventID]; !parentVisible {
				continue
			}
			edges = append(edges, InvestigationEdge{
				From:      timelineNode.DeclaredParentEventID,
				To:        timelineNode.EventID,
				EdgeBasis: "source_declared_parent_span",
			})
		}
	}

	projectionComplete := !truncated &&
		input.TotalEventCount == len(timeline) &&
		input.TotalEventCount == len(events) &&
		input.TotalGapCount == len(gaps) &&
		eventViewsMatch
	sourceComplete := input.Detail.Summary.Freshness.TimelineComplete
	importComplete := input.Detail.Summary.Freshness.SyncState == SyncStateCurrent
	unresolvedParents := parentEligible - len(edges)
	causalComplete := parentEligible > 0 && unresolvedParents == 0
	presentationMode := PresentationTerminalSnapshot
	if len(edges) > 0 {
		presentationMode = PresentationCausalTrace
	} else if hasOperationalChronology(nodes) {
		presentationMode = PresentationChronology
	}

	return InvestigationProjection{
		SchemaVersion:    InvestigationProjectionSchema,
		Execution:        input.Detail.Summary,
		AsOf:             input.Detail.AsOf.UTC(),
		PresentationMode: presentationMode,
		SummaryClaims:    buildClaims(nodes, projectionComplete && sourceComplete && importComplete),
		StageCoverage:    buildCoverage(nodes, gaps, sourceComplete, projectionComplete),
		Nodes:            nodes,
		Edges:            edges,
		Gaps:             gaps,
		Freshness:        input.Detail.Summary.Freshness,
		Integrity: InvestigationIntegrity{
			SourceProfileComplete: sourceComplete,
			ImportComplete:        importComplete,
			ProjectionComplete:    projectionComplete,
			NodeCount:             len(nodes),
			TotalEventCount:       input.TotalEventCount,
			ResolvedParentCount:   len(edges),
			ParentEligibleCount:   parentEligible,
			UnresolvedParentCount: unresolvedParents,
			CausalComplete:        causalComplete,
			Truncated:             truncated,
		},
		Completeness: buildCompleteness(
			sourceComplete,
			importComplete,
			projectionComplete,
			parentEligible,
			unresolvedParents,
		),
		UnprovableActions: buildUnprovableActions(
			gaps,
			sourceComplete,
			importComplete,
			projectionComplete,
			parentEligible,
			unresolvedParents,
		),
	}
}

func projectionNode(
	timeline TimelineNode,
	event EvidenceEventDetail,
	executionID string,
	asOf time.Time,
) InvestigationNode {
	return InvestigationNode{
		EventID:               timeline.EventID,
		EventType:             timeline.EventType,
		SourceEventType:       event.Source.SourceEventType,
		Kind:                  classifyNode(timeline.EventType, timeline.Stage, event),
		ProofBasis:            "direct",
		Stage:                 timeline.Stage,
		Severity:              timeline.Severity,
		EffectiveAt:           timeline.EffectiveAt.UTC(),
		DeclaredParentEventID: timeline.DeclaredParentEventID,
		ActorType:             event.Identity.ActorType,
		ActorRef:              event.Identity.ActorRef,
		OrgID:                 event.Identity.OrgID,
		ProjectID:             event.Identity.ProjectID,
		Provider:              event.Capability.Provider,
		Model:                 event.Capability.Model,
		RequestedModel:        event.Capability.RequestedModel,
		DecisionKind:          event.Decision.DecisionKind,
		DecisionResult:        event.Decision.DecisionResult,
		DecisionReason:        event.Decision.DecisionReason,
		OutcomeStatus:         event.Outcome.OutcomeStatus,
		StatusCode:            event.Outcome.StatusCode,
		LatencyMS:             event.Usage.LatencyMS,
		TotalTokens:           event.Usage.TotalTokens,
		CostMicros:            event.Usage.CostMicros,
		Citation: EvidenceCitation{
			EvidenceEventID: timeline.EventID,
			SourceRef:       event.Source.SourceRef,
			EventHash:       event.Source.EventHash,
			DeepLink: fmt.Sprintf(
				"/executions/%s?as_of=%s&evidence=%s",
				url.PathEscape(executionID),
				url.QueryEscape(asOf.UTC().Format(time.RFC3339Nano)),
				url.QueryEscape(timeline.EventID),
			),
		},
	}
}

func isCaptureGapEvent(timeline TimelineNode, event EvidenceEventDetail) bool {
	return timeline.Stage == "gap" ||
		timeline.EventType == "gap.capture.detected" ||
		event.Stage == "gap" ||
		event.EventType == "gap.capture.detected"
}

func classifyNode(eventType, stage string, event EvidenceEventDetail) string {
	switch eventType {
	case "fallback.route.selected":
		return "fallback_selected"
	case "retry.attempt.scheduled":
		return "retry_scheduled"
	case "provider.attempt.failed", "provider.request.failed", "provider.request_failed":
		if isFailure(event) {
			return "provider_failed"
		}
		return "evidence_observed"
	case "provider.attempt.succeeded", "provider.request.succeeded", "provider.request_succeeded":
		if isSuccess(event) {
			return "provider_succeeded"
		}
		return "evidence_observed"
	case "routing.route.selected", "routing.route_selected":
		return "route_selected"
	case "billing.settle.succeeded", "usage.cost.recorded":
		return "billing_recorded"
	case "usage.tokens.recorded", "gateway.call.completed", "model.request.completed":
		return "execution_completed"
	}
	switch {
	case event.Source.SourceEventType == "call_log":
		return "execution_completed"
	case stage == "identity" || stage == "auth":
		return "identity_bound"
	default:
		return "evidence_observed"
	}
}

func buildClaims(nodes []InvestigationNode, complete bool) []InvestigationClaim {
	claims := make([]InvestigationClaim, 0, 3)
	failed, retry := firstFailureRetryPair(nodes)
	if failed != nil && retry != nil {
		claims = append(claims, InvestigationClaim{
			Code:             "provider_failed_then_retry_scheduled",
			EvidenceEventIDs: []string{failed.EventID, retry.EventID},
			DerivationBasis:  "deterministic_chronology_not_causal_edge",
		})
	}
	recoveredFailed, recoveredRetry, fallback, recoveredSuccess := firstRecoveredFallbackChain(nodes)
	if complete &&
		recoveredFailed != nil &&
		recoveredRetry != nil &&
		fallback != nil &&
		recoveredSuccess != nil {
		claims = append(claims, InvestigationClaim{
			Code: "primary_provider_failed_then_fallback_succeeded",
			EvidenceEventIDs: []string{
				recoveredFailed.EventID,
				recoveredRetry.EventID,
				fallback.EventID,
				recoveredSuccess.EventID,
			},
			DerivationBasis: "deterministic_complete_chronology_provider_match_not_causal_edge",
		})
	}
	success := lastSuccessfulNode(nodes)
	if complete && success != nil {
		claims = append(claims, InvestigationClaim{
			Code:             "execution_completed_successfully",
			EvidenceEventIDs: []string{success.EventID},
			DerivationBasis:  "deterministic_terminal_evidence",
		})
	}
	if claims == nil {
		return []InvestigationClaim{}
	}
	return claims
}

func buildCompleteness(
	sourceComplete bool,
	importComplete bool,
	projectionComplete bool,
	parentEligible int,
	unresolvedParents int,
) []CompletenessAxis {
	causalStatus := "not_available"
	causalBasis := "source_profile_declared_no_causal_edges"
	switch {
	case parentEligible > 0 && unresolvedParents == 0:
		causalStatus = "complete"
		causalBasis = "all_source_declared_parents_resolved"
	case unresolvedParents > 0:
		causalStatus = "incomplete"
		causalBasis = "source_declared_parent_unresolved"
	}
	return []CompletenessAxis{
		{
			Axis: "source", Status: completenessStatus(sourceComplete),
			Basis: boolBasis(sourceComplete, "source_profile_declares_timeline_complete", "source_profile_declares_partial_or_unknown"),
		},
		{
			Axis: "import", Status: completenessStatus(importComplete),
			Basis: boolBasis(importComplete, "connector_checkpoint_is_current", "connector_checkpoint_not_current"),
		},
		{
			Axis: "projection", Status: completenessStatus(projectionComplete),
			Basis: boolBasis(projectionComplete, "bounded_projection_contains_all_imported_records", "projection_truncated_or_count_mismatch"),
		},
		{Axis: "causal", Status: causalStatus, Basis: causalBasis},
	}
}

func buildUnprovableActions(
	gaps []CaptureGap,
	sourceComplete bool,
	importComplete bool,
	projectionComplete bool,
	parentEligible int,
	unresolvedParents int,
) []UnprovableAction {
	items := make([]UnprovableAction, 0, len(gaps)+4)
	if !sourceComplete {
		items = append(items, UnprovableAction{
			Code:      "source_path_incomplete",
			Statement: "来源未声明完整时间线，无法证明端到端执行路径完整。",
		})
	}
	if !importComplete {
		items = append(items, UnprovableAction{
			Code:      "import_not_current",
			Statement: "Connector checkpoint 不是 current，无法证明当前导入已追平来源。",
		})
	}
	if !projectionComplete {
		items = append(items, UnprovableAction{
			Code:      "projection_bounded",
			Statement: "当前投影被截断或计数不一致，无法证明所有已导入 Evidence 均已展示。",
		})
	}
	if parentEligible == 0 {
		items = append(items, UnprovableAction{
			Code:      "causal_edges_not_available",
			Statement: "来源未提供可用的父子关系，无法从时间顺序证明因果路径。",
		})
	} else if unresolvedParents > 0 {
		items = append(items, UnprovableAction{
			Code:      "causal_parent_unresolved",
			Statement: "存在未解析的来源声明父节点，无法证明相关分支的完整因果路径。",
		})
	}
	for _, gap := range gaps {
		items = append(items, UnprovableAction{
			Code:      "capture_gap_limits_claim",
			Statement: "Capture Gap 限制声明：" + gap.AffectedClaim,
			GapIDs:    []string{gap.GapID},
		})
	}
	if items == nil {
		return []UnprovableAction{}
	}
	return items
}

func completenessStatus(complete bool) string {
	if complete {
		return "complete"
	}
	return "incomplete"
}

func boolBasis(value bool, whenTrue string, whenFalse string) string {
	if value {
		return whenTrue
	}
	return whenFalse
}

func firstFailureRetryPair(nodes []InvestigationNode) (*InvestigationNode, *InvestigationNode) {
	for failedIndex := range nodes {
		if nodes[failedIndex].Kind != "provider_failed" {
			continue
		}
		for retryIndex := failedIndex + 1; retryIndex < len(nodes); retryIndex++ {
			if nodes[retryIndex].Kind == "retry_scheduled" &&
				!nodes[retryIndex].EffectiveAt.Before(nodes[failedIndex].EffectiveAt) {
				return &nodes[failedIndex], &nodes[retryIndex]
			}
		}
	}
	return nil, nil
}

func firstRecoveredFallbackChain(
	nodes []InvestigationNode,
) (*InvestigationNode, *InvestigationNode, *InvestigationNode, *InvestigationNode) {
	for failedIndex := range nodes {
		failed := &nodes[failedIndex]
		if failed.Kind != "provider_failed" || failed.Provider == "" {
			continue
		}
		for retryIndex := failedIndex + 1; retryIndex < len(nodes); retryIndex++ {
			retry := &nodes[retryIndex]
			if retry.Kind != "retry_scheduled" || retry.EffectiveAt.Before(failed.EffectiveAt) {
				continue
			}
			for fallbackIndex := retryIndex + 1; fallbackIndex < len(nodes); fallbackIndex++ {
				fallback := &nodes[fallbackIndex]
				if fallback.Kind != "fallback_selected" ||
					fallback.Provider == "" ||
					fallback.Provider == failed.Provider ||
					fallback.EffectiveAt.Before(retry.EffectiveAt) {
					continue
				}
				for successIndex := fallbackIndex + 1; successIndex < len(nodes); successIndex++ {
					success := &nodes[successIndex]
					if isSuccessfulNode(*success) &&
						success.Provider == fallback.Provider &&
						!success.EffectiveAt.Before(fallback.EffectiveAt) {
						return failed, retry, fallback, success
					}
				}
			}
		}
	}
	return nil, nil, nil, nil
}

func lastSuccessfulNode(nodes []InvestigationNode) *InvestigationNode {
	var success *InvestigationNode
	for index := range nodes {
		if isSuccessfulNode(nodes[index]) {
			success = &nodes[index]
		}
	}
	return success
}

func buildCoverage(
	nodes []InvestigationNode,
	gaps []CaptureGap,
	sourceComplete bool,
	projectionComplete bool,
) []StageCoverage {
	evidenceByStage := make(map[string][]string, len(investigationStages))
	for _, node := range nodes {
		for _, stage := range coverageStagesForNode(node) {
			evidenceByStage[stage] = appendUnique(evidenceByStage[stage], node.EventID)
		}
	}
	gapsByStage := make(map[string][]string, len(investigationStages))
	for _, gap := range gaps {
		for _, stage := range coverageStagesForGap(gap) {
			gapsByStage[stage] = appendUnique(gapsByStage[stage], gap.GapID)
		}
	}
	coverage := make([]StageCoverage, 0, len(investigationStages))
	for _, stage := range investigationStages {
		status := CoverageUnknown
		switch {
		case len(evidenceByStage[stage]) > 0:
			status = CoveragePresent
		case len(gapsByStage[stage]) > 0:
			status = CoverageGap
		case sourceComplete && projectionComplete && stage == "retry_fallback" && !hasProviderFailure(nodes):
			status = CoverageNotApplicable
		}
		coverage = append(coverage, StageCoverage{
			Stage:            stage,
			Status:           status,
			EvidenceEventIDs: evidenceByStage[stage],
			GapIDs:           gapsByStage[stage],
		})
	}
	return coverage
}

func coverageStagesForNode(node InvestigationNode) []string {
	stages := make([]string, 0, 3)
	if node.ActorType != "" || node.ActorRef != "" || node.OrgID != "" || node.ProjectID != "" {
		stages = append(stages, "identity")
	}
	switch {
	case node.Stage == "identity" || node.Stage == "auth":
		stages = append(stages, "identity")
	case node.Stage == "routing":
		stages = append(stages, "routing")
	case node.Stage == "provider":
		stages = append(stages, "provider")
	case node.Stage == "billing":
		stages = append(stages, "billing")
	case node.Stage == "usage":
		stages = append(stages, "usage")
	case node.Stage == "logging":
		stages = append(stages, "logging")
	}
	switch node.Kind {
	case "route_selected":
		stages = appendUnique(stages, "routing")
	case "provider_failed", "provider_succeeded":
		stages = appendUnique(stages, "provider")
	case "retry_scheduled", "fallback_selected":
		stages = appendUnique(stages, "retry_fallback")
	case "billing_recorded":
		stages = appendUnique(stages, "billing")
	case "execution_completed":
		stages = appendUnique(stages, "usage")
		if node.Stage == "logging" ||
			node.SourceEventType == "call_log" ||
			strings.Contains(node.Citation.SourceRef, ":call_logs:") {
			stages = appendUnique(stages, "logging")
		}
	}
	if node.CostMicros != "" {
		stages = appendUnique(stages, "billing")
	}
	if node.TotalTokens != nil {
		stages = appendUnique(stages, "usage")
	}
	if node.Citation.SourceRef != "" && strings.Contains(node.Citation.SourceRef, ":call_logs:") {
		stages = appendUnique(stages, "logging")
	}
	return stages
}

func coverageStagesForGap(gap CaptureGap) []string {
	text := strings.ToLower(strings.Join([]string{
		gap.GapType,
		gap.AffectedClaim,
		gap.GapReason,
	}, " "))
	stages := make([]string, 0, 2)
	for _, candidate := range investigationStages {
		tokens := []string{candidate}
		switch candidate {
		case "identity":
			tokens = append(tokens, "auth", "actor", "api_key")
		case "routing":
			tokens = append(tokens, "route")
		case "provider":
			tokens = append(tokens, "upstream")
		case "retry_fallback":
			tokens = append(tokens, "retry", "fallback", "failover")
		case "billing":
			tokens = append(tokens, "money", "cost")
		case "usage":
			tokens = append(tokens, "token")
		case "logging":
			tokens = append(tokens, "call_log", "request_trace")
		}
		for _, token := range tokens {
			if strings.Contains(text, token) {
				stages = appendUnique(stages, candidate)
				break
			}
		}
	}
	return stages
}

func hasOperationalChronology(nodes []InvestigationNode) bool {
	if len(nodes) < 2 {
		return false
	}
	for _, node := range nodes {
		switch node.Kind {
		case "route_selected", "provider_failed", "provider_succeeded", "retry_scheduled", "fallback_selected":
			return true
		}
	}
	return false
}

func hasProviderFailure(nodes []InvestigationNode) bool {
	for _, node := range nodes {
		if node.Kind == "provider_failed" {
			return true
		}
	}
	return false
}

func isFailure(event EvidenceEventDetail) bool {
	return event.Outcome.OutcomeStatus == OutcomeFailure ||
		(event.Outcome.StatusCode != nil && *event.Outcome.StatusCode >= 400) ||
		event.Severity == "error"
}

func isSuccess(event EvidenceEventDetail) bool {
	return event.Outcome.OutcomeStatus == OutcomeSuccess ||
		(event.Outcome.StatusCode != nil && *event.Outcome.StatusCode >= 200 && *event.Outcome.StatusCode < 400)
}

func isSuccessfulNode(node InvestigationNode) bool {
	if node.Kind != "execution_completed" && node.Kind != "provider_succeeded" {
		return false
	}
	return node.OutcomeStatus == OutcomeSuccess ||
		(node.StatusCode != nil && *node.StatusCode >= 200 && *node.StatusCode < 400)
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
