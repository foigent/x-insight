// Package urmachine speaks only the delegated UniRouter XI machine contract.
// It carries no authority of its own: the caller supplies a separately
// authorized, purpose-specific bearer and the exact paired installation.
package urmachine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const prefix = "/api/v1/integrations/xinsight"
const maxResponseBytes = 256 << 10

var positiveID = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
var operationRef = regexp.MustCompile(`^xipw_[0-9a-f]{32}$`)
var delegationRef = regexp.MustCompile(`^xipg_[1-9][0-9]{0,18}$`)

var ErrContract = errors.New("UniRouter machine contract mismatch")
var ErrUnauthorized = errors.New("UniRouter machine authorization unavailable")
var ErrConflict = errors.New("UniRouter machine version or scope conflict")
var ErrNotFound = errors.New("UniRouter machine operation or scope not found")
var ErrUnavailable = errors.New("UniRouter machine service unavailable")
var ErrUnconfirmed = errors.New("UniRouter machine write outcome unconfirmed")

type Identity struct {
	InstallationID string
	SourceInstance string
	OrgID          string
}

type Client struct {
	origin   *url.URL
	http     *http.Client
	bearer   string
	identity Identity
}

func NewPolicyClient(origin, bearer string, identity Identity, transport *http.Client) (*Client, error) {
	return newClient(origin, bearer, "ur-xpm1-", identity, transport)
}

func newClient(origin, bearer, purposePrefix string, identity Identity, transport *http.Client) (*Client, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		u.Path != "" && u.Path != "/" || strings.HasSuffix(u.Host, ".") ||
		!strings.HasPrefix(bearer, purposePrefix) || strings.ContainsAny(bearer, "\r\n ") || identity.InstallationID == "" ||
		identity.SourceInstance == "" || !positiveID.MatchString(identity.OrgID) || transport == nil {
		return nil, ErrContract
	}
	copyClient := *transport
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{origin: u, http: &copyClient, bearer: bearer, identity: identity}, nil
}

func (c *Client) request(ctx context.Context, method, path string, body []byte, isWrite bool) ([]byte, int, error) {
	if c == nil || !strings.HasPrefix(path, prefix+"/") || strings.Contains(path, "..") || len(body) > 64<<10 {
		return nil, 0, ErrContract
	}
	u := *c.origin
	parts := strings.SplitN(path, "?", 2)
	u.Path = parts[0]
	if len(parts) == 2 {
		u.RawQuery = parts[1]
	}
	var input io.Reader
	if body != nil {
		input = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), input)
	if err != nil {
		return nil, 0, ErrContract
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-store")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		if isWrite {
			return nil, 0, ErrUnconfirmed
		}
		return nil, 0, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, response.StatusCode, ErrUnauthorized
	}
	if response.StatusCode == http.StatusConflict {
		return nil, response.StatusCode, ErrConflict
	}
	if response.StatusCode == http.StatusNotFound {
		return nil, response.StatusCode, ErrNotFound
	}
	if response.StatusCode >= 500 {
		if isWrite {
			return nil, response.StatusCode, ErrUnconfirmed
		}
		return nil, response.StatusCode, ErrUnavailable
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response.StatusCode, ErrContract
	}
	noStore := false
	for _, part := range strings.Split(response.Header.Get("Cache-Control"), ",") {
		if strings.EqualFold(strings.TrimSpace(part), "no-store") {
			noStore = true
		}
	}
	if !noStore || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		if isWrite {
			return nil, response.StatusCode, ErrUnconfirmed
		}
		return nil, response.StatusCode, ErrContract
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
		if isWrite {
			return nil, response.StatusCode, ErrUnconfirmed
		}
		return nil, response.StatusCode, ErrContract
	}
	return raw, response.StatusCode, nil
}

type Grant struct {
	GrantID    string   `json:"grant_id"`
	Revision   string   `json:"revision"`
	TargetKind string   `json:"target_kind"`
	Coverage   string   `json:"coverage"`
	ProjectID  string   `json:"project_id"`
	UserID     string   `json:"user_id"`
	Actions    []string `json:"actions"`
}
type Project struct {
	ProjectID   string `json:"project_id"`
	DisplayName string `json:"display_name"`
}
type PolicyScopes struct {
	SchemaVersion  string    `json:"schema_version"`
	InstallationID string    `json:"installation_id"`
	SourceInstance string    `json:"source_instance"`
	OrgID          string    `json:"org_id"`
	Grants         []Grant   `json:"grants"`
	Projects       []Project `json:"projects"`
	NextCursor     string    `json:"next_cursor"`
}

type PolicyUser struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

