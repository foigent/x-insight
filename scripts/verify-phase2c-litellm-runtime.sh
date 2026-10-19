#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "Phase 2C LiteLLM runtime gate: $*" >&2
  exit 1
}

require_literal() {
  local literal="$1"
  local path="$2"
  grep -F -q -- "${literal}" "${path}" ||
    fail "${path} does not contain required literal ${literal}"
}

migration_digest="$(
  find migrations -type f -print |
    LC_ALL=C sort |
    xargs sha256sum |
    sha256sum |
    awk '{print $1}'
)"
# Previous FPV16/schema45 baseline is preserved in Git history.
# FPV20 uses the same explicitly reviewed additive46 tree as Reference;
# released migrations1-45 and all LiteLLM protocol bindings stay unchanged.
if [[ "${migration_digest}" != "b18e060bcba8793d22593d51073361cdabd9df95d81ab6c3b4310d9767b41b98" ]]; then
  fail "migration tree changed (${migration_digest})"
fi

pin="contracts/foigent/event-connector-protocol.pin.json"
binding="contracts/foigent/xinsight-litellm-projection-binding.v1.json"
require_literal '"protocol_version": "2.0.2"' "${pin}"
require_literal '"tag": "v2.0.2"' "${pin}"
require_literal '"release_digest": "sha256:99f8a541b6d33b0a74d2e4c06aa312c6d6c88c35a4bfb1d4d34b98b4e4e2e8e8"' "${pin}"
require_literal '"connector_id": "foigent.litellm.spend-logs"' "${binding}"
require_literal '"connector_version": "1.0.0"' "${binding}"
require_literal '"source_system_id": "71ee805a-997c-5265-badb-4e453f30d4e0"' "${binding}"
require_literal '"adapter_manifest_id": "ae560b8a-eb62-5756-b8d1-be164a82232b"' "${binding}"
require_literal '"cursor_key": "litellm.spend-logs.v1"' "${binding}"
require_literal 'FROM connector_checkpoint_epoch_operations AS epoch_operation' "internal/integration/litellmfeed/postgres_state.go"
require_literal "epoch_operation.status = 'prepared'" "internal/integration/litellmfeed/postgres_state.go"

environment="deploy/xinsight-litellm-feed.env.example"
if [[ "$(grep -c '^XINSIGHT_LITELLM_FEED_' "${environment}")" -ne 6 ]]; then
  fail "managed LiteLLM environment must contain exactly six non-secret fields"
fi
if grep -E -q '(KEY|TOKEN|SECRET|PASSWORD|DSN)=' "${environment}"; then
  fail "managed LiteLLM environment contains a secret-shaped field"
fi
require_literal 'XINSIGHT_LITELLM_FEED_ENDPOINT=https://' "${environment}"
require_literal '/spend/logs/v2' "${environment}"
PYTHONDONTWRITEBYTECODE=1 python3 - "${environment}" <<'PY'
from pathlib import Path
import sys

from scripts.validate_managed_environment import (
    LITELLM_ENVIRONMENT_KEYS,
    LITELLM_OPTIONAL_EMPTY_KEYS,
    parse_litellm_environment,
)

path = Path(sys.argv[1])
text = path.read_text(encoding="utf-8")


def expect_invalid(candidate: str) -> None:
    try:
        parse_litellm_environment(candidate)
    except ValueError:
        return
    raise SystemExit("managed LiteLLM environment negative vector was accepted")


actual = parse_litellm_environment(text)
tenant_key = "XINSIGHT_LITELLM_FEED_TENANT_REF"
if actual[tenant_key] != "":
    raise SystemExit("managed LiteLLM environment example tenant must remain unbound")

parse_litellm_environment(
    "\n".join(
        f"{tenant_key}=tenant-example" if line.startswith(f"{tenant_key}=") else line
        for line in text.splitlines()
    )
)
for required_key in LITELLM_ENVIRONMENT_KEYS - LITELLM_OPTIONAL_EMPTY_KEYS:
    expect_invalid(
        "\n".join(
            f"{required_key}=" if line.startswith(f"{required_key}=") else line
            for line in text.splitlines()
        )
    )
expect_invalid(text + "\nXINSIGHT_LITELLM_FEED_UNKNOWN=value\n")
duplicate_line = next(
    line
    for line in text.splitlines()
    if line.strip() and not line.strip().startswith("#")
)
expect_invalid(text + "\n" + duplicate_line + "\n")
expect_invalid(
    "\n".join(
        line
        for line in text.splitlines()
        if not line.startswith("XINSIGHT_LITELLM_FEED_PROJECT_REF=")
    )
)
PY

