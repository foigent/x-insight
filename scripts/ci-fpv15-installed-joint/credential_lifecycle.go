package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

// These types describe only the disposable UR fixture seam. They are not a
// product API. The secret nonce arrives over the original private stdin pipe;
// neither it nor fixture responses are included in public CI receipts.
type lifecycleRequest struct {
	Action         string     `json:"action"`
	InstallationID string     `json:"installation_id"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	DropResponse   string     `json:"drop_response,omitempty"`
}

type lifecycleState struct {
	Paused                   bool      `json:"paused"`
	CurrentTokenSHA256       string    `json:"current_token_sha256"`
	CurrentExpiresAt         time.Time `json:"current_expires_at"`
	PendingOperationID       string    `json:"pending_operation_id"`
	PendingState             string    `json:"pending_state"`
	PendingReplacementSHA256 string    `json:"pending_replacement_sha256"`
	DroppedResponse          string    `json:"dropped_response"`
	RequestID                string    `json:"request_id"`
	SHA256                   string    `json:"sha256"`
}

func (d *driver) lifecycleFixture(ctx context.Context, action string, expiry *time.Time, drop string) (lifecycleState, error) {
	var state lifecycleState
	wire, err := json.Marshal(lifecycleRequest{action, d.installationID, expiry, drop})
	if err != nil {
		return state, errors.New("lifecycle_fixture_request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.input.BaseURL+"/__joint/credential-lifecycle", bytes.NewReader(wire))
	if err != nil {
		return state, errors.New("lifecycle_fixture_request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Joint-Lifecycle-Nonce", d.input.LifecycleNonce)
	response, err := d.client.Do(req)
	if err != nil {
		return state, errors.New("lifecycle_fixture_transport")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return state, errors.New("lifecycle_fixture_rejected")
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil {
		return state, errors.New("lifecycle_fixture_response")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return state, errors.New("lifecycle_fixture_response")
	}
	return state, nil
}

// The fixed UR installed fixture establishes its original authority snapshot
// only on pause. The FPV16 upgrade lane skips the destructive expiry and
// rotation scenarios, so establish that snapshot with a no-op pause/resume
// before host upgrade while the original control token is still current.
func (d *driver) establishUpgradeAuthority(ctx context.Context) error {
	before, err := d.lifecycleFixture(ctx, "pause", nil, "")
	if err != nil || !before.Paused || before.CurrentTokenSHA256 == "" || before.CurrentExpiresAt.IsZero() ||
		before.PendingOperationID != "" || before.DroppedResponse != "" {
		return errors.New("upgrade_authority_prestate_invalid")
	}
	after, err := d.lifecycleFixture(ctx, "resume", nil, "")
	if err != nil || after.Paused || after.CurrentTokenSHA256 != before.CurrentTokenSHA256 ||
		!after.CurrentExpiresAt.Equal(before.CurrentExpiresAt) || after.PendingOperationID != "" || after.DroppedResponse != "" {
		return errors.New("upgrade_authority_resume_invalid")
	}
	return nil
}

// Restrict synthetic time arrangement to exactly this installed test's
// identity and its current/prepared tokens. Do not change the current pointer,
// operation state, grants, history, receipt bytes or application clock.
// Both services must observe real wall time crossing the arranged deadline.
func (d *driver) arrangeCredentialExpiry(ctx context.Context, expires time.Time, includePrepared bool) error {
	state, err := d.lifecycleFixture(ctx, "pause", nil, "")
	if err != nil || !state.Paused {
		return errors.New("lifecycle_pause_failed")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("lifecycle_arrange_failed")
	}
	defer tx.Rollback()
	var identity, current string
	// Match the product's installation -> identity -> current -> token lock
	// order. The installed XI controller continues running during this setup.
	if err = tx.QueryRowContext(ctx, `SELECT control_service_identity_id FROM connector_installations WHERE id=$1 FOR UPDATE`, d.installationID).Scan(&identity); err != nil {
		return errors.New("lifecycle_lineage_unavailable")
	}
	var identityStatus string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM service_identities WHERE id=$1 FOR UPDATE`, identity).Scan(&identityStatus); err != nil || identityStatus == "revoked" {
		return errors.New("lifecycle_lineage_unavailable")
	}
	if err = tx.QueryRowContext(ctx, `SELECT current_token_digest FROM connector_control_credentials WHERE installation_id=$1 AND service_identity_id=$2 FOR UPDATE`, d.installationID, identity).Scan(&current); err != nil {
		return errors.New("lifecycle_lineage_unavailable")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE service_identity_tokens SET expires_at=$2 WHERE token_digest=$1 AND service_identity_id=$3 AND revoked_at IS NULL`, current, expires, identity); err != nil {
		return errors.New("lifecycle_arrange_failed")
	}
	if includePrepared {
		if _, err = tx.ExecContext(ctx, `UPDATE service_identity_tokens SET expires_at=$2 WHERE service_identity_id=$3 AND token_digest IN(SELECT replacement_token_sha256 FROM connector_control_credential_operations WHERE installation_id=$1 AND state='prepared') AND revoked_at IS NULL`, d.installationID, expires, identity); err != nil {
			return errors.New("lifecycle_arrange_failed")
		}
		if _, err = tx.ExecContext(ctx, `UPDATE connector_control_credential_operations SET replacement_expires_at=$2 WHERE installation_id=$1 AND state='prepared'`, d.installationID, expires); err != nil {
			return errors.New("lifecycle_arrange_failed")
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE service_identities SET expires_at=$2 WHERE id=$1 AND status='active'`, identity, expires); err != nil || tx.Commit() != nil {
		return errors.New("lifecycle_arrange_failed")
	}
	if _, err = d.lifecycleFixture(ctx, "expire_current", &expires, ""); err != nil {
		return err
	}
	return nil
}

