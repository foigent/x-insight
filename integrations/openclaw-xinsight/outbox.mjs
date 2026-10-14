import { createHash } from 'node:crypto';
import { closeSync, existsSync, lstatSync, mkdirSync, openSync } from 'node:fs';
import { dirname, isAbsolute } from 'node:path';
import { DatabaseSync } from 'node:sqlite';

const eventKeys = new Set(['schema_version', 'reporter', 'event_ref', 'revision', 'previous_hash', 'occurred_at', 'recorded_at', 'subject_kind', 'lifecycle_state', 'task_ref', 'run_id', 'model_call_id', 'tool_call_id', 'actor', 'action_kind', 'resource', 'content_state', 'content_refs']);
const relationKeys = new Set(['schema_version', 'reporter', 'relation_ref', 'event_ref', 'event_revision', 'event_hash', 'ur_target', 'ur_request_id', 'model_call_id', 'evidence_ref', 'evidence_sha256', 'recorded_at']);
const scopeKeys = new Set(['source_system_id', 'source_instance', 'org_id', 'project_id']);
const actorKeys = new Set(['kind', 'ref']);
const resourceKeys = new Set(['ref', 'name_state', 'name']);
const contentRefKeys = new Set(['kind', 'ref', 'sha256']);

function onlyKeys(value, allowed) {
  return value !== null && typeof value === 'object' && !Array.isArray(value) &&
    Object.keys(value).every((key) => allowed.has(key));
}

