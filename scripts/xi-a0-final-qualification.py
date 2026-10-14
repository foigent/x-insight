#!/usr/bin/env python3
"""Prepare a bound qualification run; --execute requires separate manager authorization.
No cleanup entry. Preparation never clones a cluster or starts PostgreSQL.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time

sys.dont_write_bytecode=True
REPO=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('xi_qualification_helpers',REPO/'scripts/xi-a0-plan-cache-diagnostic.py')
prior=importlib.util.module_from_spec(spec);spec.loader.exec_module(prior)
common,PG,ENV,GIB=prior.common,prior.PG,prior.ENV,prior.GIB
TEMPLATE=Path('/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-checkpoint-run-eeh92u0h/template/data')
MANIFEST=REPO/'output/xi-a0-checkpoint/template-files.json'
FIXTURE=REPO/'output/xi-a0-checkpoint/fixture.json'
BUILD_BINDING=REPO/'output/xi-a0-read-mode-final-run/code-binding.json'
READY=REPO/'output/xi-a0-final-qualification-ready'
OUT=REPO/'output/xi-a0-final-code-qualification'
RESOURCE_PREP=REPO/'output/xi-a0-resource-preparation'
PLAN=REPO/'docs/planning/xi-a0-qualification-resource-disposition-2026-09-09.md'
LIMIT=7*GIB;START_FREE=28*GIB
SETTINGS={'max_wal_size':'1GB','shared_buffers':'128MB','work_mem':'4MB','wal_buffers':'4MB','checkpoint_timeout':'5min','checkpoint_completion_target':'0.9','track_io_timing':'on','track_wal_io_timing':'on','log_checkpoints':'on'}
COUNT_SQL="SELECT jsonb_build_array((SELECT count(*) FROM workbench_readings),(SELECT count(*) FROM workbench_reading_members),(SELECT count(*) FROM workbench_saved_filters),(SELECT count(*) FROM workbench_saved_filter_operations),(SELECT count(*) FROM workbench_economic_facts),(SELECT count(*) FROM evidence_events))"
CONSTRAINT_SQL="SELECT jsonb_agg(jsonb_build_object('name',conname,'validated',convalidated,'definition',pg_get_constraintdef(oid)) ORDER BY conname) FROM pg_constraint WHERE conrelid IN ('workbench_economic_facts'::regclass,'workbench_reading_members'::regclass)"
TRIGGER_SQL="SELECT jsonb_agg(jsonb_build_object('name',tgname,'enabled',tgenabled,'internal',tgisinternal,'definition',pg_get_triggerdef(oid)) ORDER BY tgname) FROM pg_trigger WHERE tgrelid IN ('workbench_economic_facts'::regclass,'workbench_reading_members'::regclass)"


def stopped(data):
 if data.is_symlink() or (data/'postmaster.pid').exists():raise RuntimeError('expected stopped non-symlink cluster: '+str(data))
 control=common.capture([PG/'pg_controldata',data])
 if not re.search(r'Database cluster state:\s+shut down\s*$',control,re.M):raise RuntimeError('cluster not shut down')
 return control


def startup_guard(free_bytes):
 if free_bytes<START_FREE:raise RuntimeError('below 28GiB before allocation; no clone or PG startup')


def verify_template():
 stopped(TEMPLATE)
 if any((TEMPLATE/'pg_tblspc').iterdir()):raise RuntimeError('external tablespace not covered')
 permissions=common.capture(['/bin/ls','-leAR',TEMPLATE])
 if re.search(r'^\s*\d+:',permissions,re.M):raise RuntimeError('template ACL needs reviewed copy handling')
 inventory=prior.inventory(TEMPLATE)
 actual={p:{'bytes':v['bytes'],'sha256':v['sha256']} for p,v in inventory.items() if v['type']=='file'}
 if actual!=json.loads(MANIFEST.read_text()):raise RuntimeError('template changed')
 return inventory


def build_binding():
 original=json.loads(BUILD_BINDING.read_text())
 if original['binary_sha256']!='ab3c14624597b149bea302313c70060306aad2b456152ea3a40425b581d042b9' or len(original['sources'])!=361:raise RuntimeError('unexpected approved build')
 for p,h in [(REPO/p,h) for p,h in original['sources'].items()]+[(Path(original['binary']),original['binary_sha256'])]:
  if common.digest(p)!=h:raise RuntimeError('compiled input changed: '+str(p))
 return original


def copy_projection(inventory):
 fields={'type','mode','uid','gid','bytes','sha256'}
 return {p:{k:v for k,v in row.items() if k in fields} for p,row in inventory.items()}


def expected_migrations():
 return [{'id':p.name.removesuffix('.up.sql'),'sha256':'sha256:'+common.digest(p),'dirty':False} for p in sorted((REPO/'migrations').glob('*.up.sql'))]


def validate_preflight(before,baseline):
 if before['counts']!=[0,0,0,0,500020,500100] or before['migrations']!=expected_migrations():raise RuntimeError('wrong empty prestate/migration ledger')
 if before['session']['mode']!='auto' or before['session']['preload']!='' or before['session']['wal_buffers']!='4MB' or not before['session']['server_version'].startswith('16.14'):raise RuntimeError('wrong runtime settings')
 if before['constraints']!=baseline['constraints'] or before['triggers']!=baseline['triggers']:raise RuntimeError('constraints/guards differ')


def prepare():
 if READY.exists():raise RuntimeError('preparation exists; preserve it')
 original=build_binding();before=verify_template()
 READY.mkdir()
 common.save(READY/'template-before.json',before)
 paths=[Path(__file__),REPO/'scripts/xi-a0-plan-cache-diagnostic.py',REPO/'scripts/xi-a0-checkpoint-diagnostic.py',PLAN,MANIFEST,FIXTURE,BUILD_BINDING,REPO/'output/xi-a0-checkpoint/B1.json',READY/'template-before.json']
 binding={'prepared_at':common.utc(),'base_commit':common.capture(['git','rev-parse','HEAD'],cwd=REPO),'binary':original['binary'],'binary_sha256':original['binary_sha256'],'sources':original['sources'],'files':{str(p):common.digest(p) for p in paths},'template':str(TEMPLATE),'settings':SETTINGS,'limits':{'new_bytes':LIMIT,'allocation_free_bytes':START_FREE,'stop_free_bytes':21*GIB,'reserve_bytes':20*GIB,'rss_bytes':4*GIB,'seconds':1200},'cache_baseline_bytes':common.size(Path(ENV['GOCACHE'])),'execution_status':'not executed; resource disposition and performance window pending manager agreement'}
 common.save(READY/'code-binding.json',binding)
 free=shutil.disk_usage('/private/tmp').free
 common.save(READY/'resource-preflight.json',{'at':common.utc(),'free_bytes':free,'startup_gap_bytes':max(0,START_FREE-free),'resource_ready':free>=START_FREE,'pg_started':False,'cloned':False,'binary_reused':True})
 print(json.dumps({'prepared':str(READY),'resource_ready':free>=START_FREE,'startup_gap_bytes':max(0,START_FREE-free),'executed':False}))


def execute():
 if OUT.exists():raise RuntimeError('execution output exists; no automatic retry')
 code=json.loads((READY/'code-binding.json').read_text())
 for p,h in [(REPO/p,h) for p,h in code['sources'].items()]+[(Path(p),h) for p,h in code['files'].items()]+[(Path(code['binary']),code['binary_sha256'])]:
  if common.digest(p)!=h:raise RuntimeError('bound input changed: '+str(p))
 if code['template']!=str(TEMPLATE) or code['settings']!=SETTINGS or code['limits']!={'new_bytes':LIMIT,'allocation_free_bytes':START_FREE,'stop_free_bytes':21*GIB,'reserve_bytes':20*GIB,'rss_bytes':4*GIB,'seconds':1200}:raise RuntimeError('resource/settings binding changed')
 startup_guard(shutil.disk_usage('/private/tmp').free)
 started=time.time();source_before=verify_template()
 if source_before!=json.loads((READY/'template-before.json').read_text()):raise RuntimeError('template metadata changed')
 common.process_rss([os.getpid()])
 startup_guard(shutil.disk_usage('/private/tmp').free)
 root=Path(tempfile.mkdtemp(prefix='xi-final-qual-',dir='/private/tmp'));data=root/'data';OUT.mkdir()
 cfg={'paths':[str(root),str(READY),str(OUT),str(RESOURCE_PREP)],'cache_baseline_bytes':code['cache_baseline_bytes'],'cache_increment':max(0,common.size(Path(ENV['GOCACHE']))-code['cache_baseline_bytes']),'runner_pid':os.getpid(),'data':str(data),'started_unix':started,'new_limit_bytes':LIMIT,'output':str(OUT)}
 report={'started_at':common.utc(),'root':str(root),'source':str(TEMPLATE),'complete':False,'qualification':False,'pg_started':False,'stopped':True,'planned_300k_attempts':20,'boundaries':[]};proc=None;port=None
 fixture=json.loads(FIXTURE.read_text());log_path=root/'postgres.log'
 def persist():common.save(OUT/'run.json',report)
 def check(stage):
  row=prior.resources(cfg);report['boundaries'].append({'stage':stage,**row});persist()
  if not row['within_limits']:raise RuntimeError('resource stop: '+stage)
  return row
 def query(sql):return json.loads(common.psql(port,fixture['database'],"SET statement_timeout='10s'; "+sql))
 common.save(OUT/'resource-config.json',cfg);common.save(OUT/'code-binding.json',code);common.save(OUT/'source-before.json',source_before);persist()
 try:
  startup_guard(check('before_copy')['free_bytes'])
  shutil.copytree(TEMPLATE,data)
  check('after_copy')
  copied=prior.inventory(data)
  if copy_projection(copied)!=copy_projection(source_before):raise RuntimeError('copied bytes/types/ownership/modes differ')
  common.save(OUT/'template-match.json',{'matched':True,'files':sum(v['type']=='file' for v in copied.values()),'manifest_sha256':common.digest(MANIFEST)})
  check('before_pg_start')
  with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
  if len(os.fsencode(str(root/'.s.PGSQL.65535')))>103:raise RuntimeError('socket path too long')
  options=['-h','127.0.0.1','-p',str(port),'-k',str(root)]
  for k,v in SETTINGS.items():options+=['-c',k+'='+v]
  report.update(port=port,startup_options=options,stopped=False);persist()
  subprocess.run([str(PG/'pg_ctl'),'-D',str(data),'-l',str(log_path),'-o',shlex.join(options),'-w','start'],stdout=subprocess.DEVNULL,check=True)
  report['pg_started']=True;persist()
  before={'counts':query(COUNT_SQL),'migrations':query("SELECT jsonb_agg(jsonb_build_object('id',version,'sha256',checksum_sha256,'dirty',dirty) ORDER BY version) FROM schema_migrations"),'session':query("SELECT jsonb_build_object('mode',current_setting('plan_cache_mode'),'preload',current_setting('session_preload_libraries'),'wal_buffers',current_setting('wal_buffers'),'server_version',current_setting('server_version'))"),'constraints':query(CONSTRAINT_SQL),'triggers':query(TRIGGER_SQL)}
  common.save(OUT/'database-preflight.json',before)
  baseline=json.loads((REPO/'output/xi-a0-checkpoint/B1.json').read_text())
  validate_preflight(before,baseline)
  check('before_workload')
  env=ENV.copy();env.update(XINSIGHT_POSTGRES_DSN=f"postgres://xinsight_admin@127.0.0.1:{port}/{fixture['database']}?sslmode=disable",XINSIGHT_A0_CHECKPOINT_MODE='qualify',XINSIGHT_A0_CHECKPOINT_OUTPUT=str(OUT/'qualification.json'),XINSIGHT_A0_CHECKPOINT_FIXTURE=str(FIXTURE),XINSIGHT_A0_EXPECT_WAL_MB='1024')
  with (OUT/'qualification.log').open('w') as log,(OUT/'observations.jsonl').open('w') as obs:
   proc=subprocess.Popen([code['binary'],'-test.run','^TestWorkbenchCheckpointDiagnostic$','-test.v','-test.timeout','12m'],cwd=REPO/'internal/db',env=env,stdout=log,stderr=subprocess.STDOUT)
   while proc.poll() is None:
    row=prior.resources(cfg);row['postgres']=query(common.STAT_SQL);obs.write(json.dumps(row)+'\n');obs.flush()
    if not row['within_limits']:raise RuntimeError('resource stop during workload')
    time.sleep(1)
   report['test_exit_code']=proc.returncode
  result=json.loads((OUT/'qualification.json').read_text());report.update(creation_300k=result.get('creation_300k'),capacity_500k=result.get('capacity_500k',{}).get('success'),counts_after=query(COUNT_SQL))
  if proc.returncode:raise RuntimeError('qualification test failed; retain all attempts')
  if result.get('creation_300k',{}).get('qualified')!=True or result.get('capacity_500k',{}).get('success')!=True:raise RuntimeError('required Store qualification/capacity missing')
  if report['counts_after']!=[21,6500000,0,0,500020,500100]:raise RuntimeError('wrong committed final counts')
  check('after_workload');report['complete']=True
 except BaseException as err:
  report['failure']=str(err);report['complete']=False;persist()
  if proc is not None and proc.poll() is None:
   try:common.psql(port,fixture['database'],"SELECT pg_cancel_backend(pid) FROM pg_stat_activity WHERE usename='xinsight_workspace' AND pid<>pg_backend_pid()")
   except Exception as cancel_error:report['cancel_error']=str(cancel_error)
   try:proc.wait(timeout=10)
   except subprocess.TimeoutExpired:proc.send_signal(signal.SIGINT);proc.wait(timeout=10)
  raise
 finally:
  if (data/'postmaster.pid').exists():subprocess.run([str(PG/'pg_ctl'),'-D',str(data),'-m','fast','-w','stop'],stdout=subprocess.DEVNULL,check=True)
  report['stopped']=not(data/'postmaster.pid').exists()
  if (data/'global/pg_control').exists():
   try:report['retained_control']=stopped(data)
   except Exception as control_error:report['control_error']=str(control_error);report['complete']=False
  if log_path.exists():shutil.copy2(log_path,OUT/'postgres.log')
  after=verify_template();common.save(OUT/'source-after.json',after);report['source_unchanged']=source_before==after
  if not report['source_unchanged']:report['complete']=False
  if (OUT/'qualification.json').exists():
   result=json.loads((OUT/'qualification.json').read_text());report['creation_300k']=result.get('creation_300k');report['capacity_500k']=result.get('capacity_500k',{}).get('success');report['attempted_300k']=len(result.get('samples',[]))
  report['finished_at']=common.utc();report['final_resources']=prior.resources(cfg)
  if not report['final_resources']['within_limits']:report['complete']=False
  persist();common.save(OUT/'output-hashes.json',{p.name:common.digest(p) for p in OUT.iterdir() if p.is_file()})
  print(json.dumps({'complete':report['complete'],'stopped':report['stopped'],'failure':report.get('failure'),'attempted_300k':report.get('attempted_300k')}))


if __name__=='__main__':
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--execute',action='store_true');args=parser.parse_args()
 if args.execute:execute()
 else:prepare()
