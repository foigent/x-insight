import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, realpathSync, rmSync, readFileSync, existsSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createPrivateFile } from './managed-secret-files.mjs';
import { fetchManagedConfiguration } from './credential-renewal.mjs';

const now=Date.parse('2026-09-29T00:00:00Z'),day=86400000;
const installationId=`xiei_${'a'.repeat(32)}`,oldToken=`xib_${'a'.repeat(43)}`;
function fixture(t) {
  const root=realpathSync(mkdtempSync(join(tmpdir(),'xi-credential-renewal-')));
  t.after(()=>rmSync(root,{recursive:true,force:true}));
  const tokenFile=join(root,'ingest.key');createPrivateFile(tokenFile,Buffer.from(oldToken));
  let replacement='',lost=false,oldExpired=false,wrongInstallation=false,oldLifetime=20;
  const renewalBodies=[];
  const fetchImpl=async(url,request)=>{
    const path=new URL(url).pathname,token=request.headers.Authorization.slice(7);
    assert.equal(new URL(url).origin,'https://xi.example.test');assert.equal(request.redirect,'error');
    if(path==='/api/v1/work-event-install/renew') {
      assert.equal(token,oldToken);
      const body=JSON.parse(request.body);renewalBodies.push(body);
      const prepared=JSON.parse(readFileSync(tokenFile+'.next','utf8'));
      assert.equal(prepared.replacement_token,body.replacement_token);
      if(replacement)assert.equal(body.replacement_token,replacement);
      replacement=body.replacement_token;
      if(lost){lost=false;throw Error('reply lost after commit');}
      return Response.json({schema_version:'xinsight.local-work-install-response.v1',action:'renew',state:'renewed',expires_at:new Date(now+90*day).toISOString()});
    }
    assert.equal(path,'/api/v1/work-event-ingest/config');
    if(token===oldToken && oldExpired)return Response.json({code:'expired'},{status:401});
    assert.ok(token===oldToken || token===replacement);
    return Response.json({version:'xinsight.work-event-ingest-config.v1',items:[],managed_renewal:true,
      installation_id:wrongInstallation && token===replacement?`xiei_${'b'.repeat(32)}`:installationId,
      credential_expires_at:new Date(now+(token===oldToken?oldLifetime:90)*day).toISOString()});
  };
  return {tokenFile,renewalBodies,run:()=>fetchManagedConfiguration({origin:'https://xi.example.test',token:readFileSync(tokenFile,'utf8'),tokenFile,installationId,fetchImpl,now}),
    lose:()=>{lost=true;},expireOld:()=>{oldExpired=true;},wrong:()=>{wrongInstallation=true;},notDue:()=>{oldLifetime=60;}};
}

test('managed renewal confirms the prepared credential then activates it without changing collection permissions',async t=>{
  const h=fixture(t),result=await h.run();
  assert.notEqual(result.token,oldToken);assert.equal(result.configuration.installation_id,installationId);
  assert.equal(readFileSync(h.tokenFile,'utf8'),result.token);assert.equal(statSync(h.tokenFile).mode & 0o777,0o600);
  assert.ok(!existsSync(h.tokenFile+'.next'));assert.equal(h.renewalBodies.length,1);
  await h.run();assert.equal(h.renewalBodies.length,1);
});

test('a lost renewal reply keeps the active credential and replays the same durable replacement',async t=>{
  const h=fixture(t);h.lose();const first=await h.run();
  assert.equal(first.token,oldToken);assert.equal(first.gap,'credential_renewal_unconfirmed');
  assert.equal(readFileSync(h.tokenFile,'utf8'),oldToken);assert.ok(existsSync(h.tokenFile+'.next'));
  const result=await h.run();assert.notEqual(result.token,oldToken);
  assert.deepEqual(h.renewalBodies[0],h.renewalBodies[1]);
});

test('restart after the old credential expires can recover only the previously prepared credential for this installation',async t=>{
  const h=fixture(t);h.lose();await h.run();h.expireOld();
  const recovered=await h.run();assert.notEqual(recovered.token,oldToken);assert.equal(h.renewalBodies.length,1);
  assert.ok(!existsSync(h.tokenFile+'.next'));
});

test('another installation never replaces the active credential and renewal does not run before the window',async t=>{
  const h=fixture(t);h.wrong();const result=await h.run();
  assert.equal(result.token,oldToken);assert.equal(result.gap,'credential_renewal_unconfirmed');
  assert.equal(readFileSync(h.tokenFile,'utf8'),oldToken);
  const notDue=fixture(t);notDue.notDue();assert.equal((await notDue.run()).token,oldToken);
  assert.equal(notDue.renewalBodies.length,0);assert.ok(!existsSync(notDue.tokenFile+'.next'));
});


for (const invalidPending of [false,true]) test(`the active token cannot admit a different installation even when renewal state is corrupt (${invalidPending})`,async t=>{
  const root=realpathSync(mkdtempSync(join(tmpdir(),'xi-renewal-binding-')));
  t.after(()=>rmSync(root,{recursive:true,force:true}));
  const tokenFile=join(root,'ingest.key');createPrivateFile(tokenFile,Buffer.from(oldToken));
  if(invalidPending)createPrivateFile(tokenFile+'.next',Buffer.from('invalid'));
  let requests=0;
  const fetchImpl=async(url)=>{
    requests++;assert.equal(new URL(url).pathname,'/api/v1/work-event-ingest/config');
    return Response.json({version:'xinsight.work-event-ingest-config.v1',items:[],managed_renewal:true,
      installation_id:`xiei_${'b'.repeat(32)}`,credential_expires_at:new Date(now+90*day).toISOString()});
  };
  await assert.rejects(fetchManagedConfiguration({origin:'https://xi.example.test',token:oldToken,tokenFile,installationId,fetchImpl,now}),/credential_renewal_installation_mismatch/);
  assert.equal(requests,1);assert.equal(readFileSync(tokenFile,'utf8'),oldToken);
});
