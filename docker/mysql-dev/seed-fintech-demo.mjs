// Local-only, retained demo data. Credentials and resumable state stay in .local.
import assert from 'node:assert/strict';
import {createHash,randomBytes,randomUUID} from 'node:crypto';
import {existsSync,mkdirSync,readFileSync,readdirSync,writeFileSync,renameSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';
import {localOrigin} from '../../frontend/scripts/mysql-origin.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url));
const privateDir=resolve(root,'.local/mysql-dev/novapay-demo');
mkdirSync(privateDir,{recursive:true});
const stateFile=resolve(privateDir,'state.json');
const state=existsSync(stateFile)?JSON.parse(readFileSync(stateFile,'utf8')):{version:1,anchor:new Date().toISOString(),tables:{},scenarios:{},evaluations:{},cases:{}};
assert.equal(state.version,1,'Unsupported saved demo state');
function save(){writeFileSync(stateFile+'.tmp',JSON.stringify(state,null,2)+'\n');renameSync(stateFile+'.tmp',stateFile);}
save();
const origin=localOrigin(process.env.FRAUD_APP_ORIGIN||'http://localhost:'+(process.env.MYSQL_GATEWAY_PORT||8088)),username='novapay.demo';
const company={name:'NovaPay Fintech — Demo',external_key:'local-novapay-fintech-demo-v1'};
let browser,context;
async function direct(port,method,path,body,want=200,extra={}){
 const owner={8080:'data-model',8081:'ingestion',8082:'decisions',8085:'screening',8086:'cases',8087:'auth'}[port];
 assert(owner,'Unknown demo service');
 const response=await fetch(`${origin}/services/${owner}/v1${path}`,{method,headers:{'Content-Type':'application/json',...extra},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(90000)});
 const data=response.status===204?null:await response.json();
 assert.ok([want].flat().includes(response.status),`${method} :${port}${path}: HTTP ${response.status}: ${JSON.stringify(data?.error??'').slice(0,350)}`);
 return data;
}
async function api(service,method,path,body,want=200){
 const response=await context.request.fetch(origin+'/api/'+service+path,{method,headers:{Origin:origin},data:body,timeout:90000});
 const data=response.status()===204?null:await response.json();
 assert.ok([want].flat().includes(response.status()),`${method} ${service}${path}: HTTP ${response.status()}: ${JSON.stringify(data?.error??'').slice(0,350)}`);
 return data;
}
async function until(run,check,label){for(let n=0;n<90;n++){const value=await run();if(check(value))return value;await delay(1000);}throw Error('Timed out waiting for '+label);}
const operation=label=>{const bytes=createHash('sha256').update(company.external_key+'\0'+label).digest().subarray(0,16);bytes[6]=(bytes[6]&15)|80;bytes[8]=(bytes[8]&63)|128;const h=bytes.toString('hex');return `${h.slice(0,8)}-${h.slice(8,12)}-${h.slice(12,16)}-${h.slice(16,20)}-${h.slice(20)}`;};
const at=minutes=>new Date(Date.parse(state.anchor)+minutes*60000).toISOString();

const models=[
 {name:'customers',alias:'Customers',description:'Synthetic NovaPay customers; no real personal data.',fields:[['full_name','string'],['email','string'],['country','string'],['kyc_status','string'],['risk_band','string'],['onboarded_at','timestamp']]},
 {name:'wallets',alias:'Wallets',description:'Demo GHS wallets linked to customers.',fields:[['customer_id','string'],['wallet_number','string'],['currency','string'],['balance','float'],['status','string'],['opened_at','timestamp']]},
 {name:'transactions',alias:'Transactions',description:'Synthetic wallet transfers and card payments. Risk features are supplied demo inputs, not a trained model.',fields:[['customer_id','string'],['wallet_id','string'],['amount','float'],['currency','string'],['country','string'],['transaction_type','string'],['channel','string'],['merchant_category','string'],['beneficiary_id','string'],['beneficiary_name','string'],['beneficiary_company','string'],['is_new_beneficiary','bool'],['is_new_device','bool'],['customer_age_days','int'],['source_risk_score','int'],['occurred_at','timestamp'],['description','string']]},
];
const field=name=>({function:'field_ref',named_children:{field:{constant:name}}});
const constant=value=>({constant:value});
const fn=(name,...children)=>({function:name,children});
const filter=(name,operator,value)=>({function:'Filter',named_children:{tableName:constant('transactions'),fieldName:constant(name),operator:constant(operator),value}});
const velocity={function:'Aggregator',named_children:{tableName:constant('transactions'),fieldName:constant('object_id'),aggregator:constant('COUNT'),filters:{function:'And',children:[filter('wallet_id','=',field('wallet_id')),filter('occurred_at','>=',{function:'TimeAdd',named_children:{timestampField:field('occurred_at'),duration:constant('PT10M'),sign:constant('-')}}),filter('occurred_at','<',field('occurred_at'))]}}};
const definitions=[
 {key:'transfers',name:'Wallet transfer fraud',description:'Demo transfer policy: amount, account age, new device/beneficiary, supplied risk and a real MySQL history aggregate.',trigger:'transaction_type == "wallet_transfer"',thresholds:[30,70,100],rules:[
  ['High-value transfer','amount >= 10000',40],['New account sending a large transfer','customer_age_days < 7 and amount >= 2000',25],['New device with a large transfer','is_new_device and amount >= 1000',20],['New beneficiary with a large transfer','is_new_beneficiary and amount >= 2000',20],['High supplied source risk','source_risk_score >= 85',30],['Five earlier transfers in ten minutes',fn('gte',velocity,constant(5)),40],
 ]},
 {key:'cards',name:'Card payment risk',description:'Synthetic card-not-present and foreign-payment examples; thresholds illustrate a demo policy.',trigger:'transaction_type == "card_payment"',thresholds:[30,70,100],rules:[
  ['Large card-not-present payment','channel == "card_not_present" and amount >= 1500',25],['Large payment outside domestic market','country != "GH" and amount >= 2000',35],['High supplied card risk','source_risk_score >= 85',40],['Very large card payment','amount >= 10000',35],
 ]},
 {key:'screening',name:'Beneficiary screening',description:'Synthetic development catalog only. A possible person match forces block and review; a company match forces review. This is not real sanctions coverage.',trigger:'transaction_type == "wallet_transfer"',thresholds:[30,70,100],rules:[['Very large outbound transfer','amount >= 25000',35]]},
];
const base={customer_id:'NP-CUSTOMER-001',wallet_id:'NP-WALLET-001',amount:125,currency:'GHS',country:'GH',transaction_type:'wallet_transfer',channel:'mobile_app',merchant_category:'4829',beneficiary_id:'NP-BEN-CLEAR',beneficiary_name:'Dalia Demo',beneficiary_company:'',is_new_beneficiary:false,is_new_device:false,customer_age_days:180,source_risk_score:5,occurred_at:at(0)};
const examples=[
 {id:'NP-TX-001',label:'Everyday wallet transfer',patch:{amount:125},expected:{transfers:[0,'approve'],screening:[0,'approve']}},
 {id:'NP-TX-002',label:'High-value transfer',patch:{amount:12000},expected:{transfers:[40,'review'],screening:[0,'approve']}},
 {id:'NP-TX-003',label:'New customer, device and beneficiary',patch:{customer_id:'NP-CUSTOMER-NEW',wallet_id:'NP-WALLET-NEW',amount:2500,customer_age_days:2,is_new_beneficiary:true,is_new_device:true},expected:{transfers:[65,'review'],screening:[0,'approve']}},
 {id:'NP-TX-004',label:'Possible account takeover',patch:{amount:5000,is_new_device:true,is_new_beneficiary:true,source_risk_score:90},expected:{transfers:[70,'block_and_review'],screening:[0,'approve']}},
 {id:'NP-TX-005',label:'Burst of recent wallet transfers',patch:{customer_id:'NP-CUSTOMER-HOT',wallet_id:'NP-WALLET-HOT',amount:250},expected:{transfers:[40,'review'],screening:[0,'approve']}},
 {id:'NP-TX-006',label:'Combined severe transfer signals',patch:{customer_id:'NP-CUSTOMER-HOT',wallet_id:'NP-WALLET-HOT',amount:20000,customer_age_days:2,is_new_beneficiary:true,is_new_device:true,source_risk_score:95},expected:{transfers:[175,'decline'],screening:[0,'approve']}},
 {id:'NP-TX-007',label:'Synthetic person match — Alex Example',patch:{amount:300,beneficiary_id:'NP-BEN-ALEX',beneficiary_name:'Alex Example'},expected:{transfers:[0,'approve'],screening:[0,'block_and_review']}},
 {id:'NP-TX-008',label:'Synthetic person match — Morgan Sample',patch:{amount:450,beneficiary_id:'NP-BEN-MORGAN',beneficiary_name:'Morgan Sample'},expected:{transfers:[0,'approve'],screening:[0,'block_and_review']}},
 {id:'NP-TX-009',label:'Synthetic organization match',patch:{amount:600,beneficiary_id:'NP-BEN-COMPANY',beneficiary_company:'Example Trading Limited'},expected:{transfers:[0,'approve'],screening:[0,'review']}},
 {id:'NP-TX-010',label:'Everyday domestic card payment',patch:{transaction_type:'card_payment',channel:'pos',amount:80,merchant_category:'5411'},expected:{cards:[0,'approve']}},
 {id:'NP-TX-011',label:'Foreign card-not-present payment',patch:{transaction_type:'card_payment',channel:'card_not_present',amount:3000,country:'GB',merchant_category:'5732'},expected:{cards:[60,'review']}},
 {id:'NP-TX-012',label:'Severe card payment signals',patch:{transaction_type:'card_payment',channel:'card_not_present',amount:15000,country:'GB',source_risk_score:95,merchant_category:'5732'},expected:{cards:[135,'decline']}},
];

async function prepareModel(){
 const tenants=(await direct(8080,'GET','/tenants')).tenants;
 const matches=tenants.filter(t=>t.external_key===company.external_key);assert.ok(matches.length<=1,'Ambiguous demo tenant');
 const tenant=matches[0]??(await direct(8080,'POST','/tenants',company,201)).tenant;
 if(state.tenant)assert.equal(state.tenant,tenant.id,'Demo state refers to a different tenant');state.tenant=tenant.id;save();
 if(tenant.status==='pending')await direct(8080,'POST',`/tenants/${tenant.id}/provision`,{});
 const existing=(await direct(8080,'GET',`/tenants/${state.tenant}/tables`)).tables??[];
 for(const model of models){
  state.tablesCreated??={};
  const prior=existing.find(t=>t.name===model.name);
  const table=state.tablesCreated[model.name]&&prior?prior:(await direct(8080,'POST',`/tenants/${state.tenant}/tables`,{name:model.name,alias:model.alias,description:model.description,semantic_type:'entity'},201)).table;
  state.tables[model.name]=table.id;state.tablesCreated[model.name]=true;save();
  const fields=(await direct(8080,'GET',`/tables/${table.id}/fields`)).fields??[];
  for(const [name,data_type]of model.fields){const old=fields.find(f=>f.name===name);if(old){assert.equal(old.data_type,data_type,`Existing ${model.name}.${name} has another type`);continue;}await direct(8080,'POST',`/tables/${table.id}/fields`,{name,data_type,nullable:false},201);}
  const caption=model.name==='customers'?'full_name':model.name==='wallets'?'wallet_number':'description';
  if(!state.completed&&table.caption_field!==caption)await direct(8080,'PATCH',`/tables/${table.id}`,{caption_field:caption});
  console.log('Prepared model: '+model.alias);
 }
 const published=await until(()=>direct(8080,'GET',`/tenants/${state.tenant}/data-model`),r=>models.every(m=>m.fields.every(([f])=>r.data_model.tables[m.name]?.fields[f])),'model publication');
 const links=(await direct(8080,'GET',`/tenants/${state.tenant}/links`)).links??[];
 for(const [name,parent,child,childField]of [['customer','customers','wallets','customer_id'],['transaction_customer','customers','transactions','customer_id'],['wallet','wallets','transactions','wallet_id']]){
  if(links.some(l=>l.name===name))continue;
  await direct(8080,'POST',`/tenants/${state.tenant}/links`,{name,parent_table_id:state.tables[parent],parent_field_id:published.data_model.tables[parent].fields.object_id.id,child_table_id:state.tables[child],child_field_id:published.data_model.tables[child].fields[childField].id},201);
 }
}

async function login(){
 const manifest={version:1,tenant:{id:state.tenant},users:[{username,name:'NovaPay Demo Administrator',first_name:'NovaPay',last_name:'Demo',email:'novapay.demo@example.test',roles:['tenant_admin','platform_admin','case_manager_admin','case_investigator']}]};
 const manifestFile=resolve(privateDir,'users.json');writeFileSync(manifestFile,JSON.stringify(manifest,null,2)+'\n');
 const provision=spawnSync(process.execPath,[resolve(root,'backend/auth-service/scripts/setup-mysql-users.mjs'),'--file',manifestFile,'--data-model-url',origin+'/services/data-model','--create-missing','--apply'],{cwd:root,encoding:'utf8',windowsHide:true,timeout:300000,maxBuffer:2<<20});
 assert.equal(provision.status,0,'Dedicated demo user provisioning failed: '+provision.stderr);console.log('Dedicated demo account provisioned.');
 const credentialFile=resolve(privateDir,'login.json');
 let credentials=existsSync(credentialFile)?JSON.parse(readFileSync(credentialFile,'utf8')):null;
 if(!credentials){const receipts=resolve(root,'.local/mysql-dev/user-bootstrap');const candidates=readdirSync(receipts).filter(f=>f.endsWith('.credentials.json')).map(f=>JSON.parse(readFileSync(resolve(receipts,f),'utf8'))).filter(v=>v.username===username&&v.status==='created');assert.equal(candidates.length,1,'Use the existing demo credentials; passwords are never reset by this script');credentials={username,password:candidates[0].temporary_password,temporary:true};writeFileSync(credentialFile,JSON.stringify(credentials,null,2)+'\n',{mode:0o600});}
 const require=createRequire(resolve(root,'frontend/package.json'));const {chromium}=require('@playwright/test');browser=await chromium.launch({headless:true});context=await browser.newContext();const page=await context.newPage();page.setDefaultTimeout(30000);
 await page.goto(origin+'/login');await page.getByRole('button',{name:'Continue to sign in'}).click();await page.locator('#username').fill(username);await page.locator('#password').fill(credentials.password);await page.locator('#kc-login').click();
 if(credentials.temporary){await page.locator('#password-new').waitFor();const next=credentials.next_password??randomBytes(24).toString('base64url');credentials.next_password=next;writeFileSync(credentialFile,JSON.stringify(credentials,null,2)+'\n',{mode:0o600});await page.locator('#password-new').fill(next);await page.locator('#password-confirm').fill(next);await page.locator('button[type="submit"], input[type="submit"]').click();await page.waitForURL(origin+'/my-account',{timeout:60000});credentials={username,password:next,temporary:false};writeFileSync(credentialFile,JSON.stringify(credentials,null,2)+'\n',{mode:0o600});}
 else await page.waitForURL(origin+'/my-account',{timeout:60000});
 const tables=await api('data-model','GET',`/tenants/${state.tenant}/tables`);assert.equal(tables.tables.length,models.length,'Demo account is not viewing its dedicated tenant');
 return page;
}

async function ingest(table,record){
 const key=operation('ingest:'+table+':'+record.object_id);
 for(let attempt=0;attempt<30;attempt++){
  const response=await fetch(`${origin}/services/ingestion/v1/tenants/${state.tenant}/ingest/${table}`,{method:'POST',headers:{'Content-Type':'application/json','Idempotency-Key':key},body:JSON.stringify(record),signal:AbortSignal.timeout(60000)});
  if([200,201].includes(response.status))return;
  const error=await response.json();if([409,503].includes(response.status)&&attempt<29){await delay(1000);continue;}throw Error(`Ingestion ${table}/${record.object_id}: HTTP ${response.status}: ${JSON.stringify(error).slice(0,300)}`);
 }
}
async function seedRecords(){
 if(state.recordsSeeded)return;
 for(const [suffix,name,age]of [['001','Ama Demo',180],['NEW','Kojo Demo',2],['HOT','Akua Demo',180]]){
  await ingest('customers',{object_id:'NP-CUSTOMER-'+suffix,full_name:name,email:`${suffix.toLowerCase()}@novapay.example.test`,country:'GH',kyc_status:'verified',risk_band:suffix==='HOT'?'elevated':'standard',onboarded_at:new Date(Date.parse(state.anchor)-age*86400000).toISOString()});
  await ingest('wallets',{object_id:'NP-WALLET-'+suffix,customer_id:'NP-CUSTOMER-'+suffix,wallet_number:'NP-'+suffix,currency:'GHS',balance:50000,status:'active',opened_at:new Date(Date.parse(state.anchor)-age*86400000).toISOString()});
 }
 for(let n=0;n<6;n++)await ingest('transactions',{object_id:`NP-HISTORY-${n+1}`,...base,customer_id:'NP-CUSTOMER-HOT',wallet_id:'NP-WALLET-HOT',amount:75+n*10,occurred_at:at(-9+n),description:'Synthetic earlier transfer used by the ten-minute velocity rule'});
 for(const sample of examples)await ingest('transactions',{object_id:sample.id,...base,...sample.patch,description:sample.label});
 state.recordsSeeded=true;save();
 console.log('Ingested 3 customers, 3 wallets and 18 transactions, including 6 velocity-history rows.');
}

async function publishScenarios(){
 const own='/tenants/'+state.tenant;const current=(await api('decisions','GET',own+'/scenarios')).scenarios??[];
 for(const definition of definitions){
  let saved=state.scenarios[definition.key];
  if(saved?.published)continue;
  const scenario=current.find(s=>s.name===definition.name)??(await api('decisions','POST',own+'/scenarios',{name:definition.name,description:definition.description,trigger_object_type:'transactions'},201)).scenario;
  saved??={id:scenario.id};state.scenarios[definition.key]=saved;save();
  const path=own+'/scenarios/'+scenario.id;
  if(!saved.iteration){saved.iteration=(await api('decisions','POST',path+'/iterations',{},201)).iteration.id;save();}
  const trigger=(await api('decisions','POST',path+'/generate-ast',{expression:definition.trigger})).formula;
  await api('decisions','PUT',path+'/iterations/'+saved.iteration,{trigger_formula:trigger,score_review_threshold:definition.thresholds[0],score_block_and_review_threshold:definition.thresholds[1],score_decline_threshold:definition.thresholds[2]});
  const existing=(await api('decisions','GET',path+'/iterations/'+saved.iteration+'/rules')).rules??[];
  for(const [index,[name,expression,score_modifier]]of definition.rules.entries()){
   const formula=typeof expression==='string'?(await api('decisions','POST',path+'/generate-ast',{expression})).formula:expression;
   const prior=existing.find(r=>r.name===name);
   const payload={name,description:typeof expression==='string'?expression:'Counts earlier persisted transactions for this wallet within ten minutes; excludes the current timestamp.',display_order:index+1,formula,score_modifier};
   if(prior)await api('decisions','PUT',path+'/iterations/'+saved.iteration+'/rules/'+prior.id,payload);
   else await api('decisions','POST',path+'/iterations/'+saved.iteration+'/rules',payload,201);
  }
  const validation=await api('decisions','POST',path+'/iterations/'+saved.iteration+'/validate',{},[200,422]);assert.equal(validation.validation.valid,true,JSON.stringify(validation.validation));
  await api('decisions','POST',path+'/iterations/'+saved.iteration+'/commit',{});
  await api('decisions','POST',path+'/publications',{action:'publish',iteration_id:saved.iteration});saved.published=true;save();console.log('Published scenario: '+definition.name);
 }
}
async function screenConfigs(){
 const own='/tenants/'+state.tenant;
 const inboxes=(await api('cases','GET',own+'/inboxes')).inboxes??[];
 for(const [key,name]of [['fraud','NovaPay Fraud Operations'],['screening','NovaPay Screening Review']]){state[key+'Inbox']=inboxes.find(i=>i.name===name)?.id??(await api('cases','POST',own+'/inboxes',{name},[200,201])).inbox.id;save();}
 const path='/scenarios/'+state.scenarios.screening.id;
 const configuration=await api('scenario-screening','GET',path);
 for(const [key,name,entity_type,name_field,threshold,forced_outcome]of [['person','Beneficiary person screening — synthetic catalog','Person','beneficiary_name',85,'block_and_review'],['company','Beneficiary company screening — synthetic catalog','Organization','beneficiary_company',90,'review']]){
  const existing=configuration.rules.find(r=>r.name===name);
  if(existing){assert.equal(existing.enabled,true,'Existing demo screening rule was disabled; refusing to overwrite an operator change');if(state.completed)continue;assert.equal(existing.id,state.screeningRuleIDs?.[key],'Unexpected existing screening configuration');}
  state.screeningRuleIDs??={};state.screeningRuleIDs[key]??=randomUUID();save();
  await api('scenario-screening','PUT',path+'/configs/'+state.screeningRuleIDs[key],{id:state.screeningRuleIDs[key],name,enabled:true,entity_type,name_field,counterparty_id_field:'beneficiary_id',threshold,result_limit:10,forced_outcome,outcomes:[],condition:{kind:'payload',field:name_field,operator:'neq',value:''},inbox_id:state.screeningInbox,updated_at:existing?.updated_at??null});
 }
 console.log('Enabled person and organization screening with mapped counterparty IDs.');
}
async function evaluate(){
 const own='/tenants/'+state.tenant;
 for(const sample of examples){for(const [key,[score,outcome]]of Object.entries(sample.expected)){
  const token=sample.id+':'+key;
  if(!state.evaluations[token]){const result=(await api('decisions','POST',own+'/scenarios/'+state.scenarios[key].id+'/evaluate',{object_id:sample.id,object_type:'transactions',fields:{...base,...sample.patch,description:sample.label}})).result;state.evaluations[token]={id:result.decision.id,score:result.decision.score,outcome:result.decision.outcome,label:sample.label,scenario:key};save();}
  else {const saved=state.evaluations[token];const actual=(await api('decisions','GET',own+'/decisions/'+saved.id)).decision;assert.equal(actual.object_id,sample.id);assert.equal(actual.scenario_id,state.scenarios[key].id);assert.equal(actual.score,score);assert.equal(actual.outcome,outcome);}
  const actual=state.evaluations[token];assert.equal(actual.score,score,token+' score');assert.equal(actual.outcome,outcome,token+' outcome');
  if(outcome!=='approve'&&key!=='screening'&&!state.cases[actual.id]){const created=await api('cases','POST',own+'/cases',{operation_id:operation('case:'+actual.id),name:`${sample.id} — ${sample.label}`,type:'decision',inbox_id:state.fraudInbox,decision_ids:[actual.id]},[200,201]);state.cases[actual.id]=created.case.id;save();}
 }}
 for(const sampleID of ['NP-TX-007','NP-TX-008','NP-TX-009']){
  const decision=state.evaluations[sampleID+':screening'];
  const result=await until(()=>api('decision-screening','GET','/'+decision.id),r=>r.screening_executions.some(e=>e.status==='completed'&&e.review_case_id),'screening evidence and case delivery');
  const matched=result.screening_executions.find(e=>e.review_case_id);state.cases[decision.id]=matched.review_case_id;save();
 }
 console.log(`Verified ${Object.keys(state.evaluations).length} decisions against expected scores and outcomes.`);
}
async function preview(page){
 const output=resolve(root,'frontend/evidence/novapay-demo');mkdirSync(output,{recursive:true});
 const pages=[['data-model','/your-data'],['scenarios','/detection'],['transfer-rules','/detection/'+state.scenarios.transfers.id+'/edit?tab=Rules'],['decisions','/detection?tab=Decisions'],['screening-decision','/detection/decisions/'+state.evaluations['NP-TX-007:screening'].id],['screening-configuration','/detection/'+state.scenarios.screening.id+'/screening'],['cases','/cases']];
 for(const [name,path]of pages){await page.goto(origin+path);await page.waitForLoadState('networkidle');if(name==='screening-decision'){const panel=page.getByRole('region',{name:'Decision screening'});await panel.getByRole('link',{name:'Start reviewing'}).waitFor();await panel.screenshot({path:resolve(output,'screening-results-card.png')});}await page.screenshot({path:resolve(output,name+'.png'),fullPage:true});}
 state.completed=true;state.completedAt=new Date().toISOString();save();
 const report={at:state.completedAt,company:company.name,synthetic:true,tenant:state.tenant,username,tableCount:models.length,relationshipCount:3,customerCount:3,walletCount:3,transactionCount:18,scenarioCount:definitions.length,ruleCount:definitions.reduce((n,s)=>n+s.rules.length,0),screeningRuleCount:2,decisionCount:Object.keys(state.evaluations).length,caseCount:Object.keys(state.cases).length,verified:true,links:Object.fromEntries(pages.map(([name,path])=>[name,origin+path])),decisions:Object.values(state.evaluations),limitations:['Synthetic development screening catalog only; no live sanctions coverage.','Illustrative fraud thresholds; supplied source risk is a demo feature, not a trained model.']};
 writeFileSync(resolve(output,'report.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify({...report,decisions:undefined},null,2));console.log('Demo credentials saved privately at '+resolve(privateDir,'login.json'));
}
try{await prepareModel();const page=await login();await seedRecords();await publishScenarios();await screenConfigs();await evaluate();await preview(page);}
catch(error){console.error('Fintech demo setup stopped: '+error.message);process.exitCode=1;}
finally{if(context)await context.close();if(browser)await browser.close();}