function canonical(value) {
  if (value === null || typeof value === 'string' || typeof value === 'boolean') return JSON.stringify(value);
  if (typeof value === 'number') {
    if (!Number.isSafeInteger(value) || Object.is(value, -0)) throw new TypeError('unsafe JSON number');
    return String(value);
  }
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`;
  if (typeof value === 'object') {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonical(value[key])}`).join(',')}}`;
  }
  throw new TypeError('unsupported JSON value');
}

export function canonicalWorkEventJSON(value) { return canonical(value); }

function validate(item) {
  if (!onlyKeys(item, new Set(['collectionId', 'expectedCollectionRevision', 'scopeGeneration', 'kind', 'document', 'observationKey'])) ||
      typeof item.collectionId !== 'string' ||
      !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(item.collectionId) ||
      !Number.isSafeInteger(item.expectedCollectionRevision) || item.expectedCollectionRevision < 1 ||
      typeof item.scopeGeneration !== 'string' || !/^[1-9][0-9]{0,18}$/.test(item.scopeGeneration) ||
      !['event', 'relation'].includes(item.kind) || typeof item.document !== 'string' ||
      (item.observationKey !== undefined && !/^[a-f0-9]{64}$/.test(item.observationKey))) {
    throw new TypeError('invalid outbox item');
  }
  const bytes = Buffer.byteLength(item.document);
  if (bytes < 1 || bytes > 8192) throw new RangeError('document exceeds work-event limit');
  let document;
  try { document = JSON.parse(item.document); } catch { throw new TypeError('document must be JSON'); }
  if (canonical(document) !== item.document) throw new TypeError('document must be canonical JSON');
  const expected = item.kind === 'event' ? 'xinsight.work-event.v1' : 'xinsight.work-event-relation.v1';
  if (!onlyKeys(document, item.kind === 'event' ? eventKeys : relationKeys) ||
      document.schema_version !== expected ||
      !onlyKeys(document.reporter, scopeKeys) ||
      (item.kind === 'event' && (typeof document.event_ref !== 'string' ||
        !/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(document.event_ref) ||
        !Number.isSafeInteger(document.revision) || document.revision < 1)) ||
      (item.kind === 'relation' && (typeof document.relation_ref !== 'string' ||
        !/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(document.relation_ref))) ||
      (item.kind === 'event' && (
        !onlyKeys(document.actor, actorKeys) ||
        (document.resource !== undefined && !onlyKeys(document.resource, resourceKeys)) ||
        !Array.isArray(document.content_refs) ||
        document.content_refs.length > 8 ||
        !document.content_refs.every((ref) => onlyKeys(ref, contentRefKeys)))) ||
      (item.kind === 'relation' && !onlyKeys(document.ur_target, scopeKeys))) {
    throw new TypeError('document is not bounded work-event metadata');
  }
  return { bytes, document };
}

function preparePath(path) {
  if (!isAbsolute(path)) throw new TypeError('outbox path must be absolute');
  const parent = dirname(path);
  mkdirSync(parent, { recursive: true, mode: 0o700 });
  const stat = lstatSync(parent);
  if (!stat.isDirectory() || stat.isSymbolicLink() || stat.uid !== process.getuid() || (stat.mode & 0o077) !== 0) {
    throw new Error('outbox directory must be private and owned');
  }
  if (existsSync(path)) {
    const file = lstatSync(path);
    if (!file.isFile() || file.isSymbolicLink() || file.uid !== process.getuid() || (file.mode & 0o077) !== 0) {
      throw new Error('outbox file must be private and owned');
    }
  } else {
    closeSync(openSync(path, 'wx+', 0o600));
  }
}

// Delivery is durable only after enqueue commits. Observation hooks may fail
// before that point, so this class makes no complete-capture claim.
export class WorkEventOutbox {
  constructor(path, { maxRows = 10000, maxPayloadBytes = 16 * 1024 * 1024,
    maxReplayRows = 100000, replayWindowMs = 24 * 60 * 60 * 1000, clock = Date.now } = {}) {
    if (!Number.isSafeInteger(maxRows) || maxRows < 1 ||
        !Number.isSafeInteger(maxPayloadBytes) || maxPayloadBytes < 8192 ||
        !Number.isSafeInteger(maxReplayRows) || maxReplayRows < maxRows ||
        !Number.isSafeInteger(replayWindowMs) || replayWindowMs < 24 * 60 * 60 * 1000 ||
        replayWindowMs > 7 * 24 * 60 * 60 * 1000 || typeof clock !== 'function') {
      throw new TypeError('invalid outbox bounds');
    }
    preparePath(path);
    this.maxRows = maxRows;
    this.maxPayloadBytes = maxPayloadBytes;
    this.maxReplayRows = maxReplayRows;
    this.replayWindowMs = replayWindowMs;
    this.clock = clock;
    this.db = new DatabaseSync(path);
    // DELETE journaling avoids an unbounded WAL; SQLite also caps the main
    // database pages. The transient rollback journal can approach that cap.
    const pageCap = Math.ceil((maxPayloadBytes * 2 + maxReplayRows * 256 + 1024 * 1024) / 4096);
    this.db.exec(`PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
      PRAGMA auto_vacuum=FULL; PRAGMA max_page_count=${pageCap};
      CREATE TABLE IF NOT EXISTS items (
        id TEXT PRIMARY KEY, collection_id TEXT NOT NULL,
        expected_collection_revision INTEGER NOT NULL, scope_generation TEXT NOT NULL, kind TEXT NOT NULL,
        document TEXT NOT NULL, payload_bytes INTEGER NOT NULL,
        state TEXT NOT NULL CHECK(state IN ('pending','blocked')),
        attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
        next_attempt_ms INTEGER NOT NULL DEFAULT 0,
        created_at TEXT NOT NULL, updated_at TEXT NOT NULL
      );
      CREATE INDEX IF NOT EXISTS items_next ON items(state,next_attempt_ms,created_at,id);
      CREATE TABLE IF NOT EXISTS observations (
        observation_key TEXT PRIMARY KEY, item_id TEXT NOT NULL,
        document_ref TEXT NOT NULL, document_hash TEXT NOT NULL,
        state TEXT NOT NULL CHECK(state IN ('pending','acked','blocked')),
        expires_at_ms INTEGER NOT NULL DEFAULT 0
      );
      CREATE INDEX IF NOT EXISTS observations_expiry ON observations(state,expires_at_ms);
      CREATE TABLE IF NOT EXISTS bindings (
        producer_project_key TEXT PRIMARY KEY, binding_json TEXT NOT NULL
      );
      CREATE TABLE IF NOT EXISTS binding_refresh (
        singleton INTEGER PRIMARY KEY CHECK(singleton=1), confirmed_at_ms INTEGER NOT NULL
      );
      CREATE TABLE IF NOT EXISTS correlations (
        call_id TEXT PRIMARY KEY, run_id TEXT NOT NULL DEFAULT '',
        binding_json TEXT NOT NULL DEFAULT '', event_ref TEXT NOT NULL DEFAULT '',
        event_hash TEXT NOT NULL DEFAULT '', ur_request_id TEXT NOT NULL DEFAULT '',
        response_status INTEGER NOT NULL DEFAULT 0,
        model_context_json TEXT NOT NULL DEFAULT '{}',
        header_context_json TEXT NOT NULL DEFAULT '{}',
        ambiguous INTEGER NOT NULL DEFAULT 0, updated_ms INTEGER NOT NULL
      );
      CREATE INDEX IF NOT EXISTS correlations_age ON correlations(updated_ms);
      CREATE TABLE IF NOT EXISTS gaps (
        code TEXT PRIMARY KEY, occurrences INTEGER NOT NULL,
        first_at TEXT NOT NULL, last_at TEXT NOT NULL
      );`);
  }

  close() { this.db.close(); }

  #transaction(action) {
    this.db.exec('BEGIN IMMEDIATE');
    try {
      const value = action();
      this.db.exec('COMMIT');
      return value;
    } catch (error) {
      this.db.exec('ROLLBACK');
      throw error;
    }
  }

  enqueue(item) {
    const { bytes, document } = validate(item);
    const id = createHash('sha256').update(item.collectionId).update('\0')
      .update(String(item.expectedCollectionRevision)).update('\0').update(item.scopeGeneration).update('\0')
      .update(item.kind).update('\0').update(item.document).digest('hex');
    const documentRef = item.kind === 'event' ? document.event_ref : document.relation_ref;
    const revision = item.kind === 'event' ? document.revision : 1;
    const documentHash = 'sha256:' + createHash('sha256').update(item.document).digest('hex');
    const observationKey = item.observationKey ?? createHash('sha256')
      .update(item.collectionId).update('\0').update(item.kind).update('\0')
      .update(String(documentRef)).update('\0').update(String(revision)).digest('hex');
    const now = new Date().toISOString();
    return this.#transaction(() => {
      this.db.prepare("DELETE FROM observations WHERE state='acked' AND expires_at_ms<=?").run(this.clock());
      const prior = this.db.prepare('SELECT item_id AS id,state FROM observations WHERE observation_key=?').get(observationKey);
      if (prior) return prior.state === 'acked' ?
        { id: prior.id, duplicate: true, acknowledged: true } : { id: prior.id, duplicate: true };
      const totals = this.db.prepare('SELECT count(*) AS rows,coalesce(sum(payload_bytes),0) AS bytes FROM items').get();
      if (totals.rows >= this.maxRows || totals.bytes + bytes > this.maxPayloadBytes) throw new Error('outbox_capacity_exceeded');
      const replayRows = this.db.prepare('SELECT count(*) AS rows FROM observations').get().rows;
      if (replayRows >= this.maxReplayRows) throw new Error('replay_window_capacity_exceeded');
      this.db.prepare(`INSERT INTO items(id,collection_id,expected_collection_revision,scope_generation,kind,document,payload_bytes,state,created_at,updated_at)
        VALUES(?,?,?,?,?,?,?,'pending',?,?)`).run(id, item.collectionId, item.expectedCollectionRevision,
        item.scopeGeneration, item.kind, item.document, bytes, now, now);
      this.db.prepare("INSERT INTO observations(observation_key,item_id,document_ref,document_hash,state) VALUES(?,?,?,?,'pending')")
        .run(observationKey, id, documentRef, documentHash);
      return { id, duplicate: false };
    });
  }

  observation(key) {
    if (!/^[a-f0-9]{64}$/.test(key)) throw new TypeError('invalid observation key');
    return this.db.prepare(`SELECT document_ref AS documentRef,document_hash AS documentHash,state
      FROM observations WHERE observation_key=?`).get(key) ?? null;
  }

  replaceBindings(items) {
    if (!Array.isArray(items) || items.length > 100) throw new TypeError('invalid binding snapshot');
    const rows = items.map((item) => {
      const binding = { collection_id: item.collection_id,
        producer_project_key: item.producer_project_key,
        reporter: item.reporter, collection_revision: item.collection_revision,
        scope_generation: item.scope_generation,
        ...(item.ur_target ? { ur_target: { scope: item.ur_target.scope } } : {}),
        ...(item.body_capture ? { body_capture: item.body_capture } : {}) };
      const encoded = canonical(binding);
      if (typeof binding.producer_project_key !== 'string' || encoded.length > 2048) {
        throw new TypeError('invalid binding snapshot row');
      }
      return [binding.producer_project_key, encoded];
    });
    this.#transaction(() => {
      this.db.prepare('DELETE FROM bindings').run();
      const insert = this.db.prepare('INSERT INTO bindings(producer_project_key,binding_json) VALUES(?,?)');
      for (const [key, encoded] of rows) insert.run(key, encoded);
      this.db.prepare('INSERT OR REPLACE INTO binding_refresh(singleton,confirmed_at_ms) VALUES(1,?)').run(this.clock());
    });
  }

  invalidateBodyPolicy() {
    this.db.prepare('DELETE FROM binding_refresh').run();
  }

  bindings() {
    const now = this.clock();
    // The service and hook workers share this bounded freshness marker. A
    // stale map can identify an observed event, but cannot authorize new body
    // bytes after a restart, failed refresh, or missed configuration interval.
    return this.db.prepare(`SELECT binding_json,
      (SELECT confirmed_at_ms FROM binding_refresh WHERE singleton=1) AS confirmed_at_ms
      FROM bindings ORDER BY producer_project_key`).all().map((row) => {
      const binding = JSON.parse(row.binding_json);
      if (row.confirmed_at_ms === null || row.confirmed_at_ms > now || now - row.confirmed_at_ms >= 60000) {
        delete binding.body_capture;
      }
      return binding;
    });
  }

  #correlationSlot(callId) {
    this.db.prepare('DELETE FROM correlations WHERE updated_ms<=?').run(this.clock() - 60 * 60 * 1000);
    const prior = this.db.prepare('SELECT * FROM correlations WHERE call_id=?').get(callId);
    if (prior) return prior;
    if (this.db.prepare('SELECT count(*) AS rows FROM correlations').get().rows >= 4096) {
      throw new Error('correlation_capacity_exceeded');
    }
    this.db.prepare('INSERT INTO correlations(call_id,updated_ms) VALUES(?,?)').run(callId, this.clock());
    return this.db.prepare('SELECT * FROM correlations WHERE call_id=?').get(callId);
  }

  recordModelStart({ callId, runId, binding, eventRef, eventHash }) {
    if (typeof callId !== 'string' || typeof runId !== 'string' ||
        typeof eventRef !== 'string' || !/^sha256:[a-f0-9]{64}$/.test(eventHash)) {
      throw new TypeError('invalid model correlation');
    }
    const encoded = canonical(binding);
    if (encoded.length > 2048) throw new TypeError('oversize model correlation');
    this.#transaction(() => {
      const prior = this.#correlationSlot(callId);
      if (prior.run_id && (prior.run_id !== runId ||
          prior.event_ref && (prior.event_ref !== eventRef ||
            prior.event_hash !== eventHash || prior.binding_json !== encoded))) {
        this.db.prepare('UPDATE correlations SET ambiguous=1,updated_ms=? WHERE call_id=?').run(this.clock(), callId);
        return;
      }
      this.db.prepare(`UPDATE correlations SET run_id=?,binding_json=?,event_ref=?,event_hash=?,updated_ms=?
        WHERE call_id=?`).run(runId, encoded, eventRef, eventHash, this.clock(), callId);
    });
  }

  recordPendingModelStart({ callId, runId }) {
    if (typeof callId !== 'string' || typeof runId !== 'string') throw new TypeError('invalid pending model');
    this.#transaction(() => {
      const prior = this.#correlationSlot(callId);
      if (prior.run_id && prior.run_id !== runId) {
        this.db.prepare('UPDATE correlations SET ambiguous=1,updated_ms=? WHERE call_id=?').run(this.clock(), callId);
      } else {
        this.db.prepare('UPDATE correlations SET run_id=?,updated_ms=? WHERE call_id=?').run(runId, this.clock(), callId);
      }
    });
  }

  recordModelContext({ callId, workspaceDir, agentId }) {
    if (typeof callId !== 'string') throw new TypeError('invalid model context');
    const context = { ...(typeof workspaceDir === 'string' ? { workspaceDir } : {}),
      ...(typeof agentId === 'string' ? { agentId } : {}) };
    const encoded = canonical(context);
    if (encoded.length > 512) throw new TypeError('oversize model context');
    this.#transaction(() => {
      const prior = this.#correlationSlot(callId);
      if (prior.model_context_json !== '{}' && prior.model_context_json !== encoded) {
        this.db.prepare('UPDATE correlations SET ambiguous=1,updated_ms=? WHERE call_id=?').run(this.clock(), callId);
      } else {
        this.db.prepare('UPDATE correlations SET model_context_json=?,updated_ms=? WHERE call_id=?').run(encoded, this.clock(), callId);
      }
    });
  }

  pendingModel(callId) {
    if (typeof callId !== 'string' || callId.length > 128) throw new TypeError('invalid model call ID');
    const row = this.db.prepare('SELECT run_id,event_ref,model_context_json,ambiguous FROM correlations WHERE call_id=?').get(callId);
    if (!row || row.ambiguous || !row.run_id || row.event_ref || row.model_context_json === '{}') return null;
    return { runId: row.run_id, ...JSON.parse(row.model_context_json) };
  }

  modelContext(callId) {
    if (typeof callId !== 'string' || callId.length > 128) return null;
    const row = this.db.prepare('SELECT model_context_json FROM correlations WHERE call_id=?').get(callId);
    return row && row.model_context_json !== '{}' ? JSON.parse(row.model_context_json) : null;
  }

  recordHeader({ callId, urId, status, workspaceDir, agentId }) {
    if (typeof callId !== 'string' || typeof urId !== 'string' ||
        !Number.isSafeInteger(status) || status < 100 || status > 599) {
      throw new TypeError('invalid header correlation');
    }
    const context = { ...(typeof workspaceDir === 'string' ? { workspaceDir } : {}),
      ...(typeof agentId === 'string' ? { agentId } : {}) };
    const encoded = canonical(context);
    if (encoded.length > 512) throw new TypeError('oversize header context');
    this.#transaction(() => {
      const prior = this.#correlationSlot(callId);
      if (prior.ur_request_id && (prior.ur_request_id !== urId ||
          prior.response_status !== status || prior.header_context_json !== encoded)) {
        this.db.prepare('UPDATE correlations SET ambiguous=1,updated_ms=? WHERE call_id=?').run(this.clock(), callId);
        return;
      }
      this.db.prepare(`UPDATE correlations SET ur_request_id=?,response_status=?,header_context_json=?,updated_ms=?
        WHERE call_id=?`).run(urId, status, encoded, this.clock(), callId);
    });
  }

  correlation(callId) {
    if (typeof callId !== 'string' || callId.length > 128) throw new TypeError('invalid correlation id');
    const row = this.db.prepare('SELECT * FROM correlations WHERE call_id=?').get(callId);
    if (!row || !row.run_id || !row.ur_request_id || row.ambiguous) return null;
    return { runId: row.run_id, binding: JSON.parse(row.binding_json),
      eventRef: row.event_ref, eventHash: row.event_hash,
      urId: row.ur_request_id, status: row.response_status,
      ...JSON.parse(row.header_context_json) };
  }

  next() {
    return this.db.prepare(`SELECT id,collection_id AS collectionId,expected_collection_revision AS expectedCollectionRevision,
      scope_generation AS scopeGeneration,kind,document,attempts
      FROM items WHERE state='pending' AND next_attempt_ms<=? ORDER BY created_at,id LIMIT 1`).get(this.clock()) ?? null;
  }

  nextDueAt() {
    return this.db.prepare("SELECT min(next_attempt_ms) AS due FROM items WHERE state='pending'").get().due;
  }

  acknowledge(id) {
    if (!/^[a-f0-9]{64}$/.test(id)) throw new TypeError('invalid outbox id');
    return this.#transaction(() => {
      const removed = this.db.prepare("DELETE FROM items WHERE id=? AND state='pending'").run(id).changes === 1;
      if (removed) this.db.prepare("UPDATE observations SET state='acked',expires_at_ms=? WHERE item_id=?")
        .run(this.clock() + this.replayWindowMs, id);
      return removed;
    });
  }

  retry(id, code) {
    if (!/^[a-f0-9]{64}$/.test(id) || !/^[a-z][a-z0-9_]{0,63}$/.test(code)) throw new TypeError('invalid retry');
    return this.#transaction(() => {
      const row = this.db.prepare("SELECT attempts FROM items WHERE id=? AND state='pending'").get(id);
      if (!row) return false;
      const delayMs = code === 'dependency_pending' ? 250 : Math.min(60000, 1000 * 2 ** Math.min(row.attempts, 6));
      return this.db.prepare(`UPDATE items SET attempts=attempts+1,last_error=?,next_attempt_ms=?,updated_at=?
        WHERE id=? AND state='pending'`).run(code, this.clock() + delayMs, new Date().toISOString(), id).changes === 1;
    });
  }

  block(id, code) {
    if (!/^[a-f0-9]{64}$/.test(id) || !/^[a-z][a-z0-9_]{0,63}$/.test(code)) throw new TypeError('invalid block');
    return this.#transaction(() => {
      const changed = this.db.prepare(`UPDATE items SET state='blocked',last_error=?,updated_at=?
        WHERE id=? AND state='pending'`).run(code, new Date().toISOString(), id).changes === 1;
      if (changed) this.db.prepare("UPDATE observations SET state='blocked' WHERE item_id=?").run(id);
      return changed;
    });
  }

  status() {
    return this.db.prepare(`SELECT count(*) AS rows,coalesce(sum(payload_bytes),0) AS bytes,
      count(*) FILTER (WHERE state='pending') AS pending,
      count(*) FILTER (WHERE state='blocked') AS blocked FROM items`).get();
  }

  recordGap(code) {
    if (!/^[a-z][a-z0-9_]{0,63}$/.test(code)) throw new TypeError('invalid gap code');
    const now = new Date().toISOString();
    this.#transaction(() => this.db.prepare(`INSERT INTO gaps(code,occurrences,first_at,last_at)
      VALUES(?,1,?,?) ON CONFLICT(code) DO UPDATE SET occurrences=occurrences+1,last_at=excluded.last_at`).run(code, now, now));
  }

  gapStatus() {
    return this.db.prepare('SELECT code,occurrences,first_at AS firstAt,last_at AS lastAt FROM gaps ORDER BY code').all();
  }
}
