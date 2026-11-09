import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';
import {spawn, spawnSync} from 'node:child_process';
import {mkdirSync, writeFileSync} from 'node:fs';
import {dirname, resolve} from 'node:path';

const origin = process.env.DATA_MODEL_LOCAL_ORIGIN ?? 'http://localhost:8080';
const worker = 'fraud-mysql-dev-data-model-worker-1';
const mysqlArgs = ['exec', '-i', 'fraud-mysql-dev-mysql-1', 'sh', '-c', 'MYSQL_PWD="$(cat /run/secrets/mysql_root_password)" exec mysql --protocol=socket -uroot --database=fraud --batch --skip-column-names --unbuffered'];
const hex = id => { assert.match(id, /^[a-f0-9-]{36}$/); return `UNHEX('${id.replaceAll('-', '')}')`; };
const quote = name => { assert.match(name, /^[a-z0-9_]+$/); return '`'+name+'`'; };
const text = value => `CONVERT(UNHEX('${Buffer.from(value).toString('hex')}') USING utf8mb4)`;
function docker(...args) { const r = spawnSync('docker', args, {encoding:'utf8', timeout:90000}); assert.equal(r.status,0,r.stderr ?? r.error?.message); return r.stdout.trim(); }
function sql(query) { const r = spawnSync('docker', mysqlArgs, {input:query, encoding:'utf8', timeout:20000}); assert.equal(r.status,0,r.stderr ?? r.error?.message); return r.stdout.trim(); }
async function call(method, path, body, expected=200, key=randomUUID(), attempt=0) {
  const r=await fetch(origin+path,{method,headers:{'Content-Type':'application/json','Idempotency-Key':key},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(75000)});
  const raw=await r.text();
  if(r.status===503 && attempt<20) { await new Promise(r=>setTimeout(r,500)); return call(method,path,body,expected,key,attempt+1); }
  assert.equal(r.status,expected,`${method} ${path}: ${raw}`); return raw ? JSON.parse(raw) : null;
}
async function until(check, label, timeout=180000) {
  const end=Date.now()+timeout;
  do { const value=await check(); if(value) return value; await new Promise(r=>setTimeout(r,250)); } while(Date.now()<end);
  throw new Error('Timed out: '+label);
}
const checks=[];
const passed = name => { checks.push(name); console.log('PASS '+name); };
const jobState = async id => (await call('GET',`/v1/index-jobs/${id}`)).index_job;
const applied = id => until(async()=>{ const job=await jobState(id); assert.notEqual(job.status,'failed',JSON.stringify(job)); return job.status==='applied' && job; },'job '+id);
const startedAt=new Date().toISOString();
const tenant=(await call('POST','/v1/tenants',{name:'Worker migration acceptance',external_key:'worker-migration-'+randomUUID()},201)).tenant.id;
await call('POST',`/v1/tenants/${tenant}/provision`);
const table=(await call('POST',`/v1/tenants/${tenant}/tables`,{name:'worker_records'},201)).table;
const field=async(name,type='string')=>(await call('POST',`/v1/tables/${table.id}/fields`,{name,data_type:type,nullable:true},201)).field;
const score=await field('score','int'),title=await field('title'),reference=await field('reference');
const makeJob=async(columns,index_type='search')=>(await call('POST',`/v1/tenants/${tenant}/index-jobs`,{table_id:table.id,index_type,columns},201)).index_job.id;
const physical=sql(`SELECT physical_name FROM fraud.data_model_physical_tables WHERE tenant_id=${hex(tenant)} AND table_id=${hex(table.id)};`);
const qt='fraud.'+quote(physical);
const index=id=>'record_index_'+id.replaceAll('-','');
const indexRows=id=>Number(sql(`SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema='fraud' AND table_name='${physical}' AND index_name='${index(id)}';`));
const numeric=await makeJob(['score']); await applied(numeric); assert.equal(indexRows(numeric),1);
assert.equal(sql(`SELECT d.state FROM fraud.data_model_index_deliveries d JOIN fraud.data_model_index_delivery_reservations r ON r.tenant_id=d.tenant_id AND r.delivery_id=d.id WHERE r.tenant_id=${hex(tenant)} AND r.job_id=${hex(numeric)};`),'completed');
passed('API enqueue -> native physical index -> atomic job/delivery completion');
const stringJob=await makeJob(['title']); await applied(stringJob);
assert.equal(sql(`SELECT sub_part FROM information_schema.statistics WHERE table_schema='fraud' AND table_name='${physical}' AND index_name='${index(stringJob)}';`),'191');
const columns=Object.fromEntries(sql(`SELECT f.name,p.physical_name FROM fraud.data_model_fields f JOIN fraud.data_model_physical_fields p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id AND p.field_id=f.id WHERE f.tenant_id=${hex(tenant)} AND f.table_id=${hex(table.id)} AND p.component='value';`).split('\n').map(line=>line.split('\t')));
const common='x'.repeat(40000), a=common+'A', b=common+'B';
const records=[...Array.from({length:1000},(_,i)=>'Title '+String(i).padStart(5,'0')),a,b];
sql(`INSERT INTO ${qt}(id,${quote(columns.object_id)},${quote(columns.updated_at)},${quote(columns.title)}) VALUES `+records.map((v,i)=>`(${hex(randomUUID())},'worker-${i}','2026-11-05 00:00:00',${text(v)})`).join(',')+';');
for(const value of [a,b]) assert.equal(sql(`SELECT COUNT(*) FROM ${qt} WHERE ${quote(columns.title)}=${text(value)};`),'1');
const plan=sql(`EXPLAIN SELECT id FROM ${qt} FORCE INDEX (${quote(index(stringJob))}) WHERE ${quote(columns.title)}=${text(a)};`);
assert(plan.split('\n').some(line=>['ref','range','const'].includes(line.split('\t')[4])),plan);
passed('text prefix index preserves full 40KB values and residual equality; indexed lookup on 1002 rows');
await call('PATCH',`/v1/tables/${table.id}`,{caption_field:'title'});
const parent=(await call('POST',`/v1/tenants/${tenant}/tables`,{name:'worker_parent'},201)).table;
const parentID=(await call('GET',`/v1/tables/${parent.id}/fields`)).fields.find(f=>f.name==='object_id').id;
const ordering=(await call('GET',`/v1/tables/${table.id}/fields`)).fields.find(f=>f.name==='updated_at').id;
await call('POST',`/v1/tenants/${tenant}/links`,{name:'owner',parent_table_id:parent.id,parent_field_id:parentID,child_table_id:table.id,child_field_id:reference.id},201);
await call('POST',`/v1/tables/${parent.id}/navigation-options`,{source_field_id:parentID,target_table_id:table.id,filter_field_id:reference.id,ordering_field_id:ordering},201);
const nav=await until(async()=> (await call('GET',`/v1/tenants/${tenant}/index-jobs`)).index_jobs.find(j=>j.table_id===table.id && j.index_type==='navigation'),'navigation queue');
await applied(nav.id); assert.equal(indexRows(nav.id),2);
passed('automatic caption and navigation jobs execute, including text filter + timestamp order');
sql(`ALTER TABLE ${qt} DROP INDEX ${quote(index(stringJob))};`);
const reconciliation=(await call('GET',`/v1/admin/reconcile?tenant_id=${tenant}`)).report;
const repair=reconciliation.tenants.flatMap(t=>t.table_reports).find(t=>t.table_name===table.name).repair_job_ids[0];
assert(repair,'repair not queued'); await applied(repair);
assert.equal(indexRows(stringJob),1); assert.equal(indexRows(repair),1);
assert.equal((await call('GET',`/v1/admin/reconcile?tenant_id=${tenant}`)).report.healthy,true);
passed('reconciliation restores missing retained index, publishes repair, and returns healthy');
assert.equal(await makeJob(['score']),numeric);
assert.equal(sql(`SELECT COUNT(*) FROM fraud.data_model_schema_change_log WHERE tenant_id=${hex(tenant)} AND resource_id=${hex(numeric)} AND operation='apply_index_job';`),'1');
passed('terminal enqueue deduplication preserves one physical effect and one apply audit');

