# Slide 1: What are we deciding?

[Reading guide](README.md) | [Next: slide 2](02-executive-summary.md)

Companion to slide 1 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

The question is whether changing the database would benefit this fraud system enough to justify the work. Both databases can store records and run queries. The decision also concerns the rules that protect those records, the background work triggered by changes, and how we recover when something fails.

**Terms in plain English**

- **Database engine:** the software that stores data, executes queries, and coordinates simultaneous access. PostgreSQL and MySQL are engines.
- **Migration:** moving the application and its data to a different database implementation.
- **Business behavior:** what users should observe, such as a case being assigned once or a screening result remaining linked to the correct tenant.
- **Parity:** the replacement delivers the required behavior, even if the internal implementation differs.

**An example**

Suppose a request creates a screening record and schedules work to process it. A successful migration must preserve both actions. Copying the screening table is insufficient if the background job is lost or scheduled twice in a way that repeats an external effect.

That is why the presentation considers data, queues, permissions, and recovery together.

**What this means for our system**

The current architecture uses PostgreSQL across multiple services. Replacing it changes contracts those services rely on. The first decision is the reason to migrate: a required hosting platform, an organizational standard, or a benefit demonstrated by measurement.

A useful decision statement would be: "We need MySQL because of requirement X, and we will accept the migration only if behavior and operational targets Y are preserved." No particular requirement or target is assumed here.

**How to explain this slide aloud**

"We are evaluating the impact of changing a foundation of the application. The decision depends on benefits we can demonstrate and guarantees we can preserve."

---

[Reading guide](README.md) | [Next: slide 2](02-executive-summary.md)
