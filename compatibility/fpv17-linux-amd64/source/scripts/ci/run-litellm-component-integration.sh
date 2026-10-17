#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() {
  echo "LiteLLM component integration: $*" >&2
  exit 1
}

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workdir="$(mktemp -d)"
run_suffix="${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}-$$"
container_name="xinsight-litellm-component-${run_suffix}"
upstream_container_name="xinsight-litellm-upstream-${run_suffix}"
network_name="xinsight-litellm-component-${run_suffix}"
upstream_network_alias="xinsight-litellm-upstream"
image="ghcr.io/berriai/litellm@sha256:a1745e629abfb17d434426ff48b115f54f4f4c4a0f5af241de569e93c63c411e"
litellm_version="1.93.0"
litellm_upstream_commit="052b5a2169d8d3082e1d66e69f200a72b0c1e274"
master_key="sk-xinsight-ci-component-master-key-not-secret"
upstream_api_key="sk-xinsight-ci-upstream-not-secret"
consumer_postgres_dsn="${XINSIGHT_POSTGRES_DSN:?XINSIGHT_POSTGRES_DSN is required}"
source_postgres_dsn="${LITELLM_COMPONENT_SOURCE_POSTGRES_DSN:?LITELLM_COMPONENT_SOURCE_POSTGRES_DSN is required}"
consumer_database="xinsight_litellm_component"
source_database="litellm_component_source"
pg_dump_bin="/usr/lib/postgresql/16/bin/pg_dump"
if [ ! -x "${pg_dump_bin}" ] ||
  [[ "$("${pg_dump_bin}" --version)" != "pg_dump (PostgreSQL) 16."* ]]; then
  fail "PostgreSQL 16 pg_dump is required for privacy verification"
fi
success_prompt="xinsight-privacy-v1-7a91d0-success-prompt"
second_success_prompt="xinsight-privacy-v1-7a91d0-second-success-prompt"
failure_prompt="xinsight-privacy-v1-7a91d0-failure-prompt"
unauthorized_team_prompt="xinsight-privacy-v1-7a91d0-unauthorized-team-prompt"
observer_denied_prompt="xinsight-privacy-v1-7a91d0-observer-denied-prompt"
success_response="xinsight-privacy-v1-7a91d0-success-response"
authorized_team_id="xinsight-component-authorized"
unauthorized_team_id="xinsight-component-unauthorized"
authorized_traffic_user_id="xinsight-component-traffic-user"
observer_user_id="xinsight-component-observer-user"
unauthorized_traffic_user_id="xinsight-component-other-team-user"
authorized_traffic_key=""
observer_key=""
unauthorized_traffic_key=""
readiness_phase="not_started"
public_readiness_attempts=0
management_readiness_attempts=0
management_ready_streak=0
readiness_transport_error_count=0
readiness_http_5xx_count=0
fixture_stage="setup"
fixture_stage_started_at="$(
  python3 -c \
    'from datetime import datetime, timezone; print(datetime.now(timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z"))'
)"
[[ "${fixture_stage_started_at}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$ ]] ||
  fail "invalid initial fixture-stage timestamp"

set_fixture_stage() {
  local stage_started_at
  case "${1:-}" in
    setup | observer_denial | observer_postcheck | authorized_success | \
      authorized_second_success | \
      authorized_controlled_failure | unauthorized_team_success | \
      connector_convergence | complete)
      stage_started_at="$(
        python3 -c \
          'from datetime import datetime, timezone; print(datetime.now(timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z"))'
      )"
      [[ "${stage_started_at}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$ ]] ||
        fail "invalid fixture-stage timestamp"
      fixture_stage="$1"
      fixture_stage_started_at="${stage_started_at}"
      ;;
    *)
      fail "invalid internal fixture stage"
      ;;
  esac
}

bounded_failure_count_or_unavailable() {
  local value="${1:-}"
  if [[ "${value}" =~ ^[0-4]$ ]]; then
    printf '%s\n' "${value}"
  else
    printf 'unavailable\n'
  fi
}

sample_failure_upstream_metrics() {
  local sample=""
  local chat_post_count=""
  local success_response_started_count=""
  local success_response_completed_count=""
  local status_200_count=""
  local status_400_count=""
  local status_401_count=""
  local status_404_count=""
  local status_413_count=""
  local status_422_count=""
  if sample="$(deterministic_upstream_health_metrics 2>/dev/null)" &&
    IFS='|' read -r \
      chat_post_count \
      success_response_started_count \
      success_response_completed_count \
      status_200_count \
      status_400_count \
      status_401_count \
      status_404_count \
      status_413_count \
      status_422_count \
      <<<"${sample}" &&
    [ -n "${status_422_count}" ]; then
    printf '%s|%s|%s|%s|%s|%s|%s|%s|%s\n' \
      "$(bounded_failure_count_or_unavailable "${chat_post_count}")" \
      "$(bounded_failure_count_or_unavailable "${success_response_started_count}")" \
      "$(bounded_failure_count_or_unavailable "${success_response_completed_count}")" \
      "$(bounded_failure_count_or_unavailable "${status_200_count}")" \
      "$(bounded_failure_count_or_unavailable "${status_400_count}")" \
      "$(bounded_failure_count_or_unavailable "${status_401_count}")" \
      "$(bounded_failure_count_or_unavailable "${status_404_count}")" \
      "$(bounded_failure_count_or_unavailable "${status_413_count}")" \
      "$(bounded_failure_count_or_unavailable "${status_422_count}")"
  else
    printf '%s\n' \
      'unavailable|unavailable|unavailable|unavailable|unavailable|unavailable|unavailable|unavailable|unavailable'
  fi
}

sample_failure_source_spend_log_count() {
  local sample=""
  if sample="$(
    PGCONNECT_TIMEOUT=2 timeout 5s \
      psql "${source_postgres_dsn}" \
        --no-psqlrc \
        --tuples-only \
        --no-align \
        --set ON_ERROR_STOP=1 \
        --command 'SELECT count(*) FROM "LiteLLM_SpendLogs"' \
        2>/dev/null
  )"; then
    bounded_failure_count_or_unavailable "${sample}"
  else
    printf 'unavailable\n'
  fi
}

classify_failure_stage_log() {
  local sample=""
  local exception_class=""
  local failure_stage=""
  local serialization_substage=""
  if sample="$(
    python3 \
      "${root}/scripts/ci/litellm-safe-failure-classifier.py" \
      "${workdir}/litellm-fixture-stage.log" \
      "${litellm_upstream_commit}" \
      2>/dev/null
  )" &&
    IFS='|' read -r exception_class failure_stage serialization_substage \
      <<<"${sample}"; then
    case "${exception_class}" in
      AttributeError | TypeError | KeyError | LocalProtocolError | \
        PydanticSerializationError | RuntimeError | UnicodeEncodeError | \
        ValueError | ValidationError | CancelledError | Other | unavailable)
        ;;
      *)
        exception_class="unavailable"
        ;;
    esac
    case "${failure_stage}" in
      upstream_decode | upstream_transport | model_conversion | \
        post_call_hook | response_serialization | \
        proxy_response_pipeline | asgi_transport | async_runtime | \
        unavailable)
        ;;
      *)
        failure_stage="unavailable"
        ;;
    esac
    case "${serialization_substage}" in
      fastapi_jsonable_encoder | starlette_header_init | starlette_render | \
        starlette_send_start | starlette_send_body | h11_header_validation | \
        h11_content_length_body | unavailable)
        ;;
      *)
        serialization_substage="unavailable"
        ;;
    esac
    printf '%s|%s|%s\n' \
      "${exception_class}" \
      "${failure_stage}" \
      "${serialization_substage}"
  else
    printf 'unavailable|unavailable|unavailable\n'
  fi
}

