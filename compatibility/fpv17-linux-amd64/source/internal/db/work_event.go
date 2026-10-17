package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workevent"
)

type WorkEventCollection struct {
	ID              string
	Scope           workevent.Scope
	Profile         string
	ScopeGeneration string
	Status          string
	Revision        int64
}

var workEventUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var workEventGeneration = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

func (c WorkEventCollection) valid() bool {
	return workEventUUID.MatchString(c.ID) && c.Scope.Valid() && c.Profile == workevent.CollectionProfile && workEventGeneration.MatchString(c.ScopeGeneration) &&
		(c.Status == "staged" || c.Status == "active" || c.Status == "paused" || c.Status == "revoked") && c.Revision > 0
}

func workEventSourceScopeUnambiguous(ctx context.Context, q workspaceQueryer, scope workevent.Scope) (bool, error) {
	var foreignScope bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM work_event_collections WHERE source_system_id=$1::uuid AND (source_instance<>$2 OR org_id<>$3))`,
		scope.SourceSystemID, scope.SourceInstance, scope.OrgID).Scan(&foreignScope)
	return !foreignScope, err
}

func loadWorkEventCollection(ctx context.Context, q workspaceQueryer, id string, lock bool) (WorkEventCollection, error) {
	var c WorkEventCollection
	query := `SELECT id::text,source_system_id::text,source_instance,org_id,project_id,profile,scope_generation,status,revision FROM work_event_collections WHERE id=$1::uuid`
	if lock {
		query += ` FOR UPDATE`
	}
	err := q.QueryRowContext(ctx, query, id).Scan(&c.ID, &c.Scope.SourceSystemID, &c.Scope.SourceInstance,
		&c.Scope.OrgID, &c.Scope.ProjectID, &c.Profile, &c.ScopeGeneration, &c.Status, &c.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return c, workevent.ErrUnavailable
	}
	return c, err
}

func (s *SourceFactsStore) LoadWorkEventCollection(ctx context.Context, id string) (WorkEventCollection, error) {
	return loadWorkEventCollection(ctx, s.db, id, false)
}

// Registration is an administrator operation. The source must already be
// identified as agent_runtime; the UR managed source identity is never reused.
func (s *SourceFactsStore) RegisterWorkEventCollection(ctx context.Context, c WorkEventCollection) (WorkEventCollection, error) {
	if !c.valid() || c.Status != "staged" || c.Revision != 1 {
		return WorkEventCollection{}, workevent.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	if err = sourceLock(ctx, tx, c.Scope.SourceSystemID); err != nil {
		return c, err
	}
	var admin, source bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_has_role(current_user,relowner,'USAGE') FROM pg_class WHERE oid='public.work_event_collections'::regclass`).Scan(&admin); err != nil {
		return c, err
	}
	if !admin {
		return c, workevent.ErrUnavailable
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_systems WHERE id=$1::uuid AND source_type='agent_runtime' AND status='active')`, c.Scope.SourceSystemID).Scan(&source); err != nil {
		return c, err
	}
	if !source {
		return c, workevent.ErrScopeChanged
	}
	// A source identity belongs to one agent installation and organization.
	// Project grants can then use source_system_id + project_id without
	// accidentally crossing another instance or organization with the same ID.
	unambiguous, err := workEventSourceScopeUnambiguous(ctx, tx, c.Scope)
	if err != nil {
		return c, err
	}
	if !unambiguous {
		return c, workevent.ErrScopeChanged
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO work_event_collections(id,source_system_id,source_instance,org_id,project_id,profile,scope_generation)
 VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7) ON CONFLICT(id) DO NOTHING`, c.ID, c.Scope.SourceSystemID, c.Scope.SourceInstance, c.Scope.OrgID, c.Scope.ProjectID, c.Profile, c.ScopeGeneration)
	if err != nil {
		return c, err
	}
	live, err := loadWorkEventCollection(ctx, tx, c.ID, true)
	if err != nil {
		return c, err
	}
	if !reflect.DeepEqual(live, c) {
		return c, workevent.ErrConflict
	}
	return live, tx.Commit()
}

func (s *SourceFactsStore) ActivateWorkEventCollection(ctx context.Context, id string, expectedRevision int64) (WorkEventCollection, error) {
	return s.ControlWorkEventCollection(ctx, id, "active", expectedRevision)
}

// ControlWorkEventCollection is an administrator-only, revision-checked status
// change. Revocation is terminal and cannot be undone by a stale worker.
func (s *SourceFactsStore) ControlWorkEventCollection(ctx context.Context, id, status string, expectedRevision int64) (WorkEventCollection, error) {
	if !workEventUUID.MatchString(id) || expectedRevision < 1 || (status != "active" && status != "paused" && status != "revoked") {
		return WorkEventCollection{}, workevent.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkEventCollection{}, err
	}
	defer tx.Rollback()
	var admin bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_has_role(current_user,relowner,'USAGE') FROM pg_class WHERE oid='public.work_event_collections'::regclass`).Scan(&admin); err != nil {
		return WorkEventCollection{}, err
	}
	if !admin {
		return WorkEventCollection{}, workevent.ErrUnavailable
	}
	var sourceID string
	if err = tx.QueryRowContext(ctx, `SELECT source_system_id::text FROM work_event_collections WHERE id=$1::uuid`, id).Scan(&sourceID); err != nil {
		return WorkEventCollection{}, err
	}
	if err = sourceLock(ctx, tx, sourceID); err != nil {
		return WorkEventCollection{}, err
	}
	live, err := loadWorkEventCollection(ctx, tx, id, true)
	if err != nil {
		return live, err
	}
	if live.Revision != expectedRevision || live.Status == "revoked" ||
		(live.Status == "staged" && status != "active") {
		return live, workevent.ErrScopeChanged
	}
	if live.Status != status {
		if _, err = tx.ExecContext(ctx, `UPDATE work_event_collections SET status=$2,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, id, status); err != nil {
			return live, err
		}
	}
	live, err = loadWorkEventCollection(ctx, tx, id, false)
	if err != nil {
		return live, err
	}
	return live, tx.Commit()
}

func workEventHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *SourceFactsStore) CommitWorkEvent(ctx context.Context, expected WorkEventCollection, raw []byte, receivedAt time.Time) (bool, error) {
	event, err := workevent.DecodeEvent(raw)
	if err != nil || !expected.valid() || expected.Status != "active" || receivedAt.IsZero() || event.Reporter != expected.Scope {
		return false, workevent.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err = sourceLock(ctx, tx, expected.Scope.SourceSystemID); err != nil {
		return false, err
	}
	live, err := loadWorkEventCollection(ctx, tx, expected.ID, true)
	if err != nil {
		return false, err
	}
	if live != expected || live.Status != "active" {
		return false, workevent.ErrScopeChanged
	}
	var source bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_systems WHERE id=$1::uuid AND source_type='agent_runtime' AND status='active')`, expected.Scope.SourceSystemID).Scan(&source); err != nil {
		return false, err
	}
	if !source {
		return false, workevent.ErrScopeChanged
	}
	unambiguous, err := workEventSourceScopeUnambiguous(ctx, tx, expected.Scope)
	if err != nil {
		return false, err
	}
	if !unambiguous {
		return false, workevent.ErrScopeChanged
	}
	inserted, err := insertWorkEventTx(ctx, tx, event, raw, receivedAt)
	if err != nil {
		return false, err
	}
	return inserted, tx.Commit()
}

func insertWorkEventTx(ctx context.Context, tx *sql.Tx, event workevent.Event, raw []byte, receivedAt time.Time) (bool, error) {
	var err error
	hash := workEventHash(raw)
	var existing []byte
	err = tx.QueryRowContext(ctx, `SELECT event_bytes FROM work_event_documents WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND event_ref=$5 AND revision=$6`,
		event.Reporter.SourceSystemID, event.Reporter.SourceInstance, event.Reporter.OrgID, event.Reporter.ProjectID, event.EventRef, event.Revision).Scan(&existing)
	if err == nil {
		if !bytes.Equal(existing, raw) {
			return false, workevent.ErrConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var conflict bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM work_event_documents WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND event_ref=$5 AND
 ((revision=$6::bigint-1 AND event_hash<>$7) OR (revision=$6::bigint+1 AND previous_hash<>$8)))`,
		event.Reporter.SourceSystemID, event.Reporter.SourceInstance, event.Reporter.OrgID, event.Reporter.ProjectID,
		event.EventRef, event.Revision, event.PreviousHash, hash).Scan(&conflict); err != nil {
		return false, err
	}
	if conflict {
		return false, workevent.ErrConflict
	}
	if event.Revision > 1 {
		var predecessor string
		err = tx.QueryRowContext(ctx, `SELECT event_hash FROM work_event_documents WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND event_ref=$5 AND revision=$6`,
			event.Reporter.SourceSystemID, event.Reporter.SourceInstance, event.Reporter.OrgID, event.Reporter.ProjectID, event.EventRef, event.Revision-1).Scan(&predecessor)
		if errors.Is(err, sql.ErrNoRows) {
			return false, workevent.ErrUnavailable
		}
		if err != nil {
			return false, err
		}
		if predecessor != event.PreviousHash {
			return false, workevent.ErrConflict
		}
	}
	resourceRef, resourceName := "", ""
	if event.Resource != nil {
		resourceRef, resourceName = event.Resource.Ref, event.Resource.Name
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO work_event_documents(source_system_id,source_instance,org_id,project_id,event_ref,revision,previous_hash,event_hash,occurred_at,recorded_at,received_at,subject_kind,lifecycle_state,task_ref,run_id,model_call_id,tool_call_id,action_kind,resource_ref,resource_name,actor_kind,actor_ref,content_state,event_bytes,event_json)
 VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25::jsonb)`,
		event.Reporter.SourceSystemID, event.Reporter.SourceInstance, event.Reporter.OrgID, event.Reporter.ProjectID,
		event.EventRef, event.Revision, event.PreviousHash, hash, event.OccurredAt, event.RecordedAt, receivedAt.UTC(),
		event.SubjectKind, event.LifecycleState, event.TaskRef, event.RunID, event.ModelCallID, event.ToolCallID,
		event.ActionKind, resourceRef, resourceName, event.Actor.Kind, event.Actor.Ref, event.ContentState, raw, string(raw))
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *SourceFactsStore) CommitWorkEventRelation(ctx context.Context, expected WorkEventCollection, raw []byte, receivedAt time.Time) (bool, error) {
	relation, err := workevent.DecodeRelation(raw)
	if err != nil || !expected.valid() || expected.Status != "active" || receivedAt.IsZero() || relation.Reporter != expected.Scope {
		return false, workevent.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err = sourceLock(ctx, tx, expected.Scope.SourceSystemID); err != nil {
		return false, err
	}
	live, err := loadWorkEventCollection(ctx, tx, expected.ID, true)
	if err != nil {
		return false, err
	}
	if live != expected || live.Status != "active" {
		return false, workevent.ErrScopeChanged
	}
	var source, target bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_systems WHERE id=$1::uuid AND source_type='agent_runtime' AND status='active'),EXISTS(SELECT 1 FROM source_systems WHERE id=$2::uuid AND source_type='unirouter')`, expected.Scope.SourceSystemID, relation.URTarget.SourceSystemID).Scan(&source, &target); err != nil {
		return false, err
	}
	if !source || !target {
		return false, workevent.ErrScopeChanged
	}
	unambiguous, err := workEventSourceScopeUnambiguous(ctx, tx, expected.Scope)
	if err != nil {
		return false, err
	}
	if !unambiguous {
		return false, workevent.ErrScopeChanged
	}
	inserted, err := insertWorkEventRelationTx(ctx, tx, relation, raw, receivedAt)
	if err != nil {
		return false, err
	}
	return inserted, tx.Commit()
}

