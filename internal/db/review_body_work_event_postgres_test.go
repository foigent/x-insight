package db_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/workevent"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestWorkEventReviewBodyRequiresSeparateBindingAndExactReplay(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("isolated PG required")
	}
	ctx := t.Context()
	dsn := migrationTestDSN(t, ctx, base)
	if _, err := db.RunMigrations(ctx, dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	owner, err := db.OpenSourceFactsStore(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	reader, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	now := time.Now().UTC().Truncate(time.Microsecond)
	sourceID, collectionID, projectID := testUUID(t), testUUID(t), testUUID(t)
	if _, err = admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name)
 VALUES($1::uuid,$2,'agent_runtime','Synthetic body source')`, sourceID, "agent-runtime:body:"+sourceID); err != nil {
		t.Fatal(err)
	}
	scope := workevent.Scope{SourceSystemID: sourceID, SourceInstance: "openclaw-body-test", OrgID: "7", ProjectID: "9"}
	collection, err := owner.RegisterWorkEventCollection(ctx, db.WorkEventCollection{ID: collectionID,
		Scope: scope, Profile: workevent.CollectionProfile, ScopeGeneration: "1", Status: "staged", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	collection, err = owner.ActivateWorkEventCollection(ctx, collection.ID, collection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.BindReviewCaptureProject(ctx, sourceID, scope.SourceInstance, scope.OrgID,
		scope.ProjectID, projectID, "Synthetic body project", "xs_1111111111111111", now); err != nil {
		t.Fatal(err)
	}
	revision, err := owner.RequestReviewCaptureDefault(ctx, db.ReviewCaptureSettings{
		Mode: "full_content", RetentionSeconds: 3600, ProjectQuotaBytes: 1024, MaxItemBytes: 512,
	}, 0, "xs_1111111111111111", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.ConfirmReviewCaptureDefault(ctx, revision, "xs_1111111111111111", now); err != nil {
		t.Fatal(err)
	}
	credential, err := reader.CreateWorkEventIngestIdentity(ctx, workspace.Actor{Kind: "root", Ref: "root"},
		db.CreateWorkEventIngestIdentityInput{DisplayName: "Synthetic body producer", ExpiresAt: now.Add(time.Hour),
			Bindings: []db.WorkEventIngestBindingInput{{CollectionID: collectionID, ProducerProjectKey: "agent:main", ProjectLabel: "Synthetic body project"}}}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := workspace.ServiceTokenDigest(credential.Token)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"result":"synthetic tool output"}`)
	sum := sha256.Sum256(body)
	contentRef := "ocbody:review-body-test"
	event := workevent.Event{SchemaVersion: workevent.EventVersion, Reporter: scope, EventRef: "oc:review-body-test", Revision: 1,
		OccurredAt: now.Add(-time.Minute), RecordedAt: now, SubjectKind: "tool_call", LifecycleState: "completed",
		RunID: "run-body-test", ToolCallID: "tool-body-test", Actor: workevent.Actor{Kind: "service", Ref: "openclaw"},
		ActionKind: "tool.completed", ContentState: "referenced",
		ContentRefs: []workevent.ContentRef{{Kind: "tool_output", Ref: contentRef, SHA256: "sha256:" + hex.EncodeToString(sum[:])}}}
	receipt, err := reader.CommitAuthorizedWorkEventItem(ctx, digest, db.WorkEventIngestItem{
		CollectionID: collectionID, ExpectedCollectionRevision: collection.Revision, ScopeGeneration: collection.ScopeGeneration,
		Kind: "event", Document: canonicalWorkEvent(t, event)}, now)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatalf("event admission: %+v %v", receipt, err)
	}
	item := db.WorkEventReviewBodyInput{CollectionID: collectionID, ExpectedCollectionRevision: collection.Revision,
		ScopeGeneration: collection.ScopeGeneration, EventRef: event.EventRef, EventRevision: 1, EventHash: receipt.SHA256,
		ContentKind: "tool_output", ContentRef: contentRef, ByteDomain: "openclaw.after_tool_call.canonical-json.v1",
		SHA256: event.ContentRefs[0].SHA256, Body: body}
	keys := reviewbody.KeyRing{ActiveID: "synthetic", Keys: map[string][]byte{"synthetic": bytes.Repeat([]byte{7}, 32)}}
	if _, err = reader.CommitAuthorizedWorkEventReviewBody(ctx, digest, item, keys, now); !errors.Is(err, reviewbody.ErrScope) {
		t.Fatalf("metadata identity uploaded body without separate binding: %v", err)
	}
	if err = owner.GrantReviewBodyIngestBinding(ctx, credential.ServiceIdentity.ID, collectionID, now); err != nil {
		t.Fatal(err)
	}
	accepted, err := reader.CommitAuthorizedWorkEventReviewBody(ctx, digest, item, keys, now)
	if err != nil || accepted.Outcome != "accepted" || accepted.ContentRef != contentRef {
		t.Fatalf("body admission: %+v %v", accepted, err)
	}
	replay, err := reader.CommitAuthorizedWorkEventReviewBody(ctx, digest, item, keys, now.Add(time.Second))
	if err != nil || replay.Outcome != "identical_replay" || !replay.ExpiresAt.Equal(accepted.ExpiresAt) {
		t.Fatalf("exact body replay: %+v %v", replay, err)
	}
	conflicting := item
	conflicting.ByteDomain = "openclaw.llm_input.utf8.v1"
	if _, err = reader.CommitAuthorizedWorkEventReviewBody(ctx, digest, conflicting, keys, now.Add(time.Second)); err == nil {
		t.Fatal("changed byte domain replay accepted")
	}
}
