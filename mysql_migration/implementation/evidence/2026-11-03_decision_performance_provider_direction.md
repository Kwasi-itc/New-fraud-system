# Performance target and PostgreSQL provider compatibility

## User direction

On 2026-11-03 the user requested designing for **1,000 evaluations per second**
and retaining the providers used by the PostgreSQL implementation. This resolves
the provider approach: preserve existing integration boundaries and business
semantics; do not invent a new scoring engine or event receiver.

The throughput is a design target, not a benchmark result or runtime setting.
No application database, service configuration or worker count was changed.

## Benchmark design

Use 1,000 offered evaluations/second, with completed, durably persisted decisions
reported separately from admitted requests. Proposed engineering defaults remain:
100,000 initially retained decisions, 10 tenants with 60% of traffic in the
busiest tenant, 10 rules and 2 aggregates per evaluation, 20 concurrent history
requests/second, five minutes sustained, p95 <=250ms, p99 <=750ms and zero
unexpected errors. Only the evaluation throughput was explicitly selected by
the user; the other figures are engineering defaults. After these defaults were
stated and the assistant confirmed no further clarification was necessary, the
user instructed "SO FINISH WHAT WE HAVE TO FINISH". They are now the working
acceptance baseline under that delegated engineering direction, not a claim
that the user individually chose each number. D14.12/D15.17 record this baseline;
passing it still requires measurements and the dependent verification tasks.

At target rate the five-minute evaluation run offers 300,000 evaluations. Record
the resulting decision, evidence, effect and job counts, including growth from
the initial retained population. Measure enqueue throughput and provider drain
throughput separately, including oldest pending job age and backlog growth.
An increasing backlog cannot establish sustained end-to-end capacity.

Ramp through 100, 250, 500 and 1,000 evaluations/second. Keep concurrency bounded
by the shared database budget; do not raise every service pool independently.
At each stage retain latency including queue delay, all dropped/error requests,
CPU/I/O, pool wait time, lock waits, query plans and owner queue metrics. Preserve
tenant authorization, evidence fidelity and atomic decision/effect/job writes.
Measure before choosing batching, caching or index changes. No performance
optimization or 1,000/sec capacity is claimed by this document.

## Existing PostgreSQL contracts inspected

- `backend/decision-engine-service/internal/clients/dispatch/http_client.go`:
  `SendScoringRequest` POSTs the existing request JSON to `SCORING_PROVIDER_URL`.
  `PublishOutboxEvent` POSTs event ID, tenant, aggregate identity, event type,
  payload, status and event creation time to `OUTBOX_PUBLISHER_URL`.
  Both use the existing configured authentication and accept HTTP 2xx. Neither
  implements provider receipt lookup or atomic remote deduplication.
- `backend/decision-engine-service/internal/app/outbox.go`: blank publisher URL
  disables publication. Preserve this optional behavior; absence is not a
  successful delivery and must not create a substitute receiver.
- `backend/decision-engine-service/internal/service/dispatch_service.go`:
  dispatch and local status updates are separate operations. A local status or
  HTTP acknowledgement is not proof of recovery after an uncertain remote send.
- `backend/decision-engine-service/.env.example`: both provider URLs are blank.
  This is evidence of configuration requirements, not evidence about a deployed
  provider. No private environment values were inspected or copied.
- `backend/decision-engine-service/internal/ports/dispatch_receipt.go` requires
  atomic full-attempt key/body binding and reconciliation. Its explicit contract
  rejects adapting a legacy HTTP 2xx sender into a durable receipt provider.

## Implementation consequences

Keep the existing payloads, provider selection, optional outbox behavior and
scoring result lifecycle. Preserve the asynchronous separation from decision
evaluation. Do not change the decision score merely because asynchronous scoring
has been accepted by a provider.

D19.19/D19.20 still require evidence that the selected existing receivers can
deduplicate and reconcile uncertain requests. Their destination-specific receipt
capabilities are not supplied by the inspected client code. An adapter must not
manufacture an accepted receipt or return definitive absence after a timeout.
Provider choice is now settled; receiver capability and migration implementation
remain open. Cycle 12 subsequently completed the target records, scoped history
acceptance and evaluation fixture: **132/152**, with **20 remaining**. Its actual
evaluation ramp still fails capacity acceptance; see [cycle 12](2026-11-03_decision_cycle12.md).
