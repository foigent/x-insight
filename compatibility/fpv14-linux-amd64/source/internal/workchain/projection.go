package workchain

import "github.com/foigent/x-insight/internal/investigation"

func Boundary(f Fields) *investigation.AccessProjection {
	hidden := []string{"raw_payload", "unclassified_extensions"}
	if !f.Context {
		hidden = append(hidden, "context")
	}
	if !f.Identity {
		hidden = append(hidden, "identity")
	}
	if !f.Cost {
		hidden = append(hidden, "source_cost")
	}
	if !f.Complete() {
		hidden = append(hidden, "free_text")
	}
	return &investigation.AccessProjection{SchemaVersion: ProjectionVersion, Representation: "authorized_projection", Withheld: hidden}
}

// The original event and its hash are untouched. This is a separate view;
// callers must never hash the projected JSON and call it the original event.
func ProjectEvent(e investigation.EvidenceEventDetail, f Fields) investigation.EvidenceEventDetail {
	// Construct the supported view explicitly: future source fields must be
	// classified here before they can become visible to scoped accounts.
	e = investigation.EvidenceEventDetail{
		ExecutionID: e.ExecutionID, EventID: e.EventID, EventType: e.EventType, Stage: e.Stage, Severity: e.Severity,
		CaptureMode: e.CaptureMode, OccurredAt: e.OccurredAt, ObservedAt: e.ObservedAt, IngestedAt: e.IngestedAt, PayloadCaptureMode: e.PayloadCaptureMode,
		Correlation:     investigation.CorrelationGroup{RequestID: e.Correlation.RequestID, TraceID: e.Correlation.TraceID, SpanID: e.Correlation.SpanID, ParentSpanID: e.Correlation.ParentSpanID, RunID: e.Correlation.RunID, SessionID: e.Correlation.SessionID, UpstreamRequestID: e.Correlation.UpstreamRequestID},
		Identity:        investigation.IdentityGroup{ActorType: e.Identity.ActorType, ActorRef: e.Identity.ActorRef, OwnerRef: e.Identity.OwnerRef, OrgID: e.Identity.OrgID, ProjectID: e.Identity.ProjectID, TeamRef: e.Identity.TeamRef, APIKeyRef: e.Identity.APIKeyRef, EndUserRef: e.Identity.EndUserRef, KeyType: e.Identity.KeyType},
		Capability:      investigation.CapabilityGroup{CapabilityType: e.Capability.CapabilityType, TargetRef: e.Capability.TargetRef, Model: e.Capability.Model, RequestedModel: e.Capability.RequestedModel, Provider: e.Capability.Provider, Channel: e.Capability.Channel},
		Decision:        investigation.DecisionGroup{DecisionKind: e.Decision.DecisionKind, DecisionResult: e.Decision.DecisionResult, DecisionReason: e.Decision.DecisionReason, PolicyRef: e.Decision.PolicyRef, RuleRef: e.Decision.RuleRef},
		Usage:           investigation.UsageGroup{PromptTokens: e.Usage.PromptTokens, CompletionTokens: e.Usage.CompletionTokens, TotalTokens: e.Usage.TotalTokens, EstimatedTokens: e.Usage.EstimatedTokens, CostCents: e.Usage.CostCents, CostMicros: e.Usage.CostMicros, Currency: e.Usage.Currency, AmountScale: e.Usage.AmountScale, MoneySchema: e.Usage.MoneySchema, LatencyMS: e.Usage.LatencyMS, TTFTMS: e.Usage.TTFTMS, UsageSource: e.Usage.UsageSource, UsageConfidence: e.Usage.UsageConfidence},
		Outcome:         investigation.OutcomeGroup{OutcomeStatus: e.Outcome.OutcomeStatus, StatusCode: e.Outcome.StatusCode, ErrorCode: e.Outcome.ErrorCode, FailureKind: e.Outcome.FailureKind},
		PayloadPolicy:   investigation.PayloadPolicyGroup{PayloadCaptureMode: e.PayloadPolicy.PayloadCaptureMode, RedactionState: e.PayloadPolicy.RedactionState, RetentionPolicyRef: e.PayloadPolicy.RetentionPolicyRef},
		FieldProvenance: projectProvenance(e.FieldProvenance),
		Source:          investigation.EventSource{SourceSystemID: e.Source.SourceSystemID, AdapterManifestID: e.Source.AdapterManifestID, SchemaVersion: e.Source.SchemaVersion, SourceType: e.Source.SourceType, SourceVersion: e.Source.SourceVersion, SourceEventID: e.Source.SourceEventID, SourceEventType: e.Source.SourceEventType, SourceRef: e.Source.SourceRef, EventHash: e.Source.EventHash, SourceEventHash: e.Source.SourceEventHash, FidelityLevel: e.Source.FidelityLevel},
	}
	e.ViewBoundary = Boundary(f)
	e.PayloadPolicy.PayloadRef = ""
	if !f.Context {
		e.Correlation = investigation.CorrelationGroup{}
		e.Capability = investigation.CapabilityGroup{Model: e.Capability.Model}
		e.Decision = investigation.DecisionGroup{}
		e.PayloadPolicy = investigation.PayloadPolicyGroup{PayloadCaptureMode: e.PayloadCaptureMode}
		e.Source.SourceRef = ""
		e.Source.SourceEventID = ""
		e.Source.SourceEventType = ""
		e.FieldProvenance = investigation.FieldProvenance{}
	}
	if !f.Identity {
		e.Identity = investigation.IdentityGroup{}
	}
	if !f.Cost {
		e.Usage = investigation.UsageGroup{LatencyMS: e.Usage.LatencyMS, TTFTMS: e.Usage.TTFTMS}
	}
	if !f.Complete() {
		e.Capability.TargetRef = ""
		e.Capability.Channel = ""
		e.PayloadPolicy.RetentionPolicyRef = ""
		e.Source.SourceRef = ""
		e.Source.SourceEventID = ""
		e.Decision.DecisionReason = ""
		e.Decision.PolicyRef = ""
		e.Decision.RuleRef = ""
		e.Outcome.ErrorCode = ""
		e.Outcome.FailureKind = ""
	}
	return e
}

