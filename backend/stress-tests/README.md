# Stress Tests

Stress tests for the ingestion service and decision-engine runtime paths.

These tests are intentionally separate from `integration-tests` because they create load, depend on timing thresholds, and should not run as part of normal functional CI.

## Tool Choice

This suite has two styles of tests:

- k6 profiles for broad ingestion and mixed-workload service stress.
- Python harnesses for protocol-driven decision-engine throughput, rule complexity, and scenario-scaling tests.

The k6 tests are useful for:

- built-in pass/fail thresholds for CI
- compact terminal summaries with percentile latency stats
- JSON export for later analysis
- optional local web dashboard and Grafana Cloud k6 reporting
- single-file JavaScript workloads that are easy to run without a Python harness

The Python tests are useful when setup needs to generate many tenants, scenarios, rules, seeded records, and per-trial JSON summaries.

## Prerequisites

- k6 installed
- data-model, ingestion, and decision-engine services running and migrated

Default service URLs:

```bash
DATA_MODEL_URL=http://127.0.0.1:8080
INGESTION_URL=http://127.0.0.1:8081
DECISION_ENGINE_URL=http://127.0.0.1:8082
```

If service auth is enabled:

```bash
export SERVICE_AUTH_TOKEN=<token>
```

If the active Docker stack runs data-model and ingestion against separate Postgres databases, pass the ingestion database URL to Python tests that seed tenant data:

```bash
--ingestion-database-url 'postgres://fraud:fraud@localhost:5432/ingestion?sslmode=disable'
```

Without that option, related-record and related-field variants can fail with `SQLSTATE 42P01` because data-model creates tenant tables in its own database while ingestion writes to a separate database.

## Profiles

The suite supports these profiles through `STRESS_PROFILE`:

- `concurrent_single_ingest`: many concurrent single-record `POST /ingest` calls.
- `decision_evaluation`: direct `POST /scenarios/{scenario_id}/evaluate` load against a simple live scenario.
- `heavy_rule_evaluation`: direct evaluation against a seeded related-record velocity rule.
- `ingestion_callback`: `POST /ingestion-events/record-ingested`, exercising all live scenarios for the object type.
- `mixed`: a blend of single ingest, simple decision evaluation, heavy rule evaluation, and ingestion callback.

Per your current scope, this excludes ingestion-throughput batch testing and idempotency-pressure testing.

## Run

From the backend root:

```bash
k6 run stress-tests/decision-ingestion-stress.js
```

## Decision Throughput Limit

Deprecated: `decision_throughput_limit.py`, `decision_throughput_orchestrator.py`, and the Marble equivalents are retained for historical context only. Use the closed-loop and scaling harnesses below for current comparison work.

The first protocol-driven decision-engine stress test is a Python orchestrator. It targets direct scenario evaluation only and automatically finds the highest sustainable evaluations-per-second rate with zero errors.

Run a small smoke test:

```bash
python stress-tests/decision_throughput_orchestrator.py --target-eps 5 --start-eps 2 --trial-duration 3 --warmup-duration 0 --rate-floor 1 --cooldown-duration 1
```

Run the default throughput search:

```bash
python stress-tests/decision_throughput_orchestrator.py
```

The orchestrator writes:

```text
stress-tests/throughput-runs/<timestamp>/orchestration-summary.json
```

It runs increasing EPS trials, binary-searches after the first failure, confirms the final passing rate, and marks a trial as sustainable only when it has zero failures, zero timeouts, and zero dropped requests. The default measured duration is 60 seconds, with a 30 second warmup and a 30 second cooldown between trial attempts.

Between attempts the orchestrator waits for cooldown, checks `/readyz` on data-model, ingestion, and decision-engine, and captures database/outbox diagnostics through the local Postgres container. If the highest candidate fails confirmation, the orchestrator confirms the next lower candidate before reporting `highest_sustainable_eps`.

Useful throughput options:

```bash
python stress-tests/decision_throughput_orchestrator.py \
  --target-eps 1000 \
  --trial-duration 60 \
  --cooldown-duration 30 \
  --health-timeout 60
```

