# Slide 19: Drivers, SQL, errors, and repair tooling

[Reading guide](README.md) | [Previous: slide 18](18-atomic-enqueueing-and-outbox.md) | [Next: slide 20](20-concurrency-and-isolation.md)

Companion to slide 19 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

A driver translates application calls into communication with the database. A pool manages reusable connections. A migration runner executes versioned structural changes.

Replacing these components is necessary, but database-specific assumptions also appear in service interfaces and error handling.

**What a transaction type leaking through a port means**

A port is an interface describing what a service needs from storage. If it exposes RawTx() pgx.Tx, callers must understand a PostgreSQL transaction object.

That makes the boundary less portable. A replacement should expose meaningful operations with clear transaction ownership, rather than simply changing the name of the underlying type.

**An ingestion example**

The writer uses ON CONFLICT and RETURNING to decide whether a record was created or updated and to detect missing rows.

The equivalent MySQL path must preserve these outcomes under concurrency. Substituting an INSERT IGNORE-style strategy without examining which errors are suppressed would not establish equivalent correctness.

**Errors are part of behavior**

- Duplicate record: may represent a conflict or an idempotent retry.
- Foreign-key violation: indicates an invalid relationship.
- Deadlock: competing transactions cannot both proceed in their current order.
- Timeout: work exceeded a wait or execution limit.

These should not all become success, or all trigger the same retry. A whole transaction may need retrying, and retries must not repeat unsafe external effects.

**Repair tools count too**

The current index-state checker uses to_regclass, a PostgreSQL function. A MySQL API adapter would be incomplete if reconciliation still depended on PostgreSQL metadata queries.

**How to explain this slide aloud**

"The database contract includes returned results, errors, cancellation, and recovery tools. Porting SQL alone leaves important behavior behind."

Evidence: [transaction port](../backend/data-model-service/internal/ports/transactions.go), [ingestion writer](../backend/ingestion-service/internal/store/postgres/tenant_data_writer.go), [index inspection](../backend/data-model-service/internal/tenantdb/postgres/index_introspection.go).

---

[Reading guide](README.md) | [Previous: slide 18](18-atomic-enqueueing-and-outbox.md) | [Next: slide 20](20-concurrency-and-isolation.md)
