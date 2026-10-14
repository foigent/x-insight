package api_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/investigation"
)

type fakePostureStore struct {
	*fakeStore
	snapshot investigation.PostureSnapshot
	query    investigation.PostureQuery
}

func (store *fakePostureStore) GetPosture(
	_ context.Context,
	query investigation.PostureQuery,
) (investigation.PostureSnapshot, error) {
	store.query = query
	result := store.snapshot
	result.AsOf = query.AsOf
	result.From = query.From
	result.To = query.To
	result.SourceSystemID = query.SourceSystemID
	return result, nil
}

func TestPostureAPIUsesOneFixedBoundedScope(t *testing.T) {
	asOf := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	from := asOf.Add(-7 * 24 * time.Hour)
	sourceID := "11111111-1111-4111-8111-111111111111"
	store := &fakePostureStore{
		fakeStore: &fakeStore{},
		snapshot: investigation.PostureSnapshot{
			SchemaVersion: investigation.PostureSchema,
			Risk: investigation.PostureRiskSummary{
				OpenTotal: 2,
				Items:     []investigation.InvestigationAlert{},
			},
			Coverage:   []investigation.PostureCoverage{},
			Sources:    []investigation.PostureSourceSummary{},
			Trend:      investigation.PostureTrend{Items: []investigation.PostureTrendBucket{}},
			Boundaries: []string{"fixed_snapshot"},
		},
	}
	handler := newAuthenticatedHandler(t, store)
	path := "/api/v1/posture?as_of=" + url.QueryEscape(asOf.Format(time.RFC3339Nano)) +
		"&from=" + url.QueryEscape(from.Format(time.RFC3339Nano)) +
		"&to=" + url.QueryEscape(asOf.Format(time.RFC3339Nano)) +
		"&source_system_id=" + sourceID
	response := performRequest(t, handler, path)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body investigation.PostureSnapshot
	decodeResponse(t, response, &body)
	if body.SchemaVersion != investigation.PostureSchema || body.Risk.OpenTotal != 2 ||
		!body.AsOf.Equal(asOf) || !body.From.Equal(from) || !body.To.Equal(asOf) ||
		body.SourceSystemID != sourceID {
		t.Fatalf("body=%+v", body)
	}
	if !store.query.AsOf.Equal(asOf) || !store.query.From.Equal(from) ||
		!store.query.To.Equal(asOf) || store.query.SourceSystemID != sourceID {
		t.Fatalf("query=%+v", store.query)
	}
}

func TestPostureAPIRejectsUnboundedOrFutureWindow(t *testing.T) {
	store := &fakePostureStore{fakeStore: &fakeStore{}}
	handler := newAuthenticatedHandler(t, store)
	asOf := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

	tooWideFrom := asOf.Add(-32 * 24 * time.Hour)
	tooWide := performRequest(t, handler,
		"/api/v1/posture?as_of="+url.QueryEscape(asOf.Format(time.RFC3339))+"&from="+
			url.QueryEscape(tooWideFrom.Format(time.RFC3339))+"&to="+url.QueryEscape(asOf.Format(time.RFC3339)),
	)
	if tooWide.Code != http.StatusBadRequest {
		t.Fatalf("wide status=%d body=%s", tooWide.Code, tooWide.Body.String())
	}

	futureTo := asOf.Add(time.Second)
	future := performRequest(t, handler,
		"/api/v1/posture?as_of="+url.QueryEscape(asOf.Format(time.RFC3339))+"&to="+
			url.QueryEscape(futureTo.Format(time.RFC3339)),
	)
	if future.Code != http.StatusBadRequest {
		t.Fatalf("future status=%d body=%s", future.Code, future.Body.String())
	}
}
