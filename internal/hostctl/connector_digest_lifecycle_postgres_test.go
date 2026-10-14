package hostctl

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/releasebundle"
	"github.com/jackc/pgx/v5"
)

func digestSyntheticCredentials(t *testing.T, f installFixture, c DatabaseCredentials) DatabaseCredentials {
	t.Helper()
	for file, value := range map[string]string{"postgres-reader.dsn": c.ReaderDSN, "postgres-workspace.dsn": c.WorkspaceDSN, "postgres-importer.dsn": c.ImporterDSN, "postgres-admin.dsn": c.AdminDSN} {
		writeHostFile(t, filepath.Join(f.paths.DatabaseSetsDir, "initial", file), []byte(value+"\n"), 0600)
	}
	loaded, err := LoadDatabaseCredentials(f.paths)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

// Signed fixture artifacts contain real migration bytes and placeholder host
// binaries. Only host/systemd and runtime identity responses are simulated;
// migrations, maintenance, role verification, pg_dump and PostgreSQL are real.
func digestSyntheticArtifact(t *testing.T, f installFixture, version string, schema int, from []string, legacy []releasebundle.LegacySource) string {
	t.Helper()
	payload := filepath.Join(f.paths.Root, "artifacts", "digest-payload-"+version)
	writeHostPayload(t, payload)
	if schema >= 32 {
		for _, name := range []string{"xinsight-source-facts.service", "source-facts.json.example"} {
			raw, err := os.ReadFile(filepath.Join("../../deploy", name))
			if err != nil {
				t.Fatal(err)
			}
			writeHostFile(t, filepath.Join(payload, "deploy", name), raw, 0644)
		}
	}
	if err := os.Remove(filepath.Join(payload, "migrations", "000001_create_test.up.sql")); err != nil {
		t.Fatal(err)
	}
	dir := connectorDigestMigrations(t, schema)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeHostFile(t, filepath.Join(payload, "migrations", entry.Name()), raw, 0600)
	}
	raw, err := os.ReadFile(filepath.Join(f.paths.Root, "artifacts", "release-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config releasebundle.ReleaseConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config.ProductVersion = version
	config.Manual.Version = version + "/manual-r1"
	config.DatabaseSchema = releasebundle.SchemaCompatibility{Current: strconv.Itoa(schema), Min: strconv.Itoa(schema), Max: strconv.Itoa(schema)}
	config.ApplicationCompatibility.DatabaseSchemaMin = strconv.Itoa(schema)
	config.ApplicationCompatibility.DatabaseSchemaMax = strconv.Itoa(schema)
	config.SupportedUpgradeFromVersions = from
	config.SupportedLegacySources = legacy
	if len(legacy) != 0 {
		config.Rollback.ApplicationRollbackCompatible = false
	}
	contractPath := "clean_baseline_schema_contract.json"
	if schema == 29 {
		contractPath = "schema29_contract.json"
	}
	contractRaw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		SchemaVersion      string `json:"schema_version"`
		SemanticSHA256     string `json:"semantic_sha256"`
		MigrationSetSHA256 string `json:"migration_set_sha256"`
	}
	if err := json.Unmarshal(contractRaw, &contract); err != nil {
		t.Fatal(err)
	}
	config.SchemaContract = releasebundle.SchemaContractIdentity{SchemaVersion: contract.SchemaVersion, SemanticSHA256: contract.SemanticSHA256, MigrationSetSHA256: contract.MigrationSetSHA256}
	writeHostFile(t, filepath.Join(payload, "release", "schema-contract.json"), contractRaw, 0600)
	for _, path := range []string{"deploy/xinsight-feed.service", "deploy/xinsight-litellm-feed.service", "deploy/xinsight-litellm-feed.env.example", "deploy/xinsight-connector-controller.service", "schemas/integration/connector-checkpoint-epoch-prepare-v1.schema.json"} {
		raw, err := os.ReadFile(filepath.Join("../..", path))
		if err != nil {
			t.Fatal(err)
		}
		writeHostFile(t, filepath.Join(payload, path), raw, 0600)
	}
	configPath := filepath.Join(f.paths.Root, "artifacts", "digest-config-"+version+".json")
	writeHostJSON(t, configPath, config, 0600)
	artifact := filepath.Join(f.paths.Root, "artifacts", "digest-fixture-"+version+".tar.gz")
	if _, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: f.privateKey, OutputPath: artifact,
		Commit: "dddddddddddddddddddddddddddddddddddddddd", SourceDateEpoch: f.now.Add(-30 * time.Minute),
		BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
	}); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func digestFixtureRuntime(t *testing.T, f installFixture, artifact string) {
	t.Helper()
	target, err := f.engine.verifyTarget(artifact)
	if err != nil {
		t.Fatal(err)
	}
	f.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: target.Identity}}
}

