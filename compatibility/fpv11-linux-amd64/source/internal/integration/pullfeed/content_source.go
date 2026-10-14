package pullfeed

import (
	"context"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"net/http"
	"net/url"
)

func (s *HTTPSource) FetchContentCapabilities(ctx context.Context) ([]byte, error) {
	if s == nil || s.endpoint.Path != sourcefacts.ContentEndpointPath {
		return nil, ErrHTTPSourceConfiguration
	}
	r, e := s.newRequest(ctx, "", 1)
	if e != nil {
		return nil, e
	}
	r.URL.Path = sourcefacts.ContentCapabilitiesPath
	r.URL.RawQuery = ""
	r.Header.Set("Cache-Control", "no-store")
	return s.fetchRequestBounded(ctx, r, true, sourcefacts.MaxDocumentBytes)
}
func (s *HTTPSource) FetchContentManifests(ctx context.Context, cursor string, limit int) ([]byte, error) {
	if s == nil || s.endpoint.Path != sourcefacts.ContentEndpointPath || len(cursor) > 4096 || limit < 1 || limit > 50 {
		return nil, ErrHTTPSourceConfiguration
	}
	r, e := s.newRequest(ctx, cursor, limit)
	if e != nil {
		return nil, e
	}
	q := r.URL.Query()
	q.Set("purpose", sourcefacts.ContentPurposeHumanReview)
	if cursor == "" {
		q.Del("cursor")
	}
	r.URL.RawQuery = q.Encode()
	r.Header.Set("Cache-Control", "no-store")
	return s.fetchRequestBounded(ctx, r, true, sourcefacts.MaxPageBytes-1)
}
func isContentRequest(r *http.Request) bool {
	return r != nil && (r.URL.Path == sourcefacts.ContentEndpointPath || r.URL.Path == sourcefacts.ContentCapabilitiesPath || stringsContentPath(r.URL))
}
func stringsContentPath(u *url.URL) bool {
	const prefix = "/api/v1/integrations/xinsight/source/v2/contents/"
	return len(u.Path) > len(prefix) && u.Path[:len(prefix)] == prefix
}

// Source endpoints retain the exact source-error shell. R2 status/code/retryable
// combinations are checked together; their code names never pause a connection.
func decodeContentHTTPError(raw []byte, status int) (decodedHTTPError, bool) {
	m, e := connector.DecodeStrictJSONObject(raw)
	if e != nil || len(m) != 1 {
		return decodedHTTPError{}, false
	}
	v, ok := m["error"].(map[string]any)
	if !ok || len(v) != 8 {
		return decodedHTTPError{}, false
	}
	for k, want := range map[string]string{"error_version": "foigent.connector-error.v1", "category": "source", "phase": "fetch", "operation": "read_source", "safe_message": "Source facts request could not be completed", "recovery": "operator_repair"} {
		if v[k] != want {
			return decodedHTTPError{}, false
		}
	}
	code, ok := v["code"].(string)
	if !ok || !failureCodePattern.MatchString(code) {
		return decodedHTTPError{}, false
	}
	retry, ok := v["retryable"].(bool)
	if !ok {
		return decodedHTTPError{}, false
	}
	switch code {
	case "profile_not_authorized":
		if status != 403 || retry {
			return decodedHTTPError{}, false
		}
	case "source_paused":
		if status != 503 || !retry {
			return decodedHTTPError{}, false
		}
	case "staged_credential_requires_pause":
		if status != 409 || retry {
			return decodedHTTPError{}, false
		}
	}
	return decodedHTTPError{Code: code, Retryable: retry}, true
}
