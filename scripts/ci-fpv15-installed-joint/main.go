// ci-fpv15-installed-joint is a disposable-Linux qualification child. It
// drives installed XI only through its HTTPS human API and observes the real
// source worker through its isolated PostgreSQL database. It does not create
// connector phases, receipts, loaded proofs or systemd state.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/sourcefacts"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type installedInput struct {
	ManagedMode            string          `json:"managed_mode"`
	FutureProject          bool            `json:"managed_future_projects"`
	NormalSetup            bool            `json:"managed_normal_setup"`
	XIControlOrigin        string          `json:"xi_control_origin"`
	ReceiverNetworkBinding json.RawMessage `json:"receiver_network_binding"`
	ReceiverAllowedIP      string          `json:"receiver_allowed_ip"`
	Profiles               []string        `json:"profiles"`
	BaseURL                string          `json:"base_url"`
	TLSCAPEM               string          `json:"tls_ca_pem"`
	SourceInstance         string          `json:"source_instance"`
	OrgID                  string          `json:"org_id"`
	ProjectID              string          `json:"project_id"`
	ForeignProjectID       string          `json:"foreign_project_id"`
	PostgresDSN            string          `json:"postgres_dsn"`
	LifecycleNonce         string          `json:"lifecycle_nonce"`
	ExpectedContent        struct {
		RequestID string `json:"request_id"`
		SHA256    string `json:"sha256"`
	} `json:"expected_content"`
	Installed struct {
		SchemaVersion        string `json:"schema_version"`
		Mode                 string `json:"mode"`
		FutureProject        bool   `json:"future_project"`
		XIOrigin             string `json:"xi_origin"`
		XIDeploymentID       string `json:"xi_deployment_id"`
		URListen             string `json:"ur_listen"`
		ReceiverAllowedIP    string `json:"receiver_allowed_ip"`
		TLSCertFile          string `json:"tls_cert_file"`
		TLSKeyFile           string `json:"tls_key_file"`
		TLSCAFile            string `json:"tls_ca_file"`
		XICAFile             string `json:"xi_ca_file"`
		XIAdminUsername      string `json:"xi_admin_username"`
		XIAdminPasswordFile  string `json:"xi_admin_password_file"`
		XIReaderUsername     string `json:"xi_reader_username"`
		XIReaderPasswordFile string `json:"xi_reader_password_file"`
		XIReaderSubjectID    string `json:"xi_reader_subject_id"`
		XIAdminDSNFile       string `json:"xi_admin_dsn_file"`
		TimeoutSeconds       int    `json:"timeout_seconds"`
	} `json:"installed"`
}

type fpv17UpgradeRequest struct {
	SchemaVersion     string `json:"schema_version"`
	Script            string `json:"script"`
	CandidateArtifact string `json:"candidate_artifact"`
	CandidatePayload  string `json:"candidate_payload"`
}

type driver struct {
	input                   installedInput
	client                  *http.Client
	adminCookie             *http.Cookie
	readerCookie            *http.Cookie
	adminCSRF               string
	db                      *sql.DB
	start                   time.Time
	stage                   string
	stageStart              time.Time
	originalHistory         string
	originalStreams         string
	originalBindings        string
	originalContentProgress string
	retiredState            string
	failureCause            string
	failureHTTPStatus       int
	failureHTTPProblem      string
	installationID          string
}

func main() {
	stage, elapsed, err := run()
	state, category := "passed", "none"
	if err != nil {
		state, category = "failed", err.Error()
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"schema_version": "xinsight.ci-installed-joint-result.v1", "state": state,
		"stage": stage, "elapsed_ms": elapsed.Milliseconds(), "failure_category": category,
	})
	if err != nil {
		os.Exit(1)
	}
}

