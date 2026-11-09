import assert from 'node:assert/strict';
import {readFileSync, writeFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {spawnSync} from 'node:child_process';

// Run from the service directory after copying the complete Go JSON output.
const output = process.argv[2] ?? 'evidence/mysql-worker-migration/2026-11-05';
const read = path => readFileSync(path, 'utf8');
const json = path => JSON.parse(read(path));
const hash = value => createHash('sha256').update(value).digest('hex');
function docker(args, input) {
  const r = spawnSync('docker', args, {input, encoding:'utf8', timeout:30000});
  assert.equal(r.status, 0, r.stderr || r.error?.message);
  return r.stdout.trim();
}
function sql(query) {
  return docker(['exec','-i','fraud-mysql-dev-mysql-1','sh','-c',
    'MYSQL_PWD="$(cat /run/secrets/mysql_root_password)" exec mysql --protocol=socket -uroot --database=fraud --batch --skip-column-names'], query);
}
const events = read(output+'/full-tests.jsonl').trim().split('\n').map(JSON.parse);
assert.equal(events.filter(e=>e.Action==='fail'||e.Action==='build-fail').length,0,'Regression failures');
assert.equal(events.filter(e=>e.Action==='skip'&&e.Test).length,0,'Unexpected skipped tests');
assert.ok(!events.some(e=>/WARNING: DATA RACE|race detected|panic: test timed out/.test(e.Output??'')));
assert.equal(read(output+'/vet.log').trim(),'','Vet must pass without diagnostics');
const packages = events.filter(e=>e.Action==='pass'&&!e.Test).map(e=>e.Package);
for(const expected of ['internal/mysqlruntime','internal/store/mysql/metadata','internal/store/mysql/installation','mysqlstore/workers','mysqlstore/localdev']) {
  assert.ok(packages.some(p=>p.endsWith('/'+expected)), 'Missing passing package '+expected);
}
const passes = events.filter(e=>e.Action==='pass'&&e.Test);
const live = json(output+'/live-acceptance.json');
const endpoints = json(output+'/endpoints/report.json');
assert.equal(live.passed,true); assert.equal(live.checks.length,7);
assert.equal(endpoints.passed,true); assert.equal(endpoints.operationsCovered,43);
const planPath='../../.local/mysql-dev/plans/data_model.json';
const plan=json(planPath), previous=json(planPath+'.before-upgrade');
assert.deepEqual(plan.steps.slice(0,previous.steps.length),previous.steps);
assert.equal(plan.steps.length,previous.steps.length+2);
assert.deepEqual(plan.steps.slice(-2).map(s=>s.id),['data_model_v007_jobs_table_scope','data_model_v007_intent_epoch']);
const indexes=sql("SELECT table_name,index_name,GROUP_CONCAT(column_name ORDER BY seq_in_index) FROM information_schema.statistics WHERE table_schema='fraud' AND index_name IN ('data_model_jobs_table_scope','data_model_intent_epoch') GROUP BY table_name,index_name ORDER BY table_name;");
assert.ok(indexes.includes('tenant_id,table_id,requested_at,id'));
assert.ok(indexes.includes('tenant_id,table_id,expected_epoch,state'));
const hex=id=>{assert.match(id,/^[a-f0-9-]{36}$/);return "UNHEX('"+id.replaceAll('-','')+"')";};
const jobPlan=sql(`EXPLAIN SELECT id FROM data_model_index_jobs WHERE tenant_id=${hex(live.tenant)} AND table_id=${hex(live.table)} ORDER BY requested_at DESC,id DESC LIMIT 100;`);
const intentPlan=sql(`EXPLAIN SELECT desired_shape FROM data_model_schema_change_intents WHERE tenant_id=${hex(live.tenant)} AND table_id=${hex(live.table)} AND expected_epoch=0 AND state='published' LIMIT 2;`);
assert.ok(jobPlan.includes('data_model_jobs_table_scope'));
assert.ok(intentPlan.includes('data_model_intent_epoch'));
const inspect=JSON.parse(docker(['inspect','fraud-mysql-dev-data-model-worker-1']))[0];
docker(['exec','fraud-mysql-dev-data-model-worker-1','/app/mysql-worker','--healthcheck']);
assert.equal(inspect.State.Running,true); assert.equal(inspect.State.Paused,false);
assert.equal(inspect.State.Health.Status,'healthy');
const report={passed:true,recordedAt:new Date().toISOString(),scope:'Local development administrator MySQL runtime',
  regression:{vet:'passed',packages:packages.length,topLevelTests:passes.filter(e=>!e.Test.includes('/')).length,testAndSubtestPasses:passes.length,failed:0,skippedTests:0,raceWarnings:0,packageNames:packages},
  endpoints:{passed:true,operations:endpoints.operationsCovered,suites:endpoints.suitesPassed,requests:endpoints.requestCount,workerPausedForQueueAssertions:true},
  live:{passed:true,checks:live.checks},
  installation:{previousSteps:previous.steps.length,currentSteps:plan.steps.length,retainedPrefixUnchanged:true,previousArtifactSHA256:hash(read(planPath+'.before-upgrade')),currentArtifactSHA256:hash(read(planPath)),indexes,jobPlan,intentPlan},
  worker:{imageID:inspect.Image,running:inspect.State.Running,paused:inspect.State.Paused,health:inspect.State.Health.Status,startedAt:inspect.State.StartedAt},
  limitations:['Explicit local administrator mode; production identity/grants and River cutover not accepted','No representative sustained load acceptance','Text BTREE prefixes do not establish PostgreSQL trigram performance parity']};
writeFileSync(output+'/report.json',JSON.stringify(report,null,2)+'\n');
console.log(JSON.stringify(report,null,2));
