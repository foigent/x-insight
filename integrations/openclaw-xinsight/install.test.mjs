import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, realpathSync, rmSync, statSync, existsSync, symlinkSync, chmodSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { installManagedOpenClaw, nativeRunner, parseInvitation, discoveredProjects } from './install.mjs';

const pluginDir=dirname(fileURLToPath(import.meta.url));
const invitation={schema_version:'xinsight.openclaw-invitation.v1',invitation_id:`xiei_${'a'.repeat(32)}`,
  invitation_token:`xiet_${'a'.repeat(43)}`,origin:'https://xi.example.test',ur_base_url:'https://ur.example.test/v1',
  expires_at:'2099-01-01T00:00:00Z'};
const collection='11111111-1111-4111-8111-111111111111';

function harness(t,{agents=[{id:'main'}],approvedAgentId='main'}={}) {
  const root=realpathSync(mkdtempSync(join(tmpdir(),'xi-managed-install-')));
  t.after(()=>rmSync(root,{recursive:true,force:true}));
  const invitationFile=join(root,'invitation.json');
  writeFileSync(invitationFile,JSON.stringify(invitation),{mode:0o600});
  const stateDir=join(root,'custody');
  const config={models:{providers:{'unirouter-existing':{api:'openai-completions',baseUrl:invitation.ur_base_url,
    apiKey:'REDACTED_BY_NATIVE_CLI',models:[{id:'existing',reasoning:true,input:['text','image']}]}}},
    plugins:{allow:['unrelated'],entries:{unrelated:{enabled:true,config:{keep:42}}}}};
  const mutations=[],requests=[],xiPluginDirs=new Set([pluginDir]);
  let installed=false,installedDir=pluginDir,registryPersistedDir=pluginDir,registryCurrentDir=null,approved=false,lost=false,reloadFailure=false,installFailureOnce=null,casAdded=null,runtimeDir=null;
  const selectedDir=()=>config.plugins.load?.paths?.find(path=>xiPluginDirs.has(path))??installedDir;
  const native=async(args)=>{
    if(args[0]==='--version') return 'OpenClaw 2026.9.5 (synthetic)';
    if(args[0]==='agents') return JSON.stringify(agents.map(agent=>({...agent,workspace:agent.workspace??join(root,`workspace-${agent.id}`)})));
    if(args[0]==='config' && args[1]==='get') {
      if(args[2]==='models') return JSON.stringify(config.models);
      if(args[2]==='plugins') return JSON.stringify(config.plugins);
      if(args[2]==='plugins.load.paths') return JSON.stringify(config.plugins.load?.paths);
      return JSON.stringify(config.plugins.entries['xi-work-events']);
    }
    if(args[0]==='plugins' && args[1]==='list') return JSON.stringify({plugins:installed?[{id:'xi-work-events',rootDir:selectedDir()}]:[]});
    if(args[0]==='plugins' && args[1]==='registry') {
      const index=(dir,rootDir)=>({installRecords:installed?{'xi-work-events':{
        source:'path',sourcePath:dir,installPath:dir}}:{},
        plugins:installed?[{pluginId:'xi-work-events',rootDir}]:[]});
      const stale=installed && registryPersistedDir!==selectedDir();
      return JSON.stringify({state:stale?'stale':'fresh',
        refreshReasons:stale?['policy-changed','source-changed']:[],
        differences:stale?[{pluginId:'xi-work-events',changed:['record']}]:[],
        persisted:index(installedDir,registryPersistedDir),
        current:index(registryCurrentDir??installedDir,selectedDir())});
    }
    if(args[0]==='plugins' && args[1]==='inspect') {
      const source=args.includes('--runtime')?(runtimeDir??selectedDir()):selectedDir();
      return JSON.stringify({
      plugin:{id:'xi-work-events',rootDir:source,source:join(source,'index.mjs'),origin:'config',status:'loaded'},
      install:installed && source===installedDir?{source:'path',sourcePath:installedDir,installPath:installedDir}:undefined});
    }
    mutations.push(args);
    if(args[0]==='config' && args[1]==='set') {
      assert.ok(['plugins.entries.xi-work-events','plugins.load.paths'].includes(args[2]));
      if(args[2]==='plugins.load.paths' && casAdded) {
        config.plugins.load.paths.push(casAdded);casAdded=null;
        throw Object.assign(new Error('compare-and-swap rejected'),{xiCode:'openclaw_config_set_failed'});
      }
      const current=args[2]==='plugins.load.paths'?config.plugins.load?.paths:config.plugins.entries['xi-work-events'];
      if(args.includes('--expect-current-absent')) assert.equal(current,undefined);
      else assert.deepEqual(JSON.parse(args[args.indexOf('--expect-current-json')+1]),current);
      if(args[2]==='plugins.load.paths') {
        config.plugins.load.paths=JSON.parse(args[3]);
      } else config.plugins.entries['xi-work-events']=JSON.parse(args[3]);
    }
    if(args[0]==='plugins' && args[1]==='install') {
      installed=true;installedDir=args[2];
      xiPluginDirs.add(installedDir);
      config.plugins.load??={paths:[]};
      if(!config.plugins.load.paths.includes(installedDir)) config.plugins.load.paths.push(installedDir);
      if(!config.plugins.allow.includes('xi-work-events')) config.plugins.allow.push('xi-work-events');
      registryPersistedDir=selectedDir();
      if(installFailureOnce) {const code=installFailureOnce;installFailureOnce=null;throw Object.assign(new Error('private output'),{xiCode:code});}
    }
    if(args[0]==='plugins' && args[1]==='reload' && reloadFailure) throw reloadFailure;
    return '{}';
  };
  const fetchImpl=async(url,request)=>{
    const path=new URL(url).pathname;
    if(path==='/api/v1/work-event-ingest/config') {
      assert.ok(approved);
      assert.equal(request.headers.Authorization,`Bearer ${readFileSync(join(stateDir,'ingest.key'),'utf8')}`);
      return Response.json({version:'xinsight.work-event-ingest-config.v1',managed_renewal:true,installation_id:invitation.invitation_id,credential_expires_at:'2099-01-01T00:00:00Z',items:[{
        collection_id:collection,producer_project_key:`agent:${approvedAgentId}`,collection_revision:1,scope_generation:'1',
        reporter:{source_system_id:collection,source_instance:'openclaw-fixture',org_id:'7',project_id:'9'}}]});
    }
    const body=JSON.parse(request.body); requests.push({path,body});
    assert.equal(request.redirect,'error'); assert.equal(request.credentials,'omit');
    assert.equal(request.headers.Authorization,undefined);
    assert.equal(body.ingest_token,readFileSync(join(stateDir,'ingest.key'),'utf8'));
    assert.equal(statSync(join(stateDir,'ingest.key')).mode & 0o777,0o600);
    assert.equal(body.invitation_token,invitation.invitation_token);
    if(lost){lost=false;throw Error('response lost');}
    const state=approved?'approved':'candidates';
    return Response.json({schema_version:'xinsight.local-work-install-response.v1',action:path.split('/').at(-1),state,
      invitation:{invitation_id:invitation.invitation_id,state,source_system_id:collection,source_instance:'openclaw-fixture',bindings:approved?[{project_key:`agent:${approvedAgentId}`,collection_id:collection,project_id:'9'}]:undefined}});
  };
  return {root,config,requests,mutations,stateDir,invitationFile,native,fetchImpl,
    installedDir:()=>installedDir,setInstalledDir:dir=>{installedDir=dir;},
    setRegistryCurrentDir:dir=>{registryCurrentDir=dir;},
    selectedDir,addXiPluginDir:dir=>xiPluginDirs.add(dir),failCAS:path=>{casAdded=path;},
    setRuntimeDir:dir=>{runtimeDir=dir;},
    run:(agentId)=>installManagedOpenClaw({invitationFile,stateDir,pluginDir,native,fetchImpl,agentId}),
    approve:()=>{approved=true;},loseReply:()=>{lost=true;},
    failInstallAfterApply:(code='openclaw_plugin_install_timeout')=>{installFailureOnce=code;},
    failReload:error=>{reloadFailure=error;},stopGateway:()=>{reloadFailure=new Error('not running');}};
}

