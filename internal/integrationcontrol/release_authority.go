package integrationcontrol

import (
	"errors"
	"regexp"
	"strings"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/releasebundle"
)

const (
	ProductReleaseIdentityVersion    = "xinsight.release-identity.v1"
	M0EvidenceSchemaSHA256           = "sha256:83adb76550457e68c565b5aecd3db141a9192c2e77531359e7fd39290f4fe2b3"
	CampaignImportReceiptVersion     = "xinsight.unirouter-campaign-package-import-receipt.v1"
	HistoricalCampaignDatabaseSchema = "23"
)

var releaseSigningKeyIDPattern = regexp.MustCompile(`^ed25519:sha256:[a-f0-9]{64}$`)

// ProductReleaseIdentity is a deterministic receipt derived only after the
// existing X-Insight release manifest has passed signature and payload
// verification. It is deliberately not embedded back into that payload, so
// the signed manifest remains the single cryptographic release authority.
type ProductReleaseIdentity struct {
	SchemaVersion                  string `json:"schema_version"`
	Product                        string `json:"product"`
	ProductVersion                 string `json:"product_version"`
	Commit                         string `json:"commit"`
	SignedReleaseManifestSHA256    string `json:"signed_release_manifest_sha256"`
	ReleaseSigningKeyID            string `json:"release_signing_key_id"`
	TargetOS                       string `json:"target_os"`
	TargetArch                     string `json:"target_arch"`
	DatabaseSchema                 string `json:"database_schema"`
	ManualVersion                  string `json:"manual_version"`
	ImporterExecutableSHA256       string `json:"importer_executable_sha256"`
	ImporterExecutableSizeBytes    int64  `json:"importer_executable_size_bytes"`
	M0EvidenceSchemaSHA256         string `json:"m0_evidence_schema_sha256"`
	ComponentContractSHA256        string `json:"component_contract_sha256"`
	NeutralN1ReleaseManifestSHA256 string `json:"neutral_n1_release_manifest_sha256"`
}

type ReleaseAuthority struct {
	ProductRelease ProductReleaseIdentity
	Verifier       VerifierRelease
}

func NewReleaseAuthorityFromVerifiedBundle(bundle releasebundle.VerifiedBundle) (ReleaseAuthority, error) {
	manifest, manifestSHA256, err := bundle.VerifiedManifest()
	if err != nil {
		return ReleaseAuthority{}, err
	}
	importer, err := exactSignedReleaseFile(manifest.Files, "bin/xinsight-importer", "0755")
	if err != nil {
		return ReleaseAuthority{}, err
	}
	evidenceSchema, err := exactSignedReleaseFile(manifest.Files, "schemas/m0/evidence-event.schema.json", "0644")
	if err != nil {
		return ReleaseAuthority{}, err
	}
	if evidenceSchema.SHA256 != M0EvidenceSchemaSHA256 ||
		!campaignDatabaseSchema(manifest.DatabaseSchema.Current, manifest.DatabaseSchema.Min, manifest.DatabaseSchema.Max) ||
		(manifest.Manual.Language != "zh-CN" && manifest.Manual.Language != "en-US") ||
		!strings.HasPrefix(manifest.Manual.Version, manifest.ProductVersion+"/manual-r") ||
		!containsReleaseIdentity(manifest.ReceiptSchemaVersions, CampaignImportReceiptVersion) {
		return ReleaseAuthority{}, errors.New("signed release manifest does not authorize the campaign-package consumer")
	}
	contractSHA256, err := NewComponentContract().SHA256()
	if err != nil {
		return ReleaseAuthority{}, err
	}
	product := ProductReleaseIdentity{
		SchemaVersion:                  ProductReleaseIdentityVersion,
		Product:                        ProductXInsight,
		ProductVersion:                 manifest.ProductVersion,
		Commit:                         manifest.Commit,
		SignedReleaseManifestSHA256:    manifestSHA256,
		ReleaseSigningKeyID:            manifest.Signature.KeyID,
		TargetOS:                       manifest.TargetOS,
		TargetArch:                     manifest.TargetArch,
		DatabaseSchema:                 manifest.DatabaseSchema.Current,
		ManualVersion:                  manifest.Manual.Version,
		ImporterExecutableSHA256:       importer.SHA256,
		ImporterExecutableSizeBytes:    importer.SizeBytes,
		M0EvidenceSchemaSHA256:         evidenceSchema.SHA256,
		ComponentContractSHA256:        contractSHA256,
		NeutralN1ReleaseManifestSHA256: FrozenN1ReleaseManifestSHA256,
	}
	if err := product.Validate(); err != nil {
		return ReleaseAuthority{}, err
	}
	productSHA256, err := product.SHA256()
	if err != nil {
		return ReleaseAuthority{}, err
	}
	verifier, err := NewVerifierRelease(VerifierReleaseInput{
		Commit:                      manifest.Commit,
		Version:                     manifest.ProductVersion,
		ComponentContractSHA256:     contractSHA256,
		ProductReleaseSHA256:        productSHA256,
		SignedReleaseManifestSHA256: manifestSHA256,
		ImporterExecutableSHA256:    importer.SHA256,
		M0EvidenceSchemaSHA256:      evidenceSchema.SHA256,
	})
	if err != nil {
		return ReleaseAuthority{}, err
	}
	return ReleaseAuthority{ProductRelease: product, Verifier: verifier}, nil
}

