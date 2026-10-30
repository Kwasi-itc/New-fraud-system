# Slide 10: Advisory locks: coordinating logical operations

[Reading guide](README.md) | [Previous: slide 9](09-conditional-uniqueness.md) | [Next: slide 11](11-schema-changes-and-failure.md)

Companion to slide 10 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

A row lock protects a database row. An advisory lock lets application code coordinate around a logical key, such as a particular review operation, even when the work is not represented by one convenient row.

"Advisory" means cooperating code must use the same locking convention. The lock is not automatically attached to every possible write involving that business concept.

**Transaction versus session**

- A transaction is a bounded group of database operations.
- A session is the connection to the database.
- A connection pool keeps sessions available for reuse across requests.

PostgreSQL's transaction-scoped advisory lock ends when its transaction commits or rolls back. MySQL `GET_LOCK()` belongs to the session and is not released by either event.

**An illustrative failure**

1. Request A obtains a MySQL named lock.
2. Its transaction commits.
3. The application returns the connection to its pool without releasing the lock.
4. Request B, on another session, waits for that same lock.

The business operation is finished, but its coordination lock remains.

**What the migration must decide**

Some operations may use a narrowly scoped row lock instead. That row and its lifecycle must be designed correctly, particularly if the protected record does not yet exist.

If named locks are used, the application must retain ownership of the session and explicitly clean up. Request cancellation, errors, and uncertain connection state need handling.

Avoid locking a broad tenant or parent row merely because it is convenient; that can block unrelated operations.

**How to explain this slide aloud**

"Both engines can coordinate work, but the locks have different lifetimes. We must preserve exactly when ownership starts and ends."

Evidence: [review coordination](../backend/investigator/review.go). Source: [MySQL named locks](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html).

---

[Reading guide](README.md) | [Previous: slide 9](09-conditional-uniqueness.md) | [Next: slide 11](11-schema-changes-and-failure.md)
