#!/usr/bin/env python3
"""Bounded local read-only plan diagnosis. Native file clones only, no fallback.

Default: independent clone probe, source preflight, compile and offline tests.
--execute: use the bound prepared binary and one verified stopped-source clone.
--resume: separately authorized fresh sequence on the retained diagnostic clone.
--prepare-cohort / --cohort: approved 18-target MCV comparison on that same clone.
"""
import argparse
import ctypes
import errno
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone

sys.dont_write_bytecode = True
REPO = Path(__file__).resolve().parents[1]
module = importlib.util.spec_from_file_location('xi_checkpoint_helpers', REPO/'scripts/xi-a0-checkpoint-diagnostic.py')
common = importlib.util.module_from_spec(module)
module.loader.exec_module(common)
PG, ENV, GIB = common.PG, common.ENV, common.GIB
SOURCE = Path('/private/tmp/xi-cap-7qgkhj7u/qualification/data')
FIRST_READY = REPO/'output/xi-a0-plan-cache-ready'
READY = REPO/'output/xi-a0-plan-cache-ready-02'
OUT = REPO/'output/xi-a0-plan-cache-run'
PLAN = REPO/'docs/planning/xi-a0-plan-cache-diagnostic-2026-09-09.md'
INPUT = REPO/'output/xi-a0-plan-cache-plan/queries.json'
ORIGINAL = REPO/'output/xi-a0-capacity-run-02/qualification.json'
APPROVED_FILES = REPO/'output/xi-a0-plan-cache-plan/failed-source-files.json'
COHORT_READY = REPO/'output/xi-a0-plan-cache-mcv-ready'
COHORT_OUT = REPO/'output/xi-a0-plan-cache-mcv-run'
COHORT_INPUT = REPO/'output/xi-a0-plan-cache-mcv-plan/queries.json'
COVERAGE_BASELINE = REPO/'output/xi-a0-plan-cache-run-02/diagnostic.json'
LIB = ctypes.CDLL('/usr/lib/libSystem.B.dylib', use_errno=True)
LIB.clonefile.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_int]
LIB.clonefile.restype = ctypes.c_int
LIB.listxattr.argtypes = [ctypes.c_char_p, ctypes.c_void_p, ctypes.c_size_t, ctypes.c_int]
LIB.listxattr.restype = ctypes.c_ssize_t
LIB.getxattr.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_void_p, ctypes.c_size_t, ctypes.c_uint32, ctypes.c_int]
LIB.getxattr.restype = ctypes.c_ssize_t


def xattrs(path):
    encoded = os.fsencode(path)
    size = LIB.listxattr(encoded, None, 0, 0)
    if size < 0: raise OSError(ctypes.get_errno(), 'listxattr failed', str(path))
    if size == 0: return {}
    buffer = ctypes.create_string_buffer(size)
    count = LIB.listxattr(encoded, buffer, size, 0)
    if count < 0: raise OSError(ctypes.get_errno(), 'listxattr changed/failed', str(path))
    result = {}
    for name in filter(None, buffer.raw[:count].split(b'\0')):
        size = LIB.getxattr(encoded, name, None, 0, 0, 0)
        if size < 0: raise OSError(ctypes.get_errno(), 'getxattr failed', str(path))
        value = ctypes.create_string_buffer(max(1, size))
        count = LIB.getxattr(encoded, name, value, size, 0, 0)
        if count < 0: raise OSError(ctypes.get_errno(), 'getxattr changed/failed', str(path))
        result[os.fsdecode(name)] = __import__('hashlib').sha256(value.raw[:count]).hexdigest()
    return result


def clone_regular(source, destination):
    source, destination = Path(source), Path(destination)
    if not stat.S_ISREG(source.lstat().st_mode):
        raise RuntimeError(f'not a regular source file: {source}')
    if LIB.clonefile(os.fsencode(source), os.fsencode(destination), 0) != 0:
        error = ctypes.get_errno()
        raise OSError(error, os.strerror(error), str(destination))
    if (source.stat().st_dev, source.stat().st_ino) == (destination.stat().st_dev, destination.stat().st_ino):
        raise RuntimeError('clone unexpectedly has source inode')
    return str(destination)