// campaignDatabaseSchema preserves the historical schema23 factory while adding
// only the exact generation compiled into this binary. A signed input cannot
// expand the current consumer's schema range or choose its generation.
func campaignDatabaseSchema(current, minimum, maximum string) bool {
	if current != minimum || current != maximum {
		return false
	}
	return current == HistoricalCampaignDatabaseSchema ||
		(buildinfo.DatabaseSchemaMin != "" && buildinfo.DatabaseSchemaMin == buildinfo.DatabaseSchemaMax && current == buildinfo.DatabaseSchemaMin)
}

// Validate accepts historical receipt structure. Executable import authority still
// requires a supported exact generation in NewReleaseAuthorityFromVerifiedBundle.
func (value ProductReleaseIdentity) Validate() error {
	if value.SchemaVersion != ProductReleaseIdentityVersion ||
		value.Product != ProductXInsight ||
		!validPortableText(value.ProductVersion, 128) ||
		!gitObjectPattern.MatchString(value.Commit) ||
		!digestPattern.MatchString(value.SignedReleaseManifestSHA256) ||
		!releaseSigningKeyIDPattern.MatchString(value.ReleaseSigningKeyID) ||
		value.TargetOS != "linux" || value.TargetArch != "amd64" ||
		(value.DatabaseSchema != "20" && !campaignDatabaseSchema(value.DatabaseSchema, value.DatabaseSchema, value.DatabaseSchema)) ||
		!strings.HasPrefix(value.ManualVersion, value.ProductVersion+"/manual-r") ||
		!digestPattern.MatchString(value.ImporterExecutableSHA256) ||
		value.ImporterExecutableSizeBytes < 1 || value.ImporterExecutableSizeBytes > releasebundle.MaxPayloadFileBytes ||
		value.M0EvidenceSchemaSHA256 != M0EvidenceSchemaSHA256 ||
		!digestPattern.MatchString(value.ComponentContractSHA256) ||
		value.NeutralN1ReleaseManifestSHA256 != FrozenN1ReleaseManifestSHA256 {
		return errors.New("X-Insight product release identity is invalid")
	}
	return nil
}

func (value ProductReleaseIdentity) CanonicalBytes() ([]byte, error) {
	return canonicalProductArtifact(value, value.Validate)
}

func (value ProductReleaseIdentity) SHA256() (string, error) {
	return artifactSHA256(value.CanonicalBytes())
}

func (value ReleaseAuthority) Validate() error {
	if err := value.ProductRelease.Validate(); err != nil {
		return err
	}
	if err := value.Verifier.Validate(); err != nil {
		return err
	}
	productSHA256, err := value.ProductRelease.SHA256()
	if err != nil {
		return err
	}
	if value.Verifier.ProductReleaseSHA256 != productSHA256 ||
		value.Verifier.Commit != value.ProductRelease.Commit ||
		value.Verifier.Version != value.ProductRelease.ProductVersion ||
		value.Verifier.SignedReleaseManifestSHA256 != value.ProductRelease.SignedReleaseManifestSHA256 ||
		value.Verifier.ComponentContractSHA256 != value.ProductRelease.ComponentContractSHA256 ||
		value.Verifier.ImporterExecutableSHA256 != value.ProductRelease.ImporterExecutableSHA256 ||
		value.Verifier.M0EvidenceSchemaSHA256 != value.ProductRelease.M0EvidenceSchemaSHA256 ||
		value.Verifier.NeutralN1ReleaseManifestSHA256 != value.ProductRelease.NeutralN1ReleaseManifestSHA256 {
		return errors.New("X-Insight product and verifier release authorities do not converge")
	}
	return nil
}

func exactSignedReleaseFile(entries []releasebundle.FileEntry, expectedPath, expectedMode string) (releasebundle.FileEntry, error) {
	var result releasebundle.FileEntry
	for _, entry := range entries {
		if entry.Path != expectedPath {
			continue
		}
		if result.Path != "" || entry.Mode != expectedMode || entry.SizeBytes < 1 || !digestPattern.MatchString(entry.SHA256) {
			return releasebundle.FileEntry{}, errors.New("signed release manifest file authority is invalid")
		}
		result = entry
	}
	if result.Path == "" {
		return releasebundle.FileEntry{}, errors.New("signed release manifest is missing a campaign-package authority file")
	}
	return result, nil
}

func containsReleaseIdentity(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func ParseProductReleaseIdentity(payload []byte) (ProductReleaseIdentity, error) {
	return parseCanonicalProductArtifact(payload, "product release identity", func(value ProductReleaseIdentity) error {
		return value.Validate()
	})
}
