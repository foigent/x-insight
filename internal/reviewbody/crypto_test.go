package reviewbody

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestReviewBodyCipherAuthenticatesScopeAndDeadline(t *testing.T) {
	identity := Identity{SourceKind: "work_event", SourceSystemID: "11111111-1111-4111-8111-111111111111",
		SourceInstance: "agent-a", OrgID: "7", ProjectID: "9", EventRef: "oc:one",
		EventRevision: 1, EventHash: "sha256:source", ContentKind: "tool_output",
		ContentRef: "body:one", ByteDomain: "openclaw.hook.utf8.v1",
		PolicyVersion: "3:0", ExpiresAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	ring := KeyRing{ActiveID: "review-1", Keys: map[string][]byte{"review-1": bytes.Repeat([]byte{42}, 32)}}
	plain := []byte("synthetic tool result")
	sealed, err := ring.Seal(identity, plain, bytes.NewReader(bytes.Repeat([]byte{7}, 12)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ring.Open(identity, sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open: %q %v", got, err)
	}
	changed := identity
	changed.ProjectID = "10"
	if _, err := ring.Open(changed, sealed); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("cross-project moved ciphertext: %v", err)
	}
	changed = identity
	changed.ExpiresAt = identity.ExpiresAt.Add(time.Hour)
	if _, err := ring.Open(changed, sealed); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("deadline changed: %v", err)
	}
	corrupt := sealed
	corrupt.Ciphertext = bytes.Clone(sealed.Ciphertext)
	corrupt.Ciphertext[0] ^= 1
	if _, err := ring.Open(identity, corrupt); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("corrupt ciphertext: %v", err)
	}
	if _, err := (KeyRing{}).Open(identity, sealed); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("missing key: %v", err)
	}
	zero, err := ring.Seal(identity, []byte{}, bytes.NewReader(bytes.Repeat([]byte{8}, 12)))
	if err != nil {
		t.Fatal(err)
	}
	openedZero, err := ring.Open(identity, zero)
	if err != nil || len(openedZero) != 0 || zero.PlainBytes != 0 {
		t.Fatalf("explicit zero-byte body: %d %v", len(openedZero), err)
	}
	precise := identity
	precise.ExpiresAt = precise.ExpiresAt.Add(789 * time.Nanosecond)
	frozen := FreezeIdentity(precise)
	if frozen.ExpiresAt.Nanosecond()%1000 != 0 {
		t.Fatalf("deadline not database-safe: %s", frozen.ExpiresAt)
	}
	withFraction, err := ring.Seal(precise, plain, bytes.NewReader(bytes.Repeat([]byte{9}, 12)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ring.Open(frozen, withFraction); err != nil {
		t.Fatalf("frozen deadline readback: %v", err)
	}
}
