package connectorcontrol

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	InstallationSchema        = "xinsight.connector-installation.v1"
	InvitationSchema          = "xinsight.connector-installation-invitation.v1"
	AcknowledgementSchema     = "xinsight.connector-installation-acknowledgement.v1"
	ControlOperationSchema    = "xinsight.connector-control-operation.v1"
	ControlResultSchema       = "xinsight.connector-control-result.v1"
	ConnectorTypeUniRouter    = "unirouter"
	ConnectorAudience         = "unirouter"
	CompletedEventType        = "model.request.completed"
	PermissionControlPoll     = "connector.control.poll"
	PermissionControlAck      = "connector.control.ack"
	DefaultInvitationTTL      = 15 * time.Minute
	MaximumInvitationTTL      = 30 * time.Minute
	MaximumControlTokenTTL    = 365 * 24 * time.Hour
	MaximumConnectorListSize  = 100
	DeploymentIDEnv           = "XINSIGHT_CONNECTOR_DEPLOYMENT_ID"
	UniRouterInstallOriginEnv = "XINSIGHT_UNIROUTER_INSTALL_ORIGIN"
)

var (
	ErrInvalid  = errors.New("connector control input is invalid")
	ErrNotFound = errors.New("connector control record was not found")
	ErrConflict = errors.New("connector control state conflicts with the request")
	ErrExpired  = errors.New("connector invitation expired")
	ErrConsumed = errors.New("connector invitation was already consumed")

	installationIDPattern  = regexp.MustCompile(`^xci_[A-Za-z0-9_-]{22}$`)
	invitationIDPattern    = regexp.MustCompile(`^xii_[A-Za-z0-9_-]{22}$`)
	operationIDPattern     = regexp.MustCompile(`^xco_[A-Za-z0-9_-]{22}$`)
	deploymentIDPattern    = regexp.MustCompile(`^xid_[A-Za-z0-9_-]{22}$`)
	invitationTokenPattern = regexp.MustCompile(`^xiiv1_[A-Za-z0-9_-]{43}$`)
	serviceTokenPattern    = regexp.MustCompile(`^xib_[A-Za-z0-9_-]{43}$`)
	serviceIdentityPattern = regexp.MustCompile(`^xsi_[A-Za-z0-9_-]{22}$`)
	boundedIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	hashPattern            = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type InvitationConfig struct {
	DeploymentID           string
	XInsightOrigin         string
	UniRouterInstallOrigin string
}

func (config InvitationConfig) Validate() error {
	if !deploymentIDPattern.MatchString(config.DeploymentID) ||
		validateCanonicalHTTPSOrigin(config.XInsightOrigin) != nil ||
		validateCanonicalHTTPSOrigin(config.UniRouterInstallOrigin) != nil {
		return ErrInvalid
	}
	return nil
}

func (config InvitationConfig) Enabled() bool {
	return config.Validate() == nil
}

func IsDeploymentID(value string) bool {
	return deploymentIDPattern.MatchString(value)
}

func IsCanonicalHTTPSOrigin(value string) bool {
	return validateCanonicalHTTPSOrigin(value) == nil
}

type Installation struct {
	SchemaVersion                     string     `json:"schema_version"`
	InstallationID                    string     `json:"installation_id"`
	DeploymentID                      string     `json:"deployment_id"`
	ConnectorType                     string     `json:"connector_type"`
	RequestedEventScope               []string   `json:"requested_event_scope"`
	FinalEventScope                   []string   `json:"final_event_scope,omitempty"`
	ProjectID                         string     `json:"project_id,omitempty"`
	SourceInstance                    string     `json:"-"`
	SourceInstanceSHA256              string     `json:"source_instance_sha256,omitempty"`
	ControlServiceIdentityID          string     `json:"-"`
	InstallationAcknowledgementSHA256 string     `json:"installation_acknowledgement_sha256,omitempty"`
	Status                            string     `json:"status"`
	DesiredState                      string     `json:"desired_state"`
	ObservedState                     string     `json:"observed_state"`
	StatusReasonCode                  string     `json:"status_reason_code,omitempty"`
	LastAgentSeenAt                   *time.Time `json:"last_agent_seen_at,omitempty"`
	CreatedAt                         time.Time  `json:"created_at"`
	UpdatedAt                         time.Time  `json:"updated_at"`
	Version                           int64      `json:"version"`
}

type InvitationPackage struct {
	SchemaVersion       string    `json:"schema_version"`
	InvitationID        string    `json:"invitation_id"`
	InstallationID      string    `json:"installation_id"`
	DeploymentID        string    `json:"deployment_id"`
	Audience            string    `json:"audience"`
	XInsightOrigin      string    `json:"xinsight_origin"`
	RequestedEventScope []string  `json:"requested_event_scope"`
	ExpiresAt           time.Time `json:"expires_at"`
	InstallURL          string    `json:"install_url"`
	Token               string    `json:"-"`
}

type CreateInvitationInput struct {
	ActorSubject string
	Config       InvitationConfig
	Now          time.Time
	TTL          time.Duration
	Random       io.Reader
}

type InstallationAcknowledgement struct {
	SchemaVersion   string    `json:"schema_version"`
	InstallationID  string    `json:"installation_id"`
	DeploymentID    string    `json:"deployment_id"`
	ProjectID       string    `json:"project_id"`
	FinalEventScope []string  `json:"final_event_scope"`
	ConsentSHA256   string    `json:"consent_sha256"`
	ConfirmedAt     time.Time `json:"confirmed_at"`
}

type RedeemInvitationInput struct {
	InvitationToken     string    `json:"invitation_token"`
	ProjectID           string    `json:"project_id"`
	FinalEventScope     []string  `json:"final_event_scope"`
	ConsentSHA256       string    `json:"consent_sha256"`
	ConfirmedAt         time.Time `json:"confirmed_at"`
	ControlToken        string    `json:"control_token"`
	ControlTokenExpires time.Time `json:"control_token_expires_at"`
}

type RegistrationResult struct {
	Installation Installation     `json:"installation"`
	Operation    ControlOperation `json:"operation"`
}

type ControlOperation struct {
	SchemaVersion           string                 `json:"schema_version"`
	OperationID             string                 `json:"operation_id"`
	InstallationID          string                 `json:"installation_id"`
	Kind                    string                 `json:"kind"`
	Phase                   string                 `json:"phase,omitempty"`
	ProjectID               string                 `json:"project_id"`
	EventScope              []string               `json:"event_scope"`
	DesiredState            string                 `json:"desired_state"`
	PredecessorOperationID  string                 `json:"predecessor_operation_id,omitempty"`
	PredecessorResultSHA256 string                 `json:"predecessor_result_sha256,omitempty"`
	Request                 *ControlPhaseRequestV2 `json:"request,omitempty"`
	RequestSHA256           string                 `json:"request_sha256"`
	Status                  string                 `json:"status"`
	AttemptCount            int                    `json:"attempt_count"`
	AvailableAt             time.Time              `json:"available_at"`
	LeaseExpiresAt          *time.Time             `json:"lease_expires_at,omitempty"`
	CompletedAt             *time.Time             `json:"completed_at,omitempty"`
	ResultSHA256            string                 `json:"result_sha256,omitempty"`
	ReasonCode              string                 `json:"reason_code,omitempty"`
	CreatedAt               time.Time              `json:"created_at"`
	UpdatedAt               time.Time              `json:"updated_at"`
}

type ControlOperationResult struct {
	SchemaVersion  string                `json:"schema_version"`
	OperationID    string                `json:"operation_id"`
	InstallationID string                `json:"installation_id"`
	Phase          string                `json:"phase,omitempty"`
	Status         string                `json:"status"`
	SourceInstance string                `json:"source_instance,omitempty"`
	Result         *ControlPhaseResultV2 `json:"result,omitempty"`
	ReasonCode     string                `json:"reason_code,omitempty"`
	ObservedAt     time.Time             `json:"observed_at"`
}

type Store interface {
	ListInstallations(context.Context, int) ([]Installation, error)
	RedeemInvitation(context.Context, RedeemInvitationInput, time.Time, io.Reader) (RegistrationResult, error)
	PollControlOperation(context.Context, string, string, time.Time) (ControlOperation, error)
	CompleteControlOperation(context.Context, string, ControlOperationResult, time.Time) (Installation, error)
}

// InvitationIssuer is intentionally separate from Store. The web process uses
// the restricted workspace Store for ordinary connector state but must cross
// the fixed local controller boundary to issue an invitation.
type InvitationIssuer interface {
	CreateInvitation(context.Context, CreateInvitationInput) (InvitationPackage, Installation, error)
}

func ValidateRedeemInvitation(input RedeemInvitationInput, now time.Time) error {
	if !invitationTokenPattern.MatchString(input.InvitationToken) ||
		!boundedIdentityPattern.MatchString(input.ProjectID) ||
		!equalEventScope(input.FinalEventScope, []string{CompletedEventType}) ||
		!hashPattern.MatchString(input.ConsentSHA256) ||
		input.ConfirmedAt.IsZero() || input.ConfirmedAt.After(now.Add(time.Minute)) ||
		!serviceTokenPattern.MatchString(input.ControlToken) ||
		!input.ControlTokenExpires.After(now) ||
		input.ControlTokenExpires.After(now.Add(MaximumControlTokenTTL)) {
		return ErrInvalid
	}
	return nil
}

func ValidateControlResult(result ControlOperationResult, now time.Time) error {
	if result.SchemaVersion == ControlResultSchemaV2 {
		return ValidateControlResultV2(result, now)
	}
	if result.SchemaVersion != ControlResultSchema ||
		!operationIDPattern.MatchString(result.OperationID) ||
		!installationIDPattern.MatchString(result.InstallationID) ||
		result.Phase != "" || result.Result != nil ||
		result.ObservedAt.IsZero() || result.ObservedAt.After(now.Add(time.Minute)) {
		return ErrInvalid
	}
	switch result.Status {
	case "completed":
		if !boundedIdentityPattern.MatchString(result.SourceInstance) || result.ReasonCode != "" {
			return ErrInvalid
		}
	case "failed":
		if result.SourceInstance != "" || !validReasonCode(result.ReasonCode) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func ValidateInstallation(value Installation) error {
	if value.SchemaVersion != InstallationSchema ||
		!installationIDPattern.MatchString(value.InstallationID) ||
		!deploymentIDPattern.MatchString(value.DeploymentID) ||
		value.ConnectorType != ConnectorTypeUniRouter ||
		!equalEventScope(value.RequestedEventScope, []string{CompletedEventType}) ||
		value.Version < 1 || value.CreatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
		return ErrInvalid
	}
	if !validInstallationState(value.Status, value.DesiredState, value.ObservedState, value.StatusReasonCode) {
		return ErrInvalid
	}
	if value.ProjectID == "" {
		if len(value.FinalEventScope) != 0 || value.ControlServiceIdentityID != "" ||
			value.InstallationAcknowledgementSHA256 != "" || value.SourceInstance != "" ||
			value.SourceInstanceSHA256 != "" {
			return ErrInvalid
		}
		return nil
	}
	if !boundedIdentityPattern.MatchString(value.ProjectID) ||
		!equalEventScope(value.FinalEventScope, value.RequestedEventScope) ||
		!serviceIdentityPattern.MatchString(value.ControlServiceIdentityID) ||
		!hashPattern.MatchString(value.InstallationAcknowledgementSHA256) {
		return ErrInvalid
	}
	if value.SourceInstance != "" {
		if !boundedIdentityPattern.MatchString(value.SourceInstance) ||
			value.SourceInstanceSHA256 != sha256Reference([]byte(value.SourceInstance)) {
			return ErrInvalid
		}
	} else if value.SourceInstanceSHA256 != "" {
		return ErrInvalid
	}
	return nil
}

func NewID(prefix string, source io.Reader) (string, error) {
	switch prefix {
	case "xci_", "xii_", "xco_":
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

func NewInvitationToken(source io.Reader) (string, string, error) {
	if source == nil {
		source = rand.Reader
	}
	var value [32]byte
	if _, err := io.ReadFull(source, value[:]); err != nil {
		return "", "", err
	}
	token := "xiiv1_" + base64.RawURLEncoding.EncodeToString(value[:])
	if !invitationTokenPattern.MatchString(token) {
		return "", "", ErrInvalid
	}
	return token, sha256Reference([]byte(token)), nil
}

func InvitationTokenDigest(token string) (string, error) {
	if !invitationTokenPattern.MatchString(token) {
		return "", ErrInvalid
	}
	return sha256Reference([]byte(token)), nil
}

func BuildInstallURL(origin string, token string, xInsightOrigin string) (string, error) {
	if validateCanonicalHTTPSOrigin(origin) != nil ||
		validateCanonicalHTTPSOrigin(xInsightOrigin) != nil ||
		!invitationTokenPattern.MatchString(token) {
		return "", ErrInvalid
	}
	values := url.Values{}
	values.Set("invitation", token)
	values.Set("xinsight_origin", xInsightOrigin)
	return origin + "/connectors/x-insight/install#" + values.Encode(), nil
}

func CanonicalSHA256(value any) (string, []byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	return sha256Reference(payload), payload, nil
}

// RawSHA256 binds the exact UTF-8 bytes of an identity. It deliberately does
// not apply JSON string encoding and never returns the input value.
func RawSHA256(value string) string {
	return sha256Reference([]byte(value))
}

func IsInstallationID(value string) bool {
	return installationIDPattern.MatchString(value)
}

func IsOperationID(value string) bool {
	return operationIDPattern.MatchString(value)
}

func ValidateControlOperation(value ControlOperation) error {
	if value.SchemaVersion == ControlOperationSchemaV2 {
		return ValidateControlOperationV2(value)
	}
	if value.SchemaVersion != ControlOperationSchema ||
		!operationIDPattern.MatchString(value.OperationID) ||
		!installationIDPattern.MatchString(value.InstallationID) ||
		value.Kind != "connect" ||
		value.Phase != "" || value.PredecessorOperationID != "" ||
		value.PredecessorResultSHA256 != "" || value.Request != nil ||
		!boundedIdentityPattern.MatchString(value.ProjectID) ||
		!equalEventScope(value.EventScope, []string{CompletedEventType}) ||
		value.DesiredState != "active" ||
		!hashPattern.MatchString(value.RequestSHA256) ||
		value.AttemptCount < 0 || value.AvailableAt.IsZero() || value.CreatedAt.IsZero() ||
		value.UpdatedAt.Before(value.CreatedAt) {
		return ErrInvalid
	}
	switch value.Status {
	case "pending":
		if value.AttemptCount != 0 || value.LeaseExpiresAt != nil || value.CompletedAt != nil ||
			value.ResultSHA256 != "" || value.ReasonCode != "" {
			return ErrInvalid
		}
	case "running":
		if value.AttemptCount < 1 || value.LeaseExpiresAt == nil || value.CompletedAt != nil ||
			value.ResultSHA256 != "" || value.ReasonCode != "" {
			return ErrInvalid
		}
	case "completed":
		if value.LeaseExpiresAt != nil || value.CompletedAt == nil ||
			!hashPattern.MatchString(value.ResultSHA256) || value.ReasonCode != "" {
			return ErrInvalid
		}
	case "failed":
		if value.LeaseExpiresAt != nil || value.CompletedAt == nil ||
			!hashPattern.MatchString(value.ResultSHA256) || !validReasonCode(value.ReasonCode) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validReasonCode(value string) bool {
	switch value {
	case "authorization_denied", "scope_rejected", "source_unavailable", "permission_denied",
		"source_identity_invalid", "transport_unavailable", "contract_invalid", "internal_error":
		return true
	default:
		return false
	}
}

func validInstallationState(status string, desired string, observed string, reason string) bool {
	if desired != "active" && desired != "paused" && desired != "disconnected" {
		return false
	}
	switch status {
	case "pending":
		return reason == "" && (observed == "awaiting_consent" || observed == "agent_registered" ||
			observed == "source_ready" || observed == "controller_pending")
	case "connected":
		return reason == "" && observed == "connected"
	case "paused":
		return reason == "" && observed == "paused"
	case "abnormal":
		return observed == "abnormal" && validReasonCode(reason)
	case "disconnected":
		return reason == "" && observed == "disconnected"
	default:
		return false
	}
}

func equalEventScope(left []string, right []string) bool {
	if len(left) != len(right) || len(left) == 0 {
		return false
	}
	leftCopy := append([]string(nil), left...)
	rightCopy := append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] || leftCopy[index] != CompletedEventType {
			return false
		}
		if index > 0 && leftCopy[index] == leftCopy[index-1] {
			return false
		}
	}
	return true
}

func validateCanonicalHTTPSOrigin(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || parsed.String() != value || strings.Contains(parsed.Host, "@") {
		return fmt.Errorf("%w: HTTPS origin", ErrInvalid)
	}
	return nil
}

func sha256Reference(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}
