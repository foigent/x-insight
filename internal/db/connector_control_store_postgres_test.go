package db_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestConnectorControlPostgreSQL16LifecycleIsIdempotentAndCheckpointNeutral(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	createConnectorTestRoles(t, ctx, baseDSN)
	dsn := connectorPullMigrationDatabaseDSN(t, ctx, baseDSN)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run current migrations: %v", err)
	}
	ledgerDatabase, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer ledgerDatabase.Close()
	var postgresMajor, migrationCount, dirtyMigrations int
	if err := ledgerDatabase.QueryRowContext(ctx, `
SELECT
  current_setting('server_version_num')::integer / 10000,
  count(*),
  count(*) FILTER (WHERE dirty)
FROM schema_migrations`).Scan(&postgresMajor, &migrationCount, &dirtyMigrations); err != nil {
		t.Fatal(err)
	}
	expectedMigrations, err := strconv.Atoi(buildinfo.DatabaseSchemaMax)
	if err != nil {
		t.Fatal(err)
	}
	if postgresMajor != 16 || migrationCount != expectedMigrations || dirtyMigrations != 0 {
		t.Fatalf(
			"connector PG fixture major=%d migrations=%d dirty=%d",
			postgresMajor,
			migrationCount,
			dirtyMigrations,
		)
	}
	adminConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	readerDSN := connectorRoleDSN(t, dsn, "xinsight_reader", true)
	workspaceDSN := connectorRoleDSN(t, dsn, "xinsight_workspace", false)
	importerDSN := connectorRoleDSN(t, dsn, "xinsight_importer", false)
	readerConfig, err := pgx.ParseConfig(readerDSN)
	if err != nil {
		t.Fatal(err)
	}
	workspaceConfig, err := pgx.ParseConfig(workspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	importerConfig, err := pgx.ParseConfig(importerDSN)
	if err != nil {
		t.Fatal(err)
	}
	credentials := hostctl.DatabaseCredentials{
		ReaderDSN: readerDSN, WorkspaceDSN: workspaceDSN,
		ImporterDSN: importerDSN, AdminDSN: dsn,
		Reader: readerConfig, Workspace: workspaceConfig,
		Importer: importerConfig, Admin: adminConfig,
	}
	manager := hostctl.PostgresManager{}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		t.Fatalf("apply connector database grants: %v", err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, credentials); err != nil {
		t.Fatalf("verify connector database grants: %v", err)
	}
	issuerStore, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer issuerStore.Close()
	store, err := db.OpenWorkspaceStore(ctx, workspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)
	workspaceDatabase, err := sql.Open("pgx", workspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceDatabase.Close()
	var workspaceCurrentUser string
	if err := workspaceDatabase.QueryRowContext(ctx, `SELECT current_user`).Scan(&workspaceCurrentUser); err != nil {
		t.Fatal(err)
	}
	if workspaceCurrentUser != "xinsight_workspace" {
		t.Fatalf("workspace fixture current_user=%q", workspaceCurrentUser)
	}
	assertConnectorSQLRejected(t, ctx, workspaceDatabase, `
INSERT INTO connector_installations (
  id, deployment_id, connector_type, requested_event_scope,
  status, desired_state, observed_state, created_at, updated_at
) VALUES (
  'xci_ZZZZZZZZZZZZZZZZZZZZZZ', 'xid_AAAAAAAAAAAAAAAAAAAAAA', 'unirouter',
  ARRAY['model.request.completed']::text[], 'pending', 'active', 'awaiting_consent', now(), now()
)`)
	assertConnectorSQLRejected(t, ctx, workspaceDatabase, `
INSERT INTO connector_installation_invitations (
  id, installation_id, nonce_digest, token_prefix, audience,
  xinsight_origin, unirouter_install_origin, requested_event_scope,
  status, created_by_subject, created_at, expires_at
) VALUES (
  'xii_ZZZZZZZZZZZZZZZZZZZZZZ', 'xci_ZZZZZZZZZZZZZZZZZZZZZZ',
  'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
  'xiiv1_ZZZZZZ', 'unirouter', 'https://xinsight.example', 'https://unirouter.example',
  ARRAY['model.request.completed']::text[], 'active', 'xs_0123456789abcdef', now(), now() + interval '15 minutes'
)`)
	assertConnectorTableCount(t, ctx, dsn, "connector_installations", 0)
	assertConnectorTableCount(t, ctx, dsn, "connector_installation_invitations", 0)

	invitation, pending, err := issuerStore.CreateInvitation(ctx, connectorcontrol.CreateInvitationInput{
		ActorSubject: "xs_0123456789abcdef",
		Config: connectorcontrol.InvitationConfig{
			DeploymentID:           "xid_AAAAAAAAAAAAAAAAAAAAAA",
			XInsightOrigin:         "https://xinsight.example",
			UniRouterInstallOrigin: "https://unirouter.example",
		},
		Now: now, Random: bytes.NewReader(bytes.Repeat([]byte{0x11}, 64)),
	})
	if err != nil || pending.ObservedState != "awaiting_consent" || invitation.Token == "" ||
		strings.Contains(invitation.InstallURL, "project") {
		t.Fatalf("pending=%+v invitation=%+v err=%v", pending, invitation, err)
	}
	controlToken := "xib_" + strings.Repeat("A", 43)
	redeem := connectorcontrol.RedeemInvitationInput{
		InvitationToken:     invitation.Token,
		ProjectID:           "project.fixture",
		FinalEventScope:     []string{connectorcontrol.CompletedEventType},
		ConsentSHA256:       "sha256:" + strings.Repeat("a", 64),
		ConfirmedAt:         now.Add(10 * time.Second),
		ControlToken:        controlToken,
		ControlTokenExpires: now.Add(24 * time.Hour),
	}
	acknowledgement := connectorcontrol.InstallationAcknowledgement{
		SchemaVersion:  connectorcontrol.AcknowledgementSchema,
		InstallationID: invitation.InstallationID, DeploymentID: invitation.DeploymentID,
		ProjectID: redeem.ProjectID, FinalEventScope: redeem.FinalEventScope,
		ConsentSHA256: redeem.ConsentSHA256, ConfirmedAt: redeem.ConfirmedAt,
	}
	_, acknowledgementPayload, err := connectorcontrol.CanonicalSHA256(acknowledgement)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := callConnectorInvitationRedemption(
		ctx,
		workspaceDatabase,
		"xiiv1_"+strings.Repeat("Z", 43),
		acknowledgementPayload,
		controlToken,
		redeem.ControlTokenExpires,
	); err == nil {
		t.Fatal("workspace direct redemption accepted a missing invitation")
	}
	assertConnectorPrincipalCounts(t, ctx, dsn, 0, 0)

	rollback, err := workspaceDatabase.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var rolledBackInstallationID, rolledBackOperationID string
	err = rollback.QueryRowContext(ctx, `
SELECT installation_id, operation_id
FROM public.xinsight_redeem_connector_invitation($1, $2, $3, $4)`,
		invitation.Token, string(acknowledgementPayload), controlToken, redeem.ControlTokenExpires,
	).Scan(&rolledBackInstallationID, &rolledBackOperationID)
	if err != nil {
		_ = rollback.Rollback()
		t.Fatalf("workspace exact redemption before rollback: %v", err)
	}
	if err := rollback.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertConnectorPrincipalCounts(t, ctx, dsn, 0, 0)

	if _, _, err := callConnectorInvitationRedemption(
		ctx,
		workspaceDatabase,
		invitation.Token,
		acknowledgementPayload,
		controlToken,
		now.Add(connectorcontrol.MaximumControlTokenTTL+time.Hour),
	); err == nil {
		t.Fatal("workspace direct redemption expanded the control-token TTL")
	}
	assertConnectorPrincipalCounts(t, ctx, dsn, 0, 0)

	invalidChronology := redeem
	invalidChronology.ConfirmedAt = now.Add(-time.Second)
	if _, err := store.RedeemInvitation(
		ctx, invalidChronology, now.Add(time.Minute), bytes.NewReader(bytes.Repeat([]byte{0x21}, 32)),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("invitation accepted pre-creation consent: %v", err)
	}
	registered, err := store.RedeemInvitation(
		ctx, redeem, time.Now().UTC(), bytes.NewReader(bytes.Repeat([]byte{0x22}, 32)),
	)
	if err != nil || registered.Installation.ObservedState != "agent_registered" ||
		registered.Operation.Kind != "connect" || registered.Operation.Status != "pending" {
		t.Fatalf("registered=%+v err=%v", registered, err)
	}
	var originalCurrent, originalIdentity string
	if err := ledgerDatabase.QueryRowContext(ctx, `SELECT service_identity_id,current_token_digest FROM connector_control_credentials WHERE installation_id=$1`, invitation.InstallationID).Scan(&originalIdentity, &originalCurrent); err != nil || originalIdentity != registered.Installation.ControlServiceIdentityID || originalCurrent != connectorcontrol.RawSHA256(controlToken) {
		t.Fatalf("restricted v1 redemption missing same-transaction current lineage: identity=%q digest=%q err=%v", originalIdentity, originalCurrent, err)
	}
	replayed, err := store.RedeemInvitation(
		ctx, redeem, time.Now().UTC(), bytes.NewReader(bytes.Repeat([]byte{0x33}, 32)),
	)
	if err != nil || replayed.Installation.InstallationID != registered.Installation.InstallationID ||
		replayed.Operation.OperationID != registered.Operation.OperationID {
		t.Fatalf("registration replay=%+v err=%v", replayed, err)
	}
	var replayedCurrent string
	if err := ledgerDatabase.QueryRowContext(ctx, `SELECT current_token_digest FROM connector_control_credentials WHERE installation_id=$1`, invitation.InstallationID).Scan(&replayedCurrent); err != nil || replayedCurrent != originalCurrent {
		t.Fatalf("v1 replay changed current lineage: digest=%q err=%v", replayedCurrent, err)
	}
	drift := redeem
	drift.ProjectID = "project.other"
	if _, err := store.RedeemInvitation(ctx, drift, time.Now().UTC(), nil); !errors.Is(err, connectorcontrol.ErrConsumed) {
		t.Fatalf("consumed invitation accepted drift: %v", err)
	}

	concurrentInvitation, _, err := issuerStore.CreateInvitation(ctx, connectorcontrol.CreateInvitationInput{
		ActorSubject: "xs_0123456789abcdef",
		Config: connectorcontrol.InvitationConfig{
			DeploymentID:           "xid_AAAAAAAAAAAAAAAAAAAAAA",
			XInsightOrigin:         "https://xinsight.example",
			UniRouterInstallOrigin: "https://unirouter.example",
		},
		Now: now, Random: bytes.NewReader(bytes.Repeat([]byte{0x44}, 64)),
	})
	if err != nil {
		t.Fatal(err)
	}
	concurrentControlToken := "xib_" + strings.Repeat("B", 43)
	concurrentAcknowledgement := connectorcontrol.InstallationAcknowledgement{
		SchemaVersion:  connectorcontrol.AcknowledgementSchema,
		InstallationID: concurrentInvitation.InstallationID,
		DeploymentID:   concurrentInvitation.DeploymentID,
		ProjectID:      "project.concurrent",
		FinalEventScope: []string{
			connectorcontrol.CompletedEventType,
		},
		ConsentSHA256: "sha256:" + strings.Repeat("b", 64),
		ConfirmedAt:   now.Add(10 * time.Second),
	}
	_, concurrentAcknowledgementPayload, err := connectorcontrol.CanonicalSHA256(concurrentAcknowledgement)
	if err != nil {
		t.Fatal(err)
	}
	type concurrentRedemptionResult struct {
		installationID string
		operationID    string
		err            error
	}
	start := make(chan struct{})
	results := make(chan concurrentRedemptionResult, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			installationID, operationID, err := callConnectorInvitationRedemption(
				ctx,
				workspaceDatabase,
				concurrentInvitation.Token,
				concurrentAcknowledgementPayload,
				concurrentControlToken,
				now.Add(24*time.Hour),
			)
			results <- concurrentRedemptionResult{
				installationID: installationID,
				operationID:    operationID,
				err:            err,
			}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var firstConcurrent concurrentRedemptionResult
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent workspace redemption: %v", result.err)
		}
		if firstConcurrent.operationID == "" {
			firstConcurrent = result
			continue
		}
		if result.installationID != firstConcurrent.installationID ||
			result.operationID != firstConcurrent.operationID {
			t.Fatalf("concurrent redemption diverged: first=%+v second=%+v", firstConcurrent, result)
		}
	}
	assertConnectorPrincipalCounts(t, ctx, dsn, 2, 2)
	prematureResult := connectorcontrol.ControlOperationResult{
		SchemaVersion: connectorcontrol.ControlResultSchemaV2,
		OperationID:   registered.Operation.OperationID, InstallationID: registered.Operation.InstallationID,
		Phase:  connectorcontrol.ControlPhasePreparePausedSource,
		Status: "completed",
		Result: &connectorcontrol.ControlPhaseResultV2{
			SourceInstance: "Source.Fixture:V1", Cursor: "cursor-fixture-1", HighWatermark: "cursor-fixture-1",
			SourceState: "paused", Readiness: connectorcontrol.ReadinessPausedSourceCursorReady,
		},
		ObservedAt: now.Add(2*time.Minute + 30*time.Second),
	}
	if _, err := store.CompleteControlOperation(
		ctx, registered.Installation.ControlServiceIdentityID, prematureResult, now.Add(2*time.Minute+30*time.Second),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("unpolled operation accepted result: %v", err)
	}

	running, err := store.PollControlOperation(
		ctx,
		registered.Installation.ControlServiceIdentityID,
		registered.Installation.InstallationID,
		now.Add(3*time.Minute),
	)
	if err != nil || running.Status != "running" || running.AttemptCount != 1 || running.LeaseExpiresAt == nil {
		t.Fatalf("running operation=%+v err=%v", running, err)
	}
	if running.SchemaVersion != connectorcontrol.ControlOperationSchemaV2 ||
		running.Phase != connectorcontrol.ControlPhasePreparePausedSource {
		t.Fatalf("running operation did not preserve v2 phase authority: %+v", running)
	}
	sameLease, err := store.PollControlOperation(
		ctx,
		registered.Installation.ControlServiceIdentityID,
		registered.Installation.InstallationID,
		now.Add(3*time.Minute+time.Second),
	)
	if err != nil || sameLease.OperationID != running.OperationID || sameLease.AttemptCount != 1 ||
		sameLease.LeaseExpiresAt == nil || !sameLease.LeaseExpiresAt.Equal(*running.LeaseExpiresAt) {
		t.Fatalf("same lease=%+v err=%v", sameLease, err)
	}
	result := connectorcontrol.ControlOperationResult{
		SchemaVersion: connectorcontrol.ControlResultSchemaV2,
		OperationID:   running.OperationID, InstallationID: running.InstallationID,
		Phase:  connectorcontrol.ControlPhasePreparePausedSource,
		Status: "completed",
		Result: &connectorcontrol.ControlPhaseResultV2{
			SourceInstance: "Source.Fixture:V1", Cursor: "cursor-fixture-1", HighWatermark: "cursor-fixture-1",
			SourceState: "paused", Readiness: connectorcontrol.ReadinessPausedSourceCursorReady,
		},
		ObservedAt: now.Add(4 * time.Minute),
	}
	completed, err := store.CompleteControlOperation(
		ctx, registered.Installation.ControlServiceIdentityID, result, now.Add(4*time.Minute),
	)
	if err != nil || completed.ObservedState != "source_ready" ||
		completed.SourceInstanceSHA256 != connectorcontrol.RawSHA256(result.Result.SourceInstance) {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	if replay, err := store.CompleteControlOperation(
		ctx, registered.Installation.ControlServiceIdentityID, result, now.Add(5*time.Minute),
	); err != nil || replay.Version != completed.Version {
		t.Fatalf("result replay=%+v err=%v", replay, err)
	}
	tampered := result
	tampered.Result = &connectorcontrol.ControlPhaseResultV2{
		SourceInstance: "source.other", Cursor: "cursor-fixture-1", HighWatermark: "cursor-fixture-1",
		SourceState: "paused", Readiness: connectorcontrol.ReadinessPausedSourceCursorReady,
	}
	if _, err := store.CompleteControlOperation(
		ctx, registered.Installation.ControlServiceIdentityID, tampered, now.Add(5*time.Minute),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("terminal result accepted drift: %v", err)
	}
	backdated := result
	backdated.ObservedAt = running.CreatedAt.Add(-time.Nanosecond)
	if _, err := store.CompleteControlOperation(
		ctx, registered.Installation.ControlServiceIdentityID, backdated, now.Add(5*time.Minute),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("terminal result accepted backdated observation: %v", err)
	}

	prepareResultSHA256, _, err := connectorcontrol.CanonicalSHA256(result)
	if err != nil {
		t.Fatal(err)
	}
	receiverRegistration := connectorcontrol.PublicReceiverRegistrationV1{
		SchemaVersion: connectorcontrol.ReceiverRegistrationSchema,
		TokenFormat:   connectorcontrol.ReceiverTokenFormat, VerifierScheme: connectorcontrol.ReceiverVerifierScheme,
		KeyPrefix: "ur-xf1-0123456789abc", VerifierSHA256: strings.Repeat("6", 64),
		OperationID: strings.Repeat("7", 32),
	}
	receiverRegistrationSHA256, _, err := connectorcontrol.CanonicalSHA256(receiverRegistration)
	if err != nil {
		t.Fatal(err)
	}
	networkBinding := connectorcontrol.FixedReceiverNetworkBinding{
		Transport: connectorcontrol.ReceiverNetworkTransport, Route: connectorcontrol.ReceiverFeedRoute,
		BindingSHA256: "sha256:" + strings.Repeat("8", 64),
	}
	registerInput := connectorcontrol.EnqueueControlPhaseInput{
		InstallationID:          registered.Installation.InstallationID,
		Phase:                   connectorcontrol.ControlPhaseRegisterReceiver,
		PredecessorOperationID:  running.OperationID,
		PredecessorResultSHA256: prepareResultSHA256,
		Request: connectorcontrol.ControlPhaseRequestV2{
			SourceInstance: result.Result.SourceInstance, ConsumerEpoch: 1,
			CheckpointAuthoritySHA256: "sha256:" + strings.Repeat("5", 64),
			ReceiverRegistration:      &receiverRegistration,
			NetworkBinding:            &networkBinding,
			RequiredSourceState:       "paused",
		},
	}
	if _, err := issuerStore.EnqueueControlPhase(
		ctx, registerInput, now.Add(6*time.Minute), bytes.NewReader(bytes.Repeat([]byte{0x4f}, 32)),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("register phase bypassed activation claim from source_ready: %v", err)
	}
	if _, err := store.ClaimInstallationActivation(
		ctx,
		registered.Installation.InstallationID,
		completed.Version,
		running.OperationID,
		prepareResultSHA256,
		now.Add(5*time.Minute),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("workspace role claimed root activation authority: %v", err)
	}
	if _, err := issuerStore.ClaimInstallationActivation(
		ctx,
		registered.Installation.InstallationID,
		completed.Version+1,
		running.OperationID,
		prepareResultSHA256,
		now.Add(5*time.Minute),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("activation claim accepted version drift: %v", err)
	}
	if _, err := issuerStore.ClaimInstallationActivation(
		ctx,
		registered.Installation.InstallationID,
		completed.Version,
		running.OperationID,
		"sha256:"+strings.Repeat("f", 64),
		now.Add(5*time.Minute),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("activation claim accepted prepare result drift: %v", err)
	}
	claimed, err := issuerStore.ClaimInstallationActivation(
		ctx,
		registered.Installation.InstallationID,
		completed.Version,
		running.OperationID,
		prepareResultSHA256,
		now.Add(5*time.Minute),
	)
	if err != nil || claimed.Installation.ObservedState != "controller_pending" ||
		claimed.Installation.Status != "pending" || claimed.Installation.DesiredState != "active" ||
		claimed.Installation.Version != completed.Version+1 {
		t.Fatalf("claimed activation boundary=%+v err=%v", claimed, err)
	}
	claimReplay, err := issuerStore.ClaimInstallationActivation(
		ctx,
		registered.Installation.InstallationID,
		completed.Version,
		running.OperationID,
		prepareResultSHA256,
		now.Add(5*time.Minute),
	)
	if err != nil || claimReplay.Installation.Version != claimed.Installation.Version ||
		claimReplay.Installation.ObservedState != "controller_pending" {
		t.Fatalf("activation claim replay=%+v err=%v", claimReplay, err)
	}
	directOperation := connectorcontrol.ControlOperation{
		SchemaVersion: connectorcontrol.ControlOperationSchemaV2,
		OperationID:   "xco_ZZZZZZZZZZZZZZZZZZZZZZ", InstallationID: registerInput.InstallationID,
		Kind: "connect", Phase: registerInput.Phase, ProjectID: redeem.ProjectID,
		EventScope: redeem.FinalEventScope, DesiredState: "active",
		PredecessorOperationID:  registerInput.PredecessorOperationID,
		PredecessorResultSHA256: registerInput.PredecessorResultSHA256,
		Request:                 &registerInput.Request,
		Status:                  "pending", AvailableAt: now.Add(6 * time.Minute),
		CreatedAt: now.Add(6 * time.Minute), UpdatedAt: now.Add(6 * time.Minute),
	}
	_, directRequestSHA256, directRequestPayload, err := connectorcontrol.CanonicalControlOperationRequestV2(directOperation)
	if err != nil {
		t.Fatal(err)
	}
	assertConnectorSQLRejected(t, ctx, workspaceDatabase, `
INSERT INTO connector_control_operations (
  operation_id, installation_id, operation_kind, phase, desired_state,
  predecessor_operation_id, predecessor_result_sha256,
  request_sha256, request_payload, status, available_at, created_at, updated_at
) VALUES ($1, $2, 'connect', $3, 'active', $4, $5, $6, $7, 'pending', $8, $8, $8)`,
		directOperation.OperationID, directOperation.InstallationID, directOperation.Phase,
		directOperation.PredecessorOperationID, directOperation.PredecessorResultSHA256,
		directRequestSHA256, string(directRequestPayload), directOperation.CreatedAt)
	if _, err := issuerStore.EnqueueControlPhase(
		ctx, registerInput, now.Add(3*time.Minute), bytes.NewReader(bytes.Repeat([]byte{0x50}, 32)),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("register phase accepted chronology before predecessor completion: %v", err)
	}

	type enqueueResult struct {
		operation connectorcontrol.ControlOperation
		err       error
	}
	enqueueStart := make(chan struct{})
	enqueueResults := make(chan enqueueResult, 2)
	for _, seed := range []byte{0x51, 0x52} {
		seed := seed
		go func() {
			<-enqueueStart
			operation, enqueueErr := issuerStore.EnqueueControlPhase(
				ctx, registerInput, now.Add(6*time.Minute), bytes.NewReader(bytes.Repeat([]byte{seed}, 32)),
			)
			enqueueResults <- enqueueResult{operation: operation, err: enqueueErr}
		}()
	}
	close(enqueueStart)
	firstRegister := <-enqueueResults
	secondRegister := <-enqueueResults
	if firstRegister.err != nil || secondRegister.err != nil ||
		firstRegister.operation.OperationID != secondRegister.operation.OperationID {
		t.Fatalf("concurrent register enqueue first=%+v second=%+v", firstRegister, secondRegister)
	}
	registerOperation := firstRegister.operation
	registerRunning, err := store.PollControlOperation(
		ctx, registered.Installation.ControlServiceIdentityID, registered.Installation.InstallationID,
		now.Add(7*time.Minute),
	)
	if err != nil || registerRunning.OperationID != registerOperation.OperationID ||
		registerRunning.Phase != connectorcontrol.ControlPhaseRegisterReceiver {
		t.Fatalf("register poll=%+v err=%v", registerRunning, err)
	}
	registerResult := connectorcontrol.ControlOperationResult{
		SchemaVersion: connectorcontrol.ControlResultSchemaV2,
		OperationID:   registerRunning.OperationID, InstallationID: registerRunning.InstallationID,
		Phase: connectorcontrol.ControlPhaseRegisterReceiver, Status: "completed",
		Result: &connectorcontrol.ControlPhaseResultV2{
			SourceInstance:             result.Result.SourceInstance,
			ReceiverRegistrationSHA256: receiverRegistrationSHA256,
			NetworkBindingSHA256:       networkBinding.BindingSHA256,
			SourceState:                "paused", ReceiverState: "feed_staged",
			Readiness: connectorcontrol.ReadinessPausedReceiverRegistered,
		},
		ObservedAt: now.Add(8 * time.Minute),
	}
	registerCompleted, err := store.CompleteControlOperation(
		ctx, registered.Installation.ControlServiceIdentityID, registerResult, now.Add(8*time.Minute),
	)
	if err != nil || registerCompleted.ObservedState != "controller_pending" {
		t.Fatalf("register completed=%+v err=%v", registerCompleted, err)
	}
	registerReplay, err := issuerStore.EnqueueControlPhase(
		ctx, registerInput, now.Add(9*time.Minute), bytes.NewReader(bytes.Repeat([]byte{0x53}, 32)),
	)
	if err != nil || registerReplay.OperationID != registerOperation.OperationID {
		t.Fatalf("completed register enqueue replay=%+v err=%v", registerReplay, err)
	}
	registerDrift := registerInput
	registerDrift.Request.ConsumerEpoch++
	if _, err := issuerStore.EnqueueControlPhase(
		ctx, registerDrift, now.Add(9*time.Minute), bytes.NewReader(bytes.Repeat([]byte{0x56}, 32)),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("completed register enqueue accepted request drift: %v", err)
	}
	registerResultSHA256, _, err := connectorcontrol.CanonicalSHA256(registerResult)
	if err != nil {
		t.Fatal(err)
	}
	resumeInput := connectorcontrol.EnqueueControlPhaseInput{
		InstallationID:          registered.Installation.InstallationID,
		Phase:                   connectorcontrol.ControlPhaseResumeSource,
		PredecessorOperationID:  registerOperation.OperationID,
		PredecessorResultSHA256: registerResultSHA256,
		Request: connectorcontrol.ControlPhaseRequestV2{
			SourceInstance:                 result.Result.SourceInstance,
			ReceiverRegistrationSHA256:     receiverRegistrationSHA256,
			NetworkBindingSHA256:           networkBinding.BindingSHA256,
			PausedAuthReceiptSHA256:        "sha256:" + strings.Repeat("9", 64),
			DormantCredentialReceiptSHA256: "sha256:" + strings.Repeat("a", 64),
			RequiredSourceState:            "active", RequiredReceiverState: "active",
			Readiness: connectorcontrol.ReadinessPausedReceiverAuthenticated,
		},
	}
	resumeOperation, err := issuerStore.EnqueueControlPhase(
		ctx, resumeInput, now.Add(9*time.Minute), bytes.NewReader(bytes.Repeat([]byte{0x54}, 32)),
	)
	if err != nil {
		t.Fatalf("enqueue resume phase: %v", err)
	}
	pendingBoundary, err := issuerStore.InspectActivationBoundary(ctx, registered.Installation.InstallationID)
	if err != nil || pendingBoundary.UniRouterOrigin != "https://unirouter.example" ||
		pendingBoundary.PreparePausedSource == nil || pendingBoundary.PreparePausedSource.Result == nil ||
		pendingBoundary.RegisterReceiver == nil || pendingBoundary.RegisterReceiver.Result == nil ||
		pendingBoundary.ResumeSource == nil || pendingBoundary.ResumeSource.Result != nil ||
		pendingBoundary.ResumeSource.Operation.OperationID != resumeOperation.OperationID {
		t.Fatalf("pending activation boundary=%+v err=%v", pendingBoundary, err)
	}
	if _, err := store.InspectActivationBoundary(ctx, registered.Installation.InstallationID); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("workspace store read the root-only activation boundary: %v", err)
	}
	if _, err := issuerStore.FinalizeInstallationConnected(
		ctx,
		registered.Installation.InstallationID,
		pendingBoundary.Installation.Version,
		resumeOperation.OperationID,
		"sha256:"+strings.Repeat("f", 64),
		now.Add(10*time.Minute),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("pending resume operation marked installation connected: %v", err)
	}
	stillPending, err := issuerStore.InspectActivationBoundary(ctx, registered.Installation.InstallationID)
	if err != nil || stillPending.Installation.Status != "pending" || stillPending.Installation.ObservedState != "controller_pending" {
		t.Fatalf("rejected premature finalize changed installation=%+v err=%v", stillPending.Installation, err)
	}
	resumeRunning, err := store.PollControlOperation(
		ctx, registered.Installation.ControlServiceIdentityID, registered.Installation.InstallationID,
		now.Add(10*time.Minute),
	)
	if err != nil || resumeRunning.OperationID != resumeOperation.OperationID {
		t.Fatalf("resume poll=%+v err=%v", resumeRunning, err)
	}
	resumeResult := connectorcontrol.ControlOperationResult{
		SchemaVersion: connectorcontrol.ControlResultSchemaV2,
		OperationID:   resumeRunning.OperationID, InstallationID: resumeRunning.InstallationID,
		Phase: connectorcontrol.ControlPhaseResumeSource, Status: "completed",
		Result: &connectorcontrol.ControlPhaseResultV2{
			SourceInstance: result.Result.SourceInstance, SourceState: "active", ReceiverState: "active",
			Readiness: connectorcontrol.ReadinessSourceResumedReceiverActive,
		},
		ObservedAt: now.Add(11 * time.Minute),
	}
	resumeCompleted, err := store.CompleteControlOperation(
		ctx, registered.Installation.ControlServiceIdentityID, resumeResult, now.Add(11*time.Minute),
	)
	if err != nil || resumeCompleted.ObservedState != "controller_pending" || resumeCompleted.Status != "pending" {
		t.Fatalf("resume completed before CP2B worker readback=%+v err=%v", resumeCompleted, err)
	}
	resumeResultSHA256, _, err := connectorcontrol.CanonicalControlResultV2(resumeResult)
	if err != nil {
		t.Fatal(err)
	}
	completedBoundary, err := issuerStore.InspectActivationBoundary(ctx, registered.Installation.InstallationID)
	if err != nil || completedBoundary.ResumeSource == nil || completedBoundary.ResumeSource.Result == nil ||
		completedBoundary.ResumeSource.Operation.ResultSHA256 != resumeResultSHA256 ||
		completedBoundary.ResumeSource.Result.Result.SourceState != "active" {
		t.Fatalf("completed activation boundary=%+v err=%v", completedBoundary, err)
	}
	if _, err := store.FinalizeInstallationConnected(
		ctx,
		registered.Installation.InstallationID,
		completedBoundary.Installation.Version,
		resumeOperation.OperationID,
		resumeResultSHA256,
		now.Add(12*time.Minute),
	); err == nil {
		t.Fatal("workspace role finalized a connected installation")
	}
	if _, err := issuerStore.FinalizeInstallationConnected(
		ctx,
		registered.Installation.InstallationID,
		completedBoundary.Installation.Version+1,
		resumeOperation.OperationID,
		resumeResultSHA256,
		now.Add(12*time.Minute),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("finalize accepted installation version drift: %v", err)
	}
	if _, err := issuerStore.FinalizeInstallationConnected(
		ctx,
		registered.Installation.InstallationID,
		completedBoundary.Installation.Version,
		registerOperation.OperationID,
		registerResultSHA256,
		now.Add(12*time.Minute),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("finalize accepted a non-resume phase binding: %v", err)
	}
	connected, err := issuerStore.FinalizeInstallationConnected(
		ctx,
		registered.Installation.InstallationID,
		completedBoundary.Installation.Version,
		resumeOperation.OperationID,
		resumeResultSHA256,
		now.Add(12*time.Minute),
	)
	if err != nil || connected.Status != "connected" || connected.ObservedState != "connected" ||
		connected.Version != completedBoundary.Installation.Version+1 {
		t.Fatalf("connected installation=%+v err=%v", connected, err)
	}
	connectedReplay, err := issuerStore.FinalizeInstallationConnected(
		ctx,
		registered.Installation.InstallationID,
		completedBoundary.Installation.Version,
		resumeOperation.OperationID,
		resumeResultSHA256,
		now.Add(13*time.Minute),
	)
	if err != nil || connectedReplay.Version != connected.Version || connectedReplay.Status != "connected" {
		t.Fatalf("connected finalize replay=%+v err=%v", connectedReplay, err)
	}
	if _, err := issuerStore.FinalizeInstallationConnected(
		ctx,
		registered.Installation.InstallationID,
		connected.Version,
		resumeOperation.OperationID,
		resumeResultSHA256,
		now.Add(13*time.Minute),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("connected finalize accepted a different CAS authority: %v", err)
	}
	resumeReplay, err := issuerStore.EnqueueControlPhase(
		ctx, resumeInput, now.Add(12*time.Minute), bytes.NewReader(bytes.Repeat([]byte{0x55}, 32)),
	)
	if err != nil || resumeReplay.OperationID != resumeOperation.OperationID {
		t.Fatalf("completed resume enqueue replay=%+v err=%v", resumeReplay, err)
	}

	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	crossInstallationOperation := directOperation
	crossInstallationOperation.OperationID = "xco_YYYYYYYYYYYYYYYYYYYYYY"
	crossInstallationOperation.InstallationID = concurrentInvitation.InstallationID
	crossInstallationOperation.ProjectID = concurrentAcknowledgement.ProjectID
	_, crossRequestSHA256, crossRequestPayload, err := connectorcontrol.CanonicalControlOperationRequestV2(crossInstallationOperation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO connector_control_operations (
  operation_id, installation_id, operation_kind, phase, desired_state,
  predecessor_operation_id, predecessor_result_sha256,
  request_sha256, request_payload, status, available_at, created_at, updated_at
) VALUES ($1, $2, 'connect', $3, 'active', $4, $5, $6, $7, 'pending', $8, $8, $8)`,
		crossInstallationOperation.OperationID, crossInstallationOperation.InstallationID,
		crossInstallationOperation.Phase, crossInstallationOperation.PredecessorOperationID,
		crossInstallationOperation.PredecessorResultSHA256, crossRequestSHA256,
		string(crossRequestPayload), crossInstallationOperation.CreatedAt,
	); err == nil {
		t.Fatal("database accepted a cross-installation predecessor")
	}
	if _, err := database.ExecContext(ctx, `
UPDATE connector_control_operations
SET result_payload = jsonb_set(result_payload, '{result,source_instance}', '"source.tampered"'::jsonb)
WHERE operation_id = $1`, running.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteControlOperation(
		ctx, registered.Installation.ControlServiceIdentityID, result, now.Add(13*time.Minute),
	); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatalf("terminal replay accepted tampered durable payload: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO service_identities (
  id, display_name, status, permissions, created_by_subject,
  created_at, updated_at, expires_at
) VALUES (
  'xsi_CCCCCCCCCCCCCCCCCCCCCC', 'Existing Evidence Reader', 'active',
  ARRAY['evidence.read']::text[], 'root', $1::timestamptz, $1::timestamptz,
  $1::timestamptz + interval '1 day'
)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO service_identity_tokens (
  token_digest, service_identity_id, token_prefix, created_at, expires_at
) VALUES (
  'sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
  'xsi_CCCCCCCCCCCCCCCCCCCCCC', 'xib_CCCCCCCC', $1::timestamptz,
  $1::timestamptz + interval '1 day'
)`, now); err != nil {
		t.Fatal(err)
	}
	assertConnectorSQLRejected(t, ctx, workspaceDatabase, `
UPDATE service_identities
SET permissions = ARRAY['connector.control.poll', 'connector.control.ack']::text[],
    created_by_subject = 'system'
WHERE id = 'xsi_CCCCCCCCCCCCCCCCCCCCCC'`)
	assertConnectorSQLRejected(t, ctx, workspaceDatabase, `
UPDATE service_identities
SET expires_at = expires_at + interval '365 days'
WHERE id = 'xsi_CCCCCCCCCCCCCCCCCCCCCC'`)
	assertConnectorSQLRejected(t, ctx, workspaceDatabase, `
UPDATE service_identity_tokens
SET service_identity_id = $1, expires_at = expires_at + interval '365 days'
WHERE token_digest = 'sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd'`,
		registered.Installation.ControlServiceIdentityID)
	actor, err := workspace.HumanActor("xs_0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevokeServiceIdentity(
		ctx, registered.Installation.ControlServiceIdentityID, actor,
		"connector privilege boundary test", now.Add(7*time.Minute),
	); err != nil {
		t.Fatalf("revoke connector identity through product store: %v", err)
	}
	assertConnectorSQLRejected(t, ctx, workspaceDatabase, `
UPDATE service_identities
SET status = 'active', revoked_at = NULL, revoke_reason = NULL
WHERE id = $1`, registered.Installation.ControlServiceIdentityID)
	assertConnectorSQLRejected(t, ctx, workspaceDatabase, `
UPDATE service_identity_tokens
SET revoked_at = NULL
WHERE service_identity_id = $1`, registered.Installation.ControlServiceIdentityID)
	var controlIdentityCount int
	if err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM service_identities
WHERE permissions = ARRAY['connector.control.poll', 'connector.control.ack']::text[]`,
	).Scan(&controlIdentityCount); err != nil || controlIdentityCount != 2 {
		t.Fatalf("control identity count=%d err=%v", controlIdentityCount, err)
	}
	for table, expected := range map[string]int{
		"connector_installations":               2,
		"connector_installation_invitations":    2,
		"connector_control_operations":          4,
		"service_identities":                    3,
		"service_identity_tokens":               3,
		"connector_checkpoint_epoch_operations": 0,
		"connector_checkpoint_epoch_history":    0,
		"adapter_import_states":                 0,
	} {
		var count int
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != expected {
			t.Fatalf("table %s count=%d expected=%d err=%v", table, count, expected, err)
		}
	}
	var orphanIdentities, orphanTokens int
	if err := database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*)
   FROM service_identities identity
   LEFT JOIN connector_installations installation
     ON installation.control_service_identity_id = identity.id
   WHERE identity.permissions = ARRAY['connector.control.poll', 'connector.control.ack']::text[]
     AND installation.id IS NULL),
  (SELECT count(*)
   FROM service_identity_tokens token
	 JOIN service_identities identity ON identity.id = token.service_identity_id
   LEFT JOIN connector_installations installation
     ON installation.control_service_identity_id = token.service_identity_id
	 WHERE identity.permissions = ARRAY['connector.control.poll', 'connector.control.ack']::text[]
	   AND installation.id IS NULL)`).Scan(&orphanIdentities, &orphanTokens); err != nil {
		t.Fatal(err)
	}
	if orphanIdentities != 0 || orphanTokens != 0 {
		t.Fatalf("orphan connector principals identities=%d tokens=%d", orphanIdentities, orphanTokens)
	}
	var (
		workspaceExecute                  bool
		workspaceIdentityInsert           bool
		workspaceTokenInsert              bool
		workspaceInstallationInsert       bool
		workspaceInvitationInsert         bool
		workspaceOperationInsert          bool
		workspaceInstallationTableUpdate  bool
		workspaceInvitationTableUpdate    bool
		workspaceProjectUpdate            bool
		workspaceControlIdentityUpdate    bool
		workspaceInvitationStatusUpdate   bool
		workspaceOperationalSourceUpdate  bool
		workspaceIdentityPermissionUpdate bool
		workspaceIdentityExpiryUpdate     bool
		workspaceTokenBindingUpdate       bool
		workspaceTokenExpiryUpdate        bool
	)
	if err := database.QueryRowContext(ctx, `
SELECT
  has_function_privilege('xinsight_workspace', 'public.xinsight_redeem_connector_invitation(text,text,text,timestamp with time zone)', 'EXECUTE'),
  has_table_privilege('xinsight_workspace', 'public.service_identities', 'INSERT'),
  has_table_privilege('xinsight_workspace', 'public.service_identity_tokens', 'INSERT'),
	  has_table_privilege('xinsight_workspace', 'public.connector_installations', 'INSERT'),
	  has_table_privilege('xinsight_workspace', 'public.connector_installation_invitations', 'INSERT'),
	  has_table_privilege('xinsight_workspace', 'public.connector_control_operations', 'INSERT'),
  has_table_privilege('xinsight_workspace', 'public.connector_installations', 'UPDATE'),
  has_table_privilege('xinsight_workspace', 'public.connector_installation_invitations', 'UPDATE'),
  has_column_privilege('xinsight_workspace', 'public.connector_installations', 'project_id', 'UPDATE'),
  has_column_privilege('xinsight_workspace', 'public.connector_installations', 'control_service_identity_id', 'UPDATE'),
  has_column_privilege('xinsight_workspace', 'public.connector_installation_invitations', 'status', 'UPDATE'),
	  has_column_privilege('xinsight_workspace', 'public.connector_installations', 'source_instance', 'UPDATE'),
	  has_column_privilege('xinsight_workspace', 'public.service_identities', 'permissions', 'UPDATE'),
	  has_column_privilege('xinsight_workspace', 'public.service_identities', 'expires_at', 'UPDATE'),
	  has_column_privilege('xinsight_workspace', 'public.service_identity_tokens', 'service_identity_id', 'UPDATE'),
	  has_column_privilege('xinsight_workspace', 'public.service_identity_tokens', 'expires_at', 'UPDATE')`).Scan(
		&workspaceExecute,
		&workspaceIdentityInsert,
		&workspaceTokenInsert,
		&workspaceInstallationInsert,
		&workspaceInvitationInsert,
		&workspaceOperationInsert,
		&workspaceInstallationTableUpdate,
		&workspaceInvitationTableUpdate,
		&workspaceProjectUpdate,
		&workspaceControlIdentityUpdate,
		&workspaceInvitationStatusUpdate,
		&workspaceOperationalSourceUpdate,
		&workspaceIdentityPermissionUpdate,
		&workspaceIdentityExpiryUpdate,
		&workspaceTokenBindingUpdate,
		&workspaceTokenExpiryUpdate,
	); err != nil {
		t.Fatal(err)
	}
	if !workspaceExecute || workspaceIdentityInsert || workspaceTokenInsert ||
		workspaceInstallationInsert || workspaceInvitationInsert ||
		workspaceOperationInsert ||
		workspaceInstallationTableUpdate || workspaceInvitationTableUpdate ||
		workspaceProjectUpdate || workspaceControlIdentityUpdate ||
		workspaceInvitationStatusUpdate || !workspaceOperationalSourceUpdate ||
		workspaceIdentityPermissionUpdate || workspaceIdentityExpiryUpdate ||
		workspaceTokenBindingUpdate || workspaceTokenExpiryUpdate {
		t.Fatalf("workspace connector privilege boundary is invalid")
	}
	downSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000022_add_connector_control_phases.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, string(downSQL)); err == nil ||
		!strings.Contains(err.Error(), "cannot remove connector control phases while phase operations exist") {
		t.Fatalf("schema22 down accepted live phase operations: %v", err)
	}
	var phaseColumnStillPresent bool
	if err := database.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM information_schema.columns
  WHERE table_schema = 'public'
    AND table_name = 'connector_control_operations'
    AND column_name = 'phase'
)`).Scan(&phaseColumnStillPresent); err != nil || !phaseColumnStillPresent {
		t.Fatalf("schema22 down failure changed phase column present=%v err=%v", phaseColumnStillPresent, err)
	}
}

func createConnectorTestRoles(t *testing.T, ctx context.Context, baseDSN string) {
	t.Helper()
	database, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var adminRole string
	var adminSuperuser bool
	if err := database.QueryRowContext(
		ctx,
		`SELECT current_user, rolsuper FROM pg_roles WHERE rolname = current_user`,
	).Scan(&adminRole, &adminSuperuser); err != nil {
		t.Fatal(err)
	}
	if adminRole != "xinsight_admin" || !adminSuperuser {
		t.Fatalf("connector PostgreSQL fixture requires xinsight_admin superuser, got role=%q superuser=%v", adminRole, adminSuperuser)
	}
	for _, role := range []string{
		"xinsight_reader", "xinsight_workspace", "xinsight_importer",
	} {
		var exists bool
		if err := database.QueryRowContext(
			ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`,
			role,
		).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			parsed, parseErr := url.Parse(baseDSN)
			if parseErr != nil || parsed.Hostname() != "127.0.0.1" || os.Getenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST") != "1" {
				t.Fatalf("connector PostgreSQL fixture role %s already exists outside an explicitly selected local synthetic instance", role)
			}
			// The reused local cluster also hosts production-role fixtures. Preserve
			// their LOGIN state and credentials; this test connects as admin then
			// SET ROLE, so LOGIN is immaterial. Never retire a pre-existing role.
			var safe bool
			if err := database.QueryRowContext(ctx, `SELECT NOT (rolsuper OR rolcreatedb OR rolcreaterole OR rolinherit OR rolreplication OR rolbypassrls)
 AND NOT EXISTS (SELECT 1 FROM pg_auth_members m WHERE m.member=r.oid OR m.roleid=r.oid)
 FROM pg_roles r WHERE rolname=$1`, role).Scan(&safe); err != nil || !safe {
				t.Fatalf("existing synthetic role %s violates the fixture privilege boundary: safe=%v err=%v", role, safe, err)
			}
			continue
		}
		identifier := pgx.Identifier{role}.Sanitize()
		if _, err := database.ExecContext(
			ctx,
			"CREATE ROLE "+identifier+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS",
		); err != nil {
			t.Fatalf("create connector PostgreSQL fixture role %s: %v", role, err)
		}
		roleForCleanup := role
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanupCancel()
			cleanup, cleanupErr := sql.Open("pgx", baseDSN)
			if cleanupErr != nil {
				t.Errorf("open connector PostgreSQL role cleanup connection for %s: %v", roleForCleanup, cleanupErr)
				return
			}
			defer cleanup.Close()
			if _, cleanupErr := cleanup.ExecContext(
				cleanupCtx,
				"DROP ROLE "+pgx.Identifier{roleForCleanup}.Sanitize(),
			); cleanupErr != nil {
				t.Errorf("drop connector PostgreSQL fixture role %s: %v", roleForCleanup, cleanupErr)
			}
		})
	}
}

