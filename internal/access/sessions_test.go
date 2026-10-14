package access

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testCredentialProvider struct {
	principals map[string]Principal
	password   string
}

func (provider testCredentialProvider) Authenticate(username string, password []byte) (Principal, bool) {
	principal, exists := provider.principals[NormalizeUsername(username)]
	return principal, exists && string(password) == provider.password
}

func TestSessionLifecycleRotationExpiryAndCSRF(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	var audit bytes.Buffer
	service := newTestService(t, &now, NewWriterAuditor(&audit), ServiceOptions{})

	token, authentication, err := service.Login("investigator.one", []byte("test password"), "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if len(token) != 43 || strings.Contains(audit.String(), token) ||
		authentication.Session.Role != RoleInvestigator ||
		!service.VerifyCSRF(authentication, service.CSRFToken(authentication)) ||
		service.VerifyCSRF(authentication, "wrong") {
		t.Fatalf("unsafe or invalid session token_len=%d session=%+v audit=%s", len(token), authentication.Session, audit.String())
	}
	if _, err := service.Authenticate(token); err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	replacement, _, err := service.Login("investigator.one", []byte("test password"), token)
	if err != nil {
		t.Fatalf("rotated login: %v", err)
	}
	if _, err := service.Authenticate(token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("prior session survived login rotation")
	}
	if _, err := service.Authenticate(replacement); err != nil {
		t.Fatalf("replacement session: %v", err)
	}

	now = now.Add(SessionIdleTimeout)
	if _, err := service.Authenticate(replacement); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("idle-expired session was accepted")
	}
	if !strings.Contains(audit.String(), `"event":"access.session_expiry"`) ||
		!strings.Contains(audit.String(), `"result":"idle_timeout"`) {
		t.Fatalf("missing expiry audit=%s", audit.String())
	}
}

