package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/workspace"
)

var errAssessmentRestore = errors.New("restored Finding assessment projection, basis, action chain or creation receipt is inconsistent")

func decodeAssessmentRecord(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(target)
}

func validAssessmentReceipt(a workspace.AssessmentAction, previous workspace.Assessment, digest string) bool {
	c, err := workspace.NormalizeAssessmentCommand(a.Command)
	if err != nil || !workspace.SameAssessmentValue(c, a.Command) || assessmentRequestHash(c) != digest || a.Operation != c.Operation || a.OperationID != c.OperationID || a.CaseVersion != c.ExpectedCaseVersion+1 || !workspace.SameAssessmentValue(a.Previous, previous) {
		return false
	}
	actor, err := workspace.HumanActor(a.ActorSubjectID)
	if err != nil {
		return false
	}
	current, err := workspace.AdvanceAssessment(previous, c, a.Basis, actor, a.At, a.Initial)
	return err == nil && workspace.SameAssessmentValue(current, a.Current)
}

func validateCreationReceipt(ctx context.Context, q workspaceQueryer, r workspace.ReviewCreationReceipt, digest string) error {
	c, err := workspace.NormalizeReviewCreation(r.Command)
	if err != nil || !workspace.SameAssessmentValue(c, r.Command) || assessmentRequestHash(c) != digest || r.Operation != c.Operation || r.OperationID != c.OperationID || r.At.IsZero() || !workspace.ValidReviewBasis(r.Basis) || r.Basis.Digest != c.ExpectedBasisDigest || r.Basis.Statement != c.Finding.Statement || r.Basis.FindingVersion != 1 || !workspace.SameAssessmentValue(r.Basis.Known, c.Selection) || !workspace.SameAssessmentValue(r.Basis.Selected, c.Selection) {
		return errAssessmentRestore
	}
	if (c.ExecutionContext == nil) != (r.Basis.ResolvedExecutionContext == nil) {
		return errAssessmentRestore
	}
	if c.ExecutionContext != nil {
		if !workspace.SameAssessmentValue(c.ExecutionContext, &r.Basis.ResolvedExecutionContext.Context) || !workspace.ExecutionContextSelectionMatches(r.Basis.ResolvedExecutionContext, c.Selection) {
			return errAssessmentRestore
		}
		if err = validateDurableExecutionContext(ctx, q, r.CaseID, r.FindingID, r.Basis.ResolvedExecutionContext, false); err != nil {
			return err
		}
	}
	if _, err = workspace.HumanActor(r.ActorSubjectID); err != nil {
		return errAssessmentRestore
	}
	expectedCaseVersion := c.ExpectedCaseVersion
	if c.NewCase != nil {
		expectedCaseVersion = 1
	} else if c.CaseID != r.CaseID {
		return errAssessmentRestore
	}
	if r.CaseVersion != expectedCaseVersion+1 {
		return errAssessmentRestore
	}
	var title, statement, classification, subject string
	var created time.Time
	err = q.QueryRowContext(ctx, `SELECT title,statement,classification,created_by_subject,created_at FROM case_findings WHERE case_id=$1 AND id=$2`, r.CaseID, r.FindingID).Scan(&title, &statement, &classification, &subject, &created)
	if err != nil {
		return err
	}
	if title != c.Finding.Title || statement != c.Finding.Statement || classification != c.Finding.Classification || subject != r.ActorSubjectID || !created.Equal(r.At) {
		return errAssessmentRestore
	}
	if c.NewCase != nil {
		var description string
		err = q.QueryRowContext(ctx, `SELECT title,description,created_by_subject,created_at FROM investigation_cases WHERE id=$1`, r.CaseID).Scan(&title, &description, &subject, &created)
		if err != nil {
			return err
		}
		if title != c.NewCase.Title || description != c.NewCase.Description || subject != r.ActorSubjectID || !created.Equal(r.At) {
			return errAssessmentRestore
		}
	}
	if c.Operation == "start_review" {
		if r.Action == nil || !r.Action.Initial || r.Action.Current.CaseID != r.CaseID || r.Action.Current.FindingID != r.FindingID || r.Action.ActorSubjectID != r.ActorSubjectID || r.Action.CaseVersion != r.CaseVersion || !r.Action.At.Equal(r.At) || !workspace.SameAssessmentValue(r.Action.Basis, r.Basis) || !workspace.SameAssessmentValue(r.Action.Command.Review, c.Review) {
			return errAssessmentRestore
		}
		var raw []byte
		if err = q.QueryRowContext(ctx, `SELECT receipt FROM case_finding_assessment_actions WHERE case_id=$1 AND finding_id=$2 AND result_version=1`, r.CaseID, r.FindingID).Scan(&raw); err != nil {
			return err
		}
		var a workspace.AssessmentAction
		if decodeAssessmentRecord(raw, &a) != nil || !workspace.SameAssessmentValue(a, *r.Action) {
			return errAssessmentRestore
		}
	} else {
		if r.Action != nil {
			return errAssessmentRestore
		}
		var status, packDigest string
		var revision sql.NullInt64
		err = q.QueryRowContext(ctx, `SELECT c.status,c.sealed_digest,c.sealed_revision FROM investigation_cases c JOIN case_findings f ON c.id=f.case_id WHERE c.id=$1 AND f.id=$2`, c.From.CaseID, c.From.FindingID).Scan(&status, &packDigest, &revision)
		if err != nil {
			return err
		}
		if status != "sealed" || packDigest != c.From.Digest || !revision.Valid || revision.Int64 != c.From.Revision {
			return errAssessmentRestore
		}
	}
	return nil
}

