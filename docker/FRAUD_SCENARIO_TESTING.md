# EC2 fraud scenario behavior test

`test-fraud-scenarios-ec2.mjs` runs a continuous synthetic transaction simulation by default (a target of 200,000 transactions over up to 24 hours). It requests an evaluation against all live transaction scenarios for every successfully ingested transaction, with at most five evaluations in flight. Use `--deterministic` to run targeted fire/no-fire examples for each provisioned rule and assert their outcomes.

## Important: persistent test data

Run this only against a **disposable test tenant**. Ingestion records, audit/outbox entries, decisions, rule executions, and async execution records are persisted. The script does not delete them. Every fixture uses a unique run prefix, and `--run-id` can be used to identify a specific run, but uniqueness does not replace tenant isolation.

Ingestion is split into batches of at most 500 records. If a later batch fails, earlier batches remain persisted; rejected batches can also create validation audit entries.

The continuous simulation stops if ingestion or evaluation fails. Decision requests are not idempotent: a failed or timed-out request may already have produced decisions. Inspect the reported transaction IDs and checkpoint before retrying. `--resume` is disabled because it cannot safely reconcile interrupted requests. Checkpoints record progress for inspection, not automatic recovery.

The script exits non-zero if the time limit is reached before the transaction target. A graceful interrupt finishes the current batch before stopping.

The script requires an existing tenant whose data model exactly matches `fraud-data-model.json` and all five matching scenarios are published. It verifies those prerequisites before writing any fixtures. If provisioning used non-default late-callback or D7 settings, pass the same values to this script.

In deterministic mode, transaction fixtures set the nullable `user_median_amount_90d` field to `null` by default. The baseline-deviation cases set it to `10`.

## Dry run

From the repository root, using Node 22:

```sh
node docker/test-fraud-scenarios-ec2.mjs --dry-run
```

The dry run generates a local sample without contacting any service. Add `--deterministic` for the targeted fixture plan.

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

With `--deterministic`, the script covers each provisioned stable rule ID with one positive and one negative case. When an evaluation is deferred, either mode polls a client-only endpoint that returns status and result only for executions owned by that client. A snoozed rule is reported as a failure in deterministic mode. Continuous mode evaluates every transaction but does not perform per-rule assertions; it may not reach its transaction target if processing cannot keep up.

For a deployment configured with non-default rule parameters, include matching options, for example:

```sh
  --late-callback-seconds 450 \
  --structuring-limit 1200 \
  --structuring-band-percent 8
```
