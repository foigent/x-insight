package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/foigent/x-insight/internal/access"
)

const queryTestReading = "xrt_abcdefghijklmnopqrstuv"

func TestPersonnelQueryParserClosesActionsAndScopeInputs(t *testing.T) {
	base := []string{"runtime", "summary", "--origin", "https://xi.example", "--username", "person", "--reading", queryTestReading}
	q, err := parsePersonnelQuery(base)
	if err != nil || q.method != "GET" || q.path != "/api/v1/runtime/readings/"+queryTestReading+"/summary?entry_time=known" {
		t.Fatal("summary contract", q.path, err)
	}
	for _, tail := range [][]string{{"--limit", "1"}, {"--request-ref", "anything"}, {"--entry-time", "invalid"}, {"--origin", "http://xi.example"}, {"--origin", "https://person:private-value@xi.example"}, {"--origin", "https://xi.example/api"}, {"--origin", "https://xi.example?credential=private-value"}, {"--origin", "https://xi.example#private-value"}, {"--password", "private-value"}, {"extra"}} {
		_, err := parsePersonnelQuery(append(append([]string{}, base...), tail...))
		if err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatal("invalid flags accepted or reflected sensitive input")
		}
	}
	for _, action := range []string{"reading", "summary", "requests", "request", "associations", "resources", "measurements", "dimensions", "observations", "runtime", "attempts", "capabilities", "policies", "destinations", "current"} {
		args := []string{"runtime", action, "--origin", "https://xi.example/", "--username", "person"}
		if action != "current" {
			args = append(args, "--reading", queryTestReading)
		}
		if action == "request" || action == "associations" || action == "resources" {
			args = append(args, "--request-ref", "xrr_"+strings.Repeat("a", 64))
		}
		if action == "dimensions" {
			args = append(args, "--dimension", "selected_model")
		}
		q, err := parsePersonnelQuery(args)
		if err != nil || q.origin != "https://xi.example" || q.method != "GET" {
			t.Fatal("supported query rejected", action, err)
		}
	}
	q, err = parsePersonnelQuery([]string{"runtime", "current", "--origin", "https://xi.example", "--username", "person", "--source-projects", "project-3, project-4", "--cursor", "next+page&x=1"})
	if err != nil || !strings.Contains(q.path, "source_projects=project-3%2Cproject-4") || !strings.Contains(q.path, "cursor=next%2Bpage%26x%3D1") {
		t.Fatal("current query encoding", q.path, err)
	}
}

