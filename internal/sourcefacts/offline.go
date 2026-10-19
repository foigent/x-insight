package sourcefacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/connector"
)

const OfflineAuthorizationVersion = "xinsight.source-facts-offline-authorization.v1"
const ObservationOfflineAuthorizationVersion = "xinsight.source-facts-offline-authorization.v2"
const ObservationOfflineDatabaseSchema = buildinfo.DatabaseSchemaMax
const ProtocolCommit = "ad8fe7a38c9d7335657630baa8670dd07556b98c"
const ProtocolReleaseDigest = "sha256:99f8a541b6d33b0a74d2e4c06aa312c6d6c88c35a4bfb1d4d34b98b4e4e2e8e8"

var ErrAuthorization = errors.New("source facts authorization is invalid or inactive")
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)
var hashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// OfflineAuthorization is independent of network credentials and live streams.
// Its content digest is registered by the administrator before import is enabled.
type OfflineAuthorization struct {
	SchemaVersion         string `json:"schema_version"`
	AuthorizationID       string `json:"authorization_id"`
	Environment           string `json:"environment"`
	TargetRef             string `json:"target_ref"`
	MachineIDSHA256       string `json:"machine_id_sha256"`
	ProductVersion        string `json:"product_version"`
	ProductCommit         string `json:"product_commit"`
	ProtocolVersion       string `json:"protocol_version"`
	ProtocolCommit        string `json:"protocol_commit"`
	ProtocolReleaseDigest string `json:"protocol_release_digest"`
	SourceSchemaSHA256    string `json:"source_schema_sha256"`
	SourceSystemID        string `json:"source_system_id"`
	SourceInstance        string `json:"source_instance"`
	OrgID                 string `json:"org_id"`
	ProjectID             string `json:"project_id"`
	Profile               string `json:"profile"`
	NotBefore             string `json:"not_before"`
	ExpiresAt             string `json:"expires_at"`
	BundlePath            string `json:"bundle_path"`
	BundleSHA256          string `json:"bundle_sha256"`
	BundleID              string `json:"bundle_id"`
	OperationID           string `json:"operation_id"`
	MaxRecords            int    `json:"max_records"`
	PayloadCaptureMode    string `json:"payload_capture_mode"`
	RawPayloadPresent     bool   `json:"raw_payload_present"`
	// Omitted for A v1 so its existing serialized identity and digest stay stable.
	TargetDatabaseSchema   string `json:"target_database_schema,omitempty"`
	TargetDatabaseName     string `json:"target_database_name,omitempty"`
	TargetDatabaseOID      string `json:"target_database_oid,omitempty"`
	TargetDatabaseSystemID string `json:"target_database_system_id,omitempty"`
	TargetSourceSystemID   string `json:"target_source_system_id,omitempty"`
	TargetSourceInstance   string `json:"target_source_instance,omitempty"`
	TargetOrgID            string `json:"target_org_id,omitempty"`
	TargetProjectID        string `json:"target_project_id,omitempty"`
}

func (a OfflineAuthorization) Validate(now time.Time) error {
	if a.SchemaVersion == ResourceOfflineAuthorizationVersion {
		return a.validateResource(now)
	}
	if a.TargetDatabaseName != "" || a.TargetDatabaseOID != "" || a.TargetDatabaseSystemID != "" || a.TargetSourceSystemID != "" || a.TargetSourceInstance != "" || a.TargetOrgID != "" || a.TargetProjectID != "" {
		return ErrAuthorization
	}
	formatValid := a.SchemaVersion == OfflineAuthorizationVersion && a.TargetDatabaseSchema == "" && ValidProfile(a.Profile) && a.SourceSchemaSHA256 == Hash(schemaBytes)
	if a.SchemaVersion == ObservationOfflineAuthorizationVersion {
		formatValid = (a.TargetDatabaseSchema == ObservationOfflineDatabaseSchema || a.TargetDatabaseSchema == "35" || a.TargetDatabaseSchema == "36" || a.TargetDatabaseSchema == "37" || a.TargetDatabaseSchema == "38" || a.TargetDatabaseSchema == "39" || a.TargetDatabaseSchema == "40" || a.TargetDatabaseSchema == "41" || a.TargetDatabaseSchema == "42" || a.TargetDatabaseSchema == "43" || a.TargetDatabaseSchema == "44" || a.TargetDatabaseSchema == "45") && ValidObservationProfile(a.Profile) && a.SourceSchemaSHA256 == Hash(observationSchemaBytes)
	}
	if !formatValid || !namePattern.MatchString(a.AuthorizationID) || !namePattern.MatchString(a.TargetRef) ||
		(a.Environment != "development" && a.Environment != "test" && a.Environment != "integration") ||
		!hashPattern.MatchString(a.MachineIDSHA256) || !boundedText(a.ProductVersion, 128) || !boundedText(a.ProductCommit, 128) ||
		a.ProtocolVersion != "2.0.2" || a.ProtocolCommit != ProtocolCommit || a.ProtocolReleaseDigest != ProtocolReleaseDigest ||
		!uuidPattern.MatchString(a.SourceSystemID) || !boundedText(a.SourceInstance, 128) || !Positive(a.OrgID) || !Positive(a.ProjectID) ||
		!namePattern.MatchString(a.OperationID) || !namePattern.MatchString(a.BundleID) || !hashPattern.MatchString(a.BundleSHA256) ||
		!filepath.IsAbs(a.BundlePath) || filepath.Clean(a.BundlePath) != a.BundlePath || a.MaxRecords < 1 || a.MaxRecords > 50 ||
		a.PayloadCaptureMode != "metadata_only" || a.RawPayloadPresent || !ValidTime(a.NotBefore) || !ValidTime(a.ExpiresAt) {
		return ErrAuthorization
	}
	start, _ := time.Parse(TimeFormat, a.NotBefore)
	end, _ := time.Parse(TimeFormat, a.ExpiresAt)
	if !end.After(start) || end.Sub(start) > 2*time.Hour || now.Before(start) || !now.Before(end) {
		return ErrAuthorization
	}
	return nil
}

