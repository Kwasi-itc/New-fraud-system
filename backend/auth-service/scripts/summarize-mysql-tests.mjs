import {readFileSync,writeFileSync} from 'node:fs';
import {resolve,relative} from 'node:path';

const directory=resolve(process.argv[2]??readFileSync('evidence/mysql-integration/latest.txt','utf8').trim());
const read=name=>readFileSync(resolve(directory,name),'utf8').replace(/^\uFEFF/,'');
const events=read('raw.jsonl').trim().split(/\r?\n/).filter(Boolean).map(line=>JSON.parse(line));
const results=events.filter(e=>e.Test&&['pass','fail','skip'].includes(e.Action));
const top=results.filter(e=>!e.Test.includes('/'));
const inventory=read('inventory.jsonl').trim().split(/\r?\n/).map(line=>JSON.parse(line));
const expected=new Set(inventory.filter(e=>e.Action==='output'&&/^Test\S+\n?$/.test(e.Output??'')).map(e=>e.Package+'::'+e.Output.trim()));
const seen=new Set(top.map(e=>e.Package+'::'+e.Test));
const missing=[...expected].filter(key=>!seen.has(key));
const passedTop=new Set(top.filter(e=>e.Action==='pass').map(e=>e.Package+'::'+e.Test));
const coverage=new Set();
for(const event of events){if(event.Action!=='output'||!event.Test||!passedTop.has(event.Package+'::'+event.Test.split('/')[0]))continue;const match=event.Output?.match(/AUTH_ROUTE_PASS (GET|POST) (\/\S+) (\d+)/);if(match)coverage.add(match[1]+' '+match[2]);}
const routes=[...coverage].sort();
const failed=results.filter(e=>e.Action==='fail').map(e=>({package:e.Package,test:e.Test}));
const skipped=results.filter(e=>e.Action==='skip').map(e=>({package:e.Package,test:e.Test}));
const packageFailed=events.filter(e=>e.Action==='fail'&&!e.Test).map(e=>e.Package);
const raceWarnings=events.filter(e=>e.Output?.includes('WARNING: DATA RACE')).length;
const exit=name=>Number(read(name).trim());
const shared=read('mysqlstore-tests.jsonl').trim().split(/\r?\n/).map(line=>JSON.parse(line));
const report={reportedAt:new Date().toISOString(),scope:'Complete auth module with private MySQL target/reference and PostgreSQL characterization; native OIDC fixtures are not external Keycloak acceptance',evidence:relative(process.cwd(),directory).replaceAll('\\','/'),environment:read('environment.txt').trim(),testsExit:exit('tests.exit'),vetExit:exit('vet.exit'),mysqlstoreVetExit:exit('mysqlstore-vet.exit'),mysqlstoreTestsExit:exit('mysqlstore-tests.exit'),topLevelPassed:top.filter(e=>e.Action==='pass').length,testAndSubtestPasses:results.filter(e=>e.Action==='pass').length,expectedTopLevelTests:expected.size,missingTests:missing,failed,skipped,packageFailed,raceWarnings,packagesPassed:events.filter(e=>e.Action==='pass'&&!e.Test).map(e=>e.Package),packagesWithoutTests:events.filter(e=>e.Action==='skip'&&!e.Test).map(e=>e.Package),nativeOperationsVerified:routes.filter(r=>!r.includes('/docs')&&!r.includes('/openapi.json')&&!r.includes('/capabilities')).length,extensionOperationsVerified:routes.filter(r=>r.includes('/docs')||r.includes('/openapi.json')||r.includes('/capabilities')).length,positiveHTTPRoutes:routes,sharedTopLevelPassed:shared.filter(e=>e.Action==='pass'&&e.Test&&!e.Test.includes('/')).length};
report.passed=report.testsExit===0&&report.vetExit===0&&report.mysqlstoreVetExit===0&&report.mysqlstoreTestsExit===0&&failed.length===0&&skipped.length===0&&packageFailed.length===0&&missing.length===0&&raceWarnings===0&&report.nativeOperationsVerified===14&&report.extensionOperationsVerified===3;
writeFileSync(resolve(directory,'report.json'),JSON.stringify(report,null,2)+'\n');
console.log(JSON.stringify(report,null,2));process.exitCode=report.passed?0:1;
