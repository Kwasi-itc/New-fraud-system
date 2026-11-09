# D13–D20 integration and acceptance matrix

Reviewed 2026-11-03 against the working tree. This document records bounded
inventory/design items and links subsequent verification. It does not certify missing providers or load
acceptance. Paths below are relative to the repository root. PostgreSQL remains
the configured shared application database; MySQL compositions are inactive and
tested only in disposable fixtures.

## D13.11 — Model/index owner composition

| Boundary | Existing implementation | Remaining connection / owner |
| --- | --- | --- |
| HTTP model | `backend/data-model-service/internal/httpapi/handlers/datamodel.go`, `service/data_model_read_service.go` snapshot service | Data-model: staged HTTP constructor must require trusted read authority and use the published snapshot repository. Current `internal/app/app.go` and `httpapi/router.go` select PostgreSQL. |
| Eligible publication | `internal/store/mysql/published_model_reader.go` | Advertise only open, inactive registry rows with a non-null token; never treat an HTTP token as a substitute for transaction-time registry validation. |
| Scoped jobs | Owner `httpapi/handlers/index_jobs.go`, `store/mysql/index_job_scope.go`, `service/index_job_service.go` | Data-model: bind scoped reads/create/retry to tenant authority and `store/mysql/transaction_manager.go`; enqueue through the matching pool/schema producer. |
| Durable producer | Owner `store/mysql/index_delivery_store.go`, transaction manager | Enqueue and job creation must commit together; prove enqueue failure rolls both back. |
| Leased execution | Owner `store/mysql/index_delivery_execution.go`, `worker/index_job_runner.go`, `worker/shared.go` | Compose the claimed lease, heartbeat/recovery and actual physical execution. Metadata-only `index_job_execution.go` is insufficient. |
| Physical readiness | Owner `store/mysql/physical_execution.go`, `physical_planner.go`, `physical_publication_manager.go`, `physical_registry.go` | Connect index plan to fenced execution and publication; inspect actual index and registry version before accepting applied status. |
| Decision preparation | `backend/decision-engine-service/internal/clients/datamodel/{http_client,index_preparation}.go`, `internal/mysqlruntime/scope.go` | Supply explicit Models and IndexPreparation capabilities; no source/target database fallback. |

Acceptance order: model endpoint → atomic job endpoint → leased worker → decision
client wiring → HTTP preparation through actual publication → recovery and two
simultaneous starts → separate restricted API/worker accounts. D13.12–D13.20 own
those checks; this map does not complete them.

Cycle 1 implementation: `internal/mysqlruntime/model.go` now composes the
publication-aware snapshot endpoint with mandatory SQL read authority;
`internal/mysqlruntime/index_jobs.go` composes scoped reads/create/retry using
`store/mysql/index_job_application.go`. The latter joins the existing business
service to one owner transaction and durable producer. Retry locks the job,
preserves old delivery evidence, replaces a completed reservation or reuses an
available delivery, and rejects leased/conflicting state. Tests:
`TestStagedPublishedModelEndpoint` and `TestStagedIndexJobEndpoints`, in owner
`internal/store/mysql/metadata`. Fixture authority is explicitly injected;
real auth and worker publication remain open. No physical readiness is inferred
from a successfully queued job.

Cycle 4 closes D13.14b with owner `store/mysql/native_index_worker.go`:
`PhysicalPublicationManager.RunNativeIndexJob` consumes a committed lease and
trusted retained reference work, plans the index from persisted job columns,
prepares its immutable intent, executes physical DDL, publishes and performs
fenced completion. Same-pool/schema/tenant binding is mandatory. Local authority
is rechecked inside the completion transaction. The nine
`TestPhysicalCreationAllocated/service_allocated_native_index_worker*` scenarios
verify physical success, recovery and failure boundaries. See
[cycle 4 evidence](../evidence/2026-11-03_decision_cycle4.json).
Text/navigation/repair remain explicitly unsupported by this composition.
Reference-artifact loading, lease supervision/failure disposition, live identity,
restricted grants and configured decision integration are not supplied by this
single-lease execution method. D13.15 onward own the remaining integration gates.

## D14.09 — Finite history corpus

Baseline: `backend/decision-engine-service/internal/store/mysql/history_differential_test.go`,
`TestHistoryPostgresMySQLGoldenFiltersPaginationAndSnoozes`. Each filter compares
independently selected, normalized full summary rows and counts on both engines,
plus offset and cursor paging. No claim of universal Unicode collation parity.

