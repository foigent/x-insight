package connectorcontrol

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/connector"
)

const (
	ControlOperationSchemaV3     = "xinsight.connector-control-operation.v3"
	ControlResultSchemaV3        = "xinsight.connector-control-result.v3"
	KindAdoptExistingReceiver    = "adopt_existing_receiver"
	KindPairMachine              = "pair_machine"
	PhaseBindExistingReceiver    = "bind_existing_receiver"
	PhaseConfirmExistingReceiver = "confirm_existing_receiver"
	PhaseDeliverMachineCandidate = "deliver_machine_candidate"
	PolicyMachinePurpose         = "xinsight.policy.manage"
	EnrollmentMachinePurpose     = "xinsight.source.enroll"
)

type ControlOperationRequestV3 struct {
	SchemaVersion           string                `json:"schema_version"`
	OperationID             string                `json:"operation_id"`
	InstallationID          string                `json:"installation_id"`
	ProjectID               string                `json:"project_id"`
	EventScope              []string              `json:"event_scope"`
	DesiredState            string                `json:"desired_state"`
	Kind                    string                `json:"kind"`
	Phase                   string                `json:"phase"`
	PredecessorOperationID  string                `json:"predecessor_operation_id,omitempty"`
	PredecessorResultSHA256 string                `json:"predecessor_result_sha256,omitempty"`
	Request                 ControlPhaseRequestV3 `json:"request"`
}

type ExistingReceiverCandidate struct {
	SourceInstance       string                       `json:"source_instance"`
	SourceBindingSHA256  string                       `json:"source_binding_sha256"`
	FeedKeyID            string                       `json:"feed_key_id"`
	ReceiverRegistration PublicReceiverRegistrationV1 `json:"receiver_registration"`
	AllowedIP            string                       `json:"allowed_ip"`
	WorkerConfig         ExistingWorkerConfig         `json:"worker_config"`
	Checkpoint           ExistingCheckpoint           `json:"checkpoint"`
}
type ExistingWorkerConfig struct {
	WorkerUnit                 string `json:"worker_unit"`
	UROrigin                   string `json:"ur_origin"`
	FeedRoute                  string `json:"feed_route"`
	SourceInstance             string `json:"source_instance"`
	FeedSchema                 string `json:"feed_schema"`
	CursorKey                  string `json:"cursor_key"`
	ReceiverRegistrationSHA256 string `json:"receiver_registration_sha256"`
}
type ExistingCheckpoint struct {
	StateID                string `json:"state_id"`
	ConsumerEpoch          int64  `json:"consumer_epoch"`
	EpochOperationID       string `json:"epoch_operation_id"`
	AuthorizationRefSHA256 string `json:"authorization_ref_sha256"`
	CursorSHA256           string `json:"cursor_sha256"`
	HighWatermarkSHA256    string `json:"high_watermark_sha256"`
}
type ReceiverAdoptionBinding struct {
	InstallationID  string `json:"installation_id"`
	OrgID           int64  `json:"org_id"`
	ProjectID       int64  `json:"project_id"`
	OperationID     string `json:"operation_id"`
	FeedKeyID       string `json:"feed_key_id"`
	CandidateSHA256 string `json:"candidate_sha256"`
}
type XIAdoptionReceipt struct {
	InstallationID               string    `json:"installation_id"`
	BindOperationID              string    `json:"bind_operation_id"`
	BindResultSHA256             string    `json:"bind_result_sha256"`
	CandidateSHA256              string    `json:"candidate_sha256"`
	StateID                      string    `json:"state_id"`
	ConsumerEpoch                int64     `json:"consumer_epoch"`
	EpochOperationID             string    `json:"epoch_operation_id"`
	CommittedCursorSHA256        string    `json:"committed_cursor_sha256"`
	CommittedHighWatermarkSHA256 string    `json:"committed_high_watermark_sha256"`
	CommittedAt                  time.Time `json:"committed_at"`
	PagesCommittedBefore         int64     `json:"pages_committed_before"`
}
type FeedReadReceipt struct {
	FeedKeyID                  string    `json:"feed_key_id"`
	ReceiverRegistrationSHA256 string    `json:"receiver_registration_sha256"`
	WorkerConfigSHA256         string    `json:"worker_config_sha256"`
	CheckpointStateID          string    `json:"checkpoint_state_id"`
	ConsumerEpoch              int64     `json:"consumer_epoch"`
	EpochOperationID           string    `json:"epoch_operation_id"`
	StartedAt                  time.Time `json:"started_at"`
	CompletedAt                time.Time `json:"completed_at"`
	PagesCommitted             int64     `json:"pages_committed"`
	CursorSHA256               string    `json:"cursor_sha256"`
	HighWatermarkSHA256        string    `json:"high_watermark_sha256"`
	ResponseStatus             int       `json:"response_status"`
}
type PublicMachineRegistration struct {
	InstallationID string `json:"installation_id"`
	Purpose        string `json:"purpose"`
	KeyPrefix      string `json:"key_prefix"`
	VerifierSHA256 string `json:"verifier_sha256"`
	OperationID    string `json:"operation_id"`
	AllowedIP      string `json:"allowed_ip"`
}
type MachineCustodyReceipt struct {
	InstallationID     string `json:"installation_id"`
	Purpose            string `json:"purpose"`
	OperationID        string `json:"operation_id"`
	RegistrationSHA256 string `json:"registration_sha256"`
	CustodyState       string `json:"custody_state"`
}

