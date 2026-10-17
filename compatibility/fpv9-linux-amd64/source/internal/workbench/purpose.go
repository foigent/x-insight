package workbench

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// This vocabulary is a bounded development contract, not a frozen enterprise
// taxonomy. Business domains and technical operations are separate dimensions.
const PurposeVocabulary = "xinsight.request-purpose.dev.v1"
const PurposeRule = "xinsight.request-purpose-reading.v1"
const MaxPurposeClaims = 256

var purposeClaimID = regexp.MustCompile(`^xpc_[A-Za-z0-9_-]{22}$`)
var purposeEventID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var purposeHash = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func ValidPurposeCategory(category string) bool {
	return category == "information_query" || category == "content_transformation" || category == "software_check"
}

type PurposeBasis struct {
	EvidenceID string `json:"evidence_event_id"`
	EventHash  string `json:"event_hash,omitempty"`
}

type PurposeCommand struct {
	Action           string         `json:"action"`
	ExpectedRevision int64          `json:"expected_revision"`
	IdempotencyKey   string         `json:"idempotency_key"`
	ClaimID          string         `json:"claim_id,omitempty"`
	Category         string         `json:"category,omitempty"`
	Vocabulary       string         `json:"vocabulary,omitempty"`
	Basis            []PurposeBasis `json:"basis_refs,omitempty"`
	Reason           string         `json:"reason"`
	RejectClaims     []string       `json:"reject_claim_ids,omitempty"`
	Tags             []string       `json:"tags,omitempty"`
}

// Enforce presence, null and action-specific fields as well as Go zero values.
// The HTTP decoder separately rejects duplicate keys before invoking this.
func (c *PurposeCommand) UnmarshalJSON(raw []byte) error {
	type wire PurposeCommand
	var value wire
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return ErrInvalid
	}
	for _, key := range []string{"action", "expected_revision", "idempotency_key", "reason"} {
		if _, ok := fields[key]; !ok {
			return ErrInvalid
		}
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalid
		}
	}
	if value.Action == "declare" {
		if _, ok := fields["claim_id"]; ok {
			return ErrInvalid
		}
	}
	if value.Action != "declare" && value.Action != "modify" {
		for _, key := range []string{"category", "vocabulary", "basis_refs", "tags"} {
			if _, ok := fields[key]; ok {
				return ErrInvalid
			}
		}
	}
	if value.Action != "accept" && value.Action != "modify" {
		if _, ok := fields["reject_claim_ids"]; ok {
			return ErrInvalid
		}
	}
	*c = PurposeCommand(value)
	return ValidatePurposeCommand(*c)
}

