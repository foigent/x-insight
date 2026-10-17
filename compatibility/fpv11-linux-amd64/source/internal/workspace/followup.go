package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Responsibility is a human assignment, independent of Finding classification,
// review conclusions, source control facts, and the assignee's access rights.
type Responsibility struct {
	AssigneeSubjectID *string    `json:"assignee_subject_id"`
	AssigneeName      string     `json:"assignee_name"`
	DueAt             *time.Time `json:"due_at"`
	DueTimezone       *string    `json:"due_timezone"`
}

type Followup struct {
	CaseID    string `json:"case_id"`
	FindingID string `json:"finding_id"`
	Recorded  bool   `json:"recorded"`
	Version   int64  `json:"version"`
	Responsibility
	UpdatedAt        *time.Time `json:"updated_at"`
	UpdatedBySubject string     `json:"updated_by_subject"`
}

type FollowupPayload struct {
	AssigneeSubjectID *string    `json:"assignee_subject_id"`
	DueAt             *time.Time `json:"due_at"`
	DueTimezone       *string    `json:"due_timezone"`
	Reason            string     `json:"reason"`
}

func (p *FollowupPayload) UnmarshalJSON(raw []byte) error {
	type plain FollowupPayload
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ErrInvalid
	}
	for _, key := range []string{"assignee_subject_id", "due_at", "due_timezone", "reason"} {
		if _, ok := fields[key]; !ok {
			return ErrInvalid
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode((*plain)(p))
}

type FollowupCommand struct {
	Operation           string          `json:"operation"`
	OperationID         string          `json:"operation_id"`
	ExpectedVersion     int64           `json:"expected_version"`
	ExpectedCaseVersion int64           `json:"expected_case_version"`
	Payload             FollowupPayload `json:"payload"`
}

func (c *FollowupCommand) UnmarshalJSON(raw []byte) error {
	type plain FollowupCommand
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ErrInvalid
	}
	for _, key := range []string{"operation", "operation_id", "expected_version", "expected_case_version", "payload"} {
		if value, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalid
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode((*plain)(c))
}

func NormalizeFollowupCommand(c FollowupCommand) (FollowupCommand, error) {
	if c.Operation != "update_followup" || !uuidPattern.MatchString(c.OperationID) || c.ExpectedVersion < 0 || c.ExpectedCaseVersion < 1 {
		return c, ErrInvalid
	}
	c.Payload.Reason = strings.TrimSpace(c.Payload.Reason)
	if len(c.Payload.Reason) == 0 || len(c.Payload.Reason) > 4000 || strings.ContainsRune(c.Payload.Reason, 0) {
		return c, ErrInvalid
	}
	if p := c.Payload.AssigneeSubjectID; p != nil && !subjectPattern.MatchString(*p) {
		return c, ErrInvalid
	}
	if (c.Payload.DueAt == nil) != (c.Payload.DueTimezone == nil) {
		return c, ErrInvalid
	}
	if c.Payload.DueAt != nil {
		if c.Payload.DueAt.IsZero() || len(*c.Payload.DueTimezone) == 0 || len(*c.Payload.DueTimezone) > 128 || *c.Payload.DueTimezone == "Local" {
			return c, ErrInvalid
		}
		loc, err := time.LoadLocation(*c.Payload.DueTimezone)
		if err != nil {
			return c, ErrInvalid
		}
		_, supplied := c.Payload.DueAt.Zone()
		_, expected := c.Payload.DueAt.In(loc).Zone()
		if supplied != expected {
			return c, ErrInvalid
		}
		// PostgreSQL preserves microseconds; canonical retries use the same precision.
		at := c.Payload.DueAt.In(loc).Truncate(time.Microsecond)
		c.Payload.DueAt = &at
	}
	return c, nil
}

// AssigneeLookup is a non-secret snapshot from the already-loaded local provider.
// New assignments validate it only after looking for a committed retry receipt.
type AssigneeLookup struct {
	SubjectID string
	Name      string
	Active    bool
}

type FollowupAction struct {
	OperationID         string    `json:"operation_id"`
	Operation           string    `json:"operation"`
	ActorSubjectID      string    `json:"actor_subject_id"`
	At                  time.Time `json:"at"`
	ExpectedVersion     int64     `json:"expected_version"`
	ExpectedCaseVersion int64     `json:"expected_case_version"`
	CaseVersion         int64     `json:"case_version"`
	Reason              string    `json:"reason"`
	Previous            Followup  `json:"previous"`
	Current             Followup  `json:"current"`
}

type FollowupHistory struct {
	Items          []FollowupAction `json:"items"`
	ThroughVersion int64            `json:"through_version"`
	NextBefore     *int64           `json:"next_before_version"`
}

type FollowupReview struct {
	EvaluatedAt    time.Time       `json:"evaluated_at"`
	Followup       Followup        `json:"followup"`
	CaseVersion    int64           `json:"case_version"`
	CaseStatus     string          `json:"case_status"`
	History        FollowupHistory `json:"history"`
	AllowedActions []string        `json:"allowed_actions"`
	AssigneeState  string          `json:"assignee_state"`
}

type FollowupSnapshot struct {
	Followups []Followup       `json:"followups"`
	Actions   []FollowupAction `json:"actions"`
}

type FollowupStore interface {
	GetFindingReview(context.Context, string, string) (FollowupReview, error)
	GetFindingHistory(context.Context, string, string, int64, int64, int) (FollowupHistory, error)
	ApplyFindingFollowup(context.Context, string, string, Actor, FollowupCommand, AssigneeLookup, time.Time) (FollowupAction, error)
}
