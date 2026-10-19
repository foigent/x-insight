package pullfeed

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

const ContentMetadataHeader = "X-UR-Content-Metadata"
const MaxContentReadBytes int64 = 32 << 20
const MaxContentBlockBytes int64 = 1 << 20

var ErrContentIntegrity = errors.New("content representation failed integrity verification")
var ErrContentBudget = errors.New("selected content representation exceeds available read budget")

// ContentFile owns one verified, private, request-lifetime spool. Callers must
// Close it on every path. The representation is never truncated to fit a limit.
type ContentFile struct {
	file           *os.File
	MetadataHeader string
	Size           int64
	BlockBytes     int64
	ReadableUntil  time.Time // XI response projection; never changes UR metadata.
}

func (f *ContentFile) Close() error {
	if f == nil || f.file == nil {
		return nil
	}
	name := f.file.Name()
	err := f.file.Close()
	f.file = nil
	removeErr := os.Remove(name)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	if err != nil {
		return err
	}
	return removeErr
}

// Publish rechecks authorization before each bounded block. No authorization
// lock is held by this method while the writer can block. The caller owns its
// response deadline and cancels the context on disconnect.
func (f *ContentFile) Publish(ctx context.Context, w io.Writer, check func(context.Context) error) error {
	if f == nil || f.file == nil || check == nil {
		return ErrContentIntegrity
	}
	if _, err := f.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	buf := make([]byte, f.BlockBytes)
	remaining := f.Size
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := check(ctx); err != nil {
			return err
		}
		if remaining == 0 {
			return nil
		}
		n := min(int64(len(buf)), remaining)
		if _, err := io.ReadFull(f.file, buf[:n]); err != nil {
			return err
		}
		written, err := w.Write(buf[:n])
		if err != nil {
			return err
		}
		if int64(written) != n {
			return io.ErrShortWrite
		}
		remaining -= n
	}
}

func ContentReadBudget(ref sourcefacts.FrozenManifestRef, limits sourcefacts.ContentLimits, budget int64) (int64, error) {
	if ref.Representation == nil || budget <= 0 || limits.MaxReadBytes <= 0 || limits.MaxBufferBytes <= 0 {
		return 0, ErrContentBudget
	}
	if ref.Representation.SavedBytes < 0 || ref.Representation.SavedBytes > min(budget, MaxContentReadBytes, limits.MaxReadBytes) {
		return 0, ErrContentBudget
	}
	return min(limits.MaxBufferBytes, MaxContentBlockBytes), nil
}

