# PostgreSQL feature replacement designs

Status: proposed. Each design combines related findings while retaining exact source evidence below. The original [inventory](../02_postgresql_usage_inventory.md), [callsite index](../16_database_callsite_index.md) and [schema catalog](../14_schema_object_index.md) enumerate individual occurrences; no occurrence is declared portable merely because its family has a design. All acceptance tests are future work.

## Coverage map

| Replacement | Findings | Implementation dependency |
|---|---|---|
| R-01 Driver, errors and connection lifecycle | PG-01, PG-02, PG-28 | DB-01; exact driver/server versions and timeout policy remain open. |
| R-02 Physical namespaces | PG-03, PG-32 | SC-01, DM-01, IN-01, DE-01; physical-name collision/profile gate. |
| R-03 DDL, migrations and catalog discovery | PG-04, PG-05, PG-27 | SC-01, DM-01, OP-01; destructive/type-change compatibility needs per-operation review. |
| R-04 Contract and display trigger behavior | PG-06, PG-07 | DM-01, DB-01; canonical JSON comparison must match logical source behavior. |
| R-05 Targeted upserts and returned rows | PG-08, PG-09 | IN-01, DB-01, Q-01; native/dictionary lookup identity from SC-01. |
| R-06 Transactional queue and durable delivery | PG-10, PG-14 | Q-01, module worker tasks, OP-01; maintainer ownership and backlog profile required. |
| R-07 Advisory locks, isolation and savepoints | PG-11, PG-12 | DB-01, AU-01, CM-01; full lock-order call graph before acceptance. |
| R-08 Screening stale-worker protection | PG-13 | SR-01, Q-01; preserve existing lease duration pending measurement. |
| R-09 Cross-owner projection and review contracts | PG-15, PG-18 | SR-01, DE-01, CM-01, AU-01; privilege/autocommit-entry proof required. |
| R-10 Deferred FKs and exclusion ledger | PG-16 | SR-01, CM-01, SC-01; prove final result/version can be derived before review insertion. |
| R-11 Identity, roles and revocation | PG-17 | AU-01, SC-01, DB-01; provider state distinct from local identity. |
| R-12 Case capacity and relational integrity | PG-19, PG-35 | CM-01, AU-01, SC-01; exact FK manifest and member lock ordering. |
| R-13 JSON, arrays and bounded snapshots | PG-20, PG-21, PG-34 | DM-01, IN-01, DE-01, SR-01, CM-01; differential corpus resolves numerical edge cases. |
| R-14 Identifiers, time and scalar storage | PG-22, PG-23, PG-24 | SC-01, DT-01, module tasks; production range profile remains unknown. |
| R-15 Text comparison and manual visibility | PG-25, PG-33 | SR-01, CM-01, DE-01, SC-01; current collation corpus needs profiling. |
| R-16 Analytics and SQL expression rewrites | PG-26 | CM-01, DE-01; exact numeric and query-plan evidence required. |
| R-17 Provider, operations and tests | PG-29, PG-30, PG-31 | Provider compatibility must be checked for the pinned Keycloak release; deployment RPO/RTO/SLO and live inventory remain open. |

## R-01 Driver, errors and connection lifecycle

### Existing implementation, purpose and exact evidence

