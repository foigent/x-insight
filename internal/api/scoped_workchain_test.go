package api

import (
	"context"
	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/workchain"
	"net/http/httptest"
	"testing"
)

func TestScopedReviewerRequiresCurrentMatchingRequestContext(t *testing.T) {
	r := httptest.NewRequest("POST", "https://x-insight.local/api/v1/security/reviews", nil)
	auth := access.Authentication{Session: access.SessionView{Role: access.RoleScopedReviewer, SubjectID: "xs_1111111111111111"}}
	r = r.WithContext(context.WithValue(r.Context(), accessContextKey{}, auth))
	if canWriteInvestigation(r) {
		t.Fatal("role alone granted legacy write")
	}
	for _, a := range []*workchain.Access{{Reviewer: true, Subject: "xs_2222222222222222"}, {Subject: auth.Session.SubjectID}} {
		if canWriteInvestigation(r.WithContext(workchain.WithAccess(r.Context(), a))) {
			t.Fatal("wrong subject or read-only scope granted write")
		}
	}
	if !canWriteInvestigation(r.WithContext(workchain.WithAccess(r.Context(), &workchain.Access{Subject: auth.Session.SubjectID, Reviewer: true}))) {
		t.Fatal("authorized reviewer denied")
	}
}
func TestScopedRouteDoesNotDefaultOpenFutureOrVariantRoutes(t *testing.T) {
	s := &Server{}
	for _, path := range []string{"/api/v1/future", "/api/v1/cases", "/api/v1/posture", "/api/v1/executions/legacy-request", "/api/v1//executions", "/api/v1/executions/xe1_invalid/events/../feedback"} {
		if _, ok := s.scopedRoute(httptest.NewRequest("POST", path, nil)); ok {
			t.Fatalf("unexpected scoped POST %s", path)
		}
	}
	if _, ok := s.scopedRoute(httptest.NewRequest("GET", "/api/v1/executions", nil)); !ok {
		t.Fatal("supported collection denied")
	}
	if _, ok := s.scopedRoute(httptest.NewRequest("GET", "/api/v1%2fexecutions", nil)); ok {
		t.Fatal("encoded slash accepted")
	}
}

func TestWorkActivityScopedRouteAcceptsOnlyCanonicalEncodedRefs(t *testing.T) {
	s := &Server{}
	for _, path := range []string{
		"/api/v1/work-activity/summary?from=2026-09-29T00%3A00%3A00Z&to=2026-09-29T01%3A00%3A00Z",
		"/api/v1/work-activity/events/11111111-1111-4111-8111-111111111111/openclaw-browser/7/9/oc%3Ap2-browser-1",
		"/api/v1/work-activity/contents/ocbody%3Ap2-browser-1",
	} {
		if _, ok := s.scopedRoute(httptest.NewRequest("GET", path, nil)); !ok {
			t.Fatalf("canonical browser path denied: %s", path)
		}
	}
	for _, path := range []string{
		"/api/v1/work-activity/events/11111111-1111-4111-8111-111111111111/openclaw-browser/7/9/oc%3ap2-browser-1",
		"/api/v1/work-activity/events/11111111-1111-4111-8111-111111111111/openclaw-browser/7/9/oc%253Ap2-browser-1",
		"/api/v1/work-activity/events/11111111-1111-4111-8111-111111111111/openclaw-browser/7/9/oc%2Fp2-browser-1",
		"/api/v1/work-activity/contents/ocbody%2Fwrong",
	} {
		if _, ok := s.scopedRoute(httptest.NewRequest("GET", path, nil)); ok {
			t.Fatalf("noncanonical encoded route accepted: %s", path)
		}
	}
}

func TestScopedPersonalFilterMethodsRemainPreferences(t *testing.T) {
	s := &Server{}
	for _, path := range []string{"/api/v1/investigation/queue-filters", "/api/v1/investigation/queue-filters/xqf_abcdefghijklmnopqrstuv"} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD"} {
			route, ok := s.scopedRoute(httptest.NewRequest(method, path, nil))
			collection := path == "/api/v1/investigation/queue-filters"
			want := collection && (method == "GET" || method == "POST") || !collection && (method == "PUT" || method == "DELETE")
			if ok != want || ok && route.write {
				t.Fatalf("%s %s: allowed=%v write=%v", method, path, ok, route.write)
			}
		}
	}
}
