# EC2 fraud scenario behavior test

`test-fraud-scenarios-ec2.mjs` runs a continuous synthetic transaction simulation by default (a target of 200,000 transactions over up to 24 hours). It requests an evaluation against all live transaction scenarios for every successfully ingested transaction, with at most five evaluations in flight. Use `--deterministic` for an interactive case builder, or `--deterministic --no-ui` for the original fire/no-fire assertion suite.

## Two simulation modes

All existing command-line options remain available. The default mode is **continuous simulation**. Add **`--deterministic`** for the interactive deterministic case builder. Add **`--no-ui`** for console-only execution; `--deterministic --no-ui` retains the original complete fire/no-fire assertion suite.

Both modes generate synthetic Ghanaian customer names, correctly formatted phone-style contacts, masked usernames and wallet numbers, SHA-256 identity/instrument hashes, wallet holder and name-enquiry details, and readable payment/provider references. Email-style identities use `example.test`. Contacts are reproducible per customer ID and shared instruments retain the same masked wallet details. The dashboard shows customer names in traffic and dataset previews, and its search accepts names or transaction IDs. Rule-selected name mismatches and unknown name enquiries are reflected in the generated wallet fields. These are fictional fixtures, not imported customer data.

## Browser dashboard and direct access

The script serves **Flight Control** on port **8099**, binding to **0.0.0.0** by default. Open the complete dashboard link printed by the script. For a remote instance, use its directly reachable address; no SSH tunnel is used. To print the correct browser-facing link, pass:

```sh
--ui-public-url http://your-ec2-address:8099
```

`--ui-port` changes the listening port. `--ui-host` changes the bind address if needed. The printed link contains a run-specific access key in its URL fragment; the browser exchanges it for a dashboard session. API client credentials and private keys remain in the script process. The access key also protects deterministic submission controls.

After a run finishes or fails, the dashboard remains available until **Ctrl+C**. Continuous mode retains finish-current-batch shutdown. In the deterministic builder, shutdown cancels a pending delay, finishes any current record request, and stops before another record or evaluation is submitted. Accepted records remain saved. Use `--no-ui` for jobs that should exit on completion. An occupied dashboard port fails startup before any fixture writes.

### Continuous simulation

The pipeline appears at the top. **On the way / queued** and **Received traffic** sit side by side beneath it. Hover or keyboard-focus a queued item to inspect its generated request. Hover received traffic to inspect ingestion and evaluation requests and responses. Click to pin the detail panel; Escape closes it. Transaction timestamps are finalized on dispatch.

The view includes live ingestion/evaluation/async states, search and attention/problem filters, rule hits, outcomes, scores, timing, and a freeze-view control. Freezing the view does not stop submission. The attention total counts transactions whose most severe decision is review, block-and-review or decline; scenario triggers and rule hits are separate counts. Timeouts can have an unknown outcome and are not counted as successful evaluations.

### Deterministic simulation

Run the script with the same connection/authentication options and **`--deterministic`**:

```sh
node docker/test-fraud-scenarios-ec2.mjs \
  --host http://your-ec2-address:8088 \
  --tenant-id "<test-tenant-uuid>" --client-id "<client-uuid>" \
  --confirm-write --deterministic \
  --ui-public-url http://your-ec2-address:8099
```

In the browser:

1. Select one of the five provisioned scenarios.
2. Set each rule to **Match**, **Don’t match**, or **Any**. Match means a rule should fire; Any leaves its outcome unconstrained.
3. Select **Generate dataset**. Generation makes no service writes. Review the customer/wallet records, historical transactions for aggregations, final target request, and predicted score/outcome. Expand each record to see its complete JSON.
4. Set the wait before the first record (default 3 seconds) and the wait between records (default 1 second). Both accept 0–120 seconds.
5. Select **Submit generated dataset**. Supporting records and history are submitted individually in order; the target is ingested and evaluated last. Progress and countdowns update live.
6. Inspect the returned decision and the requested-versus-actual outcome for every rule.

The builder creates one coherent dataset for the selected expectations. It checks its recipe against the scenario formulas locally and rejects combinations it cannot produce, such as day-zero matching while new-account does not match. The local prediction is not an engine response. Before live submission, model/scenario checks run again and the selected live trigger, rule formulas, modifiers and thresholds must match the data recipes. Snoozes, dependencies and live engine behavior can still produce an expectation mismatch; this is displayed as a mismatch.

A submitted plan cannot be submitted twice. Generate a fresh dataset for another case. Plans use fresh customer, wallet, destination and transaction identities to isolate aggregation history. If a record fails, later records and evaluation stop; accepted progress remains visible. No automatic rollback or retry of a complete case is claimed. Existing ingestion retry behavior is retained.

### Offline previews and retained data

Preview either interface without credentials or service writes:

