import { installStatusSnapshot } from './install-status.mjs';
import { fetchManagedConfiguration } from './credential-renewal.mjs';
import { managedProviderIDs, isManagedModel, configuredAgentProjects } from './managed-config.mjs';
import { ingestToken, bodyQueueKey } from './managed-secret-files.mjs';
import { WorkEventOutbox } from './outbox.mjs';
import { ReviewBodyOutbox } from './body-outbox.mjs';
import { registerOpenClawWorkEventHooks } from './producer.mjs';
import { deliverNext, deliverNextReviewBody, fetchWorkEventConfig, reportWorkEventProjects, reportWorkInstallStatus } from './transport.mjs';

const providerId = 'xi-ur-observed';

function configuredModels(value) {
  if (!Array.isArray(value) || value.length === 0 || value.length > 100) return null;
  const seen = new Set();
  for (const model of value) {
    if (!model || typeof model !== 'object' || Array.isArray(model) ||
        typeof model.id !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(model.id) || seen.has(model.id) ||
        typeof model.name !== 'string' || !model.name.trim() || typeof model.reasoning !== 'boolean' ||
        !Array.isArray(model.input) || model.input.length === 0 ||
        !model.input.every(item => item === 'text' || item === 'image') ||
        !model.cost || typeof model.cost !== 'object' ||
        !['input', 'output', 'cacheRead', 'cacheWrite'].every(key =>
          typeof model.cost[key] === 'number' && Number.isFinite(model.cost[key]) && model.cost[key] >= 0) ||
        !Number.isSafeInteger(model.contextWindow) || model.contextWindow < 8192 || model.contextWindow > 10000000 ||
        !Number.isSafeInteger(model.maxTokens) || model.maxTokens < 1 || model.maxTokens > model.contextWindow) return null;
    seen.add(model.id);
  }
  return value;
}

function headerValue(headers, name) {
  if (!headers || typeof headers !== 'object') return '';
  for (const [key, value] of Object.entries(headers)) {
    if (key.toLowerCase() === name && typeof value === 'string') return value;
  }
  return '';
}

