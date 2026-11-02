package api_test

import (
	"bytes"
	"encoding/json"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
)

const managedOwnerID = "xs_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const managedPassword = "synthetic account API password"

type managedHTTPClient struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	csrf   string
}

func (c *managedHTTPClient) call(method, path string, body any, headers map[string]string) (int, []byte) {
	c.t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	r, e := http.NewRequest(method, c.server.URL+path, bytes.NewReader(raw))
	if e != nil {
		c.t.Fatal(e)
	}
	r.Header.Set("Origin", c.server.URL)
	r.Header.Set("Content-Type", "application/json")
	if c.csrf != "" {
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	resp, e := c.client.Do(r)
	if e != nil {
		c.t.Fatal(e)
	}
	defer resp.Body.Close()
	out, e := io.ReadAll(resp.Body)
	if e != nil {
		c.t.Fatal(e)
	}
	return resp.StatusCode, out
}
func newManagedHTTPClient(t *testing.T, s *httptest.Server, username, password string) *managedHTTPClient {
	t.Helper()
	base := s.Client()
	client := &http.Client{Transport: base.Transport, Timeout: base.Timeout}
	jar, _ := cookiejar.New(nil)
	client.Jar = jar
	c := &managedHTTPClient{t: t, server: s, client: client}
	status, raw := c.call("POST", "/api/v1/access/login", map[string]string{"username": username, "password": password}, nil)
	if status != 200 {
		t.Fatalf("managed login returned %d", status)
	}
	var reply struct {
		CSRF string `json:"csrf_token"`
	}
	if json.Unmarshal(raw, &reply) != nil || reply.CSRF == "" {
		t.Fatal("missing CSRF")
	}
	c.csrf = reply.CSRF
	return c
}
func startManagedAccountAPI(t *testing.T) (*httptest.Server, *db.LocalAccountStore, string, func(string) string) {
	t.Helper()
	legacy, admin, writer, _, _ := startWorkbenchProductionFixture(t, true)
	reader := "xs_6666666666666666"
	legacy.Close()
	hash, e := access.DerivePasswordHash([]byte(managedPassword))
	if e != nil {
		t.Fatal(e)
	}
	root := db.NewLocalAccountStore(admin)
	if e = root.Initialize(t.Context(), access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{{SubjectID: managedOwnerID, Username: "owner", Role: access.RolePlatformAdmin, PasswordHash: hash}, {SubjectID: reader, Username: "reader", Role: access.RoleScopedReader, PasswordHash: hash}}}, "synthetic-existing-v3"); e != nil {
		t.Fatal(e)
	}
	provider, e := access.NewDurableProvider(writer.LocalAccounts())
	if e != nil {
		t.Fatal(e)
	}
	service, e := access.NewService(provider, nil, access.ServiceOptions{})
	if e != nil {
		t.Fatal(e)
	}
	checks, e := db.LoadMigrationChecksums("../../migrations")
	if e != nil {
		t.Fatal(e)
	}
	var databaseName string
	if e = admin.QueryRow("SELECT current_database()").Scan(&databaseName); e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	if e != nil {
		t.Fatal(e)
	}
	u.Path = "/" + databaseName
	u.User = workbenchRuntimeRoleUser(t, "reader")
	evidence, e := db.OpenEvidenceStore(t.Context(), u.String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { evidence.Close() })
	s := httptest.NewTLSServer(api.NewProductionHandler(evidence, writer, checks, service, api.TransportSecurity{}, connectorcontrol.InvitationConfig{}, nil, nil))
	t.Cleanup(s.Close)
	query := func(q string) string {
		t.Helper()
		var v string
		if e := admin.QueryRow(q).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	return s, root, reader, query
}
func TestManagedAccountHTTPActivationRecoveryAndSessionIsolationPostgres(t *testing.T) {
	s, _, readerID, query := startManagedAccountAPI(t)
	anonymous := &managedHTTPClient{t: t, server: s, client: s.Client()}
	if n, raw := anonymous.call("GET", "/access/activate", nil, nil); n != 200 || !bytes.Contains(raw, []byte("<html")) {
		t.Fatalf("native activation deep link unavailable %d", n)
	}
	owner := newManagedHTTPClient(t, s, "owner", managedPassword)
	if n, raw := owner.call("GET", "/access/accounts", nil, nil); n != 200 || !bytes.Contains(raw, []byte("<html")) {
		t.Fatalf("native account page reload unavailable %d", n)
	}
	reader := newManagedHTTPClient(t, s, "reader", managedPassword)
	if n, raw := owner.call("GET", "/api/v1/access/accounts", nil, nil); n != 200 || bytes.Contains(raw, []byte("password_hash")) {
		t.Fatalf("directory failed or exposed credentials: status=%d", n)
	}
	if n, _ := reader.call("GET", "/api/v1/access/accounts", nil, nil); n != 403 {
		t.Fatalf("reader account administration %d", n)
	}
	command := map[string]any{"action": "recover", "subject_id": readerID, "expected_revision": 1, "password": managedPassword}
	if n, _ := owner.call("POST", "/api/v1/access/accounts", command, map[string]string{"X-CSRF-Token": ""}); n != 403 {
		t.Fatalf("missing CSRF accepted %d", n)
	}
	if n, _ := owner.call("POST", "/api/v1/access/accounts", command, map[string]string{"Origin": "https://unrelated.invalid"}); n != 403 {
		t.Fatalf("cross-origin accepted %d", n)
	}
	before := query(`SELECT COALESCE(jsonb_agg(to_jsonb(g) ORDER BY id),'[]')::text FROM workbench_access_grants g`)
	n, raw := owner.call("POST", "/api/v1/access/accounts", command, nil)
	if n != 200 {
		t.Fatalf("native recover returned %d", n)
	}
	var link struct {
		URL string `json:"activation_link"`
	}
	if json.Unmarshal(raw, &link) != nil || link.URL == "" {
		t.Fatal("missing holder handoff")
	}
	u, e := url.Parse(link.URL)
	if e != nil || u.Fragment == "" || u.RawQuery != "" {
		t.Fatal("secret must be fragment-only")
	}
	if n, _ = reader.call("GET", "/api/v1/access/session", nil, nil); n != 401 {
		t.Fatal("recovery did not invalidate reader session")
	}
	if n, _ = owner.call("GET", "/api/v1/access/session", nil, nil); n != 200 {
		t.Fatal("owner session interrupted")
	}
	const replacement = "synthetic holder-selected API password"
	set := map[string]string{"token": u.Fragment, "password": replacement, "confirmation": replacement}
	if n, _ = reader.call("POST", "/api/v1/access/account-password", set, nil); n != 200 {
		t.Fatalf("holder password setup %d", n)
	}
	if n, _ = reader.call("POST", "/api/v1/access/account-password", set, nil); n != 400 {
		t.Fatal("replayed holder ticket accepted")
	}
	signed := newManagedHTTPClient(t, s, "reader", replacement)
	if n, _ = signed.call("GET", "/api/v1/access/session", nil, nil); n != 200 {
		t.Fatal("recovered reader session unavailable")
	}
	after := query(`SELECT COALESCE(jsonb_agg(to_jsonb(g) ORDER BY id),'[]')::text FROM workbench_access_grants g`)
	if before != after {
		t.Fatal("password recovery rewrote project grants")
	}
	if got := query(`SELECT count(*)::text FROM access_audit_records WHERE action='access.account.password_set'`); got != "1" {
		t.Fatal("missing exactly one completion audit")
	}
}
func TestManagedOwnerProjectReadPreviewAndDefaultDenyPostgres(t *testing.T) {
	s, _, readerID, query := startManagedAccountAPI(t)
	owner := newManagedHTTPClient(t, s, "owner", managedPassword)
	if n, _ := owner.call("GET", "/api/v1/runtime/current", nil, nil); n != 403 {
		t.Fatalf("ungranted owner read returned %d", n)
	}
	if n, _ := owner.call("GET", "/api/v1/access/accounts", nil, nil); n != 200 {
		t.Fatal("data denial removed management")
	}
	for _, path := range []string{"/api/v1/executions", "/api/v1/sources", "/api/v1/cases", "/api/v1/posture"} {
		if n, _ := owner.call("GET", path, nil, nil); n != 403 {
			t.Fatalf("ungranted managed-owner data bypass %s %d", path, n)
		}
	}
	before := query(`SELECT COALESCE(jsonb_agg(to_jsonb(g) ORDER BY id),'[]')::text FROM workbench_access_grants g WHERE subject_id='` + readerID + `'`)
	var options struct {
		Projects []struct {
			Source  string `json:"source_system_id"`
			Project string `json:"source_project_ref"`
		} `json:"projects"`
	}
	n, raw := owner.call("GET", "/api/v1/access/project-read-grants", nil, nil)
	if n != 200 || json.Unmarshal(raw, &options) != nil || len(options.Projects) == 0 {
		t.Fatalf("scope options returned %d", n)
	}
	scope := options.Projects[0]
	id := randomTestUUID(t)
	command := map[string]any{"grant_id": id, "subject_id": managedOwnerID, "source_system_id": scope.Source, "source_project_ref": scope.Project, "expected_revision": 0, "capabilities": []string{"details", "coverage", "summary", "source_context", "source_runtime"}, "check_only": true, "revoke": false, "password": ""}
	if n, _ = owner.call("POST", "/api/v1/access/project-read-grants", command, nil); n != 200 {
		t.Fatalf("owner scope preview %d", n)
	}
	if got := query(`SELECT count(*)::text FROM workbench_access_grants WHERE subject_id='` + managedOwnerID + `'`); got != "0" {
		t.Fatal("preview created grant")
	}
	command["check_only"] = false
	command["password"] = managedPassword
	if n, _ = owner.call("POST", "/api/v1/access/project-read-grants", command, nil); n != 200 {
		t.Fatalf("owner explicit grant %d", n)
	}
	if n, raw = owner.call("GET", "/api/v1/runtime/current?source_ids="+scope.Source+"&source_projects="+url.QueryEscape(scope.Project), nil, nil); n != 200 {
		t.Fatalf("explicitly scoped owner cannot read %d: %s", n, raw)
	}
	request := httpReadingRequest(scope.Source)
	request.Filters.SourceProjects = []string{scope.Project}
	n, raw = owner.call("POST", "/api/v1/workbench/readings", request, nil)
	var reading workbench.Reading
	if n != 201 || json.Unmarshal(raw, &reading) != nil {
		t.Fatalf("actual owner reading creation %d %s", n, raw)
	}
	n, raw = owner.call("GET", "/api/v1/workbench/readings/"+reading.ID+"/summary", nil, nil)
	var summary workbench.Summary
	if n != 200 || json.Unmarshal(raw, &summary) != nil || summary.RequestCount == 0 {
		t.Fatalf("owner actual project facts missing %d %s", n, raw)
	}
	if n, _ = owner.call("GET", "/api/v1/runtime/current?source_ids="+scope.Source+"&source_projects=outside-project", nil, nil); n != 403 {
		t.Fatal("cross-project read accepted")
	}
	if got := query(`SELECT allow_content::text||':'||source_project_ref FROM workbench_access_grants WHERE id='` + id + `'`); got != "false:"+scope.Project {
		t.Fatal("grant widened body/project scope")
	}
	if after := query(`SELECT COALESCE(jsonb_agg(to_jsonb(g) ORDER BY id),'[]')::text FROM workbench_access_grants g WHERE subject_id='` + readerID + `'`); before != after {
		t.Fatal("owner grant automatically copied or rewrote reader grant")
	}
	command["check_only"] = true
	command["expected_revision"] = 1
	command["source_project_ref"] = "*"
	if n, _ = owner.call("POST", "/api/v1/access/project-read-grants", command, nil); n != 400 {
		t.Fatal("wildcard project accepted")
	}
	command["source_project_ref"] = scope.Project
	command["capabilities"] = []string{"details", "coverage", "content"}
	if n, _ = owner.call("POST", "/api/v1/access/project-read-grants", command, nil); n != 400 {
		t.Fatal("body capability accepted")
	}
	command["capabilities"] = []string{"details", "coverage", "summary", "source_context", "source_runtime"}
	command["check_only"] = false
	command["revoke"] = true
	if n, _ = owner.call("POST", "/api/v1/access/project-read-grants", command, nil); n != 200 {
		t.Fatalf("explicit revoke %d", n)
	}
	if n, _ = owner.call("GET", "/api/v1/runtime/current", nil, nil); n != 403 {
		t.Fatalf("revoked owner still reads %d", n)
	}
	if n, _ = owner.call("GET", "/api/v1/access/accounts", nil, nil); n != 200 {
		t.Fatal("revoke removed management")
	}
	// Safe audit payloads contain scope/identity only, never the fixture password.
	if strings.Contains(query(`SELECT COALESCE(jsonb_agg(details),'[]')::text FROM access_audit_records`), managedPassword) {
		t.Fatal("audit leaked credentials")
	}
}

// An actually available copied body has a positive ordinary-reviewer control.
// Owner metadata access never substitutes for that independent content grant.
func TestManagedOwnerActualBodyReadDeniedPostgres(t *testing.T) {
	body := []byte(`{"synthetic":"account-boundary-private-body"}`)
	keys := reviewbody.KeyRing{ActiveID: "synthetic", Keys: map[string][]byte{"synthetic": bytes.Repeat([]byte{7}, 32)}}
	f := newScopedContentFixtureWithCustody(t, body, keys)
	seedScopedContentI01WithoutB(t, f)
	owner := db.NewWorkspaceStore(f.admin)
	// apiOffset advances only the reading cutoff past the projection commit.
	// Invitation redemption uses PostgreSQL's real clock and rejects a future
	// creation time, so control operations use the fixture's actual base time.
	now := f.base
	inv, _, err := owner.CreateInvitation(f.ctx, connectorcontrol.CreateInvitationInput{ActorSubject: scopedContentSubject, Config: connectorcontrol.InvitationConfig{DeploymentID: "xid_AAAAAAAAAAAAAAAAAAAAAA", XInsightOrigin: "https://xinsight.example", UniRouterInstallOrigin: "https://unirouter.example"}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.RedeemInvitation(f.ctx, connectorcontrol.RedeemInvitationInput{InvitationToken: inv.Token, ProjectID: "project.fixture", FinalEventScope: []string{connectorcontrol.CompletedEventType}, ConsentSHA256: sourcefacts.Hash([]byte("original-consent")), ConfirmedAt: now, ControlToken: "xib_" + strings.Repeat("A", 43), ControlTokenExpires: now.Add(time.Hour)}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.ExecContext(f.ctx, `UPDATE connector_installations SET status='connected',observed_state='connected',source_instance=$2,source_instance_sha256=$3 WHERE id=$1`, inv.InstallationID, f.collection.SourceInstance, sourcefacts.Hash([]byte(f.collection.SourceInstance))); err != nil {
		t.Fatal(err)
	}
	if err = owner.SaveManagedEnrollment(f.ctx, db.ManagedEnrollment{InstallationID: inv.InstallationID, EnrollmentID: "xise_1", SourceInstance: f.collection.SourceInstance, OrgID: f.collection.OrgID, ProjectID: f.collection.ProjectID, Revision: "1", ContentCollectionID: f.collection.ID, Status: "requires_migration", LegacyCCopyPending: true}, now); err != nil {
		t.Fatal(err)
	}
	ref := f.choice.Reference
	revision, err := f.consumer.RequestReviewCaptureDefault(f.ctx, db.ReviewCaptureSettings{Mode: "full_content", RetentionSeconds: 7200, ProjectQuotaBytes: 4096, MaxItemBytes: 1024}, 0, "xs_admin", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.consumer.ConfirmReviewCaptureDefault(f.ctx, revision, "xs_admin", now); err != nil {
		t.Fatal(err)
	}
	if _, err = f.consumer.EnsureManagedReviewCaptureProject(f.ctx, inv.InstallationID, f.collection.ID, f.sourceID, f.collection.SourceInstance, f.collection.OrgID, f.collection.ProjectID, now); err != nil {
		t.Fatal(err)
	}
	// Commit through the actual restricted workspace identity, as the copy worker
	// does. The owner connection is only for control-plane preparation/transition.
	if receipt, e := f.store.CommitURCReviewBody(f.ctx, f.collection.ID, ref, body, keys, now); e != nil || receipt.Outcome != "accepted" {
		t.Fatalf("restricted copy acceptance: %+v %v", receipt, e)
	}
	if err = owner.FinishLegacyCCustodyTransition(f.ctx, f.collection.ID, now); err != nil {
		t.Fatal(err)
	}
	if managed, e := f.store.IsManagedCustodyCollection(f.ctx, f.collection.ID); e != nil || !managed {
		t.Fatal("custody did not transition", e)
	}

	hash, err := access.DerivePasswordHash([]byte(managedPassword))
	if err != nil {
		t.Fatal(err)
	}
	root := db.NewLocalAccountStore(f.admin)
	err = root.Initialize(f.ctx, access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{
		{SubjectID: managedOwnerID, Username: "owner", Role: access.RolePlatformAdmin, PasswordHash: hash},
		{SubjectID: scopedContentSubject, Username: "body.reader", Role: access.RoleScopedReviewer, PasswordHash: hash}}}, "synthetic-body-v3")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := access.NewDurableProvider(f.store.LocalAccounts())
	if err != nil {
		t.Fatal(err)
	}
	service, err := access.NewService(provider, nil, access.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	checks, err := db.LoadMigrationChecksums("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(api.NewHandlerWithDependencies(api.HandlerDependencies{Store: f.reader, WorkspaceStore: f.store, AgentAccessStore: f.store, ExpectedMigrations: checks, AccessService: service, ReviewBodyKeys: keys, Now: func() time.Time { return f.base.Add(time.Second) }}))
	defer server.Close()
	manager := newManagedHTTPClient(t, server, "owner", managedPassword)
	reviewer := newManagedHTTPClient(t, server, "body.reader", managedPassword)
	scopeHeader := func(c *managedHTTPClient) map[string]string {
		t.Helper()
		n, raw := c.call("GET", "/api/v1/access/session", nil, nil)
		var session struct {
			Scope string `json:"scope_revision"`
		}
		if n != 200 || json.Unmarshal(raw, &session) != nil || session.Scope == "" {
			t.Fatalf("scope response %d %s", n, raw)
		}
		return map[string]string{"X-XInsight-Scope-Revision": session.Scope}
	}
	path := "/api/v1/work-activity/contents/" + url.PathEscape(ref.ContentRef)
	if n, raw := reviewer.call("GET", path, nil, scopeHeader(reviewer)); n != 200 || !bytes.Equal(raw, body) {
		t.Fatalf("actual body positive control %d %s", n, raw)
	}
	command := map[string]any{"grant_id": randomTestUUID(t), "subject_id": managedOwnerID, "source_system_id": f.sourceID, "source_project_ref": "project-" + f.collection.ProjectID, "expected_revision": 0, "capabilities": []string{"summary", "details", "coverage", "source_context", "source_runtime"}, "password": managedPassword, "check_only": false, "revoke": false}
	if n, raw := manager.call("POST", "/api/v1/access/project-read-grants", command, nil); n != 200 {
		t.Fatalf("explicit metadata scope %d %s", n, raw)
	}
	f.resetSource()
	if n, raw := manager.call("GET", path, nil, scopeHeader(manager)); n != 404 || bytes.Contains(raw, body) {
		t.Fatalf("metadata owner body boundary %d %s", n, raw)
	}
	f.assertSource(t, 0, 0)
	if n, _ := manager.call("GET", "/api/v1/access/accounts", nil, nil); n != 200 {
		t.Fatal("body denial removed management")
	}
	var denied int
	if err = f.admin.QueryRowContext(f.ctx, `SELECT count(*) FROM access_audit_records WHERE actor_ref=$1 AND result='denied' AND action LIKE '%body%'`, managedOwnerID).Scan(&denied); err != nil || denied != 1 {
		t.Fatalf("body denial audit %d %v", denied, err)
	}
}

// Opt-in, disposable PG + real TLS/embedded-UI harness for browser review.
// No account on an installed product is accessed or changed.
func TestManagedAccountBrowserHarness(t *testing.T) {
	if os.Getenv("XI_ACCOUNTS_BROWSER_HARNESS") != "1" {
		t.Skip("explicit local browser qualification only")
	}
	output := os.Getenv("XI_ACCOUNTS_BROWSER_OUTPUT")
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		t.Fatal("exact browser output path required")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	server, _, _, _ := startManagedAccountAPI(t)
	raw, err := json.Marshal(map[string]string{"url": server.URL, "username": "owner", "password": managedPassword})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(output, "fixture.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("isolated TLS account harness ready")
	deadline := time.NewTimer(10 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("bounded browser qualification expired")
		case <-tick.C:
			if _, err = os.Stat(filepath.Join(output, "stop")); err == nil {
				return
			}
		}
	}
}
