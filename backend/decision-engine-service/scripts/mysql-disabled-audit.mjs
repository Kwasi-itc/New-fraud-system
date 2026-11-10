import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';
import {mkdirSync,readFileSync,writeFileSync,appendFileSync} from 'node:fs';
import {resolve,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';
import {setTimeout as delay} from 'node:timers/promises';
const service=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const config=JSON.parse(readFileSync(resolve(service,'../../.local/mysql-dev/decision-runtime/config.json'),'utf8'));
const tenant=config.TenantID,base='/v1/tenants/'+tenant,prefix='delivery-'+randomUUID();
const outputArg=process.argv.indexOf('--output');
const output=outputArg<0?resolve(service,'evidence/mysql-disabled',new Date().toISOString().replaceAll(':','-')):resolve(process.argv[outputArg+1]);
mkdirSync(output,{recursive:true});if(outputArg<0)writeFileSync(resolve(service,'evidence/mysql-disabled/latest.txt'),output+'\n');
const original=JSON.parse(readFileSync(resolve(service,'testdata/mysql-disabled-operations.json'),'utf8'));
const requests=[],suites=[],workers=[];let current='',scenario,parent,path,decision,caps,workerStopped=false,providerStopped=false;
const worker='fraud-mysql-dev-decision-engine-worker-1',provider='fraud-mysql-dev-decision-local-providers-1';
const env=resolve(service,'../../.local/mysql-dev');
function providerMode(mode){const overlay=resolve(output,'callback-policy.json');writeFileSync(overlay,JSON.stringify({services:{'decision-local-providers':{environment:{LOCAL_CALLBACK_MODE:mode}}}}));docker(['compose','--env-file',resolve(env,'compose.env'),'--env-file',resolve(env,'ingestion.env'),'--env-file',resolve(env,'decision.env'),'-f',resolve(service,'docker-compose-mysql.yml'),'-f',overlay,'up','-d','--no-deps','--wait','--force-recreate','decision-local-providers']);}
function docker(args){const r=spawnSync('docker',args,{encoding:'utf8',timeout:60000,maxBuffer:2<<20});if(r.error||r.status!==0)throw new Error(r.error?.message??r.stderr+r.stdout);return r.stdout.trim();}
const runtimeImage=JSON.parse(docker(['inspect','--format','{{json .}}','fraud-mysql-dev-decision-engine-service-1'])).Image;
async function call(method,path,body,expected=200){const entry={suite:current,method,path:path.split('?')[0],query:path.split('?')[1]??'',request:body,expected};try{const r=await fetch('http://localhost:8082'+path,{method,body:body===undefined?undefined:JSON.stringify(body),headers:body===undefined?{}:{'Content-Type':'application/json'},signal:AbortSignal.timeout(25000)});entry.status=r.status;entry.body=await r.text();assert.ok([expected].flat().includes(r.status),`${method} ${path}: ${r.status}: ${entry.body}`);return entry.body.startsWith('{')?JSON.parse(entry.body):entry.body;}catch(e){entry.error=e.message;throw e;}finally{requests.push(entry);appendFileSync(resolve(output,'http.jsonl'),JSON.stringify(entry)+'\n');}}
async function suite(name,run){current=name;try{await run();suites.push({name,passed:true});console.log('PASS '+name);}catch(error){suites.push({name,passed:false,error:error.stack});console.error('FAIL '+name+': '+error.message);}}
async function until(run,accept,label){for(let i=0;i<120;i++){const value=await run();if(accept(value))return value;await delay(500);}throw new Error('Timed out: '+label);}
function receipts(kind,id){return JSON.parse(docker(['exec','fraud-mysql-dev-decision-engine-service-1','/app/mysql-api','--delivery-receipts',kind+'/'+id])).deliveries;}
async function healthy(container){await until(async()=>JSON.parse(docker(['inspect','--format','{{json .State}}',container])),v=>v.Health?.Status==='healthy',container+' readiness');}
const wfBody=(name,active=false)=>({name,allowed_outcomes:['review'],action_type:'emit_event',action_config:{url:caps.event_url},active});
const scoreBody=(name,active=false)=>({name,allowed_outcomes:['review'],ruleset_ref:'local:decision-score-v1',config_json:{},active});
let caseWorkflow,eventWorkflow,screenConfig,scoreConfig;
try{
await suite('enabled-local-provider-profile-and-fixture',async()=>{
 caps=await call('GET','/capabilities');assert.equal(caps.profile,'local-development');for(const key of ['workflow_delivery','screening','additional_scoring','callbacks','outbound_events'])assert.equal(caps[key],true);
 scenario=(await call('POST',base+'/scenarios',{name:prefix,trigger_object_type:'transactions'},201)).scenario;parent=base+'/scenarios/'+scenario.id;
 const iteration=(await call('POST',parent+'/iterations',{},201)).iteration;path=parent+'/iterations/'+iteration.id;
 await call('PUT',path,{trigger_formula:{constant:true},score_review_threshold:10,score_block_and_review_threshold:20,score_decline_threshold:30});
 await call('POST',path+'/rules',{name:'Local delivery rule',display_order:1,formula:{constant:true},score_modifier:15},201);await call('POST',path+'/commit',{});await call('POST',parent+'/publications',{action:'publish',iteration_id:iteration.id});
});
await suite('legacy-workflow-crud-reorder-and-isolation',async()=>{
 const root=parent+'/workflows';const a=(await call('POST',root,wfBody('First'),201)).workflow,b=(await call('POST',root,wfBody('Second'),201)).workflow;
 await call('GET',root+'/'+a.id);await call('PUT',root+'/'+a.id,wfBody('Updated'));
 await call('POST',root+'/reorder',[b.id,a.id],204);assert.equal((await call('GET',root)).workflows[0].id,b.id);
 await call('POST',root+'/reorder',[a.id,a.id],[409,422]);
 await call('POST',root,{...wfBody('Foreign inbox'),action_type:'create_case',action_config:{inbox_id:randomUUID(),name:'Forbidden'}},403);
 await call('DELETE',root+'/'+a.id,{},204);await call('DELETE',root+'/'+b.id,{},204);await call('GET',root+'/'+a.id,undefined,404);
});
await suite('scoring-config-crud-and-validation',async()=>{
 const root=parent+'/scoring-configs';const item=(await call('POST',root,scoreBody('Initial'),201)).scoring_config;
 await call('GET',root);await call('GET',root+'/'+item.id);await call('PUT',root+'/'+item.id,scoreBody('Updated'));
 await call('POST',root,{name:'Invalid'},422);await call('DELETE',root+'/'+item.id,{},204);await call('GET',root+'/'+item.id,undefined,404);
});
await suite('real-case-screening-scoring-and-outbox-delivery',async()=>{
 caseWorkflow=(await call('POST',parent+'/workflows',{...wfBody('Create case',true),action_type:'create_case',action_config:{inbox_id:config.ReviewInboxID,name:'Local delivered case'}},201)).workflow;
 eventWorkflow=(await call('POST',parent+'/workflows',wfBody('Deliver event',true),201)).workflow;
 scoreConfig=(await call('POST',parent+'/scoring-configs',scoreBody('Deterministic score',true),201)).scoring_config;
 const screenBody={name:'Local exact-name watchlist',provider:'local-development',allowed_outcomes:['review'],active:true,config_json:{queries:[{name:'Example Watchlisted Person',type:'Person'}],provider_config:{mode:'exact',watchlist:[{id:'local-entity-1',name:'Example Watchlisted Person',type:'Person'}]}}};
 screenConfig=(await call('POST',parent+'/screening-configs',screenBody,201)).screening_config;
 decision=(await call('POST',parent+'/evaluate',{object_id:prefix,object_type:'transactions',fields:{amount:100,currency:'USD'}})).result.decision;
 assert.equal(decision.score,15);
 const work=await until(()=>call('GET',base+'/decisions/'+decision.id+'/workflow-executions'),v=>v.workflow_executions.length===2&&v.workflow_executions.every(x=>x.status==='dispatched'),'workflow dispatch');
 for(const item of work.workflow_executions){const saved=receipts('workflow_execution',item.id);assert.equal(saved.length,1);if(item.action_type==='create_case'){assert.equal(saved[0].result.backend,'case-owner');const state=await call('GET','/v1/investigator/tenants/'+tenant+'/cases/'+saved[0].result.case_id+'/review?resource_id='+decision.id);assert.equal(state.eligible,true);}else assert.equal(saved[0].result.received,true);}
 const screenings=await until(()=>call('GET',base+'/decisions/'+decision.id+'/screening-executions'),v=>v.screening_executions[0]?.status==='completed','screening result projection');const screen=screenings.screening_executions[0];assert.equal(screen.response_json.match_count,1);assert.equal(screen.response_json.status,'awaiting_review');assert.equal(receipts('screening_execution',screen.id).length,1);
 const scores=await until(()=>call('GET',base+'/decisions/'+decision.id+'/scoring-requests'),v=>v.scoring_requests[0]?.status==='completed','scoring result');assert.equal(scores.scoring_requests[0].response_json.backend,'local-development-scoring');assert.equal(scores.scoring_requests[0].response_json.score,15);assert.equal(scores.scoring_requests[0].response_json.risk_band,'medium');
 const events=await until(()=>call('GET',base+'/outbox-events?limit=100'),v=>v.outbox_events.some(e=>e.aggregate_id===decision.id&&e.status==='sent'),'outbox delivery');const event=events.outbox_events.find(e=>e.aggregate_id===decision.id&&e.status==='sent');assert.equal(receipts('outbox_event',event.id).length,1);
 assert.equal((await call('GET',base+'/decisions/'+decision.id)).decision.outcome,'review');
 await call('PUT',parent+'/workflows/'+caseWorkflow.id,{...wfBody('Create case',false),action_type:'create_case',action_config:{inbox_id:config.ReviewInboxID,name:'Local delivered case'}});await call('PUT',parent+'/workflows/'+eventWorkflow.id,wfBody('Deliver event',false));await call('PUT',parent+'/scoring-configs/'+scoreConfig.id,scoreBody('Deterministic score',false));await call('PUT',parent+'/screening-configs/'+screenConfig.id,{...screenBody,active:false});
 workers.push({check:'workflow case/link/history, event inbox, watchlist matches/projection, scoring result and outbound receipts',passed:true});
});
await suite('callback-rejection-redrive-cancellation-and-restart',async()=>{
 const body={scenario_id:scenario.id,object_type:'transactions',items:[{object_id:prefix+'-callback',object_type:'transactions',fields:{amount:100,currency:'USD'}}],callback_url:caps.callback_url};
 await call('POST',base+'/async-decision-executions',{...body,callback_url:'https://example.com/not-local'},422);
 providerStopped=true;providerMode('reject');
 const values=[];for(let i=0;i<2;i++){const item=(await call('POST',base+'/async-decision-executions',{...body,idempotency_key:prefix+'-callback-'+i},202)).async_decision_execution;values.push(await until(()=>call('GET',base+'/async-decision-executions/'+item.id),v=>v.status==='completed'&&v.callback_status==='failed'&&v.callback_attempt_count===v.callback_max_attempts,'callback retries exhausted'));}
 docker(['stop','--time','20',worker]);workerStopped=true;
 const cancelled=base+'/async-decision-executions/'+values[0].id,retry=base+'/async-decision-executions/'+values[1].id;
 await call('POST',cancelled+'/callback/retry',{expected_generation:0});const c=await call('POST',cancelled+'/callback/cancel',{expected_generation:1});assert.equal(c.callback_status,'cancelled');await call('POST',cancelled+'/callback/cancel',{expected_generation:1});
 const redriven=await call('POST',retry+'/callback/retry',{expected_generation:0});assert.equal(redriven.generation,1);await call('POST',retry+'/callback/retry',{expected_generation:0},409);
 providerMode('accept');providerStopped=false;await healthy(provider);docker(['start',worker]);workerStopped=false;await healthy(worker);
 const delivered=await until(()=>call('GET',retry),v=>v.callback_status==='sent','callback redrive delivered');assert.deepEqual(delivered.result_body,values[1].result_body);assert.equal(delivered.status,'completed');const saved=receipts('async_decision_execution_callback',values[1].id).filter(v=>v.outcome==='accepted');assert.equal(saved.length,1);assert.equal(saved[0].request.payload.body.status,'completed');assert.equal(receipts('async_decision_execution_callback',values[0].id).filter(v=>v.outcome==='accepted').length,0);assert.equal((await call('GET',cancelled)).callback_status,'cancelled');
 workers.push({check:'callback failure exhaustion, generation-fenced redrive, cancellation, receiver/worker restart and unchanged evaluation results',passed:true});
});
await suite('formerly-disabled-route-tenant-isolation',async()=>{for(const op of original){const path=op.path.replace(/\{[^}]+\}/g,()=>randomUUID());await call(op.method,path,op.method==='GET'?undefined:{},403);}});
}finally{
 if(providerStopped)providerMode('accept');if(workerStopped)docker(['start',worker]);
 const passing=new Set(suites.filter(v=>v.passed).map(v=>v.name));const matrix=original.map(op=>{const re=new RegExp('^'+op.path.replace(/\{[^}]+\}/g,'[^/]+')+'$');const positive=requests.filter(v=>v.method===op.method&&re.test(v.path)&&v.status>=200&&v.status<300&&!v.error&&passing.has(v.suite));return {...op,successVerified:positive.length>0,suites:[...new Set(positive.map(v=>v.suite))]};});
 const runtimeUnchanged=runtimeImage===JSON.parse(docker(['inspect','--format','{{json .}}','fraud-mysql-dev-decision-engine-service-1'])).Image;
 const report={at:new Date().toISOString(),prefix,tenant,runtimeImage,runtimeUnchanged,requestCount:requests.length,suites,workers,matrix,verifiedOperations:matrix.filter(v=>v.successVerified).length,totalOperations:matrix.length,profilePassed:runtimeUnchanged&&matrix.every(v=>v.successVerified)&&suites.every(v=>v.passed)};writeFileSync(resolve(output,'report.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify({output,verified:report.verifiedOperations,total:report.totalOperations,suites},null,2));process.exitCode=report.profilePassed?0:1;
}
