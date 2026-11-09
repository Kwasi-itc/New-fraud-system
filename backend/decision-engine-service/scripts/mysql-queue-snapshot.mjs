// Operator-only bounded read: reports queue pressure without reading payloads.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {readFileSync,mkdirSync,writeFileSync} from 'node:fs';
import {dirname,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
const service=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const fixture=JSON.parse(readFileSync(resolve(service,'evidence/mysql-fraud/2026-11-06T12-12-00.522Z/report.json'),'utf8'));
const tenant=process.argv[2]??fixture.fixtures.tenants[0].tenant;
assert.match(tenant,/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
const hex=tenant.replaceAll('-','');
const kinds=['outbox_event','workflow_execution','screening_execution','scoring_request'];
const queries=kinds.flatMap(kind=>['available','running'].map(state=>"SELECT JSON_OBJECT('kind','"+kind+"','state','"+state+"','countUpTo500',COUNT(*),'capped',COUNT(*)=500) FROM(SELECT id FROM fraud.decision_engine_effect_job_deliveries FORCE INDEX(de_effect_delivery_claim) WHERE tenant_id=0x"+hex+" AND kind='"+kind+"' AND queue_name='local_decision_"+kind+"' AND state='"+state+"' LIMIT 500) bounded"));
const result=spawnSync('docker',['exec','-i','fraud-mysql-dev-mysql-1','sh','-ec','export MYSQL_PWD="$(cat /run/secrets/mysql_root_password)"; exec mysql --protocol=socket -uroot --batch --raw --skip-column-names'],{input:queries.join(';\n')+';\n',encoding:'utf8',timeout:30000,maxBuffer:1<<20,windowsHide:true});
if(result.error||result.status!==0)throw Error(result.error?.message??result.stderr);
const report={at:new Date().toISOString(),tenant,scope:'Point-in-time bounded transport counts; capped results are lower bounds, running includes retained expired leases',queues:result.stdout.trim().split(/\r?\n/).filter(Boolean).map(JSON.parse)};
const output=resolve(service,'evidence/mysql-queue-snapshots',report.at.replaceAll(':','-'));
mkdirSync(output,{recursive:true});writeFileSync(resolve(output,'report.json'),JSON.stringify(report,null,2)+'\n');
console.log(JSON.stringify({output,...report},null,2));
