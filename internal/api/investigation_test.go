package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/investigation"
)

type fakeInvestigationStore struct {
	*fakeStore
	summaries []investigation.ExecutionSummary
	timeline  map[string][]investigation.TimelineNode
	gaps      map[string][]investigation.CaptureGap
	events    map[string]investigation.EvidenceEventDetail
	journeys  map[string]investigation.TraceJourney
	sources   []investigation.SourceOption
}

func (s *fakeInvestigationStore) GetTraceJourney(
	_ context.Context,
	executionID string,
	asOf time.Time,
	_ int,
) (investigation.TraceJourney, error) {
	if journey, ok := s.journeys[executionID]; ok {
		journey.AsOf = asOf.UTC()
		return journey, nil
	}
	for _, summary := range s.summaries {
		if summary.ExecutionID == executionID {
			return investigation.TraceJourney{
				SchemaVersion:     investigation.TraceJourneySchema,
				AnchorExecutionID: executionID,
				AsOf:              asOf.UTC(),
				Nodes:             []investigation.TraceJourneyNode{},
				Edges:             []investigation.TraceJourneyEdge{},
				UnavailableReason: "trace_identity_not_observed",
				EvidenceBoundary:  "test source-declared trace boundary",
			}, nil
		}
	}
	return investigation.TraceJourney{}, db.ErrExecutionNotFound
}

func (s *fakeInvestigationStore) SearchExecutions(_ context.Context, query investigation.SearchQuery) ([]investigation.ExecutionSummary, error) {
	start := 0
	if query.After != nil {
		start = len(s.summaries)
		for index, summary := range s.summaries {
			if summary.ExecutionID == query.After.ExecutionID {
				start = index + 1
				break
			}
		}
	}
	end := start + query.Limit
	if end > len(s.summaries) {
		end = len(s.summaries)
	}
	return append([]investigation.ExecutionSummary(nil), s.summaries[start:end]...), nil
}

func (s *fakeInvestigationStore) GetExecution(_ context.Context, executionID string, asOf time.Time) (investigation.ExecutionDetail, error) {
	for _, summary := range s.summaries {
		if summary.ExecutionID == executionID {
			return investigation.ExecutionDetail{Summary: summary, AsOf: asOf}, nil
		}
	}
	return investigation.ExecutionDetail{}, db.ErrExecutionNotFound
}

func (s *fakeInvestigationStore) ListTimeline(_ context.Context, query investigation.TimelineQuery) ([]investigation.TimelineNode, int, error) {
	items, ok := s.timeline[query.ExecutionID]
	if !ok {
		return nil, 0, db.ErrExecutionNotFound
	}
	start := 0
	if query.After != nil {
		start = len(items)
		for index, item := range items {
			if item.EventID == query.After.EventID {
				start = index + 1
				break
			}
		}
	}
	end := start + query.Limit
	if end > len(items) {
		end = len(items)
	}
	return append([]investigation.TimelineNode(nil), items[start:end]...), len(items), nil
}

func (s *fakeInvestigationStore) ListExecutionEvents(
	_ context.Context,
	executionID string,
	_ time.Time,
	limit int,
) ([]investigation.EvidenceEventDetail, int, error) {
	timeline, ok := s.timeline[executionID]
	if !ok {
		return nil, 0, db.ErrExecutionNotFound
	}
	if limit > len(timeline) {
		limit = len(timeline)
	}
	items := make([]investigation.EvidenceEventDetail, 0, limit)
	for _, node := range timeline[:limit] {
		event, exists := s.events[executionID+"/"+node.EventID]
		if !exists {
			event = investigation.EvidenceEventDetail{
				ExecutionID: executionID,
				EventID:     node.EventID,
				EventType:   node.EventType,
				Stage:       node.Stage,
				Severity:    node.Severity,
				IngestedAt:  node.IngestedAt,
				Outcome: investigation.OutcomeGroup{
					OutcomeStatus: node.OutcomeStatus,
				},
				Source: investigation.EventSource{
					SourceRef:  node.SourceRef,
					SourceType: node.SourceType,
				},
			}
		}
		items = append(items, event)
	}
	return items, len(timeline), nil
}