type PurposeClaim struct {
	ID          string         `json:"claim_id"`
	Category    string         `json:"category"`
	Vocabulary  string         `json:"vocabulary"`
	Method      string         `json:"method"`
	Basis       []PurposeBasis `json:"basis_refs"`
	Reason      string         `json:"reason"`
	DeclaredBy  string         `json:"declared_by"`
	DeclaredAt  time.Time      `json:"declared_at"`
	ReviewState string         `json:"review_state"`
	Revises     string         `json:"revises,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
}

type PurposeState struct {
	Revision     int64          `json:"revision"`
	Claims       []PurposeClaim `json:"claims"`
	AdoptedClaim string         `json:"adopted_claim_id,omitempty"`
	Status       string         `json:"status"`
	Category     string         `json:"category,omitempty"`
}

type PurposeEvent struct {
	ExecutionID  string         `json:"execution_id"`
	Revision     int64          `json:"revision"`
	Actor        string         `json:"actor"`
	RecordedAt   time.Time      `json:"recorded_at"`
	Command      PurposeCommand `json:"command"`
	NewClaimID   string         `json:"new_claim_id,omitempty"`
	PreviousHash string         `json:"previous_hash"`
	State        PurposeState   `json:"state"`
	Hash         string         `json:"event_hash,omitempty"`
}

type PurposeView struct {
	ExecutionID string                `json:"execution_id"`
	ReadingID   string                `json:"reading_id,omitempty"`
	State       PurposeState          `json:"state"`
	History     []PurposeHistoryEntry `json:"history"`
	NextBefore  int64                 `json:"next_before_revision,omitempty"`
	CanReview   bool                  `json:"can_review"`
	Vocabulary  string                `json:"vocabulary"`
}

// History carries immutable commands and their hashes, not twenty repeated
// copies of the complete claim state. State is returned once at the selected revision.
type PurposeHistoryEntry struct {
	Revision     int64          `json:"revision"`
	Actor        string         `json:"actor"`
	RecordedAt   time.Time      `json:"recorded_at"`
	Command      PurposeCommand `json:"command"`
	NewClaimID   string         `json:"new_claim_id,omitempty"`
	PreviousHash string         `json:"previous_hash"`
	Hash         string         `json:"event_hash"`
}

func (e PurposeEvent) HistoryEntry() PurposeHistoryEntry {
	return PurposeHistoryEntry{Revision: e.Revision, Actor: e.Actor, RecordedAt: e.RecordedAt, Command: e.Command, NewClaimID: e.NewClaimID, PreviousHash: e.PreviousHash, Hash: e.Hash}
}

type PurposeReading struct {
	Status      string `json:"status"`
	RuleVersion string `json:"rule_version"`
	ManifestRef string `json:"manifest_ref"`
}

func EmptyPurposeState() PurposeState {
	return PurposeState{Claims: []PurposeClaim{}, Status: "unknown"}
}

func ValidatePurposeCommand(c PurposeCommand) error {
	if c.ExpectedRevision < 0 || c.ExpectedRevision >= 9007199254740991 || !idempotencyKeyPattern.MatchString(c.IdempotencyKey) || strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 4000 || len(c.RejectClaims) > MaxPurposeClaims {
		return ErrInvalid
	}
	for _, r := range c.Reason {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return ErrInvalid
		}
	}
	creates := c.Action == "declare" || c.Action == "modify"
	if creates {
		if !ValidPurposeCategory(c.Category) || c.Vocabulary != PurposeVocabulary || len(c.Basis) == 0 || len(c.Basis) > 32 || len(c.Tags) > 3 {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for _, b := range c.Basis {
			if !purposeEventID.MatchString(b.EvidenceID) || !purposeHash.MatchString(b.EventHash) || seen[b.EvidenceID] {
				return ErrInvalid
			}
			seen[b.EvidenceID] = true
		}
		seen = map[string]bool{}
		for _, tag := range c.Tags {
			if !ValidPurposeCategory(tag) || seen[tag] {
				return ErrInvalid
			}
			seen[tag] = true
		}
	} else if c.Category != "" || c.Vocabulary != "" || len(c.Basis) != 0 || len(c.Tags) != 0 {
		return ErrInvalid
	}
	if c.Action == "declare" {
		if c.ClaimID != "" || len(c.RejectClaims) != 0 {
			return ErrInvalid
		}
	} else if !purposeClaimID.MatchString(c.ClaimID) {
		return ErrInvalid
	}
	if c.Action != "accept" && c.Action != "modify" && len(c.RejectClaims) != 0 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, id := range c.RejectClaims {
		if !purposeClaimID.MatchString(id) || id == c.ClaimID || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	switch c.Action {
	case "declare", "accept", "modify", "reject", "withdraw_declaration", "withdraw_adoption":
		return nil
	}
	return ErrInvalid
}

// ApplyPurpose is the sole adoption/state transition function. Restore replays
// the same immutable commands; the UI never derives a different current state.
func ApplyPurpose(previous PurposeState, c PurposeCommand, actor, newID string, at time.Time) (PurposeState, error) {
	if err := ValidatePurposeCommand(c); err != nil {
		return PurposeState{}, err
	}
	if c.ExpectedRevision != previous.Revision {
		return PurposeState{}, ErrStaleRevision
	}
	if actor == "" || at.IsZero() {
		return PurposeState{}, ErrInvalid
	}
	encoded, _ := json.Marshal(previous)
	var next PurposeState
	_ = json.Unmarshal(encoded, &next)
	if next.Claims == nil {
		next.Claims = []PurposeClaim{}
	}
	find := func(id string) int {
		return slices.IndexFunc(next.Claims, func(p PurposeClaim) bool { return p.ID == id })
	}
	index := find(c.ClaimID)
	if c.Action != "declare" && index < 0 {
		return PurposeState{}, ErrInvalid
	}
	if c.Action == "declare" || c.Action == "modify" {
		if !purposeClaimID.MatchString(newID) || find(newID) >= 0 {
			return PurposeState{}, ErrInvalid
		}
		if len(next.Claims) >= MaxPurposeClaims {
			return PurposeState{}, ErrTooLarge
		}
	}
	for _, id := range c.RejectClaims {
		i := find(id)
		if i < 0 || next.Claims[i].ReviewState != "pending" {
			return PurposeState{}, ErrInvalid
		}
	}
	if c.Action != "declare" && c.Action != "withdraw_adoption" && next.Claims[index].ReviewState != "pending" && next.Claims[index].ReviewState != "accepted" {
		return PurposeState{}, ErrInvalid
	}
	switch c.Action {
	case "declare", "modify":
		status := "pending"
		if c.Action == "modify" {
			next.Claims[index].ReviewState = "modified"
			status = "accepted"
			next.AdoptedClaim = newID
		}
		next.Claims = append(next.Claims, PurposeClaim{ID: newID, Category: c.Category, Vocabulary: c.Vocabulary, Method: "human_declared", Basis: c.Basis, Reason: c.Reason, DeclaredBy: actor, DeclaredAt: at.UTC(), ReviewState: status, Revises: c.ClaimID, Tags: c.Tags})
	case "accept":
		next.Claims[index].ReviewState = "accepted"
		next.AdoptedClaim = c.ClaimID
	case "reject":
		if next.Claims[index].ReviewState != "pending" {
			return PurposeState{}, ErrInvalid
		}
		next.Claims[index].ReviewState = "rejected"
	case "withdraw_declaration":
		next.Claims[index].ReviewState = "withdrawn"
		if next.AdoptedClaim == c.ClaimID {
			next.AdoptedClaim = ""
		}
	case "withdraw_adoption":
		if next.AdoptedClaim != c.ClaimID {
			return PurposeState{}, ErrInvalid
		}
		next.AdoptedClaim = ""
	}
	for _, id := range c.RejectClaims {
		next.Claims[find(id)].ReviewState = "rejected"
	}
	next.Revision++
	next.Status, next.Category = "unknown", ""
	values := map[string]bool{}
	for _, claim := range next.Claims {
		if claim.ReviewState == "pending" {
			values[claim.Category] = true
			next.Status = "unreviewed"
		}
		if claim.ID == next.AdoptedClaim && claim.ReviewState == "accepted" {
			next.Category = claim.Category
			values[claim.Category] = true
		}
	}
	if len(values) > 1 {
		next.Status, next.Category = "conflict", ""
	} else if next.AdoptedClaim != "" {
		next.Status = "adopted"
	}
	return next, nil
}
