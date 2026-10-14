package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/investigation"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestDecodeUsageGroupPreservesExactV2Money(t *testing.T) {
	var usage investigation.UsageGroup
	if err := decodeUsageGroup(`{"cost_micros":"7","currency":"CNY","amount_scale":1000000,"money_schema":"micro-cny.v1"}`, &usage); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if usage.CostMicros != "7" || usage.Currency != "CNY" || usage.AmountScale == nil || *usage.AmountScale != 1_000_000 || usage.MoneySchema != "micro-cny.v1" {
		t.Fatalf("usage=%+v", usage)
	}
	if usage.CostCents != nil {
		t.Fatalf("v2 usage unexpectedly exposed cost_cents: %+v", usage)
	}
}

func TestReservedUniRouterManifestBindingsFailClosedBeforeRegistryCreation(t *testing.T) {
	testCases := []struct {
		name              string
		sourceType        string
		sourceVersion     string
		adapterManifestID string
		wantErr           bool
	}{
		{name: "matching v1", sourceType: "unirouter", sourceVersion: uniRouterSnapshotSourceVersionV1, adapterManifestID: uniRouterAdapterManifestIDV1},
		{name: "matching v2", sourceType: "unirouter", sourceVersion: uniRouterSnapshotSourceVersionV2, adapterManifestID: uniRouterAdapterManifestIDV2},
		{name: "matching completed execution v1", sourceType: "unirouter", sourceVersion: uniRouterCompletedSourceVersionV1, adapterManifestID: uniRouterCompletedAdapterIDV1},
		{name: "legacy versionless v1", sourceType: "unirouter", adapterManifestID: uniRouterAdapterManifestIDV1},
		{name: "custom manifest", sourceType: "unirouter", sourceVersion: uniRouterSnapshotSourceVersionV2, adapterManifestID: "33333333-3333-4333-8333-333333333333"},
		{name: "v2 with reserved v1", sourceType: "unirouter", sourceVersion: uniRouterSnapshotSourceVersionV2, adapterManifestID: uniRouterAdapterManifestIDV1, wantErr: true},
		{name: "v1 with reserved v2", sourceType: "unirouter", sourceVersion: uniRouterSnapshotSourceVersionV1, adapterManifestID: uniRouterAdapterManifestIDV2, wantErr: true},
		{name: "completed source with custom manifest", sourceType: "unirouter", sourceVersion: uniRouterCompletedSourceVersionV1, adapterManifestID: "33333333-3333-4333-8333-333333333333", wantErr: true},
		{name: "completed manifest without source version", sourceType: "unirouter", adapterManifestID: uniRouterCompletedAdapterIDV1, wantErr: true},
		{name: "reserved ID with other source", sourceType: "other", sourceVersion: uniRouterSnapshotSourceVersionV1, adapterManifestID: uniRouterAdapterManifestIDV1, wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateReservedAdapterManifestBinding(testCase.sourceType, testCase.sourceVersion, testCase.adapterManifestID)
			if testCase.wantErr {
				if err == nil || !strings.Contains(err.Error(), "reserved UniRouter mapper version") {
					t.Fatalf("error=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConcurrentUniRouterSourceVersionUpsertsConvergeToV2(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer conn.Close()

	unknownSourceSystemID := internalTestUUID(t)
	unknownTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin unknown-version transaction: %v", err)
	}
	unknownErr := ensureDependencies(ctx, unknownTx, map[string]any{
		"source_system_id":    unknownSourceSystemID,
		"source_type":         "unirouter",
		"source_version":      "unirouter.prd5.xinsight.snapshot.v3",
		"adapter_manifest_id": internalTestUUID(t),
	})
	if rollbackErr := unknownTx.Rollback(); rollbackErr != nil && rollbackErr != sql.ErrTxDone {
		t.Fatalf("rollback unknown-version transaction: %v", rollbackErr)
	}
	if unknownErr == nil || !strings.Contains(unknownErr.Error(), "unsupported for UniRouter") {
		t.Fatalf("fresh unknown source version error=%v", unknownErr)
	}
	var unknownSourceCount int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM source_systems WHERE id = $1`, unknownSourceSystemID).Scan(&unknownSourceCount); err != nil {
		t.Fatalf("count unknown-version source rows: %v", err)
	}
	if unknownSourceCount != 0 {
		t.Fatalf("fresh unknown source version created %d source rows", unknownSourceCount)
	}

	versionlessSourceSystemID := internalTestUUID(t)
	versionlessTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin versionless transaction: %v", err)
	}
	if err := ensureDependencies(ctx, versionlessTx, map[string]any{
		"source_system_id":    versionlessSourceSystemID,
		"source_type":         "unirouter",
		"adapter_manifest_id": internalTestUUID(t),
	}); err != nil {
		versionlessTx.Rollback()
		t.Fatalf("create versionless registry row: %v", err)
	}
	if err := versionlessTx.Commit(); err != nil {
		t.Fatalf("commit versionless registry row: %v", err)
	}
	unknownAfterVersionlessTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin versionless-to-unknown transaction: %v", err)
	}
	unknownAfterVersionlessErr := ensureDependencies(ctx, unknownAfterVersionlessTx, map[string]any{
		"source_system_id":    versionlessSourceSystemID,
		"source_type":         "unirouter",
		"source_version":      "unirouter.prd5.xinsight.snapshot.v3",
		"adapter_manifest_id": internalTestUUID(t),
	})
	if rollbackErr := unknownAfterVersionlessTx.Rollback(); rollbackErr != nil && rollbackErr != sql.ErrTxDone {
		t.Fatalf("rollback versionless-to-unknown transaction: %v", rollbackErr)
	}
	if unknownAfterVersionlessErr == nil || !strings.Contains(unknownAfterVersionlessErr.Error(), "unsupported for UniRouter") {
		t.Fatalf("versionless-to-unknown source version error=%v", unknownAfterVersionlessErr)
	}
	var versionlessSourceVersion sql.NullString
	if err := conn.QueryRowContext(ctx, `SELECT source_version FROM source_systems WHERE id = $1`, versionlessSourceSystemID).Scan(&versionlessSourceVersion); err != nil {
		t.Fatalf("read versionless source version: %v", err)
	}
	if versionlessSourceVersion.Valid {
		t.Fatalf("versionless source advanced to %q after rejected v3", versionlessSourceVersion.String)
	}

	sourceSystemID := internalTestUUID(t)
	events := []map[string]any{
		{
			"source_system_id":    sourceSystemID,
			"source_type":         "unirouter",
			"source_version":      uniRouterSnapshotSourceVersionV1,
			"adapter_manifest_id": uniRouterAdapterManifestIDV1,
		},
		{
			"source_system_id":    sourceSystemID,
			"source_type":         "unirouter",
			"source_version":      uniRouterSnapshotSourceVersionV2,
			"adapter_manifest_id": uniRouterAdapterManifestIDV2,
		},
	}

	start := make(chan struct{})
	errors := make(chan error, len(events))
	for _, event := range events {
		event := event
		go func() {
			tx, beginErr := conn.BeginTx(ctx, nil)
			if beginErr != nil {
				errors <- beginErr
				return
			}
			defer tx.Rollback()
			<-start
			if dependencyErr := ensureDependencies(ctx, tx, event); dependencyErr != nil {
				errors <- dependencyErr
				return
			}
			errors <- tx.Commit()
		}()
	}
	close(start)
	for range events {
		if err := <-errors; err != nil {
			t.Fatalf("concurrent dependency upsert: %v", err)
		}
	}

	var sourceVersion string
	if err := conn.QueryRowContext(ctx, `SELECT source_version FROM source_systems WHERE id = $1`, sourceSystemID).Scan(&sourceVersion); err != nil {
		t.Fatalf("read converged source version: %v", err)
	}
	if sourceVersion != uniRouterSnapshotSourceVersionV2 {
		t.Fatalf("converged source version=%q want=%q", sourceVersion, uniRouterSnapshotSourceVersionV2)
	}
	for manifestID, wantVersion := range map[string]string{
		uniRouterAdapterManifestIDV1: uniRouterSnapshotSourceVersionV1,
		uniRouterAdapterManifestIDV2: uniRouterSnapshotSourceVersionV2,
	} {
		var adapterVersion string
		if err := conn.QueryRowContext(ctx, `SELECT adapter_version FROM adapter_manifests WHERE id = $1`, manifestID).Scan(&adapterVersion); err != nil {
			t.Fatalf("read adapter manifest %s: %v", manifestID, err)
		}
		if adapterVersion != wantVersion {
			t.Fatalf("adapter manifest %s version=%q want=%q", manifestID, adapterVersion, wantVersion)
		}
	}
}

func TestLivePullProvenanceMigrationAndDependencyBinding(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer conn.Close()

	var seededSourceVersion, seededSourceStatus string
	if err := conn.QueryRowContext(ctx, `
SELECT source_version, status
FROM source_systems
WHERE id = '11111111-1111-1111-1111-111111111111'`).Scan(&seededSourceVersion, &seededSourceStatus); err != nil {
		t.Fatalf("read seeded live source: %v", err)
	}
	if seededSourceVersion != uniRouterEvidenceSourceVersionV2 || seededSourceStatus != "active" {
		t.Fatalf("seeded live source version=%q status=%q", seededSourceVersion, seededSourceStatus)
	}

	sourceSystemID := internalTestUUID(t)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin dependency transaction: %v", err)
	}
	if err := ensureDependencies(ctx, tx, map[string]any{
		"source_system_id":    sourceSystemID,
		"source_type":         "unirouter",
		"source_version":      uniRouterCompletedSourceVersionV1,
		"adapter_manifest_id": uniRouterCompletedAdapterIDV1,
	}); err != nil {
		tx.Rollback()
		t.Fatalf("ensure live dependencies: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit live dependencies: %v", err)
	}

	var ingestionMode string
	var directFieldGroups string
	var derivedFieldGroups string
	var declaredEventTypes string
	if err := conn.QueryRowContext(ctx, `
SELECT
  ingestion_modes[1],
  array_to_string(direct_field_groups, ','),
  array_to_string(derived_field_groups, ','),
  array_to_string(declared_event_types, ',')
FROM adapter_manifests
WHERE id = $1`, uniRouterCompletedAdapterIDV1).Scan(
		&ingestionMode,
		&directFieldGroups,
		&derivedFieldGroups,
		&declaredEventTypes,
	); err != nil {
		t.Fatalf("read live adapter ingestion mode: %v", err)
	}
	if ingestionMode != "api_import" {
		t.Fatalf("live adapter ingestion mode=%q want=api_import", ingestionMode)
	}
	if directFieldGroups != "correlation,identity,capability,usage,outcome" {
		t.Fatalf("live adapter direct field groups=%q", directFieldGroups)
	}
	if derivedFieldGroups != "payload_policy" {
		t.Fatalf("live adapter derived field groups=%q", derivedFieldGroups)
	}
	if declaredEventTypes != "billing.execution.cancelled,billing.execution.settled,gap.capture.detected" {
		t.Fatalf("live adapter declared event types=%q", declaredEventTypes)
	}

	historicalTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin historical dependency transaction: %v", err)
	}
	if err := ensureDependencies(ctx, historicalTx, map[string]any{
		"source_system_id":    sourceSystemID,
		"source_type":         "unirouter",
		"source_version":      uniRouterSnapshotSourceVersionV2,
		"adapter_manifest_id": uniRouterAdapterManifestIDV2,
	}); err != nil {
		historicalTx.Rollback()
		t.Fatalf("ensure historical dependencies alongside live source: %v", err)
	}
	if err := historicalTx.Commit(); err != nil {
		t.Fatalf("commit historical dependencies: %v", err)
	}
	var currentSourceVersion string
	if err := conn.QueryRowContext(ctx, `
SELECT source_version
FROM source_systems
WHERE id = $1`, sourceSystemID).Scan(&currentSourceVersion); err != nil {
		t.Fatalf("read current source version after historical dependency: %v", err)
	}
	if currentSourceVersion != uniRouterCompletedSourceVersionV1 {
		t.Fatalf("historical dependency downgraded current source to %q", currentSourceVersion)
	}

	payloadHash := "sha256:" + strings.Repeat("d", 64)
	provenanceID := internalTestUUID(t)
	if _, err := conn.ExecContext(ctx, `
INSERT INTO import_provenance (
  id, source_system_id, adapter_manifest_id, integration_mode, proof_class,
  assurance_mode, source_payload_sha256, timeline_complete
) VALUES (
  $1, $2, $3, 'completed_execution_pull', 'operator_approved_integration',
  'contracted_pull', $4, true
)`, provenanceID, sourceSystemID, uniRouterCompletedAdapterIDV1, payloadHash); err != nil {
		t.Fatalf("insert valid live provenance: %v", err)
	}
	var storedPayloadHash string
	if err := conn.QueryRowContext(ctx, `
SELECT source_payload_sha256
FROM import_provenance
WHERE id = $1`, provenanceID).Scan(&storedPayloadHash); err != nil {
		t.Fatalf("read live payload hash: %v", err)
	}
	if storedPayloadHash != payloadHash {
		t.Fatalf("stored payload hash=%q want=%q", storedPayloadHash, payloadHash)
	}

	if _, err := conn.ExecContext(ctx, `
INSERT INTO import_provenance (
  id, source_system_id, adapter_manifest_id, integration_mode, proof_class,
  assurance_mode, source_artifact_sha256, source_payload_sha256,
  approval_manifest_sha256, exchange_id, timeline_complete
) VALUES (
  $1, $2, $3, 'completed_execution_pull', 'operator_approved_integration',
  'contracted_pull', $4, $5, $6, 'legacy-exchange', true
)`,
		internalTestUUID(t),
		sourceSystemID,
		uniRouterCompletedAdapterIDV1,
		"sha256:"+strings.Repeat("a", 64),
		payloadHash,
		"sha256:"+strings.Repeat("b", 64),
	); err == nil {
		t.Fatal("database accepted sidecar proof fields on live pull provenance")
	}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO import_provenance (
  id, source_system_id, adapter_manifest_id, integration_mode, proof_class,
  assurance_mode, source_payload_sha256, source_watermark, timeline_complete
) VALUES (
  $1, $2, $3, 'completed_execution_pull', 'operator_approved_integration',
  'contracted_pull', $4, 'page-high-watermark', true
)`,
		internalTestUUID(t),
		sourceSystemID,
		uniRouterCompletedAdapterIDV1,
		payloadHash,
	); err == nil {
		t.Fatal("database accepted a page high-watermark on live item provenance")
	}
}

func internalTestUUID(t *testing.T) string {
	t.Helper()
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatalf("generate UUID: %v", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}