privacy_sentinel_present() {
  local path="$1"
  local value
  local -a grep_args=()
  local -a sentinels=(
    "${success_prompt}"
    "${second_success_prompt}"
    "${failure_prompt}"
    "${unauthorized_team_prompt}"
    "${observer_denied_prompt}"
    "${success_response}"
    "${master_key}"
    "${upstream_api_key}"
    "${authorized_traffic_key}"
    "${observer_key}"
    "${unauthorized_traffic_key}"
  )

  [ -f "${path}" ] || return 1
  for value in "${sentinels[@]}"; do
    if [ -n "${value}" ]; then
      grep_args+=(--regexp="${value}")
    fi
  done
  grep \
    --fixed-strings \
    --quiet \
    "${grep_args[@]}" \
    "${path}"
}

assert_privacy_sentinels_absent() {
  local path="$1"
  local layer="$2"
  [ -f "${path}" ] ||
    fail "privacy evidence is unavailable for ${layer}"
  if privacy_sentinel_present "${path}"; then
    fail "privacy sentinel is present in ${layer}"
  fi
}

capture_container_logs() {
  local capture_failed=0
  if docker inspect "${container_name}" >/dev/null 2>&1; then
    docker logs \
      --timestamps \
      --tail 400 \
      "${container_name}" \
      >"${workdir}/litellm-container.log" 2>&1 || capture_failed=1
  fi
  if docker inspect "${upstream_container_name}" >/dev/null 2>&1; then
    docker logs \
      --timestamps \
      --tail 400 \
      "${upstream_container_name}" \
      >"${workdir}/upstream-container.log" 2>&1 || capture_failed=1
  fi
  return "${capture_failed}"
}

capture_failure_stage_log() {
  if ! docker inspect "${container_name}" >/dev/null 2>&1; then
    return
  fi
  timeout 5s docker logs \
    --timestamps \
    --since "${fixture_stage_started_at}" \
    --tail 200 \
    "${container_name}" \
    >"${workdir}/litellm-fixture-stage.log" 2>&1
}

container_state() {
  local name="$1"
  if ! docker inspect "${name}" >/dev/null 2>&1; then
    printf 'absent'
    return
  fi
  docker inspect \
    --format '{{if .State.Running}}running{{else}}stopped{{end}}:{{.State.ExitCode}}' \
    "${name}" \
    2>/dev/null || printf 'unknown'
}

emit_failure_diagnostics() {
  local exit_code="$1"
  local application_startup_complete_count=0
  local application_startup_failure_count=0
  local connection_error_count=0
  local exception_class="unavailable"
  local failure_classification="unavailable|unavailable|unavailable"
  local failure_source_spend_log_count="unavailable"
  local failure_stage="unavailable"
  local failure_upstream_metrics=""
  local failure_upstream_chat_post_count="unavailable"
  local failure_upstream_response_status_200_count="unavailable"
  local failure_upstream_response_status_400_count="unavailable"
  local failure_upstream_response_status_401_count="unavailable"
  local failure_upstream_response_status_404_count="unavailable"
  local failure_upstream_response_status_413_count="unavailable"
  local failure_upstream_response_status_422_count="unavailable"
  local failure_upstream_success_response_completed_count="unavailable"
  local failure_upstream_success_response_started_count="unavailable"
  local name_resolution_error_count=0
  local privacy_status="absent"
  local serialization_substage="unavailable"
  local server_process_started_count=0
  local server_shutdown_count=0
  local source_dump="${workdir}/failure-source-database-data.sql"
  local path

  failure_upstream_metrics="$(
    sample_failure_upstream_metrics
  )"
  IFS='|' read -r \
    failure_upstream_chat_post_count \
    failure_upstream_success_response_started_count \
    failure_upstream_success_response_completed_count \
    failure_upstream_response_status_200_count \
    failure_upstream_response_status_400_count \
    failure_upstream_response_status_401_count \
    failure_upstream_response_status_404_count \
    failure_upstream_response_status_413_count \
    failure_upstream_response_status_422_count \
    <<<"${failure_upstream_metrics}"
  failure_source_spend_log_count="$(
    sample_failure_source_spend_log_count
  )"
  capture_container_logs || true
  capture_failure_stage_log || true
  if PGCONNECT_TIMEOUT=5 timeout 15s \
    "${pg_dump_bin}" \
      "${source_postgres_dsn}" \
      --data-only \
      --no-owner \
      --no-privileges \
      --file="${source_dump}" \
    >/dev/null 2>&1; then
    :
  else
    source_dump=""
  fi

  for path in \
    "${workdir}/litellm-container.log" \
    "${workdir}/litellm-fixture-stage.log" \
    "${workdir}/upstream-container.log" \
    "${source_dump}"; do
    if [ -n "${path}" ] && privacy_sentinel_present "${path}"; then
      privacy_status="present_diagnostics_withheld"
      break
    fi
  done

  if [ "${privacy_status}" = "absent" ]; then
    failure_classification="$(classify_failure_stage_log)"
    IFS='|' read -r exception_class failure_stage serialization_substage \
      <<<"${failure_classification}"
    if [ -f "${workdir}/litellm-container.log" ]; then
      connection_error_count="$(
        grep \
          --extended-regexp \
          --ignore-case \
          --count \
          'connection refused|connecterror|apiconnectionerror|all connection attempts failed' \
          "${workdir}/litellm-container.log" || true
      )"
      name_resolution_error_count="$(
        grep \
          --extended-regexp \
          --ignore-case \
          --count \
          'name or service not known|temporary failure in name resolution|nodename nor servname' \
          "${workdir}/litellm-container.log" || true
      )"
      server_process_started_count="$(
        grep \
          --fixed-strings \
          --count \
          'Started server process' \
          "${workdir}/litellm-container.log" || true
      )"
      application_startup_complete_count="$(
        grep \
          --fixed-strings \
          --count \
          'Application startup complete' \
          "${workdir}/litellm-container.log" || true
      )"
      application_startup_failure_count="$(
        grep \
          --fixed-strings \
          --count \
          'Application startup failed' \
          "${workdir}/litellm-container.log" || true
      )"
      server_shutdown_count="$(
        grep \
          --extended-regexp \
          --count \
          'Shutting down|Finished server process' \
          "${workdir}/litellm-container.log" || true
      )"
    fi
  else
    application_startup_complete_count="withheld"
    application_startup_failure_count="withheld"
    connection_error_count="withheld"
    exception_class="withheld"
    failure_stage="withheld"
    name_resolution_error_count="withheld"
    serialization_substage="withheld"
    server_process_started_count="withheld"
    server_shutdown_count="withheld"
  fi

  echo \
    "LiteLLM component diagnostics: exit=${exit_code}; fixture_stage=${fixture_stage}; upstream_chat_post_count=${failure_upstream_chat_post_count}; upstream_success_response_started_count=${failure_upstream_success_response_started_count}; upstream_success_response_completed_count=${failure_upstream_success_response_completed_count}; upstream_response_status_200_count=${failure_upstream_response_status_200_count}; upstream_response_status_400_count=${failure_upstream_response_status_400_count}; upstream_response_status_401_count=${failure_upstream_response_status_401_count}; upstream_response_status_404_count=${failure_upstream_response_status_404_count}; upstream_response_status_413_count=${failure_upstream_response_status_413_count}; upstream_response_status_422_count=${failure_upstream_response_status_422_count}; source_spend_log_count=${failure_source_spend_log_count}; exception_class=${exception_class}; failure_stage=${failure_stage}; serialization_substage=${serialization_substage}; privacy_sentinels=${privacy_status}; readiness_phase=${readiness_phase}; public_readiness_attempts=${public_readiness_attempts}; management_readiness_attempts=${management_readiness_attempts}; management_ready_streak=${management_ready_streak}; readiness_transport_errors=${readiness_transport_error_count}; readiness_http_5xx=${readiness_http_5xx_count}; litellm=$(container_state "${container_name}"); upstream=$(container_state "${upstream_container_name}"); server_process_started_count=${server_process_started_count}; application_startup_complete_count=${application_startup_complete_count}; application_startup_failure_count=${application_startup_failure_count}; server_shutdown_count=${server_shutdown_count}; connection_error_count=${connection_error_count}; name_resolution_error_count=${name_resolution_error_count}; raw_log_lines_emitted=0; response_bodies_emitted=0" \
    >&2
}