func TestServiceReplacementInvalidatesEveryPreReplacementSession(t *testing.T) {
	now := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	before := newTestService(t, &now, DiscardAuditor{}, ServiceOptions{})
	token, _, err := before.Login("investigator.one", []byte("test password"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := before.Authenticate(token); err != nil {
		t.Fatal(err)
	}
	after := newTestService(t, &now, DiscardAuditor{}, ServiceOptions{})
	if _, err := after.Authenticate(token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("pre-replacement session survived service replacement: %v", err)
	}
}

func TestSessionAbsoluteTimeoutWinsAtExactBoundaryDespiteRecentActivity(t *testing.T) {
	createdAt := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	now := createdAt
	var audit bytes.Buffer
	service := newTestService(t, &now, NewWriterAuditor(&audit), ServiceOptions{})
	token, _, err := service.Login("investigator.one", []byte("test password"), "")
	if err != nil {
		t.Fatal(err)
	}
	for elapsed := 29 * time.Minute; elapsed < SessionAbsoluteTimeout; elapsed += 29 * time.Minute {
		now = createdAt.Add(elapsed)
		if _, err := service.Authenticate(token); err != nil {
			t.Fatalf("active session expired before absolute boundary at %s: %v", elapsed, err)
		}
	}
	now = createdAt.Add(SessionAbsoluteTimeout)
	if _, err := service.Authenticate(token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("session accepted at exact absolute boundary: %v", err)
	}
	if !strings.Contains(audit.String(), `"result":"absolute_timeout"`) {
		t.Fatalf("absolute timeout was not audited: %s", audit.String())
	}
}

func TestOperatorRBACListAndRevokeUsesOnlyNonSecretReference(t *testing.T) {
	now := time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC)
	service := newTestService(t, &now, DiscardAuditor{}, ServiceOptions{})
	investigatorToken, investigator, err := service.Login("investigator.one", []byte("test password"), "")
	if err != nil {
		t.Fatal(err)
	}
	_, operator, err := service.Login("operator.one", []byte("test password"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListSessions(investigator); !errors.Is(err, ErrForbidden) {
		t.Fatal("investigator listed sessions")
	}
	if err := service.RevokeSession(investigator, investigator.Session.SessionRef); !errors.Is(err, ErrForbidden) {
		t.Fatal("investigator revoked a session")
	}
	items, err := service.ListSessions(operator)
	if err != nil || len(items) != 2 {
		t.Fatalf("operator list count=%d err=%v", len(items), err)
	}
	if err := service.RevokeSession(operator, investigator.Session.SessionRef); err != nil {
		t.Fatalf("operator revoke: %v", err)
	}
	if _, err := service.Authenticate(investigatorToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("revoked session remained active")
	}
}

func TestSessionCapacityAndAuthenticationFailureRateLimitsAreBounded(t *testing.T) {
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	service := newTestService(t, &now, DiscardAuditor{}, ServiceOptions{
		MaxSessions:           2,
		MaxSessionsPerSubject: 1,
		PrincipalFailureLimit: 2,
		GlobalFailureLimit:    10,
	})
	first, _, err := service.Login("investigator.one", []byte("test password"), "")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := service.Login("investigator.one", []byte("test password"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(first); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("oldest per-subject session was not evicted")
	}
	if _, err := service.Authenticate(second); err != nil {
		t.Fatal("newest session was not retained")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, _, err := service.Login("unknown.user", []byte("wrong"), ""); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("failure %d err=%v", attempt, err)
		}
	}
	if _, _, err := service.Login("unknown.user", []byte("wrong"), ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate-limited attempt err=%v", err)
	}
}

type blockingCredentialProvider struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
	active  atomic.Int32
	maximum atomic.Int32
}

func (provider *blockingCredentialProvider) Authenticate(string, []byte) (Principal, bool) {
	provider.calls.Add(1)
	active := provider.active.Add(1)
	for {
		previous := provider.maximum.Load()
		if active <= previous || provider.maximum.CompareAndSwap(previous, active) {
			break
		}
	}
	provider.entered <- struct{}{}
	<-provider.release
	provider.active.Add(-1)
	return Principal{}, false
}

func TestConcurrentLoginAdmissionCannotCrossPrincipalFailureLimit(t *testing.T) {
	now := time.Date(2026, 7, 29, 10, 30, 0, 0, time.UTC)
	provider := &blockingCredentialProvider{
		entered: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	service, err := NewService(provider, DiscardAuditor{}, ServiceOptions{
		Now:                   func() time.Time { return now },
		PrincipalFailureLimit: 2,
		GlobalFailureLimit:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	wait.Add(2)
	for attempt := 0; attempt < 2; attempt++ {
		go func() {
			defer wait.Done()
			_, _, _ = service.Login("same.principal", []byte("wrong"), "")
		}()
	}
	<-provider.entered
	<-provider.entered
	if _, _, err := service.Login("same.principal", []byte("wrong"), ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("concurrent threshold crossing err=%v", err)
	}
	close(provider.release)
	wait.Wait()
	if calls := provider.calls.Load(); calls != 2 {
		t.Fatalf("provider calls=%d want=2", calls)
	}
}

func TestConcurrentPasswordVerificationAdmissionIsIndependentlyBounded(t *testing.T) {
	now := time.Date(2026, 7, 29, 10, 45, 0, 0, time.UTC)
	provider := &blockingCredentialProvider{
		entered: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	service, err := NewService(provider, DiscardAuditor{}, ServiceOptions{
		Now:                   func() time.Time { return now },
		PrincipalFailureLimit: 10,
		GlobalFailureLimit:    10,
		PasswordVerifications: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	wait.Add(2)
	for attempt := 0; attempt < 2; attempt++ {
		go func(index int) {
			defer wait.Done()
			_, _, _ = service.Login(
				"principal."+string(rune('a'+index)),
				[]byte("wrong"),
				"",
			)
		}(attempt)
	}
	<-provider.entered
	<-provider.entered
	if _, _, err := service.Login("principal.c", []byte("wrong"), ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("password verification capacity crossing err=%v", err)
	}
	if calls := provider.calls.Load(); calls != 2 {
		t.Fatalf("provider calls=%d want=2 before release", calls)
	}
	close(provider.release)
	wait.Wait()
	if maximum := provider.maximum.Load(); maximum != 2 {
		t.Fatalf("maximum concurrent password verifications=%d want=2", maximum)
	}
}

func newTestService(t *testing.T, now *time.Time, auditor Auditor, overrides ServiceOptions) *Service {
	t.Helper()
	randomBytes := make([]byte, 2048)
	for index := range randomBytes {
		randomBytes[index] = byte(index)
	}
	overrides.Now = func() time.Time { return *now }
	overrides.Random = bytes.NewReader(randomBytes)
	service, err := NewService(testCredentialProvider{
		principals: map[string]Principal{
			"investigator.one": {SubjectID: "xs_0123456789abcdef", Role: RoleInvestigator},
			"operator.one":     {SubjectID: "xs_fedcba9876543210", Role: RoleOperator},
		},
		password: "test password",
	}, auditor, overrides)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestCurrentAuthenticationOrdersCommitWithRevocation(t *testing.T) {
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	service := newTestService(t, &now, DiscardAuditor{}, ServiceOptions{})
	token, auth, err := service.Login("investigator.one", []byte("test password"), "")
	if err != nil {
		t.Fatal(err)
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- service.WithCurrentAuthentication(auth, func() error { close(entered); <-release; return nil })
	}()
	<-entered
	revoked := make(chan error, 1)
	go func() {
		if service.Logout(token) {
			revoked <- nil
		} else {
			revoked <- errors.New("logout failed")
		}
	}()
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = <-revoked; err != nil {
		t.Fatal(err)
	}
	called := false
	if err = service.WithCurrentAuthentication(auth, func() error { called = true; return nil }); !errors.Is(err, ErrUnauthenticated) || called {
		t.Fatal("revoked session reached commit", err)
	}
	_, auth, err = service.Login("investigator.one", []byte("test password"), "")
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("business failed")
	if err = service.WithCurrentAuthentication(auth, func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err = service.WithCurrentAuthentication(auth, func() error { return nil }); err != nil {
		t.Fatal("error retained session mutex", err)
	}
	now = now.Add(SessionIdleTimeout)
	if err = service.WithCurrentAuthentication(auth, func() error { called = true; return nil }); !errors.Is(err, ErrUnauthenticated) || called {
		t.Fatal("expired session reached commit", err)
	}
}
