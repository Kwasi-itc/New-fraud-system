# Shared driver, transactions, jobs and migration contracts

Status: **NEEDS_REVIEW** (design); implementation **IN_PROGRESS**. Primary task: DB-01; prerequisites: B-01, PX-01. Findings: PG-01/02/04/10/11/12/14/28.

## Verified execution path

[Source entry point](../../../backend/ingestion-service/internal/store/postgres/transaction_manager.go). Ingestion and data-model now expose typed transactional enqueue operations, not raw driver transactions, through their mutation ports. Ingestion's PostgreSQL adapter owns River insertion and explicit per-attempt retry identity; startup/readiness checks queue compatibility and access. Shared investigator calls owner SQL functions. Every API/worker/migrate/reconcile executable constructs database dependencies; separate processes multiply the connection budget. Full source occurrences are in the [audit catalog](../../11_source_evidence_catalog.md).

## Target implementation

Provide a small shared SQL foundation for typed config, codecs, error taxonomy, transaction boundaries and observability. Domain ports expose business operations, not a universal SQL dialect abstraction. Q-01 implements P-03 with owner schemas/privileges and one maintained executor. Separate session-bound DDL locks from business row locks. Migration ledgers remain independently owned with explicit cross-owner ordering. No missing queue may quietly succeed.

Use the common [schema contract](../04_schema_and_data_access_design.md), [security requirements](../05_security_and_integrity_requirements.md) and [transaction protocols](../07_transaction_and_recovery_protocols.md). These are required acceptance inputs, not already implemented helpers.

## Implementation checklist

- [ ] **DB-01.1 ? IN_PROGRESS:** DB-01: implement driver/codecs/transaction/error contract and contract tests, then migrate ports without leaking sql.Tx into unrelated domain logic.
- [ ] **DB-01.2 ? IN_PROGRESS:** Q-01: fifteen-kind inventory, strict identity/export validation, shared lease supervisor, bounded poller, maintenance scheduler, queue fairness, optional jitter/metrics, retry/error-history/cancellation/retention policies and bounded occurrence planning implemented. Data-model adopts the host and lifecycle policies. Other owners' durable transports, persisted schedules, effect contracts and migration remain open; pure planners/validators do not activate work.
- [ ] **DB-01.3 ? IN_PROGRESS:** SC-01/OP-01: owner-scoped ledger/checksum/recovery/verification runner and dependency-ordered bootstrap implemented; data-model adopts it without changing its frozen lineage. Data-model, ingestion and decision metadata rollback now preserve shared River objects and reject River-down calls. Other owners' routines, grants, inspectors and installation plans remain open; shared migration-up coordination is not established by the rollback guard.
- [ ] **DB-01.4 ? IN_PROGRESS:** V-01: cleanup/session/acknowledgement, metrics, owner-ledger recovery, bootstrap exclusion, legacy payload/export, host/lifecycle policy tests plus real ingestion atomic enqueue, tenant separation, retry identity and readiness/access tests. All-owner atomic effects, reaper races and complete versioned work migration remain open.

## Verified foundation subtasks

Parent DB-01 remains IN_PROGRESS: these ticks cover the reusable foundation only,
not service conversion or production acceptance. Evidence: [execution log](../16_execution_log.md).