func (s *fakeInvestigationStore) GetExecutionEvent(_ context.Context, executionID string, eventID string, _ time.Time) (investigation.EvidenceEventDetail, error) {
	item, ok := s.events[executionID+"/"+eventID]
	if !ok {
		if _, exists := s.timeline[executionID]; !exists {
			return investigation.EvidenceEventDetail{}, db.ErrExecutionNotFound
		}
		return investigation.EvidenceEventDetail{}, db.ErrEventNotFound
	}
	return item, nil
}

func (s *fakeInvestigationStore) ListExecutionGaps(_ context.Context, query investigation.GapQuery) ([]investigation.CaptureGap, int, error) {
	items, ok := s.gaps[query.ExecutionID]
	if !ok {
		return nil, 0, db.ErrExecutionNotFound
	}
	start := 0
	if query.After != nil {
		start = len(items)
		for index, item := range items {
			if item.GapID == query.After.GapID {
				start = index + 1
				break
			}
		}
	}
	end := start + query.Limit
	if end > len(items) {
		end = len(items)
	}
	return append([]investigation.CaptureGap(nil), items[start:end]...), len(items), nil
}

func (s *fakeInvestigationStore) ListSources(_ context.Context, limit int) ([]investigation.SourceOption, error) {
	if limit > len(s.sources) {
		limit = len(s.sources)
	}
	return append([]investigation.SourceOption(nil), s.sources[:limit]...), nil
}

