import { createHash } from 'node:crypto';

const maxResponseBytes = 4096;
const policyVersion = /^xi:(?:default:[1-9][0-9]{0,18}|xipe_[a-f0-9]{32}:[1-9][0-9]{0,18}:default:[1-9][0-9]{0,18})$/;
const bodyKinds = new Set(['user_input', 'user_output', 'model_input', 'model_output', 'tool_input', 'tool_output']);

export function effectiveBodyRule(capture, kind) {
  return capture?.by_kind?.[kind] ?? capture;
}

function validBodyRule(rule) {
  return rule && (rule.mode === 'disabled' || (rule.mode === 'full_content' &&
    policyVersion.test(rule.policy_version) && Number.isSafeInteger(rule.max_item_bytes) &&
    rule.max_item_bytes >= 0 && rule.max_item_bytes <= 33554432 &&
    Number.isSafeInteger(rule.retention_seconds) && rule.retention_seconds >= 1));
}

function validBodyCapture(capture) {
  return validBodyRule(capture) && (capture.by_kind === undefined ||
    (capture.by_kind && typeof capture.by_kind === 'object' && !Array.isArray(capture.by_kind) &&
      Object.entries(capture.by_kind).every(([kind, rule]) => bodyKinds.has(kind) && validBodyRule(rule))));
}

async function boundedJSON(response, maxBytes = maxResponseBytes) {
  if (!response.body) throw new Error('empty response');
  const reader = response.body.getReader();
  const chunks = [];
  let length = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      length += value.byteLength;
      if (length > maxBytes) throw new Error('oversize response');
      chunks.push(value);
    }
  } catch (error) {
    await reader.cancel().catch(() => {});
    throw error;
  } finally {
    reader.releaseLock();
  }
  return JSON.parse(Buffer.concat(chunks, length).toString('utf8'));
}

export function authorizedEndpoint(origin, token, path) {
  const endpoint = new URL(path, origin);
  if (endpoint.protocol !== 'https:' || endpoint.username || endpoint.password ||
      !/^xib_[A-Za-z0-9_-]{43}$/.test(token)) throw new TypeError('invalid work-event transport configuration');
  return endpoint;
}

export async function deliverNextReviewBody(outbox, { origin, token, bindings, metadataOutbox, fetchImpl = fetch, timeoutMs = 30000 }) {
  const endpoint = authorizedEndpoint(origin, token, '/api/v1/work-event-ingest/contents');
  if (!Array.isArray(bindings) || typeof metadataOutbox?.observation !== 'function' ||
      !Number.isSafeInteger(timeoutMs) || timeoutMs < 1000 || timeoutMs > 60000) {
    throw new TypeError('invalid review-body delivery configuration');
  }
  const item = outbox.next();
  if (!item) return { state: 'idle' };
  if (item.expired) return { state: 'blocked', code: 'body_delivery_expired' };
  if (item.blocked) return { state: 'blocked', code: item.reason };
  const metadata = metadataOutbox.observation(item.observation_key);
  if (!metadata || metadata.documentRef !== item.event_ref || metadata.documentHash !== item.event_hash || metadata.state === 'blocked') {
    outbox.block(item.content_ref, 'body_metadata_unavailable');
    return { state: 'blocked', code: 'body_metadata_unavailable' };
  }
  if (metadata.state !== 'acked') {
    outbox.retry(item.content_ref, 'body_metadata_pending');
    return { state: 'retry', code: 'body_metadata_pending' };
  }
  const binding = bindings.find((value) => value.collection_id === item.collection_id &&
    value.collection_revision === item.expected_collection_revision &&
    value.scope_generation === item.scope_generation);
  const policy = effectiveBodyRule(binding?.body_capture, item.content_kind);
  if (policy?.mode !== 'full_content' || policy.policy_version !== item.policy_version) {
    outbox.block(item.content_ref, 'body_policy_changed');
    return { state: 'blocked', code: 'body_policy_changed' };
  }
  const requestMetadata = {
    collection_id: item.collection_id, expected_collection_revision: item.expected_collection_revision,
    scope_generation: item.scope_generation, event_ref: item.event_ref, event_revision: item.event_revision,
    event_hash: item.event_hash, content_kind: item.content_kind, content_ref: item.content_ref,
    byte_domain: item.byte_domain, sha256: item.sha256, plain_bytes: item.body.length,
  };
  let response;
  try {
    response = await fetchImpl(endpoint, { method: 'POST', redirect: 'error', cache: 'no-store',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/octet-stream',
        'X-XI-Body-Metadata': Buffer.from(JSON.stringify(requestMetadata)).toString('base64url') },
      body: item.body, signal: AbortSignal.timeout(timeoutMs) });
  } catch {
    outbox.retry(item.content_ref, 'body_transport_unavailable');
    return { state: 'retry', code: 'body_transport_unavailable' };
  }
  if (response.status === 200 || response.status === 201) {
    try {
      const receipt = await boundedJSON(response);
      if (receipt?.content_ref !== item.content_ref || receipt?.sha256 !== item.sha256 ||
          receipt?.plain_bytes !== item.body.length ||
          receipt?.outcome !== (response.status === 201 ? 'accepted' : 'identical_replay') ||
          !Number.isFinite(Date.parse(receipt?.expires_at))) throw new Error('invalid review-body receipt');
      outbox.acknowledge(item.content_ref);
      return { state: 'acknowledged', outcome: receipt.outcome };
    } catch {
      outbox.retry(item.content_ref, 'body_invalid_ack');
      return { state: 'retry', code: 'body_invalid_ack' };
    }
  }
  if (response.status === 429 || response.status >= 500 && response.status !== 507) {
    outbox.retry(item.content_ref, 'body_receiver_unavailable');
    return { state: 'retry', code: 'body_receiver_unavailable' };
  }
  const code = response.status === 413 ? 'body_too_large' :
    response.status === 403 ? 'body_policy_changed' : response.status === 401 ? 'body_credential_invalid' :
    response.status === 507 ? 'body_quota_exhausted' : response.status === 409 ? 'body_conflict' : 'body_invalid_item';
  outbox.block(item.content_ref, code);
  return { state: 'blocked', code };
}

