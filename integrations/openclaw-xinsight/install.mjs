#!/usr/bin/env node
import { execFile } from 'node:child_process';
import { promisify, isDeepStrictEqual } from 'node:util';
import { createHash, randomBytes } from 'node:crypto';
import { constants, openSync, closeSync, fstatSync, fchmodSync, lstatSync, mkdirSync, readFileSync, realpathSync } from 'node:fs';
import { dirname, isAbsolute, relative, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { prepareManagedPluginConfig, normalizedGatewayURL } from './managed-config.mjs';
import { createPrivateFile, readPrivateFile, replacePrivateFile } from './managed-secret-files.mjs';
import { fetchWorkEventConfiguration } from './transport.mjs';

const exec = promisify(execFile);
const entryPath = 'plugins.entries.xi-work-events';
const responseSchema = 'xinsight.local-work-install-response.v1';
const rootDir = dirname(fileURLToPath(import.meta.url));
const fail = code => { throw Object.assign(new Error(code),{xiCode:code}); };
const plain = value => value && typeof value === 'object' && !Array.isArray(value);

// A browser may save the selected invitation with the user's default mode.
// Narrow only this explicitly selected, owned, regular file before reading it.
function readInvitation(path) {
  if(typeof path !== 'string' || !isAbsolute(path)) fail('invitation_requires_absolute_private_file');
  let fd;
  try {
    const named=lstatSync(path);
    if(!named.isFile() || named.size<1 || named.size>16384 || realpathSync(path)!==path ||
       typeof process.getuid!=='function' || named.uid!==process.getuid()) fail('invitation_file_invalid');
    fd=openSync(path,constants.O_RDONLY|constants.O_NOFOLLOW);
    const opened=fstatSync(fd);
    if(opened.dev!==named.dev || opened.ino!==named.ino) fail('invitation_file_changed');
    fchmodSync(fd,0o600);
  } finally { if(fd!==undefined) closeSync(fd); }
  return readPrivateFile(path,16384);
}

export function parseInvitation(value) {
  if (!plain(value) || value.schema_version !== 'xinsight.openclaw-invitation.v1' ||
      !/^xiei_[a-f0-9]{32}$/.test(value.invitation_id) ||
      !/^xiet_[A-Za-z0-9_-]{43}$/.test(value.invitation_token) ||
      !Number.isFinite(Date.parse(value.expires_at))) fail('invitation_invalid');
  const origin = normalizedGatewayURL(value.origin);
  if (new URL(origin).pathname !== '/') fail('invitation_invalid');
  return {...value, origin, ur_base_url:normalizedGatewayURL(value.ur_base_url)};
}

export function discoveredProjects(agents) {
  if (!Array.isArray(agents) || agents.length < 1 || agents.length > 32) fail('agent_discovery_limit_or_empty');
  const seen = new Set();
  return agents.map(agent => {
    if (!plain(agent) || !/^[a-z0-9][a-z0-9_-]{0,127}$/.test(agent.id) || seen.has(agent.id)) fail('agent_discovery_invalid');
    seen.add(agent.id);
    const label = agent.name ?? agent.identityName ?? agent.id;
    if (typeof label !== 'string' || label.length < 1 || label.length > 128 || label.trim() !== label || /[\x00-\x1f\x7f]/.test(label)) fail('agent_discovery_invalid');
    return {project_key:`agent:${agent.id}`,label};
  }).sort((a,b) => a.project_key.localeCompare(b.project_key));
}

function privateDirectory(path) {
  if (!isAbsolute(path)) fail('private_directory_requires_absolute_path');
  mkdirSync(path,{recursive:true,mode:0o700});
  const canonical=realpathSync(path), stat=lstatSync(canonical);
  if (!stat.isDirectory() || (stat.mode & 0o077) !== 0 || typeof process.getuid !== 'function' ||
      (stat.uid !== process.getuid() && stat.uid !== 0)) fail('private_directory_requires_0700');
  return canonical;
}
function within(parent, child) {
  const path=relative(parent,child);
  return path === '' || (path !== '..' && !path.startsWith('../') && !isAbsolute(path));
}
function existingPrivate(path, maximum=4096) {
  try { lstatSync(path); } catch(error) { if(error.code === 'ENOENT') return null; throw error; }
  return readPrivateFile(path,maximum);
}
function persistentSecret(path, generate) {
  const current=existingPrivate(path);
  if (current) return current.toString('utf8');
  const value=generate(); createPrivateFile(path,Buffer.from(value)); return value;
}

async function responseJSON(response) {
  if (!response.ok || !response.body || !response.headers.get('content-type')?.includes('application/json')) fail('installation_unconfirmed');
  const chunks=[]; let size=0; const reader=response.body.getReader();
  try {
    for (;;) {
      const {done,value}=await reader.read(); if(done) break;
      size+=value.length; if(size>65536) fail('installation_response_invalid'); chunks.push(value);
    }
    return JSON.parse(Buffer.concat(chunks,size).toString('utf8'));
  } catch { await reader.cancel().catch(()=>{}); fail('installation_response_invalid'); }
  finally { reader.releaseLock(); }
}
async function register(invitation,token,action,projects,fetchImpl) {
  const body={invitation_token:invitation.invitation_token,ingest_token:token,...(projects?{projects}:{})};
  let response;
  try { response=await fetchImpl(`${invitation.origin}/api/v1/work-event-install/${action}`,{
    method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body),
    redirect:'error',credentials:'omit',signal:AbortSignal.timeout(15000)}); }
  catch { fail('installation_unconfirmed'); }
  const result=await responseJSON(response);
  if (!plain(result) || result.schema_version !== responseSchema || result.action !== action ||
      !['candidates','approved'].includes(result.state) || result.invitation?.invitation_id !== invitation.invitation_id ||
      result.invitation.state !== result.state) fail('installation_response_invalid');
  return result.invitation;
}

