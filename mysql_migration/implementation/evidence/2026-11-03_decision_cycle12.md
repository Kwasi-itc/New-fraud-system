# Cycle 12: history acceptance, evaluation fixture and correctness fixes

## Completed scope

Completed D14.12-D14.16 and D15.17-D15.18: **132/152 complete, 20 remain**.
The user selected 1,000 evaluations/second and PostgreSQL provider reuse, then
instructed completion after the assistant stated it would retain the other
benchmark settings as engineering defaults. The [direction record](2026-11-03_decision_performance_provider_direction.md)
distinguishes that delegation from individually user-selected numbers. No new
architecture, application database or runtime activation is implied.

History baseline: 100,000 retained decisions, 10 tenants, 60,000 in the busiest
tenant, 20 scenarios per tenant, 20 offered requests/second for five minutes,
four workers, eight maximum pool connections, p95 <=250ms, p99 <=750ms and zero
unexpected errors. The request mix is tenant page, scenario page, exact hot
object page, contains-search page, tenant count and cursor page. Broad contains
counts are examined in the plan corpus, not in the timed request mix.

Evaluation target: 1,000 offered evaluations/second for five minutes; ten tenants
with 60% of requests in the busiest tenant; ten rules, including two distinct
indexed SQL aggregates; the same latency/error targets. The fixture enables all
four effect kinds and verifies decision, evidence, effect and required-job counts.
These targets are recorded, **not met by the evaluation benchmark**.

## Verification and results

- [History workload](2026-11-03_cycle12-history-acceptance.json),
  [test output](2026-11-03_cycle12-history-tests.jsonl): 6,000/6,000 requests
  succeeded, no failures/drops/cancellations; p95 21.417036ms, p99 31.468878ms.
  Tenant rows are 60,000; four tenants with 4,445 each; five with 4,444 each.
  Nine unhinted plans and 301 during/boundary resource snapshots are retained.
  Pool wait count/duration and InnoDB row-lock waits/time stayed zero. Global
  database counters include concurrent fixture setup/compilation on the host;
  these results are local fixture measurements, not isolated production sizing.
- Plan review: tenant/scenario/object/cursor pages use their scoped indexes.
  Tenant count examines 60,000 covering-index rows; scenario count 3,000; exact
  hot-object count 6,000. Contains count examines all 60,000 tenant rows and joins
  scenarios, taking about 272ms in its isolated plan. Its residual scan cost is
  explicitly retained; this is not an indexed arbitrary-substring-search claim.
- [Final race-enabled checks](2026-11-03_cycle12-fixtures-final.jsonl): 11
  top-level tests and 8 subtests pass, zero failures/skips. The repeatable command
  is `sh scripts/test-load-fixtures.sh` inside the private Compose test service.
  The script also ran vet on loadacceptance, service and store/mysql successfully.
- [Evaluation smoke](2026-11-03_cycle12-evaluation-smoke.json): 100/100 successful
  decisions, 1,000 evidence rows, 100 each of workflow/screening/scoring records,
  400 outbox records and 700 each of required receipts/deliveries. This verifies
  the fixture and atomic producer contract, not delivery throughput.
- [Initial 100/sec ramp](2026-11-03_cycle12-evaluation-ramp.json): 3,000 offered,
  1,499 succeeded, one error and 1,500 bounded-queue drops. The [InnoDB diagnostic](2026-11-03_cycle12-innodb-status.txt)
  identifies an outbox producer deadlock caused by a tenant/status index scan
  locking another decision's pending event rows.
- [Post-fix ramp](2026-11-03_cycle12-evaluation-ramp-fixed.json),
  [test output](2026-11-03_cycle12-evaluation-ramp-fixed-tests.jsonl): 3,000 offered,
  1,529 succeeded, **zero evaluation errors**, 1,471 queue drops. p95 232.004289ms,
  p99 266.867302ms. All persisted counts match successful decisions. The bounded
  four-worker configuration sustains approximately 51 successful evaluations/sec
  in this run, far below the target. No 1,000/sec acceptance is claimed; raising
  offered load after a failed 100/sec ramp would not establish capacity.

## Implemented changes

`cloneTenantModel` now preserves the table publication version. The new actual
service aggregate fixture exposed that shared metadata copying had discarded
it. The regression checks cold/shared/cache paths and isolation of returned
field/link collections; actual SQL aggregates now execute successfully.

The required-effect producer now selects the primary index explicitly for its
bounded sorted owner-ID locking query, retaining the tenant predicate. This
prevents an optimizer-selected tenant/status scan from locking unrelated owners.
The [controlled regression failed before the fix](2026-11-03_cycle12-lock-before.jsonl)
and passes after it: one unrelated owner stays locked while another producer
commits four required jobs. Existing multi-chunk, rollback, tenant, read-only,
terminal-owner and duplicate scheduling checks pass. No broad transaction retry
or weakened foreign-key checks were introduced.

History sampling now records database/pool resource counters each second during
the workload and joins the workload before returning after a sampling failure.
The new evaluation fixture uses one generated database/pool, deterministic
tenant/model/rule identities, a real clock, actual repositories, actual SQL
aggregates and actual transactional effect/job production. It retains redacted
error categories, raw samples and exact persisted counts, including on failure.

## Material remaining work

D15.19 remains open: the evaluation target is unmet. Database query/transaction
profiling and a revised capacity design within the shared resource budget are
needed. D15.14/.20 and D19/D20 real-provider acceptance remain open. The fixture's
model registry and authority are consumer contracts, not real-owner publication
or identity composition; it seeds 100 wallet records per tenant and does not
combine evaluation with the 100,000 retained history population.

The existing PostgreSQL scoring/outbox HTTP clients do not supply atomic receipt
lookup. This cycle adds no substitute scoring engine, no fake provider receipt,
and no external send. MySQL case mutation ownership, receiver recovery contracts,
restricted-account composition, process crashes, uncertain commit, full migration
compatibility and sustained combined load remain required. PostgreSQL remains
the configured application database. The initial scope-limit and publication
fixture failures are retained alongside the final passing checks.

Cleanup completed: the `decision-cycle12` runner, MySQL container and private
network were removed. Focused diff checks passed with `cr-at-eol` for the existing
CRLF query file; no application resources were stopped or changed.