func ProjectTimeline(n investigation.TimelineNode, f Fields) investigation.TimelineNode {
	n = investigation.TimelineNode{EventID: n.EventID, EventType: n.EventType, Stage: n.Stage, Severity: n.Severity, EffectiveAt: n.EffectiveAt, OccurredAt: n.OccurredAt, ObservedAt: n.ObservedAt, IngestedAt: n.IngestedAt, DeclaredParentEventID: n.DeclaredParentEventID, DeclaredParentResolved: n.DeclaredParentResolved, SourceRef: n.SourceRef, SourceType: n.SourceType, DecisionResult: n.DecisionResult, OutcomeStatus: n.OutcomeStatus, FieldProvenance: projectProvenance(n.FieldProvenance)}
	n.ViewBoundary = Boundary(f)
	if !f.Complete() {
		n.SourceRef = ""
	}
	if !f.Context {
		n.SourceRef = ""
		n.DeclaredParentEventID = ""
		n.DeclaredParentResolved = nil
		n.FieldProvenance = investigation.FieldProvenance{}
	}
	return n
}

func ProjectGap(g investigation.CaptureGap, f Fields) investigation.CaptureGap {
	g = investigation.CaptureGap{GapID: g.GapID, EvidenceEventID: g.EvidenceEventID, GapType: g.GapType, CandidateGapTypes: g.CandidateGapTypes, AffectedClaim: g.AffectedClaim, GapReason: g.GapReason, Severity: g.Severity, Recommendation: g.Recommendation, CreatedAt: g.CreatedAt, ResolutionState: g.ResolutionState, ResolutionNote: g.ResolutionNote, ResolutionUpdatedAt: g.ResolutionUpdatedAt}
	g.ViewBoundary = Boundary(f)
	if !f.Complete() {
		g.AffectedClaim = ""
		g.GapReason = ""
		g.Recommendation = ""
		g.ResolutionNote = ""
	}
	return g
}

func ProjectSummary(s investigation.ExecutionSummary, f Fields) investigation.ExecutionSummary {
	s = investigation.ExecutionSummary{ExecutionID: s.ExecutionID, RequestID: s.RequestID, SourceSystemID: s.SourceSystemID, SourceType: s.SourceType, SourceDisplayName: s.SourceDisplayName, Outcome: s.Outcome, FirstEffectiveAt: s.FirstEffectiveAt, LastEffectiveAt: s.LastEffectiveAt, EventCount: s.EventCount, GapCount: s.GapCount, HasGaps: s.HasGaps, PayloadCaptureModes: s.PayloadCaptureModes, ProofClass: s.ProofClass, AssuranceMode: s.AssuranceMode, Freshness: investigation.Freshness{IntegrationMode: s.Freshness.IntegrationMode, SyncState: s.Freshness.SyncState, TimelineComplete: s.Freshness.TimelineComplete, SourceTerminalAt: s.Freshness.SourceTerminalAt, SourceGeneratedAt: s.Freshness.SourceGeneratedAt, ImportedAt: s.Freshness.ImportedAt, SearchVisibleAt: s.Freshness.SearchVisibleAt}}
	s.ViewBoundary = Boundary(f)
	// Import revisions/watermarks are operational source strings, not evidence.
	s.Freshness.SourceRevision = ""
	s.Freshness.ImportedRevision = ""
	s.Freshness.SourceWatermark = ""
	s.Freshness.ImportWatermark = ""
	return s
}

func projectProvenance(p investigation.FieldProvenance) investigation.FieldProvenance {
	return investigation.FieldProvenance{Correlation: p.Correlation, Identity: p.Identity, Capability: p.Capability, Decision: p.Decision, Usage: p.Usage, Outcome: p.Outcome, PayloadPolicy: p.PayloadPolicy}
}