// Compare stable authority and already received facts; live timestamps,
// cursors and credential generations are deliberately excluded. Progress may
// advance, while original consent, scopes and historical facts may not change.
func (d *driver) lifecycleInvariant(ctx context.Context) (string, error) {
	var state string
	err := d.db.QueryRowContext(ctx, `SELECT jsonb_build_object(
 'installation',(SELECT jsonb_build_array(id,deployment_id,project_id,final_event_scope,source_instance,source_instance_sha256,control_service_identity_id,installation_acknowledgement_sha256,setup_request_id,setup_request_sha256,setup_consent) FROM connector_installations WHERE id=$1),
 'binding',(SELECT to_jsonb(b) FROM connector_installation_source_bindings b WHERE installation_id=$1),
 'operations',(SELECT jsonb_agg(jsonb_build_array(operation_id,request_sha256,request_payload,result_sha256,result_payload) ORDER BY operation_id) FROM connector_control_operations WHERE installation_id=$1),
 'identity',(SELECT jsonb_build_array(i.id,i.permissions,i.created_by_subject) FROM service_identities i JOIN connector_control_credentials c ON c.service_identity_id=i.id WHERE c.installation_id=$1),
 'grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY id) FROM workbench_access_grants g WHERE subject_id=$2),
 'facts',(SELECT jsonb_agg(to_jsonb(f) ORDER BY event_id) FROM source_fact_documents f WHERE source_system_id=$3::uuid AND source_instance=$4 AND org_id=$5 AND project_id=$6 AND request_id=$7)
)::text`, d.installationID, d.input.Installed.XIReaderSubjectID, unirouter.DefaultSourceSystemID, d.input.SourceInstance, d.input.OrgID, d.input.ProjectID, d.input.ExpectedContent.RequestID).Scan(&state)
	if err != nil {
		return "", errors.New("lifecycle_invariant_unavailable")
	}
	digest := sha256.Sum256([]byte(state))
	return hex.EncodeToString(digest[:]), nil
}

func (d *driver) awaitCredentialCurrent(ctx context.Context, previous string) error {
	return d.await(ctx, func() (bool, error) {
		state, err := d.lifecycleFixture(ctx, "status", nil, "")
		if err != nil {
			return false, err
		}
		if state.Paused || state.PendingOperationID != "" || state.CurrentTokenSHA256 == "" || state.CurrentTokenSHA256 == previous || !state.CurrentExpiresAt.After(time.Now()) {
			return false, nil
		}
		var same bool
		err = d.db.QueryRowContext(ctx, `SELECT c.current_token_digest=$2 AND t.revoked_at IS NULL AND t.expires_at>clock_timestamp() FROM connector_control_credentials c JOIN service_identity_tokens t ON t.token_digest=c.current_token_digest WHERE c.installation_id=$1`, d.installationID, state.CurrentTokenSHA256).Scan(&same)
		return same, err
	})
}