- [x] **DB-01.1a - COMPLETED:** Typed connection settings, bounded pools, strict UTC sessions and verified TLS configuration.
- [x] **DB-01.1a.1 - COMPLETED:** Explicit FK/unique checks, autocommit and NO_CHAIN initialization; session reuse/replacement assertions and composite tenant-FK/secondary-unique/rollback fixture passed. Production owner constraints, grants and adoption remain pending.
- [x] **DB-01.1b - COMPLETED:** Mutation/snapshot boundaries with rollback on callback error, panic and cancellation; read-only and snapshot consistency tests.
- [x] **DB-01.1c - COMPLETED:** Binary UUID and UTC DATETIME(6) codecs; precision/range rejection and real-engine round trips.
- [x] **DB-01.1d - COMPLETED:** Real deadlock and lock-timeout classification; whole-transaction rollback of preceding writes and pool reuse.
- [x] **DB-01.1e - COMPLETED:** Lost COMMIT acknowledgement fault test; no callback retry and committed operation receipt reconciliation.
- [x] **DB-01.1e.1 - COMPLETED:** Unconfirmed rollback is a typed cleanup error and cannot classify as an ordinary deadlock/timeout. Real lost-ROLLBACK-acknowledgement test preserves the callback cause, verifies rolled-back state and successful replacement-session use without retry.
- [x] **DB-01.1f - COMPLETED:** Real TLS connection plus wrong-hostname/untrusted-CA rejection; ephemeral fixture certificate reuse.
- [ ] **DB-01.1g - IN_PROGRESS:** Boolean, signed INTEGER, finite DOUBLE and UTF-8 Text codecs plus nullable variants implemented, including invalid/stale destination tests. Owner repository adoption/error-to-domain mappings remain pending; shared mechanical MySQL categories are not universal business errors. New codec real-engine tests passed on exploratory MySQL 9.7.1; pinned 8.4.8 rerun remains open.
- [x] **DB-01.1g.1 - COMPLETED:** Nullable binary UUID and UTC DATETIME(6) codecs; strict validation, stale destination clearing and real-MySQL NULL/zero-value round trips. Owner mappings remain pending.
- [x] **DB-01.1g.2 - COMPLETED:** Non-null INET storage codec preserves address family, host bits and prefix length; MySQL round trips and direct invalid-insert constraint tests passed. API parsing/formatting, nullable owner mappings, query parity and production schema adoption remain pending (PG-24).
- [x] **DB-01.1g.3 - COMPLETED:** Nullable INET codec binds all-null or validated present tuples, rejects partial NULL and clears reused destinations. Real MySQL constraints reject all six partial-null combinations; owner schema/repository adoption and query parity remain pending.
- [x] **DB-01.1g.4 - COMPLETED:** JSON/NullJSON storage codecs validate UTF-8/document syntax without float64 decoding, preserve SQL NULL versus JSON null, and clear stale scan state. Real MySQL scalar/large integer/Unicode/duplicate-key tests passed; full JSONB parity and owner adoption remain pending (PG-20).
- [ ] **DB-01.1h - IN_PROGRESS:** Shared pool/transaction observations implemented with concurrency-safe counters and separate uncertain outcomes. Data-model adopts shared migration/worker mechanics. Remaining driver-independent owner ports, runtime observability wiring and service conversion are not complete.
- [x] **DB-01.1h.1 - COMPLETED (2026-10-29 source and test recheck):** Ingestion uses typed enqueue/retry operations with no pgx/River imports in its service/port boundary; an AST contract test guards this. Required producers fail closed, client initialization errors propagate, and API/worker startup plus readiness validate pinned River lineage, required objects and role access. Disposable PostgreSQL tests cover atomic owner/job writes, tenant binding, callback/cancellation/panic rollback, schedules, missing schema/producers, and per-attempt retry deduplication. This is portable PostgreSQL preparation, not the pending MySQL ingestion adapter or exactly-once worker effects.
- [x] **DB-01.1h.2 - COMPLETED:** Data-model service ports use typed index enqueue without pgx/River imports; PostgreSQL display adapter retains an explicit transaction contract pending its own conversion. Full owner suite passed.
- [x] **DB-01.1h.3 - COMPLETED:** Data-model reconciliation uses the typed transaction/enqueue boundary; repair history and queue mutations roll back together, with exact-key contention tests.
- [ ] **DB-01.1i - IN_PROGRESS:** Explicit database-wide connection-budget validator implemented (replicas, reserved headroom, duplicate/unbounded/overflow rejection). Deployment inventory/adoption and representative load acceptance remain open. Aurora/failover testing is excluded from this work by the user, not claimed passed.

- [x] **DB-01.1i.1 - COMPLETED:** Disposable two-connection pool with sixteen waiting mutations: cancellation before callbacks, observable wait accounting, recovery and no leaked leases.
- [x] **DB-01.1i.2 - COMPLETED:** UTC/strict/default transaction session settings after successful/failed snapshots on the same connection and after physical connection replacement. Deployment failover and capacity remain pending.

## Tests and acceptance

