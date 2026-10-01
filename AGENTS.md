# Standing engineering requirements

These requirements apply throughout `New-fraud-system` and reflect the user's explicit direction.

## Quality and performance

- Use maintainable, idiomatic implementations with clear ownership and contracts. Do not use quick hacks, silently ignore failures, or return success for unimplemented work.
- Consider correctness, concurrency, query cost, operational behavior, and failure recovery during design. Verify performance-sensitive choices with representative data and query plans; do not claim optimization without evidence.
- Prefer bounded, indexed queries, pagination, batching, and appropriate projections. Avoid N+1 queries, scanning arbitrary subsets to locate relationships, and loading entire investigation histories into queue views.
- Keep transactions short and lock scope narrow. Use consistent lock ordering and explicit locking for mutations; do not broadly serialize unrelated work through shared parent rows. Do not hold database locks across network calls.
- Add meaningful tests for behavioral changes, rollback, tenant isolation, concurrency, and integration contracts. Record remaining gaps honestly; passing a limited test suite does not establish full readiness.

## Shared database architecture

- All services use the same logical PostgreSQL database. Do not introduce separate per-service application databases without an explicit architecture change from the user.
- Preserve clear service ownership of tables and business rules within that database. Use schema-qualified SQL and independently tracked service migrations; coordinate migration ordering when constraints or reads depend on another service's objects.
- Enforce tenant-consistent relationships. Sharing a database does not replace tenant authorization, inbox permissions, or trusted actor identity.
- For data owned by another service, choose a documented SQL/read-view or service contract based on consistency, coupling, and performance requirements. Do not assume network calls are necessary solely because service ownership differs. Do not bypass another service's mutation rules through arbitrary cross-service writes.
- Plan connection pools and worker concurrency against one database-wide connection and resource budget, accounting for every API/worker replica. Review shared CPU/I/O pressure, lock contention, indexes, and migration lock duration.
- Use transactions for changes that require database atomicity. Use an outbox for asynchronous delivery where needed; sharing a database does not make network delivery atomic.
- Test shared-schema compatibility and migration ordering. Isolated disposable databases for tests are permitted and do not imply separate application databases. Never run destructive test setup against the application database.

## Completion reports

Explain what changed, how it was verified, and material limitations. Distinguish implemented code, configured runtime behavior, and features still awaiting implementation.