type PolicyUsers struct {
	SchemaVersion string       `json:"schema_version"`
	ProjectID     string       `json:"project_id"`
	Users         []PolicyUser `json:"users"`
	NextCursor    string       `json:"next_cursor"`
}

func (c *Client) PolicyUsers(ctx context.Context, projectID, cursor string) (PolicyUsers, error) {
	var result PolicyUsers
	if !positiveID.MatchString(projectID) || cursor != "" && !positiveID.MatchString(cursor) {
		return result, ErrContract
	}
	path := prefix + "/payload-policy/projects/" + projectID + "/users"
	if cursor != "" {
		path += "?cursor=" + url.QueryEscape(cursor)
	}
	raw, _, err := c.request(ctx, http.MethodGet, path, nil, false)
	if err != nil {
		return result, err
	}
	if json.Unmarshal(raw, &result) != nil || result.SchemaVersion != "unirouter.xinsight.policy-users.v1" ||
		result.ProjectID != projectID || result.NextCursor != "" && !positiveID.MatchString(result.NextCursor) || len(result.Users) > 100 {
		return PolicyUsers{}, ErrContract
	}
	for _, user := range result.Users {
		if !positiveID.MatchString(user.UserID) {
			return PolicyUsers{}, ErrContract
		}
	}
	return result, nil
}

func (c *Client) PolicyScopes(ctx context.Context, cursor string) (PolicyScopes, error) {
	var result PolicyScopes
	if cursor != "" && !positiveID.MatchString(cursor) {
		return result, ErrContract
	}
	path := prefix + "/payload-policy/scopes"
	if cursor != "" {
		path += "?cursor=" + url.QueryEscape(cursor)
	}
	raw, _, err := c.request(ctx, http.MethodGet, path, nil, false)
	if err != nil {
		return result, err
	}
	if json.Unmarshal(raw, &result) != nil || result.SchemaVersion != "unirouter.xinsight.policy-scopes.v1" ||
		result.InstallationID != c.identity.InstallationID || result.SourceInstance != c.identity.SourceInstance || result.OrgID != c.identity.OrgID ||
		result.NextCursor != "" && !positiveID.MatchString(result.NextCursor) || len(result.Projects) > 100 {
		return PolicyScopes{}, ErrContract
	}
	for _, item := range result.Projects {
		if !positiveID.MatchString(item.ProjectID) {
			return PolicyScopes{}, ErrContract
		}
	}
	return result, nil
}

type PolicyView struct {
	SchemaVersion      string          `json:"schema_version"`
	SourceInstance     string          `json:"source_instance"`
	OrgID              string          `json:"org_id"`
	ProjectID          string          `json:"project_id"`
	UserID             string          `json:"user_id"`
	Enterprise         json.RawMessage `json:"enterprise"`
	Project            json.RawMessage `json:"project"`
	User               json.RawMessage `json:"user"`
	Effective          json.RawMessage `json:"effective"`
	VersionVector      string          `json:"version_vector"`
	StorageStatus      string          `json:"storage_status"`
	DeploymentMode     string          `json:"deployment_mode"`
	FullContentEnabled bool            `json:"full_content_enabled"`
	Limits             *struct {
		MaxSavedBytes int64 `json:"max_saved_bytes"`
	} `json:"limits,omitempty"`
	PolicyCapabilities *struct {
		SupportedProtocols []string `json:"supported_protocols"`
		ObservationPoints  []string `json:"observation_points"`
	} `json:"policy_capabilities,omitempty"`
}

func (c *Client) ReadProjectPolicy(ctx context.Context, projectID, delegationID string) (PolicyView, error) {
	if !positiveID.MatchString(projectID) || !delegationRef.MatchString(delegationID) {
		return PolicyView{}, ErrContract
	}
	path := fmt.Sprintf("%s/payload-policy/projects/%s?delegation_id=%s", prefix, projectID, delegationID)
	return c.readPolicy(ctx, path, projectID, "")
}

func (c *Client) ReadEnterprisePolicy(ctx context.Context, delegationID string) (PolicyView, error) {
	if !delegationRef.MatchString(delegationID) {
		return PolicyView{}, ErrContract
	}
	path := fmt.Sprintf("%s/payload-policy/organizations/%s?delegation_id=%s", prefix, c.identity.OrgID, delegationID)
	return c.readPolicy(ctx, path, "", "")
}

func (c *Client) ReadUserPolicy(ctx context.Context, projectID, userID, delegationID string) (PolicyView, error) {
	if !positiveID.MatchString(projectID) || !positiveID.MatchString(userID) || !delegationRef.MatchString(delegationID) {
		return PolicyView{}, ErrContract
	}
	path := fmt.Sprintf("%s/payload-policy/projects/%s/users/%s?delegation_id=%s", prefix, projectID, userID, delegationID)
	return c.readPolicy(ctx, path, projectID, userID)
}