const nativeStage = args => {
  if(args[0]==='plugins') {
    if(args[1]==='install') return 'plugin_install';
    if(args[1]==='enable') return 'plugin_enable';
    if(args[1]==='reload') return 'plugin_reload';
    if(args[1]==='list') return 'plugin_inventory';
    if(args[1]==='inspect') return 'plugin_inspection';
    if(args[1]==='registry') return 'plugin_registry';
  }
  if(args[0]==='config') return args[1]==='set'?'config_set':'config_read';
  if(args[0]==='agents' && args[1]==='list') return 'agent_discovery';
  if(args[0]==='--version') return 'version';
  return 'command';
};
export function nativeRunner(cli='openclaw',execCommand=exec) {
  return async (args) => {
    const stage=nativeStage(args);
    // A timed-out lifecycle command may already have applied in the Gateway.
    // Let the native client's bounded operation finish before classifying it.
    const lifecycle=['plugin_install','plugin_enable','plugin_reload'].includes(stage);
    try {
      const {stdout}=await execCommand(cli,args,{timeout:lifecycle?600000:60000,maxBuffer:4*1024*1024});
      return stdout;
    } catch(error) {
      const kind=error?.name==='AbortError'?'interrupted':
        error?.code==='ERR_CHILD_PROCESS_STDIO_MAXBUFFER'?'output_limit':
        error?.killed===true || error?.code==='ETIMEDOUT'?'timeout':
        error?.signal?'interrupted':'failed';
      fail(`openclaw_${stage}_${kind}`);
    }
  };
}
function jsonOutput(raw) {
  try { return JSON.parse(raw); } catch { fail('openclaw_response_invalid'); }
}
async function inspectPlugin(native) {
  const inspection=jsonOutput(await native(['plugins','inspect','xi-work-events','--json']));
  if(inspection.plugin?.id!=='xi-work-events' || typeof inspection.plugin.rootDir!=='string')
    fail('plugin_upgrade_unconfirmed');
  return inspection;
}
function inspectedDirectory(path) {
  if(typeof path!=='string' || !isAbsolute(path)) fail('plugin_upgrade_unconfirmed');
  try { return realpathSync(path); } catch { fail('plugin_upgrade_unconfirmed'); }
}
function installedDirectory(inspection) {
  if(inspection.install?.source!=='path' ||
     inspectedDirectory(inspection.install.sourcePath)!==inspectedDirectory(inspection.install.installPath))
    fail('existing_plugin_source_conflict');
  return inspectedDirectory(inspection.install.installPath);
}
async function registryInstalledDirectory(native, inspection, allowedRoots) {
  const registry=jsonOutput(await native(['plugins','registry','--json']));
  if(!['fresh','stale'].includes(registry.state) || !plain(registry.persisted) || !plain(registry.current))
    fail('plugin_upgrade_unconfirmed');
  if(registry.state==='stale' && (!Array.isArray(registry.refreshReasons) ||
      registry.refreshReasons.some(reason=>!['policy-changed','source-changed'].includes(reason)) ||
      !Array.isArray(registry.differences) ||
      registry.differences.some(item=>item.pluginId!=='xi-work-events')))
    fail('plugin_upgrade_unconfirmed');
  const persisted=registry.persisted.installRecords?.['xi-work-events'];
  const current=registry.current.installRecords?.['xi-work-events'];
  if(!plain(persisted) || !isDeepStrictEqual(persisted,current)) fail('existing_plugin_source_conflict');
  const recorded=installedDirectory({install:persisted});
  const selected=inspectedDirectory(inspection.plugin.rootDir);
  for(const [index,isCurrent] of [[registry.persisted,false],[registry.current,true]]) {
    if(!Array.isArray(index.plugins)) fail('plugin_upgrade_unconfirmed');
    const rows=index.plugins.filter(row=>row.pluginId==='xi-work-events');
    if(rows.length!==1) fail('plugin_upgrade_unconfirmed');
    const rowRoot=inspectedDirectory(rows[0].rootDir);
    if(!allowedRoots.includes(rowRoot) || ((isCurrent || registry.state==='fresh') && rowRoot!==selected))
      fail('plugin_upgrade_unconfirmed');
  }
  if(inspection.install===undefined && selected===recorded) fail('plugin_upgrade_unconfirmed');
  if(inspection.install!==undefined && installedDirectory(inspection)!==recorded)
    fail('existing_plugin_source_conflict');
  return recorded;
}
function upgradePaths(paths,source,target) {
  if(!Array.isArray(paths) || paths.some(path=>typeof path!=='string')) fail('plugin_load_paths_conflict');
  const old=paths.filter(path=>path===source).length, next=paths.filter(path=>path===target).length;
  if(old>1 || next>1 || (old===0 && next!==1) || (old===1 && next>1)) fail('plugin_load_paths_conflict');
  // OpenClaw appends a linked target. Keep its original slot and all unrelated
  // paths, removing only the extra copy appended by native install.
  return old===1 ? paths.flatMap(path=>path===source?[target]:path===target?[]:[path]) : paths;
}
function rejectThirdPluginPath(paths,source,target) {
  for(const path of paths) {
    if(path===source || path===target || !isAbsolute(path)) continue;
    try {
      const manifest=JSON.parse(readFileSync(resolve(path,'openclaw.plugin.json'),'utf8'));
      if(manifest.id==='xi-work-events') fail('existing_plugin_source_conflict');
    } catch(error) { if(error?.xiCode) throw error; }
  }
}