func run() (string, time.Duration, error) {
	started := time.Now()
	if runtime.GOOS != "linux" || os.Getenv("GITHUB_ACTIONS") != "true" ||
		os.Getenv("XINSIGHT_CI_EPHEMERAL_HOST") != "1" || os.Getenv("XINSIGHT_CI_INSTALLED_JOINT") != "1" {
		return "guard", time.Since(started), errors.New("environment_guard")
	}
	input, err := decodeInput(os.Stdin)
	if err != nil {
		return "input", time.Since(started), errors.New("input_invalid")
	}
	upgradeRequest, err := readFPV17UpgradeRequest(input.Installed.XIAdminDSNFile)
	if err != nil || upgradeRequest != nil && input.ManagedMode != "new_install" {
		return "input", time.Since(started), errors.New("upgrade_request_invalid")
	}
	ca, err := os.ReadFile(input.Installed.XICAFile)
	if err != nil {
		return "trust", time.Since(started), errors.New("trust_unavailable")
	}
	urCA, err := os.ReadFile(input.Installed.TLSCAFile)
	if err != nil {
		return "trust", time.Since(started), errors.New("trust_unavailable")
	}
	issued, err := os.ReadFile(input.Installed.TLSCertFile)
	if err != nil || !bytes.Contains(issued, []byte(strings.TrimSpace(input.TLSCAPEM))) {
		return "trust", time.Since(started), errors.New("trust_pin_mismatch")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) || !roots.AppendCertsFromPEM(urCA) {
		return "trust", time.Since(started), errors.New("trust_invalid")
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, Timeout: 12 * time.Second}
	d := &driver{input: input, client: client, db: nil, start: started, stage: "database", stageStart: time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(input.Installed.TimeoutSeconds-5)*time.Second)
	defer cancel()
	dsnRaw, err := os.ReadFile(input.Installed.XIAdminDSNFile)
	if err != nil {
		return d.fail("database_unavailable")
	}
	dsn := strings.TrimSpace(string(dsnRaw))
	if dsn == "" {
		return d.fail("database_unavailable")
	}
	d.db, err = sql.Open("pgx", dsn)
	if err != nil {
		return d.fail("database_unavailable")
	}
	defer func() {
		if d.db != nil {
			_ = d.db.Close()
		}
	}()
	if d.db.PingContext(ctx) != nil {
		return d.fail("database_unavailable")
	}
	if d.seedFixture(ctx) != nil {
		return d.fail("fixture_seed_failed")
	}
	if input.ManagedMode == "adopt_existing" {
		d.advance("adopt_prestate")
		if err := d.prepareRestoredPrestate(ctx); err != nil {
			return d.fail("adopt_prestate_prepare_failed", err)
		}
		var pausedStreams, pausedContent int
		err = d.db.QueryRowContext(ctx, `SELECT count(*) FROM source_fact_streams WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND status='paused' AND requires_attention AND last_error='restore_requires_authorization'`,
			unirouter.DefaultSourceSystemID, input.SourceInstance, input.OrgID, input.ProjectID).Scan(&pausedStreams)
		if err != nil {
			return d.fail("adopt_prestate_unavailable")
		}
		err = d.db.QueryRowContext(ctx, `SELECT count(*) FROM source_content_collections WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND status='paused' AND requires_attention AND last_error='restore_requires_authorization'`,
			unirouter.DefaultSourceSystemID, input.SourceInstance, input.OrgID, input.ProjectID).Scan(&pausedContent)
		if err != nil || pausedStreams < 8 || pausedContent < 1 {
			return d.fail("adopt_prestate_incomplete")
		}
	}
	d.advance("xi_admin_login")
	d.adminCookie, d.adminCSRF, err = d.login(ctx, input.Installed.XIAdminUsername, input.Installed.XIAdminPasswordFile)
	if err != nil {
		return d.fail("xi_admin_login_failed")
	}
	d.advance("xi_setup_preview")
	var preview connectorcontrol.SetupPreview
	if d.call(ctx, "GET", input.XIControlOrigin+"/api/v1/connector-setups/preview", nil, d.adminCookie, "", &preview) != nil ||
		preview.DeploymentID != input.Installed.XIDeploymentID || preview.UROrigin != input.BaseURL {
		return d.fail("xi_setup_preview_failed")
	}
	d.advance("xi_setup_create")
	var created connectorcontrol.SetupCreated
	put := connectorcontrol.SetupConsentPut{SchemaVersion: connectorcontrol.SetupConsentSchema,
		RequestID: uuid(), ExpectedCustodyRevision: preview.Custody.DesiredRevision,
		Custody: &connectorcontrol.SetupCustody{Mode: "full_content", RetentionSeconds: 90 * 86400,
			ProjectQuotaBytes: 5 << 30, MaxItemBytes: 32 << 20}}
	if d.call(ctx, "POST", input.XIControlOrigin+"/api/v1/connector-setups", put, d.adminCookie, d.adminCSRF, &created) != nil ||
		created.Invitation.InstallationID == "" || created.Invitation.DeploymentID != input.Installed.XIDeploymentID {
		return d.fail("xi_setup_create_failed")
	}
	d.installationID = created.Invitation.InstallationID
	fragment, err := url.Parse(created.Invitation.InstallURL)
	if err != nil {
		return d.fail("xi_invitation_invalid")
	}
	values, err := url.ParseQuery(fragment.Fragment)
	if err != nil || values.Get("invitation") == "" || values.Get("xinsight_origin") != input.XIControlOrigin {
		return d.fail("xi_invitation_invalid")
	}
	d.advance("ur_setup_preview")
	base := input.BaseURL + "/api/v2/organizations/" + input.OrgID
	var urPreview map[string]json.RawMessage
	if d.call(ctx, "POST", base+"/xinsight/setup-previews", map[string]string{"project_id": input.ProjectID, "invitation_token": values.Get("invitation")}, nil, "", &urPreview) != nil {
		return d.fail("ur_setup_preview_failed")
	}
	var previewHash string
	var consent connectorcontrol.SetupConsentView
	var policyView urmachine.PolicyView
	if json.Unmarshal(urPreview["preview_sha256"], &previewHash) != nil || previewHash == "" ||
		json.Unmarshal(urPreview["xi_consent"], &consent) != nil || consent.ConsentSHA256 == "" ||
		json.Unmarshal(urPreview["ur_policy"], &policyView) != nil {
		return d.fail("ur_setup_preview_invalid")
	}
	var enterprise struct {
		PolicyRef string          `json:"policy_ref"`
		Explicit  bool            `json:"explicit"`
		Desired   json.RawMessage `json:"desired"`
		Effective json.RawMessage `json:"effective"`
	}
	if json.Unmarshal(policyView.Enterprise, &enterprise) != nil {
		return d.fail("ur_policy_preview_invalid")
	}
	var urPolicy any
	if len(enterprise.Desired) == 0 || bytes.Equal(enterprise.Desired, []byte("null")) {
		if enterprise.Explicit || enterprise.PolicyRef == "" || policyView.VersionVector == "" ||
			policyView.PolicyCapabilities == nil || len(policyView.PolicyCapabilities.SupportedProtocols) == 0 ||
			len(policyView.PolicyCapabilities.ObservationPoints) == 0 {
			return d.fail("ur_policy_preview_invalid")
		}
		urPolicy = map[string]any{"scope": "enterprise", "put": map[string]any{
			"schema_version": "unirouter.payload-policy.v2", "expected_policy_ref": enterprise.PolicyRef,
			"expected_version": "0", "expected_vector": `{"enterprise":0}`,
			"execution_storage_mode": "temporary_allowed", "project_quota_bytes": int64(5 << 30),
			"desired": map[string]any{"mode": "full_content", "retention_seconds": 30 * 86400,
				"quota_bytes": int64(5 << 30), "max_saved_bytes": 32 << 20,
				"supported_protocols": policyView.PolicyCapabilities.SupportedProtocols,
				"observation_points":  policyView.PolicyCapabilities.ObservationPoints,
				"allowed_purposes":    []string{"human_review"}, "on_limit": "record_capture_gap"}}}
	}
	d.advance("ur_setup_consent")
	consentRequest := map[string]any{"schema_version": "unirouter.xinsight.setup-consent.v1",
		"request_id": uuid(), "project_id": input.ProjectID, "invitation": created.Invitation,
		"expected_preview_sha256": previewHash, "expected_xi_consent_sha256": consent.ConsentSHA256,
		"receiver_mode": input.ManagedMode, "existing_binding_sha256": urPreview["existing_binding_sha256"],
		"coverage": "all_current_future", "excluded_project_ids": []string{}, "profiles": input.Profiles,
		"include_content": true, "policy_access": []map[string]any{
			{"target_kind": "enterprise_default", "coverage": "exact", "read": true, "write": true},
			{"target_kind": "project_policy", "coverage": "all_current_future", "read": true, "write": true}}, "ur_policy": urPolicy}
	var accepted map[string]json.RawMessage
	if d.call(ctx, "POST", base+"/xinsight/setups", consentRequest, nil, "", &accepted) != nil {
		return d.fail("ur_setup_consent_failed")
	}
	if input.ManagedMode == "adopt_existing" {
		d.advance("old_runtime_failure")
		if err := d.upgradeBlockedPredecessor(ctx); err != nil {
			return d.fail("predecessor_upgrade_failed", err)
		}
	}
	// For adoption, exclude successful polls by the frozen predecessor: this
	// timestamp is taken only after the candidate upgrade has returned.
	feedReadAfter := time.Now().UTC()
	d.advance("worker_content")
	var contentRef string
	if d.await(ctx, func() (bool, error) {
		err := d.db.QueryRowContext(ctx, `SELECT content_ref FROM review_body_operations WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND digest=$5 AND state='accepted' LIMIT 1`,
			unirouter.DefaultSourceSystemID, input.SourceInstance, input.OrgID, input.ProjectID, "sha256:"+input.ExpectedContent.SHA256).Scan(&contentRef)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return contentRef != "", err
	}) != nil {
		return d.fail("worker_content_unavailable")
	}
	d.advance("economic_feed_read")
	if d.awaitEconomicFeedRead(ctx, feedReadAfter) != nil {
		return d.fail("economic_feed_unavailable")
	}
	d.advance("xi_reader_login")
	d.readerCookie, _, err = d.login(ctx, input.Installed.XIReaderUsername, input.Installed.XIReaderPasswordFile)
	if err != nil {
		return d.fail("xi_reader_login_failed")
	}
	d.advance("xi_body_read")
	if err = d.readSavedBody(ctx, contentRef, input.ExpectedContent.SHA256); err != nil {
		return d.fail("xi_body_mismatch")
	}
	d.advance("setup_status")
	if d.await(ctx, func() (bool, error) {
		var status connectorcontrol.SetupStatus
		if err := d.call(ctx, "GET", input.XIControlOrigin+"/api/v1/connector-setups/"+created.Invitation.InstallationID, nil, d.adminCookie, "", &status); err != nil {
			return false, err
		}
		if status.InstallationID != created.Invitation.InstallationID || status.DeploymentID != input.Installed.XIDeploymentID {
			return false, errors.New("identity")
		}
		if status.State != "ready" || !status.Custody.Loaded || status.Receiving.ContentReceivedAt == nil || status.Receiving.ContentReadVerifiedAt == nil {
			return false, nil
		}
		for _, profile := range []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.RuntimeProfile} {
			if !slices.Contains(status.Receiving.ProfilesReceived, profile) {
				return false, nil
			}
		}
		return true, nil
	}) != nil {
		return d.fail("setup_not_ready")
	}
	if input.FutureProject {
		d.advance("future_project")
		orgID, parseErr := strconv.ParseInt(input.OrgID, 10, 64)
		if parseErr != nil || orgID < 1 {
			return d.fail("future_project_input_invalid")
		}
		var createdProject struct {
			ID    int64 `json:"id"`
			OrgID int64 `json:"org_id"`
		}
		if d.call(ctx, "POST", input.BaseURL+"/api/v1/projects", map[string]any{"org_id": orgID, "name": "Joint future project", "slug": "joint-future-" + strings.ReplaceAll(uuid(), "-", "")}, nil, "", &createdProject) != nil || createdProject.ID < 1 || createdProject.OrgID != orgID {
			return d.fail("future_project_create_failed")
		}
		if d.await(ctx, func() (bool, error) {
			var count int
			err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM source_fact_streams WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND status='active'`,
				unirouter.DefaultSourceSystemID, input.SourceInstance, input.OrgID, fmt.Sprint(createdProject.ID)).Scan(&count)
			if err != nil || count != 8 {
				return false, err
			}
			var received int
			err = d.db.QueryRowContext(ctx, `SELECT count(*) FROM source_fact_streams WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND profile=$5 AND last_received_at IS NOT NULL AND last_error=''`,
				unirouter.DefaultSourceSystemID, input.SourceInstance, input.OrgID, fmt.Sprint(createdProject.ID), sourcefacts.ContextProfile).Scan(&received)
			return received == 1, err
		}) != nil {
			return d.fail("future_project_discovery_failed")
		}
	}
	d.advance("service_restart")
	if d.restartAndVerify(ctx, contentRef) != nil {
		return d.fail("restart_continuity_failed")
	}
	if input.ManagedMode == "adopt_existing" {
		d.advance("preserved_history")
		if d.verifyAdoptedHistory(ctx) != nil {
			return d.fail("adopted_history_changed")
		}
	}
	// Keep the UR fixture alive across the formal host upgrade. Credential
	// revocation belongs to the other lanes and would destroy this prestate.
	if upgradeRequest != nil {
		if input.ManagedMode != "new_install" {
			return d.fail("input_invalid")
		}
		d.advance("upgrade_authority")
		if err := d.establishUpgradeAuthority(ctx); err != nil {
			return d.fail("upgrade_authority_failed")
		}
		d.advance("formal_upgrade")
		if err := d.db.Close(); err != nil {
			return d.fail("upgrade_observer_close_failed")
		}
		d.db = nil
		script, artifact, payload := upgradeRequest.Script, upgradeRequest.CandidateArtifact, upgradeRequest.CandidatePayload
		log, err := os.OpenFile(filepath.Join(filepath.Dir(d.input.Installed.XIAdminDSNFile), "fpv17-upgrade-private.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return d.fail("upgrade_private_log_unavailable")
		}
		command := exec.CommandContext(ctx, "bash", script, artifact, payload)
		command.Env = append(os.Environ(), "RUNNER_TEMP="+filepath.Dir(filepath.Dir(input.Installed.XIAdminDSNFile)))
		command.Stdout, command.Stderr = log, log
		err = command.Run()
		if closeErr := log.Close(); closeErr != nil {
			return d.fail("upgrade_private_log_failed")
		}
		if err != nil {
			return d.fail("formal_upgrade_failed")
		}
		if err := d.openInstalledDatabase(ctx, false); err != nil {
			return d.fail("upgrade_database_unavailable")
		}
		d.advance("upgrade_continuity")
		feedSince := time.Now().UTC()
		if err := d.await(ctx, func() (bool, error) {
			cookie, _, loginErr := d.login(ctx, d.input.Installed.XIReaderUsername, d.input.Installed.XIReaderPasswordFile)
			if loginErr != nil {
				return false, nil
			}
			d.readerCookie = cookie
			return true, nil
		}); err != nil {
			return d.fail("upgrade_reader_login_failed")
		}
		if err := d.readSavedBody(ctx, contentRef, d.input.ExpectedContent.SHA256); err != nil {
			return d.fail("upgrade_original_body_unavailable")
		}
		published, err := d.lifecycleFixture(ctx, "publish", nil, "")
		if err != nil || published.RequestID == "" || len(published.SHA256) != 64 {
			return d.fail("upgrade_publish_failed")
		}
		var newRef string
		if err := d.await(ctx, func() (bool, error) {
			var facts int
			err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM source_fact_documents WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND request_id=$5`, unirouter.DefaultSourceSystemID, d.input.SourceInstance, d.input.OrgID, d.input.ProjectID, published.RequestID).Scan(&facts)
			if err != nil || facts == 0 {
				return false, err
			}
			err = d.db.QueryRowContext(ctx, `SELECT content_ref FROM review_body_operations WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND digest=$5 AND state='accepted' LIMIT 1`, unirouter.DefaultSourceSystemID, d.input.SourceInstance, d.input.OrgID, d.input.ProjectID, "sha256:"+published.SHA256).Scan(&newRef)
			if errors.Is(err, sql.ErrNoRows) {
				return false, nil
			}
			return newRef != "", err
		}); err != nil || d.readSavedBody(ctx, newRef, published.SHA256) != nil {
			return d.fail("upgrade_new_source_unavailable")
		}
		if err := d.awaitEconomicFeedRead(ctx, feedSince); err != nil {
			return d.fail("upgrade_economic_feed_unavailable")
		}
		d.advance("upgrade_completed")
		d.emitStage("passed")
		return d.stage, time.Since(started), nil
	}
	if err := d.verifyCredentialLifecycle(ctx, contentRef); err != nil {
		return d.fail(err.Error())
	}
	d.emitStage("passed")
	return d.stage, time.Since(started), nil
}

