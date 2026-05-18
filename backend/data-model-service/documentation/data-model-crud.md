# Data Model CRUD Operations

This document explains the `data-model` endpoint group in the data model service, what each endpoint does today, and what the design is trying to achieve.

## Purpose

The `data-model` area is where the service manages the tenant-owned schema model itself:

- tables
- fields
- links
- pivots
- table options
- assembled data-model read

These routes are defined in [internal/httpapi/router.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/httpapi/router.go) and handled in [internal/httpapi/handlers/datamodel.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/httpapi/handlers/datamodel.go).

The design assumption is that a tenant already exists and has already been provisioned before these endpoints are used.

## Endpoint Group

Current `data-model` routes:

- `GET /v1/tenants/:tenantId/data-model`
- `POST /v1/tenants/:tenantId/tables`
- `PATCH /v1/tables/:tableId`
- `DELETE /v1/tables/:tableId?dry_run=true`
- `POST /v1/tables/:tableId/fields`
- `GET /v1/fields/:fieldId/enum-values`
- `POST /v1/fields/:fieldId/enum-values`
- `PATCH /v1/fields/:fieldId`
- `PATCH /v1/enum-values/:enumValueId`
- `DELETE /v1/fields/:fieldId?dry_run=true`
- `DELETE /v1/enum-values/:enumValueId`
- `POST /v1/tenants/:tenantId/links`
- `DELETE /v1/links/:linkId?dry_run=true`
- `GET /v1/tenants/:tenantId/pivots`
- `POST /v1/tenants/:tenantId/pivots`
- `DELETE /v1/pivots/:pivotId?dry_run=true`
- `GET /v1/tables/:tableId/options`
- `PUT /v1/tables/:tableId/options`

## Design Intent

The intent of this part of the service is to make the tenant data model the source of truth in two places at once:

- logical metadata in `core.*`
- physical PostgreSQL tables and columns inside the tenant schema

For some operations, that means the service does both metadata mutation and physical DDL mutation in one transactional workflow where possible.

That is the key idea of this service:

- the metadata model should not drift away from the physical tenant schema
- a change like "create field" is not only a metadata write, it is also a physical schema change
- every change should be auditable through schema change history and tenant schema migration history

## Assembled Data Model Read

Endpoint:

- `GET /v1/tenants/:tenantId/data-model`

Service: [internal/service/data_model_read_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/data_model_read_service.go)

### Purpose

Returns the assembled read model for one tenant.

### What It Represents

This is the "consumer view" of the data model. Instead of forcing clients to separately fetch tables, fields, links, pivots, and options, the service assembles one tenant-scoped structure that contains:

- tables
- fields
- links-to-single
- pivots
- navigation options
- table options

### Design Intention

This endpoint exists so downstream systems can consume the data model as a coherent graph, not as scattered normalized rows.

## Tables

Primary service: [internal/service/table_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/table_service.go)

Endpoints:

- `POST /v1/tenants/:tenantId/tables`
- `PATCH /v1/tables/:tableId`
- `DELETE /v1/tables/:tableId?dry_run=true`

### What A Table Means

A table is a tenant-owned object collection. It exists:

- as metadata in `core.model_tables`
- as a physical PostgreSQL table inside the tenant schema

### Table Metadata Fields

Tables also carry three important metadata fields:

- `alias`
- `semantic_type`
- `caption_field`

These are part of the logical model definition, even though they do not directly change the physical PostgreSQL table shape.

#### `alias`

`alias` is the human-friendly display label for the table.

Example:

- `name`: `transactions`
- `alias`: `Transactions`

Why both exist:

- `name` is the stable technical identifier
- `alias` is the user-facing label for UI, docs, or consumer displays

Current state in the service:

- stored in metadata
- returned in API responses
- included in the assembled data model
- mainly used as display metadata

#### `semantic_type`

`semantic_type` is meant to describe what kind of real-world entity the table represents.

This is a business-meaning field, not a physical-schema field.

Historical intent from the old `api` model was:

- `unknown`
- `person`
- `company`

In the new service today:

- it is stored as a plain string
- it is not currently validated as a strict enum
- it therefore behaves as free-form metadata unless additional validation is added

Practical recommendation:

- prefer `person`
- prefer `company`
- use blank or `unknown` when the table does not clearly fit a known semantic category

