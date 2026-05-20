# Data Model CRUD Operations

This document explains the `data-model` endpoint group in the data model service, what each endpoint does today, and what the design is trying to achieve.

## Purpose

The `data-model` area is where the service manages the tenant-owned schema model itself:

- tables
- fields
- links
- pivots
- navigation options
- table options
- assembled data-model read

These routes are defined in [internal/httpapi/router.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/httpapi/router.go) and handled in [internal/httpapi/handlers/datamodel.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/httpapi/handlers/datamodel.go).

The design assumption is that a tenant already exists and has already been provisioned before these endpoints are used.

## Endpoint Group

Current `data-model` routes:

- `GET /v1/tenants/:tenantId/data-model`
- `GET /v1/tenants/:tenantId/tables`
- `POST /v1/tenants/:tenantId/tables`
- `PATCH /v1/tables/:tableId`
- `DELETE /v1/tables/:tableId?dry_run=true`
- `GET /v1/tables/:tableId/fields`
- `POST /v1/tables/:tableId/fields`
- `GET /v1/fields/:fieldId/enum-values`
- `POST /v1/fields/:fieldId/enum-values`
- `PATCH /v1/fields/:fieldId`
- `PATCH /v1/enum-values/:enumValueId`
- `DELETE /v1/fields/:fieldId?dry_run=true`
- `DELETE /v1/enum-values/:enumValueId`
- `GET /v1/tenants/:tenantId/links`
- `POST /v1/tenants/:tenantId/links`
- `DELETE /v1/links/:linkId?dry_run=true`
- `GET /v1/tenants/:tenantId/pivots`
- `POST /v1/tenants/:tenantId/pivots`
- `DELETE /v1/pivots/:pivotId?dry_run=true`
- `GET /v1/tables/:tableId/navigation-options`
- `POST /v1/tenants/:tenantId/navigation-options`
- `DELETE /v1/navigation-options/:navigationOptionId`
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

- `GET /v1/tenants/:tenantId/tables`
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

### List Tables

`GET /v1/tenants/:tenantId/tables`

This returns the table metadata rows currently defined for a tenant.

Use this when a client needs:

- a lightweight table inventory
- table ids before creating fields or links
- table metadata without loading the full assembled graph

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

- `GET /v1/tables/:tableId/fields`
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

### List Fields

`GET /v1/tables/:tableId/fields`

This returns the field metadata rows for one table.

Use this when a client needs:

- field ids for link creation, options, or navigation options
- a lighter read than the full assembled data model
- direct inspection of raw field metadata on a table

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

- `GET /v1/tenants/:tenantId/links`
- `POST /v1/tenants/:tenantId/links`
- `DELETE /v1/links/:linkId?dry_run=true`

### What A Link Means

A link is a logical relationship between two fields:

- parent table and parent field
- child table and child field

Links are metadata-only structures. They do not currently create physical foreign-key constraints in the tenant database.

### How To Read A Link

The easiest way to read a link is:

- `child_table.child_field -> parent_table.parent_field`

That is the practical meaning of the metadata.

The child field stores values that are expected to match values in the parent field.

Example:

- parent table: `accounts`
- parent field: `object_id`
- child table: `transactions`
- child field: `account_id`

This means:

- `transactions.account_id -> accounts.object_id`

In plain English:

- each row in `transactions.account_id` is expected to hold an account identifier
- that identifier should match one row in `accounts.object_id`

Example data:

`accounts`

- `object_id = acc_001`
- `object_id = acc_002`

`transactions`

- `object_id = tx_100`, `account_id = acc_001`
- `object_id = tx_101`, `account_id = acc_001`
- `object_id = tx_102`, `account_id = acc_002`

That means:

- transaction `tx_100` links to account `acc_001`
- transaction `tx_101` links to account `acc_001`
- transaction `tx_102` links to account `acc_002`

### Why Parent And Child

The parent side is the referenced side.

The child side is the side carrying the reference value.

In the example above:

- `accounts.object_id` is the parent field
- `transactions.account_id` is the child field

The parent field must be unique, because one child row should resolve to one specific parent row.

That gives a many-to-one shape:

- many `transactions`
- one `account`

So from one transaction row, the system can resolve one linked account row.

That is why the assembled model exposes these as `links_to_single`.

### Another Example

- parent table: `customers`
- parent field: `object_id`
- child table: `cases`
- child field: `customer_id`

Meaning:

- `cases.customer_id -> customers.object_id`

If a case row contains `customer_id = cust_900`, the system can interpret that as:

- this case belongs to the customer whose `object_id` is `cust_900`

### Why Links Are Semantic Instead Of Physical Foreign Keys

The service treats links as relationship metadata used for:

- assembled data model reads
- pivots
- navigation options
- graph-style traversal semantics

They are not currently enforced as PostgreSQL foreign-key constraints on tenant tables.

That matches the Marble-style design:

- physical tenant schemas own table and column shape
- link metadata owns the meaning of cross-table relationships

This keeps ingestion and tenant-schema operations more flexible while still allowing the platform to understand how records relate.

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

### List Links

`GET /v1/tenants/:tenantId/links`

This returns the link metadata rows currently defined for a tenant.

Use this when a client needs:

- link ids for pivot creation or delete operations
- a tenant-scoped relationship inventory
- relationship metadata without loading the full assembled data model

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

