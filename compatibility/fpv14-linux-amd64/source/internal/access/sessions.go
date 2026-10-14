package access

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"sort"
	"sync"
	"time"
)

const (
	SessionCookieName            = "__Host-xinsight_session"
	SessionIdleTimeout           = 30 * time.Minute
	SessionAbsoluteTimeout       = 8 * time.Hour
	DefaultMaxSessions           = 500
	DefaultMaxSessionsPerSubject = 5
	DefaultPasswordVerifications = 4
)

var (
	ErrInvalidCredentials = errors.New("authentication failed")
	ErrRateLimited        = errors.New("authentication rate limited")
	ErrUnauthenticated    = errors.New("authentication required")
	ErrForbidden          = errors.New("permission denied")
	ErrSessionNotFound    = errors.New("session not found")
)

type CredentialProvider interface {
	Authenticate(username string, password []byte) (Principal, bool)
}

type ServiceOptions struct {
	Now                    func() time.Time
	Random                 io.Reader
	MaxSessions            int
	MaxSessionsPerSubject  int
	GlobalFailureLimit     int
	GlobalFailureWindow    time.Duration
	PrincipalFailureLimit  int
	PrincipalFailureWindow time.Duration
	PasswordVerifications  int
}

type SessionView struct {
	SessionRef        string       `json:"session_ref"`
	SubjectID         string       `json:"subject_id"`
	Role              Role         `json:"role"`
	Permissions       []Permission `json:"permissions"`
	CreatedAt         time.Time    `json:"created_at"`
	LastSeenAt        time.Time    `json:"last_seen_at"`
	IdleExpiresAt     time.Time    `json:"idle_expires_at"`
	AbsoluteExpiresAt time.Time    `json:"absolute_expires_at"`
}

type Authentication struct {
	Session SessionView
	csrf    string
	digest  [32]byte
}

type session struct {
	principal  Principal
	reference  string
	csrf       string
	createdAt  time.Time
	lastSeenAt time.Time
}

type Service struct {
	mu                    sync.Mutex
	provider              CredentialProvider
	auditor               Auditor
	now                   func() time.Time
	random                io.Reader
	sessions              map[[32]byte]*session
	maxSessions           int
	maxSessionsPerSubject int
	failures              *failureLimiter
	passwordVerifications chan struct{}
}

func NewService(provider CredentialProvider, auditor Auditor, options ServiceOptions) (*Service, error) {
	if provider == nil {
		return nil, errors.New("access credential provider is required")
	}
	if auditor == nil {
		auditor = DiscardAuditor{}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	randomSource := options.Random
	if randomSource == nil {
		randomSource = rand.Reader
	}
	maxSessions := options.MaxSessions
	if maxSessions <= 0 {
		maxSessions = DefaultMaxSessions
	}
	maxPerSubject := options.MaxSessionsPerSubject
	if maxPerSubject <= 0 {
		maxPerSubject = DefaultMaxSessionsPerSubject
	}
	if maxPerSubject > maxSessions {
		return nil, errors.New("per-subject session limit exceeds global limit")
	}
	passwordVerifications := options.PasswordVerifications
	if passwordVerifications <= 0 {
		passwordVerifications = DefaultPasswordVerifications
	}
	service := &Service{
		provider:              provider,
		auditor:               auditor,
		now:                   now,
		random:                randomSource,
		sessions:              make(map[[32]byte]*session),
		maxSessions:           maxSessions,
		maxSessionsPerSubject: maxPerSubject,
		passwordVerifications: make(chan struct{}, passwordVerifications),
	}
	service.failures = newFailureLimiter(options)
	return service, nil
}

func (service *Service) Login(username string, password []byte, priorToken string) (string, Authentication, error) {
	now := service.now().UTC()
	key := principalFailureKey(username)
	if !service.failures.begin(key, now) {
		audit(service.auditor, now, "access.login_rate_limit", nil, "", "rejected")
		return "", Authentication{}, ErrRateLimited
	}
	select {
	case service.passwordVerifications <- struct{}{}:
		defer func() {
			<-service.passwordVerifications
		}()
	default:
		service.failures.cancel(key)
		audit(service.auditor, now, "access.login_rate_limit", nil, "", "rejected")
		return "", Authentication{}, ErrRateLimited
	}
	principal, accepted := service.provider.Authenticate(username, password)
	if !accepted {
		service.failures.finish(key, true)
		audit(service.auditor, now, "access.login_failure", nil, "", "rejected")
		return "", Authentication{}, ErrInvalidCredentials
	}
	service.failures.finish(key, false)
	if priorToken != "" {
		service.removeToken(priorToken)
	}
	token, authentication, err := service.create(principal, now)
	if err != nil {
		audit(service.auditor, now, "access.login_failure", &principal, "", "session_generation_failed")
		return "", Authentication{}, errors.New("session generation failed")
	}
	audit(service.auditor, now, "access.login_success", &principal, "", "accepted")
	return token, authentication, nil
}

func (service *Service) Authenticate(token string) (Authentication, error) {
	if token == "" {
		return Authentication{}, ErrUnauthenticated
	}
	digest := sha256.Sum256([]byte(token))
	now := service.now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	current, exists := service.sessions[digest]
	if !exists {
		return Authentication{}, ErrUnauthenticated
	}
	if result := expirationResult(current, now); result != "" {
		delete(service.sessions, digest)
		principal := current.principal
		audit(service.auditor, now, "access.session_expiry", &principal, "", result)
		return Authentication{}, ErrUnauthenticated
	}
	current.lastSeenAt = now
	return service.authentication(digest, current), nil
}

func (service *Service) VerifyCSRF(authentication Authentication, candidate string) bool {
	if candidate == "" || authentication.csrf == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(authentication.csrf)) == 1
}

