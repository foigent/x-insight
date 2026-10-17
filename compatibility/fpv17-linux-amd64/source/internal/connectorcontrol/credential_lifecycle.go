package connectorcontrol

import (
	"encoding/json"
	"net/url"
	"regexp"
	"time"
)

const (
	CredentialRequestSchema = "xinsight.connector-control-credential-request.v1"
	CredentialReceiptSchema = "xinsight.connector-control-credential-receipt.v1"
	RecoveryMaterialSchema  = "xinsight.connector-control-recovery.v1"
	ControlCredentialTTL    = 24 * time.Hour
	ControlRotationWindow   = 6 * time.Hour
	ControlRecoveryTTL      = 15 * time.Minute
)

var credentialOperationIDPattern = regexp.MustCompile(`^xcr_[A-Za-z0-9_-]{22}$`)
var recoveryIDPattern = regexp.MustCompile(`^xrr_[A-Za-z0-9_-]{22}$`)
var recoveryTicketPattern = regexp.MustCompile(`^xirt_[A-Za-z0-9_-]{43}$`)

// Field order is the frozen UR/XI wire order. Recover requires non-nil empty
// superseded fields when there is no stale prepared operation.
type CredentialRequest struct {
	SchemaVersion                    string   `json:"schema_version"`
	Kind                             string   `json:"kind"`
	InstallationID                   string   `json:"installation_id"`
	DeploymentID                     string   `json:"deployment_id"`
	ProjectID                        string   `json:"project_id"`
	FinalEventScope                  []string `json:"final_event_scope"`
	SourceBindingSHA256              string   `json:"source_binding_sha256"`
	XInsightOrigin                   string   `json:"xinsight_origin"`
	UniRouterOrigin                  string   `json:"unirouter_origin"`
	OperationID                      string   `json:"operation_id"`
	PreviousTokenSHA256              string   `json:"previous_token_sha256"`
	ReplacementToken                 string   `json:"replacement_token"`
	RecoveryID                       string   `json:"recovery_id,omitempty"`
	RecoveryTicket                   string   `json:"recovery_ticket,omitempty"`
	PreviousToken                    string   `json:"previous_token,omitempty"`
	SupersededOperationID            *string  `json:"superseded_operation_id,omitempty"`
	SupersededReplacementTokenSHA256 *string  `json:"superseded_replacement_token_sha256,omitempty"`
}

func DecodeCredentialRequest(raw []byte) (CredentialRequest, error) {
	var v CredentialRequest
	if decodeV3(raw, &v) != nil || !v.Valid() {
		return CredentialRequest{}, ErrInvalid
	}
	return v, nil
}

func (v CredentialRequest) Valid() bool {
	if v.SchemaVersion != CredentialRequestSchema ||
		(v.Kind != "rotate" && v.Kind != "recover") ||
		!installationIDPattern.MatchString(v.InstallationID) ||
		!deploymentIDPattern.MatchString(v.DeploymentID) ||
		!boundedIdentityPattern.MatchString(v.ProjectID) ||
		len(v.FinalEventScope) != 1 || v.FinalEventScope[0] != CompletedEventType ||
		!hashPattern.MatchString(v.SourceBindingSHA256) ||
		!IsCanonicalHTTPSOrigin(v.XInsightOrigin) || !IsCanonicalHTTPSOrigin(v.UniRouterOrigin) ||
		!credentialOperationIDPattern.MatchString(v.OperationID) ||
		!hashPattern.MatchString(v.PreviousTokenSHA256) ||
		!serviceTokenPattern.MatchString(v.ReplacementToken) ||
		RawSHA256(v.ReplacementToken) == v.PreviousTokenSHA256 {
		return false
	}
	if v.Kind == "rotate" {
		return v.RecoveryID == "" && v.RecoveryTicket == "" && v.PreviousToken == "" &&
			v.SupersededOperationID == nil && v.SupersededReplacementTokenSHA256 == nil
	}
	if !recoveryIDPattern.MatchString(v.RecoveryID) ||
		!recoveryTicketPattern.MatchString(v.RecoveryTicket) ||
		!serviceTokenPattern.MatchString(v.PreviousToken) ||
		RawSHA256(v.PreviousToken) != v.PreviousTokenSHA256 ||
		v.SupersededOperationID == nil || v.SupersededReplacementTokenSHA256 == nil {
		return false
	}
	if *v.SupersededOperationID == "" {
		return *v.SupersededReplacementTokenSHA256 == ""
	}
	return credentialOperationIDPattern.MatchString(*v.SupersededOperationID) &&
		hashPattern.MatchString(*v.SupersededReplacementTokenSHA256)
}

func (v CredentialRequest) Digest() (string, error) {
	if !v.Valid() {
		return "", ErrInvalid
	}
	sha, _, err := CanonicalSHA256(v)
	return sha, err
}

type CredentialConfirm struct {
	SchemaVersion string `json:"schema_version"`
	OperationID   string `json:"operation_id"`
	RequestSHA256 string `json:"request_sha256"`
}