Run one manual trial when investigating a specific rate:

```bash
python stress-tests/decision_throughput_limit.py --rate 500 --vus 1000 --duration 60
```

## Closed-Loop VU Baseline

Use closed-loop VU tests to measure how much throughput the system gets from a fixed number of concurrent workers. Each VU sends the next request immediately after its previous request completes.

```bash
python3 stress-tests/decision_closed_loop_vus.py --vus=5 --duration=60
```

The summary is written with the VU count and duration in the filename:

```text
stress-tests/closed-loop-vus-summary-5-vus-60s.json
```

## Rule Complexity Scaling

Use `decision_rule_complexity_scaling.py` to compare direct scenario evaluation performance across rule types.

Default variants:

- `baseline_payload`: amount threshold only.
- `nested_payload`: payload-only company transaction checks over amount, processor, channel, currency, country, and transaction id.
- `custom_list_related_fuzzy`: related merchant name fuzzy match against a merchant blacklist custom list.
- `related_field`: related account, merchant, and product lookups.
- `decision_history`: prior decision lookup.
- `related_records_count`: fetch related records and count them in the decision engine.
- `aggregate_count`: count seeded transactions for the same merchant.
- `aggregate_velocity`: sum merchant transaction amount inside the velocity window.
- `mixed_heavy`: nested payload, merchant blacklist fuzzy match, related fields, aggregate count, and aggregate velocity.

The current company-domain transaction payload uses:

```json
{
  "account_ref": "2332416370369",
  "processor": "uniwallet",
  "merchant_id": "dbb82c30-d9df-4c9c-bf96-5be052f644e8",
  "product_id": "2ae50a9e-0487-4436-a11a-cb486a04c168",
  "transaction_id": "wallet__uniwallet__<uuid>",
  "date": "2025-01-30T12:00:28Z",
  "amount": 1800,
  "currency": "GHS",
  "country": "GH",
  "channel": "wallet"
}
```

Each trial seeds three accounts, three merchants, three products, and a merchant-name blacklist where needed. Generated requests randomly choose account, merchant, product, processor (`genpay`, `uniwallet`), and channel (`card`, `wallet`, `bank`). Transaction time is monotonic and advances by a random interval between one hour and two days.

Run the default rule-complexity matrix:

```bash
python3 stress-tests/decision_rule_complexity_scaling.py \
  --vus=5 \
  --duration=60 \
  --ingestion-database-url 'postgres://fraud:fraud@localhost:5432/ingestion?sslmode=disable'
```

Run selected variants:

```bash
python3 stress-tests/decision_rule_complexity_scaling.py \
  --variants=baseline_payload,custom_list_related_fuzzy,aggregate_count,aggregate_velocity,mixed_heavy \
  --vus=5,10 \
  --duration=60 \
  --history-object-pool-size=1000 \
  --ingestion-database-url 'postgres://fraud:fraud@localhost:5432/ingestion?sslmode=disable'
```

Important options:

- `--variants`: comma-separated rule complexity variants.
- `--vus`: comma-separated closed-loop VU levels.
- `--related-seed-count`: number of related transaction records seeded for related-record and aggregate variants. Default: `100`.
- `--history-object-pool-size`: number of preseeded objects used by decision-history variants. Default: `100`.
- `--ingestion-database-url`: optional Postgres URL used to materialize tenant tables in ingestion's database when ingestion uses a separate DB.

Decision-history variants seed one prior review decision per object in the history object pool, then measured requests cycle through that pool. This avoids concentrating all measured decisions on a single object and turning the test into an unbounded history-growth benchmark.

Outputs:

```text
stress-tests/rule-complexity-runs/<timestamp>/summary.json
stress-tests/rule-complexity-runs/<timestamp>/trial-<variant>-<vus>-vus-<duration>s.json
```

## Scenario Scaling

Use `decision_scenario_scaling.py` to test live-scenario fanout, rules-per-scenario scaling, and rule complexity in one matrix.