// ValidateFindingAssessmentRestore is read-only. In particular it never joins
// capture_gap_resolutions to reconstruct or validate past adopted contents;
// those may correctly differ from the current resolution after a later edit.
func ValidateFindingAssessmentRestore(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,version,status,sealed_pack,sealed_digest FROM investigation_cases ORDER BY id`)
	if err != nil {
		return err
	}
	type caseRow struct {
		id      string
		version int64
		status  string
		pack    []byte
		digest  sql.NullString
	}
	var cases []caseRow
	for rows.Next() {
		var c caseRow
		if err = rows.Scan(&c.id, &c.version, &c.status, &c.pack, &c.digest); err != nil {
			rows.Close()
			return err
		}
		cases = append(cases, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range cases {
		snapshot, err := loadAssessmentSnapshot(ctx, tx, c.id)
		if err != nil {
			return err
		}
		if err = validateExecutionContextHistory(ctx, tx, c.id, snapshot); err != nil {
			return err
		}
		if err = validateResourceAssessmentHistory(ctx, tx, c.id, *snapshot); err != nil {
			return err
		}
		if err = validateContentAssessmentHistory(ctx, tx, c.id, *snapshot); err != nil {
			return err
		}
		usedCaseVersions := map[int64]bool{}
		responsibility, err := loadFollowupSnapshot(ctx, tx, c.id)
		if err != nil {
			return err
		}
		for _, a := range responsibility.Actions {
			usedCaseVersions[a.CaseVersion] = true
		}
		initialActions := map[string]workspace.AssessmentAction{}
		for _, current := range snapshot.Assessments {
			previous := workspace.Assessment{CaseID: c.id, FindingID: current.FindingID}
			lastCaseVersion := int64(0)
			rows, err = tx.QueryContext(ctx, `SELECT receipt,request_sha256,created_at FROM case_finding_assessment_actions WHERE case_id=$1 AND finding_id=$2 ORDER BY result_version`, c.id, current.FindingID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var raw []byte
				var digest string
				var at time.Time
				var a workspace.AssessmentAction
				if err = rows.Scan(&raw, &digest, &at); err != nil {
					rows.Close()
					return err
				}
				if decodeAssessmentRecord(raw, &a) != nil || !validAssessmentReceipt(a, previous, digest) || a.CaseVersion <= lastCaseVersion || a.CaseVersion > c.version || usedCaseVersions[a.CaseVersion] || !a.At.Equal(at) {
					rows.Close()
					return errAssessmentRestore
				}
				if a.Initial {
					initialActions[current.FindingID] = a
				}
				usedCaseVersions[a.CaseVersion] = true
				previous = a.Current
				lastCaseVersion = a.CaseVersion
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if !workspace.SameAssessmentValue(previous, current) {
				return errAssessmentRestore
			}
			if current.Recorded {
				var raw []byte
				var duplicateCase, duplicateFinding sql.NullString
				if err = tx.QueryRowContext(ctx, `SELECT record,duplicate_case_id,duplicate_finding_id FROM case_finding_assessments WHERE case_id=$1 AND finding_id=$2`, c.id, current.FindingID).Scan(&raw, &duplicateCase, &duplicateFinding); err != nil {
					return err
				}
				var strict workspace.Assessment
				if decodeAssessmentRecord(raw, &strict) != nil || !workspace.SameAssessmentValue(strict, current) {
					return errAssessmentRestore
				}
				target := duplicateTarget(current)
				if target == nil {
					if duplicateCase.Valid || duplicateFinding.Valid {
						return errAssessmentRestore
					}
				} else {
					if !duplicateCase.Valid || !duplicateFinding.Valid || duplicateCase.String != target.CaseID || duplicateFinding.String != target.FindingID {
						return errAssessmentRestore
					}
					if err = checkDuplicateTarget(ctx, tx, current, target); err != nil {
						return errAssessmentRestore
					}
				}
			}
		}
		// Read all receipts before nested queries; database/sql has one connection
		// per transaction and cannot nest while a pgx Rows stream remains open.
		type creationRow struct {
			raw                                            []byte
			digest, actor, operation, operationID, finding string
			sourceCase, sourceFinding                      sql.NullString
			at                                             time.Time
		}
		var creations []creationRow
		rows, err = tx.QueryContext(ctx, `SELECT receipt,request_sha256,actor_subject_id,operation,operation_id::text,finding_id,source_case_id,source_finding_id,created_at FROM finding_review_creations WHERE case_id=$1`, c.id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var row creationRow
			if err = rows.Scan(&row.raw, &row.digest, &row.actor, &row.operation, &row.operationID, &row.finding, &row.sourceCase, &row.sourceFinding, &row.at); err != nil {
				rows.Close()
				return err
			}
			creations = append(creations, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, row := range creations {
			var r workspace.ReviewCreationReceipt
			if decodeAssessmentRecord(row.raw, &r) != nil || r.CaseID != c.id || r.FindingID != row.finding || r.Operation != row.operation || r.OperationID != row.operationID || r.ActorSubjectID != row.actor || !r.At.Equal(row.at) || r.CaseVersion > c.version {
				return errAssessmentRestore
			}
			if r.Command.From == nil {
				if row.sourceCase.Valid || row.sourceFinding.Valid {
					return errAssessmentRestore
				}
			} else if !row.sourceCase.Valid || !row.sourceFinding.Valid || row.sourceCase.String != r.Command.From.CaseID || row.sourceFinding.String != r.Command.From.FindingID {
				return errAssessmentRestore
			}
			if err = validateCreationReceipt(ctx, tx, r, row.digest); err != nil {
				return err
			}
			if r.Action != nil {
				a, ok := initialActions[r.FindingID]
				if !ok || !workspace.SameAssessmentValue(a, *r.Action) {
					return errAssessmentRestore
				}
				delete(initialActions, r.FindingID)
			} else {
				if usedCaseVersions[r.CaseVersion] {
					return errAssessmentRestore
				}
				usedCaseVersions[r.CaseVersion] = true
			}
		}
		if len(initialActions) > 0 {
			return errAssessmentRestore
		}
		if c.status == "sealed" {
			hash := sha256.Sum256(c.pack)
			if !c.digest.Valid || c.digest.String != "sha256:"+hex.EncodeToString(hash[:]) {
				return errAssessmentRestore
			}
			var p workspace.EvidencePack
			if json.Unmarshal(c.pack, &p) != nil || !workspace.SupportedEvidencePack(p) || p.CaseID != c.id || p.Revision != c.version {
				return errAssessmentRestore
			}
			if err = validateContentPackLinks(ctx, tx, p); err != nil {
				return err
			}
			resourceLinks, e := loadResourceLinks(ctx, tx, []string{c.id})
			if e != nil {
				return e
			}
			if len(p.Resources) != len(resourceLinks) {
				return errAssessmentRestore
			}
			remaining := map[string]workspace.ResourceLink{}
			for _, link := range resourceLinks {
				remaining[queueLinkKey(link.CaseID, link.FindingID, workspace.ResourceChoiceKey(link.Reference))] = link
			}
			for _, link := range p.Resources {
				key := queueLinkKey(link.CaseID, link.FindingID, workspace.ResourceChoiceKey(link.Reference))
				stored, ok := remaining[key]
				if !ok || !workspace.SameAssessmentValue(link, stored) {
					return errAssessmentRestore
				}
				delete(remaining, key)
			}

			if p.SchemaVersion == workspace.EvidencePackSchema || p.SchemaVersion == workspace.ResourceEvidencePackSchema || p.SchemaVersion == workspace.ContentEvidencePackSchema {
				if !workspace.SameAssessmentValue(p.Assessments, snapshot) {
					return errAssessmentRestore
				}
			} else {
				if len(snapshot.Actions) != 0 || len(snapshot.Creations) != 0 {
					return errAssessmentRestore
				}
				for _, a := range snapshot.Assessments {
					if a.Recorded {
						return errAssessmentRestore
					}
				}
			}
		}
	}
	return tx.Commit()
}
