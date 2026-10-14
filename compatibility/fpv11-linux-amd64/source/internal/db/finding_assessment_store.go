package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"time"

	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

func assessmentRequestHash(c any) string {
	raw, _ := json.Marshal(c)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func readAssessment(ctx context.Context, q workspaceQueryer, caseID, findingID string) (workspace.Assessment, error) {
	a := workspace.Assessment{CaseID: caseID, FindingID: findingID}
	if _, _, err := findingBasisIdentity(ctx, q, caseID, findingID); err != nil {
		return a, err
	}
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT record FROM case_finding_assessments WHERE case_id=$1 AND finding_id=$2`, caseID, findingID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	err = json.Unmarshal(raw, &a)
	return a, err
}

func readAssessmentHistory(ctx context.Context, q workspaceQueryer, caseID, findingID string, through, before int64, limit int) (workspace.AssessmentHistory, error) {
	h := workspace.AssessmentHistory{Items: []workspace.AssessmentAction{}, ThroughVersion: through}
	if through < 0 || before < 0 || limit < 1 || limit > 100 || (before > 0 && before-1 > through) {
		return h, workspace.ErrInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT receipt FROM case_finding_assessment_actions WHERE case_id=$1 AND finding_id=$2 AND result_version<=$3 AND ($4::bigint=0 OR result_version<$4) ORDER BY result_version DESC LIMIT $5`, caseID, findingID, through, before, limit+1)
	if err != nil {
		return h, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var a workspace.AssessmentAction
		if err = rows.Scan(&raw); err != nil {
			return h, err
		}
		if err = json.Unmarshal(raw, &a); err != nil {
			return h, err
		}
		h.Items = append(h.Items, a)
	}
	if err = rows.Err(); err != nil {
		return h, err
	}
	if len(h.Items) > limit {
		h.Items = h.Items[:limit]
		v := h.Items[limit-1].Current.Version
		h.NextBefore = &v
	}
	return h, nil
}

func evaluateAssessmentBasis(ctx context.Context, q workspaceQueryer, caseID, findingID string, old *workspace.ReviewBasis) (workspace.BasisEvaluation, error) {
	if old == nil {
		return workspace.EvaluateReviewBasis(nil, workspace.ReviewBasis{}, nil)
	}
	b, err := resolveFindingBasis(ctx, q, caseID, findingID, old.Selected)
	v, err := workspace.EvaluateReviewBasis(old, b, err)
	if err != nil || v.State == "unavailable" {
		return v, err
	}
	updates, err := loadResourceUpdates(ctx, q, old.Selected.Resources)
	if err != nil {
		return v, err
	}
	return workspace.ApplyResourceUpdates(v, updates, old.Selected.Resources), nil
}