type predecessorInputs struct {
	SchemaVersion     string `json:"schema_version"`
	Helper            string `json:"helper"`
	CandidateCTL      string `json:"candidate_ctl"`
	CandidateArtifact string `json:"candidate_artifact"`
}

func (d *driver) predecessorInputs() (predecessorInputs, error) {
	var inputs predecessorInputs
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(d.input.Installed.XIAdminDSNFile), "predecessor-inputs.json"))
	if err != nil || json.Unmarshal(raw, &inputs) != nil || inputs.SchemaVersion != "xinsight.ci-fpv14-predecessor.v1" {
		return inputs, errors.New("predecessor_input_unavailable")
	}
	for _, path := range []string{inputs.Helper, inputs.CandidateCTL, inputs.CandidateArtifact} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return inputs, errors.New("predecessor_input_invalid")
		}
	}
	return inputs, nil
}

func (d *driver) privateCommand(ctx context.Context, name string, stdin []byte, args ...string) ([]byte, error) {
	d.advance(strings.ReplaceAll(name, "-", "_"))
	d.failureCause = ""
	operationKind := map[string]string{"predecessor-backup": "backup", "old-restore": "restore", "candidate-upgrade": "upgrade"}[name]
	maintenance := operationKind != ""
	if maintenance && d.db != nil {
		// The product correctly rejects any remaining administrator session.
		// The test observer must release its own connection, not weaken that gate.
		if err := d.db.Close(); err != nil {
			return nil, err
		}
		d.db = nil
	}
	cmd := exec.CommandContext(ctx, "sudo", append([]string{"-n", "env", "PATH=/usr/lib/postgresql/16/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}, args...)...)
	cmd.Stdin = bytes.NewReader(stdin)
	if name == "original-prestate" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cmd.Dir = filepath.Join(cwd, "internal", "db")
	}
	raw, err := cmd.CombinedOutput()
	// Full diagnostic output is kept only in the private synthetic fixture.
	path := filepath.Join(filepath.Dir(d.input.Installed.XIAdminDSNFile), name+".log")
	if writeErr := os.WriteFile(path, raw, 0600); writeErr != nil {
		return nil, errors.New("private_diagnostic_failed")
	}
	if err != nil {
		if maintenance {
			d.captureOperationFailure(ctx, operationKind, raw)
		} else {
			d.failureCause = safePreflightFailure(raw)
		}
		return nil, errors.New("predecessor_operation_failed")
	}
	if maintenance {
		if err := d.openInstalledDatabase(ctx, operationKind != "backup"); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

// Preflight commands have no operation journal. Keep their known fixed
// categories while withholding command output, paths and credential values.
func safePreflightFailure(raw []byte) string {
	for _, item := range []struct{ text, category string }{
		{"paused authentication requires effective controller target network:", "controller_network_unready"},
		{"original receiver activate", "original_receiver_activation_failed"},
		{"unsupported active systemd service drop-in:", "unsupported_service_dropin"},
		{"restore requires an absolute backup path, exact SHA-256, and xinsight_restore_* database name", "restore_input_invalid"},
		{"managed current release pointer drift", "managed_release_pointer_drift"},
		{"private backup ", "private_backup_preflight_failed"},
		{"restore_original_review_body_key_required", "original_body_key_required"},
		{"restore target database already exists", "restore_target_already_exists"},
	} {
		if bytes.Contains(raw, []byte(item.text)) {
			return item.category
		}
	}
	return "private_command_failed"
}

func (d *driver) openInstalledDatabase(ctx context.Context, restored bool) error {
	raw, err := exec.CommandContext(ctx, "sudo", "-n", "cat", "/etc/x-insight/database/current/postgres-admin.dsn").Output()
	if err != nil {
		return errors.New("restored_database_unavailable")
	}
	u, err := url.Parse(strings.TrimSpace(string(raw)))
	if err != nil || u.Host != "127.0.0.1:5432" || u.User == nil || u.User.Username() != "xinsight_admin" ||
		(restored && !strings.HasPrefix(u.Path, "/xinsight_restore_")) || (!restored && u.Path != "/xinsight") {
		return errors.New("restored_database_identity")
	}
	d.db, err = sql.Open("pgx", u.String())
	if err != nil {
		return errors.New("restored_database_unavailable")
	}
	return d.db.PingContext(ctx)
}

func (d *driver) captureOperationFailure(ctx context.Context, operationKind string, commandOutput []byte) {
	raw, err := exec.CommandContext(ctx, "sudo", "-n", "/opt/x-insight/current/bin/xinsightctl", "status").Output()
	if err != nil || len(raw) > 128<<10 {
		d.failureCause = "operation_diagnostic_unavailable"
		return
	}
	d.failureCause = safeOperationFailure(raw, operationKind, commandOutput)
}

// Export only known product error codes. Status/journal paths, arbitrary
// messages, SQL errors and private command output never become CI artifacts.
func safeOperationFailure(raw []byte, operationKind string, commandOutput []byte) string {
	var status struct {
		Journal *struct {
			Code   string `json:"error_code"`
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"active_journal"`
	}
	if json.Unmarshal(raw, &status) != nil || status.Journal == nil ||
		status.Journal.Kind != operationKind || status.Journal.Status != "failed" {
		return "operation_diagnostic_unavailable"
	}
	if status.Journal.Code == "backup_service_restore_failed" &&
		bytes.Contains(commandOutput, []byte("lstat /run/x-insight/connector-controller.sock: no such file or directory")) {
		return "backup_controller_socket_not_ready"
	}
	switch status.Journal.Code {
	case "writer_sessions_present", "writer_session_check_failed", "maintenance_lock_failed",
		"private_backup_failed", "private_backup_revalidation_failed", "backup_service_restore_failed",
		"backup_service_restore_proof_failed", "importer_gate_failed", "service_stop_failed",
		"database_role_setup_failed", "database_role_boundary_failed",
		"restored_database_role_boundary_failed", "migration_failed", "activation_proof_failed":
		return status.Journal.Code
	default:
		return "operation_failed"
	}
}

func (d *driver) prepareRestoredPrestate(ctx context.Context) error {
	inputs, err := d.predecessorInputs()
	if err != nil {
		return err
	}
	wire, err := json.Marshal(d.input)
	if err != nil {
		return err
	}
	if _, err = d.privateCommand(ctx, "original-prestate", wire, "env", "GITHUB_ACTIONS=true", "XINSIGHT_CI_EPHEMERAL_HOST=1", "XINSIGHT_CI_INSTALLED_PRESTATE=1", inputs.Helper, "-test.run=^TestXIInstalledOriginalPrestate$", "-test.v"); err != nil {
		return err
	}
	d.originalHistory, err = d.originalHistorySnapshot(ctx)
	if err != nil || d.originalHistory == "[]" {
		return errors.New("original_history_missing")
	}
	d.originalStreams, err = d.streamSnapshot(ctx)
	if err != nil {
		return err
	}
	d.originalBindings, err = d.bindingSnapshot(ctx)
	if err != nil {
		return err
	}
	d.originalContentProgress, err = d.contentProgressSnapshot(ctx)
	if err != nil {
		return err
	}
	const backup = "/var/backups/x-insight/ci-fpv14-installed-joint.dump.tar"
	const ctl = "/opt/x-insight/current/bin/xinsightctl"
	// The signed candidate backs up the unchanged FPV14 installation. Its
	// readiness fix must work before upgrade, and the original ctl must still
	// restore the resulting version-compatible private backup.
	if _, err = d.privateCommand(ctx, "predecessor-backup-check", nil, inputs.CandidateCTL, "backup", "--check", "--output", backup); err != nil {
		return err
	}
	raw, err := d.privateCommand(ctx, "predecessor-backup", nil, inputs.CandidateCTL, "backup", "--output", backup)
	if err != nil {
		return err
	}
	var receipt struct {
		Status string `json:"status"`
		Path   string `json:"backup_path"`
		SHA    string `json:"backup_sha256"`
	}
	if json.Unmarshal(raw, &receipt) != nil || receipt.Status != "completed" || receipt.Path != backup || len(strings.TrimPrefix(receipt.SHA, "sha256:")) != 64 {
		return errors.New("backup_receipt_invalid")
	}
	args := []string{ctl, "restore", "--check", "--recovery-bundle", backup, "--recovery-bundle-sha256", receipt.SHA, "--new-database", "xinsight_restore_fpv14_joint_ci"}
	if _, err = d.privateCommand(ctx, "old-restore-check", nil, args...); err != nil {
		return err
	}
	if history, e := d.originalHistorySnapshot(ctx); e != nil || history != d.originalHistory {
		return errors.New("check_changed_history")
	}
	if streams, e := d.streamSnapshot(ctx); e != nil || streams != d.originalStreams {
		return errors.New("check_changed_streams")
	}
	if err = d.verifyOriginalBindings(ctx, true); err != nil {
		return err
	}
	args = []string{ctl, "restore", "--recovery-bundle", backup, "--recovery-bundle-sha256", receipt.SHA, "--new-database", "xinsight_restore_fpv14_joint_ci", "--reason-code", "field_validation"}
	if _, err = d.privateCommand(ctx, "old-restore", nil, args...); err != nil {
		return err
	}
	if history, e := d.originalHistorySnapshot(ctx); e != nil || history != d.originalHistory {
		return errors.New("restore_changed_history")
	}
	if streams, e := d.streamSnapshot(ctx); e != nil || streams != d.originalStreams {
		return errors.New("restore_changed_cursors")
	}
	if err = d.verifyOriginalBindings(ctx, true); err != nil {
		return err
	}
	d.retiredState, err = d.retiredSnapshot(ctx)
	return err
}

func (d *driver) upgradeBlockedPredecessor(ctx context.Context) error {
	inputs, err := d.predecessorInputs()
	if err != nil {
		return err
	}
	if err = d.await(ctx, func() (bool, error) {
		raw, e := exec.CommandContext(ctx, "sudo", "-n", "journalctl", "-u", "xinsight-connector-controller.service", "--no-pager", "-n", "60", "-o", "cat").Output()
		if e != nil {
			return false, errors.New("old_runtime_diagnostic_unavailable")
		}
		return bytes.Contains(raw, []byte("publish machine configuration")) && bytes.Contains(raw, []byte("read-only file system")), nil
	}); err != nil {
		return err
	}
	d.advance("formal_upgrade")
	before, err := d.streamSnapshot(ctx)
	if err != nil {
		return err
	}
	if _, err = d.privateCommand(ctx, "candidate-upgrade-check", nil, inputs.CandidateCTL, "upgrade", "--check", "--artifact", inputs.CandidateArtifact); err != nil {
		return err
	}
	if after, e := d.streamSnapshot(ctx); e != nil || before != after {
		return errors.New("upgrade_check_changed_cursors")
	}
	if err = d.verifyOriginalBindings(ctx, true); err != nil {
		return err
	}
	if _, err = d.privateCommand(ctx, "candidate-upgrade", nil, inputs.CandidateCTL, "upgrade", "--artifact", inputs.CandidateArtifact); err != nil {
		return err
	}
	d.adminCookie, d.adminCSRF, err = d.login(ctx, d.input.Installed.XIAdminUsername, d.input.Installed.XIAdminPasswordFile)
	return err
}

func (d *driver) originalHistorySnapshot(ctx context.Context) (string, error) {
	var out string
	err := d.db.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY event_id)::text,'[]') FROM source_fact_documents t WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND request_id='synthetic-restored-original-history'`, unirouter.DefaultSourceSystemID, d.input.SourceInstance, d.input.OrgID, d.input.ProjectID).Scan(&out)
	return out, err
}
func (d *driver) streamSnapshot(ctx context.Context) (string, error) {
	var out string
	err := d.db.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_array(id,profile,scope_generation,cursor_value,high_watermark) ORDER BY id)::text,'[]') FROM source_fact_streams WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4`, unirouter.DefaultSourceSystemID, d.input.SourceInstance, d.input.OrgID, d.input.ProjectID).Scan(&out)
	return out, err
}
func (d *driver) retiredSnapshot(ctx context.Context) (string, error) {
	var out string
	err := d.db.QueryRowContext(ctx, `SELECT jsonb_build_object('streams',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM source_fact_streams s WHERE source_instance='retired-joint-source'),'content',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM source_content_collections c WHERE source_instance='retired-joint-source'))::text`).Scan(&out)
	return out, err
}

// Stable registrations and checkpoint history must survive restore/adoption.
// Operational status intentionally changes from paused to active; live cursors
// may advance only after the actual worker starts.
// This isolated UR fixture emits no economic terminal events, so only its
// economic feed cursor/high-watermark must remain fixed through empty polls.
func (d *driver) bindingSnapshot(ctx context.Context) (string, error) {
	var out string
	err := d.db.QueryRowContext(ctx, `SELECT jsonb_build_object(
 'streams',(SELECT jsonb_agg(jsonb_build_array(id,profile,scope_generation) ORDER BY id) FROM source_fact_streams WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4),
 'content',(SELECT jsonb_agg(jsonb_build_array(id,credential_id,registration_hash,content_scope_revision,recovery_epoch,coverage_from) ORDER BY id) FROM source_content_collections WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4),
 'feed',(SELECT jsonb_agg(jsonb_build_array(id,activation_source_instance,authorization_ref,checkpoint_epoch,checkpoint_epoch_operation_id,cursor_value,high_watermark) ORDER BY id) FROM adapter_import_states WHERE source_system_id=$1::uuid AND cursor_key='unirouter.completed-execution.v1'),
 'epochs',(SELECT jsonb_agg(to_jsonb(h) ORDER BY operation_id) FROM connector_checkpoint_epoch_history h)
)::text`, unirouter.DefaultSourceSystemID, d.input.SourceInstance, d.input.OrgID, d.input.ProjectID).Scan(&out)
	return out, err
}
func (d *driver) contentProgressSnapshot(ctx context.Context) (string, error) {
	var out string
	err := d.db.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_array(id,cursor_value,last_sequence,high_watermark_sequence,high_watermark) ORDER BY id)::text,'[]') FROM source_content_collections WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4`, unirouter.DefaultSourceSystemID, d.input.SourceInstance, d.input.OrgID, d.input.ProjectID).Scan(&out)
	return out, err
}
func (d *driver) verifyOriginalBindings(ctx context.Context, unchangedProgress bool) error {
	if actual, err := d.bindingSnapshot(ctx); err != nil || actual != d.originalBindings {
		return errors.New("original_binding_changed")
	}
	if unchangedProgress {
		if actual, err := d.contentProgressSnapshot(ctx); err != nil || actual != d.originalContentProgress {
			return errors.New("original_content_cursor_changed")
		}
	}
	return nil
}
func (d *driver) verifyAdoptedHistory(ctx context.Context) error {
	if err := d.verifyOriginalBindings(ctx, false); err != nil {
		return err
	}
	if history, err := d.originalHistorySnapshot(ctx); err != nil || history != d.originalHistory {
		return errors.New("history_changed")
	}
	if retired, err := d.retiredSnapshot(ctx); err != nil || retired != d.retiredState {
		return errors.New("retired_source_changed")
	}
	var count int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM source_fact_streams WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND status='active' AND NOT requires_attention`, unirouter.DefaultSourceSystemID, d.input.SourceInstance, d.input.OrgID, d.input.ProjectID).Scan(&count); err != nil || count != 8 {
		return errors.New("original_sources_not_resumed")
	}
	return nil
}

func (d *driver) awaitEconomicFeedRead(ctx context.Context, since time.Time) error {
	// CommitPage records this timestamp only after a real HTTP page has been
	// validated and committed. Empty fixture pages still prove the economic
	// worker's actual egress and credentials, without inventing economic facts.
	return d.await(ctx, func() (bool, error) {
		var received bool
		err := d.db.QueryRowContext(ctx, `SELECT EXISTS (
 SELECT 1 FROM adapter_import_states
 WHERE source_system_id=$1::uuid AND adapter_manifest_id=$2::uuid
   AND cursor_key=$3 AND activation_source_instance=$4
   AND last_import_completed_at >= $5 AND pages_committed > 0
   AND COALESCE(last_error,'')='')`, unirouter.DefaultSourceSystemID,
			unirouter.DefaultCompletedExecutionAdapterManifestID, connectorcontrol.ControlCursorKey,
			d.input.SourceInstance, since).Scan(&received)
		return received, err
	})
}

// This is an actual service lifecycle check on the guarded disposable VM.
// The controller and consumers must all have new invocations, both workers
// must commit another page, and the reader must retrieve the saved body.
func (d *driver) restartAndVerify(ctx context.Context, contentRef string) error {
	units := []string{"xinsight-connector-controller.service", "xinsight-source-facts.service", "xinsight.service", "xinsight-feed.service"}
	before := make(map[string]string)
	for _, unit := range units {
		id, err := invocationID(ctx, unit)
		if err != nil {
			return err
		}
		before[unit] = id
	}
	restarted := time.Now().UTC()
	cmd := exec.CommandContext(ctx, "sudo", "-n", "/usr/bin/systemctl", "restart", units[0], units[1], units[2], units[3])
	if cmd.Run() != nil {
		return errors.New("service_restart_failed")
	}
	for _, unit := range units {
		id, err := invocationID(ctx, unit)
		if err != nil || id == before[unit] {
			return errors.New("service_invocation_unchanged")
		}
	}
	if err := d.await(ctx, func() (bool, error) {
		var count int
		err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM source_fact_streams WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND profile=$5 AND status='active' AND last_error='' AND last_received_at>$6`,
			unirouter.DefaultSourceSystemID, d.input.SourceInstance, d.input.OrgID, d.input.ProjectID, sourcefacts.ContextProfile, restarted).Scan(&count)
		return count == 1, err
	}); err != nil {
		return err
	}
	if err := d.awaitEconomicFeedRead(ctx, restarted); err != nil {
		return errors.New("economic_feed_restart_unavailable")
	}
	if err := d.await(ctx, func() (bool, error) {
		cookie, _, err := d.login(ctx, d.input.Installed.XIReaderUsername, d.input.Installed.XIReaderPasswordFile)
		if err != nil {
			return false, nil
		}
		d.readerCookie = cookie
		return true, nil
	}); err != nil {
		return err
	}
	return d.readSavedBody(ctx, contentRef, d.input.ExpectedContent.SHA256)
}

