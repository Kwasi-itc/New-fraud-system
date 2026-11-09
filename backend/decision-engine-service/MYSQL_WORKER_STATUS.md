# Decision engine MySQL worker status

Updated 2026-11-07T08:36:25.448Z. The running local worker enables **all seven handlers**: scheduled evaluation, async evaluation, callbacks, workflow, screening, scoring and outbox, plus **seven maintenance loops and the recurring scheduler**. A private provider process computes watchlist matches, projects screening results and applies scoring results. Worker readiness alone does not establish execution acceptance.

All active registered tenants share these fifteen worker loops. Tenant discovery uses primary-key pages and indexed due-queue checks; idle claims create no owner transactions. Maintenance also checks indexed expired-lease and terminal-retention existence before constructing an owner. These checks use the shared retention policy and grant no authority; actual recovery/deletion rechecks tenant activation, lease identity and retention in its own transaction. Work that becomes due after discovery is picked up in a later bounded sweep. Each claim scans at most eight pages and continues across empty pages before sleeping, so a ready tenant's backlog can drain continuously. Each claimed task retains its exact tenant-scoped owner/lease callbacks. Adding a tenant creates no SQL pool or permanently running set of tenant goroutines. Authority is rechecked inside owner transactions, including after registry discovery.

## Live local acceptance

The [live report](evidence/mysql-endpoint-audit/2026-11-07T08-33-52.932Z/report.json) passes 25/25 suites. Its worker checks passed against the actual Docker API/worker accounts and shared MySQL database:

- async and scheduled execution, stored record lookup and durable decision history: **passed**
- pending cancellation and queued work across process stop/start: **passed**
- persistence lock timeout retries, exhaustion, rollback, generation-fenced redrive and recovery: **passed**
- workflow case/link/history, event inbox, watchlist matches/projection, scoring result and outbound receipts: **passed**
- callback failure exhaustion, generation-fenced redrive, cancellation, receiver/worker restart and unchanged evaluation results: **passed**

The [synthetic fraud report](evidence/mysql-fraud/2026-11-07T08-32-05.298Z/report.json) passes **7/7 suites**, verifies **256/256 transactions** across five real published rules, and retains 610 HTTP requests. Sixteen transactions have hand-calculated boundary expectations. Every score, outcome, matching rule and persisted explanation agrees with an independent business oracle. Outcomes: approve 81, review 71, block_and_review 71, decline 33. The same transaction ID evaluates under independent tenant policies; cross-tenant resource reads are denied. Both tenants exercise async/scheduled evaluation, callback receipts, case workflow delivery and real review identities. A third tenant created after startup publishes/evaluates and completes async callback work without restarting the API, worker or provider. This is synthetic functional acceptance of the decision API; risk_score is a supplied input feature.

The audit stops the decision worker, admits pending work, cancels selected executions, restarts it, and verifies durable completion of the remaining work. It then blocks the FK check for **only its fresh committed iteration** to force persistence failure. Both evaluation queues exhaust bounded transport retries without partial decisions; after lock release, generation-conditional redrive completes and stale redrive requests return 409. Cancelled executions reject redrive. Invalid typed input is rejected at admission.

This live restart check covers queued work across stop/start. In-flight lease expiry, late-worker fencing, unknown commit outcomes, and owner-process crash reconciliation are tested in disposable fixtures below; the live audit does not claim a forced in-flight crash test.

## Isolated worker and owner contracts

The [isolated regression report](evidence/mysql-integration/2026-11-07T07-30-55Z/report.json) records **580 top-level tests / 1561 passing test and subtest results**, 0 current failures, 0 race warnings, and `go vet` exit 0. 5 explicitly reported tests were skipped; they are not counted as passes. The independent full-module inventory accounts for 585 top-level tests with 0 missing outcomes. Raw failures and any focused repairs remain in the evidence. Counts use each complete top-level test's latest result without double-counting subtests.

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

- `TestConfiguredMMDBCanResolvePublicIP`: mmdb_test.go:25: GEOIP_TEST_MMDB_PATH is not set
- `TestCaseWorkflowCrashSubprocess`: case_workflow_receipt_integration_test.go:765: subprocess helper
- `TestManualEvaluationLoadAcceptance`: evaluation_load_integration_test.go:72: explicit evaluation profile required
- `TestManualHistoryLoadAcceptance`: history_load_integration_test.go:54: explicit agreed history profile required
- `TestMySQLLogicalRestoreRetainsDecisionsJobsAndCaseReceipts`: restore_rehearsal_integration_test.go:27: explicit disposable restore rehearsal required

Manual load profiles, logical restore rehearsal, and the externally supplied GeoIP database are separate acceptance resources. Live delivery acceptance covers local development providers and durable inboxes. It establishes no production capacity or external-provider delivery claim.

## Reproduce

```powershell
docker compose -p decision-mysql-acceptance -f compose.mysql.test.yaml -f compose.differential.test.yaml up -d --wait mysql postgres
docker compose -p decision-mysql-acceptance -f compose.mysql.test.yaml -f compose.differential.test.yaml run --rm test sh scripts/test-mysql.sh
node scripts/summarize-mysql-tests.mjs evidence/mysql-integration/<printed-timestamp>
docker compose -p decision-mysql-acceptance -f compose.mysql.test.yaml -f compose.differential.test.yaml down
```

The runner tests the complete decision Go module with race detection, uses generated fixture databases, records JSON test events and exit codes, and never points at the persistent application database. The differential overlay supplies disposable PostgreSQL for source/parity checks. Keep both databases private. For focused repairs or a remaining-test shard, pass the original directory followed by subsequent directories to the summarizer. It verifies every top-level test against an independent full-module inventory, selects the latest complete outcome per test, retains historical failures, and does not double-count subtests. The default package timeout is now 45 minutes.

Local policy: every active registered tenant, one shared handler per kind, eight worker SQL connections, eight API connections and four private provider connections. Fifteen worker loops fit the sixteen-slot bound. Registry discovery reads at most twenty rows per page; no tenant graph or model cache grows with the registry. Leases last one minute, heartbeat ten seconds, handler timeout thirty seconds and transport/domain attempt limits three. HTTP dispatch has a five-second timeout, rejects redirects and uses fixed private destinations; payloads are capped at four MiB. Maintenance visits at most twenty bounded pages per stream each five-second cycle. Scheduler/provider tenant walks are paced at 100 ms. Other services retain separate pools against the same application database. Setup grants published-record reads and provisions distinct reviewer/inbox identities for current active tenants; rerun setup for a new tenant's human review or newly published stored-record tables.
