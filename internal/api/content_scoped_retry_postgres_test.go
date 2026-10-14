package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workspace"
)

// This suite is only compiled in the offline source review. responsibilityDatabase
// skips with an empty DSN before migrations, account setup or either listener.
// A future admitted run uses the existing native PG roles and real XI HTTPS
// middleware. Its source is synthetic loopback HTTP via the existing explicit
// private-tunnel option; this is not source-side TLS qualification.
const scopedContentSubject = "xs_a1a1a1a1a1a1a1a1"

type scopedContentFixture struct {
	ctx                          context.Context
	importerDSN                  string
	admin                        *sql.DB
	store                        *db.WorkspaceStore
	reader                       *db.EvidenceStore
	consumer                     *db.SourceFactsStore
	server                       *httptest.Server
	client                       *http.Client
	service                      *access.Service
	csrf, scope, token, sourceID string
	collection                   sourcefacts.ContentCollection
	choice                       workspace.ContentChoice
	base                         time.Time
	mu                           sync.Mutex
	apiOffset                    time.Duration
	browserClock                 bool // only the explicit browser harness uses wall time
	sourceUnavailable            bool
	caps, bodies                 int
	hookAt                       int
	hook                         func() error
	hookErr                      error
	// Armed by final capabilities response (the second capabilities read for
	// one unique choice). Subsequent service-clock checks are recorded explicitly.
	expireOnCheck int
	authChecks    []string
	finalCaps     bool
}