export default {
  id: 'xi-work-events',
  name: 'X-Insight Work Events',
  register(api) {
    const config = api.pluginConfig ?? {};
    let outbox = null;
    let bodyOutbox = null;
    let stopped = true;
    let timer = null;
    let inFlight = null;
    let wakeRequested = false;
    let nextConfigAt = 0;
    let nextStatusAt = 0;
    let lastReportedProjects = null;
    const lastLog = new Map();
    const logGap = (code, persist = true) => {
      const now = Date.now();
      if ((lastLog.get(code) ?? 0) + 60000 <= now) {
        lastLog.set(code, now);
        if (persist) { try { outbox?.recordGap(code); } catch { /* diagnostics cannot stop observation */ } }
        api.logger?.warn?.(`XI work-event observation gap: ${code}`);
      }
    };
    try { outbox = new WorkEventOutbox(config.outboxPath); }
    catch { logGap('outbox_unavailable'); }
    let bodyKey;
    try { bodyKey = bodyQueueKey(config); } catch { /* missing custody credentials are a visible gap below */ }
    const bodyConfigured = typeof config.bodyOutboxPath === 'string' && bodyKey !== undefined;
    if (bodyConfigured) {
      try { bodyOutbox = new ReviewBodyOutbox(config.bodyOutboxPath, bodyKey); }
      catch { logGap('body_custody_unavailable'); }
    } else logGap('body_custody_unconfigured');
    const queue = {
      enqueue: (item) => {
        if (!outbox) throw new Error('outbox_unavailable');
        const result = outbox.enqueue(item);
        wakeRequested = true;
        if (!inFlight && timer) {
          clearTimeout(timer);
          timer = setTimeout(() => { void tick(); }, 0);
        }
        return result;
      },
      observation: (key) => outbox?.observation(key) ?? null,
      recordModelStart: (value) => outbox?.recordModelStart(value),
      recordPendingModelStart: (value) => outbox?.recordPendingModelStart(value),
      recordModelContext: (value) => outbox?.recordModelContext(value),
      pendingModel: (callId) => outbox?.pendingModel(callId) ?? null,
      modelContext: (callId) => outbox?.modelContext(callId) ?? null,
      recordHeader: (value) => outbox?.recordHeader(value),
      correlation: (callId) => outbox?.correlation(callId) ?? null,
      recordGap: (code) => { if (outbox) outbox.recordGap(code); },
    };
    const producer = registerOpenClawWorkEventHooks(api, { outbox: queue,
      bodyOutbox: bodyConfigured ? { enqueue: (item) => {
        if (!bodyOutbox) throw new Error('body_queue_unavailable');
        const result = bodyOutbox.enqueue(item);
        wakeRequested = true;
        if (!inFlight && timer) { clearTimeout(timer); timer = setTimeout(() => { void tick(); }, 0); }
        return result;
      } } : null,
      getBindings: () => outbox?.bindings() ?? [], onGap: code => logGap(code, false) });

    // Provider-owned wrappers are the supported place to observe the raw
    // response header. Generic model hooks do not contain that value.
    let observedProviderIDs = [];
    if (config.managedProvider) {
      try { observedProviderIDs = managedProviderIDs(api.config, config.managedProvider, config.syntheticLoopback === true); }
      catch { logGap('managed_provider_config_invalid'); }
      if (observedProviderIDs.length === 0) logGap('managed_ur_provider_unresolved');
    }
    if (config.provider?.enabled === true && config.managedProvider) logGap('managed_existing_provider_conflict');
    else if (config.provider?.enabled === true || observedProviderIDs.length > 0) {
      api.registerProvider({
        id: providerId, label: 'XI-observed UniRouter', auth: [],
        // Native hook aliases retain original provider/model/auth identities.
        // No catalog runs in managed mode and no credential is copied here.
        ...(config.managedProvider ? {hookAliases: observedProviderIDs} : {}),
        catalog: config.managedProvider ? undefined : { order: 'simple', async run() {
          const baseUrl = config.provider.baseUrl;
          const models = configuredModels(config.provider.models);
          const url = typeof baseUrl === 'string' ? new URL(baseUrl) : null;
          const loopback = url && ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname);
          if (!url || (url.protocol !== 'https:' && !(config.syntheticLoopback === true &&
              url.protocol === 'http:' && loopback)) || !models) {
            throw new Error('invalid XI observed provider configuration');
          }
          return { provider: { baseUrl, apiKey: process.env.XINSIGHT_UR_OBSERVED_API_KEY ?? '',
            api: 'openai-completions', models } };
        } },
        wrapStreamFn(ctx) {
          if (!ctx.streamFn) return undefined;
          if (config.managedProvider && !isManagedModel(ctx, ctx.model, config.managedProvider, config.syntheticLoopback === true)) {
            logGap('managed_provider_endpoint_changed');
            return undefined;
          }
          return async (model, context, options) => {
            producer.observeProviderCallStart({ requestId: options?.requestId,
              workspaceDir: ctx.workspaceDir, agentId: ctx.agentId });
            return ctx.streamFn(model, context, {
            ...options,
            onResponse: async (response, responseModel) => {
              producer.observeTransportResponse({
                requestId: options?.requestId,
                urRequestId: headerValue(response?.headers, 'x-request-id'),
                status: response?.status,
                workspaceDir: ctx.workspaceDir,
                agentId: ctx.agentId,
              });
              await options?.onResponse?.(response, responseModel);
            },
            });
          };
        },
      });
    }

    async function tick() {
      if (stopped || !outbox) return;
      inFlight = (async () => {
        try {
          let token = ingestToken(config);
          if (bodyOutbox) {
            for (let i = 0; i < 16; i++) {
              const blocked = bodyOutbox.nextBlocked();
              if (!blocked) break;
              // Keep the bounded blocked row until the durable metadata gap
              // has committed. A crash can duplicate a count, not lose it.
              outbox.recordGap(blocked.reason);
              bodyOutbox.releaseBlocked(blocked.contentRef);
            }
          }
          if (Date.now() >= nextConfigAt) {
            const refreshed = await fetchManagedConfiguration({origin:config.origin,token,
              tokenFile:config.tokenFile,installationId:config.installationId});
            token = refreshed.token;
            if (refreshed.gap) logGap(refreshed.gap);
            // Hook workers stop capturing bodies 60 seconds after the last
            // confirmed snapshot. Refresh with room for this serial tick's
            // network work, and publish the official map before reporting
            // future candidates or delivering queued items.
            const applyBindings = (items) => {
              outbox.replaceBindings(items);
              api.logger?.info?.(`XI work-event bindings loaded: ${items.length}`);
              nextConfigAt = Date.now() + 30000;
            };
            applyBindings(refreshed.configuration.items);
            if (config.installationId && refreshed.configuration.installation_id===config.installationId) {
              try {
                const projects=configuredAgentProjects(api.runtime?.config?.current?.() ?? api.config,config.selectedAgentId);
                const signature=JSON.stringify(projects);
                if(signature!==lastReportedProjects) {
                  await reportWorkEventProjects({origin:config.origin,token,installationId:config.installationId,projects});
                  // New facts use only the authoritative follow-up map. A
                  // candidate acknowledgement itself never supplies admission.
                  const discovered=await fetchWorkEventConfig({origin:config.origin,token});
                  applyBindings(discovered);
                  lastReportedProjects=signature;
                }
              } catch(error) {
                const known=new Set(['future_agent_roster_invalid','future_candidates_conflict','future_candidates_scope_changed']);
                logGap(known.has(error?.message)?error.message:'future_candidates_unconfirmed');
              }
            }
          }
          if (config.installationId && Date.now() >= nextStatusAt) {
            nextStatusAt = Date.now() + 60000;
            try {
              await reportWorkInstallStatus({origin:config.origin,token,installationId:config.installationId,
                status:installStatusSnapshot(outbox.status(),bodyOutbox?.pendingCount() ?? 0,outbox.gapStatus())});
            } catch { logGap('installation_status_unconfirmed'); }
          }
          const result = await deliverNext(outbox, { origin: config.origin, token });
          if (result.state === 'blocked' && ['credential_invalid', 'scope_changed'].includes(result.code)) {
            outbox.invalidateBodyPolicy(); nextConfigAt = 0;
          }
          if (result.state === 'retry' && result.status) logGap(`metadata_receiver_http_${result.status}`);
          if (bodyOutbox && bodyOutbox.nextDueAt() !== null && bodyOutbox.nextDueAt() <= Date.now()) {
            const candidate = bodyOutbox.next();
            if (candidate?.expired || candidate?.blocked) {
              const blocked = bodyOutbox.nextBlocked();
              if (blocked) { outbox.recordGap(blocked.reason); bodyOutbox.releaseBlocked(blocked.contentRef); }
              if (result.state === 'acknowledged' || result.state === 'blocked') return 0;
            } else if (candidate) {
              const predecessor = outbox.observation(candidate.observation_key);
              if (predecessor?.state !== 'acked') {
                if (predecessor?.state === 'blocked' || !predecessor) bodyOutbox.block(candidate.content_ref, 'body_metadata_unavailable');
                else bodyOutbox.retry(candidate.content_ref, 'body_metadata_pending');
              } else {
                const refreshed = await fetchWorkEventConfig({ origin: config.origin, token });
                outbox.replaceBindings(refreshed);
                const bodyResult = await deliverNextReviewBody(bodyOutbox, { origin: config.origin, token, bindings: refreshed, metadataOutbox: outbox });
                if (bodyResult.state === 'blocked') {
                  if (['body_policy_changed', 'body_credential_invalid'].includes(bodyResult.code)) {
                    outbox.invalidateBodyPolicy(); nextConfigAt = 0;
                  }
                  const blocked = bodyOutbox.nextBlocked();
                  if (blocked) { outbox.recordGap(blocked.reason); bodyOutbox.releaseBlocked(blocked.contentRef); }
                  logGap(bodyResult.code, false);
                }
                if (bodyResult.state === 'acknowledged' || bodyResult.state === 'blocked') return 0;
              }
            }
          }
          if (result.state === 'acknowledged' || result.state === 'blocked') return 0;
          const due = outbox.nextDueAt();
          const bodyDue = bodyOutbox?.nextDueAt() ?? null;
          const next = [due, bodyDue].filter((value) => value !== null);
          return next.length === 0 ? 5000 : Math.min(5000, Math.max(250, Math.min(...next) - Date.now()));
        } catch (error) {
          nextConfigAt = 0;
          try { outbox.invalidateBodyPolicy(); } catch { /* freshness still expires independently */ }
          try { outbox.recordGap('receiver_unavailable'); } catch { /* storage may also be unavailable */ }
          const known = new Set(['work_event_config_unavailable', 'invalid_work_event_config', 'body_queue_integrity_failed', 'managed_secret_unavailable', 'managed_ingest_credential_invalid']);
          logGap(known.has(error?.message) ? error.message : 'receiver_unavailable', false);
          return 5000;
        }
      })();
      const delay = await inFlight;
      inFlight = null;
      if (!stopped) {
        timer = setTimeout(() => { void tick(); }, wakeRequested ? 0 : delay);
        wakeRequested = false;
      }
    }

    api.registerService({
      id: 'xi-work-event-delivery',
      start() {
        api.logger?.info?.('XI work-event delivery service starting');
        stopped = false;
        if (!outbox) {
          try { outbox = new WorkEventOutbox(config.outboxPath); }
          catch { logGap('outbox_unavailable'); return; }
        }
        if (bodyConfigured && !bodyOutbox) {
          try { bodyOutbox = new ReviewBodyOutbox(config.bodyOutboxPath, bodyKey); }
          catch { logGap('body_custody_unavailable'); }
        }
        outbox.invalidateBodyPolicy();
        nextConfigAt = 0;
        nextStatusAt = 0;
        void tick();
      },
      async stop() {
        stopped = true;
        if (timer) clearTimeout(timer);
        if (inFlight) await inFlight;
        outbox?.close();
        outbox = null;
        bodyOutbox?.close();
        bodyOutbox = null;
      },
    });
  },
};