// FetchContentFile uses the already selected managed C binding. Neither the
// endpoint nor purpose is supplied by the browser. Budget admission precedes
// source I/O and temporary-file creation; the API also serializes spool owners.
func (s *HTTPSource) FetchContentFile(ctx context.Context, ref sourcefacts.FrozenManifestRef, limits sourcefacts.ContentLimits, budget int64, tempDir string, checks ...func(context.Context) error) (_ *ContentFile, err error) {
	block, err := ContentReadBudget(ref, limits, budget)
	if err != nil {
		return nil, err
	}
	if s == nil || s.endpoint.Path != sourcefacts.ContentEndpointPath || len(ref.ContentRef) != 68 || !strings.HasPrefix(ref.ContentRef, "uc1_") {
		return nil, ErrHTTPSourceConfiguration
	}
	if _, err := hex.DecodeString(ref.ContentRef[4:]); err != nil {
		return nil, ErrHTTPSourceConfiguration
	}
	r, err := s.newRequest(ctx, "", 1)
	if err != nil {
		return nil, err
	}
	r.URL.Path = "/api/v1/integrations/xinsight/source/v2/contents/" + ref.ContentRef
	r.URL.RawQuery = url.Values{"request_id": {ref.RequestID}, "source_fact_id": {ref.SourceFactID}, "revision": {ref.Revision}, "purpose": {sourcefacts.ContentPurposeHumanReview}}.Encode()
	r.Header.Set("Accept", "application/octet-stream")
	r.Header.Set("Cache-Control", "no-store")
	for _, check := range checks {
		if err = check(ctx); err != nil {
			return nil, err
		}
	}
	// The managed body read shares the manifest poll's live permit. Count and
	// recheck it after custody checks, immediately before any source I/O.
	if err = connectorsync.BeforeHTTP(ctx, s.syncRequired); err != nil {
		return nil, err
	}
	if err = connectorsync.Page(ctx); err != nil {
		return nil, err
	}
	response, err := s.client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, classifyHTTPTransportError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, contentResponseError(response)
	}
	metadataHeader := response.Header.Get(ContentMetadataHeader)
	if len(metadataHeader) > 32<<10 {
		return nil, ErrContentIntegrity
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(metadataHeader)
	if err != nil {
		return nil, ErrContentIntegrity
	}
	metadata, err := sourcefacts.DecodeContentReadMetadata(raw)
	if err != nil || sourcefacts.MatchContentReadMetadata(metadata, ref) != nil {
		return nil, ErrContentIntegrity
	}
	noStore := false
	for _, item := range strings.Split(response.Header.Get("Cache-Control"), ",") {
		noStore = noStore || strings.EqualFold(strings.TrimSpace(item), "no-store")
	}
	encoding := strings.TrimSpace(response.Header.Get("Content-Encoding"))
	if response.Header.Get("X-Content-Type-Options") != "nosniff" || response.Header.Get("Content-Disposition") != `attachment; filename="content.bin"` || !noStore || response.Header.Get("Content-Type") != "application/octet-stream" || (encoding != "" && encoding != "identity") || response.Header.Get("Content-Length") != strconv.FormatInt(ref.Representation.SavedBytes, 10) || response.Uncompressed {
		return nil, ErrContentIntegrity
	}
	f, err := os.CreateTemp(tempDir, "xi-content-*")
	if err != nil {
		return nil, err
	}
	// Unlink the task-owned empty file before storing sensitive bytes. The
	// Linux service retains the descriptor only; process death cannot leave a
	// named content cache behind. Unsupported unlink semantics fail closed.
	if err = os.Remove(f.Name()); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, err
	}
	result := &ContentFile{file: f, MetadataHeader: metadataHeader, Size: ref.Representation.SavedBytes, BlockBytes: block}
	defer func() {
		if err != nil {
			_ = result.Close()
		}
	}()
	h := sha256.New()
	buf := make([]byte, block)
	var count int64
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		for _, check := range checks {
			if err = check(ctx); err != nil {
				return nil, err
			}
		}
		// The extra byte distinguishes overflow from a complete representation.
		n, readErr := io.ReadFull(response.Body, buf[:min(int64(len(buf)), result.Size-count+1)])
		count += int64(n)
		if count > result.Size {
			return nil, ErrContentIntegrity
		}
		if n > 0 {
			if _, err = f.Write(buf[:n]); err != nil {
				return nil, err
			}
			_, _ = h.Write(buf[:n])
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	if count != result.Size || "sha256:"+hex.EncodeToString(h.Sum(nil)) != ref.Representation.Digest {
		return nil, ErrContentIntegrity
	}
	return result, nil
}

func contentResponseError(response *http.Response) error {
	disposition := classifyHTTPStatus(response.StatusCode)
	code := ""
	raw, err := io.ReadAll(io.LimitReader(response.Body, MaxHTTPErrorBodyBytes+1))
	if err == nil && len(raw) <= MaxHTTPErrorBodyBytes {
		if parsed, ok := decodeContentHTTPError(raw, response.StatusCode); ok {
			code = parsed.Code
			if !parsed.Retryable {
				disposition = HTTPErrorPermanent
			}
		}
	}
	retry := time.Duration(0)
	if disposition == HTTPErrorRetryable {
		retry = parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	}
	return &HTTPSourceError{Disposition: disposition, Category: HTTPErrorCategoryStatus, StatusCode: response.StatusCode, Code: code, RetryAfter: retry}
}

// CloseContentConnections releases only this request-owned source client.
func (s *HTTPSource) CloseContentConnections() {
	if s != nil && s.client != nil {
		s.client.CloseIdleConnections()
	}
}

// SameContentBinding compares private configuration without exposing keys.
func (s *HTTPSource) SameContentBinding(other *HTTPSource) bool {
	return s != nil && other != nil && s.endpoint == other.endpoint && s.serviceKey == other.serviceKey
}
