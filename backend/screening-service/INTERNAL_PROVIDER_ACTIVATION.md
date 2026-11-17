# Internal screening activation and recovery runbook

Prepared 2026-11-15 UTC. **Draft for operations, security, legal/compliance and release approval. Production activation is not authorized.** Existing tasks: DB-01, LIF-01, OPS-01/02, HARD-01, PERF-01, REL-01. The [acceptance register](INTERNAL_PROVIDER_ACCEPTANCE.md) governs release; a successful engineering rehearsal does not override a failed gate.

## Preconditions and release record

- [ ] Named release commander, database owner, on-call owner and rollback authority accept this runbook, maintenance window and incident routing.
- [ ] Approve the exact code/image digests, software inventory and container/OS scan. The Docker tags in development are not an immutable release attestation.
- [ ] Independently adjudicated matching data and source/type/category coverage meet the approved risk floors. Separate name and identifier profiles require distinct decisions. Current synthetic evidence does not meet this gate.
- [ ] Approve each source's intended use, access, attribution, redistribution, personal-data handling, corrections, retention and freshness in the [source register](INTERNAL_PROVIDER_SOURCE_APPROVALS.md). EU/Ghana live coverage and excluded PEP/non-SDN/ORC/linking remain off.
- [ ] Qualify the intended EC2/RDS deployment with representative tenant traffic, full effects, background imports/rescreens and all service replicas. Accept a database-wide pool budget and measured headroom. A local provider benchmark is insufficient.
- [ ] Approve backup encryption, restricted storage, retention, restore access, RPO/RTO, managed PITR and credential recovery. Complete an actual restore at deployment data volume.
- [ ] Record explicit activation approval, approved tenants/scenarios/datasets/profile/threshold, timeout/error policy, canary schedule and rollback criteria. No approval is implied by a checked engineering task.

## Staging rehearsal and deployment order

Use a restored, isolated shared PostgreSQL staging database with external deliveries disabled or directed to approved test receivers. Review all owner migrations and checksums; do not mix independent service databases. Restore original owner roles/ACLs and verify schema ownership before applications connect.

1. Inventory `core`, `identity`, `screening`, `case_manager` and River tables/sequences, migration versions and extensions. Check free disk, connection headroom and long-running transactions. Record a consistent backup and its checksum.
2. Follow the existing owner migration runners: prerequisite data-model/ingestion/decision/case schemas, identity through schema v4, screening through **000022**, and the matching River schema. Rehearse the same upgrade path as the target, not only a fresh install. Screening migrations remain owned by screening; authentication permissions remain owned by auth.
3. Migration 18 builds/tunes read indexes and can wait on or block table operations. Migration 21 invalidates old normalizer projections for current search without rewriting historical evidence. Migration 22 creates a narrow owner statistics routine and revokes PUBLIC execution. Respect migration lock/statement limits; abort and investigate lock-timeout failures instead of increasing them blindly. Measure migration duration on staging volume before approving the maintenance window.
4. Apply [source-role-grants.sql](scripts/source-role-grants.sql) **after screening 22 and auth v4**, as the migration owner. It creates NOLOGIN capability roles; it neither provisions passwords nor enables sources. Provision separate LOGIN identities in the approved secret manager and grant only their reviewed capabilities. Never run APIs/workers as the migration owner or a superuser. Do not grant broad `pg_maintain` or table ownership to satisfy statistics maintenance.
5. Deploy compatible APIs/BFF first with source collection and inline enforcement inactive; then existing decision/screening/case-delivery workers. Check migration compatibility, current signed sessions and machine-service authentication. The private writer must resolve to the **same primary/database** as the reader; copied database UUIDs alone are insufficient. The application now rejects a different restored clone.
6. Provision the separate source collector secret, verify approved outbound TLS/DNS/proxy policy, then deploy its optional profile only when authorized. `SOURCE_DATABASE_URL_FILE` reads a mounted secret; never commit DSNs or pass credentials in command arguments. The optional Compose service is non-root, read-only, capability-dropped and bounded, but deployment network/image acceptance is still required.
7. Rehearse source registration, complete staged publication, no-change checks, rollback, revoked access, worker restart and inline error paths. Confirm normalizer `nfkc-casefold-u17-v1`; an old normalizer requires a new reviewed projection/publication, even if the raw artifact is unchanged. Historical versions remain immutable.
8. Record exact migrations, roles, image digests, plans, resource observations, queue drain, restored evidence and rollback outcome. Obtain the release decision before repeating the procedure in production.

