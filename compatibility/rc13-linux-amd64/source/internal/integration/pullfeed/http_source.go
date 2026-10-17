package pullfeed

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultHTTPConnectTimeout        = 3 * time.Second
	DefaultHTTPResponseHeaderTimeout = 5 * time.Second
	DefaultHTTPRequestTimeout        = 15 * time.Second

	MaxHTTPConnectTimeout        = 10 * time.Second
	MaxHTTPResponseHeaderTimeout = 30 * time.Second
	MaxHTTPRequestTimeout        = time.Minute
	MaxHTTPResponseHeaderBytes   = 64 << 10
	MaxHTTPErrorBodyBytes        = 16 << 10
	MaxServiceKeyBytes           = 4096
	MaxRetryAfter                = 5 * time.Minute

	PausedConnectionErrorCode = "completed_execution_feed.connection_paused"
)

var (
	ErrHTTPSourceConfiguration = errors.New("completed-execution HTTP source configuration is invalid")
	ErrHTTPSourceTransport     = errors.New("completed-execution HTTP source transport failed")
	ErrHTTPSourceBody          = errors.New("completed-execution HTTP source response body failed")
	ErrHTTPSourceBodyTooLarge  = errors.New("completed-execution HTTP source response body is too large")
	ErrHTTPSourceBodyEncoding  = errors.New("completed-execution HTTP source response body encoding is unsupported")
	ErrHTTPSourcePausedAuth    = errors.New("completed-execution HTTP source staged authentication did not prove paused state")
)

type HTTPErrorDisposition string

const (
	HTTPErrorRetryable HTTPErrorDisposition = "retryable"
	HTTPErrorPermanent HTTPErrorDisposition = "permanent"
)

type HTTPErrorCategory string

const (
	HTTPErrorCategoryTransport HTTPErrorCategory = "transport"
	HTTPErrorCategoryStatus    HTTPErrorCategory = "status"
	HTTPErrorCategoryBody      HTTPErrorCategory = "body"
)

// HTTPSourceError is safe to include in worker status and logs. It deliberately
// excludes the endpoint, credential, response body, and underlying URL error.
type HTTPSourceError struct {
	Disposition HTTPErrorDisposition
	Category    HTTPErrorCategory
	StatusCode  int
	Code        string
	RetryAfter  time.Duration
	cause       error
}

func (e *HTTPSourceError) Error() string {
	if e == nil {
		return "completed-execution HTTP source failed"
	}
	if e.Category == HTTPErrorCategoryStatus {
		return fmt.Sprintf(
			"completed-execution HTTP source returned %s status %d",
			e.Disposition,
			e.StatusCode,
		)
	}
	return fmt.Sprintf(
		"completed-execution HTTP source failed with %s %s error",
		e.Disposition,
		e.Category,
	)
}

func (e *HTTPSourceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *HTTPSourceError) IsRetryable() bool {
	return e != nil && e.Disposition == HTTPErrorRetryable
}

type HTTPSourceConfig struct {
	Endpoint               string
	ServiceKey             string
	AllowPrivateTunnelHTTP bool
	ConnectTimeout         time.Duration
	ResponseHeaderTimeout  time.Duration
	RequestTimeout         time.Duration
}

type HTTPSource struct {
	endpoint   url.URL
	serviceKey string
	client     *http.Client
}

func NewHTTPSource(config HTTPSourceConfig) (*HTTPSource, error) {
	endpoint, err := validateHTTPEndpoint(config.Endpoint, config.AllowPrivateTunnelHTTP)
	if err != nil {
		return nil, err
	}
	if err := validateServiceKey(config.ServiceKey); err != nil {
		return nil, err
	}

	connectTimeout, err := boundedHTTPTimeout(
		"connect timeout",
		config.ConnectTimeout,
		DefaultHTTPConnectTimeout,
		MaxHTTPConnectTimeout,
	)
	if err != nil {
		return nil, err
	}
	responseHeaderTimeout, err := boundedHTTPTimeout(
		"response-header timeout",
		config.ResponseHeaderTimeout,
		DefaultHTTPResponseHeaderTimeout,
		MaxHTTPResponseHeaderTimeout,
	)
	if err != nil {
		return nil, err
	}
	requestTimeout, err := boundedHTTPTimeout(
		"request timeout",
		config.RequestTimeout,
		DefaultHTTPRequestTimeout,
		MaxHTTPRequestTimeout,
	)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           2,
		MaxIdleConnsPerHost:    2,
		IdleConnTimeout:        30 * time.Second,
		TLSHandshakeTimeout:    connectTimeout,
		ResponseHeaderTimeout:  responseHeaderTimeout,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: MaxHTTPResponseHeaderBytes,
		DisableCompression:     true,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &HTTPSource{
		endpoint:   *endpoint,
		serviceKey: config.ServiceKey,
		client:     client,
	}, nil
}

