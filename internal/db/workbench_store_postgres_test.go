package db_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestWorkbenchSchema24BackfillsQualifiedV1Evidence(t *testing.T) {
	for _, scenario := range []string{"single", "duplicate", "conflict"} {
		t.Run(scenario, func(t *testing.T) { testWorkbenchSchema24Backfill(t, scenario) })
	}
}

func testWorkbenchSchema24Backfill(t *testing.T, scenario string) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := workspaceTestDSN(t, ctx, baseDSN)
	legacyDir := t.TempDir()
	entries, err := os.ReadDir(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") || entry.Name() >= "000024_" {
			continue
		}
		payload, err := os.ReadFile(filepath.Join("..", "..", "migrations", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacyDir, entry.Name()), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if applied, err := db.RunMigrations(ctx, dsn, legacyDir); err != nil || applied != 23 {
		t.Fatalf("legacy migrations applied=%d err=%v", applied, err)
	}
	admin := openTestSQL(t, dsn)
	defer admin.Close()
	sourceSystemID := testUUID(t)
	result := mapWorkbenchFixture(t, sourceSystemID, 0, "17")
	event := result.Events[0]
	if _, err := admin.ExecContext(ctx, `
INSERT INTO source_systems(id,source_key,source_type,display_name,source_version,status)
VALUES($1::uuid,$2,'unirouter','UniRouter','unirouter.completed-execution.v1','active')`, sourceSystemID, "source-system:"+sourceSystemID); err != nil {
		t.Fatal(err)
	}
	insertWorkbenchHistoricalEvidence(t, ctx, admin, event, sourceSystemID, result.RequestID)
	if scenario != "single" {
		historicalAdapter := testUUID(t)
		if _, err := admin.ExecContext(ctx, `INSERT INTO adapter_manifests
          SELECT (jsonb_populate_record(NULL::adapter_manifests, to_jsonb(a) ||
           jsonb_build_object('id',$2::uuid,'adapter_id','historical-workbench-test'))).*
          FROM adapter_manifests a WHERE id=$1::uuid`, event["adapter_manifest_id"], historicalAdapter); err != nil {
			t.Fatal(err)
		}
		sourceHash := event["source_event_hash"].(string)
		if scenario == "conflict" {
			sourceHash = "sha256:" + strings.Repeat("f", 64)
		}
		if _, err := admin.ExecContext(ctx, `INSERT INTO evidence_events
   SELECT (jsonb_populate_record(NULL::evidence_events, to_jsonb(e) ||
    jsonb_build_object('id',gen_random_uuid(),'adapter_manifest_id',$3::uuid,
    'idempotency_key','historical-second-adapter','source_event_hash',$2::text))).*
   FROM evidence_events e WHERE id=$1::uuid`, event["id"], sourceHash, historicalAdapter); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "conflict" {
		if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err == nil {
			t.Fatal("conflicting historical facts accepted")
		}
		var present bool
		if err := admin.QueryRowContext(ctx, `SELECT to_regclass('workbench_economic_facts') IS NOT NULL`).Scan(&present); err != nil || present {
			t.Fatalf("failed migration left table: %v %v", present, err)
		}
		var n int
		if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM evidence_events`).Scan(&n); err != nil || n != 2 {
			t.Fatalf("historical evidence changed: %d %v", n, err)
		}
		return
	}
	checksums, err := db.LoadMigrationChecksums(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil || applied != len(checksums)-23 {
		t.Fatalf("forward migrations applied=%d want=%d err=%v", applied, len(checksums)-23, err)
	}
	var factCount int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM workbench_economic_facts`).Scan(&factCount); err != nil || factCount != 1 {
		t.Fatalf("backfill count=%d err=%v", factCount, err)
	}
	var requestID, charged, calculated string
	if err := admin.QueryRowContext(ctx, `SELECT request_id,gateway_charged_micros::text,calculated_reference_micros::text FROM workbench_economic_facts`).Scan(&requestID, &charged, &calculated); err != nil {
		t.Fatal(err)
	}
	if requestID != result.RequestID || charged != "17" || calculated != "17" {
		t.Fatalf("backfill request=%s charged=%s calculated=%s", requestID, charged, calculated)
	}
}

func TestWorkbenchProjectionReadingAuthorizationPaginationAndLateEvidence(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dsn := workspaceTestDSN(t, ctx, baseDSN)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	sourceSystemID := testUUID(t)
	evidenceStore, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer evidenceStore.Close()
	workspaceStore, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceStore.Close()
	admin := openTestSQL(t, dsn)
	defer admin.Close()

	amounts := []string{"17", "20", "30", "40", "50", "9999999999999999999"}
	wantTotal := new(big.Int)
	for index, amount := range amounts {
		wantTotal.Add(wantTotal, mustBigInt(t, amount))
		result := mapWorkbenchFixture(t, sourceSystemID, index, amount)
		report, persistErr := evidenceStore.PersistEvents(ctx, result.Events)
		if persistErr != nil || report.Inserted != 5 || report.CaptureGapsInserted != 4 {
			t.Fatalf("persist %d report=%+v err=%v", index, report, persistErr)
		}
		if index == 0 {
			replay, replayErr := evidenceStore.PersistEvents(ctx, result.Events)
			if replayErr != nil || replay.DuplicateNoop != 5 {
				t.Fatalf("replay report=%+v err=%v", replay, replayErr)
			}
		}
	}
	var facts int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM workbench_economic_facts`).Scan(&facts); err != nil || facts != len(amounts) {
		t.Fatalf("projected facts=%d err=%v", facts, err)
	}

	subject := "xs_8899aabbccddeeff"
	for index := 0; index < 7; index++ {
		if _, err := admin.ExecContext(ctx, `
INSERT INTO workbench_access_grants(
  subject_id,source_system_id,source_project_ref,
  allow_summary,allow_details,allow_coverage,allow_preferences
) VALUES($1,$2::uuid,$3,true,true,true,true)`, subject, sourceSystemID, "project-"+big.NewInt(int64(202+index)).String()); err != nil {
			t.Fatalf("insert grant %d: %v", index, err)
		}
	}

	now := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	request := workbench.ReadingRequest{
		ContractVersion: workbench.ContractVersion,
		Page:            "cost",
		MetricVersion:   workbench.MetricVersion,
		Period: workbench.PeriodInput{
			StartInclusive:  timePointer(time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)),
			EndExclusive:    timePointer(now),
			DisplayTimezone: "UTC",
		},
		MoneyBasis: workbench.MoneyGatewayCharged,
		Currency:   "CNY",
		Filters:    workbench.Filters{SourceIDs: []string{}, SourceProjects: []string{}, CostProjects: []string{}},
	}
	reading, err := workspaceStore.CreateWorkbenchReading(ctx, subject, request, now)
	if err != nil {
		t.Fatal(err)
	}
	if reading.Coverage.PeriodCompleteness != "unknown" || !reading.Coverage.ProjectionCompleteForMembers || reading.Coverage.KnownItems != 6 || reading.Coverage.Status != "scope_limited_unknown" {
		t.Fatalf("coverage=%+v", reading.Coverage)
	}
	summary, err := workspaceStore.GetWorkbenchSummary(ctx, subject, reading.ID, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if summary.RequestCount != 6 || summary.SelectedAmountMicros != wantTotal.String() || summary.AmountComplete || summary.RequestTotalComplete {
		t.Fatalf("summary=%+v want=%s", summary, wantTotal)
	}

	top, err := workspaceStore.GetWorkbenchContributions(ctx, subject, reading.ID, "source_project", 0, 2, "", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if top.Mode != "top_n" || len(top.Rows) != 2 || top.AuthorizedGroupCount != 6 || top.DenominatorMicros != wantTotal.String() || top.UnassignedMicros != "0" || top.UnassignedScope != "reading_constant" || top.OtherKnownMicros == "0" {
		t.Fatalf("top contributions=%+v", top)
	}

	page, err := workspaceStore.GetWorkbenchContributions(ctx, subject, reading.ID, "source_project", 2, 0, "", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	pageTotal := new(big.Int)
	pages := 0
	for {
		pages++
		if page.Mode != "full" || page.DenominatorMicros != wantTotal.String() || page.UnassignedMicros != "0" || page.UnassignedScope != "reading_constant" || page.OtherKnownMicros != "0" {
			t.Fatalf("full page %d=%+v", pages, page)
		}
		for _, row := range page.Rows {
			pageTotal.Add(pageTotal, mustBigInt(t, row.SelectedAmountMicros))
		}
		if page.NextCursor == "" {
			break
		}
		page, err = workspaceStore.GetWorkbenchContributions(ctx, subject, reading.ID, "source_project", 2, 0, page.NextCursor, now.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
	}
	if pages != 3 || pageTotal.String() != wantTotal.String() {
		t.Fatalf("full pages=%d total=%s want=%s", pages, pageTotal, wantTotal)
	}

	unassigned, err := workspaceStore.GetWorkbenchContributions(ctx, subject, reading.ID, "cost_project", 0, 5, "", now.Add(time.Minute))
	if err != nil || len(unassigned.Rows) != 0 || unassigned.UnassignedMicros != wantTotal.String() || unassigned.OtherKnownMicros != "0" {
		t.Fatalf("unassigned=%+v err=%v", unassigned, err)
	}
	details, err := workspaceStore.GetWorkbenchDetails(ctx, subject, reading.ID, top.Rows[0].Ref, 10, "", now.Add(time.Minute))
	if err != nil || len(details.Rows) != 1 || details.Rows[0].GatewayChargedMicros != amounts[len(amounts)-1] || details.Rows[0].AccountState != "unknown" {
		t.Fatalf("details=%+v err=%v", details, err)
	}
	sources, err := workspaceStore.GetWorkbenchSourceStatuses(ctx, subject, reading.ID, now.Add(time.Minute))
	if err != nil || len(sources.Items) != 1 || sources.Items[0].Status != "scope_limited_unknown" || sources.Items[0].ScopeLabel != "project_restricted" || sources.Items[0].CaptureStartedAt != nil || sources.Items[0].ReceivedWatermark != "" {
		t.Fatalf("sources=%+v err=%v", sources, err)
	}
	series, err := workspaceStore.GetWorkbenchSeries(ctx, subject, reading.ID, "amount", 3600, now.Add(time.Minute))
	if err != nil || len(series.Points) != 24 || series.Points[4].Complete {
		t.Fatalf("series points=%d err=%v series=%+v", len(series.Points), err, series)
	}

	restrictedSubject := "xs_7766554433221100"
	if _, err := admin.ExecContext(ctx, `
INSERT INTO workbench_access_grants(
  subject_id,source_system_id,source_project_ref,allow_summary
) VALUES($1,$2::uuid,'project-202',true)`, restrictedSubject, sourceSystemID); err != nil {
		t.Fatal(err)
	}
	restrictedReading, err := workspaceStore.CreateWorkbenchReading(ctx, restrictedSubject, request, now)
	if err != nil {
		t.Fatal(err)
	}
	restrictedSummary, err := workspaceStore.GetWorkbenchSummary(ctx, restrictedSubject, restrictedReading.ID, now.Add(time.Minute))
	if err != nil || restrictedSummary.RequestCount != 1 || restrictedSummary.SelectedAmountMicros != "17" {
		t.Fatalf("restricted summary=%+v err=%v", restrictedSummary, err)
	}
	restrictedContributions, err := workspaceStore.GetWorkbenchContributions(ctx, restrictedSubject, restrictedReading.ID, "source_project", 0, 5, "", now.Add(time.Minute))
	if err != nil || len(restrictedContributions.Rows) != 1 {
		t.Fatalf("restricted contributions=%+v err=%v", restrictedContributions, err)
	}
	if _, err := workspaceStore.GetWorkbenchDetails(ctx, restrictedSubject, restrictedReading.ID, restrictedContributions.Rows[0].Ref, 10, "", now.Add(time.Minute)); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatalf("summary-only detail error=%v", err)
	}
	deniedRequest := request
	deniedRequest.Filters.SourceProjects = []string{"project-203"}
	if _, err := workspaceStore.CreateWorkbenchReading(ctx, restrictedSubject, deniedRequest, now); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatalf("cross-project reading error=%v", err)
	}

	late := mapWorkbenchFixture(t, sourceSystemID, 6, "60")
	if _, err := evidenceStore.PersistEvents(ctx, late.Events); err != nil {
		t.Fatal(err)
	}
	fixed, err := workspaceStore.GetWorkbenchSummary(ctx, subject, reading.ID, now.Add(2*time.Minute))
	if err != nil || fixed.RequestCount != 6 || fixed.SelectedAmountMicros != wantTotal.String() {
		t.Fatalf("fixed summary=%+v err=%v", fixed, err)
	}
	newReading, err := workspaceStore.CreateWorkbenchReading(ctx, subject, request, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	newSummary, err := workspaceStore.GetWorkbenchSummary(ctx, subject, newReading.ID, now.Add(3*time.Minute))
	if err != nil || newSummary.RequestCount != 7 {
		t.Fatalf("new summary=%+v err=%v", newSummary, err)
	}

	t.Run("backup_restore_preserves_reading_and_current_authorization", func(t *testing.T) {
		bin := os.Getenv("XINSIGHT_PG_BIN")
		if bin == "" {
			t.Skip("XINSIGHT_PG_BIN required for actual PG16 backup/restore")
		}
		dump := filepath.Join(t.TempDir(), "reading.dump")
		run := func(program, dsn string, args ...string) error {
			u, err := url.Parse(dsn)
			if err != nil {
				return err
			}
			cmd := exec.CommandContext(ctx, filepath.Join(bin, program), args...)
			cmd.Env = append(os.Environ(), "PGHOST="+u.Hostname(), "PGPORT="+u.Port(), "PGDATABASE="+strings.TrimPrefix(u.Path, "/"), "PGUSER="+u.User.Username())
			if password, ok := u.User.Password(); ok {
				cmd.Env = append(cmd.Env, "PGPASSWORD="+password)
			}
			output, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("%s: %w: %s", program, err, output)
			}
			return nil
		}
		if err := run("pg_dump", dsn, "--format=custom", "--file", dump); err != nil {
			t.Fatal(err)
		}
		restoredDSN := workspaceTestDSN(t, ctx, baseDSN)
		if err := run("pg_restore", restoredDSN, "--exit-on-error", "--no-owner", "--dbname", strings.TrimPrefix(mustParseWorkbenchURL(t, restoredDSN).Path, "/"), dump); err != nil {
			t.Fatal(err)
		}
		restored, err := db.OpenWorkspaceStore(ctx, restoredDSN)
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close()
		got, err := restored.GetWorkbenchSummary(ctx, subject, reading.ID, now.Add(3*time.Minute))
		if err != nil || got.SelectedAmountMicros != fixed.SelectedAmountMicros || got.RequestCount != 6 {
			t.Fatalf("restored old reading=%+v err=%v", got, err)
		}
		gotReading, err := restored.GetWorkbenchReading(ctx, subject, reading.ID, now.Add(3*time.Minute))
		if err != nil || gotReading.MembershipRef != reading.MembershipRef || gotReading.InterpretationRevision != reading.InterpretationRevision {
			t.Fatalf("restored identity=%+v err=%v", gotReading, err)
		}
		restoredAdmin := openTestSQL(t, restoredDSN)
		defer restoredAdmin.Close()
		for _, table := range []string{"evidence_events", "workbench_economic_facts", "workbench_readings", "workbench_reading_members", "workbench_access_grants"} {
			query := "SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM " + table + " t"
			var before, after string
			if err := admin.QueryRowContext(ctx, query).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if err := restoredAdmin.QueryRowContext(ctx, query).Scan(&after); err != nil || before != after {
				t.Fatalf("restored %s identity/content mismatch: %v", table, err)
			}
		}
		assertWorkbenchFactGuards(t, ctx, restoredAdmin)
		if err := db.ValidateWorkbenchRestore(ctx, restoredAdmin); err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			`UPDATE evidence_events SET event_hash='sha256:'||repeat('d',64) WHERE id IN (SELECT evidence_event_id FROM workbench_economic_facts)`,
			`DELETE FROM workbench_reading_members WHERE ordinal=1`,
		} {
			tx, err := restoredAdmin.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
			if err := db.ValidateWorkbenchRestore(ctx, tx); err == nil {
				t.Fatalf("corrupted restored binding accepted: %s", statement)
			}
			tx.Rollback()
		}

		if _, err := restoredAdmin.ExecContext(ctx, `UPDATE workbench_readings SET interpretation_revision='sha256:'||repeat('e',64) WHERE id=$1`, reading.ID); err != nil {
			t.Fatal(err)
		}
		if err := db.ValidateWorkbenchRestore(ctx, restoredAdmin); err == nil {
			t.Fatal("unsupported restored interpretation accepted")
		}
		if _, err := restoredAdmin.ExecContext(ctx, `UPDATE workbench_readings SET interpretation_revision=$2 WHERE id=$1`, reading.ID, reading.InterpretationRevision); err != nil {
			t.Fatal(err)
		}

		if _, err := restoredAdmin.ExecContext(ctx, `UPDATE workbench_access_grants SET active=false,revision=revision+1 WHERE subject_id=$1`, subject); err != nil {
			t.Fatal(err)
		}
		if _, err := restored.GetWorkbenchSummary(ctx, subject, reading.ID, now.Add(3*time.Minute)); !errors.Is(err, workbench.ErrAccessChanged) {
			t.Fatalf("restored revoke=%v", err)
		}
		broken := filepath.Join(t.TempDir(), "invalid.dump")
		if err := os.WriteFile(broken, []byte("not a PostgreSQL archive"), 0600); err != nil {
			t.Fatal(err)
		}
		negativeDSN := workspaceTestDSN(t, ctx, baseDSN)
		if err := run("pg_restore", negativeDSN, "--exit-on-error", "--dbname", strings.TrimPrefix(mustParseWorkbenchURL(t, negativeDSN).Path, "/"), broken); err == nil {
			t.Fatal("invalid restore accepted")
		}
	})

	if _, err := workspaceStore.GetWorkbenchSummary(ctx, "xs_0011223344556677", reading.ID, now.Add(time.Minute)); !errors.Is(err, workbench.ErrNotFound) {
		t.Fatalf("cross-subject read error=%v", err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE workbench_access_grants SET active=false,revision=revision+1,updated_at=now() WHERE subject_id=$1 AND source_project_ref='project-202'`, subject); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceStore.GetWorkbenchSummary(ctx, subject, reading.ID, now.Add(4*time.Minute)); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatalf("revoked reading error=%v", err)
	}
	if _, err := workspaceStore.GetWorkbenchSummary(ctx, subject, newReading.ID, newReading.ExpiresAt); !errors.Is(err, workbench.ErrExpired) {
		t.Fatalf("expired reading error=%v", err)
	}
}