- [x] **DB-01.1j - COMPLETED:** MySQL pinned-connection DDL lock lifecycle: fail-fast contention, explicit release, physical session disposal and real-engine failure tests. This is a P-01 prerequisite; operation ledgers, publication barriers, recovery and service adoption remain pending. POC-04 is only partially exercised.
- [x] **DB-01.1j.1 - COMPLETED:** Actual ALTER TABLE metadata-lock wait behind a reader: observed contention, server timeout/client cancellation, named-lock cleanup, unchanged shape and explicit successful retry tested. Production DDL recovery protocol and timeout selection remain pending.
- [x] **DB-01.1j.2 - COMPLETED:** Lost real ALTER success acknowledgement: callback runs once, cleanup ambiguity remains explicit, and a fresh lock owner verifies type/nullability/default before using the already-applied column without replay. Full schema inspector/publication recovery remains pending.
- [x] **DB-01.1j.3 - COMPLETED:** Reusable pinned-connection visible-column inspector with ordered definitions, explicit empty-result error, generated/collation/default metadata and real-engine drift/cancellation tests. Complete schema/grant inspection and publication decisions remain pending.
- [x] **DB-01.1j.4 - COMPLETED:** Reusable visible-index-part inspection preserves uniqueness, order, prefix, direction, expressions and optimizer visibility; MySQL same-name drift and cancellation tests passed. Complete constraint/grant inspection and publication decisions remain pending.
- [x] **DB-01.1j.5 - COMPLETED:** Visible FK/check inspection preserves composite mappings, referential actions and check enforcement; real MySQL drift, invalid-insert and cancellation tests passed. Table/grant verification, safe constraint replacement and publication remain pending.

POC-01/02; reuse existing owner transaction/integration tests and case maintenance/delivery tests. New real-engine tests must prove stale worker cannot commit business effects, not only that stale acknowledgement fails. Test all 15 kinds and negative configuration paths.

No global transaction mutex or one giant service account. Executor and exact-key/DDL mechanisms need named maintenance ownership. Shared code owns mechanics; services retain business transitions.

## Delivery and rollback dependency

Implement and validate in disposable environments. All shared consumers must be compatible before live activation; an individual module passing its suite does not authorize a partial cross-engine production cutover. Preserve old adapters/compatible API artifacts through rehearsal as needed, with a single chosen runtime authority. The [roadmap](../10_implementation_roadmap.md) owns task dependencies and [cutover plan](../11_data_transfer_and_cutover.md) owns data rollback. Module schema rollback cannot undo externally delivered effects.

The connection/transaction foundation is implemented in [mysqlstore](../../../backend/mysqlstore/README.md), with reusable [migration mechanics](../../../backend/mysqlstore/migrations/runner.go) and [worker mechanics](../../../backend/mysqlstore/workers). Data-model's staged adapters import these and its mutation manager optionally records shared transaction metrics. Ingestion's portable transaction/enqueue preparation is implemented; configured services still use PostgreSQL/River. See the [execution log](../16_execution_log.md) and the dated evidence below.

## Current shared work inventory and remaining adoption

The [executable catalog](../../../backend/mysqlstore/workers/catalog.go) returns all fifteen existing kinds, their default queues, timeout and transport uniqueness. Source-contract tests compare payload fields/kind methods with owner code. `DecodeLegacyIdentity` strictly accepts unversioned input (`version=0`); `DecodeIdentity` additionally understands ingestion's explicit version-1 per-attempt payload. Unknown/missing/duplicate fields, malformed/zero identifiers and oversized input fail. Neither invents a tenant for resource-only jobs. `CheckLegacyExport` requires authoritative owner scope, actual error history and valid state/timestamps, copies source buffers and rejects running work until drained. These are validation contracts, **not** target insertion, import receipts, external-effect reconciliation or a complete delivery migration.

| Owner | Kind / resource payload field | Timeout | Transport uniqueness |
| --- | --- | --- | --- |
| Data-model | index_job_execute / index_job_id | 30s | Kind + args, retained completion |
| Ingestion | upload_log_execute / upload_log_id | 5m | Kind + args, retained completion; v1 adds per-attempt identity |
| Ingestion | deferred_ingest_execute / deferred_ingest_id | 5m | Kind + args, retained completion; v1 adds per-attempt identity |
| Decision | scheduled_execution / execution_id | 10m | Kind + args, retained completion |
| Decision | async_decision_execution / execution_id | 10m | Kind + args, retained completion |
| Decision | async_decision_execution_callback / tenant_id, execution_id | 2m | Kind + args, retained completion |
| Decision | workflow_execution / tenant_id, execution_id | 5m | Kind + args, retained completion |
| Decision | screening_execution / tenant_id, execution_id | 5m | Kind + args, retained completion |
| Decision | scoring_request / tenant_id, request_id | 5m | Kind + args, retained completion |
| Decision | outbox_event / tenant_id, event_id | 5m | Kind + args, retained completion |
| Screening | screening_dispatch / tenant_id, screening_id | 5m | None: domain claim fences work |
| Screening | dataset_update_job / tenant_id, job_id | 5m | Kind + args, retained completion |
| Screening | monitored_object / tenant_id, monitored_object_id | 5m | None: preserve changes arriving during execution |
| Case | case_maintenance / empty global payload | 30m | Kind + queue, active states only |
| Case | case_delivery / empty global payload | 30m | Kind + queue, active states only |

