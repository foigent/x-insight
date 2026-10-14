package access

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/config"
)

const (
	AccountConfigSchema   = "xinsight.local-accounts.v1"
	AccountConfigSchemaV2 = "xinsight.local-accounts.v2"
	AccountConfigSchemaV3 = "xinsight.local-accounts.v3"
	AccountCredential     = "access-accounts.json"
	MaxAccountFileBytes   = 64 << 10
	MaxAccounts           = 100
)

type Role string

const (
	RoleInvestigator        Role = "investigator"
	RoleOperator            Role = "operator"
	RoleAccessOperator      Role = "access_operator"
	RolePlatformAdmin       Role = "platform_admin"
	RoleWorkbenchReader     Role = "workbench_reader"
	RoleInvestigationReader Role = "investigation_reader"
	RoleScopedReader        Role = "workbench_investigation_reader"
	RoleScopedReviewer      Role = "workbench_investigation_reviewer"
	RolePurposeReviewer     Role = "workbench_purpose_reviewer"
)

type Permission string

const PermissionPurposeReview Permission = "workbench.purpose.review"

const (
	PermissionEvidenceRead                 Permission = "evidence.read"
	PermissionScopedRead                   Permission = "investigation.scoped.read"
	PermissionScopedCite                   Permission = "investigation.scoped.cite"
	PermissionScopedWrite                  Permission = "investigation.scoped.write"
	PermissionEvidenceCite                 Permission = "evidence.cite"
	PermissionInvestigationWrite           Permission = "investigation.write"
	PermissionInvestigationPreferences     Permission = "investigation.preferences.write"
	PermissionAccessSessionsRead           Permission = "access.sessions.read"
	PermissionAccessSessionsRevoke         Permission = "access.sessions.revoke"
	PermissionServiceIdentitiesRead        Permission = "service_identities.read"
	PermissionServiceIdentitiesRevoke      Permission = "service_identities.revoke"
	PermissionConnectorInstallationsManage Permission = "connector.installations.manage"
	PermissionReviewPolicyManage           Permission = "review.policy.manage"
	PermissionWorkbenchSummary             Permission = "workbench.summary.read"
	PermissionWorkbenchDetails             Permission = "workbench.details.read"
	PermissionWorkbenchCoverage            Permission = "workbench.coverage.read"
	PermissionWorkbenchPreferences         Permission = "workbench.preferences.write"
)

var (
	subjectIDPattern = regexp.MustCompile(`^xs_[a-z0-9]{16,64}$`)
	usernamePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)
)

type AccountConfig struct {
	SchemaVersion string               `json:"schema_version"`
	Accounts      []AccountConfigEntry `json:"accounts"`
}

type AccountConfigEntry struct {
	SubjectID    string `json:"subject_id"`
	Username     string `json:"username"`
	Role         Role   `json:"role"`
	Disabled     bool   `json:"disabled"`
	PasswordHash string `json:"password_hash"`
}

type accountConfigWire struct {
	SchemaVersion string                   `json:"schema_version"`
	Accounts      []accountConfigEntryWire `json:"accounts"`
}

type accountConfigEntryWire struct {
	SubjectID    string `json:"subject_id"`
	Username     string `json:"username"`
	Role         Role   `json:"role"`
	Disabled     *bool  `json:"disabled"`
	PasswordHash string `json:"password_hash"`
}

type Principal struct {
	SubjectID string
	Role      Role
}

type credentialAccount struct {
	principal Principal
	disabled  bool
	hash      PasswordHash
}

type LocalProvider struct {
	accounts map[string]credentialAccount
	dummy    PasswordHash
}

func ParseAccountConfig(contents []byte) (AccountConfig, error) {
	if len(contents) == 0 || len(contents) > MaxAccountFileBytes {
		return AccountConfig{}, errors.New("access account configuration size is invalid")
	}
	if err := rejectDuplicateJSONKeys(contents); err != nil {
		return AccountConfig{}, err
	}
	var wire accountConfigWire
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return AccountConfig{}, errors.New("access account configuration is invalid")
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return AccountConfig{}, err
	}
	config := AccountConfig{
		SchemaVersion: wire.SchemaVersion,
		Accounts:      make([]AccountConfigEntry, 0, len(wire.Accounts)),
	}
	for _, entry := range wire.Accounts {
		if entry.Disabled == nil {
			return AccountConfig{}, errors.New("access account configuration entry is invalid")
		}
		config.Accounts = append(config.Accounts, AccountConfigEntry{
			SubjectID:    entry.SubjectID,
			Username:     entry.Username,
			Role:         entry.Role,
			Disabled:     *entry.Disabled,
			PasswordHash: entry.PasswordHash,
		})
	}
	if (config.SchemaVersion != AccountConfigSchema && config.SchemaVersion != AccountConfigSchemaV2 && config.SchemaVersion != AccountConfigSchemaV3) || len(config.Accounts) == 0 || len(config.Accounts) > MaxAccounts {
		return AccountConfig{}, errors.New("access account configuration identity or account count is invalid")
	}
	subjects := make(map[string]struct{}, len(config.Accounts))
	usernames := make(map[string]struct{}, len(config.Accounts))
	for _, account := range config.Accounts {
		normalized := NormalizeUsername(account.Username)
		if !subjectIDPattern.MatchString(account.SubjectID) ||
			account.Username != normalized ||
			!usernamePattern.MatchString(normalized) ||
			!roleSupportedByConfig(account.Role, config.SchemaVersion) {
			return AccountConfig{}, errors.New("access account configuration entry is invalid")
		}
		if _, exists := subjects[account.SubjectID]; exists {
			return AccountConfig{}, errors.New("access account subject IDs must be unique")
		}
		if _, exists := usernames[normalized]; exists {
			return AccountConfig{}, errors.New("access account normalized usernames must be unique")
		}
		if _, err := ParsePasswordHash(account.PasswordHash); err != nil {
			return AccountConfig{}, errors.New("access account password hash is invalid")
		}
		subjects[account.SubjectID] = struct{}{}
		usernames[normalized] = struct{}{}
	}
	return config, nil
}

