package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	LiteLLMFeedEndpointEnv       = "XINSIGHT_LITELLM_FEED_ENDPOINT"
	LiteLLMFeedPollIntervalEnv   = "XINSIGHT_LITELLM_FEED_POLL_INTERVAL"
	LiteLLMFeedSourceInstanceEnv = "XINSIGHT_LITELLM_FEED_SOURCE_INSTANCE"
	LiteLLMFeedTenantRefEnv      = "XINSIGHT_LITELLM_FEED_TENANT_REF"
	LiteLLMFeedOrgRefEnv         = "XINSIGHT_LITELLM_FEED_ORG_REF"
	LiteLLMFeedProjectRefEnv     = "XINSIGHT_LITELLM_FEED_PROJECT_REF"
	LiteLLMFeedKeyFileEnv        = "XINSIGHT_LITELLM_FEED_KEY_FILE"
	LiteLLMFeedCredential        = "litellm-feed.key"
	LiteLLMSpendLogsPath         = "/spend/logs/v2"

	DefaultLiteLLMFeedPollInterval = 30 * time.Second
	MinLiteLLMFeedPollInterval     = time.Second
	MaxLiteLLMFeedPollInterval     = time.Minute
	MaxLiteLLMFeedKeyBytes         = 4096
)

type LiteLLMFeedRuntimeConfig struct {
	Endpoint       string
	PollInterval   time.Duration
	SourceInstance string
	TenantRef      *string
	OrgRef         string
	ProjectRef     string
}

func LoadLiteLLMFeedRuntimeConfig() (LiteLLMFeedRuntimeConfig, error) {
	return ParseLiteLLMFeedRuntimeConfig(
		os.Getenv(LiteLLMFeedEndpointEnv),
		os.Getenv(LiteLLMFeedPollIntervalEnv),
		os.Getenv(LiteLLMFeedSourceInstanceEnv),
		os.Getenv(LiteLLMFeedTenantRefEnv),
		os.Getenv(LiteLLMFeedOrgRefEnv),
		os.Getenv(LiteLLMFeedProjectRefEnv),
	)
}

func ParseLiteLLMFeedRuntimeConfig(
	endpoint string,
	pollIntervalValue string,
	sourceInstance string,
	tenantRef string,
	orgRef string,
	projectRef string,
) (LiteLLMFeedRuntimeConfig, error) {
	parsedEndpoint, err := url.Parse(endpoint)
	if endpoint == "" ||
		strings.TrimSpace(endpoint) != endpoint ||
		err != nil ||
		parsedEndpoint.Scheme != "https" ||
		parsedEndpoint.Host == "" ||
		parsedEndpoint.User != nil ||
		parsedEndpoint.Path != LiteLLMSpendLogsPath ||
		parsedEndpoint.RawPath != "" ||
		parsedEndpoint.RawQuery != "" ||
		parsedEndpoint.Fragment != "" {
		return LiteLLMFeedRuntimeConfig{}, fmt.Errorf(
			"%s must be exact HTTPS %s",
			LiteLLMFeedEndpointEnv,
			LiteLLMSpendLogsPath,
		)
	}
	if !validBoundedConfigText(sourceInstance, 1, 128) {
		return LiteLLMFeedRuntimeConfig{}, fmt.Errorf(
			"%s must be 1..128 safe UTF-8 bytes",
			LiteLLMFeedSourceInstanceEnv,
		)
	}
	if !validPositiveRef(orgRef) || !validPositiveRef(projectRef) {
		return LiteLLMFeedRuntimeConfig{}, errors.New(
			"LiteLLM org and project refs must be canonical positive decimal strings",
		)
	}
	var tenant *string
	if tenantRef != "" {
		if !validBoundedConfigText(tenantRef, 1, 128) {
			return LiteLLMFeedRuntimeConfig{}, fmt.Errorf(
				"%s is invalid",
				LiteLLMFeedTenantRefEnv,
			)
		}
		copy := tenantRef
		tenant = &copy
	}
	pollInterval := DefaultLiteLLMFeedPollInterval
	if pollIntervalValue != "" {
		parsed, err := time.ParseDuration(pollIntervalValue)
		if err != nil ||
			parsed < MinLiteLLMFeedPollInterval ||
			parsed > MaxLiteLLMFeedPollInterval {
			return LiteLLMFeedRuntimeConfig{}, fmt.Errorf(
				"%s must be between %s and %s",
				LiteLLMFeedPollIntervalEnv,
				MinLiteLLMFeedPollInterval,
				MaxLiteLLMFeedPollInterval,
			)
		}
		pollInterval = parsed
	}
	return LiteLLMFeedRuntimeConfig{
		Endpoint:       endpoint,
		PollInterval:   pollInterval,
		SourceInstance: sourceInstance,
		TenantRef:      tenant,
		OrgRef:         orgRef,
		ProjectRef:     projectRef,
	}, nil
}

func LoadLiteLLMFeedAPIKey() (string, error) {
	explicitFile := strings.TrimSpace(os.Getenv(LiteLLMFeedKeyFileEnv))
	credentialFile, err := systemdCredentialPath(LiteLLMFeedCredential)
	if err != nil {
		return "", errors.New("LiteLLM feed credential cannot be inspected")
	}
	if explicitFile != "" && credentialFile != "" {
		return "", errors.New("configure exactly one explicit or systemd LiteLLM feed credential file")
	}
	path := explicitFile
	if path == "" {
		path = credentialFile
	}
	if path == "" {
		return "", errors.New("LiteLLM feed credential file is required")
	}
	return loadLiteLLMCredentialFile(path)
}

func loadLiteLLMCredentialFile(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("LiteLLM feed credential file path must be absolute and clean")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return "", errors.New("LiteLLM feed credential file cannot be inspected")
	}
	if err := validateLiteLLMSecretFileMetadata(before); err != nil {
		return "", errors.New("LiteLLM feed credential file permissions or ownership are invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("LiteLLM feed credential file cannot be opened")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return "", errors.New("LiteLLM feed credential file changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, MaxLiteLLMFeedKeyBytes+1))
	if err != nil || len(contents) > MaxLiteLLMFeedKeyBytes {
		return "", errors.New("LiteLLM feed credential file cannot be read")
	}
	value := strings.TrimSuffix(string(contents), "\n")
	value = strings.TrimSuffix(value, "\r")
	if value == "" || strings.TrimSpace(value) != value {
		return "", errors.New("LiteLLM feed credential is empty or malformed")
	}
	return value, nil
}

func validateLiteLLMSecretFileMetadata(info os.FileInfo) error {
	permissions := info.Mode().Perm()
	if permissions != 0o400 && permissions != 0o600 {
		return errors.New("LiteLLM feed credential permissions must be 0400 or 0600")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok ||
		(stat.Uid != 0 && int(stat.Uid) != os.Geteuid()) ||
		stat.Nlink != 1 {
		return errors.New("LiteLLM feed credential ownership or link count is invalid")
	}
	return nil
}

func validBoundedConfigText(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character == 0 || character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func validPositiveRef(value string) bool {
	if value == "" || value == "0" || (len(value) > 1 && value[0] == '0') {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}
