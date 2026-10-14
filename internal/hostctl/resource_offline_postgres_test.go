package hostctl

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/jackc/pgx/v5"
)

func TestSyntheticResourceOfflineRolesAtomicityAndRestore(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	m := PostgresManager{Runner: ExecRunner{}}
	if err := m.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, 36)); err != nil {
		t.Fatal(err)
	}
	admin := managedSQL(t, c.AdminDSN)
	var reporter, target, name, oid, systemID string
	if err := admin.QueryRow(`SELECT gen_random_uuid()::text,gen_random_uuid()::text,current_database(),oid::text FROM pg_database WHERE datname=current_database()`).Scan(&reporter, &target, &name, &oid); err != nil {
		t.Fatal(err)
	}
	managedExec(t, admin, `INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1::uuid,$2,'unirouter','existing target')`, target, "source-system:"+target)
	var preserved string
	if err := admin.QueryRow(`SELECT to_jsonb(s)::text FROM source_systems s WHERE id=$1::uuid`, target).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if n, err := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil || n != currentMigrationSchema(t)-36 {
		t.Fatal("actual schema36 predecessor", n, err)
	}
	if err := m.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyRoleBoundaries(ctx, c); err != nil {
		t.Fatal(err)
	}
	managedExec(t, admin, `GRANT EXECUTE ON FUNCTION public.synthetic_resource_cluster_id() TO xinsight_workspace`)
	if _, err := GenerateCleanBaselineSchemaContract(ctx, admin); err == nil {
		t.Fatal("broadened resource identity privilege accepted")
	}
	if err := m.VerifyRoleBoundaries(ctx, c); err == nil {
		t.Fatal("broadened resource identity privilege not detected")
	}
	if err := m.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	converged, err := GenerateCleanBaselineSchemaContract(ctx, admin)
	if err != nil || converged.SemanticSHA256 != ExpectedCleanBaselineSchemaContract().SemanticSHA256 {
		t.Fatalf("current schema role-converged contract differs: %v", err)
	}
	for _, grant := range []string{
		`GRANT EXECUTE ON FUNCTION public.synthetic_resource_cluster_id() TO PUBLIC`,
		`GRANT EXECUTE ON FUNCTION public.synthetic_resource_cluster_id() TO xinsight_reader`,
		`GRANT EXECUTE ON FUNCTION public.synthetic_resource_cluster_id() TO xinsight_importer WITH GRANT OPTION`,
	} {
		managedExec(t, admin, grant)
		if _, err := GenerateCleanBaselineSchemaContract(ctx, admin); err == nil {
			t.Fatalf("unauthorized synthetic function grant accepted: %s", grant)
		}
		if err := m.VerifyRoleBoundaries(ctx, c); err == nil {
			t.Fatalf("unauthorized synthetic function grant passed role check: %s", grant)
		}
		if err := m.ApplyGrants(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	managedExec(t, admin, `REVOKE EXECUTE ON FUNCTION public.synthetic_resource_cluster_id() FROM xinsight_importer`)
	migrationOnly, err := GenerateCleanBaselineSchemaContract(ctx, admin)
	if err != nil || migrationOnly.SemanticSHA256 == converged.SemanticSHA256 {
		t.Fatalf("migration-only function ACL not distinguished from role convergence: %v", err)
	}
	if inspection, err := InspectCleanBaselineSchemaContract(ctx, admin); err != nil || inspection.Valid {
		t.Fatalf("unconverged importer grant passed schema inspection: %+v %v", inspection, err)
	}
	if err := m.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	if inspection, err := InspectCleanBaselineSchemaContract(ctx, admin); err != nil || !inspection.Valid {
		t.Fatalf("restored role-converged contract failed: %+v %v", inspection, err)
	}
	var after string
	_ = admin.QueryRow(`SELECT to_jsonb(s)::text FROM source_systems s WHERE id=$1::uuid`, target).Scan(&after)
	if after != preserved {
		t.Fatal("upgrade changed existing source")
	}
	if err := admin.QueryRow(`SELECT public.synthetic_resource_cluster_id()`).Scan(&systemID); err != nil {
		t.Fatal(err)
	}
	owner, importer := managedStore(t, c.AdminDSN), managedStore(t, c.ImporterDSN)
	scope := workbench.ResourceScope{SourceSystemID: reporter, SourceInstance: "synthetic-reporter", OrgID: "17", ProjectID: "101"}
	targetScope := workbench.ResourceScope{SourceSystemID: target, SourceInstance: "synthetic-target", OrgID: "18", ProjectID: "102"}
	record := workbench.ResourceAccessRecord{Version: workbench.ResourceAccessVersion, Synthetic: true, RecordRef: "resource-1", Revision: 1, Reporter: scope, Target: targetScope, RequestID: "request-1", ApplicationRef: "app-1", ActorRef: "actor-1", OccurredAt: time.Now().UTC().Truncate(time.Microsecond), ReportKind: "source_audit", Resource: workbench.ResourceObject{Ref: "document-1", Version: "v1", Name: "Synthetic only", Location: "enterprise_service", Ownership: "unknown", Classification: "unknown", DeviceManagement: "unknown"}, Action: "read", AccessOutcome: "unknown", AccessDecision: "unknown"}
	encode := func(records ...workbench.ResourceAccessRecord) []byte {
		raw, err := json.Marshal(map[string]any{"schema_version": sourcefacts.ResourceOfflineBundleVersion, "records": records})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := encode(record)
	now := time.Now().UTC()
	a := sourcefacts.OfflineAuthorization{SchemaVersion: sourcefacts.ResourceOfflineAuthorizationVersion, AuthorizationID: "resource-test", Environment: "test", TargetRef: "resource-target", MachineIDSHA256: sourcefacts.Hash([]byte("machine")), ProductVersion: "dev", ProductCommit: "resource-build", SourceSchemaSHA256: sourcefacts.ResourceSchemaSHA256(), SourceSystemID: reporter, SourceInstance: scope.SourceInstance, OrgID: scope.OrgID, ProjectID: scope.ProjectID, Profile: sourcefacts.ResourceOfflineProfile, NotBefore: now.Add(-time.Minute).Format(sourcefacts.TimeFormat), ExpiresAt: now.Add(time.Hour).Format(sourcefacts.TimeFormat), BundlePath: "/synthetic/resource.json", BundleSHA256: sourcefacts.Hash(raw), BundleID: "resource-bundle", OperationID: "resource-operation", MaxRecords: 50, PayloadCaptureMode: "metadata_only", TargetDatabaseSchema: buildinfo.DatabaseSchemaMax, TargetDatabaseName: name, TargetDatabaseOID: oid, TargetDatabaseSystemID: systemID, TargetSourceSystemID: target, TargetSourceInstance: targetScope.SourceInstance, TargetOrgID: targetScope.OrgID, TargetProjectID: targetScope.ProjectID}
	activate := func(v sourcefacts.OfflineAuthorization) db.SourceOfflineRegistration {
		r, err := owner.RegisterSourceOfflineAuthorization(ctx, v)
		if err != nil {
			t.Fatal("register", err)
		}
		r, err = owner.ControlSourceOfflineAuthorization(ctx, v.AuthorizationID, "active", r.Revision, v.Digest(), v)
		if err != nil {
			t.Fatal("enable", err)
		}
		return r
	}
	for _, dsn := range []string{c.ReaderDSN, c.WorkspaceDSN, c.ImporterDSN} {
		if _, err := managedStore(t, dsn).RegisterSourceOfflineAuthorization(ctx, a); err == nil {
			t.Fatal("non-admin registered authority")
		}
	}
	for _, change := range []func(*sourcefacts.OfflineAuthorization){func(v *sourcefacts.OfflineAuthorization) { v.TargetDatabaseName = "another_database" }, func(v *sourcefacts.OfflineAuthorization) { v.TargetDatabaseOID = "1" }, func(v *sourcefacts.OfflineAuthorization) { v.TargetDatabaseSystemID = "1" }} {
		v := a
		change(&v)
		before := managedOfflineDatabaseState(t, admin)
		if _, err := owner.RegisterSourceOfflineAuthorization(ctx, v); err == nil {
			t.Fatal("different live database identity registered")
		}
		if !reflect.DeepEqual(before, managedOfflineDatabaseState(t, admin)) {
			t.Fatal("rejected registration wrote")
		}
	}
	grant := activate(a)
	before := managedOfflineDatabaseState(t, admin)
	if n, err := importer.CheckSyntheticResources(ctx, a, raw, time.Now()); err != nil || n != 1 {
		t.Fatal("check", n, err)
	}
	if !reflect.DeepEqual(before, managedOfflineDatabaseState(t, admin)) {
		t.Fatal("check wrote")
	}
	if _, err := importer.ImportSourceFactsOffline(ctx, a, raw, time.Now()); err == nil {
		t.Fatal("UR A/B entry accepted resource profile")
	}
	for _, dsn := range []string{c.ReaderDSN, c.WorkspaceDSN} {
		if _, err := managedStore(t, dsn).ImportSyntheticResources(ctx, a, raw, time.Now()); err == nil {
			t.Fatal("non-importer wrote")
		}
	}
	receipt, err := importer.ImportSyntheticResources(ctx, a, raw, time.Now())
	if err != nil || receipt.Counts.Inserted != 1 {
		t.Fatal("formal resource import", receipt, err)
	}
	replay, err := importer.ImportSyntheticResources(ctx, a, raw, time.Now())
	if err != nil || !reflect.DeepEqual(receipt, replay) {
		t.Fatal("immutable replay", err)
	}
	if _, err = importer.StoreSyntheticResourceRecord(ctx, record, time.Now()); err == nil {
		t.Fatal("original qualification database guard removed")
	}
	// Rejections include a previously unseen first record followed by a conflict.
	for i, kind := range []string{"not-synthetic", "reporter", "target", "alias", "existing-conflict", "adjacent-conflict"} {
		t.Run(kind, func(t *testing.T) {
			v := a
			v.AuthorizationID = fmt.Sprintf("resource-invalid-%d", i)
			v.OperationID = v.AuthorizationID
			changed := record
			changed.RecordRef = "new-record"
			switch kind {
			case "not-synthetic":
				changed.Synthetic = false
			case "reporter":
				changed.Reporter.OrgID = "99"
			case "target":
				changed.Target.SourceInstance = "other"
			}
			payload := encode(changed)
			switch kind {
			case "alias":
				payload = []byte(strings.Replace(string(payload), `"synthetic":true`, `"synthetic":true,"SYNTHETIC":true`, 1))
			case "existing-conflict":
				conflict := record
				conflict.Resource.Name = "changed"
				payload = encode(changed, conflict)
			case "adjacent-conflict":
				next := changed
				next.Revision = 2
				next.PreviousHash = "sha256:" + strings.Repeat("0", 64)
				payload = encode(changed, next)
			}
			v.BundleSHA256 = sourcefacts.Hash(payload)
			activate(v)
			snapshot := managedOfflineDatabaseState(t, admin)
			if _, err := importer.CheckSyntheticResources(ctx, v, payload, time.Now()); err == nil {
				t.Fatal("invalid check accepted")
			}
			if _, err := importer.ImportSyntheticResources(ctx, v, payload, time.Now()); err == nil {
				t.Fatal("invalid batch accepted")
			}
			if !reflect.DeepEqual(snapshot, managedOfflineDatabaseState(t, admin)) {
				t.Fatal("invalid batch left records/binding/receipt")
			}
		})
	}
	// Missing predecessors remain explicit chain gaps, as in the existing contract.
	next := record
	next.Revision = 3
	next.PreviousHash = "sha256:" + strings.Repeat("a", 64)
	gap := encode(next)
	v := a
	v.AuthorizationID = "resource-gap"
	v.OperationID = "resource-gap"
	v.BundleSHA256 = sourcefacts.Hash(gap)
	activate(v)
	if _, err := importer.ImportSyntheticResources(ctx, v, gap, time.Now()); err != nil {
		t.Fatal("existing gap semantics changed", err)
	}
	if err := db.ValidateWorkbenchRestore(ctx, admin); err != nil {
		t.Fatal("accepted input cannot restore", err)
	}
	t.Run("expires-while-waiting-for-source-lock", func(t *testing.T) {
		v := a
		v.AuthorizationID = "resource-expiring"
		v.OperationID = "resource-expiring"
		until := time.Now().UTC().Add(500 * time.Millisecond)
		v.NotBefore = time.Now().UTC().Add(-time.Second).Format(sourcefacts.TimeFormat)
		v.ExpiresAt = until.Format(sourcefacts.TimeFormat)
		activate(v)
		before := managedOfflineDatabaseState(t, admin)
		tx, err := admin.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "source-facts:"+reporter); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := importer.ImportSyntheticResources(ctx, v, raw, time.Now()); done <- err }()
		awaitManagedWaiter(t, ctx, admin, "pg_advisory_xact_lock(hashtextextended")
		time.Sleep(time.Until(until) + 20*time.Millisecond)
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err = <-done; err == nil {
			t.Fatal("expired authority accepted after source lock")
		}
		if !reflect.DeepEqual(before, managedOfflineDatabaseState(t, admin)) {
			t.Fatal("expired admission left durable operation state")
		}
	})
	// An import revocation blocks replay without withdrawing the stored record.
	if _, err := owner.ControlSourceOfflineAuthorization(ctx, a.AuthorizationID, "revoked", grant.Revision, ""); err != nil {
		t.Fatal(err)
	}
	before = managedOfflineDatabaseState(t, admin)
	if _, err := importer.ImportSyntheticResources(ctx, a, raw, time.Now()); err == nil {
		t.Fatal("revoked replay accepted")
	}
	if !reflect.DeepEqual(before, managedOfflineDatabaseState(t, admin)) {
		t.Fatal("revocation denial wrote")
	}
	// Restore uses the supported new-database path and pauses the active gap grant.
	backup, err := m.Backup(ctx, c, filepath.Join(t.TempDir(), "resources.dump"))
	if err != nil {
		t.Fatal(err)
	}
	f := newInstallFixture(t)
	bound := digestSyntheticCredentials(t, f, c)
	restoredName := fmt.Sprintf("xinsight_restore_resources_%d", time.Now().UnixNano())
	restored, err := StageRestoredCredentialSet(f.paths, bound, restoredName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{restoredName}.Sanitize() + " WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	expected, err := db.LoadMigrationChecksums("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.RestoreToNewDatabase(ctx, c, restored, backup.Path, "../../migrations", expected); err != nil {
		t.Fatal("resource restore", err)
	}
	restoredSQL := managedSQL(t, restored.AdminDSN)
	recovered := managedStore(t, restored.AdminDSN)
	if managedCount(t, restoredSQL, "resource_access_records") != 2 {
		t.Fatal("resource restore lost records")
	}
	r, err := recovered.LoadSourceOfflineRegistration(ctx, v.AuthorizationID)
	if err != nil || r.Status != "disabled" {
		t.Fatal("restore did not pause", r, err)
	}
	if _, err = recovered.ControlSourceOfflineAuthorization(ctx, v.AuthorizationID, "active", r.Revision, v.Digest(), v); err == nil {
		t.Fatal("old database binding re-enabled")
	}
	v.AuthorizationID = "resource-rebound"
	v.OperationID = "resource-rebound"
	v.TargetDatabaseName = restoredName
	if err = restoredSQL.QueryRow(`SELECT oid::text FROM pg_database WHERE datname=current_database()`).Scan(&v.TargetDatabaseOID); err != nil {
		t.Fatal(err)
	}
	rebound, err := recovered.RegisterSourceOfflineAuthorization(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = recovered.ControlSourceOfflineAuthorization(ctx, v.AuthorizationID, "active", rebound.Revision, v.Digest(), v); err != nil {
		t.Fatal(err)
	}
	restoredImporter := managedStore(t, restored.ImporterDSN)
	replayed, err := restoredImporter.ImportSyntheticResources(ctx, v, gap, time.Now())
	if err != nil || replayed.Counts.DuplicateNoop != 1 {
		t.Fatal("rebound import", err)
	}
}
