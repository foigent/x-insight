package api_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestP3FormalExactPathCleanupIncludesCC15ReporterGrants(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		name := "known-only"
		if unknown {
			name = "unknown-child"
		}
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "account-cli-root")
			grants := filepath.Join(root, "grants")
			if err := os.MkdirAll(grants, 0700); err != nil {
				t.Fatal(err)
			}
			owned := []string{root, grants}
			known := p3FormalOwnedGrantFiles(root)
			if len(known) != 9 {
				t.Fatalf("owned grant count=%d", len(known))
			}
			owned = append(owned, known...)
			// These are the actual grant names emitted by formalGrant, independent
			// of the cleanup list under test. In particular, both CC15 names must
			// exist even if a future cleanup list accidentally omits them.
			actualNames := []string{"reviewer-target", "reader-target", "reviewer-reporter", "reader-reporter", "reader-content", "reviewer-revoke-target", "reviewer-revoke-reporter", "reviewer-action-reporter", "reader-action-reporter"}
			for _, name := range actualNames {
				path := filepath.Join(grants, name+".json")
				if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			unknownPath := filepath.Join(grants, "owner-unknown.json")
			if unknown {
				if err := os.WriteFile(unknownPath, []byte("owner data"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if failed := p3RemoveFormalOwned(owned); failed != unknown {
				t.Fatalf("cleanup failed=%v, unknown=%v", failed, unknown)
			}
			for _, name := range actualNames {
				path := filepath.Join(grants, name+".json")
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("known grant remains: %s, %v", filepath.Base(path), err)
				}
			}
			if unknown {
				if raw, err := os.ReadFile(unknownPath); err != nil || string(raw) != "owner data" {
					t.Fatalf("unknown grant was not retained: %v", err)
				}
			} else if _, err := os.Lstat(root); !os.IsNotExist(err) {
				t.Fatalf("known-only root remains: %v", err)
			}
		})
	}
}