| Case | Expected contract | Existing evidence / gap |
| --- | --- | --- |
| H01 | Unfiltered, exact object/type/outcome and decision/scenario selectors | Existing differential cases, including missing scenario. |
| H02 | Inclusive score range and impossible reversed range | Existing differential cases. |
| H03 | Prefix case and escaped SQL wildcards | Existing prefix and escaped wildcard cases. |
| H04 | Case-insensitive scenario search; accent-sensitive Café/Cafe | Existing finite differential strings. |
| H05 | Scenario/type/outcome/min-score conjunction | Existing combined case. |
| H06 | Exact empty, whitespace and case-distinct object IDs; tenant isolation | Existing dedicated subtest. |
| H07 | Equal timestamp UUID ordering, five-row pages, offset parity | Existing pagination loop; add one-row and exact-size pages in D14.11. |
| H08 | Newer concurrent insert excluded; older insert included; missing cursor anchor | Existing changing-write subtest. |
| H09 | Count/page share snapshot while concurrent writer commits | Existing caller-owned snapshot subtest. |
| H10 | Snooze expiry exclusive, exact object and tenant | Existing snooze cases. |
| H11 | Cursor tenant/filter/version/date/ID validation | `internal/mysqlapi/history_query_test.go`; add every filter field replay in D14.11. |
| H12 | Single-sided score bounds, zero-width zero range, negative bound | D14.10: add to both-engine golden corpus. |
| H13 | Search by UUID/object type and escaped literal wildcards | D14.10: add independent predicates. |
| H14 | All applicable filters together, including search/prefix and both bounds | D14.10: ensure nonempty positive and empty mismatching conjunctions. |
| H15 | Exhausted cursor, offset past end, exact-sized final page, page size one | D14.11: compare both engines, final hasMore=false and no duplicates. |

Invalid external query parameters are tested at the API boundary; repositories
are not required to invent HTTP validation. H01–H15 are correctness acceptance;
D14.12–D14.15 separately require approved scale/rate/latency/error targets,
representative seeds, query plans and sustained measurement.

Cycle 1 closes H12–H15 correctness gaps: eight new golden filters; page sizes
1/5/48; terminal cursor and offset-past-end assertions for every filter; replay
rejection after changing each of the nine filter fields. Existing concurrent
write/snapshot cases were rerun alongside these additions. See
[cycle 1 evidence](../evidence/2026-11-03_decision_cycle1.json).

## D15.10 — Evaluator and effect corpus

Canonical branches inventoried from
`backend/decision-engine-service/internal/runtime/ast_eval/evaluator.go`.
Aliases normalize to these branches and need alias-vs-canonical regression
coverage. Unit evaluation is not equivalent to actual-service persistence parity.

| Family | Canonical branches | Required golden cases / closure owner |
| --- | --- | --- |
| E01 values | field_ref, payload, list, coalesce | Present/missing/null/false/zero, nested field; D15.11. |
| E02 boolean/comparison | and, or, not, eq, neq, gt, gte, lt, lte | Short circuit, equality boundary, mixed/invalid types; D15.11. |
| E03 strings/membership | contains, string_not_contain, in, contains_any, contains_none, starts_with, ends_with, lower, upper, is_null, is_empty, is_not_empty, is_in_list, is_not_in_list, string_concat, string_template | Empty/null, case, Unicode, list vs scalar, invalid input; D15.11. |
| E04 arithmetic | add, subtract, multiply, divide, is_multiple_of | Negative, zero, fractions, zero divisor, invalid operands; D15.11. |
| E05 clock | time_now, parse_time, timestamp_extract, time_add | Fixed clock, timezone boundary, malformed timestamp/unit; D15.11. |
| E06 local transforms | filter, switch, score_computation, fuzzy_match, fuzzy_match_any_of, fuzzy_match_filter_options, filter_eq, map_field, list_count, sum, avg, min, max, group_count, group_sum, object_get | Empty collections, duplicate groups, missing key, malformed branch; D15.11. |
| E07 data model | aggregator, database_access, related_count, related_field, related_records | COUNT/COUNT_DISTINCT/SUM/AVG/MIN/MAX, null/empty/grouped/filter cases on actual published tables; D15.12. Unsupported STDDEV/PCTILE/MEDIAN pushdown must fail explicitly rather than report success. |
| E08 platform | in_custom_list, custom_list_access, record_has_tag, record_risk_level, has_ip_flag, ip_country, ip_country_code, ip_region, ip_region_code, ip_continent_code, ip_geo_found, past_decision_count | Positive/missing/foreign tenant and storage/authorization failure using actual SQL reads; D15.13 and D18.13. |
| E09 effects | Workflow, screening, scoring, outbox | None, each separately, all together; failure of every required write rolls everything back; replay produces no extra effect; D15.14 after D19.21. |
| E10 races | Identity revocation and publication change | Real owner revocation during authorization wait; model changes between preparation and persistence fail safely; D15.15–D15.16. |

