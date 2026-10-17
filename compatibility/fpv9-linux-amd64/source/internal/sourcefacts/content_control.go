package sourcefacts

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"reflect"
	"regexp"
	"strconv"

	"github.com/foigent/x-insight/internal/connector"
)

const SourceCredentialRegistrationVersion = "unirouter.xinsight.source-credential-registration.v1"
const ContentControlVersion = "unirouter.source-content-control.v1"

var contentControlOperation = regexp.MustCompile(`^[0-9a-f]{32}$`)
var contentControlPrefix = regexp.MustCompile(`^ur-xf1-[0-9a-f]{13}$`)
var contentControlVerifier = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Field order is UR's json.Marshal registration identity. This is deliberately
// separate from the six-field feed registration and source-fact CanonicalJSON.
type SourceCredentialRegistration struct {
	SchemaVersion  string   `json:"schema_version"`
	TokenFormat    string   `json:"token_format"`
	VerifierScheme string   `json:"verifier_scheme"`
	KeyPrefix      string   `json:"key_prefix"`
	VerifierSHA256 string   `json:"verifier_sha256"`
	OperationID    string   `json:"operation_id"`
	SourceInstance string   `json:"source_instance"`
	OrgID          string   `json:"org_id"`
	ProjectID      string   `json:"project_id"`
	Profiles       []string `json:"profiles"`
}

func (r SourceCredentialRegistration) MarshalRegistration() ([]byte, string, error) {
	if r.SchemaVersion != SourceCredentialRegistrationVersion || r.TokenFormat != "ur-xf1-hex256.v1" || r.VerifierScheme != "receiver_sha256_v1" || !contentControlPrefix.MatchString(r.KeyPrefix) || !contentControlVerifier.MatchString(r.VerifierSHA256) || !contentControlOperation.MatchString(r.OperationID) || !boundedText(r.SourceInstance, 128) || !Positive(r.OrgID) || !Positive(r.ProjectID) || !reflect.DeepEqual(r.Profiles, []string{ContentProfile}) {
		return nil, "", ErrInvalid
	}
	raw, err := json.Marshal(r)
	return raw, Hash(raw), err
}
func DecodeSourceCredentialRegistration(raw []byte) (SourceCredentialRegistration, error) {
	var r SourceCredentialRegistration
	if _, err := contentControlObject(raw, &r, []string{"schema_version", "token_format", "verifier_scheme", "key_prefix", "verifier_sha256", "operation_id", "source_instance", "org_id", "project_id", "profiles"}, nil); err != nil {
		return r, err
	}
	_, _, err := r.MarshalRegistration()
	return r, err
}

// A request owns the exact validated bytes. Callers cannot silently advance its
// CAS or replace its operation ID when recovering an ambiguous POST result.
type ContentControlRequest struct {
	action string
	raw    []byte
	value  contentControlRequestWire
}
type contentControlRequestWire struct {
	OperationID          string                        `json:"operation_id"`
	ExpectedStateVersion string                        `json:"expected_state_version"`
	CredentialID         string                        `json:"credential_id,omitempty"`
	RegistrationHash     string                        `json:"registration_hash,omitempty"`
	Registration         *SourceCredentialRegistration `json:"registration,omitempty"`
	AllowedIP            string                        `json:"allowed_ip,omitempty"`
}

func (r ContentControlRequest) Action() string { return r.action }
func (r ContentControlRequest) Bytes() []byte  { return bytes.Clone(r.raw) }
func (r ContentControlRequest) Hash() string   { return Hash(r.raw) }
func (r ContentControlRequest) RetryBytes(operationID, requestHash string) ([]byte, error) {
	if len(r.raw) == 0 || r.value.OperationID != operationID || r.Hash() != requestHash {
		return nil, ErrConflict
	}
	return r.Bytes(), nil
}
func DecodeContentControlRequest(action string, raw []byte) (ContentControlRequest, error) {
	r := ContentControlRequest{action: action}
	fields := []string{"operation_id", "expected_state_version"}
	switch action {
	case "prepare":
		fields = append(fields, "registration", "allowed_ip")
	case "rotate":
		fields = append(fields, "credential_id", "registration_hash", "registration")
	case "activate", "pause", "resume", "cancel", "revoke":
		fields = append(fields, "credential_id", "registration_hash")
	default:
		return r, ErrInvalid
	}
	m, err := contentControlObject(raw, &r.value, fields, nil)
	if err != nil || !contentControlOperation.MatchString(r.value.OperationID) || !contentControlDecimal(r.value.ExpectedStateVersion) {
		return r, ErrInvalid
	}
	if action != "prepare" && (!Positive(r.value.CredentialID) || !hashPattern.MatchString(r.value.RegistrationHash)) {
		return r, ErrInvalid
	}
	if action == "prepare" || action == "rotate" {
		registrationRaw, _ := json.Marshal(m["registration"])
		registration, err := DecodeSourceCredentialRegistration(registrationRaw)
		if err != nil || registration.OperationID != r.value.OperationID {
			return r, ErrInvalid
		}
		r.value.Registration = &registration
	}
	if action == "prepare" {
		if _, err := netip.ParsePrefix(r.value.AllowedIP); err != nil {
			return r, ErrInvalid
		}
	}
	r.raw = bytes.Clone(raw)
	return r, nil
}