litellm_get_status() {
  local route="$1"
  local output_path="$2"
  local authorization="${3:-}"
  local -a curl_args=(
    --silent
    --noproxy '*'
    --cacert "${workdir}/tls/server.crt"
    --connect-timeout 3
    --max-time 10
    --output "${output_path}"
    --write-out '%{http_code}'
    --request GET
    --header 'Accept: application/json'
  )

  if [ -n "${authorization}" ]; then
    curl_args+=(--header "Authorization: Bearer ${authorization}")
  fi
  curl \
    "${curl_args[@]}" \
    "https://127.0.0.1:4000${route}"
}

record_retryable_readiness_failure() {
  local curl_exit="$1"
  local status="$2"

  if [ "${curl_exit}" -ne 0 ]; then
    readiness_transport_error_count=$((readiness_transport_error_count + 1))
  elif [[ "${status}" == 5?? ]]; then
    readiness_http_5xx_count=$((readiness_http_5xx_count + 1))
  else
    return 1
  fi
}

assert_litellm_container_running_during_readiness() {
  [ "$(container_state "${container_name}")" = "running:0" ] ||
    fail "pinned LiteLLM container exited during ${readiness_phase}"
}

container_post() {
  local route="$1"
  local expected_statuses="$2"
  local authorization_mode="$3"
  local output_path="$4"
  local disconnect_policy="$5"
  local request_profile="$6"

  docker exec --interactive "${container_name}" \
    python -c '
import http.client
import json
import os
import ssl
import sys
import urllib.error
import urllib.request

MAX_INPUT_BYTES = 72 * 1024
MAX_REQUEST_BYTES = 64 * 1024
MAX_RESPONSE_BYTES = 256 * 1024
ALLOWED_ROUTES = {
    "/team/new",
    "/team/member_add",
    "/key/generate",
    "/v1/chat/completions",
}
ALLOWED_STATUSES = {200, 400, 403, 422}


class NoRedirectHandler(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, file_pointer, code, message, headers, new_url):
        return None


def fail(reason):
    print(f"litellm_container_post_failed:{reason}", file=sys.stderr)
    raise SystemExit(1)


def reject_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON key")
        result[key] = value
    return result


def reject_nonfinite(value):
    raise ValueError("non-finite JSON number")


def observer_disconnect_false_conditions(
    exception,
    disconnect_policy,
    route,
    expected_statuses,
    authorization_mode,
    request_profile,
    litellm_version,
    image_identity,
    litellm_source_commit,
    request_json,
):
    conditions = (
        (
            "exception_type",
            type(exception) is http.client.RemoteDisconnected,
        ),
        (
            "disconnect_policy",
            disconnect_policy
            == "pinned_observer_remote_disconnected_v1_93_0",
        ),
        ("route", route == "/v1/chat/completions"),
        ("expected_statuses", expected_statuses == {403}),
        ("authorization_mode", authorization_mode == "stdin_bearer"),
        ("request_profile", request_profile == "observer_route_denial"),
        ("litellm_version", litellm_version == "1.93.0"),
        (
            "image_identity",
            image_identity
            == (
                "ghcr.io/berriai/litellm@"
                "sha256:a1745e629abfb17d434426ff48b115f54f4f4c4a0f5af241de569e93c63c411e"
            ),
        ),
        (
            "litellm_source_commit",
            litellm_source_commit
            == "052b5a2169d8d3082e1d66e69f200a72b0c1e274",
        ),
        (
            "request_shape",
            request_json
            == {
                "model": "xinsight-ci-model",
                "messages": [
                    {
                        "role": "user",
                        "content": "xinsight-privacy-v1-7a91d0-observer-denied-prompt",
                    }
                ],
                "max_tokens": 8,
            },
        ),
    )
    return [name for name, matched in conditions if not matched]


route = sys.argv[1]
try:
    expected_statuses = {int(value) for value in sys.argv[2].split(",")}
except ValueError:
    fail("expected_status")
authorization_mode = sys.argv[3]
disconnect_policy = sys.argv[4]
request_profile = sys.argv[5]
litellm_version = sys.argv[6]
image_identity = sys.argv[7]
litellm_source_commit = sys.argv[8]
if route not in ALLOWED_ROUTES:
    fail("route_not_allowed")
if not expected_statuses or not expected_statuses.issubset(ALLOWED_STATUSES):
    fail("expected_status")
if disconnect_policy not in {
    "strict",
    "pinned_observer_remote_disconnected_v1_93_0",
}:
    fail("disconnect_policy")
if request_profile not in {
    "management_bootstrap",
    "observer_route_denial",
    "fixture_traffic",
}:
    fail("request_profile")

input_bytes = sys.stdin.buffer.read(MAX_INPUT_BYTES + 1)
if not input_bytes or len(input_bytes) > MAX_INPUT_BYTES:
    fail("input_size")
if authorization_mode == "master_env":
    master_key = os.environ.get("LITELLM_MASTER_KEY", "")
    authorization = master_key
    request_body = input_bytes
elif authorization_mode == "stdin_bearer":
    authorization_bytes, separator, request_body = input_bytes.partition(b"\n")
    if not separator:
        fail("authorization_input")
    try:
        authorization = authorization_bytes.decode("utf-8")
    except UnicodeDecodeError:
        fail("authorization_input")
else:
    fail("authorization_mode")

if not request_body or len(request_body) > MAX_REQUEST_BYTES:
    fail("request_size")
try:
    request_json = json.loads(
        request_body.decode("utf-8"),
        object_pairs_hook=reject_duplicate_keys,
        parse_constant=reject_nonfinite,
    )
except (UnicodeDecodeError, ValueError):
    fail("request_json")
if not isinstance(request_json, dict):
    fail("request_shape")

if not authorization or any(
    character.isspace() or ord(character) < 0x20 or ord(character) == 0x7F
    for character in authorization
):
    fail("authorization")

post_request = urllib.request.Request(
    f"https://127.0.0.1:4000{route}",
    data=request_body,
    method="POST",
)
post_request.add_header("Authorization", f"Bearer {authorization}")
post_request.add_header("Content-Type", "application/json")
post_request.add_header("Accept", "application/json")
tls_context = ssl.create_default_context(cafile="/tls/server.crt")
post_opener = urllib.request.build_opener(
    urllib.request.ProxyHandler({}),
    NoRedirectHandler(),
    urllib.request.HTTPSHandler(context=tls_context),
)

try:
    with post_opener.open(post_request, timeout=30) as response:
        if response.status not in expected_statuses:
            fail(f"http_{response.status}")
        if response.headers.get_content_type() != "application/json":
            fail("response_media_type")
        response_body = response.read(MAX_RESPONSE_BYTES + 1)
except urllib.error.HTTPError as exception:
    with exception:
        if exception.code not in expected_statuses:
            fail(f"http_{exception.code}")
        if exception.headers.get_content_type() != "application/json":
            fail("response_media_type")
        response_body = exception.read(MAX_RESPONSE_BYTES + 1)
except http.client.RemoteDisconnected as exception:
    false_conditions = observer_disconnect_false_conditions(
        exception,
        disconnect_policy,
        route,
        expected_statuses,
        authorization_mode,
        request_profile,
        litellm_version,
        image_identity,
        litellm_source_commit,
        request_json,
    )
    if not false_conditions:
        print(
            "litellm_container_post_degraded:"
            "pinned_observer_remote_disconnected_v1_93_0",
            file=sys.stderr,
        )
        raise SystemExit(72)
    fail(
        "RemoteDisconnected:false_conditions="
        + ",".join(false_conditions)
    )
except Exception as exception:
    if type(exception).__name__ == "RemoteDisconnected":
        print(
            "litellm_container_post_failed:RemoteDisconnected:"
            f"exception_module={type(exception).__module__}:"
            f"exception_qualname={type(exception).__qualname__}:"
            "exact_stdlib_match=false",
            file=sys.stderr,
        )
        raise SystemExit(1)
    fail(type(exception).__name__)

if not response_body or len(response_body) > MAX_RESPONSE_BYTES:
    fail("response_size")
try:
    response_json = json.loads(
        response_body.decode("utf-8"),
        object_pairs_hook=reject_duplicate_keys,
        parse_constant=reject_nonfinite,
    )
except (UnicodeDecodeError, ValueError):
    fail("response_json")
if not isinstance(response_json, dict):
    fail("response_shape")

sys.stdout.buffer.write(response_body)
' \
    "${route}" \
    "${expected_statuses}" \
    "${authorization_mode}" \
    "${disconnect_policy}" \
    "${request_profile}" \
    "${litellm_version}" \
    "${image}" \
    "${litellm_upstream_commit}" \
    >"${output_path}"
}