func TestWorkbenchProjectionConflictRollsBackEvidenceTransaction(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := workspaceTestDSN(t, ctx, baseDSN)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	admin := openTestSQL(t, dsn)
	defer admin.Close()
	sourceSystemID := testUUID(t)
	first := mapWorkbenchFixture(t, sourceSystemID, 0, "17")
	second := mapWorkbenchFixture(t, sourceSystemID, 1, "23")
	if _, err := store.PersistEvents(ctx, first.Events); err != nil {
		t.Fatal(err)
	}
	secondSourceRef := second.Events[0]["source_ref"]
	if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_economic_facts SELECT (jsonb_populate_record(NULL::workbench_economic_facts,to_jsonb(f)||jsonb_build_object('id',gen_random_uuid(),'source_ref',$2::text))).* FROM workbench_economic_facts f WHERE source_system_id=$1::uuid`, sourceSystemID, secondSourceRef); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PersistEvents(ctx, second.Events); err == nil || !strings.Contains(err.Error(), "identity conflicts with immutable source content") {
		t.Fatalf("projection conflict error=%v", err)
	}
	var evidenceRows int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM evidence_events WHERE source_system_id=$1::uuid AND source_version='unirouter.completed-execution.v1' AND source_ref=$2`, sourceSystemID, secondSourceRef).Scan(&evidenceRows); err != nil {
		t.Fatal(err)
	}
	if evidenceRows != 0 {
		t.Fatalf("conflicting Evidence escaped transaction rollback: rows=%d", evidenceRows)
	}
}

