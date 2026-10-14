package db_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/integration/reviewcopy"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestOriginalCCustodyTransitionPostgresHistoricalRevisionsRetryAndPartialGap(t *testing.T) {
	for _, scenario := range []string{"bounded_partial_gap", "unknown_capture_and_transient_history", "retention_floor_gap"} {
		t.Run(scenario, func(t *testing.T) { testOriginalCCustodyTransition(t, scenario) })
	}
}

func testOriginalCCustodyTransition(t *testing.T, scenario string) {
	unknownCapture := scenario != "bounded_partial_gap"
	f := newSourceFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	at := now.Add(-time.Minute).Format(sourcefacts.TimeFormat)
	canonical := func(value any) []byte {
		t.Helper()
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		object, e := connector.DecodeStrictJSONObject(raw)
		if e != nil {
			t.Fatal(e)
		}
		raw, e = connector.CanonicalizeJSONValue(object)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	c := sourcefacts.ContentCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: "original-content", OrgID: "7", ProjectID: "9", CredentialID: "107", RegistrationHash: sourcefacts.Hash([]byte("original-registration")), ContentScopeRevision: "original-scope", RecoveryEpoch: "2", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: sourcefacts.ContentBoundary{Time: at, AfterSequence: "0"}}
	var err error
	c, err = f.consumerStore.RegisterContentCollection(f.ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	c, err = f.consumerStore.ControlContentCollection(f.ctx, c.ID, "active", c.Revision, false, c)
	if err != nil {
		t.Fatal(err)
	}
	entries := []sourcefacts.ContentManifestEntry{}
	bodies := map[string][]byte{}
	entryCount := 3
	if unknownCapture {
		entryCount = 4
	}
	for n := 1; n <= entryCount; n++ {
		body := []byte(`{"text":"historical-` + strconv.Itoa(n) + `"}`)
		d := sourcefacts.ContentManifest{SchemaVersion: sourcefacts.ContentManifestVersion, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, RequestID: "original-request", Profile: sourcefacts.ContentProfile, Revision: "1", OccurredAt: at, RecordedAt: at, Protocol: "openai.responses", ObservationPoint: "client_request", ContentItemID: "original-body", Capture: sourcefacts.ContentCapture{Mode: "full_content", PolicyRef: "original-policy", PolicyVersion: "1", PolicyEffectiveAt: at, Completeness: "complete", Reason: "none"}, ContentRef: "uc1_" + strings.Repeat(strconv.Itoa(n), 64), Representation: &sourcefacts.ContentRepresentation{MediaType: "application/json", Encoding: "identity", ByteDomain: "stored_representation_before_http_content_encoding", SavedBytes: int64(len(body)), Digest: sourcefacts.Hash(body)}, CapturedAt: at, ExpiresAt: now.Add(time.Hour).Format(sourcefacts.TimeFormat)}
		if n == 2 {
			d.Revision = "2"
			d.PreviousRevision = "1"
			d.PreviousHash = entries[0].SourceSHA256
			d.CorrectionReason = "source_correction"
		}
		if n == 3 {
			d.ContentItemID = "partial-body"
			d.Capture.Completeness = "partial"
			d.Capture.Reason = "limit_exceeded"
			d.ExpiresAt = now.Add(3 * time.Minute).Format(sourcefacts.TimeFormat)
		}
		if n == 4 {
			d.ContentItemID = "temporarily-unavailable-body"
		}
		d.SourceFactID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.ObservationPoint, d.AttemptID, d.ContentItemID)
		d.EventID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion+".revision", d.SourceFactID, d.Revision)
		availability := sourcefacts.ContentAvailability{State: "available", EvaluatedAt: at}
		if n == 4 {
			availability.State, availability.Retryable = "source_unavailable", true
		}
		entries = append(entries, sourcefacts.ContentManifestEntry{Sequence: strconv.Itoa(n), Document: d, SourceSHA256: sourcefacts.Hash(canonical(d)), Availability: availability})
		bodies[d.ContentRef] = body
	}
	// Match the real UR page contract: capture health is unknown even when the
	// manifest stream can be fully enumerated. Do not invent bounded coverage.
	page := sourcefacts.ContentManifestPage{SchemaVersion: sourcefacts.ContentManifestPageVersion, Profile: sourcefacts.ContentProfile, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, GeneratedAt: at, ContentScopeRevision: c.ContentScopeRevision, RecoveryEpoch: c.RecoveryEpoch, CoverageFrom: c.CoverageFrom, RetentionFloor: c.CoverageFrom, AfterSequence: "0", LastSequence: "2", HighWatermarkSequence: strconv.Itoa(entryCount), NextCursor: "original-cursor-first", HighWatermark: "original-hwm", HasMore: true, Coverage: "unknown", Gaps: []sourcefacts.ContentGap{{Reason: "capture_health_unknown"}}, Items: entries[:2]}
	if !unknownCapture {
		page.Coverage, page.Gaps = "bounded", []sourcefacts.ContentGap{}
	}
	if err = f.consumerStore.CommitContentManifests(f.ctx, c, canonical(page), now); err != nil {
		t.Fatal(err)
	}
	// Only this domain-transition fixture's initial installation is synthetic;
	// manifests, cursor, policy, copy loop, encryption and reads use production PG.
	inv, _, err := f.workspace.CreateInvitation(f.ctx, connectorcontrol.CreateInvitationInput{ActorSubject: f.subject, Config: connectorcontrol.InvitationConfig{DeploymentID: "xid_AAAAAAAAAAAAAAAAAAAAAA", XInsightOrigin: "https://xinsight.example", UniRouterInstallOrigin: "https://unirouter.example"}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.workspace.RedeemInvitation(f.ctx, connectorcontrol.RedeemInvitationInput{InvitationToken: inv.Token, ProjectID: "project.fixture", FinalEventScope: []string{connectorcontrol.CompletedEventType}, ConsentSHA256: sourcefacts.Hash([]byte("original-consent")), ConfirmedAt: now, ControlToken: "xib_" + strings.Repeat("A", 43), ControlTokenExpires: now.Add(time.Hour)}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := inv.InstallationID
	f.exec(`UPDATE connector_installations SET status='connected',observed_state='connected',source_instance=$2,source_instance_sha256=$3 WHERE id=$1`, id, c.SourceInstance, sourcefacts.Hash([]byte(c.SourceInstance)))
	record := db.ManagedEnrollment{InstallationID: id, EnrollmentID: "xise_1", SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, Revision: "1", ContentCollectionID: c.ID, Status: "requires_migration", LegacyCCopyPending: true}
	if err = f.workspace.SaveManagedEnrollment(f.ctx, record, now); err != nil {
		t.Fatal(err)
	}
	otherProject := record
	otherProject.ProjectID = "999"
	otherProject.ContentCollectionID = ""
	otherProject.LegacyCCopyPending = false
	otherProject.Status = "active"
	if err = f.workspace.SaveManagedEnrollment(f.ctx, otherProject, now); err != nil {
		t.Fatal("same grant second project", err)
	}
	if other, e := f.workspace.ManagedEnrollment(f.ctx, id, record.EnrollmentID, otherProject.ProjectID); e != nil || other.ProjectID != otherProject.ProjectID || other.ContentCollectionID != "" {
		t.Fatal("project scoped enrollment read", e)
	}
	assertPending := func(want bool) {
		t.Helper()
		v, e := f.workspace.ManagedEnrollment(f.ctx, id, "xise_1", c.ProjectID)
		if e != nil || v.LegacyCCopyPending != want || want && v.Status != "requires_migration" || !want && v.Status != "active" {
			t.Fatalf("transition %+v: %v", v, e)
		}
		custody, e := f.workspace.IsManagedCustodyCollection(f.ctx, c.ID)
		if e != nil || custody == want {
			t.Fatalf("custody=%v pending=%v err=%v", custody, want, e)
		}
	}
	assertPending(true)
	if err = f.workspace.FinishLegacyCCustodyTransition(f.ctx, c.ID, now); err != nil {
		t.Fatal(err)
	}
	assertPending(true)
	keys := reviewbody.KeyRing{ActiveID: "synthetic", Keys: map[string][]byte{"synthetic": bytes.Repeat([]byte{7}, 32)}}
	revision, err := f.consumerStore.RequestReviewCaptureDefault(f.ctx, db.ReviewCaptureSettings{Mode: "full_content", RetentionSeconds: 7200, ProjectQuotaBytes: 4096, MaxItemBytes: 1024}, 0, "xs_admin", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.ConfirmReviewCaptureDefault(f.ctx, revision, "xs_admin", now); err != nil {
		t.Fatal(err)
	}
	// Exercise the managed owner binding in the mandatory PostgreSQL suite,
	// not only the separately selected HTTPS joint scenario.
	xiProject, err := f.consumerStore.EnsureManagedReviewCaptureProject(f.ctx, id, c.ID,
		f.source, c.SourceInstance, c.OrgID, c.ProjectID, now)
	if err != nil || xiProject == "" {
		t.Fatal("managed owner binding", err)
	}
	if repeated, e := f.consumerStore.EnsureManagedReviewCaptureProject(f.ctx, id, c.ID,
		f.source, c.SourceInstance, c.OrgID, c.ProjectID, now); e != nil || repeated != xiProject {
		t.Fatal("managed owner binding changed on retry", e)
	}
	if _, e := f.consumerStore.EnsureManagedReviewCaptureProject(f.ctx, id, c.ID,
		f.source, c.SourceInstance, c.OrgID, "999", now); !errors.Is(e, reviewbody.ErrScope) {
		t.Fatalf("unmatched project acquired the original collection: %v", e)
	}
	var bindings, projects, audits int
	if err = f.admin.QueryRowContext(f.ctx, `SELECT
 (SELECT count(*) FROM review_capture_project_bindings),
 (SELECT count(*) FROM review_capture_projects),
 (SELECT count(*) FROM access_audit_records WHERE action='review.policy.project.auto_bind'
 AND actor_kind='system' AND actor_ref=$1)`, id).Scan(&bindings, &projects, &audits); err != nil || bindings != 1 || projects != 1 || audits != 1 {
		t.Fatalf("managed replay/denial changed bindings=%d projects=%d audits=%d: %v", bindings, projects, audits, err)
	}
	// A previously explicit owner mapping retains its ID and display name.
	other := sourcefacts.ContentCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: c.SourceInstance,
		OrgID: c.OrgID, ProjectID: "999", CredentialID: "other-content", RegistrationHash: sourcefacts.Hash([]byte("other-registration")),
		ContentScopeRevision: "other-scope", RecoveryEpoch: "2", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: c.CoverageFrom}
	other, err = f.consumerStore.RegisterContentCollection(f.ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	other, err = f.consumerStore.ControlContentCollection(f.ctx, other.ID, "active", other.Revision, false, other)
	if err != nil {
		t.Fatal(err)
	}
	existingProject := testUUID(t)
	if err = f.consumerStore.BindReviewCaptureProject(f.ctx, f.source, other.SourceInstance, other.OrgID, other.ProjectID, existingProject, "Existing owner project", "xs_admin", now); err != nil {
		t.Fatal(err)
	}
	if preserved, e := f.consumerStore.EnsureManagedReviewCaptureProject(f.ctx, id, other.ID,
		f.source, other.SourceInstance, other.OrgID, other.ProjectID, now); e != nil || preserved != existingProject || preserved == xiProject {
		t.Fatal("existing project mapping was replaced or merged", e)
	}
	var preservedName string
	if err = f.admin.QueryRowContext(f.ctx, `SELECT display_name FROM review_capture_projects WHERE xi_project_id=$1::uuid`, existingProject).Scan(&preservedName); err != nil || preservedName != "Existing owner project" {
		t.Fatal("existing project display name changed", err)
	}
	var available atomic.Bool
	var fetches atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refString := strings.TrimPrefix(r.URL.Path, "/api/v1/integrations/xinsight/source/v2/contents/")
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer original-c-token" || bodies[refString] == nil {
			w.WriteHeader(403)
			return
		}
		fetches.Add(1)
		if !available.Load() {
			w.WriteHeader(503)
			return
		}
		for _, e := range entries {
			if e.Document.ContentRef != refString {
				continue
			}
			ref, _ := sourcefacts.FrozenContentManifestRef(f.source, e)
			metadata := sourcefacts.ContentReadMetadata{SchemaVersion: sourcefacts.ContentReadMetadataVersion, SourceInstance: ref.SourceInstance, OrgID: ref.OrgID, ProjectID: ref.ProjectID, RequestID: ref.RequestID, SourceFactID: ref.SourceFactID, Revision: ref.Revision, SourceSHA256: ref.SourceSHA256, ContentRef: ref.ContentRef, Representation: *ref.Representation, ExpiresAt: ref.ExpiresAt}
			w.Header().Set(pullfeed.ContentMetadataHeader, base64.RawURLEncoding.EncodeToString(canonical(metadata)))
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(bodies[refString])))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", `attachment; filename="content.bin"`)
		w.Write(bodies[refString])
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: server.URL + sourcefacts.ContentEndpointPath, ServiceKey: "original-c-token", TLSRootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.CloseContentConnections()
	limits := sourcefacts.ContentLimits{MaxReadBytes: 32 << 20, MaxBufferBytes: 1 << 20}
	candidates, err := f.workspace.ListURCCopyCandidates(f.ctx, c.ID, now, 16)
	if err != nil || len(candidates) != 2 || candidates[0].Ref.Revision != "1" || candidates[1].Ref.Revision != "2" {
		t.Fatalf("historical candidates=%+v %v", candidates, err)
	}
	// The original historical bytes remain readable while the transition is pending.
	available.Store(true)
	oldRef, _ := sourcefacts.FrozenContentManifestRef(f.source, entries[0])
	file, err := reader.FetchContentFile(f.ctx, oldRef, limits, 1<<20, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	err = file.Publish(f.ctx, &raw, func(context.Context) error { return nil })
	file.Close()
	if err != nil || !bytes.Equal(raw.Bytes(), bodies[oldRef.ContentRef]) {
		t.Fatal("original history changed", err)
	}
	available.Store(false)
	if err = reviewcopy.PollURCCopies(f.ctx, reader, f.workspace, c.ID, limits, keys, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	assertPending(true)
	// A fresh store resumes the existing durable attempts; no new runner/cursor.
	restarted, err := db.OpenWorkspaceStore(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	available.Store(true)
	if err = reviewcopy.PollURCCopies(f.ctx, reader, restarted, c.ID, limits, keys, func() time.Time { return now.Add(61 * time.Second) }); err != nil {
		t.Fatal(err)
	}
	assertPending(true)
	// Both available items are copied, but the producer declared another page.
	// The owner sweep must not switch custody before that page is committed.
	if _, err = restarted.SweepLegacyCCustodyTransitions(f.ctx, now.Add(62*time.Second), 100); err != nil {
		t.Fatal(err)
	}
	assertPending(true)
	page.AfterSequence, page.LastSequence, page.NextCursor, page.HasMore, page.Items = "2", strconv.Itoa(entryCount), "original-cursor", false, entries[2:]
	current, err := f.consumerStore.LoadContentCollection(f.ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.CommitContentManifests(f.ctx, current, canonical(page), now.Add(63*time.Second)); err != nil {
		t.Fatal(err)
	}
	var copied int
	if err = f.admin.QueryRow(`SELECT count(*) FROM review_body_operations WHERE content_collection_id=$1 AND state='accepted'`, c.ID).Scan(&copied); err != nil || copied != 2 {
		t.Fatalf("copies=%d %v", copied, err)
	}
	// Partial bytes and a temporarily unavailable historical body both prevent
	// premature cutover. Expiry of the partial body does not excuse the latter.
	if _, err = restarted.SweepLegacyCCustodyTransitions(f.ctx, now.Add(64*time.Second), 100); err != nil {
		t.Fatal(err)
	}
	assertPending(true)
	finishAt := now.Add(181 * time.Second)
	if unknownCapture {
		if _, err = restarted.SweepLegacyCCustodyTransitions(f.ctx, now.Add(181*time.Second), 100); err != nil {
			t.Fatal(err)
		}
		assertPending(true)
		available.Store(false)
		if err = reviewcopy.PollURCCopies(f.ctx, reader, restarted, c.ID, limits, keys, func() time.Time { return now.Add(181 * time.Second) }); err != nil {
			t.Fatal(err)
		}
		assertPending(true) // The source worker cannot write enrollment custody state.
		if _, err = restarted.SweepLegacyCCustodyTransitions(f.ctx, now.Add(181*time.Second), 100); err != nil {
			t.Fatal(err)
		}
		assertPending(true)
		available.Store(true)
		if err = reviewcopy.PollURCCopies(f.ctx, reader, restarted, c.ID, limits, keys, func() time.Time { return now.Add(243 * time.Second) }); err != nil {
			t.Fatal(err)
		}
		if err = f.admin.QueryRow(`SELECT count(*) FROM review_body_operations WHERE content_collection_id=$1 AND state='accepted'`, c.ID).Scan(&copied); err != nil || copied != 3 {
			t.Fatalf("recovered historical copies=%d %v", copied, err)
		}
		assertPending(true)
		// Other source gaps still block even after every readable representation has
		// a local copy. Use the real page commit path, not direct enrollment updates.
		page.AfterSequence, page.Items = "4", []sourcefacts.ContentManifestEntry{}
		commitEmptyPage := func() {
			t.Helper()
			live, e := f.consumerStore.LoadContentCollection(f.ctx, c.ID)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.consumerStore.CommitContentManifests(f.ctx, live, canonical(page), now.Add(244*time.Second)); e != nil {
				t.Fatal(e)
			}
		}
		for _, reason := range []string{"manifest_write_unavailable", "missing_predecessor", "source_recovery"} {
			page.Gaps = []sourcefacts.ContentGap{{Reason: "capture_health_unknown"}, {Reason: reason}}
			commitEmptyPage()
			if _, err = restarted.SweepLegacyCCustodyTransitions(f.ctx, now.Add(244*time.Second), 100); err != nil {
				t.Fatal(err)
			}
			assertPending(true)
		}
		page.Gaps = []sourcefacts.ContentGap{{Reason: "capture_health_unknown"}}
		if scenario == "retention_floor_gap" {
			// Use UR's actual retention gap shape and advance the floor. This
			// separate fixture never rewinds a producer retention boundary.
			page.RetentionFloor = sourcefacts.ContentBoundary{Time: at, AfterSequence: "1"}
			page.Gaps = append(page.Gaps, sourcefacts.ContentGap{Reason: "retention_floor_ahead", RangeKnown: true, AfterSequence: "0", ThroughSequence: "1"})
		}
		commitEmptyPage()
		finishAt = now.Add(244 * time.Second)
	}
	before, err := f.consumerStore.LoadContentCollection(f.ctx, c.ID)
	if err != nil || before.Coverage != page.Coverage || !reflect.DeepEqual(before.Gaps, page.Gaps) {
		t.Fatal("source coverage was rewritten", err)
	}
	if _, err = restarted.SweepLegacyCCustodyTransitions(f.ctx, finishAt, 100); err != nil {
		t.Fatal(err)
	}
	if scenario == "retention_floor_gap" {
		assertPending(true)
		return
	}
	assertPending(false)
	// A controller may have read pending before the copy loop completed. Its
	// stale save must never reopen the old live-source path after the switch.
	if err = f.workspace.SaveManagedEnrollment(f.ctx, record, finishAt.Add(time.Second)); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("stale pending save reopened custody: %v", err)
	}
	assertPending(false)
	after, err := f.consumerStore.LoadContentCollection(f.ctx, c.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("original collection/cursor changed: %+v %+v %v", before, after, err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_content=true,allowed_content_purposes='["human_review"]' WHERE subject_id=$1`, f.subject)
	count := fetches.Load()
	preserved := append([]sourcefacts.ContentManifestEntry{}, entries[:2]...)
	if unknownCapture {
		preserved = append(preserved, entries[3])
	}
	for _, e := range preserved {
		read, err := restarted.ReadReviewBody(f.ctx, f.subject, e.Document.ContentRef, keys, now.Add(90*time.Minute))
		if err != nil || !bytes.Equal(read.Bytes, bodies[e.Document.ContentRef]) {
			t.Fatalf("copied original history %s: %v", e.Document.Revision, err)
		}
	}
	if fetches.Load() != count {
		t.Fatal("XI historical read fetched UR")
	}
	// Custody is monotonic even after own expiry; it never silently resumes UR.
	if _, err = f.consumerStore.ExpireReviewBodies(f.ctx, now.Add(3*time.Hour), 10); err != nil {
		t.Fatal(err)
	}
	if err = restarted.FinishLegacyCCustodyTransition(f.ctx, c.ID, now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertPending(false)
}
