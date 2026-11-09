import assert from 'node:assert/strict';
import {randomUUID,createHash} from 'node:crypto';
import {appendFileSync,mkdirSync,readFileSync,writeFileSync} from 'node:fs';
import {dirname,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';
import {setTimeout as delay} from 'node:timers/promises';
import {fields,rules,expected} from './generate-fraud-fixtures.mjs';
const service=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const runId=randomUUID();
const phase=process.argv[2]??'all';
const fixtureSource=phase==='--run'?resolve(process.argv[3]):undefined;
const output=resolve(service,'evidence/mysql-fraud',new Date().toISOString().replaceAll(':','-'));
mkdirSync(output,{recursive:true});writeFileSync(resolve(service,'evidence/mysql-fraud/latest.txt'),output+'\n');
const trace=resolve(output,'http.jsonl'),suites=[],results=[];let current='prepare',fixtures,requestCount=0;
async function call(port,method,path,body,status=200){
 const entry={suite:current,port,method,path,request:body,expected:status};
 try{const r=await fetch('http://localhost:'+port+path,{method,headers:body===undefined?{}:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(30000)});entry.status=r.status;entry.body=await r.text();assert.ok([status].flat().includes(r.status),`${method} ${path}: ${r.status} ${entry.body}`);return entry.body.startsWith('{')?JSON.parse(entry.body):entry.body;}catch(e){entry.error=e.message;throw e;}finally{requestCount++;appendFileSync(trace,JSON.stringify(entry)+'\n');}
}
function command(binary,args,timeout=180000){const p=spawnSync(binary,args,{cwd:service,encoding:'utf8',timeout,maxBuffer:4<<20});if(p.error||p.status!==0)throw Error(p.error?.message??p.stdout+'\n'+p.stderr);return p.stdout.trim();}
async function suite(name,run){current=name;try{await run();suites.push({name,passed:true});console.log('PASS '+name);}catch(e){suites.push({name,passed:false,error:e.stack});console.error('FAIL '+name+': '+e.message);}}
async function until(run,accept,label){for(let i=0;i<180;i++){const result=await run();if(accept(result))return result;await delay(500);}throw Error('Timed out: '+label);}
async function tenantFixture(label,provision=true){
 const tenant=(await call(8080,'POST','/v1/tenants',{name:'Synthetic fraud '+label,external_key:'fraud-'+label+'-'+randomUUID()},201)).tenant;
 if(!provision)return {tenant:tenant.id};
 await call(8080,'POST','/v1/tenants/'+tenant.id+'/provision',{});
 const table=(await call(8080,'POST','/v1/tenants/'+tenant.id+'/tables',{name:'fraud_transactions'},201)).table;
 for(const [name,data_type]of Object.entries(fields))await call(8080,'POST','/v1/tables/'+table.id+'/fields',{name,data_type,nullable:false},201);
 return {tenant:tenant.id,table:table.id};
}
if(phase!=='--run'){
 fixtures={at:new Date().toISOString(),tenants:[]};
 for(const name of ['merchant-a','merchant-b'])fixtures.tenants.push(await tenantFixture(name));
 fixtures.inactive=await tenantFixture('inactive',false);
 writeFileSync(resolve(output,'fixtures.json'),JSON.stringify(fixtures,null,2)+'\n');console.log('Prepared '+output);
 if(phase==='--prepare')process.exit(0);
 command('powershell.exe',['-NoProfile','-ExecutionPolicy','Bypass','-File',resolve(service,'scripts/setup-mysql.ps1')],600000);
}else fixtures=JSON.parse(readFileSync(resolve(fixtureSource,'fixtures.json'),'utf8'));
const dataset=JSON.parse(readFileSync(resolve(service,'testdata/fraud-synthetic-transactions.json'),'utf8'));
const runtimeImage=command('docker',['inspect','--format','{{.Image}}','fraud-mysql-dev-decision-engine-service-1']);
function runtime(){return ['decision-engine-service','decision-engine-worker','decision-local-providers'].map(name=>{const v=JSON.parse(command('docker',['inspect','--format','{{json .}}','fraud-mysql-dev-'+name+'-1']));return {name,id:v.Id,image:v.Image,startedAt:v.State.StartedAt,restarts:v.RestartCount};});}
const before=runtime();
const root=t=>'/v1/tenants/'+t.tenant;
async function author(t,variant=false){
 const s=(await call(8082,'POST',root(t)+'/scenarios',{name:(variant?'Independent merchant policy':'Synthetic payment fraud rules')+' '+runId,trigger_object_type:'fraud_transactions'},201)).scenario;
 t.scenario=s.id;t.parent=root(t)+'/scenarios/'+s.id;
 const iteration=(await call(8082,'POST',t.parent+'/iterations',{},201)).iteration;
 t.iteration=iteration.id;const path=t.parent+'/iterations/'+iteration.id;
 const trigger=(await call(8082,'POST',t.parent+'/generate-ast',{expression:'amount >= 0'})).formula;
 await call(8082,'PUT',path,{trigger_formula:trigger,score_review_threshold:20,score_block_and_review_threshold:50,score_decline_threshold:80});
 const definitions=variant?[{name:'Merchant B amount policy',expression:'amount >= 10000',score:10}]:rules;
 for(let i=0;i<definitions.length;i++){const rule=definitions[i];const formula=(await call(8082,'POST',t.parent+'/generate-ast',{expression:rule.expression})).formula;await call(8082,'POST',path+'/rules',{name:rule.name,display_order:i+1,formula,score_modifier:rule.score},201);}
 await call(8082,'POST',path+'/commit',{});await call(8082,'POST',t.parent+'/publications',{action:'publish',iteration_id:iteration.id});
}
function item(row){return {object_id:row.object_id,object_type:'fraud_transactions',fields:row.fields};}
function assertEvaluation(result,want){assert.equal(result.triggered,true);assert.equal(result.decision.score,want.score);assert.equal(result.decision.outcome,want.outcome);const hits=result.rule_executions.filter(r=>r.outcome==='hit').map(r=>r.rule_name).sort();assert.deepEqual(hits,[...want.hit_rules].sort());}
function receipts(t,kind,id){return JSON.parse(command('docker',['exec','fraud-mysql-dev-decision-engine-service-1','/app/mysql-api','--tenant',t.tenant,'--delivery-receipts',kind+'/'+id])).deliveries;}
function localInbox(tenant){const b=createHash('sha1').update(Buffer.from(tenant.replaceAll('-',''),'hex')).update('local-decision-review-inbox').digest().subarray(0,16);b[6]=(b[6]&15)|80;b[8]=(b[8]&63)|128;const h=b.toString('hex');return h.slice(0,8)+'-'+h.slice(8,12)+'-'+h.slice(12,16)+'-'+h.slice(16,20)+'-'+h.slice(20);}
try{
 const [a,b]=fixtures.tenants;
 await suite('all-active-tenant-mode-and-rule-publication',async()=>{assert.equal((await call(8082,'GET','/capabilities')).tenant_mode,'all-active');for(const t of [a,b])assert.ok(Array.isArray((await call(8082,'GET',root(t)+'/scenarios')).scenarios));await author(a);await author(b,true);});
 await suite('synthetic-fraud-dataset-and-persisted-rule-explanations',async()=>{
  for(const row of dataset.rows){assert.deepEqual(row.expected,expected(row.fields),'fixture oracle drift');const result=(await call(8082,'POST',a.parent+'/evaluate',item(row))).result;assertEvaluation(result,row.expected);assert.equal(result.decision.tenant_id,a.tenant);const saved=await call(8082,'GET',root(a)+'/decisions/'+result.decision.id);assert.equal(saved.decision.score,row.expected.score);assert.equal(saved.decision.outcome,row.expected.outcome);assert.deepEqual(saved.rule_executions.filter(r=>r.outcome==='hit').map(r=>r.rule_name).sort(),[...row.expected.hit_rules].sort());results.push({object_id:row.object_id,decision_id:result.decision.id,...row.expected});}
  assert.deepEqual(new Set(results.map(r=>r.outcome)),new Set(['approve','review','block_and_review','decline']));
 });
 await suite('multi-tenant-policy-and-resource-isolation',async()=>{
  const row=dataset.rows.find(r=>r.name==='amount exactly at threshold');
  const other=(await call(8082,'POST',b.parent+'/evaluate',item(row))).result;assert.equal(other.decision.score,10);assert.equal(other.decision.outcome,'approve');
  await call(8082,'GET',root(b)+'/decisions/'+results[0].decision_id,undefined,404);
  await call(8082,'GET',root(b)+'/scenarios/'+a.scenario,undefined,404);
  await call(8082,'POST',root(b)+'/scenarios/'+a.scenario+'/evaluate',item(row),404);
  await call(8082,'GET',root(fixtures.inactive)+'/scenarios',undefined,403);
  await call(8082,'GET','/v1/tenants/'+randomUUID()+'/scenarios',undefined,403);
 });
 await suite('typed-input-validation-and-trigger-nonmatch',async()=>{
  const row=dataset.rows[0];for(const invalid of [{...row.fields,amount:'not-a-number'},{...row.fields,risk_score:true},{...row.fields,card_present:'not-a-boolean'}])await call(8082,'POST',a.parent+'/evaluate',{...item(row),fields:invalid},422);
  const normalized=(await call(8082,'POST',a.parent+'/evaluate',{...item(row),fields:{...row.fields,amount:5000,card_present:'false'}})).result;assert.equal(normalized.decision.score,20);
  const value=(await call(8082,'POST',a.parent+'/evaluate',{...item(row),fields:{...row.fields,amount:-1}})).result;assert.equal(value.triggered,false);assert.equal(value.decision??null,null);
 });
 await suite('async-scheduled-evaluation-and-cross-tenant-local-delivery',async()=>{
  const row=dataset.rows.find(r=>r.name==='block exactly at threshold');
  for(const t of [a,b]){
   const body={scenario_id:t.scenario,object_type:'fraud_transactions',items:[item(row)],idempotency_key:'fraud-async-'+randomUUID(),callback_url:'http://decision-local-providers:8090/callbacks'};
   const created=(await call(8082,'POST',root(t)+'/async-decision-executions',body,202)).async_decision_execution;
   const done=await until(()=>call(8082,'GET',root(t)+'/async-decision-executions/'+created.id),r=>r.status==='completed'&&r.callback_status==='sent','async and callback '+t.tenant);
   const saved=receipts(t,'async_decision_execution_callback',created.id);assert.equal(saved.filter(r=>r.outcome==='accepted').length,1);assert.equal(saved[0].request.tenant_id,t.tenant);
   assert.equal(done.result_body.decision.Score,t===a?50:0);
   const schedule=(await call(8082,'POST',t.parent+'/scheduled-executions',{scheduled_for:new Date().toISOString(),idempotency_key:'fraud-scheduled-'+randomUUID(),items:[item(row)]},202)).scheduled_execution;
   await until(()=>call(8082,'GET',t.parent+'/scheduled-executions/'+schedule.id),r=>(r.scheduled_execution??r).status==='completed','scheduled '+t.tenant);
   await call(8082,'GET',root(t===a?b:a)+'/async-decision-executions/'+created.id,undefined,404);
  }
 });
 await suite('case-workflow-delivery-and-real-review-identities-in-both-tenants',async()=>{
  for(const t of [a,b]){
   const config={name:'Synthetic tenant case workflow',allowed_outcomes:['approve','review','block_and_review','decline'],action_type:'create_case',action_config:{inbox_id:localInbox(t.tenant),name:'Synthetic local fraud case'},active:true};
   const wf=(await call(8082,'POST',t.parent+'/workflows',config,201)).workflow;
   try{
    const evaluated=(await call(8082,'POST',t.parent+'/evaluate',item(dataset.rows.find(r=>r.name==='amount exactly at threshold')))).result;
    const executions=await until(()=>call(8082,'GET',root(t)+'/decisions/'+evaluated.decision.id+'/workflow-executions'),r=>r.workflow_executions[0]?.status==='dispatched','case workflow '+t.tenant);
    const receipt=receipts(t,'workflow_execution',executions.workflow_executions[0].id)[0];assert.equal(receipt.result.backend,'case-owner');
    const path='/v1/investigator/tenants/'+t.tenant+'/cases/'+receipt.result.case_id+'/review?resource_id='+evaluated.decision.id;
    const review=await call(8082,'GET',path);assert.equal(review.machine_outcome,evaluated.decision.outcome);assert.equal(review.eligible,['review','block_and_review'].includes(evaluated.decision.outcome));
    await call(8082,'GET',path.replace(t.tenant,(t===a?b:a).tenant),undefined,[403,404]);
   }finally{await call(8082,'PUT',t.parent+'/workflows/'+wf.id,{...config,active:false});}
  }
 });
 await suite('new-tenant-discovery-without-restarting-the-decision-service',async()=>{
  const late=await tenantFixture('created-after-startup');fixtures.late=late;
  assert.ok(Array.isArray((await call(8082,'GET',root(late)+'/scenarios')).scenarios));await author(late,true);
  const row=dataset.rows.find(r=>r.name==='amount exactly at threshold');const direct=(await call(8082,'POST',late.parent+'/evaluate',item(row))).result;assert.equal(direct.decision.score,10);
  const job=(await call(8082,'POST',root(late)+'/async-decision-executions',{scenario_id:late.scenario,object_type:'fraud_transactions',items:[item(row)],idempotency_key:randomUUID(),callback_url:'http://decision-local-providers:8090/callbacks'},202)).async_decision_execution;
  await until(()=>call(8082,'GET',root(late)+'/async-decision-executions/'+job.id),r=>r.status==='completed'&&r.callback_status==='sent','new tenant worker discovery');
  assert.equal(receipts(late,'async_decision_execution_callback',job.id).filter(r=>r.outcome==='accepted').length,1);
 });
}finally{
 const after=runtime(),unchanged=JSON.stringify(before)===JSON.stringify(after);
 const report={at:new Date().toISOString(),runtimeImage,runtimeUnchanged:unchanged,runtimeBefore:before,runtimeAfter:after,fixtureSource,requestCount,fixtures,datasetVersion:dataset.version,syntheticTransactions:dataset.rows.length,verifiedTransactions:results.length,outcomes:Object.fromEntries(['approve','review','block_and_review','decline'].map(o=>[o,results.filter(r=>r.outcome===o).length])),suites,passed:unchanged&&results.length===dataset.rows.length&&suites.length===7&&suites.every(s=>s.passed)};
 writeFileSync(resolve(output,'fixtures.json'),JSON.stringify(fixtures,null,2)+'\n');writeFileSync(resolve(output,'results.json'),JSON.stringify(results,null,2)+'\n');writeFileSync(resolve(output,'report.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify({output,passed:report.passed,verifiedTransactions:report.verifiedTransactions,outcomes:report.outcomes,requestCount,suites:suites.map(s=>({...s,error:s.error?.split('\n')[0].slice(0,200)}))},null,2));process.exitCode=report.passed?0:1;
}
