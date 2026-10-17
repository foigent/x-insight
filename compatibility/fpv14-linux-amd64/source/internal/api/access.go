package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/workspace"
)

const (
	maxLoginBodyBytes = 4 << 10
	csrfHeaderName    = "X-CSRF-Token"
)

var sessionReferencePattern = regexp.MustCompile(`^xr_[A-Za-z0-9_-]{22}$`)
var publicAssetPathPattern = regexp.MustCompile(`^/assets/[A-Za-z0-9][A-Za-z0-9._-]*$`)

type accessContextKey struct{}
type serviceAccessContextKey struct{}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type sessionResponse struct {
	ScopeRevision string                    `json:"scope_revision,omitempty"`
	Session       productaccess.SessionView `json:"session"`
	CSRFToken     string                    `json:"csrf_token"`
}

type sessionListResponse struct {
	Items []productaccess.SessionView `json:"items"`
}

func (s *Server) handleAccessLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	if s.access == nil {
		writeProblem(w, http.StatusServiceUnavailable, "access_unavailable", "Access service is unavailable", true)
		return
	}
	if !s.transportSecurity.requireSameOriginHTTPS(r) || !requireJSONContentType(r) {
		writeProblem(w, http.StatusForbidden, "same_origin_required", "Same-origin HTTPS is required", false)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBodyBytes)
	decoder := json.NewDecoder(r.Body)
	request, err := decodeLoginRequest(decoder)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_authentication_request", "Authentication request is invalid", false)
		return
	}
	password := []byte(request.Password)
	request.Password = ""
	defer clearBytes(password)
	priorToken, duplicateCookie := sessionCookie(r)
	if duplicateCookie {
		writeProblem(w, http.StatusBadRequest, "invalid_authentication_request", "Authentication request is invalid", false)
		return
	}
	token, authentication, err := s.access.Login(request.Username, password, priorToken)
	switch {
	case errors.Is(err, productaccess.ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		writeProblem(w, http.StatusTooManyRequests, "authentication_rate_limited", "Authentication failed", true)
		return
	case errors.Is(err, productaccess.ErrInvalidCredentials):
		writeProblem(w, http.StatusUnauthorized, "authentication_failed", "Authentication failed", false)
		return
	case err != nil:
		writeProblem(w, http.StatusServiceUnavailable, "access_unavailable", "Access service is unavailable", true)
		return
	}
	setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, sessionResponse{
		Session:   authentication.Session,
		CSRFToken: s.access.CSRFToken(authentication),
	})
}

func (s *Server) handleAccessSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	authentication, ok := accessAuthentication(r)
	if !ok || s.access == nil {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	revision := ""
	if productaccess.IsScopedRole(authentication.Session.Role) ||
		(authentication.Session.Role == productaccess.RoleWorkbenchReader && s.scopedStore != nil) {
		if s.scopedStore == nil {
			writeProblem(w, http.StatusServiceUnavailable, "scoped_workchain_unavailable", "Scoped workchain is unavailable", false)
			return
		}
		var err error
		revision, err = s.scopedStore.CurrentRevision(r.Context(), authentication.Session.SubjectID, authentication.Session.Role == productaccess.RoleScopedReviewer)
		if err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "scoped_workchain_unavailable", "Scoped workchain is unavailable", false)
			return
		}
		// A legacy reader with no grants keeps the existing empty session view.
		// A reader with grants needs this same workchain revision for independent
		// review-body reads and mid-stream revocation checks.
		if authentication.Session.Role == productaccess.RoleWorkbenchReader && revision == "no_authorized_scope" {
			revision = ""
		}
	}
	writeJSON(w, http.StatusOK, sessionResponse{ScopeRevision: revision,
		Session:   authentication.Session,
		CSRFToken: s.access.CSRFToken(authentication),
	})
}

func (s *Server) handleAccessProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	writeJSON(w, http.StatusOK, productaccess.ProviderCapabilities())
}

func (s *Server) handleAccessLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	authentication, ok := accessAuthentication(r)
	if !ok || s.access == nil {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	if !s.transportSecurity.requireSameOriginHTTPS(r) || !s.access.VerifyCSRF(authentication, r.Header.Get(csrfHeaderName)) {
		writeProblem(w, http.StatusForbidden, "csrf_validation_failed", "Request authorization failed", false)
		return
	}
	token, duplicate := sessionCookie(r)
	if duplicate {
		writeProblem(w, http.StatusForbidden, "csrf_validation_failed", "Request authorization failed", false)
		return
	}
	s.access.Logout(token)
	clearSessionCookie(w)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAccessSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	authentication, ok := accessAuthentication(r)
	if !ok || s.access == nil {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	items, err := s.access.ListSessions(authentication)
	if errors.Is(err, productaccess.ErrForbidden) {
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return
	}
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "access_unavailable", "Access service is unavailable", true)
		return
	}
	if items == nil {
		items = make([]productaccess.SessionView, 0)
	}
	writeJSON(w, http.StatusOK, sessionListResponse{Items: items})
}

