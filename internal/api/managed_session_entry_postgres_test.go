package api_test

import (
	"encoding/json"
	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/workbench"
	"io"
	"net/url"
	"testing"
	"time"
)

func TestManagedSessionIdentityAndMetadataOnlyEntryHintsPostgres(t *testing.T) {
	s, _, readerID, query := startManagedAccountAPI(t)
	owner := newManagedHTTPClient(t, s, "owner", managedPassword)
	readSession := func() (string, workbench.ReadingCapabilities) {
		t.Helper()
		n, raw := owner.call("GET", "/api/v1/access/session?username=reader", nil, nil)
		var reply struct {
			ScopedData   bool                          `json:"scoped_data_access"`
			Session      access.SessionView            `json:"session"`
			Revision     string                        `json:"scope_revision"`
			Capabilities workbench.ReadingCapabilities `json:"reading_capabilities"`
		}
		if n != 200 || json.Unmarshal(raw, &reply) != nil || reply.Session.Username != "owner" || reply.Session.SubjectID != managedOwnerID || reply.Session.Role != access.RolePlatformAdmin {
			t.Fatalf("current identity not server-derived: status%d", n)
		}
		if !reply.ScopedData {
			t.Fatal("managed owner session omitted the enforced scoped data boundary")
		}
		return reply.Revision, reply.Capabilities
	}
	_, beforeCaps := readSession()
	if beforeCaps != (workbench.ReadingCapabilities{}) {
		t.Fatal("ungranted owner received entry authority")
	}
	beforeReader := query(`SELECT COALESCE(jsonb_agg(to_jsonb(g) ORDER BY id),'[]')::text FROM workbench_access_grants g WHERE subject_id='` + readerID + `'`)
	var options struct {
		Projects []struct {
			Source  string `json:"source_system_id"`
			Project string `json:"source_project_ref"`
		} `json:"projects"`
	}
	n, raw := owner.call("GET", "/api/v1/access/project-read-grants", nil, nil)
	if n != 200 || json.Unmarshal(raw, &options) != nil || len(options.Projects) == 0 {
		t.Fatal("missing explicit project options")
	}
	scope := options.Projects[0]
	id := randomTestUUID(t)
	command := map[string]any{"grant_id": id, "subject_id": managedOwnerID, "source_system_id": scope.Source, "source_project_ref": scope.Project, "expected_revision": 0, "capabilities": []string{"details", "coverage", "source_context", "source_runtime"}, "check_only": false, "revoke": false, "password": managedPassword}
	if n, _ = owner.call("POST", "/api/v1/access/project-read-grants", command, nil); n != 200 {
		t.Fatalf("explicit metadata grant%d", n)
	}
	revision, caps := readSession()
	if revision == "" || revision == "no_authorized_scope" || caps != (workbench.ReadingCapabilities{Runtime: true, Coverage: true}) {
		t.Fatalf("wrong effective entry hints%+v", caps)
	}
	for _, path := range []string{"/api/v1/posture", "/api/v1/saved-views", "/api/v1/sessions", "/api/v1/sessions/legacy-session", "/api/v1/discovery/hypotheses"} {
		if n, _ = owner.call("GET", path, nil, nil); n != 403 {
			t.Fatalf("managed owner legacy data route admitted: %s status%d", path, n)
		}
	}
	path := "/api/v1/work-activity/summary?from=" + url.QueryEscape(time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)) + "&to=" + url.QueryEscape(time.Now().UTC().Format(time.RFC3339))
	response, e := owner.client.Get(s.URL + path)
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(response.Body)
	response.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	var problem struct {
		Code string `json:"code"`
	}
	if response.StatusCode != 403 || json.Unmarshal(body, &problem) != nil || problem.Code != "workbench_scope_denied" || response.Header.Get("X-XInsight-Scope-Revision") != revision {
		t.Fatalf("local work activity denial lost current revision: status%d code%s", response.StatusCode, problem.Code)
	}
	request := httpReadingRequest(scope.Source)
	request.Filters.SourceProjects = []string{scope.Project}
	if n, _ = owner.call("POST", "/api/v1/workbench/readings", request, nil); n != 403 {
		t.Fatalf("metadata grant acquired economic summary%d", n)
	}
	own := "/api/v1/runtime/current?source_ids=" + scope.Source + "&source_projects=" + url.QueryEscape(scope.Project)
	if n, _ = owner.call("GET", own, nil, nil); n != 200 {
		t.Fatalf("own project denied after local summary denial%d", n)
	}
	if n, _ = owner.call("GET", "/api/v1/runtime/current?source_ids="+scope.Source+"&source_projects=outside-project", nil, nil); n != 403 {
		t.Fatal("foreign project admitted")
	}
	if n, _ = owner.call("GET", own, nil, nil); n != 200 {
		t.Fatal("local denial removed usable project")
	}
	if again, _ := readSession(); again != revision {
		t.Fatal("legitimate local denial changed session scope")
	}
	if after := query(`SELECT COALESCE(jsonb_agg(to_jsonb(g) ORDER BY id),'[]')::text FROM workbench_access_grants g WHERE subject_id='` + readerID + `'`); beforeReader != after {
		t.Fatal("owner change copied reader authority")
	}
	command["expected_revision"] = 1
	command["revoke"] = true
	if n, _ = owner.call("POST", "/api/v1/access/project-read-grants", command, nil); n != 200 {
		t.Fatal("revoke failed")
	}
	_, afterCaps := readSession()
	if afterCaps != (workbench.ReadingCapabilities{}) {
		t.Fatal("revoked capabilities remained visible")
	}
	if n, _ = owner.call("GET", own, nil, nil); n != 403 {
		t.Fatal("revoked metadata still readable")
	}
	if n, _ = owner.call("GET", "/api/v1/access/accounts", nil, nil); n != 200 {
		t.Fatal("scope revocation removed account management")
	}
}
