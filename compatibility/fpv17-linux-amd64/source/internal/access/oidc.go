package access

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const OIDCConfigSchema = "xinsight.oidc-provider.v1"

var oidcProviderKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,63}$`)

// OIDCConfig is a provider-neutral configuration contract. It intentionally
// contains no client secret and does not activate network discovery by itself.
// A later deployment can supply a secret through a separate systemd
// credential and bind this contract to an OIDC runtime implementation.
type OIDCConfig struct {
	SchemaVersion         string          `json:"schema_version"`
	ProviderKey           string          `json:"provider_key"`
	Issuer                string          `json:"issuer"`
	ClientID              string          `json:"client_id"`
	RedirectPath          string          `json:"redirect_path"`
	Scopes                []string        `json:"scopes"`
	SubjectClaim          string          `json:"subject_claim"`
	RoleClaim             string          `json:"role_claim,omitempty"`
	RoleMapping           map[string]Role `json:"role_mapping,omitempty"`
	PKCERequired          bool            `json:"pkce_required"`
	AuthorizationCodeOnly bool            `json:"authorization_code_only"`
	Enabled               bool            `json:"enabled"`
}

type AccessProviderCapabilities struct {
	LocalAccountState string `json:"local_account_state"`
	OIDCContract      string `json:"oidc_contract"`
	OIDCRuntimeState  string `json:"oidc_runtime_state"`
	Boundary          string `json:"boundary"`
}

func ProviderCapabilities() AccessProviderCapabilities {
	return AccessProviderCapabilities{
		LocalAccountState: "active",
		OIDCContract:      OIDCConfigSchema,
		OIDCRuntimeState:  "not_configured",
		Boundary:          "provider-neutral configuration seam only; no named provider or external OIDC traffic is activated",
	}
}

func ParseOIDCConfig(payload []byte) (OIDCConfig, error) {
	if len(payload) == 0 || len(payload) > MaxAccountFileBytes {
		return OIDCConfig{}, errors.New("OIDC configuration size is invalid")
	}
	if err := rejectDuplicateJSONKeys(payload); err != nil {
		return OIDCConfig{}, errors.New("OIDC configuration contains duplicate or invalid JSON fields")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var config OIDCConfig
	if err := decoder.Decode(&config); err != nil {
		return OIDCConfig{}, errors.New("OIDC configuration is invalid")
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return OIDCConfig{}, errors.New("OIDC configuration has trailing content")
	}
	if err := ValidateOIDCConfig(config); err != nil {
		return OIDCConfig{}, err
	}
	return config, nil
}

func ValidateOIDCConfig(config OIDCConfig) error {
	if config.SchemaVersion != OIDCConfigSchema ||
		!oidcProviderKeyPattern.MatchString(config.ProviderKey) ||
		len(config.ClientID) == 0 || len(config.ClientID) > 256 ||
		config.RedirectPath != "/api/v1/access/oidc/callback" ||
		config.SubjectClaim != "sub" ||
		!config.PKCERequired ||
		!config.AuthorizationCodeOnly {
		return errors.New("OIDC configuration contract is invalid")
	}
	issuer, err := url.Parse(config.Issuer)
	if err != nil ||
		issuer.Scheme != "https" ||
		issuer.Host == "" ||
		issuer.User != nil ||
		issuer.RawQuery != "" ||
		issuer.Fragment != "" ||
		strings.HasSuffix(issuer.Path, "/") {
		return errors.New("OIDC issuer must be a canonical HTTPS origin or path")
	}
	if len(config.Scopes) == 0 || len(config.Scopes) > 8 {
		return errors.New("OIDC scopes are invalid")
	}
	seenScopes := make(map[string]struct{}, len(config.Scopes))
	for _, scope := range config.Scopes {
		if scope == "" || strings.TrimSpace(scope) != scope || strings.ContainsAny(scope, "\t\r\n ") {
			return errors.New("OIDC scopes are invalid")
		}
		if _, duplicate := seenScopes[scope]; duplicate {
			return errors.New("OIDC scopes must be unique")
		}
		seenScopes[scope] = struct{}{}
	}
	if _, ok := seenScopes["openid"]; !ok {
		return errors.New("OIDC openid scope is required")
	}
	if config.RoleClaim == "" && len(config.RoleMapping) != 0 {
		return errors.New("OIDC role mapping requires a role claim")
	}
	if config.RoleClaim != "" {
		if len(config.RoleClaim) > 128 || strings.TrimSpace(config.RoleClaim) != config.RoleClaim {
			return errors.New("OIDC role claim is invalid")
		}
		if len(config.RoleMapping) == 0 || len(config.RoleMapping) > 64 {
			return errors.New("OIDC role mapping is invalid")
		}
		for external, role := range config.RoleMapping {
			if external == "" || len(external) > 160 || strings.TrimSpace(external) != external ||
				(role != RoleInvestigator &&
					role != RoleOperator &&
					role != RoleAccessOperator &&
					role != RolePlatformAdmin) {
				return errors.New("OIDC role mapping entry is invalid")
			}
		}
	}
	return nil
}

// CanonicalOIDCConfig returns stable bytes for review and deployment pinning.
// Map keys are projected to a sorted array so Go map iteration cannot change
// the digest input.
func CanonicalOIDCConfig(config OIDCConfig) ([]byte, error) {
	if err := ValidateOIDCConfig(config); err != nil {
		return nil, err
	}
	type roleBinding struct {
		External string `json:"external"`
		Role     Role   `json:"role"`
	}
	type canonical struct {
		SchemaVersion         string        `json:"schema_version"`
		ProviderKey           string        `json:"provider_key"`
		Issuer                string        `json:"issuer"`
		ClientID              string        `json:"client_id"`
		RedirectPath          string        `json:"redirect_path"`
		Scopes                []string      `json:"scopes"`
		SubjectClaim          string        `json:"subject_claim"`
		RoleClaim             string        `json:"role_claim,omitempty"`
		RoleMapping           []roleBinding `json:"role_mapping,omitempty"`
		PKCERequired          bool          `json:"pkce_required"`
		AuthorizationCodeOnly bool          `json:"authorization_code_only"`
		Enabled               bool          `json:"enabled"`
	}
	keys := make([]string, 0, len(config.RoleMapping))
	for external := range config.RoleMapping {
		keys = append(keys, external)
	}
	sort.Strings(keys)
	mapping := make([]roleBinding, 0, len(keys))
	for _, external := range keys {
		mapping = append(mapping, roleBinding{External: external, Role: config.RoleMapping[external]})
	}
	scopes := append([]string(nil), config.Scopes...)
	sort.Strings(scopes)
	return json.Marshal(canonical{
		SchemaVersion:         config.SchemaVersion,
		ProviderKey:           config.ProviderKey,
		Issuer:                config.Issuer,
		ClientID:              config.ClientID,
		RedirectPath:          config.RedirectPath,
		Scopes:                scopes,
		SubjectClaim:          config.SubjectClaim,
		RoleClaim:             config.RoleClaim,
		RoleMapping:           mapping,
		PKCERequired:          config.PKCERequired,
		AuthorizationCodeOnly: config.AuthorizationCodeOnly,
		Enabled:               config.Enabled,
	})
}
