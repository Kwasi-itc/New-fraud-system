// Local acceptance uses an existing auth session and creates one labelled
// synthetic manual search. Secrets and access tokens remain in memory.
import assert from 'node:assert/strict';
import {createPublicKey,verify,randomUUID} from 'node:crypto';
import {readFileSync,mkdirSync,writeFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
import {execFileSync} from 'node:child_process';
const root=fileURLToPath(new URL('../../../',import.meta.url));
const local=resolve(root,'.local/mysql-dev'),evidence=resolve(root,'backend/screening-service/evidence'),trace=[];
async function request(method,path,body,status=200,token='',origin='http://127.0.0.1:8085',extraHeaders={}){
 const response=await fetch(origin+path,{method,redirect:'error',signal:AbortSignal.timeout(15000),headers:{...(token?{Authorization:'Bearer '+token}:{}),'Content-Type':'application/json',...extraHeaders},body:body===undefined?undefined:JSON.stringify(body)});
 trace.push({method,path,status:response.status,expected:status});assert.equal(response.status,status,method+' '+path+' returned an unexpected status');
 const text=await response.text();return response.headers.get('content-type')?.startsWith('application/json')&&text?JSON.parse(text):text;
}
let browser;
try{
 await request('GET','/healthz');await request('GET','/readyz');
 assert.ok((await request('GET','/docs')).includes('SwaggerUIBundle'));
 const spec=await request('GET','/openapi.json');assert.ok(/^3\./.test(spec.openapi));
 const operations=Object.values(spec.paths).reduce((n,v)=>n+Object.keys(v).filter(k=>['get','post','put','patch','delete'].includes(k)).length,0);
 await request('GET','/v1/manual-screenings',undefined,401);
 await request('GET','/internal/screening-capabilities',undefined,401);
 const proxy=readFileSync(resolve(local,'auth-proxy-token.txt'),'utf8');
 const session=process.env.SCREENING_SESSION_FILE?readFileSync(process.env.SCREENING_SESSION_FILE,'utf8').trim():Buffer.from(readFileSync(resolve(local,'decision-review-session.txt'),'utf8').trim(),'hex').toString('base64url');
 const resolved=await request('POST','/internal/session',{session},200,proxy,'http://127.0.0.1:8087');
 assert.equal(typeof resolved.access_token,'string');const [header,payload,signature]=resolved.access_token.split('.');
 assert.ok(verify('RSA-SHA256',Buffer.from(header+'.'+payload),createPublicKey(readFileSync(resolve(local,'auth-public.pem'))),Buffer.from(signature,'base64url')));
 const claims=JSON.parse(Buffer.from(payload,'base64url')),tenant=resolved.session.tenant_id,token=resolved.access_token;
 assert.equal(claims.tenant_id,tenant);
 const capabilities=await request('GET','/v1/manual-screenings/capabilities',undefined,200,token);
 assert.equal(capabilities.configured,true);
 await request('GET','/v1/manual-screenings',undefined,200,token);
 await request('GET','/v1/investigator/tenants/'+randomUUID()+'/cases/'+randomUUID()+'/review',undefined,403,token);
 const input={operation_id:randomUUID(),name:'Alex Example',entity_type:'Person',threshold:90,limit:10};
 const started=await request('POST','/v1/manual-screenings',input,202,token);
 const replay=await request('POST','/v1/manual-screenings',input,202,token);assert.equal(replay.id,started.id);
 let details;
 for(let i=0;i<30;i++){
  details=await request('GET','/v1/manual-screenings/'+started.id,undefined,200,token);
  if(details.matches?.length)break;
  await new Promise(resolve=>setTimeout(resolve,1000));
 }
 assert.equal(details.matches?.length,1,'Docker worker must persist the synthetic match');
 assert.equal(details.matches[0].entity_id,'local-person-001');
 const caseOrigin='http://127.0.0.1:8086',casePrefix='/v1/tenants/'+tenant;
 const inboxes=await request('GET',casePrefix+'/inboxes',undefined,200,token,caseOrigin);
 const inbox=inboxes.inboxes.find(item=>item.status==='active');assert.ok(inbox,'An active development inbox is required');
 const investigation=await request('POST',casePrefix+'/manual-screening-investigations',{screening_id:started.id,candidate_id:details.matches[0].id,inbox_id:inbox.id},200,token,caseOrigin);
 const casePath=casePrefix+'/cases/'+investigation.case.id;
 const bytes='Synthetic screening evidence.\n';
 const owner=readFileSync(resolve(local,'screening-service-token.txt'),'utf8').trim(),operator=readFileSync(resolve(local,'screening-operator-token.txt'),'utf8').trim();
 const upload=await request('POST',casePrefix+'/screenings/'+started.id+'/file-uploads',{file_name:'synthetic-screening-note.txt',content_type:'text/plain',file_size:Buffer.byteLength(bytes),uploaded_by:claims.sub},201,owner,'http://127.0.0.1:8085',{'X-Screening-Operator-Token':operator});
 const capability=new URL(upload.session.upload_url);assert.ok(['localhost','127.0.0.1'].includes(capability.hostname)&&capability.port==='8085'&&capability.pathname.startsWith('/local/evidence/'));
 const uploaded=await fetch(capability,{method:'PUT',redirect:'error',signal:AbortSignal.timeout(15000),headers:{'Content-Type':'text/plain'},body:bytes});
 trace.push({method:'PUT',path:'/local/evidence/:capability',status:uploaded.status,expected:204});assert.equal(uploaded.status,204);
 const sourceFile=upload.file.ID??upload.file.id;let imported;
 for(let i=0;i<30;i++){
  const page=await request('GET',casePath+'/links/files',undefined,200,token,caseOrigin);
  imported=page.items.find(file=>file.source_file_id===sourceFile);
  if(imported)break;
  await new Promise(resolve=>setTimeout(resolve,1000));
 }
 assert.ok(imported,'Docker worker must deliver the owner evidence callback to its case');
 assert.equal(await request('GET',casePath+'/files/'+imported.id+'/download',undefined,200,token,caseOrigin),bytes);
 const duplicate=await request('POST',casePath+'/files',{source_file_id:sourceFile},201,token,caseOrigin);assert.equal(duplicate.file.id,imported.id);
 const {chromium}=createRequire(resolve(root,'frontend/package.json'))('@playwright/test');browser=await chromium.launch({headless:true});
 const page=await browser.newPage();await page.goto('http://127.0.0.1:8085/docs',{waitUntil:'networkidle',timeout:45000});
 await page.waitForFunction(count=>document.querySelectorAll('.opblock').length===count,operations,{timeout:30000});
 mkdirSync(evidence,{recursive:true});await page.screenshot({path:resolve(evidence,'mysql-docs.png'),fullPage:true});
 const containers=JSON.parse(execFileSync('docker',['inspect','--format','{{json .State.Health}}','fraud-mysql-dev-screening-worker-1'],{encoding:'utf8'}));
 assert.equal(containers.Status,'healthy');
 const report={verified_at:new Date().toISOString(),passed:true,auth_session:'existing session resolved by auth service',browser_operations:operations,worker_healthy:true,synthetic_worker_match:true,manual_intake_replay:true,evidence_callback_delivery:true,checks:trace};
 writeFileSync(resolve(evidence,'mysql-runtime-smoke.json'),JSON.stringify(report,null,2)+'\n');
 console.log(JSON.stringify({passed:true,checks:trace.length,browserOperations:operations,syntheticWorkerMatch:true},null,2));
}catch(error){console.error('Screening MySQL local acceptance failed:',error.message);process.exitCode=1;}
finally{await browser?.close();}
