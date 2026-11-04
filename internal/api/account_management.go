package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/foigent/x-insight/internal/access"
)

// All these requests are flat objects. Reject duplicate and unknown keys,
// trailing data and large bodies before any credential work; never log bodies.
func decodeAccountJSON(w http.ResponseWriter, r *http.Request, target any) error {
	if !requireJSONContentType(r) {
		return access.ErrAccountInvalid
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return access.ErrAccountInvalid
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, e := d.Token()
		if e != nil {
			return access.ErrAccountInvalid
		}
		name, ok := key.(string)
		if !ok {
			return access.ErrAccountInvalid
		}
		if _, ok = fields[name]; ok {
			return access.ErrAccountInvalid
		}
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			return access.ErrAccountInvalid
		}
		fields[name] = value
	}
	if _, e = d.Token(); e != nil {
		return access.ErrAccountInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return access.ErrAccountInvalid
	}
	raw, e := json.Marshal(fields)
	if e != nil {
		return e
	}
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	return strict.Decode(target)
}
func accountProblem(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, access.ErrForbidden):
		writeProblem(w, 403, "permission_denied", "Permission is denied", false)
	case errors.Is(e, access.ErrInvalidCredentials), errors.Is(e, access.ErrUnauthenticated):
		writeProblem(w, 401, "reauthentication_required", "Confirm your current password", false)
	case errors.Is(e, access.ErrRateLimited):
		writeProblem(w, 429, "authentication_rate_limited", "Authentication is rate limited", true)
	case errors.Is(e, access.ErrAccountConflict):
		writeProblem(w, 409, "account_revision_changed", "Account or authorization changed; refresh before retrying", false)
	case errors.Is(e, access.ErrAccountExists):
		writeProblem(w, 409, "account_username_exists", "The username already exists; use the existing account", false)
	case errors.Is(e, access.ErrAccountInvalid):
		writeProblem(w, 400, "invalid_account_operation", "Account operation or activation link is invalid or expired", false)
	default:
		writeProblem(w, 503, "access_unavailable", "Account authority is unavailable", true)
	}
}
func (s *Server) accountManager(w http.ResponseWriter, r *http.Request, mutation bool) (*access.DurableProvider, access.Authentication, bool) {
	auth, ok := accessAuthentication(r)
	if !ok || s.access == nil {
		writeProblem(w, 401, "authentication_required", "Authentication is required", false)
		return nil, auth, false
	}
	if !access.HasPermission(auth.Session.Role, access.PermissionAccountsManage) {
		accountProblem(w, access.ErrForbidden)
		return nil, auth, false
	}
	if mutation && (!s.transportSecurity.requireSameOriginHTTPS(r) || !s.access.VerifyCSRF(auth, r.Header.Get(csrfHeaderName))) {
		writeProblem(w, 403, "csrf_validation_failed", "Request authorization failed", false)
		return nil, auth, false
	}
	p, ok := s.access.ManagedAccounts()
	if !ok {
		accountProblem(w, access.ErrAccountUnavailable)
		return nil, auth, false
	}
	w.Header().Set("Cache-Control", "no-store")
	return p, auth, true
}
func (s *Server) handleManagedAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodGet, http.MethodPost)
		return
	}
	p, auth, ok := s.accountManager(w, r, r.Method == http.MethodPost)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		items, e := p.List(r.Context())
		if e != nil {
			accountProblem(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	var request struct {
		access.AccountChange
		Password string `json:"password"`
	}
	if decodeAccountJSON(w, r, &request) != nil {
		accountProblem(w, access.ErrAccountInvalid)
		return
	}
	password := []byte(request.Password)
	request.Password = ""
	defer clearBytes(password)
	if e := s.access.Reauthenticate(auth, password); e != nil {
		accountProblem(w, e)
		return
	}
	result, e := p.Change(r.Context(), s.access.Actor(auth), request.AccountChange)
	if e != nil {
		accountProblem(w, e)
		return
	}
	response := map[string]any{"account": result.Account, "expires_at": result.ExpiresAt}
	if result.Token != "" {
		response["activation_link"] = r.Header.Get("Origin") + "/access/activate#" + result.Token
	}
	writeJSON(w, 200, response)
}
func (s *Server) handleAccountPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !s.transportSecurity.requireSameOriginHTTPS(r) {
		writeProblem(w, 403, "csrf_validation_failed", "Request authorization failed", false)
		return
	}
	var request struct {
		Token        string `json:"token"`
		Password     string `json:"password"`
		Confirmation string `json:"confirmation"`
	}
	if decodeAccountJSON(w, r, &request) != nil || s.access == nil {
		accountProblem(w, access.ErrAccountInvalid)
		return
	}
	password, confirmation := []byte(request.Password), []byte(request.Confirmation)
	request.Password = ""
	request.Confirmation = ""
	defer clearBytes(password)
	defer clearBytes(confirmation)
	a, e := s.access.SetManagedPassword(r.Context(), request.Token, password, confirmation)
	request.Token = ""
	if e != nil {
		accountProblem(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"username": a.Username, "status": "password_set_sign_in_required"})
}
func (s *Server) handleProjectReadGrants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodGet, http.MethodPost)
		return
	}
	p, auth, ok := s.accountManager(w, r, r.Method == http.MethodPost)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		out, e := p.GrantOptions(r.Context(), s.access.Actor(auth))
		if e != nil {
			accountProblem(w, e)
			return
		}
		writeJSON(w, 200, out)
		return
	}
	var request struct {
		access.ProjectReadChange
		Password string `json:"password"`
	}
	if decodeAccountJSON(w, r, &request) != nil {
		accountProblem(w, access.ErrAccountInvalid)
		return
	}
	password := []byte(request.Password)
	request.Password = ""
	defer clearBytes(password)
	if !request.CheckOnly {
		if e := s.access.Reauthenticate(auth, password); e != nil {
			accountProblem(w, e)
			return
		}
	}
	out, e := p.Grant(r.Context(), s.access.Actor(auth), request.ProjectReadChange)
	if e != nil {
		accountProblem(w, e)
		return
	}
	writeJSON(w, 200, out)
}