function validScope(value) {
  return value && typeof value === 'object' && !Array.isArray(value) &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value.source_system_id) &&
    typeof value.source_instance === 'string' && value.source_instance.length > 0 &&
    /^[1-9][0-9]{0,18}$/.test(value.org_id) && /^[1-9][0-9]{0,18}$/.test(value.project_id);
}

export async function fetchWorkEventConfiguration({ origin, token, fetchImpl = fetch, timeoutMs = 5000 }) {
  const endpoint = authorizedEndpoint(origin, token, '/api/v1/work-event-ingest/config');
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 100 || timeoutMs > 30000) {
    throw new TypeError('invalid work-event transport timeout');
  }
  const response = await fetchImpl(endpoint, { method: 'GET', redirect: 'error', cache: 'no-store',
    headers: { Authorization: `Bearer ${token}` }, signal: AbortSignal.timeout(timeoutMs) });
  if (response.status !== 200) throw new Error('work_event_config_unavailable');
  const config = await boundedJSON(response, 65536);
  if (config?.version !== 'xinsight.work-event-ingest-config.v1' || !Array.isArray(config.items) ||
      config.items.length > 100 || new Set(config.items.map((item) => item.producer_project_key)).size !== config.items.length ||
      !config.items.every((item) => typeof item.producer_project_key === 'string' &&
        (/^workspace:\/[^\u0000-\u001f]{1,245}$/.test(item.producer_project_key) ||
         /^agent:[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(item.producer_project_key)) &&
        /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(item.collection_id) &&
        Number.isSafeInteger(item.collection_revision) && item.collection_revision > 0 &&
        /^[1-9][0-9]{0,18}$/.test(item.scope_generation) &&
        validScope(item.reporter) &&
        (item.body_capture === undefined || validBodyCapture(item.body_capture)) &&
        (item.ur_target === undefined || validScope(item.ur_target?.scope)))) {
    throw new Error('invalid_work_event_config');
  }
  if (config.managed_renewal === true && (!/^xiei_[a-f0-9]{32}$/.test(config.installation_id) ||
      !Number.isFinite(Date.parse(config.credential_expires_at)))) throw new Error('invalid_work_event_config');
  return {version:config.version,items:config.items,managed_renewal:config.managed_renewal === true,
    installation_id:config.installation_id,credential_expires_at:config.credential_expires_at};
}

export async function fetchWorkEventConfig(options) {
  return (await fetchWorkEventConfiguration(options)).items;
}

function exactReceipt(receipt, item, status) {
  const document = JSON.parse(item.document);
  const ref = item.kind === 'event' ? document.event_ref : document.relation_ref;
  const expectedOutcome = status === 201 ? 'accepted' : 'identical_replay';
  const expectedHash = 'sha256:' + createHash('sha256').update(item.document).digest('hex');
  const reporter = document.reporter;
  return receipt?.kind === item.kind && receipt?.document_ref === ref &&
    receipt?.revision === (item.kind === 'event' ? document.revision : undefined) &&
    receipt?.sha256 === expectedHash && receipt?.outcome === expectedOutcome &&
    receipt?.reporter?.source_system_id === reporter?.source_system_id &&
    receipt?.reporter?.source_instance === reporter?.source_instance &&
    receipt?.reporter?.org_id === reporter?.org_id &&
    receipt?.reporter?.project_id === reporter?.project_id;
}

