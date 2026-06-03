# Aggregate Pushdown Checklist

## Contract Design

- [x] Define aggregate query request DTO in `decision-engine-service`
- [x] Define aggregate query response DTO in `decision-engine-service`
- [x] Define matching endpoint contract in `ingestion-service`
- [x] Define filter tree model with grouped `and` / `or` / `not`
- [x] Define predicate model with `field`, `op`, and `value`
- [x] Decide exact V1 aggregate names and enum values
- [x] Decide exact V1 operator names and enum values
- [x] Decide whether `is_empty` is included in V1
- [x] Decide whether `count_distinct` is included in V1 or deferred
- [x] Document runtime value resolution rules before dispatch
- [x] Update OpenAPI for `ingestion-service`
- [x] Update architecture docs for both services

## Ingestion Service Endpoint

- [x] Add new aggregate endpoint route in `ingestion-service`
- [x] Add HTTP handler for aggregate requests
- [x] Add request validation for aggregate type
- [x] Add request validation for object type
- [x] Add request validation for field existence
- [x] Add request validation for operator support
- [x] Add request validation for filter tree shape
- [x] Add service-layer aggregate execution method
- [x] Add repository-layer aggregate execution method
- [x] Build parameterized SQL generation for filter groups
- [x] Build parameterized SQL generation for predicate leaves
- [x] Sanitize all identifiers through model-aware lookup
- [x] Return aggregate result in a minimal response body
- [x] Add bad-request responses for unsupported shapes
- [x] Add internal logging for aggregate query execution

## Decision Engine Aggregate Client

- [x] Add ingestion aggregate client interface or port
- [x] Add HTTP client implementation for aggregate requests
- [x] Add request/response DTO mapping
- [x] Add timeout handling for aggregate calls
- [x] Add failure handling for non-200 responses
- [x] Add structured logging around aggregate calls

## AST Compiler

- [x] Add a compiler package/module in `decision-engine-service`
- [x] Add compile result type with `supported` / `unsupported` semantics
- [x] Compile `Aggregator` node into aggregate query spec
- [x] Compile `Filter` node into predicate nodes
- [x] Compile `List` of filters into AND groups
- [x] Preserve grouped `and` / `or` structure where present
- [x] Resolve payload-based values before dispatch
- [x] Resolve time-based values before dispatch
- [x] Normalize operator names to canonical query operators
- [x] Reject unsupported AST nodes in aggregate pushdown mode
- [x] Return explicit unsupported reasons for diagnostics

## OR / Grouping Behavior

- [x] Preserve nested boolean grouping in compiler output
- [x] Add tests for `(A and B) or C`
- [x] Add tests for `A and (B or C)`
- [x] Add tests for nested OR groups
- [x] Add optional normalization from repeated OR equality checks to `IN`
- [x] Ensure normalization does not change semantics

## Evaluator Integration

- [x] Replace current in-memory aggregate path when compilation succeeds
- [x] Keep feature-flagged fallback behavior during rollout
- [x] Add `strict_pushdown` mode
- [x] Add fallback metrics
- [x] Add aggregate compile success/failure metrics
- [x] Add remote aggregate latency metrics
- [x] Add logs that identify unsupported aggregate AST shapes

## V1 Supported Function Scope

- [x] Support `Aggregator`
- [x] Support `Filter`
- [x] Support `List` as AND list
- [x] Support payload-derived values
- [x] Support resolved time expressions
- [x] Support aggregates: `count`
- [x] Extend to `sum`
- [x] Extend to `avg`
- [x] Extend to `min`
- [x] Extend to `max`

## V1 Explicitly Deferred Scope

- [x] Defer fuzzy matching pushdown
- [x] Defer decision-history joins
- [x] Defer custom-list joins
- [x] Defer tag/risk helper joins
- [x] Defer broad relationship join support
- [x] Defer arbitrary SQL-like expression pushdown

## Testing: Decision Engine

- [x] Unit test AST to query spec compilation
- [x] Unit test payload value resolution
- [x] Unit test time expression resolution
- [x] Unit test unsupported node detection
- [x] Unit test grouped boolean condition compilation
- [x] Unit test evaluator integration with remote aggregate result
- [x] Unit test fallback behavior
- [x] Unit test strict mode behavior

## Testing: Ingestion Service

- [x] Unit test filter tree validation
- [x] Unit test object type validation
- [x] Unit test field validation
- [x] Unit test SQL generation for AND groups
- [x] Unit test SQL generation for OR groups
- [x] Unit test SQL generation for NOT groups
- [x] Unit test aggregate correctness for `count`
- [x] Unit test aggregate correctness for `sum`
- [x] Unit test aggregate correctness for `avg`
- [x] Unit test aggregate correctness for `min`
- [x] Unit test aggregate correctness for `max`
- [x] Unit test unsupported operator rejection

## Integration Testing

- [x] End-to-end test for aggregate count decision
- [x] End-to-end test for aggregate sum decision
- [x] End-to-end test for nested OR aggregate filters
- [x] End-to-end test for time-window aggregate filters
- [x] End-to-end test for large dataset scenario
- [x] End-to-end test comparing fallback vs pushdown results on supported rules

## Rollout

- [x] Add feature flag for aggregate pushdown
- [x] Enable only for `count` first
- [x] Run comparison tests on sample tenant data
- [ ] Inspect performance and correctness metrics
- [x] Expand to additional aggregate types
- [ ] Remove in-memory aggregate fallback after rollout confidence
- [x] Update team docs and operating notes

## Recommended First Milestone

- [x] Contract defined
- [x] Ingestion aggregate endpoint implemented
- [x] Decision-engine aggregate client implemented
- [x] `Aggregator(COUNT)` compiler implemented
- [x] AND-only filter support implemented
- [x] Payload and time resolution implemented
- [x] Feature-flagged evaluator integration shipped
- [x] Initial end-to-end tests passing