The workload uses:

```text
POST /v1/tenants/{tenantId}/decisions/all
```

This evaluates all live scenarios for the object type on each request.

Recommended first run:

```bash
python3 stress-tests/decision_scenario_scaling.py \
  --scenario-counts=1,5,10 \
  --rules-per-scenario=1,5,10 \
  --complexities=baseline_payload,mixed_heavy \
  --vus=5 \
  --duration=60 \
  --ingestion-database-url 'postgres://fraud:fraud@localhost:5432/ingestion?sslmode=disable'
```

Important options:

- `--scenario-counts`: comma-separated live scenario counts.
- `--rules-per-scenario`: comma-separated rule counts created in each scenario.
- `--complexities`: comma-separated rule complexity variants. `baseline_payload` is auto-added when omitted so ratios can be calculated.
- `--vus`: comma-separated closed-loop VU levels.
- `--related-seed-count`: number of related records seeded for related-record and aggregate complexity variants. Default: `100`.
- `--ingestion-database-url`: optional Postgres URL used for split data-model/ingestion DB setups.

Each trial records:

- request RPS
- expected decision writes per second
- expected rule execution writes per second
- total rules per request: `scenario_count * rules_per_scenario`
- p50, p95, p99, max latency
- success, error, and timeout rates

Outputs:

```text
stress-tests/scenario-scaling-runs/<timestamp>/summary.json
stress-tests/scenario-scaling-runs/<timestamp>/trial-<complexity>-<scenario_count>-scenarios-<rules_per_scenario>-rules-<vus>-vus-<duration>s.json
```

Run a specific profile:

```bash
STRESS_PROFILE=decision_evaluation k6 run stress-tests/decision-ingestion-stress.js
```

Tune load:

```bash
STRESS_PROFILE=heavy_rule_evaluation \
STRESS_RATE=10 \
STRESS_DURATION=5m \
STRESS_PRE_ALLOCATED_VUS=30 \
STRESS_MAX_VUS=80 \
k6 run stress-tests/decision-ingestion-stress.js
```

For the `concurrent_single_ingest` and `mixed` profiles, use VU count:

```bash
STRESS_PROFILE=mixed \
STRESS_VUS=40 \
STRESS_DURATION=5m \
k6 run stress-tests/decision-ingestion-stress.js
```

## Reports

The script writes a focused service-performance summary after every run:

```text
stress-tests/performance-summary.json
```

This file emphasizes workload behavior over generic HTTP/network details:

- profile and load settings
- tenant/object/scenario ids used in the run
- setup counts: scenarios, rules, seeded records, workflows, screening configs, scoring configs
- runtime counts: ingests, direct evaluations, heavy evaluations, callback evaluations
- success rates
- p95/p99/max latency for ingest, direct decision, heavy decision, callback, and overall HTTP

Write a machine-readable summary:

```bash
k6 run --summary-export stress-tests/summary.json stress-tests/decision-ingestion-stress.js
```

Write request-level metrics for later processing:

```bash
k6 run --out json=stress-tests/results.json stress-tests/decision-ingestion-stress.js
```

Use the local k6 web dashboard:

```bash
K6_WEB_DASHBOARD=true \
K6_WEB_DASHBOARD_EXPORT=stress-tests/report.html \
k6 run stress-tests/decision-ingestion-stress.js
```

Run the heavy rule evaluation in isolation:

```bash
STRESS_PROFILE=heavy_rule_evaluation \
STRESS_RATE=20 \
STRESS_DURATION=2m \
k6 run stress-tests/decision-ingestion-stress.js
```

The heavy-only run reports `stress_heavy_decision_latency` and `stress_heavy_evaluations` separately from the simple direct-decision metrics.

Run mixed workload with a bounded total arrival rate:

```bash
STRESS_PROFILE=mixed \
STRESS_EXECUTOR=arrival-rate \
STRESS_RATE=20 \
STRESS_DURATION=2m \
STRESS_PRE_ALLOCATED_VUS=40 \
STRESS_MAX_VUS=80 \
k6 run stress-tests/decision-ingestion-stress.js
```

