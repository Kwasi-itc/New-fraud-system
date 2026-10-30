# Ingestion deployment schema and principal review

Status: candidate review, reconciled 2026-10-30. Tasks IN-01.2.016/018/019/021.
This inventories the staged source; it is not an independently approved installation.
Runtime remains PostgreSQL/River in the single shared logical application database.

## Sources and scope

Source inventory: ingestion `internal/migrations/metadata/000001_init.up.sql`
and `000002_deferred_ingests.up.sql` (five tables, 53 columns), plus the
[characterization contract](02_ingestion_characterization_contract.md).
Target authority: ingestion `internal/store/mysql/installation.go`,
`receipt_schema.go`, `effect_schema.go`, `lookup_guards.go`, `work_owner.go`,
`legacy_import.go`, `work_operator.go`, `work_inspection.go`; shared `backend/mysqlstore/work/queue.go` and
`backend/mysqlstore/migrations/runner.go`. Paths refer to repository source,
not deployed objects. The manifest currently has 23 ordered steps, 13 owned
application tables and one independently bootstrapped ingestion migration ledger.

Update to the original inventory below: `InstallationStatements` now contains
**23 steps and 13 owned application tables**, plus the independently bootstrapped
ledger. The original 20-step inventory is historical, not a deployment reference.
The appended steps are `ingestion_v004_deferred_metrics` (index on
jobs tenant_id/queue/kind/state), `ingestion_v005_current_job` (nullable resource
current_job_id with tenant-consistent job FK), and `ingestion_v005_work_redrives`.
The current-job FK creates a resource/job reference cycle that any approved
archival protocol must handle without disabling integrity constraints.

`ingestion_work_redrives` columns are action_id/tenant_id/table_id/resource_id/
previous_job_id binary UUIDs, actor_id VARBINARY(256), reason_code VARBINARY(64),
scheduled_at DATETIME(6), created_at DATETIME(6) default CURRENT_TIMESTAMP(6).
Primary key action_id identifies the new job; previous_job_id is unique.
Tenant-consistent FKs reference table, resource, previous job and new job. Checks
require distinct previous/new job IDs and nonempty actor/reason. Authoritative
definitions: `internal/store/mysql/work_redrive.go` and `work_metrics.go`.

The legacy importer CLI and `LegacyOperatorGrants` are implemented; dry-run and
import use separate privileges. `RedriveGrants`, scoped work inspection, metrics,
API status/error mapping and isolated API/worker composition are also implemented.
Earlier "pending" statements below describe the original review baseline.
Production account/IAM binding, source historical-time/payload dispositions,
outbox delivery ownership and independent approval remain unresolved. Current
component evidence is in the [reader contract](02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30)
and [operation contract](02_ingestion_operation_contracts.md).

Independent review must identify reviewer, date, decision and exact source revision
or dirty-tree source hash manifest; pin server version, all 23 ordered step IDs,
SQL checksums, reference definitions and per-role grant artifacts. Record explicit
dispositions for historical requested_at, nullable completed upload payloads,
pending outbox ownership, resource/job FK cycle and retention/replay protection.
Attach negative privilege and cross-tenant evidence; fixture success is supporting
evidence, not reviewer approval. IN-01.2.018 remains BLOCKED pending that decision.

### Fresh-install review disposition (2026-10-30)

User confirmed a new system with no existing data. Historical requested_at,
nullable retained upload payloads and legacy source attribution therefore do not
require a data-transfer path for this deployment. They must be reassessed if
existing data is introduced. Current outbox delivery ownership, least privilege,
tenant-consistent constraints, FK cycles and retention remain relevant to new data.

| Review record | Current value |
|---|---|
| Scope | Fresh installation; no legacy import or pending-work handover |
| Manifest | 23 ingestion steps; shared data-model prerequisites remain mandatory |
| Source identity | [Scoped source hashes](../evidence/2026-10-30_ingestion-remaining3/source_hashes.json); these identify inspected source, not a released build |
| Runtime authority | PostgreSQL/River; MySQL executable configuration remains isolated-fixture-only |
| Independent reviewer | Not assigned |
| Decision | Pending; no approval inferred from automated tests |
| Retention disposition | Existing default retain; first-release retain-all proposal awaits user decision |
| Operational disposition | Ingestion queue tests are component evidence; all 15 shared kinds and deployment budget still require acceptance |

