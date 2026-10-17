package uniroutercampaign

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/integration/boundedoffline"
	"github.com/foigent/x-insight/internal/integrationcontrol"
)

const (
	preparedImportAuthorizationSealVersion = "xinsight.prepared-campaign-import-authorization.v1"
	verifiedImportAuthoritySealVersion     = "xinsight.verified-campaign-import-authority.v1"
)

var campaignAuthorityDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// VerifiedCampaignImportAuthority is deliberately opaque. The only product
// issuer is ImportPreparedVerified, after ImportPrepared has completed and the
// exact completed operational receipt has been read back from its durable
// operation store.
type VerifiedCampaignImportAuthority struct {
	receipt          unirouter.CampaignPackageImportReceipt
	verifiedPackage  unirouter.VerifiedCampaignPackage
	authorization    integrationcontrol.N1AuthorizationVerification
	operationBinding boundedoffline.OperationBinding
	verificationSeal string
}

func sealVerifiedCampaignImportAuthority(
	ctx context.Context,
	operations ImportOperationStore,
	prepared PreparedImport,
	receipt unirouter.CampaignPackageImportReceipt,
) (VerifiedCampaignImportAuthority, error) {
	if ctx == nil || operations == nil {
		return VerifiedCampaignImportAuthority{}, errors.New(
			"completed campaign import operation readback is required",
		)
	}
	if err := ctx.Err(); err != nil {
		return VerifiedCampaignImportAuthority{}, err
	}
	if err := prepared.validateAuthorizedSeal(); err != nil {
		return VerifiedCampaignImportAuthority{}, err
	}
	if err := validateCampaignImportAuthorityInputs(
		receipt,
		prepared.verified,
		prepared.operationBinding,
	); err != nil {
		return VerifiedCampaignImportAuthority{}, err
	}
	if err := readbackCompletedCampaignImport(
		ctx,
		operations,
		prepared.operationBinding,
		receipt.OperationalReceipt,
	); err != nil {
		return VerifiedCampaignImportAuthority{}, err
	}

	// Detach receipt slices from the compatibility value returned to the caller.
	receiptBytes, err := receipt.CanonicalBytes()
	if err != nil {
		return VerifiedCampaignImportAuthority{}, err
	}
	detachedReceipt, err := unirouter.ParseCampaignPackageImportReceipt(receiptBytes)
	if err != nil {
		return VerifiedCampaignImportAuthority{}, err
	}
	authority := VerifiedCampaignImportAuthority{
		receipt:          detachedReceipt,
		verifiedPackage:  prepared.verified,
		authorization:    cloneCampaignAuthorization(prepared.authorization),
		operationBinding: prepared.operationBinding,
	}
	authority.verificationSeal, err = authority.expectedSeal()
	if err != nil {
		return VerifiedCampaignImportAuthority{}, err
	}
	if err := authority.validate(); err != nil {
		return VerifiedCampaignImportAuthority{}, err
	}
	return authority, nil
}

func readbackCompletedCampaignImport(
	ctx context.Context,
	operations ImportOperationStore,
	binding boundedoffline.OperationBinding,
	receipt connector.SourceNeutralOperationalReceipt,
) error {
	if ctx == nil || operations == nil {
		return errors.New("completed campaign import operation readback is required")
	}
	receiptBytes, err := receipt.CanonicalBytes()
	if err != nil {
		return err
	}
	readback, err := operations.WithOperation(
		ctx,
		binding,
		func(
			_ context.Context,
			existing *connector.SourceNeutralOperationalReceipt,
		) (connector.SourceNeutralOperationalReceipt, error) {
			if existing == nil || existing.Validate() != nil {
				return connector.SourceNeutralOperationalReceipt{}, boundedoffline.ErrOperationConflict
			}
			return *existing, nil
		},
	)
	if err != nil {
		return err
	}
	readbackBytes, err := readback.CanonicalBytes()
	if err != nil || !bytes.Equal(readbackBytes, receiptBytes) {
		return boundedoffline.ErrOperationConflict
	}
	return nil
}

func (prepared PreparedImport) expectedAuthorizationSeal() (string, error) {
	payload, err := json.Marshal(struct {
		SealVersion      string                                    `json:"seal_version"`
		OperationBinding boundedoffline.OperationBinding           `json:"operation_binding"`
		Authorization    campaignImportAuthorizationAuthorityFacts `json:"authorization"`
	}{
		SealVersion:      preparedImportAuthorizationSealVersion,
		OperationBinding: prepared.operationBinding,
		Authorization:    campaignAuthorizationFacts(prepared.authorization),
	})
	if err != nil {
		return "", err
	}
	return campaignAuthoritySHA256(payload), nil
}