test('native lifecycle commands use the host deadline and report only fixed stage and failure codes',async()=>{
  const calls=[];let failure;
  const runner=nativeRunner('/reviewed/openclaw',async(_cli,args,options)=>{
    calls.push({command:args.slice(0,2),timeout:options.timeout});
    if(failure) throw failure;
    return {stdout:'ok'};
  });
  assert.equal(await runner(['--version']),'ok');
  assert.equal(await runner(['agents','list','--json']),'ok');
  for(const action of ['install','enable','reload']) assert.equal(await runner(['plugins',action,'private-argument']),'ok');
  assert.deepEqual(calls.map(call=>call.timeout),[60000,60000,600000,600000,600000]);
  failure=Object.assign(new Error('xib_private_token'),{killed:true,signal:'SIGTERM',stderr:'xib_private_token'});
  await assert.rejects(runner(['plugins','install','xib_private_token']),error=>
    error.xiCode==='openclaw_plugin_install_timeout' && !JSON.stringify(error).includes('xib_private_token'));
  failure=Object.assign(new Error('xib_private_token'),{code:2,stderr:'xib_private_token'});
  await assert.rejects(runner(['plugins','enable','xib_private_token']),error=>
    error.xiCode==='openclaw_plugin_enable_failed' && !JSON.stringify(error).includes('xib_private_token'));
  await assert.rejects(runner(['plugins','inspect','xib_private_token','--json']),error=>
    error.xiCode==='openclaw_plugin_inspection_failed' && !JSON.stringify(error).includes('xib_private_token'));
  failure=Object.assign(new Error('xib_private_token'),{code:'ERR_CHILD_PROCESS_STDIO_MAXBUFFER',killed:true});
  await assert.rejects(runner(['config','get','plugins','--json']),error=>
    error.xiCode==='openclaw_config_read_output_limit' && !JSON.stringify(error).includes('xib_private_token'));
  failure=Object.assign(new Error('xib_private_token'),{name:'AbortError',stderr:'xib_private_token'});
  await assert.rejects(runner(['plugins','reload','xib_private_token']),error=>
    error.xiCode==='openclaw_plugin_reload_interrupted' && !JSON.stringify(error).includes('xib_private_token'));
});

