# Local decision engine with MySQL

The local development API runs at **http://localhost:8082/docs**, alongside data-model
at port 8080 and ingestion at port 8081. All three use the persistent MySQL
8.4.8 `fraud` database in Compose project `fraud-mysql-dev`.

## Start

With Docker Desktop running Linux containers, run from this service directory:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/setup-mysql.ps1
```

Setup starts the ingestion/data-model prerequisites if needed, uses ingestion's
tenant as a default test fixture, installs/verifies the 149-step decision migration plan and the
independently tracked identity, case-review, screening-source and local-delivery plans, creates
separate API, worker and private provider accounts, and starts all three decision containers. Repeating
setup preserves application data and generated secrets. **The decision runtime
accepts every active, registered tenant.** `-TenantId <uuid>` changes the default
test fixture; it does not restrict the API or workers to that tenant.

| Address | Purpose |
| --- | --- |
| http://localhost:8082/docs | Swagger UI with a local profile notice |
| http://localhost:8082/openapi.yaml | Shared service API reference |
| http://localhost:8082/readyz | Database and migration readiness; models are checked per request |
| http://localhost:8082/capabilities | Enabled features, tenant mode and default test fixture |

Configuration is retained in `../../.local/mysql-dev/decision-runtime/config.json`.
`TenantMode` is `all-active`. The retained `TenantID` and the capability response's
`tenant_id`/`default_tenant_id` identify the default test fixture for compatibility.
They are not an authorization allowlist. The original fixture remains
`5c1ea673-693c-4bcb-adc8-eb3db2f2f868`, with object type `transactions`.

List registered tenants at `http://localhost:8080/v1/tenants`. For any active
tenant, use its ID in `/v1/tenants/{tenantId}/scenarios` and other decision routes.
Register and provision new tenants through data-model endpoints, then publish
their tables/fields before evaluating. Requests, workers and private receivers
discover tenants without restarting the decision service. Unknown and inactive
tenants are denied, and resources remain scoped to their owning tenant.

## Scope

This explicitly opted-in **local development profile** supports scenario/rule authoring,
validation, commit, publication, synchronous and async/scheduled decision execution,
ingestion triggers, decision history, test-run comparison/analytics, structured
workflow and workflow-rule authoring, screening/scoring configuration, status and retry,
authoritative screening-status reconciliation, and case-linked investigator review.
Its worker runs seven SQL-backed handlers (scheduled, async, callback, workflow,
screening, scoring, outbox), seven maintenance loops and the scheduler. The API
and worker each use eight SQL connections; the private provider uses four.
The provider computes local matches, projects screening results and applies scoring
results. Fifteen worker loops share the same pools across tenants; tenant discovery
uses primary-key pages of 20 and indexed due-queue checks. Each claim sweeps at most
eight pages before yielding, and wraps across idle pages without imposing a sleep
between ready events. Adding a tenant creates no pool or permanent worker
goroutines. The local model reader fetches current publications without retaining
an unbounded per-tenant cache. This is a development budget, not a throughput claim.

All documented routes are enabled. Actual case actions use the case-owner receipt
service. Screening uses explicit local watchlists; scoring uses a deterministic
local ruleset. Callbacks, workflow events and outbound events are persisted in a
private local inbox. A sent receipt establishes local persistence. GeoIP requires
a separately supplied database. Unsupported provider configurations/actions are
rejected explicitly; arbitrary external destinations are never contacted.

The full production runtime still requires its real authentication and external
provider contracts. This setup does not switch the default PostgreSQL entrypoint,
transfer PostgreSQL records, or establish production migration readiness.

## Accounts and persistence

Only `127.0.0.1:8082` is published; worker HTTP stays inside Docker. As with the
existing local stack, login is disabled using `LOCAL_MYSQL_NO_AUTH=1`, a loopback
`APP_ORIGIN`, and a generated internal credential. Foreign browser origins are
rejected. Every request rechecks the selected tenant's registration and activation;
each SQL owner also rechecks activation inside its transaction.

