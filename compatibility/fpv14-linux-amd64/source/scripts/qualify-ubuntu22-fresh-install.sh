#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: scripts/qualify-ubuntu22-fresh-install.sh TARGET_ARTIFACT TARGET_PAYLOAD TRUST_STORE" >&2
  exit 2
fi
if [ "${GITHUB_ACTIONS:-}" != "true" ] || [ "${XINSIGHT_CI_EPHEMERAL_HOST:-}" != "1" ]; then
  echo "fresh-install qualification is restricted to an explicitly disposable GitHub Actions VM" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET_ARTIFACT="$1"
TARGET_PAYLOAD="$2"
TRUST_STORE="$3"
for input in "$TARGET_ARTIFACT" "$TARGET_PAYLOAD" "$TRUST_STORE"; do
  case "$input" in
    /*) ;;
    *)
      echo "qualification inputs must use absolute paths" >&2
      exit 1
      ;;
  esac
done
if [ ! -f "$TARGET_ARTIFACT" ] || [ ! -d "$TARGET_PAYLOAD" ] || [ ! -f "$TRUST_STORE" ]; then
  echo "qualification input is missing or has the wrong type" >&2
  exit 1
fi
TARGET_MATRIX="$TARGET_PAYLOAD/release/compatibility-matrix.json"
TARGET_PRODUCT_VERSION="$(python3 "$ROOT/scripts/release-qualification-identity.py" --matrix "$TARGET_MATRIX" --field product_version)"
TARGET_MANUAL_VERSION="$(python3 "$ROOT/scripts/release-qualification-identity.py" --matrix "$TARGET_MATRIX" --field manual_version)"

QUALIFICATION_ROOT="${RUNNER_TEMP:?}/xinsight-fresh-install-qualification"
if [ -e "$QUALIFICATION_ROOT" ]; then
  echo "qualification output already exists: $QUALIFICATION_ROOT" >&2
  exit 1
fi
mkdir -p "$QUALIFICATION_ROOT"

TARGET_CTL="$TARGET_PAYLOAD/bin/xinsightctl"
CTL_PATH="/usr/lib/postgresql/16/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

qualification_failure() {
  exit_code="$?"
  trap - ERR
  set +e
  if sudo test -e /etc/systemd/system/xinsight.service; then
    sudo systemctl status xinsight.service --no-pager >&2
    sudo journalctl -u xinsight.service --since "5 minutes ago" --no-pager >&2
  fi
  if sudo test -e /etc/systemd/system/xinsight-connector-controller.service; then
    sudo systemctl status xinsight-connector-controller.service --no-pager >&2
    sudo journalctl -u xinsight-connector-controller.service --since "5 minutes ago" --no-pager >&2
  fi
  if sudo test -e /etc/systemd/system/xinsight-litellm-feed.service; then
    sudo systemctl status xinsight-litellm-feed.service --no-pager >&2
    sudo journalctl -u xinsight-litellm-feed.service --since "5 minutes ago" --no-pager >&2
  fi
  if [ -x "$TARGET_CTL" ]; then
    sudo env PATH="$CTL_PATH" "$TARGET_CTL" status >&2
  fi
  exit "$exit_code"
}
trap qualification_failure ERR

for path in /opt/x-insight /etc/x-insight /var/lib/x-insight /var/backups/x-insight /data/x-insight /etc/systemd/system/xinsight.service /etc/systemd/system/xinsight-connector-controller.service /etc/systemd/system/xinsight-feed.service /etc/systemd/system/xinsight-litellm-feed.service; do
  if sudo test -e "$path" || sudo test -L "$path"; then
    echo "disposable VM is not clean at $path" >&2
    exit 1
  fi
done

POSTGRES_IMAGE="postgres:16.14-alpine3.24@sha256:57c72fd2a128e416c7fcc499958864df5301e940bca0a56f58fddf30ffc07777"
ADMIN_PASSWORD="$(openssl rand -hex 24)"
READER_PASSWORD="$(openssl rand -hex 24)"
WORKSPACE_PASSWORD="$(openssl rand -hex 24)"
IMPORTER_PASSWORD="$(openssl rand -hex 24)"
ADMIN_DSN="postgres://xinsight_admin:${ADMIN_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"
READER_DSN="postgres://xinsight_reader:${READER_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"
WORKSPACE_DSN="postgres://xinsight_workspace:${WORKSPACE_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"
IMPORTER_DSN="postgres://xinsight_importer:${IMPORTER_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"

sudo useradd --system --home /var/lib/x-insight --shell /usr/sbin/nologin xinsight
sudo install -d -m 0755 /etc/x-insight /var/lib/x-insight /data/x-insight
sudo install -d -m 0700 /etc/x-insight/database/sets/initial /data/x-insight/postgres
sudo install -m 0644 "$TARGET_PAYLOAD/deploy/postgres.compose.yml" /etc/x-insight/postgres.compose.yml
sudo install -m 0644 "$TRUST_STORE" /etc/x-insight/trusted-release-keys.json
sudo tee /etc/x-insight/access-accounts.json >/dev/null <<'JSON'
{
  "schema_version": "xinsight.local-accounts.v1",
  "accounts": [
    {
      "subject_id": "xs_0000000000000000",
      "username": "ci.disabled",
      "role": "investigator",
      "disabled": true,
      "password_hash": "$argon2id$v=19$m=19456,t=2,p=1$l/Jq1AZhEtyhc18PB2oOXg$v2CbNPksEVetOCbGuBIIOeKNHdF/Z+JBU9YuhqQ7piY"
    }
  ]
}
JSON
sudo chown root:root /etc/x-insight/access-accounts.json
sudo chmod 0600 /etc/x-insight/access-accounts.json
sudo env PATH="$CTL_PATH" "$TARGET_CTL" access validate \
  --file /etc/x-insight/access-accounts.json \
  > "$QUALIFICATION_ROOT/access-account-validation.json"

printf 'POSTGRES_DB=xinsight\nPOSTGRES_USER=xinsight_admin\nPOSTGRES_PASSWORD=%s\n' "$ADMIN_PASSWORD" |
  sudo tee /etc/x-insight/postgres.env >/dev/null
sudo chmod 0600 /etc/x-insight/postgres.env
printf '%s\n' "$ADMIN_DSN" | sudo tee /etc/x-insight/database/sets/initial/postgres-admin.dsn >/dev/null
printf '%s\n' "$READER_DSN" | sudo tee /etc/x-insight/database/sets/initial/postgres-reader.dsn >/dev/null
printf '%s\n' "$WORKSPACE_DSN" | sudo tee /etc/x-insight/database/sets/initial/postgres-workspace.dsn >/dev/null
printf '%s\n' "$IMPORTER_DSN" | sudo tee /etc/x-insight/database/sets/initial/postgres-importer.dsn >/dev/null
sudo chmod 0600 /etc/x-insight/database/sets/initial/postgres-admin.dsn
sudo chmod 0600 /etc/x-insight/database/sets/initial/postgres-reader.dsn
sudo chmod 0600 /etc/x-insight/database/sets/initial/postgres-workspace.dsn
sudo chmod 0600 /etc/x-insight/database/sets/initial/postgres-importer.dsn
sudo ln -s /etc/x-insight/database/sets/initial /etc/x-insight/database/current

sudo docker pull "$POSTGRES_IMAGE" > "$QUALIFICATION_ROOT/postgres-image-pull.txt"
sudo docker image inspect "$POSTGRES_IMAGE" > "$QUALIFICATION_ROOT/postgres-image-inspect.json"
POSTGRES_UID="$(sudo docker run --rm --entrypoint id "$POSTGRES_IMAGE" -u postgres)"
POSTGRES_GID="$(sudo docker run --rm --entrypoint id "$POSTGRES_IMAGE" -g postgres)"
if ! [[ "$POSTGRES_UID" =~ ^[1-9][0-9]*$ ]] || ! [[ "$POSTGRES_GID" =~ ^[1-9][0-9]*$ ]]; then
  echo "pinned PostgreSQL image returned an invalid postgres UID/GID" >&2
  exit 1
fi
sudo chown "$POSTGRES_UID:$POSTGRES_GID" /data/x-insight/postgres
sudo docker compose --project-name xinsight-fresh-ci -f /etc/x-insight/postgres.compose.yml up -d
for attempt in $(seq 1 30); do
  if PGPASSWORD="$ADMIN_PASSWORD" /usr/lib/postgresql/16/bin/pg_isready -q -h 127.0.0.1 -p 5432 -U xinsight_admin -d xinsight; then
    break
  fi
  if [ "$attempt" -eq 30 ]; then
    echo "PostgreSQL 16 Compose did not become ready" >&2
    sudo docker compose --project-name xinsight-fresh-ci -f /etc/x-insight/postgres.compose.yml ps -a >&2 || true
    sudo docker compose --project-name xinsight-fresh-ci -f /etc/x-insight/postgres.compose.yml logs --no-color postgres >&2 || true
    sudo ls -ld /data/x-insight/postgres >&2 || true
    exit 1
  fi
  sleep 2
done

test "$(sudo docker inspect xinsight-postgres --format '{{.Name}}')" = "/xinsight-postgres"
SAFE_POSTGRES_ENV="$(sudo docker inspect xinsight-postgres --format '{{range .Config.Env}}{{println .}}{{end}}' | grep -E '^(PGDATA|POSTGRES_USER)=')"
grep -qx 'PGDATA=/var/lib/postgresql/data' <<<"$SAFE_POSTGRES_ENV"
grep -qx 'POSTGRES_USER=xinsight_admin' <<<"$SAFE_POSTGRES_ENV"
POSTGRES_DATA_SOURCE="$(sudo docker inspect xinsight-postgres --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Source}}{{end}}{{end}}')"
test "$POSTGRES_DATA_SOURCE" = "/data/x-insight/postgres"
python3 - <<'PY' > "$QUALIFICATION_ROOT/postgres-profile-result.json"
import json

print(json.dumps({
    "schema_version": "xinsight.ci-postgres-profile.v1",
    "classification": "ci_clean_vm_not_target",
    "container_name": "xinsight-postgres",
    "admin_role": "xinsight_admin",
    "data_root": "/data/x-insight/postgres",
    "container_pgdata": "/var/lib/postgresql/data",
}, indent=2, sort_keys=True))
PY

sudo env PATH="$CTL_PATH" "$TARGET_CTL" install --check \
  --artifact "$TARGET_ARTIFACT" > "$QUALIFICATION_ROOT/install-check.json"
sudo env PATH="$CTL_PATH" "$TARGET_CTL" install \
  --artifact "$TARGET_ARTIFACT" > "$QUALIFICATION_ROOT/install-receipt.json"

wait_ready() {
  for attempt in 1 2 3 4 5 6 7 8 9 10 11 12; do
    if curl --fail --silent --show-error http://127.0.0.1:8080/readyz > "$QUALIFICATION_ROOT/readyz.json"; then
      return 0
    fi
    if [ "$attempt" -eq 12 ]; then
      sudo systemctl status xinsight.service --no-pager >&2 || true
      sudo journalctl -u xinsight.service --since "5 minutes ago" --no-pager >&2 || true
      return 1
    fi
    sleep 2
  done
}
wait_ready

test "$(sudo stat -c '%a %U:%G' /opt/x-insight/control)" = "755 root:root"
test "$(sudo stat -c '%a %U:%G' /etc/x-insight/access-accounts.json)" = "600 root:root"
test "$(sudo stat -c '%a %U:%G' /etc/systemd/system/xinsight.service.d)" = "755 root:root"
test "$(sudo stat -c '%a %U:%G' /etc/systemd/system/xinsight.service.d/99-xinsight-managed-launcher.conf)" = "644 root:root"
sudo cmp "$TARGET_PAYLOAD/deploy/99-xinsight-managed-launcher.conf" /etc/systemd/system/xinsight.service.d/99-xinsight-managed-launcher.conf
sudo systemctl show xinsight.service --property ExecStart --value | grep -F 'path=/opt/x-insight/control/xinsight-launcher' >/dev/null
test "$(sudo stat -c '%a %U:%G' /etc/systemd/system/xinsight-connector-controller.service)" = "644 root:root"
sudo cmp "$TARGET_PAYLOAD/deploy/xinsight-connector-controller.service" /etc/systemd/system/xinsight-connector-controller.service
test "$(sudo systemctl show xinsight-connector-controller.service --property LoadState --value)" = "loaded"
test "$(sudo systemctl show xinsight-connector-controller.service --property ActiveState --value)" = "active"
test "$(sudo systemctl show xinsight-connector-controller.service --property UnitFileState --value)" = "enabled"
sudo test -S /run/x-insight/connector-controller.sock
test "$(sudo stat -c '%a %U:%G' /run/x-insight/connector-controller.sock)" = "660 root:xinsight"
sudo test ! -e /etc/systemd/system/xinsight-connector-controller.socket
test "$(sudo stat -c '%a %U:%G' /etc/systemd/system/xinsight-feed.service)" = "644 root:root"
sudo cmp "$TARGET_PAYLOAD/deploy/xinsight-feed.service" /etc/systemd/system/xinsight-feed.service
test "$(sudo systemctl show xinsight-feed.service --property LoadState --value)" = "loaded"
test "$(sudo systemctl show xinsight-feed.service --property ActiveState --value)" = "inactive"
test "$(sudo systemctl show xinsight-feed.service --property UnitFileState --value)" = "disabled"
sudo test ! -e /etc/x-insight/xinsight-feed.env
sudo test ! -L /etc/x-insight/xinsight-feed.env
sudo test ! -e /etc/x-insight/unirouter-feed.key
sudo test ! -L /etc/x-insight/unirouter-feed.key
sudo test ! -e /etc/x-insight/unirouter-feed.key.next
sudo test ! -L /etc/x-insight/unirouter-feed.key.next
sudo test ! -e /var/lib/x-insight/control/feed-credential-operation.json
sudo test ! -L /var/lib/x-insight/control/feed-credential-operation.json
if sudo pgrep -u xinsight -f 'xinsight-importer.*unirouter pull-feed' >/dev/null; then
  echo "fresh install unexpectedly activated the feed worker" >&2
  exit 1
fi
test "$(sudo stat -c '%a %U:%G' /etc/systemd/system/xinsight-litellm-feed.service)" = "644 root:root"
sudo cmp "$TARGET_PAYLOAD/deploy/xinsight-litellm-feed.service" /etc/systemd/system/xinsight-litellm-feed.service
test "$(sudo systemctl show xinsight-litellm-feed.service --property LoadState --value)" = "loaded"
test "$(sudo systemctl show xinsight-litellm-feed.service --property ActiveState --value)" = "inactive"
test "$(sudo systemctl show xinsight-litellm-feed.service --property UnitFileState --value)" = "disabled"
sudo test ! -e /etc/x-insight/xinsight-litellm-feed.env
sudo test ! -L /etc/x-insight/xinsight-litellm-feed.env
sudo test ! -e /etc/x-insight/litellm-feed.key
sudo test ! -L /etc/x-insight/litellm-feed.key
if sudo pgrep -u xinsight -f 'xinsight-importer.*litellm pull-spend-logs' >/dev/null; then
  echo "fresh install unexpectedly activated the LiteLLM feed worker" >&2
  exit 1
fi

sudo env \
  XINSIGHT_IMPORTER_POSTGRES_DSN_FILE=/etc/x-insight/database/current/postgres-importer.dsn \
  /opt/x-insight/current/bin/xinsight-importer unirouter gate-a-check \
  --snapshot /opt/x-insight/current/fixtures/unirouter_snapshot/model_permission_denied.json \
  --schema /opt/x-insight/current/schemas/m0/evidence-event.schema.json \
  --receipt-schema /opt/x-insight/current/schemas/integration/import-receipt.schema.json \
  --persist \
  --receipt-output "$QUALIFICATION_ROOT/import-receipt.json" \
  > "$QUALIFICATION_ROOT/import-report.json"
sudo chmod 0644 "$QUALIFICATION_ROOT/import-receipt.json" "$QUALIFICATION_ROOT/import-report.json"

sudo env PATH="$CTL_PATH" /opt/x-insight/current/bin/xinsightctl status > "$QUALIFICATION_ROOT/final-status.json"
sudo env PATH="$CTL_PATH" /opt/x-insight/current/bin/xinsightctl schema-contract inspect \
  > "$QUALIFICATION_ROOT/schema-contract-inspection.json"
sudo env PATH="$CTL_PATH" /opt/x-insight/current/bin/xinsightctl feed-credential status \
  > "$QUALIFICATION_ROOT/feed-credential-status.json"
sudo cat /opt/x-insight/current/manual/zh-CN/index.html > "$QUALIFICATION_ROOT/manual.html"
curl --silent --show-error --output /dev/null \
  --dump-header "$QUALIFICATION_ROOT/anonymous-ui-headers.txt" \
  --write-out '%{http_code}\n' \
  http://127.0.0.1:8080/executions \
  > "$QUALIFICATION_ROOT/anonymous-ui-status.txt"
curl --silent --show-error \
  --dump-header "$QUALIFICATION_ROOT/anonymous-api-headers.txt" \
  --write-out '%{http_code}\n' \
  --output "$QUALIFICATION_ROOT/anonymous-api-problem.json" \
  http://127.0.0.1:8080/api/v1/executions/req-auth-denied-001 \
  > "$QUALIFICATION_ROOT/anonymous-api-status.txt"
ss -ltnH > "$QUALIFICATION_ROOT/listeners.txt"
if grep -Eq '(^|[[:space:]])(0\.0\.0\.0|\*|\[::\]):(5432|8080)([[:space:]]|$)' "$QUALIFICATION_ROOT/listeners.txt"; then
  echo "X-Insight or PostgreSQL is exposed beyond loopback" >&2
  exit 1
fi
grep -F '127.0.0.1:5432' "$QUALIFICATION_ROOT/listeners.txt" >/dev/null
grep -F '127.0.0.1:8080' "$QUALIFICATION_ROOT/listeners.txt" >/dev/null

python3 - "$QUALIFICATION_ROOT" "$TARGET_ARTIFACT" "$TARGET_PRODUCT_VERSION" "$TARGET_MANUAL_VERSION" <<'PY' > "$QUALIFICATION_ROOT/qualification-result.json"
import hashlib
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
artifact = pathlib.Path(sys.argv[2])
expected_product_version = sys.argv[3]
expected_manual_version = sys.argv[4]

check = json.loads((root / "install-check.json").read_text(encoding="utf-8"))
if check["operation"] != "install" or not check["supported"]:
    raise SystemExit(f"unexpected install check: {check}")
receipt = json.loads((root / "install-receipt.json").read_text(encoding="utf-8"))
if receipt["kind"] != "install" or receipt["status"] != "completed":
    raise SystemExit(f"unexpected install receipt: {receipt}")
receipt_checks = {item.get("name"): item.get("status") for item in receipt.get("checks", [])}
if (
    receipt_checks.get("service_launcher") != "pass"
    or receipt_checks.get("representative_read") != "pass"
    or receipt_checks.get("anonymous_access_boundary") != "pass"
):
    raise SystemExit(f"effective managed launcher proof missing: {receipt_checks}")
status = json.loads((root / "final-status.json").read_text(encoding="utf-8"))
if not status["managed"] or status["install_state"]["current"]["product_version"] != expected_product_version:
    raise SystemExit(f"unexpected final status: {status}")
schema_contract = json.loads((root / "schema-contract-inspection.json").read_text(encoding="utf-8"))
if not schema_contract.get("valid"):
    raise SystemExit(f"fresh-install schema contract drift: {schema_contract}")
anonymous_api_status = (root / "anonymous-api-status.txt").read_text(encoding="utf-8").strip()
anonymous_api = json.loads((root / "anonymous-api-problem.json").read_text(encoding="utf-8"))
anonymous_api_headers = (root / "anonymous-api-headers.txt").read_text(encoding="utf-8").lower()
if (
    anonymous_api_status != "401"
    or anonymous_api.get("status") != 401
    or anonymous_api.get("code") != "authentication_required"
    or "content-type: application/problem+json" not in {
        line.strip() for line in anonymous_api_headers.splitlines()
    }
    or set(anonymous_api) != {
        "type", "title", "status", "code", "correlation_id", "retryable"
    }
):
    raise SystemExit(f"anonymous API boundary is invalid: status={anonymous_api_status} body={anonymous_api}")
anonymous_ui_status = (root / "anonymous-ui-status.txt").read_text(encoding="utf-8").strip()
anonymous_ui_headers = (root / "anonymous-ui-headers.txt").read_text(encoding="utf-8").lower()
if (
    anonymous_ui_status != "303"
    or "location: /login" not in {
        line.strip() for line in anonymous_ui_headers.splitlines()
    }
    or "cache-control: no-store" not in {
        line.strip() for line in anonymous_ui_headers.splitlines()
    }
):
    raise SystemExit(f"anonymous UI boundary is invalid: status={anonymous_ui_status}")
import_receipt = json.loads((root / "import-receipt.json").read_text(encoding="utf-8"))
if import_receipt["result"]["status"] != "persisted" or import_receipt["result"]["readback_events"] != 2:
    raise SystemExit(f"fresh-install import receipt is incomplete: {import_receipt}")
access_validation = json.loads((root / "access-account-validation.json").read_text(encoding="utf-8"))
if (
    access_validation.get("schema_version") != "xinsight.local-accounts.v1"
    or access_validation.get("account_count") != 1
    or access_validation.get("disabled_count") != 1
):
    raise SystemExit(f"disabled CI account credential is invalid: {access_validation}")
manual = (root / "manual.html").read_text(encoding="utf-8")
if (
    f'name="xinsight-product-version" content="{expected_product_version}"' not in manual
    or f'name="xinsight-manual-version" content="{expected_manual_version}"' not in manual
):
    raise SystemExit("packaged manual identity mismatch")
postgres_profile = json.loads((root / "postgres-profile-result.json").read_text(encoding="utf-8"))
if postgres_profile.get("data_root") != "/data/x-insight/postgres" or postgres_profile.get("container_pgdata") != "/var/lib/postgresql/data":
    raise SystemExit(f"PostgreSQL profile mismatch: {postgres_profile}")
feed_credential = json.loads((root / "feed-credential-status.json").read_text(encoding="utf-8"))
if (
    feed_credential.get("schema_version") != "xinsight.feed-credential-status.v1"
    or feed_credential.get("status") != "absent"
    or feed_credential.get("active_file", {}).get("present") is not False
    or feed_credential.get("staged_file", {}).get("present") is not False
):
    raise SystemExit(f"feed credential dormant proof is incomplete: {feed_credential}")

result = {
    "schema_version": "xinsight.ci-fresh-install-qualification.v1",
    "classification": "ci_clean_vm_not_target",
    "host": "ubuntu-22.04-amd64-systemd",
    "postgresql": "16.14-compose-loopback",
    "target_artifact_sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
    "operations": ["fresh_install", "managed_launcher_dropin", "synthetic_fixture_import"],
    "final_product_version": status["install_state"]["current"]["product_version"],
    "final_database_schema": status["install_state"]["database_schema_current"],
    "database_set_ref": status["database_set_ref"],
    "import_readback_events": import_receipt["result"]["readback_events"],
    "anonymous_ui_status": int(anonymous_ui_status),
    "anonymous_api_status": int(anonymous_api_status),
    "authenticated_https_target": "pending",
    "access_account_credential": "all_disabled_ci_synthetic_no_login",
    "manual_version": expected_manual_version,
    "managed_launcher_dropin": True,
    "dormant_feed_state": "loaded_inactive_disabled",
    "dormant_litellm_feed_state": "loaded_inactive_disabled",
    "feed_credential_state": "absent_no_prepare",
    "control_directory_mode": "0755",
    "postgres_profile_schema": postgres_profile["schema_version"],
    "postgres_data_root": postgres_profile["data_root"],
    "postgres_container_pgdata": postgres_profile["container_pgdata"],
    "public_listener_count": 0,
}
print(json.dumps(result, indent=2, sort_keys=True))
PY

printf '%s\n' 'ci_clean_vm_fresh_install=pass'
printf 'qualification_result=%s\n' "$QUALIFICATION_ROOT/qualification-result.json"
