package sourcefacts

import (
	"context"
	"errors"
	"testing"
	"time"
)

type contentFakeStore struct {
	c            ContentCollection
	committed    int
	failure      string
	beforeCommit func(*ContentCollection)
}

func (s *contentFakeStore) LoadContentCollection(context.Context, string) (ContentCollection, error) {
	return s.c, nil
}
func (s *contentFakeStore) RecordContentFailure(_ context.Context, c ContentCollection, code string, _ time.Time) error {
	if c.Revision != s.c.Revision {
		return ErrStreamChanged
	}
	s.failure = code
	s.c.RequiresAttention = ContentNeedsAttention(code)
	return nil
}
func (s *contentFakeStore) CommitContentManifests(_ context.Context, c ContentCollection, _ []byte, _ time.Time) error {
	if s.beforeCommit != nil {
		s.beforeCommit(&s.c)
	}
	if !SameContentCollection(c, s.c) || c.Revision != s.c.Revision || s.c.Status != "active" {
		return ErrStreamChanged
	}
	s.committed++
	return nil
}

type contentFakeReader struct {
	cap, page []byte
	err       error
	calls     int
}

func (r *contentFakeReader) FetchContentCapabilities(context.Context) ([]byte, error) {
	r.calls++
	return r.cap, r.err
}
func (r *contentFakeReader) FetchContentManifests(context.Context, string, int) ([]byte, error) {
	r.calls++
	return r.page, r.err
}
func contentConsumerFixture(t *testing.T) (ContentConsumer, *contentFakeStore, *contentFakeReader) {
	c := contentConfigFixture()
	p, e := c.ResolveContent(c.Contents[0])
	if e != nil {
		t.Fatal(e)
	}
	collection := p.Registration()
	collection.Status = "active"
	collection.Revision = 1
	collection.LastSequence = "0"
	collection.HighWatermarkSequence = "0"
	cap := ContentCapabilities{SchemaVersion: ContentCapabilitiesVersion, Profile: ContentProfile, SourceInstance: collection.SourceInstance, OrgID: collection.OrgID, ProjectID: collection.ProjectID, Status: "active", CredentialStatus: "feed_active", SupportedProtocols: []string{"openai.responses"}, ObservationPoints: []string{"client_request"}, CaptureModes: []string{"disabled", "metadata_only", "full_content"}, AllowedPurposes: collection.AllowedPurposes, ContentScopeRevision: collection.ContentScopeRevision, RecoveryEpoch: collection.RecoveryEpoch, CoverageFrom: collection.CoverageFrom, RetentionFloor: collection.CoverageFrom, Limits: ContentLimits{100, 100, 10, 65536, 100}, Endpoints: ContentEndpoints{ContentCapabilitiesPath, ContentEndpointPath, "/api/v1/integrations/xinsight/source/v2/contents/{content_ref}"}}
	store := &contentFakeStore{c: collection}
	reader := &contentFakeReader{cap: contentTestWire(t, cap), page: contentTestWire(t, contentTestPage(t))}
	return ContentConsumer{Store: store, Reader: reader, Expected: collection}, store, reader
}
func TestContentConsumerFirstPullAndStaleBindings(t *testing.T) {
	c, s, _ := contentConsumerFixture(t)
	if more, e := c.PullOnce(context.Background()); e != nil || more || s.committed != 1 {
		t.Fatal("first pull", more, e, s.committed)
	}
	for name, change := range map[string]func(*ContentCollection){"credential": func(s *ContentCollection) { s.CredentialID = "rotated" }, "hash": func(s *ContentCollection) { s.RegistrationHash = Hash(nil) }, "scope": func(s *ContentCollection) { s.ContentScopeRevision = "scope-2" }, "epoch": func(s *ContentCollection) { s.RecoveryEpoch = "2" }, "paused": func(s *ContentCollection) { s.Status = "paused"; s.Revision++ }} {
		t.Run(name, func(t *testing.T) {
			c, s, _ := contentConsumerFixture(t)
			s.beforeCommit = change
			if _, e := c.PullOnce(context.Background()); !errors.Is(e, ErrStreamChanged) || s.committed != 0 {
				t.Fatal("late page committed", e)
			}
		})
	}
}

func TestContentConsumerCommittedPageContinuesAfterRecordedBodyGap(t *testing.T) {
	for _, outcome := range []string{"effective_policy_disabled", "source_copy_unavailable"} {
		t.Run(outcome, func(t *testing.T) {
			consumer, store, reader := contentConsumerFixture(t)
			page := contentTestPage(t)
			page.HighWatermarkSequence = "2"
			page.HasMore = true
			reader.page = contentTestWire(t, page)
			observed := false
			consumer.AfterCommit = func(_ context.Context, _ ContentCollection, _ ContentLimits) error {
				observed = true // independent copy attempt was durably recorded
				return nil
			}
			more, err := consumer.PullOnce(context.Background())
			if err != nil || !more || store.committed != 1 || !observed || store.failure != "" {
				t.Fatalf("metadata page stalled by %s: more=%t error=%v committed=%d observed=%t failure=%q",
					outcome, more, err, store.committed, observed, store.failure)
			}
		})
	}
}
func TestContentConsumerAttentionStopsRequests(t *testing.T) {
	c, s, r := contentConsumerFixture(t)
	s.c.RequiresAttention = true
	if _, e := c.PullOnce(context.Background()); !errors.Is(e, ErrUnavailable) || r.calls != 0 {
		t.Fatal(e, r.calls)
	}
}
func TestContentFailureR2(t *testing.T) {
	for _, v := range []struct {
		status    int
		code      string
		attention bool
	}{{403, "profile_not_authorized", true}, {503, "source_paused", false}, {409, "staged_credential_requires_pause", true}} {
		if got := ContentFailure(httpFailure{v.status, v.code}); got != v.code || ContentNeedsAttention(got) != v.attention {
			t.Fatal(got)
		}
	}
}
func TestContentPageContinuationAndScope(t *testing.T) {
	c, _, _ := contentConsumerFixture(t)
	p := contentTestPage(t)
	if e := MatchContentPage(c.Expected, p); e != nil {
		t.Fatal(e)
	}
	c.Expected.Cursor = "opaque"
	c.Expected.LastSequence = "9"
	if e := MatchContentPage(c.Expected, p); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	p.RecoveryEpoch = "2"
	if e := MatchContentPage(c.Expected, p); !errors.Is(e, ErrScopeChanged) {
		t.Fatal(e)
	}
}

func TestContentConsumerManagedCOnly(t *testing.T) {
	c, s, _ := contentConsumerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := ManagedWorker{ConfigVersion: ManagedContentConfigVersion, Contents: []ContentConsumer{c}, PollInterval: time.Second, RetryMaximum: time.Second, Observe: func(_ string, e error) {
		if e != nil {
			t.Fatal(e)
		}
		cancel()
	}}
	if e := worker.Run(ctx); !errors.Is(e, context.Canceled) || s.committed != 1 {
		t.Fatal("C-only worker", e, s.committed)
	}
}
