package db

import (
	"context"
	"encoding/json"
	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

// One query per read/queue batch. Check the newest record's own target scope;
// never disclose its revision, hash or withdrawal metadata without both grants.
func loadResourceUpdates(ctx context.Context, q workspaceQueryer, refs []workspace.ResourceChoice) (map[string]workspace.ResourceUpdate, error) {
	out := map[string]workspace.ResourceUpdate{}
	if len(refs) == 0 {
		return out, nil
	}
	unique := []workspace.ResourceChoice{}
	seen := map[string]bool{}
	for _, r := range refs {
		k := workspace.ResourceChoiceKey(r)
		if !seen[k] {
			unique = append(unique, r)
			seen[k] = true
		}
	}
	raw, err := json.Marshal(unique)
	if err != nil {
		return nil, err
	}
	subject := ""
	if a := workchain.FromContext(ctx); a != nil {
		subject = a.Subject
	}
	rows, err := q.QueryContext(ctx, `SELECT ref,d.revision,d.record_hash,(d.record_json->>'withdrawn')::boolean,COALESCE((`+resourceRecordAccessSQL("$2")+`),false)
 FROM jsonb_array_elements($1::jsonb) ref CROSS JOIN LATERAL (
 SELECT d.source_system_id,d.project_id,d.target_source_system_id,d.target_source_instance,d.target_org_id,d.target_project_id,d.request_id,d.revision,d.record_hash,d.record_json FROM resource_access_records d WHERE d.source_system_id::text=ref#>>'{reporter,source_system_id}' AND d.source_instance=ref#>>'{reporter,source_instance}' AND d.org_id=ref#>>'{reporter,org_id}' AND d.project_id=ref#>>'{reporter,project_id}' AND d.record_ref=ref->>'record_ref' ORDER BY d.revision DESC LIMIT 1
 ) d WHERE d.revision>(ref->>'revision')::bigint`, raw, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r workspace.ResourceChoice
		var encoded []byte
		var revision int64
		var hash string
		var withdrawn, allowed bool
		if err = rows.Scan(&encoded, &revision, &hash, &withdrawn, &allowed); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(encoded, &r); err != nil {
			return nil, err
		}
		u := workspace.ResourceUpdate{Reference: r, State: "unavailable"}
		if allowed {
			u.State = "revised"
			if withdrawn {
				u.State = "withdrawn"
			}
			u.LatestRevision = revision
			u.LatestHash = hash
		}
		out[workspace.ResourceChoiceKey(r)] = u
	}
	return out, rows.Err()
}