#### `caption_field`

`caption_field` tells the system which field should be used as the main human-readable label for a record in that table.

Example:

- table: `customers`
- fields: `object_id`, `email`, `full_name`
- `caption_field`: `full_name`

That means a downstream system should prefer `full_name` when it needs a readable label for a customer record.

Think of it as:

- title field
- label field
- primary display field

Current state in the service:

- stored in metadata
- updateable
- returned in the assembled data model
- not strongly validated yet against existing table fields

Practical recommendation:

- point it to a real descriptive field such as `full_name`, `email`, `merchant_name`, or `account_name`
- avoid using technical fields like `id` or `updated_at` unless there is no better display candidate

### Important Current Limitation

In the new service, `alias`, `semantic_type`, and `caption_field` are primarily metadata today.

- `alias` is immediately useful as a display label
- `semantic_type` carries intended business meaning but is not strongly enforced yet
- `caption_field` carries intended record-label meaning but is not strongly enforced yet

So these fields should be understood as part of the logical contract of the model, even though some of their intended downstream behavior is still soft rather than strictly validated.

### Create Table

`POST /v1/tenants/:tenantId/tables`

This:

- validates the table name
- verifies the tenant exists and is `active`
- creates the table metadata row
- creates the physical tenant table
- creates default metadata fields:
  - `object_id`
  - `updated_at`
- creates the default unique index on `object_id`
- records schema-change and tenant-schema-migration history

### Why Default Fields Exist

The service treats these as required platform-level fields:

- `object_id` is the tenant object identifier
- `updated_at` is a required timestamp

At the physical table level, the service also creates:

- `id`
- `valid_from`
- `valid_until`

So table creation is not "just create an empty table name". It is the creation of a managed object table with a known baseline shape.

### Update Table

`PATCH /v1/tables/:tableId`

This updates mutable metadata such as:

- `description`
- `alias`
- `semantic_type`
- `caption_field`

The table name itself is not mutable after creation.

### Delete Table

`DELETE /v1/tables/:tableId?dry_run=true`

Current V1 behavior is hard delete.

The service first checks for internal conflicts:

- links that still reference the table
- pivots that still use the table as a base table

If `dry_run=true`, the service returns the delete report without executing the delete.

If deletion proceeds, it:

- drops the physical tenant table
- deletes the metadata row
- records schema-change and migration history

## Fields

Primary service: [internal/service/field_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/field_service.go)

Endpoints:

- `POST /v1/tables/:tableId/fields`
- `PATCH /v1/fields/:fieldId`
- `DELETE /v1/fields/:fieldId?dry_run=true`

### What A Field Means

A field is a logical column belonging to one table. It exists:

- as metadata in `core.model_fields`
- as a physical column in the tenant PostgreSQL table

### Create Field

This:

- validates field rules
- loads the target table
- creates the metadata row
- adds the physical column
- creates a unique index if `is_unique=true`
- records schema-change and migration history

Supported data types currently include:

- `bool`
- `int`
- `float`
- `string`
- `timestamp`
- `ip_address`

### Enum Values

If `is_enum=true`, the field can now have a managed list of enum values.

Endpoints:

- `GET /v1/fields/:fieldId/enum-values`
- `POST /v1/fields/:fieldId/enum-values`
- `PATCH /v1/enum-values/:enumValueId`
- `DELETE /v1/enum-values/:enumValueId`

Enum values are metadata records attached to a field. Each enum value contains:

- `value`
- `label`
- `sort_order`

Current rules:

- enum values can only be attached to fields where `is_enum=true`
- enum values are only supported for `string`, `int`, and `float` fields
- the raw stored value is modeled as text, but it is validated against the owning field's data type

Example:

- field name: `status`
- enum values:
  - `pending`
  - `approved`
  - `rejected`

Design intent:

- `is_enum` is no longer just a conceptual flag
- the service can now act as the source of truth for the allowed values of an enum-like field
- downstream systems can read enum options from the service rather than managing them separately

### Update Field

This updates mutable field properties such as:

- `description`
- `nullable`
- `is_enum`
- `is_unique`

If uniqueness changes, the service also updates the physical unique index accordingly.

### Delete Field

Current V1 behavior is hard delete.

The service blocks deletion when:

- the field is reserved
  - `object_id`
  - `updated_at`
- a link references the field
- a pivot references the field

If deletion proceeds, it:

- drops the unique index if needed
- drops the physical column
- deletes the metadata row
- records schema-change and migration history

## Links

Primary service: [internal/service/link_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/link_service.go)

Endpoints:

- `POST /v1/tenants/:tenantId/links`
- `DELETE /v1/links/:linkId?dry_run=true`

### What A Link Means

A link is a logical relationship between two fields:

- parent table and parent field
- child table and child field

Links are metadata-only structures. They do not currently create physical foreign-key constraints in the tenant database.

### Create Link

This:

- validates the link name
- loads the parent and child tables
- loads the parent and child fields
- checks that the tables belong to the specified tenant
- requires the parent field to be unique
- requires both link fields to be string fields
- stores the link metadata
- records schema-change and migration history

### Why Links Matter

Links are used to describe navigation and graph relationships inside the tenant model. They are a semantic layer over the underlying table data, not just a relational-DB constraint layer.

### Delete Link

Deletion is blocked if a pivot path still references the link.

If deletion proceeds, the service deletes the metadata row and records schema-change and migration history.

## Pivots

Primary service: [internal/service/pivot_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/pivot_service.go)

Endpoints:

- `GET /v1/tenants/:tenantId/pivots`
- `POST /v1/tenants/:tenantId/pivots`
- `DELETE /v1/pivots/:pivotId?dry_run=true`

### What A Pivot Means

A pivot is a tenant-level navigation or aggregation definition rooted at a base table.

It may use:

- a direct string field
- a chained path of links

### Create Pivot

This:

- validates pivot input
- verifies the base table belongs to the tenant
- if a field is provided, requires that field to be a string field
- if a link path is provided, validates that the path is chained consistently
- requires the first path link to start from the base table
- stores the pivot metadata
- records schema-change and migration history

### Why Pivots Matter

Pivots describe how higher-level graph traversal or grouping should be expressed over the tenant data model. They are an interpretation layer over tables and links.

### List Pivots

Returns all pivot definitions for a tenant.

### Delete Pivot

Current V1 behavior is simple hard delete. Pivots are removed from metadata and the delete is recorded in the audit history.

## Table Options

Primary service: [internal/service/options_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/options_service.go)

Endpoints:

- `GET /v1/tables/:tableId/options`
- `PUT /v1/tables/:tableId/options`

### What Table Options Mean

Table options are UI-oriented configuration for how a table should be presented.

Current options include:

- `displayed_fields`
- `field_order`

### Get Options

If options do not exist yet, the service returns a generated default empty options object for the table.

It also sorts field order using the current table fields so consumers get a normalized output.

### Upsert Options

This:

- verifies the table exists
- verifies referenced field ids belong to that table
- creates or replaces the table options row
- records schema-change and migration history

### Why Options Live Here

Even though these are not physical DDL changes, they are still part of the tenant-owned data-model definition. The service is intended to be the source of truth for both structural model shape and model-adjacent configuration.

## Delete Strategy Across Data-Model CRUD

Current V1 delete behavior is:

- hard delete
- with `dry_run=true` support
- with internal conflict reporting

Current internal conflict checks include:

- reserved fields
- links depending on fields or tables
- pivots depending on fields, links, or tables

There is no archive or soft-delete behavior in the current V1 contract.

## Overall Intention

This whole `data-model` area is meant to make the service the authoritative owner of tenant structure.

That means:

- tables and fields define the tenant's physical data shape
- links and pivots define the tenant's navigation and graph semantics
- table options define model-adjacent UI semantics
- the assembled read endpoint exposes the model in a consumer-friendly form

The service is not just storing definitions. It is actively synchronizing metadata, physical schema, and model history into one managed boundary.

## Related Files

- [internal/httpapi/router.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/httpapi/router.go)
- [internal/httpapi/handlers/datamodel.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/httpapi/handlers/datamodel.go)
- [internal/service/table_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/table_service.go)
- [internal/service/field_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/field_service.go)
- [internal/service/link_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/link_service.go)
- [internal/service/pivot_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/pivot_service.go)
- [internal/service/options_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/options_service.go)
- [internal/service/data_model_read_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/data_model_read_service.go)