func (d *driver) readSavedBody(ctx context.Context, ref, expected string) error {
	var session struct {
		Revision string `json:"scope_revision"`
	}
	if d.call(ctx, "GET", d.input.XIControlOrigin+"/api/v1/access/session", nil, d.readerCookie, "", &session) != nil || session.Revision == "" {
		return errors.New("lifecycle_reader_scope")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.input.XIControlOrigin+"/api/v1/work-activity/contents/"+url.PathEscape(ref), nil)
	if err != nil {
		return errors.New("lifecycle_body_request")
	}
	req.AddCookie(d.readerCookie)
	req.Header.Set("X-XInsight-Scope-Revision", session.Revision)
	response, err := d.client.Do(req)
	if err != nil {
		return errors.New("lifecycle_body_transport")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	digest := sha256.Sum256(body)
	if err != nil || response.StatusCode != http.StatusOK || hex.EncodeToString(digest[:]) != expected || response.Header.Get("X-XInsight-Content-Expires-At") == "" {
		return errors.New("lifecycle_body_mismatch")
	}
	return nil
}

func (d *driver) verifyLifecycleData(ctx context.Context, invariant, oldRef string) error {
	actual, err := d.lifecycleInvariant(ctx)
	if err != nil || actual != invariant {
		return errors.New("lifecycle_authority_or_history_changed")
	}
	if err = d.readSavedBody(ctx, oldRef, d.input.ExpectedContent.SHA256); err != nil {
		return err
	}
	if d.input.ManagedMode == "adopt_existing" {
		if err = d.verifyAdoptedHistory(ctx); err != nil {
			return errors.New("lifecycle_adopted_history_changed")
		}
	}
	published, err := d.lifecycleFixture(ctx, "publish", nil, "")
	if err != nil || published.RequestID == "" || len(published.SHA256) != 64 {
		return errors.New("lifecycle_publish_failed")
	}
	var newRef string
	if err = d.await(ctx, func() (bool, error) {
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
	}); err != nil {
		return errors.New("lifecycle_new_data_unavailable")
	}
	return d.readSavedBody(ctx, newRef, published.SHA256)
}

func (d *driver) restartLifecycleController(ctx context.Context) error {
	const unit = "xinsight-connector-controller.service"
	before, err := invocationID(ctx, unit)
	if err != nil || exec.CommandContext(ctx, "sudo", "-n", "/usr/bin/systemctl", "restart", unit).Run() != nil {
		return errors.New("lifecycle_controller_restart")
	}
	after, err := invocationID(ctx, unit)
	if err != nil || before == after {
		return errors.New("lifecycle_controller_invocation")
	}
	// Restart the actual controller with its shipped sandbox and credentials.
	// Re-login uses the original human identity; it grants no new authority.
	err = d.await(ctx, func() (bool, error) {
		cookie, csrf, err := d.login(ctx, d.input.Installed.XIAdminUsername, d.input.Installed.XIAdminPasswordFile)
		if err == nil {
			d.adminCookie, d.adminCSRF = cookie, csrf
		}
		return err == nil, nil
	})
	if err != nil {
		return errors.New("lifecycle_admin_session_unavailable")
	}
	return d.awaitLifecycleController(ctx)
}

// A changed systemd InvocationID and a login to the separate web service do
// not prove that the restarted controller can answer requests. Check the
// actual read-only boundary before exercising recovery. Use the two-second
// startup allowance of hostctl.waitForConnectorControllerRuntimeState (21
// attempts, 100 ms apart), not the whole qualification deadline. An unavailable
// response is not proof of a transient failure: it must become a valid status
// inside that window, or qualification fails. Other failures are immediate.
func (d *driver) awaitLifecycleController(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := d.await(ctx, func() (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		var status connectorcontrol.SetupStatus
		path := d.input.XIControlOrigin + "/api/v1/connector-setups/" + d.installationID
		if err := d.call(ctx, "GET", path, nil, d.adminCookie, "", &status); err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			if d.failureHTTPStatus == http.StatusServiceUnavailable && d.failureHTTPProblem == "connector_control_unavailable" {
				return false, nil
			}
			return false, errors.New("lifecycle_controller_status_unavailable")
		}
		request := connectorcontrol.SetupLocalRequest{Action: "status", InstallationID: d.installationID}
		response := connectorcontrol.SetupLocalResponse{SchemaVersion: connectorcontrol.LocalSetupResponseSchema, Action: "status", Status: &status}
		if !connectorcontrol.ValidSetupResponse(request, response) {
			return false, errors.New("lifecycle_controller_status_invalid")
		}
		// needs_action is expected for an expired credential. Readiness here
		// means a valid response, not that the business connection is ready.
		return true, nil
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return errors.New("lifecycle_controller_not_ready")
	}
	return err
}

func (d *driver) recoverExpiredCredential(ctx context.Context) (connectorcontrol.RecoveryMaterial, error) {
	var material connectorcontrol.RecoveryMaterial
	path := d.input.XIControlOrigin + "/api/v1/connector-setups/" + d.installationID
	var status connectorcontrol.SetupStatus
	if err := d.call(ctx, "GET", path, nil, d.adminCookie, "", &status); err != nil {
		return material, errors.New("lifecycle_expiry_status")
	}
	expired := false
	for _, issue := range status.Issues {
		if issue.Code == "control_identity_expired" || issue.Code == "control_credential_expired" {
			expired = true
		}
	}
	if !expired || status.State == "ready" {
		return material, errors.New("lifecycle_expiry_misdiagnosed")
	}
	if d.call(ctx, "POST", path+"/credential-recovery", map[string]string{"request_id": uuid()}, d.adminCookie, d.adminCSRF, &material) != nil || material.InstallationID != d.installationID || !connectorcontrol.IsRecoveryTicket(material.Ticket) {
		return material, errors.New("lifecycle_xi_human_recovery")
	}
	base := d.input.BaseURL + "/api/v2/organizations/" + d.input.OrgID + "/xinsight/setups/" + d.installationID + "/credential-recovery"
	request := map[string]string{"recovery_ticket": material.Ticket}
	var preview struct {
		InstallationID string `json:"installation_id"`
	}
	if d.call(ctx, "POST", base+"/preview", request, nil, "", &preview) != nil || preview.InstallationID != d.installationID {
		return material, errors.New("lifecycle_ur_human_preview")
	}
	var accepted struct {
		OperationID string `json:"operation_id"`
	}
	if d.call(ctx, "POST", base, request, nil, "", &accepted) != nil || !connectorcontrol.IsCredentialOperationID(accepted.OperationID) {
		return material, errors.New("lifecycle_ur_human_confirmation")
	}
	return material, nil
}

func (d *driver) verifyCredentialLifecycle(ctx context.Context, oldRef string) error {
	d.advance("credential_lifecycle_baseline")
	invariant, err := d.lifecycleInvariant(ctx)
	if err != nil {
		return err
	}
	// Do not shorten a replacement after UR has durably received its original
	// expiry: that would conflict with the immutable receipt, not model elapsed
	// time. Known-receipt replacement expiry is covered by the PG clock tests.
	// The prepared-expiry case below drops the receipt before UR can store it.
	for _, scenario := range []struct {
		name, drop string
		recover    bool
	}{
		{"credential_continuous_rotation", "", false},
		{"credential_lost_prepare", "prepare", false},
		{"credential_lost_confirm", "confirm", false},
		{"credential_offline_expiry", "", true},
		{"credential_expired_prepared", "prepare", true},
	} {
		d.advance(scenario.name)
		before, err := d.lifecycleFixture(ctx, "pause", nil, "")
		if err != nil || !before.Paused || before.PendingOperationID != "" || before.CurrentTokenSHA256 == "" {
			return errors.New("lifecycle_prestate_invalid")
		}
		var operationsBefore int
		if d.db.QueryRowContext(ctx, `SELECT count(*) FROM connector_control_credential_operations WHERE installation_id=$1`, d.installationID).Scan(&operationsBefore) != nil {
			return errors.New("lifecycle_operations_unavailable")
		}
		expiry := time.Now().UTC().Add(10 * time.Second).Truncate(time.Microsecond)
		continuous := scenario.drop == "" && !scenario.recover
		if scenario.drop == "" && scenario.recover {
			expiry = time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
		}
		if err = d.arrangeCredentialExpiry(ctx, expiry, false); err != nil {
			return err
		}
		var interrupted lifecycleState
		if continuous {
			if _, err = d.lifecycleFixture(ctx, "resume", nil, ""); err != nil {
				return err
			}
			if err = d.awaitCredentialCurrent(ctx, before.CurrentTokenSHA256); err != nil {
				return errors.New("lifecycle_continuous_rotation_failed")
			}
			if !time.Now().Before(expiry) {
				return errors.New("lifecycle_rotation_missed_deadline")
			}
		}
		if scenario.drop != "" {
			if _, err = d.lifecycleFixture(ctx, "resume", nil, scenario.drop); err != nil {
				return err
			}
			if err = d.await(ctx, func() (bool, error) {
				interrupted, err = d.lifecycleFixture(ctx, "status", nil, "")
				return interrupted.Paused && interrupted.DroppedResponse == scenario.drop && interrupted.PendingOperationID != "" && interrupted.CurrentTokenSHA256 == before.CurrentTokenSHA256, err
			}); err != nil {
				return errors.New("lifecycle_interruption_not_observed")
			}
			var state, replacement string
			if d.db.QueryRowContext(ctx, `SELECT state,replacement_token_sha256 FROM connector_control_credential_operations WHERE installation_id=$1 AND operation_id=$2`, d.installationID, interrupted.PendingOperationID).Scan(&state, &replacement) != nil || replacement != interrupted.PendingReplacementSHA256 {
				return errors.New("lifecycle_interruption_not_durable")
			}
			want := "prepared"
			if scenario.drop == "confirm" {
				want = "committed"
			}
			if state != want {
				return errors.New("lifecycle_interruption_wrong_phase")
			}
			if scenario.recover {
				expiry = time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
				if err = d.arrangeCredentialExpiry(ctx, expiry, true); err != nil {
					return err
				}
			}
		}
		// Cross the original deadline using wall time. Normal rotation must
		// keep running throughout; interrupted cases keep the sender offline.
		if err = d.await(ctx, func() (bool, error) { return time.Now().After(expiry), nil }); err != nil {
			return errors.New("lifecycle_expiry_not_crossed")
		}
		if !continuous {
			if err = d.restartLifecycleController(ctx); err != nil {
				return err
			}
		}
		if scenario.recover {
			material, err := d.recoverExpiredCredential(ctx)
			if err != nil {
				return err
			}
			if scenario.drop == "prepare" && (material.SupersededOperationID != interrupted.PendingOperationID || material.PreviousTokenSHA256 != before.CurrentTokenSHA256) {
				return errors.New("lifecycle_expired_prepared_binding")
			}
		}
		if !continuous {
			if _, err = d.lifecycleFixture(ctx, "resume", nil, ""); err != nil {
				return err
			}
		}
		if err = d.awaitCredentialCurrent(ctx, before.CurrentTokenSHA256); err != nil {
			return errors.New("lifecycle_current_not_promoted")
		}
		var operationsAfter, unfinished int
		if d.db.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE state='prepared') FROM connector_control_credential_operations WHERE installation_id=$1`, d.installationID).Scan(&operationsAfter, &unfinished) != nil {
			return errors.New("lifecycle_operations_unavailable")
		}
		wantOperations := 1
		if scenario.recover && scenario.drop != "" {
			wantOperations = 2
		}
		if unfinished != 0 || operationsAfter != operationsBefore+wantOperations {
			return errors.New("lifecycle_duplicate_or_unfinished_operation")
		}
		if interrupted.PendingOperationID != "" {
			want := "committed"
			if scenario.recover && scenario.drop == "prepare" {
				want = "superseded"
			}
			var actual string
			if d.db.QueryRowContext(ctx, `SELECT state FROM connector_control_credential_operations WHERE operation_id=$1 AND installation_id=$2`, interrupted.PendingOperationID, d.installationID).Scan(&actual) != nil || actual != want {
				return errors.New("lifecycle_old_operation_changed")
			}
		}
		if err = d.verifyLifecycleData(ctx, invariant, oldRef); err != nil {
			return err
		}
	}
	d.advance("credential_revocation_boundary")
	return d.verifyCredentialRevocation(ctx, invariant, oldRef)
}

func (d *driver) expectLifecycleDenied(ctx context.Context, target string, body any, human bool) error {
	wire, err := json.Marshal(body)
	if err != nil {
		return errors.New("lifecycle_negative_request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(wire))
	if err != nil {
		return errors.New("lifecycle_negative_request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", d.originFor(target))
	if human {
		req.AddCookie(d.adminCookie)
		req.Header.Set("X-CSRF-Token", d.adminCSRF)
	}
	response, err := d.client.Do(req)
	if err != nil {
		return errors.New("lifecycle_negative_transport")
	}
	defer response.Body.Close()
	// Network failures, 404 (unwired route), 401 (bad login) and 5xx are not
	// proof that revocation blocks an otherwise authorized recovery.
	if response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusConflict && response.StatusCode != http.StatusGone {
		return errors.New("lifecycle_revocation_not_enforced")
	}
	return nil
}

func (d *driver) verifyCredentialRevocation(ctx context.Context, invariant, oldRef string) error {
	expiry := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
	if err := d.arrangeCredentialExpiry(ctx, expiry, false); err != nil {
		return err
	}
	if err := d.await(ctx, func() (bool, error) { return time.Now().After(expiry), nil }); err != nil {
		return errors.New("lifecycle_expiry_not_crossed")
	}
	path := d.input.XIControlOrigin + "/api/v1/connector-setups/" + d.installationID + "/credential-recovery"
	var material connectorcontrol.RecoveryMaterial
	if d.call(ctx, "POST", path, map[string]string{"request_id": uuid()}, d.adminCookie, d.adminCSRF, &material) != nil || !connectorcontrol.IsRecoveryTicket(material.Ticket) {
		return errors.New("lifecycle_revocation_ticket_unavailable")
	}
	var identity, before string
	if d.db.QueryRowContext(ctx, `SELECT service_identity_id,current_token_digest FROM connector_control_credentials WHERE installation_id=$1`, d.installationID).Scan(&identity, &before) != nil {
		return errors.New("lifecycle_lineage_unavailable")
	}
	if d.call(ctx, "POST", d.input.XIControlOrigin+"/api/v1/service-identities/"+identity+"/revoke", map[string]string{"reason": "Disposable installed lifecycle qualification"}, d.adminCookie, d.adminCSRF, nil) != nil {
		return errors.New("lifecycle_formal_revocation_failed")
	}
	if err := d.expectLifecycleDenied(ctx, path, map[string]string{"request_id": uuid()}, true); err != nil {
		return err
	}
	if err := d.expectLifecycleDenied(ctx, d.input.XIControlOrigin+"/api/v1/connector-control/credential-recoveries/resolve", map[string]string{"ticket": material.Ticket}, false); err != nil {
		return err
	}
	base := d.input.BaseURL + "/api/v2/organizations/" + d.input.OrgID + "/xinsight/setups/" + d.installationID + "/credential-recovery"
	if err := d.expectLifecycleDenied(ctx, base, map[string]string{"recovery_ticket": material.Ticket}, false); err != nil {
		return err
	}
	var stillRevoked bool
	if d.db.QueryRowContext(ctx, `SELECT i.status='revoked' AND c.current_token_digest=$2 FROM connector_control_credentials c JOIN service_identities i ON i.id=c.service_identity_id WHERE c.installation_id=$1`, d.installationID, before).Scan(&stillRevoked) != nil || !stillRevoked {
		return errors.New("lifecycle_revocation_reactivated")
	}
	if actual, err := d.lifecycleInvariant(ctx); err != nil || actual != invariant {
		return errors.New("lifecycle_revocation_changed_history")
	}
	return d.readSavedBody(ctx, oldRef, d.input.ExpectedContent.SHA256)
}

// Failure evidence is deliberately just fixed enums, booleans and counts.
// Credential bytes, hashes, installation IDs and URLs stay in the private VM.
func (d *driver) writeCredentialDiagnostic(ctx context.Context, out map[string]any) {
	out["credential_available"] = false
	var currentExpired, currentRevoked, identityExpired, identityRevoked bool
	var prepared, committed, superseded int
	err := d.db.QueryRowContext(ctx, `SELECT t.expires_at<=clock_timestamp(),t.revoked_at IS NOT NULL,
 i.expires_at<=clock_timestamp(),i.status='revoked',
 (SELECT count(*) FROM connector_control_credential_operations WHERE installation_id=c.installation_id AND state='prepared'),
 (SELECT count(*) FROM connector_control_credential_operations WHERE installation_id=c.installation_id AND state='committed'),
 (SELECT count(*) FROM connector_control_credential_operations WHERE installation_id=c.installation_id AND state='superseded')
 FROM connector_control_credentials c JOIN service_identity_tokens t ON t.token_digest=c.current_token_digest
 JOIN service_identities i ON i.id=c.service_identity_id WHERE c.installation_id=$1`, d.installationID).Scan(&currentExpired, &currentRevoked, &identityExpired, &identityRevoked, &prepared, &committed, &superseded)
	if err != nil {
		return
	}
	out["credential_available"] = true
	out["credential_current_expired"], out["credential_current_revoked"] = currentExpired, currentRevoked
	out["credential_identity_expired"], out["credential_identity_revoked"] = identityExpired, identityRevoked
	out["credential_prepared"], out["credential_committed"], out["credential_superseded"] = prepared, committed, superseded
}