Generated passwords, tokens, configuration and retained plans live under ignored
`../../.local/mysql-dev`. The installer receives the root socket secret; API and
worker receive their respective restricted account and internal/provider tokens. The
private provider receives its own restricted account and provider token. The API
also receives a distinct development reviewer credential. They
have decision-owned DML grants and read-only access to published tenant records,
without data-model authoring or schema creation privileges. Runtime accounts are
restricted to the shared Docker IPv4 subnet.

Setup provisions each active tenant with its own actual identity user, membership,
seven-day opaque session and case-manager inbox. Credentials are distinct per
tenant; the original fixture credential remains valid. Changing the default fixture
does not change session identities or bypass revocation. Repeated setup renews expiry while
preserving revocation, activation and membership versions. Review requests recheck
the SQL session and exact case/resource/inbox access; actor/role headers cannot
override it. Review and case history/outbox commit together, while machine outcomes
remain read-only. The API receives scoped case-owner permissions for these operations.
Setup prints the reviewer and inbox IDs; `ReviewerID` and `ReviewInboxID` are also
retained in the local config file. Workflow case actions and screening review inbox
configuration use this verified session and the actual case-owner access functions.

The three legacy AI-named routes use a local expression compiler/explainer and
return `backend: local-expression-assistant` and `uses_ai: false`. For example,
`POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/generate-ast` accepts:

```json
{"expression":"amount > 100 and currency == \"USD\""}
```

It supports explicit comparisons, JSON literals, lowercase `and`/`or`/`not` and
parentheses, validates against the published model, and changes no stored rule.
It does not perform free-form natural-language or language-model generation.

Setup grants reads on current published physical tables across tenants, using
bounded metadata pages and table-specific grants. Rerun setup after publishing a
new table when stored-record lookup/aggregation is needed, provisioning a new
tenant's reviewer/inbox, or changing the Docker
network. Keep the generated secret files with the persistent database volume.

To stop just the decision services, from this directory:

```powershell
$local = '../../.local/mysql-dev'
docker compose --env-file "$local/compose.env" --env-file "$local/ingestion.env" --env-file "$local/decision.env" -f docker-compose-mysql.yml stop decision-engine-service decision-engine-worker decision-local-providers
```

Run setup again to rebuild and restart. Avoid `down --volumes` on this shared
project if you want to keep the data-model, ingestion and decision records.

## Verification

Current operation-by-operation and worker acceptance is recorded in
[MYSQL_ENDPOINT_STATUS.md](MYSQL_ENDPOINT_STATUS.md) and
[MYSQL_WORKER_STATUS.md](MYSQL_WORKER_STATUS.md). These distinguish successful
local behavior, disabled capabilities, unmounted routes, isolated fault testing,
and explicitly skipped tests.

For the live endpoint/worker audit:

```powershell
node scripts/mysql-endpoint-audit.mjs
```

This inventories the shared OpenAPI file and the actual running image's routes,
records positive/negative HTTP evidence and image IDs, checks every mounted tenant
route rejects unregistered tenant IDs, and exercises cancellation, retry exhaustion,
redrive and process restart. It retains fresh fixtures and restores the decision
worker, provider acceptance policy and temporary lock even if an assertion fails. Full-module regression
commands, the 45-minute package timeout, and skip explanations are in the worker
status report. The main Compose/default server is not changed by these checks.

With the three APIs running and Node.js available:

```powershell
node scripts/mysql-local-smoke.mjs
```

The smoke check creates uniquely named scenario/transaction fixtures, publishes
a rule, ingests a record, runs async and scheduled evaluation (including a stored
record lookup), reads the persisted decisions,
checks idempotent replay, invalid configuration, tenant and origin denial.
It retains its fixtures and writes requests/results under `evidence/mysql-local`.

Focused race-enabled checks use the separate, disposable MySQL fixture:

```powershell
docker compose -p decision-mysql-local-tests -f compose.mysql.test.yaml up -d --wait mysql
docker compose -p decision-mysql-local-tests -f compose.mysql.test.yaml run --rm test sh scripts/mysql-local-checks.sh
docker compose -p decision-mysql-local-tests -f compose.mysql.test.yaml down
```

