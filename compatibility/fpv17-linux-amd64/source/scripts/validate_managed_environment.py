LITELLM_ENVIRONMENT_KEYS = frozenset(
    {
        "XINSIGHT_LITELLM_FEED_ENDPOINT",
        "XINSIGHT_LITELLM_FEED_POLL_INTERVAL",
        "XINSIGHT_LITELLM_FEED_SOURCE_INSTANCE",
        "XINSIGHT_LITELLM_FEED_TENANT_REF",
        "XINSIGHT_LITELLM_FEED_ORG_REF",
        "XINSIGHT_LITELLM_FEED_PROJECT_REF",
    }
)
LITELLM_OPTIONAL_EMPTY_KEYS = frozenset(
    {
        "XINSIGHT_LITELLM_FEED_TENANT_REF",
    }
)


def parse_litellm_environment(text: str) -> dict[str, str]:
    environment = {}
    for raw_line in text.splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        key, separator, value = line.partition("=")
        if (
            separator != "="
            or not key
            or key not in LITELLM_ENVIRONMENT_KEYS
            or key in environment
            or (not value and key not in LITELLM_OPTIONAL_EMPTY_KEYS)
        ):
            raise ValueError("invalid managed LiteLLM environment")
        environment[key] = value
    if set(environment) != LITELLM_ENVIRONMENT_KEYS:
        raise ValueError("incomplete managed LiteLLM environment")
    return environment
