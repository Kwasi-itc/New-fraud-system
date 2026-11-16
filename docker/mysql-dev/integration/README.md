# Local MySQL integration suite

Run from the repository root with the local stack already healthy:

```bash
python3 -B docker/mysql-dev/integration/run.py
```

Requires Python 3, Node.js 22+, and Docker Desktop. No Python packages or
PowerShell are needed. The suite and its generated evidence are Git-ignored.
Both the application and reference MySQL containers must be running. Preflight
also checks that the reference server has the retained data-model migration
ledger; a healthy but newly empty reference server cannot support schema tests.

The runner executes the established service-specific MySQL behavioral audits,
then a cross-service pipeline test. It compares their actual passing coverage
with the current service OpenAPI files: 43 data-model operations, 15 ingestion
operations, and 120 decision operations. The decision audit also checks its
additional mounted routes. A route probe returning an error does not count as
successful behavior. Missing/stale coverage, failed assertions, missing reports,
child failures, and unsuccessful worker restoration fail the run.

Coverage includes schema authoring/import/export, links/display/navigation,
uniqueness/enums/constraints, audit/revisions/index jobs/reconciliation;
ingestion writes/batches/replay/rollback/reads/aggregates/CSV/deferred work;
scenario/rule validation/commit/publication, decisions/history, custom lists,
async/scheduled/recurring work, workflows, screening/scoring, local deliveries,
review, retries/cancellation, and tenant/origin rejection. Existing detailed
assertions remain owned by each service's audit rather than being duplicated.

The additional seven pipeline cases check the actual published model, ingestion
readback and replay, synchronous decisions loaded from stored records, async and
scheduled worker persistence, visibility of later ingestion updates, batch
rollback and tenant isolation, and unpublishing the suite's scenario.

These tests create fresh retained fixtures, alter/drop only their own fixture
objects, and deliberately exercise failure paths. The data-model worker is
paused for deterministic pending-job checks; other audits stop/restart workers
and temporarily change the local provider's callback policy. The runner restores
workers/provider policy and reports any restoration failure. Do not run other
tests or setup scripts concurrently. No database or volume is reset. The suite
shares the setup script's local filesystem lock.

The pipeline and ingestion audit use the configured demo ingestion tenant/table
(`amount=float`, `currency=string`). They do not retarget ingestion or add grants.
The pipeline creates its own uniquely named scenario and record IDs. Other
audits create separate tenants/objects. Fixtures, audit history, and all evidence
remain available after failures; the suite does not delete prior workload data.

Reports, per-service logs, runtime identities, HTTP traces, per-operation gaps,
and pipeline fixture IDs are under `.local/mysql-dev/integration/<run>/`.
Run time depends on Docker resources; these are behavioral tests, not a load
benchmark or production readiness certification. Production identity and real
external providers are separate acceptance work.

Useful commands:

```bash
# Inventory only; no API/Docker calls.
python3 -B docker/mysql-dev/integration/run.py --list

# Focused rerun. Its report explicitly identifies partial coverage.
python3 -B docker/mysql-dev/integration/run.py --only pipeline

# Runner regression tests; no running stack required.
python3 -B docker/mysql-dev/integration/test_runner.py
```

The decision audits accept `--output` so a combined run keeps all reports under
one unique directory and never reads a stale `latest.txt` result.
