package access

import (
	"strings"
	"testing"
)

func TestArgon2idDeriveParseAndVerifyUsesExactBoundedProfile(t *testing.T) {
	password := []byte("correct horse battery staple")
	encoded, err := DerivePasswordHash(password)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	for _, fragment := range []string{"$argon2id$", "$v=19$", "$m=19456,t=2,p=1$"} {
		if !strings.Contains(encoded, fragment) {
			t.Fatalf("hash is missing %q: %s", fragment, encoded)
		}
	}
	hash, err := ParsePasswordHash(encoded)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !hash.Verify(password) {
		t.Fatal("correct password was rejected")
	}
	if hash.Verify([]byte("incorrect password")) {
		t.Fatal("incorrect password was accepted")
	}
}

func TestArgon2idRejectsWeakMalformedAndOversizedInputs(t *testing.T) {
	if _, err := DerivePasswordHash([]byte("short")); err == nil {
		t.Fatal("short password was accepted")
	}
	if _, err := DerivePasswordHash(make([]byte, MaxPasswordBytes+1)); err == nil {
		t.Fatal("oversized password was accepted")
	}
	valid, err := DerivePasswordHash([]byte("valid bounded password"))
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		"",
		strings.Replace(valid, "argon2id", "argon2i", 1),
		strings.Replace(valid, "m=19456", "m=1024", 1),
		strings.Replace(valid, "t=2", "t=02", 1),
		valid + "=",
	} {
		if _, err := ParsePasswordHash(invalid); err == nil {
			t.Fatalf("invalid hash accepted: %s", invalid)
		}
	}
}
