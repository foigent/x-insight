package uniroutercampaign

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/integrationcontrol"
)

// CampaignTerminalProjection is the authoritative product emitter. Its public
// wire fields are the integration-control projection, while canonical emission
// remains sealed to the completed importer authority in this package.
type CampaignTerminalProjection struct {
	integrationcontrol.CampaignTerminalProjection
	constructionAuthorized bool
}

// ProjectCampaignTerminalProjection only accepts the opaque authority issued
// by ImportPreparedVerified. Raw receipt bytes and manually assembled package
// projections are not construction inputs.
func ProjectCampaignTerminalProjection(
	importAuthority VerifiedCampaignImportAuthority,
	lifecycle integrationcontrol.VerifiedComponentLifecycle,
	release integrationcontrol.ReleaseAuthority,
) (CampaignTerminalProjection, error) {
	receipt, verifiedPackage, authorization, err := importAuthority.verifiedInputs()
	if err != nil {
		return CampaignTerminalProjection{}, err
	}
	verification, err := lifecycle.VerificationReceipt()
	if err != nil {
		return CampaignTerminalProjection{}, err
	}
	if err := release.Validate(); err != nil {
		return CampaignTerminalProjection{}, err
	}
	productReleaseSHA256, err := release.ProductRelease.SHA256()
	if err != nil {
		return CampaignTerminalProjection{}, err
	}
	verifierReleaseSHA256, err := release.Verifier.SHA256()
	if err != nil {
		return CampaignTerminalProjection{}, err
	}
	productReference := integrationcontrol.ArtifactReference{
		SchemaVersion: integrationcontrol.ProductReleaseIdentityVersion,
		SHA256:        productReleaseSHA256,
	}
	verifierReference := integrationcontrol.ContractReference{
		ID:     integrationcontrol.VerifierReleaseVersion,
		SHA256: verifierReleaseSHA256,
	}
	acknowledgement, err := integrationcontrol.NewComponentAcknowledgement(lifecycle)
	if err != nil {
		return CampaignTerminalProjection{}, err
	}
	acknowledgementPayload, err := acknowledgement.CanonicalBytes()
	if err != nil {
		return CampaignTerminalProjection{}, err
	}
	if verification.ProductRelease != productReference ||
		verification.VerifierRelease != verifierReference ||
		verification.CampaignManifestSHA256 != authorization.CampaignManifestSHA256 ||
		authorization.AcknowledgementSHA256[integrationcontrol.ProductXInsight] !=
			campaignTerminalSHA256(acknowledgementPayload) {
		return CampaignTerminalProjection{}, errors.New(
			"campaign terminal projection product lifecycle authority does not converge",
		)
	}

	packageProjection := verifiedPackage.Projection()
	if err := validateCampaignAuthorizationConvergence(authorization, packageProjection); err != nil ||
		receipt.AuthorizationManifestSHA256 != authorization.CampaignManifestSHA256 ||
		receipt.AuthorizationSHA256 != authorization.AuthorizationSHA256 ||
		receipt.AuthorizationSignatureSHA256 != authorization.AuthorizationSignatureSHA256 ||
		receipt.SourceBindingSHA256 != authorization.SourceBinding.SHA256 {
		return CampaignTerminalProjection{}, errors.New(
			"campaign terminal projection receipt, package and N1 authority do not converge",
		)
	}

	packageCreatedAt, err := parseCampaignTerminalPackageTime(packageProjection.PackageCreatedAt)
	if err != nil {
		return CampaignTerminalProjection{}, fmt.Errorf("package created_at: %w", err)
	}
	completedAt, err := parseCampaignTerminalCompletedTime(receipt.OperationalReceipt.ObservedAt)
	if err != nil {
		return CampaignTerminalProjection{}, fmt.Errorf("completed_at: %w", err)
	}
	notBefore, err := time.Parse(time.RFC3339, authorization.AuthorizationWindow.NotBefore)
	if err != nil {
		return CampaignTerminalProjection{}, errors.New("sealed authorization not_before is invalid")
	}
	notAfter, err := time.Parse(time.RFC3339, authorization.AuthorizationWindow.NotAfter)
	if err != nil {
		return CampaignTerminalProjection{}, errors.New("sealed authorization not_after is invalid")
	}
	if completedAt.Before(packageCreatedAt) {
		return CampaignTerminalProjection{}, errors.New(
			"campaign import completed before the verified package was created",
		)
	}
	if completedAt.Before(notBefore) || !completedAt.Before(notAfter) {
		return CampaignTerminalProjection{}, errors.New(
			"campaign import completed outside the sealed half-open authorization window",
		)
	}

	gapCodeSet := make(map[string]struct{})
	for _, member := range receipt.Members {
		for _, code := range member.GapCodes {
			gapCodeSet[code] = struct{}{}
		}
	}
	gapCodes := make([]string, 0, len(gapCodeSet))
	for code := range gapCodeSet {
		gapCodes = append(gapCodes, code)
	}
	sort.Strings(gapCodes)
	receiptSHA256, err := receipt.SHA256()
	if err != nil {
		return CampaignTerminalProjection{}, err
	}
	wire := integrationcontrol.CampaignTerminalProjection{
		SchemaVersion: integrationcontrol.CampaignTerminalProjectionVersion,
		ImportReceipt: integrationcontrol.ArtifactReference{
			SchemaVersion: unirouter.CampaignPackageImportReceiptVersion,
			SHA256:        receiptSHA256,
		},
		ProductRelease:               productReference,
		VerifierRelease:              verifierReference,
		CampaignManifestSHA256:       receipt.AuthorizationManifestSHA256,
		AuthorizationSHA256:          receipt.AuthorizationSHA256,
		AuthorizationSignatureSHA256: receipt.AuthorizationSignatureSHA256,
		SourceBindingSHA256:          receipt.SourceBindingSHA256,
		Package: integrationcontrol.CampaignTerminalPackageObservation{
			SHA256:         receipt.PackageSHA256,
			SizeBytes:      receipt.PackageSizeBytes,
			ManifestSHA256: receipt.ManifestSHA256,
			CreatedAt:      packageProjection.PackageCreatedAt,
		},
		SourceSigner: authorization.SourceEvidenceSigning,
		Status:       receipt.Status,
		Outcomes: integrationcontrol.CampaignTerminalOutcomeCounts{
			Received:      receipt.OperationalReceipt.Counts.Received,
			Inserted:      receipt.OperationalReceipt.Counts.Inserted,
			DuplicateNoop: receipt.OperationalReceipt.Counts.DuplicateNoop,
			Conflict:      receipt.OperationalReceipt.Counts.Conflict,
			Rejected:      receipt.OperationalReceipt.Counts.Rejected,
		},
		Completeness: integrationcontrol.CampaignTerminalCompleteness{
			PackageScopeComplete:            receipt.PackageScopeComplete,
			AllMemberSourceProfilesComplete: receipt.AllMemberSourceProfilesComplete,
			MemberCount:                     receipt.Counts.Members,
			PartialMemberCount:              receipt.Counts.SourceProfilePartialMembers,
			CaptureGapCount:                 receipt.Counts.CaptureGaps,
			GapCodes:                        gapCodes,
		},
		OperationalReceiptSHA256: receipt.OperationalReceiptSHA256,
		CompletedAt:              receipt.OperationalReceipt.ObservedAt,
	}
	if err := wire.Validate(); err != nil {
		return CampaignTerminalProjection{}, err
	}
	return CampaignTerminalProjection{
		CampaignTerminalProjection: wire,
		constructionAuthorized:     true,
	}, nil
}

