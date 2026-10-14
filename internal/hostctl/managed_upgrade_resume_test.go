package hostctl

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/releasebundle"
	"github.com/foigent/x-insight/internal/reviewbody"
)

// Only the original command boundary is interrupted; no journal phase or
// installed identity is rewritten to manufacture an accepted recovery state.
type managedRestartInterruption struct{}
type managedRestartBarrierRunner struct {
	base   CommandRunner
	engine *Engine
}

func (r managedRestartBarrierRunner) Run(ctx context.Context, c Command) (CommandResult, error) {
	if c.Name == "systemctl" && reflect.DeepEqual(c.Args, []string{"daemon-reload"}) {
		j, err := r.engine.readActiveJournal()
		if err == nil && j.Kind == "upgrade" && j.Phase == "restart_service" {
			panic(managedRestartInterruption{})
		}
	}
	return r.base.Run(ctx, c)
}

func publishedFPV11ResumeSource(t *testing.T) string {
	t.Helper()
	return publishedManagedResumeSource(t, "FPV11", "9c58e07f011918c4a0aed2f876ce4baa9fb12b5e")
}

const publishedFPV12ResumeCommit = "b1b6067dbd5bce1c9e9988fc12c1e1f28d979952"

func publishedManagedResumeSource(t *testing.T, profile, commit string) string {
	t.Helper()
	source, archive := t.TempDir(), filepath.Join(t.TempDir(), "source.tar")
	c := exec.Command("git", "archive", "--format=tar", "--output="+archive, commit,
		"migrations", "deploy", "schemas", "fixtures", "internal/webui/manual/zh-CN", "release/d-validation-config.json",
		"release/d-validation-compatibility-matrix.json", "release/d-validation-notes.md", "internal/hostctl/development_schema_contract.json")
	c.Dir = "../.."
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("read exact published %s: %v %s", profile, err, out)
	}
	if out, err := exec.Command("tar", "-xf", archive, "-C", source).CombinedOutput(); err != nil {
		t.Fatalf("extract exact %s: %v %s", profile, err, out)
	}
	return source
}