// The caller supplies an administrator-created invitation file, never a token
// argument. Every retry reuses protected files and the original candidate list.
// OpenClaw alone writes its config and install index; no whole-config rewrite.
export async function installManagedOpenClaw({invitationFile,stateDir,pluginDir=rootDir,
  upgradeFromDir,agentId,native=nativeRunner(),fetchImpl=fetch}={}) {
  if(agentId!==undefined && (typeof agentId!=='string' || !/^[a-z0-9][a-z0-9_-]{0,127}$/.test(agentId))) fail('agent_selection_invalid');
  const invitation=parseInvitation(JSON.parse(readInvitation(invitationFile).toString('utf8')));
  const version=(await native(['--version'])).trim();
  if (!/^OpenClaw 2026\.9\.5(?: \([^\r\n)]+\))?$/.test(version)) fail('openclaw_version_not_validated');
  const agents=jsonOutput(await native(['agents','list','--json']));
  const roster=discoveredProjects(agents);
  const projects=agentId===undefined?roster:roster.filter(project=>project.project_key===`agent:${agentId}`);
  if(projects.length!==1 && agentId!==undefined) fail('agent_not_found');
  if (!isAbsolute(stateDir)) fail('private_directory_requires_absolute_path');
  const proposed=resolve(stateDir);
  for (const agent of agents) {
    if (typeof agent.workspace !== 'string' || !isAbsolute(agent.workspace)) fail('agent_workspace_unresolved');
    let workspace=resolve(agent.workspace); try { workspace=realpathSync(workspace); } catch { /* native configured path may not exist yet */ }
    if (within(workspace,proposed)) fail('custody_directory_inside_agent_workspace');
  }
  const models=jsonOutput(await native(['config','get','models','--json']));
  const plugins=jsonOutput(await native(['config','get','plugins','--json']));
  const inventory=jsonOutput(await native(['plugins','list','--json']));
  pluginDir=realpathSync(pluginDir);
  if(upgradeFromDir!==undefined) {
    if(typeof upgradeFromDir!=='string' || !isAbsolute(upgradeFromDir)) fail('upgrade_source_invalid');
    upgradeFromDir=realpathSync(upgradeFromDir);
    if(upgradeFromDir===pluginDir) fail('upgrade_source_invalid');
    // This bounded migration retains the exact plugin identity and capability
    // declaration. Source code may change; the native package contract may not.
    for(const name of ['openclaw.plugin.json','package.json']) {
      if(!readFileSync(resolve(upgradeFromDir,name)).equals(readFileSync(resolve(pluginDir,name)))) fail('upgrade_package_changed');
    }
  }
  const manifest=JSON.parse(readFileSync(resolve(pluginDir,'openclaw.plugin.json'),'utf8'));
  const pkg=JSON.parse(readFileSync(resolve(pluginDir,'package.json'),'utf8'));
  let extensionValid=false;
  try { extensionValid=lstatSync(resolve(pluginDir,'index.mjs')).isFile(); } catch { /* invalid package */ }
  if (manifest.id !== 'xi-work-events' || pkg.name !== 'xi-work-events' ||
      !pkg.openclaw?.extensions?.includes('./index.mjs') || !extensionValid)
    fail('plugin_package_invalid');
  if (!Array.isArray(inventory.plugins)) fail('openclaw_response_invalid');
  const installed=inventory.plugins.filter(item=>item.id==='xi-work-events');
  const installedDir=installed.length===1 && typeof installed[0].rootDir==='string' ? realpathSync(installed[0].rootDir) : null;
  if (installed.length>1 || (installed[0] && installedDir!==pluginDir && installedDir!==upgradeFromDir) ||
      (upgradeFromDir!==undefined && !installed[0])) fail('existing_plugin_source_conflict');
  if(upgradeFromDir!==undefined) {
    const inspection=await inspectPlugin(native);
    const recorded=await registryInstalledDirectory(native,inspection,[upgradeFromDir,pluginDir]);
    if(recorded!==upgradeFromDir && recorded!==pluginDir) fail('existing_plugin_source_conflict');
    if(inspectedDirectory(inspection.plugin.rootDir)!==installedDir) fail('existing_plugin_source_conflict');
    upgradePaths(plugins.load?.paths,upgradeFromDir,pluginDir);
    rejectThirdPluginPath(plugins.load.paths,upgradeFromDir,pluginDir);
    try { if(!lstatSync(stateDir).isDirectory()) fail('existing_installation_conflict'); }
    catch(error) { if(error?.xiCode) throw error; fail('existing_installation_conflict'); }
  }
  const directory=privateDirectory(stateDir);
  for (const agent of agents) {
    let workspace=resolve(agent.workspace); try { workspace=realpathSync(workspace); } catch {}
    if (within(workspace,directory)) fail('custody_directory_inside_agent_workspace');
  }
  const settings={origin:invitation.origin,urBaseUrl:invitation.ur_base_url,installationId:invitation.invitation_id,
    tokenFile:resolve(directory,'ingest.key'),bodyKeyFile:resolve(directory,'body.key'),
    outboxPath:resolve(directory,'metadata','queue.sqlite'),bodyOutboxPath:resolve(directory,'bodies','queue.sqlite'),
    ...(agentId===undefined?{}:{selectedAgentId:agentId})};
  const prepared=prepareManagedPluginConfig({models,plugins},settings);
  const statePath=resolve(directory,'installation.json');
  const identity={schema_version:'xinsight.openclaw-host.v1',invitation_id:invitation.invitation_id,
    invitation_digest:createHash('sha256').update(invitation.invitation_token).digest('hex'),
    origin:invitation.origin,ur_base_url:invitation.ur_base_url,plugin_dir:pluginDir};
  const previous=existingPrivate(statePath,16384);
  let nextState;
  let frozenProjects=projects;
  if (previous) {
    const saved=JSON.parse(previous.toString('utf8'));
    if((saved.selected_agent_id??null)!==(agentId??null)) fail('candidate_configuration_changed');
    for(const [key,value] of Object.entries(identity)) {
      if(saved[key]!==value && !(key==='plugin_dir' && upgradeFromDir!==undefined && saved[key]===upgradeFromDir))
        fail('existing_installation_conflict');
    }
    if(upgradeFromDir!==undefined && plugins.entries?.['xi-work-events']?.config?.installationId!==invitation.invitation_id)
      fail('existing_installation_conflict');
    frozenProjects=saved.projects;
    if(!Array.isArray(frozenProjects) || (agentId!==undefined &&
       (frozenProjects.length!==1 || frozenProjects[0]?.project_key!==`agent:${agentId}`)) ||
       frozenProjects.some(item=>!projects.some(current=>current.project_key===item.project_key))) fail('candidate_configuration_changed');
    if(upgradeFromDir!==undefined) {
      nextState=Buffer.from(JSON.stringify({...saved,plugin_dir:pluginDir}));
      if(nextState.length>16384) fail('installation_state_too_large');
    }
  } else {
    if(upgradeFromDir!==undefined) fail('existing_installation_conflict');
    if(Date.now()>=Date.parse(invitation.expires_at)) fail('invitation_expired');
    createPrivateFile(statePath,Buffer.from(JSON.stringify({...identity,...(agentId===undefined?{}:{selected_agent_id:agentId}),projects})),16384);
  }
  const token=upgradeFromDir!==undefined ? existingPrivate(settings.tokenFile)?.toString('utf8') :
    persistentSecret(settings.tokenFile,()=>`xib_${randomBytes(32).toString('base64url')}`);
  if(!/^xib_[A-Za-z0-9_-]{43}$/.test(token)) fail('managed_ingest_credential_invalid');
  const bodyKey=upgradeFromDir!==undefined ? existingPrivate(settings.bodyKeyFile)?.toString('utf8') :
    persistentSecret(settings.bodyKeyFile,()=>randomBytes(32).toString('hex'));
  if(!/^[a-f0-9]{64}$/.test(bodyKey)) fail('managed_body_key_invalid');
  // An already-installed host may have renewed its credential. Authenticate
  // the current file against the exact installation, not the original token
  // digest from the one-time candidate exchange.
  const existing = installed[0] && plugins.entries?.['xi-work-events']?.config?.installationId===invitation.invitation_id;
  if(existing) {
    const configuration=await fetchWorkEventConfiguration({origin:invitation.origin,token,fetchImpl});
    if(configuration.managed_renewal!==true || configuration.installation_id!==invitation.invitation_id ||
       configuration.items.length===0) fail('existing_installation_unconfirmed');
  } else {
    await register(invitation,token,'candidates',frozenProjects,fetchImpl);
    const approval=await register(invitation,token,'pickup',undefined,fetchImpl);
    if(approval.state!=='approved') return {state:'awaiting_administrator',invitation_id:invitation.invitation_id,projects:frozenProjects};
    const configuration=await fetchWorkEventConfiguration({origin:invitation.origin,token,fetchImpl});
    const bindings=configuration.items;
    if(configuration.managed_renewal!==true || configuration.installation_id!==invitation.invitation_id ||
       !Array.isArray(approval.bindings) || approval.bindings.length===0 ||
       approval.bindings.some(item=>!frozenProjects.some(project=>project.project_key===item.project_key) || !bindings.some(binding=>
         binding.producer_project_key===item.project_key && binding.collection_id===item.collection_id &&
         binding.reporter.source_system_id===approval.source_system_id && binding.reporter.source_instance===approval.source_instance &&
         binding.reporter.project_id===item.project_id))) fail('approved_binding_unconfirmed');
  }

  let current=plugins.entries?.['xi-work-events'];
  const desired=prepared.config.plugins.entries['xi-work-events'];
  if(!installed[0]) {
    const disabled={...desired,enabled:false};
    await native(['config','set',entryPath,JSON.stringify(disabled),'--strict-json',
      ...(current===undefined?['--expect-current-absent']:['--expect-current-json',JSON.stringify(current)])]);
    await native(['plugins','install',pluginDir,'--link','--force','--accept-capabilities']);
    current=jsonOutput(await native(['config','get',entryPath,'--json']));
  } else if(upgradeFromDir!==undefined) {
    let inspection=await inspectPlugin(native);
    let recorded=await registryInstalledDirectory(native,inspection,[upgradeFromDir,pluginDir]);
    if(recorded===upgradeFromDir) {
      await native(['plugins','install',pluginDir,'--link','--force','--accept-capabilities']);
      inspection=await inspectPlugin(native);
      recorded=await registryInstalledDirectory(native,inspection,[upgradeFromDir,pluginDir]);
    }
    if(recorded!==pluginDir) fail('plugin_upgrade_unconfirmed');
    const latest=jsonOutput(await native(['config','get','plugins','--json']));
    const before=latest.load?.paths;
    const after=upgradePaths(before,upgradeFromDir,pluginDir);
    rejectThirdPluginPath(before,upgradeFromDir,pluginDir);
    if(JSON.stringify(after)!==JSON.stringify(before)) {
      await native(['config','set','plugins.load.paths',JSON.stringify(after),'--strict-json',
        '--expect-current-json',JSON.stringify(before)]);
    }
    const saved=jsonOutput(await native(['config','get','plugins','--json']));
    if(JSON.stringify(saved.load?.paths)!==JSON.stringify(after)) fail('plugin_upgrade_unconfirmed');
    const selected=await inspectPlugin(native);
    if(installedDirectory(selected)!==pluginDir || inspectedDirectory(selected.plugin.rootDir)!==pluginDir)
      fail('plugin_upgrade_unconfirmed');
    current=jsonOutput(await native(['config','get',entryPath,'--json']));
  }
  if(JSON.stringify(current)!==JSON.stringify(desired)) {
    await native(['config','set',entryPath,JSON.stringify(desired),'--strict-json',
      ...(current===undefined?['--expect-current-absent']:['--expect-current-json',JSON.stringify(current)])]);
  }
  await native(['plugins','enable','xi-work-events','--accept-capabilities']);
  // Native reload owns live replacement. If Gateway is stopped or its reply is
  // uncertain, report saved/unconfirmed; never silently restart the host.
  try { await native(['plugins','reload','xi-work-events','--json']); }
  catch(error) {
    const allowed=new Set(['openclaw_plugin_reload_timeout','openclaw_plugin_reload_failed','openclaw_plugin_reload_interrupted']);
    const reason=allowed.has(error?.xiCode)?error.xiCode:'openclaw_plugin_reload_failed';
    if(reason==='openclaw_plugin_reload_interrupted') fail(reason);
    return {state:'configured_activation_unconfirmed',activation_failure:reason,
      invitation_id:invitation.invitation_id,providers:prepared.providers};
  }
  if(upgradeFromDir!==undefined) {
    const confirmed=await inspectPlugin(native);
    if(installedDirectory(confirmed)!==pluginDir || inspectedDirectory(confirmed.plugin.rootDir)!==pluginDir)
      fail('plugin_upgrade_unconfirmed');
    // --runtime imports the package in this CLI process. It is a local source
    // check, not independent evidence of what the running Gateway has loaded.
    const runtime=jsonOutput(await native(['plugins','inspect','xi-work-events','--json','--runtime']));
    if(runtime.plugin?.id!=='xi-work-events' || runtime.plugin.status!=='loaded' ||
       inspectedDirectory(runtime.plugin.rootDir)!==pluginDir) fail('plugin_upgrade_unconfirmed');
    replacePrivateFile(statePath,previous,nextState,16384);
  }
  return {state:'activated_waiting_for_events',invitation_id:invitation.invitation_id,providers:prepared.providers};
}