func invocationID(ctx context.Context, unit string) (string, error) {
	raw, err := exec.CommandContext(ctx, "sudo", "-n", "/usr/bin/systemctl", "show", unit, "--property=InvocationID", "--value").Output()
	id := strings.TrimSpace(string(raw))
	if err != nil || len(id) != 32 {
		return "", errors.New("service_invocation_unavailable")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", errors.New("service_invocation_invalid")
	}
	return id, nil
}

// Keep the independently serialized UR fixture input strict, including the
// nested mode fields. Decode before opening credentials or contacting either
// service, so a transport mismatch cannot become a misleading runtime failure.
func decodeInput(reader io.Reader) (installedInput, error) {
	var input installedInput
	decoder := json.NewDecoder(io.LimitReader(reader, 20<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, errors.New("input_invalid")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || input.Installed.SchemaVersion != "xinsight.ci-installed-joint.v1" ||
		input.ManagedMode != "new_install" && input.ManagedMode != "adopt_existing" ||
		input.ManagedMode != input.Installed.Mode || input.FutureProject != input.Installed.FutureProject ||
		input.XIControlOrigin != input.Installed.XIOrigin || !input.NormalSetup || len(input.Profiles) != 8 ||
		input.ReceiverAllowedIP != input.Installed.ReceiverAllowedIP || input.PostgresDSN != "" ||
		input.Installed.TimeoutSeconds < 120 || input.Installed.TimeoutSeconds > 1200 ||
		input.FutureProject && input.ManagedMode != "new_install" {
		return input, errors.New("input_invalid")
	}
	nonce, err := hex.DecodeString(input.LifecycleNonce)
	if err != nil || len(nonce) != 32 {
		return input, errors.New("input_invalid")
	}
	return input, nil
}

// The fixed UR fixture deliberately scrubs child environment variables. Its
// installed stdin carries the private fixture directory, so the optional CI
// upgrade request is resolved there without changing UR or existing lanes.
func readFPV17UpgradeRequest(adminDSNFile string) (*fpv17UpgradeRequest, error) {
	path := filepath.Join(filepath.Dir(adminDSNFile), "fpv17-upgrade-request.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4096 || info.Size() == 0 {
		return nil, errors.New("upgrade_request_invalid")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Getuid()) {
		return nil, errors.New("upgrade_request_invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("upgrade_request_invalid")
	}
	defer file.Close()
	var request fpv17UpgradeRequest
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	var extra any
	if decoder.Decode(&request) != nil || decoder.Decode(&extra) != io.EOF ||
		request.SchemaVersion != "xinsight.ci-fpv17-installed-upgrade-request.v1" ||
		request.CandidateArtifact != "/opt/xinsight-ci-artifacts/candidate.tar.gz" ||
		request.CandidatePayload != filepath.Join(filepath.Dir(filepath.Dir(adminDSNFile)), "fresh-install-payload") ||
		!filepath.IsAbs(request.Script) || filepath.Clean(request.Script) != request.Script ||
		filepath.Base(request.Script) != "qualify-ubuntu22-fpv17-upgrade.sh" {
		return nil, errors.New("upgrade_request_invalid")
	}
	return &request, nil
}

func (d *driver) fail(category string, causes ...error) (string, time.Duration, error) {
	d.emitStage("failed")
	d.writeRuntimeDiagnostic()
	if len(causes) > 0 && causes[0] != nil && d.failureCause == "" {
		switch cause := causes[0].Error(); cause {
		case "original_history_missing", "backup_receipt_invalid", "check_changed_history", "check_changed_streams",
			"original_binding_changed", "original_content_cursor_changed", "restore_changed_history", "restore_changed_cursors",
			"restored_database_unavailable", "restored_database_identity", "upgrade_check_changed_cursors":
			category = cause
		}
	}
	if d.failureCause != "" {
		category = d.failureCause
	}
	return d.stage, time.Since(d.start), errors.New(category)
}

func safeRuntimeEnum(value string, allowed ...string) string {
	if value == "" {
		return "none"
	}
	if slices.Contains(allowed, value) {
		return value
	}
	return "unclassified"
}

// The installed-service check can time out after a successful human consent.
// Preserve only bounded counts and fixed states, never database rows, IDs,
// credentials, body values or free-form errors. Use a separate short context
// because the failed acceptance context may already have expired.
func (d *driver) writeRuntimeDiagnostic() {
	out := map[string]any{"schema_version": "xinsight.ci-installed-runtime.v1", "setup_available": false, "phases_available": false, "sources_available": false}
	if d.failureHTTPStatus >= 400 && d.failureHTTPStatus <= 599 {
		out["failure_http_status"] = d.failureHTTPStatus
		out["failure_http_category"] = httpFailureCategory(d.failureHTTPStatus)
		out["failure_problem_code"] = d.failureHTTPProblem
	}
	if d.db != nil && connectorcontrol.IsInstallationID(d.installationID) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var state, setupError string
		var applied bool
		if d.db.QueryRowContext(ctx, `SELECT status,COALESCE(setup_error_code,''),setup_policy_applied_at IS NOT NULL FROM connector_installations WHERE id=$1`, d.installationID).Scan(&state, &setupError, &applied) == nil {
			out["setup_available"], out["setup_applied"] = true, applied
			out["installation_state"] = safeRuntimeEnum(state, "pending", "abnormal", "connected", "paused", "disconnected")
			out["setup_error"] = safeRuntimeEnum(setupError, "custody_loading", "custody_revision_changed", "xi_actor_permission_changed")
		}
		var pending, completed, failed, delivered int
		if d.db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE status='pending'),count(*) FILTER(WHERE status='completed'),count(*) FILTER(WHERE status='failed'),count(*) FILTER(WHERE status='completed' AND phase='deliver_machine_candidate') FROM connector_control_operations WHERE installation_id=$1`, d.installationID).Scan(&pending, &completed, &failed, &delivered) == nil {
			out["phases_available"] = true
			out["phases_pending"], out["phases_completed"], out["phases_failed"], out["machines_delivered"] = pending, completed, failed, delivered
		}
		var streams, contents, accepted int
		if d.db.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM source_fact_streams WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND source_system_id=$4::uuid AND status='active'),
 (SELECT count(*) FROM source_content_collections WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND source_system_id=$4::uuid AND status='active'),
 (SELECT count(*) FROM review_body_operations WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND source_system_id=$4::uuid AND state='accepted')`, d.input.SourceInstance, d.input.OrgID, d.input.ProjectID, unirouter.DefaultSourceSystemID).Scan(&streams, &contents, &accepted) == nil {
			out["sources_available"] = true
			out["active_streams"], out["active_contents"], out["accepted_bodies"] = streams, contents, accepted
		}
		d.writeContentProgress(ctx, out)
		d.writeCredentialDiagnostic(ctx, out)
	}
	path := filepath.Join(filepath.Dir(d.input.Installed.XIAdminDSNFile), "runtime-diagnostic.json")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(out)
}

