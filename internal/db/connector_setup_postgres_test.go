package db_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/jackc/pgx/v5/pgconn"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestConnectorSetupPostgresReplayRedeemCASAndForeignAuthority(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("PG fixture not selected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	createConnectorTestRoles(t, ctx, base)
	dsn := connectorPullMigrationDatabaseDSN(t, ctx, base)
	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("test database URL invalid")
	}
	query := parsedDSN.Query()
	query.Set("options", "-cTimeZone=Asia/Shanghai")
	parsedDSN.RawQuery = query.Encode()
	dsn = parsedDSN.String()
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var timezone string
	if err = database.QueryRowContext(ctx, `SHOW TimeZone`).Scan(&timezone); err != nil || timezone != "Asia/Shanghai" {
		t.Fatal("non-UTC fixture not active", err)
	}
	store := db.NewWorkspaceStore(database)
	deployment := "xid_AAAAAAAAAAAAAAAAAAAAAA"
	if err = store.SetConnectorDeploymentID(deployment); err != nil {
		t.Fatal(err)
	}
	owner, err := db.OpenSourceFactsStore(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	// Keep the precision/timezone fixture in the past even at the start of a second.
	now := time.Now().Add(-time.Second).In(time.FixedZone("Asia/Shanghai", 8*3600)).Truncate(time.Second).Add(123456789 * time.Nanosecond)
	put := connectorcontrol.SetupConsentPut{SchemaVersion: connectorcontrol.SetupConsentSchema, RequestID: "12345678-1234-4234-8234-123456789abc", Custody: &connectorcontrol.SetupCustody{Mode: "full_content", RetentionSeconds: 3600, ProjectQuotaBytes: 1048576, MaxItemBytes: 1024}}
	hash := "sha256:" + strings.Repeat("1", 64)
	input := connectorcontrol.CreateInvitationInput{ActorSubject: "xs_0123456789abcdef", Config: connectorcontrol.InvitationConfig{DeploymentID: deployment, XInsightOrigin: "https://xi.example", UniRouterInstallOrigin: "https://ur.example"}, Now: now, Random: bytes.NewReader(bytes.Repeat([]byte{0x11}, 64))}
	first, err := store.CreateSetupInvitation(ctx, input, put, hash)
	if err != nil {
		t.Fatal(err)
	}
	expectedExpiry := now.UTC().Truncate(time.Microsecond).Add(connectorcontrol.DefaultInvitationTTL)
	if first.Invitation.ExpiresAt.Location() != time.UTC || !first.Invitation.ExpiresAt.Equal(expectedExpiry) || !first.Consent.ExpiresAt.Equal(expectedExpiry) {
		t.Fatal("initial invitation is not canonical PG-precision UTC")
	}
	if !connectorcontrol.ValidSetupConsentView(first.Consent) {
		t.Fatal("invalid canonical consent")
	}
	input.Random = bytes.NewReader(bytes.Repeat([]byte{0x11}, 64))
	replay, err := store.CreateSetupInvitation(ctx, input, put, hash)
	if err != nil || first.Invitation.Token != replay.Invitation.Token || first.Invitation.InstallationID != replay.Invitation.InstallationID || first.Consent != replay.Consent {
		t.Fatal("lost response replay changed invitation", err)
	}
	firstWire, _ := json.Marshal(first)
	replayWire, _ := json.Marshal(replay)
	if !bytes.Equal(firstWire, replayWire) {
		t.Fatal("PG timezone/precision changed exact invitation replay wire")
	}
	var invites, audits int
	if err = database.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM connector_installation_invitations),(SELECT count(*) FROM access_audit_records)`).Scan(&invites, &audits); err != nil || invites != 1 || audits != 2 {
		t.Fatal("duplicate invitation/audit", invites, audits, err)
	}
	if _, err = store.ReadReviewCaptureDefault(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("unredeemed invitation applied policy", err)
	}
	if _, err = store.PrepareConnectorSetupPolicy(ctx, first.Invitation.InstallationID, input.ActorSubject, now); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("unredeemed setup allowed policy", err)
	}
	input.Random = bytes.NewReader(bytes.Repeat([]byte{0x11}, 64))
	if _, err = store.CreateSetupInvitation(ctx, input, put, "sha256:"+strings.Repeat("2", 64)); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("different request content replay accepted", err)
	}
	// A partial setup row and mutation of accepted consent must be rejected even by the owner.
	if _, err = database.ExecContext(ctx, `UPDATE connector_installations SET setup_actor_subject=NULL WHERE id=$1`, first.Invitation.InstallationID); err == nil {
		t.Fatal("setup immutable binding changed")
	}
	resolver, err := store.ResolveSetupInvitation(ctx, first.Invitation.Token, now)
	if err != nil || resolver.Consent != first.Consent || resolver.Invitation.InstallURL != first.Invitation.InstallURL || resolver.Invitation.InvitationID != first.Invitation.InvitationID {
		t.Fatal("invitation resolver changed consent", err)
	}
	resolvedWire, _ := json.Marshal(resolver)
	if !bytes.Equal(firstWire, resolvedWire) || resolver.Invitation.ExpiresAt.Location() != time.UTC {
		t.Fatal("resolve changed original frozen invitation bytes or UTC expiry")
	}
	if _, err = store.ResolveSetupInvitation(ctx, "xiiv1_"+strings.Repeat("A", 43), now); !errors.Is(err, connectorcontrol.ErrNotFound) {
		t.Fatal("wrong token resolved original package", err)
	}
	if _, err = store.ResolveSetupInvitation(ctx, first.Invitation.Token, first.Consent.ExpiresAt); !errors.Is(err, connectorcontrol.ErrExpired) {
		t.Fatal("expired invitation resolves", err)
	}
	redeem, err := store.RedeemInvitation(ctx, connectorcontrol.RedeemInvitationInput{InvitationToken: first.Invitation.Token, ProjectID: "synthetic-control-project", FinalEventScope: []string{connectorcontrol.CompletedEventType}, ConsentSHA256: "sha256:" + strings.Repeat("c", 64), ConfirmedAt: now, ControlToken: "xib_" + strings.Repeat("A", 43), ControlTokenExpires: now.Add(time.Hour)}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the real setup/HTTP installation-update serialization race.
	blocker, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	var blockerPID int
	if err = blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	if _, err = blocker.ExecContext(ctx, `UPDATE connector_installations SET last_agent_seen_at=$2 WHERE id=$1`, first.Invitation.InstallationID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	serialized := make(chan error, 1)
	go func() {
		_, err := store.PrepareConnectorSetupPolicy(ctx, first.Invitation.InstallationID, input.ActorSubject, now)
		serialized <- err
	}()
	waitScopeLock(t, ctx, database, fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND %d=ANY(pg_blocking_pids(pid)) AND query LIKE 'SELECT setup_actor_subject%%')`, blockerPID))
	if err = blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	var serialization *pgconn.PgError
	if err = <-serialized; !errors.As(err, &serialization) || serialization.Code != "40001" {
		t.Fatal("expected real rolled-back serialization conflict", err)
	}
	if _, err = store.ReadReviewCaptureDefault(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("serialization left a policy intent", err)
	}
	failedRecord, err := store.ReadConnectorSetup(ctx, first.Invitation.InstallationID)
	if err != nil || failedRecord.PolicyRevision != 0 || failedRecord.AppliedAt != nil || failedRecord.Consent != first.Consent {
		t.Fatal("serialization changed accepted intent/revision", err)
	}
	revision, err := store.PrepareConnectorSetupPolicy(ctx, first.Invitation.InstallationID, input.ActorSubject, now)
	if err != nil || revision != 1 {
		t.Fatal("setup policy target", revision, err)
	}
	if again, err := store.PrepareConnectorSetupPolicy(ctx, first.Invitation.InstallationID, input.ActorSubject, now); err != nil || again != 1 {
		t.Fatal("replay requested another policy revision", again, err)
	}
	policy, err := store.ReadReviewCaptureDefault(ctx)
	if err != nil || policy.DesiredRevision != 1 || policy.EffectiveRevision != 0 {
		t.Fatal("desired was presented as effective", policy, err)
	}
	if err = owner.ConfirmReviewCaptureDefault(ctx, 1, input.ActorSubject, now); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordConnectorSetupResult(ctx, first.Invitation.InstallationID, true, ""); err != nil {
		t.Fatal(err)
	}
	record, err := store.ReadConnectorSetup(ctx, first.Invitation.InstallationID)
	if err != nil || record.AppliedAt == nil || record.PolicyRevision != 1 {
		t.Fatal("lost confirm response not recoverable", err)
	}
	if err = store.CheckConnectorSetupControlAccess(ctx, first.Invitation.InstallationID, redeem.Installation.ControlServiceIdentityID, now); err != nil {
		t.Fatal(err)
	}
	if err = store.CheckConnectorSetupControlAccess(ctx, first.Invitation.InstallationID, "xsi_BBBBBBBBBBBBBBBBBBBBBB", now); !errors.Is(err, connectorcontrol.ErrSetupUnauthorized) {
		t.Fatal("other installation identity allowed", err)
	}
	if err = store.CheckConnectorSetupControlAccess(ctx, first.Invitation.InstallationID, redeem.Installation.ControlServiceIdentityID, now.Add(2*time.Hour)); !errors.Is(err, connectorcontrol.ErrSetupUnauthorized) {
		t.Fatal("expired control authority survived", err)
	}
	var identityCreated, identityExpiry time.Time
	if err = database.QueryRowContext(ctx, `SELECT created_at,expires_at FROM service_identities WHERE id=$1`, redeem.Installation.ControlServiceIdentityID).Scan(&identityCreated, &identityExpiry); err != nil {
		t.Fatal(err)
	}
	if _, err = database.ExecContext(ctx, `UPDATE service_identities SET created_at=$2,expires_at=$3 WHERE id=$1`, redeem.Installation.ControlServiceIdentityID, now.Add(-2*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if expired, readErr := store.ReadConnectorSetup(ctx, first.Invitation.InstallationID); readErr != nil || expired.ControlIdentityIssue != "control_identity_expired" {
		t.Fatal("read-only setup missed expired control identity", expired.ControlIdentityIssue, readErr)
	}
	if _, err = database.ExecContext(ctx, `UPDATE service_identities SET created_at=$2,expires_at=$3 WHERE id=$1`, redeem.Installation.ControlServiceIdentityID, identityCreated, identityExpiry); err != nil {
		t.Fatal(err)
	}
	if current, readErr := store.ReadConnectorSetup(ctx, first.Invitation.InstallationID); readErr != nil || current.ControlIdentityIssue != "" {
		t.Fatal("valid control identity remained blocked", current.ControlIdentityIssue, readErr)
	}
	foreign := db.NewWorkspaceStore(database)
	if err = foreign.SetConnectorDeploymentID("xid_BBBBBBBBBBBBBBBBBBBBBB"); err != nil {
		t.Fatal(err)
	}
	if _, err = foreign.ReadConnectorSetup(ctx, first.Invitation.InstallationID); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("foreign installation setup access", err)
	}
	if _, err = foreign.ResolveSetupInvitation(ctx, first.Invitation.Token, now); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("foreign invitation resolver", err)
	}
	if err = db.SuspendForeignRestoredAuthority(ctx, database, deployment, "xid_BBBBBBBBBBBBBBBBBBBBBB", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if revoked, readErr := store.ReadConnectorSetup(ctx, first.Invitation.InstallationID); readErr != nil || revoked.ControlIdentityIssue != "control_identity_revoked" {
		t.Fatal("read-only setup missed revoked control identity", revoked.ControlIdentityIssue, readErr)
	}
	if err = store.CheckConnectorSetupControlAccess(ctx, first.Invitation.InstallationID, redeem.Installation.ControlServiceIdentityID, now); !errors.Is(err, connectorcontrol.ErrSetupUnauthorized) {
		t.Fatal("restored old machine authority survived", err)
	}
	receiving, err := store.ReadConnectorSetupReceiving(ctx, first.Invitation.InstallationID, now)
	if err != nil || receiving.LastSuccessAt != nil || len(receiving.ProfilesReceived) != 0 {
		t.Fatal("empty selected streams claimed receiving", receiving, err)
	}
	// Current policy drift is never silently accepted by null-reuse consent.
	_, err = owner.RequestReviewCaptureDefault(ctx, db.ReviewCaptureSettings{Mode: "disabled", RetentionSeconds: 3600, ProjectQuotaBytes: 1048576, MaxItemBytes: 1024}, 1, input.ActorSubject, now)
	if err != nil {
		t.Fatal(err)
	}
	put.RequestID = "22345678-1234-4234-8234-123456789abc"
	put.Custody = nil
	put.ExpectedCustodyRevision = 2
	input.Random = bytes.NewReader(bytes.Repeat([]byte{0x22}, 64))
	if _, err = store.CreateSetupInvitation(ctx, input, put, hash); !errors.Is(err, reviewbody.ErrPolicy) {
		t.Fatal("null reused a pending policy", err)
	}
}