await field('retry_score','int');
let paused=false;
const trigger='worker_fault_'+tenant.replaceAll('-','');
let failedJob;
try {
  docker('pause',worker); paused=true;
  failedJob=await makeJob(['retry_score']);
  sql(`DELIMITER $$\nCREATE TRIGGER fraud.${quote(trigger)} BEFORE INSERT ON fraud.data_model_schema_change_log FOR EACH ROW BEGIN IF NEW.resource_id=${hex(failedJob)} AND NEW.operation='apply_index_job' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='worker acceptance audit fault'; END IF; END$$\nDELIMITER ;\n`);
  docker('unpause',worker); paused=false;
  await until(async()=>{const j=await jobState(failedJob); return j.status==='pending' && j.attempt_count>0 && j;},'audited retry');
  assert.equal(indexRows(failedJob),1);
  assert.equal(sql(`SELECT COUNT(*) FROM fraud.data_model_schema_change_log WHERE tenant_id=${hex(tenant)} AND resource_id=${hex(failedJob)} AND operation='apply_index_job';`),'0');
} finally { sql(`DROP TRIGGER IF EXISTS fraud.${quote(trigger)};`); if(paused) docker('unpause',worker); }
const retried=await applied(failedJob); assert(retried.attempt_count>=2);
assert.equal(sql(`SELECT COUNT(*) FROM fraud.data_model_schema_change_steps WHERE tenant_id=${hex(tenant)} AND operation_id=${hex(failedJob)};`),'1');
passed('failed atomic apply/audit retries from committed publication without duplicate DDL');

