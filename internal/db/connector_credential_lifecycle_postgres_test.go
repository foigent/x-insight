package db_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsetup"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestControlCredentialLifecyclePostgres16InitialRotateConfirmRecover(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		if os.Getenv("XINSIGHT_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatal("PostgreSQL 16 lifecycle fixture is required")
		}
		t.Skip("PostgreSQL fixture not selected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	createConnectorTestRoles(t, ctx, base)
	dsn := connectorPullMigrationDatabaseDSN(t, ctx, base)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	adminConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err = (hostctl.PostgresManager{}).ApplyGrants(ctx, hostctl.DatabaseCredentials{AdminDSN: dsn, Admin: adminConfig}); err != nil {
		t.Fatal("formal role grants", err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := db.NewWorkspaceStore(database)
	workspaceStore, err := db.OpenWorkspaceStore(ctx, connectorRoleDSN(t, dsn, "xinsight_workspace", false))
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceStore.Close()
	workspaceDB, err := sql.Open("pgx", connectorRoleDSN(t, dsn, "xinsight_workspace", false))
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceDB.Close()
	var effectiveRole string
	var superuser bool
	if err = workspaceDB.QueryRowContext(ctx, `SELECT current_user,r.rolsuper FROM pg_roles r WHERE r.rolname=current_user`).Scan(&effectiveRole, &superuser); err != nil || effectiveRole != "xinsight_workspace" || superuser {
		t.Fatal("first redemption must use restricted workspace role", effectiveRole, superuser, err)
	}
	_, err = workspaceDB.ExecContext(ctx, `UPDATE connector_control_credentials SET updated_at=updated_at WHERE false`)
	var privilegeError *pgconn.PgError
	if !errors.As(err, &privilegeError) || privilegeError.Code != "42501" {
		t.Fatal("workspace must not directly write current credential", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	xi, ur := "https://xi.example", "https://ur.example"
	config := connectorcontrol.InvitationConfig{DeploymentID: "xid_AAAAAAAAAAAAAAAAAAAAAA", XInsightOrigin: xi, UniRouterInstallOrigin: ur}
	if err = store.SetConnectorDeploymentID(config.DeploymentID); err != nil {
		t.Fatal(err)
	}
	put := connectorcontrol.SetupConsentPut{SchemaVersion: connectorcontrol.SetupConsentSchema, RequestID: "12345678-1234-4234-8234-123456789abc", Custody: &connectorcontrol.SetupCustody{Mode: "full_content", RetentionSeconds: 3600, ProjectQuotaBytes: 1048576, MaxItemBytes: 1024}}
	created, err := store.CreateSetupInvitation(ctx, connectorcontrol.CreateInvitationInput{ActorSubject: "xs_0123456789abcdef", Config: config, Now: now, Random: bytes.NewReader(bytes.Repeat([]byte{0x11}, 64))}, put, "sha256:"+strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	inv := created.Invitation
	boot := connectorcontrol.ReceiverAdoptionBootstrap{InstallationID: inv.InstallationID, OrgID: 2, ProjectID: 86, SourceInstance: "synthetic-source", SourceBindingSHA256: "sha256:" + strings.Repeat("c", 64), FeedKeyID: "105", AllowedIP: "192.0.2.10/32", KeyPrefix: "ur-xf1-0123456789abc", RegistrationOperationID: strings.Repeat("2", 32), VerifierScheme: "receiver_sha256_v1"}
	boot.SourceBindingSHA256, err = connectorcontrol.SourceBindingDigest(config.DeploymentID, boot)
	if err != nil {
		t.Fatal(err)
	}
	bootSHA, _, err := connectorcontrol.CanonicalSHA256(boot)
	if err != nil {
		t.Fatal(err)
	}
	old := "xib_" + strings.Repeat("A", 43)
	redeem := connectorcontrol.RedeemInvitationInputV2{SchemaVersion: connectorcontrol.RedeemInvitationSchemaV2, RedeemInvitationInput: connectorcontrol.RedeemInvitationInput{InvitationToken: inv.Token, ProjectID: "synthetic-control-project", FinalEventScope: []string{connectorcontrol.CompletedEventType}, ConsentSHA256: boot.SourceBindingSHA256, ConfirmedAt: now, ControlToken: old, ControlTokenExpires: now.Add(5 * time.Hour)}, ReceiverAdoptionBootstrap: boot, ReceiverAdoptionBootstrapSHA256: bootSHA}
	installed, err := workspaceStore.RedeemAdoptionInvitation(ctx, redeem, now, nil)
	if err != nil {
		t.Fatal("initial redeem", err)
	}
	identity := installed.Installation.ControlServiceIdentityID
	oldDigest := connectorcontrol.RawSHA256(old)
	var current string
	if err = database.QueryRowContext(ctx, `SELECT current_token_digest FROM connector_control_credentials WHERE installation_id=$1`, inv.InstallationID).Scan(&current); err != nil || current != oldDigest {
		t.Fatal("initial pointer", current, err)
	}
	preflight := func() hostctl.ControlCredentialStatus {
		t.Helper()
		if e := (hostctl.PostgresManager{}).VerifyControlCredentialLifecycle(ctx, hostctl.DatabaseCredentials{AdminDSN: dsn}); e != nil {
			t.Fatal("read-only preflight", e)
		}
		rows, e := (hostctl.PostgresManager{}).InspectControlCredentialLifecycle(ctx, hostctl.DatabaseCredentials{AdminDSN: dsn}, time.Now().UTC())
		if e != nil || len(rows) != 1 || rows[0].InstallationID != inv.InstallationID || rows[0].SourceBindingSHA256 != boot.SourceBindingSHA256 || rows[0].IdentityExpiresAt == nil || rows[0].CurrentTokenExpiresAt == nil {
			t.Fatal("read-only runtime status", rows, e)
		}
		return rows[0]
	}
	if state := preflight(); state.IngressState != "ready" {
		t.Fatal("initial runtime", state)
	}
	makeRequest := func(kind, op, previous, replacement string) connectorcontrol.CredentialRequest {
		return connectorcontrol.CredentialRequest{SchemaVersion: connectorcontrol.CredentialRequestSchema, Kind: kind, InstallationID: inv.InstallationID, DeploymentID: config.DeploymentID, ProjectID: redeem.ProjectID, FinalEventScope: redeem.FinalEventScope, SourceBindingSHA256: boot.SourceBindingSHA256, XInsightOrigin: xi, UniRouterOrigin: ur, OperationID: op, PreviousTokenSHA256: previous, ReplacementToken: replacement}
	}
	newToken := "xib_" + strings.Repeat("B", 43)
	rotate := makeRequest("rotate", "xcr_"+strings.Repeat("A", 22), oldDigest, newToken)
	for _, tc := range []struct {
		name   string
		change func(*connectorcontrol.CredentialRequest)
	}{
		{"source_binding", func(r *connectorcontrol.CredentialRequest) {
			r.SourceBindingSHA256 = "sha256:" + strings.Repeat("f", 64)
		}},
		{"xi_origin", func(r *connectorcontrol.CredentialRequest) { r.XInsightOrigin = "https://other-xi.example" }},
		{"ur_origin", func(r *connectorcontrol.CredentialRequest) { r.UniRouterOrigin = "https://other-ur.example" }},
		{"project", func(r *connectorcontrol.CredentialRequest) { r.ProjectID = "other-project" }},
	} {
		t.Run("reject_"+tc.name, func(t *testing.T) {
			changed := rotate
			tc.change(&changed)
			if _, e := store.PrepareControlCredential(ctx, changed, identity, oldDigest, time.Now().UTC(), nil); !errors.Is(e, connectorcontrol.ErrConflict) {
				t.Fatal("foreign binding accepted", e)
			}
		})
	}
	prepared, err := store.PrepareControlCredential(ctx, rotate, identity, oldDigest, time.Now().UTC(), nil)
	if err != nil || prepared.State != "prepared" {
		t.Fatal("rotate prepare", err)
	}
	if setup, e := store.ReadConnectorSetup(ctx, inv.InstallationID); e != nil || setup.ControlCredentialIssue != "control_credential_pending" {
		t.Fatal("setup pending diagnostic", setup.ControlCredentialIssue, e)
	}
	if _, err = store.PrepareControlCredential(ctx, rotate, identity, oldDigest, time.Now().UTC(), nil); err != nil {
		t.Fatal("exact replay", err)
	}
	if _, err = store.ReadControlCredentialOperation(ctx, inv.InstallationID, rotate.OperationID, identity, connectorcontrol.RawSHA256(newToken), time.Now().UTC()); err != nil {
		t.Fatal("pending query", err)
	}
	other := makeRequest("rotate", "xcr_"+strings.Repeat("C", 22), oldDigest, "xib_"+strings.Repeat("D", 43))
	if _, err = store.PrepareControlCredential(ctx, other, identity, oldDigest, time.Now().UTC(), nil); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("second pending", err)
	}
	replacedSecret := rotate
	replacedSecret.ReplacementToken = "xib_" + strings.Repeat("E", 43)
	if _, err = store.PrepareControlCredential(ctx, replacedSecret, identity, oldDigest, time.Now().UTC(), nil); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("same operation with different secret", err)
	}
	confirm := connectorcontrol.CredentialConfirm{SchemaVersion: connectorcontrol.CredentialRequestSchema, OperationID: rotate.OperationID, RequestSHA256: prepared.RequestSHA256}
	committed, err := store.ConfirmControlCredential(ctx, inv.InstallationID, confirm, identity, connectorcontrol.RawSHA256(newToken), time.Now().UTC())
	if err != nil || committed.State != "committed" {
		t.Fatal("confirm", err)
	}
	if _, err = store.ConfirmControlCredential(ctx, inv.InstallationID, confirm, identity, connectorcontrol.RawSHA256(newToken), time.Now().UTC()); err != nil {
		t.Fatal("confirm replay", err)
	}
	if err = database.QueryRowContext(ctx, `SELECT current_token_digest FROM connector_control_credentials WHERE installation_id=$1`, inv.InstallationID).Scan(&current); err != nil || current != connectorcontrol.RawSHA256(newToken) {
		t.Fatal("confirmed pointer", current, err)
	}
	if state := preflight(); state.IngressState != "ready" {
		t.Fatal("rotated runtime", state)
	}
	if setup, e := store.ReadConnectorSetup(ctx, inv.InstallationID); e != nil || setup.ControlCredentialIssue != "" {
		t.Fatal("setup rotated diagnostic", setup.ControlCredentialIssue, e)
	}
	var revoked bool
	if err = database.QueryRowContext(ctx, `SELECT revoked_at IS NOT NULL FROM service_identity_tokens WHERE token_digest=$1`, oldDigest).Scan(&revoked); err != nil || !revoked {
		t.Fatal("old not revoked", err)
	}
	if _, err = store.AuthenticateServiceToken(ctx, old, time.Now().UTC()); !errors.Is(err, workspace.ErrCredentialRevoked) {
		t.Fatal("old bearer authenticated", err)
	}
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{AgentAccessStore: store, ConnectorConfig: config, Now: time.Now})
	oldRequest := httptest.NewRequest(http.MethodGet, "https://xi.example/api/v1/connector-control/installations/"+inv.InstallationID+"/credentials/operations/"+rotate.OperationID, nil)
	oldRequest.Header.Set("Authorization", "Bearer "+old)
	oldResponse := httptest.NewRecorder()
	handler.ServeHTTP(oldResponse, oldRequest)
	if oldResponse.Code != http.StatusUnauthorized || !strings.Contains(oldResponse.Body.String(), `"code":"agent_credential_revoked"`) {
		t.Fatal("old bearer HTTP denial", oldResponse.Code, oldResponse.Body.String())
	}
	// The request starts while the token is valid but must wait for its row
	// lock. Crossing expiry during that wait cannot authorize a new prepare.
	if _, err = database.ExecContext(ctx, `UPDATE service_identity_tokens SET expires_at=clock_timestamp()+interval '250 milliseconds' WHERE token_digest=$1`, current); err != nil {
		t.Fatal(err)
	}
	blocker, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var locked string
	if err = blocker.QueryRowContext(ctx, `SELECT token_digest FROM service_identity_tokens WHERE token_digest=$1 FOR UPDATE`, current).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	late := makeRequest("rotate", "xcr_"+strings.Repeat("Z", 22), current, "xib_"+strings.Repeat("Y", 43))
	lateResult := make(chan error, 1)
	go func() {
		_, e := store.PrepareControlCredential(ctx, late, identity, current, time.Now().UTC(), nil)
		lateResult <- e
	}()
	time.Sleep(350 * time.Millisecond)
	if err = blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	if e := <-lateResult; !errors.Is(e, connectorcontrol.ErrConflict) {
		t.Fatal("lock-wait expiry accepted", e)
	}
	// Expire exactly the current lineage; a human ticket and old secret are both
	// required to restore this same identity, with no new installation.
	if _, err = database.ExecContext(ctx, `UPDATE service_identity_tokens SET expires_at=created_at+interval '1 microsecond' WHERE token_digest=$1`, current); err != nil {
		t.Fatal(err)
	}
	if _, err = database.ExecContext(ctx, `UPDATE service_identities SET status='expired',expires_at=created_at+interval '1 microsecond' WHERE id=$1`, identity); err != nil {
		t.Fatal(err)
	}
	if state := preflight(); state.IngressState != "recovery_required" || state.Reason != "control_credential_expired" {
		t.Fatal("expired runtime", state)
	}
	if setup, e := store.ReadConnectorSetup(ctx, inv.InstallationID); e != nil || setup.ControlCredentialIssue != "control_credential_expired" {
		t.Fatal("setup expired diagnostic", setup.ControlCredentialIssue, e)
	}
	ticket := "xirt_" + strings.Repeat("E", 43)
	if _, err = store.IssueControlCredentialRecovery(ctx, inv.InstallationID, "12345678-1234-4234-8234-123456789aba", "xs_0123456789abcdef", "xrr_"+strings.Repeat("Z", 22), "xirt_"+strings.Repeat("Z", 43), time.Now().UTC(), func(string) error { return errors.New("permission removed") }); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("removed human permission issued ticket", err)
	}
	material, err := store.IssueControlCredentialRecovery(ctx, inv.InstallationID, "12345678-1234-4234-8234-123456789abc", "xs_0123456789abcdef", "xrr_"+strings.Repeat("F", 22), ticket, time.Now().UTC(), func(string) error { return nil })
	if err != nil || material.Ticket != ticket || material.RecoverURL == "" {
		t.Fatal("issue recovery", err)
	}
	resolved, err := store.ResolveControlCredentialRecovery(ctx, ticket, time.Now().UTC())
	if err != nil || resolved.Ticket != "" || resolved.PreviousTokenSHA256 != current {
		t.Fatal("resolve", err)
	}
	recoveredToken := "xib_" + strings.Repeat("G", 43)
	recover := makeRequest("recover", "xcr_"+strings.Repeat("H", 22), current, recoveredToken)
	recover.RecoveryID = material.RecoveryID
	recover.RecoveryTicket = ticket
	recover.PreviousToken = newToken
	empty := ""
	recover.SupersededOperationID = &empty
	recover.SupersededReplacementTokenSHA256 = &empty
	if _, err = store.PrepareControlCredential(ctx, recover, "", "", time.Now().UTC(), func(string) error { return errors.New("permission removed") }); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("removed human permission redeemed ticket", err)
	}
	got, err := store.PrepareControlCredential(ctx, recover, "", "", time.Now().UTC(), func(string) error { return nil })
	if err != nil || got.ControlIdentityID != identity || got.State != "prepared" {
		t.Fatal("recover", err)
	}
	if _, err = store.PrepareControlCredential(ctx, recover, "", "", time.Now().UTC(), func(string) error { return nil }); err != nil {
		t.Fatal("recovery replay", err)
	}
	if _, err = store.ResolveControlCredentialRecovery(ctx, ticket, time.Now().UTC()); err == nil {
		t.Fatal("consumed ticket resolved")
	}
	// A second recovery must bind and supersede the exact expired pending
	// replacement; its old operation cannot later be queried or confirmed.
	if _, err = database.ExecContext(ctx, `UPDATE service_identity_tokens SET expires_at=created_at+interval '1 microsecond' WHERE token_digest=$1`, connectorcontrol.RawSHA256(recoveredToken)); err != nil {
		t.Fatal(err)
	}
	if _, err = database.ExecContext(ctx, `UPDATE connector_control_credential_operations SET replacement_expires_at=created_at+interval '1 microsecond' WHERE operation_id=$1`, recover.OperationID); err != nil {
		t.Fatal(err)
	}
	ticket2 := "xirt_" + strings.Repeat("I", 43)
	material2, err := store.IssueControlCredentialRecovery(ctx, inv.InstallationID, "12345678-1234-4234-8234-123456789abd", "xs_0123456789abcdef", "xrr_"+strings.Repeat("J", 22), ticket2, time.Now().UTC(), func(string) error { return nil })
	if err != nil || material2.SupersededOperationID != recover.OperationID || material2.SupersededReplacementTokenSHA256 != connectorcontrol.RawSHA256(recoveredToken) {
		t.Fatal("stale pending ticket", err)
	}
	recovery2 := makeRequest("recover", "xcr_"+strings.Repeat("K", 22), current, "xib_"+strings.Repeat("L", 43))
	recovery2.RecoveryID = material2.RecoveryID
	recovery2.RecoveryTicket = ticket2
	recovery2.PreviousToken = newToken
	recovery2.SupersededOperationID = &material2.SupersededOperationID
	recovery2.SupersededReplacementTokenSHA256 = &material2.SupersededReplacementTokenSHA256
	if _, err = store.PrepareControlCredential(ctx, recovery2, "", "", time.Now().UTC(), func(string) error { return nil }); err != nil {
		t.Fatal("supersede pending", err)
	}
	if state := preflight(); state.IngressState != "recovery_required" && state.IngressState != "pending_continuable" {
		t.Fatal("pending recovery runtime", state)
	}
	if _, err = store.ReadControlCredentialOperation(ctx, inv.InstallationID, recover.OperationID, identity, connectorcontrol.RawSHA256(recoveredToken), time.Now().UTC()); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("superseded query", err)
	}
	// Reconstruct an old managed source with no authoritative pointer but
	// several token rows. The migration preflight must refuse to guess lineage.
	if _, err = database.ExecContext(ctx, `DELETE FROM connector_control_recovery_tickets WHERE installation_id=$1`, inv.InstallationID); err != nil {
		t.Fatal(err)
	}
	if _, err = database.ExecContext(ctx, `DELETE FROM connector_control_credential_operations WHERE installation_id=$1`, inv.InstallationID); err != nil {
		t.Fatal(err)
	}
	if _, err = database.ExecContext(ctx, `DELETE FROM connector_control_credentials WHERE installation_id=$1`, inv.InstallationID); err != nil {
		t.Fatal(err)
	}
	if err = (hostctl.PostgresManager{}).VerifyControlCredentialLifecycle(ctx, hostctl.DatabaseCredentials{AdminDSN: dsn}); err == nil {
		t.Fatal("ambiguous old managed lineage passed preflight")
	}
}

