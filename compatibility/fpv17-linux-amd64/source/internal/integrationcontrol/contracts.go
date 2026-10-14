package integrationcontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	CanonicalizationVersion = "foigent.integration-control.canonical-json.v1"
	CampaignManifestVersion = "foigent.integration-campaign-manifest.v1"
	AcknowledgementVersion  = "foigent.integration-campaign-component-ack.v1"

	ComponentContractVersion     = "xinsight.signed-campaign-package-consumer.v1"
	ComponentScopeVersion        = "xinsight.integration-campaign-component-scope-receipt.v1"
	ComponentVerificationVersion = "xinsight.signed-campaign-package-component-verification-receipt.v1"
	VerifierReleaseVersion       = "xinsight.signed-campaign-package-consumer-verifier.v1"

	CampaignProfile          = "poc_source_evidence"
	ProductXInsight          = "xinsight"
	ComponentRole            = "source_evidence_consumer"
	ImportModeBoundedOffline = "bounded_offline"
	CapabilityOfflineFiles   = "offline_files_only"
	StatusAccepted           = "accepted"
)

var (
	digestPattern    = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	gitObjectPattern = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)
	portablePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
)

type ArtifactReference struct {
	SchemaVersion string `json:"schema_version"`
	SHA256        string `json:"sha256"`
}

type ContractReference struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

// ComponentContract is X-Insight's product authority. It describes only the
// bounded consumer capability and deliberately does not define the producer
// package wire format.
type ComponentContract struct {
	SchemaVersion                        string `json:"schema_version"`
	Profile                              string `json:"profile"`
	Role                                 string `json:"role"`
	ImportMode                           string `json:"import_mode"`
	CapabilityBoundary                   string `json:"capability_boundary"`
	PackageScopeCompleteRequired         bool   `json:"package_scope_complete_required"`
	PartialMemberSourceProfilesPreserved bool   `json:"partial_member_source_profiles_preserved"`
	ExplicitCaptureGapsRequired          bool   `json:"explicit_capture_gaps_required"`
	RuntimeCloseMarkerRequired           bool   `json:"runtime_close_marker_required"`
}

func NewComponentContract() ComponentContract {
	return ComponentContract{
		SchemaVersion:                        ComponentContractVersion,
		Profile:                              CampaignProfile,
		Role:                                 ComponentRole,
		ImportMode:                           ImportModeBoundedOffline,
		CapabilityBoundary:                   CapabilityOfflineFiles,
		PackageScopeCompleteRequired:         true,
		PartialMemberSourceProfilesPreserved: true,
		ExplicitCaptureGapsRequired:          true,
		RuntimeCloseMarkerRequired:           false,
	}
}

func (value ComponentContract) Validate() error {
	if value.SchemaVersion != ComponentContractVersion ||
		value.Profile != CampaignProfile ||
		value.Role != ComponentRole ||
		value.ImportMode != ImportModeBoundedOffline ||
		value.CapabilityBoundary != CapabilityOfflineFiles ||
		!value.PackageScopeCompleteRequired ||
		!value.PartialMemberSourceProfilesPreserved ||
		!value.ExplicitCaptureGapsRequired ||
		value.RuntimeCloseMarkerRequired {
		return errors.New("X-Insight signed-package consumer contract is invalid")
	}
	return nil
}

func (value ComponentContract) CanonicalBytes() ([]byte, error) {
	return canonicalProductArtifact(value, value.Validate)
}

func (value ComponentContract) SHA256() (string, error) {
	return artifactSHA256(value.CanonicalBytes())
}

