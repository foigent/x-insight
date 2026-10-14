import { constants, openSync, closeSync, fstatSync, lstatSync, readFileSync, realpathSync, writeFileSync, fsyncSync, linkSync, unlinkSync, renameSync } from 'node:fs';
import { isAbsolute, normalize, dirname } from 'node:path';
import { timingSafeEqual, randomBytes } from 'node:crypto';

const ownerAllowed = stat => typeof process.getuid === 'function' && (stat.uid === process.getuid() || stat.uid === 0);
function exactPath(path) {
  if (typeof path !== 'string' || !isAbsolute(path) || normalize(path) !== path || path.includes('\0')) {
    throw new Error('managed_secret_path_invalid');
  }
}
function checkFile(stat, maximum) {
  if (!stat.isFile() || !ownerAllowed(stat) || (stat.mode & 0o777) !== 0o600 || stat.size < 1 || stat.size > maximum) {
    throw new Error('managed_secret_file_invalid');
  }
}

// The managed Gateway principal (or root) owns this file. A config reference
// never grants an agent a general secret lookup or changes host permissions.
export function readPrivateFile(path, maximum = 4096) {
  exactPath(path);
  let fd;
  try {
    const named = lstatSync(path);
    checkFile(named, maximum);
    if (realpathSync(path) !== path) throw new Error('managed_secret_file_invalid');
    fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    const opened = fstatSync(fd);
    checkFile(opened, maximum);
    if (opened.dev !== named.dev || opened.ino !== named.ino) throw new Error('managed_secret_file_changed');
    const bytes = readFileSync(fd);
    if (bytes.length !== opened.size || bytes.length > maximum) throw new Error('managed_secret_file_changed');
    return bytes;
  } catch {
    // Do not echo OS error paths or the supplied secret into plugin logs.
    throw new Error('managed_secret_unavailable');
  } finally { if (fd !== undefined) closeSync(fd); }
}

// Initial credential delivery uses exclusive create. An interrupted installer
// can confirm identical bytes; rotation writes a fresh versioned path instead
// of replacing a concurrently changed or user-owned file.
export function createPrivateFile(path, bytes, maximum = 4096) {
  exactPath(path);
  if (!Number.isSafeInteger(maximum) || maximum < 1 || maximum > 16384 || !Buffer.isBuffer(bytes) || bytes.length < 1 || bytes.length > maximum) throw new Error('managed_secret_bytes_invalid');
  let fd, staged;
  try {
    const parent = dirname(path), stat = lstatSync(parent);
    if (!stat.isDirectory() || !ownerAllowed(stat) || (stat.mode & 0o077) !== 0 || realpathSync(parent) !== parent) {
      throw new Error('managed_secret_directory_invalid');
    }
    staged = `${path}.pending-${randomBytes(12).toString('hex')}`;
    fd = openSync(staged, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
    writeFileSync(fd, bytes);
    fsyncSync(fd);
    closeSync(fd); fd = undefined;
    try { linkSync(staged,path); }
    catch (error) {
      if (error.code !== 'EEXIST') throw error;
      const current = readPrivateFile(path, maximum);
      if (current.length !== bytes.length || !timingSafeEqual(current,bytes)) throw new Error('managed_secret_file_conflict');
      return {state:'identical'};
    }
    unlinkSync(staged); staged = undefined;
    const directoryFD = openSync(parent, constants.O_RDONLY | constants.O_DIRECTORY);
    try { fsyncSync(directoryFD); } finally { closeSync(directoryFD); }
    return {state:'created'};
  } catch {
    throw new Error('managed_secret_write_failed');
  } finally {
    if (fd !== undefined) closeSync(fd);
    if (staged !== undefined) { try { unlinkSync(staged); } catch { /* this invocation's staging file only */ } }
  }
}

export function ingestToken(config, env = process.env) {
  const value = config.tokenFile !== undefined ? readPrivateFile(config.tokenFile, 256).toString('utf8').trim() : env.XINSIGHT_WORK_EVENT_TOKEN ?? '';
  if (!/^xib_[A-Za-z0-9_-]{43}$/.test(value)) throw new Error('managed_ingest_credential_invalid');
  return value;
}

export function bodyQueueKey(config, env = process.env) {
  const value = config.bodyKeyFile !== undefined ? readPrivateFile(config.bodyKeyFile, 128).toString('utf8').trim() : env.XINSIGHT_WORK_EVENT_BODY_QUEUE_KEY ?? '';
  if (!/^[a-fA-F0-9]{64}$/.test(value)) throw new Error('managed_body_key_invalid');
  return Buffer.from(value, 'hex');
}

// The managed Gateway service is the single credential-file writer. Native
// reload joins the previous service before starting its replacement. Expected
// bytes detect a changed operator file; concurrent renewers use the same
// durable next credential rather than inventing competing replacement values.
export function replacePrivateFile(path, expected, bytes, maximum=4096) {
  exactPath(path);
  if (!Number.isSafeInteger(maximum) || maximum < 1 || maximum > 16384 ||
      !Buffer.isBuffer(expected) || !Buffer.isBuffer(bytes) || bytes.length < 1 || bytes.length > maximum) {
    throw new Error('managed_secret_bytes_invalid');
  }
  const equal = (left, right) => left.length === right.length && timingSafeEqual(left, right);
  let current = readPrivateFile(path,maximum);
  if (equal(current, bytes)) return {state:'identical'};
  if (!equal(current, expected)) throw new Error('managed_secret_file_conflict');
  const staged = `${path}.replacement-${randomBytes(12).toString('hex')}`;
  try {
    createPrivateFile(staged,bytes,maximum);
    current = readPrivateFile(path,maximum);
    if (equal(current,bytes)) return {state:'identical'};
    if (!equal(current,expected)) throw new Error('managed_secret_file_conflict');
    renameSync(staged,path);
    const directoryFD = openSync(dirname(path), constants.O_RDONLY | constants.O_DIRECTORY);
    try { fsyncSync(directoryFD); } finally { closeSync(directoryFD); }
    return {state:'replaced'};
  } finally { try { unlinkSync(staged); } catch (error) { if (error.code !== 'ENOENT') throw new Error('managed_secret_staging_cleanup_failed'); } }
}