func (s *WorkspaceStore) GetFindingAssessment(ctx context.Context, caseID, findingID string, now time.Time) (workspace.AssessmentView, error) {
	v := workspace.AssessmentView{EvaluatedAt: now.UTC(), AllowedActions: []string{}, Continuations: []workspace.ReviewCreationReceipt{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT status,version FROM investigation_cases WHERE id=$1`, caseID).Scan(&v.CaseStatus, &v.CaseVersion); err != nil {
		return v, workspaceError(err)
	}
	if v.Assessment, err = readAssessment(ctx, tx, caseID, findingID); err != nil {
		return v, err
	}
	var reviewBasis, progressBasis *workspace.ReviewBasis
	if v.Assessment.Review != nil {
		reviewBasis = &v.Assessment.Review.Basis
	}
	if v.Assessment.Progress != nil {
		progressBasis = &v.Assessment.Progress.Basis
	}
	if v.ReviewBasis, err = evaluateAssessmentBasis(ctx, tx, caseID, findingID, reviewBasis); err != nil {
		return v, err
	}
	if v.ProgressBasis, err = evaluateAssessmentBasis(ctx, tx, caseID, findingID, progressBasis); err != nil {
		return v, err
	}
	if v.History, err = readAssessmentHistory(ctx, tx, caseID, findingID, v.Assessment.Version, 0, 20); err != nil {
		return v, err
	}
	if workspace.FindingProgressState(v.Assessment) != "ended" {
		responsibility, e := readFollowup(ctx, tx, caseID, findingID)
		if e != nil {
			return v, e
		}
		v.Overdue = workspace.FindingIsOverdue(v.Assessment, responsibility, now)
	}
	rows, err := tx.QueryContext(ctx, `SELECT receipt FROM finding_review_creations WHERE (case_id=$1 AND finding_id=$2) OR (source_case_id=$1 AND source_finding_id=$2) ORDER BY created_at,case_id,finding_id`, caseID, findingID)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var raw []byte
		var receipt workspace.ReviewCreationReceipt
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return v, err
		}
		if err = json.Unmarshal(raw, &receipt); err != nil {
			rows.Close()
			return v, err
		}
		v.Continuations = append(v.Continuations, receipt)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	return v, commitWorkspaceTx(ctx, tx)
}

func (s *WorkspaceStore) GetAssessmentHistory(ctx context.Context, caseID, findingID string, through, before int64, limit int) (workspace.AssessmentHistory, error) {
	var h workspace.AssessmentHistory
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return h, err
	}
	defer tx.Rollback()
	a, err := readAssessment(ctx, tx, caseID, findingID)
	if err != nil {
		return h, err
	}
	if through > a.Version {
		return h, workspace.ErrInvalid
	}
	h, err = readAssessmentHistory(ctx, tx, caseID, findingID, through, before, limit)
	if err != nil {
		return h, err
	}
	return h, commitWorkspaceTx(ctx, tx)
}

func lockAssessmentCases(ctx context.Context, tx *sql.Tx, ids ...string) error {
	sort.Strings(ids)
	last := ""
	for _, id := range ids {
		if id == "" || id == last {
			continue
		}
		last = id
		if err := prepareScopedCase(ctx, tx, id); err != nil {
			return err
		}
		var got string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM investigation_cases WHERE id=$1 FOR UPDATE`, id).Scan(&got); err != nil {
			return workspaceError(err)
		}
	}
	return nil
}

// The finite duplicate graph has one outgoing edge per Finding. All progress
// changes acquire this native transaction lock BEFORE Case locks. Serializing
// graph changes (including reopen/removal) covers cycles assembled by disjoint
// source/target pairs, which endpoint row locks alone cannot prevent. Other
// review, responsibility, source and seal transactions do not need this lock.
const findingDuplicateGraphLock int64 = 0x5849445550475241

func checkDuplicateTarget(ctx context.Context, q workspaceQueryer, source workspace.Assessment, target *workspace.FindingReference) error {
	if target == nil {
		return nil
	}
	if _, _, err := findingBasisIdentity(ctx, q, target.CaseID, target.FindingID); err != nil {
		return err
	}
	var cycle bool
	err := q.QueryRowContext(ctx, `WITH RECURSIVE chain(case_id,finding_id) AS (
 SELECT $1::text,$2::text UNION
 SELECT a.duplicate_case_id,a.duplicate_finding_id FROM chain c JOIN case_finding_assessments a ON a.case_id=c.case_id AND a.finding_id=c.finding_id WHERE a.duplicate_case_id IS NOT NULL
) SELECT EXISTS(SELECT 1 FROM chain WHERE case_id=$3 AND finding_id=$4)`, target.CaseID, target.FindingID, source.CaseID, source.FindingID).Scan(&cycle)
	if err != nil {
		return err
	}
	if cycle {
		return workspace.ErrConflict
	}
	return nil
}

func duplicateTarget(a workspace.Assessment) *workspace.FindingReference {
	if a.Progress != nil && a.Progress.State == "ended" && a.Progress.Closure != nil && a.Progress.Closure.Kind == "duplicate" {
		return a.Progress.Closure.Duplicate
	}
	return nil
}

