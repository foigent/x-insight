package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/foigent/x-insight/internal/reviewbody"
)

type ReviewCaptureExceptionState struct {
	ReviewCaptureException
	DesiredRevision   int64  `json:"desired_revision"`
	EffectiveRevision int64  `json:"effective_revision"`
	EffectiveMode     string `json:"effective_mode,omitempty"`
}

type VerifiedReviewAccount struct {
	SourceSystemID string `json:"source_system_id"`
	SourceInstance string `json:"source_instance"`
	OrgID          string `json:"org_id"`
	ProjectID      string `json:"project_id"`
	Authority      string `json:"authority"`
	AccountID      string `json:"account_id"`
}

type ReviewCaptureExceptionCatalog struct {
	Items             []ReviewCaptureExceptionState `json:"items"`
	Accounts          []VerifiedReviewAccount       `json:"verified_accounts"`
	ItemsTruncated    bool                          `json:"items_truncated"`
	AccountsTruncated bool                          `json:"accounts_truncated"`
}

func (s *WorkspaceStore) ListReviewCaptureExceptions(ctx context.Context) (ReviewCaptureExceptionCatalog, error) {
	result := ReviewCaptureExceptionCatalog{Items: []ReviewCaptureExceptionState{}, Accounts: []VerifiedReviewAccount{}}
	if s == nil || s.db == nil {
		return result, reviewbody.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT policy_ref,selector_kind,COALESCE(source_system_id::text,''),
 source_instance,org_id,project_id,account_authority,account_id,content_kind,desired_mode,
 desired_retention_seconds,desired_max_item_bytes,provenance,desired_revision,effective_revision,
 COALESCE(effective_mode,'') FROM review_capture_exceptions ORDER BY policy_ref LIMIT 257`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ReviewCaptureExceptionState
		if err = rows.Scan(&item.PolicyRef, &item.SelectorKind, &item.SourceSystemID,
			&item.SourceInstance, &item.OrgID, &item.ProjectID, &item.AccountAuthority,
			&item.AccountID, &item.ContentKind, &item.Mode, &item.RetentionSeconds,
			&item.MaxItemBytes, &item.Provenance, &item.DesiredRevision,
			&item.EffectiveRevision, &item.EffectiveMode); err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	if len(result.Items) > 256 {
		result.ItemsTruncated = true
		result.Items = result.Items[:256]
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	accounts, err := s.db.QueryContext(ctx, verifiedReviewAccountsSQL+` ORDER BY source_system_id,source_instance,org_id,project_id,owner_id LIMIT 257`)
	if err != nil {
		return result, err
	}
	defer accounts.Close()
	for accounts.Next() {
		var item VerifiedReviewAccount
		if err = accounts.Scan(&item.SourceSystemID, &item.SourceInstance, &item.OrgID,
			&item.ProjectID, &item.AccountID); err != nil {
			return result, err
		}
		item.Authority = "ur_key_owner"
		result.Accounts = append(result.Accounts, item)
	}
	if len(result.Accounts) > 256 {
		result.AccountsTruncated = true
		result.Accounts = result.Accounts[:256]
	}
	return result, accounts.Err()
}

// The account is the source-authenticated owner of a personal API key for an
// exact request, not the bill-to party or a claim about the human actor.
const verifiedReviewAccountsSQL = `WITH latest AS (
 SELECT d.source_system_id,d.source_instance,d.org_id,d.project_id,d.request_id,
        d.profile,d.source_json
 FROM source_fact_documents d
 WHERE d.stage='authenticated' AND d.source_json->>'capture_status'='available'
 AND d.profile IN ('unirouter.execution-context.v1','unirouter.execution-identity.v1')
 AND NOT EXISTS(SELECT 1 FROM source_fact_documents newer WHERE newer.source_system_id=d.source_system_id
   AND newer.source_fact_id=d.source_fact_id AND newer.revision>d.revision)
), verified AS (
 SELECT source_system_id,source_instance,org_id,project_id,request_id,
        count(*) FILTER (WHERE profile='unirouter.execution-context.v1') AS contexts,
        count(*) FILTER (WHERE profile='unirouter.execution-identity.v1') AS identities,
        max(source_json->'core'->>'key_type') FILTER (WHERE profile='unirouter.execution-context.v1'
          AND source_json->'core'->>'auth_kind'='api_key') AS key_type,
        max(source_json->'identity'->>'owner_user_id') FILTER (WHERE profile='unirouter.execution-identity.v1') AS owner_id
 FROM latest GROUP BY source_system_id,source_instance,org_id,project_id,request_id
)
SELECT DISTINCT v.source_system_id::text,v.source_instance,v.org_id,v.project_id,v.owner_id
FROM verified v JOIN source_systems s ON s.id=v.source_system_id AND s.source_type='unirouter' AND s.status='active'
WHERE v.contexts=1 AND v.identities=1 AND v.key_type='personal' AND v.owner_id ~ '^[1-9][0-9]{0,18}$'
AND EXISTS(SELECT 1 FROM source_content_collections c WHERE c.source_system_id=v.source_system_id
  AND c.source_instance=v.source_instance AND c.org_id=v.org_id AND c.project_id=v.project_id AND c.status='active')
`

func verifiedReviewExceptionSelectorTx(ctx context.Context, tx *sql.Tx, p ReviewCaptureException) error {
	if p.SelectorKind == "tenant_category" {
		return nil
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM source_systems s WHERE s.id=$1::uuid AND s.status='active' AND (
   EXISTS(SELECT 1 FROM source_content_collections c WHERE c.source_system_id=s.id AND
      c.source_instance=$2 AND c.org_id=$3 AND c.project_id=$4 AND c.status='active') OR
   EXISTS(SELECT 1 FROM work_event_collections c WHERE c.source_system_id=s.id AND
      c.source_instance=$2 AND c.org_id=$3 AND c.project_id=$4 AND c.status='active')))`,
		p.SourceSystemID, p.SourceInstance, p.OrgID, p.ProjectID).Scan(&active); err != nil {
		return err
	}
	if !active {
		return reviewbody.ErrScope
	}
	if p.SelectorKind != "account" && p.SelectorKind != "account_category" {
		return nil
	}
	if p.AccountAuthority != "ur_key_owner" || !urAccountID.MatchString(p.AccountID) {
		return reviewbody.ErrScope
	}
	var verified bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM (`+verifiedReviewAccountsSQL+`) accounts
WHERE source_system_id=$1 AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND owner_id=$5)`,
		p.SourceSystemID, p.SourceInstance, p.OrgID, p.ProjectID, p.AccountID).Scan(&verified); err != nil {
		return err
	}
	if !verified {
		return reviewbody.ErrScope
	}
	return nil
}

func (s *WorkspaceStore) ReadReviewCaptureException(ctx context.Context, ref string) (ReviewCaptureExceptionState, error) {
	var result ReviewCaptureExceptionState
	if s == nil || s.db == nil || !reviewExceptionRef.MatchString(ref) {
		return result, reviewbody.ErrInvalid
	}
	err := s.db.QueryRowContext(ctx, `SELECT policy_ref,selector_kind,COALESCE(source_system_id::text,''),
source_instance,org_id,project_id,account_authority,account_id,content_kind,desired_mode,
desired_retention_seconds,desired_max_item_bytes,provenance,desired_revision,effective_revision,
COALESCE(effective_mode,'') FROM review_capture_exceptions WHERE policy_ref=$1`, ref).Scan(
		&result.PolicyRef, &result.SelectorKind, &result.SourceSystemID, &result.SourceInstance,
		&result.OrgID, &result.ProjectID, &result.AccountAuthority, &result.AccountID,
		&result.ContentKind, &result.Mode, &result.RetentionSeconds, &result.MaxItemBytes,
		&result.Provenance, &result.DesiredRevision, &result.EffectiveRevision, &result.EffectiveMode)
	if errors.Is(err, sql.ErrNoRows) {
		return result, reviewbody.ErrScope
	}
	return result, err
}