type VerifierRelease struct {
	SchemaVersion                  string `json:"schema_version"`
	Product                        string `json:"product"`
	Commit                         string `json:"commit"`
	Version                        string `json:"version"`
	ComponentContractSHA256        string `json:"component_contract_sha256"`
	ProductReleaseSHA256           string `json:"product_release_sha256"`
	SignedReleaseManifestSHA256    string `json:"signed_release_manifest_sha256"`
	ImporterExecutableSHA256       string `json:"importer_executable_sha256"`
	M0EvidenceSchemaSHA256         string `json:"m0_evidence_schema_sha256"`
	NeutralN1ReleaseManifestSHA256 string `json:"neutral_n1_release_manifest_sha256"`
}

type VerifierReleaseInput struct {
	Commit                      string
	Version                     string
	ComponentContractSHA256     string
	ProductReleaseSHA256        string
	SignedReleaseManifestSHA256 string
	ImporterExecutableSHA256    string
	M0EvidenceSchemaSHA256      string
}

func NewVerifierRelease(input VerifierReleaseInput) (VerifierRelease, error) {
	value := VerifierRelease{
		SchemaVersion:                  VerifierReleaseVersion,
		Product:                        ProductXInsight,
		Commit:                         input.Commit,
		Version:                        input.Version,
		ComponentContractSHA256:        input.ComponentContractSHA256,
		ProductReleaseSHA256:           input.ProductReleaseSHA256,
		SignedReleaseManifestSHA256:    input.SignedReleaseManifestSHA256,
		ImporterExecutableSHA256:       input.ImporterExecutableSHA256,
		M0EvidenceSchemaSHA256:         input.M0EvidenceSchemaSHA256,
		NeutralN1ReleaseManifestSHA256: FrozenN1ReleaseManifestSHA256,
	}
	return value, value.Validate()
}

func (value VerifierRelease) Validate() error {
	if value.SchemaVersion != VerifierReleaseVersion ||
		value.Product != ProductXInsight ||
		!gitObjectPattern.MatchString(value.Commit) ||
		!validPortableText(value.Version, 128) ||
		!digestPattern.MatchString(value.ComponentContractSHA256) ||
		!digestPattern.MatchString(value.ProductReleaseSHA256) ||
		!digestPattern.MatchString(value.SignedReleaseManifestSHA256) ||
		!digestPattern.MatchString(value.ImporterExecutableSHA256) ||
		value.M0EvidenceSchemaSHA256 != M0EvidenceSchemaSHA256 ||
		value.NeutralN1ReleaseManifestSHA256 != FrozenN1ReleaseManifestSHA256 {
		return errors.New("X-Insight component-verifier release identity is invalid")
	}
	return nil
}

func (value VerifierRelease) CanonicalBytes() ([]byte, error) {
	return canonicalProductArtifact(value, value.Validate)
}

func (value VerifierRelease) SHA256() (string, error) {
	return artifactSHA256(value.CanonicalBytes())
}

type ComponentScopeReceipt struct {
	SchemaVersion                        string            `json:"schema_version"`
	Canonicalization                     string            `json:"canonicalization"`
	CampaignID                           string            `json:"campaign_id"`
	Product                              string            `json:"product"`
	Status                               string            `json:"status"`
	ComponentContract                    ContractReference `json:"component_contract"`
	ProductRelease                       ArtifactReference `json:"product_release"`
	VerifierRelease                      ContractReference `json:"verifier_release"`
	ImportMode                           string            `json:"import_mode"`
	PackageScopeCompleteRequired         bool              `json:"package_scope_complete_required"`
	PartialMemberSourceProfilesPreserved bool              `json:"partial_member_source_profiles_preserved"`
	ExplicitCaptureGapsRequired          bool              `json:"explicit_capture_gaps_required"`
	ZeroTransportAtVerification          bool              `json:"zero_transport_at_verification"`
	IssuedAt                             string            `json:"issued_at"`
}

type ComponentScopeInput struct {
	CampaignID              string
	ComponentContractSHA256 string
	ProductRelease          ArtifactReference
	VerifierReleaseSHA256   string
	IssuedAt                time.Time
}

