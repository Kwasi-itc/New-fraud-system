# Decision engine MySQL endpoint status

Updated 2026-11-07T08:36:25.448Z. This is the **local development profile** at port 8082.

**Multi-tenant mode: all active, registered tenants.** Tenant IDs in request URLs select their own SQL scope. Registration/activation is rechecked on each request and owner transaction; resources remain tenant-isolated. The retained default tenant is a test fixture, not an allowlist. Workers and private providers discover tenants through bounded registry pages with shared pools and fixed slots.

**The 39 formerly unrouted operations are implemented, mounted and positively verified.** This includes synchronous evaluation and ingestion triggers, draft test-run comparison and analytics, structured workflow rules/conditions/actions, screening configuration and status, scoring status/retry, authoritative screening callbacks, investigator review and expression assistance. The focused audit also verifies rejection of unregistered tenant IDs for tenant-scoped operations.

The shared OpenAPI document contains **120 operations**, all positively verified. There are **0 unmounted**, **0 disabled**, and **0 coverage gaps**. The focused delivery audit verifies all **12/12 formerly disabled routes**, including callback retry/cancel extensions. **All documented operations covered: true.**

The three AI-named expression routes use the user-selected **local expression assistant**: a bounded compiler and structural explainer with `backend=local-expression-assistant` and `uses_ai=false`. They validate expressions against the actual published tenant model. They provide no language-model or free-form natural-language generation.

The [live report](evidence/mysql-endpoint-audit/2026-11-07T08-33-52.932Z/report.json) passed **25/25 suites across 682 HTTP requests**. Only successful responses with assertions in passing suites count as positive endpoint coverage. Expected 403/404/503 responses do not turn an unavailable endpoint green. Runtime container/image identities and HTTP traces are retained beside the report.

The running image's `/app/mysql-api --routes` diagnostic inventories **130 actual mounted routes**, including documentation and runtime extensions missing from the shared spec. **All 130 mounted routes have asserted success**.

Every documented path is probed with fresh nonexistent resource IDs. Mounted tenant routes also receive an unregistered tenant ID, which must return 403. The synthetic audit separately verifies resource isolation between active tenants. The positive suites exercise authoring, validation failures, immutable commit, publication, copy, test runs, custom lists/CSV, helper data, execution/history, replay, cancellation/redrive and schedule preconditions. Documentation and capability routes are additional checks.

The [isolated regression report](evidence/mysql-integration/2026-11-07T07-30-55Z/report.json) records **580 top-level tests / 1561 passing test and subtest results**, 0 current failures, 0 race warnings, and `go vet` exit 0. 5 explicitly reported tests were skipped; they are not counted as passes. The independent full-module inventory accounts for 585 top-level tests with 0 missing outcomes. Raw failures and any focused repairs remain in the evidence. Counts use each complete top-level test's latest result without double-counting subtests.

The [synthetic fraud report](evidence/mysql-fraud/2026-11-07T08-32-05.298Z/report.json) passes **7/7 suites**, verifies **256/256 transactions** across five real published rules, and retains 610 HTTP requests. Sixteen transactions have hand-calculated boundary expectations. Every score, outcome, matching rule and persisted explanation agrees with an independent business oracle. Outcomes: approve 81, review 71, block_and_review 71, decline 33. The same transaction ID evaluates under independent tenant policies; cross-tenant resource reads are denied. Both tenants exercise async/scheduled evaluation, callback receipts, case workflow delivery and real review identities. A third tenant created after startup publishes/evaluates and completes async callback work without restarting the API, worker or provider. This is synthetic functional acceptance of the decision API; risk_score is a supplied input feature.

## Operation matrix

A checked row means successful **live local** coverage. Delivery acceptance uses actual persisted provider receipts, owner results and case links, with local provider limitations described below.