// Each terminal fence gets its own installed identity so an earlier denial
// cannot accidentally be the reason a later operation is rejected.
func TestControlCredentialTerminalFencesPostgres16(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		if os.Getenv("XINSIGHT_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatal("PostgreSQL 16 lifecycle fixture is required")
		}
		t.Skip("PostgreSQL fixture not selected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	dsn := connectorPullMigrationDatabaseDSN(t, ctx, base)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := db.NewWorkspaceStore(database)
	config := connectorcontrol.InvitationConfig{DeploymentID: "xid_AAAAAAAAAAAAAAAAAAAAAA", XInsightOrigin: "https://xi.example", UniRouterInstallOrigin: "https://ur.example"}
	if err := store.SetConnectorDeploymentID(config.DeploymentID); err != nil {
		t.Fatal(err)
	}
	for n, tc := range []struct {
		name, phase, fence string
	}{
		{"revoked_identity_prepare", "prepare", "identity"},
		{"revoked_current_prepare", "prepare", "current"},
		{"missing_poll_ack_prepare", "prepare", "permissions"},
		{"disconnected_prepare", "prepare", "installation"},
		{"revoked_identity_confirm", "confirm", "identity"},
		{"revoked_current_confirm", "confirm", "current"},
		{"revoked_replacement_confirm", "confirm", "replacement"},
		{"missing_poll_ack_confirm", "confirm", "permissions"},
		{"disconnected_confirm", "confirm", "installation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Microsecond)
			requestID := fmt.Sprintf("12345678-1234-4234-8234-%012x", n+1)
			put := connectorcontrol.SetupConsentPut{SchemaVersion: connectorcontrol.SetupConsentSchema, RequestID: requestID, Custody: &connectorcontrol.SetupCustody{Mode: "full_content", RetentionSeconds: 3600, ProjectQuotaBytes: 1048576, MaxItemBytes: 1024}}
			created, e := store.CreateSetupInvitation(ctx, connectorcontrol.CreateInvitationInput{ActorSubject: "xs_0123456789abcdef", Config: config, Now: now, Random: bytes.NewReader(bytes.Repeat([]byte{byte(n + 1)}, 64))}, put, "sha256:"+strings.Repeat("1", 64))
			if e != nil {
				t.Fatal(e)
			}
			inv := created.Invitation
			boot := connectorcontrol.ReceiverAdoptionBootstrap{InstallationID: inv.InstallationID, OrgID: 2, ProjectID: 86, SourceInstance: fmt.Sprintf("synthetic-source-%d", n), SourceBindingSHA256: "sha256:" + strings.Repeat("c", 64), FeedKeyID: "105", AllowedIP: "192.0.2.10/32", KeyPrefix: "ur-xf1-0123456789abc", RegistrationOperationID: strings.Repeat("2", 32), VerifierScheme: "receiver_sha256_v1"}
			boot.SourceBindingSHA256, e = connectorcontrol.SourceBindingDigest(config.DeploymentID, boot)
			if e != nil {
				t.Fatal(e)
			}
			bootSHA, _, e := connectorcontrol.CanonicalSHA256(boot)
			if e != nil {
				t.Fatal(e)
			}
			old := "xib_" + strings.Repeat(string(rune('A'+n)), 43)
			redeem := connectorcontrol.RedeemInvitationInputV2{SchemaVersion: connectorcontrol.RedeemInvitationSchemaV2, RedeemInvitationInput: connectorcontrol.RedeemInvitationInput{InvitationToken: inv.Token, ProjectID: "synthetic-control-project", FinalEventScope: []string{connectorcontrol.CompletedEventType}, ConsentSHA256: boot.SourceBindingSHA256, ConfirmedAt: now, ControlToken: old, ControlTokenExpires: now.Add(5 * time.Hour)}, ReceiverAdoptionBootstrap: boot, ReceiverAdoptionBootstrapSHA256: bootSHA}
			installed, e := store.RedeemAdoptionInvitation(ctx, redeem, now, nil)
			if e != nil {
				t.Fatal(e)
			}
			identity := installed.Installation.ControlServiceIdentityID
			oldDigest := connectorcontrol.RawSHA256(old)
			replacement := "xib_" + strings.Repeat(string(rune('H'+n)), 43)
			req := connectorcontrol.CredentialRequest{SchemaVersion: connectorcontrol.CredentialRequestSchema, Kind: "rotate", InstallationID: inv.InstallationID, DeploymentID: config.DeploymentID, ProjectID: redeem.ProjectID, FinalEventScope: redeem.FinalEventScope, SourceBindingSHA256: boot.SourceBindingSHA256, XInsightOrigin: config.XInsightOrigin, UniRouterOrigin: config.UniRouterInstallOrigin, OperationID: "xcr_" + strings.Repeat(string(rune('A'+n)), 22), PreviousTokenSHA256: oldDigest, ReplacementToken: replacement}
			var prepared connectorcontrol.CredentialReceipt
			if tc.phase == "confirm" {
				prepared, e = store.PrepareControlCredential(ctx, req, identity, oldDigest, time.Now().UTC(), nil)
				if e != nil || prepared.State != "prepared" {
					t.Fatal("positive prepare", e)
				}
			}
			switch tc.fence {
			case "identity":
				_, e = database.ExecContext(ctx, `UPDATE service_identities SET status='revoked',revoked_at=clock_timestamp(),revoke_reason='isolated lifecycle test' WHERE id=$1`, identity)
			case "current":
				_, e = database.ExecContext(ctx, `UPDATE service_identity_tokens SET revoked_at=clock_timestamp() WHERE token_digest=$1`, oldDigest)
			case "replacement":
				_, e = database.ExecContext(ctx, `UPDATE service_identity_tokens SET revoked_at=clock_timestamp() WHERE token_digest=$1`, connectorcontrol.RawSHA256(replacement))
			case "permissions":
				// The normal schema forbids weakening a system identity. Break
				// only this disposable fixture's check to exercise the runtime
				// permission guard against a damaged or legacy record.
				_, e = database.ExecContext(ctx, `ALTER TABLE service_identities DROP CONSTRAINT IF EXISTS service_identities_permissions_check`)
				if e == nil {
					_, e = database.ExecContext(ctx, `UPDATE service_identities SET permissions=ARRAY['connector.control.poll']::text[] WHERE id=$1`, identity)
				}
			case "installation":
				_, e = database.ExecContext(ctx, `UPDATE connector_installations SET status='disconnected',desired_state='disconnected',observed_state='disconnected',updated_at=clock_timestamp() WHERE id=$1`, inv.InstallationID)
			}
			if e != nil {
				t.Fatal("arrange terminal fence", e)
			}
			if tc.phase == "prepare" {
				_, e = store.PrepareControlCredential(ctx, req, identity, oldDigest, time.Now().UTC(), nil)
			} else {
				confirm := connectorcontrol.CredentialConfirm{SchemaVersion: connectorcontrol.CredentialRequestSchema, OperationID: req.OperationID, RequestSHA256: prepared.RequestSHA256}
				_, e = store.ConfirmControlCredential(ctx, inv.InstallationID, confirm, identity, connectorcontrol.RawSHA256(replacement), time.Now().UTC())
			}
			if !errors.Is(e, connectorcontrol.ErrConflict) {
				t.Fatal("terminal fence accepted", e)
			}
			var current string
			var operations int
			if e = database.QueryRowContext(ctx, `SELECT current_token_digest FROM connector_control_credentials WHERE installation_id=$1`, inv.InstallationID).Scan(&current); e != nil || current != oldDigest {
				t.Fatal("denial changed current", current, e)
			}
			if e = database.QueryRowContext(ctx, `SELECT count(*) FROM connector_control_credential_operations WHERE installation_id=$1`, inv.InstallationID).Scan(&operations); e != nil || (tc.phase == "prepare" && operations != 0) || (tc.phase == "confirm" && operations != 1) {
				t.Fatal("denial changed operations", operations, e)
			}
		})
	}
}

