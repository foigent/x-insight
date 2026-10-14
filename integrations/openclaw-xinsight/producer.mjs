import { createHash, randomUUID } from 'node:crypto';
import pathModule from 'node:path';
import { canonicalWorkEventJSON } from './outbox.mjs';
import { effectiveBodyRule } from './transport.mjs';

const safeRef = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
const urRequestPattern = /^op_[a-f0-9]{32}$/;

function ref(prefix, ...parts) {
  return `${prefix}:${createHash('sha256').update(parts.join('\0')).digest('hex')}`;
}

// Hook bodies are JSON source views, not the integer-only event envelope.
// Optional undefined object fields are absent; finite decimal values retain
// their JSON representation. Unsupported array values must remain a gap.
function canonicalHookBody(value) {
  if (value === null || ['string','boolean'].includes(typeof value)) return JSON.stringify(value);
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) throw new TypeError('nonfinite body number');
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return `[${Array.from(value,canonicalHookBody).join(',')}]`;
  if (value && typeof value === 'object' && [Object.prototype,null].includes(Object.getPrototypeOf(value))) {
    return `{${Object.keys(value).filter(key=>value[key]!==undefined).sort()
      .map(key=>`${JSON.stringify(key)}:${canonicalHookBody(value[key])}`).join(',')}}`;
  }
  throw new TypeError('unsupported body value');
}

// OpenClaw 9.5 exposes runtime tool objects here, including execute().
// Retain the public declaration view; runtime functions are neither request
// body data nor model-visible code. Malformed declarations remain a gap.
function modelToolDeclarations(tools) {
  if (!Array.isArray(tools)) return Symbol('invalid tool declarations');
  return tools.map(tool => {
    if (!tool || typeof tool !== 'object' || typeof tool.name !== 'string' ||
        !tool.parameters || typeof tool.parameters !== 'object' || Array.isArray(tool.parameters) ||
        (tool.description !== undefined && typeof tool.description !== 'string')) return undefined;
    return {name:tool.name, ...(tool.description !== undefined ? {description:tool.description} : {}), parameters:tool.parameters};
  });
}

function matchingBinding(bindings, context) {
  const keys = [];
  if (typeof context?.workspaceDir === 'string') keys.push(`workspace:${context.workspaceDir}`);
  if (typeof context?.agentId === 'string') keys.push(`agent:${context.agentId}`);
  const found = bindings.filter((item) => keys.includes(item.producer_project_key));
  return found.length === 1 ? found[0] : null;
}

function agentEndTaskOutcome(event) {
  if (event?.success === false) return { lifecycle: 'failed', action: 'task.failed' };
  // OpenClaw 2026.9.5 can report success=true when the terminal assistant has
  // stopReason=error; its final run outcome checks that reason separately.
  const messages = event?.messages;
  if (!Array.isArray(messages)) return { lifecycle: 'unknown', action: 'task.ended_observed' };
  let lastUser = -1;
  for (let i = 0; i < messages.length; i++) if (messages[i]?.role === 'user') lastUser = i;
  if (lastUser < 0) return { lifecycle: 'unknown', action: 'task.ended_observed' };
  let terminalAssistant;
  for (let i = lastUser + 1; i < messages.length; i++) {
    if (messages[i]?.role === 'assistant') terminalAssistant = messages[i];
  }
  if (terminalAssistant?.stopReason === 'error' || terminalAssistant?.stopReason === 'aborted') {
    return { lifecycle: 'failed', action: 'task.failed' };
  }
  if (event?.success === true && terminalAssistant?.stopReason === 'stop') {
    return { lifecycle: 'completed', action: 'task.completed' };
  }
  return { lifecycle: 'unknown', action: 'task.ended_observed' };
}

