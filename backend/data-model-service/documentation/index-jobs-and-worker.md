# Index Jobs And Worker

This document explains the standalone service's async index-job system:

- what an index job is
- which operations create jobs automatically
- which job types the API accepts
- how the background worker picks jobs up
- what the status transitions mean

## Purpose

The service does not apply every secondary index synchronously during API writes.

Instead, some index work is stored as a job in `core.index_jobs` and later applied by the background worker in `cmd/worker`.

This keeps model mutations fast and makes index creation:

- auditable
- retryable
- operationally separate from the request path

## Endpoints

The index-job API surface is:

- `POST /v1/tenants/:tenantId/index-jobs`
- `GET /v1/tenants/:tenantId/index-jobs`
- `GET /v1/index-jobs/:jobId`
- `POST /v1/index-jobs/:jobId/retry`

## Job Types

The service currently accepts exactly these index-job types:

- `navigation`
- `search`
- `repair`

These are validated in the domain layer before a job is stored.

### `navigation`

Used for managed indexes that support navigation options.

Typical shape:

- target table columns used for filtering and ordering

Example:

- target table: `transactions`
- columns: `merchantid`, `updated_at`

This is the kind of job automatically created when a navigation option is created.

### `search`

Used for search-oriented managed indexes.

The API allows this type, and the worker treats it as a managed index job the same way it treats other index types.

### `repair`

Used when the system needs to recreate or re-apply an expected managed index.

This is useful for operational correction flows where metadata says an index should exist but the physical schema needs to be brought back into sync.

## Which Operations Create Jobs Automatically

Today, the main automatic job-creation path is:

- creating a navigation option

When a navigation option is created, the service automatically enqueues a `navigation` index job for the target table.

Why:

- a navigation option defines how a client will query related records
- that query pattern normally filters on one target field and orders by another
- the service therefore requests a managed secondary index for those target columns

Example:

- source table: `merchants`
- source field: `object_id`
- target table: `transactions`
- filter field: `merchantid`
- ordering field: `updated_at`

This creates a job like:

- `index_type = navigation`
- `columns = ["merchantid", "updated_at"]`

## What A Stored Job Looks Like

An index job stores metadata such as:

- `id`
- `tenant_id`
- `table_id`
- `table_name`
- `index_type`
- `columns`
- `status`
- `requested_by_operation`
- `attempt_count`
- `requested_at`
- `started_at`
- `completed_at`
- `scheduled_at`
- `dedupe_key`

## Worker Model

The worker is a separate process:

- `go run ./cmd/worker`

It is not part of the HTTP server process.

If the worker is not running, pending jobs stay pending.

## Polling Behavior

The worker is poll-based, not cron-based.

Current config keys:

- `INDEX_WORKER_POLL_INTERVAL`
- `INDEX_WORKER_MAX_ATTEMPTS`
- `INDEX_WORKER_RETRY_BASE_DELAY`
- `INDEX_WORKER_RETRY_MAX_DELAY`

Default values from config:

- poll interval: `2s`
- max attempts: `5`
- retry base delay: `5s`
- retry max delay: `2m`

That means:

- the worker starts
- it immediately tries to claim the next due pending job
- if no job is available, it waits until the next poll tick

## How A Job Gets Claimed

The worker claims the next job where:

- `status = pending`
- `scheduled_at` is null or due
- `attempt_count < max_attempts`

When claimed, the repository updates the job to:

- `status = running`
- `attempt_count = attempt_count + 1`
- `started_at = now`

## Status Lifecycle

The normal lifecycle is:

- `pending`
- `running`
- `applied`

Failure lifecycle:

- `pending`
- `running`
- `pending` again if rescheduled after failure
- `failed` once retries are exhausted or the worker cannot continue

### `pending`

The job has been enqueued and is waiting for the worker.

Common reasons a job remains pending:

- the worker process is not running
- the job has a future `scheduled_at`
- the worker cannot connect to the same database as the API

### `running`

The worker has claimed the job and is currently applying the managed index.

Typical job shape while running:

```json
{
  "index_job": {
    "id": "855b6e0c-84a5-4e68-b695-d96cf3bfb764",
    "tenant_id": "e2ce3b58-55f2-47ad-9105-17c6938a780d",
    "table_id": "483dee77-e06d-480e-8505-7fa82844ed0f",
    "table_name": "transactions",
    "index_type": "navigation",
    "columns": ["merchantid", "updated_at"],
    "status": "running",
    "requested_by_operation": "create_navigation_option",
    "attempt_count": 1,
    "requested_at": "2026-05-19T14:25:58.384377Z",
    "started_at": "2026-05-19T14:26:00.100000Z",
    "completed_at": null,
    "scheduled_at": null,
    "dedupe_key": "f38738086f97d61d4bf9ae9bcf93dc11fb6136f2"
  }
}
```

### `applied`

The worker successfully applied the managed index.

Typical job shape after success:

```json
{
  "index_job": {
    "id": "855b6e0c-84a5-4e68-b695-d96cf3bfb764",
    "tenant_id": "e2ce3b58-55f2-47ad-9105-17c6938a780d",
    "table_id": "483dee77-e06d-480e-8505-7fa82844ed0f",
    "table_name": "transactions",
    "index_type": "navigation",
    "columns": ["merchantid", "updated_at"],
    "status": "applied",
    "requested_by_operation": "create_navigation_option",
    "attempt_count": 1,
    "requested_at": "2026-05-19T14:25:58.384377Z",
    "started_at": "2026-05-19T14:26:00.100000Z",
    "completed_at": "2026-05-19T14:26:00.450000Z",
    "scheduled_at": null,
    "dedupe_key": "f38738086f97d61d4bf9ae9bcf93dc11fb6136f2"
  }
}
```

### `failed`

The worker gave up after retries or hit a terminal error.

In that case the job should carry:

- `status = failed`
- `error_message`
- `completed_at`

It can then be retried through:

- `POST /v1/index-jobs/:jobId/retry`

## How To Verify The Worker Is Running

The worker currently logs these important events:

- startup
- successful index application
- index-job failure
- worker exit with error

Expected startup log shape:

```json
{
  "msg": "starting index job worker",
  "poll_interval": "2s",
  "max_attempts": 5,
  "retry_base_delay": "5s",
  "retry_max_delay": "2m"
}
```

Expected success log shape:

```json
{
  "msg": "index job applied",
  "job_id": "855b6e0c-84a5-4e68-b695-d96cf3bfb764",
  "tenant_id": "e2ce3b58-55f2-47ad-9105-17c6938a780d",
  "table_name": "transactions",
  "index_type": "navigation",
  "columns": ["merchantid", "updated_at"]
}
```

Expected failure log shape:

```json
{
  "msg": "index job failed",
  "job_id": "855b6e0c-84a5-4e68-b695-d96cf3bfb764",
  "error": "..."
}
```

Important current limitation:

- the worker does not currently emit a dedicated "claimed job" log entry

So logs alone can prove:

- the worker started
- a job applied
- a job failed

But logs do not yet explicitly show every polling tick or every claim attempt.

## If A Job Stays Pending

If a job remains pending well past the poll interval, check these first:

1. the worker process is actually running
2. the worker and the API are pointing at the same `DATABASE_URL`
3. the worker started without config or DB connection errors
4. the job does not have a future `scheduled_at`

If all of those are true and the job is still pending, that is no longer an expected operational state and should be treated as a bug or an environment mismatch.

