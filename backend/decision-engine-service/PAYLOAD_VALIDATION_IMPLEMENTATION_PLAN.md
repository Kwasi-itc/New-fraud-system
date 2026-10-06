# Decision Engine Payload Validation Implementation Plan

Date: October 6, 2026

Status: Planned. This document authorizes no deployment and records no implemented validation.

Validate evaluation objects against the tenant data model before triggers, rules, historical reads, or decision writes. Prepare each object once and share its validated fields and model snapshot across scenario evaluation. Keep the warm path local and measure its cost before making performance claims.

## Agreed requirements

- Supplied fields represent a full evaluation object. Completeness follows ingestion's create path: active non-nullable business fields must be present; omitted nullable fields become NULL.
- Use a shared preparation boundary: resolve model, resolve object, validate and normalize, then fan out to scenarios.
- Async payload validation happens at execution time. Admission continues to validate transport and queue controls; it does not promise that the object satisfies the data model.
- Return async validation failures through the existing execution status, inline wait result, and callback mechanisms applicable to the request.
- Preserve one shared PostgreSQL database, service ownership, tenant isolation, and independently tracked migrations.

Normalization follows ingestion's accepted scalar types as the proposed compatibility baseline. The implementation must specify safe numeric conversions and enum behavior explicitly rather than copying ingestion defects. The proposed policy details below are reviewable implementation choices.

## Current execution and costs

Live single-scenario evaluation loads scenario and iteration, checks trigger object type, optionally fetches a record when fields are empty, loads the tenant model, then evaluates. Non-empty payloads are used directly and are not merged with stored records. Payload accessors return nil for both missing fields and explicit NULL.

All-scenario evaluation selects live scenarios and performs preparation inside each scenario. Zero matching scenarios currently bypass model checks. Test-run evaluation has separate live and phantom helpers. Async workers eventually call the same decision evaluation services.

The decision service and data-model HTTP client each cache tenant models for 30 seconds and use singleflight on concurrent misses. Decision-service hits copy the entire tenant model; scenario fan-out repeats that copy. Direct database readers independently obtain model information through the HTTP client's cache. A revision ID is available but is not pinned across those operations.

Relevant implementation locations:

- [Decision service](internal/service/decision_service.go)
- [Test run service](internal/service/testrun_service.go)
- [Execution service](internal/service/execution_service.go)
- [Data model adapter](internal/clients/datamodel/http_client.go)
- [Model port](internal/ports/data_model_reader.go)
- [HTTP requests](internal/httpapi/dto/decision.go)
- [Evaluation errors](internal/evalerrors/classify.go)
- [Ingestion validation reference](../ingestion-service/internal/domain/ingestion/validation.go)

## Evaluation object contract

Keep the existing object_id, object_type, and fields request shape. Explicitly document two sources: a supplied full object, or a stored-record lookup when fields is omitted, null, or empty. Preserve this existing lookup behavior initially; an empty object is not silently accepted as a complete payload.

| Condition | Proposed behavior |
| --- | --- |
| Blank object ID or object type | Reject before evaluation |
| Unknown or archived object type | Reject even if no scenarios match |
| Missing active non-nullable business field | Reject with missing_required |
| Omitted nullable business field | Materialize NULL in prepared fields |
| Explicit NULL for non-nullable field | Reject with null_not_allowed |
| Explicit NULL for nullable field | Accept |
| Unknown or archived supplied field | Reject |
| Payload lookup ID differs from top-level ID | Reject with identity_mismatch |
| Payload lookup ID omitted | Populate from top-level ID; top-level identity is authoritative |
| Invalid supplied value | Reject; do not also report it as missing |

Normalize object IDs consistently with ingestion's trimmed non-empty string contract. Validate equality after this normalization. Never fetch a stored record to fill gaps in a non-empty supplied payload.

Managed fields need a read/evaluation policy, not ingestion's mutation prohibition. Resolve the published system-field catalog first. Exempt service-maintained fields from completeness requirements; define which may appear in supplied and fetched objects, validate their declared types, and keep identity consistency mandatory. Do not reject a legitimate fetched row solely because it contains id, updated_at, valid_from, or valid_until. Do not use supplied system metadata to mutate storage or establish trusted actor identity.

Validate fetched records as complete evaluation objects too, using the same business-field rules and an explicit policy for managed metadata. Surface schema drift as a distinguishable stored_record_invalid failure rather than blaming a supplied payload.

## Value normalization

Normalize into a new map without modifying the caller's map. Ordinary strings retain whitespace; IDs, numeric strings, and IP addresses follow their documented normalization rules.