export async function deliverNext(outbox, { origin, token, fetchImpl = fetch, timeoutMs = 5000 }) {
  const endpoint = authorizedEndpoint(origin, token, '/api/v1/work-event-ingest/items');
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 100 || timeoutMs > 30000) {
    throw new TypeError('invalid work-event transport configuration');
  }
  const item = outbox.next();
  if (!item) return { state: 'idle' };
  // Preserve the already-canonical document bytes inside a bounded envelope.
  const body = `{"collection_id":${JSON.stringify(item.collectionId)},` +
    `"expected_collection_revision":${item.expectedCollectionRevision},` +
    `"scope_generation":${JSON.stringify(item.scopeGeneration)},` +
    `"kind":${JSON.stringify(item.kind)},"document":${item.document}}`;
  let response;
  try {
    response = await fetchImpl(endpoint, {
      method: 'POST', redirect: 'error', cache: 'no-store',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
      body, signal: AbortSignal.timeout(timeoutMs),
    });
  } catch {
    outbox.retry(item.id, 'transport_unavailable');
    return { state: 'retry', code: 'transport_unavailable' };
  }
  if (response.status === 200 || response.status === 201) {
    try {
      const receipt = await boundedJSON(response);
      if (!exactReceipt(receipt, item, response.status)) throw new Error('mismatched acknowledgement');
      outbox.acknowledge(item.id);
      return { state: 'acknowledged', outcome: receipt.outcome };
    } catch {
      outbox.retry(item.id, 'invalid_ack');
      return { state: 'retry', code: 'invalid_ack' };
    }
  }
  let code = '';
  try { code = (await boundedJSON(response))?.code ?? ''; } catch { /* status still decides */ }
  if (response.status === 409 && code === 'work_event_dependency_pending') {
    outbox.retry(item.id, 'dependency_pending');
    return { state: 'retry', code: 'dependency_pending' };
  }
  if (response.status === 429 || response.status >= 500) {
    outbox.retry(item.id, 'receiver_unavailable');
    return { state: 'retry', code: 'receiver_unavailable', status: response.status };
  }
  const terminal = response.status === 409 ? 'conflict' : response.status === 403 ? 'scope_changed' :
    response.status === 401 ? 'credential_invalid' : 'invalid_item';
  outbox.block(item.id, terminal);
  return { state: 'blocked', code: terminal };
}


export async function reportWorkEventProjects({origin,token,installationId,projects,fetchImpl=fetch}) {
  if(!/^xiei_[a-f0-9]{32}$/.test(installationId) || !Array.isArray(projects) || projects.length<1 || projects.length>32) throw new Error('future_agent_roster_invalid');
  const response=await fetchImpl(authorizedEndpoint(origin,token,'/api/v1/work-event-install/future-candidates'),{
    method:'POST',redirect:'error',credentials:'omit',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},
    body:JSON.stringify({projects}),signal:AbortSignal.timeout(15000)});
  if(response.status!==200) throw new Error('future_candidates_unconfirmed');
  const result=await boundedJSON(response,65536);
  if(result?.schema_version!=='xinsight.local-work-install-response.v1' || result.action!=='future_candidates') throw new Error('future_candidates_unconfirmed');
  if(result.state==='conflict' || result.state==='scope_changed') throw new Error(`future_candidates_${result.state}`);
  if(result.state!=='approved' || result.invitation?.state!=='approved' || result.invitation.invitation_id!==installationId) throw new Error('future_candidates_unconfirmed');
}


export async function reportWorkInstallStatus({origin,token,installationId,status,fetchImpl=fetch}) {
  if (!/^xiei_[a-f0-9]{32}$/.test(installationId)) throw new TypeError('invalid installation identity');
  const endpoint=authorizedEndpoint(origin,token,'/api/v1/work-event-install/status');
  const response=await fetchImpl(endpoint,{method:'POST',redirect:'error',cache:'no-store',credentials:'omit',
    headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},
    body:JSON.stringify({status}),signal:AbortSignal.timeout(5000)});
  if(response.status!==200) throw new Error('installation_status_unconfirmed');
  const receipt=await boundedJSON(response);
  if(receipt.schema_version!=='xinsight.local-work-install-response.v1'||receipt.action!=='status'||
    receipt.state!=='reported'||receipt.invitation_id!==installationId) throw new Error('installation_status_unconfirmed');
}
