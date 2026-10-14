package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

const contentIdentityWhere = `source_system_id=$1::uuid AND source_fact_id=$2 AND revision=$3`
const contentLinkJoin = `ROW(d.source_system_id,d.source_fact_id,d.revision)=ROW(l.source_system_id,l.source_fact_id,l.revision)`

func contentIdentityArgs(c workspace.ContentChoice) []any {
	return []any{c.Reference.SourceSystemID, c.Reference.SourceFactID, c.Reference.Revision}
}

// Read the immutable retained manifest, not runtime membership, current body
// availability, or a user-supplied registration. Historical restore uses this
// resolver without consulting today's grants or requiring unexpired bytes.
func readContentChoice(ctx context.Context, q workspaceQueryer, c workspace.ContentChoice) error {
	if !workspace.ValidContentChoice(c) {
		return workspace.ErrInvalid
	}
	var raw []byte
	var hash string
	if err := q.QueryRowContext(ctx, `SELECT source_bytes,source_sha256 FROM source_content_manifests WHERE `+contentIdentityWhere, contentIdentityArgs(c)...).Scan(&raw, &hash); err != nil {
		return workspaceError(err)
	}
	m, err := sourcefacts.DecodeContentManifest(raw)
	if err != nil || sourcefacts.Hash(raw) != hash {
		return workspace.ErrConflict
	}
	if err = sourcefacts.MatchContentManifestReference(c.Reference, sourcefacts.ContentManifestEntry{Document: m, SourceSHA256: hash}); err != nil {
		return workspace.ErrConflict
	}
	return nil
}

