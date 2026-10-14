package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
)

type scopedRoute struct {
	handler         func(http.ResponseWriter, *http.Request)
	write           bool
	purposeWrite    bool
	budgetRead      bool
	contentBytes    bool
	reviewBodyBytes bool
}

// Legacy capability checks remain unchanged. The new role is accepted only
// after this request has entered the scoped route and grant boundary.
func canWriteInvestigation(r *http.Request) bool {
	auth, ok := accessAuthentication(r)
	if !ok {
		return false
	}
	if productaccess.HasPermission(auth.Session.Role, productaccess.PermissionInvestigationWrite) {
		return true
	}
	a := workchain.FromContext(r.Context())
	return auth.Session.Role == productaccess.RoleScopedReviewer && a != nil && a.Reviewer && a.Subject == auth.Session.SubjectID
}

// This is an explicit method/path boundary. A future legacy route does not
// become scoped merely because a role has a similarly named permission.
func (s *Server) scopedRoute(r *http.Request) (scopedRoute, bool) {
	path, method := r.URL.Path, r.Method
	if r.URL.RawPath != "" && !canonicalWorkActivityRawPath(r.URL) {
		return scopedRoute{}, false
	}
	get, post := method == http.MethodGet, method == http.MethodPost
	choose := func(h func(http.ResponseWriter, *http.Request), allowed, write bool) (scopedRoute, bool) {
		return scopedRoute{handler: h, write: write}, allowed
	}
	switch path {
	case "/api/v1/executions":
		return choose(s.handleExecutionCollection, get, false)
	case "/api/v1/sources":
		return choose(s.handleSourceCollection, get, false)
	case "/api/v1/compare":
		return choose(s.handleExecutionCompare, get, false)
	case "/api/v1/cases":
		return choose(s.handleCaseCollection, get, false)
	case "/api/v1/investigation/queue":
		return choose(s.handleInvestigationQueue, get, false)
	case "/api/v1/investigation/queue-filters":
		return choose(s.handleQueueFilters, get || post, false)
	case "/api/v1/security/reviews":
		return choose(s.handleReviewCreation, post, true)
	case "/api/v1/security/reviews/preview":
		return choose(s.handleReviewCreation, post, false)
	case "/api/v1/security/assignee-candidates":
		return choose(s.handleAssigneeCandidates, get, true)
	case "/api/v1/workbench/readings":
		return choose(s.handleWorkbenchReadings, post, false)
	case "/api/v1/workbench/budgets/readings":
		return scopedRoute{handler: s.handleBudgetReadings, budgetRead: true}, post
	case "/api/v1/runtime/readings":
		return choose(s.handleRuntimeReadings, post, false)
	case "/api/v1/runtime/current":
		return choose(s.handleRuntimeCurrent, get, false)
	case "/api/v1/work-activity":
		return choose(s.handleWorkActivity, get, false)
	case "/api/v1/work-activity/scopes":
		return choose(s.handleWorkActivity, get, false)
	case "/api/v1/work-activity/summary":
		return choose(s.handleWorkActivity, get, false)
	case "/api/v1/workbench/saved-filters":
		return choose(s.handleWorkbenchSavedFilters, get || post, false)
	}
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/"), "/")
	if !strings.HasPrefix(path, "/api/v1/") {
		return scopedRoute{}, false
	}
	if len(parts) >= 4 && parts[0] == "workbench" && parts[1] == "budgets" && parts[2] == "readings" && workbench.ValidBudgetReadingID(parts[3]) {
		return scopedRoute{handler: s.handleBudgetReadingPath, budgetRead: true}, get && (len(parts) == 4 || len(parts) == 5 && parts[4] == "items" || len(parts) == 7 && parts[4] == "items" && workbench.ValidBudgetItemRef(parts[5]) && parts[6] == "history")
	}
	if len(parts) == 7 && parts[0] == "work-activity" && parts[1] == "events" && workActivityDecodedRef.MatchString(parts[6]) {
		return choose(s.handleWorkActivity, get, false)
	}
	if len(parts) == 3 && parts[0] == "work-activity" && parts[1] == "contents" && workActivityDecodedRef.MatchString(parts[2]) {
		return scopedRoute{handler: s.handleReviewBodyBytes, reviewBodyBytes: true}, get
	}
	if len(parts) >= 3 && parts[0] == "runtime" && parts[1] == "readings" && workbench.ValidRuntimeReadingID(parts[2]) {
		if len(parts) == 3 {
			return choose(s.handleRuntimeReadingPath, get, false)
		}
		if len(parts) == 5 && parts[3] == "executions" && executionid.Valid(parts[4]) {
			return choose(s.handleRuntimeReadingPath, get, false)
		}
		if len(parts) == 4 {
			_, collection := workbench.RuntimeCollectionProfile(parts[3])
			return choose(s.handleRuntimeReadingPath, get && (collection || parts[3] == "requests" || parts[3] == "sources" || parts[3] == "measurements" || parts[3] == "summary"), false)
		}
		if len(parts) == 7 && parts[3] == "requests" && workbench.ValidRuntimeRequestRef(parts[4]) && parts[5] == "contents" {
			return scopedRoute{handler: s.handleRuntimeContentBytes, contentBytes: true}, get
		}
		if len(parts) == 6 && parts[3] == "requests" && workbench.ValidRuntimeRequestRef(parts[4]) && parts[5] == "contents" {
			return choose(s.handleRuntimeContents, get, false)
		}
		if len(parts) == 6 && parts[3] == "requests" && workbench.ValidRuntimeRequestRef(parts[4]) && (parts[5] == "associations" || parts[5] == "resources" || parts[5] == "action-witnesses") {
			return choose(s.handleRuntimeReadingPath, get, false)
		}
		if len(parts) == 5 && ((parts[3] == "requests" && workbench.ValidRuntimeRequestRef(parts[4])) || (parts[3] == "dimensions" && workbench.ValidRuntimeDimension(parts[4]))) {
			return choose(s.handleRuntimeReadingPath, get, false)
		}
		return scopedRoute{}, false
	}
	if len(parts) == 4 && parts[0] == "workbench" && parts[1] == "executions" && executionid.Valid(parts[2]) && parts[3] == "purpose" {
		return scopedRoute{handler: s.handleWorkbenchPurpose, purposeWrite: post}, get || post
	}
	if len(parts) >= 2 && parts[0] == "executions" && executionid.Valid(parts[1]) {
		if len(parts) == 2 {
			return choose(s.handleExecutionPath, get, false)
		}
		if len(parts) == 3 {
			switch parts[2] {
			case "timeline", "investigation", "gaps", "trace-journey":
				return choose(s.handleExecutionPath, get, false)
			}
		}
		if len(parts) == 4 && parts[2] == "events" && uuidPattern.MatchString(parts[3]) {
			return choose(s.handleExecutionPath, get, false)
		}
	}
	if len(parts) >= 2 && parts[0] == "cases" && caseIDPattern.MatchString(parts[1]) {
		if len(parts) == 2 {
			return choose(s.handleCasePath, get, false)
		}
		if len(parts) == 3 {
			switch parts[2] {
			case "executions", "findings", "evidence", "gaps", "annotations":
				return choose(s.handleCasePath, post, true)
			}
		}
		if len(parts) >= 4 && parts[2] == "findings" && findingIDPattern.MatchString(parts[3]) {
			tail := strings.Join(parts[4:], "/")
			switch tail {
			case "review", "review/history", "assessment", "assessment/history":
				return choose(s.handleCasePath, get, false)
			case "assessment/preview", "continuations/preview":
				return choose(s.handleCasePath, post, false)
			case "review-actions", "assessment-actions", "continuations":
				return choose(s.handleCasePath, post, true)
			}
		}
	}
	if len(parts) == 3 && parts[0] == "investigation" && parts[1] == "queue-filters" && parts[2] != "" {
		return choose(s.handleQueueFilters, method == http.MethodPut || method == http.MethodDelete, false)
	}
	if len(parts) >= 3 && parts[0] == "workbench" {
		if parts[1] == "saved-filters" && len(parts) == 3 && parts[2] != "" {
			return choose(s.handleWorkbenchSavedFilterPath, method == http.MethodPut || method == http.MethodDelete, false)
		}
		if parts[1] == "readings" && strings.HasPrefix(parts[2], "xwr_") {
			if len(parts) == 3 {
				return choose(s.handleWorkbenchReadingPath, get, false)
			}
			if len(parts) == 4 {
				switch parts[3] {
				case "summary", "contributions", "series", "details", "recent-requests", "sources", "source-facts":
					return choose(s.handleWorkbenchReadingPath, get, false)
				}
			}
		}
	}
	return scopedRoute{}, false
}

