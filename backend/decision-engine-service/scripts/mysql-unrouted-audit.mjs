import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';
import {mkdirSync,readFileSync,writeFileSync,appendFileSync} from 'node:fs';
import {resolve,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {setTimeout as delay} from 'node:timers/promises';
import {spawnSync} from 'node:child_process';
const service=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const local=JSON.parse(readFileSync(resolve(service,'../../.local/mysql-dev/decision-runtime/config.json'),'utf8'));
const tenant=local.TenantID;
const base='/v1/tenants/'+tenant;
const prefix='unrouted-'+randomUUID();
const outputArg=process.argv.indexOf('--output');
const output=outputArg<0?resolve(service,'evidence/mysql-unrouted',new Date().toISOString().replaceAll(':','-')):resolve(process.argv[outputArg+1]);
mkdirSync(output,{recursive:true});
if(outputArg<0)writeFileSync(resolve(service,'evidence/mysql-unrouted/latest.txt'),output+'\n');
const original=JSON.parse(readFileSync(resolve(service,'testdata/mysql-unrouted-operations.json'),'utf8'));
const requests=[],suites=[];let current='';
async function call(method,path,body,expected=200){
 const entry={suite:current,method,path:path.split('?')[0],query:path.split('?')[1]??'',expected,request:body};
 try{const r=await fetch('http://localhost:8082'+path,{method,body:body===undefined?undefined:JSON.stringify(body),headers:body===undefined?{}:{'Content-Type':'application/json'},signal:AbortSignal.timeout(30000)});entry.status=r.status;entry.body=await r.text();assert.ok([expected].flat().includes(r.status),`${method} ${path}: ${r.status}: ${entry.body}`);return entry.body.startsWith('{')?JSON.parse(entry.body):entry.body;}
 catch(e){entry.error=e.message;throw e;}finally{requests.push(entry);appendFileSync(resolve(output,'http.jsonl'),JSON.stringify(entry)+'\n');}
}
async function suite(name,run){current=name;try{await run();suites.push({name,passed:true});console.log('PASS '+name);}catch(error){suites.push({name,passed:false,error:error.stack});console.error('FAIL '+name+': '+error.message);}}
function docker(args){const r=spawnSync('docker',args,{encoding:'utf8',timeout:180000,maxBuffer:2<<20});if(r.error||r.status!==0)throw new Error(r.error?.message??r.stderr+'\n'+r.stdout);return r.stdout.trim();}
const image=()=>JSON.parse(docker(['inspect','--format','{{json .}}','fraud-mysql-dev-decision-engine-service-1'])).Image;
const runtimeImage=image();
let scenario,parent,iteration,path,phantom,run,decision,rule;
try{
await suite('fixture-and-new-direct-routes',async()=>{
 assert.equal((await call('GET','/v1/service-info')).service,'decision-engine-service');
 scenario=(await call('POST',base+'/scenarios',{name:prefix,trigger_object_type:'transactions'},201)).scenario;
 parent=base+'/scenarios/'+scenario.id;
 iteration=(await call('POST',parent+'/iterations',{},201)).iteration;path=parent+'/iterations/'+iteration.id;
 await call('PUT',path,{trigger_formula:{constant:true},score_review_threshold:10,score_block_and_review_threshold:20,score_decline_threshold:30});
 rule=(await call('POST',path+'/rules',{name:'Unrouted rule',display_order:1,formula:{constant:true},score_modifier:15,rule_group:'audit'},201)).rule;
 await call('POST',path+'/commit',{});await call('POST',parent+'/publications',{action:'publish',iteration_id:iteration.id});
 const body={object_id:prefix,object_type:'transactions',fields:{amount:100,currency:'USD'}};
 for(const [url,request,multi]of [[parent+'/evaluate',body,false],[base+'/decisions',{...body,scenario_id:scenario.id},false],[base+'/decisions/all',body,true],[base+'/ingestion-events/record-ingested',{...body,mode:'sync'},true]]){
  const value=(await call('POST',url,request)).result;
  if(multi)assert.ok(value.results.some(v=>v.decision?.scenario_id===scenario.id&&v.decision.score===15));else {assert.equal(value.decision.score,15);decision=value.decision;}
 }
 await call('POST',parent+'/evaluate',{...body,fields:{amount:'bad'}},422);
 const async=await call('POST',base+'/ingestion-events/record-ingested',{...body,mode:'async'},202);
 for(let i=0;i<80;i++){const item=await call('GET',base+'/async-decision-executions/'+async.async_decision_execution.id);if(item.status==='completed')break;assert.notEqual(item.status,'failed');await delay(500);if(i===79)throw new Error('Ingestion trigger async worker did not finish');}
 const accessors=await call('GET',parent+'/editor-identifiers');assert.ok(accessors.payload_accessors.length>=2);
});
await suite('test-run-draft-comparison-and-analytics',async()=>{
 phantom=(await call('POST',path+'/draft',{},201)).iteration;
 run=(await call('POST',parent+'/test-runs',{phantom_iteration_id:phantom.id,expires_at:new Date(Date.now()+3600000).toISOString()},201)).test_run;
 const before=(await call('GET',parent+'/decisions?limit=100')).decisions.length;
 const compared=(await call('POST',base+'/test-runs/'+run.id+'/evaluate',{object_id:prefix+'-comparison',object_type:'transactions',fields:{amount:100,currency:'USD'}})).result;
 assert.equal(compared.live.decision.score,15);assert.equal(compared.phantom.decision.score,15);
 assert.equal((await call('GET',parent+'/decisions?limit=100')).decisions.length,before,'Comparison wrote a live decision');
 const scores=await call('GET',base+'/test-runs/'+run.id+'/decision-data-by-score');assert.ok(scores.decisions.some(v=>v.score===15&&v.count===1));
 const rules=await call('GET',base+'/test-runs/'+run.id+'/data-by-rule-execution');assert.equal(rules.rules[0].hit_count,1);
 await call('POST',base+'/test-runs/'+run.id+'/cancel',{});
 await call('POST',base+'/test-runs/'+run.id+'/evaluate',{object_id:prefix,object_type:'transactions',fields:{amount:100,currency:'USD'}},409);
});
await suite('workflow-rule-condition-action-lifecycle',async()=>{
 const root=parent+'/workflow-rules';
 const first=(await call('POST',root,{name:'First',fallthrough:true},201)).workflow_rule;
 const second=(await call('POST',root,{name:'Second',fallthrough:false},201)).workflow_rule;
 const p=root+'/'+first.id;
 await call('GET',root);await call('GET',p);await call('PUT',p,{name:'Updated',fallthrough:true});
 await call('POST',root+'/reorder',[second.id,first.id],204);
 const list=(await call('GET',root)).workflow_rules;assert.equal(list[0].id,second.id);
 const condition=(await call('POST',p+'/conditions',{function:'always',params:null},201)).condition;
 await call('PUT',p+'/conditions/'+condition.id,{function:'never',params:null});
 await call('POST',p+'/conditions',{function:'outcome_in',params:['invalid-outcome']},422);
 await call('PUT',p+'/conditions/not-a-uuid',{function:'always',params:null},400);
 await call('DELETE',p+'/conditions/'+condition.id,{},204);
 const action=(await call('POST',p+'/actions',{action_type:'do_nothing',action_config:{}},201)).action;
 await call('PUT',p+'/actions/'+action.id,{action_type:'do_nothing',action_config:{}});
 await call('DELETE',p+'/actions/'+action.id,{},204);
 const caseAction=(await call('POST',p+'/actions',{action_type:'create_case',action_config:{inbox_id:local.ReviewInboxID,name:'Local case action'}},201)).action;
 await call('POST',p+'/actions',{action_type:'create_case',action_config:{inbox_id:randomUUID(),name:'Foreign inbox'}},403);
 await call('POST',p+'/actions',{action_type:'create_case',action_config:{inbox_id:local.ReviewInboxID,tag_ids:[randomUUID()]}},403);
 await call('DELETE',p+'/actions/'+caseAction.id,{},204);
 await call('DELETE',p,{},204);await call('DELETE',root+'/'+second.id,{},204);
 await call('GET',p,undefined,404);
});
await suite('screening-config-lifecycle-and-list-entry-alias',async()=>{
 const root=parent+'/screening-configs';
 const fields={name:prefix,provider:'local-development',allowed_outcomes:['review'],config_json:{},active:false,review_inbox_id:local.ReviewInboxID};
 const config=(await call('POST',root,fields,201)).screening_config;
 await call('GET',root);await call('GET',root+'/'+config.id);
 await call('PUT',root+'/'+config.id,{...fields,name:prefix+'-updated'});
 await call('POST',root,{...fields,review_inbox_id:'invalid'},422);
 await call('POST',root,{...fields,review_inbox_id:randomUUID()},403);
 await call('DELETE',root+'/'+config.id,{},204);await call('GET',root+'/'+config.id,undefined,404);
 const list=(await call('POST',base+'/platform/custom-lists',{name:prefix,kind:'string'},201)).custom_list;
 const entry=(await call('POST',base+'/platform/custom-list-entries',{list_id:list.id,value:prefix},201)).custom_list_entry;
 assert.equal(entry.value,prefix);
 await call('DELETE',base+'/platform/custom-lists/'+list.id,{},204);
});
await suite('investigator-review-replay-version-and-case-access',async()=>{
 const fixture=JSON.parse(docker(['exec','fraud-mysql-dev-decision-engine-service-1','/app/mysql-api','--review-fixture-decision',decision.id]));
 const root='/v1/investigator/tenants/'+tenant+'/cases/'+fixture.case_id+'/review';
 const before=await call('GET',root+'?resource_id='+decision.id);assert.equal(before.eligible,true);assert.equal(before.version,'0');
 await call('GET',root+'?resource_id='+randomUUID(),undefined,403);
 const command={operation_id:randomUUID(),resource_id:decision.id,expected_version:'0',action:'approve',findings:'Local endpoint audit: examined immutable machine evidence.'};
 const after=await call('POST',root,command);assert.equal(after.status,'approve');assert.equal(after.version,'1');assert.equal(after.machine_outcome,'review');
 assert.deepEqual(await call('POST',root,command),after);
 await call('POST',root,{...command,findings:'Conflicting operation payload'},409);
 await call('POST',root,{...command,operation_id:randomUUID()},409);
 const stored=await call('GET',root+'?resource_id='+decision.id);assert.deepEqual(stored,after);
 const immutable=(await call('GET',base+'/decisions/'+decision.id)).decision;assert.equal(immutable.outcome,'review');
});
await suite('screening-scoring-status-retry-and-authoritative-callback',async()=>{
 const worker='fraud-mysql-dev-decision-engine-worker-1';
 docker(['stop','--time','20',worker]);
 const provider='fraud-mysql-dev-decision-local-providers-1';
 docker(['stop','--time','20',provider]);
 try {
 const repo=resolve(service,'../..');const env=resolve(repo,'.local/mysql-dev');
 const lines=docker(['compose','--env-file',resolve(env,'compose.env'),'--env-file',resolve(env,'ingestion.env'),'--env-file',resolve(env,'decision.env'),'-f',resolve(service,'docker-compose-mysql.yml'),'run','--rm','--no-deps','-e','LOCAL_DECISION_FIXTURE_DECISION='+decision.id,'migrate-decisions']);
 const marker=lines.split(/\r?\n/).find(v=>v.startsWith('LOCAL_EFFECT_FIXTURE='));assert.ok(marker,'Missing genuine owner fixture output');const fixture=JSON.parse(marker.slice('LOCAL_EFFECT_FIXTURE='.length));
 const screening=base+'/screening-executions/'+fixture.screening_id,score=base+'/scoring-requests/'+fixture.scoring_id;
 assert.equal((await call('GET',base+'/decisions/'+decision.id+'/screening-executions')).screening_executions.length,2);
 assert.equal((await call('GET',screening)).screening_execution.status,'pending');
 await call('POST',screening+'/status',{status:'failed',last_error:'Explicit local audit failure'});
 await call('POST',score+'/status',{status:'failed',last_error:'Explicit local audit failure'});
 assert.equal((await call('GET',score)).scoring_request.status,'failed','Scoring status was not persisted');
 assert.equal((await call('POST',screening+'/retry',{})).screening_execution.status,'pending');
 assert.equal((await call('POST',score+'/retry',{})).scoring_request.status,'pending');
 assert.equal((await call('GET',score)).scoring_request.status,'pending','Scoring retry was not persisted');
 await call('POST',screening+'/retry',{},409);await call('POST',score+'/retry',{},409);
 await call('POST',screening+'/status',{status:'invalid'},422);
 const body={tenant_id:tenant,screening_id:fixture.source_id,decision_id:decision.id,scenario_id:scenario.id,screening_config_id:fixture.screening_config_id,status:'completed',provider_reference:'untrusted-callback-value'};
 await call('POST','/internal/screening-status-updates',{...body,scenario_id:randomUUID()},409);
 assert.equal((await call('GET',base+'/screening-executions/'+fixture.linked_execution_id)).screening_execution.status,'pending','Mismatched callback committed before rejection');
 const result=(await call('POST','/internal/screening-status-updates',body)).screening_execution;
 assert.equal(result.status,'failed');assert.notEqual(result.provider_reference,'untrusted-callback-value');assert.equal(result.id,fixture.linked_execution_id);
 assert.deepEqual((await call('POST','/internal/screening-status-updates',body)).screening_execution,result);
 await call('POST',base+'/screening-executions/'+fixture.linked_execution_id+'/retry',{},409);
 await call('POST','/internal/screening-status-updates',{...body,tenant_id:randomUUID()},403);
 } finally {docker(['start',provider]);docker(['start',worker]);}
});
await suite('local-expression-assistance-and-model-validation',async()=>{
 const generated=await call('POST',parent+'/generate-ast',{expression:'amount > 99 and currency == "USD"'});
 assert.equal(generated.backend,'local-expression-assistant');assert.equal(generated.uses_ai,false);assert.equal(generated.formula.function,'and');assert.ok(generated.model_revision);
 const described=await call('POST',parent+'/ast-ai-description',{formula:generated.formula});assert.equal(described.description,generated.description);
 const stored=await call('GET',path+'/rules/'+rule.id+'/ai-description');assert.equal(stored.description,'true');assert.equal(stored.uses_ai,false);
 await call('POST',parent+'/generate-ast',{expression:'unknown_field > 0'},422);
 await call('POST',parent+'/generate-ast',{expression:'amount > "wrong type"'},422);
 await call('POST',parent+'/generate-ast',{expression:'amount = 1'},422);
 await call('POST',parent+'/ast-ai-description',{formula:{function:'not_a_function'}},422);
 await call('GET',path+'/rules/'+randomUUID()+'/ai-description',undefined,404);
});
await suite('all-tenant-route-isolation-and-mounted-inventory',async()=>{
 const routes=JSON.parse(docker(['exec','fraud-mysql-dev-decision-engine-service-1','/app/mysql-api','--routes']));
 writeFileSync(resolve(output,'routes.json'),JSON.stringify(routes,null,2)+'\n');
 for(const op of original){
  assert.ok(routes.some(r=>r.method===op.method&&r.path===op.path.replace(/\{([^}]+)\}/g,':$1')),'Unrouted operation: '+op.method+' '+op.path);
  if(!op.path.includes('{tenantId}'))continue;
  const foreign=op.path.replace(/\{[^}]+\}/g,()=>randomUUID());await call(op.method,foreign,op.method==='GET'?undefined:{},403);
 }
});
}finally{
 const passing=new Set(suites.filter(v=>v.passed).map(v=>v.name));
 const matrix=original.map(op=>{
  const regex=new RegExp('^'+op.path.replace(/\{[^}]+\}/g,'[^/]+')+'$');
  const successes=requests.filter(r=>r.method===op.method&&regex.test(r.path)&&r.status>=200&&r.status<300&&!r.error&&passing.has(r.suite));
  return {method:op.method,path:op.path,successVerified:successes.length>0,suites:[...new Set(successes.map(v=>v.suite))]};
 });
 const runtimeUnchanged=image()===runtimeImage;
 const report={at:new Date().toISOString(),prefix,tenant,runtimeImage,runtimeUnchanged,scenarioId:scenario?.id,requestCount:requests.length,suites,verifiedOperations:matrix.filter(v=>v.successVerified).length,totalOperations:matrix.length,all39Passed:runtimeUnchanged&&matrix.every(v=>v.successVerified)&&suites.every(v=>v.passed),matrix};
 writeFileSync(resolve(output,'report.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify({output,verified:report.verifiedOperations,total:matrix.length,suites:suites.map(v=>({name:v.name,passed:v.passed}))},null,2));
 process.exitCode=suites.every(v=>v.passed)&&report.all39Passed?0:1;
}