func scopedContentCanonical(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = connector.CanonicalizeJSONValue(obj)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func newScopedContentFixture(t *testing.T) *scopedContentFixture {
	t.Helper()
	return newScopedContentFixtureWithBody(t, []byte(`{"value":1,"other":2}`))
}

// Body variations stay in the existing synthetic C source. All old callers
// retain their exact bytes, manifest identity, server and authorization setup.
func newScopedContentFixtureWithBody(t *testing.T, body []byte) *scopedContentFixture {
	t.Helper()
	return newScopedContentFixtureWithCustody(t, body, reviewbody.KeyRing{})
}

func newScopedContentFixtureWithCustody(t *testing.T, body []byte, keys reviewbody.KeyRing) *scopedContentFixture {
	t.Helper()
	ctx, dsn, admin := responsibilityDatabase(t)
	migrations := filepath.Join("..", "..", "migrations")
	if _, err := db.RunMigrations(ctx, dsn, migrations); err != nil {
		t.Fatal(err)
	}
	importerDSN, writerDSN, readerDSN := prepareWorkbenchRuntimeRoles(t, ctx, dsn)
	store, err := db.OpenWorkspaceStore(ctx, writerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	reader, err := db.OpenEvidenceStore(ctx, readerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	consumer, err := db.OpenSourceFactsStore(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { consumer.Close() })
	f := &scopedContentFixture{ctx: ctx, importerDSN: importerDSN, admin: admin, store: store, reader: reader, consumer: consumer, sourceID: randomTestUUID(t), base: time.Now().UTC().Truncate(time.Microsecond)}
	if _, err = admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name,source_version,status) VALUES($1::uuid,$2,'unirouter','Synthetic scoped content','unirouter.completed-execution.v1','active')`, f.sourceID, "scoped-content:"+f.sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_content,allowed_content_purposes) VALUES($1,$2::uuid,true,true,true,'["human_review"]')`, scopedContentSubject, f.sourceID); err != nil {
		t.Fatal(err)
	}
	at := f.base.Add(-time.Minute).Format(sourcefacts.TimeFormat)
	c := sourcefacts.ContentCollection{ID: randomTestUUID(t), SourceSystemID: f.sourceID, SourceInstance: "scoped-content", OrgID: "7", ProjectID: "9", CredentialID: "synthetic-credential", RegistrationHash: sourcefacts.Hash([]byte("synthetic-registration")), ContentScopeRevision: "scope-1", RecoveryEpoch: "1", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: sourcefacts.ContentBoundary{Time: at, AfterSequence: "0"}}
	c, err = consumer.RegisterContentCollection(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	c, err = consumer.ControlContentCollection(ctx, c.ID, "active", c.Revision, false, c)
	if err != nil {
		t.Fatal(err)
	}
	d := sourcefacts.ContentManifest{SchemaVersion: sourcefacts.ContentManifestVersion, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, RequestID: "scoped-request", Profile: sourcefacts.ContentProfile, Revision: "1", OccurredAt: at, RecordedAt: at, Protocol: "openai.responses", ObservationPoint: "client_request", ContentItemID: "body", Capture: sourcefacts.ContentCapture{Mode: "full_content", PolicyRef: "test-policy", PolicyVersion: "1", PolicyEffectiveAt: at, Completeness: "complete", Reason: "none"}, ContentRef: "uc1_" + strings.Repeat("a", 64), Representation: &sourcefacts.ContentRepresentation{MediaType: "application/json", Encoding: "identity", ByteDomain: "stored_representation_before_http_content_encoding", SavedBytes: int64(len(body)), Digest: sourcefacts.Hash(body)}, CapturedAt: at, ExpiresAt: f.base.Add(time.Hour).Format(sourcefacts.TimeFormat)}
	d.SourceFactID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.ObservationPoint, d.AttemptID, d.ContentItemID)
	d.EventID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion+".revision", d.SourceFactID, d.Revision)
	entry := sourcefacts.ContentManifestEntry{Sequence: "1", Document: d, SourceSHA256: sourcefacts.Hash(scopedContentCanonical(t, d)), Availability: sourcefacts.ContentAvailability{State: "available", EvaluatedAt: at}}
	page := sourcefacts.ContentManifestPage{SchemaVersion: sourcefacts.ContentManifestPageVersion, Profile: sourcefacts.ContentProfile, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, GeneratedAt: f.base.Format(sourcefacts.TimeFormat), ContentScopeRevision: c.ContentScopeRevision, RecoveryEpoch: c.RecoveryEpoch, CoverageFrom: c.CoverageFrom, RetentionFloor: c.CoverageFrom, AfterSequence: "0", LastSequence: "1", HighWatermarkSequence: "1", NextCursor: "next-1", HighWatermark: "high-1", Coverage: "bounded", Gaps: []sourcefacts.ContentGap{}, Items: []sourcefacts.ContentManifestEntry{entry}}
	if err = consumer.CommitContentManifests(ctx, c, scopedContentCanonical(t, page), f.base); err != nil {
		t.Fatal(err)
	}
	f.collection, err = consumer.LoadContentCollection(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := sourcefacts.FrozenContentManifestRef(f.sourceID, entry)
	if err != nil {
		t.Fatal(err)
	}
	pointer := "/value"
	f.choice = workspace.ContentChoice{Scope: "finding", Relation: "supports", Reference: ref, Location: sourcefacts.ContentLocation{Kind: "json_pointer", Pointer: &pointer}}
	caps := scopedContentCanonical(t, sourcefacts.ContentCapabilities{SchemaVersion: sourcefacts.ContentCapabilitiesVersion, Profile: sourcefacts.ContentProfile, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, Status: "active", CredentialStatus: "feed_active", SupportedProtocols: []string{"openai.responses"}, ObservationPoints: []string{"client_request"}, CaptureModes: []string{"disabled", "metadata_only", "full_content"}, AllowedPurposes: c.AllowedPurposes, ContentScopeRevision: c.ContentScopeRevision, RecoveryEpoch: c.RecoveryEpoch, CoverageFrom: c.CoverageFrom, RetentionFloor: c.CoverageFrom, Limits: sourcefacts.ContentLimits{MaxSavedBytes: 1024, MaxReadBytes: 1024, MaxBufferBytes: 1024, MaxManifestBytes: 65536, MaxManifestItems: 50}, Endpoints: sourcefacts.ContentEndpoints{Capabilities: sourcefacts.ContentCapabilitiesPath, Manifests: sourcefacts.ContentEndpointPath, Content: "/api/v1/integrations/xinsight/source/v2/contents/{content_ref}"}})
	metadata := scopedContentCanonical(t, sourcefacts.ContentReadMetadata{SchemaVersion: sourcefacts.ContentReadMetadataVersion, SourceInstance: ref.SourceInstance, OrgID: ref.OrgID, ProjectID: ref.ProjectID, RequestID: ref.RequestID, SourceFactID: ref.SourceFactID, Revision: ref.Revision, SourceSHA256: ref.SourceSHA256, ContentRef: ref.ContentRef, Representation: *ref.Representation, ExpiresAt: ref.ExpiresAt})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-content-key" {
			http.Error(w, "synthetic authorization mismatch", 403)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		f.mu.Lock()
		unavailable := f.sourceUnavailable
		isCaps := r.URL.Path == sourcefacts.ContentCapabilitiesPath
		if isCaps {
			f.caps++
		} else {
			f.bodies++
		}
		var hook func() error
		if isCaps && f.caps == f.hookAt {
			hook = f.hook
			f.hook = nil
		}
		if isCaps && f.caps == 2 && f.expireOnCheck > 0 {
			f.finalCaps = true
		}
		f.mu.Unlock()
		if hook != nil {
			if e := hook(); e != nil {
				f.mu.Lock()
				f.hookErr = e
				f.mu.Unlock()
				http.Error(w, "synthetic hook failed", 500)
				return
			}
		}
		if unavailable {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"error":{"error_version":"foigent.connector-error.v1","category":"source","code":"source_paused","phase":"fetch","operation":"read_source","retryable":true,"safe_message":"Source facts request could not be completed","recovery":"operator_repair"}}`))
			return
		}
		if isCaps {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(caps)
			return
		}
		if r.URL.Path != "/api/v1/integrations/xinsight/source/v2/contents/"+ref.ContentRef || r.URL.Query().Get("source_fact_id") != ref.SourceFactID || r.URL.Query().Get("revision") != ref.Revision || r.URL.Query().Get("purpose") != "human_review" {
			http.Error(w, "unexpected synthetic route", 400)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", `attachment; filename="content.bin"`)
		w.Header().Set(pullfeed.ContentMetadataHeader, base64.RawURLEncoding.EncodeToString(metadata))
		_, _ = w.Write(body)
	}))
	t.Cleanup(source.Close)
	provider := apiTestProvider{"scoped.content": {password: "synthetic responsibility password", principal: access.Principal{SubjectID: scopedContentSubject, Role: access.RoleScopedReviewer}}}
	f.service, err = access.NewService(provider, access.DiscardAuditor{}, access.ServiceOptions{Now: func() time.Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.finalCaps && f.expireOnCheck > 0 {
			f.authChecks = append(f.authChecks, fmt.Sprintf("after-final-cap:%d", len(f.authChecks)+1))
			if len(f.authChecks) >= f.expireOnCheck {
				return f.base.Add(24 * time.Hour)
			}
		}
		if f.browserClock {
			return time.Now().UTC()
		}
		return f.base
	}})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := db.LoadMigrationChecksums(migrations)
	if err != nil {
		t.Fatal(err)
	}
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: reader, WorkspaceStore: store, AgentAccessStore: store, ExpectedMigrations: expected, AccessService: f.service, ReviewBodyKeys: keys, Now: func() time.Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.browserClock {
			return time.Now().UTC()
		}
		return f.base.Add(f.apiOffset)
	}, ContentSource: func(_ context.Context, expected sourcefacts.ContentCollection) (*pullfeed.HTTPSource, error) {
		if !sourcefacts.SameContentCollection(expected, f.collection) {
			return nil, sourcefacts.ErrScopeChanged
		}
		return pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: source.URL + sourcefacts.ContentEndpointPath, ServiceKey: "synthetic-content-key", AllowPrivateTunnelHTTP: true})
	}})
	f.server = httptest.NewTLSServer(handler)
	t.Cleanup(f.server.Close)
	f.client = f.server.Client()
	f.client.Timeout = 15 * time.Second
	f.client.Jar, _ = cookiejar.New(nil)
	f.must(t, "POST", "/api/v1/access/login", map[string]string{"username": "scoped.content", "password": "synthetic responsibility password"}, 200, nil)
	var session struct {
		CSRF  string `json:"csrf_token"`
		Scope string `json:"scope_revision"`
	}
	if err = json.Unmarshal(f.must(t, "GET", "/api/v1/access/session", nil, 200, nil), &session); err != nil || session.CSRF == "" || session.Scope == "" {
		t.Fatal("authenticated scoped session missing", err)
	}
	f.csrf, f.scope = session.CSRF, session.Scope
	cookieURL, _ := url.Parse(f.server.URL)
	for _, cookie := range f.client.Jar.Cookies(cookieURL) {
		if cookie.Name == access.SessionCookieName {
			f.token = cookie.Value
		}
	}
	if f.token == "" {
		t.Fatal("synthetic session cookie missing")
	}
	return f
}

func (f *scopedContentFixture) do(method, path string, body any, headers map[string]string) (int, []byte, error) {
	var raw []byte
	var err error
	if body != nil {
		if b, ok := body.([]byte); ok {
			raw = b
		} else {
			raw, err = json.Marshal(body)
			if err != nil {
				return 0, nil, err
			}
		}
	}
	req, err := http.NewRequestWithContext(f.ctx, method, f.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Origin", f.server.URL)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", f.csrf)
	if f.scope != "" {
		req.Header.Set("X-XInsight-Scope-Revision", f.scope)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	response, err := f.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, raw, err
}
func (f *scopedContentFixture) must(t *testing.T, method, path string, body any, want int, headers map[string]string) []byte {
	t.Helper()
	status, raw, err := f.do(method, path, body, headers)
	if err != nil || status != want {
		t.Fatalf("%s %s status=%d want=%d error=%v response=%s", method, path, status, want, err, raw)
	}
	return raw
}
func (f *scopedContentFixture) resetSource() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.caps = 0
	f.bodies = 0
	f.hookAt = 0
	f.hook = nil
	f.hookErr = nil
	f.expireOnCheck = 0
	f.authChecks = nil
	f.finalCaps = false
}
func (f *scopedContentFixture) assertSource(t *testing.T, caps, bodies int) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.caps != caps || f.bodies != bodies || f.hookErr != nil {
		t.Fatalf("source calls caps=%d body=%d want %d/%d hook=%v", f.caps, f.bodies, caps, bodies, f.hookErr)
	}
}
func (f *scopedContentFixture) snapshot(t *testing.T, labels bool) string {
	t.Helper()
	tables := []string{"investigation_cases", "case_findings", "case_content_links", "case_finding_assessments", "case_finding_assessment_actions", "finding_review_creations"}
	if labels {
		tables = append(tables, "investigation_case_scope_labels")
	}
	var values []string
	for _, table := range tables {
		var value string
		if err := f.admin.QueryRowContext(f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM `+table+` t`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	return sourcefacts.Hash([]byte(strings.Join(values, "\n")))
}
func (f *scopedContentFixture) creation(t *testing.T) workspace.ReviewCreationCommand {
	t.Helper()
	command := workspace.ReviewCreationCommand{Operation: "start_review", OperationID: randomTestUUID(t), NewCase: &workspace.CreateCaseInput{Title: "Scoped content review"}, Finding: workspace.CreateFindingInput{Title: "Bounded claim", Statement: "The saved JSON value was reviewed", Classification: "hypothesis"}, Selection: workspace.BasisSelection{Contents: []workspace.ContentChoice{f.choice}}, Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Inspected saved bytes"}, Reason: "Start content review"}
	var basis workspace.ReviewBasis
	if err := json.Unmarshal(f.must(t, "POST", "/api/v1/security/reviews/preview", command, 200, nil), &basis); err != nil {
		t.Fatal(err)
	}
	if basis.Digest == "" || basis.Schema != workspace.ContentReviewBasisSchema {
		t.Fatal("content preview basis missing")
	}
	command.ExpectedBasisDigest = basis.Digest
	return command
}
func (f *scopedContentFixture) create(t *testing.T) (workspace.ReviewCreationCommand, workspace.ReviewCreationReceipt) {
	t.Helper()
	command := f.creation(t)
	f.resetSource()
	var receipt workspace.ReviewCreationReceipt
	if err := json.Unmarshal(f.must(t, "POST", "/api/v1/security/reviews", command, 200, nil), &receipt); err != nil {
		t.Fatal(err)
	}
	f.assertSource(t, 2, 1)
	if receipt.CaseID == "" || receipt.FindingID == "" {
		t.Fatal("creation receipt missing")
	}
	return command, receipt
}
func (f *scopedContentFixture) closure(t *testing.T, created workspace.ReviewCreationReceipt) (string, workspace.AssessmentCommand) {
	t.Helper()
	path := "/api/v1/cases/" + created.CaseID + "/findings/" + created.FindingID + "/assessment"
	selection := workspace.BasisSelection{Contents: []workspace.ContentChoice{f.choice}}
	var basis workspace.ReviewBasis
	if err := json.Unmarshal(f.must(t, "POST", path+"/preview", selection, 200, nil), &basis); err != nil {
		t.Fatal(err)
	}
	at := f.base
	command := workspace.AssessmentCommand{Operation: "record_progress", OperationID: randomTestUUID(t), ExpectedVersion: 1, ExpectedCaseVersion: created.CaseVersion, ExpectedBasisDigest: basis.Digest, Selection: selection, Progress: &workspace.ProgressPayload{State: "ended", Reason: "Human remediation verification", Closure: &workspace.Closure{Kind: "verified_remediation", Explanation: "Reviewed the precise saved JSON value", Scope: "This finding", VerifiedAt: &at, VerificationContents: []workspace.ContentChoice{f.choice}}}}
	return path + "-actions", command
}

func TestContentScopedRetryPostgresCommittedLossAndNewProof(t *testing.T) {
	if os.Getenv("XINSIGHT_POSTGRES_DSN") == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN required; compile-only boundary")
	}
	f := newScopedContentFixture(t)
	command := f.creation(t)
	f.resetSource()
	transport := &lostCreationResponse{base: f.client.Transport}
	f.client.Transport = transport
	if _, _, err := f.do("POST", "/api/v1/security/reviews", command, nil); err == nil {
		t.Fatal("expected synthetic receipt loss after commit")
	}
	if !transport.dropped {
		t.Fatal("response was not committed before loss")
	}
	f.client.Transport = transport.base
	f.assertSource(t, 2, 1)
	var created workspace.ReviewCreationReceipt
	if err := json.Unmarshal(transport.body, &created); err != nil || created.CaseID == "" {
		t.Fatal("lost receipt missing", err)
	}
	path, closure := f.closure(t, created)
	f.resetSource()
	closed := f.loseReceipt(t, path, closure)
	var committedClosure workspace.AssessmentAction
	if err := json.Unmarshal(closed, &committedClosure); err != nil || committedClosure.OperationID != closure.OperationID {
		t.Fatal("lost closure receipt missing", err)
	}
	f.assertSource(t, 2, 1) // selection+verification_contents share one actual proof.
	before := f.snapshot(t, true)
	f.mu.Lock()
	f.sourceUnavailable = true
	f.mu.Unlock()
	f.resetSource()
	for _, item := range []struct {
		path    string
		command any
		receipt []byte
	}{{"/api/v1/security/reviews", command, transport.body}, {path, closure, closed}} {
		if got := f.must(t, "POST", item.path, item.command, 200, nil); !bytes.Equal(got, item.receipt) {
			t.Fatal("unavailable source changed committed receipt")
		}
	}
	f.assertSource(t, 0, 0)
	f.mu.Lock()
	f.apiOffset = 2 * time.Hour
	f.mu.Unlock()
	for _, item := range []struct {
		path    string
		command any
		receipt []byte
	}{{"/api/v1/security/reviews", command, transport.body}, {path, closure, closed}} {
		if got := f.must(t, "POST", item.path, item.command, 200, nil); !bytes.Equal(got, item.receipt) {
			t.Fatal("expired body changed committed receipt")
		}
	}
	f.assertSource(t, 0, 0)
	// Same actor/op concurrently sees one immutable result, no source traffic.
	type response struct {
		status int
		raw    []byte
		err    error
	}
	done := make(chan response, 2)
	for i := 0; i < 2; i++ {
		go func() { status, raw, err := f.do("POST", path, closure, nil); done <- response{status, raw, err} }()
	}
	for i := 0; i < 2; i++ {
		r := <-done
		if r.err != nil || r.status != 200 || !bytes.Equal(r.raw, closed) {
			t.Fatalf("concurrent replay: %d %v", r.status, r.err)
		}
	}
	f.assertSource(t, 0, 0)
	changed := closure
	changed.Progress = &workspace.ProgressPayload{State: "ended", Reason: "changed request", Closure: closure.Progress.Closure}
	f.must(t, "POST", path, changed, 409, nil)
	changed = closure
	changed.Selection.Contents = append([]workspace.ContentChoice(nil), closure.Selection.Contents...)
	pointer := "/other"
	changed.Selection.Contents[0].Location.Pointer = &pointer
	f.must(t, "POST", path, changed, 409, nil)
	changedCreation := command
	changedCreation.Selection.Contents = append([]workspace.ContentChoice(nil), command.Selection.Contents...)
	changedCreation.Selection.Contents[0].Relation = "context"
	f.must(t, "POST", "/api/v1/security/reviews", changedCreation, 409, nil)
	fresh := command
	fresh.OperationID = randomTestUUID(t)
	f.must(t, "POST", "/api/v1/security/reviews", fresh, 410, nil)
	f.assertSource(t, 0, 0)
	f.mu.Lock()
	f.apiOffset = 0
	f.mu.Unlock()
	f.must(t, "POST", "/api/v1/security/reviews", fresh, 503, nil)
	f.assertSource(t, 1, 0)
	if f.snapshot(t, true) != before {
		t.Fatal("retry/conflict/failed fresh proof wrote case, links, labels, projection or receipts")
	}
	// Two new requests for one operation overlap at the actual source boundary.
	// The second must respect the one-body owner; retry after the winner commits
	// recovers that winner's exact receipt without another verification.
	f.mu.Lock()
	f.sourceUnavailable = false
	f.mu.Unlock()
	f.resetSource()
	fresh.OperationID = randomTestUUID(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	f.mu.Lock()
	f.hookAt = 1
	f.hook = func() error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-f.ctx.Done():
			return f.ctx.Err()
		}
	}
	f.mu.Unlock()
	winner := make(chan response, 1)
	go func() {
		status, raw, err := f.do("POST", "/api/v1/security/reviews", fresh, nil)
		winner <- response{status, raw, err}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("new operation never reached real source capabilities")
	}
	denied := f.must(t, "POST", "/api/v1/security/reviews", fresh, 503, nil)
	if !bytes.Contains(denied, []byte("content_read_busy")) {
		t.Fatalf("overlap was not rejected by the byte owner: %s", denied)
	}
	unblock()
	var winning response
	select {
	case winning = <-winner:
	case <-time.After(10 * time.Second):
		t.Fatal("winning operation did not complete")
	}
	if winning.err != nil || winning.status != 200 {
		t.Fatalf("winning operation failed: %d %v %s", winning.status, winning.err, winning.raw)
	}
	f.assertSource(t, 2, 1)
	committed := f.snapshot(t, true)
	if got := f.must(t, "POST", "/api/v1/security/reviews", fresh, 200, nil); !bytes.Equal(got, winning.raw) {
		t.Fatal("overlapping new operation failed exact receipt recovery")
	}
	f.assertSource(t, 2, 1)
	if f.snapshot(t, true) != committed {
		t.Fatal("winner replay wrote again")
	}
	var count int
	if err := f.admin.QueryRowContext(f.ctx, `SELECT count(*) FROM finding_review_creations WHERE operation_id=$1::uuid`, fresh.OperationID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("same new operation created %d receipts: %v", count, err)
	}
}

