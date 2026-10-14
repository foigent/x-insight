import assert from 'node:assert/strict';
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, statSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { readPrivateFile, createPrivateFile, replacePrivateFile, ingestToken, bodyQueueKey } from './managed-secret-files.mjs';

function fixture(t) {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), 'xi-managed-files-')));
  chmodSync(dir, 0o700);
  t.after(() => rmSync(dir, {recursive:true,force:true}));
  return dir;
}
test('exclusive credential placement survives an identical resume without replacing another value', t => {
  const file = join(fixture(t), 'ingest.key'), bytes = Buffer.from(`xib_${'a'.repeat(43)}\n`);
  assert.deepEqual(createPrivateFile(file,bytes), {state:'created'});
  const original = statSync(file);
  assert.equal(original.mode & 0o777, 0o600);
  assert.deepEqual(createPrivateFile(file,bytes), {state:'identical'});
  assert.equal(statSync(file).ino,original.ino);
  assert.throws(() => createPrivateFile(file,Buffer.from('different')), /managed_secret_write_failed/);
  assert.deepEqual(readFileSync(file),bytes);
});
test('managed reads reject symlink and broad access and never fall back to an environment credential', t => {
  const dir = fixture(t), file=join(dir,'ingest.key');
  createPrivateFile(file,Buffer.from(`xib_${'b'.repeat(43)}`));
  const link=join(dir,'link.key'); symlinkSync(file,link);
  assert.throws(() => readPrivateFile(link), /managed_secret_unavailable/);
  assert.throws(() => ingestToken({tokenFile:link},{XINSIGHT_WORK_EVENT_TOKEN:`xib_${'c'.repeat(43)}`}), /managed_secret_unavailable/);
  chmodSync(file,0o644);
  assert.throws(() => readPrivateFile(file), /managed_secret_unavailable/);
  assert.throws(() => createPrivateFile(file,Buffer.from('replacement')), /managed_secret_write_failed/);
  const publicDir=join(dir,'public'); mkdirSync(publicDir,{mode:0o755});
  assert.throws(() => createPrivateFile(join(publicDir,'new.key'),Buffer.from('value')), /managed_secret_write_failed/);
});
test('token and independent queue-key file inputs remain distinct and bounded', t => {
  const dir=fixture(t), token=join(dir,'ingest.key'), key=join(dir,'body.key');
  createPrivateFile(token,Buffer.from(`xib_${'d'.repeat(43)}`));
  createPrivateFile(key,Buffer.from('12'.repeat(32)));
  assert.equal(ingestToken({tokenFile:token}),`xib_${'d'.repeat(43)}`);
  assert.deepEqual(bodyQueueKey({bodyKeyFile:key}),Buffer.alloc(32,0x12));
  assert.throws(() => ingestToken({tokenFile:key}), /managed_ingest_credential_invalid/);
  assert.throws(() => bodyQueueKey({bodyKeyFile:token}), /managed_body_key_invalid/);
  writeFileSync(key,Buffer.alloc(129,0x61),{mode:0o600});
  assert.throws(() => bodyQueueKey({bodyKeyFile:key}), /managed_secret_unavailable/);
});


test('renewal atomically activates the prepared credential and refuses a changed current file',t=>{
  const root=realpathSync(mkdtempSync(join(tmpdir(),'xi-secret-replace-')));
  t.after(()=>rmSync(root,{recursive:true,force:true}));
  const path=join(root,'token.key'), before=Buffer.from('old-synthetic'), next=Buffer.from('next-synthetic');
  createPrivateFile(path,before);
  assert.throws(()=>replacePrivateFile(path,Buffer.from('not-current'),next),/conflict/);
  assert.deepEqual(readPrivateFile(path),before);
  assert.equal(replacePrivateFile(path,before,next).state,'replaced');
  assert.deepEqual(readPrivateFile(path),next);
  assert.equal(replacePrivateFile(path,before,next).state,'identical');
  assert.equal(statSync(path).mode & 0o777,0o600);
});