func (prepared PreparedImport) validateAuthorizedSeal() error {
	if err := prepared.validateSeal(); err != nil {
		return err
	}
	expected, err := prepared.expectedAuthorizationSeal()
	if err != nil || !campaignAuthorityDigestPattern.MatchString(prepared.authorizationSeal) ||
		prepared.authorizationSeal != expected {
		return errors.New("sealed preauthorized campaign import is required")
	}
	if err := prepared.authorization.Validate(); err != nil {
		return err
	}
	if err := validateCampaignAuthorizationConvergence(
		prepared.authorization,
		prepared.verified.Projection(),
	); err != nil {
		return err
	}
	return nil
}

func (authority VerifiedCampaignImportAuthority) verifiedInputs() (
	unirouter.CampaignPackageImportReceipt,
	unirouter.VerifiedCampaignPackage,
	integrationcontrol.N1AuthorizationVerification,
	error,
) {
	if err := authority.validate(); err != nil {
		return unirouter.CampaignPackageImportReceipt{}, unirouter.VerifiedCampaignPackage{},
			integrationcontrol.N1AuthorizationVerification{}, err
	}
	return authority.receipt, authority.verifiedPackage, authority.authorization, nil
}

func (authority VerifiedCampaignImportAuthority) validate() error {
	if err := validateCampaignImportAuthorityInputs(
		authority.receipt,
		authority.verifiedPackage,
		authority.operationBinding,
	); err != nil {
		return err
	}
	expected, err := authority.expectedSeal()
	if err != nil || !campaignAuthorityDigestPattern.MatchString(authority.verificationSeal) ||
		authority.verificationSeal != expected {
		return errors.New("sealed completed campaign import authority is required")
	}
	if err := authority.authorization.Validate(); err != nil {
		return err
	}
	if err := validateCampaignAuthorizationConvergence(
		authority.authorization,
		authority.verifiedPackage.Projection(),
	); err != nil {
		return err
	}
	return nil
}

func (authority VerifiedCampaignImportAuthority) expectedSeal() (string, error) {
	receiptSHA256, err := authority.receipt.SHA256()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(struct {
		SealVersion      string                                    `json:"seal_version"`
		OperationBinding boundedoffline.OperationBinding           `json:"operation_binding"`
		ReceiptSHA256    string                                    `json:"receipt_sha256"`
		Package          unirouter.CampaignPackageProjection       `json:"package"`
		Authorization    campaignImportAuthorizationAuthorityFacts `json:"authorization"`
	}{
		SealVersion:      verifiedImportAuthoritySealVersion,
		OperationBinding: authority.operationBinding,
		ReceiptSHA256:    receiptSHA256,
		Package:          authority.verifiedPackage.Projection(),
		Authorization:    campaignAuthorizationFacts(authority.authorization),
	})
	if err != nil {
		return "", err
	}
	return campaignAuthoritySHA256(payload), nil
}

type campaignImportAuthorizationAuthorityFacts struct {
	Valid                        bool                                               `json:"valid"`
	CampaignManifestSHA256       string                                             `json:"campaign_manifest_sha256"`
	AcknowledgementSHA256        map[string]string                                  `json:"acknowledgement_sha256"`
	AuthorizationSHA256          string                                             `json:"authorization_sha256"`
	AuthorizationSignatureSHA256 string                                             `json:"authorization_signature_sha256"`
	AuthorizationTrustSetSHA256  string                                             `json:"authorization_trust_set_sha256"`
	SourceEvidenceTrustSetSHA256 string                                             `json:"source_evidence_trust_set_sha256"`
	CapabilityBoundary           string                                             `json:"capability_boundary"`
	UniRouterComponent           integrationcontrol.N1ComponentIdentity             `json:"unirouter_component"`
	SourceBinding                integrationcontrol.ArtifactReference               `json:"source_binding"`
	SourceEvidenceSigning        integrationcontrol.N1SourceEvidenceSigningIdentity `json:"source_evidence_signing"`
	AuthorizationWindow          integrationcontrol.N1AuthorizationWindow           `json:"authorization_window"`
}

func campaignAuthorizationFacts(
	authorization integrationcontrol.N1AuthorizationVerification,
) campaignImportAuthorizationAuthorityFacts {
	acknowledgements := make(map[string]string, len(authorization.AcknowledgementSHA256))
	for product, digest := range authorization.AcknowledgementSHA256 {
		acknowledgements[product] = digest
	}
	return campaignImportAuthorizationAuthorityFacts{
		Valid:                        authorization.Valid,
		CampaignManifestSHA256:       authorization.CampaignManifestSHA256,
		AcknowledgementSHA256:        acknowledgements,
		AuthorizationSHA256:          authorization.AuthorizationSHA256,
		AuthorizationSignatureSHA256: authorization.AuthorizationSignatureSHA256,
		AuthorizationTrustSetSHA256:  authorization.AuthorizationTrustSetSHA256,
		SourceEvidenceTrustSetSHA256: authorization.SourceEvidenceTrustSetSHA256,
		CapabilityBoundary:           authorization.CapabilityBoundary,
		UniRouterComponent:           authorization.UniRouterComponent,
		SourceBinding:                authorization.SourceBinding,
		SourceEvidenceSigning:        authorization.SourceEvidenceSigning,
		AuthorizationWindow:          authorization.AuthorizationWindow,
	}
}