| Type | Proposed representation and checks |
| --- | --- |
| bool | Boolean; accept trimmed case-insensitive true and false strings |
| int | int64; accept integral values and decimal integer strings; enforce range and precision |
| float | float64; accept numeric values and numeric strings; reject NaN, infinity, and overflow |
| timestamp | UTC time.Time from RFC3339 including fractional seconds; accept valid typed time.Time from internal sources |
| ip_address | Valid IPv4 or IPv6 string, trimmed |
| string | String only; preserve ordinary string content |
| enum | Check normalized scalar against the published catalog with deterministic canonicalization |

Use number-preserving JSON decoding on sync requests, queued request decoding, and any stored-row decoding that can lose integer precision. Audit all internal request sources before changing decoder behavior. The prepared representation must remain compatible with AST evaluation, aggregate parameters, request-body serialization, and callbacks. Add explicit serialization and round-trip tests for int64 and timestamps.

Do not import ingestion's internal Go package across services. Keep decision preparation owned by decision-engine and establish cross-service compatibility with shared fixture cases or contract tests. A shared library is a separate architecture choice, not a prerequisite.

## Preparation and model snapshots

Introduce a private prepared object abstraction containing tenant ID, object type, normalized object ID, validated fields, and the model snapshot/revision. Construct it through preparation only; internal evaluation methods consume it instead of accepting arbitrary request maps. Treat maps and snapshots as immutable during evaluation.

Prepare once per object before all-scenario selection/fan-out. Prepare once for single-scenario evaluation and enforce scenario object-type compatibility. Prepare once for live/phantom test comparisons so both use identical fields and revision. Scheduled and async item execution prepare each item at execution time.

Extend the data-model adapter and port with table archive state, field nullability, enum metadata, archive state, and managed field information. Resolve unsupported model types as contract failures, not permissive acceptance. Verify the actual system-field metadata representation before selecting a schema type.

Use one resolved model snapshot for validation, AST execution, related-path resolution, and direct database table resolution within the object execution. Add an explicit model-aware reader or execution-scoped reader rather than a context value that silently changes behavior. HTTP ingestion reads remain a separately versioned service contract: define how revision mismatch is detected or reported before claiming end-to-end revision consistency across that path.

Execution-time validation uses the current model resolved through the documented cache policy. It does not guarantee the latest database revision on every request. Record the actual revision used. Async retries resolve again at their execution time; deterministic invalid input terminates that execution rather than repeatedly retrying it.

Cache compiled validation schemas by tenant, object type, and revision. Derive them from immutable model snapshots. Build enum membership sets once, not for every object. Bound cache size and evict expired/superseded entries; revision keys alone are not a memory bound. Coalesce concurrent refreshes and avoid holding cache locks during network calls. Do not remove protective model copying without replacing it with an enforceable ownership contract.

## Error and async contracts

Add a typed validation error with bounded, deterministic field issues and the model revision. Sort issues by field/code; distinguish absent, NULL, type mismatch, enum mismatch, unknown field, and identity mismatch. Avoid echoing raw field values or PII. Cap issue counts and disclose truncation.

Use the existing structured live error envelope with HTTP 422 and category payload_validation_failed, plus a validation_errors array. Reserve 400 for malformed transport, 502/503 for dependency failures as documented, and internal errors for implementation failures. Classify typed errors through errors.As rather than error-message substrings. Update test-run evaluation to the same contract.

Async validation failures are terminal and non-retryable. Retain structured issues in execution status and callback/inline-wait results; last_error can remain a safe summary for existing consumers. Preserve delivery retries separately from evaluation retries. Continue retrying transient model/read failures under the existing operational policy.

Persist model revision and structured async validation evidence if existing storage cannot express them. Any migration belongs to decision-engine's migration history in the shared database. Make terminal state and pending callback delivery durable together using the existing transaction/outbox contract, with rollback tests. Review inline wait response mapping so failed executions cannot look like successful evaluations.

Invalid preparation must produce no decision, rule execution, or decision workflow effects. Async execution lifecycle/failure records are expected. Multi-scenario runtime failures after successful preparation retain existing partial-commit semantics; this work does not promise atomicity across all scenarios or all async batch items.

## Implementation sequence

