package sourcefacts

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/foigent/x-insight/internal/connector"
)

const BudgetOfflineAuthorizationVersion = "xinsight.budget-offline-authorization.v1"
const BudgetOfflineBundleVersion = "xinsight.budget-offline-bundle.v1"
const BudgetSourceCommit = "a84ac2726e079796ade0af573b575b7c11bbdfbc"
const MaxBudgetOfflineBytes = 50*MaxBudgetReadingBytes + 128*1024

// Each authorization binds one bounded catalog and its complete, independent
// source readings. It cannot activate the network consumer.
type BudgetOfflineAuthorization struct {
	SchemaVersion        string   `json:"schema_version"`
	CollectionID         string   `json:"collection_id"`
	Environment          string   `json:"environment"`
	TargetRef            string   `json:"target_ref"`
	MachineIDSHA256      string   `json:"machine_id_sha256"`
	ProductVersion       string   `json:"product_version"`
	ProductCommit        string   `json:"product_commit"`
	TargetDatabaseSchema string   `json:"target_database_schema"`
	SourceCommit         string   `json:"source_commit"`
	SourceManifestSHA256 string   `json:"source_manifest_sha256"`
	SourceSystemID       string   `json:"source_system_id"`
	SourceInstance       string   `json:"source_instance"`
	OrgID                string   `json:"org_id"`
	ProjectID            string   `json:"project_id"`
	ScopeGeneration      string   `json:"scope_generation"`
	Profiles             []string `json:"profiles"`
	NotBefore            string   `json:"not_before"`
	ExpiresAt            string   `json:"expires_at"`
	BundlePath           string   `json:"bundle_path"`
	BundleSHA256         string   `json:"bundle_sha256"`
}

func BudgetManifestSHA256() string {
	b, _ := budgetAssets.ReadFile("contract/budgets/manifest.json")
	return Hash(b)
}
func (a BudgetOfflineAuthorization) Collection() BudgetCollection {
	return BudgetCollection{ID: a.CollectionID, SourceSystemID: a.SourceSystemID, SourceInstance: a.SourceInstance, OrgID: a.OrgID, ProjectID: a.ProjectID, ScopeGeneration: a.ScopeGeneration, Profiles: a.Profiles, Transport: "bounded_offline"}
}
func (a BudgetOfflineAuthorization) Digest() string { b, _ := json.Marshal(a); return Hash(b) }
func (a BudgetOfflineAuthorization) Validate(now time.Time) error {
	if a.SchemaVersion != BudgetOfflineAuthorizationVersion || !ValidBudgetCollection(a.Collection()) ||
		(a.Environment != "development" && a.Environment != "test" && a.Environment != "integration") ||
		!namePattern.MatchString(a.TargetRef) || !hashPattern.MatchString(a.MachineIDSHA256) || !boundedText(a.ProductVersion, 128) || !boundedText(a.ProductCommit, 128) ||
		(a.TargetDatabaseSchema != "36" && a.TargetDatabaseSchema != "37" && a.TargetDatabaseSchema != "38" && a.TargetDatabaseSchema != "39" && a.TargetDatabaseSchema != "40" && a.TargetDatabaseSchema != "41" && a.TargetDatabaseSchema != "42" && a.TargetDatabaseSchema != "43" && a.TargetDatabaseSchema != "44") || a.SourceCommit != BudgetSourceCommit || a.SourceManifestSHA256 != BudgetManifestSHA256() ||
		!filepath.IsAbs(a.BundlePath) || filepath.Clean(a.BundlePath) != a.BundlePath || !hashPattern.MatchString(a.BundleSHA256) || !ValidTime(a.NotBefore) || !ValidTime(a.ExpiresAt) {
		return ErrAuthorization
	}
	start, _ := time.Parse(TimeFormat, a.NotBefore)
	end, _ := time.Parse(TimeFormat, a.ExpiresAt)
	if !end.After(start) || end.Sub(start) > 2*time.Hour || now.Before(start) || !now.Before(end) {
		return ErrAuthorization
	}
	return nil
}
func ParseBudgetOfflineAuthorization(raw []byte, now time.Time) (BudgetOfflineAuthorization, error) {
	var a BudgetOfflineAuthorization
	fields, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return a, ErrAuthorization
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil || d.Decode(&struct{}{}) != io.EOF {
		return a, ErrAuthorization
	}
	serialized, _ := json.Marshal(a)
	expected, _ := connector.DecodeStrictJSONObject(serialized)
	if len(fields) != len(expected) {
		return a, ErrAuthorization
	}
	for k := range expected {
		if fields[k] == nil {
			return a, ErrAuthorization
		}
	}
	return a, a.Validate(now)
}
func LoadBudgetOfflineAuthorization(path, target, version, commit string, now time.Time) (BudgetOfflineAuthorization, error) {
	raw, err := ReadOwnedFile(path, 64<<10, 0)
	if err != nil {
		return BudgetOfflineAuthorization{}, err
	}
	a, err := ParseBudgetOfflineAuthorization(raw, now)
	if err != nil {
		return a, err
	}
	machine, err := os.ReadFile("/etc/machine-id")
	if err != nil || a.MachineIDSHA256 != Hash(bytes.TrimSpace(machine)) || a.TargetRef != target || a.ProductVersion != version || a.ProductCommit != commit {
		return a, ErrAuthorization
	}
	return a, nil
}

type BudgetOfflineEntry struct {
	AlertsInputCursor string          `json:"alerts_input_cursor"`
	Reading           json.RawMessage `json:"reading"`
}
type BudgetOfflineBundle struct {
	SchemaVersion string               `json:"schema_version"`
	Catalog       json.RawMessage      `json:"catalog"`
	Readings      []BudgetOfflineEntry `json:"readings"`
}

func ValidateBudgetOfflineBundle(raw []byte, a BudgetOfflineAuthorization, now time.Time) (BudgetOfflineBundle, error) {
	var b BudgetOfflineBundle
	if a.Validate(now) != nil || len(raw) > MaxBudgetOfflineBytes || Hash(raw) != a.BundleSHA256 {
		return b, ErrAuthorization
	}
	if _, err := connector.DecodeStrictJSONObject(raw); err != nil {
		return b, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&b) != nil || d.Decode(&struct{}{}) != io.EOF || b.SchemaVersion != BudgetOfflineBundleVersion || b.Readings == nil || len(b.Readings) > 50 {
		return b, ErrInvalid
	}
	c, err := DecodeBudgetCatalog(b.Catalog)
	if err != nil || c.HasMore || c.SourceInstance != a.SourceInstance || c.OrgID != a.OrgID || c.ProjectID != a.ProjectID || !SameBudgetProfiles(c.Profiles, a.Profiles) {
		return b, ErrInvalid
	}
	index, cursor := 0, ""
	seen := map[string]bool{}
	for _, item := range b.Readings {
		r, err := DecodeBudgetReading(item.Reading)
		if err != nil || index >= len(c.Entries) || item.AlertsInputCursor != cursor || seen[r.ReadingID] || MatchBudgetReading(r, c, c.Entries[index], a.Profiles) != nil {
			return b, ErrInvalid
		}
		seen[r.ReadingID] = true
		next := Text(r.Body(BudgetAlertProfile), "next_cursor")
		if next != "" && next == cursor {
			return b, ErrInvalid
		}
		cursor = next
		if cursor == "" {
			index++
		}
	}
	if index != len(c.Entries) || cursor != "" {
		return b, ErrInvalid
	}
	return b, nil
}