func TestContentScopedRetryPostgresRevocationAroundProof(t *testing.T) {
	if os.Getenv("XINSIGHT_POSTGRES_DSN") == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN required; compile-only boundary")
	}
	for _, stage := range []string{"before-first-pass", "before-body", "after-proof"} {
		for _, boundary := range []string{"session", "case", "content-grant"} {
			t.Run(stage+"/"+boundary, func(t *testing.T) {
				f := newScopedContentFixture(t)
				_, created := f.create(t)
				path, command := f.closure(t, created)
				f.resetSource()
				revoke := func() error { return f.revoke(boundary, created.CaseID) }
				if stage == "before-first-pass" {
					if err := revoke(); err != nil {
						t.Fatal(err)
					}
				} else {
					f.mu.Lock()
					f.hookAt = 1
					if stage == "after-proof" {
						f.hookAt = 2
					}
					f.hook = revoke
					f.mu.Unlock()
				}
				before := f.snapshot(t, boundary != "case")
				status, raw, err := f.do("POST", path, command, nil)
				if err != nil || status < 400 || status >= 500 || strings.Contains(string(raw), `"operation_id"`) {
					t.Fatalf("revocation not denied without receipt: %d %v %s", status, err, raw)
				}
				if f.snapshot(t, boundary != "case") != before {
					t.Fatal("denied proof/action wrote persistent review state")
				}
				if boundary == "case" {
					var labels int
					if err := f.admin.QueryRowContext(f.ctx, `SELECT count(*) FROM investigation_case_scope_labels WHERE case_id=$1`, created.CaseID).Scan(&labels); err != nil || labels != 0 {
						t.Fatalf("denied request recreated revoked classification: %d %v", labels, err)
					}
				}
				f.mu.Lock()
				caps, bodies, hookErr := f.caps, f.bodies, f.hookErr
				f.mu.Unlock()
				if hookErr != nil {
					t.Fatal(hookErr)
				}
				if stage == "before-first-pass" && (caps != 0 || bodies != 0) {
					t.Fatal("preexisting denial reached source")
				}
				if stage == "before-body" && boundary != "case" && (caps != 1 || bodies != 0) {
					t.Fatalf("revoked authority reached body: %d/%d", caps, bodies)
				}
				if stage == "after-proof" && (caps != 2 || bodies != 1) {
					t.Fatalf("post-proof hook did not follow complete body+pointer validation: %d/%d", caps, bodies)
				}
			})
		}
	}
}

