package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	UniRouterFeedEndpointEnv         = "XINSIGHT_UNIROUTER_FEED_ENDPOINT"
	UniRouterFeedPollIntervalEnv     = "XINSIGHT_UNIROUTER_FEED_POLL_INTERVAL"
	UniRouterFeedAllowHTTPEnv        = "XINSIGHT_UNIROUTER_FEED_ALLOW_PRIVATE_TUNNEL_HTTP"
	UniRouterFeedMappingModeEnv      = "XINSIGHT_UNIROUTER_FEED_MAPPING_MODE"
	UniRouterFeedKeyFileEnv          = "XINSIGHT_UNIROUTER_FEED_KEY_FILE"
	UniRouterFeedCredential          = "unirouter-feed.key"
	UniRouterCompletedExecutionsPath = "/api/v1/integrations/xinsight/completed-executions"

	UniRouterFeedMappingLegacy        = "legacy"
	UniRouterFeedMappingShadow        = "shadow"
	UniRouterFeedMappingNeutralCanary = "neutral_canary"

	DefaultUniRouterFeedPollInterval = 5 * time.Second
	MinUniRouterFeedPollInterval     = time.Second
	MaxUniRouterFeedPollInterval     = time.Minute
	MaxUniRouterFeedKeyBytes         = 4096
)

type UniRouterFeedRuntimeConfig struct {
	Endpoint               string
	PollInterval           time.Duration
	AllowPrivateTunnelHTTP bool
	MappingMode            string
}

func LoadUniRouterFeedRuntimeConfig() (UniRouterFeedRuntimeConfig, error) {
	return ParseUniRouterFeedRuntimeConfig(
		os.Getenv(UniRouterFeedEndpointEnv),
		os.Getenv(UniRouterFeedPollIntervalEnv),
		os.Getenv(UniRouterFeedAllowHTTPEnv),
		os.Getenv(UniRouterFeedMappingModeEnv),
	)
}

// ParseUniRouterFeedRuntimeConfig validates the exact nonsecret runtime values
// used by both the long-running worker and xinsightctl's staged-credential
// authentication preflight.
func ParseUniRouterFeedRuntimeConfig(
	endpoint string,
	pollIntervalValue string,
	allowPrivateTunnelHTTPValue string,
	mappingModeValues ...string,
) (UniRouterFeedRuntimeConfig, error) {
	if endpoint == "" || strings.TrimSpace(endpoint) != endpoint {
		return UniRouterFeedRuntimeConfig{}, fmt.Errorf("%s is required without surrounding whitespace", UniRouterFeedEndpointEnv)
	}
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil || parsedEndpoint.Path != UniRouterCompletedExecutionsPath || parsedEndpoint.RawPath != "" {
		return UniRouterFeedRuntimeConfig{}, fmt.Errorf(
			"%s must use the exact UniRouter completed-execution route %s",
			UniRouterFeedEndpointEnv,
			UniRouterCompletedExecutionsPath,
		)
	}

	pollInterval := DefaultUniRouterFeedPollInterval
	if raw := strings.TrimSpace(pollIntervalValue); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < MinUniRouterFeedPollInterval || parsed > MaxUniRouterFeedPollInterval {
			return UniRouterFeedRuntimeConfig{}, fmt.Errorf(
				"%s must be between %s and %s",
				UniRouterFeedPollIntervalEnv,
				MinUniRouterFeedPollInterval,
				MaxUniRouterFeedPollInterval,
			)
		}
		pollInterval = parsed
	}
	allowPrivateTunnelHTTP := false
	switch raw := allowPrivateTunnelHTTPValue; raw {
	case "", "false":
	case "true":
		allowPrivateTunnelHTTP = true
	default:
		return UniRouterFeedRuntimeConfig{}, fmt.Errorf(
			"%s must be exactly true or false",
			UniRouterFeedAllowHTTPEnv,
		)
	}
	mappingMode := UniRouterFeedMappingLegacy
	if len(mappingModeValues) > 1 {
		return UniRouterFeedRuntimeConfig{}, fmt.Errorf(
			"%s must be configured at most once",
			UniRouterFeedMappingModeEnv,
		)
	}
	if len(mappingModeValues) == 1 && mappingModeValues[0] != "" {
		mappingMode = mappingModeValues[0]
	}
	switch mappingMode {
	case UniRouterFeedMappingLegacy,
		UniRouterFeedMappingShadow,
		UniRouterFeedMappingNeutralCanary:
	default:
		return UniRouterFeedRuntimeConfig{}, fmt.Errorf(
			"%s must be exactly legacy, shadow, or neutral_canary",
			UniRouterFeedMappingModeEnv,
		)
	}

	return UniRouterFeedRuntimeConfig{
		Endpoint:               endpoint,
		PollInterval:           pollInterval,
		AllowPrivateTunnelHTTP: allowPrivateTunnelHTTP,
		MappingMode:            mappingMode,
	}, nil
}

// LoadUniRouterFeedServiceKey reads the feed-only credential from an explicit
// development file or the systemd credential directory. It never returns the
// path or credential contents in an error.
func LoadUniRouterFeedServiceKey() (string, error) {
	explicitFile := strings.TrimSpace(os.Getenv(UniRouterFeedKeyFileEnv))
	credentialFile, err := systemdCredentialPath(UniRouterFeedCredential)
	if err != nil {
		return "", errors.New("UniRouter feed credential cannot be inspected")
	}
	if explicitFile != "" && credentialFile != "" {
		return "", errors.New("configure exactly one explicit or systemd UniRouter feed credential file")
	}
	path := explicitFile
	if path == "" {
		path = credentialFile
	}
	if path == "" {
		return "", errors.New("UniRouter feed credential file is required")
	}
	return loadUniRouterFeedServiceKeyFile(path)
}

func loadUniRouterFeedServiceKeyFile(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("UniRouter feed credential file path must be absolute and clean")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return "", errors.New("UniRouter feed credential file cannot be inspected")
	}
	if !before.Mode().IsRegular() {
		return "", errors.New("UniRouter feed credential file must be a regular file, not a link")
	}
	if err := validateSecretFileMetadata(before); err != nil {
		return "", errors.New("UniRouter feed credential file permissions or ownership are invalid")
	}

	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("UniRouter feed credential file cannot be opened")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return "", errors.New("UniRouter feed credential file changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, MaxUniRouterFeedKeyBytes+1))
	if err != nil {
		return "", errors.New("UniRouter feed credential file cannot be read")
	}
	if len(contents) > MaxUniRouterFeedKeyBytes {
		return "", errors.New("UniRouter feed credential file is too large")
	}
	value := strings.TrimSuffix(string(contents), "\n")
	value = strings.TrimSuffix(value, "\r")
	if value == "" {
		return "", errors.New("UniRouter feed credential file is empty")
	}
	return value, nil
}
