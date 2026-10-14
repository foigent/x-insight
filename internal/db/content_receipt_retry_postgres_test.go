package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workspace"
)

// Compiled locally; requires a separately admitted real PostgreSQL window.
// The known synthetic JSON location proof covers Store behavior only. The
// source byte verifier and HTTP authorization are tested at their own layer.
func TestContentReceiptRetryPostgresCreationClosureAndRevocation(t *testing.T) {
	f := newSourceFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	at := now.Add(-time.Minute).Format(sourcefacts.TimeFormat)
	c := sourcefacts.ContentCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: "receipt-retry", OrgID: "7", ProjectID: "9", CredentialID: "retry-credential", RegistrationHash: sourcefacts.Hash([]byte("retry-registration")), ContentScopeRevision: "retry-scope", RecoveryEpoch: "1", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: sourcefacts.ContentBoundary{Time: at, AfterSequence: "0"}}
	var err error
	c, err = f.consumerStore.RegisterContentCollection(f.ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	c, err = f.consumerStore.ControlContentCollection(f.ctx, c.ID, "active", c.Revision, false, c)
	if err != nil {
		t.Fatal(err)
	}
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
	d := sourcefacts.ContentManifest{SchemaVersion: sourcefacts.ContentManifestVersion, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, RequestID: "retry-request", Profile: sourcefacts.ContentProfile, Revision: "1", OccurredAt: at, RecordedAt: at, Protocol: "openai.responses", ObservationPoint: "client_request", ContentItemID: "body", Capture: sourcefacts.ContentCapture{Mode: "full_content", PolicyRef: "retry-policy", PolicyVersion: "1", PolicyEffectiveAt: at, Completeness: "complete", Reason: "none"}, ContentRef: "uc1_" + strings.Repeat("a", 64), Representation: &sourcefacts.ContentRepresentation{MediaType: "application/json", Encoding: "identity", ByteDomain: "stored_representation_before_http_content_encoding", SavedBytes: 2, Digest: sourcefacts.Hash([]byte("{}"))}, CapturedAt: at, ExpiresAt: now.Add(time.Minute).Format(sourcefacts.TimeFormat)}
	d.SourceFactID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.ObservationPoint, d.AttemptID, d.ContentItemID)
	d.EventID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion+".revision", d.SourceFactID, d.Revision)
	entry := sourcefacts.ContentManifestEntry{Sequence: "1", Document: d, SourceSHA256: sourcefacts.Hash(canonical(d)), Availability: sourcefacts.ContentAvailability{State: "available", EvaluatedAt: at}}
	page := sourcefacts.ContentManifestPage{SchemaVersion: sourcefacts.ContentManifestPageVersion, Profile: sourcefacts.ContentProfile, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, GeneratedAt: now.Format(sourcefacts.TimeFormat), ContentScopeRevision: c.ContentScopeRevision, RecoveryEpoch: c.RecoveryEpoch, CoverageFrom: c.CoverageFrom, RetentionFloor: c.CoverageFrom, AfterSequence: "0", LastSequence: "1", HighWatermarkSequence: "1", NextCursor: "retry-next", HighWatermark: "retry-high", Coverage: "bounded", Gaps: []sourcefacts.ContentGap{}, Items: []sourcefacts.ContentManifestEntry{entry}}
	if err = f.consumerStore.CommitContentManifests(f.ctx, c, canonical(page), now); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_content=true,allowed_content_purposes='["human_review"]' WHERE subject_id=$1`, f.subject)
	ref, err := sourcefacts.FrozenContentManifestRef(f.source, entry)
	if err != nil {
		t.Fatal(err)
	}
	pointer := ""
	choice := workspace.ContentChoice{Scope: "finding", Relation: "supports", Reference: ref, Location: sourcefacts.ContentLocation{Kind: "json_pointer", Pointer: &pointer}}
	scoped := db.NewScopedWorkchainStore(f.evidence, f.workspace)
	withScope := func(proof bool, fn func(context.Context) error) error {
		base := f.ctx
		if proof {
			base = workspace.WithVerifiedContentLocations(base, f.subject, []workspace.ContentChoice{choice})
		}
		ctx, finish, e := scoped.Begin(base, f.subject, true, func(f func() error) error { return f() })
		if e != nil {
			return e
		}
		defer finish()
		return fn(ctx)
	}
	actor, _ := workspace.HumanActor(f.subject)
	command := workspace.ReviewCreationCommand{Operation: "start_review", OperationID: testUUID(t), NewCase: &workspace.CreateCaseInput{Title: "Content retry"}, Finding: workspace.CreateFindingInput{Title: "Bounded claim", Statement: "Exact saved JSON was reviewed", Classification: "hypothesis"}, Selection: workspace.BasisSelection{Contents: []workspace.ContentChoice{choice}}, Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Inspected the stored representation"}, Reason: "Review content"}
	var created workspace.ReviewCreationReceipt
	if err = withScope(true, func(ctx context.Context) error {
		b, e := f.workspace.PreviewReviewCreation(ctx, command)
		if e != nil {
			return e
		}
		command.ExpectedBasisDigest = b.Digest
		created, e = f.workspace.CreateReview(ctx, actor, command, now, nil)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	closure := workspace.AssessmentCommand{Operation: "record_progress", OperationID: testUUID(t), ExpectedVersion: 1, ExpectedCaseVersion: created.CaseVersion, ExpectedBasisDigest: created.Basis.Digest, Selection: command.Selection, Progress: &workspace.ProgressPayload{State: "ended", Reason: "Verified", Closure: &workspace.Closure{Kind: "verified_remediation", Explanation: "Verified this reference", Scope: "This finding", VerifiedAt: &now, VerificationContents: []workspace.ContentChoice{choice}}}}
	var closed workspace.AssessmentAction
	if err = withScope(true, func(ctx context.Context) error {
		b, e := f.workspace.PreviewFindingBasis(ctx, created.CaseID, created.FindingID, closure.Selection)
		if e != nil {
			return e
		}
		closure.ExpectedBasisDigest = b.Digest
		closed, e = f.workspace.ApplyFindingAssessment(ctx, created.CaseID, created.FindingID, actor, closure, now)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// Pause the source and advance beyond expiry: a retry is not a new adoption.
	c, err = f.consumerStore.LoadContentCollection(f.ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.consumerStore.ControlContentCollection(f.ctx, c.ID, "paused", c.Revision, false, c); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var out string
		e := f.admin.QueryRowContext(f.ctx, `SELECT md5(jsonb_build_array((SELECT to_jsonb(c) FROM investigation_cases c WHERE id=$1),(SELECT jsonb_agg(to_jsonb(a) ORDER BY result_version) FROM case_finding_assessment_actions a WHERE case_id=$1),(SELECT to_jsonb(l) FROM investigation_case_scope_labels l WHERE case_id=$1),(SELECT jsonb_agg(to_jsonb(r)) FROM finding_review_creations r WHERE case_id=$1))::text)`, created.CaseID).Scan(&out)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	before := snapshot()
	if err = withScope(false, func(ctx context.Context) error {
		replay, e := f.workspace.CreateReview(ctx, actor, command, now.Add(time.Hour), nil)
		if e != nil {
			return e
		}
		if !workspace.SameAssessmentValue(replay, created) {
			t.Fatal("creation receipt changed")
		}
		again, e := f.workspace.ApplyFindingAssessment(ctx, created.CaseID, created.FindingID, actor, closure, now.Add(time.Hour))
		if e != nil {
			return e
		}
		if !workspace.SameAssessmentValue(again, closed) {
			t.Fatal("closure receipt changed")
		}
		changed := command
		changed.OperationID = testUUID(t)
		if _, e = f.workspace.CreateReview(ctx, actor, changed, now.Add(time.Hour), nil); !errors.Is(e, workspace.ErrContentLocationProofRequired) {
			t.Fatalf("new operation bypassed proof: %v", e)
		}
		changed = command
		changed.Reason = "different request"
		if _, e = f.workspace.CreateReview(ctx, actor, changed, now.Add(time.Hour), nil); !errors.Is(e, workspace.ErrConflict) {
			t.Fatalf("mutated retry: %v", e)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Fatal("retry changed persisted case, label or receipt")
	}
	f.exec(`UPDATE workbench_access_grants SET allow_content=false,allowed_content_purposes='[]' WHERE subject_id=$1`, f.subject)
	err = withScope(false, func(ctx context.Context) error {
		_, e := f.workspace.CreateReview(ctx, actor, command, now.Add(time.Hour), nil)
		return e
	})
	if !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("revoked caller recovered receipt: %v", err)
	}
}
