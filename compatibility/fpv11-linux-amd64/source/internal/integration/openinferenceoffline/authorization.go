package openinferenceoffline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	AuthorizationSchemaVersion = "xinsight.openinference-bounded-offline-authorization.v1"
	maxAuthorizationBytes      = 64 << 10
	maxAuthorizationLifetime   = 2 * time.Hour
	productionAuthorizationUID = uint32(0)
	machineIDPath              = "/etc/machine-id"
)

var (
	authorizationIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)
	commitPattern          = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern          = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	machineIDPattern       = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type BoundedOfflineAuthorization struct {
	SchemaVersion   string                       `json:"schema_version"`
	AuthorizationID string                       `json:"authorization_id"`
	Environment     string                       `json:"environment"`
	Purpose         string                       `json:"purpose"`
	NotBefore       string                       `json:"not_before"`
	ExpiresAt       string                       `json:"expires_at"`
	Target          AuthorizationTarget          `json:"target"`
	Product         AuthorizationProduct         `json:"product"`
	Import          AuthorizationImport          `json:"import"`
	DataPolicy      AuthorizationDataPolicy      `json:"data_policy"`
	RuntimeBoundary AuthorizationRuntimeBoundary `json:"runtime_boundary"`
}

type AuthorizationTarget struct {
	TargetRef       string `json:"target_ref"`
	MachineIDSHA256 string `json:"machine_id_sha256"`
}

type AuthorizationProduct struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type AuthorizationImport struct {
	BundlePath       string  `json:"bundle_path"`
	BundleSHA256     string  `json:"bundle_sha256"`
	BundleID         string  `json:"bundle_id"`
	OperationID      string  `json:"operation_id"`
	ProducerInstance string  `json:"producer_instance"`
	TenantRef        *string `json:"tenant_ref,omitempty"`
	OrgRef           string  `json:"org_ref"`
	ProjectRef       string  `json:"project_ref"`
	MaxRecords       int     `json:"max_records"`
}

type AuthorizationDataPolicy struct {
	PayloadCaptureMode string `json:"payload_capture_mode"`
	RawPayloadPresent  bool   `json:"raw_payload_present"`
}

type AuthorizationRuntimeBoundary struct {
	DeliveryMode     string `json:"delivery_mode"`
	NetworkListener  bool   `json:"network_listener"`
	FeedActivation   bool   `json:"feed_activation"`
	CursorCheckpoint bool   `json:"cursor_checkpoint"`
}

type AuthorizedImportRequest struct {
	TargetRef        string
	BundlePath       string
	BundleSHA256     string
	BundleID         string
	OperationID      string
	ProducerInstance string
	TenantRef        *string
	OrgRef           string
	ProjectRef       string
	RecordCount      int
}

// LoadBoundedOfflineAuthorization loads one exact root-owned, expiring
// authorization. It authorizes only a bounded-offline import and never enables
// a listener, feed, Cursor or Checkpoint.
func LoadBoundedOfflineAuthorization(
	path string,
	targetRef string,
	productVersion string,
	productCommit string,
	now time.Time,
) (BoundedOfflineAuthorization, string, error) {
	authorization, digest, err := readAndParseAuthorization(path, productionAuthorizationUID)
	if err != nil {
		return BoundedOfflineAuthorization{}, "", err
	}
	machineDigest, err := readMachineIDDigest(machineIDPath)
	if err != nil {
		return BoundedOfflineAuthorization{}, "", err
	}
	if err := authorization.validateActivation(
		targetRef,
		machineDigest,
		productVersion,
		productCommit,
		now,
	); err != nil {
		return BoundedOfflineAuthorization{}, "", err
	}
	return authorization, digest, nil
}

func loadBoundedOfflineAuthorization(
	path string,
	requiredUID uint32,
	targetRef string,
	machineDigest string,
	productVersion string,
	productCommit string,
	now time.Time,
) (BoundedOfflineAuthorization, string, error) {
	authorization, digest, err := readAndParseAuthorization(path, requiredUID)
	if err != nil {
		return BoundedOfflineAuthorization{}, "", err
	}
	if err := authorization.validateActivation(
		targetRef,
		machineDigest,
		productVersion,
		productCommit,
		now,
	); err != nil {
		return BoundedOfflineAuthorization{}, "", err
	}
	return authorization, digest, nil
}

