#!/usr/bin/env python3
"""Approved local 1GB/4GB checkpoint experiment; no production configuration writes.

Run without flags for preflight/compilation only; --execute starts disposable PG.
All subprocess argument lists are literal arrays. Only newly created tempfile
directories can be copied/removed. Failed instances are stopped and retained.
"""
import argparse
import hashlib
import json
import os
import pathlib
import shutil
import signal
import socket
import statistics
import subprocess
import tempfile
import time
from datetime import datetime, timezone

REPO = pathlib.Path(__file__).resolve().parents[1]
PG = pathlib.Path('/opt/homebrew/opt/postgresql@16/bin')
GIB = 1024 ** 3
OUT = REPO / 'output/xi-a0-checkpoint'
ENV = os.environ.copy()
ENV.update(GOTOOLCHAIN='go1.25.11', GOCACHE='/private/tmp/xinsight-go-cache', GOMAXPROCS='10')
for key in list(ENV):
    if key.startswith('XINSIGHT_A0_') or key == 'XINSIGHT_POSTGRES_DSN':
        ENV.pop(key)


def utc():
    return datetime.now(timezone.utc).isoformat()


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')
    path.chmod(0o600)


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda: f.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def files(root):
    return {str(p.relative_to(root)): {'bytes': p.stat().st_size, 'sha256': digest(p)}
            for p in sorted(root.rglob('*')) if p.is_file()}


def size(root):
    return sum(p.stat().st_size for p in root.rglob('*') if p.is_file())


def capture(args, **kwargs):
    return subprocess.check_output([str(a) for a in args], text=True, **kwargs).strip()


def process_rss(roots):
    rows = []
    for line in capture(['/bin/ps', '-axo', 'pid=,ppid=,rss=']).splitlines():
        rows.append(tuple(map(int, line.split())))
    chosen = set(roots)
    while True:
        nxt = chosen | {pid for pid, parent, rss in rows if parent in chosen}
        if nxt == chosen:
            break
        chosen = nxt
    return sum(rss * 1024 for pid, _, rss in rows if pid in chosen), sorted(chosen)


def psql(port, database, query):
    # No credentials in command parameters: this task's loopback-only trust cluster.
    return capture([PG/'psql', '-X', '-qAt', '-h', '127.0.0.1', '-p', port,
                    '-U', 'xinsight_admin', '-d', database, '-v', 'ON_ERROR_STOP=1', '-c', query], timeout=10)


STAT_SQL = """SELECT jsonb_build_object('at',clock_timestamp(),
'bgwriter',(SELECT to_jsonb(s) FROM pg_stat_bgwriter s),
'wal',(SELECT to_jsonb(s) FROM pg_stat_wal s),
'io',(SELECT jsonb_agg(to_jsonb(s)) FROM pg_stat_io s),
'activity',(SELECT jsonb_agg(jsonb_build_object('pid',pid,'user',usename,'state',state,'query_start',query_start,'xact_start',xact_start,'wait_type',wait_event_type,'wait',wait_event)) FROM pg_stat_activity WHERE datname=current_database()))"""


