package access

import (
	"context"
	"errors"
)

func (service *Service) Authenticate(token string) (Authentication, error) {
	auth, e := service.authenticateMemory(token)
	if e != nil {
		return Authentication{}, e
	}
	if provider, ok := service.provider.(interface {
		CurrentPrincipal(string) (Principal, error)
	}); ok {
		p, err := provider.CurrentPrincipal(auth.Session.SubjectID)
		if errors.Is(err, ErrAccountUnavailable) {
			return Authentication{}, err
		}
		if err != nil || p.Role != auth.Session.Role || p.AuthRevision != auth.Session.AuthRevision {
			service.removeToken(token)
			return Authentication{}, ErrUnauthenticated
		}
	}
	return auth, nil
}
func (service *Service) Actor(auth Authentication) Principal {
	return Principal{SubjectID: auth.Session.SubjectID, Role: auth.Session.Role, AuthRevision: auth.Session.AuthRevision}
}
func (service *Service) SetManagedPassword(ctx context.Context, token string, password, confirmation []byte) (ManagedAccount, error) {
	p, ok := service.ManagedAccounts()
	if !ok {
		return ManagedAccount{}, ErrAccountUnavailable
	}
	key := principalFailureKey("account-ticket:" + TicketDigest(token))
	if !service.failures.begin(key, service.now().UTC()) {
		return ManagedAccount{}, ErrRateLimited
	}
	select {
	case service.passwordVerifications <- struct{}{}:
		defer func() { <-service.passwordVerifications }()
	default:
		service.failures.cancel(key)
		return ManagedAccount{}, ErrRateLimited
	}
	result, err := p.SetPassword(ctx, token, password, confirmation)
	if errors.Is(err, ErrAccountUnavailable) {
		service.failures.cancel(key)
	} else {
		service.failures.finish(key, err != nil)
	}
	return result, err
}