// The grant's optional economic project is resolved from the actual exact I01
// association; a bare input project can never satisfy a restricted grant.
func contentRequirements(ctx context.Context, q workspaceQueryer, c workspace.ContentChoice) ([]workchain.Requirement, error) {
	a := workchain.FromContext(ctx)
	if a == nil {
		return nil, workspace.ErrNotFound
	}
	var cost string
	args := append(contentIdentityArgs(c), a.Subject)
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(f.cost_project_ref,'') FROM source_content_manifests d LEFT JOIN workbench_economic_facts f ON `+sourceEconomicJoinSQL+` WHERE d.source_system_id=$1::uuid AND d.source_fact_id=$2 AND d.revision=$3 AND `+contentRecordAccessSQL("$4")+` ORDER BY f.id NULLS LAST LIMIT 1`, args...).Scan(&cost); err != nil {
		return nil, workspaceError(err)
	}
	req := workchain.Requirement{Source: c.Reference.SourceSystemID, Project: "project-" + c.Reference.ProjectID, CostProject: cost, Fields: workchain.Fields{Content: true}}
	if !a.ContentAllowedFor(req.Source, req.Project, req.CostProject) {
		return nil, workspace.ErrNotFound
	}
	if err := readContentChoice(ctx, q, c); err != nil {
		return nil, err
	}
	return []workchain.Requirement{req}, nil
}

func contentRecordAccessSQL(subject string) string {
	return `EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=` + subject + ` AND g.active AND g.allow_content AND g.allowed_content_purposes='["human_review"]'::jsonb AND g.source_system_id=d.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id) AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref))`
}

func contentBasisLookup(ctx context.Context, q workspaceQueryer, caseID, findingID string, creating bool) func(workspace.ContentChoice) error {
	return func(c workspace.ContentChoice) error {
		if _, err := contentRequirements(ctx, q, c); err != nil {
			return err
		}
		if creating {
			return nil
		}
		finding := findingID
		if c.Scope == "case" {
			finding = ""
		}
		var raw []byte
		err := q.QueryRowContext(ctx, `SELECT reference_snapshot FROM case_content_links WHERE case_id=$1 AND finding_id IS NOT DISTINCT FROM NULLIF($2,'') AND choice_sha256=$3`, caseID, finding, sourcefacts.Hash([]byte(workspace.ContentChoiceKey(c)))).Scan(&raw)
		if err != nil {
			return workspaceError(err)
		}
		var stored workspace.ContentChoice
		if json.Unmarshal(raw, &stored) != nil || workspace.ContentChoiceKey(stored) != workspace.ContentChoiceKey(c) {
			return workspace.ErrConflict
		}
		return nil
	}
}

// ResolveContentChoice admits a short, request-local read for validating a
// new JSON Pointer selection. It does not depend on a temporary reading URL,
// and releases all grant/source locks before the caller performs body I/O.
func (s *WorkspaceStore) ResolveContentChoice(ctx context.Context, subject string, c workspace.ContentChoice, now time.Time) (workbench.RuntimeContentRead, error) {
	var result workbench.RuntimeContentRead
	if !workspace.ValidContentChoice(c) {
		return result, workspace.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `LOCK TABLE workbench_access_grants IN ACCESS SHARE MODE`); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, WorkchainGrantLock); err != nil {
		return result, err
	}
	auth, err := loadWorkbenchAuthorization(ctx, tx, subject)
	if err != nil {
		return result, err
	}
	a := &workchain.Access{Subject: subject, Authorization: auth}
	if _, err = contentRequirements(workchain.WithAccess(ctx, a), tx, c); err != nil {
		return result, err
	}
	if auth.ContentRevision == "" {
		return result, workbench.ErrScopeForbidden
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended('source-facts:'||$1,0))`, c.Reference.SourceSystemID); err != nil {
		return result, err
	}
	var collectionID, scope, epoch, sequence string
	if err = tx.QueryRowContext(ctx, `SELECT a.collection_id::text,a.content_scope_revision,a.recovery_epoch,a.sequence::text,d.occurred_at
 FROM source_content_manifests d JOIN source_content_receipts a USING(source_system_id,source_fact_id,revision)
 JOIN source_content_collections c ON c.id=a.collection_id
 WHERE d.source_system_id=$1::uuid AND d.source_fact_id=$2 AND d.revision=$3
 AND c.status='active' AND NOT c.requires_attention
 AND ROW(a.credential_id,a.registration_hash,a.content_scope_revision,a.recovery_epoch,a.coverage_from)=ROW(c.credential_id,c.registration_hash,c.content_scope_revision,c.recovery_epoch,c.coverage_from)
 AND a.source_sha256=d.source_sha256 ORDER BY a.received_at DESC,a.id LIMIT 1`, contentIdentityArgs(c)...).Scan(&collectionID, &scope, &epoch, &sequence, &result.ManifestOccurredAt); err != nil {
		return result, workspaceError(err)
	}
	result.Collection, err = loadContentCollection(ctx, tx, collectionID, false)
	if err != nil {
		return result, err
	}
	collection := result.Collection
	if collection.Status != "active" || collection.RequiresAttention || collection.ContentScopeRevision != scope || collection.RecoveryEpoch != epoch || collection.SourceSystemID != c.Reference.SourceSystemID || collection.SourceInstance != c.Reference.SourceInstance || collection.OrgID != c.Reference.OrgID || collection.ProjectID != c.Reference.ProjectID || len(collection.AllowedPurposes) != 1 || collection.AllowedPurposes[0] != sourcefacts.ContentPurposeHumanReview {
		return result, workbench.ErrAccessChanged
	}
	// Both string sequences are validated by the retained collection contract.
	if len(sequence) < len(collection.CoverageFrom.AfterSequence) || len(sequence) == len(collection.CoverageFrom.AfterSequence) && sequence <= collection.CoverageFrom.AfterSequence {
		return result, workbench.ErrScopeForbidden
	}
	expires, err := time.Parse(sourcefacts.TimeFormat, c.Reference.ExpiresAt)
	if err != nil || !now.Before(expires) {
		return result, workbench.ErrContentExpired
	}
	result.ManifestSequence = sequence
	result.Reference = c.Reference
	result.ContentAuthorizationRevision = auth.ContentRevision
	result.MembershipRef = sourcefacts.Hash([]byte(workspace.ContentChoiceKey(c)))
	return result, tx.Rollback()
}

