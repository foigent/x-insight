package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
)

// This is the UR producer's JSON shape, deliberately not marshaled from the
// consumer struct: doing that hid missing nested fields in the first driver.
const producerInput = `{
 "managed_mode":"new_install","managed_future_projects":true,"managed_normal_setup":true,
 "xi_control_origin":"https://127.0.0.1:9443","receiver_network_binding":{},
 "receiver_allowed_ip":"198.18.0.2/32","profiles":["a","b","c","d","e","f","g","h"],
 "base_url":"https://198.18.0.2:9444","tls_ca_pem":"synthetic",
 "source_instance":"synthetic-source-control","org_id":"1","project_id":"2","foreign_project_id":"3",
 "postgres_dsn":"","lifecycle_nonce":"0000000000000000000000000000000000000000000000000000000000000000",
 "expected_content":{"request_id":"synthetic","sha256":"synthetic"},
 "installed":{
  "schema_version":"xinsight.ci-installed-joint.v1","mode":"new_install","future_project":true,
  "xi_origin":"https://127.0.0.1:9443","xi_deployment_id":"xid_synthetic",
  "ur_listen":"198.18.0.2:9444","receiver_allowed_ip":"198.18.0.2/32",
  "tls_cert_file":"/private/ur.pem","tls_key_file":"/private/ur.key","tls_ca_file":"/private/ur-ca.pem",
  "xi_ca_file":"/private/xi-ca.pem","xi_admin_username":"joint.admin","xi_admin_password_file":"/private/admin.password",
  "xi_reader_username":"joint.reader","xi_reader_password_file":"/private/reader.password",
  "xi_reader_subject_id":"xs_synthetic","xi_admin_dsn_file":"/private/admin.dsn","timeout_seconds":300
 }
}`

