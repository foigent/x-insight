import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { WorkEventOutbox, canonicalWorkEventJSON } from './outbox.mjs';

const collectionId = '11111111-1111-4111-8111-111111111111';
const event = canonicalWorkEventJSON({
  schema_version: 'xinsight.work-event.v1',
  reporter: { source_system_id: collectionId, source_instance: 'agent-local', org_id: '7', project_id: '9' },
  event_ref: 'event-a', revision: 1,
  actor: { kind: 'agent', ref: 'agent-1' },
  content_refs: [],
});
const item = { collectionId, expectedCollectionRevision: 2, scopeGeneration: '1', kind: 'event', document: event };

test('commit survives restart, replay is idempotent, and acknowledgement removes only pending', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-outbox-'));
  try {
    const path = join(dir, 'queue.sqlite');
    let clock = 100000;
    const first = new WorkEventOutbox(path, { clock: () => clock });
    const queued = first.enqueue(item);
    assert.equal(queued.duplicate, false);
    assert.equal(first.enqueue(item).duplicate, true);
    assert.equal(first.status().rows, 1);
    first.close();

    const recovered = new WorkEventOutbox(path, { clock: () => clock });
    assert.equal(recovered.next().id, queued.id);
    assert.equal(recovered.retry(queued.id, 'receiver_unavailable'), true);
    assert.equal(recovered.next(), null);
    assert.equal(recovered.nextDueAt(), clock + 1000);
    clock += 1000;
    assert.equal(recovered.next().attempts, 1);
    assert.equal(recovered.acknowledge(queued.id), true);
    assert.equal(recovered.acknowledge(queued.id), false);
    assert.equal(recovered.status().rows, 0);
    recovered.close();
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('capacity and terminal block are visible; body-shaped documents are rejected', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-outbox-'));
  try {
    const outbox = new WorkEventOutbox(join(dir, 'queue.sqlite'), { maxRows: 1, maxPayloadBytes: 8192 });
    const queued = outbox.enqueue(item);
    assert.throws(() => outbox.enqueue({ ...item, document: canonicalWorkEventJSON({ ...JSON.parse(event), body: 'secret' }) }), /metadata/);
    assert.throws(() => outbox.enqueue({ ...item, document: canonicalWorkEventJSON({ ...JSON.parse(event), reporter: { ...JSON.parse(event).reporter, body: 'secret' } }) }), /metadata/);
    assert.throws(() => outbox.enqueue({ ...item, document: canonicalWorkEventJSON({ ...JSON.parse(event), event_ref: 'other' }) }), /outbox_capacity_exceeded/);
    assert.throws(() => outbox.enqueue({ ...item, document: JSON.stringify(JSON.parse(event)) + ' ' }), /canonical/);
    assert.equal(outbox.block(queued.id, 'scope_revoked'), true);
    assert.equal(outbox.next(), null);
    assert.deepEqual({ ...outbox.status() }, { rows: 1, bytes: Buffer.byteLength(event), pending: 0, blocked: 1 });
    assert.equal(outbox.acknowledge(queued.id), false);
    outbox.recordGap('project_unmapped');
    assert.equal(outbox.gapStatus()[0].occurrences, 1);
    outbox.close();
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});


test('body policy freshness is shared with hook workers and expires without removing event identity', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-work-policy-freshness-'));
  let time = 100000;
  const first = new WorkEventOutbox(join(dir, 'queue.sqlite'), {clock:()=>time});
  const worker = new WorkEventOutbox(join(dir, 'queue.sqlite'), {clock:()=>time});
  const binding = {collection_id:collectionId, producer_project_key:'agent:main',
    reporter:JSON.parse(event).reporter,collection_revision:1,scope_generation:'1',
    body_capture:{mode:'full_content',policy_version:'xi:default:1',max_item_bytes:1024}};
  try {
    first.replaceBindings([binding]);
    assert.deepEqual(worker.bindings()[0].body_capture,binding.body_capture);
    time += 60000;
    assert.equal(worker.bindings()[0].body_capture,undefined);
    assert.equal(worker.bindings()[0].collection_id,collectionId);
    first.replaceBindings([binding]);
    first.invalidateBodyPolicy();
    assert.equal(worker.bindings()[0].body_capture,undefined);
    first.replaceBindings([{...binding,body_capture:{mode:'disabled'}}]);
    assert.deepEqual(worker.bindings()[0].body_capture,{mode:'disabled'});
    time -= 1;
    assert.equal(worker.bindings()[0].body_capture,undefined,'clock rollback cannot extend an old policy');
  } finally {worker.close();first.close();rmSync(dir,{recursive:true,force:true});}
});
