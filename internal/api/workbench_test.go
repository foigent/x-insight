package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestWorkbenchRoutesUseFormalDTOsAndStrictTransportBoundary(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	store := &workbenchStoreStub{now: now}
	handler, cookie, csrf := productTestHandler(
		t, &fakeStore{}, store, nil, "workbench.one", "workbench password",
	)
	body := `{"contract_version":"xinsight.workbench.v1","page":"cost","metric_version":"xinsight.workbench.cost.v1","period":{"relative_hours":24,"display_timezone":"UTC"},"money_basis":"gateway_charged","currency":"CNY","filters":{"source_ids":[],"source_projects":[],"cost_projects":[]}}`

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodPost, "/api/v1/workbench/readings", body, cookie, csrf))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"reading_id":"xwr_AAAAAAAAAAAAAAAAAAAAAA"`) || store.createCalls != 1 {
		t.Fatalf("create status=%d body=%s calls=%d", response.Code, response.Body.String(), store.createCalls)
	}

	for _, testCase := range []struct {
		path string
		want string
	}{
		{"/api/v1/workbench/readings/xwr_AAAAAAAAAAAAAAAAAAAAAA", `"coverage"`},
		{"/api/v1/workbench/readings/xwr_AAAAAAAAAAAAAAAAAAAAAA/summary", `"selected_amount_micros":"17"`},
		{"/api/v1/workbench/readings/xwr_AAAAAAAAAAAAAAAAAAAAAA/contributions?dimension=model&top_n=5", `"unassigned_scope":"reading_constant"`},
		{"/api/v1/workbench/readings/xwr_AAAAAAAAAAAAAAAAAAAAAA/series?metric=amount&bucket_seconds=3600", `"known_value":"17"`},
		{"/api/v1/workbench/readings/xwr_AAAAAAAAAAAAAAAAAAAAAA/details?contribution_ref=bW9kZWwAc3ludGhldGlj&limit=50", `"account_state":"unknown"`},
		{"/api/v1/workbench/readings/xwr_AAAAAAAAAAAAAAAAAAAAAA/recent-requests?limit=10", `"scope_basis":"economic_reading_members"`},
		{"/api/v1/workbench/readings/xwr_AAAAAAAAAAAAAAAAAAAAAA/sources", `"scope_limited_unknown"`},
		{"/api/v1/workbench/saved-filters", `"items":[]`},
	} {
		request := httptest.NewRequest(http.MethodGet, "https://xinsight.local"+testCase.path, nil)
		request.AddCookie(cookie)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), testCase.want) {
			t.Fatalf("GET %s status=%d body=%s", testCase.path, response.Code, response.Body.String())
		}
	}

	noCSRF := productRequest(http.MethodPost, "/api/v1/workbench/readings", body, cookie, "")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, noCSRF)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"csrf_validation_failed"`) || store.createCalls != 1 {
		t.Fatalf("missing csrf status=%d body=%s calls=%d", response.Code, response.Body.String(), store.createCalls)
	}

	unknownQuery := httptest.NewRequest(http.MethodGet, "https://xinsight.local/api/v1/workbench/readings/xwr_AAAAAAAAAAAAAAAAAAAAAA/summary?unexpected=1", nil)
	unknownQuery.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, unknownQuery)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_argument"`) {
		t.Fatalf("unknown query status=%d body=%s", response.Code, response.Body.String())
	}

	duplicate := strings.Replace(body, `"relative_hours":24`, `"relative_hours":24,"relative_hours":72`, 1)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodPost, "/api/v1/workbench/readings", duplicate, cookie, csrf))
	if response.Code != http.StatusBadRequest || store.createCalls != 1 {
		t.Fatalf("duplicate JSON status=%d body=%s calls=%d", response.Code, response.Body.String(), store.createCalls)
	}

	for _, path := range []string{"/api/v1/executions", "/api/v1/cases", "/api/v1/saved-views"} {
		legacy := httptest.NewRequest(http.MethodGet, "https://xinsight.local"+path, nil)
		legacy.AddCookie(cookie)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, legacy)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"permission_denied"`) {
			t.Fatalf("workbench reader legacy path=%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestWorkbenchProblemMappingIsStable(t *testing.T) {
	testCases := []struct {
		err    error
		status int
		code   string
	}{
		{workbench.ErrInvalid, http.StatusBadRequest, "invalid_workbench_request"},
		{workbench.ErrScopeForbidden, http.StatusForbidden, "workbench_scope_denied"},
		{workbench.ErrNotFound, http.StatusNotFound, "workbench_record_not_found"},
		{workbench.ErrExpired, http.StatusGone, "workbench_reading_expired"},
		{workbench.ErrAccessChanged, http.StatusConflict, "workbench_access_changed"},
		{workbench.ErrTooLarge, http.StatusUnprocessableEntity, "workbench_reading_too_large"},
		{workbench.ErrConflict, http.StatusConflict, "workbench_conflict"},
		{workbench.ErrStaleRevision, http.StatusConflict, "workbench_revision_stale"},
		{context.DeadlineExceeded, http.StatusServiceUnavailable, "workbench_unavailable"},
	}
	for _, testCase := range testCases {
		store := &workbenchStoreStub{summaryErr: testCase.err}
		handler, cookie, _ := productTestHandler(t, &fakeStore{}, store, nil, "workbench.one", "workbench password")
		request := httptest.NewRequest(http.MethodGet, "https://xinsight.local/api/v1/workbench/readings/xwr_AAAAAAAAAAAAAAAAAAAAAA/summary", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != testCase.status || !strings.Contains(response.Body.String(), `"code":"`+testCase.code+`"`) {
			t.Fatalf("err=%v status=%d body=%s", testCase.err, response.Code, response.Body.String())
		}
	}
}

type workbenchStoreStub struct {
	workspaceStoreStub
	now               time.Time
	contributionOrder string
	createCalls       int
	summaryErr        error
}

func (store *workbenchStoreStub) reading() workbench.Reading {
	now := store.now
	if now.IsZero() {
		now = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	}
	return workbench.Reading{
		ID: "xwr_AAAAAAAAAAAAAAAAAAAAAA", ContractVersion: workbench.ContractVersion,
		MetricVersion: workbench.MetricVersion, Period: workbench.Period{StartInclusive: now.Add(-time.Hour), EndExclusive: now, Timezone: "UTC"},
		Cutoff: now, ExpiresAt: now.Add(time.Hour), MoneyBasis: workbench.MoneyGatewayCharged,
		MoneyTitle: "网关扣费", Currency: "CNY", MembershipRef: "sha256:" + strings.Repeat("a", 64),
		InterpretationRevision: "sha256:" + strings.Repeat("b", 64), AuthorizationRevision: "sha256:" + strings.Repeat("c", 64),
		Dimensions: []string{"model", "source_project", "cost_project"}, Coverage: testWorkbenchCoverage(),
	}
}

func (store *workbenchStoreStub) CreateWorkbenchReading(context.Context, string, workbench.ReadingRequest, time.Time) (workbench.Reading, error) {
	store.createCalls++
	return store.reading(), nil
}
func (store *workbenchStoreStub) GetWorkbenchReading(context.Context, string, string, time.Time) (workbench.Reading, error) {
	return store.reading(), nil
}
func (store *workbenchStoreStub) GetWorkbenchSummary(context.Context, string, string, time.Time) (workbench.Summary, error) {
	if store.summaryErr != nil {
		return workbench.Summary{}, store.summaryErr
	}
	return workbench.Summary{ReadingID: store.reading().ID, MoneyBasis: workbench.MoneyGatewayCharged, Currency: "CNY", RequestCount: 1, SelectedAmountMicros: "17", GatewayChargedMicros: "17", CalculatedReferenceMicros: "23", Coverage: testWorkbenchCoverage()}, nil
}
func (store *workbenchStoreStub) GetWorkbenchContributions(context.Context, string, string, string, int, int, string, time.Time) (workbench.Contributions, error) {
	return workbench.Contributions{ReadingID: store.reading().ID, Dimension: "model", Mode: "top_n", Rows: []workbench.Contribution{}, OtherKnownMicros: "0", UnassignedMicros: "17", UnassignedScope: "reading_constant", DenominatorMicros: "17", IsLastPage: true}, nil
}
func (store *workbenchStoreStub) GetWorkbenchContributionsOrdered(ctx context.Context, subject, id, dimension, orderBy string, limit, topN int, cursor string, now time.Time) (workbench.Contributions, error) {
	store.contributionOrder = orderBy
	result, err := store.GetWorkbenchContributions(ctx, subject, id, dimension, limit, topN, cursor, now)
	result.OrderBy = orderBy
	result.DenominatorRequests = 1
	result.UnassignedRequests = 1
	return result, err
}
func (store *workbenchStoreStub) GetWorkbenchSeries(context.Context, string, string, string, int64, time.Time) (workbench.Series, error) {
	now := store.reading().Cutoff
	return workbench.Series{ReadingID: store.reading().ID, Metric: "amount", Unit: "micros", BucketSeconds: 3600, Points: []workbench.SeriesPoint{{StartInclusive: now.Add(-time.Hour), EndExclusive: now, KnownValue: "17"}}, Coverage: testWorkbenchCoverage()}, nil
}
func (store *workbenchStoreStub) GetWorkbenchDetails(context.Context, string, string, string, int, string, time.Time) (workbench.Details, error) {
	return workbench.Details{ReadingID: store.reading().ID, ContributionRef: "bW9kZWwAc3ludGhldGlj", Rows: []workbench.DetailRow{{RequestID: "req-1", ExecutionID: "xe1_" + strings.Repeat("d", 64), EvidenceEventID: "11111111-1111-4111-8111-111111111111", EventHash: "sha256:" + strings.Repeat("e", 64), SourceSystemID: "22222222-2222-4222-8222-222222222222", SourceVersion: "unirouter.completed-execution.v1", TerminalStatus: "settled", OccurredAt: store.reading().Cutoff, GatewayChargedMicros: "17", CalculatedReferenceMicros: "23", AccountState: "unknown", ApplicationState: "unknown", PurposeState: "unknown"}}}, nil
}
func (store *workbenchStoreStub) GetWorkbenchRecentRequests(_ context.Context, _ string, id, runtimeID string, limit int, cursor string, _ time.Time) (workbench.RecentRequests, error) {
	if limit < 1 || limit > workbench.MaxPageSize || cursor != "" {
		return workbench.RecentRequests{}, workbench.ErrInvalid
	}
	return workbench.RecentRequests{Version: workbench.RecentRequestsVersion, ScopeBasis: "economic_reading_members", DetailScope: "currently_authorized_details_only", ReadingID: id, Cutoff: store.reading().Cutoff, Items: []workbench.RecentRequest{}}, nil
}
func (store *workbenchStoreStub) GetWorkbenchSourceStatuses(context.Context, string, string, time.Time) (workbench.SourceStatuses, error) {
	return workbench.SourceStatuses{ReadingID: store.reading().ID, Items: []workbench.SourceStatus{{SourceSystemID: "22222222-2222-4222-8222-222222222222", DisplayName: "UniRouter", Status: "scope_limited_unknown", ScopeLabel: "project_restricted", Gaps: []string{"scope_limited_source_state_unknown"}}}}, nil
}
func (store *workbenchStoreStub) CreateWorkbenchSavedFilter(context.Context, string, workbench.SavedFilterCommand, time.Time) (workbench.SavedFilter, error) {
	return workbench.SavedFilter{}, nil
}
func (store *workbenchStoreStub) UpdateWorkbenchSavedFilter(context.Context, string, string, workbench.SavedFilterCommand, time.Time) (workbench.SavedFilter, error) {
	return workbench.SavedFilter{}, nil
}
func (store *workbenchStoreStub) ListWorkbenchSavedFilters(context.Context, string) (workbench.SavedFilterList, error) {
	return workbench.SavedFilterList{Items: []workbench.SavedFilter{}}, nil
}

func testWorkbenchCoverage() workbench.Coverage {
	return workbench.Coverage{Status: "period_completeness_unknown", PeriodCompleteness: "unknown", ProjectionCompleteForMembers: true, KnownItems: 1, Gaps: []string{"source_period_completeness_not_asserted"}}
}

var _ api.WorkbenchStore = (*workbenchStoreStub)(nil)

func TestWorkbenchContributionOrderHTTPBoundary(t *testing.T) {
	for _, tc := range []struct {
		query, want string
		status      int
	}{
		{"", "amount", 200}, {"&order_by=amount", "amount", 200}, {"&order_by=requests", "requests", 200},
		{"&order_by=unknown", "", 400}, {"&order_by=requests&order_by=amount", "", 400},
	} {
		t.Run(tc.query, func(t *testing.T) {
			store := &workbenchStoreStub{}
			handler, cookie, _ := productTestHandler(t, &fakeStore{}, store, nil, "workbench.one", "workbench password")
			request := httptest.NewRequest(http.MethodGet, "https://xinsight.local/api/v1/workbench/readings/xwr_AAAAAAAAAAAAAAAAAAAAAA/contributions?dimension=model&top_n=5"+tc.query, nil)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status || store.contributionOrder != tc.want {
				t.Fatalf("status=%d order=%q body=%s", response.Code, store.contributionOrder, response.Body.String())
			}
			if tc.status == 200 && (!strings.Contains(response.Body.String(), `"order_by":"`+tc.want+`"`) || !strings.Contains(response.Body.String(), `"denominator_requests":1`)) {
				t.Fatal(response.Body.String())
			}
		})
	}
}