type ContentControlCredential struct {
	CredentialID     string `json:"credential_id"`
	RegistrationHash string `json:"registration_hash"`
}
type ContentControlGET struct {
	SchemaVersion        string                    `json:"schema_version"`
	SourceInstance       string                    `json:"source_instance"`
	OrgID                string                    `json:"org_id"`
	ProjectID            string                    `json:"project_id"`
	ConnectionStatus     string                    `json:"connection_status"`
	ContentStatus        string                    `json:"content_status"`
	StateVersion         string                    `json:"state_version"`
	ContentScopeRevision string                    `json:"content_scope_revision"`
	Active               *ContentControlCredential `json:"active,omitempty"`
	Staged               *ContentControlCredential `json:"staged,omitempty"`
	ContentAccess        *ContentAccess            `json:"content_access,omitempty"`
}

func DecodeContentControlGET(raw []byte) (ContentControlGET, error) {
	var g ContentControlGET
	m, err := contentControlObject(raw, &g, []string{"schema_version", "source_instance", "org_id", "project_id", "connection_status", "content_status", "state_version", "content_scope_revision"}, []string{"active", "staged", "content_access"})
	if err != nil || g.SchemaVersion != ContentControlVersion || !boundedText(g.SourceInstance, 128) || !Positive(g.OrgID) || !Positive(g.ProjectID) || !contentControlDecimal(g.StateVersion) || !contentControlDecimal(g.ContentScopeRevision) || !contentControlStatus(g.ConnectionStatus) || !contentControlStatus(g.ContentStatus) {
		return g, ErrInvalid
	}
	for _, field := range []string{"active", "staged"} {
		if v, ok := m[field]; ok {
			raw, _ := json.Marshal(v)
			var b ContentControlCredential
			if _, err := contentControlObject(raw, &b, []string{"credential_id", "registration_hash"}, nil); err != nil || !Positive(b.CredentialID) || !hashPattern.MatchString(b.RegistrationHash) {
				return g, ErrInvalid
			}
		}
	}
	if g.Active != nil && g.Staged != nil && (g.Active.CredentialID == g.Staged.CredentialID || g.Active.RegistrationHash == g.Staged.RegistrationHash) {
		return g, ErrInvalid
	}
	if g.ConnectionStatus == "revoked" || g.ContentStatus == "revoked" || g.ContentStatus == "not_configured" || g.ConnectionStatus == "not_configured" {
		if g.Active != nil || g.Staged != nil || g.ContentAccess != nil {
			return g, ErrInvalid
		}
		return g, nil
	}
	binding := g.Active
	if binding == nil {
		binding = g.Staged
	}
	if binding == nil {
		if g.ContentAccess != nil {
			return g, ErrInvalid
		}
		return g, nil
	}
	accessRaw, _ := json.Marshal(m["content_access"])
	access, err := DecodeContentAccess(accessRaw)
	if err != nil || access.RegistrationHash != binding.RegistrationHash || access.ContentScopeRevision != g.ContentScopeRevision || access.CoverageFrom == nil {
		return g, ErrInvalid
	}
	if access.Enabled && (g.Active == nil || g.ConnectionStatus != "active" || g.ContentStatus != "active") {
		return g, ErrInvalid
	}
	if !access.Enabled && len(access.AllowedPurposes) != 0 {
		return g, ErrInvalid
	}
	g.ContentAccess = &access
	return g, nil
}