func TestDecodeURProducerInput(t *testing.T) {
	if _, err := decodeInput(strings.NewReader(producerInput)); err != nil {
		t.Fatal("valid UR producer input rejected:", err)
	}
	for _, tc := range []struct{ name, old, new string }{
		{"mode mismatch", `"mode":"new_install"`, `"mode":"adopt_existing"`},
		{"future mismatch", `"future_project":true`, `"future_project":false`},
		{"unknown field", `"mode":"new_install"`, `"mode":"new_install","unexpected":true`},
		{"missing mode", `"mode":"new_install",`, ``},
		{"missing lifecycle seam", `"lifecycle_nonce":"0000000000000000000000000000000000000000000000000000000000000000",`, ``},
		{"shared UR database", `"postgres_dsn":""`, `"postgres_dsn":"synthetic-not-allowed"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeInput(strings.NewReader(strings.Replace(producerInput, tc.old, tc.new, 1))); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	if _, err := decodeInput(strings.NewReader(producerInput + `{}`)); err == nil {
		t.Fatal("second JSON document accepted")
	}
}

func TestFPV17UpgradeRequestSurvivesURSanitizedChildEnvironment(t *testing.T) {
	private := filepath.Join(t.TempDir(), "xinsight-fpv15-joint-private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	adminDSN := filepath.Join(private, "xi-admin.dsn")
	request := fpv17UpgradeRequest{
		SchemaVersion:     "xinsight.ci-fpv17-installed-upgrade-request.v1",
		Script:            filepath.Join(private, "qualify-ubuntu22-fpv17-upgrade.sh"),
		CandidateArtifact: "/opt/xinsight-ci-artifacts/candidate.tar.gz",
		CandidatePayload:  filepath.Join(filepath.Dir(private), "fresh-install-payload"),
	}
	if got, err := readFPV17UpgradeRequest(adminDSN); err != nil || got != nil {
		t.Fatal("existing lanes must remain unchanged without request", err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(private, "fpv17-upgrade-request.json")
	if err := os.WriteFile(requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestFPV17UpgradeRequestSanitizedChild$")
	child.Env = []string{"PATH=" + os.Getenv("PATH"), "GITHUB_ACTIONS=true", "XINSIGHT_CI_EPHEMERAL_HOST=1", "XINSIGHT_CI_INSTALLED_JOINT=1"}
	child.Stdin = strings.NewReader(strings.Replace(producerInput, "/private/admin.dsn", adminDSN, 1))
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("sanitized UR child lost upgrade request: %v: %s", err, output)
	}
	if err := os.Chmod(requestPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readFPV17UpgradeRequest(adminDSN); err == nil {
		t.Fatal("loose request permissions accepted")
	}
}

func TestFPV17UpgradeRequestSanitizedChild(t *testing.T) {
	if os.Getenv("XINSIGHT_CI_INSTALLED_JOINT") != "1" {
		return
	}
	input, err := decodeInput(os.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	request, err := readFPV17UpgradeRequest(input.Installed.XIAdminDSNFile)
	if err != nil || request == nil || request.SchemaVersion != "xinsight.ci-fpv17-installed-upgrade-request.v1" {
		t.Fatal("fixture request not available through scrubbed child environment", err)
	}
}

func TestFPV17UpgradeEstablishesURAuthorityWithoutChangingCredential(t *testing.T) {
	const installation = "xci_0000000000000000000000"
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	expires := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		name, wantError string
		before, after   lifecycleState
		wantActions     []string
	}{
		{"valid pause and resume", "",
			lifecycleState{Paused: true, CurrentTokenSHA256: digest, CurrentExpiresAt: expires},
			lifecycleState{CurrentTokenSHA256: digest, CurrentExpiresAt: expires},
			[]string{"pause", "resume"}},
		{"missing original credential", "upgrade_authority_prestate_invalid",
			lifecycleState{Paused: true, CurrentExpiresAt: expires}, lifecycleState{}, []string{"pause"}},
		{"credential changed on resume", "upgrade_authority_resume_invalid",
			lifecycleState{Paused: true, CurrentTokenSHA256: digest, CurrentExpiresAt: expires},
			lifecycleState{CurrentTokenSHA256: strings.Repeat("b", 64), CurrentExpiresAt: expires},
			[]string{"pause", "resume"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &driver{installationID: installation}
			d.input.BaseURL = "https://fixture.invalid"
			d.input.LifecycleNonce = "synthetic-nonce"
			var actions []string
			d.client = &http.Client{Transport: diagnosticTransport(func(r *http.Request) (*http.Response, error) {
				var request lifecycleRequest
				if r.Method != http.MethodPost || r.URL.Path != "/__joint/credential-lifecycle" ||
					r.Header.Get("X-Joint-Lifecycle-Nonce") != d.input.LifecycleNonce ||
					json.NewDecoder(r.Body).Decode(&request) != nil || request.InstallationID != installation ||
					request.ExpiresAt != nil || request.DropResponse != "" {
					t.Fatal("upgrade authority used an unexpected fixture request")
				}
				actions = append(actions, request.Action)
				state := tc.before
				if request.Action == "resume" {
					state = tc.after
				}
				body, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
			})}
			err := d.establishUpgradeAuthority(context.Background())
			if tc.wantError == "" && err != nil || tc.wantError != "" && (err == nil || err.Error() != tc.wantError) {
				t.Fatalf("authority result=%v, want=%q", err, tc.wantError)
			}
			if strings.Join(actions, ",") != strings.Join(tc.wantActions, ",") {
				t.Fatalf("fixture actions=%v, want=%v", actions, tc.wantActions)
			}
		})
	}
}

type diagnosticTransport func(*http.Request) (*http.Response, error)

func (f diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLifecycleControllerReadinessRequiresItsOwnValidResponse(t *testing.T) {
	const installation = "xci_0000000000000000000000"
	valid := `{"schema_version":"` + connectorcontrol.SetupStatusSchema + `","installation_id":"` + installation + `","state":"needs_action","issues":[{"code":"control_credential_expired","requires_choice":true}]}`
	for _, tc := range []struct {
		name, firstBody, secondBody, want string
		firstStatus, wantCalls            int
		cancel                            bool
	}{
		{"ready but expired", valid, "", "", 200, 1, false},
		{"controller starts after web", `{"code":"connector_control_unavailable"}`, valid, "", 503, 2, false},
		{"still unavailable", `{"code":"connector_control_unavailable"}`, "", "lifecycle_controller_not_ready", 503, 1, true},
		{"permission denied", `{"code":"permission_denied"}`, "", "lifecycle_controller_status_unavailable", 403, 1, false},
		{"session expired", `{"code":"unauthenticated"}`, "", "lifecycle_controller_status_unavailable", 401, 1, false},
		{"unwired route", `{}`, "", "lifecycle_controller_status_unavailable", 404, 1, false},
		{"other server failure", `{"code":"connector_control_unavailable"}`, "", "lifecycle_controller_status_unavailable", 500, 1, false},
		{"unknown unavailable", `{"code":"other_failure"}`, "", "lifecycle_controller_status_unavailable", 503, 1, false},
		{"invalid JSON", `{`, "", "lifecycle_controller_status_unavailable", 200, 1, false},
		{"wrong installation", strings.Replace(valid, installation, "xci_1111111111111111111111", 1), "", "lifecycle_controller_status_invalid", 200, 1, false},
		{"wrong schema", strings.Replace(valid, connectorcontrol.SetupStatusSchema, "unrecognized.v1", 1), "", "lifecycle_controller_status_invalid", 200, 1, false},
		{"wrong state", strings.Replace(valid, "needs_action", "unrecognized", 1), "", "lifecycle_controller_status_invalid", 200, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			d := &driver{installationID: installation, adminCookie: &http.Cookie{Name: "session", Value: "synthetic"}}
			d.input.XIControlOrigin = "https://synthetic.invalid"
			calls := 0
			d.client = &http.Client{Transport: diagnosticTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/api/v1/connector-setups/"+installation {
					t.Fatal("readiness did not use the actual read-only setup endpoint")
				}
				if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "synthetic" {
					t.Fatal("readiness did not retain the original human session")
				}
				code, body := tc.firstStatus, tc.firstBody
				if calls > 1 && tc.secondBody != "" {
					code, body = 200, tc.secondBody
				}
				if tc.cancel {
					cancel()
				}
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			err := d.awaitLifecycleController(ctx)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || err.Error() != tc.want) {
				t.Fatalf("readiness error=%v, want=%q", err, tc.want)
			}
			if calls != tc.wantCalls {
				t.Fatalf("readiness requests=%d, want=%d", calls, tc.wantCalls)
			}
			if tc.want == "" && d.failureHTTPStatus != 0 {
				t.Fatal("valid response retained stale HTTP failure")
			}
		})
	}
}

func TestLifecycleControllerReadinessDoesNotWaitForWholeQualification(t *testing.T) {
	d := &driver{installationID: "xci_0000000000000000000000"}
	d.input.XIControlOrigin = "https://synthetic.invalid"
	var requestDeadline time.Time
	d.client = &http.Client{Transport: diagnosticTransport(func(r *http.Request) (*http.Response, error) {
		requestDeadline, _ = r.Context().Deadline()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	started := time.Now()
	if err := d.awaitLifecycleController(ctx); err == nil {
		t.Fatal("unavailable controller accepted")
	}
	if requestDeadline.IsZero() || requestDeadline.Sub(started) > 2100*time.Millisecond {
		t.Fatal("readiness inherited the whole qualification deadline")
	}
}

func TestLifecycleControllerReadinessPersistent503FailsAtDeadline(t *testing.T) {
	d := &driver{installationID: "xci_0000000000000000000000"}
	d.input.XIControlOrigin = "https://synthetic.invalid"
	d.client = &http.Client{Transport: diagnosticTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"code":"connector_control_unavailable"}`)), Header: make(http.Header)}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := d.awaitLifecycleController(ctx); err == nil || err.Error() != "lifecycle_controller_not_ready" {
		t.Fatalf("persistent failure was not rejected: %v", err)
	}
	if d.failureHTTPStatus != 503 || d.failureHTTPProblem != "connector_control_unavailable" {
		t.Fatal("startup deadline discarded the final HTTP failure evidence")
	}
}

