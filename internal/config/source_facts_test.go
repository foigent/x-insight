package config

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestReviewBodyKeyFileIsSeparateExactAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review-body.key")
	want := bytes.Repeat([]byte{0x42}, 32)
	if err := os.WriteFile(path, []byte(hex.EncodeToString(want)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadReviewBodyKeyFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("load separate key: %v", err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReviewBodyKeyFile(path); err == nil {
		t.Fatal("group-readable key accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-a-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReviewBodyKeyFile(path); err == nil {
		t.Fatal("malformed key accepted")
	}
}

func TestSourceFactsCredentialIsExplicitAndPrivate(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "source.key")
	if e := os.WriteFile(p, []byte("synthetic-source-only\n"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv(UniRouterFeedKeyFileEnv, p)
	if _, e := LoadSourceFactsServiceKey(""); e == nil {
		t.Fatal("silently reused I-01 credential")
	}
	key, e := LoadSourceFactsServiceKey(p)
	if e != nil || key != "synthetic-source-only" {
		t.Fatal("explicit credential", e)
	}
	if e = os.Chmod(p, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadSourceFactsServiceKey(p); e == nil {
		t.Fatal("public credential accepted")
	}
	if e = os.Chmod(p, 0640); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadSourceFactsServiceKey(p); e == nil {
		t.Fatal("group-readable source credential accepted")
	}
	if e = os.Chmod(p, 0600); e != nil {
		t.Fatal(e)
	}
	hard := filepath.Join(root, "hardlink")
	if e = os.Link(p, hard); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadSourceFactsServiceKey(p); e == nil {
		t.Fatal("hardlinked source credential accepted")
	}
	link := filepath.Join(root, "link")
	if e = os.Symlink(p, link); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadSourceFactsServiceKey(link); e == nil {
		t.Fatal("symlink credential accepted")
	}
}
