package db_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/integration/reviewcopy"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestURCReviewBodyPostgresFirstExceptionActivationWaitsForUploadGate(t *testing.T) {
	if os.Getenv("XINSIGHT_POSTGRES_DSN") == "" {
		t.Skip("explicit PostgreSQL fixture required")
	}
	f := newSourceFixture(t)
	now := time.Now().UTC()
	collection := sourcefacts.ContentCollection{ID: testUUID(t), SourceSystemID: f.source,
		SourceInstance: "content-source", OrgID: "7", ProjectID: "9", CredentialID: "c-gate",
		RegistrationHash: sourcefacts.Hash([]byte("gate-registration")), ContentScopeRevision: "scope-gate",
		RecoveryEpoch: "1", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview},
		CoverageFrom: sourcefacts.ContentBoundary{Time: now.Add(-time.Minute).Format(sourcefacts.TimeFormat), AfterSequence: "0"}}
	collection, err := f.consumerStore.RegisterContentCollection(f.ctx, collection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.consumerStore.ControlContentCollection(f.ctx, collection.ID, "active", collection.Revision, false, collection); err != nil {
		t.Fatal(err)
	}
	revision, err := f.consumerStore.RequestReviewCaptureDefault(f.ctx, db.ReviewCaptureSettings{
		Mode: "full_content", RetentionSeconds: 3600, ProjectQuotaBytes: 1024, MaxItemBytes: 1024,
	}, 0, "xs_admin", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.ConfirmReviewCaptureDefault(f.ctx, revision, "xs_admin", now); err != nil {
		t.Fatal(err)
	}
	ref := "xipe_" + strings.Repeat("a", 32)
	exceptionRevision, err := f.consumerStore.RequestReviewCaptureException(f.ctx, db.ReviewCaptureException{
		PolicyRef: ref, SelectorKind: "source_project", SourceSystemID: f.source,
		SourceInstance: "content-source", OrgID: "7", ProjectID: "9", Mode: "disabled",
		RetentionSeconds: 3600, MaxItemBytes: 0, Provenance: "synthetic-test",
	}, 0, "xs_admin", now)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.admin.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var gate int64
	if err = tx.QueryRowContext(f.ctx, `SELECT effective_revision FROM review_capture_defaults WHERE singleton FOR SHARE`).Scan(&gate); err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	defer cancel()
	if err = f.consumerStore.ConfirmReviewCaptureException(blocked, ref, exceptionRevision, "xs_admin", now); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first exception activation passed an in-flight upload gate: %v", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.ConfirmReviewCaptureException(f.ctx, ref, exceptionRevision, "xs_admin", now); err != nil {
		t.Fatal(err)
	}
}

