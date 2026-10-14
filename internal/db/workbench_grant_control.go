package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/workspace"
)

// Projects must be explicit: "*" means all projects; nil never means all.
// Capabilities replace the complete set. This does not change the user's role.
type WorkbenchGrantChange struct {
	SchemaVersion          string   `json:"schema_version"`
	Action                 string   `json:"action"`
	ID                     string   `json:"grant_id"`
	Subject                string   `json:"subject_id"`
	Source                 string   `json:"source_system_id"`
	SourceProject          *string  `json:"source_project_ref"`
	CostProject            *string  `json:"cost_project_ref"`
	ExpectedRevision       *int64   `json:"expected_revision"`
	Capabilities           []string `json:"capabilities"`
	AllowContent           *bool    `json:"allow_content,omitempty"`
	AllowedContentPurposes []string `json:"allowed_content_purposes,omitempty"`
}

// Preserve v1 encoding while retaining the explicit empty v2 purpose array.
func (r WorkbenchGrantChange) MarshalJSON() ([]byte, error) {
	type wire WorkbenchGrantChange
	if r.SchemaVersion != "xinsight.workbench-grant-change.v2" {
		return json.Marshal(wire(r))
	}
	return json.Marshal(struct {
		wire
		Purposes []string `json:"allowed_content_purposes"`
	}{wire: wire(r), Purposes: r.AllowedContentPurposes})
}

var grantUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var grantSubject = regexp.MustCompile(`^xs_[a-z0-9]{16,64}$`)
var workbenchGrantCapabilities = []string{"summary", "details", "coverage", "preferences", "source_context", "source_identity", "source_cost", "purpose_read", "purpose_review", "source_runtime", "source_attempt", "source_capability", "source_policy", "source_destination", "resource", "budget_definition", "budget_summary", "budget_alert"}

func (r WorkbenchGrantChange) Validate() error {
	if (r.SchemaVersion != "xinsight.workbench-grant-change.v1" && r.SchemaVersion != "xinsight.workbench-grant-change.v2") || !grantUUID.MatchString(r.ID) || !grantUUID.MatchString(r.Source) || !grantSubject.MatchString(r.Subject) || r.ExpectedRevision == nil || *r.ExpectedRevision < 0 || *r.ExpectedRevision == int64(^uint64(0)>>1) {
		return fmt.Errorf("invalid workbench grant identity or revision")
	}
	if r.Action != "create" && r.Action != "replace" && r.Action != "revoke" {
		return fmt.Errorf("invalid workbench grant action")
	}
	if (r.Action == "create") != (*r.ExpectedRevision == 0) {
		return fmt.Errorf("create requires revision zero; replace/revoke require a current positive revision")
	}
	for _, p := range []*string{r.SourceProject, r.CostProject} {
		if p == nil || *p == "" || len(*p) > 256 || strings.TrimSpace(*p) != *p || strings.ContainsAny(*p, "\r\n\x00") {
			return fmt.Errorf("project scope must be explicit; use * only for deliberately granting all projects")
		}
	}
	if r.Capabilities == nil {
		return fmt.Errorf("explicit capabilities required (empty for revoke)")
	}
	if r.SchemaVersion == "xinsight.workbench-grant-change.v2" {
		if r.AllowContent == nil || r.AllowedContentPurposes == nil {
			return fmt.Errorf("v2 requires explicit allow_content and allowed_content_purposes")
		}
		if (*r.AllowContent && (len(r.AllowedContentPurposes) != 1 || r.AllowedContentPurposes[0] != "human_review")) || (!*r.AllowContent && len(r.AllowedContentPurposes) != 0) {
			return fmt.Errorf("content requires true/[human_review] or false/[]")
		}
	} else if r.AllowContent != nil || r.AllowedContentPurposes != nil {
		return fmt.Errorf("content fields require grant command v2")
	}
	if r.Action == "revoke" {
		if r.AllowContent != nil && *r.AllowContent {
			return fmt.Errorf("full revoke requires content false/[]")
		}
		if len(r.Capabilities) != 0 {
			return fmt.Errorf("revoke requires an empty capabilities list")
		}
		return nil
	}
	known := map[string]bool{}
	for _, c := range workbenchGrantCapabilities {
		known[c] = true
	}
	seen := map[string]bool{}
	primary := false
	for _, c := range r.Capabilities {
		if !known[c] || seen[c] {
			return fmt.Errorf("unknown or duplicate workbench capability")
		}
		if strings.HasPrefix(c, "budget_") && *r.CostProject != "*" {
			return fmt.Errorf("source budget capability requires an explicit all-cost-project scope; restrict its source project separately")
		}
		seen[c] = true
		switch c {
		case "source_context", "source_identity", "source_cost", "purpose_read", "purpose_review":
		default:
			primary = true
		}
	}
	if !primary && (r.AllowContent == nil || !*r.AllowContent) {
		return fmt.Errorf("at least one summary, detail, coverage, preference, runtime, resource or budget capability is required")
	}
	return nil
}

