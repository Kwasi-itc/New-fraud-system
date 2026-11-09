import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';
import {mkdirSync,readFileSync,writeFileSync} from 'node:fs';
import {dirname,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {setTimeout as delay} from 'node:timers/promises';

const service=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const repo=resolve(service,'../..');
const saved=Object.fromEntries(readFileSync(resolve(repo,'.local/mysql-dev/ingestion.env'),'utf8').trim().split(/\r?\n/).map(line=>line.split('=')));
const config=JSON.parse(readFileSync(resolve(repo,'.local/mysql-dev/decision-runtime/config.json'),'utf8'));
const tenant=config.TenantID, object=saved.INGESTION_OBJECT_TYPE;
const origin='http://localhost:8082', prefix='decision-smoke-'+randomUUID();
const requests=[];
async function call(method,path,body,status=200,base=origin,headers={}){
 const response=await fetch(base+path,{method,headers:{...(body===undefined?{}:{'Content-Type':'application/json'}),...headers},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(25000)});
 const text=await response.text();requests.push({method,path,status:response.status,body:text});
 assert.ok([status].flat().includes(response.status),`${method} ${path}: ${response.status}: ${text}`);
 return text.startsWith('{')?JSON.parse(text):text;
}
const output=resolve(service,'evidence/mysql-local',new Date().toISOString().replaceAll(':','-'));
mkdirSync(output,{recursive:true});
let result;
try{
 assert.ok((await call('GET','/docs')).includes('SwaggerUIBundle'));
 assert.ok((await call('GET','/openapi.yaml')).includes('openapi: 3.0.3'));
 assert.equal((await call('GET','/readyz')).database,'mysql');
 const capabilities=await call('GET','/capabilities');assert.equal(capabilities.tenant_id,tenant);assert.equal(capabilities.callbacks,true);
 const base=`/v1/tenants/${tenant}`;
 const scenario=(await call('POST',base+'/scenarios',{name:prefix,trigger_object_type:object},201)).scenario;
 const parent=base+'/scenarios/'+scenario.id;
 const iteration=(await call('POST',parent+'/iterations',{},201)).iteration;
 const path=parent+'/iterations/'+iteration.id;
 await call('PUT',path,{trigger_formula:{constant:true},score_review_threshold:10,score_block_and_review_threshold:20,score_decline_threshold:30});
 await call('POST',path+'/rules',{name:'Local review rule',display_order:1,formula:{constant:true},score_modifier:15,rule_group:'local'},201);
 await call('POST',path+'/validate',{});
 await call('POST',path+'/commit',{});
 await call('POST',parent+'/publications',{action:'publish',iteration_id:iteration.id});
 await call('POST',`/v1/tenants/${tenant}/ingest/${object}`,{object_id:prefix,amount:125.5,currency:'USD'},200,'http://localhost:8081');
 const body={scenario_id:scenario.id,object_type:object,idempotency_key:prefix,items:[{object_id:prefix,object_type:object,fields:{amount:125.5,currency:'USD'}}]};
 const created=await call('POST',base+'/async-decision-executions',body,[200,202]);
 const execution=created.async_decision_execution;
 assert.ok(execution.id);
 let current;
 for(let i=0;i<80;i++){
  const wire=await call('GET',base+'/async-decision-executions/'+execution.id);
  current=wire.async_decision_execution??wire;
  if(current.status==='completed')break;
  assert.notEqual(current.status,'failed',JSON.stringify(current));
  await delay(500);
 }
 assert.equal(current.status,'completed',JSON.stringify(current));
 const history=await call('GET',parent+'/decisions?limit=10');
 const decision=history.decisions.find(d=>d.object_id===prefix);
 assert.ok(decision,JSON.stringify(history));assert.equal(decision.outcome,'review');assert.equal(decision.score,15);
 const replay=await call('POST',base+'/async-decision-executions',body,[200,202]);
 assert.equal(replay.async_decision_execution.id,execution.id);assert.equal(replay.replay,true);
 // Omitted fields exercise the worker's real read of the ingested MySQL record.
 const scheduled=(await call('POST',parent+'/scheduled-executions',{scheduled_for:new Date().toISOString(),idempotency_key:prefix+'-scheduled',items:[{object_id:prefix,object_type:object}]},202)).scheduled_execution;
 let scheduledResult;
 for(let i=0;i<80;i++){
  const wire=await call('GET',parent+'/scheduled-executions/'+scheduled.id);
  scheduledResult=wire.scheduled_execution??wire;
  if(scheduledResult.status==='completed')break;
  assert.notEqual(scheduledResult.status,'failed',JSON.stringify(scheduledResult));
  await delay(500);
 }
 assert.equal(scheduledResult.status,'completed',JSON.stringify(scheduledResult));
 const storedHistory=await call('GET',parent+'/decisions?limit=10');
 const persisted=storedHistory.decisions.filter(d=>d.object_id===prefix);
 assert.equal(persisted.length,2);assert.ok(persisted.every(d=>d.outcome==='review'&&d.score===15));
 await call('POST',base+'/async-decision-executions',{...body,idempotency_key:prefix+'-callback',callback_url:'https://example.com/callback'},422);
 await call('POST',parent+'/workflows',{name:'Invalid workflow'},422);
 await call('GET',`/v1/tenants/${randomUUID()}/scenarios`,undefined,403);
 await call('GET','/docs',undefined,403,origin,{Origin:'http://evil.example'});
 result={passed:true,tenantId:tenant,objectType:object,scenarioId:scenario.id,iterationId:iteration.id,executionId:execution.id,scheduledExecutionId:scheduled.id,decisionId:decision.id,recordId:prefix,requestCount:requests.length,checks:'docs, MySQL readiness, authoring/validation/publication, ingestion, async and scheduled worker persistence, stored-record lookup, replay, unsupported providers, tenant/origin rejection'};
 console.log(JSON.stringify(result,null,2));
}catch(error){result={passed:false,tenantId:tenant,error:error.stack,requestCount:requests.length};throw error;}
finally{writeFileSync(resolve(output,'report.json'),JSON.stringify(result,null,2)+'\n');writeFileSync(resolve(output,'http.jsonl'),requests.map(r=>JSON.stringify(r)).join('\n')+'\n');console.log('Evidence: '+output);}
