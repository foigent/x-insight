package connectorcontrol

import (
	"context"
	"time"
)

const LocalCredentialRequestSchema = "xinsight.local-connector-credential-request.v1"
const LocalCredentialResponseSchema = "xinsight.local-connector-credential-response.v1"

type CredentialLocalRequest struct {
	SchemaVersion  string             `json:"schema_version"`
	Action         string             `json:"action"`
	InstallationID string             `json:"installation_id,omitempty"`
	OperationID    string             `json:"operation_id,omitempty"`
	ActorSubject   string             `json:"actor_subject,omitempty"`
	RequestID      string             `json:"request_id,omitempty"`
	Ticket         string             `json:"ticket,omitempty"`
	IdentityID     string             `json:"identity_id,omitempty"`
	BearerDigest   string             `json:"bearer_digest,omitempty"`
	Credential     *CredentialRequest `json:"credential,omitempty"`
	Confirm        *CredentialConfirm `json:"confirm,omitempty"`
}

type CredentialLocalResponse struct {
	SchemaVersion string             `json:"schema_version"`
	Action        string             `json:"action"`
	Recovery      *RecoveryMaterial  `json:"recovery,omitempty"`
	Receipt       *CredentialReceipt `json:"receipt,omitempty"`
	Error         string             `json:"error,omitempty"`
}

type CredentialController interface {
	ExecuteCredential(context.Context, CredentialLocalRequest) (CredentialLocalResponse, error)
}

func (r CredentialLocalRequest) Valid() bool {
	if r.SchemaVersion != LocalCredentialRequestSchema {
		return false
	}
	switch r.Action {
	case "issue":
		return IsInstallationID(r.InstallationID) && SetupRequestIDPattern.MatchString(r.RequestID) &&
			controllerHumanSubjectPattern.MatchString(r.ActorSubject) && r.OperationID == "" && r.Ticket == "" && r.IdentityID == "" && r.BearerDigest == "" && r.Credential == nil && r.Confirm == nil
	case "resolve":
		return IsRecoveryTicket(r.Ticket) && r.InstallationID == "" && r.OperationID == "" && r.ActorSubject == "" && r.RequestID == "" && r.IdentityID == "" && r.BearerDigest == "" && r.Credential == nil && r.Confirm == nil
	case "rotate", "recover":
		return r.Credential != nil && r.Credential.Valid() && r.Credential.Kind == r.Action &&
			IsInstallationID(r.InstallationID) && r.InstallationID == r.Credential.InstallationID &&
			r.OperationID == "" && r.ActorSubject == "" && r.RequestID == "" && r.Ticket == "" && r.Confirm == nil &&
			((r.Action == "rotate" && serviceIdentityPattern.MatchString(r.IdentityID) && hashPattern.MatchString(r.BearerDigest)) ||
				(r.Action == "recover" && r.IdentityID == "" && r.BearerDigest == ""))
	case "query", "confirm":
		return IsInstallationID(r.InstallationID) && IsCredentialOperationID(r.OperationID) &&
			serviceIdentityPattern.MatchString(r.IdentityID) && hashPattern.MatchString(r.BearerDigest) &&
			r.ActorSubject == "" && r.RequestID == "" && r.Ticket == "" && r.Credential == nil &&
			((r.Action == "query" && r.Confirm == nil) ||
				(r.Action == "confirm" && r.Confirm != nil && r.Confirm.SchemaVersion == CredentialRequestSchema &&
					r.Confirm.OperationID == r.OperationID && hashPattern.MatchString(r.Confirm.RequestSHA256)))
	}
	return false
}

func ValidCredentialLocalResponse(r CredentialLocalRequest, v CredentialLocalResponse) bool {
	if !r.Valid() || v.SchemaVersion != LocalCredentialResponseSchema || v.Action != r.Action {
		return false
	}
	if v.Error != "" {
		return v.Recovery == nil && v.Receipt == nil
	}
	if r.Action == "issue" || r.Action == "resolve" {
		return v.Recovery != nil && v.Receipt == nil && v.Recovery.SchemaVersion == RecoveryMaterialSchema &&
			v.Recovery.ExpiresAt.After(time.Time{}) &&
			((r.Action == "issue" && v.Recovery.InstallationID == r.InstallationID && IsRecoveryTicket(v.Recovery.Ticket)) ||
				(r.Action == "resolve" && v.Recovery.Ticket == "" && v.Recovery.RecoverURL == ""))
	}
	return v.Receipt != nil && v.Receipt.Valid() && v.Recovery == nil &&
		v.Receipt.InstallationID == r.InstallationID &&
		(r.Action != "query" || v.Receipt.OperationID == r.OperationID) &&
		(r.Action != "confirm" || v.Receipt.OperationID == r.OperationID)
}
