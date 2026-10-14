package reviewbody

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestLoadedProofBindsActualKeyConsumerAndCurrentInvocation(t *testing.T) {
	r := KeyRing{ActiveID: "instance", Keys: map[string][]byte{"instance": []byte(strings.Repeat("a", 32))}}
	p, err := r.LoadedProof("xinsight.service", strings.Repeat("1", 32), 321)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.VerifyLoadedProof(p); err != nil {
		t.Fatal(err)
	}
	other := KeyRing{ActiveID: "instance", Keys: map[string][]byte{"instance": []byte(strings.Repeat("b", 32))}}
	if other.VerifyLoadedProof(p) == nil {
		t.Fatal("same key ID concealed different loaded bytes")
	}
	p.PID++
	if r.VerifyLoadedProof(p) == nil {
		t.Fatal("stale PID accepted")
	}
	p.PID--
	p.InvocationID = strings.Repeat("2", 32)
	if r.VerifyLoadedProof(p) == nil {
		t.Fatal("stale invocation accepted")
	}
}

func TestWorkerLoadedProofWriterModeWithPrivateUmask(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "loaded.json")
	old := syscall.Umask(0077)
	t.Cleanup(func() { syscall.Umask(old) })
	if err := writeWorkerLoadedProof(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0640 {
		t.Fatal("worker proof is not a group-readable regular file", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeWorkerLoadedProof(path, []byte("{}")); err == nil {
		t.Fatal("private directory accepted for group-readable proof")
	}
}
