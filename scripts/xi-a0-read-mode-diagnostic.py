#!/usr/bin/env python3
"""Bounded read-mode candidate calibration on the retained synthetic instance.
Default: compile/offline validation and exact resource binding only.
--execute: one independently bounded sequence, never automatic retries or copies.
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
spec=importlib.util.spec_from_file_location('xi_plan_helpers',REPO/'scripts/xi-a0-plan-cache-diagnostic.py')
prior=importlib.util.module_from_spec(spec);spec.loader.exec_module(prior)
common,PG,ENV,GIB=prior.common,prior.PG,prior.ENV,prior.GIB
ROOT=Path('/private/tmp/xi-pc-hcpwpsza');DATA=ROOT/'data'
READY=REPO/'output/xi-a0-read-mode-ready';OUT=REPO/'output/xi-a0-read-mode-run'
PLAN=REPO/'docs/planning/xi-a0-read-mode-candidate-2026-09-09.md'
LIMIT=4480*1024**2
FINAL=False
APPROVED_FINAL=False
ORIGINAL_FINAL_READY=REPO/'output/xi-a0-read-mode-final-ready'
STARTUP_GIB=23
SETTINGS={'max_wal_size':'1GB','shared_buffers':'128MB','work_mem':'4MB','wal_buffers':'4MB',
 'checkpoint_timeout':'5min','checkpoint_completion_target':'0.9','track_io_timing':'on','track_wal_io_timing':'on','log_checkpoints':'on'}

def stopped(data):
 if data.is_symlink() or (data/'postmaster.pid').exists():raise RuntimeError('instance not stopped or path is symlink')
 control=common.capture([PG/'pg_controldata',data])
 if not re.search(r'Database cluster state:\s+shut down\s*$',control,re.M):raise RuntimeError('instance not cleanly stopped')
 return control

def config(binary):
 old=json.loads((REPO/('output/xi-a0-read-mode-run/resource-config.json' if FINAL else 'output/xi-a0-auto-create-run-02/resource-config.json')).read_text())
 paths=list(dict.fromkeys(old['paths']+([str(ORIGINAL_FINAL_READY)] if APPROVED_FINAL else [])+[str(Path(binary).parent),str(READY),str(OUT)]))
 return {'paths':paths,'cache_baseline_bytes':old['cache_baseline_bytes'],'cache_increment':max(0,common.size(Path(ENV['GOCACHE']))-old['cache_baseline_bytes']),
  'runner_pid':os.getpid(),'data':str(DATA),'started_unix':time.time(),'new_limit_bytes':LIMIT,'output':str(OUT if OUT.exists() else READY)}

def prepare():
 stopped(DATA);stopped(prior.SOURCE)
 if shutil.disk_usage('/private/tmp').free<=21.5*GIB:raise RuntimeError('below preparation protection threshold')
 READY.mkdir(exist_ok=False);build=Path(tempfile.mkdtemp(prefix='xi-read-mode-build-',dir='/private/tmp'));binary=build/'db.test'
 common.save(READY/'preparation-start.json',{'at':common.utc(),'binary':str(binary),'pg_started':False})
 for name,cmd in [('compile',['go','test','-c','-o',str(binary),'./internal/db']),('offline-tests',[str(binary),'-test.run','^TestWorkbench','-test.v']),('vet',['go','vet','./internal/db','./internal/workbench'])]:
  with (READY/(name+'.log')).open('w') as log:subprocess.run(cmd,cwd=REPO if name!='offline-tests' else REPO/'internal/db',env=ENV,stdout=log,stderr=subprocess.STDOUT,check=True)
 sources=common.capture(['git','ls-files','*.go','go.mod','go.sum','migrations/*'],cwd=REPO).splitlines()+['internal/db/workbench_read_mode_postgres_test.go']
 binding={'head':common.capture(['git','rev-parse','HEAD'],cwd=REPO),'tree':common.capture(['git','rev-parse','HEAD^{tree}'],cwd=REPO),'binary':str(binary),'binary_sha256':common.digest(binary),
  'sources':{p:common.digest(REPO/p) for p in sorted(set(sources))},'files':{str(p):common.digest(p) for p in [Path(__file__),REPO/'scripts/xi-a0-plan-cache-diagnostic.py',REPO/'scripts/xi-a0-checkpoint-diagnostic.py',PLAN,prior.ORIGINAL,REPO/'output/xi-a0-auto-create-run-02/evidence-binding.json']},'settings':SETTINGS,'prepared_at':common.utc()}
 common.save(READY/'code-binding.json',binding)
 row=prior.resources(config(binary));row.update(remaining_budget_bytes=LIMIT-row['new_bytes'],startup_gap_bytes=max(0,23*GIB-row['free_bytes']),pg_started=False,startup_ready=row['within_limits'] and LIMIT-row['new_bytes']>=512*1024**2 and row['free_bytes']>=23*GIB)
 common.save(READY/'resource-preflight.json',row);common.save(READY/'output-hashes.json',{p.name:common.digest(p) for p in READY.iterdir() if p.is_file()})
 print(json.dumps({k:row[k] for k in ['new_bytes','remaining_budget_bytes','free_bytes','startup_gap_bytes','startup_ready']}),flush=True)

def startup_guard(free_bytes, remaining_bytes=None):
 if free_bytes<STARTUP_GIB*GIB:raise RuntimeError(f'below approved {STARTUP_GIB}GiB startup threshold; no PG started')
 if remaining_bytes is not None and remaining_bytes<512*1024**2:raise RuntimeError('startup budget reserve failed; no PG started')

def prepare_approved_final():
 # This approval changes only this run's startup gate, never its Go binary.
 old=json.loads((ORIGINAL_FINAL_READY/'code-binding.json').read_text())
 if old['binary']!='/private/tmp/xi-read-mode-build-saaq_bb0/db.test' or old['binary_sha256']!='ab3c14624597b149bea302313c70060306aad2b456152ea3a40425b581d042b9':raise RuntimeError('unexpected final binary identity')
 if len(old['sources'])!=361 or old['settings']!=SETTINGS:raise RuntimeError('unexpected prepared inputs/settings')
 for p,h in [(REPO/p,h) for p,h in old['sources'].items()]+[(Path(old['binary']),old['binary_sha256'])]:
  if common.digest(p)!=h:raise RuntimeError('final build input changed: '+str(p))
 permitted={str(Path(__file__)),str(PLAN)}
 for p,h in old['files'].items():
  if p not in permitted and common.digest(Path(p))!=h:raise RuntimeError('prepared dependency changed: '+p)
 stopped(DATA);stopped(prior.SOURCE)
 if prior.inventory(DATA)!=json.loads((ORIGINAL_FINAL_READY/'retained-stopped-inventory.json').read_text()):raise RuntimeError('retained prestate changed')
 READY.mkdir(exist_ok=False)
 checks=[]
 for gate,free,remaining,expected in [(22,22*GIB,512*1024**2,True),(22,22*GIB-1,512*1024**2,False),(22,22*GIB,512*1024**2-1,False),(23,22*GIB,512*1024**2,False),(23,23*GIB,512*1024**2,True)]:
  global STARTUP_GIB
  STARTUP_GIB=gate
  try:startup_guard(free,remaining);accepted=True;error=None
  except RuntimeError as err:accepted=False;error=str(err)
  if accepted!=expected:raise RuntimeError('offline startup boundary failed')
  checks.append({'startup_gib':gate,'free_bytes':free,'remaining_bytes':remaining,'accepted':accepted,'expected':expected,'error':error})
 STARTUP_GIB=22
 common.save(READY/'offline-startup-boundaries.json',{'at':common.utc(),'checks':checks,'passed':True,'pg_started':False,'subprocess_launches':0})
 binding=dict(old)
 binding.update(head=common.capture(['git','rev-parse','HEAD'],cwd=REPO),tree=common.capture(['git','rev-parse','HEAD^{tree}'],cwd=REPO),prepared_at=common.utc(),binary_reused=True,startup_gib=22,profile='approved-final-22gib',original_preparation=str(ORIGINAL_FINAL_READY/'code-binding.json'))
 files=set(old['files'])|{str(p) for p in ORIGINAL_FINAL_READY.iterdir() if p.is_file()}|{str(READY/'offline-startup-boundaries.json')}
 binding['files']={p:common.digest(Path(p)) for p in sorted(files)}
 common.save(READY/'code-binding.json',binding)
 row=prior.resources(config(old['binary']));row.update(remaining_budget_bytes=LIMIT-row['new_bytes'],startup_gap_bytes=max(0,22*GIB-row['free_bytes']),pg_started=False,startup_ready=row['within_limits'] and LIMIT-row['new_bytes']>=512*1024**2 and row['free_bytes']>=22*GIB)
 common.save(READY/'resource-preflight.json',row)
 common.save(READY/'output-hashes.json',{p.name:common.digest(p) for p in READY.iterdir() if p.is_file()})
 print(json.dumps({k:row[k] for k in ['new_bytes','remaining_budget_bytes','free_bytes','startup_gap_bytes','startup_ready']}),flush=True)

def headers(port,database):
 return json.loads(common.psql(port,database,"SET statement_timeout='10s'; SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb) FROM workbench_readings r"))

def execute():
 if OUT.exists():raise RuntimeError('output already exists; preserve and do not rerun')
 code=json.loads((READY/'code-binding.json').read_text())
 for p,h in [(REPO/p,h) for p,h in code['sources'].items()]+[(Path(p),h) for p,h in code['files'].items()]+[(Path(code['binary']),code['binary_sha256'])]:
  if common.digest(p)!=h:raise RuntimeError('bound input changed: '+str(p))
 if code['settings']!=SETTINGS:raise RuntimeError('settings changed')
 if code.get('startup_gib',23)!=STARTUP_GIB:raise RuntimeError('bound startup threshold changed')
 stopped(DATA);stopped(prior.SOURCE)
 startup_guard(shutil.disk_usage('/private/tmp').free)
 common.process_rss([os.getpid()]);log_path=ROOT/('postgres-read-mode-final.log' if FINAL else 'postgres-read-mode.log')
 if log_path.exists():raise RuntimeError('execution log exists')
 OUT.mkdir();cfg=config(code['binary']);common.save(OUT/'resource-config.json',cfg);common.save(OUT/'code-binding.json',code)
 report={'started_at':common.utc(),'root':str(ROOT),'source':str(prior.SOURCE),'complete':False,'stopped':True,'qualification':False,'attempted_calibration_samples':0,'planned_store_calls':5 if FINAL else 11,'startup_gib':STARTUP_GIB};proc=None;port=None;before=None
 def persist():common.save(OUT/'run.json',report)
 def check(stage):
  row=prior.resources(cfg);report.setdefault('boundaries',[]).append({'stage':stage,**row});persist()
  if not row['within_limits']:raise RuntimeError('resource stop: '+stage)
  return row
 try:
  before,_,_=prior.source_preflight();common.save(OUT/'source-before.json',before);common.save(OUT/'retained-before.json',prior.inventory(DATA))
  if APPROVED_FINAL and json.loads((OUT/'retained-before.json').read_text())!=json.loads((ORIGINAL_FINAL_READY/'retained-stopped-inventory.json').read_text()):raise RuntimeError('retained prestate changed before startup')
  row=check('before_pg_start')
  startup_guard(row['free_bytes'],LIMIT-row['new_bytes'])
  with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
  options=['-h','127.0.0.1','-p',str(port),'-k',str(ROOT)]
  for k,v in SETTINGS.items():options+=['-c',k+'='+v]
  report.update(port=port,startup_options=options,stopped=False);persist()
  subprocess.run([str(PG/'pg_ctl'),'-D',str(DATA),'-l',str(log_path),'-o',shlex.join(options),'-w','start'],stdout=subprocess.DEVNULL,check=True)
  check('before_calibration');fixture=json.loads(prior.ORIGINAL.read_text())['fixture'];env=ENV.copy()
  env.update(XINSIGHT_POSTGRES_DSN=f"postgres://xinsight_admin@127.0.0.1:{port}/{fixture['database']}?sslmode=disable",XINSIGHT_A0_READ_MODE='1',XINSIGHT_A0_READ_MODE_OUTPUT=str(OUT/'diagnostic.json'),XINSIGHT_A0_PLAN_ORIGINAL=str(prior.ORIGINAL),XINSIGHT_A0_PLAN_RUNNER=str(Path(__file__)),XINSIGHT_A0_PLAN_RESOURCES=str(OUT/'resource-config.json'))
  if FINAL:env['XINSIGHT_A0_READ_MODE_FINAL']='1';env.pop('XINSIGHT_A0_READ_MODE',None)
  test_name='TestWorkbenchReadModeFinalCandidate' if FINAL else 'TestWorkbenchReadModeCalibration'
  if APPROVED_FINAL:
   before_headers=headers(port,fixture['database']);common.save(OUT/'headers-before.json',before_headers)
   if len(before_headers)!=30:raise RuntimeError('expected original 30 headers')
  with (OUT/'diagnostic.log').open('w') as log,(OUT/'observations.jsonl').open('w') as obs:
   proc=subprocess.Popen([code['binary'],'-test.run','^'+test_name+'$','-test.v','-test.timeout','20m'],cwd=REPO/'internal/db',env=env,stdout=log,stderr=subprocess.STDOUT)
   while proc.poll() is None:
    row=prior.resources(cfg);row['postgres']=json.loads(common.psql(port,fixture['database'],common.STAT_SQL));obs.write(json.dumps(row)+'\n');obs.flush()
    if not row['within_limits']:raise RuntimeError('resource stop during calibration')
    time.sleep(1)
   report['test_exit_code']=proc.returncode
  if (OUT/'diagnostic.json').exists():
   result=json.loads((OUT/'diagnostic.json').read_text());report.update(complete=result['complete'],attempted_calibration_samples=len(result['samples']))
  if proc.returncode:raise RuntimeError('calibration exited '+str(proc.returncode))
  if not report['complete']:raise RuntimeError('incomplete calibration')
  if APPROVED_FINAL:
   after_headers=headers(port,fixture['database']);common.save(OUT/'headers-after.json',after_headers)
   old_by_id={r['id']:r for r in before_headers};new_by_id={r['id']:r for r in after_headers}
   report['original_headers_unchanged']=all(new_by_id.get(k)==v for k,v in old_by_id.items())
   sample_ids={s['reading']['reading_id'] for s in result['samples']}
   report['exact_new_headers']=set(new_by_id)-set(old_by_id)==sample_ids and len(sample_ids)==5
   if not report['original_headers_unchanged'] or not report['exact_new_headers']:raise RuntimeError('header preservation/new identity mismatch')
   ordinals=[]
   for sample in result['samples']:
    check('before_ordinal_readback')
    rid=sample['reading']['reading_id']
    if not re.fullmatch(r'xwr_[A-Za-z0-9_-]+',rid):raise RuntimeError('invalid reading ID')
    sql="SET statement_timeout='10s'; SELECT jsonb_build_object('members',count(*),'min',min(ordinal),'max',max(ordinal),'mismatches',count(*) FILTER(WHERE ordinal<>expected)) FROM (SELECT m.ordinal,row_number() OVER(ORDER BY f.occurred_at,f.id) expected FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE m.reading_id='"+rid+"') q"
    row=json.loads(common.psql(port,fixture['database'],sql));row['reading_id']=rid;ordinals.append(row);common.save(OUT/'ordinal-readback.json',ordinals)
    if row['members']!=sample['expected_members'] or row['mismatches']!=0:raise RuntimeError('ordinal mismatch')
   check('after_readback')
 except BaseException as err:
  report['failure']=str(err);report['complete']=False;persist()
  if proc is not None and proc.poll() is None:
   if port is not None:
    try:common.psql(port,json.loads(prior.ORIGINAL.read_text())['fixture']['database'],"SELECT pg_cancel_backend(pid) FROM pg_stat_activity WHERE usename='xinsight_workspace' AND pid<>pg_backend_pid()")
    except Exception as err:report['cancel_error']=str(err)
   try:proc.wait(timeout=12)
   except subprocess.TimeoutExpired:proc.send_signal(signal.SIGINT);proc.wait(timeout=12)
  raise
 finally:
  if (DATA/'postmaster.pid').exists():subprocess.run([str(PG/'pg_ctl'),'-D',str(DATA),'-m','fast','-w','stop'],stdout=subprocess.DEVNULL,check=True)
  report['stopped']=not(DATA/'postmaster.pid').exists();report['retained_control']=stopped(DATA)
  if log_path.exists():shutil.copy2(log_path,OUT/'postgres.log')
  if before is not None:
   after,_,_=prior.source_preflight();common.save(OUT/'source-after.json',after);report['source_unchanged']=before==after
   if before!=after:report['complete']=False
  if (OUT/'diagnostic.json').exists():report['attempted_calibration_samples']=len(json.loads((OUT/'diagnostic.json').read_text())['samples'])
  report['finished_at']=common.utc();report['final_resources']=prior.resources(cfg);persist();common.save(OUT/'output-hashes.json',{p.name:common.digest(p) for p in OUT.iterdir() if p.is_file()});print(json.dumps({'complete':report['complete'],'attempted_calibration_samples':report['attempted_calibration_samples'],'failure':report.get('failure'),'stopped':report['stopped']}),flush=True)

if __name__=='__main__':
 parser=argparse.ArgumentParser();parser.add_argument('--execute',action='store_true');parser.add_argument('--resource-check');parser.add_argument('--prepare-final',action='store_true');parser.add_argument('--final',action='store_true');parser.add_argument('--prepare-final-approved',action='store_true');parser.add_argument('--final-approved',action='store_true');args=parser.parse_args()
 if args.prepare_final or args.final:FINAL=True;READY=REPO/'output/xi-a0-read-mode-final-ready';OUT=REPO/'output/xi-a0-read-mode-final-run'
 if args.prepare_final_approved or args.final_approved:FINAL=True;APPROVED_FINAL=True;STARTUP_GIB=22;READY=REPO/'output/xi-a0-read-mode-final-execution-ready';OUT=REPO/'output/xi-a0-read-mode-final-run'
 if args.resource_check:
  row=prior.resources(json.loads(Path(args.resource_check).read_text()));print(json.dumps(row));sys.exit(0 if row['within_limits'] else 2)
 elif args.prepare_final_approved:prepare_approved_final()
 elif args.execute or args.final or args.final_approved:execute()
 else:prepare()