// Installation connection is the synthetic domain prestate; observation bytes,
// stream registration, page validation/commit and status reads use original PG paths.
func TestConnectorSetupReceivingPostgresActualObservationBoundaries(t *testing.T) {
	f := newSourceFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	deployment := "xid_AAAAAAAAAAAAAAAAAAAAAA"
	if err := f.workspace.SetConnectorDeploymentID(deployment); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../sourcefacts/contract/observations/golden-observations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Valid []struct {
			Canonical string `json:"canonical_utf8"`
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	documents := map[string][]json.RawMessage{}
	var sample sourcefacts.Observation
	for _, item := range fixture.Valid {
		observation, err := sourcefacts.DecodeObservation([]byte(item.Canonical))
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := sourcefacts.ObservationEnvelope([]byte(item.Canonical))
		if err != nil {
			t.Fatal(err)
		}
		documents[observation.Profile] = append(documents[observation.Profile], envelope)
		sample = observation
	}
	enrollment := func(source, org, project string) db.ManagedEnrollment {
		inv, _, err := f.workspace.CreateInvitation(f.ctx, connectorcontrol.CreateInvitationInput{ActorSubject: f.subject, Config: connectorcontrol.InvitationConfig{DeploymentID: deployment, XInsightOrigin: "https://xi.example", UniRouterInstallOrigin: "https://ur.example"}, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		tokenHash := sha256.Sum256([]byte(inv.InstallationID))
		_, err = f.workspace.RedeemInvitation(f.ctx, connectorcontrol.RedeemInvitationInput{InvitationToken: inv.Token, ProjectID: "synthetic-control-project", FinalEventScope: []string{connectorcontrol.CompletedEventType}, ConsentSHA256: sourcefacts.Hash([]byte("synthetic-consent")), ConfirmedAt: now, ControlToken: "xib_" + base64.RawURLEncoding.EncodeToString(tokenHash[:]), ControlTokenExpires: now.Add(time.Hour)}, now, nil)
		if err != nil {
			t.Fatal(err)
		}
		f.exec(`UPDATE connector_installations SET status='connected',observed_state='connected',source_instance=$2,source_instance_sha256=$3 WHERE id=$1`, inv.InstallationID, source, sourcefacts.Hash([]byte(source)))
		r := db.ManagedEnrollment{InstallationID: inv.InstallationID, EnrollmentID: "xise_1", SourceInstance: source, OrgID: org, ProjectID: project, Revision: "1", Status: "active"}
		if err = f.workspace.SaveManagedEnrollment(f.ctx, r, now); err != nil {
			t.Fatal(err)
		}
		return r
	}
	register := func(r db.ManagedEnrollment, system, generation, profile string, exact bool) sourcefacts.Stream {
		id := connectorcontrol.ManagedCollectionID(r.InstallationID, r.EnrollmentID, r.ProjectID, "profile:"+profile)
		if !exact {
			id = testUUID(t)
		}
		stream, err := f.consumerStore.RegisterSourceFactsStream(f.ctx, sourcefacts.Stream{ID: id, SourceSystemID: system, SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, Profile: profile, ScopeGeneration: generation})
		if err != nil {
			t.Fatal(err)
		}
		stream, err = f.consumerStore.ControlSourceFactsStream(f.ctx, id, "active", stream.Revision, false)
		if err != nil {
			t.Fatal(err)
		}
		return stream
	}
	status := func(r db.ManagedEnrollment, since time.Time) connectorcontrol.SetupReceiving {
		out, err := f.workspace.ReadConnectorSetupReceiving(f.ctx, r.InstallationID, since)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	original := enrollment(sample.SourceInstance, sample.OrgID, sample.ProjectID)
	streams := map[string]sourcefacts.Stream{}
	for _, profile := range sourcefacts.ObservationProfiles() {
		stream := register(original, f.source, "1", profile, true)
		page := sourcefacts.Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: sourcefacts.ConnectorID, ConnectorVersion: sourcefacts.ObservationConnectorVersion, ProducerInstance: sample.SourceInstance, GeneratedAt: sample.RecordedAt, Events: []json.RawMessage{}, NextCursor: "bootstrap", HighWatermark: "bootstrap"}
		if err = f.consumerStore.CommitSourceFactsPage(f.ctx, stream, page, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		stream, err = f.consumerStore.LoadSourceFactsStream(f.ctx, stream.ID)
		if err != nil {
			t.Fatal(err)
		}
		streams[profile] = stream
	}
	empty := status(original, now)
	if len(empty.ProfilesReceived) != 0 || empty.LastSuccessAt == nil {
		t.Fatal("successful empty bootstrap claimed observations", empty)
	}
	for profile, stream := range streams {
		page := sourcefacts.Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: sourcefacts.ConnectorID, ConnectorVersion: sourcefacts.ObservationConnectorVersion, ProducerInstance: sample.SourceInstance, GeneratedAt: sample.RecordedAt, Events: documents[profile], NextCursor: "received", HighWatermark: "received"}
		if err = f.consumerStore.CommitSourceFactsPage(f.ctx, stream, page, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	received := status(original, now)
	for _, profile := range sourcefacts.ObservationProfiles() {
		if !slices.Contains(received.ProfilesReceived, profile) {
			t.Fatal("real committed observation absent", profile, received)
		}
	}
	if len(received.ProfilesReceived) != len(sourcefacts.ObservationProfiles()) {
		t.Fatal("unexpected profile", received)
	}
	var persisted int
	if err = f.admin.QueryRow(`SELECT count(*) FROM source_observation_documents`).Scan(&persisted); err != nil || persisted != len(fixture.Valid) {
		t.Fatal("original observation commits missing", persisted, err)
	}
	if out := status(original, time.Now().UTC().Add(time.Second)); len(out.ProfilesReceived) != 0 || out.LastSuccessAt != nil {
		t.Fatal("old receipt crossed time boundary", out)
	}
	// A real original budget receipt follows the same exact-ID/digest boundary.
	budgetProfiles := []string{sourcefacts.BudgetDefinitionProfile, sourcefacts.BudgetSummaryProfile, sourcefacts.BudgetAlertProfile}
	budgetRaw := budgetFixtureReading(t, 987, budgetProfiles, func(m map[string]any) {
		m["source_instance"] = sample.SourceInstance
		for _, raw := range m["documents"].([]any) {
			raw.(map[string]any)["document"].(map[string]any)["source_instance"] = sample.SourceInstance
		}
	})
	budgetReading, err := sourcefacts.DecodeBudgetReading(budgetRaw)
	if err != nil {
		t.Fatal(err)
	}
	budgetEnrollment := enrollment(budgetReading.SourceInstance, budgetReading.OrgID, budgetReading.ProjectID)
	budgetCollection, err := f.consumerStore.RegisterBudgetCollection(f.ctx, sourcefacts.BudgetCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: budgetReading.SourceInstance, OrgID: budgetReading.OrgID, ProjectID: budgetReading.ProjectID, ScopeGeneration: "1", Profiles: budgetProfiles})
	if err != nil {
		t.Fatal(err)
	}
	budgetCollection, err = f.consumerStore.ControlBudgetCollection(f.ctx, budgetCollection.ID, "active", budgetCollection.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	budgetCfg := sourcefacts.ManagedConfig{SchemaVersion: sourcefacts.ManagedBudgetConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 10, Budgets: []sourcefacts.ManagedBudgetConfig{{CollectionID: budgetCollection.ID, SourceSystemID: f.source, SourceInstance: budgetReading.SourceInstance, OrgID: budgetReading.OrgID, ProjectID: budgetReading.ProjectID, ScopeGeneration: "1", Profiles: budgetProfiles, Endpoint: "https://ur.example" + sourcefacts.BudgetEndpointPath, KeyFile: "/etc/x-insight/synthetic-budget.key"}}}
	budgetWire, _ := json.Marshal(budgetCfg)
	budgetEnrollment.ConfigurationSHA256 = sourcefacts.Hash(budgetWire)
	if err = f.workspace.SaveManagedEnrollment(f.ctx, budgetEnrollment, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.CommitBudgetCatalog(f.ctx, budgetCollection, budgetFixtureCatalog(t, budgetReading), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	budgetCollection, err = f.consumerStore.LoadBudgetCollection(f.ctx, budgetCollection.ID)
	if err != nil {
		t.Fatal(err)
	}
	budgetStatus, err := f.workspace.ReadConnectorSetupReceivingWithOriginalConfig(f.ctx, budgetEnrollment.InstallationID, now, budgetCfg)
	if err != nil || len(budgetStatus.ProfilesReceived) != 0 {
		t.Fatal("catalog without readings claimed budget documents", budgetStatus, err)
	}
	if err = f.consumerStore.CommitBudgetReading(f.ctx, budgetCollection, budgetRaw, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	budgetStatus, err = f.workspace.ReadConnectorSetupReceivingWithOriginalConfig(f.ctx, budgetEnrollment.InstallationID, now, budgetCfg)
	if err != nil || len(budgetStatus.ProfilesReceived) != 3 {
		t.Fatal("digest-bound original budget receipt absent", budgetStatus, err)
	}
	if out := status(budgetEnrollment, now); len(out.ProfilesReceived) != 0 {
		t.Fatal("original budget accepted without root snapshot", out)
	}
	budgetCollection, err = f.consumerStore.LoadBudgetCollection(f.ctx, budgetCollection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.consumerStore.ControlBudgetCollection(f.ctx, budgetCollection.ID, "paused", budgetCollection.Revision, false); err != nil {
		t.Fatal(err)
	}
	budgetStatus, err = f.workspace.ReadConnectorSetupReceivingWithOriginalConfig(f.ctx, budgetEnrollment.InstallationID, now, budgetCfg)
	if err != nil || len(budgetStatus.ProfilesReceived) != 0 {
		t.Fatal("paused original budget counted", budgetStatus, err)
	}
	// Original random IDs require the already-persisted project config digest.
	adopted := enrollment(sample.SourceInstance, sample.OrgID, sample.ProjectID)
	originalCfg := sourcefacts.ManagedConfig{SchemaVersion: sourcefacts.ManagedObservationConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 10}
	adoptedStreams := map[string]sourcefacts.Stream{}
	for _, profile := range sourcefacts.ObservationProfiles() {
		stream := register(adopted, f.source, "3", profile, false)
		originalCfg.Profiles = append(originalCfg.Profiles, sourcefacts.ManagedProfileConfig{StreamID: stream.ID, SourceSystemID: f.source, SourceInstance: sample.SourceInstance, OrgID: sample.OrgID, ProjectID: sample.ProjectID, Profile: profile, ScopeGeneration: stream.ScopeGeneration, Endpoint: "https://ur.example" + sourcefacts.ManagedEndpointPath(profile), KeyFile: "/etc/x-insight/synthetic-original.key"})
		page := sourcefacts.Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: sourcefacts.ConnectorID, ConnectorVersion: sourcefacts.ObservationConnectorVersion, ProducerInstance: sample.SourceInstance, GeneratedAt: sample.RecordedAt, Events: []json.RawMessage{}, NextCursor: "original-start", HighWatermark: "original-start"}
		if err = f.consumerStore.CommitSourceFactsPage(f.ctx, stream, page, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		stream, err = f.consumerStore.LoadSourceFactsStream(f.ctx, stream.ID)
		if err != nil {
			t.Fatal(err)
		}
		page.Events = documents[profile]
		page.NextCursor = "original-received"
		page.HighWatermark = "original-received"
		if err = f.consumerStore.CommitSourceFactsPage(f.ctx, stream, page, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		adoptedStreams[profile] = stream
	}
	wire, err := json.Marshal(originalCfg)
	if err != nil {
		t.Fatal(err)
	}
	adopted.ConfigurationSHA256 = sourcefacts.Hash(wire)
	if err = f.workspace.SaveManagedEnrollment(f.ctx, adopted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if out := status(adopted, now); len(out.ProfilesReceived) != 0 {
		t.Fatal("random original IDs accepted without root snapshot", out)
	}
	readOriginal := func(r db.ManagedEnrollment, cfg sourcefacts.ManagedConfig, since time.Time) connectorcontrol.SetupReceiving {
		out, err := f.workspace.ReadConnectorSetupReceivingWithOriginalConfig(f.ctx, r.InstallationID, since, cfg)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := readOriginal(adopted, originalCfg, now); len(out.ProfilesReceived) != 5 || out.LastSuccessAt == nil {
		t.Fatal("digest-bound original IDs absent", out)
	}
	multi := originalCfg
	multi.Profiles = slices.Clone(originalCfg.Profiles)
	foreignProfile := originalCfg.Profiles[0]
	foreignProfile.StreamID = testUUID(t)
	foreignProfile.ProjectID = "999"
	multi.Profiles = append(multi.Profiles, foreignProfile)
	if out := readOriginal(adopted, multi, now); len(out.ProfilesReceived) != 5 {
		t.Fatal("exact multi-project subset did not retain original digest", out)
	}
	changed := originalCfg
	changed.RetryMaximumSeconds++
	if out := readOriginal(adopted, changed, now); len(out.ProfilesReceived) != 0 || out.LastSuccessAt != nil {
		t.Fatal("changed root config digest accepted", out)
	}
	changed = originalCfg
	changed.Profiles = slices.Clone(originalCfg.Profiles)
	changed.Profiles[0].ScopeGeneration = "99"
	if out := readOriginal(adopted, changed, now); len(out.ProfilesReceived) != 0 {
		t.Fatal("drifted root registration accepted", out)
	}
	// Even an exact saved digest cannot authorize a different live registration.
	changedWire, _ := json.Marshal(changed)
	adopted.Revision = "2"
	adopted.ConfigurationSHA256 = sourcefacts.Hash(changedWire)
	if err = f.workspace.SaveManagedEnrollment(f.ctx, adopted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if out := readOriginal(adopted, changed, now); len(out.ProfilesReceived) != 4 || slices.Contains(out.ProfilesReceived, changed.Profiles[0].Profile) {
		t.Fatal("live registration drift was counted", out)
	}
	adopted.Revision = "3"
	adopted.ConfigurationSHA256 = sourcefacts.Hash(wire)
	if err = f.workspace.SaveManagedEnrollment(f.ctx, adopted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if out := readOriginal(adopted, multi, time.Now().UTC().Add(time.Second)); len(out.ProfilesReceived) != 0 || out.LastSuccessAt != nil {
		t.Fatal("old original receipt crossed time boundary", out)
	}
	for _, stream := range adoptedStreams {
		live, err := f.consumerStore.LoadSourceFactsStream(f.ctx, stream.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.consumerStore.ControlSourceFactsStream(f.ctx, live.ID, "paused", live.Revision, false); err != nil {
			t.Fatal(err)
		}
	}
	if out := readOriginal(adopted, multi, now); len(out.ProfilesReceived) != 0 || out.LastSuccessAt != nil {
		t.Fatal("paused original stream counted", out)
	}
	adopted.Revision = "4"
	adopted.Status = "revoked"
	if err = f.workspace.SaveManagedEnrollment(f.ctx, adopted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if out := readOriginal(adopted, multi, now); len(out.ProfilesReceived) != 0 {
		t.Fatal("revoked original enrollment counted", out)
	}
	for _, negative := range []struct {
		name, source, org, project, system, generation string
		exact                                          bool
	}{
		{"other installation arbitrary stream", sample.SourceInstance, sample.OrgID, sample.ProjectID, f.source, "2", false},
		{"other source system", sample.SourceInstance, sample.OrgID, sample.ProjectID, testUUID(t), "1", true},
		{"other project", sample.SourceInstance, sample.OrgID, "999", f.source, "1", true},
		{"other org", sample.SourceInstance, "999", sample.ProjectID, f.source, "1", true},
		{"other instance", "foreign-source", sample.OrgID, sample.ProjectID, f.source, "1", true},
	} {
		r := enrollment(negative.source, negative.org, negative.project)
		for _, profile := range sourcefacts.ObservationProfiles() {
			register(r, negative.system, negative.generation, profile, negative.exact)
		}
		if out := status(r, now); len(out.ProfilesReceived) != 0 {
			t.Fatal("foreign/unbound receipt counted", negative.name, out)
		}
		if out := readOriginal(r, multi, now); len(out.ProfilesReceived) != 0 {
			t.Fatal("root config leaked to foreign/unbound enrollment", negative.name, out)
		}
	}
	original.Revision = "2"
	original.Status = "revoked"
	if err = f.workspace.SaveManagedEnrollment(f.ctx, original, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if out := status(original, now); len(out.ProfilesReceived) != 0 || out.LastSuccessAt != nil {
		t.Fatal("revoked enrollment still receiving", out)
	}
}

func TestRestoredManagedOriginalSelectionPostgresKeepsUnapprovedSourcePaused(t *testing.T) {
	f := newSourceFixture(t)
	cfg := sourcefacts.ManagedConfig{SchemaVersion: sourcefacts.ManagedContentConfigVersion, PollIntervalSeconds: 30, RetryMaximumSeconds: 300}
	contentBySource := map[string]sourcefacts.ContentCollection{}
	allProfiles := []sourcefacts.ManagedProfileConfig{}
	for _, scope := range []struct{ source, project string }{{"daily-source", "86"}, {"retired-source", "2"}} {
		for _, profile := range append([]string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile}, sourcefacts.ObservationProfiles()...) {
			s, err := f.consumerStore.RegisterSourceFactsStream(f.ctx, sourcefacts.Stream{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: scope.source, OrgID: "2", ProjectID: scope.project, Profile: profile, ScopeGeneration: "1"})
			if err != nil {
				t.Fatal(err)
			}
			s, err = f.consumerStore.ControlSourceFactsStream(f.ctx, s.ID, "active", s.Revision, false)
			if err != nil {
				t.Fatal(err)
			}
			version := sourcefacts.ConnectorVersion
			if sourcefacts.ValidObservationProfile(profile) {
				version = sourcefacts.ObservationConnectorVersion
			}
			page := sourcefacts.Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: sourcefacts.ConnectorID, ConnectorVersion: version, ProducerInstance: scope.source, GeneratedAt: "2026-10-05T00:00:00.000000Z", Events: []json.RawMessage{}, NextCursor: "original-stream-cursor", HighWatermark: "original-stream-cursor"}
			if err := f.consumerStore.CommitSourceFactsPage(f.ctx, s, page, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			allProfiles = append(allProfiles, sourcefacts.ManagedProfileConfig{StreamID: s.ID, SourceSystemID: s.SourceSystemID, SourceInstance: s.SourceInstance, OrgID: s.OrgID, ProjectID: s.ProjectID, Profile: s.Profile, ScopeGeneration: s.ScopeGeneration, Endpoint: "https://source.example" + sourcefacts.ManagedEndpointPath(profile), KeyFile: "/etc/x-insight/fixture.key"})
		}
		c, err := f.consumerStore.RegisterContentCollection(f.ctx, sourcefacts.ContentCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: scope.source, OrgID: "2", ProjectID: scope.project, CredentialID: "existing-content", RegistrationHash: sourcefacts.Hash([]byte(scope.source)), ContentScopeRevision: "scope-original", RecoveryEpoch: "1", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: sourcefacts.ContentBoundary{Time: "2026-10-05T00:00:00.000000Z", AfterSequence: "0"}})
		if err != nil {
			t.Fatal(err)
		}
		c, err = f.consumerStore.ControlContentCollection(f.ctx, c.ID, "active", c.Revision, false, c)
		if err != nil {
			t.Fatal(err)
		}
		page := sourcefacts.ContentManifestPage{SchemaVersion: sourcefacts.ContentManifestPageVersion, Profile: sourcefacts.ContentProfile, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, GeneratedAt: "2026-10-05T00:00:00.000000Z", ContentScopeRevision: c.ContentScopeRevision, RecoveryEpoch: c.RecoveryEpoch, CoverageFrom: c.CoverageFrom, RetentionFloor: c.CoverageFrom, AfterSequence: "0", LastSequence: "0", HighWatermarkSequence: "0", NextCursor: "original-content-cursor", HighWatermark: "original-content-cursor", Coverage: "bounded", Gaps: []sourcefacts.ContentGap{}, Items: []sourcefacts.ContentManifestEntry{}}
		raw, _ := json.Marshal(page)
		if err := f.consumerStore.CommitContentManifests(f.ctx, c, raw, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		contentBySource[scope.source] = c
		if scope.source != "daily-source" {
			continue
		}
		cfg.Profiles = append(cfg.Profiles, allProfiles[:8]...)
		cfg.Bindings = []sourcefacts.ManagedCredentialBinding{
			{Name: "daily-legacy", Scope: "legacy", SourceSystemID: f.source, SourceInstance: scope.source, OrgID: "2", ProjectID: scope.project, CredentialID: "original-facts", RegistrationHash: sourcefacts.Hash([]byte("original-facts")), KeyFile: "/etc/x-insight/fixture.key"},
			{Name: "daily-content", Scope: "content", SourceSystemID: f.source, SourceInstance: scope.source, OrgID: "2", ProjectID: scope.project, CredentialID: c.CredentialID, RegistrationHash: c.RegistrationHash, KeyFile: "/etc/x-insight/fixture-content.key"},
		}

		cfg.Contents = append(cfg.Contents, sourcefacts.ManagedContentConfig{CollectionID: c.ID, SourceSystemID: c.SourceSystemID, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, CredentialID: c.CredentialID, RegistrationHash: c.RegistrationHash, ContentScopeRevision: c.ContentScopeRevision, RecoveryEpoch: c.RecoveryEpoch, AllowedPurposes: c.AllowedPurposes, CoverageFrom: c.CoverageFrom, CredentialBinding: "daily-content", Endpoint: "https://source.example" + sourcefacts.ContentEndpointPath, KeyFile: "/etc/x-insight/fixture-content.key"})
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal("original config not valid", err)
	}
	wire, _ := json.Marshal(cfg)
	var decodeErr error
	cfg, decodeErr = sourcefacts.ParseManagedConfig(wire)
	if decodeErr != nil {
		t.Fatal("original config readback failed", decodeErr)
	}
	history := func() string {
		var body string
		if err := f.admin.QueryRow(`SELECT COALESCE(jsonb_agg(row ORDER BY row::text)::text,'[]') FROM(SELECT to_jsonb(t) AS row FROM source_fact_documents t) q`).Scan(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	immutable := history()
	if err := db.SuspendRestoredSourceFacts(f.ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	// Both distinct sources have the identical restore reason. Only the current
	// original config/authorized project projection selects IDs to acknowledge.
	selected := sourcefacts.ManagedProjectConfig(cfg, "daily-source", "2", "86", true, true)
	if len(selected.Profiles) != 8 || len(selected.Contents) != 1 {
		t.Fatal("exact original selection lost", len(selected.Profiles), len(selected.Contents))
	}
	for _, profile := range selected.Profiles {
		s, err := f.consumerStore.LoadSourceFactsStream(f.ctx, profile.StreamID)
		if err != nil || !profile.Matches(s) || s.Status != "paused" || !s.RequiresAttention || s.LastError != "restore_requires_authorization" {
			t.Fatal("restore prestate differs", err)
		}
		resumed, err := f.consumerStore.ControlSourceFactsStream(f.ctx, s.ID, "active", s.Revision, true)
		if err != nil || resumed.Cursor != s.Cursor || resumed.HighWatermark != s.HighWatermark || resumed.ScopeGeneration != s.ScopeGeneration {
			t.Fatal("checkpoint changed on acknowledgement", err)
		}
		if _, err := f.consumerStore.ControlSourceFactsStream(f.ctx, s.ID, "active", s.Revision, true); !errors.Is(err, sourcefacts.ErrStreamChanged) {
			t.Fatal("stale CAS accepted", err)
		}
	}
	c, err := f.consumerStore.LoadContentCollection(f.ctx, selected.Contents[0].CollectionID)
	if err != nil || c.LastError != "restore_requires_authorization" {
		t.Fatal(err)
	}
	drift := c
	drift.ProjectID = "87"
	if _, err := f.consumerStore.ControlContentCollection(f.ctx, c.ID, "active", c.Revision, true, drift); !errors.Is(err, sourcefacts.ErrScopeChanged) {
		t.Fatal("content scope drift accepted", err)
	}
	resumed, err := f.consumerStore.ControlContentCollection(f.ctx, c.ID, "active", c.Revision, true, c)
	if err != nil || resumed.Cursor != c.Cursor || resumed.HighWatermark != c.HighWatermark || resumed.LastSequence != c.LastSequence || resumed.HighWatermarkSequence != c.HighWatermarkSequence || resumed.RecoveryEpoch != c.RecoveryEpoch || resumed.CredentialID != c.CredentialID {
		t.Fatal("content identity/checkpoint changed", err)
	}
	for _, profile := range allProfiles {
		if profile.SourceInstance != "retired-source" {
			continue
		}
		old, err := f.consumerStore.LoadSourceFactsStream(f.ctx, profile.StreamID)
		if err != nil || old.Status != "paused" || !old.RequiresAttention || old.LastError != "restore_requires_authorization" {
			t.Fatal("retired source resumed", err)
		}
	}
	retired, err := f.consumerStore.LoadContentCollection(f.ctx, contentBySource["retired-source"].ID)
	if err != nil || retired.Status != "paused" || !retired.RequiresAttention || retired.LastError != "restore_requires_authorization" {
		t.Fatal("retired content resumed", err)
	}
	if history() != immutable {
		t.Fatal("original immutable source facts changed")
	}
}
