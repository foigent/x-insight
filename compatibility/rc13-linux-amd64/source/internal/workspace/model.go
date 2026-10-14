package workspace

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/executionid"
)

const (
	SchemaVersion = "xinsight.investigation-workspace.v1"

	MaxCaseTitleBytes        = 160
	MaxCaseDescriptionBytes  = 4000
	MaxFindingTitleBytes     = 160
	MaxFindingStatementBytes = 8000
	MaxAnnotationBytes       = 4000
	MaxSavedViewNameBytes    = 120
	MaxSavedViewFilterBytes  = 8192
)

var (
	ErrNotFound          = errors.New("workspace record not found")
	ErrConflict          = errors.New("workspace record conflict")
	ErrSealed            = errors.New("investigation case is sealed")
	ErrInvalid           = errors.New("workspace input is invalid")
	ErrCredentialInvalid = errors.New("service credential is invalid")
	ErrCredentialExpired = errors.New("service credential is expired")
	ErrCredentialRevoked = errors.New("service credential is revoked")

	subjectPattern     = regexp.MustCompile(`^xs_[a-z0-9]{16,64}$`)
	executionIDPattern = regexp.MustCompile(`^xe1_(?:[a-f0-9]{64}|[A-Za-z0-9_-]{8,128})$`)
	hashPattern        = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	uuidPattern        = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
)

type Actor struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

func HumanActor(subject string) (Actor, error) {
	if !subjectPattern.MatchString(subject) {
		return Actor{}, fmt.Errorf("%w: human subject", ErrInvalid)
	}
	return Actor{Kind: "human", Ref: subject}, nil
}

type Case struct {
	ID               string          `json:"id"`
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	Status           string          `json:"status"`
	CreatedBySubject string          `json:"created_by_subject"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	Version          int64           `json:"version"`
	SealedAt         *time.Time      `json:"sealed_at,omitempty"`
	SealedBySubject  string          `json:"sealed_by_subject,omitempty"`
	SealedRevision   *int64          `json:"sealed_revision,omitempty"`
	SealedDigest     string          `json:"sealed_digest,omitempty"`
	SealedPack       json.RawMessage `json:"-"`
}

type CaseExecution struct {
	CaseID         string    `json:"case_id"`
	ExecutionID    string    `json:"execution_id"`
	AsOf           time.Time `json:"as_of"`
	AddedBySubject string    `json:"added_by_subject"`
	AddedAt        time.Time `json:"added_at"`
}

type Finding struct {
	ID               string    `json:"id"`
	CaseID           string    `json:"case_id"`
	Title            string    `json:"title"`
	Statement        string    `json:"statement"`
	Classification   string    `json:"classification"`
	Status           string    `json:"status"`
	CreatedBySubject string    `json:"created_by_subject"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Version          int64     `json:"version"`
}

type EvidenceLink struct {
	CaseID            string    `json:"case_id"`
	FindingID         string    `json:"finding_id,omitempty"`
	EvidenceEventID   string    `json:"evidence_event_id"`
	Relation          string    `json:"relation"`
	EventHashSnapshot string    `json:"event_hash"`
	SourceRefSnapshot string    `json:"source_ref"`
	AddedBySubject    string    `json:"added_by_subject"`
	AddedAt           time.Time `json:"added_at"`
}

type GapLink struct {
	CaseID                string    `json:"case_id"`
	FindingID             string    `json:"finding_id,omitempty"`
	GapID                 string    `json:"gap_id"`
	Relation              string    `json:"relation"`
	AffectedClaimSnapshot string    `json:"affected_claim"`
	GapTypeSnapshot       string    `json:"gap_type"`
	AddedBySubject        string    `json:"added_by_subject"`
	AddedAt               time.Time `json:"added_at"`
}

type Annotation struct {
	ID               string    `json:"id"`
	CaseID           string    `json:"case_id"`
	TargetKind       string    `json:"target_kind"`
	TargetRef        string    `json:"target_ref"`
	Body             string    `json:"body"`
	CreatedBySubject string    `json:"created_by_subject"`
	CreatedAt        time.Time `json:"created_at"`
}

type CaseRecord struct {
	Case        Case            `json:"case"`
	Executions  []CaseExecution `json:"executions"`
	Findings    []Finding       `json:"findings"`
	Evidence    []EvidenceLink  `json:"evidence"`
	Gaps        []GapLink       `json:"gaps"`
	Alerts      []AlertCaseLink `json:"alerts"`
	Annotations []Annotation    `json:"annotations"`
}