These checks cover local capability guards, the full runtime's provider gates and
application composition, and API/worker grant boundaries. They do not replace the
complete decision engine integration, parity or load suites. Never aim disposable
fixture tests at the persistent `fraud` application database.

To run only the 39 formerly unrouted endpoints:

```powershell
node scripts/mysql-unrouted-audit.mjs
```

This retains fresh fixtures and traces under `evidence/mysql-unrouted`. The audit
uses explicit operator CLI commands to create genuine linked cases and inactive
screening/scoring fixtures through their owners. Fixture dispatches are cancelled
durably before retry tests; the screening source records an explicit local failure.
The worker and projector pause during manual fixture status tests and restart
afterwards. No external provider is called or reported as successful. The complete
endpoint audit runs this focused suite and incorporates its positive evidence.

To run the formerly disabled delivery routes and actual local effects:

```powershell
node scripts/mysql-disabled-audit.mjs
```

This creates a case with a real decision link, persists screening matches and
scoring results, and verifies local event/callback receipts. It temporarily
recreates the private provider with `LOCAL_CALLBACK_MODE=reject` to obtain durable
negative receipts, waits for all automatic attempts, and tests callback-only
redrive/cancellation before restoring normal acceptance. Fixtures and traces remain
under `evidence/mysql-disabled`. Latest verified counts and report links are in
the endpoint and worker status files.

## Local provider requests

Use the configured tenant and a published scenario that yields `review`. Callback
URLs must equal `http://decision-local-providers:8090/callbacks`:

```json
{
  "scenario_id": "<scenario UUID>",
  "object_type": "transactions",
  "idempotency_key": "local-callback-001",
  "items": [{"object_id":"local-001","object_type":"transactions","fields":{"amount":125.5,"currency":"USD"}}],
  "callback_url": "http://decision-local-providers:8090/callbacks"
}
```

Post that body to `/v1/tenants/{tenantId}/async-decision-executions`.
For `/scenarios/{scenarioId}/workflows`, an event workflow body is:

```json
{"name":"Local event","allowed_outcomes":["review"],"action_type":"emit_event","action_config":{"url":"http://decision-local-providers:8090/events"},"active":true}
```

Case workflow actions also use the real owner; for `create_case`, set
`action_config` to `{"inbox_id":"<ReviewInboxID>","name":"Local case"}`.
Unsupported local actions such as `add_tag` produce durable rejected receipts.
For `/scenarios/{scenarioId}/scoring-configs`:

```json
{"name":"Local score","allowed_outcomes":["review"],"ruleset_ref":"local:decision-score-v1","config_json":{},"active":true}
```

This ruleset returns the immutable decision score plus `risk_band`: low below 10,
medium from 10 through 29, high from 30. It does not change the machine outcome.
For `/scenarios/{scenarioId}/screening-configs`:

```json
{
  "name":"Local watchlist",
  "provider":"local-development",
  "allowed_outcomes":["review"],
  "active":true,
  "config_json":{
    "queries":[{"name":"Example Person","type":"Person"}],
    "provider_config":{"mode":"exact","watchlist":[{"id":"local-entity-1","name":"Example Person","type":"Person"}]}
  }
}
```

Matching normalizes case/whitespace, optionally filters type, and persists stable
match IDs through the screening owner. Supply an explicit empty watchlist for a
real no-hit result. Missing or unsupported configuration fails explicitly. Query,
watchlist and match counts are capped at 500. Delivery payloads are capped at four
MiB, dispatch timeouts at five seconds, and HTTP redirects are rejected.

Only the API port is published. Local destination names resolve within Docker;
the private receiver is not a public webhook. Inspect bounded retained receipts
without SQL edits using:

```powershell
docker exec fraud-mysql-dev-decision-engine-service-1 /app/mysql-api --delivery-receipts async_decision_execution_callback/<execution UUID>
```

