package api

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const (
	TrustedTLSProxyModeEnv = "XINSIGHT_TRUSTED_TLS_PROXY_MODE"
	ExternalOriginEnv      = "XINSIGHT_EXTERNAL_ORIGIN"

	trustedTLSProxyModeLoopback = "loopback"
	forwardedProtoHeader        = "X-Forwarded-Proto"
	forwardedHostHeader         = "X-Forwarded-Host"
)

// TransportSecurity keeps direct TLS as the default and allows one explicit
// deployment contract: a TLS terminator connecting from loopback and
// forwarding one pinned HTTPS origin. The zero value is direct-TLS-only.
type TransportSecurity struct {
	trustLoopbackProxy bool
	externalOrigin     *url.URL
}

func NewTransportSecurity(proxyMode string, externalOrigin string) (TransportSecurity, error) {
	proxyMode = strings.TrimSpace(proxyMode)
	externalOrigin = strings.TrimSpace(externalOrigin)
	if proxyMode == "" && externalOrigin == "" {
		return TransportSecurity{}, nil
	}
	if proxyMode != trustedTLSProxyModeLoopback || externalOrigin == "" {
		return TransportSecurity{}, errors.New(
			"trusted TLS proxy requires loopback mode and one external HTTPS origin",
		)
	}
	origin, err := url.Parse(externalOrigin)
	if err != nil ||
		origin.Scheme != "https" ||
		origin.User != nil ||
		origin.Host == "" ||
		origin.Path != "" ||
		origin.RawPath != "" ||
		origin.ForceQuery ||
		origin.RawQuery != "" ||
		origin.Fragment != "" ||
		origin.String() != externalOrigin {
		return TransportSecurity{}, errors.New("external origin must be one canonical HTTPS origin")
	}
	return TransportSecurity{
		trustLoopbackProxy: true,
		externalOrigin:     origin,
	}, nil
}

func (security TransportSecurity) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !security.trustLoopbackProxy || security.externalOrigin == nil || !remoteIsLoopback(r.RemoteAddr) {
		return false
	}
	proto, protoOK := oneHeaderValue(r, forwardedProtoHeader)
	forwardedHost, hostOK := oneHeaderValue(r, forwardedHostHeader)
	if !protoOK || !hostOK ||
		proto != "https" ||
		!sameHost(forwardedHost, security.externalOrigin.Host) ||
		!sameHost(r.Host, security.externalOrigin.Host) {
		return false
	}
	return len(r.Header.Values("Forwarded")) == 0
}

func (security TransportSecurity) requireSameOriginHTTPS(r *http.Request) bool {
	if !security.isHTTPS(r) || r.Host == "" {
		return false
	}
	origins := r.Header.Values("Origin")
	if len(origins) != 1 {
		return false
	}
	origin, err := url.Parse(origins[0])
	if err != nil ||
		origin.Scheme != "https" ||
		origin.User != nil ||
		origin.Host == "" ||
		origin.Path != "" ||
		origin.RawPath != "" ||
		origin.ForceQuery ||
		origin.RawQuery != "" ||
		origin.Fragment != "" ||
		!sameHost(origin.Host, r.Host) {
		return false
	}
	if r.TLS == nil && (security.externalOrigin == nil || origin.String() != security.externalOrigin.String()) {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return false
	}
	return true
}

func remoteIsLoopback(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func oneHeaderValue(r *http.Request, name string) (string, bool) {
	values := r.Header.Values(name)
	if len(values) != 1 {
		return "", false
	}
	value := strings.TrimSpace(values[0])
	return value, value != "" && !strings.Contains(value, ",")
}

func sameHost(left string, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}
