package hostctl

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

// Exercise the host administrator entry points as well as the stores: a
// successful store operation must not make an untested CLI management path safe.
func TestManagedSourceAndGrantCommandsUseScopedAdminState(t *testing.T) {
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

	engine, paths := newConnectorTargetTestEngine(t)
	stageDatabaseCredentials(t, paths)
	digestSyntheticCredentials(t, installFixture{paths: paths}, credentials)
	owner := managedStore(t, credentials.AdminDSN)
	admin := managedSQL(t, credentials.AdminDSN)
	var source, streamID, budgetID, contentID string
	if err := admin.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&source, &streamID, &budgetID, &contentID); err != nil {
		t.Fatal(err)
	}
	authorization, _, _ := managedOfflineFixture(t, source)
	stream, err := owner.RegisterSourceFactsStream(ctx, sourcefacts.Stream{ID: streamID, SourceSystemID: source, SourceInstance: authorization.SourceInstance, OrgID: authorization.OrgID, ProjectID: authorization.ProjectID, Profile: authorization.Profile, ScopeGeneration: "1"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err = owner.ControlSourceFactsStream(ctx, stream.ID, "active", stream.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "status", ID: stream.ID})
	if err != nil || got.(sourcefacts.Stream).Status != "active" {
		t.Fatalf("administrator status=%v err=%v", got, err)
	}
	paused, err := engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "pause", ID: stream.ID, Revision: stream.Revision})
	if err != nil || paused.(sourcefacts.Stream).Status != "paused" {
		t.Fatalf("administrator pause=%v err=%v", paused, err)
	}
	// Stopping collection may use the pre-pause revision: it only reduces
	// authority. A revoked registration cannot be moved back to paused.
	revoked, err := engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "revoke", ID: stream.ID, Revision: stream.Revision})
	if err != nil || revoked.(sourcefacts.Stream).Status != "revoked" {
		t.Fatalf("administrator revoke after pause=%v err=%v", revoked, err)
	}
	if _, err := engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "pause", ID: stream.ID, Revision: revoked.(sourcefacts.Stream).Revision}); err == nil {
		t.Fatal("revoked stream moved back to paused")
	}

	offline, err := owner.RegisterSourceOfflineAuthorization(ctx, authorization)
	if err != nil {
		t.Fatal(err)
	}
	got, err = engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "offline-status", ID: offline.ID})
	if err != nil || got.(db.SourceOfflineRegistration).Status != "disabled" {
		t.Fatalf("offline status=%v err=%v", got, err)
	}
	got, err = engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "offline-revoke", ID: offline.ID, Revision: offline.Revision})
	if err != nil || got.(db.SourceOfflineRegistration).Status != "revoked" {
		t.Fatalf("offline revoke=%v err=%v", got, err)
	}

	budget, err := owner.RegisterBudgetCollection(ctx, sourcefacts.BudgetCollection{ID: budgetID, SourceSystemID: source, SourceInstance: authorization.SourceInstance, OrgID: authorization.OrgID, ProjectID: authorization.ProjectID, ScopeGeneration: "1", Profiles: []string{sourcefacts.BudgetDefinitionProfile, sourcefacts.BudgetSummaryProfile}})
	if err != nil {
		t.Fatal(err)
	}
	got, err = engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "budget-status", ID: budget.ID})
	if err != nil || got.(sourcefacts.BudgetCollection).Status != "paused" {
		t.Fatalf("budget status=%v err=%v", got, err)
	}
	got, err = engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "budget-revoke", ID: budget.ID, Revision: budget.Revision})
	if err != nil || got.(sourcefacts.BudgetCollection).Status != "revoked" {
		t.Fatalf("budget revoke=%v err=%v", got, err)
	}
	if _, err := engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "budget-pause", ID: budget.ID, Revision: budget.Revision}); err == nil {
		t.Fatal("stale budget revision accepted")
	}
	if _, err := engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "budget-offline-register", AuthorizationFile: filepath.Join(paths.Root, "missing-budget-authorization.json")}); err == nil {
		t.Fatal("bounded offline budget registered without an authorization file")
	}

	content, err := owner.RegisterContentCollection(ctx, sourcefacts.ContentCollection{ID: contentID, SourceSystemID: source, SourceInstance: authorization.SourceInstance, OrgID: authorization.OrgID, ProjectID: authorization.ProjectID, CredentialID: "synthetic-content", RegistrationHash: sourcefacts.Hash([]byte("synthetic-content-registration")), ContentScopeRevision: "1", RecoveryEpoch: "1", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: sourcefacts.ContentBoundary{Time: time.Now().UTC().Format(sourcefacts.TimeFormat), AfterSequence: "0"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err = engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "content-status", ID: content.ID})
	if err != nil || got.(sourcefacts.ContentCollection).Status != "registered" {
		t.Fatalf("content status=%v err=%v", got, err)
	}
	if _, err := engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "content-preflight", ID: content.ID}); err == nil {
		t.Fatal("content preflight accepted without managed configuration")
	}
	got, err = engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "content-revoke", ID: content.ID, Revision: content.Revision})
	if err != nil || got.(sourcefacts.ContentCollection).Status != "revoked" {
		t.Fatalf("content revoke=%v err=%v", got, err)
	}
	if _, err := engine.ManageSourceFacts(ctx, SourceFactsRequest{Action: "content-pause", ID: content.ID, Revision: content.Revision}); err == nil {
		t.Fatal("stale content revision accepted")
	}

	create := strings.Replace(grantRequest, "22222222-2222-2222-2222-222222222222", source, 1)
	command, err := DecodeWorkbenchGrantChange([]byte(create))
	if err != nil {
		t.Fatal(err)
	}
	createPath := filepath.Join(paths.Root, "grant-create.json")
	writeHostFile(t, createPath, []byte(create), 0o600)
	if _, err := engine.ManageWorkbenchGrant(ctx, createPath, true); err == nil {
		t.Fatal("grant creation accepted without a configured account")
	}
	if err := os.Chmod(createPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ManageWorkbenchGrant(ctx, createPath, false); err == nil {
		t.Fatal("world-readable grant command accepted")
	}
	writeHostFile(t, filepath.Join(paths.Root, "bad-grant.json"), []byte("{"), 0o600)
	if _, err := engine.ManageWorkbenchGrant(ctx, filepath.Join(paths.Root, "bad-grant.json"), true); err == nil {
		t.Fatal("malformed grant command accepted")
	}
	grant, err := owner.ChangeWorkbenchGrant(ctx, command, false)
	if err != nil || !grant.Changed {
		t.Fatalf("stage grant=%v err=%v", grant, err)
	}
	revision := grant.Revision
	revoke := db.WorkbenchGrantChange{SchemaVersion: command.SchemaVersion, Action: "revoke", ID: command.ID, Subject: command.Subject, Source: command.Source, SourceProject: command.SourceProject, CostProject: command.CostProject, ExpectedRevision: &revision, Capabilities: []string{}}
	raw, err := json.Marshal(revoke)
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(paths.Root, "grant-revoke.json")
	writeHostFile(t, requestPath, raw, 0o600)
	if err := os.Symlink(requestPath, requestPath+".link"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ManageWorkbenchGrant(ctx, requestPath+".link", false); err == nil {
		t.Fatal("symlink grant command accepted")
	}
	before, err := engine.InspectWorkbenchGrant(ctx, command.ID)
	if err != nil || len(before) == 0 {
		t.Fatalf("grant inspect before revoke=%s err=%v", before, err)
	}
	preview, err := engine.ManageWorkbenchGrant(ctx, requestPath, true)
	if err != nil || !preview.CheckOnly || preview.Changed {
		t.Fatalf("grant check=%v err=%v", preview, err)
	}
	after, err := engine.InspectWorkbenchGrant(ctx, command.ID)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("grant check changed stored grant: before=%s after=%s err=%v", before, after, err)
	}
	removed, err := engine.ManageWorkbenchGrant(ctx, requestPath, false)
	if err != nil || !removed.Changed || removed.Revision <= grant.Revision {
		t.Fatalf("grant revoke=%v err=%v", removed, err)
	}
	if _, err := engine.ManageWorkbenchGrant(ctx, requestPath, false); err == nil {
		t.Fatal("stale grant revocation accepted")
	}
}

