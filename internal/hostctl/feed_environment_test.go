package hostctl

import (
	"os"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/config"
)

func TestFeedEnvironmentAcceptsLegacyThreeLinesAndOptionalStrictMode(t *testing.T) {
	engine, paths, _ := newFeedCredentialFixture(t)
	endpoint := "https://private.example.invalid" + config.UniRouterCompletedExecutionsPath
	writeFeedCredentialRuntimeEnvironment(t, paths, endpoint)

	values, err := engine.readFeedCredentialEnvironment()
	if err != nil {
		t.Fatalf("read legacy environment: %v", err)
	}
	if _, exists := values[config.UniRouterFeedMappingModeEnv]; exists {
		t.Fatal("legacy three-line environment unexpectedly supplied a mode")
	}
	runtimeConfig, err := config.ParseUniRouterFeedRuntimeConfig(
		values[config.UniRouterFeedEndpointEnv],
		values[config.UniRouterFeedPollIntervalEnv],
		values[config.UniRouterFeedAllowHTTPEnv],
		values[config.UniRouterFeedMappingModeEnv],
	)
	if err != nil || runtimeConfig.MappingMode != config.UniRouterFeedMappingLegacy {
		t.Fatalf("legacy environment config=%+v err=%v", runtimeConfig, err)
	}

	contents, err := os.ReadFile(paths.FeedEnvironmentFile)
	if err != nil {
		t.Fatal(err)
	}
	contents = append(contents, []byte(
		"XINSIGHT_UNIROUTER_FEED_MAPPING_MODE=shadow\n",
	)...)
	if err := os.WriteFile(paths.FeedEnvironmentFile, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	values, err = engine.readFeedCredentialEnvironment()
	if err != nil {
		t.Fatalf("read four-line environment: %v", err)
	}
	runtimeConfig, err = config.ParseUniRouterFeedRuntimeConfig(
		values[config.UniRouterFeedEndpointEnv],
		values[config.UniRouterFeedPollIntervalEnv],
		values[config.UniRouterFeedAllowHTTPEnv],
		values[config.UniRouterFeedMappingModeEnv],
	)
	if err != nil || runtimeConfig.MappingMode != config.UniRouterFeedMappingShadow {
		t.Fatalf("shadow environment config=%+v err=%v", runtimeConfig, err)
	}

	if err := os.WriteFile(
		paths.FeedEnvironmentFile,
		[]byte(strings.ReplaceAll(
			string(contents),
			"XINSIGHT_UNIROUTER_FEED_MAPPING_MODE=shadow",
			"XINSIGHT_UNIROUTER_FEED_MAPPING_MODE=unsupported",
		)),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	values, err = engine.readFeedCredentialEnvironment()
	if err != nil {
		t.Fatalf("strict reader should defer value validation: %v", err)
	}
	if _, err := config.ParseUniRouterFeedRuntimeConfig(
		values[config.UniRouterFeedEndpointEnv],
		values[config.UniRouterFeedPollIntervalEnv],
		values[config.UniRouterFeedAllowHTTPEnv],
		values[config.UniRouterFeedMappingModeEnv],
	); err == nil {
		t.Fatal("unsupported mapping mode was accepted")
	}
}