func TestHTTPFailureKeepsOperationAndOnlySafeProblemFields(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"known", `{"code":"connector_control_unavailable","title":"secret-token-password","fields":[{"name":"private-path"}]}`, "connector_control_unavailable"},
		{"UR known", `{"error":"setup_unavailable","message":"secret-token-password"}`, "setup_unavailable"},
		{"UR invalid input", `{"error":"invalid_setup"}`, "invalid_setup"},
		{"UR conflict", `{"error":"setup_changed"}`, "setup_changed"},
		{"UR denied", `{"error":"recovery_denied"}`, "recovery_denied"},
		{"UR missing", `{"error":"setup_not_found"}`, "setup_not_found"},
		{"UR private text", `{"error":"setup_unavailable: secret-token-password"}`, "unclassified"},
		{"ambiguous envelopes", `{"code":"connector_control_unavailable","error":"setup_unavailable"}`, "unclassified"},
		{"private code", `{"code":"connector_control_unavailable: secret-token-password"}`, "unclassified"},
		{"trailing body", `{"code":"connector_control_unavailable"} secret-token-password`, "unclassified"},
		{"not JSON", `secret-token-password`, "unclassified"},
		{"oversized", `{"code":"connector_control_unavailable","title":"` + strings.Repeat("x", 64<<10) + `"}`, "unclassified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &driver{start: time.Now(), stage: "credential_offline_expiry", stageStart: time.Now()}
			d.input.Installed.XIAdminDSNFile = filepath.Join(t.TempDir(), "unused.dsn")
			d.client = &http.Client{Transport: diagnosticTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			if err := d.call(context.Background(), "POST", "https://synthetic.invalid/private-path", nil, nil, "", nil); err == nil {
				t.Fatal("HTTP failure accepted")
			}
			_, _, err := d.fail("lifecycle_xi_human_recovery")
			if err == nil || err.Error() != "lifecycle_xi_human_recovery" {
				t.Fatalf("precise operation replaced by generic HTTP category: %v", err)
			}
			raw, err := os.ReadFile(filepath.Join(filepath.Dir(d.input.Installed.XIAdminDSNFile), "runtime-diagnostic.json"))
			if err != nil {
				t.Fatal(err)
			}
			var record struct {
				Status   int    `json:"failure_http_status"`
				Category string `json:"failure_http_category"`
				Problem  string `json:"failure_problem_code"`
			}
			if json.Unmarshal(raw, &record) != nil || record.Status != 503 || record.Category != "server_error" || record.Problem != tc.want {
				t.Fatalf("missing safe HTTP evidence: %+v", record)
			}
			for _, secret := range []string{"secret-token-password", "private-path", "synthetic.invalid"} {
				if strings.Contains(string(raw), secret) {
					t.Fatal("private HTTP data entered diagnostic")
				}
			}
			d.client.Transport = diagnosticTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})
			if err = d.call(context.Background(), "POST", "https://synthetic.invalid/", nil, nil, "", nil); err != nil {
				t.Fatal(err)
			}
			d.input.Installed.XIAdminDSNFile = filepath.Join(t.TempDir(), "unused.dsn")
			d.writeRuntimeDiagnostic()
			raw, err = os.ReadFile(filepath.Join(filepath.Dir(d.input.Installed.XIAdminDSNFile), "runtime-diagnostic.json"))
			if err != nil || strings.Contains(string(raw), "failure_http_") || strings.Contains(string(raw), "failure_problem_code") {
				t.Fatal("successful later request retained stale HTTP failure")
			}
		})
	}
}

