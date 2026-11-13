// Real local Keycloak + authenticated Next routes + retained synthetic MySQL
// fixtures. Reports contain status metadata only, never credentials or bodies.
import assert from 'node:assert/strict';
import {randomBytes,randomUUID} from 'node:crypto';
import {readFileSync,writeFileSync,mkdirSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';
const root=fileURLToPath(new URL('../../',import.meta.url)), local=resolve(root,'.local/mysql-dev');
const parse=path=>Object.fromEntries(readFileSync(path,'utf8').split(/\r?\n/).filter(line=>line&&!line.startsWith('#')).map(line=>{const at=line.indexOf('=');return [line.slice(0,at),line.slice(at+1)];}));
const provider=parse(resolve(root,'.local/auth-provider/auth.env')), admin=parse(resolve(root,'.local/auth-provider/keycloak.env'));
const issuer=new URL(provider.OIDC_ISSUER_URL), origin='http://localhost:3002';
assert.ok(['localhost','127.0.0.1'].includes(issuer.hostname));
const output=resolve(root,'frontend/evidence/mysql-integration',new Date().toISOString().replaceAll(':','-'));mkdirSync(output,{recursive:true});
const trace=[],suites=[];let user,browser,context,adminHeaders,tenant,table,scenario,decision,inbox,inlineRule,inlineDecision,current='setup',failure,removedUser=false;
const request=async(url,options={})=>{try{return await fetch(url,{...options,redirect:'manual',signal:AbortSignal.timeout(30000)});}catch{throw Error('Request unavailable at '+new URL(url).pathname);}};
const json=async(url,method,body,want=200,headers={})=>{const r=await request(url,{method,headers:{'Content-Type':'application/json',...headers},body:body===undefined?undefined:JSON.stringify(body)});assert.ok([want].flat().includes(r.status),method+' '+new URL(url).pathname+' status '+r.status);return r.status===204?undefined:r.json();};
async function suite(name,run){current=name;await run();suites.push({name,passed:true});console.log('PASS '+name);}
async function api(service,method,path,body,want=200){const r=await context.request.fetch(origin+'/api/'+service+path,{method,headers:{Origin:origin},data:body,timeout:35000});trace.push({suite:current,service,method,path,status:r.status(),expected:want});assert.ok([want].flat().includes(r.status()),method+' '+service+path+' status '+r.status());return r.status()===204?undefined:r.json();}
async function until(run,check,label){for(let i=0;i<60;i++){const value=await run();if(check(value))return value;await delay(500);}throw Error('Timed out: '+label);}
async function adminLogin(){const response=await request(issuer.origin+'/realms/master/protocol/openid-connect/token',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:new URLSearchParams({grant_type:'password',client_id:'admin-cli',username:admin.KC_BOOTSTRAP_ADMIN_USERNAME,password:admin.KC_BOOTSTRAP_ADMIN_PASSWORD})});assert.equal(response.status,200);adminHeaders={Authorization:'Bearer '+(await response.json()).access_token,'Content-Type':'application/json'};}
try {
 await until(()=>request(origin+'/login').catch(()=>({ok:false})),r=>r.ok,'frontend readiness');
 await suite('isolated-synthetic-tenant-and-real-provider-account',async()=>{
  tenant=(await json('http://localhost:8080/v1/tenants','POST',{name:'Frontend MySQL acceptance '+randomUUID(),external_key:'ui-'+randomUUID()},201)).tenant.id;
  await json('http://localhost:8080/v1/tenants/'+tenant+'/provision','POST',{});
  await adminLogin();
  user={username:'frontend-mysql-'+randomBytes(6).toString('hex'),password:randomBytes(32).toString('base64url')};
  const created=await request(issuer.origin+'/admin/realms/fraud/users',{method:'POST',headers:adminHeaders,body:JSON.stringify({username:user.username,enabled:true,firstName:'Frontend',lastName:'Acceptance',email:user.username+'@example.test',emailVerified:true,credentials:[{type:'password',value:user.password,temporary:false}]})});assert.equal(created.status,201);user.id=created.headers.get('location')?.split('/').pop();assert.ok(user.id);
  writeFileSync(resolve(local,'auth-runtime/provision-frontend.json'),JSON.stringify({TenantID:tenant,Issuer:provider.OIDC_ISSUER_URL,Subject:user.id,Name:'Frontend MySQL acceptance',Roles:['tenant_admin','platform_admin','case_manager_admin','case_investigator']})+'\n',{mode:0o600});
  const p=spawnSync('docker',['compose','--profile','install','--env-file',resolve(local,'compose.env'),'-f','docker-compose-mysql.yml','run','--rm','--no-deps','migrate-auth','/app/install-auth','--provision','/runtime/provision-frontend.json'],{cwd:resolve(root,'backend/auth-service'),encoding:'utf8',windowsHide:true,timeout:180000,maxBuffer:4<<20});assert.equal(p.status,0,'Synthetic identity provisioning failed; private output omitted');
 });
 const require=createRequire(resolve(root,'frontend/package.json'));const {chromium}=require('@playwright/test');browser=await chromium.launch({headless:true});context=await browser.newContext();const page=await context.newPage();page.setDefaultTimeout(20000);
 page.on('response',response=>{const url=new URL(response.url());if(url.origin===origin&&url.pathname.startsWith('/api/case-refinement/'))trace.push({suite:current,service:'case-refinement',method:response.request().method(),path:url.pathname.slice('/api/case-refinement'.length),status:response.status(),expected:200});});
 await suite('real-browser-login-and-secure-cookie',async()=>{
  await page.goto(origin+'/login');await page.getByRole('button',{name:'Continue to sign in'}).click();
  await page.locator('#username').fill(user.username);await page.locator('#password').fill(user.password);await page.locator('#kc-login').click();await page.waitForURL(origin+'/my-account',{timeout:40000});
  const cookie=(await context.cookies()).find(c=>c.name==='__Host-fraud_session');assert.ok(cookie?.httpOnly&&cookie.secure&&cookie.sameSite==='Lax');
  await api('data-model','GET','/tenants/'+tenant+'/tables');
 });
 await suite('custom-list-overview-and-indexed-counts',async()=>{
  const path='/tenants/'+tenant+'/platform/custom-lists';
  const list=(await api('decisions','POST',path,{name:'Frontend risk values',description:'Synthetic list integration',kind:'generic_text'},201)).custom_list;
  const empty=(await api('decisions','POST',path,{name:'Frontend empty values',description:'Synthetic empty list',kind:'generic_text'},201)).custom_list;
  const counts=async(want)=>{
   const values=(await api('decisions','GET',path)).custom_lists;
   assert.equal(values.find(v=>v.id===list.id)?.entry_count,want);
   assert.equal(values.find(v=>v.id===empty.id)?.entry_count,0);
  };
  await counts(0);
  const entry=(await api('decisions','POST',path+'/'+list.id+'/entries',{value:'synthetic-risk'},201)).custom_list_entry;
  await counts(1);
  const requests=[];
  const watch=request=>{if(request.url().includes('/platform/custom-list-entries'))requests.push(request.url());};
  page.on('request',watch);
  try{
   await page.goto(origin+'/detection?tab=Lists');
   await page.getByText('Frontend risk values',{exact:true}).waitFor();
   await page.getByText('1 value',{exact:true}).waitFor();
   await page.getByText('0 values',{exact:true}).waitFor();
   assert.equal(requests.length,0,'List overview downloaded entry payloads');
  }finally{page.off('request',watch);}
  await api('decisions','DELETE',path+'/'+list.id+'/entries/'+entry.id,undefined,204);
  await counts(0);
 });
 await suite('data-model-authoring-and-resource-ownership',async()=>{
  const own='/tenants/'+tenant;
  table=(await api('data-model','POST',own+'/tables',{name:'ui_payments'},201)).table.id;
  for(const [name,data_type]of [['amount','float'],['customer_name','string']])await api('data-model','POST','/tables/'+table+'/fields',{name,data_type,nullable:false},201);
  await api('data-model','POST','/tables/'+table+'/fields',{name:'last_name',data_type:'string',nullable:true},201);
  await api('data-model','POST','/tables/'+table+'/fields',{name:'account_id',data_type:'string',nullable:true},201);
  await until(()=>api('data-model','GET',own+'/data-model'),r=>r.data_model.tables.ui_payments?.fields.customer_name,'published payment fields');
  const settings=await api('data-model','GET','/tables/'+table+'/display-settings');assert.ok(settings);
  await page.goto(origin+'/your-data');await page.getByRole('button',{name:'Display settings for ui_payments'}).waitFor();
  await api('data-model','GET','/tenants/'+randomUUID()+'/tables',undefined,403);
  await api('data-model','GET','/tables/'+randomUUID()+'/display-settings',undefined,403);
 });
 await suite('csv-ingestion-and-record-browsing',async()=>{
  const response=await context.request.post(origin+'/api/ingestion/tenants/'+tenant+'/ingest/ui_payments/csv',{headers:{Origin:origin},multipart:{file:{name:'synthetic-payments.csv',mimeType:'text/csv',buffer:Buffer.from('object_id,amount,customer_name\nui-low,25,Synthetic Customer\nui-high,15000,Synthetic Customer\n')}}});
  assert.ok([200,201,202].includes(response.status()),'CSV upload status '+response.status());const upload=(await response.json()).upload_log;assert.ok(upload?.id);
  await until(()=>api('ingestion','GET','/upload-logs/'+upload.id),r=>r.upload_log.status==='completed','CSV completion');
  const records=await api('ingestion','GET','/tenants/'+tenant+'/records/ui_payments');assert.equal(records.records.length,2);
  await page.goto(origin+'/your-data/upload/ui_payments');
 });
 await suite('scenario-rule-publication-and-real-evaluation',async()=>{
  const own='/tenants/'+tenant;
  scenario=(await api('decisions','POST',own+'/scenarios',{name:'Frontend high amount rule',trigger_object_type:'ui_payments'},201)).scenario.id;
  const p=own+'/scenarios/'+scenario, iteration=(await api('decisions','POST',p+'/iterations',{},201)).iteration;
  const trigger=(await api('decisions','POST',p+'/generate-ast',{expression:'amount >= 0'})).formula;
  await api('decisions','PUT',p+'/iterations/'+iteration.id,{trigger_formula:trigger,score_review_threshold:40,score_block_and_review_threshold:80,score_decline_threshold:90});
  const formula=(await api('decisions','POST',p+'/generate-ast',{expression:'amount >= 10000'})).formula;
  await api('decisions','POST',p+'/iterations/'+iteration.id+'/rules',{name:'High amount',display_order:1,formula,score_modifier:50},201);
  await api('decisions','POST',p+'/iterations/'+iteration.id+'/commit',{});await api('decisions','POST',p+'/publications',{action:'publish',iteration_id:iteration.id});
  for(const [object_id,amount,score,outcome]of [['ui-low',25,0,'approve'],['ui-high',15000,50,'review']]){
   const result=(await api('decisions','POST',p+'/evaluate',{object_id,object_type:'ui_payments',fields:{amount,customer_name:'Synthetic Customer'}})).result;
   assert.equal(result.decision.score,score);assert.equal(result.decision.outcome,outcome);decision=result.decision.id;
  }
  await api('decisions','GET',own+'/decisions/'+decision);await api('decisions','GET','/tenants/'+randomUUID()+'/scenarios',undefined,403);
 });
 await suite('case-workspace-and-screening-authoring',async()=>{
  const own='/tenants/'+tenant;
  inbox=(await api('cases','POST',own+'/inboxes',{name:'Frontend synthetic reviews'},[200,201])).inbox.id;
  const investigation=await api('cases','POST',own+'/cases',{operation_id:randomUUID(),name:'Frontend synthetic investigation',type:'decision',inbox_id:inbox,decision_ids:[decision]},[200,201]);
  assert.ok(investigation.case?.id);
  const evidence=await api('case-evidence','GET','/'+tenant+'/'+investigation.case.id+'/decisions/'+decision+'?rules=1');
  assert.equal(evidence.decision.id,decision);assert.equal(evidence.rules.length,1);
  await api('workflows','GET','/scenarios/'+scenario);
  const config=await api('scenario-screening','GET','/scenarios/'+scenario);assert.ok(config.fields.includes('customer_name'));
  const rule={id:randomUUID(),name:'Synthetic name screening',enabled:false,entity_type:'Person',name_field:'customer_name',outcomes:['review'],inbox_id:inbox,updated_at:null};
  const saved=await api('scenario-screening','PUT','/scenarios/'+scenario+'/configs/'+rule.id,rule);
  await api('scenario-screening','PUT','/scenarios/'+scenario+'/configs/'+rule.id,{...rule,name:'Stale overwrite'},409);
  await api('scenario-screening','PUT','/scenarios/'+scenario+'/configs/'+rule.id,{...saved,name:'Updated synthetic screening'});
  const summary=await api('decision-screening','GET','/'+decision);assert.deepEqual(summary.screening_executions,[]);
  await page.goto(origin+'/cases');await page.getByRole('link',{name:'Frontend synthetic investigation',exact:true}).waitFor();
 });
 await suite('scenario-add-menu-opens-screening-without-creating',async()=>{
  await page.goto(origin+'/detection/'+scenario+'/edit?tab=Rules');
  await page.getByRole('button',{name:'Add',exact:true}).click();
  const menu=page.getByRole('menu',{name:'Add to scenario'});
  await menu.waitFor();
  assert.equal(await menu.getByRole('menuitem').count(),2);
  assert.equal(await menu.getByRole('menuitem',{name:'Add a rule',exact:true}).isDisabled(),true);
  await menu.getByRole('menuitem',{name:'Add a screening',exact:true}).click();
  await page.waitForURL(origin+'/detection/'+scenario+'/screening?create=1');
  const form=page.getByRole('form',{name:'Screening configuration'});
  await form.waitFor();
  assert.equal(await form.getByLabel('Name',{exact:true}).inputValue(),'');
  await form.getByRole('button',{name:'Cancel',exact:true}).click();
  const configs=await api('scenario-screening','GET','/scenarios/'+scenario);
  assert.equal(configs.rules.length,1);
  assert.equal(configs.rules[0].name,'Updated synthetic screening');
 });
 await suite('inline-screening-authoring-evaluation-and-durable-evidence',async()=>{
  const own='/tenants/'+tenant, path='/scenarios/'+scenario;
  const capabilities=await api('scenario-screening','GET',path);
  assert.equal(capabilities.inline_supported,true);assert.equal(capabilities.default_threshold,80);assert.equal(capabilities.default_result_limit,50);
  await page.goto(origin+'/detection/'+scenario+'/screening?create=1');
  const form=page.getByRole('form',{name:'Screening configuration'});
  await form.getByLabel('Name',{exact:true}).fill('Inline synthetic beneficiary match');
  await form.getByLabel('Screening timing').selectOption('during');
  await form.getByLabel('Outcome when a possible match is found').selectOption('block_and_review');
  await form.getByLabel('Name field',{exact:true}).selectOption('customer_name');
  await form.getByRole('button',{name:'Add name field',exact:true}).click();
  await form.getByLabel('Additional name field 1').selectOption('last_name');
  await form.getByLabel('Counterparty identifier field (optional)').selectOption('account_id');
  await form.getByLabel('Match threshold (0-100)').fill('100');
  await form.getByLabel('Result limit (1-50)').fill('1');
  await form.getByLabel('Review inbox').selectOption(inbox);
  await form.getByLabel('Enable for future decisions').check();
  await page.screenshot({path:resolve(output,'screening-editor-desktop.png'),fullPage:true});
  await form.getByRole('button',{name:'Save configuration'}).click();
  await page.getByRole('status').filter({hasText:'Screening configuration saved.'}).waitFor();
  const saved=(await api('scenario-screening','GET',path)).rules.find(r=>r.name==='Inline synthetic beneficiary match');
  assert.ok(saved);assert.deepEqual(saved.additional_name_fields,['last_name']);assert.equal(saved.forced_outcome,'block_and_review');assert.equal(saved.counterparty_id_field,'account_id');inlineRule=saved;
  const badID=randomUUID();await api('scenario-screening','PUT',path+'/configs/'+badID,{...saved,id:badID,additional_name_fields:['unknown_field'],updated_at:null},422);
  for(const [first,last,want]of [['Alex','Example','block_and_review'],['Unrelated','Synthetic','approve']]){
   const evaluated=(await api('decisions','POST',own+'/scenarios/'+scenario+'/evaluate',{object_id:'ui-inline-'+randomUUID(),object_type:'ui_payments',fields:{amount:25,customer_name:first,last_name:last,account_id:'synthetic-account-001'}})).result.decision;
   if(want==='block_and_review')inlineDecision=evaluated;
   assert.equal(evaluated.outcome,want);assert.equal(evaluated.score,0);
   // Raw execution evidence is intentionally unavailable through the browser
   // proxy. Inspect it through the loopback-only development owner API.
   const effects=(await json('http://localhost:8082/v1'+own+'/decisions/'+evaluated.id+'/screening-executions','GET')).screening_executions;
   assert.equal(effects.length,1);assert.equal(effects[0].request_json.queries[0].name,first+' '+last);assert.equal(effects[0].request_json.inline_result.result.matches.length,want==='approve'?0:1);
   await until(()=>api('decision-screening','GET','/'+evaluated.id),r=>r.screening_executions.some(e=>e.status==='completed'),'captured inline evidence projection');
  }
 });
 await suite('screening-review-barrier-and-audited-refinement',async()=>{
  const target=(await until(()=>api('decision-screening','GET','/'+inlineDecision.id),r=>r.screening_executions.some(e=>e.review_case_id&&e.source_id),'authorized screening case link')).screening_executions[0];
  const reviewPath='/'+tenant+'/'+target.review_case_id+'/decisions';
  const state=await api('case-review','GET',reviewPath+'?resource_id='+inlineDecision.id);
  assert.ok(state.disabled_actions.approve);assert.deepEqual(state.actions,['decline']);
  await api('case-review','POST',reviewPath,{operation_id:randomUUID(),resource_id:inlineDecision.id,expected_version:state.version,action:'approve',findings:'Must be rejected while matches remain'},400);
  await page.goto(origin+'/detection/decisions/'+inlineDecision.id);
  const panel=page.getByRole('region',{name:'Decision screening'});
  await panel.getByRole('heading',{name:'Inline synthetic beneficiary match'}).waitFor();
  await panel.getByRole('link',{name:'Start reviewing'}).click();
  await page.getByRole('button',{name:'Inspect evidence',exact:true}).click();
  await page.getByRole('button',{name:'Refine search',exact:true}).click();
  await page.getByLabel('Refined name',{exact:true}).fill('Unrelated complete beneficiary');
  await page.getByLabel('Refined similarity threshold').fill('100');
  await page.getByRole('button',{name:'Preview results',exact:true}).click();
  await page.getByRole('heading',{name:'No possible matches found'}).waitFor();
  await page.getByLabel('Refinement findings').fill('Validated complete identity against synthetic customer record');
  await page.screenshot({path:resolve(output,'screening-refinement-preview.png'),fullPage:true});
  await page.getByRole('button',{name:'Apply changes',exact:true}).click();
  await page.getByText('Refinement applied. Original evidence retained; review the replacement results in this case.').waitFor();
  const cleared=await api('case-review','GET',reviewPath+'?resource_id='+inlineDecision.id);
  assert.ok(cleared.actions.includes('approve'));assert.equal(cleared.disabled_actions?.approve,undefined);
  const original=await api('case-evidence','GET','/'+tenant+'/'+target.review_case_id+'/screenings/'+target.source_id);assert.equal(original.matches.length,1);
  const summary=(await api('decision-screening','GET','/'+inlineDecision.id)).screening_executions[0];assert.equal(summary.response_json.refined,true);assert.equal(summary.response_json.status,'no_hit');
  const command={operation_id:randomUUID(),resource_id:inlineDecision.id,expected_version:cleared.version,action:'approve',findings:'Complete refined evidence reviewed'};
  await api('case-review','POST',reviewPath,command);await api('case-review','POST',reviewPath,command);
  const immutable=await api('decisions','GET','/tenants/'+tenant+'/decisions/'+inlineDecision.id);assert.equal(immutable.decision.outcome,'block_and_review');
 });
 await suite('counterparty-whitelist-and-future-evaluation-isolation',async()=>{
  const evaluate=async account=>(await api('decisions','POST','/tenants/'+tenant+'/scenarios/'+scenario+'/evaluate',{object_id:'ui-whitelist-'+randomUUID(),object_type:'ui_payments',fields:{amount:25,customer_name:'Alex',last_name:'Example',account_id:account}})).result.decision;
  const candidate=await evaluate('synthetic-account-002');assert.equal(candidate.outcome,'block_and_review');
  const target=(await until(()=>api('decision-screening','GET','/'+candidate.id),r=>r.screening_executions.some(e=>e.review_case_id&&e.source_id),'whitelist screening case')).screening_executions[0];
  const evidence=await api('case-evidence','GET','/'+tenant+'/'+target.review_case_id+'/screenings/'+target.source_id);const match=evidence.matches[0].id;
  const path='/'+tenant+'/'+target.review_case_id+'/matches',state=await api('case-review','GET',path+'?resource_id='+match);
  assert.ok(state.actions.includes('dismiss_and_whitelist'));
  const command={operation_id:randomUUID(),resource_id:match,expected_version:state.version,action:'dismiss_and_whitelist',findings:'Confirmed different synthetic person for this account'};
  await api('case-review','POST',path,command);await api('case-review','POST',path,command);
  const suppressed=await evaluate('synthetic-account-002');assert.equal(suppressed.outcome,'approve');
  await until(()=>api('decision-screening','GET','/'+suppressed.id),r=>r.screening_executions.some(e=>e.status==='completed'&&e.response_json.status==='no_hit'),'captured whitelist suppression in worker');
  assert.equal((await evaluate('synthetic-account-003')).outcome,'block_and_review');
  const active=await api('case-review','GET',path+'?resource_id='+match);assert.ok(active.actions.includes('remove_counterparty_exclusion'));
  await api('case-review','POST',path,{operation_id:randomUUID(),resource_id:match,expected_version:active.version,action:'remove_counterparty_exclusion',findings:'Reassess subsequent synthetic evaluations'});
  assert.equal((await evaluate('synthetic-account-002')).outcome,'block_and_review');
  const currentRule=(await api('scenario-screening','GET','/scenarios/'+scenario)).rules.find(r=>r.id===inlineRule.id);
  await api('scenario-screening','PUT','/scenarios/'+scenario+'/configs/'+inlineRule.id,{...currentRule,enabled:false});
 });
 await suite('manual-screening-through-current-user-identity',async()=>{
  const capabilities=await api('screening-search','GET','/capabilities');assert.equal(capabilities.configured,true);
  const search=await api('screening-search','POST','',{operation_id:randomUUID(),name:'Synthetic Example',entity_type:'Person',threshold:70,limit:10},[200,201,202]);assert.ok(search.id);
  await until(()=>api('screening-search','GET','/'+search.id),r=>!['pending','queued','processing'].includes(r.status),'manual search completion');
  await page.goto(origin+'/screening-search');await page.getByRole('heading',{name:/screen/i}).first().waitFor();
 });
 await suite('browser-decision-list-renders-published-scenario',async()=>{
  await page.goto(origin+'/detection?tab=Decisions');await page.getByText('Frontend high amount rule').first().waitFor();
 });
 await suite('csrf-and-logout-reject-replay',async()=>{
  const forged=await context.request.post(origin+'/api/decisions/tenants/'+tenant+'/scenarios',{headers:{Origin:'https://foreign.example'},data:{}});assert.equal(forged.status(),403);
  await context.request.post(origin+'/api/auth/logout',{headers:{Origin:origin}});
  for(const service of ['data-model','ingestion','decisions'])await api(service,'GET','/tenants/'+tenant+(service==='decisions'?'/scenarios':service==='data-model'?'/tables':'/records/ui_payments'),undefined,401);
 });
} catch(error) {failure=current+': '+String(error.message).replace(/https?:\/\/\S+/g,'[URL omitted]').replace(/[A-Za-z0-9_-]{40,}/g,'[opaque value omitted]');console.error(failure);
 if(context){const pages=context.pages(),last=pages[pages.length-1];if(last&&last.url().startsWith(origin+'/')&&!new URL(last.url()).pathname.startsWith('/api/')){writeFileSync(resolve(local,'frontend-browser-failure.txt'),(await last.locator('body').innerText()).replace(/[A-Za-z0-9_-]{40,}/g,'[opaque value omitted]'));await last.screenshot({path:resolve(local,'frontend-browser-failure.png'),fullPage:true});}}
}
finally {
 if(context){try{await context.request.post(origin+'/api/auth/logout',{headers:{Origin:origin}});}catch{}await context.close();}if(browser)await browser.close();
 if(user?.id)try{await adminLogin();const r=await request(issuer.origin+'/admin/realms/fraud/users/'+user.id,{method:'DELETE',headers:adminHeaders});removedUser=r.status===204;if(!removedUser)failure??='Temporary provider user cleanup failed';}catch{failure??='Temporary provider user cleanup failed';}
 const report={at:new Date().toISOString(),profile:'authenticated-local-mysql',passed:!failure,suites,requestCount:trace.length,syntheticTenant:tenant,temporaryProviderUserRemoved:removedUser,retainedSyntheticMySQLData:true,failure};
 writeFileSync(resolve(output,'report.json'),JSON.stringify(report,null,2)+'\n');writeFileSync(resolve(output,'http.jsonl'),trace.map(v=>JSON.stringify(v)).join('\n')+'\n');
 console.log(JSON.stringify({passed:report.passed,suites:suites.length,requests:trace.length,report:resolve(output,'report.json')}));if(failure)process.exitCode=1;
}
