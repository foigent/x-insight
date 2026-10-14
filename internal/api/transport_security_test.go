package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	productaccess "github.com/foigent/x-insight/internal/access"
)

func TestTransportSecurityDefaultsToDirectTLSOnly(t *testing.T) {
	security, err := NewTransportSecurity("", "")
	if err != nil {
		t.Fatal(err)
	}
	direct := httptest.NewRequest(http.MethodPost, "https://xinsight.example/api/v1/access/login", nil)
	direct.Header.Set("Origin", "https://xinsight.example")
	direct.Header.Set("Sec-Fetch-Site", "same-origin")
	if !security.requireSameOriginHTTPS(direct) {
		t.Fatal("direct same-origin TLS was rejected")
	}

	spoofed := httptest.NewRequest(http.MethodPost, "http://xinsight.example/api/v1/access/login", nil)
	spoofed.RemoteAddr = "127.0.0.1:42000"
	spoofed.Header.Set("Origin", "https://xinsight.example")
	spoofed.Header.Set(forwardedProtoHeader, "https")
	spoofed.Header.Set(forwardedHostHeader, "xinsight.example")
	if security.requireSameOriginHTTPS(spoofed) {
		t.Fatal("forwarded TLS was trusted without explicit proxy configuration")
	}
}

func TestTransportSecurityAcceptsOnlyPinnedLoopbackTLSProxy(t *testing.T) {
	security, err := NewTransportSecurity("loopback", "https://8.130.79.92")
	if err != nil {
		t.Fatal(err)
	}
	valid := proxiedRequest("127.0.0.1:42000", "8.130.79.92", "https://8.130.79.92")
	if !security.requireSameOriginHTTPS(valid) {
		t.Fatal("pinned loopback TLS proxy was rejected")
	}

	tests := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"remote", func(request *http.Request) { request.RemoteAddr = "192.0.2.4:42000" }},
		{"proto", func(request *http.Request) { request.Header.Set(forwardedProtoHeader, "http") }},
		{"forwarded-host", func(request *http.Request) { request.Header.Set(forwardedHostHeader, "attacker.invalid") }},
		{"host", func(request *http.Request) { request.Host = "attacker.invalid" }},
		{"origin", func(request *http.Request) { request.Header.Set("Origin", "https://attacker.invalid") }},
		{"forwarded", func(request *http.Request) { request.Header.Set("Forwarded", "proto=https") }},
		{"cross-site", func(request *http.Request) { request.Header.Set("Sec-Fetch-Site", "cross-site") }},
		{"duplicate-proto", func(request *http.Request) { request.Header.Add(forwardedProtoHeader, "https") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := proxiedRequest("127.0.0.1:42000", "8.130.79.92", "https://8.130.79.92")
			test.mutate(request)
			if security.requireSameOriginHTTPS(request) {
				t.Fatal("unsafe proxy request was accepted")
			}
		})
	}
}

func TestTransportSecurityConfigurationIsStrict(t *testing.T) {
	for _, input := range []struct {
		mode   string
		origin string
	}{
		{"loopback", ""},
		{"other", "https://xinsight.example"},
		{"loopback", "http://xinsight.example"},
		{"loopback", "https://user@xinsight.example"},
		{"loopback", "https://xinsight.example/"},
		{"loopback", "https://xinsight.example?query"},
	} {
		if _, err := NewTransportSecurity(input.mode, input.origin); err == nil {
			t.Fatalf("mode=%q origin=%q unexpectedly accepted", input.mode, input.origin)
		}
	}
}

func TestAuthenticatedHandlerAcceptsPinnedProxyLoginAndEmitsHSTS(t *testing.T) {
	hash, err := productaccess.DerivePasswordHash([]byte("test password"))
	if err != nil {
		t.Fatal(err)
	}
	provider, err := productaccess.NewLocalProvider(productaccess.AccountConfig{
		SchemaVersion: productaccess.AccountConfigSchema,
		Accounts: []productaccess.AccountConfigEntry{{
			SubjectID:    "xs_0123456789abcdef",
			Username:     "investigator.one",
			Role:         productaccess.RoleInvestigator,
			Disabled:     false,
			PasswordHash: hash,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := productaccess.NewService(
		provider,
		productaccess.NewWriterAuditor(io.Discard),
		productaccess.ServiceOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	security, err := NewTransportSecurity("loopback", "https://xinsight.example")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAuthenticatedHandlerWithTransport(nil, nil, service, security)
	request, err := http.NewRequest(
		http.MethodPost,
		"http://xinsight.example/api/v1/access/login",
		strings.NewReader(`{"username":"investigator.one","password":"test password"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "xinsight.example"
	request.RemoteAddr = "127.0.0.1:42000"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://xinsight.example")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set(forwardedProtoHeader, "https")
	request.Header.Set(forwardedHostHeader, "xinsight.example")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, recorder.Body.String())
	}
	if response.Header.Get("Strict-Transport-Security") != "max-age=31536000" {
		t.Fatalf("missing HSTS: %q", response.Header.Get("Strict-Transport-Security"))
	}
	cookies := response.Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("unsafe Session cookie: %+v", cookies)
	}
}

func proxiedRequest(remoteAddress string, host string, origin string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/v1/access/login", nil)
	request.RemoteAddr = remoteAddress
	request.Host = host
	request.Header.Set("Origin", origin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set(forwardedProtoHeader, "https")
	request.Header.Set(forwardedHostHeader, host)
	return request
}