func NewComponentScopeReceipt(input ComponentScopeInput) (ComponentScopeReceipt, error) {
	issuedAt, err := canonicalTime(input.IssuedAt)
	if err != nil {
		return ComponentScopeReceipt{}, err
	}
	value := ComponentScopeReceipt{
		SchemaVersion:                        ComponentScopeVersion,
		Canonicalization:                     CanonicalizationVersion,
		CampaignID:                           input.CampaignID,
		Product:                              ProductXInsight,
		Status:                               StatusAccepted,
		ComponentContract:                    ContractReference{ID: ComponentContractVersion, SHA256: input.ComponentContractSHA256},
		ProductRelease:                       input.ProductRelease,
		VerifierRelease:                      ContractReference{ID: VerifierReleaseVersion, SHA256: input.VerifierReleaseSHA256},
		ImportMode:                           ImportModeBoundedOffline,
		PackageScopeCompleteRequired:         true,
		PartialMemberSourceProfilesPreserved: true,
		ExplicitCaptureGapsRequired:          true,
		ZeroTransportAtVerification:          true,
		IssuedAt:                             issuedAt,
	}
	return value, value.Validate()
}

func (value ComponentScopeReceipt) Validate() error {
	if value.SchemaVersion != ComponentScopeVersion ||
		value.Canonicalization != CanonicalizationVersion ||
		!validPortableText(value.CampaignID, 128) ||
		value.Product != ProductXInsight ||
		value.Status != StatusAccepted ||
		value.ComponentContract.ID != ComponentContractVersion ||
		value.VerifierRelease.ID != VerifierReleaseVersion ||
		value.ImportMode != ImportModeBoundedOffline ||
		!value.PackageScopeCompleteRequired ||
		!value.PartialMemberSourceProfilesPreserved ||
		!value.ExplicitCaptureGapsRequired ||
		!value.ZeroTransportAtVerification {
		return errors.New("X-Insight campaign component scope receipt is invalid")
	}
	if err := value.ComponentContract.validate(); err != nil {
		return err
	}
	if err := value.ProductRelease.validate(); err != nil {
		return err
	}
	if err := value.VerifierRelease.validate(); err != nil {
		return err
	}
	return validateCanonicalTimestamp(value.IssuedAt)
}

func (value ComponentScopeReceipt) CanonicalBytes() ([]byte, error) {
	return canonicalProductArtifact(value, value.Validate)
}

func (value ComponentScopeReceipt) SHA256() (string, error) {
	return artifactSHA256(value.CanonicalBytes())
}

// ComponentVerificationReceipt is generated only after M exists. Its closed
// shape prevents A, P, the eventual X import receipt, or R from flowing back
// into the component acknowledgement phase.
type ComponentVerificationReceipt struct {
	SchemaVersion                  string            `json:"schema_version"`
	Canonicalization               string            `json:"canonicalization"`
	CampaignID                     string            `json:"campaign_id"`
	Product                        string            `json:"product"`
	Status                         string            `json:"status"`
	CampaignManifestSHA256         string            `json:"campaign_manifest_sha256"`
	ComponentContract              ContractReference `json:"component_contract"`
	ComponentScopeReceipt          ArtifactReference `json:"component_scope_receipt"`
	ProductRelease                 ArtifactReference `json:"product_release"`
	VerifierRelease                ContractReference `json:"verifier_release"`
	NeutralN1ReleaseManifestSHA256 string            `json:"neutral_n1_release_manifest_sha256"`
	ZeroTransportAtVerification    bool              `json:"zero_transport_at_verification"`
	VerifiedAt                     string            `json:"verified_at"`
	constructionAuthorized         bool
}