// The hook API is observational. Every callback catches admission errors so
// an unavailable XI receiver or a full outbox cannot change agent execution.
export function registerOpenClawWorkEventHooks(api, { outbox, bindings = [], getBindings = () => bindings,
  bodyOutbox = null, onGap = () => {}, now = () => new Date() }) {
  if (!api?.on || !outbox?.enqueue || typeof getBindings !== 'function' || typeof now !== 'function') {
    throw new TypeError('invalid producer');
  }
  const runBindings = new Map();
  const pendingInbound = new Map();
  const pendingToolPaths = new Map();
  const maxRememberedRuns = 2048;
  const runMemoryMs = 60 * 60 * 1000;

  function gap(code) {
    try { outbox.recordGap(code); } catch { /* keep the source callback nonblocking */ }
    try { onGap(code); } catch { /* observer diagnostics cannot affect execution */ }
  }

  function prune(timeMs) {
    for (const [key, value] of runBindings) {
      if (value.lastSeen + runMemoryMs <= timeMs) runBindings.delete(key);
    }
    for (const [key, value] of pendingInbound) {
      if (value.createdMs + 5 * 60 * 1000 <= timeMs) { pendingInbound.delete(key); gap('message_project_unresolved'); }
    }
    for (const [key, value] of pendingToolPaths) {
      if (value.createdMs + 5 * 60 * 1000 <= timeMs) pendingToolPaths.delete(key);
    }
  }

  function relationFor(callId) {
    const correlation = outbox.correlation(callId);
    if (!correlation) return;
    const headerBinding = matchingBinding([correlation.binding], correlation);
    if ((correlation.workspaceDir || correlation.agentId) && !headerBinding) {
      gap('ur_header_project_mismatch');
      return;
    }
    if (!correlation.binding.ur_target?.scope) return;
    try {
      const recordedAt = new Date(now()).toISOString();
      const evidence = canonicalWorkEventJSON({ request_id: callId, status: correlation.status,
        ur_request_id: correlation.urId });
      const document = {
        schema_version: 'xinsight.work-event-relation.v1', reporter: correlation.binding.reporter,
        relation_ref: ref('ur', correlation.runId, callId, correlation.urId),
        event_ref: correlation.eventRef, event_revision: 1, event_hash: correlation.eventHash,
        ur_target: correlation.binding.ur_target.scope, ur_request_id: correlation.urId,
        model_call_id: callId, evidence_ref: ref('header', callId, correlation.urId),
        evidence_sha256: 'sha256:' + createHash('sha256').update(evidence).digest('hex'),
        recorded_at: recordedAt,
      };
      const observationKey = createHash('sha256').update(correlation.binding.collection_id).update('\0')
        .update(correlation.runId).update('\0').update(callId).update('\0').update(correlation.urId).digest('hex');
      outbox.enqueue({ collectionId: correlation.binding.collection_id,
        expectedCollectionRevision: correlation.binding.collection_revision,
        scopeGeneration: correlation.binding.scope_generation, kind: 'relation', observationKey,
        document: canonicalWorkEventJSON(document) });
    } catch (error) {
      gap(error?.message === 'outbox_capacity_exceeded' ? 'outbox_capacity_exceeded' : 'relation_enqueue_failed');
    }
  }

  function observe({ event, context, subjectKind, lifecycle, action, sourceId, bodyCapture, resource }) {
    try {
      const observedAt = new Date(now());
      const timeMs = observedAt.getTime();
      prune(timeMs);
      const runId = event?.runId ?? context?.runId;
      const validRun = typeof runId === 'string' && safeRef.test(runId);
      if ((!validRun && subjectKind !== 'message') ||
          typeof sourceId !== 'string' || !safeRef.test(sourceId)) {
        gap('missing_source_identity');
        return;
      }
      const currentBindings = getBindings();
      if (!Array.isArray(currentBindings)) throw new TypeError('invalid binding snapshot');
      const effectiveContext = subjectKind === 'model_call' && !context?.workspaceDir && !context?.agentId ?
        outbox.modelContext(sourceId) ?? context : context;
      const observedBinding = matchingBinding(currentBindings, effectiveContext);
      const prior = validRun ? runBindings.get(runId) : undefined;
      const priorBinding = prior?.binding;
      if (priorBinding && (context?.workspaceDir || context?.agentId) && !observedBinding) {
        gap('run_project_changed');
        return;
      }
      if (priorBinding && observedBinding && (observedBinding.collection_id !== priorBinding.collection_id ||
          observedBinding.collection_revision !== priorBinding.collection_revision ||
          observedBinding.scope_generation !== priorBinding.scope_generation ||
          canonicalWorkEventJSON(observedBinding.reporter) !== canonicalWorkEventJSON(priorBinding.reporter))) {
        gap('run_project_changed');
        return;
      }
      const binding = priorBinding ?? observedBinding;
      if (!binding) {
        if (subjectKind === 'model_call' && action === 'model.requested') {
          outbox.recordPendingModelStart({ callId: sourceId, runId });
          const pending = outbox.pendingModel(sourceId);
          if (pending && matchingBinding(currentBindings, pending)) {
            observe({ event, context: pending, subjectKind, lifecycle, action, sourceId });
          }
          return;
        }
        gap(currentBindings.length === 0 ? 'config_not_loaded' :
          !context?.workspaceDir && !context?.agentId ? 'project_context_missing' : 'unbound_project');
        return;
      }
      if (validRun && !prior && runBindings.size >= maxRememberedRuns) {
        gap('active_runs_full');
      } else if (validRun) {
        runBindings.set(runId, { binding, lastSeen: timeMs,
          workspaceDir: typeof context?.workspaceDir === 'string' ? context.workspaceDir : prior?.workspaceDir });
      }
      if (validRun && pendingInbound.has(runId)) {
        const inbound = pendingInbound.get(runId);
        pendingInbound.delete(runId);
        observe({ event: inbound.event, context: { ...context, runId }, subjectKind: 'message',
          lifecycle: 'unknown', action: 'message.received_observed', sourceId: inbound.sourceId,
          bodyCapture: { kind: 'user_input', domain: 'openclaw.message_received.utf8.v1', value: inbound.event.content } });
      }
      const recordedAt = observedAt.toISOString();
      // Source identity remains stable across the bounded local replay window.
      // Once the tombstone expires, a changed timestamp may conflict at XI;
      // never disguise the same observation as a new event to avoid that 409.
      const eventRef = ref('oc', validRun ? runId : ref('message', sourceId), subjectKind, sourceId, action);
      const observationKey = createHash('sha256').update(binding.collection_id).update('\0')
        .update(validRun ? runId : ref('message', sourceId)).update('\0').update(subjectKind).update('\0')
        .update(sourceId).update('\0').update(action).digest('hex');
      const document = {
        schema_version: 'xinsight.work-event.v1', reporter: binding.reporter,
        event_ref: eventRef, revision: 1,
        occurred_at: recordedAt, recorded_at: recordedAt, subject_kind: subjectKind,
        lifecycle_state: lifecycle,
        actor: { kind: 'service', ref: 'openclaw' }, action_kind: action,
        content_state: 'not_captured', content_refs: [],
      };
      if (validRun) document.run_id = runId;
      if (resource) document.resource = resource;
      else if (subjectKind === 'tool_call' && typeof event?.toolName === 'string' &&
        event.toolName.length > 0 && event.toolName.length <= 512) {
        document.resource = { ref: ref('tool', event.toolName), name_state: 'readable', name: event.toolName };
      }
      let bodyItem = null;
      if (bodyCapture) {
        // Run identity stays fixed, but retention is always taken from the
        // current authorized map, including hooks without fresh agent context.
        const currentBinding = currentBindings.find(item => item.collection_id === binding.collection_id &&
          item.collection_revision === binding.collection_revision && item.scope_generation === binding.scope_generation &&
          canonicalWorkEventJSON(item.reporter) === canonicalWorkEventJSON(binding.reporter));
        const policy = effectiveBodyRule(currentBinding?.body_capture, bodyCapture.kind);
        if (!bodyOutbox) gap('body_custody_unconfigured');
        else if (policy?.mode === 'disabled') { /* deliberate omission is not an observation failure */ }
        else if (policy?.mode !== 'full_content') gap('body_policy_unconfirmed');
        else {
          try {
            const bytes = bodyCapture.domain.endsWith('.utf8.v1') ?
              (typeof bodyCapture.value === 'string' ? Buffer.from(bodyCapture.value, 'utf8') : null) :
              Buffer.from(canonicalHookBody(bodyCapture.value), 'utf8');
            if (!bytes) throw new TypeError('body_value_unavailable');
            if (bytes.length > Math.min(32 << 20, policy.max_item_bytes)) throw new RangeError('body_item_limit');
            const digest = 'sha256:' + createHash('sha256').update(bytes).digest('hex');
            const contentRef = ref('ocbody', eventRef, bodyCapture.kind, digest);
            document.content_state = 'referenced';
            document.content_refs = [{ kind: bodyCapture.kind, ref: contentRef, sha256: digest }];
            bodyItem = { observation_key: observationKey, collection_id: binding.collection_id,
              expected_collection_revision: binding.collection_revision,
              scope_generation: binding.scope_generation, event_ref: eventRef, event_revision: 1,
              content_kind: bodyCapture.kind, content_ref: contentRef, byte_domain: bodyCapture.domain,
              sha256: digest, policy_version: policy.policy_version, body: bytes };
          } catch (error) { gap(error?.message === 'body_item_limit' ? 'body_item_limit' : 'body_value_unavailable'); }
        }
      }
      if (subjectKind === 'task') document.task_ref = ref('task', runId);
      if (subjectKind === 'model_call' && action !== 'model.input_observed' && action !== 'model.output_observed') document.model_call_id = sourceId;
      if (subjectKind === 'tool_call') document.tool_call_id = sourceId;
      outbox.enqueue({
        collectionId: binding.collection_id,
        expectedCollectionRevision: binding.collection_revision,
        scopeGeneration: binding.scope_generation,
        kind: 'event', observationKey, document: canonicalWorkEventJSON(document),
      });
      if (bodyItem) {
        try {
          const committed = outbox.observation(observationKey);
          if (!committed || committed.documentRef !== eventRef) throw new Error('event_reference_unavailable');
          bodyOutbox.enqueue({ ...bodyItem, event_hash: committed.documentHash });
        } catch (error) {
          gap(error?.message === 'body_queue_full' ? 'body_queue_full' : 'body_enqueue_failed');
        }
      }
      if (subjectKind === 'model_call' && action === 'model.requested') {
        const committed = outbox.observation(observationKey);
        if (!committed) {
          gap('model_correlation_full');
        } else {
          outbox.recordModelStart({ callId: sourceId, runId, binding,
            eventRef: committed.documentRef, eventHash: committed.documentHash });
          relationFor(sourceId);
        }
      }
    } catch {
      gap('enqueue_failed');
    }
  }

  api.on('model_call_started', (event, context) => observe({
    event, context, subjectKind: 'model_call', lifecycle: 'started', action: 'model.requested', sourceId: event?.callId,
  }));
  api.on('model_call_ended', (event, context) => observe({
    event, context, subjectKind: 'model_call', lifecycle: event?.outcome === 'completed' ? 'completed' : 'failed',
    action: event?.outcome === 'completed' ? 'model.completed' : 'model.failed', sourceId: event?.callId,
  }));
  api.on('before_tool_call', (event, context) => {
    const tool = event?.toolName;
    const sourceId = event?.toolCallId ?? context?.toolCallId;
    const runId = event?.runId ?? context?.runId;
    const args = event?.params;
    const path = args && typeof args === 'object' ?
      [args.path, args.file_path, args.filePath, args.file].find((value) => typeof value === 'string' && value.length > 0) : undefined;
    if (['read', 'edit', 'write'].includes(tool) && typeof sourceId === 'string' && typeof runId === 'string' &&
        typeof path === 'string' && path.length <= 512 && !/[\u0000-\u001f]/.test(path)) {
      if (pendingToolPaths.size >= 2048) gap('tool_path_memory_full');
      else pendingToolPaths.set(`${runId}\0${sourceId}`, { tool, path, optional: args?.optional === true,
        createdMs: new Date(now()).getTime() });
    }
    observe({ event, context, subjectKind: 'tool_call', lifecycle: 'started', action: 'tool.invoked', sourceId,
      bodyCapture: args === undefined ? null : { kind: 'tool_input', domain: 'openclaw.before_tool_call.canonical-json.v1', value: args } });
  });
  api.on('after_tool_call', (event, context) => {
    observe({ event, context, subjectKind: 'tool_call', lifecycle: event?.error ? 'failed' : 'completed',
      action: event?.error ? 'tool.failed' : 'tool.completed',
      sourceId: event?.toolCallId ?? context?.toolCallId,
      bodyCapture: event?.result === undefined && event?.error === undefined ? null :
        { kind: 'tool_output', domain: 'openclaw.after_tool_call.canonical-json.v1',
          value: { ...(event.result !== undefined ? { result: event.result } : {}), ...(event.error !== undefined ? { error: event.error } : {}) } },
    });
    const tool = event?.toolName;
    if (!['read', 'edit', 'write'].includes(tool)) return;
    const sourceId = event?.toolCallId ?? context?.toolCallId;
    const runId = event?.runId ?? context?.runId;
    const pendingKey = `${runId}\0${sourceId}`;
    const priorPath = pendingToolPaths.get(pendingKey);
    pendingToolPaths.delete(pendingKey);
    const args = event?.params;
    const path = args && typeof args === 'object' ?
      [args.path, args.file_path, args.filePath, args.file].find((value) => typeof value === 'string' && value.length > 0) : undefined;
    const observedPath = path ?? (priorPath?.tool === tool ? priorPath.path : undefined);
    if (typeof observedPath !== 'string' || observedPath.length > 512 || /[\u0000-\u001f]/.test(observedPath)) { gap('resource_path_unavailable'); return; }
    const workspaceDir = typeof context?.workspaceDir === 'string' ? context.workspaceDir : runBindings.get(runId)?.workspaceDir;
    const absoluteWorkspace = typeof workspaceDir === 'string' && pathModule.isAbsolute(workspaceDir) ? workspaceDir : null;
    if (!pathModule.isAbsolute(observedPath) && !absoluteWorkspace) { gap('resource_workspace_unresolved'); return; }
    const locator = /^(?:~(?:\/|$)|[a-z][a-z0-9+.-]*:\/\/)/i.test(observedPath) ? null : observedPath;
    if (!locator) { gap('resource_path_unavailable'); return; }
    const resolvedLocator = pathModule.isAbsolute(locator) ? pathModule.normalize(locator) : pathModule.resolve(absoluteWorkspace, locator);
    const failed = Boolean(event?.error);
    const optionalRead = tool === 'read' && (args?.optional === true || priorPath?.optional === true);
    const details = event?.result?.details;
    const detailsPath = details?.path;
    const safeDetailsPath = typeof detailsPath === 'string' && detailsPath.length > 0 && detailsPath.length <= 512 &&
      !/[\u0000-\u001f]/.test(detailsPath) && !/^(?:~(?:\/|$)|[a-z][a-z0-9+.-]*:\/\/)/i.test(detailsPath);
    const resolvedDetailsPath = safeDetailsPath ?
      (pathModule.isAbsolute(detailsPath) ? pathModule.normalize(detailsPath) :
        absoluteWorkspace ? pathModule.resolve(absoluteWorkspace, detailsPath) : null) : null;
    const missing = optionalRead && details?.kind === 'not_found' && details?.status === 'not_found' &&
      details?.optional === true && resolvedDetailsPath === resolvedLocator;
    const unconfirmed = optionalRead && !failed && !missing &&
      !(details && ['text', 'image', 'truncated'].includes(details.kind));
    observe({ event, context, subjectKind: 'resource_action',
      lifecycle: failed || missing ? 'failed' : unconfirmed ? 'unknown' : 'completed',
      action: `resource.${tool}${failed ? '.failed' : missing ? '.not_found' : unconfirmed ? '.unconfirmed' : ''}`,
      sourceId,
      resource: { ref: ref('file', runBindings.get(runId)?.binding.collection_id ?? '', resolvedLocator),
        name_state: 'readable', name: observedPath },
    });
  });
  api.on('llm_input', (event, context) => observe({
    event, context, subjectKind: 'model_call', lifecycle: 'started', action: 'model.input_observed',
    sourceId: ref('hook', event?.runId ?? context?.runId ?? '', 'llm_input', randomUUID()),
    bodyCapture: event?.prompt === undefined ? null : { kind: 'model_input', domain: 'openclaw.llm_input.canonical-json.v1',
      value: { prompt: event.prompt, ...(event.systemPrompt !== undefined ? { systemPrompt: event.systemPrompt } : {}),
        historyMessages: event.historyMessages, ...(event.tools !== undefined ? { tools: modelToolDeclarations(event.tools) } : {}) } },
  }));
  api.on('llm_output', (event, context) => observe({
    event, context, subjectKind: 'model_call', lifecycle: 'completed', action: 'model.output_observed',
    sourceId: ref('hook', event?.runId ?? context?.runId ?? '', 'llm_output', randomUUID()),
    bodyCapture: !Array.isArray(event?.assistantTexts) ? null : { kind: 'model_output', domain: 'openclaw.llm_output.canonical-json.v1',
      value: { assistantTexts: event.assistantTexts,
        ...(event.lastAssistant !== undefined ? { lastAssistant: event.lastAssistant } : {}) } },
  }));
  api.on('message_received', (event, context) => {
    const sourceId = event?.messageId && safeRef.test(event.messageId) ? event.messageId :
      ref('observation', 'message_received', randomUUID());
    if (typeof event?.content !== 'string') { gap('message_body_unavailable'); return; }
    if (matchingBinding(getBindings(), context)) {
      observe({ event, context, subjectKind: 'message', lifecycle: 'unknown', action: 'message.received_observed', sourceId,
        bodyCapture: { kind: 'user_input', domain: 'openclaw.message_received.utf8.v1', value: event.content } });
      return;
    }
    // Public inbound message context can carry a runId but no project key.
    // Hold only a small, short-lived in-memory observation until an agent
    // hook for that exact run supplies the authorized project binding.
    const runId = event?.runId ?? context?.runId;
    if (typeof runId !== 'string' || !safeRef.test(runId)) { gap('message_project_unresolved'); return; }
    if (Buffer.byteLength(event.content) > (256 << 10) || pendingInbound.size >= 128) {
      gap('message_pending_capacity'); return;
    }
    if (pendingInbound.has(runId)) { gap('message_run_ambiguous'); pendingInbound.delete(runId); return; }
    pendingInbound.set(runId, { event, sourceId, createdMs: new Date(now()).getTime() });
  });
  api.on('message_sent', (event, context) => {
    if (event?.success !== true) { gap('message_delivery_failed'); return; }
    if (!matchingBinding(getBindings(), context)) { gap('message_project_unresolved'); return; }
    observe({ event, context, subjectKind: 'message', lifecycle: 'unknown', action: 'message.sent_observed',
      sourceId: event?.messageId && safeRef.test(event.messageId) ? event.messageId : ref('hook', event?.runId ?? '', 'message_sent', randomUUID()),
      bodyCapture: typeof event?.content === 'string' ? { kind: 'user_output', domain: 'openclaw.message_sent.utf8.v1', value: event.content } : null,
    });
  });
  api.on('agent_end', (event, context) => {
    const outcome = agentEndTaskOutcome(event);
    observe({ event, context, subjectKind: 'task', ...outcome, sourceId: event?.runId ?? context?.runId });
  });
  return {
    observeProviderCallStart({ requestId, workspaceDir, agentId }) {
      try {
        if (typeof requestId !== 'string' || !safeRef.test(requestId)) return gap('ur_request_correlation_missing');
        outbox.recordModelContext({ callId: requestId, workspaceDir, agentId });
        const pending = outbox.pendingModel(requestId);
        if (pending && matchingBinding(getBindings(), pending)) observe({ event: { runId: pending.runId, callId: requestId }, context: pending,
          subjectKind: 'model_call', lifecycle: 'started', action: 'model.requested', sourceId: requestId });
      } catch { gap('model_context_failed'); }
    },
    observeTransportResponse({ requestId, urRequestId, status, workspaceDir, agentId }) {
      try {
        const now = new Date().getTime();
        prune(now);
        if (typeof requestId !== 'string' || !safeRef.test(requestId)) return gap('ur_request_correlation_missing');
        if (typeof urRequestId !== 'string' || !urRequestPattern.test(urRequestId)) return gap('ur_header_missing');
        if (!Number.isSafeInteger(status) || status < 100 || status > 599) return gap('ur_response_invalid');
        outbox.recordHeader({ callId: requestId, urId: urRequestId, status, workspaceDir, agentId });
        relationFor(requestId);
      } catch {
        gap('ur_response_observation_failed');
      }
    },
  };
}
