import {existsSync,readFileSync,writeFileSync} from 'node:fs';
const readJSON=path=>existsSync(path)?JSON.parse(readFileSync(path,'utf8')):null;
const names=['router.go','manual_search.go','investigator.go','case_exclusions.go','docs.go'];
const prefix={router:'',v1:'/v1',internal:'/internal'},routes=[];
for(const name of names){const source=readFileSync('internal/httpapi/'+name,'utf8');for(const match of source.matchAll(/\b(router|v1|internal)\.(GET|POST|PUT|PATCH|DELETE)\("([^"]+)"/g))routes.push(match[2]+' '+prefix[match[1]]+match[3]);}
if(new Set(routes).size!==routes.length)throw new Error('Duplicate source route inventory');
const latest=existsSync('evidence/mysql-integration/latest.txt')?readFileSync('evidence/mysql-integration/latest.txt','utf8').trim():null;
const full=latest?readJSON(latest+'/report.json'):null,http=readJSON('evidence/mysql-http-results.json'),runtime=readJSON('evidence/mysql-runtime-smoke.json');
const positive=new Set(full?.passed&&http?.passed?http.successful_operations:[]),covered=routes.filter(route=>positive.has(route));
const lines=['# Screening MySQL endpoint status','',`Source inventory: **${routes.length} mounted operations**, including documentation and health/metrics. **${covered.length} positively verified; ${routes.length-covered.length} awaiting passing complete evidence.**`, '',
 '`[x]` requires successful native behavior in passing integration evidence. Code presence, expected rejection, synthetic data and a documented route do not establish production/provider acceptance.', '',
 '## Endpoint checklist','',...routes.map(route=>`- [${positive.has(route)?'x':' '}] \`${route}\` - ${positive.has(route)?'Successful native MySQL workflow evidenced.':'Implementation/positive integration acceptance pending.'}`),'',
 '## Verification and activation','',
 `- [x] Source endpoint inventory derived from the shared router and registration functions.`,
 `- [${full?.passed?'x':' '}] Complete screening suite, MySQL target/reference fixtures, PostgreSQL characterization, race detection and vet.`,
 `- [${http?.passed&&full?.passed?'x':' '}] Authenticated positive workflows and unauthenticated/tenant/resource denial checks.`,
 `- [${runtime?.passed?'x':' '}] Live Docker, auth and rendered Swagger acceptance at http://localhost:8085/docs.`,
 '- [ ] Real sanctions-provider/data coverage and production workload acceptance (separate release work).','',
 full?.passed?`Retained full evidence: [report](${latest}/report.json), ${full.topLevelPassed} top-level tests / ${full.testAndSubtestPasses} test/subtest results.`:'The complete evidence report has not passed yet; endpoint checkboxes remain pending.', '',
 'The default development provider searches a small built-in synthetic catalog only. It does not query OpenSanctions. Human manual/investigator APIs require a current auth access JWT. Owner routes retain their separate private operator and service credentials; integration routes use a dedicated intake credential.','',
 'See [migration plan](MYSQL_PLAN.md), [worker checklist](MYSQL_WORKER_STATUS.md) and [Docker instructions](MYSQL_DOCKER.md).',''];
writeFileSync('MYSQL_ENDPOINT_STATUS.md',lines.join('\n'));
writeFileSync('MYSQL_WORKER_STATUS.md',[
 '# Screening MySQL worker status','',
 'Native work uses durable domain rows and fenced leases. PostgreSQL River jobs are not copied into this runtime. Checked acceptance requires complete passing evidence.','',
 '## Worker checklist','',
 `- [${full?.passed?'x':' '}] Screening dispatch, recovery, match persistence and stale-claim fencing.`,
 `- [${full?.passed?'x':' '}] Monitored-object reads, continuous-run provenance and atomic pending screening creation.`,
 `- [${full?.passed?'x':' '}] Dataset catalog/delta/rescreen jobs, bounded checkpoint pages beyond 500 objects and fenced recovery.`,
 `- [${full?.passed?'x':' '}] Decision projection through the decision-owned reconciliation contract and retained source queue.`,
 `- [${full?.passed?'x':' '}] Case callback claims, retry/backoff, terminal failure and explicit replay with stable event identity.`,
 `- [${runtime?.worker_healthy?'x':' '}] Restricted screening worker healthy in local Docker.`,
 `- [${full?.passed&&runtime?.evidence_callback_delivery?'x':' '}] Case evidence callback: authoritative publication, atomic import/hash verification, replay and real Docker worker delivery.`,
 '- [ ] Real provider acceptance, production worker capacity, process recovery and queue/cutover reconciliation.','',
 'Provider calls occur outside SQL transactions. Manual results are synthetic in the default development profile. Source error responses and unconfigured integrations must not be counted as successful functional coverage.','',
 'See [endpoint checklist](MYSQL_ENDPOINT_STATUS.md) and [migration plan](MYSQL_PLAN.md).',''
].join('\n'));
console.log(JSON.stringify({sourceOperations:routes.length,verified:covered.length,remaining:routes.length-covered.length},null,2));
if(full?.passed&&runtime?.passed&&runtime?.worker_healthy){
 const plan=readFileSync('MYSQL_PLAN.md','utf8');
 const boundary=plan.indexOf('## Separate release acceptance');
 if(boundary<0)throw new Error('Release acceptance boundary missing from plan.');
 writeFileSync('MYSQL_PLAN.md',plan.slice(0,boundary).replaceAll('- [ ]','- [x]')+plan.slice(boundary));
}
