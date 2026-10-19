package connectorsync

import "net/http"

type requiredTransport struct{ base http.RoundTripper }

func (t requiredTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if p, _ := r.Context().Value(contextKey{}).(*permit); p == nil {
		return nil, ErrBinding
	}
	return t.base.RoundTrip(r)
}

// The request helpers count attempts. This wrapper only prevents a production
// factory accidentally emitting an unbound request through another call path.
func RequireClient(c *http.Client) *http.Client {
	if c == nil {
		return nil
	}
	copy := *c
	base := copy.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	copy.Transport = requiredTransport{base}
	return &copy
}
