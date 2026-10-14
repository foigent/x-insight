package access

import "sort"

// LocalSubject is the complete non-secret directory projection. It is never an
// AccountConfigEntry and carries neither credentials, roles, nor session data.
type LocalSubject struct {
	SubjectID   string `json:"subject_id"`
	DisplayName string `json:"display_name"`
	State       string `json:"state"`
}

func (provider *LocalProvider) LocalSubjects() []LocalSubject {
	items := make([]LocalSubject, 0, len(provider.accounts))
	for name, account := range provider.accounts {
		state := "active"
		if account.disabled {
			state = "disabled"
		}
		items = append(items, LocalSubject{SubjectID: account.principal.SubjectID, DisplayName: name, State: state})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].SubjectID < items[j].SubjectID })
	return items
}

// The service exposes only the already-loaded provider snapshot. It does not
// reload credentials or invent a directory for another identity provider.
func (service *Service) LocalSubjects() ([]LocalSubject, bool) {
	provider, ok := service.provider.(*LocalProvider)
	if !ok {
		return nil, false
	}
	items := provider.LocalSubjects()
	if len(items) > MaxAccounts {
		return nil, false
	}
	return items, true
}

func (service *Service) LocalSubjectCanReview(subject string) bool {
	provider, ok := service.provider.(*LocalProvider)
	if !ok {
		return false
	}
	for _, account := range provider.accounts {
		if account.principal.SubjectID == subject {
			return !account.disabled && (HasPermission(account.principal.Role, PermissionInvestigationWrite) || account.principal.Role == RoleScopedReviewer)
		}
	}
	return false
}

func (service *Service) LocalSubjectHasGlobalReview(subject string) bool {
	provider, ok := service.provider.(*LocalProvider)
	if !ok {
		return false
	}
	for _, account := range provider.accounts {
		if account.principal.SubjectID == subject {
			return !account.disabled && HasPermission(account.principal.Role, PermissionEvidenceRead) && HasPermission(account.principal.Role, PermissionInvestigationWrite)
		}
	}
	return false
}
