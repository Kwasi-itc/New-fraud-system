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

const tenant=(await call('POST','/v1/tenants',{name:'Text constraint smoke',external_key:'text-patch-'+randomUUID()},randomUUID(),201)).tenant;
await call('POST',`/v1/tenants/${tenant.id}/provision`);
const table=(await call('POST',`/v1/tenants/${tenant.id}/tables`,{name:'patch_records'},randomUUID(),201)).table;
const tablePath=`/v1/tables/${table.id}/fields`;
const field=(await call('POST',tablePath,{name:'reference',data_type:'string',nullable:true},randomUUID(),201)).field;
const path=`/v1/fields/${field.id}`;
const physical=sql(`SELECT physical_name FROM fraud.data_model_physical_tables WHERE table_id=UNHEX('${hex(table.id)}');`);
const qt='fraud.'+quote(physical);
const columns=Object.fromEntries(sql(`SELECT f.name,p.physical_name FROM fraud.data_model_fields f JOIN fraud.data_model_physical_fields p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id AND p.field_id=f.id WHERE f.table_id=UNHEX('${hex(table.id)}') AND p.component='value';`).split('\n').map(v=>v.split('\t')));
const value=quote(columns.reference),object=quote(columns.object_id),updated=quote(columns.updated_at);
const record=(id,v)=>`INSERT INTO ${qt}(id,${object},${updated},${value}) VALUES(UNHEX('${hex(randomUUID())}'),${text(id)},'2026-11-04 00:00:00',${text(v)});`;
const gate=()=>sql(`SELECT write_state FROM fraud.data_model_physical_tables WHERE table_id=UNHEX('${hex(table.id)}');`);
const current=async()=>(await call('GET',tablePath)).fields.find(f=>f.id===field.id);
for(let offset=0;offset<600;offset+=100) {
 let rows=[];
 for(let i=offset;i<offset+100;i++)rows.push(`(UNHEX('${hex(randomUUID())}'),'batch_${i}','2026-11-04 00:00:00','ref_${i}')`);
 sql(`INSERT INTO ${qt}(id,${object},${updated},${value}) VALUES ${rows.join(',')};`);
}
const long='x'.repeat(40000);
sql(record('long_a',long+'a')+record('long_b',long+'b')+record('null_a',null)+record('null_b',null)+record('empty','')+record('upper','Case')+record('lower','case')+record('space','case '));
await call('PATCH',path,{is_unique:true,nullable:false},randomUUID(),409);
assert.equal(gate(),'open');assert.equal((await current()).is_unique,false);
const enableKey=randomUUID(),enabled=await call('PATCH',path,{is_unique:true,alias:'Unique reference'},enableKey);
assert.equal(enabled.field.is_unique,true);assert.equal(enabled.field.alias,'Unique reference');
assert.equal(gate(),'open');
const hidden=quote('record_text_key_'+hex(field.id));
assert.equal(sql(`SELECT COUNT(*) FROM ${qt} WHERE (${value} IS NULL AND ${hidden} IS NOT NULL) OR (${value} IS NOT NULL AND ${hidden} IS NULL);`),'0');
sql(record('duplicate','ref_511'),1062);
sql(record('long_duplicate',long+'a'),1062);
sql(`UPDATE ${qt} SET ${value}='ref_511' WHERE ${object}='batch_1';`,1062);
const races=await Promise.all([concurrentSQL(record('race_a','race_value')),concurrentSQL(record('race_b','race_value'))]);
assert.equal(races.filter(r=>r.code===0).length,1,JSON.stringify(races));
assert.match(races.find(r=>r.code!==0).stderr,/ERROR 1062 /);
await call('PATCH',path,{is_unique:true}); // no-op catalog result
await call('PATCH',path,{nullable:false},randomUUID(),409);
assert.equal(gate(),'open');
sql(`UPDATE ${qt} SET ${value}=CONCAT('filled_',${object}) WHERE ${value} IS NULL;`);
await call('PATCH',path,{nullable:false});
sql(record('required_null',null),1048);
await call('PATCH',path,{nullable:true});
sql(record('nullable_again',null));
// Only this newly created fixture relationship is inserted/removed.
const link=hex(randomUUID());
sql(`INSERT INTO fraud.data_model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES(UNHEX('${link}'),UNHEX('${hex(tenant.id)}'),'text_dependency',UNHEX('${hex(table.id)}'),UNHEX('${hex(field.id)}'),UNHEX('${hex(table.id)}'),UNHEX('${hex(field.id)}'),UTC_TIMESTAMP(6));`);
await call('PATCH',path,{is_unique:false},randomUUID(),409);
assert.equal(gate(),'open');assert.equal((await current()).is_unique,true);
sql(`DELETE FROM fraud.data_model_links WHERE tenant_id=UNHEX('${hex(tenant.id)}') AND id=UNHEX('${link}');`);
const disableKey=randomUUID(),disabled=await call('PATCH',path,{is_unique:false},disableKey);
assert.equal(disabled.field.is_unique,false);
assert.equal(sql(`SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE EVENT_OBJECT_SCHEMA='fraud' AND EVENT_OBJECT_TABLE='${physical}';`),'2');
sql(record('duplicate_allowed',long+'a'));
await call('PATCH',path,{is_unique:true},randomUUID(),409);
assert.equal(gate(),'open');assert.equal((await current()).is_unique,false);
sql(`UPDATE ${qt} SET ${value}='now_distinct' WHERE ${object}='duplicate_allowed';`);
await call('PATCH',path,{is_unique:true});
sql(record('duplicate_again',long+'a'),1062);
assert.deepEqual(await call('PATCH',path,{is_unique:true,alias:'Unique reference'},enableKey),enabled);
assert.deepEqual(await call('PATCH',path,{is_unique:false},disableKey),disabled);
await call('PATCH',path,{is_unique:false},enableKey,409);
assert.equal((await current()).is_unique,true);
await call('POST',tablePath,{name:'score',data_type:'int',nullable:true},randomUUID(),201);
sql(record('after_native_duplicate','ref_511'),1062);
await call('PATCH',path,{is_unique:false,is_enum:true});
assert.equal((await current()).is_enum,true);
await call('PATCH',path,{is_unique:true,is_enum:false});
assert.equal((await current()).is_enum,false);
sql(record('final_duplicate','ref_511'),1062);
const installer='lcn_'+hex(table.id).slice(0,24);
assert.equal(sql(`SELECT account_locked FROM mysql.user WHERE user='${installer}' AND host='localhost';`),'Y');
assert.equal(sql(`SELECT COUNT(*) FROM mysql.global_grants WHERE user='${installer}';`),'0');
assert.equal(sql(`SELECT COUNT(*) FROM mysql.columns_priv WHERE User='${installer}' AND Db='fraud' AND Table_name='${physical}' AND Column_priv LIKE '%Update%';`),'0');
// Inspect the exact duplicate-candidate access pattern on the test records.
const probe=`SELECT 1 FROM dm_text_batch c STRAIGHT_JOIN dm_text_seen s FORCE INDEX(digest_id) ON s.digest=c.digest AND s.id<c.id STRAIGHT_JOIN ${qt} a ON a.id=c.id STRAIGHT_JOIN ${qt} b ON b.id=s.id WHERE CAST(a.${value} AS BINARY)=CAST(b.${value} AS BINARY) LIMIT 1`;
const explain=sql(`CREATE TEMPORARY TABLE dm_text_seen(id BINARY(16) PRIMARY KEY,digest BINARY(32),INDEX digest_id(digest,id)) ENGINE=InnoDB; CREATE TEMPORARY TABLE dm_text_batch LIKE dm_text_seen; INSERT INTO dm_text_seen SELECT id,UNHEX(SHA2(CAST(${value} AS BINARY),256)) FROM ${qt} WHERE ${value} IS NOT NULL; INSERT INTO dm_text_batch SELECT * FROM dm_text_seen LIMIT 256; EXPLAIN ${probe};`);
assert.match(explain,/digest_id/);assert.match(explain,/eq_ref/);
console.log(explain);
console.log(JSON.stringify({tenantId:tenant.id,tableId:table.id,fieldId:field.id,checks:'600+ record backfill; 40KB prefix equality; duplicate insert/update; concurrent duplicates; nullable/required transitions; dependency rejection; disable/reenable; duplicate rollback leaves gate open; original receipts; later native changes; enum combination; temporary grant cleanup; indexed duplicate probe'}));