func TestDecodeAdoptInput(t *testing.T) {
	var wire map[string]any
	if err := json.Unmarshal([]byte(producerInput), &wire); err != nil {
		t.Fatal(err)
	}
	wire["managed_mode"], wire["managed_future_projects"] = "adopt_existing", false
	nested := wire["installed"].(map[string]any)
	nested["mode"], nested["future_project"] = "adopt_existing", false
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeInput(strings.NewReader(string(raw))); err != nil {
		t.Fatal(err)
	}
}

func TestOperationDiagnosticNeverExportsPrivateFields(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"source_authorization_denied", "source_authorization_denied"},
		{"", "none"},
		{"source_unavailable: secret-token-password", "unclassified"},
		{"postgres://user:secret-token-password@127.0.0.1/db", "unclassified"},
	} {
		if got := safeContentFailure(tc.raw); got != tc.want {
			t.Fatalf("content diagnostic=%q, want=%q", got, tc.want)
		}
	}
	for _, tc := range []struct{ raw, want string }{
		{"custody_loading", "custody_loading"},
		{"", "none"},
		{"custody_loading: secret-token-password", "unclassified"},
		{"postgres://user:secret-token-password@127.0.0.1/db", "unclassified"},
	} {
		if got := safeRuntimeEnum(tc.raw, "custody_loading"); got != tc.want {
			t.Fatalf("runtime diagnostic=%q, want=%q", got, tc.want)
		}
	}
	for _, tc := range []struct{ raw, want string }{
		{"original receiver activate: paused authentication requires effective controller target network: secret-token-password", "controller_network_unready"},
		{"original receiver activate: secret-token-password", "original_receiver_activation_failed"},
		{"unsupported active systemd service drop-in: secret-token-password.conf", "unsupported_service_dropin"},
		{"private backup metadata is unsafe: secret-token-password", "private_backup_preflight_failed"},
		{"postgres://user:secret-token-password@127.0.0.1/db", "private_command_failed"},
	} {
		if got := safePreflightFailure([]byte(tc.raw)); got != tc.want {
			t.Fatalf("preflight diagnostic=%q, want=%q", got, tc.want)
		}
	}
	for _, tc := range []struct{ raw, want string }{
		{`{"active_journal":{"kind":"backup","status":"failed","error_code":"writer_sessions_present","private":"secret-token-password"}}`, "writer_sessions_present"},
		{`{"active_journal":{"kind":"backup","status":"failed","error_code":"secret-token-password"}}`, "operation_failed"},
		{`{"active_journal":{"kind":"restore","status":"failed","error_code":"writer_sessions_present"}}`, "operation_diagnostic_unavailable"},
		{`{"active_journal":{"kind":"backup","status":"completed","error_code":"writer_sessions_present"}}`, "operation_diagnostic_unavailable"},
		{`{"active_journal":null}`, "operation_diagnostic_unavailable"},
		{`secret-token-password`, "operation_diagnostic_unavailable"},
	} {
		if got := safeOperationFailure([]byte(tc.raw), "backup", nil); got != tc.want {
			t.Fatalf("safe diagnostic=%q, want=%q", got, tc.want)
		}
	}
	status := []byte(`{"active_journal":{"kind":"backup","status":"failed","error_code":"backup_service_restore_failed"}}`)
	if got := safeOperationFailure(status, "backup", []byte("backup_service_restore_failed: lstat /run/x-insight/connector-controller.sock: no such file or directory")); got != "backup_controller_socket_not_ready" {
		t.Fatal("known socket readiness failure was not classified", got)
	}
	if got := safeOperationFailure(status, "backup", []byte("secret-token-password")); got != "backup_service_restore_failed" {
		t.Fatal("unknown private detail changed safe category", got)
	}
	for _, tc := range []struct{ kind, code string }{
		{"upgrade", "database_role_boundary_failed"},
		{"restore", "restored_database_role_boundary_failed"},
	} {
		raw := []byte(`{"active_journal":{"kind":"` + tc.kind + `","status":"failed","error_code":"` + tc.code + `","private":"secret-token-password"}}`)
		if got := safeOperationFailure(raw, tc.kind, []byte("secret-token-password")); got != tc.code {
			t.Fatalf("role boundary failure lost its safe category: %q", got)
		}
		if got := safeOperationFailure(raw, "backup", nil); got != "operation_diagnostic_unavailable" {
			t.Fatalf("wrong operation accepted: %q", got)
		}
	}
}
