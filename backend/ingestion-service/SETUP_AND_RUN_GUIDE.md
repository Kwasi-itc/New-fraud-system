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
- `DATA_MODEL_SERVICE_URL`
- `SERVICE_AUTH_MODE`
- `SERVICE_AUTH_TOKEN`
- `PORT`
- `LOG_LEVEL`

This file should be expanded as soon as Phase 0 and Phase 1 of the blueprint are implemented.
