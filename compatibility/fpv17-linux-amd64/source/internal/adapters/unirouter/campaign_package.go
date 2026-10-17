package unirouter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// SourceEvidencePurpose is deliberately separate from every release and
	// upgrade authority. A producer contract adapter may reuse cryptographic
	// primitives, but it must verify against this product-local trust policy.
	SourceEvidencePurpose          = "source_evidence"
	SourceEvidenceDomain           = "unirouter.source-evidence.component.v1"
	SourceEvidenceSignatureEd25519 = "ed25519"

	DefaultCampaignPackageMaxBytes       = 64 << 20
	DefaultCampaignPackageMaxMembers     = 256
	DefaultCampaignPackageMaxMemberBytes = 64 << 20
)

var (
	campaignPackageDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	campaignPackageKeyIDPattern  = regexp.MustCompile(`^ed25519:sha256:[a-f0-9]{64}$`)
)

// CampaignPackageContractIdentity pins one immutable producer-owned contract.
// It is an X-Insight verification input, not a copy of the producer manifest
// schema. The concrete parser and signature preimage remain isolated in the
// adapter that pins the immutable producer contract and fixtures.
type CampaignPackageContractIdentity struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

// SourceEvidenceTrustPolicy is intentionally not compatible with an
// X-Insight release trust store. The caller has to provide an exact
// source-evidence purpose, domain and immutable trust pin.
type SourceEvidenceTrustPolicy struct {
	Purpose           string `json:"purpose"`
	Domain            string `json:"domain"`
	Algorithm         string `json:"algorithm"`
	SigningKeyRef     string `json:"signing_key_ref"`
	SignerPublicKeyID string `json:"signer_public_key_id"`
	TrustSetSHA256    string `json:"trust_set_sha256"`
}

// SourceEvidenceSignatureIdentity is the non-secret identity returned after
// the producer contract adapter has cryptographically verified the envelope.
type SourceEvidenceSignatureIdentity struct {
	Purpose           string `json:"purpose"`
	Domain            string `json:"domain"`
	Algorithm         string `json:"algorithm"`
	SigningKeyRef     string `json:"signing_key_ref"`
	SignerPublicKeyID string `json:"signer_public_key_id"`
	TrustSetSHA256    string `json:"trust_set_sha256"`
}

