package hostctl

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStageAdminCredentialSetUsesCompleteAtomicSuccessor(t *testing.T) {
	points := []string{"after_staging_directory_create", "after_directory_sync", "before_rename", "after_rename", "after_parent_sync"}
	for _, spec := range adminCredentialMemberSpecs {
		points = append(points,
			"after_"+spec.role+"_create",
			"after_"+spec.role+"_write",
			"after_"+spec.role+"_sync",
		)
	}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			paths := adminCredentialStageTestPaths(t)
			predecessor, err := LoadDatabaseCredentials(paths)
			if err != nil {
				t.Fatal(err)
			}
			finalRef := filepath.Join(paths.DatabaseSetsDir, "admin-rotation-test")
			t.Setenv("XINSIGHTCTL_FAIL_ADMIN_CREDENTIAL_STAGE_AT", point)
			if _, _, err := StageAdminCredentialSet(paths, predecessor, finalRef, adminCredentialTestEntropy()); err == nil {
				t.Fatalf("fault point %s did not interrupt staging", point)
			}
			if _, statErr := os.Lstat(finalRef); statErr == nil {
				if point != "after_rename" && point != "after_parent_sync" {
					t.Fatalf("partial final successor exists after %s", point)
				}
				if _, err := LoadDatabaseCredentialSet(paths, finalRef); err != nil {
					t.Fatalf("final successor after atomic rename is incomplete: %v", err)
				}
			} else if !os.IsNotExist(statErr) {
				t.Fatal(statErr)
			}

			t.Setenv("XINSIGHTCTL_FAIL_ADMIN_CREDENTIAL_STAGE_AT", "")
			successor, summary, err := StageAdminCredentialSet(paths, predecessor, finalRef, adminCredentialTestEntropy())
			if err != nil {
				t.Fatalf("resume staging after %s: %v", point, err)
			}
			if successor.SetRef != finalRef || summary.SetRef != finalRef || summary.CanonicalSHA256 == "" {
				t.Fatalf("successor=%+v summary=%+v", successor, summary)
			}
			beforeRaw, err := readAdminCredentialSetBytes(paths, predecessor.SetRef)
			if err != nil {
				t.Fatal(err)
			}
			afterRaw, err := readAdminCredentialSetBytes(paths, finalRef)
			if err != nil {
				t.Fatal(err)
			}
			for _, inherited := range adminCredentialMemberSpecs[:3] {
				if !bytes.Equal(beforeRaw[inherited.filename], afterRaw[inherited.filename]) {
					t.Fatalf("inherited member changed: %s", inherited.role)
				}
			}
			if bytes.Equal(beforeRaw["postgres-admin.dsn"], afterRaw["postgres-admin.dsn"]) {
				t.Fatal("admin member was reused")
			}
		})
	}
}

func TestStageAdminCredentialSetNeverRepairsAnExistingFinalDirectory(t *testing.T) {
	paths := adminCredentialStageTestPaths(t)
	predecessor, err := LoadDatabaseCredentials(paths)
	if err != nil {
		t.Fatal(err)
	}
	finalRef := filepath.Join(paths.DatabaseSetsDir, "admin-rotation-test")
	if err := os.Mkdir(finalRef, 0o700); err != nil {
		t.Fatal(err)
	}
	partial := []byte(predecessor.ReaderDSN + "\n")
	if err := os.WriteFile(filepath.Join(finalRef, "postgres-reader.dsn"), partial, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := StageAdminCredentialSet(paths, predecessor, finalRef, adminCredentialTestEntropy()); err == nil {
		t.Fatal("partial final credential set was repaired in place")
	}
	entries, err := os.ReadDir(finalRef)
	if err != nil || len(entries) != 1 || entries[0].Name() != "postgres-reader.dsn" {
		t.Fatalf("existing final set was mutated: entries=%v err=%v", entries, err)
	}
	contents, err := os.ReadFile(filepath.Join(finalRef, "postgres-reader.dsn"))
	if err != nil || !bytes.Equal(contents, partial) {
		t.Fatalf("existing member was mutated: %v", err)
	}
}

func TestAdminCredentialSetSummaryContainsMetadataOnly(t *testing.T) {
	paths := adminCredentialStageTestPaths(t)
	predecessor, err := LoadDatabaseCredentials(paths)
	if err != nil {
		t.Fatal(err)
	}
	finalRef := filepath.Join(paths.DatabaseSetsDir, "admin-rotation-test")
	successor, summary, err := StageAdminCredentialSet(paths, predecessor, finalRef, adminCredentialTestEntropy())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := marshalJSON(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		predecessor.ReaderDSN, predecessor.WorkspaceDSN, predecessor.ImporterDSN,
		predecessor.AdminDSN, predecessor.Admin.Password, successor.AdminDSN, successor.Admin.Password,
	} {
		if forbidden != "" && bytes.Contains(payload, []byte(forbidden)) {
			t.Fatal("credential material appeared in the summary")
		}
	}
	if len(summary.Members) != 4 || summary.AdminRoleAlias != adminDatabaseRole ||
		!strings.HasPrefix(summary.CanonicalSHA256, "sha256:") {
		t.Fatalf("summary=%+v", summary)
	}
	if !reflect.DeepEqual(summary.Members[:3], mustInspectAdminCredentialSet(t, paths, predecessor.SetRef).Members[:3]) {
		t.Fatal("inherited member summaries changed")
	}
}

func adminCredentialStageTestPaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	t.Setenv(AllowTestRootEnv, "1")
	t.Setenv(TestRootEnv, root)
	paths, err := PathsFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	stageDatabaseCredentials(t, paths)
	return paths
}

func adminCredentialTestEntropy() *bytes.Reader {
	return bytes.NewReader(bytes.Repeat([]byte{0x5a, 0x17, 0xc3, 0x8e, 0x41}, 128))
}

func mustInspectAdminCredentialSet(t *testing.T, paths Paths, setRef string) AdminCredentialSetSummary {
	t.Helper()
	summary, err := InspectAdminCredentialSet(paths, setRef)
	if err != nil {
		t.Fatal(err)
	}
	return summary
}
