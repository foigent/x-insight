package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProductManagedConnectorRegistryFailsClosedAndLegacySeedIsNotUsed(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	defer database.Close()

	missingSourceID := internalTestUUID(t)
	missingAdapterID := internalTestUUID(t)
	missingTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	missingErr := ensureDependencies(ctx, missingTx, map[string]any{
		"source_system_id":    missingSourceID,
		"adapter_manifest_id": missingAdapterID,
		"source_type":         "litellm",
		"source_version":      "foigent.litellm.spend-log.v1",
		"capture_mode":        "api_import",
		"event_type":          "model.request.succeeded",
	})
	_ = missingTx.Rollback()
	if missingErr == nil ||
		!strings.Contains(missingErr.Error(), "exact active product-managed Connector registry") {
		t.Fatalf("missing registry error=%v", missingErr)
	}
	var missingSourceCount, missingAdapterCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT count(*) FROM source_systems WHERE id = $1::uuid`,
		missingSourceID,
	).Scan(&missingSourceCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(
		ctx,
		`SELECT count(*) FROM adapter_manifests WHERE id = $1::uuid`,
		missingAdapterID,
	).Scan(&missingAdapterCount); err != nil {
		t.Fatal(err)
	}
	if missingSourceCount != 0 || missingAdapterCount != 0 {
		t.Fatalf(
			"missing registry was auto-seeded source=%d adapter=%d",
			missingSourceCount,
			missingAdapterCount,
		)
	}

	sourceID := internalTestUUID(t)
	adapterID := internalTestUUID(t)
	insertProductRegistry(
		t,
		ctx,
		database,
		sourceID,
		adapterID,
		true,
		"foigent.litellm.spend-logs",
		"foigent.litellm.spend-logs",
		"1.0.0",
		"1.0.0",
		"1.1.0",
		"1.1.0",
	)
	exactTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureDependencies(ctx, exactTx, map[string]any{
		"source_system_id":    sourceID,
		"adapter_manifest_id": adapterID,
		"source_type":         "litellm",
		"source_version":      "foigent.litellm.spend-log.v1",
		"capture_mode":        "api_import",
		"event_type":          "model.request.succeeded",
	}); err != nil {
		_ = exactTx.Rollback()
		t.Fatalf("exact registry rejected: %v", err)
	}
	if err := exactTx.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := database.ExecContext(
		ctx,
		`UPDATE source_systems SET status = 'inactive' WHERE id = $1::uuid`,
		sourceID,
	); err != nil {
		t.Fatal(err)
	}
	inactiveTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	inactiveErr := ensureDependencies(ctx, inactiveTx, map[string]any{
		"source_system_id":    sourceID,
		"adapter_manifest_id": adapterID,
		"source_type":         "litellm",
		"source_version":      "foigent.litellm.spend-log.v1",
		"capture_mode":        "api_import",
		"event_type":          "model.request.succeeded",
	})
	_ = inactiveTx.Rollback()
	if inactiveErr == nil ||
		!strings.Contains(inactiveErr.Error(), "conflict with product-managed Connector registry") {
		t.Fatalf("inactive registry error=%v", inactiveErr)
	}

	unmarkedSourceID := internalTestUUID(t)
	unmarkedAdapterID := internalTestUUID(t)
	insertProductRegistry(
		t,
		ctx,
		database,
		unmarkedSourceID,
		unmarkedAdapterID,
		false,
		"foigent.litellm.spend-logs",
		"foigent.litellm.spend-logs",
		"1.0.0",
		"1.0.0",
		"1.1.0",
		"1.1.0",
	)
	unmarkedTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	unmarkedErr := ensureDependencies(ctx, unmarkedTx, map[string]any{
		"source_system_id":    unmarkedSourceID,
		"adapter_manifest_id": unmarkedAdapterID,
		"source_type":         "litellm",
		"source_version":      "foigent.litellm.spend-log.v1",
		"capture_mode":        "api_import",
		"event_type":          "model.request.succeeded",
	})
	_ = unmarkedTx.Rollback()
	if unmarkedErr == nil ||
		!strings.Contains(unmarkedErr.Error(), "exact active product-managed Connector registry") {
		t.Fatalf("unmarked registry error=%v", unmarkedErr)
	}

	for _, test := range []struct {
		name                    string
		sourceConnectorID       string
		adapterConnectorID      string
		sourceConnectorVersion  string
		adapterConnectorVersion string
		sourceProtocolVersion   string
		adapterProtocolVersion  string
		manifestDrift           string
	}{
		{
			name:                    "connector_id",
			sourceConnectorID:       "foigent.litellm.spend-logs",
			adapterConnectorID:      "foigent.other.spend-logs",
			sourceConnectorVersion:  "1.0.0",
			adapterConnectorVersion: "1.0.0",
			sourceProtocolVersion:   "1.1.0",
			adapterProtocolVersion:  "1.1.0",
		},
		{
			name:                    "connector_version",
			sourceConnectorID:       "foigent.litellm.spend-logs",
			adapterConnectorID:      "foigent.litellm.spend-logs",
			sourceConnectorVersion:  "1.0.0",
			adapterConnectorVersion: "2.0.0",
			sourceProtocolVersion:   "1.1.0",
			adapterProtocolVersion:  "1.1.0",
		},
		{
			name:                    "protocol_version",
			sourceConnectorID:       "foigent.litellm.spend-logs",
			adapterConnectorID:      "foigent.litellm.spend-logs",
			sourceConnectorVersion:  "1.0.0",
			adapterConnectorVersion: "1.0.0",
			sourceProtocolVersion:   "1.1.0",
			adapterProtocolVersion:  "1.0.0",
		},
		{
			name:                    "supported_source_versions",
			sourceConnectorID:       "foigent.litellm.spend-logs",
			adapterConnectorID:      "foigent.litellm.spend-logs",
			sourceConnectorVersion:  "1.0.0",
			adapterConnectorVersion: "1.0.0",
			sourceProtocolVersion:   "1.1.0",
			adapterProtocolVersion:  "1.1.0",
			manifestDrift:           "supported_source_versions",
		},
		{
			name:                    "ingestion_modes",
			sourceConnectorID:       "foigent.litellm.spend-logs",
			adapterConnectorID:      "foigent.litellm.spend-logs",
			sourceConnectorVersion:  "1.0.0",
			adapterConnectorVersion: "1.0.0",
			sourceProtocolVersion:   "1.1.0",
			adapterProtocolVersion:  "1.1.0",
			manifestDrift:           "ingestion_modes",
		},
		{
			name:                    "declared_event_types",
			sourceConnectorID:       "foigent.litellm.spend-logs",
			adapterConnectorID:      "foigent.litellm.spend-logs",
			sourceConnectorVersion:  "1.0.0",
			adapterConnectorVersion: "1.0.0",
			sourceProtocolVersion:   "1.1.0",
			adapterProtocolVersion:  "1.1.0",
			manifestDrift:           "declared_event_types",
		},
	} {
		t.Run("mismatched_"+test.name+"_writes_nothing", func(t *testing.T) {
			mismatchSourceID := internalTestUUID(t)
			mismatchAdapterID := internalTestUUID(t)
			insertProductRegistry(
				t,
				ctx,
				database,
				mismatchSourceID,
				mismatchAdapterID,
				true,
				test.sourceConnectorID,
				test.adapterConnectorID,
				test.sourceConnectorVersion,
				test.adapterConnectorVersion,
				test.sourceProtocolVersion,
				test.adapterProtocolVersion,
			)
			if test.manifestDrift != "" {
				if _, err := database.ExecContext(ctx, `
UPDATE adapter_manifests
SET supported_source_versions = CASE
      WHEN $2 = 'supported_source_versions' THEN ARRAY['wrong.source.v1']::text[]
      ELSE supported_source_versions
    END,
    ingestion_modes = CASE
      WHEN $2 = 'ingestion_modes' THEN ARRAY['db_import']::text[]
      ELSE ingestion_modes
    END,
    declared_event_types = CASE
      WHEN $2 = 'declared_event_types' THEN ARRAY['model.request.failed']::text[]
      ELSE declared_event_types
    END
WHERE id = $1::uuid`,
					mismatchAdapterID,
					test.manifestDrift,
				); err != nil {
					t.Fatal(err)
				}
			}
			mismatchTx, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			mismatchErr := ensureDependencies(ctx, mismatchTx, map[string]any{
				"source_system_id":    mismatchSourceID,
				"adapter_manifest_id": mismatchAdapterID,
				"source_type":         "litellm",
				"source_version":      "foigent.litellm.spend-log.v1",
				"capture_mode":        "api_import",
				"event_type":          "model.request.succeeded",
			})
			_ = mismatchTx.Rollback()
			if mismatchErr == nil ||
				!strings.Contains(
					mismatchErr.Error(),
					"conflict with product-managed Connector registry",
				) {
				t.Fatalf("mismatched registry error=%v", mismatchErr)
			}
			var eventCount int
			if err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM evidence_events
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid`,
				mismatchSourceID,
				mismatchAdapterID,
			).Scan(&eventCount); err != nil {
				t.Fatal(err)
			}
			if eventCount != 0 {
				t.Fatalf("mismatched registry wrote %d evidence events", eventCount)
			}
		})
	}
}