func (d *driver) writeContentProgress(ctx context.Context, out map[string]any) {
	out["content_progress_available"] = false
	keys := []string{"content_attention", "content_success", "content_manifests", "expected_manifests",
		"copy_attempts", "copy_retry", "copy_policy_excluded", "copy_scope_lost", "copy_integrity_gap", "copy_expired",
		"copy_identity_pending", "copy_binding_pending", "legacy_copy_pending",
		"content_pages_pending", "content_coverage_unknown", "content_capture_health_unknown", "content_other_gaps"}
	counts := make([]int64, len(keys))
	dest := make([]any, len(keys))
	for i := range counts {
		dest[i] = &counts[i]
	}
	err := d.db.QueryRowContext(ctx, `WITH collections AS (
 SELECT id,requires_attention,last_success_at,last_sequence,high_watermark_sequence,coverage,gaps FROM source_content_collections
 WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND status='active'
), attempts AS (SELECT a.status,a.reason FROM review_body_copy_attempts a JOIN collections c ON c.id=a.collection_id)
SELECT
 (SELECT count(*) FROM collections WHERE requires_attention),
 (SELECT count(*) FROM collections WHERE last_success_at IS NOT NULL),
 (SELECT count(*) FROM source_content_manifests m JOIN collections c ON c.id=m.collection_id),
 (SELECT count(*) FROM source_content_manifests m JOIN collections c ON c.id=m.collection_id WHERE m.request_id=$5 AND m.source_json->'representation'->>'digest'=$6),
 (SELECT count(*) FROM attempts),
 (SELECT count(*) FROM attempts WHERE status='retry'),
 (SELECT count(*) FROM attempts WHERE status='excluded_policy'),
 (SELECT count(*) FROM attempts WHERE status='scope_lost'),
 (SELECT count(*) FROM attempts WHERE status='integrity_gap'),
 (SELECT count(*) FROM attempts WHERE status='source_expired'),
 (SELECT count(*) FROM attempts WHERE reason='account_identity_pending'),
 (SELECT count(*) FROM attempts WHERE reason='project_binding_missing'),
 (SELECT count(*) FROM connector_source_enrollments e JOIN collections c ON c.id=e.content_collection_id WHERE e.installation_id=$7 AND e.legacy_c_copy_pending),
 (SELECT count(*) FROM collections WHERE last_sequence<>high_watermark_sequence),
 (SELECT count(*) FROM collections WHERE coverage='unknown'),
 (SELECT count(*) FROM collections c CROSS JOIN LATERAL jsonb_array_elements(c.gaps) g WHERE g->>'reason'='capture_health_unknown'),
 (SELECT count(*) FROM collections c CROSS JOIN LATERAL jsonb_array_elements(c.gaps) g WHERE g->>'reason'<>'capture_health_unknown')`,
		unirouter.DefaultSourceSystemID, d.input.SourceInstance, d.input.OrgID, d.input.ProjectID,
		d.input.ExpectedContent.RequestID, "sha256:"+d.input.ExpectedContent.SHA256, d.installationID).Scan(dest...)
	if err == nil {
		out["content_progress_available"] = true
		for i, key := range keys {
			out[key] = counts[i]
		}
	}
	var code string
	if d.db.QueryRowContext(ctx, `SELECT last_error FROM source_content_collections
 WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND status='active'
 ORDER BY last_attempt_at DESC NULLS LAST,id LIMIT 1`, unirouter.DefaultSourceSystemID,
		d.input.SourceInstance, d.input.OrgID, d.input.ProjectID).Scan(&code) == nil {
		out["content_error"] = safeContentFailure(code)
	}
	rows, err := d.db.QueryContext(ctx, `SELECT last_error,count(*) FROM source_fact_streams
 WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND status='active'
 GROUP BY last_error ORDER BY last_error LIMIT 9`, unirouter.DefaultSourceSystemID,
		d.input.SourceInstance, d.input.OrgID, d.input.ProjectID)
	if err != nil {
		return
	}
	defer rows.Close()
	codes := map[string]int64{}
	for rows.Next() {
		var count int64
		if rows.Scan(&code, &count) != nil {
			return
		}
		codes[safeContentFailure(code)] += count
	}
	if rows.Err() == nil {
		out["source_errors"] = codes
	}
}