func persistAssessmentAction(ctx context.Context, tx *sql.Tx, a workspace.AssessmentAction) error {
	record, err := json.Marshal(a.Current)
	if err != nil {
		return err
	}
	var targetCase, targetFinding any
	if target := duplicateTarget(a.Current); target != nil {
		targetCase = target.CaseID
		targetFinding = target.FindingID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO case_finding_assessments(case_id,finding_id,version,record,duplicate_case_id,duplicate_finding_id) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(case_id,finding_id) DO UPDATE SET version=EXCLUDED.version,record=EXCLUDED.record,duplicate_case_id=EXCLUDED.duplicate_case_id,duplicate_finding_id=EXCLUDED.duplicate_finding_id`, a.Current.CaseID, a.Current.FindingID, a.Current.Version, record, targetCase, targetFinding)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO case_finding_assessment_actions(case_id,finding_id,actor_subject_id,operation_id,request_sha256,result_version,created_at,receipt) VALUES($1,$2,$3,$4::uuid,$5,$6,$7,$8)`, a.Current.CaseID, a.Current.FindingID, a.ActorSubjectID, a.OperationID, assessmentRequestHash(a.Command), a.Current.Version, a.At, raw)
	return err
}

func (s *WorkspaceStore) ApplyFindingAssessment(ctx context.Context, caseID, findingID string, actor workspace.Actor, input workspace.AssessmentCommand, now time.Time) (workspace.AssessmentAction, error) {
	var result workspace.AssessmentAction
	if actor.Kind != "human" || now.IsZero() {
		return result, workspace.ErrInvalid
	}
	if _, err := workspace.HumanActor(actor.Ref); err != nil {
		return result, err
	}
	input.Selection = scopedSelectionInput(ctx, input.Selection)
	c, err := workspace.NormalizeAssessmentCommand(input)
	if err != nil {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = resolveFindingSelection(ctx, tx, caseID, findingID, &c.Selection); err != nil {
		return result, err
	}
	if err = authorizeScopedCase(ctx, tx, caseID); err != nil {
		return result, err
	}
	if result, found, replayErr := readAssessmentReceipt(ctx, tx, caseID, findingID, actor.Ref, c); found || replayErr != nil {
		return result, replayErr
	}
	if err = requireContentLocationProof(ctx, c.Selection); err != nil {
		return result, err
	}
	if c.Progress != nil && c.Progress.Closure != nil {
		if err = requireContentLocationProof(ctx, workspace.BasisSelection{Contents: c.Progress.Closure.VerificationContents}); err != nil {
			return result, err
		}
	}
	if c.Operation == "record_progress" {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, findingDuplicateGraphLock); err != nil {
			return result, err
		}
	}
	ids := []string{caseID}
	if c.Progress != nil && c.Progress.Closure != nil && c.Progress.Closure.Duplicate != nil {
		ids = append(ids, c.Progress.Closure.Duplicate.CaseID)
	}
	if err = lockAssessmentCases(ctx, tx, ids...); err != nil {
		return result, followupError(err)
	}
	previous, err := readAssessment(ctx, tx, caseID, findingID)
	if err != nil {
		return result, err
	}
	var raw []byte
	var digest string
	err = tx.QueryRowContext(ctx, `SELECT request_sha256,receipt FROM case_finding_assessment_actions WHERE case_id=$1 AND finding_id=$2 AND actor_subject_id=$3 AND operation_id=$4::uuid`, caseID, findingID, actor.Ref, c.OperationID).Scan(&digest, &raw)
	if err == nil {
		if digest != assessmentRequestHash(c) {
			return result, workspace.ErrConflict
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			return result, err
		}
		return result, finishAssessmentReplay(ctx)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	var status string
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT status,version FROM investigation_cases WHERE id=$1`, caseID).Scan(&status, &version); err != nil {
		return result, workspaceError(err)
	}
	if status != "open" {
		return result, workspace.ErrSealed
	}
	if version != c.ExpectedCaseVersion || previous.Version != c.ExpectedVersion {
		return result, workspace.ErrConflict
	}
	basis, err := resolveFindingBasis(ctx, tx, caseID, findingID, c.Selection)
	if err != nil {
		return result, err
	}
	if basis.Digest != c.ExpectedBasisDigest {
		return result, workspace.ErrConflict
	}
	now = now.UTC().Truncate(time.Microsecond)
	current, err := workspace.AdvanceAssessment(previous, c, basis, actor, now, false)
	if err != nil {
		return result, err
	}
	if err = checkDuplicateTarget(ctx, tx, previous, duplicateTarget(current)); err != nil {
		return result, err
	}
	result = workspace.AssessmentAction{OperationID: c.OperationID, Operation: c.Operation, ActorSubjectID: actor.Ref, At: now, CaseVersion: version + 1, Command: c, Basis: basis, Previous: previous, Current: current}
	if err = persistAssessmentAction(ctx, tx, result); err != nil {
		return workspace.AssessmentAction{}, followupError(err)
	}
	if err = touchOpenCase(ctx, tx, caseID, now); err != nil {
		return workspace.AssessmentAction{}, followupError(err)
	}
	if err = commitWorkspaceTx(ctx, tx); err != nil {
		return workspace.AssessmentAction{}, followupError(err)
	}
	return result, nil
}

// Receipt retries do not update case labels, versions, or actions. The outer
// authorization transaction still holds current grants; session revocation is
// checked here as well as immediately before publishing the buffered response.
func finishAssessmentReplay(ctx context.Context) error {
	if a := workchain.FromContext(ctx); a != nil {
		if a.CommitCheck == nil {
			return workchain.ErrDenied
		}
		return a.CommitCheck(func() error { return nil })
	}
	return nil
}

func readAssessmentReceipt(ctx context.Context, q workspaceQueryer, caseID, findingID, actor string, c workspace.AssessmentCommand) (workspace.AssessmentAction, bool, error) {
	var result workspace.AssessmentAction
	var digest string
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT request_sha256,receipt FROM case_finding_assessment_actions WHERE case_id=$1 AND finding_id=$2 AND actor_subject_id=$3 AND operation_id=$4::uuid`, caseID, findingID, actor, c.OperationID).Scan(&digest, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	if digest != assessmentRequestHash(c) {
		return result, true, workspace.ErrConflict
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, true, err
	}
	return result, true, finishAssessmentReplay(ctx)
}

func creationLockKey(actor string, c workspace.ReviewCreationCommand) int64 {
	h := sha256.Sum256([]byte("xinsight.review-creation.v1\x00" + actor + "\x00" + c.Operation + "\x00" + c.OperationID))
	return int64(binary.BigEndian.Uint64(h[:8]))
}

func insertCreationReferences(ctx context.Context, tx *sql.Tx, caseID, findingID string, actor workspace.Actor, selection workspace.BasisSelection, now time.Time) error {
	for _, e := range selection.Evidence {
		if _, err := tx.ExecContext(ctx, `INSERT INTO case_evidence_links(case_id,finding_id,evidence_event_id,relation,event_hash_snapshot,source_ref_snapshot,added_by_subject,added_at) VALUES($1,$2,$3::uuid,$4,$5,$6,$7,$8)`, caseID, findingID, e.EvidenceEventID, e.Relation, e.EventHash, e.SourceRef, actor.Ref, now); err != nil {
			return err
		}
	}
	for _, g := range selection.Gaps {
		if _, err := tx.ExecContext(ctx, `INSERT INTO case_gap_links(case_id,finding_id,gap_id,relation,affected_claim_snapshot,gap_type_snapshot,added_by_subject,added_at) VALUES($1,$2,$3::uuid,$4,$5,$6,$7,$8)`, caseID, findingID, g.GapID, g.Relation, g.AffectedClaim, g.GapType, actor.Ref, now); err != nil {
			return err
		}
	}
	if err := insertResourceLinks(ctx, tx, caseID, findingID, actor, selection.Resources, now); err != nil {
		return err
	}
	return insertContentLinks(ctx, tx, caseID, findingID, actor, selection.Contents, now)
}

func (s *WorkspaceStore) CreateReview(ctx context.Context, actor workspace.Actor, input workspace.ReviewCreationCommand, now time.Time, random io.Reader) (workspace.ReviewCreationReceipt, error) {
	var result workspace.ReviewCreationReceipt
	if actor.Kind != "human" || now.IsZero() {
		return result, workspace.ErrInvalid
	}
	if _, err := workspace.HumanActor(actor.Ref); err != nil {
		return result, err
	}
	input.Selection = scopedSelectionInput(ctx, input.Selection)
	c, err := workspace.NormalizeReviewCreation(input)
	if err != nil {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = resolveScopedSelection(ctx, tx, &c.Selection); err != nil {
		return result, err
	}
	// Read Committed sees the first transaction's committed receipt after this
	// wait, including a retry whose client never received the original IDs.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, creationLockKey(actor.Ref, c)); err != nil {
		return result, err
	}
	var digest string
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT request_sha256,receipt FROM finding_review_creations WHERE actor_subject_id=$1 AND operation=$2 AND operation_id=$3::uuid`, actor.Ref, c.Operation, c.OperationID).Scan(&digest, &raw)
	if err == nil {
		if err = json.Unmarshal(raw, &result); err != nil {
			return result, err
		}
		if err = authorizeScopedCase(ctx, tx, result.CaseID); err != nil {
			return result, err
		}
		if digest != assessmentRequestHash(c) {
			return result, workspace.ErrConflict
		}
		return result, finishAssessmentReplay(ctx)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if err = requireContentLocationProof(ctx, c.Selection); err != nil {
		return result, err
	}
	ids := []string{c.CaseID}
	if c.From != nil {
		ids = append(ids, c.From.CaseID)
	}
	if err = lockAssessmentCases(ctx, tx, ids...); err != nil {
		return result, followupError(err)
	}
	if err = validateCreationContext(ctx, tx, c); err != nil {
		return result, err
	}
	basis, err := resolveReviewBasis(ctx, tx, "", "", c.Finding.Statement, 1, c.Selection, c.Selection, true)
	if err != nil {
		return result, err
	}
	basis, err = resolveCreationExecutionContext(ctx, tx, c, basis)
	if err != nil {
		return result, err
	}
	if basis.Digest != c.ExpectedBasisDigest {
		return result, workspace.ErrConflict
	}
	now = now.UTC().Truncate(time.Microsecond)
	caseID := c.CaseID
	version := c.ExpectedCaseVersion
	if c.NewCase != nil {
		caseID, err = workspace.NewID("xc_", random)
		if err != nil {
			return result, err
		}
		item, e := insertCaseRecord(ctx, tx, caseID, actor, *c.NewCase, now)
		if e != nil {
			return result, followupError(e)
		}
		version = item.Version
		if a := workchain.FromContext(ctx); a != nil {
			if !a.Reviewer {
				return result, workspace.ErrNotFound
			}
			registerScopedCase(a, tx, caseID, nil)
		}
	}
	findingID, err := workspace.NewID("xf_", random)
	if err != nil {
		return result, err
	}
	if _, err = insertFindingRecord(ctx, tx, findingID, caseID, actor, c.Finding, now); err != nil {
		return result, followupError(err)
	}
	if err = insertCreationReferences(ctx, tx, caseID, findingID, actor, c.Selection, now); err != nil {
		return result, followupError(err)
	}
	if err = insertReviewExecution(ctx, tx, caseID, actor, basis, now); err != nil {
		return result, err
	}
	result = workspace.ReviewCreationReceipt{OperationID: c.OperationID, Operation: c.Operation, ActorSubjectID: actor.Ref, At: now, CaseID: caseID, FindingID: findingID, CaseVersion: version + 1, Command: c, Basis: basis}
	if c.Operation == "start_review" {
		command := workspace.AssessmentCommand{Operation: "record_review", OperationID: c.OperationID, ExpectedVersion: 0, ExpectedCaseVersion: version, ExpectedBasisDigest: basis.Digest, Selection: c.Selection, Review: c.Review}
		previous := workspace.Assessment{CaseID: caseID, FindingID: findingID}
		current, e := workspace.AdvanceAssessment(previous, command, basis, actor, now, true)
		if e != nil {
			return workspace.ReviewCreationReceipt{}, e
		}
		a := workspace.AssessmentAction{OperationID: c.OperationID, Operation: "record_review", ActorSubjectID: actor.Ref, At: now, CaseVersion: version + 1, Initial: true, Command: command, Basis: basis, Previous: previous, Current: current}
		if err = persistAssessmentAction(ctx, tx, a); err != nil {
			return workspace.ReviewCreationReceipt{}, followupError(err)
		}
		result.Action = &a
	}
	if err = touchOpenCase(ctx, tx, caseID, now); err != nil {
		return workspace.ReviewCreationReceipt{}, followupError(err)
	}
	var sourceCase, sourceFinding any
	if c.From != nil {
		sourceCase = c.From.CaseID
		sourceFinding = c.From.FindingID
	}
	raw, err = json.Marshal(result)
	if err != nil {
		return workspace.ReviewCreationReceipt{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO finding_review_creations(actor_subject_id,operation,operation_id,request_sha256,case_id,finding_id,source_case_id,source_finding_id,created_at,receipt) VALUES($1,$2,$3::uuid,$4,$5,$6,$7,$8,$9,$10)`, actor.Ref, c.Operation, c.OperationID, assessmentRequestHash(c), caseID, findingID, sourceCase, sourceFinding, now, raw)
	if err != nil {
		return workspace.ReviewCreationReceipt{}, followupError(err)
	}
	if err = commitWorkspaceTx(ctx, tx, func() error {
		return checkExecutionContextDeadline(ctx, tx, c.ExecutionContext, c.Selection.Contents...)
	}); err != nil {
		return workspace.ReviewCreationReceipt{}, followupError(err)
	}
	return result, nil
}

