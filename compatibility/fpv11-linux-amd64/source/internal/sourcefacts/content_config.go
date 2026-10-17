package sourcefacts

import (
	"net/url"
	"path/filepath"
	"strings"
)

const ManagedContentConfigVersion = "xinsight.managed-source-facts.v4"
const ContentEndpointPath = "/api/v1/integrations/xinsight/source/v2/content-manifests"
const ContentCapabilitiesPath = "/api/v1/integrations/xinsight/source/v2/content-capabilities"

// These are XI configuration bindings, not an upstream management DTO.
// RegistrationHash is supplied by the registration authority, never recomputed.
type ManagedCredentialBinding struct {
	Name             string `json:"name"`
	Scope            string `json:"scope"`
	SourceSystemID   string `json:"source_system_id"`
	SourceInstance   string `json:"source_instance"`
	OrgID            string `json:"org_id"`
	ProjectID        string `json:"project_id"`
	CredentialID     string `json:"credential_id"`
	RegistrationHash string `json:"registration_hash"`
	KeyFile          string `json:"key_file"`
}

type ManagedContentConfig struct {
	CollectionID           string          `json:"collection_id"`
	SourceSystemID         string          `json:"source_system_id"`
	SourceInstance         string          `json:"source_instance"`
	OrgID                  string          `json:"org_id"`
	ProjectID              string          `json:"project_id"`
	CredentialBinding      string          `json:"credential_binding"`
	ContentScopeRevision   string          `json:"content_scope_revision"`
	RecoveryEpoch          string          `json:"recovery_epoch"`
	AllowedPurposes        []string        `json:"allowed_purposes"`
	CoverageFrom           ContentBoundary `json:"coverage_from"`
	Endpoint               string          `json:"endpoint"`
	AllowPrivateTunnelHTTP bool            `json:"allow_private_tunnel_http"`
	CredentialID           string          `json:"-"`
	RegistrationHash       string          `json:"-"`
	KeyFile                string          `json:"-"`
}

func (p ManagedContentConfig) Registration() ContentCollection {
	return ContentCollection{ID: p.CollectionID, SourceSystemID: p.SourceSystemID, SourceInstance: p.SourceInstance, OrgID: p.OrgID, ProjectID: p.ProjectID, CredentialID: p.CredentialID, RegistrationHash: p.RegistrationHash, ContentScopeRevision: p.ContentScopeRevision, RecoveryEpoch: p.RecoveryEpoch, AllowedPurposes: p.AllowedPurposes, CoverageFrom: p.CoverageFrom}
}
func safeContentKeyPath(p string) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p && strings.TrimSpace(p) == p && !strings.ContainsAny(p, " \r\n\t\v\f\x00%\"\\")
}
func (c ManagedConfig) ResolveContent(p ManagedContentConfig) (ManagedContentConfig, error) {
	for _, b := range c.Bindings {
		if b.Name == p.CredentialBinding && b.Scope == "content" && b.SourceSystemID == p.SourceSystemID && b.SourceInstance == p.SourceInstance && b.OrgID == p.OrgID && b.ProjectID == p.ProjectID {
			p.CredentialID, p.RegistrationHash, p.KeyFile = b.CredentialID, b.RegistrationHash, b.KeyFile
			return p, nil
		}
	}
	return p, ErrAuthorization
}
func (p ManagedContentConfig) Validate() error {
	u, e := url.Parse(p.Endpoint)
	if !ValidContentCollection(p.Registration()) || !boundedText(p.CredentialBinding, 128) || !safeContentKeyPath(p.KeyFile) || e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Path != ContentEndpointPath || (u.Scheme != "https" && !(u.Scheme == "http" && p.AllowPrivateTunnelHTTP)) {
		return ErrInvalid
	}
	return nil
}
func (c ManagedConfig) validateContentConfig() error {
	if c.SchemaVersion != ManagedContentConfigVersion {
		if len(c.Bindings)+len(c.Contents) > 0 {
			return ErrInvalid
		}
		return nil
	}
	if len(c.Contents) == 0 || len(c.Bindings) == 0 || len(c.Bindings) > 32 {
		return ErrInvalid
	}
	names, scopes := map[string]bool{}, map[string]bool{}
	for _, b := range c.Bindings {
		key := strings.Join([]string{b.SourceSystemID, b.SourceInstance, b.OrgID, b.ProjectID, b.Scope}, "\x00")
		if names[b.Name] || scopes[key] || !boundedText(b.Name, 128) || (b.Scope != "legacy" && b.Scope != "content") || !uuidPattern.MatchString(b.SourceSystemID) || !boundedText(b.SourceInstance, 128) || !Positive(b.OrgID) || !Positive(b.ProjectID) || !boundedText(b.CredentialID, 128) || !hashPattern.MatchString(b.RegistrationHash) || !safeContentKeyPath(b.KeyFile) {
			return ErrInvalid
		}
		for _, other := range c.Bindings {
			if other.Name != b.Name && other.SourceSystemID == b.SourceSystemID && other.SourceInstance == b.SourceInstance && other.OrgID == b.OrgID && other.ProjectID == b.ProjectID && other.Scope != b.Scope && (other.CredentialID == b.CredentialID || other.RegistrationHash == b.RegistrationHash || other.KeyFile == b.KeyFile) {
				return ErrAuthorization
			}
		}
		names[b.Name], scopes[key] = true, true
	}
	// Existing profile/budget fields keep their historical wire representation;
	// v4 additionally requires their exact legacy binding, without rewriting it.
	legacy := func(source, instance, org, project, key string) bool {
		for _, b := range c.Bindings {
			if b.Scope == "legacy" && b.SourceSystemID == source && b.SourceInstance == instance && b.OrgID == org && b.ProjectID == project && b.KeyFile == key {
				return true
			}
		}
		return false
	}
	for _, p := range c.Profiles {
		if !legacy(p.SourceSystemID, p.SourceInstance, p.OrgID, p.ProjectID, p.KeyFile) {
			return ErrAuthorization
		}
	}
	for _, p := range c.Budgets {
		if !legacy(p.SourceSystemID, p.SourceInstance, p.OrgID, p.ProjectID, p.KeyFile) {
			return ErrAuthorization
		}
	}
	for _, p := range c.Contents {
		resolved, e := c.ResolveContent(p)
		if e != nil {
			return e
		}
		if e = resolved.Validate(); e != nil {
			return e
		}
	}
	return nil
}

// R1 has not specified a management DTO for credential IDs; preserve exact
// opaque binding IDs here rather than applying legacy evidence credential IDs.
func validManagedContentConfigValues(v any, key string) bool {
	switch value := v.(type) {
	case map[string]any:
		for k, item := range value {
			if !validManagedContentConfigValues(item, k) {
				return false
			}
		}
		return true
	case []any:
		for _, item := range value {
			if !validManagedContentConfigValues(item, key) {
				return false
			}
		}
		return true
	case string:
		if key == "credential_id" {
			return boundedText(value, 128)
		}
	}
	return validValues(v, key)
}
