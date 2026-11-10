// Read-only acceptance against local application services. Credentials/JWTs stay
// in memory; no users, cases, inboxes, or delivery notifications are created.
import assert from 'node:assert/strict';
import {createPublicKey,verify,randomUUID} from 'node:crypto';
import {readFileSync,mkdirSync,writeFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
const root=fileURLToPath(new URL('../../../',import.meta.url));
const local=resolve(root,'.local/mysql-dev'),evidence=resolve(root,'backend/case-manager-service/evidence');
const trace=[];
async function request(method,path,body,status=200,token='',origin='http://127.0.0.1:8086'){
 const response=await fetch(origin+path,{method,redirect:'error',signal:AbortSignal.timeout(15000),headers:{...(token?{Authorization:'Bearer '+token}:{}),'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});
 trace.push({method,path,status:response.status,expected:status});assert.equal(response.status,status,method+' '+path+' returned an unexpected status');
 const text=await response.text();return response.headers.get('content-type')?.startsWith('application/json')&&text?JSON.parse(text):text;
}
let browser;
try{
 await request('GET','/healthz');await request('GET','/readyz');
 assert.ok((await request('GET','/docs')).includes('SwaggerUIBundle'));
 const spec=await request('GET','/openapi.json');assert.equal(spec.openapi,'3.0.3');
 const operations=Object.values(spec.paths).reduce((n,v)=>n+Object.keys(v).filter(k=>['get','post','put','patch','delete'].includes(k)).length,0);assert.equal(operations,61);
 await request('GET','/v1/service-info',undefined,401);await request('POST','/internal/v1/workflow-actions',{},401);
 const proxy=readFileSync(resolve(local,'auth-proxy-token.txt'),'utf8');
 // The decision setup retains its seed as hex and stores the digest of its
 // base64url credential. CASE_SESSION_FILE accepts an already opaque session.
 const session=process.env.CASE_SESSION_FILE
  ?readFileSync(process.env.CASE_SESSION_FILE,'utf8').trim()
  :Buffer.from(readFileSync(resolve(local,'decision-review-session.txt'),'utf8').trim(),'hex').toString('base64url');
 const resolved=await request('POST','/internal/session',{session},200,proxy,'http://127.0.0.1:8087');
 assert.equal(typeof resolved.access_token,'string');const [header,payload,signature]=resolved.access_token.split('.');
 assert.ok(verify('RSA-SHA256',Buffer.from(header+'.'+payload),createPublicKey(readFileSync(resolve(local,'auth-public.pem'))),Buffer.from(signature,'base64url')));
 const claims=JSON.parse(Buffer.from(payload,'base64url')),tenant=resolved.session.tenant_id;
 assert.equal(claims.tenant_id,tenant);assert.ok(claims.roles.includes('case_manager_admin')||claims.roles.includes('case_investigator'),'An active case-role development session is required');
 const token=resolved.access_token,prefix='/v1/tenants/'+tenant;
 const current=await request('GET',prefix+'/case-session',undefined,200,token);assert.equal(current.tenant_id,tenant);
 const inboxes=await request('GET',prefix+'/inboxes',undefined,200,token);
 await request('GET',prefix+'/cases',undefined,200,token);await request('GET',prefix+'/case-queue?limit=2',undefined,200,token);
 const now=Date.now();await request('GET',prefix+'/case-analytics?from='+new Date(now-3600000).toISOString()+'&to='+new Date(now+3600000).toISOString(),undefined,200,token);
 await request('GET','/v1/tenants/'+randomUUID()+'/cases',undefined,403,token);
 const service=readFileSync(resolve(local,'case-service-token.txt'),'utf8');
 const inbox=inboxes.inboxes?.[0];if(inbox)await request('GET','/internal/v1/tenants/'+tenant+'/inboxes/'+inbox.id,undefined,200,service);
 await request('GET','/internal/v1/tenants/'+randomUUID()+'/inboxes/'+randomUUID(),undefined,403,service);
 const {chromium}=createRequire(resolve(root,'frontend/package.json'))('@playwright/test');browser=await chromium.launch({headless:true});
 const page=await browser.newPage();await page.goto('http://127.0.0.1:8086/docs',{waitUntil:'networkidle',timeout:45000});
 await page.waitForFunction(()=>document.querySelectorAll('.opblock').length===61,undefined,{timeout:30000});
 mkdirSync(evidence,{recursive:true});await page.screenshot({path:resolve(evidence,'mysql-docs.png'),fullPage:true});
 const report={verified_at:new Date().toISOString(),passed:true,read_only_application_smoke:true,auth_session:'existing local session resolved by auth service',browser_operations:operations,checks:trace};
 writeFileSync(resolve(evidence,'mysql-runtime-smoke.json'),JSON.stringify(report,null,2)+'\n');
 console.log(JSON.stringify({passed:true,checks:trace.length,browserOperations:operations},null,2));
}catch(error){console.error('Case MySQL local acceptance failed:',error.message);process.exitCode=1;}
finally{await browser?.close();}