| Operation | Local status | Live evidence or limitation |
| --- | --- | --- |
| GET /v1/investigator/tenants/{tenantId}/cases/{caseId}/review | [x] Asserted success | unrouted/investigator-review-replay-version-and-case-access, delivery/real-case-screening-scoring-and-outbox-delivery |
| POST /v1/investigator/tenants/{tenantId}/cases/{caseId}/review | [x] Asserted success | unrouted/investigator-review-replay-version-and-case-access |
| GET /healthz | [x] Asserted success | health-docs-catalog-and-origin |
| GET /readyz | [x] Asserted success | health-docs-catalog-and-origin |
| GET /v1/service-info | [x] Asserted success | unrouted/fixture-and-new-direct-routes |
| GET /v1/rule-functions | [x] Asserted success | health-docs-catalog-and-origin |
| GET /v1/tenants/{tenantId}/scenarios | [x] Asserted success | scenario-authoring-and-validation |
| POST /v1/tenants/{tenantId}/scenarios | [x] Asserted success | scenario-authoring-and-validation, unrouted/fixture-and-new-direct-routes, delivery/enabled-local-provider-profile-and-fixture |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId} | [x] Asserted success | scenario-authoring-and-validation |
| PUT /v1/tenants/{tenantId}/scenarios/{scenarioId} | [x] Asserted success | scenario-authoring-and-validation |
| DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId} | [x] Asserted success | scenario-authoring-and-validation |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/copy | [x] Asserted success | publication-clone-and-test-run-administration |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/rules/latest | [x] Asserted success | publication-clone-and-test-run-administration |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/ast-ai-description | [x] Asserted success | unrouted/local-expression-assistance-and-model-validation |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/generate-ast | [x] Asserted success | unrouted/local-expression-assistance-and-model-validation |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/editor-identifiers | [x] Asserted success | unrouted/fixture-and-new-direct-routes |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations | [x] Asserted success | scenario-authoring-and-validation |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations | [x] Asserted success | scenario-authoring-and-validation, unrouted/fixture-and-new-direct-routes, delivery/enabled-local-provider-profile-and-fixture |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/metadata | [x] Asserted success | scenario-authoring-and-validation |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId} | [x] Asserted success | scenario-authoring-and-validation |
| PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId} | [x] Asserted success | scenario-authoring-and-validation, unrouted/fixture-and-new-direct-routes, delivery/enabled-local-provider-profile-and-fixture |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/draft | [x] Asserted success | publication-clone-and-test-run-administration, unrouted/test-run-draft-comparison-and-analytics |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/commit | [x] Asserted success | scenario-authoring-and-validation, publication-clone-and-test-run-administration, unrouted/fixture-and-new-direct-routes, delivery/enabled-local-provider-profile-and-fixture |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/deactivate | [x] Asserted success | local-provider-validation-and-publication-deactivation |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/publications | [x] Asserted success | publication-clone-and-test-run-administration |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/publications | [x] Asserted success | publication-clone-and-test-run-administration, local-provider-validation-and-publication-deactivation, unrouted/fixture-and-new-direct-routes, delivery/enabled-local-provider-profile-and-fixture |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/publications/preparation | [x] Asserted success | publication-clone-and-test-run-administration |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/publications/preparation | [x] Asserted success | publication-clone-and-test-run-administration |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules | [x] Asserted success | scenario-authoring-and-validation |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules | [x] Asserted success | scenario-authoring-and-validation, unrouted/fixture-and-new-direct-routes, delivery/enabled-local-provider-profile-and-fixture |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/rule-groups | [x] Asserted success | scenario-authoring-and-validation |
| PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules/{ruleId} | [x] Asserted success | scenario-authoring-and-validation |
| DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules/{ruleId} | [x] Asserted success | scenario-authoring-and-validation |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules/{ruleId}/ai-description | [x] Asserted success | unrouted/local-expression-assistance-and-model-validation |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/validate | [x] Asserted success | scenario-authoring-and-validation |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/evaluate | [x] Asserted success | unrouted/fixture-and-new-direct-routes, delivery/real-case-screening-scoring-and-outbox-delivery |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/decisions | [x] Asserted success | async-scheduled-execution-history-and-replay, worker-cancellation-redrive-and-process-restart, unrouted/test-run-draft-comparison-and-analytics |
| GET /v1/tenants/{tenantId}/decisions | [x] Asserted success | async-scheduled-execution-history-and-replay |
| POST /v1/tenants/{tenantId}/decisions | [x] Asserted success | unrouted/fixture-and-new-direct-routes |
| GET /v1/tenants/{tenantId}/decisions/count | [x] Asserted success | async-scheduled-execution-history-and-replay |
| POST /v1/tenants/{tenantId}/decisions/all | [x] Asserted success | unrouted/fixture-and-new-direct-routes |
| GET /v1/tenants/{tenantId}/decisions/{decisionId} | [x] Asserted success | async-scheduled-execution-history-and-replay, unrouted/investigator-review-replay-version-and-case-access, delivery/real-case-screening-scoring-and-outbox-delivery |
| POST /v1/tenants/{tenantId}/ingestion-events/record-ingested | [x] Asserted success | unrouted/fixture-and-new-direct-routes |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/test-runs | [x] Asserted success | publication-clone-and-test-run-administration |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/test-runs | [x] Asserted success | publication-clone-and-test-run-administration, unrouted/test-run-draft-comparison-and-analytics |
| GET /v1/tenants/{tenantId}/test-runs/{testRunId} | [x] Asserted success | publication-clone-and-test-run-administration |
| POST /v1/tenants/{tenantId}/test-runs/{testRunId}/cancel | [x] Asserted success | publication-clone-and-test-run-administration, unrouted/test-run-draft-comparison-and-analytics |
| GET /v1/tenants/{tenantId}/test-runs/{testRunId}/decision-data-by-score | [x] Asserted success | unrouted/test-run-draft-comparison-and-analytics |
| GET /v1/tenants/{tenantId}/test-runs/{testRunId}/data-by-rule-execution | [x] Asserted success | unrouted/test-run-draft-comparison-and-analytics |
| POST /v1/tenants/{tenantId}/test-runs/{testRunId}/evaluate | [x] Asserted success | unrouted/test-run-draft-comparison-and-analytics |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflows | [x] Asserted success | async-scheduled-execution-history-and-replay, delivery/legacy-workflow-crud-reorder-and-isolation |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflows | [x] Asserted success | delivery/legacy-workflow-crud-reorder-and-isolation, delivery/real-case-screening-scoring-and-outbox-delivery |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflows/reorder | [x] Asserted success | delivery/legacy-workflow-crud-reorder-and-isolation |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflows/{workflowId} | [x] Asserted success | delivery/legacy-workflow-crud-reorder-and-isolation |
| PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflows/{workflowId} | [x] Asserted success | delivery/legacy-workflow-crud-reorder-and-isolation, delivery/real-case-screening-scoring-and-outbox-delivery |
| DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflows/{workflowId} | [x] Asserted success | delivery/legacy-workflow-crud-reorder-and-isolation |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules | [x] Asserted success | unrouted/workflow-rule-condition-action-lifecycle |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules | [x] Asserted success | unrouted/workflow-rule-condition-action-lifecycle |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules/reorder | [x] Asserted success | unrouted/workflow-rule-condition-action-lifecycle |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules/{ruleId} | [x] Asserted success | unrouted/workflow-rule-condition-action-lifecycle |
| PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules/{ruleId} | [x] Asserted success | unrouted/workflow-rule-condition-action-lifecycle |
| DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules/{ruleId} | [x] Asserted success | unrouted/workflow-rule-condition-action-lifecycle |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules/{ruleId}/conditions | [x] Asserted success | unrouted/workflow-rule-condition-action-lifecycle |
| PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules/{ruleId}/conditions/{conditionId} | [x] Asserted success | unrouted/workflow-rule-condition-action-lifecycle |
| DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules/{ruleId}/conditions/{conditionId} | [x] Asserted success | unrouted/workflow-rule-condition-action-lifecycle |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules/{ruleId}/actions | [x] Asserted success | unrouted/workflow-rule-condition-action-lifecycle |
| PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules/{ruleId}/actions/{actionId} | [x] Asserted success | unrouted/workflow-rule-condition-action-lifecycle |
| DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules/{ruleId}/actions/{actionId} | [x] Asserted success | unrouted/workflow-rule-condition-action-lifecycle |
| GET /v1/tenants/{tenantId}/decisions/{decisionId}/workflow-executions | [x] Asserted success | async-scheduled-execution-history-and-replay, delivery/real-case-screening-scoring-and-outbox-delivery |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/screening-configs | [x] Asserted success | unrouted/screening-config-lifecycle-and-list-entry-alias |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/screening-configs | [x] Asserted success | unrouted/screening-config-lifecycle-and-list-entry-alias, delivery/real-case-screening-scoring-and-outbox-delivery |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/screening-configs/{configId} | [x] Asserted success | unrouted/screening-config-lifecycle-and-list-entry-alias |
| PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/screening-configs/{configId} | [x] Asserted success | unrouted/screening-config-lifecycle-and-list-entry-alias, delivery/real-case-screening-scoring-and-outbox-delivery |
| DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/screening-configs/{configId} | [x] Asserted success | unrouted/screening-config-lifecycle-and-list-entry-alias |
| GET /v1/tenants/{tenantId}/decisions/{decisionId}/screening-executions | [x] Asserted success | unrouted/screening-scoring-status-retry-and-authoritative-callback, delivery/real-case-screening-scoring-and-outbox-delivery |
| GET /v1/tenants/{tenantId}/screening-executions/{executionId} | [x] Asserted success | unrouted/screening-scoring-status-retry-and-authoritative-callback |
| POST /v1/tenants/{tenantId}/screening-executions/{executionId}/status | [x] Asserted success | unrouted/screening-scoring-status-retry-and-authoritative-callback |
| POST /v1/tenants/{tenantId}/screening-executions/{executionId}/retry | [x] Asserted success | unrouted/screening-scoring-status-retry-and-authoritative-callback |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/scoring-configs | [x] Asserted success | local-provider-validation-and-publication-deactivation, delivery/scoring-config-crud-and-validation |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/scoring-configs | [x] Asserted success | delivery/scoring-config-crud-and-validation, delivery/real-case-screening-scoring-and-outbox-delivery |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/scoring-configs/{configId} | [x] Asserted success | delivery/scoring-config-crud-and-validation |
| PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/scoring-configs/{configId} | [x] Asserted success | delivery/scoring-config-crud-and-validation, delivery/real-case-screening-scoring-and-outbox-delivery |
| DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/scoring-configs/{configId} | [x] Asserted success | delivery/scoring-config-crud-and-validation |
| GET /v1/tenants/{tenantId}/decisions/{decisionId}/scoring-requests | [x] Asserted success | async-scheduled-execution-history-and-replay, delivery/real-case-screening-scoring-and-outbox-delivery |
| GET /v1/tenants/{tenantId}/scoring-requests/{requestId} | [x] Asserted success | unrouted/screening-scoring-status-retry-and-authoritative-callback |
| POST /v1/tenants/{tenantId}/scoring-requests/{requestId}/status | [x] Asserted success | unrouted/screening-scoring-status-retry-and-authoritative-callback |
| POST /v1/tenants/{tenantId}/scoring-requests/{requestId}/retry | [x] Asserted success | unrouted/screening-scoring-status-retry-and-authoritative-callback |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/rule-snoozes | [x] Asserted success | async-scheduled-execution-history-and-replay |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/rule-snoozes | [x] Asserted success | async-scheduled-execution-history-and-replay |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/scheduled-executions | [x] Asserted success | async-scheduled-execution-history-and-replay |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/scheduled-executions | [x] Asserted success | async-scheduled-execution-history-and-replay, worker-cancellation-redrive-and-process-restart |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/scheduled-executions/{executionId} | [x] Asserted success | async-scheduled-execution-history-and-replay, worker-cancellation-redrive-and-process-restart |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/scheduled-executions/{executionId}/retry | [x] Asserted success | worker-cancellation-redrive-and-process-restart |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/scheduled-executions/status-summary | [x] Asserted success | async-scheduled-execution-history-and-replay |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/recurring-schedule | [x] Asserted success | recurring-schedule-configuration-and-preconditions |
| PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/recurring-schedule | [x] Asserted success | recurring-schedule-configuration-and-preconditions |
| GET /v1/tenants/{tenantId}/async-decision-executions | [x] Asserted success | async-scheduled-execution-history-and-replay |
| POST /v1/tenants/{tenantId}/async-decision-executions | [x] Asserted success | async-scheduled-execution-history-and-replay, worker-cancellation-redrive-and-process-restart, delivery/callback-rejection-redrive-cancellation-and-restart |
| GET /v1/tenants/{tenantId}/async-decision-executions/status-summary | [x] Asserted success | async-scheduled-execution-history-and-replay |
| GET /v1/tenants/{tenantId}/async-decision-executions/{executionId} | [x] Asserted success | async-scheduled-execution-history-and-replay, worker-cancellation-redrive-and-process-restart, unrouted/fixture-and-new-direct-routes, delivery/callback-rejection-redrive-cancellation-and-restart |
| POST /v1/tenants/{tenantId}/async-decision-executions/{executionId}/retry | [x] Asserted success | worker-cancellation-redrive-and-process-restart |
| GET /v1/tenants/{tenantId}/platform/custom-lists | [x] Asserted success | custom-lists-csv-and-platform-records; [frontend list counts](../../frontend/evidence/mysql-integration/2026-11-10T13-20-22.737Z/report.json) |
| POST /v1/tenants/{tenantId}/platform/custom-lists | [x] Asserted success | custom-lists-csv-and-platform-records, unrouted/screening-config-lifecycle-and-list-entry-alias |
| GET /v1/tenants/{tenantId}/platform/custom-lists/{listId} | [x] Asserted success | custom-lists-csv-and-platform-records |
| PUT /v1/tenants/{tenantId}/platform/custom-lists/{listId} | [x] Asserted success | custom-lists-csv-and-platform-records |
| DELETE /v1/tenants/{tenantId}/platform/custom-lists/{listId} | [x] Asserted success | custom-lists-csv-and-platform-records, unrouted/screening-config-lifecycle-and-list-entry-alias |
| GET /v1/tenants/{tenantId}/platform/custom-lists/{listId}/entries | [x] Asserted success | custom-lists-csv-and-platform-records |
| POST /v1/tenants/{tenantId}/platform/custom-lists/{listId}/entries | [x] Asserted success | custom-lists-csv-and-platform-records |
| POST /v1/tenants/{tenantId}/platform/custom-lists/{listId}/entries/import | [x] Asserted success | custom-lists-csv-and-platform-records |
| PUT /v1/tenants/{tenantId}/platform/custom-lists/{listId}/entries/{entryId} | [x] Asserted success | custom-lists-csv-and-platform-records |
| DELETE /v1/tenants/{tenantId}/platform/custom-lists/{listId}/entries/{entryId} | [x] Asserted success | custom-lists-csv-and-platform-records |
| GET /v1/tenants/{tenantId}/platform/custom-list-entries | [x] Asserted success | custom-lists-csv-and-platform-records |
| POST /v1/tenants/{tenantId}/platform/custom-list-entries | [x] Asserted success | unrouted/screening-config-lifecycle-and-list-entry-alias |
| GET /v1/tenants/{tenantId}/platform/record-tags | [x] Asserted success | custom-lists-csv-and-platform-records |
| POST /v1/tenants/{tenantId}/platform/record-tags | [x] Asserted success | custom-lists-csv-and-platform-records |
| POST /v1/tenants/{tenantId}/platform/risk-snapshots | [x] Asserted success | custom-lists-csv-and-platform-records |
| GET /v1/tenants/{tenantId}/platform/ip-flags | [x] Asserted success | custom-lists-csv-and-platform-records |
| POST /v1/tenants/{tenantId}/platform/ip-flags | [x] Asserted success | custom-lists-csv-and-platform-records |
| GET /v1/tenants/{tenantId}/outbox-events | [x] Asserted success | async-scheduled-execution-history-and-replay, delivery/real-case-screening-scoring-and-outbox-delivery |
| POST /internal/screening-status-updates | [x] Asserted success | unrouted/screening-scoring-status-retry-and-authoritative-callback |

