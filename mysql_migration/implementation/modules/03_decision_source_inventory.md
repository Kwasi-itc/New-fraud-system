# Decision-engine source inventory and first implementation slice

This inventory describes the current PostgreSQL implementation. It does not
approve a MySQL physical schema, grant broad cross-owner writes, or establish
cross-engine parity. Runtime remains PostgreSQL/River.

## Source coverage and drift control

The checked-in [machine inventory](../../../backend/decision-engine-service/internal/store/postgres/testdata/postgres_inventory.json)
records every production Go file under `internal/store/postgres`, `internal/ports`
and `internal/tenantdata`, and every metadata up/down migration. Entries include
SHA-256, lexical qualified references, PostgreSQL feature markers and Scan-call
counts. `TestDecisionPostgresSourceInventory` rejects changed, added or removed
files until the inventory is explicitly reviewed and regenerated:

```sh
cd backend/decision-engine-service
go test ./internal/store/postgres -run TestDecisionPostgresSourceInventory -args -update-postgres-inventory
```

This is drift detection, not a SQL parser or automatic ownership inference.
Computed identifiers and concatenated SQL need the manual contracts below.
The earlier [schema object index](../../14_schema_object_index.md) and
[SQL catalog](../../12_schema_source_catalog.md) remain reference inputs;
the new inventory ties this implementation slice to the actual working source.

## Ownership and access groups

| Group | Current implementation | Migration contract / remaining work |
| --- | --- | --- |
| Scenario/rule authoring | scenario, scenario_iteration, publication and rule repositories; publication/live iteration relationships | Preserve version allocation, committed iteration rules, tenant binding, uniqueness and publication atomicity. Translate RETURNING, JSONB and nullable UUID/time scanners explicitly. |
| Decision and rule evidence | decision, rule_execution, phantom_decision, phantom_rule_execution and testrun repositories | Keep outcome/score/request/evidence unchanged; batched writes and all-or-nothing rollback; tenant-authorized evidence reads through the decision parent. |
| Historical tenant reads | tenant_data_reader and tenantdata/reader_factory; AST compiler/evaluator consumes the port | Resolve published registry/model ownership instead of guessing MySQL table names. Preserve filter/group/operator, numeric and null contracts; direct and HTTP modes need differential tests. Current fallback selection must not silently choose a different authority in target wiring. |
| Workflow authoring and ordered snapshots | workflow, workflow_rule/condition/action, workflow_authoring and workflow_snapshot | Replace PostgreSQL ordered JSON aggregation with bounded ordered assembly in one read snapshot. Preserve 501-row sentinel/500-parent cap and existing child ordering; no N+1 history hydration. Review scenario-parent locks rather than copying broad serialization blindly. |
| Execution lifecycle and queue work | scheduled/async/workflow_execution, screening/scoring/outbox repositories; seven River job kinds | Typed owner-scoped mutation enqueue is implemented. Staged target SQL adds initial receipts/jobs atomic with the four effect owners plus scheduled/async/callback owners. Corrected calendar contract and D23.1–2 scoped execution transport exist. D23.3–4 now add seven-kind expired-lease recovery, distinct execution cancellation and bounded terminal transport cleanup retaining receipts/owners; native checks passed, new SQL/migration/concurrency/plans await the user's Docker window. Calendar public adapter, four-effect domain cancellation/retry/redrive, real handlers/providers and runtime composition remain pending. Preserve domain attempts separately from transport attempts and error history; use the current module checklist for exact source-scoped evidence. |
| Screening/scoring configuration | screening_repository, scoring_repository | Preserve allowed-outcome arrays and configuration JSON, tenant-consistent parent references and result state. Target relational array representation and grant/constraint mapping still require review. |
| Screening source projection | decision-owned `core.reconcile_screening_source`; screening source/queue inputs | P-04 owner routine must validate tenant/decision/config/scenario binding, monotonic version, source-before-queue lock ordering and caller transaction; target implementation depends on the screening contract. |
| Platform effects | platform_repository: lists, tags, risk snapshots and IP flags | Exact text comparisons, scoped reads/writes and append/update semantics; verify indexes and numeric/JSON/text mappings without incidental domain redesign. |
| History/search/pagination | decision_repository filtering/counts/offset/cursor queries; rule snoozes | Preserve case/wildcard/null behavior and equal-time ID ordering. Binary UUID ordering must match the cursor contract. Representative MySQL plans are still needed. |
| Shared investigation contracts | investigator adapter and shared investigator module | Preserve trusted actor/tenant authorization and owner routine boundaries; no arbitrary case/screening mutations. Auth/case target routines are external dependencies, not successful placeholders. |