// Receipts report one operation. They have no method that admits a current read.
type ContentControlReceipt struct {
	Action               string           `json:"action"`
	OperationID          string           `json:"operation_id"`
	StateVersion         string           `json:"state_version"`
	ContentScopeRevision string           `json:"content_scope_revision"`
	CredentialID         string           `json:"credential_id,omitempty"`
	RegistrationHash     string           `json:"registration_hash,omitempty"`
	AllowedPurposes      []string         `json:"allowed_purposes,omitempty"`
	CoverageFrom         *ContentBoundary `json:"coverage_from,omitempty"`
}

func DecodeContentControlReceipt(raw []byte) (ContentControlReceipt, error) {
	var r ContentControlReceipt
	m, err := contentControlObject(raw, &r, []string{"action", "operation_id", "state_version", "content_scope_revision"}, []string{"credential_id", "registration_hash", "allowed_purposes", "coverage_from"})
	if err != nil || !contentControlOperation.MatchString(r.OperationID) || !contentControlDecimal(r.StateVersion) || !contentControlDecimal(r.ContentScopeRevision) {
		return r, ErrInvalid
	}
	switch r.Action {
	case "prepare", "activate", "pause", "resume", "rotate", "cancel", "revoke":
	default:
		return r, ErrInvalid
	}
	if len(m) == 4 {
		if r.Action != "revoke" {
			return r, ErrInvalid
		}
		return r, nil
	}
	if len(m) != 8 || !Positive(r.CredentialID) || !hashPattern.MatchString(r.RegistrationHash) || !reflect.DeepEqual(r.AllowedPurposes, []string{ContentPurposeHumanReview}) || r.CoverageFrom == nil {
		return r, ErrInvalid
	}
	boundaryRaw, _ := json.Marshal(m["coverage_from"])
	if _, err := contentControlObject(boundaryRaw, &ContentBoundary{}, []string{"time", "after_sequence"}, nil); err != nil || !ValidTime(r.CoverageFrom.Time) || !contentControlDecimal(r.CoverageFrom.AfterSequence) {
		return r, ErrInvalid
	}
	return r, nil
}

// MatchReceipt binds a historical result to the original immutable request.
// Versions may be older than the latest GET; never synthesize a new CAS here.
func (r ContentControlRequest) MatchReceipt(raw []byte) (ContentControlReceipt, error) {
	receipt, err := DecodeContentControlReceipt(raw)
	if err != nil || len(r.raw) == 0 || receipt.Action != r.action || receipt.OperationID != r.value.OperationID {
		return receipt, ErrConflict
	}
	expected, _ := strconv.ParseInt(r.value.ExpectedStateVersion, 10, 64)
	actual, _ := strconv.ParseInt(receipt.StateVersion, 10, 64)
	if actual <= expected {
		return receipt, ErrConflict
	}
	if receipt.CredentialID == "" {
		if r.action != "revoke" {
			return receipt, ErrConflict
		}
		return receipt, nil
	}
	if r.value.Registration != nil {
		_, hash, _ := r.value.Registration.MarshalRegistration()
		if receipt.RegistrationHash != hash {
			return receipt, ErrConflict
		}
	} else if receipt.CredentialID != r.value.CredentialID || receipt.RegistrationHash != r.value.RegistrationHash {
		return receipt, ErrConflict
	}
	return receipt, nil
}

func contentControlObject(raw []byte, out any, required, optional []string) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > MaxDocumentBytes {
		return nil, ErrInvalid
	}
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil || !validContentFields(m, "") {
		return nil, ErrInvalid
	}
	allowed := map[string]bool{}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			return nil, ErrInvalid
		}
		allowed[k] = true
	}
	for _, k := range optional {
		allowed[k] = true
	}
	for k := range m {
		if !allowed[k] {
			return nil, ErrInvalid
		}
	}
	if json.Unmarshal(raw, out) != nil {
		return nil, ErrInvalid
	}
	return m, nil
}
func contentControlDecimal(s string) bool {
	n, e := strconv.ParseInt(s, 10, 64)
	return e == nil && n >= 0 && strconv.FormatInt(n, 10) == s
}
func contentControlStatus(s string) bool {
	return s == "active" || s == "paused" || s == "revoked" || s == "not_configured"
}