Reviewers must record the exact approved reference/grant artifacts as well as
source hashes. The scoped source manifest is not a signature or a replacement
for the independently pinned server-generated reference definitions. No accounts
were provisioned or application grants changed by this documentation update.

## Object and column inventory

Unless explicitly nullable below, columns are NOT NULL. UUIDs use BINARY(16);
digests use fixed binary widths; times use UTC DATETIME(6). SQL files/functions
above remain authoritative for exact definitions and server-generated FK indexes.

| Object | Complete column inventory | Keys, indexes and relationships |
|---|---|---|
| ingestion_work_actions | id/tenant_id/table_id/resource_id/job_id UUID; actor_id VARBINARY(256); action VARBINARY(16); reason_code VARBINARY(64); result_state VARBINARY(12); changed BOOLEAN; created_at time DEFAULT CURRENT_TIMESTAMP(6) | PK id; resource_history tenant_id/resource_id/created_at/id; tenant-consistent FKs to data_model_tables, work resources and jobs; cancel-only action; terminal result states; changed implies cancelled |
| ingestion_receipt_buckets | tenant_id UUID; key_digest BINARY(32) | PK tenant_id/key_digest; FK tenant_id to data_model_tenants.id |
| ingestion_request_receipts | tenant_id UUID; key_digest BINARY(32); slot SMALLINT UNSIGNED; request_key LONGBLOB; table_id UUID; mode VARBINARY(6); response_kind VARBINARY(6); request_hash BINARY(32); created_at time DEFAULT CURRENT_TIMESTAMP(6); response_payload LONGTEXT utf8mb4_0900_bin | PK tenant_id/key_digest/slot; FK bucket tenant/digest; FK data_model_tables tenant/id |
| ingestion_execution_buckets | Same columns as public receipt buckets | Same PK/tenant FK; separate private namespace |
| ingestion_execution_receipts | Same columns as public receipts | Same key/constraint shape; FK private bucket; FK data_model_tables tenant/id |
| ingestion_audit | id/tenant_id/table_id UUID; object_type/object_id/revision_id LONGTEXT; mode VARBINARY(6); status VARBINARY(17); payload/validation_errors JSON; idempotency_key LONGBLOB NULL; created_at time | PK id; history tenant_id/table_id/created_at DESC/id; FK data_model_tables tenant/id |
| ingestion_outbox_events | id/tenant_id/table_id UUID; event_type VARBINARY(25); aggregate_type/aggregate_key LONGTEXT; payload JSON; status VARBINARY(7); created_at time | PK id; pending status/created_at/id; history tenant_id/table_id/created_at DESC/id; FK data_model_tables tenant/id |
| ingestion_lookup_guards | tenant_id/table_id UUID; key_digest BINARY(32) | PK all columns; FK data_model_physical_tables tenant_id/table_id, DELETE RESTRICT |
| ingestion_work_resources | id/tenant_id/table_id UUID; object_type LONGTEXT; kind VARCHAR(48) ascii_bin; mode VARBINARY(6); payload LONGBLOB; request_key LONGBLOB NULL; filename/content_type LONGTEXT; scheduled_at time; result_payload JSON NULL; total_rows/successful_rows/failed_rows INT DEFAULT 0; created_at time DEFAULT CURRENT_TIMESTAMP(6); completed_at time NULL | PK id; UNIQUE tenant_id/id; history tenant_id/table_id/created_at DESC/id DESC; upload_history tenant_id/table_id/kind/created_at DESC/id DESC; FK data_model_tables tenant/id |
| ingestion_work_jobs | id/tenant_id/resource_id UUID; kind VARCHAR(48) ascii_bin; queue VARCHAR(128) ascii_bin; payload_version INT; payload JSON; state VARCHAR(12) ascii_bin; available_at time; attempt INT DEFAULT 0; max_attempts INT; lease_token UUID NULL; lease_until time NULL; created_at time DEFAULT CURRENT_TIMESTAMP(6); started_at time NULL; finished_at time NULL; last_error VARCHAR(256) NULL | PK id; UNIQUE tenant_id/id; ready queue/state/available_at/id; expired state/lease_until/id; terminal state/finished_at/id; resource tenant_id/resource_id/created_at/id; operator_history tenant_id/queue/state/created_at DESC/id DESC; FK resources tenant/id |
| ingestion_work_reservations | tenant_id/resource_id/job_id UUID; kind VARCHAR(48) ascii_bin | PK tenant_id/kind/resource_id; UNIQUE job_id; FK jobs tenant/id |
| ingestion_legacy_imports | tenant_id UUID; key_digest BINARY(32); slot SMALLINT UNSIGNED; source_sha1 BINARY(20); source_evidence_sha256/original_response_sha256 BINARY(32); imported_at time DEFAULT CURRENT_TIMESTAMP(6) | PK tenant_id/key_digest/slot; FK public receipt same key |
| ingestion_schema_migrations | step_no INT; step_id VARCHAR(64) ascii_bin; checksum BINARY(32); state VARCHAR(8) ascii_bin; started_at time; applied_at time NULL | PK step_no; UNIQUE step_id; separately bootstrapped by shared runner |

