package connectorcontrol

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"time"
)

const SetupConsentSchema = "xinsight.connector-setup-consent.v1"
const SetupConsentViewSchema = "xinsight.connector-setup-consent-view.v1"
const SetupPreviewSchema = "xinsight.connector-setup-preview.v1"
const SetupStatusSchema = "xinsight.connector-setup-status.v1"
const LocalSetupRequestSchema = "xinsight.local-connector-setup-request.v1"
const LocalSetupResponseSchema = "xinsight.local-connector-setup-response.v1"

type SetupCustody struct {
	Mode              string `json:"mode"`
	RetentionSeconds  int64  `json:"retention_seconds"`
	ProjectQuotaBytes int64  `json:"project_quota_bytes"`
	MaxItemBytes      int64  `json:"max_item_bytes"`
}

func (s SetupCustody) Valid() bool {
	return (s.Mode == "disabled" || s.Mode == "full_content") && s.RetentionSeconds >= 1 && s.RetentionSeconds <= 31536000 && s.ProjectQuotaBytes >= 1 && s.ProjectQuotaBytes <= 1<<40 && s.MaxItemBytes >= 0 && s.MaxItemBytes <= 32<<20
}

type SetupConsentPut struct {
	SchemaVersion           string        `json:"schema_version"`
	RequestID               string        `json:"request_id"`
	ExpectedCustodyRevision int64         `json:"expected_custody_revision"`
	Custody                 *SetupCustody `json:"custody"`
}

var SetupRequestIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func (s SetupConsentPut) Valid() bool {
	return s.SchemaVersion == SetupConsentSchema && SetupRequestIDPattern.MatchString(s.RequestID) && s.ExpectedCustodyRevision >= 0 && (s.Custody == nil || s.Custody.Valid())
}

type SetupConsentView struct {
	SchemaVersion           string       `json:"schema_version"`
	InstallationID          string       `json:"installation_id"`
	DeploymentID            string       `json:"deployment_id"`
	XIOrigin                string       `json:"xi_origin"`
	UROrigin                string       `json:"ur_origin"`
	ConsentRevision         int64        `json:"consent_revision"`
	ConsentSHA256           string       `json:"consent_sha256"`
	ExpiresAt               time.Time    `json:"expires_at"`
	Custody                 SetupCustody `json:"custody"`
	ExpectedCustodyRevision int64        `json:"expected_custody_revision"`
}
type SetupCreated struct {
	Invitation InvitationPackage `json:"invitation"`
	Consent    SetupConsentView  `json:"consent"`
}
type SetupCustodyStatus struct {
	DesiredRevision   int64         `json:"desired_revision"`
	EffectiveRevision int64         `json:"effective_revision"`
	Loaded            bool          `json:"loaded"`
	Settings          *SetupCustody `json:"settings"`
}
type SetupPreviewCustody struct {
	DesiredRevision   int64         `json:"desired_revision"`
	EffectiveRevision int64         `json:"effective_revision"`
	Settings          *SetupCustody `json:"settings"`
}
type SetupPreview struct {
	SchemaVersion    string              `json:"schema_version"`
	DeploymentID     string              `json:"deployment_id"`
	UROrigin         string              `json:"ur_origin"`
	Custody          SetupPreviewCustody `json:"custody"`
	CanChangeCustody bool                `json:"can_change_custody"`
}
type SetupIssue struct {
	Code           string `json:"code"`
	RequiresChoice bool   `json:"requires_choice"`
}
type SetupReceiving struct {
	LastSuccessAt         *time.Time `json:"last_success_at"`
	ProfilesReceived      []string   `json:"profiles_received"`
	ContentReceivedAt     *time.Time `json:"content_received_at"`
	ContentReadVerifiedAt *time.Time `json:"content_read_verified_at"`
}
type SetupStatus struct {
	SchemaVersion   string             `json:"schema_version"`
	InstallationID  string             `json:"installation_id"`
	DeploymentID    string             `json:"deployment_id"`
	ConsentRevision int64              `json:"consent_revision"`
	State           string             `json:"state"`
	Custody         SetupCustodyStatus `json:"custody"`
	Receiving       SetupReceiving     `json:"receiving"`
	Issues          []SetupIssue       `json:"issues"`
}
type SetupLocalRequest struct {
	SchemaVersion     string           `json:"schema_version"`
	Action            string           `json:"action"`
	ActorSubject      string           `json:"actor_subject,omitempty"`
	ControlIdentityID string           `json:"control_identity_id,omitempty"`
	Consent           *SetupConsentPut `json:"consent,omitempty"`
	InstallationID    string           `json:"installation_id,omitempty"`
}
type SetupLocalResponse struct {
	SchemaVersion string        `json:"schema_version"`
	Action        string        `json:"action"`
	Preview       *SetupPreview `json:"preview,omitempty"`
	Created       *SetupCreated `json:"created,omitempty"`
	Status        *SetupStatus  `json:"status,omitempty"`
	Error         string        `json:"error,omitempty"`
}
type SetupController interface {
	ExecuteSetup(context.Context, SetupLocalRequest) (SetupLocalResponse, error)
}

