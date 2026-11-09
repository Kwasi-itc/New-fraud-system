import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';

// Creates a dedicated, retained smoke tenant. Never resets the application DB.
const origin = process.env.DATA_MODEL_LOCAL_ORIGIN ?? 'http://localhost:8080';
async function call(method,path,body,key,status=200,attempt=0) {
 const response=await fetch(origin+path,{method,headers:{'Content-Type':'application/json',...(key?{'Idempotency-Key':key}:{})},body:body===undefined?undefined:JSON.stringify(body)});
 const text=await response.text();
 // Schema locks are deliberately nonblocking. Retry the SAME operation,
 // retaining its key after contention or an uncertain response.
 if(response.status===503 && key && status!==503 && attempt<10) {
  await new Promise(resolve=>setTimeout(resolve,1000));
  return call(method,path,body,key,status,attempt+1);
 }
 assert.equal(response.status,status,`${method} ${path}: ${text}`);
 return text?JSON.parse(text):null;
}
const tenant=(await call('POST','/v1/tenants',{name:'MySQL mutation smoke',external_key:'mutation-smoke-'+randomUUID()},undefined,201)).tenant;
await call('POST',`/v1/tenants/${tenant.id}/provision`);
const table=(await call('POST',`/v1/tenants/${tenant.id}/tables`,{name:'mutation_records',alias:'Initial'},undefined,201)).table;
const tablePath=`/v1/tables/${table.id}`;
const key=randomUUID();
const first=await call('PATCH',tablePath,{alias:'First'},key);
await call('PATCH',tablePath,{alias:'Second'},randomUUID());
assert.deepEqual(await call('PATCH',tablePath,{alias:'First'},key),first);
await call('PATCH',tablePath,{alias:'Changed command'},key,409);
await Promise.all([
 call('PATCH',tablePath,{alias:'Concurrent alias'},randomUUID()),
 call('PATCH',tablePath,{description:'Concurrent description'},randomUUID()),
]);
const tables=await call('GET',`/v1/tenants/${tenant.id}/tables`);
const current=tables.tables.find(t=>t.id===table.id);
assert.equal(current.alias,'Concurrent alias'); assert.equal(current.description,'Concurrent description');
for(const [name,type] of [['score','int'],['amount','float'],['enabled','bool'],['memo','string'],['occurred','timestamp'],['address','ip_address']]) {
 const body={name,data_type:type,nullable:true};
 const operation=randomUUID();
 const created=await call('POST',tablePath+'/fields',body,operation,201);
 assert.deepEqual(await call('POST',tablePath+'/fields',body,operation,201),created);
 await call('POST',tablePath+'/fields',{...body,description:'changed'},operation,409);
 const path='/v1/fields/'+created.field.id;
 const edit=randomUUID();
 const updated=await call('PATCH',path,{alias:'Edited '+name,description:'Smoke field'},edit);
 assert.deepEqual(await call('PATCH',path,{alias:'Edited '+name,description:'Smoke field'},edit),updated);
}
await call('PATCH','/v1/tables/'+randomUUID(),{alias:'missing'},randomUUID(),404);
await call('POST',tablePath+'/fields',{name:'required_number',data_type:'int',nullable:false,is_unique:true},randomUUID(),201);
await Promise.all(['parallel_a','parallel_b'].map(name=>call('POST',tablePath+'/fields',{name,data_type:'int',nullable:true},randomUUID(),201)));
assert.deepEqual(await call('PATCH',tablePath,{alias:'First'},key),first);
await call('PATCH',tablePath,{caption_field:'memo'},randomUUID());
for (const path of [
 '/healthz','/readyz','/v1/tenants',`/v1/tenants/${tenant.id}`,
 `/v1/tenants/${tenant.id}/data-model`,`${tablePath}/options`,`${tablePath}/navigation-options`,
 ...['links','pivots','schema-change-log','schema-migrations','index-jobs'].map(name=>`/v1/tenants/${tenant.id}/${name}`),
]) await call('GET',path);
const fields=(await call('GET',`${tablePath}/fields`)).fields;
assert.equal(fields.length,11);
assert.deepEqual((await call('GET',`/v1/fields/${fields[0].id}/enum-values`)).enum_values,[]);
await call('GET',`/v1/index-jobs/${randomUUID()}`,undefined,undefined,404);
const second=(await call('POST','/v1/tenants',{name:'MySQL scope smoke',external_key:'scope-smoke-'+randomUUID()},undefined,201)).tenant;
assert.deepEqual((await call('GET',`/v1/tenants/${second.id}/tables`)).tables,[]);
console.log(JSON.stringify({tenantId:tenant.id,tableId:table.id,checks:'all mounted operation paths, concurrent edits, six native field types, replay, conflicts, missing resources and tenant-scoped lists'}));
