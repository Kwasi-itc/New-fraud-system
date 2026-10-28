# Data transfer, cutover and rollback

Status: proposed runbook design; no export, load, container change or database operation has been performed. The [audit data plan](../06_schema_and_data_migration_plan.md) remains the evidence base. Implementation task DT-01 must turn this into a rehearsed, versioned tool and operator runbook.

## Preferred strategy and alternatives

Use a coordinated maintenance window unless measured extraction/load/validation time exceeds approved downtime. Shared identity/review/projection/tenant transactions prohibit independent live service cutovers. A CDC-assisted migration is a separate design if required: it must preserve transaction boundaries, ordering, deletes, dynamic DDL, transformed keys and final source position, plus prove dedupe/restart and source-to-target reconciliation. Do not assume a generic connector supports this schema. Uncoordinated application dual writes are rejected.

## Preflight and transformation

Capture source revision and actual applied migration versions, all tables/columns/indexes/FKs/routines/triggers/grants, dynamic registry candidates, live extension usage, River/provider versions, row counts by tenant, sizes, encoding/collations and active work. Compare to the 119-file source catalog; do not infer live state from migration filenames. Profile key byte lengths, duplicate equivalence under target normalization, UUID validity, time range/infinity, float nonfinite values, IP masks, JSON values and orphan/cross-tenant relationships.

Create the SC-01 mapping manifest and deterministic transforms. Use protected chunk files or a resumable typed stream keyed by stable PK and snapshot/export ID. Each chunk records row count, canonical logical checksum, first/last key, transform version and load receipt. Compare canonical values, not database-specific JSON byte serialization. Include null tags, float representation policy and UUID order. Validate every chunk, every table total and all critical relationships; sample comparisons supplement rather than replace full key/checksum reconciliation.

Data load order derives from the actual FK graph: tenant/model parents and registry; identity users/memberships/roles; dynamic business records; scenario/configuration parents; decisions/evidence/workflow/scoring; screening parents/candidates; immutable review parents before exclusion events/current state/applications; cases/inboxes/membership and linked events in the order required by cross-owner references; receipts/outbox/jobs. Where this informal order conflicts with the manifest graph, the graph wins. Split bootstrap passes for cross-linked objects; maintain immediate target constraints. Do not blindly load alphabetically or disable FKs in application-accessible tables.

Preserve original IDs, operation/request hashes, receipt responses, event IDs, actor identities and audit timestamps. Do not recompute historical reviews from current business state. Normalize roles and ordered arrays, map infinity explicitly, backfill validated manual actor/origin and allocate exact-key references deterministically with retained mapping. Validate existing uploads/digests byte-for-byte. Production volumes and transfer duration remain unknown.

## Work, sessions and external effects

Stop schedulers, claimers, API mutations, ingestion sources and provider-dependent writers; finish or explicitly recover in-flight transactions. Establish a consistent final source snapshot with no remaining writer. Record queue/resource state and external delivery receipts. Translate outstanding work to versioned target jobs with the same logical dedupe identity; exclude completed work unless reconciliation proves it still needs an effect. Preserve retry/dead-letter state or document an approved transformation. Fence old worker credentials/endpoints before target workers start.

Provider migration is separate from local identity row conversion. The [Keycloak export guide](https://www.keycloak.org/server/importExport) states export does not include all persistent session/security state. Do not claim realm JSON is a full backup. AU-02 chooses a supported path and an explicit session/reauthentication/revocation policy; issuer/subject mapping must survive. Provider storage namespace within the one-database design is unresolved and blocks activation.

## Cutover checkpoints

1. Verify backup restore, source read-only/fencing, complete manifest, approved change window and named decision owner. Rehearsal must already have passed.
2. Drain/quiesce all writers and external deliveries, capture final source position/snapshot and work inventory. Abort if writers remain.
3. Export/transform/load with resumable receipts; reconcile counts/checksums, constraints, tenant relationships, permissions, review/outbox linkage and active-work uniqueness.
4. Start target services with mutations and external delivery disabled. Run bounded smoke reads, authorization negatives and no-side-effect scenario evaluation. Verify all pool URLs and readers point to target authority.
5. Obtain cutover approval using actual reconciliation artifacts. Enable one authoritative target writer set, then schedulers/consumers in controlled order; monitor business outcomes, queue age, locks and latency.
6. Maintain a protected source snapshot and target change journal/binlogs throughout the recovery window. Decommission PostgreSQL only after accepted observation, backup/PITR proof and provider/queue dependency exit checks.

## Rollback boundaries

Before target writes, abandon target and resume source after confirming no external effects or divergent accepted operations. After target writes, flipping URLs loses accepted data and may repeat external effects. Pause target writes, reconcile target-only business operations and receipts through a rehearsed reverse transform/change journal, or choose controlled roll-forward recovery. Any reverse migration must preserve audit immutability and operation identities. Never promise rollback unless that exact path has been tested with new target data and worker deliveries.

A failed schema step uses forward repair/reconciliation; an old PostgreSQL down migration is not MySQL rollback. Restore backups and apply binary logs according to the selected recovery design; [MySQL PITR documentation](https://dev.mysql.com/doc/refman/8.4/en/point-in-time-recovery.html) describes mechanics, not this system's achieved RPO/RTO. Run an isolated timed restore with grants/routines/keys and queue reconciliation before production approval.
