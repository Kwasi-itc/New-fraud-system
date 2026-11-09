# Decision service production-readiness subtasks

Updated: 2026-11-04. Supporting breakdown for D20.12, D20.17 and D25.
These rows explain readiness work; they do not add leaves to the existing 152
subtask count or mark the broad acceptance parents complete. PostgreSQL stays active.

**Local scope: 9/9 checked. Deployed-system acceptance: 0/6 checked.**
Evidence: [cycle 17](../evidence/2026-11-04_decision_cycle17.md); these local results do not establish production readiness.

## Locally verifiable release checks

- [x] **PR01: Frozen release identity.** Reject changed artifact bytes, SQL,
  engine/source/image identities and unstamped deployment binaries.
- [x] **PR02: Earlier schema upgrade.** Rehearse the original 115-step prefix,
  then 140, 145 and the current decision plan; verify each stage.
- [x] **PR03: Interrupted migration recovery.** Recover dirty intent before DDL
  and after DDL; repeated apply must preserve data and reject drift.
- [x] **PR04: Target and owner protection.** Reject wrong writer UUID and an
  unlocked definer; preserve unrelated owner data and migration records.
- [x] **PR05: API/worker permission boundaries.** Exercise worker transitions;
  deny API dispatch forging, foreign writes, DDL, migration ledger changes and
  receipt rewrites with separate restricted accounts.
- [x] **PR06: Declared relationship inspection.** Reject orphan parents and
  cross-tenant composite references after checks are reenabled; allow nullable
  FK semantics and reject canceled/empty inspection.
- [x] **PR07: Restore relationship validation.** Run the inspector after a real
  logical restore, in addition to exact definitions, full counts/checksums and
  original migration-ledger checks.
- [x] **PR08: Receipt replay after restore.** Reconcile/redeliver the retained
  actual case receipt with restricted grants; prove no new effects or data changes.
- [x] **PR09: Reproducible runner.** Fail on required tests absent/skipped/failed;
  retain raw logs and clean both private servers without touching application data.

Run the isolated checks with:

```powershell
./backend/decision-engine-service/scripts/test-restore.ps1 -ReleaseChecks -EvidenceDirectory C:/temp/decision-readiness-new
```

## Staging and production acceptance still required

- [ ] **PR10: Complete shared-owner manifest.** Freeze every participating owner's
  exact migration definitions, source/image identity, FK dependencies and upgrade
  ordering; compare against actual deployed state. Current fixtures cover selected
  decision/identity/case objects, not the complete deployed stack.
- [ ] **PR11: Deployed grants and TLS.** Run the complete service flow using actual
  API, worker, inspection and owner identities; inspect effective grants and TLS,
  including unintended inherited/global privileges and foreign-owner denials.
- [ ] **PR12: Production-sized restore/PITR.** Measure restore duration and recovery
  point on representative backups; verify all business relationships, files/keys,
  credentials and receipts. Local checksums and declared FKs alone are insufficient.
- [ ] **PR13: Writer and delivery fencing.** Stop/drain old schedulers, workers,
  ingestion and API mutations; prove their credentials cannot resume writes/sends.
  Record in-flight work and reconcile receipts with real external receivers before
  enabling target consumers. Do not infer this from database flags alone.
- [ ] **PR14: Runtime and capacity acceptance.** Validate one shared database's
  replica/pool budget, real receiver wiring, scoring deferral and representative
  sustained 1,000 evaluations/sec. Target remains unmet.
- [ ] **PR15: Approved cutover and recovery window.** Retain immutable evidence,
  source snapshot and target journal; rehearse pre-write abort and post-write
  reconciliation/roll-forward. Obtain approval for the concrete deployment only
  after all required gates pass. No production activation is performed here.

The [cutover guide](../11_data_transfer_and_cutover.md) defines the coordinated
sequence. The [restore procedure](../../../backend/decision-engine-service/scripts/RESTORE_REHEARSAL.md)
describes the isolated implementation and its limits.