func TestInvestigationAPIProvidesStablePagedWorkflow(t *testing.T) {
	fixture := newInvestigationFixture(t)
	handler := newAuthenticatedHandler(t, fixture)

	firstSearch := performRequest(t, handler, "/api/v1/executions?request_id=req-api-1&limit=1")
	if firstSearch.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", firstSearch.Code, firstSearch.Body.String())
	}
	var firstPage struct {
		Items []investigation.ExecutionSummary `json:"items"`
		Page  struct {
			AsOf       time.Time `json:"as_of"`
			Count      int       `json:"count"`
			HasMore    bool      `json:"has_more"`
			NextCursor string    `json:"next_cursor"`
		} `json:"page"`
	}
	decodeResponse(t, firstSearch, &firstPage)
	if len(firstPage.Items) != 1 || firstPage.Page.Count != 1 || !firstPage.Page.HasMore || firstPage.Page.NextCursor == "" {
		t.Fatalf("first page=%+v", firstPage)
	}
	asOf := url.QueryEscape(firstPage.Page.AsOf.UTC().Format(time.RFC3339Nano))
	fixedSearch := performRequest(t, handler, "/api/v1/executions?request_id=req-api-1&limit=1&as_of="+asOf)
	if fixedSearch.Code != http.StatusOK {
		t.Fatalf("fixed search status=%d body=%s", fixedSearch.Code, fixedSearch.Body.String())
	}
	var fixedPage struct {
		Items []investigation.ExecutionSummary `json:"items"`
		Page  struct {
			AsOf time.Time `json:"as_of"`
		} `json:"page"`
	}
	decodeResponse(t, fixedSearch, &fixedPage)
	if len(fixedPage.Items) != 1 || fixedPage.Items[0].ExecutionID != firstPage.Items[0].ExecutionID || !fixedPage.Page.AsOf.Equal(firstPage.Page.AsOf) {
		t.Fatalf("fixed page=%+v", fixedPage)
	}

	secondSearch := performRequest(t, handler, "/api/v1/executions?request_id=req-api-1&limit=1&as_of="+asOf+"&cursor="+url.QueryEscape(firstPage.Page.NextCursor))
	if secondSearch.Code != http.StatusOK {
		t.Fatalf("second search status=%d body=%s", secondSearch.Code, secondSearch.Body.String())
	}
	var secondPage struct {
		Items []investigation.ExecutionSummary `json:"items"`
		Page  struct {
			HasMore bool `json:"has_more"`
		} `json:"page"`
	}
	decodeResponse(t, secondSearch, &secondPage)
	if len(secondPage.Items) != 1 || secondPage.Items[0].ExecutionID == firstPage.Items[0].ExecutionID || secondPage.Page.HasMore {
		t.Fatalf("second page=%+v", secondPage)
	}
	mismatchedSearch := performRequest(t, handler, "/api/v1/executions?request_id=req-api-1&outcome=failure&cursor="+url.QueryEscape(firstPage.Page.NextCursor))
	if mismatchedSearch.Code != http.StatusConflict {
		t.Fatalf("mismatched search status=%d body=%s", mismatchedSearch.Code, mismatchedSearch.Body.String())
	}
	mismatchedAsOf := url.QueryEscape(firstPage.Page.AsOf.Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	mismatchedSnapshot := performRequest(t, handler, "/api/v1/executions?request_id=req-api-1&limit=1&as_of="+mismatchedAsOf+"&cursor="+url.QueryEscape(firstPage.Page.NextCursor))
	if mismatchedSnapshot.Code != http.StatusConflict {
		t.Fatalf("mismatched snapshot status=%d body=%s", mismatchedSnapshot.Code, mismatchedSnapshot.Body.String())
	}

	executionID := firstPage.Items[0].ExecutionID
	detail := performRequest(t, handler, "/api/v1/executions/"+executionID+"?as_of="+asOf)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}

	projectionResponse := performRequest(t, handler, "/api/v1/executions/"+executionID+"/investigation?as_of="+asOf)
	if projectionResponse.Code != http.StatusOK {
		t.Fatalf("investigation status=%d body=%s", projectionResponse.Code, projectionResponse.Body.String())
	}
	var projection investigation.InvestigationProjection
	decodeResponse(t, projectionResponse, &projection)
	if projection.SchemaVersion != investigation.InvestigationProjectionSchema ||
		projection.Execution.ExecutionID != executionID ||
		len(projection.Nodes) != 2 ||
		!projection.Integrity.ProjectionComplete {
		t.Fatalf("investigation projection=%+v", projection)
	}
	traceResponse := performRequest(t, handler, "/api/v1/executions/"+executionID+"/trace-journey?as_of="+asOf)
	if traceResponse.Code != http.StatusOK {
		t.Fatalf("trace journey status=%d body=%s", traceResponse.Code, traceResponse.Body.String())
	}
	var traceJourney investigation.TraceJourney
	decodeResponse(t, traceResponse, &traceJourney)
	if traceJourney.SchemaVersion != investigation.TraceJourneySchema ||
		!traceJourney.Available ||
		traceJourney.AnchorExecutionID != executionID ||
		len(traceJourney.Nodes) != 2 ||
		len(traceJourney.Edges) != 1 ||
		!traceJourney.Integrity.VisibleParentResolutionComplete {
		t.Fatalf("trace journey=%+v", traceJourney)
	}
	compare := performRequest(
		t,
		handler,
		"/api/v1/compare?left="+url.QueryEscape(firstPage.Items[0].ExecutionID)+
			"&left_as_of="+asOf+
			"&right="+url.QueryEscape(secondPage.Items[0].ExecutionID)+
			"&right_as_of="+asOf,
	)
	if compare.Code != http.StatusOK {
		t.Fatalf("compare status=%d body=%s", compare.Code, compare.Body.String())
	}
	var comparison investigation.ExecutionComparison
	decodeResponse(t, compare, &comparison)
	if comparison.SchemaVersion != investigation.ComparisonSchema ||
		comparison.Left.ExecutionID != firstPage.Items[0].ExecutionID ||
		comparison.Right.ExecutionID != secondPage.Items[0].ExecutionID ||
		!comparison.LeftAsOf.Equal(firstPage.Page.AsOf) ||
		!comparison.RightAsOf.Equal(firstPage.Page.AsOf) {
		t.Fatalf("comparison snapshot identity=%+v", comparison)
	}

	firstTimeline := performRequest(t, handler, "/api/v1/executions/"+executionID+"/timeline?limit=1&as_of="+asOf)
	if firstTimeline.Code != http.StatusOK {
		t.Fatalf("timeline status=%d body=%s", firstTimeline.Code, firstTimeline.Body.String())
	}
	var timelinePage struct {
		Total int                          `json:"total_event_count"`
		Items []investigation.TimelineNode `json:"items"`
		Page  struct {
			NextCursor string `json:"next_cursor"`
			HasMore    bool   `json:"has_more"`
		} `json:"page"`
	}
	decodeResponse(t, firstTimeline, &timelinePage)
	if timelinePage.Total != 2 || len(timelinePage.Items) != 1 || !timelinePage.Page.HasMore || timelinePage.Page.NextCursor == "" {
		t.Fatalf("timeline page=%+v", timelinePage)
	}
	secondTimeline := performRequest(t, handler, "/api/v1/executions/"+executionID+"/timeline?limit=1&as_of="+asOf+"&cursor="+url.QueryEscape(timelinePage.Page.NextCursor))
	if secondTimeline.Code != http.StatusOK {
		t.Fatalf("second timeline status=%d body=%s", secondTimeline.Code, secondTimeline.Body.String())
	}

	eventID := timelinePage.Items[0].EventID
	event := performRequest(t, handler, "/api/v1/executions/"+executionID+"/events/"+eventID+"?as_of="+asOf)
	if event.Code != http.StatusOK {
		t.Fatalf("event status=%d body=%s", event.Code, event.Body.String())
	}
	if strings.Contains(event.Body.String(), "raw_source_excerpt") || strings.Contains(event.Body.String(), "secret.invalid") {
		t.Fatalf("unsafe event response=%s", event.Body.String())
	}
	var eventResponse investigation.EvidenceEventDetail
	decodeResponse(t, event, &eventResponse)
	if eventResponse.Usage.CostMicros != "7" || eventResponse.Usage.Currency != "CNY" || eventResponse.Usage.AmountScale == nil || *eventResponse.Usage.AmountScale != 1_000_000 || eventResponse.Usage.MoneySchema != "micro-cny.v1" {
		t.Fatalf("v2 money API response=%+v", eventResponse.Usage)
	}

	gaps := performRequest(t, handler, "/api/v1/executions/"+executionID+"/gaps?as_of="+asOf)
	if gaps.Code != http.StatusOK || !strings.Contains(gaps.Body.String(), "candidate_gap_types") {
		t.Fatalf("gaps status=%d body=%s", gaps.Code, gaps.Body.String())
	}
	sources := performRequest(t, handler, "/api/v1/sources")
	if sources.Code != http.StatusOK || !strings.Contains(sources.Body.String(), "source-api-1") {
		t.Fatalf("sources status=%d body=%s", sources.Code, sources.Body.String())
	}
}