func cloneCampaignAuthorization(
	authorization integrationcontrol.N1AuthorizationVerification,
) integrationcontrol.N1AuthorizationVerification {
	clone := authorization
	clone.AcknowledgementSHA256 = make(map[string]string, len(authorization.AcknowledgementSHA256))
	for product, digest := range authorization.AcknowledgementSHA256 {
		clone.AcknowledgementSHA256[product] = digest
	}
	return clone
}

func validateCampaignAuthorizationConvergence(
	authorization integrationcontrol.N1AuthorizationVerification,
	projection unirouter.CampaignPackageProjection,
) error {
	expectedSignature := unirouter.SourceEvidenceSignatureIdentity{
		Purpose:           authorization.SourceEvidenceSigning.Purpose,
		Domain:            authorization.SourceEvidenceSigning.Domain,
		Algorithm:         unirouter.SourceEvidenceSignatureEd25519,
		SigningKeyRef:     authorization.SourceEvidenceSigning.SigningKeyRef,
		SignerPublicKeyID: authorization.SourceEvidenceSigning.SignerPublicKeyID,
		TrustSetSHA256:    authorization.SourceEvidenceSigning.TrustSetSHA256,
	}
	if !authorization.Valid || authorization.CapabilityBoundary != integrationcontrol.CapabilityOfflineFiles ||
		projection.Contract.ID != authorization.UniRouterComponent.Contract.ID ||
		projection.Contract.SHA256 != authorization.UniRouterComponent.Contract.SHA256 ||
		projection.AuthorizationManifestSHA256 != authorization.CampaignManifestSHA256 ||
		projection.AuthorizationSHA256 != authorization.AuthorizationSHA256 ||
		projection.AuthorizationSignatureSHA256 != authorization.AuthorizationSignatureSHA256 ||
		projection.SourceBindingSHA256 != authorization.SourceBinding.SHA256 ||
		projection.Signature != expectedSignature ||
		authorization.SourceEvidenceTrustSetSHA256 != authorization.SourceEvidenceSigning.TrustSetSHA256 {
		return errors.New("campaign import package and sealed N1 authorization do not converge")
	}
	return nil
}

func validateCampaignImportAuthorityInputs(
	receipt unirouter.CampaignPackageImportReceipt,
	verifiedPackage unirouter.VerifiedCampaignPackage,
	binding boundedoffline.OperationBinding,
) error {
	if err := receipt.Validate(); err != nil {
		return err
	}
	if binding.ConnectorID != receipt.OperationalReceipt.ConnectorID ||
		binding.OperationID != receipt.OperationID ||
		!campaignAuthorityDigestPattern.MatchString(binding.RequestSHA256) {
		return errors.New("campaign import operation binding does not match its completed receipt")
	}
	projection := verifiedPackage.Projection()
	if receipt.Contract != projection.Contract || receipt.PackageID != projection.PackageID ||
		receipt.PackageSHA256 != projection.PackageSHA256 ||
		receipt.PackageSizeBytes != projection.PackageSizeBytes ||
		receipt.ManifestSHA256 != projection.ManifestSHA256 ||
		receipt.PackageScopeComplete != projection.PackageScopeComplete ||
		receipt.SourceBindingSHA256 != projection.SourceBindingSHA256 ||
		receipt.AuthorizationManifestSHA256 != projection.AuthorizationManifestSHA256 ||
		receipt.AuthorizationSHA256 != projection.AuthorizationSHA256 ||
		receipt.AuthorizationSignatureSHA256 != projection.AuthorizationSignatureSHA256 ||
		receipt.Signature != projection.Signature ||
		receipt.OperationalReceipt.ProducerInstance != projection.SourceInstance ||
		len(receipt.Members) != len(projection.Members) {
		return errors.New("campaign import receipt does not match its verified package")
	}
	for index, member := range receipt.Members {
		inventory := projection.Members[index]
		if member.MemberID != inventory.MemberID || member.Path != inventory.Path ||
			member.SHA256 != inventory.SHA256 || member.SizeBytes != inventory.SizeBytes {
			return errors.New("campaign import receipt member inventory does not match its verified package")
		}
	}
	return nil
}

func campaignAuthoritySHA256(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}
