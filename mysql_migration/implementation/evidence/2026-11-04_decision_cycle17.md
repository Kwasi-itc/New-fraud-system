# Decision cycle 17: production-readiness checks

Scope: supporting D20.12/D20.17/D25 checks on isolated MySQL 8.4.8. No application
database was accessed and PostgreSQL remains active. This does not close the full
shared-stack release gates or change the 132/152 completed acceptance count.

## Implementation

- Added read-only offline declared-FK inspection. It checks every visible declared
  relationship, including every column of composite tenant keys, and rejects an
  empty inventory, oversized metadata inventory, query errors or cancellation.
  Nullable references follow MySQL FK semantics. Imported orphans are rejected
  even after `foreign_key_checks` has been reenabled.
- Added this inspection after restoring original plans/data/guards and before
  the actual restricted case-owner receipt replay.
- Expanded the frozen decision release upgrade rehearsal to include the original
  115-step prefix before 140, 145 and the current plan.
- Added `-ReleaseChecks` to the reproducible restore runner. It runs release and
  command validation, actual restricted API/worker grant tests and vet before
  starting the bounded restore companion. Missing required tests or skips fail.
- Added the [production-readiness checklist](../modules/03_decision_engine_production_readiness.md)
  with distinct local and deployed-system acceptance criteria. Corrected stale
  documentation: library installation has same-session target guards, while the
  production command still exposes no apply/down/force/activate operation.

## Verification

Raw results and cleanup are stored in
[the runner evidence directory](2026-11-04_cycle17-production-readiness/result.json).
The release/command/grant suite passed **7 top-level tests and 12 subtests**, race
enabled, with zero failures or skips. This includes fresh/upgrade/dirty-step
recovery, wrong-writer and unlocked-definer rejection, drift/lineage detection,
unrelated owner preservation, restricted worker dispatch and mutation denials.
The new negative corpus detects missing parents and mismatched tenant parents;
valid nullable keys pass, and cancellation/empty inspection fail closed.

The separate real restore test also passed: **77 foreign-key relationships** and
**263 exact schema/data inventory entries** verified before retained case receipt
reconciliation/replay. Combined result: **8 top-level tests and 12 subtests**, no
failures/skips, race enabled; vet passed. The runner reported `Status: passed`,
`ReleaseChecks: passed`, `Cleanup: passed`, `ApplicationDatabaseTouched: false`.
Snapshot SHA256: `5f030dc075c413583707ba37e3e8f63edb50e2eb2fd3534e40a7bcce99484dd8`.
The new readiness breakdown has **9 local checks completed and 6 deployed-system
checks still open**; these are supporting checks, not new leaves in the 152 count.

## Limits

Inspection requires a fully privileged read-only inspection identity with metadata
visibility and SELECT on the entire reviewed schema inventory; partial metadata
visibility cannot prove completeness. The operation is for an offline fenced
restore, not an API readiness probe. It may scan entire child tables, using parent
keys for lookup, and must be timed on production-sized backups. No query-plan or
capacity optimization is claimed. Relationships absent from FK declarations need
separate business validation. Shared-owner inventory, deployed grants/TLS,
production-sized restore/PITR, writer/provider fencing, actual event receivers,
capacity and cutover approval remain open. External scoring remains deferred.