1. Inventory all entry points, sources, model metadata, and consumer response shapes. Capture representative current payload fixtures, including database-scale inputs. Finalize managed-field policy and normalization compatibility before code changes.
2. Extend and test model decoding. Define immutable snapshots, typed issues, preparation ownership, serialization, and revision evidence.
3. Implement local completeness/type/enum/identity validation and bounded compiled-schema caching. Add deterministic contract tests before integration.
4. Refactor synchronous single/all-scenario evaluation to prepare once. Pass prepared snapshots into historical readers; ensure invalid objects fail even with zero scenarios.
5. Integrate test-run, scheduled, and async execution paths. Preserve execution-time validation, number fidelity, typed terminal failures, status/inline/callback contracts, and delivery atomicity.
6. Add any required decision-owned migrations and update OpenAPI, API client types, examples, and operational documentation. Exercise old/new schema compatibility and ordered migrations on disposable databases.
7. Run behavioral and representative performance verification, then review compatibility findings before rollout.

## Behavioral verification

- Full-object completeness, nullable omissions, explicit NULL, duplicate-error prevention, and deterministic issue ordering/caps.
- Every scalar type, enum, invalid strings, non-finite floats, integer limits, and numbers above the exact float64 integer range.
- Unknown/archived tables and fields, managed fields, lookup-ID injection/mismatch, and stored-record drift.
- Equivalent normalized results for supplied and stored objects; no caller-map mutation or races during fan-out.
- Single/all-scenario, zero-scenario, ingestion-trigger, test-run live/phantom, scheduled, and async item paths.
- Tenant separation in compiled caches and SQL resolution; revision changes, concurrent cache refresh, cache eviction, and dependency failure recovery.
- Invalid preparation causes no decision effects. Inject failures into terminal-state/callback writes to verify rollback and recovery.
- Async status, inline waits, callbacks, delivery retry, and non-retryable validation failures; old consumers continue to read safe summaries.
- Dedicated disposable PostgreSQL integration tests with real owning-service migrations. Never recreate or mutate the application database as test setup.

## Critical path measurement

Measure preparation separately from scenario evaluation. Record model resolution, object fetch, normalization/validation duration, compiled-schema cache hit/miss/build, allocations, and rejected-object counts. Keep metric labels bounded; do not label by object IDs, arbitrary field names, or revision values.

Compare existing behavior and the implementation on the same host, images, object corpus, rules, database state, and concurrency. Cover supplied and stored objects, cold/warm caches, small and large payloads, small and large tenant models, multiple scenarios, valid/invalid input, concurrent refresh, and async worker load. Measure p50/p95/p99, successful throughput, CPU, allocated bytes, and retained cache memory. Separate the cost of model copies, parsing, local validation, and extra I/O.

The warm supplied-object target is zero additional network/database operations, one local validation pass per object, and no full-model copy per scenario. Verify query counts and representative plans when stored-record or reader integration changes. Set an explicit acceptable latency/throughput budget from baseline evidence before release; no numeric overhead claim is established by this plan.

## Compatibility and rollout

Rejecting incomplete supplied payloads is a behavior change. Update API examples, frontend test payloads, producers, and scale fixtures to supply all active non-nullable fields or deliberately use stored-record lookup. Do not add a permissive fallback that reports successful validation.

Deploy migrations before readers/writers that need new columns; keep API and worker versions compatible during rollout. Rebuild/restart APIs and workers together with their documented cache behavior. Inspect queued payload compatibility before enabling execution-time enforcement; pending requests may be rejected under the model used when they execute.

Roll out after the behavioral suite, disposable integration checks, and agreed performance budget pass. Report implemented code, deployed runtime behavior, performance evidence, and remaining revision-consistency gaps separately.

## Completion checklist

- [x] Finalize managed-field, normalization, and fetched-record contracts.
- [x] Extend model metadata and implement immutable preparation.
- [x] Integrate all evaluation paths and model-aware readers.
- [x] Implement typed sync and terminal async validation errors.
- [x] Preserve number fidelity and persist revision/error evidence where required.
- [ ] Verify migrations, rollback, tenant isolation, concurrency, and consumer contracts.
- [ ] Measure critical-path overhead and bounded cache behavior.
- [ ] Update callers, OpenAPI, fixtures, and rollout documentation.

Implementation status (2026-10-06): code, OpenAPI, client error types and operational documentation are implemented. The Go suite and static checks passed. Local warm-schema benchmarks are recorded in README.md. The remaining checklist items include execution of the disposable PostgreSQL tests, frontend checks, representative scale/performance verification and caller/queued-payload compatibility review before rollout. Database-scale harness changes present before this task were preserved. No application database was reset and no running service was deployed or restarted. HTTP ingestion reads still lack revision pinning across service boundaries; arbitrary-precision formula arithmetic is outside this change.
