package pullfeed

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

type contentRoundTrip func(*http.Request) (*http.Response, error)

func (f contentRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func contentWireFixture(t *testing.T, payload []byte) (sourcefacts.FrozenManifestRef, string) {
	t.Helper()
	ref := sourcefacts.FrozenManifestRef{SourceSystemID: "11111111-2222-3333-4444-555555555555", SourceInstance: "synthetic", OrgID: "7", ProjectID: "9", RequestID: "request-one", SourceFactID: sourcefacts.SourceID(sourcefacts.ContentManifestVersion, "synthetic", "7", "9", sourcefacts.ContentProfile, "request-one", "provider_response", "attempt-one", "body-0"), Revision: "1", SourceSHA256: "sha256:" + strings.Repeat("b", 64), CaptureStatus: "complete", ContentRef: "uc1_" + strings.Repeat("c", 64), ExpiresAt: "2026-09-16T00:00:00.000000Z", Representation: &sourcefacts.ContentRepresentation{MediaType: "application/octet-stream", Encoding: "identity", ByteDomain: "stored_representation_before_http_content_encoding", SavedBytes: int64(len(payload)), Digest: sourcefacts.Hash(payload)}}
	metadata := sourcefacts.ContentReadMetadata{SchemaVersion: sourcefacts.ContentReadMetadataVersion, SourceInstance: ref.SourceInstance, OrgID: ref.OrgID, ProjectID: ref.ProjectID, RequestID: ref.RequestID, SourceFactID: ref.SourceFactID, Revision: ref.Revision, SourceSHA256: ref.SourceSHA256, ContentRef: ref.ContentRef, Representation: *ref.Representation, ExpiresAt: ref.ExpiresAt}
	raw, _ := json.Marshal(metadata)
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = connector.CanonicalizeJSONValue(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sourcefacts.DecodeContentReadMetadata(raw); err != nil {
		t.Fatal(err)
	}
	return ref, base64.RawURLEncoding.EncodeToString(raw)
}

func TestContentFileIntegrityBudgetAndCleanup(t *testing.T) {
	for _, size := range []int{0, 19, (17 << 20) + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			payload := bytes.Repeat([]byte{'x'}, size)
			ref, metadata := contentWireFixture(t, payload)
			for _, mutation := range []string{"valid", "short", "long", "digest", "metadata", "encoding"} {
				t.Run(mutation, func(t *testing.T) {
					dir := t.TempDir()
					calls := 0
					s := &HTTPSource{endpoint: url.URL{Scheme: "https", Host: "synthetic.invalid", Path: sourcefacts.ContentEndpointPath}, serviceKey: "synthetic-only"}
					s.client = &http.Client{Transport: contentRoundTrip(func(r *http.Request) (*http.Response, error) {
						calls++
						if r.URL.Query().Get("purpose") != "human_review" || r.URL.Query().Get("request_id") != ref.RequestID || r.Header.Get("Range") != "" {
							t.Fatal("request scope changed")
						}
						body := payload
						h := http.Header{"Content-Type": {"application/octet-stream"}, "Content-Length": {strconv.Itoa(size)}, "Cache-Control": {"no-store"}}
						h.Set(ContentMetadataHeader, metadata)
						h.Set("X-Content-Type-Options", "nosniff")
						h.Set("Content-Disposition", `attachment; filename="content.bin"`)
						switch mutation {
						case "short":
							if size == 0 {
								body = []byte{1}
							} else {
								body = payload[:size-1]
							}
						case "long":
							body = append(append([]byte(nil), payload...), 1)
						case "digest":
							if size == 0 {
								h.Set("Content-Length", "1")
							} else {
								body = append([]byte(nil), payload...)
								body[0] = 'y'
							}
						case "metadata":
							h.Set(ContentMetadataHeader, "bad")
						case "encoding":
							h.Set("Content-Encoding", "gzip")
						}
						return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(bytes.NewReader(body))}, nil
					})}
					limits := sourcefacts.ContentLimits{MaxReadBytes: 32 << 20, MaxBufferBytes: 64 << 10}
					file, err := s.FetchContentFile(context.Background(), ref, limits, 32<<20, dir)
					if mutation == "valid" {
						if err != nil {
							t.Fatal(err)
						}
						var out bytes.Buffer
						checks := 0
						if err = file.Publish(context.Background(), &out, func(context.Context) error { checks++; return nil }); err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(out.Bytes(), payload) || checks == 0 {
							t.Fatal("not exact bytes")
						}
						if err = file.Close(); err != nil {
							t.Fatal(err)
						}
					} else if err == nil {
						_ = file.Close()
						t.Fatal("accepted invalid representation")
					}
					entries, _ := os.ReadDir(dir)
					if len(entries) != 0 {
						t.Fatal("spool leaked")
					}
					if calls != 1 {
						t.Fatal("unexpected retry")
					}
				})
			}
		})
	}
}

func TestContentFileBudgetBeforeIOAndRecheck(t *testing.T) {
	payload := bytes.Repeat([]byte{'x'}, 4096)
	ref, _ := contentWireFixture(t, payload)
	s := &HTTPSource{endpoint: url.URL{Path: sourcefacts.ContentEndpointPath}, client: &http.Client{Transport: contentRoundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("I/O before budget"); return nil, nil })}}
	if _, err := s.FetchContentFile(context.Background(), ref, sourcefacts.ContentLimits{MaxReadBytes: 1024, MaxBufferBytes: 100}, 32<<20, t.TempDir()); !errors.Is(err, ErrContentBudget) {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "owned-*")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(payload)
	file := &ContentFile{file: f, Size: int64(len(payload)), BlockBytes: 100}
	defer file.Close()
	var out bytes.Buffer
	checks := 0
	denied := errors.New("synthetic revocation")
	err = file.Publish(context.Background(), &out, func(context.Context) error {
		checks++
		if checks == 2 {
			return denied
		}
		return nil
	})
	if !errors.Is(err, denied) || out.Len() != 100 {
		t.Fatalf("revoke boundary: %v, %d", err, out.Len())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = file.Publish(ctx, io.Discard, func(context.Context) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