management_post() {
  local route="$1"
  local body="$2"
  local output_path="$3"

  # The seven bootstrap writes deliberately avoid the host-published Docker
  # port. They execute once inside the pinned LiteLLM container while the
  # public/authenticated readiness probes, Connector /spend/logs/v2 GETs and
  # the X worker below continue to prove the host-published HTTPS path.
  if ! printf '%s' "${body}" |
    container_post \
      "${route}" \
      "200" \
      "master_env" \
      "${output_path}" \
      "strict" \
      "management_bootstrap"; then
    fail "LiteLLM setup ${route} failed through container-local HTTPS"
  fi
}

fixture_post() {
  local route="$1"
  local body="$2"
  local output_path="$3"
  local authorization="$4"
  local expected_statuses="$5"
  local disconnect_policy="$6"
  local outcome_path="$7"
  local request_profile="$8"
  local post_status=0

  # These four POSTs only generate deterministic producer-local fixture rows.
  # The dynamic key and body enter docker exec only through stdin; the public
  # Connector GET and worker transport remain on the host-published HTTPS path.
  if printf '%s\n%s' "${authorization}" "${body}" |
    container_post \
      "${route}" \
      "${expected_statuses}" \
      "stdin_bearer" \
      "${output_path}" \
      "${disconnect_policy}" \
      "${request_profile}"; then
    if [ -n "${outcome_path}" ]; then
      [ ! -e "${outcome_path}" ] ||
        fail "observer denial outcome marker already exists"
      printf '%s\n' \
        '{"schema_version":"xinsight.litellm.observer-denial-outcome.v1","mode":"strict_http_403","source_response_present":true}' \
        >"${outcome_path}"
    fi
    return
  else
    post_status=$?
  fi

  if [ "${disconnect_policy}" = "pinned_observer_remote_disconnected_v1_93_0" ] &&
    [ "${route}" = "/v1/chat/completions" ] &&
    [ "${expected_statuses}" = "403" ] &&
    [ "${post_status}" -eq 72 ] &&
    [ -n "${outcome_path}" ]; then
    [ ! -s "${output_path}" ] ||
      fail "degraded observer denial unexpectedly produced source response bytes"
    if [ -e "${output_path}" ]; then
      unlink "${output_path}"
    fi
    [ ! -e "${outcome_path}" ] ||
      fail "observer denial outcome marker already exists"
    printf '%s\n' \
      '{"schema_version":"xinsight.litellm.observer-denial-outcome.v1","mode":"pinned_remote_disconnected","source_response_present":false}' \
      >"${outcome_path}"
  else
    fail "LiteLLM fixture ${route} failed through container-local HTTPS"
  fi
}

assert_observer_boundary_health() {
  [ "$(container_state "${container_name}")" = "running:0" ] ||
    fail "pinned LiteLLM container was not healthy at observer boundary"
  [ "$(container_state "${upstream_container_name}")" = "running:0" ] ||
    fail "deterministic upstream was not healthy at observer boundary"

  docker exec "${container_name}" \
    python -c '
import json
import ssl
import sys
import urllib.request

MAX_RESPONSE_BYTES = 64 * 1024


class NoRedirectHandler(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, file_pointer, code, message, headers, new_url):
        return None


def reject_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON key")
        result[key] = value
    return result


def reject_nonfinite(value):
    raise ValueError("non-finite JSON number")


def get_json(url, tls_context=None):
    handlers = [
        urllib.request.ProxyHandler({}),
        NoRedirectHandler(),
    ]
    if tls_context is not None:
        handlers.append(urllib.request.HTTPSHandler(context=tls_context))
    get_opener = urllib.request.build_opener(*handlers)
    get_request = urllib.request.Request(
        url,
        method="GET",
        headers={"Accept": "application/json"},
    )
    with get_opener.open(get_request, timeout=10) as response:
        if response.status != 200:
            raise ValueError("http_status")
        if response.headers.get_content_type() != "application/json":
            raise ValueError("response_media_type")
        response_body = response.read(MAX_RESPONSE_BYTES + 1)
    if not response_body or len(response_body) > MAX_RESPONSE_BYTES:
        raise ValueError("response_size")
    payload = json.loads(
        response_body.decode("utf-8"),
        object_pairs_hook=reject_duplicate_keys,
        parse_constant=reject_nonfinite,
    )
    if not isinstance(payload, dict):
        raise ValueError("response_shape")
    return payload


try:
    readiness = get_json(
        "https://127.0.0.1:4000/health/readiness",
        ssl.create_default_context(cafile="/tls/server.crt"),
    )
    upstream = get_json("http://xinsight-litellm-upstream:18081/health")
except Exception as exception:
    print(
        f"litellm_post_observer_health_failed:{type(exception).__name__}",
        file=sys.stderr,
    )
    raise SystemExit(1)

if readiness != {"status": "healthy", "db": "connected"}:
    raise SystemExit("litellm_post_observer_health_failed:readiness")
if upstream != {
    "status": "ok",
    "chat_post_count": 0,
    "chat_success_response_started_count": 0,
    "chat_success_response_completed_count": 0,
    "chat_response_status_200_count": 0,
    "chat_response_status_400_count": 0,
    "chat_response_status_401_count": 0,
    "chat_response_status_404_count": 0,
    "chat_response_status_413_count": 0,
    "chat_response_status_422_count": 0,
}:
    raise SystemExit("litellm_post_observer_health_failed:upstream")
' \
    >/dev/null ||
    fail "LiteLLM or deterministic upstream health failed after observer denial"
}

deterministic_upstream_health_metrics() {
  timeout 5s docker exec \
    "${container_name}" \
    python -c '
import json
import urllib.request

FIELDS = (
    "chat_post_count",
    "chat_success_response_started_count",
    "chat_success_response_completed_count",
    "chat_response_status_200_count",
    "chat_response_status_400_count",
    "chat_response_status_401_count",
    "chat_response_status_404_count",
    "chat_response_status_413_count",
    "chat_response_status_422_count",
)
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
request = urllib.request.Request(
    "http://xinsight-litellm-upstream:18081/health",
    method="GET",
    headers={"Accept": "application/json"},
)
with opener.open(request, timeout=2) as response:
    if response.status != 200:
        raise SystemExit(1)
    if response.headers.get_content_type() != "application/json":
        raise SystemExit(1)
    response_body = response.read(4097)
if not response_body or len(response_body) > 4096:
    raise SystemExit(1)
payload = json.loads(response_body.decode("utf-8"))
if (
    not isinstance(payload, dict)
    or payload.get("status") != "ok"
    or set(payload) != {"status", *FIELDS}
    or any(type(payload.get(field)) is not int for field in FIELDS)
    or any(payload[field] < 0 for field in FIELDS)
):
    raise SystemExit(1)
print("|".join(str(payload[field]) for field in FIELDS))
'
}

deterministic_upstream_chat_post_count() {
  local metrics
  metrics="$(deterministic_upstream_health_metrics)"
  printf '%s\n' "${metrics%%|*}"
}

