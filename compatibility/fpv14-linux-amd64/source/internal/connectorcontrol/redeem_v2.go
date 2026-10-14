package connectorcontrol

import (
	"context"
	"encoding/json"
	"io"
	"regexp"
	"time"
)

const RedeemInvitationSchemaV2 = "xinsight.connector-invitation-redeem.v2"
const RedeemInvitationResultSchemaV2 = "xinsight.connector-invitation-redeem-result.v2"

// Bootstrap carries public consent-bound identities. It is not a receipt or
// permission to replace the locally active receiver.
type ReceiverAdoptionBootstrap struct {
	InstallationID          string `json:"installation_id"`
	OrgID                   int64  `json:"org_id"`
	ProjectID               int64  `json:"project_id"`
	SourceInstance          string `json:"source_instance"`
	SourceBindingSHA256     string `json:"source_binding_sha256"`
	FeedKeyID               string `json:"feed_key_id"`
	AllowedIP               string `json:"allowed_ip"`
	KeyPrefix               string `json:"key_prefix"`
	RegistrationOperationID string `json:"registration_operation_id"`
	VerifierScheme          string `json:"verifier_scheme"`
}
type RedeemInvitationInputV2 struct {
	SchemaVersion string `json:"schema_version"`
	RedeemInvitationInput
	ReceiverAdoptionBootstrap       ReceiverAdoptionBootstrap `json:"receiver_adoption_bootstrap"`
	ReceiverAdoptionBootstrapSHA256 string                    `json:"receiver_adoption_bootstrap_sha256"`
}
type RedeemInvitationResultV2 struct {
	SchemaVersion                   string       `json:"schema_version"`
	Installation                    Installation `json:"installation"`
	ReceiverAdoptionBootstrapSHA256 string       `json:"receiver_adoption_bootstrap_sha256"`
}
type AdoptionInvitationStore interface {
	RedeemAdoptionInvitation(context.Context, RedeemInvitationInputV2, time.Time, io.Reader) (RedeemInvitationResultV2, error)
}

func DecodeRedeemInvitationV2(raw []byte, now time.Time) (RedeemInvitationInputV2, error) {
	var r RedeemInvitationInputV2
	if decodeV3(raw, &r) != nil || ValidateRedeemInvitationV2(r, now) != nil {
		return r, ErrInvalid
	}
	return r, nil
}
func ValidateReceiverAdoptionBootstrap(b ReceiverAdoptionBootstrap) error {
	if !IsInstallationID(b.InstallationID) || b.OrgID < 1 || b.ProjectID < 1 || !v3Source(b.SourceInstance) || !hashPattern.MatchString(b.SourceBindingSHA256) || !v3Positive.MatchString(b.FeedKeyID) || !v3IP(b.AllowedIP) || !regexp.MustCompile(`^ur-xf1-[a-f0-9]{13}$`).MatchString(b.KeyPrefix) || !receiverOperationPattern.MatchString(b.RegistrationOperationID) || b.VerifierScheme != "receiver_sha256_v1" {
		return ErrInvalid
	}
	return nil
}
func ValidateRedeemInvitationV2(r RedeemInvitationInputV2, now time.Time) error {
	if r.SchemaVersion != RedeemInvitationSchemaV2 || ValidateRedeemInvitation(r.RedeemInvitationInput, now) != nil || ValidateReceiverAdoptionBootstrap(r.ReceiverAdoptionBootstrap) != nil || !MatchesCanonicalDigest(r.ReceiverAdoptionBootstrap, r.ReceiverAdoptionBootstrapSHA256) || r.ConsentSHA256 != r.ReceiverAdoptionBootstrap.SourceBindingSHA256 {
		return ErrInvalid
	}
	return nil
}

// SourceBindingDigest recomputes the original UR consent object, including the
// numeric UR scope which is distinct from the opaque control project ID.
func SourceBindingDigest(deployment string, b ReceiverAdoptionBootstrap) (string, error) {
	if !boundedIdentityPattern.MatchString(deployment) || ValidateReceiverAdoptionBootstrap(b) != nil {
		return "", ErrInvalid
	}
	v := struct {
		SchemaVersion  string `json:"schema_version"`
		InstallationID string `json:"installation_id"`
		DeploymentID   string `json:"x_deployment_id"`
		OrgID          int64  `json:"org_id"`
		ProjectID      int64  `json:"project_id"`
		SourceInstance string `json:"source_instance"`
		EventScope     string `json:"event_scope"`
		FeedSchema     string `json:"feed_schema"`
	}{SourceBindingSchema, b.InstallationID, deployment, b.OrgID, b.ProjectID, b.SourceInstance, CompletedEventType, SourceBindingFeedSchema}
	sha, _, e := CanonicalSHA256(v)
	return sha, e
}
func BootstrapFromJSON(raw []byte) (ReceiverAdoptionBootstrap, error) {
	var b ReceiverAdoptionBootstrap
	if decodeV3(raw, &b) != nil || ValidateReceiverAdoptionBootstrap(b) != nil {
		return b, ErrInvalid
	}
	return b, nil
}

// Explicit dispatch keeps the original seven-field redeem contract closed.
func RedeemSchema(raw []byte) (string, error) {
	var h struct {
		SchemaVersion string `json:"schema_version"`
	}
	if json.Unmarshal(raw, &h) != nil {
		return "", ErrInvalid
	}
	return h.SchemaVersion, nil
}