def inventory(root):
    root = Path(root)
    result = {}
    for p in [root] + sorted(root.rglob('*')):
        s = p.lstat()
        if not (stat.S_ISREG(s.st_mode) or stat.S_ISDIR(s.st_mode)):
            raise RuntimeError(f'unsupported link/special path: {p}')
        if s.st_mode & (stat.S_ISUID | stat.S_ISGID):
            raise RuntimeError(f'unsupported special permission bits: {p}')
        row = {'type': 'file' if stat.S_ISREG(s.st_mode) else 'directory',
               'mode': stat.S_IMODE(s.st_mode), 'uid': s.st_uid, 'gid': s.st_gid,
               'flags': s.st_flags, 'xattrs': xattrs(p)}
        if row['type'] == 'file':
            row.update(bytes=s.st_size, sha256=common.digest(p))
        result[str(p.relative_to(root))] = row
    return result


def source_preflight():
    if SOURCE.is_symlink() or (SOURCE/'postmaster.pid').exists():
        raise RuntimeError('source is a link or has postmaster.pid')
    control = common.capture([PG/'pg_controldata', SOURCE])
    if not re.search(r'Database cluster state:\s+shut down\s*$', control, re.M):
        raise RuntimeError('source is not cleanly stopped')
    if any((SOURCE/'pg_tblspc').iterdir()):
        raise RuntimeError('external tablespaces not covered by this plan')
    permissions = common.capture(['/bin/ls', '-led', SOURCE]) + '\n' + common.capture(['/bin/ls', '-leAR', SOURCE])
    if re.search(r'^\s*\d+:', permissions, re.M):
        raise RuntimeError('source ACL requires separate reviewed copy handling')
    data = inventory(SOURCE)
    compact = {p: {'bytes': v['bytes'], 'sha256': v['sha256']} for p, v in data.items() if v['type'] == 'file'}
    if compact != json.loads(APPROVED_FILES.read_text()):
        raise RuntimeError('source differs from approved 1604-file manifest')
    return data, permissions, control


def allocation(root):
    logical = allocated = 0
    for p in Path(root).rglob('*'):
        try:
            s = p.lstat()
        except FileNotFoundError:  # A completed query may remove its own temp file.
            continue
        if stat.S_ISREG(s.st_mode):
            logical += s.st_size
            allocated += s.st_blocks * 512
    return {'logical': logical, 'allocated': allocated, 'charged': max(logical, allocated)}


def resources(config):
    roots = [config['runner_pid']]
    pid_path = Path(config['data'])/'postmaster.pid'
    if pid_path.exists():
        roots.append(int(pid_path.read_text().splitlines()[0]))
    rss, pids = common.process_rss(roots)
    parts = {p: allocation(p) for p in config['paths']}
    row = {'at': common.utc(), 'parts': parts, 'new_bytes': sum(p['charged'] for p in parts.values()) + config['cache_increment'],
           'rss_bytes': rss, 'processes': pids, 'free_bytes': shutil.disk_usage('/private/tmp').free,
           'elapsed_seconds': time.time()-config['started_unix']}
    row['within_limits'] = row['new_bytes'] < config.get('new_limit_bytes',4*GIB) and row['free_bytes'] > 21*GIB and rss < 4*GIB and row['elapsed_seconds'] < 1200
    if not row['within_limits']:
        common.save(Path(config.get('output',str(OUT)))/'resource-stop.json', row)
    return row


