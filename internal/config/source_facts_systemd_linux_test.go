package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReviewBodyNativeACLAbsenceFailsClosed(t *testing.T) {
	for _, value := range []struct {
		err  error
		want bool
	}{{unix.ENODATA, true}, {unix.ENOTSUP, true}, {fmt.Errorf("wrapped: %w", unix.ENODATA), true},
		{nil, false}, {unix.EACCES, false}, {unix.EPERM, false}, {unix.EIO, false}, {unix.ERANGE, false}, {fmt.Errorf("unsupported"), false}} {
		if reviewBodyAccessACLAbsent(value.err) != value.want {
			t.Fatal("access ACL evidence was not exact", value.err)
		}
	}
}

func TestSourceCredentialACLRejectsAdditionalAccess(t *testing.T) {
	// Native systemd 255 root-owned 0440 credential, service UID 1000.
	acl, err := hex.DecodeString("0200000001000400ffffffff02000400e803000004000000ffffffff10000400ffffffff20000000ffffffff")
	if err != nil || !sourceCredentialACL(acl, 1000) || sourceCredentialACL(acl, 1001) {
		t.Fatal("service identity ACL", err)
	}
	for _, offset := range []int{14, 22, 30, 38} {
		changed := append([]byte(nil), acl...)
		changed[offset] |= 2 // service, group, mask or other write permission.
		if sourceCredentialACL(changed, 1000) {
			t.Fatal("additional ACL permission accepted", offset)
		}
	}
	for _, offset := range []int{22, 38} {
		changed := append([]byte(nil), acl...)
		changed[offset] = 4 // Group or other read access must stay absent.
		if sourceCredentialACL(changed, 1000) {
			t.Fatal("additional reader accepted", offset)
		}
	}
	if sourceCredentialACL(append(acl, make([]byte, 8)...), 1000) {
		t.Fatal("additional principal accepted")
	}
}

func TestReviewBodySystemdRejectsWrongNameUnitAndWritableFilesystem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review-body.key")
	if err := os.WriteFile(path, []byte("fake"), 0440); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"/run/credentials/xinsight.service", "/run/credentials/xinsight-source-facts.service", "/run/credentials/xinsight-feed.service"} {
		t.Setenv(CredentialsDirEnv, dir)
		for _, name := range []string{"review-body.key", "source-profile-0.key", "review.key"} {
			want := ""
			if name == "review-body.key" && dir != "/run/credentials/xinsight-feed.service" {
				want = dir
			}
			if got := reviewBodyCredentialDirectory(filepath.Join(dir, name)); got != want {
				t.Fatal("incorrect purpose/unit boundary", got, want)
			}
			if validReviewBodySystemdCredential(filepath.Join(dir, name), info, nil) {
				t.Fatal("ordinary/wrong copy accepted", dir, name)
			}
		}
	}
}
