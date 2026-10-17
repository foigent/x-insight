#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [ "$#" -eq 3 ] && [ "$1" = --check-old-inputs ]; then
  exec python3 "$ROOT/scripts/check-fpv9-source.py" "$2" --binaries "$3" --allow-private-worktree
fi

# One exact, disposable FPV9 direct-run predecessor. This script is deliberately
# not a generic unmanaged-install converter or a Target runbook.
if [ "$#" -ne 5 ]; then
  echo "usage: qualify-ubuntu22-direct-fpv9.sh TARGET_ARTIFACT TARGET_PAYLOAD TRUST_STORE OLD_SOURCE OLD_BIN_DIR" >&2
  exit 2
fi
if [ "${GITHUB_ACTIONS:-}" != true ] || [ "${XINSIGHT_CI_EPHEMERAL_HOST:-}" != 1 ] || [ "$(id -u)" -eq 0 ]; then
  echo "direct FPV9 qualification requires an unprivileged caller on an explicitly disposable Actions VM" >&2
  exit 1
fi
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
TARGET_ARTIFACT="$1"
TARGET_PAYLOAD="$2"
TRUST_STORE="$3"
OLD_SOURCE="$4"
OLD_BIN_DIR="$5"
for input in "$@"; do
  case "$input" in /*) ;; *) echo "qualification inputs must be absolute" >&2; exit 1 ;; esac
done
test -f "$TARGET_ARTIFACT" && test -d "$TARGET_PAYLOAD" && test -f "$TRUST_STORE"
test -d "$OLD_SOURCE/migrations" && test -x "$OLD_BIN_DIR/xinsight-server" && test -x "$OLD_BIN_DIR/xinsight-importer"
if [ -f "$ROOT/compatibility/fpv9-linux-amd64/source-manifest.json" ]; then
  python3 "$ROOT/scripts/check-fpv9-source.py" "$OLD_SOURCE" --binaries "$OLD_BIN_DIR" --allow-private-worktree
else
  test "$(git -C "$OLD_SOURCE" rev-parse HEAD)" = 8ee764accbda21f21b489e01a4ab7751571beed4
fi
test "$(sha256sum "$OLD_BIN_DIR/xinsight-server" | cut -d' ' -f1)" = 80573c95149f5b9827b06f264f8d70e436a29ea1d8e93ddb6f1f46a75f4866d7
test "$(sha256sum "$OLD_BIN_DIR/xinsight-importer" | cut -d' ' -f1)" = c6b71ded9aa3fac35c8099aba222e13edb8772a77616f6ab3bb53094cbbcfda8
test "$(python3 "$ROOT/scripts/release-qualification-identity.py" --matrix "$TARGET_PAYLOAD/release/compatibility-matrix.json" --field product_version)" = 0.2.0-dev.fpv11

QUAL_ROOT="${RUNNER_TEMP:?}/xinsight-direct-fpv9-qualification"
test ! -e "$QUAL_ROOT"
mkdir -m 0700 "$QUAL_ROOT"
CTL="$TARGET_PAYLOAD/bin/xinsightctl"
CTL_PATH="/usr/lib/postgresql/16/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
OLD_BUILD=/data/x-insight/experience/fpv9-ci-exact-build
SOURCE_IP=203.0.113.10
SOURCE_CIDR="$SOURCE_IP/32"
SOURCE_ID=11111111-1111-4111-8111-111111111111
STREAM_ID=22222222-2222-4222-8222-222222222222
COLLECTION_ID=7996279c-df2b-4c0d-aaba-7c2a3b3d510b
QUAL_STARTED="$(date +%s)"
CURRENT_STAGE=preflight
stage() {
  CURRENT_STAGE="$1"
  printf 'direct_fpv9_stage=%s elapsed_seconds=%s\n' "$CURRENT_STAGE" "$(( $(date +%s) - QUAL_STARTED ))"
}
write_stage_result() {
  python3 - "$QUAL_ROOT/stage-result.json" "$1" "$CURRENT_STAGE" "$(( $(date +%s) - QUAL_STARTED ))" "$2" <<'PY'
import json,os,sys
path,state,stage,elapsed,code=sys.argv[1:]
fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
with os.fdopen(fd,'w') as out:
    json.dump({'schema_version':'xinsight.ci-direct-fpv9-stage.v1','state':state,
               'stage':stage,'elapsed_seconds':int(elapsed),'exit_code':int(code)},out,sort_keys=True)
    out.write('\n')
PY
}

qualification_finish() {
  result="$?"
  trap - EXIT
  if [ "$result" -eq 0 ]; then return 0; fi
  set +e
  if [ ! -e "$QUAL_ROOT/stage-result.json" ]; then write_stage_result failed "$result"; fi
  for unit in xinsight.service xinsight-feed.service xinsight-source-facts.service; do
    if sudo test -e "/etc/systemd/system/$unit"; then
      sudo systemctl show "$unit" --property ActiveState --property UnitFileState --property Result --property ExecMainStatus --property NRestarts --no-pager >&2
    fi
  done
  echo "direct FPV9 qualification failed at $CURRENT_STAGE after $(( $(date +%s) - QUAL_STARTED )) seconds; owner-only runtime files and backup remain on this disposable VM" >&2
  exit "$result"
}
trap qualification_finish EXIT

for path in /opt/x-insight /etc/x-insight /var/lib/x-insight /var/backups/x-insight /data/x-insight /etc/systemd/system/xinsight.service /etc/systemd/system/xinsight-feed.service /etc/systemd/system/xinsight-source-facts.service; do
  if sudo test -e "$path" || sudo test -L "$path"; then
    echo "qualification VM is not clean: $path" >&2
    exit 1
  fi
done
# TEST-NET-3 is explicitly blackholed on this VM. The synthetic credentials
# therefore cannot reach a daily UR/XI endpoint even while old workers run.
sudo ip -4 route add blackhole "$SOURCE_CIDR"
sudo ip -4 route show "$SOURCE_CIDR" | grep -q "^blackhole $SOURCE_IP"

POSTGRES_IMAGE=postgres:16.14-alpine3.24@sha256:57c72fd2a128e416c7fcc499958864df5301e940bca0a56f58fddf30ffc07777
ADMIN_PASSWORD="$(openssl rand -hex 24)"
READER_PASSWORD="$(openssl rand -hex 24)"
WORKSPACE_PASSWORD="$(openssl rand -hex 24)"
IMPORTER_PASSWORD="$(openssl rand -hex 24)"
ADMIN_DSN="postgres://xinsight_admin:${ADMIN_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"
READER_DSN="postgres://xinsight_reader:${READER_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"
WORKSPACE_DSN="postgres://xinsight_workspace:${WORKSPACE_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"
IMPORTER_DSN="postgres://xinsight_importer:${IMPORTER_PASSWORD}@127.0.0.1:5432/xinsight?sslmode=disable"

sudo useradd --system --home /var/lib/x-insight --shell /usr/sbin/nologin xinsight
sudo install -d -m 0755 /etc/x-insight /var/lib/x-insight /opt/x-insight/releases /data/x-insight /data/x-insight/experience
sudo install -d -m 0700 /etc/x-insight/database/sets/initial /data/x-insight/postgres
sudo install -m 0644 "$TRUST_STORE" /etc/x-insight/trusted-release-keys.json
sudo install -m 0644 "$TARGET_PAYLOAD/deploy/postgres.compose.yml" /etc/x-insight/postgres.compose.yml
sudo install -d -m 0755 "$OLD_BUILD"
sudo install -m 0755 "$OLD_BIN_DIR/xinsight-server" "$OLD_BUILD/xinsight-server"
sudo install -m 0755 "$OLD_BIN_DIR/xinsight-importer" "$OLD_BUILD/xinsight-importer"
sudo cmp "$OLD_BIN_DIR/xinsight-server" "$OLD_BUILD/xinsight-server"
sudo cmp "$OLD_BIN_DIR/xinsight-importer" "$OLD_BUILD/xinsight-importer"
# The exact old server loads migration identity relative to its working directory.
# Retain the matching runtime files as well as the two reproduced binaries.
sudo install -d -m 0755 "$OLD_BUILD/migrations"
for migration in "$OLD_SOURCE"/migrations/*.sql; do
  sudo install -m 0644 "$migration" "$OLD_BUILD/migrations/$(basename "$migration")"
  sudo cmp "$migration" "$OLD_BUILD/migrations/$(basename "$migration")"
done
sudo install -m 0600 /dev/null /etc/x-insight/postgres.env
printf 'POSTGRES_DB=xinsight\nPOSTGRES_USER=xinsight_admin\nPOSTGRES_PASSWORD=%s\n' "$ADMIN_PASSWORD" |
  sudo tee /etc/x-insight/postgres.env >/dev/null
for pair in "admin:$ADMIN_DSN" "reader:$READER_DSN" "workspace:$WORKSPACE_DSN" "importer:$IMPORTER_DSN"; do
  role="${pair%%:*}"
  dsn="${pair#*:}"
  sudo install -m 0600 /dev/null "/etc/x-insight/database/sets/initial/postgres-$role.dsn"
  printf '%s\n' "$dsn" | sudo tee "/etc/x-insight/database/sets/initial/postgres-$role.dsn" >/dev/null
done
sudo ln -s /etc/x-insight/database/sets/initial /etc/x-insight/database/current
sudo install -m 0600 /dev/null /etc/x-insight/access-accounts.json
sudo tee /etc/x-insight/access-accounts.json >/dev/null <<'JSON'
{"schema_version":"xinsight.local-accounts.v1","accounts":[{"subject_id":"xs_0000000000000000","username":"ci.disabled","role":"investigator","disabled":true,"password_hash":"$argon2id$v=19$m=19456,t=2,p=1$l/Jq1AZhEtyhc18PB2oOXg$v2CbNPksEVetOCbGuBIIOeKNHdF/Z+JBU9YuhqQ7piY"}]}
JSON
sudo chmod 0600 /etc/x-insight/access-accounts.json
sudo env PATH="$CTL_PATH" "$CTL" access validate --file /etc/x-insight/access-accounts.json > "$QUAL_ROOT/access-validation.json"

stage postgres16
sudo docker pull "$POSTGRES_IMAGE" > "$QUAL_ROOT/postgres-image-pull.txt"
POSTGRES_UID="$(sudo docker run --rm --entrypoint id "$POSTGRES_IMAGE" -u postgres)"
POSTGRES_GID="$(sudo docker run --rm --entrypoint id "$POSTGRES_IMAGE" -g postgres)"
[[ "$POSTGRES_UID" =~ ^[1-9][0-9]*$ && "$POSTGRES_GID" =~ ^[1-9][0-9]*$ ]]
sudo chown "$POSTGRES_UID:$POSTGRES_GID" /data/x-insight/postgres
sudo docker compose --project-name xinsight-fpv9-ci -f /etc/x-insight/postgres.compose.yml up -d
for attempt in $(seq 1 30); do
  if PGPASSWORD="$ADMIN_PASSWORD" /usr/lib/postgresql/16/bin/pg_isready -q -h 127.0.0.1 -p 5432 -U xinsight_admin -d xinsight; then
    break
  fi
  if [ "$attempt" -eq 30 ]; then echo "PG16 did not become ready" >&2; exit 1; fi
  sleep 2
done

stage old_source_seed
sudo env XINSIGHT_ADMIN_POSTGRES_DSN_FILE=/etc/x-insight/database/current/postgres-admin.dsn XINSIGHT_MIGRATIONS_DIR="$OLD_SOURCE/migrations" \
  "$OLD_BUILD/xinsight-server" migrate up > "$QUAL_ROOT/old-migrate.txt"
go build -o "$QUAL_ROOT/fpv9-source-roles" "$ROOT/scripts/ci-direct-fpv9-source-roles.go"
sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 PATH="$CTL_PATH" \
  "$QUAL_ROOT/fpv9-source-roles" > "$QUAL_ROOT/source-role-result.txt"

# Fixed source, cursor and content-reference rows; all content is synthetic.
PGPASSWORD="$ADMIN_PASSWORD" /usr/lib/postgresql/16/bin/psql -h 127.0.0.1 -U xinsight_admin -d xinsight \
  -v ON_ERROR_STOP=1 -v source_id="$SOURCE_ID" -v stream_id="$STREAM_ID" -v collection_id="$COLLECTION_ID" >/dev/null <<'SQL'
INSERT INTO source_systems(id,source_key,source_type,display_name)
VALUES(:'source_id'::uuid,'ci-fpv9:'||:'source_id','unirouter','synthetic FPV9 predecessor');
INSERT INTO source_fact_streams(id,source_system_id,source_instance,org_id,project_id,profile,scope_generation,status,cursor_value,high_watermark)
VALUES(:'stream_id'::uuid,:'source_id'::uuid,'ci-source','2','86','unirouter.execution-context.v1','1','active','ci-old-cursor','ci-old-hwm');
SQL
sudo env XINSIGHT_IMPORTER_POSTGRES_DSN_FILE=/etc/x-insight/database/current/postgres-importer.dsn "$OLD_BUILD/xinsight-importer" unirouter import-snapshot \
  --snapshot "$OLD_SOURCE/fixtures/unirouter_snapshot/model_permission_denied.json" --persist \
  > "$QUAL_ROOT/old-import-receipt.json"
sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 XINSIGHT_ADMIN_POSTGRES_DSN_FILE=/etc/x-insight/database/current/postgres-admin.dsn \
  "$QUAL_ROOT/fpv9-source-roles" seed-content "$SOURCE_ID" "$COLLECTION_ID" > "$QUAL_ROOT/content-seed-result.txt"

sudo install -m 0600 /dev/null /etc/x-insight/ci-source.key
sudo install -m 0600 /dev/null /etc/x-insight/ci-content.key
sudo install -m 0600 /dev/null /etc/x-insight/unirouter-feed.key
for key in ci-source.key ci-content.key unirouter-feed.key; do
  printf 'ci-only-%s\n' "$(openssl rand -hex 24)" | sudo tee "/etc/x-insight/$key" >/dev/null
done
sudo chmod 0600 /etc/x-insight/{ci-source.key,ci-content.key,unirouter-feed.key}
python3 - "$SOURCE_IP" "$SOURCE_ID" "$STREAM_ID" "$COLLECTION_ID" <<'PY' > "$QUAL_ROOT/source-facts.json"
import json, sys
ip, source, stream, collection = sys.argv[1:]
endpoint = 'https://' + ip
config = {
  'schema_version': 'xinsight.managed-source-facts.v4', 'poll_interval_seconds': 5, 'retry_maximum_seconds': 20,
  'profiles': [{'stream_id': stream, 'source_system_id': source, 'source_instance': 'ci-source', 'org_id': '2',
    'project_id': '86', 'profile': 'unirouter.execution-context.v1', 'scope_generation': '1',
    'endpoint': endpoint + '/api/v1/integrations/xinsight/source-facts', 'key_file': '/etc/x-insight/ci-source.key',
    'allow_private_tunnel_http':False}],
  'bindings': [
    {'name':'ci-legacy','scope':'legacy','source_system_id':source,'source_instance':'ci-source','org_id':'2','project_id':'86',
     'credential_id':'ci-legacy-credential','registration_hash':'sha256:'+'b'*64,'key_file':'/etc/x-insight/ci-source.key'},
    {'name':'ci-content','scope':'content','source_system_id':source,'source_instance':'ci-source','org_id':'2','project_id':'86',
     'credential_id':'ci-content-credential','registration_hash':'sha256:'+'a'*64,'key_file':'/etc/x-insight/ci-content.key'}],
  'contents': [{'collection_id':collection,'source_system_id':source,'source_instance':'ci-source','org_id':'2','project_id':'86',
    'credential_binding':'ci-content','content_scope_revision':'2','recovery_epoch':'1','allowed_purposes':['human_review'],
    'coverage_from':{'time':'2026-09-25T00:00:00.000000Z','after_sequence':'0'},
    'endpoint':endpoint+'/api/v1/integrations/xinsight/source/v2/content-manifests',
    'allow_private_tunnel_http':False}],
}
print(json.dumps(config, sort_keys=True, separators=(',', ':')))
PY
sudo install -m 0644 "$QUAL_ROOT/source-facts.json" /etc/x-insight/source-facts.json
printf 'XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xinsight.ci.invalid\n' |
  sudo tee /etc/x-insight/trusted-tls-proxy.env >/dev/null
sudo chmod 0644 /etc/x-insight/trusted-tls-proxy.env
printf 'XINSIGHT_UNIROUTER_FEED_ENDPOINT=https://%s/api/v1/integrations/xinsight/completed-executions\nXINSIGHT_UNIROUTER_FEED_POLL_INTERVAL=5s\nXINSIGHT_UNIROUTER_FEED_ALLOW_PRIVATE_TUNNEL_HTTP=false\nXINSIGHT_UNIROUTER_FEED_MAPPING_MODE=legacy\n' "$SOURCE_IP" |
  sudo tee /etc/x-insight/xinsight-feed.env >/dev/null
sudo chmod 0644 /etc/x-insight/xinsight-feed.env

sudo install -d -m 0755 /etc/systemd/system/xinsight.service.d /etc/systemd/system/xinsight-feed.service.d /etc/systemd/system/xinsight-source-facts.service.d
cat > "$QUAL_ROOT/xinsight.service" <<EOF
[Unit]
Description=Exact FPV9 direct-run CI server
After=network-online.target
[Service]
Type=simple
User=xinsight
Group=xinsight
WorkingDirectory=$OLD_BUILD
Environment=XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback
Environment=XINSIGHT_EXTERNAL_ORIGIN=https://xinsight.ci.invalid
LoadCredential=postgres-reader.dsn:/etc/x-insight/database/current/postgres-reader.dsn
LoadCredential=postgres-workspace.dsn:/etc/x-insight/database/current/postgres-workspace.dsn
LoadCredential=access-accounts.json:/etc/x-insight/access-accounts.json
ExecStart=$OLD_BUILD/xinsight-server serve 127.0.0.1:8080
Restart=on-failure
RestartSec=3s
NoNewPrivileges=true
ProtectSystem=strict
IPAddressDeny=any
IPAddressAllow=localhost
[Install]
WantedBy=multi-user.target
EOF
cat > "$QUAL_ROOT/xinsight-feed.service" <<EOF
[Unit]
Description=Exact FPV9 direct-run CI feed
After=network-online.target
[Service]
Type=simple
User=xinsight
Group=xinsight
WorkingDirectory=$OLD_BUILD
EnvironmentFile=/etc/x-insight/xinsight-feed.env
LoadCredential=postgres-importer.dsn:/etc/x-insight/database/current/postgres-importer.dsn
LoadCredential=unirouter-feed.key:/etc/x-insight/unirouter-feed.key
ExecStart=$OLD_BUILD/xinsight-importer unirouter pull-feed
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
ProtectSystem=strict
IPAddressDeny=any
IPAddressAllow=localhost
[Install]
WantedBy=multi-user.target
EOF
cat > "$QUAL_ROOT/xinsight-source-facts.service" <<EOF
[Unit]
Description=Exact FPV9 direct-run CI Source Facts
After=network-online.target
ConditionPathExists=/etc/x-insight/source-facts.json
[Service]
Type=simple
User=xinsight
Group=xinsight
WorkingDirectory=$OLD_BUILD
LoadCredential=postgres-importer.dsn:/etc/x-insight/database/current/postgres-importer.dsn
ExecStart=$OLD_BUILD/xinsight-importer unirouter run-source-facts
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
ProtectSystem=strict
IPAddressDeny=any
IPAddressAllow=localhost
[Install]
WantedBy=multi-user.target
EOF
for unit in xinsight.service xinsight-feed.service xinsight-source-facts.service; do
  sudo install -m 0644 "$QUAL_ROOT/$unit" "/etc/systemd/system/$unit"
done
printf '[Service]\nEnvironment=XINSIGHT_CONTENT_CONFIG=/etc/x-insight/source-facts.json\nLoadCredential=source-content-%s.key:/etc/x-insight/ci-content.key\n' "$COLLECTION_ID" |
  sudo tee /etc/systemd/system/xinsight.service.d/30-content-credentials.conf >/dev/null
printf '[Service]\nLoadCredential=\nLoadCredential=postgres-importer.dsn:/etc/x-insight/database/current/postgres-importer.dsn\nLoadCredential=source-profile-0.key:/etc/x-insight/ci-source.key\nLoadCredential=source-profile-1.key:/etc/x-insight/ci-content.key\n' |
  sudo tee /etc/systemd/system/xinsight-source-facts.service.d/10-source-credentials.conf >/dev/null
printf '[Service]\nExecStart=\nExecStart=%s/xinsight-server serve 127.0.0.1:8080\nIPAddressAllow=%s\n' "$OLD_BUILD" "$SOURCE_CIDR" |
  sudo tee /etc/systemd/system/xinsight.service.d/50-github-program-20260927.conf >/dev/null
printf '[Service]\nExecStart=\nExecStart=%s/xinsight-importer unirouter pull-feed\nIPAddressAllow=%s\n' "$OLD_BUILD" "$SOURCE_CIDR" |
  sudo tee /etc/systemd/system/xinsight-feed.service.d/50-github-program-20260927.conf >/dev/null
printf '[Service]\nExecStart=\nExecStart=%s/xinsight-importer unirouter run-source-facts\nIPAddressAllow=%s\n' "$OLD_BUILD" "$SOURCE_CIDR" |
  sudo tee /etc/systemd/system/xinsight-source-facts.service.d/50-github-program-20260927.conf >/dev/null
sudo chmod 0644 /etc/systemd/system/xinsight*.service.d/*.conf
stage old_systemd
sudo systemctl daemon-reload
sudo systemctl enable --now xinsight.service xinsight-feed.service xinsight-source-facts.service
for unit in xinsight.service xinsight-feed.service xinsight-source-facts.service; do
  sudo systemctl is-active --quiet "$unit"
  test "$(sudo systemctl show "$unit" --property UnitFileState --value)" = enabled
done
wait_ready() {
  for attempt in $(seq 1 30); do
    if [ "$(curl --silent --output /dev/null --write-out '%{http_code}' http://127.0.0.1:8080/readyz || true)" = 200 ]; then return 0; fi
    sleep 1
  done
  echo "server readyz did not return 200" >&2
  return 1
}
wait_ready

PG_SYSTEM_ID="$(PGPASSWORD="$ADMIN_PASSWORD" /usr/lib/postgresql/16/bin/psql -At -h 127.0.0.1 -U xinsight_admin -d xinsight -c 'SELECT system_identifier FROM pg_control_system()')"
sudo python3 - "$PG_SYSTEM_ID" "$SOURCE_CIDR" "$OLD_BUILD" <<'PY' > "$QUAL_ROOT/source-proof.json"
import hashlib, json, pathlib, sys
system_id, cidr, old_build = sys.argv[1:]
base = pathlib.Path('/etc/systemd/system')
files = {
 'service_unit':base/'xinsight.service', 'feed_unit':base/'xinsight-feed.service',
 'source_facts_unit':base/'xinsight-source-facts.service',
 'service_content_dropin':base/'xinsight.service.d/30-content-credentials.conf',
 'service_delivery_dropin':base/'xinsight.service.d/50-github-program-20260927.conf',
 'feed_delivery_dropin':base/'xinsight-feed.service.d/50-github-program-20260927.conf',
 'source_facts_credentials_dropin':base/'xinsight-source-facts.service.d/10-source-credentials.conf',
 'source_facts_delivery_dropin':base/'xinsight-source-facts.service.d/50-github-program-20260927.conf',
 'source_facts_config':pathlib.Path('/etc/x-insight/source-facts.json'),
}
proof = {'schema_version':'xinsight.unmanaged-fpv9-source.v1','product_version':'0.2.0-dev.fpv9',
 'commit':'8ee764accbda21f21b489e01a4ab7751571beed4','database_name':'xinsight',
 'system_identifier':system_id,'egress_allow_cidr':cidr,
 'server_binary':old_build+'/xinsight-server','worker_binary':old_build+'/xinsight-importer',
 'files':{role:'sha256:'+hashlib.sha256(path.read_bytes()).hexdigest() for role,path in files.items()}}
print(json.dumps(proof,sort_keys=True,separators=(',',':')))
PY
sudo install -m 0600 "$QUAL_ROOT/source-proof.json" /etc/x-insight/fpv9-source-proof.json
stable_snapshot() {
  PGPASSWORD="$ADMIN_PASSWORD" /usr/lib/postgresql/16/bin/psql -At -h 127.0.0.1 -U xinsight_admin -d "$1" \
    -v ON_ERROR_STOP=1 -v stream_id="$STREAM_ID" -v collection_id="$COLLECTION_ID" <<'SQL'
SELECT jsonb_build_object(
 'stream',(SELECT jsonb_build_array(source_system_id,source_instance,org_id,project_id,profile,scope_generation,status,cursor_value,high_watermark) FROM source_fact_streams WHERE id=:'stream_id'::uuid),
 'content',(SELECT jsonb_build_array(source_system_id,source_instance,org_id,project_id,credential_id,registration_hash,content_scope_revision,recovery_epoch,status,cursor_value,last_sequence,high_watermark_sequence) FROM source_content_collections WHERE id=:'collection_id'::uuid),
 'manifests',(SELECT jsonb_agg(jsonb_build_array(sequence,source_fact_id,source_sha256,source_json->>'content_ref') ORDER BY sequence) FROM source_content_manifests WHERE collection_id=:'collection_id'::uuid),
 'receipts',(SELECT count(*) FROM source_content_receipts WHERE collection_id=:'collection_id'::uuid),
 'evidence',(SELECT count(*) FROM evidence_events)
)::text;
SQL
}
stable_snapshot xinsight > "$QUAL_ROOT/before-check-snapshot.json"
python3 - "$QUAL_ROOT/before-check-snapshot.json" <<'PY'
import json,sys
snapshot=json.load(open(sys.argv[1]))
assert snapshot['evidence']>0 and snapshot['receipts']==1 and len(snapshot['manifests'])==1
assert snapshot['stream'][-3:]==['active','ci-old-cursor','ci-old-hwm']
assert snapshot['content'][-4:]==['active','ci-content-cursor','1','1']
assert snapshot['manifests'][0][-1]=='uc1_'+'c'*64
PY
sudo sha256sum /etc/x-insight/access-accounts.json > "$QUAL_ROOT/before-account.sha256"
sudo find /etc/systemd/system/xinsight*.service* /etc/x-insight /opt/x-insight /var/lib/x-insight -type f -exec sha256sum {} + |
  LC_ALL=C sort > "$QUAL_ROOT/before-check-files.txt"
stage read_only_check
sudo env PATH="$CTL_PATH" "$CTL" upgrade-direct-fpv9 --check --artifact "$TARGET_ARTIFACT" \
  --source-proof /etc/x-insight/fpv9-source-proof.json > "$QUAL_ROOT/direct-check.json"
stable_snapshot xinsight > "$QUAL_ROOT/after-check-snapshot.json"
cmp "$QUAL_ROOT/before-check-snapshot.json" "$QUAL_ROOT/after-check-snapshot.json"
sudo find /etc/systemd/system/xinsight*.service* /etc/x-insight /opt/x-insight /var/lib/x-insight -type f -exec sha256sum {} + |
  LC_ALL=C sort > "$QUAL_ROOT/after-check-files.txt"
cmp "$QUAL_ROOT/before-check-files.txt" "$QUAL_ROOT/after-check-files.txt"
sudo test ! -e /var/lib/x-insight/control/active-operation.json

stage upgrade_sigkill
CRASH_BIN="$QUAL_ROOT/sigkill-bin"
CRASH_MARKER="$QUAL_ROOT/sigkill-ready.pid"
mkdir -m 0700 "$CRASH_BIN"
ln -s "$ROOT/scripts/ci-systemctl-sigkill-wrapper.sh" "$CRASH_BIN/systemctl"
sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 RUNNER_TEMP="$RUNNER_TEMP" \
  XINSIGHT_CI_SIGKILL_ACTION=restart_service_reload XINSIGHT_CI_SIGKILL_MARKER="$CRASH_MARKER" \
  XINSIGHT_CI_EXPECTED_CTL="$CTL" PATH="$CRASH_BIN:$CTL_PATH" \
  setsid --wait "$CTL" upgrade-direct-fpv9 --artifact "$TARGET_ARTIFACT" \
  --source-proof /etc/x-insight/fpv9-source-proof.json > "$QUAL_ROOT/upgrade-command.txt" 2>&1 &
CRASH_JOB_PID="$!"
for attempt in $(seq 1 200); do
  if [ -s "$CRASH_MARKER" ]; then break; fi
  if ! kill -0 "$CRASH_JOB_PID" 2>/dev/null; then
    echo "upgrade exited before SIGKILL point" >&2; exit 1
  fi
  if [ "$attempt" -eq 200 ]; then echo "SIGKILL point timeout" >&2; exit 1; fi
  sleep 0.1
done
CRASH_CTL_PID="$(tr -d '[:space:]' < "$CRASH_MARKER")"
[[ "$CRASH_CTL_PID" =~ ^[1-9][0-9]*$ ]]
test "$(stat -c '%u:%a' "$CRASH_MARKER")" = 0:444
test "$(sudo readlink -f "/proc/$CRASH_CTL_PID/exe")" = "$(readlink -f "$CTL")"
CRASH_PGID="$(ps -o pgid= -p "$CRASH_CTL_PID" | tr -d '[:space:]')"
CRASH_SID="$(ps -o sid= -p "$CRASH_CTL_PID" | tr -d '[:space:]')"
CALLER_PGID="$(ps -o pgid= -p "$$" | tr -d '[:space:]')"
test "$CRASH_PGID" = "$CRASH_CTL_PID" && test "$CRASH_SID" = "$CRASH_CTL_PID" && test "$CRASH_PGID" != "$CALLER_PGID"
sudo /usr/bin/kill -KILL -- "-$CRASH_PGID"
if wait "$CRASH_JOB_PID"; then echo "SIGKILL did not terminate upgrade" >&2; exit 1; fi
sudo test ! -e "/proc/$CRASH_CTL_PID"
sudo env PATH="$CTL_PATH" "$CTL" recover > "$QUAL_ROOT/recover.json"
python3 - "$QUAL_ROOT/recover.json" <<'PY'
import json,sys
j=(json.load(open(sys.argv[1]))['active_journal'])
assert j['kind']=='upgrade_direct_fpv9' and j['status']=='active' and j['phase']=='restart_service'
assert 'migrate_database' in j['completed_phases'] and 'restart_service' not in j['completed_phases']
assert set(j['allowed_actions'])=={'recover','resume'}
PY
stage upgrade_resume
sudo env PATH="$CTL_PATH" "$CTL" resume > "$QUAL_ROOT/upgrade-receipt.json"
sudo env PATH="$CTL_PATH" "$CTL" status > "$QUAL_ROOT/after-upgrade-status.json"
wait_ready
for unit in xinsight.service xinsight-feed.service xinsight-source-facts.service; do sudo systemctl is-active --quiet "$unit"; done
stable_snapshot xinsight > "$QUAL_ROOT/after-upgrade-snapshot.json"
cmp "$QUAL_ROOT/before-check-snapshot.json" "$QUAL_ROOT/after-upgrade-snapshot.json"
sudo sha256sum /etc/x-insight/access-accounts.json > "$QUAL_ROOT/after-upgrade-account.sha256"
cmp "$QUAL_ROOT/before-account.sha256" "$QUAL_ROOT/after-upgrade-account.sha256"

python3 - "$QUAL_ROOT/source-proof.json" "$QUAL_ROOT/upgrade-receipt.json" "$QUAL_ROOT/after-upgrade-status.json" <<'PY' > "$QUAL_ROOT/upgrade-assertions.json"
import hashlib,json,pathlib,sys
proof=json.load(open(sys.argv[1])); receipt=json.load(open(sys.argv[2])); status=json.load(open(sys.argv[3]))
assert receipt['kind']=='upgrade_direct_fpv9' and receipt['status']=='completed'
assert receipt['backup_path'] and receipt['backup_sha256'].startswith('sha256:')
assert status['install_state']['database_schema_current']=='43'
assert status['install_state'].get('previous') is None
base=pathlib.Path('/etc/systemd/system')
for unit,role in [('xinsight.service','service_delivery_dropin'),('xinsight-feed.service','feed_delivery_dropin'),('xinsight-source-facts.service','source_facts_delivery_dropin')]:
    old=base/(unit+'.d')/'50-github-program-20260927.conf'
    retired=pathlib.Path(str(old)+'.retired-by-'+receipt['operation_id'])
    assert not old.exists() and retired.is_file()
    assert 'sha256:'+hashlib.sha256(retired.read_bytes()).hexdigest()==proof['files'][role]
    egress=base/(unit+'.d')/'40-xinsight-reviewed-source-egress.conf'
    assert egress.read_text()=='[Service]\nIPAddressAllow=\nIPAddressAllow=localhost\nIPAddressAllow=203.0.113.10/32\n'
for role,path in [('service_content_dropin',base/'xinsight.service.d/30-content-credentials.conf'),
                  ('source_facts_credentials_dropin',base/'xinsight-source-facts.service.d/10-source-credentials.conf')]:
    assert 'sha256:'+hashlib.sha256(path.read_bytes()).hexdigest()==proof['files'][role]
print(json.dumps({'schema_version':'xinsight.ci-direct-fpv9-upgrade-assertions.v1',
 'source':'exact_fpv9','target_schema':43,'backup_present':True,'retired_program_dropins':3,
 'credential_dropins_preserved':2,'reviewed_egress_dropins':3},sort_keys=True))
PY
if sudo env PATH="$CTL_PATH" "$CTL" rollback --check --previous > "$QUAL_ROOT/rollback.stdout" 2> "$QUAL_ROOT/rollback.stderr"; then
  echo "unsigned FPV9 predecessor was unexpectedly accepted as an application rollback target" >&2
  exit 1
fi
grep -F 'managed previous release is required for application rollback' "$QUAL_ROOT/rollback.stderr" >/dev/null

BACKUP_PATH="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["backup_path"])' "$QUAL_ROOT/upgrade-receipt.json")"
BACKUP_SHA256="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["backup_sha256"])' "$QUAL_ROOT/upgrade-receipt.json")"
sudo test "$(sudo sha256sum "$BACKUP_PATH" | cut -d' ' -f1)" = "${BACKUP_SHA256#sha256:}"
sudo install -m 0600 -o "$(id -u)" -g "$(id -g)" "$BACKUP_PATH" "$QUAL_ROOT/fpv9-backup.dump"

# Independently prove that the owned backup is a genuine schema-40 PostgreSQL
# recovery source. This database is not the managed restore target.
stage schema40_backup_restore
PGPASSWORD="$ADMIN_PASSWORD" /usr/lib/postgresql/16/bin/createdb -h 127.0.0.1 -U xinsight_admin \
  --maintenance-db xinsight --owner xinsight_admin --template template0 --encoding UTF8 xinsight_restore_old_ci
PGPASSWORD="$ADMIN_PASSWORD" /usr/lib/postgresql/16/bin/pg_restore -h 127.0.0.1 -U xinsight_admin \
  -d xinsight_restore_old_ci --no-owner --no-privileges --exit-on-error --single-transaction "$QUAL_ROOT/fpv9-backup.dump" \
  > "$QUAL_ROOT/old-restore-command.txt"
test "$(PGPASSWORD="$ADMIN_PASSWORD" /usr/lib/postgresql/16/bin/psql -At -h 127.0.0.1 -U xinsight_admin -d xinsight_restore_old_ci -c 'SELECT count(*) FROM schema_migrations')" = 40
stable_snapshot xinsight_restore_old_ci > "$QUAL_ROOT/old-restore-snapshot.json"
cmp "$QUAL_ROOT/before-check-snapshot.json" "$QUAL_ROOT/old-restore-snapshot.json"

stage managed_forward_restore
sudo env PATH="$CTL_PATH" "$CTL" restore --check --backup "$BACKUP_PATH" --backup-sha256 "$BACKUP_SHA256" \
  --new-database xinsight_restore_managed_ci > "$QUAL_ROOT/restore-check.json"
sudo env PATH="$CTL_PATH" "$CTL" restore --backup "$BACKUP_PATH" --backup-sha256 "$BACKUP_SHA256" \
  --new-database xinsight_restore_managed_ci --reason-code field_validation > "$QUAL_ROOT/restore-receipt.json"
stage final_assertions
wait_ready
sudo env PATH="$CTL_PATH" "$CTL" status > "$QUAL_ROOT/final-status.json"
sudo sha256sum /etc/x-insight/access-accounts.json > "$QUAL_ROOT/after-restore-account.sha256"
cmp "$QUAL_ROOT/before-account.sha256" "$QUAL_ROOT/after-restore-account.sha256"
test "$(PGPASSWORD="$ADMIN_PASSWORD" /usr/lib/postgresql/16/bin/psql -At -h 127.0.0.1 -U xinsight_admin -d xinsight_restore_managed_ci -c 'SELECT count(*) FROM schema_migrations')" = 43
PGPASSWORD="$ADMIN_PASSWORD" /usr/lib/postgresql/16/bin/psql -At -h 127.0.0.1 -U xinsight_admin -d xinsight_restore_managed_ci \
  -v ON_ERROR_STOP=1 -v stream_id="$STREAM_ID" -v collection_id="$COLLECTION_ID" > "$QUAL_ROOT/managed-restore-state.txt" <<'SQL'
SELECT status||'|'||cursor_value||'|'||high_watermark||'|'||requires_attention::text FROM source_fact_streams WHERE id=:'stream_id'::uuid;
SELECT status||'|'||cursor_value||'|'||last_sequence||'|'||requires_attention::text FROM source_content_collections WHERE id=:'collection_id'::uuid;
SELECT count(*) FROM source_content_manifests WHERE collection_id=:'collection_id'::uuid;
SELECT count(*) FROM source_content_receipts WHERE collection_id=:'collection_id'::uuid;
SQL
python3 - "$QUAL_ROOT/managed-restore-state.txt" "$QUAL_ROOT/final-status.json" "$QUAL_ROOT/restore-receipt.json" <<'PY' > "$QUAL_ROOT/qualification-result.json"
import json,pathlib,sys
rows=pathlib.Path(sys.argv[1]).read_text().splitlines()
status=json.load(open(sys.argv[2])); receipt=json.load(open(sys.argv[3]))
assert rows==['paused|ci-old-cursor|ci-old-hwm|true','paused|ci-content-cursor|1|true','1','1'],rows
assert receipt['kind']=='restore' and receipt['status']=='completed'
assert status['install_state']['database_schema_current']=='43'
print(json.dumps({'schema_version':'xinsight.ci-direct-fpv9-qualification.v1',
 'classification':'disposable_ubuntu22_ci_not_target','source':'exact_fpv9_linux_binaries_schema40',
 'target_schema':43,'read_only_check':'pass','external_sigkill_phase':'restart_service',
 'resume':'completed','backup_schema40_isolated_restore':'pass','managed_forward_restore':'pass',
 'source_cursor_preserved':True,'content_ref_and_receipt_preserved':True,
 'old_program_dropins_retired':3,'credential_dropins_preserved':2,
 'unsigned_predecessor_rollback':'rejected'},indent=2,sort_keys=True))
PY
write_stage_result passed 0
printf '%s\n' 'direct_fpv9_qualification=pass'