Other kinds are `workflow_execution`, `screening_execution`, `scoring_request` and
`outbox_event`. Receipt identity includes tenant, resource, domain attempt and
redrive generation. Lost responses reconcile the same key and payload. Exhausted
unresolved sends remain uncertain until reconciliation; they do not fabricate
failure or block unrelated handlers. The new local receipt plan preserves its
original migration prefix and appends its payload-limit upgrade; screening adds
its own indexed ready-work migration. Setup retains the previous plan artifacts.

Add `--tenant <uuid>` to the operator CLI for a tenant other than the default fixture.

## Synthetic fraud acceptance

```powershell
node scripts/generate-fraud-fixtures.mjs
node scripts/mysql-fraud-audit.mjs
```

The audit creates two tenant models through the data-model API, runs setup to
provision their local review identities/read grants, and publishes five real rules:

| Rule | Condition | Score |
| --- | --- | --- |
| High transaction amount | amount >= 10000 | 35 |
| High supplied risk score | risk_score >= 80 | 40 |
| New account making a large payment | age < 7 days and amount >= 1000 | 25 |
| Foreign currency and merchant country | currency != USD and merchant country != US | 10 |
| Large card-not-present payment | card_present = false and amount >= 5000 | 20 |

The deterministic [dataset](testdata/fraud-synthetic-transactions.json) contains
256 synthetic transactions, including 16 hand-calculated boundary cases. Review,
block-and-review and decline thresholds are 20, 50 and 80. An independent JavaScript
business oracle asserts every score, outcome and matching rule, then verifies the
persisted decision and rule explanations. The audit also exercises typed-input
validation/normalization, trigger nonmatch, different policies for the same object
ID in separate tenants, foreign-resource denial, async and scheduled workers,
callback receipts and real case delivery/review in both tenants. It then registers
a third tenant after startup and proves publication, evaluation, async work and
callback delivery without restarting the API, worker or provider.

Raw requests, per-transaction results, container identities and the acceptance
report are retained in `evidence/mysql-fraud`. These are functional decision-API
tests with synthetic input; the supplied risk score is an input feature. The
port 8081 ingestion host has its own configured tenant/object-type scope.

## Live HTTP latency benchmark

With a passing synthetic fraud fixture and the Docker stack running:

```powershell
node scripts/mysql-http-performance.mjs
node scripts/mysql-http-performance.mjs --duration 20 --rates 1,5 --concurrency 8
```

The default test offers 10, 25 and 50 requests/sec for thirty seconds each, after
persisted warm-up decisions. It uses HTTP keep-alive, limits in-flight requests
to eight, and validates every completed decision against the independent fraud
oracle. Timing ends after the full synchronous decision response, including SQL
persistence and durable outbox production; asynchronous delivery completion is
outside that timer. All background workers remain running. Each test retains
synthetic decisions rather than deleting or resetting application data.

Reports, per-request samples, resource samples and raw Docker statistics are
saved under `evidence/mysql-http-performance`. `measurementValid` means the
measurement completed with stable container identities and successful warm-up;
`offeredLoadMet` separately checks whether every scheduled request was sent and
validated, and `targetMet` checks p95 <= 50 ms without errors or client skips.
Skipped requests were not sent because the client reached its in-flight cap;
they are not successful requests or server errors. Excessive errors/skips stop
the load ramp. Timed-out mutations are recorded as uncertain and never retried.

The initial live Docker measurements **did not meet 50 ms**. In the thirty-second
ramp, evaluation p95 was 379 ms at 10 offered requests/sec, 420 ms at 25/sec and
992 ms at 50/sec; client skips occurred at every stage. A separate lower-load
follow-up had no errors/skips: at 1 request/sec, p50/p95 were 146/254 ms, and at
5/sec they were 182/692 ms. Short runs on the shared development host varied;
these measurements establish neither a production capacity limit nor a latency
improvement. The model-read control alone measured p50/p95 of 45/93 ms, while
health-check p95 was 6 ms. The model/SQL path needs profiling before optimization.