Checks: receipts require nonempty key, create/patch mode, single/batch kind and
valid JSON response. Audit checks mode, succeeded/validation_failed status,
object payload and array errors. Outbox checks three supported event types,
pending-only status and object payload. Resources check two supported kinds,
mode and nonnegative counters with successes+failures <= total. Jobs check
positive payload version, nonnegative attempt, max_attempts 1..100, five states,
lease presence iff running, finish time iff terminal, and object JSON payload.
Ledger state is dirty/applied. Other value/payload limits are application checks,
not SQL CHECK guarantees. No cascade-delete policy is installed.

## Source-to-target reconciliation and freeze blockers

| Source | Target mapping and unresolved differences |
|---|---|
| ingestion_audit: all 12 columns | Same logical fields plus table_id; TIMESTAMPTZ becomes UTC DATETIME(6), UUID binary, JSONB JSON, key text bytes. Source JSON defaults become explicit owner inputs. Text type history becomes stable table-ID history. |
| outbox_events: all 8 columns | Same logical fields plus table_id. Source pending default becomes explicit input. Target pending-only CHECK cannot support consumer delivery-state updates yet: delivery owner/contract must be established before freezing. |
| idempotency_keys: tenant_id/key/request_hash/response_kind/response_payload/created_at | First five map to public receipt tenant/key/canonical hash/kind/result with additional table/mode/digest/slot. Legacy SHA-1 requires evidence-checked import, not relabeling. Receipt created_at now retains the original source creation time through authorized import; provenance imported_at separately records import time. Missing, out-of-range or sub-microsecond source times fail import instead of being guessed or rounded. |
| upload_logs: identity/object/mode/file/content/payload | Resource id/tenant/object/mode/filename/content_type/payload; target adds stable table, kind and schedule. Source nullable payload requires a retained completed-history policy; target input is NOT NULL. |
| upload_logs: status/attempt_count/error_message/row counters | Job state/attempt/redacted last_error and resource counters. API enum/error mapping remains unimplemented; redacted code is not verbatim source error_message. |
| upload_logs: requested_at/started_at/completed_at | Resource created_at is potential requested_at mapping only if explicitly imported; job.started_at records the latest claim, survives heartbeat/retry and is replaced on the next claim; completion uses resource.completed_at or terminal job.finished_at. |
| deferred_ingests: id/tenant/object/mode/idempotency_key/payload | Resource identity/type/mode/request_key/payload, with kind deferred_ingest_execute; JSONB becomes raw bounded JSON bytes, not arbitrary CSV. |
| deferred_ingests: status/attempt_count/error_message/requested_at/started_at/completed_at | Same work-state and historical requested_at mapping gaps as uploads. Latest-attempt started_at is implemented; transfer of historical pending/terminal times still requires import tooling. Source status/requested index needs a mapped metrics query plan; do not infer equivalence from queue ready index. |

