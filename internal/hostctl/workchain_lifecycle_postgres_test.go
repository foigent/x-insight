package hostctl

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/testsourcefacts"
	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5"
)

func TestScopedSchema29To30CaseAndNativeRestore(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, 29)); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	store, err := db.OpenWorkspaceStore(ctx, c.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	actor := workspace.Actor{Kind: "human", Ref: "xs_1111111111111111"}
	now := time.Now().UTC()
	legacy, err := store.CreateCase(ctx, actor, workspace.CreateCaseInput{Title: "Schema29 legacy case", Description: "Unclassified original text remains retained"}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := testsourcefacts.RegisterSource(t, admin)
	bundle := testsourcefacts.Load(t, testsourcefacts.Chat)
	testsourcefacts.PersistMoney(t, ctx, c.ImporterDSN, source, bundle)
	if _, err = admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences,allow_source_context,allow_source_identity,allow_source_cost) VALUES($1,$2,true,true,true,true,true,true,true)`, actor.Ref, source); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetCase(ctx, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err = manager.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err = manager.VerifyRoleBoundaries(ctx, c); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetCase(ctx, legacy.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("migration altered legacy case", err)
	}
	// Empty schema30 extension can roll back in an isolated transaction; the
	// original schema29 data remains present. Rollback restores this test's 30.
	down, err := os.ReadFile("../../migrations/000030_add_scoped_workchain.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, string(down)); err != nil {
		tx.Rollback()
		t.Fatal("empty extension rollback", err)
	}
	tx.Rollback()
	var event, hash string
	if err = admin.QueryRow(`SELECT evidence_event_id::text,event_hash FROM workbench_economic_facts WHERE source_system_id=$1 ORDER BY id LIMIT 1`, source).Scan(&event, &hash); err != nil {
		t.Fatal(err)
	}
	reader, err := db.OpenEvidenceStore(ctx, c.ReaderDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	scope := db.NewScopedWorkchainStore(reader, store)
	local, finish, err := scope.Begin(ctx, actor.Ref, true, func(f func() error) error { return f() })
	if err != nil {
		t.Fatal(err)
	}
	command := workspace.ReviewCreationCommand{Operation: "start_review", OperationID: "11111111-1111-4111-8111-111111111111", NewCase: &workspace.CreateCaseInput{Title: "Restorable scoped case"}, Finding: workspace.CreateFindingInput{Title: "Retained source", Statement: "Synthetic complete source scope", Classification: "hypothesis"}, Selection: workspace.BasisSelection{Evidence: []workspace.EvidenceChoice{{Scope: "finding", EvidenceEventID: event, EventHash: hash, Relation: "supports"}}}, Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Explicitly inspected source"}, Reason: "Start classified case"}
	basis, err := store.PreviewReviewCreation(local, command)
	if err != nil {
		finish()
		t.Fatal(err)
	}
	command.ExpectedBasisDigest = basis.Digest
	receipt, err := store.CreateReview(local, actor, command, now, nil)
	if err != nil {
		finish()
		t.Fatal(err)
	}
	original, err := store.GetCase(local, receipt.CaseID)
	if err != nil {
		finish()
		t.Fatal(err)
	}
	finish()
	var label json.RawMessage
	if err = admin.QueryRow(`SELECT to_jsonb(l) FROM investigation_case_scope_labels l WHERE case_id=$1`, receipt.CaseID).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if err = db.ValidateWorkbenchRestore(ctx, admin); err != nil {
		t.Fatal(err)
	}
	preview, err := manager.PreviewCleanBaseline(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := manager.Backup(ctx, c, filepath.Join(t.TempDir(), "scope30.dump"))
	if err != nil {
		t.Fatal(err)
	}
	fixture := newInstallFixture(t)
	c = digestSyntheticCredentials(t, fixture, c)
	name := fmt.Sprintf("xinsight_restore_scope_%d", time.Now().UnixNano())
	restored, err := StageRestoredCredentialSet(fixture.paths, c, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupDB, e := sql.Open("pgx", c.AdminDSN)
		if e != nil {
			t.Error(e)
			return
		}
		defer cleanupDB.Close()
		if _, e := cleanupDB.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"); e != nil {
			t.Error(e)
		}
	})
	expected, err := db.LoadMigrationChecksums("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.RestoreToNewDatabase(ctx, c, restored, backup.Path, "../../migrations", expected); err != nil {
		t.Fatal(err)
	}
	if err = manager.VerifyRoleBoundaries(ctx, restored); err != nil {
		t.Fatal(err)
	}
	recovered, err := db.OpenWorkspaceStore(ctx, restored.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	recoveredReader, err := db.OpenEvidenceStore(ctx, restored.ReaderDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer recoveredReader.Close()
	local, finish, err = db.NewScopedWorkchainStore(recoveredReader, recovered).Begin(ctx, actor.Ref, true, func(f func() error) error { return f() })
	if err != nil {
		t.Fatal(err)
	}
	actual, err := recovered.GetCase(local, receipt.CaseID)
	if err != nil || !reflect.DeepEqual(original, actual) {
		finish()
		t.Fatal("restored scoped record changed", err)
	}
	replay, err := recovered.CreateReview(local, actor, command, now, nil)
	finish()
	if err != nil || !reflect.DeepEqual(receipt, replay) {
		t.Fatal("restored creation retry changed", err)
	}
	check, err := manager.PreviewCleanBaseline(ctx, c)
	if err != nil || !reflect.DeepEqual(preview, check) {
		t.Fatal("read-only backup/check altered source", err)
	}
	t.Log("schema29 legacy case preserved; empty rollback succeeds; native schema30 classification, case, history and retry restored to separate database")
}
