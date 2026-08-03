# Setup And Run Guide

This guide is a placeholder for the standalone ingestion service in `new/backend/ingestion-service`.

The service has been planned and scaffolded at the documentation level, but runtime setup instructions should be finalized once the first executable server and worker are added.

## Planned local components

- ingestion API process
- ingestion worker process
- PostgreSQL for service metadata
- tenant data database access
- reachable `data-model-service` instance

## Planned local workflow

1. start PostgreSQL for ingestion metadata
2. run ingestion metadata migrations
3. start `data-model-service`
4. start `ingestion-service`
5. start `ingestion-service` worker if testing CSV jobs
6. call ingest endpoints against a provisioned tenant

## Planned docs endpoints

- `http://127.0.0.1:8081/docs`
- `http://127.0.0.1:8081/redoc`
- `http://127.0.0.1:8081/openapi.yaml`

## Planned minimum environment variables

- `DATABASE_URL`
- `DATABASE_MAX_CONNS`
- `DATABASE_MIN_CONNS`
- `READ_DATABASE_URL`
- `READ_DATABASE_MAX_CONNS`
- `READ_DATABASE_MIN_CONNS`
- `WORKER_DATABASE_URL`
- `WORKER_DATABASE_MAX_CONNS`
- `WORKER_DATABASE_MIN_CONNS`
- `DATA_MODEL_SERVICE_URL`
- `SERVICE_AUTH_MODE`
- `SERVICE_AUTH_TOKEN`
- `PORT`
- `LOG_LEVEL`

This file should be expanded as soon as Phase 0 and Phase 1 of the blueprint are implemented.

## Isolation guidance

To keep async CSV processing from starving live decision-time aggregate queries or synchronous ingest writes:

1. run the HTTP API with `DATABASE_URL` for writes
2. set `READ_DATABASE_URL` to create a dedicated read/aggregate pool
3. cap the CSV worker pool with `WORKER_DATABASE_MAX_CONNS`
4. optionally point `WORKER_DATABASE_URL` at a separate primary or connection class

If you need isolation but only have one PostgreSQL primary, you can still set `READ_DATABASE_URL` equal to `DATABASE_URL`. The service will create a separate read pool when `READ_DATABASE_URL` is present, which lets you cap read connections independently from write connections.