type CampaignPackageMemberInventory struct {
	MemberID  string `json:"member_id"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

// CampaignPackageProjection is the minimal, payload-neutral result that a
// producer-contract adapter must return. It deliberately does not define or
// decode the producer wire manifest.
type CampaignPackageProjection struct {
	Contract                     CampaignPackageContractIdentity  `json:"contract"`
	PackageID                    string                           `json:"package_id"`
	PackageSHA256                string                           `json:"package_sha256"`
	PackageSizeBytes             int64                            `json:"package_size_bytes"`
	ManifestSHA256               string                           `json:"manifest_sha256"`
	PackageCreatedAt             string                           `json:"package_created_at"`
	PackageScopeComplete         bool                             `json:"package_scope_complete"`
	SourceInstance               string                           `json:"source_instance"`
	SourceBindingSHA256          string                           `json:"source_binding_sha256"`
	AuthorizationManifestSHA256  string                           `json:"authorization_manifest_sha256"`
	AuthorizationSHA256          string                           `json:"authorization_sha256"`
	AuthorizationSignatureSHA256 string                           `json:"authorization_signature_sha256"`
	Signature                    SourceEvidenceSignatureIdentity  `json:"signature"`
	Members                      []CampaignPackageMemberInventory `json:"members"`
}

// OpenedCampaignPackage contains member bytes only in memory. The bytes are
// never included in a typed receipt.
type OpenedCampaignPackage struct {
	Projection     CampaignPackageProjection
	MemberPayloads map[string][]byte
}

// CampaignPackageContractAdapter is the only place allowed to understand the
// producer-owned envelope, canonical manifest and signature preimage. A
// concrete implementation must be bound to immutable contract bytes and
// fixtures; X-Insight must not invent or vendor an editable producer schema.
type CampaignPackageContractAdapter interface {
	ContractIdentity() CampaignPackageContractIdentity
	VerifyAndOpen([]byte, SourceEvidenceTrustPolicy) (OpenedCampaignPackage, error)
}

type CampaignPackageVerificationLimits struct {
	MaxPackageBytes int64
	MaxMembers      int
	MaxMemberBytes  int64
}

type CampaignPackageVerificationRequest struct {
	ExpectedContract CampaignPackageContractIdentity
	// ExpectedPackageID and ExpectedPackageSHA256 are observations of the
	// transferred object. They are not fields of M or A and do not become
	// preauthorized campaign scope merely because verification requires them.
	ExpectedPackageID                    string
	ExpectedPackageSHA256                string
	ExpectedSourceBindingSHA256          string
	ExpectedAuthorizationManifestSHA256  string
	ExpectedAuthorizationSHA256          string
	ExpectedAuthorizationSignatureSHA256 string
	Trust                                SourceEvidenceTrustPolicy
	Limits                               CampaignPackageVerificationLimits
}

type VerifiedCampaignPackage struct {
	projection     CampaignPackageProjection
	memberPayloads map[string][]byte
}

func (verified VerifiedCampaignPackage) Projection() CampaignPackageProjection {
	return cloneCampaignPackageProjection(verified.projection)
}

func (verified VerifiedCampaignPackage) MemberPayload(memberID string) ([]byte, bool) {
	payload, exists := verified.memberPayloads[memberID]
	return bytes.Clone(payload), exists
}

func VerifyCampaignPackage(
	packageBytes []byte,
	request CampaignPackageVerificationRequest,
	adapter CampaignPackageContractAdapter,
) (VerifiedCampaignPackage, error) {
	if adapter == nil {
		return VerifiedCampaignPackage{}, errors.New("immutable campaign package contract adapter is required")
	}
	limits := request.Limits.withDefaults()
	if err := limits.validate(); err != nil {
		return VerifiedCampaignPackage{}, err
	}
	if len(packageBytes) == 0 || int64(len(packageBytes)) > limits.MaxPackageBytes {
		return VerifiedCampaignPackage{}, errors.New("campaign package object exceeds the product-local bound")
	}
	if err := request.validate(); err != nil {
		return VerifiedCampaignPackage{}, err
	}
	if adapter.ContractIdentity() != request.ExpectedContract {
		return VerifiedCampaignPackage{}, errors.New("campaign package contract adapter identity does not match the immutable pin")
	}
	packageSHA256 := campaignPackageSHA256(packageBytes)
	if packageSHA256 != request.ExpectedPackageSHA256 {
		return VerifiedCampaignPackage{}, errors.New("campaign package object digest does not match the observed transfer identity")
	}
	opened, err := adapter.VerifyAndOpen(packageBytes, request.Trust)
	if err != nil {
		return VerifiedCampaignPackage{}, fmt.Errorf("verify producer-owned campaign package contract: %w", err)
	}
	if err := validateOpenedCampaignPackage(opened, request, limits, packageSHA256, int64(len(packageBytes))); err != nil {
		return VerifiedCampaignPackage{}, err
	}
	return VerifiedCampaignPackage{
		projection:     cloneCampaignPackageProjection(opened.Projection),
		memberPayloads: cloneCampaignPackagePayloads(opened.MemberPayloads),
	}, nil
}

func (request CampaignPackageVerificationRequest) validate() error {
	if err := request.ExpectedContract.validate(); err != nil {
		return err
	}
	if !validCampaignPackageOpaqueText(request.ExpectedPackageID, 1, 160) ||
		!campaignPackageDigestPattern.MatchString(request.ExpectedPackageSHA256) ||
		!campaignPackageDigestPattern.MatchString(request.ExpectedSourceBindingSHA256) ||
		!campaignPackageDigestPattern.MatchString(request.ExpectedAuthorizationManifestSHA256) ||
		!campaignPackageDigestPattern.MatchString(request.ExpectedAuthorizationSHA256) ||
		!campaignPackageDigestPattern.MatchString(request.ExpectedAuthorizationSignatureSHA256) {
		return errors.New("campaign package authorization binding is invalid")
	}
	return request.Trust.validate()
}

func (identity CampaignPackageContractIdentity) validate() error {
	if !validCampaignPackageOpaqueText(identity.ID, 1, 160) ||
		!campaignPackageDigestPattern.MatchString(identity.SHA256) {
		return errors.New("campaign package contract identity is invalid")
	}
	return nil
}

func (policy SourceEvidenceTrustPolicy) validate() error {
	if policy.Purpose != SourceEvidencePurpose ||
		policy.Domain != SourceEvidenceDomain ||
		policy.Algorithm != SourceEvidenceSignatureEd25519 ||
		!validCampaignPackageOpaqueText(policy.SigningKeyRef, 1, 160) ||
		!strings.HasPrefix(policy.SigningKeyRef, "source-evidence-") ||
		!campaignPackageKeyIDPattern.MatchString(policy.SignerPublicKeyID) ||
		!campaignPackageDigestPattern.MatchString(policy.TrustSetSHA256) {
		return errors.New("campaign package source-evidence trust policy is invalid")
	}
	return nil
}

func (identity SourceEvidenceSignatureIdentity) validate(policy SourceEvidenceTrustPolicy) error {
	if err := policy.validate(); err != nil {
		return err
	}
	if identity.Purpose != policy.Purpose || identity.Domain != policy.Domain ||
		identity.Algorithm != policy.Algorithm || identity.SigningKeyRef != policy.SigningKeyRef ||
		identity.SignerPublicKeyID != policy.SignerPublicKeyID ||
		identity.TrustSetSHA256 != policy.TrustSetSHA256 {
		return errors.New("campaign package signature did not use the exact source-evidence authority")
	}
	return nil
}

func (limits CampaignPackageVerificationLimits) withDefaults() CampaignPackageVerificationLimits {
	if limits.MaxPackageBytes == 0 {
		limits.MaxPackageBytes = DefaultCampaignPackageMaxBytes
	}
	if limits.MaxMembers == 0 {
		limits.MaxMembers = DefaultCampaignPackageMaxMembers
	}
	if limits.MaxMemberBytes == 0 {
		limits.MaxMemberBytes = DefaultCampaignPackageMaxMemberBytes
	}
	return limits
}

func (limits CampaignPackageVerificationLimits) validate() error {
	if limits.MaxPackageBytes < 1 || limits.MaxPackageBytes > DefaultCampaignPackageMaxBytes ||
		limits.MaxMembers < 1 || limits.MaxMembers > DefaultCampaignPackageMaxMembers ||
		limits.MaxMemberBytes < 1 || limits.MaxMemberBytes > DefaultCampaignPackageMaxMemberBytes ||
		limits.MaxMemberBytes > limits.MaxPackageBytes {
		return errors.New("campaign package verification limits are invalid")
	}
	return nil
}

func validateOpenedCampaignPackage(
	opened OpenedCampaignPackage,
	request CampaignPackageVerificationRequest,
	limits CampaignPackageVerificationLimits,
	packageSHA256 string,
	packageSize int64,
) error {
	projection := opened.Projection
	if projection.Contract != request.ExpectedContract ||
		projection.PackageID != request.ExpectedPackageID ||
		projection.PackageSHA256 != packageSHA256 ||
		projection.PackageSizeBytes != packageSize ||
		!validCampaignPackageCanonicalTime(projection.PackageCreatedAt) ||
		!validCampaignPackageOpaqueText(projection.SourceInstance, 1, 128) ||
		projection.SourceBindingSHA256 != request.ExpectedSourceBindingSHA256 ||
		projection.AuthorizationManifestSHA256 != request.ExpectedAuthorizationManifestSHA256 ||
		projection.AuthorizationSHA256 != request.ExpectedAuthorizationSHA256 ||
		projection.AuthorizationSignatureSHA256 != request.ExpectedAuthorizationSignatureSHA256 ||
		!campaignPackageDigestPattern.MatchString(projection.ManifestSHA256) {
		return errors.New("campaign package projection does not match its preauthorization or observed package identity")
	}
	if !projection.PackageScopeComplete {
		return errors.New("campaign package does not completely cover its frozen package scope")
	}
	if err := projection.Signature.validate(request.Trust); err != nil {
		return err
	}
	if len(projection.Members) == 0 || len(projection.Members) > limits.MaxMembers ||
		len(opened.MemberPayloads) != len(projection.Members) {
		return errors.New("campaign package member inventory is incomplete or exceeds its bound")
	}
	seenIDs := make(map[string]struct{}, len(projection.Members))
	seenPaths := make(map[string]struct{}, len(projection.Members))
	var totalMemberBytes int64
	previousPath := ""
	for index, member := range projection.Members {
		if err := validateCampaignPackageMemberInventory(member, limits); err != nil {
			return fmt.Errorf("campaign package member[%d]: %w", index, err)
		}
		if previousPath != "" && previousPath >= member.Path {
			return errors.New("campaign package member inventory is not in strict path order")
		}
		previousPath = member.Path
		if _, exists := seenIDs[member.MemberID]; exists {
			return errors.New("campaign package member inventory contains a duplicate member identity")
		}
		seenIDs[member.MemberID] = struct{}{}
		if _, exists := seenPaths[member.Path]; exists {
			return errors.New("campaign package member inventory contains a duplicate path")
		}
		seenPaths[member.Path] = struct{}{}
		payload, exists := opened.MemberPayloads[member.MemberID]
		if !exists || int64(len(payload)) != member.SizeBytes ||
			campaignPackageSHA256(payload) != member.SHA256 {
			return errors.New("campaign package member bytes do not match the content-addressed inventory")
		}
		totalMemberBytes += member.SizeBytes
		if totalMemberBytes > limits.MaxPackageBytes {
			return errors.New("campaign package member bytes exceed the aggregate product-local bound")
		}
	}
	for memberID := range opened.MemberPayloads {
		if _, exists := seenIDs[memberID]; !exists {
			return errors.New("campaign package contains an unmanifested member payload")
		}
	}
	return nil
}

func validCampaignPackageCanonicalTime(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.Location() == time.UTC &&
		value == parsed.UTC().Format(time.RFC3339Nano)
}

func validateCampaignPackageMemberInventory(
	member CampaignPackageMemberInventory,
	limits CampaignPackageVerificationLimits,
) error {
	if !validCampaignPackageOpaqueText(member.MemberID, 1, 160) ||
		!validCampaignPackageMemberPath(member.Path) ||
		!campaignPackageDigestPattern.MatchString(member.SHA256) ||
		member.SizeBytes <= 0 || member.SizeBytes > limits.MaxMemberBytes {
		return errors.New("content-addressed member identity is invalid")
	}
	return nil
}

func validCampaignPackageMemberPath(value string) bool {
	if !validCampaignPackageOpaqueText(value, 1, 512) || strings.Contains(value, "\\") ||
		strings.ContainsRune(value, '\x00') || strings.HasPrefix(value, "/") ||
		path.Clean(value) != value || value == "." || strings.HasPrefix(value, "../") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func validCampaignPackageOpaqueText(value string, minimum, maximum int) bool {
	if !utf8.ValidString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum || value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func campaignPackageSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func cloneCampaignPackageProjection(value CampaignPackageProjection) CampaignPackageProjection {
	value.Members = append([]CampaignPackageMemberInventory(nil), value.Members...)
	return value
}

func cloneCampaignPackagePayloads(values map[string][]byte) map[string][]byte {
	result := make(map[string][]byte, len(values))
	for key, value := range values {
		result[key] = bytes.Clone(value)
	}
	return result
}

func sortedCampaignPackageMemberInventory(values []CampaignPackageMemberInventory) []CampaignPackageMemberInventory {
	result := append([]CampaignPackageMemberInventory(nil), values...)
	sort.Slice(result, func(left, right int) bool {
		return result[left].Path < result[right].Path
	})
	return result
}