test('one-time setup waits for human mapping, resumes with identical credential, and preserves unrelated native config',async t=>{
  const h=harness(t);chmodSync(h.invitationFile,0o644);const models=structuredClone(h.config.models),other=structuredClone(h.config.plugins.entries.unrelated);
  assert.equal((await h.run()).state,'awaiting_administrator');assert.equal(h.mutations.length,0);
  assert.equal(statSync(h.invitationFile).mode & 0o777,0o600);
  const token=readFileSync(join(h.stateDir,'ingest.key'),'utf8'),bodyKey=readFileSync(join(h.stateDir,'body.key'),'utf8');
  h.approve();assert.equal((await h.run()).state,'activated_waiting_for_events');
  assert.equal(readFileSync(join(h.stateDir,'ingest.key'),'utf8'),token);
  assert.equal(readFileSync(join(h.stateDir,'body.key'),'utf8'),bodyKey);
  assert.deepEqual(h.config.models,models);assert.deepEqual(h.config.plugins.entries.unrelated,other);
  const config=JSON.stringify(h.config);assert.ok(!config.includes(token));assert.ok(!config.includes(bodyKey));
  assert.ok(!JSON.stringify(h.mutations).includes(token));assert.ok(!JSON.stringify(h.mutations).includes(bodyKey));
  assert.ok(!JSON.stringify(h.requests).includes(bodyKey));
  assert.deepEqual(h.config.plugins.allow,['unrelated','xi-work-events']);
  assert.equal(h.config.plugins.entries['xi-work-events'].config.provider,undefined);
  await h.run();assert.equal(h.mutations.filter(args=>args[0]==='plugins' && args[1]==='install').length,1);
});

test('lost candidate response reuses exact identity and never enables before approval; stopped Gateway stays visibly unconfirmed',async t=>{
  const h=harness(t);h.loseReply();await assert.rejects(h.run(),/installation_unconfirmed/);
  const first=h.requests[0].body;assert.equal(h.mutations.length,0);
  assert.equal((await h.run()).state,'awaiting_administrator');assert.deepEqual(h.requests[1].body,first);
  h.approve();h.stopGateway();
  const result=await h.run();
  assert.equal(result.state,'configured_activation_unconfirmed');
  assert.equal(result.activation_failure,'openclaw_plugin_reload_failed');
});

test('a timed-out native install that applied can resume without registering or installing twice',async t=>{
  const h=harness(t);
  assert.equal((await h.run()).state,'awaiting_administrator');
  h.approve();h.failInstallAfterApply();
  await assert.rejects(h.run(),error=>error.xiCode==='openclaw_plugin_install_timeout');
  assert.equal(h.config.plugins.entries['xi-work-events'].enabled,false);
  assert.equal(h.mutations.filter(args=>args[0]==='plugins' && args[1]==='install').length,1);
  assert.equal((await h.run()).state,'activated_waiting_for_events');
  assert.equal(h.mutations.filter(args=>args[0]==='plugins' && args[1]==='install').length,1);
  assert.equal(h.requests.filter(item=>item.path.endsWith('/candidates')).length,2);
});

