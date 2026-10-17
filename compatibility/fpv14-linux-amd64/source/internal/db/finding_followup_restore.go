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

var errResponsibilityRestore = errors.New("restored Finding responsibility projection, history or sealed package is inconsistent")

func sameFollowup(a, b workspace.Followup) bool {
	left, e1 := json.Marshal(a)
	right, e2 := json.Marshal(b)
	return e1 == nil && e2 == nil && bytes.Equal(left, right)
}

func validateResponsibilityReceipt(a workspace.FollowupAction, previous workspace.Followup, digest string) bool {
	if !sameFollowup(a.Previous, previous) || !a.Current.Recorded || a.Current.CaseID != previous.CaseID || a.Current.FindingID != previous.FindingID || a.Current.Version != previous.Version+1 || a.ExpectedVersion != previous.Version || a.CaseVersion != a.ExpectedCaseVersion+1 || a.Current.UpdatedBySubject != a.ActorSubjectID || a.Current.UpdatedAt == nil || !a.Current.UpdatedAt.Equal(a.At) || a.At.IsZero() {
		return false
	}
	if _, err := workspace.HumanActor(a.ActorSubjectID); err != nil {
		return false
	}
	if a.Current.AssigneeSubjectID == nil {
		if a.Current.AssigneeName != "" {
			return false
		}
	} else if len(a.Current.AssigneeName) == 0 || len(a.Current.AssigneeName) > 64 {
		return false
	}
	if previous.AssigneeSubjectID != nil && a.Current.AssigneeSubjectID != nil && *previous.AssigneeSubjectID == *a.Current.AssigneeSubjectID && previous.AssigneeName != a.Current.AssigneeName {
		return false
	}
	c := workspace.FollowupCommand{Operation: a.Operation, OperationID: a.OperationID, ExpectedVersion: a.ExpectedVersion, ExpectedCaseVersion: a.ExpectedCaseVersion, Payload: workspace.FollowupPayload{AssigneeSubjectID: a.Current.AssigneeSubjectID, DueAt: a.Current.DueAt, DueTimezone: a.Current.DueTimezone, Reason: a.Reason}}
	if c.Payload.DueAt != nil && c.Payload.DueTimezone != nil {
		loc, err := time.LoadLocation(*c.Payload.DueTimezone)
		if err != nil {
			return false
		}
		at := c.Payload.DueAt.In(loc)
		c.Payload.DueAt = &at
	}
	normalized, err := workspace.NormalizeFollowupCommand(c)
	if err != nil || normalized.Payload.Reason != a.Reason {
		return false
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return false
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]) == digest
}

// ValidateFindingResponsibilityRestore checks the persisted chain and exact
// sealed bytes. Historical account availability is deliberately not consulted.
func ValidateFindingResponsibilityRestore(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,version,status,sealed_pack,sealed_digest,sealed_revision FROM investigation_cases ORDER BY id`)
	if err != nil {
		return err
	}
	type record struct {
		id       string
		version  int64
		status   string
		pack     []byte
		digest   sql.NullString
		revision sql.NullInt64
	}
	var cases []record
	for rows.Next() {
		var item record
		if err = rows.Scan(&item.id, &item.version, &item.status, &item.pack, &item.digest, &item.revision); err != nil {
			rows.Close()
			return err
		}
		cases = append(cases, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range cases {
		snapshot, err := loadFollowupSnapshot(ctx, tx, item.id)
		if err != nil {
			return err
		}
		caseVersions := make(map[int64]bool, len(snapshot.Actions))
		for _, current := range snapshot.Followups {
			previous := workspace.Followup{CaseID: current.CaseID, FindingID: current.FindingID}
			lastCaseVersion := int64(0)
			rows, err := tx.QueryContext(ctx, `SELECT receipt,request_sha256,created_at FROM case_finding_actions WHERE case_id=$1 AND finding_id=$2 ORDER BY result_version`, item.id, current.FindingID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var raw []byte
				var digest string
				var createdAt time.Time
				var a workspace.FollowupAction
				if err = rows.Scan(&raw, &digest, &createdAt); err != nil {
					rows.Close()
					return err
				}
				if json.Unmarshal(raw, &a) != nil || !validateResponsibilityReceipt(a, previous, digest) || a.CaseVersion <= lastCaseVersion || a.CaseVersion > item.version || caseVersions[a.CaseVersion] || !a.At.Equal(createdAt) {
					rows.Close()
					return errResponsibilityRestore
				}
				caseVersions[a.CaseVersion] = true
				previous = a.Current
				lastCaseVersion = a.CaseVersion
			}
			if err = rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
			if !sameFollowup(previous, current) {
				return errResponsibilityRestore
			}
		}
		if item.status == "open" {
			continue
		}
		hash := sha256.Sum256(item.pack)
		if !item.digest.Valid || "sha256:"+hex.EncodeToString(hash[:]) != item.digest.String {
			return errResponsibilityRestore
		}
		var pack workspace.EvidencePack
		if json.Unmarshal(item.pack, &pack) != nil || !workspace.SupportedEvidencePack(pack) || pack.CaseID != item.id || !item.revision.Valid || pack.Revision != item.revision.Int64 {
			return errResponsibilityRestore
		}
		if pack.SchemaVersion == workspace.EvidencePackSchemaV1 {
			if len(snapshot.Actions) != 0 {
				return errResponsibilityRestore
			}
			continue
		}
		left, _ := json.Marshal(snapshot)
		right, _ := json.Marshal(pack.Responsibility)
		if !bytes.Equal(left, right) {
			return errResponsibilityRestore
		}
	}
	return tx.Commit()
}
