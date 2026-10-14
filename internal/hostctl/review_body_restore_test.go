package hostctl

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreCustodyFailureStopsBeforeCredentialSwitchAndRechecksOnResume(t *testing.T) {
	f := newInstallFixture(t)
	if _, err := f.engine.Install(context.Background(), f.artifact); err != nil {
		t.Fatal(err)
	}
	credentials, err := LoadDatabaseCredentials(f.paths)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := f.database.Backup(context.Background(), credentials, filepath.Join(f.paths.BackupsDir, "custody-fixture.dump"))
	if err != nil {
		t.Fatal(err)
	}
	f.database.reviewBodyRestoreErr = fmt.Errorf("synthetic wrong custody key")
	if _, err := f.engine.Restore(context.Background(), backup.Path, backup.SHA256, "xinsight_restore_custody_fixture", "field_validation"); err == nil || !strings.Contains(err.Error(), "restored_review_body_key_validation_failed") {
		t.Fatal("wrong custody key did not fail", err)
	}
	link, err := readAbsoluteSymlink(f.paths.DatabaseCurrentLink)
	if err != nil || link != credentials.SetRef {
		t.Fatal("failed check switched current database", err)
	}
	j, err := f.engine.readActiveJournal()
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(j.CompletedPhases, "restore_database") || containsString(j.CompletedPhases, "switch_database_credentials") {
		t.Fatal("wrong stop boundary")
	}
	checks := f.database.reviewBodyRestoreChecks
	if _, err := f.engine.Resume(context.Background()); err == nil || !strings.Contains(err.Error(), "restored_review_body_key_validation_failed") {
		t.Fatal("resume skipped custody check", err)
	}
	if f.database.reviewBodyRestoreChecks != checks+1 {
		t.Fatal("restore check was cached")
	}
	f.database.reviewBodyRestoreErr = nil
	if _, err := f.engine.Resume(context.Background()); err != nil {
		t.Fatal("resume with corrected key", err)
	}
}
