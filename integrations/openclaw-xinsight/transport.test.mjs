import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { WorkEventOutbox, canonicalWorkEventJSON } from './outbox.mjs';
import { deliverNext, fetchWorkEventConfig, reportWorkEventProjects } from './transport.mjs';

const collectionId = '11111111-1111-4111-8111-111111111111';
const reporter = { source_system_id: collectionId, source_instance: 'agent-a', org_id: '7', project_id: '9' };
const document = canonicalWorkEventJSON({
  schema_version: 'xinsight.work-event.v1', reporter, event_ref: 'model-a', revision: 1,
  actor: { kind: 'agent', ref: 'agent-a' }, content_refs: [],
});
const item = { collectionId, expectedCollectionRevision: 2, scopeGeneration: '1', kind: 'event', document };
const options = { origin: 'https://xi.example.test', token: `xib_${'a'.repeat(43)}` };
const receipt = {
  kind: 'event', reporter, document_ref: 'model-a', revision: 1,
  sha256: `sha256:${createHash('sha256').update(document).digest('hex')}`,
  outcome: 'accepted',
};

async function withOutbox(action) {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-transport-'));
  let clock = 100000;
  const outbox = new WorkEventOutbox(join(dir, 'queue.sqlite'), { clock: () => clock });
  try { return await action(outbox, (ms) => { clock += ms; }); } finally { outbox.close(); rmSync(dir, { recursive: true, force: true }); }
}

test('network loss and invalid acknowledgement retain a committed item; exact ack removes it', async () => {
  await withOutbox(async (outbox, advance) => {
    outbox.enqueue(item);
    const lost = await deliverNext(outbox, { ...options, fetchImpl: async () => { throw new Error('lost'); } });
    assert.deepEqual(lost, { state: 'retry', code: 'transport_unavailable' });
    assert.equal(outbox.status().rows, 1);
    advance(1000);
    const invalid = await deliverNext(outbox, { ...options, fetchImpl: async () => Response.json({ ...receipt, sha256: 'wrong' }, { status: 201 }) });
    assert.deepEqual(invalid, { state: 'retry', code: 'invalid_ack' });
    advance(2000);
    assert.equal(outbox.next().attempts, 2);
    const accepted = await deliverNext(outbox, { ...options, fetchImpl: async (url, request) => {
      assert.equal(url.pathname, '/api/v1/work-event-ingest/items');
      assert.equal(request.headers.Authorization, `Bearer ${options.token}`);
      const body = JSON.parse(request.body);
      assert.equal(canonicalWorkEventJSON(body.document), document);
      assert.equal(body.expected_collection_revision, 2);
      return Response.json(receipt, { status: 201 });
    } });
    assert.deepEqual(accepted, { state: 'acknowledged', outcome: 'accepted' });
    assert.equal(outbox.status().rows, 0);
  });
});

test('missing predecessor retries, identical replay acks, and scope loss blocks visibly', async () => {
  await withOutbox(async (outbox, advance) => {
    outbox.enqueue(item);
    const pending = await deliverNext(outbox, { ...options, fetchImpl: async () => Response.json({ code: 'work_event_dependency_pending' }, { status: 409 }) });
    assert.deepEqual(pending, { state: 'retry', code: 'dependency_pending' });
    assert.equal(outbox.next(), null);
    const predecessorDocument = canonicalWorkEventJSON({ ...JSON.parse(document), event_ref: 'predecessor' });
    outbox.enqueue({ ...item, document: predecessorDocument });
    const preceding = await deliverNext(outbox, { ...options, fetchImpl: async () => Response.json({
      ...receipt, document_ref: 'predecessor',
      sha256: `sha256:${createHash('sha256').update(predecessorDocument).digest('hex')}`,
    }, { status: 201 }) });
    assert.deepEqual(preceding, { state: 'acknowledged', outcome: 'accepted' });
    advance(250);
    const replay = await deliverNext(outbox, { ...options, fetchImpl: async () => Response.json({ ...receipt, outcome: 'identical_replay' }, { status: 200 }) });
    assert.deepEqual(replay, { state: 'acknowledged', outcome: 'identical_replay' });
    outbox.enqueue({ ...item, document: canonicalWorkEventJSON({ ...JSON.parse(document), event_ref: 'model-b' }) });
    const blocked = await deliverNext(outbox, { ...options, fetchImpl: async () => Response.json({ code: 'work_event_scope_changed' }, { status: 403 }) });
    assert.deepEqual(blocked, { state: 'blocked', code: 'scope_changed' });
    assert.equal(outbox.status().blocked, 1);
    assert.equal(outbox.next(), null);
  });
});

test('configuration discovery accepts only an exact HTTPS-bound project map', async () => {
  const binding = { collection_id: collectionId, producer_project_key: 'workspace:/synthetic/project',
    reporter, collection_revision: 2, scope_generation: '1' };
  const config = { version: 'xinsight.work-event-ingest-config.v1', items: [binding] };
  const items = await fetchWorkEventConfig({ ...options, fetchImpl: async (url, request) => {
    assert.equal(url.pathname, '/api/v1/work-event-ingest/config');
    assert.equal(request.headers.Authorization, `Bearer ${options.token}`);
    return Response.json(config);
  } });
  assert.deepEqual(items, [binding]);
  await assert.rejects(fetchWorkEventConfig({ ...options,
    fetchImpl: async () => Response.json({ ...config, items: [binding, binding] }) }), /invalid_work_event_config/);
  await assert.rejects(fetchWorkEventConfig({ ...options, origin: 'http://xi.example.test',
    fetchImpl: async () => Response.json(config) }), /invalid work-event transport/);
});


test('future agent report stays within the managed installation and never treats acknowledgement as admission',async()=>{
 const installationId=`xiei_${'b'.repeat(32)}`,projects=[{project_key:'agent:main',label:'main'},{project_key:'agent:writer',label:'Writer'}];
 const response={schema_version:'xinsight.local-work-install-response.v1',action:'future_candidates',state:'approved',
   invitation:{invitation_id:installationId,state:'approved',bindings:[{unverified:'never consumed'}]}};
 const result=await reportWorkEventProjects({...options,installationId,projects,fetchImpl:async(url,request)=>{
   assert.equal(url.pathname,'/api/v1/work-event-install/future-candidates');
   assert.equal(request.headers.Authorization,`Bearer ${options.token}`);
   assert.equal(request.credentials,'omit');assert.equal(request.redirect,'error');
   assert.deepEqual(JSON.parse(request.body),{projects});
   return Response.json(response);
 }});
 assert.equal(result,undefined,'only a separately authenticated config read can grant bindings');
 for(const state of ['conflict','scope_changed']) await assert.rejects(reportWorkEventProjects({...options,installationId,projects,
   fetchImpl:async()=>Response.json({...response,state})}),new RegExp(`future_candidates_${state}`));
 await assert.rejects(reportWorkEventProjects({...options,installationId,projects,fetchImpl:async()=>Response.json({...response,
   invitation:{...response.invitation,invitation_id:`xiei_${'c'.repeat(32)}`}})}),/future_candidates_unconfirmed/);
 await assert.rejects(reportWorkEventProjects({...options,installationId,projects,fetchImpl:async()=>Response.json(response,{status:403})}),/future_candidates_unconfirmed/);
 await assert.rejects(reportWorkEventProjects({...options,installationId,projects,fetchImpl:async()=>new Response('x'.repeat(65537))}),/oversize response/);
});