observer_allowed_empty_get() {
  local output_path="$1"
  local authorization="$2"

  if ! printf '%s' "${authorization}" |
    docker exec --interactive "${container_name}" \
      python -c '
import json
import ssl
import sys
import urllib.request

MAX_AUTHORIZATION_BYTES = 4096
MAX_RESPONSE_BYTES = 256 * 1024
ROUTE = (
    "/spend/logs/v2"
    "?start_date=2020-01-01%2000%3A00%3A00"
    "&end_date=2030-01-01%2000%3A00%3A00"
    "&page=1&page_size=100&sort_by=startTime&sort_order=asc"
)


class NoRedirectHandler(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, file_pointer, code, message, headers, new_url):
        return None


def fail(reason):
    print(f"litellm_observer_allowed_get_failed:{reason}", file=sys.stderr)
    raise SystemExit(1)


def reject_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON key")
        result[key] = value
    return result


def reject_nonfinite(value):
    raise ValueError("non-finite JSON number")


authorization_bytes = sys.stdin.buffer.read(MAX_AUTHORIZATION_BYTES + 1)
if not authorization_bytes or len(authorization_bytes) > MAX_AUTHORIZATION_BYTES:
    fail("authorization_size")
try:
    authorization = authorization_bytes.decode("utf-8")
except UnicodeDecodeError:
    fail("authorization")
if any(
    character.isspace() or ord(character) < 0x20 or ord(character) == 0x7F
    for character in authorization
):
    fail("authorization")

get_request = urllib.request.Request(
    f"https://127.0.0.1:4000{ROUTE}",
    method="GET",
    headers={
        "Accept": "application/json",
        "Authorization": f"Bearer {authorization}",
    },
)
get_opener = urllib.request.build_opener(
    urllib.request.ProxyHandler({}),
    NoRedirectHandler(),
    urllib.request.HTTPSHandler(
        context=ssl.create_default_context(cafile="/tls/server.crt")
    ),
)
try:
    with get_opener.open(get_request, timeout=30) as response:
        if response.status != 200:
            fail(f"http_{response.status}")
        if response.headers.get_content_type() != "application/json":
            fail("response_media_type")
        response_body = response.read(MAX_RESPONSE_BYTES + 1)
except Exception as exception:
    fail(type(exception).__name__)
if not response_body or len(response_body) > MAX_RESPONSE_BYTES:
    fail("response_size")
try:
    response_json = json.loads(
        response_body.decode("utf-8"),
        object_pairs_hook=reject_duplicate_keys,
        parse_constant=reject_nonfinite,
    )
except (UnicodeDecodeError, ValueError):
    fail("response_json")
if not isinstance(response_json, dict):
    fail("response_shape")
sys.stdout.buffer.write(response_body)
' \
      >"${output_path}"; then
    fail "route-scoped observer could not read the empty /spend/logs/v2 state"
  fi
}

cleanup() {
  local exit_code="$1"
  local cleanup_failed=0

  trap - EXIT
  if [ "${exit_code}" -ne 0 ]; then
    emit_failure_diagnostics "${exit_code}"
  fi
  docker rm --force "${container_name}" >/dev/null 2>&1 || true
  docker rm --force "${upstream_container_name}" >/dev/null 2>&1 || true
  if docker network inspect "${network_name}" >/dev/null 2>&1; then
    docker network rm "${network_name}" >/dev/null 2>&1 || cleanup_failed=1
  fi
  if [ "${exit_code}" -eq 0 ] && [ "${cleanup_failed}" -ne 0 ]; then
    echo "LiteLLM component integration: failed to remove exact test network" >&2
    exit 1
  fi
  exit "${exit_code}"
}
trap 'cleanup $?' EXIT

actual_source_database="$(
  psql "${source_postgres_dsn}" \
    --no-psqlrc \
    --tuples-only \
    --no-align \
    --set ON_ERROR_STOP=1 \
    --command 'SELECT current_database()'
)"
[ "${actual_source_database}" = "${source_database}" ] ||
  fail "Source DSN resolved to ${actual_source_database}, want ${source_database}"
if psql "${source_postgres_dsn}" \
  --no-psqlrc \
  --tuples-only \
  --no-align \
  --set ON_ERROR_STOP=1 \
  --command "SELECT 1 FROM pg_database WHERE datname = '${consumer_database}'" |
  grep -q '^1$'; then
  fail "Consumer database already exists; component job requires a fresh two-database boundary"
fi
createdb \
  --maintenance-db="${source_postgres_dsn}" \
  --owner=xinsight_admin \
  "${consumer_database}"
actual_consumer_database="$(
  psql "${consumer_postgres_dsn}" \
    --no-psqlrc \
    --tuples-only \
    --no-align \
    --set ON_ERROR_STOP=1 \
    --command 'SELECT current_database()'
)"
[ "${actual_consumer_database}" = "${consumer_database}" ] ||
  fail "Consumer DSN resolved to ${actual_consumer_database}, want ${consumer_database}"
[ "${actual_source_database}" != "${actual_consumer_database}" ] ||
  fail "Source and Consumer DSNs resolve to the same database"

mkdir -p "${workdir}/tls"
openssl req \
  -x509 \
  -newkey rsa:2048 \
  -nodes \
  -days 1 \
  -subj "/CN=127.0.0.1" \
  -addext "subjectAltName=IP:127.0.0.1" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,keyCertSign,digitalSignature,keyEncipherment" \
  -addext "extendedKeyUsage=serverAuth" \
  -keyout "${workdir}/tls/server.key" \
  -out "${workdir}/tls/server.crt" \
  >/dev/null 2>&1
chmod 0600 "${workdir}/tls/server.key"
chmod 0644 "${workdir}/tls/server.crt"

# A host-only 127.0.0.1 listener is a different loopback namespace and cannot
# be reached through host.docker.internal. Reuse the exact pinned LiteLLM
# image's Python runtime on a dedicated bridge instead; the upstream publishes
# no host port and is reachable only by its run-unique network peers.
docker pull "${image}" >/dev/null
docker network create \
  --driver bridge \
  "${network_name}" \
  >/dev/null
docker run \
  --detach \
  --name "${upstream_container_name}" \
  --network "${network_name}" \
  --network-alias "${upstream_network_alias}" \
  --entrypoint python \
  --volume "${root}/scripts/ci/litellm-component-upstream.py:/ci/litellm-component-upstream.py:ro" \
  "${image}" \
  /ci/litellm-component-upstream.py \
  --host 0.0.0.0 \
  --port 18081 \
  >/dev/null
for _ in $(seq 1 30); do
  if docker exec \
    "${upstream_container_name}" \
    python -c \
      'import urllib.request; response = urllib.request.urlopen("http://127.0.0.1:18081/health", timeout=2); raise SystemExit(0 if response.status == 200 else 1)' \
    >/dev/null 2>&1; then
    break
  fi
  if [ "$(container_state "${upstream_container_name}")" != "running:0" ]; then
    fail "deterministic upstream container exited before readiness"
  fi
  sleep 1
done
docker exec \
  "${upstream_container_name}" \
  python -c \
    'import urllib.request; response = urllib.request.urlopen("http://127.0.0.1:18081/health", timeout=2); raise SystemExit(0 if response.status == 200 else 1)' \
  >/dev/null 2>&1 ||
  fail "deterministic upstream did not become ready"

docker run \
  --detach \
  --name "${container_name}" \
  --network "${network_name}" \
  --add-host host.docker.internal:host-gateway \
  --publish 127.0.0.1:4000:4000 \
  --env "DATABASE_URL=${source_postgres_dsn/localhost/host.docker.internal}" \
  --env "DIRECT_URL=${source_postgres_dsn/localhost/host.docker.internal}" \
  --env "LITELLM_MASTER_KEY=${master_key}" \
  --env "STORE_MODEL_IN_DB=True" \
  --volume "${root}/scripts/ci/litellm-component-config.yaml:/app/config.yaml:ro" \
  --volume "${workdir}/tls:/tls:ro" \
  "${image}" \
  --config=/app/config.yaml \
  --host=0.0.0.0 \
  --port=4000 \
  --ssl_keyfile_path=/tls/server.key \
  --ssl_certfile_path=/tls/server.crt \
  --enforce_prisma_migration_check \
  >/dev/null