// Browsers percent-encode the colon in source event/body refs. Permit only
// the exact canonical escaping of these two scoped routes; encoded slashes,
// dot segments, alternate spellings and every other RawPath stay denied.
func canonicalWorkActivityRawPath(u *url.URL) bool {
	if u == nil || u.RawPath == "" ||
		!strings.HasPrefix(u.Path, "/api/v1/work-activity/events/") &&
			!strings.HasPrefix(u.Path, "/api/v1/work-activity/contents/") {
		return false
	}
	parts := strings.Split(u.Path, "/")
	if len(parts) == 0 || !workActivityDecodedRef.MatchString(parts[len(parts)-1]) {
		return false
	}
	for i := range parts {
		parts[i] = url.QueryEscape(parts[i])
	}
	return u.RawPath == strings.Join(parts, "/")
}

var workActivityDecodedRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// One bounded response is constructed while the grant authorization is held.
// It is published only after the authorization transaction and session check
// finish, so no database lock is held on client network I/O.
type scopedResponse struct {
	headers           http.Header
	status            int
	body              bytes.Buffer
	err               error
	needsContentProof bool
}

func (w *scopedResponse) requireContentLocationVerification() { w.needsContentProof = true }

type contentProofAttemptKey struct{}

const maxScopedResponseBytes = 16 << 20