func TestManagedSourceWorkerCommandsDoNotStartCollectionImplicitly(t *testing.T) {
	engine, _ := newConnectorTargetTestEngine(t)
	runner := &fakeRunner{}
	engine.Runner = runner
	status, err := engine.ManageSourceFacts(context.Background(), SourceFactsRequest{Action: "worker-status"})
	if err != nil {
		t.Fatal(err)
	}
	state := status.(map[string]bool)
	if state["present"] || state["active"] || state["enabled"] {
		t.Fatalf("absent worker reported running: %v", state)
	}
	stopped, err := engine.ManageSourceFacts(context.Background(), SourceFactsRequest{Action: "worker-stop"})
	if err != nil || stopped.(map[string]bool)["active"] {
		t.Fatalf("worker stop=%v err=%v", stopped, err)
	}
	if _, err := engine.ManageSourceFacts(context.Background(), SourceFactsRequest{Action: "configure", ConfigFile: "relative.json"}); err == nil {
		t.Fatal("relative managed configuration accepted")
	}
	if _, err := engine.ManageSourceFacts(context.Background(), SourceFactsRequest{Action: "worker-start"}); err == nil {
		t.Fatal("worker started without a managed configuration")
	}
	if runner.sourceFactsWorkerActive || runner.sourceFactsWorkerEnabled {
		t.Fatal("configuration failure started collection")
	}
}