func (service *Service) Logout(token string) bool {
	digest := sha256.Sum256([]byte(token))
	now := service.now().UTC()
	service.mu.Lock()
	current, exists := service.sessions[digest]
	if exists {
		delete(service.sessions, digest)
	}
	service.mu.Unlock()
	if exists {
		principal := current.principal
		audit(service.auditor, now, "access.logout", &principal, "", "revoked")
	}
	return exists
}

func (service *Service) ListSessions(actor Authentication) ([]SessionView, error) {
	if !HasPermission(actor.Session.Role, PermissionAccessSessionsRead) {
		principal := Principal{SubjectID: actor.Session.SubjectID, Role: actor.Session.Role}
		audit(service.auditor, service.now().UTC(), "access.authorization_denial", &principal, PermissionAccessSessionsRead, "denied")
		return nil, ErrForbidden
	}
	now := service.now().UTC()
	service.mu.Lock()
	service.pruneLocked(now)
	items := make([]SessionView, 0, len(service.sessions))
	for digest, current := range service.sessions {
		items = append(items, service.view(digest, current))
	}
	service.mu.Unlock()
	sort.Slice(items, func(left, right int) bool {
		if items[left].CreatedAt.Equal(items[right].CreatedAt) {
			return items[left].SessionRef < items[right].SessionRef
		}
		return items[left].CreatedAt.Before(items[right].CreatedAt)
	})
	principal := Principal{SubjectID: actor.Session.SubjectID, Role: actor.Session.Role}
	audit(service.auditor, now, "access.session_list", &principal, PermissionAccessSessionsRead, "accepted")
	return items, nil
}

func (service *Service) RevokeSession(actor Authentication, reference string) error {
	if !HasPermission(actor.Session.Role, PermissionAccessSessionsRevoke) {
		principal := Principal{SubjectID: actor.Session.SubjectID, Role: actor.Session.Role}
		audit(service.auditor, service.now().UTC(), "access.authorization_denial", &principal, PermissionAccessSessionsRevoke, "denied")
		return ErrForbidden
	}
	now := service.now().UTC()
	service.mu.Lock()
	service.pruneLocked(now)
	var found [32]byte
	exists := false
	for digest, current := range service.sessions {
		if current.reference == reference {
			found = digest
			exists = true
			break
		}
	}
	if exists {
		delete(service.sessions, found)
	}
	service.mu.Unlock()
	principal := Principal{SubjectID: actor.Session.SubjectID, Role: actor.Session.Role}
	if !exists {
		audit(service.auditor, now, "access.session_revoke", &principal, PermissionAccessSessionsRevoke, "not_found")
		return ErrSessionNotFound
	}
	audit(service.auditor, now, "access.session_revoke", &principal, PermissionAccessSessionsRevoke, "revoked")
	return nil
}

func (service *Service) CSRFToken(authentication Authentication) string {
	return authentication.csrf
}

