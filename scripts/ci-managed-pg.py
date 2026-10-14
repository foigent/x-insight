#!/usr/bin/env python3
"""Exact disposable FPV11 fixture; root DSN travels only in child environment."""
import json
import os
from pathlib import Path
import subprocess
import sys
from urllib.parse import urlsplit, unquote

SOURCE = '11111111-1111-4111-8111-111111111111'
COLLECTION = '7996279c-df2b-4c0d-aaba-7c2a3b3d510b'
PROFILES = ['unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1',
            'unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1',
            'unirouter.policy-observation.v1','unirouter.destination-observation.v1']

def stream(i):
    return f'22222222-2222-4222-8222-{i+1:012d}'

def configuration(partition=False):
    project = '87' if partition else '86'
    key = '/etc/x-insight/source-managed-33333333-3333-4333-8333-333333333333.key' if partition else '/etc/x-insight/ci-source.key'
    profiles = PROFILES[:1] if partition else PROFILES
    entries = []
    for i, profile in enumerate(profiles):
        entries.append(dict(stream_id='33333333-3333-4333-8333-333333333334' if partition else stream(i),
           source_system_id=SOURCE, source_instance='ci-source', org_id='2', project_id=project,
           profile=profile, scope_generation='1', endpoint='https://203.0.113.10/api/v1/integrations/xinsight/'+('source-facts' if i<3 else 'source/v2/events'),
           key_file=key, allow_private_tunnel_http=False))
    bindings=[]
    for scope in (['legacy'] if partition else ['legacy','content']):
        bindings.append(dict(name='ci-'+scope,scope=scope,source_system_id=SOURCE,source_instance='ci-source',org_id='2',project_id=project,
            credential_id='ci-content-credential' if scope=='content' else 'ci-'+scope+'-credential',registration_hash='sha256:'+('a' if scope=='content' else 'b')*64,
            key_file='/etc/x-insight/ci-content.key' if scope=='content' else key))
    contents=[] if partition else [dict(collection_id=COLLECTION,source_system_id=SOURCE,source_instance='ci-source',org_id='2',project_id='86',credential_binding='ci-content',content_scope_revision='2',recovery_epoch='1',allowed_purposes=['human_review'],coverage_from=dict(time='2026-09-25T00:00:00.000000Z',after_sequence='0'),endpoint='https://203.0.113.10/api/v1/integrations/xinsight/source/v2/content-manifests',allow_private_tunnel_http=False)]
    if partition:
        return dict(schema_version='xinsight.managed-source-facts.v2',poll_interval_seconds=5,retry_maximum_seconds=20,profiles=entries)
    return dict(schema_version='xinsight.managed-source-facts.v4',poll_interval_seconds=5,retry_maximum_seconds=20,profiles=entries,bindings=bindings,contents=contents)

mode = sys.argv[1] if len(sys.argv)==2 else ''
if mode in {'config','partition'}:
    print(json.dumps(configuration(mode=='partition'),sort_keys=True,separators=(',',':')))
    raise SystemExit(0)
if mode not in {'seed','seed-partition','snapshot','importer-nologin','importer-is-nologin'}:
    raise SystemExit('usage: ci-managed-pg.py seed|seed-partition|snapshot|config|partition|importer-nologin|importer-is-nologin')
if os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('XINSIGHT_CI_EPHEMERAL_HOST')!='1' or os.geteuid()!=0:
    raise SystemExit('database fixture requires explicitly disposable root Actions host')
path = Path(os.environ['XINSIGHT_ADMIN_POSTGRES_DSN_FILE'])
if str(path) != '/etc/x-insight/database/current/postgres-admin.dsn':
    raise SystemExit('unexpected credential reference')
u = urlsplit(path.read_text().strip())
if u.scheme!='postgres' or u.hostname!='127.0.0.1' or u.username!='xinsight_admin' or u.port!=5432:
    raise SystemExit('unexpected synthetic database identity')
env = dict(os.environ,PGHOST=u.hostname,PGPORT=str(u.port),PGUSER=u.username,PGPASSWORD=unquote(u.password or ''),PGDATABASE=u.path[1:],PGSSLMODE='disable')
if mode=='importer-nologin':
    sql="ALTER ROLE xinsight_importer NOLOGIN;"
elif mode=='importer-is-nologin':
    sql="DO $$ BEGIN IF (SELECT rolcanlogin FROM pg_roles WHERE rolname='xinsight_importer') IS DISTINCT FROM false THEN RAISE EXCEPTION 'importer prior NOLOGIN not preserved'; END IF; END $$;"
elif mode=='seed':
    queries=[f"INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES('{SOURCE}','ci-managed-fpv11','unirouter','CI synthetic managed predecessor');"]
    for i,profile in enumerate(PROFILES):
        queries.append(f"INSERT INTO source_fact_streams(id,source_system_id,source_instance,org_id,project_id,profile,scope_generation,status,cursor_value,high_watermark) VALUES('{stream(i)}','{SOURCE}','ci-source','2','86','{profile}','1','active','ci-old-cursor','ci-old-hwm');")
    sql='\n'.join(queries)
elif mode=='seed-partition':
    sql=f"INSERT INTO source_fact_streams(id,source_system_id,source_instance,org_id,project_id,profile,scope_generation,status) VALUES('33333333-3333-4333-8333-333333333334','{SOURCE}','ci-source','2','87','unirouter.execution-context.v1','1','active');"
else:
    sql=f"""SELECT jsonb_build_object(
      'streams',(SELECT jsonb_agg(jsonb_build_array(id,source_instance,org_id,project_id,profile,scope_generation,status,cursor_value,high_watermark) ORDER BY id) FROM source_fact_streams WHERE source_system_id='{SOURCE}'),
      'content',(SELECT jsonb_build_array(source_instance,org_id,project_id,credential_id,registration_hash,content_scope_revision,recovery_epoch,status,cursor_value,last_sequence,high_watermark_sequence) FROM source_content_collections WHERE id='{COLLECTION}'),
      'manifests',(SELECT jsonb_agg(jsonb_build_array(sequence,source_fact_id,source_sha256,source_json->>'content_ref') ORDER BY sequence) FROM source_content_manifests WHERE collection_id='{COLLECTION}'),
      'receipts',(SELECT count(*) FROM source_content_receipts WHERE collection_id='{COLLECTION}'),
      'evidence',(SELECT count(*) FROM evidence_events))::text;"""
subprocess.run(['/usr/lib/postgresql/16/bin/psql','-X','-At','-v','ON_ERROR_STOP=1'],input=sql,text=True,env=env,check=True)
