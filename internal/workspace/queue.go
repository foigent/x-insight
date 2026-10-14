package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const QueueFilterSchema = "xinsight.investigation-queue-filter.v1"

type QueueFilters struct {
	Mode       string     `json:"mode"`
	Period     string     `json:"period"`
	From       *time.Time `json:"from,omitempty"`
	To         *time.Time `json:"to,omitempty"`
	Assignee   string     `json:"assignee,omitempty"`
	Progress   string     `json:"progress,omitempty"`
	Conclusion string     `json:"conclusion,omitempty"`
	Basis      string     `json:"basis,omitempty"`
	CaseStatus string     `json:"case_status,omitempty"`
	Search     string     `json:"search,omitempty"`
	Sort       string     `json:"sort"`
}

func (f *QueueFilters) UnmarshalJSON(raw []byte) error {
	type plain QueueFilters
	return decodeAssessmentFields(raw, (*plain)(f), "mode", "period", "sort")
}
func NormalizeQueueFilters(f QueueFilters) (QueueFilters, error) {
	if f.Mode == "" {
		f.Mode = "current"
	}
	if f.Period == "" {
		f.Period = "24h"
	}
	if f.Sort == "" {
		f.Sort = "created_desc"
	}
	f.Search = strings.TrimSpace(f.Search)
	one := func(value string, choices ...string) bool {
		for _, v := range choices {
			if v == value {
				return true
			}
		}
		return false
	}
	if !one(f.Mode, "current", "created", "reassessment") || !one(f.Period, "24h", "7d", "custom") || !one(f.Sort, "created_desc", "due_asc") ||
		!one(f.Progress, "", "unrecorded", "not_started", "in_progress", "ended") || !one(f.Conclusion, "", "unrecorded", "pending_review", "supported", "not_supported", "insufficient_evidence") ||
		!one(f.Basis, "", "unrecorded", "unchanged", "changed", "unavailable") || !one(f.CaseStatus, "", "open", "sealed") || len(f.Search) > 200 || strings.ContainsRune(f.Search, 0) ||
		(!one(f.Assignee, "", "me", "unassigned", "inactive") && !subjectPattern.MatchString(f.Assignee)) {
		return f, ErrInvalid
	}
	if f.Period == "custom" {
		if f.From == nil || f.To == nil || f.From.IsZero() || f.To.IsZero() || !f.From.Before(*f.To) {
			return f, ErrInvalid
		}
		a, b := f.From.UTC(), f.To.UTC()
		f.From, f.To = &a, &b
	} else if f.From != nil || f.To != nil {
		return f, ErrInvalid
	}
	return f, nil
}
func (f QueueFilters) Window(at time.Time) (time.Time, time.Time) {
	if f.Period == "custom" {
		return *f.From, *f.To
	}
	d := 24 * time.Hour
	if f.Period == "7d" {
		d *= 7
	}
	return at.Add(-d), at
}
func FindingProgressState(a Assessment) string {
	if a.Progress == nil {
		return "unrecorded"
	}
	return a.Progress.State
}
func FindingIsOverdue(a Assessment, f Followup, at time.Time) bool {
	return FindingProgressState(a) != "ended" && f.DueAt != nil && f.DueAt.Before(at)
}
func FindingBasisState(review, progress BasisEvaluation) string {
	for _, state := range []string{"unavailable", "changed", "unchanged"} {
		if review.State == state || progress.State == state {
			return state
		}
	}
	return "unrecorded"
}

// This comparison is shared by single Finding and batch queue reads.
func EvaluateReviewBasis(old *ReviewBasis, current ReviewBasis, err error) (BasisEvaluation, error) {
	if old == nil {
		return BasisEvaluation{State: "unrecorded"}, nil
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalid) {
		return BasisEvaluation{State: "unavailable"}, nil
	}
	if err != nil {
		return BasisEvaluation{}, err
	}
	state := "unchanged"
	if old.Digest != current.Digest {
		state = "changed"
	}
	return BasisEvaluation{State: state, Current: &current}, nil
}

