# Outbox

This document explains the tenant-scoped outbox inspection endpoint.

## Endpoint Group

- `GET /v1/tenants/:tenantId/outbox-events`

Primary files:

- [internal/httpapi/handlers/outbox.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/outbox.go)
- [internal/service/outbox_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/service/outbox_service.go)

## Parameters

- `tenantId`
  - tenant whose outbox events should be listed

Current query parameter meaning:

- `limit`
  - optional cap on returned outbox rows

## What This Endpoint Is For

This is an inspection/read endpoint for integration events already persisted by the decision engine.

Typical use:

- verify that decisions or workflow side effects emitted outbox rows
- inspect event payloads and statuses

## Endpoint Detail

### `GET /v1/tenants/:tenantId/outbox-events`

What it does:

- lists persisted outbox events for one tenant

How it works:

- reads the decision-engine outbox table directly
- returns event metadata such as aggregate type, aggregate id, event type, payload, and status

How it should be used:

- verify whether downstream integration events were created
- inspect payloads during debugging
- check whether events are still pending, sent, or failed