Evidence starting points: `internal/service/evaluator_test.go` (unit platform,
Marble-name JSON and boolean result), `internal/runtime/ast_eval/aggregate_compiler_test.go`,
`aggregate_http_integration_test.go`, `custom_list_failure_test.go`;
`internal/store/mysql/tenant_differential_test.go`, `custom_list_differential_test.go`,
`aggregate_publication_integration_test.go`, `decision_service_differential_test.go`.
The latter already compares actual services at thresholds and configured effects
with required-write rollback. E01–E08 still need a case-by-case actual-service
golden ledger; these existing suites do not establish all branch coverage.
Cycle 3 closes the E01–E06 scalar/value corpus in D15.11 with 219 actual-service
golden cases in `internal/store/mysql/scalar_golden_differential_test.go`.
This includes canonical/alias outcomes, invalid-input rejection, complete saved
evidence and decision/evidence row-count invariance. Arithmetic/prefix alias
defects were corrected, with additional plain/evidence normalization tests.
See [cycle 3 evidence](../evidence/2026-11-03_decision_cycle3.json). E07–E10 and
actual model/authority/provider acceptance remain open. The earlier statement
about E01–E08 missing a ledger describes the cycle 1 baseline.

Each new case must assert result, evidence, persisted rows and error/rollback
where applicable, with fixed clock and normalized generated IDs.

## D17.10–D17.11 — Approved permission matrix (2026-11-03)

Existing role vocabulary comes from `backend/auth-service/internal/platform/schema_v2.sql`.
Existing target action vocabulary comes from decision `internal/store/mysql/identity_contract.go`.
All checks additionally require active user, active tenant membership, current
membership version, active unrevoked/unexpired session and matching tenant.
Read-only sessions deny writes regardless of role. Missing provider fails closed.

| Existing role | decision.read / list.read | decision.author / list.mutate | decision.publish | workflow.read / workflow.author |
| --- | --- | --- | --- | --- |
| platform_viewer | Allow | Deny | Deny | Deny under retained target contract |
| platform_builder | Allow | Allow | Deny | Deny under retained target contract |
| platform_publisher | Allow | Deny | Allow | Deny under retained target contract |
| platform_admin | Allow | Allow | Allow | Allow only with case access under retained target contract |
| tenant_admin | Deny alone | Deny alone | Deny alone | Deny alone |
| case_manager_admin | Deny alone | Deny alone | Deny alone | Deny alone |
| case_investigator | Deny alone | Deny alone | Deny alone | Deny alone |

Multiple roles grant the union of explicitly allowed actions, subject to session,
tenant and case/inbox restrictions. Service principals are separate: no user-role
inheritance, no browser session substitution; bind each trusted worker to its
tenant and required persistence/effect capability. A fixture allow callback is
never an identity provider.

**Approved source/target distinction:** source
`internal/store/postgres/workflow_authoring.go` combines
`identity.authorize_data_model_v1` with `identity.authorize_case_v1`.
Source reads permit platform roles plus case access; source writes require
platform_admin plus case access. The retained target browser contract requires
platform_admin plus case access for both reads and writes. The user explicitly approved this stricter target policy on 2026-11-03 ("i approve"), including builder/author, publisher/publish and separate service-principal bindings. Source PostgreSQL authorization is unchanged. Cycle 6 completes D17.11–D17.22 with real owner migrations/functions, credential verification and restricted grants. Browser edit/copy parity remains D17.23–D17.26. See [owner composition notes](../../../backend/auth-service/mysqlidentity/README.md) and [cycle 6 evidence](../evidence/2026-11-03_decision_cycle6.json).

