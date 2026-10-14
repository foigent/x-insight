import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtempSync, readFileSync, realpathSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import plugin from './index.mjs';
import { createPrivateFile } from './managed-secret-files.mjs';
import { WorkEventOutbox, canonicalWorkEventJSON } from './outbox.mjs';

for (const managed of [false,true]) test(`public plugin entry preserves callbacks and sends exact receipts (managed=${managed})`, async () => {
  const manifest = JSON.parse(readFileSync(new URL('./openclaw.plugin.json', import.meta.url)));
  assert.equal(manifest.id, plugin.id);
  const dir = realpathSync(mkdtempSync(join(tmpdir(), 'xi-work-plugin-')));
  const tokenFile = join(dir, 'ingest.key');
  createPrivateFile(tokenFile, Buffer.from(`xib_${'a'.repeat(43)}`));
  const priorToken = process.env.XINSIGHT_WORK_EVENT_TOKEN;
  const priorFetch = globalThis.fetch;
  process.env.XINSIGHT_WORK_EVENT_TOKEN = `xib_${'z'.repeat(43)}`;
  const reporter = { source_system_id: '11111111-1111-4111-8111-111111111111',
    source_instance: 'agent-a', org_id: '7', project_id: '9' };
  const urTarget = { scope: { source_system_id: '22222222-2222-4222-8222-222222222222',
    source_instance: 'ur-a', org_id: '7', project_id: '86' }, label: 'Synthetic UR' };
  const binding = { collection_id: '33333333-3333-4333-8333-333333333333',
    producer_project_key: 'workspace:/synthetic/project', reporter,
    collection_revision: 2, scope_generation: '1', ur_target: urTarget };
  const sent = [], reports = [], futureReports = [];
  const installationId = `xiei_${'d'.repeat(32)}`;
  globalThis.fetch = async (url, request) => {
    assert.equal(request.headers.Authorization, `Bearer xib_${'a'.repeat(43)}`);
    if (url.pathname.endsWith('/config')) return Response.json({ version: 'xinsight.work-event-ingest-config.v1', items: [binding],
      ...(managed ? {managed_renewal:true,installation_id:installationId,
        credential_expires_at:new Date(Date.now()+60*86400000).toISOString()} : {}) });
    if (managed && url.pathname.endsWith('/future-candidates')) {
      futureReports.push(JSON.parse(request.body));
      return Response.json({schema_version:'xinsight.local-work-install-response.v1',action:'future_candidates',state:'approved',
        invitation:{invitation_id:installationId,state:'approved'}});
    }
    if (managed && url.pathname.endsWith('/status')) {
      reports.push(JSON.parse(request.body));
      // A diagnostic endpoint failure must not block metadata delivery.
      return Response.json({code:'temporarily_unavailable'},{status:503});
    }
    assert.equal(url.pathname, '/api/v1/work-event-ingest/items');
    const item = JSON.parse(request.body);
    const document = canonicalWorkEventJSON(item.document);
    sent.push(item);
    return Response.json({ kind: item.kind, reporter, document_ref: item.kind === 'event' ?
      item.document.event_ref : item.document.relation_ref,
    ...(item.kind === 'event' ? { revision: item.document.revision } : {}),
    sha256: `sha256:${createHash('sha256').update(document).digest('hex')}`, outcome: 'accepted' }, { status: 201 });
  };
  const hooks = new Map();
  const workerHooks = new Map();
  let service;
  let provider;
  let workerProvider;
  const models = [{ id: 'synthetic-fast', name: 'Synthetic Fast', reasoning: false, input: ['text'],
    cost: { input: 1, output: 2, cacheRead: 0.1, cacheWrite: 0.2 }, contextWindow: 128000, maxTokens: 4096 },
  { id: 'synthetic-reasoning', name: 'Synthetic Reasoning', reasoning: true, input: ['text', 'image'],
    cost: { input: 3, output: 8, cacheRead: 0.3, cacheWrite: 0.4 }, contextWindow: 256000, maxTokens: 8192 }];
  const hostConfig = {agents:{entries:{'xi-acceptance':{name:'Acceptance Agent'},personal:{name:'Private Agent'}}},
    models:{providers:{'unirouter-existing':{baseUrl:'https://ur.example.test/v1',api:'openai-completions',apiKey:{source:'env',provider:'default',id:'EXISTING_KEY'},models}}}};
  const pluginConfig = { origin: 'https://xi.example.test', tokenFile, outboxPath: join(dir, 'outbox.sqlite'),
    ...(managed ? {installationId,selectedAgentId:'xi-acceptance',managedProvider:{baseUrl:'https://ur.example.test/v1'}} : {provider: { enabled: true, baseUrl: 'https://ur.example.test/v1', models }}) };
  plugin.register({ pluginConfig, config: hostConfig,
  logger: { warn() {} }, on: (name, callback) => hooks.set(name, callback),
  registerService: (value) => { assert.equal(service, undefined); service = value; },
  registerProvider: (value) => { assert.equal(provider, undefined); provider = value; } });
  plugin.register({ pluginConfig, config: hostConfig,
    logger: { warn() {} }, on: (name, callback) => workerHooks.set(name, callback),
    registerService() {}, registerProvider: (value) => { workerProvider = value; } });
  try {
    if (managed) { assert.equal(workerProvider.catalog,undefined); assert.deepEqual(workerProvider.hookAliases,['unirouter-existing']); }
    else { const catalog = await workerProvider.catalog.run({}); assert.deepEqual(catalog.provider.models,models); }
    service.start();
    for (let i = 0; i < 350 && sent.length === 0; i++) {
      if (i === 0) await new Promise((resolve) => setTimeout(resolve, 20));
      if (i === 1) workerHooks.get('model_call_started')({ runId: 'run-a', callId: 'call-a' },
        { runId: 'run-a', workspaceDir: '/synthetic/project' });
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
    assert.equal(sent.length, 1);
    let originalCallback = 0;
    const stream = { marker: 'original-stream' };
    const wrapped = workerProvider.wrapStreamFn({ workspaceDir: '/synthetic/project',
      config:hostConfig,provider:'unirouter-existing',model:{baseUrl:'https://ur.example.test/v1'},
      streamFn: async (_model, _context, options) => {
        await options.onResponse({ status: 200, headers: { 'X-Request-ID': `op_${'b'.repeat(32)}` } }, {});
        return stream;
      } });
    const result = await wrapped({}, {}, { requestId: 'call-a', onResponse: async () => { originalCallback++; } });
    assert.equal(result, stream);
    assert.equal(originalCallback, 1);
    for (let i = 0; i < 350 && sent.length < 2; i++) await new Promise((resolve) => setTimeout(resolve, 20));
    assert.equal(sent.length, 2);
    const event = sent[0].document;
    const relation = sent[1].document;
    assert.equal(relation.event_ref, event.event_ref);
    assert.equal(relation.event_hash, `sha256:${createHash('sha256').update(canonicalWorkEventJSON(event)).digest('hex')}`);
    assert.equal(relation.ur_request_id, `op_${'b'.repeat(32)}`);
    if (managed) {
      assert.equal(reports.length,1,'failed diagnostic report is bounded by the normal refresh period');
      assert.ok(reports[0].status.gaps.some(item=>item.code==='body_unavailable'));
      assert.deepEqual(Object.keys(reports[0]),['status']);
      assert.deepEqual(futureReports,[{projects:[{project_key:'agent:xi-acceptance',label:'Acceptance Agent'}]}]);
      hostConfig.agents.entries['later-personal']={name:'Later Private Agent'};
      await service.stop(); service.start();
      for (let i=0; i<100 && reports.length<2; i++) await new Promise(resolve=>setTimeout(resolve,20));
      assert.equal(reports.length,2,'restart must perform another managed refresh');
      assert.equal(futureReports.length,1,'new unselected Agents must not be reported');
    } else assert.equal(reports.length,0);
  } finally {
    await service.stop();
    globalThis.fetch = priorFetch;
    if (priorToken === undefined) delete process.env.XINSIGHT_WORK_EVENT_TOKEN;
    else process.env.XINSIGHT_WORK_EVENT_TOKEN = priorToken;
    rmSync(dir, { recursive: true, force: true });
  }
});

test('provider catalog rejects an invented or incomplete model definition', async () => {
  let provider;
  plugin.register({ pluginConfig: { provider: { enabled: true, baseUrl: 'https://ur.example.test/v1',
    models: [{ id: 'unknown', name: 'Unknown' }] } },
  on() {}, registerService() {}, registerProvider: item => { provider = item; }, logger: { warn() {} } });
  await assert.rejects(provider.catalog.run({}), /invalid XI observed provider configuration/);
});

test('service tick refreshes the shared body policy before live tool hooks cross its 60-second expiry', async (t) => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), 'xi-work-policy-refresh-')));
  const tokenFile = join(dir, 'ingest.key'), bodyKeyFile = join(dir, 'body.key');
  const outboxPath = join(dir, 'events.sqlite');
  createPrivateFile(tokenFile, Buffer.from(`xib_${'a'.repeat(43)}`));
  createPrivateFile(bodyKeyFile, Buffer.from('a'.repeat(64)));
  const binding = { collection_id: '33333333-3333-4333-8333-333333333333',
    producer_project_key: 'agent:xi-acceptance', collection_revision: 2, scope_generation: '1',
    reporter: { source_system_id: '11111111-1111-4111-8111-111111111111',
      source_instance: 'agent-a', org_id: '7', project_id: '9' },
    body_capture: { mode: 'full_content', policy_version: 'xi:default:1', max_item_bytes: 1024,
      retention_seconds: 86400 } };
  const config = { origin: 'https://xi.example.test', tokenFile, bodyKeyFile,
    outboxPath, bodyOutboxPath: join(dir, 'bodies.sqlite') };
  const priorFetch = globalThis.fetch;
  t.mock.timers.enable({ apis: ['Date', 'setTimeout'] });
  let configFetches = 0, completedConfigFetches = 0, service, workerService;
  const hooks = new Map();
  globalThis.fetch = async (url) => {
    if (url.pathname.endsWith('/config')) {
      configFetches++;
      if (configFetches > 1) await new Promise(resolve => setTimeout(resolve, 5000));
      completedConfigFetches++;
      return Response.json({ version: 'xinsight.work-event-ingest-config.v1', items: [binding] });
    }
    throw new Error(`unexpected request: ${url.pathname}`);
  };
  const host = { pluginConfig: config, config: {}, logger: { warn() {} }, registerProvider() {} };
  plugin.register({ ...host, on() {}, registerService: value => { service = value; } });
  plugin.register({ ...host, on: (name, hook) => hooks.set(name, hook),
    registerService: value => { workerService = value; } });
  const reader = new WorkEventOutbox(outboxPath);
  const settle = async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
    await new Promise(resolve => setImmediate(resolve));
  };
  const advanceTo = async (target) => {
    while (Date.now() + 1000 <= target) { t.mock.timers.tick(1000); await settle(); }
    if (Date.now() < target) { t.mock.timers.tick(target - Date.now()); await settle(); }
  };
  const observeTool = (id) => {
    const context = { runId: 'same-run', agentId: 'xi-acceptance', workspaceDir: '/synthetic/project' };
    const event = { runId: 'same-run', toolCallId: id, toolName: 'read', params: { path: 'source.txt' } };
    hooks.get('before_tool_call')(event, context);
    hooks.get('after_tool_call')({ ...event, result: { details: { kind: 'text', content: 'source' } } }, context);
  };
  try {
    service.start();
    await settle();
    assert.equal(reader.bindings()[0]?.body_capture?.mode, 'full_content');
    await advanceTo(59000);
    observeTool('before-expiry');
    // An OpenClaw hook can arrive after the deadline but before a due tick's
    // network refresh completes. Real workers read the shared SQLite marker.
    t.mock.timers.setTime(60001);
    observeTool('at-expiry');
    const states = reader.db.prepare("SELECT document FROM items WHERE kind='event'").all()
      .map(row => JSON.parse(row.document)).filter(row => row.subject_kind === 'tool_call')
      .map(row => [row.tool_call_id, row.action_kind, row.content_state]);
    assert.deepEqual(states, [
      ['before-expiry', 'tool.invoked', 'referenced'],
      ['before-expiry', 'tool.completed', 'referenced'],
      ['at-expiry', 'tool.invoked', 'referenced'],
      ['at-expiry', 'tool.completed', 'referenced'],
    ]);
    assert.ok(configFetches >= 2, 'the actual service tick refreshed before expiry');
    assert.ok(completedConfigFetches >= 2, 'the serial tick completed a five-second official refresh');
    assert.equal(reader.gapStatus().some(item => item.code === 'body_policy_unconfirmed'), false);
  } finally {
    await service.stop();
    await workerService.stop();
    reader.close();
    globalThis.fetch = priorFetch;
    t.mock.timers.reset();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('service tick keeps failed refresh, disabled policy, and changed binding fail-closed', async (t) => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), 'xi-work-policy-denial-')));
  const tokenFile = join(dir, 'ingest.key'), bodyKeyFile = join(dir, 'body.key');
  const outboxPath = join(dir, 'events.sqlite');
  createPrivateFile(tokenFile, Buffer.from(`xib_${'a'.repeat(43)}`));
  createPrivateFile(bodyKeyFile, Buffer.from('a'.repeat(64)));
  const full = { collection_id: '33333333-3333-4333-8333-333333333333',
    producer_project_key: 'agent:xi-acceptance', collection_revision: 2, scope_generation: '1',
    reporter: { source_system_id: '11111111-1111-4111-8111-111111111111',
      source_instance: 'agent-a', org_id: '7', project_id: '9' },
    body_capture: { mode: 'full_content', policy_version: 'xi:default:1', max_item_bytes: 1024,
      retention_seconds: 86400 } };
  const config = { origin: 'https://xi.example.test', tokenFile, bodyKeyFile,
    outboxPath, bodyOutboxPath: join(dir, 'bodies.sqlite') };
  const priorFetch = globalThis.fetch;
  t.mock.timers.enable({ apis: ['Date', 'setTimeout'] });
  let service, workerService, configFetches = 0;
  const hooks = new Map();
  globalThis.fetch = async (url, request) => {
    if (url.pathname.endsWith('/config')) {
      configFetches++;
      if (configFetches >= 4) return Response.json({ code: 'unavailable' }, { status: 503 });
      const binding = configFetches === 1 ? full : configFetches === 2 ?
        { ...full, body_capture: { mode: 'disabled' } } : { ...full, collection_revision: 3 };
      return Response.json({ version: 'xinsight.work-event-ingest-config.v1', items: [binding] });
    }
    if (url.pathname.endsWith('/items')) {
      const item = JSON.parse(request.body), document = canonicalWorkEventJSON(item.document);
      return Response.json({ kind: item.kind, reporter: item.document.reporter,
        document_ref: item.kind === 'event' ? item.document.event_ref : item.document.relation_ref,
        ...(item.kind === 'event' ? { revision: item.document.revision } : {}),
        sha256: `sha256:${createHash('sha256').update(document).digest('hex')}`, outcome: 'accepted' },
      { status: 201 });
    }
    throw new Error(`unexpected request: ${url.pathname}`);
  };
  const host = { pluginConfig: config, config: {}, logger: { warn() {} }, registerProvider() {} };
  plugin.register({ ...host, on() {}, registerService: value => { service = value; } });
  plugin.register({ ...host, on: (name, hook) => hooks.set(name, hook),
    registerService: value => { workerService = value; } });
  const reader = new WorkEventOutbox(outboxPath);
  const settle = async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
    await new Promise(resolve => setImmediate(resolve));
  };
  const advanceTo = async (target) => {
    while (Date.now() + 1000 <= target) { t.mock.timers.tick(1000); await settle(); }
    if (Date.now() < target) { t.mock.timers.tick(target - Date.now()); await settle(); }
  };
  const observeTool = (id, runId = 'same-run') => {
    const context = { runId, agentId: 'xi-acceptance', workspaceDir: '/synthetic/project' };
    const event = { runId, toolCallId: id, toolName: 'read', params: { path: 'source.txt' } };
    hooks.get('before_tool_call')(event, context);
    hooks.get('after_tool_call')({ ...event, result: { details: { kind: 'text', content: 'source' } } }, context);
  };
  const toolStates = id => reader.db.prepare("SELECT document FROM items WHERE kind='event'").all()
    .map(row => JSON.parse(row.document)).filter(row => row.tool_call_id === id && row.subject_kind === 'tool_call')
    .map(row => row.content_state);
  try {
    service.start();
    await settle();
    await advanceTo(31000);
    assert.equal(reader.bindings()[0].body_capture.mode, 'disabled');
    observeTool('disabled');
    assert.deepEqual(toolStates('disabled'), ['not_captured', 'not_captured']);
    assert.equal(reader.gapStatus().some(item => item.code === 'body_policy_unconfirmed'), false);

    await advanceTo(70000);
    assert.equal(reader.bindings()[0].collection_revision, 3);
    observeTool('changed');
    assert.deepEqual(toolStates('changed'), []);
    assert.ok(reader.gapStatus().some(item => item.code === 'run_project_changed'));

    await advanceTo(140000);
    assert.equal(reader.bindings()[0].body_capture, undefined);
    observeTool('expired', 'new-run');
    assert.deepEqual(toolStates('expired'), ['not_captured', 'not_captured']);
    assert.ok(reader.gapStatus().some(item => item.code === 'body_policy_unconfirmed'));
  } finally {
    await service.stop();
    await workerService.stop();
    reader.close();
    globalThis.fetch = priorFetch;
    t.mock.timers.reset();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('managed tick publishes a confirmed binding before future-agent reporting completes', async () => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), 'xi-work-managed-policy-')));
  const tokenFile = join(dir, 'ingest.key'), bodyKeyFile = join(dir, 'body.key');
  const outboxPath = join(dir, 'events.sqlite');
  createPrivateFile(tokenFile, Buffer.from(`xib_${'a'.repeat(43)}`));
  createPrivateFile(bodyKeyFile, Buffer.from('a'.repeat(64)));
  const installationId = `xiei_${'d'.repeat(32)}`;
  const binding = { collection_id: '33333333-3333-4333-8333-333333333333',
    producer_project_key: 'agent:xi-acceptance', collection_revision: 2, scope_generation: '1',
    reporter: { source_system_id: '11111111-1111-4111-8111-111111111111',
      source_instance: 'agent-a', org_id: '7', project_id: '9' },
    body_capture: { mode: 'full_content', policy_version: 'xi:default:1', max_item_bytes: 1024,
      retention_seconds: 86400 } };
  let releaseReport, releaseFollowup, reportStarted = false, service, workerService;
  let configFetches = 0, reportedProjects;
  const pendingReport = new Promise(resolve => { releaseReport = resolve; });
  const pendingFollowup = new Promise(resolve => { releaseFollowup = resolve; });
  const priorFetch = globalThis.fetch;
  const configResponse = () => Response.json({ version: 'xinsight.work-event-ingest-config.v1',
    items: [binding], managed_renewal: true, installation_id: installationId,
    credential_expires_at: new Date(Date.now() + 60 * 86400000).toISOString() });
  globalThis.fetch = async (url, request) => {
    if (url.pathname.endsWith('/config')) {
      configFetches++;
      return configFetches === 2 ? pendingFollowup : configResponse();
    }
    if (url.pathname.endsWith('/future-candidates')) {
      reportStarted = true;
      reportedProjects = JSON.parse(request.body).projects;
      return pendingReport;
    }
    if (url.pathname.endsWith('/status')) return Response.json({
      schema_version: 'xinsight.local-work-install-response.v1', action: 'status', state: 'reported', invitation_id: installationId });
    if (url.pathname.endsWith('/items')) {
      const item = JSON.parse(request.body), document = canonicalWorkEventJSON(item.document);
      return Response.json({ kind: item.kind, reporter: item.document.reporter,
        document_ref: item.kind === 'event' ? item.document.event_ref : item.document.relation_ref,
        ...(item.kind === 'event' ? { revision: item.document.revision } : {}),
        sha256: `sha256:${createHash('sha256').update(document).digest('hex')}`, outcome: 'accepted' },
      { status: 201 });
    }
    if (url.pathname.endsWith('/contents')) return Response.json({ code: 'unavailable' }, { status: 503 });
    throw new Error(`unexpected request: ${url.pathname}`);
  };
  const hostConfig = { agents: { entries: { 'xi-acceptance': { name: 'Acceptance Agent' },
    'later-personal': { name: 'Later Agent' } } } };
  const pluginConfig = { origin: 'https://xi.example.test', tokenFile, bodyKeyFile,
    outboxPath, bodyOutboxPath: join(dir, 'bodies.sqlite'), installationId };
  const host = { pluginConfig, config: hostConfig, logger: { warn() {} }, registerProvider() {} };
  plugin.register({ ...host, on() {}, registerService: value => { service = value; } });
  const hooks = new Map();
  plugin.register({ ...host, on: (name, hook) => hooks.set(name, hook),
    registerService: value => { workerService = value; } });
  const reader = new WorkEventOutbox(outboxPath);
  const observeTool = (id, agentId) => {
    const context = { runId: id, agentId, workspaceDir: '/synthetic/project' };
    const event = { runId: id, toolCallId: id, toolName: 'read', params: { path: 'source.txt' } };
    hooks.get('before_tool_call')(event, context);
    hooks.get('after_tool_call')({ ...event, result: { details: { kind: 'text', content: 'source' } } }, context);
  };
  const toolStates = id => reader.db.prepare("SELECT document FROM items WHERE kind='event'").all()
    .map(row => JSON.parse(row.document)).filter(row => row.tool_call_id === id && row.subject_kind === 'tool_call')
    .map(row => row.content_state);
  try {
    service.start();
    for (let i = 0; i < 20 && !reportStarted; i++) await new Promise(resolve => setImmediate(resolve));
    assert.equal(reportStarted, true);
    assert.equal(reader.bindings()[0]?.body_capture?.mode, 'full_content');
    assert.ok(reportedProjects.some(project => project.project_key === 'agent:later-personal'));
    observeTool('existing', 'xi-acceptance');
    assert.deepEqual(toolStates('existing'), ['referenced', 'referenced']);
    releaseReport(Response.json({ schema_version: 'xinsight.local-work-install-response.v1',
      action: 'future_candidates', state: 'approved',
      invitation: { invitation_id: installationId, state: 'approved' } }));
    for (let i = 0; i < 20 && configFetches < 2; i++) await new Promise(resolve => setImmediate(resolve));
    assert.equal(configFetches, 2, 'candidate acknowledgement requires a follow-up official map');
    observeTool('unadmitted', 'later-personal');
    assert.deepEqual(toolStates('unadmitted'), []);
    assert.ok(reader.gapStatus().some(item => item.code === 'unbound_project'));
  } finally {
    releaseReport(Response.json({ schema_version: 'xinsight.local-work-install-response.v1',
      action: 'future_candidates', state: 'approved',
      invitation: { invitation_id: installationId, state: 'approved' } }));
    releaseFollowup(configResponse());
    await service.stop();
    await workerService.stop();
    reader.close();
    globalThis.fetch = priorFetch;
    rmSync(dir, { recursive: true, force: true });
  }
});