func safeContentFailure(code string) string {
	return safeRuntimeEnum(code, "profile_not_authorized", "source_paused", "staged_credential_requires_pause",
		"cursor_or_epoch_conflict", "cursor_expired", "source_authorization_denied", "scope_changed",
		"invalid_content_page", "source_fact_conflict", "source_unavailable", "restore_requires_authorization",
		"invalid_capabilities", "invalid_page")
}

func (d *driver) advance(next string) {
	d.emitStage("passed")
	d.stage, d.stageStart = next, time.Now()
}

func (d *driver) emitStage(state string) {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"schema_version": "xinsight.ci-installed-joint-stage.v1",
		"stage":          d.stage, "state": state, "elapsed_ms": time.Since(d.stageStart).Milliseconds(),
	})
}

func (d *driver) seedFixture(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name,status) VALUES($1,'unirouter','unirouter','Joint UR','active') ON CONFLICT DO NOTHING`, unirouter.DefaultSourceSystemID)
	if err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, `INSERT INTO adapter_manifests(id,adapter_id,adapter_version,source_type,fidelity_level,payload_behavior,idempotency_strategy) VALUES($1,'synthetic-joint-ur','1.0.0','unirouter','F2','metadata_only_default','source_ref') ON CONFLICT DO NOTHING`, unirouter.DefaultCompletedExecutionAdapterManifestID)
	if err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences,allow_content,allowed_content_purposes) VALUES($1,$2::uuid,true,true,true,true,true,'["human_review"]') ON CONFLICT ON CONSTRAINT workbench_access_grants_scope_unique DO UPDATE SET allow_content=true,allowed_content_purposes='["human_review"]'`, d.input.Installed.XIReaderSubjectID, unirouter.DefaultSourceSystemID)
	return err
}