func (service *Service) create(principal Principal, now time.Time) (string, Authentication, error) {
	for attempt := 0; attempt < 4; attempt++ {
		tokenBytes := make([]byte, 32)
		csrfBytes := make([]byte, 32)
		if _, err := io.ReadFull(service.random, tokenBytes); err != nil {
			return "", Authentication{}, err
		}
		if _, err := io.ReadFull(service.random, csrfBytes); err != nil {
			return "", Authentication{}, err
		}
		token := base64.RawURLEncoding.EncodeToString(tokenBytes)
		digest := sha256.Sum256([]byte(token))
		current := &session{
			principal:  principal,
			reference:  "xr_" + base64.RawURLEncoding.EncodeToString(digest[:16]),
			csrf:       base64.RawURLEncoding.EncodeToString(csrfBytes),
			createdAt:  now,
			lastSeenAt: now,
		}
		service.mu.Lock()
		if _, exists := service.sessions[digest]; exists {
			service.mu.Unlock()
			continue
		}
		service.pruneLocked(now)
		service.enforceCapacityLocked(principal.SubjectID)
		service.sessions[digest] = current
		authentication := service.authentication(digest, current)
		service.mu.Unlock()
		return token, authentication, nil
	}
	return "", Authentication{}, errors.New("session token collision")
}

func (service *Service) enforceCapacityLocked(subjectID string) {
	for service.countSubjectLocked(subjectID) >= service.maxSessionsPerSubject {
		digest, current, found := service.oldestLocked(subjectID)
		if !found {
			break
		}
		delete(service.sessions, digest)
		principal := current.principal
		audit(service.auditor, service.now().UTC(), "access.session_revoke", &principal, "", "per_subject_capacity")
	}
	for len(service.sessions) >= service.maxSessions {
		digest, current, found := service.oldestLocked("")
		if !found {
			break
		}
		delete(service.sessions, digest)
		principal := current.principal
		audit(service.auditor, service.now().UTC(), "access.session_revoke", &principal, "", "global_capacity")
	}
}

func (service *Service) pruneLocked(now time.Time) {
	for digest, current := range service.sessions {
		if result := expirationResult(current, now); result != "" {
			delete(service.sessions, digest)
			principal := current.principal
			audit(service.auditor, now, "access.session_expiry", &principal, "", result)
		}
	}
}

func (service *Service) removeToken(token string) {
	if token == "" {
		return
	}
	digest := sha256.Sum256([]byte(token))
	service.mu.Lock()
	delete(service.sessions, digest)
	service.mu.Unlock()
}

func (service *Service) countSubjectLocked(subjectID string) int {
	count := 0
	for _, current := range service.sessions {
		if current.principal.SubjectID == subjectID {
			count++
		}
	}
	return count
}

func (service *Service) oldestLocked(subjectID string) ([32]byte, *session, bool) {
	var selected [32]byte
	var oldest *session
	for digest, current := range service.sessions {
		if subjectID != "" && current.principal.SubjectID != subjectID {
			continue
		}
		if oldest == nil || current.createdAt.Before(oldest.createdAt) ||
			(current.createdAt.Equal(oldest.createdAt) && current.reference < oldest.reference) {
			selected = digest
			oldest = current
		}
	}
	return selected, oldest, oldest != nil
}

func (service *Service) authentication(digest [32]byte, current *session) Authentication {
	return Authentication{
		Session: service.view(digest, current),
		csrf:    current.csrf,
		digest:  digest,
	}
}

func (service *Service) view(_ [32]byte, current *session) SessionView {
	return SessionView{
		SessionRef:        current.reference,
		SubjectID:         current.principal.SubjectID,
		Role:              current.principal.Role,
		Permissions:       Permissions(current.principal.Role),
		CreatedAt:         current.createdAt,
		LastSeenAt:        current.lastSeenAt,
		IdleExpiresAt:     current.lastSeenAt.Add(SessionIdleTimeout),
		AbsoluteExpiresAt: current.createdAt.Add(SessionAbsoluteTimeout),
	}
}

func expirationResult(current *session, now time.Time) string {
	if !now.Before(current.createdAt.Add(SessionAbsoluteTimeout)) {
		return "absolute_timeout"
	}
	if !now.Before(current.lastSeenAt.Add(SessionIdleTimeout)) {
		return "idle_timeout"
	}
	return ""
}

// WithCurrentAuthentication orders publication/commit with local session
// revocation. The callback must be bounded and must not send a network response.
func (service *Service) WithCurrentAuthentication(auth Authentication, commit func() error) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	current, ok := service.sessions[auth.digest]
	if !ok || current.reference != auth.Session.SessionRef || current.principal.SubjectID != auth.Session.SubjectID || current.principal.Role != auth.Session.Role || expirationResult(current, service.now().UTC()) != "" {
		return ErrUnauthenticated
	}
	return commit()
}
