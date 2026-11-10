# Case management MySQL endpoint status

Verified 2026-11-09. Local Swagger: http://localhost:8086/docs.

**61 source operations: 59 with successful native MySQL HTTP workflows, 0 waiting for screening-owner contracts, 2 existing source placeholders.** All 59 protected operations reject unauthenticated requests.

`[x]` means a successful native workflow is evidenced; `[ ]` means functional work remains. A verified error response, including an expected `501`, does not complete an endpoint. The successful operations include the two public health endpoints.

Evidence: [full test report](evidence/mysql-integration/2026-11-09T22-56-11Z/report.json), [HTTP operation inventory](evidence/mysql-http-results.json), [live auth/Swagger smoke](evidence/mysql-runtime-smoke.json). The full suite results below come from the retained test run; the runtime smoke was last run at 2026-11-09T23:15:57.711Z.

## Verified endpoints (59)

- [x] `GET /healthz` — Public health/readiness response.
- [x] `GET /readyz` — Public health/readiness response.
- [x] `GET /v1/service-info` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/inboxes` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/inboxes` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/inboxes/:inboxId` — Successful authenticated native MySQL workflow.
- [x] `PATCH /v1/tenants/:tenantId/inboxes/:inboxId` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/inboxes/:inboxId/users` — Successful authenticated native MySQL workflow.
- [x] `PUT /v1/tenants/:tenantId/inboxes/:inboxId/users/:userId` — Successful authenticated native MySQL workflow.
- [x] `DELETE /v1/tenants/:tenantId/inboxes/:inboxId/users/:userId` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/case-queue` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/case-analytics` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/case-session` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/decision-case-memberships` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/manual-screening-investigations` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/case-bulk` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/cases/:caseId/evidence-access/:kind/:resourceId` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/cases/:caseId/overview` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/explore` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/explore/attach` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/cases/:caseId/reports` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/reports` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/cases/:caseId/reports/:reportId` — Successful authenticated native MySQL workflow.
- [x] `PATCH /v1/tenants/:tenantId/cases/:caseId/reports/:reportId` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/reports/:reportId/complete` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/cases/:caseId/reports/:reportId/export` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/cases/:caseId/links/:kind` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/close` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/cases` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/cases/:caseId` — Successful authenticated native MySQL workflow.
- [x] `PATCH /v1/tenants/:tenantId/cases/:caseId` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/decisions` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/decision-transfers` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/cases/:caseId/events` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/comments` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/tags` — Successful authenticated native MySQL workflow.
- [x] `DELETE /v1/tenants/:tenantId/cases/:caseId/tags/:tagId` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/files` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/uploads` — Successful authenticated native MySQL workflow.
- [x] `PUT /v1/tenants/:tenantId/cases/:caseId/uploads/:uploadId/content` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/uploads/:uploadId/finalize` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/cases/:caseId/files/:fileId/download` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/assign` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/unassign` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/snooze` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/unsnooze` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/cases/:caseId/escalate` — Successful authenticated native MySQL workflow.
- [x] `GET /v1/tenants/:tenantId/tags` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/tenants/:tenantId/tags` — Successful authenticated native MySQL workflow.
- [x] `PATCH /v1/tenants/:tenantId/tags/:tagId` — Successful authenticated native MySQL workflow.
- [x] `DELETE /v1/tenants/:tenantId/tags/:tagId` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/screening-events/reviewed` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/screening-events/requires-review` — Successful authenticated native MySQL workflow.
- [x] `POST /v1/screening-events/evidence-uploaded` — Successful authenticated native MySQL workflow.
- [x] `GET /internal/v1/tenants/:tenantId/inboxes/:inboxId` — Successful authenticated native MySQL workflow.
- [x] `POST /internal/v1/workflow-actions` — Successful authenticated native MySQL workflow.
- [x] `POST /internal/v1/workflow-receipts/deliver` — Successful authenticated native MySQL workflow.
- [x] `POST /internal/v1/workflow-receipts/reconcile` — Successful authenticated native MySQL workflow.

## Screening dependencies (0)


## Source placeholders (2)

- [ ] `POST /internal/v1/ai-case-reviews/:reviewId/run` — Existing source 501: AI case review. Expected error behavior is tested; positive functionality remains unimplemented.
- [ ] `POST /internal/v1/auto-assignment/run` — Existing source 501; automatic assignment runs in the maintenance worker. Expected error behavior is tested; positive functionality remains unimplemented.

## Verification checklist

- [x] Complete suite: 98 top-level tests and 190 test/subtest results; zero failures, skipped/missing tests or race warnings; vet passed.
- [x] Isolated MySQL 8.4.8 target/reference fixtures and retained PostgreSQL source characterization.
- [x] Authentication rejection on all 59 protected operations.
- [x] Tenant/inbox isolation, current authorization version and session revocation checks.
- [x] Decision attachment/transfer, bulk outcomes, receipt replay/reconciliation, audit rollback and legacy tag reattachment.
- [x] Native evidence upload/download, report versions, typed exploration and analytics workflows.
- [x] Live read-only auth/tenant smoke and browser rendering of 61 operations.
- [x] Successful workflows for all five screening contracts, including imported evidence bytes and SHA-256.
- [ ] Positive functionality and integration tests for the two source placeholders.
- [ ] Production workload/performance acceptance, historical data transfer and frontend/production cutover (separate release work).

The five screening contracts are verified with their actual owner schema. Case management reads authoritative provenance and does not write screening-owned records. Positive route coverage does not establish every production branch or external provider behavior. See [migration plan](MYSQL_PLAN.md), [Docker/runtime instructions](MYSQL_DOCKER.md) and [worker status](MYSQL_WORKER_STATUS.md).
