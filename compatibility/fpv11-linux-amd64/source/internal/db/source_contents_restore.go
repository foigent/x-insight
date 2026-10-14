package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"reflect"
	"strconv"
)

// ValidateSourceContentsRestore validates persisted history, without current
// user grants, remote requests, or rewriting the capture/availability record.
func ValidateSourceContentsRestore(ctx context.Context, q workspaceQueryer) error {
	var collections, manifests, receipts bool
	if e := q.QueryRowContext(ctx, `SELECT to_regclass('public.source_content_collections') IS NOT NULL,to_regclass('public.source_content_manifests') IS NOT NULL,to_regclass('public.source_content_receipts') IS NOT NULL`).Scan(&collections, &manifests, &receipts); e != nil {
		return e
	}
	if !collections && !manifests && !receipts {
		return nil
	}
	if !collections || !manifests || !receipts {
		return fmt.Errorf("restored content tables are incomplete")
	}
	rows, e := q.QueryContext(ctx, `SELECT id::text FROM source_content_collections ORDER BY id`)
	if e != nil {
		return e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		c, err := loadContentCollection(ctx, q, id, false)
		if err != nil {
			return err
		}
		if !validRestoredContentProgress(c) {
			return fmt.Errorf("restored content progress is invalid")
		}
	}
	var valid bool
	e = q.QueryRowContext(ctx, `SELECT (SELECT count(*)=4 FROM pg_trigger WHERE tgenabled='A' AND (tgrelid,tgname) IN (('public.source_content_manifests'::regclass,'source_content_manifests_immutable'),('public.source_content_collections'::regclass,'source_content_collection_management'),('public.source_content_receipts'::regclass,'source_content_receipts_immutable'),('public.source_content_receipts'::regclass,'source_content_receipt_identity'))) AND NOT EXISTS(SELECT 1 FROM source_content_manifests d LEFT JOIN source_content_collections c ON c.id=d.collection_id WHERE c.id IS NULL OR ROW(c.source_system_id,c.source_instance,c.org_id,c.project_id) IS DISTINCT FROM ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id) OR d.source_sha256<>'sha256:'||encode(sha256(d.source_bytes),'hex') OR d.source_json IS DISTINCT FROM convert_from(d.source_bytes,'UTF8')::jsonb)`).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return fmt.Errorf("restored content scope, source hash or progress is invalid")
	}
	rows, e = q.QueryContext(ctx, `SELECT source_system_id::text,source_instance,org_id,project_id,request_id,source_fact_id,revision::text,event_id,COALESCE(previous_revision::text,''),previous_hash,source_sha256,source_bytes,availability,occurred_at,recorded_at FROM source_content_manifests ORDER BY source_system_id,source_fact_id,revision`)
	if e != nil {
		return e
	}
	defer rows.Close()
	var previous sourcefacts.ContentManifestEntry
	previousSource := ""
	for rows.Next() {
		var source, instance, org, project, request, fact, revision, event, prev, prevHash, hash string
		var raw, availability []byte
		var occurred, recorded sql.NullTime
		if e = rows.Scan(&source, &instance, &org, &project, &request, &fact, &revision, &event, &prev, &prevHash, &hash, &raw, &availability, &occurred, &recorded); e != nil {
			return e
		}
		d, e := sourcefacts.DecodeContentManifest(raw)
		if e != nil {
			return e
		}
		var a sourcefacts.ContentAvailability
		if json.Unmarshal(availability, &a) != nil || !validRestoredContentAvailability(d, a) {
			return sourcefacts.ErrInvalid
		}
		if d.SourceInstance != instance || d.OrgID != org || d.ProjectID != project || d.RequestID != request || d.SourceFactID != fact || d.Revision != revision || d.EventID != event || d.PreviousRevision != prev || d.PreviousHash != prevHash || sourcefacts.Hash(raw) != hash || !occurred.Valid || !recorded.Valid || occurred.Time.UTC().Format("2006-01-02T15:04:05.000000Z") != d.OccurredAt || recorded.Time.UTC().Format("2006-01-02T15:04:05.000000Z") != d.RecordedAt {
			return sourcefacts.ErrInvalid
		}
		entry := sourcefacts.ContentManifestEntry{Document: d, SourceSHA256: hash, Availability: a}
		if previousSource == source && previous.Document.SourceFactID == fact && previous.Document.Revision == prev {
			if e = sourcefacts.ValidateContentManifestSuccessor(previous, entry); e != nil {
				return e
			}
		}
		previous, previousSource = entry, source
	}
	if e = rows.Err(); e != nil {
		return e
	}
	rows.Close()
	return validateContentReceiptsRestore(ctx, q)
}

// Root restore orchestration must call this in its existing transaction, after
// validation and before the restored database becomes available to readers.
func SuspendRestoredContents(ctx context.Context, tx *sql.Tx) error {
	var present bool
	if e := tx.QueryRowContext(ctx, `SELECT to_regclass('public.source_content_collections') IS NOT NULL`).Scan(&present); e != nil {
		return e
	}
	if !present {
		return nil
	}
	_, e := tx.ExecContext(ctx, `WITH changed AS (UPDATE source_content_collections SET status='paused',requires_attention=true,last_error='restore_requires_authorization',revision=revision+1 WHERE status<>'revoked' RETURNING id,revision,status) INSERT INTO source_fact_management_events(registration_kind,registration_id,revision,action,status,actor) SELECT 'content',id::text,revision,'state_change',status,current_user FROM changed`)
	return e
}

