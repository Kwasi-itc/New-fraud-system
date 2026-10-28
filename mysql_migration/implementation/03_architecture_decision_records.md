# Architecture decision records

All decisions are **proposed**. The selected option is a recommendation for validation, not user approval or completed implementation. Acceptance requires the listed evidence plus review by the relevant engineering/security/operations owner. Rejection or supersession must retain this history.

## ADR-01: Engine and deployment baseline

- **Status:** proposed.
- **Context/problem:** PG-02/04/29/30; six services currently share PostgreSQL16, with optional provider.
- **Functional/non-functional requirements and evaluation criteria:** Compatibility, recovery objectives, support lifecycle and measured total operation cost.
- **Alternatives considered:** Innovation release adds upgrade churn; MariaDB changes the target; enterprise edition is justified only by a required capability.
- **Security/privacy assessment:** TLS, private access and per-owner accounts; no assumed production controls.
- **Correctness/concurrency assessment:** One engine for all atomic writes; failover must fence old primary.
- **Performance/scalability assessment:** Capacity measured with all APIs/workers; no claimed speedup.
- **Operational complexity/maintenance:** Managed failover reduces local maintenance but adds vendor dependencies; self-managed HA requires staffing.
- **Selected option and rationale:** MySQL 8.4 LTS/InnoDB, pinned patch, one primary authority and tested recovery. This best matches the stated evaluation criteria, subject to the evidence gate below.
- **Consequences/trade-offs:** Avoids unnecessary engine/edition features; exact hosting remains unresolved.
- **Evidence/authoritative references:** 01_target_architecture.md; REF-01/02/16. See the [reference register](13_authoritative_references.md) and [feature evidence](02_postgresql_feature_replacements.md).
- **Validation before acceptance:** G0 records patch/support/provider compatibility, RPO/RTO and deployment choice; restore/failover tests before acceptance.

## ADR-02: One database and physical namespace registry

- **Status:** proposed.
- **Context/problem:** PG-03/32; schemas and tenant-derived names are embedded in readers/writers.
- **Functional/non-functional requirements and evaluation criteria:** Shared transaction reachability, object identity, privilege enforceability and naming stability.
- **Alternatives considered:** Database-per-service breaks standing requirement; raw logical names complicate authorization and identifier limits.
- **Security/privacy assessment:** Authorization precedes registry access; explicit object grants, not prefix-based trust.
- **Correctness/concurrency assessment:** Registry version coordinates schema publication and prevents tenant aliasing.
- **Performance/scalability assessment:** One cached lookup per contract is plausible but requires invalidation/overhead tests.
- **Operational complexity/maintenance:** Registry and grant lifecycle add maintenance; naming becomes consistent across consumers.
- **Selected option and rationale:** Owner prefixes plus stable registry-backed dynamic object names. This best matches the stated evaluation criteria, subject to the evidence gate below.
- **Consequences/trade-offs:** Preserves ownership without separate application databases; physical names no longer mirror user names.
- **Evidence/authoritative references:** 04_schema_and_data_access_design.md; PG-03 exact sources in 02. See the [reference register](13_authoritative_references.md) and [feature evidence](02_postgresql_feature_replacements.md).
- **Validation before acceptance:** SC-01 manifest, collision/import analysis and POC-04; review provider exception separately.

## ADR-03: Recoverable runtime DDL publication

- **Status:** proposed.
- **Context/problem:** PG-05/06/07; current metadata and physical DDL commit together.
- **Functional/non-functional requirements and evaluation criteria:** No premature publication, deterministic recovery and narrow interruption.
- **Alternatives considered:** Transaction wrapper cannot roll back DDL; global downtime is excessive; lease-only execution cannot fence ongoing DDL.
- **Security/privacy assessment:** DDL credentials isolated; published objects have complete grants.
- **Correctness/concurrency assessment:** Prior writers drain through registry lock; lost acknowledgement reconciles actual shape.
- **Performance/scalability assessment:** DDL duration/metadata locks may stall one table; benchmark every alteration family.
- **Operational complexity/maintenance:** Requires reconciler, drift tooling, operator states and repair runbook.
- **Selected option and rationale:** P-01 intent, table write barrier, dedicated session DDL lock, inspect and publish. This best matches the stated evaluation criteria, subject to the evidence gate below.
- **Consequences/trade-offs:** More state than PostgreSQL transaction but preserves externally visible schema integrity.
- **Evidence/authoritative references:** 07_transaction_and_recovery_protocols.md P-01; REF-03/04/08. See the [reference register](13_authoritative_references.md) and [feature evidence](02_postgresql_feature_replacements.md).
- **Validation before acceptance:** POC-04 including executor disconnect/failover and all six model scalar types, archive/display/index lifecycle.

