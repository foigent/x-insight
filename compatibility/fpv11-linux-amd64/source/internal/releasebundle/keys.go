package releasebundle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func GenerateKeyPair(privatePath string, trustStorePath string, now time.Time) (string, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(publicKey)
	keyID := "ed25519:sha256:" + hex.EncodeToString(digest[:])
	createdAt := now.UTC().Truncate(time.Second)
	privateFile := PrivateKeyFile{
		SchemaVersion: PrivateKeySchemaVersion,
		Algorithm:     SignatureAlgorithm,
		KeyID:         keyID,
		CreatedAt:     createdAt,
		PrivateKey:    base64.StdEncoding.EncodeToString(privateKey),
	}
	trustStore := TrustStore{
		SchemaVersion: TrustStoreSchemaVersion,
		Keys: []TrustedKey{
			{
				KeyID:       keyID,
				Algorithm:   SignatureAlgorithm,
				PublicKey:   base64.StdEncoding.EncodeToString(publicKey),
				Status:      "active",
				NotBefore:   createdAt,
				Description: "X-Insight release signing key",
			},
		},
	}
	if err := writeJSONExclusive(privatePath, privateFile, 0o600); err != nil {
		return "", err
	}
	if err := writeJSONExclusive(trustStorePath, trustStore, 0o644); err != nil {
		return "", err
	}
	return keyID, nil
}

func loadPrivateKey(path string) (PrivateKeyFile, ed25519.PrivateKey, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return PrivateKeyFile{}, nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return PrivateKeyFile{}, nil, fmt.Errorf("release private key must be a regular owner-only file")
	}
	var file PrivateKeyFile
	if err := decodeStrictFile(path, &file, 64<<10); err != nil {
		return PrivateKeyFile{}, nil, err
	}
	if file.SchemaVersion != PrivateKeySchemaVersion || file.Algorithm != SignatureAlgorithm || !keyIDPattern.MatchString(file.KeyID) || file.CreatedAt.IsZero() {
		return PrivateKeyFile{}, nil, fmt.Errorf("invalid release private key metadata")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(file.PrivateKey)
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return PrivateKeyFile{}, nil, fmt.Errorf("invalid release private key bytes")
	}
	privateKey := ed25519.PrivateKey(decoded)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(publicKey)
	if file.KeyID != "ed25519:sha256:"+hex.EncodeToString(digest[:]) {
		return PrivateKeyFile{}, nil, fmt.Errorf("release private key ID does not match public key")
	}
	return file, privateKey, nil
}

func LoadTrustStore(path string) (TrustStore, error) {
	var store TrustStore
	if err := decodeStrictFile(path, &store, 1<<20); err != nil {
		return TrustStore{}, err
	}
	if store.SchemaVersion != TrustStoreSchemaVersion || len(store.Keys) == 0 {
		return TrustStore{}, fmt.Errorf("invalid release trust store")
	}
	seen := make(map[string]struct{}, len(store.Keys))
	for _, key := range store.Keys {
		if !keyIDPattern.MatchString(key.KeyID) || key.Algorithm != SignatureAlgorithm || key.NotBefore.IsZero() {
			return TrustStore{}, fmt.Errorf("invalid trusted release key metadata")
		}
		switch key.Status {
		case "active", "revoked":
		default:
			return TrustStore{}, fmt.Errorf("invalid trusted release key status")
		}
		if key.NotAfter != nil && !key.NotAfter.After(key.NotBefore) {
			return TrustStore{}, fmt.Errorf("invalid trusted release key validity window")
		}
		if _, exists := seen[key.KeyID]; exists {
			return TrustStore{}, fmt.Errorf("duplicate trusted release key")
		}
		seen[key.KeyID] = struct{}{}
		publicKey, err := base64.StdEncoding.Strict().DecodeString(key.PublicKey)
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			return TrustStore{}, fmt.Errorf("invalid trusted release public key")
		}
		digest := sha256.Sum256(publicKey)
		if key.KeyID != "ed25519:sha256:"+hex.EncodeToString(digest[:]) {
			return TrustStore{}, fmt.Errorf("trusted release key ID does not match public key")
		}
	}
	return store, nil
}

func trustedPublicKey(store TrustStore, keyID string, createdAt time.Time) (ed25519.PublicKey, error) {
	for _, key := range store.Keys {
		if key.KeyID != keyID {
			continue
		}
		if key.Status != "active" || createdAt.Before(key.NotBefore) || (key.NotAfter != nil && createdAt.After(*key.NotAfter)) {
			return nil, ErrUntrustedRelease
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(key.PublicKey)
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, ErrUntrustedRelease
		}
		return ed25519.PublicKey(decoded), nil
	}
	return nil, ErrUntrustedRelease
}

func decodeStrictFile(path string, target any, limit int64) error {
	before, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > limit {
		return fmt.Errorf("JSON file size or type is invalid: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) {
		return fmt.Errorf("JSON file changed while opening: %s", path)
	}
	decoder := json.NewDecoder(io.LimitReader(file, limit+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("JSON file contains trailing content: %s", path)
	}
	return nil
}

func decodeStrictBytes(value []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("JSON contains trailing content")
	}
	return nil
}

func writeJSONExclusive(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