func TestContentScopedRetryPostgresSessionExpiryAtCommitAndPublication(t *testing.T) {
	if os.Getenv("XINSIGHT_POSTGRES_DSN") == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN required; compile-only boundary")
	}
	for _, at := range []int{1, 2} {
		t.Run(fmt.Sprint("current-session-check-", at), func(t *testing.T) {
			f := newScopedContentFixture(t)
			_, created := f.create(t)
			path, command := f.closure(t, created)
			f.resetSource()
			before := f.snapshot(t, true)
			f.mu.Lock()
			f.expireOnCheck = at
			f.mu.Unlock()
			raw := f.must(t, "POST", path, command, 401, nil)
			if strings.Contains(string(raw), `"operation_id"`) {
				t.Fatal("expired session received committed receipt")
			}
			f.assertSource(t, 2, 1)
			f.mu.Lock()
			checks := append([]string(nil), f.authChecks...)
			armed := f.finalCaps
			f.mu.Unlock()
			if !armed || len(checks) != 2 || checks[0] != "after-final-cap:1" || checks[1] != "after-final-cap:2" {
				t.Fatalf("authentication ordering changed; cannot infer commit/publication boundary: %v", checks)
			}
			var receipts int
			if err := f.admin.QueryRowContext(f.ctx, `SELECT count(*) FROM case_finding_assessment_actions WHERE operation_id=$1::uuid`, command.OperationID).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			if at == 1 {
				if receipts != 0 || f.snapshot(t, true) != before {
					t.Fatal("expiry before commit did not roll back all review writes")
				}
			} else {
				if receipts != 1 || f.snapshot(t, true) == before {
					t.Fatal("publication expiry must retain the already committed receipt")
				}
				var receipt workspace.AssessmentAction
				var stored []byte
				if err := f.admin.QueryRowContext(f.ctx, `SELECT receipt FROM case_finding_assessment_actions WHERE operation_id=$1::uuid`, command.OperationID).Scan(&stored); err != nil || json.Unmarshal(stored, &receipt) != nil || receipt.OperationID != command.OperationID {
					t.Fatal("committed receipt missing", err)
				}
			}
		})
	}
}