func TestWorkbenchProjectionRejectsAdapterIdentityDriftAndKeepsSourcesDistinct(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := workspaceTestDSN(t, ctx, baseDSN)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	admin := openTestSQL(t, dsn)
	defer admin.Close()

	firstSource := testUUID(t)
	first := mapWorkbenchFixture(t, firstSource, 0, "17")
	if _, err := store.PersistEvents(ctx, first.Events); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(first.Events)
	if err != nil {
		t.Fatal(err)
	}
	var adapterDrift []map[string]any
	if err := json.Unmarshal(encoded, &adapterDrift); err != nil {
		t.Fatal(err)
	}
	for _, event := range adapterDrift {
		event["adapter_manifest_id"] = testUUID(t)
	}
	if _, err := store.PersistEvents(ctx, adapterDrift); err == nil || !strings.Contains(err.Error(), "conflicts with reserved UniRouter mapper version") {
		t.Fatalf("adapter identity drift error=%v", err)
	}
	secondSource := testUUID(t)
	second := mapWorkbenchFixture(t, secondSource, 0, "17")
	if first.RequestID != second.RequestID {
		t.Fatalf("fixture requests differ: %s %s", first.RequestID, second.RequestID)
	}
	if _, err := store.PersistEvents(ctx, second.Events); err != nil {
		t.Fatal(err)
	}

	var facts, sources, requests int
	if err := admin.QueryRowContext(ctx, `
SELECT count(*),count(DISTINCT source_system_id),count(DISTINCT request_id)
FROM workbench_economic_facts`).Scan(&facts, &sources, &requests); err != nil {
		t.Fatal(err)
	}
	if facts != 2 || sources != 2 || requests != 1 {
		t.Fatalf("facts=%d sources=%d requests=%d", facts, sources, requests)
	}
}