func readAndParseAuthorization(path string, requiredUID uint32) (BoundedOfflineAuthorization, string, error) {
	payload, err := readStableAuthorizationFile(path, requiredUID)
	if err != nil {
		return BoundedOfflineAuthorization{}, "", err
	}
	authorization, err := parseBoundedOfflineAuthorization(payload)
	if err != nil {
		return BoundedOfflineAuthorization{}, "", err
	}
	digest := sha256.Sum256(payload)
	return authorization, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (authorization BoundedOfflineAuthorization) ValidateImport(request AuthorizedImportRequest) error {
	if err := authorization.ValidateImportBinding(request); err != nil {
		return err
	}
	if authorization.Import.BundleSHA256 != request.BundleSHA256 ||
		request.RecordCount < 1 ||
		request.RecordCount > authorization.Import.MaxRecords {
		return errors.New("OpenInference bundle does not match the exact bounded-offline authorization")
	}
	return nil
}

func (authorization BoundedOfflineAuthorization) ValidateImportBinding(request AuthorizedImportRequest) error {
	if authorization.Target.TargetRef != request.TargetRef ||
		authorization.Import.BundlePath != request.BundlePath ||
		authorization.Import.BundleID != request.BundleID ||
		authorization.Import.OperationID != request.OperationID ||
		authorization.Import.ProducerInstance != request.ProducerInstance ||
		!sameOptionalString(authorization.Import.TenantRef, request.TenantRef) ||
		authorization.Import.OrgRef != request.OrgRef ||
		authorization.Import.ProjectRef != request.ProjectRef {
		return errors.New("OpenInference import does not match the exact bounded-offline authorization")
	}
	return nil
}

func (authorization BoundedOfflineAuthorization) validateActivation(
	targetRef string,
	machineDigest string,
	productVersion string,
	productCommit string,
	now time.Time,
) error {
	if authorization.SchemaVersion != AuthorizationSchemaVersion ||
		!authorizationIDPattern.MatchString(authorization.AuthorizationID) ||
		authorization.Environment != "nonproduction" ||
		authorization.Purpose != "openinference_trace_study" {
		return errors.New("OpenInference authorization identity or purpose is invalid")
	}
	notBefore, err := parseAuthorizationTime(authorization.NotBefore)
	if err != nil {
		return err
	}
	expiresAt, err := parseAuthorizationTime(authorization.ExpiresAt)
	if err != nil {
		return err
	}
	now = now.UTC()
	if !expiresAt.After(notBefore) || expiresAt.Sub(notBefore) > maxAuthorizationLifetime ||
		now.Before(notBefore) || !now.Before(expiresAt) {
		return errors.New("OpenInference authorization window is invalid or inactive")
	}
	if invalidBoundedString(authorization.Target.TargetRef, 256) ||
		authorization.Target.TargetRef != targetRef ||
		!sha256Pattern.MatchString(authorization.Target.MachineIDSHA256) ||
		authorization.Target.MachineIDSHA256 != machineDigest {
		return errors.New("OpenInference authorization target identity does not match this host")
	}
	if invalidBoundedString(authorization.Product.Version, 128) ||
		authorization.Product.Version != productVersion ||
		!commitPattern.MatchString(authorization.Product.Commit) ||
		authorization.Product.Commit != productCommit {
		return errors.New("OpenInference authorization product identity does not match this build")
	}
	if invalidBoundedString(authorization.Import.BundlePath, 4096) ||
		!filepath.IsAbs(authorization.Import.BundlePath) ||
		filepath.Clean(authorization.Import.BundlePath) != authorization.Import.BundlePath ||
		!sha256Pattern.MatchString(authorization.Import.BundleSHA256) ||
		invalidBoundedString(authorization.Import.BundleID, 256) ||
		invalidBoundedString(authorization.Import.OperationID, 256) ||
		invalidBoundedString(authorization.Import.ProducerInstance, 256) ||
		invalidOptionalBoundedString(authorization.Import.TenantRef, 256) ||
		invalidBoundedString(authorization.Import.OrgRef, 256) ||
		invalidBoundedString(authorization.Import.ProjectRef, 256) ||
		authorization.Import.MaxRecords < 1 || authorization.Import.MaxRecords > 10 {
		return errors.New("OpenInference authorization import scope is invalid")
	}
	if authorization.DataPolicy.PayloadCaptureMode != "metadata_only" ||
		authorization.DataPolicy.RawPayloadPresent ||
		authorization.RuntimeBoundary.DeliveryMode != "bounded_offline" ||
		authorization.RuntimeBoundary.NetworkListener ||
		authorization.RuntimeBoundary.FeedActivation ||
		authorization.RuntimeBoundary.CursorCheckpoint {
		return errors.New("OpenInference authorization expands the metadata-only bounded-offline boundary")
	}
	return nil
}

func parseAuthorizationTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || !strings.HasSuffix(value, "Z") || parsed.Nanosecond()%1000 != 0 ||
		parsed.UTC().Format(time.RFC3339Nano) != value {
		return time.Time{}, errors.New("OpenInference authorization time must be canonical UTC with at most microsecond precision")
	}
	return parsed.UTC(), nil
}

