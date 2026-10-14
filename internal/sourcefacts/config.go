package sourcefacts

import (
	"bytes"
	"encoding/json"
	"github.com/foigent/x-insight/internal/connector"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const ManagedConfigVersion = "xinsight.managed-source-facts.v1"
const ManagedObservationConfigVersion = "xinsight.managed-source-facts.v2"
const ManagedBudgetConfigVersion = "xinsight.managed-source-facts.v3"

func ManagedEndpointPath(profile string) string {
	if ValidProfile(profile) {
		return "/api/v1/integrations/xinsight/source-facts"
	}
	if ValidObservationProfile(profile) {
		return "/api/v1/integrations/xinsight/source/v2/events"
	}
	return ""
}

type ManagedProfileConfig struct {
	StreamID               string `json:"stream_id"`
	SourceSystemID         string `json:"source_system_id"`
	SourceInstance         string `json:"source_instance"`
	OrgID                  string `json:"org_id"`
	ProjectID              string `json:"project_id"`
	Profile                string `json:"profile"`
	ScopeGeneration        string `json:"scope_generation"`
	Endpoint               string `json:"endpoint"`
	KeyFile                string `json:"key_file"`
	AllowPrivateTunnelHTTP bool   `json:"allow_private_tunnel_http"`
}

func (p ManagedProfileConfig) Registration() Stream {
	return Stream{ID: p.StreamID, SourceSystemID: p.SourceSystemID, SourceInstance: p.SourceInstance, OrgID: p.OrgID, ProjectID: p.ProjectID, Profile: p.Profile, ScopeGeneration: p.ScopeGeneration}
}
func (p ManagedProfileConfig) Matches(s Stream) bool {
	r := p.Registration()
	return r.ID == s.ID && r.SourceSystemID == s.SourceSystemID && r.SourceInstance == s.SourceInstance && r.OrgID == s.OrgID && r.ProjectID == s.ProjectID && r.Profile == s.Profile && r.ScopeGeneration == s.ScopeGeneration
}

type ManagedConfig struct {
	SchemaVersion       string                     `json:"schema_version"`
	PollIntervalSeconds int                        `json:"poll_interval_seconds"`
	RetryMaximumSeconds int                        `json:"retry_maximum_seconds"`
	Profiles            []ManagedProfileConfig     `json:"profiles"`
	Budgets             []ManagedBudgetConfig      `json:"budgets,omitempty"`
	Bindings            []ManagedCredentialBinding `json:"bindings,omitempty"`
	Contents            []ManagedContentConfig     `json:"contents,omitempty"`
}

func (c ManagedConfig) Validate() error {
	if err := c.validateContentConfig(); err != nil {
		return err
	}
	if c.SchemaVersion == ManagedBudgetConfigVersion && len(c.Budgets) == 0 {
		return ErrInvalid
	}
	maximum := 3
	if c.SchemaVersion == ManagedObservationConfigVersion || c.SchemaVersion == ManagedBudgetConfigVersion || c.SchemaVersion == ManagedContentConfigVersion {
		maximum = 16
	} else if c.SchemaVersion != ManagedConfigVersion {
		return ErrInvalid
	}
	if c.PollIntervalSeconds < 1 || c.PollIntervalSeconds > 600 || c.RetryMaximumSeconds < c.PollIntervalSeconds || c.RetryMaximumSeconds > 3600 || len(c.Profiles)+len(c.Budgets)+len(c.Contents) < 1 || len(c.Profiles)+len(c.Budgets)+len(c.Contents) > maximum || (c.SchemaVersion != ManagedBudgetConfigVersion && c.SchemaVersion != ManagedContentConfigVersion && len(c.Budgets) > 0) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, p := range c.Profiles {
		if c.SchemaVersion == ManagedConfigVersion && !ValidProfile(p.Profile) {
			return ErrInvalid
		}
		u, e := url.Parse(p.Endpoint)
		if !ValidRegistration(p.Registration()) || seen[p.StreamID] || e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != ManagedEndpointPath(p.Profile) ||
			(u.Scheme != "https" && !(u.Scheme == "http" && p.AllowPrivateTunnelHTTP)) || !filepath.IsAbs(p.KeyFile) || filepath.Clean(p.KeyFile) != p.KeyFile || strings.TrimSpace(p.KeyFile) != p.KeyFile || strings.ContainsAny(p.KeyFile, " \r\n\t\v\f\x00%\"\\") {
			return ErrInvalid
		}
		seen[p.StreamID] = true
	}
	for _, p := range c.Budgets {
		if seen[p.CollectionID] || p.Validate() != nil {
			return ErrInvalid
		}
		seen[p.CollectionID] = true
	}
	for _, p := range c.Contents {
		if seen[p.CollectionID] {
			return ErrInvalid
		}
		seen[p.CollectionID] = true
	}
	return nil
}
func ParseManagedConfig(raw []byte) (ManagedConfig, error) {
	var c ManagedConfig
	// The shared strict scanner rejects duplicate keys, invalid Unicode and null.
	fields, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return c, err
	}
	count := 4
	if Text(fields, "schema_version") == ManagedBudgetConfigVersion {
		count = 5
	}
	if Text(fields, "schema_version") == ManagedContentConfigVersion {
		count = 6
		if _, ok := fields["budgets"]; ok {
			count++
		}
	}
	validFields := validValues(fields, "")
	if Text(fields, "schema_version") == ManagedContentConfigVersion {
		validFields = validManagedContentConfigValues(fields, "")
	}
	if len(fields) != count || !validFields {
		return c, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(&struct{}{}) != io.EOF {
		return c, ErrInvalid
	}
	profiles, ok := fields["profiles"].([]any)
	if !ok {
		return c, ErrInvalid
	}
	for _, value := range profiles {
		p, ok := value.(map[string]any)
		if !ok || len(p) != 10 {
			return c, ErrInvalid
		}
	}
	if c.SchemaVersion == ManagedBudgetConfigVersion || (c.SchemaVersion == ManagedContentConfigVersion && fields["budgets"] != nil) {
		budgets, ok := fields["budgets"].([]any)
		if !ok {
			return c, ErrInvalid
		}
		for _, value := range budgets {
			p, ok := value.(map[string]any)
			if !ok || len(p) != 10 {
				return c, ErrInvalid
			}
		}
	}
	if c.SchemaVersion == ManagedContentConfigVersion {
		for key, count := range map[string]int{"bindings": 9, "contents": 12} {
			values, ok := fields[key].([]any)
			if !ok {
				return c, ErrInvalid
			}
			for _, v := range values {
				m, ok := v.(map[string]any)
				if !ok || len(m) != count {
					return c, ErrInvalid
				}
			}
		}
	}
	return c, c.Validate()
}
func LoadManagedConfig(path string) (ManagedConfig, error) {
	var zero ManagedConfig
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return zero, ErrInvalid
	}
	before, err := os.Lstat(path)
	if err != nil {
		return zero, ErrInvalid
	}
	valid := func(info os.FileInfo) bool {
		v, ok := info.Sys().(*syscall.Stat_t)
		return ok && v.Uid == 0 && v.Nlink == 1 && info.Mode().IsRegular() && info.Mode().Perm()&0o022 == 0 && info.Mode().Perm()&0o400 != 0 && info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0
	}
	if !valid(before) {
		return zero, ErrAuthorization
	}
	f, err := os.Open(path)
	if err != nil {
		return zero, ErrInvalid
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !valid(after) {
		return zero, ErrAuthorization
	}
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(raw) > 65536 {
		return zero, ErrInvalid
	}
	return ParseManagedConfig(raw)
}
