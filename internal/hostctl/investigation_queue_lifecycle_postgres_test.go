package hostctl

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5"
)

func TestInvestigationQueueNativeUpgradeRestore(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, 27)); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenWorkspaceStore(ctx, c.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	actor := workspace.Actor{Kind: "human", Ref: "xs_1111111111111111"}
	oldPacks := map[string]workspace.Case{}
	for revision := 1; revision <= 3; revision++ {
		old, e := store.CreateCase(ctx, actor, workspace.CreateCaseInput{Title: fmt.Sprintf("Predecessor schema27 v%d pack", revision)}, time.Now(), nil)
		if e != nil {
			t.Fatal(e)
		}
		finding, e := store.CreateFinding(ctx, old.ID, actor, workspace.CreateFindingInput{Title: "Historical claim", Statement: "Preserve prior pack bytes", Classification: "hypothesis"}, time.Now(), nil)
		if e != nil {
			t.Fatal(e)
		}
		if revision == 2 {
			_, e = store.ApplyFindingFollowup(ctx, old.ID, finding.ID, actor, workspace.FollowupCommand{Operation: "update_followup", OperationID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", ExpectedVersion: 0, ExpectedCaseVersion: 2, Payload: workspace.FollowupPayload{Reason: "Historical responsibility"}}, workspace.AssigneeLookup{}, time.Now())
			if e != nil {
				t.Fatal(e)
			}
		}
		if revision == 3 {
			basis, e := store.PreviewFindingBasis(ctx, old.ID, finding.ID, workspace.BasisSelection{})
			if e != nil {
				t.Fatal(e)
			}
			_, e = store.ApplyFindingAssessment(ctx, old.ID, finding.ID, actor, workspace.AssessmentCommand{Operation: "record_review", OperationID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", ExpectedCaseVersion: 2, ExpectedBasisDigest: basis.Digest, Selection: basis.Selected, Review: &workspace.ReviewPayload{Conclusion: "pending_review", Reason: "Historical review pending source confirmation"}}, time.Now())
			if e != nil {
				t.Fatal(e)
			}
		}
		sealed, e := store.SealCase(ctx, old.ID, actor, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		if revision < 3 {
			// Seed supported historical wire formats in this disposable predecessor.
			// The migration/restore must preserve these exact stored bytes afterward.
			var legacy map[string]any
			if json.Unmarshal(sealed.SealedPack, &legacy) != nil {
				t.Fatal("decode legacy fixture")
			}
			legacy["schema_version"] = fmt.Sprintf("xinsight.evidence-pack.v%d", revision)
			delete(legacy, "assessments")
			if revision == 1 {
				delete(legacy, "responsibility")
			}
			sealed.SealedPack, e = json.Marshal(legacy)
			if e != nil {
				t.Fatal(e)
			}
			digest := sha256.Sum256(sealed.SealedPack)
			sealed.SealedDigest = fmt.Sprintf("sha256:%x", digest)
			admin, e := sql.Open("pgx", c.AdminDSN)
			if e != nil {
				t.Fatal(e)
			}
			_, e = admin.Exec(`UPDATE investigation_cases SET sealed_pack=$2,sealed_digest=$3 WHERE id=$1`, old.ID, []byte(sealed.SealedPack), sealed.SealedDigest)
			admin.Close()
			if e != nil {
				t.Fatal(e)
			}
		}
		var pack workspace.EvidencePack
		if json.Unmarshal(sealed.SealedPack, &pack) != nil || pack.SchemaVersion != fmt.Sprintf("xinsight.evidence-pack.v%d", revision) {
			t.Fatalf("wrong predecessor pack v%d: %s", revision, pack.SchemaVersion)
		}
		oldPacks[old.ID] = sealed
	}
	if _, err = db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err = manager.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	for id, sealed := range oldPacks {
		after, e := store.GetCase(ctx, id)
		if e != nil || !bytes.Equal(sealed.SealedPack, after.Case.SealedPack) {
			t.Fatal("upgrade altered old pack", e)
		}
	}
	cmd := workspace.QueueFilterCommand{OperationID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Retained preference", Schema: workspace.QueueFilterSchema, Filters: workspace.QueueFilters{Mode: "current", Period: "7d", Sort: "due_asc"}}
	saved, err := store.ApplyQueueFilter(ctx, actor.Ref, "", "create", cmd, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	deletedCmd := cmd
	deletedCmd.OperationID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	deletedCmd.Name = "Deleted preference"
	deleted, err := store.ApplyQueueFilter(ctx, actor.Ref, "", "create", deletedCmd, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	deletedCmd.OperationID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	deletedCmd.ExpectedRevision = 1
	tombstone, err := store.ApplyQueueFilter(ctx, actor.Ref, deleted.ID, "delete", deletedCmd, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	before, err := manager.PreviewCleanBaseline(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := manager.Backup(ctx, c, filepath.Join(t.TempDir(), "queue.dump"))
	if err != nil {
		t.Fatal(err)
	}
	f := newInstallFixture(t)
	c = digestSyntheticCredentials(t, f, c)
	name := fmt.Sprintf("xinsight_restore_queue_%d", time.Now().UnixNano())
	restored, err := StageRestoredCredentialSet(f.paths, c, name)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	t.Cleanup(func() {
		control, e := sql.Open("pgx", c.AdminDSN)
		if e != nil {
			t.Error(e)
			return
		}
		defer control.Close()
		_, e = control.Exec(`DROP DATABASE IF EXISTS ` + pgx.Identifier{name}.Sanitize() + ` WITH (FORCE)`)
		if e != nil {
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
	restoredStore, err := db.OpenWorkspaceStore(ctx, restored.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredStore.Close()
	prefs, err := restoredStore.ListQueueFilters(ctx, actor.Ref)
	if err != nil || len(prefs) != 1 || !workspace.SameQueueValue(prefs[0], saved) {
		t.Fatal("restored preferences mismatch", err)
	}
	replay, err := restoredStore.ApplyQueueFilter(ctx, actor.Ref, deleted.ID, "delete", deletedCmd, time.Now())
	if err != nil || !workspace.SameQueueValue(replay, tombstone) {
		t.Fatal("restored tombstone receipt mismatch", err)
	}
	for id, sealed := range oldPacks {
		after, e := restoredStore.GetCase(ctx, id)
		if e != nil || !bytes.Equal(sealed.SealedPack, after.Case.SealedPack) {
			t.Fatal("restore altered old pack", e)
		}
	}
	final, err := manager.PreviewCleanBaseline(ctx, c)
	if err != nil || !reflect.DeepEqual(before, final) {
		t.Fatal("backup/restore modified original database", err)
	}
	if _, err = admin.Exec(`UPDATE investigation_queue_filters SET record=jsonb_set(record,'{name}','"tampered"') WHERE id=$1`, saved.ID); err != nil {
		t.Fatal(err)
	}
	badBackup, err := manager.Backup(ctx, c, filepath.Join(t.TempDir(), "queue-corrupt.dump"))
	if err != nil {
		t.Fatal(err)
	}
	badName := fmt.Sprintf("xinsight_restore_queue_bad_%d", time.Now().UnixNano())
	badTarget, err := StageRestoredCredentialSet(f.paths, c, badName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		control, e := sql.Open("pgx", c.AdminDSN)
		if e != nil {
			t.Error(e)
			return
		}
		defer control.Close()
		_, e = control.Exec(`DROP DATABASE IF EXISTS ` + pgx.Identifier{badName}.Sanitize() + ` WITH (FORCE)`)
		if e != nil {
			t.Error(e)
		}
	})
	if err = manager.RestoreToNewDatabase(ctx, c, badTarget, badBackup.Path, "../../migrations", expected); err == nil {
		t.Fatal("hostctl accepted corrupted preference projection")
	}
	t.Log("Native PG16 schema27→28; preexisting v1/v2/v3 sealed bytes unchanged; own live preference + deletion tombstone restored to isolated database; original receipt replay; runtime grants; source unchanged by preview/backup/restore; hostctl rejects tampered restored state")
}

// Invoke the existing full native qualification only against a new synthetic DB.
func TestInvestigationQueueCurrentNativeQualification(t *testing.T) {
	_, c := connectorDigestDatabase(t)
	t.Setenv("XINSIGHT_HOSTCTL_DATABASE_TEST", "1")
	t.Setenv("XINSIGHT_HOSTCTL_ADMIN_DSN", c.AdminDSN)
	t.Setenv("XINSIGHT_HOSTCTL_READER_DSN", c.ReaderDSN)
	t.Setenv("XINSIGHT_HOSTCTL_WORKSPACE_DSN", c.WorkspaceDSN)
	t.Setenv("XINSIGHT_HOSTCTL_IMPORTER_DSN", c.ImporterDSN)
	TestPostgreSQL16RoleBackupAndIsolatedRestoreQualification(t)
}
