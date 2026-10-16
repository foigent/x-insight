package hostctl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/releasebundle"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

type directFPV9FixtureRunner struct {
	base       CommandRunner
	properties map[string]map[string]string
}

type directFPV9ChangedEvidenceDatabase struct {
	DatabaseManager
}

func (directFPV9ChangedEvidenceDatabase) EvidenceFingerprint(context.Context, DatabaseCredentials) (EvidenceFingerprint, error) {
	return EvidenceFingerprint{SHA256: "sha256:" + strings.Repeat("f", 64)}, nil
}

func (runner directFPV9FixtureRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	if command.Name == "systemctl" && len(command.Args) == 5 && command.Args[0] == "show" &&
		command.Args[2] == "--property" && command.Args[4] == "--value" {
		if values, ok := runner.properties[command.Args[1]]; ok {
			if value, ok := values[command.Args[3]]; ok {
				return CommandResult{Output: value + "\n"}, nil
			}
		}
	}
	return runner.base.Run(ctx, command)
}

func TestDirectFPV9ExactReproductionCheckAndResumeAfterMigration(t *testing.T) {
	for _, scenario := range []struct {
		name, phase     string
		changedEvidence bool
		mustReject      bool
	}{
		{"migration_resume", "migrate_database", false, false},
		{"runtime_files_resume", "install_runtime_files", false, false},
		{"managed_state_resume", "write_install_state", false, false},
		{"pre_state_evidence_drift", "migrate_database", true, true},
		{"post_state_new_evidence", "write_receipt", true, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runDirectFPV9ExactReproductionCheckAndResume(t, scenario.phase, scenario.changedEvidence, scenario.mustReject)
		})
	}
}

