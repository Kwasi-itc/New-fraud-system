# Decision cycle 14: real MySQL case workflow owner

This cycle advances workflow integration, owner-flow verification, crash recovery
and migration preparation. It does not complete the six requested workstreams or
activate MySQL. The checklist remains 132/152 completed, with 19 active remaining
and D19.19 deferred by the user. Internal rule scoring remains implemented.

## Implemented

- Shared case-owner workflow receipt service for PostgreSQL and MySQL, retaining
  full attempt identity, immutable payload binding and reconciliation semantics.
- MySQL case-owned `create_case`, `add_to_case` and `add_to_case_if_possible`
  mutations. Case, membership, tags, audit, pending case notifications and receipts
  commit together. Accepted attempts retain the original case after closure.
- Explicitly authenticated, tenant-allowlisted receipt-only HTTP composition;
  no runtime command selects it. Decision dispatch uses the existing HTTP adapter.
- Bounded tag validation and membership/event/notification batches; duplicate tag
  actions preserve source audit semantics without duplicate memberships.
- Case-owner schema extension, explicit trigger definers, integrity guards and
  separate runtime/definer grants. The plan preserves the existing case migration
  ledger prefix and requires trusted canonical before/after definitions.
- Corrected the PostgreSQL shared test fixture to install actual identity-owner
  schemas required by contributor/name reads. No application database is changed.

## Verification

The saved `scripts/test-case-workflow-receipts.sh` runs race-enabled owner and
consumer tests against private MySQL 8.4.8 and PostgreSQL 16, followed by vet.
The raw final output is [workflow tests](2026-11-03_cycle14-workflow-tests.jsonl).

The MySQL corpus exercises lost HTTP acknowledgement, concurrent replay, case
reuse, closed-case replay, changed payload rejection, a new domain attempt,
foreign-tenant and wrong-token denial, restricted-account write denial, strict
missing-case behavior, cancellation during a lock wait, and late outbox failure
rolling back all case writes. A 121-action tag batch yields 120 memberships and
the expected audit events; missing and over-limit batches cannot commit a case.

Actual PostgreSQL and MySQL owners agree on case creation/reuse results and
membership/tag/audit counts. Another journey persists a decision and workflow,
claims its real delivery, prepares the durable dispatch intent, sends it to the
real MySQL case owner, and verifies persisted decision-side completion proof.

A subprocess runs the real case owner, commits a case and exits before returning
the HTTP acknowledgement. A fresh owner/pool reconciles the retained receipt and
replays without another case. This is a real process-crash test, not merely a
mocked HTTP failure. It does not cover every decision/queue/provider crash window
or a database failover/uncertain-COMMIT transport injection.

Migration checks cover a fresh combined installation, upgrade from the existing
case access prefix, repeated apply, canonical-definition verification and drift
rejection. Reference definitions are learned only on disposable fixtures, not
application databases; frozen production artifacts and complete shared-stack
upgrade/restore acceptance remain separate.

An earlier broad source run failed six tests because its fixture omitted
`identity.users`. That output is retained in
[the initial failure artifact](2026-11-03_cycle14-broad-source-fixture-failure.jsonl).
The corrected fixture and those six tests are included in the final runner.

## Final verification

Final verification: **26 top-level tests and 44 subtests passed with the race
detector, zero failures and zero skips**. The script's decision and case vet
checks also exited successfully. Raw results are recorded in
[the final workflow test artifact](2026-11-03_cycle14-workflow-tests.jsonl).

The final run additionally injects real MySQL protocol connection loss in two
places: before forwarding COMMIT, and after receiving the server's COMMIT OK
but before forwarding that acknowledgement. The owner returns an error in both
cases. A separate pool reconciles the actual durable receipt: absent for the
rolled-back transaction, accepted for the committed transaction. Retrying the
original identity produces exactly one case, decision link and set of audit,
outbox and receipt rows. This covers the case-owner commit boundary, not every
decision/effect transaction, database failover or storage-loss scenario.

The bounded tag test covers 121 actions with 120 unique memberships, preserving
123 audit events while batching validation, memberships and audit/outbox writes.
Missing tags and oversized batches fail without committing a case. This is
correctness evidence; it does not establish representative capacity.

## Capacity experiment

An experimental adapter capability avoided the service's duplicate enqueue pass
after MySQL's atomic effect/job creation. Its rollback/parity regressions passed,
but the 30-second local ramp completed 2,141 of 3,000 offered evaluations, with
zero evaluation errors, 859 drops, p95 154.77ms and p99 183.90ms. This is about
71 completed evaluations/sec, and does not establish 1,000/sec capacity. Compared
with cycle 13's 2,122 completions, it provides no meaningful throughput evidence.

The experimental capability was removed; the validated batching path remains.
The following artifacts describe that removed experiment, not the final runtime:
[measurement](2026-11-03_cycle14-evaluation-experiment.json),
[test output](2026-11-03_cycle14-evaluation-experiment-tests.jsonl).
As in prior ramps, all four fixture effects are persisted, including the now
deferred additional scoring effect; providers are not drained. This is not a
representative full shared-stack capacity acceptance run.

## Still open

External workflow receivers and the actual decision-event receiver remain
unidentified. Existing `OUTBOX_PUBLISHER_URL` clients are send-only; a durable
receiver/reconciliation contract cannot be inferred from them. No replacement
receiver or fake acceptance receipt was introduced.

The new case adapter exposes workflow intake, not the case service's complete
MySQL investigator/queue/assignment/screening/maintenance APIs. It must not run
alongside PostgreSQL case mutation authority. Decision authority/model setup in
the integration fixture remains explicit fixture scope; full real worker identity
composition and all-provider lifecycle/revocation acceptance are still required.

Remaining gates include safe runtime exclusion of deferred additional scoring,
real event delivery, full service composition, all crash/uncertain-commit cases,
representative 1,000/sec shared-database load, frozen release/grant artifacts and
full upgrade/restore/cutover rehearsal. PostgreSQL remains active.
