package releasebundle

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/db"
)

const (
	ConfigSchemaVersion     = "xinsight.release-config.v1"
	ManifestSchemaVersion   = "xinsight.release-manifest.v2"
	SignatureSchemaVersion  = "xinsight.release-signature.v1"
	PrivateKeySchemaVersion = "xinsight.release-private-key.v1"
	TrustStoreSchemaVersion = "xinsight.trusted-release-keys.v1"
	SignatureAlgorithm      = "ed25519"
	PayloadArtifactPath     = "payload.tar.gz"
	DefaultTrustReference   = "/etc/x-insight/trusted-release-keys.json"
	MaxBundleBytes          = 256 << 20
	MaxPayloadFiles         = 2000
	MaxPayloadFileBytes     = 128 << 20
)

var (
	ErrInvalidBundle       = errors.New("invalid X-Insight release bundle")
	ErrUntrustedRelease    = errors.New("X-Insight release signature is not trusted")
	ErrUnsupportedPlatform = errors.New("X-Insight release target is not supported")
	semverPattern          = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	commitPattern          = regexp.MustCompile(`^[a-f0-9]{40}$`)
	keyIDPattern           = regexp.MustCompile(`^ed25519:sha256:[a-f0-9]{64}$`)
	manualVersionPattern   = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?/manual-r[1-9][0-9]*$`)
	sha256ReferencePattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type ReleaseConfig struct {
	SchemaVersion                string                   `json:"schema_version"`
	ProductName                  string                   `json:"product_name"`
	ProductVersion               string                   `json:"product_version"`
	ReleaseChannel               string                   `json:"release_channel"`
	TrustReference               string                   `json:"trust_reference"`
	SupportedUpgradeFromVersions []string                 `json:"supported_upgrade_from_versions"`
	SupportedLegacySources       []LegacySource           `json:"supported_legacy_sources"`
	ApplicationCompatibility     ApplicationCompatibility `json:"application_compatibility"`
	DatabaseSchema               SchemaCompatibility      `json:"database_schema"`
	ConfigSchema                 SchemaCompatibility      `json:"config_schema"`
	APIVersions                  []string                 `json:"api_versions"`
	EvidenceSchemaVersions       []string                 `json:"evidence_schema_versions"`
	ReceiptSchemaVersions        []string                 `json:"receipt_schema_versions"`
	Manual                       ManualIdentity           `json:"manual"`
	Rollback                     RollbackPolicy           `json:"rollback"`
	SchemaContract               SchemaContractIdentity   `json:"schema_contract"`
}

type LegacySource struct {
	ReleaseID                    string                 `json:"release_id"`
	Commit                       string                 `json:"commit"`
	ArtifactSHA256               string                 `json:"artifact_sha256"`
	ManifestSHA256               string                 `json:"manifest_sha256"`
	ReleaseLayout                string                 `json:"release_layout"`
	DatabaseSchema               string                 `json:"database_schema"`
	ConfigSchema                 string                 `json:"config_schema"`
	ServiceLayout                string                 `json:"service_layout"`
	ApplicationDatabaseSchemaMin string                 `json:"application_database_schema_min"`
	ApplicationDatabaseSchemaMax string                 `json:"application_database_schema_max"`
	CompatibleConfigSchemas      []string               `json:"compatible_config_schemas"`
	MigrationChecksums           []db.MigrationChecksum `json:"migration_checksums"`
}

type ApplicationCompatibility struct {
	DatabaseSchemaMin string `json:"database_schema_min"`
	DatabaseSchemaMax string `json:"database_schema_max"`
	ConfigSchemaMin   string `json:"config_schema_min"`
	ConfigSchemaMax   string `json:"config_schema_max"`
}

type SchemaCompatibility struct {
	Current string `json:"current"`
	Min     string `json:"min"`
	Max     string `json:"max"`
}

type ManualIdentity struct {
	Language string `json:"language"`
	Version  string `json:"version"`
}

type RollbackPolicy struct {
	ApplicationRollbackCompatible bool     `json:"application_rollback_compatible"`
	Barriers                      []string `json:"barriers"`
}

type SchemaContractIdentity struct {
	SchemaVersion      string `json:"schema_version"`
	SemanticSHA256     string `json:"semantic_sha256"`
	MigrationSetSHA256 string `json:"migration_set_sha256"`
}

type BuildToolchain struct {
	GoVersion   string `json:"go_version"`
	NodeVersion string `json:"node_version"`
	NPMVersion  string `json:"npm_version"`
}

type FileEntry struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	Mode      string `json:"mode"`
}

type ManifestSignatureIdentity struct {
	Algorithm      string `json:"algorithm"`
	KeyID          string `json:"key_id"`
	TrustReference string `json:"trust_reference"`
}

type ReleaseManifest struct {
	SchemaVersion                string                    `json:"schema_version"`
	ProductName                  string                    `json:"product_name"`
	ProductVersion               string                    `json:"product_version"`
	ReleaseChannel               string                    `json:"release_channel"`
	Commit                       string                    `json:"commit"`
	BuildToolchain               BuildToolchain            `json:"build_toolchain"`
	TargetOS                     string                    `json:"target_os"`
	TargetArch                   string                    `json:"target_arch"`
	CGOEnabled                   bool                      `json:"cgo_enabled"`
	ArtifactPath                 string                    `json:"artifact_path"`
	ArtifactDigest               string                    `json:"artifact_digest"`
	ArtifactSizeBytes            int64                     `json:"artifact_size_bytes"`
	Signature                    ManifestSignatureIdentity `json:"signature"`
	CreatedAt                    time.Time                 `json:"created_at"`
	SupportedUpgradeFromVersions []string                  `json:"supported_upgrade_from_versions"`
	SupportedLegacySources       []LegacySource            `json:"supported_legacy_sources"`
	ApplicationCompatibility     ApplicationCompatibility  `json:"application_compatibility"`
	DatabaseSchema               SchemaCompatibility       `json:"database_schema"`
	MigrationChecksums           []db.MigrationChecksum    `json:"migration_checksums"`
	ConfigSchema                 SchemaCompatibility       `json:"config_schema"`
	APIVersions                  []string                  `json:"api_versions"`
	EvidenceSchemaVersions       []string                  `json:"evidence_schema_versions"`
	ReceiptSchemaVersions        []string                  `json:"receipt_schema_versions"`
	Manual                       ManualIdentity            `json:"manual"`
	Rollback                     RollbackPolicy            `json:"rollback"`
	SchemaContract               SchemaContractIdentity    `json:"schema_contract"`
	Files                        []FileEntry               `json:"files"`
}

type DetachedSignature struct {
	SchemaVersion  string `json:"schema_version"`
	Algorithm      string `json:"algorithm"`
	KeyID          string `json:"key_id"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Signature      string `json:"signature_base64"`
}

