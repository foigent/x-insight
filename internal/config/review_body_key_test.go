package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewBodyRootSourceIsOwnerOnlySingleLinkAndNeverLeaks(t *testing.T) {
	for _, kind := range []string{"valid", "group-readable", "symlink", "hardlink", "bad-key", "source-purpose"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "review-body.key")
			secret := strings.Repeat("d0", 32)
			if err := os.WriteFile(path, []byte(secret+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "group-readable":
				if err := os.Chmod(path, 0440); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(path, path+".link"); err != nil {
					t.Fatal(err)
				}
				path += ".link"
			case "hardlink":
				if err := os.Link(path, path+".link"); err != nil {
					t.Fatal(err)
				}
			case "bad-key":
				if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			case "source-purpose":
				if err := os.WriteFile(path, []byte("ur-xf1-"+secret), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := LoadReviewBodyKeyFile(path)
			if (err == nil) != (kind == "valid") {
				t.Fatal("unexpected validation", err)
			}
			if err != nil && strings.Contains(err.Error(), secret) {
				t.Fatal("secret leak")
			}
		})
	}
}

func TestMachineCredentialFormatsArePurposeBound(t *testing.T) {
	for _, kind := range []struct {
		token, purpose string
		valid          bool
	}{{"ur-xpm1-" + strings.Repeat("a", 64), "xpm1", true}, {"ur-xse1-" + strings.Repeat("b", 64), "xse1", true}, {"ur-xpm1-" + strings.Repeat("a", 64), "xse1", false}, {"ur-xse1-short", "xse1", false}, {"ur-xf1-" + strings.Repeat("a", 64), "xpm1", false}} {
		if (ValidateURMachineToken(kind.token, kind.purpose) == nil) != kind.valid {
			t.Fatal("purpose validation")
		}
	}
}