func NewLocalProvider(config AccountConfig) (*LocalProvider, error) {
	encodedDummy, err := DerivePasswordHash([]byte("xinsight-dummy-password"))
	if err != nil {
		return nil, errors.New("access provider dummy hash generation failed")
	}
	dummy, err := ParsePasswordHash(encodedDummy)
	if err != nil {
		return nil, errors.New("access provider dummy hash parsing failed")
	}
	provider := &LocalProvider{
		accounts: make(map[string]credentialAccount, len(config.Accounts)),
		dummy:    dummy,
	}
	for _, entry := range config.Accounts {
		hash, err := ParsePasswordHash(entry.PasswordHash)
		if err != nil {
			return nil, errors.New("access provider account hash parsing failed")
		}
		provider.accounts[entry.Username] = credentialAccount{
			principal: Principal{SubjectID: entry.SubjectID, Role: entry.Role},
			disabled:  entry.Disabled,
			hash:      hash,
		}
	}
	return provider, nil
}

func LoadLocalProvider(auditor Auditor) (*LocalProvider, error) {
	now := time.Now().UTC()
	directory := strings.TrimSpace(os.Getenv("CREDENTIALS_DIRECTORY"))
	if directory == "" || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		audit(auditor, now, "access.config_reject", nil, "", "credential_directory_invalid")
		return nil, errors.New("access account systemd credential is required")
	}
	path := filepath.Join(directory, AccountCredential)
	contents, err := readAccountFileWithPolicy(path, nil, true)
	if err != nil {
		audit(auditor, now, "access.config_reject", nil, "", "credential_file_invalid")
		return nil, err
	}
	config, err := ParseAccountConfig(contents)
	if err != nil {
		audit(auditor, now, "access.config_reject", nil, "", "configuration_invalid")
		return nil, err
	}
	provider, err := NewLocalProvider(config)
	if err != nil {
		audit(auditor, now, "access.config_reject", nil, "", "provider_initialization_failed")
		return nil, err
	}
	audit(auditor, now, "access.config_load", nil, "", "accepted")
	return provider, nil
}

func ReadAndValidateAccountFile(path string) (AccountConfig, error) {
	contents, err := readRestrictedAccountFile(path)
	if err != nil {
		return AccountConfig{}, err
	}
	return ParseAccountConfig(contents)
}

func (provider *LocalProvider) Authenticate(username string, password []byte) (Principal, bool) {
	normalized := NormalizeUsername(username)
	account, exists := provider.accounts[normalized]
	hash := provider.dummy
	if exists {
		hash = account.hash
	}
	matches := hash.Verify(password)
	if !exists || account.disabled || !matches {
		return Principal{}, false
	}
	return account.principal, true
}

func NormalizeUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func Permissions(role Role) []Permission {
	switch role {
	case RoleInvestigator:
		return []Permission{
			PermissionEvidenceRead,
			PermissionInvestigationPreferences,
			PermissionEvidenceCite,
			PermissionInvestigationWrite,
		}
	case RoleOperator:
		return []Permission{
			PermissionEvidenceRead,
			PermissionInvestigationPreferences,
			PermissionEvidenceCite,
			PermissionInvestigationWrite,
			PermissionAccessSessionsRead,
			PermissionAccessSessionsRevoke,
			PermissionConnectorInstallationsManage,
		}
	case RoleAccessOperator:
		return []Permission{
			PermissionEvidenceRead,
			PermissionInvestigationPreferences,
			PermissionEvidenceCite,
			PermissionInvestigationWrite,
			PermissionAccessSessionsRead,
			PermissionAccessSessionsRevoke,
		}
	case RolePlatformAdmin:
		return []Permission{
			PermissionEvidenceRead,
			PermissionInvestigationPreferences,
			PermissionEvidenceCite,
			PermissionInvestigationWrite,
			PermissionAccessSessionsRead,
			PermissionAccessSessionsRevoke,
			PermissionServiceIdentitiesRead,
			PermissionServiceIdentitiesRevoke,
			PermissionConnectorInstallationsManage,
			PermissionReviewPolicyManage,
		}
	case RoleInvestigationReader:
		return []Permission{PermissionEvidenceRead, PermissionInvestigationPreferences}
	case RoleScopedReader, RoleScopedReviewer, RolePurposeReviewer:
		permissions := []Permission{PermissionWorkbenchSummary, PermissionWorkbenchDetails, PermissionWorkbenchCoverage, PermissionWorkbenchPreferences, PermissionScopedRead, PermissionInvestigationPreferences}
		if role == RolePurposeReviewer {
			permissions = append(permissions, PermissionPurposeReview)
		}
		if role == RoleScopedReviewer {
			permissions = append(permissions, PermissionScopedCite, PermissionScopedWrite)
		}
		return permissions
	case RoleWorkbenchReader:
		return []Permission{
			PermissionWorkbenchSummary,
			PermissionWorkbenchDetails,
			PermissionWorkbenchCoverage,
			PermissionWorkbenchPreferences,
		}
	default:
		return []Permission{}
	}
}

func HasPermission(role Role, permission Permission) bool {
	for _, candidate := range Permissions(role) {
		if candidate == permission {
			return true
		}
	}
	return false
}

func readRestrictedAccountFile(path string) ([]byte, error) {
	return readRestrictedAccountFileWithHook(path, nil)
}

func readRestrictedAccountFileWithHook(path string, afterOpen func()) ([]byte, error) {
	return readAccountFileWithPolicy(path, afterOpen, false)
}

func readAccountFileWithPolicy(path string, afterOpen func(), runtimeCopy bool) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("access account file path must be absolute and clean")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return nil, errors.New("access account file cannot be inspected")
	}
	validate := func(info os.FileInfo, file *os.File) error {
		err := validateAccountFileMetadata(info)
		if err != nil && runtimeCopy && config.ValidAccessAccountSystemdCredential(path, info, file) {
			return nil
		}
		return err
	}
	if err := validate(before, nil); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("access account file cannot be opened")
	}
	defer file.Close()
	if afterOpen != nil {
		afterOpen()
	}
	after, err := file.Stat()
	currentPath, currentPathErr := os.Lstat(path)
	if err != nil ||
		currentPathErr != nil ||
		!os.SameFile(before, after) ||
		!os.SameFile(currentPath, after) ||
		validate(after, file) != nil ||
		validate(currentPath, nil) != nil {
		return nil, errors.New("access account file changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, MaxAccountFileBytes+1))
	if err != nil || len(contents) > MaxAccountFileBytes {
		return nil, errors.New("access account file cannot be read")
	}
	return contents, nil
}

func validateAccountFileMetadata(info os.FileInfo) error {
	permissions := info.Mode().Perm()
	if permissions != 0o400 && permissions != 0o600 {
		return errors.New("access account file permissions must be 0400 or 0600")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok ||
		(stat.Uid != 0 && int(stat.Uid) != os.Geteuid()) ||
		stat.Nlink != 1 {
		return errors.New("access account file ownership or link count is invalid")
	}
	return nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return fmt.Errorf("access account configuration has trailing content")
}

func rejectDuplicateJSONKeys(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := validateUniqueJSONValue(decoder); err != nil {
		return errors.New("access account configuration contains duplicate or invalid JSON fields")
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return err
	}
	return nil
}

func validateUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		fields := make(map[string]struct{})
		for decoder.More() {
			rawName, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := rawName.(string)
			if !ok {
				return errors.New("JSON object field name is not a string")
			}
			if _, exists := fields[name]; exists {
				return errors.New("duplicate JSON object field")
			}
			fields[name] = struct{}{}
			if err := validateUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("JSON object is not terminated")
		}
	case '[':
		for decoder.More() {
			if err := validateUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("JSON array is not terminated")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

func IsScopedRole(role Role) bool {
	return role == RoleScopedReader || role == RoleScopedReviewer || role == RolePurposeReviewer
}

func roleSupportedByConfig(role Role, schema string) bool {
	switch role {
	case RoleInvestigator, RoleOperator, RoleAccessOperator, RolePlatformAdmin:
		return true
	case RoleWorkbenchReader, RoleInvestigationReader:
		return schema == AccountConfigSchemaV2 || schema == AccountConfigSchemaV3
	case RoleScopedReader, RoleScopedReviewer, RolePurposeReviewer:
		return schema == AccountConfigSchemaV3
	default:
		return false
	}
}