func loadAssessmentSnapshot(ctx context.Context, q workspaceQueryer, caseID string) (*workspace.AssessmentSnapshot, error) {
	s := &workspace.AssessmentSnapshot{Assessments: []workspace.Assessment{}, Actions: []workspace.AssessmentAction{}, Creations: []workspace.ReviewCreationReceipt{}}
	rows, err := q.QueryContext(ctx, `SELECT f.id,a.record FROM case_findings f LEFT JOIN case_finding_assessments a ON a.case_id=f.case_id AND a.finding_id=f.id WHERE f.case_id=$1 ORDER BY f.id`, caseID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		a := workspace.Assessment{CaseID: caseID, FindingID: id}
		if len(raw) > 0 {
			if err = json.Unmarshal(raw, &a); err != nil {
				rows.Close()
				return nil, err
			}
		}
		s.Assessments = append(s.Assessments, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT receipt FROM case_finding_assessment_actions WHERE case_id=$1 ORDER BY finding_id,result_version`, caseID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw []byte
		var a workspace.AssessmentAction
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(raw, &a); err != nil {
			rows.Close()
			return nil, err
		}
		s.Actions = append(s.Actions, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Only creations INTO this Case belong to its immutable pack. Later outgoing
	// continuations are read separately and can never alter a sealed snapshot.
	rows, err = q.QueryContext(ctx, `SELECT receipt FROM finding_review_creations WHERE case_id=$1 ORDER BY finding_id`, caseID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw []byte
		var a workspace.ReviewCreationReceipt
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(raw, &a); err != nil {
			rows.Close()
			return nil, err
		}
		s.Creations = append(s.Creations, a)
	}
	err = rows.Err()
	rows.Close()
	return s, err
}
