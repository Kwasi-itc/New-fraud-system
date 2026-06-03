# Screening Service Operations Runbook

This runbook covers the main operational situations still needed for rollout adoption.

## Retry a failed screening

Use when a screening is in `failed` state due to provider timeout, auth failure, or transient downstream error.

Procedure:

1. inspect the screening record and `last_error`
2. confirm provider and auth configuration are healthy
3. retry through `POST /v1/tenants/:tenantId/screenings/:screeningId/retry`
4. confirm the worker picks the screening back up
5. confirm the callback reaches `decision-engine-service` if `DECISION_ENGINE_URL` is configured

Expected result:

- screening returns to an active worker state
- a new provider request is attempted
- final status is reflected back to the decision engine

## Recover from worker backlog

Use when screenings or dataset update jobs are accumulating faster than the worker drains them.

Procedure:

1. check `WORKER_MODE`, `WORKER_POLL_INTERVAL`, and `WORKER_BATCH_LIMIT`
2. inspect logs for repeated provider or downstream failures
3. count `pending` and `failed` screenings
4. count `pending` and `failed` dataset update jobs
5. increase worker replicas or batch size if the bottleneck is throughput rather than upstream errors
6. restart workers only after confirming in-flight failures are not caused by bad configuration

Expected result:

- pending queues begin to drain
- no repeated auth or routing failures

## Provider outage handling

Use when provider requests are timing out, returning `5xx`, or rejecting credentials.

Procedure:

1. confirm which provider key is selected for the affected screenings
2. verify `SCREENING_PROVIDER_URLS` or `SCREENING_PROVIDER_URL`
3. verify provider credentials or network routing outside the service
4. allow failed screenings to accumulate only if retry is expected to succeed after the outage
5. once the provider is healthy, retry failed screenings and failed dataset update jobs

Expected result:

- new screenings stop failing
- old failures can be replayed without manual database edits

## Callback verification

Use after rollout or auth changes.

Procedure:

1. confirm `DECISION_ENGINE_URL` on `screening-service`
2. confirm `SERVICE_AUTH_MODE` and `SERVICE_AUTH_TOKEN` match on both services
3. execute one screening to completion
4. verify `decision-engine-service` receives `POST /internal/screening-status-updates`
5. verify the execution row status advances from `pending` or `sent` to `completed` or `failed`

Expected result:

- callback accepted with `2xx`
- decision-engine screening execution status matches screening-service state

## Production verification checklist

After deployment, verify:

- `/readyz` succeeds for both services
- one live screening dispatch completes
- one match review produces the expected case-side effect
- one file upload produces blob session and download behavior
- one dataset update job completes and re-screens monitored objects if applicable