func TestWorkbenchSavedFilterOperationIdempotencyAndRevision(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := workspaceTestDSN(t, ctx, baseDSN)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	admin := openTestSQL(t, dsn)
	defer admin.Close()
	sourceSystemID := testUUID(t)
	if _, err := admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name,source_version,status) VALUES($1::uuid,$2,'unirouter','UniRouter','unirouter.completed-execution.v1','active')`, sourceSystemID, "source-system:"+sourceSystemID); err != nil {
		t.Fatal(err)
	}
	subject := "xs_8899aabbccddeeff"
	if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_preferences) VALUES($1,$2::uuid,true,true)`, subject, sourceSystemID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	command := workbench.SavedFilterCommand{
		Name: "Current cost", ExpectedRevision: 0, IdempotencyKey: "create:stable-1",
		Config: relativeWorkbenchRequest(),
	}
	items := make([]workbench.SavedFilter, 2)
	errs := make([]error, 2)
	var wait sync.WaitGroup
	for index := range items {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			items[index], errs[index] = store.CreateWorkbenchSavedFilter(ctx, subject, command, now)
		}(index)
	}
	wait.Wait()
	if errs[0] != nil || errs[1] != nil || items[0].ID == "" || items[0].ID != items[1].ID || items[0].Revision != 1 || items[1].Revision != 1 {
		t.Fatalf("create items=%+v errs=%v", items, errs)
	}
	conflict := command
	conflict.Name = "Different content"
	if _, err := store.CreateWorkbenchSavedFilter(ctx, subject, conflict, now); !errors.Is(err, workbench.ErrConflict) {
		t.Fatalf("idempotency conflict error=%v", err)
	}
	update := command
	update.Name = "Updated cost"
	update.ExpectedRevision = 1
	update.IdempotencyKey = "update:stable-1"
	first, err := store.UpdateWorkbenchSavedFilter(ctx, subject, items[0].ID, update, now.Add(time.Second))
	if err != nil || first.Revision != 2 {
		t.Fatalf("first update=%+v err=%v", first, err)
	}
	otherSubject := "xs_0011223344556677"
	if _, err := admin.ExecContext(ctx, `
INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_preferences)
VALUES($1,$2::uuid,true,true)`, otherSubject, sourceSystemID); err != nil {
		t.Fatal(err)
	}
	otherUpdate := update
	otherUpdate.IdempotencyKey = "update:other-owner-1"
	if _, err := store.UpdateWorkbenchSavedFilter(ctx, otherSubject, items[0].ID, otherUpdate, now.Add(time.Second)); !errors.Is(err, workbench.ErrNotFound) {
		t.Fatalf("cross-owner update error=%v", err)
	}
	replay, err := store.UpdateWorkbenchSavedFilter(ctx, subject, items[0].ID, update, now.Add(2*time.Second))
	if err != nil || replay.ID != first.ID || replay.Revision != first.Revision || !replay.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("replay=%+v first=%+v err=%v", replay, first, err)
	}
	stale := update
	stale.IdempotencyKey = "update:stale-2"
	if _, err := store.UpdateWorkbenchSavedFilter(ctx, subject, items[0].ID, stale, now.Add(3*time.Second)); !errors.Is(err, workbench.ErrStaleRevision) {
		t.Fatalf("stale error=%v", err)
	}
	historical, err := store.CreateWorkbenchSavedFilter(ctx, subject, command, now.Add(4*time.Second))
	if err != nil || historical.ID != items[0].ID || historical.Revision != 1 || historical.Name != command.Name {
		t.Fatalf("historical create replay=%+v err=%v", historical, err)
	}
	list, err := store.ListWorkbenchSavedFilters(ctx, subject)
	if err != nil || len(list.Items) != 1 || list.Items[0].Revision != 2 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}

