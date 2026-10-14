import { isAbsolute, normalize } from 'node:path';

const providerID = /^[a-z0-9][a-z0-9._-]{0,127}$/;
const agentID = /^[a-z0-9][a-z0-9_-]{0,127}$/;
export function normalizedGatewayURL(value, syntheticLoopback = false) {
  try {
    const url = new URL(value);
    const loopback = ['127.0.0.1','localhost','[::1]'].includes(url.hostname);
    if ((url.protocol !== 'https:' && !(syntheticLoopback && loopback && url.protocol === 'http:')) ||
      url.username || url.password || url.search || url.hash) throw new Error();
    return `${url.origin}${url.pathname.replace(/\/+$/,'')}`;
  } catch { throw new Error('managed_gateway_url_invalid'); }
}

// Inspect configuration metadata only. The catalog, selected model references,
// authorization headers and SecretRef values remain in OpenClaw's own config.
export function managedProviderIDs(config, managed, syntheticLoopback = false) {
  if (!managed || typeof managed !== 'object') throw new Error('managed_provider_config_invalid');
  const expected = normalizedGatewayURL(managed.baseUrl,syntheticLoopback);
  const excluded = managed.excludedProviderIds ?? [];
  if (!Array.isArray(excluded) || excluded.length > 100 || excluded.some(id => !providerID.test(id))) throw new Error('managed_provider_config_invalid');
  const selected = [];
  for (const [id,provider] of Object.entries(config?.models?.providers ?? {})) {
    if (!providerID.test(id) || id === 'xi-ur-observed' || excluded.includes(id) || !provider ||
      !['openai-completions','openai-responses'].includes(provider.api)) continue;
    let endpoint;
    try { endpoint = normalizedGatewayURL(provider.baseUrl,syntheticLoopback); } catch { continue; }
    if (endpoint === expected) selected.push(id);
  }
  if (selected.length > 100) throw new Error('managed_provider_config_invalid');
  return selected.sort();
}

export function isManagedModel(ctx, model, managed, syntheticLoopback = false) {
  try {
    return managedProviderIDs(ctx.config,managed,syntheticLoopback).includes(ctx.provider) &&
      normalizedGatewayURL(model?.baseUrl ?? ctx.model?.baseUrl,syntheticLoopback) === normalizedGatewayURL(managed.baseUrl,syntheticLoopback);
  } catch { return false; }
}

export function prepareManagedPluginConfig(original, settings) {
  if (!original || typeof original !== 'object' || Array.isArray(original)) throw new Error('managed_host_config_invalid');
  if (settings.selectedAgentId !== undefined && (typeof settings.selectedAgentId !== 'string' ||
      !agentID.test(settings.selectedAgentId))) throw new Error('managed_agent_selection_invalid');
  const origin = normalizedGatewayURL(settings.origin);
  if (new URL(origin).pathname !== '/') throw new Error('managed_xi_origin_invalid');
  const managedProvider = {baseUrl:normalizedGatewayURL(settings.urBaseUrl), excludedProviderIds:settings.excludedProviderIds ?? []};
  const providers = managedProviderIDs(original,managedProvider);
  if (providers.length === 0) throw new Error('managed_ur_provider_unresolved');
  for (const name of ['tokenFile','bodyKeyFile','outboxPath','bodyOutboxPath']) {
    const value=settings[name];
    if (typeof value !== 'string' || !isAbsolute(value) || normalize(value) !== value) throw new Error('managed_host_path_invalid');
  }
  if(settings.installationId !== undefined && !/^xiei_[a-f0-9]{32}$/.test(settings.installationId)) throw new Error('managed_installation_invalid');
  const desired={origin,...(settings.installationId ? {installationId:settings.installationId} : {}),
    ...(settings.selectedAgentId === undefined ? {} : {selectedAgentId:settings.selectedAgentId}),
    tokenFile:settings.tokenFile,bodyKeyFile:settings.bodyKeyFile,
    outboxPath:settings.outboxPath,bodyOutboxPath:settings.bodyOutboxPath,managedProvider};
  const current=original.plugins?.entries?.['xi-work-events'];
  if (current?.config) {
    if ((current.config.selectedAgentId ?? null) !== (settings.selectedAgentId ?? null)) throw new Error('managed_existing_installation_conflict');
    for (const key of ['origin','installationId','tokenFile','bodyKeyFile','outboxPath','bodyOutboxPath']) {
      if (current.config[key] !== undefined && current.config[key] !== desired[key]) throw new Error('managed_existing_installation_conflict');
    }
    if (current.config.provider?.enabled || (current.config.managedProvider &&
      normalizedGatewayURL(current.config.managedProvider.baseUrl) !== managedProvider.baseUrl)) throw new Error('managed_existing_provider_conflict');
  }
  const config=structuredClone(original);
  config.plugins ??= {};
  config.plugins.entries ??= {};
  config.plugins.entries['xi-work-events']={...config.plugins.entries['xi-work-events'],enabled:true,
    hooks:{...config.plugins.entries['xi-work-events']?.hooks,allowConversationAccess:true},
    config:{...config.plugins.entries['xi-work-events']?.config,...desired}};
  // An absent allowlist stays absent; creating an XI-only list would disable
  // previously available unrelated plugins.
  if (config.plugins.allow !== undefined) {
    if (!Array.isArray(config.plugins.allow)) throw new Error('managed_host_config_invalid');
    config.plugins.allow=[...new Set([...config.plugins.allow,'xi-work-events'])];
  }
  return {config,providers};
}


export function configuredAgentProjects(config, selectedAgentId) {
  if (selectedAgentId !== undefined && (typeof selectedAgentId !== 'string' || !agentID.test(selectedAgentId))) throw new Error('future_agent_roster_invalid');
  const entries=config?.agents?.entries;
  // OpenClaw 2026.9.5 has an implicit main agent when no roster is authored.
  const agents=entries===undefined || (entries && typeof entries==='object' && !Array.isArray(entries) && Object.keys(entries).length===0)
    ? [['main',{}]] : Object.entries(entries ?? {});
  if(!entries && entries!==undefined || Array.isArray(entries) || agents.length<1 || agents.length>32) throw new Error('future_agent_roster_invalid');
  const projects=agents.map(([id,agent])=>{
    if(!agentID.test(id) || !agent || typeof agent!=='object' || Array.isArray(agent)) throw new Error('future_agent_roster_invalid');
    const label=agent.name ?? id;
    if(typeof label!=='string' || label.length<1 || label.length>128 || label.trim()!==label || /[\x00-\x1f\x7f]/.test(label)) throw new Error('future_agent_roster_invalid');
    return {project_key:`agent:${id}`,label};
  }).sort((a,b)=>a.project_key.localeCompare(b.project_key));
  if (selectedAgentId === undefined) return projects;
  const selected=projects.filter(project=>project.project_key===`agent:${selectedAgentId}`);
  if(selected.length!==1) throw new Error('future_agent_roster_invalid');
  return selected;
}
