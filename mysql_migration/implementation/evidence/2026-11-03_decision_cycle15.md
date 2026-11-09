# Decision cycle 15: explicit scoring deferral and decision COMMIT recovery

PostgreSQL remains active. This cycle changes staged MySQL capabilities and
tests; it does not activate MySQL or complete all six requested workstreams.

## Implemented and verified

- Explicit `EvaluationPolicy.DeferAdditionalScoring` removes only the additional
  scoring worker and its maintenance loop from runtime composition and slot
  accounting. The corresponding worker policy and dispatcher must be absent.
  Other required dispatchers remain mandatory. Scoring configuration HTTP
  operations return unavailable; retained request history remains readable.
- Cold startup rejects active scoring configurations, unfinished requests
  (including failed requests requiring disposition), or available/running scoring
  deliveries. It does not alter or discard any of them. Completed history with
  terminal delivery state is allowed.
- Preparation rechecks actual scoring configuration and propagates read errors.
  Deferred effect capabilities reject request creation, updates and enqueue.
  Ignoring a rejection still poisons the enclosing decision transaction. Internal
  rule-score persistence remains available; default scoring-enabled behavior is
  preserved.
- Real MySQL protocol faults now exercise both decision/effect/job COMMIT
  outcomes: command lost before the server commits, and server COMMIT OK lost
  before the client receives it. The transaction exposes `mysqlstore.CommitError`.
  A healthy owner pool reconciles by retained decision identity. Rollback leaves
  none of the rows; successful commit retains one decision, each of four effects
  and all four receipt/delivery pairs. No committed mutation is blindly repeated.
- Fixed an outdated runtime test expecting case-insensitive list lookup. Source
  PostgreSQL `CustomListRepository.ListByName` uses exact equality; tests now
  require exact case, accent and trailing-space matching. No lookup behavior was
  changed to satisfy the test.

## Verification

`scripts/test-scoring-deferral.sh` ran on the private `decision-cycle15` stack,
MySQL 8.4.8 and Go 1.25.8. **5 top-level tests and 84 subtests passed with the race
detector; zero failures and zero skips.** Vet passed for the changed store,
execution and runtime packages. The selection includes the complete runtime
route/worker lifecycle test and required-effect job atomicity regressions.

[Final raw results](2026-11-03_cycle15-final.jsonl).
[Initial stale list-name assertion failure](2026-11-03_cycle15-deferral-before.jsonl).

The real databases and SQL transactions are used. Identity/model providers in
the existing runtime fixture remain explicitly test providers. The commit test
proves the local atomic boundary; it does not prove downstream event delivery,
replica failover, disk loss or every scheduled/async completion boundary.

## Staged configuration and operating limits

Set `policy.Evaluation.DeferAdditionalScoring = true`, remove `JobScoring` from
`policy.Workers` and `deps.Dispatch`, and retain the four effect queue policies
used by the storage contract. Do not supply a successful no-op scoring provider.
Startup checks run with the scope's authorizer and bound startup timeout. Their
existence queries use tenant-scoped predicates and return no unbounded row data;
they are cold-start checks, not a claim of representative query-cost acceptance.

The previous runtime's writers must be fenced before activation. These local
capabilities do not prevent an independently authorized process or database
administrator from introducing work. Existing active configuration/backlog must
be resolved through its actual owner before selecting deferred mode. The mode
is not wired into deployed commands; PostgreSQL continues to be selected there.

## Remaining acceptance

Checklist remains **132/152 complete: 19 active remaining, 1 deferred**. D19.19
stays deferred, not completed. Its safe-runtime-exclusion prerequisite is now
implemented. D20.16 has additional local decision-transaction evidence but its
full-service/provider prerequisites remain open.

The real decision-event receiver contract and external workflow-action receiver
contracts remain unidentified. Full real-provider composition, all remaining
cancellation/crash boundaries, representative 1,000 evaluations/sec shared-load
acceptance, and production upgrade/restore/cutover verification remain open.
This cycle makes no new throughput claim. The prior local measurements remain
below the requested target.