test('a saved but failed first native install stops, then resumes without installing twice',async t=>{
  const h=harness(t);
  assert.equal((await h.run()).state,'awaiting_administrator');
  h.approve();h.failInstallAfterApply('openclaw_plugin_install_failed');
  await assert.rejects(h.run(),error=>error.xiCode==='openclaw_plugin_install_failed');
  assert.equal(h.config.plugins.entries['xi-work-events'].enabled,false);
  assert.equal(h.mutations.filter(args=>args[0]==='plugins' && args[1]==='install').length,1);
  assert.equal(h.mutations.filter(args=>args[0]==='plugins' && args[1]==='reload').length,0);
  assert.equal((await h.run()).state,'activated_waiting_for_events');
  assert.equal(h.mutations.filter(args=>args[0]==='plugins' && args[1]==='install').length,1);
});

test('reload timeout stays unconfirmed with a fixed reason and cancellation remains a failure',async t=>{
  const h=harness(t);await h.run();h.approve();
  h.failReload(Object.assign(new Error('private output'),{xiCode:'openclaw_plugin_reload_timeout'}));
  const result=await h.run();
  assert.equal(result.state,'configured_activation_unconfirmed');
  assert.equal(result.activation_failure,'openclaw_plugin_reload_timeout');
  h.failReload(Object.assign(new Error('private output'),{xiCode:'openclaw_plugin_reload_interrupted'}));
  await assert.rejects(h.run(),error=>error.xiCode==='openclaw_plugin_reload_interrupted' &&
    !JSON.stringify(error).includes('private output'));
  h.failReload(null);
  assert.equal((await h.run()).state,'activated_waiting_for_events');
});

test('invalid transport, oversized discovery, symlink inputs and workspace secret placement fail before external registration',async t=>{
  assert.throws(()=>parseInvitation({...invitation,origin:'http://xi.example.test'}));
  assert.throws(()=>discoveredProjects(Array.from({length:33},(_,i)=>({id:`a${i}`}))),/limit/);
  const h=harness(t);
  const link=join(h.root,'invite-link.json');symlinkSync(h.invitationFile,link);
  await assert.rejects(installManagedOpenClaw({invitationFile:link,stateDir:h.stateDir,pluginDir,native:h.native,fetchImpl:h.fetchImpl}),/invitation_file_invalid/);
  await assert.rejects(installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:join(h.root,'workspace-main','custody'),pluginDir,native:h.native,fetchImpl:h.fetchImpl}),/inside_agent_workspace/);
  assert.equal(h.requests.length,0);assert.ok(!existsSync(join(h.root,'workspace-main','custody')));
});

test('changed installation and unsupported host version do not create a second identity or mutate host config',async t=>{
  const h=harness(t);await h.run();const token=readFileSync(join(h.stateDir,'ingest.key'),'utf8');
  writeFileSync(h.invitationFile,JSON.stringify({...invitation,invitation_id:`xiei_${'b'.repeat(32)}`}));
  await assert.rejects(h.run(),/existing_installation_conflict/);
  assert.equal(readFileSync(join(h.stateDir,'ingest.key'),'utf8'),token);assert.equal(h.mutations.length,0);
  await assert.rejects(installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:join(h.root,'new'),pluginDir,
    native:async()=> 'OpenClaw 2026.9.6',fetchImpl:h.fetchImpl}),/version_not_validated/);
  assert.ok(!existsSync(join(h.root,'new')));
});