func runDirectFPV9ExactReproductionCheckAndResume(t *testing.T, interruptionPhase string, changedEvidence bool, mustReject bool) {
	reproduction := os.Getenv("XI_FPV9_REPRO_DIR")
	if reproduction == "" {
		t.Skip("requires locally reproduced exact FPV9 binaries; no Target fetch")
	}
	fixture := newInstallFixture(t)
	writeSyntheticAccessCredential(t, fixture.paths.AccessAccountsFile)
	root := fixture.paths.Root
	buildDir := filepath.Join(root, "exact-old-build")
	for name, expected := range map[string]string{"xinsight-server": directFPV9ServerSHA256, "xinsight-importer": directFPV9WorkerSHA256} {
		raw, err := os.ReadFile(filepath.Join(reproduction, name))
		if err != nil {
			t.Fatal(err)
		}
		writeHostFile(t, filepath.Join(buildDir, name), raw, 0o755)
		if digest, _, err := fileSHA256(filepath.Join(buildDir, name), 256<<20); err != nil || digest != expected {
			t.Fatalf("locally reproduced binary differs from exact Target: %s %v", digest, err)
		}
	}
	// Keep the exact published FPV11 metadata and migrations without requiring
	// its original private Git history in the independent source repository.
	source := publishedFPV11ResumeSource(t)
	payload := filepath.Join(root, "artifacts", "fpv11-payload")
	for _, directory := range []string{"migrations", "deploy", "schemas", "fixtures"} {
		if err := os.CopyFS(filepath.Join(payload, directory), os.DirFS(filepath.Join(source, directory))); err != nil {
			t.Fatalf("copy %s: %v", directory, err)
		}
	}
	if err := os.CopyFS(filepath.Join(payload, "manual", "zh-CN"), os.DirFS(filepath.Join(source, "internal/webui/manual/zh-CN"))); err != nil {
		t.Fatal(err)
	}
	for destination, source := range map[string]string{
		"release/compatibility-matrix.json": filepath.Join(source, "release/d-validation-compatibility-matrix.json"),
		"release/release-notes.md":          filepath.Join(source, "release/d-validation-notes.md"),
		"release/schema-contract.json":      filepath.Join(source, "internal/hostctl/development_schema_contract.json"),
	} {
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		writeHostFile(t, filepath.Join(payload, destination), raw, 0o644)
	}
	for _, name := range []string{"xinsight-server", "xinsight-importer", "xinsightctl", "xinsight-launcher"} {
		writeHostFile(t, filepath.Join(payload, "bin", name), []byte("synthetic signed FPV11 "+name+"\n"), 0o755)
	}
	artifact := filepath.Join(root, "artifacts", "fpv11-test-signed.tar.gz")
	if _, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir: payload, ConfigPath: filepath.Join(source, "release/d-validation-config.json"), SigningKeyPath: fixture.privateKey,
		OutputPath: artifact, Commit: strings.Repeat("d", 40), SourceDateEpoch: fixture.now.Add(-30 * 60 * 1e9),
		BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.12", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
	}); err != nil {
		t.Fatalf("pack ephemeral test-signed FPV11 target: %v", err)
	}
	config := sourcefacts.ManagedConfig{
		SchemaVersion: sourcefacts.ManagedContentConfigVersion, PollIntervalSeconds: 5, RetryMaximumSeconds: 60,
		Profiles: []sourcefacts.ManagedProfileConfig{}, Bindings: []sourcefacts.ManagedCredentialBinding{{
			Name: "content-test", Scope: "content", SourceSystemID: "11111111-1111-1111-1111-111111111111",
			SourceInstance: "synthetic", OrgID: "2", ProjectID: "86", CredentialID: "synthetic-1",
			RegistrationHash: sourcefacts.Hash([]byte("synthetic registration")), KeyFile: filepath.Join(root, "content.key"),
		}},
		Contents: []sourcefacts.ManagedContentConfig{{
			CollectionID: "7996279c-df2b-4c0d-aaba-7c2a3b3d510b", SourceSystemID: "11111111-1111-1111-1111-111111111111",
			SourceInstance: "synthetic", OrgID: "2", ProjectID: "86", CredentialBinding: "content-test",
			ContentScopeRevision: "2", RecoveryEpoch: "1", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview},
			CoverageFrom: sourcefacts.ContentBoundary{Time: "2026-09-28T04:19:05.454353Z", AfterSequence: "0"},
			Endpoint:     "https://8.130.18.85" + sourcefacts.ContentEndpointPath,
		}},
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("synthetic source config: %v", err)
	}
	writeHostJSON(t, fixture.paths.SourceFactsConfig, config, 0o644)
	if err := fixture.engine.installSourceFactsCredentials(config); err != nil {
		t.Fatal(err)
	}
	writeHostFile(t, fixture.paths.TrustedTLSProxyEnv,
		[]byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xinsight.example\n"), 0o644)
	proof := DirectFPV9SourceProof{
		SchemaVersion: "xinsight.unmanaged-fpv9-source.v1", ProductVersion: "0.2.0-dev.fpv9", Commit: directFPV9Commit,
		DatabaseName: "xinsight", SystemIdentifier: "123456789", EgressAllowCIDR: "8.130.18.85/32",
		ServerBinary: filepath.Join(buildDir, "xinsight-server"), WorkerBinary: filepath.Join(buildDir, "xinsight-importer"),
		Files: map[string]string{},
	}
	for role, path := range directFPV9Paths(fixture.paths) {
		if role == "service_content_dropin" || role == "source_facts_credentials_dropin" || role == "source_facts_config" {
			// Produced by the existing source-facts configuration path.
		} else {
			contents := "[Service]\n# synthetic reviewed " + role + "\n"
			if role == "service_unit" {
				contents += "Environment=XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nEnvironment=XINSIGHT_EXTERNAL_ORIGIN=https://xinsight.example\n"
			}
			if containsString(directFPV9DropInRoles(), role) {
				contents += "IPAddressAllow=8.130.18.85/32\n"
			}
			writeHostFile(t, path, []byte(contents), 0o644)
		}
		proof.Files[role], _, _ = fileSHA256(path, 1<<20)
	}
	checksums, err := db.LoadMigrationChecksums("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	fixture.database.facts.SystemIdentifier = proof.SystemIdentifier
	fixture.database.facts.ReaderRoleExists = true
	fixture.database.facts.WorkspaceRoleExists = true
	fixture.database.facts.ImporterRoleExists = true
	fixture.database.facts.MigrationLedger = nil
	for _, checksum := range checksums[:40] {
		fixture.database.facts.MigrationLedger = append(fixture.database.facts.MigrationLedger, db.MigrationRecord{MigrationChecksum: checksum})
	}
	base := fixture.engine.Runner
	properties := map[string]map[string]string{}
	for i, spec := range []struct {
		unit, binary, fragment string
		dropins                []string
	}{
		{"xinsight.service", proof.ServerBinary, fixture.paths.ServiceUnit, []string{directFPV9Paths(fixture.paths)["service_content_dropin"], directFPV9Paths(fixture.paths)["service_delivery_dropin"]}},
		{"xinsight-feed.service", proof.WorkerBinary, fixture.paths.FeedServiceUnit, []string{directFPV9Paths(fixture.paths)["feed_delivery_dropin"]}},
		{"xinsight-source-facts.service", proof.WorkerBinary, fixture.paths.SourceFactsServiceUnit, []string{directFPV9Paths(fixture.paths)["source_facts_credentials_dropin"], directFPV9Paths(fixture.paths)["source_facts_delivery_dropin"]}},
	} {
		pid := 2001 + i
		proc := filepath.Join(root, "proc", fmt.Sprintf("%d", pid))
		if err := os.MkdirAll(proc, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(spec.binary, filepath.Join(proc, "exe")); err != nil {
			t.Fatal(err)
		}
		properties[spec.unit] = map[string]string{"FragmentPath": spec.fragment, "DropInPaths": strings.Join(spec.dropins, " "),
			"UnitFileState": "enabled", "ActiveState": "active", "MainPID": fmt.Sprintf("%d", pid)}
	}
	fixture.engine.Runner = directFPV9FixtureRunner{base: base, properties: properties}
	target, err := fixture.engine.verifyTarget(artifact)
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: target.Identity, accessProtected: true}}
	proofPath := filepath.Join(root, "source-proof.json")
	writeHostJSON(t, proofPath, proof, 0o600)
	beforeBackupCount := fixture.database.backupCount
	report, err := fixture.engine.CheckDirectFPV9Upgrade(context.Background(), artifact, proofPath)
	if err != nil || !report.Supported || report.Operation != "upgrade_direct_fpv9" {
		t.Fatalf("positive exact-source check failed: %+v %v", report, err)
	}
	if fixture.database.backupCount != beforeBackupCount {
		t.Fatal("check wrote a database backup")
	}
	for _, path := range []string{fixture.paths.ActiveOperation, fixture.paths.InstallState, fixture.paths.CurrentLink, target.Identity.ReleaseDir} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("check created %s: %v", path, err)
		}
	}
	if interruptionPhase == "write_receipt" {
		clock := *fixture.now
		fixture.engine.Now = func() time.Time {
			clock = clock.Add(time.Second)
			return clock
		}
	}
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", interruptionPhase)
	if _, err := fixture.engine.DirectFPV9Upgrade(context.Background(), artifact, proofPath); err == nil ||
		!strings.Contains(err.Error(), map[string]string{
			"migrate_database":      "database_migration_failed",
			"install_runtime_files": "runtime_file_install_failed",
			"write_install_state":   "install_state_write_failed",
			"write_receipt":         "receipt_finalize_failed",
		}[interruptionPhase]) {
		t.Fatalf("expected synthetic %s interruption: %v", interruptionPhase, err)
	}
	interrupted, err := fixture.engine.readActiveJournal()
	if err != nil || !containsString(interrupted.CompletedPhases, "migrate_database") ||
		containsString(interrupted.AllowedActions, "abort") || interrupted.BackupSHA256 == "" {
		t.Fatalf("migration barrier did not retain recovery state: %+v %v", interrupted, err)
	}
	if _, err := fixture.engine.Abort(context.Background()); err == nil {
		t.Fatal("completed schema43 migration could abort to unsigned FPV9")
	}
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	if changedEvidence {
		fixture.engine.Database = directFPV9ChangedEvidenceDatabase{DatabaseManager: fixture.engine.Database}
	}
	receipt, err := fixture.engine.Resume(context.Background())
	if mustReject {
		if err == nil || !strings.Contains(err.Error(), "source_evidence_changed") {
			t.Fatalf("pre-state evidence drift was accepted: %+v %v", receipt, err)
		}
		if _, err := os.Lstat(fixture.paths.InstallState); !os.IsNotExist(err) {
			t.Fatalf("rejected drift wrote managed state: %v", err)
		}
		if fixture.database.backupCount != beforeBackupCount+1 {
			t.Fatal("rejected drift created an additional backup")
		}
		return
	}
	if err != nil {
		t.Fatalf("resume exact-source upgrade: %v", err)
	}
	if receipt.Status != "completed" || receipt.Kind != "upgrade_direct_fpv9" || receipt.BackupSHA256 != interrupted.BackupSHA256 {
		t.Fatalf("resumed receipt lost exact operation identity: %+v", receipt)
	}
	if changedEvidence {
		found := false
		for _, check := range receipt.Checks {
			if check.Name == "evidence_integrity" {
				found = check.Detail == interrupted.BeforeEvidenceFingerprintSHA256
			}
		}
		if !found {
			t.Fatalf("receipt lost pre-migration evidence baseline: %+v", receipt.Checks)
		}
	}
	state, err := fixture.engine.readInstallState()
	if err != nil || state.Previous != nil || state.Current != target.Identity {
		t.Fatalf("resumed state exposed unsigned rollback or wrong target: %+v %v", state, err)
	}
	for _, role := range directFPV9DropInRoles() {
		old := directFPV9Paths(fixture.paths)[role]
		if _, err := os.Lstat(old); !os.IsNotExist(err) {
			t.Fatalf("old direct program override remains active: %s", old)
		}
		if err := validateExactServiceDropIn(managedDirectEgressPath(old), managedDirectEgressContent(proof.EgressAllowCIDR)); err != nil {
			t.Fatalf("managed egress not transferred: %v", err)
		}
	}
}

