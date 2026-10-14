package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

// These tests exercise the real handlers and buffered proof-error boundary.
// ScopedWorkchainStore is a concrete database store: transaction release,
// CurrentRevision and the bounded second scoped pass need database-layer tests.
// No server, socket, source connection or PostgreSQL is opened here.
type contentReceiptProvider struct{ role productaccess.Role }

func (p contentReceiptProvider) Authenticate(string, []byte) (productaccess.Principal, bool) {
	return productaccess.Principal{SubjectID: "xs_0123456789abcdef", Role: p.role}, true
}

type contentReceiptStore struct {
	workspace.Store
	workspace.AssessmentStore
	err                   error
	calls                 int
	selection             workspace.BasisSelection
	resolveCalls          int
	syntheticSessionToken string
}

func (s *contentReceiptStore) ApplyFindingAssessment(_ context.Context, _, _ string, _ workspace.Actor, c workspace.AssessmentCommand, _ time.Time) (workspace.AssessmentAction, error) {
	s.calls++
	s.selection = c.Selection
	return workspace.AssessmentAction{OperationID: c.OperationID, Command: c}, s.err
}
func (s *contentReceiptStore) CreateReview(_ context.Context, _ workspace.Actor, c workspace.ReviewCreationCommand, _ time.Time, _ io.Reader) (workspace.ReviewCreationReceipt, error) {
	s.calls++
	s.selection = c.Selection
	return workspace.ReviewCreationReceipt{OperationID: c.OperationID, Command: c}, s.err
}
func contentReceiptChoice() workspace.ContentChoice {
	pointer := "/value"
	return workspace.ContentChoice{Scope: "finding", Relation: "supports", Reference: sourcefacts.FrozenManifestRef{SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "synthetic", OrgID: "17", ProjectID: "101", RequestID: "test-request", SourceFactID: "uf1_" + strings.Repeat("a", 64), Revision: "1", SourceSHA256: "sha256:" + strings.Repeat("b", 64), CaptureStatus: "complete", ContentRef: "uc1_" + strings.Repeat("c", 64), Representation: &sourcefacts.ContentRepresentation{MediaType: "application/json", Encoding: "identity", ByteDomain: "stored_representation_before_http_content_encoding", SavedBytes: 11, Digest: sourcefacts.Hash([]byte(`{"value":1}`))}, ExpiresAt: "2020-01-01T00:00:00.000000Z"}, Location: sourcefacts.ContentLocation{Kind: "json_pointer", Pointer: &pointer}}
}
func contentReceiptServer(t *testing.T, role productaccess.Role, store *contentReceiptStore) (*Server, productaccess.Authentication, string, *int) {
	t.Helper()
	service, err := productaccess.NewService(contentReceiptProvider{role: role}, nil, productaccess.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	token, auth, err := service.Login("synthetic-test", []byte("synthetic-test-only"), "")
	if err != nil {
		t.Fatal(err)
	}
	store.syntheticSessionToken = token
	calls := new(int)
	s := &Server{workspaceStore: store, access: service, now: time.Now, contentCapacity: make(chan struct{}, 1), contentSource: func(context.Context, sourcefacts.ContentCollection) (*pullfeed.HTTPSource, error) {
		*calls++
		return nil, errors.New("source must not be called by receipt handler")
	}}
	return s, auth, service.CSRFToken(auth), calls
}
func contentReceiptRequest(auth productaccess.Authentication, csrf, path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "https://xi.invalid"+path, strings.NewReader(body))
	r.Header.Set("Origin", "https://xi.invalid")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(csrfHeaderName, csrf)
	r = r.WithContext(context.WithValue(r.Context(), accessContextKey{}, auth))
	if auth.Session.Role == productaccess.RoleScopedReviewer {
		r = r.WithContext(workchain.WithAccess(r.Context(), &workchain.Access{Subject: auth.Session.SubjectID, Reviewer: true, Revision: "test-scope"}))
	}
	return r
}
func contentReceiptBody(t *testing.T, creation bool) string {
	t.Helper()
	selection := workspace.BasisSelection{Contents: []workspace.ContentChoice{contentReceiptChoice()}}
	var command any = workspace.AssessmentCommand{Operation: "record_review", OperationID: "receipt-retry", Selection: selection, ExpectedVersion: 1, ExpectedCaseVersion: 2, ExpectedBasisDigest: "sha256:" + strings.Repeat("d", 64), Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Previously verified pointer"}}
	if creation {
		command = workspace.ReviewCreationCommand{Operation: "start_review", OperationID: "receipt-retry", Selection: selection}
	}
	raw, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func runContentReceiptHandler(s *Server, w http.ResponseWriter, r *http.Request, creation bool) {
	if creation {
		s.handleReviewCreation(w, r)
		return
	}
	s.handleFindingAssessmentPath(w, r, "xc_"+strings.Repeat("A", 22), "xf_"+strings.Repeat("B", 22), []string{"assessment-actions"})
}
func TestContentReceiptRetryHandlerPublishesStoreReceiptWithoutSourceRead(t *testing.T) {
	for _, creation := range []bool{false, true} {
		t.Run(fmt.Sprint("creation=", creation), func(t *testing.T) {
			store := &contentReceiptStore{}
			s, auth, csrf, sourceCalls := contentReceiptServer(t, productaccess.RoleScopedReviewer, store)
			r := contentReceiptRequest(auth, csrf, "/api/v1/security/reviews", contentReceiptBody(t, creation))
			w := &scopedResponse{headers: make(http.Header)}
			runContentReceiptHandler(s, w, r, creation)
			if store.calls != 1 || *sourceCalls != 0 || w.needsContentProof || w.status != http.StatusOK || !strings.Contains(w.body.String(), `"operation_id":"receipt-retry"`) {
				t.Fatalf("calls=%d sources=%d proof=%t status=%d body=%s", store.calls, *sourceCalls, w.needsContentProof, w.status, w.body.String())
			}
			if workspace.ContentChoiceKey(store.selection.Contents[0]) != workspace.ContentChoiceKey(contentReceiptChoice()) {
				t.Fatal("receipt command choice changed")
			}
		})
	}
}
func TestContentReceiptNewOperationSignalsProofWithoutPublishingAResponse(t *testing.T) {
	for _, creation := range []bool{false, true} {
		t.Run(fmt.Sprint("creation=", creation), func(t *testing.T) {
			store := &contentReceiptStore{err: fmt.Errorf("new adoption: %w", workspace.ErrContentLocationProofRequired)}
			s, auth, csrf, sourceCalls := contentReceiptServer(t, productaccess.RoleScopedReviewer, store)
			r := contentReceiptRequest(auth, csrf, "/api/v1/security/reviews", contentReceiptBody(t, creation))
			w := &scopedResponse{headers: make(http.Header)}
			runContentReceiptHandler(s, w, r, creation)
			if store.calls != 1 || *sourceCalls != 0 || !w.needsContentProof || w.status != 0 || w.body.Len() != 0 {
				t.Fatalf("proof request leaked receipt/problem: calls=%d sources=%d proof=%t status=%d body=%s", store.calls, *sourceCalls, w.needsContentProof, w.status, w.body.String())
			}
		})
	}
}
func TestContentReceiptProofSignalDoesNotConvertOrdinaryFailuresOrEscapeBuffer(t *testing.T) {
	for _, err := range []error{workspace.ErrInvalid, workspace.ErrConflict, workspace.ErrNotFound, errors.New("unavailable")} {
		w := &scopedResponse{headers: make(http.Header)}
		writeWorkspaceFailure(w, err)
		if w.needsContentProof || w.status < 400 || w.body.Len() == 0 {
			t.Fatalf("ordinary failure became source-read instruction: %v", err)
		}
	}
	w := httptest.NewRecorder()
	writeWorkspaceFailure(w, workspace.ErrContentLocationProofRequired)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"invalid_content_choice"`) {
		t.Fatalf("unbuffered proof error: %d %s", w.Code, w.Body.String())
	}
}
func TestContentReceiptProofAndAttemptContextNeverBypassHandlerAuthorization(t *testing.T) {
	for _, kind := range []string{"missing-csrf", "wrong-origin", "read-only-role", "no-scoped-context"} {
		t.Run(kind, func(t *testing.T) {
			role := productaccess.RoleScopedReviewer
			if kind == "read-only-role" {
				role = productaccess.RoleWorkbenchReader
			}
			store := &contentReceiptStore{}
			s, auth, csrf, sourceCalls := contentReceiptServer(t, role, store)
			r := contentReceiptRequest(auth, csrf, "/api/v1/security/reviews", contentReceiptBody(t, true))
			if kind == "missing-csrf" {
				r.Header.Del(csrfHeaderName)
			}
			if kind == "wrong-origin" {
				r.Header.Set("Origin", "https://another.invalid")
			}
			if kind == "no-scoped-context" {
				r = r.WithContext(context.WithValue(context.Background(), accessContextKey{}, auth))
			}
			ctx := workspace.WithVerifiedContentLocations(r.Context(), auth.Session.SubjectID, []workspace.ContentChoice{contentReceiptChoice()})
			r = r.WithContext(context.WithValue(ctx, contentProofAttemptKey{}, true))
			w := &scopedResponse{headers: make(http.Header)}
			runContentReceiptHandler(s, w, r, true)
			if w.status != 403 || store.calls != 0 || *sourceCalls != 0 || w.needsContentProof {
				t.Fatalf("proof bypassed %s: status=%d store=%d source=%d", kind, w.status, store.calls, *sourceCalls)
			}
		})
	}
}
func TestContentReceiptProofPrepassInspectsClosureAndPreservesExactBodyOnFailure(t *testing.T) {
	raw, err := json.Marshal(contentReceiptChoice())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"selection":{"contents":[%s]}`, `"progress":{"closure":{"verification_contents":[%s]}}`} {
		body := "{\n " + fmt.Sprintf(field, raw) + ",\"reason\":\"\\u0061\",\"expected_version\":9007199254740993\n}\n"
		r := httptest.NewRequest(http.MethodPost, "https://xi.invalid/api/v1/security/reviews", strings.NewReader(body))
		w := httptest.NewRecorder()
		result, ok := (&Server{}).verifyContentLocations(w, r, productaccess.Authentication{})
		preserved, e := io.ReadAll(result.Body)
		if ok || w.Code != 503 || !strings.Contains(w.Body.String(), "content_not_supported") || e != nil || !bytes.Equal(preserved, []byte(body)) {
			t.Fatalf("closure/selection omitted or raw body changed: ok=%t status=%d error=%v", ok, w.Code, e)
		}
		if workspace.ContentLocationVerified(result.Context(), "", contentReceiptChoice()) {
			t.Fatal("failed prepass installed a proof")
		}
	}
}
func TestContentReceiptProofPrepassRejectsDuplicateAndOversizedCommands(t *testing.T) {
	for _, body := range []string{`{"selection":{"contents":[],"contents":[]}}`, strings.Repeat(" ", maxAssessmentBodyBytes+1)} {
		r := httptest.NewRequest(http.MethodPost, "https://xi.invalid/api/v1/security/reviews", strings.NewReader(body))
		w := httptest.NewRecorder()
		_, ok := (&Server{}).verifyContentLocations(w, r, productaccess.Authentication{})
		if ok || w.Code != 400 {
			t.Fatalf("invalid prepass accepted: ok=%t status=%d", ok, w.Code)
		}
	}
}

func (s *contentReceiptStore) ResolveContentChoice(context.Context, string, workspace.ContentChoice, time.Time) (workbench.RuntimeContentRead, error) {
	s.resolveCalls++
	return workbench.RuntimeContentRead{}, errors.New("must not resolve a stale session")
}
func TestContentReceiptProofPrepassRechecksCurrentSessionBeforeSourceOrDatabase(t *testing.T) {
	for _, kind := range []string{"revoked", "changed-role"} {
		t.Run(kind, func(t *testing.T) {
			store := &contentReceiptStore{}
			s, auth, csrf, sourceCalls := contentReceiptServer(t, productaccess.RoleScopedReviewer, store)
			// Zero store deliberately has no DB. Current authentication must fail first.
			s.scopedStore = &db.ScopedWorkchainStore{}
			if kind == "revoked" {
				if !s.access.Logout(store.syntheticSessionToken) {
					t.Fatal("synthetic session was not removed")
				}
			} else {
				auth.Session.Role = productaccess.RoleWorkbenchReader
			}
			r := contentReceiptRequest(auth, csrf, "/api/v1/security/reviews", contentReceiptBody(t, true))
			ctx := workspace.WithVerifiedContentLocations(r.Context(), auth.Session.SubjectID, []workspace.ContentChoice{contentReceiptChoice()})
			r = r.WithContext(context.WithValue(ctx, contentProofAttemptKey{}, true))
			r.Header.Set("X-XInsight-Scope-Revision", "test-scope")
			w := httptest.NewRecorder()
			_, ok := s.verifyContentLocations(w, r, auth)
			if ok || w.Code != 401 || store.resolveCalls != 0 || *sourceCalls != 0 {
				t.Fatalf("stale session reached source/database: ok=%t status=%d resolves=%d source=%d", ok, w.Code, store.resolveCalls, *sourceCalls)
			}
		})
	}
}
func TestContentReceiptProofBudgetCountsUniqueClosureSelections(t *testing.T) {
	for _, count := range []int{32, 33} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			choices := make([]workspace.ContentChoice, count)
			for i := range choices {
				choices[i] = contentReceiptChoice()
				pointer := fmt.Sprintf("/value/%d", i)
				choices[i].Location.Pointer = &pointer
			}
			body, err := json.Marshal(map[string]any{"selection": map[string]any{"contents": choices}, "progress": map[string]any{"closure": map[string]any{"verification_contents": choices}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(body) > maxAssessmentBodyBytes {
				t.Fatalf("test fixture exceeded body limit: %d", len(body))
			}
			r := httptest.NewRequest(http.MethodPost, "https://xi.invalid/api/v1/security/reviews", bytes.NewReader(body))
			w := httptest.NewRecorder()
			store := &contentReceiptStore{}
			s, auth, _, sourceCalls := contentReceiptServer(t, productaccess.RoleScopedReviewer, store)
			s.scopedStore = &db.ScopedWorkchainStore{}
			auth.Session.Role = productaccess.RoleWorkbenchReader
			_, ok := s.verifyContentLocations(w, r, auth)
			// 32 unique choices reach the actual current-session check, which
			// rejects this deliberately stale role before any database/source
			// access. Duplicate closure choices consume no further slots.
			want := 401
			if count == 33 {
				want = 400
			}
			if ok || w.Code != want || store.resolveCalls != 0 || *sourceCalls != 0 {
				t.Fatalf("unique=%d status=%d body=%s", count, w.Code, w.Body.String())
			}
		})
	}
}
