import { createCipheriv, createDecipheriv, createHash, randomBytes } from 'node:crypto';
import { closeSync, existsSync, lstatSync, mkdirSync, openSync } from 'node:fs';
import { dirname, isAbsolute } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { canonicalWorkEventJSON } from './outbox.mjs';

const digestOf = (bytes) => 'sha256:' + createHash('sha256').update(bytes).digest('hex');
const safeRef = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
const digestPattern = /^sha256:[a-f0-9]{64}$/;

function privatePath(path) {
  if (!isAbsolute(path)) throw new TypeError('body outbox path must be absolute');
  const parent = dirname(path);
  mkdirSync(parent, { recursive: true, mode: 0o700 });
  const stat = lstatSync(parent);
  if (!stat.isDirectory() || stat.isSymbolicLink() || stat.uid !== process.getuid() || (stat.mode & 0o077)) throw new Error('body outbox directory must be private');
  if (existsSync(path)) {
    const file = lstatSync(path);
    if (!file.isFile() || file.isSymbolicLink() || file.uid !== process.getuid() || (file.mode & 0o077)) throw new Error('body outbox file must be private');
  } else closeSync(openSync(path, 'wx+', 0o600));
}

function validate(item) {
  if (!item || typeof item !== 'object' || !Buffer.isBuffer(item.body) || item.body.length > (32 << 20) ||
      !/^[a-f0-9]{64}$/.test(item.observation_key) || typeof item.collection_id !== 'string' || typeof item.scope_generation !== 'string' ||
      !Number.isSafeInteger(item.expected_collection_revision) || item.expected_collection_revision < 1 ||
      !safeRef.test(item.event_ref) || !safeRef.test(item.content_ref) ||
      !Number.isSafeInteger(item.event_revision) || item.event_revision < 1 ||
      !digestPattern.test(item.event_hash) || !digestPattern.test(item.sha256) ||
      item.sha256 !== digestOf(item.body) || !safeRef.test(item.content_kind) ||
      !safeRef.test(item.byte_domain) || !/^xi:(?:default:[1-9][0-9]{0,18}|xipe_[a-f0-9]{32}:[1-9][0-9]{0,18}:default:[1-9][0-9]{0,18})$/.test(item.policy_version)) {
    throw new TypeError('invalid body outbox item');
  }
  const { body, ...metadata } = item;
  return canonicalWorkEventJSON(metadata);
}

// Separate from the metadata outbox. A deployment-supplied installation key
// is required; absence means metadata-only operation and an explicit gap.
export class ReviewBodyOutbox {
  constructor(path, key, { maxRows = 256, maxBytes = 128 << 20, maxAgeMs = 24 * 60 * 60 * 1000, clock = Date.now } = {}) {
    if (!Buffer.isBuffer(key) || key.length !== 32 || !Number.isSafeInteger(maxRows) || maxRows < 1 ||
        !Number.isSafeInteger(maxBytes) || maxBytes < 32 || maxBytes > 1024 * 1024 * 1024 ||
        !Number.isSafeInteger(maxAgeMs) || maxAgeMs < 60_000 || maxAgeMs > 7 * 86400000) throw new TypeError('invalid body outbox configuration');
    privatePath(path);
    this.key = Buffer.from(key);
    this.maxRows = maxRows; this.maxBytes = maxBytes; this.maxAgeMs = maxAgeMs; this.clock = clock;
    this.db = new DatabaseSync(path);
    const pageCap = Math.ceil((maxBytes * 2 + maxRows * 2048 + 1024 * 1024) / 4096);
    this.db.exec(`PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
      PRAGMA auto_vacuum=FULL; PRAGMA max_page_count=${pageCap};
      CREATE TABLE IF NOT EXISTS bodies (
        content_ref TEXT PRIMARY KEY, metadata TEXT NOT NULL, nonce BLOB NOT NULL,
        ciphertext BLOB NOT NULL, payload_bytes INTEGER NOT NULL,
        created_ms INTEGER NOT NULL, next_attempt_ms INTEGER NOT NULL,
        attempts INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL CHECK(state IN ('pending','blocked')),
        reason TEXT NOT NULL DEFAULT ''
      );
      CREATE INDEX IF NOT EXISTS bodies_next ON bodies(state,next_attempt_ms,created_ms,content_ref);`);
  }