// Observe an actual successful response and discard it only at the client
// transport boundary. A later replay must recover that committed receipt.
type lostScopedContentReceipt struct {
	base    http.RoundTripper
	path    string
	dropped bool
	body    []byte
}

func (x *lostScopedContentReceipt) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := x.base.RoundTrip(r)
	if err != nil || x.dropped || r.Method != http.MethodPost || r.URL.Path != x.path || response.StatusCode != http.StatusOK {
		return response, err
	}
	x.body, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return nil, err
	}
	x.dropped = true
	return nil, errors.New("synthetic response loss after scoped content commit")
}

func (f *scopedContentFixture) loseReceipt(t *testing.T, path string, command any) []byte {
	t.Helper()
	transport := &lostScopedContentReceipt{base: f.client.Transport, path: path}
	f.client.Transport = transport
	defer func() { f.client.Transport = transport.base }()
	if _, _, err := f.do("POST", path, command, nil); err == nil || !transport.dropped {
		t.Fatal("expected receipt loss after an actual successful response", err)
	}
	return transport.body
}

func (f *scopedContentFixture) revoke(boundary, caseID string) error {
	// A regression that holds a grant lock across source I/O must fail instead
	// of leaving the source hook blocked during httptest cleanup.
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	switch boundary {
	case "session":
		if !f.service.Logout(f.token) {
			return errors.New("synthetic session was not revoked")
		}
		return nil
	case "case":
		_, err := f.admin.ExecContext(ctx, `DELETE FROM investigation_case_scope_labels WHERE case_id=$1`, caseID)
		return err
	case "content-grant":
		_, err := f.admin.ExecContext(ctx, `UPDATE workbench_access_grants SET allow_content=false,allowed_content_purposes='[]' WHERE subject_id=$1`, scopedContentSubject)
		return err
	default:
		return errors.New("unknown synthetic revocation boundary")
	}
}