test('explicit reviewed package upgrade preserves the installation and resumes after uncertain reload',async t=>{
  const h=harness(t,{agents:[{id:'xi-acceptance'}],approvedAgentId:'xi-acceptance'});
  await h.run('xi-acceptance');h.approve();await h.run('xi-acceptance');
  const target=join(h.root,'reviewed-next');mkdirSync(target);
  for(const name of ['openclaw.plugin.json','package.json'])
    writeFileSync(join(target,name),readFileSync(join(pluginDir,name)));
  writeFileSync(join(target,'index.mjs'),'export default {};');
  const prior=readFileSync(join(h.stateDir,'installation.json'));
  const token=readFileSync(join(h.stateDir,'ingest.key'));
  const key=readFileSync(join(h.stateDir,'body.key'));
  const models=structuredClone(h.config.models);
  const unrelatedPath=join(h.root,'unrelated-plugin');
  h.config.plugins.load.paths.unshift(unrelatedPath);
  const count=h.mutations.length;
  const upgrade=()=>installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,upgradeFromDir:pluginDir,native:h.native,fetchImpl:h.fetchImpl,agentId:'xi-acceptance'});
  h.failInstallAfterApply();
  await assert.rejects(upgrade(),error=>error.xiCode==='openclaw_plugin_install_timeout');
  assert.equal(h.installedDir(),target);
  assert.equal(h.selectedDir(),pluginDir);
  assert.deepEqual(h.config.plugins.load.paths,[unrelatedPath,pluginDir,target]);
  assert.deepEqual(readFileSync(join(h.stateDir,'installation.json')),prior);
  h.failReload(Object.assign(new Error('uncertain'),{xiCode:'openclaw_plugin_reload_timeout'}));
  assert.equal((await upgrade()).state,'configured_activation_unconfirmed');
  assert.equal(h.installedDir(),target);
  assert.deepEqual(readFileSync(join(h.stateDir,'installation.json')),prior);
  h.failReload(null);
  assert.equal((await upgrade()).state,'activated_waiting_for_events');
  assert.equal(h.selectedDir(),target);
  assert.deepEqual(h.config.plugins.load.paths,[unrelatedPath,target]);
  assert.equal(JSON.parse(readFileSync(join(h.stateDir,'installation.json'))).plugin_dir,target);
  assert.equal(statSync(join(h.stateDir,'installation.json')).mode & 0o777,0o600);
  assert.deepEqual(readFileSync(join(h.stateDir,'ingest.key')),token);
  assert.deepEqual(readFileSync(join(h.stateDir,'body.key')),key);
  assert.deepEqual(h.config.models,models);
  assert.equal(h.mutations.slice(count).filter(args=>args[0]==='plugins' && args[1]==='install').length,1);
  assert.equal(h.requests.filter(item=>item.path.endsWith('/candidates')).length,2);
  assert.equal((await upgrade()).state,'activated_waiting_for_events');
  assert.equal((await installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir,upgradeFromDir:target,native:h.native,fetchImpl:h.fetchImpl,agentId:'xi-acceptance'})).state,
    'activated_waiting_for_events');
  assert.equal(h.installedDir(),pluginDir);
  assert.equal(h.selectedDir(),pluginDir);
  assert.deepEqual(h.config.plugins.load.paths,[unrelatedPath,pluginDir]);
  assert.equal(JSON.parse(readFileSync(join(h.stateDir,'installation.json'))).plugin_dir,pluginDir);
  assert.deepEqual(readFileSync(join(h.stateDir,'ingest.key')),token);
});

test('saved native install with old selected root and no inspect install resumes without a second install',async t=>{
  const h=harness(t);await h.run();h.approve();await h.run();
  const target=join(h.root,'reviewed-next');mkdirSync(target);
  for(const name of ['openclaw.plugin.json','package.json'])
    writeFileSync(join(target,name),readFileSync(join(pluginDir,name)));
  writeFileSync(join(target,'index.mjs'),'export default {};');
  const savedBefore=readFileSync(join(h.stateDir,'installation.json'));
  const upgrade=()=>installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,upgradeFromDir:pluginDir,native:h.native,fetchImpl:h.fetchImpl});
  h.failInstallAfterApply('openclaw_plugin_install_failed');
  await assert.rejects(upgrade(),error=>error.xiCode==='openclaw_plugin_install_failed');
  assert.equal(h.installedDir(),target);
  assert.equal(h.selectedDir(),pluginDir);
  assert.deepEqual(h.config.plugins.load.paths,[pluginDir,target]);
  assert.equal(JSON.parse(await h.native(['plugins','inspect','xi-work-events','--json'])).install,undefined);
  assert.deepEqual(readFileSync(join(h.stateDir,'installation.json')),savedBefore);
  const count=h.mutations.filter(args=>args[0]==='plugins' && args[1]==='install').length;
  assert.equal((await upgrade()).state,'activated_waiting_for_events');
  assert.equal(h.mutations.filter(args=>args[0]==='plugins' && args[1]==='install').length,count);
  assert.deepEqual(h.config.plugins.load.paths,[target]);
  assert.equal(JSON.parse(readFileSync(join(h.stateDir,'installation.json'))).plugin_dir,target);
});

