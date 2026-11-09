import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';

// Allocates new tenants only. Does not reset the shared database or delete data.
const base=process.env.DATA_MODEL_LOCAL_ORIGIN??'http://localhost:8080';
async function call(method,path,body,status=200,key=randomUUID()){
 const r=await fetch(base+path,{method,headers:{'Content-Type':'application/json','Idempotency-Key':key},body:body===undefined?undefined:JSON.stringify(body)});
 const text=await r.text();assert.equal(r.status,status,`${method} ${path}: ${text}`);return text?JSON.parse(text):null;
}
async function tenant(){const t=(await call('POST','/v1/tenants',{name:'Portable acceptance',external_key:'portable-'+randomUUID()},201)).tenant;await call('POST',`/v1/tenants/${t.id}/provision`);return t.id}
const path=id=>`/v1/tenants/${id}/data-model`;
const field=(name,type='string',extra={})=>({name,alias:name+' alias',description:'Portable field',data_type:type,nullable:true,is_enum:false,is_unique:false,enum_values:[],...extra});
const document={version:'v1',tables:[
 {name:'accounts',alias:'Accounts',description:'Parent records',semantic_type:'entity',caption_field:'name',fields:[field('reference','string',{is_unique:true}),field('name')],options:{displayed_fields:['name'],field_order:['name','reference','object_id','updated_at']},navigation_options:[{source_field:'reference',target_table:'transactions',filter_field:'account_ref',ordering_field:'updated_at'}]},
 {name:'transactions',alias:'Transactions',description:'Child records',semantic_type:'entity',caption_field:'',fields:[field('account_ref'),field('amount','float'),field('state','string',{is_enum:true,enum_values:[{value:'open',label:'Open',sort_order:1},{value:'closed',label:'Closed',sort_order:2}]})],options:{displayed_fields:['amount','state'],field_order:['amount','state','account_ref','object_id','updated_at']},navigation_options:[]}
],links:[{name:'account',parent_table:'accounts',parent_field:'reference',child_table:'transactions',child_field:'account_ref'}],pivots:[{base_table:'accounts',field:'reference'},{base_table:'transactions',path_links:['account']}]};
const source=await tenant();
const invalid=structuredClone(document);invalid.tables[1].fields[0].data_type='unsupported';
await call('POST',path(source)+'/import',{data_model:invalid},400);
assert.equal((await call('GET',path(source)+'/export')).data_model.tables.length,0);
const key=randomUUID();
const imported=await call('POST',path(source)+'/import',{data_model:document},200,key);
assert.deepEqual({...imported.summary,revision_id:''},{tables_created:2,fields_created:5,links_created:1,pivots_created:2,table_options_upserted:2,navigation_options_created:1,revision_id:''});
assert.deepEqual(await call('POST',path(source)+'/import',{data_model:document},200,key),imported);
await call('POST',path(source)+'/import',{data_model:{...document,revision_id:'changed'}},409,key);
await call('POST',path(source)+'/import',{data_model:document},409);
const exported=(await call('GET',path(source)+'/export')).data_model;
assert.equal(exported.tables[0].fields.find(f=>f.name==='name').alias,'name alias');
assert.equal(exported.links.length,1);assert.equal(exported.pivots.length,2);assert.equal(exported.tables[0].navigation_options.length,1);
const target=await tenant();assert.equal((await call('GET',path(target)+'/export')).data_model.tables.length,0);
await call('POST',path(target)+'/import',{data_model:exported});
const again=(await call('GET',path(target)+'/export')).data_model;
assert.deepEqual({...again,revision_id:''},{...exported,revision_id:''});
const model=(await call('GET',path(target))).data_model;
assert.notEqual(model.tables.accounts.id,(await call('GET',path(source))).data_model.tables.accounts.id);
assert.equal((await call('GET',`/v1/tables/${model.tables.accounts.id}/display-settings`)).caption_field,'name');
await call('GET',path(randomUUID())+'/export',undefined,404);
await call('POST',path(randomUUID())+'/import',{data_model:{version:'v1',tables:[]}},404);
// Historical completion response survives later schema changes.
await call('POST',`/v1/tenants/${source}/tables`,{name:'later_table'},201);
assert.deepEqual(await call('POST',path(source)+'/import',{data_model:document},200,key),imported);
// Competing same-key callers either replay or receive a retryable busy response.
const concurrent=await tenant(),concurrentKey=randomUUID(),small={version:'v1',tables:[{name:'retry_table',fields:[field('value')]}]};
const send=()=>fetch(base+path(concurrent)+'/import',{method:'POST',headers:{'Content-Type':'application/json','Idempotency-Key':concurrentKey},body:JSON.stringify({data_model:small})});
const responses=await Promise.all([send(),send()]);for(const r of responses){assert.ok([200,503].includes(r.status),await r.text())}
const stable=await call('POST',path(concurrent)+'/import',{data_model:small},200,concurrentKey);
assert.equal(stable.summary.tables_created,1);assert.equal((await call('GET',path(concurrent)+'/export')).data_model.tables.length,1);
// Cancel after observing allocated work, then resume with the original key/body.
const interrupted=await tenant(),resumeKey=randomUUID();
const resumeDocument={version:'v1',tables:[{name:'resumable',fields:Array.from({length:8},(_,i)=>field('value_'+i))}]};
const controller=new AbortController();let settled=false;
const pending=fetch(base+path(interrupted)+'/import',{method:'POST',signal:controller.signal,headers:{'Content-Type':'application/json','Idempotency-Key':resumeKey},body:JSON.stringify({data_model:resumeDocument})}).then(async r=>{settled=true;return {status:r.status,body:await r.text()}}).catch(e=>{settled=true;return {aborted:e.name==='AbortError'}});
let observed=false;
for(let i=0;i<100&&!settled;i++){const rows=await call('GET',`/v1/tenants/${interrupted}/tables`);if(rows.tables.length){observed=true;controller.abort();break}await new Promise(r=>setTimeout(r,30))}
const cancelled=await pending;assert.ok(observed&&cancelled.aborted,JSON.stringify(cancelled));
let resumed;
for(let i=0;i<15;i++){
 const r=await fetch(base+path(interrupted)+'/import',{method:'POST',headers:{'Content-Type':'application/json','Idempotency-Key':resumeKey},body:JSON.stringify({data_model:resumeDocument})});
 const body=await r.json();if(r.status===200){resumed=body;break}assert.equal(r.status,503,JSON.stringify(body));await new Promise(r=>setTimeout(r,1000));
}
assert.ok(resumed,'interrupted import did not recover');assert.equal(resumed.summary.fields_created,8);
const recovered=(await call('GET',path(interrupted)+'/export')).data_model;assert.equal(recovered.tables.length,1);assert.equal(recovered.tables[0].fields.length,8);
console.log(JSON.stringify({passed:true,source,target,concurrent,interrupted,checks:'full portable round trip; aliases; typed fields/enums; captions/options; links/pivots/navigation; tenant isolation; invalid graph before writes; conflict; historical/concurrent replay; cancellation after allocation and resume'}));
