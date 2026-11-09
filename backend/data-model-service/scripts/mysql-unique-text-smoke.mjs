import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';
import {spawnSync,spawn} from 'node:child_process';

// Retains its own new tenant/table. No destructive setup or application reset.
const origin=process.env.DATA_MODEL_LOCAL_ORIGIN??'http://localhost:8080';
async function call(method,path,body,key=randomUUID(),status=200) {
 const response=await fetch(origin+path,{method,headers:{'Content-Type':'application/json','Idempotency-Key':key},body:body===undefined?undefined:JSON.stringify(body)});
 const raw=await response.text();assert.equal(response.status,status,`${method} ${path}: ${raw}`);return raw?JSON.parse(raw):null;
}
const mysqlArgs=['exec','-i','fraud-mysql-dev-mysql-1','sh','-c','MYSQL_PWD="$(cat /run/secrets/mysql_root_password)" exec mysql --protocol=socket -uroot --database=fraud --batch --skip-column-names'];
function sql(query,errorCode) {
 const r=spawnSync('docker',mysqlArgs,{input:query,encoding:'utf8'});
 if(errorCode) {assert.notEqual(r.status,0);assert.match(r.stderr,new RegExp(`ERROR ${errorCode} `));}
 else assert.equal(r.status,0,r.stderr);
 return r.stdout.trim();
}
function concurrentSQL(query) {return new Promise((resolve,reject)=>{const p=spawn('docker',mysqlArgs);let stderr='';p.stderr.on('data',v=>stderr+=v);p.on('error',reject);p.on('close',code=>resolve({code,stderr}));p.stdin.end(query);});}
const hex=id=>id.replaceAll('-','');
const quote=name=>{assert.match(name,/^[a-z0-9_]+$/);return '`'+name+'`';};
const text=value=>value===null?'NULL':`CONVERT(UNHEX('${Buffer.from(value).toString('hex')}') USING utf8mb4)`;
const tenant=(await call('POST','/v1/tenants',{name:'Unique text smoke',external_key:'text-'+randomUUID()},randomUUID(),201)).tenant;
await call('POST',`/v1/tenants/${tenant.id}/provision`);
const table=(await call('POST',`/v1/tenants/${tenant.id}/tables`,{name:'text_records'},randomUUID(),201)).table;
const path=`/v1/tables/${table.id}/fields`;
const key=randomUUID(),body={name:'external_ref',data_type:'string',nullable:true,is_unique:true};
const created=await call('POST',path,body,key,201);
assert.equal(created.field.is_unique,true);
assert.deepEqual(await call('POST',path,body,key,201),created);
await call('POST',path,{...body,nullable:false},key,409);
const physical=sql(`SELECT physical_name FROM fraud.data_model_physical_tables WHERE table_id=UNHEX('${hex(table.id)}');`);
const qt='fraud.'+quote(physical);
const columns=()=>Object.fromEntries(sql(`SELECT f.name,p.physical_name FROM fraud.data_model_fields f JOIN fraud.data_model_physical_fields p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id AND p.field_id=f.id WHERE f.table_id=UNHEX('${hex(table.id)}') AND p.component='value';`).split('\n').map(v=>v.split('\t')));
let c=columns();
const record=(object,value)=>`INSERT INTO ${qt}(id,${quote(c.object_id)},${quote(c.updated_at)},${quote(c.external_ref)}) VALUES(UNHEX('${hex(randomUUID())}'),${text(object)},'2026-11-04 00:00:00',${text(value)});`;
const long='x'.repeat(20000);
sql(record('long_a',long+'a')+record('long_b',long+'b'));
sql(record('duplicate',long+'a'),1062);
sql(record('null_a',null)+record('null_b',null)+record('empty','')+record('case_a','Case')+record('case_b','case')+record('space','case '));
sql(record('empty_duplicate',''),1062);
sql(`UPDATE ${qt} SET ${quote(c.external_ref)}=${text(long+'a')} WHERE ${quote(c.object_id)}='long_b';`,1062);
assert.equal(sql(`SELECT RIGHT(${quote(c.external_ref)},1) FROM ${qt} WHERE ${quote(c.object_id)}='long_b';`),'b');
const hidden=quote('record_text_key_'+hex(created.field.id));
sql(`UPDATE ${qt} SET ${hidden}=UNHEX('${hex(randomUUID())}') WHERE ${quote(c.object_id)}='long_a';`);
sql(record('forged_key_cannot_bypass',long+'a'),1062);
sql(`UPDATE ${qt} SET ${quote(c.external_ref)}=NULL WHERE ${quote(c.object_id)}='long_a';`);
sql(record('released_value',long+'a'));
sql('START TRANSACTION;'+record('rolled_back','rollback_value')+'ROLLBACK;');
sql(record('after_rollback','rollback_value'));
const races=await Promise.all([concurrentSQL(record('race_a','race_value')),concurrentSQL(record('race_b','race_value'))]);
assert.equal(races.filter(r=>r.code===0).length,1,JSON.stringify(races));
assert.match(races.find(r=>r.code!==0).stderr,/ERROR 1062 /);
// Nullable additions work on populated tables; required additions fail atomically.
const second=(await call('POST',path,{name:'second_ref',data_type:'string',nullable:true,is_unique:true},randomUUID(),201)).field;
await call('POST',path,{name:'required_ref',data_type:'string',nullable:false,is_unique:true},randomUUID(),409);
assert.equal(sql(`SELECT write_state FROM fraud.data_model_physical_tables WHERE table_id=UNHEX('${hex(table.id)}');`),'open');
assert.ok(!(await call('GET',path)).fields.some(f=>f.name==='required_ref'));
const native=(await call('POST',path,{name:'score',data_type:'int',nullable:true},randomUUID(),201)).field;
await call('PATCH',`/v1/fields/${native.id}`,{is_unique:true});
c=columns();
sql(`UPDATE ${qt} SET ${quote(c.second_ref)}='same_value' WHERE ${quote(c.object_id)}='case_a';`);
sql(`UPDATE ${qt} SET ${quote(c.external_ref)}='same_value' WHERE ${quote(c.object_id)}='case_b';`);
sql(`UPDATE ${qt} SET ${quote(c.second_ref)}='same_value' WHERE ${quote(c.object_id)}='case_b';`,1062);
sql(record('still_duplicate',long+'a'),1062);
assert.deepEqual(await call('POST',path,body,key,201),created);
assert.equal(sql(`SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE EVENT_OBJECT_SCHEMA='fraud' AND EVENT_OBJECT_TABLE='${physical}';`),'6');
const installer='lcn_'+hex(table.id).slice(0,24);
assert.equal(sql(`SELECT account_locked FROM mysql.user WHERE user='${installer}' AND host='localhost';`),'Y');
assert.equal(sql(`SELECT COUNT(*) FROM mysql.global_grants WHERE user='${installer}';`),'0');
assert.equal(sql(`SELECT COUNT(*) FROM performance_schema.threads WHERE PROCESSLIST_USER='${installer}';`),'0');
// Identical text in a different tenant/table is independent.
const otherTenant=(await call('POST','/v1/tenants',{name:'Text isolation',external_key:'text-isolation-'+randomUUID()},randomUUID(),201)).tenant;
await call('POST',`/v1/tenants/${otherTenant.id}/provision`);
const other=(await call('POST',`/v1/tenants/${otherTenant.id}/tables`,{name:'text_records'},randomUUID(),201)).table;
await call('POST',`/v1/tables/${other.id}/fields`,{...body,nullable:false},randomUUID(),201);
const otherPhysical=sql(`SELECT physical_name FROM fraud.data_model_physical_tables WHERE table_id=UNHEX('${hex(other.id)}');`);
const otherCols=Object.fromEntries(sql(`SELECT f.name,p.physical_name FROM fraud.data_model_fields f JOIN fraud.data_model_physical_fields p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id AND p.field_id=f.id WHERE f.table_id=UNHEX('${hex(other.id)}');`).split('\n').map(v=>v.split('\t')));
sql(`INSERT INTO fraud.${quote(otherPhysical)}(id,${quote(otherCols.object_id)},${quote(otherCols.updated_at)},${quote(otherCols.external_ref)}) VALUES(UNHEX('${hex(randomUUID())}'),'long_a','2026-11-04 00:00:00',${text(long+'a')});`);
console.log(JSON.stringify({tenantId:tenant.id,tableId:table.id,fieldId:created.field.id,secondFieldId:second.id,isolationTenantId:otherTenant.id,checks:'full long values, duplicate insert/update, multiple NULLs, empty/case/trailing spaces, derived keys, rollback, concurrent duplicate, populated-table additions, required-field rollback, later native changes, replay/conflict, scope isolation, installer deactivation'}));