func TestPersonnelQueryReadingInputPreflightRejectsWideningAndLinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	raw := `{"contract_version":"xinsight.runtime-reading.v1","metric_version":"xinsight.runtime.request-observations.v1","period":{"relative_hours":24,"display_timezone":"UTC"},"filters":{"source_ids":[],"source_projects":[],"cost_projects":[]}}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"runtime", "create", "--origin", "https://xi.example", "--username", "person", "--request-file", path}
	q, err := parsePersonnelQuery(args)
	if err != nil || q.method != "POST" || q.path != "/api/v1/runtime/readings" || string(q.body) != raw {
		t.Fatal("valid reading request", err)
	}
	for _, invalid := range []string{strings.Replace(raw, `"filters":`, `"money_basis":"gateway_charged","filters":`, 1), strings.Replace(raw, `"relative_hours":24`, `"relative_hours":24,"relative_hours":168`, 1), raw + `{}`} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := parsePersonnelQuery(args); err == nil {
			t.Fatal("widened or ambiguous reading accepted")
		}
	}
	link := filepath.Join(filepath.Dir(path), "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	args[len(args)-1] = link
	if _, err := parsePersonnelQuery(args); err == nil {
		t.Fatal("symlink reading request accepted")
	}
}

func TestPersonnelQueryHTTPSUsesCurrentScopeAndLogsOut(t *testing.T) {
	var calls []string
	var callsMu sync.Mutex
	var origin string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsMu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		callsMu.Unlock()
		if r.Header.Get("Origin") != origin {
			t.Error("missing exact same-origin header")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/access/login":
			var input map[string]string
			if json.NewDecoder(r.Body).Decode(&input) != nil || input["username"] != "person" || input["password"] != "synthetic-password" {
				t.Error("wrong login input")
			}
			http.SetCookie(w, &http.Cookie{Name: access.SessionCookieName, Value: "synthetic-session", Secure: true, HttpOnly: true, Path: "/", SameSite: http.SameSiteStrictMode})
			fmt.Fprint(w, `{"session":{"subject_id":"xs_person","session_ref":"session-one"},"csrf_token":"synthetic-csrf"}`)
		case "/api/v1/access/session":
			cookie, err := r.Cookie(access.SessionCookieName)
			if err != nil || cookie.Value != "synthetic-session" {
				t.Error("Session missing on scope check")
			}
			fmt.Fprint(w, `{"session":{"subject_id":"xs_person","session_ref":"session-one"},"csrf_token":"synthetic-csrf","scope_revision":"scope-one"}`)
		case "/api/v1/runtime/readings":
			if r.Header.Get("X-CSRF-Token") != "synthetic-csrf" || r.Header.Get("X-XInsight-Scope-Revision") != "scope-one" || r.Method != "POST" {
				t.Error("reading did not preserve personnel CSRF and scope")
			}
			w.Header().Set("X-XInsight-Scope-Revision", "scope-one")
			w.WriteHeader(201)
			fmt.Fprint(w, `{"reading_id":"xrt_abcdefghijklmnopqrstuv","wide_identity":"9007199254740993"}`)
		case "/api/v1/access/logout":
			if r.Header.Get("X-CSRF-Token") != "synthetic-csrf" {
				t.Error("logout lacks CSRF")
			}
			w.WriteHeader(204)
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	origin = server.URL
	password := []byte("synthetic-password")
	raw, err := executePersonnelQuery(t.Context(), personnelQuery{origin: origin, username: "person", method: "POST", path: "/api/v1/runtime/readings", body: []byte(`{}`)}, password, server.Client().Transport)
	if err != nil || !strings.Contains(string(raw), "9007199254740993") || strings.Contains(string(raw), "synthetic-csrf") || strings.Contains(string(raw), "synthetic-session") {
		t.Fatal("query output contract", err)
	}
	if !bytes.Equal(password, make([]byte, len(password))) {
		t.Fatal("password buffer retained")
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if len(calls) != 4 || calls[0] != "POST /api/v1/access/login" || calls[1] != "GET /api/v1/access/session" || calls[2] != "POST /api/v1/runtime/readings" || calls[3] != "POST /api/v1/access/logout" {
		t.Fatal("query lifecycle", calls)
	}
}

func TestPersonnelQueryRejectsScopeChangesAndNeverReflectsErrorBodies(t *testing.T) {
	for _, mode := range []string{"scope-changed", "forbidden", "bad-json", "wrong-session", "logout-failure"} {
		t.Run(mode, func(t *testing.T) {
			var logout atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/access/login":
					http.SetCookie(w, &http.Cookie{Name: access.SessionCookieName, Value: "synthetic-session", Secure: true, HttpOnly: true, Path: "/", SameSite: http.SameSiteStrictMode})
					fmt.Fprint(w, `{"session":{"subject_id":"xs_person","session_ref":"session-one"},"csrf_token":"synthetic-csrf"}`)
				case "/api/v1/access/session":
					ref := "session-one"
					if mode == "wrong-session" {
						ref = "different-session"
					}
					fmt.Fprintf(w, `{"session":{"subject_id":"xs_person","session_ref":%q},"csrf_token":"synthetic-csrf","scope_revision":"scope-one"}`, ref)
				case "/api/v1/access/logout":
					logout.Add(1)
					if mode == "logout-failure" {
						w.WriteHeader(503)
					} else {
						w.WriteHeader(204)
					}
				default:
					w.Header().Set("X-XInsight-Scope-Revision", "scope-one")
					if mode == "scope-changed" {
						w.Header().Set("X-XInsight-Scope-Revision", "scope-two")
					}
					if mode == "forbidden" {
						w.WriteHeader(403)
					}
					if mode == "bad-json" {
						fmt.Fprint(w, `{"value":1,"value":2}`)
					} else {
						fmt.Fprint(w, `{"title":"synthetic-csrf synthetic-password should never appear in an error"}`)
					}
				}
			}))
			defer server.Close()
			raw, err := executePersonnelQuery(t.Context(), personnelQuery{origin: server.URL, username: "person", method: "GET", path: "/api/v1/runtime/current"}, []byte("synthetic-password"), server.Client().Transport)
			if err == nil || raw != nil || strings.Contains(err.Error(), "synthetic-") || logout.Load() != 1 {
				t.Fatal("failed query did not close or minimize output", err, logout.Load())
			}
		})
	}
}

func TestPersonnelQueryNeverFollowsLoginRedirectOrBypassesTLS(t *testing.T) {
	var otherCalls atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { otherCalls.Add(1); w.WriteHeader(204) }))
	defer other.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	q := personnelQuery{origin: server.URL, username: "person", method: "GET", path: "/api/v1/runtime/current"}
	if _, err := executePersonnelQuery(t.Context(), q, []byte("synthetic-password"), server.Client().Transport); err == nil || otherCalls.Load() != 0 {
		t.Fatal("credential-bearing redirect followed")
	}
	if _, err := executePersonnelQuery(context.Background(), q, []byte("synthetic-password"), http.DefaultTransport); err == nil || otherCalls.Load() != 0 {
		t.Fatal("untrusted TLS accepted")
	}
}

func TestBudgetPersonnelQueryUsesBudgetRoutesAndRejectsCrossViewInputs(t *testing.T) {
	id := "xbd_abcdefghijklmnopqrstuv"
	base := []string{"budgets", "history", "--origin", "https://xi.example", "--username", "person", "--reading", id, "--budget-ref", "10000000-0000-4000-8000-000000000001", "--cursor", "opaque&next"}
	q, err := parsePersonnelQuery(base)
	if err != nil || q.path != "/api/v1/workbench/budgets/readings/"+id+"/items/10000000-0000-4000-8000-000000000001/history?cursor=opaque%26next&limit=50" {
		t.Fatal(q.path, err)
	}
	for _, extra := range [][]string{{"--entry-time", "known"}, {"--cost-projects", "cost-1"}, {"--reading", queryTestReading}, {"--password", "hidden"}} {
		if _, err := parsePersonnelQuery(append(append([]string{}, base...), extra...)); err == nil {
			t.Fatal("runtime or secret flag accepted for budget query")
		}
	}
	path := filepath.Join(t.TempDir(), "budget.json")
	raw := `{"contract_version":"xinsight.budget-reading.v1","filters":{"source_ids":[],"source_projects":[],"cost_projects":[]}}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"budgets", "create", "--origin", "https://xi.example", "--username", "person", "--request-file", path}
	q, err = parsePersonnelQuery(args)
	if err != nil || q.path != "/api/v1/workbench/budgets/readings" || q.method != "POST" {
		t.Fatal(q, err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(raw, `"cost_projects":[]`, `"cost_projects":["project-1"]`, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := parsePersonnelQuery(args); err == nil {
		t.Fatal("unproven detail scope silently changes whole-period budget")
	}
}