type PrivateKeyFile struct {
	SchemaVersion string    `json:"schema_version"`
	Algorithm     string    `json:"algorithm"`
	KeyID         string    `json:"key_id"`
	CreatedAt     time.Time `json:"created_at"`
	PrivateKey    string    `json:"private_key_base64"`
}

type TrustStore struct {
	SchemaVersion string       `json:"schema_version"`
	Keys          []TrustedKey `json:"keys"`
}

type TrustedKey struct {
	KeyID       string     `json:"key_id"`
	Algorithm   string     `json:"algorithm"`
	PublicKey   string     `json:"public_key_base64"`
	Status      string     `json:"status"`
	NotBefore   time.Time  `json:"not_before"`
	NotAfter    *time.Time `json:"not_after,omitempty"`
	Description string     `json:"description"`
}

func (config ReleaseConfig) Validate() error {
	if config.SchemaVersion != ConfigSchemaVersion || config.ProductName != "x-insight" {
		return fmt.Errorf("%w: release config identity", ErrInvalidBundle)
	}
	if !semverPattern.MatchString(config.ProductVersion) {
		return fmt.Errorf("%w: product_version", ErrInvalidBundle)
	}
	switch config.ReleaseChannel {
	case "release_candidate", "stable":
	default:
		return fmt.Errorf("%w: release_channel", ErrInvalidBundle)
	}
	if config.TrustReference != DefaultTrustReference {
		return fmt.Errorf("%w: trust_reference", ErrInvalidBundle)
	}
	for _, version := range config.SupportedUpgradeFromVersions {
		if !semverPattern.MatchString(version) {
			return fmt.Errorf("%w: supported upgrade version", ErrInvalidBundle)
		}
	}
	if err := validateSchemaCompatibility(config.DatabaseSchema, true); err != nil {
		return err
	}
	if err := validateSchemaCompatibility(config.ConfigSchema, false); err != nil {
		return err
	}
	if config.ApplicationCompatibility.DatabaseSchemaMin == "" || config.ApplicationCompatibility.DatabaseSchemaMax == "" ||
		config.ApplicationCompatibility.ConfigSchemaMin == "" || config.ApplicationCompatibility.ConfigSchemaMax == "" {
		return fmt.Errorf("%w: application compatibility", ErrInvalidBundle)
	}
	if (config.Manual.Language != "zh-CN" && config.Manual.Language != "en-US") || !manualVersionPattern.MatchString(config.Manual.Version) ||
		!strings.HasPrefix(config.Manual.Version, config.ProductVersion+"/") {
		return fmt.Errorf("%w: manual identity", ErrInvalidBundle)
	}
	if len(config.APIVersions) == 0 || len(config.EvidenceSchemaVersions) == 0 || len(config.ReceiptSchemaVersions) == 0 {
		return fmt.Errorf("%w: compatibility version lists", ErrInvalidBundle)
	}
	if len(config.Rollback.Barriers) == 0 {
		return fmt.Errorf("%w: rollback barriers", ErrInvalidBundle)
	}
	if requiresGeneratedSchemaContract(config.ProductVersion) &&
		(config.SchemaContract.SchemaVersion != "xinsight.schema-contract.v2" ||
			!validSHA256(config.SchemaContract.SemanticSHA256) ||
			!validSHA256(config.SchemaContract.MigrationSetSHA256)) {
		return fmt.Errorf("%w: schema contract identity", ErrInvalidBundle)
	}
	seenLegacy := make(map[string]struct{}, len(config.SupportedLegacySources))
	for _, legacy := range config.SupportedLegacySources {
		if legacy.ReleaseID == "" || !commitPattern.MatchString(legacy.Commit) || !validSHA256(legacy.ArtifactSHA256) ||
			!validSHA256(legacy.ManifestSHA256) || legacy.ReleaseLayout == "" || legacy.DatabaseSchema == "" ||
			legacy.ConfigSchema == "" || legacy.ServiceLayout == "" {
			return fmt.Errorf("%w: supported legacy source", ErrInvalidBundle)
		}
		if _, exists := seenLegacy[legacy.ReleaseID]; exists {
			return fmt.Errorf("%w: duplicate legacy source", ErrInvalidBundle)
		}
		seenLegacy[legacy.ReleaseID] = struct{}{}
		if err := validateMigrationChecksums(legacy.MigrationChecksums); err != nil {
			return err
		}
		legacySchema, schemaErr := strconv.Atoi(legacy.DatabaseSchema)
		minimum, minErr := strconv.Atoi(legacy.ApplicationDatabaseSchemaMin)
		maximum, maxErr := strconv.Atoi(legacy.ApplicationDatabaseSchemaMax)
		if schemaErr != nil || minErr != nil || maxErr != nil || minimum < 1 || minimum > legacySchema || legacySchema > maximum ||
			len(legacy.CompatibleConfigSchemas) == 0 || len(legacy.CompatibleConfigSchemas) > 16 {
			return fmt.Errorf("%w: legacy application compatibility", ErrInvalidBundle)
		}
		seenConfigs := make(map[string]struct{}, len(legacy.CompatibleConfigSchemas))
		for _, schema := range legacy.CompatibleConfigSchemas {
			if schema == "" || len(schema) > 128 {
				return fmt.Errorf("%w: legacy compatible config schema", ErrInvalidBundle)
			}
			if _, exists := seenConfigs[schema]; exists {
				return fmt.Errorf("%w: duplicate legacy compatible config schema", ErrInvalidBundle)
			}
			seenConfigs[schema] = struct{}{}
		}
		if config.Rollback.ApplicationRollbackCompatible {
			targetSchema, err := strconv.Atoi(config.DatabaseSchema.Current)
			_, configCompatible := seenConfigs[config.ConfigSchema.Current]
			if err != nil || targetSchema < minimum || targetSchema > maximum || !configCompatible {
				return fmt.Errorf("%w: declared legacy rollback is not compatible with the target runtime", ErrInvalidBundle)
			}
		}
	}
	return nil
}