## Mounted extensions outside the shared specification

| Operation | Live local status |
| --- | --- |
| GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/recurring-schedule/{scheduleId} | [x] Asserted success |
| GET /redoc | [x] Asserted success |
| GET /openapi.yaml | [x] Asserted success |
| GET /docs | [x] Asserted success |
| GET /capabilities | [x] Asserted success |
| POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/scheduled-executions/{executionId}/cancel | [x] Asserted success |
| POST /v1/tenants/{tenantId}/async-decision-executions/{executionId}/callback/cancel | [x] Asserted success |
| POST /v1/tenants/{tenantId}/async-decision-executions/{executionId}/callback/retry | [x] Asserted success |
| POST /v1/tenants/{tenantId}/async-decision-executions/{executionId}/cancel | [x] Asserted success |
| DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/recurring-schedule | [x] Asserted success |

## Reproduce

From this service directory with the stack running:

```powershell
node scripts/mysql-endpoint-audit.mjs
```

The audit retains uniquely named fixtures. It stops/restarts the decision worker and private provider, restores both, and locks only its newly created committed iteration for persistence failure checks. The local callback receiver temporarily applies a rejection policy to exercise actual durable negative receipts, automatic retry exhaustion, cancellation and generation-fenced redrive. A future recurring schedule is created and disabled; calendar ticking is covered in private integration tests.

## Local delivery behavior and limits

Workflow and scoring configuration routes and callback controls are enabled. Case actions use the case owner's real receipt service. Screening uses explicit exact-name local watchlists and publishes real owner matches/results. Additional scoring uses `local:decision-score-v1`, which returns the immutable decision score and a deterministic risk band. Callback, workflow event and outbox acceptance means a durable local inbox receipt; it does not mean delivery to an external network. Arbitrary callback/event URLs, external scoring rulesets and unsupported workflow actions receive validation errors or durable rejected receipts. See [MYSQL_DOCKER.md](MYSQL_DOCKER.md) for request examples.

Screening callbacks reconcile authoritative screening-owner SQL rather than trusting snapshots. Investigator review checks a real seeded session and actual case membership. Machine decisions remain unchanged. The local stack does not establish production provider integration, production throughput, or transfer existing PostgreSQL records.
