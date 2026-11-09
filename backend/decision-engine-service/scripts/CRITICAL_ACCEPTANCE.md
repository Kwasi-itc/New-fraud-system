# Critical integration and mixed-load checks

Use the existing private `compose.mysql.test.yaml` plus
`compose.differential.test.yaml` overlay. Keep the application database and live
runtime untouched. The fixture runner must receive both required-integration flags
and the private PostgreSQL URL supplied by that overlay.

Run these inside the decision-service test container, retaining stdout/stderr and
checking exit status; do not run load measurement concurrently with regression:

```sh
sh scripts/test-load-fixtures.sh
sh scripts/test-case-workflow-receipts.sh
sh scripts/test-critical-recovery.sh
sh scripts/test-critical-shared-owners.sh
sh scripts/test-shared-load.sh ramp /tmp/shared-ramp-new.json
sh scripts/test-shared-load.sh target /tmp/shared-target-new.json
sh scripts/test-shared-load.sh tuned /tmp/shared-tuned-new.json
```

The last three commands are separate measurements. A failed ramp does not prevent
collecting the target-rate diagnostic; neither failure is acceptance. Each output
path must be new. Save artifacts before removing the private stack.

The mixed profile runs actual evaluation SQL and bounded tenant-scoped cursor
history reads concurrently on **one database and one eight-connection pool**.
Ten tenants use ten rules (including two indexed SQL aggregates); evaluation has
a 60% hot tenant. Scoring configs are omitted in the deferred profile, and exact
persisted counts prove no scoring work was queued. Both workloads have separate
offered/success/error/drop/latency reports. Overall acceptance fails if either
workload misses its profile. SQL diagnostics cover both workloads.

These are local engineering diagnostics: 30 seconds, small generated business
tables, initially empty history, fixture model/authorization and no receiver
draining. The 1000/sec profile raises evaluation workers to 16 while retaining the
eight-connection database pool; history uses two workers at 20 reads/sec. It is
not a sustained production acceptance profile. Record host/container CPU, memory,
pool and other workload limits before comparing runs. Do not compare deferred
scoring results directly with earlier four-effect measurements as an optimization.

`aggregate_remote_concurrency` records the process-wide aggregate limiter setting
used by the service. Omitted values preserve the earlier fixture setting of one.
The tuned profile measures four slots with the same pool and offered workload;
it changes only a test policy, not the deployed service configuration. Raising
concurrency is an experiment, not proof that the production budget supports it.

Real case HTTP delivery, receipt reconciliation, producer lease recovery and
stale completion fencing are checked separately. The recovery test advances only
fixture lease/retry timestamps and reconstructs the dispatcher; it does not claim
a new process-crash test. Existing subprocess crash and COMMIT-loss cases remain
in the same suite. Other scripts cover shared-owner migrations, actual sessions,
revocation before persistence, screening projection, cancellation and callback
recovery. This does not establish full configured-provider acceptance: actual
outbox/other external action receiver contracts, search-worker composition and
staging resource allocation are still required.
