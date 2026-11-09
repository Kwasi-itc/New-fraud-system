import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';
import {spawnSync} from 'node:child_process';

// Dedicated retained data only. Never resets or drops the shared database.
const origin=process.env.DATA_MODEL_LOCAL_ORIGIN ?? 'http://localhost:8080';
async function call(method,path,body,key=randomUUID(),status=200,attempt=0) {
 const response=await fetch(origin+path,{method,headers:{'Content-Type':'application/json','Idempotency-Key':key},body:body===undefined?undefined:JSON.stringify(body)});
 const raw=await response.text();
 if(response.status===503 && attempt<5) { await new Promise(r=>setTimeout(r,1000)); return call(method,path,body,key,status,attempt+1); }
 assert.equal(response.status,status,`${method} ${path}: ${raw}`);
 return raw?JSON.parse(raw):null;
}
const tenant=(await call('POST','/v1/tenants',{name:'Partial endpoint smoke',external_key:'partial-'+randomUUID()},randomUUID(),201)).tenant;
await call('POST',`/v1/tenants/${tenant.id}/provision`);
const table=(await call('POST',`/v1/tenants/${tenant.id}/tables`,{name:'partial_records'},randomUUID(),201)).table;
const tablePath=`/v1/tables/${table.id}`;
const create=async(body)=> (await call('POST',tablePath+'/fields',body,randomUUID(),201)).field;
const memo=await create({name:'memo',data_type:'string',nullable:true});
const score=await create({name:'score',data_type:'int',nullable:true});
const captionKey=randomUUID();
const caption=await call('PATCH',tablePath,{caption_field:'memo',alias:'Caption label'},captionKey);
assert.equal(caption.table.caption_field,'memo');
assert.equal(caption.table.alias,'Caption label');
await call('PATCH',tablePath,{caption_field:''});
assert.deepEqual(await call('PATCH',tablePath,{caption_field:'memo',alias:'Caption label'},captionKey),caption);
await call('PATCH',tablePath,{caption_field:'object_id'},captionKey,409);
await call('PATCH',tablePath,{caption_field:'score',alias:'Must roll back'},randomUUID(),400);
let tables=(await call('GET',`/v1/tenants/${tenant.id}/tables`)).tables;
assert.equal(tables.find(t=>t.id===table.id).alias,'Caption label');
assert.equal(tables.find(t=>t.id===table.id).caption_field,'');
const scorePath=`/v1/fields/${score.id}`;
const constraintKey=randomUUID();
const constrained=await call('PATCH',scorePath,{nullable:false,is_unique:true,alias:'Required unique'},constraintKey);
assert.equal(constrained.field.nullable,false); assert.equal(constrained.field.is_unique,true);
await call('PATCH',scorePath,{nullable:true,is_unique:false});
assert.deepEqual(await call('PATCH',scorePath,{nullable:false,is_unique:true,alias:'Required unique'},constraintKey),constrained);
await call('PATCH',scorePath,{nullable:true},constraintKey,409);
const converted=await call('PATCH',scorePath,{is_enum:true,nullable:false});
assert.equal(converted.field.is_enum,true); assert.equal(converted.field.nullable,false);
await call('PATCH',scorePath,{is_enum:false,nullable:true});
const catalogKey=randomUUID();
const catalogResult=await call('PATCH',scorePath,{is_enum:true},catalogKey);
await call('PATCH',scorePath,{is_enum:false});
assert.deepEqual(await call('PATCH',scorePath,{is_enum:true},catalogKey),catalogResult);
await call('PATCH',scorePath,{nullable:true});
await call('PATCH',scorePath,{is_enum:true,is_unique:true},randomUUID(),400);
const seeds=[{value:'open',label:'Open',sort_order:0},{value:'closed',label:'Closed',sort_order:1}];
const enumBody={name:'state',data_type:'string',nullable:true,is_enum:true,enum_values:seeds};
const enumKey=randomUUID();
const created=await call('POST',tablePath+'/fields',enumBody,enumKey,201);
assert.equal(created.field.is_enum,true);
assert.deepEqual(await call('POST',tablePath+'/fields',enumBody,enumKey,201),created);
await call('POST',tablePath+'/fields',{...enumBody,enum_values:[]},enumKey,409);
const values=(await call('GET',`/v1/fields/${created.field.id}/enum-values`)).enum_values;
assert.equal(values.length,2); assert.deepEqual(values.map(v=>v.value).sort(),['closed','open']);
await call('PATCH',`/v1/fields/${created.field.id}`,{is_enum:false},randomUUID(),409);
await call('PATCH',`/v1/fields/${created.field.id}`,{nullable:false});
await call('POST',tablePath+'/fields',{name:'bad_enum',data_type:'int',is_enum:true,enum_values:[{value:'bad',label:'Bad'}]},randomUUID(),400);
await call('POST',tablePath+'/fields',{name:'bad_seed',data_type:'string',enum_values:seeds},randomUUID(),400);
for(const [name,type,value] of [['rating','int','1'],['ratio','float','1.25']]) {
 const field=await create({name,data_type:type,is_enum:true,nullable:true,enum_values:[{value,label:'First'}]});
 assert.equal((await call('GET',`/v1/fields/${field.id}/enum-values`)).enum_values[0].value,value);
}
await Promise.all([
 call('PATCH',scorePath,{alias:'Concurrent field alias'}),
 call('PATCH',scorePath,{nullable:false}),
]);
await call('PATCH',scorePath,{nullable:true});
const fields=(await call('GET',tablePath+'/fields')).fields;
assert.equal(fields.length,7);
assert.equal(fields.find(f=>f.id===score.id).alias,'Concurrent field alias');
const mandatory=fields.find(f=>f.name==='object_id');
await call('PATCH',`/v1/fields/${mandatory.id}`,{nullable:true},randomUUID(),400);
const jobs=(await call('GET',`/v1/tenants/${tenant.id}/index-jobs`)).index_jobs;
assert.equal(jobs.length,1);
assert.equal((await call('GET',`/v1/index-jobs/${jobs[0].id}`)).index_job.id,jobs[0].id);
const finalModel=await call('GET',`/v1/tenants/${tenant.id}/data-model`);
assert.ok(JSON.stringify(finalModel).includes('closed'));
if(process.argv.includes('--physical')) {
 // SQL is confined to the newly allocated smoke table. Credentials stay inside
 // the container and are neither printed nor passed on the command line.
 function sql(query,ok=true) {
  const r=spawnSync('docker',['exec','-i','fraud-mysql-dev-mysql-1','sh','-c','MYSQL_PWD="$(cat /run/secrets/mysql_root_password)" exec mysql --protocol=socket -uroot --database=fraud --batch --skip-column-names'],{input:query,encoding:'utf8'});
  if(ok) assert.equal(r.status,0,r.stderr); else assert.notEqual(r.status,0,'SQL unexpectedly accepted invalid data');
  return r.stdout.trim();
 }
 assert.match(table.id,/^[a-f0-9-]{36}$/);
 const compact=table.id.replaceAll('-','');
 const physical=sql(`SELECT physical_name FROM fraud.data_model_physical_tables WHERE table_id=UNHEX('${compact}');`);
 const columns=Object.fromEntries(sql(`SELECT f.name,p.physical_name FROM fraud.data_model_fields f JOIN fraud.data_model_physical_fields p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id AND p.field_id=f.id WHERE f.table_id=UNHEX('${compact}') AND p.component='value';`).split('\n').map(v=>v.split('\t')));
 function quote(name) { assert.match(name,/^[a-z0-9_]+$/); return '`'+name+'`'; }
 const qtable='fraud.'+quote(physical);
 const record=(id,value)=>`INSERT INTO ${qtable}(id,${quote(columns.object_id)},${quote(columns.updated_at)},${quote(columns.score)},${quote(columns.state)}) VALUES(UNHEX(REPLACE('${randomUUID()}','-','')),'${id}','2026-11-04 00:00:00',${value},'open');`;
 sql(record('a',1)+record('b',1));
 await call('PATCH',scorePath,{is_unique:true},randomUUID(),409);
 assert.equal(sql(`SELECT write_state FROM fraud.data_model_physical_tables WHERE table_id=UNHEX('${compact}');`),'open');
 await call('PATCH',scorePath,{nullable:false});
 await call('PATCH',scorePath,{nullable:true});
 assert.equal(sql(`SELECT write_state FROM fraud.data_model_physical_tables WHERE table_id=UNHEX('${compact}');`),'open');
 sql(`UPDATE ${qtable} SET ${quote(columns.score)}=2 WHERE ${quote(columns.object_id)}='b';`);
 await call('PATCH',scorePath,{is_unique:true});
 const link=randomUUID().replaceAll('-','');
 const compactTenant=tenant.id.replaceAll('-','');
 const compactField=score.id.replaceAll('-','');
 sql(`INSERT INTO fraud.data_model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES(UNHEX('${link}'),UNHEX('${compactTenant}'),'smoke_dependency',UNHEX('${compact}'),UNHEX('${compactField}'),UNHEX('${compact}'),UNHEX('${compactField}'),UTC_TIMESTAMP(6));`);
 await call('PATCH',scorePath,{is_unique:false},randomUUID(),409);
 assert.equal(sql(`SELECT write_state FROM fraud.data_model_physical_tables WHERE table_id=UNHEX('${compact}');`),'open');
 sql(`DELETE FROM fraud.data_model_links WHERE tenant_id=UNHEX('${compactTenant}') AND id=UNHEX('${link}');`);
 sql(record('duplicate',1),false);
 sql(record('null_a','NULL')+record('null_b','NULL'));
 await call('PATCH',scorePath,{nullable:false},randomUUID(),409);
 assert.equal(sql(`SELECT write_state FROM fraud.data_model_physical_tables WHERE table_id=UNHEX('${compact}');`),'open');
 await call('PATCH',scorePath,{is_unique:false});
 sql(record('duplicate_allowed',1));
 assert.equal(sql(`SELECT COUNT(*) FROM ${qtable};`),'5');
 assert.equal(sql(`SELECT COUNT(*) FROM fraud.data_model_index_deliveries d JOIN fraud.data_model_index_jobs j ON j.tenant_id=d.tenant_id AND j.id=d.job_id WHERE j.table_id=UNHEX('${compact}');`),'1');
 console.log('PASS: physical uniqueness, multiple NULLs, duplicate rejection, rollback leaves writer gate open, caption delivery deduplication');
}
console.log(JSON.stringify({tenantId:tenant.id,tableId:table.id,memoId:memo.id,scoreId:score.id,checks:'caption set/clear, atomic invalid-caption rollback, combined constraints, enum seeds, invalid input, dependency rejection, original-result replay and conflicts'}));
