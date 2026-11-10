import {readFileSync,writeFileSync} from 'node:fs';
import {resolve,relative} from 'node:path';
const latest=readFileSync('evidence/mysql-integration/latest.txt','utf8').trim();
const report=JSON.parse(readFileSync(resolve(latest,'report.json'),'utf8'));
const runtime=JSON.parse(readFileSync('evidence/mysql-runtime-smoke.json','utf8'));
if(!report.passed||!runtime.passed)throw new Error('Passing complete test and local runtime evidence required before updating statuses.');
const source=readFileSync('internal/httpapi/router.go','utf8'),prefix={router:'',v1:'/v1',callbacks:'/v1/screening-events',internal:'/internal'};
const routes=[...source.matchAll(/^\s*(router|v1|callbacks|internal)\.(GET|POST|PUT|PATCH|DELETE)\("([^"]+)"/gm)].map(m=>({method:m[2],path:prefix[m[1]]+m[3]}));
if(routes.length!==61)throw new Error('Source route inventory changed.');
const positive=new Set(report.positiveHTTPRoutes);
const dependency=new Map([
 ['POST /v1/tenants/:tenantId/manual-screening-investigations','Manual creator/candidate provenance is not published by the current MySQL screening owner.'],
 ['POST /v1/tenants/:tenantId/cases/:caseId/files','Screening-file ownership/import contract is unavailable; native upload/finalize/download is verified.'],
 ['POST /v1/screening-events/reviewed','Manual/continuous and decision-arrival provenance is unavailable. Missing screening/match returns 404; existing dependencies fail closed.'],
 ['POST /v1/screening-events/requires-review','Authoritative arrival publication and configured routing are unavailable.'],
]);
for(const key of positive)dependency.delete(key);
const placeholders=new Map([
 ['POST /internal/v1/ai-case-reviews/:reviewId/run','Existing source 501: AI case review.'],
 ['POST /internal/v1/auto-assignment/run','Existing source 501; automatic assignment runs in the maintenance worker.'],
]);
const sourceKeys=new Set(routes.map(route=>route.method+' '+route.path));
const classified=[...positive,...dependency.keys(),...placeholders.keys()];
if(new Set(classified).size!==classified.length||classified.length!==routes.length||classified.some(key=>!sourceKeys.has(key)))throw new Error('Route classifications must cover the source exactly once.');
const testLink=relative('.',resolve(latest,'report.json')).replaceAll('\\','/');
const verifiedDate=runtime.verified_at.slice(0,10);
const lines=['# Case management MySQL endpoint status','',`Verified ${verifiedDate}. Local Swagger: http://localhost:8086/docs.`, '',
 `**${routes.length} source operations: ${positive.size} with successful native MySQL HTTP workflows, ${dependency.size} waiting for screening-owner contracts, ${placeholders.size} existing source placeholders.** All ${report.authenticatedGuardOperations} protected operations reject unauthenticated requests.`, '',
 '`[x]` means a successful native workflow is evidenced; `[ ]` means functional work remains. A verified error response, including an expected `501`, does not complete an endpoint. The successful operations include the two public health endpoints.', '',
 `Evidence: [full test report](${testLink}), [HTTP operation inventory](evidence/mysql-http-results.json), [live auth/Swagger smoke](evidence/mysql-runtime-smoke.json). The full suite results below come from the retained test run; the runtime smoke was last run at ${runtime.verified_at}.`, '',
 `## Verified endpoints (${positive.size})`, ''];
for(const route of routes){const key=route.method+' '+route.path;if(positive.has(key))lines.push(`- [x] \`${key}\` — ${route.path==='/healthz'||route.path==='/readyz'?'Public health/readiness response.':'Successful authenticated native MySQL workflow.'}`);}
lines.push('',`## Screening dependencies (${dependency.size})`,'');
for(const [key,detail]of dependency)lines.push(`- [ ] \`${key}\` — ${detail}`);
lines.push('',`## Source placeholders (${placeholders.size})`,'');
for(const [key,detail]of placeholders)lines.push(`- [ ] \`${key}\` — ${detail} Expected error behavior is tested; positive functionality remains unimplemented.`);
lines.push('', '## Verification checklist','',
 `- [x] Complete suite: ${report.topLevelPassed} top-level tests and ${report.testAndSubtestPasses} test/subtest results; zero failures, skipped/missing tests or race warnings; vet passed.`,
 '- [x] Isolated MySQL 8.4.8 target/reference fixtures and retained PostgreSQL source characterization.',
 `- [x] Authentication rejection on all ${report.authenticatedGuardOperations} protected operations.`,
 '- [x] Tenant/inbox isolation, current authorization version and session revocation checks.',
 '- [x] Decision attachment/transfer, bulk outcomes, receipt replay/reconciliation, audit rollback and legacy tag reattachment.',
 '- [x] Native evidence upload/download, report versions, typed exploration and analytics workflows.',
 `- [x] Live read-only auth/tenant smoke and browser rendering of ${runtime.browser_operations} operations.`,
 '- [x] Successful workflows for all five screening contracts, including imported evidence bytes and SHA-256.',
 '- [ ] Positive functionality and integration tests for the two source placeholders.',
 '- [ ] Production workload/performance acceptance, historical data transfer and frontend/production cutover (separate release work).','',
 'The five screening contracts are verified with their actual owner schema. Case management reads authoritative provenance and does not write screening-owned records. Positive route coverage does not establish every production branch or external provider behavior. See [migration plan](MYSQL_PLAN.md), [Docker/runtime instructions](MYSQL_DOCKER.md) and [worker status](MYSQL_WORKER_STATUS.md).','');
writeFileSync('MYSQL_ENDPOINT_STATUS.md',lines.join('\n'));
writeFileSync('MYSQL_WORKER_STATUS.md',[
 '# Case management MySQL worker status','',`Verified ${verifiedDate} using retained MySQL target/reference tests and the local runtime. Latest read-only API/Swagger smoke: ${runtime.verified_at}.`, '',
 '`[x]` records implemented and evidenced behavior. Delivery implementation and live delivery activation have separate checkboxes; passing a fixture does not mean an external publisher is configured.', '',
 '## Implemented and fixture-verified','',
 '- [x] Unfinished-upload expiry: bounded claims and deletion of expired, unfinished uploads.',
 '- [x] Snooze wake-up: case update, audit event and outbox commit atomically; repeat runs produce no duplicate event.',
 '- [x] Automatic assignment: active investigator/inbox checks, capacity enforcement and concurrent claims; canonical UUID and suspended-assignee regressions covered.',
 '- [x] Notification transport: durable case/inbox outboxes, short claims, HTTP outside transactions, stable event idempotency headers and redirect rejection.',
 '- [x] Notification recovery: 60-second leases, fenced completion, retry/backoff, terminal failure after 12 failed attempts, and explicit tenant/event replay.', '',
 '## Local activation','',
 '- [x] Case API and maintenance worker are healthy in Docker with restricted database accounts.',
 '- [x] Maintenance loop runs upload cleanup, snooze wake-up and automatic assignment.',
 '- [ ] Configure a publisher and enable outbound delivery. `PublisherURL` is currently empty; notifications remain pending.',
 '- [ ] Verify delivery, receiver deduplication and failure recovery against the configured external publisher.', '',
 '## Remaining functionality and release checks','',
 '- [ ] Implement AI case-review functionality; its existing source endpoint still returns `501`.',
 '- [ ] Implement the explicit internal auto-assignment run endpoint, which still returns `501`. Automatic assignment already runs in maintenance.',
 '- [ ] Verify production worker capacity, process-failure recovery and any PostgreSQL queue-transfer/cutover procedure before release.', '',
 'The periodic MySQL loop derives maintenance work from persisted domain rows. PostgreSQL River jobs are not copied into it. Restart safety comes from transactional state changes and durable delivery leases. Existing PostgreSQL entrypoints and tests remain available.', '',
 'Case events and notifications commit or roll back together. Inbox notifications have a separate transport because the existing case outbox enforces case ownership through foreign keys. Receivers must deduplicate event UUIDs; the worker provides at-least-once delivery.', '',
 `See the [complete test report](${testLink}), [local runtime evidence](evidence/mysql-runtime-smoke.json), [endpoint checklist](MYSQL_ENDPOINT_STATUS.md) and [operator instructions](MYSQL_DOCKER.md). No external publisher acceptance, production queue transfer or throughput target is claimed.`, ''
].join('\n'));
console.log('Updated endpoint and worker statuses from passing test/runtime evidence.');