`--profile true` (the default) requests local `Server-Timing` diagnostics and
requires them on successful evaluations. Reports retain request-local stages
and stage percentiles. Outer stages include substages, so do not add all stage
durations together. `--profile false` measures ordinary responses without the
diagnostic header. Timing disclosure is enabled only by the local composition;
the production composition does not enable it from a caller header alone.

### 500 transactions/sec investigation

The generator accepts offered rates up to 500/sec, up to 128 in-flight requests,
and a total scheduled-request budget of 30,000. Warm-up opens at most eight
connections before paced traffic; it fails acceptance if any warm-up fails.

```powershell
node scripts/mysql-http-performance.mjs --duration 20 --rates 5,25 --concurrency 64
node scripts/mysql-http-performance.mjs --duration 5 --rates 500 --concurrency 64
node scripts/mysql-sql-profile.mjs 20
```

The SQL profiler reads cumulative counters without resetting them. It includes
all services, records account-level activity, and flags digest loss. Digest
counts can omit statements; snapshot windows include Docker overhead. These
numbers do not measure rule execution CPU or attribute all database time to the
decision API.

Maintenance now checks the native expired-lease and terminal-retention indexes
before opening tenant owner transactions. The actual owner still authorizes
and revalidates recovery/deletion. Native race-enabled tests verify indexed
plans, real lease rescue, retained receipts, retention periods and revocation
after positive discovery. This reduces unnecessary maintenance transactions;
it has **not demonstrated an HTTP latency or throughput improvement**.

Matched short runs used the same five rules, synthetic fixture, 64-request cap
and unchanged resource limits (API one CPU/eight SQL connections, MySQL two CPUs).
At 5 offered requests/sec, p95 changed from 146 ms before to 200 ms after. At
25/sec, observed completion rate changed from 24.2/sec to 17.8/sec; the latter
had 18 timeouts and 99 client skips. Shared host/background activity varies,
so these results establish neither a causal regression nor an improvement.
The separate burst warm-up failure is retained and excluded from this comparison.

The five-second 500/sec attempt offered 2,500 requests: **138 sent, 134 validated,
4 timed out, 2,362 unsent**. It observed **18.8 completed evaluations/sec** over
the phase and response drain, with successful-response p95 **4,628 ms**. Timed-out
mutations have uncertain commit outcomes and are never retried. Measurement
validity passed; offered-load and latency acceptance failed. This is not a
500-TPS capacity result. Asynchronous delivery completion is still outside the
timer. The detailed evidence and limitations are in
[optimization analysis](evidence/mysql-http-performance/2026-11-06T12-13-53.544Z/optimization-analysis.json).

The next performance candidates are version-validated model and compiled-rule
caches, fewer SQL round trips/batched writes within the existing atomic owner
transaction, and measured API/worker/pool sizing against the shared database.
Any cache must retain tenant isolation, current authorization and publication
fences. A longer representative run with bounded delivery backlog is required
before claiming sustained 500 completed evaluations/sec or p95 <= 50 ms.

### Evaluation path changes

Pinned MySQL evaluations load workflow routing, screening configuration and
scoring configuration in one fresh, authorized, read-only snapshot. Structured
workflow routing retains precedence. Each repository keeps its projection
bounds, and the complete routing snapshot has a byte budget. The snapshot is
released before evaluation/provider work and is not retained between requests.

API tenant admission uses one indexed point read. Every actual owner operation
still rechecks authorization/tenant activation in its own transaction. The local
API, worker and provider pools opt into the driver's UTF-8 parameter encoder to
reduce one-shot SQL round trips; other compositions retain the default. JSON
column values must use `mysqlstore.JSON`. Byte-exact archived payloads remain
binary. This distinction is covered by receipt and scoring regression checks,
including the actual case workflow/review owner under interpolation.