// Phase-specific pointers preserve the exact closed object and distinguish absence.
type ControlPhaseRequestV3 struct {
	Candidate               *ExistingReceiverCandidate `json:"candidate,omitempty"`
	CandidateSHA256         string                     `json:"candidate_sha256,omitempty"`
	AdoptionBindingSHA256   string                     `json:"adoption_binding_sha256,omitempty"`
	XIAdoptionReceipt       *XIAdoptionReceipt         `json:"xi_adoption_receipt,omitempty"`
	XIAdoptionReceiptSHA256 string                     `json:"xi_adoption_receipt_sha256,omitempty"`
	FeedReadReceipt         *FeedReadReceipt           `json:"feed_read_receipt,omitempty"`
	FeedReadReceiptSHA256   string                     `json:"feed_read_receipt_sha256,omitempty"`
	SourceInstance          string                     `json:"source_instance,omitempty"`
	SourceBindingSHA256     string                     `json:"source_binding_sha256,omitempty"`
	Registration            *PublicMachineRegistration `json:"registration,omitempty"`
	RegistrationSHA256      string                     `json:"registration_sha256,omitempty"`
	CustodyReceipt          *MachineCustodyReceipt     `json:"custody_receipt,omitempty"`
	CustodyReceiptSHA256    string                     `json:"custody_receipt_sha256,omitempty"`
}
type ControlResultV3 struct {
	SchemaVersion  string                `json:"schema_version"`
	OperationID    string                `json:"operation_id"`
	InstallationID string                `json:"installation_id"`
	Kind           string                `json:"kind"`
	Phase          string                `json:"phase"`
	Status         string                `json:"status"`
	Result         *ControlPhaseResultV3 `json:"result,omitempty"`
	ReasonCode     string                `json:"reason_code,omitempty"`
	ObservedAt     time.Time             `json:"observed_at"`
}
type ControlPhaseResultV3 struct {
	SourceInstance          string                   `json:"source_instance"`
	CandidateSHA256         string                   `json:"candidate_sha256,omitempty"`
	AdoptionBinding         *ReceiverAdoptionBinding `json:"adoption_binding,omitempty"`
	AdoptionBindingSHA256   string                   `json:"adoption_binding_sha256,omitempty"`
	XIAdoptionReceiptSHA256 string                   `json:"xi_adoption_receipt_sha256,omitempty"`
	FeedReadReceiptSHA256   string                   `json:"feed_read_receipt_sha256,omitempty"`
	Purpose                 string                   `json:"purpose,omitempty"`
	RegistrationSHA256      string                   `json:"registration_sha256,omitempty"`
	CustodyReceiptSHA256    string                   `json:"custody_receipt_sha256,omitempty"`
	SourceState             string                   `json:"source_state,omitempty"`
	ReceiverState           string                   `json:"receiver_state,omitempty"`
	Readiness               string                   `json:"readiness"`
	Activation              string                   `json:"activation,omitempty"`
}

var v3UUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var v3Positive = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

func v3Source(s string) bool {
	return len(s) > 0 && len(s) <= 128 && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}
func v3Time(t time.Time) bool {
	return !t.IsZero() && t.Location() == time.UTC && t.Nanosecond()%1000 == 0
}
func v3IP(s string) bool {
	p, e := netip.ParsePrefix(s)
	return e == nil && p.String() == s && p.Bits() == p.Addr().BitLen() && !p.Addr().Is4In6() && !p.Addr().IsUnspecified() && !p.Addr().IsMulticast()
}
func MatchesCanonicalDigest(value any, digest string) bool {
	h, _, e := CanonicalSHA256(value)
	return e == nil && h == digest
}

