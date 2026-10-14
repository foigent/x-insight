package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workevent"
	"github.com/foigent/x-insight/internal/workspace"
)

// seedP2BrowserActivity keeps browser qualification on the production handler
// and an isolated database. Its content is synthetic, independently accepted
// into XI custody, and never obtained from a real user or provider.
func seedP2BrowserActivity(t *testing.T, admin *sql.DB, ownerDSN string,
	workspaceStore *db.WorkspaceStore) reviewbody.KeyRing {
	t.Helper()
	if os.Getenv("XINSIGHT_A0_RUNTIME_ROLES") != "1" {
		t.Fatal("P2 browser requires XINSIGHT_A0_RUNTIME_ROLES=1 to qualify production database roles")
	}
	ctx := t.Context()
	owner, err := db.OpenSourceFactsStore(ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	now := time.Now().UTC().Truncate(time.Microsecond)
	sourceID, collectionID, projectID := randomTestUUID(t), randomTestUUID(t), randomTestUUID(t)
	if _, err = admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name)
 VALUES($1::uuid,$2,'agent_runtime','P2 synthetic OpenClaw')`, sourceID, "agent-runtime:p2-browser:"+sourceID); err != nil {
		t.Fatal(err)
	}
	scope := workevent.Scope{SourceSystemID: sourceID, SourceInstance: "openclaw-browser", OrgID: "7", ProjectID: "9"}
	collection, err := owner.RegisterWorkEventCollection(ctx, db.WorkEventCollection{ID: collectionID,
		Scope: scope, Profile: workevent.CollectionProfile, ScopeGeneration: "1", Status: "staged", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	collection, err = owner.ActivateWorkEventCollection(ctx, collection.ID, collection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	// A second registered project remains unbound for administrator browser
	// qualification of discovery, explicit binding, and readback.
	futureScope := scope
	futureScope.ProjectID = "10"
	future, err := owner.RegisterWorkEventCollection(ctx, db.WorkEventCollection{ID: randomTestUUID(t),
		Scope: futureScope, Profile: workevent.CollectionProfile, ScopeGeneration: "1", Status: "staged", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.ActivateWorkEventCollection(ctx, future.ID, future.Revision); err != nil {
		t.Fatal(err)
	}
	if err = owner.BindReviewCaptureProject(ctx, sourceID, scope.SourceInstance, scope.OrgID,
		scope.ProjectID, projectID, "P2 browser project", "xs_1111111111111111", now); err != nil {
		t.Fatal(err)
	}
	revision, err := owner.RequestReviewCaptureDefault(ctx, db.ReviewCaptureSettings{
		Mode: "full_content", RetentionSeconds: 30 * 86400, ProjectQuotaBytes: 5 << 30, MaxItemBytes: 32 << 20,
	}, 0, "xs_1111111111111111", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.ConfirmReviewCaptureDefault(ctx, revision, "xs_1111111111111111", now); err != nil {
		t.Fatal(err)
	}
	adminWorkspace, err := db.OpenWorkspaceStore(ctx, ownerDSN)
	if err != nil {
		t.Fatalf("P2 fixture owner workspace: %v", err)
	}
	defer adminWorkspace.Close()
	credential, err := adminWorkspace.CreateWorkEventIngestIdentity(ctx, workspace.Actor{Kind: "root", Ref: "root"},
		db.CreateWorkEventIngestIdentityInput{DisplayName: "P2 browser OpenClaw", ExpiresAt: now.Add(time.Hour),
			Bindings: []db.WorkEventIngestBindingInput{{CollectionID: collectionID, ProducerProjectKey: "agent:main", ProjectLabel: "P2 browser project"}}}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.GrantReviewBodyIngestBinding(ctx, credential.ServiceIdentity.ID, collectionID, now); err != nil {
		t.Fatal(err)
	}
	digest, err := workspace.ServiceTokenDigest(credential.Token)
	if err != nil {
		t.Fatal(err)
	}
	keys := reviewbody.KeyRing{ActiveID: "p2-browser", Keys: map[string][]byte{"p2-browser": bytes.Repeat([]byte{7}, 32)}}
	for index, action := range []string{"tool.completed", "resource.read", "resource.edit", "resource.write", "resource.read.failed"} {
		observedAt := now.Add(-time.Minute + time.Duration(index)*time.Second)
		body := []byte(`{"result":"Synthetic browser content: read, edit, write, and failed read."}`)
		bodyHash := sha256.Sum256(body)
		contentRef := "ocbody:p2-browser-" + string(rune('1'+index))
		subjectKind, lifecycle := "resource_action", "completed"
		if index == 0 {
			subjectKind = "tool_call"
		}
		if action == "resource.read.failed" {
			lifecycle = "failed"
		}
		event := workevent.Event{SchemaVersion: workevent.EventVersion, Reporter: scope,
			EventRef: "oc:p2-browser-" + string(rune('1'+index)), Revision: 1,
			OccurredAt: observedAt, RecordedAt: observedAt,
			SubjectKind: subjectKind, LifecycleState: lifecycle, RunID: "run-p2-browser", ToolCallID: "tool-p2-browser-" + string(rune('1'+index)),
			Actor: workevent.Actor{Kind: "service", Ref: "openclaw"}, ActionKind: action,
			Resource:     &workevent.Resource{Ref: "file:p2-browser", NameState: "readable", Name: "report.txt"},
			ContentState: "not_captured", ContentRefs: []workevent.ContentRef{}}
		if index == 0 {
			event.ContentState = "referenced"
			event.ContentRefs = []workevent.ContentRef{{Kind: "tool_output", Ref: contentRef,
				SHA256: "sha256:" + hex.EncodeToString(bodyHash[:])}}
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		value, err := connector.DecodeStrictJSONObject(encoded)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := connector.CanonicalizeJSONValue(value)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := workspaceStore.CommitAuthorizedWorkEventItem(ctx, digest, db.WorkEventIngestItem{
			CollectionID: collectionID, ExpectedCollectionRevision: collection.Revision,
			ScopeGeneration: collection.ScopeGeneration, Kind: "event", Document: canonical}, now)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			_, err = workspaceStore.CommitAuthorizedWorkEventReviewBody(ctx, digest, db.WorkEventReviewBodyInput{
				CollectionID: collectionID, ExpectedCollectionRevision: collection.Revision, ScopeGeneration: collection.ScopeGeneration,
				EventRef: event.EventRef, EventRevision: 1, EventHash: receipt.SHA256,
				ContentKind: "tool_output", ContentRef: contentRef, ByteDomain: "openclaw.after_tool_call.canonical-json.v1",
				SHA256: event.ContentRefs[0].SHA256, Body: body}, keys, now)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	// A separate synthetic completed task lets the browser inspect a run with
	// four source-reported reads and no writes. Tool output bodies are retained
	// through the existing XI review path; they are not verified file bytes.
	for index, read := range []struct{ action, lifecycle, name string }{
		{"resource.read", "completed", "briefing.md"},
		{"resource.read", "completed", "notes.md"},
		{"resource.read.not_found", "failed", "optional-context.md"},
		{"resource.read.failed", "failed", "restricted.txt"},
	} {
		observedAt := now.Add(-45*time.Second + time.Duration(index)*2*time.Second)
		toolID := "tool-p2-completed-" + string(rune('1'+index))
		if index < 2 {
			body := []byte(`{"content":"Synthetic tool response for ` + read.name + `"}`)
			bodyHash := sha256.Sum256(body)
			contentRef := "ocbody:p2-completed-" + string(rune('1'+index))
			tool := workevent.Event{SchemaVersion: workevent.EventVersion, Reporter: scope,
				EventRef: "oc:p2-completed-tool-" + string(rune('1'+index)), Revision: 1,
				OccurredAt: observedAt, RecordedAt: observedAt, SubjectKind: "tool_call", LifecycleState: "completed",
				RunID: "run-p2-completed", ToolCallID: toolID, Actor: workevent.Actor{Kind: "service", Ref: "openclaw"},
				ActionKind: "tool.completed", Resource: &workevent.Resource{Ref: "tool:read-file", NameState: "readable", Name: "read_file"},
				ContentState: "referenced", ContentRefs: []workevent.ContentRef{{Kind: "tool_output", Ref: contentRef,
					SHA256: "sha256:" + hex.EncodeToString(bodyHash[:])}}}
			receipt, commitErr := workspaceStore.CommitAuthorizedWorkEventItem(ctx, digest, db.WorkEventIngestItem{
				CollectionID: collectionID, ExpectedCollectionRevision: collection.Revision,
				ScopeGeneration: collection.ScopeGeneration, Kind: "event", Document: p2BrowserCanonical(t, tool)}, now)
			if commitErr != nil {
				t.Fatal(commitErr)
			}
			if _, commitErr = workspaceStore.CommitAuthorizedWorkEventReviewBody(ctx, digest, db.WorkEventReviewBodyInput{
				CollectionID: collectionID, ExpectedCollectionRevision: collection.Revision, ScopeGeneration: collection.ScopeGeneration,
				EventRef: tool.EventRef, EventRevision: 1, EventHash: receipt.SHA256,
				ContentKind: "tool_output", ContentRef: contentRef, ByteDomain: "openclaw.after_tool_call.canonical-json.v1",
				SHA256: tool.ContentRefs[0].SHA256, Body: body}, keys, now); commitErr != nil {
				t.Fatal(commitErr)
			}
		}
		resource := workevent.Event{SchemaVersion: workevent.EventVersion, Reporter: scope,
			EventRef: "oc:p2-completed-read-" + string(rune('1'+index)), Revision: 1,
			OccurredAt: observedAt.Add(time.Second), RecordedAt: observedAt.Add(time.Second),
			SubjectKind: "resource_action", LifecycleState: read.lifecycle, RunID: "run-p2-completed", ToolCallID: toolID,
			Actor: workevent.Actor{Kind: "service", Ref: "openclaw"}, ActionKind: read.action,
			Resource:     &workevent.Resource{Ref: "file:p2-completed-" + string(rune('1'+index)), NameState: "readable", Name: read.name},
			ContentState: "not_captured", ContentRefs: []workevent.ContentRef{}}
		if _, commitErr := workspaceStore.CommitAuthorizedWorkEventItem(ctx, digest, db.WorkEventIngestItem{
			CollectionID: collectionID, ExpectedCollectionRevision: collection.Revision,
			ScopeGeneration: collection.ScopeGeneration, Kind: "event", Document: p2BrowserCanonical(t, resource)}, now); commitErr != nil {
			t.Fatal(commitErr)
		}
	}
	finishedAt := now.Add(-35 * time.Second)
	completed := workevent.Event{SchemaVersion: workevent.EventVersion, Reporter: scope,
		EventRef: "oc:p2-completed-task", Revision: 1, OccurredAt: finishedAt, RecordedAt: finishedAt,
		SubjectKind: "task", LifecycleState: "completed", TaskRef: "task-p2-completed", RunID: "run-p2-completed",
		Actor: workevent.Actor{Kind: "service", Ref: "openclaw"}, ActionKind: "task.completed",
		ContentState: "not_captured", ContentRefs: []workevent.ContentRef{}}
	if _, err = workspaceStore.CommitAuthorizedWorkEventItem(ctx, digest, db.WorkEventIngestItem{
		CollectionID: collectionID, ExpectedCollectionRevision: collection.Revision,
		ScopeGeneration: collection.ScopeGeneration, Kind: "event", Document: p2BrowserCanonical(t, completed)}, now); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.ExecContext(context.Background(), `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,
 allow_resource,allow_content,allowed_content_purposes) VALUES('xs_8899aabbccddeeff',$1::uuid,'project-9',true,true,'["human_review"]')`, sourceID); err != nil {
		t.Fatal(err)
	}

	// The browser must follow a real stored B observation, rather than a mocked
	// runtime response or a relation assertion without a matching UR identifier.
	target, requestID, evidenceHash := seedP2BrowserURObservation(t, admin, owner, ownerDSN, now.Add(-57*time.Second))
	if err = owner.RegisterWorkEventURTarget(ctx, collectionID, target, "P2 synthetic UR"); err != nil {
		t.Fatalf("P2 fixture exact UR target: %v", err)
	}
	event := workevent.Event{SchemaVersion: workevent.EventVersion, Reporter: scope,
		EventRef: "oc:p2-browser-model", Revision: 1, OccurredAt: now.Add(-57 * time.Second), RecordedAt: now.Add(-57 * time.Second),
		SubjectKind: "model_call", LifecycleState: "started", RunID: "run-p2-browser", ModelCallID: "model-p2-browser",
		Actor: workevent.Actor{Kind: "service", Ref: "openclaw"}, ActionKind: "model.started",
		ContentState: "not_captured", ContentRefs: []workevent.ContentRef{}}
	receipt, err := workspaceStore.CommitAuthorizedWorkEventItem(ctx, digest, db.WorkEventIngestItem{
		CollectionID: collectionID, ExpectedCollectionRevision: collection.Revision, ScopeGeneration: collection.ScopeGeneration,
		Kind: "event", Document: p2BrowserCanonical(t, event)}, now)
	if err != nil {
		t.Fatal(err)
	}
	relation := workevent.RelationAssertion{SchemaVersion: workevent.RelationVersion, Reporter: scope,
		RelationRef: "relation-p2-browser-model", EventRef: event.EventRef, EventRevision: 1, EventHash: receipt.SHA256,
		URTarget: target, URRequestID: requestID, ModelCallID: event.ModelCallID,
		EvidenceRef: "synthetic-response-header", EvidenceHash: evidenceHash, RecordedAt: now}
	if _, err = workspaceStore.CommitAuthorizedWorkEventItem(ctx, digest, db.WorkEventIngestItem{
		CollectionID: collectionID, ExpectedCollectionRevision: collection.Revision, ScopeGeneration: collection.ScopeGeneration,
		Kind: "relation", Document: p2BrowserCanonical(t, relation)}, now); err != nil {
		t.Fatalf("P2 fixture model relation: %v", err)
	}
	return keys
}

func p2BrowserCanonical(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	object, err := connector.DecodeStrictJSONObject(encoded)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := connector.CanonicalizeJSONValue(object)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Copy two schema-validated golden shapes into a fresh, explicitly synthetic
// scope and recompute their identity. The retained contract vectors stay intact.
func seedP2BrowserURObservation(t *testing.T, admin *sql.DB, owner *db.SourceFactsStore, ownerDSN string, observedAt time.Time) (workevent.Scope, string, string) {
	t.Helper()
	ctx := t.Context()
	target := workevent.Scope{SourceSystemID: randomTestUUID(t), SourceInstance: "ur-p2-browser", OrgID: "7", ProjectID: "9"}
	requestID := "op_" + strings.Repeat("b", 32)
	if _, err := admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name)
 VALUES($1::uuid,$2,'unirouter','P2 synthetic UR')`, target.SourceSystemID, "unirouter:p2-browser:"+target.SourceSystemID); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	u.User = workbenchRuntimeRoleUser(t, "importer")
	importer, err := db.OpenSourceFactsStore(u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer importer.Close()
	raw, err := os.ReadFile("../sourcefacts/contract/observations/golden-observations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Valid []struct {
			Canonical string `json:"canonical_utf8"`
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	matched := 0
	for _, vector := range fixture.Valid {
		d, err := sourcefacts.DecodeObservation([]byte(vector.Canonical))
		if err != nil {
			t.Fatal(err)
		}
		if d.Profile != sourcefacts.RuntimeProfile || d.Kind != "request_started" {
			if d.Profile != sourcefacts.CapabilityProfile || d.Kind != "selection" {
				continue
			}
		}
		d.SourceInstance, d.OrgID, d.ProjectID, d.RequestID = target.SourceInstance, target.OrgID, target.ProjectID, requestID
		if d.SubjectKind == "request" {
			d.SubjectID = requestID
		}
		d.OccurredAt, d.RecordedAt = observedAt.Format("2006-01-02T15:04:05.000000Z"), observedAt.Format("2006-01-02T15:04:05.000000Z")
		d.SourceFactID = sourcefacts.SourceID(d.SchemaVersion, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.SubjectKind, d.SubjectID, d.Kind)
		d.EventID = sourcefacts.SourceID(d.SchemaVersion+".revision", d.SourceFactID, d.Revision)
		canonical := p2BrowserCanonical(t, d)
		envelope, err := sourcefacts.ObservationEnvelope(canonical)
		if err != nil {
			t.Fatal(err)
		}
		stream, err := owner.RegisterSourceFactsStream(ctx, sourcefacts.Stream{ID: randomTestUUID(t), SourceSystemID: target.SourceSystemID,
			SourceInstance: target.SourceInstance, OrgID: target.OrgID, ProjectID: target.ProjectID, Profile: d.Profile, ScopeGeneration: "1"})
		if err != nil {
			t.Fatal(err)
		}
		stream, err = owner.ControlSourceFactsStream(ctx, stream.ID, "active", stream.Revision, false)
		if err != nil {
			t.Fatal(err)
		}
		page := sourcefacts.Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: sourcefacts.ConnectorID,
			ConnectorVersion: sourcefacts.ObservationConnectorVersion, ProducerInstance: target.SourceInstance,
			GeneratedAt: d.RecordedAt, Events: []json.RawMessage{}, NextCursor: "p2-start", HighWatermark: "p2-start"}
		if err = importer.CommitSourceFactsPage(ctx, stream, page, observedAt); err != nil {
			t.Fatalf("P2 fixture B bootstrap: %v", err)
		}
		stream, err = importer.LoadSourceFactsStream(ctx, stream.ID)
		if err != nil {
			t.Fatal(err)
		}
		page.Events, page.NextCursor, page.HighWatermark = []json.RawMessage{envelope}, "p2-complete", "p2-complete"
		if err = importer.CommitSourceFactsPage(ctx, stream, page, observedAt); err != nil {
			t.Fatalf("P2 fixture B page: %v", err)
		}
		matched++
	}
	if matched != 2 {
		t.Fatalf("expected two synthetic B shapes, got %d", matched)
	}
	if _, err = admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,
 allow_source_runtime,allow_source_capability) VALUES('xs_8899aabbccddeeff',$1::uuid,'project-9',true,true)`, target.SourceSystemID); err != nil {
		t.Fatal(err)
	}
	header := sha256.Sum256([]byte("X-UniRouter-Request-Id: " + requestID))
	return target, requestID, "sha256:" + hex.EncodeToString(header[:])
}

// Optional real UR machine fixture. Only its protected local fixture file is
// accepted; no product credential or non-loopback endpoint may be used here.
func p2BrowserPolicyManager(t *testing.T, owner *db.SourceFactsStore, admin *sql.DB) reviewpolicycontrol.Manager {
	t.Helper()
	manager := reviewpolicycontrol.Manager{Store: owner}
	path := os.Getenv("XINSIGHT_P2_BROWSER_UR_FIXTURE")
	if path == "" {
		return manager
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("UR browser fixture must be a private regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 64<<10 {
		t.Fatal("UR browser fixture cannot be read")
	}
	var input struct {
		BaseURL        string `json:"base_url"`
		TLSCAPEM       string `json:"tls_ca_pem"`
		InstallationID string `json:"installation_id"`
		SourceInstance string `json:"source_instance"`
		OrgID          string `json:"org_id"`
		ProjectID      string `json:"project_id"`
		PolicyBearer   string `json:"policy_bearer"`
	}
	if json.Unmarshal(raw, &input) != nil {
		t.Fatal("invalid UR browser fixture")
	}
	u, err := url.Parse(input.BaseURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		t.Fatal("invalid UR browser origin")
	}
	address := net.ParseIP(u.Hostname())
	if address == nil || !address.IsLoopback() {
		t.Fatal("UR browser requires loopback")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(input.TLSCAPEM)) {
		t.Fatal("invalid UR fixture CA")
	}
	identity := urmachine.Identity{InstallationID: input.InstallationID, SourceInstance: input.SourceInstance, OrgID: input.OrgID}
	client, err := urmachine.NewPolicyClient(input.BaseURL, input.PolicyBearer, identity, &http.Client{
		Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}}, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal("invalid UR browser machine identity")
	}
	serviceID, err := workspace.NewID("xsi_", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = admin.Exec(`INSERT INTO service_identities(id,display_name,status,permissions,created_by_subject,created_at,updated_at,expires_at)
 VALUES($1,'Synthetic paired UR control','active',ARRAY['connector.control.poll','connector.control.ack']::text[],'system',$2,$2,$3)`, serviceID, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sourceHash := sha256.Sum256([]byte(input.SourceInstance))
	if _, err = admin.Exec(`INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,final_event_scope,
 project_id,source_instance,source_instance_sha256,control_service_identity_id,installation_acknowledgement_sha256,
 status,desired_state,observed_state,created_at,updated_at)
 VALUES($1,'xid_AAAAAAAAAAAAAAAAAAAAAA','unirouter',ARRAY['model.request.completed']::text[],ARRAY['model.request.completed']::text[],
 $2,$3,$4,$5,$6,'connected','active','connected',$7,$7)`, input.InstallationID, input.ProjectID, input.SourceInstance,
		"sha256:"+hex.EncodeToString(sourceHash[:]), serviceID, "sha256:"+strings.Repeat("a", 64), now); err != nil {
		t.Fatal(err)
	}
	manager.URClient, manager.URStore, manager.URIdentity = client, owner, identity
	return manager
}