func insertWorkEventRelationTx(ctx context.Context, tx *sql.Tx, relation workevent.RelationAssertion, raw []byte, receivedAt time.Time) (bool, error) {
	var err error
	// Relation may arrive after the source event; it cannot invent an event.
	var eventHash string
	if err = tx.QueryRowContext(ctx, `SELECT event_hash FROM work_event_documents WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND event_ref=$5 AND revision=$6`,
		relation.Reporter.SourceSystemID, relation.Reporter.SourceInstance, relation.Reporter.OrgID, relation.Reporter.ProjectID, relation.EventRef, relation.EventRevision).Scan(&eventHash); errors.Is(err, sql.ErrNoRows) {
		return false, workevent.ErrUnavailable
	} else if err != nil {
		return false, err
	}
	if eventHash != relation.EventHash {
		return false, workevent.ErrConflict
	}
	var existing []byte
	err = tx.QueryRowContext(ctx, `SELECT relation_bytes FROM work_event_relations WHERE reporter_source_system_id=$1::uuid AND reporter_source_instance=$2 AND reporter_org_id=$3 AND reporter_project_id=$4 AND relation_ref=$5`,
		relation.Reporter.SourceSystemID, relation.Reporter.SourceInstance, relation.Reporter.OrgID, relation.Reporter.ProjectID, relation.RelationRef).Scan(&existing)
	if err == nil {
		if !bytes.Equal(existing, raw) {
			return false, workevent.ErrConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO work_event_relations(reporter_source_system_id,reporter_source_instance,reporter_org_id,reporter_project_id,relation_ref,event_ref,event_revision,event_hash,target_source_system_id,target_source_instance,target_org_id,target_project_id,target_request_id,model_call_id,evidence_ref,evidence_sha256,recorded_at,received_at,relation_hash,relation_bytes,relation_json)
 VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9::uuid,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21::jsonb)`,
		relation.Reporter.SourceSystemID, relation.Reporter.SourceInstance, relation.Reporter.OrgID, relation.Reporter.ProjectID,
		relation.RelationRef, relation.EventRef, relation.EventRevision, relation.EventHash, relation.URTarget.SourceSystemID, relation.URTarget.SourceInstance,
		relation.URTarget.OrgID, relation.URTarget.ProjectID, relation.URRequestID, relation.ModelCallID, relation.EvidenceRef,
		relation.EvidenceHash, relation.RecordedAt, receivedAt.UTC(), workEventHash(raw), raw, string(raw))
	if err != nil {
		return false, err
	}
	return true, nil
}

type WorkEventQuery struct {
	SourceID     string
	ProjectID    string
	SourceIDs    []string
	ProjectIDs   []string
	ResourceRef  string
	ResourceName string
	From         time.Time
	To           time.Time
	Limit        int
	Cursor       string
}

type WorkEventScopeChoice struct {
	SourceID       string `json:"source_id"`
	SourceLabel    string `json:"source_label"`
	ProjectLabel   string `json:"project_label,omitempty"`
	SourceInstance string `json:"source_instance"`
	OrgID          string `json:"org_id"`
	ProjectID      string `json:"project_id"`
}

func (s *WorkspaceStore) ListWorkEventScopes(ctx context.Context, subject string) ([]WorkEventScopeChoice, error) {
	result := []WorkEventScopeChoice{}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT c.source_system_id::text,s.display_name,c.source_instance,c.org_id,c.project_id,COALESCE(p.display_name,'')
 FROM work_event_collections c JOIN source_systems s ON s.id=c.source_system_id
 LEFT JOIN review_capture_project_bindings b ON b.source_system_id=c.source_system_id
 AND b.source_instance=c.source_instance AND b.org_id=c.org_id AND b.project_id=c.project_id
 LEFT JOIN review_capture_projects p ON p.xi_project_id=b.xi_project_id
 WHERE (c.status IN ('active','paused') OR EXISTS(SELECT 1 FROM work_event_documents d WHERE d.source_system_id=c.source_system_id AND d.source_instance=c.source_instance AND d.org_id=c.org_id AND d.project_id=c.project_id))
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$1 AND g.active AND g.allow_resource AND
 g.source_system_id=c.source_system_id AND g.cost_project_ref IS NULL AND
 (g.source_project_ref IS NULL OR g.source_project_ref='project-'||c.project_id))
 ORDER BY s.display_name,c.source_system_id::text,c.source_instance,c.org_id,c.project_id`, subject)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var choice WorkEventScopeChoice
		if err := rows.Scan(&choice.SourceID, &choice.SourceLabel, &choice.SourceInstance, &choice.OrgID, &choice.ProjectID, &choice.ProjectLabel); err != nil {
			return result, err
		}
		result = append(result, choice)
	}
	return result, rows.Err()
}

type WorkEventItem struct {
	Event      workevent.Event `json:"event"`
	EventHash  string          `json:"event_hash"`
	ReceivedAt time.Time       `json:"received_at"`
}

