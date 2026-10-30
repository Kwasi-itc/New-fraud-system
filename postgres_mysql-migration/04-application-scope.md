# Slide 4: How much of the application is affected?

[Reading guide](README.md) | [Previous: slide 3](03-current-architecture.md) | [Next: slide 5](05-what-mysql-supports.md)

Companion to slide 4 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

A database dependency can appear in more places than repository files that run SELECT or INSERT statements. This slide explains why we must inventory the whole system.

**Understanding the counts**

The presentation's review found 57 upward SQL migration files and 155 Go files referencing `jackc/pgx`. An upward migration applies a structural change. The `pgx` library connects Go code to PostgreSQL and represents PostgreSQL rows, errors, and transactions.

These figures describe the inspected working tree, including tests and uncommitted files. They are dated observations, not permanent project totals.

**Why file counts do not estimate effort**

Replacing a small connection setup may be straightforward. Replacing a short function that coordinates concurrent assignment can require much more design and validation. Counting both as one file hides the difference.

Some files might disappear after an interface change; others may require entirely new implementations.

**Processes that must be included**

- **API:** handles incoming application requests.
- **Worker:** executes background tasks.
- **Migration command:** updates database structure during deployment.
- **Reconciliation tool:** detects and repairs disagreement between intended and actual state.
- **Test fixture:** builds example data and structures for tests.
- **Observation tooling:** measures queries, connections, or database activity.

**An example**

The API could create a new field successfully while a forgotten reconciliation command still connects through PostgreSQL. Recovery would then fail when that field operation is interrupted.

**How to explain this slide aloud**

"The migration affects normal requests, background processing, deployment, recovery, and tests. The counts indicate breadth, but we need behavior-level analysis before estimating effort."

---

[Reading guide](README.md) | [Previous: slide 3](03-current-architecture.md) | [Next: slide 5](05-what-mysql-supports.md)
