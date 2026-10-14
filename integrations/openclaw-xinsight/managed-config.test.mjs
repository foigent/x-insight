import assert from 'node:assert/strict';
import test from 'node:test';
import { managedProviderIDs, isManagedModel, prepareManagedPluginConfig, configuredAgentProjects } from './managed-config.mjs';
const settings={origin:'https://xi.example.test',urBaseUrl:'https://ur.example.test/v1',
 tokenFile:'/synthetic/private/ingest.key',bodyKeyFile:'/synthetic/private/body.key',
 outboxPath:'/synthetic/queue/metadata.sqlite',bodyOutboxPath:'/synthetic/queue/body.sqlite'};
function fixture(){return {agents:{defaults:{model:{primary:'unirouter-fast/fast',fallbacks:['unirouter-deep/deep']}}},
 auth:{profiles:{'ur:existing':{provider:'unirouter-fast',mode:'api_key'}}},
 models:{providers:{'unirouter-fast':{baseUrl:settings.urBaseUrl,api:'openai-completions',apiKey:{source:'env',provider:'default',id:'EXISTING_UR_KEY'},models:[{id:'fast',reasoning:false,cost:{input:2,output:3}}]},
 'unirouter-deep':{baseUrl:settings.urBaseUrl+'/',api:'openai-responses',apiKey:{source:'file',provider:'existing',id:'/ur-key'},models:[{id:'deep',reasoning:true,input:['text','image']}]},
 'direct-other':{baseUrl:'https://other.example.test/v1',api:'openai-completions',models:[{id:'other'}]}}},
 plugins:{allow:['unrelated'],entries:{unrelated:{enabled:true,config:{keep:true}}}}};}
test('managed setup preserves models, capabilities, references, profiles, defaults and unrelated plugins exactly',()=>{
 const original=fixture(), before=structuredClone(original);
 const result=prepareManagedPluginConfig(original,settings);
 assert.deepEqual(original,before);
 for(const key of ['agents','auth','models']) assert.deepEqual(result.config[key],original[key]);
 assert.deepEqual(result.providers,['unirouter-deep','unirouter-fast']);
 assert.deepEqual(result.config.plugins.entries.unrelated,original.plugins.entries.unrelated);
 assert.deepEqual(result.config.plugins.allow,['unrelated','xi-work-events']);
 assert.equal(result.config.plugins.entries['xi-work-events'].config.provider,undefined);
 assert.deepEqual(prepareManagedPluginConfig(result.config,settings).config,result.config);
});
test('future matching providers join at startup while excluded and changed endpoints are not observed',()=>{
 const config=fixture(), managed={baseUrl:settings.urBaseUrl,excludedProviderIds:['unirouter-fast']};
 config.models.providers['unirouter-new']={baseUrl:settings.urBaseUrl,api:'openai-completions',models:[{id:'new'}]};
 assert.deepEqual(managedProviderIDs(config,managed),['unirouter-deep','unirouter-new']);
 const ctx={config,provider:'unirouter-new'};
 assert.equal(isManagedModel(ctx,{baseUrl:settings.urBaseUrl},managed),true);
 assert.equal(isManagedModel(ctx,{baseUrl:'https://other.example.test/v1'},managed),false);
});
test('setup refuses an unresolved gateway and an existing installation relocation',()=>{
 assert.throws(()=>prepareManagedPluginConfig(fixture(),{...settings,urBaseUrl:'https://unknown.example.test/v1'}),/managed_ur_provider_unresolved/);
 const current=prepareManagedPluginConfig(fixture(),settings).config;
 assert.throws(()=>prepareManagedPluginConfig(current,{...settings,outboxPath:'/other/location.sqlite'}),/managed_existing_installation_conflict/);
 const original=fixture(); delete original.plugins.allow;
 assert.equal(prepareManagedPluginConfig(original,settings).config.plugins.allow,undefined);
});

test('selected Agent is pinned in managed config and cannot silently widen or change',()=>{
 const selected={...settings,selectedAgentId:'xi-acceptance'};
 const config=prepareManagedPluginConfig(fixture(),selected).config;
 assert.equal(config.plugins.entries['xi-work-events'].config.selectedAgentId,'xi-acceptance');
 assert.deepEqual(prepareManagedPluginConfig(config,selected).config,config);
 assert.throws(()=>prepareManagedPluginConfig(config,settings),/managed_existing_installation_conflict/);
 assert.throws(()=>prepareManagedPluginConfig(config,{...settings,selectedAgentId:'personal'}),/managed_existing_installation_conflict/);
 assert.throws(()=>prepareManagedPluginConfig(fixture(),{...settings,selectedAgentId:'bad/id'}),/managed_agent_selection_invalid/);
});


test('future agent discovery uses only public configured identities, including the implicit main agent',()=>{
 assert.deepEqual(configuredAgentProjects({agents:{defaults:{workspace:'/do-not-parse'}}}),[{project_key:'agent:main',label:'main'}]);
 const config={agents:{entries:{writer:{name:'写作助手',workspace:'/private',model:'original/model'},main:{}}}};
 const before=structuredClone(config);
 assert.deepEqual(configuredAgentProjects(config),[{project_key:'agent:main',label:'main'},{project_key:'agent:writer',label:'写作助手'}]);
 assert.deepEqual(configuredAgentProjects(config,'writer'),[{project_key:'agent:writer',label:'写作助手'}]);
 assert.deepEqual(config,before);
 config.agents.entries.personal={name:'Later Personal Agent'};
 assert.deepEqual(configuredAgentProjects(config,'writer'),[{project_key:'agent:writer',label:'写作助手'}]);
 assert.throws(()=>configuredAgentProjects(config,'unknown'),/future_agent_roster_invalid/);
 for(const entries of [null,[],{'bad/path':{}},{main:{name:' '}}]) assert.throws(()=>configuredAgentProjects({agents:{entries}}),/future_agent_roster_invalid/);
 const entries=Object.fromEntries(Array.from({length:33},(_,i)=>[`agent-${i}`,{}]));
 assert.throws(()=>configuredAgentProjects({agents:{entries}}),/future_agent_roster_invalid/);
});
