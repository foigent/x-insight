import { createHash, randomBytes, timingSafeEqual } from 'node:crypto';
import { lstatSync, unlinkSync } from 'node:fs';
import { readPrivateFile, createPrivateFile, replacePrivateFile } from './managed-secret-files.mjs';
import { fetchWorkEventConfiguration, authorizedEndpoint } from './transport.mjs';

const tokenPattern=/^xib_[A-Za-z0-9_-]{43}$/;
const digest=token=>createHash('sha256').update(token).digest('hex');
const equal=(left,right)=>left.length===right.length && timingSafeEqual(left,right);
function pendingCredential(path) {
  try {lstatSync(path);} catch(error) {if(error.code==='ENOENT')return null;throw error;}
  const bytes=readPrivateFile(path,1024), value=JSON.parse(bytes.toString('utf8'));
  if(value.schema_version!=='xinsight.host-credential-renewal.v1' || !/^[a-f0-9]{64}$/.test(value.prior_digest) ||
     !tokenPattern.test(value.replacement_token)) throw new Error('credential_renewal_file_invalid');
  return {bytes,...value};
}
function belongs(configuration,installationId) {
  if(configuration.managed_renewal!==true || configuration.installation_id!==installationId) {
    throw new Error('credential_renewal_installation_mismatch');
  }
}
function activate(tokenFile,currentToken,pending,configuration,installationId) {
  belongs(configuration,installationId);
  const current=readPrivateFile(tokenFile,256);
  const value=current.toString('utf8').trim();
  if(value!==currentToken && value!==pending.replacement_token) throw new Error('credential_renewal_file_changed');
  replacePrivateFile(tokenFile,current,Buffer.from(pending.replacement_token));
  const path=`${tokenFile}.next`, latest=pendingCredential(path);
  if(latest && equal(latest.bytes,pending.bytes)) unlinkSync(path);
  return {token:pending.replacement_token,configuration};
}
async function confirmRenewal(origin,token,replacement,fetchImpl) {
  const response=await fetchImpl(authorizedEndpoint(origin,token,'/api/v1/work-event-install/renew'),{
    method:'POST',redirect:'error',credentials:'omit',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},
    body:JSON.stringify({replacement_token:replacement}),signal:AbortSignal.timeout(15000)});
  if(response.status!==200 || !response.body) throw new Error('credential_renewal_unconfirmed');
  const reader=response.body.getReader();let size=0;const chunks=[];
  try {
    for(;;){const {done,value}=await reader.read();if(done)break;size+=value.length;if(size>4096)throw new Error();chunks.push(value);}
    const value=JSON.parse(Buffer.concat(chunks,size).toString('utf8'));
    if(value.schema_version!=='xinsight.local-work-install-response.v1' || value.action!=='renew' ||
       value.state!=='renewed' || !Number.isFinite(Date.parse(value.expires_at)) || value.token!==undefined)throw new Error();
    return Date.parse(value.expires_at);
  } catch {await reader.cancel().catch(()=>{});throw new Error('credential_renewal_unconfirmed');}
  finally {reader.releaseLock();}
}

// File state is prepared before the request. Retries keep the same replacement;
// an ambiguous response never changes the active credential. Normal collection
// continues with its still-valid token while a renewal is unconfirmed.
export async function fetchManagedConfiguration({origin,token,tokenFile,installationId,fetchImpl=fetch,now=Date.now()}={}) {
  const fetchConfig=value=>fetchWorkEventConfiguration({origin,token:value,fetchImpl});
  if(!tokenFile || !installationId) return {token,configuration:await fetchConfig(token)};
  const pendingPath=`${tokenFile}.next`;
  let pending;
  try {pending=pendingCredential(pendingPath);} catch {
    const configuration=await fetchConfig(token);
    belongs(configuration,installationId);
    return {token,configuration,gap:'credential_renewal_file_invalid'};
  }
  let configuration;
  try {configuration=await fetchConfig(token);} catch(error) {
    // A crash can outlive the old token. The protected prepared replacement is
    // tried only at the same XI origin and must prove this installation.
    if(!pending || pending.prior_digest!==digest(token))throw error;
    const recovered=await fetchConfig(pending.replacement_token);
    return activate(tokenFile,token,pending,recovered,installationId);
  }
  // Installation binding is admission, not a best-effort renewal failure.
  // A mismatched configuration must never fall through to the old-token path.
  belongs(configuration,installationId);
  try {
    if(pending?.replacement_token===token) {
      return activate(tokenFile,token,pending,configuration,installationId);
    }
    if(pending && pending.prior_digest!==digest(token))throw new Error('credential_renewal_file_changed');
    const expires=Date.parse(configuration.credential_expires_at);
    if(!Number.isFinite(expires))throw new Error('credential_renewal_expiry_missing');
    if(!pending && expires-now>30*24*3600*1000)return {token,configuration};
    if(!pending) {
      const record={schema_version:'xinsight.host-credential-renewal.v1',prior_digest:digest(token),
        replacement_token:`xib_${randomBytes(32).toString('base64url')}`};
      createPrivateFile(pendingPath,Buffer.from(JSON.stringify(record)));
      pending=pendingCredential(pendingPath);
    }
    const expiresAt=await confirmRenewal(origin,token,pending.replacement_token,fetchImpl);
    const next=await fetchConfig(pending.replacement_token);
    if(expiresAt<=now || expiresAt!==Date.parse(next.credential_expires_at))throw new Error('credential_renewal_unconfirmed');
    return activate(tokenFile,token,pending,next,installationId);
  } catch {
    return {token,configuration,gap:'credential_renewal_unconfirmed'};
  }
}