func validateSchemaCompatibility(value SchemaCompatibility, numeric bool) error {
	if value.Current == "" || value.Min == "" || value.Max == "" {
		return fmt.Errorf("%w: schema compatibility", ErrInvalidBundle)
	}
	if !numeric {
		if value.Min != value.Current || value.Max != value.Current {
			return fmt.Errorf("%w: config schema range", ErrInvalidBundle)
		}
		return nil
	}
	current, currentErr := strconv.Atoi(value.Current)
	minimum, minErr := strconv.Atoi(value.Min)
	maximum, maxErr := strconv.Atoi(value.Max)
	if currentErr != nil || minErr != nil || maxErr != nil || minimum < 1 || minimum > current || current > maximum {
		return fmt.Errorf("%w: database schema range", ErrInvalidBundle)
	}
	return nil
}

func validateMigrationChecksums(values []db.MigrationChecksum) error {
	if len(values) == 0 {
		return fmt.Errorf("%w: empty migration checksum set", ErrInvalidBundle)
	}
	previous := ""
	for _, value := range values {
		if value.ID == "" || !validSHA256(value.SHA256) || (previous != "" && strings.Compare(previous, value.ID) >= 0) {
			return fmt.Errorf("%w: migration checksum set", ErrInvalidBundle)
		}
		previous = value.ID
	}
	return nil
}

func validSHA256(value string) bool {
	return sha256ReferencePattern.MatchString(value)
}