  close() { this.db.close(); this.key.fill(0); }

  enqueue(item) {
    const metadata = validate(item);
    const existing = this.db.prepare('SELECT metadata FROM bodies WHERE content_ref=?').get(item.content_ref);
    if (existing) {
      if (existing.metadata !== metadata) throw new Error('body_identity_conflict');
      return { duplicate: true };
    }
    const total = this.db.prepare('SELECT count(*) AS rows,coalesce(sum(payload_bytes),0) AS bytes FROM bodies').get();
    if (total.rows >= this.maxRows || total.bytes + item.body.length + 16 > this.maxBytes) throw new Error('body_queue_full');
    const nonce = randomBytes(12);
    const cipher = createCipheriv('aes-256-gcm', this.key, nonce);
    cipher.setAAD(Buffer.from(metadata));
    const ciphertext = Buffer.concat([cipher.update(item.body), cipher.final(), cipher.getAuthTag()]);
    this.db.prepare(`INSERT INTO bodies(content_ref,metadata,nonce,ciphertext,payload_bytes,created_ms,next_attempt_ms,state)
      VALUES(?,?,?,?,?, ?,0,'pending')`).run(item.content_ref, metadata, nonce, ciphertext, ciphertext.length, this.clock());
    return { duplicate: false };
  }

  next() {
    const row = this.db.prepare(`SELECT * FROM bodies WHERE state='pending' AND next_attempt_ms<=?
      ORDER BY created_ms,content_ref LIMIT 1`).get(this.clock());
    if (!row) return null;
    if (row.created_ms + this.maxAgeMs <= this.clock()) {
      this.block(row.content_ref, 'body_delivery_expired');
      return { expired: true, content_ref: row.content_ref };
    }
    try {
      const metadata = JSON.parse(row.metadata);
      const encrypted = Buffer.from(row.ciphertext);
      if (metadata.content_ref !== row.content_ref || encrypted.length < 16) throw new Error('invalid sealed body');
      const decipher = createDecipheriv('aes-256-gcm', this.key, row.nonce);
      decipher.setAAD(Buffer.from(row.metadata));
      decipher.setAuthTag(encrypted.subarray(encrypted.length - 16));
      const body = Buffer.concat([decipher.update(encrypted.subarray(0, encrypted.length - 16)), decipher.final()]);
      if (digestOf(body) !== metadata.sha256) throw new Error('invalid body digest');
      return { ...metadata, body };
    } catch {
      this.block(row.content_ref, 'body_queue_integrity_failed');
      return { blocked: true, content_ref: row.content_ref, reason: 'body_queue_integrity_failed' };
    }
  }

  acknowledge(ref) { this.db.prepare('DELETE FROM bodies WHERE content_ref=?').run(ref); }
  block(ref, reason) {
    this.db.prepare(`UPDATE bodies SET state='blocked',reason=?,ciphertext=x'',payload_bytes=0 WHERE content_ref=? AND state='pending'`).run(reason, ref);
  }
  nextBlocked() {
    return this.db.prepare("SELECT content_ref AS contentRef,reason FROM bodies WHERE state='blocked' ORDER BY created_ms,content_ref LIMIT 1").get() ?? null;
  }
  releaseBlocked(ref) {
    return this.db.prepare("DELETE FROM bodies WHERE content_ref=? AND state='blocked'").run(ref).changes === 1;
  }
  retry(ref, reason) {
    const row = this.db.prepare("SELECT attempts FROM bodies WHERE content_ref=? AND state='pending'").get(ref);
    if (!row) return;
    const dependency = reason === 'body_metadata_pending';
    const attempts = dependency ? row.attempts : row.attempts + 1;
    const delay = dependency ? 250 : Math.min(60_000, 1000 * 2 ** Math.min(attempts, 6));
    this.db.prepare('UPDATE bodies SET attempts=?,next_attempt_ms=?,reason=? WHERE content_ref=? AND state=\'pending\'')
      .run(attempts, this.clock() + delay, reason, ref);
  }
  pendingCount() {
    return this.db.prepare("SELECT count(*) AS count FROM bodies WHERE state='pending'").get().count;
  }
  nextDueAt() {
    return this.db.prepare("SELECT min(next_attempt_ms) AS due FROM bodies WHERE state='pending'").get().due ?? null;
  }
}