func validRestoredContentProgress(c sourcefacts.ContentCollection) bool {
	last, e := strconv.ParseInt(c.LastSequence, 10, 64)
	high, eh := strconv.ParseInt(c.HighWatermarkSequence, 10, 64)
	coverage, ec := strconv.ParseInt(c.CoverageFrom.AfterSequence, 10, 64)
	if e != nil || eh != nil || ec != nil || last < coverage || high < last {
		return false
	}
	if c.Cursor == "" {
		return last == coverage && high == coverage && c.HighWatermark == "" && c.Coverage == "unknown" && len(c.Gaps) == 0 && reflect.DeepEqual(c.RetentionFloor, sourcefacts.ContentBoundary{})
	}
	floor, ef := strconv.ParseInt(c.RetentionFloor.AfterSequence, 10, 64)
	if ef != nil || floor < 0 || floor > last || !sourcefacts.ValidTime(c.RetentionFloor.Time) || c.HighWatermark == "" || (c.Coverage != "bounded" && c.Coverage != "partial" && c.Coverage != "unknown") {
		return false
	}
	retentionGap := false
	for _, g := range c.Gaps {
		switch g.Reason {
		case "retention_floor_ahead":
			retentionGap = true
		case "manifest_write_unavailable", "missing_predecessor", "source_recovery", "capture_health_unknown":
		default:
			return false
		}
		if g.RangeKnown {
			a, e := strconv.ParseInt(g.AfterSequence, 10, 64)
			b, eb := strconv.ParseInt(g.ThroughSequence, 10, 64)
			if e != nil || eb != nil || a < 0 || b <= a {
				return false
			}
		} else if g.AfterSequence != "" || g.ThroughSequence != "" {
			return false
		}
	}
	return floor <= coverage || retentionGap
}
func validRestoredContentAvailability(d sourcefacts.ContentManifest, a sourcefacts.ContentAvailability) bool {
	if !sourcefacts.ValidTime(a.EvaluatedAt) {
		return false
	}
	saved := d.Representation != nil
	switch a.State {
	case "available":
		return saved && !a.Retryable && a.EvaluatedAt < d.ExpiresAt
	case "expired":
		return saved && !a.Retryable && a.EvaluatedAt >= d.ExpiresAt
	case "pending":
		return d.Capture.Completeness == "pending" && a.Retryable
	case "not_captured":
		return d.Capture.Completeness == "not_captured" && !a.Retryable
	case "deleted", "missing", "corrupt":
		return saved && !a.Retryable
	case "source_unavailable":
		return a.Retryable
	}
	return false
}

func validateContentReceiptsRestore(ctx context.Context, q workspaceQueryer) error {
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM source_content_receipts a
 LEFT JOIN source_content_collections c ON c.id=a.collection_id LEFT JOIN source_content_manifests d ON d.source_system_id=a.source_system_id AND d.source_fact_id=a.source_fact_id AND d.revision=a.revision
 WHERE c.id IS NULL OR d.source_fact_id IS NULL OR a.source_sha256<>d.source_sha256 OR a.received_at<d.received_at
 OR ROW(c.source_system_id,c.source_instance,c.org_id,c.project_id) IS DISTINCT FROM ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id)
 OR (ROW(a.credential_id,a.registration_hash,a.content_scope_revision,a.recovery_epoch,a.coverage_from)=ROW(c.credential_id,c.registration_hash,c.content_scope_revision,c.recovery_epoch,c.coverage_from) AND a.sequence>c.last_sequence::numeric)
 ) AND NOT EXISTS(SELECT 1 FROM source_content_manifests d WHERE NOT EXISTS(SELECT 1 FROM source_content_receipts a WHERE
 ROW(a.source_system_id,a.source_fact_id,a.revision)=ROW(d.source_system_id,d.source_fact_id,d.revision)
 AND a.collection_id=d.collection_id AND a.content_scope_revision=d.content_scope_revision AND a.recovery_epoch=d.recovery_epoch AND a.sequence=d.sequence AND a.received_at=d.received_at))`).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("restored content receipt identity, first reception or current progress is invalid")
	}
	rows, err := q.QueryContext(ctx, `SELECT a.collection_id::text,a.source_system_id::text,c.source_instance,c.org_id,c.project_id,a.credential_id,a.registration_hash,a.content_scope_revision,a.recovery_epoch,a.coverage_from,a.sequence::text,a.availability,d.source_bytes
 FROM source_content_receipts a JOIN source_content_collections c ON c.id=a.collection_id JOIN source_content_manifests d ON d.source_system_id=a.source_system_id AND d.source_fact_id=a.source_fact_id AND d.revision=a.revision ORDER BY a.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c sourcefacts.ContentCollection
		var coverage, availability, raw []byte
		var sequence string
		if err = rows.Scan(&c.ID, &c.SourceSystemID, &c.SourceInstance, &c.OrgID, &c.ProjectID, &c.CredentialID, &c.RegistrationHash, &c.ContentScopeRevision, &c.RecoveryEpoch, &coverage, &sequence, &availability, &raw); err != nil {
			return err
		}
		c.AllowedPurposes = []string{sourcefacts.ContentPurposeHumanReview}
		if json.Unmarshal(coverage, &c.CoverageFrom) != nil || !sourcefacts.ValidContentCollection(c) {
			return sourcefacts.ErrInvalid
		}
		d, e := sourcefacts.DecodeContentManifest(raw)
		if e != nil {
			return e
		}
		var a sourcefacts.ContentAvailability
		if json.Unmarshal(availability, &a) != nil || !validRestoredContentAvailability(d, a) {
			return sourcefacts.ErrInvalid
		}
		seq, e := strconv.ParseInt(sequence, 10, 64)
		floor, ef := strconv.ParseInt(c.CoverageFrom.AfterSequence, 10, 64)
		if e != nil || ef != nil || seq <= floor || d.OccurredAt < c.CoverageFrom.Time {
			return fmt.Errorf("restored content receipt lies outside its frozen authorization")
		}
	}
	return rows.Err()
}
