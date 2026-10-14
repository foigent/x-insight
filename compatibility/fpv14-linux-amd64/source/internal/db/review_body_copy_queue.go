package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

type URCCopyCandidate struct {
	CollectionID string
	Ref          sourcefacts.FrozenManifestRef
}

// ListURCCopyCandidates revisits committed C manifests after a crash or a
// failed source read. New managed C copies the latest complete revision;
// original C transitions also preserve all still-readable historical revisions.
func (s *WorkspaceStore) ListURCCopyCandidates(ctx context.Context, collectionID string, now time.Time, limit int) ([]URCCopyCandidate, error) {
	if s == nil || s.db == nil || !workEventUUID.MatchString(collectionID) || now.IsZero() || limit < 1 || limit > 50 {
		return nil, reviewbody.ErrInvalid
	}
	var legacy bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM connector_source_enrollments WHERE content_collection_id=$1::uuid AND legacy_c_copy_pending AND status='requires_migration')`, collectionID).Scan(&legacy); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.source_system_id::text,d.sequence::text,d.source_sha256,d.source_bytes,d.availability
 FROM source_content_manifests d JOIN source_content_collections c ON c.id=d.collection_id
 LEFT JOIN review_body_copy_attempts a ON a.collection_id=d.collection_id AND a.content_ref=d.source_json->>'content_ref'
 WHERE d.collection_id=$1::uuid AND c.status='active' AND NOT c.requires_attention
 AND d.availability->>'state'='available' AND d.source_json->'capture'->>'completeness'='complete'
 AND (NOT $4 OR (d.source_json->>'expires_at')::timestamptz>$2)
 AND ($4 OR NOT EXISTS (SELECT 1 FROM source_content_manifests newer WHERE newer.source_system_id=d.source_system_id
   AND newer.source_fact_id=d.source_fact_id AND newer.revision>d.revision))
 AND NOT EXISTS (SELECT 1 FROM review_body_operations o WHERE o.content_ref=d.source_json->>'content_ref' AND o.state='accepted')
 AND (a.status IS NULL OR (a.status='retry' AND a.next_attempt_at<=$2))
 ORDER BY COALESCE(a.next_attempt_at,d.received_at),d.sequence LIMIT $3`, collectionID, now.UTC(), limit, legacy)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]URCCopyCandidate, 0, limit)
	for rows.Next() {
		var sourceID, sequence, hash string
		var raw, availabilityRaw []byte
		if err = rows.Scan(&sourceID, &sequence, &hash, &raw, &availabilityRaw); err != nil {
			return nil, err
		}
		manifest, err := sourcefacts.DecodeContentManifest(raw)
		if err != nil {
			return nil, reviewbody.ErrIntegrity
		}
		var availability sourcefacts.ContentAvailability
		if json.Unmarshal(availabilityRaw, &availability) != nil {
			return nil, reviewbody.ErrIntegrity
		}
		ref, err := sourcefacts.FrozenContentManifestRef(sourceID, sourcefacts.ContentManifestEntry{
			Sequence: sequence, Document: manifest, SourceSHA256: hash, Availability: availability,
		})
		if err != nil {
			return nil, reviewbody.ErrIntegrity
		}
		result = append(result, URCCopyCandidate{CollectionID: collectionID, Ref: ref})
	}
	return result, rows.Err()
}

// RecordURCCopyAttempt never changes the immutable source identity. A retry
// is due on a later managed poll; other outcomes retain an inspectable gap.
func (s *WorkspaceStore) RecordURCCopyAttempt(ctx context.Context, candidate URCCopyCandidate, status, reason string, now time.Time) error {
	if s == nil || s.db == nil || !workEventUUID.MatchString(candidate.CollectionID) || now.IsZero() ||
		candidate.Ref.ContentRef == "" || len(reason) < 1 || len(reason) > 80 ||
		status != "retry" && status != "excluded_policy" && status != "scope_lost" &&
			status != "source_expired" && status != "integrity_gap" && status != "accepted" {
		return reviewbody.ErrInvalid
	}
	var next any
	if status == "retry" {
		next = now.UTC().Add(time.Minute)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO review_body_copy_attempts(collection_id,content_ref,source_system_id,
 source_fact_id,source_revision,source_sha256,status,reason,attempts,next_attempt_at,last_attempt_at)
 VALUES($1::uuid,$2,$3::uuid,$4,$5::numeric,$6,$7,$8,1,$9,$10)
 ON CONFLICT(collection_id,content_ref) DO UPDATE SET status=$7,reason=$8,
 attempts=review_body_copy_attempts.attempts+1,next_attempt_at=$9,last_attempt_at=$10
 WHERE review_body_copy_attempts.source_system_id=$3::uuid AND review_body_copy_attempts.source_fact_id=$4
 AND review_body_copy_attempts.source_revision=$5::numeric AND review_body_copy_attempts.source_sha256=$6`,
		candidate.CollectionID, candidate.Ref.ContentRef, candidate.Ref.SourceSystemID, candidate.Ref.SourceFactID,
		candidate.Ref.Revision, candidate.Ref.SourceSHA256, status, reason, next, now.UTC())
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return reviewbody.ErrConflict
	}
	return nil
}