test('upgrade resumes after an external exact path switch leaves the persisted registry row stale',async t=>{
  const h=harness(t);await h.run();h.approve();await h.run();
  const target=join(h.root,'reviewed-next');mkdirSync(target);
  for(const name of ['openclaw.plugin.json','package.json'])
    writeFileSync(join(target,name),readFileSync(join(pluginDir,name)));
  writeFileSync(join(target,'index.mjs'),'export default {};');
  const upgrade=()=>installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,upgradeFromDir:pluginDir,native:h.native,fetchImpl:h.fetchImpl});
  h.failInstallAfterApply('openclaw_plugin_install_failed');
  await assert.rejects(upgrade(),error=>error.xiCode==='openclaw_plugin_install_failed');
  h.config.plugins.load.paths=[target];
  const registry=JSON.parse(await h.native(['plugins','registry','--json']));
  assert.equal(registry.state,'stale');
  assert.equal(registry.persisted.plugins[0].rootDir,pluginDir);
  assert.equal(registry.current.plugins[0].rootDir,target);
  const count=h.mutations.filter(args=>args[0]==='plugins' && args[1]==='install').length;
  assert.equal((await upgrade()).state,'activated_waiting_for_events');
  assert.equal(h.mutations.filter(args=>args[0]==='plugins' && args[1]==='install').length,count);
  assert.equal(JSON.parse(readFileSync(join(h.stateDir,'installation.json'))).plugin_dir,target);
});

test('upgrade stops before host mutation when persisted and current native records disagree',async t=>{
  const h=harness(t);await h.run();h.approve();await h.run();
  const target=join(h.root,'reviewed-next');mkdirSync(target);
  for(const name of ['openclaw.plugin.json','package.json'])
    writeFileSync(join(target,name),readFileSync(join(pluginDir,name)));
  writeFileSync(join(target,'index.mjs'),'export default {};');
  h.setRegistryCurrentDir(target);
  const count=h.mutations.length;
  await assert.rejects(installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,upgradeFromDir:pluginDir,native:h.native,fetchImpl:h.fetchImpl}),/existing_plugin_source_conflict/);
  assert.equal(h.mutations.length,count);
  assert.equal(h.installedDir(),pluginDir);
});

test('upgrade resumes after a native path CAS conflict without repeating the committed install',async t=>{
  const h=harness(t);await h.run();h.approve();await h.run();
  const target=join(h.root,'reviewed-next');mkdirSync(target);
  for(const name of ['openclaw.plugin.json','package.json'])
    writeFileSync(join(target,name),readFileSync(join(pluginDir,name)));
  writeFileSync(join(target,'index.mjs'),'export default {};');
  const prior=readFileSync(join(h.stateDir,'installation.json'));
  const upgrade=()=>installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,upgradeFromDir:pluginDir,native:h.native,fetchImpl:h.fetchImpl});
  const concurrent=join(h.root,'concurrent-unrelated');h.failCAS(concurrent);
  await assert.rejects(upgrade(),error=>error.xiCode==='openclaw_config_set_failed');
  assert.equal(h.installedDir(),target);
  assert.equal(h.selectedDir(),pluginDir);
  assert.deepEqual(h.config.plugins.load.paths,[pluginDir,target,concurrent]);
  assert.deepEqual(readFileSync(join(h.stateDir,'installation.json')),prior);
  assert.equal((await upgrade()).state,'activated_waiting_for_events');
  assert.deepEqual(h.config.plugins.load.paths,[target,concurrent]);
  assert.equal(h.mutations.filter(args=>args[0]==='plugins' && args[1]==='install' && args[2]===target).length,1);
});

test('upgrade rejects a third configured XI source before native mutation',async t=>{
  const h=harness(t);await h.run();h.approve();await h.run();
  const target=join(h.root,'reviewed-next'),third=join(h.root,'third-source');
  for(const dir of [target,third]) {
    mkdirSync(dir);
    for(const name of ['openclaw.plugin.json','package.json'])
      writeFileSync(join(dir,name),readFileSync(join(pluginDir,name)));
    writeFileSync(join(dir,'index.mjs'),'export default {};');
  }
  h.config.plugins.load.paths.push(third);h.addXiPluginDir(third);
  const count=h.mutations.length;
  await assert.rejects(installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,upgradeFromDir:pluginDir,native:h.native,fetchImpl:h.fetchImpl}),/existing_plugin_source_conflict/);
  assert.equal(h.mutations.length,count);
  assert.equal(h.installedDir(),pluginDir);
});

