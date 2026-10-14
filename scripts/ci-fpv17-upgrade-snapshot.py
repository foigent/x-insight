#!/usr/bin/env python3
"""Private, read-only snapshot for the disposable FPV17->FPV19 host lane."""

import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
from urllib.parse import unquote, urlsplit


if (os.geteuid() != 0 or os.environ.get("GITHUB_ACTIONS") != "true"
        or os.environ.get("XINSIGHT_CI_EPHEMERAL_HOST") != "1" or len(sys.argv) != 1):
    raise SystemExit("disposable root CI host required")


def command(*args, env=None):
    return subprocess.run(args, check=True, capture_output=True, text=True, env=env, timeout=15).stdout.strip()


def file_identity(name):
    path = Path(name)
    info = path.stat()
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
        raise SystemExit("required regular single-link host file unavailable")
    return [stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid,
            hashlib.sha256(path.read_bytes()).hexdigest()]


dsn_path = Path("/etc/x-insight/database/current/postgres-admin.dsn")
dsn = urlsplit(dsn_path.read_text().strip())
if (dsn.scheme != "postgres" or dsn.hostname != "127.0.0.1" or dsn.port != 5432
        or unquote(dsn.username or "") != "xinsight_admin" or dsn.path != "/xinsight"
        or not dsn.password):
    raise SystemExit("unexpected isolated PostgreSQL credential")
pg_env = dict(os.environ, PGHOST="127.0.0.1", PGPORT="5432", PGUSER="xinsight_admin",
              PGDATABASE="xinsight", PGPASSWORD=unquote(dsn.password))
tables = (
    "schema_migrations", "source_fact_documents", "source_fact_streams",
    "source_content_collections", "source_content_manifests", "source_content_receipts",
    "adapter_import_states", "review_body_operations", "workbench_access_grants",
    "work_event_collections", "work_event_documents", "workbench_economic_facts",
    "connector_installation_source_bindings",
)
database = {}
for table in tables:
    sql = ("SELECT count(*)::text || ':' || md5(COALESCE(jsonb_agg(to_jsonb(t) "
           "ORDER BY to_jsonb(t)::text)::text,'[]')) FROM public." + table + " t")
    value = command("/usr/lib/postgresql/16/bin/psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1",
                    "-c", sql, env=pg_env)
    if not re.fullmatch(r"[0-9]+:[0-9a-f]{32}", value):
        raise SystemExit("invalid database snapshot")
    database[table] = value
for label, sql in {
    "installation_identity": "SELECT md5(COALESCE(jsonb_agg(jsonb_build_array(id,deployment_id,control_service_identity_id,installation_acknowledgement_sha256,source_instance_sha256) ORDER BY id)::text,'[]')) FROM connector_installations",
    "table_acl": "SELECT md5(COALESCE(jsonb_agg(jsonb_build_array(n.nspname,c.relname,c.relacl::text) ORDER BY n.nspname,c.relname)::text,'[]')) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p','v','m','S')",
    "role_boundary": "SELECT md5(COALESCE(jsonb_agg(jsonb_build_array(rolname,rolsuper,rolcreaterole,rolcreatedb,rolcanlogin,rolconnlimit) ORDER BY rolname)::text,'[]')) FROM pg_roles WHERE rolname LIKE 'xinsight_%'",
}.items():
    value = command("/usr/lib/postgresql/16/bin/psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1",
                    "-c", sql, env=pg_env)
    if not re.fullmatch(r"[0-9a-f]{32}", value):
        raise SystemExit("invalid grant snapshot")
    database[label] = value

immutable_rows = {}
for table in ("source_fact_documents", "source_content_manifests", "source_content_receipts",
              "review_body_operations", "workbench_economic_facts"):
    sql = ("SELECT COALESCE(jsonb_agg(row_sha ORDER BY row_sha)::text,'[]') FROM "
           "(SELECT md5(to_jsonb(t)::text) AS row_sha FROM public." + table + " t) rows")
    immutable_rows[table] = json.loads(command("/usr/lib/postgresql/16/bin/psql", "-X", "-A", "-t",
                                               "-v", "ON_ERROR_STOP=1", "-c", sql, env=pg_env))

progress = {}
for label, sql in {
    "source_fact": "SELECT COALESCE(jsonb_agg(jsonb_build_array(id,cursor_value,high_watermark) ORDER BY id)::text,'[]') FROM source_fact_streams",
    "source_content": "SELECT COALESCE(jsonb_agg(jsonb_build_array(id,cursor_value,last_sequence,high_watermark_sequence,high_watermark) ORDER BY id)::text,'[]') FROM source_content_collections",
    "economic_feed": "SELECT COALESCE(jsonb_agg(jsonb_build_array(id,cursor_value,high_watermark) ORDER BY id)::text,'[]') FROM adapter_import_states",
}.items():
    progress[label] = json.loads(command("/usr/lib/postgresql/16/bin/psql", "-X", "-A", "-t",
                                         "-v", "ON_ERROR_STOP=1", "-c", sql, env=pg_env))

status = json.loads(command("/opt/x-insight/current/bin/xinsightctl", "status"))
status.pop("generated_at", None)
pid = int(command("systemctl", "show", "xinsight.service", "--property=MainPID", "--value"))
invocation = command("systemctl", "show", "xinsight.service", "--property=InvocationID", "--value")
if pid < 1 or not re.fullmatch(r"[0-9a-f]{32}", invocation):
    raise SystemExit("server invocation unavailable")
release = str(Path("/opt/x-insight/current").resolve(strict=True))
server_exe = str(Path("/proc").joinpath(str(pid), "exe").resolve(strict=True))
if server_exe != release + "/bin/xinsight-server":
    raise SystemExit("running server does not match current release")
worker_exes = {}
for unit in ("xinsight-feed.service", "xinsight-source-facts.service"):
    if subprocess.run(("systemctl", "is-active", "--quiet", unit), check=False).returncode == 0:
        worker_pid = int(command("systemctl", "show", unit, "--property=MainPID", "--value"))
        if worker_pid < 1:
            raise SystemExit("active worker PID unavailable")
        worker_exes[unit] = str(Path("/proc").joinpath(str(worker_pid), "exe").resolve(strict=True))
files = {name: file_identity(name) for name in (
    "/etc/x-insight/review-body.key", "/etc/x-insight/xinsight-review-body.env",
    "/etc/x-insight/xinsight-connector-id.env", "/etc/x-insight/xinsight-connector.env",
    "/etc/x-insight/trusted-release-keys.json",
)}
print(json.dumps({"schema_version": "xinsight.ci-fpv17-upgrade-snapshot.v1",
                  "classification": "ci_private_synthetic_not_target", "status": status,
                  "release": release, "server_invocation": invocation, "server_exe": server_exe,
                  "database": database, "immutable_rows": immutable_rows,
                  "progress": progress, "worker_exes": worker_exes,
                  "files": files}, sort_keys=True))
