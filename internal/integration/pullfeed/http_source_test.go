package pullfeed_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/integration/pullfeed"
)

func TestHTTPSourceFetchUsesFixedEndpointAndServiceKey(t *testing.T) {
	const serviceKey = "private-service-key"
	var targetRequests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		targetRequests.Add(1)
		if request.Method != http.MethodGet {
			t.Errorf("method=%q", request.Method)
		}
		if request.URL.Path != "/api/v1/integrations/xinsight/completed-executions" {
			t.Errorf("path=%q", request.URL.Path)
		}
		if request.URL.Query().Get("cursor") != "cursor/+? opaque" {
			t.Errorf("cursor=%q raw_query=%q", request.URL.Query().Get("cursor"), request.URL.RawQuery)
		}
		if request.URL.Query().Get("limit") != "50" {
			t.Errorf("limit=%q", request.URL.Query().Get("limit"))
		}
		if request.Header.Get("Authorization") != "Bearer "+serviceKey {
			t.Errorf("authorization header was not supplied")
		}
		if request.Header.Get("Accept") != "application/json" || request.Header.Get("Accept-Encoding") != "identity" {
			t.Errorf("headers=%v", request.Header)
		}
		_, _ = io.WriteString(w, `{"page":"ok"}`)
	}))
	defer target.Close()

	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	source := newPrivateTestHTTPSource(
		t,
		target.URL+"/api/v1/integrations/xinsight/completed-executions",
		serviceKey,
	)
	payload, err := source.Fetch(context.Background(), "cursor/+? opaque", pullfeed.MaxPageItems)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(payload) != `{"page":"ok"}` || targetRequests.Load() != 1 {
		t.Fatalf("payload=%q requests=%d", payload, targetRequests.Load())
	}
}

func TestHTTPSourceVerifyPausedAuthenticationRequiresExactStrict503(t *testing.T) {
	const serviceKey = "ur-xf1-" + "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet ||
			request.URL.Path != "/feed" ||
			request.URL.Query().Get("cursor") != "" ||
			request.URL.Query().Get("limit") != "1" ||
			request.Header.Get("Authorization") != "Bearer "+serviceKey {
			t.Fatalf("unexpected staged-authentication request")
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(
			writer,
			`{"error":{"code":"completed_execution_feed.connection_paused","message":"Completed-execution feed connection is paused","retryable":true}}`,
		)
	}))
	defer server.Close()

	source := newPrivateTestHTTPSource(t, server.URL+"/feed", serviceKey)
	if err := source.VerifyPausedAuthentication(context.Background()); err != nil {
		t.Fatalf("verify paused staged authentication: %v", err)
	}
}