func TestConnectorDigestLegacyAdopt(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	f := newInstallFixture(t)
	c = digestSyntheticCredentials(t, f, c)
	manager := PostgresManager{Runner: ExecRunner{}}
	f.engine.Database = manager
	if err := manager.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	migrations := connectorDigestMigrations(t, 9)
	if _, err := db.RunMigrations(ctx, c.AdminDSN, migrations); err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	admin.SetMaxIdleConns(0)
	if _, err := admin.Exec(`UPDATE schema_migrations SET checksum_sha256=NULL`); err != nil {
		t.Fatal(err)
	}
	legacyArtifact, files, source := packLegacyRelease(t, f, migrations)
	legacyDirectory := filepath.Join(f.paths.ReleasesDir, "xinsight-release-linux-amd64-"+source.Commit)
	for name, contents := range files {
		mode := os.FileMode(0644)
		if len(name) > 4 && name[:4] == "bin/" {
			mode = 0755
		}
		writeHostFile(t, filepath.Join(legacyDirectory, filepath.FromSlash(name)), contents, mode)
	}
	if err := atomicSymlink(legacyDirectory, f.paths.CurrentLink); err != nil {
		t.Fatal(err)
	}
	writeHostFile(t, f.paths.ServiceUnit, []byte("[Service]\n"), 0644)
	writeHostFile(t, f.paths.EnvironmentFile, []byte("XINSIGHT_POSTGRES_DSN="+c.AdminDSN+"\nXINSIGHT_MIGRATIONS_DIR=/opt/x-insight/current/migrations\n"), 0600)
	target := digestSyntheticArtifact(t, f, "0.3.0-rc.99", 29, nil, []releasebundle.LegacySource{source})
	before, err := manager.Inspect(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	check, err := f.engine.CheckAdopt(ctx, target, legacyArtifact)
	if err != nil || !check.Supported || check.Mutating {
		t.Fatalf("legacy adopt check: %v", err)
	}
	after, err := manager.Inspect(ctx, c)
	if err != nil || !reflect.DeepEqual(before.MigrationLedger, after.MigrationLedger) {
		t.Fatal("adopt check mutated ledger")
	}
	if _, err := f.engine.Adopt(ctx, target, legacyArtifact); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, c); err != nil {
		t.Fatal(err)
	}
	ledger, err := db.ReadMigrationLedger(ctx, c.AdminDSN)
	if err != nil || len(ledger) != 9 {
		t.Fatal("adopt changed legacy schema")
	}
	for i, row := range ledger {
		if row.MigrationChecksum != source.MigrationChecksums[i] {
			t.Fatal("adopt did not bind legacy checksums")
		}
	}
	digestFixtureRuntime(t, f, target)
	if _, err := f.engine.CheckUpgrade(ctx, target); err != nil {
		t.Fatalf("adopted source cannot enter upgrade: %v", err)
	}
	if _, err := f.engine.Upgrade(ctx, target); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, c); err != nil {
		t.Fatal(err)
	}
	t.Log("real schema9 legacy ledger adoption and forward upgrade through Engine passed; fixture-signed identities and host runtime responses are synthetic, not published release qualification")
}

