package hostctl

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/testsourcefacts"
	"github.com/jackc/pgx/v5"
)

func managedOfflineFixture(t *testing.T, source string) (sourcefacts.OfflineAuthorization, []byte, sourcefacts.Page) {
	t.Helper()
	b := testsourcefacts.Load(t, testsourcefacts.Chat)
	var page sourcefacts.Page
	var org, project, profile string
	for _, candidate := range b.Pages {
		if candidate.Profile != sourcefacts.ContextProfile {
			continue
		}
		if err := json.Unmarshal(candidate.Page, &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Events) > 0 {
			org, project, profile = candidate.Org, candidate.Project, candidate.Profile
			break
		}
	}
	if len(page.Events) == 0 {
		t.Fatal("retained source fixture has no documents")
	}
	page.NextCursor = ""
	page.HighWatermark = ""
	page.HasMore = false
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("../sourcefacts/contract/source-fact.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	a := sourcefacts.OfflineAuthorization{SchemaVersion: sourcefacts.OfflineAuthorizationVersion, AuthorizationID: "synthetic-offline", Environment: "test", TargetRef: "synthetic-asus-xi", MachineIDSHA256: sourcefacts.Hash([]byte("synthetic-machine")), ProductVersion: "0.2.0-dev", ProductCommit: "synthetic-managed-build", ProtocolVersion: "2.0.2", ProtocolCommit: sourcefacts.ProtocolCommit, ProtocolReleaseDigest: sourcefacts.ProtocolReleaseDigest, SourceSchemaSHA256: sourcefacts.Hash(schema), SourceSystemID: source, SourceInstance: page.ProducerInstance, OrgID: org, ProjectID: project, Profile: profile, NotBefore: now.Add(-time.Minute).Format(sourcefacts.TimeFormat), ExpiresAt: now.Add(time.Hour).Format(sourcefacts.TimeFormat), BundlePath: "/tmp/synthetic-source-bundle.json", BundleSHA256: sourcefacts.Hash(raw), BundleID: "synthetic-bundle", OperationID: "synthetic-operation", MaxRecords: 50, PayloadCaptureMode: "metadata_only"}
	if _, err := sourcefacts.ValidateOfflineBatch(raw, a, now); err != nil {
		t.Fatal("offline reprojection", err)
	}
	return a, raw, page
}

func managedStore(t *testing.T, dsn string) *db.SourceFactsStore {
	t.Helper()
	s, e := db.OpenSourceFactsStore(dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func managedSQL(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	s, e := sql.Open("pgx", dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func managedExec(t *testing.T, d *sql.DB, query string, args ...any) {
	t.Helper()
	if _, e := d.ExecContext(t.Context(), query, args...); e != nil {
		t.Fatal(e)
	}
}
func managedCount(t *testing.T, d *sql.DB, table string) int64 {
	t.Helper()
	var n int64
	if e := d.QueryRow(`SELECT count(*) FROM ` + pgx.Identifier{table}.Sanitize()).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}

func TestManagedSourceNativeRolesOfflineAndRestore(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	m := PostgresManager{Runner: ExecRunner{}}
	if e := m.EnsureRoles(ctx, c); e != nil {
		t.Fatal(e)
	}
	if _, e := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, 31)); e != nil {
		t.Fatal(e)
	}
	admin := managedSQL(t, c.AdminDSN)
	var source, id string
	if e := admin.QueryRow(`SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&source, &id); e != nil {
		t.Fatal(e)
	}
	a, raw, page := managedOfflineFixture(t, source)
	// A genuine schema31 predecessor has no managed attention or offline grants.
	managedExec(t, admin, `INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1::uuid,$2,'unirouter','synthetic predecessor')`, source, "managed:"+source)
	managedExec(t, admin, `INSERT INTO source_fact_streams(id,source_system_id,source_instance,org_id,project_id,profile,scope_generation,status,cursor_value,high_watermark) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,'1','paused','retained-position','retained-position')`, id, source, a.SourceInstance, a.OrgID, a.ProjectID, a.Profile)
	targetSchema, parseErr := strconv.Atoi(buildinfo.DatabaseSchemaMax)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if n, e := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); e != nil || n != targetSchema-31 {
		t.Fatal("populated schema31 through current schema", n, e)
	}
	if e := m.ApplyGrants(ctx, c); e != nil {
		t.Fatal(e)
	}
	if e := m.VerifyRoleBoundaries(ctx, c); e != nil {
		t.Fatal(e)
	}
	owner := managedStore(t, c.AdminDSN)
	importer := managedStore(t, c.ImporterDSN)
	stream, e := owner.LoadSourceFactsStream(ctx, id)
	if e != nil || stream.Cursor != "retained-position" || stream.RequiresAttention || stream.Status != "paused" {
		t.Fatal("migration changed predecessor", stream, e)
	}
	for _, dsn := range []string{c.ImporterDSN, c.WorkspaceDSN, c.ReaderDSN} {
		s := managedStore(t, dsn)
		if _, e := s.RegisterSourceFactsStream(ctx, stream); e == nil {
			t.Fatal("runtime role registered live scope")
		}
		if _, e := s.ControlSourceFactsStream(ctx, id, "active", stream.Revision, false); e == nil {
			t.Fatal("runtime role enabled live scope")
		}
		if _, e := s.RegisterSourceOfflineAuthorization(ctx, a); e == nil {
			t.Fatal("runtime role registered offline grant")
		}
	}
	grant, e := owner.RegisterSourceOfflineAuthorization(ctx, a)
	if e != nil || grant.Status != "disabled" {
		t.Fatal(e)
	}
	if _, e := importer.ImportSourceFactsOffline(ctx, a, raw, time.Now()); e == nil {
		t.Fatal("disabled offline grant imported")
	}
	if managedCount(t, admin, "connector_import_operation_bindings") != 0 {
		t.Fatal("invalid auth reserved input")
	}
	grant, e = owner.ControlSourceOfflineAuthorization(ctx, a.AuthorizationID, "active", grant.Revision, a.Digest())
	if e != nil {
		t.Fatal(e)
	}
	for _, dsn := range []string{c.ImporterDSN, c.WorkspaceDSN, c.ReaderDSN} {
		d := managedSQL(t, dsn)
		if _, e := d.Exec(`UPDATE source_fact_offline_authorizations SET revision=revision+1 WHERE id=$1`, a.AuthorizationID); e == nil {
			t.Fatal("runtime role mutated offline grant")
		}
	}
	// Offline succeeds with live paused and no HTTP reader/credential configured.
	receipt, e := importer.ImportSourceFactsOffline(ctx, a, raw, time.Now())
	if e != nil || receipt.Counts.Inserted != int64(len(page.Events)) {
		t.Fatal("offline import", receipt, e)
	}
	after, e := owner.LoadSourceFactsStream(ctx, id)
	if e != nil || !reflect.DeepEqual(stream, after) {
		t.Fatal("offline altered live state", e)
	}
	retry, e := importer.ImportSourceFactsOffline(ctx, a, raw, time.Now())
	if e != nil || !reflect.DeepEqual(receipt, retry) {
		t.Fatal("completed replay", e)
	}
	if managedCount(t, admin, "source_fact_documents") != receipt.Counts.Inserted || managedCount(t, admin, "connector_operational_receipts") != 1 {
		t.Fatal("receipt/fact cardinality")
	}
	bad := a
	bad.BundleSHA256 = sourcefacts.Hash([]byte("changed input"))
	if _, e := importer.ImportSourceFactsOffline(ctx, bad, raw, time.Now()); e == nil {
		t.Fatal("different input accepted")
	}
	// An independent valid operation with the same facts yields no new facts.
	duplicate := a
	duplicate.AuthorizationID = "synthetic-duplicate"
	duplicate.OperationID = "synthetic-duplicate"
	g, e := owner.RegisterSourceOfflineAuthorization(ctx, duplicate)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = owner.ControlSourceOfflineAuthorization(ctx, g.ID, "active", g.Revision, duplicate.Digest()); e != nil {
		t.Fatal(e)
	}
	r, e := importer.ImportSourceFactsOffline(ctx, duplicate, raw, time.Now())
	if e != nil || r.Counts.Inserted != 0 || r.Counts.DuplicateNoop != int64(len(page.Events)) {
		t.Fatal("duplicate facts", r, e)
	}
	grant, e = owner.ControlSourceOfflineAuthorization(ctx, grant.ID, "revoked", grant.Revision, "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = importer.ImportSourceFactsOffline(ctx, a, raw, time.Now()); !errors.Is(e, sourcefacts.ErrAuthorization) {
		t.Fatal("revoked completed replay", e)
	}
	if _, e = owner.ControlSourceOfflineAuthorization(ctx, grant.ID, "active", grant.Revision, a.Digest()); e == nil {
		t.Fatal("revoked grant revived")
	}
	// Persistent attention cannot be cleared by the importer or a process restart.
	stream, e = owner.ControlSourceFactsStream(ctx, id, "active", stream.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = importer.RecordSourceFactsFailure(ctx, stream, "cursor_expired", time.Now()); e != nil {
		t.Fatal(e)
	}
	stream, e = owner.LoadSourceFactsStream(ctx, id)
	if e != nil || !stream.RequiresAttention {
		t.Fatal(e)
	}
	impSQL := managedSQL(t, c.ImporterDSN)
	if _, e = impSQL.Exec(`UPDATE source_fact_streams SET requires_attention=false,revision=revision+1 WHERE id=$1::uuid`, id); e == nil {
		t.Fatal("importer cleared attention")
	}
	if _, e = owner.ControlSourceFactsStream(ctx, id, "active", stream.Revision, false); !errors.Is(e, sourcefacts.ErrUnavailable) {
		t.Fatal("implicit attention acknowledgement", e)
	}
	stream, e = owner.ControlSourceFactsStream(ctx, id, "active", stream.Revision, true)
	if e != nil || stream.RequiresAttention {
		t.Fatal(e)
	}
	old := stream
	stream, e = owner.ControlSourceFactsStream(ctx, id, "paused", stream.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	stream, e = owner.ControlSourceFactsStream(ctx, id, "active", stream.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	livePage := page
	livePage.NextCursor = "new-position"
	livePage.HighWatermark = "new-position"
	if e = importer.CommitSourceFactsPage(ctx, old, livePage, time.Now()); !errors.Is(e, sourcefacts.ErrStreamChanged) {
		t.Fatal("pause-resume ABA committed", e)
	}
	if e = importer.CommitSourceFactsPage(ctx, stream, livePage, time.Now()); e != nil {
		t.Fatal("live/offline shared immutable facts", e)
	}
	if managedCount(t, admin, "source_fact_documents") != receipt.Counts.Inserted {
		t.Fatal("live duplicated offline facts")
	}
	// Binding validation is read-only; restore then suspends the independent grants.
	before := managedCount(t, admin, "source_fact_management_events")
	for i := 0; i < 2; i++ {
		if e = db.ValidateWorkbenchRestore(ctx, admin); e != nil {
			t.Fatal(e)
		}
	}
	if managedCount(t, admin, "source_fact_management_events") != before {
		t.Fatal("restore validation mutated state")
	}
	backup, e := m.Backup(ctx, c, filepath.Join(t.TempDir(), "managed-source.dump"))
	if e != nil {
		t.Fatal(e)
	}
	f := newInstallFixture(t)
	bound := digestSyntheticCredentials(t, f, c)
	name := fmt.Sprintf("xinsight_restore_ma_%d", time.Now().UnixNano())
	restored, e := StageRestoredCredentialSet(f.paths, bound, name)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	expected, e := db.LoadMigrationChecksums("../../migrations")
	if e != nil {
		t.Fatal(e)
	}
	if e = m.RestoreToNewDatabase(ctx, c, restored, backup.Path, "../../migrations", expected); e != nil {
		t.Fatal("native restore", e)
	}
	recovered := managedStore(t, restored.AdminDSN)
	s, e := recovered.LoadSourceFactsStream(ctx, id)
	if e != nil || s.Status != "paused" || !s.RequiresAttention || s.Cursor != "new-position" {
		t.Fatal("restore collection default", s, e)
	}
	for _, want := range []struct{ id, status string }{{a.AuthorizationID, "revoked"}, {duplicate.AuthorizationID, "disabled"}} {
		got, e := recovered.LoadSourceOfflineRegistration(ctx, want.id)
		if e != nil || got.Status != want.status {
			t.Fatal("restored offline authority", got, e)
		}
	}
	restoredSQL := managedSQL(t, restored.AdminDSN)
	if managedCount(t, restoredSQL, "source_fact_documents") != receipt.Counts.Inserted || managedCount(t, restoredSQL, "connector_operational_receipts") != 2 {
		t.Fatal("restore facts/receipts")
	}
	if e = db.ValidateWorkbenchRestore(ctx, restoredSQL); e != nil {
		t.Fatal(e)
	}
	managedExec(t, restoredSQL, `ALTER TABLE source_fact_offline_authorizations DISABLE TRIGGER source_fact_offline_authorization_identity`)
	if e = db.ValidateWorkbenchRestore(ctx, restoredSQL); e == nil {
		t.Fatal("damaged managed guard accepted")
	}
	t.Log("schema31→32→33; real four-role grants; no-network offline/replay/revoke; live-offline duplicates; pause-resume revision; durable attention; native backup/restore suspension and immutable history; read-only validation and damaged guard rejection")
}

// Wait on the actual PG lock graph, not a timing assumption about goroutines.
func awaitManagedWaiter(t *testing.T, ctx context.Context, admin *sql.DB, fragment string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if e := admin.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE $1`, "%"+fragment+"%").Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("expected database lock waiter absent: " + strings.TrimSpace(fragment))
}

func TestManagedOfflineAtomicReceiptAndAuthorizationLocks(t *testing.T) {
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
	admin := managedSQL(t, c.AdminDSN)
	owner := managedStore(t, c.AdminDSN)
	importer := managedStore(t, c.ImporterDSN)
	var source string
	if e := admin.QueryRow(`SELECT gen_random_uuid()::text`).Scan(&source); e != nil {
		t.Fatal(e)
	}
	a, raw, page := managedOfflineFixture(t, source)
	activate := func(a sourcefacts.OfflineAuthorization) db.SourceOfflineRegistration {
		t.Helper()
		g, e := owner.RegisterSourceOfflineAuthorization(ctx, a)
		if e != nil {
			t.Fatal(e)
		}
		g, e = owner.ControlSourceOfflineAuthorization(ctx, g.ID, "active", g.Revision, a.Digest())
		if e != nil {
			t.Fatal(e)
		}
		return g
	}
	activate(a)
	managedExec(t, admin, `CREATE FUNCTION synthetic_receipt_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic receipt persistence failure'; END $$`)
	managedExec(t, admin, `CREATE TRIGGER synthetic_receipt_failure BEFORE INSERT ON connector_operational_receipts FOR EACH ROW EXECUTE FUNCTION synthetic_receipt_failure()`)
	if _, e := importer.ImportSourceFactsOffline(ctx, a, raw, time.Now()); e == nil {
		t.Fatal("synthetic receipt failure did not fire")
	}
	if managedCount(t, admin, "source_fact_documents") != 0 || managedCount(t, admin, "connector_operational_receipts") != 0 || managedCount(t, admin, "connector_import_operation_bindings") != 1 {
		t.Fatal("fact/receipt atomic rollback or independent reservation failed")
	}
	managedExec(t, admin, `DROP TRIGGER synthetic_receipt_failure ON connector_operational_receipts`)
	receipt, e := importer.ImportSourceFactsOffline(ctx, a, raw, time.Now())
	if e != nil || receipt.Counts.Inserted != int64(len(page.Events)) {
		t.Fatal("retry after reserved failure", receipt, e)
	}
	changed := a
	changed.AuthorizationID = "different-binding"
	changed.BundleID = "different-bundle-binding"
	activate(changed)
	if _, e := importer.ImportSourceFactsOffline(ctx, changed, raw, time.Now()); e == nil {
		t.Fatal("same operation rebound")
	}
	if managedCount(t, admin, "connector_import_operation_bindings") != 1 {
		t.Fatal("operation conflict wrote reservation")
	}
	// A valid grant can expire while waiting for the shared source lock. The DB
	// clock is checked after that wait, before even reserving an operation ID.
	expires := a
	expires.AuthorizationID = "expires-in-lock"
	expires.OperationID = "expires-in-lock"
	expires.ExpiresAt = time.Now().Add(1500 * time.Millisecond).UTC().Format(sourcefacts.TimeFormat)
	activate(expires)
	block, e := admin.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback()
	if _, e = block.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "source-facts:"+source); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := importer.ImportSourceFactsOffline(ctx, expires, raw, time.Now()); done <- e }()
	awaitManagedWaiter(t, ctx, admin, "pg_advisory_xact_lock")
	if _, e = admin.ExecContext(ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM $1::timestamptz-clock_timestamp())))`, expires.ExpiresAt); e != nil {
		t.Fatal(e)
	}
	if e = block.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = <-done; !errors.Is(e, sourcefacts.ErrAuthorization) {
		t.Fatal("expired while waiting", e)
	}
	if managedCount(t, admin, "connector_import_operation_bindings") != 1 {
		t.Fatal("expired grant reserved operation")
	}
	// Hold the real completion transaction at receipt insertion. Revocation must
	// wait for its shared grant row lock, then return only after commit finishes.
	race := a
	race.AuthorizationID = "commit-before-revoke"
	race.OperationID = "commit-before-revoke"
	grant := activate(race)
	managedExec(t, admin, `CREATE FUNCTION synthetic_receipt_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(91827364); RETURN NEW; END $$`)
	managedExec(t, admin, `CREATE TRIGGER synthetic_receipt_wait BEFORE INSERT ON connector_operational_receipts FOR EACH ROW EXECUTE FUNCTION synthetic_receipt_wait()`)
	block, e = admin.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback()
	if _, e = block.ExecContext(ctx, `SELECT pg_advisory_xact_lock(91827364)`); e != nil {
		t.Fatal(e)
	}
	go func() { _, e := importer.ImportSourceFactsOffline(ctx, race, raw, time.Now()); done <- e }()
	awaitManagedWaiter(t, ctx, admin, "INSERT INTO connector_operational_receipts")
	revoked := make(chan error, 1)
	go func() {
		_, e := owner.ControlSourceOfflineAuthorization(ctx, grant.ID, "revoked", grant.Revision, "")
		revoked <- e
	}()
	awaitManagedWaiter(t, ctx, admin, "FOR UPDATE")
	select {
	case e := <-revoked:
		t.Fatal("revocation returned before admitted transaction", e)
	default:
	}
	if e = block.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal("admitted transaction", e)
	}
	if e = <-revoked; e != nil {
		t.Fatal("serialized revoke", e)
	}
	if _, e = importer.ImportSourceFactsOffline(ctx, race, raw, time.Now()); !errors.Is(e, sourcefacts.ErrAuthorization) {
		t.Fatal("post-revoke replay", e)
	}
	if managedCount(t, admin, "connector_operational_receipts") != 2 || managedCount(t, admin, "source_fact_documents") != int64(len(page.Events)) {
		t.Fatal("commit/revoke outcome")
	}
	t.Log("receipt failure rolls back facts but retains immutable operation reservation; input rebinding rejected; DB-time expiry after source-lock wait writes nothing; admitted completion holds authorization lock until receipt commit; revocation blocks later replay")
}

type managedBlockingReader struct {
	cap, page        []byte
	started, release chan struct{}
}

func (r *managedBlockingReader) FetchSourceCapabilities(context.Context) ([]byte, error) {
	return r.cap, nil
}
func (r *managedBlockingReader) FetchSourceFacts(ctx context.Context, _ string, _ string, _ int) ([]byte, error) {
	close(r.started)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.release:
		return r.page, nil
	}
}

func TestManagedPauseOrdersInFlightCommit(t *testing.T) {
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
	admin := managedSQL(t, c.AdminDSN)
	owner := managedStore(t, c.AdminDSN)
	importer := managedStore(t, c.ImporterDSN)
	var source, id string
	if e := admin.QueryRow(`SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&source, &id); e != nil {
		t.Fatal(e)
	}
	a, _, page := managedOfflineFixture(t, source)
	s, e := owner.RegisterSourceFactsStream(ctx, sourcefacts.Stream{ID: id, SourceSystemID: source, SourceInstance: a.SourceInstance, OrgID: a.OrgID, ProjectID: a.ProjectID, Profile: a.Profile, ScopeGeneration: "1"})
	if e != nil || s.Status != "paused" {
		t.Fatal("explicit initial registration", s, e)
	}
	s, e = owner.ControlSourceFactsStream(ctx, id, "active", s.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	bootstrap := page
	bootstrap.Events = []json.RawMessage{}
	bootstrap.NextCursor = "bootstrap"
	bootstrap.HighWatermark = "bootstrap"
	if e = importer.CommitSourceFactsPage(ctx, s, bootstrap, time.Now()); e != nil {
		t.Fatal(e)
	}
	s, e = owner.LoadSourceFactsStream(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	page.NextCursor = "after-page"
	page.HighWatermark = "after-page"
	wire, _ := json.Marshal(page)
	cap, _ := json.Marshal(sourcefacts.Capabilities{SchemaVersion: "unirouter.source-capabilities.v1", SourceInstance: a.SourceInstance, Profiles: []string{a.Profile}, ProfileVersion: "1.0.0", Status: "active", CredentialStatus: "feed_active", ScopeGeneration: "1", RetentionSeconds: 604800, Bootstrap: "forward_only", FieldAuthorization: "fixed_profiles"})
	r := &managedBlockingReader{cap: cap, page: wire, started: make(chan struct{}), release: make(chan struct{})}
	testsourcefacts.BindSyncScope(t, admin, a.SourceInstance, a.OrgID, a.ProjectID, true)
	done := make(chan error, 1)
	go func() {
		_, e := (sourcefacts.Consumer{Store: importer, Reader: r, ExpectedStream: &s}).PullOnce(ctx, id)
		done <- e
	}()
	select {
	case <-r.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	paused, e := owner.ControlSourceFactsStream(ctx, id, "paused", s.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	close(r.release)
	if e = <-done; !errors.Is(e, sourcefacts.ErrStreamChanged) {
		t.Fatal("page committed after pause confirmation", e)
	}
	if managedCount(t, admin, "source_fact_documents") != 0 {
		t.Fatal("paused page wrote facts")
	}
	s, e = owner.ControlSourceFactsStream(ctx, id, "active", paused.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	// In the opposite ordering the commit owns the source lock; pause waits.
	managedExec(t, admin, `CREATE FUNCTION synthetic_source_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(91827365); RETURN NEW; END $$`)
	managedExec(t, admin, `CREATE TRIGGER synthetic_source_wait BEFORE INSERT ON source_fact_documents FOR EACH ROW EXECUTE FUNCTION synthetic_source_wait()`)
	block, e := admin.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback()
	if _, e = block.ExecContext(ctx, `SELECT pg_advisory_xact_lock(91827365)`); e != nil {
		t.Fatal(e)
	}
	attempt := s
	go func() { done <- importer.CommitSourceFactsPage(ctx, attempt, page, time.Now()) }()
	awaitManagedWaiter(t, ctx, admin, "INSERT INTO source_fact_documents")
	stop := make(chan error, 1)
	go func() { _, e := owner.ControlSourceFactsStream(ctx, id, "paused", s.Revision, false); stop <- e }()
	awaitManagedWaiter(t, ctx, admin, "pg_advisory_xact_lock(hashtextextended")
	select {
	case e := <-stop:
		t.Fatal("pause returned while commit held source lock", e)
	default:
	}
	if e = block.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if e = <-stop; e != nil {
		t.Fatal("pause after in-flight page", e)
	}
	s, e = owner.LoadSourceFactsStream(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	if s.Status != "paused" || s.Cursor != "after-page" {
		t.Fatal("pause confirmation before commit completion", s)
	}
	if e = importer.CommitSourceFactsPage(ctx, attempt, page, time.Now()); !errors.Is(e, sourcefacts.ErrStreamChanged) {
		t.Fatal("stale page after final pause", e)
	}
	if managedCount(t, admin, "source_fact_documents") != int64(len(page.Events)) {
		t.Fatal("serialized live count")
	}
}
