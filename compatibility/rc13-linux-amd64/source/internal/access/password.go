package access

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	Argon2MemoryKiB   uint32 = 19 * 1024
	Argon2Iterations  uint32 = 2
	Argon2Parallelism uint8  = 1
	Argon2SaltBytes          = 16
	Argon2KeyBytes           = 32

	MinPasswordBytes = 12
	MaxPasswordBytes = 1024
)

var ErrInvalidPasswordHash = errors.New("invalid Argon2id password hash")

type PasswordHash struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	salt        []byte
	key         []byte
}

func DerivePasswordHash(password []byte) (string, error) {
	if len(password) < MinPasswordBytes || len(password) > MaxPasswordBytes {
		return "", fmt.Errorf("password must be %d..%d bytes", MinPasswordBytes, MaxPasswordBytes)
	}
	salt := make([]byte, Argon2SaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", errors.New("password salt generation failed")
	}
	key := argon2.IDKey(password, salt, Argon2Iterations, Argon2MemoryKiB, Argon2Parallelism, Argon2KeyBytes)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		Argon2MemoryKiB,
		Argon2Iterations,
		Argon2Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func ParsePasswordHash(encoded string) (PasswordHash, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return PasswordHash{}, ErrInvalidPasswordHash
	}
	parameters := strings.Split(parts[3], ",")
	if len(parameters) != 3 {
		return PasswordHash{}, ErrInvalidPasswordHash
	}
	memory, errMemory := parseUintParameter(parameters[0], "m=", 32)
	iterations, errIterations := parseUintParameter(parameters[1], "t=", 32)
	parallelism, errParallelism := parseUintParameter(parameters[2], "p=", 8)
	if errMemory != nil || errIterations != nil || errParallelism != nil ||
		uint32(memory) != Argon2MemoryKiB ||
		uint32(iterations) != Argon2Iterations ||
		uint8(parallelism) != Argon2Parallelism {
		return PasswordHash{}, ErrInvalidPasswordHash
	}
	salt, errSalt := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	key, errKey := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if errSalt != nil || errKey != nil || len(salt) != Argon2SaltBytes || len(key) != Argon2KeyBytes {
		return PasswordHash{}, ErrInvalidPasswordHash
	}
	return PasswordHash{
		memory:      uint32(memory),
		iterations:  uint32(iterations),
		parallelism: uint8(parallelism),
		salt:        append([]byte(nil), salt...),
		key:         append([]byte(nil), key...),
	}, nil
}

func (hash PasswordHash) Verify(password []byte) bool {
	if len(password) == 0 || len(password) > MaxPasswordBytes ||
		hash.memory != Argon2MemoryKiB ||
		hash.iterations != Argon2Iterations ||
		hash.parallelism != Argon2Parallelism ||
		len(hash.salt) != Argon2SaltBytes ||
		len(hash.key) != Argon2KeyBytes {
		return false
	}
	actual := argon2.IDKey(password, hash.salt, hash.iterations, hash.memory, hash.parallelism, uint32(len(hash.key)))
	return subtle.ConstantTimeCompare(actual, hash.key) == 1
}

func parseUintParameter(value string, prefix string, bits int) (uint64, error) {
	if !strings.HasPrefix(value, prefix) {
		return 0, ErrInvalidPasswordHash
	}
	raw := strings.TrimPrefix(value, prefix)
	if raw == "" || (len(raw) > 1 && raw[0] == '0') {
		return 0, ErrInvalidPasswordHash
	}
	parsed, err := strconv.ParseUint(raw, 10, bits)
	if err != nil {
		return 0, ErrInvalidPasswordHash
	}
	return parsed, nil
}
