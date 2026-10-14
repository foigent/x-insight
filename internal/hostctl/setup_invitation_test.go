package hostctl

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

func TestSetupInvitationSeedReplayExpiryAndLiveActorAreIndependentOfBodyKey(t *testing.T) {
	e, c := managedCustodyFixture(t)
	if err := e.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatal(err)
	}
	putManagedFile(t, e.Paths.TrustedTLSProxyEnv, []byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xi.example\n"), 0644)
	if err := e.SetConnectorTargetOrigin("https://8.130.18.85"); err != nil {
		t.Fatal(err)
	}
	subject := "xs_0123456789abcdef"
	accounts := access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{{SubjectID: subject, Username: "setup-admin", Role: access.RolePlatformAdmin, PasswordHash: cleanBaselineTestPasswordHash}}}
	writeHostJSON(t, e.Paths.AccessAccountsFile, accounts, 0600)
	now := time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Truncate(time.Second).Add(123456789 * time.Nanosecond)
	e.Now = func() time.Time { return now }
	e.Random = bytes.NewReader(bytes.Repeat([]byte{0x71}, 64))
	put := connectorcontrol.SetupConsentPut{SchemaVersion: connectorcontrol.SetupConsentSchema, RequestID: "12345678-1234-4234-8234-123456789abc", Custody: &connectorcontrol.SetupCustody{Mode: "full_content", RetentionSeconds: 90 * 86400, ProjectQuotaBytes: 5 << 30, MaxItemBytes: 32 << 20}}
	hash := "sha256:" + strings.Repeat("a", 64)
	var first []byte
	var original SetupInvitationSeed
	run := func(seed SetupInvitationSeed, random io.Reader) error {
		raw, err := io.ReadAll(random)
		if err != nil {
			return err
		}
		if first == nil {
			first = raw
			original = seed
		} else if !bytes.Equal(first, raw) || seed != original {
			t.Fatal("replay changed original entropy or expiry")
		}
		return nil
	}
	if err := e.WithSetupInvitationSeed(t.Context(), subject, hash, put, false, run); err != nil {
		t.Fatal(err)
	}
	if original.CreatedAt.Location() != time.UTC || original.CreatedAt.Nanosecond()%1000 != 0 || original.ExpiresAt.Nanosecond()%1000 != 0 {
		t.Fatal("root seed is not canonical PG-precision UTC")
	}
	// Same request replays after body key changes; body ciphertext key is never invitation authority.
	putManagedFile(t, c.KeyFile, []byte(strings.Repeat("43", 32)+"\n"), 0600)
	e.Random = bytes.NewReader(nil)
	if err := e.WithSetupInvitationSeed(t.Context(), subject, hash, put, true, run); err != nil {
		t.Fatal(err)
	}
	if err := e.WithSetupInvitationSeed(t.Context(), subject, "sha256:"+strings.Repeat("b", 64), put, true, run); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("conflicting content accepted", err)
	}
	accounts.Accounts[0].Disabled = true
	writeHostJSON(t, e.Paths.AccessAccountsFile, accounts, 0600)
	if err := e.WithSetupInvitationSeed(t.Context(), subject, hash, put, true, run); err == nil {
		t.Fatal("revoked actor replayed")
	}
	accounts.Accounts[0].Disabled = false
	writeHostJSON(t, e.Paths.AccessAccountsFile, accounts, 0600)
	now = original.ExpiresAt
	if err := e.WithSetupInvitationSeed(t.Context(), subject, hash, put, true, run); !errors.Is(err, connectorcontrol.ErrExpired) {
		t.Fatal("expiry extended on replay", err)
	}
	now = original.CreatedAt
	seedPath := filepath.Join(e.Paths.EtcDir, "connector-invitations", put.RequestID+".seed.json")
	if err := os.Rename(seedPath, seedPath+".fixture"); err != nil {
		t.Fatal(err)
	}
	if err := e.WithSetupInvitationSeed(t.Context(), subject, hash, put, true, run); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("missing accepted request seed regenerated", err)
	}
	if _, err := os.Stat(seedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failure published another seed")
	}
}

func TestSetupPeerAcceptsOriginalPublicTLSConfigurationAndRejectsUnsafeReferences(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	if err := e.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatal(err)
	}
	path := e.Paths.TrustedTLSProxyEnv
	raw := []byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xi.example\n")
	putManagedFile(t, path, raw, 0644)
	if err := e.SetConnectorTargetOrigin("https://ur.example"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0644, 0600} {
		putManagedFile(t, path, raw, mode)
		peer, err := e.SetupPeerConfig()
		if err != nil || peer.XInsightOrigin != "https://xi.example" || peer.UniRouterInstallOrigin != "https://ur.example" {
			t.Fatal("original public TLS config rejected", err)
		}
	}
	for _, mode := range []os.FileMode{0664, 0666} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := e.SetupPeerConfig(); err == nil {
			t.Fatal("writable TLS configuration accepted")
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	original := path + ".original"
	if err := os.Rename(path, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(original, path); err != nil {
		t.Fatal(err)
	}
	if _, err := e.SetupPeerConfig(); err == nil {
		t.Fatal("symlink TLS configuration accepted")
	}
}
