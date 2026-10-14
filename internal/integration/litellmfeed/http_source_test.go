package litellmfeed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestNewHTTPSourceRejectsEncodedEndpointPath(t *testing.T) {
	_, err := NewHTTPSource(HTTPSourceConfig{
		Endpoint: "https://litellm.test.invalid/spend%2Flogs/v2",
		APIKey:   "test-litellm-key",
	})
	if err == nil {
		t.Fatal("encoded endpoint path was accepted")
	}
}

func TestFetchCompleteWindowFloorsCheckpointSecondAndRetainsOverlap(t *testing.T) {
	checkpoint := Checkpoint{
		LastStartTime:     "2026-07-28T02:00:00.500000Z",
		BoundaryRequestID: []string{"request-seen"},
	}
	var queryStart string
	transport := roundTripFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		if request.URL.Path != "/spend/logs/v2" {
			t.Errorf("path=%q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-litellm-key" {
			t.Error("authorization header mismatch")
		}
		queryStart = request.URL.Query().Get("start_date")
		if request.URL.Query().Get("end_date") != "2026-07-28 02:01:00" ||
			request.URL.Query().Get("page") != "1" ||
			request.URL.Query().Get("page_size") != "100" ||
			request.URL.Query().Get("sort_by") != "startTime" ||
			request.URL.Query().Get("sort_order") != "asc" {
			t.Errorf("query=%v", request.URL.Query())
		}
		return jsonResponse(t, request, map[string]any{
			"data": []any{
				upstreamRow("request-old", "2026-07-28T02:00:00.100000Z"),
				upstreamRow("request-seen", checkpoint.LastStartTime),
				upstreamRow("request-late", checkpoint.LastStartTime),
				upstreamRow("请求-new", "2026-07-28T02:00:00.500001Z"),
			},
			"total":           4,
			"page":            1,
			"page_size":       100,
			"total_pages":     1,
			"total_is_capped": false,
		}), nil
	})

	source, err := NewHTTPSource(HTTPSourceConfig{
		Endpoint: "https://litellm.test.invalid/spend/logs/v2",
		APIKey:   "test-litellm-key",
	})
	if err != nil {
		t.Fatalf("new HTTP source: %v", err)
	}
	source.client = &http.Client{Transport: transport}
	window, err := source.FetchCompleteWindow(
		context.Background(),
		checkpoint,
		time.Date(2026, 7, 28, 2, 1, 0, 999999000, time.UTC),
	)
	if err != nil {
		t.Fatalf("fetch complete window: %v", err)
	}
	if queryStart != "2026-07-28 02:00:00" {
		t.Fatalf("start_date=%q", queryStart)
	}
	if window.Total != 4 || len(window.Rows) != 4 {
		t.Fatalf("window=%+v", window)
	}
	selection, err := SelectWindow(checkpoint, window.Rows)
	if err != nil {
		t.Fatalf("select overlap: %v", err)
	}
	got := make([]string, len(selection.Accepted))
	for index := range selection.Accepted {
		got[index] = selection.Accepted[index].Record.RequestID
	}
	if want := []string{"request-late", "请求-new"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("accepted=%v want=%v", got, want)
	}
}