type QueueAccess struct {
	Subject        string
	Revision       string
	AssigneeStates map[string]string
}
type QueueRequest struct {
	Filters QueueFilters
	Limit   int
	Cursor  string
}
type QueueItem struct {
	ResourceUpdates   []ResourceUpdate `json:"resource_updates,omitempty"`
	Finding           Finding          `json:"finding"`
	CaseTitle         string           `json:"case_title"`
	CaseStatus        string           `json:"case_status"`
	AssessmentVersion int64            `json:"assessment_version"`
	Followup          Followup         `json:"followup"`
	Progress          string           `json:"progress"`
	Conclusion        string           `json:"conclusion"`
	Basis             string           `json:"basis"`
	ReviewBasis       string           `json:"review_basis"`
	ProgressBasis     string           `json:"progress_basis"`
	BasisRevision     string           `json:"basis_revision"`
	Overdue           bool             `json:"overdue"`
	AssigneeState     string           `json:"assignee_state"`
}
type QueueStatistics struct {
	Current          int `json:"current"`
	Created          int `json:"created"`
	Reassessment     int `json:"reassessment"`
	Unassigned       int `json:"unassigned"`
	Overdue          int `json:"overdue"`
	BasisUnrecorded  int `json:"basis_unrecorded"`
	BasisChanged     int `json:"basis_changed"`
	BasisUnavailable int `json:"basis_unavailable"`
}
type QueuePage struct {
	Items       []QueueItem     `json:"items"`
	Total       int             `json:"total"`
	Statistics  QueueStatistics `json:"statistics"`
	EvaluatedAt time.Time       `json:"evaluated_at"`
	Version     string          `json:"version"`
	Filters     QueueFilters    `json:"filters"`
	NextCursor  string          `json:"next_cursor,omitempty"`
	Coverage    string          `json:"coverage"`
}
type QueueFilter struct {
	ID           string       `json:"id"`
	OwnerSubject string       `json:"owner_subject"`
	Schema       string       `json:"schema_version"`
	Name         string       `json:"name"`
	Filters      QueueFilters `json:"filters"`
	Revision     int64        `json:"revision"`
	Deleted      bool         `json:"deleted"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

func (f *QueueFilter) UnmarshalJSON(raw []byte) error {
	type plain QueueFilter
	return decodeAssessmentFields(raw, (*plain)(f), "id", "owner_subject", "schema_version", "name", "filters", "revision", "deleted", "created_at", "updated_at")
}

type QueueFilterCommand struct {
	OperationID      string       `json:"operation_id"`
	ExpectedRevision int64        `json:"expected_revision"`
	Name             string       `json:"name"`
	Schema           string       `json:"schema_version"`
	Filters          QueueFilters `json:"filters"`
}

func (c *QueueFilterCommand) UnmarshalJSON(raw []byte) error {
	type plain QueueFilterCommand
	return decodeAssessmentFields(raw, (*plain)(c), "operation_id", "expected_revision", "name", "schema_version", "filters")
}
func NormalizeQueueFilterCommand(c QueueFilterCommand) (QueueFilterCommand, error) {
	c.Name = strings.TrimSpace(c.Name)
	if !uuidPattern.MatchString(c.OperationID) || c.ExpectedRevision < 0 || len(c.Name) == 0 || len(c.Name) > 120 || strings.ContainsRune(c.Name, 0) || c.Schema != QueueFilterSchema {
		return c, ErrInvalid
	}
	var err error
	c.Filters, err = NormalizeQueueFilters(c.Filters)
	return c, err
}
func ValidQueueFilterID(id string) bool {
	return len(id) == 26 && strings.HasPrefix(id, "xqf_") && assessmentFindingIDPattern.MatchString("xf_"+id[4:])
}
func SameQueueValue(a, b any) bool {
	x, e := json.Marshal(a)
	y, f := json.Marshal(b)
	return e == nil && f == nil && bytes.Equal(x, y)
}

type QueueStore interface {
	ListInvestigationQueue(context.Context, QueueAccess, QueueRequest, time.Time) (QueuePage, error)
	ListQueueFilters(context.Context, string) ([]QueueFilter, error)
	ApplyQueueFilter(context.Context, string, string, string, QueueFilterCommand, time.Time) (QueueFilter, error)
}

type QueueFilterReceipt struct {
	Kind     string             `json:"kind"`
	Command  QueueFilterCommand `json:"command"`
	Previous *QueueFilter       `json:"previous"`
	Current  QueueFilter        `json:"current"`
}

func (r *QueueFilterReceipt) UnmarshalJSON(raw []byte) error {
	type plain QueueFilterReceipt
	// A creation has a null previous value; presence is checked separately.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ErrInvalid
	}
	if _, ok := fields["previous"]; !ok {
		return ErrInvalid
	}
	return decodeAssessmentFields(raw, (*plain)(r), "kind", "command", "current")
}

func EvolveQueueFilter(previous *QueueFilter, subject, id, kind string, c QueueFilterCommand, now time.Time) (QueueFilter, error) {
	var item QueueFilter
	if _, err := HumanActor(subject); err != nil || !ValidQueueFilterID(id) || now.IsZero() {
		return item, ErrInvalid
	}
	var err error
	c, err = NormalizeQueueFilterCommand(c)
	if err != nil {
		return item, err
	}
	if kind == "create" {
		if previous != nil || c.ExpectedRevision != 0 {
			return item, ErrConflict
		}
		return QueueFilter{ID: id, OwnerSubject: subject, Schema: c.Schema, Name: c.Name, Filters: c.Filters, Revision: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}, nil
	}
	if kind != "update" && kind != "delete" {
		return item, ErrInvalid
	}
	if previous == nil || previous.ID != id || previous.OwnerSubject != subject {
		return item, ErrNotFound
	}
	if previous.Deleted || previous.Revision != c.ExpectedRevision {
		return item, ErrConflict
	}
	item = *previous
	item.Revision++
	item.UpdatedAt = now.UTC()
	if kind == "delete" {
		item.Deleted = true
	} else {
		item.Name = c.Name
		item.Filters = c.Filters
	}
	return item, nil
}