func connectorRoleDSN(t *testing.T, dsn string, role string, readOnly bool) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	options := "-c role=" + role + " -c default_transaction_read_only=off"
	if readOnly {
		options = "-c role=" + role + " -c default_transaction_read_only=on"
	}
	query.Set("options", options)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func callConnectorInvitationRedemption(
	ctx context.Context,
	database *sql.DB,
	invitationToken string,
	acknowledgementPayload []byte,
	controlToken string,
	controlTokenExpires time.Time,
) (string, string, error) {
	var installationID, operationID string
	err := database.QueryRowContext(ctx, `
SELECT installation_id, operation_id
FROM public.xinsight_redeem_connector_invitation($1, $2, $3, $4)`,
		invitationToken,
		string(acknowledgementPayload),
		controlToken,
		controlTokenExpires.UTC(),
	).Scan(&installationID, &operationID)
	return installationID, operationID, err
}

func assertConnectorPrincipalCounts(
	t *testing.T,
	ctx context.Context,
	adminDSN string,
	expectedIdentities int,
	expectedTokens int,
) {
	t.Helper()
	database, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var identities, tokens int
	if err := database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM service_identities),
  (SELECT count(*) FROM service_identity_tokens)`).Scan(&identities, &tokens); err != nil {
		t.Fatal(err)
	}
	if identities != expectedIdentities || tokens != expectedTokens {
		t.Fatalf(
			"connector principal counts identities=%d/%d tokens=%d/%d",
			identities,
			expectedIdentities,
			tokens,
			expectedTokens,
		)
	}
}

func assertConnectorSQLRejected(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	statement string,
	arguments ...any,
) {
	t.Helper()
	if _, err := database.ExecContext(ctx, statement, arguments...); err == nil {
		t.Fatalf("workspace SQL unexpectedly succeeded: %s", strings.TrimSpace(statement))
	}
}

func assertConnectorTableCount(
	t *testing.T,
	ctx context.Context,
	adminDSN string,
	table string,
	expected int,
) {
	t.Helper()
	if table != "connector_installations" && table != "connector_installation_invitations" {
		t.Fatalf("unsupported connector table count %q", table)
	}
	database, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var count int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != expected {
		t.Fatalf("table %s count=%d expected=%d err=%v", table, count, expected, err)
	}
}
