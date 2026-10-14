package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTokenBucketIsBoundedAndRefills(t *testing.T) {
	start := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	bucket := &tokenBucket{rate: 2, burst: 2, tokens: 2, last: start}
	if !bucket.allow(start) || !bucket.allow(start) || bucket.allow(start) {
		t.Fatal("burst accounting is not bounded")
	}
	if !bucket.allow(start.Add(500 * time.Millisecond)) {
		t.Fatal("bucket did not refill one token")
	}
}

func TestAPILimitsReturnStableProblems(t *testing.T) {
	testCases := []struct {
		name       string
		server     *Server
		requestURI string
		status     int
		code       string
	}{
		{
			name: "request URI",
			server: &Server{
				apiCapacity: make(chan struct{}, 1),
				rateLimiter: newTokenBucket(20, 40),
			},
			requestURI: "/api/v1/executions?request_id=" + strings.Repeat("a", maxRequestURIBytes),
			status:     http.StatusBadRequest,
			code:       "invalid_argument",
		},
		{
			name: "rate",
			server: &Server{
				apiCapacity: make(chan struct{}, 1),
				rateLimiter: &tokenBucket{rate: 0, burst: 0, tokens: 0, last: time.Now()},
			},
			requestURI: "/api/v1/executions",
			status:     http.StatusTooManyRequests,
			code:       "request_rate_limited",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			handler := testCase.server.withAPILimits(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, testCase.requestURI, nil)
			handler.ServeHTTP(response, request)
			if response.Code != testCase.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var problem Problem
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if problem.Code != testCase.code {
				t.Fatalf("problem=%+v", problem)
			}
		})
	}
}

func TestAPILimitsFailFastWhenConcurrencyIsFull(t *testing.T) {
	capacity := make(chan struct{}, 1)
	capacity <- struct{}{}
	server := &Server{apiCapacity: capacity, rateLimiter: newTokenBucket(20, 40)}
	handler := server.withAPILimits(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream handler must not run when capacity is full")
	}))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/executions", nil)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "temporarily_overloaded" || !problem.Retryable {
		t.Fatalf("problem=%+v", problem)
	}
}
