import assert from 'node:assert/strict';
import {readFileSync, writeFileSync} from 'node:fs';
import {dirname, relative, resolve} from 'node:path';
import {fileURLToPath} from 'node:url';

const service = resolve(dirname(fileURLToPath(import.meta.url)), '..');
if (!process.argv[2] || !process.argv[3] || !process.argv[4]) throw new Error('Usage: node scripts/update-mysql-status.mjs <audit directory> <regression directory> <fraud audit directory>');
const auditDirectory = resolve(process.argv[2]);
const regressionDirectory = resolve(process.argv[3]);
const audit = JSON.parse(readFileSync(resolve(auditDirectory, 'report.json'), 'utf8'));
const regression = JSON.parse(readFileSync(resolve(regressionDirectory, 'report.json'), 'utf8'));
const fraudDirectory=resolve(process.argv[4]);
const fraud=JSON.parse(readFileSync(resolve(fraudDirectory,'report.json'),'utf8'));
const auditedRuntime=JSON.parse(readFileSync(resolve(auditDirectory,'runtime.json'),'utf8'));
assert.ok(fraud.passed && fraud.runtimeUnchanged,'Synthetic multi-tenant fraud acceptance must pass');
assert.equal(fraud.runtimeImage,auditedRuntime['fraud-mysql-dev-decision-engine-service-1'].image,'Endpoint and synthetic audits must use the same final image');
assert.ok(audit.profilePassed && regression.regressionPassed, 'Status publication requires passing profile and regression evidence');
assert.ok(audit.unroutedEvidence?.all39Passed,'All formerly unrouted endpoints must have positive acceptance evidence');
assert.ok(audit.disabledEvidence?.profilePassed && audit.allDocumentedOperationsCovered && audit.mountedSuccessfulOperations === audit.mountedOperations, 'All formerly disabled and mounted operations must have positive acceptance evidence');
const link = path => relative(service, path).replaceAll('\\', '/');
const liveReport = link(resolve(auditDirectory, 'report.json'));
const testsReport = link(resolve(regressionDirectory, 'report.json'));
const fraudReport=link(resolve(fraudDirectory,'report.json'));
const fraudEvidence=`The [synthetic fraud report](${fraudReport}) passes **${fraud.suites.filter(s=>s.passed).length}/${fraud.suites.length} suites**, verifies **${fraud.verifiedTransactions}/${fraud.syntheticTransactions} transactions** across five real published rules, and retains ${fraud.requestCount} HTTP requests. Sixteen transactions have hand-calculated boundary expectations. Every score, outcome, matching rule and persisted explanation agrees with an independent business oracle. Outcomes: ${Object.entries(fraud.outcomes).map(([name,count])=>name+' '+count).join(', ')}. The same transaction ID evaluates under independent tenant policies; cross-tenant resource reads are denied. Both tenants exercise async/scheduled evaluation, callback receipts, case workflow delivery and real review identities. A third tenant created after startup publishes/evaluates and completes async callback work without restarting the API, worker or provider. This is synthetic functional acceptance of the decision API; risk_score is a supplied input feature.`;
const label = {success_verified: '[x] Asserted success', not_mounted: '[ ] Not mounted', capability_disabled: '[ ] Disabled capability', identity_unavailable: '[ ] Session unavailable', coverage_gap: '[ ] Coverage gap'};
const endpointRows = audit.matrix.map(row => `| ${row.method} ${row.path} | ${label[row.status]} | ${row.successfulSuites.length ? row.successfulSuites.join(', ') : row.reason} |`).join('\n');
const extraRows = audit.extraMatrix.map(row => `| ${row.method} ${row.path} | ${label[row.status]} |`).join('\n');
const diagnosticRepair = regression.attempts.some(attempt => attempt.testsExit !== 0 && readFileSync(resolve(attempt.directory, 'raw.jsonl'), 'utf8').includes('SIGQUIT: quit')) ? ' The first module run was stopped for a stack dump after an existing load-test logger bridge stalled. The fixture helper was repaired, and a passing shard reran both load smoke profiles together with wallet parity and all unfinished tests. The diagnostic exit and stack dump remain in the original raw evidence.' : '';
const commonRegression = `The [isolated regression report](${testsReport}) records **${regression.topLevelPassed} top-level tests / ${regression.testAndSubtestPasses} passing test and subtest results**, ${regression.failed.length} current failures, ${regression.raceWarnings} race warnings, and \`go vet\` exit ${regression.vetExit}. ${regression.skipped.length} explicitly reported tests were skipped; they are not counted as passes. The independent full-module inventory accounts for ${regression.expectedTopLevelTests} top-level tests with ${regression.missingTests.length} missing outcomes. Raw failures and any focused repairs remain in the evidence. Counts use each complete top-level test's latest result without double-counting subtests.`;
const regressionEvidence=commonRegression+diagnosticRepair;
const skipText = regression.skipped.map(item => `- \`${item.test}\`: ${item.output.split('\n').find(line => !line.includes('=== RUN') && !line.includes('--- SKIP'))?.trim() ?? 'See raw evidence'}`).join('\n');
writeFileSync(resolve(service, 'MYSQL_ENDPOINT_STATUS.md'), `# Decision engine MySQL endpoint status

Updated ${audit.reportedAt}. This is the **local development profile** at port 8082.

**Multi-tenant mode: all active, registered tenants.** Tenant IDs in request URLs select their own SQL scope. Registration/activation is rechecked on each request and owner transaction; resources remain tenant-isolated. The retained default tenant is a test fixture, not an allowlist. Workers and private providers discover tenants through bounded registry pages with shared pools and fixed slots.

**The 39 formerly unrouted operations are implemented, mounted and positively verified.** This includes synchronous evaluation and ingestion triggers, draft test-run comparison and analytics, structured workflow rules/conditions/actions, screening configuration and status, scoring status/retry, authoritative screening callbacks, investigator review and expression assistance. The focused audit also verifies rejection of unregistered tenant IDs for tenant-scoped operations.

The shared OpenAPI document contains **${audit.documentedOperations} operations**, all positively verified. There are **${audit.totals.not_mounted} unmounted**, **${audit.totals.capability_disabled} disabled**, and **${audit.totals.coverage_gap} coverage gaps**. The focused delivery audit verifies all **${audit.disabledEvidence.verified}/${audit.disabledEvidence.total} formerly disabled routes**, including callback retry/cancel extensions. **All documented operations covered: ${audit.allDocumentedOperationsCovered}.**

The three AI-named expression routes use the user-selected **local expression assistant**: a bounded compiler and structural explainer with \`backend=local-expression-assistant\` and \`uses_ai=false\`. They validate expressions against the actual published tenant model. They provide no language-model or free-form natural-language generation.

The [live report](${liveReport}) passed **${audit.suites.filter(s => s.passed).length}/${audit.suites.length} suites across ${audit.requestCount} HTTP requests**. Only successful responses with assertions in passing suites count as positive endpoint coverage. Expected 403/404/503 responses do not turn an unavailable endpoint green. Runtime container/image identities and HTTP traces are retained beside the report.

The running image's \`/app/mysql-api --routes\` diagnostic inventories **${audit.mountedOperations} actual mounted routes**, including documentation and runtime extensions missing from the shared spec. **All ${audit.mountedSuccessfulOperations} mounted routes have asserted success**.

Every documented path is probed with fresh nonexistent resource IDs. Mounted tenant routes also receive an unregistered tenant ID, which must return 403. The synthetic audit separately verifies resource isolation between active tenants. The positive suites exercise authoring, validation failures, immutable commit, publication, copy, test runs, custom lists/CSV, helper data, execution/history, replay, cancellation/redrive and schedule preconditions. Documentation and capability routes are additional checks.

${regressionEvidence}

${fraudEvidence}

## Operation matrix

A checked row means successful **live local** coverage. Delivery acceptance uses actual persisted provider receipts, owner results and case links, with local provider limitations described below.

| Operation | Local status | Live evidence or limitation |
| --- | --- | --- |
${endpointRows}

## Mounted extensions outside the shared specification

| Operation | Live local status |
| --- | --- |
${extraRows}

## Reproduce

From this service directory with the stack running:

\`\`\`powershell
node scripts/mysql-endpoint-audit.mjs
\`\`\`

The audit retains uniquely named fixtures. It stops/restarts the decision worker and private provider, restores both, and locks only its newly created committed iteration for persistence failure checks. The local callback receiver temporarily applies a rejection policy to exercise actual durable negative receipts, automatic retry exhaustion, cancellation and generation-fenced redrive. A future recurring schedule is created and disabled; calendar ticking is covered in private integration tests.

## Local delivery behavior and limits

Workflow and scoring configuration routes and callback controls are enabled. Case actions use the case owner's real receipt service. Screening uses explicit exact-name local watchlists and publishes real owner matches/results. Additional scoring uses \`local:decision-score-v1\`, which returns the immutable decision score and a deterministic risk band. Callback, workflow event and outbox acceptance means a durable local inbox receipt; it does not mean delivery to an external network. Arbitrary callback/event URLs, external scoring rulesets and unsupported workflow actions receive validation errors or durable rejected receipts. See [MYSQL_DOCKER.md](MYSQL_DOCKER.md) for request examples.

Screening callbacks reconcile authoritative screening-owner SQL rather than trusting snapshots. Investigator review checks a real seeded session and actual case membership. Machine decisions remain unchanged. The local stack does not establish production provider integration, production throughput, or transfer existing PostgreSQL records.
`);

