package hostctl

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBackupCheckAndResumePreserveOriginalRuntimeAndLogin(t *testing.T) {
	for _, active := range []bool{false, true} {
		for _, recovery := range []string{"complete", "resume", "abort"} {
			t.Run(fmt.Sprintf("active_%v_%s", active, recovery), func(t *testing.T) {
				f := newInstallFixture(t)
				ctx := context.Background()
				if _, err := f.engine.Install(ctx, f.artifact); err != nil {
					t.Fatal(err)
				}
				runner := f.engine.Runner.(*fakeRunner)
				runner.mainServiceActive = active
				runner.controllerActive = active
				runner.controllerLoaded = true
				// An active but disabled controller must not be enabled by backup.
				runner.controllerEnabled = false
				f.database.importerLogin = active
				path := filepath.Join(f.paths.BackupsDir, "explicit-private.tar")
				commands := len(runner.commands)
				transitions := len(f.database.loginTransitions)
				report, err := f.engine.CheckBackup(ctx, path)
				if err != nil || !report.Supported {
					t.Fatal("check", err)
				}
				if f.database.backupCount != 0 || len(f.database.loginTransitions) != transitions {
					t.Fatal("check mutated database")
				}
				for _, command := range runner.commands[commands:] {
					if command.Name == "systemctl" && len(command.Args) > 0 && command.Args[0] != "show" && command.Args[0] != "--version" {
						t.Fatalf("check mutated service: %v", command.Args)
					}
				}
				if recovery != "complete" {
					f.engine.PrivateBackupPublishHook = func() error { return fmt.Errorf("injected publication failure") }
				}
				receipt, err := f.engine.Backup(ctx, path)
				if recovery != "complete" {
					if err == nil {
						t.Fatal("failure not reached")
					}
					j, jerr := f.engine.readActiveJournal()
					if jerr != nil || !j.BackupRuntimeStateCaptured || j.BackupImporterWasLogin == nil || *j.BackupImporterWasLogin != active {
						t.Fatal("original state not durable", jerr)
					}
					if runner.mainServiceActive || runner.controllerActive || f.database.importerLogin {
						t.Fatal("failed backup unexpectedly resumed writers")
					}
					f.engine.PrivateBackupPublishHook = nil
					if recovery == "abort" {
						receipt, err = f.engine.Abort(ctx)
					} else {
						receipt, err = f.engine.Resume(ctx)
					}
				}
				expectedStatus := "completed"
				if recovery == "abort" {
					expectedStatus = "aborted"
				}
				if err != nil || receipt.Status != expectedStatus || receipt.Kind != "backup" {
					t.Fatal("backup/resume", err)
				}
				if runner.mainServiceActive != active || runner.controllerActive != active || runner.controllerEnabled || f.database.importerLogin != active {
					t.Fatal("backup changed original runtime/login")
				}
				if f.database.backupCount != 1 {
					t.Fatal("resume duplicated original dump")
				}
				for _, command := range runner.commands[commands:] {
					if reflect.DeepEqual(command.Args, []string{"enable", "--now", connectorControllerUnitName}) {
						t.Fatal("backup enabled controller")
					}
					if !active && (reflect.DeepEqual(command.Args, []string{"start", "xinsight.service"}) || reflect.DeepEqual(command.Args, []string{"start", connectorControllerUnitName})) {
						t.Fatal("backup started originally stopped service")
					}
				}
			})
		}
	}
}
