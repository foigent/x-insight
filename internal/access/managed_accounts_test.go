package access

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type credentialFixtureStore struct {
	ManagedAccountStore
	credential ManagedCredential
	err        error
}

func (s credentialFixtureStore) ManagedCredential(context.Context, string) (ManagedCredential, error) {
	return s.credential, s.err
}

func TestDurableProviderRejectsMalformedHashEvenWithKnownDummyPassword(t *testing.T) {
	valid, e := DerivePasswordHash([]byte("synthetic valid provider password"))
	if e != nil {
		t.Fatal(e)
	}
	base := ManagedCredential{ManagedAccount: ManagedAccount{SubjectID: "xs_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Username: "reader", Role: RoleScopedReader, Revision: 1}, PasswordHash: valid}
	cases := []struct {
		name, hash, password string
		disabled, pending    bool
		storeErr             error
		accepted             bool
	}{
		{name: "valid", hash: valid, password: "synthetic valid provider password", accepted: true},
		{name: "wrong password", hash: valid, password: "xinsight-dummy-password"},
		{name: "malformed hash dummy input", hash: "malformed", password: "xinsight-dummy-password"},
		{name: "empty hash dummy input", hash: "", password: "xinsight-dummy-password"},
		{name: "unsupported algorithm dummy input", hash: strings.Replace(valid, "argon2id", "argon2i", 1), password: "xinsight-dummy-password"},
		{name: "unsupported parameters dummy input", hash: strings.Replace(valid, "m=19456", "m=1", 1), password: "xinsight-dummy-password"},
		{name: "unknown account dummy input", password: "xinsight-dummy-password", storeErr: ErrInvalidCredentials},
		{name: "disabled", hash: valid, password: "synthetic valid provider password", disabled: true},
		{name: "activation required", hash: valid, password: "synthetic valid provider password", pending: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			record := base
			record.PasswordHash = c.hash
			record.Disabled = c.disabled
			record.NeedsActivation = c.pending
			p, e := NewDurableProvider(credentialFixtureStore{credential: record, err: c.storeErr})
			if e != nil {
				t.Fatal(e)
			}
			principal, e := p.AuthenticateCurrent("reader", []byte(c.password))
			if c.accepted {
				if e != nil || principal.SubjectID != base.SubjectID {
					t.Fatal("valid credential rejected")
				}
			} else if !errors.Is(e, ErrInvalidCredentials) || principal != (Principal{}) {
				t.Fatal("invalid hash/state returned a principal")
			}
		})
	}
}