- **PG-01**: All six `go.mod` files and investigator import `jackc/pgx/v5`; migrations use lib/pq/PostgreSQL driver. [Ingestion MutationStore](../../backend/ingestion-service/internal/store/postgres/transaction_manager.go#L66) exposes `RawTx() pgx.Tx`; queries use pgx rows, batches, identifiers and command tags. Optional stress scripts import psycopg.
- **PG-02**: [Root Compose](../../docker-compose.yml#L36), each `internal/store/postgres/db.go`, auth server pgx pool. `DATABASE_URL`, read/worker URLs, pool min/max, PostgreSQL TLS options, healthcheck/volume.
- **PG-28**: [case HTTP mapping](../../backend/case-manager-service/internal/httpapi/handlers/helpers.go#L56), bulk/auto-assignment, investigator 23505, display 42P01/42703; pgx.ErrNoRows and RowsAffected throughout.

**Recommendation and required behavior:** database/sql plus the pinned MySQL driver, typed repository outcomes and explicit isolation. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** A full ORM introduces model/query churn without resolving SQL contracts. A global string-rewriting adapter hides semantic differences. Keep domain ports and replace concrete repositories.

**Security, correctness and concurrency:** No driver error text or DSN leaks. Preserve context cancellation and rollback; map duplicate-key, FK/check violations, deadlock 1213 and timeout 1205 by operation/constraint context, not by assuming all integrity errors mean conflict.

**Performance/scalability hypothesis:** Prepared SQL and bounded pools can control overhead; no speedup is assumed. Measure scanner/round-trip and saturation costs.

**Required application/schema/operational changes:** Replace pgx pool/Tx/Rows/Batch/Identifier/ErrNoRows and migration imports; encode UUID/time/JSON; explicitly return unchanged vs missing. Invalid config fails startup. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-01: commit acknowledgement loss, canceled query, nullable scanner vectors, duplicate and no-op outcomes, session reset and TLS refusal. Every existing HTTP/domain error mapping stays compatible.

**Dependencies and unresolved evidence:** DB-01; exact driver/server versions and timeout policy remain open.

## R-02 Physical namespaces

### Existing implementation, purpose and exact evidence

- **PG-03**: [ProvisionTenantSchema](../../backend/data-model-service/internal/tenantdb/postgres/schema_manager.go#L27) executes `CREATE SCHEMA`; writer uses metadata schema or `tenant_<uuid>` fallback. Decision [tenantSchemaName](../../backend/decision-engine-service/internal/store/postgres/tenant_data_reader.go#L183) derives namespace. Explorer/display quote metadata identifiers.
- **PG-32**: [ingestion router](../../backend/ingestion-service/internal/httpapi/router.go#L76) selects SQL metadata reader; [decision router](../../backend/decision-engine-service/internal/httpapi/router.go#L188) selects tenant reader; Compose default SQL model/ingestion_http tenant read.

**Recommendation and required behavior:** One database with owner prefixes and a persistent tenant/table physical-name registry. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** Per-service databases violate the standing architecture. A schema-per-tenant MySQL mapping becomes database-per-tenant. Registry lookup adds metadata dependence but preserves one logical DB.

**Security, correctness and concurrency:** Resolve tenant identity before names; never use a request-supplied table. Registry uniqueness prevents aliasing; fail closed on absent or unpublished mappings.

**Performance/scalability hypothesis:** Cache only published versioned mappings. Measure lookup overhead and invalidation; stale cache cannot bypass write barriers.

**Required application/schema/operational changes:** Replace every guessed tenant schema, direct reader and HTTP contract mapping; qualify fixed SQL. Grant new dynamic objects before publication. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-04: two tenants with same logical names, forged table IDs, mapping restart and concurrent model changes; SQL and ingestion_http read modes agree.

**Dependencies and unresolved evidence:** SC-01, DM-01, IN-01, DE-01; physical-name collision/profile gate.

## R-03 DDL, migrations and catalog discovery

### Existing implementation, purpose and exact evidence

- **PG-04**: Service `cmd/migrate/main.go`; [data-model runner](../../backend/data-model-service/cmd/migrate/main.go#L38) uses default migration ledger; ingestion/decision/screening/case specify independent names. [Auth Migrate](../../backend/auth-service/internal/platform/store.go#L36) executes three embedded scripts under advisory lock.
- **PG-05**: [TableService.Create](../../backend/data-model-service/internal/service/table_service.go#L151) writes metadata, creates tenant table/object_id unique index within same transaction. [indexing.go](../../backend/data-model-service/internal/tenantdb/postgres/indexing.go#L14) defines types, archive/drop and managed indexes. Migrations contain partial/expression indexes.
- **PG-27**: [GetManagedIndexState](../../backend/data-model-service/internal/tenantdb/postgres/index_introspection.go#L11) `to_regclass`; metadata/catalog/preflight SQL; stress PostgreSQL observation and EXPLAIN BUFFERS; consolidation pg_database.

**Recommendation and required behavior:** Checksummed owner migration lineages and recoverable intent/verify/publish runtime DDL (P-01). Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** Transactional DDL emulation is invalid. Blocking all services for every model edit is simpler but unnecessarily broad. A table barrier plus dedicated executor limits interruption.

**Security, correctness and concurrency:** Separate DDL credentials; named lock stays on DDL session. An expired lease cannot authorize a second in-flight DDL. Failover fences old primary.

**Performance/scalability hypothesis:** DDL may rebuild tables or wait on metadata locks. Measure copy space, duration and online-algorithm availability per alteration; do not force an unsafe fallback.

**Required application/schema/operational changes:** New migration runner ledger and inspection, runtime operation state machine, recovery CLI/worker, information_schema shape checks, deployment order and dirty-state handling. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-04: disconnect after DDL success, duplicate executors, index shape drift, blocked reader/writer and resume after crash; no premature published schema.

**Dependencies and unresolved evidence:** SC-01, DM-01, OP-01; destructive/type-change compatibility needs per-operation review.

## R-04 Contract and display trigger behavior

### Existing implementation, purpose and exact evidence

- **PG-06**: [migration 000005](../../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql#L18): table-scoped `FOR NO KEY UPDATE`, JSON aggregation, change-sensitive UUID version; triggers on table/field/enum writes; views consumed by ingestion SQL reader.
- **PG-07**: Data-model [000006](../../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L5), [000007](../../backend/data-model-service/internal/migrations/metadata/000007_display_columns.up.sql#L4), and [display store](../../backend/data-model-service/internal/display/store.go#L175): JSON validation, display-version updates, cleanup after field/link changes, locking and dynamic projections.

**Recommendation and required behavior:** Owner-controlled metadata publication with change-sensitive contract version and atomic display cleanup (P-01). Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** Keep equivalent MySQL triggers only if every validation/aggregation path is proven; uncoordinated application callbacks can omit updates. A single publication command is more inspectable but requires restricting other writers.

**Security, correctness and concurrency:** Tenant/field/link validation and permission checks stay in the write boundary. Unchanged canonical contract retains version; all allowed metadata mutations must publish consistently.

**Performance/scalability hypothesis:** Assemble bounded ordered metadata rather than aggregating arbitrary histories. Version cache invalidation only after commit.

**Required application/schema/operational changes:** Convert contract views/readers, version generation, display JSON validation and archived-field cleanup; owner grants prohibit bypass. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-04 and V-DM: no-op contract, enum/link removal, field archive, cross-tenant field, cache refresh and failure before publication.

**Dependencies and unresolved evidence:** DM-01, DB-01; canonical JSON comparison must match logical source behavior.

## R-05 Targeted upserts and returned rows

### Existing implementation, purpose and exact evidence

- **PG-08**: [TenantDataWriter.UpsertRecord](../../backend/ingestion-service/internal/store/postgres/tenant_data_writer.go#L26): `ON CONFLICT (lookup_field) DO NOTHING RETURNING id`, then patch on no row. Repository CRUD and authoring use RETURNING; exclusions use conditional conflict update.
- **PG-09**: [Ingest](../../backend/ingestion-service/internal/service/ingest_service.go#L112), BatchIngest line 294; tenant/key hash and response-kind checks; tenant write + audit/outbox + idempotency response in one transaction. [RecordIngestion](../../backend/ingestion-service/internal/store/postgres/transaction_manager.go#L42) uses pgx batch. PK `(tenant_id,key)` in ingestion 000001.

**Recommendation and required behavior:** P-02 narrow idempotency/object guards, explicit lookup/update/insert and readback within transaction. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** ON DUPLICATE KEY UPDATE can respond to an unintended unique key. Insert-and-classify-retry can be efficient but requires reliable conflict identity and a race proof. Guarded lookup is clearer; benchmark its extra round trips.

**Security, correctness and concurrency:** Unique constraints remain mandatory. Same-key replay checks hash and response kind; alternate-key collision cannot mutate a different row. No-op updates are valid existing rows.

**Performance/scalability hypothesis:** Hot object keys serialize inherently; unrelated keys remain concurrent. Sorted batch locks avoid common deadlocks without changing patch order.

**Required application/schema/operational changes:** Replace RETURNING and pgx batch; stable generated IDs, atomic audit/outbox/receipt, domain result enum and bounded retry. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-05: concurrent same key/hash and conflicting hash, two unique fields, unchanged patch, batch rollback, unknown commit and cross-tenant reuse.

**Dependencies and unresolved evidence:** IN-01, DB-01, Q-01; native/dictionary lookup identity from SC-01.

## R-06 Transactional queue and durable delivery

### Existing implementation, purpose and exact evidence

- **PG-10**: `internal/riverjobs/enqueuer.go` in four services; [decision persistence](../../backend/decision-engine-service/internal/service/decision_service.go#L597) calls EnqueueTx; [case worker](../../backend/case-manager-service/internal/worker/maintenance.go#L208) uses `case_queue`.
- **PG-14**: [CaseEventRepository.Claim/Finish](../../backend/screening-service/internal/store/postgres/case_event_outbox.go#L28) CTE claim/update/RETURNING, 60-second lease, attempts fencing/backoff/20-attempt failure. Case outbox and identity invitation delivery have their own lease protocols.

**Recommendation and required behavior:** P-03 owner work tables and shared executor; preserve separate domain lease policies (P-04). Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** External broker plus outbox is viable when operations already require it, but adds delivery infrastructure. River on PostgreSQL leaves the dependency. A MySQL transport needs full schedule/lease parity, not just INSERT support.

**Security, correctness and concurrency:** Transactional enqueue and fenced local effects; receiver idempotency for external effects. Restrict operator redrive and redact payload/error data.

**Performance/scalability hypothesis:** Bounded claim batches, separate ready/expired indexes and worker budgets; poll cost/age/fairness measured. Retention prevents hot-table growth.

**Required application/schema/operational changes:** Map all 15 job kinds, active uniqueness, retries, schedules, cancellation, metrics, dead letters and old-work migration. Missing enqueuer is an explicit failure. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-02: crashes before/after enqueue/claim/effect/ack, restart schedules, stale heartbeat, deadlock and duplicate external delivery. No lost committed job.

**Dependencies and unresolved evidence:** Q-01, module worker tasks, OP-01; maintainer ownership and backlog profile required.

## R-07 Advisory locks, isolation and savepoints

### Existing implementation, purpose and exact evidence

- **PG-11**: [IntakeRepository.Lock](../../backend/case-manager-service/internal/store/postgres/intake.go#L18) hashes tenant/scope/key; [investigator.Run](../../backend/investigator/review.go#L111) hashes operation scope; auth serializes identity binding/admin mutations and migrations; case worker uses session migration lock.
- **PG-12**: pgx `BeginTx(...TxOptions{})`; identity `FOR SHARE`; case/worker `FOR UPDATE OF ... SKIP LOCKED`; projector source-before-queue locking and savepoint. Writer explicitly assumes next READ COMMITTED statement sees concurrent insert.

**Recommendation and required behavior:** Transactional guard rows for business scope; pinned named locks only for DDL/migration; explicit RC writes and RR read snapshots. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** GET_LOCK for all business transactions leaks session lifetime across pool use. Serializable globally amplifies contention. Unique constraints alone cannot guard last-admin/capacity read-modify-write rules.

**Security, correctness and concurrency:** Document exact scope and lock order; no shared tenant row for unrelated work. Retry whole transaction after deadlock; preserve source-to-queue savepoint behavior.

**Performance/scalability hypothesis:** Measure lock waits and hot-scope contention; SKIP LOCKED only on work queues. Split joined locking queries when lock scope/order is uncertain.

**Required application/schema/operational changes:** Replace advisory calls, Tx defaults and FOR UPDATE OF syntax; add guard cleanup policy, cancellation-safe rollback and snapshot read helper. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-01/06: deadlocks, session reuse, cancellation, revoke-vs-mutation, capacity races and stable analytics snapshot.

**Dependencies and unresolved evidence:** DB-01, AU-01, CM-01; full lock-order call graph before acceptance.

## R-08 Screening stale-worker protection

### Existing implementation, purpose and exact evidence

- **PG-13**: [Claim/LockClaim](../../backend/screening-service/internal/store/postgres/continuous_intake.go#L34), [processScreening](../../backend/screening-service/internal/service/dispatch_service.go#L75): six-minute token lease; provider search outside transaction; lock token before candidate replacement, status and case-event writes.

**Recommendation and required behavior:** P-04 preserves provider-outside-transaction and token-checked publication. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** A process-local mutex fails across replicas. Checking token only before provider call or only during queue ack allows stale candidate writes.

**Security, correctness and concurrency:** Lock parent and validate current token/status/lease before replacing candidates. Atomic replacement/exclusion applications/parent/case event; trusted tenant remains required.

**Performance/scalability hypothesis:** Keep provider latency out of lock duration; bound result count and replacement batch. Measure contention for continuous reruns.

**Required application/schema/operational changes:** Port claim/recovery/replacement and explicit unchanged/stale outcomes; do not interpret MySQL changed-row count as existence. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-06: old provider response after reclaim, canceled request, partial zero-match response, crash during replacement and cross-tenant candidate.

**Dependencies and unresolved evidence:** SR-01, Q-01; preserve existing lease duration pending measurement.

## R-09 Cross-owner projection and review contracts

### Existing implementation, purpose and exact evidence

- **PG-15**: [decision 000019](../../backend/decision-engine-service/internal/migrations/metadata/000019_screening_projection.up.sql#L7), [screening 000007](../../backend/screening-service/internal/migrations/metadata/000007_execution_projection.up.sql#L15), [ExecutionProjector](../../backend/screening-service/internal/store/postgres/execution_projection.go#L36). Trigger increments source version/enqueues; owner function checks relationship and monotonic projection; savepoint isolates failure.
- **PG-18**: [Run](../../backend/investigator/review.go#L86) locks identity/case/resource, checks membership/version, hashes operation, applies owner mutation, inserts immutable review, calls [case-owned function](../../backend/case-manager-service/internal/migrations/metadata/000013_investigator_reviews.up.sql#L6) to append case event/outbox.

**Recommendation and required behavior:** P-04/P-05 fixed owner routines joining the caller transaction, monotonic source versions and immutable review. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** Independent HTTP calls lose database atomicity. Broad cross-owner DML weakens ownership. Narrow routine privileges are preferable but require deliberate definer security review.

**Security, correctness and concurrency:** Validate tenant/source/decision/scenario/operation binding; reject older projection; replay exact review result; no routine commits.

**Performance/scalability hypothesis:** Bounded source/queue batches and savepoint retries; case views project needed fields rather than loading all events.

**Required application/schema/operational changes:** Replace PL/pgSQL functions, triggers and callers with tested MySQL routines/owner interfaces; provision narrow grants and immutable audit triggers. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-06/07: forged binding, older/equal/newer versions, failed reconcile savepoint, review replay, stale case and atomic case-outbox rollback.

**Dependencies and unresolved evidence:** SR-01, DE-01, CM-01, AU-01; privilege/autocommit-entry proof required.

## R-10 Deferred FKs and exclusion ledger

### Existing implementation, purpose and exact evidence

- **PG-16**: [screening 000009](../../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql#L10), [Set](../../backend/screening-service/internal/store/postgres/object_exclusions.go#L33), [InvestigatorReview](../../backend/screening-service/internal/service/investigator.go#L69): exclusions then events then shared review insertion, deferred tenant/operation FKs; immutable audit trigger; parallel-array unnest for applications.

**Recommendation and required behavior:** P-05 precomputes locked transition and writes review -> event -> current state/applications before commit. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** Disabling FKs or dropping ledger references weakens integrity. A separate asynchronous ledger breaks atomic audit. Immediate parent-first order preserves the existing graph.

**Security, correctness and concurrency:** Immutable review result must be final before insert; all resource and case changes share transaction. Exact long tuple keys use SC-01 design.

**Performance/scalability hypothesis:** Extra transition planning is bounded to one review; batch exclusion application writes with explicit paired rows, no parallel-array ambiguity.

**Required application/schema/operational changes:** Reorder service/store orchestration, replace deferred constraints with immediate composite FKs, preserve event immutability and replace unnest with validated rows. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-07: every write failure rolls back all state; repeated operation unchanged vs conflicting; cross-tenant event refs fail; forced key collision retains both values.

**Dependencies and unresolved evidence:** SR-01, CM-01, SC-01; prove final result/version can be derived before review insertion.

## R-11 Identity, roles and revocation

### Existing implementation, purpose and exact evidence

- **PG-17**: [identity schema](../../backend/auth-service/internal/platform/schema.sql#L44), v2 function, store/sessions/invitations. Versioned roles/session/tenant checks with user→membership→session shared locks, role arrays and digest uniqueness.

**Recommendation and required behavior:** P-06 normalized role relation, current primary identity checks and narrow last-admin/issuer-subject guards. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** JSON roles reduce joins but weaken relational constraints. External OIDC alone does not replace local tenant/membership/session authorization. Cache-only authorization misses revocation.

**Security, correctness and concurrency:** Preserve digest uniqueness, active tenant, user/membership/session versions, issuer/subject identity and locked mutation authorization.

**Performance/scalability hypothesis:** Index membership roles and digest lookups; bounded preflight pages use explicit snapshot. Measure contention for revocation and invitation delivery.

**Required application/schema/operational changes:** Translate authorization routines, role storage, session SQL, invitation uniqueness/generation leases and embedded migrations; keep trusted actor contract. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-08: simultaneous admin removals, revoke racing review/model write, invitation stale generation, identity bind races, token expiry after lock wait.

**Dependencies and unresolved evidence:** AU-01, SC-01, DB-01; provider state distinct from local identity.

## R-12 Case capacity and relational integrity

### Existing implementation, purpose and exact evidence

- **PG-19**: Tenant FKs in case 000002/3; intake receipt `(tenant_id,source,event_id)`; decision membership unique `(tenant_id,decision_id)` in 000012; [assignment trigger](../../backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql#L10) locks inbox member before workload check.
- **PG-35**: All migration FKs/checks/uniques, especially case tenant composites, identity tenant memberships, screening continuous runs/exclusion references. Some other metadata FKs are ID-only with application/trigger tenant validation.

**Recommendation and required behavior:** P-06 owner mutation contract plus native tenant-composite FKs/checks/full unique keys. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** Handler-only capacity checks miss worker/reopen/move paths. Counters require reconciliation and are unjustified before counting is measured. Keep narrow locked count initially.

**Security, correctness and concurrency:** Restrict direct assignment/status writes or preserve equivalent triggers. One decision per tenant case membership and link transfer history remain atomic.

**Performance/scalability hypothesis:** Indexed workload count and bounded case projections; measure contention per inbox member, not global case serialization.

**Required application/schema/operational changes:** Translate tenant relationships, conditional unique links, capacity enforcement, receipt locks and move transactions; validate existing orphan data. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-09: last slot races, reopen/move, inactive membership, duplicate intake hash, concurrent transfer and restricted-account bypass.

**Dependencies and unresolved evidence:** CM-01, AU-01, SC-01; exact FK manifest and member lock ordering.

## R-13 JSON, arrays and bounded snapshots

### Existing implementation, purpose and exact evidence

- **PG-20**: JSONB payloads/rules/reports/audit; [explorer](../../backend/case-manager-service/internal/store/postgres/explorer.go#L99) `jsonb_each(to_jsonb(rec))`; manual history JSON path; workflow ordered `jsonb_agg`; published contracts compare `IS DISTINCT FROM`.
- **PG-21**: Identity roles `text[]`, model display `uuid[]`, index job columns, match texts; `ANY`, `unnest`, overlaps/containment/cardinality. [Object exclusion applications](../../backend/screening-service/internal/store/postgres/object_exclusions.go#L60) zips two arrays.
- **PG-34**: [aggregate builder](../../backend/ingestion-service/internal/store/postgres/tenant_data_reader.go#L193), SQL decision equivalent; AST pushdown; [ListStructured](../../backend/decision-engine-service/internal/store/postgres/workflow_snapshot.go#L12) one snapshot with ordered child JSON, 501-row guard.

**Recommendation and required behavior:** Native JSON for documents; relational children for roles/ordered references; explicit ordered Go assembly for workflows and typed aggregates. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** Text JSON loses validation. JSON for every relationship weakens integrity/indexing. Naive JSON_ARRAYAGG may not preserve required order. N+1 assembly scales poorly.

**Security, correctness and concurrency:** Preserve SQL NULL/JSON null and scalar types, ordinal order and tenant filters. Multiquery snapshots stay in one RR read transaction.

**Performance/scalability hypothesis:** Batch children by bounded parent IDs; keep 500 workflow limit and aggregate pushdown; benchmark large rule/explorer payloads.

**Required application/schema/operational changes:** Rewrite JSON operators, array predicates and unnest; typed codecs, batch projections and JSON-path extraction; preserve AST function set. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-10: empty/duplicate/null arrays, large JSON numbers, empty aggregates, count_distinct, ordered children and concurrent authoring.

**Dependencies and unresolved evidence:** DM-01, IN-01, DE-01, SR-01, CM-01; differential corpus resolves numerical edge cases.

## R-14 Identifiers, time and scalar storage

### Existing implementation, purpose and exact evidence

- **PG-22**: UUID primary/foreign keys, application `uuid.New`, SQL `gen_random_uuid`, UUID cursor casts. Match IDs also include non-UUID entity-derived text: not every identifier is UUID.
- **PG-23**: [tenant DDL](../../backend/data-model-service/internal/tenantdb/postgres/indexing.go#L20) infinity default; timestamptz, intervals, clock/statement timestamps, UTC analytics grouping, retry/lease math.
- **PG-24**: `toPgType` maps integer/double/INET; SQL schemas use BYTEA uploads/session digests and numeric fields; [decision initial schema](../../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L72) stores scoring attributes.

**Recommendation and required behavior:** Type mapping in 04: standard BINARY UUIDs, UTC DATETIME(6), explicit open-ended validity, exact bytes and IP family/prefix. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** CHAR UUIDs are easier to inspect but wider in keys; byte swapping changes ordering. A maximum timestamp sentinel risks collision. Automatic float-to-decimal conversion changes rules.

**Security, correctness and concurrency:** Preserve digest equality and identifier scope; lease/expiry uses current DB time; quarantine nonrepresentable values rather than coerce.

**Performance/scalability hypothesis:** Binary identifiers reduce key width but no measured improvement claimed; random UUID insertion and secondary indexes require representative tests.

**Required application/schema/operational changes:** Codecs, migration transforms, cursor binds, interval arithmetic, generated IDs and temporal API handling; retain textual match IDs. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-01/10: UUID ordering, microseconds/DST/UTC, open-ended dates, IP masks, nonfinite floats, int overflow and binary payload round trips.

**Dependencies and unresolved evidence:** SC-01, DT-01, module tasks; production range profile remains unknown.

## R-15 Text comparison and manual visibility

### Existing implementation, purpose and exact evidence

- **PG-25**: Case active `lower(name)` uniqueness; decisions `ILIKE` search and prefix `text_pattern_ops`; manual ownership uses `split_part(idempotency_key,':',2)`; many full TEXT unique keys.
- **PG-33**: [ListManualHistory](../../backend/screening-service/internal/store/postgres/manual_history.go#L8) filters tenant+actor, true manual origin, time/status/type and `(created_at,id)` cursor. [case ManualScreening](../../backend/case-manager-service/internal/store/postgres/manual_screening.go#L14) checks actor-bound origin and candidate under shared locks.

**Recommendation and required behavior:** Domain-specific equality/search collations; explicit manual_actor_id and origin populated from validated existing key semantics. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** Default case-insensitive collation changes opaque identities. Prefix uniqueness is not equality. Deriving actor on every query complicates indexing; stored actor must stay consistent with immutable origin.

**Security, correctness and concurrency:** Tenant+actor+manual-origin predicates remain mandatory even if actor is indexed. Preserve private history eligibility and candidate links.

**Performance/scalability hypothesis:** Use indexed cursor/filter fields; ILIKE substring cannot be assumed B-tree accelerated. Search service/fulltext is not added without measured need.

**Required application/schema/operational changes:** Backfill actor/origin only after validating source format; rewrite ILIKE/lower and expression indexes; exact name normalization and stable cursor. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-11: accents/case/trailing spaces, wildcard escaping, malformed keys, same actor across tenants, nonmanual impostor and timestamp ties.

**Dependencies and unresolved evidence:** SR-01, CM-01, DE-01, SC-01; current collation corpus needs profiling.

## R-16 Analytics and SQL expression rewrites

### Existing implementation, purpose and exact evidence

- **PG-26**: [analyticsSQL](../../backend/case-manager-service/internal/store/postgres/analytics.go#L14) MATERIALIZED CTEs/FILTER/GROUPING SETS/JSON output; explorer LATERAL; delete USING/update FROM; NULLS FIRST; tuple cursors. Ordinary CTEs are not inherently PG-only.

**Recommendation and required behavior:** Conditional aggregates, bounded grouped queries and explicit row projections with one read snapshot. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** Literal GROUPING SETS/FILTER/LATERAL translation is not available for every pattern. Multiple autocommit queries permit internally inconsistent reports; broad Go scans defeat boundedness.

**Security, correctness and concurrency:** Reuse identical permission scope across all branches; null ordering and tuple direction explicit. Preserve existing report version and output bounds.

**Performance/scalability hypothesis:** Compare selective/unselective plans, rows examined and temp spill. Snapshot duration must be bounded and cancellation release resources.

**Required application/schema/operational changes:** Rewrite UPDATE FROM/DELETE USING/NULLS ordering; build typed analytics output and explorer projections; preserve 1001 and 25+1 sentinel behavior. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** POC-10/11: concurrent updates between report queries, empty buckets, denied inbox, equal cursor timestamps and high-cardinality scopes.

**Dependencies and unresolved evidence:** CM-01, DE-01; exact numeric and query-plan evidence required.

## R-17 Provider, operations and tests

### Existing implementation, purpose and exact evidence

- **PG-29**: [Compose local-idp](../../docker-compose.yml#L108), [provision.sql](../../docker/keycloak/provision.sql#L1): JDBC postgres, fraud_keycloak role, schema ownership/privilege checks, psql gexec/DO.
- **PG-30**: [consolidation script](../../backend/tools/db_consolidation/consolidate_to_fraud_db.sh#L194) pg_dump/pg_restore; forced legacy DB drop is optional but defaults true; shell/PowerShell/run scripts and Makefiles invoke PG setup.
- **PG-31**: Go real-DB suites, Python `postgres_pilot.py`, HTTP integration and frontend fixtures. Tests create schemas, seed SQL and inspect plans; some skip without test URLs.

**Recommendation and required behavior:** Version-supported Keycloak migration plus MySQL-native deployment, backup/restore, fixtures and mandatory real-engine suites. Preserve the business operation and invariants identified above.

**Alternatives and trade-offs:** JDBC URL replacement does not migrate provider state. Existing PG dump/restore tooling cannot load MySQL. Mock-only tests cannot prove SQL or locks.

**Security, correctness and concurrency:** Separate provider/backup privileges, preserve issuer/subject links, protected exports and disposable test targets. No destructive legacy script against shared application data.

**Performance/scalability hypothesis:** Rehearse restore/cutover duration and resource budget; run workers/auth load absent from historical scale harness.

**Required application/schema/operational changes:** Compose variants, Docker/env/Make/scripts, provider provisioning, migration jobs, stress observation and CI integration; archive unsafe PG-only procedures. Related deployment/fixture changes belong to OP-01/V-01 even when no additional runtime infrastructure is required.

**Acceptance and verification:** OP-01, DT-01 and V-01: fresh boot/upgrade/restore, provider login/logout, all database tests actually executed, full-system load and failover.

**Dependencies and unresolved evidence:** Provider compatibility must be checked for the pinned Keycloak release; deployment RPO/RTO/SLO and live inventory remain open.

## Authoritative basis and implementation limits

Engine and driver mechanics are sourced in [references](13_authoritative_references.md); concrete algorithms and their rationale are in [protocols](07_transaction_and_recovery_protocols.md). This is an engineering synthesis, not a claim that official documentation prescribes this application architecture. Unsupported/uncertain behavior is gated by POCs, not treated as accepted. No application GIN, trigram extension, RLS or ORM replacement is invented; absent features remain outside scope unless live discovery establishes them.
