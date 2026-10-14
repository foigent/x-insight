import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { ReviewBodyOutbox } from './body-outbox.mjs';
import { WorkEventOutbox } from './outbox.mjs';
import { deliverNextReviewBody } from './transport.mjs';

const digest = (body) => 'sha256:' + createHash('sha256').update(body).digest('hex');
const token = 'xib_' + 'A'.repeat(43);
const item = (body) => ({ observation_key: 'a'.repeat(64), collection_id: '33333333-3333-4333-8333-333333333333',
  expected_collection_revision: 2, scope_generation: '1', event_ref: 'oc:event', event_revision: 1,
  event_hash: digest(Buffer.from('event')), content_kind: 'model_input', content_ref: 'ocbody:one',
  byte_domain: 'openclaw.llm_input.canonical-json.v1', sha256: digest(body), policy_version: 'xi:default:1', body });
const bindings = [{ collection_id: item(Buffer.alloc(0)).collection_id, collection_revision: 2,
  scope_generation: '1', body_capture: { mode: 'full_content', policy_version: 'xi:default:1' } }];
const metadataOutbox = { observation: () => ({ documentRef: 'oc:event', documentHash: digest(Buffer.from('event')), state: 'acked' }) };

test('separate body queue encrypts bytes, survives restart, and uses exact receipt', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-body-queue-'));
  const path = join(dir, 'body.sqlite');
  const key = Buffer.alloc(32, 7);
  const secret = Buffer.from('synthetic plaintext only in memory');
  let queue;
  try {
    queue = new ReviewBodyOutbox(path, key);
    assert.deepEqual(queue.enqueue(item(secret)), { duplicate: false });
    assert.doesNotMatch(readFileSync(path).toString('latin1'), /synthetic plaintext only in memory/);
    queue.close();
    queue = new ReviewBodyOutbox(path, key);
    assert.deepEqual(queue.next().body, secret);
    const result = await deliverNextReviewBody(queue, { origin: 'https://xi.example/', token, bindings, metadataOutbox,
      fetchImpl: async (_url, request) => {
        assert.deepEqual(Buffer.from(request.body), secret);
        const metadata = JSON.parse(Buffer.from(request.headers['X-XI-Body-Metadata'], 'base64url').toString());
        assert.equal(metadata.event_ref, 'oc:event');
        return new Response(JSON.stringify({ content_ref: 'ocbody:one', sha256: digest(secret),
          plain_bytes: secret.length, expires_at: '2026-10-29T00:00:00Z', outcome: 'accepted' }), { status: 201 });
      } });
    assert.equal(result.state, 'acknowledged');
    assert.equal(queue.next(), null);
  } finally { queue?.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('body queue preserves fixed identity, caps rows, and blocks expired or changed policy', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-body-bound-'));
  let now = 100_000;
  const queue = new ReviewBodyOutbox(join(dir, 'body.sqlite'), Buffer.alloc(32, 8),
    { maxRows: 1, maxAgeMs: 60_000, clock: () => now });
  const metadata = new WorkEventOutbox(join(dir, 'events.sqlite'));
  try {
    const first = item(Buffer.from('one'));
    queue.enqueue(first);
    assert.deepEqual(queue.enqueue(first), { duplicate: true });
    assert.throws(() => queue.enqueue({ ...first, body: Buffer.from('two'), sha256: digest(Buffer.from('two')) }), /body_identity_conflict/);
    assert.throws(() => queue.enqueue({ ...first, content_ref: 'ocbody:two' }), /body_queue_full/);
    const mismatch = await deliverNextReviewBody(queue, { origin: 'https://xi.example/', token,
      bindings: [{ ...bindings[0], body_capture: { mode: 'disabled' } }], metadataOutbox,
      fetchImpl: () => { throw new Error('must not send'); } });
    assert.deepEqual(mismatch, { state: 'blocked', code: 'body_policy_changed' });
    assert.equal(queue.next(), null);
    const blocked = queue.nextBlocked();
    assert.equal(blocked.reason, 'body_policy_changed');
    metadata.recordGap(blocked.reason);
    queue.releaseBlocked(blocked.contentRef);
    assert.equal(metadata.gapStatus()[0].code, 'body_policy_changed');
    assert.deepEqual(queue.enqueue({ ...first, content_ref: 'ocbody:two' }), { duplicate: false });
  } finally { metadata.close(); queue.close(); rmSync(dir, { recursive: true, force: true }); }

  const nextDir = mkdtempSync(join(tmpdir(), 'xi-body-expire-'));
  const expiring = new ReviewBodyOutbox(join(nextDir, 'body.sqlite'), Buffer.alloc(32, 9),
    { maxAgeMs: 60_000, clock: () => now });
  try {
    expiring.enqueue(item(Buffer.from('old')));
    now += 60_000;
    assert.deepEqual(expiring.next(), { expired: true, content_ref: 'ocbody:one' });
    assert.equal(expiring.next(), null);
  } finally { expiring.close(); rmSync(nextDir, { recursive: true, force: true }); }
});

test('HTTP 413 is a terminal body-size gap, distinct from an invalid item', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-body-413-'));
  const queue = new ReviewBodyOutbox(join(dir, 'body.sqlite'), Buffer.alloc(32, 4));
  try {
    queue.enqueue(item(Buffer.from('synthetic model input')));
    const result = await deliverNextReviewBody(queue, { origin: 'https://xi.example/', token,
      bindings, metadataOutbox,
      fetchImpl: async () => new Response(null, { status: 413 }) });
    assert.deepEqual(result, { state: 'blocked', code: 'body_too_large' });
    const blocked = queue.nextBlocked();
    assert.equal(blocked.contentRef, 'ocbody:one');
    assert.equal(blocked.reason, 'body_too_large');
    assert.equal(queue.pendingCount(), 0);
  } finally { queue.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('corrupt encrypted body is isolated and later queue items remain readable', () => {
  const dir = mkdtempSync(join(tmpdir(), 'xi-body-corrupt-'));
  const queue = new ReviewBodyOutbox(join(dir, 'body.sqlite'), Buffer.alloc(32, 6));
  try {
    queue.enqueue(item(Buffer.from('first')));
    queue.enqueue({ ...item(Buffer.from('second')), content_ref: 'ocbody:two' });
    queue.db.prepare("UPDATE bodies SET ciphertext=x'00000000000000000000000000000000' WHERE content_ref='ocbody:one'").run();
    assert.deepEqual(queue.next(), { blocked: true, content_ref: 'ocbody:one', reason: 'body_queue_integrity_failed' });
    assert.equal(queue.next().content_ref, 'ocbody:two');
  } finally { queue.close(); rmSync(dir, { recursive: true, force: true }); }
});