func TestContentScopedRetryPostgresCommittedReceiptStillRequiresCurrentAuthority(t *testing.T) {
	if os.Getenv("XINSIGHT_POSTGRES_DSN") == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN required; compile-only boundary")
	}
	for _, boundary := range []string{"session", "case", "content-grant"} {
		t.Run(boundary, func(t *testing.T) {
			f := newScopedContentFixture(t)
			creation, created := f.create(t)
			path, closure := f.closure(t, created)
			f.must(t, "POST", path, closure, http.StatusOK, nil)
			if err := f.revoke(boundary, created.CaseID); err != nil {
				t.Fatal(err)
			}
			before := f.snapshot(t, true)
			f.resetSource()
			f.mu.Lock()
			f.sourceUnavailable = true
			f.mu.Unlock()
			if boundary == "content-grant" {
				// First reject the stale session scope; then refresh it so the
				// actual current content permission, not just a stale header,
				// must deny both already committed operations.
				f.must(t, "POST", path, closure, http.StatusConflict, nil)
				var session struct {
					Scope string `json:"scope_revision"`
				}
				if err := json.Unmarshal(f.must(t, "GET", "/api/v1/access/session", nil, http.StatusOK, nil), &session); err != nil || session.Scope == "" || session.Scope == f.scope {
					t.Fatal("revoked content grant did not change current scope", err)
				}
				f.scope = session.Scope
			}
			for _, item := range []struct {
				path    string
				command any
			}{{"/api/v1/security/reviews", creation}, {path, closure}} {
				status, raw, err := f.do("POST", item.path, item.command, nil)
				if err != nil || status < 400 || status >= 500 || bytes.Contains(raw, []byte(`"operation_id"`)) {
					t.Fatalf("committed receipt bypassed current %s authority: %d %v %s", boundary, status, err, raw)
				}
			}
			f.assertSource(t, 0, 0)
			if f.snapshot(t, true) != before {
				t.Fatal("denied committed replay changed cases, labels, links, projections or receipts")
			}
		})
	}
}

