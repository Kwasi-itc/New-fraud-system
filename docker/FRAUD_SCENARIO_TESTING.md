# EC2 fraud scenario behavior test

`test-fraud-scenarios-ec2.mjs` verifies that every rule in the five provisioned fraud scenarios both fires and does not fire against targeted synthetic examples. It ingests complete fixture rows for the tenant model, calls the scenario evaluation endpoint, and asserts the exact rule execution outcome.

## Important: persistent test data

Run this only against a **disposable test tenant**. Ingestion records, audit/outbox entries, decisions, rule executions, and async execution records are persisted. The script does not delete them. Every fixture uses a unique run prefix, and `--run-id` can be used to identify a specific run, but uniqueness does not replace tenant isolation.

The script requires an existing tenant whose data model exactly matches `fraud-data-model.json` and all five matching scenarios are published. It verifies those prerequisites before writing any fixtures. If provisioning used non-default late-callback or D7 settings, pass the same values to this script.

Transaction fixtures set the nullable `user_median_amount_90d` field to `null` by default, exercising the null-dependent rule skip behavior. The dedicated baseline-deviation fire/no-fire cases set the field to `10` so that rule itself is still tested with a usable baseline.

## Dry run

From the repository root, using Node 22:

```sh
node docker/test-fraud-scenarios-ec2.mjs --dry-run
```

The dry run generates the fixtures and reports planned record/evaluation counts without contacting any service.

## Run against the EC2 gateway

On the EC2 host, the stack's shared service token is usually in the Compose environment file. Set it in the shell without placing the value in command history:

```sh
read -rsp 'Service token: ' SERVICE_AUTH_TOKEN; export SERVICE_AUTH_TOKEN; echo
node docker/test-fraud-scenarios-ec2.mjs \
  --host http://127.0.0.1:8088 \
  --tenant-id "<disposable-test-tenant-uuid>" \
  --confirm-write
unset SERVICE_AUTH_TOKEN
```

If running from a machine that does not have Node 22 installed, use the Node container. On Linux/EC2, `--network host` lets it reach a gateway bound to the host's loopback interface:

```sh
docker run --rm --network host \
  -v "$PWD:/workspace" -w /workspace \
  -e SERVICE_AUTH_TOKEN \
  node:22-alpine \
  node docker/test-fraud-scenarios-ec2.mjs \
    --host http://127.0.0.1:8088 \
    --tenant-id "<disposable-test-tenant-uuid>" \
    --confirm-write
```

For separate service tokens, set `INGESTION_SERVICE_TOKEN`, `DECISION_ENGINE_SERVICE_TOKEN`, and `DATA_MODEL_SERVICE_TOKEN`; each overrides `SERVICE_AUTH_TOKEN` for that service.

The script covers each provisioned stable rule ID with one positive and one negative case, ingests fixture batches, then evaluates each target through the live scenario API. When the API defers an evaluation, it polls the corresponding async execution and checks its completed result. A snoozed rule cannot be asserted and is reported as a failure. The run exits non-zero if any expected hit/no-hit assertion fails.

For a deployment configured with non-default rule parameters, include matching options, for example:

```sh
  --late-callback-seconds 450 \
  --structuring-limit 1200 \
  --structuring-band-percent 8
```