type SavedView struct {
	ID           string          `json:"id"`
	OwnerSubject string          `json:"owner_subject"`
	Name         string          `json:"name"`
	Filters      json.RawMessage `json:"filters"`
	Presentation string          `json:"presentation"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	Version      int64           `json:"version"`
}

type GapResolution struct {
	GapID            string    `json:"gap_id"`
	State            string    `json:"state"`
	Note             string    `json:"note"`
	UpdatedBySubject string    `json:"updated_by_subject"`
	UpdatedAt        time.Time `json:"updated_at"`
	Version          int64     `json:"version"`
}

type AlertCaseLink struct {
	AlertID         string    `json:"alert_id"`
	CaseID          string    `json:"case_id"`
	AlertState      string    `json:"alert_state"`
	LinkedBySubject string    `json:"linked_by_subject"`
	LinkedAt        time.Time `json:"linked_at"`
}

type CreateCaseInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type AddExecutionInput struct {
	ExecutionID string    `json:"execution_id"`
	AsOf        time.Time `json:"as_of"`
}

type CreateFindingInput struct {
	Title          string `json:"title"`
	Statement      string `json:"statement"`
	Classification string `json:"classification"`
}

type AddEvidenceInput struct {
	FindingID       string `json:"finding_id,omitempty"`
	EvidenceEventID string `json:"evidence_event_id"`
	Relation        string `json:"relation"`
	EventHash       string `json:"event_hash"`
	SourceRef       string `json:"source_ref"`
}

type AddGapInput struct {
	FindingID     string `json:"finding_id,omitempty"`
	GapID         string `json:"gap_id"`
	Relation      string `json:"relation"`
	AffectedClaim string `json:"affected_claim"`
	GapType       string `json:"gap_type"`
}

type CreateAnnotationInput struct {
	TargetKind string `json:"target_kind"`
	TargetRef  string `json:"target_ref"`
	Body       string `json:"body"`
}

type CreateSavedViewInput struct {
	Name         string          `json:"name"`
	Filters      json.RawMessage `json:"filters"`
	Presentation string          `json:"presentation"`
}

type ResolveGapInput struct {
	State string `json:"state"`
	Note  string `json:"note"`
}

type Store interface {
	ListCases(context.Context, int) ([]Case, error)
	GetCase(context.Context, string) (CaseRecord, error)
	CreateCase(context.Context, Actor, CreateCaseInput, time.Time, io.Reader) (Case, error)
	AddCaseExecution(context.Context, string, Actor, AddExecutionInput, time.Time) (CaseExecution, error)
	CreateFinding(context.Context, string, Actor, CreateFindingInput, time.Time, io.Reader) (Finding, error)
	AddEvidenceLink(context.Context, string, Actor, AddEvidenceInput, time.Time) (EvidenceLink, error)
	AddGapLink(context.Context, string, Actor, AddGapInput, time.Time) (GapLink, error)
	CreateAnnotation(context.Context, string, Actor, CreateAnnotationInput, time.Time, io.Reader) (Annotation, error)
	SealCase(context.Context, string, Actor, time.Time) (Case, error)
	ListSavedViews(context.Context, string, int) ([]SavedView, error)
	CreateSavedView(context.Context, Actor, CreateSavedViewInput, time.Time, io.Reader) (SavedView, error)
	ResolveGap(context.Context, Actor, string, ResolveGapInput, time.Time) (GapResolution, error)
	LinkAlertToCase(context.Context, Actor, string, string, time.Time) (AlertCaseLink, error)
}

func ValidateCreateCase(input CreateCaseInput) error {
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || len(input.Title) > MaxCaseTitleBytes ||
		len(input.Description) > MaxCaseDescriptionBytes {
		return ErrInvalid
	}
	return nil
}

func ValidateAddExecution(input AddExecutionInput) error {
	if !executionIDPattern.MatchString(input.ExecutionID) ||
		input.AsOf.IsZero() ||
		input.AsOf.Location() != time.UTC {
		return ErrInvalid
	}
	return nil
}

func ValidateCreateFinding(input CreateFindingInput) error {
	title := strings.TrimSpace(input.Title)
	statement := strings.TrimSpace(input.Statement)
	if title == "" || statement == "" ||
		len(title) > MaxFindingTitleBytes ||
		len(statement) > MaxFindingStatementBytes {
		return ErrInvalid
	}
	switch input.Classification {
	case "evidence_backed", "deterministic_derived", "hypothesis", "unknown":
		return nil
	default:
		return ErrInvalid
	}
}

func ValidateAddEvidence(input AddEvidenceInput) error {
	if !uuidPattern.MatchString(input.EvidenceEventID) ||
		(input.Relation != "supports" && input.Relation != "contradicts" && input.Relation != "context") ||
		!hashPattern.MatchString(input.EventHash) ||
		len(input.SourceRef) == 0 || len(input.SourceRef) > 2048 {
		return ErrInvalid
	}
	return nil
}

func ValidateAddGap(input AddGapInput) error {
	if !uuidPattern.MatchString(input.GapID) ||
		(input.Relation != "limits" && input.Relation != "blocks" && input.Relation != "context") ||
		len(input.AffectedClaim) == 0 || len(input.AffectedClaim) > 2048 ||
		len(input.GapType) == 0 || len(input.GapType) > 256 {
		return ErrInvalid
	}
	return nil
}

func ValidateAnnotation(input CreateAnnotationInput) error {
	switch input.TargetKind {
	case "case", "execution", "finding", "evidence", "gap":
	default:
		return ErrInvalid
	}
	if len(input.TargetRef) == 0 || len(input.TargetRef) > 256 ||
		len(strings.TrimSpace(input.Body)) == 0 || len(input.Body) > MaxAnnotationBytes {
		return ErrInvalid
	}
	return nil
}

func ValidateSavedView(input CreateSavedViewInput) error {
	filterText := strings.TrimSpace(string(input.Filters))
	if len(strings.TrimSpace(input.Name)) == 0 || len(input.Name) > MaxSavedViewNameBytes ||
		len(input.Filters) == 0 || len(input.Filters) > MaxSavedViewFilterBytes ||
		!json.Valid(input.Filters) || len(filterText) < 2 || filterText[0] != '{' {
		return ErrInvalid
	}
	type savedViewFilters struct {
		RequestID      *string `json:"request_id"`
		RequestPrefix  *string `json:"request_id_prefix"`
		SourceSystemID *string `json:"source_system_id"`
		Outcome        *string `json:"outcome"`
		HasGaps        *bool   `json:"has_gaps"`
		From           *string `json:"from"`
		To             *string `json:"to"`
	}
	decoder := json.NewDecoder(strings.NewReader(filterText))
	decoder.DisallowUnknownFields()
	var filters savedViewFilters
	if err := decoder.Decode(&filters); err != nil {
		return ErrInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	var rawFilters map[string]json.RawMessage
	if err := json.Unmarshal(input.Filters, &rawFilters); err != nil {
		return ErrInvalid
	}
	for _, raw := range rawFilters {
		if strings.TrimSpace(string(raw)) == "null" {
			return ErrInvalid
		}
	}
	if filters.RequestID != nil && filters.RequestPrefix != nil {
		return ErrInvalid
	}
	if filters.RequestID != nil {
		if err := executionid.ValidateRequestID(*filters.RequestID); err != nil {
			return ErrInvalid
		}
	}
	if filters.RequestPrefix != nil {
		if err := executionid.ValidateRequestIDPrefix(*filters.RequestPrefix); err != nil {
			return ErrInvalid
		}
	}
	if filters.SourceSystemID != nil {
		if _, err := executionid.CanonicalSourceSystemID(*filters.SourceSystemID); err != nil {
			return ErrInvalid
		}
	}
	if filters.Outcome != nil {
		switch *filters.Outcome {
		case "success", "failure", "partial", "unknown":
		default:
			return ErrInvalid
		}
	}
	from, fromPresent, err := savedViewUTC(filters.From)
	if err != nil {
		return ErrInvalid
	}
	to, toPresent, err := savedViewUTC(filters.To)
	if err != nil {
		return ErrInvalid
	}
	if fromPresent && toPresent &&
		(!from.Before(to) || to.Sub(from) > 31*24*time.Hour) {
		return ErrInvalid
	}
	switch input.Presentation {
	case "inbox", "explorer", "story", "causal_graph", "chronology", "proof":
		return nil
	default:
		return ErrInvalid
	}
}

func savedViewUTC(raw *string) (time.Time, bool, error) {
	if raw == nil {
		return time.Time{}, false, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, *raw)
	if err != nil {
		return time.Time{}, true, err
	}
	_, offset := parsed.Zone()
	if offset != 0 {
		return time.Time{}, true, ErrInvalid
	}
	return parsed.UTC(), true, nil
}

func ValidateResolveGap(input ResolveGapInput) error {
	switch input.State {
	case "open", "investigating", "source_fixed", "accepted_limitation", "superseded":
	default:
		return ErrInvalid
	}
	if len(input.Note) > 4000 {
		return ErrInvalid
	}
	return nil
}

func NewID(prefix string, source io.Reader) (string, error) {
	switch prefix {
	case "xc_", "xf_", "xn_", "xv_", "xsi_", "xaa_":
	default:
		return "", ErrInvalid
	}
	if source == nil {
		source = rand.Reader
	}
	var value [16]byte
	if _, err := io.ReadFull(source, value[:]); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(value[:]), nil
}