## ADR-04: Transactional work engine

- **Status:** proposed.
- **Context/problem:** PG-10/14; River pgx enqueue participates in local transactions.
- **Functional/non-functional requirements and evaluation criteria:** Atomic enqueue, bounded recovery, schedules, active uniqueness, observability and maintainability.
- **Alternatives considered:** Broker plus outbox adds infrastructure; retained PostgreSQL violates exit; an existing transport may replace custom code only if lifecycle contract is complete.
- **Security/privacy assessment:** Per-owner work access; protected payloads and controlled redrive.
- **Correctness/concurrency assessment:** At-least-once execution; token-fenced local effects and receiver dedupe.
- **Performance/scalability assessment:** Measure poll overhead, lease contention, queue-age and fairness across 15 kinds.
- **Operational complexity/maintenance:** Custom queue maintenance is substantial and needs named ownership; feature completeness is a release blocker.
- **Selected option and rationale:** Owner MySQL work tables with one shared typed executor protocol, conditional on POC-02. This best matches the stated evaluation criteria, subject to the evidence gate below.
- **Consequences/trade-offs:** Small infrastructure footprint but more application responsibility; reject if staffing or recovery tests fail.
- **Evidence/authoritative references:** 07_transaction_and_recovery_protocols.md P-03; REF-05/12. See the [reference register](13_authoritative_references.md) and [feature evidence](02_postgresql_feature_replacements.md).
- **Validation before acceptance:** Q-01 job contract matrix, POC-02, crash/soak tests and operational ownership; never accept based only on enqueue tests.

## ADR-05: Exact long-key uniqueness

- **Status:** proposed.
- **Context/problem:** PG-16/25/35; compound and dynamic TEXT uniqueness may exceed index budget.
- **Functional/non-functional requirements and evaluation criteria:** Exact equality, tenant scope, data compatibility, enforceability and query cost.
- **Alternatives considered:** Prefix uniqueness merges different values; hash-only uniqueness assumes no collisions; tightening lengths changes contracts.
- **Security/privacy assessment:** Restricted allocator prevents forged canonical keys; sensitive raw key bytes protected.
- **Correctness/concurrency assessment:** Bucket lock plus full-byte comparison yields one ID per equal value without merging collisions.
- **Performance/scalability assessment:** Additional lookup/storage and hot-key contention; forced-collision performance and abuse bounds required.
- **Operational complexity/maintenance:** Dictionary lifecycle and dynamic-field enforcement are nontrivial; prefer native keys whenever valid.
- **Selected option and rationale:** Native full keys for bounded domains; collision-safe dictionary for necessary long keys. This best matches the stated evaluation criteria, subject to the evidence gate below.
- **Consequences/trade-offs:** Preserves full values at complexity cost; cannot ship with Go-only integrity enforcement.
- **Evidence/authoritative references:** 04_schema_and_data_access_design.md; REF-07/09. See the [reference register](13_authoritative_references.md) and [feature evidence](02_postgresql_feature_replacements.md).
- **Validation before acceptance:** POC-03 with forced collisions, restricted-account bypass and representative dynamic unique-field load.

## ADR-06: Transactions, locks and targeted mutations

- **Status:** proposed.
- **Context/problem:** PG-08/09/11/12/28; writer assumes RC visibility and specific conflict target.
- **Functional/non-functional requirements and evaluation criteria:** Preserve replay, atomicity, concurrent progress and error contracts.
- **Alternatives considered:** Default RR changes visibility; global serializable adds contention; generic ODKU can choose wrong unique conflict.
- **Security/privacy assessment:** Tenant/actor guard scope must be validated; no broad identity caches.
- **Correctness/concurrency assessment:** Whole-transaction retry on classified errors; ambiguous commit reconciles operation ID.
- **Performance/scalability assessment:** Extra round trips are a hypothesis to benchmark; narrow guards avoid unrelated serialization.
- **Operational complexity/maintenance:** Shared transaction helper and error taxonomy reduce scattered dialect assumptions.
- **Selected option and rationale:** Explicit RC mutations, narrow guard rows, deterministic lock order, typed outcomes; RR for multiread snapshots. This best matches the stated evaluation criteria, subject to the evidence gate below.
- **Consequences/trade-offs:** More explicit code; preserves behavior that a syntactic translation loses.
- **Evidence/authoritative references:** 07_transaction_and_recovery_protocols.md P-02/P-06; REF-01/05/06/10. See the [reference register](13_authoritative_references.md) and [feature evidence](02_postgresql_feature_replacements.md).
- **Validation before acceptance:** POC-01/05, lock-order review, mixed-case/identity stress and cancellation tests.

