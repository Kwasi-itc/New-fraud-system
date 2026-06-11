# Stress Tests

k6 stress tests for the ingestion service and decision-engine runtime paths.

These tests are intentionally separate from `integration-tests` because they create load, depend on timing thresholds, and should not run as part of normal functional CI.

## Tool Choice

This suite uses k6 instead of Locust because it has stronger repeatable reporting for service stress tests:

- built-in pass/fail thresholds for CI
- compact terminal summaries with percentile latency stats
- JSON export for later analysis
- optional local web dashboard and Grafana Cloud k6 reporting
- single-file JavaScript workloads that are easy to run without a Python harness

## Prerequisites

- k6 installed
- data-model, ingestion, and decision-engine services running and migrated

Default service URLs:

```bash
DATA_MODEL_URL=http://localhost:8080
INGESTION_URL=http://localhost:8081
DECISION_ENGINE_URL=http://localhost:8082
```

If service auth is enabled:

```bash
export SERVICE_AUTH_TOKEN=<token>
```

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