## Source provisioning and publication

Use private operator binaries with the approved source-role DSN file. `source-admin -action catalog` lists supported source keys without enabling them. `-action register -source <key> -dataset <id>` creates an unreviewed, disabled connection. It does not authorize use or grant tenant access.

Only after the source decision exists may an operator use `-action enable` with an exact `-rights-review-reference`, approved `-poll-interval` and `-max-stale`. Reviewed-notice datasets use `-action approve-use`; they are not automatic web feeds. Record grants separately using the screening-owned access contract and approved tenant list. Do not substitute a generic rights reference or share private datasets across tenants.

The source worker claims fenced leases, downloads bounded complete artifacts from the registry, retains hashes/raw provenance, validates/parses, stages and publishes atomically. Never publish a truncated snapshot or import arbitrary tenant URLs. For snapshots of at least 5,000 records, publication refreshes planner statistics before mutation locks, through the narrow routine in migration 22. A nonblocking advisory lock avoids overlapping maintenance; monitor plans/statistics after concurrent large publications rather than assuming a refresh always ran. No network call is made under the publication mutation lock.

Verify record/type counts, source IDs, parse warnings, artifact/version hashes, provenance, current readiness, actual search/enrichment and a repeated no-change import. Review unexpected count drops or new parser/normalizer revisions before proceeding. A quarantine or stale source must produce an evaluation error under the approved error-only inline policy; it must never become a successful clean decision.

## Monitoring and alert handling

Schedule the private, read-only check `source-admin -action health -max-queue-age 5m` only after approving the threshold and alert destination. Exit **0** means operationally healthy or inactive; inspect the JSON `status` to distinguish them. Exit **2** means degraded with alert codes. Exit **1** means the check/config/database failed and must itself alert. An inactive collector is not proof of functioning production coverage.

Signals include enabled-but-not-ready sources, quarantine, expired source leases, source/rescreen retry exhaustion, failed rescreens and oldest pending publication/rescreen age. Missing/malformed counters fail the check. Output contains aggregate counts, no names, queries, raw records or credentials. `-action diagnostics` adds backlog and statistics timestamps. External routing, notification delivery and on-call acknowledgement have **not** been configured by this work.

Collect API latency/error/incomplete counts, persisted decision/evidence counts, River queue/running/discarded/retry counts and oldest job age, outbox/workflow receipts, provider freshness/generation lag, PostgreSQL CPU/memory/I/O/locks, query plans and pool wait/acquired/max. Count every API/worker replica in the connection budget; a queue worker count is not a DB pool size. River fetch cooldown/poll are configurable, bounded and retain existing defaults of 100 ms/1 s. The acceptance workload explicitly uses 5 ms/100 ms and 4 workflow/8 outbox workers with a four-connection worker pool; these are measured test settings, not a production sizing prescription.

For stale/quarantined data: pause affected source use and inline scenarios if the release authority directs, retain evidence, inspect sanitized attempt codes and source changes, repair and republish a complete reviewed snapshot. For expired leases: verify the old process is dead, run bounded recovery and ensure old fenced publishers cannot commit. For retry exhaustion: repair authorization/data/dependency first, then use an audited bounded retry; do not loop blindly or mark jobs complete. For queue lag: compare arrival/drain rate, DB waits and receipt failures before increasing workers. For a tenant/credential/evidence breach: stop new access, revoke the affected capability through its owner, preserve audit/log evidence and invoke incident response.

## Canary and stop criteria