type WorkbenchGrantResult struct {
	ID        string          `json:"grant_id"`
	Revision  int64           `json:"revision"`
	CheckOnly bool            `json:"check_only"`
	Changed   bool            `json:"changed"`
	Before    json.RawMessage `json:"before"`
	After     json.RawMessage `json:"after"`
}

// Keep admission and content-domain writes on the same explicit compatibility
// list; a new release schema must be reviewed rather than accepted by a range.
func workbenchGrantSchemaSupported(schema string, content bool) bool {
	switch schema {
	case "000036_add_source_budgets", "000037_add_synthetic_resource_authorization", "000038_add_case_resource_links":
		return !content
	case "000039_add_content_readings", "000040_add_resource_action_profile", "000041_add_work_events", "000042_add_review_body_custody", "000043_add_work_event_install_invites", "000044_add_managed_connector_adoption", "000045_add_connector_credential_lifecycle":
		return true
	default:
		return false
	}
}

// A host-admin operation; reader/workspace/importer roles cannot write this
// table. The existing table -> advisory lock order also fences in-flight
// scoped reads and review commits. No new ACL, schema or lock is introduced.
func (s *SourceFactsStore) ChangeWorkbenchGrant(ctx context.Context, r WorkbenchGrantChange, check bool) (WorkbenchGrantResult, error) {
	var result WorkbenchGrantResult
	if err := r.Validate(); err != nil {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: check})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var schema string
	var dirty bool
	if err = tx.QueryRowContext(ctx, `SELECT max(version),bool_or(dirty) FROM schema_migrations`).Scan(&schema, &dirty); err != nil {
		return result, err
	}
	if !workbenchGrantSchemaSupported(schema, false) || dirty {
		return result, fmt.Errorf("workbench grant control requires clean schema36 through schema45")
	}
	if r.SchemaVersion == "xinsight.workbench-grant-change.v2" && !workbenchGrantSchemaSupported(schema, true) {
		return result, fmt.Errorf("content grant command v2 requires clean schema39 through schema45")
	}
	mode, lock := "ROW EXCLUSIVE", "pg_advisory_xact_lock"
	if check {
		mode, lock = "ACCESS SHARE", "pg_advisory_xact_lock_shared"
	}
	if _, err = tx.ExecContext(ctx, "LOCK TABLE workbench_access_grants IN "+mode+" MODE"); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, "SELECT "+lock+"($1)", WorkchainGrantLock); err != nil {
		return result, err
	}
	var before []byte
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT to_jsonb(g),revision FROM workbench_access_grants g WHERE id=$1::uuid`, r.ID).Scan(&before, &revision)
	if r.Action == "create" {
		if err != sql.ErrNoRows {
			if err == nil {
				err = fmt.Errorf("grant already exists")
			}
			return result, err
		}
	} else {
		if err != nil {
			return result, err
		}
		if revision != *r.ExpectedRevision {
			return result, fmt.Errorf("workbench grant revision changed")
		}
	}
	legacyChanged, contentChanged, err := grantRevisionChanges(r, before)
	if err != nil {
		return result, err
	}
	var sourceExists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_systems WHERE id=$1::uuid)`, r.Source).Scan(&sourceExists); err != nil {
		return result, err
	}
	if !sourceExists {
		return result, fmt.Errorf("source system is not registered")
	}
	if r.Action == "revoke" {
		var prior struct {
			Subject       string  `json:"subject_id"`
			Source        string  `json:"source_system_id"`
			SourceProject *string `json:"source_project_ref"`
			CostProject   *string `json:"cost_project_ref"`
		}
		if err = json.Unmarshal(before, &prior); err != nil {
			return result, err
		}
		scope := func(p *string) string {
			if p == nil {
				return "*"
			}
			return *p
		}
		if prior.Subject != r.Subject || prior.Source != r.Source || scope(prior.SourceProject) != *r.SourceProject || scope(prior.CostProject) != *r.CostProject {
			return result, fmt.Errorf("revoke must name the current subject/source/project scope")
		}
	} else {
		// Match NULLS NOT DISTINCT scope uniqueness in a truly read-only check.
		var duplicate bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workbench_access_grants WHERE id<>$1::uuid AND subject_id=$2 AND source_system_id=$3::uuid AND source_project_ref IS NOT DISTINCT FROM NULLIF($4,'*') AND cost_project_ref IS NOT DISTINCT FROM NULLIF($5,'*'))`, r.ID, r.Subject, r.Source, *r.SourceProject, *r.CostProject).Scan(&duplicate)
		if err != nil {
			return result, err
		}
		if duplicate {
			return result, fmt.Errorf("another grant already owns this exact scope")
		}
	}
	if before == nil {
		before = []byte("null")
	}
	result = WorkbenchGrantResult{ID: r.ID, Revision: revision, CheckOnly: check, Before: before, After: json.RawMessage("null")}
	if check {
		return result, tx.Commit()
	}
	columns, values, updates := []string{}, []string{}, []string{}
	args := []any{r.ID, r.Subject, r.Source, *r.SourceProject, *r.CostProject}
	granted := map[string]bool{}
	for _, c := range r.Capabilities {
		granted[c] = true
	}
	for i, c := range workbenchGrantCapabilities {
		col := "allow_" + c
		columns = append(columns, col)
		v := fmt.Sprintf("$%d", i+6)
		values = append(values, v)
		updates = append(updates, col+"="+v)
		args = append(args, granted[c])
	}
	domainUpdates := ""
	if workbenchGrantSchemaSupported(schema, true) {
		if r.Action != "revoke" {
			content := r.AllowContent != nil && *r.AllowContent
			purposes := "[]"
			if content {
				purposes = `["human_review"]`
			}
			for _, field := range []struct {
				name  string
				value any
			}{{"allow_content", content}, {"allowed_content_purposes", purposes}} {
				args = append(args, field.value)
				v := fmt.Sprintf("$%d", len(args))
				if field.name == "allowed_content_purposes" {
					v += "::jsonb"
				}
				columns = append(columns, field.name)
				values = append(values, v)
				updates = append(updates, field.name+"="+v)
			}
			if r.Action == "create" {
				columns = append(columns, "legacy_revision", "content_revision")
				c := "0"
				if content {
					c = "1"
				}
				values = append(values, "1", c)
			}
		}
		if legacyChanged {
			domainUpdates += ",legacy_revision=legacy_revision+1"
		}
		if contentChanged {
			domainUpdates += ",content_revision=content_revision+1"
		}
	}
	switch r.Action {
	case "create":
		err = tx.QueryRowContext(ctx, `INSERT INTO workbench_access_grants (id,subject_id,source_system_id,source_project_ref,cost_project_ref,`+strings.Join(columns, ",")+`) VALUES ($1::uuid,$2,$3::uuid,NULLIF($4,'*'),NULLIF($5,'*'),`+strings.Join(values, ",")+`) RETURNING revision`, args...).Scan(&result.Revision)
	case "replace":
		args = append(args, *r.ExpectedRevision)
		err = tx.QueryRowContext(ctx, `UPDATE workbench_access_grants SET subject_id=$2,source_system_id=$3::uuid,source_project_ref=NULLIF($4,'*'),cost_project_ref=NULLIF($5,'*'),`+strings.Join(updates, ",")+`,active=true,revision=revision+1`+domainUpdates+`,updated_at=clock_timestamp() WHERE id=$1::uuid AND revision=$`+fmt.Sprint(len(args))+` RETURNING revision`, args...).Scan(&result.Revision)
	case "revoke":
		err = tx.QueryRowContext(ctx, `UPDATE workbench_access_grants SET active=false,revision=revision+1`+domainUpdates+`,updated_at=clock_timestamp() WHERE id=$1::uuid AND revision=$2 RETURNING revision`, r.ID, *r.ExpectedRevision).Scan(&result.Revision)
	}
	if err != nil {
		return result, err
	}
	var after []byte
	if err = tx.QueryRowContext(ctx, `SELECT to_jsonb(g) FROM workbench_access_grants g WHERE id=$1::uuid`, r.ID).Scan(&after); err != nil {
		return result, err
	}
	result.After = after
	details, err := json.Marshal(map[string]any{"before": json.RawMessage(before), "after": json.RawMessage(after)})
	if err != nil {
		return result, err
	}
	id, err := workspace.NewID("xaa_", rand.Reader)
	if err != nil {
		return result, err
	}
	outcome := "accepted"
	if r.Action == "revoke" {
		outcome = "revoked"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO access_audit_records (id,occurred_at,actor_kind,actor_ref,action,resource_kind,resource_ref,result,correlation_id,details) VALUES ($1,$2,'root','root',$3,'workbench_grant',$4,$5,$6,$7::jsonb)`, id, time.Now().UTC(), "workbench.grant."+r.Action, r.ID, outcome, "xa_"+strings.TrimPrefix(id, "xaa_"), string(details))
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	result.Changed = true
	return result, nil
}

