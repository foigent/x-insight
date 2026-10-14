package hostctl

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/workevent"
	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5"
)

func TestPrivateBackupPostgresIndependentRootReadsOriginalBodyAndPreservesExpired(t *testing.T) {
	ctx, credentials := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.EnsureRoles(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	migrations := "../../migrations"
	if _, err := db.RunMigrations(ctx, credentials.AdminDSN, migrations); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", credentials.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	owner, err := db.OpenSourceFactsStore(credentials.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	writer, err := db.OpenWorkspaceStore(ctx, credentials.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	e, c := managedCustodyFixture(t)
	e.Database = manager
	if err = e.SetReviewBodyConfig(c, false); err != nil {
		t.Fatal(err)
	}
	if err = e.ensureControlDirectories(); err != nil {
		t.Fatal(err)
	}
	if err = e.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatal(err)
	}
	ring, err := reviewbody.LoadKeyRingFromFile(c.KeyID, c.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	originalKey, err := os.ReadFile(c.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	const sourceID = "11111111-1111-4111-8111-111111111111"
	const collectionID = "22222222-2222-4222-8222-222222222222"
	const projectID = "33333333-3333-4333-8333-333333333333"
	const subject = "xs_1111111111111111"
	now := time.Now().UTC().Truncate(time.Microsecond)
	past := now.Add(-2 * time.Hour)
	if _, err = admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1,'agent-runtime:backup-fixture','agent_runtime','Synthetic backup source')`, sourceID); err != nil {
		t.Fatal(err)
	}
	scope := workevent.Scope{SourceSystemID: sourceID, SourceInstance: "offline-backup-fixture", OrgID: "7", ProjectID: "9"}
	collection, err := owner.RegisterWorkEventCollection(ctx, db.WorkEventCollection{ID: collectionID, Scope: scope, Profile: workevent.CollectionProfile, ScopeGeneration: "1", Status: "staged", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	collection, err = owner.ActivateWorkEventCollection(ctx, collection.ID, collection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.BindReviewCaptureProject(ctx, sourceID, scope.SourceInstance, scope.OrgID, scope.ProjectID, projectID, "Synthetic backup project", subject, past); err != nil {
		t.Fatal(err)
	}
	revision, err := owner.RequestReviewCaptureDefault(ctx, db.ReviewCaptureSettings{Mode: "full_content", RetentionSeconds: 1, ProjectQuotaBytes: 1048576, MaxItemBytes: 1024}, 0, subject, past)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.ConfirmReviewCaptureDefault(ctx, revision, subject, past); err != nil {
		t.Fatal(err)
	}
	provisioner := db.NewWorkspaceStore(admin)
	credential, err := provisioner.CreateWorkEventIngestIdentity(ctx, workspace.Actor{Kind: "root", Ref: "root"}, db.CreateWorkEventIngestIdentityInput{DisplayName: "Synthetic backup producer", ExpiresAt: now.Add(time.Hour), Bindings: []db.WorkEventIngestBindingInput{{CollectionID: collectionID, ProducerProjectKey: "agent:main", ProjectLabel: "Synthetic backup project"}}}, past, nil)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := workspace.ServiceTokenDigest(credential.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.GrantReviewBodyIngestBinding(ctx, credential.ServiceIdentity.ID, collectionID, past); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"result":"synthetic offline restore"}`)
	sum := sha256.Sum256(body)
	admit := func(ref string, at time.Time) time.Time {
		t.Helper()
		event := workevent.Event{SchemaVersion: workevent.EventVersion, Reporter: scope, EventRef: "oc:" + ref, Revision: 1, OccurredAt: at.Add(-time.Minute), RecordedAt: at, SubjectKind: "tool_call", LifecycleState: "completed", RunID: "run-" + ref, ToolCallID: "tool-" + ref, Actor: workevent.Actor{Kind: "service", Ref: "openclaw"}, ActionKind: "tool.completed", ContentState: "referenced", ContentRefs: []workevent.ContentRef{{Kind: "tool_output", Ref: "ocbody:" + ref, SHA256: "sha256:" + hex.EncodeToString(sum[:])}}}
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		object, err := connector.DecodeStrictJSONObject(raw)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = connector.CanonicalizeJSONValue(object)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := writer.CommitAuthorizedWorkEventItem(ctx, digest, db.WorkEventIngestItem{CollectionID: collectionID, ExpectedCollectionRevision: collection.Revision, ScopeGeneration: collection.ScopeGeneration, Kind: "event", Document: raw}, at)
		if err != nil || receipt.Outcome != "accepted" {
			t.Fatal(receipt, err)
		}
		accepted, err := writer.CommitAuthorizedWorkEventReviewBody(ctx, digest, db.WorkEventReviewBodyInput{CollectionID: collectionID, ExpectedCollectionRevision: collection.Revision, ScopeGeneration: collection.ScopeGeneration, EventRef: event.EventRef, EventRevision: 1, EventHash: receipt.SHA256, ContentKind: "tool_output", ContentRef: event.ContentRefs[0].Ref, ByteDomain: "openclaw.after_tool_call.canonical-json.v1", SHA256: event.ContentRefs[0].SHA256, Body: body}, ring, at)
		if err != nil || accepted.Outcome != "accepted" {
			t.Fatal(accepted, err)
		}
		return accepted.ExpiresAt
	}
	expiredAt := admit("backup-expired", now)
	revision, err = owner.RequestReviewCaptureDefault(ctx, db.ReviewCaptureSettings{Mode: "full_content", RetentionSeconds: 3600, ProjectQuotaBytes: 1048576, MaxItemBytes: 1024}, revision, subject, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.ConfirmReviewCaptureDefault(ctx, revision, subject, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	liveAt := admit("backup-live", time.Now().UTC().Truncate(time.Microsecond))
	// Real admission deliberately uses the production publication clock. Wait
	// for the short retained object to expire rather than editing its AAD/state.
	if remaining := time.Until(expiredAt); remaining > 0 {
		time.Sleep(remaining)
	}
	now = time.Now().UTC().Truncate(time.Microsecond)
	if _, err = admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_content,allowed_content_purposes) VALUES($1,$2::uuid,'project-9',true,'["human_review"]')`, subject, sourceID); err != nil {
		t.Fatal(err)
	}
	var beforeObjects string
	const objectDigest = `SELECT encode(public.digest(string_agg(row_to_json(x)::text,E'\n' ORDER BY x.content_ref),'sha256'),'hex') FROM (SELECT * FROM review_body_objects) x`
	if err = admin.QueryRowContext(ctx, objectDigest).Scan(&beforeObjects); err != nil {
		t.Fatal(err)
	}
	checksums, err := db.LoadMigrationChecksums(migrations)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newInstallFixture(t)
	target, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	target.Identity.DatabaseSchema = "44"
	target.Bundle.Manifest.MigrationChecksums = checksums
	journal, err := e.startJournal("backup", "", "", &target.Identity, &target.Identity)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Paths.BackupsDir, "offline-full.tar")
	backup, err := e.writePrivateBackup(ctx, path, journal, credentials, target)
	if err != nil {
		t.Fatal(err)
	}
	// Archive and original key are independent: only the archive is handed to the new root.
	if err = os.Rename(c.KeyFile, c.KeyFile+".fixture-unavailable"); err != nil {
		t.Fatal(err)
	}
	other, _, _ := automaticCustodyFixture(t)
	other.Random = bytes.NewReader(nil)
	recoveryJournal, err := other.startJournal("install", "", "", nil, &target.Identity)
	if err != nil {
		t.Fatal(err)
	}
	recoveryJournal.RecoveryBundlePath = path
	recoveryJournal.RecoveryBundleSHA256 = backup.SHA256
	recoveryJournal.CustodyCandidate = &ReviewBodyConfig{c.KeyID, filepath.Join(other.Paths.EtcDir, "review-body.key")}
	if err = other.prepareCustody(ctx, recoveryJournal, DatabaseCredentials{}, target.Identity); err != nil {
		t.Fatal("import through normal custody preparation", err)
	}
	restoredBinding, active, present, err := other.custodyBinding()
	if err != nil || !active || !present {
		t.Fatal(restoredBinding, err)
	}
	importedKey, err := os.ReadFile(restoredBinding.KeyFile)
	if err != nil || !bytes.Equal(importedKey, originalKey) {
		t.Fatal("original key regenerated", err)
	}
	importedRing, err := reviewbody.LoadKeyRingFromFile(restoredBinding.KeyID, restoredBinding.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	other.Database = manager
	archive, err := other.inspectPrivateBackup(ctx, path, backup.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Clear()
	dump, err := other.extractPrivateDump(ctx, archive, recoveryJournal)
	if err != nil {
		t.Fatal(err)
	}
	// Restoration uses the scoped control DB for createdb, never the source DB or provider.
	control := credentials
	control.AdminDSN = os.Getenv("XINSIGHT_POSTGRES_DSN")
	control.Admin, err = pgx.ParseConfig(control.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	newName := fmt.Sprintf("xinsight_restore_offline_%d", time.Now().UnixNano())
	restored := credentials
	for _, pair := range []struct {
		dsn    *string
		config **pgx.ConnConfig
	}{{&restored.AdminDSN, &restored.Admin}, {&restored.WorkspaceDSN, &restored.Workspace}, {&restored.ReaderDSN, &restored.Reader}, {&restored.ImporterDSN, &restored.Importer}} {
		u, err := url.Parse(*pair.dsn)
		if err != nil {
			t.Fatal(err)
		}
		u.Path = "/" + newName
		*pair.dsn = u.String()
		*pair.config, err = pgx.ParseConfig(*pair.dsn)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		controlDB, err := sql.Open("pgx", control.AdminDSN)
		if err != nil {
			t.Error(err)
			return
		}
		defer controlDB.Close()
		if _, err = controlDB.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{newName}.Sanitize() + " WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	if err = manager.RestoreToNewDatabase(ctx, control, restored, dump.Path, migrations, checksums); err != nil {
		t.Fatal(err)
	}
	if err = manager.VerifyReviewBodyRestore(ctx, restored, importedRing, now); err != nil {
		t.Fatal(err)
	}
	restoredDB, err := sql.Open("pgx", restored.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	if err = db.SuspendForeignRestoredAuthority(ctx, restoredDB, archive.Manifest.DeploymentID, "xid_BBBBBBBBBBBBBBBBBBBBBB", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	historical, err := db.OpenWorkspaceStore(ctx, restored.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer historical.Close()
	read, err := historical.ReadReviewBody(ctx, subject, "ocbody:backup-live", importedRing, now)
	if err != nil || !bytes.Equal(read.Bytes, body) || !read.ExpiresAt.Equal(liveAt) {
		t.Fatal("independent retained read", err)
	}
	if _, err = historical.ReadReviewBody(ctx, subject, "ocbody:backup-expired", importedRing, now); !errors.Is(err, reviewbody.ErrExpired) {
		t.Fatal("expired body restored readable", err)
	}
	var restoredObjects, sourceObjects string
	var expiration time.Time
	if err = restoredDB.QueryRowContext(ctx, objectDigest).Scan(&restoredObjects); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRowContext(ctx, objectDigest).Scan(&sourceObjects); err != nil {
		t.Fatal(err)
	}
	if restoredObjects != beforeObjects || sourceObjects != beforeObjects {
		t.Fatal("ciphertext changed or original database overwritten")
	}
	if err = restoredDB.QueryRowContext(ctx, `SELECT expires_at FROM review_body_operations WHERE content_ref='ocbody:backup-expired'`).Scan(&expiration); err != nil || !expiration.Equal(expiredAt) {
		t.Fatal("expiry reset", err)
	}
	var activeTokens int
	if err = restoredDB.QueryRowContext(ctx, `SELECT count(*) FROM service_identity_tokens WHERE revoked_at IS NULL`).Scan(&activeTokens); err != nil || activeTokens != 0 {
		t.Fatal("foreign ingest authority survived", activeTokens, err)
	}
	if err = manager.VerifyReviewBodyInventory(ctx, restored); err == nil {
		t.Fatal("expired ciphertext allowed key regeneration")
	}
	t.Log("Local PG16 original admission, full custom dump/key package, independent root recovery, pg_restore into new DB, restricted historical read, unchanged live+expired ciphertext and expiry, foreign machine authority suspended; source key unavailable. No provider or Target evidence.")
}
