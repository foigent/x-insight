package pullfeed

import (
	"context"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

func (s *HTTPSource) FetchConnectorSourceDescriptor(ctx context.Context, id string) ([]byte, error) {
	if s == nil || s.endpoint.Scheme != "https" || s.endpoint.Path != connectorcontrol.ReceiverFeedRoute || !connectorcontrol.IsInstallationID(id) {
		return nil, ErrHTTPSourceConfiguration
	}
	r, e := s.newRequest(ctx, "", 1)
	if e != nil {
		return nil, e
	}
	r.URL.Path = "/api/v1/integrations/xinsight/connector-installations/" + id + "/source-binding"
	r.URL.RawQuery = ""
	r.Header.Set("Cache-Control", "no-store")
	return s.fetchRequestBounded(ctx, r, true, 16<<10)
}
