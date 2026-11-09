# Decision cycle 18: critical/high-priority integration and mixed load

PostgreSQL remains active. No application database or runtime setting was changed.
This is supporting evidence, not full provider or capacity acceptance. The original
checklist remains 132/152 complete, 19 active remaining and 1 deferred.

## Changes

- Extended the actual MySQL case-owner handoff test: the receiver commits a case,
  the producer lease expires before completion, receipt-aware maintenance rescues
  the delivery, a reconstructed dispatcher claims a new lease and reconciles the
  original key/hash. A stale worker cannot complete; the replacement completes
  with the retained case receipt and no duplicate case. Lease/retry deadlines are
  advanced only in the disposable fixture. This adds a consumer-recovery scenario;
  existing subprocess-crash and uncertain-COMMIT tests remain in the same suite.
- Added concurrent, bounded, tenant-scoped history reads to the evaluation load
  harness, using the same database and eight-connection pool. Both workloads have
  independent offered/success/error/drop/latency reports; either missing its
  target fails the run. Reads validate tenant identity.
- Added an explicit scoring-deferred profile: no active scoring configuration,
  requests or jobs. Each successful evaluation produces ten evidence rows,
  workflow/screening work, three outbox events and five required deliveries.
  This fixture does not replace the separately tested runtime deferral guards.
- Exposed the existing aggregate-concurrency setting in the diagnostic profile,
  bounded by the eight-connection fixture budget. The default remains one; an
  experiment uses four. No deployed setting or production SQL behavior changed.
- Added reproducible scripts for critical recovery/shared-owner checks and mixed
  load. See [commands and limits](../../../backend/decision-engine-service/scripts/CRITICAL_ACCEPTANCE.md).

## Correctness verification

All four scripts passed with race detection, zero failures/skips. Relevant vet
checks passed. Total: **62 top-level tests and 165 subtests**.

| Suite | Top-level | Subtests | Raw evidence |
| --- | ---: | ---: | --- |
| Load harness, batching and related checks | 16 | 14 | [log](2026-11-04_cycle18-load-regression.jsonl) |
| Actual MySQL/PostgreSQL case workflow and owner regression | 26 | 44 | [log](2026-11-04_cycle18-workflow.jsonl) |
| Runtime/scoring exclusion, uncertain commit, cancellation and callback recovery | 15 | 97 | [log](2026-11-04_cycle18-critical.jsonl) |
| Shared model owner, verified sessions, browser edits, revocation and screening | 5 | 10 | [log](2026-11-04_cycle18-shared-owners-final.jsonl) |

These verify the named real-owner paths and local recovery boundaries. They do
not mean every configured external action or event destination was exercised.
After adding the explicit aggregate policy, the mixed smoke was rerun with race
detection and vet: [final smoke](2026-11-04_cycle18-final-smoke.jsonl),
[vet](2026-11-04_cycle18-final-vet.log). Both passed. This repeat is not counted as
an additional distinct test in the totals above.

## Mixed-load results: acceptance failed

Pinned MySQL 8.4.8; Docker reports 16 CPUs and 7,702,482,944 memory bytes. The MySQL
container has no dedicated CPU/memory cap. Runner GOMAXPROCS=2; database pool=8.
Each profile runs 30 seconds, ten tenants with 60% evaluation hot-tenant traffic,
ten rules including two indexed SQL aggregates, and 20 history reads/sec with
two history workers. Small business tables and initially empty history are used.
Model and authority are explicit fixtures; no external receiver drains work.

| Profile | Evaluation workers / aggregate slots | Offered | Completed | Dropped | Errors | Evaluation p95/p99 ms |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| Ramp | 4 / 1 | 3,000 | 1,749 | 1,251 | 0 | 218.95 / 263.17 |
| Target diagnostic | 16 / 1 | 30,000 | 2,306 | 27,694 | 0 | 629.14 / 905.59 |
| Tuning experiment | 16 / 4 | 30,000 | 3,237 | 26,763 | 0 | 411.65 / 486.49 |

History passed 600/600 in every run, with p95/p99 respectively 7.58/12.45,
9.38/15.26 and 47.08/60.70 ms. Exact persisted counts matched successes and scoring
requests remained zero. **None met evaluation acceptance; 1,000/sec is not achieved.**

The four-slot experiment completed about 40% more evaluations in this single
comparison. Aggregate-evaluation mean time fell from about 156 ms to 59 ms, while
pool waits increased to 16,753 with 179.84 seconds of accumulated wait across
requests. This identifies a tuning tradeoff, not a repeatable production gain or
permission to raise all pool limits. Shared resource allocation remains required.
Earlier four-effect profiles are not comparable to these scoring-deferred runs.

Raw reports: [ramp](2026-11-04_cycle18-shared-ramp.json),
[target](2026-11-04_cycle18-shared-target.json),
[tuning](2026-11-04_cycle18-shared-tuned.json). Corresponding `*-tests.jsonl` files
retain nonzero acceptance results. Initial shell/profile-path invocation errors
were corrected before these measured runs; they are not capacity observations.

## Open critical/high-priority work

1. Actual outbox event destination and durable receipt/reconciliation contract.
   Repository inspection still finds an optional sender and empty example URL,
   not an identified receiving service. No placeholder receiver was substituted.
2. Remaining external workflow actions and full screening search-worker/runtime
   composition with trusted identities and deployed grants.
3. Full real-provider integration/failure/cancellation acceptance after those
   receivers are available; selected owner tests do not close this gate.
4. Representative shared-load data and staging CPU/RAM/replica/connection budget,
   sustained load acceptance and further performance work to reach 1,000/sec.

The receiver and staging resource questions were raised while independent work
continued. Production-sized restore, fencing and cutover remain separate gates.