def probe():
    # Only these newly created synthetic files are removed by TemporaryDirectory.
    with tempfile.TemporaryDirectory(prefix='xi-pc-clone-probe-', dir='/private/tmp') as d:
        root = Path(d); src = root/'source'; dst = root/'clone'
        src.write_bytes(bytes(range(256))*(8*1024*1024//256)); src.chmod(0o600)
        initial = common.digest(src); clone_regular(src, dst)
        result = {'bytes': src.stat().st_size, 'initial_sha256': initial,
                  'different_inodes': src.stat().st_ino != dst.stat().st_ino,
                  'initial_equal': common.digest(dst) == initial,
                  'permissions_equal': stat.S_IMODE(src.stat().st_mode) == stat.S_IMODE(dst.stat().st_mode)}
        with dst.open('r+b') as f: f.write(b'CLONE PRIVATE')
        dst_hash = common.digest(dst)
        result['clone_write_preserves_source'] = common.digest(src) == initial
        with src.open('r+b') as f: f.write(b'SOURCE PRIVATE')
        result['source_write_preserves_clone'] = common.digest(dst) == dst_hash
        try:
            clone_regular(src, dst)
            raise AssertionError('existing destination unexpectedly replaced')
        except OSError as e:
            result['existing_destination_errno'] = e.errno
            if e.errno != errno.EEXIST: raise
        if not all(result[k] for k in ['different_inodes','initial_equal','permissions_equal','clone_write_preserves_source','source_write_preserves_clone']):
            raise RuntimeError('native clone isolation probe failed')
        result['existing_destination_unchanged'] = common.digest(dst) == dst_hash
        return result


def prepare():
    READY.mkdir(exist_ok=False)
    common.save(READY/'native-clone-probe.json', probe())
    source, permissions, control = source_preflight()
    common.save(READY/'source-inventory.json', source)
    (READY/'source-permissions.txt').write_text(permissions+'\n')
    (READY/'source-control.txt').write_text(control+'\n')
    prior_cache = json.loads((REPO/'output/xi-a0-capacity-run-02/code-binding.json').read_text())
    before = prior_cache['build_cache_before_bytes'] + prior_cache['build_cache_increment_bytes']
    # Conservatively charge all cache growth since that retained binding, including
    # this turn's initial compile check before prepare() was invoked.
    build = Path(tempfile.mkdtemp(prefix='xi-pc-build-', dir='/private/tmp'))
    binary = build/'db.test'
    with (READY/'compile.log').open('w') as log:
        subprocess.run(['go','test','-c','-o',str(binary),'./internal/db'],cwd=REPO,env=ENV,stdout=log,stderr=subprocess.STDOUT,check=True)
    increment = max(0,common.size(Path(ENV['GOCACHE']))-before)
    with (READY/'offline-tests.log').open('w') as log:
        subprocess.run([str(binary),'-test.run','^TestWorkbenchPlanCache','-test.v'],cwd=REPO/'internal/db',env=ENV,stdout=log,stderr=subprocess.STDOUT,check=True)
    with (READY/'vet.log').open('w') as log:
        subprocess.run(['go','vet','./internal/db'],cwd=REPO,env=ENV,stdout=log,stderr=subprocess.STDOUT,check=True)
    increment = max(0,common.size(Path(ENV['GOCACHE']))-before)
    tracked = common.capture(['git','ls-files','*.go','go.mod','go.sum','migrations/*'],cwd=REPO).splitlines()+['internal/db/workbench_plan_cache_postgres_test.go']
    common.save(READY/'code-binding.json', {'head':common.capture(['git','rev-parse','HEAD'],cwd=REPO),'tree':common.capture(['git','rev-parse','HEAD^{tree}'],cwd=REPO),
        'sources':{p:common.digest(REPO/p) for p in sorted(set(tracked))},'binary':str(binary),'binary_sha256':common.digest(binary),
        'runner_sha256':common.digest(Path(__file__)),'helper_sha256':common.digest(REPO/'scripts/xi-a0-checkpoint-diagnostic.py'),
        'plan_sha256':common.digest(PLAN),'queries_sha256':common.digest(INPUT),'cache_increment':increment,
        'cache_baseline_bytes':before,'cache_baseline_source':'output/xi-a0-capacity-run-02/code-binding.json','prepared_at':common.utc()})
    common.save(READY/'output-hashes.json',{p.name:common.digest(p) for p in READY.iterdir() if p.is_file()})
    print(json.dumps({'prepared':True,'binary':str(binary),'cache_increment':increment,'free_bytes':shutil.disk_usage('/private/tmp').free}),flush=True)


def prepare_cohort(recover=False, coverage=False):
    global COHORT_READY, COHORT_INPUT
    if recover: COHORT_READY=REPO/'output/xi-a0-plan-cache-mcv-fix'
    if coverage:
        COHORT_READY=REPO/'output/xi-a0-coverage-ready';COHORT_INPUT=REPO/'output/xi-a0-coverage-plan/queries.json'
    new_limit=(4256 if coverage else 4192)*1024**2
    if COHORT_READY.exists(): raise RuntimeError('cohort preparation exists; no overwrite')
    if shutil.disk_usage('/private/tmp').free < 23*GIB: raise RuntimeError('below cohort 23GiB preparation entry')
    root = Path('/private/tmp/xi-pc-hcpwpsza'); data = root/'data'
    if (data/'postmaster.pid').exists(): raise RuntimeError('retained instance running')
    control = common.capture([PG/'pg_controldata',data])
    if not re.search(r'Database cluster state:\s+shut down\s*$',control,re.M): raise RuntimeError('retained instance not cleanly stopped')
    old = json.loads((READY/'code-binding.json').read_text())
    prior = json.loads((REPO/'output/xi-a0-plan-cache-run-02/resource-config.json').read_text())
    COHORT_READY.mkdir()
    build = Path(tempfile.mkdtemp(prefix='xi-pc-mcv-build-',dir='/private/tmp')); binary = build/'db.test'
    with (COHORT_READY/'compile.log').open('w') as log:
        subprocess.run(['go','test','-c','-o',str(binary),'./internal/db'],cwd=REPO,env=ENV,stdout=log,stderr=subprocess.STDOUT,check=True)
    offline_env=ENV.copy()
    if coverage:
        offline_env.update(XINSIGHT_A0_PLAN_VALIDATE_INPUT=str(COHORT_INPUT),XINSIGHT_A0_PLAN_REFERENCE=str(REPO/'output/xi-a0-plan-cache-plan/queries.json'),XINSIGHT_A0_COVERAGE_BASELINE=str(COVERAGE_BASELINE))
    with (COHORT_READY/'offline-tests.log').open('w') as log:
        subprocess.run([str(binary),'-test.run','^TestWorkbenchPlanCache','-test.v'],cwd=REPO/'internal/db',env=offline_env,stdout=log,stderr=subprocess.STDOUT,check=True)
    with (COHORT_READY/'vet.log').open('w') as log:
        subprocess.run(['go','vet','./internal/db'],cwd=REPO,env=ENV,stdout=log,stderr=subprocess.STDOUT,check=True)
    tracked = common.capture(['git','ls-files','*.go','go.mod','go.sum','migrations/*'],cwd=REPO).splitlines()
    common.save(COHORT_READY/'code-binding.json',{'head':common.capture(['git','rev-parse','HEAD'],cwd=REPO),
        'sources':{p:common.digest(REPO/p) for p in sorted(set(tracked))},'binary':str(binary),'binary_sha256':common.digest(binary),
        'runner_sha256':common.digest(Path(__file__)),'helper_sha256':common.digest(REPO/'scripts/xi-a0-checkpoint-diagnostic.py'),
        'plan_sha256':common.digest(PLAN),'queries_sha256':common.digest(COHORT_INPUT),
        'semantic_baseline_sha256':common.digest(COVERAGE_BASELINE) if coverage else None,
        'reference_queries_sha256':common.digest(REPO/'output/xi-a0-plan-cache-plan/queries.json'),
        'cache_baseline_bytes':old['cache_baseline_bytes'],'prepared_at':common.utc()})
    if recover: prior=json.loads((REPO/'output/xi-a0-plan-cache-mcv-run/resource-config.json').read_text())
    if coverage: prior={'paths':list(json.loads((REPO/'output/xi-a0-plan-cache-mcv-argument-fix/resource-snapshot.json').read_text())['parts'])}
    paths = list(dict.fromkeys(prior['paths']+[str(build),str(COHORT_READY),str(COHORT_INPUT.parent)]))
    parts = {p:allocation(p) for p in paths}
    cache = max(0,common.size(Path(ENV['GOCACHE']))-old['cache_baseline_bytes'])
    new = sum(p['charged'] for p in parts.values())+cache; free=shutil.disk_usage('/private/tmp').free
    common.save(COHORT_READY/'resource-preflight.json',{'at':common.utc(),'parts':parts,'cache_increment':cache,'new_bytes':new,
        'free_bytes':free,'new_limit_bytes':new_limit,'remaining_budget_bytes':new_limit-new,
        'ready':new+512*1024**2<=new_limit and free>=23*GIB,'pg_started':False})
    common.save(COHORT_READY/'output-hashes.json',{p.name:common.digest(p) for p in COHORT_READY.iterdir() if p.is_file()})
    print(json.dumps({'prepared':True,'binary':str(binary),'new_bytes':new,'free_bytes':free,'remaining_budget_bytes':new_limit-new}),flush=True)


def execute(resume=False, cohort=False, recover=False, coverage=False):
    global OUT, INPUT, COHORT_READY, COHORT_OUT, COHORT_INPUT
    recovery_config=None
    if coverage:
        cohort=True;COHORT_READY=REPO/'output/xi-a0-coverage-ready';COHORT_OUT=REPO/'output/xi-a0-coverage-run';COHORT_INPUT=REPO/'output/xi-a0-coverage-plan/queries.json'
    if recover:
        cohort=True
        failed_out=REPO/'output/xi-a0-plan-cache-mcv-run'
        failed=json.loads((failed_out/'diagnostic.json').read_text())
        failed_run=json.loads((failed_out/'run.json').read_text())
        if failed['samples'] or failed_run['stopped'] is not True: raise RuntimeError('recovery requires zero target executions and stopped instance')
        recovery_config=json.loads((failed_out/'resource-config.json').read_text())
        if time.time()-recovery_config['started_unix']>=1200: raise RuntimeError('original cohort 20-minute window exhausted')
        COHORT_READY=REPO/'output/xi-a0-plan-cache-mcv-fix'; COHORT_OUT=REPO/'output/xi-a0-plan-cache-mcv-run-02'
    if cohort: resume=True; INPUT=COHORT_INPUT
    previous_output = REPO/'output/xi-a0-plan-cache-run-02' if cohort else OUT
    if coverage: previous_output=REPO/'output/xi-a0-plan-cache-mcv-run-02'
    if resume: OUT = COHORT_OUT if cohort else REPO/'output/xi-a0-plan-cache-run-02'
    if OUT.exists(): raise RuntimeError('execution output exists; no overwrite or automatic rerun')
    binding_path = COHORT_READY/'code-binding.json' if cohort else REPO/'output/xi-a0-plan-cache-fix/code-binding.json' if resume else READY/'code-binding.json'
    code = json.loads(binding_path.read_text())
    for p,h in code['sources'].items():
        if common.digest(REPO/p)!=h: raise RuntimeError('compiled source changed: '+p)
    bindings = [(Path(code['binary']),code['binary_sha256'])]
    if not resume or cohort: bindings += [(Path(__file__),code['runner_sha256']),(PLAN,code['plan_sha256']),(INPUT,code['queries_sha256'])]
    if coverage:bindings += [(COVERAGE_BASELINE,code['semantic_baseline_sha256']),(REPO/'output/xi-a0-plan-cache-plan/queries.json',code['reference_queries_sha256'])]
    for p,h in bindings:
        if common.digest(p)!=h: raise RuntimeError('prepared binding changed: '+str(p))
    old_code = json.loads((READY/'code-binding.json').read_text())
    if common.digest(REPO/'scripts/xi-a0-checkpoint-diagnostic.py') != old_code['helper_sha256']: raise RuntimeError('helper changed')
    if resume and not cohort and common.digest(INPUT) != old_code['queries_sha256']: raise RuntimeError('reviewed query inputs changed')
    startup_gib = 23 if cohort else 24
    if shutil.disk_usage('/private/tmp').free < startup_gib*GIB: raise RuntimeError('below approved startup free-space threshold')
    common.process_rss([os.getpid()])
    if resume:
        previous = json.loads((previous_output/'run.json').read_text())
        root = Path('/private/tmp/xi-pc-hcpwpsza')
        if previous['root'] != str(root) or previous['stopped'] is not True: raise RuntimeError('unexpected retained diagnostic instance')
        data = root/'data'
        if (data/'postmaster.pid').exists(): raise RuntimeError('retained diagnostic instance has postmaster.pid')
        control = common.capture([PG/'pg_controldata',data])
        if not re.search(r'Database cluster state:\s+shut down\s*$',control,re.M): raise RuntimeError('retained instance not cleanly stopped')
        cache_increment = max(0,common.size(Path(ENV['GOCACHE']))-old_code['cache_baseline_bytes'])
        new_limit = (4256 if coverage else 4192 if cohort else 4128)*1024**2
    else:
        root=Path(tempfile.mkdtemp(prefix='xi-pc-',dir='/private/tmp'));data=root/'data'
        cache_increment = code['cache_increment'];new_limit = 4*GIB
    OUT.mkdir()
    report={'started_at':common.utc(),'root':str(root),'source':str(SOURCE),'method':'per-regular-file clonefile(flags=0), no fallback','complete':False,'stopped':True}
    paths=[str(root),str(Path(code['binary']).parent),str(FIRST_READY),str(READY),str(OUT)]
    if resume:
        paths += [str(Path(old_code['binary']).parent),str(previous_output),str(REPO/'output/xi-a0-plan-cache-fix')]
        if cohort:
            prior_paths=json.loads((previous_output/'resource-config.json').read_text())['paths']
            if recover: prior_paths=recovery_config['paths']
            if coverage:prior_paths=list(json.loads((REPO/'output/xi-a0-plan-cache-mcv-argument-fix/resource-snapshot.json').read_text())['parts'])
            paths=list(dict.fromkeys(prior_paths+paths+[str(COHORT_READY),str(COHORT_INPUT.parent)]))
        report.update(method='reuse retained stopped CoW instance; no new copy',previous_attempt=str(previous_output/'run.json'),previous_targets_excluded=55 if coverage else 37 if cohort else 1,fresh_target_limit=9 if coverage else 18 if cohort else 36)
    config={'paths':paths,'cache_increment':cache_increment,'runner_pid':os.getpid(),'data':str(data),'started_unix':time.time(),'new_limit_bytes':new_limit,'output':str(OUT)}
    if recover:
        config['started_unix']=recovery_config['started_unix']
        report.update(recovery_of=str(failed_out/'run.json'),failed_attempt_targets=0,original_window_started_unix=config['started_unix'])
    common.save(OUT/'code-binding.json',{'base_commit':common.capture(['git','rev-parse','HEAD'],cwd=REPO),'binary':code['binary'],'binary_sha256':code['binary_sha256'],
        'sources':code['sources'],'binary_preparation_binding':str(binding_path),'binary_preparation_binding_sha256':common.digest(binding_path),
        'runner_sha256':common.digest(Path(__file__)),'helper_sha256':common.digest(REPO/'scripts/xi-a0-checkpoint-diagnostic.py'),
        'plan_sha256':common.digest(PLAN),'queries_sha256':common.digest(INPUT),'new_limit_bytes':new_limit,'bound_at':common.utc()})
    if coverage:
        binding=json.loads((OUT/'code-binding.json').read_text());binding.update(semantic_baseline=str(COVERAGE_BASELINE),semantic_baseline_sha256=common.digest(COVERAGE_BASELINE),reference_queries_sha256=common.digest(REPO/'output/xi-a0-plan-cache-plan/queries.json'));common.save(OUT/'code-binding.json',binding)
    common.save(OUT/'resource-config.json',config)
    postgres_log = root/('postgres-coverage.log' if coverage else 'postgres-cohort-recover.log' if recover else 'postgres-cohort.log' if cohort else 'postgres-resume.log' if resume else 'postgres.log')
    if postgres_log.exists(): raise RuntimeError('new attempt log already exists')
    proc = None; port = None; before_source = None
    def persist():common.save(OUT/'run.json',report)
    def check(stage):
        row=resources(config);report.setdefault('boundaries',[]).append({'stage':stage,**row});persist()
        if not row['within_limits']:raise RuntimeError('resource stop at '+stage)
        return row
    try:
        before_source,permissions,control=source_preflight();common.save(OUT/'source-before.json',before_source)
        if resume:
            if any((data/'pg_tblspc').iterdir()):raise RuntimeError('unexpected retained tablespace')
            permissions=common.capture(['/bin/ls','-led',data])+'\n'+common.capture(['/bin/ls','-leAR',data])
            if re.search(r'^\s*\d+:',permissions,re.M):raise RuntimeError('unexpected retained ACL')
            common.save(OUT/'retained-before.json',inventory(data))
            row=check('retained_instance_preflight')
        else:
            check('before_clone');shutil.copytree(SOURCE,data,copy_function=clone_regular)
            clone=inventory(data)
            if clone!=before_source:raise RuntimeError('clone bytes/types/permissions differ')
            regular=[p for p,v in clone.items() if v['type']=='file']
            if not all((data/p).stat().st_ino != (SOURCE/p).stat().st_ino for p in regular):raise RuntimeError('source inode reused')
            common.save(OUT/'clone-verification.json',{'source_inventory_sha256':common.digest(OUT/'source-before.json'),'files':len(regular),'all_bytes_types_permissions_match':True,'all_regular_inodes_different':True,'fallback':False})
            row=check('after_clone')
        if new_limit-row['new_bytes']<512*1024**2 or row['free_bytes']<21.5*GIB:raise RuntimeError('query reserve not met')
        if len(os.fsencode(str(root/'.s.PGSQL.65535')))>103:raise RuntimeError('socket path too long')
        with socket.socket() as s:s.bind(('127.0.0.1',0));port=s.getsockname()[1]
        report['port']=port;report['stopped']=False;persist()
        options=f'-h 127.0.0.1 -p {port} -k {root} -c max_wal_size=1GB -c shared_buffers=128MB -c work_mem=4MB -c checkpoint_timeout=5min -c checkpoint_completion_target=0.9 -c track_io_timing=on -c track_wal_io_timing=on -c log_checkpoints=on'
        row=check('before_pg_start')
        if resume and row['free_bytes']<startup_gib*GIB:raise RuntimeError('free space fell below startup threshold before PG start')
        subprocess.run([str(PG/'pg_ctl'),'-D',str(data),'-l',str(postgres_log),'-o',options,'-w','start'],stdout=subprocess.DEVNULL,check=True)
        row=check('before_test')
        if new_limit-row['new_bytes']<512*1024**2 or row['free_bytes']<21.5*GIB:raise RuntimeError('query reserve fell below approved gate')
        fixture=json.loads(ORIGINAL.read_text())['fixture'];env=ENV.copy()
        env.update(XINSIGHT_POSTGRES_DSN=f"postgres://xinsight_admin@127.0.0.1:{port}/{fixture['database']}?sslmode=disable",XINSIGHT_A0_PLAN_CACHE='1',
            XINSIGHT_A0_PLAN_OUTPUT=str(OUT/'diagnostic.json'),XINSIGHT_A0_PLAN_QUERIES=str(INPUT),XINSIGHT_A0_PLAN_ORIGINAL=str(ORIGINAL),
            XINSIGHT_A0_PLAN_RUNNER=str(Path(__file__)),XINSIGHT_A0_PLAN_RESOURCES=str(OUT/'resource-config.json'))
        if cohort:
            env['XINSIGHT_A0_PLAN_COHORT']='1'
            env['XINSIGHT_A0_PLAN_REFERENCE']=str(REPO/'output/xi-a0-plan-cache-plan/queries.json')
        if coverage:env.update(XINSIGHT_A0_COVERAGE_ONLY='1',XINSIGHT_A0_COVERAGE_BASELINE=str(COVERAGE_BASELINE))
        with (OUT/'diagnostic.log').open('w') as log,(OUT/'observations.jsonl').open('w') as observations:
            proc=subprocess.Popen([code['binary'],'-test.run','^TestWorkbenchPlanCacheDiagnostic$','-test.v','-test.timeout','20m'],cwd=REPO/'internal/db',env=env,stdout=log,stderr=subprocess.STDOUT)
            while proc.poll() is None:
                row=resources(config)
                row['postgres']=json.loads(common.psql(port,fixture['database'],common.STAT_SQL))
                observations.write(json.dumps(row)+'\n');observations.flush()
                if not row['within_limits']:raise RuntimeError('resource stop during diagnostic')
                time.sleep(1)
            report['test_exit_code']=proc.returncode
        if proc.returncode:raise RuntimeError('diagnostic test exited '+str(proc.returncode))
        result=json.loads((OUT/'diagnostic.json').read_text());report['complete']=result['complete'];report['target_executions']=result['target_executions']
        if not result['complete']:raise RuntimeError('diagnostic did not finish all planned targets')
    except BaseException as e:
        report['failure']=str(e);persist()
        if proc is not None and proc.poll() is None:
            if port is not None:
                try:common.psql(port,json.loads(ORIGINAL.read_text())['fixture']['database'],"SELECT pg_cancel_backend(pid) FROM pg_stat_activity WHERE usename='xinsight_workspace' AND pid<>pg_backend_pid()")
                except Exception as cancel_error:report['cancel_error']=str(cancel_error)
            try:proc.wait(timeout=12)
            except subprocess.TimeoutExpired:proc.send_signal(signal.SIGINT);proc.wait(timeout=12)
        raise
    finally:
        if (data/'postmaster.pid').exists():
            subprocess.run([str(PG/'pg_ctl'),'-D',str(data),'-m','fast','-w','stop'],stdout=subprocess.DEVNULL,check=True)
        report['stopped']=not(data/'postmaster.pid').exists()
        if postgres_log.exists():shutil.copy2(postgres_log,OUT/'postgres.log')
        if before_source is not None:
            after,_,_=source_preflight();common.save(OUT/'source-after.json',after);report['source_unchanged']=after==before_source
            if not report['source_unchanged']:report['complete']=False
        report['finished_at']=common.utc();report['final_resources']=resources(config);persist()
        common.save(OUT/'output-hashes.json',{p.name:common.digest(p) for p in OUT.iterdir() if p.is_file() and p.name!='output-hashes.json'})
        print(json.dumps({'complete':report['complete'],'failure':report.get('failure'),'stopped':report['stopped'],'root':str(root)}),flush=True)


if __name__=='__main__':
    parser=argparse.ArgumentParser();action=parser.add_mutually_exclusive_group();action.add_argument('--execute',action='store_true');action.add_argument('--resume',action='store_true');action.add_argument('--prepare-cohort',action='store_true');action.add_argument('--cohort',action='store_true');action.add_argument('--prepare-cohort-fix',action='store_true');action.add_argument('--cohort-recover',action='store_true');action.add_argument('--prepare-coverage',action='store_true');action.add_argument('--coverage',action='store_true');parser.add_argument('--resource-check')
    args=parser.parse_args()
    if args.resource_check:
        row=resources(json.loads(Path(args.resource_check).read_text()));print(json.dumps(row));sys.exit(0 if row['within_limits'] else 2)
    elif args.prepare_coverage:prepare_cohort(coverage=True)
    elif args.coverage:execute(coverage=True)
    elif args.prepare_cohort_fix:prepare_cohort(recover=True)
    elif args.cohort_recover:execute(recover=True)
    elif args.prepare_cohort:prepare_cohort()
    elif args.cohort:execute(cohort=True)
    elif args.resume:execute(resume=True)
    elif args.execute:execute()
    else:prepare()
