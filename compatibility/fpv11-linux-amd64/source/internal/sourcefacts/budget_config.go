package sourcefacts

import (
	"net/url"
	"path/filepath"
	"strings"
)

// Explicit D registration uses a separate collection identity and source-only
// credential. Adding it never promotes an existing event-stream registration.
type ManagedBudgetConfig struct {
	CollectionID           string   `json:"collection_id"`
	SourceSystemID         string   `json:"source_system_id"`
	SourceInstance         string   `json:"source_instance"`
	OrgID                  string   `json:"org_id"`
	ProjectID              string   `json:"project_id"`
	Profiles               []string `json:"profiles"`
	ScopeGeneration        string   `json:"scope_generation"`
	Endpoint               string   `json:"endpoint"`
	KeyFile                string   `json:"key_file"`
	AllowPrivateTunnelHTTP bool     `json:"allow_private_tunnel_http"`
}

func (p ManagedBudgetConfig) Registration() BudgetCollection {
	return BudgetCollection{ID: p.CollectionID, SourceSystemID: p.SourceSystemID, SourceInstance: p.SourceInstance, OrgID: p.OrgID, ProjectID: p.ProjectID, ScopeGeneration: p.ScopeGeneration, Profiles: p.Profiles}
}

func (p ManagedBudgetConfig) Validate() error {
	u, err := url.Parse(p.Endpoint)
	if !ValidBudgetCollection(p.Registration()) || err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Path != BudgetEndpointPath || (u.Scheme != "https" && !(u.Scheme == "http" && p.AllowPrivateTunnelHTTP)) || !filepath.IsAbs(p.KeyFile) || filepath.Clean(p.KeyFile) != p.KeyFile || strings.TrimSpace(p.KeyFile) != p.KeyFile || strings.ContainsAny(p.KeyFile, " \r\n\t\v\f\x00%\"\\") {
		return ErrInvalid
	}
	return nil
}

func (c ManagedConfig) CredentialFiles() []string {
	files := make([]string, 0, len(c.Profiles)+len(c.Budgets))
	for _, p := range c.Profiles {
		files = append(files, p.KeyFile)
	}
	for _, p := range c.Budgets {
		files = append(files, p.KeyFile)
	}
	for _, p := range c.Contents {
		if resolved, err := c.ResolveContent(p); err == nil {
			files = append(files, resolved.KeyFile)
		}
	}
	return files
}