```sh
# Continuous traffic preview
node docker/test-fraud-scenarios-ec2.mjs --demo

# Interactive deterministic data builder with simulated submission responses
node docker/test-fraud-scenarios-ec2.mjs --demo --deterministic
```

Both previews are labelled **OFFLINE PREVIEW**. Ordinary `--dry-run` retains the console-only fixture preview and starts no server.

The monitor retains 60 responses and 30 log entries. Hover payloads use a separate cache capped at 8 MiB and 1,100 request IDs, with individual sections capped at 64 KiB; truncation or expired details are reported. Streaming snapshots do not copy full request payloads. Response summaries show at most 12 scenarios and 40 rule results, while counters use the full response. The deterministic builder retains its current generated plan. Refreshing reconnects to that plan and its progress. No run history is persisted by the dashboard.

## Important: persistent test data

Run this only against a **disposable test tenant**. Ingestion records, audit/outbox entries, decisions, rule executions, and async execution records are persisted. The script does not delete them. Every fixture uses a unique run prefix, and `--run-id` can be used to identify a specific run, but uniqueness does not replace tenant isolation.

Ingestion is split into batches of at most 500 records. If a later batch fails, earlier batches remain persisted; rejected batches can also create validation audit entries.

The continuous simulation stops if ingestion or evaluation fails. Decision requests are not idempotent: a failed or timed-out request may already have produced decisions. Inspect the reported transaction IDs and checkpoint before retrying. `--resume` is disabled because it cannot safely reconcile interrupted requests. Checkpoints record progress for inspection, not automatic recovery.

The script exits non-zero if the time limit is reached before the transaction target. A graceful interrupt finishes the current batch before stopping.

The script requires an existing tenant whose data model exactly matches `fraud-data-model.json` and all five matching scenarios are published. It verifies those prerequisites before writing any fixtures. If provisioning used non-default late-callback or D7 settings, pass the same values to this script.

To proceed despite a model mismatch, add `--allow-model-mismatch` alongside `--confirm-write`. The script still loads the local model, fetches the tenant model and checks the published scenarios; it warns about the mismatch instead of stopping. Fixture writes can fail or have unintended meaning, so use this only after reviewing the differences and only with a disposable test tenant.

In the console-only deterministic suite, transaction fixtures set the nullable `user_median_amount_90d` field to `null` by default. The baseline-deviation cases set it to `10`.

## Dry run

From the repository root, using Node 22:

```sh
node docker/test-fraud-scenarios-ec2.mjs --dry-run
```

The dry run generates a local sample without contacting any service. Add `--deterministic` for the targeted fixture plan.

Local contract and dashboard-server checks use mocked service responses:

```sh
node --test docker/test-fraud-scenarios-ec2.test.mjs docker/fraud-simulation-monitor.test.mjs docker/fraud-deterministic-lab.test.mjs
```

## Client setup and run

Generate a client key pair with the same helper used by the provisioner:

```sh
node backend/auth-service/scripts/client-proof.mjs keygen \
  --dir .local/client-auth \
  --kid fraud-scenario-test
```

Register `public.pem` and its key ID on an API client in the disposable tenant, and grant these client scopes: `models:read`, `scenarios:read`, `ingest:write`, and `decisions:evaluate`. An existing provisioner client can be used if it also has the two testing scopes. Do not grant publication access. Keep `private.pem` private.

After deploying the updated Decision Engine and applying its migrations, run:

```sh
node docker/test-fraud-scenarios-ec2.mjs \
  --host http://127.0.0.1:8088 \
  --tenant-id "<disposable-test-tenant-uuid>" \
  --client-id "<registered-client-uuid>" \
  --key-dir .local/client-auth \
  --confirm-write
```

The token URL defaults to `<host>/oauth/token`; override it with `--token-url` when needed. The script signs and exchanges fresh client proofs and refreshes access tokens in memory before expiry. Use HTTPS outside trusted local development.

If running from a machine that does not have Node 22 installed, use the Node container. On Linux/EC2, `--network host` lets it reach a gateway bound to the host's loopback interface:

```sh
docker run --rm --network host \
  -v "$PWD:/workspace" -w /workspace \
  node:22-alpine \
  node docker/test-fraud-scenarios-ec2.mjs \
    --host http://127.0.0.1:8088 \
    --tenant-id "<disposable-test-tenant-uuid>" \
    --client-id "<registered-client-uuid>" \
    --key-dir .local/client-auth \
    --confirm-write
```

With `--deterministic --no-ui`, the script covers each provisioned stable rule ID with one positive and one negative case. When an evaluation is deferred, either mode polls a client-only endpoint that returns status and result only for executions owned by that client. A snoozed rule is reported as a failure in deterministic mode. Continuous mode evaluates every transaction but does not perform per-rule assertions; it may not reach its transaction target if processing cannot keep up.

For a deployment configured with non-default rule parameters, include matching options, for example:

```sh
  --late-callback-seconds 450 \
  --structuring-limit 1200 \
  --structuring-band-percent 8
```
