# Transaction Replay TODO

The final June extract is now wired into the harness. Completed source-format decisions are recorded here alongside the remaining environment and acceptance work.

- [x] Inventory the final transaction date range from parsed event timestamps rather than directory names.
- [x] Add all six final inflow and outflow streams to one globally merged replay timeline.
- [x] Map the mobile/wallet sources to `channel=wallet`, inflow/outflow to incoming/outgoing, and retain processor metadata from each row.
- [x] Validate that all 180 final transaction files use the shared `production_transaction_csv_v1` format.
- [x] Parse `%Y-%m-%d %H:%M:%S` source timestamps in `Africa/Accra` for all configured streams.
- [x] Version repeated source transaction identifiers by deterministic stream/file/row identity.
- [x] Confirm compressed-input support is not required for the delivered final extract.
- [x] Add drained checkpoints, source fingerprint validation, and resume-after-cursor behavior.
- [ ] Define cleanup and retention for the isolated tenant, reference data, decisions, ingestion audits, and outbox rows.
- [ ] Define CPU, memory, database, queue, and connection-pool telemetry collected from each service during a run.
- [ ] Define SLOs and pass/fail thresholds for ingestion latency, decision latency, error rate, scheduling lag, and sustained throughput.
- [ ] Decide whether the completed system test should use direct decision callbacks or the future ingestion-outbox consumer.
- [ ] Decide when screening and case-manager execution should be included after the base ingest-and-decide workload is characterized.
- [ ] Add environment-specific auth and secret injection through the deployment system; do not place tokens in manifests.