readiness_phase="public_db_readiness"
public_readiness_ready="false"
for attempt in $(seq 1 120); do
  public_readiness_attempts="${attempt}"
  public_readiness_status=""
  if public_readiness_status="$(
    litellm_get_status \
      "/health/readiness" \
      "${workdir}/public-readiness.json"
  )"; then
    public_readiness_curl_exit=0
  else
    public_readiness_curl_exit=$?
  fi

  if [ "${public_readiness_curl_exit}" -eq 0 ] &&
    [ "${public_readiness_status}" = "200" ]; then
    jq --exit-status \
      '
        type == "object"
        and length == 2
        and .status == "healthy"
        and .db == "connected"
      ' \
      "${workdir}/public-readiness.json" \
      >/dev/null ||
      fail "public readiness returned an unexpected HTTP 200 body"
    public_readiness_ready="true"
    break
  fi

  record_retryable_readiness_failure \
    "${public_readiness_curl_exit}" \
    "${public_readiness_status}" ||
    fail "public readiness returned HTTP ${public_readiness_status:-unknown}"
  assert_litellm_container_running_during_readiness
  sleep 1
done
[ "${public_readiness_ready}" = "true" ] ||
  fail "public database readiness did not converge after ${public_readiness_attempts} probes"

# A process-alive or DB-only probe is insufficient for the first management
# write. Require two consecutive, one-second-separated observations that both
# authenticate through the pinned master key and execute a read-only Team
# management query against the fresh LiteLLM database. No setup POST is retried.
readiness_phase="authenticated_management_readiness"
for attempt in $(seq 1 60); do
  management_readiness_attempts="${attempt}"
  authenticated_readiness_status=""
  if authenticated_readiness_status="$(
    litellm_get_status \
      "/health/readiness/details" \
      "${workdir}/authenticated-readiness.json" \
      "${master_key}"
  )"; then
    authenticated_readiness_curl_exit=0
  else
    authenticated_readiness_curl_exit=$?
  fi

  if [ "${authenticated_readiness_curl_exit}" -eq 0 ] &&
    [ "${authenticated_readiness_status}" = "200" ]; then
    jq --exit-status \
      --arg version "${litellm_version}" \
      '
        type == "object"
        and .status == "healthy"
        and .db == "connected"
        and .litellm_version == $version
      ' \
      "${workdir}/authenticated-readiness.json" \
      >/dev/null ||
      fail "authenticated readiness returned an unexpected HTTP 200 body"
  else
    management_ready_streak=0
    record_retryable_readiness_failure \
      "${authenticated_readiness_curl_exit}" \
      "${authenticated_readiness_status}" ||
      fail "authenticated readiness returned HTTP ${authenticated_readiness_status:-unknown}"
    assert_litellm_container_running_during_readiness
    sleep 1
    continue
  fi

  team_list_status=""
  if team_list_status="$(
    litellm_get_status \
      "/team/list" \
      "${workdir}/management-readiness-team-list.json" \
      "${master_key}"
  )"; then
    team_list_curl_exit=0
  else
    team_list_curl_exit=$?
  fi

  if [ "${team_list_curl_exit}" -eq 0 ] &&
    [ "${team_list_status}" = "200" ]; then
    jq --exit-status \
      'type == "array" and length == 0' \
      "${workdir}/management-readiness-team-list.json" \
      >/dev/null ||
      fail "read-only Team management readiness did not return the fresh empty state"
    management_ready_streak=$((management_ready_streak + 1))
    if [ "${management_ready_streak}" -eq 2 ]; then
      break
    fi
  else
    management_ready_streak=0
    record_retryable_readiness_failure \
      "${team_list_curl_exit}" \
      "${team_list_status}" ||
      fail "read-only Team management readiness returned HTTP ${team_list_status:-unknown}"
  fi

  assert_litellm_container_running_during_readiness
  sleep 1
done
[ "${management_ready_streak}" -eq 2 ] ||
  fail "authenticated management readiness did not remain stable after ${management_readiness_attempts} probes"
readiness_phase="complete"
readiness_persistent_state_counts="$(
  psql "${source_postgres_dsn}" \
    --no-psqlrc \
    --tuples-only \
    --no-align \
    --set ON_ERROR_STOP=1 \
    --command '
      SELECT
        (SELECT count(*) FROM "LiteLLM_TeamTable"),
        (SELECT count(*) FROM "LiteLLM_TeamMembership"),
        (SELECT count(*) FROM "LiteLLM_VerificationToken"),
        (SELECT count(*) FROM "LiteLLM_AuditLog")
    '
)"
[ "${readiness_persistent_state_counts}" = "0|0|0|0" ] ||
  fail "read-only management readiness created persistent team, membership, key, or audit state"
installed_version="$(
  docker exec "${container_name}" \
    python -c 'from importlib.metadata import version; print(version("litellm"))'
)"
[ "${installed_version}" = "${litellm_version}" ] ||
  fail "pinned image reports LiteLLM ${installed_version}, want ${litellm_version}"
docker exec \
  "${container_name}" \
  python -c \
    'import urllib.request; response = urllib.request.urlopen("http://xinsight-litellm-upstream:18081/health", timeout=2); raise SystemExit(0 if response.status == 200 else 1)' \
  >/dev/null 2>&1 ||
  fail "pinned LiteLLM container cannot reach the private deterministic upstream alias"

source_table_count="$(
  psql "${source_postgres_dsn}" \
    --no-psqlrc \
    --tuples-only \
    --no-align \
    --set ON_ERROR_STOP=1 \
    --command "
      SELECT count(*)
      FROM information_schema.tables
      WHERE table_schema = 'public'
    "
)"
[ "${source_table_count}" -gt 0 ] ||
  fail "Source database contains no LiteLLM tables after readiness"
consumer_table_count="$(
  psql "${consumer_postgres_dsn}" \
    --no-psqlrc \
    --tuples-only \
    --no-align \
    --set ON_ERROR_STOP=1 \
    --command "
      SELECT count(*)
      FROM information_schema.tables
      WHERE table_schema = 'public'
    "
)"
[ "${consumer_table_count}" = "0" ] ||
  fail "Consumer database is not fresh before X-Insight migrations"
echo \
  "LiteLLM component database isolation: source=${source_database} tables=${source_table_count}; consumer=${consumer_database} pre_migration_tables=0; source_access=http_spend_logs_v2_only"

# The master key is bootstrap-only. It creates two isolated teams, their
# non-admin members, and three route-scoped virtual keys. It is never used for
# model traffic or spend-log reads.
management_post \
  "/team/new" \
  "{\"team_id\":\"${authorized_team_id}\",\"team_alias\":\"${authorized_team_id}\",\"models\":[\"xinsight-ci-model\"],\"team_member_permissions\":[\"/spend/logs\"]}" \
  "${workdir}/authorized-team.json"
management_post \
  "/team/member_add" \
  "{\"team_id\":\"${authorized_team_id}\",\"member\":[{\"role\":\"user\",\"user_id\":\"${authorized_traffic_user_id}\"},{\"role\":\"user\",\"user_id\":\"${observer_user_id}\"}]}" \
  "${workdir}/authorized-team-members.json"
management_post \
  "/team/new" \
  "{\"team_id\":\"${unauthorized_team_id}\",\"team_alias\":\"${unauthorized_team_id}\",\"models\":[\"xinsight-ci-model\"],\"team_member_permissions\":[]}" \
  "${workdir}/unauthorized-team.json"
management_post \
  "/team/member_add" \
  "{\"team_id\":\"${unauthorized_team_id}\",\"member\":{\"role\":\"user\",\"user_id\":\"${unauthorized_traffic_user_id}\"}}" \
  "${workdir}/unauthorized-team-member.json"

management_post \
  "/key/generate" \
  "{\"key_alias\":\"xinsight-component-authorized-traffic\",\"team_id\":\"${authorized_team_id}\",\"user_id\":\"${authorized_traffic_user_id}\",\"models\":[\"xinsight-ci-model\"],\"allowed_routes\":[\"/v1/chat/completions\"]}" \
  "${workdir}/authorized-traffic-key.json"