// NewComponentVerificationLifecycle is the only product generator for a
// status=accepted verification receipt. It derives every product identity from
// the pre-M scope receipt and exact N1-sealed M; callers cannot supply a second
// release, contract, scope, or manifest identity.
func NewComponentVerificationLifecycle(
	scope ComponentScopeReceipt,
	manifest N1ManifestVerification,
	verifiedTime time.Time,
) (VerifiedComponentLifecycle, error) {
	if err := scope.Validate(); err != nil {
		return VerifiedComponentLifecycle{}, err
	}
	if err := manifest.validateVerified(); err != nil {
		return VerifiedComponentLifecycle{}, err
	}
	verifiedAt, err := canonicalTime(verifiedTime)
	if err != nil {
		return VerifiedComponentLifecycle{}, err
	}
	scopeSHA256, err := scope.SHA256()
	if err != nil {
		return VerifiedComponentLifecycle{}, err
	}
	value := ComponentVerificationReceipt{
		SchemaVersion:                  ComponentVerificationVersion,
		Canonicalization:               CanonicalizationVersion,
		CampaignID:                     scope.CampaignID,
		Product:                        ProductXInsight,
		Status:                         StatusAccepted,
		CampaignManifestSHA256:         manifest.CampaignManifestSHA256,
		ComponentContract:              scope.ComponentContract,
		ComponentScopeReceipt:          ArtifactReference{SchemaVersion: ComponentScopeVersion, SHA256: scopeSHA256},
		ProductRelease:                 scope.ProductRelease,
		VerifierRelease:                scope.VerifierRelease,
		NeutralN1ReleaseManifestSHA256: FrozenN1ReleaseManifestSHA256,
		ZeroTransportAtVerification:    true,
		VerifiedAt:                     verifiedAt,
	}
	return VerifyComponentLifecycle(scope, manifest, value)
}

func (value ComponentVerificationReceipt) Validate() error {
	if value.SchemaVersion != ComponentVerificationVersion ||
		value.Canonicalization != CanonicalizationVersion ||
		!validPortableText(value.CampaignID, 128) ||
		value.Product != ProductXInsight ||
		value.Status != StatusAccepted ||
		!digestPattern.MatchString(value.CampaignManifestSHA256) ||
		value.ComponentContract.ID != ComponentContractVersion ||
		value.ComponentScopeReceipt.SchemaVersion != ComponentScopeVersion ||
		value.VerifierRelease.ID != VerifierReleaseVersion ||
		value.NeutralN1ReleaseManifestSHA256 != FrozenN1ReleaseManifestSHA256 ||
		!value.ZeroTransportAtVerification {
		return errors.New("X-Insight campaign component verification receipt is invalid")
	}
	if err := value.ComponentContract.validate(); err != nil {
		return err
	}
	if err := value.ComponentScopeReceipt.validate(); err != nil {
		return err
	}
	if err := value.ProductRelease.validate(); err != nil {
		return err
	}
	if err := value.VerifierRelease.validate(); err != nil {
		return err
	}
	return validateCanonicalTimestamp(value.VerifiedAt)
}

func (value ComponentVerificationReceipt) ValidateAgainstScope(
	scope ComponentScopeReceipt,
) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	scopeSHA256, err := scope.SHA256()
	if err != nil {
		return err
	}
	if value.CampaignID != scope.CampaignID ||
		value.ComponentContract != scope.ComponentContract ||
		value.ComponentScopeReceipt.SHA256 != scopeSHA256 ||
		value.ProductRelease != scope.ProductRelease ||
		value.VerifierRelease != scope.VerifierRelease {
		return errors.New("component verification receipt contradicts its pre-manifest scope receipt")
	}
	issuedAt, _ := time.Parse(time.RFC3339, scope.IssuedAt)
	verifiedAt, _ := time.Parse(time.RFC3339, value.VerifiedAt)
	if verifiedAt.Before(issuedAt) {
		return errors.New("component verification predates its scope receipt")
	}
	return nil
}

func (value ComponentVerificationReceipt) ValidateLifecycle(
	scope ComponentScopeReceipt,
	manifest N1ManifestVerification,
) error {
	_, err := VerifyComponentLifecycle(scope, manifest, value)
	return err
}