func (s *HTTPSource) Fetch(ctx context.Context, cursor string, limit int) ([]byte, error) {
	request, err := s.newRequest(ctx, cursor, limit)
	if err != nil {
		return nil, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, classifyHTTPTransportError(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		disposition := classifyHTTPStatus(response.StatusCode)
		retryAfter := time.Duration(0)
		if disposition == HTTPErrorRetryable {
			retryAfter = parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
		}
		code := ""
		errorPayload, readErr := io.ReadAll(io.LimitReader(response.Body, MaxHTTPErrorBodyBytes+1))
		if readErr == nil && len(errorPayload) <= MaxHTTPErrorBodyBytes {
			if endpointError, ok := decodeHTTPError(errorPayload); ok {
				code = endpointError.Code
				if disposition == HTTPErrorRetryable && !endpointError.Retryable {
					disposition = HTTPErrorPermanent
					retryAfter = 0
				}
			}
		}
		return nil, &HTTPSourceError{
			Disposition: disposition,
			Category:    HTTPErrorCategoryStatus,
			StatusCode:  response.StatusCode,
			Code:        code,
			RetryAfter:  retryAfter,
		}
	}
	if encoding := strings.TrimSpace(response.Header.Get("Content-Encoding")); encoding != "" &&
		!strings.EqualFold(encoding, "identity") {
		return nil, &HTTPSourceError{
			Disposition: HTTPErrorPermanent,
			Category:    HTTPErrorCategoryBody,
			StatusCode:  response.StatusCode,
			cause:       ErrHTTPSourceBodyEncoding,
		}
	}

	payload, err := io.ReadAll(io.LimitReader(response.Body, MaxPageBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, &HTTPSourceError{
			Disposition: HTTPErrorRetryable,
			Category:    HTTPErrorCategoryBody,
			StatusCode:  response.StatusCode,
			cause:       ErrHTTPSourceBody,
		}
	}
	if len(payload) > MaxPageBytes {
		return nil, &HTTPSourceError{
			Disposition: HTTPErrorPermanent,
			Category:    HTTPErrorCategoryBody,
			StatusCode:  response.StatusCode,
			cause:       ErrHTTPSourceBodyTooLarge,
		}
	}
	return payload, nil
}

// VerifyPausedAuthentication proves that the configured credential is
// accepted by the exact feed route while the source is paused. It deliberately
// does not read a successful feed response body: a 200 is a failed preflight,
// not permission to receive source data.
func (s *HTTPSource) VerifyPausedAuthentication(ctx context.Context) error {
	request, err := s.newRequest(ctx, "", 1)
	if err != nil {
		return ErrHTTPSourcePausedAuth
	}
	response, err := s.client.Do(request)
	if err != nil {
		return ErrHTTPSourcePausedAuth
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusServiceUnavailable {
		return ErrHTTPSourcePausedAuth
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return ErrHTTPSourcePausedAuth
	}
	if encoding := strings.TrimSpace(response.Header.Get("Content-Encoding")); encoding != "" &&
		!strings.EqualFold(encoding, "identity") {
		return ErrHTTPSourcePausedAuth
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, MaxHTTPErrorBodyBytes+1))
	if err != nil || len(payload) > MaxHTTPErrorBodyBytes {
		return ErrHTTPSourcePausedAuth
	}
	endpointError, ok := decodeHTTPError(payload)
	if !ok ||
		endpointError.Code != PausedConnectionErrorCode ||
		!endpointError.Retryable {
		return ErrHTTPSourcePausedAuth
	}
	return nil
}

func (s *HTTPSource) newRequest(ctx context.Context, cursor string, limit int) (*http.Request, error) {
	if s == nil || s.client == nil {
		return nil, ErrHTTPSourceConfiguration
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(cursor) > MaxCursorBytes {
		return nil, fmt.Errorf("%w: cursor exceeds %d bytes", ErrHTTPSourceConfiguration, MaxCursorBytes)
	}
	if !utf8.ValidString(cursor) {
		return nil, fmt.Errorf("%w: cursor is not valid UTF-8", ErrHTTPSourceConfiguration)
	}
	if limit < 1 || limit > MaxPageItems {
		return nil, fmt.Errorf(
			"%w: page limit must be between 1 and %d",
			ErrHTTPSourceConfiguration,
			MaxPageItems,
		)
	}

	requestURL := s.endpoint
	query := make(url.Values, 2)
	query.Set("cursor", cursor)
	query.Set("limit", strconv.Itoa(limit))
	requestURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, ErrHTTPSourceConfiguration
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Authorization", "Bearer "+s.serviceKey)
	return request, nil
}

type decodedHTTPError struct {
	Code      string
	Retryable bool
}

func decodeHTTPError(payload []byte) (decodedHTTPError, bool) {
	if len(payload) == 0 || len(payload) > MaxHTTPErrorBodyBytes {
		return decodedHTTPError{}, false
	}
	var envelope struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			Retryable *bool  `json:"retryable"`
		} `json:"error"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return decodedHTTPError{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return decodedHTTPError{}, false
	}
	if !failureCodePattern.MatchString(envelope.Error.Code) ||
		envelope.Error.Retryable == nil ||
		envelope.Error.Message == "" ||
		len(envelope.Error.Message) > 1024 {
		return decodedHTTPError{}, false
	}
	return decodedHTTPError{
		Code:      envelope.Error.Code,
		Retryable: *envelope.Error.Retryable,
	}, true
}

func validateHTTPEndpoint(raw string, allowPrivateTunnelHTTP bool) (*url.URL, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || !utf8.ValidString(raw) {
		return nil, fmt.Errorf("%w: endpoint is required", ErrHTTPSourceConfiguration)
	}
	endpoint, err := url.Parse(raw)
	if err != nil ||
		!endpoint.IsAbs() ||
		endpoint.Host == "" ||
		endpoint.User != nil ||
		endpoint.Fragment != "" ||
		endpoint.RawQuery != "" ||
		endpoint.RawPath != "" {
		return nil, fmt.Errorf("%w: endpoint must be a fixed absolute URL", ErrHTTPSourceConfiguration)
	}
	if endpoint.Scheme == "https" {
		return endpoint, nil
	}
	if endpoint.Scheme != "http" || !allowPrivateTunnelHTTP || !isPrivateTunnelHTTPHost(endpoint.Hostname()) {
		return nil, fmt.Errorf(
			"%w: endpoint must use HTTPS or explicitly allowed private-tunnel HTTP",
			ErrHTTPSourceConfiguration,
		)
	}
	return endpoint, nil
}

func isPrivateTunnelHTTPHost(host string) bool {
	address, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	address = address.Unmap()
	if address.IsLoopback() || address.IsPrivate() {
		return true
	}
	tailscaleCGNAT := netip.MustParsePrefix("100.64.0.0/10")
	return address.Is4() && tailscaleCGNAT.Contains(address)
}

func validateServiceKey(serviceKey string) error {
	if serviceKey == "" ||
		len(serviceKey) > MaxServiceKeyBytes ||
		strings.TrimSpace(serviceKey) != serviceKey ||
		!utf8.ValidString(serviceKey) {
		return fmt.Errorf("%w: service key is invalid", ErrHTTPSourceConfiguration)
	}
	for _, character := range serviceKey {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return fmt.Errorf("%w: service key is invalid", ErrHTTPSourceConfiguration)
		}
	}
	return nil
}

func boundedHTTPTimeout(name string, configured time.Duration, defaultValue time.Duration, maximum time.Duration) (time.Duration, error) {
	if configured == 0 {
		return defaultValue, nil
	}
	if configured < 0 || configured > maximum {
		return 0, fmt.Errorf(
			"%w: %s must be positive and no greater than %s",
			ErrHTTPSourceConfiguration,
			name,
			maximum,
		)
	}
	return configured, nil
}

func classifyHTTPTransportError(err error) *HTTPSourceError {
	disposition := HTTPErrorRetryable
	cause := ErrHTTPSourceTransport
	if errors.Is(err, context.DeadlineExceeded) {
		cause = context.DeadlineExceeded
	} else {
		var unknownAuthorityError x509.UnknownAuthorityError
		var hostnameError x509.HostnameError
		var certificateInvalidError x509.CertificateInvalidError
		var recordHeaderError tls.RecordHeaderError
		if errors.As(err, &unknownAuthorityError) ||
			errors.As(err, &hostnameError) ||
			errors.As(err, &certificateInvalidError) ||
			errors.As(err, &recordHeaderError) {
			disposition = HTTPErrorPermanent
		}
	}
	return &HTTPSourceError{
		Disposition: disposition,
		Category:    HTTPErrorCategoryTransport,
		cause:       cause,
	}
}

func classifyHTTPStatus(statusCode int) HTTPErrorDisposition {
	switch statusCode {
	case http.StatusRequestTimeout,
		http.StatusTooEarly,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return HTTPErrorRetryable
	default:
		return HTTPErrorPermanent
	}
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.ParseUint(raw, 10, 32); err == nil {
		delay := time.Duration(seconds) * time.Second
		if delay <= MaxRetryAfter {
			return delay
		}
		return MaxRetryAfter
	}
	retryAt, err := http.ParseTime(raw)
	if err != nil || !retryAt.After(now) {
		return 0
	}
	delay := retryAt.Sub(now)
	if delay > MaxRetryAfter {
		return MaxRetryAfter
	}
	return delay
}