// This closes the XI HTTP/PG component gap with a synthetic C source, not the
// independent UR capture/importer/browser acceptance. No B document is seeded.
func TestContentScopedPostgresI01WithoutBReadAdoptReopenAndRevoke(t *testing.T) {
	f := newScopedContentFixture(t)
	seedScopedContentI01WithoutB(t, f)
	verifyScopedContentI01WithoutB(t, f)
}

// Shared setup only: the browser harness never runs destructive negative checks
// until the browser has explicitly finished the positive chain.
func seedScopedContentI01WithoutB(t *testing.T, f *scopedContentFixture) {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join("..", "..", "contracts", "completed-execution", "v1", "fixtures", "valid-settled.json"))
	if err != nil {
		t.Fatal(err)
	}
	var execution unirouter.CompletedExecutionV1
	if err = json.Unmarshal(wire, &execution); err != nil {
		t.Fatal(err)
	}
	execution.RequestID = f.choice.Reference.RequestID
	execution.OrgID, _ = strconv.ParseInt(f.collection.OrgID, 10, 64)
	execution.ProjectID, _ = strconv.ParseInt(f.collection.ProjectID, 10, 64)
	execution.Terminal.AvailableAt = f.base.Add(-time.Minute)
	wire, err = json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := unirouter.MapCompletedExecutionV1(wire, unirouter.CompletedExecutionMapOptions{SourceInstance: f.collection.SourceInstance, SourceSystemID: f.sourceID, AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID, ObservedAt: f.base, IngestedAt: f.base})
	if err != nil {
		t.Fatal(err)
	}
	importer, err := db.OpenEvidenceStore(f.ctx, f.importerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer importer.Close()
	if _, err = importer.PersistEvents(f.ctx, mapped.Events); err != nil {
		t.Fatal(err)
	}
	// Select after the actual projection commit, without changing source time.
	// This future reading cutoff is not a clock for control/credential creation;
	// those paths also check PostgreSQL's wall clock and must use f.base.
	f.mu.Lock()
	f.apiOffset = time.Since(f.base) + time.Second
	f.mu.Unlock()
	if _, err = f.admin.ExecContext(f.ctx, `UPDATE workbench_access_grants SET source_project_ref=$1 WHERE subject_id=$2 AND source_system_id=$3::uuid`, "project-"+f.collection.ProjectID, scopedContentSubject, f.sourceID); err != nil {
		t.Fatal(err)
	}
	var session struct {
		Scope string `json:"scope_revision"`
	}
	if err = json.Unmarshal(f.must(t, "GET", "/api/v1/access/session", nil, 200, nil), &session); err != nil || session.Scope == "" {
		t.Fatal("current scoped session missing", err)
	}
	f.scope = session.Scope
}

func (f *scopedContentFixture) assertI01WithoutB(t *testing.T) {
	t.Helper()
	var observations, legacy, grants, money int
	err := f.admin.QueryRowContext(f.ctx, `SELECT
		 (SELECT count(*) FROM source_observation_documents),
		 (SELECT count(*) FROM source_fact_documents),
		 (SELECT count(*) FROM workbench_access_grants WHERE subject_id=$1 AND (allow_source_runtime OR allow_source_attempt OR allow_source_capability OR allow_source_policy OR allow_source_destination OR allow_source_context OR allow_source_identity OR allow_source_cost)),
		 (SELECT count(*) FROM workbench_economic_facts WHERE source_system_id=$2::uuid AND request_id=$3)`, scopedContentSubject, f.sourceID, f.choice.Reference.RequestID).Scan(&observations, &legacy, &grants, &money)
	if err != nil || observations != 0 || legacy != 0 || grants != 0 || money != 1 {
		t.Fatalf("I01+C boundary: observations=%d legacy=%d B-grants=%d money=%d err=%v", observations, legacy, grants, money, err)
	}
}