func (d *driver) login(ctx context.Context, username, passwordFile string) (*http.Cookie, string, error) {
	d.failureCause = ""
	d.failureHTTPStatus, d.failureHTTPProblem = 0, ""
	password, err := os.ReadFile(passwordFile)
	if err != nil {
		return nil, "", err
	}
	wire, err := json.Marshal(map[string]string{"username": username, "password": strings.TrimSuffix(string(password), "\n")})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", d.input.XIControlOrigin+"/api/v1/access/login", bytes.NewReader(wire))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", d.input.XIControlOrigin)
	response, err := d.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		d.recordHTTPFailure(response)
		return nil, "", errors.New("login status")
	}
	var out struct {
		CSRF string `json:"csrf_token"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&out); err != nil {
		return nil, "", err
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == "__Host-xinsight_session" && cookie.Value != "" && out.CSRF != "" {
			return cookie, out.CSRF, nil
		}
	}
	return nil, "", errors.New("session")
}

func (d *driver) call(ctx context.Context, method, target string, body any, cookie *http.Cookie, csrf string, out any) error {
	d.failureCause = ""
	d.failureHTTPStatus, d.failureHTTPProblem = 0, ""
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != "GET" {
		req.Header.Set("Origin", d.originFor(target))
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	response, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		d.recordHTTPFailure(response)
		return errors.New("http_status")
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 128<<10)).Decode(out)
}

// Keep the caller's precise failed operation in the stage receipt. HTTP status
// and an allowlisted problem code are separate evidence, not a replacement for
// that operation. Never export the URL, headers, problem title or raw body.
func (d *driver) recordHTTPFailure(response *http.Response) {
	d.failureHTTPStatus = response.StatusCode
	d.failureHTTPProblem = "unclassified"
	raw, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return
	}
	var problem struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &problem) != nil {
		return
	}
	// UR and XI retain their own public error envelopes. Accept only one
	// fixed machine code; never export either product's human error text.
	if problem.Code != "" && problem.Error != "" {
		return
	}
	if problem.Code == "" {
		switch problem.Error {
		case "setup_unavailable", "invalid_setup", "setup_changed", "recovery_denied", "setup_not_found":
			d.failureHTTPProblem = problem.Error
		}
		return
	}
	switch problem.Code {
	case "invalid_connector_request", "connector_not_found", "connector_state_conflict",
		"connector_credential_recovery_expired", "connector_control_unavailable":
		d.failureHTTPProblem = problem.Code
	}
}

func httpFailureCategory(status int) string {
	switch status {
	case 400:
		return "bad_request"
	case 401:
		return "unauthenticated"
	case 403:
		return "forbidden"
	case 404:
		return "not_found"
	case 409:
		return "conflict"
	case 429:
		return "throttled"
	default:
		if status >= 500 {
			return "server_error"
		}
		return "unexpected_status"
	}
}

func (d *driver) originFor(target string) string {
	u, err := url.Parse(target)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
func (d *driver) await(ctx context.Context, check func() (bool, error)) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready, err := check()
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func uuid() string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:])
}