func (r SetupLocalRequest) Valid() bool {
	if r.SchemaVersion != LocalSetupRequestSchema {
		return false
	}
	human := controllerHumanSubjectPattern.MatchString(r.ActorSubject) && r.ControlIdentityID == ""
	switch r.Action {
	case "preview":
		return human && r.Consent == nil && r.InstallationID == ""
	case "create":
		return human && r.Consent != nil && r.Consent.Valid() && r.InstallationID == ""
	case "status":
		return IsInstallationID(r.InstallationID) && r.Consent == nil && (human || r.ActorSubject == "" && connectorSetupIdentityPattern.MatchString(r.ControlIdentityID))
	}
	return false
}

var connectorSetupIdentityPattern = regexp.MustCompile(`^xsi_[A-Za-z0-9_-]{22}$`)

var ErrSetupUnauthorized = errors.New("XI setup subject is no longer authorized")

func ValidSetupConsentView(v SetupConsentView) bool {
	if v.SchemaVersion != SetupConsentViewSchema || !IsInstallationID(v.InstallationID) || !regexp.MustCompile(`^xid_[A-Za-z0-9_-]{22}$`).MatchString(v.DeploymentID) || !IsCanonicalHTTPSOrigin(v.XIOrigin) || !IsCanonicalHTTPSOrigin(v.UROrigin) || v.ConsentRevision != 1 || !IsSHA256Reference(v.ConsentSHA256) || v.ExpiresAt.IsZero() || v.ExpectedCustodyRevision < 0 || !v.Custody.Valid() {
		return false
	}
	saved := v.ConsentSHA256
	v.ConsentSHA256 = ""
	hash, _, err := CanonicalSHA256(v)
	return err == nil && hash == saved
}
func ValidSetupResponse(r SetupLocalRequest, out SetupLocalResponse) bool {
	if out.SchemaVersion != LocalSetupResponseSchema || out.Action != r.Action {
		return false
	}
	if out.Error != "" {
		return out.Preview == nil && out.Created == nil && out.Status == nil
	}
	switch r.Action {
	case "preview":
		return out.Preview != nil && out.Created == nil && out.Status == nil && out.Preview.SchemaVersion == SetupPreviewSchema && IsCanonicalHTTPSOrigin(out.Preview.UROrigin)
	case "create":
		if out.Preview != nil || out.Created == nil || out.Status != nil || !ValidSetupConsentView(out.Created.Consent) {
			return false
		}
		i, c := out.Created.Invitation, out.Created.Consent
		u, err := url.Parse(i.InstallURL)
		if err != nil {
			return false
		}
		fragment, err := url.ParseQuery(u.Fragment)
		if err != nil {
			return false
		}
		expected, err := BuildInstallURL(c.UROrigin, fragment.Get("invitation"), c.XIOrigin)
		return err == nil && i.InstallURL == expected && i.SchemaVersion == InvitationSchema && i.InstallationID == c.InstallationID && i.DeploymentID == c.DeploymentID && i.XInsightOrigin == c.XIOrigin && i.ExpiresAt.Equal(c.ExpiresAt)
	case "status":
		if out.Preview != nil || out.Created != nil || out.Status == nil || out.Status.SchemaVersion != SetupStatusSchema || out.Status.InstallationID != r.InstallationID {
			return false
		}
		switch out.Status.State {
		case "waiting_consent", "provisioning", "needs_action", "waiting_data", "ready", "failed":
			return true
		}
	}
	return false
}