// VerifiedComponentLifecycle is the mandatory construction authority for an
// accepted acknowledgement. Its state is private so callers cannot bypass the
// scope -> M product tuple closure with a merely well-formed receipt.
type VerifiedComponentLifecycle struct {
	verification ComponentVerificationReceipt
	manifestSHA  string
	valid        bool
}

func (lifecycle VerifiedComponentLifecycle) VerificationReceipt() (ComponentVerificationReceipt, error) {
	if !lifecycle.valid || lifecycle.manifestSHA != lifecycle.verification.CampaignManifestSHA256 {
		return ComponentVerificationReceipt{}, errors.New("verified component lifecycle authority is required")
	}
	return lifecycle.verification, lifecycle.verification.Validate()
}

func VerifyComponentLifecycle(
	scope ComponentScopeReceipt,
	manifest N1ManifestVerification,
	verification ComponentVerificationReceipt,
) (VerifiedComponentLifecycle, error) {
	if err := manifest.validateVerified(); err != nil {
		return VerifiedComponentLifecycle{}, err
	}
	if err := verification.ValidateAgainstScope(scope); err != nil {
		return VerifiedComponentLifecycle{}, err
	}
	scopeSHA256, err := scope.SHA256()
	if err != nil {
		return VerifiedComponentLifecycle{}, err
	}
	expectedComponent := N1ComponentIdentity{
		Product:  ProductXInsight,
		Role:     ComponentRole,
		Contract: scope.ComponentContract,
		Release:  scope.ProductRelease,
		ScopeReceipt: ArtifactReference{
			SchemaVersion: ComponentScopeVersion,
			SHA256:        scopeSHA256,
		},
	}
	if manifest.XInsightComponent != expectedComponent {
		return VerifiedComponentLifecycle{}, errors.New("verified campaign manifest contradicts the X-Insight contract, release, or scope receipt")
	}
	if !manifest.Valid || manifest.CampaignID != scope.CampaignID ||
		manifest.CampaignManifestSHA256 != verification.CampaignManifestSHA256 ||
		validateCanonicalTimestamp(manifest.FrozenAt) != nil {
		return VerifiedComponentLifecycle{}, errors.New("component verification receipt contradicts the verified campaign manifest")
	}
	issuedAt, _ := time.Parse(time.RFC3339, scope.IssuedAt)
	frozenAt, _ := time.Parse(time.RFC3339, manifest.FrozenAt)
	verifiedAt, _ := time.Parse(time.RFC3339, verification.VerifiedAt)
	if issuedAt.After(frozenAt) || verifiedAt.Before(frozenAt) {
		return VerifiedComponentLifecycle{}, errors.New("component receipt chronology does not preserve scope-before-M-before-verification")
	}
	verification.constructionAuthorized = true
	return VerifiedComponentLifecycle{
		verification: verification,
		manifestSHA:  manifest.CampaignManifestSHA256,
		valid:        true,
	}, nil
}

func (value ComponentVerificationReceipt) CanonicalBytes() ([]byte, error) {
	if value.Status == StatusAccepted && !value.constructionAuthorized {
		return nil, errors.New("verified component lifecycle authority is required to emit an accepted verification receipt")
	}
	return canonicalProductArtifact(value, value.Validate)
}

func (value ComponentVerificationReceipt) SHA256() (string, error) {
	return artifactSHA256(canonicalProductArtifact(value, value.Validate))
}