func (s *SourceFactsStore) InspectWorkbenchGrant(ctx context.Context, id string) (json.RawMessage, error) {
	if !grantUUID.MatchString(id) {
		return nil, fmt.Errorf("invalid grant id")
	}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT to_jsonb(g) FROM workbench_access_grants g WHERE id=$1::uuid`, id).Scan(&raw)
	return json.RawMessage(raw), err
}

// grantRevisionChanges is shared by mutation and --check before either can write.
// The command revision still fences the entire row; domain revisions fence readers.
func grantRevisionChanges(r WorkbenchGrantChange, before []byte) (legacy, content bool, err error) {
	if len(before) == 0 || string(before) == "null" {
		return true, r.AllowContent != nil && *r.AllowContent, nil
	}
	var prior struct {
		Subject       string   `json:"subject_id"`
		Source        string   `json:"source_system_id"`
		SourceProject *string  `json:"source_project_ref"`
		CostProject   *string  `json:"cost_project_ref"`
		Active        bool     `json:"active"`
		AllowContent  bool     `json:"allow_content"`
		Purposes      []string `json:"allowed_content_purposes"`
	}
	if err = json.Unmarshal(before, &prior); err != nil {
		return
	}
	if r.Action == "replace" && r.SchemaVersion == "xinsight.workbench-grant-change.v1" && prior.AllowContent {
		return false, false, fmt.Errorf("v1 replace cannot replace a grant with content; use v2 with explicit complete state")
	}
	if r.Action == "revoke" {
		return true, true, nil
	}
	scope := func(p *string) string {
		if p == nil {
			return "*"
		}
		return *p
	}
	if prior.Subject != r.Subject || prior.Source != r.Source || scope(prior.SourceProject) != *r.SourceProject || scope(prior.CostProject) != *r.CostProject || !prior.Active {
		return true, true, nil
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(before, &fields); err != nil {
		return
	}
	granted := map[string]bool{}
	for _, c := range r.Capabilities {
		granted[c] = true
	}
	for _, c := range workbenchGrantCapabilities {
		var old bool
		if raw, ok := fields["allow_"+c]; ok {
			if err = json.Unmarshal(raw, &old); err != nil {
				return
			}
		}
		legacy = legacy || old != granted[c]
	}
	allow := r.AllowContent != nil && *r.AllowContent
	content = prior.AllowContent != allow || len(prior.Purposes) != len(r.AllowedContentPurposes)
	if !content {
		for i := range prior.Purposes {
			content = content || prior.Purposes[i] != r.AllowedContentPurposes[i]
		}
	}
	return
}
