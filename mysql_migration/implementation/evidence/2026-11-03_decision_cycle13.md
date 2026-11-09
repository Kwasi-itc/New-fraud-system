# Decision cycle 13: bounded enqueue batching and capacity diagnostics

Status: 132/152 subtasks complete; 20 remain. No additional acceptance leaf is closed. PostgreSQL runtime configuration is unchanged.

## Implementation

Evaluation persistence now uses an optional bounded job-batch capability. MySQL validates the entire request before owner locking, deduplicates identities, orders effect kinds consistently and reuses existing sorted resource chunks and permanent receipt checks. Invalid requests poison the enclosing transaction even if the caller ignores the returned error. Adapters without the capability retain sequential enqueue behavior; a failed batch never falls back to individual writes.

Fixture-only diagnostics retain duration aggregates, normalized text-protocol SQL digests and private-server command counters. The digest summaries omitted binary-protocol prepared statement bodies in this fixture, so they are not a complete SQL inventory. No object payloads or provider response bodies are recorded.

## Verification

`scripts/test-evaluation-batching.sh` passed against isolated PostgreSQL 16 and MySQL 8.4.8 fixtures. Race-enabled suites passed 25 top-level tests and 53 subtests, with no failures or skips; vet passed. Coverage includes validation, duplicate identities, ignored-error rollback, terminal owners, legacy adapters, actual-service source/target threshold and configured-effect parity, required-job rollback, publication caching and unrelated-owner lock scope.

Raw results: [regression output](2026-11-03_cycle13-regression-final.jsonl).

## Local diagnostic capacity results

Each run offered 3,000 evaluations over 30 seconds at 100/sec, with ten tenants, two indexed SQL aggregate rules, all four persisted effect kinds and an eight-connection pool. These runs use model/authority fixtures and do not send to real providers or compose all service workloads.

| Run | Workers | Completed | Evaluation errors | Dropped | p95 ms | p99 ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Before batching | 4 | 1601 | 0 | 1399 | 217.15 | 276.90 |
| Batched | 4 | 2122 | 0 | 878 | 150.14 | 179.93 |
| Batched concurrency experiment | 8 | 2416 | 0 | 584 | 266.61 | 320.15 |

The single local before/after comparison completed about 32.5% more work with batching. All runs failed offered-rate acceptance. Eight workers also exceeded the 250ms p95 limit; this experiment does not justify adopting that runtime setting. The requested 1,000/sec target is not met.

Raw artifacts: [before](2026-11-03_cycle13-profile-before.json), [batched](2026-11-03_cycle13-profile-batched.json), [eight workers](2026-11-03_cycle13-profile-eight.json), and their adjacent `-tests.jsonl` files. Earlier artifacts call the timing field `successful_evaluation_stages`; the helper can also collect failure-stage durations. The final fixture names it `evaluation_stages`.

## Remaining dependencies

Actual scoring/outbox receiver locations and their durable receipt/reconciliation contracts are not supplied by the checked local root and referenced decision environment configuration. This does not establish what is deployed remotely. Reusing PostgreSQL HTTP clients alone cannot establish receiver idempotency or recovery after a lost acknowledgement. Real workflow owner composition, provider conformance, dependent restricted-grant and lifecycle journeys, crash/uncertain-commit acceptance and shared-stack capacity remain open. No application database, provider delivery or runtime activation was changed.
