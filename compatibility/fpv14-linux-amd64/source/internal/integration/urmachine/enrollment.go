package urmachine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

var enrollmentRef = regexp.MustCompile(`^xise_[1-9][0-9]{0,18}$`)

// EnrollmentClient cannot be constructed with a policy-manage bearer. UR
// still reauthorizes every request against the delegated source enrollment.
type EnrollmentClient struct{ client *Client }

func NewEnrollmentClient(origin, bearer string, identity Identity, transport *http.Client) (*EnrollmentClient, error) {
	c, err := newClient(origin, bearer, "ur-xse1-", identity, transport)
	if err != nil {
		return nil, err
	}
	return &EnrollmentClient{client: c}, nil
}

type Enrollment struct {
	EnrollmentID   string   `json:"enrollment_id"`
	Revision       string   `json:"revision"`
	Profiles       []string `json:"profiles"`
	IncludeContent bool     `json:"include_content"`
	FactsStatus    string   `json:"facts_status"`
	ContentStatus  string   `json:"content_status"`
}
type EnrollmentProject struct {
	ProjectID   string       `json:"project_id"`
	DisplayName string       `json:"display_name"`
	Enrollments []Enrollment `json:"enrollments"`
}
type EnrollmentProjects struct {
	SchemaVersion  string              `json:"schema_version"`
	InstallationID string              `json:"installation_id"`
	SourceInstance string              `json:"source_instance"`
	OrgID          string              `json:"org_id"`
	Projects       []EnrollmentProject `json:"projects"`
	NextCursor     string              `json:"next_cursor"`
}

func (c *EnrollmentClient) Projects(ctx context.Context, cursor string) (EnrollmentProjects, error) {
	var result EnrollmentProjects
	if c == nil || c.client == nil || cursor != "" && !positiveID.MatchString(cursor) {
		return result, ErrContract
	}
	path := prefix + "/source-enrollments/projects"
	if cursor != "" {
		path += "?cursor=" + url.QueryEscape(cursor)
	}
	raw, _, err := c.client.request(ctx, http.MethodGet, path, nil, false)
	if err != nil {
		return result, err
	}
	if json.Unmarshal(raw, &result) != nil || result.SchemaVersion != "unirouter.xinsight.source-enrollment-projects.v1" ||
		result.InstallationID != c.client.identity.InstallationID || result.SourceInstance != c.client.identity.SourceInstance ||
		result.OrgID != c.client.identity.OrgID || result.NextCursor != "" && !positiveID.MatchString(result.NextCursor) ||
		len(result.Projects) > 100 {
		return EnrollmentProjects{}, ErrContract
	}
	for _, project := range result.Projects {
		if !positiveID.MatchString(project.ProjectID) {
			return EnrollmentProjects{}, ErrContract
		}
		for _, enrollment := range project.Enrollments {
			if !enrollmentRef.MatchString(enrollment.EnrollmentID) || !positiveID.MatchString(enrollment.Revision) ||
				!validEnrollmentStatus(enrollment.FactsStatus) || !validEnrollmentStatus(enrollment.ContentStatus) {
				return EnrollmentProjects{}, ErrContract
			}
		}
	}
	return result, nil
}

func validEnrollmentStatus(value string) bool {
	switch value {
	case "not_requested", "pending", "active", "revoked", "requires_migration":
		return true
	default:
		return false
	}
}

type RegistrationReceipt struct {
	EnrollmentID     string `json:"enrollment_id"`
	Kind             string `json:"kind"`
	CredentialID     string `json:"credential_id"`
	Status           string `json:"status"`
	RegistrationHash string `json:"registration_hash"`
	Replayed         bool   `json:"replayed"`
}

// Register sends an already prepared, independent A or C verifier. The
// bearer itself is never sent in JSON; only UR's source-enroll credential may
// authorize the registration request.
func (c *EnrollmentClient) Register(ctx context.Context, projectID, enrollmentID, kind string, registration json.RawMessage) (RegistrationReceipt, error) {
	var result RegistrationReceipt
	if c == nil || c.client == nil || !positiveID.MatchString(projectID) || !enrollmentRef.MatchString(enrollmentID) ||
		(kind != "facts" && kind != "content") || len(registration) == 0 || !json.Valid(registration) {
		return result, ErrContract
	}
	var header struct {
		SchemaVersion  string `json:"schema_version"`
		TokenFormat    string `json:"token_format"`
		SourceInstance string `json:"source_instance"`
		OrgID          string `json:"org_id"`
		ProjectID      string `json:"project_id"`
	}
	if json.Unmarshal(registration, &header) != nil || header.SchemaVersion != "unirouter.xinsight.source-credential-registration.v1" ||
		header.TokenFormat != "ur-xf1-hex256.v1" || header.SourceInstance != c.client.identity.SourceInstance ||
		header.OrgID != c.client.identity.OrgID || header.ProjectID != projectID {
		return result, ErrContract
	}
	var normalized sourcefacts.SourceCredentialRegistration
	if json.Unmarshal(registration, &normalized) != nil {
		return result, ErrContract
	}
	normalizedRaw, err := json.Marshal(normalized)
	if err != nil {
		return result, ErrContract
	}
	expectedHash := sourcefacts.Hash(normalizedRaw)
	request, _ := json.Marshal(struct {
		EnrollmentID string          `json:"enrollment_id"`
		Kind         string          `json:"kind"`
		Registration json.RawMessage `json:"registration"`
	}{
		EnrollmentID: enrollmentID, Kind: kind, Registration: registration,
	})
	path := prefix + "/source-enrollments/projects/" + projectID + "/registrations"
	raw, status, err := c.client.request(ctx, http.MethodPost, path, request, true)
	if err != nil {
		return result, err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return result, ErrUnconfirmed
	}
	if json.Unmarshal(raw, &result) != nil || result.EnrollmentID != enrollmentID || result.Kind != kind ||
		result.CredentialID == "" || result.Status != "active" ||
		(status == http.StatusCreated && (result.Replayed || result.RegistrationHash != expectedHash)) ||
		(status == http.StatusOK && (!result.Replayed || result.RegistrationHash != expectedHash)) {
		return RegistrationReceipt{}, ErrUnconfirmed
	}
	return result, nil
}