management_post \
  "/key/generate" \
  "{\"key_alias\":\"xinsight-component-observer\",\"team_id\":\"${authorized_team_id}\",\"user_id\":\"${observer_user_id}\",\"models\":[],\"allowed_routes\":[\"/spend/logs/v2\"]}" \
  "${workdir}/observer-key.json"
management_post \
  "/key/generate" \
  "{\"key_alias\":\"xinsight-component-unauthorized-traffic\",\"team_id\":\"${unauthorized_team_id}\",\"user_id\":\"${unauthorized_traffic_user_id}\",\"models\":[\"xinsight-ci-model\"],\"allowed_routes\":[\"/v1/chat/completions\"]}" \
  "${workdir}/unauthorized-traffic-key.json"

authorized_traffic_key="$(
  jq --exit-status --raw-output \
    --arg team "${authorized_team_id}" \
    'select(.team_id == $team and .allowed_routes == ["/v1/chat/completions"]) | .key | select(type == "string" and startswith("sk-"))' \
    "${workdir}/authorized-traffic-key.json"
)"
observer_key="$(
  jq --exit-status --raw-output \
    --arg team "${authorized_team_id}" \
    'select(.team_id == $team and .allowed_routes == ["/spend/logs/v2"]) | .key | select(type == "string" and startswith("sk-"))' \
    "${workdir}/observer-key.json"
)"
unauthorized_traffic_key="$(
  jq --exit-status --raw-output \
    --arg team "${unauthorized_team_id}" \
    'select(.team_id == $team and .allowed_routes == ["/v1/chat/completions"]) | .key | select(type == "string" and startswith("sk-"))' \
    "${workdir}/unauthorized-traffic-key.json"
)"
[ "${authorized_traffic_key}" != "${observer_key}" ] ||
  fail "authorized traffic and observer keys are not distinct"
[ "${authorized_traffic_key}" != "${unauthorized_traffic_key}" ] ||
  fail "authorized and unauthorized team traffic keys are not distinct"
[ "${observer_key}" != "${unauthorized_traffic_key}" ] ||
  fail "observer and unauthorized team traffic keys are not distinct"
observer_denial_authorization="${observer_key}"

litellm_observer_pid_before="$(
  docker inspect --format '{{.State.Pid}}' "${container_name}"
)"
upstream_observer_pid_before="$(
  docker inspect --format '{{.State.Pid}}' "${upstream_container_name}"
)"
[[ "${litellm_observer_pid_before}" =~ ^[1-9][0-9]*$ ]] ||
  fail "pinned LiteLLM container had no process before observer denial"
[[ "${upstream_observer_pid_before}" =~ ^[1-9][0-9]*$ ]] ||
  fail "deterministic upstream had no process before observer denial"
assert_observer_boundary_health
observer_upstream_chat_post_count_before="$(
  deterministic_upstream_chat_post_count
)"
[ "${observer_upstream_chat_post_count_before}" = "0" ] ||
  fail "deterministic upstream was not fresh before observer denial"
observer_source_spend_count_before="$(
  psql "${source_postgres_dsn}" \
    --no-psqlrc \
    --tuples-only \
    --no-align \
    --set ON_ERROR_STOP=1 \
    --command 'SELECT count(*) FROM "LiteLLM_SpendLogs"'
)"
[ "${observer_source_spend_count_before}" = "0" ] ||
  fail "LiteLLM Source was not fresh before observer denial"

# One attempt only. The exact observer request is deliberately placed after
# the no-forward/no-persistence baselines and before every producer fixture.
set_fixture_stage "observer_denial"
fixture_post \
  "/v1/chat/completions" \
  "{\"model\":\"xinsight-ci-model\",\"messages\":[{\"role\":\"user\",\"content\":\"${observer_denied_prompt}\"}],\"max_tokens\":8}" \
  "${workdir}/observer-denied-source-response.json" \
  "${observer_denial_authorization}" \
  "403" \
  "pinned_observer_remote_disconnected_v1_93_0" \
  "${workdir}/observer-denied-outcome.json" \
  "observer_route_denial"

set_fixture_stage "observer_postcheck"
assert_observer_boundary_health
[ "$(
  docker inspect --format '{{.State.Pid}}' "${container_name}"
)" = "${litellm_observer_pid_before}" ] ||
  fail "pinned LiteLLM container restarted across observer denial"
[ "$(
  docker inspect --format '{{.State.Pid}}' "${upstream_container_name}"
)" = "${upstream_observer_pid_before}" ] ||
  fail "deterministic upstream restarted across observer denial"
observer_upstream_chat_post_count_after="$(
  deterministic_upstream_chat_post_count
)"
[ "${observer_upstream_chat_post_count_after}" = "${observer_upstream_chat_post_count_before}" ] ||
  fail "observer denial reached the deterministic upstream"
observer_allowed_empty_get \
  "${workdir}/observer-empty-spend-logs-v2-response.json" \
  "${observer_denial_authorization}"
jq --exit-status \
  '
    type == "object"
    and .total == 0
    and (.data | type == "array" and length == 0)
  ' \
  "${workdir}/observer-empty-spend-logs-v2-response.json" \
  >/dev/null ||
  fail "route-scoped observer did not read the exact empty /spend/logs/v2 state"
observer_source_spend_count="$(
  psql "${source_postgres_dsn}" \
    --no-psqlrc \
    --tuples-only \
    --no-align \
    --set ON_ERROR_STOP=1 \
    --command 'SELECT count(*) FROM "LiteLLM_SpendLogs"'
)"
[ "${observer_source_spend_count}" = "${observer_source_spend_count_before}" ] ||
  fail "observer denial created a LiteLLM Source spend-log row"
observer_denial_mode="$(
  jq --exit-status --raw-output \
    '
      select(
        type == "object"
        and .schema_version == "xinsight.litellm.observer-denial-outcome.v1"
        and (
          (
            .mode == "strict_http_403"
            and .source_response_present == true
          )
          or (
            .mode == "pinned_remote_disconnected"
            and .source_response_present == false
          )
        )
      )
      | .mode
    ' \
    "${workdir}/observer-denied-outcome.json"
)"
[ -n "${observer_denial_mode}" ] ||
  fail "observer denial outcome marker is invalid"
set_fixture_stage "authorized_success"
fixture_post \
  "/v1/chat/completions" \
  "{\"model\":\"xinsight-ci-model\",\"messages\":[{\"role\":\"user\",\"content\":\"${success_prompt}\"}],\"max_tokens\":8}" \
  "${workdir}/success-response.json" \
  "${authorized_traffic_key}" \
  "200" \
  "strict" \
  "" \
  "fixture_traffic"
set_fixture_stage "authorized_second_success"
fixture_post \
  "/v1/chat/completions" \
  "{\"model\":\"xinsight-ci-model\",\"messages\":[{\"role\":\"user\",\"content\":\"${second_success_prompt}\"}],\"max_tokens\":8}" \
  "${workdir}/second-success-response.json" \
  "${authorized_traffic_key}" \
  "200" \
  "strict" \
  "" \
  "fixture_traffic"
set_fixture_stage "authorized_controlled_failure"
fixture_post \
  "/v1/chat/completions" \
  "{\"model\":\"xinsight-ci-model\",\"messages\":[{\"role\":\"user\",\"content\":\"${failure_prompt}\"}],\"max_tokens\":8}" \
  "${workdir}/failure-response.json" \
  "${authorized_traffic_key}" \
  "400" \
  "strict" \
  "" \
  "fixture_traffic"
set_fixture_stage "unauthorized_team_success"
fixture_post \
  "/v1/chat/completions" \
  "{\"model\":\"xinsight-ci-model\",\"messages\":[{\"role\":\"user\",\"content\":\"${unauthorized_team_prompt}\"}],\"max_tokens\":8}" \
  "${workdir}/unauthorized-team-response.json" \
  "${unauthorized_traffic_key}" \
  "200" \
  "strict" \
  "" \
  "fixture_traffic"

