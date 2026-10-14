//go:build linux

package hostctl

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/foigent/x-insight/internal/reviewbody"
)

// The dedicated backend step is mandatory evidence; an ordinary skipped test
// does not establish the real capless root/nonzero group permission contract.
func TestCaplessRootManagedReferenceWriters(t *testing.T) {
	root := requireCaplessReferenceRoot(t)
	e := &Engine{Paths: Paths{EtcDir: filepath.Join(root, "etc"), FeedCredentialState: filepath.Join(root, "state", "feed.json")}}
	if e.Paths.IsTestRoot {
		t.Fatal("production ownership path must execute")
	}
	if err := e.ensureFeedCredentialDirectories(); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(e.Paths.EtcDir, "connector-machines", "xci_AAAAAAAAAAAAAAAAAAAAAA", "policy.key")
	if err := e.writeConnectorEnvironmentExclusive(key, []byte("synthetic-test-value\n")); err != nil {
		t.Fatal(err)
	}
	if err := e.publishReference(key+".prepared.json", []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0600, 0644} {
		path := filepath.Join(e.Paths.EtcDir, "published-"+mode.String())
		if err := e.publishReference(path, []byte("public-fixture\n"), mode); err != nil {
			t.Fatal(err)
		}
		if err := e.validateRootReference(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	canonical, err := buildManagedFeedConfig("https://192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(e.Paths.EtcDir, "managed-feed.env")
	if err := e.writeManagedFeedConfigExclusive(configPath, canonical.contents); err != nil {
		t.Fatal(err)
	}
	if _, present, err := e.readManagedFeedConfigOptional(configPath); err != nil || !present {
		t.Fatal("capless-created feed config rejected", err)
	}
	feed := filepath.Join(e.Paths.EtcDir, "feed.key")
	metadata, err := e.writeFeedCredentialExclusive(feed, "ur-xf1-"+strings.Repeat("a", 64))
	if err != nil || metadata.OwnerUID != 0 || metadata.OwnerGID != uint32(os.Getegid()) {
		t.Fatal("feed writer/actual metadata differs", err)
	}
	for _, path := range []string{key, key + ".prepared.json", feed} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if stat.Uid != 0 || stat.Gid != uint32(os.Getegid()) || stat.Nlink != 1 || info.Mode().Perm() != 0600 {
			t.Fatal("actual protected file metadata differs")
		}
		if err := e.validateRootReference(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{filepath.Dir(key), filepath.Dir(filepath.Dir(key))} {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatal("private parent widened", err)
		}
	}
	nonroot := filepath.Join(root, "nonroot.key")
	untrusted, err := os.Lstat(nonroot)
	if err != nil || untrusted.Sys().(*syscall.Stat_t).Uid == 0 || untrusted.Mode().Perm() != 0600 {
		t.Fatal("nonroot fixture must actually be ordinary0600/nonroot", err)
	}
	if err := e.validateRootReference(nonroot, 0600); err == nil {
		t.Fatal("nonroot reference accepted")
	}
	legacy := filepath.Join(root, "legacy-root.key")
	info, err := os.Lstat(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if info.Sys().(*syscall.Stat_t).Gid != 0 {
		t.Fatal("legacy fixture must actually be root:root")
	}
	if err := e.validateRootReference(legacy, 0600); err != nil {
		t.Fatal("legacy reference rejected", err)
	}
	for _, mode := range []os.FileMode{0640, 0660, 0666} {
		if err := os.Chmod(key, mode); err != nil {
			t.Fatal(err)
		}
		if err := e.validateRootReference(key, 0600); err == nil {
			t.Fatal("unsafe secret mode accepted")
		}
	}
	if err := os.Chmod(key, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(key, key+".link"); err != nil {
		t.Fatal(err)
	}
	if err := e.validateRootReference(key, 0600); err == nil {
		t.Fatal("hardlink accepted")
	}
	if err := os.Symlink(feed, key+".symlink"); err != nil {
		t.Fatal(err)
	}
	if err := e.validateRootReference(key+".symlink", 0600); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := e.writeConnectorEnvironmentExclusive(key+".symlink", []byte("replace")); err == nil {
		t.Fatal("exclusive writer replaced existing symlink")
	}
}

func requireCaplessReferenceRoot(t *testing.T) string {
	t.Helper()
	if os.Getenv("XINSIGHT_CAPLESS_REFERENCE_TEST") != "1" {
		t.Skip("requires dedicated capless Linux backend step")
	}
	if os.Geteuid() != 0 || os.Getegid() == 0 {
		t.Fatal("requires real root with nonzero group")
	}
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string][]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		parts := strings.Fields(line)
		if len(parts) > 0 {
			fields[strings.TrimSuffix(parts[0], ":")] = parts[1:]
		}
	}
	for _, name := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
		if len(fields[name]) != 1 || fields[name][0] != "0000000000000000" {
			t.Fatalf("%s must actually be empty", name)
		}
	}
	if len(fields["Groups"]) != 0 || len(fields["NoNewPrivs"]) != 1 || fields["NoNewPrivs"][0] != "1" {
		t.Fatal("supplementary groups or NNP differ")
	}
	root := os.Getenv("XINSIGHT_CAPLESS_REFERENCE_ROOT")
	if filepath.Dir(root) != "/run" || !strings.HasPrefix(filepath.Base(root), "xi-hostctl-credential-") {
		t.Fatal("requires dedicated precreated root-owned /run/xi-hostctl-credential-* fixture")
	}
	return root
}

func TestCaplessRootWorkerLoadedProof(t *testing.T) {
	root := requireCaplessReferenceRoot(t)
	for _, tc := range []struct {
		name, directory string
		mode            os.FileMode
		fileMode        os.FileMode
		allowed         bool
	}{
		{"legacy", "legacy-worker", 0700, 0600, false},
		{"private", "private-worker", 0750, 0600, false},
		{"group", "group-worker", 0750, 0640, true},
		{"writable", "writable-worker", 0750, 0660, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(root, tc.directory, "loaded.json")
			info, err := os.Lstat(filepath.Dir(path))
			if err != nil || !info.IsDir() || info.Mode().Perm() != tc.mode {
				t.Fatal("worker proof directory fixture differs", err)
			}
			stat := info.Sys().(*syscall.Stat_t)
			if stat.Uid == 0 || stat.Gid != uint32(os.Getegid()) {
				t.Fatal("worker proof directory identity differs")
			}
			if tc.mode == 0750 {
				file, err := os.Lstat(path)
				if err != nil || !file.Mode().IsRegular() || file.Mode().Perm() != tc.fileMode {
					t.Fatal("worker proof file fixture differs", err)
				}
				owner := file.Sys().(*syscall.Stat_t)
				if owner.Uid == 0 || owner.Gid != uint32(os.Getegid()) {
					t.Fatal("worker proof file identity differs")
				}
			}
			var proof reviewbody.LoadedProof
			err = readSourceLoadedProof(path, &proof)
			if (err == nil) != tc.allowed {
				t.Fatalf("capless worker proof allowed=%t, want=%t: %v", err == nil, tc.allowed, err)
			}
		})
	}
}