func TestFreshDatabaseBaselineReadAPIReturnsExplicitEmptyCollections(t *testing.T) {
	store := &fakeInvestigationStore{
		fakeStore: &fakeStore{},
		timeline:  map[string][]investigation.TimelineNode{},
		gaps:      map[string][]investigation.CaptureGap{},
		events:    map[string]investigation.EvidenceEventDetail{},
		journeys:  map[string]investigation.TraceJourney{},
	}
	response := performRequest(t, newAuthenticatedHandler(t, store), "/api/v1/executions?limit=20")
	if response.Code != http.StatusOK {
		t.Fatalf("empty baseline search status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Items []investigation.ExecutionSummary `json:"items"`
		Page  struct {
			Count      int    `json:"count"`
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		} `json:"page"`
	}
	decodeResponse(t, response, &result)
	if result.Items == nil || len(result.Items) != 0 || result.Page.Count != 0 ||
		result.Page.HasMore || result.Page.NextCursor != "" {
		t.Fatalf("empty baseline response=%+v", result)
	}
}

func TestInvestigationAPIRejectsAmbiguousAndMismatchedInputs(t *testing.T) {
	testCases := []struct {
		path   string
		status int
		code   string
	}{
		{"/api/v1/executions?limit=1&limit=2", http.StatusBadRequest, "invalid_argument"},
		{"/api/v1/executions?limit=1;unknown=2", http.StatusBadRequest, "invalid_argument"},
		{"/api/v1/executions?unknown=true", http.StatusBadRequest, "invalid_argument"},
		{"/api/v1/executions?request_id=abc&request_id_prefix=abc", http.StatusBadRequest, "invalid_argument"},
		{"/api/v1/executions?request_id_prefix=ab", http.StatusBadRequest, "invalid_argument"},
		{"/api/v1/executions?has_gaps=True", http.StatusBadRequest, "invalid_argument"},
		{"/api/v1/executions?cursor=", http.StatusBadRequest, "invalid_cursor"},
		{"/api/v1/executions?from=2026-07-13T12:00:00%2B08:00", http.StatusBadRequest, "invalid_argument"},
		{"/api/v1/executions/xe1_invalid", http.StatusBadRequest, "invalid_argument"},
		{"/api/v1/executions/xe1_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa?as_of=", http.StatusBadRequest, "invalid_argument"},
		{"/api/v1/executions/xe1_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa?as_of=2020-01-01T00:00:00Z", http.StatusGone, "cursor_expired"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.path, func(t *testing.T) {
			response := performRequest(t, newAuthenticatedHandler(t, newInvestigationFixture(t)), testCase.path)
			if response.Code != testCase.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var problem api.Problem
			decodeResponse(t, response, &problem)
			if problem.Code != testCase.code {
				t.Fatalf("problem=%+v", problem)
			}
		})
	}
}

func newInvestigationFixture(t *testing.T) *fakeInvestigationStore {
	t.Helper()
	now := time.Now().UTC().Add(-time.Hour)
	sourceOne := "11111111-1111-1111-1111-111111111111"
	sourceTwo := "33333333-3333-3333-3333-333333333333"
	executionOne, err := executionid.New(sourceOne, "req-api-1")
	if err != nil {
		t.Fatalf("execution one: %v", err)
	}
	executionTwo, err := executionid.New(sourceTwo, "req-api-1")
	if err != nil {
		t.Fatalf("execution two: %v", err)
	}
	traceExecution, err := executionid.New(sourceOne, "req-api-trace-2")
	if err != nil {
		t.Fatalf("trace execution: %v", err)
	}
	eventOne := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	eventTwo := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	gapID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	freshness := investigation.Freshness{
		IntegrationMode:  investigation.IntegrationFixture,
		SyncState:        investigation.SyncStateGap,
		TimelineComplete: true,
		ImportedAt:       timePointer(now.Add(time.Minute)),
	}
	makeSummary := func(executionID string, sourceID string, offset time.Duration) investigation.ExecutionSummary {
		return investigation.ExecutionSummary{
			ExecutionID:         executionID,
			RequestID:           "req-api-1",
			SourceSystemID:      sourceID,
			SourceType:          "unirouter",
			SourceDisplayName:   "source-api-1",
			Outcome:             investigation.OutcomeSuccess,
			FirstEffectiveAt:    now.Add(offset),
			LastEffectiveAt:     now.Add(offset + time.Minute),
			EventCount:          2,
			GapCount:            1,
			HasGaps:             true,
			PayloadCaptureModes: []string{"metadata_only"},
			ProofClass:          investigation.ProofSyntheticFixture,
			AssuranceMode:       investigation.AssuranceSyntheticFixture,
			Freshness:           freshness,
		}
	}
	firstSummary := makeSummary(executionOne, sourceOne, 2*time.Minute)
	secondSummary := makeSummary(executionTwo, sourceTwo, 0)
	firstNode := investigation.TimelineNode{
		EventID:         eventOne,
		EventType:       "gateway.request.completed",
		Stage:           "gateway",
		Severity:        "info",
		EffectiveAt:     now,
		IngestedAt:      now.Add(time.Minute),
		SourceRef:       "unirouter:fixture:trace:1",
		SourceType:      "unirouter",
		OutcomeStatus:   investigation.OutcomeSuccess,
		FieldProvenance: investigation.FieldProvenance{Correlation: "direct", Outcome: "derived"},
	}
	secondNode := firstNode
	secondNode.EventID = eventTwo
	secondNode.EffectiveAt = now.Add(time.Second)
	secondNode.IngestedAt = now.Add(time.Minute + time.Second)
	return &fakeInvestigationStore{
		fakeStore: &fakeStore{},
		summaries: []investigation.ExecutionSummary{firstSummary, secondSummary},
		timeline: map[string][]investigation.TimelineNode{
			executionOne: {firstNode, secondNode},
			executionTwo: {firstNode},
		},
		gaps: map[string][]investigation.CaptureGap{
			executionOne: {{
				GapID:             gapID,
				GapType:           "not_available_from_source",
				CandidateGapTypes: []string{"sampled_out", "retention_expired"},
				AffectedClaim:     "trace_events",
				GapReason:         "source omitted trace",
				Severity:          "warning",
				CreatedAt:         now.Add(2 * time.Minute),
			}},
			executionTwo: {},
		},
		events: map[string]investigation.EvidenceEventDetail{
			executionOne + "/" + eventOne: {
				ExecutionID: executionOne,
				EventID:     eventOne,
				EventType:   firstNode.EventType,
				Stage:       firstNode.Stage,
				Severity:    firstNode.Severity,
				CaptureMode: "db_import",
				IngestedAt:  firstNode.IngestedAt,
				Usage: investigation.UsageGroup{
					CostMicros:  "7",
					Currency:    "CNY",
					AmountScale: int64Pointer(1_000_000),
					MoneySchema: "micro-cny.v1",
				},
				FieldProvenance: investigation.FieldProvenance{Correlation: "direct"},
				Source: investigation.EventSource{
					SourceSystemID:    sourceOne,
					AdapterManifestID: "22222222-2222-2222-2222-222222222222",
					SchemaVersion:     "m0.1",
					SourceType:        "unirouter",
					SourceRef:         firstNode.SourceRef,
					EventHash:         "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					FidelityLevel:     "F2",
				},
			},
		},
		journeys: map[string]investigation.TraceJourney{
			executionOne: {
				SchemaVersion:     investigation.TraceJourneySchema,
				Available:         true,
				AnchorExecutionID: executionOne,
				SourceSystemID:    sourceOne,
				TraceID:           "0123456789abcdef0123456789abcdef",
				Nodes: []investigation.TraceJourneyNode{
					{
						ExecutionID: executionOne,
						EventID:     eventOne,
						RequestID:   "req-api-1",
						TraceID:     "0123456789abcdef0123456789abcdef",
						SpanID:      "0123456789abcdef",
						Action:      "agent.plan",
						ServiceName: "agent-runtime",
						EffectiveAt: now,
					},
					{
						ExecutionID:  traceExecution,
						EventID:      eventTwo,
						RequestID:    "req-api-2",
						TraceID:      "0123456789abcdef0123456789abcdef",
						SpanID:       "fedcba9876543210",
						ParentSpanID: "0123456789abcdef",
						Action:       "chat.completions",
						ServiceName:  "model-gateway",
						EffectiveAt:  now.Add(time.Second),
					},
				},
				Edges: []investigation.TraceJourneyEdge{{
					FromEventID: eventOne,
					ToEventID:   eventTwo,
					EdgeBasis:   "source_declared_parent_span_across_execution",
				}},
				Integrity: investigation.TraceJourneyIntegrity{
					NodeCount:                       2,
					ExecutionCount:                  2,
					DeclaredParentCount:             1,
					ResolvedParentCount:             1,
					VisibleParentResolutionComplete: true,
				},
			},
		},
		sources: []investigation.SourceOption{{
			SourceSystemID: sourceOne,
			SourceKey:      "source-api-1",
			SourceType:     "unirouter",
			DisplayName:    "source-api-1",
		}},
	}
}

func performRequest(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	handler.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v body=%s", err, response.Body.String())
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func int64Pointer(value int64) *int64 {
	return &value
}