func (w *scopedResponse) Header() http.Header { return w.headers }
func (w *scopedResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *scopedResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.body.Len()+len(p) > maxScopedResponseBytes {
		w.err = errors.New("scoped response is too large")
		return 0, w.err
	}
	return w.body.Write(p)
}

func (s *Server) handleScopedWorkchain(w http.ResponseWriter, r *http.Request, auth productaccess.Authentication, next http.Handler) {
	// Own-session operations and static resources must still work with no grants.
	if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/v1/access/session" || r.URL.Path == "/api/v1/access/logout" {
		next.ServeHTTP(w, r)
		return
	}
	if s.scopedStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "scoped_workchain_unavailable", "Scoped workchain is unavailable", false)
		return
	}
	localServer := &Server{store: s.scopedStore.Evidence(), workspaceStore: s.workspaceStore, workbenchStore: s.workbenchStore, access: s.access, transportSecurity: s.transportSecurity, now: s.now}
	route, allowed := localServer.scopedRoute(r)
	reviewer := auth.Session.Role == productaccess.RoleScopedReviewer
	if !allowed || (route.write && !reviewer) || (route.purposeWrite && !productaccess.HasPermission(auth.Session.Role, productaccess.PermissionPurposeReview)) {
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return
	}
	if route.contentBytes {
		s.handleRuntimeContentBytes(w, r)
		return
	}
	if route.reviewBodyBytes {
		s.handleReviewBodyBytes(w, r)
		return
	}
	// Keep the exact bounded command for a second pass only when the store
	// reports a new adoption. A successful receipt retry never reads source bytes.
	var commandBody []byte
	if r.Method == http.MethodPost && r.Body != nil {
		var err error
		commandBody, err = io.ReadAll(http.MaxBytesReader(w, r.Body, maxAssessmentBodyBytes))
		if err != nil {
			writeProblem(w, 400, "invalid_request", "Request body is invalid", false)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(commandBody))
	}
	check := func(commit func() error) error { return s.access.WithCurrentAuthentication(auth, commit) }
	begin := func() (context.Context, func() error, error) {
		if route.budgetRead {
			return s.scopedStore.BeginBudgetReading(r.Context(), auth.Session.SubjectID, reviewer, check)
		}
		if auth.Session.Role == productaccess.RoleWorkbenchReader {
			return s.scopedStore.BeginWorkbenchReading(r.Context(), auth.Session.SubjectID, check)
		}
		return s.scopedStore.Begin(r.Context(), auth.Session.SubjectID, reviewer, check)
	}
	ctx, finish, err := begin()
	if err != nil {
		if errors.Is(err, workchain.ErrDenied) {
			writeProblem(w, http.StatusForbidden, "scope_denied", "No authorized scope is available", false)
		} else {
			writeProblem(w, http.StatusServiceUnavailable, "scoped_workchain_unavailable", "Scoped workchain is unavailable", false)
		}
		return
	}
	defer finish()
	workchain.FromContext(ctx).PurposeReviewer = productaccess.HasPermission(auth.Session.Role, productaccess.PermissionPurposeReview)
	if expected := r.Header.Get("X-XInsight-Scope-Revision"); expected != "" && expected != workchain.FromContext(ctx).Revision {
		writeProblem(w, http.StatusConflict, "scope_revision_changed", "Authorization changed; refresh the session", false)
		return
	}
	buffer := &scopedResponse{headers: make(http.Header)}
	route.handler(buffer, r.WithContext(ctx))
	if buffer.needsContentProof {
		// Release the grant transaction before source I/O. The complete normal
		// route, authorization and serialized receipt check run again afterwards.
		if err = finish(); err != nil && !errors.Is(err, context.Canceled) {
			writeProblem(w, 503, "scoped_workchain_unavailable", "Scoped workchain is unavailable", false)
			return
		}
		if r.Context().Value(contentProofAttemptKey{}) != nil {
			writeProblem(w, 400, "invalid_content_choice", "Content location verification is required", false)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(commandBody))
		verified, ok := s.verifyContentLocations(w, r, auth)
		if !ok {
			return
		}
		verified = verified.WithContext(context.WithValue(verified.Context(), contentProofAttemptKey{}, true))
		s.handleScopedWorkchain(w, verified, auth, next)
		return
	}
	if buffer.err != nil {
		writeProblem(w, http.StatusInternalServerError, "scoped_response_failed", "Scoped response could not be produced", false)
		return
	}
	if err = check(func() error { return nil }); err != nil {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	if err = finish(); err != nil && !errors.Is(err, context.Canceled) {
		writeProblem(w, http.StatusServiceUnavailable, "scoped_workchain_unavailable", "Scoped workchain is unavailable", false)
		return
	}
	for key, values := range buffer.headers {
		w.Header()[key] = values
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-XInsight-Representation", "authorized_projection")
	w.Header().Set("X-XInsight-Projection-Version", workchain.ProjectionVersion)
	w.Header().Set("X-XInsight-Scope-Revision", workchain.FromContext(ctx).Revision)
	if buffer.status == 0 {
		buffer.status = http.StatusOK
	}
	w.WriteHeader(buffer.status)
	_, _ = w.Write(buffer.body.Bytes())
}
