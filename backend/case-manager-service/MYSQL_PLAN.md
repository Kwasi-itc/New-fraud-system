# Case management MySQL migration

Application storage remains in the shared `fraud` database. The existing
`case_manager` migration owner and physical case tables are extended in place;
decision workflow receipts and case reviews must retain their identities.
Keycloak continues to use its existing storage. The PostgreSQL entrypoints remain
available until a separate production cutover is approved.

Verified on 2026-11-09. `[x]` means completed with evidence; `[ ]` means work
remains. The core local migration is verified, but full endpoint functionality
is still incomplete: **59 of 61 operations have successful workflows; two
operations remain unfinished.**

## Completed migration work

- [x] Inventory all 61 source operations and the maintenance/delivery workers.
- [x] Append the case lifecycle schema to the existing owner lineage, with
  independent pinned MySQL reference verification before application deployment.
- [x] Preserve existing case, membership and workflow receipt identities during
  the in-place upgrade; verify compatibility with existing decision workflows.
- [x] Implement native typed repositories and reuse the source domain/handlers.
- [x] Enforce tenant/inbox permissions, current identity sessions, revocation and
  authorization versions, including checks within mutation transactions.
- [x] Port case lifecycle, inboxes/members, tags, decision references, workspace,
  native evidence upload/download, reports, analytics and record exploration.
- [x] Implement maintenance: upload expiry, snooze wake-up and automatic assignment.
- [x] Implement and fixture-test durable notification transport, bounded claims,
  retry/backoff, lease fencing, idempotency and explicit replay. HTTP runs outside
  SQL transactions; live delivery activation remains unchecked below.
- [x] Configure restricted API/worker accounts and Docker services using the
  shared MySQL `fraud` database. The API and maintenance worker are healthy.
- [x] Provide Swagger at `http://localhost:8086/docs`; a real browser renders all
  61 operations. Rendering a route does not establish positive functionality.
- [x] Retain the PostgreSQL source tests and buildable server, worker, migration
  and outbox executables; the updated decision installer passes package checks.

## Completed verification

- [x] Full suite: 98 top-level tests / 190 test and subtest results, with no
  failures, skipped/missing tests or race warnings; vet passed. This is the
  full regression run after restoring the screening contracts.
- [x] Independent MySQL 8.4.8 target/reference fixtures and disposable PostgreSQL
  source characterization, without destructive application-database testing.
- [x] Successful HTTP workflows for 59 operations and unauthenticated rejection
  for all 59 protected operations.
- [x] Validation, permissions, tenant isolation, revocation, rollback, capacity,
  concurrent assignments, receipt replay and legacy-provider compatibility.
- [x] Recheck running API/worker health and rerun read-only auth/tenant/Swagger
  acceptance while updating these checklists; all 15 checks passed.

Evidence: [full test report](evidence/mysql-integration/2026-11-09T22-56-11Z/report.json),
[HTTP inventory](evidence/mysql-http-results.json) and
[latest live smoke](evidence/mysql-runtime-smoke.json).

## Screening integration and remaining functional work

- [x] Complete manual screening investigations using authoritative creator and
  candidate provenance.
- [x] Complete screening-file import and permission-checked download, verifying
  actual owner bytes and SHA-256 alongside metadata.
- [x] Complete screening-reviewed intake using manual/continuous and decision
  provenance with stable receipts.
- [x] Complete screening-requires-review intake using immutable owner arrivals
  and configured inbox routing.
- [x] Implement native screening evidence callbacks with verified owner publication,
  atomic file import/hash checks, replay and concurrent delivery/rollback tests.
- [ ] Implement the source AI case-review run endpoint and its execution (`501`).
- [ ] Implement the explicit internal auto-assignment run endpoint (`501`).
  Automatic assignment already runs in the maintenance worker.
- [x] Add successful tests for the five screening operations, including
  replay, forged provenance rejection and imported evidence bytes.
- [ ] Add successful functionality/tests for the two source placeholders;
  expected error responses do not count as completed workflows.
- [ ] Configure the notification publisher and verify delivery, receiver
  deduplication and recovery against it. `PublisherURL` is currently empty;
  outbound delivery is disabled and pending notifications are retained.

The [endpoint checklist](MYSQL_ENDPOINT_STATUS.md) lists every route, its current
evidence and its blocker. The [worker checklist](MYSQL_WORKER_STATUS.md) separates
implemented transport from running maintenance and pending delivery activation.

## Separate production release work

- [ ] Verify representative workload latency/throughput, query plans and the
  shared database connection/resource budget.
- [ ] Verify process-failure recovery, configured provider behavior and any
  PostgreSQL queue handover procedure.
- [ ] Plan and verify historical PostgreSQL data transfer and reconciliation.
- [ ] Complete frontend integration, production configuration and deployment
  acceptance before cutover.

No production readiness or throughput target is claimed by the local tests.
