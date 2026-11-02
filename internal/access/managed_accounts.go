package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var ErrAccountUnavailable = errors.New("account authority unavailable")
var ErrAccountConflict = errors.New("account revision changed")
var ErrAccountInvalid = errors.New("account operation invalid")

const PermissionAccountsManage Permission = "accounts.manage"

type ManagedAccount struct {
	SubjectID       string `json:"subject_id"`
	Username        string `json:"username"`
	Role            Role   `json:"role"`
	Disabled        bool   `json:"disabled"`
	NeedsActivation bool   `json:"needs_activation"`
	Revision        int64  `json:"revision"`
}
type ManagedCredential struct {
	ManagedAccount
	PasswordHash string `json:"-"`
}
type AccountChange struct {
	Action           string `json:"action"`
	SubjectID        string `json:"subject_id"`
	Username         string `json:"username,omitempty"`
	Role             Role   `json:"role,omitempty"`
	ExpectedRevision int64  `json:"expected_revision"`
}
type AccountChangeResult struct {
	Account   ManagedAccount `json:"account"`
	Token     string         `json:"-"`
	ExpiresAt *time.Time     `json:"expires_at,omitempty"`
}
type ProjectReadChange struct {
	GrantID          string   `json:"grant_id"`
	SubjectID        string   `json:"subject_id"`
	SourceID         string   `json:"source_system_id"`
	Project          string   `json:"source_project_ref"`
	ExpectedRevision int64    `json:"expected_revision"`
	Capabilities     []string `json:"capabilities"`
	Revoke           bool     `json:"revoke"`
	CheckOnly        bool     `json:"check_only"`
}
type ManagedAccountStore interface {
	ManagedCredential(context.Context, string) (ManagedCredential, error)
	CurrentAccount(context.Context, string) (ManagedAccount, error)
	ListAccounts(context.Context) ([]ManagedAccount, error)
	ChangeAccount(context.Context, Principal, AccountChange, string) (AccountChangeResult, error)
	CompletePassword(context.Context, string, string) (ManagedAccount, error)
	ProjectReadOptions(context.Context, Principal) (any, error)
	ChangeProjectRead(context.Context, Principal, ProjectReadChange) (any, error)
}
type DurableProvider struct {
	store ManagedAccountStore
	dummy PasswordHash
}

