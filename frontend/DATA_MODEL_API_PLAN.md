# Data Model API Plan

## Purpose

This document maps the current frontend surfaces to the `data-model-service` OpenAPI contract in:

- [new/backend/data-model-service/docs/openapi.yaml](C:\Users\Kwasi%20Addo\Dev\Work\IT%20Consortium\Marble\marble\new\backend\data-model-service\docs\openapi.yaml)

The frontend state approach is:

- TanStack Query for server state
- Zustand for client/workspace state

## Service Domains

The API naturally groups into these frontend domains:

1. Tenant lifecycle
2. Data model authoring
3. Navigation and table presentation
4. Schema audit/history
5. Async index jobs
6. Admin reconcile

## Endpoint Summary

### Tenant lifecycle

- `POST /v1/tenants`
- `GET /v1/tenants`
- `GET /v1/tenants/{tenantId}`
- `POST /v1/tenants/{tenantId}/provision`

Frontend use:

- tenant picker
- tenant bootstrap flow
- provision status banner

### Data model authoring

- `GET /v1/tenants/{tenantId}/data-model`
- `GET /v1/tenants/{tenantId}/tables`
- `POST /v1/tenants/{tenantId}/tables`
- `PATCH /v1/tables/{tableId}`
- `DELETE /v1/tables/{tableId}`
- `GET /v1/tables/{tableId}/fields`
- `POST /v1/tables/{tableId}/fields`
- `PATCH /v1/fields/{fieldId}`
- `DELETE /v1/fields/{fieldId}`
- `GET /v1/fields/{fieldId}/enum-values`
- `POST /v1/fields/{fieldId}/enum-values`
- `PATCH /v1/enum-values/{enumValueId}`
- `DELETE /v1/enum-values/{enumValueId}`
- `GET /v1/tenants/{tenantId}/links`
- `POST /v1/tenants/{tenantId}/links`
- `DELETE /v1/links/{linkId}`
- `GET /v1/tenants/{tenantId}/pivots`
- `POST /v1/tenants/{tenantId}/pivots`
- `DELETE /v1/pivots/{pivotId}`

Frontend use:

- `Your Data Model`
- future schema builder
- table detail drawers
- create/edit field modals
- enum value editors
- link/pivot modals

### Navigation and table presentation

- `GET /v1/tables/{tableId}/options`
- `PUT /v1/tables/{tableId}/options`
- `GET /v1/tables/{tableId}/navigation-options`
- `POST /v1/tables/{tableId}/navigation-options`
- `DELETE /v1/navigation-options/{navigationOptionId}`

Frontend use:

- field visibility/order panels
- navigation option modal
- list/view layout configuration

### Schema audit/history

- `GET /v1/tenants/{tenantId}/schema-change-log`
- `GET /v1/tenants/{tenantId}/schema-migrations`

Frontend use:

- schema history tab
- operations timeline
- migration status views

### Async index jobs

- `GET /v1/tenants/{tenantId}/index-jobs`
- `POST /v1/tenants/{tenantId}/index-jobs`
- `GET /v1/index-jobs/{jobId}`
- `POST /v1/index-jobs/{jobId}/retry`

Frontend use:

- indexing status panel
- failed job retry actions
- operational health widgets

### Admin reconcile

- `GET /v1/admin/reconcile`

Frontend use:

- admin-only support page
- drift diagnosis tools

## Page Mapping

### `/your-data`

Current scope:

- `Data model` tab
  - assembled model summary
  - table list
  - create table entry point
- `Data Model Schema` tab
  - schema change log
  - migration history
- `Ingested data viewer` tab
  - index job visibility
  - ingestion contract visibility

Important note:

- The current OpenAPI spec does not expose a true row-level ingested-record browser.
- `Ingested data viewer` in the frontend can currently show ingestion contract and index/indexing status, but not actual ingested tenant records yet.

### `/detection`

Not directly backed by the data-model-service right now.
Keep separate from data-model API integration.

### `/cases`

Potential future use:

- table navigation options
- entity/table configuration that shapes case views

### `/investigator`

Potential future use:

- links and pivots to define relationship traversal

### `/settings`

Potential future use:

- tenant provisioning status
- admin/reconcile and migration diagnostics

## Query Strategy

### Query ownership

- Query: remote entities, lists, status, history
- Zustand: selected tenant, active builder tab, selected table, local modal state

### Initial query set

Phase 1:

- `getTenant`
- `getAssembledDataModel`
- `listTables`
- `listSchemaChanges`
- `listIndexJobs`

Phase 2:

- `listTenantSchemaMigrations`
- `listFields`
- `listLinks`
- `listPivots`
- `getTableOptions`
- `listNavigationOptions`

Phase 3:

- create/update/delete mutations for tables, fields, links, pivots, options, jobs

## Zustand Scope

Recommended store slices:

- selected tenant id
- active `Your Data Model` view
- selected table id
- open modal ids
- optimistic local builder selections

## UI Work Todo

### Foundation

- [x] Install TanStack Query
- [x] Install Zustand
- [x] Add shared query provider
- [x] Add typed data-model client
- [x] Add workspace store for tenant/view state

### Your Data Model

- [x] Connect top tabs to shared state
- [x] Add backend-backed overview state
- [ ] Add tenant selector or tenant bootstrap control
- [ ] Add create table modal wired to `POST /v1/tenants/{tenantId}/tables`
- [ ] Add table list/details panel
- [ ] Add create field modal wired to `POST /v1/tables/{tableId}/fields`
- [ ] Add enum value editor flow
- [ ] Add link creation modal
- [ ] Add pivot creation modal

### Schema Views

- [ ] Add schema change timeline UI
- [ ] Add migration history panel
- [ ] Add table options editor
- [ ] Add navigation option editor

### Operations

- [ ] Add index job status list
- [ ] Add retry action for failed jobs
- [ ] Add reconcile/admin page if needed

## Implementation Order

1. Shared provider and client
2. Tenant + assembled model queries
3. `Your Data Model` live overview
4. Table creation/editing
5. Field and enum management
6. Links, pivots, and navigation options
7. Schema history and index job tooling