## D18.09 — List consumers and authority

| Consumer | Required owner action | Evidence / outstanding check |
| --- | --- | --- |
| HTTP list metadata and entries/export | decision.list.read | `internal/mysqlapi/custom_list_http.go`, `store/mysql/custom_list_admin.go`; real auth binding D18.10. |
| Create/update/delete list, entry mutation and CSV import | decision.list.mutate | Same modules, `custom_list_csv_test.go`; transaction-wide rollback and concurrent revoke with real auth D18.12/D18.14. |
| in_custom_list AST | Authorized evaluation read scope; exact tenant/name/value lookup | `runtime/ast_eval/evaluator.go`, `store/mysql/custom_list_repository.go`; actual-service positive/missing/foreign case D18.13. |
| custom_list_access AST | Same scope; UUID lookup, name fallback only for explicit not-found | `custom_list_failure_test.go`, `custom_list_differential_test.go`; retain fail-closed authorization/storage/cancel behavior. |
| Effect/worker list access | Distinct decision.list.read or decision.list.mutate authority, explicitly opted in | `store/mysql/effect_custom_lists.go` (`WithCustomLists`/`authorizedCustomLists`); generic decision persistence does not grant list writes. Reauthorize before owner locks within the same decision/effect transaction. |

Install only needed read/mutation grants under D18.11. Bounded entry limits,
no N+1 lookup and query plans belong to D18.15. Evaluation authority is inherited
from a trusted execution scope, never from an arbitrary tenant header.

Cycle 6 completes D18.10–D18.15: real session authority, restricted SQL accounts, snapshot/revocation races, nine actual-service PostgreSQL/MySQL cases, late-job rollback and unhinted indexed plans across 5 tenants, 40 lists and 14,080 entries. List names are exact for lookup; folded hashes only prefilter candidates and enforce folded uniqueness. Full parent evidence review remains D18.16. See [cycle 6 evidence](../evidence/2026-11-03_decision_cycle6.json).

## D19.11 — Screening source/publication ownership

Cycle 5 implements D19.12–D19.13 in `backend/screening-service/mysqlschema`:
independent owner ledger, exact linked execution bindings, bounded generated
summary and an updatable MERGE view whose row lock reaches the source. Initial
enqueue triggers and repeated real-view reconciliation are tested supporting
work. Cycle 7 completes mutation/match-count maintenance and delivery draining under
D19.14-D19.16. See [cycle 7 evidence](../evidence/2026-11-03_decision_cycle7.json).

Source: `backend/screening-service/internal/migrations/metadata/000007_execution_projection.up.sql`
and `internal/store/postgres/execution_projection.go`.
Consumer: decision `internal/store/mysql/owner_contracts.go`.

| Contract | Source behavior | Target provider responsibility |
| --- | --- | --- |
| Source identity | Tenant + screening ID; decision/config/scenario/execution relationship | Screening owns tenant-consistent FKs and source row; D19.12. |
| Positive monotonic version | projection_version starts at 1; row update advances it | Every status/result/match-affecting mutation must advance the source snapshot atomically; D19.14. Audit indirect match writes explicitly. |
| Locked source view | Consumer expects FOR UPDATE on authoritative source | D19.13: SQL SECURITY/grants reviewed, updatable MERGE direct source view; no aggregate join view that loses row-lock semantics. |
| Projection fields | Consumer needs id, tenant_id, decision_id, scenario_id, config_id, execution_id, projection_version, status, updated_at, sent_at, completed_at, failed_at, response_json, provider_reference, last_error | Publish owner-maintained values on the source row in the same transaction; missing/renamed source fields require explicit owner mapping, not inferred JSON defaults. |
| Delivery | Source trigger upserts queue newest version, resets retry state; unlink removes queue | D19.15: source change/version/queue atomic, bounded due index, no network call in transaction. |
| Reconcile | Lock source then queue, recheck due, reconcile under savepoint, delete only matching version | D19.16: shared-database owned contract, retain capped retry/error on failure; later source version cannot be deleted by old completion. |
| Status vocabulary | pending, queued, processing, awaiting_review, confirmed_hit, no_hit, archived, failed | Reject unknown status; compare source version and relationships before accepting projection. |