// Use exact historical metadata/migrations/units for both FPV11 and FPV12,
// with the existing ephemeral test trust only. Current release metadata may advance.
func packManagedResumeProfile(t *testing.T, f installFixture, source, label, commit string, rollbackProof bool) string {
	t.Helper()
	payload := filepath.Join(f.paths.Root, "artifacts", label+"-payload")
	for _, name := range []string{"migrations", "deploy", "schemas", "fixtures"} {
		if err := os.CopyFS(filepath.Join(payload, name), os.DirFS(filepath.Join(source, name))); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.CopyFS(filepath.Join(payload, "manual/zh-CN"), os.DirFS(filepath.Join(source, "internal/webui/manual/zh-CN"))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(source, "licenses/manifest.json")); err == nil {
		for dest, src := range map[string]string{"manual/en-US": "internal/webui/manual/en-US", "licenses": "licenses"} {
			if err := os.CopyFS(filepath.Join(payload, dest), os.DirFS(filepath.Join(source, src))); err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range []string{"LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"} {
			raw, err := os.ReadFile(filepath.Join(source, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(payload, name), raw, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for dest, src := range map[string]string{"release/compatibility-matrix.json": "release/d-validation-compatibility-matrix.json", "release/release-notes.md": "release/d-validation-notes.md", "release/schema-contract.json": "internal/hostctl/development_schema_contract.json"} {
		raw, err := os.ReadFile(filepath.Join(source, src))
		if err != nil {
			t.Fatal(err)
		}
		writeHostFile(t, filepath.Join(payload, dest), raw, 0644)
	}
	for _, name := range []string{"xinsight-server", "xinsight-importer", "xinsightctl", "xinsight-launcher"} {
		writeHostFile(t, filepath.Join(payload, "bin", name), []byte("non-publishable managed resume fixture "+label+" "+name+"\n"), 0755)
	}
	configPath := filepath.Join(source, "release/d-validation-config.json")
	if rollbackProof { // Explicit synthetic policy to reach downstream guards only.
		raw, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		var cfg releasebundle.ReleaseConfig
		if err = json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		cfg.Rollback.ApplicationRollbackCompatible = true
		configPath = filepath.Join(f.paths.Root, "artifacts", label+"-synthetic-rollback-policy.json")
		writeHostJSON(t, configPath, cfg, 0644)
	}
	artifact := filepath.Join(f.paths.Root, "artifacts", label+".tar.gz")
	if _, err := releasebundle.Pack(releasebundle.PackOptions{PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: f.privateKey, OutputPath: artifact, Commit: commit, SourceDateEpoch: f.now.Add(-30 * 60 * 1e9), BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.12", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"}}); err != nil {
		t.Fatal(err)
	}
	return artifact
}

const managedResumeInvocation = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type managedResumeRuntimeRunner struct{ base *fakeRunner }

func (r managedResumeRuntimeRunner) Run(ctx context.Context, c Command) (CommandResult, error) {
	if c.Name == "systemctl" && reflect.DeepEqual(c.Args, []string{"show", "xinsight.service", "--property", "InvocationID", "--value"}) {
		return CommandResult{Output: managedResumeInvocation + "\n"}, nil
	}
	return r.base.Run(ctx, c)
}

type managedResumeProductTransport struct {
	base   fixedProductTransport
	engine *Engine
	runner *fakeRunner
}

func (r managedResumeProductTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := r.base.RoundTrip(req)
	if err != nil || req.URL.Path != "/readyz" {
		return response, err
	}
	c, active, present, err := r.engine.custodyBinding()
	if err != nil {
		return nil, err
	}
	if !active || !present {
		return response, nil
	}
	ring, err := reviewbody.LoadKeyRingFromFile(c.KeyID, c.KeyFile)
	if err != nil {
		return nil, err
	}
	proof, err := ring.LoadedProof("xinsight.service", managedResumeInvocation, int(r.runner.mainServicePID))
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err = json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, err
	}
	response.Body.Close()
	body["review_body_loaded"] = proof
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return response, nil
}

// The old fake accepted any SHA-matching file. This wrapper enforces the
// native-versus-private boundary; the PG16 regression also proves real formats.
type strictManagedResumeDatabase struct {
	*fakeDatabase
	outerNativeCalls int
}

func (d *strictManagedResumeDatabase) VerifyBackup(ctx context.Context, path, sha string) (BackupResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return BackupResult{}, err
	}
	defer f.Close()
	if h, err := tar.NewReader(f).Next(); err == nil && h.Name == "manifest.json" {
		d.outerNativeCalls++
		return BackupResult{}, fmt.Errorf("private outer tar sent to native PG verifier")
	}
	return d.fakeDatabase.VerifyBackup(ctx, path, sha)
}

func managedFPV11ResumeFixture(t *testing.T) (installFixture, verifiedTarget, verifiedTarget) {
	return managedFPV11ResumeFixturePolicy(t, false)
}
func managedFPV11ResumeFixturePolicy(t *testing.T, rollbackProof bool) (installFixture, verifiedTarget, verifiedTarget) {
	t.Helper()
	f := newInstallFixture(t)
	writeSyntheticAccessCredential(t, f.paths.AccessAccountsFile)
	oldArtifact := packManagedResumeProfile(t, f, publishedFPV11ResumeSource(t), "original-fpv11", "9c58e07f011918c4a0aed2f876ce4baa9fb12b5e", false)
	before, err := f.engine.verifyTarget(oldArtifact)
	if err != nil {
		t.Fatal(err)
	}
	if before.Identity.DatabaseSchema != "43" || before.Identity.ProductVersion != "0.2.0-dev.fpv11" {
		t.Fatal("predecessor profile changed")
	}
	f.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: before.Identity, accessProtected: true}}
	if _, err = f.engine.Install(t.Context(), oldArtifact); err != nil {
		t.Fatal("install original FPV11", err)
	}
	if _, err := os.Lstat(filepath.Join(f.paths.EtcDir, "review-body.key")); !os.IsNotExist(err) {
		t.Fatal("old install unexpectedly created custody", err)
	}
	targetSource := publishedManagedResumeSource(t, "FPV12", publishedFPV12ResumeCommit)
	artifact := packManagedResumeProfile(t, f, targetSource, "target-fpv12", publishedFPV12ResumeCommit, rollbackProof)
	target, err := f.engine.verifyTarget(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if target.Identity.DatabaseSchema != "44" || target.Identity.ProductVersion != "0.2.0-dev.fpv12" {
		t.Fatal("historical FPV12 target profile changed")
	}
	runner := f.engine.Runner.(*fakeRunner)
	f.engine.Runner = managedResumeRuntimeRunner{base: runner}
	f.engine.HTTPClient = &http.Client{Transport: managedResumeProductTransport{base: fixedProductTransport{identity: target.Identity, accessProtected: true}, engine: f.engine, runner: runner}}
	f.engine.Database = &strictManagedResumeDatabase{fakeDatabase: f.database}
	return f, before, target
}

func interruptManagedUpgrade(t *testing.T, f installFixture, target verifiedTarget, phase string) Journal {
	t.Helper()
	if phase == "restart_service_reload" {
		base := f.engine.Runner
		f.engine.Runner = managedRestartBarrierRunner{base: base, engine: f.engine}
		interrupted := false
		func() {
			defer func() {
				if p := recover(); p != nil {
					if _, ok := p.(managedRestartInterruption); !ok {
						panic(p)
					}
					interrupted = true
				}
			}()
			_, _ = f.engine.Upgrade(t.Context(), target.Identity.ArtifactPath)
		}()
		f.engine.Runner = base
		if !interrupted {
			t.Fatal("original restart-service boundary was not reached")
		}
	} else {
		t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", phase)
		if _, err := f.engine.Upgrade(t.Context(), target.Identity.ArtifactPath); err == nil {
			t.Fatal("phase did not interrupt", phase)
		}
		t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	}
	j, err := f.engine.readActiveJournal()
	if err != nil {
		t.Fatal(err)
	}
	if phase == "restart_service_reload" && (j.Status != "active" || j.Phase != "restart_service" || containsString(j.CompletedPhases, "restart_service") || !containsString(j.CompletedPhases, "activate_release")) {
		t.Fatal("wrong unfinished crash boundary", j.Status, j.Phase, j.CompletedPhases)
	}
	return j
}

type custodyResumeSnapshot struct {
	config ReviewBodyConfig
	body   []byte
	info   os.FileInfo
}

func snapshotResumeCustody(t *testing.T, e *Engine) custodyResumeSnapshot {
	t.Helper()
	c, active, present, err := e.custodyBinding()
	if err != nil || !active || !present {
		t.Fatal("custody unavailable", err)
	}
	body, err := os.ReadFile(c.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(c.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	return custodyResumeSnapshot{c, body, info}
}
func assertResumeCustodyUnchanged(t *testing.T, e *Engine, want custodyResumeSnapshot) {
	t.Helper()
	got := snapshotResumeCustody(t, e)
	if got.config != want.config || !bytes.Equal(got.body, want.body) || !os.SameFile(got.info, want.info) {
		t.Fatal("resume replaced original custody identity/key/inode")
	}
}

func TestManagedFPV11UpgradeResumePreservesOriginalCustodyAndIdentity(t *testing.T) {
	for _, phase := range []string{"install_runtime_files", "activate_release", "restart_service_reload", "write_install_state"} {
		t.Run(phase, func(t *testing.T) {
			f, before, target := managedFPV11ResumeFixture(t)
			backups := f.database.backupCount
			j := interruptManagedUpgrade(t, f, target, phase)
			if j.Before == nil || *j.Before != before.Identity || j.Target == nil || *j.Target != target.Identity || j.BackupPath == "" || j.BackupSHA256 == "" {
				t.Fatal("interruption lost original journal identities")
			}
			state, err := f.engine.readInstallState()
			if err != nil {
				t.Fatal(err)
			}
			link, err := readAbsoluteSymlink(f.paths.CurrentLink)
			if err != nil {
				t.Fatal(err)
			}
			wantState, wantLink := before.Identity, before.Identity.ReleaseDir
			if phase != "install_runtime_files" {
				wantLink = target.Identity.ReleaseDir
			}
			if phase == "write_install_state" {
				wantState = target.Identity
			}
			if state.Current != wantState || link != wantLink {
				t.Fatalf("unsupported phase/state/link combination: state=%+v want=%+v link=%s want_link=%s journal_phase=%s completed=%v", state.Current, wantState, link, wantLink, j.Phase, j.CompletedPhases)
			}
			var custody *custodyResumeSnapshot
			if phase != "install_runtime_files" {
				c := snapshotResumeCustody(t, f.engine)
				custody = &c
				f.engine.Random = bytes.NewReader(nil)
			}
			recovered, err := f.engine.Recover(t.Context())
			if err != nil || recovered.ActiveJournal == nil || recovered.ActiveJournal.OperationID != j.OperationID {
				t.Fatal("recover changed active operation", err)
			}
			receipt, err := f.engine.Resume(t.Context())
			if err != nil {
				t.Fatal("resume original signed upgrade", err)
			}
			if receipt.Status != "completed" || receipt.Before == nil || *receipt.Before != before.Identity || receipt.After == nil || *receipt.After != target.Identity || receipt.BackupPath != j.BackupPath || receipt.BackupSHA256 != j.BackupSHA256 || f.database.backupCount != backups+1 {
				t.Fatal("resume lost exact identities or repeated backup")
			}
			state, err = f.engine.readInstallState()
			if err != nil || state.Current != target.Identity || state.Previous == nil || *state.Previous != before.Identity || state.DatabaseSchemaCurrent != "44" {
				t.Fatal("wrong resumed managed state", err)
			}
			if custody != nil {
				assertResumeCustodyUnchanged(t, f.engine, *custody)
			}
			if _, err = f.engine.verifyTarget(before.Identity.ArtifactPath); err == nil || !strings.Contains(err.Error(), "enabled review custody is unsupported") {
				t.Fatal("old profile accepted as actual target with enabled custody", err)
			}
			checkBefore := snapshotManagedFiles(t, f.paths.Root)
			if _, err = f.engine.CheckRollback(t.Context()); err == nil || !strings.Contains(err.Error(), "current release does not declare application rollback compatibility") {
				t.Fatal("original signed rollback policy not enforced", err)
			}
			if !bytes.Equal(checkBefore, snapshotManagedFiles(t, f.paths.Root)) {
				t.Fatal("rollback check mutated files")
			}
			if before.Bundle.Manifest.ApplicationCompatibility.DatabaseSchemaMax != "43" || target.Identity.DatabaseSchema != "44" {
				t.Fatal("original predecessor schema compatibility changed")
			}
			if d := f.engine.Database.(*strictManagedResumeDatabase); d.outerNativeCalls != 0 {
				t.Fatal("native verifier received outer private tar")
			}
		})
	}
}

func TestManagedFPV11UpgradeResumeRejectsArtifactPayloadAndConfigurationDrift(t *testing.T) {
	f, before, target := managedFPV11ResumeFixture(t)
	j := interruptManagedUpgrade(t, f, target, "restart_service_reload")
	custody := snapshotResumeCustody(t, f.engine)
	backups := f.database.backupCount
	for _, scenario := range []struct {
		name, path string
	}{
		{"old_artifact", before.Identity.ArtifactPath}, {"old_payload", filepath.Join(before.Identity.ReleaseDir, "bin/xinsight-server")},
		{"target_artifact", target.Identity.ArtifactPath}, {"target_payload", filepath.Join(target.Identity.ReleaseDir, "bin/xinsight-server")},
		{"invalid_host_config", f.engine.reviewBodyEnvironment()},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			raw, err := os.ReadFile(scenario.path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(scenario.path)
			if err != nil {
				t.Fatal(err)
			}
			defer writeHostFile(t, scenario.path, raw, info.Mode().Perm())
			changed := []byte("invalid current host configuration\n")
			if scenario.name != "invalid_host_config" {
				changed = append(append([]byte{}, raw...), byte('x'))
			}
			writeHostFile(t, scenario.path, changed, info.Mode().Perm())
			if _, err := f.engine.resumeUpgradePlan(t.Context(), j); err == nil {
				t.Fatal("changed original/target/config admitted")
			}
			if f.database.backupCount != backups {
				t.Fatal("failed preflight repeated backup")
			}
			if scenario.name != "invalid_host_config" {
				assertResumeCustodyUnchanged(t, f.engine, custody)
			}
		})
	}
	// Every recorded identity field remains significant, including boolean policy.
	v := reflect.ValueOf(before.Identity)
	for i := 0; i < v.NumField(); i++ {
		t.Run("before_"+v.Type().Field(i).Name, func(t *testing.T) {
			changed := before.Identity
			field := reflect.ValueOf(&changed).Elem().Field(i)
			switch field.Kind() {
			case reflect.String:
				field.SetString(field.String() + "-drift")
			case reflect.Bool:
				field.SetBool(!field.Bool())
			default:
				t.Fatal("unhandled identity field")
			}
			bad := j
			bad.Before = &changed
			if _, err := f.engine.resumeUpgradePlan(t.Context(), bad); err == nil {
				t.Fatal("Before identity drift admitted")
			}
		})
	}
	assertResumeCustodyUnchanged(t, f.engine, custody)
	if f.database.backupCount != backups {
		t.Fatal("negative preflight created backup")
	}
	if _, err := f.engine.resumeUpgradePlan(t.Context(), j); err != nil {
		t.Fatal(fmt.Errorf("valid unchanged original plan rejected after negative tests: %w", err))
	}
}

func resealResumeBackupManifest(t *testing.T, path string, change func(*PrivateBackupManifest)) (string, string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	reader := tar.NewReader(f)
	for {
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == "manifest.json" {
			var m PrivateBackupManifest
			if err = json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			change(&m)
			raw, err = json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			h.Size = int64(len(raw))
		}
		if err = writer.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err = writer.Write(raw); err != nil {
			t.Fatal(err)
		}
		clear(raw)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "resealed-private.tar")
	writeHostFile(t, out, buf.Bytes(), 0600)
	sha, _, err := fileSHA256(out, privateBackupMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	return out, sha
}
func TestManagedUpgradeResumeBindsPrivateBackupToOriginalJournal(t *testing.T) {
	f, _, target := managedFPV11ResumeFixture(t)
	j := interruptManagedUpgrade(t, f, target, "restart_service_reload")
	custody := snapshotResumeCustody(t, f.engine)
	for name, change := range map[string]func(*PrivateBackupManifest){
		"operation":   func(m *PrivateBackupManifest) { m.OperationID = "20260713T130000Z-ffffffffffff" },
		"release":     func(m *PrivateBackupManifest) { m.Release.Commit = strings.Repeat("f", 40) },
		"migration":   func(m *PrivateBackupManifest) { m.Migrations[0].SHA256 = "sha256:" + strings.Repeat("f", 64) },
		"fingerprint": func(m *PrivateBackupManifest) { m.Fingerprint.SHA256 = "sha256:" + strings.Repeat("f", 64) },
		"dump":        func(m *PrivateBackupManifest) { m.Dump.SHA256 = "sha256:" + strings.Repeat("f", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := j
			bad.BackupPath, bad.BackupSHA256 = resealResumeBackupManifest(t, j.BackupPath, change)
			if _, err := f.engine.resumeUpgradePlan(t.Context(), bad); err == nil {
				t.Fatal("resealed foreign snapshot admitted")
			}
		})
	}
	for name, change := range map[string]func(*Journal){
		"missing_dump": func(j *Journal) { j.PrivateDumpSHA256 = "" }, "wrong_dump": func(j *Journal) { j.PrivateDumpSHA256 = "sha256:" + strings.Repeat("f", 64) },
		"missing_fingerprint": func(j *Journal) { j.PrivateFingerprint = nil }, "wrong_fingerprint": func(j *Journal) { v := *j.PrivateFingerprint; v.SampleCount++; j.PrivateFingerprint = &v },
		"native_format_flag": func(j *Journal) { j.BackupIsPrivate = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := j
			change(&bad)
			if _, err := f.engine.resumeUpgradePlan(t.Context(), bad); err == nil {
				t.Fatal("inconsistent journal backup binding admitted")
			}
		})
	}
	assertResumeCustodyUnchanged(t, f.engine, custody)
	if _, err := f.engine.resumeUpgradePlan(t.Context(), j); err != nil {
		t.Fatal("original package became unusable", err)
	}
}

// A real native verifier validates only inner custom dumps. The outer private
// tar is deliberately passed once to prove that PostgreSQL rejects its format;
// neither the production resume nor this wrapper may then send it natively.
type nativeManagedResumeDatabase struct {
	*strictManagedResumeDatabase
	manager     PostgresManager
	credentials DatabaseCredentials
}

func (d *nativeManagedResumeDatabase) Backup(ctx context.Context, _ DatabaseCredentials, path string) (BackupResult, error) {
	d.fakeDatabase.backupCount++
	return d.manager.Backup(ctx, d.credentials, path)
}
func (d *nativeManagedResumeDatabase) VerifyBackup(ctx context.Context, path, sha string) (BackupResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return BackupResult{}, err
	}
	h, tarErr := tar.NewReader(f).Next()
	f.Close()
	if tarErr == nil && h.Name == "manifest.json" {
		d.outerNativeCalls++
		return BackupResult{}, fmt.Errorf("outer private tar must not reach native verifier")
	}
	return d.manager.VerifyBackup(ctx, path, sha)
}
func TestManagedUpgradePrivateBackupUsesRealPG16DumpFormat(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	f, _, target := managedFPV11ResumeFixture(t)
	// The task-only real database contains the exact historical migration set.
	source := publishedFPV11ResumeSource(t)
	if count, err := db.RunMigrations(ctx, c.AdminDSN, filepath.Join(source, "migrations")); err != nil || count != 43 {
		t.Fatal("real PG16 schema43 predecessor", count, err)
	}
	d := &nativeManagedResumeDatabase{strictManagedResumeDatabase: f.engine.Database.(*strictManagedResumeDatabase), manager: PostgresManager{Runner: ExecRunner{}}, credentials: c}
	f.engine.Database = d
	j := interruptManagedUpgrade(t, f, target, "restart_service_reload")
	if !j.BackupIsPrivate || j.PrivateDumpSHA256 == "" {
		t.Fatal("original private backup was not produced")
	}
	if _, err := d.manager.VerifyBackup(ctx, j.BackupPath, j.BackupSHA256); err == nil {
		t.Fatal("real pg_restore accepted outer private tar")
	}
	inner := filepath.Join(f.paths.OperationsDir, j.OperationID+".dump")
	if _, err := d.manager.VerifyBackup(ctx, inner, j.PrivateDumpSHA256); err != nil {
		t.Fatal("real PG16 rejected inner custom dump", err)
	}
	bundle, err := f.engine.inspectPrivateBackup(ctx, j.BackupPath, j.BackupSHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Clear()
	if bundle.Manifest.Dump.SHA256 != j.PrivateDumpSHA256 || bundle.Manifest.OperationID != j.OperationID {
		t.Fatal("original inner dump/operation lost")
	}
	key := snapshotResumeCustody(t, f.engine)
	backups := d.fakeDatabase.backupCount
	if _, err := f.engine.Resume(ctx); err != nil {
		t.Fatal("resume with strict real native verifier", err)
	}
	if d.outerNativeCalls != 0 || d.fakeDatabase.backupCount != backups {
		t.Fatal("resume sent outer tar to native verifier or repeated real dump")
	}
	assertResumeCustodyUnchanged(t, f.engine, key)
	t.Log("Actual PG16 exact schema43 inner custom dump accepted; original private outer tar rejected by formal pg_restore; strict resume never sends outer tar natively, no repeated dump or original key replacement. Lifecycle database remains simulated; no Linux/Target qualification claim.")
}

func TestManagedFPV11RollbackCapabilityAndSchemaGuardsRemainIndependent(t *testing.T) {
	// The historical FPV12 profile forbids rollback. This explicit synthetic policy
	// enables it solely to exercise the subsequent unchanged guards independently.
	f, _, target := managedFPV11ResumeFixturePolicy(t, true)
	interruptManagedUpgrade(t, f, target, "restart_service_reload")
	if _, err := f.engine.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := snapshotManagedFiles(t, f.paths.Root)
	if _, err := f.engine.CheckRollback(t.Context()); err == nil || !strings.Contains(err.Error(), "enabled review custody is unsupported") {
		t.Fatal("original target capability guard bypassed", err)
	}
	if !bytes.Equal(before, snapshotManagedFiles(t, f.paths.Root)) {
		t.Fatal("capability check wrote files")
	}
	if err := f.engine.DisableReviewBodyConfig(false); err != nil {
		t.Fatal(err)
	}
	before = snapshotManagedFiles(t, f.paths.Root)
	if _, err := f.engine.CheckRollback(t.Context()); err == nil || !strings.Contains(err.Error(), "database schema") {
		t.Fatal("original schema43 application admitted44", err)
	}
	if !bytes.Equal(before, snapshotManagedFiles(t, f.paths.Root)) {
		t.Fatal("schema check wrote files")
	}
	t.Log("Synthetic rollback-eligible target policy only: original capability guard and original FPV11 max-schema43 guard each reject read-only; real release policy is unchanged and rejects earlier.")
}
