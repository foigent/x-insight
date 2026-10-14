package api

import (
	"encoding/json"
	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/workspace"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// The content pre-pass must leave historical JSON bytes untouched, including
// escaped user text, whitespace and integers that float64 cannot represent.
func TestContentReviewLegacyPOSTBytesPreserved(t *testing.T) {
	for _, body := range []string{
		"{\n  \"evidence\": [], \"gaps\": []\n}\n",
		`{"statement":"literal contents and verification_contents are user text","version":9007199254740993}`,
		`{"annotation":"{\"contents\":null}","text":"\u0061"}`,
	} {
		r := httptest.NewRequest("POST", "https://xi.invalid/api/v1/security/reviews/preview", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		next, ok := (&Server{}).verifyContentLocations(httptest.NewRecorder(), r, productaccess.Authentication{})
		if !ok {
			t.Fatal("old POST was made dependent on content source")
		}
		raw, e := io.ReadAll(next.Body)
		if e != nil || string(raw) != body {
			t.Fatal("old command bytes changed", e)
		}
	}
}
func TestContentReviewGETDoesNotConsumeBody(t *testing.T) {
	body := "opaque body"
	r := httptest.NewRequest("GET", "https://xi.invalid/api/v1/runtime/readings", strings.NewReader(body))
	next, ok := (&Server{}).verifyContentLocations(httptest.NewRecorder(), r, productaccess.Authentication{})
	raw, e := io.ReadAll(next.Body)
	if !ok || e != nil || string(raw) != body {
		t.Fatal("GET changed", e)
	}
}

// Reproduces the exact empty-selection wire input without a listener or PG.
// Nil slices were already legal JSON input; the r4 rejection was store admission.
func TestContentExecutionContextEmptySelectionHTTPDecode(t *testing.T) {
	raw, err := json.Marshal(workspace.BasisSelection{})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{string(raw), `{}`, `{"evidence":[],"gaps":[]}`} {
		r := httptest.NewRequest("POST", "https://xi.invalid/assessment/preview", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		var selection workspace.BasisSelection
		if err := decodeBoundedWorkspaceRequest(httptest.NewRecorder(), r, &selection, maxAssessmentBodyBytes); err != nil {
			t.Fatal("empty selection rejected by existing HTTP decoder", err)
		}
		if len(selection.Evidence)+len(selection.Gaps)+len(selection.Resources)+len(selection.Contents) != 0 {
			t.Fatal("empty selection gained citations")
		}
	}
}
