package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func offlineVector(t *testing.T) (OfflineAuthorization, []byte, Page) {
	t.Helper()
	var vectors struct{ Valid []struct{ Canonical string } }
	raw, err := os.ReadFile("contract/golden-source-facts.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	doc, err := DecodeDocument([]byte(vectors.Valid[0].Canonical))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := Envelope(doc, []byte(vectors.Valid[0].Canonical))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	p := Page{BatchVersion: "foigent.event-batch.v1", ConnectorID: ConnectorID, ConnectorVersion: ConnectorVersion, ProducerInstance: doc.SourceInstance, GeneratedAt: now.Format(TimeFormat), Events: []json.RawMessage{wire}}
	raw, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	a := OfflineAuthorization{SchemaVersion: OfflineAuthorizationVersion, AuthorizationID: "test-grant", Environment: "test", TargetRef: "test-target", MachineIDSHA256: Hash([]byte("synthetic-machine")), ProductVersion: "0.2.0-dev", ProductCommit: "synthetic-build", ProtocolVersion: "2.0.2", ProtocolCommit: ProtocolCommit, ProtocolReleaseDigest: ProtocolReleaseDigest, SourceSchemaSHA256: Hash(schemaBytes), SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: doc.SourceInstance, OrgID: doc.OrgID, ProjectID: doc.ProjectID, Profile: doc.Profile, NotBefore: now.Add(-time.Minute).Format(TimeFormat), ExpiresAt: now.Add(time.Hour).Format(TimeFormat), BundlePath: "/tmp/synthetic-bundle.json", BundleSHA256: Hash(raw), BundleID: "bundle-one", OperationID: "operation-one", MaxRecords: 50, PayloadCaptureMode: "metadata_only"}
	return a, raw, p
}

func TestOfflineAuthorizationAndPortableBatchStrict(t *testing.T) {
	a, raw, p := offlineVector(t)
	now := time.Now().UTC()
	auth, _ := json.Marshal(a)
	parsed, err := ParseOfflineAuthorization(auth, now)
	if err != nil || parsed.Digest() != a.Digest() {
		t.Fatal(err)
	}
	batch, err := ValidateOfflineBatch(raw, a, now)
	if err != nil || len(batch.Items) != 1 || batch.SHA256 != Hash(raw) {
		t.Fatal("portable batch", err)
	}
	for _, change := range []func(*OfflineAuthorization){
		func(a *OfflineAuthorization) { a.Profile = CostProfile }, func(a *OfflineAuthorization) { a.OrgID = "999" },
		func(a *OfflineAuthorization) { a.BundleSHA256 = Hash([]byte("other")) }, func(a *OfflineAuthorization) { a.ExpiresAt = now.Add(-time.Second).Format(TimeFormat) },
		func(a *OfflineAuthorization) { a.ProtocolVersion = "2.0.1" }, func(a *OfflineAuthorization) { a.RawPayloadPresent = true },
		func(a *OfflineAuthorization) { a.NotBefore = now.Add(time.Minute).Format(TimeFormat) }, func(a *OfflineAuthorization) { a.MaxRecords = 51 },
	} {
		wrong := a
		change(&wrong)
		if _, err := ValidateOfflineBatch(raw, wrong, now); err == nil {
			t.Fatal("invalid authorization accepted")
		}
	}
	for _, input := range []string{strings.Replace(string(auth), `"raw_payload_present":false`, `"raw_payload_present":null`, 1), strings.Replace(string(auth), `"max_records":50`, `"max_records":50,"max_records":50`, 1), string(auth) + "{}", strings.Replace(string(auth), `"environment":"test"`, `"environment":"production"`, 1)} {
		if _, err := ParseOfflineAuthorization([]byte(input), now); err == nil {
			t.Fatal("invalid authorization JSON accepted")
		}
	}
	for _, change := range []func(*Page){func(p *Page) { p.NextCursor = "live" }, func(p *Page) { p.HighWatermark = "live" }, func(p *Page) { p.HasMore = true }, func(p *Page) { p.ProducerInstance = "other" }, func(p *Page) { p.Events = append(p.Events, p.Events[0]) }} {
		changed := p
		change(&changed)
		wire, _ := json.Marshal(changed)
		grant := a
		grant.BundleSHA256 = Hash(wire)
		grant.MaxRecords = 1
		if _, err := ValidateOfflineBatch(wire, grant, now); err == nil {
			t.Fatal("invalid offline batch accepted")
		}
	}
}

func TestManagedConfigAndOwnedFiles(t *testing.T) {
	a, _, _ := offlineVector(t)
	c := ManagedConfig{SchemaVersion: ManagedConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 30, Profiles: []ManagedProfileConfig{{StreamID: "22222222-2222-4222-8222-222222222222", SourceSystemID: a.SourceSystemID, SourceInstance: a.SourceInstance, OrgID: a.OrgID, ProjectID: a.ProjectID, Profile: a.Profile, ScopeGeneration: "1", Endpoint: "https://source.example/api/v1/integrations/xinsight/source-facts", KeyFile: "/etc/x-insight/source.key"}}}
	raw, _ := json.Marshal(c)
	if _, err := ParseManagedConfig(raw); err != nil {
		t.Fatal(err)
	}
	for _, wrong := range []string{strings.Replace(string(raw), `"allow_private_tunnel_http":false`, `"allow_private_tunnel_http":null`, 1), strings.Replace(string(raw), `"allow_private_tunnel_http":false`, `"extra":false`, 1), strings.Replace(string(raw), `"scope_generation":"1"`, `"scope_generation":"01"`, 1), strings.Replace(string(raw), `https://source.example`, `http://source.example`, 1)} {
		if _, err := ParseManagedConfig([]byte(wrong)); err == nil {
			t.Fatal("invalid managed config accepted")
		}
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "owned.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadOwnedFile(file, int64(len(raw)), uint32(os.Getuid())); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadOwnedFile(file, 1, uint32(os.Getuid())); err == nil {
		t.Fatal("unbounded file")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadOwnedFile(link, 65536, uint32(os.Getuid())); err == nil {
		t.Fatal("symlink")
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadOwnedFile(file, 65536, uint32(os.Getuid())); err == nil {
		t.Fatal("public authorization")
	}
	if err := os.Chmod(file, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(file, filepath.Join(dir, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadOwnedFile(file, 65536, uint32(os.Getuid())); err == nil {
		t.Fatal("hardlinked authorization")
	}
}

func TestManagedWorkerFairnessAndAttention(t *testing.T) {
	a, _, p := offlineVector(t)
	p.NextCursor = "next"
	p.HighWatermark = "next"
	p.HasMore = true
	p.HighWatermark = "future"
	wire, _ := json.Marshal(p)
	cap := capabilities()
	cap.SourceInstance = a.SourceInstance
	cap.Profiles = []string{a.Profile}
	capRaw, _ := json.Marshal(cap)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var seen []string
	w := ManagedWorker{PollInterval: time.Second, RetryMaximum: 8 * time.Second, Observe: func(id string, err error) {
		if err != nil {
			t.Error(err)
		}
		seen = append(seen, id)
		if len(seen) == 6 {
			cancel()
		}
	}}
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
		s := Stream{ID: id, SourceSystemID: a.SourceSystemID, SourceInstance: a.SourceInstance, OrgID: a.OrgID, ProjectID: a.ProjectID, Profile: a.Profile, ScopeGeneration: "1", Status: "active", Cursor: "prior"}
		w.Profiles = append(w.Profiles, ManagedProfile{StreamID: id, Consumer: Consumer{Store: &consumerMemory{stream: s}, Reader: &readerMemory{cap: capRaw, page: wire}, ExpectedStream: &s}})
	}
	if err := w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	want := []string{w.Profiles[0].StreamID, w.Profiles[1].StreamID, w.Profiles[2].StreamID, w.Profiles[0].StreamID, w.Profiles[1].StreamID, w.Profiles[2].StreamID}
	if !reflect.DeepEqual(seen, want) {
		t.Fatal("profile starvation", seen)
	}
	c := w.Profiles[0].Consumer
	s := c.Store.(*consumerMemory)
	r := c.Reader.(*readerMemory)
	before := r.calls
	s.stream.RequiresAttention = true
	for i := 0; i < 2; i++ {
		if _, err := c.PullOnce(t.Context(), s.stream.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatal(err)
		}
	}
	if r.calls != before {
		t.Fatal("attention was retried over network")
	}
	s.stream.RequiresAttention = false
	s.stream.OrgID = "999"
	if _, err := c.PullOnce(t.Context(), s.stream.ID); !errors.Is(err, ErrScopeChanged) || r.calls != before {
		t.Fatal("configured scope bypass", err)
	}
}

func TestManagedWorkerPerProfileBackoff(t *testing.T) {
	a, _, p := offlineVector(t)
	p.NextCursor = "next"
	p.HighWatermark = "next"
	wire, _ := json.Marshal(p)
	cap := capabilities()
	cap.SourceInstance = a.SourceInstance
	cap.Profiles = []string{a.Profile}
	capRaw, _ := json.Marshal(cap)
	now := time.Unix(0, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var failedAt, healthyAt []int64
	w := ManagedWorker{PollInterval: time.Second, RetryMaximum: 4 * time.Second, Now: func() time.Time { return now }, Wait: func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }, Observe: func(id string, err error) {
		if err != nil {
			failedAt = append(failedAt, now.Unix())
		} else {
			healthyAt = append(healthyAt, now.Unix())
		}
		if now.Unix() >= 8 {
			cancel()
		}
	}}
	for i := 1; i <= 2; i++ {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
		r := &readerMemory{cap: capRaw, page: wire}
		if i == 1 {
			r.capError = errors.New("synthetic network failure")
		}
		w.Profiles = append(w.Profiles, ManagedProfile{StreamID: id, Consumer: Consumer{Store: &consumerMemory{stream: Stream{ID: id, SourceInstance: a.SourceInstance, OrgID: a.OrgID, ProjectID: a.ProjectID, Profile: a.Profile, ScopeGeneration: "1", Status: "active", Cursor: "prior"}}, Reader: r}})
	}
	if err := w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(failedAt, []int64{0, 1, 3, 7}) || len(healthyAt) != 9 {
		t.Fatal("backoff or healthy fairness", failedAt, healthyAt)
	}
}

func TestManagedPartitionsRotateBeyondSingleConfigLimitAndPreservePerConfigMaximum(t *testing.T) {
	a, _, page := offlineVector(t)
	page.NextCursor, page.HighWatermark, page.HasMore = "next", "future", true
	wire, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	cap := capabilities()
	cap.SourceInstance, cap.Profiles = a.SourceInstance, []string{a.Profile}
	capRaw, err := json.Marshal(cap)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	seen := []string{}
	w := ManagedWorker{ConfigVersion: ManagedObservationConfigVersion, PollInterval: time.Second, RetryMaximum: 8 * time.Second}
	ids := []string{}
	for partition := 0; partition < 3; partition++ {
		part := ManagedWorker{ConfigVersion: ManagedObservationConfigVersion, PollInterval: time.Second, RetryMaximum: 8 * time.Second}
		for i := 0; i < 9; i++ {
			id := fmt.Sprintf("00000000-0000-4000-8000-%012d", partition*9+i+1)
			s := Stream{ID: id, SourceSystemID: a.SourceSystemID, SourceInstance: a.SourceInstance, OrgID: a.OrgID, ProjectID: a.ProjectID, Profile: a.Profile, ScopeGeneration: "1", Status: "active", Cursor: "prior"}
			part.Profiles = append(part.Profiles, ManagedProfile{StreamID: id, Consumer: Consumer{Store: &consumerMemory{stream: s}, Reader: &readerMemory{cap: capRaw, page: wire}, ExpectedStream: &s}})
			ids = append(ids, id)
		}
		if partition == 0 {
			w.Profiles = part.Profiles
		} else {
			w.Partitions = append(w.Partitions, part)
		}
	}
	w.Observe = func(id string, err error) {
		if err != nil {
			t.Error(err)
		}
		seen = append(seen, id)
		if len(seen) == 2*len(ids) {
			cancel()
		}
	}
	if err := w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	want := append(append([]string{}, ids...), ids...)
	if !reflect.DeepEqual(seen, want) {
		t.Fatal("partition starvation", seen)
	}
	// Partitioning cannot silently increase the frozen per-config maximum.
	oversized := w
	oversized.Profiles = append(append([]ManagedProfile{}, w.Profiles...), w.Partitions[0].Profiles...)
	oversized.Partitions = nil
	if err := oversized.Run(t.Context()); !errors.Is(err, ErrInvalid) {
		t.Fatal("single-config limit bypassed", err)
	}
	duplicate := w
	duplicate.Partitions = []ManagedWorker{w}
	if err := duplicate.Run(t.Context()); !errors.Is(err, ErrInvalid) {
		t.Fatal("nested partitions accepted", err)
	}
	duplicate.Partitions = []ManagedWorker{{ConfigVersion: w.ConfigVersion, Profiles: w.Profiles, PollInterval: w.PollInterval, RetryMaximum: w.RetryMaximum}}
	if err := duplicate.Run(t.Context()); !errors.Is(err, ErrInvalid) {
		t.Fatal("duplicate stream accepted", err)
	}
}