### How To Think About A Pivot

A pivot tells the system:

- when starting from rows in this base table, which value or linked identity should be used as the grouping anchor

If links describe relationships between tables, pivots describe the higher-level identity the platform should use to gather related activity.

In practice, a pivot answers a question like:

- for one row in this table, what customer, account, merchant, or other identity should we group around?

### Field-Based Pivot

The simplest pivot uses a field directly on the base table.

Example:

- base table: `transactions`
- field: `account_id`

That means the pivot value for a transaction is simply:

- `transactions.account_id`

Example data:

- `tx_100.account_id = acc_001`
- `tx_101.account_id = acc_001`
- `tx_102.account_id = acc_002`

That gives grouping like:

- `tx_100` and `tx_101` belong to the pivot value `acc_001`
- `tx_102` belongs to the pivot value `acc_002`

Use this when the base table already contains the identity value you want to group on.

### Path-Based Pivot

A path-based pivot uses one or more links starting from the base table.

Example:

- base table: `transactions`
- path link ids: link path representing `account`

If the link means:

- `transactions.account_id -> accounts.object_id`

then the pivot says:

- start from a transaction
- follow the `account` link
- use the linked account-side identity as the grouping anchor

This lets the system group or traverse around an identity that is reached through relationships, not only around a raw field already present on the base table.

Longer paths are also possible when links chain consistently.

That means the system can move from:

- `transactions`
- to `accounts`
- to some other linked entity

and use that final linked identity as the pivot anchor.

### Why Pivots Exist

Fraud systems often need to answer questions like:

- which alerts belong to the same customer?
- which transactions are tied to the same account?
- which decisions should be grouped into the same case?
- what other objects are related to the same investigation anchor?

A pivot provides that grouping rule.

### Link Versus Pivot

These two concepts are related but different:

- a link says how one table points to another
- a pivot says which identity or value should be used to group or traverse activity

So:

- link = relationship metadata
- pivot = grouping and traversal anchor

### Why The Base Table Matters

The base table is where evaluation starts.

If the base table is `transactions`, then each transaction row is interpreted through the pivot definition.

So the pivot is effectively answering:

- for this transaction row, what is the related identity we care about for grouping?

### Why Pivot Fields Must Resolve To Strings

The service requires the pivot field to resolve to a string-compatible field because pivots are used as identity or grouping values.

That usually means values like:

- `acc_001`
- `cust_900`
- `merchant_42`

These are stable values that can be used to group records, relate decisions, or drive navigation.

### Typical Uses Of Pivots

In a fraud platform, pivots are commonly used for:

- grouping activity into the same case
- finding all records related to the same customer or account
- driving investigation traversal
- centering analytics or exploration around a business identity

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

## Navigation Options

Primary service: [internal/service/navigation_option_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/navigation_option_service.go)

Endpoints:

- `GET /v1/tables/:tableId/navigation-options`
- `POST /v1/tenants/:tenantId/navigation-options`
- `DELETE /v1/navigation-options/:navigationOptionId`

### What A Navigation Option Means

A navigation option is a saved rule that tells a client:

- when viewing records from this source table
- and using this source field value
- here is the target table you can navigate into
- and here is how to find and order the matching target rows

In simpler terms, it is a pre-declared "jump" from one part of the tenant model into another.

### How To Read One

A navigation option stores:

- `source_table_id`
- `source_field_id`
- `target_table_id`
- `filter_field_id`
- `ordering_field_id`

That means:

- start from a record in the source table
- read the source field value
- look in the target table for rows whose filter field matches that value
- sort the result using the ordering field

Example:

- source table: `accounts`
- source field: `object_id`
- target table: `transactions`
- filter field: `account_id`
- ordering field: `updated_at`

This means:

- when a client is on an account
- it can navigate to transactions
- by finding `transactions.account_id = accounts.object_id`
- and ordering the transactions by `updated_at`

### Why Navigation Options Exist

Links and pivots describe the model's relationship and grouping semantics.

Navigation options go one step further and define a concrete consumer-facing traversal:

- from this source object
- open that target collection
- using this match field
- in this sort order

That is useful for:

- detail pages that need a related-records view
- investigation screens that jump from one entity to its activity
- API consumers that want stable navigation behavior without inventing their own join rules

### Why They Need Backing Model Semantics

The service does not allow arbitrary source-target combinations.

Creation is only allowed when the option is backed by existing data-model semantics:

- a reverse link
- or a self-table pivot

That is why the service validates that the requested navigation path is already meaningful in the model instead of letting clients create free-form table jumps.

### Create Navigation Option

This:

- verifies source and target tables belong to the tenant
- verifies the referenced fields belong to the correct tables
- requires the source field to be a string field
- requires filter and ordering fields to be different
- if source and target are the same table, requires source and filter fields to match
- requires a backing reverse link or self-table pivot
- stores the navigation option metadata
- enqueues an async navigation index job for the target table
- records schema-change history

### List Navigation Options

`GET /v1/tables/:tableId/navigation-options`

This returns the stored navigation options for one source table, including resolved table and field names.

Use this when a client needs:

- the available related-record navigations from a table
- the exact filter and ordering fields for those navigations
- navigation metadata without loading the full assembled data model

### Delete Navigation Option

Deletion removes the stored navigation rule from metadata and records the delete in schema-change history.

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