func TestDirectFPV9UpgradeUsesManagedJournalBackupAndNoUnsignedRollback(t *testing.T) {
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("prepare managed test runtime: %v", err)
	}
	target, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := LoadDatabaseCredentials(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	proof := DirectFPV9SourceProof{EgressAllowCIDR: "8.130.18.85/32", Files: map[string]string{}}
	for role, path := range directFPV9Paths(fixture.paths) {
		content := []byte("[Service]\n# synthetic direct-run " + role + "\n")
		if containsString(directFPV9DropInRoles(), role) {
			content = append(content, []byte("IPAddressAllow=8.130.18.85/32\n")...)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		digest, _, err := fileSHA256(path, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		proof.Files[role] = digest
	}
	before := ReleaseIdentity{SourceKind: directFPV9Kind, ProductVersion: "0.2.0-dev.fpv9", Commit: directFPV9Commit,
		ReleaseDir: filepath.Join(fixture.paths.Root, "synthetic-old-build"), DatabaseSchema: "40"}
	journal, err := fixture.engine.startJournal("upgrade_direct_fpv9", fixture.artifact, "", &before, &target.Identity)
	if err != nil {
		t.Fatal(err)
	}
	journal.DatabaseSetBefore = credentials.SetRef
	journal.SourceProofSHA256 = "sha256:" + strings.Repeat("b", 64)
	if err := fixture.engine.updateJournal(journal); err != nil {
		t.Fatal(err)
	}
	plan := upgradePlan{State: InstallState{Current: before, ManagedAt: fixture.engine.now()}, DirectSource: &proof,
		Target: target, Credentials: credentials, Facts: fixture.database.facts}
	receipt, err := fixture.engine.executeUpgrade(context.Background(), journal, plan)
	if err != nil {
		t.Fatalf("direct upgrade execution failed: %v", err)
	}
	if receipt.Kind != "upgrade_direct_fpv9" || receipt.BackupSHA256 == "" || receipt.Before == nil ||
		receipt.Before.SourceKind != directFPV9Kind {
		t.Fatalf("missing distinct direct-run receipt or backup: %+v", receipt)
	}
	state, err := fixture.engine.readInstallState()
	if err != nil || state.Previous != nil || state.Current != target.Identity {
		t.Fatalf("unsigned predecessor exposed as rollback target: %+v %v", state, err)
	}
	for _, role := range directFPV9DropInRoles() {
		path := directFPV9Paths(fixture.paths)[role]
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("direct program override still active: %s %v", path, err)
		}
		if digest, _, err := fileSHA256(directFPV9RetiredPath(path, journal.OperationID), 1<<20); err != nil || digest != proof.Files[role] {
			t.Fatalf("override recovery copy invalid: %s %v", digest, err)
		}
		if err := validateExactServiceDropIn(managedDirectEgressPath(path), managedDirectEgressContent(proof.EgressAllowCIDR)); err != nil {
			t.Fatalf("reviewed egress was not carried into managed service: %v", err)
		}
	}
	for _, role := range []string{"service_content_dropin", "source_facts_credentials_dropin"} {
		path := directFPV9Paths(fixture.paths)[role]
		if digest, _, err := fileSHA256(path, 1<<20); err != nil || digest != proof.Files[role] {
			t.Fatalf("credential reference changed: %s %v", digest, err)
		}
	}
}

func TestDirectFPV9PartialMigrationFailureCannotAbortToSchema40(t *testing.T) {
	fixture := newInstallFixture(t)
	target, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	before := ReleaseIdentity{SourceKind: directFPV9Kind, ProductVersion: "0.2.0-dev.fpv9", Commit: directFPV9Commit,
		ReleaseDir: filepath.Join(fixture.paths.Root, "old-build"), DatabaseSchema: "40"}
	journal, err := fixture.engine.startJournal("upgrade_direct_fpv9", fixture.artifact, "", &before, &target.Identity)
	if err != nil {
		t.Fatal(err)
	}
	checksums, err := db.LoadMigrationChecksums("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, checksum := range checksums[:41] {
		fixture.database.facts.MigrationLedger = append(fixture.database.facts.MigrationLedger,
			db.MigrationRecord{MigrationChecksum: checksum})
	}
	journal.Phase = "migrate_database"
	if err := fixture.engine.operationFailed(journal, "database_migration_failed", fmt.Errorf("synthetic failure after migration 41")); err == nil {
		t.Fatal("synthetic migration failure was not reported")
	}
	if containsString(journal.AllowedActions, "abort") {
		t.Fatalf("partial schema43 migration exposed abort: %+v", journal.AllowedActions)
	}
	if _, err := fixture.engine.Abort(context.Background()); err == nil || !strings.Contains(err.Error(), "crossed a database or activation barrier") {
		t.Fatalf("partial migration could abort to FPV9: %v", err)
	}
	if len(fixture.database.facts.MigrationLedger) != 41 {
		t.Fatal("abort changed partially migrated database")
	}
}

func TestDirectFPV9LedgerPinsExactFortyMigrationBytes(t *testing.T) {
	checksums, err := db.LoadMigrationChecksums("../../migrations")
	if err != nil || len(checksums) < 43 {
		t.Fatalf("load migrations: count=%d err=%v", len(checksums), err)
	}
	records := make([]db.MigrationRecord, 40)
	for i := range records {
		records[i].MigrationChecksum = checksums[i]
	}
	digest, err := directFPV9LedgerDigest(records)
	if err != nil || digest != directFPV9LedgerSHA256 {
		t.Fatalf("closed FPV9 prefix drift: %s %v", digest, err)
	}
	records[39].Dirty = true
	if _, err := directFPV9LedgerDigest(records); err == nil {
		t.Fatal("dirty migration accepted")
	}
	records[39].Dirty = false
	records[39].SHA256 = records[38].SHA256
	if digest, err := directFPV9LedgerDigest(records); err != nil || digest == directFPV9LedgerSHA256 {
		t.Fatalf("changed migration accepted: %s %v", digest, err)
	}
}

func TestDirectFPV9ProofRejectsUnknownSourceAndIncompleteFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proof.json")
	proof := DirectFPV9SourceProof{
		SchemaVersion: "xinsight.unmanaged-fpv9-source.v1", ProductVersion: "0.2.0-dev.fpv9",
		Commit: directFPV9Commit, DatabaseName: "synthetic_xi", SystemIdentifier: "123456789", EgressAllowCIDR: "8.130.18.85/32",
		ServerBinary: filepath.Join(dir, "build", "xinsight-server"), WorkerBinary: filepath.Join(dir, "build", "xinsight-importer"),
		Files: map[string]string{},
	}
	for _, role := range directFPV9FileRoles {
		proof.Files[role] = "sha256:" + strings.Repeat("a", 64)
	}
	write := func() {
		t.Helper()
		data, err := json.Marshal(proof)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if _, digest, err := readDirectFPV9Proof(path); err != nil || !sha256ReferencePattern.MatchString(digest) {
		t.Fatalf("valid reviewed source proof rejected: %s %v", digest, err)
	}
	proof.Commit = strings.Repeat("b", 40)
	write()
	if _, _, err := readDirectFPV9Proof(path); err == nil {
		t.Fatal("unknown direct-run commit accepted")
	}
	proof.Commit = directFPV9Commit
	delete(proof.Files, "source_facts_credentials_dropin")
	write()
	if _, _, err := readDirectFPV9Proof(path); err == nil {
		t.Fatal("incomplete service override inventory accepted")
	}
}

func TestDirectFPV9OverrideRetirementPreservesCredentialsAndRestoresOnAbort(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		ServiceUnit:            filepath.Join(root, "xinsight.service"),
		ServiceDropInDir:       filepath.Join(root, "xinsight.service.d"),
		FeedServiceUnit:        filepath.Join(root, "xinsight-feed.service"),
		SourceFactsServiceUnit: filepath.Join(root, "xinsight-source-facts.service"),
		SourceFactsConfig:      filepath.Join(root, "source-facts.json"),
	}
	engine := Engine{Paths: paths}
	proof := DirectFPV9SourceProof{EgressAllowCIDR: "8.130.18.85/32", Files: map[string]string{}}
	for role, path := range directFPV9Paths(paths) {
		content := []byte("[Service]\n# synthetic " + role + "\n")
		if containsString(directFPV9DropInRoles(), role) {
			content = append(content, []byte("IPAddressAllow=8.130.18.85/32\n")...)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		digest, _, err := fileSHA256(path, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		proof.Files[role] = digest
	}
	if err := engine.retireDirectFPV9DropIns("synthetic-op", proof); err != nil {
		t.Fatal(err)
	}
	if err := engine.retireDirectFPV9DropIns("synthetic-op", proof); err != nil {
		t.Fatalf("retirement not idempotent: %v", err)
	}
	for _, role := range directFPV9DropInRoles() {
		path := directFPV9Paths(paths)[role]
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("old override %s still active: %v", role, err)
		}
		if _, err := os.Lstat(directFPV9RetiredPath(path, "synthetic-op")); err != nil {
			t.Fatalf("recovery copy missing: %v", err)
		}
		if err := validateExactServiceDropIn(managedDirectEgressPath(path), managedDirectEgressContent(proof.EgressAllowCIDR)); err != nil {
			t.Fatalf("reviewed egress missing: %v", err)
		}
	}
	for _, role := range []string{"service_content_dropin", "source_facts_credentials_dropin"} {
		if _, err := os.Lstat(directFPV9Paths(paths)[role]); err != nil {
			t.Fatalf("credential reference %s was retired: %v", role, err)
		}
	}
	if err := engine.restoreDirectFPV9DropIns("synthetic-op", proof); err != nil {
		t.Fatal(err)
	}
	if err := engine.restoreDirectFPV9DropIns("synthetic-op", proof); err != nil {
		t.Fatalf("restore not idempotent: %v", err)
	}
	for _, role := range directFPV9DropInRoles() {
		path := directFPV9Paths(paths)[role]
		if digest, _, err := fileSHA256(path, 1<<20); err != nil || digest != proof.Files[role] {
			t.Fatalf("override %s not restored: %s %v", role, digest, err)
		}
		if _, err := os.Lstat(managedDirectEgressPath(path)); !os.IsNotExist(err) {
			t.Fatalf("managed egress still active after abort: %v", err)
		}
	}
}

func TestDirectFPV9AbortRestoresRetiredOverridesBeforeMigration(t *testing.T) {
	fixture := newInstallFixture(t)
	paths := directFPV9Paths(fixture.paths)
	proof := DirectFPV9SourceProof{
		SchemaVersion: "xinsight.unmanaged-fpv9-source.v1", ProductVersion: "0.2.0-dev.fpv9", Commit: directFPV9Commit,
		DatabaseName: "synthetic_xi", SystemIdentifier: "123456789", EgressAllowCIDR: "8.130.18.85/32",
		ServerBinary: filepath.Join(fixture.paths.Root, "old", "xinsight-server"),
		WorkerBinary: filepath.Join(fixture.paths.Root, "old", "xinsight-importer"),
		Files:        map[string]string{},
	}
	for role, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		content := []byte("[Service]\n# " + role + "\n")
		if containsString(directFPV9DropInRoles(), role) {
			content = append(content, []byte("IPAddressAllow=8.130.18.85/32\n")...)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		proof.Files[role], _, _ = fileSHA256(path, 1<<20)
	}
	proofPath := filepath.Join(fixture.paths.Root, "source-proof.json")
	raw, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proofPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, digest, err := readDirectFPV9Proof(proofPath)
	if err != nil {
		t.Fatal(err)
	}
	before := ReleaseIdentity{SourceKind: directFPV9Kind, ProductVersion: proof.ProductVersion, Commit: proof.Commit,
		ReleaseDir: filepath.Dir(proof.ServerBinary), DatabaseSchema: "40"}
	target, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := fixture.engine.startJournal("upgrade_direct_fpv9", fixture.artifact, "", &before, &target.Identity)
	if err != nil {
		t.Fatal(err)
	}
	journal.SourceProofPath, journal.SourceProofSHA256 = proofPath, digest
	journal.CompletedPhases = append(journal.CompletedPhases, "stop_service")
	if err := fixture.engine.retireDirectFPV9DropIns(journal.OperationID, proof); err != nil {
		t.Fatal(err)
	}
	journal.CompletedPhases = append(journal.CompletedPhases, "retire_direct_dropins")
	if err := fixture.engine.updateJournal(journal); err != nil {
		t.Fatal(err)
	}
	receipt, err := fixture.engine.Abort(context.Background())
	if err != nil || receipt.Status != "aborted" {
		t.Fatalf("abort failed: %+v %v", receipt, err)
	}
	for _, role := range directFPV9DropInRoles() {
		if actual, _, err := fileSHA256(paths[role], 1<<20); err != nil || actual != proof.Files[role] {
			t.Fatalf("old override %s not restored: %s %v", role, actual, err)
		}
	}
}
