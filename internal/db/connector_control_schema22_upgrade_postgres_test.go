package db_test

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
)

func TestConnectorControlSchema22PreservesSchema21OperationV1(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dsn := connectorPullMigrationDatabaseDSN(t, ctx, baseDSN)
	migrationsDir := filepath.Join("..", "..", "migrations")
	schema21Dir := connectorMigrationPrefixDir(t, migrationsDir, "000021")
	if applied, err := db.RunMigrations(ctx, dsn, schema21Dir); err != nil || applied != 21 {
		t.Fatalf("apply schema21 migrations applied=%d err=%v", applied, err)
	}

	store, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Second)
	invitation, _, err := store.CreateInvitation(ctx, connectorcontrol.CreateInvitationInput{
		ActorSubject: "xs_0123456789abcdef",
		Config: connectorcontrol.InvitationConfig{
			DeploymentID:           "xid_AAAAAAAAAAAAAAAAAAAAAA",
			XInsightOrigin:         "https://xinsight.example",
			UniRouterInstallOrigin: "https://unirouter.example",
		},
		Now: now, Random: bytes.NewReader(bytes.Repeat([]byte{0x61}, 64)),
	})
	if err != nil {
		t.Fatalf("create schema21 invitation: %v", err)
	}
	acknowledgement := connectorcontrol.InstallationAcknowledgement{
		SchemaVersion:  connectorcontrol.AcknowledgementSchema,
		InstallationID: invitation.InstallationID,
		DeploymentID:   invitation.DeploymentID,
		ProjectID:      "Project.Legacy:V1",
		FinalEventScope: []string{
			connectorcontrol.CompletedEventType,
		},
		ConsentSHA256: "sha256:" + strings.Repeat("b", 64),
		ConfirmedAt:   now,
	}
	_, acknowledgementPayload, err := connectorcontrol.CanonicalSHA256(acknowledgement)
	if err != nil {
		t.Fatal(err)
	}
	controlToken := "xib_" + strings.Repeat("L", 43)
	controlTokenExpires := now.Add(24 * time.Hour)
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	installationID, operationID, err := callConnectorInvitationRedemption(
		ctx,
		database,
		invitation.Token,
		acknowledgementPayload,
		controlToken,
		controlTokenExpires,
	)
	if err != nil || installationID != invitation.InstallationID {
		t.Fatalf("redeem schema21 invitation installation=%q operation=%q err=%v", installationID, operationID, err)
	}
	var identityID, schemaVersion string
	if err := database.QueryRowContext(ctx, `
SELECT installation.control_service_identity_id,
       operation.request_payload ->> 'schema_version'
FROM connector_installations installation
JOIN connector_control_operations operation
  ON operation.installation_id = installation.id
WHERE installation.id = $1 AND operation.operation_id = $2`,
		installationID,
		operationID,
	).Scan(&identityID, &schemaVersion); err != nil {
		t.Fatal(err)
	}
	if schemaVersion != connectorcontrol.ControlOperationSchema {
		t.Fatalf("schema21 redemption operation schema=%q", schemaVersion)
	}

	if err := os.Symlink(
		mustAbsolutePath(t, filepath.Join(migrationsDir, "000022_add_connector_control_phases.up.sql")),
		filepath.Join(schema21Dir, "000022_add_connector_control_phases.up.sql"),
	); err != nil {
		t.Fatalf("add schema22 migration to prefix: %v", err)
	}
	if applied, err := db.RunMigrations(ctx, dsn, schema21Dir); err != nil || applied != 1 {
		t.Fatalf("apply schema22 migration applied=%d err=%v", applied, err)
	}
	var phase sql.NullString
	if err := database.QueryRowContext(ctx, `
SELECT phase
FROM connector_control_operations
WHERE operation_id = $1`, operationID).Scan(&phase); err != nil {
		t.Fatal(err)
	}
	if phase.Valid {
		t.Fatalf("schema22 rewrote legacy operation phase=%q", phase.String)
	}

	replayedRegistration, err := store.RedeemInvitation(ctx, connectorcontrol.RedeemInvitationInput{
		InvitationToken:     invitation.Token,
		ProjectID:           acknowledgement.ProjectID,
		FinalEventScope:     acknowledgement.FinalEventScope,
		ConsentSHA256:       acknowledgement.ConsentSHA256,
		ConfirmedAt:         acknowledgement.ConfirmedAt,
		ControlToken:        controlToken,
		ControlTokenExpires: controlTokenExpires,
	}, time.Now().UTC(), nil)
	if err != nil || replayedRegistration.Installation.InstallationID != installationID ||
		replayedRegistration.Operation.OperationID != operationID ||
		replayedRegistration.Operation.SchemaVersion != connectorcontrol.ControlOperationSchema {
		t.Fatalf("schema22 product replay registration=%+v err=%v", replayedRegistration, err)
	}
	pollAt := time.Now().UTC().Add(time.Second)
	running, err := store.PollControlOperation(ctx, identityID, installationID, pollAt)
	if err != nil || running.SchemaVersion != connectorcontrol.ControlOperationSchema ||
		running.Phase != "" || running.OperationID != operationID || running.Status != "running" {
		t.Fatalf("poll upgraded legacy operation=%+v err=%v", running, err)
	}
	result := connectorcontrol.ControlOperationResult{
		SchemaVersion:  connectorcontrol.ControlResultSchema,
		OperationID:    operationID,
		InstallationID: installationID,
		Status:         "completed",
		SourceInstance: "Source.Legacy:V1",
		ObservedAt:     pollAt.Add(time.Second),
	}
	completed, err := store.CompleteControlOperation(ctx, identityID, result, result.ObservedAt)
	if err != nil || completed.ObservedState != "source_ready" {
		t.Fatalf("complete upgraded legacy operation=%+v err=%v", completed, err)
	}
	replayed, err := store.CompleteControlOperation(ctx, identityID, result, result.ObservedAt.Add(time.Second))
	if err != nil || replayed.Version != completed.Version {
		t.Fatalf("replay upgraded legacy completion=%+v err=%v", replayed, err)
	}

	var requestSHA256 string
	var requestPayload []byte
	if err := database.QueryRowContext(ctx, `
SELECT request_sha256, request_payload::text
FROM connector_control_operations
WHERE operation_id = $1`, operationID).Scan(&requestSHA256, &requestPayload); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO connector_control_operations (
  operation_id, installation_id, operation_kind, desired_state,
  request_sha256, request_payload, status, available_at, created_at, updated_at
) VALUES (
  'xco_ZZZZZZZZZZZZZZZZZZZZZZ', $1, 'connect', 'active',
  $2, $3, 'pending', $4, $4, $4
)`, installationID, requestSHA256, string(requestPayload), result.ObservedAt.Add(2*time.Second)); err == nil ||
		!strings.Contains(err.Error(), "legacy connector control operations cannot be created") {
		t.Fatalf("schema22 new phase-null operation error=%v", err)
	}
	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000022_add_connector_control_phases.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, string(downSQL)); err != nil {
		t.Fatalf("remove schema22 with only preserved v1 rows: %v", err)
	}
	var phaseColumnPresent bool
	if err := database.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM information_schema.columns
  WHERE table_schema = 'public'
    AND table_name = 'connector_control_operations'
    AND column_name = 'phase'
)`).Scan(&phaseColumnPresent); err != nil || phaseColumnPresent {
		t.Fatalf("schema21 shape after down phase column present=%v err=%v", phaseColumnPresent, err)
	}
	downReplayInstallationID, downReplayOperationID, err := callConnectorInvitationRedemption(
		ctx,
		database,
		invitation.Token,
		acknowledgementPayload,
		controlToken,
		controlTokenExpires,
	)
	if err != nil || downReplayInstallationID != installationID || downReplayOperationID != operationID {
		t.Fatalf(
			"schema21 function after down installation=%q operation=%q err=%v",
			downReplayInstallationID,
			downReplayOperationID,
			err,
		)
	}
}

func connectorMigrationPrefixDir(t *testing.T, migrationsDir string, maximum string) string {
	t.Helper()
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) < 6 || !strings.HasSuffix(name, ".up.sql") || name[:6] > maximum {
			continue
		}
		if err := os.Symlink(
			mustAbsolutePath(t, filepath.Join(migrationsDir, name)),
			filepath.Join(target, name),
		); err != nil {
			t.Fatalf("link migration %s: %v", name, err)
		}
	}
	return target
}

func mustAbsolutePath(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}
