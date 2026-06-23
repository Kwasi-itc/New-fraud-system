# Stress Test Protocol

Use this protocol to define every stress test before adding or running it.

## 1. Objective

State the question the test answers.

Example:

```text
Find the maximum sustainable direct decision evaluations per second before p95 latency exceeds 500ms.
```

## 2. System Under Test

Name the exact path being tested.

Examples:

- direct decision evaluation
- ingestion callback fan-out
- aggregate-heavy rule evaluation
- decision write amplification
- mixed workload behavior

## 3. Controlled Variables

List what must stay fixed so results are comparable.

Examples:

- service versions or git SHA
- database size and seed shape
- scenario count
- rules per scenario
- payload shape
- hardware or environment
- service configuration
- dependency availability

## 4. Independent Variables

List what the test intentionally changes.

Examples:

- request rate
- concurrent users
- scenario count
- rules per scenario
- seeded record count
- payload size
- aggregate complexity
- downstream latency

## 5. Workload Shape

Define how load is applied.

Examples:

- constant request rate
- ramp-up
- spike
- soak
- mixed workload
- burst after idle

## 6. Measured Outputs

List the metrics that must be recorded.

Examples:

- p50, p95, and p99 latency
- max latency
- requests per second
- success, error, and timeout rate
- database query time
- CPU and memory
- queue or outbox backlog
- dependency latency

## 7. Expectation / Pass Criteria

Define what acceptable behavior means before the run starts.

Examples:

- p95 latency below 500ms
- p99 latency below 2s
- error rate below 0.1%
- no sustained memory growth
- no backlog growth after load stops

## 8. Run Procedure

Document the exact steps needed to reproduce the test.

Example:

```text
1. Reset the database.
2. Seed the dataset.
3. Warm up for 2 minutes.
4. Run load for 10 minutes.
5. Collect service and infrastructure metrics.
6. Export the stress report.
```

## 9. Result Interpretation

State how the result should be classified.

Examples:

- pass
- regression from previous baseline
- saturation point found
- bottleneck requires profiling
- inconclusive due to dependency instability

## 10. Artifacts

Save enough context to explain and reproduce the result.

Examples:

- test configuration
- summary report
- raw metrics
- service logs
- environment metadata
- git SHA
- database seed size

## Summary

Every stress test should define the question, isolate the variables, run a reproducible workload, measure agreed outputs, compare against an explicit expectation, and save enough context to explain or reproduce the result.

## Planned Decision Engine Tests

These are the initial decision-engine stress tests to define and perform. Each test should be expanded separately using the protocol above before implementation or execution.

### 1. Throughput Limit

Question:

```text
What is the maximum sustainable decision evaluations per second before p95 latency or error rate breaches the target?
```

Test definition:

- Objective: find the maximum sustainable direct decision evaluations per second.
- System under test: direct scenario evaluation only, using `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/evaluate`.
- Expected target: sustain `1000` evaluations per second with `0%` errors.
- Sustainability definition: the highest confirmed evaluations-per-second level that completes the measured run with `0%` errors, `0` timeouts, and `0` dropped requests.

Controlled variables:

- services run locally in Docker
- service versions and git SHA
- Docker/container configuration
- host machine CPU and memory information
- database engine/version and container resource limits
- decision-engine service configuration
- data-model service configuration
- ingestion service configuration
- database size at start of run
- one tenant
- one direct-evaluation scenario
- one simple payload-based rule
- fixed payload shape and payload size
- minimal tenant data volume
- same measured duration per EPS level, defaulting to `60` seconds unless overridden
- same warmup duration per EPS level
- cooldown and service readiness check between trial attempts

Independent variables:

- target evaluations per second
- virtual users / concurrency available to sustain that target rate

Workload shape:

- constant arrival-rate runs at each target RPS level
- workload shape is not the primary subject of this test
- concurrency should be high enough to allow the target RPS, but recorded because it can affect latency and saturation behavior
- each attempted EPS level is isolated by a cooldown period before the next attempted EPS level starts

Measured outputs:

- achieved evaluations per second
- p50, p95, and p99 latency
- max latency
- success rate
- error rate
- timeout rate
- dropped request count
- HTTP failure rate
- outbox pending and failed counts where available
- CPU and memory where available
- database connection usage where available
- database query latency where available

Result interpretation:

- the throughput limit is the highest confirmed EPS that sustains the measured run with `0%` errors, `0` timeouts, and `0` dropped requests
- if `1000` evaluations per second is reached with `0%` errors, the test passes the initial expected target
- if a candidate EPS passes during search but fails confirmation, the result is downgraded to the next lower candidate that also passes confirmation
- if `1000` evaluations per second is not reached, the highest confirmed sustainable EPS becomes the baseline and the first failing EPS should be used for bottleneck investigation

### 2. Rule Complexity Scaling

Question:

```text
How does rule complexity affect performance, especially with aggregates, related records, custom lists, decision history, and nested logical conditions?
```

### 3. Scenario Scaling

Question:

```text
How does performance change as the number of scenarios and rules in the system increases?
```

### 4. Data Volume Sensitivity

Question:

```text
How does evaluation latency change as the tenant dataset grows from small to large volumes?
```

### 5. Soak Stability

Question:

```text
Can the engine sustain expected production load over time without rising latency, memory growth, errors, or backlog accumulation?
```
