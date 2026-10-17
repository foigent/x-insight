#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT="${1:-}"
TRUST_STORE="${2:-}"
DESTINATION="${3:-}"
RELEASE_TOOL="${XINSIGHT_RELEASE_TOOL:-}"

if [ -z "$ARTIFACT" ] || [ -z "$TRUST_STORE" ]; then
  echo "usage: scripts/verify-signed-release.sh ARTIFACT TRUST_STORE [ABSOLUTE_DESTINATION]" >&2
  exit 1
fi
case "$ARTIFACT" in /*) ;; *) ARTIFACT="$PWD/$ARTIFACT" ;; esac
case "$TRUST_STORE" in /*) ;; *) TRUST_STORE="$PWD/$TRUST_STORE" ;; esac
if [ -z "$DESTINATION" ]; then
  VERIFY_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/xinsight-signed-verify.XXXXXX")"
  DESTINATION="$VERIFY_ROOT/payload"
fi
case "$DESTINATION" in
  /*) ;;
  *)
    echo "verification destination must be an absolute path" >&2
    exit 1
    ;;
esac

if [ "$(uname -s)" != "Linux" ] || [ "$(uname -m)" != "x86_64" ]; then
  echo "executable release verification requires Linux x86_64" >&2
  exit 1
fi

if [ -n "$RELEASE_TOOL" ]; then
  case "$RELEASE_TOOL" in
    /*) ;;
    *)
      echo "XINSIGHT_RELEASE_TOOL must be an absolute path" >&2
      exit 1
      ;;
  esac
  if [ ! -x "$RELEASE_TOOL" ]; then
    echo "XINSIGHT_RELEASE_TOOL is not executable: $RELEASE_TOOL" >&2
    exit 1
  fi
fi

run_release_tool() {
  if [ -n "$RELEASE_TOOL" ]; then
    "$RELEASE_TOOL" "$@"
    return
  fi
  go run ./cmd/xinsight-release "$@"
}

cd "$ROOT"
EXPECTED_COMMIT="$(git rev-parse HEAD)"
PRODUCT_VERSION="$(python3 -c 'import json; print(json.load(open("release/release-config.json", encoding="utf-8"))["product_version"])')"
MANUAL_VERSION="$(python3 -c 'import json; print(json.load(open("release/release-config.json", encoding="utf-8"))["manual"]["version"])')"

run_release_tool verify \
  --artifact "$ARTIFACT" \
  --trust-store "$TRUST_STORE" \
  --expected-commit "$EXPECTED_COMMIT" \
  --expected-version "$PRODUCT_VERSION" > "${DESTINATION}.verify.json"

run_release_tool extract \
  --artifact "$ARTIFACT" \
  --trust-store "$TRUST_STORE" \
  --destination "$DESTINATION" \
  --expected-commit "$EXPECTED_COMMIT" \
  --expected-version "$PRODUCT_VERSION" > "${DESTINATION}.extract.json"

CAMPAIGN_AUTHORITY_DIRECTORY="${DESTINATION}.campaign-authority"
run_release_tool campaign-authority derive \
  --artifact "$ARTIFACT" \
  --trust-store "$TRUST_STORE" \
  --expected-commit "$EXPECTED_COMMIT" \
  --expected-version "$PRODUCT_VERSION" \
  --output-directory "$CAMPAIGN_AUTHORITY_DIRECTORY" \
  > "${DESTINATION}.campaign-authority.derive.json"
run_release_tool campaign-authority verify \
  --artifact "$ARTIFACT" \
  --trust-store "$TRUST_STORE" \
  --product-release "$CAMPAIGN_AUTHORITY_DIRECTORY/xinsight.release-identity.v1.json" \
  --verifier-release "$CAMPAIGN_AUTHORITY_DIRECTORY/xinsight.signed-campaign-package-consumer-verifier.v1.json" \
  --expected-commit "$EXPECTED_COMMIT" \
  --expected-version "$PRODUCT_VERSION" \
  > "${DESTINATION}.campaign-authority.verify.json"

for command in xinsight-server xinsight-importer xinsightctl xinsight-launcher; do
  "$DESTINATION/bin/$command" version --json > "${DESTINATION}.${command}.version.json"
done
"$DESTINATION/bin/xinsight-server" webui-manifest --json \
  > "${DESTINATION}.xinsight-server.webui-manifest.json"
"$DESTINATION/bin/xinsightctl" schema-contract expected --raw \
  > "${DESTINATION}.xinsightctl.schema-contract.json"
run_release_tool version --json > "${DESTINATION}.xinsight-release.version.json"
FEED_CREDENTIAL_TEST_ROOT="${DESTINATION}.feed-credential-status-root"
env \
  XINSIGHTCTL_ALLOW_TEST_ROOT=1 \
  XINSIGHTCTL_TEST_ROOT="$FEED_CREDENTIAL_TEST_ROOT" \
  "$DESTINATION/bin/xinsightctl" feed-credential status \
  > "${DESTINATION}.feed-credential-status.json"

PYTHONDONTWRITEBYTECODE=1 python3 - \
  "$DESTINATION" \
  "$EXPECTED_COMMIT" \
  "$PRODUCT_VERSION" \
  "$MANUAL_VERSION" <<'PY'
from html.parser import HTMLParser
import hashlib
import json
import pathlib
import re
import sys

from scripts.validate_managed_environment import parse_litellm_environment

root = pathlib.Path(sys.argv[1])
expected_commit = sys.argv[2]
expected_version = sys.argv[3]
expected_manual = sys.argv[4]

committed_webui_manifest_path = pathlib.Path(
    "internal/webui/dist/asset-manifest.json"
)
runtime_webui_manifest_path = pathlib.Path(
    str(root) + ".xinsight-server.webui-manifest.json"
)
committed_webui_manifest_bytes = committed_webui_manifest_path.read_bytes()
runtime_webui_manifest_bytes = runtime_webui_manifest_path.read_bytes()
if runtime_webui_manifest_bytes != committed_webui_manifest_bytes:
    raise SystemExit("signed runtime web UI manifest differs from committed generated manifest")
webui_manifest = json.loads(committed_webui_manifest_bytes)
if (
    webui_manifest.get("schema_version")
    != "xinsight.webui-asset-manifest.v1"
    or len(webui_manifest.get("entries", [])) != 3
):
    raise SystemExit("committed web UI asset manifest identity is invalid")
for entry in webui_manifest["entries"]:
    asset_path = pathlib.Path("internal/webui/dist") / entry["path"]
    asset_bytes = asset_path.read_bytes()
    if (
        len(asset_bytes) != entry["size_bytes"]
        or "sha256:" + hashlib.sha256(asset_bytes).hexdigest() != entry["sha256"]
    ):
        raise SystemExit(
            f"committed web UI asset differs from generated manifest: {entry['path']}"
        )

committed_schema_contract = pathlib.Path(
    "internal/hostctl/clean_baseline_schema_contract.json"
).read_bytes()
payload_schema_contract = (root / "release/schema-contract.json").read_bytes()
runtime_schema_contract = pathlib.Path(
    str(root) + ".xinsightctl.schema-contract.json"
).read_bytes()
if not (
    committed_schema_contract == payload_schema_contract == runtime_schema_contract
):
    raise SystemExit(
        "generated, signed-payload, and embedded runtime schema contracts differ"
    )
schema_contract = json.loads(committed_schema_contract)
if schema_contract.get("schema_version") != "xinsight.schema-contract.v2":
    raise SystemExit("schema contract version is invalid")

matrix = json.loads((root / "release/compatibility-matrix.json").read_text(encoding="utf-8"))
if matrix["product_version"] != expected_version or matrix["manual_version"] != expected_manual:
    raise SystemExit("compatibility matrix version mismatch")
expected_api_versions = ["xinsight.investigation.v1", "xinsight.access.v1"]
if matrix.get("api_versions") != expected_api_versions:
    raise SystemExit("compatibility matrix API version mismatch")
if matrix.get("schema_contract") != {
    "schema_version": schema_contract["schema_version"],
    "semantic_sha256": schema_contract["semantic_sha256"],
    "migration_set_sha256": schema_contract["migration_set_sha256"],
}:
    raise SystemExit("compatibility matrix schema contract binding mismatch")
managed_drop_in = (root / "deploy/99-xinsight-managed-launcher.conf").read_text(encoding="utf-8")
if managed_drop_in != "[Service]\nExecStart=\nExecStart=/opt/x-insight/control/xinsight-launcher\n":
    raise SystemExit("managed systemd launcher drop-in mismatch")
server_unit = (root / "deploy/xinsight.service").read_text(encoding="utf-8")
expected_server_credentials = {
    "LoadCredential=postgres-reader.dsn:/etc/x-insight/database/current/postgres-reader.dsn",
    "LoadCredential=postgres-workspace.dsn:/etc/x-insight/database/current/postgres-workspace.dsn",
    "LoadCredential=access-accounts.json:/etc/x-insight/access-accounts.json",
}
actual_server_credentials = {
    line.strip()
    for line in server_unit.splitlines()
    if line.strip().startswith("LoadCredential=")
}
if actual_server_credentials != expected_server_credentials:
    raise SystemExit(
        f"managed server credential boundary mismatch: {sorted(actual_server_credentials)}"
    )
for required in (
    "User=xinsight",
    "ExecStart=/opt/x-insight/control/xinsight-launcher",
    "EnvironmentFile=/etc/x-insight/xinsight.env",
    "EnvironmentFile=-/etc/x-insight/trusted-tls-proxy.env",
    "EnvironmentFile=-/etc/x-insight/xinsight-connector-id.env",
    "EnvironmentFile=-/etc/x-insight/xinsight-connector.env",
    "ExecStartPre=+/usr/bin/env -i /opt/x-insight/current/bin/xinsightctl connector-target-config validate",
    "IPAddressDeny=any",
    "IPAddressAllow=localhost",
):
    if required not in server_unit:
        raise SystemExit(f"managed server unit is missing: {required}")
for forbidden in (
    "postgres-importer.dsn",
    "postgres-admin.dsn",
    "unirouter-feed.key",
    "litellm-feed.key",
    "XINSIGHT_ACCESS",
    "XINSIGHT_PASSWORD",
):
    if forbidden in server_unit:
        raise SystemExit(f"managed server unit crossed a credential boundary: {forbidden}")
connector_environment_example = (
    root / "deploy/xinsight-connector.env.example"
).read_text(encoding="utf-8")
expected_connector_environment_example = (
    "# Reference only; do not install this placeholder file.\n"
    "# Use xinsightctl connector-target-config set; the installer owns the separate identity file.\n"
    "XINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://REPLACE_WITH_APPROVED_UNIROUTER_ORIGIN\n"
)
if connector_environment_example != expected_connector_environment_example:
    raise SystemExit("managed Connector environment example mismatch")
controller_unit = (
    root / "deploy/xinsight-connector-controller.service"
).read_text(encoding="utf-8")
for required in (
    "Before=xinsight.service",
    "User=root",
    "Group=xinsight",
    "LoadCredential=postgres-admin.dsn:/etc/x-insight/database/current/postgres-admin.dsn",
    "ExecStart=/opt/x-insight/current/bin/xinsightctl connector-controller serve",
    "RuntimeDirectory=x-insight",
    "RuntimeDirectoryMode=0750",
    "CapabilityBoundingSet=",
    "AmbientCapabilities=",
    "RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
    "IPAddressDeny=any",
    "IPAddressAllow=100.64.0.0/10",
):
    if required not in controller_unit:
        raise SystemExit(f"managed connector controller unit is missing: {required}")
for forbidden in (
    "postgres-reader.dsn",
    "postgres-workspace.dsn",
    "postgres-importer.dsn",
    "unirouter-feed.key",
    "litellm-feed.key",
    "--socket",
    "--dsn",
):
    if forbidden in controller_unit:
        raise SystemExit(
            f"managed connector controller crossed a boundary: {forbidden}"
        )
if (root / "deploy/xinsight-connector-controller.socket").exists():
    raise SystemExit("connector controller must not ship a separate systemd socket unit")
proxy_environment = (
    root / "deploy/trusted-tls-proxy.env.example"
).read_text(encoding="utf-8")
for required in (
    "XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback",
    "XINSIGHT_EXTERNAL_ORIGIN=https://REPLACE_WITH_APPROVED_HOST_OR_IP",
):
    if required not in proxy_environment:
        raise SystemExit(f"trusted TLS proxy environment example is missing: {required}")
nginx_template = (
    root / "deploy/xinsight-nginx-ip.conf.example"
).read_text(encoding="utf-8")
for required in (
    "server_name REPLACE_WITH_PUBLIC_IP;",
    "proxy_pass http://127.0.0.1:8080;",
    "proxy_set_header X-Forwarded-Proto https;",
    'proxy_set_header Forwarded "";',
):
    if required not in nginx_template:
        raise SystemExit(f"X-Insight Nginx template is missing: {required}")
feed_unit = (root / "deploy/xinsight-feed.service").read_text(encoding="utf-8")
for required in (
    "User=xinsight",
    "LoadCredential=postgres-importer.dsn:/etc/x-insight/database/current/postgres-importer.dsn",
    "LoadCredential=unirouter-feed.key:/etc/x-insight/unirouter-feed.key",
    "ExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed",
    "EnvironmentFile=/etc/x-insight/xinsight-feed.env",
    "IPAddressDeny=any",
):
    if required not in feed_unit:
        raise SystemExit(f"managed feed worker unit is missing: {required}")
if "xinsight-server" in feed_unit or "postgres-reader.dsn" in feed_unit:
    raise SystemExit("managed feed worker unit crossed the writer/reader service boundary")

feed_environment = {}
for line in (root / "deploy/xinsight-feed.env.example").read_text(encoding="utf-8").splitlines():
    line = line.strip()
    if not line or line.startswith("#"):
        continue
    key, separator, value = line.partition("=")
    if separator != "=" or not key or not value or key in feed_environment:
        raise SystemExit("managed feed worker environment example is invalid")
    feed_environment[key] = value
if set(feed_environment) != {
    "XINSIGHT_UNIROUTER_FEED_ENDPOINT",
    "XINSIGHT_UNIROUTER_FEED_POLL_INTERVAL",
    "XINSIGHT_UNIROUTER_FEED_ALLOW_PRIVATE_TUNNEL_HTTP",
    "XINSIGHT_UNIROUTER_FEED_MAPPING_MODE",
}:
    raise SystemExit("managed feed worker environment keys are invalid")
expected_feed_endpoint = (
    "https://REPLACE_WITH_PRIVATE_UNIROUTER_HOST"
    "/api/v1/integrations/xinsight/completed-executions"
)
if feed_environment["XINSIGHT_UNIROUTER_FEED_ENDPOINT"] != expected_feed_endpoint:
    raise SystemExit("managed feed worker endpoint does not match the UniRouter V1 route template")
if feed_environment["XINSIGHT_UNIROUTER_FEED_MAPPING_MODE"] != "legacy":
    raise SystemExit("managed feed worker mapping mode must default to legacy")

litellm_unit = (root / "deploy/xinsight-litellm-feed.service").read_text(encoding="utf-8")
for required in (
    "User=xinsight",
    "LoadCredential=postgres-importer.dsn:/etc/x-insight/database/current/postgres-importer.dsn",
    "LoadCredential=litellm-feed.key:/etc/x-insight/litellm-feed.key",
    "ExecStart=/opt/x-insight/current/bin/xinsight-importer litellm pull-spend-logs",
    "EnvironmentFile=/etc/x-insight/xinsight-litellm-feed.env",
    "IPAddressDeny=any",
):
    if required not in litellm_unit:
        raise SystemExit(f"managed LiteLLM worker unit is missing: {required}")
if (
    "xinsight-server" in litellm_unit
    or "postgres-reader.dsn" in litellm_unit
    or "unirouter-feed.key" in litellm_unit
):
    raise SystemExit("managed LiteLLM worker crossed a reader or Connector credential boundary")

try:
    litellm_environment = parse_litellm_environment(
        (root / "deploy/xinsight-litellm-feed.env.example").read_text(
            encoding="utf-8"
        )
    )
except ValueError:
    raise SystemExit("managed LiteLLM worker environment example is invalid") from None
if (
    litellm_environment["XINSIGHT_LITELLM_FEED_ENDPOINT"]
    != "https://REPLACE_WITH_PRIVATE_LITELLM_HOST/spend/logs/v2"
):
    raise SystemExit("managed LiteLLM worker endpoint does not match the V1 route template")

registration_schema = json.loads(
    (root / "schemas/integration/feed-credential-registration-v1.schema.json").read_text(
        encoding="utf-8"
    )
)
expected_registration_fields = {
    "schema_version",
    "token_format",
    "verifier_scheme",
    "key_prefix",
    "verifier_sha256",
    "operation_id",
}
if (
    registration_schema.get("additionalProperties") is not False
    or set(registration_schema.get("required", [])) != expected_registration_fields
    or set(registration_schema.get("properties", {})) != expected_registration_fields
    or registration_schema["properties"]["schema_version"].get("const")
    != "unirouter.xinsight.feed-credential-registration.v1"
    or registration_schema["properties"]["token_format"].get("const") != "ur-xf1-hex256.v1"
    or registration_schema["properties"]["verifier_scheme"].get("const")
    != "receiver_sha256_v1"
):
    raise SystemExit("Frozen Registration Bundle V1 schema is not exact")

account_schema = json.loads(
    (root / "schemas/access/local-accounts-v1.schema.json").read_text(
        encoding="utf-8"
    )
)
expected_account_fields = {
    "subject_id",
    "username",
    "role",
    "disabled",
    "password_hash",
}
account_items = account_schema.get("properties", {}).get("accounts", {})
account_entry = account_items.get("items", {})
if (
    account_schema.get("additionalProperties") is not False
    or set(account_schema.get("required", [])) != {"schema_version", "accounts"}
    or account_schema["properties"]["schema_version"].get("const")
    != "xinsight.local-accounts.v1"
    or account_items.get("type") != "array"
    or account_items.get("minItems") != 1
    or account_items.get("maxItems") != 100
    or account_entry.get("type") != "object"
    or account_entry.get("additionalProperties") is not False
    or set(account_entry.get("required", [])) != expected_account_fields
    or set(account_entry.get("properties", {})) != expected_account_fields
    or account_entry["properties"]["role"].get("enum")
    != ["investigator", "operator"]
    or account_entry["properties"]["subject_id"].get("pattern")
    != r"^xs_[a-z0-9]{16,64}$"
    or account_entry["properties"]["username"].get("pattern")
    != r"^[a-z0-9][a-z0-9._-]{2,63}$"
    or account_entry["properties"]["disabled"].get("type") != "boolean"
    or account_entry["properties"]["password_hash"].get("pattern")
    != r"^\$argon2id\$v=19\$m=19456,t=2,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$"
):
    raise SystemExit("Local Accounts V1 schema is not exact")

zero_token = "ur-xf1-" + ("00" * 32)
zero_verifier = hashlib.sha256(
    b"unirouter.xinsight.feed-credential.v1\x00" + zero_token.encode("ascii")
).hexdigest()
if (
    zero_token[:20] != "ur-xf1-0000000000000"
    or zero_verifier != "d14985e8cedc38985529b846e34f152448401b2b2e5e28db0f64bd8727c1ff07"
):
    raise SystemExit("Frozen Registration Bundle V1 golden vector mismatch")

credential_status = json.loads(
    pathlib.Path(str(root) + ".feed-credential-status.json").read_text(encoding="utf-8")
)
if (
    credential_status.get("schema_version") != "xinsight.feed-credential-status.v1"
    or credential_status.get("status") != "absent"
    or credential_status.get("active_file", {}).get("present") is not False
    or credential_status.get("staged_file", {}).get("present") is not False
):
    raise SystemExit(f"feed credential absent-status proof failed: {credential_status}")

for command in ("xinsight-server", "xinsight-importer", "xinsightctl", "xinsight-launcher", "xinsight-release"):
    version_path = pathlib.Path(str(root) + f".{command}.version.json")
    identity = json.loads(version_path.read_text(encoding="utf-8"))
    expected = {
        "product_name": "x-insight",
        "product_version": expected_version,
        "commit": expected_commit,
        "database_schema_min": matrix["database_schema"],
        "database_schema_max": matrix["database_schema"],
        "config_schema_version": matrix["config_schema"],
        "api_version": expected_api_versions[0],
        "access_api_version": expected_api_versions[1],
        "manual_version": expected_manual,
    }
    for field, value in expected.items():
        if identity.get(field) != value:
            raise SystemExit(f"{command} {field}={identity.get(field)!r}, expected {value!r}")

class ManualParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.ids = set()
        self.refs = []
        self.meta = {}

    def handle_starttag(self, tag, attrs):
        values = dict(attrs)
        if values.get("id"):
            self.ids.add(values["id"])
        if tag in {"a", "link", "img", "script"}:
            attribute = "href" if tag in {"a", "link"} else "src"
            if values.get(attribute):
                self.refs.append(values[attribute])
        if tag == "meta" and values.get("name"):
            self.meta[values["name"]] = values.get("content", "")

manual_path = root / "manual/zh-CN/index.html"
manual = manual_path.read_text(encoding="utf-8")
parser = ManualParser()
parser.feed(manual)
if parser.meta.get("xinsight-product-version") != expected_version:
    raise SystemExit("manual product version mismatch")
if parser.meta.get("xinsight-manual-version") != expected_manual:
    raise SystemExit("manual revision mismatch")
for ref in parser.refs:
    if ref.startswith(("http:", "https:", "//", "file:", "data:", "javascript:")):
        raise SystemExit(f"manual external or unsafe dependency: {ref}")
    if ref.startswith("#"):
        if ref[1:] not in parser.ids:
            raise SystemExit(f"manual unresolved anchor: {ref}")
        continue
    if ref.startswith("/"):
        if ref != "/executions":
            raise SystemExit(f"manual unexpected same-origin route: {ref}")
        continue
    target = (manual_path.parent / ref.split("#", 1)[0]).resolve()
    if manual_path.parent.resolve() not in target.parents and target != manual_path.parent.resolve():
        raise SystemExit(f"manual path escapes bundle: {ref}")
    if not target.is_file():
        raise SystemExit(f"manual missing asset: {ref}")

css = (root / "manual/zh-CN/manual.css").read_text(encoding="utf-8")
if re.search(r"url\s*\(", css, re.IGNORECASE):
    raise SystemExit("manual CSS must not load URL dependencies")

for path in root.rglob("*"):
    if not path.is_file() or "bin" in path.relative_to(root).parts:
        continue
    data = path.read_bytes()
    for marker in (b"BEGIN PRIVATE KEY", b"BEGIN OPENSSH PRIVATE KEY", b"BEGIN RSA PRIVATE KEY"):
        if marker in data:
            raise SystemExit(f"private key material in payload: {path.relative_to(root)}")

print(f"verified_payload={root}")
print(f"product_version={expected_version}")
print(f"manual_version={expected_manual}")
print(f"commit={expected_commit}")
print("runtime_binary_count=4")
print("bootstrap_binary_count=1")
print("webui_source_generated_embedded_signed_manifest_parity=exact")
print("manual_external_dependencies=0")
PY