## ADR-07: Database-enforced owner mutation contracts

- **Status:** proposed.
- **Context/problem:** PG-15/17/18/19; cross-service atomic SQL functions and invariant triggers.
- **Functional/non-functional requirements and evaluation criteria:** Atomic ownership, least privilege, bypass resistance and auditability.
- **Alternatives considered:** HTTP breaks atomicity; arbitrary cross-owner DML bypasses rules; handler-only checks miss other writers.
- **Security/privacy assessment:** No root definer, dynamic SQL or leaked execute grants; trusted caller identity validation explicit.
- **Correctness/concurrency assessment:** No internal commits; immediate FK-compatible order; restrict direct invariant-field writes.
- **Performance/scalability assessment:** Short routine work and narrow locks; count-based capacity before unmeasured counters.
- **Operational complexity/maintenance:** Grants/definers need versioned deployment, restore tests and security review.
- **Selected option and rationale:** Fixed owner routines with minimal reviewed definers where needed; retain native constraints and proven immutable triggers. This best matches the stated evaluation criteria, subject to the evidence gate below.
- **Consequences/trade-offs:** Introduces privileged code boundary; remains proposed until transaction-entry and bypass proofs exist.
- **Evidence/authoritative references:** 05_security_and_integrity_requirements.md; REF-11/13. See the [reference register](13_authoritative_references.md) and [feature evidence](02_postgresql_feature_replacements.md).
- **Validation before acceptance:** POC-07/08/09, direct SQL bypass, accidental autocommit, revocation races and grant audit.

## ADR-08: Scalar and document representations

- **Status:** proposed.
- **Context/problem:** PG-20/21/22/23/24; UUIDs, arrays, timestamps/infinity, JSONB, INET and BYTEA.
- **Functional/non-functional requirements and evaluation criteria:** Round-trip fidelity, API compatibility, FK/index shape and numerical correctness.
- **Alternatives considered:** CHAR UUIDs wider but simpler; JSON relationships weaker; sentinel dates may collide; blanket decimal conversion changes evaluation.
- **Security/privacy assessment:** Digest bytes and identity comparisons exact; tenant relation constraints preserved.
- **Correctness/concurrency assessment:** Temporal predicates explicitly translated; snapshot and lease clocks distinguish current time.
- **Performance/scalability assessment:** Measure UUID insertion and JSON projection; avoid full-document reads in queues.
- **Operational complexity/maintenance:** Typed codecs and data quarantine increase migration tooling work.
- **Selected option and rationale:** BINARY UUIDs, UTC DATETIME(6), explicit open-ended validity, normalized relational arrays and native JSON documents. This best matches the stated evaluation criteria, subject to the evidence gate below.
- **Consequences/trade-offs:** No opaque text IDs converted to UUID; no unrelated monetary redesign.
- **Evidence/authoritative references:** 04_schema_and_data_access_design.md; REF-07/14/15. See the [reference register](13_authoritative_references.md) and [feature evidence](02_postgresql_feature_replacements.md).
- **Validation before acceptance:** SC-01 profiling and POC-01/10 with edge-value corpus and exported-data reconciliation.

## ADR-09: Bounded query rewrites

- **Status:** proposed.
- **Context/problem:** PG-20/26/33/34; ordered workflow JSON, analytics sets and explorer projection.
- **Functional/non-functional requirements and evaluation criteria:** Same permission/result semantics, stable pagination, bounded memory and measured plans.
- **Alternatives considered:** Autocommit multireads lose snapshot; N+1 increases cost; full Go scans abandon index use.
- **Security/privacy assessment:** All branches reuse authorized scope; actor-private history remains filtered.
- **Correctness/concurrency assessment:** Tie ordering, JSON order and NULL semantics explicit; preserve existing cap rejection.
- **Performance/scalability assessment:** Measure rows examined, spill, p95/p99, bytes and snapshot duration at skewed data sizes.
- **Operational complexity/maintenance:** More query orchestration, simpler type checking; no additional analytical system without evidence.
- **Selected option and rationale:** Bounded batched queries and Go assembly inside one read snapshot; native typed aggregate pushdown. This best matches the stated evaluation criteria, subject to the evidence gate below.
- **Consequences/trade-offs:** Retains API shape with controlled extra round trips.
- **Evidence/authoritative references:** 04_schema_and_data_access_design.md; REF-17. See the [reference register](13_authoritative_references.md) and [feature evidence](02_postgresql_feature_replacements.md).
- **Validation before acceptance:** POC-10/11 and differential result/plan suites; no blanket MySQL performance claim.

