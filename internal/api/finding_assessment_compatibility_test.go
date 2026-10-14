package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5"
)

func TestAssessmentUpgrade26PreservesResponsibilityV2AndSession(t *testing.T) {
	ctx, dsn, admin := responsibilityDatabase(t)
	migrations := filepath.Join("..", "..", "migrations")
	old := t.TempDir()
	entries, err := os.ReadDir(migrations)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") || entry.Name()[:6] > "000026" {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(migrations, entry.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(old, entry.Name()), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if _, err = db.RunMigrations(ctx, dsn, old); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reader, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	actor := assessmentActor()
	c, err := store.CreateCase(ctx, actor, workspace.CreateCaseInput{Title: "Historical schema26 Case"}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	finding, err := store.CreateFinding(ctx, c.ID, actor, workspace.CreateFindingInput{Title: "Old responsibility is not review", Statement: "Only a historical human hypothesis", Classification: "hypothesis"}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := access.DerivePasswordHash([]byte("synthetic responsibility password"))
	if err != nil {
		t.Fatal(err)
	}
	// A real old v1 account configuration and human session exist before migration.
	config := access.AccountConfig{SchemaVersion: access.AccountConfigSchema, Accounts: []access.AccountConfigEntry{{SubjectID: actor.Ref, Username: "reviewer.one", Role: access.RoleInvestigator, PasswordHash: hash}}}
	f := &responsibilityFixture{admin: admin, store: store, reader: reader, caseItem: c, finding: finding, dsn: dsn, config: config}
	f.reloadAccounts(t, config)
	client := responsibilityLogin(t, f, "reviewer.one")
	view, err := store.GetFindingReview(ctx, c.ID, finding.ID)
	if err != nil {
		t.Fatal(err)
	}
	command := workspace.FollowupCommand{Operation: "update_followup", OperationID: randomTestUUID(t), ExpectedVersion: 0, ExpectedCaseVersion: view.CaseVersion, Payload: workspace.FollowupPayload{Reason: "Explicit old responsibility record"}}
	status, oldReceipt := client.call(t, "POST", f.reviewPath()+"-actions", command, nil)
	if status != 200 {
		t.Fatalf("old command %d: %s", status, oldReceipt)
	}
	record, err := store.GetCase(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	responsibility, err := store.GetFindingReview(ctx, c.ID, finding.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Responsibility = &workspace.FollowupSnapshot{Followups: []workspace.Followup{responsibility.Followup}, Actions: responsibility.History.Items}
	now := time.Now().UTC().Truncate(time.Microsecond)
	pack, _, _, err := workspace.BuildEvidencePack(record, now, actor.Ref, record.Case.Version+1)
	if err != nil {
		t.Fatal(err)
	}
	pack.SchemaVersion, pack.Assessments = workspace.EvidencePackSchemaV2, nil
	oldBytes, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	oldBytes = append(oldBytes, '\n')
	digest := responsibilityDigest(oldBytes)
	if _, err = admin.Exec(`UPDATE investigation_cases SET status='sealed',version=version+1,sealed_at=$2,sealed_by_subject=$3,sealed_revision=$6,sealed_digest=$4,sealed_pack=$5 WHERE id=$1`, c.ID, now, actor.Ref, digest, oldBytes, pack.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RunMigrations(ctx, dsn, migrations); err != nil {
		t.Fatal(err)
	}
	if status, raw := client.call(t, "POST", f.reviewPath()+"-actions", command, nil); status != 200 || !bytes.Equal(raw, oldReceipt) {
		t.Fatalf("existing human session or receipt changed across migration: %d", status)
	}
	if status, raw := client.call(t, "GET", "/api/v1/cases/"+c.ID+"/evidence-pack.json", nil, nil); status != 200 || !bytes.Equal(raw, oldBytes) {
		t.Fatalf("v2 bytes changed: %d", status)
	}
	current := assessmentCurrent(t, f, c.ID, finding.ID)
	if current.Assessment.Recorded || current.Assessment.Review != nil || current.Assessment.Progress != nil || len(current.History.Items) != 0 {
		t.Fatal("old responsibility inferred review or progress")
	}
	if err = db.ValidateFindingResponsibilityRestore(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err = db.ValidateFindingAssessmentRestore(ctx, admin); err != nil {
		t.Fatal(err)
	}
	_, workspaceDSN, _ := prepareWorkbenchRuntimeRoles(t, ctx, dsn)
	reopened, err := db.OpenWorkspaceStore(ctx, workspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replayed, err := reopened.GetCase(ctx, c.ID)
	if err != nil || !bytes.Equal(replayed.Case.SealedPack, oldBytes) {
		t.Fatal("reopened production connection altered v2", err)
	}
	t.Log("Native 26→27, legacy v1 account/current pre-upgrade session, exact old responsibility receipt and noncanonical v2 pack bytes, unrecorded assessment/progress and production-role reconnect passed")
}

func TestAssessmentNativeBackupRestoreAndReadonlyCheck(t *testing.T) {
	f := startResponsibilityProduction(t)
	ctx := context.Background()
	selection := assessmentFixtureSources(t, f)
	actor := assessmentActor()
	first, err := f.store.ResolveGap(ctx, actor, selection.Gaps[0].GapID, workspace.ResolveGapInput{State: "investigating", Note: "ADOPTED IN ORIGINAL REVIEW"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	command := previewAssessmentCreation(t, f, newAssessmentCreation(t, selection))
	created, err := f.store.CreateReview(ctx, actor, command, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.SealCase(ctx, created.CaseID, actor, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ResolveGap(ctx, actor, selection.Gaps[0].GapID, workspace.ResolveGapInput{State: "source_fixed", Note: "LATEST CONTENT MUST NOT REBUILD HISTORY"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := f.store.GetCase(ctx, created.CaseID)
	if err != nil {
		t.Fatal(err)
	}
	manager := hostctl.PostgresManager{Runner: hostctl.ExecRunner{}}
	credentials := responsibilityCredentials(t, f.dsn)
	fingerprint, err := manager.EvidenceFingerprint(ctx, credentials)
	if err != nil {
		t.Fatal(err)
	}
	var beforeRows string
	rowSnapshot := `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a) ORDER BY case_id,finding_id) FROM case_finding_assessments a),(SELECT jsonb_agg(to_jsonb(a) ORDER BY case_id,finding_id,result_version) FROM case_finding_assessment_actions a),(SELECT jsonb_agg(to_jsonb(a) ORDER BY case_id,finding_id) FROM finding_review_creations a))::text`
	if err = f.admin.QueryRow(rowSnapshot).Scan(&beforeRows); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.PreviewCleanBaseline(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	if err = db.ValidateFindingAssessmentRestore(ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	var afterRows string
	if err = f.admin.QueryRow(rowSnapshot).Scan(&afterRows); err != nil || beforeRows != afterRows {
		t.Fatal("check changed assessment records", err)
	}
	if after, e := manager.EvidenceFingerprint(ctx, credentials); e != nil || after != fingerprint {
		t.Fatal("check changed source evidence", e)
	}
	backup, err := manager.Backup(ctx, credentials, filepath.Join(t.TempDir(), "assessment.dump"))
	if err != nil {
		t.Fatal(err)
	}
	name := "xinsight_restore_asmt_" + strings.ReplaceAll(randomTestUUID(t), "-", "")
	u, err := url.Parse(f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	restored := responsibilityCredentials(t, u.String())
	t.Cleanup(func() {
		if _, e := f.admin.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"); e != nil {
			t.Error(e)
		}
	})
	expected, err := db.LoadMigrationChecksums(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.RestoreToNewDatabase(ctx, credentials, restored, backup.Path, filepath.Join("..", "..", "migrations"), expected); err != nil {
		t.Fatal(err)
	}
	if err = manager.VerifyRoleBoundaries(ctx, restored); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenWorkspaceStore(ctx, restored.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	replay, err := store.CreateReview(ctx, actor, command, time.Now(), nil)
	if err != nil || !workspace.SameAssessmentValue(replay, created) {
		t.Fatal("restored creation retry changed identity/receipt", err)
	}
	after, err := store.GetCase(ctx, created.CaseID)
	if err != nil || !bytes.Equal(before.Case.SealedPack, after.Case.SealedPack) {
		t.Fatal("native restore changed v3 bytes", err)
	}
	v, err := store.GetFindingAssessment(ctx, created.CaseID, created.FindingID, time.Now())
	if err != nil || v.Assessment.Review.Basis.AdoptedGaps[0].Note != first.Note || v.ReviewBasis.State != "changed" {
		t.Fatal("restore replaced old adopted Gap contents", err)
	}
	database, err := sql.Open("pgx", restored.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err = database.Exec(`ALTER DATABASE ` + pgx.Identifier{name}.Sanitize() + ` SET default_transaction_read_only=on`); err != nil {
		t.Fatal(err)
	}
	readonly, err := sql.Open("pgx", restored.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ValidateFindingAssessmentRestore(ctx, readonly); err != nil {
		t.Fatal(err)
	}
	readonly.Close()
	if _, err = database.Exec(`ALTER DATABASE ` + pgx.Identifier{name}.Sanitize() + ` RESET default_transaction_read_only`); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`UPDATE case_finding_assessment_actions SET receipt=jsonb_set(receipt,'{basis,adopted_gaps,0,note}','"TAMPERED"')`); err != nil {
		t.Fatal(err)
	}
	if err = db.ValidateFindingAssessmentRestore(ctx, database); err == nil {
		t.Fatal("restore accepted tampered historical Gap snapshot")
	}
	t.Logf("Native backup/new-database restore (%d bytes), production roles, creation replay, v3 exact bytes, old-versus-current Gap contents, side-effect-free checks and tamper rejection passed", backup.SizeBytes)
}