func invalidBoundedString(value string, maximum int) bool {
	return value == "" || value != strings.TrimSpace(value) || len(value) > maximum || strings.ContainsRune(value, '\x00')
}

func invalidOptionalBoundedString(value *string, maximum int) bool {
	return value != nil && invalidBoundedString(*value, maximum)
}

func sameOptionalString(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func parseBoundedOfflineAuthorization(payload []byte) (BoundedOfflineAuthorization, error) {
	if len(payload) == 0 || len(payload) > maxAuthorizationBytes {
		return BoundedOfflineAuthorization{}, errors.New("OpenInference authorization bytes are invalid")
	}
	if err := rejectAuthorizationDuplicateKeys(payload); err != nil {
		return BoundedOfflineAuthorization{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var authorization BoundedOfflineAuthorization
	if err := decoder.Decode(&authorization); err != nil {
		return BoundedOfflineAuthorization{}, errors.New("OpenInference authorization JSON is invalid")
	}
	if err := ensureAuthorizationJSONEOF(decoder); err != nil {
		return BoundedOfflineAuthorization{}, err
	}
	if err := requireExplicitAuthorizationBoundaryFields(payload); err != nil {
		return BoundedOfflineAuthorization{}, err
	}
	return authorization, nil
}

func requireExplicitAuthorizationBoundaryFields(payload []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(payload, &root); err != nil {
		return errors.New("OpenInference authorization JSON is invalid")
	}
	for objectName, fields := range map[string][]string{
		"data_policy":      {"raw_payload_present"},
		"runtime_boundary": {"network_listener", "feed_activation", "cursor_checkpoint"},
	} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(root[objectName], &object); err != nil {
			return errors.New("OpenInference authorization boundary object is missing or invalid")
		}
		for _, field := range fields {
			if _, present := object[field]; !present {
				return errors.New("OpenInference authorization must explicitly state every false boundary field")
			}
		}
	}
	return nil
}

func readStableAuthorizationFile(path string, requiredUID uint32) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("--authorization-file must be an absolute clean path")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("OpenInference authorization file cannot be inspected")
	}
	if err := validateAuthorizationFileMetadata(before, requiredUID); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.New("OpenInference authorization file cannot be opened")
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, errors.New("OpenInference authorization file descriptor is unavailable")
	}
	defer file.Close()
	opened, err := file.Stat()
	pathInfo, pathErr := os.Lstat(path)
	if err != nil || pathErr != nil || !os.SameFile(before, opened) || !os.SameFile(opened, pathInfo) ||
		validateAuthorizationFileMetadata(opened, requiredUID) != nil ||
		validateAuthorizationFileMetadata(pathInfo, requiredUID) != nil {
		return nil, errors.New("OpenInference authorization file changed while opening")
	}
	payload, err := io.ReadAll(io.LimitReader(file, maxAuthorizationBytes+1))
	if err != nil || len(payload) == 0 || len(payload) > maxAuthorizationBytes || int64(len(payload)) != opened.Size() {
		return nil, errors.New("OpenInference authorization file cannot be read")
	}
	firstDigest := sha256.Sum256(payload)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, errors.New("OpenInference authorization file cannot be verified")
	}
	verification, err := io.ReadAll(io.LimitReader(file, maxAuthorizationBytes+1))
	if err != nil || !bytes.Equal(payload, verification) {
		return nil, errors.New("OpenInference authorization file changed while it was read")
	}
	secondDigest := sha256.Sum256(verification)
	after, err := file.Stat()
	pathAfter, pathAfterErr := os.Lstat(path)
	if err != nil || pathAfterErr != nil || firstDigest != secondDigest ||
		!os.SameFile(opened, after) || !os.SameFile(after, pathAfter) ||
		validateAuthorizationFileMetadata(after, requiredUID) != nil ||
		validateAuthorizationFileMetadata(pathAfter, requiredUID) != nil ||
		opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return nil, errors.New("OpenInference authorization file changed while it was read")
	}
	return payload, nil
}

