package litellmfeed

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/adapters/litellm"
)

const (
	defaultConnectTimeout        = 3 * time.Second
	defaultResponseHeaderTimeout = 5 * time.Second
	defaultRequestTimeout        = 20 * time.Second
	maxHeaderBytes               = 64 << 10
	maxErrorBodyBytes            = 16 << 10
	maxCredentialBytes           = 4096
	liteLLMQueryTimeLayout       = "2006-01-02 15:04:05"
)

type SourceError struct {
	Code       string
	Retryable  bool
	StatusCode int
	cause      error
}

func (sourceErr *SourceError) Error() string {
	if sourceErr == nil {
		return "LiteLLM spend-log Source failed"
	}
	if sourceErr.StatusCode != 0 {
		return fmt.Sprintf(
			"LiteLLM spend-log Source returned status %d (%s)",
			sourceErr.StatusCode,
			sourceErr.Code,
		)
	}
	return "LiteLLM spend-log Source failed (" + sourceErr.Code + ")"
}

func (sourceErr *SourceError) Unwrap() error {
	if sourceErr == nil {
		return nil
	}
	return sourceErr.cause
}

type HTTPSourceConfig struct {
	Endpoint              string
	APIKey                string
	ConnectTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
	RequestTimeout        time.Duration
}

type HTTPSource struct {
	endpoint url.URL
	apiKey   string
	client   *http.Client
}

type CompleteWindow struct {
	Rows            []litellm.SanitizedRecord
	Total           int
	CumulativeBytes int
	EligibleThrough time.Time
}