func validateContentBasisReferences(b workspace.ReviewBasis, lookup func(workspace.ContentChoice) error) error {
	for _, c := range b.Selected.Contents {
		if err := lookup(c); err != nil {
			return err
		}
	}
	return nil
}

func scopedContentCaseSQL(caseID, subject string) string {
	return `NOT EXISTS(SELECT 1 FROM case_content_links l LEFT JOIN source_content_manifests d ON ` + contentLinkJoin + ` WHERE l.case_id=` + caseID + ` AND (d.source_fact_id IS NULL OR NOT EXISTS(SELECT 1 FROM source_content_manifests d LEFT JOIN workbench_economic_facts f ON ` + sourceEconomicJoinSQL + ` WHERE ROW(d.source_system_id,d.source_fact_id,d.revision)=ROW(l.source_system_id,l.source_fact_id,l.revision) AND ` + contentRecordAccessSQL(subject) + `)))`
}

// Without an authenticated workchain context, legacy workspace reads may
// still read old Cases, but cannot enumerate content references or a pack.
// Related Cases are included so a continuation cannot expose the sealed source.
func unscopedContentCaseSQL(caseID string) string {
	return `NOT EXISTS(WITH RECURSIVE related(id) AS (SELECT ` + caseID + `::text UNION SELECT refs.id FROM related r CROSS JOIN LATERAL (SELECT DISTINCT ref #>> '{}' AS id FROM (` + caseDocumentsSQL("r.id") + `) docs CROSS JOIN LATERAL jsonb_path_query(docs.doc,'lax $.**.case_id') ref UNION SELECT c.case_id FROM finding_review_creations c WHERE c.source_case_id=r.id) refs WHERE refs.id IS NOT NULL AND refs.id<>'') SELECT 1 FROM related r WHERE EXISTS(SELECT 1 FROM case_content_links l WHERE l.case_id=r.id) OR EXISTS(SELECT 1 FROM (` + caseDocumentsSQL("r.id") + `) docs WHERE jsonb_path_exists(docs.doc,'lax $.**.contents[*]') OR jsonb_path_exists(docs.doc,'lax $.**.verification_contents[*]')))`
}

func contentLinksPresent(ctx context.Context, q workspaceQueryer) (bool, error) {
	var present bool
	err := q.QueryRowContext(ctx, `SELECT to_regclass('public.case_content_links') IS NOT NULL`).Scan(&present)
	return present, err
}

func unscopedContentCaseCondition(ctx context.Context, q workspaceQueryer, id string) (string, error) {
	present, err := contentLinksPresent(ctx, q)
	if err != nil {
		return "", err
	}
	if !present {
		return "true", nil
	}
	return unscopedContentCaseSQL(id), nil
}

func authorizeUnscopedContentCase(ctx context.Context, q workspaceQueryer, id string) error {
	condition, err := unscopedContentCaseCondition(ctx, q, "$1")
	if err != nil {
		return err
	}
	if condition == "true" {
		return nil
	}
	var allowed bool
	if err = q.QueryRowContext(ctx, `SELECT `+condition, id).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return workspace.ErrNotFound
	}
	return nil
}

// Mutation and preview entry points require byte-backed JSON Pointer evidence.
// Ordinary historical/current basis reads never create a new adoption.
func requireContentLocationProof(ctx context.Context, selection workspace.BasisSelection) error {
	for _, c := range selection.Contents {
		if c.Location.Kind == "json_pointer" {
			a := workchain.FromContext(ctx)
			if a == nil || !workspace.ContentLocationVerified(ctx, a.Subject, c) {
				return workspace.ErrContentLocationProofRequired
			}
		}
	}
	return nil
}
