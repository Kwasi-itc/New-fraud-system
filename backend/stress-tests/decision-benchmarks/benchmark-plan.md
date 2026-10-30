# Progressive benchmark plan

Status: proposed, awaiting review. No workloads have been run. Scope is the production decision API, with separate evaluator diagnostics and optional async completion experiments. [Capabilities](capability-matrix.md) determine which cases qualify.

## Experiment identity and fixture contract

Every matrix cell gets `L<level>-<family>-S<count>-R<count>-H<rows>-<variant>` plus a unique run/repetition ID. Store canonical ASTs, trigger, score modifiers, thresholds, stable rule IDs, publication/model revisions, dependency fixtures and input seed in a hashed manifest. Record configured, selected and triggered scenario counts separately. Rule count means rules per scenario, not AST node count; record node count, depth, distinct subtree count and unique aggregate query count too.

Primary endpoints: single scenario `/v1/tenants/{tenant}/scenarios/{scenario}/evaluate`; multi-scenario `/v1/tenants/{tenant}/decisions/all`. Keep route choice fixed within a comparison. Single-scenario and all-scenarios-with-one-scenario are separate controls. Inline and stored-object evaluation are separate variants. Use synthetic data and retain intended authentication.

Default baseline fixture: one live scenario; trigger constant true; exactly one rule with score modifier 1; thresholds review=1, block_and_review=2, decline=3; no configured workflows, screening or scoring actions. Hit => score 1/review; miss => score 0/approve. This measures normal persistence, including platform behavior that still runs with no configured actions. Explicit trigger-false control expects no decision row; do not call it an approve decision.

Proposed flat transaction model: required `amount` float, `channel` string, `country` string, `is_verified` bool, `timestamp` timestamp; nullable `merchant_id` string and `note` string. Add `limit_amount`, customer/destination keys and declared padding fields only for the relevant experiments. All valid payloads contain required fields; missing cases remove one field at a time. Base fields: amount=3001, channel=mobile, country=GH, is_verified=false, timestamp=2026-01-15T12:00:00Z, merchant_id=m-1, note=null. Object ID is unique except explicitly stateful experiments.

Actual single-rule AST:

```json
{"function":"gt","children":[{"function":"field_ref","named_children":{"field":{"constant":"amount"}}},{"constant":3000}]}
```

The following tables use the documented `F`/`C` shorthand from the capability matrix. Before Stage C performance runs, materialize these into the existing JSON fixture format and assert authoring, API response and persisted state.

## Level 1: one scenario, one rule

| ID | Formula | Match / nonmatch / boundary | Missing, null and malformed expectations |
|---|---|---|---|
| L1-NUM | `gt(F(amount),C(3000))` | 3001 hit; 2999 miss; 3000 miss. Add gte/lt/lte/eq/neq counterparts | Required missing/null => 422; numeric string `"3001"` normalizes and hits; `"abc"`, object, list, boolean => invalid type. Nullable numeric variant => null ordered comparison false |
| L1-STR | `eq(F(channel),C("mobile"))` | mobile hit; web miss; Mobile miss; empty string miss | Missing/null required => 422; number/list => 422. Add lower-normalized variant and empty/prefix/substring cases |
| L1-LIST | `in(F(country),C(["GH","NG","KE"]))` | GH/NG/KE hit; US/gh miss; first/last item, empty RHS | Missing/null required => 422; wrong payload type => 422. Invalid nonlist RHS is authoring rejection. Add IsNotInList and duplicates/mixed-type RHS runtime cases |
| L1-BOOL | `eq(F(is_verified),C(false))` | false hit; true miss | `" false "` normalizes and hits; null/missing => 422; `0`, `"no"` invalid. Boolean ordering is invalid authoring |
| L1-NONNULL | `not(is_null(F(merchant_id)))` | m-1 hit; null/missing miss; empty string hit | Number/list invalid type. This is non-null checking, not a true exists predicate. Add IsNotEmpty where empty string misses |
| L1-DATE | `and(gte(F(timestamp),ParseTime(C(start))),lt(F(timestamp),ParseTime(C(end))))` | start=2026-01-01T00:00:00Z, end=2026-02-01T00:00:00Z; inside/start hit; end/before start miss | Missing/null required => 422; malformed date => 422. Equal instants with different offsets agree; add fractional-second boundaries |
| L1-FIELD | `gt(F(amount),F(limit_amount))` | 3001>3000 hit; 2999>3000 and equality miss | Add declared numeric second field; malformed/missing required => 422 |
| L1-CONTROL | constant true/false trigger; each with same one-rule formula | True trigger evaluates rule; false trigger returns no decision/rules | No live scenario and wrong object type are separate API contracts, not fast successful matches |