component="scripts/ci/run-litellm-component-integration.sh"
component_config="scripts/ci/litellm-component-config.yaml"
failure_classifier="scripts/ci/litellm-safe-failure-classifier.py"
postgres_convergence="scripts/ci/run-litellm-postgres-convergence.sh"
protocol_workflow=".github/workflows/foigent-protocol-conformance.yml"
require_literal 'source_postgres_dsn="${LITELLM_COMPONENT_SOURCE_POSTGRES_DSN:?LITELLM_COMPONENT_SOURCE_POSTGRES_DSN is required}"' "${component}"
require_literal 'image="ghcr.io/berriai/litellm@sha256:a1745e629abfb17d434426ff48b115f54f4f4c4a0f5af241de569e93c63c411e"' "${component}"
require_literal 'litellm_version="1.93.0"' "${component}"
require_literal 'litellm_upstream_commit="052b5a2169d8d3082e1d66e69f200a72b0c1e274"' "${component}"
require_literal 'upstream_network_alias="xinsight-litellm-upstream"' "${component}"
require_literal '--network "${network_name}"' "${component}"
require_literal '--network-alias "${upstream_network_alias}"' "${component}"
require_literal '--entrypoint python' "${component}"
require_literal '--tail 400' "${component}"
require_literal '"${master_key}"' "${component}"
require_literal '"${upstream_api_key}"' "${component}"
require_literal '"${success_prompt}"' "${component}"
require_literal '"${second_success_prompt}"' "${component}"
require_literal '"${success_response}"' "${component}"
require_literal 'PGCONNECT_TIMEOUT=5 timeout 15s' "${component}"
require_literal 'privacy_status="present_diagnostics_withheld"' "${component}"
require_literal 'failed to capture bounded container logs for privacy verification' "${component}"
require_literal 'raw_log_lines_emitted=0; response_bodies_emitted=0' "${component}"
require_literal 'fixture_stage="setup"' "${component}"
require_literal 'fixture_stage_started_at=' "${component}"
require_literal 'timespec="microseconds"' "${component}"
require_literal 'set_fixture_stage() {' "${component}"
require_literal 'bounded_failure_count_or_unavailable() {' "${component}"
require_literal '[[ "${value}" =~ ^[0-4]$ ]]' "${component}"
require_literal 'sample_failure_upstream_metrics() {' "${component}"
require_literal 'sample_failure_source_spend_log_count() {' "${component}"
require_literal 'classify_failure_stage_log() {' "${component}"
require_literal 'capture_failure_stage_log() {' "${component}"
require_literal '"${litellm_upstream_commit}" \' "${component}"
require_literal 'PGCONNECT_TIMEOUT=2 timeout 5s' "${component}"
require_literal 'timeout 5s docker exec \' "${component}"
require_literal 'timeout 5s docker logs \' "${component}"
require_literal '--since "${fixture_stage_started_at}"' "${component}"
require_literal '--tail 200' "${component}"
require_literal '--command '\''SELECT count(*) FROM "LiteLLM_SpendLogs"'\''' "${component}"
require_literal 'fixture_stage=${fixture_stage}; upstream_chat_post_count=${failure_upstream_chat_post_count}; upstream_success_response_started_count=${failure_upstream_success_response_started_count}; upstream_success_response_completed_count=${failure_upstream_success_response_completed_count}' "${component}"
require_literal 'exception_class=${exception_class}; failure_stage=${failure_stage}; serialization_substage=${serialization_substage}; privacy_sentinels=${privacy_status}' "${component}"
require_literal 'exception_class="withheld"' "${component}"
require_literal 'failure_stage="withheld"' "${component}"
require_literal 'serialization_substage="withheld"' "${component}"
if [[ "$(grep -c '^set_fixture_stage "' "${component}")" -ne 8 ]]; then
  fail "component must make exactly eight enumerated fixture-stage transitions"
fi
for fixture_stage in \
  observer_denial \
  observer_postcheck \
  authorized_success \
  authorized_second_success \
  authorized_controlled_failure \
  unauthorized_team_success \
  connector_convergence \
  complete; do
  if [[ "$(grep -F -c -- "set_fixture_stage \"${fixture_stage}\"" "${component}")" -ne 1 ]]; then
    fail "component fixture stage ${fixture_stage} must be selected exactly once"
  fi
done
diagnostic_count_pattern='^[0-4]$'
for diagnostic_count in 0 1 2 3 4; do
  [[ "${diagnostic_count}" =~ ${diagnostic_count_pattern} ]] ||
    fail "diagnostic count validator rejected ${diagnostic_count}"
done
for diagnostic_count in "" -1 5 01 1.0 " 1" "1 " 9223372036854775807; do
  if [[ "${diagnostic_count}" =~ ${diagnostic_count_pattern} ]]; then
    fail "diagnostic count validator accepted a noncanonical value"
  fi
done
python3 - "${component}" <<'PY'
from pathlib import Path
import sys

text = Path(sys.argv[1]).read_text(encoding="utf-8")


def function_body(name: str, next_name: str) -> str:
    start_marker = f"{name}() {{"
    end_marker = f"\n\n{next_name}() {{"
    start = text.index(start_marker)
    end = text.index(end_marker, start)
    return text[start:end]


upstream = function_body(
    "sample_failure_upstream_metrics",
    "sample_failure_source_spend_log_count",
)
source = function_body(
    "sample_failure_source_spend_log_count",
    "classify_failure_stage_log",
)
classifier = function_body(
    "classify_failure_stage_log",
    "privacy_sentinel_present",
)
for name, body in (("upstream", upstream), ("source", source)):
    for forbidden in ("for ", "while ", "sleep ", "--retry"):
        if forbidden in body:
            raise SystemExit(
                f"failure-only {name} diagnostic contains retry behavior"
            )
if upstream.count("deterministic_upstream_health_metrics") != 1:
    raise SystemExit("failure-only upstream diagnostic must contain one sample call")
if "timeout 5s" not in text[
    text.index("deterministic_upstream_health_metrics() {"):
    text.index("\n}\n\ndeterministic_upstream_chat_post_count() {")
]:
    raise SystemExit("upstream diagnostic helper must have a five-second outer timeout")
if source.count('SELECT count(*) FROM "LiteLLM_SpendLogs"') != 1:
    raise SystemExit("failure-only source diagnostic must contain one fixed count query")
if "2>/dev/null" not in upstream or "2>/dev/null" not in source:
    raise SystemExit("failure-only diagnostics must suppress command error text")
if classifier.count("litellm-safe-failure-classifier.py") != 1:
    raise SystemExit("failure classifier must run exactly once per invocation")
if classifier.count('"${litellm_upstream_commit}"') != 1:
    raise SystemExit("failure classifier must bind the pinned source commit")

capture = function_body("capture_failure_stage_log", "container_state")
if capture.count('docker logs') != 1:
    raise SystemExit("stage-local failure log must have one Docker log capture")
if capture.count('--since "${fixture_stage_started_at}"') != 1:
    raise SystemExit("stage-local failure log must bind the exact stage timestamp")
if capture.count("--tail 200") != 1 or "timeout 5s" not in capture:
    raise SystemExit("stage-local failure log must be bounded")
for forbidden in ("for ", "while ", "sleep ", "--retry"):
    if forbidden in capture:
        raise SystemExit("stage-local failure log contains retry behavior")

diagnostics = function_body("emit_failure_diagnostics", "litellm_get_status")
if diagnostics.count("sample_failure_upstream_metrics") != 1:
    raise SystemExit("failure diagnostics must sample upstream metrics once")
if diagnostics.count("capture_failure_stage_log") != 1:
    raise SystemExit("failure diagnostics must capture one stage-local log")
privacy_gate = 'if [ "${privacy_status}" = "absent" ]; then'
if privacy_gate not in diagnostics:
    raise SystemExit("failure diagnostics lack the privacy gate")
gated = diagnostics[diagnostics.index(privacy_gate):]
if gated.count("classify_failure_stage_log") != 1:
    raise SystemExit("failure classification must run only after privacy passes")
for forbidden in (
    "traceback_path=",
    "trace_hash=",
    "exception_message=",
    "exception_args=",
    "source_line=",
    "function_name=",
):
    if forbidden in diagnostics:
        raise SystemExit(f"failure diagnostics expose forbidden field {forbidden}")
PY
require_literal '"/health/readiness"' "${component}"
require_literal '"/health/readiness/details"' "${component}"
require_literal '"/team/list"' "${component}"
require_literal 'management_ready_streak=$((management_ready_streak + 1))' "${component}"
require_literal '.litellm_version == $version' "${component}"
require_literal 'readiness_phase=${readiness_phase}' "${component}"
require_literal 'readiness_persistent_state_counts=' "${component}"
require_literal '"LiteLLM_AuditLog"' "${component}"
if grep -F -q -- '/health/liveliness' "${component}"; then
  fail "component setup must gate on readiness, not process liveliness"
fi
if grep -F -q -- '--retry' "${component}"; then
  fail "component setup must not retry management writes"
fi
require_literal 'docker exec --interactive "${container_name}"' "${component}"
require_literal 'ssl.create_default_context(cafile="/tls/server.crt")' "${component}"
require_literal 'urllib.request.ProxyHandler({})' "${component}"
require_literal 'NoRedirectHandler()' "${component}"
require_literal 'with post_opener.open(post_request, timeout=30) as response:' "${component}"
require_literal 'if response.status not in expected_statuses:' "${component}"
require_literal 'post_request.add_header("Authorization", f"Bearer {authorization}")' "${component}"
require_literal 'MAX_RESPONSE_BYTES = 256 * 1024' "${component}"
require_literal 'authorization_bytes, separator, request_body = input_bytes.partition(b"\n")' "${component}"
require_literal 'printf '\''%s\n%s'\'' "${authorization}" "${body}"' "${component}"
if [[ "$(grep -F -c -- 'with post_opener.open(post_request, timeout=30) as response:' "${component}")" -ne 1 ]]; then
  fail "container-local POST helper must contain exactly one request operation"
fi
if [[ "$(grep -F -c -- 'management_post \' "${component}")" -ne 7 ]]; then
  fail "component setup must contain exactly seven single-attempt management writes"
fi
if [[ "$(grep -F -c -- 'fixture_post \' "${component}")" -ne 5 ]]; then
  fail "component setup must contain exactly five single-attempt fixture writes"
fi
require_literal 'http://xinsight-litellm-upstream:18081/v1' "${component_config}"
if grep -F -q -- 'api_base: http://host.docker.internal:18081/v1' "${component_config}"; then
  fail "deterministic upstream must not route through the host loopback gateway"
fi
require_literal '\"team_member_permissions\":[\"/spend/logs\"]' "${component}"
require_literal '\"allowed_routes\":[\"/spend/logs/v2\"]' "${component}"
require_literal '"{\"key_alias\":\"xinsight-component-observer\",\"team_id\":\"${authorized_team_id}\",\"user_id\":\"${observer_user_id}\",\"models\":[],\"allowed_routes\":[\"/spend/logs/v2\"]}" \' "${component}"
require_literal 'except http.client.RemoteDisconnected as exception:' "${component}"
require_literal 'observer_disconnect_false_conditions(' "${component}"
require_literal 'type(exception) is http.client.RemoteDisconnected' "${component}"
require_literal '"exception_type"' "${component}"
require_literal '"disconnect_policy"' "${component}"
require_literal '"route"' "${component}"
require_literal '"expected_statuses"' "${component}"
require_literal '"authorization_mode"' "${component}"
require_literal '"request_profile"' "${component}"
require_literal '"litellm_version"' "${component}"
require_literal '"image_identity"' "${component}"
require_literal '"litellm_source_commit"' "${component}"
require_literal '"request_shape"' "${component}"
require_literal 'return [name for name, matched in conditions if not matched]' "${component}"
require_literal '"RemoteDisconnected:false_conditions="' "${component}"
require_literal '+ ",".join(false_conditions)' "${component}"
require_literal 'if type(exception).__name__ == "RemoteDisconnected":' "${component}"
require_literal 'f"exception_module={type(exception).__module__}:"' "${component}"
require_literal 'f"exception_qualname={type(exception).__qualname__}:"' "${component}"
require_literal '"exact_stdlib_match=false"' "${component}"
if grep -F -q 'str(exception)' "${component}" ||
  grep -F -q 'exception.args' "${component}"; then
  fail "observer disconnect diagnostics must not emit exception text or arguments"
fi
require_literal 'pinned_observer_remote_disconnected_v1_93_0' "${component}"
require_literal '("route", route == "/v1/chat/completions")' "${component}"
require_literal '("expected_statuses", expected_statuses == {403})' "${component}"
require_literal '("authorization_mode", authorization_mode == "stdin_bearer")' "${component}"
require_literal '("request_profile", request_profile == "observer_route_denial")' "${component}"
require_literal '("litellm_version", litellm_version == "1.93.0")' "${component}"
require_literal '"sha256:a1745e629abfb17d434426ff48b115f54f4f4c4a0f5af241de569e93c63c411e"' "${component}"
require_literal '== "052b5a2169d8d3082e1d66e69f200a72b0c1e274"' "${component}"
require_literal '"content": "xinsight-privacy-v1-7a91d0-observer-denied-prompt"' "${component}"
require_literal 'raise SystemExit(72)' "${component}"
if [[ "$(grep -c -- '^  "pinned_observer_remote_disconnected_v1_93_0" \\$' "${component}")" -ne 1 ]]; then
  fail "exactly one fixture call may request the pinned observer disconnect policy"
fi
if [[ "$(grep -c -- '^  "observer_route_denial"$' "${component}")" -ne 1 ]]; then
  fail "exactly one fixture call may use the observer denial profile"
fi
require_literal '"{\"model\":\"xinsight-ci-model\",\"messages\":[{\"role\":\"user\",\"content\":\"${observer_denied_prompt}\"}],\"max_tokens\":8}" \' "${component}"
require_literal '"mode":"strict_http_403","source_response_present":true' "${component}"
require_literal '"mode":"pinned_remote_disconnected","source_response_present":false' "${component}"
require_literal 'assert_observer_boundary_health' "${component}"
if [[ "$(grep -c '^assert_observer_boundary_health$' "${component}")" -ne 2 ]]; then
  fail "observer boundary health must run exactly before and after the negative request"
fi
require_literal 'litellm_observer_pid_before=' "${component}"
require_literal 'upstream_observer_pid_before=' "${component}"
require_literal 'pinned LiteLLM container restarted across observer denial' "${component}"
require_literal 'deterministic upstream restarted across observer denial' "${component}"
require_literal '[ ! -s "${output_path}" ]' "${component}"
require_literal 'unlink "${output_path}"' "${component}"
require_literal 'observer_allowed_empty_get' "${component}"
require_literal '"${workdir}/observer-empty-spend-logs-v2-response.json" \' "${component}"
require_literal 'observer_denial_authorization="${observer_key}"' "${component}"
if [[ "$(grep -F -c -- '"${observer_denial_authorization}"' "${component}")" -ne 2 ]]; then
  fail "the exact same observer key must drive denial and allowed-route probes"
fi
require_literal 'observer_source_spend_count=' "${component}"
require_literal '[ "${observer_source_spend_count}" = "${observer_source_spend_count_before}" ]' "${component}"
require_literal '[ "${observer_upstream_chat_post_count_after}" = "${observer_upstream_chat_post_count_before}" ]' "${component}"
require_literal '[ "${upstream_chat_post_count}" = "4" ]' "${component}"
require_literal 'XINSIGHT_LITELLM_COMPONENT_API_KEY="${observer_key}"' "${component}"
require_literal 'observer spend-log response did not contain exactly the three authorized-team rows' "${component}"
require_literal 'XINSIGHT_LITELLM_COMPONENT_AUTHORIZED_TRAFFIC_KEY_SENTINEL="${authorized_traffic_key}"' "${component}"
require_literal 'XINSIGHT_LITELLM_COMPONENT_OBSERVER_KEY_SENTINEL="${observer_key}"' "${component}"
require_literal 'XINSIGHT_LITELLM_COMPONENT_UNAUTHORIZED_TRAFFIC_KEY_SENTINEL="${unauthorized_traffic_key}"' "${component}"
require_literal 'docker rm --force "${container_name}"' "${component}"
require_literal 'docker rm --force "${upstream_container_name}"' "${component}"
require_literal 'docker network rm "${network_name}"' "${component}"
require_literal 'litellmfeed_database="xinsight_protocol_litellmfeed"' "${postgres_convergence}"
require_literal 'registry_database="xinsight_protocol_registry"' "${postgres_convergence}"
require_literal 'env -u XINSIGHT_POSTGRES_DSN' "${postgres_convergence}"
require_literal 'XINSIGHT_POSTGRES_DSN="${litellmfeed_dsn}"' "${postgres_convergence}"
require_literal 'XINSIGHT_POSTGRES_DSN="${registry_dsn}"' "${postgres_convergence}"
require_literal 'run: bash scripts/ci/run-litellm-postgres-convergence.sh' "${protocol_workflow}"
if grep -F -q -- '-p 1' "${postgres_convergence}"; then
  fail "PostgreSQL convergence must prove database isolation, not serialize package tests"
fi
require_literal 'input_cost_per_token: 0.000001' "${component_config}"
require_literal 'output_cost_per_token: 0.000002' "${component_config}"
require_literal 'row.Record.ReportedSpend != "0.000007"' "internal/integration/litellmfeed/component_integration_test.go"
require_literal '[4]string{"3", "2", "5", "0.000007"}' "internal/integration/litellmfeed/component_integration_test.go"
if grep -F -q -- 'Authorization: Bearer ${master_key}' "${component}"; then
  fail "LiteLLM management writes must not send the master key through host curl"
fi
if grep -F -q -- 'https://127.0.0.1:4000/v1/chat/completions' "${component}"; then
  fail "LiteLLM fixture POSTs must not use the host-published HTTPS path"
fi

security_manual="docs/manual/zh-CN/admin/03-configuration-security-and-access.md"
require_literal 'team_member_permissions=["/spend/logs"]' "${security_manual}"
require_literal 'allowed_routes=["/spend/logs/v2"]' "${security_manual}"
require_literal 'LiteLLM master key 只能用于受控的团队/成员/key 初始化或清理' "${security_manual}"
require_literal 'X credential、模型流量 key 或 `/spend/logs/v2` 日常读取 key' "${security_manual}"
require_literal 'LiteLLM `v1.93.0`、容器 digest' "${security_manual}"
require_literal 'sha256:a1745e629abfb17d434426ff48b115f54f4f4c4a0f5af241de569e93c63c411e' "${security_manual}"
require_literal '052b5a2169d8d3082e1d66e69f200a72b0c1e274' "${security_manual}"
require_literal 'pinned fail-closed degraded error' "${security_manual}"
require_literal '但不得称为 HTTP `403`' "${security_manual}"
require_literal '官方 Source commit 或负向测试形状' "${security_manual}"
require_literal '必须删除该例外并恢复' "${security_manual}"

upstream="scripts/ci/litellm-component-upstream.py"
require_literal 'import uuid' "${upstream}"
require_literal 'CHAT_COMPLETION_ID_PREFIX = "chatcmpl-xinsight-component-"' "${upstream}"
require_literal 'return f"{CHAT_COMPLETION_ID_PREFIX}{uuid.uuid4().hex}"' "${upstream}"
require_literal '_chat_post_count = 0' "${upstream}"
require_literal '_chat_success_response_started_count = 0' "${upstream}"
require_literal '_chat_success_response_completed_count = 0' "${upstream}"
require_literal 'CHAT_RESPONSE_STATUSES = (200, 400, 401, 404, 413, 422)' "${upstream}"
require_literal '_chat_metrics_lock = threading.Lock()' "${upstream}"
require_literal '**_get_chat_metrics()' "${upstream}"
require_literal '_record_chat_post()' "${upstream}"
require_literal '_record_chat_success_response_started()' "${upstream}"
require_literal 'self.wfile.flush()' "${upstream}"
require_literal '_record_chat_response_completed(status)' "${upstream}"
require_literal 'def version_string(self) -> str:' "${upstream}"
if grep -E -q '(_request_body|_authorization|_headers)[[:space:]]*=' "${upstream}"; then
  fail "deterministic upstream must not retain request body, authorization, or headers"
fi

python3 - "${upstream}" "${failure_classifier}" <<'PY'
from __future__ import annotations

import importlib.util
import io
import json
import re
import sys
from pathlib import Path


def load_module(name: str, path: str):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise SystemExit(f"cannot load {name}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


upstream = load_module("litellm_component_upstream", sys.argv[1])
classifier = load_module("litellm_safe_failure_classifier", sys.argv[2])
if (
    classifier.PINNED_LITELLM_SOURCE_COMMIT
    != "052b5a2169d8d3082e1d66e69f200a72b0c1e274"
):
    raise SystemExit("failure classifier source pin drifted")


def reset_metrics():
    upstream._chat_post_count = 0
    upstream._chat_success_response_started_count = 0
    upstream._chat_success_response_completed_count = 0
    upstream._chat_response_status_counts = {
        status: 0 for status in upstream.CHAT_RESPONSE_STATUSES
    }


class FlushProbe:
    def __init__(self, exception=None):
        self.exception = exception
        self.calls = 0

    def flush(self):
        self.calls += 1
        if self.exception is not None:
            raise self.exception


def handler(json_exception=None, flush_exception=None):
    result = object.__new__(upstream.Handler)

    def emit(_status, _payload):
        if json_exception is not None:
            raise json_exception

    result._json = emit
    result.wfile = FlushProbe(flush_exception)
    return result


version_probe = object.__new__(upstream.Handler)
version_string = version_probe.version_string()
if version_string != "xinsight-litellm-ci-upstream/1":
    raise SystemExit("deterministic upstream Server identity drifted")
if version_string != version_string.strip() or any(
    character.isspace() for character in version_string
):
    raise SystemExit("deterministic upstream Server identity contains whitespace")

completion_ids = []
completion_profiles = []
for prompt in (
    upstream.SUCCESS_PROMPT,
    "xinsight-privacy-v1-7a91d0-second-success-prompt",
):
    success_body = json.dumps(
        {"messages": [{"content": prompt}]}
    ).encode("utf-8")
    success_probe = object.__new__(upstream.Handler)
    success_probe.path = "/v1/chat/completions"
    success_probe.headers = {
        "Authorization": upstream.UPSTREAM_BEARER,
        "Content-Length": str(len(success_body)),
    }
    success_probe.rfile = io.BytesIO(success_body)
    success_responses = []
    success_probe._chat_json = lambda status, payload: success_responses.append(
        (status, payload)
    )
    success_probe.do_POST()
    if len(success_responses) != 1 or success_responses[0][0] != 200:
        raise SystemExit("repeat successful upstream call was not HTTP 200")
    response = success_responses[0][1]
    completion_ids.append(response["id"])
    completion_profiles.append(
        {key: value for key, value in response.items() if key != "id"}
    )
if len(set(completion_ids)) != 2:
    raise SystemExit("successful upstream response IDs are not unique")
if any(
    re.fullmatch(r"chatcmpl-xinsight-component-[0-9a-f]{32}", value) is None
    for value in completion_ids
):
    raise SystemExit("successful upstream response ID grammar drifted")
if completion_profiles[0] != completion_profiles[1]:
    raise SystemExit("successful upstream non-identity fields are not deterministic")

controlled_body = json.dumps(
    {"messages": [{"content": upstream.FAILURE_PROMPT}]}
).encode("utf-8")
controlled_probe = object.__new__(upstream.Handler)
controlled_probe.path = "/v1/chat/completions"
controlled_probe.headers = {
    "Authorization": upstream.UPSTREAM_BEARER,
    "Content-Length": str(len(controlled_body)),
}
controlled_probe.rfile = io.BytesIO(controlled_body)
controlled_responses = []
controlled_probe._chat_json = lambda status, payload: controlled_responses.append(
    (status, payload["error"]["code"])
)
controlled_probe.do_POST()
if controlled_responses != [(400, "xinsight_controlled_failure")]:
    raise SystemExit("controlled upstream response is not deterministic HTTP 400")


reset_metrics()
success = handler()
success._chat_json(200, {"ok": True})
metrics = upstream._get_chat_metrics()
if metrics["chat_success_response_started_count"] != 1:
    raise SystemExit("success response was not marked started")
if metrics["chat_success_response_completed_count"] != 1:
    raise SystemExit("success response was not marked completed")
if metrics["chat_response_status_200_count"] != 1:
    raise SystemExit("completed 200 response was not counted")

reset_metrics()
json_failed = handler(json_exception=RuntimeError())
try:
    json_failed._chat_json(200, {"ok": True})
except RuntimeError:
    pass
else:
    raise SystemExit("JSON failure fixture did not fail")
metrics = upstream._get_chat_metrics()
if metrics["chat_success_response_started_count"] != 1:
    raise SystemExit("failed JSON response was not marked started")
if metrics["chat_success_response_completed_count"] != 0:
    raise SystemExit("failed JSON response was marked completed")
if metrics["chat_response_status_200_count"] != 0:
    raise SystemExit("failed JSON response status was marked completed")

reset_metrics()
flush_failed = handler(flush_exception=RuntimeError())
try:
    flush_failed._chat_json(200, {"ok": True})
except RuntimeError:
    pass
else:
    raise SystemExit("flush failure fixture did not fail")
metrics = upstream._get_chat_metrics()
if metrics["chat_success_response_started_count"] != 1:
    raise SystemExit("flush failure response was not marked started")
if metrics["chat_success_response_completed_count"] != 0:
    raise SystemExit("flush failure response was marked completed")
if metrics["chat_response_status_200_count"] != 0:
    raise SystemExit("flush failure status was marked completed")

reset_metrics()
controlled = handler()
controlled._chat_json(400, {"error": {}})
metrics = upstream._get_chat_metrics()
if metrics["chat_success_response_started_count"] != 0:
    raise SystemExit("controlled response was marked as success-started")
if metrics["chat_response_status_400_count"] != 1:
    raise SystemExit("completed 400 response was not counted")

reset_metrics()
untracked_status = handler()
untracked_status._chat_json(500, {"error": {}})
metrics = upstream._get_chat_metrics()
if any(
    metrics[f"chat_response_status_{status}_count"] != 0
    for status in upstream.CHAT_RESPONSE_STATUSES
):
    raise SystemExit("non-allowlisted response status changed a bounded counter")

timestamp = "2026-07-28T00:00:00.000000000Z "
cases = (
    (
        (
            "/usr/local/lib/python3.13/site-packages/"
            "litellm/llms/custom_httpx/http_handler.py",
        ),
        "TypeError",
        "upstream_decode",
        "unavailable",
    ),
    (
        (
            "/usr/local/lib/python3.13/site-packages/"
            "litellm/proxy/proxy_server.py",
            "/usr/local/lib/python3.13/site-packages/"
            "litellm/llms/openai/chat/handler.py",
            "/usr/local/lib/python3.13/site-packages/"
            "httpcore/_async/connection_pool.py",
        ),
        "RuntimeError",
        "upstream_transport",
        "unavailable",
    ),
    (
        (
            "/usr/local/lib/python3.13/site-packages/"
            "litellm/types/utils.py",
        ),
        "ValueError",
        "model_conversion",
        "unavailable",
    ),
    (
        (
            "/usr/local/lib/python3.13/site-packages/"
            "litellm/proxy/proxy_logging.py",
        ),
        "pydantic_core._pydantic_core.ValidationError",
        "post_call_hook",
        "unavailable",
    ),
    (
        (
            "/usr/local/lib/python3.13/site-packages/"
            "starlette/responses.py",
        ),
        "asyncio.exceptions.CancelledError",
        "response_serialization",
        "unavailable",
    ),
    (
        (
            "/usr/local/lib/python3.13/site-packages/"
            "litellm/proxy/proxy_server.py",
        ),
        "TypeError",
        "proxy_response_pipeline",
        "unavailable",
    ),
    (
        (
            "/usr/local/lib/python3.13/site-packages/"
            "uvicorn/protocols/http/h11_impl.py",
            "/usr/local/lib/python3.13/asyncio/tasks.py",
        ),
        "RuntimeError",
        "asgi_transport",
        "unavailable",
    ),
    (
        ("/usr/local/lib/python3.13/asyncio/tasks.py",),
        "RuntimeError",
        "async_runtime",
        "unavailable",
    ),
)
for frame_paths, exception_name, expected_stage, expected_substage in cases:
    text = "\n".join(
        [timestamp + "Traceback (most recent call last):"]
        + [
            timestamp + f'  File "{frame_path}", line 7, in fixture'
            for frame_path in frame_paths
        ]
        + [timestamp + f"{exception_name}: private text"]
    )
    exception_class, failure_stage, serialization_substage = (
        classifier.classify_log_text(text)
    )
    expected_exception = (
        exception_name.rsplit(".", 1)[-1]
        if exception_name.rsplit(".", 1)[-1]
        in {
            "AttributeError",
            "TypeError",
            "KeyError",
            "LocalProtocolError",
            "PydanticSerializationError",
            "RuntimeError",
            "UnicodeEncodeError",
            "ValueError",
            "ValidationError",
            "CancelledError",
        }
        else "Other"
    )
    if (exception_class, failure_stage, serialization_substage) != (
        expected_exception,
        expected_stage,
        expected_substage,
    ):
        raise SystemExit("closed failure classifier vector mismatch")
    emitted = f"{exception_class}|{failure_stage}|{serialization_substage}"
    if not re.fullmatch(
        (
            r"(?:AttributeError|TypeError|KeyError|LocalProtocolError|"
            r"PydanticSerializationError|RuntimeError|UnicodeEncodeError|"
            r"ValueError|ValidationError|CancelledError|Other|unavailable)"
            r"\|(?:upstream_decode|upstream_transport|"
            r"model_conversion|post_call_hook|response_serialization|"
            r"proxy_response_pipeline|asgi_transport|async_runtime|"
            r"unavailable)\|(?:fastapi_jsonable_encoder|"
            r"starlette_header_init|starlette_render|starlette_send_start|"
            r"starlette_send_body|h11_header_validation|"
            r"h11_content_length_body|unavailable)"
        ),
        emitted,
    ):
        raise SystemExit("failure classifier emitted non-enum output")
    if any(frame_path in emitted for frame_path in frame_paths):
        raise SystemExit("failure classifier emitted private traceback path")
    if "private text" in emitted:
        raise SystemExit("failure classifier emitted private traceback data")

serialization_cases = (
    (
        (
            (
                "/usr/local/lib/python3.13/site-packages/"
                "fastapi/encoders.py",
                129,
                "jsonable_encoder",
            ),
        ),
        "PydanticSerializationError",
        "fastapi_jsonable_encoder",
    ),
    (
        (
            (
                "/usr/local/lib/python3.13/site-packages/"
                "starlette/responses.py",
                61,
                "init_headers",
            ),
        ),
        "UnicodeEncodeError",
        "starlette_header_init",
    ),
    (
        (
            (
                "/usr/local/lib/python3.13/site-packages/"
                "starlette/responses.py",
                195,
                "render",
            ),
        ),
        "AttributeError",
        "starlette_render",
    ),
    (
        (
            (
                "/usr/local/lib/python3.13/site-packages/"
                "starlette/responses.py",
                166,
                "__call__",
            ),
        ),
        "KeyError",
        "starlette_send_start",
    ),
    (
        (
            (
                "/usr/local/lib/python3.13/site-packages/"
                "starlette/responses.py",
                167,
                "__call__",
            ),
        ),
        "RuntimeError",
        "starlette_send_body",
    ),
    (
        (
            (
                "/usr/local/lib/python3.13/site-packages/"
                "starlette/responses.py",
                166,
                "__call__",
            ),
            (
                "/usr/local/lib/python3.13/site-packages/"
                "h11/_headers.py",
                165,
                "normalize_and_validate",
            ),
        ),
        "LocalProtocolError",
        "h11_header_validation",
    ),
    (
        (
            (
                "/usr/local/lib/python3.13/site-packages/"
                "starlette/responses.py",
                167,
                "__call__",
            ),
            (
                "/usr/local/lib/python3.13/site-packages/"
                "h11/_writers.py",
                91,
                "send_data",
            ),
        ),
        "LocalProtocolError",
        "h11_content_length_body",
    ),
)
for frames, exception_name, expected_substage in serialization_cases:
    text = "\n".join(
        [timestamp + "Traceback (most recent call last):"]
        + [
            timestamp
            + f'  File "{frame_path}", line {line_number}, in {function_name}'
            for frame_path, line_number, function_name in frames
        ]
        + [timestamp + f"{exception_name}: private text"]
    )
    result = classifier.classify_log_text(text)
    if result != (
        exception_name,
        "response_serialization",
        expected_substage,
    ):
        raise SystemExit("serialization substage classifier vector mismatch")
    emitted = "|".join(result)
    if any(
        private_value in emitted
        for frame_path, line_number, function_name in frames
        for private_value in (
            frame_path,
            str(line_number),
            "private text",
        )
    ):
        raise SystemExit("serialization classifier emitted private traceback data")

generic_transport = "\n".join(
    (
        "Traceback (most recent call last):",
        (
            '  File "/usr/local/lib/python3.13/site-packages/'
            'httpx/_client.py", line 1, in fixture'
        ),
        "RuntimeError: private text",
    )
)
if classifier.classify_log_text(generic_transport) != (
    "RuntimeError",
    "unavailable",
    "unavailable",
):
    raise SystemExit("generic HTTP transport was misclassified without provider context")

disallowed = "\n".join(
    (
        "Traceback (most recent call last):",
        '  File "/private/unknown.py", line 1, in fixture',
        "TypeError: private text",
    )
)
if classifier.classify_log_text(disallowed) != (
    "TypeError",
    "unavailable",
    "unavailable",
):
    raise SystemExit("disallowed traceback path was classified")
malformed = "\n".join(
    (
        "Traceback (most recent call last):",
        '  File "/private/unknown.py", line 1, in fixture',
        "PrivateFailure: private text",
    )
)
if classifier.classify_log_text(malformed) != (
    "unavailable",
    "unavailable",
    "unavailable",
):
    raise SystemExit("malformed exception type was classified")
if classifier.classify_log_text("\n".join("line" for _ in range(201))) != (
    "unavailable",
    "unavailable",
    "unavailable",
):
    raise SystemExit("over-line-limit log was classified")
if classifier.classify_log_text(
    "x" * (classifier.MAX_LOG_BYTES + 1)
) != ("unavailable", "unavailable", "unavailable"):
    raise SystemExit("over-byte-limit log was classified")
if classifier.classify_log_text(
    "\n".join(
        (
            "Traceback (most recent call last):",
            (
                '  File "/usr/local/lib/python3.13/site-packages/'
                'litellm/types/utils.py", line 1, in fixture'
            ),
            "TypeError: private text",
        )
    ),
    source_commit="0" * 40,
) != ("unavailable", "unavailable", "unavailable"):
    raise SystemExit("unbound source commit was classified")

classifier_source = Path(sys.argv[2]).read_text(encoding="utf-8")
for forbidden in (
    "print(text",
    "print(path",
    "print(line",
    "trace_hash",
    "exception_message",
    "exception_args",
):
    if forbidden in classifier_source:
        raise SystemExit(f"failure classifier contains forbidden output {forbidden}")
PY

unit="deploy/xinsight-litellm-feed.service"
require_literal 'EnvironmentFile=/etc/x-insight/xinsight-litellm-feed.env' "${unit}"
require_literal 'LoadCredential=postgres-importer.dsn:/etc/x-insight/database/current/postgres-importer.dsn' "${unit}"
require_literal 'LoadCredential=litellm-feed.key:/etc/x-insight/litellm-feed.key' "${unit}"
require_literal 'ExecStart=/opt/x-insight/current/bin/xinsight-importer litellm pull-spend-logs' "${unit}"
if grep -q '^Environment=' "${unit}"; then
  fail "LiteLLM unit places configuration or credentials in systemd Environment"
fi

if find \
  internal/integration/pullfeed \
  internal/adapters/unirouter \
  cmd/xinsight-server \
  -type f -name '*.go' ! -name '*_test.go' -print0 |
  xargs -0 grep -E -i -q 'litellm'; then
  fail "LiteLLM source switching leaked into the completed-execution or reader runtime"
fi
if find \
  internal/integration/litellmfeed \
  internal/adapters/litellm \
  -type f -name '*.go' ! -name '*_test.go' -print0 |
  xargs -0 grep -E -i -q 'unirouter|completed.execution'; then
  fail "completed-execution source switching leaked into the LiteLLM runtime"
fi
if find deploy -maxdepth 1 -type f -iname '*litellm*compose*' -print -quit |
  grep -q .; then
  fail "X-Insight release embeds LiteLLM source infrastructure"
fi

echo "Phase 2C LiteLLM runtime gate: exact registration, isolated worker, least-privilege observer, credential boundary, checkpoint state, and reviewed migration tree verified"
