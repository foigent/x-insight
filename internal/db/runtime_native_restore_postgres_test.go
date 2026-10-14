package db_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/jackc/pgx/v5"
)

// Exercise the product pg_dump/new-database restore path with every runtime
// membership populated, using actual workspace/importer roles and a live lease.
func TestRuntimeNativePopulatedReadingAssociationsAndCurrentRestore(t *testing.T) {
	if os.Getenv("XINSIGHT_POSTGRES_DSN") == "" {
		t.Skip("dedicated native PostgreSQL required")
	}
	base, e := url.Parse(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	if e != nil || base.Hostname() != "127.0.0.1" || os.Getenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST") != "1" {
		t.Skip("native restore uses the separately authorized TCP test instance")
	}
	f := newSourceFixture(t)
	credentials := func(dsn string) hostctl.DatabaseCredentials {
		t.Helper()
		u, e := url.Parse(dsn)
		if e != nil {
			t.Fatal(e)
		}
		c := hostctl.DatabaseCredentials{AdminDSN: dsn}
		c.Admin, e = pgx.ParseConfig(dsn)
		if e != nil {
			t.Fatal(e)
		}
		for _, role := range []string{"reader", "workspace", "importer"} {
			v := *u
			v.User = url.UserPassword("xinsight_"+role, "synthetic digest qualification "+role)
			config, e := pgx.ParseConfig(v.String())
			if e != nil {
				t.Fatal(e)
			}
			switch role {
			case "reader":
				c.ReaderDSN, c.Reader = v.String(), config
			case "workspace":
				c.WorkspaceDSN, c.Workspace = v.String(), config
			case "importer":
				c.ImporterDSN, c.Importer = v.String(), config
			}
		}
		return c
	}
	c := credentials(f.dsn)
	manager := hostctl.PostgresManager{Runner: hostctl.ExecRunner{}}
	if e = manager.EnsureRoles(f.ctx, c); e != nil {
		t.Fatal(e)
	}
	if e = manager.ApplyGrants(f.ctx, c); e != nil {
		t.Fatal(e)
	}
	if e = manager.VerifyRoleBoundaries(f.ctx, c); e != nil {
		t.Fatal(e)
	}
	a := f.documents[sourcefacts.ContextProfile]
	b := runtimeGolden(t)[0]
	b.SourceInstance, b.OrgID, b.ProjectID, b.RequestID, b.SubjectID = a.SourceInstance, a.OrgID, a.ProjectID, a.RequestID, a.RequestID
	b.OccurredAt, b.RecordedAt = a.OccurredAt, a.RecordedAt
	var destination sourcefacts.Observation
	for _, candidate := range runtimeGolden(t) {
		if candidate.Profile == sourcefacts.DestinationProfile {
			destination = candidate
			break
		}
	}
	if destination.Destination == nil {
		t.Fatal("destination fixture absent")
	}
	destination.SourceInstance, destination.OrgID, destination.ProjectID, destination.RequestID = b.SourceInstance, b.OrgID, b.ProjectID, b.RequestID
	destination.OccurredAt, destination.RecordedAt = b.OccurredAt, b.RecordedAt
	destination.Destination.InputBindingState, destination.Destination.InputBindingRef = "known", "input-one"
	putRuntimeDocuments(t, f, f.source, b, destination)
	f.money()
	f.allow()
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true,allow_source_destination=true,allow_resource=true WHERE subject_id=$1`, f.subject)
	ws, e := db.OpenWorkspaceStore(f.ctx, c.WorkspaceDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer ws.Close()
	importer, e := db.OpenSourceFactsStore(c.ImporterDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer importer.Close()
	now := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	record := resourceForRuntime(f.source, b, destination)
	next := record
	next.Revision, next.PreviousHash = 2, sourcefacts.Hash(canonicalResourceFixture(t, record))
	if inserted, err := importer.StoreSyntheticResourceRecord(f.ctx, next, now.Add(-time.Second)); err != nil || !inserted {
		t.Fatal("actual importer accepts absent predecessor", inserted, err)
	}
	conflicting := record
	conflicting.ActorRef = "contradictory-predecessor"
	if inserted, err := importer.StoreSyntheticResourceRecord(f.ctx, conflicting, now); inserted || !errors.Is(err, workbench.ErrConflict) {
		t.Fatal("actual importer accepted known successor conflict", inserted, err)
	}
	if inserted, err := importer.StoreSyntheticResourceRecord(f.ctx, record, now.Add(-time.Second)); err != nil || !inserted {
		t.Fatal("actual importer completes matching chain", inserted, err)
	}
	lateParent := record
	lateParent.RecordRef, lateParent.Input = "late-native-predecessor", nil
	gap := lateParent
	gap.Revision, gap.PreviousHash = 2, sourcefacts.Hash(canonicalResourceFixture(t, lateParent))
	if inserted, err := importer.StoreSyntheticResourceRecord(f.ctx, gap, now.Add(-time.Second)); err != nil || !inserted {
		t.Fatal("actual importer gap before frozen cutoff", inserted, err)
	}
	missingParent := lateParent
	missingParent.RecordRef = "unresolved-native-predecessor"
	unresolved := missingParent
	unresolved.Revision, unresolved.PreviousHash = 2, sourcefacts.Hash(canonicalResourceFixture(t, missingParent))
	if inserted, err := importer.StoreSyntheticResourceRecord(f.ctx, unresolved, now.Add(-time.Second)); err != nil || !inserted {
		t.Fatal("actual importer unresolved predecessor", inserted, err)
	}
	r, e := ws.CreateRuntimeReading(f.ctx, f.subject, runtimeRequestInput(t, b), now)
	if e != nil {
		t.Fatal("actual workspace freezes association tables", e)
	}
	requests, e := ws.GetRuntimeRequests(f.ctx, f.subject, r.ID, "all", 100, "", now)
	if e != nil || len(requests.Items) != 1 {
		t.Fatal(requests, e)
	}
	ref := requests.Items[0].Ref
	associations, e := ws.GetRuntimeAssociations(f.ctx, f.subject, r.ID, ref, 100, "", now)
	if e != nil || len(associations.Items) != 4 {
		t.Fatal("populated associations", associations, e)
	}
	resources, e := ws.GetRuntimeResources(f.ctx, f.subject, r.ID, ref, 100, "", now)
	if e != nil || len(resources.Items) != 3 {
		t.Fatal("populated resource and frozen input witnesses", resources, e)
	}
	for _, item := range resources.Items {
		if item.Record.RecordRef == record.RecordRef {
			if item.ChainState != "complete" || item.InputState != "reference_matched" {
				t.Fatal("completed input chain", item)
			}
		} else if item.ChainState != "missing_predecessor" {
			t.Fatal("genuine predecessor gap", item)
		}
	}
	if inserted, err := importer.StoreSyntheticResourceRecord(f.ctx, lateParent, now.Add(time.Second)); err != nil || !inserted {
		t.Fatal("actual importer late matching predecessor", inserted, err)
	}
	var streamID string
	if e = f.admin.QueryRow(`SELECT id::text FROM source_fact_streams WHERE source_system_id=$1 AND profile=$2`, f.source, sourcefacts.RuntimeProfile).Scan(&streamID); e != nil {
		t.Fatal(e)
	}
	stream, e := importer.LoadSourceFactsStream(f.ctx, streamID)
	if e != nil {
		t.Fatal(e)
	}
	cursor, e := importer.LoadCurrentRuntimeCursor(f.ctx, stream)
	if e != nil {
		t.Fatal(e)
	}
	lease := sourcefacts.CurrentRuntimeEntry{RequestID: b.RequestID, Family: "stream", SubjectKind: "provider_attempt", SubjectID: "synthetic-attempt", Operation: "openai.chat_completions.v1", Freshness: "lease_valid_execution", OriginalState: "streaming", LastObservedAt: now.Format(sourcefacts.TimeFormat), ValidUntil: now.Add(time.Minute).Format(sourcefacts.TimeFormat), LeaseGeneration: "1", EvidenceRef: "uf1_" + strings.Repeat("a", 64)}
	if e = importer.CommitCurrentRuntimePage(f.ctx, stream, cursor, currentRuntimeWire(t, stream, now, "", lease), now); e != nil {
		t.Fatal(e)
	}
	current, e := ws.GetRuntimeCurrent(f.ctx, f.subject, workbench.Filters{}, 100, "", now)
	if e != nil || len(current.Items) != 1 {
		t.Fatal("live lease fixture", current, e)
	}
	if _, e = hostctl.InspectCleanBaselineSchemaContract(f.ctx, f.admin); e != nil {
		t.Fatal("fresh schema35 contract mismatch", e)
	}
	if e = db.ValidateWorkbenchRestore(f.ctx, f.admin); e != nil {
		t.Fatal(e)
	}
	// Emit an optional development contract from this real, fully migrated and
	// privilege-converged database; never overwrite an existing candidate.
	if output := os.Getenv("XINSIGHT_B_SCHEMA_CONTRACT_OUTPUT"); output != "" {
		if !filepath.IsAbs(output) || filepath.Clean(output) != output {
			t.Fatal("absolute contract output required")
		}
		manifest, e := hostctl.GenerateCleanBaselineSchemaContract(f.ctx, f.admin)
		if e != nil {
			t.Fatal(e)
		}
		raw, e := json.MarshalIndent(manifest, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		file, e := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			t.Fatal(e)
		}
		_, writeErr := file.Write(append(raw, '\n'))
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatal("write development contract", writeErr, closeErr)
		}
	}
	backup, e := manager.Backup(f.ctx, c, filepath.Join(t.TempDir(), "populated-runtime.dump"))
	if e != nil {
		t.Fatal(e)
	}
	target := fmt.Sprintf("xinsight_restore_runtime_%d", time.Now().UnixNano())
	u, e := url.Parse(f.dsn)
	if e != nil {
		t.Fatal(e)
	}
	u.Path = "/" + target
	restored := credentials(u.String())
	t.Cleanup(func() {
		if _, e := f.admin.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{target}.Sanitize() + " WITH (FORCE)"); e != nil {
			t.Error(e)
		}
	})
	expected, e := db.LoadMigrationChecksums("../../migrations")
	if e != nil {
		t.Fatal(e)
	}
	if e = manager.RestoreToNewDatabase(f.ctx, c, restored, backup.Path, "../../migrations", expected); e != nil {
		t.Fatal("populated runtime native restore", e)
	}
	recovered, e := db.OpenWorkspaceStore(f.ctx, restored.WorkspaceDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	restoredSQL := openTestSQL(t, restored.AdminDSN)
	defer restoredSQL.Close()
	if _, e = hostctl.InspectCleanBaselineSchemaContract(f.ctx, restoredSQL); e != nil {
		t.Fatal("restored schema35 contract mismatch", e)
	}
	historical, e := recovered.GetRuntimeAssociations(f.ctx, f.subject, r.ID, ref, 100, "", now)
	if e != nil || !reflect.DeepEqual(historical, associations) {
		t.Fatal("restore changed frozen associations", historical, e)
	}
	recoveredResources, e := recovered.GetRuntimeResources(f.ctx, f.subject, r.ID, ref, 100, "", now)
	if e != nil || !reflect.DeepEqual(recoveredResources, resources) {
		t.Fatal("restore changed frozen resources", recoveredResources, e)
	}
	originalResources, e := ws.GetRuntimeResources(f.ctx, f.subject, r.ID, ref, 100, "", now)
	if e != nil || !reflect.DeepEqual(originalResources, resources) {
		t.Fatal("restore changed original resources", originalResources, e)
	}
	after, e := recovered.GetRuntimeCurrent(f.ctx, f.subject, workbench.Filters{}, 100, "", now)
	if e != nil || len(after.Items) != 0 {
		t.Fatal("restore revived a current lease", after, e)
	}
	original, e := ws.GetRuntimeAssociations(f.ctx, f.subject, r.ID, ref, 100, "", now)
	if e != nil || !reflect.DeepEqual(original, associations) {
		t.Fatal("restore changed original reading", e)
	}
	originalCurrent, e := ws.GetRuntimeCurrent(f.ctx, f.subject, workbench.Filters{}, 100, "", now)
	if e != nil || !reflect.DeepEqual(originalCurrent, current) {
		t.Fatal("restore changed original lease", e)
	}
	for _, table := range []string{"runtime_reading_source_members", "runtime_reading_economic_members", "runtime_reading_resource_members"} {
		var forbidden bool
		if e = f.admin.QueryRow(`SELECT has_table_privilege('xinsight_workspace',$1,'UPDATE') OR has_table_privilege('xinsight_workspace',$1,'DELETE') OR has_table_privilege('xinsight_importer',$1,'INSERT') OR has_table_privilege('xinsight_reader',$1,'INSERT')`, table).Scan(&forbidden); e != nil || forbidden {
			t.Fatal("association role boundary", table, e)
		}
	}
	t.Log("actual roles admit matching out-of-order resources and reject known conflict; native pg_dump/new database preserves complete, missing and late-filled frozen chains, clears current eligibility, and leaves original history and lease unchanged")
}
