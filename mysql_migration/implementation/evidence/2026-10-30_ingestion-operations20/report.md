# Isolated ingestion comparison and reconciliation

Two final process runs passed 32/32 each. No required tests failed or skipped.

These are 32-request closed-loop samples with race instrumentation and no dedicated warm-up. First requests are included. Host, generator and server-cache effects are not controlled; these values are not capacity or SLO evidence. Hot keys replay one receipt, while distributed keys create 32, so work per request differs.

| Workload | Worker | Requests/s, two runs | p95 ms, two runs | Peak account connections |
|---|---|---|---|---|
| hot_true_c1 | False | 45.6, 47.3 | 34.3, 26.0 | 3 |
| hot_true_c4 | False | 128.3, 123.6 | 84.2, 71.0 | 5 |
| hot_false_c1 | False | 29.4, 30.9 | 43.6, 44.8 | 5 |
| hot_false_c4 | False | 59.2, 94.7 | 72.6, 46.7 | 5 |
| hot_true_c1 | True | 24.7, 55.6 | 53.1, 22.7 | 9 |
| hot_true_c4 | True | 50.7, 131.6 | 259.5, 61.4 | 11 |
| hot_false_c1 | True | 10.5, 22.7 | 132.6, 67.5 | 11 |
| hot_false_c4 | True | 27.4, 60.8 | 332.4, 69.4 | 11 |

Reconciliation retained 66 scoped fixture records across both runs: 104 terminal work-resource observations and 4 explicitly pending observations. Pending resources belong only to the API-only enqueue and intentionally unselected queue fixtures. Foreign-tenant control resources are separately asserted unchanged by the isolation test. No unexpected pending work or terminal active reservations were accepted.

Measurement extraction independently verified that retained record, audit and outbox totals equal the sum of public and private execution receipts in every workload snapshot. Full raw latencies, sampled lock-wait observations, configured pools and durable totals are in measurements.json; raw test output and installation artifacts are in accepted-1/ and accepted-2/.

The combined application pool budget was 16 API connections plus 16 worker connections; the sampled account also includes one fixture connection. Sampling can miss short peaks. No deployment-wide budget, CPU/IO saturation, causal bottleneck or performance improvement is claimed.

Rejected runs are retained in rejected/. The successful pre-atomic-readiness run is superseded, not counted as final repeated acceptance. The DTO package has no standalone test files; its transports are exercised by handler/process tests, and this package-level no-tests event is recorded separately from skipped tests.

Extraction joins consecutive Go test Output events before decoding measurement/reconciliation lines, because test2json splits long lines across events.