// Shape equality enforces required zero-valued fields, absent conditional fields,
// exact integer spellings and canonical UTC times without requiring input key order.
func decodeV3(raw []byte, target any) error {
	if len(raw) > 65536 {
		return ErrInvalid
	}
	if _, e := connector.DecodeStrictJSONObject(raw); e != nil {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return ErrInvalid
	}
	canonical, e := json.Marshal(target)
	if e != nil {
		return ErrInvalid
	}
	var a, b any
	da := json.NewDecoder(bytes.NewReader(raw))
	da.UseNumber()
	db := json.NewDecoder(bytes.NewReader(canonical))
	db.UseNumber()
	if da.Decode(&a) != nil || db.Decode(&b) != nil || !reflect.DeepEqual(a, b) {
		return ErrInvalid
	}
	return nil
}
func DecodeControlOperationRequestV3(raw []byte) (ControlOperationRequestV3, error) {
	var v ControlOperationRequestV3
	if decodeV3(raw, &v) != nil || ValidateControlOperationRequestV3(v) != nil {
		return ControlOperationRequestV3{}, ErrInvalid
	}
	return v, nil
}
func DecodeControlResultV3(raw []byte, now time.Time) (ControlResultV3, error) {
	var v ControlResultV3
	if decodeV3(raw, &v) != nil || ValidateControlResultV3(v, now) != nil {
		return ControlResultV3{}, ErrInvalid
	}
	return v, nil
}
func ValidatePublicMachineRegistration(r PublicMachineRegistration) error {
	prefix := ""
	switch r.Purpose {
	case PolicyMachinePurpose:
		prefix = "ur-xpm1-"
	case EnrollmentMachinePurpose:
		prefix = "ur-xse1-"
	default:
		return ErrInvalid
	}
	if !IsInstallationID(r.InstallationID) || !regexp.MustCompile(`^`+prefix+`[a-f0-9]{12}$`).MatchString(r.KeyPrefix) || !receiverDigestPattern.MatchString(r.VerifierSHA256) || !receiverOperationPattern.MatchString(r.OperationID) || !v3IP(r.AllowedIP) {
		return ErrInvalid
	}
	return nil
}
func ValidateExistingReceiverCandidate(c ExistingReceiverCandidate) error {
	w, k := c.WorkerConfig, c.Checkpoint
	if !v3Source(c.SourceInstance) || !hashPattern.MatchString(c.SourceBindingSHA256) || !v3Positive.MatchString(c.FeedKeyID) || validatePublicReceiverRegistration(c.ReceiverRegistration) != nil || !v3IP(c.AllowedIP) || w.WorkerUnit != "xinsight-feed.service" || !IsCanonicalHTTPSOrigin(w.UROrigin) || w.FeedRoute != ReceiverFeedRoute || w.SourceInstance != c.SourceInstance || w.FeedSchema != ControlFeedSchema || w.CursorKey != ControlCursorKey || !MatchesCanonicalDigest(c.ReceiverRegistration, w.ReceiverRegistrationSHA256) || !v3UUID.MatchString(k.StateID) || k.ConsumerEpoch < 1 || !receiverOperationPattern.MatchString(k.EpochOperationID) || !hashPattern.MatchString(k.AuthorizationRefSHA256) || !hashPattern.MatchString(k.CursorSHA256) || !hashPattern.MatchString(k.HighWatermarkSHA256) {
		return ErrInvalid
	}
	return nil
}
func ValidateControlOperationRequestV3(o ControlOperationRequestV3) error {
	if o.SchemaVersion != ControlOperationSchemaV3 || !IsOperationID(o.OperationID) || !IsInstallationID(o.InstallationID) || !boundedIdentityPattern.MatchString(o.ProjectID) || !equalEventScope(o.EventScope, []string{CompletedEventType}) || o.DesiredState != "active" {
		return ErrInvalid
	}
	r := o.Request
	var exact ControlPhaseRequestV3
	if o.Phase == PhaseBindExistingReceiver {
		if o.Kind != KindAdoptExistingReceiver || o.PredecessorOperationID != "" || o.PredecessorResultSHA256 != "" || r.Candidate == nil || ValidateExistingReceiverCandidate(*r.Candidate) != nil || !MatchesCanonicalDigest(*r.Candidate, r.CandidateSHA256) {
			return ErrInvalid
		}
		exact = ControlPhaseRequestV3{Candidate: r.Candidate, CandidateSHA256: r.CandidateSHA256}
	} else {
		if !IsOperationID(o.PredecessorOperationID) || !hashPattern.MatchString(o.PredecessorResultSHA256) || o.PredecessorOperationID == o.OperationID {
			return ErrInvalid
		}
		switch o.Phase {
		case PhaseConfirmExistingReceiver:
			if o.Kind != KindAdoptExistingReceiver || r.XIAdoptionReceipt == nil || r.FeedReadReceipt == nil || !hashPattern.MatchString(r.AdoptionBindingSHA256) || !MatchesCanonicalDigest(*r.XIAdoptionReceipt, r.XIAdoptionReceiptSHA256) || !MatchesCanonicalDigest(*r.FeedReadReceipt, r.FeedReadReceiptSHA256) {
				return ErrInvalid
			}
			a, f := *r.XIAdoptionReceipt, *r.FeedReadReceipt
			if a.InstallationID != o.InstallationID || a.BindOperationID != o.PredecessorOperationID || a.BindResultSHA256 != o.PredecessorResultSHA256 || !hashPattern.MatchString(a.CandidateSHA256) || !v3UUID.MatchString(a.StateID) || a.ConsumerEpoch < 1 || !receiverOperationPattern.MatchString(a.EpochOperationID) || !hashPattern.MatchString(a.CommittedCursorSHA256) || !hashPattern.MatchString(a.CommittedHighWatermarkSHA256) || !v3Time(a.CommittedAt) || a.PagesCommittedBefore < 0 || !v3Positive.MatchString(f.FeedKeyID) || !hashPattern.MatchString(f.ReceiverRegistrationSHA256) || !hashPattern.MatchString(f.WorkerConfigSHA256) || f.CheckpointStateID != a.StateID || f.ConsumerEpoch != a.ConsumerEpoch || f.EpochOperationID != a.EpochOperationID || !v3Time(f.StartedAt) || !v3Time(f.CompletedAt) || !f.StartedAt.After(a.CommittedAt) || f.CompletedAt.Before(f.StartedAt) || f.PagesCommitted <= a.PagesCommittedBefore || !hashPattern.MatchString(f.CursorSHA256) || !hashPattern.MatchString(f.HighWatermarkSHA256) || f.ResponseStatus != 200 {
				return ErrInvalid
			}
			exact = ControlPhaseRequestV3{AdoptionBindingSHA256: r.AdoptionBindingSHA256, XIAdoptionReceipt: r.XIAdoptionReceipt, XIAdoptionReceiptSHA256: r.XIAdoptionReceiptSHA256, FeedReadReceipt: r.FeedReadReceipt, FeedReadReceiptSHA256: r.FeedReadReceiptSHA256}
		case PhaseDeliverMachineCandidate:
			if o.Kind != KindPairMachine || !v3Source(r.SourceInstance) || !hashPattern.MatchString(r.SourceBindingSHA256) || r.Registration == nil || r.CustodyReceipt == nil || ValidatePublicMachineRegistration(*r.Registration) != nil || r.Registration.InstallationID != o.InstallationID || !MatchesCanonicalDigest(*r.Registration, r.RegistrationSHA256) || !MatchesCanonicalDigest(*r.CustodyReceipt, r.CustodyReceiptSHA256) {
				return ErrInvalid
			}
			c := *r.CustodyReceipt
			if c.InstallationID != o.InstallationID || c.Purpose != r.Registration.Purpose || c.OperationID != r.Registration.OperationID || c.RegistrationSHA256 != r.RegistrationSHA256 || c.CustodyState != "prepared" {
				return ErrInvalid
			}
			exact = ControlPhaseRequestV3{SourceInstance: r.SourceInstance, SourceBindingSHA256: r.SourceBindingSHA256, Registration: r.Registration, RegistrationSHA256: r.RegistrationSHA256, CustodyReceipt: r.CustodyReceipt, CustodyReceiptSHA256: r.CustodyReceiptSHA256}
		default:
			return ErrInvalid
		}
	}
	if !reflect.DeepEqual(r, exact) {
		return ErrInvalid
	}
	return nil
}
func ValidateControlResultV3(v ControlResultV3, now time.Time) error {
	if v.SchemaVersion != ControlResultSchemaV3 || !IsOperationID(v.OperationID) || !IsInstallationID(v.InstallationID) || !v3Time(v.ObservedAt) || v.ObservedAt.After(now.Add(time.Minute)) {
		return ErrInvalid
	}
	if (v.Kind == KindAdoptExistingReceiver && v.Phase != PhaseBindExistingReceiver && v.Phase != PhaseConfirmExistingReceiver) || (v.Kind == KindPairMachine && v.Phase != PhaseDeliverMachineCandidate) || (v.Kind != KindAdoptExistingReceiver && v.Kind != KindPairMachine) {
		return ErrInvalid
	}
	if v.Status == "failed" {
		if v.Result != nil {
			return ErrInvalid
		}
		switch v.ReasonCode {
		case "installation_authority_changed":
			return nil
		case "adoption_identity_conflict", "adoption_binding_conflict", "receiver_read_unavailable":
			if v.Kind == KindAdoptExistingReceiver {
				return nil
			}
		case "machine_custody_unavailable", "machine_purpose_conflict":
			if v.Kind == KindPairMachine {
				return nil
			}
		}
		return ErrInvalid
	}
	if v.Status != "completed" || v.Result == nil || v.ReasonCode != "" {
		return ErrInvalid
	}
	r := *v.Result
	var exact ControlPhaseResultV3
	if !v3Source(r.SourceInstance) {
		return ErrInvalid
	}
	switch v.Phase {
	case PhaseBindExistingReceiver:
		if r.AdoptionBinding == nil || !hashPattern.MatchString(r.CandidateSHA256) || !MatchesCanonicalDigest(*r.AdoptionBinding, r.AdoptionBindingSHA256) {
			return ErrInvalid
		}
		b := *r.AdoptionBinding
		if b.InstallationID != v.InstallationID || b.OperationID != v.OperationID || b.OrgID < 1 || b.ProjectID < 1 || !v3Positive.MatchString(b.FeedKeyID) || b.CandidateSHA256 != r.CandidateSHA256 {
			return ErrInvalid
		}
		exact = ControlPhaseResultV3{SourceInstance: r.SourceInstance, CandidateSHA256: r.CandidateSHA256, AdoptionBinding: r.AdoptionBinding, AdoptionBindingSHA256: r.AdoptionBindingSHA256, SourceState: "active", ReceiverState: "feed_active", Readiness: "existing_receiver_bound"}
	case PhaseConfirmExistingReceiver:
		if !hashPattern.MatchString(r.AdoptionBindingSHA256) || !hashPattern.MatchString(r.XIAdoptionReceiptSHA256) || !hashPattern.MatchString(r.FeedReadReceiptSHA256) {
			return ErrInvalid
		}
		exact = ControlPhaseResultV3{SourceInstance: r.SourceInstance, AdoptionBindingSHA256: r.AdoptionBindingSHA256, XIAdoptionReceiptSHA256: r.XIAdoptionReceiptSHA256, FeedReadReceiptSHA256: r.FeedReadReceiptSHA256, SourceState: "active", ReceiverState: "feed_active", Readiness: "existing_receiver_adopted"}
	case PhaseDeliverMachineCandidate:
		if r.Purpose != PolicyMachinePurpose && r.Purpose != EnrollmentMachinePurpose || !hashPattern.MatchString(r.RegistrationSHA256) || !hashPattern.MatchString(r.CustodyReceiptSHA256) {
			return ErrInvalid
		}
		exact = ControlPhaseResultV3{SourceInstance: r.SourceInstance, Purpose: r.Purpose, RegistrationSHA256: r.RegistrationSHA256, CustodyReceiptSHA256: r.CustodyReceiptSHA256, Readiness: "public_candidate_delivered", Activation: "not_asserted"}
	}
	if !reflect.DeepEqual(r, exact) {
		return ErrInvalid
	}
	return nil
}
func ValidateControlResultV3ForOperation(o ControlOperationRequestV3, v ControlResultV3) error {
	if ValidateControlOperationRequestV3(o) != nil || ValidateControlResultV3(v, v.ObservedAt) != nil || o.OperationID != v.OperationID || o.InstallationID != v.InstallationID || o.Kind != v.Kind || o.Phase != v.Phase {
		return ErrInvalid
	}
	if v.Status == "failed" {
		return nil
	}
	r, b := o.Request, *v.Result
	switch o.Phase {
	case PhaseBindExistingReceiver:
		if b.SourceInstance != r.Candidate.SourceInstance || b.CandidateSHA256 != r.CandidateSHA256 || b.AdoptionBinding.FeedKeyID != r.Candidate.FeedKeyID {
			return ErrInvalid
		}
	case PhaseConfirmExistingReceiver:
		if b.AdoptionBindingSHA256 != r.AdoptionBindingSHA256 || b.XIAdoptionReceiptSHA256 != r.XIAdoptionReceiptSHA256 || b.FeedReadReceiptSHA256 != r.FeedReadReceiptSHA256 {
			return ErrInvalid
		}
	case PhaseDeliverMachineCandidate:
		if b.SourceInstance != r.SourceInstance || b.Purpose != r.Registration.Purpose || b.RegistrationSHA256 != r.RegistrationSHA256 || b.CustodyReceiptSHA256 != r.CustodyReceiptSHA256 {
			return ErrInvalid
		}
	}
	return nil
}