func TestControlCredentialV1InitialLineagePostgres16(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		if os.Getenv("XINSIGHT_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatal("PostgreSQL 16 lifecycle fixture is required")
		}
		t.Skip("PostgreSQL fixture not selected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	createConnectorTestRoles(t, ctx, base)
	dsn := connectorPullMigrationDatabaseDSN(t, ctx, base)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	adminConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err = (hostctl.PostgresManager{}).ApplyGrants(ctx, hostctl.DatabaseCredentials{AdminDSN: dsn, Admin: adminConfig}); err != nil {
		t.Fatal(err)
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	admin := db.NewWorkspaceStore(adminDB)
	workspaceDSN := connectorRoleDSN(t, dsn, "xinsight_workspace", false)
	workspaceDB, err := sql.Open("pgx", workspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceDB.Close()
	var role string
	var superuser bool
	if err = workspaceDB.QueryRowContext(ctx, `SELECT current_user,r.rolsuper FROM pg_roles r WHERE r.rolname=current_user`).Scan(&role, &superuser); err != nil || role != "xinsight_workspace" || superuser {
		t.Fatal("v1 redemption did not use restricted workspace role", role, superuser, err)
	}
	workspace := db.NewWorkspaceStore(workspaceDB)
	now := time.Now().UTC().Truncate(time.Second)
	config := connectorcontrol.InvitationConfig{DeploymentID: "xid_AAAAAAAAAAAAAAAAAAAAAA", XInsightOrigin: "https://xi.example", UniRouterInstallOrigin: "https://ur.example"}
	if err = admin.SetConnectorDeploymentID(config.DeploymentID); err != nil {
		t.Fatal(err)
	}
	if err = workspace.SetConnectorDeploymentID(config.DeploymentID); err != nil {
		t.Fatal(err)
	}
	put := connectorcontrol.SetupConsentPut{SchemaVersion: connectorcontrol.SetupConsentSchema, RequestID: "12345678-1234-4234-8234-123456789abc", Custody: &connectorcontrol.SetupCustody{Mode: "full_content", RetentionSeconds: 3600, ProjectQuotaBytes: 1048576, MaxItemBytes: 1024}}
	created, err := admin.CreateSetupInvitation(ctx, connectorcontrol.CreateInvitationInput{ActorSubject: "xs_0123456789abcdef", Config: config, Now: now, Random: bytes.NewReader(bytes.Repeat([]byte{0x51}, 64))}, put, "sha256:"+strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	inv := created.Invitation
	token := "xib_" + strings.Repeat("Q", 43)
	redeem := connectorcontrol.RedeemInvitationInput{InvitationToken: inv.Token, ProjectID: "synthetic-control-project", FinalEventScope: []string{connectorcontrol.CompletedEventType}, ConsentSHA256: "sha256:" + strings.Repeat("a", 64), ConfirmedAt: now.Add(time.Second), ControlToken: token, ControlTokenExpires: now.Add(5 * time.Hour)}
	registered, err := workspace.RedeemInvitation(ctx, redeem, time.Now().UTC(), nil)
	if err != nil {
		t.Fatal("restricted v1 redemption", err)
	}
	var current, identity string
	if err = adminDB.QueryRowContext(ctx, `SELECT service_identity_id,current_token_digest FROM connector_control_credentials WHERE installation_id=$1`, inv.InstallationID).Scan(&identity, &current); err != nil || identity != registered.Installation.ControlServiceIdentityID || current != connectorcontrol.RawSHA256(token) {
		t.Fatal("v1 same-transaction current lineage", identity, current, err)
	}
	replayed, err := workspace.RedeemInvitation(ctx, redeem, time.Now().UTC(), nil)
	if err != nil || replayed.Installation.InstallationID != inv.InstallationID || replayed.Operation.OperationID != registered.Operation.OperationID {
		t.Fatal("v1 exact replay", err)
	}
	var count int
	if err = adminDB.QueryRowContext(ctx, `SELECT count(*) FROM connector_control_credentials WHERE installation_id=$1 AND current_token_digest=$2`, inv.InstallationID, current).Scan(&count); err != nil || count != 1 {
		t.Fatal("v1 replay changed lineage", count, err)
	}
	pending, err := admin.ListPendingConnectorSetups(ctx)
	if err != nil || len(pending) != 1 || pending[0] != inv.InstallationID {
		t.Fatal("new install not selected for formal setup maintenance", pending, err)
	}
	policyOwner, err := db.OpenSourceFactsStore(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer policyOwner.Close()
	manager := connectorsetup.Manager{Store: admin, Host: lifecycleSetupHost{}, Policy: reviewpolicycontrol.Manager{Store: policyOwner, EnsureCustody: func(context.Context) error { return nil }}, Now: time.Now}
	if err = manager.Tick(ctx); err != nil {
		t.Fatal("fresh setup maintenance", err)
	}
	setup, err := admin.ReadConnectorSetup(ctx, inv.InstallationID)
	if err != nil || setup.PolicyRevision == 0 || setup.AppliedAt == nil || setup.ErrorCode != "" || setup.ControlCredentialIssue != "" {
		t.Fatal("fresh setup did not apply after v1 redemption", setup, err)
	}
	policy, err := admin.ReadReviewCaptureDefault(ctx)
	if err != nil || policy.EffectiveRevision != setup.PolicyRevision || policy.Effective == nil || policy.Effective.Mode != "full_content" {
		t.Fatal("fresh custody default not effective", policy, err)
	}
}

// Exercise the deployed predecessor rather than constructing a schema 45
// installation: migration must adopt its one expired original token, and the
// ordinary two-administrator recovery must retain the original authority.
func TestControlCredentialSchema44ExpiredPredecessorPostgres16(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		if os.Getenv("XINSIGHT_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatal("PostgreSQL 16 lifecycle fixture is required")
		}
		t.Skip("PostgreSQL fixture not selected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	createConnectorTestRoles(t, ctx, base)
	dsn := connectorPullMigrationDatabaseDSN(t, ctx, base)
	migrations := filepath.Join("..", "..", "migrations")
	if applied, err := db.RunMigrations(ctx, dsn, connectorMigrationPrefixDir(t, migrations, "000044")); err != nil || applied != 44 {
		t.Fatalf("schema44 predecessor applied=%d err=%v", applied, err)
	}
	adminConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	roleCredentials := hostctl.DatabaseCredentials{AdminDSN: dsn, Admin: adminConfig}
	roleCredentials.ReaderDSN = connectorRoleDSN(t, dsn, "xinsight_reader", true)
	roleCredentials.WorkspaceDSN = connectorRoleDSN(t, dsn, "xinsight_workspace", false)
	roleCredentials.ImporterDSN = connectorRoleDSN(t, dsn, "xinsight_importer", false)
	if roleCredentials.Reader, err = pgx.ParseConfig(roleCredentials.ReaderDSN); err != nil {
		t.Fatal(err)
	}
	if roleCredentials.Workspace, err = pgx.ParseConfig(roleCredentials.WorkspaceDSN); err != nil {
		t.Fatal(err)
	}
	if roleCredentials.Importer, err = pgx.ParseConfig(roleCredentials.ImporterDSN); err != nil {
		t.Fatal(err)
	}
	if err = (hostctl.PostgresManager{}).ApplyGrants(ctx, roleCredentials); err != nil {
		t.Fatal("schema44 grants", err)
	}
	if err = (hostctl.PostgresManager{}).VerifyRoleBoundaries(ctx, roleCredentials); err != nil {
		t.Fatal("schema44 role boundary", err)
	}
	if err = (hostctl.PostgresManager{}).VerifyUpgradeSourceRoleBoundaries(ctx, roleCredentials, "0.2.0-dev.fpv15"); err != nil {
		t.Fatal("schema44 predecessor source boundary", err)
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	admin := db.NewWorkspaceStore(adminDB)
	workspaceDB, err := sql.Open("pgx", connectorRoleDSN(t, dsn, "xinsight_workspace", false))
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceDB.Close()
	var role string
	if err = workspaceDB.QueryRowContext(ctx, `SELECT current_user`).Scan(&role); err != nil || role != "xinsight_workspace" {
		t.Fatal("schema44 redeem role", role, err)
	}
	workspaceStore := db.NewWorkspaceStore(workspaceDB)
	now := time.Now().UTC().Truncate(time.Second)
	config := connectorcontrol.InvitationConfig{DeploymentID: "xid_AAAAAAAAAAAAAAAAAAAAAA", XInsightOrigin: "https://xi.example", UniRouterInstallOrigin: "https://ur.example"}
	if err = admin.SetConnectorDeploymentID(config.DeploymentID); err != nil {
		t.Fatal(err)
	}
	if err = workspaceStore.SetConnectorDeploymentID(config.DeploymentID); err != nil {
		t.Fatal(err)
	}
	put := connectorcontrol.SetupConsentPut{SchemaVersion: connectorcontrol.SetupConsentSchema, RequestID: "12345678-1234-4234-8234-123456789abc", Custody: &connectorcontrol.SetupCustody{Mode: "full_content", RetentionSeconds: 3600, ProjectQuotaBytes: 1048576, MaxItemBytes: 1024}}
	created, err := admin.CreateSetupInvitation(ctx, connectorcontrol.CreateInvitationInput{ActorSubject: "xs_0123456789abcdef", Config: config, Now: now, Random: bytes.NewReader(bytes.Repeat([]byte{0x61}, 64))}, put, "sha256:"+strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	old := "xib_" + strings.Repeat("M", 43)
	redeem := connectorcontrol.RedeemInvitationInput{InvitationToken: created.Invitation.Token, ProjectID: "synthetic-control-project", FinalEventScope: []string{connectorcontrol.CompletedEventType}, ConsentSHA256: "sha256:" + strings.Repeat("a", 64), ConfirmedAt: now.Add(time.Second), ControlToken: old, ControlTokenExpires: now.Add(5 * time.Hour)}
	installed, err := workspaceStore.RedeemInvitation(ctx, redeem, time.Now().UTC(), nil)
	if err != nil {
		t.Fatal("real schema44 restricted v1 redeem", err)
	}
	id, identity, oldDigest := created.Invitation.InstallationID, installed.Installation.ControlServiceIdentityID, connectorcontrol.RawSHA256(old)
	var lifecycleExists bool
	if err = adminDB.QueryRowContext(ctx, `SELECT to_regclass('public.connector_control_credentials') IS NOT NULL`).Scan(&lifecycleExists); err != nil || lifecycleExists {
		t.Fatal("predecessor was not schema44", lifecycleExists, err)
	}
	var binding string
	if err = adminDB.QueryRowContext(ctx, `SELECT source_binding_sha256 FROM connector_installation_source_bindings WHERE installation_id=$1`, id).Scan(&binding); err != nil || binding != redeem.ConsentSHA256 {
		t.Fatal("original source binding", binding, err)
	}
	// Reproduce the actual elapsed predecessor without inventing a current
	// pointer. The old installation, acknowledgement and control operation are
	// then frozen and compared through migration and recovery.
	if _, err = adminDB.ExecContext(ctx, `UPDATE service_identity_tokens SET expires_at=created_at+interval '1 microsecond' WHERE service_identity_id=$1 AND token_digest=$2`, identity, oldDigest); err != nil {
		t.Fatal(err)
	}
	if _, err = adminDB.ExecContext(ctx, `UPDATE service_identities SET status='expired',expires_at=created_at+interval '1 microsecond' WHERE id=$1`, identity); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var original string
		err := adminDB.QueryRowContext(ctx, `SELECT jsonb_build_object('installation',to_jsonb(c),'invitation',to_jsonb(v),'binding',to_jsonb(b),'operation',to_jsonb(o))::text
FROM connector_installations c
JOIN connector_installation_invitations v ON v.installation_id=c.id AND v.status='consumed'
JOIN connector_installation_source_bindings b ON b.installation_id=c.id
JOIN connector_control_operations o ON o.installation_id=c.id AND o.operation_id=$2
WHERE c.id=$1`, id, installed.Operation.OperationID).Scan(&original)
		if err != nil {
			t.Fatal("original authority snapshot", err)
		}
		return original
	}
	original := snapshot()
	preflight := func() hostctl.ControlCredentialStatus {
		t.Helper()
		credentials := hostctl.DatabaseCredentials{AdminDSN: dsn}
		if e := (hostctl.PostgresManager{}).VerifyControlCredentialLifecycle(ctx, credentials); e != nil {
			t.Fatal("unique predecessor preflight", e)
		}
		statuses, e := (hostctl.PostgresManager{}).InspectControlCredentialLifecycle(ctx, credentials, time.Now().UTC())
		if e != nil || len(statuses) != 1 || statuses[0].InstallationID != id || statuses[0].SourceBindingSHA256 != binding {
			t.Fatal("predecessor runtime inspection", statuses, e)
		}
		return statuses[0]
	}
	if status := preflight(); status.IngressState != "recovery_required" || status.Reason != "control_credential_expired" {
		t.Fatal("schema44 expired predecessor", status)
	}
	// Exercise the exact lifecycle migration before the independent sync-control
	// migration. Applying every pending migration here would hide schema45's
	// own backfill and privilege assertions behind the current schema version.
	if applied, e := db.RunMigrations(ctx, dsn, connectorMigrationPrefixDir(t, migrations, "000045")); e != nil || applied != 1 {
		t.Fatalf("schema44 to45 applied=%d err=%v", applied, e)
	}
	if err = (hostctl.PostgresManager{}).ApplyGrants(ctx, roleCredentials); err != nil {
		t.Fatal("schema45 grants", err)
	}
	if err = (hostctl.PostgresManager{}).VerifyRoleBoundaries(ctx, roleCredentials); err != nil {
		t.Fatal("schema45 role boundary", err)
	}
	for _, probe := range []struct{ name, grant string }{
		{"reader_initializer", `GRANT EXECUTE ON FUNCTION public.xinsight_initialize_connector_control_credential(text,text) TO xinsight_reader`},
		{"public_initializer", `GRANT EXECUTE ON FUNCTION public.xinsight_initialize_connector_control_credential(text,text) TO PUBLIC`},
		{"delegable_initializer", `GRANT EXECUTE ON FUNCTION public.xinsight_initialize_connector_control_credential(text,text) TO xinsight_workspace WITH GRANT OPTION`},
	} {
		t.Run(probe.name, func(t *testing.T) {
			if _, e := adminDB.ExecContext(ctx, probe.grant); e != nil {
				t.Fatal(e)
			}
			if e := (hostctl.PostgresManager{}).VerifyRoleBoundaries(ctx, roleCredentials); e == nil {
				t.Fatal("schema45 accepted extra initializer authority")
			}
			if e := (hostctl.PostgresManager{}).ApplyGrants(ctx, roleCredentials); e != nil {
				t.Fatal("restore schema45 grants", e)
			}
			if e := (hostctl.PostgresManager{}).VerifyRoleBoundaries(ctx, roleCredentials); e != nil {
				t.Fatal("restored schema45 role boundary", e)
			}
		})
	}
	var current, boundIdentity string
	if err = adminDB.QueryRowContext(ctx, `SELECT service_identity_id,current_token_digest FROM connector_control_credentials WHERE installation_id=$1`, id).Scan(&boundIdentity, &current); err != nil || boundIdentity != identity || current != oldDigest {
		t.Fatal("migration did not backfill exact old lineage", boundIdentity, current, err)
	}
	if snapshot() != original {
		t.Fatal("schema45 changed original installation, binding or acknowledgement")
	}
	if status := preflight(); status.IngressState != "recovery_required" || status.Reason != "control_credential_expired" {
		t.Fatal("schema45 expired predecessor", status)
	}
	ticket := "xirt_" + strings.Repeat("M", 43)
	material, err := admin.IssueControlCredentialRecovery(ctx, id, "12345678-1234-4234-8234-123456789abd", "xs_0123456789abcdef", "xrr_"+strings.Repeat("M", 22), ticket, time.Now().UTC(), func(string) error { return nil })
	if err != nil || material.PreviousTokenSHA256 != oldDigest || material.SourceBindingSHA256 != binding {
		t.Fatal("old installation recovery ticket", err)
	}
	resolved, err := admin.ResolveControlCredentialRecovery(ctx, ticket, time.Now().UTC())
	if err != nil || resolved.PreviousTokenSHA256 != oldDigest || resolved.InstallationID != id {
		t.Fatal("old installation recovery resolution", err)
	}
	replacement := "xib_" + strings.Repeat("N", 43)
	empty := ""
	recover := connectorcontrol.CredentialRequest{SchemaVersion: connectorcontrol.CredentialRequestSchema, Kind: "recover", InstallationID: id, DeploymentID: config.DeploymentID, ProjectID: redeem.ProjectID, FinalEventScope: redeem.FinalEventScope, SourceBindingSHA256: binding, XInsightOrigin: config.XInsightOrigin, UniRouterOrigin: config.UniRouterInstallOrigin, OperationID: "xcr_" + strings.Repeat("M", 22), PreviousTokenSHA256: oldDigest, PreviousToken: old, ReplacementToken: replacement, RecoveryID: material.RecoveryID, RecoveryTicket: ticket, SupersededOperationID: &empty, SupersededReplacementTokenSHA256: &empty}
	prepared, err := admin.PrepareControlCredential(ctx, recover, "", "", time.Now().UTC(), func(string) error { return nil })
	if err != nil || prepared.State != "prepared" || prepared.ControlIdentityID != identity {
		t.Fatal("same-installation recovery prepare", err)
	}
	confirm := connectorcontrol.CredentialConfirm{SchemaVersion: connectorcontrol.CredentialRequestSchema, OperationID: recover.OperationID, RequestSHA256: prepared.RequestSHA256}
	committed, err := admin.ConfirmControlCredential(ctx, id, confirm, identity, connectorcontrol.RawSHA256(replacement), time.Now().UTC())
	if err != nil || committed.State != "committed" {
		t.Fatal("same-installation recovery confirm", err)
	}
	if err = adminDB.QueryRowContext(ctx, `SELECT service_identity_id,current_token_digest FROM connector_control_credentials WHERE installation_id=$1`, id).Scan(&boundIdentity, &current); err != nil || boundIdentity != identity || current != connectorcontrol.RawSHA256(replacement) {
		t.Fatal("recovered predecessor lineage", boundIdentity, current, err)
	}
	if snapshot() != original {
		t.Fatal("recovery changed original installation, binding or acknowledgement")
	}
	if status := preflight(); status.IngressState != "ready" {
		t.Fatal("recovered predecessor not ready", status)
	}
	if _, err = admin.AuthenticateServiceToken(ctx, old, time.Now().UTC()); !errors.Is(err, workspace.ErrCredentialRevoked) {
		t.Fatal("expired original bearer not revoked", err)
	}
	if _, err = admin.AuthenticateServiceToken(ctx, replacement, time.Now().UTC()); err != nil {
		t.Fatal("recovered bearer unavailable", err)
	}
	var schema45Ledger string
	if err = adminDB.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m`).Scan(&schema45Ledger); err != nil {
		t.Fatal("schema45 ledger", err)
	}
	if applied, e := db.RunMigrations(ctx, dsn, migrations); e != nil || applied != 1 {
		t.Fatalf("schema45 to46 applied=%d err=%v", applied, e)
	}
	if applied, e := db.RunMigrations(ctx, dsn, migrations); e != nil || applied != 0 {
		t.Fatalf("schema46 no-op applied=%d err=%v", applied, e)
	}
	var retainedLedger string
	if err = adminDB.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m WHERE version<'000046'`).Scan(&retainedLedger); err != nil || retainedLedger != schema45Ledger {
		t.Fatal("schema46 changed prior migration records", err)
	}
	var auto, setupUsed bool
	var cadence *int
	var revision int64
	if err = adminDB.QueryRowContext(ctx, `SELECT auto_sync,setup_used,cadence_seconds,revision FROM connector_sync_controls WHERE installation_id=$1`, id).Scan(&auto, &setupUsed, &cadence, &revision); err != nil || !auto || setupUsed || cadence != nil || revision != 1 {
		t.Fatal("schema46 did not preserve predecessor collection intent", err)
	}
	if snapshot() != original {
		t.Fatal("schema46 changed original installation, binding or acknowledgement")
	}
	if err = adminDB.QueryRowContext(ctx, `SELECT service_identity_id,current_token_digest FROM connector_control_credentials WHERE installation_id=$1`, id).Scan(&boundIdentity, &current); err != nil || boundIdentity != identity || current != connectorcontrol.RawSHA256(replacement) {
		t.Fatal("schema46 changed recovered credential lineage", err)
	}
	if err = (hostctl.PostgresManager{}).ApplyGrants(ctx, roleCredentials); err != nil {
		t.Fatal("schema46 grants", err)
	}
	if err = (hostctl.PostgresManager{}).VerifyRoleBoundaries(ctx, roleCredentials); err != nil {
		t.Fatal("schema46 role boundary", err)
	}
	if status := preflight(); status.IngressState != "ready" {
		t.Fatal("schema46 recovered predecessor not ready", status)
	}
}

type lifecycleSetupHost struct{ connectorsetup.Host }

func (lifecycleSetupHost) AuthorizeSetupActor(string, bool) error       { return nil }
func (lifecycleSetupHost) EnsureReviewBodyLoaded(context.Context) error { return nil }
func (lifecycleSetupHost) ReviewBodyLoaded(context.Context) error       { return nil }
func (lifecycleSetupHost) ReadOriginalSourceConfig(context.Context) (sourcefacts.ManagedConfig, bool, error) {
	return sourcefacts.ManagedConfig{}, false, nil
}
func (lifecycleSetupHost) WithSetupInvitationSeed(context.Context, string, string, connectorcontrol.SetupConsentPut, bool, func(hostctl.SetupInvitationSeed, io.Reader) error) error {
	return connectorcontrol.ErrInvalid
}