type WorkEventPage struct {
	Version    string          `json:"version"`
	Cutoff     time.Time       `json:"cutoff"`
	Items      []WorkEventItem `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type workEventCursor struct {
	Scope  string    `json:"scope"`
	Cutoff time.Time `json:"cutoff"`
	Offset int       `json:"offset"`
}

func (s *WorkspaceStore) ListWorkEvents(ctx context.Context, subject string, query WorkEventQuery, now time.Time) (WorkEventPage, error) {
	result := WorkEventPage{Version: "xinsight.work-activity.v1", Cutoff: now.UTC(), Items: []WorkEventItem{}}
	if query.Limit < 1 || query.Limit > 100 || !query.From.Before(query.To) || query.To.After(now.Add(time.Second)) ||
		query.To.Sub(query.From) > 31*24*time.Hour || query.SourceID != "" && !(workevent.Scope{SourceSystemID: query.SourceID, SourceInstance: "x", OrgID: "1", ProjectID: "1"}).Valid() ||
		query.ProjectID != "" && !(workevent.Scope{SourceSystemID: "00000000-0000-0000-0000-000000000000", SourceInstance: "x", OrgID: "1", ProjectID: query.ProjectID}).Valid() ||
		query.ResourceRef != "" && len(query.ResourceRef) > 128 ||
		len(query.ResourceName) > 160 || strings.TrimSpace(query.ResourceName) != query.ResourceName ||
		len(query.SourceIDs) > 16 || len(query.ProjectIDs) > 16 ||
		(query.SourceID != "" && len(query.SourceIDs) > 0) || (query.ProjectID != "" && len(query.ProjectIDs) > 0) {
		return result, workbench.ErrInvalid
	}
	for _, source := range query.SourceIDs {
		if !(workevent.Scope{SourceSystemID: source, SourceInstance: "x", OrgID: "1", ProjectID: "1"}).Valid() {
			return result, workbench.ErrInvalid
		}
	}
	for _, project := range query.ProjectIDs {
		if !(workevent.Scope{SourceSystemID: "00000000-0000-0000-0000-000000000000", SourceInstance: "x", OrgID: "1", ProjectID: project}).Valid() {
			return result, workbench.ErrInvalid
		}
	}
	auth, err := loadWorkbenchAuthorization(ctx, s.db, subject)
	if err != nil {
		return result, err
	}
	allowed := false
	for _, grant := range auth.Grants {
		if grant.AllowResource && grant.CostProject == "" && (query.SourceID == "" || query.SourceID == grant.SourceSystemID) &&
			(query.ProjectID == "" || grant.SourceProject == "" || grant.SourceProject == "project-"+query.ProjectID) {
			allowed = true
		}
	}
	if !allowed {
		return result, workbench.ErrScopeForbidden
	}
	if query.SourceID != "" {
		query.SourceIDs = []string{query.SourceID}
	}
	if query.ProjectID != "" {
		query.ProjectIDs = []string{query.ProjectID}
	}
	if query.SourceIDs == nil {
		query.SourceIDs = []string{}
	}
	if query.ProjectIDs == nil {
		query.ProjectIDs = []string{}
	}
	binding, _ := json.Marshal([]any{"xinsight.work-activity.v1", subject, auth.ObservationRevision, query.SourceIDs, query.ProjectIDs, query.ResourceRef, query.ResourceName, query.From.UTC(), query.To.UTC(), query.Limit})
	position := workEventCursor{Scope: workEventHash(binding), Cutoff: now.UTC()}
	if query.Cursor != "" {
		raw, decodeErr := base64.RawURLEncoding.DecodeString(query.Cursor)
		var previous workEventCursor
		if decodeErr != nil || len(raw) > 1024 || json.Unmarshal(raw, &previous) != nil || previous.Scope != position.Scope || previous.Offset < 0 || previous.Offset > 100000 || previous.Cutoff.IsZero() || previous.Cutoff.After(now) {
			return result, workbench.ErrInvalid
		}
		position = previous
	}
	result.Cutoff = position.Cutoff
	rows, err := s.db.QueryContext(ctx, `SELECT d.event_bytes,d.event_hash,d.received_at FROM work_event_documents d
 WHERE d.received_at<=$1 AND d.occurred_at>=$2 AND d.occurred_at<$3
 AND (cardinality($4::text[])=0 OR d.source_system_id::text=ANY($4::text[]))
 AND (cardinality($5::text[])=0 OR d.project_id=ANY($5::text[])) AND ($6='' OR d.resource_ref=$6)
 AND ($10='' OR (d.resource_name<>'' AND strpos(lower(d.resource_name),lower($10))>0))
 AND NOT EXISTS(SELECT 1 FROM work_event_documents newer WHERE newer.source_system_id=d.source_system_id AND
 newer.source_instance=d.source_instance AND newer.org_id=d.org_id AND newer.project_id=d.project_id AND
 newer.event_ref=d.event_ref AND newer.revision>d.revision AND newer.received_at<=$1)
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$7 AND g.active AND g.allow_resource AND
 g.source_system_id=d.source_system_id AND g.cost_project_ref IS NULL AND
 (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id))
 ORDER BY d.occurred_at DESC,d.source_system_id,d.source_instance COLLATE "C",d.org_id COLLATE "C",d.project_id COLLATE "C",d.event_ref COLLATE "C"
 LIMIT $8 OFFSET $9`, position.Cutoff, query.From.UTC(), query.To.UTC(), query.SourceIDs, query.ProjectIDs, query.ResourceRef, subject, query.Limit+1, position.Offset, query.ResourceName)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item WorkEventItem
		var raw []byte
		if err = rows.Scan(&raw, &item.EventHash, &item.ReceivedAt); err != nil {
			return result, err
		}
		item.Event, err = workevent.DecodeEvent(raw)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > query.Limit {
		result.Items = result.Items[:query.Limit]
		position.Offset += query.Limit
		raw, _ := json.Marshal(position)
		result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return result, nil
}

type WorkEventRelationView struct {
	Relation          workevent.RelationAssertion `json:"relation"`
	State             string                      `json:"state"`
	BindingState      string                      `json:"binding_state"`
	RuntimeRequestRef string                      `json:"runtime_request_ref,omitempty"`
	ObservedAt        *time.Time                  `json:"observed_at,omitempty"`
}

type WorkEventDetail struct {
	Version            string                  `json:"version"`
	Item               WorkEventItem           `json:"item"`
	Relations          []WorkEventRelationView `json:"relations"`
	ReviewBodies       []WorkEventReviewBody   `json:"review_bodies"`
	RunItems           []WorkEventItem         `json:"run_items"`
	RunItemsMore       bool                    `json:"run_items_more"`
	RunCutoff          *time.Time              `json:"run_cutoff,omitempty"`
	RunItemsNextCursor string                  `json:"run_items_next_cursor,omitempty"`
	RunSummary         *WorkEventRunSummary    `json:"run_summary,omitempty"`
}

type WorkEventRunSummary struct {
	ResourceReadOutcomes      WorkActivityOutcomeCounts `json:"resource_read_outcomes"`
	ResourceChangeOutcomes    WorkActivityOutcomeCounts `json:"resource_change_outcomes"`
	DistinctResources         int64                     `json:"distinct_resources"`
	ModelCalls                int64                     `json:"model_calls"`
	ToolCalls                 int64                     `json:"tool_calls"`
	UnlinkedModelObservations int64                     `json:"unlinked_model_observations"`
	TaskCompleted             int64                     `json:"task_completed"`
	TaskFailed                int64                     `json:"task_failed"`
	TaskUnknown               int64                     `json:"task_unknown"`
}

const workEventRunPageSize = 100

type workEventRunCursor struct {
	Scope           string    `json:"scope"`
	Members         string    `json:"members"`
	Cutoff          time.Time `json:"cutoff"`
	AfterOccurredAt time.Time `json:"after_occurred_at"`
	AfterEventRef   string    `json:"after_event_ref"`
}

func nullableRunAfter(at time.Time) any {
	if at.IsZero() {
		return nil
	}
	return at
}

// V2 classifies only actions with an explicit resource-operation meaning.
// Unknown resource actions remain visible as other observations.
const workActivityClassifyColumns = `
 CASE WHEN l.subject_kind='resource_action' AND l.action_kind IN ('resource.read','resource.read.failed','resource.read.not_found','resource.read.unconfirmed') THEN 'read'
      WHEN l.subject_kind='resource_action' AND l.action_kind IN ('resource.write','resource.write.failed','resource.edit','resource.edit.failed') THEN 'change'
      ELSE '' END AS resource_operation,
 CASE WHEN l.action_kind='resource.read.not_found' AND l.lifecycle_state='failed' THEN 'optional_not_found'
      WHEN l.action_kind LIKE '%.failed' OR l.lifecycle_state='failed' THEN 'failed'
      WHEN l.lifecycle_state='completed' AND l.action_kind IN ('resource.read','resource.write','resource.edit') THEN 'completed'
      ELSE 'unknown' END AS resource_outcome`

type WorkEventReviewBody struct {
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	SHA256  string `json:"sha256"`
	State   string `json:"state"`
	CanRead bool   `json:"can_read"`
}

func (s *WorkspaceStore) GetWorkEvent(ctx context.Context, subject string, scope workevent.Scope, eventRef string) (WorkEventDetail, error) {
	return s.getWorkEventPage(ctx, subject, scope, eventRef, "", time.Now().UTC(), false)
}

func (s *WorkspaceStore) GetWorkEventPage(ctx context.Context, subject string, scope workevent.Scope, eventRef, runCursor string, now time.Time) (WorkEventDetail, error) {
	return s.getWorkEventPage(ctx, subject, scope, eventRef, runCursor, now, true)
}

func (s *WorkspaceStore) getWorkEventPage(ctx context.Context, subject string, scope workevent.Scope, eventRef, runCursor string, now time.Time, reading bool) (WorkEventDetail, error) {
	result := WorkEventDetail{Version: "xinsight.work-activity-detail.v1", Relations: []WorkEventRelationView{}, ReviewBodies: []WorkEventReviewBody{}, RunItems: []WorkEventItem{}}
	if !scope.Valid() || eventRef == "" || len(eventRef) > 128 || now.IsZero() || len(runCursor) > 2048 {
		return result, workbench.ErrInvalid
	}
	now = now.UTC()
	position := workEventRunCursor{Cutoff: now}
	if runCursor != "" {
		raw, decodeErr := base64.RawURLEncoding.DecodeString(runCursor)
		if decodeErr != nil || len(raw) > 1024 {
			return result, workbench.ErrInvalid
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&position) != nil || decoder.Decode(new(any)) != io.EOF || position.Scope == "" ||
			position.Cutoff.IsZero() || position.Cutoff.After(now) ||
			position.AfterOccurredAt.IsZero() || position.AfterEventRef == "" || len(position.AfterEventRef) > 128 ||
			len(position.Members) != 64 {
			return result, workbench.ErrInvalid
		}
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	auth, err := loadWorkbenchAuthorization(ctx, tx, subject)
	if err != nil {
		return result, err
	}
	allowed := false
	for _, grant := range auth.Grants {
		if grant.AllowResource && grant.CostProject == "" && grant.SourceSystemID == scope.SourceSystemID &&
			(grant.SourceProject == "" || grant.SourceProject == "project-"+scope.ProjectID) {
			allowed = true
			break
		}
	}
	if !allowed {
		// Event detail does not disclose whether an inaccessible identity exists.
		return result, workbench.ErrNotFound
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT d.event_bytes,d.event_hash,d.received_at FROM work_event_documents d
 WHERE d.source_system_id=$1::uuid AND d.source_instance=$2 AND d.org_id=$3 AND d.project_id=$4 AND d.event_ref=$5
 AND d.received_at<=$7
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$6 AND g.active AND g.allow_resource AND
 g.source_system_id=d.source_system_id AND g.cost_project_ref IS NULL AND
 (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id))
 ORDER BY d.revision DESC LIMIT 1`, scope.SourceSystemID, scope.SourceInstance, scope.OrgID, scope.ProjectID, eventRef, subject, position.Cutoff).Scan(&raw, &result.Item.EventHash, &result.Item.ReceivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, workbench.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	result.Item.Event, err = workevent.DecodeEvent(raw)
	if err != nil {
		return result, err
	}
	// The source's run ID groups observations in the exact reporter scope. It
	// asserts neither tool-to-model causation nor file mutation success.
	if result.Item.Event.RunID == "" && runCursor != "" {
		return result, workbench.ErrInvalid
	}
	if result.Item.Event.RunID != "" {
		binding, _ := json.Marshal([]any{"xinsight.work-activity-run.v2", subject, auth.ObservationRevision, scope,
			eventRef, result.Item.Event.Revision, result.Item.EventHash, result.Item.Event.RunID, position.Cutoff, workEventRunPageSize})
		expectedScope := workEventHash(binding)
		if runCursor != "" && position.Scope != expectedScope {
			return result, workbench.ErrInvalid
		}
		position.Scope = expectedScope
		if reading {
			// The selected event, aggregate and page share one repeatable-read
			// snapshot. Recheck the exact latest members for every continuation;
			// a backdated ingest may commit after the first page's cutoff.
			var members string
			result.RunCutoff = &position.Cutoff
			result.RunSummary = &WorkEventRunSummary{}
			err = tx.QueryRowContext(ctx, `WITH latest AS (
 SELECT DISTINCT ON (d.event_ref) d.event_ref,d.revision,d.event_hash,d.run_id,d.subject_kind,d.action_kind,d.lifecycle_state,
 d.resource_ref,d.model_call_id,d.tool_call_id
 FROM work_event_documents d
 WHERE d.source_system_id=$1::uuid AND d.source_instance=$2 AND d.org_id=$3 AND d.project_id=$4
 AND d.received_at<=$6
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$7 AND g.active AND g.allow_resource AND
 g.source_system_id=d.source_system_id AND g.cost_project_ref IS NULL AND
 (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id))
 ORDER BY d.event_ref,d.revision DESC
), classified AS (SELECT l.*,`+workActivityClassifyColumns+` FROM latest l)
 SELECT count(*) FILTER(WHERE resource_operation='read' AND resource_outcome='completed'),
 count(*) FILTER(WHERE resource_operation='read' AND resource_outcome='failed'),
 count(*) FILTER(WHERE resource_operation='read' AND resource_outcome='optional_not_found'),
 count(*) FILTER(WHERE resource_operation='read' AND resource_outcome='unknown'),
 count(*) FILTER(WHERE resource_operation='change' AND resource_outcome='completed'),
 count(*) FILTER(WHERE resource_operation='change' AND resource_outcome='failed'),
 count(*) FILTER(WHERE resource_operation='change' AND resource_outcome='optional_not_found'),
 count(*) FILTER(WHERE resource_operation='change' AND resource_outcome='unknown'),
 count(DISTINCT resource_ref) FILTER(WHERE subject_kind='resource_action' AND resource_ref<>''),
 count(DISTINCT model_call_id) FILTER(WHERE subject_kind='model_call' AND model_call_id<>''),
 count(DISTINCT tool_call_id) FILTER(WHERE subject_kind='tool_call' AND tool_call_id<>''),
 count(*) FILTER(WHERE subject_kind='model_call' AND model_call_id='' AND action_kind IN ('model.input_observed','model.output_observed')),
 count(*) FILTER(WHERE subject_kind='task' AND action_kind='task.completed'),
 count(*) FILTER(WHERE subject_kind='task' AND action_kind='task.failed'),
 count(*) FILTER(WHERE subject_kind='task' AND action_kind='task.ended_observed'),
 encode(sha256(convert_to(coalesce(jsonb_agg(jsonb_build_array(event_ref,revision,event_hash) ORDER BY event_ref),'[]'::jsonb)::text,'UTF8')),'hex')
 FROM classified WHERE run_id=$5`, scope.SourceSystemID, scope.SourceInstance, scope.OrgID,
				scope.ProjectID, result.Item.Event.RunID, position.Cutoff, subject).Scan(
				&result.RunSummary.ResourceReadOutcomes.Completed, &result.RunSummary.ResourceReadOutcomes.Failed,
				&result.RunSummary.ResourceReadOutcomes.OptionalNotFound, &result.RunSummary.ResourceReadOutcomes.Unknown,
				&result.RunSummary.ResourceChangeOutcomes.Completed, &result.RunSummary.ResourceChangeOutcomes.Failed,
				&result.RunSummary.ResourceChangeOutcomes.OptionalNotFound, &result.RunSummary.ResourceChangeOutcomes.Unknown,
				&result.RunSummary.DistinctResources, &result.RunSummary.ModelCalls, &result.RunSummary.ToolCalls,
				&result.RunSummary.UnlinkedModelObservations, &result.RunSummary.TaskCompleted,
				&result.RunSummary.TaskFailed, &result.RunSummary.TaskUnknown, &members)
			if err != nil {
				return result, err
			}
			if runCursor != "" && position.Members != members {
				return result, workbench.ErrConflict
			}
			position.Members = members
		}
		runRows, queryErr := tx.QueryContext(ctx, `SELECT event_bytes,event_hash,received_at FROM (
 SELECT DISTINCT ON (event_ref) event_ref,event_bytes,event_hash,received_at,occurred_at,run_id
 FROM work_event_documents WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4
 AND received_at<=$6
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$9 AND g.active AND g.allow_resource AND
 g.source_system_id=work_event_documents.source_system_id AND g.cost_project_ref IS NULL AND
 (g.source_project_ref IS NULL OR g.source_project_ref='project-'||work_event_documents.project_id))
 ORDER BY event_ref,revision DESC
 ) latest WHERE run_id=$5 AND ($7::timestamptz IS NULL OR (occurred_at,event_ref)>($7::timestamptz,$8))
 ORDER BY occurred_at,event_ref LIMIT 101`, scope.SourceSystemID, scope.SourceInstance, scope.OrgID,
			scope.ProjectID, result.Item.Event.RunID, position.Cutoff, nullableRunAfter(position.AfterOccurredAt), position.AfterEventRef, subject)
		if queryErr != nil {
			return result, queryErr
		}
		for runRows.Next() {
			var item WorkEventItem
			var eventRaw []byte
			if err = runRows.Scan(&eventRaw, &item.EventHash, &item.ReceivedAt); err != nil {
				runRows.Close()
				return result, err
			}
			item.Event, err = workevent.DecodeEvent(eventRaw)
			if err != nil {
				runRows.Close()
				return result, err
			}
			if len(result.RunItems) == workEventRunPageSize {
				result.RunItemsMore = true
				break
			}
			result.RunItems = append(result.RunItems, item)
		}
		if err = runRows.Err(); err != nil {
			runRows.Close()
			return result, err
		}
		runRows.Close()
		if result.RunItemsMore && reading {
			last := result.RunItems[len(result.RunItems)-1].Event
			position.AfterOccurredAt, position.AfterEventRef = last.OccurredAt, last.EventRef
			encoded, encodeErr := json.Marshal(position)
			if encodeErr != nil {
				return result, encodeErr
			}
			result.RunItemsNextCursor = base64.RawURLEncoding.EncodeToString(encoded)
		}
	}
	var mayReadBody bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$1 AND
 g.active AND g.allow_content AND g.allowed_content_purposes='["human_review"]'::jsonb AND
 g.source_system_id=$2::uuid AND g.cost_project_ref IS NULL AND
 (g.source_project_ref IS NULL OR g.source_project_ref='project-'||$3))`, subject, scope.SourceSystemID, scope.ProjectID).Scan(&mayReadBody); err != nil {
		return result, err
	}
	for _, ref := range result.Item.Event.ContentRefs {
		view := WorkEventReviewBody{Kind: ref.Kind, Ref: ref.Ref, SHA256: ref.SHA256, State: "source_reference_only"}
		if !mayReadBody {
			view.State = "content_not_authorized"
		} else {
			var storedState string
			var deadline time.Time
			var objectPresent bool
			err = tx.QueryRowContext(ctx, `SELECT o.state,o.expires_at,
 EXISTS(SELECT 1 FROM review_body_objects b WHERE b.content_ref=o.content_ref)
 FROM review_body_operations o WHERE o.content_ref=$1 AND o.source_kind='work_event' AND
 o.source_system_id=$2::uuid AND o.source_instance=$3 AND o.org_id=$4 AND o.project_id=$5 AND
 o.event_ref=$6 AND o.event_revision=$7 AND o.event_hash=$8 AND o.content_kind=$9 AND o.digest=$10`,
				ref.Ref, scope.SourceSystemID, scope.SourceInstance, scope.OrgID, scope.ProjectID,
				eventRef, result.Item.Event.Revision, result.Item.EventHash, ref.Kind, ref.SHA256).Scan(&storedState, &deadline, &objectPresent)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return result, err
			}
			if err == nil {
				switch {
				case storedState == "accepted" && !time.Now().Before(deadline):
					view.State = "expired"
				case storedState == "accepted" && objectPresent:
					view.State, view.CanRead = "available", true
				case storedState == "accepted":
					view.State = "copy_missing"
				case storedState == "reserved":
					view.State = "pending"
				default:
					view.State = storedState
				}
			}
		}
		result.ReviewBodies = append(result.ReviewBodies, view)
	}
	rows, err := tx.QueryContext(ctx, `SELECT r.relation_bytes,r.event_revision,r.event_hash,
 (SELECT min(d.occurred_at) FROM source_observation_documents d WHERE d.source_system_id=r.target_source_system_id AND d.source_instance=r.target_source_instance AND d.org_id=r.target_org_id AND d.project_id=r.target_project_id AND d.request_id=r.target_request_id),
 'xrr_'||encode(sha256(convert_to(jsonb_build_array(r.target_source_system_id::text,r.target_source_instance,r.target_org_id,r.target_project_id,r.target_request_id)::text,'UTF8')),'hex')
 FROM work_event_relations r WHERE r.reporter_source_system_id=$1::uuid AND r.reporter_source_instance=$2 AND r.reporter_org_id=$3 AND r.reporter_project_id=$4 AND r.event_ref=$5
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$6 AND g.active AND g.allow_source_runtime AND
 g.source_system_id=r.target_source_system_id AND g.cost_project_ref IS NULL AND
 (g.source_project_ref IS NULL OR g.source_project_ref='project-'||r.target_project_id))
 ORDER BY r.recorded_at,r.relation_ref LIMIT 50`, scope.SourceSystemID, scope.SourceInstance, scope.OrgID, scope.ProjectID, eventRef, subject)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var relationRaw []byte
		var matched bool
		var boundRevision int64
		var boundHash string
		var observedAt sql.NullTime
		var runtimeRef string
		var view WorkEventRelationView
		if err = rows.Scan(&relationRaw, &boundRevision, &boundHash, &observedAt, &runtimeRef); err != nil {
			return result, err
		}
		view.Relation, err = workevent.DecodeRelation(relationRaw)
		if err != nil {
			return result, err
		}
		view.State = "source_asserted_unverified"
		view.BindingState = "prior_revision"
		if boundRevision == result.Item.Event.Revision && boundHash == result.Item.EventHash {
			view.BindingState = "current_revision"
		}
		matched = observedAt.Valid
		if matched {
			view.State = "ur_identifier_matched"
			view.RuntimeRequestRef = runtimeRef
			view.ObservedAt = &observedAt.Time
		}
		result.Relations = append(result.Relations, view)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

// WorkActivitySummary counts source-reported actions, not verified outcomes.
// It is independent of economic/runtime reading availability.
type WorkActivitySummary struct {
	Version                 string                    `json:"version"`
	From                    time.Time                 `json:"from"`
	To                      time.Time                 `json:"to"`
	Cutoff                  time.Time                 `json:"cutoff"`
	ReportedActions         int64                     `json:"reported_actions"`
	ReportedResourceReads   int64                     `json:"reported_resource_reads"`
	ReportedResourceChanges int64                     `json:"reported_resource_changes"`
	LegacyResourceReads     int64                     `json:"-"`
	LegacyResourceChanges   int64                     `json:"-"`
	ResourceReadOutcomes    WorkActivityOutcomeCounts `json:"resource_read_outcomes"`
	ResourceChangeOutcomes  WorkActivityOutcomeCounts `json:"resource_change_outcomes"`
	ReportedModelEvents     int64                     `json:"reported_model_events"`
	ReportedLinkedEvents    int64                     `json:"reported_linked_events"`
	ReportedUnlinkedEvents  int64                     `json:"reported_unlinked_events"`
	SourcesNeedingAttention int64                     `json:"sources_needing_attention"`
	Coverage                []string                  `json:"coverage"`
}

// These are source-reported operation results, not independent file or body checks.
type WorkActivityOutcomeCounts struct {
	Completed        int64 `json:"completed"`
	Failed           int64 `json:"failed"`
	OptionalNotFound int64 `json:"optional_not_found"`
	Unknown          int64 `json:"unknown"`
}

type WorkActivitySummaryQuery struct {
	From       time.Time
	To         time.Time
	SourceIDs  []string
	ProjectIDs []string
}

func (s *WorkspaceStore) GetWorkActivitySummary(ctx context.Context, subject string, query WorkActivitySummaryQuery, now time.Time) (WorkActivitySummary, error) {
	result := WorkActivitySummary{Version: "xinsight.work-activity-summary.v1", From: query.From.UTC(), To: query.To.UTC(),
		Cutoff: now.UTC(), Coverage: []string{"source_period_completeness_not_asserted"}}
	if query.SourceIDs == nil {
		query.SourceIDs = []string{}
	}
	if query.ProjectIDs == nil {
		query.ProjectIDs = []string{}
	}
	if !query.From.Before(query.To) || query.To.After(now.Add(time.Second)) || query.To.Sub(query.From) > 31*24*time.Hour ||
		len(query.SourceIDs) > 16 || len(query.ProjectIDs) > 16 {
		return result, workbench.ErrInvalid
	}
	for _, source := range query.SourceIDs {
		if !(workevent.Scope{SourceSystemID: source, SourceInstance: "x", OrgID: "1", ProjectID: "1"}).Valid() {
			return result, workbench.ErrInvalid
		}
	}
	for _, project := range query.ProjectIDs {
		if !(workevent.Scope{SourceSystemID: "00000000-0000-0000-0000-000000000000", SourceInstance: "x", OrgID: "1", ProjectID: project}).Valid() {
			return result, workbench.ErrInvalid
		}
	}
	auth, err := loadWorkbenchAuthorization(ctx, s.db, subject)
	if err != nil {
		return result, err
	}
	allowed := false
	for _, grant := range auth.Grants {
		if grant.AllowResource && grant.CostProject == "" {
			allowed = true
			break
		}
	}
	if !allowed {
		return result, workbench.ErrScopeForbidden
	}
	// Select the latest accepted revision per identity before counting. The
	// classification is shared by both resource operation aggregates; a
	// relation is a source assertion, not a verified tool/file result.
	err = s.db.QueryRowContext(ctx, `WITH latest AS (
 SELECT DISTINCT ON (d.source_system_id,d.source_instance,d.org_id,d.project_id,d.event_ref)
 d.source_system_id,d.source_instance,d.org_id,d.project_id,d.event_ref,d.revision,d.event_hash,
 d.subject_kind,d.action_kind,d.lifecycle_state,d.occurred_at
 FROM work_event_documents d
 WHERE d.received_at<=$1
 AND (cardinality($5::text[])=0 OR d.source_system_id::text=ANY($5::text[]))
 AND (cardinality($6::text[])=0 OR d.project_id=ANY($6::text[]))
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$4 AND g.active AND g.allow_resource
   AND g.source_system_id=d.source_system_id AND g.cost_project_ref IS NULL
   AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id))
 ORDER BY d.source_system_id,d.source_instance,d.org_id,d.project_id,d.event_ref,d.revision DESC
), classified AS (
 SELECT l.*,`+workActivityClassifyColumns+` FROM latest l
), counted AS (
 SELECT l.*, EXISTS(SELECT 1 FROM work_event_relations r WHERE
   r.reporter_source_system_id=l.source_system_id AND r.reporter_source_instance=l.source_instance AND
   r.reporter_org_id=l.org_id AND r.reporter_project_id=l.project_id AND r.event_ref=l.event_ref AND
   r.event_revision=l.revision AND r.event_hash=l.event_hash) AS linked FROM classified l
 WHERE l.occurred_at>=$2 AND l.occurred_at<$3
)
 SELECT count(*),count(*) FILTER(WHERE resource_operation='read'),
 count(*) FILTER(WHERE resource_operation='change'),
 count(*) FILTER(WHERE subject_kind='resource_action' AND action_kind LIKE 'resource.read%'),
 count(*) FILTER(WHERE subject_kind='resource_action' AND (action_kind LIKE 'resource.write%' OR action_kind LIKE 'resource.edit%')),
 count(*) FILTER(WHERE subject_kind='model_call'),
 count(*) FILTER(WHERE subject_kind='model_call' AND linked),
 count(*) FILTER(WHERE subject_kind='model_call' AND NOT linked),
 count(*) FILTER(WHERE resource_operation='read' AND resource_outcome='completed'),
 count(*) FILTER(WHERE resource_operation='read' AND resource_outcome='failed'),
 count(*) FILTER(WHERE resource_operation='read' AND resource_outcome='optional_not_found'),
 count(*) FILTER(WHERE resource_operation='read' AND resource_outcome='unknown'),
 count(*) FILTER(WHERE resource_operation='change' AND resource_outcome='completed'),
 count(*) FILTER(WHERE resource_operation='change' AND resource_outcome='failed'),
 count(*) FILTER(WHERE resource_operation='change' AND resource_outcome='optional_not_found'),
 count(*) FILTER(WHERE resource_operation='change' AND resource_outcome='unknown')
 FROM counted`, now.UTC(), query.From.UTC(), query.To.UTC(), subject, query.SourceIDs, query.ProjectIDs).Scan(
		&result.ReportedActions, &result.ReportedResourceReads, &result.ReportedResourceChanges,
		&result.LegacyResourceReads, &result.LegacyResourceChanges,
		&result.ReportedModelEvents, &result.ReportedLinkedEvents, &result.ReportedUnlinkedEvents,
		&result.ResourceReadOutcomes.Completed, &result.ResourceReadOutcomes.Failed,
		&result.ResourceReadOutcomes.OptionalNotFound, &result.ResourceReadOutcomes.Unknown,
		&result.ResourceChangeOutcomes.Completed, &result.ResourceChangeOutcomes.Failed,
		&result.ResourceChangeOutcomes.OptionalNotFound, &result.ResourceChangeOutcomes.Unknown)
	if err != nil {
		return result, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM work_event_collections c WHERE c.requires_attention AND
 (cardinality($2::text[])=0 OR c.source_system_id::text=ANY($2::text[])) AND
 (cardinality($3::text[])=0 OR c.project_id=ANY($3::text[])) AND
 EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$1 AND g.active AND g.allow_resource
   AND g.source_system_id=c.source_system_id AND g.cost_project_ref IS NULL AND
   (g.source_project_ref IS NULL OR g.source_project_ref='project-'||c.project_id))`, subject, query.SourceIDs, query.ProjectIDs).Scan(&result.SourcesNeedingAttention)
	if err != nil {
		return result, err
	}
	if result.SourcesNeedingAttention > 0 {
		result.Coverage = append(result.Coverage, "source_attention_present")
	}
	return result, nil
}
