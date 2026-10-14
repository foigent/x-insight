import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { WorkEventOutbox } from './outbox.mjs';
import { ReviewBodyOutbox } from './body-outbox.mjs';
import { registerOpenClawWorkEventHooks } from './producer.mjs';
import { deliverNext } from './transport.mjs';

const reporter = { source_system_id: '11111111-1111-4111-8111-111111111111', source_instance: 'agent-a', org_id: '7', project_id: '9' };
const binding = { collection_id: '33333333-3333-4333-8333-333333333333', producer_project_key: 'workspace:/synthetic/project',
  reporter, scope_generation: '1', collection_revision: 2 };

test('public body hooks retain exact source domains and read/edit paths without guessing outbound run identity', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-body-hooks-'));
  const outbox = new WorkEventOutbox(join(dir, 'events.sqlite'));
  const bodies = new ReviewBodyOutbox(join(dir, 'bodies.sqlite'), Buffer.alloc(32, 5));
  try {
    const hooks = new Map(), gaps = [];
    registerOpenClawWorkEventHooks({ on: (name, callback) => hooks.set(name, callback) }, {
      outbox, bodyOutbox: bodies, onGap: (code) => gaps.push(code),
      bindings: [{ ...binding, body_capture: { mode: 'full_content', policy_version: 'xi:default:1', max_item_bytes: 1 << 20 } }],
    });
    hooks.get('message_received')({ runId: 'run-body', messageId: 'incoming-one', content: 'hello from human' }, { sessionKey: 'same-session' });
    assert.equal(outbox.status().rows, 0, 'unbound inbound content remains in bounded memory only');
    const context = { runId: 'run-body', workspaceDir: '/synthetic/project', agentId: 'agent-one', sessionKey: 'same-session' };
    hooks.get('model_call_started')({ runId: 'run-body', callId: 'provider-call-one' }, context);
    const input = bodies.next();
    assert.equal(input.content_kind, 'user_input');
    assert.equal(input.byte_domain, 'openclaw.message_received.utf8.v1');
    assert.equal(input.body.toString(), 'hello from human');
    bodies.acknowledge(input.content_ref);
    hooks.get('llm_input')({ runId: 'run-body', prompt: 'model text', historyMessages: [{ role: 'user', content: 'hello' }] }, context);
    const model = bodies.next();
    assert.equal(model.content_kind, 'model_input');
    assert.equal(model.byte_domain, 'openclaw.llm_input.canonical-json.v1');
    assert.match(model.body.toString(), /model text/);
    bodies.acknowledge(model.content_ref);
    hooks.get('after_tool_call')({ runId: 'run-body', toolCallId: 'read-one', toolName: 'read',
      params: { path: '/synthetic/project/notes.txt' }, result: 'file contents' }, context);
    hooks.get('after_tool_call')({ runId: 'run-body', toolCallId: 'edit-one', toolName: 'edit',
      params: { file_path: '/synthetic/project/notes.txt' }, result: 'edited' }, context);
    hooks.get('message_sent')({ sessionKey: 'same-session', content: 'final answer', success: true }, { sessionKey: 'same-session' });
    const events = outbox.db.prepare("SELECT document FROM items WHERE kind='event'").all().map((row) => JSON.parse(row.document));
    assert.equal(events.filter((value) => value.action_kind === 'resource.read').length, 1);
    assert.equal(events.filter((value) => value.action_kind === 'resource.edit').length, 1);
    assert.ok(events.filter((value) => value.subject_kind === 'resource_action').every((value) =>
      value.resource.name === '/synthetic/project/notes.txt'));
    assert.equal(events.filter((value) => value.action_kind === 'model.requested').length, 1);
    assert.equal(events.filter((value) => value.action_kind === 'model.input_observed').length, 1);
    assert.ok(gaps.includes('message_project_unresolved'));
    assert.doesNotMatch(outbox.db.prepare('SELECT group_concat(document) AS raw FROM items').get().raw, /hello from human|file contents|model text/);
  } finally { bodies.close(); outbox.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('public model and message hooks do not invent call or task identity', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-independent-observations-'));
  const outbox = new WorkEventOutbox(join(dir, 'events.sqlite'));
  try {
    const hooks = new Map();
    registerOpenClawWorkEventHooks({ on: (name, callback) => hooks.set(name, callback) },
      { outbox, bindings: [binding] });
    const context = { workspaceDir: '/synthetic/project', runId: 'run-observation' };
    hooks.get('llm_input')({ runId: 'run-observation', prompt: 'one' }, context);
    hooks.get('llm_output')({ runId: 'run-observation', assistantTexts: ['two'] }, context);
    hooks.get('message_sent')({ success: true, messageId: 'message-one', content: 'three' },
      { workspaceDir: '/synthetic/project' });
    const events = outbox.db.prepare("SELECT document FROM items WHERE kind='event'").all().map(row => JSON.parse(row.document));
    assert.equal(events.length, 3);
    assert.equal(new Set(events.map(event => event.event_ref)).size, 3);
    for (const event of events.filter(event => event.subject_kind === 'model_call')) {
      assert.equal(event.model_call_id, undefined);
      assert.equal(event.run_id, 'run-observation');
    }
    const message = events.find(event => event.subject_kind === 'message');
    assert.equal(message.action_kind, 'message.sent_observed');
    assert.equal(message.run_id, undefined);
    assert.equal(message.task_ref, undefined);
    assert.equal(message.lifecycle_state, 'unknown');
  } finally { outbox.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('agent_end uses the current turn terminal assistant without retaining message bodies', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-agent-end-'));
  const outbox = new WorkEventOutbox(join(dir, 'events.sqlite'));
  try {
    const hooks = new Map();
    registerOpenClawWorkEventHooks({ on: (name, callback) => hooks.set(name, callback) },
      { outbox, bindings: [binding] });
    const end = (runId, success, messages) => hooks.get('agent_end')({ runId, success, messages },
      { runId, workspaceDir: '/synthetic/project' });
    const turn = stopReason => [
      { role: 'user', content: 'private user message' },
      { role: 'assistant', stopReason: 'toolUse', content: 'private tool call' },
      { role: 'toolResult', content: 'private tool result' },
      { role: 'assistant', stopReason, errorMessage: 'private provider error', content: 'private assistant message' },
    ];
    end('run-terminal-error', true, turn('error'));
    end('run-terminal-aborted', true, turn('aborted'));
    end('run-hook-failed', false, undefined);
    end('run-terminal-ok', true, turn('stop'));
    end('run-unsettled-tool', true, turn('toolUse'));
    end('run-missing-messages', true, undefined);
    end('run-history-only', true, [
      { role: 'assistant', stopReason: 'error', content: 'private older assistant' },
      { role: 'user', content: 'private current message' },
    ]);
    end('run-terminal-error', true, turn('error'));
    const raw = outbox.db.prepare("SELECT document FROM items WHERE kind='event'").all().map(row => row.document);
    const results = new Map(raw.map(document => {
      const event = JSON.parse(document);
      return [event.run_id, [event.action_kind, event.lifecycle_state]];
    }));
    assert.equal(raw.length, 7, 'exact replay does not create another task event');
    assert.deepEqual(results.get('run-terminal-error'), ['task.failed', 'failed']);
    assert.deepEqual(results.get('run-terminal-aborted'), ['task.failed', 'failed']);
    assert.deepEqual(results.get('run-hook-failed'), ['task.failed', 'failed']);
    assert.deepEqual(results.get('run-terminal-ok'), ['task.completed', 'completed']);
    assert.deepEqual(results.get('run-unsettled-tool'), ['task.ended_observed', 'unknown']);
    assert.deepEqual(results.get('run-missing-messages'), ['task.ended_observed', 'unknown']);
    assert.deepEqual(results.get('run-history-only'), ['task.ended_observed', 'unknown']);
    assert.doesNotMatch(raw.join('\n'), /private user|private tool|private provider|private assistant|private older|private current/);
  } finally { outbox.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('file observations retain failed attempts and scope lexical file identity to the observed workspace', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-resource-identity-'));
  const outbox = new WorkEventOutbox(join(dir, 'events.sqlite'));
  try {
    const hooks = new Map(), gaps = [];
    const second = { ...binding, collection_id: '44444444-4444-4444-8444-444444444444',
      producer_project_key: 'workspace:/synthetic/other' };
    registerOpenClawWorkEventHooks({ on: (name, callback) => hooks.set(name, callback) },
      { outbox, bindings: [binding, second], onGap: code => gaps.push(code) });
    const a = { runId: 'run-a', workspaceDir: '/synthetic/project' };
    const b = { runId: 'run-b', workspaceDir: '/synthetic/other' };
    hooks.get('agent_end')({ runId: 'run-a', success: true }, a);
    hooks.get('agent_end')({ runId: 'run-b', success: true }, b);
    hooks.get('before_tool_call')({ runId: 'run-a', toolCallId: 'one', toolName: 'read',
      params: { path: 'notes.txt' } }, { runId: 'run-a' });
    hooks.get('after_tool_call')({ runId: 'run-a', toolCallId: 'one', toolName: 'read',
      result: 'contents' }, { runId: 'run-a' });
    hooks.get('after_tool_call')({ runId: 'run-a', toolCallId: 'two', toolName: 'edit',
      params: { path: '/synthetic/project/notes.txt' }, error: 'rejected' }, { runId: 'run-a' });
    hooks.get('after_tool_call')({ runId: 'run-b', toolCallId: 'three', toolName: 'write',
      params: { path: 'notes.txt' }, error: 'denied' }, { runId: 'run-b' });
    const documents = outbox.db.prepare("SELECT document FROM items WHERE kind='event'").all()
      .map(row => JSON.parse(row.document));
    const resources = documents.filter(document => document.subject_kind === 'resource_action');
    assert.equal(resources.length, 3);
    assert.equal(resources[0].resource.ref, resources[1].resource.ref);
    assert.notEqual(resources[0].resource.ref, resources[2].resource.ref);
    assert.equal(resources[0].resource.name, 'notes.txt');
    assert.equal(resources[1].resource.name, '/synthetic/project/notes.txt');
    assert.equal(resources[1].action_kind, 'resource.edit.failed');
    assert.equal(resources[1].lifecycle_state, 'failed');
    assert.equal(resources[2].action_kind, 'resource.write.failed');
    assert.equal(resources[2].lifecycle_state, 'failed');
    assert.equal(gaps.includes('resource_workspace_unresolved'), false);
  } finally { outbox.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('optional missing and rejected read arguments never claim file contents were read', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-optional-read-'));
  const outbox = new WorkEventOutbox(join(dir, 'events.sqlite'));
  try {
    const hooks = new Map();
    registerOpenClawWorkEventHooks({ on: (name, callback) => hooks.set(name, callback) },
      { outbox, bindings: [binding] });
    const context = { runId: 'run-optional', workspaceDir: '/synthetic/project' };
    hooks.get('before_tool_call')({ runId: 'run-optional', toolCallId: 'missing', toolName: 'read',
      params: { path: 'missing.txt', optional: true } }, context);
    hooks.get('after_tool_call')({ runId: 'run-optional', toolCallId: 'missing', toolName: 'read',
      result: { content: [{ type: 'text', text: 'Optional file not found' }],
        details: { kind: 'not_found', status: 'not_found', path: 'missing.txt', optional: true } } }, context);
    hooks.get('after_tool_call')({ runId: 'run-optional', toolCallId: 'different', toolName: 'read',
      params: { path: 'different.txt', optional: true },
      result: { details: { kind: 'not_found', status: 'not_found', path: 'another.txt', optional: true } } }, context);
    hooks.get('after_tool_call')({ runId: 'run-optional', toolCallId: 'absolute', toolName: 'read',
      params: { path: 'absolute.txt', optional: true },
      result: { details: { kind: 'not_found', status: 'not_found', path: '/synthetic/project/absolute.txt', optional: true } } }, context);
    hooks.get('after_tool_call')({ runId: 'run-optional', toolCallId: 'ambiguous', toolName: 'read',
      params: { path: 'ambiguous.txt', optional: true }, result: { content: [{ type: 'text', text: 'unclassified' }] } }, context);
    hooks.get('after_tool_call')({ runId: 'run-optional', toolCallId: 'present', toolName: 'read',
      params: { path: 'present.txt', optional: true }, result: { details: { kind: 'text', content: 'contents' } } }, context);
    hooks.get('after_tool_call')({ runId: 'run-optional', toolCallId: 'strict', toolName: 'read',
      params: { path: 'strict.txt' }, result: { details: { kind: 'text', content: 'contents' } } }, context);
    hooks.get('after_tool_call')({ runId: 'run-optional', toolCallId: 'rejected', toolName: 'read',
      params: { path: 'rejected.txt', optional: false },
      error: 'Validation failed for tool "read": optional must be equal to constant' }, context);
    const events = outbox.db.prepare("SELECT document FROM items WHERE kind='event'").all()
      .map(row => JSON.parse(row.document));
    const action = name => events.find(item => item.resource?.name === name && item.subject_kind === 'resource_action');
    assert.equal(events.find(item => item.action_kind === 'tool.completed' && item.tool_call_id === 'missing')?.lifecycle_state, 'completed');
    assert.equal(action('missing.txt')?.action_kind, 'resource.read.not_found');
    assert.equal(action('missing.txt')?.lifecycle_state, 'failed');
    assert.equal(action('different.txt')?.action_kind, 'resource.read.unconfirmed');
    assert.equal(action('absolute.txt')?.action_kind, 'resource.read.not_found');
    assert.equal(action('ambiguous.txt')?.action_kind, 'resource.read.unconfirmed');
    assert.equal(action('ambiguous.txt')?.lifecycle_state, 'unknown');
    assert.equal(action('present.txt')?.action_kind, 'resource.read');
    assert.equal(action('present.txt')?.lifecycle_state, 'completed');
    assert.equal(action('strict.txt')?.action_kind, 'resource.read');
    assert.equal(action('strict.txt')?.lifecycle_state, 'completed');
    assert.equal(events.find(item => item.action_kind === 'tool.failed' && item.tool_call_id === 'rejected')?.lifecycle_state, 'failed');
    assert.equal(action('rejected.txt')?.action_kind, 'resource.read.failed');
    assert.equal(action('rejected.txt')?.lifecycle_state, 'failed');
  } finally { outbox.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('relative file path without a verified workspace remains a gap', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-resource-unresolved-'));
  const outbox = new WorkEventOutbox(join(dir, 'events.sqlite'));
  try {
    const hooks = new Map(), gaps = [];
    registerOpenClawWorkEventHooks({ on: (name, callback) => hooks.set(name, callback) },
      { outbox, bindings: [{ ...binding, producer_project_key: 'agent:agent-one' }], onGap: code => gaps.push(code) });
    hooks.get('after_tool_call')({ runId: 'run-unresolved', toolCallId: 'read-one', toolName: 'read',
      params: { path: 'notes.txt' }, error: 'not found' }, { runId: 'run-unresolved', agentId: 'agent-one' });
    const events = outbox.db.prepare("SELECT document FROM items WHERE kind='event'").all()
      .map(row => JSON.parse(row.document));
    assert.equal(events.some(item => item.subject_kind === 'resource_action'), false);
    assert.equal(events.some(item => item.action_kind === 'tool.failed'), true);
    assert.ok(gaps.includes('resource_workspace_unresolved'));
  } finally { outbox.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('hook observations persist only bounded metadata and never change tool outcomes', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-producer-'));
  const outbox = new WorkEventOutbox(join(dir, 'outbox.sqlite'), { maxRows: 4 });
  try {
    const hooks = new Map();
    const diagnostics = [];
    registerOpenClawWorkEventHooks({ on: (name, callback) => hooks.set(name, callback) },
      { outbox, bindings: [binding], onGap: (code) => diagnostics.push(code) });
    const context = { workspaceDir: '/synthetic/project', runId: 'run-a' };
    hooks.get('model_call_started')({ runId: 'run-a', callId: 'run-a:model:1', prompt: 'secret model input' }, context);
    hooks.get('model_call_started')({ runId: 'run-a', callId: 'run-a:model:1', prompt: 'secret model input' }, context);
    assert.equal(outbox.status().rows, 1);
    hooks.get('model_call_started')({ runId: 'run-a', callId: 'foreign' },
      { workspaceDir: '/synthetic/other', runId: 'run-a' });
    assert.equal(outbox.status().rows, 1);
    hooks.get('model_call_ended')({ runId: 'run-a', callId: 'run-a:model:1', outcome: 'completed', response: 'secret model output' }, context);
    hooks.get('before_tool_call')({ runId: 'run-a', toolCallId: 'tool-1', params: { code: 'secret tool input' } }, { runId: 'run-a' });
    hooks.get('after_tool_call')({ runId: 'run-a', toolCallId: 'tool-1', result: 'secret tool output' }, { runId: 'run-a' });
    assert.equal(outbox.status().rows, 4);
    const raw = outbox.db.prepare('SELECT document FROM items').all().map((row) => row.document).join('\n');
    assert.doesNotMatch(raw, /secret/);
    assert.match(raw, /model.requested/);
    assert.match(raw, /tool.completed/);
    hooks.get('agent_end')({ runId: 'run-a', success: true, messages: ['secret history'] }, context);
    assert.equal(outbox.status().rows, 4);
    assert.deepEqual(diagnostics, ['run_project_changed', 'body_custody_unconfigured', 'body_custody_unconfigured', 'enqueue_failed']);
    assert.equal(outbox.gapStatus().reduce((sum, row) => sum + row.occurrences, 0), 4);
    hooks.get('model_call_started')({ runId: 'run-b', callId: 'call-b' }, { workspaceDir: '/unknown', runId: 'run-b' });
    assert.deepEqual(diagnostics, ['run_project_changed', 'body_custody_unconfigured', 'body_custody_unconfigured', 'enqueue_failed']);
    assert.equal(outbox.pendingModel('call-b'), null);
  } finally { outbox.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('ended and restarted callbacks retain exact bytes through lost and successful acknowledgement', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-replay-'));
  const path = join(dir, 'outbox.sqlite');
  let clock = 100000;
  let outbox = new WorkEventOutbox(path, { clock: () => clock });
  const createHooks = () => {
    const hooks = new Map();
    registerOpenClawWorkEventHooks({ on: (name, callback) => hooks.set(name, callback) }, { outbox, bindings: [binding] });
    return hooks;
  };
  try {
    const context = { workspaceDir: '/synthetic/project', runId: 'run-replay' };
    let hooks = createHooks();
    hooks.get('agent_end')({ runId: 'run-replay', success: true }, context);
    const original = outbox.next();
    assert.ok(original);
    hooks.get('agent_end')({ runId: 'run-replay', success: true }, context);
    assert.equal(outbox.status().rows, 1);
    outbox.retry(original.id, 'transport_unavailable');
    outbox.close();
    outbox = new WorkEventOutbox(path, { clock: () => clock });
    hooks = createHooks();
    hooks.get('agent_end')({ runId: 'run-replay', success: true }, context);
    assert.equal(outbox.status().rows, 1);
    clock += 1000;
    assert.equal(outbox.next().document, original.document);
    assert.equal(outbox.acknowledge(original.id), true);
    assert.equal(outbox.status().rows, 0);
    hooks.get('agent_end')({ runId: 'run-replay', success: true }, context);
    assert.equal(outbox.status().rows, 0);
    assert.equal(outbox.db.prepare('SELECT count(*) AS n FROM observations').get().n, 1);
    hooks.get('model_call_started')({ runId: 'run-model-restart', callId: 'call-1' },
      { ...context, runId: 'run-model-restart' });
    const modelDocument = outbox.next().document;
    outbox.close();
    outbox = new WorkEventOutbox(path, { clock: () => clock });
    createHooks().get('model_call_started')({ runId: 'run-model-restart', callId: 'call-1' },
      { ...context, runId: 'run-model-restart' });
    assert.equal(outbox.status().rows, 1);
    assert.equal(outbox.next().document, modelDocument);
  } finally { outbox.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('interleaved model calls relate only exact observed UR headers to their own event hash', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-relations-'));
  const outbox = new WorkEventOutbox(join(dir, 'outbox.sqlite'));
  try {
    const hooks = new Map();
    const urTarget = { scope: { source_system_id: '22222222-2222-4222-8222-222222222222',
      source_instance: 'ur-a', org_id: '7', project_id: '86' }, label: 'Synthetic UR' };
    const producer = registerOpenClawWorkEventHooks({ on: (name, callback) => hooks.set(name, callback) },
      { outbox, bindings: [{ ...binding, ur_target: urTarget }] });
    for (const [runId, callId] of [['run-a', 'call-a'], ['run-b', 'call-b']]) {
      hooks.get('model_call_started')({ runId, callId }, { runId, workspaceDir: '/synthetic/project' });
    }
    producer.observeTransportResponse({ requestId: 'call-b', urRequestId: `op_${'b'.repeat(32)}`,
      status: 200, workspaceDir: '/synthetic/project' });
    producer.observeTransportResponse({ requestId: 'call-a', urRequestId: `op_${'a'.repeat(32)}`,
      status: 200, workspaceDir: '/synthetic/project' });
    hooks.get('model_call_started')({ runId: 'run-c', callId: 'call-c' },
      { runId: 'run-c', workspaceDir: '/synthetic/project' });
    producer.observeTransportResponse({ requestId: 'call-c', urRequestId: '',
      status: 200, workspaceDir: '/synthetic/project' });
    const rows = outbox.db.prepare('SELECT kind,document FROM items').all();
    assert.equal(rows.filter((row) => row.kind === 'relation').length, 2);
    const events = new Map(rows.filter((row) => row.kind === 'event')
      .map((row) => [JSON.parse(row.document).model_call_id, row.document]));
    for (const row of rows.filter((entry) => entry.kind === 'relation')) {
      const relation = JSON.parse(row.document);
      const source = events.get(relation.model_call_id);
      assert.ok(source);
      assert.equal(relation.event_ref, JSON.parse(source).event_ref);
      assert.equal(relation.event_hash, `sha256:${createHash('sha256').update(source).digest('hex')}`);
      assert.equal(relation.ur_request_id, relation.model_call_id === 'call-a' ? `op_${'a'.repeat(32)}` : `op_${'b'.repeat(32)}`);
      assert.deepEqual(relation.ur_target, urTarget.scope);
    }
    assert.equal(rows.some((row) => row.kind === 'relation' && JSON.parse(row.document).model_call_id === 'call-c'), false);
  } finally { outbox.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('a model hook without project context waits for its exact provider call context', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-deferred-'));
  const outbox = new WorkEventOutbox(join(dir, 'outbox.sqlite'));
  try {
    const hooks = new Map();
    const producer = registerOpenClawWorkEventHooks({ on: (name, callback) => hooks.set(name, callback) },
      { outbox, bindings: [binding] });
    hooks.get('model_call_started')({ runId: 'run-deferred', callId: 'call-deferred' }, { runId: 'run-deferred' });
    assert.equal(outbox.status().rows, 0);
    producer.observeProviderCallStart({ requestId: 'call-deferred', workspaceDir: '/synthetic/project' });
    assert.equal(outbox.status().rows, 1);
    assert.equal(JSON.parse(outbox.next().document).model_call_id, 'call-deferred');
  } finally { outbox.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('cross-midnight replay inside the bounded window keeps original event and relation bytes', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-midnight-'));
  let at = Date.parse('2026-09-29T23:59:59.500Z');
  const outbox = new WorkEventOutbox(join(dir, 'outbox.sqlite'), { clock: () => at });
  try {
    const hooks = new Map();
    const target = { scope: { source_system_id: '22222222-2222-4222-8222-222222222222',
      source_instance: 'ur-a', org_id: '7', project_id: '86' } };
    const producer = registerOpenClawWorkEventHooks({ on: (name, callback) => hooks.set(name, callback) },
      { outbox, bindings: [{ ...binding, ur_target: target }], now: () => at });
    const source = { runId: 'run-midnight', callId: 'call-midnight' };
    const context = { runId: source.runId, workspaceDir: '/synthetic/project' };
    const header = `op_${'1'.repeat(32)}`;
    hooks.get('model_call_started')(source, context);
    producer.observeTransportResponse({ requestId: source.callId, urRequestId: header,
      status: 200, workspaceDir: context.workspaceDir });
    const originals = outbox.db.prepare('SELECT id,kind,document FROM items ORDER BY kind').all();
    assert.equal(originals.length, 2);
    for (const row of originals) assert.equal(outbox.acknowledge(row.id), true);
    at += 1000;
    hooks.get('model_call_started')(source, context);
    producer.observeTransportResponse({ requestId: source.callId, urRequestId: header,
      status: 200, workspaceDir: context.workspaceDir });
    assert.equal(outbox.status().rows, 0);
    assert.equal(outbox.db.prepare('SELECT count(*) AS n FROM observations').get().n, 2);
    assert.ok(originals.every((row) => JSON.parse(row.document).recorded_at.startsWith('2026-09-29')));
  } finally { outbox.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('replay after the bounded window keeps source identity and exposes receiver conflicts', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-expired-replay-'));
  let at = Date.parse('2026-09-29T23:59:59.500Z');
  const outbox = new WorkEventOutbox(join(dir, 'outbox.sqlite'), { clock: () => at });
  try {
    const target = { scope: { source_system_id: '22222222-2222-4222-8222-222222222222',
      source_instance: 'ur-a', org_id: '7', project_id: '86' } };
    const source = { runId: 'run-expired', callId: 'call-expired' };
    const context = { runId: source.runId, workspaceDir: '/synthetic/project' };
    const header = `op_${'2'.repeat(32)}`;
    const produce = () => {
      const hooks = new Map();
      const producer = registerOpenClawWorkEventHooks({ on: (name, callback) => hooks.set(name, callback) },
        { outbox, bindings: [{ ...binding, ur_target: target }], now: () => at });
      hooks.get('model_call_started')(source, context);
      producer.observeTransportResponse({ requestId: source.callId, urRequestId: header,
        status: 200, workspaceDir: context.workspaceDir });
    };
    produce();
    const originals = new Map(outbox.db.prepare('SELECT id,kind,document FROM items').all()
      .map((row) => [row.kind, row]));
    assert.equal(originals.size, 2);
    for (const row of originals.values()) assert.equal(outbox.acknowledge(row.id), true);
    at += 24 * 60 * 60 * 1000 + 1;
    produce();
    const replays = outbox.db.prepare('SELECT kind,document FROM items').all();
    assert.equal(replays.length, 2);
    for (const row of replays) {
      const before = JSON.parse(originals.get(row.kind).document);
      const after = JSON.parse(row.document);
      assert.equal(after[row.kind === 'event' ? 'event_ref' : 'relation_ref'],
        before[row.kind === 'event' ? 'event_ref' : 'relation_ref']);
      assert.notEqual(row.document, originals.get(row.kind).document);
    }
    const fetchImpl = async () => new Response(JSON.stringify({ code: 'work_event_conflict' }),
      { status: 409, headers: { 'Content-Type': 'application/json' } });
    for (let i = 0; i < 2; i++) {
      const outcome = await deliverNext(outbox, { origin: 'https://xi.test',
        token: `xib_${'a'.repeat(43)}`, fetchImpl });
      assert.deepEqual(outcome, { state: 'blocked', code: 'conflict' });
    }
    assert.equal(outbox.status().blocked, 2);
    assert.equal(outbox.next(), null);
  } finally { outbox.close(); rmSync(dir, { recursive: true, force: true }); }
});


test('an ongoing run uses current retention policy and never resumes body capture from its remembered binding', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-live-policy-'));
  const outbox = new WorkEventOutbox(join(dir,'events.sqlite'));
  const captured=[],gaps=[],hooks=new Map();
  let current=[{...binding,body_capture:{mode:'full_content',policy_version:'xi:default:1',max_item_bytes:1024}}];
  try {
    registerOpenClawWorkEventHooks({on:(name,hook)=>hooks.set(name,hook)},
      {outbox,getBindings:()=>current,bodyOutbox:{enqueue:item=>captured.push(item)},onGap:code=>gaps.push(code)});
    const input=(text,context={runId:'same-run'})=>hooks.get('llm_input')({runId:'same-run',prompt:text,historyMessages:[]},context);
    input('first',{runId:'same-run',workspaceDir:'/synthetic/project'});
    current=[{...binding,body_capture:{mode:'disabled'}}];
    input('disabled');
    assert.equal(captured.length,1);
    assert.equal(gaps.includes('body_policy_unconfirmed'),false,'an explicit disabled policy is deliberate');
    current=[{...binding,body_capture:{mode:'full_content',policy_version:'xi:default:2',max_item_bytes:1024}}];
    input('new policy');
    assert.equal(captured.length,2);
    assert.equal(captured[1].policy_version,'xi:default:2');
    current=[binding]; // config refresh failed or expired: only identity is retained
    input('unconfirmed');
    current=[]; // the bound project was removed altogether
    input('removed');
    assert.equal(captured.length,2);
    assert.equal(gaps.filter(code=>code==='body_policy_unconfirmed').length,2);
    const docs=outbox.db.prepare("SELECT document FROM items WHERE kind='event' ORDER BY rowid").all().map(row=>JSON.parse(row.document));
    assert.deepEqual(docs.map(row=>row.content_state),['referenced','not_captured','referenced','not_captured','not_captured']);
  } finally {outbox.close();rmSync(dir,{recursive:true,force:true});}
});


test('hook JSON bodies preserve decimal values and omit absent optional fields without weakening event metadata',()=>{
  const dir=mkdtempSync(join(tmpdir(),'xi-hook-body-json-')),outbox=new WorkEventOutbox(join(dir,'events.sqlite'));
  const hooks=new Map(),bodies=[],gaps=[];
  try {
    registerOpenClawWorkEventHooks({on:(name,hook)=>hooks.set(name,hook)}, {outbox,
      bindings:[{...binding,body_capture:{mode:'full_content',policy_version:'xi:default:1',max_item_bytes:4096}}],
      bodyOutbox:{enqueue:item=>bodies.push(item)},onGap:code=>gaps.push(code)});
    const context={runId:'body-json',workspaceDir:'/synthetic/project'};
    hooks.get('llm_input')({runId:'body-json',prompt:'source view',historyMessages:[],
      tools:[{name:'calculate',description:undefined,parameters:{temperature:0.7}}]},context);
    assert.equal(bodies.length,1);
    assert.deepEqual(JSON.parse(bodies[0].body.toString()),{prompt:'source view',historyMessages:[],
      tools:[{name:'calculate',parameters:{temperature:0.7}}]});
    hooks.get('after_tool_call')({runId:'body-json',toolCallId:'decimal',toolName:'calculate',result:{value:1.25}},context);
    assert.equal(bodies.length,2);assert.equal(JSON.parse(bodies[1].body.toString()).result.value,1.25);
    hooks.get('after_tool_call')({runId:'body-json',toolCallId:'nonfinite',toolName:'calculate',result:{value:Infinity}},context);
    assert.equal(bodies.length,2);assert.ok(gaps.includes('body_value_unavailable'));
  } finally {outbox.close();rmSync(dir,{recursive:true,force:true});}
});


test('native runtime tool functions are excluded from the model input declaration without executing them',()=>{
  const dir=mkdtempSync(join(tmpdir(),'xi-native-tool-declaration-')),outbox=new WorkEventOutbox(join(dir,'events.sqlite'));
  const hooks=new Map(),bodies=[],gaps=[];let executed=false;
  try {
    registerOpenClawWorkEventHooks({on:(name,hook)=>hooks.set(name,hook)}, {outbox,
      bindings:[{...binding,body_capture:{mode:'full_content',policy_version:'xi:default:1',max_item_bytes:4096}}],
      bodyOutbox:{enqueue:item=>bodies.push(item)},onGap:code=>gaps.push(code)});
    const declaration={name:'read',description:'Read the selected file',parameters:{type:'object',properties:{path:{type:'string'}}}};
    const runtime={...declaration,label:'Runtime-only label',execute(){executed=true;throw Error('must not execute');}};
    hooks.get('llm_input')({runId:'native-tools',prompt:'read a file',historyMessages:[],tools:[runtime]},
      {runId:'native-tools',workspaceDir:'/synthetic/project'});
    assert.equal(executed,false);assert.equal(bodies.length,1);assert.deepEqual(gaps,[]);
    assert.deepEqual(JSON.parse(bodies[0].body.toString()).tools,[declaration]);
    assert.equal(typeof runtime.execute,'function','the source object remains unchanged');
  } finally {outbox.close();rmSync(dir,{recursive:true,force:true});}
});
