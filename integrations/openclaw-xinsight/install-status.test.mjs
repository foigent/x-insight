import assert from 'node:assert/strict';
import test from 'node:test';
import {installStatusSnapshot} from './install-status.mjs';
import {reportWorkInstallStatus} from './transport.mjs';

test('installation snapshot groups diagnostics without native messages, paths or body bytes',()=>{
 const snapshot=installStatusSnapshot({pending:3,blocked:1,bytes:999},2,[
  {code:'body_item_limit',occurrences:2,lastAt:'not transmitted'},
  {code:'body_queue_integrity_failed',occurrences:1},
  {code:'future_candidates_unconfirmed',occurrences:1},
  {code:'arbitrary native value /private/file',occurrences:1},
  {code:'future_candidates_conflict',occurrences:2}]);
 assert.deepEqual(snapshot,{metadata_pending:3,metadata_blocked:1,body_pending:2,gaps:[
  {code:'body_limit',occurrences:2},{code:'body_unavailable',occurrences:1},
  {code:'future_scope_unconfirmed',occurrences:3},{code:'observation_unavailable',occurrences:1}]});
 assert.throws(()=>installStatusSnapshot({pending:-1,blocked:0},0,[]),/invalid/);
 assert.throws(()=>installStatusSnapshot({pending:0,blocked:0},0,[{code:'body_item_limit',occurrences:Infinity}]),/invalid/);
 assert.deepEqual(installStatusSnapshot({pending:0,blocked:0},0,[]).gaps,[]);
 assert.deepEqual(installStatusSnapshot({pending:0,blocked:0},0,[
  {code:'managed_secret_unavailable',occurrences:1},{code:'managed_provider_endpoint_changed',occurrences:1}]).gaps,[
  {code:'observation_unavailable',occurrences:1},{code:'ur_relation_unconfirmed',occurrences:1}]);
});

test('installation status receipt binds the same installation and never sends cookies or follows redirects',async()=>{
 const installationId=`xiei_${'a'.repeat(32)}`,status=installStatusSnapshot({pending:1,blocked:0},0,[]);
 const options={origin:'https://xi.example.test',token:`xib_${'b'.repeat(43)}`,installationId,status};
 const receipt={schema_version:'xinsight.local-work-install-response.v1',action:'status',state:'reported',invitation_id:installationId};
 await reportWorkInstallStatus({...options,fetchImpl:async(url,request)=>{
  assert.equal(url.pathname,'/api/v1/work-event-install/status');assert.equal(request.credentials,'omit');assert.equal(request.redirect,'error');
  assert.deepEqual(JSON.parse(request.body),{status});return Response.json(receipt);
 }});
 await assert.rejects(reportWorkInstallStatus({...options,fetchImpl:async()=>Response.json({...receipt,invitation_id:`xiei_${'c'.repeat(32)}`})}),/unconfirmed/);
 await assert.rejects(reportWorkInstallStatus({...options,fetchImpl:async()=>Response.json(receipt,{status:403})}),/unconfirmed/);
 await assert.rejects(reportWorkInstallStatus({...options,fetchImpl:async()=>new Response('x'.repeat(4097))}),/oversize/);
});