func OperationRequestV3(o ControlOperation) (ControlOperationRequestV3, error) {
	if o.SchemaVersion != ControlOperationSchemaV3 || o.RequestV3 == nil || o.Request != nil {
		return ControlOperationRequestV3{}, ErrInvalid
	}
	v := ControlOperationRequestV3{SchemaVersion: o.SchemaVersion, OperationID: o.OperationID, InstallationID: o.InstallationID, ProjectID: o.ProjectID, EventScope: o.EventScope, DesiredState: o.DesiredState, Kind: o.Kind, Phase: o.Phase, PredecessorOperationID: o.PredecessorOperationID, PredecessorResultSHA256: o.PredecessorResultSHA256, Request: *o.RequestV3}
	return v, ValidateControlOperationRequestV3(v)
}
func ValidateControlOperationV3(o ControlOperation) error {
	v, e := OperationRequestV3(o)
	if e != nil || !MatchesCanonicalDigest(v, o.RequestSHA256) || o.AttemptCount < 0 || o.AvailableAt.IsZero() || o.CreatedAt.IsZero() || o.UpdatedAt.Before(o.CreatedAt) {
		return ErrInvalid
	}
	if o.Status == "failed" {
		if o.CompletedAt == nil || o.LeaseExpiresAt != nil || !hashPattern.MatchString(o.ResultSHA256) {
			return ErrInvalid
		}
		return ValidateControlResultV3(ControlResultV3{SchemaVersion: ControlResultSchemaV3, OperationID: o.OperationID, InstallationID: o.InstallationID, Kind: o.Kind, Phase: o.Phase, Status: "failed", ReasonCode: o.ReasonCode, ObservedAt: *o.CompletedAt}, *o.CompletedAt)
	}
	return validateControlDeliveryState(o)
}
func (o ControlOperation) MarshalJSON() ([]byte, error) {
	type plain ControlOperation
	if o.SchemaVersion != ControlOperationSchemaV3 {
		return json.Marshal(plain(o))
	}
	if o.RequestV3 == nil {
		return nil, ErrInvalid
	}
	raw, e := json.Marshal(o.RequestV3)
	if e != nil {
		return nil, e
	}
	return json.Marshal(struct {
		*plain
		Request json.RawMessage `json:"request"`
	}{(*plain)(&o), raw})
}
func (v ControlOperationResult) MarshalJSON() ([]byte, error) {
	type plain ControlOperationResult
	if v.SchemaVersion == ControlResultSchemaV3 {
		if v.ResultV3 == nil {
			return nil, ErrInvalid
		}
		return json.Marshal(v.ResultV3)
	}
	return json.Marshal(plain(v))
}
func (v *ControlOperationResult) UnmarshalJSON(raw []byte) error {
	var h struct {
		SchemaVersion string `json:"schema_version"`
	}
	if json.Unmarshal(raw, &h) != nil {
		return ErrInvalid
	}
	if h.SchemaVersion == ControlResultSchemaV3 {
		var r ControlResultV3
		if decodeV3(raw, &r) != nil {
			return ErrInvalid
		}
		*v = ControlOperationResult{SchemaVersion: r.SchemaVersion, OperationID: r.OperationID, InstallationID: r.InstallationID, Phase: r.Phase, Status: r.Status, ReasonCode: r.ReasonCode, ObservedAt: r.ObservedAt, ResultV3: &r}
		return nil
	}
	type plain ControlOperationResult
	var p plain
	if decodeStrictControlJSON(raw, &p) != nil {
		return ErrInvalid
	}
	*v = ControlOperationResult(p)
	return nil
}