func NewHTTPSource(config HTTPSourceConfig) (*HTTPSource, error) {
	endpoint, err := validateEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	if err := validateAPIKey(config.APIKey); err != nil {
		return nil, err
	}
	connectTimeout := config.ConnectTimeout
	if connectTimeout == 0 {
		connectTimeout = defaultConnectTimeout
	}
	responseTimeout := config.ResponseHeaderTimeout
	if responseTimeout == 0 {
		responseTimeout = defaultResponseHeaderTimeout
	}
	requestTimeout := config.RequestTimeout
	if requestTimeout == 0 {
		requestTimeout = defaultRequestTimeout
	}
	if connectTimeout <= 0 || connectTimeout > 10*time.Second ||
		responseTimeout <= 0 || responseTimeout > 30*time.Second ||
		requestTimeout <= 0 || requestTimeout > time.Minute {
		return nil, errors.New("LiteLLM HTTP Source timeout is invalid")
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           2,
		MaxIdleConnsPerHost:    2,
		IdleConnTimeout:        30 * time.Second,
		TLSHandshakeTimeout:    connectTimeout,
		ResponseHeaderTimeout:  responseTimeout,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: maxHeaderBytes,
		DisableCompression:     true,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &HTTPSource{
		endpoint: *endpoint,
		apiKey:   config.APIKey,
		client: &http.Client{
			Transport: transport,
			Timeout:   requestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (source *HTTPSource) FetchCompleteWindow(
	ctx context.Context,
	after Checkpoint,
	eligibleThrough time.Time,
) (CompleteWindow, error) {
	if source == nil || source.client == nil {
		return CompleteWindow{}, &SourceError{Code: "source_uninitialized"}
	}
	if err := after.Validate(); err != nil {
		return CompleteWindow{}, &SourceError{Code: "checkpoint_invalid", cause: err}
	}
	eligibleThrough = eligibleThrough.UTC().Truncate(time.Microsecond)
	checkpointTime := mustParseCheckpointTime(after.LastStartTime)
	if eligibleThrough.Before(checkpointTime) {
		return CompleteWindow{}, &SourceError{Code: "eligibility_window_invalid"}
	}
	if eligibleThrough.Equal(checkpointTime) {
		return CompleteWindow{
			Rows:            []litellm.SanitizedRecord{},
			EligibleThrough: eligibleThrough,
		}, nil
	}
	queryStartTime := checkpointQueryStart(after)
	queryEndTime := eligibleThrough.UTC().Truncate(time.Second)
	rows := make([]litellm.SanitizedRecord, 0)
	seen := map[string]struct{}{}
	expectedTotal := -1
	expectedPages := -1
	cumulativeBytes := 0
	for page := 1; page <= MaxWindowPages; page++ {
		payload, err := source.fetchPage(
			ctx,
			queryStartTime,
			queryEndTime,
			page,
		)
		if err != nil {
			return CompleteWindow{}, err
		}
		cumulativeBytes += len(payload)
		if cumulativeBytes > MaxWindowBytes {
			return CompleteWindow{}, &SourceError{Code: "complete_window_too_large"}
		}
		decoded, err := decodePage(payload)
		if err != nil {
			return CompleteWindow{}, &SourceError{
				Code:  "source_page_invalid",
				cause: err,
			}
		}
		if decoded.Page != page || decoded.PageSize != PageSize ||
			decoded.TotalIsCapped ||
			decoded.Total < 0 || decoded.Total > MaxWindowRows ||
			decoded.TotalPages < 0 || decoded.TotalPages > MaxWindowPages {
			return CompleteWindow{}, &SourceError{Code: "incomplete_paginated_snapshot"}
		}
		if expectedTotal < 0 {
			expectedTotal = decoded.Total
			expectedPages = decoded.TotalPages
			if expectedTotal == 0 && expectedPages != 0 && expectedPages != 1 {
				return CompleteWindow{}, &SourceError{Code: "incomplete_paginated_snapshot"}
			}
			if expectedTotal > 0 &&
				expectedPages != (expectedTotal+PageSize-1)/PageSize {
				return CompleteWindow{}, &SourceError{Code: "incomplete_paginated_snapshot"}
			}
		} else if decoded.Total != expectedTotal || decoded.TotalPages != expectedPages {
			return CompleteWindow{}, &SourceError{Code: "incomplete_paginated_snapshot"}
		}
		expectedPageRows := 0
		if expectedTotal > 0 {
			offset := (page - 1) * PageSize
			if offset >= expectedTotal {
				return CompleteWindow{}, &SourceError{Code: "incomplete_paginated_snapshot"}
			}
			expectedPageRows = expectedTotal - offset
			if expectedPageRows > PageSize {
				expectedPageRows = PageSize
			}
		}
		if len(decoded.Data) != expectedPageRows {
			return CompleteWindow{}, &SourceError{Code: "incomplete_paginated_snapshot"}
		}
		for _, rawRow := range decoded.Data {
			sanitized, err := litellm.SanitizeUpstreamRowBytes(rawRow)
			if err != nil {
				return CompleteWindow{}, &SourceError{Code: "source_row_invalid", cause: err}
			}
			rowStart := mustParseCheckpointTime(sanitized.Record.StartTime)
			if rowStart.Before(queryStartTime) || rowStart.After(queryEndTime) {
				return CompleteWindow{}, &SourceError{Code: "source_row_outside_requested_window"}
			}
			if _, duplicate := seen[sanitized.Record.RequestID]; duplicate {
				return CompleteWindow{}, &SourceError{Code: "incomplete_paginated_snapshot"}
			}
			seen[sanitized.Record.RequestID] = struct{}{}
			rows = append(rows, sanitized)
		}
		if len(rows) > MaxWindowRows {
			return CompleteWindow{}, &SourceError{Code: "complete_window_too_many_rows"}
		}
		if page >= expectedPages || expectedTotal == 0 {
			break
		}
		if page == MaxWindowPages {
			return CompleteWindow{}, &SourceError{Code: "incomplete_paginated_snapshot"}
		}
	}
	if expectedTotal < 0 || len(rows) != expectedTotal || len(seen) != expectedTotal {
		return CompleteWindow{}, &SourceError{Code: "incomplete_paginated_snapshot"}
	}
	return CompleteWindow{
		Rows:            rows,
		Total:           expectedTotal,
		CumulativeBytes: cumulativeBytes,
		EligibleThrough: eligibleThrough,
	}, nil
}

func checkpointQueryStart(checkpoint Checkpoint) time.Time {
	return mustParseCheckpointTime(checkpoint.LastStartTime).
		UTC().
		Truncate(time.Second)
}

type sourcePage struct {
	Data          []json.RawMessage
	Total         int
	Page          int
	PageSize      int
	TotalPages    int
	TotalIsCapped bool
}

type sourcePageWire struct {
	Data          *[]json.RawMessage `json:"data"`
	Total         *int               `json:"total"`
	Page          *int               `json:"page"`
	PageSize      *int               `json:"page_size"`
	TotalPages    *int               `json:"total_pages"`
	TotalIsCapped *bool              `json:"total_is_capped"`
}

func decodePage(payload []byte) (sourcePage, error) {
	if len(payload) == 0 || !utf8.Valid(payload) {
		return sourcePage{}, errors.New("LiteLLM page bytes are invalid")
	}
	if err := litellm.ValidateStrictJSON(payload); err != nil {
		return sourcePage{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var wire sourcePageWire
	if err := decoder.Decode(&wire); err != nil {
		return sourcePage{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return sourcePage{}, errors.New("LiteLLM page contains trailing content")
	}
	if wire.Data == nil ||
		wire.Total == nil ||
		wire.Page == nil ||
		wire.PageSize == nil ||
		wire.TotalPages == nil ||
		wire.TotalIsCapped == nil {
		return sourcePage{}, errors.New("LiteLLM page is missing a required field")
	}
	return sourcePage{
		Data:          *wire.Data,
		Total:         *wire.Total,
		Page:          *wire.Page,
		PageSize:      *wire.PageSize,
		TotalPages:    *wire.TotalPages,
		TotalIsCapped: *wire.TotalIsCapped,
	}, nil
}

func (source *HTTPSource) fetchPage(
	ctx context.Context,
	start time.Time,
	end time.Time,
	page int,
) ([]byte, error) {
	requestURL := source.endpoint
	query := requestURL.Query()
	// LiteLLM v1.93.0 /spend/logs/v2 parses only its SQL-style UTC second
	// layout (or a date-only value). The end_date query is the inclusive
	// upper bound for row startTime, not a row endTime finality assertion.
	// Durable Checkpoints remain canonical six-digit UTC timestamps and are
	// translated only at this HTTP boundary.
	query.Set("start_date", start.UTC().Format(liteLLMQueryTimeLayout))
	query.Set("end_date", end.UTC().Format(liteLLMQueryTimeLayout))
	query.Set("page", strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(PageSize))
	query.Set("sort_by", "startTime")
	query.Set("sort_order", "asc")
	requestURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, &SourceError{Code: "request_invalid", cause: err}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Authorization", "Bearer "+source.apiKey)
	response, err := source.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, &SourceError{Code: "transport_failed", Retryable: true}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxErrorBodyBytes))
		return nil, &SourceError{
			Code:       "source_status_" + strconv.Itoa(response.StatusCode),
			StatusCode: response.StatusCode,
			Retryable: response.StatusCode == http.StatusRequestTimeout ||
				response.StatusCode == http.StatusTooManyRequests ||
				response.StatusCode >= 500,
		}
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, &SourceError{Code: "source_media_type_invalid"}
	}
	if encoding := strings.TrimSpace(response.Header.Get("Content-Encoding")); encoding != "" &&
		!strings.EqualFold(encoding, "identity") {
		return nil, &SourceError{Code: "source_encoding_invalid"}
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, MaxWindowBytes+1))
	if err != nil {
		return nil, &SourceError{Code: "source_body_failed", Retryable: true}
	}
	if len(payload) > MaxWindowBytes {
		return nil, &SourceError{Code: "source_page_too_large"}
	}
	return payload, nil
}

func validateEndpoint(raw string) (*url.URL, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || len(raw) > 2048 {
		return nil, errors.New("LiteLLM endpoint is invalid")
	}
	endpoint, err := url.Parse(raw)
	if err != nil ||
		endpoint.Scheme != "https" ||
		endpoint.Host == "" ||
		endpoint.User != nil ||
		endpoint.RawQuery != "" ||
		endpoint.Fragment != "" ||
		endpoint.RawPath != "" ||
		endpoint.Path != "/spend/logs/v2" {
		return nil, errors.New("LiteLLM endpoint must be exact HTTPS /spend/logs/v2")
	}
	return endpoint, nil
}

func validateAPIKey(value string) error {
	if value == "" || len(value) > maxCredentialBytes || !utf8.ValidString(value) ||
		strings.TrimSpace(value) != value {
		return errors.New("LiteLLM API credential is invalid")
	}
	for _, character := range value {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return errors.New("LiteLLM API credential is invalid")
		}
	}
	return nil
}

func mustParseCheckpointTime(value string) time.Time {
	parsed, _ := time.Parse(checkpointTimeLayout, value)
	return parsed
}