The following are **proposed engineering criteria requiring risk/operations approval**, not an approved release policy:

- Start with one explicitly approved tenant and one scenario, at a capped request rate validated in staging; observe at least 30 minutes and 10,000 completed evaluations before widening. Start in controlled comparison mode where the product permits; enforcement requires its own approval.
- Require 100% expected fixture hit/outcome agreement, zero successful decisions on failed/partial/stale screening, zero orphan or generation-misattributed evidence, zero cross-tenant leakage and zero self-approved private publications.
- At the approved test rate, require the agreed API p95/p99 and complete-effects throughput SLO, error rate below 0.1%, no incomplete evaluation reported as success, no exhausted/discarded jobs, queue age below five minutes and a queue that returns to baseline after the burst. The requested 500 decisions/s and p95 ≤50 ms remain targets to qualify, not measured production promises.
- Require all enabled sources ready, publication/rescreen lag within the approved freshness budget, no sustained pool saturation/lock wait and resource headroom approved against deployment limits. Compare decisions and actual downstream receipts, not only HTTP 200s.
- Stop immediately on any authorization/evidence/failure-policy breach. Pause expansion on an SLO breach over two consecutive five-minute windows, queue age over five minutes, source quarantine/staleness or exhausted retry; investigate and obtain the rollback authority's decision. Source freshness policy may demand an immediate stop sooner.

## Backup and actual restore procedure

Use secret-managed `PGSERVICEFILE`/`PGPASSFILE` or the deployment's approved identity, TLS and restricted filesystem permissions. Do not put passwords in command lines or logs. Choose backup tooling compatible with the server major version, include all owner schemas and River state, and capture role/grant definitions separately without password hashes in ordinary evidence.

For a logical rehearsal: quiesce writes and workers, run `pg_dump --format=custom --file=<protected-path> <service-connection>`, record SHA-256, tool/server versions, consistent inventory and migration versions, encrypt and verify the retained artifact. An online consistent dump can also be valid, but the observed rehearsal used quiesced synthetic data and does not establish production RPO.

Restore with `pg_restore --exit-on-error --single-transaction --dbname=<new-empty-approved-target> <protected-archive>`. Create required owner roles first; retain ownership/ACLs or explicitly review the mapping. Do not use `--clean`, overwrite an application database, strip ACLs, or expose restored external deliveries. Check checksum, schema/table/sequence inventory, migration versions, PUBLIC definer execution denial, primary/database binding, dataset generations/grants, private review history, source artifacts/audits, screening history and immutable decision evidence.

Start restored APIs/workers against controlled receivers. Recover expired staging/source leases, verify current search and historical generation attribution, replay pending workflow/outbox effects with the established delivery/idempotency contract, and confirm all queues drain. Recheck revoked/cross-tenant/private approval controls and immutable triggers. Record measured dump/restore/replay times; they are not a production RTO. Managed snapshots, WAL/PITR, geographical recovery, encryption/key recovery and deployment-size restore require separate qualification.

## Rollback and post-deployment verification

Disable new feature activation or pause source collection through approved configuration; do not erase successful decisions or rewrite evidence. A dataset rollback creates a **new** fenced generation/publication from retained data, preserving historical events and triggering currentness/rescreening. Retain imports and approvals. Old-normalizer generations remain unavailable for current matching until deliberately rebuilt. A paused/quarantined required source still causes an error; rollback must not silently bypass screening.

Prefer a compatible application rollback with forward schema compatibility. Destructive down migrations are test/rehearsal tools, not a production incident response. If schema recovery is necessary, use an approved restored target and the incident commander's cutover procedure, with explicit reconciliation of post-backup decisions/effects. Never restore over the primary as an improvised fix.

After any rollout/rollback, verify authenticated login, tenant scope, provider catalog/readiness, representative positive/negative/ambiguous searches, inline hit forcing, error-without-effects, exact saved evidence, private two-person approval, source monitoring, actual worker receipts and audit continuity. Record the final operational state and incident/release references.