func (c *Client) readPolicy(ctx context.Context, path, projectID, userID string) (PolicyView, error) {
	var view PolicyView
	raw, _, err := c.request(ctx, http.MethodGet, path, nil, false)
	if err != nil {
		return view, err
	}
	if json.Unmarshal(raw, &view) != nil || view.SchemaVersion != "unirouter.payload-policy.v2" ||
		view.SourceInstance != c.identity.SourceInstance || view.OrgID != c.identity.OrgID || view.ProjectID != projectID ||
		view.UserID != userID ||
		len(view.Enterprise) == 0 || view.VersionVector == "" {
		return PolicyView{}, ErrContract
	}
	return view, nil
}

type WriteResult struct {
	SchemaVersion     string `json:"schema_version"`
	OperationRef      string `json:"operation_ref"`
	Result            string `json:"result"`
	SourceInstance    string `json:"source_instance"`
	PolicyRef         string `json:"policy_ref"`
	DesiredVersion    string `json:"desired_version"`
	EffectiveVersion  string `json:"effective_version"`
	ApplicationStatus string `json:"application_status"`
	AuditRef          string `json:"audit_ref"`
	ReadbackPath      string `json:"readback_path"`
}

// WriteProjectPolicy never interprets a 202 as effective. A transport failure
// remains unconfirmed until the same operation is read back.
func (c *Client) WriteProjectPolicy(ctx context.Context, projectID, operationID string, envelope json.RawMessage) (WriteResult, error) {
	if !positiveID.MatchString(projectID) || !operationRef.MatchString(operationID) || len(envelope) == 0 {
		return WriteResult{}, ErrContract
	}
	return c.writePolicy(ctx, prefix+"/payload-policy/projects/"+projectID, operationID, envelope)
}

func (c *Client) WriteEnterprisePolicy(ctx context.Context, operationID string, envelope json.RawMessage) (WriteResult, error) {
	if !operationRef.MatchString(operationID) || len(envelope) == 0 {
		return WriteResult{}, ErrContract
	}
	return c.writePolicy(ctx, prefix+"/payload-policy/organizations/"+c.identity.OrgID, operationID, envelope)
}

func (c *Client) WriteUserPolicy(ctx context.Context, projectID, userID, operationID string, envelope json.RawMessage) (WriteResult, error) {
	if !positiveID.MatchString(projectID) || !positiveID.MatchString(userID) || !operationRef.MatchString(operationID) || len(envelope) == 0 {
		return WriteResult{}, ErrContract
	}
	return c.writePolicy(ctx, prefix+"/payload-policy/projects/"+projectID+"/users/"+userID, operationID, envelope)
}

func (c *Client) writePolicy(ctx context.Context, path, operationID string, envelope json.RawMessage) (WriteResult, error) {
	var result WriteResult
	var header struct {
		SchemaVersion  string `json:"schema_version"`
		OperationRef   string `json:"operation_ref"`
		InstallationID string `json:"installation_id"`
		SourceInstance string `json:"source_instance"`
	}
	if json.Unmarshal(envelope, &header) != nil || header.SchemaVersion != "unirouter.xinsight.policy-write.v1" ||
		header.OperationRef != operationID || header.InstallationID != c.identity.InstallationID || header.SourceInstance != c.identity.SourceInstance {
		return result, ErrContract
	}
	raw, _, err := c.request(ctx, http.MethodPut, path, envelope, true)
	if err != nil {
		return result, err
	}
	if json.Unmarshal(raw, &result) != nil || result.SchemaVersion != "unirouter.xinsight.policy-write-result.v1" ||
		result.OperationRef != operationID || result.Result != "committed" || result.SourceInstance != c.identity.SourceInstance ||
		result.ReadbackPath != path || result.PolicyRef == "" || result.ApplicationStatus == "" {
		return WriteResult{}, ErrUnconfirmed
	}
	return result, nil
}

func (c *Client) ReadPolicyOperation(ctx context.Context, operationID string) (WriteResult, error) {
	var result WriteResult
	if !operationRef.MatchString(operationID) {
		return result, ErrContract
	}
	raw, _, err := c.request(ctx, http.MethodGet, prefix+"/payload-policy/operations/"+operationID, nil, false)
	if err != nil {
		return result, err
	}
	if json.Unmarshal(raw, &result) != nil || result.SchemaVersion != "unirouter.xinsight.policy-write-result.v1" ||
		result.OperationRef != operationID || result.SourceInstance != c.identity.SourceInstance {
		return WriteResult{}, ErrContract
	}
	return result, nil
}
