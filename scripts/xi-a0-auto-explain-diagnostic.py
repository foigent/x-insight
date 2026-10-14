#!/usr/bin/env python3
"""One authorized synthetic Store create on the retained instance. No copy or retry.
Default prepares only; --execute verifies the immutable preparation and runs once.
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

sys.dont_write_bytecode = True
REPO = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('xi_plan_helpers', REPO/'scripts/xi-a0-plan-cache-diagnostic.py')
prior = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prior)
common, PG, ENV, GIB = prior.common, prior.PG, prior.ENV, prior.GIB
ROOT = Path('/private/tmp/xi-pc-hcpwpsza')
DATA = ROOT/'data'
READY = REPO/'output/xi-a0-auto-create-ready'
OUT = REPO/'output/xi-a0-auto-create-run'
PLAN = REPO/'docs/planning/xi-a0-auto-explain-create-2026-09-09.md'
ORIGINAL = prior.ORIGINAL
LIBRARY = Path('/opt/homebrew/Cellar/postgresql@16/16.14/lib/postgresql/auto_explain.dylib')
LIB_HASH = 'e880e306b60a240cfdee2ff6a653f92ff59c63a0e67fd9c7e0fcc70687b78cf6'
LIMIT = 4480*1024**2
RECOVERY = False
FAILED = REPO/'output/xi-a0-auto-create-run'
SETTINGS = {'max_wal_size':'1GB','shared_buffers':'128MB','work_mem':'4MB',
    'checkpoint_timeout':'5min','checkpoint_completion_target':'0.9',
    'track_io_timing':'on','track_wal_io_timing':'on','log_checkpoints':'on',
    'session_preload_libraries':'auto_explain','auto_explain.log_min_duration':'0',
    'auto_explain.log_parameter_max_length':'-1','auto_explain.sample_rate':'1',
    'auto_explain.log_analyze':'on','auto_explain.log_buffers':'on','auto_explain.log_wal':'on',
    'auto_explain.log_settings':'on','auto_explain.log_triggers':'on',
    'auto_explain.log_format':'json','auto_explain.log_timing':'off',
    'auto_explain.log_nested_statements':'off','log_line_prefix':'XI_AUTO[%p] '}

def stopped(data):
    if data.is_symlink() or (data/'postmaster.pid').exists(): raise RuntimeError('instance not stopped or path is symlink')
    control=common.capture([PG/'pg_controldata',data])
    if not re.search(r'Database cluster state:\s+shut down\s*$',control,re.M): raise RuntimeError('instance not cleanly stopped')
    return control

def library_check():
    if LIBRARY.stat().st_size!=53376 or common.digest(LIBRARY)!=LIB_HASH: raise RuntimeError('auto_explain library changed')
    if Path('/opt/homebrew/opt/postgresql@16/lib/postgresql/auto_explain.dylib').resolve()!=LIBRARY: raise RuntimeError('active PG library path changed')

def resource_config(binary,started=None):
    previous=FAILED/'resource-config.json' if RECOVERY else REPO/'output/xi-a0-coverage-run/resource-config.json'
    paths=json.loads(previous.read_text())['paths']
    paths=list(dict.fromkeys(paths+[str(Path(binary).parent),str(READY),str(OUT)]))
    old=json.loads((REPO/'output/xi-a0-plan-cache-ready-02/code-binding.json').read_text())
    return {'paths':paths,'cache_increment':max(0,common.size(Path(ENV['GOCACHE']))-old['cache_baseline_bytes']),
        'cache_baseline_bytes':old['cache_baseline_bytes'],'runner_pid':os.getpid(),'data':str(DATA),
        'started_unix':json.loads((FAILED/'resource-config.json').read_text())['started_unix'] if RECOVERY else started or time.time(),'new_limit_bytes':LIMIT,'output':str(OUT if OUT.exists() else READY)}

def bound_files():
    files=common.capture(['git','ls-files','*.go','go.mod','go.sum','migrations/*'],cwd=REPO).splitlines()
    files+=['internal/db/workbench_auto_explain_postgres_test.go']
    return sorted(set(files))

def prepare():
    library_check();stopped(DATA)
    if shutil.disk_usage('/private/tmp').free<23*GIB: raise RuntimeError('below 23GiB preparation threshold')
    READY.mkdir(exist_ok=False)
    build=Path(tempfile.mkdtemp(prefix='xi-auto-build-',dir='/private/tmp'));binary=build/'db.test'
    common.save(READY/'preparation-start.json',{'at':common.utc(),'binary':str(binary),'pg_started':False})
    for name,cmd in [
        ('compile',['go','test','-c','-o',str(binary),'./internal/db']),
        ('offline-tests',[str(binary),'-test.run','^TestWorkbench(AutoExplain|PlanCache|Checkpoint)','-test.v']),
        ('vet',['go','vet','./internal/db'])]:
        with (READY/(name+'.log')).open('w') as log:
            subprocess.run(cmd,cwd=REPO if name!='offline-tests' else REPO/'internal/db',env=ENV,stdout=log,stderr=subprocess.STDOUT,check=True)
    binding={'head':common.capture(['git','rev-parse','HEAD'],cwd=REPO),'tree':common.capture(['git','rev-parse','HEAD^{tree}'],cwd=REPO),
        'binary':str(binary),'binary_sha256':common.digest(binary),'sources':{p:common.digest(REPO/p) for p in bound_files()},
        'files':{str(p):common.digest(p) for p in [Path(__file__),REPO/'scripts/xi-a0-plan-cache-diagnostic.py',REPO/'scripts/xi-a0-checkpoint-diagnostic.py',PLAN,ORIGINAL,LIBRARY,REPO/'output/xi-a0-coverage-run/evidence-binding.json']},
        'settings':SETTINGS,'prepared_at':common.utc()}
    common.save(READY/'code-binding.json',binding)
    row=prior.resources(resource_config(binary))
    row.update(remaining_budget_bytes=LIMIT-row['new_bytes'],pg_started=False,
        ready=row['within_limits'] and LIMIT-row['new_bytes']>=512*1024**2 and row['free_bytes']>=23*GIB,
        incremental_component_estimate_mib={'member_indexes':96,'wal':256,'temp':160,'logs':8,'unmodeled':128})
    common.save(READY/'resource-preflight.json',row)
    common.save(READY/'output-hashes.json',{p.name:common.digest(p) for p in READY.iterdir() if p.is_file()})
    print(json.dumps({k:row[k] for k in ['new_bytes','free_bytes','remaining_budget_bytes','ready']}),flush=True)

def execute():
    if OUT.exists(): raise RuntimeError('execution output exists; never overwrite or rerun')
    code=json.loads((READY/'code-binding.json').read_text())
    for p,h in [(REPO/p,h) for p,h in code['sources'].items()]+[(Path(p),h) for p,h in code['files'].items()]+[(Path(code['binary']),code['binary_sha256'])]:
        if common.digest(p)!=h: raise RuntimeError('bound input changed: '+str(p))
    if code['settings']!=SETTINGS: raise RuntimeError('startup settings changed')
    library_check();stopped(DATA);stopped(prior.SOURCE)
    if shutil.disk_usage('/private/tmp').free<23*GIB: raise RuntimeError('below 23GiB startup threshold')
    common.process_rss([os.getpid()])
    log_path=ROOT/('postgres-auto-create-recovery.log' if RECOVERY else 'postgres-auto-create.log')
    if RECOVERY:
        failed=json.loads((FAILED/'run.json').read_text());diagnostic=json.loads((FAILED/'diagnostic.json').read_text())
        if failed['attempted']!=0 or diagnostic['attempted']!=0 or not failed['stopped']:raise RuntimeError('recovery requires zero creation attempts and stopped instance')
        if time.time()-json.loads((FAILED/'resource-config.json').read_text())['started_unix']>=1200:raise RuntimeError('original 20-minute window exhausted')
    if log_path.exists(): raise RuntimeError('new log already exists')
    OUT.mkdir()
    config=resource_config(code['binary']);common.save(OUT/'resource-config.json',config)
    common.save(OUT/'code-binding.json',code)
    report={'started_at':common.utc(),'root':str(ROOT),'source':str(prior.SOURCE),'complete':False,'stopped':True,'attempted':0,
        'preparation_sha256':common.digest(READY/'code-binding.json'),'fresh_session':True,'qualification':False}
    if RECOVERY:report.update(recovery_of=str(FAILED/'run.json'),original_window_started_unix=config['started_unix'])
    proc=None;port=None;before=None
    def persist():common.save(OUT/'run.json',report)
    def check(stage):
        row=prior.resources(config);report.setdefault('boundaries',[]).append({'stage':stage,**row});persist()
        if not row['within_limits']:raise RuntimeError('resource stop: '+stage)
        return row
    try:
        before,_,_=prior.source_preflight();common.save(OUT/'source-before.json',before)
        common.save(OUT/'retained-before.json',prior.inventory(DATA))
        row=check('before_pg_start')
        if row['free_bytes']<23*GIB or LIMIT-row['new_bytes']<512*1024**2: raise RuntimeError('startup reserve failed')
        with socket.socket() as s:s.bind(('127.0.0.1',0));port=s.getsockname()[1]
        args=['-h','127.0.0.1','-p',str(port),'-k',str(ROOT)]
        for k,v in SETTINGS.items():args+=['-c',k+'='+v]
        report.update(port=port,startup_options=args,stopped=False);persist()
        subprocess.run([str(PG/'pg_ctl'),'-D',str(DATA),'-l',str(log_path),'-o',shlex.join(args),'-w','start'],stdout=subprocess.DEVNULL,check=True)
        check('before_light_preflight')
        fixture=json.loads(ORIGINAL.read_text())['fixture'];env=ENV.copy()
        env.update(XINSIGHT_POSTGRES_DSN=f"postgres://xinsight_admin@127.0.0.1:{port}/{fixture['database']}?sslmode=disable",XINSIGHT_A0_AUTO_CREATE='1',
            XINSIGHT_A0_AUTO_OUTPUT=str(OUT/'diagnostic.json'),XINSIGHT_A0_AUTO_LOG=str(log_path),
            XINSIGHT_A0_PLAN_ORIGINAL=str(ORIGINAL),XINSIGHT_A0_PLAN_RUNNER=str(Path(__file__)),XINSIGHT_A0_PLAN_RESOURCES=str(OUT/'resource-config.json'))
        with (OUT/'diagnostic.log').open('w') as log,(OUT/'observations.jsonl').open('w') as observations:
            proc=subprocess.Popen([code['binary'],'-test.run','^TestWorkbenchAutoExplainCreateDiagnostic$','-test.v','-test.timeout','20m'],cwd=REPO/'internal/db',env=env,stdout=log,stderr=subprocess.STDOUT)
            while proc.poll() is None:
                row=prior.resources(config)
                # The existing official stats query observes waits/checkpoints without product writes.
                row['postgres']=json.loads(common.psql(port,fixture['database'],common.STAT_SQL))
                observations.write(json.dumps(row)+'\n');observations.flush()
                if not row['within_limits']:raise RuntimeError('resource stop during diagnostic')
                time.sleep(1)
            report['test_exit_code']=proc.returncode
        if (OUT/'diagnostic.json').exists():
            result=json.loads((OUT/'diagnostic.json').read_text());report.update(attempted=result['attempted'],complete=result['complete'])
        if proc.returncode:raise RuntimeError('diagnostic test exited '+str(proc.returncode))
        if report['attempted']!=1 or not report['complete']:raise RuntimeError('sole diagnostic incomplete')
    except BaseException as e:
        report['failure']=str(e);persist()
        if proc is not None and proc.poll() is None:
            if port is not None:
                try:common.psql(port,json.loads(ORIGINAL.read_text())['fixture']['database'],"SELECT pg_cancel_backend(pid) FROM pg_stat_activity WHERE usename='xinsight_workspace' AND pid<>pg_backend_pid()")
                except Exception as e:report['cancel_error']=str(e)
            try:proc.wait(timeout=12)
            except subprocess.TimeoutExpired:proc.send_signal(signal.SIGINT);proc.wait(timeout=12)
        raise
    finally:
        if (DATA/'postmaster.pid').exists():subprocess.run([str(PG/'pg_ctl'),'-D',str(DATA),'-m','fast','-w','stop'],stdout=subprocess.DEVNULL,check=True)
        report['stopped']=not(DATA/'postmaster.pid').exists();report['retained_control']=stopped(DATA)
        if log_path.exists():shutil.copy2(log_path,OUT/'postgres.log')
        if (OUT/'diagnostic.json').exists():report['attempted']=json.loads((OUT/'diagnostic.json').read_text())['attempted']
        if before is not None:
            after,_,_=prior.source_preflight();common.save(OUT/'source-after.json',after);report['source_unchanged']=before==after
            if before!=after:report['complete']=False
        report['finished_at']=common.utc();report['final_resources']=prior.resources(config);persist()
        common.save(OUT/'output-hashes.json',{p.name:common.digest(p) for p in OUT.iterdir() if p.is_file()})
        print(json.dumps({'complete':report['complete'],'attempted':report['attempted'],'failure':report.get('failure'),'stopped':report['stopped']}),flush=True)

if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('--execute',action='store_true');parser.add_argument('--prepare-recovery',action='store_true');parser.add_argument('--recover',action='store_true');parser.add_argument('--resource-check')
    args=parser.parse_args()
    if args.prepare_recovery or args.recover:
        RECOVERY=True;READY=REPO/'output/xi-a0-auto-create-ready-02';OUT=REPO/'output/xi-a0-auto-create-run-02'
    if args.resource_check:
        row=prior.resources(json.loads(Path(args.resource_check).read_text()));print(json.dumps(row));sys.exit(0 if row['within_limits'] else 2)
    elif args.execute or args.recover:execute()
    else:prepare()