## Portable boundary now implemented

`MutationStore.Enqueue(JobRequest)` covers scheduled evaluation, async evaluation,
async callbacks, workflows, screening, scoring and outbox publication. The kind
is an explicit owner operation, not arbitrary SQL/queue configuration. The
PostgreSQL adapter validates nonzero identities and binds tenant/resource using
an indexed owner-row lookup in the same transaction before River insertion.
This adds a per-enqueue lookup; no performance improvement is claimed. API and
worker use the same configured producers/pool; no extra application DB is added.

Services and their mutation ports no longer import pgx/River. Missing required
producers and queue-client initialization errors propagate. No-op producer types
and non-atomic retry fallbacks are removed. Optional integration publication still
creates neither event nor job when disabled; other required work is unaffected.

Execution completion/failure state, callback jobs and enabled lifecycle outbox
events now commit together. Recovery after a failed transition resets only the
same tenant/resource/running attempt; a newer attempt or committed terminal
result survives an uncertain acknowledgement. This does not make all evaluation
effects exactly once or implement full stale-worker fencing.

## Baseline of the first portable slice (historical scope)

The baseline corpus exercises actual service evaluation around configured score
thresholds, including no-hit evidence and request/response persistence inputs.
Real PostgreSQL fixtures cover every job kind, tenant/resource binding, schedule
preservation and rollback on later callback/queue/cancellation/panic failures.
Existing optional-outbox, async-validation and rule-evidence tests remain active.

The initial portable slice described in this baseline did not implement the main MySQL decision repositories/readers or
least-privilege production installation, full golden AST/numeric/null/collation/aggregate
differential coverage, all-kind worker crash/rescue, import/redrive, persisted
schedule watermarks or query-plan/performance acceptance. In particular, existing
River retained-argument deduplication and explicit retry/reset identity need a
separate lifecycle review; moving enqueue behind a port does not certify redrive
correctness. Decision startup/readiness now checks pinned River lineage, required
objects and runtime permissions in the explicit public schema, without repairs.
Complete schema-drift auditing and target MySQL queue readiness remain pending.
The cross-owner dependencies in DE-01 remain open. No EC2, application database,
runtime cutover or Aurora test is part of this pass.

