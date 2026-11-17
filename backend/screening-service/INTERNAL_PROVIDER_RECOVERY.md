# Phase 8 deployment and recovery verification

Verified 2026-11-15 UTC. Existing tasks: DB-01, LIF-01, OPS-01/02, HARD-01, REL-01. **Disposable PostgreSQL rehearsal passed; production recovery is not accepted.** All application services/data, including preserved MySQL data, were left untouched.

## Migration and privilege review

The complete screening suite passes fresh owner-schema installation plus screening down/up through **000022**, shared auth schema compatibility through v4, old-normalizer rejection/republication, immutable history and revoked access. The new maintenance routine's PUBLIC execution is revoked, fixed owner/search path is used, and actual restricted logins verify narrow maintenance permission during a 5,001-record private publication. No broad table ownership/`pg_maintain` grant was introduced.

The owner grant template now explicitly requires screening 22 and auth v4. The source read/private writer binding checks the actual same primary/database in addition to the copied DB identity marker. Production staging must still rehearse the target's real upgrade path, table sizes, lock window, role mappings, failover and image compatibility. Test down migrations do not authorize destructive production downgrade.

## Actual backup and restored database

`TestAcceptanceLogicalRestorePreservesEvidenceAndResumesWorkers` creates fictional data through real APIs and stores source versions, signed-session search history/monitor audits, private imports/two-person approvals, inline decision evidence, a newer publication and committed pending worker effects. It stops actual workers before taking the consistent logical backup and seeds an expired staging lease for recovery.

The test uses actual PostgreSQL 16.15 `pg_dump`/`pg_restore` tools from the [acceptance tools image](scripts/Dockerfile.acceptance). It dumps the shared test database in custom format, computes SHA-256, creates a **new empty disposable target**, restores with exit-on-error/single-transaction behavior and retains ACLs. It does not use `--clean`, overwrite an application database or omit ACLs. Generated test passwords are supplied through child environment, never command arguments or report text.

The final run's archive is 398,055 bytes, SHA-256 `8c988790f2580e6fe9a007df29169adb44ed23df508d02027ca525c3c0323060`. Dump took **549 ms**, restore **2,370 ms**, and replay of pending effects **213 ms**. Earlier runs are historical measurements; these tiny synthetic timings are not a production RTO.

Verified after restore:

- Exact semantic table inventory and sequence state in `core`, `identity`, `screening`, `case_manager` and River/public objects match the quiesced source before recovery. Row counts/content fingerprints and sequence last values are retained in machine evidence.
- Source generations, private reviews/approvals, original historical decision evidence and search/monitor audit history survive.
- PUBLIC execution of the auth-owner definer remains denied; immutable evidence triggers and foreign-tenant private-read denial survive.
- Pairing an original reader with the restored-clone writer is rejected despite the copied identity UUID; the same restored primary is accepted.
- Current provider search succeeds; historical inline evidence still references its original generation after newer publications.
- One expired staging lease is recovered. Dataset rollback creates a new generation/event, preserving history.
- Fresh restored-target APIs and actual decision/screening workers replay pending workflow/outbox effects to controlled receivers and drain them. Existing idempotency/delivery contracts remain in force.

## Operational health rehearsal

The new operator-only `source-admin -action health` contract emits aggregate sanitized JSON and status/exit codes: 0 for healthy/inactive, 2 for degraded, 1 for check/config/database failure. Unit tests cover every signal, queue-age boundaries and missing/null/malformed counters. A disposable DB fault injection produces enabled-but-not-ready, quarantined, exhausted-source-retry and expired-lease alerts; the freshly built CLI returns JSON and exit 2. No source is actually fetched/approved by this test. Separate existing suites exercise retry storms, rollback and killed-process recovery.

The health check does not send notifications. Deployment scheduling, on-call ownership, routes, dead-man monitoring of failed checks and end-to-end notification/acknowledgement tests still need approval and rehearsal. Five-minute queue thresholds in the runbook are proposed engineering values, not a signed operational policy.

## Remaining deployment acceptance

Logical synthetic restore does not verify production-scale storage, backup encryption/access/retention, secret/key recovery, cross-region restore, managed snapshots, WAL/PITR, failover networking or institution-approved RPO/RTO. No production migration or configuration was executed. Complete those checks on an approved staging/managed environment, record the results and obtain the deployment/release owners' decisions. The [activation runbook](INTERNAL_PROVIDER_ACTIVATION.md) defines preconditions, order, canary, monitoring, rollback, incident response and post-deployment checks.

## Reproduction

Use a dedicated disposable PostgreSQL server with CREATE DATABASE privileges, never the application DSN. Build the acceptance image with `docker build -f backend/screening-service/scripts/Dockerfile.acceptance -t fraud-provider-acceptance-tools:phase8 .`, mount this checkout at `/workspace`, mount optional Go caches and set `GOFLAGS=-buildvcs=false`. Run from `backend/screening-service`:

```sh
# Shared-schema regression with fresh API/worker/operator binaries
SCREENING_TEST_DATABASE_URL='<isolated-test-dsn>' bash scripts/test-controlled-provider.sh full

# Actual logical restore; requires pg_dump/pg_restore in the test image
SCREENING_RUN_RESTORE_TEST=true \
CONTROLLED_TEST_RUN=TestAcceptanceLogicalRestorePreservesEvidenceAndResumesWorkers \
SCREENING_TEST_DATABASE_URL='<isolated-test-dsn>' \
bash scripts/test-controlled-provider.sh full

# Explicit full decision load (not a provider-only benchmark)
SCREENING_RUN_ACCEPTANCE_LOAD=true SCREENING_ACCEPTANCE_REQUESTS=10000 \
SCREENING_ACCEPTANCE_CONCURRENCY=16 \
CONTROLLED_TEST_RUN=TestAcceptanceCompleteDecisionWorkload \
SCREENING_TEST_DATABASE_URL='<isolated-test-dsn>' \
bash scripts/test-controlled-provider.sh full
```

Set `SCREENING_ACCEPTANCE_REPORT_DIR` to an existing protected output directory to retain JSON reports. Optional `SCREENING_ACCEPTANCE_SQL_PROFILE=true` requires `pg_stat_statements` preloaded on an **owned profiling test server**, and resets its statistics; it is not a production diagnostic command. Separate name evaluation uses `MATCHING_CORPUS_VERSION=2`, `MATCHING_PROFILE=name-fuzzy-v1` or `name-token-v2`, `QUALITY_DIAGNOSTICS=true` and `MATCHING_QUALITY_REPORT=<path>` with the labelled-quality test. The corpus audit is `node scripts/audit-matching-corpus.mjs`; it reads the frozen v2 corpus and emits JSON on stdout. Its optional argument is a separate report output path, never an input path; matching code/test-data output targets are rejected.