## ADR-10: Coordinated cross-engine cutover

- **Status:** proposed.
- **Context/problem:** PG-03/10/18/30/35; shared cross-owner transactions cannot span two independent engines.
- **Functional/non-functional requirements and evaluation criteria:** Data fidelity, bounded downtime, recoverability and one authoritative writer.
- **Alternatives considered:** Per-service live cutover breaks atomic contracts; uncontrolled dual writes create divergence.
- **Security/privacy assessment:** Protected exports, audited credentials and external-effect fencing.
- **Correctness/concurrency assessment:** No simultaneous active source/target writers; queue/receipt reconciliation and rollback write boundary.
- **Performance/scalability assessment:** Measure load/check duration and backlog drain; CDC needs independent ordering/idempotency proof.
- **Operational complexity/maintenance:** A coordinated window is operationally simpler; CDC and reverse replication are separate projects if required.
- **Selected option and rationale:** Default rehearsed quiesce/export/transform/load/validate/switch; CDC only if measured downtime requires it. This best matches the stated evaluation criteria, subject to the evidence gate below.
- **Consequences/trade-offs:** Rollback after target writes is not a DSN flip; preserve journals or roll forward.
- **Evidence/authoritative references:** 11_data_transfer_and_cutover.md; REF-16. See the [reference register](13_authoritative_references.md) and [feature evidence](02_postgresql_feature_replacements.md).
- **Validation before acceptance:** DT-01 full rehearsal with actual distributions and failure checkpoints; explicit downtime/recovery approval.

## ADR-11: Identity provider migration boundary

- **Status:** proposed.
- **Context/problem:** PG-29; local Keycloak has separate role/schema, while local identity remains application-owned.
- **Functional/non-functional requirements and evaluation criteria:** Stable issuer/subject, session/revocation continuity, supported upgrade path and least privilege.
- **Alternatives considered:** Realm export may omit live state; vendor table-prefix changes unsupported; external provider storage exception is architectural.
- **Security/privacy assessment:** Export protection and credential rebind; do not lose revoked-token state or silently reactivate sessions.
- **Correctness/concurrency assessment:** Provider state and local sessions need coordinated validity policy.
- **Performance/scalability assessment:** Measure login/refresh and provider pool usage independently from fraud requests.
- **Operational complexity/maintenance:** Provider downtime/version compatibility and storage namespace require explicit decision.
- **Selected option and rationale:** Pin supported provider/database combination and approve a provider-specific state migration plan. This best matches the stated evaluation criteria, subject to the evidence gate below.
- **Consequences/trade-offs:** A deliberate forced-login plan may be acceptable only with explicit security/product approval.
- **Evidence/authoritative references:** modules/06_authentication_and_authorization.md; REF-18/19. See the [reference register](13_authoritative_references.md) and [feature evidence](02_postgresql_feature_replacements.md).
- **Validation before acceptance:** AU-02 confirms pinned release docs, state coverage, database coexistence/exception and login/logout/revoke tests.

## ADR-12: Performance acceptance and optional scaling

- **Status:** proposed.
- **Context/problem:** PG-27/31/34; historical scale report lacks current revision and worker/full-system coverage.
- **Functional/non-functional requirements and evaluation criteria:** Correctness first, tail latency, sustainable throughput, fairness, recovery and cost.
- **Alternatives considered:** Historical API success is not business correctness; arbitrary pool growth or caches can mask bottlenecks.
- **Security/privacy assessment:** Use sanitized data and tenant-skew scenarios; authorization never read from stale replicas.
- **Correctness/concurrency assessment:** Load tests include races, retries, background delivery and schema changes.
- **Performance/scalability assessment:** No inferred saturation from 30 req/s report; find capacity knee with controlled sweeps.
- **Operational complexity/maintenance:** Benchmark artifacts and ongoing telemetry become maintained deliverables.
- **Selected option and rationale:** Paired reproducible baseline, domain-correctness checks and approved SLO gates before tuning topology. This best matches the stated evaluation criteria, subject to the evidence gate below.
- **Consequences/trade-offs:** Replicas, partitioning and caching remain optional proposals requiring measured benefit.
- **Evidence/authoritative references:** 06_performance_and_scalability_design.md; source stress report. See the [reference register](13_authoritative_references.md) and [feature evidence](02_postgresql_feature_replacements.md).
- **Validation before acceptance:** PF-01 records approved SLOs, reproducible PG/MySQL results, plans, resource profiles and soak/failover evidence.