func TestFetchCompleteWindowDefersRowsAboveSecondQueryBoundAndIgnoresRowEndTime(
	t *testing.T,
) {
	checkpoint := Checkpoint{
		LastStartTime:     "2026-07-28T02:00:00.000000Z",
		BoundaryRequestID: []string{},
	}
	queryEnds := make([]string, 0, 2)
	requests := 0
	transport := roundTripFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		requests++
		queryEnds = append(queryEnds, request.URL.Query().Get("end_date"))
		if requests == 1 {
			return jsonResponse(t, request, map[string]any{
				"data":            []any{},
				"total":           0,
				"page":            1,
				"page_size":       PageSize,
				"total_pages":     0,
				"total_is_capped": false,
			}), nil
		}
		row := upstreamRow(
			"request-deferred-same-second",
			"2026-07-28T02:00:00.500000Z",
		)
		row["endTime"] = "2026-07-29T02:00:00.000000Z"
		return jsonResponse(t, request, map[string]any{
			"data":            []any{row},
			"total":           1,
			"page":            1,
			"page_size":       PageSize,
			"total_pages":     1,
			"total_is_capped": false,
		}), nil
	})
	source, err := NewHTTPSource(HTTPSourceConfig{
		Endpoint: "https://litellm.test.invalid/spend/logs/v2",
		APIKey:   "test-litellm-key",
	})
	if err != nil {
		t.Fatalf("new HTTP source: %v", err)
	}
	source.client = &http.Client{Transport: transport}
	first, err := source.FetchCompleteWindow(
		context.Background(),
		checkpoint,
		time.Date(2026, 7, 28, 2, 0, 0, 999999000, time.UTC),
	)
	if err != nil {
		t.Fatalf("first eligibility window: %v", err)
	}
	if first.Total != 0 || len(first.Rows) != 0 {
		t.Fatalf("first eligibility window=%+v", first)
	}
	second, err := source.FetchCompleteWindow(
		context.Background(),
		checkpoint,
		time.Date(2026, 7, 28, 2, 0, 1, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("second eligibility window: %v", err)
	}
	if !reflect.DeepEqual(
		queryEnds,
		[]string{"2026-07-28 02:00:00", "2026-07-28 02:00:01"},
	) {
		t.Fatalf("inclusive second query bounds=%v", queryEnds)
	}
	selection, err := SelectWindow(checkpoint, second.Rows)
	if err != nil {
		t.Fatalf("select deferred row: %v", err)
	}
	if second.Total != 1 ||
		len(selection.Accepted) != 1 ||
		selection.Accepted[0].Record.RequestID !=
			"request-deferred-same-second" ||
		selection.Accepted[0].Record.EndTime !=
			"2026-07-29T02:00:00.000000Z" {
		t.Fatalf("deferred/endTime-independent result=%+v/%+v", second, selection)
	}
}

func TestFetchCompleteWindowRequiresExactRowsOnEveryPage(t *testing.T) {
	requests := 0
	transport := roundTripFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		requests++
		page := request.URL.Query().Get("page")
		count := PageSize
		startIndex := 0
		if page == "2" {
			count = 1
			startIndex = PageSize
		}
		data := make([]any, count)
		for index := 0; index < count; index++ {
			itemIndex := startIndex + index
			start := time.Date(
				2026,
				7,
				28,
				2,
				0,
				1,
				(itemIndex+1)*1000,
				time.UTC,
			).Format(checkpointTimeLayout)
			data[index] = upstreamRow(
				fmt.Sprintf("request-%03d", itemIndex),
				start,
			)
		}
		return jsonResponse(t, request, map[string]any{
			"data":            data,
			"total":           101,
			"page":            requests,
			"page_size":       PageSize,
			"total_pages":     2,
			"total_is_capped": false,
		}), nil
	})
	source, err := NewHTTPSource(HTTPSourceConfig{
		Endpoint: "https://litellm.test.invalid/spend/logs/v2",
		APIKey:   "test-litellm-key",
	})
	if err != nil {
		t.Fatalf("new HTTP source: %v", err)
	}
	source.client = &http.Client{Transport: transport}
	window, err := source.FetchCompleteWindow(
		context.Background(),
		Checkpoint{
			LastStartTime:     "2026-07-28T02:00:00.000000Z",
			BoundaryRequestID: []string{},
		},
		time.Date(2026, 7, 28, 2, 2, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("fetch exact multi-page window: %v", err)
	}
	if requests != 2 || window.Total != 101 || len(window.Rows) != 101 {
		t.Fatalf("requests=%d window total/rows=%d/%d", requests, window.Total, len(window.Rows))
	}
}

func TestFetchCompleteWindowRejectsIncompletePagination(t *testing.T) {
	requests := 0
	transport := roundTripFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		requests++
		return jsonResponse(t, request, map[string]any{
			"data":            []any{upstreamRow("request-only", "2026-07-28T02:00:01.000000Z")},
			"total":           101,
			"page":            1,
			"page_size":       100,
			"total_pages":     2,
			"total_is_capped": false,
		}), nil
	})
	source, err := NewHTTPSource(HTTPSourceConfig{
		Endpoint: "https://litellm.test.invalid/spend/logs/v2",
		APIKey:   "test-litellm-key",
	})
	if err != nil {
		t.Fatalf("new HTTP source: %v", err)
	}
	source.client = &http.Client{Transport: transport}
	_, err = source.FetchCompleteWindow(
		context.Background(),
		Checkpoint{
			LastStartTime:     "2026-07-28T02:00:00.000000Z",
			BoundaryRequestID: []string{},
		},
		time.Date(2026, 7, 28, 2, 2, 0, 0, time.UTC),
	)
	var sourceErr *SourceError
	if err == nil || !errors.As(err, &sourceErr) ||
		sourceErr.Code != "incomplete_paginated_snapshot" || requests != 1 {
		t.Fatalf("error=%v requests=%d", err, requests)
	}
}

