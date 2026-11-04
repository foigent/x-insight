#!/usr/bin/env python3
"""Private, read-only snapshot for the disposable FPV17/FPV20->FPV21 host lane."""

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
def valid_snapshot_database(dsn, profile):
    port={"container":5432,"native":54554}.get(profile)
    return (port is not None and dsn.scheme=="postgres" and dsn.hostname=="127.0.0.1" and dsn.port==port
        and unquote(dsn.username or "")=="xinsight_admin" and dsn.path=="/xinsight" and bool(dsn.password))
profile=os.environ.get("XINSIGHT_CI_DATABASE_PROFILE","container")
if not valid_snapshot_database(dsn,profile):
    raise SystemExit("unexpected isolated PostgreSQL credential")
pg_env = dict(os.environ, PGHOST="127.0.0.1", PGPORT=str(dsn.port), PGUSER="xinsight_admin",
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

# The current upgrade appends only reviewed migrations46/47 after its exact predecessor. Retain
# the original rows too, so additions cannot mask edits to predecessor history
# or privileges behind a changed whole-database digest.
migration_rows = json.loads(command("/usr/lib/postgresql/16/bin/psql", "-X", "-A", "-t",
    "-v", "ON_ERROR_STOP=1", "-c",
    "SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version)::text,'[]') FROM schema_migrations m", env=pg_env))
table_acl_rows = json.loads(command("/usr/lib/postgresql/16/bin/psql", "-X", "-A", "-t",
    "-v", "ON_ERROR_STOP=1", "-c",
    "SELECT COALESCE(jsonb_object_agg(c.relname,c.relacl::text ORDER BY c.relname)::text,'{}') "
    "FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
    "WHERE n.nspname='public' AND c.relkind IN ('r','p','v','m','S')", env=pg_env))
sync_privileges = {}
sync_tables = "'connector_sync_controls','connector_sync_slots','connector_sync_runs'"
for label, sql in {
    "owners": "SELECT COALESCE(jsonb_object_agg(c.relname,pg_get_userbyid(c.relowner))::text,'{}') "
        "FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
        "WHERE n.nspname='public' AND c.relname IN ("+sync_tables+")",
    "tables": "SELECT COALESCE(jsonb_agg(jsonb_build_array(c.relname,CASE WHEN x.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(x.grantee) END,x.privilege_type,x.is_grantable) "
        "ORDER BY c.relname,x.grantee,x.privilege_type)::text,'[]') "
        "FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
        "CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl,acldefault('r',c.relowner))) x "
        "WHERE n.nspname='public' AND c.relname IN ("+sync_tables+") AND x.grantee<>c.relowner",
    "columns": "SELECT COALESCE(jsonb_agg(jsonb_build_array(c.relname,a.attname,CASE WHEN x.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(x.grantee) END,x.privilege_type,x.is_grantable) "
        "ORDER BY c.relname,a.attname,x.grantee,x.privilege_type)::text,'[]') "
        "FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace "
        "CROSS JOIN LATERAL aclexplode(a.attacl) x "
        "WHERE n.nspname='public' AND c.relname IN ("+sync_tables+") AND a.attnum>0 AND NOT a.attisdropped AND x.grantee<>c.relowner",
    "functions": "SELECT COALESCE(jsonb_agg(jsonb_build_array(p.proname,CASE WHEN x.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(x.grantee) END,x.privilege_type,x.is_grantable) "
        "ORDER BY p.proname,x.grantee,x.privilege_type)::text,'[]') "
        "FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace "
        "CROSS JOIN LATERAL aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) x "
        "WHERE n.nspname='public' AND p.proname IN ('xinsight_sync_scope','xinsight_sync_receiver','xinsight_sync_units','xinsight_init_connector_sync') AND x.grantee<>p.proowner",
}.items():
    sync_privileges[label] = json.loads(command("/usr/lib/postgresql/16/bin/psql", "-X", "-A", "-t",
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
# Account values remain private. Compare stable imported identities and PHC
# hashes via one aggregate digest; never include credentials in safe evidence.
def account_inventory(rows):
    rows=sorted(rows,key=lambda row:row[0])
    return dict(count=len(rows),sha256=hashlib.sha256(json.dumps(rows,separators=(',',':'),ensure_ascii=False).encode()).hexdigest())
legacy=json.loads(Path('/etc/x-insight/access-accounts.json').read_text())
legacy_account_inventory=account_inventory([[a[k] for k in ('subject_id','username','role','disabled','password_hash')] for a in legacy['accounts']])
account_inventory_value=None
account_boundary=None
account_state_digests=None
account_ticket_summary=None
present=command('/usr/lib/postgresql/16/bin/psql','-X','-A','-t','-v','ON_ERROR_STOP=1','-c',
                "SELECT to_regclass('xinsight_accounts.authority') IS NOT NULL",env=pg_env)
if present=='t':
    rows=json.loads(command('/usr/lib/postgresql/16/bin/psql','-X','-A','-t','-v','ON_ERROR_STOP=1','-c',
        "SELECT COALESCE(jsonb_agg(jsonb_build_array(subject_id,username,role,disabled,password_hash) ORDER BY subject_id)::text,'[]') FROM xinsight_accounts.accounts",env=pg_env))
    account_inventory_value=account_inventory(rows)
    account_ticket_summary=json.loads(command('/usr/lib/postgresql/16/bin/psql','-X','-A','-t','-v','ON_ERROR_STOP=1','-c',
        "SELECT jsonb_build_object('total',count(*),'pending',count(*) FILTER(WHERE consumed_at IS NULL)) FROM xinsight_accounts.password_tickets",env=pg_env))

    account_state_digests={}
    for table in ('authority','accounts','password_tickets'):
        value=command('/usr/lib/postgresql/16/bin/psql','-X','-A','-t','-v','ON_ERROR_STOP=1','-c',
            "SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text,'[]') FROM xinsight_accounts."+table+" t",env=pg_env)
        account_state_digests[table]=hashlib.sha256(value.encode()).hexdigest()

    account_boundary=json.loads(command('/usr/lib/postgresql/16/bin/psql','-X','-A','-t','-v','ON_ERROR_STOP=1','-c',
        """SELECT jsonb_build_object('initialized',(SELECT initialized FROM xinsight_accounts.authority WHERE singleton),
        'roles_separated',NOT has_schema_privilege('xinsight_reader','xinsight_accounts','USAGE')
        AND NOT has_schema_privilege('xinsight_importer','xinsight_accounts','USAGE')
        AND has_schema_privilege('xinsight_workspace','xinsight_accounts','USAGE')
        AND NOT has_schema_privilege('xinsight_workspace','xinsight_accounts','CREATE')
        AND has_table_privilege('xinsight_workspace','xinsight_accounts.accounts','SELECT')
        AND has_table_privilege('xinsight_workspace','xinsight_accounts.accounts','INSERT')
        AND has_table_privilege('xinsight_workspace','xinsight_accounts.accounts','UPDATE')
        AND has_table_privilege('xinsight_workspace','xinsight_accounts.password_tickets','SELECT')
        AND has_table_privilege('xinsight_workspace','xinsight_accounts.password_tickets','INSERT')
        AND has_table_privilege('xinsight_workspace','xinsight_accounts.password_tickets','UPDATE')
        AND NOT has_table_privilege('xinsight_workspace','xinsight_accounts.accounts','DELETE,TRUNCATE,TRIGGER,REFERENCES')
        AND NOT has_table_privilege('xinsight_workspace','xinsight_accounts.authority','INSERT,UPDATE,DELETE,TRUNCATE')
        AND NOT EXISTS(SELECT 1 FROM pg_tables WHERE schemaname='xinsight_accounts'
          AND (has_table_privilege('xinsight_reader',format('%I.%I',schemaname,tablename),'SELECT,INSERT,UPDATE,DELETE')
          OR has_table_privilege('xinsight_importer',format('%I.%I',schemaname,tablename),'SELECT,INSERT,UPDATE,DELETE')))
        AND EXISTS(SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
          WHERE n.nspname='xinsight_accounts' AND p.proname='set_project_read_grant'
          AND p.prosecdef AND has_function_privilege('xinsight_workspace',p.oid,'EXECUTE')
          AND NOT has_function_privilege('xinsight_reader',p.oid,'EXECUTE')
          AND NOT has_function_privilege('xinsight_importer',p.oid,'EXECUTE')
          AND NOT EXISTS(SELECT 1 FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')),
        'function_owner',(SELECT pg_get_userbyid(p.proowner) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
          WHERE n.nspname='xinsight_accounts' AND p.proname='set_project_read_grant'))""",env=pg_env))
print(json.dumps({"schema_version": "xinsight.ci-fpv17-upgrade-snapshot.v1",
                  "classification": "ci_private_synthetic_not_target", "status": status,
                  "release": release, "server_invocation": invocation, "server_exe": server_exe,
                  "database": database, "immutable_rows": immutable_rows,
                  "migration_rows": migration_rows, "table_acl_rows": table_acl_rows,
                  "sync_privileges": sync_privileges,
                  "legacy_account_inventory": legacy_account_inventory,
                  "account_inventory": account_inventory_value,"account_boundary":account_boundary,"account_state_digests":account_state_digests,"account_ticket_summary":account_ticket_summary,
                  "progress": progress, "worker_exes": worker_exes,
                  "files": files}, sort_keys=True))
