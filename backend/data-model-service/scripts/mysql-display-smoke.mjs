import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';
import {spawnSync} from 'node:child_process';

// Only newly allocated test resources are changed; no database/volume reset.
const origin=process.env.DATA_MODEL_LOCAL_ORIGIN??'http://localhost:8080';
async function call(method,path,body,status=200,key=randomUUID()){
 const r=await fetch(origin+path,{method,headers:{'Content-Type':'application/json','Idempotency-Key':key},body:body===undefined?undefined:JSON.stringify(body)});
 const raw=await r.text();assert.equal(r.status,status,`${method} ${path}: ${raw}`);return raw?JSON.parse(raw):null;
}
function sql(query){const r=spawnSync('docker',['exec','-i','fraud-mysql-dev-mysql-1','sh','-c','MYSQL_PWD="$(cat /run/secrets/mysql_root_password)" exec mysql --protocol=socket -uroot --database=fraud --batch --skip-column-names'],{input:query,encoding:'utf8'});assert.equal(r.status,0,r.stderr);return r.stdout.trim()}
const hex=id=>{assert.match(id,/^[a-f0-9-]{36}$/);return `UNHEX('${id.replaceAll('-','')}')`};
const quote=s=>{assert.match(s,/^[a-z0-9_]+$/);return '`'+s+'`'};
const makeTenant=async()=>{const t=(await call('POST','/v1/tenants',{name:'Display acceptance',external_key:'display-'+randomUUID()},201)).tenant;await call('POST',`/v1/tenants/${t.id}/provision`);return t};
const tenant=await makeTenant();
const createTable=async name=>(await call('POST',`/v1/tenants/${tenant.id}/tables`,{name},201)).table;
const parent=await createTable('display_parent'),child=await createTable('display_child');
const create=async(table,name,type='string',extra={})=>(await call('POST',`/v1/tables/${table.id}/fields`,{name,data_type:type,nullable:true,...extra},201)).field;
const parentKey=await create(parent,'reference','string',{is_unique:true});
const title=await create(parent,'title');
const ref=await create(child,'reference'),name=await create(child,'name'),flag=await create(child,'flag','bool'),score=await create(child,'score','int'),amount=await create(child,'amount','float'),ip=await create(child,'address','ip_address');
const state=await create(child,'state','string',{is_enum:true,enum_values:[{value:'open',label:'Open state'}]});
const path=t=>`/v1/tables/${t.id}/display-settings`;
const initial=await call('GET',path(child));assert.equal(initial.table_id,child.id);
const get=async t=>call('GET',path(t));
await call('PUT',path(child),{},400);
await call('PUT',path(child),{version:-1},400);
await call('PUT',path(child),{version:initial.version,unknown:1},400);
await call('PUT',path(child),{version:initial.version,caption_field:'score'},400);
await call('PUT',path(child),{version:initial.version,caption_field:'',displayed_fields:[title.id]},400);
const direct={version:initial.version,caption_field:'name',displayed_fields:[name.id,score.id,amount.id,flag.id,ip.id,state.id],field_order:[score.id,name.id,amount.id,flag.id,ip.id,state.id],field_display:{[score.id]:{label:'Risk score'}}};
const operation=randomUUID();const saved=await call('PUT',path(child),direct,200,operation);assert.ok(saved.version>initial.version);
await call('PUT',path(child),direct,409);
assert.deepEqual(await call('PUT',path(child),direct,200,operation),saved);
await call('PUT',path(child),{...direct,caption_field:''},409,operation);
const physical=t=>sql(`SELECT physical_name FROM fraud.data_model_physical_tables WHERE table_id=${hex(t.id)};`);
const mappings=t=>Object.fromEntries(sql(`SELECT CONCAT(f.name,'.',p.component),p.physical_name FROM fraud.data_model_fields f JOIN fraud.data_model_physical_fields p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id AND p.field_id=f.id WHERE f.table_id=${hex(t.id)};`).split('\n').map(v=>v.split('\t')));
const pc=mappings(parent),cc=mappings(child),pn=physical(parent),cn=physical(child);
sql(`INSERT INTO fraud.${quote(pn)}(id,${quote(pc['object_id.value'])},${quote(pc['updated_at.value'])},${quote(pc['reference.value'])},${quote(pc['title.value'])}) VALUES(${hex(randomUUID())},'parent-1','2026-11-04 00:00:00','Account A','Parent caption');`);
sql(`INSERT INTO fraud.${quote(cn)}(id,${quote(cc['object_id.value'])},${quote(cc['updated_at.value'])},${quote(cc['reference.value'])},${quote(cc['name.value'])},${quote(cc['score.value'])},${quote(cc['amount.value'])},${quote(cc['flag.value'])},${quote(cc['address.address'])},${quote(cc['address.family'])},${quote(cc['address.prefix'])},${quote(cc['state.value'])}) VALUES(${hex(randomUUID())},'child-1','2026-11-04 00:00:00','Account A','Child caption',7,12.5,1,INET6_ATON('192.0.2.1'),4,32,'open');`);
const read=objects=>call('POST',`/v1/tenants/${tenant.id}/record-displays`,{objects});
const obj=(type,id)=>({object_type:type,object_id:id});
const first=await read([obj(child.name,'child-1'),obj(child.name,'missing'),obj('unknown','child-1'),obj(child.name,'child-1')]);
assert.equal(first.records[0].caption,'Child caption');assert.equal(first.records[0].values[score.id],7);assert.equal(first.records[0].values[amount.id],12.5);assert.equal(first.records[0].values[flag.id],true);assert.equal(first.records[0].values[ip.id],'192.0.2.1');assert.equal(first.records[0].values[state.id],'Open state');
assert.equal(first.columns[child.name][0].label,'Risk score');assert.deepEqual(first.records[0],first.records[3]);assert.equal(first.records[1].found,false);assert.equal(first.records[2].found,false);
await call('POST',`/v1/tenants/${tenant.id}/record-displays`,{objects:[]},400);
await call('POST',`/v1/tenants/${tenant.id}/record-displays`,{objects:Array.from({length:101},()=>obj(child.name,'child-1'))},400);
await call('GET',`/v1/tables/${randomUUID()}/display-settings`,undefined,404);
const parentSettings=await get(parent);await call('PUT',path(parent),{version:parentSettings.version,caption_field:'title',displayed_fields:[title.id]});
const link=randomUUID();sql(`INSERT INTO fraud.data_model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES(${hex(link)},${hex(tenant.id)},'parent_display',${hex(parent.id)},${hex(parentKey.id)},${hex(child.id)},${hex(ref.id)},UTC_TIMESTAMP(6));`);
const current=await get(child);assert.equal(current.links[0].id,link);
await call('PUT',path(child),{version:current.version,caption_field:'name',display_columns:[{id:'bad',type:'relation',link_id:link,target_field_id:score.id}]},400);
const relationInput={version:current.version,caption_field:'name',display_columns:[{id:'title',type:'relation',link_id:link,target_field_id:title.id},{id:'risk',type:'direct',field_id:score.id,label:'Score'}]};
const related=await call('PUT',path(child),relationInput);
const joined=await read([obj(child.name,'child-1')]);assert.equal(joined.records[0].values.title,'Parent caption');assert.equal(joined.records[0].values.risk,7);
sql(`UPDATE fraud.${quote(cn)} SET ${quote(cc['reference.value'])}='Account missing' WHERE ${quote(cc['object_id.value'])}='child-1';`);
assert.equal((await read([obj(child.name,'child-1')])).records[0].values.title,'Account missing');
sql(`UPDATE fraud.${quote(cn)} SET ${quote(cc['reference.value'])}=NULL WHERE ${quote(cc['object_id.value'])}='child-1';`);
assert.equal((await read([obj(child.name,'child-1')])).records[0].values.title,null);
// Optimistic concurrency: exactly one distinct command can consume a version.
const statuses=await Promise.all(['One','Two'].map(label=>fetch(origin+path(child),{method:'PUT',headers:{'Content-Type':'application/json','Idempotency-Key':randomUUID()},body:JSON.stringify({...relationInput,version:related.version,display_columns:[{id:'risk',type:'direct',field_id:score.id,label}]})}).then(r=>r.status)));
assert.deepEqual(statuses.sort(),[200,409]);
assert.deepEqual(await call('PUT',path(child),direct,200,operation),saved);
const foreign=await makeTenant();const isolated=await call('POST',`/v1/tenants/${foreign.id}/record-displays`,{objects:[obj(child.name,'child-1')]});assert.equal(isolated.records[0].found,false);
sql(`UPDATE fraud.data_model_physical_tables SET write_state='closed' WHERE table_id=${hex(child.id)};`);
try{await call('POST',`/v1/tenants/${tenant.id}/record-displays`,{objects:[obj(child.name,'child-1')]},503)}finally{sql(`UPDATE fraud.data_model_physical_tables SET write_state='open' WHERE table_id=${hex(child.id)};`)}
sql(`UPDATE fraud.${quote(cn)} SET valid_until='2026-11-04 00:00:01' WHERE ${quote(cc['object_id.value'])}='child-1';`);
assert.equal((await read([obj(child.name,'child-1')])).records[0].found,false);
sql(`UPDATE fraud.${quote(cn)} SET valid_until=NULL WHERE ${quote(cc['object_id.value'])}='child-1';`);
// Exercise the maximum request size against a larger physical record set.
const bulk=Array.from({length:1000},(_,i)=>`(${hex(randomUUID())},'batch-${i}','2026-11-04 00:00:00',${i})`).join(',');
sql(`INSERT INTO fraud.${quote(cn)}(id,${quote(cc['object_id.value'])},${quote(cc['updated_at.value'])},${quote(cc['score.value'])}) VALUES ${bulk};`);
const batch=await read(Array.from({length:100},(_,i)=>obj(child.name,`batch-${i*10}`)));
assert.equal(batch.records.length,100);
for(let i=0;i<100;i++){assert.equal(batch.records[i].found,true);assert.equal(batch.records[i].values.risk,i*10)}
const objectField=(await get(child)).fields.find(f=>f.name==='object_id');
const plan=sql(`EXPLAIN SELECT r.id FROM (SELECT fraud.data_model_find_record_object_key_v1(${hex(tenant.id)},${hex(child.id)},'child-1') AS key_id LIMIT 100) requested STRAIGHT_JOIN fraud.${quote(cn)} r FORCE INDEX (record_unique_${objectField.id.replaceAll('-','')}) ON r.record_object_key_id=requested.key_id WHERE r.valid_until IS NULL;`);
assert.ok(plan.split('\n').some(row=>{const c=row.split('\t');return c[2]==='r'&&['eq_ref','ref','const'].includes(c[4])}),plan);console.log(plan);
console.log(JSON.stringify({passed:true,tenant:tenant.id,child:child.id,parent:parent.id,checks:'version conflicts, historical replay, atomic caption jobs, direct types, enum labels, ordered batches, related exact keys, fallback/null, tenant isolation, closed gate, current records, indexed lookup'}));
