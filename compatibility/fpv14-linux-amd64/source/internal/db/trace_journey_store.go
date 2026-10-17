package db

import (
	"context"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/investigation"
	"github.com/foigent/x-insight/internal/workchain"
)

const traceJourneyEvidenceBoundary = "source-declared trace/span correlation across the same source, organization and project; chronology is not an inferred causal edge"

type traceJourneyAnchor struct {
	sourceSystemID string
	traceID        string
	orgRef         string
	projectRef     string
}

// GetTraceJourney projects a bounded, source-neutral trace graph anchored by
// an existing execution. It does not mutate Evidence or reinterpret request_id
// as trace identity.
func (s *EvidenceStore) getTraceJourney(
	ctx context.Context,
	executionID string,
	asOf time.Time,
	limit int,
) (investigation.TraceJourney, error) {
	journey := investigation.TraceJourney{
		SchemaVersion:     investigation.TraceJourneySchema,
		AnchorExecutionID: executionID,
		AsOf:              asOf.UTC(),
		Nodes:             []investigation.TraceJourneyNode{},
		Edges:             []investigation.TraceJourneyEdge{},
		EvidenceBoundary:  traceJourneyEvidenceBoundary,
	}
	if s == nil || s.db == nil {
		return journey, fmt.Errorf("evidence store is not initialized")
	}
	if limit < 1 || limit > 200 {
		return journey, fmt.Errorf("trace journey limit must be between 1 and 200")
	}

	anchorRows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT
  e.source_system_id::text,
  e.trace_id,
  COALESCE(e.org_ref, ''),
  COALESCE(e.project_ref, '')
FROM evidence_events e
WHERE e.execution_id = $1
  AND e.ingested_at <= $2
  AND e.created_at <= $2
  AND e.trace_id IS NOT NULL
  AND e.trace_id <> ''
  AND e.stage <> 'gap'
  AND e.event_type <> 'gap.capture.detected'
ORDER BY e.source_system_id::text, e.trace_id, COALESCE(e.org_ref, ''), COALESCE(e.project_ref, '')
LIMIT 2`, executionID, asOf.UTC())
	if err != nil {
		return journey, err
	}
	anchors := make([]traceJourneyAnchor, 0, 2)
	for anchorRows.Next() {
		var anchor traceJourneyAnchor
		if err := anchorRows.Scan(
			&anchor.sourceSystemID,
			&anchor.traceID,
			&anchor.orgRef,
			&anchor.projectRef,
		); err != nil {
			anchorRows.Close()
			return journey, err
		}
		anchors = append(anchors, anchor)
	}
	if err := anchorRows.Close(); err != nil {
		return journey, err
	}
	if len(anchors) == 0 {
		journey.UnavailableReason = "trace_identity_not_observed"
		return journey, nil
	}
	if len(anchors) != 1 {
		journey.UnavailableReason = "multiple_trace_identities_in_anchor_execution"
		return journey, nil
	}
	anchor := anchors[0]
	journey.SourceSystemID = anchor.sourceSystemID
	journey.TraceID = anchor.traceID

	rows, err := s.db.QueryContext(ctx, `
SELECT
  e.execution_id,
  e.id::text,
  e.request_id,
  e.source_type,
  e.source_ref,
  e.event_hash,
  e.trace_id,
  COALESCE(e.span_id, ''),
  COALESCE(e.correlation->>'parent_span_id', ''),
  COALESCE(e.identity->>'actor_type', ''),
  COALESCE(e.identity->>'actor_ref', ''),
  COALESCE(e.identity->>'org_id', ''),
  COALESCE(e.identity->>'project_id', ''),
  COALESCE(e.capability->>'target_ref', ''),
  COALESCE(e.capability->'extensions'->>'service_name', ''),
  COALESCE(e.capability->'extensions'->>'provider', e.capability->>'provider', ''),
  COALESCE(e.capability->>'model', ''),
  COALESCE(e.outcome->>'outcome_status', ''),
  COALESCE(e.occurred_at, e.observed_at, e.ingested_at) AS effective_at
FROM evidence_events e
WHERE e.source_system_id = $1::uuid
  AND e.trace_id = $2
  AND COALESCE(e.org_ref, '') = $3
  AND COALESCE(e.project_ref, '') = $4
  AND e.request_id IS NOT NULL
  AND e.request_id <> ''
  AND e.execution_id IS NOT NULL
  AND e.span_id IS NOT NULL
  AND e.span_id <> ''
  AND e.ingested_at <= $5
  AND e.created_at <= $5
  AND e.stage <> 'gap'
  AND e.event_type <> 'gap.capture.detected'
ORDER BY effective_at, e.ingested_at, e.id
LIMIT $6`,
		anchor.sourceSystemID,
		anchor.traceID,
		anchor.orgRef,
		anchor.projectRef,
		asOf.UTC(),
		limit+1,
	)
	if err != nil {
		return journey, err
	}
	defer rows.Close()
	for rows.Next() {
		var node investigation.TraceJourneyNode
		if err := rows.Scan(
			&node.ExecutionID,
			&node.EventID,
			&node.RequestID,
			&node.SourceType,
			&node.SourceRef,
			&node.EventHash,
			&node.TraceID,
			&node.SpanID,
			&node.ParentSpanID,
			&node.ActorType,
			&node.ActorRef,
			&node.OrgID,
			&node.ProjectID,
			&node.Action,
			&node.ServiceName,
			&node.Provider,
			&node.Model,
			&node.Outcome,
			&node.EffectiveAt,
		); err != nil {
			return journey, err
		}
		node.EffectiveAt = node.EffectiveAt.UTC()
		journey.Nodes = append(journey.Nodes, node)
	}
	if err := rows.Err(); err != nil {
		return journey, err
	}
	if len(journey.Nodes) > limit {
		journey.Nodes = journey.Nodes[:limit]
		journey.Integrity.Truncated = true
	}
	if len(journey.Nodes) == 0 {
		journey.UnavailableReason = "trace_events_not_visible_at_snapshot"
		return journey, nil
	}

	finalizeTraceJourney(&journey)
	return journey, nil
}

func finalizeTraceJourney(journey *investigation.TraceJourney) {
	if journey == nil {
		return
	}
	spanEvents := make(map[string][]string, len(journey.Nodes))
	executions := make(map[string]struct{}, len(journey.Nodes))
	for _, node := range journey.Nodes {
		executions[node.ExecutionID] = struct{}{}
		if node.SpanID != "" {
			spanEvents[node.SpanID] = append(spanEvents[node.SpanID], node.EventID)
		}
	}
	for _, node := range journey.Nodes {
		if node.ParentSpanID == "" {
			continue
		}
		journey.Integrity.DeclaredParentCount++
		parents := spanEvents[node.ParentSpanID]
		switch len(parents) {
		case 0:
			journey.Integrity.UnresolvedParentCount++
		case 1:
			journey.Edges = append(journey.Edges, investigation.TraceJourneyEdge{
				FromEventID: parents[0],
				ToEventID:   node.EventID,
				EdgeBasis:   "source_declared_parent_span_across_execution",
			})
			journey.Integrity.ResolvedParentCount++
		default:
			journey.Integrity.AmbiguousParentCount++
		}
	}
	journey.Available = true
	journey.Integrity.NodeCount = len(journey.Nodes)
	journey.Integrity.ExecutionCount = len(executions)
	journey.Integrity.CycleDetected = traceJourneyHasCycle(journey.Edges)
	journey.Integrity.SourceTimelineComplete = false
	journey.Integrity.VisibleParentResolutionComplete =
		journey.Integrity.DeclaredParentCount > 0 &&
			journey.Integrity.UnresolvedParentCount == 0 &&
			journey.Integrity.AmbiguousParentCount == 0 &&
			!journey.Integrity.CycleDetected &&
			!journey.Integrity.Truncated
}

func traceJourneyHasCycle(edges []investigation.TraceJourneyEdge) bool {
	adjacency := make(map[string][]string, len(edges))
	for _, edge := range edges {
		adjacency[edge.FromEventID] = append(adjacency[edge.FromEventID], edge.ToEventID)
	}
	states := make(map[string]uint8, len(adjacency))
	var visit func(string) bool
	visit = func(eventID string) bool {
		switch states[eventID] {
		case 1:
			return true
		case 2:
			return false
		}
		states[eventID] = 1
		for _, child := range adjacency[eventID] {
			if visit(child) {
				return true
			}
		}
		states[eventID] = 2
		return false
	}
	for eventID := range adjacency {
		if visit(eventID) {
			return true
		}
	}
	return false
}

func (s *EvidenceStore) GetTraceJourney(ctx context.Context, id string, at time.Time, limit int) (investigation.TraceJourney, error) {
	if err := s.checkScopedExecution(ctx, id); err != nil {
		return investigation.TraceJourney{}, err
	}
	j, err := s.getTraceJourney(ctx, id, at, limit)
	if err != nil || workchain.FromContext(ctx) == nil {
		return j, err
	}
	denied := func() investigation.TraceJourney {
		return investigation.TraceJourney{SchemaVersion: j.SchemaVersion, AnchorExecutionID: id, AsOf: j.AsOf, Nodes: []investigation.TraceJourneyNode{}, Edges: []investigation.TraceJourneyEdge{}, UnavailableReason: "authorized_view_unavailable", EvidenceBoundary: "The current authorization cannot expose the complete declared journey."}
	}
	// The look-ahead row has not been authorized. Do not expose its existence
	// through a truncated count or return a graph whose complete scope is unknown.
	if j.Integrity.Truncated {
		return denied(), nil
	}
	req, err := s.AuthorizeScopedExecution(ctx, id)
	if err != nil {
		return denied(), err
	}
	if !req.Fields.Context {
		return denied(), nil
	}
	fields := map[string]workchain.Fields{id: req.Fields}
	for i := range j.Nodes {
		n := &j.Nodes[i]
		f, ok := fields[n.ExecutionID]
		if !ok {
			r, e := s.AuthorizeScopedExecution(ctx, n.ExecutionID)
			if e != nil {
				return denied(), nil
			}
			f = r.Fields
			fields[n.ExecutionID] = f
		}
		if !f.Context {
			return denied(), nil
		}
		if !f.Identity {
			n.ActorType = ""
			n.ActorRef = ""
			n.OrgID = ""
			n.ProjectID = ""
		}
		if !f.Complete() {
			n.SourceRef = ""
			n.Action = ""
			n.ServiceName = ""
		}
	}
	return j, nil
}