func TestHTTPSourceVerifyPausedAuthenticationRejectsEveryNonProofResponse(t *testing.T) {
	const serviceKey = "ur-xf1-" + "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff"
	tests := map[string]struct {
		status      int
		contentType string
		body        string
	}{
		"unauthorized": {
			status:      http.StatusUnauthorized,
			contentType: "application/json",
			body:        `{"error":{"code":"auth.invalid","message":"Denied","retryable":false}}`,
		},
		"forbidden": {
			status:      http.StatusForbidden,
			contentType: "application/json",
			body:        `{"error":{"code":"auth.forbidden","message":"Denied","retryable":false}}`,
		},
		"success": {
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"schema_version":"xinsight.completed-execution-feed.v1","items":[]}`,
		},
		"wrong retryability": {
			status:      http.StatusServiceUnavailable,
			contentType: "application/json",
			body:        `{"error":{"code":"completed_execution_feed.connection_paused","message":"Paused","retryable":false}}`,
		},
		"wrong code": {
			status:      http.StatusServiceUnavailable,
			contentType: "application/json",
			body:        `{"error":{"code":"completed_execution_feed.overloaded","message":"Overloaded","retryable":true}}`,
		},
		"unknown field": {
			status:      http.StatusServiceUnavailable,
			contentType: "application/json",
			body:        `{"error":{"code":"completed_execution_feed.connection_paused","message":"Paused","retryable":true,"extra":"rejected"}}`,
		},
		"wrong media type": {
			status:      http.StatusServiceUnavailable,
			contentType: "text/plain",
			body:        `{"error":{"code":"completed_execution_feed.connection_paused","message":"Paused","retryable":true}}`,
		},
	}
	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", testCase.contentType)
				writer.WriteHeader(testCase.status)
				_, _ = io.WriteString(writer, testCase.body)
			}))
			defer server.Close()

			source := newPrivateTestHTTPSource(t, server.URL+"/feed", serviceKey)
			err := source.VerifyPausedAuthentication(context.Background())
			if !errors.Is(err, pullfeed.ErrHTTPSourcePausedAuth) {
				t.Fatalf("error=%v", err)
			}
			if strings.Contains(err.Error(), serviceKey) || strings.Contains(err.Error(), server.URL) {
				t.Fatal("staged-authentication error leaked credential or endpoint")
			}
		})
	}
}

func TestHTTPSourceVerifyPausedAuthenticationRejectsTransportFailure(t *testing.T) {
	const serviceKey = "ur-xf1-" + "ffeeddccbbaa00998877665544332211ffeeddccbbaa00998877665544332211"
	source := newPrivateTestHTTPSource(t, "http://127.0.0.1:1/feed", serviceKey)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := source.VerifyPausedAuthentication(ctx)
	if !errors.Is(err, pullfeed.ErrHTTPSourcePausedAuth) {
		t.Fatalf("error=%v", err)
	}
	if strings.Contains(err.Error(), serviceKey) || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatal("transport preflight error leaked credential or endpoint")
	}
}

func TestHTTPSourceRejectsUnsafeConfigurationAndFetchInputsWithoutLeaks(t *testing.T) {
	const secret = "secret-value-must-not-leak"
	for name, config := range map[string]pullfeed.HTTPSourceConfig{
		"public HTTP": {
			Endpoint:               "http://example.com/feed",
			ServiceKey:             secret,
			AllowPrivateTunnelHTTP: true,
		},
		"HTTP not explicitly enabled": {
			Endpoint:   "http://127.0.0.1/feed",
			ServiceKey: secret,
		},
		"endpoint query": {
			Endpoint:   "https://private.example/feed?tenant=one",
			ServiceKey: secret,
		},
		"endpoint user info": {
			Endpoint:   "https://user:password@private.example/feed",
			ServiceKey: secret,
		},
		"blank service key": {
			Endpoint: "https://private.example/feed",
		},
		"service key whitespace": {
			Endpoint:   "https://private.example/feed",
			ServiceKey: "not allowed",
		},
		"excessive request timeout": {
			Endpoint:       "https://private.example/feed",
			ServiceKey:     secret,
			RequestTimeout: pullfeed.MaxHTTPRequestTimeout + time.Nanosecond,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := pullfeed.NewHTTPSource(config)
			if !errors.Is(err, pullfeed.ErrHTTPSourceConfiguration) {
				t.Fatalf("error=%v", err)
			}
			if strings.Contains(err.Error(), secret) ||
				strings.Contains(err.Error(), config.Endpoint) ||
				strings.Contains(err.Error(), "user:password") {
				t.Fatalf("configuration error leaked private input: %v", err)
			}
		})
	}

	source := newPrivateTestHTTPSource(t, "http://127.0.0.1:1/feed", secret)
	for name, testCase := range map[string]struct {
		cursor string
		limit  int
	}{
		"cursor too long": {cursor: strings.Repeat("c", pullfeed.MaxCursorBytes+1), limit: 1},
		"limit zero":      {cursor: "cursor", limit: 0},
		"limit too high":  {cursor: "cursor", limit: pullfeed.MaxPageItems + 1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := source.Fetch(context.Background(), testCase.cursor, testCase.limit)
			if !errors.Is(err, pullfeed.ErrHTTPSourceConfiguration) {
				t.Fatalf("error=%v", err)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), testCase.cursor) {
				t.Fatalf("fetch error leaked private input: %v", err)
			}
		})
	}
}

func TestHTTPSourceDoesNotFollowRedirect(t *testing.T) {
	var targetRequests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetRequests.Add(1)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	source := newPrivateTestHTTPSource(t, redirect.URL, "private-key")
	_, err := source.Fetch(context.Background(), "cursor", 1)
	assertHTTPSourceError(t, err, pullfeed.HTTPErrorPermanent, pullfeed.HTTPErrorCategoryStatus, http.StatusTemporaryRedirect)
	if targetRequests.Load() != 0 {
		t.Fatalf("redirect target requests=%d", targetRequests.Load())
	}
	if strings.Contains(err.Error(), target.URL) || strings.Contains(err.Error(), redirect.URL) {
		t.Fatalf("error leaked private URL: %v", err)
	}
}

func TestHTTPSourceClassifiesStatusesAndRetryAfterWithoutBodyLeak(t *testing.T) {
	const privateBody = "private-body-must-not-leak"
	for name, testCase := range map[string]struct {
		statusCode  int
		disposition pullfeed.HTTPErrorDisposition
	}{
		"unauthorized":        {statusCode: http.StatusUnauthorized, disposition: pullfeed.HTTPErrorPermanent},
		"rate limited":        {statusCode: http.StatusTooManyRequests, disposition: pullfeed.HTTPErrorRetryable},
		"service unavailable": {statusCode: http.StatusServiceUnavailable, disposition: pullfeed.HTTPErrorRetryable},
		"not implemented":     {statusCode: http.StatusNotImplemented, disposition: pullfeed.HTTPErrorPermanent},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(testCase.statusCode)
				_, _ = io.WriteString(w, privateBody)
			}))
			defer server.Close()

			source := newPrivateTestHTTPSource(t, server.URL, "private-key")
			_, err := source.Fetch(context.Background(), "cursor", 1)
			fetchError := assertHTTPSourceError(
				t,
				err,
				testCase.disposition,
				pullfeed.HTTPErrorCategoryStatus,
				testCase.statusCode,
			)
			expectedRetryAfter := time.Duration(0)
			if testCase.disposition == pullfeed.HTTPErrorRetryable {
				expectedRetryAfter = 7 * time.Second
			}
			if fetchError.RetryAfter != expectedRetryAfter {
				t.Fatalf("retry_after=%s", fetchError.RetryAfter)
			}
			if strings.Contains(err.Error(), privateBody) || strings.Contains(err.Error(), server.URL) {
				t.Fatalf("error leaked private response data: %v", err)
			}
		})
	}
}

func TestHTTPSourceStructuredErrorCanOnlyNarrowStatusRetryability(t *testing.T) {
	for name, testCase := range map[string]struct {
		statusCode          int
		body                string
		expectedDisposition pullfeed.HTTPErrorDisposition
		expectedCode        string
	}{
		"500 valid permanent endpoint error": {
			statusCode:          http.StatusInternalServerError,
			body:                `{"error":{"code":"completed_execution_feed.integrity_failed","message":"retained page integrity failed","retryable":false}}`,
			expectedDisposition: pullfeed.HTTPErrorPermanent,
			expectedCode:        "completed_execution_feed.integrity_failed",
		},
		"500 unknown error remains retryable": {
			statusCode:          http.StatusInternalServerError,
			body:                `{"unknown":true}`,
			expectedDisposition: pullfeed.HTTPErrorRetryable,
		},
		"403 retryable claim cannot elevate": {
			statusCode:          http.StatusForbidden,
			body:                `{"error":{"code":"completed_execution_feed.forbidden","message":"feed access denied","retryable":true}}`,
			expectedDisposition: pullfeed.HTTPErrorPermanent,
			expectedCode:        "completed_execution_feed.forbidden",
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(testCase.statusCode)
				_, _ = io.WriteString(w, testCase.body)
			}))
			defer server.Close()

			source := newPrivateTestHTTPSource(t, server.URL, "private-key")
			_, err := source.Fetch(context.Background(), "cursor", 1)
			fetchError := assertHTTPSourceError(
				t,
				err,
				testCase.expectedDisposition,
				pullfeed.HTTPErrorCategoryStatus,
				testCase.statusCode,
			)
			if fetchError.Code != testCase.expectedCode {
				t.Fatalf("code=%q", fetchError.Code)
			}
			if testCase.expectedDisposition == pullfeed.HTTPErrorPermanent &&
				fetchError.RetryAfter != 0 {
				t.Fatalf("permanent retry_after=%s", fetchError.RetryAfter)
			}
		})
	}
}

func TestHTTPSourceEncodedUnknown500RemainsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "not-a-decodable-structured-error")
	}))
	defer server.Close()

	source := newPrivateTestHTTPSource(t, server.URL, "private-key")
	_, err := source.Fetch(context.Background(), "cursor", 1)
	fetchError := assertHTTPSourceError(
		t,
		err,
		pullfeed.HTTPErrorRetryable,
		pullfeed.HTTPErrorCategoryStatus,
		http.StatusInternalServerError,
	)
	if fetchError.Code != "" {
		t.Fatalf("code=%q want empty", fetchError.Code)
	}
}

func TestHTTPSourceBoundsResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", pullfeed.MaxPageBytes+1))
	}))
	defer server.Close()

	source := newPrivateTestHTTPSource(t, server.URL, "private-key")
	_, err := source.Fetch(context.Background(), "cursor", 1)
	assertHTTPSourceError(t, err, pullfeed.HTTPErrorPermanent, pullfeed.HTTPErrorCategoryBody, http.StatusOK)
	if !errors.Is(err, pullfeed.ErrHTTPSourceBodyTooLarge) {
		t.Fatalf("error=%v", err)
	}
}

func TestHTTPSourceRejectsEncodedBodySoDecodedBytesRemainBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = io.WriteString(w, "compressed-private-body")
	}))
	defer server.Close()

	source := newPrivateTestHTTPSource(t, server.URL, "private-key")
	_, err := source.Fetch(context.Background(), "cursor", 1)
	assertHTTPSourceError(t, err, pullfeed.HTTPErrorPermanent, pullfeed.HTTPErrorCategoryBody, http.StatusOK)
	if !errors.Is(err, pullfeed.ErrHTTPSourceBodyEncoding) {
		t.Fatalf("error=%v", err)
	}
}

func TestHTTPSourceRespectsCallerCancellationAndClassifiesRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer server.Close()

	t.Run("caller cancellation", func(t *testing.T) {
		source := newPrivateTestHTTPSource(t, server.URL, "private-key")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := source.Fetch(ctx, "cursor", 1)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("bounded request timeout", func(t *testing.T) {
		source, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{
			Endpoint:               server.URL,
			ServiceKey:             "private-key",
			AllowPrivateTunnelHTTP: true,
			RequestTimeout:         20 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("new HTTP source: %v", err)
		}
		_, err = source.Fetch(context.Background(), "cursor", 1)
		fetchError := assertHTTPSourceError(t, err, pullfeed.HTTPErrorRetryable, pullfeed.HTTPErrorCategoryTransport, 0)
		if !errors.Is(fetchError, context.DeadlineExceeded) {
			t.Fatalf("error=%v", fetchError)
		}
	})
}

func newPrivateTestHTTPSource(t *testing.T, endpoint string, serviceKey string) *pullfeed.HTTPSource {
	t.Helper()
	source, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{
		Endpoint:               endpoint,
		ServiceKey:             serviceKey,
		AllowPrivateTunnelHTTP: true,
	})
	if err != nil {
		t.Fatalf("new HTTP source: %v", err)
	}
	return source
}

func assertHTTPSourceError(
	t *testing.T,
	err error,
	disposition pullfeed.HTTPErrorDisposition,
	category pullfeed.HTTPErrorCategory,
	statusCode int,
) *pullfeed.HTTPSourceError {
	t.Helper()
	var fetchError *pullfeed.HTTPSourceError
	if !errors.As(err, &fetchError) {
		t.Fatalf("error=%T %v", err, err)
	}
	if fetchError.Disposition != disposition ||
		fetchError.Category != category ||
		fetchError.StatusCode != statusCode ||
		fetchError.IsRetryable() != (disposition == pullfeed.HTTPErrorRetryable) {
		t.Fatalf("HTTP source error=%+v", fetchError)
	}
	return fetchError
}
