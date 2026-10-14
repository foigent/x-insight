package db_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workevent"
)

func TestWorkEventMigration40To41PreservesExistingRows(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("isolated PG required")
	}
	ctx := t.Context()
	dsn := migrationTestDSN(t, ctx, base)
	legacyDir := t.TempDir()
	entries, err := os.ReadDir("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") || entry.Name() >= "000041_" {
			continue
		}
		payload, err := os.ReadFile(filepath.Join("../../migrations", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacyDir, entry.Name()), payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := db.RunMigrations(ctx, dsn, legacyDir); err != nil || count != 40 {
		t.Fatalf("schema 40 prestate: %d %v", count, err)
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	sourceID := testUUID(t)
	if _, err := admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1::uuid,$2,'unirouter','Existing source')`, sourceID, "existing:"+sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_summary) VALUES('xs_a1a1a1a1a1a1a1a1',$1::uuid,'project-9',true)`, sourceID); err != nil {
		t.Fatal(err)
	}
	var beforeSource, beforeGrant string
	if err := admin.QueryRowContext(ctx, `SELECT to_jsonb(s)::text FROM source_systems s WHERE id=$1::uuid`, sourceID).Scan(&beforeSource); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT to_jsonb(g)::text FROM workbench_access_grants g WHERE subject_id='xs_a1a1a1a1a1a1a1a1' AND source_system_id=$1::uuid`, sourceID).Scan(&beforeGrant); err != nil {
		t.Fatal(err)
	}
	currentSchema, err := strconv.Atoi(buildinfo.DatabaseSchemaMax)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := db.RunMigrations(ctx, dsn, "../../migrations"); err != nil || count != currentSchema-40 {
		t.Fatalf("schema 40 to current schema: %d %v", count, err)
	}
	var afterSource, afterGrant string
	if err := admin.QueryRowContext(ctx, `SELECT to_jsonb(s)::text FROM source_systems s WHERE id=$1::uuid`, sourceID).Scan(&afterSource); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT to_jsonb(g)::text FROM workbench_access_grants g WHERE subject_id='xs_a1a1a1a1a1a1a1a1' AND source_system_id=$1::uuid`, sourceID).Scan(&afterGrant); err != nil {
		t.Fatal(err)
	}
	if beforeSource != afterSource || beforeGrant != afterGrant {
		t.Fatal("schema 41-42 changed existing source or grant rows")
	}
}