`sh scripts/test-mysql-optimization.sh` runs evaluator/runtime checks, the routing
snapshot check, scoring result fencing and shared connector checks against the
private Compose fixture. Additional actual case-owner coverage uses
`DECISION_MYSQL_TEST_INTERPOLATE=1` with
`testdata/mysql-interpolated-owner-pattern.txt`. Source audit regeneration is
recorded with the native evidence; the database catalog and SQL fragments did
not change.

For repeatable comparisons, `--fixture <passing-fraud-evidence-directory>` pins
the retained tenant/scenario. This avoids changing fixtures when a later fraud
audit creates a new scenario. Use identical rates, duration, diagnostic setting,
connection cap and container limits for the compared runs. Stop disposable test
fixtures before measuring load.

`docker-compose-mysql.performance.yml` is an optional CPU experiment (API two
CPUs, shared MySQL four). Add it after the base Compose file, with the same three
generated env files. The standard limits remain API one CPU/MySQL two; connection
and memory limits are unchanged. Resource experiments must be reported separately
from code comparisons. Intermediate timings from builds with failed delivery
acceptance are retained as investigation evidence, not accepted performance gains.

The November 6 routing/transport build passed the complete endpoint and synthetic
fraud audits. Its
accepted measurements use the same retained five-rule scenario and standard
CPU/memory limits as the before measurement:

| Offered rate | Before p50 / p95 | Final run p50 / p95 | Final repeat p50 / p95 |
| --- | --- | --- | --- |
| 5/sec | 82 / 149 ms | 84 / 135 ms | 55 / 100 ms |
| 25/sec | 458 / 990 ms | 1,757 / 3,319 ms | 90 / 607 ms |

The first final 25/sec phase had two timeouts and six client skips. The repeat
had none; both 5/sec phases had none. This variation does not establish a
consistent end-to-end latency improvement. The earlier candidate's 90 ms p95
was measured before delivery acceptance was complete and is not an accepted gain.

That build's final five-second 500/sec attempt scheduled 2,500 requests: **209 sent and
validated, zero errors among sent requests, 2,291 unsent**. It observed **34.1
validated responses/sec** including response drain, with p95 **2,899 ms**.
The earlier accepted short attempt observed 18.8/sec, but neither short burst
establishes sustained capacity. **500 TPS and p95 <= 50 ms remain unmet.**
All intermediate measurements, exclusions, native checks and both final repeats
are linked in the [final optimization analysis](evidence/mysql-http-performance/2026-11-06T14-27-33.700Z/optimization-analysis.json).

`node scripts/mysql-queue-snapshot.mjs <tenant-uuid>` reports bounded available/
running counts for local effect queues without reading payloads. Counts at the
500-row cap are lower bounds. The retained post-load snapshot shows these queues
drained for the benchmark tenant; it does not certify sustained delivery capacity.

## Historical aggregate indexes and measurements (2026-11-06)

Publication preparation now discovers nested aggregate `List`, `And`, `Or` and
`Not` filters, including case-insensitive function names. It requests separate
index candidates for independent OR branches and orders equality/IN columns
before range/residual columns. Boolean expansion is limited to 64 candidates per
aggregate before contacting the data-model owner. This is index preparation,
not a guarantee that every predicate can use an index; broad windows and negated
conditions still require query-plan review.

For text equality without an owner-published digest index, SQL now includes an
unwrapped, indexable comparison alongside the authoritative complete binary
comparison. This permits existing owner-managed text-prefix/composite indexes
to narrow account/time scans while preserving case, trailing spaces and values
beyond a shared prefix. Existing digest indexes retain their exact residual.
No physical DDL or owner grants changed. New index requirements are requested
through publication preparation; deployment does not create them automatically.

The [native history report](evidence/mysql-integration/2026-11-06T15-25-25Z/velocity-summary.json)
uses 50,008 records, a hot account containing approximately 10% of history and
the same account-prefix/time index for both predicates. Median SQL times were:

| Historical function | Previous predicate | Indexable predicate | Matching rows visited after |
| --- | --- | --- | --- |
| Count over five minutes | 82.6 ms | 1.4 ms | 31 |
| Sum over one hour | 93.5 ms | 4.7 ms | 361 |
| Distinct recipients over 24 hours | 143.1 ms | 28.9 ms | 5,001 |

