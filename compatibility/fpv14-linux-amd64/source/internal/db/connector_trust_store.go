package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/foigent/x-insight/internal/investigation"
)

func (s *EvidenceStore) ListConnectorTrust(ctx context.Context, limit int) ([]investigation.ConnectorTrust, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("evidence store is not initialized")
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("connector trust limit must be between 1 and 100")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT
  st.source_system_id::text || ':' || st.adapter_manifest_id::text || ':' || st.cursor_key,
  src.id::text, src.source_key, src.source_type, src.source_version,
  src.display_name, src.status,
  manifest.id::text, manifest.adapter_id, manifest.adapter_version, manifest.active,
  manifest.fidelity_level, array_to_json(manifest.ingestion_modes)::text,
  manifest.payload_behavior, manifest.idempotency_strategy, manifest.cursor_strategy,
  st.checkpoint_epoch, COALESCE(st.checkpoint_epoch_operation_id, ''),
  st.cursor_key,
  st.cursor_value IS NOT NULL,
  CASE WHEN st.cursor_value IS NULL THEN NULL
       ELSE 'sha256:' || encode(public.digest(st.cursor_value, 'sha256'), 'hex') END,
  st.high_watermark IS NOT NULL,
  CASE WHEN st.high_watermark IS NULL THEN NULL
       ELSE 'sha256:' || encode(public.digest(st.high_watermark, 'sha256'), 'hex') END,
  st.worker_status, st.status, st.last_import_started_at, st.last_import_completed_at,
  st.last_poll_failed_at, st.consecutive_failures, st.pages_committed,
  st.items_received, st.items_committed, st.duplicate_noop,
  NULLIF(st.last_error, '') IS NOT NULL, st.updated_at
FROM adapter_import_states st
JOIN source_systems src ON src.id = st.source_system_id
JOIN adapter_manifests manifest ON manifest.id = st.adapter_manifest_id
ORDER BY src.display_name, manifest.adapter_id, st.cursor_key
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]investigation.ConnectorTrust, 0)
	for rows.Next() {
		var (
			item                investigation.ConnectorTrust
			ingestionModes      string
			sourceVersion       sql.NullString
			cursorStrategy      sql.NullString
			cursorDigest        sql.NullString
			highWatermarkDigest sql.NullString
			lastStarted         sql.NullTime
			lastCompleted       sql.NullTime
			lastFailed          sql.NullTime
			updatedAt           sql.NullTime
		)
		if err := rows.Scan(
			&item.ConnectorKey,
			&item.SourceSystemID,
			&item.SourceKey,
			&item.SourceType,
			&sourceVersion,
			&item.SourceDisplayName,
			&item.SourceStatus,
			&item.AdapterManifestID,
			&item.AdapterID,
			&item.AdapterVersion,
			&item.ManifestActive,
			&item.FidelityLevel,
			&ingestionModes,
			&item.PayloadBehavior,
			&item.IdempotencyStrategy,
			&cursorStrategy,
			&item.Checkpoint.Epoch,
			&item.Checkpoint.EpochOperationID,
			&item.Checkpoint.CursorKey,
			&item.Checkpoint.CursorPresent,
			&cursorDigest,
			&item.Checkpoint.HighWatermarkPresent,
			&highWatermarkDigest,
			&item.Checkpoint.WorkerStatus,
			&item.Checkpoint.ImportStatus,
			&lastStarted,
			&lastCompleted,
			&lastFailed,
			&item.Checkpoint.ConsecutiveFailures,
			&item.Checkpoint.PagesCommitted,
			&item.Checkpoint.ItemsReceived,
			&item.Checkpoint.ItemsCommitted,
			&item.Checkpoint.DuplicateNoop,
			&item.Checkpoint.ErrorPresent,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		item.SourceVersion = sourceVersion.String
		item.IngestionModes = decodeJSONStringSlice(ingestionModes)
		item.CursorStrategy = cursorStrategy.String
		item.Checkpoint.CursorDigest = cursorDigest.String
		item.Checkpoint.HighWatermarkDigest = highWatermarkDigest.String
		item.Checkpoint.LastImportStartedAt = nullTimePointer(lastStarted)
		item.Checkpoint.LastImportCompletedAt = nullTimePointer(lastCompleted)
		item.Checkpoint.LastPollFailedAt = nullTimePointer(lastFailed)
		item.Checkpoint.UpdatedAt = nullTimePointer(updatedAt)
		item.TrustState = connectorTrustState(item)
		item.DeliveryStateBoundary = "mutable_checkpoint_status_not_event_evidence"
		items = append(items, item)
	}
	return items, rows.Err()
}

func connectorTrustState(item investigation.ConnectorTrust) string {
	switch {
	case item.SourceStatus != "active" || !item.ManifestActive || item.Checkpoint.WorkerStatus == "inactive":
		return "inactive"
	case item.Checkpoint.ErrorPresent ||
		item.Checkpoint.ConsecutiveFailures > 0 ||
		item.Checkpoint.WorkerStatus == "error" ||
		item.Checkpoint.ImportStatus == "error":
		return "degraded"
	case item.Checkpoint.LastImportCompletedAt == nil:
		return "not_observed"
	case item.Checkpoint.WorkerStatus == "polling" || item.Checkpoint.ImportStatus == "running":
		return "syncing"
	default:
		return "reported_current"
	}
}
