import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';
import {spawnSync} from 'node:child_process';

// Mutates only newly allocated, retained test resources. No database reset.
const origin=process.env.DATA_MODEL_LOCAL_ORIGIN??'http://localhost:8080';
async function call(method,path,body,status=200,key=randomUUID()) {
 const r=await fetch(origin+path,{method,headers:{'Content-Type':'application/json','Idempotency-Key':key},body:body===undefined?undefined:JSON.stringify(body)});
 const text=await r.text();assert.equal(r.status,status,`${method} ${path}: ${text}`);return text?JSON.parse(text):null;
}
function sql(query){
 const r=spawnSync('docker',['exec','-i','fraud-mysql-dev-mysql-1','sh','-c','MYSQL_PWD="$(cat /run/secrets/mysql_root_password)" exec mysql --protocol=socket -uroot --database=fraud --batch --skip-column-names'],{input:query,encoding:'utf8'});
 assert.equal(r.status,0,r.stderr);return r.stdout.trim();
}
const hex=id=>{assert.match(id,/^[a-f0-9-]{36}$/);return `UNHEX('${id.replaceAll('-','')}')`};
const quote=s=>{assert.match(s,/^[a-z0-9_]+$/);return '`'+s+'`'};
const tenant=(await call('POST','/v1/tenants',{name:'Delete and enum smoke',external_key:'delete-enum-'+randomUUID()},201)).tenant;
await call('POST',`/v1/tenants/${tenant.id}/provision`);
const table=(await call('POST',`/v1/tenants/${tenant.id}/tables`,{name:'delete_enum_records'},201)).table;
const create=async body=>(await call('POST',`/v1/tables/${table.id}/fields`,body,201)).field;
const list=async()=>(await call('GET',`/v1/tables/${table.id}/fields`)).fields;
const enums=async id=>(await call('GET',`/v1/fields/${id}/enum-values`)).enum_values;
const physical=sql(`SELECT physical_name FROM fraud.data_model_physical_tables WHERE table_id=${hex(table.id)};`);
const state=()=>sql(`SELECT CONCAT(write_state,':',HEX(published_version)) FROM fraud.data_model_physical_tables WHERE table_id=${hex(table.id)};`);
const field=await create({name:'state',data_type:'string',nullable:true,is_enum:true,enum_values:[{value:'open',label:'Open'},{value:'closed',label:'Closed'}]});
const values=await enums(field.id);const first=values.find(v=>v.value==='open'),second=values.find(v=>v.value==='closed');
const path=`/v1/enum-values/${first.id}`;const key=randomUUID();
const before=state();const patch={value:'review',label:'Review',sort_order:3};
const updated=await call('PATCH',path,patch,200,key);
assert.equal(updated.enum_value.value,'review');assert.notEqual(state(),before);
await call('PATCH',path,{label:'Review later'});
assert.deepEqual(await call('PATCH',path,patch,200,key),updated);
await call('PATCH',path,{label:'Wrong replay'},409,key);
const concurrentKey=randomUUID();
const copies=await Promise.all(Array.from({length:4},()=>call('PATCH',path,{label:'Concurrent'},200,concurrentKey)));
for(const copy of copies)assert.deepEqual(copy,copies[0]);
const stable=state();await call('PATCH',path,{value:'closed'},409);assert.equal(state(),stable);
await call('PATCH',path,{label:'Closed'},409);await call('PATCH',path,{sort_order:2147483648},400);
await call('PATCH',path,{},400);await call('PATCH',path,{unexpected:true},400);
await call('PATCH','/v1/enum-values/bad',{label:'bad'},400);
await call('PATCH',`/v1/enum-values/${randomUUID()}`,{label:'missing'},404);
await call('DELETE',`/v1/enum-values/${second.id}`,undefined,204);
assert.equal((await enums(field.id)).length,1);
await call('DELETE',`/v1/enum-values/${second.id}`,undefined,404);
for(const type of ['int','float']) {
 const f=await create({name:'enum_'+type,data_type:type,nullable:true,is_enum:true,enum_values:[{value:'1',label:'One'}]});
 const v=(await enums(f.id))[0];await call('PATCH',`/v1/enum-values/${v.id}`,{value:'bad'},400);
 await call('PATCH',`/v1/enum-values/${v.id}`,{value:type==='int'?'2':'2.5'});
}
const mandatory=(await list()).find(f=>f.name==='object_id');
const denied=await call('DELETE',`/v1/fields/${mandatory.id}?dry_run=true`,undefined,409);
assert.equal(denied.conflicts.reserved,true);
await call('DELETE',`/v1/fields/${field.id}?dry_run=invalid`,undefined,400);
for(const [type,unique] of [['bool',false],['int',true],['float',false],['timestamp',false],['ip_address',false],['string',false],['string',true]]){
 const f=await create({name:'drop_'+type+'_'+unique,data_type:type,nullable:true,is_unique:unique});
 const p=`/v1/fields/${f.id}`;
 const columns=sql(`SELECT physical_name FROM fraud.data_model_physical_fields WHERE field_id=${hex(f.id)} ORDER BY component;`).split('\n');
 const before=state();const dry=await call('DELETE',p+'?dry_run=true');assert.equal(dry.performed,false);assert.equal(state(),before);
 assert.ok((await list()).some(v=>v.id===f.id));
 // Preserve actual rows and mandatory guards throughout physical retirement.
 const mandatoryColumns=Object.fromEntries(sql(`SELECT f.name,p.physical_name FROM fraud.data_model_fields f JOIN fraud.data_model_physical_fields p ON p.field_id=f.id AND p.tenant_id=f.tenant_id AND p.table_id=f.table_id WHERE f.table_id=${hex(table.id)} AND f.name IN ('object_id','updated_at');`).split('\n').map(v=>v.split('\t')));
 sql(`INSERT INTO fraud.${quote(physical)}(id,${quote(mandatoryColumns.object_id)},${quote(mandatoryColumns.updated_at)}) VALUES(${hex(randomUUID())},'record-${randomUUID()}','2026-11-04 00:00:00');`);
 const count=sql(`SELECT COUNT(*) FROM fraud.${quote(physical)};`);
 const deleted=await call('DELETE',p);assert.equal(deleted.performed,true);
 assert.equal(sql(`SELECT COUNT(*) FROM fraud.${quote(physical)};`),count);
 assert.ok(!(await list()).some(v=>v.id===f.id));
 assert.equal(sql(`SELECT COUNT(*) FROM fraud.data_model_physical_fields WHERE field_id=${hex(f.id)};`),'0');
 for(const col of columns)assert.equal(sql(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='fraud' AND table_name='${physical}' AND column_name='${col}';`),'0');
 if(unique&&type==='string') assert.equal(sql(`SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema='fraud' AND trigger_name IN ('record_text_insert_${f.id.replaceAll('-','')}','record_text_update_${f.id.replaceAll('-','')}');`),'0');
 await call('DELETE',p,undefined,404);
}
// Indexed dependency checks stop retirement before any DDL or writer-gate change.
const presentationKey=randomUUID();
await call('PATCH',`/v1/fields/${field.id}`,{alias:'Keep me'},200,presentationKey);
const beforeConflict=state();await call('DELETE',`/v1/fields/${field.id}`,undefined,409,presentationKey);assert.equal(state(),beforeConflict);
const parent=await create({name:'dependency_parent',data_type:'int',nullable:true,is_unique:true});
const child=await create({name:'dependency_child',data_type:'int',nullable:true});
const link=randomUUID();
sql(`INSERT INTO fraud.data_model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES(${hex(link)},${hex(tenant.id)},'smoke_link',${hex(table.id)},${hex(parent.id)},${hex(table.id)},${hex(child.id)},UTC_TIMESTAMP(6));`);
const dependencyPlan=sql(`EXPLAIN SELECT id FROM fraud.data_model_links WHERE tenant_id=${hex(tenant.id)} AND parent_table_id=${hex(table.id)} AND parent_field_id=${hex(parent.id)} LIMIT 1001;`).split('\t');
assert.ok(['ref','const'].includes(dependencyPlan[4]),dependencyPlan.join(' '));
const unchanged=state();
for(const f of [parent,child]) { const blocked=await call('DELETE',`/v1/fields/${f.id}`,undefined,409);assert.ok(blocked.conflicts.links.includes(link)); }
assert.equal(state(),unchanged);
sql(`DELETE FROM fraud.data_model_links WHERE tenant_id=${hex(tenant.id)} AND id=${hex(link)};`);
const pivot=randomUUID();
sql(`INSERT INTO fraud.data_model_pivots(id,tenant_id,base_table_id,field_id,created_at) VALUES(${hex(pivot)},${hex(tenant.id)},${hex(table.id)},${hex(child.id)},UTC_TIMESTAMP(6));`);
const blocked=await call('DELETE',`/v1/fields/${child.id}?dry_run=true`,undefined,409);assert.ok(blocked.conflicts.pivots.includes(pivot));
sql(`DELETE FROM fraud.data_model_pivots WHERE tenant_id=${hex(tenant.id)} AND id=${hex(pivot)};`);
await call('DELETE',`/v1/fields/${child.id}`);
// Seeded values cascade with field deletion, and a following schema change works.
await call('DELETE',`/v1/fields/${field.id}`);
assert.equal(sql(`SELECT COUNT(*) FROM fraud.data_model_field_enum_values WHERE field_id=${hex(field.id)};`),'0');
await create({name:'after_retirement',data_type:'int',nullable:true});
assert.match(state(),/^open:/);
console.log(JSON.stringify({passed:true,tenant:tenant.id,table:table.id,checks:'enum validation, conflicts, receipts, catalog versions, dry-run, all field types, unique-text guards, metadata cleanup, row preservation'}));