func verifyScopedContentI01WithoutB(t *testing.T, f *scopedContentFixture) {
	t.Helper()
	var err error
	var session struct {
		Scope string `json:"scope_revision"`
	}
	f.assertI01WithoutB(t)
	input := httpReadingRequest(f.sourceID)
	input.Filters.SourceProjects = []string{"project-" + f.collection.ProjectID}
	var money workbench.Reading
	if err = json.Unmarshal(f.must(t, "POST", "/api/v1/workbench/readings", input, 201, nil), &money); err != nil {
		t.Fatal(err)
	}
	var summary workbench.Summary
	if err = json.Unmarshal(f.must(t, "GET", "/api/v1/workbench/readings/"+money.ID+"/summary", nil, 200, nil), &summary); err != nil || summary.RequestCount != 1 {
		t.Fatal("same user could not read the single I01 execution", err)
	}
	f.resetSource()
	var reading workbench.RuntimeReading
	if err = json.Unmarshal(f.must(t, "POST", "/api/v1/runtime/readings", workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: input.Period, Filters: input.Filters}, 201, nil), &reading); err != nil || reading.ObservationCount != 0 {
		t.Fatal("C reading fabricated B", err)
	}
	base := "/api/v1/runtime/readings/" + reading.ID
	var requests workbench.RuntimeRequests
	if err = json.Unmarshal(f.must(t, "GET", base+"/requests?entry_time=all", nil, 200, nil), &requests); err != nil || len(requests.Items) != 1 {
		t.Fatal("I01+C request not listed", err)
	}
	request := requests.Items[0]
	if request.SourceSystemID != f.sourceID || request.SourceInstance != f.collection.SourceInstance || request.OrgID != f.collection.OrgID || request.ProjectID != f.collection.ProjectID || request.RequestID != f.choice.Reference.RequestID || request.ObservationCount != 0 || request.EntryAt != nil || request.ContentEligibleAt == nil {
		t.Fatal("wrong C-only member", request)
	}
	listPath := base + "/requests/" + request.Ref + "/contents"
	var contents workbench.RuntimeContents
	if err = json.Unmarshal(f.must(t, "GET", listPath, nil, 200, nil), &contents); err != nil || len(contents.Items) != 1 || contents.MembershipRef != reading.MembershipRef {
		t.Fatal("frozen C list missing", err)
	}
	f.assertSource(t, 0, 0) // Listing never downloads the source body.
	listed := contents.Items[0].Reference
	if !workspace.SameAssessmentValue(listed, f.choice.Reference) || contents.Items[0].Observation == nil {
		t.Fatal("list changed source reference or lost observation")
	}
	bodyPath := listPath + "/" + listed.ContentRef + "?source_fact_id=" + url.QueryEscape(listed.SourceFactID) + "&revision=" + listed.Revision
	body := f.must(t, "GET", bodyPath, nil, 200, nil)
	if int64(len(body)) != listed.Representation.SavedBytes || sourcefacts.Hash(body) != listed.Representation.Digest || string(body) != `{"value":1,"other":2}` {
		t.Fatal("body did not match frozen saved bytes")
	}
	// Choose from the returned list. The existing preview/create proof fetches
	// and validates /value against actual source bytes, without a proof bypass.
	f.choice.Reference = listed
	creation, created := f.create(t)
	assessmentPath := "/api/v1/cases/" + created.CaseID + "/findings/" + created.FindingID + "/assessment"
	f.resetSource()
	f.mu.Lock()
	f.sourceUnavailable = true
	f.mu.Unlock()
	f.must(t, "GET", "/api/v1/cases/"+created.CaseID, nil, 200, nil)
	var reopened workspace.AssessmentView
	if err = json.Unmarshal(f.must(t, "GET", assessmentPath, nil, 200, nil), &reopened); err != nil || reopened.Assessment.Review == nil || !workspace.SameAssessmentValue(reopened.Assessment.Review.Basis.Selected.Contents, []workspace.ContentChoice{f.choice}) || reopened.Assessment.Review.ActorSubjectID != scopedContentSubject {
		t.Fatal("same-user reopen lost exact content choice", err)
	}
	f.assertSource(t, 0, 0) // Historical reference reads do not fetch body.
	// Revoke after the complete body was fetched, before publication to XI's
	// caller. A late verified source response cannot publish secret bytes.
	f.mu.Lock()
	f.sourceUnavailable = false
	f.hookAt = 2
	f.hook = func() error { return f.revoke("content-grant", created.CaseID) }
	f.mu.Unlock()
	before := f.snapshot(t, true)
	status, raw, err := f.do("GET", bodyPath, nil, nil)
	// The second preflight can complete just after the grant is revoked. The
	// per-block check then aborts before the first write (ErrAbortHandler), so
	// net/http may report EOF instead of a JSON 4xx. A reused GET connection can
	// also retry and receive the normal 4xx. Neither path may publish body bytes;
	// unrelated transport errors/timeouts and partially published 2xx still fail.
	denied := err == nil && status >= 400 && status < 500 && !bytes.Contains(raw, body)
	abortedBeforeHeaders := status == 0 && len(raw) == 0 && errors.Is(err, io.EOF)
	if !denied && !abortedBeforeHeaders {
		t.Fatalf("late body survived content revoke: %d %v", status, err)
	}
	f.assertSource(t, 2, 1)
	if err = json.Unmarshal(f.must(t, "GET", "/api/v1/access/session", nil, 200, nil), &session); err != nil || session.Scope == "" || session.Scope == f.scope {
		t.Fatal("content revoke did not change scope", err)
	}
	f.scope = session.Scope
	f.resetSource()
	// Refresh the session so stale-header rejection cannot mask actual denial.
	for _, path := range []string{base, listPath, bodyPath, assessmentPath} {
		status, raw, err = f.do("GET", path, nil, nil)
		if err != nil || status < 400 || status >= 500 || bytes.Contains(raw, body) || bytes.Contains(raw, []byte(listed.SourceFactID)) {
			t.Fatalf("old reading/reference escaped revoked content scope: %s status=%d err=%v", path, status, err)
		}
	}
	creation.OperationID = randomTestUUID(t)
	status, _, err = f.do("POST", "/api/v1/security/reviews", creation, nil)
	if err != nil || status < 400 || status >= 500 {
		t.Fatalf("old choice authorized a new operation after revoke: %d %v", status, err)
	}
	f.assertSource(t, 0, 0)
	if f.snapshot(t, true) != before {
		t.Fatal("denied old-reference operation changed durable state")
	}
	f.assertI01WithoutB(t)
}