Later target work and its exact implementation/testing/dependency statuses are
tracked in [the current module checklist](03_decision_engine_service.md#granular-execution-plan-five-tasks-per-batch).
Calendar schedules are not equivalent to fixed intervals: source supports named
timezones and hourly/daily/weekly/monthly frequencies. The user approved the
corrected target contract on 2026-10-30: local weekday, independent month-end
clamping, skipped/counted nonexistent times and one earlier-instant occurrence
for overlaps. Pure planner race tests passed; calendar SQL code/tests are staged
with execution status in the checklist. Source/public DTO adaptation and runtime
activation remain blocked; this does not silently change the PostgreSQL scheduler.

## Full owner mapping and review checkpoint

The [full proposed mapping](../../../backend/decision-engine-service/internal/migrationaudit/testdata/decision-schema-mapping.json)
is extracted from all 22 decision migrations applied to a freshly generated,
private PostgreSQL 16 database. It covers 29 owner tables, 284 columns, 73
constraints, 71 indexes, two routines and one trigger. This is not a count of all
tables in the shared application database: data-model, identity, screening, case
and River objects have different owners and are listed as dependencies instead.

Every column has a target representation, null/default/codec/reverse-transform
proposal and preflight/verification requirements. Constraints retain source
definitions and reference bindings; indexes retain expressions, predicates and
ordering. Routines and triggers retain full source bodies rather than guessed
translations. Query records carry SQL fragments, source locations and manual
access contracts. Column/index query associations are candidates, not a parsed
call graph or proof that a plan uses an index. Production data, installed grants
and deployment-specific collations have not been inspected.

The final artifact ties 183 query/access records to 157 source files, including
all 44 migration up/down files and the shared investigator dependency. Verification
and version/setup limitations are recorded in the
[section 1/2 evidence](../evidence/2026-10-29_decision_mapping_and_platform.json).

Regenerate only after reviewing source/design changes, inside the disposable
PostgreSQL fixture (from the decision service directory):

```sh
go test ./internal/service -run TestIntegrationDecisionSchemaManifest -args -update-decision-schema-manifest
```

`DECISION_ENGINE_REQUIRE_INTEGRATION=1` makes missing fixture configuration fail.
The normal unit suite detects source/proposal drift; the real-PG test compares
the review artifact with a fresh migration catalog. Neither installs target
MySQL objects nor accepts existing deployed objects as correct.

### Approved choices (2026-10-29)

All nine choices below were reviewed with the user. Optional links retain
tenant-aware foreign keys in separate relationship tables; exact-key dictionaries
are decision-owned. List names may use explicitly documented MySQL
case-insensitive behaviour (mostly English names), without exact PostgreSQL
case-fold emulation or an English-only input restriction. This is a fresh
development installation: no legacy-data backfill/import tooling is required.
Tenant/scenario/iteration constraints still apply to every new row.

Ordered outcome children preserve order, duplicates and NULL values. Explicit
UTC timestamps during processing are acceptable; do not add request-boundary
checks or reproduce PostgreSQL transaction-start clock behaviour. Dedicated
version counters and authorized tenant/resource/attempt-bound worker operations
are approved. Cross-owner contracts and their grants must be reviewed when needed,
not bypassed or implemented as successful no-ops. These approvals do not activate
MySQL or imply that the corresponding code/tests are complete. The machine
manifest remains the earlier proposed snapshot until its next reviewed update.

### Original recommendations and implementation requirements

1. **Nullable relationships:** a composite FK with `tenant_id NOT NULL` cannot
   safely copy `ON DELETE SET NULL` onto every key column. The proposal uses
   optional relationship rows for the live iteration and async scenario link.
   Deleting a referenced iteration/scenario removes that relationship while the
   owning record remains, with API NULL reconstructed by a left join. These
   auxiliary relations and their query/delete behaviour are not implemented.
   See [MySQL foreign-key actions](https://dev.mysql.com/doc/refman/8.4/en/create-table-foreign-keys.html).
2. **Exact uniqueness:** scenario names and nonempty execution idempotency keys
   are unbounded source text. They need a collision-safe owner dictionary and
   database-enforced raw/key binding; a unique digest or prefix is insufficient.
   Decision must not arbitrarily write data-model's private dictionary tables.
3. **Case-folded list names:** source `lower(name)` uniqueness is not proven
   equivalent to MySQL `LOWER` or a default case-insensitive collation. Profile
   and define source-locale/Unicode compatibility before choosing normalization.
4. **Tenant and scenario binding:** many source FKs contain only resource IDs;
   rule/phantom evidence lacks a stored tenant column. Proposed composite keys
   and derived tenant/iteration fields strengthen those relationships, but need
   orphan/cross-tenant profiling and explicit backfill/rejection policy first.
5. **Array fidelity:** allowed outcomes become ordered child rows, preserving
   duplicates and NULL elements. Source outer arrays are NOT NULL, but their
   element nullability, dimensions and lower bounds still need data profiling.
6. **Defaults and clocks:** PostgreSQL `now()` is transaction-start time;
   `clock_timestamp()` is current time. Neither may be replaced blindly with a
   MySQL statement-start timestamp. Owners must bind the appropriate explicit
   instant, especially after authorization/lock waits and for expiry/versioning.
   See [PostgreSQL date/time function semantics](https://www.postgresql.org/docs/16/functions-datetime.html).
7. **Version allocation:** `MAX(version)+1` is not a concurrent allocator on its
   own. Choose a narrow scenario/version allocation guard plus unique version
   enforcement, preserving signed INT overflow and operation replay semantics;
   do not serialize all tenant activity through a parent tenant/scenario lock.
8. **Worker versus API scope:** several source worker status mutations are
   ID-only and scheduling enumerates tenants globally. Target APIs remain
   tenant-bound; workers need an explicit authorized system capability and
   verified resource tenant/attempt/lease before effects, not a fake tenant ID.
9. **Cross-owner contracts:** identity authorization and case review history must
   join the caller transaction through reviewed owner SQL. Screening projection
   retains source-before-queue order and monotonic versions. Those target
   contracts and restricted grants are still dependencies, not successful no-ops.
   The source case effect is `case_manager.record_investigator_review`; identity
   calls include `identity.authorize_data_model_v1` and `identity.authorize_case_v1`.

### Permission and installation proposal

The manifest records each table's source-owner DML set, not actual deployed
grants. The three staged append/read repositories require only SELECT/INSERT on
their tables; fixture tests deny UPDATE/DELETE/DDL, ledger and other-owner access.
Future mutable repositories need operation/column-specific owner executors,
separate API/worker roles and protected history. Do not grant each runtime login
every operation observed in the source. Runtime identities cannot grant privileges,
perform DDL or bypass identity/case rules. Deployment grants and real identity
authorization have not been installed.

Proposed installation order is: reviewed owner exact-key mechanics and independent
parents; tenant-composite parent keys and child tables; optional relationships and
ordered arrays; decisions/evidence and execution/effect tables; then cross-owner
authorization/reconciliation/review contracts and explicit grants. Shared worker
objects have their own coordinated ownership. Source migration history is not
copied into the new owner lineage. The staged three-table platform plan is only
the first additive slice, not a complete decision migration or runtime activation.

### Verified platform slice and limitations

Record tags, latest risk snapshots and IP flags have transaction-/tenant-bound
MySQL repositories using the shared codecs and transaction helpers, with required
local authorization callbacks. Source IP flags are TEXT, not INET; equivalent
IPv6 spellings are deliberately not canonicalized. Hash indexes are non-unique
accelerators, always followed by full binary text predicates; forced-bucket tests
verify that collisions do not establish equality. Limits return explicit errors
and no partial list, rather than truncating values or pretending missing risk.

Exploratory tests run on cached MySQL 9.7.1/Go 1.25.0. The committed fixture remains
MySQL 8.4.8/Go 1.25.8, whose image download is currently blocked by the local
credential helper. Plans on 5,000 synthetic rows/table include 2,000 hot-key rows;
indexed lookups were observed for lists, latest risk and existence checks, but
this does not establish production throughput or the value of every extra index.
Tag/IP lists add a deterministic ID tie-break where source equal-time order was
undefined; latest risk now explicitly uses the same tie-break in both readers.

Results inside mutation callbacks remain provisional until commit succeeds.
There are no platform operation receipts or automatic uncertain-commit retries.
The adapters are not selected by runtime and do not yet compose with main decision
persistence or required MySQL jobs. Pinned-version acceptance, full cross-engine
evidence, deployment identity/grants and broader plans/load remain open. No EC2,
application database, deployment or Aurora access occurred.

### Approved staged owner-consumer contracts (2026-10-29)

Auth uses `identity_authorize_decision_v1` with trusted actor/session identity,
tenant, membership version and explicit read/author/publish action on the caller
transaction. Browser actions `decision.workflow.read`/`decision.workflow.author`
additionally retain the source's platform-admin AND case-access policy; generic
scenario-author permission is insufficient. Case inbox authorization uses
`case_manager_authorize_decision_inbox_v1`; screening projection reads the
updatable `screening_decision_source_v1` publication before locking its own
projection. These consumers fail closed. Their test providers are not AU-01,
CM-01 or SR-01 implementations, and activation stays blocked until owners supply
the real contracts and reviewed grants.

Approved batch consumers now include:

- `case_manager_authorize_decision_tags_v1(actor,tenant,session,version,ids_json,require_active,locks)`: at most 25 distinct nonzero UUIDs, sorted before invocation; boolean result. The provider checks trusted current identity, permission, same-tenant tag ownership and active state when required, with deterministic tag lock order in the caller transaction. NULL/missing provider results are unavailable, never successful authorization.
- `case_manager_visible_decision_inboxes_v1(actor,tenant,session,version,ids_json)`: read-only caller snapshot, at most 500 distinct nonzero UUIDs; JSON array containing only visible requested inboxes, no duplicates/NULL. Historical inactive inboxes remain visible when authorized. Provider enforces current case permission and tenant/session binding. The consumer bounds returned bytes before transfer and rejects malformed/foreign additions. This replaces N+1 permission lookups for browser listing.

The dedicated workflow authoring guard is acquired before workflow row/child
locks by both browser and low-level legacy/structured writers. It serializes only
same-scenario workflow edits; decision writes do not acquire it. Versioned save,
idempotent replay, audited delete and child-ID retention are staged locally;
scenario/workflow copy and runtime/provider composition remain open.

### Approved decision delivery identity (D21)

The target MySQL transport uses a separate explicit v1 envelope implemented in
`internal/mysqljobs`; it does not silently translate or enable legacy River work.
The exact reservation key is `(kind, tenant_id, resource_id, domain_attempt,
redrive_generation)`. Payload encoding version and configured queue labels are
not part of logical uniqueness. Domain attempts are positive signed INT values;
redrive generation starts at zero. Transport lease attempts/retries/rescue retain
this whole tuple. New domain retries and authorized redrives receive new tuples.
Callback attempts use their own counter, not the evaluation attempt counter.

Owner adapters must lock/recheck tenant/resource and current attempt/generation
before enqueue or effects. Allocation and corresponding owner changes/jobs must
commit together. Decoding a valid tuple is neither authorization nor proof of a
current lease. Permanent owner receipts, retained transport records, cancellation,
redrive and cleanup must be composed before runtime activation. The codec and
catalog binding alone do not implement durable jobs or start workers.

The subsequent four-owner SQL slice (`EffectJobsStatements`, 115 total additive
steps) binds receipts to actual workflow/screening/scoring/outbox rows with
same-tenant FKs. The explicit queued store atomically creates initial attempt 1,
generation 0 with those effects, in batches of at most 100. No API accepts an
arbitrary codec-valid future attempt as owner authority. Retained receipts
deduplicate even after transport removal; resource deletion remains restricted
until an explicit receipt lifecycle is supplied.

The separate delivery store requires an explicit tenant/kind/action worker
authorizer for claim/heartbeat/finish. Finish uses owner -> receipt -> delivery
lock order and checks expiry again after local SQL effects. Transport retries
reuse the same receipt and shared error-count policy. No real worker provider,
external handler, rescue/cancel, domain-redrive allocator, retention sweeper or
runtime activation is included in that four-owner slice. Subsequent D22/D23.1–2
add separate execution owner lifecycle, scoped claim/heartbeat, transport
retry/exhaustion and callback-only redrive. The four-effect adapter still rejects
execution kinds; a typed execution wrapper uses the common fixed-catalog lease
machinery without exposing generic effect completion. Subsequent D23.3–4 add
scoped expired-delivery rescue, execution-only cancellation and retained-receipt
terminal transport cleanup. Their native checks and SQL test preparation are
finished, but new database assertions are deliberately deferred. Two appended
cleanup-index ALTERs extend the unreleased execution draft to 140 steps and
require reviewed before/after shape chains. Calendar public adaptation,
four-effect domain cancellation/retry/redrive, real handlers and main runtime
composition remain open. The seven-kind codec is not complete handler activation.

The data-model physical registry provides table-scoped publication UUIDs, open
barriers and registered field names/types. Approved v005 additionally exposes
the hidden indexed object-ID mapping and a read-only resolver, without private
dictionary privileges. An approved optional text equality mapping supplies
`sha256_binary_v1` acceleration with a complete raw-value residual; generated
column/index names must be owner-published, never guessed by decision code.
Typed owner DDL, DTO validation and consumer tests exist. Persistent text-index
mapping lifecycle/publication integration and static capability preparation are
still pending; no claim of general indexed text range/prefix support is made.

History search follows approved Unicode case-insensitive/accent-sensitive MySQL
semantics. Exact object IDs/values remain binary exact. Custom-list names use
MySQL LOWER with accent/whitespace-sensitive binary comparison, rather than
claiming PostgreSQL locale equivalence or arbitrary collation expansion.
General aggregate comparisons retain SQL NULL semantics. Incoming wallet hash
and ownership validity still needs model/preparation/explicit rule requirements;
a NULL stored owner is not another account. Invalid dynamic comparison types and
oversized compiled predicates fail rather than return a successful zero count.