test('upgrade does not save the new package while CLI runtime import still selects the old source',async t=>{
  const h=harness(t);await h.run();h.approve();await h.run();
  const target=join(h.root,'reviewed-next');mkdirSync(target);
  for(const name of ['openclaw.plugin.json','package.json'])
    writeFileSync(join(target,name),readFileSync(join(pluginDir,name)));
  writeFileSync(join(target,'index.mjs'),'export default {};');
  const prior=readFileSync(join(h.stateDir,'installation.json'));
  const upgrade=()=>installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,upgradeFromDir:pluginDir,native:h.native,fetchImpl:h.fetchImpl});
  h.setRuntimeDir(pluginDir);
  await assert.rejects(upgrade(),/plugin_upgrade_unconfirmed/);
  assert.equal(h.installedDir(),target);
  assert.equal(h.selectedDir(),target);
  assert.deepEqual(readFileSync(join(h.stateDir,'installation.json')),prior);
  h.setRuntimeDir(null);
  assert.equal((await upgrade()).state,'activated_waiting_for_events');
  assert.equal(h.mutations.filter(args=>args[0]==='plugins' && args[1]==='install' && args[2]===target).length,1);
});

test('plugin upgrade refuses a changed package contract or unrelated saved source before host mutation',async t=>{
  const h=harness(t);await h.run();h.approve();await h.run();
  const target=join(h.root,'reviewed-next');mkdirSync(target);
  for(const name of ['openclaw.plugin.json','package.json'])
    writeFileSync(join(target,name),readFileSync(join(pluginDir,name)));
  writeFileSync(join(target,'index.mjs'),'export default {};');
  const count=h.mutations.length;
  await assert.rejects(installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,native:h.native,fetchImpl:h.fetchImpl}),/existing_plugin_source_conflict/);
  writeFileSync(join(target,'openclaw.plugin.json'),'{"id":"different"}');
  await assert.rejects(installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,upgradeFromDir:pluginDir,native:h.native,fetchImpl:h.fetchImpl}),/upgrade_package_changed/);
  writeFileSync(join(target,'openclaw.plugin.json'),readFileSync(join(pluginDir,'openclaw.plugin.json')));
  const saved=JSON.parse(readFileSync(join(h.stateDir,'installation.json')));
  writeFileSync(join(h.stateDir,'installation.json'),JSON.stringify({...saved,plugin_dir:'/unrelated/package'}));
  await assert.rejects(installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,upgradeFromDir:pluginDir,native:h.native,fetchImpl:h.fetchImpl}),/existing_installation_conflict/);
  assert.equal(h.mutations.length,count);
  assert.equal(h.installedDir(),pluginDir);
});

test('upgrade refuses missing existing keys before changing plugin or installation state',async t=>{
  const h=harness(t);await h.run();h.approve();await h.run();
  const target=join(h.root,'reviewed-next');mkdirSync(target);
  for(const name of ['openclaw.plugin.json','package.json'])
    writeFileSync(join(target,name),readFileSync(join(pluginDir,name)));
  writeFileSync(join(target,'index.mjs'),'export default {};');
  const upgrade=()=>installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,upgradeFromDir:pluginDir,native:h.native,fetchImpl:h.fetchImpl});
  const before=readFileSync(join(h.stateDir,'installation.json'));
  const token=readFileSync(join(h.stateDir,'ingest.key'));
  const count=h.mutations.length;
  rmSync(join(h.stateDir,'ingest.key'));
  await assert.rejects(upgrade(),/managed_ingest_credential_invalid/);
  assert.equal(existsSync(join(h.stateDir,'ingest.key')),false);
  writeFileSync(join(h.stateDir,'ingest.key'),token,{mode:0o600});
  rmSync(join(h.stateDir,'body.key'));
  await assert.rejects(upgrade(),/managed_body_key_invalid/);
  assert.equal(existsSync(join(h.stateDir,'body.key')),false);
  assert.deepEqual(readFileSync(join(h.stateDir,'installation.json')),before);
  assert.equal(h.mutations.length,count);
  assert.equal(h.installedDir(),pluginDir);
});