func (s *Server) handleAccessSessionPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	authentication, ok := accessAuthentication(r)
	if !ok || s.access == nil {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	suffix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/access/sessions/"), "/")
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 || parts[1] != "revoke" || !sessionReferencePattern.MatchString(parts[0]) {
		writeProblem(w, http.StatusNotFound, "session_not_found", "Session not found", false)
		return
	}
	if !s.transportSecurity.requireSameOriginHTTPS(r) || !s.access.VerifyCSRF(authentication, r.Header.Get(csrfHeaderName)) {
		writeProblem(w, http.StatusForbidden, "csrf_validation_failed", "Request authorization failed", false)
		return
	}
	err := s.access.RevokeSession(authentication, parts[0])
	switch {
	case errors.Is(err, productaccess.ErrForbidden):
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return
	case errors.Is(err, productaccess.ErrSessionNotFound):
		writeProblem(w, http.StatusNotFound, "session_not_found", "Session not found", false)
		return
	case err != nil:
		writeProblem(w, http.StatusServiceUnavailable, "access_unavailable", "Access service is unavailable", true)
		return
	}
	if authentication.Session.SessionRef == parts[0] {
		clearSessionCookie(w)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) withAccessEnforcement(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicAccessPath(r.URL) {
			next.ServeHTTP(w, r)
			return
		}
		authorization := r.Header.Values("Authorization")
		if len(authorization) > 0 {
			s.withServiceIdentity(w, r, next, authorization)
			return
		}
		token, duplicate := sessionCookie(r)
		if s.access == nil || duplicate {
			s.denyUnauthenticated(w, r)
			return
		}
		authentication, err := s.access.Authenticate(token)
		if err != nil {
			s.denyUnauthenticated(w, r)
			return
		}
		// Workbench-only readers keep their existing route surface. Their
		// workbench requests now share the grant lock with purpose revocation;
		// this does not admit them to execution or Case endpoints.
		budgetPath := strings.HasPrefix(r.URL.Path, "/api/v1/workbench/budgets/")
		if (strings.HasPrefix(r.URL.Path, "/api/v1/runtime/") || strings.HasPrefix(r.URL.Path, "/api/v1/work-activity") || budgetPath) && !productaccess.HasPermission(authentication.Session.Role, productaccess.PermissionWorkbenchDetails) {
			writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
			return
		}
		if productaccess.IsScopedRole(authentication.Session.Role) || strings.HasPrefix(r.URL.Path, "/api/v1/runtime/") || strings.HasPrefix(r.URL.Path, "/api/v1/work-activity") || budgetPath ||
			(authentication.Session.Role == productaccess.RoleWorkbenchReader && s.scopedStore != nil && strings.HasPrefix(r.URL.Path, "/api/v1/workbench/")) {
			ctx := context.WithValue(r.Context(), accessContextKey{}, authentication)
			s.handleScopedWorkchain(w, r.WithContext(ctx), authentication, next)
			return
		}
		if r.URL.Path == "/api/v1/posture" ||
			strings.HasPrefix(r.URL.Path, "/api/v1/executions") ||
			r.URL.Path == "/api/v1/sources" ||
			strings.HasPrefix(r.URL.Path, "/api/v1/connectors") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/sessions") ||
			r.URL.Path == "/api/v1/compare" ||
			strings.HasPrefix(r.URL.Path, "/api/v1/alerts") ||
			r.URL.Path == "/api/v1/discovery/hypotheses" ||
			strings.HasPrefix(r.URL.Path, "/api/v1/cases") ||
			r.URL.Path == "/api/v1/saved-views" {
			if !productaccess.HasPermission(authentication.Session.Role, productaccess.PermissionEvidenceRead) {
				writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/workbench/") &&
			!productaccess.HasPermission(authentication.Session.Role, productaccess.PermissionWorkbenchSummary) {
			writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
			return
		}
		ctx := context.WithValue(r.Context(), accessContextKey{}, authentication)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) withServiceIdentity(
	w http.ResponseWriter,
	r *http.Request,
	next http.Handler,
	authorization []string,
) {
	if len(authorization) != 1 || len(r.CookiesNamed(productaccess.SessionCookieName)) != 0 {
		writeProblem(w, http.StatusUnauthorized, "agent_authentication_failed", "Agent authentication failed", false)
		return
	}
	permission, allowed := serviceIdentityRoutePermission(r.Method, r.URL.Path)
	if s.agentAccessStore == nil || !allowed {
		writeProblem(w, http.StatusForbidden, "agent_scope_denied", "Agent scope does not permit this route", false)
		return
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization[0], prefix) ||
		strings.TrimSpace(authorization[0]) != authorization[0] {
		writeProblem(w, http.StatusUnauthorized, "agent_authentication_failed", "Agent authentication failed", false)
		return
	}
	token := strings.TrimPrefix(authorization[0], prefix)
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		writeProblem(w, http.StatusUnauthorized, "agent_authentication_failed", "Agent authentication failed", false)
		return
	}
	authentication, err := s.agentAccessStore.AuthenticateServiceToken(r.Context(), token, s.now().UTC())
	token = ""
	switch {
	case errors.Is(err, workspace.ErrCredentialExpired):
		writeProblem(w, http.StatusUnauthorized, "agent_credential_expired", "Agent credential expired", false)
		return
	case errors.Is(err, workspace.ErrCredentialRevoked):
		writeProblem(w, http.StatusUnauthorized, "agent_credential_revoked", "Agent credential revoked", false)
		return
	case errors.Is(err, workspace.ErrCredentialInvalid):
		writeProblem(w, http.StatusUnauthorized, "agent_authentication_failed", "Agent authentication failed", false)
		return
	case err != nil:
		writeProblem(w, http.StatusServiceUnavailable, "agent_access_unavailable", "Agent access service is unavailable", true)
		return
	}
	if !containsString(authentication.Identity.Permissions, permission) {
		writeProblem(w, http.StatusForbidden, "agent_scope_denied", "Agent scope does not permit this route", false)
		return
	}
	if !s.serviceRateLimiter(authentication.Identity.ID, permission).allow(s.now().UTC()) {
		w.Header().Set("Retry-After", "1")
		writeProblem(w, http.StatusTooManyRequests, "agent_rate_limited", "Agent request rate is limited", true)
		return
	}
	ctx := context.WithValue(r.Context(), serviceAccessContextKey{}, authentication)
	next.ServeHTTP(w, r.WithContext(ctx))
}

func serviceIdentityReadPath(path string) bool {
	return path == "/api/v1/executions" ||
		strings.HasPrefix(path, "/api/v1/executions/") ||
		path == "/api/v1/sources"
}

func serviceIdentityRoutePermission(method string, requestPath string) (string, bool) {
	if (method == http.MethodGet && requestPath == "/api/v1/work-event-ingest/config") ||
		(method == http.MethodPost && (requestPath == "/api/v1/work-event-ingest/items" ||
			requestPath == "/api/v1/work-event-ingest/contents" || requestPath == "/api/v1/work-event-install/renew" ||
			requestPath == "/api/v1/work-event-install/future-candidates" ||
			requestPath == "/api/v1/work-event-install/status")) {
		return "work_event.ingest", true
	}
	if method == http.MethodGet && serviceIdentityReadPath(requestPath) {
		return string(productaccess.PermissionEvidenceRead), true
	}
	if method == http.MethodGet && strings.HasPrefix(requestPath, "/api/v1/connector-control/installations/") {
		parts := strings.Split(strings.TrimPrefix(requestPath, "/api/v1/connector-control/installations/"), "/")
		if len(parts) == 2 && parts[1] == "setup" && connectorcontrol.IsInstallationID(parts[0]) {
			return connectorcontrol.PermissionControlPoll, true
		}
	}
	if method == http.MethodGet && requestPath == "/api/v1/connector-control/operations" {
		return connectorcontrol.PermissionControlPoll, true
	}
	if method == http.MethodPost && strings.HasPrefix(requestPath, "/api/v1/connector-control/operations/") {
		suffix := strings.TrimPrefix(requestPath, "/api/v1/connector-control/operations/")
		if connectorcontrol.IsOperationID(suffix) && !strings.Contains(suffix, "/") {
			return connectorcontrol.PermissionControlAck, true
		}
	}
	return "", false
}

func (s *Server) serviceRateLimiter(identityID, permission string) *tokenBucket {
	s.serviceLimiterMu.Lock()
	defer s.serviceLimiterMu.Unlock()
	limiter := s.serviceLimiters[identityID]
	if limiter == nil {
		const maximumCachedServiceLimiters = 1024
		if len(s.serviceLimiters) >= maximumCachedServiceLimiters && len(s.serviceLimiterOrder) > 0 {
			delete(s.serviceLimiters, s.serviceLimiterOrder[0])
			s.serviceLimiterOrder = s.serviceLimiterOrder[1:]
		}
		if permission == "work_event.ingest" {
			limiter = newTokenBucket(20, 20)
		} else {
			limiter = newTokenBucket(5, 10)
		}
		s.serviceLimiters[identityID] = limiter
		s.serviceLimiterOrder = append(s.serviceLimiterOrder, identityID)
	}
	return limiter
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Server) denyUnauthenticated(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	target := "/login"
	if r.URL.Path == "/connectors/unirouter/setup" {
		returnTo := "/connectors/unirouter/setup"
		if id := r.URL.Query().Get("installation"); connectorcontrol.IsInstallationID(id) {
			returnTo += "?installation=" + id
		}
		target += "?return_to=" + url.QueryEscape(returnTo)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func isPublicAccessPath(target *url.URL) bool {
	requestPath := target.Path
	if requestPath == "/healthz" ||
		requestPath == "/readyz" ||
		requestPath == "/login" ||
		requestPath == "/login/" ||
		requestPath == "/api/v1/access/login" ||
		requestPath == "/api/v1/access/providers" {
		return true
	}
	if requestPath == "/api/v1/connector-setups/resolve" || requestPath == "/api/v1/connector-control/invitations/redeem" ||
		requestPath == "/api/v1/work-event-install/candidates" ||
		requestPath == "/api/v1/work-event-install/pickup" {
		return true
	}
	return publicAssetPathPattern.MatchString(requestPath) &&
		path.Clean(requestPath) == requestPath &&
		target.EscapedPath() == requestPath
}

func accessAuthentication(r *http.Request) (productaccess.Authentication, bool) {
	authentication, ok := r.Context().Value(accessContextKey{}).(productaccess.Authentication)
	return authentication, ok
}

func serviceAuthentication(r *http.Request) (workspace.ServiceAuthentication, bool) {
	authentication, ok := r.Context().Value(serviceAccessContextKey{}).(workspace.ServiceAuthentication)
	return authentication, ok
}

func sessionCookie(r *http.Request) (string, bool) {
	cookies := r.CookiesNamed(productaccess.SessionCookieName)
	if len(cookies) == 0 {
		return "", false
	}
	if len(cookies) != 1 || cookies[0].Value == "" {
		return "", true
	}
	return cookies[0].Value, false
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     productaccess.SessionCookieName,
		Value:    token,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     productaccess.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func requireJSONContentType(r *http.Request) bool {
	values := r.Header.Values("Content-Type")
	if len(values) != 1 {
		return false
	}
	mediaType, parameters, err := mime.ParseMediaType(values[0])
	return err == nil && mediaType == "application/json" && len(parameters) == 0
}

func rejectTrailingJSONBody(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == nil {
		return errors.New("trailing JSON")
	}
	if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func decodeLoginRequest(decoder *json.Decoder) (loginRequest, error) {
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return loginRequest{}, errors.New("login request must be an object")
	}
	var request loginRequest
	seen := make(map[string]struct{}, 2)
	for decoder.More() {
		rawName, err := decoder.Token()
		name, ok := rawName.(string)
		if err != nil || !ok {
			return loginRequest{}, errors.New("login request field is invalid")
		}
		if _, exists := seen[name]; exists {
			return loginRequest{}, errors.New("login request contains a duplicate field")
		}
		seen[name] = struct{}{}
		switch name {
		case "username":
			err = decoder.Decode(&request.Username)
		case "password":
			err = decoder.Decode(&request.Password)
		default:
			return loginRequest{}, errors.New("login request contains an unknown field")
		}
		if err != nil {
			return loginRequest{}, errors.New("login request field value is invalid")
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return loginRequest{}, errors.New("login request is not terminated")
	}
	if _, present := seen["username"]; !present {
		return loginRequest{}, errors.New("login username is required")
	}
	if _, present := seen["password"]; !present {
		return loginRequest{}, errors.New("login password is required")
	}
	if err := rejectTrailingJSONBody(decoder); err != nil {
		return loginRequest{}, err
	}
	return request, nil
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
