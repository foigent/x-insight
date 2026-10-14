package pullfeed

import (
	"context"
	"net/url"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func (s *HTTPSource) FetchBudgetCatalog(ctx context.Context, cursor string) ([]byte, error) {
	if s == nil || s.endpoint.Path != sourcefacts.BudgetEndpointPath || len(cursor) > 4096 {
		return nil, ErrHTTPSourceConfiguration
	}
	r, err := s.newRequest(ctx, cursor, 50)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Cache-Control", "no-store")
	return s.fetchRequestBounded(ctx, r, true, sourcefacts.MaxBudgetCatalogBytes)
}

func (s *HTTPSource) FetchBudgetReading(ctx context.Context, ref, alertsCursor string) ([]byte, error) {
	if s == nil || s.endpoint.Path != sourcefacts.BudgetEndpointPath || ref == "" || len(ref) > 1024 || len(alertsCursor) > 4096 {
		return nil, ErrHTTPSourceConfiguration
	}
	r, err := s.newRequest(ctx, "", 50)
	if err != nil {
		return nil, err
	}
	r.URL.Path = sourcefacts.BudgetEndpointPath + "/current"
	q := url.Values{"budget_ref": {ref}, "limit": {"50"}}
	if alertsCursor != "" {
		q.Set("alerts_cursor", alertsCursor)
	}
	r.URL.RawQuery = q.Encode()
	r.Header.Set("Cache-Control", "no-store")
	return s.fetchRequestBounded(ctx, r, true, sourcefacts.MaxBudgetReadingBytes)
}