test('upgrade atomically persists a protected installation record above the token-file limit',async t=>{
  const h=harness(t);await h.run();h.approve();await h.run();
  const target=join(h.root,'reviewed-next');mkdirSync(target);
  for(const name of ['openclaw.plugin.json','package.json'])
    writeFileSync(join(target,name),readFileSync(join(pluginDir,name)));
  writeFileSync(join(target,'index.mjs'),'export default {};');
  const statePath=join(h.stateDir,'installation.json');
  const saved=JSON.parse(readFileSync(statePath));
  writeFileSync(statePath,JSON.stringify({...saved,operator_note:'x'.repeat(5000)}));
  assert.equal((await installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,upgradeFromDir:pluginDir,native:h.native,fetchImpl:h.fetchImpl})).state,
    'activated_waiting_for_events');
  const updated=JSON.parse(readFileSync(statePath));
  assert.equal(updated.plugin_dir,target);
  assert.equal(updated.operator_note,'x'.repeat(5000));
  assert.equal(statSync(statePath).mode & 0o777,0o600);
});

test('upgrade accepts the original native source and rejects a third native source',async t=>{
  const h=harness(t);await h.run();h.approve();await h.run();
  const target=join(h.root,'reviewed-next');mkdirSync(target);
  const third=join(h.root,'unrelated-plugin-source');mkdirSync(third);
  for(const name of ['openclaw.plugin.json','package.json'])
    writeFileSync(join(target,name),readFileSync(join(pluginDir,name)));
  writeFileSync(join(target,'index.mjs'),'export default {};');
  const upgrade=()=>installManagedOpenClaw({invitationFile:h.invitationFile,stateDir:h.stateDir,
    pluginDir:target,upgradeFromDir:pluginDir,native:h.native,fetchImpl:h.fetchImpl});
  const count=h.mutations.length;
  h.setInstalledDir(third);
  await assert.rejects(upgrade(),/existing_plugin_source_conflict/);
  assert.equal(h.mutations.length,count);
  h.setInstalledDir(pluginDir);
  assert.equal((await upgrade()).state,'activated_waiting_for_events');
  assert.equal(h.installedDir(),target);
  assert.equal(JSON.parse(readFileSync(join(h.stateDir,'installation.json'))).plugin_dir,target);
});

test('selected Agent alone is registered and frozen across retries without changing native roster discovery',async t=>{
  const h=harness(t,{agents:[{id:'personal',name:'Private Agent'},{id:'xi-acceptance',name:'Acceptance Agent'}],approvedAgentId:'xi-acceptance'});
  assert.equal((await h.run('xi-acceptance')).state,'awaiting_administrator');
  assert.deepEqual(h.requests[0].body.projects,[{project_key:'agent:xi-acceptance',label:'Acceptance Agent'}]);
  const state=JSON.parse(readFileSync(join(h.stateDir,'installation.json'),'utf8'));
  assert.equal(state.selected_agent_id,'xi-acceptance');
  assert.deepEqual(state.projects,h.requests[0].body.projects);
  const token=readFileSync(join(h.stateDir,'ingest.key'),'utf8');
  await assert.rejects(h.run('personal'),/candidate_configuration_changed/);
  await assert.rejects(h.run(),/candidate_configuration_changed/);
  assert.equal(readFileSync(join(h.stateDir,'ingest.key'),'utf8'),token);
  assert.equal(h.requests.length,2,'changed selection must fail before registration');
  assert.equal(h.mutations.length,0);
  h.approve();
  assert.equal((await h.run('xi-acceptance')).state,'activated_waiting_for_events');
  assert.equal(h.config.plugins.entries['xi-work-events'].config.selectedAgentId,'xi-acceptance');
  assert.deepEqual(h.requests.filter(item=>item.path.endsWith('/candidates')).map(item=>item.body.projects),
    [state.projects,state.projects]);
});

test('unknown Agent and custody inside any unselected Agent workspace fail before identity or registration',async t=>{
  const h=harness(t,{agents:[{id:'personal'},{id:'xi-acceptance'}]});
  await assert.rejects(h.run('unknown'),/agent_not_found/);
  await assert.rejects(h.run('bad/id'),/agent_selection_invalid/);
  assert.equal(existsSync(h.stateDir),false);
  await assert.rejects(installManagedOpenClaw({invitationFile:h.invitationFile,
    stateDir:join(h.root,'workspace-personal','custody'),pluginDir,native:h.native,fetchImpl:h.fetchImpl,
    agentId:'xi-acceptance'}),/inside_agent_workspace/);
  assert.equal(h.requests.length,0);
  assert.equal(existsSync(join(h.root,'workspace-personal','custody')),false);
});
