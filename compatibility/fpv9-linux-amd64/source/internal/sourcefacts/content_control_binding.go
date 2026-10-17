package sourcefacts

import "reflect"

// ContentControlCandidate is a GET-confirmed public binding, not permission to
// fetch bytes. The independent content capabilities authority must still match.
// No state_version is ever substituted for recovery_epoch.
type ContentControlCandidate struct {
	expected     ContentCollection
	stateVersion string
}

func (c ContentControlCandidate) StateVersion() string { return c.stateVersion }

func ValidateContentControlBinding(raw []byte, registration SourceCredentialRegistration, managed ManagedContentConfig) (ContentControlCandidate, error) {
	var candidate ContentControlCandidate
	_, hash, err := registration.MarshalRegistration()
	if err != nil || managed.Validate() != nil {
		return candidate, ErrInvalid
	}
	if hash != managed.RegistrationHash || registration.SourceInstance != managed.SourceInstance || registration.OrgID != managed.OrgID || registration.ProjectID != managed.ProjectID {
		return candidate, ErrScopeChanged
	}
	g, err := DecodeContentControlGET(raw)
	if err != nil {
		return candidate, err
	}
	if g.SourceInstance != managed.SourceInstance || g.OrgID != managed.OrgID || g.ProjectID != managed.ProjectID {
		return candidate, ErrScopeChanged
	}
	if g.Active == nil || g.ContentAccess == nil || !g.ContentAccess.Enabled || g.ConnectionStatus != "active" || g.ContentStatus != "active" || !reflect.DeepEqual(g.ContentAccess.AllowedPurposes, []string{ContentPurposeHumanReview}) {
		return candidate, ErrAuthorization
	}
	if g.Active.CredentialID != managed.CredentialID || g.Active.RegistrationHash != hash || g.ContentScopeRevision != managed.ContentScopeRevision || g.ContentAccess.CoverageFrom == nil || *g.ContentAccess.CoverageFrom != managed.CoverageFrom {
		return candidate, ErrScopeChanged
	}
	candidate.expected = managed.Registration()
	candidate.expected.AllowedPurposes = append([]string(nil), managed.AllowedPurposes...)
	candidate.stateVersion = g.StateVersion
	return candidate, nil
}

// ValidateCapabilities consumes an independently obtained capabilities document.
// An offline match describes supplied snapshots only, not current live state.
func (c ContentControlCandidate) ValidateCapabilities(raw []byte) (ContentCapabilities, error) {
	if c.stateVersion == "" || !ValidContentCollection(c.expected) {
		return ContentCapabilities{}, ErrInvalid
	}
	caps, err := DecodeContentCapabilities(raw)
	if err != nil {
		return caps, err
	}
	e := c.expected
	if caps.SourceInstance != e.SourceInstance || caps.OrgID != e.OrgID || caps.ProjectID != e.ProjectID || caps.ContentScopeRevision != e.ContentScopeRevision || caps.RecoveryEpoch != e.RecoveryEpoch || caps.CoverageFrom != e.CoverageFrom || !reflect.DeepEqual(caps.AllowedPurposes, e.AllowedPurposes) {
		return caps, ErrScopeChanged
	}
	if caps.Status != "active" || caps.CredentialStatus != "feed_active" {
		return caps, ErrUnavailable
	}
	return caps, nil
}