Transport errors use the pinned River error-count fourth-power delay with +/-10% jitter, now shared; this must not overwrite owner retry policy. Index max attempts defaults to five; ingestion defaults to three. Decision scheduled/async transport attempts are configured (default three), callback API enqueue currently uses async max attempts, while workflow/screening/scoring/outbox API enqueue uses one. Decision scheduled/async domain retries additionally use configured base backoff (default 30s), doubling with a capped exponent; callback delivery has independent domain attempt accounting. Screening intake/monitored transport attempts are ten, dataset API enqueue uses one. Case singleton jobs have five attempts. These are current-source observations, not new deployment defaults.

Case intervals are configured and RunOnStart is true; case delivery scheduling is explicitly disabled without its publisher. Decision scheduled occurrences are domain-owned, not case periodic jobs. The shared occurrence planner bounds catch-up/coalescing/skip-late calculations, but owners must persist the occurrence, enqueue and watermark atomically and define their own singleton/RunOnStart policies. Owner-specific cancellation/redrive and independent domain effects still require implementation/tests before activation. Data-model adopts shared lifecycle policies while its claims/fencing/retention/operator SQL remains owner-owned; extracting the host does not port those tables to another service or authorize cross-owner writes. Optional publisher disabling remains distinct from silently skipping required work.

The shared runner deploys reviewed CREATE/ALTER statements (including complete fixed routines/triggers) and compares exact definitions and checksums. Owner privilege verification is mandatory in the bootstrap pass's Verify callback; it is not inferred by the generic runner. Bootstrap rejects mixed databases, absent/duplicate/cyclic dependencies, missing callbacks and unbounded/undersized pools before writing, and holds a dedicated database-scoped deployment lock across ordered apply/verify passes. Each service retains its ledger, routine allowlist and grant ownership. No down/force-clean, implicit object adoption or blind retry is provided.

## Concrete remaining owner dependencies

These are unfinished implementation and integration tasks, not merely unchecked general acceptance boxes. The roadmap permits isolated owner development with explicit fixtures; a dependency does not make that development impossible or authorize a live cutover.

| Remaining work | Required owner contract / acceptance dependency |
| --- | --- |
| Shared production installation plans, routines and grants | SC-01 exhaustive physical manifest and owner allowlists; generic runner cannot invent business objects or privileges. |
| Full identity/authorization adoption | AU-01 locked membership/session/routine contracts and restricted-role tests. |
| Ingestion MySQL keyed writes and durable effects | DM-01 published physical schema/barrier contract, IN-01 P-02 receipt/audit/outbox semantics and Q-01 target enqueue adapter. |
| Decision MySQL repositories, schedules and effects | IN-01 read/write contract plus DE-01 owner transitions, SC-01 schema and Q-01 delivery adapters. |
| Screening target queues/publication | SR-01 candidate/source/projection/exclusion policies and DE-01/AU-01/SC-01 contracts. |
| Case target queues/periodic work | CM-01 global schedule/singleton/maintenance policies and SR-01/DE-01/AU-01/SC-01 contracts. |
| Full legacy delivery import/activation | Each owner's state mapping, authoritative tenant/resource lookup, immutable receipt and external-effect reconciliation; common validation alone is insufficient. |
| Production connection budget and load acceptance | Approved deployment inventory, all API/worker replica pools and workload targets. Aurora testing is explicitly excluded. |

Verification scope: local Go 1.25.0 vet/race tests, real disposable PostgreSQL 16 ingestion tests and exploratory MySQL 9.7.1 tests; the declared MySQL fixture remains 8.4.8 / Go 1.25.8. Docker credential-helper failure prevented pulling the pinned images in this environment. No EC2/application database was used and no runtime engine or deployment permissions changed. Other owners' SQL, nil/no-op enqueue paths and service activation remain implementation work, not merely unperformed Aurora tests.

Detailed commands, test exclusions, corrected failures and remaining scope are in the [structured execution evidence](../evidence/2026-10-28_shared_foundation.json). The existing execution log has invalid UTF-8 bytes and was preserved without re-encoding. The disposable exploratory database and its private fixture network/certificate volume were removed after testing; application resources were not touched.

The subsequent dependency-free implementation pass is recorded separately in [2026-10-29 evidence](../evidence/2026-10-29_dependency_free_shared_work.json).