IN-01.2.016 remains IN_PROGRESS: inventory/reconciliation is complete, but source
historical requested_at import, nullable historical upload payload, delivery-state ownership,
status/error mapping and metrics access need explicit dispositions before freeze.
Do not generate a purported final reference artifact from unresolved definitions.
Changes after accepted freeze must append migration steps, not rewrite history.

## Principal separation review (IN-01.2.019)

This records the intended separation and compares the actual staged grant helper.
It does not install accounts, approve production grants or satisfy IN-01.2.022.
All principals operate in one shared database. Database grants do not provide
row-level tenant authorization: API/worker owner contracts remain mandatory.

| Principal | Required authority | Explicit exclusions / implementation status |
|---|---|---|
| Installer | CREATE/ALTER on ingestion-owned tables; ledger read/insert/update; metadata visibility; REFERENCES on explicitly referenced data-model objects; migration advisory locks | No API use, GRANT OPTION or general application credential. Account provisioning/grant administrator is separate. Exact deployment grant artifact still pending. |
| API owner | Read published tenant/table/field/physical contracts; EXECUTE v005 lookup; publication-controlled physical SELECT/INSERT/UPDATE; receipt SELECT/INSERT; narrow bucket/guard digest locks; audit/outbox INSERT; work resource/job/reservation SELECT/INSERT | RuntimeGrants(worker=false) implements the ingestion/shared-object subset. No DDL, physical DELETE, private dictionary reads/resolver execution, receipt mutation/deletion, audit/outbox updates, legacy provenance writes or worker completion. |
| Worker | API owner capabilities plus resource result/counters/completion UPDATE; job state/schedule/attempt/lease/finish/error UPDATE; reservation DELETE; private bucket/receipt access | RuntimeGrants(worker=true) implements the subset. Immutable resource/job input and evidence updates denied. Current helper also permits creation; narrower claim-only worker separation can be reviewed without claiming it exists. |
| Record/status reader | SELECT only on explicitly mapped physical/read-contract objects and bounded status projection/view; lookup EXECUTE only if required by implemented reader | Dedicated reader grant generator/views pending. Do not reuse worker/API credentials or grant raw payload/receipt/audit SELECT by convenience. |
| Legacy importer | Current tenant/table authorization reads; public receipt SELECT/INSERT; narrow bucket locks; legacy provenance SELECT/INSERT; explicit trusted operator policy | Separate credential; no physical/audit/outbox INSERT or queue authority needed by importer. Generator/CLI pending. RuntimeGrants intentionally omits provenance writes. |
| Queue operator | Authorized inspect/cancel/redrive/retention contracts with trusted actor evidence | CancellationGrants supplies narrow owner-object cancellation access and append-only action evidence for a separate principal; restricted-account tests pass. Deployment policy, endpoints, redrive and retention remain pending Q-01. |
| Outbox delivery owner | To be defined from actual ingestion event consumers | No broad UPDATE/DELETE granted speculatively; pending-only schema is a freeze blocker. |

Data-model publication owns granting/revoking access to mapped physical tables,
lookup routine availability/definer policy and registry compatibility. Ingestion
must not enumerate arbitrary tables or mutate private exact-key dictionaries.
RuntimeGrants emits grants to an existing named account at host '%'; it does not
create or activate MySQL roles, configure authentication/TLS, or restrict network
reachability. Deployment provisioning must choose the approved account/host and
transport policy rather than treating the helper as complete production IAM.

Next verification: fresh complete data-model installation then ingestion lineage;
exercise each restricted principal and negative privileges, archive SHOW GRANTS,
and verify startup rejects missing capabilities. That is IN-01.2.020/022/023,
not completion implied by this documented review.

