import assert from 'node:assert/strict';
import {randomUUID,createHash} from 'node:crypto';
import {mkdirSync,readFileSync,writeFileSync} from 'node:fs';
import {dirname,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import http from 'node:http';
import {performance} from 'node:perf_hooks';
import {spawn,spawnSync} from 'node:child_process';
import {setTimeout as delay} from 'node:timers/promises';
import {expected} from './generate-fraud-fixtures.mjs';

const service=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const args=process.argv.slice(2),options={};
for(let i=0;i<args.length;i+=2){assert.ok(['--duration','--rates','--concurrency','--target-ms','--profile','--fixture'].includes(args[i]),'Unknown option '+args[i]);assert.ok(args[i+1],'Option value required');options[args[i]]=args[i+1];}
assert.ok(['true','false'].includes(options['--profile']??'true'),'--profile requires true or false');
const profileEnabled=(options['--profile']??'true')==='true';
const seconds=Number(options['--duration']??30),rates=(options['--rates']??'10,25,50').split(',').map(Number),concurrency=Number(options['--concurrency']??8),targetMs=Number(options['--target-ms']??50);
assert.ok(Number.isInteger(seconds)&&seconds>=5&&seconds<=60);
assert.ok(rates.length>=1&&rates.length<=5&&rates.every(r=>Number.isInteger(r)&&r>=1&&r<=500));
assert.ok(Number.isInteger(concurrency)&&concurrency>=1&&concurrency<=128);
assert.ok(Number.isFinite(targetMs)&&targetMs>0&&seconds*rates.reduce((a,b)=>a+b,0)<=30000,'Bounded request budget required');
const fixtureSource=resolve(service,options['--fixture']??readFileSync(resolve(service,'evidence/mysql-fraud/latest.txt'),'utf8').trim());
const fixtureReport=JSON.parse(readFileSync(resolve(fixtureSource,'report.json'),'utf8'));
assert.equal(fixtureReport.passed,true,'Use a passing synthetic fraud fixture');
const tenant=fixtureReport.fixtures.tenants[0];
const datasetBytes=readFileSync(resolve(service,'testdata/fraud-synthetic-transactions.json'));
const dataset=JSON.parse(datasetBytes);assert.equal(dataset.synthetic,true);assert.equal(dataset.rows.length,256);
for(const row of dataset.rows)assert.deepEqual(row.expected,expected(row.fields),'Independent oracle drift');
const output=resolve(service,'evidence/mysql-http-performance',new Date().toISOString().replaceAll(':','-'));
mkdirSync(output,{recursive:true});writeFileSync(resolve(service,'evidence/mysql-http-performance/latest.txt'),output+'\n');
const origin=new URL('http://127.0.0.1:8082');
const agent=new http.Agent({keepAlive:true,maxSockets:concurrency,maxFreeSockets:concurrency,scheduling:'fifo'});
const runId=randomUUID(),records=[],resources=[],resourceRaw=[],phases=[];
const names=['decision-engine-service','decision-engine-worker','decision-local-providers','mysql','data-model-service'].map(n=>'fraud-mysql-dev-'+n+'-1');
function docker(args){const r=spawnSync('docker',args,{encoding:'utf8',timeout:30000,maxBuffer:2<<20,windowsHide:true});if(r.error||r.status!==0)throw Error(r.error?.message??r.stderr);return r.stdout.trim();}
function runtime(){return names.map(name=>{const r=JSON.parse(docker(['inspect','--format','{{json .}}',name]));return {name,id:r.Id,image:r.Image,startedAt:r.State.StartedAt,restarts:r.RestartCount,health:r.State.Health?.Status,state:r.State.Status,cpuLimit:r.HostConfig.NanoCpus/1e9||null,memoryLimitBytes:r.HostConfig.Memory||null};});}
function identity(items){return items.map(({health,state,...rest})=>rest);}
function stats(values){if(!values.length)return null;const sorted=[...values].sort((a,b)=>a-b),q=p=>sorted[Math.max(0,Math.ceil(sorted.length*p)-1)],round=v=>Math.round(v*1000)/1000;return {samples:sorted.length,minMs:round(sorted[0]),meanMs:round(sorted.reduce((a,b)=>a+b,0)/sorted.length),p50Ms:round(q(.5)),p95Ms:round(q(.95)),p99Ms:round(q(.99)),maxMs:round(sorted.at(-1)),atOrBelowTargetPercent:round(sorted.filter(v=>v<=targetMs).length/sorted.length*100)};}
function request(method,path,body,base=origin){
 const wire=body===undefined?undefined:JSON.stringify(body);
 return new Promise(resolveRequest=>{
  const start=performance.now();let firstByte,settled=false;
  const finish=value=>{if(settled)return;settled=true;resolveRequest({...value,durationMs:performance.now()-start,firstByteMs:firstByte});};
  const headers=wire===undefined?{}:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(wire)};
  if(profileEnabled&&method==='POST'&&path.endsWith('/evaluate'))headers['X-Decision-Profile']='1';
  const req=http.request(new URL(path,base),{method,agent,headers},res=>{
   firstByte=performance.now()-start;const chunks=[];let bytes=0;
   res.on('data',chunk=>{bytes+=chunk.length;if(bytes>1<<20){req.destroy(Error('response exceeds one MiB'));return;}chunks.push(chunk);});
   res.on('error',error=>finish({status:res.statusCode,error:error.message}));
   res.on('end',()=>finish({status:res.statusCode,bytes,serverTiming:res.headers['server-timing'],body:Buffer.concat(chunks).toString('utf8')}));
  });
  req.setTimeout(5000,()=>req.destroy(Error('client timeout; decision commit outcome unknown')));
  req.on('error',error=>finish({status:null,error:error.message}));
  if(wire!==undefined)req.write(wire);req.end();
 });
}
async function json(method,path,body){const result=await request(method,path,body);assert.equal(result.status,200,result.error??result.body);return JSON.parse(result.body);}
const parent='/v1/tenants/'+tenant.tenant+'/scenarios/'+tenant.scenario;
let sequence=0;
async function evaluate(phase,scheduledAt){
 const n=sequence++,row=dataset.rows[n%dataset.rows.length],startedAt=performance.now(),objectId='perf-'+runId+'-'+n;
 const result=await request('POST',parent+'/evaluate',{object_id:objectId,object_type:'fraud_transactions',fields:row.fields});
 const entry={phase,sequence:n,objectId,scheduledDelayMs:scheduledAt===undefined?0:startedAt-scheduledAt,durationMs:result.durationMs,firstByteMs:result.firstByteMs,status:result.status,bytes:result.bytes,expectedScore:row.expected.score,expectedOutcome:row.expected.outcome,validated:false};
 entry.serverTimings=Object.fromEntries((result.serverTiming??'').split(',').map(v=>v.trim().match(/^([a-z0-9_]+);dur=([0-9.]+)$/)).filter(Boolean).map(m=>[m[1],Number(m[2])]));
 try{
  assert.equal(result.status,200,result.error??result.body);
  if(profileEnabled)assert.ok(Number.isFinite(entry.serverTimings.direct_total),'Opt-in server timings missing');
  const value=JSON.parse(result.body).result;assert.equal(value.triggered,true);assert.equal(value.decision.tenant_id,tenant.tenant);assert.equal(value.decision.score,row.expected.score);assert.equal(value.decision.outcome,row.expected.outcome);assert.equal(value.rule_executions.length,5);assert.deepEqual(value.rule_executions.filter(r=>r.outcome==='hit').map(r=>r.rule_name).sort(),[...row.expected.hit_rules].sort());
  entry.validated=true;entry.decisionId=value.decision.id;
 }catch(error){entry.error=error.message.slice(0,1000);}
 records.push(entry);return entry;
}
async function phase(rate){
 const label=rate+'rps',count=rate*seconds;
 // Open the connection pool and execute real persisted warm-up decisions.
 // A high admission cap must not turn warm-up into a synchronized load spike.
 // Exercise the configured eight-connection API pool before paced traffic.
 const warmed=await Promise.all(Array.from({length:Math.min(concurrency,8)},()=>evaluate('warmup-'+label)));
 assert.ok(warmed.every(r=>r.validated),'Warm-up failed; measured phase was not started');
 const active=new Set(),phaseRecords=[];let skipped=0;
 const start=performance.now();
 for(let n=0;n<count;n++){
  const planned=start+n*1000/rate,wait=planned-performance.now();if(wait>0)await delay(wait);
  if(active.size>=concurrency){skipped++;records.push({phase:label,skipped:true,reason:'client_max_inflight',scheduledDelayMs:performance.now()-planned});continue;}
  const p=evaluate(label,planned).then(r=>phaseRecords.push(r));active.add(p);p.finally(()=>active.delete(p));
 }
 await delay(Math.max(0,start+seconds*1000-performance.now()));await Promise.all(active);
 const elapsed=(performance.now()-start)/1000,good=phaseRecords.filter(r=>r.validated),bad=phaseRecords.filter(r=>!r.validated);
 const latency=stats(good.map(r=>r.durationMs));
const result={name:label,configuredSeconds:seconds,offeredRate:rate,concurrencyLimit:concurrency,offered:count,sent:phaseRecords.length,validated:good.length,errors:bad.length,clientSkipped:skipped,elapsedSeconds:elapsed,completedPerSecond:good.length/elapsed,successfulResponseLatency:latency,firstByteLatency:stats(good.map(r=>r.firstByteMs)),schedulerDelay:stats(phaseRecords.map(r=>r.scheduledDelayMs)),targetP95Ms:targetMs,targetMet:bad.length===0&&skipped===0&&good.length===count&&latency.p95Ms<=targetMs};
 if(profileEnabled){const stages=[...new Set(good.flatMap(r=>Object.keys(r.serverTimings)))];result.serverStageLatencies=Object.fromEntries(stages.map(stage=>[stage,stats(good.map(r=>r.serverTimings[stage]??0))]));}
 result.profileTimings=profileEnabled;
 phases.push(result);console.log(JSON.stringify(result));
 const sample=good.at(-1);
 if(sample){const saved=await json('GET','/v1/tenants/'+tenant.tenant+'/decisions/'+sample.decisionId);assert.equal(saved.decision.score,sample.expectedScore);assert.equal(saved.decision.outcome,sample.expectedOutcome);}
 if(bad.length/count>.1||skipped/count>.2){stopReason='Stopped load ramp after excessive errors or client-capacity skips';console.error(stopReason);return false;}
 await delay(5000);
 return true;
}
let before,after,baseline,modelBaseline,failure,monitor,stopReason;
let statsBuffer='';
function resourceLine(line){const clean=line.replace(/\u001b\[[0-?]*[ -/]*[@-~]/g,'').trim();if(!clean)return;const start=clean.indexOf('{');if(start<0)return;try{resources.push({at:new Date().toISOString(),...JSON.parse(clean.slice(start))});}catch{}}
try{
 before=runtime();
 assert.ok(before.every(r=>r.state==='running'),'Required containers must be running');
 assert.equal((await json('GET','/capabilities')).profile,'local-development');
 const scenario=(await json('GET',parent)).scenario;assert.ok(scenario.name.startsWith('Synthetic payment fraud rules'),'Use only retained synthetic fixtures');
 for(const [route,key]of [['workflows','workflows'],['screening-configs','screening_configs'],['scoring-configs','scoring_configs']]){const config=await json('GET',parent+'/'+route);assert.ok(config[key].every(c=>!c.active),'Benchmark scenario must have no active optional provider configuration');}
 // Resource sampling runs in a separate process; no synchronous Docker calls
 // or file writes interrupt the measured request generator.
 monitor=spawn('docker',['stats','--format','{{json .}}',...names],{stdio:['ignore','pipe','pipe'],windowsHide:true});
 monitor.stdout.setEncoding('utf8');monitor.stdout.on('data',chunk=>{resourceRaw.push(chunk);statsBuffer+=chunk;const lines=statsBuffer.split(/\r?\n/);statsBuffer=lines.pop();for(const line of lines)resourceLine(line);});monitor.on('error',error=>resources.push({error:error.message}));monitor.stderr.on('data',chunk=>resources.push({error:chunk.toString()}));
 const controls=[];for(let i=0;i<100;i++){const r=await request('GET','/healthz');assert.equal(r.status,200);controls.push(r.durationMs);}baseline=stats(controls);
 const models=[];for(let i=0;i<20;i++){const r=await request('GET','/v1/tenants/'+tenant.tenant+'/data-model',undefined,new URL('http://127.0.0.1:8080'));assert.equal(r.status,200);models.push(r.durationMs);}modelBaseline=stats(models);
 for(let i=0;i<20;i++){const r=await evaluate('initial-warmup');assert.equal(r.validated,true,r.error);}
 for(const rate of rates)if(!await phase(rate))break;
}catch(error){failure=error.stack;console.error(error.message);}
finally{
 agent.destroy();if(monitor){const stopped=new Promise(resolve=>monitor.once('close',resolve));monitor.kill();await Promise.race([stopped,delay(3000)]);}resourceLine(statsBuffer);
 after=runtime();
 const stable=before&&JSON.stringify(identity(before))===JSON.stringify(identity(after));
 const warmups=records.filter(r=>r.phase?.includes('warmup'));
 const measurementValid=!failure&&stable&&warmups.every(r=>r.validated)&&phases.length>0;
 const report={at:new Date().toISOString(),origin:origin.href,fixtureSource,tenant:tenant.tenant,scenario:tenant.scenario,datasetSha256:createHash('sha256').update(datasetBytes).digest('hex'),rules:5,scope:'Completed synchronous HTTP decision including model retrieval, SQL persistence and durable outbox production; excludes asynchronous delivery completion',requestGenerator:{mode:'open-loop offered rate with a bounded in-flight cap; skips are reported, no retries',maxInflight:concurrency,timeoutMs:5000,keepAlive:true},targetP95Ms:targetMs,baselineHealthLatency:baseline,baselineModelReadLatency:modelBaseline,warmupRequests:warmups.length,warmupErrors:warmups.filter(r=>!r.validated).length,before,after,runtimeUnchanged:stable,resourceSamples:resources.length,phases,failure,stopReason,valid:measurementValid,measurementValid,offeredLoadMet:phases.length===rates.length&&phases.every(p=>p.clientSkipped===0&&p.errors===0),targetMet:measurementValid&&phases.length===rates.length&&phases.every(p=>p.targetMet)};
 writeFileSync(resolve(output,'requests.jsonl'),records.map(r=>JSON.stringify(r)).join('\n')+'\n');writeFileSync(resolve(output,'resources.jsonl'),resources.map(r=>JSON.stringify(r)).join('\n')+'\n');writeFileSync(resolve(output,'docker-stats.raw.txt'),resourceRaw.join(''));writeFileSync(resolve(output,'report.json'),JSON.stringify(report,null,2)+'\n');
 console.log(JSON.stringify({output,measurementValid,offeredLoadMet:report.offeredLoadMet,targetMet:report.targetMet,baselineHealthLatency:baseline,baselineModelReadLatency:modelBaseline,phases},null,2));process.exitCode=report.valid?0:1;
}
