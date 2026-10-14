package hostctl

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/testsourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/jackc/pgx/v5"
)

func TestSourceFactsGenerateDevelopmentSchema(t *testing.T) {
	output := os.Getenv("XINSIGHT_A1_SCHEMA_OUTPUT")
	if output == "" {
		t.Skip("explicit schema generation output required")
	}
	if !filepath.IsAbs(output) {
		t.Fatal("absolute output required")
	}
	ctx, c := connectorDigestDatabase(t)
	m := PostgresManager{Runner: ExecRunner{}}
	if e := m.EnsureRoles(ctx, c); e != nil {
		t.Fatal(e)
	}
	if _, e := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); e != nil {
		t.Fatal(e)
	}
	if e := m.ApplyGrants(ctx, c); e != nil {
		t.Fatal(e)
	}
	a, e := sql.Open("pgx", c.AdminDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	manifest, e := GenerateCleanBaselineSchemaContract(ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(manifest)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(output, append(raw, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	t.Logf("generated schema%s from actual new scoped native PG database", manifest.DatabaseSchema)
}

func TestSourceFactsNativeUpgradeRestore(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if e := manager.EnsureRoles(ctx, c); e != nil {
		t.Fatal(e)
	}
	if _, e := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, 28)); e != nil {
		t.Fatal(e)
	}
	if e := manager.ApplyGrants(ctx, c); e != nil {
		t.Fatal(e)
	}
	admin, e := sql.Open("pgx", c.AdminDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	source := testsourcefacts.RegisterSource(t, admin)
	bundle := testsourcefacts.Load(t, testsourcefacts.Chat)
	media := testsourcefacts.Load(t, testsourcefacts.Media)
	testsourcefacts.PersistMoney(t, ctx, c.ImporterDSN, source, bundle)
	subject := "xs_a1a1a1a1a1a1a1a1"
	if _, e = admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences) VALUES($1,$2::uuid,true,true,true,true)`, subject, source); e != nil {
		t.Fatal(e)
	}
	store, e := db.OpenWorkspaceStore(ctx, c.WorkspaceDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	periodBundle := bundle
	periodBundle.Pages = append(append([]testsourcefacts.CapturedPage{}, bundle.Pages...), media.Pages...)
	first, last := testsourcefacts.RetainedPeriod(t, periodBundle)
	request := workbench.ReadingRequest{ContractVersion: workbench.ContractVersion, MetricVersion: workbench.MetricVersion, Page: "cost", MoneyBasis: workbench.MoneyGatewayCharged, Currency: "CNY", Period: workbench.PeriodInput{StartInclusive: &first, EndExclusive: &last, DisplayTimezone: "UTC"}, Filters: workbench.Filters{SourceIDs: []string{source}}}
	old, e := store.CreateWorkbenchReading(ctx, subject, request, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	old, e = store.GetWorkbenchReading(ctx, subject, old.ID, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); e != nil {
		t.Fatal(e)
	}
	if e = manager.ApplyGrants(ctx, c); e != nil {
		t.Fatal(e)
	}
	if e = manager.VerifyRoleBoundaries(ctx, c); e != nil {
		t.Fatal(e)
	}
	after, e := store.GetWorkbenchReading(ctx, subject, old.ID, time.Now())
	if e != nil || !reflect.DeepEqual(old, after) {
		t.Fatal("schema extension invalidated old reading", e)
	}
	var oldStored string
	if e = admin.QueryRowContext(ctx, `SELECT to_jsonb(r)::text FROM workbench_readings r WHERE id=$1`, old.ID).Scan(&oldStored); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec(`UPDATE workbench_access_grants SET allow_source_context=true,allow_source_identity=true,allow_source_cost=true WHERE subject_id=$1`, subject); e != nil {
		t.Fatal(e)
	}
	if _, e = store.GetWorkbenchReading(ctx, subject, old.ID, time.Now()); !errors.Is(e, workbench.ErrAccessChanged) {
		t.Fatal("changed authorization served an earlier reading", e)
	}
	testsourcefacts.Replay(t, admin, c.ImporterDSN, source, bundle, "1") // I-01 arrives before profiles
	testsourcefacts.Replay(t, admin, c.ImporterDSN, source, media, "1")
	current, e := store.CreateWorkbenchReading(ctx, subject, request, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	current, e = store.GetWorkbenchReading(ctx, subject, current.ID, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	sum, e := store.GetWorkbenchSummary(ctx, subject, current.ID, time.Now())
	if e != nil || sum.RequestCount != 1 || sum.SelectedAmountMicros != "700" || sum.SourceCoverage.MediaPending != 1 {
		t.Fatalf("media/economic isolation %+v %v", sum, e)
	}
	// Persist through the real importer grant, then freeze collection pause and
	// failure alongside an earlier healthy A1 reading in the native backup.
	collector, e := db.OpenSourceFactsStore(c.ImporterDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer collector.Close()
	var streamID string
	if e = admin.QueryRow(`SELECT id::text FROM source_fact_streams WHERE source_system_id=$1::uuid AND profile=$2 ORDER BY id LIMIT 1`, source, sourcefacts.IdentityProfile).Scan(&streamID); e != nil {
		t.Fatal(e)
	}
	stream, e := collector.LoadSourceFactsStream(ctx, streamID)
	if e != nil {
		t.Fatal(e)
	}
	if e = collector.RecordSourceFactsFailure(ctx, stream, "source_fact_conflict", time.Now()); e != nil {
		t.Fatal("importer failure write", e)
	}
	if _, e = admin.Exec(`UPDATE source_fact_streams SET revision=revision+1,status='paused' WHERE id=$1::uuid`, streamID); e != nil {
		t.Fatal(e)
	}
	stopped, e := store.CreateWorkbenchReading(ctx, subject, request, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	stopped, e = store.GetWorkbenchReading(ctx, subject, stopped.ID, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if !slices.Contains(stopped.SourceCoverage.CollectionStates, "paused") || !slices.Contains(stopped.SourceCoverage.Gaps, "source_fact_conflict") {
		t.Fatal("native stopped state missing")
	}
	for _, dsn := range []string{c.ImporterDSN, c.WorkspaceDSN, c.ReaderDSN} {
		runtime, e := sql.Open("pgx", dsn)
		if e != nil {
			t.Fatal(e)
		}
		for _, query := range []string{`UPDATE source_fact_documents SET revision=revision WHERE false`, `DELETE FROM workbench_source_members WHERE false`, `UPDATE workbench_source_manifests SET manifest_sha256=manifest_sha256 WHERE false`, `UPDATE workbench_access_grants SET allow_source_identity=true WHERE false`, `UPDATE source_fact_streams SET revision=revision+1,status='active',requires_attention=false,last_error='' WHERE false`} {
			if _, e = runtime.Exec(query); e == nil {
				t.Fatal("runtime crossed role/immutable boundary")
			}
		}
		runtime.Close()
	}
	if e = db.ValidateWorkbenchRestore(ctx, admin); e != nil {
		t.Fatal(e)
	}
	before, e := manager.PreviewCleanBaseline(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	// Preview and check are read-only; repeat inspection must match exactly.
	if e = db.ValidateWorkbenchRestore(ctx, admin); e != nil {
		t.Fatal(e)
	}
	backup, e := manager.Backup(ctx, c, filepath.Join(t.TempDir(), "source-facts.dump"))
	if e != nil {
		t.Fatal(e)
	}
	f := newInstallFixture(t)
	c = digestSyntheticCredentials(t, f, c)
	name := fmt.Sprintf("xinsight_restore_a1_%d", time.Now().UnixNano())
	restored, e := StageRestoredCredentialSet(f.paths, c, name)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		a, e := sql.Open("pgx", c.AdminDSN)
		if e != nil {
			t.Error(e)
			return
		}
		defer a.Close()
		if _, e = a.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"); e != nil {
			t.Error(e)
		}
	})
	expected, e := db.LoadMigrationChecksums("../../migrations")
	if e != nil {
		t.Fatal(e)
	}
	if e = manager.RestoreToNewDatabase(ctx, c, restored, backup.Path, "../../migrations", expected); e != nil {
		t.Fatal(e)
	}
	if e = manager.VerifyRoleBoundaries(ctx, restored); e != nil {
		t.Fatal(e)
	}
	recovered, e := db.OpenWorkspaceStore(ctx, restored.WorkspaceDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	if _, e = recovered.GetWorkbenchReading(ctx, subject, old.ID, time.Now()); !errors.Is(e, workbench.ErrAccessChanged) {
		t.Fatal("restore lost the current authorization boundary", e)
	}
	restoredAdmin, e := sql.Open("pgx", restored.AdminDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer restoredAdmin.Close()
	for _, database := range []*sql.DB{admin, restoredAdmin} {
		var stored string
		if e = database.QueryRowContext(ctx, `SELECT to_jsonb(r)::text FROM workbench_readings r WHERE id=$1`, old.ID).Scan(&stored); e != nil || stored != oldStored {
			t.Fatal("grant change or restore rewrote the earlier reading", e)
		}
	}
	for _, r := range []workbench.Reading{current, stopped} {
		got, e := recovered.GetWorkbenchReading(ctx, subject, r.ID, time.Now())
		if e != nil || !reflect.DeepEqual(got, r) {
			t.Fatal("restored reading differs", e)
		}
	}
	got, e := recovered.GetWorkbenchSummary(ctx, subject, current.ID, time.Now())
	if e != nil || !reflect.DeepEqual(sum, got) {
		t.Fatal("restored summary differs", e)
	}
	final, e := manager.PreviewCleanBaseline(ctx, c)
	if e != nil || !reflect.DeepEqual(before, final) {
		t.Fatal("preview/check/backup/restore modified source", e)
	}
	// Roll back corruptions of this disposable fixture. Validate on the same
	// connection to prove native restore checks reject these states.
	for _, mutation := range []string{`ALTER TABLE source_fact_documents DISABLE TRIGGER source_fact_documents_immutable`, `ALTER TABLE workbench_source_manifests DISABLE TRIGGER workbench_source_manifests_immutable; UPDATE workbench_source_manifests SET manifest_sha256='sha256:'||repeat('0',64); ALTER TABLE workbench_source_manifests ENABLE ALWAYS TRIGGER workbench_source_manifests_immutable`, `ALTER TABLE source_fact_documents DISABLE TRIGGER source_fact_documents_immutable; UPDATE source_fact_documents SET org_id='999'; ALTER TABLE source_fact_documents ENABLE ALWAYS TRIGGER source_fact_documents_immutable`} {
		tx, e := admin.BeginTx(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.ExecContext(ctx, mutation); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
		e = db.ValidateWorkbenchRestore(ctx, tx)
		tx.Rollback()
		if e == nil {
			t.Fatal("corrupt restore accepted")
		}
	}
	t.Log("schema28→current default-deny preserves A0 reading; actual importer/workspace/reader roles; I01-first + captured chat/media; native pg_dump/isolated RestoreToNewDatabase; immutable source/reading recovery; preview/check/source nonmutation; corrupt guards/bindings rejected")
}