`EXPLAIN ANALYZE` records full scans of 50,008 rows before and composite index
range scans after. The complete reader, including authorization and published
registry validation, observed median times of 13.8 / 12.2 / 30.3 ms respectively.
These are ten warm sequential samples after three SQL warmups per variant in a
disposable consumer-contract fixture. They are not HTTP latency, owner DDL
acceptance, ingestion throughput or sustained concurrent capacity results.

The fixture also runs actual AST velocity-rule evaluations against an independent
history oracle and checks half-open window boundaries, future records, exact
long-prefix/Unicode/case/space matches, empty results, payload-specific cache
keys, tenant isolation, cancellation and the publication barrier. Native
publication rollback and PostgreSQL/HTTP contract parity pass using the same
parameter interpolation setting as the local runtime. Evaluator/publication
race tests and full-module vet pass. The updated regression report merges the
prior baseline with these focused reruns: 574 top-level passes, 1,555 test/subtest
passes, five explicitly optional skips and no missing tests or race warnings.

Use `scripts/test-mysql-aggregates.sh` with the existing disposable differential
Compose test runner, then run `node scripts/summarize-mysql-velocity.mjs
<evidence-directory>` to extract the retained plans and samples. The native
pattern is `testdata/mysql-aggregate-native-pattern.txt`; set
`DECISION_MYSQL_TEST_INTERPOLATE=1` on the test container for the local transport.

The 24-hour distinct calculation still reads thousands of matching records.
The subsequent batching, admission budgets and exact summaries below address
additional costs. **The index measurements do not establish 500 TPS
or end-to-end p95 <= 50 ms.** The earlier HTTP capacity results above remain the
last measured HTTP workload.

## Batched historical reads and bounded evaluation

The local runtime groups actual aggregate demands for the same resolved table
and filter tree within one evaluation. Up to sixteen compatible expressions use
one SQL aggregate query and one fresh authorized registry transaction. Triggers,
short-circuited branches and snoozed rules are not prefetched. Existing identical
query deduplication remains request-local; historical results are never shared
between requests, tenants or payloads. Filter/cache keys use fixed-size SHA-256
digests instead of retaining hex copies of complete parameter payloads.

Each API/worker runtime owns four aggregate admission slots against its existing
eight-connection pool. Admission and execution share a two-second deadline;
the collection window is 150 microseconds. Rule concurrency is four. These are
per-process budgets, so replicas must still fit the shared database budget.
Changing a separate runtime cannot replace another runtime's semaphore.

Validated expression plans use a bounded 256-entry cache with an 8 MiB weight
budget. Keys include tenant, complete pinned iteration content, current model
fingerprint and expression content. Draft/model edits invalidate reuse. Mutable
constant containers are detached from returned evidence. Authorization, current
registry gates, workflow configuration and evaluation results remain fresh.

Persistence reuses iteration bindings only for decisions inserted in the same
owner transaction. New effect resources are already exclusively locked by their
inserts and have bounded persisted-state readback, so they no longer require a
second owner locking query. Receipt reconciliation, delivery enqueueing, commit
durability, rollback and the existing paths for older resources remain required.

