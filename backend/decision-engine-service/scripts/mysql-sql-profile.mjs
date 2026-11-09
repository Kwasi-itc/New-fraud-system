// Read-only, cumulative-counter deltas. Never resets shared server statistics.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdirSync,writeFileSync} from 'node:fs';
import {dirname,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {setTimeout as delay} from 'node:timers/promises';
const service=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const seconds=Number(process.argv[2]??20);
assert.ok(Number.isInteger(seconds)&&seconds>=5&&seconds<=60);
function snapshot(){
 const query="SELECT JSON_OBJECT('digest',DIGEST,'sql',DIGEST_TEXT,'count',COUNT_STAR,'picoseconds',SUM_TIMER_WAIT,'rowsExamined',SUM_ROWS_EXAMINED,'rowsAffected',SUM_ROWS_AFFECTED) FROM performance_schema.events_statements_summary_by_digest WHERE SCHEMA_NAME='fraud' AND DIGEST IS NOT NULL; SELECT JSON_OBJECT('status',VARIABLE_NAME,'value',VARIABLE_VALUE) FROM performance_schema.global_status WHERE VARIABLE_NAME IN ('Com_stmt_execute','Com_stmt_prepare','Com_commit','Queries','Innodb_row_lock_time','Innodb_buffer_pool_reads','Innodb_buffer_pool_read_requests','Performance_schema_digest_lost')";
 const accounts="; SELECT JSON_OBJECT('account',USER,'count',SUM(COUNT_STAR),'picoseconds',SUM(SUM_TIMER_WAIT)) FROM performance_schema.events_statements_summary_by_account_by_event_name WHERE USER IS NOT NULL AND USER<>'root' GROUP BY USER";
 const r=spawnSync('docker',['exec','-i','fraud-mysql-dev-mysql-1','sh','-ec','export MYSQL_PWD="$(cat /run/secrets/mysql_root_password)"; exec mysql --protocol=socket -uroot --batch --raw --skip-column-names'],{input:query+accounts+';\n',encoding:'utf8',timeout:30000,maxBuffer:8<<20,windowsHide:true});
 if(r.error||r.status!==0)throw Error(r.error?.message??r.stderr);
 const rows=r.stdout.trim().split(/\r?\n/).filter(Boolean).map(JSON.parse);
 return {digests:rows.filter(r=>r.digest),accounts:rows.filter(r=>r.account),status:Object.fromEntries(rows.filter(r=>r.status).map(r=>[r.status,Number(r.value)]))};
}
const start=new Date(),before=snapshot();await delay(seconds*1000);const after=snapshot(),end=new Date();
const prior=new Map(before.digests.map(r=>[r.digest,r]));
const deltas=after.digests.map(r=>{const p=prior.get(r.digest)??{count:0,picoseconds:0,rowsExamined:0,rowsAffected:0};return {...r,count:r.count-p.count,totalMs:(r.picoseconds-p.picoseconds)/1e9,rowsExamined:r.rowsExamined-p.rowsExamined,rowsAffected:r.rowsAffected-p.rowsAffected,picoseconds:undefined};}).filter(r=>r.count>0).sort((a,b)=>b.totalMs-a.totalMs);
const output=resolve(service,'evidence/mysql-sql-profile',start.toISOString().replaceAll(':','-'));
mkdirSync(output,{recursive:true});
const statusDelta=Object.fromEntries(Object.entries(after.status).map(([k,v])=>[k,v-before.status[k]]));
const accounts=after.accounts.map(r=>{const p=before.accounts.find(p=>p.account===r.account)??{count:0,picoseconds:0};return {account:r.account,statementEvents:r.count-p.count,totalMs:(r.picoseconds-p.picoseconds)/1e9};}).filter(r=>r.statementEvents>0);
const report={start,end,elapsedSeconds:(end-start)/1000,scope:'Server-wide counters, plus available fraud schema digests; includes every service and excludes client/network time. Digests may omit prepared statements or overflow. Account event counts can include nested protocol/SQL events. Concurrent statement durations can overlap. Window includes Docker snapshot overhead.',statusBefore:before.status,statusAfter:after.status,statusDelta,accounts,count:deltas.reduce((n,r)=>n+r.count,0),totalStatementMs:deltas.reduce((n,r)=>n+r.totalMs,0),deltas};
writeFileSync(resolve(output,'report.json'),JSON.stringify(report,null,2)+'\n');
console.log(JSON.stringify({output,...report,deltas:deltas.slice(0,12)},null,2));