Additional correctness-only probes: canonical/compatibility aliases, null equality, empty AND/OR authoring rejection, list element type errors, string timestamps, int64 values above 2^53, division by zero and malformed durations. Do not average invalid-input latency into valid-rule latency. For expected 422 cases, report correct rejection separately from successful decision count.

## Level 2: rules within one scenario

Rule counts **1, 2, 5, 10, 25, 50, 100**. First sweep payload-only numeric rules at concurrency 1 and the deployment default, with all hit, all miss and deterministic 50% hit configurations. Compute expected score by independently summing known modifiers; keep identical thresholds across a given sweep and derive expected outcome for every score.

Then use the same counts with (a) identical ASTs, (b) distinct constants/fields with equivalent match rate, (c) mixed scalar/string/date/list cost. This exposes memoization rather than attributing all behavior to count. Single-rule AST complexity is an independent sweep: 2, 5, 10, 25, 50, 100 predicates; shallow versus depth 2/4/8; early/late AND false or OR true; all branches needed. These do not imply short-circuit across separate rules.

Include 0%, 50%, 100% snoozed controls after basic counts pass. Observe evidence bytes, rule inserts and tx time. Upper counts are staged only after resource preflight; no arbitrary engine limit was found, and count is distinct from worker concurrency caps.

## Level 3: scenarios per input

Scenario counts **1, 2, 5, 10, 25, 50** with first R=1, then R=5 and 10. Avoid the full S x R x operator x concurrency Cartesian product initially. Run trigger-match ratios 0%, 50%, 100%; then rule match ratios 0%, 50%, 100% among triggered scenarios. Include unrelated object-type scenarios and unpublished scenarios that must never be selected.

Compare identical overlapping formulas against distinct formulas. Vary thresholds to yield approve/review/block_and_review/decline across scenarios and assert the complete per-scenario result set. There is no final priority winner to assert. Test scenario concurrency 1/default, then approved explicit limits while holding rule concurrency fixed. Include one failing scenario with successful siblings: confirm observed persisted partial results and do not retry blindly. Untriggered result entries lack scenario IDs, so use known fixture order/count plus persistence checks or separate single-scenario validation; document this observability limit rather than inventing identity.

## Level 4: historical aggregates

Use 0, 1 and small hand-calculated histories for correctness, then **10,000; 100,000; 1,000,000; 5,000,000** rows where approved. Five million is an optional ceiling, not a required local run. Count rows actually seeded; preserve table bytes, index bytes, null fraction, key cardinality, timestamp distribution and matching-row count. Total table size alone does not specify query cost.

| ID | Workload | Independent expected value / controlled axes |
|---|---|---|
| L4-COUNT | COUNT(object_id), customer + `[anchor-5m,anchor)` | Count known qualifying records, exclude exact upper boundary; include exact lower boundary |
| L4-SUM | SUM(amount), same merchant/window | Sum generator's selected values, signed/zero/null cases; empty => 0 |
| L4-AVG | AVG(amount), customer/window | Sum / number of non-null amounts; no qualifying values => null |
| L4-DISTINCT | COUNT_DISTINCT(destination_id) | Independent set excluding null; repeated destination/merchant fixtures |
| L4-MINMAX | MIN/MAX(amount), optional timestamp field variant | Extremes or null for empty; requires runtime allow-list |
| L4-HISTAVG | current amount > configured multiple of historical AVG | Independent historical calculation and zero/null baseline semantics; current transaction explicitly excluded by ID/time or not ingested |
| L4-DECISIONS | past_decision_count on one object, optional outcome | Separate decision-row counts 0/10/100/1k/10k before approved larger values; measured writes change counts, use per-request known sequence or fresh equivalent object histories |

Pin event time using payload timestamp or literal anchor for deterministic aggregate fixtures; reserve `TimeNow` for a separately recorded moving-window workload. Build histories at just before/on/after both boundaries. Test null handling, empty IN, duplicate values, nested filters and SQL NOT/NULL independently. Use a Python reference over generated fixture parameters/streamed rows with explicit SQL null semantics; do not call the production evaluator to create expectations. Validate smaller samples against independently written parameterized SQL in the disposable database.

Cross two selectivities (approximately 0.01% and 10%, recording actual counts), uniform keys and a 90%-hot-key variant. Compare shared versus unique aggregate query shapes per input. Run HTTP and direct_db as distinct configuration cohorts against the same logical schema and dataset. Inspect and save `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` for representative read-only queries outside timed load, with bound values, PostgreSQL version, statistics and index definitions. ANALYZE executes reads and requires approved scale resources. Check related-key lookup and decision-history plans too.