func validateAuthorizationFileMetadata(info os.FileInfo, requiredUID uint32) error {
	if info == nil || !info.Mode().IsRegular() ||
		(info.Mode().Perm() != 0o400 && info.Mode().Perm() != 0o600) ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return errors.New("OpenInference authorization file must be one regular 0400 or 0600 file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != requiredUID || stat.Nlink != 1 {
		return errors.New("OpenInference authorization file ownership or link count is invalid")
	}
	return nil
}

func readMachineIDDigest(path string) (string, error) {
	payload, err := os.ReadFile(path)
	if err != nil || len(payload) > 128 {
		return "", errors.New("target machine identity cannot be read")
	}
	value := strings.ToLower(strings.TrimSpace(string(payload)))
	if !machineIDPattern.MatchString(value) {
		return "", errors.New("target machine identity is invalid")
	}
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func rejectAuthorizationDuplicateKeys(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := validateUniqueAuthorizationJSONValue(decoder); err != nil {
		return errors.New("OpenInference authorization contains duplicate or invalid JSON fields")
	}
	return ensureAuthorizationJSONEOF(decoder)
}

func validateUniqueAuthorizationJSONValue(decoder *json.Decoder) error {
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
			nameToken, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := nameToken.(string)
			if !ok {
				return errors.New("JSON object field name is invalid")
			}
			if _, exists := fields[name]; exists {
				return errors.New("duplicate JSON object field")
			}
			fields[name] = struct{}{}
			if err := validateUniqueAuthorizationJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("JSON object is not terminated")
		}
	case '[':
		for decoder.More() {
			if err := validateUniqueAuthorizationJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("JSON array is not terminated")
		}
	default:
		return errors.New("JSON delimiter is invalid")
	}
	return nil
}

func ensureAuthorizationJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("OpenInference authorization has trailing content")
	}
	return nil
}

// ValidateMetadataOnlyOTLP rejects prompt/response-bearing OpenInference
// attributes before credentials or PostgreSQL are accessed. The adapter's
// allowlist remains a second independent boundary.
func ValidateMetadataOnlyOTLP(payload []byte) error {
	if err := rejectAuthorizationDuplicateKeys(payload); err != nil {
		return errors.New("OpenInference bundle contains duplicate or invalid JSON fields")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return errors.New("OpenInference bundle JSON is invalid")
	}
	if err := ensureAuthorizationJSONEOF(decoder); err != nil {
		return err
	}
	if containsRawOpenInferenceAttribute(value) {
		return errors.New("OpenInference bundle contains prompt or response payload attributes")
	}
	return nil
}

func containsRawOpenInferenceAttribute(value any) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if containsRawOpenInferenceAttribute(item) {
				return true
			}
		}
	case map[string]any:
		if key, ok := typed["key"].(string); ok && rawOpenInferenceAttribute(key) {
			return true
		}
		for _, item := range typed {
			if containsRawOpenInferenceAttribute(item) {
				return true
			}
		}
	}
	return false
}

func rawOpenInferenceAttribute(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, exact := range []string{
		"input.value",
		"output.value",
		"llm.prompt_template.variables",
		"llm.prompts",
	} {
		if key == exact {
			return true
		}
	}
	return strings.HasPrefix(key, "llm.input_messages") ||
		strings.HasPrefix(key, "llm.output_messages")
}
