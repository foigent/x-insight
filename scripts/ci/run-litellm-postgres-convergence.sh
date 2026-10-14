#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "LiteLLM PostgreSQL convergence: $*" >&2
  exit 1
}

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
admin_dsn="${XINSIGHT_POSTGRES_DSN:?XINSIGHT_POSTGRES_DSN is required}"
litellmfeed_database="xinsight_protocol_litellmfeed"
registry_database="xinsight_protocol_registry"
litellmfeed_created=false
registry_created=false

case "${admin_dsn}" in
  postgres://* | postgresql://*)
    ;;
  *)
    fail "admin DSN must be a PostgreSQL URL"
    ;;
esac
case "${admin_dsn}" in
  *'#'*)
    fail "admin DSN must not contain a fragment"
    ;;
esac

dsn_without_query="${admin_dsn%%\?*}"
if [ "${dsn_without_query}" = "${admin_dsn}" ]; then
  dsn_query=""
else
  dsn_query="?${admin_dsn#*\?}"
fi
admin_database="${dsn_without_query##*/}"
[ "${admin_database}" = "xinsight_protocol" ] ||
  fail "admin DSN must resolve to the dedicated xinsight_protocol database"
dsn_server="${dsn_without_query%/*}"
litellmfeed_dsn="${dsn_server}/${litellmfeed_database}${dsn_query}"
registry_dsn="${dsn_server}/${registry_database}${dsn_query}"

database_count() {
  local database_name="$1"
  case "${database_name}" in
    "${litellmfeed_database}" | "${registry_database}")
      ;;
    *)
      fail "database count target is outside the exact isolated set"
      ;;
  esac
  psql \
    "${admin_dsn}" \
    --no-psqlrc \
    --tuples-only \
    --no-align \
    --set ON_ERROR_STOP=1 \
    --command "SELECT count(*) FROM pg_database WHERE datname = '${database_name}'"
}

cleanup() {
  local exit_code="$1"
  local cleanup_failed=0

  trap - EXIT
  if [ "${registry_created}" = "true" ]; then
    dropdb \
      --maintenance-db="${admin_dsn}" \
      "${registry_database}" ||
      cleanup_failed=1
  fi
  if [ "${litellmfeed_created}" = "true" ]; then
    dropdb \
      --maintenance-db="${admin_dsn}" \
      "${litellmfeed_database}" ||
      cleanup_failed=1
  fi
  if [ "$(database_count "${registry_database}")" != "0" ] ||
    [ "$(database_count "${litellmfeed_database}")" != "0" ]; then
    cleanup_failed=1
  fi
  if [ "${cleanup_failed}" -ne 0 ]; then
    echo "LiteLLM PostgreSQL convergence: exact test database cleanup failed" >&2
  fi
  if [ "${exit_code}" -eq 0 ] && [ "${cleanup_failed}" -ne 0 ]; then
    exit 1
  fi
  exit "${exit_code}"
}
trap 'cleanup $?' EXIT

[ "${litellmfeed_database}" != "${registry_database}" ] ||
  fail "package databases must be distinct"
[ "$(database_count "${litellmfeed_database}")" = "0" ] ||
  fail "${litellmfeed_database} already exists"
[ "$(database_count "${registry_database}")" = "0" ] ||
  fail "${registry_database} already exists"

createdb \
  --maintenance-db="${admin_dsn}" \
  --owner=xinsight_admin \
  "${litellmfeed_database}"
litellmfeed_created=true
createdb \
  --maintenance-db="${admin_dsn}" \
  --owner=xinsight_admin \
  "${registry_database}"
registry_created=true

cd "${root}"
env -u XINSIGHT_POSTGRES_DSN \
  go test \
  ./internal/adapters/litellm \
  -run '^(TestLiteLLM|TestCanonicalReportedDecimal|TestSanitizeUpstreamRow|TestDecodeSourceRecord)' \
  -count=1

# These packages intentionally run concurrently to retain the original CI
# concurrency pressure while proving that migration/maintenance locks and
# product registry rows cannot cross package database boundaries.
set +e
env \
  XINSIGHT_POSTGRES_DSN="${litellmfeed_dsn}" \
  go test \
  ./internal/integration/litellmfeed \
  -run '^(TestActivationCheckpoint|TestSelectWindow|TestFetchCompleteWindow|TestWorker|TestPostgresLiteLLM)' \
  -count=1 &
litellmfeed_pid="$!"
env \
  XINSIGHT_POSTGRES_DSN="${registry_dsn}" \
  go test \
  ./internal/db \
  -run '^TestProductManagedConnectorRegistry' \
  -count=1 &
registry_pid="$!"

wait "${litellmfeed_pid}"
litellmfeed_status="$?"
wait "${registry_pid}"
registry_status="$?"
set -e

[ "${litellmfeed_status}" -eq 0 ] ||
  fail "litellmfeed isolated PostgreSQL tests failed"
[ "${registry_status}" -eq 0 ] ||
  fail "registry isolated PostgreSQL tests failed"

echo "LiteLLM PostgreSQL convergence: adapter pure tests and concurrent isolated litellmfeed/registry databases PASS"