set_fixture_stage "connector_convergence"
for _ in $(seq 1 60); do
  source_spend_count="$(
    psql "${source_postgres_dsn}" \
      --no-psqlrc \
      --tuples-only \
      --no-align \
      --set ON_ERROR_STOP=1 \
      --command 'SELECT count(*) FROM "LiteLLM_SpendLogs"'
  )"
  if [ "${source_spend_count}" = "4" ]; then
    break
  fi
  [ "${source_spend_count}" -le 4 ] ||
    fail "LiteLLM Source persisted ${source_spend_count} rows, want 4"
  sleep 1
done
[ "${source_spend_count}" = "4" ] ||
  fail "LiteLLM Source persisted ${source_spend_count} rows, want 4"
upstream_health_metrics="$(deterministic_upstream_health_metrics)"
upstream_chat_post_count="${upstream_health_metrics%%|*}"
[ "${upstream_chat_post_count}" = "4" ] ||
  fail "deterministic upstream observed ${upstream_chat_post_count} chat POSTs, want 4"
[ "${upstream_health_metrics}" = "4|3|3|3|1|0|0|0|0" ] ||
  fail "deterministic upstream response counters did not converge"
source_team_counts="$(
  psql "${source_postgres_dsn}" \
    --no-psqlrc \
    --tuples-only \
    --no-align \
    --set ON_ERROR_STOP=1 \
    --command "
      SELECT
        count(*) FILTER (WHERE team_id = '${authorized_team_id}'),
        count(*) FILTER (WHERE team_id = '${unauthorized_team_id}'),
        count(*) FILTER (
          WHERE team_id IS NULL
             OR team_id NOT IN ('${authorized_team_id}', '${unauthorized_team_id}')
        )
      FROM \"LiteLLM_SpendLogs\"
    "
)"
[ "${source_team_counts}" = "3|1|0" ] ||
  fail "LiteLLM Source team row counts=${source_team_counts}, want 3|1|0"

observer_spend_status="$(
  curl \
    --silent \
    --show-error \
    --noproxy '*' \
    --cacert "${workdir}/tls/server.crt" \
    --output "${workdir}/observer-spend-logs-v2-response.json" \
    --write-out '%{http_code}' \
    --get \
    --header "Authorization: Bearer ${observer_key}" \
    --header 'Accept: application/json' \
    --data-urlencode 'start_date=2020-01-01 00:00:00' \
    --data-urlencode 'end_date=2030-01-01 00:00:00' \
    --data-urlencode 'page=1' \
    --data-urlencode 'page_size=100' \
    --data-urlencode 'sort_by=startTime' \
    --data-urlencode 'sort_order=asc' \
    https://127.0.0.1:4000/spend/logs/v2
)"
[ "${observer_spend_status}" = "200" ] ||
  fail "route-scoped observer /spend/logs/v2 returned HTTP ${observer_spend_status}"
jq --exit-status \
  --arg authorized_team "${authorized_team_id}" \
  --arg unauthorized_team "${unauthorized_team_id}" \
  '
    .total == 3
    and (.data | type == "array" and length == 3)
    and ([.data[].team_id] | all(. == $authorized_team))
    and ([.data[].team_id] | any(. == $unauthorized_team) | not)
  ' \
  "${workdir}/observer-spend-logs-v2-response.json" \
  >/dev/null ||
  fail "observer spend-log response did not contain exactly the three authorized-team rows"

cd "${root}"
env \
  -u LITELLM_COMPONENT_SOURCE_POSTGRES_DSN \
  SSL_CERT_FILE="${workdir}/tls/server.crt" \
  XINSIGHT_LITELLM_COMPONENT_ENDPOINT="https://127.0.0.1:4000/spend/logs/v2" \
  XINSIGHT_LITELLM_COMPONENT_API_KEY="${observer_key}" \
  XINSIGHT_LITELLM_COMPONENT_SOURCE_DATABASE="${source_database}" \
  XINSIGHT_LITELLM_COMPONENT_CONSUMER_DATABASE="${consumer_database}" \
  XINSIGHT_LITELLM_COMPONENT_SOURCE_ACCESS="http_spend_logs_v2_only" \
  XINSIGHT_LITELLM_COMPONENT_SUCCESS_PROMPT="${success_prompt}" \
  XINSIGHT_LITELLM_COMPONENT_SECOND_SUCCESS_PROMPT="${second_success_prompt}" \
  XINSIGHT_LITELLM_COMPONENT_FAILURE_PROMPT="${failure_prompt}" \
  XINSIGHT_LITELLM_COMPONENT_UNAUTHORIZED_TEAM_PROMPT="${unauthorized_team_prompt}" \
  XINSIGHT_LITELLM_COMPONENT_OBSERVER_DENIED_PROMPT="${observer_denied_prompt}" \
  XINSIGHT_LITELLM_COMPONENT_SUCCESS_RESPONSE="${success_response}" \
  XINSIGHT_LITELLM_COMPONENT_AUTHORIZED_TRAFFIC_KEY_SENTINEL="${authorized_traffic_key}" \
  XINSIGHT_LITELLM_COMPONENT_OBSERVER_KEY_SENTINEL="${observer_key}" \
  XINSIGHT_LITELLM_COMPONENT_UNAUTHORIZED_TRAFFIC_KEY_SENTINEL="${unauthorized_traffic_key}" \
  go test \
  ./internal/integration/litellmfeed \
  -run '^TestLiteLLMComponentIntegration$' \
  -count=1

spend_status="$(
  curl \
    --silent \
    --show-error \
    --noproxy '*' \
    --cacert "${workdir}/tls/server.crt" \
    --output "${workdir}/spend-logs-v2-response.json" \
    --write-out '%{http_code}' \
    --get \
    --header "Authorization: Bearer ${observer_key}" \
    --header 'Accept: application/json' \
    --data-urlencode 'start_date=2020-01-01 00:00:00' \
    --data-urlencode 'end_date=2030-01-01 00:00:00' \
    --data-urlencode 'page=1' \
    --data-urlencode 'page_size=100' \
    --data-urlencode 'sort_by=startTime' \
    --data-urlencode 'sort_order=asc' \
    https://127.0.0.1:4000/spend/logs/v2
)"
[ "${spend_status}" = "200" ] ||
  fail "privacy verification /spend/logs/v2 returned HTTP ${spend_status}"

"${pg_dump_bin}" \
  "${source_postgres_dsn}" \
  --data-only \
  --no-owner \
  --no-privileges \
  --file="${workdir}/source-database-data.sql"
capture_container_logs ||
  fail "failed to capture bounded container logs for privacy verification"

assert_privacy_sentinels_absent \
  "${workdir}/source-database-data.sql" \
  "LiteLLM Source database"
assert_privacy_sentinels_absent \
  "${workdir}/litellm-container.log" \
  "LiteLLM container logs"
assert_privacy_sentinels_absent \
  "${workdir}/upstream-container.log" \
  "deterministic upstream container logs"
assert_privacy_sentinels_absent \
  "${workdir}/spend-logs-v2-response.json" \
  "LiteLLM /spend/logs/v2 response"
assert_privacy_sentinels_absent \
  "${workdir}/observer-denied-outcome.json" \
  "LiteLLM observer route-denial outcome marker"
assert_privacy_sentinels_absent \
  "${workdir}/observer-empty-spend-logs-v2-response.json" \
  "LiteLLM observer empty spend-log response"
if [ -f "${workdir}/observer-denied-source-response.json" ]; then
  assert_privacy_sentinels_absent \
    "${workdir}/observer-denied-source-response.json" \
    "LiteLLM observer route-denial source response"
fi

set_fixture_stage "complete"
echo "LiteLLM component privacy: unique request/response/raw-key sentinels absent from Source DB, container logs, /spend/logs/v2, and X Evidence"
echo "LiteLLM component least privilege: master=setup-only; traffic=team-scoped LLM key; observer=team member + exact /spend/logs/v2 route; fixed-version effective denial, zero forwarding, zero persistence, outcome=${observer_denial_mode}; other-team row invisible"
echo "LiteLLM component integration: pinned ${litellm_version}/${litellm_upstream_commit}, isolated Source/Consumer databases, HTTP-only least-privilege Source read, deterministic outcomes with per-call unique success IDs, and X-Insight convergence PASS"
