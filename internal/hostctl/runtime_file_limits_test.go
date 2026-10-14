package hostctl

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/releasebundle"
)

func packRuntimeFileFixture(t *testing.T, fixture installFixture, path string, contents []byte) (string, error) {
	t.Helper()
	payload := filepath.Join(fixture.paths.Root, "artifacts", "payload")
	mode := os.FileMode(0o644)
	if strings.HasPrefix(path, "bin/") {
		mode = 0o755
	}
	writeHostFile(t, filepath.Join(payload, path), contents, mode)
	artifact := filepath.Join(fixture.paths.Root, "artifacts", "runtime-file-test.tar.gz")
	_, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir: payload, ConfigPath: filepath.Join(fixture.paths.Root, "artifacts", "release-config.json"),
		SigningKeyPath: fixture.privateKey, OutputPath: artifact,
		Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SourceDateEpoch: fixture.now.Add(-time.Hour),
		BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
	})
	return artifact, err
}

func TestInstallLargeLauncherAndResumeAfterMigration(t *testing.T) {
	for _, resume := range []bool{false, true} {
		name := "fresh"
		if resume {
			name = "resume_after_migration"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newInstallFixture(t)
			// Size of the actual Go 1.25.11 F candidate that passed signature
			// verification but failed the old 8 MiB runtime-file copy limit.
			launcher := bytes.Repeat([]byte{0x5a}, 8477714)
			artifact, err := packRuntimeFileFixture(t, fixture, "bin/xinsight-launcher", launcher)
			if err != nil {
				t.Fatal(err)
			}
			target, err := fixture.engine.verifyTarget(artifact)
			if err != nil {
				t.Fatal(err)
			}
			fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{identity: target.Identity}}
			if report, err := fixture.engine.CheckInstall(context.Background(), artifact); err != nil || !report.Supported {
				t.Fatalf("check: report=%+v err=%v", report, err)
			}
			if resume {
				t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "apply_role_grants")
				if _, err := fixture.engine.Install(context.Background(), artifact); err == nil {
					t.Fatal("expected interruption after migration and grants")
				}
				journal, err := fixture.engine.readActiveJournal()
				if err != nil || !containsString(journal.CompletedPhases, "migrate_database") {
					t.Fatalf("migration checkpoint: %+v %v", journal, err)
				}
				t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
				receipt, err := fixture.engine.Resume(context.Background())
				if err != nil || receipt.Status != "completed" || receipt.OperationID != journal.OperationID {
					t.Fatalf("resume: %+v %v", receipt, err)
				}
			} else if receipt, err := fixture.engine.Install(context.Background(), artifact); err != nil || receipt.Status != "completed" {
				t.Fatalf("install: %+v %v", receipt, err)
			}
			assertFileContents(t, fixture.paths.ServiceLauncher, launcher)
			state, err := fixture.engine.readInstallState()
			if err != nil {
				t.Fatal(err)
			}
			bridge := InstallState{Current: ReleaseIdentity{LegacyReleaseID: "legacy-test"}, Previous: &state.Current}
			if err := fixture.engine.validateLegacyUpgradeRuntime(bridge); err != nil {
				t.Fatalf("large launcher legacy bridge: %v", err)
			}
			launcher[len(launcher)-1] ^= 1
			writeHostFile(t, fixture.paths.ServiceLauncher, launcher, 0o755)
			if err := fixture.engine.validateLegacyUpgradeRuntime(bridge); err == nil {
				t.Fatal("legacy bridge accepted launcher content drift")
			}
		})
	}
}

func TestPackRejectsUninstallableRuntimeFiles(t *testing.T) {
	for _, tc := range []struct {
		path string
		size int
	}{
		{"deploy/xinsight.service", (8 << 20) + 1},
		{"deploy/xinsight-feed.service", (1 << 20) + 1},
		{"deploy/xinsight.env.example", (64 << 10) + 1},
		{"bin/xinsight-launcher", 0},
	} {
		t.Run(tc.path, func(t *testing.T) {
			fixture := newInstallFixture(t)
			artifact, err := packRuntimeFileFixture(t, fixture, tc.path, bytes.Repeat([]byte{'x'}, tc.size))
			if err == nil || !strings.Contains(err.Error(), "runtime file "+tc.path) {
				t.Fatalf("expected runtime-size refusal before signing: %v", err)
			}
			if _, err := os.Lstat(artifact); !os.IsNotExist(err) {
				t.Fatalf("invalid immutable artifact was written: %v", err)
			}
		})
	}
}