func VerifyCampaignTerminalProjection(
	payload []byte,
	importAuthority VerifiedCampaignImportAuthority,
	lifecycle integrationcontrol.VerifiedComponentLifecycle,
	release integrationcontrol.ReleaseAuthority,
) (CampaignTerminalProjection, error) {
	parsed, err := integrationcontrol.ParseCampaignTerminalProjection(payload)
	if err != nil {
		return CampaignTerminalProjection{}, err
	}
	expected, err := ProjectCampaignTerminalProjection(importAuthority, lifecycle, release)
	if err != nil {
		return CampaignTerminalProjection{}, err
	}
	expectedPayload, err := expected.CanonicalBytes()
	if err != nil || !bytes.Equal(payload, expectedPayload) {
		return CampaignTerminalProjection{}, errors.New(
			"campaign terminal projection contradicts its sealed product authority",
		)
	}
	return CampaignTerminalProjection{
		CampaignTerminalProjection: parsed,
		constructionAuthorized:     true,
	}, nil
}

func (projection CampaignTerminalProjection) CanonicalBytes() ([]byte, error) {
	if !projection.constructionAuthorized {
		return nil, errors.New("sealed campaign terminal projection authority is required")
	}
	if err := projection.CampaignTerminalProjection.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(projection.CampaignTerminalProjection)
}

func (projection CampaignTerminalProjection) SHA256() (string, error) {
	payload, err := projection.CanonicalBytes()
	if err != nil {
		return "", err
	}
	return campaignTerminalSHA256(payload), nil
}

func parseCampaignTerminalPackageTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC ||
		value != parsed.UTC().Format(time.RFC3339Nano) {
		return time.Time{}, errors.New("campaign terminal projection package timestamp is not canonical UTC")
	}
	return parsed, nil
}

func parseCampaignTerminalCompletedTime(value string) (time.Time, error) {
	const canonicalOperationalReceiptTime = "2006-01-02T15:04:05.000000Z"
	parsed, err := time.Parse(canonicalOperationalReceiptTime, value)
	if err != nil || parsed.Location() != time.UTC ||
		value != parsed.UTC().Format(canonicalOperationalReceiptTime) {
		return time.Time{}, errors.New(
			"campaign terminal projection completion timestamp is not canonical UTC microsecond time",
		)
	}
	return parsed, nil
}

func campaignTerminalSHA256(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}