Warm metadata/DB steady state, process-restart metadata cold with DB warm, and true DB/OS cold are different experiments. Restarting the app or using fresh IDs does not clear PostgreSQL buffers/OS cache. True cold experiments require an isolated approved restart/cache protocol; otherwise label DB cache state unknown. Never flush shared infrastructure.

## Level 5: mixed workloads

Initial proposed mix by submitted input: 50% scalar-only family, 20% nested string/list/date family, 20% count/sum/avg family, 10% declared relationship/custom-list family. Use separate object types/scenario trigger partitions so requested fractions correspond to actual selected work; record realized selections and matches. Start S=5/R=5 per family. Set valid-rule hit ratio to 30% using known deterministic inputs; keep 1% deliberately invalid inputs in a separate labeled variant, not the baseline.

Payload sizes approximately 1/10/100 KiB using declared string padding, recording exact encoded bytes and response/evidence size. Key distributions: uniform and hot customer/merchant. Tenants: 1, 2 and 10 with duplicate object IDs/reference values across tenants but distinct expected results. Concurrent ingestion: off, then a separately rate-limited approved stream with exact operation/time ledger. Immutable-history experiments remain distinct from evolving-history experiments. Add enabled workflows/screening/scoring only as a separately provisioned dependency profile, with side effects restricted to test endpoints.

## Level 6: load, concurrency, recovery and soak

Start low-load correctness smoke, then VUs 1,2,4,8 with one outstanding request per VU. Extend 16/32/64 only after shared database/generator headroom is checked. Sweep one concurrency layer at a time; record API replicas, rule/scenario/aggregate limits and every pool/worker. Use existing closed-loop runner patterns for saturation diagnostics, and existing fixed-arrival campaign scheduler for offered-rate experiments.

Derive fixed rates from the observed stable pilot throughput (for example 25%, 50%, 75%, 100%, then small increments); these are experimental rates, not service targets. Report target and achieved send/completion rates separately. Proposed short trial: warm-up at least 30 seconds and longer if caches/queues have not stabilized, then 120 seconds measured, bounded drain, recovery gate and cooldown. Run at least 3 independent repetitions, preferably 5 near the knee; randomize/interleave order and reset datasets equivalently. Keep warm-up writes out of stateful oracle histories or account for them explicitly.

Burst proposal: stable rate, bounded 2x pulse for 10 seconds, return to baseline; stop on resource guardrail, unacceptable backlog or correctness anomaly. Soak proposal: 30 minutes pilot followed by an approved 2-hour run at an observed stable rate. Record time series and first/last-window behavior. Capacity against an SLO is **blocked until objectives are supplied**; without them report the observed saturation knee and errors, not production capacity.

Generator controls: CPU/memory/network/socket usage, event-loop scheduling lag, offered/sent/dropped slots, max-inflight and verification time. Reject a fixed-rate run as capacity evidence when generator saturation prevents the intended arrivals; retain it as an invalid run with reason. Schedule-based completion latency must include delay rather than conceal coordinated omission. Use bounded result queues and preserve correctness checks; scale the generator if verification becomes expensive.

Optional async: measure acceptance, execution queue wait, terminal completion and polling separately; reconcile persisted decisions, failed jobs and unresolved timeouts. Reuse completion/identity ledger and queue-recovery modules. HTTP 202 never counts as a synchronous completed decision. Fault injection only in disposable approved fixtures: database unavailability/timeout, worker interruption and downstream failure; prove restoration and rollback/partial-commit behavior before load.

## Safety, measurement and acceptance

Use one isolated shared PostgreSQL database for the participating service schemas, with separately tracked ordered migrations. Never target the application database, even with a new tenant. Preflight explicit endpoint/DB allow-list, disposable identity, owner/run marker, source/image revision, auth mode, quotas and shared connection budget. Generate/cleanup only owned benchmark fixtures; preserve failed-run artifacts and journal partial setup. Root application Compose is not automatically an isolated benchmark environment.

All runs collect the [result contract](results-and-reporting.md). Compare expected trigger, scenario identity where available, exact rule hit/miss/snooze set, score/outcome, persisted evidence and expected error categories. Verify each response; reconcile writes separately in bounded reads outside timed load where possible. Every successful fixture has zero unexplained semantic mismatches before it qualifies for performance comparison. Expected rejections are correct negative outcomes, not completed decisions. Unknown commits after timeout remain unresolved until reconciliation.

Approval of this plan does not imply that fixtures are implemented, tested or that any performance result exists. Stage completion is tracked in the [checklist](implementation-checklist.md).
