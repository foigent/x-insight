package db

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func validateSourceFactsRestore(ctx context.Context, q workspaceQueryer) error {
	bad := func() error {
		return fmt.Errorf("restored source documents, immutable guards or reading bindings are invalid")
	}
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT
 (SELECT count(*)=6 FROM pg_trigger WHERE tgenabled='A' AND NOT tgisinternal AND tgname IN ('source_fact_documents_immutable','workbench_source_members_immutable','workbench_source_manifests_immutable','workbench_source_members_append','workbench_source_manifests_finalize','source_fact_stream_identity'))
 AND NOT EXISTS(SELECT 1 FROM workbench_readings r LEFT JOIN workbench_source_manifests sm ON sm.reading_id=r.id
 WHERE (r.interpretation_revision=$1) IS DISTINCT FROM (sm.reading_id IS NOT NULL))
 AND NOT EXISTS(SELECT 1 FROM workbench_source_members m LEFT JOIN workbench_source_manifests sm ON sm.reading_id=m.reading_id WHERE sm.reading_id IS NULL)
 AND NOT EXISTS(SELECT 1 FROM source_fact_documents child JOIN source_fact_documents parent
 ON parent.source_system_id=child.source_system_id AND parent.source_fact_id=child.source_fact_id AND parent.revision=child.revision-1 WHERE child.previous_hash<>parent.source_hash)
 AND NOT EXISTS(SELECT 1 FROM workbench_source_members sm JOIN source_fact_documents d USING(source_system_id,event_id)
 WHERE sm.economic_state='received' AND NOT EXISTS(SELECT 1 FROM workbench_economic_facts f WHERE f.id=sm.economic_fact_id AND `+sourceEconomicJoinSQL+`
 AND (d.profile<>'unirouter.cost-basis.v1' OR (d.source_json#>>'{cost,economic_reference,source}'='completed_execution' AND d.source_json#>>'{cost,economic_reference,revision}'='1' AND d.source_json#>>'{cost,economic_reference,snapshot_sha256}'=f.source_event_hash))))`, digestText(workbenchInterpretationA1)).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return bad()
	}
	rows, err := q.QueryContext(ctx, `SELECT source_instance,org_id,project_id,request_id,profile,stage,source_fact_id,revision,event_id,previous_hash,source_hash,envelope_hash,source_bytes,envelope_bytes,occurred_at,recorded_at FROM source_fact_documents`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var d sourcefacts.Document
		var revision int64
		var sh, eh string
		var source, envelope []byte
		var occurred, recorded time.Time
		if err = rows.Scan(&d.SourceInstance, &d.OrgID, &d.ProjectID, &d.RequestID, &d.Profile, &d.Stage, &d.SourceFactID, &revision, &d.EventID, &d.PreviousHash, &sh, &eh, &source, &envelope, &occurred, &recorded); err != nil {
			rows.Close()
			return err
		}
		item, e := sourcefacts.ValidateItem(envelope, d.Profile)
		if e != nil || !bytes.Equal(item.SourceBytes, source) || item.SourceHash != sh || item.EnvelopeHash != eh {
			rows.Close()
			return bad()
		}
		v := item.Document
		if v.SourceInstance != d.SourceInstance || v.OrgID != d.OrgID || v.ProjectID != d.ProjectID || v.RequestID != d.RequestID || v.Profile != d.Profile || v.Stage != d.Stage || v.SourceFactID != d.SourceFactID || v.EventID != d.EventID || v.PreviousHash != d.PreviousHash || v.Revision != strconv.FormatInt(revision, 10) || occurred.UTC().Format(sourcefacts.TimeFormat) != v.OccurredAt || recorded.UTC().Format(sourcefacts.TimeFormat) != v.RecordedAt {
			rows.Close()
			return bad()
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Collect headers before nested queries so a one-connection restore also
	// works. Ancestors and late unassociated economics stay immutable history.
	type header struct {
		id, rule, auth, hash string
		coverage             []byte
	}
	headers := []header{}
	rows, err = q.QueryContext(ctx, `SELECT reading_id,rule_version,authorization_sha256,manifest_sha256,coverage FROM workbench_source_manifests`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var h header
		if err = rows.Scan(&h.id, &h.rule, &h.auth, &h.hash, &h.coverage); err != nil {
			rows.Close()
			return err
		}
		headers = append(headers, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, h := range headers {
		var actual string
		if h.rule != workbenchInterpretationA1 {
			return bad()
		}
		if err = q.QueryRowContext(ctx, sourceManifestDigestSQL, h.id, h.rule, h.auth, string(h.coverage)).Scan(&actual); err != nil {
			return err
		}
		if actual != h.hash {
			return bad()
		}
		var coverage workbench.SourceCoverage
		if json.Unmarshal(h.coverage, &coverage) != nil {
			return bad()
		}
		var count, pending, unreceived, conflicts, media, uncaptured int64
		if err = q.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE sm.chain_state='missing_predecessor'),count(*) FILTER(WHERE sm.economic_state='not_received'),count(*) FILTER(WHERE sm.economic_state='hash_mismatch'),count(*) FILTER(WHERE sm.economic_state IN ('media_pricing_pending','media_not_eligible')),count(*) FILTER(WHERE d.source_json->>'capture_status'='not_captured') FROM workbench_source_members sm JOIN source_fact_documents d USING(source_system_id,event_id) WHERE sm.reading_id=$1`, h.id).Scan(&count, &pending, &unreceived, &conflicts, &media, &uncaptured); err != nil {
			return err
		}
		if count != coverage.KnownDocuments || pending != coverage.PendingPredecessor || unreceived != coverage.EconomicNotReceived || conflicts != coverage.EconomicHashConflicts || media != coverage.MediaPending || uncaptured != coverage.NotCaptured {
			return bad()
		}
	}
	return validateWorkchainRestore(ctx, q)
}