// ComponentAcknowledgement is the narrow neutral N1 envelope. X-Insight owns
// its referenced verification receipt; N1 owns validation of this envelope
// against the exact manifest M.
type ComponentAcknowledgement struct {
	SchemaVersion              string            `json:"schema_version"`
	Product                    string            `json:"product"`
	CampaignManifestSHA256     string            `json:"campaign_manifest_sha256"`
	ComponentContract          ContractReference `json:"component_contract"`
	ComponentScopeReceipt      ArtifactReference `json:"component_scope_receipt"`
	ProductVerificationReceipt ArtifactReference `json:"product_verification_receipt"`
	VerifierRelease            ContractReference `json:"verifier_release"`
	Status                     string            `json:"status"`
	VerifiedAt                 string            `json:"verified_at"`
	constructionAuthorized     bool
}

func NewComponentAcknowledgement(
	lifecycle VerifiedComponentLifecycle,
) (ComponentAcknowledgement, error) {
	verification, err := lifecycle.VerificationReceipt()
	if err != nil {
		return ComponentAcknowledgement{}, err
	}
	verificationSHA256, err := verification.SHA256()
	if err != nil {
		return ComponentAcknowledgement{}, err
	}
	value := ComponentAcknowledgement{
		SchemaVersion:              AcknowledgementVersion,
		Product:                    ProductXInsight,
		CampaignManifestSHA256:     verification.CampaignManifestSHA256,
		ComponentContract:          verification.ComponentContract,
		ComponentScopeReceipt:      verification.ComponentScopeReceipt,
		ProductVerificationReceipt: ArtifactReference{SchemaVersion: ComponentVerificationVersion, SHA256: verificationSHA256},
		VerifierRelease:            verification.VerifierRelease,
		Status:                     StatusAccepted,
		VerifiedAt:                 verification.VerifiedAt,
		constructionAuthorized:     true,
	}
	return value, value.Validate()
}

func (value ComponentAcknowledgement) Validate() error {
	if value.SchemaVersion != AcknowledgementVersion ||
		value.Product != ProductXInsight ||
		!digestPattern.MatchString(value.CampaignManifestSHA256) ||
		value.ComponentContract.ID != ComponentContractVersion ||
		value.ComponentScopeReceipt.SchemaVersion != ComponentScopeVersion ||
		value.ProductVerificationReceipt.SchemaVersion != ComponentVerificationVersion ||
		value.VerifierRelease.ID != VerifierReleaseVersion ||
		value.Status != StatusAccepted {
		return errors.New("X-Insight campaign component acknowledgement is invalid")
	}
	if err := value.ComponentContract.validate(); err != nil {
		return err
	}
	if err := value.ComponentScopeReceipt.validate(); err != nil {
		return err
	}
	if err := value.ProductVerificationReceipt.validate(); err != nil {
		return err
	}
	if err := value.VerifierRelease.validate(); err != nil {
		return err
	}
	return validateCanonicalTimestamp(value.VerifiedAt)
}

func (value ComponentAcknowledgement) ValidateAgainstVerification(
	verification ComponentVerificationReceipt,
) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if err := verification.Validate(); err != nil {
		return err
	}
	verificationSHA256, err := verification.SHA256()
	if err != nil {
		return err
	}
	if value.CampaignManifestSHA256 != verification.CampaignManifestSHA256 ||
		value.ComponentContract != verification.ComponentContract ||
		value.ComponentScopeReceipt != verification.ComponentScopeReceipt ||
		value.ProductVerificationReceipt.SHA256 != verificationSHA256 ||
		value.VerifierRelease != verification.VerifierRelease ||
		value.VerifiedAt != verification.VerifiedAt {
		return errors.New("component acknowledgement contradicts its X-Insight verification receipt")
	}
	return nil
}

func (value ComponentAcknowledgement) CanonicalBytes() ([]byte, error) {
	if value.Status == StatusAccepted && !value.constructionAuthorized {
		return nil, errors.New("verified component lifecycle authority is required to emit an accepted acknowledgement")
	}
	return canonicalProductArtifact(value, value.Validate)
}

func (value ComponentAcknowledgement) SHA256() (string, error) {
	return artifactSHA256(canonicalProductArtifact(value, value.Validate))
}

