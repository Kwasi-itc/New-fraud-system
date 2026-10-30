# Implementation and validation evidence

Executed 2026-10-29 after the instruction to continue. Application logic was not changed. Tooling reuses the existing service clients, local lifecycle/migrations, observers, histogram and report writer. The shared fixed-arrival iterator was extracted to `system_tests/arrival_schedule.py` and covered by both existing and new tests. No new dependency was installed.

## Automated verification

- `python -m unittest discover -s backend/stress-tests/decision-benchmarks/tests -v`: 17 passed. Checks config bounds, deterministic fixtures, independent aggregate math, incorrect decision/evidence rejection, authentication configuration, schedule/drop accounting, no retry and ingestion response identity.
- From `backend/stress-tests`, `python -m unittest system_tests.tests.test_queue_campaign system_tests.tests.test_campaign system_tests.tests.test_queue_recovery -q`: 31 passed. A final regression run caught removal of stage offsets still used by queue reports; those offsets were restored and all 31 passed. This restoration happened after live qualification and affects the existing queue runner's reporting, not the decision workload path.
- Host Go unit tests were not run (Go absent from PATH); live Docker builds compiled the services. These tests do not establish full application readiness.

## Saved live campaigns

| Campaign | Valid trials | Reference cases | Measured inputs | Scenario decisions | Rule executions |
|---|---:|---:|---:|---:|---:|
| [Development smoke](runs/local-smoke-20261029-r2/report.md) | 9 | 324 | 45 | 60 | 255 |
| [Extended qualification](runs/qualification-20261029/report.md) | 11 | 432 | 36 | 36 | 75 |

Both campaigns recorded zero measured failed requests, generator drops and persisted-evidence mismatches. They used actual synchronous APIs, authentication, isolated shared PostgreSQL and explicit `direct_db` historical reads. Each `lifecycle.json` records `passed:true` and `cleanup_verified:true`. The earlier `runs/local-smoke-20261028` setup failure is retained: the ingestion Dockerfile required the backend build context; the benchmark fixture was corrected. No workload ran in that failed attempt.

Development smoke ran three repeats each of S1/R1 numeric, S2/R5 mixed scalar and S1/R6 aggregate with 100 history rows. It is development evidence: harness files evolved during that campaign. Extended qualification froze source before building and checked it again before traffic; the campaign stores hashes and image identities. Keep this distinction when comparing versions.

Extended qualification covered seven scalar families, a three-level AND with the aggregate predicate last, all six aggregate values, two tenant schemas, alternating rule outcomes, one false trigger among two scenarios and concurrent ingestion. The ingestion case completed six measured plus two warm-up inserts without retries; excluded merchant keys kept selected-history results immutable. Qualification includes boundaries, required missing/null fields, malformed values, nullable normalization, invalid/absent auth and cross-tenant decision reads. Qualification-case counts exclude extra auth/read assertions.

The repeated numeric cohort achieved 0.9966-0.9990 correct inputs/s at an offered 1/s. Its per-run p50 was 18/22/17 ms and p99 142/96/87 ms, with only five measured requests per repetition. These tails are descriptive smoke observations, not stable percentile estimates. Other extended trials had only two to six measured requests. Configured concurrency of two did not ensure overlap: the interference trial's measured peak decision inflight was one. Do not call this a concurrent decision capacity test.

Aggregate plans and exact table counts/index definitions were captured outside timing. Query-plan execution and qualification warmed data before measurement; these are not cold-cache tests. Service/host/process/PostgreSQL observations had no incomplete samples in the interference trial, but app container CPU, DB CPU, per-rule timing and exclusive SQL attribution remain unavailable. Short telemetry windows cannot establish a bottleneck.

## Scope still open

This is an implemented, locally qualified foundation, not completion of every enterprise benchmark stage. The checklist retains open acceptance work: HTTP/direct reader parity, exhaustive operators and snoozes, full rule/scenario sweeps, 10k-5m history, genuine concurrent decision overlap, burst/soak/overload recovery, DB failures/partial commits, evolving selected history, instrumentation calibration, resource attribution and production SLOs. The progressive examples are plans, not measured results. No production capacity or optimization benefit is claimed.

Prioritize authenticated reader parity and telemetry coverage before scale experiments. Source inspection shows the ingestion HTTP reader does not attach the service token; weakening auth to make a benchmark pass would invalidate the exercise. Preserve authentication and test/fix the service integration under a separate application change. Then calibrate the generator and resource budget, run repeated one-axis sweeps and attribute latency with plans/phase counters before proposing production optimizations. The audit's optimization candidates remain hypotheses.

Generated runs are ignored by git. Archive complete directories, including environment/source manifests and raw NDJSON, to retain reproducible evidence beyond this workspace. Source hashes identify dirty working-tree contents but do not archive those contents; retain the corresponding checkout or patch separately.
