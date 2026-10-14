package hostctl

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workevent"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestWorkInstallMigration43RevokesExistingRoleDefaultsBeforeGrantConvergence(t *testing.T) {
	ctx, credentials := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.EnsureRoles(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, credentials.AdminDSN, connectorDigestMigrations(t, 42)); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	admin := managedSQL(t, credentials.AdminDSN)
	var defaultReaders int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM pg_default_acl d
 CROSS JOIN LATERAL aclexplode(d.defaclacl) a
 JOIN pg_roles r ON r.oid=a.grantee
 WHERE d.defaclobjtype='r' AND d.defaclnamespace='public'::regnamespace
 AND r.rolname IN ('xinsight_reader','xinsight_workspace','xinsight_importer')
 AND a.privilege_type='SELECT'`).Scan(&defaultReaders); err != nil || defaultReaders != 3 {
		t.Fatalf("predecessor default SELECT grants: %d %v", defaultReaders, err)
	}
	if applied, err := db.RunMigrations(ctx, credentials.AdminDSN, connectorDigestMigrations(t, 43)); err != nil || applied != 1 {
		t.Fatalf("schema 42 to 43: %d %v", applied, err)
	}
	// M43 must close the inherited default-SELECT window in its own commit,
	// before the later owner grant-convergence transaction.
	for _, role := range []string{"xinsight_reader", "xinsight_workspace", "xinsight_importer"} {
		var canRead bool
		if err := admin.QueryRowContext(ctx, `SELECT has_table_privilege($1,'public.work_event_install_invitations','SELECT')`, role).Scan(&canRead); err != nil || canRead {
			t.Fatalf("%s can read invitation rows before grant convergence: %t %v", role, canRead, err)
		}
	}
	for _, test := range []struct {
		role string
		want bool
	}{{"xinsight_reader", false}, {"xinsight_workspace", true}, {"xinsight_importer", false}} {
		var canExecute bool
		if err := admin.QueryRowContext(ctx, `SELECT has_function_privilege($1,'public.xinsight_managed_work_install_id(text)','EXECUTE')`, test.role).Scan(&canExecute); err != nil || canExecute != test.want {
			t.Fatalf("%s lookup privilege before grant convergence: %t %v", test.role, canExecute, err)
		}
	}
	// Preserve the exact M43 privilege proof before continuing to the current release.
	if applied, err := db.RunMigrations(ctx, credentials.AdminDSN, "../../migrations"); err != nil || applied != currentMigrationSchema(t)-43 {
		t.Fatalf("schema43 to current schema: %d %v", applied, err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	workspaceDB := managedSQL(t, credentials.WorkspaceDSN)
	var managedID sql.NullString
	if err := workspaceDB.QueryRowContext(ctx, `SELECT public.xinsight_managed_work_install_id('unknown')`).Scan(&managedID); err != nil || managedID.Valid {
		t.Fatalf("workspace lookup after grant convergence: %v %v", managedID, err)
	}
}

func TestWorkEventIngestRestrictedRoleAndRevocation(t *testing.T) {
	ctx, credentials := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.EnsureRoles(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, credentials.AdminDSN, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	admin := managedSQL(t, credentials.AdminDSN)
	if output := os.Getenv("XINSIGHT_P2_SCHEMA_OUTPUT"); output != "" {
		manifest, generateErr := GenerateCleanBaselineSchemaContract(ctx, admin)
		if generateErr != nil {
			t.Fatal(generateErr)
		}
		raw, marshalErr := json.Marshal(manifest)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if writeErr := os.WriteFile(output, append(raw, '\n'), 0600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	restricted := managedSQL(t, credentials.WorkspaceDSN)
	// Formal grant application must preserve the narrow managed-install lookup
	// while leaving its token-bearing invitation rows owner-only.
	var managedID sql.NullString
	if err := restricted.QueryRowContext(ctx, `SELECT public.xinsight_managed_work_install_id('unknown')`).Scan(&managedID); err != nil || managedID.Valid {
		t.Fatalf("workspace managed-install lookup boundary: id=%v err=%v", managedID, err)
	}
	for _, roleDSN := range []string{credentials.ReaderDSN, credentials.WorkspaceDSN, credentials.ImporterDSN} {
		roleDB := managedSQL(t, roleDSN)
		var count int
		if err := roleDB.QueryRowContext(ctx, `SELECT count(*) FROM work_event_install_invitations`).Scan(&count); err == nil {
			t.Fatal("restricted role can read managed-install invitations")
		}
	}
	reporterID, targetID, collectionID := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"
	for _, source := range []struct{ id, kind string }{{reporterID, "agent_runtime"}, {targetID, "unirouter"}} {
		if _, err := admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name)
 VALUES($1::uuid,$2,$3,$4)`, source.id, source.kind+":"+source.id, source.kind, source.kind); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := db.OpenSourceFactsStore(credentials.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	scope := workevent.Scope{SourceSystemID: reporterID, SourceInstance: "agent-a", OrgID: "7", ProjectID: "9"}
	if _, err = owner.RegisterWorkEventCollection(ctx, db.WorkEventCollection{ID: collectionID, Scope: scope, Profile: workevent.CollectionProfile, ScopeGeneration: "1", Status: "staged", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	active, err := owner.ActivateWorkEventCollection(ctx, collectionID, 1)
	if err != nil {
		t.Fatal(err)
	}
	target := workevent.Scope{SourceSystemID: targetID, SourceInstance: "ur-a", OrgID: "7", ProjectID: "86"}
	if _, err = admin.ExecContext(ctx, `INSERT INTO source_fact_streams(source_system_id,source_instance,org_id,project_id,profile,scope_generation,status)
 VALUES($1::uuid,$2,$3,$4,'unirouter.execution-context.v1','1','active')`, target.SourceSystemID, target.SourceInstance, target.OrgID, target.ProjectID); err != nil {
		t.Fatal(err)
	}
	if err = owner.RegisterWorkEventURTarget(ctx, collectionID, target, "Production UR"); err != nil {
		t.Fatal(err)
	}
	issuer, err := db.OpenWorkspaceStore(ctx, credentials.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer issuer.Close()
	now := time.Now().UTC().Truncate(time.Microsecond)
	producerWorkspace := t.TempDir()
	producerProjectKey := "agent:main"
	credential, err := issuer.CreateWorkEventIngestIdentity(ctx, workspace.Actor{Kind: "root", Ref: "root"}, db.CreateWorkEventIngestIdentityInput{
		DisplayName: "Synthetic agent", ExpiresAt: now.Add(time.Hour),
		Bindings: []db.WorkEventIngestBindingInput{{CollectionID: collectionID, ProducerProjectKey: producerProjectKey, ProjectLabel: "Research project"}},
	}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := workspace.ServiceTokenDigest(credential.Token)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := db.OpenWorkspaceStore(ctx, credentials.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	config, err := writer.ListWorkEventIngestConfig(ctx, digest, now)
	if err != nil || len(config.Items) != 1 || config.Items[0].Reporter != scope ||
		config.Items[0].ProducerProjectKey != producerProjectKey ||
		config.Items[0].CollectionRevision != active.Revision || config.Items[0].URTarget == nil ||
		config.Items[0].URTarget.Scope != target {
		t.Fatalf("restricted config: %+v %v", config, err)
	}
	event := workevent.Event{SchemaVersion: workevent.EventVersion, Reporter: scope, EventRef: "model-a", Revision: 1,
		OccurredAt: now, RecordedAt: now, SubjectKind: "model_call", LifecycleState: "started", RunID: "run-a", ModelCallID: "run-a:model:1",
		Actor: workevent.Actor{Kind: "agent", Ref: "agent-a"}, ActionKind: "model.requested", ContentState: "not_captured", ContentRefs: []workevent.ContentRef{}}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	value, err := connector.DecodeStrictJSONObject(encoded)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := connector.CanonicalizeJSONValue(value)
	if err != nil {
		t.Fatal(err)
	}
	item := db.WorkEventIngestItem{CollectionID: collectionID, ExpectedCollectionRevision: active.Revision,
		ScopeGeneration: active.ScopeGeneration, Kind: "event", Document: raw}
	receipt, err := writer.CommitAuthorizedWorkEventItem(ctx, digest, item, now)
	if err != nil || receipt.Outcome != "accepted" || receipt.Reporter != scope {
		t.Fatalf("admit: %+v %v", receipt, err)
	}
	receipt, err = writer.CommitAuthorizedWorkEventItem(ctx, digest, item, now)
	if err != nil || receipt.Outcome != "identical_replay" {
		t.Fatalf("replay: %+v %v", receipt, err)
	}
	predecessor := event
	predecessor.EventRef = "late-predecessor"
	predecessorRawJSON, marshalErr := json.Marshal(predecessor)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	predecessorValue, decodeErr := connector.DecodeStrictJSONObject(predecessorRawJSON)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	predecessorRaw, canonicalErr := connector.CanonicalizeJSONValue(predecessorValue)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	predecessorDigest := sha256.Sum256(predecessorRaw)
	late := predecessor
	late.Revision, late.PreviousHash = 2, "sha256:"+hex.EncodeToString(predecessorDigest[:])
	late.LifecycleState, late.ActionKind = "completed", "model.completed"
	lateJSON, marshalErr := json.Marshal(late)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	lateValue, decodeErr := connector.DecodeStrictJSONObject(lateJSON)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	lateRaw, canonicalErr := connector.CanonicalizeJSONValue(lateValue)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	lateItem := item
	lateItem.Document = lateRaw
	if _, err = writer.CommitAuthorizedWorkEventItem(ctx, digest, lateItem, now); !errors.Is(err, workevent.ErrUnavailable) {
		t.Fatalf("missing predecessor must remain retryable: %v", err)
	}
	firstItem := item
	firstItem.Document = predecessorRaw
	if _, err = writer.CommitAuthorizedWorkEventItem(ctx, digest, firstItem, now); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.CommitAuthorizedWorkEventItem(ctx, digest, lateItem, now); err != nil {
		t.Fatal(err)
	}
	relation := workevent.RelationAssertion{SchemaVersion: workevent.RelationVersion, Reporter: scope, RelationRef: "relation-a",
		EventRef: event.EventRef, EventRevision: event.Revision, EventHash: receipt.SHA256, URTarget: target,
		URRequestID: "op_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ModelCallID: event.ModelCallID,
		EvidenceRef: "header-a", EvidenceHash: "sha256:" + hex.EncodeToString(predecessorDigest[:]), RecordedAt: now}
	relationJSON, marshalErr := json.Marshal(relation)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	relationValue, decodeErr := connector.DecodeStrictJSONObject(relationJSON)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	relationRaw, canonicalErr := connector.CanonicalizeJSONValue(relationValue)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	relationItem := item
	relationItem.Kind, relationItem.Document = "relation", relationRaw
	if result, submitErr := writer.CommitAuthorizedWorkEventItem(ctx, digest, relationItem, now); submitErr != nil || result.Outcome != "accepted" {
		t.Fatalf("paired relation: %+v %v", result, submitErr)
	}
	wrongTarget := relation
	wrongTarget.RelationRef, wrongTarget.URTarget.ProjectID = "relation-wrong", "9"
	wrongJSON, marshalErr := json.Marshal(wrongTarget)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	wrongValue, decodeErr := connector.DecodeStrictJSONObject(wrongJSON)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	wrongRaw, canonicalErr := connector.CanonicalizeJSONValue(wrongValue)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	relationItem.Document = wrongRaw
	if _, err = writer.CommitAuthorizedWorkEventItem(ctx, digest, relationItem, now); !errors.Is(err, workevent.ErrScopeChanged) {
		t.Fatalf("unpaired UR target admitted: %v", err)
	}
	stale := item
	stale.ExpectedCollectionRevision--
	if _, err = writer.CommitAuthorizedWorkEventItem(ctx, digest, stale, now); !errors.Is(err, workevent.ErrScopeChanged) {
		t.Fatalf("stale revision: %v", err)
	}
	var user string
	if err = restricted.QueryRowContext(ctx, `SELECT current_user`).Scan(&user); err != nil || user != "xinsight_workspace" {
		t.Fatalf("wrong role %q: %v", user, err)
	}
	tx, err := restricted.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`SELECT revision FROM work_event_ingest_bindings WHERE service_identity_id='` + credential.ServiceIdentity.ID + `' FOR SHARE`,
		`SELECT revision FROM work_event_collections WHERE id='` + collectionID + `'::uuid FOR SHARE`,
		`SELECT status FROM source_systems WHERE id='` + reporterID + `'::uuid FOR SHARE`,
	} {
		var value any
		if err = tx.QueryRowContext(ctx, query).Scan(&value); err != nil {
			t.Fatal("workspace row lock", err)
		}
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`UPDATE work_event_ingest_bindings SET revision=revision WHERE service_identity_id='` + credential.ServiceIdentity.ID + `'`,
		`UPDATE work_event_collections SET revision=revision WHERE id='` + collectionID + `'::uuid`,
		`UPDATE source_systems SET status=status WHERE id='` + reporterID + `'::uuid`,
	} {
		if _, err = restricted.ExecContext(ctx, query); err == nil {
			t.Fatal("workspace mutated authority", query)
		}
	}
	var eventInsert, sourceFactInsert, bindingInsert bool
	if err = restricted.QueryRowContext(ctx, `SELECT has_table_privilege(current_user,'work_event_documents','INSERT'),
 has_table_privilege(current_user,'source_fact_documents','INSERT'),
 has_table_privilege(current_user,'work_event_ingest_bindings','INSERT')`).Scan(&eventInsert, &sourceFactInsert, &bindingInsert); err != nil {
		t.Fatal(err)
	}
	if !eventInsert || sourceFactInsert || bindingInsert {
		t.Fatalf("role widened: event=%t source=%t binding=%t", eventInsert, sourceFactInsert, bindingInsert)
	}
	serverNow := now
	keys := reviewbody.KeyRing{ActiveID: "test-key", Keys: map[string][]byte{"test-key": bytes.Repeat([]byte{7}, 32)}}
	server := httptest.NewTLSServer(api.NewHandlerWithDependencies(api.HandlerDependencies{
		WorkspaceStore: writer, AgentAccessStore: writer, ReviewBodyKeys: keys, Now: func() time.Time { return serverNow },
	}))
	defer server.Close()
	call := func(method, path, token string, body []byte) int {
		t.Helper()
		request, requestErr := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, callErr := server.Client().Do(request)
		if callErr != nil {
			t.Fatal(callErr)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if status := call(http.MethodGet, "/api/v1/work-event-ingest/config", credential.Token, nil); status != http.StatusOK {
		t.Fatalf("ingest config status=%d", status)
	}
	second := event
	second.EventRef = "model-b"
	encoded, err = json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	value, err = connector.DecodeStrictJSONObject(encoded)
	if err != nil {
		t.Fatal(err)
	}
	secondRaw, err := connector.CanonicalizeJSONValue(value)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := json.Marshal(map[string]any{
		"collection_id": collectionID, "expected_collection_revision": active.Revision,
		"scope_generation": active.ScopeGeneration, "kind": "event", "document": json.RawMessage(secondRaw),
	})
	if err != nil {
		t.Fatal(err)
	}
	if status := call(http.MethodPost, "/api/v1/work-event-ingest/items", credential.Token, envelope); status != http.StatusCreated {
		t.Fatalf("ingest POST status=%d", status)
	}
	if status := call(http.MethodPost, "/api/v1/work-event-ingest/items", credential.Token, envelope); status != http.StatusOK {
		t.Fatalf("ingest replay status=%d", status)
	}
	if status := call(http.MethodGet, "/api/v1/executions", credential.Token, nil); status != http.StatusForbidden {
		t.Fatalf("ingest token read evidence status=%d", status)
	}
	readCredential, err := issuer.CreateServiceIdentity(ctx, workspace.Actor{Kind: "root", Ref: "root"}, workspace.CreateServiceIdentityInput{
		DisplayName: "Synthetic read", ExpiresAt: now.Add(time.Hour),
	}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status := call(http.MethodPost, "/api/v1/work-event-ingest/items", readCredential.Token, envelope); status != http.StatusForbidden {
		t.Fatalf("read token write status=%d", status)
	}
	bodyHash := sha256.Sum256([]byte{})
	bodyDigest := "sha256:" + hex.EncodeToString(bodyHash[:])
	withBody := event
	withBody.EventRef = "model-with-body"
	withBody.ContentState = "referenced"
	withBody.ContentRefs = []workevent.ContentRef{
		{Kind: "model_input", Ref: "ocbody:empty", SHA256: bodyDigest},
		{Kind: "model_input", Ref: "ocbody:lease", SHA256: bodyDigest},
	}
	bodyJSON, err := json.Marshal(withBody)
	if err != nil {
		t.Fatal(err)
	}
	bodyValue, err := connector.DecodeStrictJSONObject(bodyJSON)
	if err != nil {
		t.Fatal(err)
	}
	bodyEvent, err := connector.CanonicalizeJSONValue(bodyValue)
	if err != nil {
		t.Fatal(err)
	}
	bodyItem := item
	bodyItem.Document = bodyEvent
	bodyEventReceipt, err := writer.CommitAuthorizedWorkEventItem(ctx, digest, bodyItem, now)
	if err != nil {
		t.Fatal(err)
	}
	bodyInput := db.WorkEventReviewBodyInput{CollectionID: collectionID, ExpectedCollectionRevision: active.Revision,
		ScopeGeneration: active.ScopeGeneration, EventRef: withBody.EventRef, EventRevision: 1, EventHash: bodyEventReceipt.SHA256,
		ContentKind: "model_input", ContentRef: "ocbody:empty", ByteDomain: "openclaw.llm_input.utf8.v1", SHA256: bodyDigest, Body: []byte{}}
	if _, err := writer.CommitAuthorizedWorkEventReviewBody(ctx, digest, bodyInput, keys, now); !errors.Is(err, reviewbody.ErrScope) {
		t.Fatalf("old metadata-only ingest binding gained body authority: %v", err)
	}
	settings := db.ReviewCaptureSettings{Mode: "full_content", RetentionSeconds: 3600,
		ProjectQuotaBytes: 1024, MaxItemBytes: 512}
	if os.Getenv("XINSIGHT_OPENCLAW_GATEWAY_TEST") == "1" {
		// The native model input includes real built-in tool declarations. Use
		// the product defaults instead of the tiny zero-byte unit fixture.
		settings.ProjectQuotaBytes, settings.MaxItemBytes = 5<<30, 32<<20
	}
	revision, err := owner.RequestReviewCaptureDefault(ctx, settings, 0, "xs_admin", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.ConfirmReviewCaptureDefault(ctx, revision, "xs_admin", now); err != nil {
		t.Fatal(err)
	}
	if err := owner.GrantReviewBodyIngestBinding(ctx, credential.ServiceIdentity.ID, collectionID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.CommitAuthorizedWorkEventReviewBody(ctx, digest, bodyInput, keys, now); !errors.Is(err, reviewbody.ErrProjectBindingPending) {
		t.Fatalf("unbound XI project admitted a new review copy: %v", err)
	}
	if err := owner.BindReviewCaptureProject(ctx, reporterID, scope.SourceInstance, scope.OrgID, scope.ProjectID,
		"44444444-4444-4444-8444-444444444444", "Synthetic XI project", "xs_admin", now); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("XINSIGHT_OPENCLAW_GATEWAY_TEST") == "1" {
		gatewayServer := httptest.NewTLSServer(api.NewHandlerWithDependencies(api.HandlerDependencies{
			WorkspaceStore: writer, AgentAccessStore: writer, ReviewBodyKeys: keys, Now: time.Now,
		}))
		defer gatewayServer.Close()
		runSyntheticOpenClawGateway(t, gatewayServer, credential.Token, producerWorkspace, admin)
	}
	var gatewayBodies, gatewayStored int64
	if err := admin.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(charged_bytes),0) FROM review_body_operations
 WHERE source_kind='work_event' AND state='accepted'`).Scan(&gatewayBodies, &gatewayStored); err != nil {
		t.Fatal(err)
	}
	bodyReceipt, err := writer.CommitAuthorizedWorkEventReviewBody(ctx, digest, bodyInput, keys, now)
	if err != nil || bodyReceipt.Outcome != "accepted" || bodyReceipt.PlainBytes != 0 {
		t.Fatalf("zero-byte body custody: %+v %v", bodyReceipt, err)
	}
	replayedBody, err := writer.CommitAuthorizedWorkEventReviewBody(ctx, digest, bodyInput, keys, now.Add(5*time.Minute))
	if err != nil || replayedBody.Outcome != "identical_replay" || !replayedBody.ExpiresAt.Equal(bodyReceipt.ExpiresAt) {
		t.Fatalf("body replay changed deadline: %+v %v", replayedBody, err)
	}
	var acceptedAt, expiresAt time.Time
	if err := admin.QueryRowContext(ctx, `SELECT accepted_at,expires_at FROM review_body_operations WHERE content_ref=$1`,
		bodyInput.ContentRef).Scan(&acceptedAt, &expiresAt); err != nil || !acceptedAt.Equal(now.UTC().Truncate(time.Microsecond)) || !expiresAt.Equal(bodyReceipt.ExpiresAt) {
		t.Fatalf("body replay changed stored admission: %s %s %v", acceptedAt, expiresAt, err)
	}
	leaseInput := bodyInput
	leaseInput.ContentRef = "ocbody:lease"
	missingKey := reviewbody.KeyRing{ActiveID: "missing", Keys: map[string][]byte{}}
	if _, err := writer.CommitAuthorizedWorkEventReviewBody(ctx, digest, leaseInput, missingKey, now); !errors.Is(err, reviewbody.ErrKeyUnavailable) {
		t.Fatalf("reservation fixture did not stop before publish: %v", err)
	}
	if _, err := writer.CommitAuthorizedWorkEventReviewBody(ctx, digest, leaseInput, keys, now.Add(11*time.Minute)); !errors.Is(err, reviewbody.ErrExpired) {
		t.Fatalf("expired reservation lease restarted: %v", err)
	}
	var leaseAcceptedAt, leaseExpiresAt time.Time
	var leaseState string
	if err := admin.QueryRowContext(ctx, `SELECT accepted_at,expires_at,state FROM review_body_operations WHERE content_ref=$1`,
		leaseInput.ContentRef).Scan(&leaseAcceptedAt, &leaseExpiresAt, &leaseState); err != nil || leaseState != "reserved" ||
		!leaseAcceptedAt.Equal(now.UTC().Truncate(time.Microsecond)) || !leaseExpiresAt.Equal(bodyReceipt.ExpiresAt) {
		t.Fatalf("expired lease mutated reservation: %s %s %s %v", leaseAcceptedAt, leaseExpiresAt, leaseState, err)
	}
	var stored, reserved int64
	if err := admin.QueryRowContext(ctx, `SELECT stored_bytes,reserved_bytes FROM review_body_project_usage WHERE quota_scope=(SELECT quota_scope FROM review_body_operations WHERE content_ref=$1)`,
		bodyInput.ContentRef).Scan(&stored, &reserved); err != nil || stored != gatewayStored+16 || reserved != 16 {
		t.Fatalf("project body ledger: %d %d %v", stored, reserved, err)
	}
	reviewer := "xs_a1a1a1a1a1a1a1a1"
	if _, err := writer.ReadReviewBody(ctx, reviewer, bodyInput.ContentRef, keys, now); !errors.Is(err, workbench.ErrNotFound) {
		t.Fatalf("ungranted human body read: %v", err)
	}
	if err := writer.AuditReviewBodyRead(ctx, reviewer, bodyInput.ContentRef, "denied", now); err != nil {
		t.Fatalf("denied human body read audit: %v", err)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,
 allow_content,allowed_content_purposes) VALUES($1,$2::uuid,$3,true,'["human_review"]'::jsonb)`,
		reviewer, reporterID, "project-"+scope.ProjectID); err != nil {
		t.Fatal(err)
	}
	readBody, err := writer.ReadReviewBody(ctx, reviewer, bodyInput.ContentRef, keys, now)
	if err != nil || len(readBody.Bytes) != 0 || readBody.SHA256 != bodyDigest {
		t.Fatalf("authorized zero-byte human read: %+v %v", readBody, err)
	}
	if err := writer.AuditReviewBodyRead(ctx, reviewer, bodyInput.ContentRef, "accepted", now); err != nil {
		t.Fatalf("accepted human body read audit: %v", err)
	}
	var deniedAudits, acceptedAudits int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE result='denied'),count(*) FILTER (WHERE result='accepted')
 FROM access_audit_records WHERE action='review_body.read' AND actor_ref=$1 AND resource_ref=$2`,
		reviewer, bodyInput.ContentRef).Scan(&deniedAudits, &acceptedAudits); err != nil || deniedAudits != 1 || acceptedAudits != 1 {
		t.Fatalf("human body read audit: denied=%d accepted=%d err=%v", deniedAudits, acceptedAudits, err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE workbench_access_grants SET active=false,revision=revision+1
 WHERE subject_id=$1 AND source_system_id=$2::uuid`, reviewer, reporterID); err != nil {
		t.Fatal(err)
	}
	if err := writer.ReauthorizeReviewBodyRead(ctx, reviewer, bodyInput.ContentRef, now); !errors.Is(err, workbench.ErrNotFound) {
		t.Fatalf("revoked human stream remained authorized: %v", err)
	}
	if swept, err := owner.ExpireReviewBodies(ctx, now.Add(11*time.Minute), 10); err != nil || swept != 1 {
		t.Fatalf("reservation sweep: %d %v", swept, err)
	}
	var latestBodyExpiry time.Time
	if err := admin.QueryRowContext(ctx, `SELECT max(expires_at) FROM review_body_operations WHERE state='accepted'`).Scan(&latestBodyExpiry); err != nil {
		t.Fatal(err)
	}
	if swept, err := owner.ExpireReviewBodies(ctx, latestBodyExpiry.Add(time.Second), int(1+gatewayBodies)); err != nil || int64(swept) != 1+gatewayBodies {
		t.Fatalf("accepted body sweep: %d %v", swept, err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT stored_bytes,reserved_bytes FROM review_body_project_usage WHERE quota_scope=(SELECT quota_scope FROM review_body_operations WHERE content_ref=$1)`,
		bodyInput.ContentRef).Scan(&stored, &reserved); err != nil || stored != 0 || reserved != 0 {
		t.Fatalf("body ledger after expiry: %d %d %v", stored, reserved, err)
	}
	limited := 0
	for i := 0; i < 25; i++ {
		if status := call(http.MethodGet, "/api/v1/work-event-ingest/config", credential.Token, nil); status == http.StatusTooManyRequests {
			limited++
		} else if status != http.StatusOK {
			t.Fatalf("ingest rate response=%d", status)
		}
	}
	if limited == 0 {
		t.Fatal("ingest limiter never bounded the single-item identity")
	}
	serverNow = serverNow.Add(time.Second)
	if status := call(http.MethodGet, "/api/v1/work-event-ingest/config", credential.Token, nil); status != http.StatusOK {
		t.Fatalf("ingest limiter did not recover: %d", status)
	}
	paused, err := owner.ControlWorkEventCollection(ctx, collectionID, "paused", active.Revision)
	if err != nil || paused.Status != "paused" {
		t.Fatalf("pause: %+v %v", paused, err)
	}
	if _, err = writer.CommitAuthorizedWorkEventItem(ctx, digest, item, now); !errors.Is(err, workevent.ErrScopeChanged) {
		t.Fatalf("paused collection admitted: %v", err)
	}
	config, err = writer.ListWorkEventIngestConfig(ctx, digest, now)
	if err != nil || len(config.Items) != 0 {
		t.Fatalf("paused config: %+v %v", config, err)
	}
	if _, err = issuer.RevokeServiceIdentity(ctx, credential.ServiceIdentity.ID, workspace.Actor{Kind: "root", Ref: "root"}, "synthetic test", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.CommitAuthorizedWorkEventItem(ctx, digest, item, now.Add(2*time.Second)); !errors.Is(err, workspace.ErrCredentialRevoked) {
		t.Fatalf("revoked token admitted: %v", err)
	}
}
