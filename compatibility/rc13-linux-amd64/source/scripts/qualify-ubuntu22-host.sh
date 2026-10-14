#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 4 ]; then
  echo "usage: scripts/qualify-ubuntu22-host.sh TARGET_ARTIFACT TARGET_PAYLOAD TRUST_STORE LEGACY_ARTIFACT" >&2
  exit 2
fi
if [ "${GITHUB_ACTIONS:-}" != "true" ] || [ "${XINSIGHT_CI_EPHEMERAL_HOST:-}" != "1" ]; then
  echo "host qualification is restricted to an explicitly disposable GitHub Actions VM" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET_ARTIFACT="$1"
TARGET_PAYLOAD="$2"
TRUST_STORE="$3"
LEGACY_ARTIFACT="$4"
for input in "$TARGET_ARTIFACT" "$TARGET_PAYLOAD" "$TRUST_STORE" "$LEGACY_ARTIFACT"; do
  case "$input" in
    /*) ;;
    *)
      echo "qualification inputs must use absolute paths" >&2
      exit 1
      ;;
  esac
done
if [ ! -f "$TARGET_ARTIFACT" ] || [ ! -d "$TARGET_PAYLOAD" ] || [ ! -f "$TRUST_STORE" ] || [ ! -f "$LEGACY_ARTIFACT" ]; then
  echo "qualification input is missing or has the wrong type" >&2
  exit 1
fi
TARGET_MATRIX="$TARGET_PAYLOAD/release/compatibility-matrix.json"
TARGET_PRODUCT_VERSION="$(python3 "$ROOT/scripts/release-qualification-identity.py" --matrix "$TARGET_MATRIX" --field product_version)"
TARGET_MANUAL_VERSION="$(python3 "$ROOT/scripts/release-qualification-identity.py" --matrix "$TARGET_MATRIX" --field manual_version)"

QUALIFICATION_ROOT="${RUNNER_TEMP:?}/xinsight-host-qualification"
if [ -e "$QUALIFICATION_ROOT" ]; then
  echo "qualification output already exists: $QUALIFICATION_ROOT" >&2
  exit 1
fi
mkdir -p "$QUALIFICATION_ROOT"

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
  if [ -n "${TARGET_CTL:-}" ] && [ -x "${TARGET_CTL:-}" ]; then
    sudo env PATH="${CTL_PATH:-/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin}" \
      "$TARGET_CTL" status >&2
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

LEGACY_COMMIT="b9e5811cc53a5cb06614956e66a1c54756617f0a"
LEGACY_RELEASE_NAME="xinsight-release-linux-amd64-$LEGACY_COMMIT"
LEGACY_DIR="/opt/x-insight/releases/$LEGACY_RELEASE_NAME"
EXPECTED_LEGACY_SHA="$(python3 - "$ROOT/release/compatibility-matrix.json" <<'PY'
import json
import pathlib
import sys

matrix = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
sources = [source for source in matrix["supported_legacy_sources"] if source["commit"] == "b9e5811cc53a5cb06614956e66a1c54756617f0a"]
if len(sources) != 1:
    raise SystemExit("exact Phase 1 source is not uniquely declared")
print(sources[0]["artifact_sha256"])
PY
)"
ACTUAL_LEGACY_SHA="sha256:$(sha256sum "$LEGACY_ARTIFACT" | awk '{print $1}')"
if [ "$ACTUAL_LEGACY_SHA" != "$EXPECTED_LEGACY_SHA" ]; then
  echo "reproduced Phase 1 artifact digest mismatch" >&2
  exit 1
fi

POSTGRES_IMAGE="postgres:16.14-alpine3.24@sha256:57c72fd2a128e416c7fcc499958864df5301e940bca0a56f58fddf30ffc07777"
LEGACY_PASSWORD="$(openssl rand -hex 24)"
ADMIN_PASSWORD="$(openssl rand -hex 24)"
READER_PASSWORD="$(openssl rand -hex 24)"
WORKSPACE_PASSWORD="$(openssl rand -hex 24)"
IMPORTER_PASSWORD="$(openssl rand -hex 24)"
LEGACY_DSN="postgres://xinsight:${LEGACY_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"
ADMIN_DSN="postgres://xinsight_admin:${ADMIN_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"
READER_DSN="postgres://xinsight_reader:${READER_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"
WORKSPACE_DSN="postgres://xinsight_workspace:${WORKSPACE_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"
IMPORTER_DSN="postgres://xinsight_importer:${IMPORTER_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"
CTL_PATH="/usr/lib/postgresql/16/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
TARGET_CTL="$TARGET_PAYLOAD/bin/xinsightctl"

sudo useradd --system --home /var/lib/x-insight --shell /usr/sbin/nologin xinsight
sudo install -d -m 0755 /etc/x-insight /var/lib/x-insight /opt/x-insight/releases /data/x-insight
sudo install -d -m 0700 /opt/x-insight/control
sudo install -d -m 0700 /etc/x-insight/database/sets/initial /data/x-insight/postgres
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
printf '%s\n' "$LEGACY_PASSWORD" | sudo tee /etc/x-insight/pg_password.txt >/dev/null
sudo chmod 0600 /etc/x-insight/pg_password.txt

sudo docker pull "$POSTGRES_IMAGE" > "$QUALIFICATION_ROOT/postgres-image-pull.txt"
sudo docker image inspect "$POSTGRES_IMAGE" > "$QUALIFICATION_ROOT/postgres-image-inspect.json"
POSTGRES_UID="$(sudo docker run --rm --entrypoint id "$POSTGRES_IMAGE" -u postgres)"
POSTGRES_GID="$(sudo docker run --rm --entrypoint id "$POSTGRES_IMAGE" -g postgres)"
if ! [[ "$POSTGRES_UID" =~ ^[1-9][0-9]*$ ]] || ! [[ "$POSTGRES_GID" =~ ^[1-9][0-9]*$ ]]; then
  echo "pinned PostgreSQL image returned an invalid postgres UID/GID" >&2
  exit 1
fi
sudo chown "$POSTGRES_UID:$POSTGRES_GID" /data/x-insight/postgres
cat > "$QUALIFICATION_ROOT/legacy-postgres.compose.yml" <<EOF
services:
  postgres:
    image: $POSTGRES_IMAGE
    platform: linux/amd64
    container_name: xinsight-postgres
    pull_policy: never
    restart: unless-stopped
    environment:
      POSTGRES_USER: xinsight
      POSTGRES_PASSWORD_FILE: /run/secrets/pg_password
      POSTGRES_DB: xinsight
      PGDATA: /var/lib/postgresql/data
    ports:
      - "127.0.0.1:5432:5432"
    volumes:
      - type: bind
        source: /data/x-insight/postgres
        target: /var/lib/postgresql/data
      - type: bind
        source: /etc/x-insight/pg_password.txt
        target: /run/secrets/pg_password
        read_only: true
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -q -U xinsight -d xinsight"]
      interval: 5s
      timeout: 3s
      retries: 12
      start_period: 15s
    shm_size: 256mb
    stop_grace_period: 60s
    security_opt:
      - no-new-privileges:true
EOF
sudo docker compose --project-name xinsight-ci -f "$QUALIFICATION_ROOT/legacy-postgres.compose.yml" config > "$QUALIFICATION_ROOT/legacy-postgres-compose-config.txt"
sudo docker compose --project-name xinsight-ci -f "$QUALIFICATION_ROOT/legacy-postgres.compose.yml" up -d
for attempt in $(seq 1 30); do
  if PGPASSWORD="$LEGACY_PASSWORD" /usr/lib/postgresql/16/bin/pg_isready -q -h 127.0.0.1 -p 5432 -U xinsight -d xinsight; then
    break
  fi
  if [ "$attempt" -eq 30 ]; then
    echo "PostgreSQL 16 Compose did not become ready" >&2
    sudo docker compose --project-name xinsight-ci -f "$QUALIFICATION_ROOT/legacy-postgres.compose.yml" ps -a >&2 || true
    sudo docker compose --project-name xinsight-ci -f "$QUALIFICATION_ROOT/legacy-postgres.compose.yml" logs --no-color postgres >&2 || true
    sudo ls -ld /data/x-insight/postgres >&2 || true
    exit 1
  fi
  sleep 2
done

sudo tar -xzf "$LEGACY_ARTIFACT" -C /opt/x-insight/releases
sudo ln -s "$LEGACY_DIR" /opt/x-insight/current
printf 'XINSIGHT_POSTGRES_DSN=%s\nXINSIGHT_MIGRATIONS_DIR=/opt/x-insight/current/migrations\n' "$LEGACY_DSN" |
  sudo tee /etc/x-insight/xinsight.env >/dev/null
sudo chmod 0600 /etc/x-insight/xinsight.env

sudo env XINSIGHT_POSTGRES_DSN="$LEGACY_DSN" XINSIGHT_MIGRATIONS_DIR="$LEGACY_DIR/migrations" \
  "$LEGACY_DIR/bin/xinsight-server" migrate up > "$QUALIFICATION_ROOT/legacy-migrate-first.txt"
sudo env XINSIGHT_POSTGRES_DSN="$LEGACY_DSN" XINSIGHT_MIGRATIONS_DIR="$LEGACY_DIR/migrations" \
  "$LEGACY_DIR/bin/xinsight-server" migrate up > "$QUALIFICATION_ROOT/legacy-migrate-second.txt"
sudo env XINSIGHT_POSTGRES_DSN="$LEGACY_DSN" \
  "$LEGACY_DIR/bin/xinsight-importer" unirouter import-snapshot \
  --snapshot "$LEGACY_DIR/fixtures/unirouter_snapshot/model_permission_denied.json" \
  --persist > "$QUALIFICATION_ROOT/legacy-import-receipt.json"
chmod 0644 "$QUALIFICATION_ROOT/legacy-import-receipt.json"

sudo tee /etc/systemd/system/xinsight.service >/dev/null <<'UNIT'
[Unit]
Description=X-Insight Phase 1 legacy qualification service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=xinsight
Group=xinsight
WorkingDirectory=/opt/x-insight/current
EnvironmentFile=/etc/x-insight/xinsight.env
ExecStart=/opt/x-insight/current/xinsight-server serve 127.0.0.1:8080
Restart=on-failure
RestartSec=3s
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
IPAddressDeny=any
IPAddressAllow=localhost

[Install]
WantedBy=multi-user.target
UNIT
sudo chmod 0644 /etc/systemd/system/xinsight.service
sudo install -d -m 0755 /etc/systemd/system/xinsight.service.d
sudo tee /etc/systemd/system/xinsight.service.d/10-release-layout.conf >/dev/null <<'DROPIN'
[Service]
ExecStart=
ExecStart=/opt/x-insight/current/bin/xinsight-server serve 127.0.0.1:8080
DROPIN
sudo chmod 0644 /etc/systemd/system/xinsight.service.d/10-release-layout.conf
sudo systemctl daemon-reload
sudo systemctl enable --now xinsight.service

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

verify_dormant_feed_worker() {
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
    echo "legacy transition unexpectedly activated the feed worker" >&2
    exit 1
  fi
}

verify_dormant_litellm_feed_worker() {
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
    echo "legacy transition unexpectedly activated the LiteLLM feed worker" >&2
    exit 1
  fi
}

verify_connector_controller() {
  test "$(sudo stat -c '%a %U:%G' /etc/systemd/system/xinsight-connector-controller.service)" = "644 root:root"
  sudo cmp "$TARGET_PAYLOAD/deploy/xinsight-connector-controller.service" /etc/systemd/system/xinsight-connector-controller.service
  test "$(sudo systemctl show xinsight-connector-controller.service --property LoadState --value)" = "loaded"
  test "$(sudo systemctl show xinsight-connector-controller.service --property ActiveState --value)" = "active"
  test "$(sudo systemctl show xinsight-connector-controller.service --property UnitFileState --value)" = "enabled"
  sudo test -S /run/x-insight/connector-controller.sock
  test "$(sudo stat -c '%a %U:%G' /run/x-insight/connector-controller.sock)" = "660 root:xinsight"
  sudo test ! -e /etc/systemd/system/xinsight-connector-controller.socket
}
wait_ready

PRE_BRIDGE_BACKUP="$QUALIFICATION_ROOT/pre-bridge.dump"
(
  umask 077
  PGPASSWORD="$LEGACY_PASSWORD" /usr/lib/postgresql/16/bin/pg_dump \
    --host=127.0.0.1 --port=5432 --username=xinsight --dbname=xinsight \
    --format=custom --compress=9 --no-password --file="$PRE_BRIDGE_BACKUP"
)
PRE_BRIDGE_BACKUP_SHA="sha256:$(sha256sum "$PRE_BRIDGE_BACKUP" | awk '{print $1}')"
PRE_BRIDGE_BACKUP_SIZE="$(stat -c '%s' "$PRE_BRIDGE_BACKUP")"
PRE_BRIDGE_LISTING_COUNT="$(/usr/lib/postgresql/16/bin/pg_restore --list "$PRE_BRIDGE_BACKUP" | awk 'NF && $1 !~ /^;/ {count++} END {print count+0}')"
test "$PRE_BRIDGE_BACKUP_SIZE" -gt 0
test "$PRE_BRIDGE_LISTING_COUNT" -gt 0
python3 - "$PRE_BRIDGE_BACKUP_SHA" "$PRE_BRIDGE_BACKUP_SIZE" "$PRE_BRIDGE_LISTING_COUNT" <<'PY' > "$QUALIFICATION_ROOT/pre-bridge-backup.json"
import json
import sys

print(json.dumps({
    "schema_version": "xinsight.ci-pre-bridge-backup.v1",
    "classification": "ci_clean_vm_not_target",
    "sha256": sys.argv[1],
    "size_bytes": int(sys.argv[2]),
    "pg_dump_major": 16,
    "pg_restore_major": 16,
    "pg_restore_listing_entries": int(sys.argv[3]),
    "source_role": "xinsight",
    "admin_bridge_applied": False,
}, indent=2, sort_keys=True))
PY

PGPASSWORD="$LEGACY_PASSWORD" /usr/lib/postgresql/16/bin/psql \
  --host=127.0.0.1 --port=5432 --username=xinsight --dbname=xinsight \
  --no-password --set=ON_ERROR_STOP=1 >/dev/null <<SQL
CREATE ROLE xinsight_admin WITH LOGIN SUPERUSER CREATEDB CREATEROLE NOREPLICATION CONNECTION LIMIT 5 PASSWORD '$ADMIN_PASSWORD';
SQL
PGPASSWORD="$ADMIN_PASSWORD" /usr/lib/postgresql/16/bin/psql \
  --host=127.0.0.1 --port=5432 --username=xinsight_admin --dbname=xinsight \
  --no-password --tuples-only --no-align \
  --command="SELECT current_user || '|' || (SELECT rolsuper::text FROM pg_roles WHERE rolname = current_user)" |
  grep -qx 'xinsight_admin|true'

sudo env PATH="$CTL_PATH" "$TARGET_CTL" adopt --check \
  --artifact "$TARGET_ARTIFACT" --legacy-artifact "$LEGACY_ARTIFACT" > "$QUALIFICATION_ROOT/adopt-check.json"
sudo env PATH="$CTL_PATH" "$TARGET_CTL" adopt \
  --artifact "$TARGET_ARTIFACT" --legacy-artifact "$LEGACY_ARTIFACT" > "$QUALIFICATION_ROOT/adopt-receipt.json"
# Reproduce the named Target's pre-rc.4 residue after adoption: an existing
# product-owned control directory inherited 0700 from a restrictive umask.
sudo chmod 0700 /opt/x-insight/control
sudo env PATH="$CTL_PATH" "$TARGET_CTL" upgrade --check \
  --artifact "$TARGET_ARTIFACT" > "$QUALIFICATION_ROOT/upgrade-check.json"

CRASH_BIN="$QUALIFICATION_ROOT/sigkill-bin"
CRASH_MARKER="$QUALIFICATION_ROOT/sigkill-ready.pid"
CRASH_OUTPUT="$QUALIFICATION_ROOT/sigkill-command.txt"
mkdir -m 0700 "$CRASH_BIN"
ln -s "$ROOT/scripts/ci-systemctl-sigkill-wrapper.sh" "$CRASH_BIN/systemctl"

sudo env \
  GITHUB_ACTIONS=true \
  XINSIGHT_CI_EPHEMERAL_HOST=1 \
  RUNNER_TEMP="$RUNNER_TEMP" \
  XINSIGHT_CI_SIGKILL_MARKER="$CRASH_MARKER" \
  XINSIGHT_CI_EXPECTED_CTL="$TARGET_CTL" \
  PATH="$CRASH_BIN:$CTL_PATH" \
  setsid --wait "$TARGET_CTL" upgrade --artifact "$TARGET_ARTIFACT" \
  > "$CRASH_OUTPUT" 2>&1 &
CRASH_JOB_PID="$!"

for attempt in $(seq 1 200); do
  if [ -s "$CRASH_MARKER" ]; then
    break
  fi
  if ! kill -0 "$CRASH_JOB_PID" 2>/dev/null; then
    if wait "$CRASH_JOB_PID"; then
      early_exit=0
    else
      early_exit="$?"
    fi
    echo "upgrade exited before the bounded SIGKILL point: $early_exit" >&2
    sed -n '1,120p' "$CRASH_OUTPUT" >&2
    exit 1
  fi
  if [ "$attempt" -eq 200 ]; then
    echo "timed out waiting for the bounded SIGKILL point" >&2
    exit 1
  fi
  sleep 0.1
done

CRASH_CTL_PID="$(tr -d '[:space:]' < "$CRASH_MARKER")"
CRASH_MARKER_UID="$(stat -c '%u' "$CRASH_MARKER")"
CRASH_MARKER_MODE="$(stat -c '%a' "$CRASH_MARKER")"
if ! [[ "$CRASH_CTL_PID" =~ ^[1-9][0-9]*$ ]] || [ "$CRASH_MARKER_UID" != "0" ] || [ "$CRASH_MARKER_MODE" != "444" ]; then
  echo "SIGKILL marker is not one root-owned read-only process ID" >&2
  exit 1
fi
EXPECTED_CTL="$(readlink -f "$TARGET_CTL")"
ACTUAL_CTL="$(sudo readlink -f "/proc/$CRASH_CTL_PID/exe")"
CRASH_PGID="$(ps -o pgid= -p "$CRASH_CTL_PID" | tr -d '[:space:]')"
CRASH_SID="$(ps -o sid= -p "$CRASH_CTL_PID" | tr -d '[:space:]')"
CALLER_PGID="$(ps -o pgid= -p "$$" | tr -d '[:space:]')"
if [ "$ACTUAL_CTL" != "$EXPECTED_CTL" ] || [ "$CRASH_PGID" != "$CRASH_CTL_PID" ] || \
   [ "$CRASH_SID" != "$CRASH_CTL_PID" ] || [ "$CRASH_PGID" = "$CALLER_PGID" ]; then
  echo "refusing an unbounded SIGKILL target" >&2
  exit 1
fi

sudo /usr/bin/kill -KILL -- "-$CRASH_PGID"
if wait "$CRASH_JOB_PID"; then
  CRASH_EXIT_CODE=0
else
  CRASH_EXIT_CODE="$?"
fi
if [ "$CRASH_EXIT_CODE" -eq 0 ] || sudo test -e "/proc/$CRASH_CTL_PID"; then
  echo "xinsightctl was not terminated by the bounded SIGKILL" >&2
  exit 1
fi

sudo env PATH="$CTL_PATH" "$TARGET_CTL" recover > "$QUALIFICATION_ROOT/sigkill-recover.json"
CRASH_OPERATION_ID="$(python3 - "$QUALIFICATION_ROOT/sigkill-recover.json" <<'PY'
import json
import pathlib
import sys

status = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
journal = status.get("active_journal") or {}
if status.get("schema_version") != "xinsight.host-status.v1":
    raise SystemExit(f"unexpected recovery status schema: {status}")
if journal.get("kind") != "upgrade" or journal.get("status") != "active" or journal.get("phase") != "stop_service":
    raise SystemExit(f"SIGKILL did not preserve the expected active journal: {journal}")
if "stop_service" in journal.get("completed_phases", []):
    raise SystemExit(f"interrupted phase was incorrectly marked complete: {journal}")
if set(journal.get("allowed_actions", [])) != {"recover", "resume"}:
    raise SystemExit(f"unexpected recovery actions after SIGKILL: {journal}")
print(journal["operation_id"])
PY
)"
if sudo test -e "/var/lib/x-insight/control/receipts/$CRASH_OPERATION_ID.json"; then
  echo "interrupted operation unexpectedly has a final receipt" >&2
  exit 1
fi

sudo env PATH="$CTL_PATH" "$TARGET_CTL" resume > "$QUALIFICATION_ROOT/upgrade-receipt.json"
wait_ready
verify_dormant_feed_worker
verify_dormant_litellm_feed_worker
verify_connector_controller

python3 - <<'PY' > "$QUALIFICATION_ROOT/dormant-feed-worker.json"
import json

print(json.dumps({
    "schema_version": "xinsight.ci-dormant-feed-worker.v1",
    "classification": "ci_clean_vm_not_target",
    "upgrade_controller": "verified_target_payload",
    "unit_matches_target_payload": True,
    "unit_mode": "0644",
    "load_state": "loaded",
    "active_state": "inactive",
    "unit_file_state": "disabled",
    "environment_present": False,
    "credential_present": False,
    "staged_credential_present": False,
    "credential_operation_state_present": False,
    "worker_process_present": False,
    "litellm_unit_matches_target_payload": True,
    "litellm_unit_mode": "0644",
    "litellm_load_state": "loaded",
    "litellm_active_state": "inactive",
    "litellm_unit_file_state": "disabled",
    "litellm_environment_present": False,
    "litellm_credential_present": False,
    "litellm_worker_process_present": False,
}, indent=2, sort_keys=True))
PY

python3 - "$QUALIFICATION_ROOT/sigkill-recover.json" "$QUALIFICATION_ROOT/upgrade-receipt.json" "$CRASH_EXIT_CODE" <<'PY' \
  > "$QUALIFICATION_ROOT/sigkill-recovery-result.json"
import json
import pathlib
import sys

recover = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
receipt = json.loads(pathlib.Path(sys.argv[2]).read_text(encoding="utf-8"))
journal = recover["active_journal"]
if receipt.get("operation_id") != journal.get("operation_id"):
    raise SystemExit("resume did not complete the interrupted operation")
if receipt.get("kind") != "upgrade" or receipt.get("status") != "completed":
    raise SystemExit(f"unexpected resume receipt: {receipt}")
result = {
    "schema_version": "xinsight.ci-sigkill-recovery.v1",
    "classification": "ci_clean_vm_not_target",
    "host": "ubuntu-22.04-amd64-systemd",
    "injection": "external_process_group_sigkill",
    "operation": "upgrade",
    "interrupted_phase": "stop_service",
    "phase_action_completed_before_sigkill": True,
    "phase_recorded_complete_before_sigkill": False,
    "journal_status_after_sigkill": journal["status"],
    "allowed_actions_after_sigkill": journal["allowed_actions"],
    "final_receipt_absent_before_resume": True,
    "resume_operation_id_match": True,
    "resume_status": receipt["status"],
    "sigkill_command_exit_code": int(sys.argv[3]),
    "service_ready_after_resume": True,
}
print(json.dumps(result, indent=2, sort_keys=True))
PY

sudo cat /opt/x-insight/current/manual/zh-CN/index.html > "$QUALIFICATION_ROOT/manual.html"

test "$(sudo stat -c '%a %U:%G' /opt/x-insight/control)" = "755 root:root"
test "$(sudo stat -c '%a %U:%G' /etc/x-insight/access-accounts.json)" = "600 root:root"
test "$(sudo stat -c '%a %U:%G' /etc/systemd/system/xinsight.service.d)" = "755 root:root"
test "$(sudo stat -c '%a %U:%G' /etc/systemd/system/xinsight.service.d/99-xinsight-managed-launcher.conf)" = "644 root:root"
sudo cmp "$TARGET_PAYLOAD/deploy/99-xinsight-managed-launcher.conf" /etc/systemd/system/xinsight.service.d/99-xinsight-managed-launcher.conf
sudo python3 - /etc/systemd/system/xinsight.service.d/10-release-layout.conf <<'PY'
import pathlib
import sys

expected = "[Service]\nExecStart=\nExecStart=/opt/x-insight/current/bin/xinsight-server serve 127.0.0.1:8080\n"
if pathlib.Path(sys.argv[1]).read_text(encoding="utf-8") != expected:
    raise SystemExit("Phase 1 release-layout drop-in changed")
PY
sudo systemctl show xinsight.service --property ExecStart --value | grep -F 'path=/opt/x-insight/control/xinsight-launcher' >/dev/null

if sudo env PATH="$CTL_PATH" /opt/x-insight/current/bin/xinsightctl rollback --check \
  --previous > "$QUALIFICATION_ROOT/rollback-check.stdout" \
  2> "$QUALIFICATION_ROOT/rollback-check.stderr"; then
  echo "rc.13 unexpectedly allowed application rollback onto an older schema-11 application" >&2
  exit 1
fi
grep -F 'current release does not declare application rollback compatibility' \
  "$QUALIFICATION_ROOT/rollback-check.stderr" >/dev/null
python3 - "$QUALIFICATION_ROOT/rollback-check.stderr" <<'PY' \
  > "$QUALIFICATION_ROOT/rollback-incompatibility.json"
import json
import pathlib
import sys

error = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
if "current release does not declare application rollback compatibility" not in error:
    raise SystemExit(f"unexpected rollback rejection: {error}")
print(json.dumps({
    "schema_version": "xinsight.ci-rollback-incompatibility.v1",
    "operation": "rollback",
    "supported": False,
    "reason": "current_release_does_not_declare_application_rollback_compatibility",
}, indent=2, sort_keys=True))
PY

sudo systemctl show xinsight.service --property ExecStart --value | grep -F 'path=/opt/x-insight/control/xinsight-launcher' >/dev/null
sudo cmp "$TARGET_PAYLOAD/deploy/99-xinsight-managed-launcher.conf" /etc/systemd/system/xinsight.service.d/99-xinsight-managed-launcher.conf

sudo cmp "$TARGET_PAYLOAD/deploy/postgres.compose.yml" /etc/x-insight/postgres.compose.yml
sudo docker inspect xinsight-postgres > "$QUALIFICATION_ROOT/legacy-postgres-runtime.json"
sudo python3 - \
  "$QUALIFICATION_ROOT/legacy-postgres-runtime.json" \
  /etc/x-insight/postgres.compose.yml \
  /etc/x-insight/postgres.env <<'PY' > "$QUALIFICATION_ROOT/postgres-bridge-result.json"
import json
import pathlib
import sys

runtime = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))[0]
compose = pathlib.Path(sys.argv[2]).read_text(encoding="utf-8")
postgres_env = {}
for line in pathlib.Path(sys.argv[3]).read_text(encoding="utf-8").splitlines():
    if line and not line.startswith("#") and "=" in line:
        key, value = line.split("=", 1)
        postgres_env[key] = value
runtime_env = {}
for line in runtime["Config"]["Env"]:
    if "=" in line:
        key, value = line.split("=", 1)
        runtime_env[key] = value
mounts = {item["Destination"]: item for item in runtime["Mounts"]}
database_mount = mounts.get("/var/lib/postgresql/data", {})
if runtime["Name"] != "/xinsight-postgres":
    raise SystemExit("legacy PostgreSQL container name mismatch")
if runtime_env.get("POSTGRES_USER") != "xinsight" or runtime_env.get("PGDATA") != "/var/lib/postgresql/data":
    raise SystemExit("legacy PostgreSQL runtime identity mismatch")
if database_mount.get("Source") != "/data/x-insight/postgres" or database_mount.get("Type") != "bind":
    raise SystemExit("legacy PostgreSQL data mount mismatch")
for required in (
    "container_name: xinsight-postgres",
    "PGDATA: /var/lib/postgresql/data",
    "source: /data/x-insight/postgres",
    "target: /var/lib/postgresql/data",
):
    if required not in compose:
        raise SystemExit(f"managed PostgreSQL Compose is missing {required}")
if "/var/lib/postgresql/data/pgdata" in compose or "source: /var/lib/x-insight/postgres" in compose:
    raise SystemExit("managed PostgreSQL Compose retains unsafe alternate data root")
if postgres_env.get("POSTGRES_DB") != "xinsight" or postgres_env.get("POSTGRES_USER") != "xinsight_admin" or not postgres_env.get("POSTGRES_PASSWORD"):
    raise SystemExit("managed PostgreSQL environment identity mismatch")
print(json.dumps({
    "schema_version": "xinsight.ci-postgres-legacy-bridge.v1",
    "classification": "ci_clean_vm_not_target",
    "active_legacy_role": "xinsight",
    "managed_admin_role": "xinsight_admin",
    "container_name": "xinsight-postgres",
    "data_root": "/data/x-insight/postgres",
    "container_pgdata": "/var/lib/postgresql/data",
    "simultaneous_database_containers": False,
    "managed_compose_staged": True,
}, indent=2, sort_keys=True))
PY

BACKUP_PATH="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["backup_path"])' "$QUALIFICATION_ROOT/upgrade-receipt.json")"
BACKUP_SHA256="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["backup_sha256"])' "$QUALIFICATION_ROOT/upgrade-receipt.json")"
sudo env PATH="$CTL_PATH" /opt/x-insight/current/bin/xinsightctl restore --check \
  --backup "$BACKUP_PATH" --backup-sha256 "$BACKUP_SHA256" \
  --new-database xinsight_restore_ci > "$QUALIFICATION_ROOT/restore-check.json"
sudo env PATH="$CTL_PATH" /opt/x-insight/current/bin/xinsightctl restore \
  --backup "$BACKUP_PATH" --backup-sha256 "$BACKUP_SHA256" \
  --new-database xinsight_restore_ci --reason-code field_validation > "$QUALIFICATION_ROOT/restore-receipt.json"
wait_ready
verify_dormant_feed_worker
verify_dormant_litellm_feed_worker
verify_connector_controller

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

python3 - "$QUALIFICATION_ROOT" "$TARGET_ARTIFACT" "$ACTUAL_LEGACY_SHA" "$TARGET_PRODUCT_VERSION" "$TARGET_MANUAL_VERSION" <<'PY' > "$QUALIFICATION_ROOT/qualification-result.json"
import hashlib
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
artifact = pathlib.Path(sys.argv[2])
legacy_sha = sys.argv[3]
expected_product_version = sys.argv[4]
expected_manual_version = sys.argv[5]

for name, kind in (
    ("adopt-receipt.json", "adopt"),
    ("upgrade-receipt.json", "upgrade"),
    ("restore-receipt.json", "restore"),
):
    receipt = json.loads((root / name).read_text(encoding="utf-8"))
    if receipt["kind"] != kind or receipt["status"] != "completed":
        raise SystemExit(f"unexpected {name}: {receipt}")
    if kind != "adopt":
        checks = {item.get("name"): item.get("status") for item in receipt.get("checks", [])}
        if (
            checks.get("service_launcher") != "pass"
            or checks.get("representative_read") != "pass"
            or checks.get("anonymous_access_boundary") != "pass"
        ):
            raise SystemExit(f"effective managed launcher proof missing from {name}: {checks}")

status = json.loads((root / "final-status.json").read_text(encoding="utf-8"))
if not status["managed"] or status["install_state"]["current"]["product_version"] != expected_product_version:
    raise SystemExit(f"unexpected final status: {status}")
if status["database_set_ref"].endswith("/initial"):
    raise SystemExit("restore did not activate a new credential set")
schema_contract = json.loads((root / "schema-contract-inspection.json").read_text(encoding="utf-8"))
if not schema_contract.get("valid"):
    raise SystemExit(f"supported-upgrade schema contract drift: {schema_contract}")
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
crash = json.loads((root / "sigkill-recovery-result.json").read_text(encoding="utf-8"))
if crash.get("resume_status") != "completed" or not crash.get("service_ready_after_resume"):
    raise SystemExit(f"SIGKILL recovery sample is incomplete: {crash}")
bridge = json.loads((root / "postgres-bridge-result.json").read_text(encoding="utf-8"))
if bridge.get("data_root") != "/data/x-insight/postgres" or bridge.get("container_pgdata") != "/var/lib/postgresql/data" or not bridge.get("managed_compose_staged"):
    raise SystemExit(f"PostgreSQL legacy bridge proof is incomplete: {bridge}")
pre_bridge_backup = json.loads((root / "pre-bridge-backup.json").read_text(encoding="utf-8"))
if pre_bridge_backup.get("source_role") != "xinsight" or pre_bridge_backup.get("admin_bridge_applied") is not False or pre_bridge_backup.get("pg_restore_listing_entries", 0) <= 0:
    raise SystemExit(f"pre-bridge backup proof is incomplete: {pre_bridge_backup}")
rollback_incompatibility = json.loads((root / "rollback-incompatibility.json").read_text(encoding="utf-8"))
if rollback_incompatibility.get("supported") is not False or rollback_incompatibility.get("reason") != "current_release_does_not_declare_application_rollback_compatibility":
    raise SystemExit(f"rollback incompatibility proof is incomplete: {rollback_incompatibility}")
dormant_feed = json.loads((root / "dormant-feed-worker.json").read_text(encoding="utf-8"))
if (
    dormant_feed.get("upgrade_controller") != "verified_target_payload"
    or dormant_feed.get("unit_matches_target_payload") is not True
    or dormant_feed.get("load_state") != "loaded"
    or dormant_feed.get("active_state") != "inactive"
    or dormant_feed.get("unit_file_state") != "disabled"
    or dormant_feed.get("environment_present") is not False
    or dormant_feed.get("credential_present") is not False
    or dormant_feed.get("staged_credential_present") is not False
    or dormant_feed.get("credential_operation_state_present") is not False
    or dormant_feed.get("worker_process_present") is not False
    or dormant_feed.get("litellm_unit_matches_target_payload") is not True
    or dormant_feed.get("litellm_load_state") != "loaded"
    or dormant_feed.get("litellm_active_state") != "inactive"
    or dormant_feed.get("litellm_unit_file_state") != "disabled"
    or dormant_feed.get("litellm_environment_present") is not False
    or dormant_feed.get("litellm_credential_present") is not False
    or dormant_feed.get("litellm_worker_process_present") is not False
):
    raise SystemExit(f"dormant feed worker proof is incomplete: {dormant_feed}")
feed_credential = json.loads((root / "feed-credential-status.json").read_text(encoding="utf-8"))
if (
    feed_credential.get("schema_version") != "xinsight.feed-credential-status.v1"
    or feed_credential.get("status") != "absent"
    or feed_credential.get("active_file", {}).get("present") is not False
    or feed_credential.get("staged_file", {}).get("present") is not False
):
    raise SystemExit(f"feed credential dormant proof is incomplete: {feed_credential}")

result = {
    "schema_version": "xinsight.ci-host-qualification.v1",
    "classification": "ci_clean_vm_not_target",
    "host": "ubuntu-22.04-amd64-systemd",
    "postgresql": "16.14-compose-loopback",
    "target_artifact_sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
    "legacy_artifact_sha256": legacy_sha,
    "operations": ["adopt", "phase1_dropin_bridge", "sigkill_recover_resume_upgrade", "incompatible_application_rollback_rejected", "isolated_restore"],
    "sigkill_recovery_schema": crash["schema_version"],
    "sigkill_interrupted_phase": crash["interrupted_phase"],
    "sigkill_resume_status": crash["resume_status"],
    "final_product_version": status["install_state"]["current"]["product_version"],
    "final_database_schema": status["install_state"]["database_schema_current"],
    "final_database_set_ref": status["database_set_ref"],
    "anonymous_ui_status": int(anonymous_ui_status),
    "anonymous_api_status": int(anonymous_api_status),
    "authenticated_https_target": "pending",
    "access_account_credential": "all_disabled_ci_synthetic_no_login",
    "manual_version": expected_manual_version,
    "postgres_legacy_bridge_schema": bridge["schema_version"],
    "postgres_data_root": bridge["data_root"],
    "postgres_container_pgdata": bridge["container_pgdata"],
    "pre_bridge_backup_schema": pre_bridge_backup["schema_version"],
    "pre_bridge_backup_sha256": pre_bridge_backup["sha256"],
    "rollback_incompatibility_schema": rollback_incompatibility["schema_version"],
    "upgrade_controller": dormant_feed["upgrade_controller"],
    "dormant_feed_worker_schema": dormant_feed["schema_version"],
    "dormant_feed_unit_matches_target_payload": dormant_feed["unit_matches_target_payload"],
    "dormant_feed_state": "loaded_inactive_disabled",
    "dormant_litellm_feed_state": "loaded_inactive_disabled",
    "feed_credential_state": "absent_no_prepare",
    "phase1_dropin_preserved": True,
    "managed_launcher_dropin": True,
    "control_directory_mode": "0755",
    "public_listener_count": 0,
}
print(json.dumps(result, indent=2, sort_keys=True))
PY

printf '%s\n' 'ci_clean_vm=pass'
printf '%s\n' 'external_sigkill_recover_resume=pass'
printf '%s\n' 'legacy_adopt_upgrade_rollback_rejected_restore=pass'
printf 'qualification_result=%s\n' "$QUALIFICATION_ROOT/qualification-result.json"
