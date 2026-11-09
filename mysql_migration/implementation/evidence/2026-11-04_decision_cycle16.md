# Decision migration cycle 16: restore rehearsal and measured performance experiment

Status: supporting verification complete; full migration acceptance remains open.
PostgreSQL remains active. Checklist stays **132/152 complete, 19 active remaining,
1 deferred**. No production database was used.

## Restore implementation and result

Added a private MySQL 8.4.8 restore target, dump companion, explicit opt-in Go
integration test and PowerShell runner. The test persists a decision, its four
effect kinds and required jobs, and an actual case-owner HTTP receipt. It restores
onto a server with a different UUID. All **263 schema/data inventory entries**
match: exact definitions, every base-table row count and full table checksums.
Original decision, identity and case migration ledgers verify. Reconciliation and
redelivery through the restored real case HTTP owner return the original receipt
without changing the inventory. Restricted runtime grants deny receipt deletion.

The race-enabled restore test passed. Raw evidence:
[final restore](2026-11-04_cycle16-restore-final.jsonl).
Snapshot SHA256: `29dbe46e339d3a55c7bff6cc4373171961fa51b107adc0dafa4784dd1ebb2d3c`.

The initial full schema dump did **not** pass exact verification: restored character
set declaration rendering and routine line endings differed. Retained evidence:
[initial failure](2026-11-04_cycle16-restore-before.jsonl),
[diagnostic](2026-11-04_cycle16-restore-diagnostic.jsonl).
The passing approach installs the original frozen plans and imports data only;
it does not relax drift checks. Guard triggers are absent only while constructing
the new isolated offline target and are reinstated and verified before access.

Both the manual Compose/Go path and the complete PowerShell runner passed. The
runner independently created fresh servers, compiled and ran the race-enabled
test, validated its required pass and removed its containers/network. See
[runner result](2026-11-04_cycle16-restore-runner/result.json) and adjacent raw logs.
Its independently generated snapshot hash was
`d2f323bbd42a0a7819b4f328b0a65be97a3aea3ef14b25b5dbb177971e83c3a5`.
Both disposable Compose projects were removed after saving evidence. This is a small
logical-restore rehearsal, not production-volume restore, PITR, failover, or full
service activation acceptance. The fixture includes deferred scoring storage for
preservation checks; it does not activate an external scoring provider.

## Performance experiment rejected

Experiment: avoid redundant owner/receipt checks only for newly inserted effects.
Paired fixed-binary runs did not reproduce an improvement, so the entire shortcut
was removed. The established checked enqueue path remains.

| Paired run | Offered | Successful | Dropped | Errors | p95 ms | p99 ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Baseline | 3000 | 2006 | 994 | 0 | 152.107383 | 179.267230 |
| Experiment, reverted | 3000 | 1953 | 1047 | 0 | 170.067539 | 193.102039 |

Both failed load acceptance. These local fixture results do not establish the
requested 1,000 evaluations/second or representative shared production load.
The offered rate was 100/second for 30 seconds with four workers and ten tenants;
even that engineering ramp dropped requests and is not sustained acceptance.
Raw results: [baseline](2026-11-04_cycle16-paired-baseline.json),
[experiment](2026-11-04_cycle16-paired-optimized.json); corresponding `.log` files
retain the test output. Initial before/after results are also retained and must
not be mistaken for a proven gain.

## Remaining boundaries

Final-code regression scripts both exited successfully, including vet:

- [Scoring/runtime/atomicity](2026-11-04_cycle16-final-regression.jsonl):
  5 top-level tests and 85 subtests, race enabled, zero failures/skips.
- [Actual case workflow and PostgreSQL regression](2026-11-04_cycle16-final-workflow.jsonl):
  26 top-level tests and 44 subtests, race enabled, zero failures/skips.

The new stored-status regression proves that a rejected enqueue poisons its
transaction even when its immediate error is ignored, rolling back both effects
and receipts. The owner-plan helper returns support exact restore artifacts.

Real outbox destination/receipt contract, remaining configured action receivers,
complete shared-runtime acceptance and agreed staging resource allocation remain
unresolved. Production switch, provider fencing and capacity acceptance remain
open. The new restore evidence supports release preparation without ticking those
broader gates.