func TestConnectorDigestEngineCheckAndResume(t *testing.T) {
	for _, phase := range []string{"migrate_database", "apply_role_grants"} {
		t.Run(phase, func(t *testing.T) {
			ctx, c := connectorDigestDatabase(t)
			f := newInstallFixture(t)
			c = digestSyntheticCredentials(t, f, c)
			manager := PostgresManager{Runner: ExecRunner{}}
			f.engine.Database = manager
			source := digestSyntheticArtifact(t, f, "0.3.0-rc.13", 23, nil, nil)
			digestFixtureRuntime(t, f, source)
			if _, err := f.engine.Install(ctx, source); err != nil {
				t.Fatal(err)
			}
			admin, err := sql.Open("pgx", c.AdminDSN)
			if err != nil {
				t.Fatal(err)
			}
			defer admin.Close()
			admin.SetMaxIdleConns(0)
			if _, err := admin.Exec(`REVOKE EXECUTE ON FUNCTION public.digest(text,text) FROM xinsight_reader`); err != nil {
				t.Fatal(err)
			}
			target := digestSyntheticArtifact(t, f, "0.3.0-rc.99", 29, []string{"0.3.0-rc.13"}, nil)
			before, err := manager.PreviewCleanBaseline(ctx, c)
			// The current clean-baseline manifest targets schema29, so schema23
			// is deliberately not certified by the target preview.
			if err == nil {
				t.Fatal("target preview unexpectedly certified schema23")
			}
			_ = before
			fingerprint, err := manager.EvidenceFingerprint(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := manager.Inspect(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			journal, err := f.engine.readActiveJournal()
			if err != nil {
				t.Fatal(err)
			}
			runner := f.engine.Runner.(*fakeRunner)
			commandCount := len(runner.commands)
			report, err := f.engine.CheckUpgrade(ctx, target)
			if err != nil || !report.Supported || report.Mutating {
				t.Fatalf("missing digest blocked repair check: %v", err)
			}
			afterFacts, err := manager.Inspect(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			facts.DatabaseSizeBytes, afterFacts.DatabaseSizeBytes = 0, 0
			afterJournal, err := f.engine.readActiveJournal()
			if err != nil || !reflect.DeepEqual(journal, afterJournal) || !reflect.DeepEqual(facts, afterFacts) {
				t.Fatal("check mutated journal or database facts")
			}
			assertNoMutatingHostCommands(t, runner.commands[commandCount:])
			var granted bool
			if err := admin.QueryRow(`SELECT has_function_privilege('xinsight_reader','public.digest(text,text)','EXECUTE')`).Scan(&granted); err != nil || granted {
				t.Fatal("check silently repaired grants")
			}
			if _, err := admin.Exec(`GRANT EXECUTE ON FUNCTION public.digest(bytea,text) TO xinsight_reader`); err != nil {
				t.Fatal(err)
			}
			if _, err := f.engine.CheckUpgrade(ctx, target); err == nil {
				t.Fatal("check accepted source overgrant")
			}
			if _, err := admin.Exec(`REVOKE EXECUTE ON FUNCTION public.digest(bytea,text) FROM xinsight_reader; ALTER FUNCTION public.digest(text,text) SECURITY DEFINER`); err != nil {
				t.Fatal(err)
			}
			if _, err := f.engine.CheckUpgrade(ctx, target); err == nil {
				t.Fatal("check accepted abnormal source identity")
			}
			if _, err := admin.Exec(`ALTER FUNCTION public.digest(text,text) SECURITY INVOKER`); err != nil {
				t.Fatal(err)
			}
			digestFixtureRuntime(t, f, target)
			t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", phase)
			_, upgradeErr := f.engine.Upgrade(ctx, target)
			if upgradeErr == nil {
				t.Fatal("missing injected phase interruption")
			}
			failed, err := f.engine.readActiveJournal()
			if err != nil || !containsString(failed.CompletedPhases, phase) {
				t.Fatalf("durable failure checkpoint missing: upgrade=%v journal=%v", upgradeErr, err)
			}
			t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
			if _, err := f.engine.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			recovered, err := f.engine.readActiveJournal()
			if err != nil || !reflect.DeepEqual(failed, recovered) {
				t.Fatal("read-only recovery inspection mutated journal")
			}
			if _, err := admin.Exec(`ALTER FUNCTION public.digest(text,text) SECURITY DEFINER`); err != nil {
				t.Fatal(err)
			}
			if _, err := f.engine.Resume(ctx); err == nil || !strings.Contains(err.Error(), "connector digest function identity is invalid") {
				t.Fatalf("resume did not reject abnormal digest identity: %v", err)
			}
			if _, err := admin.Exec(`ALTER FUNCTION public.digest(text,text) SECURITY INVOKER`); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec(`REVOKE EXECUTE ON FUNCTION public.digest(text,text) FROM xinsight_reader`); err != nil {
				t.Fatal(err)
			}
			if _, err := f.engine.Resume(ctx); err != nil {
				t.Fatal(err)
			}
			if err := manager.VerifyRoleBoundaries(ctx, c); err != nil {
				t.Fatal(err)
			}
			after, err := manager.EvidenceFingerprint(ctx, c)
			if err != nil || after != fingerprint {
				t.Fatal("upgrade changed evidence fingerprint")
			}
			t.Log("actual PG source23→target29: install, read-only CheckUpgrade, source drift rejection, native backup, interrupted upgrade and journal resume passed; host binaries/runtime proof simulated")
		})
	}
}

func TestConnectorDigestNativeRestoreCheckpointPreviewAndRC10(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if err := manager.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, 20)); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	qualifyRC10SourcePrivilegeUpgrade(t, ctx, manager, c)
	if _, err := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	// Provision grants for the newly migrated resource identity function before
	// checking the current contract; the second application must be idempotent.
	if err := manager.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	beforeContract, err := InspectCleanBaselineSchemaContract(ctx, admin)
	if err != nil || !beforeContract.Valid {
		t.Fatalf("current schema contract invalid after provisioning: error=%v mismatches=%+v", err, beforeContract.Mismatches)
	}
	if err := manager.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	afterContract, err := InspectCleanBaselineSchemaContract(ctx, admin)
	if err != nil || !reflect.DeepEqual(beforeContract, afterContract) {
		t.Fatal("digest grant changed immutable schema contract")
	}
	input := checkpointepoch.EmptyBootstrapInput{PrepareInput: checkpointepoch.PrepareInput{
		Binding: cleanBaselineBinding(), WorkerUnit: checkpointepoch.CompletedExecutionWorkerUnit,
		NextSourceInstance: "synthetic-digest-restore", NextCursor: "合成游标é🙂'", NextHighWatermark: "合成游标é🙂'",
		AuthorizationRef: "decision://local-synthetic-digest-repair",
	}, CheckpointEpoch: 5}
	if _, err := checkpointepoch.BootstrapEmpty(ctx, admin, input, time.Now()); err != nil {
		t.Fatal(err)
	}
	preview, err := manager.PreviewCleanBaseline(ctx, c)
	if err != nil || preview.CheckpointFloor.HistoricalMaxEpoch != 5 || preview.CheckpointFloor.PriorIdentityRows != 1 {
		t.Fatalf("admin checkpoint preview: %v", err)
	}
	if !containsString(preview.CheckpointFloor.PriorCursorSHA256, hashCleanBaselineText(input.NextCursor)) || !containsString(preview.CheckpointFloor.PriorHighWatermarkSHA256, hashCleanBaselineText(input.NextHighWatermark)) {
		t.Fatal("checkpoint identity hashes changed")
	}
	readonly := c
	u, _ := url.Parse(c.AdminDSN)
	query := u.Query()
	query.Set("default_transaction_read_only", "on")
	u.RawQuery = query.Encode()
	readonly.AdminDSN = u.String()
	readOnlyPreview, err := manager.PreviewCleanBaseline(ctx, readonly)
	if err != nil || !reflect.DeepEqual(preview, readOnlyPreview) {
		t.Fatal("read-only preview failed or changed result")
	}
	reader, err := db.OpenEvidenceStore(ctx, c.ReaderDSN)
	if err != nil {
		t.Fatal(err)
	}
	items, err := reader.ListConnectorTrust(ctx, 100)
	reader.Close()
	if err != nil || len(items) != 1 {
		t.Fatal("source Connectors reader failed")
	}
	backup, err := manager.Backup(ctx, c, filepath.Join(t.TempDir(), "digest.dump"))
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("xinsight_restore_digest_%d", time.Now().UnixNano())
	f := newInstallFixture(t)
	c = digestSyntheticCredentials(t, f, c)
	restored, err := StageRestoredCredentialSet(f.paths, c, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(`DROP DATABASE IF EXISTS ` + pgx.Identifier{name}.Sanitize() + ` WITH (FORCE)`); err != nil {
			t.Error(err)
		}
	})
	expected, err := db.LoadMigrationChecksums("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RestoreToNewDatabase(ctx, c, restored, backup.Path, "../../migrations", expected); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, restored); err != nil {
		t.Fatal(err)
	}
	reader, err = db.OpenEvidenceStore(ctx, restored.ReaderDSN)
	if err != nil {
		t.Fatal(err)
	}
	restoredItems, err := reader.ListConnectorTrust(ctx, 100)
	reader.Close()
	if err != nil || !reflect.DeepEqual(items, restoredItems) {
		t.Fatal("restored Connectors data or hashes differ")
	}
	afterPreview, err := manager.PreviewCleanBaseline(ctx, c)
	if err != nil || !reflect.DeepEqual(preview, afterPreview) {
		t.Fatal("preview/backup/restore mutated original database")
	}
	t.Log("actual PG16 rc.10 role contract retained; current schema contract unchanged; admin text checkpoint identities and enforced read-only preview passed; native no-ACL new-database restore regranted reader and preserved exact Connectors projection")
}
