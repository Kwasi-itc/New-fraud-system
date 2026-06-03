# Aggregate Pushdown Plan

## Goal

Keep rule authoring and decision semantics in `decision-engine-service`, but move aggregate-heavy execution into `ingestion-service` so large-volume aggregate decisions are correct and scalable.

## Target Outcome

After this change:

- aggregate rules no longer fetch large record sets into the decision engine
- the decision engine compiles eligible aggregate/filter AST into a query spec
- ingestion service executes that query spec close to the database
- the evaluator receives only the aggregate result
- unsupported AST shapes still fall back safely or fail explicitly, depending on rollout mode

## Current Problem

Today, `aggregator` evaluation in `decision-engine-service`:

1. loads records from a target table through `ingestion-service`
2. resolves the filter list in the decision engine
3. applies filters in memory
4. aggregates in memory

This creates two issues:

- performance: many rows move across services before filtering
- correctness: the current evaluation path is bounded and may not reflect the full matching dataset

## Design Direction

The architecture should remain:

- `decision-engine-service` owns AST semantics, rule evaluation, and planning
- `ingestion-service` owns tenant record querying and aggregate execution
- `data-model-service` owns tenant schema and link metadata

The enhancement is to add an aggregate pushdown path:

1. the decision engine parses aggregate/filter AST
2. it compiles supported subtrees into a logical query spec
3. it sends that spec to `ingestion-service`
4. ingestion service translates the spec into SQL
5. ingestion service returns only the aggregate result

## Phase 1: Define The Query Contract

Add a logical aggregate query DTO. It should describe the query semantically rather than exposing SQL.

Suggested fields:

- `object_type`
- `aggregate`
- `field`
- optional `distinct`
- optional `group_by`
- `filter`

Add a filter tree DTO that preserves nesting:

- group node:
  - `operator`: `and` | `or` | `not`
  - `children`
- predicate node:
  - `field`
  - `op`
  - `value`

### V1 Operator Scope

Support only pushdown-safe operators:

- `eq`
- `neq`
- `gt`
- `gte`
- `lt`
- `lte`
- `in`
- `is_null`
- `is_empty` if SQL semantics are clearly defined
- time comparisons

### V1 Aggregate Scope

Support:

- `count`
- `count_distinct`
- `sum`
- `avg`
- `min`
- `max`

### Runtime Value Resolution

Values should be fully resolved in the decision engine before dispatch:

- constants stay literal
- payload-derived values are resolved from the current evaluation runtime
- time expressions like `now - 24h` become concrete timestamps

## Phase 2: Add Ingestion Aggregate Endpoint

Add an endpoint in `ingestion-service`, for example:

- `POST /v1/tenants/:tenantId/query/aggregate`

The request should contain the logical aggregate query spec. The response should be minimal:

- `value`
- optionally `matched_count`
- optionally diagnostic metadata later

### Endpoint Responsibilities

The ingestion service should:

1. validate the request
2. validate object type and field names against the published model
3. validate operator and aggregate support
4. translate the filter tree into parameterized SQL
5. execute the aggregate in Postgres
6. return only the computed result

### SQL Safety

The ingestion service should:

- never accept raw SQL from callers
- sanitize identifiers using model-aware resolution
- parameterize all values

## Phase 3: Add Aggregate Compiler In Decision Engine

Add a compiler in `decision-engine-service`:

- input: AST node
- output: aggregate query spec

### Initial Compilation Scope

Start with:

- `Aggregator`
- `Filter`
- `List`

And compile them into the logical query contract.

### Compilation Behavior

The compiler should:

1. extract `tableName`
2. extract `fieldName`
3. extract aggregate function
4. resolve filter nodes
5. resolve runtime-dependent values
6. emit a query spec only if the subtree is fully supported

### Unsupported Functions In V1

Reject or fall back for:

- `fuzzy_match`
- `past_decision_count`
- `record_has_tag`
- `custom_list_access`
- `related_field` unless explicit join support is added
- arbitrary subexpressions that cannot resolve to literal values

## Phase 4: Wire Evaluator To Remote Aggregate Execution

Replace the current in-memory aggregate path with:

1. attempt compilation
2. if compilation succeeds, call the ingestion aggregate endpoint
3. use the returned aggregate value in AST evaluation

Add a dedicated ingestion aggregate client in `decision-engine-service`.

### Fallback Strategy

Recommended rollout behavior:

- `strict_pushdown=true`: unsupported aggregate AST returns an error
- `strict_pushdown=false`: unsupported aggregate AST may temporarily use the old in-memory fallback

### Observability

Add:

- compile success/failure logs
- aggregate latency metrics
- fallback counters
- query shape/operator mix metrics if useful

## Phase 5: Preserve OR And Condition Structure

Do not flatten grouped boolean logic into a plain list.

This matters because:

- `(A and B) or C` is not the same as `A and (B or C)`
- SQL translation needs grouping preserved

### Recommended Internal Shape

Use a nested filter tree:

- group:
  - `operator`
  - `children`
- predicate:
  - `field`
  - `op`
  - `value`

### Normalization Rules

The compiler may normalize:

- repeated `or(eq(field, a), eq(field, b), eq(field, c))`

into:

- `field IN [a, b, c]`

This is an optimization, not a semantic rewrite of unrelated expressions.

### Marble Filter Compatibility

Existing Marble-style `List[Filter...]` behavior should remain AND-based for compatibility unless an explicit grouped structure is introduced.

## Phase 6: Performance And Index Strategy

Pushdown improves architecture, but performance still depends on indexing.

Identify common aggregate query patterns:

- customer id + time window
- status + time window
- foreign key + created_at
- account id + amount threshold

Document or enforce index expectations in ingestion-service.

Potential later guardrails:

- reject expensive query shapes for some tenants
- require at least one selective predicate for heavy aggregates
- add materialized or feature tables for high-frequency metrics

## Phase 7: Validation, Testing, And Rollout

### Decision Engine Tests

- AST to query spec compilation
- supported vs unsupported subtree detection
- runtime value resolution
- evaluator integration with remote aggregate results

### Ingestion Service Tests

- filter tree validation
- SQL generation
- aggregate correctness
- model-aware field validation

### Integration Tests

- end-to-end decision evaluation with aggregate rules
- large dataset scenarios
- nested `and` / `or` behavior
- time-window scenarios

### Rollout Strategy

1. ship behind a feature flag
2. enable `count` first
3. compare old vs new behavior on sample datasets
4. then expand to `sum`, `avg`, `min`, `max`
5. remove the in-memory fallback after confidence is high

## Suggested Implementation Order

1. query DTOs and OpenAPI updates
2. ingestion aggregate endpoint
3. decision-engine aggregate client
4. AST compiler for `Aggregator` + `Filter` + `List`
5. evaluator integration
6. OR/group normalization
7. testing and rollout

## Recommended V1 Scope

Keep the first version intentionally narrow:

- support `Aggregator`
- support `Filter`
- support `List` as AND list
- support grouped `and` / `or` where already representable
- support `count`, `sum`, `avg`, `min`, `max`
- support `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `in`
- resolve `Payload`, `TimeNow`, `TimeAdd`, and `ParseTime` before dispatch

Defer:

- fuzzy matching
- cross-service helper functions inside SQL
- decision-history joins
- custom list joins
- tags/risk helper joins
- broader relationship join support

## Recommended First Milestone

The highest-value first milestone is:

- push down `Aggregator(COUNT)` with AND-only filters
- support payload-based values and time windows
- add remote execution path
- keep old behavior only as a temporary fallback

That gives immediate correctness and scalability improvement for large count-based rules without requiring the entire AST language to be pushdown-ready in the first iteration.
