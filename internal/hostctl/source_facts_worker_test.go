package hostctl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedSourceWorkerMaintenanceAndRestore(t *testing.T) {
	for _, kind := range []string{"upgrade", "restore", "rollback"} {
		t.Run(kind, func(t *testing.T) {
			f := newInstallFixture(t)
			if _, e := f.engine.Install(context.Background(), f.artifact); e != nil {
				t.Fatal(e)
			}
			r := f.engine.Runner.(*fakeRunner)
			if e := os.WriteFile(f.paths.SourceFactsServiceUnit, []byte("synthetic service"), 0644); e != nil {
				t.Fatal(e)
			}
			r.sourceFactsWorkerLoaded = true
			r.sourceFactsWorkerActive = true
			r.sourceFactsWorkerEnabled = true
			j := &Journal{OperationID: "synthetic-source-maintenance", Kind: kind, CompletedPhases: []string{}}
			if e := f.engine.captureAndStopSourceFactsWorker(t.Context(), j); e != nil {
				t.Fatal(e)
			}
			if !j.SourceFactsWorkerWasActive || !j.SourceFactsWorkerWasEnabled || r.sourceFactsWorkerActive {
				t.Fatal("source worker not drained")
			}
			release := t.TempDir()
			if kind != "rollback" {
				for _, name := range []string{"bin/xinsight-importer", "deploy/xinsight-source-facts.service"} {
					p := filepath.Join(release, name)
					if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
						t.Fatal(e)
					}
					if e := os.WriteFile(p, []byte("synthetic"), 0644); e != nil {
						t.Fatal(e)
					}
				}
			}
			if e := f.engine.restartSourceFactsWorkerAfterMaintenance(t.Context(), j, release); e != nil {
				t.Fatal(e)
			}
			if kind == "upgrade" {
				if !r.sourceFactsWorkerActive || !r.sourceFactsWorkerEnabled {
					t.Fatal("active managed worker not resumed")
				}
			} else if r.sourceFactsWorkerActive || r.sourceFactsWorkerEnabled {
				t.Fatal("restored or unsupported runtime auto-started source collection")
			}
		})
	}
}

func TestManagedSourceUnitAndConfigurationBoundaries(t *testing.T) {
	raw, e := os.ReadFile("../../deploy/xinsight-source-facts.service")
	if e != nil {
		t.Fatal(e)
	}
	unit := string(raw)
	for _, needed := range []string{"User=xinsight", "unirouter run-source-facts", "LoadCredential=postgres-importer.dsn:", "ConditionPathExists=|/etc/x-insight/source-facts.json", "ConditionPathExistsGlob=|/etc/x-insight/source-facts.d/xci_*-xise_*-project-*.json", "RuntimeDirectoryMode=0750", "ProtectSystem=strict", "NoNewPrivileges=true", "KillSignal=SIGTERM"} {
		if !strings.Contains(unit, needed) {
			t.Fatal("missing worker boundary", needed)
		}
	}
	for _, forbidden := range []string{"postgres-admin.dsn", "postgres-workspace.dsn", "unirouter-feed.key", "XINSIGHT_UNIROUTER_FEED"} {
		if strings.Contains(unit, forbidden) {
			t.Fatal("worker credential crossover", forbidden)
		}
	}
	dir := t.TempDir()
	if e = os.Chmod(dir, 0755); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "source-facts.json")
	if e = writeSourceFactsConfiguration(path, []byte("{}\n")); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(dir)
	if e != nil || info.Mode().Perm() != 0755 {
		t.Fatal("nonsecret configuration write changed parent access", e)
	}
	info, e = os.Stat(path)
	if e != nil || info.Mode().Perm() != 0644 {
		t.Fatal("configuration mode", e)
	}
}
