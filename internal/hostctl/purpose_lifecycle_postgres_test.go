package hostctl

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/testsourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5"
)

func TestPurposeSchema30To31NativeRestore(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, 30)); err != nil {
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
	source := testsourcefacts.RegisterSource(t, admin)
	bundle := testsourcefacts.Load(t, testsourcefacts.Chat)
	testsourcefacts.PersistMoney(t, ctx, c.ImporterDSN, source, bundle)
	// Seed the genuine old schema from retained validated pages. The managed
	// schema32 importer deliberately cannot run against a schema30 database.
	testsourcefacts.SeedPreManaged(t, admin, source, bundle, "1")
	const subject = "xs_1234567890abcdef"
	if _, err = admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences,allow_source_context,allow_source_identity,allow_source_cost) VALUES($1,$2,true,true,true,true,true,true,true)`, subject, source); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenWorkspaceStore(ctx, c.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reader, err := db.OpenEvidenceStore(ctx, c.ReaderDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var execution, event, hash string
	var occurred time.Time
	if err = admin.QueryRow(`SELECT execution_id,evidence_event_id::text,event_hash,occurred_at FROM workbench_economic_facts WHERE source_system_id=$1 ORDER BY id LIMIT 1`, source).Scan(&execution, &event, &hash, &occurred); err != nil {
		t.Fatal(err)
	}
	start, end := occurred.Add(-time.Hour), occurred.Add(time.Hour)
	request := workbench.ReadingRequest{ContractVersion: workbench.ContractVersion, MetricVersion: workbench.MetricVersion, Page: "cost", MoneyBasis: workbench.MoneyGatewayCharged, Currency: "CNY", Period: workbench.PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "UTC"}, Filters: workbench.Filters{SourceIDs: []string{source}}}
	old, err := store.CreateWorkbenchReading(ctx, subject, request, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	old, err = store.GetWorkbenchReading(ctx, subject, old.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := store.CreateCase(ctx, workspace.Actor{Kind: "human", Ref: subject}, workspace.CreateCaseInput{Title: "Retained schema30 case"}, time.Now(), nil)
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
	after, err := store.GetWorkbenchReading(ctx, subject, old.ID, time.Now())
	if err != nil || !reflect.DeepEqual(old, after) {
		t.Fatal("default-false migration changed old A1 reading", err)
	}
	beforeScope, err := db.NewScopedWorkchainStore(reader, store).CurrentRevision(ctx, subject, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(`UPDATE workbench_access_grants SET allow_purpose_read=true,allow_purpose_review=true WHERE subject_id=$1`, subject); err != nil {
		t.Fatal(err)
	}
	afterScope, err := db.NewScopedWorkchainStore(reader, store).CurrentRevision(ctx, subject, false)
	if err != nil || beforeScope == afterScope {
		t.Fatal("new purpose grant did not change scope", err)
	}
	if _, err = store.GetWorkbenchReading(ctx, subject, old.ID, time.Now()); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("grant change retained old reading", err)
	}
	apply := func(s *db.WorkspaceStore, r *db.EvidenceStore, command workbench.PurposeCommand) workbench.PurposeEvent {
		t.Helper()
		local, finish, e := db.NewScopedWorkchainStore(r, s).Begin(ctx, subject, false, func(f func() error) error { return f() })
		if e != nil {
			t.Fatal(e)
		}
		defer finish()
		workchain.FromContext(local).PurposeReviewer = true
		value, e := s.ApplyWorkbenchPurpose(local, subject, execution, command, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		return value
	}
	decl := workbench.PurposeCommand{Action: "declare", ExpectedRevision: 0, IdempotencyKey: "restore-declare", Category: "software_check", Vocabulary: workbench.PurposeVocabulary, Basis: []workbench.PurposeBasis{{EvidenceID: event, EventHash: hash}}, Reason: "Original purpose statement"}
	created := apply(store, reader, decl)
	accepted := apply(store, reader, workbench.PurposeCommand{Action: "accept", ExpectedRevision: 1, IdempotencyKey: "restore-adopt", ClaimID: created.NewClaimID, Reason: "Reviewed request"})
	reading, err := store.CreateWorkbenchReading(ctx, subject, request, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	withdraw := workbench.PurposeCommand{Action: "withdraw_adoption", ExpectedRevision: 2, IdempotencyKey: "restore-withdraw", ClaimID: accepted.State.AdoptedClaim, Reason: "Withdraw without fallback"}
	withdrawn := apply(store, reader, withdraw)
	current, err := store.GetWorkbenchPurpose(ctx, subject, execution, "", 0, time.Now())
	if err != nil || current.State.Status != "unknown" {
		t.Fatal("withdraw state", err)
	}
	frozen, err := store.GetWorkbenchPurpose(ctx, subject, execution, reading.ID, 0, time.Now())
	if err != nil || frozen.State.Status != "adopted" {
		t.Fatal("frozen state", err)
	}
	if err = db.ValidateWorkbenchRestore(ctx, admin); err != nil {
		t.Fatal(err)
	}
	preview, err := manager.PreviewCleanBaseline(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := manager.Backup(ctx, c, filepath.Join(t.TempDir(), "purpose31.dump"))
	if err != nil {
		t.Fatal(err)
	}
	fixture := newInstallFixture(t)
	c = digestSyntheticCredentials(t, fixture, c)
	name := fmt.Sprintf("xinsight_restore_purpose_%d", time.Now().UnixNano())
	restored, err := StageRestoredCredentialSet(fixture.paths, c, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, e := sql.Open("pgx", c.AdminDSN)
		if e != nil {
			t.Error(e)
			return
		}
		defer cleanup.Close()
		if _, e = cleanup.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"); e != nil {
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
	actual, err := recovered.GetWorkbenchPurpose(ctx, subject, execution, "", 0, time.Now())
	if err != nil || !reflect.DeepEqual(current, actual) {
		t.Fatal("current purpose changed after restore", err)
	}
	actual, err = recovered.GetWorkbenchPurpose(ctx, subject, execution, reading.ID, 0, time.Now())
	if err != nil || !reflect.DeepEqual(frozen, actual) {
		t.Fatal("frozen purpose changed after restore", err)
	}
	if replay := apply(recovered, recoveredReader, withdraw); !reflect.DeepEqual(withdrawn, replay) {
		t.Fatal("native restored retry changed")
	}
	if _, err = recovered.GetCase(ctx, legacy.ID); err != nil {
		t.Fatal("old case lost", err)
	}
	if _, err = recovered.GetWorkbenchReading(ctx, subject, old.ID, time.Now()); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("old grant-bound reading incorrectly revived", err)
	}
	check, err := manager.PreviewCleanBaseline(ctx, c)
	if err != nil || !reflect.DeepEqual(preview, check) {
		t.Fatal("preview/check/backup changed original database", err)
	}
	t.Log("native schema30→31 with default-false old reading compatibility, formal roles, grant revision, declaration/adoption/withdrawal, separate pg_dump/pg_restore recovery, immutable retry and no-effect preview verified")
}
