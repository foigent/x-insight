package pullfeed

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

type contentNeverReadBody struct{ t *testing.T }

func (b contentNeverReadBody) Read([]byte) (int, error) {
	b.t.Fatal("representation bytes read before metadata identity validation")
	return 0, errors.New("unexpected read")
}
func (b contentNeverReadBody) Close() error { return nil }
func TestContentReviewMetadataIdentityBeforeBody(t *testing.T) {
	ref, metadata := contentWireFixture(t, []byte("body"))
	for name, change := range map[string]func(*sourcefacts.FrozenManifestRef){
		"source_instance": func(r *sourcefacts.FrozenManifestRef) { r.SourceInstance = "other" },
		"org":             func(r *sourcefacts.FrozenManifestRef) { r.OrgID = "8" },
		"project":         func(r *sourcefacts.FrozenManifestRef) { r.ProjectID = "10" },
		"request":         func(r *sourcefacts.FrozenManifestRef) { r.RequestID = "other" },
		"revision":        func(r *sourcefacts.FrozenManifestRef) { r.Revision = "2" },
		"source_sha":      func(r *sourcefacts.FrozenManifestRef) { r.SourceSHA256 = sourcefacts.Hash(nil) },
		"expiry":          func(r *sourcefacts.FrozenManifestRef) { r.ExpiresAt = "2026-09-17T00:00:00.000000Z" },
	} {
		t.Run(name, func(t *testing.T) {
			selected := ref
			change(&selected)
			s := contentHTTPFixture(t, func(*http.Request) (*http.Response, error) {
				h := http.Header{"Content-Type": []string{"application/octet-stream"}, "Content-Length": []string{strconv.Itoa(4)}, "Cache-Control": []string{"no-store"}, "X-Content-Type-Options": []string{"nosniff"}, "Content-Disposition": []string{`attachment; filename="content.bin"`}}
				h.Set(ContentMetadataHeader, metadata)
				return &http.Response{StatusCode: 200, Header: h, Body: contentNeverReadBody{t}}, nil
			})
			_, e := s.FetchContentFile(context.Background(), selected, sourcefacts.ContentLimits{MaxReadBytes: 1024, MaxBufferBytes: 64}, 1024, "")
			if !errors.Is(e, ErrContentIntegrity) {
				t.Fatal("mismatched source metadata accepted", e)
			}
		})
	}
}
func TestContentReviewCanceledFetchMakesNoRequest(t *testing.T) {
	ref, _ := contentWireFixture(t, []byte("body"))
	s := contentHTTPFixture(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("canceled fetch performed source request")
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := s.FetchContentFile(ctx, ref, sourcefacts.ContentLimits{MaxReadBytes: 1024, MaxBufferBytes: 64}, 1024, ""); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

type contentReviewObservedBody struct {
	*bytes.Reader
	reads, received int
	closed          bool
}

func (b *contentReviewObservedBody) Read(p []byte) (int, error) {
	b.reads++
	n, err := b.Reader.Read(p)
	b.received += n
	return n, err
}
func (b *contentReviewObservedBody) Close() error { b.closed = true; return nil }

func TestContentReviewRevocationStopsUpstreamAtNextBlock(t *testing.T) {
	for _, reason := range []string{"grant_revoked", "credential_replaced", "endpoint_replaced", "binding_removed"} {
		t.Run(reason, func(t *testing.T) {
			payload := bytes.Repeat([]byte{'x'}, 24)
			ref, metadata := contentWireFixture(t, payload)
			body := &contentReviewObservedBody{Reader: bytes.NewReader(payload)}
			s := contentHTTPFixture(t, func(*http.Request) (*http.Response, error) {
				h := http.Header{"Content-Type": {"application/octet-stream"}, "Content-Length": {strconv.Itoa(len(payload))}, "Cache-Control": {"no-store"}, "X-Content-Type-Options": {"nosniff"}, "Content-Disposition": {`attachment; filename="content.bin"`}}
				h.Set(ContentMetadataHeader, metadata)
				return &http.Response{StatusCode: 200, Header: h, Body: body}, nil
			})
			current := *s
			denied := errors.New(reason)
			checks := 0
			check := func(context.Context) error {
				checks++
				if body.reads == 0 {
					return nil
				}
				switch reason {
				case "credential_replaced":
					current.serviceKey = "replacement-synthetic-key"
				case "endpoint_replaced":
					current.endpoint.Host = "replacement.invalid"
				case "binding_removed":
					if s.SameContentBinding(nil) {
						t.Fatal("missing binding accepted")
					}
					return denied
				case "grant_revoked":
					return denied
				}
				if s.SameContentBinding(&current) {
					t.Fatal("changed binding accepted")
				}
				return denied
			}
			dir := t.TempDir()
			file, err := s.FetchContentFile(context.Background(), ref, sourcefacts.ContentLimits{MaxReadBytes: 1024, MaxBufferBytes: 8}, 1024, dir, check)
			if !errors.Is(err, denied) || file != nil {
				t.Fatal("revoked read returned data", err)
			}
			if checks != 3 || body.reads != 1 || body.received != 8 || !body.closed {
				t.Fatalf("unbounded upstream work: checks=%d reads=%d bytes=%d closed=%v", checks, body.reads, body.received, body.closed)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatal("revoked spool retained", err)
			}
		})
	}
}

func TestContentReviewDeniedCheckPrecedesSourceIO(t *testing.T) {
	ref, _ := contentWireFixture(t, []byte("body"))
	s := contentHTTPFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("denied check made request"); return nil, nil })
	denied := errors.New("revoked")
	dir := t.TempDir()
	_, err := s.FetchContentFile(context.Background(), ref, sourcefacts.ContentLimits{MaxReadBytes: 1024, MaxBufferBytes: 8}, 1024, dir, func(context.Context) error { return denied })
	if !errors.Is(err, denied) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("denied request created spool", err)
	}
}