func mapWorkbenchFixture(t *testing.T, sourceSystemID string, index int, amount string) unirouter.CompletedExecutionMapResult {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("..", "..", "contracts", "completed-execution", "v1", "fixtures", "valid-settled.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	value["request_id"] = "req-workbench-" + big.NewInt(int64(index+1)).String()
	value["project_id"] = float64(202 + index)
	value["terminal"].(map[string]any)["available_at"] = time.Date(2026, 7, 25, 4+index, 5, 6, 0, time.UTC).Format(time.RFC3339)
	value["usage"].(map[string]any)["model"] = "model-" + big.NewInt(int64(index+1)).String()
	value["money"].(map[string]any)["charged_cost_micros"] = amount
	value["money"].(map[string]any)["calculated_cost_micros"] = amount
	payload, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	result, err := unirouter.MapCompletedExecutionV1(payload, unirouter.CompletedExecutionMapOptions{
		SourceInstance:    "workbench-test",
		SourceSystemID:    sourceSystemID,
		AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID,
		ObservedAt:        time.Date(2026, 7, 25, 20, 0, 0, 0, time.UTC),
		IngestedAt:        time.Date(2026, 7, 25, 20, 0, 1, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func relativeWorkbenchRequest() workbench.ReadingRequest {
	return workbench.ReadingRequest{
		ContractVersion: workbench.ContractVersion,
		Page:            "cost",
		MetricVersion:   workbench.MetricVersion,
		Period:          workbench.PeriodInput{RelativeHours: 24, DisplayTimezone: "UTC"},
		MoneyBasis:      workbench.MoneyGatewayCharged,
		Currency:        "CNY",
		Filters:         workbench.Filters{SourceIDs: []string{}, SourceProjects: []string{}, CostProjects: []string{}},
	}
}

func mustBigInt(t *testing.T, value string) *big.Int {
	t.Helper()
	out, ok := new(big.Int).SetString(value, 10)
	if !ok {
		t.Fatalf("invalid integer %q", value)
	}
	return out
}

func timePointer(value time.Time) *time.Time { return &value }

func openTestSQL(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func marshalTestJSON(t *testing.T, value any) string {
	t.Helper()
	if value == nil {
		return "{}"
	}
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func mustParseWorkbenchURL(t *testing.T, dsn string) *url.URL {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestWorkbenchCoverageKeepsHistoryGapsAndConnectionStatesDistinct(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("PG required")
	}
	ctx := t.Context()
	dsn := workspaceTestDSN(t, ctx, base)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	evidence, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer evidence.Close()
	store, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	admin := openTestSQL(t, dsn)
	defer admin.Close()
	source := testUUID(t)
	mapped := mapWorkbenchFixture(t, source, 0, "17")
	if _, err := evidence.PersistEvents(ctx, mapped.Events); err != nil {
		t.Fatal(err)
	}
	subject := "xs_8899aabbccddeeff"
	if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_coverage) VALUES($1,$2::uuid,true,true)`, subject, source); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	request := relativeWorkbenchRequest()
	create := func() workbench.Reading {
		t.Helper()
		r, err := store.CreateWorkbenchReading(ctx, subject, request, now)
		if err != nil {
			t.Fatal(err)
		}
		if r.Coverage.PeriodCompleteness != "unknown" || r.Coverage.KnownItems != 1 {
			t.Fatalf("false completeness/count: %+v", r.Coverage)
		}
		return r
	}
	initial := create()
	if initial.Coverage.Status != "not_connected" {
		t.Fatalf("initial=%+v", initial.Coverage)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO adapter_import_states(source_system_id,adapter_manifest_id,cursor_key,activation_source_instance,activation_cursor,activation_high_watermark,activated_at,authorization_ref,cursor_value,high_watermark,worker_status,last_import_completed_at) VALUES($1::uuid,$2::uuid,'unirouter.completed-execution.v1','synthetic','opaque','opaque',$3,'synthetic-test','opaque','opaque','idle',$4)`, source, unirouter.DefaultCompletedExecutionAdapterManifestID, now.Add(-12*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	connected := create()
	if connected.Coverage.Status != "observed_with_gaps" || !containsWorkbenchGap(connected.Coverage.Gaps, "period_precedes_capture_start") || connected.Coverage.ReceivedWatermark != "opaque" {
		t.Fatalf("history hidden by profile gaps: %+v", connected.Coverage)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE adapter_import_states SET status='error',worker_status='error' WHERE source_system_id=$1::uuid`, source); err != nil {
		t.Fatal(err)
	}
	disconnected := create()
	if disconnected.Coverage.Status != "disconnected" {
		t.Fatalf("disconnected=%+v", disconnected.Coverage)
	}
	fixed, err := store.GetWorkbenchReading(ctx, subject, connected.ID, now.Add(time.Second))
	if err != nil || fixed.Coverage.Status != connected.Coverage.Status {
		t.Fatalf("fixed coverage drift=%+v err=%v", fixed, err)
	}
	statuses, err := store.GetWorkbenchSourceStatuses(ctx, subject, connected.ID, now.Add(time.Second))
	if err != nil || len(statuses.Items) != 1 || statuses.Items[0].Status != "disconnected" {
		t.Fatalf("live observation=%+v err=%v", statuses, err)
	}
}
func containsWorkbenchGap(gaps []string, want string) bool {
	for _, gap := range gaps {
		if gap == want {
			return true
		}
	}
	return false
}

func insertWorkbenchHistoricalEvidence(t *testing.T, ctx context.Context, admin *sql.DB, event map[string]any, sourceSystemID, requestID string) {
	t.Helper()
	if _, err := admin.ExecContext(ctx, `
INSERT INTO evidence_events(
 id,schema_version,source_system_id,adapter_manifest_id,source_type,source_version,
 source_event_type,source_ref,idempotency_key,event_type,stage,severity,capture_mode,
 occurred_at,observed_at,ingested_at,request_id,org_ref,project_ref,payload_capture_mode,
 event_hash,source_event_hash,correlation,identity,capability,decision,usage_data,outcome,
 payload_policy,field_provenance,raw_source_excerpt
) VALUES(
 $1::uuid,$2,$3::uuid,$4::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13,
 $14::timestamptz,$15::timestamptz,$16::timestamptz,$17,$18,$19,$20,
 $21,$22,$23::jsonb,$24::jsonb,$25::jsonb,$26::jsonb,$27::jsonb,$28::jsonb,
 $29::jsonb,$30::jsonb,$31::jsonb
	)`, event["id"], event["schema_version"], event["source_system_id"], event["adapter_manifest_id"],
		event["source_type"], event["source_version"], event["source_event_type"], event["source_ref"], event["idempotency_key"],
		event["event_type"], event["stage"], event["severity"], event["capture_mode"], event["occurred_at"], event["observed_at"],
		event["ingested_at"], event["request_id"], event["org_ref"], event["project_ref"], event["payload_capture_mode"],
		event["event_hash"], event["source_event_hash"], marshalTestJSON(t, event["correlation"]), marshalTestJSON(t, event["identity"]),
		marshalTestJSON(t, event["capability"]), marshalTestJSON(t, event["decision"]), marshalTestJSON(t, event["usage"]),
		marshalTestJSON(t, event["outcome"]), marshalTestJSON(t, event["payload_policy"]), marshalTestJSON(t, event["field_provenance"]),
		marshalTestJSON(t, event["raw_source_excerpt"])); err != nil {
		t.Fatal(err)
	}
	executionID, err := executionid.New(sourceSystemID, requestID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE evidence_events SET execution_id=$2 WHERE id=$1::uuid`, event["id"], executionID); err != nil {
		t.Fatal(err)
	}
}

// Exercises the actual projection and frozen-member queries in the established
// isolated-schema PG harness. It deliberately does not replace PG with a mock.
func TestWorkbenchContributionRequestOrderConservationAndCursors(t *testing.T) {
	f := newSourceFixture(t)
	payload, err := os.ReadFile("../../contracts/completed-execution/v1/fixtures/valid-settled.json")
	if err != nil {
		t.Fatal(err)
	}
	var sample map[string]any
	if err = json.Unmarshal(payload, &sample); err != nil {
		t.Fatal(err)
	}
	ingest := func(index int, model, charged, calculated string) {
		t.Helper()
		sample["request_id"] = fmt.Sprintf("req-ranked-%d", index)
		sample["usage"].(map[string]any)["model"] = model
		sample["money"].(map[string]any)["charged_cost_micros"] = charged
		sample["money"].(map[string]any)["calculated_cost_micros"] = calculated
		raw, marshalErr := json.Marshal(sample)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		mapped, mapErr := unirouter.MapCompletedExecutionV1(raw, unirouter.CompletedExecutionMapOptions{SourceInstance: "workbench-test", SourceSystemID: f.source, AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID, ObservedAt: time.Now().UTC(), IngestedAt: time.Now().UTC()})
		if mapErr != nil {
			t.Fatal(mapErr)
		}
		if _, persistErr := f.evidence.PersistEvents(f.ctx, mapped.Events); persistErr != nil {
			t.Fatal(persistErr)
		}
	}
	// Six known groups; count rank reverses amount rank and exercises stable ties.
	for i := 0; i < 8; i++ {
		ingest(i, "frequent", "0", "10")
	}
	ingest(8, "expensive", "1000", "1000")
	for i, model := range []string{"alpha", "beta", "delta", "gamma"} {
		ingest(9+i, model, "1", "1")
	}
	now := time.Now().UTC()
	request := relativeWorkbenchRequest()
	request.Period = workbench.PeriodInput{StartInclusive: timePointer(time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)), EndExclusive: timePointer(time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)), DisplayTimezone: "UTC"}
	reading, err := f.workspace.CreateWorkbenchReading(f.ctx, f.subject, request, now)
	if err != nil {
		t.Fatal(err)
	}
	// Recent rows are a global economic-member page, independent of the
	// contribution grouping and its amount/count sort.
	seenRecent := map[string]bool{}
	recentCursor := ""
	for {
		page, recentErr := f.workspace.GetWorkbenchRecentRequests(f.ctx, f.subject, reading.ID, "", 2, recentCursor, now)
		if recentErr != nil {
			t.Fatal(recentErr)
		}
		for _, item := range page.Items {
			if seenRecent[item.RequestID] || item.Runtime.MatchState != "unavailable" {
				t.Fatal("duplicate or invented runtime", item)
			}
			if item.AccountState != "unknown" || item.ApplicationState != "unknown" || item.ApplicationID != "" {
				t.Fatal("absent source identity became known", item)
			}
			seenRecent[item.RequestID] = true
		}
		if page.NextCursor == "" {
			break
		}
		recentCursor = page.NextCursor
	}
	if len(seenRecent) != 13 {
		t.Fatal("recent pagination dropped a member", len(seenRecent))
	}
	get := func(dimension, order string, limit, top int, cursor string) workbench.Contributions {
		t.Helper()
		result, e := f.workspace.GetWorkbenchContributionsOrdered(f.ctx, f.subject, reading.ID, dimension, order, limit, top, cursor, now)
		if e != nil {
			t.Fatal(e)
		}
		return result
	}
	byCount := get("model", "requests", 0, 5, "")
	byAmount, err := f.workspace.GetWorkbenchContributions(f.ctx, f.subject, reading.ID, "model", 0, 5, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if byCount.Rows[0].Label != "frequent" || byAmount.Rows[0].Label != "expensive" || byAmount.OrderBy != "amount" || byCount.AuthorizedGroupCount != 6 {
		t.Fatalf("rank count=%+v amount=%+v", byCount, byAmount)
	}
	for _, result := range []workbench.Contributions{byCount, byAmount} {
		count := result.OtherKnownRequests + result.UnassignedRequests
		amount := new(big.Int).Add(mustBigInt(t, result.OtherKnownMicros), mustBigInt(t, result.UnassignedMicros))
		for _, row := range result.Rows {
			count += row.RequestCount
			amount.Add(amount, mustBigInt(t, row.SelectedAmountMicros))
			if row.Share.Unit != "micros" || row.Share.Denominator != "1004" {
				t.Fatal("monetary share changed", row)
			}
		}
		if count != 13 || result.DenominatorRequests != 13 || amount.String() != "1004" || result.DenominatorMicros != "1004" || result.OtherKnownRequests == 0 {
			t.Fatalf("not conserved %+v", result)
		}
	}
	unknown := get("cost_project", "requests", 0, 5, "")
	if len(unknown.Rows) != 0 || unknown.UnassignedRequests != 13 || unknown.OtherKnownRequests != 0 || unknown.UnassignedMicros != "1004" {
		t.Fatalf("unknown %+v", unknown)
	}
	countPage := get("model", "requests", 2, 0, "")
	moneyPage := get("model", "amount", 2, 0, "")
	legacyCursor := base64.RawURLEncoding.EncodeToString([]byte(reading.ID + "\x00model\x002"))
	if moneyPage.NextCursor != legacyCursor {
		t.Fatal("legacy amount cursor changed")
	}
	var labels []string
	page := countPage
	for {
		if page.OtherKnownRequests != 0 || page.DenominatorRequests != 13 || page.UnassignedRequests != 0 {
			t.Fatalf("page %+v", page)
		}
		for _, row := range page.Rows {
			labels = append(labels, row.Label)
		}
		if page.NextCursor == "" {
			break
		}
		page = get("model", "requests", 2, 0, page.NextCursor)
	}
	if strings.Join(labels, ",") != "frequent,alpha,beta,delta,expensive,gamma" {
		t.Fatal("unstable count pagination", labels)
	}
	for _, tc := range []struct{ dimension, order, cursor string }{{"model", "requests", moneyPage.NextCursor}, {"model", "amount", countPage.NextCursor}, {"source_project", "requests", countPage.NextCursor}} {
		if _, e := f.workspace.GetWorkbenchContributionsOrdered(f.ctx, f.subject, reading.ID, tc.dimension, tc.order, 2, 0, tc.cursor, now); !errors.Is(e, workbench.ErrInvalid) {
			t.Fatalf("cross-order/dimension cursor accepted %+v %v", tc, e)
		}
	}
	referenceRequest := request
	referenceRequest.MoneyBasis = workbench.MoneyCalculatedReference
	reference, err := f.workspace.CreateWorkbenchReading(f.ctx, f.subject, referenceRequest, now)
	if err != nil {
		t.Fatal(err)
	}
	calculated, err := f.workspace.GetWorkbenchContributionsOrdered(f.ctx, f.subject, reference.ID, "model", "requests", 0, 5, "", now)
	if err != nil || calculated.DenominatorRequests != 13 || calculated.DenominatorMicros != "1084" || calculated.Rows[0].SelectedAmountMicros != "80" || calculated.Rows[0].GatewayChargedMicros != "0" || calculated.Rows[0].Ref != byCount.Rows[0].Ref {
		t.Fatalf("reference basis %+v %v", calculated, err)
	}
	if _, e := f.workspace.GetWorkbenchContributionsOrdered(f.ctx, f.subject, reference.ID, "model", "requests", 2, 0, countPage.NextCursor, now); !errors.Is(e, workbench.ErrInvalid) {
		t.Fatal("cross-reading cursor accepted", e)
	}
	detail, err := f.workspace.GetWorkbenchDetails(f.ctx, f.subject, reading.ID, byCount.Rows[0].Ref, 50, "", now)
	if err != nil || len(detail.Rows) != 8 {
		t.Fatalf("detail continuity %d %v", len(detail.Rows), err)
	}
	ingest(20, "late", "9000", "9000")
	if frozen := get("model", "requests", 0, 5, ""); frozen.DenominatorRequests != 13 || frozen.DenominatorMicros != "1004" {
		t.Fatal("late member changed reading", frozen)
	}
	sample["project_id"] = float64(203)
	ingest(21, "hidden-project", "999999", "999999")
	// Summary-only, source/project-constrained authority remains in force.
	restricted := "xs_rankrestricted01"
	f.exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_summary) VALUES($1,$2,'project-202',true)`, restricted, f.source)
	restrictedReading, err := f.workspace.CreateWorkbenchReading(f.ctx, restricted, request, now)
	if err != nil {
		t.Fatal(err)
	}
	recentRestricted, recentErr := f.workspace.GetWorkbenchRecentRequests(f.ctx, restricted, restrictedReading.ID, "", 5, "", now)
	if recentErr != nil || len(recentRestricted.Items) != 0 || recentRestricted.NextCursor != "" {
		t.Fatal("summary-only scope disclosed recent details", recentRestricted, recentErr)
	}
	restrictedPage, err := f.workspace.GetWorkbenchContributionsOrdered(f.ctx, restricted, restrictedReading.ID, "model", "requests", 0, 5, "", now)
	if err != nil || restrictedPage.DenominatorRequests != 14 || restrictedPage.Rows[0].DrilldownAllowed {
		t.Fatalf("restricted %+v %v", restrictedPage, err)
	}
	if _, e := f.workspace.GetWorkbenchDetails(f.ctx, restricted, restrictedReading.ID, restrictedPage.Rows[0].Ref, 50, "", now); !errors.Is(e, workbench.ErrScopeForbidden) {
		t.Fatal("detail permission escaped", e)
	}
	if _, e := f.workspace.GetWorkbenchContributionsOrdered(f.ctx, restricted, restrictedReading.ID, "application", "requests", 0, 5, "", now); !errors.Is(e, workbench.ErrScopeForbidden) {
		t.Fatal("dimension permission escaped", e)
	}
	if _, e := f.workspace.GetWorkbenchContributionsOrdered(f.ctx, f.subject, reading.ID, "model", "requests", 0, 5, "", reading.ExpiresAt); !errors.Is(e, workbench.ErrExpired) {
		t.Fatal("expired reading served", e)
	}
	f.exec(`UPDATE workbench_access_grants SET active=false,revision=revision+1 WHERE subject_id=$1`, f.subject)
	if _, e := f.workspace.GetWorkbenchContributionsOrdered(f.ctx, f.subject, reading.ID, "model", "requests", 0, 5, "", now); !errors.Is(e, workbench.ErrAccessChanged) {
		t.Fatal("revoked reading served", e)
	}
}

func TestWorkbenchContributionRequestOrderFreezesPurposeInterpretation(t *testing.T) {
	f := newPurposeFixture(t, []string{"1", "1", "100", "5"})
	for i := 0; i < 2; i++ {
		f.accept(t, i, f.declare(t, i, "software_check", 0))
	}
	old := f.reading(t)
	check := func(r workbench.Reading, wantSoftware, wantUnknown int64) {
		t.Helper()
		groups, err := f.store.GetWorkbenchContributionsOrdered(f.ctx, f.subject, r.ID, "purpose", "requests", 0, 5, "", time.Now())
		if err != nil || groups.DenominatorRequests != 4 {
			t.Fatalf("purpose %+v %v", groups, err)
		}
		got := map[string]int64{}
		for _, row := range groups.Rows {
			got[row.Label] = row.RequestCount
		}
		if got["software_check"] != wantSoftware || got["unknown"] != wantUnknown {
			t.Fatal("purpose interpretation", got)
		}
	}
	check(old, 2, 2)
	f.accept(t, 2, f.declare(t, 2, "software_check", 0))
	check(old, 2, 2)
	check(f.reading(t), 3, 1)
}

func TestWorkbenchContributionRequestOrderAuthorizedApplicationProjection(t *testing.T) {
	f := newSourceFixture(t)
	f.allow()
	before := f.reading()
	f.money()
	current := f.reading()
	for _, reading := range []workbench.Reading{before, current} {
		rows, err := f.workspace.GetWorkbenchContributionsOrdered(f.ctx, f.subject, reading.ID, "application", "requests", 0, 5, "", time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if reading.ID == before.ID {
			if rows.DenominatorRequests != 0 || len(rows.Rows) != 0 {
				t.Fatal("late economics escaped frozen members", rows)
			}
		} else if rows.DenominatorRequests != 1 || len(rows.Rows) != 1 || rows.Rows[0].Label != "9007199254740993" || rows.Rows[0].RequestCount != 1 || rows.Rows[0].GatewayChargedMicros != "0" || rows.Rows[0].CalculatedReferenceMicros != "30000000" || rows.Rows[0].Share.Unit != "micros" {
			t.Fatal("authorized application projection", rows)
		}
	}
	f.exec(`UPDATE workbench_access_grants SET allow_source_context=false,revision=revision+1 WHERE subject_id=$1`, f.subject)
	if _, err := f.workspace.GetWorkbenchContributionsOrdered(f.ctx, f.subject, current.ID, "application", "requests", 0, 5, "", time.Now().UTC()); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("source-context revoke escaped", err)
	}
}