func canonicalWorkEvent(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	object, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = connector.CanonicalizeJSONValue(object)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWorkEventIndependentAdmissionQueryAndLateRelation(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("isolated PG required")
	}
	ctx := t.Context()
	dsn := migrationTestDSN(t, ctx, base)
	if _, err := db.RunMigrations(ctx, dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	writer, err := db.OpenSourceFactsStore(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	reader, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	reporterID, targetID, collectionID := testUUID(t), testUUID(t), testUUID(t)
	subject, other := "xs_a1a1a1a1a1a1a1a1", "xs_b2b2b2b2b2b2b2b2"
	for _, entry := range []struct{ id, kind string }{{reporterID, "agent_runtime"}, {targetID, "unirouter"}} {
		if _, err := admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1::uuid,$2,$3,$4)`, entry.id, entry.kind+":"+entry.id, entry.kind, "synthetic-local"); err != nil {
			t.Fatal(err)
		}
	}
	scope := workevent.Scope{SourceSystemID: reporterID, SourceInstance: "managed-runtime-1", OrgID: "7", ProjectID: "9"}
	collection := db.WorkEventCollection{ID: collectionID, Scope: scope, Profile: workevent.CollectionProfile, ScopeGeneration: "1", Status: "staged", Revision: 1}
	registered, err := writer.RegisterWorkEventCollection(ctx, collection)
	if err != nil || registered != collection {
		t.Fatalf("registration: %+v %v", registered, err)
	}
	for _, foreign := range []workevent.Scope{
		{SourceSystemID: reporterID, SourceInstance: "managed-runtime-2", OrgID: "7", ProjectID: "9"},
		{SourceSystemID: reporterID, SourceInstance: scope.SourceInstance, OrgID: "8", ProjectID: "9"},
	} {
		candidate := collection
		candidate.ID = testUUID(t)
		candidate.Scope = foreign
		if _, err := writer.RegisterWorkEventCollection(ctx, candidate); !errors.Is(err, workevent.ErrScopeChanged) {
			t.Fatalf("same source identity crossed installation/organization: %+v %v", foreign, err)
		}
	}
	if _, err := writer.CommitWorkEvent(ctx, collection, []byte(`{}`), time.Now()); !errors.Is(err, workevent.ErrInvalid) {
		t.Fatalf("staged admission: %v", err)
	}
	active, err := writer.ActivateWorkEventCollection(ctx, collectionID, 1)
	if err != nil || active.Status != "active" || active.Revision != 2 {
		t.Fatalf("activation: %+v %v", active, err)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_resource) VALUES($1,$2::uuid,'project-9',true)`, subject, reporterID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	event := workevent.Event{SchemaVersion: workevent.EventVersion, Reporter: scope, EventRef: "event-1", Revision: 1,
		OccurredAt: now.Add(-time.Minute), RecordedAt: now, SubjectKind: "resource_action", LifecycleState: "completed", RunID: "run-1", ToolCallID: "tool-1",
		Actor: workevent.Actor{Kind: "agent", Ref: "agent-1"}, ActionKind: "resource.read",
		Resource:     &workevent.Resource{Ref: "resource-1", NameState: "readable", Name: "report.csv"},
		ContentState: "not_captured", ContentRefs: []workevent.ContentRef{}}
	raw := canonicalWorkEvent(t, event)
	if inserted, err := writer.CommitWorkEvent(ctx, active, raw, now); err != nil || !inserted {
		t.Fatalf("event admission: %v %v", inserted, err)
	}
	if inserted, err := writer.CommitWorkEvent(ctx, active, raw, now.Add(time.Second)); err != nil || inserted {
		t.Fatalf("identical retry: %v %v", inserted, err)
	}
	changed := event
	changed.LifecycleState = "failed"
	if inserted, err := writer.CommitWorkEvent(ctx, active, canonicalWorkEvent(t, changed), now); inserted || !errors.Is(err, workevent.ErrConflict) {
		t.Fatalf("conflicting retry: %v %v", inserted, err)
	}
	missing := event
	missing.EventRef = "event-without-predecessor"
	missing.Revision = 2
	missing.PreviousHash = "sha256:" + strings.Repeat("f", 64)
	if inserted, err := writer.CommitWorkEvent(ctx, active, canonicalWorkEvent(t, missing), now); inserted || !errors.Is(err, workevent.ErrUnavailable) {
		t.Fatalf("missing predecessor: %v %v", inserted, err)
	}
	eventSum := sha256.Sum256(raw)
	updated := event
	updated.Revision = 2
	updated.PreviousHash = "sha256:" + hex.EncodeToString(eventSum[:])
	updated.ContentState = "pending"
	updated.Resource = &workevent.Resource{Ref: "resource-2", NameState: "readable", Name: "corrected.csv"}
	updated.OccurredAt = now.Add(-2 * time.Minute)
	updatedRaw := canonicalWorkEvent(t, updated)
	updatedSum := sha256.Sum256(updatedRaw)
	if inserted, err := writer.CommitWorkEvent(ctx, active, updatedRaw, now); err != nil || !inserted {
		t.Fatalf("linked revision: %v %v", inserted, err)
	}
	wrong := active
	wrong.Scope.ProjectID = "10"
	if inserted, err := writer.CommitWorkEvent(ctx, wrong, raw, now); inserted || err == nil {
		t.Fatalf("scope substitution: %v %v", inserted, err)
	}
	query := db.WorkEventQuery{SourceID: reporterID, ProjectID: "9", ResourceRef: "resource-2", From: now.Add(-time.Hour), To: now.Add(time.Second), Limit: 10}
	page, err := reader.ListWorkEvents(ctx, subject, query, now.Add(2*time.Second))
	if err != nil || len(page.Items) != 1 || page.Items[0].Event.EventRef != event.EventRef {
		t.Fatalf("independent activity: %+v %v", page, err)
	}
	summary, err := reader.GetWorkActivitySummary(ctx, subject, db.WorkActivitySummaryQuery{
		From: now.Add(-time.Hour), To: now.Add(time.Second), SourceIDs: []string{reporterID}, ProjectIDs: []string{"9"},
	}, now.Add(2*time.Second))
	if err != nil || summary.ReportedActions != 1 || summary.ReportedResourceReads != 1 || summary.ReportedResourceChanges != 0 {
		t.Fatalf("summary must count the latest revision once: %+v %v", summary, err)
	}
	if _, err := reader.GetWorkActivitySummary(ctx, other, db.WorkActivitySummaryQuery{
		From: now.Add(-time.Hour), To: now.Add(time.Second),
	}, now.Add(2*time.Second)); err == nil {
		t.Fatal("unauthorized activity summary")
	}
	query.ResourceRef = "resource-1"
	stalePage, err := reader.ListWorkEvents(ctx, subject, query, now.Add(2*time.Second))
	if err != nil || len(stalePage.Items) != 0 {
		t.Fatalf("superseded resource leaked as current: %+v %v", stalePage, err)
	}
	query.ResourceRef = "resource-2"
	query.From = now.Add(-90 * time.Second)
	stalePage, err = reader.ListWorkEvents(ctx, subject, query, now.Add(2*time.Second))
	if err != nil || len(stalePage.Items) != 0 {
		t.Fatalf("superseded occurrence time leaked as current: %+v %v", stalePage, err)
	}
	query.From = now.Add(-time.Hour)
	otherSourceID, otherCollectionID := testUUID(t), testUUID(t)
	if _, err := admin.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1::uuid,$2,'agent_runtime','other-synthetic')`, otherSourceID, "agent_runtime:"+otherSourceID); err != nil {
		t.Fatal(err)
	}
	otherScope := workevent.Scope{SourceSystemID: otherSourceID, SourceInstance: "other-managed-runtime", OrgID: "8", ProjectID: "9"}
	otherCollection := db.WorkEventCollection{ID: otherCollectionID, Scope: otherScope, Profile: workevent.CollectionProfile, ScopeGeneration: "1", Status: "staged", Revision: 1}
	if _, err := writer.RegisterWorkEventCollection(ctx, otherCollection); err != nil {
		t.Fatal(err)
	}
	otherCollection, err = writer.ActivateWorkEventCollection(ctx, otherCollectionID, 1)
	if err != nil {
		t.Fatal(err)
	}
	otherEvent := event
	otherEvent.Reporter = otherScope
	if inserted, err := writer.CommitWorkEvent(ctx, otherCollection, canonicalWorkEvent(t, otherEvent), now); err != nil || !inserted {
		t.Fatalf("other source same project: %v %v", inserted, err)
	}
	query.SourceID = ""
	query.ResourceRef = ""
	page, err = reader.ListWorkEvents(ctx, subject, query, now.Add(2*time.Second))
	if err != nil || len(page.Items) != 1 || page.Items[0].Event.Reporter.SourceSystemID != reporterID {
		t.Fatalf("other source project-9 crossed grant: %+v %v", page, err)
	}
	query.SourceID = reporterID
	query.ResourceRef = "resource-2"
	if _, err := reader.ListWorkEvents(ctx, other, query, now.Add(2*time.Second)); err == nil {
		t.Fatal("unauthorized activity list")
	}
	if _, err := reader.GetWorkEvent(ctx, other, scope, event.EventRef); err == nil {
		t.Fatal("unauthorized detail")
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_resource) VALUES($1,$2::uuid,'project-9',true)`, subject, otherSourceID); err != nil {
		t.Fatal(err)
	}
	separateReporter, err := reader.GetWorkEventPage(ctx, subject, scope, event.EventRef, "", now.Add(2*time.Second))
	if err != nil || separateReporter.RunSummary == nil || separateReporter.RunSummary.ResourceReadOutcomes.Completed != 1 || len(separateReporter.RunItems) != 1 {
		t.Fatalf("same run ID in another reporter crossed run boundary: %+v %v", separateReporter, err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE workbench_access_grants SET active=false WHERE subject_id=$1 AND source_system_id=$2::uuid`, subject, otherSourceID); err != nil {
		t.Fatal(err)
	}
	movedRun := event
	movedRun.EventRef = "moved-run-event"
	movedRun.OccurredAt = now.Add(-2 * time.Hour)
	movedRun.Resource = &workevent.Resource{Ref: "moved-resource", NameState: "readable", Name: "moved.txt"}
	movedRunRaw := canonicalWorkEvent(t, movedRun)
	if inserted, err := writer.CommitWorkEvent(ctx, active, movedRunRaw, now); err != nil || !inserted {
		t.Fatalf("run predecessor: %v %v", inserted, err)
	}
	movedRunSum := sha256.Sum256(movedRunRaw)
	movedRun.Revision = 2
	movedRun.PreviousHash = "sha256:" + hex.EncodeToString(movedRunSum[:])
	movedRun.RunID = "run-2"
	if inserted, err := writer.CommitWorkEvent(ctx, active, canonicalWorkEvent(t, movedRun), now); err != nil || !inserted {
		t.Fatalf("run revision moved: %v %v", inserted, err)
	}
	detail, err := reader.GetWorkEvent(ctx, subject, scope, event.EventRef)
	if err != nil || len(detail.Relations) != 0 || len(detail.RunItems) != 1 || detail.RunItems[0].Event.EventRef != event.EventRef {
		t.Fatalf("unlinked detail: %+v %v", detail, err)
	}
	target := workevent.Scope{SourceSystemID: targetID, SourceInstance: "ur-1", OrgID: "7", ProjectID: "9"}
	relation := workevent.RelationAssertion{SchemaVersion: workevent.RelationVersion, Reporter: scope,
		RelationRef: "relation-1", EventRef: event.EventRef, EventRevision: 1,
		EventHash: "sha256:" + hex.EncodeToString(eventSum[:]), URTarget: target,
		URRequestID: "op_" + strings.Repeat("a", 32), ModelCallID: "run-1:model:1",
		EvidenceRef: "header-1", EvidenceHash: "sha256:" + strings.Repeat("b", 64), RecordedAt: now}
	if inserted, err := writer.CommitWorkEventRelation(ctx, active, canonicalWorkEvent(t, relation), now); err != nil || !inserted {
		t.Fatalf("late relation: %v %v", inserted, err)
	}
	currentRelation := relation
	currentRelation.RelationRef = "relation-2"
	currentRelation.EventRevision = 2
	currentRelation.EventHash = "sha256:" + hex.EncodeToString(updatedSum[:])
	if inserted, err := writer.CommitWorkEventRelation(ctx, active, canonicalWorkEvent(t, currentRelation), now); err != nil || !inserted {
		t.Fatalf("current revision relation: %v %v", inserted, err)
	}
	wrongHash := relation
	wrongHash.RelationRef = "relation-wrong-hash"
	wrongHash.EventHash = "sha256:" + strings.Repeat("f", 64)
	if inserted, err := writer.CommitWorkEventRelation(ctx, active, canonicalWorkEvent(t, wrongHash), now); inserted || !errors.Is(err, workevent.ErrConflict) {
		t.Fatalf("relation bound to wrong event hash: %v %v", inserted, err)
	}
	detail, err = reader.GetWorkEvent(ctx, subject, scope, event.EventRef)
	if err != nil || len(detail.Relations) != 0 {
		t.Fatalf("target grant bypass: %+v %v", detail, err)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_source_runtime) VALUES($1,$2::uuid,'project-9',true)`, subject, targetID); err != nil {
		t.Fatal(err)
	}
	detail, err = reader.GetWorkEvent(ctx, subject, scope, event.EventRef)
	if err != nil || len(detail.Relations) != 2 || detail.Relations[0].State != "source_asserted_unverified" ||
		detail.Relations[0].BindingState != "prior_revision" || detail.Relations[1].BindingState != "current_revision" {
		t.Fatalf("source assertion misrepresented: %+v %v", detail, err)
	}
	// A typed UR B row proves only exact server-identifier presence; it does not
	// prove tool success, body availability or independent file verification.
	observation := map[string]any{
		"schema_version": "unirouter.source-observation.v1", "source_instance": target.SourceInstance,
		"org_id": target.OrgID, "project_id": target.ProjectID, "request_id": relation.URRequestID,
		"profile": "unirouter.runtime-observation.v1", "subject_kind": "request",
		"subject_id": relation.URRequestID, "observation_kind": "request_started",
		"source_fact_id": "uf1_" + strings.Repeat("c", 64), "event_id": "uf1_" + strings.Repeat("d", 64), "revision": "1",
	}
	bytes := canonicalWorkEvent(t, observation)
	sum := sha256.Sum256(bytes)
	hash := "sha256:" + hex.EncodeToString(sum[:])
	if _, err := admin.ExecContext(ctx, `INSERT INTO source_observation_documents(source_system_id,source_instance,org_id,project_id,request_id,profile,subject_kind,subject_id,observation_kind,source_fact_id,revision,event_id,previous_hash,source_hash,envelope_hash,source_bytes,envelope_bytes,source_json,occurred_at,recorded_at)
 VALUES($1::uuid,$2,$3,$4,$5,'unirouter.runtime-observation.v1','request',$5,'request_started',$6,1,$7,'',$8,$8,$9,$9,$10::jsonb,$11,$11)`,
		targetID, target.SourceInstance, target.OrgID, target.ProjectID, relation.URRequestID,
		observation["source_fact_id"], observation["event_id"], hash, bytes, string(bytes), now); err != nil {
		t.Fatal(err)
	}
	detail, err = reader.GetWorkEvent(ctx, subject, scope, event.EventRef)
	if err != nil || len(detail.Relations) != 2 || detail.Relations[0].State != "ur_identifier_matched" || detail.Relations[1].State != "ur_identifier_matched" ||
		detail.Relations[1].RuntimeRequestRef == "" || detail.Relations[1].ObservedAt == nil {
		t.Fatalf("exact UR identifier matching: %+v %v", detail, err)
	}
	if len(detail.RunItems) != 1 || detail.RunItems[0].Event.EventRef != event.EventRef || detail.RunItemsMore {
		t.Fatalf("exact source-run observations: %+v", detail.RunItems)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE workbench_access_grants SET active=false WHERE subject_id=$1 AND source_system_id=$2::uuid`, subject, targetID); err != nil {
		t.Fatal(err)
	}
	detail, err = reader.GetWorkEvent(ctx, subject, scope, event.EventRef)
	if err != nil || len(detail.Relations) != 0 {
		t.Fatalf("target grant revocation: %+v %v", detail, err)
	}
	taskEvent := workevent.Event{SchemaVersion: workevent.EventVersion, Reporter: scope, EventRef: "task-start-1", Revision: 1,
		OccurredAt: now.Add(-30 * time.Second), RecordedAt: now, SubjectKind: "task", LifecycleState: "started",
		TaskRef: "task-1", Actor: workevent.Actor{Kind: "unknown"}, ActionKind: "task.started",
		ContentState: "not_captured", ContentRefs: []workevent.ContentRef{}}
	if inserted, err := writer.CommitWorkEvent(ctx, active, canonicalWorkEvent(t, taskEvent), now); err != nil || !inserted {
		t.Fatalf("task start without fabricated run/tool/resource: %v %v", inserted, err)
	}
	taskDetail, err := reader.GetWorkEvent(ctx, subject, scope, taskEvent.EventRef)
	if err != nil || taskDetail.Item.Event.LifecycleState != "started" || taskDetail.Item.Event.Resource != nil || taskDetail.Item.Event.ToolCallID != "" {
		t.Fatalf("task start readback: %+v %v", taskDetail, err)
	}
	noRun, err := reader.GetWorkEventPage(ctx, subject, scope, taskEvent.EventRef, "", now.Add(2*time.Second))
	if err != nil || noRun.RunSummary != nil || noRun.RunCutoff != nil || len(noRun.RunItems) != 0 || noRun.RunItemsNextCursor != "" {
		t.Fatalf("event without run must not invent a run: %+v %v", noRun, err)
	}
	outside := updated
	outside.Revision = 3
	outside.PreviousHash = "sha256:" + hex.EncodeToString(updatedSum[:])
	outside.OccurredAt = now.Add(-2 * time.Hour)
	if inserted, err := writer.CommitWorkEvent(ctx, active, canonicalWorkEvent(t, outside), now); err != nil || !inserted {
		t.Fatalf("new revision outside summary period: %v %v", inserted, err)
	}
	summary, err = reader.GetWorkActivitySummary(ctx, subject, db.WorkActivitySummaryQuery{
		From: now.Add(-time.Hour), To: now.Add(time.Second), SourceIDs: []string{reporterID}, ProjectIDs: []string{"9"},
	}, now.Add(2*time.Second))
	if err != nil || summary.ReportedActions != 1 || summary.ReportedResourceReads != 0 {
		t.Fatalf("old in-period revision was counted after latest moved out: %+v %v", summary, err)
	}
	outcomeExamples := []struct{ ref, action, lifecycle string }{
		{"failed-read", "resource.read.failed", "failed"},
		{"optional-missing", "resource.read.not_found", "failed"},
		{"missing-unknown", "resource.read.not_found", "unknown"},
		{"missing-started", "resource.read.not_found", "started"},
		{"missing-completed", "resource.read.not_found", "completed"},
		{"unknown-write", "resource.write", "unknown"},
		{"policy-action", "resource.write_policy", "completed"},
		{"inspect-action", "resource.inspect", "completed"},
	}
	for _, example := range outcomeExamples {
		observed := event
		observed.EventRef, observed.ActionKind, observed.LifecycleState = example.ref, example.action, example.lifecycle
		observed.OccurredAt = now.Add(-35 * time.Second)
		observed.Resource = &workevent.Resource{Ref: example.ref, NameState: "readable", Name: example.ref + ".txt"}
		if inserted, err := writer.CommitWorkEvent(ctx, active, canonicalWorkEvent(t, observed), now); err != nil || !inserted {
			t.Fatalf("outcome fixture %s: %v %v", example.ref, inserted, err)
		}
	}
	negative, err := reader.GetWorkActivitySummary(ctx, subject, db.WorkActivitySummaryQuery{
		From: now.Add(-time.Hour), To: now.Add(time.Second), SourceIDs: []string{reporterID}, ProjectIDs: []string{"9"},
	}, now.Add(2*time.Second))
	if err != nil || negative.ResourceReadOutcomes.Failed != 1 || negative.ResourceReadOutcomes.OptionalNotFound != 1 ||
		negative.ResourceReadOutcomes.Unknown != 3 || negative.ResourceReadOutcomes.Completed != 0 ||
		negative.ResourceChangeOutcomes.Unknown != 1 || negative.ReportedResourceChanges != 1 || negative.LegacyResourceChanges != 2 {
		t.Fatalf("source-reported failure, optional absence, and unknown outcome: %+v %v", negative, err)
	}
	// The detail aggregate covers the full current run before the 100-item
	// display page. Equal display names remain separate source resource refs.
	const bulkCount = 105
	const firstPageSize = 100
	expectedRunCount := 1 + len(outcomeExamples) + bulkCount
	var firstBulkRaw []byte
	for i := 0; i < bulkCount; i++ {
		bulk := event
		bulk.EventRef = fmt.Sprintf("bulk-%03d", i)
		bulk.PreviousHash = ""
		bulk.OccurredAt = now.Add(-30*time.Second + time.Duration(i)*time.Millisecond)
		bulk.Resource = &workevent.Resource{Ref: fmt.Sprintf("bulk-resource-%03d", i), NameState: "readable", Name: "same-name.txt"}
		encoded := canonicalWorkEvent(t, bulk)
		if i == 0 {
			firstBulkRaw = encoded
		}
		if inserted, err := writer.CommitWorkEvent(ctx, active, encoded, now); err != nil || !inserted {
			t.Fatalf("bulk run event %d: %v %v", i, inserted, err)
		}
	}
	cutoff := now.Add(2 * time.Second)
	full, err := reader.GetWorkEventPage(ctx, subject, scope, event.EventRef, "", cutoff)
	if err != nil || full.RunSummary == nil || full.RunSummary.ResourceReadOutcomes.Completed != int64(bulkCount+1) ||
		full.RunSummary.ResourceReadOutcomes.Failed != 1 || full.RunSummary.ResourceReadOutcomes.OptionalNotFound != 1 ||
		full.RunSummary.ResourceReadOutcomes.Unknown != 3 || full.RunSummary.ResourceChangeOutcomes.Unknown != 1 ||
		full.RunSummary.DistinctResources != int64(expectedRunCount) ||
		len(full.RunItems) != firstPageSize || !full.RunItemsMore || full.RunItemsNextCursor == "" {
		t.Fatalf("whole-run aggregate or first bounded page: %+v %v", full, err)
	}
	if _, err := reader.GetWorkEventPage(ctx, other, scope, event.EventRef, full.RunItemsNextCursor, cutoff); err == nil {
		t.Fatal("another subject used the run cursor")
	}
	if _, err := reader.GetWorkEventPage(ctx, subject, scope, movedRun.EventRef, full.RunItemsNextCursor, cutoff); !errors.Is(err, workbench.ErrInvalid) {
		t.Fatalf("cursor crossed selected event or run: %v", err)
	}
	decodedCursor, err := base64.RawURLEncoding.DecodeString(full.RunItemsNextCursor)
	if err != nil {
		t.Fatal(err)
	}
	var changedCutoff map[string]any
	if err = json.Unmarshal(decodedCursor, &changedCutoff); err != nil {
		t.Fatal(err)
	}
	changedCutoff["cutoff"] = cutoff.Add(time.Second).Format(time.RFC3339Nano)
	changedBytes, err := json.Marshal(changedCutoff)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reader.GetWorkEventPage(ctx, subject, scope, event.EventRef,
		base64.RawURLEncoding.EncodeToString(changedBytes), cutoff.Add(2*time.Second)); !errors.Is(err, workbench.ErrInvalid) {
		t.Fatalf("cursor changed received-time cutoff: %v", err)
	}
	firstSum := sha256.Sum256(firstBulkRaw)
	bulkRevision := event
	bulkRevision.EventRef = "bulk-000"
	bulkRevision.Revision = 2
	bulkRevision.PreviousHash = "sha256:" + hex.EncodeToString(firstSum[:])
	bulkRevision.RunID = "run-2"
	bulkRevision.OccurredAt = now.Add(-30 * time.Second)
	bulkRevision.Resource = &workevent.Resource{Ref: "bulk-resource-000", NameState: "readable", Name: "same-name.txt"}
	if inserted, err := writer.CommitWorkEvent(ctx, active, canonicalWorkEvent(t, bulkRevision), cutoff.Add(time.Second)); err != nil || !inserted {
		t.Fatalf("late moved run revision: %v %v", inserted, err)
	}
	second, err := reader.GetWorkEventPage(ctx, subject, scope, event.EventRef, full.RunItemsNextCursor, cutoff.Add(2*time.Second))
	if err != nil || len(second.RunItems) != expectedRunCount-firstPageSize || second.RunItemsMore || second.RunSummary == nil ||
		second.RunSummary.ResourceReadOutcomes.Completed != int64(bulkCount+1) {
		t.Fatalf("cursor cutoff or second page: %+v %v", second, err)
	}
	fresh, err := reader.GetWorkEventPage(ctx, subject, scope, event.EventRef, "", cutoff.Add(2*time.Second))
	if err != nil || fresh.RunSummary == nil || fresh.RunSummary.ResourceReadOutcomes.Completed != int64(bulkCount) {
		t.Fatalf("latest revision must move out of run before aggregate: %+v %v", fresh, err)
	}
	// Ingest receives its timestamp before acquiring the source lock. A
	// backdated member can therefore commit after page one was read.
	lockTx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lockTx.Rollback()
	if _, err = lockTx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "source-facts:"+reporterID); err != nil {
		t.Fatal(err)
	}
	late := event
	late.EventRef, late.PreviousHash = "late-backdated-member", ""
	late.Resource = &workevent.Resource{Ref: "late-backdated-resource", NameState: "readable", Name: "late.txt"}
	lateRaw := canonicalWorkEvent(t, late)
	raceCutoff := time.Now().UTC().Add(2 * time.Second)
	committed := make(chan error, 1)
	go func() {
		inserted, commitErr := writer.CommitWorkEvent(ctx, active, lateRaw, raceCutoff.Add(-time.Microsecond))
		if commitErr == nil && !inserted {
			commitErr = errors.New("late member was not inserted")
		}
		committed <- commitErr
	}()
	raceFirst, readErr := reader.GetWorkEventPage(ctx, subject, scope, event.EventRef, "", raceCutoff)
	if unlockErr := lockTx.Commit(); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	if readErr != nil || raceFirst.RunItemsNextCursor == "" {
		t.Fatalf("first page while ingest blocked: %+v %v", raceFirst, readErr)
	}
	if commitErr := <-committed; commitErr != nil {
		t.Fatal(commitErr)
	}
	if _, err = reader.GetWorkEventPage(ctx, subject, scope, event.EventRef, raceFirst.RunItemsNextCursor, raceCutoff.Add(time.Second)); !errors.Is(err, workbench.ErrConflict) {
		t.Fatalf("changed run membership must reject continuation: %v", err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE workbench_access_grants SET active=false WHERE subject_id=$1 AND source_system_id=$2::uuid`, subject, reporterID); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.GetWorkEventPage(ctx, subject, scope, event.EventRef, full.RunItemsNextCursor, cutoff.Add(2*time.Second)); err == nil {
		t.Fatal("revoked subject used run cursor")
	}
	paused, err := writer.ControlWorkEventCollection(ctx, collectionID, "paused", active.Revision)
	if err != nil || paused.Status != "paused" {
		t.Fatalf("pause: %+v %v", paused, err)
	}
	if inserted, err := writer.CommitWorkEvent(ctx, active, raw, now); inserted || err == nil {
		t.Fatalf("stale active registration admitted: %v %v", inserted, err)
	}
	reopened, err := writer.ActivateWorkEventCollection(ctx, collectionID, paused.Revision)
	if err != nil || reopened.Status != "active" {
		t.Fatalf("resume: %+v %v", reopened, err)
	}
	revoked, err := writer.ControlWorkEventCollection(ctx, collectionID, "revoked", reopened.Revision)
	if err != nil || revoked.Status != "revoked" {
		t.Fatalf("revoke: %+v %v", revoked, err)
	}
	if _, err := writer.ActivateWorkEventCollection(ctx, collectionID, revoked.Revision); !errors.Is(err, workevent.ErrScopeChanged) {
		t.Fatalf("revoked registration revived: %v", err)
	}
}