## DDL recovery contract (IN-01.2.021)

A step journals dirty intent before nontransactional DDL. On resume, exact Before
means execute; exact After with dirty intent means acknowledge already-applied
DDL; any other definition means stop with ErrDrift and preserve dirty history.
An object without matching history is not adopted. After completion the runner
rechecks the whole plan and a repeated Apply must be idempotent. There is no
force-clean/down command; investigate drift rather than drop application objects.

`TestIngestionInstallationRecoveryBoundaries` constructs durable before-DDL,
after-DDL-before-ack and drift states for every one of the 20 ingestion steps.
Each case reconstructs a runner and verifies recovery or explicit dirty rejection.
These are deterministic durable-state simulations, not process-kill or packet-loss
tests, and use minimal fixture metadata. Full DM-01 installation and deployment
crash acceptance remain separate gates.


## Timestamp gap implementation (2026-10-29)

Receipt creation time and latest work-attempt time are now explicit candidate
schema columns. New receipts use database UTC time; authorized legacy imports
use the source instant after strict DATETIME(6) validation. Replays never rewrite
creation time. Queue claims update started_at in the same fenced claim transaction;
heartbeat and retry leave the previous attempt timestamp intact. WorkOwner.Get
returns it for running/terminal work without loading raw payloads; queued retries
return a null StartedAt, matching the source API, while the queue retains the
previous claim time for operational accounting. Worker column grants include started_at;
API principals still cannot update job state. Timestamp comparisons use database
instants; source timezone offsets normalize to UTC without loss of microseconds.

These modify the unfrozen candidate schema. No accepted/deployed migration was
rewritten. Any previously generated candidate reference definitions must be
regenerated; installed deployments would require append-only upgrade steps.
Historical requested_at and completed-history import, nullable payload handling,
status/error mapping and outbox delivery state remain unresolved freeze gates.


## Upload-list index and projection (IN-01.2.035)

The unfrozen work-resource schema now includes upload_history with tenant, table,
kind, descending creation time and descending ID. ListUploads uses this index
explicitly because the unhinted MySQL 8.4.8 plan selected the older kind-agnostic
history index. On 2,000 mixed records, first-page EXPLAIN uses scoped ref access,
continuation uses range access, both avoid filesort, and the job join uses its
primary key with eq_ref. These are plan observations, not measured production
throughput or proof of a latency improvement; estimates are not actual row counts.

The list projects bounded filename/content-type and scalar counters/status/times,
never raw payload, public key or result JSON. Restricted column grants in tests
prove these sensitive columns are unnecessary. Pagination returns at most 100
items with one lookahead row, preserving tied-timestamp order by UUID. Missing
jobs and oversized retained metadata fail the page without partial success.
Read policy currently requires both create and patch owner permissions; dedicated
read-only business authorization and endpoint DTO wiring remain separate tasks.

## Cancellation manifest extension (2026-10-29)

The appended ingestion_v002_work_actions step preserves existing step identities.
The current 19-step candidate passed 57 durable recovery/drift cases plus lineage
verification (evidence/2026-10-29_ingestion-operator-lineage.log). Earlier 18-step,
54-case evidence remains historical. Independent deployment review is still open.
Cancellation grants omit payload, physical records and public receipts; action
UPDATE/DELETE are denied. Policy reads must be granted separately for the chosen
trusted authorizer. No role, endpoint or runtime backend is activated by this helper.

## Inspection index extension (2026-10-29)

The appended ingestion_v003_operator_history step adds the scoped queue inspection
index; current candidate is 20 steps, 12 application tables plus the ledger.
WorkInspector requires it explicitly based on the retained mixed-history EXPLAIN
evidence. InspectionGrants is a separate column-only reader grant helper, with no
payload/token/error-text access or mutations. The queue operator's inspection
policy is tenant-wide and cannot reuse table-scoped submission permission.
See 02_ingestion_operation_contracts.md for authorization, pagination, plan evidence
and deployment limits. Earlier 19-step cancellation evidence remains historical.
