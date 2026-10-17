package db

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

func validateWorkbenchPurposeRestore(ctx context.Context, q workspaceQueryer) error {
	var exists bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('public.workbench_purpose_revisions') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	bad := func() error {
		return fmt.Errorf("restored purpose revisions, adoption, references or reading bindings are invalid")
	}
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT
 (SELECT count(*)=7 FROM pg_trigger WHERE NOT tgisinternal AND tgenabled='A' AND tgname IN ('workbench_purpose_grant_revision','workbench_purpose_revision_append','workbench_purpose_members_append','workbench_purpose_manifests_finalize','workbench_purpose_revisions_immutable','workbench_purpose_members_immutable','workbench_purpose_manifests_immutable'))
 AND NOT EXISTS(SELECT 1 FROM workbench_purpose_revisions p LEFT JOIN workbench_economic_facts f ON f.id=p.economic_fact_id WHERE f.id IS NULL OR f.execution_id<>p.execution_id)
 AND NOT EXISTS(SELECT 1 FROM workbench_purpose_members m LEFT JOIN workbench_purpose_manifests h USING(reading_id) LEFT JOIN workbench_reading_members r ON r.reading_id=m.reading_id AND r.economic_fact_id=m.economic_fact_id LEFT JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
 WHERE h.reading_id IS NULL OR h.status<>'available' OR r.reading_id IS NULL OR f.execution_id IS DISTINCT FROM m.execution_id)
 AND NOT EXISTS(SELECT 1 FROM workbench_purpose_manifests h WHERE h.rule_version<>$1 OR (h.status='available' AND (SELECT count(*) FROM workbench_purpose_members m WHERE m.reading_id=h.reading_id)<>(SELECT count(*) FROM workbench_reading_members r WHERE r.reading_id=h.reading_id)))
 AND NOT EXISTS(SELECT 1 FROM workbench_purpose_members m JOIN workbench_readings r ON r.id=m.reading_id LEFT JOIN workbench_purpose_revisions p ON p.execution_id=m.execution_id AND p.revision=m.purpose_revision
 WHERE (m.purpose_revision IS NOT NULL AND (p.execution_id IS NULL OR m.event_sha256<>p.event_sha256 OR m.status IS DISTINCT FROM p.event_doc#>>'{state,status}' OR m.category IS DISTINCT FROM COALESCE(p.event_doc#>>'{state,category}','') OR p.recorded_at>r.cutoff)) OR (m.purpose_revision IS NULL AND (m.status<>'unknown' OR m.category<>'' OR m.event_sha256<>'')))`, workbench.PurposeRule).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return bad()
	}
	rows, err := q.QueryContext(ctx, `SELECT execution_id,revision,actor_subject,idempotency_key,command_sha256,previous_sha256,event_sha256,event_bytes,recorded_at FROM workbench_purpose_revisions ORDER BY execution_id,revision`)
	if err != nil {
		return err
	}
	type stored struct {
		execution, actor, key, command, previous, hash string
		revision                                       int64
		raw                                            []byte
		at                                             time.Time
	}
	events := []stored{}
	for rows.Next() {
		var e stored
		if err = rows.Scan(&e.execution, &e.revision, &e.actor, &e.key, &e.command, &e.previous, &e.hash, &e.raw, &e.at); err != nil {
			rows.Close()
			return err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	previousExecution, previousHash := "", ""
	state := workbench.EmptyPurposeState()
	for _, e := range events {
		if e.execution != previousExecution {
			previousExecution = e.execution
			previousHash = ""
			state = workbench.EmptyPurposeState()
		}
		var event workbench.PurposeEvent
		if json.Unmarshal(e.raw, &event) != nil || digestBytes(e.raw) != e.hash || event.Hash != "" || event.ExecutionID != e.execution || event.Revision != e.revision || event.Actor != e.actor || event.Command.IdempotencyKey != e.key || event.PreviousHash != previousHash || e.previous != previousHash || !event.RecordedAt.Equal(e.at) {
			return bad()
		}
		canonical, _ := json.Marshal(event)
		if !bytes.Equal(canonical, e.raw) {
			return bad()
		}
		raw, _ := json.Marshal(event.Command)
		if digestBytes(raw) != e.command {
			return bad()
		}
		next, err := workbench.ApplyPurpose(state, event.Command, event.Actor, event.NewClaimID, event.RecordedAt)
		if err != nil || purposeStateDigest(next) != purposeStateDigest(event.State) || next.Revision != e.revision {
			return bad()
		}
		for _, b := range event.Command.Basis {
			var bound bool
			if err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM evidence_events WHERE id::text=$1 AND event_hash=$2)`, b.EvidenceID, b.EventHash).Scan(&bound); err != nil {
				return err
			}
			if !bound {
				return bad()
			}
		}
		state = next
		previousHash = e.hash
	}
	rows, err = q.QueryContext(ctx, `SELECT reading_id,rule_version,status,authorization_sha256,manifest_sha256 FROM workbench_purpose_manifests`)
	if err != nil {
		return err
	}
	type header struct{ id, rule, status, auth, hash string }
	headers := []header{}
	for rows.Next() {
		var h header
		if err = rows.Scan(&h.id, &h.rule, &h.status, &h.auth, &h.hash); err != nil {
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
		if err = q.QueryRowContext(ctx, purposeManifestDigestSQL, h.id, h.rule, h.auth, h.status).Scan(&actual); err != nil {
			return err
		}
		if actual != h.hash {
			return bad()
		}
	}
	return nil
}