async function main() {
  const args=process.argv.slice(2), options={};
  const names={'--invitation-file':'invitationFile','--state-dir':'stateDir','--openclaw':'openclaw','--agent':'agentId',
    '--plugin-dir':'pluginDir','--upgrade-from':'upgradeFromDir'};
  for(let i=0;i<args.length;i+=2) {
    const name=names[args[i]];
    if(!name || !args[i+1] || options[name]!==undefined) fail('usage: install.mjs --invitation-file PATH --state-dir PATH [--openclaw PATH] [--agent ID] [--plugin-dir PATH --upgrade-from PATH]');
    options[name]=args[i+1];
  }
  if(!options.invitationFile || !options.stateDir || (options.pluginDir && !options.upgradeFromDir))
    fail('usage: install.mjs --invitation-file PATH --state-dir PATH [--openclaw PATH] [--agent ID] [--plugin-dir PATH --upgrade-from PATH]');
  const result=await installManagedOpenClaw({...options,native:nativeRunner(options.openclaw)});
  process.stdout.write(JSON.stringify(result)+'\n');
}
if(process.argv[1] && import.meta.url===pathToFileURL(resolve(process.argv[1])).href) {
  main().catch(error=>{
    const publicFailures = {
      managed_secret_unavailable:'private_file_requires_canonical_path_owner_and_0600',
      work_event_config_unavailable:'XI 配置暂不可用；保留同一连接文件与专用目录，管理员确认接收服务后再运行同一命令。',
      invalid_work_event_config:'XI 配置合同未通过校验；安装尚未启用，请管理员核对版本兼容。',
    };
    process.stderr.write(`XI installation: ${error.xiCode ?? publicFailures[error.message] ?? 'installation_failed'}\n`);
    process.exitCode=1;
  });
}