func TestDecodePageRequiresEveryEnvelopeFieldAndAcceptsPresentFalse(t *testing.T) {
	required := map[string]any{
		"data":            []any{},
		"total":           0,
		"page":            1,
		"page_size":       PageSize,
		"total_pages":     0,
		"total_is_capped": false,
	}
	for _, field := range []string{
		"data",
		"total",
		"page",
		"page_size",
		"total_pages",
		"total_is_capped",
	} {
		t.Run("missing_"+field, func(t *testing.T) {
			candidate := make(map[string]any, len(required)-1)
			for key, value := range required {
				if key != field {
					candidate[key] = value
				}
			}
			payload, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodePage(payload); err == nil {
				t.Fatalf("page missing %s was accepted", field)
			}
		})
	}
	payload, err := json.Marshal(required)
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodePage(payload)
	if err != nil {
		t.Fatalf("page with explicit total_is_capped=false: %v", err)
	}
	if page.TotalIsCapped {
		t.Fatal("explicit false total_is_capped changed value")
	}
}

func TestDecodePageRejectsUnpairedUTF16Surrogate(t *testing.T) {
	payload := []byte(`{
	  "data":[{
	    "request_id":"bad-\ud800",
	    "status":null,
	    "startTime":"2026-07-28T02:00:01.000000Z",
	    "endTime":"2026-07-28T02:00:02.000000Z",
	    "model":"model-a",
	    "prompt_tokens":1,
	    "completion_tokens":1,
	    "total_tokens":2,
	    "spend":0,
	    "cache_hit":false
	  }],
	  "total":1,
	  "page":1,
	  "page_size":100,
	  "total_pages":1,
	  "total_is_capped":false
	}`)
	if _, err := decodePage(payload); err == nil {
		t.Fatal("unpaired surrogate reached LiteLLM page decoding")
	}
}

func TestFetchCompleteWindowTreatsEqualEligibilityFenceAsEmptyWithoutRequest(
	t *testing.T,
) {
	requests := 0
	source, err := NewHTTPSource(HTTPSourceConfig{
		Endpoint: "https://litellm.test.invalid/spend/logs/v2",
		APIKey:   "test-litellm-key",
	})
	if err != nil {
		t.Fatalf("new HTTP source: %v", err)
	}
	source.client = &http.Client{Transport: roundTripFunc(func(
		*http.Request,
	) (*http.Response, error) {
		requests++
		return nil, errors.New("equal eligibility fence issued an HTTP request")
	})}
	checkpoint := Checkpoint{
		LastStartTime:     "2026-07-28T02:00:00.123456Z",
		BoundaryRequestID: []string{},
	}
	window, err := source.FetchCompleteWindow(
		context.Background(),
		checkpoint,
		time.Date(2026, 7, 28, 2, 0, 0, 123456000, time.UTC),
	)
	if err != nil {
		t.Fatalf("equal eligibility fence: %v", err)
	}
	if requests != 0 ||
		window.Total != 0 ||
		len(window.Rows) != 0 ||
		window.EligibleThrough.Format(checkpointTimeLayout) !=
			checkpoint.LastStartTime {
		t.Fatalf("requests=%d window=%+v", requests, window)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func jsonResponse(
	t *testing.T,
	request *http.Request,
	value any,
) *http.Response {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(payload)),
		Request:    request,
	}
}

func upstreamRow(requestID, startTime string) map[string]any {
	end, err := time.Parse(checkpointTimeLayout, startTime)
	if err != nil {
		panic(err)
	}
	return map[string]any{
		"request_id":          requestID,
		"status":              "success",
		"startTime":           startTime,
		"endTime":             end.Add(time.Second).Format(checkpointTimeLayout),
		"model":               "model-v1",
		"model_group":         "group-v1",
		"custom_llm_provider": "provider-v1",
		"prompt_tokens":       2,
		"completion_tokens":   3,
		"total_tokens":        5,
		"spend":               json.Number("0.001"),
		"cache_hit":           false,
		"api_key":             "must-never-be-retained",
		"messages":            []any{map[string]any{"content": "secret"}},
	}
}