await field('restart_score','int');
let lockProcess, connection;
let restartJob;
try {
  docker('pause',worker); paused=true;
  restartJob=await makeJob(['restart_score']);
  lockProcess=spawn('docker',mysqlArgs,{stdio:['pipe','pipe','pipe']});
  let output='',error=''; lockProcess.stderr.on('data',v=>error+=v);
  const locked=new Promise((resolve,reject)=>{
    lockProcess.on('error',reject); lockProcess.on('exit',code=>{if(!connection) reject(new Error('lock fixture exited '+code+': '+error));});
    lockProcess.stdout.on('data',v=>{output+=v; const m=output.match(/LOCKED\t([0-9]+)/); if(m){connection=Number(m[1]);resolve();}});
  });
  lockProcess.stdin.end(`LOCK TABLES ${qt} READ; SELECT 'LOCKED',CONNECTION_ID(); SELECT SLEEP(120);`);
  await Promise.race([locked,new Promise((_,reject)=>{const t=setTimeout(()=>reject(new Error('lock fixture timeout')),15000);t.unref();})]);
  docker('unpause',worker); paused=false;
  await until(async()=>{const j=await jobState(restartJob); if(j.status!=='running') return false; return sql(`SELECT state FROM fraud.data_model_schema_change_intents WHERE tenant_id=${hex(tenant)} AND operation_id=${hex(restartJob)};`)==='applying';},'worker waiting in actual DDL',30000);
  docker('kill','--signal','KILL',worker);
  sql(`KILL ${connection};`); connection=undefined;
  docker('start',worker);
  const recovered=await applied(restartJob); assert(recovered.attempt_count>=2);
  assert.equal(indexRows(restartJob),1);
  assert.equal(sql(`SELECT write_state FROM fraud.data_model_physical_tables WHERE tenant_id=${hex(tenant)} AND table_id=${hex(table.id)};`),'open');
  assert.equal(sql(`SELECT COUNT(*) FROM fraud.data_model_schema_change_log WHERE tenant_id=${hex(tenant)} AND resource_id=${hex(restartJob)} AND operation='apply_index_job';`),'1');
} finally {
  if(connection) sql(`KILL ${connection};`);
  if(paused) docker('unpause',worker);
  if(lockProcess && lockProcess.exitCode===null) lockProcess.kill();
}
passed('hard process kill during DDL, lease expiry rescue, retained-plan resume and single completion');
docker('exec',worker,'/app/mysql-worker','--healthcheck');
const report={passed:true,startedAt,finishedAt:new Date().toISOString(),tenant,table:table.id,parent:parent.id,jobs:{numeric,stringJob,navigation:nav.id,repair,failedJob,restartJob},checks,lookupPlan:plan};
const at=process.argv.indexOf('--output');
if(at>=0){const path=resolve(process.argv[at+1]);mkdirSync(dirname(path),{recursive:true});writeFileSync(path,JSON.stringify(report,null,2)+'\n');}
console.log(JSON.stringify(report,null,2));
