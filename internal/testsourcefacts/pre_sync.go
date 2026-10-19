package testsourcefacts

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ReplayPreSync35 prepares the real schema35 migration predecessor with the
// pinned pre-sync HTTP consumer. Current Replay always requires real ownership;
// missing sync tables never become a production authorization fallback.
func ReplayPreSync35(t *testing.T, admin *sql.DB, adminDSN, importerDSN, source string, bundle Bundle, generation string) {
	t.Helper()
	var version string
	var absent bool
	if err := admin.QueryRow(`SELECT max(version),to_regclass('public.connector_sync_controls') IS NULL FROM schema_migrations`).Scan(&version, &absent); err != nil || !strings.HasPrefix(version, "000035_") || !absent {
		t.Fatal("exact pre-sync schema35 fixture required", version, absent, err)
	}
	const commit = "2c05fcb38330337b362f36bee7348e2916abf700"
	const tree = "a59f36f034ca8ab7424baaf421246a16788f8ebc"
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	run := func(dir, name string, args ...string) []byte {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("pinned pre-sync helper %s failed: %v\n%s", name, err, out)
		}
		return out
	}
	if strings.TrimSpace(string(run(root, "git", "rev-parse", commit+"^{tree}"))) != tree {
		t.Fatal("pre-sync source tree differs")
	}
	temp := t.TempDir()
	legacy := filepath.Join(temp, "source")
	if err := os.Mkdir(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(temp, "source.tar")
	run(root, "git", "archive", "--format=tar", "--output", archive, commit)
	run(root, "tar", "-xf", archive, "-C", legacy)
	// Only this task-generated test driver is added to the verified old tree.
	// The old Replay/Consumer/HTTP/store implementation stays byte-identical.
	program := `package xinsight_test
import (
 "database/sql"
 "encoding/json"
 "os"
 "testing"
 "github.com/foigent/x-insight/internal/testpostgres"
 "github.com/foigent/x-insight/internal/testsourcefacts"
 _ "github.com/jackc/pgx/v5/stdlib"
)
func TestMain(m *testing.M) { os.Exit(testpostgres.Main(m.Run)) }
func TestPinnedPreSyncReplay(t *testing.T) {
 var r struct { AdminDSN, ImporterDSN, Source, Generation string; Bundle testsourcefacts.Bundle }
 if err := json.NewDecoder(os.Stdin).Decode(&r); err != nil { t.Fatal(err) }
 admin,err := sql.Open("pgx",r.AdminDSN); if err != nil { t.Fatal(err) }; defer admin.Close()
 testsourcefacts.Replay(t,admin,r.ImporterDSN,r.Source,r.Bundle,r.Generation)
}
`
	if err := os.WriteFile(filepath.Join(legacy, "pinned_pre_sync_replay_test.go"), []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(temp, "pre-sync-replay.test")
	run(legacy, "go", "test", "-c", "-o", binary, ".")
	input, err := json.Marshal(struct {
		AdminDSN, ImporterDSN, Source, Generation string
		Bundle                                    Bundle
	}{adminDSN, importerDSN, source, generation, bundle})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestPinnedPreSyncReplay$", "-test.v")
	cmd.Stdin = bytes.NewReader(input) // Credentials are never argv or evidence files.
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pinned schema35 HTTP replay failed: %v\n%s", err, out)
	}
	t.Log("schema35 real HTTP replay passed using exact pre-sync commit", commit, "tree", tree)
}