func (a OfflineAuthorization) Digest() string { raw, _ := json.Marshal(a); return Hash(raw) }
func boundedText(s string, n int) bool {
	return len(s) > 0 && len(s) <= n && strings.TrimSpace(s) == s && validValues(s, "")
}

func ParseOfflineAuthorization(raw []byte, now time.Time) (OfflineAuthorization, error) {
	var a OfflineAuthorization
	fields, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return a, ErrAuthorization
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil || d.Decode(&struct{}{}) != io.EOF {
		return a, ErrAuthorization
	}
	// No omitted/null policy flags or identity fields are accepted.
	canonical, _ := json.Marshal(a)
	expected, _ := connector.DecodeStrictJSONObject(canonical)
	if len(fields) != len(expected) {
		return a, ErrAuthorization
	}
	for key := range expected {
		if fields[key] == nil {
			return a, ErrAuthorization
		}
	}
	return a, a.Validate(now)
}

// ReadOwnedFile rejects symlinks, hardlinks, permissions widening and replacement
// during open. It never returns file contents in an error.
func ReadOwnedFile(path string, limit int64, owner uint32) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrAuthorization
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, ErrAuthorization
	}
	valid := func(info os.FileInfo) bool {
		stat, ok := info.Sys().(*syscall.Stat_t)
		return ok && info.Mode().IsRegular() && stat.Uid == owner && stat.Nlink == 1 &&
			(info.Mode().Perm() == 0o400 || info.Mode().Perm() == 0o600) && info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0
	}
	if !valid(before) {
		return nil, ErrAuthorization
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrAuthorization
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !valid(after) {
		return nil, ErrAuthorization
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, ErrAuthorization
	}
	return raw, nil
}

func LoadOfflineAuthorization(path, target, version, commit string, now time.Time) (OfflineAuthorization, error) {
	raw, err := ReadOwnedFile(path, 64<<10, 0)
	if err != nil {
		return OfflineAuthorization{}, err
	}
	a, err := ParseOfflineAuthorization(raw, now)
	if err != nil {
		return a, err
	}
	machine, err := os.ReadFile("/etc/machine-id")
	if err != nil || a.MachineIDSHA256 != Hash(bytes.TrimSpace(machine)) || a.TargetRef != target || a.ProductVersion != version || a.ProductCommit != commit {
		return a, ErrAuthorization
	}
	return a, nil
}

type OfflineBatch struct {
	Items        []Item
	Observations []ObservationItem
	SHA256       string
}

func (b OfflineBatch) Count() int { return len(b.Items) + len(b.Observations) }

func ValidateOfflineBatch(raw []byte, a OfflineAuthorization, now time.Time) (OfflineBatch, error) {
	var result OfflineBatch
	if a.SchemaVersion == ResourceOfflineAuthorizationVersion || a.Validate(now) != nil || len(raw) >= MaxPageBytes || Hash(raw) != a.BundleSHA256 {
		return result, ErrAuthorization
	}
	var binding connector.EnvelopeBinding = profileBinding(a.Profile)
	if a.SchemaVersion == ObservationOfflineAuthorizationVersion {
		binding = observationBinding(a.Profile)
	}
	batch, err := connector.ValidatePortableEventBatch(raw, binding)
	if err != nil {
		return result, ErrInvalid
	}
	if batch.ProducerInstance != a.SourceInstance || len(batch.Events) > a.MaxRecords || batch.NextCursor != "" || batch.HighWatermark != "" || batch.HasMore {
		return result, ErrInvalid
	}
	for _, event := range batch.Events {
		if a.SchemaVersion == ObservationOfflineAuthorizationVersion {
			item, err := ValidateObservationItem(event.CanonicalBytes(), a.Profile)
			if err != nil || item.Document.SourceInstance != a.SourceInstance || item.Document.OrgID != a.OrgID || item.Document.ProjectID != a.ProjectID {
				return OfflineBatch{}, ErrInvalid
			}
			result.Observations = append(result.Observations, item)
			continue
		}
		item, err := ValidateItem(event.CanonicalBytes(), a.Profile)
		if err != nil || item.Document.SourceInstance != a.SourceInstance || item.Document.OrgID != a.OrgID || item.Document.ProjectID != a.ProjectID {
			return result, ErrInvalid
		}
		result.Items = append(result.Items, item)
	}
	result.SHA256 = Hash(raw)
	return result, nil
}