const liveWorkers = audit.workers.map(item => `- ${item.check}: **${item.passed ? 'passed' : 'failed'}**`).join('\n');
writeFileSync(resolve(service, 'MYSQL_WORKER_STATUS.md'), `# Decision engine MySQL worker status

Updated ${audit.reportedAt}. The running local worker enables **all seven handlers**: scheduled evaluation, async evaluation, callbacks, workflow, screening, scoring and outbox, plus **seven maintenance loops and the recurring scheduler**. A private provider process computes watchlist matches, projects screening results and applies scoring results. Worker readiness alone does not establish execution acceptance.

All active registered tenants share these fifteen worker loops. Tenant discovery uses primary-key pages and indexed due-queue checks; idle claims create no owner transactions. Maintenance also checks indexed expired-lease and terminal-retention existence before constructing an owner. These checks use the shared retention policy and grant no authority; actual recovery/deletion rechecks tenant activation, lease identity and retention in its own transaction. Work that becomes due after discovery is picked up in a later bounded sweep. Each claim scans at most eight pages and continues across empty pages before sleeping, so a ready tenant's backlog can drain continuously. Each claimed task retains its exact tenant-scoped owner/lease callbacks. Adding a tenant creates no SQL pool or permanently running set of tenant goroutines. Authority is rechecked inside owner transactions, including after registry discovery.

## Live local acceptance

The [live report](${liveReport}) passes ${audit.suites.filter(s => s.passed).length}/${audit.suites.length} suites. Its worker checks passed against the actual Docker API/worker accounts and shared MySQL database:

${liveWorkers}

${fraudEvidence}

The audit stops the decision worker, admits pending work, cancels selected executions, restarts it, and verifies durable completion of the remaining work. It then blocks the FK check for **only its fresh committed iteration** to force persistence failure. Both evaluation queues exhaust bounded transport retries without partial decisions; after lock release, generation-conditional redrive completes and stale redrive requests return 409. Cancelled executions reject redrive. Invalid typed input is rejected at admission.

This live restart check covers queued work across stop/start. In-flight lease expiry, late-worker fencing, unknown commit outcomes, and owner-process crash reconciliation are tested in disposable fixtures below; the live audit does not claim a forced in-flight crash test.

## Isolated worker and owner contracts

${regressionEvidence}

| Behavior | Passing test coverage in the regression |
| --- | --- |
| Scheduled batch claim, atomic output, lost completion acknowledgement | TestMySQLScheduledWorkerActualClaimWholeBatchAndReconciliation |
| Scheduled rollback and transport retry | TestMySQLScheduledWorkerPersistenceRollbackAndTransportRetry |
| Invalid input and authority revocation | TestMySQLScheduledWorkerInvalidInputIsTerminalAndRevocationFailsClosed; TestMySQLEvaluationWorkerScopeAndCancellation |
| Async output and required callback atomicity | TestMySQLAsyncWorkerAllOutputsAndRequiredCallbackAtomic; TestMySQLAsyncWorkerRequiredCallbackFailureRollsBackAllOutputs |
| Tenant-scoped claim and heartbeat | TestMySQLExecutionTransportClaimHeartbeatAndOwnerAck |
| Lease expiry, retry exhaustion, concurrent rescue and stale-worker fencing | TestMySQLExecutionRescueRetryExhaustionAndCallbackAtomicity; TestMySQLExecutionConcurrentRescueAndLateWorkerAreFenced |
| Cancellation, generations, callback-only recovery | TestMySQLExecutionCancellationRequiredCallbackAndStaleWorkers; TestMySQLCallbackOnlyRedrivePreservesCompletedResult |
| Callback send uncertainty and crash/rescue reconciliation | TestMySQLCallbackDispatchLostCommitAcknowledgementReconcilesWithoutResend; TestMySQLCallbackDispatchCrashRescueReconcilesOriginalIntent |
| Scheduler bounded pages, concurrency and atomic occurrence recording | TestMySQLSchedulerBoundedPagesAtomicityAndConcurrentTicks |
| Civil-time/DST calendar and tenant isolation | TestMySQLExecutionCalendarAtomicityCivilTimesAndTenantIsolation |
| Shutdown, sibling cancellation and readiness withdrawal | TestApplicationWorkerFailureCancelsAndJoinsSiblings; TestApplicationShutdownIsBoundedAndRejectsAdmissionFirst; TestUnexpectedSuccessfulWorkerExitIsFailure |
| Restricted API/worker grants and dispatch ownership | TestMySQLReleaseRuntimeGrantBoundaryAndWorkerDispatch; TestMySQLDispatchRestrictedWorkerCannotBypassIntentOwnership |
| Exhausted unresolved sends, concurrent reconciliation and stale-token fencing | TestMySQLExhaustedDispatchReconciliationPreservesIdentityAndFencesWorkers; TestMySQLReceiptAwareMaintenanceDoesNotExhaustUnreconciledSend |
| Provider scoring replay, stale generations and manual status/reference overrides | TestMySQLProviderScoringResultIsIdempotentAndGenerationFenced |
| Durable local receiver deduplication, payload binding, lost response and rollback | TestLocalDeliveryConcurrentReceiptsPayloadBindingAndRevocation; TestLocalDeliveryHTTPUnknownResponseReconcilesAndRejectsRedirects; TestLocalDeliveryLateReceiptFailureHasNoAcceptance |
| Tenant discovery, activation and revocation with bounded native pagination | TestLocalTenantRegistryPaginationActivationAndRevocation |
| Due-work discovery skips idle owner transactions and uses native queue indexes | TestLocalTenantWorkDiscoveryUsesIndexedDueQueues |
| Same resource UUID in two tenants and receiver revocation checks | TestLocalMultiTenantProviderSeparatesSameResourceAndRechecksActivation |
| Distinct opaque review credentials, fixed worker-slot budgets and fresh model publications | TestLocalReviewCredentialsAreTenantScopedAndPreserveLegacyFixture; TestLocalWorkerLoopsRespectFixedSlotBudget; TestLocalDynamicModelsObservePublicationChanges |

The JSON report lists the actual passing worker tests. Live local delivery uses restricted Docker accounts and actual owner contracts. A transport error preserves send uncertainty; expired exhausted sends reconcile their original intent under a new fenced token without changing the domain attempt or generation. Maintenance defers unresolved sends so unrelated work continues. The case-owner HTTP crash test runs its helper under its parent test; the helper is intentionally skipped when run alone.

## Skips and limits

${skipText}

Manual load profiles, logical restore rehearsal, and the externally supplied GeoIP database are separate acceptance resources. Live delivery acceptance covers local development providers and durable inboxes. It establishes no production capacity or external-provider delivery claim.

## Reproduce

\`\`\`powershell
docker compose -p decision-mysql-acceptance -f compose.mysql.test.yaml -f compose.differential.test.yaml up -d --wait mysql postgres
docker compose -p decision-mysql-acceptance -f compose.mysql.test.yaml -f compose.differential.test.yaml run --rm test sh scripts/test-mysql.sh
node scripts/summarize-mysql-tests.mjs evidence/mysql-integration/<printed-timestamp>
docker compose -p decision-mysql-acceptance -f compose.mysql.test.yaml -f compose.differential.test.yaml down
\`\`\`

The runner tests the complete decision Go module with race detection, uses generated fixture databases, records JSON test events and exit codes, and never points at the persistent application database. The differential overlay supplies disposable PostgreSQL for source/parity checks. Keep both databases private. For focused repairs or a remaining-test shard, pass the original directory followed by subsequent directories to the summarizer. It verifies every top-level test against an independent full-module inventory, selects the latest complete outcome per test, retains historical failures, and does not double-count subtests. The default package timeout is now 45 minutes.

Local policy: every active registered tenant, one shared handler per kind, eight worker SQL connections, eight API connections and four private provider connections. Fifteen worker loops fit the sixteen-slot bound. Registry discovery reads at most twenty rows per page; no tenant graph or model cache grows with the registry. Leases last one minute, heartbeat ten seconds, handler timeout thirty seconds and transport/domain attempt limits three. HTTP dispatch has a five-second timeout, rejects redirects and uses fixed private destinations; payloads are capped at four MiB. Maintenance visits at most twenty bounded pages per stream each five-second cycle. Scheduler/provider tenant walks are paced at 100 ms. Other services retain separate pools against the same application database. Setup grants published-record reads and provisions distinct reviewer/inbox identities for current active tenants; rerun setup for a new tenant's human review or newly published stored-record tables.
`);
console.log('Updated MYSQL_ENDPOINT_STATUS.md and MYSQL_WORKER_STATUS.md');