func TestURCReviewBodyPostgresExactManifestAndFixedExpiry(t *testing.T) {
	if os.Getenv("XINSIGHT_POSTGRES_DSN") == "" {
		t.Skip("explicit PostgreSQL fixture required")
	}
	createConnectorTestRoles(t, t.Context(), os.Getenv("XINSIGHT_POSTGRES_DSN"))
	f := newSourceFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	at := now.Add(-time.Minute).Format(sourcefacts.TimeFormat)
	body := []byte(`{"prompt":"synthetic"}`)
	c := sourcefacts.ContentCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: "content-source",
		OrgID: "7", ProjectID: "9", CredentialID: "c-one", RegistrationHash: sourcefacts.Hash([]byte("registration-one")),
		ContentScopeRevision: "scope-one", RecoveryEpoch: "1", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview},
		CoverageFrom: sourcefacts.ContentBoundary{Time: at, AfterSequence: "0"}}
	var err error
	c, err = f.consumerStore.RegisterContentCollection(f.ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	c, err = f.consumerStore.ControlContentCollection(f.ctx, c.ID, "active", c.Revision, false, c)
	if err != nil {
		t.Fatal(err)
	}
	doc := sourcefacts.ContentManifest{SchemaVersion: sourcefacts.ContentManifestVersion, SourceInstance: c.SourceInstance,
		OrgID: c.OrgID, ProjectID: c.ProjectID, RequestID: "content-only", Profile: sourcefacts.ContentProfile,
		Revision: "1", OccurredAt: at, RecordedAt: at, Protocol: "openai.responses", ObservationPoint: "client_request",
		ContentItemID: "body", Capture: sourcefacts.ContentCapture{Mode: "full_content", PolicyRef: "policy",
			PolicyVersion: "1", PolicyEffectiveAt: at, Completeness: "complete", Reason: "none"},
		ContentRef: "uc1_" + strings.Repeat("a", 64), Representation: &sourcefacts.ContentRepresentation{
			MediaType: "application/json", Encoding: "identity", ByteDomain: "stored_representation_before_http_content_encoding",
			SavedBytes: int64(len(body)), Digest: sourcefacts.Hash(body)}, CapturedAt: at,
		ExpiresAt: now.Add(time.Hour).Format(sourcefacts.TimeFormat)}
	doc.SourceFactID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion, doc.SourceInstance, doc.OrgID,
		doc.ProjectID, doc.Profile, doc.RequestID, doc.ObservationPoint, doc.AttemptID, doc.ContentItemID)
	doc.EventID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion+".revision", doc.SourceFactID, doc.Revision)
	canonical := func(v any) []byte {
		t.Helper()
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		obj, e := connector.DecodeStrictJSONObject(raw)
		if e != nil {
			t.Fatal(e)
		}
		raw, e = connector.CanonicalizeJSONValue(obj)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	entry := sourcefacts.ContentManifestEntry{Sequence: "1", Document: doc, SourceSHA256: sourcefacts.Hash(canonical(doc)),
		Availability: sourcefacts.ContentAvailability{State: "available", EvaluatedAt: at}}
	page := sourcefacts.ContentManifestPage{SchemaVersion: sourcefacts.ContentManifestPageVersion, Profile: sourcefacts.ContentProfile,
		SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, GeneratedAt: at,
		ContentScopeRevision: c.ContentScopeRevision, RecoveryEpoch: c.RecoveryEpoch, CoverageFrom: c.CoverageFrom,
		RetentionFloor: c.CoverageFrom, AfterSequence: "0", LastSequence: "1", HighWatermarkSequence: "1",
		NextCursor: "next", HighWatermark: "high", Coverage: "bounded", Gaps: []sourcefacts.ContentGap{},
		Items: []sourcefacts.ContentManifestEntry{entry}}
	if err = f.consumerStore.CommitContentManifests(f.ctx, c, canonical(page), now); err != nil {
		t.Fatal(err)
	}
	ref, err := sourcefacts.FrozenContentManifestRef(f.source, entry)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := f.consumerStore.RequestReviewCaptureDefault(f.ctx, db.ReviewCaptureSettings{
		Mode: "full_content", RetentionSeconds: 7200,
		ProjectQuotaBytes: 1024, MaxItemBytes: 1024}, 0, "xs_admin", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.ConfirmReviewCaptureDefault(f.ctx, revision, "xs_admin", now); err != nil {
		t.Fatal(err)
	}
	xiProjectID := testUUID(t)
	keys := reviewbody.KeyRing{ActiveID: "synthetic", Keys: map[string][]byte{"synthetic": bytes.Repeat([]byte{7}, 32)}}
	metadata := sourcefacts.ContentReadMetadata{SchemaVersion: sourcefacts.ContentReadMetadataVersion,
		SourceInstance: ref.SourceInstance, OrgID: ref.OrgID, ProjectID: ref.ProjectID, RequestID: ref.RequestID,
		SourceFactID: ref.SourceFactID, Revision: ref.Revision, SourceSHA256: ref.SourceSHA256,
		ContentRef: ref.ContentRef, Representation: *ref.Representation, ExpiresAt: ref.ExpiresAt}
	header := base64.RawURLEncoding.EncodeToString(canonical(metadata))
	var available atomic.Bool
	var fetches atomic.Int64
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/integrations/xinsight/source/v2/contents/"+ref.ContentRef ||
			r.URL.Query().Get("purpose") != "human_review" || r.URL.Query().Get("source_fact_id") != ref.SourceFactID {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fetches.Add(1)
		if !available.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", `attachment; filename="content.bin"`)
		w.Header().Set(pullfeed.ContentMetadataHeader, header)
		_, _ = w.Write(body)
	}))
	defer source.Close()
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: source.URL + sourcefacts.ContentEndpointPath,
		ServiceKey: "synthetic-content-key", AllowPrivateTunnelHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	// The source worker uses the formal workspace role. Apply the actual host
	// grant set in this isolated database, without changing shared role credentials.
	copyCredentials := reviewCopyWorkspaceCredentials(t, f.ctx, f.dsn)
	workerStore, err := db.OpenWorkspaceStore(f.ctx, copyCredentials.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer workerStore.Close()
	workerDB := openTestSQL(t, copyCredentials.WorkspaceDSN)
	defer workerDB.Close()
	var currentRole string
	if err = workerDB.QueryRowContext(f.ctx, `SELECT current_user`).Scan(&currentRole); err != nil || currentRole != "xinsight_workspace" {
		t.Fatalf("copy role=%q error=%v", currentRole, err)
	}
	for _, statement := range []string{
		`UPDATE connector_source_enrollments SET legacy_c_copy_pending=false WHERE false`,
		`UPDATE source_content_manifests SET availability=availability WHERE false`,
		`UPDATE source_content_collections SET status=status WHERE false`,
	} {
		_, updateErr := workerDB.ExecContext(f.ctx, statement)
		var pgErr *pgconn.PgError
		if !errors.As(updateErr, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("source worker UPDATE must fail with insufficient_privilege: %v", updateErr)
		}
	}
	if err = workerStore.FinishLegacyCCustodyTransition(f.ctx, c.ID, now); !errors.Is(err, sourcefacts.ErrAuthorization) {
		t.Fatalf("source worker crossed owner custody boundary: %v", err)
	}
	limits := sourcefacts.ContentLimits{MaxReadBytes: 32 << 20, MaxBufferBytes: 1 << 20}
	if _, err = reviewcopy.CopyURCReviewBody(f.ctx, reader, workerStore, c.ID, ref, limits, keys,
		func() time.Time { return now }); !errors.Is(err, reviewbody.ErrProjectBindingPending) || fetches.Load() != 0 {
		t.Fatalf("unbound XI project fetched source or admitted body: err=%v fetches=%d", err, fetches.Load())
	}
	if err = f.consumerStore.BindReviewCaptureProject(f.ctx, f.source, c.SourceInstance, c.OrgID, c.ProjectID,
		xiProjectID, "Synthetic XI project", "xs_admin", now); err != nil {
		t.Fatal(err)
	}
	candidates, err := workerStore.ListURCCopyCandidates(f.ctx, c.ID, now, 16)
	if err != nil || len(candidates) != 1 || candidates[0].Ref.ContentRef != ref.ContentRef {
		t.Fatalf("committed manifest recovery candidates=%+v error=%v", candidates, err)
	}
	if err = reviewcopy.PollURCCopies(f.ctx, reader, workerStore, c.ID, limits, keys, func() time.Time { return now }); err != nil {
		t.Fatalf("item failure changed managed metadata result: %v", err)
	}
	var attemptStatus string
	if err = f.admin.QueryRow(`SELECT status FROM review_body_copy_attempts WHERE collection_id=$1::uuid AND content_ref=$2`, c.ID, ref.ContentRef).Scan(&attemptStatus); err != nil || attemptStatus != "retry" {
		t.Fatalf("copy attempt status=%q error=%v", attemptStatus, err)
	}
	candidates, err = workerStore.ListURCCopyCandidates(f.ctx, c.ID, now.Add(time.Second), 16)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("retry ran before due time: %+v %v", candidates, err)
	}
	if fetches.Load() != 1 {
		t.Fatalf("initial source fetches=%d", fetches.Load())
	}
	available.Store(true)
	quotaScope := "project:" + xiProjectID
	f.exec(`INSERT INTO review_body_project_usage(quota_scope,stored_bytes) VALUES($1,1024)`, quotaScope)
	if err = reviewcopy.PollURCCopies(f.ctx, reader, workerStore, c.ID, limits, keys, func() time.Time { return now.Add(61 * time.Second) }); err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 1 {
		t.Fatalf("full quota triggered unnecessary source fetches=%d", fetches.Load())
	}
	f.exec(`UPDATE review_body_project_usage SET stored_bytes=0 WHERE quota_scope=$1`, quotaScope)
	copyAt := now.Add(122 * time.Second)
	if err = reviewcopy.PollURCCopies(f.ctx, reader, workerStore, c.ID, limits, keys, func() time.Time { return copyAt }); err != nil {
		t.Fatal(err)
	}
	if err = f.admin.QueryRow(`SELECT status FROM review_body_copy_attempts WHERE collection_id=$1::uuid AND content_ref=$2`, c.ID, ref.ContentRef).Scan(&attemptStatus); err != nil || attemptStatus != "accepted" {
		t.Fatalf("recovered copy status=%q error=%v", attemptStatus, err)
	}
	var storedState string
	if err = f.admin.QueryRow(`SELECT state FROM review_body_operations WHERE content_ref=$1`, ref.ContentRef).Scan(&storedState); err != nil || storedState != "accepted" {
		t.Fatalf("independent XI copy state=%q error=%v", storedState, err)
	}
	if fetches.Load() != 2 {
		t.Fatalf("recovered source fetches=%d", fetches.Load())
	}
	replay, err := workerStore.CommitURCReviewBody(f.ctx, c.ID, ref, body, keys, copyAt.Add(time.Second))
	if err != nil || replay.Outcome != "identical_replay" || replay.SHA256 != sourcefacts.Hash(body) {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	if err := db.ValidateReviewBodyRestore(f.ctx, f.admin, keys, copyAt); err != nil {
		t.Fatalf("restore custody key validation: %v", err)
	}
	wrongKeys := reviewbody.KeyRing{ActiveID: keys.ActiveID, Keys: map[string][]byte{keys.ActiveID: bytes.Repeat([]byte{8}, 32)}}
	if err := db.ValidateReviewBodyRestore(f.ctx, f.admin, wrongKeys, copyAt); err == nil {
		t.Fatal("restore accepted wrong custody key")
	}
	unknownKeys := reviewbody.KeyRing{ActiveID: "unknown", Keys: map[string][]byte{"unknown": bytes.Repeat([]byte{7}, 32)}}
	if err := db.ValidateReviewBodyRestore(f.ctx, f.admin, unknownKeys, copyAt); err == nil {
		t.Fatal("restore accepted unknown stored key ID")
	}
	identity := sourcefacts.Document{SchemaVersion: sourcefacts.Version, SourceInstance: ref.SourceInstance,
		OrgID: ref.OrgID, ProjectID: ref.ProjectID, RequestID: ref.RequestID,
		Profile: sourcefacts.IdentityProfile, Stage: "authenticated", Revision: "1",
		OccurredAt: at, RecordedAt: at, CaptureStatus: "available",
		Identity: map[string]any{"owner_user_id": "42", "authority_read": "live_key", "actual_human": "unknown"}}
	identity.SourceFactID = sourcefacts.SourceID(sourcefacts.Version, identity.SourceInstance, identity.OrgID,
		identity.ProjectID, identity.Profile, identity.RequestID, identity.Stage)
	identity.EventID = sourcefacts.SourceID(sourcefacts.Version+".revision", identity.SourceFactID, identity.Revision)
	identityRaw := canonical(identity)
	if _, err = sourcefacts.DecodeDocument(identityRaw); err != nil {
		t.Fatalf("synthetic B identity is invalid: %v", err)
	}
	identityHash := sourcefacts.Hash(identityRaw)
	f.exec(`INSERT INTO source_fact_documents(source_system_id,source_instance,org_id,project_id,request_id,
 profile,stage,source_fact_id,revision,event_id,previous_hash,source_hash,envelope_hash,source_bytes,
 envelope_bytes,source_json,occurred_at,recorded_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,1,$9,'',$10,$10,$11,$11,$12::jsonb,$13,$13)`,
		f.source, identity.SourceInstance, identity.OrgID, identity.ProjectID, identity.RequestID,
		identity.Profile, identity.Stage, identity.SourceFactID, identity.EventID, identityHash,
		identityRaw, string(identityRaw), at)
	// A different, already authenticated personal request proves that account
	// 42 is selectable, while this manifest's own B context remains unresolved.
	knownIdentity := identity
	knownIdentity.RequestID = "known-personal"
	knownIdentity.SourceFactID = sourcefacts.SourceID(sourcefacts.Version, knownIdentity.SourceInstance,
		knownIdentity.OrgID, knownIdentity.ProjectID, knownIdentity.Profile, knownIdentity.RequestID, knownIdentity.Stage)
	knownIdentity.EventID = sourcefacts.SourceID(sourcefacts.Version+".revision", knownIdentity.SourceFactID, knownIdentity.Revision)
	knownIdentityRaw := canonical(knownIdentity)
	if _, err = sourcefacts.DecodeDocument(knownIdentityRaw); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO source_fact_documents(source_system_id,source_instance,org_id,project_id,request_id,
 profile,stage,source_fact_id,revision,event_id,previous_hash,source_hash,envelope_hash,source_bytes,
 envelope_bytes,source_json,occurred_at,recorded_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,1,$9,'',$10,$10,$11,$11,$12::jsonb,$13,$13)`,
		f.source, knownIdentity.SourceInstance, knownIdentity.OrgID, knownIdentity.ProjectID, knownIdentity.RequestID,
		knownIdentity.Profile, knownIdentity.Stage, knownIdentity.SourceFactID, knownIdentity.EventID,
		sourcefacts.Hash(knownIdentityRaw), knownIdentityRaw, string(knownIdentityRaw), at)
	knownContext := sourcefacts.Document{SchemaVersion: sourcefacts.Version, SourceInstance: ref.SourceInstance,
		OrgID: ref.OrgID, ProjectID: ref.ProjectID, RequestID: knownIdentity.RequestID,
		Profile: sourcefacts.ContextProfile, Stage: "authenticated", Revision: "1", OccurredAt: at, RecordedAt: at,
		CaptureStatus: "available", Core: map[string]any{"request_id": knownIdentity.RequestID,
			"org_id": ref.OrgID, "project_id": ref.ProjectID, "api_key_id": "1", "key_type": "personal",
			"auth_kind": "api_key", "auth_epoch": "0", "operation": "chat.completions",
			"protocol": "openai.v1", "captured_at": at}}
	knownContext.SourceFactID = sourcefacts.SourceID(sourcefacts.Version, knownContext.SourceInstance,
		knownContext.OrgID, knownContext.ProjectID, knownContext.Profile, knownContext.RequestID, knownContext.Stage)
	knownContext.EventID = sourcefacts.SourceID(sourcefacts.Version+".revision", knownContext.SourceFactID, knownContext.Revision)
	knownContextRaw := canonical(knownContext)
	if _, err = sourcefacts.DecodeDocument(knownContextRaw); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO source_fact_documents(source_system_id,source_instance,org_id,project_id,request_id,
 profile,stage,source_fact_id,revision,event_id,previous_hash,source_hash,envelope_hash,source_bytes,
 envelope_bytes,source_json,occurred_at,recorded_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,1,$9,'',$10,$10,$11,$11,$12::jsonb,$13,$13)`,
		f.source, knownContext.SourceInstance, knownContext.OrgID, knownContext.ProjectID, knownContext.RequestID,
		knownContext.Profile, knownContext.Stage, knownContext.SourceFactID, knownContext.EventID,
		sourcefacts.Hash(knownContextRaw), knownContextRaw, string(knownContextRaw), at)
	accounts, err := f.workspace.ListReviewCaptureExceptions(f.ctx)
	if err != nil || len(accounts.Accounts) != 1 || accounts.Accounts[0].AccountID != "42" {
		t.Fatalf("verified personal-key owner catalog: %+v %v", accounts.Accounts, err)
	}
	accountException := db.ReviewCaptureException{PolicyRef: "xipe_" + strings.Repeat("b", 32),
		SelectorKind: "account", SourceSystemID: f.source, SourceInstance: ref.SourceInstance,
		OrgID: ref.OrgID, ProjectID: ref.ProjectID, AccountAuthority: "ur_key_owner", AccountID: "42",
		Mode: "disabled", RetentionSeconds: 3600, MaxItemBytes: 0, Provenance: "synthetic-ur-account"}
	wrongOwner := accountException
	wrongOwner.AccountID = "43"
	wrongOwner.PolicyRef = "xipe_" + strings.Repeat("c", 32)
	if _, err = f.consumerStore.RequestReviewCaptureException(f.ctx, wrongOwner, 0, "xs_admin", copyAt); !errors.Is(err, reviewbody.ErrScope) {
		t.Fatalf("unverified account exception accepted: %v", err)
	}
	exceptionRevision, err := f.consumerStore.RequestReviewCaptureException(f.ctx, accountException, 0, "xs_admin", copyAt)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.ConfirmReviewCaptureException(f.ctx, accountException.PolicyRef, exceptionRevision, "xs_admin", copyAt); err != nil {
		t.Fatal(err)
	}
	if err = workerStore.CheckURCReviewCopy(f.ctx, c.ID, ref, copyAt.Add(time.Second)); !errors.Is(err, reviewbody.ErrIdentityPending) {
		t.Fatalf("late identity must hold source copy while an account exception may match: %v", err)
	}
	beforePendingFetch := fetches.Load()
	if _, err = reviewcopy.CopyURCReviewBody(f.ctx, reader, workerStore, c.ID, ref, limits, keys,
		func() time.Time { return copyAt.Add(time.Second) }); !errors.Is(err, reviewbody.ErrIdentityPending) || fetches.Load() != beforePendingFetch {
		t.Fatalf("pending B identity fetched source bytes: error=%v fetches=%d", err, fetches.Load())
	}
	contextFact := sourcefacts.Document{SchemaVersion: sourcefacts.Version, SourceInstance: ref.SourceInstance,
		OrgID: ref.OrgID, ProjectID: ref.ProjectID, RequestID: ref.RequestID,
		Profile: sourcefacts.ContextProfile, Stage: "authenticated", Revision: "1",
		OccurredAt: at, RecordedAt: at, CaptureStatus: "available",
		Core: map[string]any{"request_id": ref.RequestID, "org_id": ref.OrgID, "project_id": ref.ProjectID,
			"api_key_id": "1", "key_type": "service", "auth_kind": "api_key", "auth_epoch": "0",
			"operation": "chat.completions", "protocol": "openai.v1", "captured_at": at}}
	contextFact.SourceFactID = sourcefacts.SourceID(sourcefacts.Version, contextFact.SourceInstance, contextFact.OrgID,
		contextFact.ProjectID, contextFact.Profile, contextFact.RequestID, contextFact.Stage)
	contextFact.EventID = sourcefacts.SourceID(sourcefacts.Version+".revision", contextFact.SourceFactID, contextFact.Revision)
	contextRaw := canonical(contextFact)
	if _, err = sourcefacts.DecodeDocument(contextRaw); err != nil {
		t.Fatalf("synthetic B context is invalid: %v", err)
	}
	contextHash := sourcefacts.Hash(contextRaw)
	f.exec(`INSERT INTO source_fact_documents(source_system_id,source_instance,org_id,project_id,request_id,
 profile,stage,source_fact_id,revision,event_id,previous_hash,source_hash,envelope_hash,source_bytes,
 envelope_bytes,source_json,occurred_at,recorded_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,1,$9,'',$10,$10,$11,$11,$12::jsonb,$13,$13)`,
		f.source, contextFact.SourceInstance, contextFact.OrgID, contextFact.ProjectID, contextFact.RequestID,
		contextFact.Profile, contextFact.Stage, contextFact.SourceFactID, contextFact.EventID, contextHash,
		contextRaw, string(contextRaw), at)
	if err = workerStore.CheckURCReviewCopy(f.ctx, c.ID, ref, copyAt.Add(time.Second)); err != nil {
		t.Fatalf("service key owner must not match personal-account exception: %v", err)
	}
	contextFact.PreviousRevision, contextFact.PreviousHash, contextFact.CorrectionReason = "1", contextHash, "source_correction"
	contextFact.Revision = "2"
	contextFact.Core["key_type"] = "personal"
	contextFact.EventID = sourcefacts.SourceID(sourcefacts.Version+".revision", contextFact.SourceFactID, contextFact.Revision)
	contextRaw = canonical(contextFact)
	if _, err = sourcefacts.DecodeDocument(contextRaw); err != nil {
		t.Fatalf("synthetic corrected B context is invalid: %v", err)
	}
	contextHash = sourcefacts.Hash(contextRaw)
	f.exec(`INSERT INTO source_fact_documents(source_system_id,source_instance,org_id,project_id,request_id,
 profile,stage,source_fact_id,revision,event_id,previous_hash,source_hash,envelope_hash,source_bytes,
 envelope_bytes,source_json,occurred_at,recorded_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,2,$9,$10,$11,$11,$12,$12,$13::jsonb,$14,$14)`,
		f.source, contextFact.SourceInstance, contextFact.OrgID, contextFact.ProjectID, contextFact.RequestID,
		contextFact.Profile, contextFact.Stage, contextFact.SourceFactID, contextFact.EventID, contextFact.PreviousHash,
		contextHash, contextRaw, string(contextRaw), at)
	if err = workerStore.CheckURCReviewCopy(f.ctx, c.ID, ref, copyAt.Add(time.Second)); !errors.Is(err, reviewbody.ErrPolicy) {
		t.Fatalf("verified UR key-owner exception did not block new copy: %v", err)
	}
	identity.PreviousRevision, identity.PreviousHash, identity.CorrectionReason = "1", identityHash, "source_correction"
	identity.Revision = "2"
	identity.Identity["owner_user_id"] = "43"
	identity.EventID = sourcefacts.SourceID(sourcefacts.Version+".revision", identity.SourceFactID, identity.Revision)
	identityRaw = canonical(identity)
	if _, err = sourcefacts.DecodeDocument(identityRaw); err != nil {
		t.Fatalf("synthetic corrected B identity is invalid: %v", err)
	}
	identityHash = sourcefacts.Hash(identityRaw)
	f.exec(`INSERT INTO source_fact_documents(source_system_id,source_instance,org_id,project_id,request_id,
 profile,stage,source_fact_id,revision,event_id,previous_hash,source_hash,envelope_hash,source_bytes,
 envelope_bytes,source_json,occurred_at,recorded_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,2,$9,$10,$11,$11,$12,$12,$13::jsonb,$14,$14)`,
		f.source, identity.SourceInstance, identity.OrgID, identity.ProjectID, identity.RequestID,
		identity.Profile, identity.Stage, identity.SourceFactID, identity.EventID, identity.PreviousHash,
		identityHash, identityRaw, string(identityRaw), at)
	if err = workerStore.CheckURCReviewCopy(f.ctx, c.ID, ref, copyAt.Add(time.Second)); err != nil {
		t.Fatalf("different authenticated personal key owner incorrectly matched account 42: %v", err)
	}
	// A successful preflight cannot authorize a later commit after the owner
	// pauses the collection while the source request is in flight.
	liveCollection, err := f.consumerStore.LoadContentCollection(f.ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := f.consumerStore.ControlContentCollection(f.ctx, c.ID, "paused", liveCollection.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = workerStore.CommitURCReviewBody(f.ctx, c.ID, ref, body, keys, copyAt.Add(time.Second)); !errors.Is(err, reviewbody.ErrScope) {
		t.Fatalf("copy commit accepted a collection paused after preflight: %v", err)
	}
	if _, err = f.consumerStore.ControlContentCollection(f.ctx, c.ID, "active", paused.Revision, false, paused); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_content=true,allowed_content_purposes='["human_review"]' WHERE subject_id=$1`, f.subject)
	read, err := workerStore.ReadReviewBody(f.ctx, f.subject, ref.ContentRef, keys, copyAt.Add(time.Second))
	if err != nil || !bytes.Equal(read.Bytes, body) {
		t.Fatalf("read: %+v %v", read, err)
	}
	bad := ref
	bad.SourceSHA256 = sourcefacts.Hash([]byte("wrong"))
	if _, err = workerStore.CommitURCReviewBody(f.ctx, c.ID, bad, body, keys, now); !errors.Is(err, reviewbody.ErrConflict) {
		t.Fatalf("wrong immutable manifest accepted: %v", err)
	}
	if _, err = workerStore.CommitURCReviewBody(f.ctx, c.ID, ref, bytes.Repeat([]byte{'x'}, len(body)), keys, now); !errors.Is(err, reviewbody.ErrIntegrity) {
		t.Fatalf("wrong digest accepted: %v", err)
	}
	if _, err = f.workspace.ReadReviewBody(f.ctx, f.subject, ref.ContentRef, keys, now.Add(90*time.Minute)); err != nil {
		t.Fatalf("accepted XI copy lost when UR source expired: %v", err)
	}
	assertManagedCustodyRuntimeHTTPS(t, f, c, ref, keys, body, now.Add(90*time.Minute), fetches.Load(), &fetches)
	f.exec(`UPDATE workbench_access_grants SET allow_content=false,allowed_content_purposes='[]' WHERE subject_id=$1`, f.subject)
	if _, err = f.workspace.ReadReviewBody(f.ctx, f.subject, ref.ContentRef, keys, now.Add(90*time.Minute)); err == nil {
		t.Fatal("revoked human content grant retained read access")
	}
	sweptAt := copyAt.Add(2*time.Hour + time.Second)
	if err := db.ValidateReviewBodyRestore(f.ctx, f.admin, reviewbody.KeyRing{}, sweptAt); err != nil {
		t.Fatalf("expired bodies incorrectly require keys: %v", err)
	}
	if count, sweepErr := f.consumerStore.ExpireReviewBodies(f.ctx, sweptAt, 10); sweepErr != nil || count != 1 {
		t.Fatalf("managed expiry count=%d error=%v", count, sweepErr)
	}
	var objectCount, quotaBytes int64
	if err = f.admin.QueryRow(`SELECT count(*) FROM review_body_objects WHERE content_ref=$1`, ref.ContentRef).Scan(&objectCount); err != nil || objectCount != 0 {
		t.Fatalf("expired ciphertext remains: count=%d error=%v", objectCount, err)
	}
	if err = f.admin.QueryRow(`SELECT stored_bytes FROM review_body_project_usage WHERE quota_scope=$1`, quotaScope).Scan(&quotaBytes); err != nil || quotaBytes != 0 {
		t.Fatalf("expired copy did not release quota: bytes=%d error=%v", quotaBytes, err)
	}
	if err = f.admin.QueryRow(`SELECT state FROM review_body_operations WHERE content_ref=$1`, ref.ContentRef).Scan(&storedState); err != nil || storedState != "expired" {
		t.Fatalf("expiry lost metadata state=%q error=%v", storedState, err)
	}
}

// Use the same host grant set and role DSN for standalone and managed C copy.
// The roles themselves must be prepared before the isolated database fixture.
func reviewCopyWorkspaceCredentials(t *testing.T, ctx context.Context, adminDSN string) hostctl.DatabaseCredentials {
	t.Helper()
	credentials := hostctl.DatabaseCredentials{AdminDSN: adminDSN}
	var err error
	credentials.Admin, err = pgx.ParseConfig(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"reader", "workspace", "importer"} {
		roleDSN := connectorRoleDSN(t, adminDSN, "xinsight_"+role, role == "reader")
		roleConfig, parseErr := pgx.ParseConfig(roleDSN)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		switch role {
		case "reader":
			credentials.ReaderDSN, credentials.Reader = roleDSN, roleConfig
		case "workspace":
			credentials.WorkspaceDSN, credentials.Workspace = roleDSN, roleConfig
		case "importer":
			credentials.ImporterDSN, credentials.Importer = roleDSN, roleConfig
		}
	}
	if err = (hostctl.PostgresManager{}).ApplyGrants(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	return credentials
}

type custodyFixtureProvider struct{ subject string }

func (p custodyFixtureProvider) Authenticate(username string, password []byte) (access.Principal, bool) {
	return access.Principal{SubjectID: p.subject, Role: access.RoleWorkbenchReader}, username == "custody.fixture" && string(password) == "fixture-password"
}

func assertManagedCustodyRuntimeHTTPS(t *testing.T, f *sourceFixture, c sourcefacts.ContentCollection, ref sourcefacts.FrozenManifestRef, keys reviewbody.KeyRing, body []byte, now time.Time, before int64, fetches *atomic.Int64) {
	t.Helper()
	// Synthetic control membership selects custody-only; the original collection,
	// manifest, encrypted copy, human grant and production reading stay real PG.
	id := "xci_AAAAAAAAAAAAAAAAAAAAAA"
	f.exec(`INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,status,desired_state,observed_state,created_at,updated_at) VALUES($1,'xid_AAAAAAAAAAAAAAAAAAAAAA','unirouter',ARRAY['model.request.completed'],'pending','active','awaiting_consent',$2,$2)`, id, now)
	f.exec(`INSERT INTO connector_source_enrollments(installation_id,enrollment_id,source_instance,org_id,project_id,revision,content_collection_id,status,updated_at) VALUES($1,'xise_1',$2,$3,$4,'1',$5,'active',$6)`, id, c.SourceInstance, c.OrgID, c.ProjectID, c.ID, now)
	start, err := time.Parse(sourcefacts.TimeFormat, c.CoverageFrom.Time)
	if err != nil {
		t.Fatal(err)
	}
	start = start.Add(-time.Minute)
	end := now
	reading, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: workbench.PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "Asia/Shanghai"}}, now)
	if err != nil {
		t.Fatal("custody runtime reading", err)
	}
	requests, err := f.workspace.GetRuntimeRequests(f.ctx, f.subject, reading.ID, "all", 100, "", now)
	if err != nil {
		t.Fatal(err)
	}
	requestRef := ""
	for _, request := range requests.Items {
		if request.RequestID == ref.RequestID {
			requestRef = request.Ref
		}
	}
	if requestRef == "" {
		t.Fatal("content-only request absent")
	}
	contents, err := f.workspace.GetRuntimeContents(f.ctx, f.subject, reading.ID, requestRef, 100, "", now)
	if err != nil || len(contents.Items) != 1 || contents.Items[0].Availability.State != "available" || contents.Items[0].Reference.ExpiresAt != ref.ExpiresAt {
		t.Fatalf("XI availability lost original source expiry: %+v %v", contents, err)
	}
	if _, err = f.workspace.ResolveRuntimeContent(f.ctx, f.subject, reading.ID, requestRef, ref, now); err != nil {
		t.Fatal("source expired but XI admission failed", err)
	}
	service, err := access.NewService(custodyFixtureProvider{f.subject}, access.DiscardAuditor{}, access.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := service.Login("custody.fixture", []byte("fixture-password"), "")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(api.NewHandlerWithDependencies(api.HandlerDependencies{Store: f.evidence, WorkspaceStore: f.workspace, AccessService: service, ReviewBodyKeys: keys, Now: func() time.Time { return now }}))
	defer server.Close()
	path := "/api/v1/runtime/readings/" + reading.ID + "/requests/" + requestRef + "/contents/" + ref.ContentRef + "?source_fact_id=" + ref.SourceFactID + "&revision=" + ref.Revision
	request, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: access.SessionCookieName, Value: token})
	sessionRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/access/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	sessionRequest.AddCookie(&http.Cookie{Name: access.SessionCookieName, Value: token})
	sessionResponse, err := server.Client().Do(sessionRequest)
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		ScopeRevision string `json:"scope_revision"`
	}
	err = json.NewDecoder(sessionResponse.Body).Decode(&session)
	sessionResponse.Body.Close()
	if err != nil || session.ScopeRevision == "" {
		t.Fatal("current human scope", err)
	}
	request.Header.Set("X-XInsight-Scope-Revision", session.ScopeRevision)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Equal(raw, body) {
		t.Fatalf("production custody HTTPS status=%d bytes=%q error=%v", response.StatusCode, raw, err)
	}
	until, err := time.Parse(sourcefacts.TimeFormat, response.Header.Get("X-XInsight-Content-Expires-At"))
	if err != nil || !until.After(now) {
		t.Fatal("missing independent XI read deadline", err)
	}
	if fetches.Load() != before {
		t.Fatal("XI read fetched UR after source expiry")
	}
	stored, err := f.workspace.ReadReviewBody(f.ctx, f.subject, ref.ContentRef, keys, now)
	if err != nil {
		t.Fatal(err)
	}
	workRequest := request.Clone(f.ctx)
	workURL := *request.URL
	workURL.Path, workURL.RawQuery = "/api/v1/work-activity/contents/"+ref.ContentRef, ""
	workRequest.URL = &workURL
	workResponse, err := server.Client().Do(workRequest)
	if err != nil {
		t.Fatal(err)
	}
	workRaw, err := io.ReadAll(workResponse.Body)
	workResponse.Body.Close()
	workUntil, expiryErr := time.Parse(sourcefacts.TimeFormat, workResponse.Header.Get("X-XInsight-Content-Expires-At"))
	if err != nil || expiryErr != nil || workResponse.StatusCode != http.StatusOK || !bytes.Equal(workRaw, body) || !workUntil.Equal(stored.ExpiresAt) {
		t.Fatal("work activity actual XI custody deadline", err, expiryErr)
	}
	now = stored.ExpiresAt.Add(time.Second)
	end = now
	expiredReading, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: workbench.PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "Asia/Shanghai"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	request.URL.Path = strings.Replace(request.URL.Path, reading.ID, expiredReading.ID, 1)
	response, err = server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusGone {
		t.Fatal("XI own expiry did not deny", response.StatusCode)
	}
	workResponse, err = server.Client().Do(workRequest)
	if err != nil {
		t.Fatal(err)
	}
	workResponse.Body.Close()
	if workResponse.StatusCode != http.StatusNotFound || workResponse.Header.Get("X-XInsight-Content-Expires-At") != "" {
		t.Fatal("expired work body leaked available deadline", workResponse.StatusCode)
	}
}