Migration ordering: auth/core tenant foundations → decision scenario/config and
execution parents → screening source with tenant-consistent relationships and
positive version → source publication fields/view and delivery table/index →
reconcile consumer contract → restricted grants → trusted workers. Where FKs
form a cycle, create both owner tables first and add the separately tracked FK
migration afterward. Test forward install and upgrade against the same logical
database. Do not create per-service application databases.

The staged target publication API and projector now pass actual owner acceptance; production provider lifecycle wiring remains separate. Source tests in
`screening-service/internal/store/postgres/execution_projection_integration_test.go`
provide cases for missing execution retry, version/status/count, foreign
relationships, lock consistency and protection from stale callback data.
Cycle 10 completes the screening receipt binding D19.18 through its actual
screening-owned local intake, retained acceptance receipt and reconciliation.
Both publication/acknowledgement orderings preserve the newer source result.
See [cycle 10 evidence](../evidence/2026-11-03_decision_cycle10.json).
Workflow, scoring and outbox receipt providers remain D19.17/D19.19/D19.20;
trusted runtime identity, full provider composition and external search worker
acceptance remain separate gates. Source projection alone cannot close them.

Exact source-to-view mapping for D19.12–D19.14:

- `id`, `tenant_id`, `decision_id`, `scenario_id`, status, timestamps, reference,
  error and version map from identically named screening source columns.
- `config_id` aliases `screening_config_id`; manual/unlinked rows must not be
  accepted by the decision consumer.
- `execution_id` is a new nullable typed publication binding. Source producers
  use the text `idempotency_key` as execution ID; legacy empty keys require a
  unique tenant/decision/config candidate. Invalid or ambiguous values fail;
  do not cast arbitrary idempotency text silently or use LIMIT 1.
- View `response_json` must expose a new owner-maintained decision projection
  payload, not the raw provider `response_json`. Source decision migration
  `000019_screening_projection.up.sql` constructs screening_id, decision_id,
  scenario_id, screening_config_id, status, provider, object_type, object_id,
  provider_reference, last_error, partial, idempotency_key, match_count,
  completed_at and source_version. Keep this shape and compute bounded
  match_count using the tenant/screening index while holding the source lock.
  Updates to matches must refresh this payload/version in the same transaction.
- Preserve source transition semantics: queued→pending; processing→sent;
  awaiting_review/confirmed_hit/no_hit/archived→completed; failed→failed.
  Completion/failure timestamps use source updated_at when absent; existing sent
  timestamps survive completed/failed transitions as specified in migration 19.

## D20.07 — Integrated acceptance cases

| Case | Required real components | Pass criterion / checklist owner |
| --- | --- | --- |
| I01 prepare/publish/evaluate/history | Model endpoint, index producer/worker, decision API | Actual eligible index/token, persisted evidence, bounded history; D20.10 after D13.16. |
| I02 browser author/copy/list | Auth sessions, case inbox/tag owners, browser API | Approved roles, tenant/inbox isolation, graph-copy rollback; D20.11. |
| I03 restricted accounts | Owner migrations and separate API/workers | Intended calls succeed; direct foreign writes/DDL fail; D20.12. |
| I04 worker races | Trusted lease/receipt providers | Cancel/finish/cleanup races preserve one terminal outcome and no orphan effects; D20.14. |
| I05 crash/restart | Actual delivery and source owners | Restart drains committed work without duplicate effects; D20.15. |
| I06 uncertain commit | Idempotent receipt/replay boundaries | Lost response resolves to committed receipt or safe retry; D20.16. |
| I07 shared migrations | All owner definitions, ordered install/upgrade | No missing contract, cross-tenant FK hole or accidental per-service application DB; D20.17. |
| I08 sustained load | Approved targets and database-wide connection budget | Numeric latency/throughput/error and resource limits pass with plans/evidence; D20.13/D20.18. |

D20.08 supplies a fixture with actual owner implementations, not stand-in allow
functions; D20.09 supplies a repeatable runner. Each run records versions,
commands, assertions, failures/skips, resource budget and cleanup. D20.19
reconciles every case to evidence; only then can D20.20 close the parent.



Cycle 7 closes E07's bounded aggregate/filter/navigation corpus: 19 actual-service rules and committed evidence agree on both engines; unsupported aggregates and disabled broad-read helpers reject explicitly. Real-owner edit/copy acceptance and permission races close D17/D18 granular review. See [cycle 7 evidence](../evidence/2026-11-03_decision_cycle7.json); E08-E10/provider/load/release gates remain separate.
