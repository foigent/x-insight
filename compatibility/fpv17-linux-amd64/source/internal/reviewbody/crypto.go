package reviewbody

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"
)

var ErrInvalid = errors.New("invalid review body")
var ErrIntegrity = errors.New("review body integrity failure")
var ErrKeyUnavailable = errors.New("review body key unavailable")
var ErrPolicy = errors.New("review body capture policy disallows admission")
var ErrIdentityPending = errors.New("review body account identity pending")
var ErrScope = errors.New("review body source scope changed")
var ErrProjectBindingPending = errors.New("review body XI project binding is missing")
var ErrQuota = errors.New("review body project quota exhausted")
var ErrConflict = errors.New("review body identity conflicts with accepted evidence")
var ErrExpired = errors.New("review body reservation or retention expired")

var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

// Identity is the immutable owner and source selector. It is authenticated
// alongside the ciphertext, so a database row cannot be moved to another
// project, event, or retention deadline without detection.
type Identity struct {
	SourceKind     string    `json:"source_kind"`
	SourceSystemID string    `json:"source_system_id"`
	SourceInstance string    `json:"source_instance"`
	OrgID          string    `json:"org_id"`
	ProjectID      string    `json:"project_id"`
	EventRef       string    `json:"event_ref"`
	EventRevision  int64     `json:"event_revision"`
	EventHash      string    `json:"event_hash"`
	SourceFactID   string    `json:"source_fact_id"`
	SourceRevision string    `json:"source_revision"`
	SourceSHA256   string    `json:"source_sha256"`
	ContentKind    string    `json:"content_kind"`
	ContentRef     string    `json:"content_ref"`
	ByteDomain     string    `json:"byte_domain"`
	PolicyVersion  string    `json:"policy_version"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type Sealed struct {
	KeyID      string
	Nonce      []byte
	Ciphertext []byte
	SHA256     string
	PlainBytes int64
}

type KeyRing struct {
	ActiveID string
	Keys     map[string][]byte
}

func (r KeyRing) Validate() error {
	_, err := r.key(r.ActiveID)
	return err
}

// FreezeIdentity must be used for both the INSERT parameters and Seal. A
// timestamptz written from a non-microsecond Go time can otherwise be rounded
// by PostgreSQL to a different AAD value.
func FreezeIdentity(identity Identity) Identity {
	identity.ExpiresAt = identity.ExpiresAt.UTC().Truncate(time.Microsecond)
	return identity
}

func (r KeyRing) key(id string) ([]byte, error) {
	if !keyIDPattern.MatchString(id) {
		return nil, ErrKeyUnavailable
	}
	key := r.Keys[id]
	if len(key) != 32 {
		return nil, ErrKeyUnavailable
	}
	return key, nil
}

func aad(identity Identity) ([]byte, error) {
	if identity.SourceSystemID == "" || identity.SourceInstance == "" || identity.OrgID == "" ||
		identity.ProjectID == "" || identity.ContentKind == "" || identity.ContentRef == "" ||
		identity.ByteDomain == "" || identity.PolicyVersion == "" || identity.ExpiresAt.IsZero() {
		return nil, ErrInvalid
	}
	validEvent := identity.SourceKind == "work_event" && identity.EventRef != "" && identity.EventRevision > 0 && identity.EventHash != "" &&
		identity.SourceFactID == "" && identity.SourceRevision == "" && identity.SourceSHA256 == ""
	validUR := identity.SourceKind == "ur_c" && identity.SourceFactID != "" && identity.SourceRevision != "" && identity.SourceSHA256 != "" &&
		identity.EventRef == "" && identity.EventRevision == 0 && identity.EventHash == ""
	if !validEvent && !validUR {
		return nil, ErrInvalid
	}
	// PostgreSQL timestamptz stores microseconds. Freeze this precision before
	// sealing so a DB round trip does not change the authenticated deadline.
	identity = FreezeIdentity(identity)
	return json.Marshal(identity)
}

// Seal operates on one already bounded item. Callers must enforce an
// independently measured item and concurrency limit before reading bytes.
func (r KeyRing) Seal(identity Identity, plain []byte, random io.Reader) (Sealed, error) {
	var result Sealed
	associated, err := aad(identity)
	if err != nil {
		return result, ErrInvalid
	}
	key, err := r.key(r.ActiveID)
	if err != nil {
		return result, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return result, ErrKeyUnavailable
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return result, ErrKeyUnavailable
	}
	if random == nil {
		random = rand.Reader
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(random, nonce); err != nil {
		return result, err
	}
	digest := sha256.Sum256(plain)
	return Sealed{KeyID: r.ActiveID, Nonce: nonce,
		Ciphertext: gcm.Seal(nil, nonce, plain, associated),
		SHA256:     "sha256:" + hex.EncodeToString(digest[:]), PlainBytes: int64(len(plain))}, nil
}

// Open authenticates all owner fields and checks the frozen source digest
// before any caller can publish plaintext to an HTTP response.
func (r KeyRing) Open(identity Identity, object Sealed) ([]byte, error) {
	associated, err := aad(identity)
	if err != nil || object.PlainBytes < 0 || object.SHA256 == "" {
		return nil, ErrInvalid
	}
	key, err := r.key(object.KeyID)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrKeyUnavailable
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrKeyUnavailable
	}
	if len(object.Nonce) != gcm.NonceSize() || len(object.Ciphertext) < gcm.Overhead() {
		return nil, ErrIntegrity
	}
	plain, err := gcm.Open(nil, object.Nonce, object.Ciphertext, associated)
	if err != nil {
		return nil, ErrIntegrity
	}
	digest := sha256.Sum256(plain)
	if int64(len(plain)) != object.PlainBytes || "sha256:"+hex.EncodeToString(digest[:]) != object.SHA256 {
		return nil, ErrIntegrity
	}
	return plain, nil
}
