package pullfeed

import (
	"context"
	"errors"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"io"
	"net/http"
	"strings"
	"testing"
)

type contentRoundTripper func(*http.Request) (*http.Response, error)

func (f contentRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func contentHTTPFixture(t *testing.T, f contentRoundTripper) *HTTPSource {
	t.Helper()
	s, e := NewHTTPSource(HTTPSourceConfig{Endpoint: "https://source.invalid" + sourcefacts.ContentEndpointPath, ServiceKey: "synthetic-content-key"})
	if e != nil {
		t.Fatal(e)
	}
	s.client = &http.Client{Transport: f}
	return s
}
func contentHTTPResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Cache-Control": []string{"no-store"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func contentErrorBody(code string, retry bool) string {
	b := "false"
	if retry {
		b = "true"
	}
	return `{"error":{"error_version":"foigent.connector-error.v1","category":"source","code":"` + code + `","phase":"fetch","operation":"read_source","retryable":` + b + `,"safe_message":"Source facts request could not be completed","recovery":"operator_repair"}}`
}
func TestContentHTTPMetadataPathsAndLimits(t *testing.T) {
	paths := []string{}
	s := contentHTTPFixture(t, func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer synthetic-content-key" || r.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("request headers")
		}
		if r.URL.Path == sourcefacts.ContentEndpointPath && r.URL.Query().Get("purpose") != sourcefacts.ContentPurposeHumanReview {
			t.Fatal("fixed purpose missing")
		}
		if r.URL.Path == sourcefacts.ContentCapabilitiesPath && r.URL.RawQuery != "" {
			t.Fatal("capabilities query")
		}
		return contentHTTPResponse(200, `{}`), nil
	})
	if _, e := s.FetchContentCapabilities(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e := s.FetchContentManifests(context.Background(), "cursor", 50); e != nil {
		t.Fatal(e)
	}
	if len(paths) != 2 || paths[0] != sourcefacts.ContentCapabilitiesPath || paths[1] != sourcefacts.ContentEndpointPath {
		t.Fatal(paths)
	}
	if _, e := s.FetchContentManifests(context.Background(), "", 51); e == nil {
		t.Fatal("unbounded limit")
	}
	s.client.Transport = contentRoundTripper(func(r *http.Request) (*http.Response, error) {
		return contentHTTPResponse(200, strings.Repeat("x", sourcefacts.MaxDocumentBytes+1)), nil
	})
	if _, e := s.FetchContentCapabilities(context.Background()); !errors.Is(e, ErrHTTPSourceBodyTooLarge) {
		t.Fatal(e)
	}
	s.endpoint.Path = "/api/v1/payload"
	if _, e := s.FetchContentCapabilities(context.Background()); !errors.Is(e, ErrHTTPSourceConfiguration) {
		t.Fatal("legacy fallback", e)
	}
}
func TestContentHTTPR2ExactErrorShell(t *testing.T) {
	for _, v := range []struct {
		status int
		code   string
		retry  bool
	}{{403, "profile_not_authorized", false}, {503, "source_paused", true}, {409, "staged_credential_requires_pause", false}} {
		t.Run(v.code, func(t *testing.T) {
			s := contentHTTPFixture(t, func(*http.Request) (*http.Response, error) {
				return contentHTTPResponse(v.status, contentErrorBody(v.code, v.retry)), nil
			})
			_, e := s.FetchContentCapabilities(context.Background())
			var h *HTTPSourceError
			if !errors.As(e, &h) || h.Code != v.code || h.IsRetryable() != v.retry {
				t.Fatalf("%#v", e)
			}
			if _, ok := decodeContentHTTPError([]byte(contentErrorBody(v.code, !v.retry)), v.status); ok {
				t.Fatal("contradictory retry accepted")
			}
			if _, ok := decodeContentHTTPError([]byte(contentErrorBody(v.code, v.retry)), 400); ok {
				t.Fatal("wrong status accepted")
			}
		})
	}
}
func TestContentHTTPNoStoreAndCancellation(t *testing.T) {
	s := contentHTTPFixture(t, func(*http.Request) (*http.Response, error) {
		r := contentHTTPResponse(200, `{}`)
		r.Header.Del("Cache-Control")
		return r, nil
	})
	if _, e := s.FetchContentCapabilities(context.Background()); !errors.Is(e, ErrHTTPSourceBody) {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := s.FetchContentCapabilities(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