func ParseComponentContract(payload []byte) (ComponentContract, error) {
	return parseCanonicalProductArtifact(payload, "component contract", func(value ComponentContract) error {
		return value.Validate()
	})
}

func ParseVerifierRelease(payload []byte) (VerifierRelease, error) {
	return parseCanonicalProductArtifact(payload, "verifier release", func(value VerifierRelease) error {
		return value.Validate()
	})
}

func ParseComponentScopeReceipt(payload []byte) (ComponentScopeReceipt, error) {
	return parseCanonicalProductArtifact(payload, "component scope receipt", func(value ComponentScopeReceipt) error {
		return value.Validate()
	})
}

func ParseComponentVerificationReceipt(payload []byte) (ComponentVerificationReceipt, error) {
	return parseCanonicalProductArtifact(payload, "component verification receipt", func(value ComponentVerificationReceipt) error {
		return value.Validate()
	})
}

func ParseComponentAcknowledgement(payload []byte) (ComponentAcknowledgement, error) {
	return parseCanonicalProductArtifact(payload, "component acknowledgement", func(value ComponentAcknowledgement) error {
		return value.Validate()
	})
}

func (value ArtifactReference) validate() error {
	if !validPortableText(value.SchemaVersion, 200) || !digestPattern.MatchString(value.SHA256) {
		return errors.New("artifact reference is invalid")
	}
	return nil
}

func (value ContractReference) validate() error {
	if !validPortableText(value.ID, 200) || !digestPattern.MatchString(value.SHA256) {
		return errors.New("contract reference is invalid")
	}
	return nil
}

func canonicalProductArtifact[T any](value T, validate func() error) ([]byte, error) {
	if err := validate(); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func parseCanonicalProductArtifact[T any](
	payload []byte,
	label string,
	validate func(T) error,
) (T, error) {
	var zero T
	if len(payload) == 0 || len(payload) > 1<<20 || !utf8.Valid(payload) {
		return zero, fmt.Errorf("%s size or UTF-8 encoding is invalid", label)
	}
	if err := rejectDuplicateJSONKeys(payload); err != nil {
		return zero, fmt.Errorf("%s: %w", label, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		return zero, fmt.Errorf("decode %s: %w", label, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return zero, fmt.Errorf("%s contains trailing content", label)
	}
	if err := validate(value); err != nil {
		return zero, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(payload, canonical) {
		return zero, fmt.Errorf("%s is not canonical JSON", label)
	}
	return value, nil
}

func rejectDuplicateJSONKeys(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, "$", 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains trailing content")
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder, path string, depth int) error {
	if depth > 64 {
		return errors.New("JSON nesting exceeds the product bound")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON key at %s.%s", path, key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, path+"."+key, depth+1); err != nil {
				return err
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim('}') {
			return errors.New("JSON object is not closed")
		}
	case '[':
		index := 0
		for decoder.More() {
			if err := scanJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index), depth+1); err != nil {
				return err
			}
			index++
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim(']') {
			return errors.New("JSON array is not closed")
		}
	default:
		return errors.New("JSON contains an unexpected delimiter")
	}
	return nil
}

func artifactSHA256(payload []byte, err error) (string, error) {
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validPortableText(value string, maximum int) bool {
	if !utf8.ValidString(value) || value != strings.TrimSpace(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	if length == 0 || length > maximum || !portablePattern.MatchString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func canonicalTime(value time.Time) (string, error) {
	if value.Location() != time.UTC || value.Nanosecond() != 0 {
		return "", errors.New("campaign artifact time must be UTC with whole-second precision")
	}
	return value.Format(time.RFC3339), nil
}

func validateCanonicalTimestamp(value string) error {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || parsed.Location() != time.UTC || parsed.Nanosecond() != 0 || parsed.Format(time.RFC3339) != value {
		return errors.New("campaign artifact timestamp is not canonical UTC")
	}
	return nil
}