def main():
    global OUT
    parser = argparse.ArgumentParser()
    parser.add_argument('--execute', action='store_true')
    parser.add_argument('--qualify', action='store_true', help='20x300k followed by 500k at 1GB; reuse the verified stopped template read-only')
    args = parser.parse_args()
    if args.qualify:
        # Keep the reviewed preparation snapshot immutable.
        OUT = REPO / 'output/xi-a0-capacity-run-02'
    limits = {'new_disk_bytes':(7 if args.qualify else 12)*GIB,
              'start_minimum_bytes':(28 if args.qualify else 32)*GIB,
              'free_disk_floor_bytes':(21 if args.qualify else 20)*GIB,
              'rss_bytes':4*GIB,'seconds':1200}
    OUT.mkdir(exist_ok=True)
    # Never overwrite an execution's immutable outputs on an accidental rerun.
    if (OUT/'run.json').exists():
        raise RuntimeError('run.json already exists; preserve this run and review before another')
    cache_root = pathlib.Path(ENV['GOCACHE'])
    cache_before = size(cache_root)
    build_dir = pathlib.Path(tempfile.mkdtemp(prefix='xi-checkpoint-build-'))
    binary = build_dir/'db.test'
    with (OUT/'compile.log').open('w') as log:
        subprocess.run(['go', 'test', '-c', '-o', str(binary), './internal/db'], cwd=REPO,
                       env=ENV, stdout=log, stderr=subprocess.STDOUT, check=True)
    cache_increment = max(0, size(cache_root) - cache_before)
    tracked = capture(['git', 'ls-files', '*.go', 'go.mod', 'go.sum', 'migrations/*'], cwd=REPO).splitlines()
    tracked += ['internal/db/workbench_checkpoint_postgres_test.go', 'internal/db/workbench_checkpoint_roles_postgres_test.go']
    binding = {p: digest(REPO/p) for p in sorted(set(tracked))}
    save(OUT/'code-binding.json', {'head':capture(['git','rev-parse','HEAD'],cwd=REPO),
         'tree':capture(['git','rev-parse','HEAD^{tree}'],cwd=REPO), 'compiled_at':utc(),
         'binary':str(binary),'binary_sha256':digest(binary),'sources':binding,
         'runner_sha256':digest(pathlib.Path(__file__)), 'plan_sha256':digest(REPO/'docs/planning/xi-a0-checkpoint-performance-plan-2026-09-09.md'),
         'build_cache_before_bytes':cache_before,'build_cache_increment_bytes':cache_increment,
         'resource_limits':limits,
         'resource_plan_sha256':digest(REPO/'docs/planning/xi-a0-capacity-resource-review-2026-09-09.md') if args.qualify else None,
         'workload':'20x300k_then_500k_1GB' if args.qualify else 'paired_6x300k'})
    with (OUT/'focused-tests.log').open('w') as log:
        subprocess.run([str(binary),'-test.run','^TestWorkbenchCheckpoint','-test.v'], cwd=REPO/'internal/db', env=ENV,
                       stdout=log,stderr=subprocess.STDOUT,check=True)
    if not args.execute:
        print(json.dumps({'compiled':str(binary),'sha256':digest(binary),'executed':False}),flush=True)
        return
    if shutil.disk_usage('/private/tmp').free < limits['start_minimum_bytes']:
        raise RuntimeError('less than approved starting free space before execution')
    process_rss([os.getpid()])  # Fail before PG startup if RSS cannot be observed.
    # macOS Unix socket paths are limited to 103 bytes; the default TMPDIR plus
    # "qualification/.s.PGSQL.<port>" exceeded that before any workload began.
    root = pathlib.Path(tempfile.mkdtemp(prefix='xi-cap-',dir='/private/tmp')) if args.qualify else pathlib.Path(tempfile.mkdtemp(prefix='xi-checkpoint-run-'))
    carried_paths=[]
    elapsed_before=0.0
    if args.qualify:
        previous_attempt=REPO/'output/xi-a0-capacity-run'
        failed=json.loads((previous_attempt/'run.json').read_text())
        failed_code=json.loads((previous_attempt/'code-binding.json').read_text())
        carried_paths=[pathlib.Path(failed['temporary_root']),pathlib.Path(failed_code['binary']).parent,previous_attempt]
        if any((pathlib.Path(i['path'])/'data/postmaster.pid').exists() for i in failed['instances']):
            raise RuntimeError('previous startup failure unexpectedly has a running postmaster')
        if (previous_attempt/'qualification.json').exists() or (previous_attempt/'qualification.log').exists():
            raise RuntimeError('previous attempt contains workload evidence; no automatic rerun allowed')
        elapsed_before=(datetime.now(timezone.utc)-datetime.fromisoformat(failed['started_at'])).total_seconds()
    report = {'started_at':utc(),'temporary_root':str(root),'instances':[], 'arms':[],
              'limits':limits,
              'initial_free_bytes':shutil.disk_usage(root).free,'diagnostic_only':True}
    report['carried_resource_paths']=[str(p) for p in carried_paths]
    report['elapsed_from_startup_failure_seconds']=elapsed_before
    deadline = time.monotonic()+limits['seconds']-elapsed_before
    active = None

    def persist(): save(OUT/'run.json',report)

    def check_resources():
        roots = [os.getpid()]
        if active and (active/'data/postmaster.pid').exists():
            roots.append(int((active/'data/postmaster.pid').read_text().splitlines()[0]))
        rss,pids=process_rss(roots)
        values={'at':utc(),'rss_bytes':rss,'processes':pids,'new_disk_bytes':size(root)+size(build_dir)+size(OUT)+cache_increment+sum(size(p) for p in carried_paths),
                'free_bytes':shutil.disk_usage(root).free}
        if rss>=limits['rss_bytes'] or values['new_disk_bytes']>=limits['new_disk_bytes'] or values['free_bytes']<=limits['free_disk_floor_bytes'] or time.monotonic()>deadline:
            report['resource_stop']=values;persist();raise RuntimeError('approved resource/time boundary reached')
        return values

    def run_logged(cmd, env, log_path, port=None, database='postgres'):
        with log_path.open('w') as log, (log_path.with_suffix('.observations.jsonl')).open('w') as observations:
            p=subprocess.Popen([str(a) for a in cmd],cwd=REPO/'internal/db',env=env,stdout=log,stderr=subprocess.STDOUT)
            try:
                while p.poll() is None:
                    sample=check_resources()
                    if port is not None:
                        sample['postgres']=json.loads(psql(port,database,STAT_SQL))
                    observations.write(json.dumps(sample)+'\n');observations.flush()
                    time.sleep(1)
                if p.returncode:
                    raise RuntimeError(f'{log_path.name} exited {p.returncode}')
            except BaseException:
                if p.poll() is None:
                    p.send_signal(signal.SIGINT)
                    try: p.wait(timeout=10)
                    except subprocess.TimeoutExpired: p.kill();p.wait()
                raise

    def start(path, wal):
        nonlocal active
        if len(os.fsencode(str(path/'.s.PGSQL.65535')))>103:
            raise RuntimeError('Unix socket path exceeds verified local PostgreSQL limit')
        with socket.socket() as s:
            s.bind(('127.0.0.1',0));port=s.getsockname()[1]
        options=f'-h 127.0.0.1 -p {port} -k {path} -c max_wal_size={wal} -c shared_buffers=128MB -c work_mem=4MB -c checkpoint_timeout=5min -c checkpoint_completion_target=0.9 -c track_io_timing=on -c track_wal_io_timing=on -c log_checkpoints=on'
        item={'path':str(path),'port':port,'max_wal_size':wal,'started_at':utc(),'stopped':False}
        report['instances'].append(item);persist();active=path
        subprocess.run([str(PG/'pg_ctl'),'-D',str(path/'data'),'-l',str(path/'postgres.log'),'-o',options,'-w','start'],
                       stdout=subprocess.DEVNULL,check=True)
        return port,item

    def stop(path,item):
        nonlocal active
        if (path/'data/postmaster.pid').exists():
            subprocess.run([str(PG/'pg_ctl'),'-D',str(path/'data'),'-m','fast','-w','stop'],stdout=subprocess.DEVNULL,check=True)
        item.update(stopped=not(path/'data/postmaster.pid').exists(),stopped_at=utc())
        if (path/'postgres.log').exists():
            destination=OUT/(path.name+'-postgres.log');shutil.copy2(path/'postgres.log',destination)
            item['postgres_log_sha256']=digest(destination)
        active=None;persist()

    def test_env(port,database,mode,output,wal=1024):
        env=ENV.copy();env.update(XINSIGHT_POSTGRES_DSN=f'postgres://xinsight_admin@127.0.0.1:{port}/{database}?sslmode=disable',
             XINSIGHT_A0_CHECKPOINT_MODE=mode,XINSIGHT_A0_CHECKPOINT_OUTPUT=str(output),
             XINSIGHT_A0_CHECKPOINT_FIXTURE=str(OUT/'fixture.json'),XINSIGHT_A0_EXPECT_WAL_MB=str(wal))
        return env

    def arm(name,wal):
        path=root/name;path.mkdir();shutil.copytree(template/'data',path/'data')
        copied=files(path/'data')
        if copied!=template_files: raise RuntimeError('physical template differs')
        save(OUT/(name+'-template-match.json'),{'matched_files':len(copied),'template_manifest_sha256':digest(OUT/'template-files.json'),'match':True})
        port,item=start(path,wal)
        result_path=OUT/(name+'.json')
        try:
            print(f'{utc()} {name}: measuring six 300k transactions, max_wal_size={wal}',flush=True)
            env=test_env(port,fixture['database'],'measure',result_path,1024 if wal=='1GB' else 4096)
            run_logged([binary,'-test.run','^TestWorkbenchCheckpointDiagnostic$','-test.v','-test.timeout','6m'],env,OUT/(name+'.log'),port,fixture['database'])
            result=json.loads(result_path.read_text()); samples=[s['duration_ns']/1e6 for s in result['samples']]
            row={'name':name,'max_wal_size':wal,'samples_ms':samples,'median_ms':statistics.median(samples),
                 'max_ms':max(samples),'tail_ms':result['tail_checkpoint']['duration_ns']/1e6,
                 'cycle_amortized_ms':result['cycle_duration_ns']/1e6/6,'all_success':all(s['success'] for s in result['samples'])}
            report['arms'].append(row);persist();print(json.dumps(row),flush=True)
            if name in ['A1','B1']:
                env=test_env(port,fixture['database'],'explain',OUT/(name+'-explain.json'),1024 if wal=='1GB' else 4096)
                run_logged([binary,'-test.run','^TestWorkbenchCheckpointDiagnostic$','-test.v','-test.timeout','6m'],env,OUT/(name+'-explain.log'),port,fixture['database'])
            return row,path,item
        finally:
            stop(path,item)

    def remove_success(path,item):
        assert path.parent==root and path!=template and item['stopped'] and not(path/'data/postmaster.pid').exists()
        # Exact disposable directory created by this invocation; outputs already preserved.
        shutil.rmtree(path);item['cleaned_at']=utc();item['removed_exact_path']=str(path);persist()

    try:
        persist()
        if args.qualify:
            # Prior template is an immutable read source: never start or delete it.
            previous=REPO/'output/xi-a0-checkpoint'
            previous_run=json.loads((previous/'run.json').read_text())
            template=pathlib.Path(previous_run['temporary_root'])/'template'
            if (template/'data/postmaster.pid').exists():
                raise RuntimeError('source template has a postmaster.pid; not safe to copy')
            control=capture([PG/'pg_controldata',template/'data'])
            if not any(line.split(':',1)[0].strip()=='Database cluster state' and line.split(':',1)[1].strip()=='shut down' for line in control.splitlines()):
                raise RuntimeError('source template is not cleanly shut down')
            template_files=json.loads((previous/'template-files.json').read_text())
            if files(template/'data')!=template_files:
                raise RuntimeError('source template changed since accepted A/B')
            report['reused_template']={'path':str(template),'source_manifest_sha256':digest(previous/'template-files.json'),'files':len(template_files),'read_only_source':True}
            shutil.copy2(previous/'fixture.json',OUT/'fixture.json')
            fixture=json.loads((OUT/'fixture.json').read_text())
            before_copy=check_resources()
            if before_copy['free_bytes']<limits['start_minimum_bytes']:
                raise RuntimeError('free space fell below startup prerequisite before copy')
            report['resource_boundaries']=[{'stage':'before_copy',**before_copy}];persist()
            path=root/'qualification';path.mkdir();shutil.copytree(template/'data',path/'data')
            report['resource_boundaries'].append({'stage':'after_copy',**check_resources()});persist()
            if files(path/'data')!=template_files:raise RuntimeError('qualification clone differs from template')
            save(OUT/'template-match.json',{'source':str(template),'files':len(template_files),'match':True,'source_manifest_sha256':digest(previous/'template-files.json')})
            report['resource_boundaries'].append({'stage':'before_pg_start',**check_resources()});persist()
            port,item=start(path,'1GB')
            try:
                print(f'{utc()} qualifying 20 consecutive 300k transactions, then 500k at retained 6M; max_wal_size=1GB',flush=True)
                env=test_env(port,fixture['database'],'qualify',OUT/'qualification.json',1024)
                run_logged([binary,'-test.run','^TestWorkbenchCheckpointDiagnostic$','-test.v','-test.timeout','12m'],env,OUT/'qualification.log',port,fixture['database'])
                result=json.loads((OUT/'qualification.json').read_text())
                report['creation_300k']=result['creation_300k']
                report['capacity_500k_success']=result['capacity_500k']['success']
                report['measurement_complete']=True
                report['diagnostic_complete']=True
                report['retained_instance']=str(path)
            finally:
                if (OUT/'qualification.json').exists():
                    result=json.loads((OUT/'qualification.json').read_text())
                    report['creation_300k']=result.get('creation_300k')
                    report['capacity_500k_success']=result.get('capacity_500k',{}).get('success')
                    report['measurement_complete']=result.get('measurement_complete',False)
                stop(path,item)
            return
        template=root/'template';template.mkdir()
        print(f'{utc()} preparing one shared physical template',flush=True)
        run_logged([PG/'initdb','-D',template/'data','-U','xinsight_admin','-A','trust','--no-locale','--encoding=UTF8'],ENV,OUT/'initdb.log')
        port,item=start(template,'1GB')
        try:
            env=test_env(port,'postgres','seed',OUT/'fixture.json');env['XINSIGHT_A0_PERFORMANCE']='1'
            run_logged([binary,'-test.run','^TestWorkbenchA0LocalPerformanceQualification$','-test.v','-test.timeout','6m'],env,OUT/'seed.log',port)
            fixture=json.loads((OUT/'fixture.json').read_text())
            env=test_env(port,fixture['database'],'roles',OUT/'roles.json')
            run_logged([binary,'-test.run','^TestWorkbenchCheckpointPrepareRoles$','-test.v','-test.timeout','2m'],env,OUT/'roles.log',port,fixture['database'])
            facts=psql(port,fixture['database'],"SELECT jsonb_build_object('facts',(SELECT count(*) FROM workbench_economic_facts),'members',(SELECT count(*) FROM workbench_reading_members),'headers',(SELECT count(*) FROM workbench_readings),'money',(SELECT sum(gateway_charged_micros)::text FROM workbench_economic_facts),'min_time',(SELECT min(occurred_at) FROM workbench_economic_facts),'max_time',(SELECT max(occurred_at) FROM workbench_economic_facts))")
            save(OUT/'template-facts.json',json.loads(facts))
        finally: stop(template,item)
        template_files=files(template/'data');save(OUT/'template-files.json',template_files)
        a,ap,ai=arm('A1','1GB');remove_success(ap,ai)
        b,bp,bi=arm('B1','4GB')
        promote=(a['all_success'] and b['all_success'] and b['median_ms']<=a['median_ms']*.8 and b['max_ms']<=a['max_ms'] and b['cycle_amortized_ms']<=a['cycle_amortized_ms']*.8)
        report['reverse_pair_criteria_met']=promote;persist()
        if promote:
            remove_success(bp,bi)
            b2,bp,bi=arm('B2','4GB')
            a2,ap,ai=arm('A2','1GB');remove_success(ap,ai)
            confirmed=(b2['all_success'] and b2['median_ms']<=a2['median_ms']*.8 and b2['max_ms']<=a2['max_ms'] and b2['cycle_amortized_ms']<=a2['cycle_amortized_ms']*.8)
            report['reverse_confirmed']=confirmed
            report['capacity_probe_eligible']=confirmed and max(b['median_ms'],b2['median_ms'])<=5000
            if report['capacity_probe_eligible']:
                # B2 retains the six successful readings. Restart is explicit:
                # this is a capacity probe, not another warm A/B timing sample.
                port,capacity_item=start(bp,'4GB')
                try:
                    env=test_env(port,fixture['database'],'capacity',OUT/'B2-capacity.json',4096)
                    run_logged([binary,'-test.run','^TestWorkbenchCheckpointDiagnostic$','-test.v','-test.timeout','6m'],env,OUT/'B2-capacity.log',port,fixture['database'])
                    report['capacity_probe_passed']=True
                finally: stop(bp,capacity_item)
                remove_success(bp,capacity_item)
            else:
                report['retained_nonqualifying_arm']=str(bp)
        else:
            report['retained_nonqualifying_arm']=str(bp)
        report['diagnostic_complete']=True
    except BaseException as e:
        report['failure']=str(e);report['diagnostic_complete']=False
        raise
    finally:
        if active:
            stop(active,report['instances'][-1])
        report['finished_at']=utc();report['final_free_bytes']=shutil.disk_usage(root).free;persist()
        save(OUT/'output-hashes.json',{p.name:digest(p) for p in OUT.iterdir() if p.is_file() and p.name!='output-hashes.json'})
        print(json.dumps({'complete':report.get('diagnostic_complete'), 'report':str(OUT/'run.json'),'failure':report.get('failure')}),flush=True)


if __name__=='__main__': main()