func NewDurableProvider(store ManagedAccountStore) (*DurableProvider, error) {
	if store == nil {
		return nil, ErrAccountUnavailable
	}
	encoded, e := DerivePasswordHash([]byte("xinsight-dummy-password"))
	if e != nil {
		return nil, e
	}
	dummy, e := ParsePasswordHash(encoded)
	if e != nil {
		return nil, e
	}
	return &DurableProvider{store: store, dummy: dummy}, nil
}
func (p *DurableProvider) Authenticate(username string, password []byte) (Principal, bool) {
	a, e := p.AuthenticateCurrent(username, password)
	return a, e == nil
}
func (p *DurableProvider) AuthenticateCurrent(username string, password []byte) (Principal, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, e := p.store.ManagedCredential(ctx, NormalizeUsername(username))
	hash := p.dummy
	hashValid := false
	if e != nil && !errors.Is(e, ErrInvalidCredentials) {
		return Principal{}, ErrAccountUnavailable
	}
	if e == nil {
		var pe error
		hash, pe = ParsePasswordHash(a.PasswordHash)
		hashValid = pe == nil
		if pe != nil {
			hash = p.dummy
		}
	}
	matches := hash.Verify(password)
	if e != nil || !hashValid || !matches || a.Disabled || a.NeedsActivation {
		return Principal{}, ErrInvalidCredentials
	}
	return Principal{SubjectID: a.SubjectID, Role: a.Role, AuthRevision: a.Revision}, nil
}
func (p *DurableProvider) CurrentPrincipal(subject string) (Principal, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, e := p.store.CurrentAccount(ctx, subject)
	if e != nil {
		return Principal{}, e
	}
	if a.Disabled || a.NeedsActivation {
		return Principal{}, ErrUnauthenticated
	}
	return Principal{SubjectID: a.SubjectID, Role: a.Role, AuthRevision: a.Revision}, nil
}
func (p *DurableProvider) Subjects() ([]LocalSubject, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, e := p.store.ListAccounts(ctx)
	if e != nil {
		return nil, e
	}
	out := make([]LocalSubject, 0, len(a))
	for _, v := range a {
		state := "active"
		if v.Disabled {
			state = "disabled"
		} else if v.NeedsActivation {
			state = "activation_required"
		}
		out = append(out, LocalSubject{SubjectID: v.SubjectID, DisplayName: v.Username, State: state})
	}
	return out, nil
}
func (s *Service) ManagedAccounts() (*DurableProvider, bool) {
	p, ok := s.provider.(*DurableProvider)
	return p, ok
}
func (p *DurableProvider) List(ctx context.Context) ([]ManagedAccount, error) {
	return p.store.ListAccounts(ctx)
}
func (p *DurableProvider) Change(ctx context.Context, actor Principal, c AccountChange) (AccountChangeResult, error) {
	var token, digest string
	if c.Action == "invite" || c.Action == "recover" {
		b := make([]byte, 32)
		if _, e := rand.Read(b); e != nil {
			return AccountChangeResult{}, e
		}
		token = "xi-ac1_" + base64.RawURLEncoding.EncodeToString(b)
		digest = TicketDigest(token)
	}
	if c.Action == "invite" {
		b := make([]byte, 16)
		if _, e := rand.Read(b); e != nil {
			return AccountChangeResult{}, e
		}
		c.SubjectID = "xs_" + hex.EncodeToString(b)
	}
	r, e := p.store.ChangeAccount(ctx, actor, c, digest)
	if e != nil {
		return AccountChangeResult{}, e
	}
	r.Token = token
	return r, nil
}
func TicketDigest(token string) string {
	if len(token) != 50 || token[:7] != "xi-ac1_" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (p *DurableProvider) SetPassword(ctx context.Context, token string, password, confirmation []byte) (ManagedAccount, error) {
	if TicketDigest(token) == "" || subtle.ConstantTimeCompare(password, confirmation) != 1 {
		return ManagedAccount{}, ErrAccountInvalid
	}
	encoded, e := DerivePasswordHash(password)
	if e != nil {
		return ManagedAccount{}, ErrAccountInvalid
	}
	return p.store.CompletePassword(ctx, TicketDigest(token), encoded)
}
func (s *Service) Reauthenticate(a Authentication, password []byte) error {
	p, ok := s.provider.(*DurableProvider)
	if !ok {
		return ErrAccountUnavailable
	}
	select {
	case s.passwordVerifications <- struct{}{}:
		defer func() { <-s.passwordVerifications }()
	default:
		return ErrRateLimited
	}
	current, e := p.CurrentPrincipal(a.Session.SubjectID)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	account, e := p.store.CurrentAccount(ctx, current.SubjectID)
	if e != nil {
		return e
	}
	key := principalFailureKey(account.Username)
	if !s.failures.begin(key, s.now().UTC()) {
		return ErrRateLimited
	}
	verified, e := p.AuthenticateCurrent(account.Username, password)
	if errors.Is(e, ErrAccountUnavailable) {
		s.failures.cancel(key)
	} else {
		s.failures.finish(key, e != nil)
	}
	if e != nil {
		return e
	}
	if verified != current {
		return ErrInvalidCredentials
	}
	return nil
}
func (p *DurableProvider) GrantOptions(ctx context.Context, actor Principal) (any, error) {
	return p.store.ProjectReadOptions(ctx, actor)
}
func (p *DurableProvider) Grant(ctx context.Context, actor Principal, c ProjectReadChange) (any, error) {
	if c.Project == "" || c.Project == "*" || c.GrantID == "" || len(c.Capabilities) == 0 {
		return nil, fmt.Errorf("%w: exact project metadata scope required", ErrAccountInvalid)
	}
	return p.store.ChangeProjectRead(ctx, actor, c)
}

func (p *DurableProvider) WithPrincipal(principal Principal, fn func() error) error {
	guard, ok := p.store.(interface {
		WithAccount(context.Context, Principal, func() error) error
	})
	if !ok {
		return ErrAccountUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return guard.WithAccount(ctx, principal, fn)
}
