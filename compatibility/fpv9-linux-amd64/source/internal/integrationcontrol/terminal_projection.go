package integrationcontrol

import (
	"errors"
	"regexp"
	"time"

	unirouteradapter "github.com/foigent/x-insight/internal/adapters/unirouter"
)

const (
	CampaignTerminalProjectionVersion = "xinsight.unirouter-campaign-terminal-projection.v1"
	maxTerminalProjectionJSONInteger  = int64(1<<53 - 1)
)

var terminalProjectionGapCodePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,119}$`)

type CampaignTerminalPackageObservation struct {
	SHA256         string `json:"sha256"`
	SizeBytes      int64  `json:"size_bytes"`
	ManifestSHA256 string `json:"manifest_sha256"`
	CreatedAt      string `json:"created_at"`
}

type CampaignTerminalOutcomeCounts struct {
	Received      int64 `json:"received"`
	Inserted      int64 `json:"inserted"`
	DuplicateNoop int64 `json:"duplicate_noop"`
	Conflict      int64 `json:"conflict"`
	Rejected      int64 `json:"rejected"`
}

type CampaignTerminalCompleteness struct {
	PackageScopeComplete            bool     `json:"package_scope_complete"`
	AllMemberSourceProfilesComplete bool     `json:"all_member_source_profiles_complete"`
	MemberCount                     int64    `json:"member_count"`
	PartialMemberCount              int64    `json:"partial_member_count"`
	CaptureGapCount                 int64    `json:"capture_gap_count"`
	GapCodes                        []string `json:"gap_codes"`
}

// CampaignTerminalProjection is the narrow X-Insight-owned compatibility
// surface consumed by Neutral N2. The full import receipt remains the product
// authority; this projection contains no member inventory, M0 payload,
// provenance, database identity, checkpoint or Target evidence.
type CampaignTerminalProjection struct {
	SchemaVersion                string                             `json:"schema_version"`
	ImportReceipt                ArtifactReference                  `json:"import_receipt"`
	ProductRelease               ArtifactReference                  `json:"product_release"`
	VerifierRelease              ContractReference                  `json:"verifier_release"`
	CampaignManifestSHA256       string                             `json:"campaign_manifest_sha256"`
	AuthorizationSHA256          string                             `json:"authorization_sha256"`
	AuthorizationSignatureSHA256 string                             `json:"authorization_signature_sha256"`
	SourceBindingSHA256          string                             `json:"source_binding_sha256"`
	Package                      CampaignTerminalPackageObservation `json:"package"`
	SourceSigner                 N1SourceEvidenceSigningIdentity    `json:"source_signer"`
	Status                       string                             `json:"status"`
	Outcomes                     CampaignTerminalOutcomeCounts      `json:"outcomes"`
	Completeness                 CampaignTerminalCompleteness       `json:"completeness"`
	OperationalReceiptSHA256     string                             `json:"operational_receipt_sha256"`
	CompletedAt                  string                             `json:"completed_at"`
}

func (projection CampaignTerminalProjection) Validate() error {
	if projection.SchemaVersion != CampaignTerminalProjectionVersion ||
		projection.ImportReceipt.SchemaVersion != unirouteradapter.CampaignPackageImportReceiptVersion ||
		projection.ProductRelease.SchemaVersion != ProductReleaseIdentityVersion ||
		projection.VerifierRelease.ID != VerifierReleaseVersion ||
		projection.Status != "completed" {
		return errors.New("campaign terminal projection identity or status is invalid")
	}
	for _, reference := range []ArtifactReference{projection.ImportReceipt, projection.ProductRelease} {
		if err := reference.validate(); err != nil {
			return err
		}
	}
	if err := projection.VerifierRelease.validate(); err != nil {
		return err
	}
	for _, digest := range []string{
		projection.CampaignManifestSHA256,
		projection.AuthorizationSHA256,
		projection.AuthorizationSignatureSHA256,
		projection.SourceBindingSHA256,
		projection.Package.SHA256,
		projection.Package.ManifestSHA256,
		projection.OperationalReceiptSHA256,
	} {
		if !digestPattern.MatchString(digest) {
			return errors.New("campaign terminal projection contains an invalid digest")
		}
	}
	if projection.Package.SizeBytes < 1 ||
		projection.Package.SizeBytes > unirouteradapter.DefaultCampaignPackageMaxBytes {
		return errors.New("campaign terminal projection package size is invalid")
	}
	if err := projection.SourceSigner.validate(); err != nil {
		return err
	}
	if err := validateTerminalProjectionOutcomes(projection.Outcomes); err != nil {
		return err
	}
	if err := validateTerminalProjectionCompleteness(projection.Completeness); err != nil {
		return err
	}
	packageCreatedAt, err := parseTerminalPackageTime(projection.Package.CreatedAt)
	if err != nil {
		return err
	}
	completedAt, err := parseTerminalCompletedTime(projection.CompletedAt)
	if err != nil {
		return err
	}
	if completedAt.Before(packageCreatedAt) {
		return errors.New("campaign terminal projection completion predates package creation")
	}
	return nil
}

func ParseCampaignTerminalProjection(payload []byte) (CampaignTerminalProjection, error) {
	return parseCanonicalProductArtifact(payload, "campaign terminal projection", func(value CampaignTerminalProjection) error {
		return value.Validate()
	})
}

func validateTerminalProjectionOutcomes(counts CampaignTerminalOutcomeCounts) error {
	for _, value := range []int64{
		counts.Received,
		counts.Inserted,
		counts.DuplicateNoop,
		counts.Conflict,
		counts.Rejected,
	} {
		if value < 0 || value > maxTerminalProjectionJSONInteger {
			return errors.New("campaign terminal projection outcome count is outside its bound")
		}
	}
	if counts.Conflict != 0 || counts.Rejected != 0 ||
		counts.Inserted+counts.DuplicateNoop != counts.Received {
		return errors.New("campaign terminal projection outcomes did not converge")
	}
	return nil
}

func validateTerminalProjectionCompleteness(value CampaignTerminalCompleteness) error {
	if !value.PackageScopeComplete || value.MemberCount < 1 ||
		value.MemberCount > int64(unirouteradapter.DefaultCampaignPackageMaxMembers) ||
		value.PartialMemberCount < 0 || value.PartialMemberCount > value.MemberCount ||
		value.CaptureGapCount < 0 ||
		value.CaptureGapCount > value.MemberCount*int64(unirouteradapter.MaxExecutionEvidenceGaps) ||
		value.AllMemberSourceProfilesComplete != (value.PartialMemberCount == 0) ||
		(value.PartialMemberCount == 0 && (value.CaptureGapCount != 0 || len(value.GapCodes) != 0)) ||
		(value.PartialMemberCount > 0 && (value.CaptureGapCount < value.PartialMemberCount || len(value.GapCodes) == 0)) {
		return errors.New("campaign terminal projection completeness is contradictory")
	}
	previous := ""
	for _, code := range value.GapCodes {
		if !terminalProjectionGapCodePattern.MatchString(code) ||
			(previous != "" && previous >= code) {
			return errors.New("campaign terminal projection gap-code aggregate is not canonical")
		}
		previous = code
	}
	return nil
}

func parseTerminalPackageTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC ||
		value != parsed.UTC().Format(time.RFC3339Nano) {
		return time.Time{}, errors.New("campaign terminal projection package timestamp is not canonical UTC")
	}
	return parsed, nil
}

func parseTerminalCompletedTime(value string) (time.Time, error) {
	const canonicalOperationalReceiptTime = "2006-01-02T15:04:05.000000Z"
	parsed, err := time.Parse(canonicalOperationalReceiptTime, value)
	if err != nil || parsed.Location() != time.UTC ||
		value != parsed.UTC().Format(canonicalOperationalReceiptTime) {
		return time.Time{}, errors.New("campaign terminal projection completion timestamp is not canonical UTC microsecond time")
	}
	return parsed, nil
}
