# Slide 26: Data conversion and reconciliation

[Reading guide](README.md) | [Previous: slide 25](25-feasibility-prototype.md) | [Next: slide 27](27-cutover-choices.md)

Companion to slide 26 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

Data conversion changes records into the target representation. Reconciliation checks that the converted system still contains the intended information and relationships.

Copying every row is necessary, but equal row counts do not establish correctness.

**An illustrative count problem**

Both databases might have one thousand case records, yet some cases could reference the wrong tenant or have the wrong status. Counts match while the business meaning differs.

Validation must inspect relationships and values as well as quantities.

**What must be included**

Inventory service metadata, dynamic tenant tables, evidence, audit records, pending jobs, and identity-provider data. Preserve identifiers so relationships and external references continue to resolve.

Convert arrays, JSON, timestamps, infinity, UUIDs, and binary values according to a documented contract.

**What canonical comparison means**

Canonical comparison puts equivalent data into a common logical form before comparing it.

For example, two JSON objects can have the same properties in different serialized orders. Raw byte comparison may report a difference even if the application meaning is identical. Conversely, numbers or missing/null fields must not be normalized in a way that hides meaningful changes.

**Changes during the copy**

If the source remains writable, the initial copy becomes stale. The migration must account for inserts, updates, and deletes occurring afterward.

Queued work also changes state. A copied pending job may already have completed in the source, so job-state reconciliation is necessary.

**How to explain this slide aloud**

"We need to prove that records retain their meaning and relationships. Matching table counts alone does not prove a correct transfer."

---

[Reading guide](README.md) | [Previous: slide 25](25-feasibility-prototype.md) | [Next: slide 27](27-cutover-choices.md)