Run with many scenarios and rules:

```bash
STRESS_PROFILE=ingestion_callback \
STRESS_EXECUTOR=arrival-rate \
STRESS_RATE=10 \
SCENARIO_COUNT=20 \
RULES_PER_SCENARIO=5 \
STRESS_DURATION=2m \
k6 run stress-tests/decision-ingestion-stress.js
```

That creates 20 simple live scenarios with 5 rules each. For `ingestion_callback`, one callback request evaluates all live scenarios for the object type.

Run heavy rule evaluation across many heavy scenarios:

```bash
STRESS_PROFILE=heavy_rule_evaluation \
STRESS_EXECUTOR=arrival-rate \
STRESS_RATE=10 \
SCENARIO_COUNT=20 \
RULES_PER_SCENARIO=5 \
HEAVY_SEED_RECORDS=1000 \
STRESS_DURATION=2m \
k6 run stress-tests/decision-ingestion-stress.js
```

That creates 20 simple scenarios plus 20 heavy scenarios. Heavy evaluation requests are spread across the heavy scenarios.

## Environment Variables

- `DATA_MODEL_URL`: data-model service URL.
- `INGESTION_URL`: ingestion service URL.
- `DECISION_ENGINE_URL`: decision-engine service URL.
- `SERVICE_AUTH_TOKEN`: optional bearer token used for all protected API calls.
- `STRESS_PROFILE`: workload profile. Default: `mixed`.
- `STRESS_EXECUTOR`: `auto`, `arrival-rate`, or `constant-vus`. Default: `auto`.
- `STRESS_RATE`: iterations per second for arrival-rate profiles. Default: `20`.
- `STRESS_VUS`: VUs for VU-based profiles. Default: `20`.
- `STRESS_DURATION`: test duration. Default: `2m`.
- `STRESS_PRE_ALLOCATED_VUS`: starting VU pool for arrival-rate profiles.
- `STRESS_MAX_VUS`: maximum VU pool for arrival-rate profiles.
- `SCENARIO_COUNT`: number of scenario sets to create. Default: `2`.
- `RULES_PER_SCENARIO`: number of rules per created scenario. Default: `2`.
- `HEAVY_SEED_RECORDS`: records seeded for heavy related-record evaluation. Default: `750`.
- `THINK_TIME_SECONDS`: optional delay after each iteration. Default: `0`.

Executor behavior:

- `arrival-rate`: k6 targets `STRESS_RATE` iterations per second and uses `STRESS_PRE_ALLOCATED_VUS` up to `STRESS_MAX_VUS` to maintain that rate.
- `constant-vus`: k6 runs `STRESS_VUS` workers as fast as possible for `STRESS_DURATION`.
- `auto`: uses `constant-vus` for `mixed` and `concurrent_single_ingest`, and `arrival-rate` for the decisioning profiles.

## Setup Behavior

The k6 `setup()` phase:

1. waits for all three services to return `200` from `/readyz`
2. creates and provisions a fresh tenant
3. creates transaction/account tables and fields using the same model shape as the integration tests
4. publishes `SCENARIO_COUNT` simple decision scenarios with `RULES_PER_SCENARIO` rules each
5. adds workflow, screening, and scoring configs to amplify decision-write behavior
6. seeds related records and publishes `SCENARIO_COUNT` heavy scenarios only for `heavy_rule_evaluation` and `mixed`

Runtime load metrics therefore focus on the selected workload, not tenant or scenario setup.

## Thresholds

The default thresholds are intentionally conservative starting points:

- checks must pass at least 99%
- HTTP request failure rate must stay below 1%
- overall p95 latency below 2s and p99 below 5s
- ingest p95 below 1.5s
- decision p95 below 2.5s
- heavy decision p95 below 3s
- ingestion callback p95 below 3s

Adjust these after you have a baseline from a known-good local or staging run.
