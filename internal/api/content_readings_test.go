package api

import (
	"bytes"
	"github.com/foigent/x-insight/internal/workbench"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	productaccess "github.com/foigent/x-insight/internal/access"
)

func TestContentRouteAndRangeBoundary(t *testing.T) {
	s := &Server{}
	// These route identifiers follow the existing runtime grammar.
	id := "xrt_" + strings.Repeat("a", 22)
	ref := "xrr_" + strings.Repeat("b", 64)
	path := "/api/v1/runtime/readings/" + id + "/requests/" + ref + "/contents/uc1_" + strings.Repeat("c", 64)
	r := httptest.NewRequest(http.MethodGet, path, nil)
	route, allowed := s.scopedRoute(r)
	if !allowed || !route.contentBytes {
		t.Fatal("binary route did not leave JSON buffer")
	}
	r.Header.Set("Range", "bytes=0-1")
	w := httptest.NewRecorder()
	s.handleRuntimeContentBytes(w, r)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "unsupported_range") {
		t.Fatalf("range: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest(http.MethodGet, path+"?purpose=human_review", nil)
	w = httptest.NewRecorder()
	s.handleRuntimeContentBytes(w, r)
	if w.Code != 400 {
		t.Fatalf("browser must not supply purpose: %d", w.Code)
	}
}

func TestContentLocationPreflightPreservesLegacyBodies(t *testing.T) {
	s := &Server{}
	for _, body := range []string{"", `{"selection":{"executions":[],"evidence":[],"gaps":[]}}`} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/cases/synthetic/seal", bytes.NewBufferString(body))
		result, ok := s.verifyContentLocations(httptest.NewRecorder(), r, productaccess.Authentication{})
		if !ok {
			t.Fatal("legacy request required content source")
		}
		actual, err := io.ReadAll(result.Body)
		if err != nil || string(actual) != body {
			t.Fatal("legacy body changed")
		}
	}
}

func TestContentObservationIntegrityFailureUsesExistingProblem(t *testing.T) {
	w := httptest.NewRecorder()
	writeContentFailure(w, workbench.ErrContentIntegrity)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"content_integrity_failed"`) || strings.Contains(w.Body.String(), "manifest does not match") {
		t.Fatalf("unexpected integrity response: %d %s", w.Code, w.Body.String())
	}
}
