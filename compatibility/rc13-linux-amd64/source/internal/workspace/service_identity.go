package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	ServiceBearerPrefix       = "xib_"
	MaxServiceIdentityTTL     = 365 * 24 * time.Hour
	DefaultServiceIdentityTTL = 30 * 24 * time.Hour
)

var serviceTokenPattern = regexp.MustCompile(`^xib_[A-Za-z0-9_-]{43}$`)

type ServiceIdentity struct {
	ID               string     `json:"id"`
	DisplayName      string     `json:"display_name"`
	Status           string     `json:"status"`
	Permissions      []string   `json:"permissions"`
	CreatedBySubject string     `json:"created_by_subject"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	RevokeReason     string     `json:"revoke_reason,omitempty"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"`
}

type ServiceCredential struct {
	ServiceIdentity ServiceIdentity `json:"service_identity"`
	Token           string          `json:"token"`
	TokenPrefix     string          `json:"token_prefix"`
	ExpiresAt       time.Time       `json:"expires_at"`
}

type CreateServiceIdentityInput struct {
	DisplayName string
	ExpiresAt   time.Time
}

type ServiceAuthentication struct {
	Identity    ServiceIdentity
	TokenDigest string
}

type AgentAccessStore interface {
	CreateServiceIdentity(
		ctx context.Context,
		actor Actor,
		input CreateServiceIdentityInput,
		now time.Time,
		random io.Reader,
	) (ServiceCredential, error)
	AuthenticateServiceToken(ctx context.Context, token string, now time.Time) (ServiceAuthentication, error)
	ListServiceIdentities(ctx context.Context, limit int) ([]ServiceIdentity, error)
	RevokeServiceIdentity(ctx context.Context, id string, actor Actor, reason string, now time.Time) (ServiceIdentity, error)
}

func ValidateCreateServiceIdentity(input CreateServiceIdentityInput, now time.Time) error {
	name := strings.TrimSpace(input.DisplayName)
	if len(name) == 0 || len(name) > 120 ||
		input.ExpiresAt.IsZero() || !input.ExpiresAt.After(now) ||
		input.ExpiresAt.After(now.Add(MaxServiceIdentityTTL)) {
		return ErrInvalid
	}
	return nil
}

func NewServiceToken(source io.Reader) (string, string, error) {
	if source == nil {
		source = rand.Reader
	}
	var raw [32]byte
	if _, err := io.ReadFull(source, raw[:]); err != nil {
		return "", "", err
	}
	token := ServiceBearerPrefix + base64.RawURLEncoding.EncodeToString(raw[:])
	if !serviceTokenPattern.MatchString(token) {
		return "", "", errors.New("service token generation failed")
	}
	digest := sha256.Sum256([]byte(token))
	return token, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func ServiceTokenDigest(token string) (string, error) {
	if !serviceTokenPattern.MatchString(token) {
		return "", ErrCredentialInvalid
	}
	digest := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