func DecodeCredentialConfirm(raw []byte) (CredentialConfirm, error) {
	var v CredentialConfirm
	if decodeV3(raw, &v) != nil || v.SchemaVersion != CredentialRequestSchema ||
		!credentialOperationIDPattern.MatchString(v.OperationID) || !hashPattern.MatchString(v.RequestSHA256) {
		return CredentialConfirm{}, ErrInvalid
	}
	return v, nil
}

type CredentialReceipt struct {
	SchemaVersion          string    `json:"schema_version"`
	InstallationID         string    `json:"installation_id"`
	DeploymentID           string    `json:"deployment_id"`
	ProjectID              string    `json:"project_id"`
	FinalEventScope        []string  `json:"final_event_scope"`
	SourceBindingSHA256    string    `json:"source_binding_sha256"`
	XInsightOrigin         string    `json:"xinsight_origin"`
	UniRouterOrigin        string    `json:"unirouter_origin"`
	OperationID            string    `json:"operation_id"`
	RequestSHA256          string    `json:"request_sha256"`
	ControlIdentityID      string    `json:"control_identity_id"`
	PreviousTokenSHA256    string    `json:"previous_token_sha256"`
	ReplacementTokenSHA256 string    `json:"replacement_token_sha256"`
	ReplacementExpiresAt   time.Time `json:"replacement_expires_at"`
	State                  string    `json:"state"`
}

func DecodeCredentialReceipt(raw []byte) (CredentialReceipt, error) {
	var v CredentialReceipt
	if decodeV3(raw, &v) != nil || !v.Valid() {
		return CredentialReceipt{}, ErrInvalid
	}
	return v, nil
}

func (v CredentialReceipt) Valid() bool {
	return v.SchemaVersion == CredentialReceiptSchema &&
		installationIDPattern.MatchString(v.InstallationID) && deploymentIDPattern.MatchString(v.DeploymentID) &&
		boundedIdentityPattern.MatchString(v.ProjectID) && len(v.FinalEventScope) == 1 &&
		v.FinalEventScope[0] == CompletedEventType && hashPattern.MatchString(v.SourceBindingSHA256) &&
		IsCanonicalHTTPSOrigin(v.XInsightOrigin) && IsCanonicalHTTPSOrigin(v.UniRouterOrigin) &&
		credentialOperationIDPattern.MatchString(v.OperationID) && hashPattern.MatchString(v.RequestSHA256) &&
		serviceIdentityPattern.MatchString(v.ControlIdentityID) &&
		hashPattern.MatchString(v.PreviousTokenSHA256) && hashPattern.MatchString(v.ReplacementTokenSHA256) &&
		!v.ReplacementExpiresAt.IsZero() && (v.State == "prepared" || v.State == "committed")
}

func (v CredentialReceipt) JSON() ([]byte, error) {
	if !v.Valid() {
		return nil, ErrInvalid
	}
	return json.Marshal(v)
}

type RecoveryMaterial struct {
	SchemaVersion                    string    `json:"schema_version"`
	InstallationID                   string    `json:"installation_id"`
	DeploymentID                     string    `json:"deployment_id"`
	ProjectID                        string    `json:"project_id"`
	FinalEventScope                  []string  `json:"final_event_scope"`
	SourceBindingSHA256              string    `json:"source_binding_sha256"`
	XInsightOrigin                   string    `json:"xinsight_origin"`
	UniRouterOrigin                  string    `json:"unirouter_origin"`
	RecoveryID                       string    `json:"recovery_id"`
	PreviousTokenSHA256              string    `json:"previous_token_sha256"`
	SupersededOperationID            string    `json:"superseded_operation_id"`
	SupersededReplacementTokenSHA256 string    `json:"superseded_replacement_token_sha256"`
	ExpiresAt                        time.Time `json:"expires_at"`
	Ticket                           string    `json:"ticket,omitempty"`
	RecoverURL                       string    `json:"recover_url,omitempty"`
}

func IsCredentialOperationID(id string) bool { return credentialOperationIDPattern.MatchString(id) }
func IsRecoveryID(id string) bool            { return recoveryIDPattern.MatchString(id) }
func IsRecoveryTicket(ticket string) bool    { return recoveryTicketPattern.MatchString(ticket) }

func BuildCredentialRecoveryURL(material RecoveryMaterial) (string, error) {
	if material.SchemaVersion != RecoveryMaterialSchema || !IsInstallationID(material.InstallationID) ||
		!IsCanonicalHTTPSOrigin(material.XInsightOrigin) || !IsCanonicalHTTPSOrigin(material.UniRouterOrigin) ||
		!IsRecoveryID(material.RecoveryID) || !IsRecoveryTicket(material.Ticket) ||
		!hashPattern.MatchString(material.PreviousTokenSHA256) || material.ExpiresAt.IsZero() {
		return "", ErrInvalid
	}
	fragment := url.Values{}
	fragment.Set("xinsight_origin", material.XInsightOrigin)
	fragment.Set("installation_id", material.InstallationID)
	fragment.Set("recovery_id", material.RecoveryID)
	fragment.Set("recovery_ticket", material.Ticket)
	return material.UniRouterOrigin + "/connectors/x-insight/recover#" + fragment.Encode(), nil
}