Exact ingestion-owned minute summaries accelerate eligible count/integer-sum
queries. Summary interiors and raw boundary fragments use one SQL statement.
Other aggregates, unsupported filters, long groups and stale versions fall back
to ordinary SQL. Definitions are explicitly configured through the ingestion
installer; the default float demo amount is not converted or approximated.
See [ingestion summary setup](../ingestion-service/MYSQL_DOCKER.md#exact-velocity-summaries).

The [retained native profile](evidence/mysql-integration/2026-11-06T21-53-50Z/throughput-summary.json)
uses 50,000 records over one hour, approximately 10% on one hot account, and
count/sum/count over the same half-open window. Median complete reader times:

| Path | Median | p95 |
| --- | --- | --- |
| Three separate reads | 209.1 ms | 388.8 ms |
| One batched read | 97.7 ms | 142.6 ms |
| One batch with exact summaries | 21.3 ms | 34.7 ms |

The eight-connection fixture also compared two/four/eight aggregate slots with
32 concurrent evaluation groups and ordinary control reads. Four slots completed
the burst in 357 ms versus 360 ms with eight; control-read p95 was 4.1 ms versus
26.1 ms. Two slots took 584 ms with 2.6 ms control-read p95. All returned valid
results without errors. Four is a measured local starting point, not a universal
optimum. The profile retains all samples and actual aggregate batch counts.

These are race-enabled native fixture observations with contract authorization,
not HTTP or ingestion throughput. Live authorization, decision persistence and
effect delivery require their separate audits. The workload is denser than the
earlier 50,008-row index experiment; the two tables are not interchangeable
benchmarks. No 500 TPS or end-to-end p95 <= 50 ms claim follows from these numbers.

### November 7 live rollout and HTTP check

All five changes above are installed in the local MySQL images. Ingestion's
retained 23-step migration prefix is unchanged; its two new v006 steps applied
successfully and a second installer run passed. Ingestion API/worker and decision
API/worker/private-provider roles are healthy. The default float-valued demo model
has no summary definition; its ordinary SQL behavior is preserved.

The final image passes [25 decision endpoint suites](evidence/mysql-endpoint-audit/2026-11-07T08-33-52.932Z/report.json),
covering all 120 documented and 130 mounted operations, and the
[seven-suite synthetic fraud audit](evidence/mysql-fraud/2026-11-07T08-32-05.298Z/report.json)
verifies all 256 transactions. The [ingestion audit](../ingestion-service/evidence/mysql-endpoint-audit/2026-11-07-throughput/report.json)
passes nine suites and all 15 documented operations. Native regression inventories
account for 580 decision passes plus five explicitly optional skips and 250
ingestion passes; their reports retain all initial failures and focused repairs.

Fresh HTTP results use the same retained five-rule scenario and standard resource
limits as the older runs. This scenario has no historical aggregate rules, so it
does not exercise the summary speedup above. The completed synchronous response
includes model retrieval, evaluation, SQL persistence and durable event production;
asynchronous delivery completion is measured separately.

| Offered rate | Offered / sent / validated | Errors / unsent | Response p50 / p95 |
| --- | --- | --- | --- |
| 5/sec for 20 seconds | 100 / 100 / 100 | 0 / 0 | 110.8 / 184.4 ms |
| 25/sec for 20 seconds | 500 / 419 / 401 | 18 / 81 | 2678.4 / 4267.1 ms |
| 500/sec for 5 seconds | 2500 / 156 / 155 | 1 / 2344 | 2420.1 / 4190.3 ms |

The [5/25 profile](evidence/mysql-http-performance/2026-11-07T08-38-14.578Z/report.json)
and [500/sec attempt](evidence/mysql-http-performance/2026-11-07T08-40-25.260Z/report.json)
retain raw requests, server timing and Docker resource samples. Both measurements
are valid observations, and both fail the offered-load/latency target. The latter
completed 21.9 validated responses/sec including response drain. Percentiles cover
successful responses only; errors and unsent requests must not be omitted when
assessing capacity. These short runs show no consistent end-to-end improvement.
At 5/sec, median rule execution was 0.9 ms while model retrieval and persistence
were 29.0 ms and 46.1 ms. At 25/sec, transaction-begin, tenant-authority and
registry-validation timings rise sharply; shared MySQL samples reach its two-CPU
limit under the 500/sec attempt.
Those stages are nested, so their percentiles cannot be added.

The [post-load queue snapshot](evidence/mysql-queue-snapshots/2026-11-07T08-41-37.703Z/report.json)
shows no available/running outbox, workflow, screening or scoring work for the
benchmark tenant. It is a point-in-time transport check, not an end-to-end delivery
latency metric. **500 completed evaluations/sec and p95 <= 50 ms remain unmet.**
