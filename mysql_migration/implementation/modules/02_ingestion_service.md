# Ingestion, upload/deferred work and tenant reads

Status: **NEEDS_REVIEW** (design); implementation **IN_PROGRESS** (portable queue boundary and staged MySQL receipt coordinator; complete MySQL adapter pending). Primary task: IN-01; prerequisites: DM-01, DB-01, Q-01. Findings: PG-08/09/20/24/28/32/34.

Current work: ingestion planning, following the user's 2026-10-28 direction.
This does not complete DM-01 or authorize runtime activation. The
[implementation map](02_ingestion_implementation_map.md) records source behavior,
open compatibility decisions, owner coverage and acceptance requirements under
the existing IN-01 tasks. All services retain one logical application database.

## Verified execution path

[Source entry point](../../../backend/ingestion-service/internal/service/ingest_service.go#L112). Ingest/BatchIngest validate a model contract, enter a transaction, check receipt/hash/kind, write tenant records and persist audit/outbox/idempotency response. Upload/deferred creation also enqueues work transactionally. SQL model reader and HTTP model fallback plus aggregate endpoints are part of the migration. Full source occurrences are in the [audit catalog](../../11_source_evidence_catalog.md).

## Target implementation

Implement P-02: request-scoped idempotency reservation, published schema barrier check and narrow lookup-key guards. Preserve lookup-field conflict targeting, patch-only supplied columns, alternate uniqueness errors and all-or-nothing batch behavior. Keep audit/outbox/receipt/enqueue in the owner transaction. Remove success-through-noop queue wiring from the target design, with explicit startup/configuration failures. Typed aggregate builders retain count/sum/avg/min/max/count_distinct, null/filter behavior and tenant mapping.

Use the common [schema contract](../04_schema_and_data_access_design.md), [security requirements](../05_security_and_integrity_requirements.md) and [transaction protocols](../07_transaction_and_recovery_protocols.md). These are required acceptance inputs, not already implemented helpers.

## Implementation checklist

- [x] **IN-01.1 - COMPLETED:** Characterized source replay/hash/kind, failure-audit and upload/deferred failure/retry behavior with 29 PostgreSQL scenarios plus a five-table/53-column inventory test. Forced absent-receipt races, cancellation after writes, malformed/invalid worker inputs, exhaustion, missing jobs, terminal redelivery and completion gaps are verified. Five repeated receipt-race runs and module-wide vet/race regression passed (85 top-level tests, no skips/failures). Recorded target/legacy compatibility decisions and repository consumers in the [characterization contract](02_ingestion_characterization_contract.md). This completes source characterization only: source weaknesses are not fixed or approved for MySQL; target schema/implementation, running worker acceptance, live inventory and cutover remain under IN-01.2/3/4 and shared gates.
- [ ] **IN-01.2 - IN_PROGRESS:** Verified staged canonical receipts, keyed writes/readback, atomic record/audit/outbox composition, authorized shared-metadata binding, evidence-checked legacy translation, upload/deferred persistence, and shared fenced queue execution. Added source-bound installation lineage, restricted role grants and isolated write/work API/worker entry points with process acceptance. Remaining: independently reviewed deployment schema/grants, production API/worker adoption and full queue operational acceptance. Legacy inventory/transfer is not applicable to the user-confirmed fresh deployment. Runtime stays PostgreSQL/River; no generic any-unique record upsert.
- [ ] **IN-01.3 - IN_PROGRESS:** Reader/filter/aggregate implementation, isolated service/API adoption, query/result budgets and live PostgreSQL/MySQL parity are verified under the documented compatibility policies. Production rollout, source-locale certification and shared deployment gates remain open; PostgreSQL/River stays active.
- [ ] **IN-01.4 - IN_PROGRESS:** Complete shared-schema fixtures, restricted test principals, reusable source datasets, durable accounting and fail-closed discovery are verified. Isolated write/work/read/aggregate API and worker process adoption, repeated recovery/fault tests and bounded resource measurements are verified. Full live parity, shared operational gates and legacy reconciliation stay open.

## Granular delivery checklist

This is the current task tracker; dated progress notes below are historical evidence, not a second status source. Existing IDs `IN-01.2a/b/c` remain valid historical preparation tasks. The numeric child IDs below are new, stable IDs: do not renumber completed items.

Status rules: `[x] NOT_APPLICABLE` closes an item by explicit scope decision, not implementation or testing; report these separately from completed work. `[x] COMPLETE` means the stated scoped acceptance criterion has evidence; `[ ] NOT_STARTED` means that deliverable has not been completed or started in this tracker. Change it to `IN_PROGRESS` when work starts, or `BLOCKED` with the specific dependency. Parent completion requires every child and its external acceptance gates; component completion does not authorize runtime activation.

Each line states one deliverable and its **Done when** criterion. When ticking an item, append its source/test/report link and execution date. For the precompleted items, the source/test families listed under each group and the dated evidence links below are the initial evidence anchors.

Execution sequence: finish IN-01.2 schema/readiness contracts, implement the necessary IN-01.3 readers, wire isolated API/worker paths, then run IN-01.4 full-stack acceptance. Queue operations and legacy tooling can proceed independently once their contracts are fixed. Production activation remains a separate coordinated cutover decision.

### IN-01.2 - A. Canonical receipts and atomic writes

Existing staged adapter evidence: `receipt_command_test.go`, `receipts_test.go`, `receipt_commit_test.go`, `record_writer_test.go`, `record_owner_test.go`, `effects_test.go` under `internal/store/mysql/`; canonical domain tests under `internal/domain/ingestion/`. Completion here applies to that component scope.

- [x] **IN-01.2.001 - COMPLETE: Pin canonical request identity.** Done when: Golden tests bind tenant, stable table ID, mode, response kind and ordered raw input to version 1.
- [x] **IN-01.2.002 - COMPLETE: Bound canonical commands.** Done when: Invalid UTF-8, unsupported values, excessive bytes and invalid batch sizes are rejected before mutation.
- [x] **IN-01.2.003 - COMPLETE: Serialize absent receipt keys.** Done when: Concurrent identical requests produce one durable set of effects.
- [x] **IN-01.2.004 - COMPLETE: Distinguish digest collision from key equality.** Done when: Forced collisions compare complete key bytes and respect bucket bounds.
- [x] **IN-01.2.005 - COMPLETE: Reauthorize receipt replay.** Done when: Revoked or foreign-tenant access cannot retrieve a retained response.
- [x] **IN-01.2.006 - COMPLETE: Retain original responses.** Done when: Replay returns committed results without rerunning mutable model validation.
- [x] **IN-01.2.007 - COMPLETE: Coordinate publication and lookup locks.** Done when: Stale/closed publication gates fail; sorted exact-key guards preserve input/result order.
- [x] **IN-01.2.008 - COMPLETE: Preserve targeted write conflicts.** Done when: Lookup-key patch behavior is retained and alternate unique collisions remain errors; no generic any-unique upsert.
- [x] **IN-01.2.009 - COMPLETE: Commit successful effects atomically.** Done when: Record, success audit, outbox and receipt roll back together on a late failure.
- [x] **IN-01.2.010 - COMPLETE: Handle validation-audit failures explicitly.** Done when: Separate failure audits reauthorize and their persistence errors propagate.
- [x] **IN-01.2.011 - COMPLETE: Recover keyed writes after lost COMMIT acknowledgement.** Done when: Real lost-ack replay proves one physical effect and one retained audit/outbox set.

### IN-01.2 - B. Schema and least-privilege installation

Depends on DM-01 published metadata/v005 lookup contracts and DB-01. Staged source exists in `installation.go`, `installation_test.go`, `grants.go` and `work_grants_test.go`; independent review is still open.

- [x] **IN-01.2.012 - COMPLETE: Bind installation steps to shipped SQL.** Done when: Source substitutions and incomplete reference plans are rejected.
- [x] **IN-01.2.013 - COMPLETE: Track ingestion migration lineage separately.** Done when: Owner ledger checksums and object definitions detect drift in the shared database.
- [x] **IN-01.2.014 - COMPLETE: Define tenant-consistent schema relationships.** Done when: Staged manifest includes composite tenant/table and resource/job relationships.
- [x] **IN-01.2.015 - COMPLETE: Restrict mutable worker columns.** Done when: Restricted-account tests allow completion but deny rewriting inputs, receipts and audit evidence.
- [ ] **IN-01.2.016 - IN_PROGRESS: Freeze the deployment object manifest.** Done when: Review every ingestion table, column, index, FK and referenced data-model object against the source inventory. Inventory and differences recorded in [schema review](02_ingestion_schema_review.md); freeze awaits historical requested-at import, nullable historical payload and outbox delivery-state dispositions (2026-10-29).
- [x] **IN-01.2.016a - COMPLETE: Preserve receipt creation time.** Done when: New receipts use database time, replay preserves it, and legacy imports retain source microseconds/timezone instants while rejecting missing or unrepresentable timestamps. Evidence: [timestamp and full MySQL regression](../evidence/2026-10-29_ingestion-timestamps.log), 2026-10-29.
- [x] **IN-01.2.016b - COMPLETE: Preserve latest work-attempt start time.** Done when: Claim records the start, heartbeat/retry retain it, a new claim replaces it, and authorized status reads expose it for running/terminal work under restricted worker grants; queued retries expose null to preserve the source DTO contract. Evidence: [final worker/status regression](../evidence/2026-10-29_ingestion-timestamp-status.log), 2026-10-29.
- [x] **IN-01.2.017 - COMPLETE: Generate pinned-server reference artifacts.** Done when: Save expected definitions from an isolated approved MySQL version with source revision and generation command. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [ ] **IN-01.2.018 - BLOCKED: Record independent schema review.** Done when: Reviewer and decision are recorded for the manifest and reference artifacts; test-generated definitions alone do not close this item. Blocked 2026-10-30: Independent reviewer identity and schema/grants decision have not been supplied; self-review cannot close this gate.
- [x] **IN-01.2.019 - COMPLETE: Review production principal separation.** Done when: Document installer, API, worker, reader and legacy-import privileges, including physical-table grant ownership. Evidence: [principal separation review](02_ingestion_schema_review.md#principal-separation-review-in-012019), 2026-10-29; production grant installation/acceptance stays separate.
- [x] **IN-01.2.020 - COMPLETE: Validate complete installation ordering.** Done when: Fresh installation succeeds after actual DM-01 migrations, including v005, without fixture-only metadata shortcuts. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.2.021 - COMPLETE: Exercise interrupted DDL recovery.** Done when: Stop after each migration boundary; resume or reject dirty state according to documented recovery rules. Evidence: [54 durable-state boundary cases](../evidence/2026-10-29_ingestion-ddl-boundaries.log), 2026-10-29; full-stack process-kill acceptance remains separate.
- [x] **IN-01.2.022 - COMPLETE: Verify role grants against complete installation.** Done when: Required operations pass and cross-owner writes, private dictionary reads and DDL fail for each runtime principal. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.2.023 - COMPLETE: Implement installation/readiness checks.** Done when: Startup checks required schema lineage, lookup routine and grants, and rejects missing or incompatible capabilities. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.

### IN-01.2 - C. Authorized API and worker adoption

Depends on section B readiness and the required IN-01.3 readers. Construction in an isolated runtime precedes any activation decision.

- [x] **IN-01.2.024 - COMPLETE: Bind owner authorization to shared metadata.** Done when: Token and tenant/table status checks execute on initial calls and replay without network work under locks.
- [x] **IN-01.2.025 - COMPLETE: Separate public revision and physical version.** Done when: Published-model results retain the public revision while writes check the registry publication UUID.
- [x] **IN-01.2.026 - COMPLETE: Resolve route names to stable table IDs.** Done when: Use an authorized bounded/indexed owner contract; reject foreign, archived, missing and recreated-table mismatches. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.2.027 - COMPLETE: Wire single-record create.** Done when: Existing POST transport calls the staged owner with trusted scope and preserves its response DTO. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.2.028 - COMPLETE: Wire single-record patch.** Done when: Existing PATCH transport preserves supplied-field semantics and action/result mapping. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.2.029 - COMPLETE: Wire batch create.** Done when: Existing batch POST preserves input order, batch limits and all-or-nothing behavior. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.2.030 - COMPLETE: Wire batch patch.** Done when: Existing batch PATCH preserves per-record field omission and ordered results. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.2.031 - COMPLETE: Map typed owner errors to HTTP.** Done when: Pin status/body behavior for validation, uniqueness, receipt conflict, authorization, limits and unavailable dependencies. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.2.032 - COMPLETE: Define unkeyed synchronous request behavior.** Done when: Document execution identity and ambiguous-commit response/retry policy without inventing caller idempotency. Evidence: [operation contract](02_ingestion_operation_contracts.md), 2026-10-29; definition complete, production adoption remains separate.
- [x] **IN-01.2.033 - COMPLETE: Wire upload submission.** Done when: Authorized CSV submission commits the resource and job through WorkOwner and returns the expected API DTO. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.2.034 - COMPLETE: Wire deferred submission.** Done when: Authorized deferred submission preserves optional public key and schedule, with atomic resource/job creation. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.2.035 - COMPLETE: Implement upload list projection.** Done when: Tenant-scoped pagination returns bounded status metadata without loading raw payloads. Evidence: [projection, isolation and query plans](../evidence/2026-10-29_ingestion-upload-list.log), [index installation/recovery](../evidence/2026-10-29_ingestion-upload-index-lineage.log), 2026-10-29. Staged repository only; HTTP/read-policy wiring remains separate.
- [x] **IN-01.2.036 - COMPLETE: Wire upload and deferred detail reads.** Done when: Authorized detail endpoints map attempts, counters, errors and timestamps to existing DTOs. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.2.037 - COMPLETE: Implement deferred operational metrics.** Done when: Authorized bounded/indexed queries reconcile pending, running and terminal counts. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.2.038 - COMPLETE: Construct the MySQL worker entry point.** Done when: Configured queues, limits, owner credentials and handlers are required; missing wiring fails startup. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.2.039 - COMPLETE: Add isolated runtime selection/configuration.** Done when: Server and worker select one database authority with explicit configuration; incomplete MySQL capabilities fail closed. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.2.040 - COMPLETE: Verify downstream event contracts.** Done when: Document actual ingestion outbox consumers and confirm event identity/schema and delivery ownership. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).

### IN-01.2 - D. Work persistence and queue operations

Reuse `backend/mysqlstore/work`, not a second ingestion-private queue. Component evidence is in `work_owner_test.go`, `work_grants_test.go` and `work_recovery_test.go`. Operational items depend on Q-01.

- [x] **IN-01.2.041 - COMPLETE: Create resources and jobs atomically.** Done when: Enqueue failure rolls back resource creation and stable execution IDs reconcile duplicate creation.
- [x] **IN-01.2.042 - COMPLETE: Isolate internal execution receipts.** Done when: Upload/unkeyed deferred identities cannot collide with public caller receipt keys.
- [x] **IN-01.2.043 - COMPLETE: Fence work before business locks.** Done when: Only the live tenant-scoped lease can commit resource and ingestion effects.
- [x] **IN-01.2.044 - COMPLETE: Complete work in the effects transaction.** Done when: Record/audit/outbox/receipt and durable job/resource completion share the commit.
- [x] **IN-01.2.045 - COMPLETE: Run bounded concurrent claims.** Done when: SKIP LOCKED claims and worker concurrency limits avoid duplicate active claims.
- [x] **IN-01.2.046 - COMPLETE: Recover failed and expired attempts.** Done when: Heartbeat, retry exhaustion, expiry recovery and stale-worker rejection pass component tests.
- [x] **IN-01.2.047 - COMPLETE: Reject no-op handler success.** Done when: A handler returning without terminal job state produces an observable failure.
- [x] **IN-01.2.048 - COMPLETE: Specify operator action authorization.** Done when: Document who may inspect, cancel or redrive work and how trusted actor attribution is retained. Evidence: [operation contract](02_ingestion_operation_contracts.md), 2026-10-29; definition complete, production adoption remains separate.
- [x] **IN-01.2.049 - COMPLETE: Implement bounded queue inspection.** Done when: Tenant/queue/state filters paginate with reviewed indexes and exclude raw sensitive payloads. Evidence: five subtasks below; staged adapter complete, production policy/endpoint/access-audit binding remains open.
- [x] **IN-01.2.049a - COMPLETE: Enforce inspection authorization and bounds.** Done when: A separate current tenant-wide inspect policy is mandatory; exact queue/state, page bounds and scoped cursors are validated; denied/revoked calls return no data. Evidence: [race-enabled inspection tests](../evidence/2026-10-29_ingestion-inspection.log), [contract](02_ingestion_operation_contracts.md#bounded-queue-inspection-in-012049a-e), 2026-10-29.
- [x] **IN-01.2.049b - COMPLETE: Implement safe queue metadata projection.** Done when: One read-only bounded query returns identity, attempt and timestamp metadata without payloads, keys, tokens, actor evidence or free-text errors. Evidence: [race-enabled inspection tests](../evidence/2026-10-29_ingestion-inspection.log), [contract](02_ingestion_operation_contracts.md#bounded-queue-inspection-in-012049a-e), 2026-10-29.
- [x] **IN-01.2.049c - COMPLETE: Implement stable inspection pagination.** Done when: Creation-time/UUID keyset pages preserve ordering and cover tied timestamps without duplicates on fixed state; live-state behavior is documented and concurrent reads tested. Evidence: [race-enabled inspection tests](../evidence/2026-10-29_ingestion-inspection.log), [contract](02_ingestion_operation_contracts.md#bounded-queue-inspection-in-012049a-e), 2026-10-29.
- [x] **IN-01.2.049d - COMPLETE: Restrict inspection database privileges.** Done when: A dedicated column-only reader passes inspection and cannot read sensitive columns or mutate queue state; policy metadata grants remain explicit. Evidence: [race-enabled inspection tests](../evidence/2026-10-29_ingestion-inspection.log), [contract](02_ingestion_operation_contracts.md#bounded-queue-inspection-in-012049a-e), 2026-10-29.
- [x] **IN-01.2.049e - COMPLETE: Verify inspection index and installation lineage.** Done when: First/continuation EXPLAIN plans use the tenant/queue/state/order index on mixed history without filesort; appended migration passes lineage and recovery checks. Evidence: [plans](../evidence/2026-10-29_ingestion-inspection.log), [20-step lineage and 60 recovery cases](../evidence/2026-10-29_ingestion-inspection-lineage.log), 2026-10-29.

- [x] **IN-01.2.050 - COMPLETE: Implement fenced cancellation.** Done when: Cancellation prevents later stale commits and records a durable authorized terminal outcome. Evidence: WorkOperator.Cancel and shared Queue.CancelTx; [cancellation/grant tests](../evidence/2026-10-29_ingestion-operator-cancel.log), [lost COMMIT replay](../evidence/2026-10-29_ingestion-operator-commit-loss.log), [19-step lineage/recovery](../evidence/2026-10-29_ingestion-operator-lineage.log). Staged adapter only; production policy/endpoint binding remains open.
- [x] **IN-01.2.051 - COMPLETE: Define redrive identity and receipt semantics.** Done when: Specify retryable terminal states, attempt lineage and existing receipt handling before implementing redrive. Evidence: [operation contract](02_ingestion_operation_contracts.md), 2026-10-29; definition complete, production adoption remains separate.
- [x] **IN-01.2.052 - COMPLETE: Implement authorized redrive.** Done when: A new attempt preserves immutable history and prevents duplicate active reservations/effects. Evidence: resource-preserving WorkOperator.Redrive, [contract and limitations](02_ingestion_operation_contracts.md#resource-preserving-redrive-implementation-in-012052), [13-test full suite](../evidence/2026-10-29_ingestion-redrive/accepted/summary.json), [source/verification record](../evidence/2026-10-29_ingestion-redrive/provenance.json), 2026-10-29. Staged adapter only; runtime/operator policy and independent deployment review remain open.
- [x] **IN-01.2.053 - COMPLETE: Define retention dependencies.** Done when: Specify retention order for jobs, resources, reservations, receipts, audits and outbox without erasing replay protection. Evidence: [operation contract](02_ingestion_operation_contracts.md), 2026-10-29; definition complete, production adoption remains separate.
- [ ] **IN-01.2.054 - BLOCKED: Implement bounded retention tooling.** Done when: Eligible rows are processed in bounded batches; dry-run reports and ownership checks precede deletion. Blocked 2026-10-30: Reviewed replay/redrive horizons, holds, delivery obligations and permitted archival/deletion scope are missing; current retention contract defines no default deletion set.
- [x] **IN-01.2.055 - COMPLETE: Add queue operational telemetry.** Done when: Expose claim failures, retries, expirations, queue age, processing time and pool pressure without payload leakage. Evidence: five subtasks below; shared runner snapshots complete. Queue age is ready-to-claim wait, not oldest backlog age. Production authorized export remains open.
- [x] **IN-01.2.055a - COMPLETE: Add bounded concurrent telemetry snapshots.** Done when: Fixed counters and histogram buckets retain no samples, identities or raw errors; snapshots are immutable and race-tested. Evidence: [telemetry and worker regressions](../evidence/2026-10-29_ingestion-telemetry.log), [final telemetry checks](../evidence/2026-10-29_ingestion-telemetry-final.log), [semantics](02_ingestion_operation_contracts.md#shared-runner-telemetry-in-012055a-e), 2026-10-29.
- [x] **IN-01.2.055b - COMPLETE: Instrument claim and heartbeat outcomes.** Done when: Actual runner calls/failures and acknowledged claims are counted without changing fencing or retry behavior. Evidence: [telemetry and worker regressions](../evidence/2026-10-29_ingestion-telemetry.log), [final telemetry checks](../evidence/2026-10-29_ingestion-telemetry-final.log), [semantics](02_ingestion_operation_contracts.md#shared-runner-telemetry-in-012055a-e), 2026-10-29.
- [x] **IN-01.2.055c - COMPLETE: Instrument retry and expiry outcomes.** Done when: Confirmed retry/exhaustion/expiry counts are distinct from errors and stale-lease rejections; rollback does not increment confirmed counts. Evidence: [telemetry and worker regressions](../evidence/2026-10-29_ingestion-telemetry.log), [final telemetry checks](../evidence/2026-10-29_ingestion-telemetry-final.log), [semantics](02_ingestion_operation_contracts.md#shared-runner-telemetry-in-012055a-e), 2026-10-29.
- [x] **IN-01.2.055d - COMPLETE: Measure queue wait and handler duration.** Done when: Queue wait uses database time and bounded conversion; fixed duration buckets and active-handler counts cover processing without retaining individual jobs. Evidence: [telemetry and worker regressions](../evidence/2026-10-29_ingestion-telemetry.log), [final telemetry checks](../evidence/2026-10-29_ingestion-telemetry-final.log), [semantics](02_ingestion_operation_contracts.md#shared-runner-telemetry-in-012055a-e), 2026-10-29.
- [x] **IN-01.2.055e - COMPLETE: Expose pool pressure and verify integration.** Done when: Memory-only snapshots include DB pool utilization/wait statistics; isolated contention, restricted grants and existing worker regressions pass; production export gaps are documented. Evidence: [telemetry and worker regressions](../evidence/2026-10-29_ingestion-telemetry.log), [final telemetry checks](../evidence/2026-10-29_ingestion-telemetry-final.log), [semantics](02_ingestion_operation_contracts.md#shared-runner-telemetry-in-012055a-e), 2026-10-29.

- [x] **IN-01.2.056 - COMPLETE: Verify deployed worker shutdown.** Done when: Stop claims, cancel/join handlers and prove unfinished leases recover under the configured shutdown budget. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [ ] **IN-01.2.057 - BLOCKED: Obtain Q-01 operational acceptance.** Done when: Link shared queue acceptance evidence; ingestion component tests alone cannot close the shared gate. Blocked 2026-10-30: Shared Q-01 acceptance evidence has not been supplied; ingestion-only tests do not satisfy the gate.

### IN-01.2 - E. Legacy receipt and pending-work migration

The translator is implemented in `legacy_import.go`; trusted source export, bounded operator tooling and real inventory reconciliation remain separate.

- [x] **IN-01.2.058 - COMPLETE: Translate legacy single and batch identities.** Done when: Original raw input plus historical schema reproduce the distinct legacy SHA-1 rules.
- [x] **IN-01.2.059 - COMPLETE: Validate source attribution.** Done when: Tenant, table, mode, successful audit and original result evidence must agree; missing evidence fails closed.
- [x] **IN-01.2.060 - COMPLETE: Import receipt and provenance atomically.** Done when: Authorized import retains the original semantic result without repeating physical/audit/outbox effects.
- [x] **IN-01.2.061 - COMPLETE: Require separate operator authorization.** Done when: LegacyImporter requires an explicit operator policy in addition to normal owner authorization.
- [x] **IN-01.2.062 - NOT_APPLICABLE: Inventory retained legacy receipts.** Closed by scope clarification 2026-10-30: User confirmed "new system, no existing data." No legacy source or pending work requires migration in this deployment. This is a scope closure, not executed migration evidence; reopen if existing data/work is introduced. See [fresh-deployment scope](#fresh-deployment-scope-2026-10-30).
- [x] **IN-01.2.063 - NOT_APPLICABLE: Classify untranslatable records.** Closed by scope clarification 2026-10-30: User confirmed "new system, no existing data." No legacy source or pending work requires migration in this deployment. This is a scope closure, not executed migration evidence; reopen if existing data/work is introduced. See [fresh-deployment scope](#fresh-deployment-scope-2026-10-30).
- [x] **IN-01.2.064 - NOT_APPLICABLE: Build bounded trusted export.** Closed by scope clarification 2026-10-30: User confirmed "new system, no existing data." No legacy source or pending work requires migration in this deployment. This is a scope closure, not executed migration evidence; reopen if existing data/work is introduced. See [fresh-deployment scope](#fresh-deployment-scope-2026-10-30).
- [x] **IN-01.2.065 - COMPLETE: Bound complete import evidence.** Done when: Limit aggregate model/audit/provenance size before large allocations; oversized evidence yields a clear error. Evidence: [bounded evidence and translation regression](../evidence/2026-10-29_ingestion-legacy-bounds.log), 2026-10-29; conservative aggregate guard runs before decoding/hash/provenance allocations. Export/CLI must separately bound input before constructing the import object.
- [x] **IN-01.2.066 - COMPLETE: Build an operator dry-run command.** Done when: Validate evidence and report would-import/conflict/rejection counts without writes. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.2.067 - COMPLETE: Build a resumable operator import command.** Done when: Restricted operator credentials, checkpoints and committed-result reconciliation permit restart after interruption. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.2.068 - NOT_APPLICABLE: Reconcile translated receipts.** Closed by scope clarification 2026-10-30: User confirmed "new system, no existing data." No legacy source or pending work requires migration in this deployment. This is a scope closure, not executed migration evidence; reopen if existing data/work is introduced. See [fresh-deployment scope](#fresh-deployment-scope-2026-10-30).
- [x] **IN-01.2.069 - NOT_APPLICABLE: Inventory pending upload/deferred work.** Closed by scope clarification 2026-10-30: User confirmed "new system, no existing data." No legacy source or pending work requires migration in this deployment. This is a scope closure, not executed migration evidence; reopen if existing data/work is introduced. See [fresh-deployment scope](#fresh-deployment-scope-2026-10-30).
- [x] **IN-01.2.070 - NOT_APPLICABLE: Rehearse pending-work transfer.** Closed by scope clarification 2026-10-30: User confirmed "new system, no existing data." No legacy source or pending work requires migration in this deployment. This is a scope closure, not executed migration evidence; reopen if existing data/work is introduced. See [fresh-deployment scope](#fresh-deployment-scope-2026-10-30).

### IN-01.3 - A. Contract readers and schema coordination

Depends on DM-01 consumer contracts. Reuse the existing decoder and staged metadata binding; the production SQL/HTTP reader port is not complete.

- [x] **IN-01.3.001 - COMPLETE: Map contract-reader consumers.** Done when: List each SQL/full-model HTTP caller and its required table/version/status semantics. Evidence: [reader contract/consumer map](02_ingestion_reader_contract.md), [race regression](../evidence/2026-10-29_ingestion-reader-batch.log), 2026-10-29. Portable client scope; concrete MySQL store and runtime adoption remain open.
- [x] **IN-01.3.002 - COMPLETE: Define the supported MySQL read contract.** Done when: Pin schema-qualified objects, projections, tenant keys and published-version fields with the data-model owner. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.3.003 - COMPLETE: Implement MySQL table-contract lookup.** Done when: Use stable tenant/table identity and bounded payload projection; do not scan arbitrary metadata subsets. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.3.004 - COMPLETE: Port explicit HTTP read mode.** Done when: Preserve credentials, timeout and error behavior for supported full-model consumers. Evidence: [reader contract/consumer map](02_ingestion_reader_contract.md), [race regression](../evidence/2026-10-29_ingestion-reader-batch.log), 2026-10-29. Portable client scope; concrete MySQL store and runtime adoption remain open.
- [x] **IN-01.3.005 - COMPLETE: Prevent fallback bypass.** Done when: A SQL error, closed gate or failed authorization never silently switches to HTTP. Evidence: [reader contract/consumer map](02_ingestion_reader_contract.md), [race regression](../evidence/2026-10-29_ingestion-reader-batch.log), 2026-10-29. Portable client scope; concrete MySQL store and runtime adoption remain open.
- [x] **IN-01.3.006 - COMPLETE: Port versioned cache keys.** Done when: Tenant, stable table identity and publication version prevent cross-tenant or recreated-table cache reuse. Evidence: [reader contract/consumer map](02_ingestion_reader_contract.md), [race regression](../evidence/2026-10-29_ingestion-reader-batch.log), 2026-10-29. Portable client scope; concrete MySQL store and runtime adoption remain open.
- [x] **IN-01.3.007 - COMPLETE: Bound cache resources.** Done when: Document/test entry, byte and lifetime limits and concurrent access behavior. Evidence: [reader contract/consumer map](02_ingestion_reader_contract.md), [race regression](../evidence/2026-10-29_ingestion-reader-batch.log), 2026-10-29. Portable client scope; concrete MySQL store and runtime adoption remain open.
- [x] **IN-01.3.008 - COMPLETE: Coordinate reads with schema publication.** Done when: Define and implement version/barrier validation through query execution; stale mappings fail safely. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.3.009 - COMPLETE: Handle schema transitions.** Done when: Archive, rename, recreation and closed-publication states return the agreed contract result. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.

### IN-01.3 - B. Record projection and filters

Depends on section A mapping/readiness. Port verified source behavior; do not add unsupported operators. The [source semantics audit](02_ingestion_query_semantics.md) records missing temporal/cursor support and compatibility decisions for wildcard escaping, node budgets and error mapping.

- [x] **IN-01.3.010 - COMPLETE: Inventory source query semantics.** Done when: List supported projections, filters, ordering, temporal behavior, null handling and errors from source/tests. Evidence: [source semantics audit](02_ingestion_query_semantics.md), five audit subtasks below; no MySQL reader implementation claimed.
- [x] **IN-01.3.010a - COMPLETE: Inventory projection and type boundaries.** Done when: Trace lookup/projection/row decoding and six logical types, distinguishing driver-dependent behavior. Evidence: [audit A](02_ingestion_query_semantics.md#a-projections-identities-and-types-in-013010a).
- [x] **IN-01.3.010b - COMPLETE: Inventory scalar, string, membership and null predicates.** Done when: List exact supported operators, parameters, null/list/wildcard edges and unsupported features. Evidence: [audit B](02_ingestion_query_semantics.md#b-scalar-string-collection-and-null-predicates-in-013010b), source characterization tests.
- [x] **IN-01.3.010c - COMPLETE: Characterize logical groups and validation limits.** Done when: Pin nesting, empty groups, NOT arity, parameter order and actual limit behavior. Evidence: [audit C](02_ingestion_query_semantics.md#c-logical-groups-and-validation-limits-in-013010c), repeated-node-count characterization.
- [x] **IN-01.3.010d - COMPLETE: Inventory temporal and pagination behavior.** Done when: Identify actual predicates, ordering/ties, limit defaults and bounds; explicitly mark absent temporal/cursor support. Evidence: [audit D](02_ingestion_query_semantics.md#d-temporal-reads-ordering-and-pagination-in-013010d), captured source query shapes.
- [x] **IN-01.3.010e - COMPLETE: Inventory aggregate, error and execution contracts.** Done when: Pin expressions, validation layers, response/error behavior, concurrency/timeouts and remaining database parity tests. Evidence: [audit E](02_ingestion_query_semantics.md#e-aggregates-errors-and-execution-controls-in-013010e), [test log](../evidence/2026-10-29_ingestion-source-read-contract.log); PostgreSQL integration cases skipped without isolated test URL.

- [x] **IN-01.3.011 - COMPLETE: Resolve projection identifiers safely.** Done when: Only published mapped table/column identifiers can enter SQL; unknown and forbidden fields fail. Evidence: [projection contract](02_ingestion_record_projection.md), [race-enabled projection tests](../evidence/2026-10-29_ingestion-record-projection.log), 2026-10-29. Transaction-local component; read-policy, byte-budget and HTTP integration remain open.
- [x] **IN-01.3.011a - COMPLETE: Validate requested projection fields.** Done when: Require 1..100 distinct exact logical fields; unknown, archived, mismatched and unmodeled system fields fail before registry access. Evidence: [projection contract](02_ingestion_record_projection.md), [race-enabled projection tests](../evidence/2026-10-29_ingestion-record-projection.log), 2026-10-29. Transaction-local component; read-policy, byte-budget and HTTP integration remain open.
- [x] **IN-01.3.011b - COMPLETE: Bind projection to published mappings.** Done when: Tenant/table/version and complete field-ID/type/component consistency use the existing registry barrier; stale, closed and inconsistent mappings fail. Evidence: [projection contract](02_ingestion_record_projection.md), [race-enabled projection tests](../evidence/2026-10-29_ingestion-record-projection.log), 2026-10-29. Transaction-local component; read-policy, byte-budget and HTTP integration remain open.
- [x] **IN-01.3.011c - COMPLETE: Compile physical columns safely.** Done when: Only validated physical identifiers enter SQL; logical aliases never enter SQL; IP component order is fixed and no SELECT * is generated. Evidence: [projection contract](02_ingestion_record_projection.md), [race-enabled projection tests](../evidence/2026-10-29_ingestion-record-projection.log), 2026-10-29. Transaction-local component; read-policy, byte-budget and HTTP integration remain open.
- [x] **IN-01.3.011d - COMPLETE: Decode explicit projections atomically.** Done when: Copied logical names survive caller mutation; typed values follow request order/component counts; malformed input returns no partial record. Evidence: [projection contract](02_ingestion_record_projection.md), [race-enabled projection tests](../evidence/2026-10-29_ingestion-record-projection.log), 2026-10-29. Transaction-local component; read-policy, byte-budget and HTTP integration remain open.
- [x] **IN-01.3.011e - COMPLETE: Verify mapped readback and barrier lifetime.** Done when: Isolated physical readback covers all six types; publication preparation cannot cross the held transaction; authority/transfer-limit integration gaps are documented. Evidence: [projection contract](02_ingestion_record_projection.md), [race-enabled projection tests](../evidence/2026-10-29_ingestion-record-projection.log), 2026-10-29. Transaction-local component; read-policy, byte-budget and HTTP integration remain open.

- [x] **IN-01.3.012 - COMPLETE: Implement typed record decoding.** Done when: All six supported source types round-trip, including nulls and precision/time boundaries. Evidence: [decoder contract](02_ingestion_record_decoder.md), [five passing decoder tests](../evidence/2026-10-29_ingestion-record-decoder.log), 2026-10-29. Component scope; mapped queries and HTTP compatibility remain open.
- [x] **IN-01.3.012a - COMPLETE: Decode booleans and signed integers.** Done when: Only 0/1 boolean storage and signed INT boundaries decode; wrong types and overflow fail without a value. Evidence: [decoder contract](02_ingestion_record_decoder.md), [five passing decoder tests](../evidence/2026-10-29_ingestion-record-decoder.log), 2026-10-29. Component scope; mapped queries and HTTP compatibility remain open.
- [x] **IN-01.3.012b - COMPLETE: Decode finite floating-point values.** Done when: Finite DOUBLE values retain binary64 precision; malformed, NaN and infinite inputs fail. Evidence: [decoder contract](02_ingestion_record_decoder.md), [five passing decoder tests](../evidence/2026-10-29_ingestion-record-decoder.log), 2026-10-29. Component scope; mapped queries and HTTP compatibility remain open.
- [x] **IN-01.3.012c - COMPLETE: Bound strings and preserve SQL nulls.** Done when: UTF-8 and byte caps are enforced before string copying; returned strings do not alias driver buffers; nullability and component arity are checked for every type. Evidence: [decoder contract](02_ingestion_record_decoder.md), [five passing decoder tests](../evidence/2026-10-29_ingestion-record-decoder.log), 2026-10-29. Component scope; mapped queries and HTTP compatibility remain open.
- [x] **IN-01.3.012d - COMPLETE: Decode UTC microsecond timestamps.** Done when: Shared DATETIME(6) range/precision rules are reused; UTC instants round-trip and zero, unparsed or excessive-precision values fail. Evidence: [decoder contract](02_ingestion_record_decoder.md), [five passing decoder tests](../evidence/2026-10-29_ingestion-record-decoder.log), 2026-10-29. Component scope; mapped queries and HTTP compatibility remain open.
- [x] **IN-01.3.012e - COMPLETE: Decode IP components and verify database round trips.** Done when: Family/address/prefix consistency and all-or-none nulls are enforced, host bits/mapped IPv6 survive, and all six writer encodings round-trip through isolated MySQL including nulls/boundaries. Evidence: [decoder contract](02_ingestion_record_decoder.md), [five passing decoder tests](../evidence/2026-10-29_ingestion-record-decoder.log), 2026-10-29. Component scope; mapped queries and HTTP compatibility remain open.

- [x] **IN-01.3.013 - COMPLETE: Port exact record lookup.** All component and service/API adoption children verified in the isolated runtime; production activation remains gated. [Acceptance](02_ingestion_operations_batch20.md), 2026-10-30.
- [x] **IN-01.3.013a - COMPLETE: Bind the read to scoped publication metadata.** Done when: Validate tenant/table/version and requested physical fields in the caller transaction under the publication barrier. Evidence: [exact-read contract](02_ingestion_exact_read.md), [tests](../evidence/2026-10-29_ingestion-exact-read.log), 2026-10-29.
- [x] **IN-01.3.013b - COMPLETE: Implement parameterized full-byte lookup.** Done when: Resolve the scoped retained key and binary object identity in one SELECT with LIMIT 1; no dictionary allocation or mutation. Evidence: [exact-read contract](02_ingestion_exact_read.md), [tests](../evidence/2026-10-29_ingestion-exact-read.log), 2026-10-29.
- [x] **IN-01.3.013c - COMPLETE: Enforce bounded projected decoding.** Done when: Apply SQL string guards and field/row/page budgets; return only typed requested fields with no partial result. Evidence: [exact-read contract](02_ingestion_exact_read.md), [tests](../evidence/2026-10-29_ingestion-exact-read.log), 2026-10-29.
- [x] **IN-01.3.013d - COMPLETE: Preserve missing and failure distinctions.** Done when: Return sql.ErrNoRows only for an empty result; propagate cancellation, mapping, decoding and database errors. Evidence: [exact-read contract](02_ingestion_exact_read.md), [tests](../evidence/2026-10-29_ingestion-exact-read.log), 2026-10-29.
- [x] **IN-01.3.013e - COMPLETE: Verify isolated exact-read cases.** Done when: Race-enabled MySQL tests cover byte identity, tenant/table/version rejection, bounds and failure propagation. Evidence: [exact-read contract](02_ingestion_exact_read.md), [tests](../evidence/2026-10-29_ingestion-exact-read.log), 2026-10-29.
- [x] **IN-01.3.013f - COMPLETE: Adopt the production exact-read contract.** Done when: Authorized service/API adapters supply scoped metadata, a READ COMMITTED transaction and approved bounds; missing-record HTTP behavior and field shape match the agreed contract; restricted runtime grants and concurrent ingestion behavior are integration-tested. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.3.014 - COMPLETE: Port current/temporal row predicates.** Done when: Preserve source validity-time selection at exact boundaries. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.3.015 - COMPLETE: Port scalar filter predicates.** All scoped children verified, including isolated service/API adoption and live source parity. [Compatibility limits and evidence](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30), 2026-10-30. Deployment acceptance remains separate.
- [x] **IN-01.3.015a - COMPLETE: Bind scalar predicates to validated mappings.** Done when: Only active selected single-column fields compile; logical names and operator strings cannot become SQL syntax. Evidence: [scalar contract](02_ingestion_scalar_predicates.md), [tests](../evidence/2026-10-29_ingestion-scalar-predicates.log), 2026-10-29.
- [x] **IN-01.3.015b - COMPLETE: Compile normalized numeric comparisons.** Done when: eq/neq/gt/gte/lt/lte bind INT32-range int64 and finite float64; real MySQL truth tables and boundaries pass. Evidence: [scalar contract](02_ingestion_scalar_predicates.md), [tests](../evidence/2026-10-29_ingestion-scalar-predicates.log), 2026-10-29.
- [x] **IN-01.3.015c - COMPLETE: Compile Boolean and timestamp comparisons.** Done when: All six operators preserve Boolean ordering and microsecond timestamp boundaries; timezone conversion is explicit and invalid dates fail. Evidence: [scalar contract](02_ingestion_scalar_predicates.md), [tests](../evidence/2026-10-29_ingestion-scalar-predicates.log), 2026-10-29.
- [x] **IN-01.3.015d - COMPLETE: Preserve SQL null semantics.** Done when: nil comparisons remain bound NULL; explicit null checks ignore Value and distinguish nullable versus required rows. Evidence: [scalar contract](02_ingestion_scalar_predicates.md), [tests](../evidence/2026-10-29_ingestion-scalar-predicates.log), 2026-10-29.
- [x] **IN-01.3.015e - COMPLETE: Verify rejected inputs and integration behavior.** Done when: Race-enabled MySQL tests reject coercion/unsupported operators without partial fragments; detached arguments and existing reader regressions pass. Evidence: [scalar contract](02_ingestion_scalar_predicates.md), [tests](../evidence/2026-10-29_ingestion-scalar-predicates.log), 2026-10-29.
- [x] **IN-01.3.015f - COMPLETE: Reconcile source compatibility and adopt scalar filters.** Done when: Raw API value normalization/error behavior is explicitly reviewed, supported inputs pass live PostgreSQL/MySQL parity tests, and authorized query/aggregate paths integrate the compiler with total parameter budgets, bounded results and publication lifetime guarantees. String/IP semantics remain separately unresolved. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.3.016 - COMPLETE: Port membership predicates.** All scoped children verified, including isolated service/API adoption and live source parity. [Compatibility limits and evidence](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30), 2026-10-30. Deployment acceptance remains separate.
- [x] **IN-01.3.016a - COMPLETE: Compile mapped membership with owned parameters.** Done when: Use validated single-column mappings and placeholders; preserve list order/duplicates and detach input/output argument slices. Evidence: [membership contract](02_ingestion_membership.md), [race-enabled tests](../evidence/2026-10-29_ingestion-membership.log), 2026-10-29.
- [x] **IN-01.3.016b - COMPLETE: Verify normalized typed membership.** Done when: Integer, float, Boolean and timestamp lists produce expected real MySQL matches through bounded result collection. Evidence: [membership contract](02_ingestion_membership.md), [race-enabled tests](../evidence/2026-10-29_ingestion-membership.log), 2026-10-29.
- [x] **IN-01.3.016c - COMPLETE: Preserve NULL membership semantics.** Done when: Mixed/all-NULL lists retain SQL UNKNOWN, including under negation; required-field NULL membership does not become a write validation error. Evidence: [membership contract](02_ingestion_membership.md), [race-enabled tests](../evidence/2026-10-29_ingestion-membership.log), 2026-10-29.
- [x] **IN-01.3.016d - COMPLETE: Enforce list bounds at compilation.** Done when: Accept and execute 1 and 100 items; reject empty/nil, wrong container types and more than 100 before item iteration/allocation. Evidence: [membership contract](02_ingestion_membership.md), [race-enabled tests](../evidence/2026-10-29_ingestion-membership.log), 2026-10-29.
- [x] **IN-01.3.016e - COMPLETE: Reject invalid members without partial output.** Done when: Wrong types, overflow/non-finite values, bad timestamps, unknown/unsupported fields and an invalid final member return no fragment; scalar regressions pass. Evidence: [membership contract](02_ingestion_membership.md), [race-enabled tests](../evidence/2026-10-29_ingestion-membership.log), 2026-10-29.
- [x] **IN-01.3.016f - COMPLETE: Complete membership compatibility and runtime adoption.** Done when: Resolve string/IP coverage and raw-input normalization/errors, verify live PostgreSQL/MySQL parity, and compose authorized query/aggregate paths with publication lifetime, whole-query budgets and bounded results. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.3.017 - COMPLETE: Port string predicates.** Done when: Preserve supported matching/case behavior and escape wildcard values correctly. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.3.018 - COMPLETE: Port logical filter groups.** All scoped children verified, including isolated service/API adoption and live source parity. [Compatibility limits and evidence](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30), 2026-10-30. Deployment acceptance remains separate.
- [x] **IN-01.3.018a - COMPLETE: Validate tree shape before compilation.** Done when: Enforce depth, child count, NOT arity and source-compatible weighted node limits before SQL generation; cyclic/deep input terminates. Evidence: [logical-filter contract](02_ingestion_logical_filters.md), [race-enabled tests](../evidence/2026-10-29_ingestion-logical-filters.log), 2026-10-29.
- [x] **IN-01.3.018b - COMPLETE: Compose mapped AND/OR/NOT filters.** Done when: Nested supported scalar/membership leaves produce parenthesized SQL and correct real MySQL truth tables, including NULL under NOT. Evidence: [logical-filter contract](02_ingestion_logical_filters.md), [race-enabled tests](../evidence/2026-10-29_ingestion-logical-filters.log), 2026-10-29.
- [x] **IN-01.3.018c - COMPLETE: Preserve empty-group and default behavior.** Done when: Nil/empty AND/OR/NOT-empty emit no filter; empty children disappear and omitted kind/operator defaults match the source. Evidence: [logical-filter contract](02_ingestion_logical_filters.md), [race-enabled tests](../evidence/2026-10-29_ingestion-logical-filters.log), 2026-10-29.
- [x] **IN-01.3.018d - COMPLETE: Preserve ordered owned parameters.** Done when: Depth-first leaf and membership parameter order remains intact; compiled and returned argument slices are detached from caller mutation. Evidence: [logical-filter contract](02_ingestion_logical_filters.md), [race-enabled tests](../evidence/2026-10-29_ingestion-logical-filters.log), 2026-10-29.
- [x] **IN-01.3.018e - COMPLETE: Verify bounds and atomic failure.** Done when: Exact depth/width/weighted-count boundaries, invalid structures and unsupported leaves pass race-enabled tests; errors return no partial SQL and preserve leaf causes. Evidence: [logical-filter contract](02_ingestion_logical_filters.md), [race-enabled tests](../evidence/2026-10-29_ingestion-logical-filters.log), 2026-10-29.
- [x] **IN-01.3.018f - COMPLETE: Reconcile compatibility and adopt logical filters.** Done when: Review predicate-child rejection/weighted-count policy, verify live source parity and integrate authorized runtime execution with explicit empty-clause handling, request parsing limits, error mapping and whole-query budgets. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.3.019 - COMPLETE: Port stable sorting and pagination.** Done when: Tie-breaking is deterministic; page/offset or cursor bounds match the public contract. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.3.020 - COMPLETE: Bound query parameters.** All scoped children verified, including isolated service/API adoption and live source parity. [Compatibility limits and evidence](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30), 2026-10-30. Deployment acceptance remains separate.
- [x] **IN-01.3.020a - COMPLETE: Define bounded statement policies.** Done when: Validate positive parameter-count, aggregate parameter-byte and SQL-text limits with explicit staged ceilings. Evidence: [query-budget contract](02_ingestion_query_budgets.md), [race-enabled tests](../evidence/2026-10-29_ingestion-query-budgets.log), 2026-10-29.
- [x] **IN-01.3.020b - COMPLETE: Account all supported parameter occurrences.** Done when: Count projection guards, normalized values, binary identities and repeated keys without invoking unknown Valuers or overflowing byte totals. Evidence: [query-budget contract](02_ingestion_query_budgets.md), [race-enabled tests](../evidence/2026-10-29_ingestion-query-budgets.log), 2026-10-29.
- [x] **IN-01.3.020c - COMPLETE: Compose bounded mapped selections.** Done when: Combine guarded projections, nested supported filters and parameterized LIMIT under one shared budget; failures return no partial statement. Evidence: [query-budget contract](02_ingestion_query_budgets.md), [race-enabled tests](../evidence/2026-10-29_ingestion-query-budgets.log), 2026-10-29.
- [x] **IN-01.3.020d - COMPLETE: Enforce budgets before record execution.** Done when: Staged selection and exact-read paths check the complete statement before QueryContext and retain bounded result/error handling. Evidence: [query-budget contract](02_ingestion_query_budgets.md), [race-enabled tests](../evidence/2026-10-29_ingestion-query-budgets.log), 2026-10-29.
- [x] **IN-01.3.020e - COMPLETE: Verify statement and execution boundaries.** Done when: Exact/over budget tests, repeated values, invalid types, real MySQL selection, cancellation and existing exact-read/filter regressions pass. Evidence: [query-budget contract](02_ingestion_query_budgets.md), [race-enabled tests](../evidence/2026-10-29_ingestion-query-budgets.log), 2026-10-29.
- [x] **IN-01.3.020f - COMPLETE: Adopt one budget across all production read paths.** Done when: Reviewed policies cover future batch keys, aggregate expressions and every authorized SQL/HTTP query path; request parsing limits, error mapping and tests prevent any exposed path bypassing whole-statement budgets. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.3.021 - COMPLETE: Bound result payloads.** All scoped children verified, including isolated service/API adoption and live source parity. [Compatibility limits and evidence](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30), 2026-10-30. Deployment acceptance remains separate.
- [x] **IN-01.3.021a - COMPLETE: Define bounded result policies.** Done when: Validate field, worst-case transfer, row, page and row-count limits; reject impossible configurations before SQL execution. Evidence: [result-bounds contract](02_ingestion_result_bounds.md), [race-enabled SQL/boundary tests](../evidence/2026-10-29_ingestion-record-bounds.log), 2026-10-29; production adoption remains 021f.
- [x] **IN-01.3.021b - COMPLETE: Guard string transfer in SQL.** Done when: Oversized strings transfer only length/NULL markers; legitimate NULL remains distinguishable and bound parameters are detached from callers. Evidence: [result-bounds contract](02_ingestion_result_bounds.md), [race-enabled SQL/boundary tests](../evidence/2026-10-29_ingestion-record-bounds.log), 2026-10-29; production adoption remains 021f.
- [x] **IN-01.3.021c - COMPLETE: Check complete row budgets before decoding.** Done when: Validate length markers and conservative output size before copying strings; bad rows return no partial record. Evidence: [result-bounds contract](02_ingestion_result_bounds.md), [race-enabled SQL/boundary tests](../evidence/2026-10-29_ingestion-record-bounds.log), 2026-10-29; production adoption remains 021f.
- [x] **IN-01.3.021d - COMPLETE: Collect bounded pages without partial success.** Done when: Enforce row/page limits, propagate scan/iteration/cancellation errors and close rows on all exits. Evidence: [result-bounds contract](02_ingestion_result_bounds.md), [race-enabled SQL/boundary tests](../evidence/2026-10-29_ingestion-record-bounds.log), 2026-10-29; production adoption remains 021f.
- [x] **IN-01.3.021e - COMPLETE: Verify boundary and restricted-reader behavior.** Done when: Isolated SQL confirms large-value suppression, exact row/page boundaries, scoped column grants and connection release after cancellation. Evidence: [result-bounds contract](02_ingestion_result_bounds.md), [race-enabled SQL/boundary tests](../evidence/2026-10-29_ingestion-record-bounds.log), 2026-10-29; production adoption remains 021f.
- [x] **IN-01.3.021f - COMPLETE: Adopt bounds in all production read paths.** Done when: Record/aggregate endpoints and HTTP serialization enforce approved transfer/output budgets and bounded query limits; no unguarded projection path remains exposed. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.

- [x] **IN-01.3.022 - COMPLETE: Wire record read endpoints.** Done when: Get/list/search routes use authorized MySQL readers and preserve response/error DTOs. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).

### IN-01.3 - C. Aggregates and query verification

Depends on section B shared predicate/type builders. Representative query plans are evidence, not assumed index use.

- [x] **IN-01.3.023 - COMPLETE: Port COUNT.** All scoped children verified, including isolated service/API adoption and live source parity. [Compatibility limits and evidence](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30), 2026-10-30. Deployment acceptance remains separate.
- [x] **IN-01.3.023a - COMPLETE: Compile mapped COUNT(field).** Done when: Require an active selected single-column field; preserve non-null counting without substituting COUNT(*) or an arbitrary IP component. Evidence: [COUNT contract](02_ingestion_count.md), [race-enabled tests](../evidence/2026-10-29_ingestion-count.log), 2026-10-29.
- [x] **IN-01.3.023b - COMPLETE: Compose filtered COUNT queries.** Done when: Reuse supported scalar/membership/logical filters with ordered parameters and source empty-group semantics; no row LIMIT changes aggregate input. Evidence: [COUNT contract](02_ingestion_count.md), [race-enabled tests](../evidence/2026-10-29_ingestion-count.log), 2026-10-29.
- [x] **IN-01.3.023c - COMPLETE: Decode COUNT results strictly.** Done when: Return nonnegative signed int64; reject NULL, negative, overflowed and unexpected driver values without truncation or false success. Evidence: [COUNT contract](02_ingestion_count.md), [race-enabled tests](../evidence/2026-10-29_ingestion-count.log), 2026-10-29.
- [x] **IN-01.3.023d - COMPLETE: Enforce statement and execution contracts.** Done when: Check shared budgets before SQL; retain caller transaction/publication requirements and propagate database/cancellation errors. Evidence: [COUNT contract](02_ingestion_count.md), [race-enabled tests](../evidence/2026-10-29_ingestion-count.log), 2026-10-29.
- [x] **IN-01.3.023e - COMPLETE: Verify count semantics and boundaries.** Done when: Isolated MySQL tests verify five types, nullable/zero/false values, all-null/empty input, nested filters, decoder/budget bounds and query/filter regressions. Evidence: [COUNT contract](02_ingestion_count.md), [race-enabled tests](../evidence/2026-10-29_ingestion-count.log), 2026-10-29.
- [x] **IN-01.3.023f - COMPLETE: Complete COUNT compatibility and adoption.** Done when: Verify live PostgreSQL/MySQL result/error parity, resolve IP COUNT coverage, and integrate authorized endpoints with restricted grants, deadline/concurrency policies, schema/data concurrency checks and representative query-plan evidence. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.3.024 - COMPLETE: Port SUM.** All scoped children verified, including isolated service/API adoption and live source parity. [Compatibility limits and evidence](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30), 2026-10-30. Deployment acceptance remains separate.
- [x] **IN-01.3.024a - COMPLETE: Compile mapped numeric SUM queries.** Done when: Accept validated single-column int/float fields, reuse supported filters and enforce complete statement budgets before SQL. Evidence: [SUM contract](02_ingestion_sum.md), [race-enabled tests](../evidence/2026-10-29_ingestion-sum.log), 2026-10-29.
- [x] **IN-01.3.024b - COMPLETE: Preserve exact integer totals.** Done when: Decode scale-zero MySQL totals directly to signed int64 without float conversion; negative and beyond-INT32 totals work and overflow fails. Evidence: [SUM contract](02_ingestion_sum.md), [race-enabled tests](../evidence/2026-10-29_ingestion-sum.log), 2026-10-29.
- [x] **IN-01.3.024c - COMPLETE: Implement guarded floating totals.** Done when: Return finite float64; same-statement count/magnitude guard rejects unsafe accumulation including the observed silent-zero overflow case. Evidence: [SUM contract](02_ingestion_sum.md), [race-enabled tests](../evidence/2026-10-29_ingestion-sum.log), 2026-10-29.
- [x] **IN-01.3.024d - COMPLETE: Preserve empty/null and error behavior.** Done when: Synthesize typed zero only for zero non-null inputs; propagate malformed results, unsafe totals, cancellation and database failures. Evidence: [SUM contract](02_ingestion_sum.md), [race-enabled tests](../evidence/2026-10-29_ingestion-sum.log), 2026-10-29.
- [x] **IN-01.3.024e - COMPLETE: Verify precision and overflow cases.** Done when: Real SQL and decoder tests cover integer precision, fractional tolerance, empty/all-null input, overflow discovery, conservative cancellation rejection and COUNT/budget regressions. Evidence: [SUM contract](02_ingestion_sum.md), [race-enabled tests](../evidence/2026-10-29_ingestion-sum.log), 2026-10-29.
- [x] **IN-01.3.024f - COMPLETE: Review SUM policy and complete adoption.** Done when: Review normalized-input and conservative float magnitude/count restrictions (including finite cancellation rejection), verify live PostgreSQL parity, and integrate authorized endpoints with DTO/error mapping, restricted grants, concurrency acceptance and representative query-plan evidence. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.3.025 - COMPLETE: Port AVG.** All scoped children verified, including isolated service/API adoption and live source parity. [Compatibility limits and evidence](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30), 2026-10-30. Deployment acceptance remains separate.
- [x] **IN-01.3.025a - COMPLETE: Compile mapped numeric AVG.** Done when: Validated int/float fields and supported filters produce budget-checked SQL without changing input rows. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.025b - COMPLETE: Retain integer-average decimal precision.** Done when: Decode bounded decimal results without binary-float conversion; document database scale and reject malformed values. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.025c - COMPLETE: Guard floating averages.** Done when: Apply tested finite-result and accumulation bounds using same-statement count/magnitude evidence. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.025d - COMPLETE: Preserve nullable aggregate results.** Done when: Empty/all-null input returns nil; query and decode failures propagate without false zero success. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.025e - COMPLETE: Verify AVG integration and boundaries.** Done when: Isolated tests cover fractions, filters, nulls, decimals, unsafe floats, query budgets and errors. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.025f - COMPLETE: Complete AVG parity and runtime adoption.** Done when: Review staged type/precision restrictions, resolve excluded types, verify live PostgreSQL parity and integrate authorized endpoints with restricted grants, concurrent publication/state tests and representative query plans. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.3.026 - COMPLETE: Port MIN.** All scoped children verified, including isolated service/API adoption and live source parity. [Compatibility limits and evidence](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30), 2026-10-30. Deployment acceptance remains separate.
- [x] **IN-01.3.026a - COMPLETE: Compile mapped MIN expressions.** Done when: Allow validated numeric/timestamp fields and reject unreviewed string/Boolean/IP types. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.026b - COMPLETE: Decode typed MIN values.** Done when: Return int64/finite float64/UTC time with field range and precision checks. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.026c - COMPLETE: Preserve MIN empty/null behavior.** Done when: Ignore nulls and return nil for empty/all-null input, including required fields. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.026d - COMPLETE: Integrate filtered bounded MIN execution.** Done when: Retain filter semantics, parameter budgets, publication ownership and error propagation. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.026e - COMPLETE: Verify MIN boundaries and mapping failures.** Done when: Real SQL and decoder tests cover numeric/time minima, filters, invalid mappings, cancellation and budgets. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.026f - COMPLETE: Complete MIN parity and runtime adoption.** Done when: Review staged type/precision restrictions, resolve excluded types, verify live PostgreSQL parity and integrate authorized endpoints with restricted grants, concurrent publication/state tests and representative query plans. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.3.027 - COMPLETE: Port MAX.** All scoped children verified, including isolated service/API adoption and live source parity. [Compatibility limits and evidence](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30), 2026-10-30. Deployment acceptance remains separate.
- [x] **IN-01.3.027a - COMPLETE: Compile mapped MAX expressions.** Done when: Allow validated numeric/timestamp fields and reject unreviewed string/Boolean/IP types. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.027b - COMPLETE: Decode typed MAX values.** Done when: Return int64/finite float64/UTC time with field range and precision checks. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.027c - COMPLETE: Preserve MAX empty/null behavior.** Done when: Ignore nulls and return nil for empty/all-null input, including required fields. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.027d - COMPLETE: Integrate filtered bounded MAX execution.** Done when: Retain filter semantics, parameter budgets, publication ownership and error propagation. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.027e - COMPLETE: Verify MAX boundaries and mapping failures.** Done when: Real SQL and decoder tests cover numeric/time maxima, filters, invalid mappings, cancellation and budgets. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.027f - COMPLETE: Complete MAX parity and runtime adoption.** Done when: Review staged type/precision restrictions, resolve excluded types, verify live PostgreSQL parity and integrate authorized endpoints with restricted grants, concurrent publication/state tests and representative query plans. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.3.028 - COMPLETE: Port COUNT DISTINCT.** All scoped children verified, including isolated service/API adoption and live source parity. [Compatibility limits and evidence](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30), 2026-10-30. Deployment acceptance remains separate.
- [x] **IN-01.3.028a - COMPLETE: Compile mapped COUNT DISTINCT.** Done when: Use validated numeric/Boolean/timestamp fields; do not infer text/IP equality semantics. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.028b - COMPLETE: Verify numeric distinct equality.** Done when: Real SQL pins duplicate, negative, zero and adjacent floating-value equality behavior. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.028c - COMPLETE: Verify Boolean/time and NULL distinctness.** Done when: Repeated values collapse, nulls are excluded and timestamps retain microsecond distinction. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.028d - COMPLETE: Integrate filtered distinct count and decoding.** Done when: Reuse signed-count decoding, filters and whole-statement budgets with error propagation. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.028e - COMPLETE: Verify distinct count failure boundaries.** Done when: Empty/all-null input, unsupported mappings, count overflow, cancellation and pre-execution budgets are tested. Evidence: [aggregate contract](02_ingestion_aggregate_batch20.md), [race-enabled batch tests](../evidence/2026-10-29_ingestion-aggregate-batch20.log), 2026-10-29.
- [x] **IN-01.3.028f - COMPLETE: Complete COUNT DISTINCT parity and runtime adoption.** Done when: Review staged type/precision restrictions, resolve excluded types, verify live PostgreSQL parity and integrate authorized endpoints with restricted grants, concurrent publication/state tests and representative query plans. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.3.029 - COMPLETE: Port aggregate grouping/windows where supported.** Done when: Implement only source-supported grouping/time predicates with exact boundary semantics. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.3.030 - COMPLETE: Wire aggregate endpoints.** Done when: Authorized route inputs use the typed builder and preserve public result/error shape. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.3.031 - COMPLETE: Review representative read query plans.** Done when: Retain EXPLAIN evidence for exact lookup, list/filter and aggregates on representative isolated datasets. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.3.032 - COMPLETE: Close identified query-cost gaps.** Done when: Each unacceptable plan has a reviewed fix or explicit remaining limitation; no unsupported optimization claim. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.

### IN-01.4 - A. Integration fixture and accounting

Depends on IN-01.2/3 paths being available. Existing component fixtures are reused; these unchecked items concern full-stack acceptance.

- [x] **IN-01.4.001 - COMPLETE: Create the complete isolated MySQL fixture.** Done when: Install real shared data-model and ingestion migrations with guarded generated databases; reject application targets. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.4.002 - COMPLETE: Provision restricted test principals.** Done when: Use distinct runtime/worker/reader/operator accounts rather than the fixture administrator for service operations. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.4.003 - COMPLETE: Start real API and worker processes.** Done when: Exercise configured entry points and startup readiness in the isolated fixture. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.4.004 - COMPLETE: Port reusable source test datasets.** Done when: Preserve seeded payloads and expected results without duplicating source fixture logic unnecessarily. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.4.005 - COMPLETE: Add durable outcome accounting.** Done when: Count physical rows, receipts, audits, outbox, resources and jobs by test identity; responses alone are insufficient. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.
- [x] **IN-01.4.006 - COMPLETE: Add fail-closed test discovery.** Done when: Required integration suites fail when infrastructure is missing; report discovered/executed/skipped counts. Evidence: [shared-schema/operator acceptance](02_ingestion_reader_contract.md#complete-shared-schema-and-operator-batch-20-existing-tasks-2026-10-29), [source-pinned final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json), 2026-10-29.

### IN-01.4 - B. API, transaction and read correctness

Run against the full fixture; link reusable component evidence rather than checking these off from adapter-only tests.

- [x] **IN-01.4.007 - COMPLETE: Verify single create/patch parity.** Done when: Compare PostgreSQL/MySQL decisions, results, omitted fields and typed errors for identical fixtures. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.4.008 - COMPLETE: Verify batch parity.** Done when: Compare ordering, duplicate IDs, partial input and late-error atomic rollback. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.4.009 - COMPLETE: Verify record-read parity.** Done when: Compare projected values, missing/NULL fields, temporal rows, filters, sort and pagination. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.4.010 - COMPLETE: Verify aggregate parity.** Done when: Independently compute expected aggregates and compare both adapters at boundaries and empty sets. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.4.011 - COMPLETE: Race identical API receipt requests.** Done when: Concurrent duplicates return consistent results with exactly one durable effect set. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.4.012 - COMPLETE: Race conflicting API receipt requests.** Done when: Same key with changed tenant/table/mode/kind/content obeys scoped identity and conflict rules. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.4.013 - COMPLETE: Force exact-key digest collisions.** Done when: Distinct full-byte identities remain distinct and oversized collision buckets fail safely. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.4.014 - COMPLETE: Race alternate unique collisions.** Done when: Conflicting secondary unique fields remain targeted errors without overwriting another record. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.4.015 - COMPLETE: Cancel requests before commit.** Done when: Physical writes, evidence and receipt roll back; connections/locks are released. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.4.016 - COMPLETE: Lose API COMMIT acknowledgement.** Done when: Reconnect and replay through the public route; reconcile one retained outcome. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.4.017 - COMPLETE: Deny each required effect grant.** Done when: Missing audit/outbox/receipt rights cause rollback and no provisional success. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.4.018 - COMPLETE: Verify cross-tenant isolation.** Done when: Write, replay, read, aggregate, upload/deferred and administrative routes reject foreign resources. Verified 2026-10-30 in the isolated runtime: [live parity, policy review and acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). Production activation remains gated.
- [x] **IN-01.4.019 - COMPLETE: Race authorization/publication changes.** Done when: Test revocation and schema changes at documented transaction boundaries without stale writes or fallback bypass. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).

### IN-01.4 - C. Worker and operational acceptance

Depends on the production worker binding and queue operator contracts in IN-01.2.

- [x] **IN-01.4.020 - COMPLETE: Verify scheduled work timing.** Done when: Both work kinds respect database time, queue selection and configured attempts. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.4.021 - COMPLETE: Deliver the same work concurrently.** Done when: Only one live lease commits; duplicate deliveries produce no duplicate effects. Evidence: [isolated process adoption and task mapping](02_ingestion_process_batch20.md), [17-test process acceptance](../evidence/2026-10-30_ingestion-process20/accepted/summary.json), 2026-10-30. Production activation remains separate.
- [x] **IN-01.4.022 - COMPLETE: Lose worker completion COMMIT acknowledgement.** Done when: Fault the integrated final commit, restart/redeliver and prove durable completion/effects are reconciled once. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.4.023 - COMPLETE: Kill a worker holding a lease.** Done when: Another process rescues expired work; the old token cannot commit after restart. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.4.024 - COMPLETE: Verify malformed and invalid work.** Done when: CSV/JSON decode and model-validation failures produce agreed terminal status, counters and audit behavior. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.4.025 - COMPLETE: Exercise retries to exhaustion.** Done when: Backoff/heartbeat/timeout/reaper behavior reaches the configured terminal outcome without lost resources. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.4.026 - COMPLETE: Exercise cancellation and redrive.** Done when: Authorized operator actions retain lineage and cannot bypass receipt/fencing rules. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.4.027 - COMPLETE: Exercise shutdown under work.** Done when: No new claims after stop; handlers join or expire safely within the configured shutdown policy. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.4.028 - COMPLETE: Exercise database outage and recovery.** Done when: No false success during outage; resumed API/worker operations reconcile durable state. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.4.029 - NOT_APPLICABLE: Rehearse legacy and pending-work migration.** Closed by scope clarification 2026-10-30: User confirmed "new system, no existing data." No legacy source or pending work requires migration in this deployment. This is a scope closure, not executed migration evidence; reopen if existing data/work is introduced. See [fresh-deployment scope](#fresh-deployment-scope-2026-10-30).
- [x] **IN-01.4.030 - COMPLETE: Measure hot-key and distributed-key workloads.** Done when: Record latency, throughput, lock waits and durable counts using representative isolated data. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.4.031 - COMPLETE: Measure shared pool/worker interference.** Done when: Verify configured API/worker concurrency against the shared database connection/resource budget. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.4.032 - COMPLETE: Run repeated race-enabled acceptance.** Done when: Retain commands, versions, source revision, repeated results and failure/skip accounting. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).
- [x] **IN-01.4.033 - COMPLETE: Publish the acceptance reconciliation.** Done when: Every accepted test input has a durable terminal or explicitly pending outcome and all discrepancies are resolved. Verified 2026-10-30: [implementation, acceptance and limitations](02_ingestion_operations_batch20.md).

Evidence anchors: [atomic owner regression](../evidence/2026-10-29_ingestion-mysql-owner.log), [work/owner regression](../evidence/2026-10-29_ingestion-work-owner.log), [work recovery](../evidence/2026-10-29_ingestion-work-recovery.log), [execution log](../16_execution_log.md). The last work/owner run reported 53 passing top-level tests; the separate recovery run reported one passing top-level test. These are component/regression results, not IN-01.4 full-stack acceptance.

## Original remaining ten: execution packet (2026-10-30)

Historical packet before the fresh-deployment clarification: 206 complete / 10 open. The following work advances
existing leaves; it does not add artificial checklist items or close external gates.

| Task | Prepared or implemented locally | Required next input / acceptance evidence |
|---|---|---|
| IN-01.2.018 | Reconciled [review package](02_ingestion_schema_review.md) with 23-step source manifest, redrive lineage/FK cycle, implemented importer/grants and isolated runtime evidence. | Independent named reviewer decision tied to exact source/reference/grant hashes; resolve listed schema dispositions. |
| IN-01.2.054 | Retention dependency order and default retain policy remain defined in [operation contract](02_ingestion_operation_contracts.md). | Reviewed eligibility policy specifying horizons, holds, confirmed delivery, archival references and scope. Then implement dry-run, indexed keyset batches, narrow-lock eligibility rechecks, audit and restart tests. No deletion set is currently authorized. |
| IN-01.2.057 | Existing component/process evidence remains available; shared DB-01.2 tracker still lacks whole-queue acceptance. | Evidence for all shared kinds/consumers, database-wide connection budget, operational supervision/recovery and single execution authority; named operational decision. Ingestion-only tests cannot close this. |
| IN-01.2.062 | Added offline checksum-bound receipt inventory using the existing importer evidence format. | Trusted snapshot manifest and all chunks, source counts and extraction coverage; execute inventory and reconcile all chunks with source totals. |
| IN-01.2.063 | Added deterministic redacted exception report with scope, duplicate-key, envelope, evidence-limit and canonical-validation categories. | Actual source exceptions, independent handling dispositions and owner approval. Rejected evidence must not be repaired by guessed metadata or payloads. |
| IN-01.2.064 | Offline preflight accepts the exact existing JSONL importer format and pins snapshot/checksum. | Trusted extraction contract linking original raw bytes, historical model, mode and ordered audits to each receipt; implement paginated/checkpointed export against that source. This preflight is not an exporter. |
| IN-01.2.068 | Existing import/dry-run/checkpoint functionality can consume preflighted chunks. | Compare source inventory, imported receipt/provenance counts and canonical results per tenant/table; account for duplicate chunks and reviewed exceptions. Preflight success alone cannot detect target conflicts. |
| IN-01.2.069 | Verified source tables core_ingestion.upload_logs/deferred_ingests and River argument identities. | Same-snapshot resource/River rows including schedules, states, attempts, public keys and payload availability. Join upload_log_execute via upload_log_id and deferred_ingest_execute via deferred_ingest_id; resolve orphans/duplicates explicitly. |
| IN-01.2.070 | Existing fenced queue and stable resource/private-receipt semantics provide handover building blocks. | Approved representative pending snapshot, one authority schedule, drain/fence proof, restart and effects reconciliation. Do not turn on a second executor while River remains authoritative. |
| IN-01.4.029 | Existing isolated fixtures cover importer replay and process recovery. | Approved combined receipt/pending-work input, exception decisions and isolated rehearsal; retain dry-run/import/restart/count/effect reconciliation and rollback evidence. Synthetic success is not source migration acceptance. |

### Offline receipt preflight

Implemented in `backend/ingestion-service/cmd/inspect-legacy-receipts` using
`mysql.ValidateLegacyEvidence`, which reuses `translateLegacy` without a database.
It performs no writes, credential loading, target lookups or authorization decisions.
From the ingestion module, with an approved evidence file and its externally
supplied SHA-256:

```sh
go run ./cmd/inspect-legacy-receipts -input receipts.jsonl -snapshot SOURCE_CHUNK_ID \
  -sha256 EXPECTED_SHA256 -tenant TENANT_UUID -table TABLE_UUID \
  -object-type records -max-records 1000 -command-bytes 1048576
```

Input is the existing `mysql.LegacyImport` JSON serialization: byte fields such
as RawJSON use Go's base64 representation; the tool does not reinterpret a raw
HTTP request as an export envelope. Limit each file to 256 MiB, each line to
16 MiB, and each invocation to 1..10,000 records. The conservative canonical
evidence budget remains independently enforced. Scope is one tenant/table/object;
receipt keys are bounded to 1,024 bytes. Larger approved target keys require a
reviewed tooling change, not silent truncation.

Output version 1 reports snapshot, checksum, exact scope, row count, valid evidence
count, presence counts for raw input/model/mode/audits/results, and exception line
numbers/codes. Presence is not validity or proof of provenance. No receipt keys,
payloads, audit contents or raw decoder errors are emitted. Any exception yields
a nonzero exit status after the report; checksum/read/file/row-bound failure emits
no report. Equal bytes/scope/options yield equal reports. Checksums identify bytes,
not authenticity. Snapshot naming and trust must come from the export authority.

Verification: race-enabled offline preflight and existing importer command tests
passed (5 top-level tests and 7 subtests, zero failures/skips); scoped vet passed
for both commands and the MySQL adapter. Retained [raw test events](../evidence/2026-10-30_ingestion-offline-preflight/raw.jsonl)
and [source hashes](../evidence/2026-10-30_ingestion-offline-preflight/source_hashes.json)
cover synthetic evidence only. No production or application database was accessed.

Each invocation verifies one complete bounded chunk; it has no resume mode and
does not silently report a prefix as a complete inventory. Duplicate keys are
detected within that chunk. Cross-chunk duplicates, gaps, source completeness,
target conflicts, current authorization, retention and pending River work remain
separate checks. Do not concatenate reports and infer globally unique counts.

## Tests and acceptance

POC-05, PERF-02; reuse internal/service/integration_test.go, internal/service/table_contract_test.go, internal/store/postgres/contract_integration_test.go, tenant_data_reader_test.go and clients/datamodel/contract_reader_test.go. Add record accounting beyond successful-response counts.

Every accepted input has a reconciled durable outcome. Successful-ingestion audit
and outbox rows must share the successful data transaction. Validation-failure
audits are a separate existing path; characterize their error handling explicitly.
Current batches reject duplicate lookup IDs before mutation. Preserve that rule;
sort guard acquisition without reordering accepted inputs or result association.
Benchmark guard cost under hot and distributed keys.

## Implementation order and exit evidence

1. **IN-01.1:** Pin current behavior with runnable source-characterization tests.
   Resolve request identity, failed-audit policy and worker redelivery decisions;
   inventory both metadata migration pairs, five owned tables and all consumers.
2. **IN-01.2:** Implement reviewed MySQL schema/grants, tenant-bound transaction
   adapters, exact request reservation and lookup-key serialization. Preserve
   atomic data/audit/outbox/receipt effects and explicit ambiguous-commit recovery.
   Add upload/deferred persistence and fenced delivery completion using Q-01.
3. **IN-01.3:** Implement registry-backed contract/write/read coordination, typed
   projections/filters/aggregates, SQL and explicit HTTP read modes, payload and
   parameter bounds, then server/worker/API wiring. A stale cache or unavailable
   SQL contract must not bypass readiness through HTTP fallback.
4. **IN-01.4:** Run PostgreSQL/MySQL differential tests, real-engine rollback,
   concurrency, grant-denial and crash/recovery acceptance, then representative
   PERF-02 workloads and source-data/pending-work migration rehearsal. Record
   discovered/executed/skipped tests and reconcile durable record/event counts.

DM-01 must supply compatible published contracts, stable mappings, full-byte
lookup enforcement and physical readiness barriers before integrated acceptance.
DB-01/Q-01 supply shared transaction/codec/error and delivery contracts; importing
data-model-private Go packages or creating an ingestion-only application database
is not an integration strategy. Credentials, shared consumer grants, migration
ordering, pool budgets and runtime templates also require SC-01/OP-01 coordination.

## Delivery and rollback dependency

Implement and validate in disposable environments. All shared consumers must be compatible before live activation; an individual module passing its suite does not authorize a partial cross-engine production cutover. Preserve old adapters/compatible API artifacts through rehearsal as needed, with a single chosen runtime authority. The [roadmap](../10_implementation_roadmap.md) owns task dependencies and [cutover plan](../11_data_transfer_and_cutover.md) owns data rollback. Module schema rollback cannot undo externally delivered effects.

## Verified preparation (2026-10-22)

Physical record transaction update (2026-10-29): implemented the data-model-owned
staged v005 read-only exact-key lookup and ingestion `FindRecordKey`/`RecordWriter`.
Writes hold publication/lookup locks, use registry-backed six-type mappings,
choose INSERT or supplied-field UPDATE by exact object identity, and keep
alternate unique errors as conflicts. Readback verifies trigger-derived keys.
Tests now compose actual physical rows with audit/outbox/receipt effects, including
owner/receipt rollback, real lost-COMMIT replay and forced dictionary collisions.
Both modules passed vet; the current ingestion domain/MySQL suite passed 43
top-level tests without skips/failures, including the concurrently present owner,
legacy and worker components. Focused upgrade binding tests passed with race
detection. Evidence: [physical transactions](../evidence/2026-10-29_ingestion-physical-transaction.log)
and [lookup upgrade](../evidence/2026-10-29_record-lookup-upgrade.log).
Initial failures exposed the required lookup-definer self-EXECUTE grant and a
noncanonical JSON unit fixture; both were corrected and failed evidence retained.
The current granular tracker remains authoritative: these tests support component
items, not full v005 installation/readiness, independent review, runtime adoption
or IN-01.2 parent completion. Existing workspace changes were preserved.

Atomic owner update (2026-10-29): `KeyedRecordOwner.Execute` now integrates the
existing `RecordWriter`, `EffectStore` and `ReceiptManager`. Raw v1 input is
detached before callbacks with exact-number decoding. Current authorization runs
before and within the transaction, including replay; the local model loader is
called only for new effects. Model validation, duplicate normalized-key rejection,
physical publication/lookup locking, ordered batch results, successful audits,
record/batch outbox events and retained response share one transaction. Validation
failure audits use a separate reauthorized transaction; audit errors propagate.

The final module-wide vet and domain/MySQL race suite passed without skips or
failures; evidence: [atomic owner regression](../evidence/2026-10-29_ingestion-mysql-owner.log).
New cases cover replay after model changes, batch ordering/effects, late uniqueness,
outbox/receipt rollback, validation and failed audit, revoked authorization,
callback mutation isolation, eight concurrent duplicates, foreign tenant/duplicate
batch rejection, restricted grants and real lost COMMIT OK with physical effects.
The staged writer uses data-model's v005 full-byte lookup routine; earlier notes
below describe the previously missing dependency and are superseded for this
isolated component path. Complete installation/readiness acceptance is still open.

| Remaining IN-01.2 gate | Required closure evidence |
|---|---|
| Schema/grants | Reviewed append-only ingestion installation lineage, composite tenant/table constraints, restart/dirty-DDL handling and production restricted policies against complete data-model v005 installation |
| Owner entry points | Concrete trusted authorization and transaction-local published-model loader, single/batch API DTO/error mapping, unkeyed execution identity and worker composition; callbacks in fixture tests do not satisfy this |
| Legacy translation | Explicit retained SHA-1 compatibility/import policy with source identity proof; never relabel old hashes as v1 or guess missing table/mode/raw input |
| Upload/deferred work | Tenant-bound bounded repositories, stable execution identity, fenced attempt/completion and post-commit recovery |
| Queue integration | Implemented shared Q-01 enqueue/lease/ack/recovery contract, same-transaction work creation and running-worker acceptance; no ingestion-private replacement queue |

No application database, production grants or PostgreSQL/River configuration were
changed. This is a verified staged composition, not completion of IN-01.2/3/4.

Write coordination update (2026-10-28): added transaction-held shared publication
locks, exact version/state checks and bounded complete physical-field mapping
reads. Added persistent, sorted, bounded lookup guard reservation with a composite
tenant/table registry FK; forced digest collisions serialize without defining
record identity. Actual registry DDL tests cover stale/closed/archived/retired
states, missing mappings, blocked DDL preparation, concurrent readers, independent
tables/keys, cancellation, opposite batch orders and restricted grants. Vet and
domain/MySQL race regression passed without skips/failures. Evidence:
[write coordination](../evidence/2026-10-28_ingestion-write-coordination.log).
Fixture gates are privileged setup, not production readiness. IN-01.2 remains
open: physical record lookup needs an approved indexed full-byte data-model
consumer interface; existing writer grants deny private dictionary SELECT and
resolver execution. Record writes/readback, complete owner integration,
upload/deferred persistence, queue integration and production installation/legacy
translation remain incomplete. See the staged adapter README for the lock order.

Audit/outbox update (2026-10-28): staged `EffectStore` uses the existing authorized
transaction and fixed tenant/table/name scope. Pair validation precedes inserts;
bounded 1-500 pair batches use two parameterized multi-row INSERTs. MySQL tests
verify receipt/effect replay, outbox/receipt/owner failure rollback, cancellation
after writes, insert-only grants, denied outbox insertion, exact integer/NUL/time
round trips, invalid pair rejection and late bulk failures. Actual lost-COMMIT
replay now includes retained audit/outbox rows. Module-wide vet and domain/MySQL
race regression passed without skips/failures. Evidence:
[audit/outbox regression](../evidence/2026-10-28_ingestion-mysql-effects-verified.log).
Physical record writes remain fixture effects; schema installation/foreign-key
lineage, full owner/API/worker integration and outbox delivery are not complete.
IN-01.2 remains unchecked and runtime remains PostgreSQL/River.

Canonical command update (2026-10-28): added driver-free
`ingestion-request/v1` binding tenant, stable table, mode, single/batch kind and
ordered raw decoded content, plus the staged MySQL request constructor. Pinned
encoding/hash, input changes, exact large numbers, map order, batch order,
UTF-8/number/type/cycle/scope rejection and byte limits pass race tests. Real MySQL
replay reconstructs the command without mutable model validation; changing table,
mode, kind or content conflicts without rerunning owner effects. Module-wide vet
and domain/MySQL race regression passed without skips/failures. Evidence:
[canonical command regression](../evidence/2026-10-28_ingestion-mysql-command-regression.log).
This is not API/worker adoption, legacy translation or completed IN-01.2.

MySQL receipt update (2026-10-28): the staged
[receipt coordinator](../../../backend/ingestion-service/internal/store/mysql/README.md)
serializes absent receipt keys before owner effects, compares complete binary
keys within bounded digest buckets, rejects table/mode/kind/hash substitutions,
and reauthorizes initial calls and replays. Original UTF-8 JSON responses commit
with callback effects; failures return no provisional response. MySQL 8.4.8 race
tests passed for duplicate concurrency, forced collisions, rollback, cancellation,
retained-result/collision limits, revoked authorization and actual lost COMMIT OK.
A restricted fixture account cannot update/delete receipts or execute DDL.
Module-wide vet and all 85 existing PostgreSQL top-level tests passed; the MySQL
package was tested separately with four top-level tests, with no skips/failures
in either final suite. Initial binary JSON CHECK and column-grant failures were
fixed and their failed evidence retained. Evidence:
[MySQL](../evidence/2026-10-28_ingestion-mysql-receipts-regression.log),
[PostgreSQL dependency regression](../evidence/2026-10-28_ingestion-receipt-dependency-regression.log).
The DDL is not a deployment migration and callback effects are fixture effects,
not integrated physical ingestion. No additional checklist item is complete.
Runtime remains PostgreSQL/River; no application database or grants were changed.

Characterization update (2026-10-28):
`internal/service/migration_characterization_test.go` adds twelve replay/audit/
write scenarios and three worker completion-gap scenarios using the isolated
PostgreSQL fixture. Worker faults reject the completion repository call after
real data commit; they are not network-level lost-COMMIT tests. CSV retry produces
two success audits and four outbox events for one record; unkeyed deferred retry
produces two audits/two events; keyed deferred retry retains one audit/one event.
These source weaknesses are documented, not fixed or approved for MySQL.
Module-wide vet and race regression passed with 81 top-level tests, no failures
and no skips. Evidence: [focused](../evidence/2026-10-28_ingestion-characterization.log)
and [full regression](../evidence/2026-10-28_ingestion-characterization-regression.log).
No parent completion or MySQL runtime acceptance is implied.

- [x] **IN-01.2a - COMPLETED:** Replace ingestion RawTx with typed transactional enqueue methods; keep pgx and River inside PostgreSQL adapters.
- [x] **IN-01.2b - COMPLETED:** Verify upload/deferred row and job commit/rollback together, cancellation cleanup, scheduled times and missing-producer rollback against disposable PostgreSQL.
- [x] **IN-01.2c - COMPLETED:** Remove no-op queue success, require transaction managers on creation, and return retryable processing failures to River after persisting retry state.

The parent IN-01.2 remains open. MySQL keyed record composition is now verified as documented above; production bindings, legacy translation and target queue integration remain incomplete. Portable PostgreSQL enqueue tests do not verify a running MySQL worker's scheduling/rescue lifecycle. Full evidence and reproduction: [execution log](../16_execution_log.md).


### Staged work-owner integration - 2026-10-29

- [x] Transactional resource/job creation with immutable identity and isolated internal receipt namespace.
- [x] Shared bounded SKIP LOCKED claims, token fencing, heartbeat, retry/exhaustion and expired-lease recovery; completion joins physical writes, audit, outbox and receipt.
- [x] Token-authenticated owner using bounded shared metadata reads; current authorization also applies to replay. Public revision strings remain separate from physical publication UUIDs.
- [x] Legacy single/batch translation checks original request bytes, legacy SHA-1 semantics, historical model, audit attribution and stored result; separate operator authorization required. It imports receipts/provenance without repeating physical effects.
- [x] Source-bound owner migration ledger with checksum/definition drift checks. Shared extraction preserves the default data-model ledger/lock names.
- [x] Explicit object/column runtime grant manifest and real restricted-worker verification; immutable payload/evidence mutation is denied.
- [ ] Independent schema/grant reference review and full data-model installation compatibility acceptance. Fixture reference capture is test evidence, not deployment review.
- [ ] Production HTTP/worker construction and route/table resolution; staged constructors do not switch runtime.
- [ ] Operator queue inspection/redrive/retention, deployment shutdown/recovery acceptance and full Q-01 gate.
- [ ] Reconciled legacy export/inventory and bounded operator import tooling. Missing historical evidence blocks cutover.

Evidence: `../evidence/2026-10-29_ingestion-work-owner.log` and
`../evidence/2026-10-29_ingestion-work-recovery.log`. No throughput or production-readiness claim.


## 2026-10-29 - Twenty existing leaves completed

This batch closes existing tasks rather than adding new child IDs. Completed:
IN-01.2.017/020/022/026/037/066/067;
IN-01.3.002/003/008/009/014/029/031/032;
IN-01.4.001/002/004/005/006.

Granular totals (excluding rollups and numeric parents with lettered children):
**149 complete, 67 open, 216 total**. IN-01.2: 56/23; IN-01.3: 88/16;
IN-01.4: 5/28 (complete/open). No IN-01.4 B/C acceptance item was ticked from
adapter-only evidence. Independent deployment review and production runtime
adoption remain open; PostgreSQL/River is unchanged.

Evidence: [final suite](../evidence/2026-10-29_ingestion-existing20/final/summary.json),
[source fingerprints and commands](../evidence/2026-10-29_ingestion-existing20/provenance.json),
[reference artifacts](../evidence/2026-10-29_ingestion-existing20/reference_manifest.json),
[query plans](../evidence/2026-10-29_ingestion-existing20/query_plans.json),
[regression/recovery log](../evidence/2026-10-29_ingestion-existing20-final.log).
Full scope, limits and reproduction are in the [acceptance contract](02_ingestion_reader_contract.md).

## 2026-10-30 isolated process batch

Completed exactly **20 existing leaves** with retained process and regression evidence.
Previous batch leaf totals: **170 complete / 46 open / 216 total**.
See the [implementation and acceptance map](02_ingestion_process_batch20.md).


### 2026-10-30 - Reads and operations: next 20 completed leaves

Exactly 20 existing leaves completed with retained evidence in the [batch acceptance](02_ingestion_operations_batch20.md). Two final race-enabled process runs each passed 32/32; full-schema and scoped regressions also passed. Previous batch leaf totals: **190 complete / 26 open / 216 total**. Parent exact-read task completion is excluded from leaf counts. Default runtime remains PostgreSQL/River; independent deployment review, remaining parity, shared Q-01 and live legacy/cutover gates remain open.


### 2026-10-30 - Live read/write parity and adoption: 16 completed, request not yet fulfilled

Completed 16 existing leaves with [live source parity and scoped acceptance](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30). **Leaf totals at that checkpoint: 206 complete / 10 open / 216 total.** This is **16 of the requested 20**, not twenty. The ten remaining leaves are explicitly blocked on retained source/authority/retention inputs or independent acceptance. No fixture data or self-review was substituted, and no new leaves were created to inflate completion.

## Fresh-deployment scope (2026-10-30)

### Work on the remaining three

Added an explicit ingestion queue suite to the existing fixture verifier, retaining
test discovery, race execution and failure-on-skip/missing-pass accounting. Unknown
suites and missing full-schema importer configuration now fail before creating
result output, with unit coverage. No new dependency or application logic changed.

Current [verification evidence](../evidence/2026-10-30_ingestion-remaining3/verification.json):
queue **25/25**, full-schema/restricted-owner **13/13**, zero failed/skipped tests
in accepted runs; six verifier unit tests and scoped vet passed. Suites overlap;
these are execution counts, not 38 distinct test definitions. The first schema run
was rejected for missing importer configuration and retained separately; the
accepted run rebuilt/configured the importer and passed the entire selected suite.
Source hashes and raw logs are retained alongside the summaries. Queue-source
hashes precede the added full-schema importer prerequisite guard; the final source
manifest identifies the verifier used for schema acceptance.

For IN-01.2.018, the [review package](02_ingestion_schema_review.md) now separates
fresh-install dispositions, current evidence and the pending independent decision.
For IN-01.2.054, the [retention packet](02_ingestion_operation_contracts.md#fresh-deployment-retention-decision-packet-2026-10-30)
specifies the retain-all proposal and concrete deletion implementation criteria;
no deletion policy has been selected or cleanup implementation claimed.
For IN-01.2.057, the [shared queue inventory](07_shared_database_and_workers.md#q-01-fresh-deployment-review-inventory-2026-10-30)
records all 15 source kinds, payload identities, deployment budget accounting and
the remaining cross-service acceptance requirements. Ingestion tests alone do not
close the shared gate. **All three leaves remain open.**

The user explicitly confirmed: **"new system, no existing data."** This supersedes
requests for retained legacy snapshots and pending-work handover evidence for this
deployment. It is user-provided scope information, not a database inspection or a
measured zero-row inventory. No existing database was accessed or cleared.

Closed as **NOT_APPLICABLE**, not implemented migration work: IN-01.2.062, .063,
.064, .068, .069, .070 and IN-01.4.029. These seven legacy inventory, export,
reconciliation and transfer/rehearsal tasks must reopen if pre-existing data or
queued work is introduced. Existing importer/preflight code and synthetic tests
remain available; no additional legacy tooling is required for this fresh install.

**Current leaf totals: 206 complete / 7 not applicable / 3 open / 216 total.**
There are 213 resolved checkboxes, but only 206 completed implementation/verification
items. Parents and production activation remain open where their gates are unmet.

The three remaining leaves are:

- **IN-01.2.018 — BLOCKED:** Independent review of deployment schema/grants.
  Fresh installation removes historical-data transfer obligations; it does not
  replace review of current constraints, privileges and installation lineage.
- **IN-01.2.054 — BLOCKED:** Bounded retention tooling. A new system will accumulate
  receipts, jobs, audit and outbox data. No deletion policy has been selected;
  the existing default is retain. The user's no-data statement does not authorize
  deletion or make future retention tooling complete.
- **IN-01.2.057 — BLOCKED:** Shared queue operational acceptance. Fresh deployment
  still requires validated workers, fencing/recovery, resource budgets and owner
  readiness; there is no legacy execution-authority transfer to rehearse.

Fresh-install deployment checks must establish the intended empty target before
accepting traffic and test installation ordering/grants, API/worker readiness,
new ingestion effects, replay, tenant isolation, recovery and queue completion.
Do not silently clear a nonempty target: stop and reopen the migration assessment.
This clarification does not authorize runtime switching; PostgreSQL/River remains
configured. Source/target parity tests remain useful compatibility evidence.
