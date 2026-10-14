# Data Model display settings and decision columns

## Scope and contracts

Data Model owns record captions, selected columns, ordering, friendly labels and
one-hop linked-caption choices. Decisions retain their own standard columns and
request display values for only the current page of source records. Values are
current ingested values, not historical decision-time snapshots. Mixed object
types show captions; filtering to one type enables that type's configured columns.

The read implementation uses a read-only SQL contract with the
ingestion-owned current-record tables in the shared PostgreSQL database. Object
IDs are batched; identifiers come from tenant-owned metadata and are quoted.
Linked captions use declared unique parent relationships, never arbitrary joins.
Display configuration is presentation metadata, not field-level authorization.

## TODO

- [x] Persist friendly labels and linked-caption choices alongside existing table options.
- [x] Add atomic display-settings API with tenant/admin checks, validation, concurrency and lifecycle handling.
- [x] Add bounded batch record-display API with projected reads and one-hop joins.
- [x] Add Data Model display editor with caption, column selection/order, labels and link choices.
- [x] Connect Decisions captions/columns to the current page and object-type filter.
- [x] Verify validation, rollback, tenant isolation, schema lifecycle and query plans with disposable PostgreSQL.
- [x] Verify frontend build and browser setup-to-Decisions behavior, errors and case selection.
- [x] Record migration/restart instructions, evidence and remaining limitations.

Existing unrelated working-tree changes will be preserved. Runtime rollout is
separate from implementation; no application database is used for test teardown.

## Implementation and enforcement

- `GET/PUT /v1/tables/:tableId/display-settings`: versioned settings, active fields
  and eligible links. PUT requires platform administrator access in that tenant;
  the authorization is checked again inside the mutation transaction. Caption,
  options, audit and caption search-index scheduling commit together. A stale
  version returns 409. Reload saved settings after an uncertain save response.
- Existing `core.model_tables.caption_field`, `core.table_options.displayed_fields`
  and `field_order` are reused. Migration 000006 adds `field_display` (field UUID to
  label and optional declared link UUID) and `display_version`. Lifecycle triggers
  remove archived/deleted fields and deleted links, clear deleted captions and
  advance versions. Legacy option writes also enforce valid/distinct fields.
- `POST /v1/tenants/:tenantId/record-displays` accepts 1–100 source references.
  There are at most 12 configured columns. Reads require platform read permission;
  this is not a way to give case-only accounts access to ingestion data.
- The shared SQL contract reads `core.tenants.schema_name` and active tenant-owned
  model metadata, then projects only object ID, caption and selected columns from
  ingestion's current `<tenant schema>.<object type>` tables. It uses the owner's
  unique `object_id` index. A relationship uses a declared, same-tenant, active
  unique parent key; missing parents fall back to the source identifier. Enum
  values use their configured labels. No history, arbitrary SQL paths or cross-
  service mutations are used. Metadata reads select configured fields/links only
  in the record-read path. One projected source query runs per object type,
  independent of the number of records of that type, with a 10-second deadline.
- Existing Data Model pools are reused; no new connection pool or worker is added.
  Source reads hold no mutation locks. Settings saves serialize only the selected
  metadata table. No network call occurs inside a database transaction.
- OpenAPI copies in `docs/` and `internal/httpapi/` document the three new routes.

## Verification

- Data Model `go test -p 1 ./... -count=1 -timeout=10m` passed with both
  `AUTH_PREFLIGHT_TEST_DATABASE_URL` and `DATA_MODEL_TEST_DATABASE_URL` pointing
  into an isolated PostgreSQL 18 instance. This includes the existing owner
  migration/DDL/integration tests, new display tests and signed authorization tests.
- Tests cover invalid/foreign/duplicate/oversized columns, caption types, unknown
  links, concurrent stale saves, rollback after caption/index failure, atomic
  audit, tenant separation, missing records, enum labels, linked captions and
  fallback, archived fields, deleted captions/links, and stale lifecycle versions.
- The actual 100-record SQL projection was explained against 50,000 payments and
  50,000 customers. It used `payments_pkey` and `customers_pkey`, with no source
  sequential scan. One local sample executed in 1.943ms. This is query-plan
  evidence, not a production throughput guarantee.
- Frontend production build and scoped lint passed (two pre-existing unused
  Detection components remain warnings). Six browser tests passed: editor save,
  persistence, ordering, captions and linked values in Decisions, permission errors,
  plus four existing case-selection/create/retry regressions. Browser tests use
  real Next proxies backed by controlled service fixtures, not live provider login.
- Display editor screenshot inspected at `frontend/test-results/display-settings.png`.
- Final production build passed with tenant `73900736-1141-4883-8fa9-58f4ef324ee5`.
  Both OpenAPI documents parse and their display routes match. `go vet ./...`
  and `git diff --check` passed. The isolated PostgreSQL test cluster was stopped
  and removed after verification.

## Rollout (not run against the application database)

From the repository root, with the existing stack running:

```powershell
docker compose build data-model-migrate data-model-service data-model-worker frontend
docker compose run --rm --no-deps data-model-migrate
docker compose up -d --no-deps data-model-service data-model-worker frontend
```

Apply metadata migration 000006 before starting the updated Data Model API. This
uses the existing shared application database. No auth or decision-engine schema
change is required. Existing display selections are retained; labels/link choices
start empty. In Your Data Model, open a table's **Display settings**, configure it,
then open Decisions and filter **Trigger object** to that table name.

## Deliberate limits and runtime acceptance

- Current values only; no decision-time historical snapshots.
- One-hop linked captions only; no derived expressions or multi-hop joins.
- Enriched-column sorting/filtering is not enabled. Existing decision filters stay intact.
- The raw Data Viewer keeps its existing key/value representation.
- Portable model import/export retains its existing contract and does not yet carry
  the new friendly-label/link-display map. Use the display-settings API to copy it
  with remapped field/link IDs when moving configuration to another model.
- [ ] Apply the migration/rebuild to the running stack and verify a real signed-in
  administrator saves settings and sees live source data in Decisions.