func insertProductRegistry(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	sourceID string,
	adapterID string,
	productManaged bool,
	sourceConnectorID string,
	adapterConnectorID string,
	sourceConnectorVersion string,
	adapterConnectorVersion string,
	sourceProtocolVersion string,
	adapterProtocolVersion string,
) {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO source_systems (
  id, source_key, source_type, display_name, source_version, status, metadata
) VALUES (
  $1::uuid, $2, 'litellm', 'Product registry test',
  'foigent.litellm.spend-log.v1', 'active',
  jsonb_build_object(
    'connector_id', $3::text,
    'connector_version', $4::text,
    'protocol_version', $5::text
  )
)`,
		sourceID,
		"product-registry-test-"+sourceID,
		sourceConnectorID,
		sourceConnectorVersion,
		sourceProtocolVersion,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO adapter_manifests (
  id, adapter_id, adapter_version, source_type, fidelity_level,
  supported_source_versions, ingestion_modes, declared_event_types,
  payload_behavior, idempotency_strategy, manifest_json, active
) VALUES (
  $1::uuid, $2, '1.0.0', 'litellm', 'F1',
  ARRAY['foigent.litellm.spend-log.v1']::text[],
  ARRAY['api_import']::text[],
  ARRAY['model.request.succeeded','model.request.failed','gap.capture.detected']::text[],
  'metadata_only_default', 'source_ref_event_type_source_event_id',
  jsonb_build_object(
    'product_managed_registry', $3::boolean,
    'connector_id', $4::text,
    'connector_version', $5::text,
    'protocol_version', $6::text
  ), true
)`,
		adapterID,
		"product-registry-test-"+adapterID,
		productManaged,
		adapterConnectorID,
		adapterConnectorVersion,
		adapterProtocolVersion,
	); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = database.ExecContext(
			cleanupContext,
			`DELETE FROM adapter_manifests WHERE id = $1::uuid`,
			adapterID,
		)
		_, _ = database.ExecContext(
			cleanupContext,
			`DELETE FROM source_systems WHERE id = $1::uuid`,
			sourceID,
		)
	})
}
