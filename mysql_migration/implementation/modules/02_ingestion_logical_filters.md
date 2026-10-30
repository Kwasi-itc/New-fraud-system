# Staged logical filter groups

IN-01.3.018a-e, 2026-10-29. Production compatibility/adoption remains 018f.
Paths below are relative to backend/ingestion-service.

## Source contract

PostgreSQL `internal/store/postgres/tenant_data_reader.go`,
buildAggregateFilterSQL, supports AND/OR/NOT, defaults omitted kind to group and
omitted group operator to AND, and normalizes kind/operator with trim/lowercase.
NOT requires exactly one child. Each child SQL clause is parenthesized and values
are appended in depth-first input order. Empty children disappear: empty AND,
empty OR and NOT(empty) all emit no filter. This differs from treating empty OR
as false and is preserved here rather than silently changed.

Domain `internal/domain/ingestion/aggregate.go` sets maximum depth 5, children 20
and nodes 50. Its recursive validator recounts each subtree, making the effective
node count the sum of node depths for ordinary trees. The prior source audit pins
a 22-node, depth-3 tree rejected at weighted count 63. This batch does not change
that production validator or reinterpret its currently accepted set of trees.

## Implementation

`internal/store/mysql/record_filter.go` adds RecordFilter and
`(*RecordProjection).CompileFilter`. First validate the whole tree with bounded
traversal: check depth before descending, group width before visiting children and
accumulate each node's depth once, rejecting weighted count above 50. Cyclic caller
slices stop at the depth bound. This avoids calling the existing unbounded shape
measurement before validation. Compilation then walks only the bounded tree.

NOT arity, unknown kind/operator and predicate children fail validation. Rejecting
predicate children is deliberately stricter than the source builder, which ignores
them; this staged policy must be reviewed before API adoption. Nil filter means
no filter but still requires an initialized projection. Caller must not mutate the
input during compilation; no concurrent caller mutation guarantee is provided.

Leaves delegate to ScalarPredicate or MembershipPredicate. Thus the staged
normalized numeric/Boolean/timestamp and single-column null-check capabilities
apply; string comparisons and multi-column IP remain unsupported. Nested groups
do not widen accepted leaf types or introduce NOT IN, field comparisons or a new
query execution API. Case normalization and defaults match the source builder.

AND/OR joins wrap each nonempty child; NOT wraps exactly its one nonempty child.
Empty clauses retain source behavior, including top-level empty OR. SQL() returns
an empty clause and no arguments for an unfiltered result; the caller must omit
WHERE deliberately. An empty clause is not an always-false predicate or a successful
record query. This batch performs no query itself and claims no database
short-circuit order. SQL NULL/UNKNOWN semantics remain intact under NOT.

Arguments retain depth-first order, with membership order/duplicates/NULL intact.
Compiled arguments are detached from input list/tree and each SQL() return owns its
slice. A failure returns no clause or arguments even after earlier leaves compiled.
Errors retain ErrRecordFilter and the leaf error where applicable via errors.Join.

## Ownership and remaining work

Use the originating projection's table and already-authorized READ COMMITTED
transaction, holding its publication barrier through execution/collection. A
fragment cannot authorize a caller or enforce its own lifetime. No cache, network,
mutation or transaction boundary is introduced. Callers still need SQL row limits,
bounded projection/collection and whole-statement parameter/payload budgets under
020. Tree/list bounds here are not a global query budget or an HTTP JSON body cap.

018f must reconcile predicate-child rejection and weighted-count policy, verify
live PostgreSQL/MySQL parity, and integrate authorized runtime paths with explicit
empty-clause handling, pre-parse/body limits, error mapping and whole-query budgets.
Source tests establish observed behavior, not full parity for the stricter staged
leaf contracts. Production shape traversal remains unchanged and is not fixed by
this optional compiler. Representative query plans/concurrency acceptance remain
open. No performance improvement is claimed without measurements.

## Verification

Five new race-enabled isolated MySQL tests in record_filter_test.go cover:

- AND/OR/NOT and nested scalar/membership matches, including NULL under NOT.
- Nil/empty/default groups, NOT(empty), and removal of empty children.
- Parentheses, normalized operators, depth-first parameters and detached inputs.
- Depth 5 accepted/6 rejected, 20 children accepted/21 rejected, weighted count
  exactly 50 accepted/53 rejected, existing weighted-63 rejection and cyclic input.
- Invalid arity/kind/operator, predicate children, unsupported leaves, no partial
  output after an earlier successful leaf, and preservation of leaf error causes.

Five scalar and five membership regressions also passed; all 15 tests passed
without skips, with go vet ./... passing. Evidence:
[logical-filter test log](../evidence/2026-10-29_ingestion-logical-filters.log).
Only disposable fixtures were used. PostgreSQL/River remains the configured
runtime; no dependency, deployment schema or application database changed.
