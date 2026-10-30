# Slide 14: Timestamps and infinity

[Reading guide](README.md) | [Previous: slide 13](13-jsonb-arrays-and-types.md) | [Next: slide 15](15-text-comparison-and-keys.md)

Companion to slide 14 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

A timestamp represents a date and time. Our tenant table creation uses PostgreSQL `TIMESTAMPTZ` and gives `valid_until` a default of `INFINITY`.

Infinity here means an endpoint that is later than every finite timestamp. It can represent an open-ended validity period.

**Why infinity needs a design decision**

MySQL temporal types have finite ranges. A candidate replacement is SQL NULL meaning "no end date," but every relevant query must adopt that convention.

Illustratively:

```text
Before: valid_until is later than the current instant.
After:  valid_until is NULL, or it is later than the current instant.
```

Replacing the stored value without changing filters would give incorrect results because comparisons with SQL NULL do not evaluate as true.

**TIMESTAMP is not a safe name-based mapping**

MySQL 8.4 TIMESTAMP has an upper limit in 2038. DATETIME supports a wider range and has different time-zone conversion behavior. We need an explicit type and UTC policy rather than matching names.

PostgreSQL TIMESTAMPTZ represents an instant; it does not preserve an arbitrary original time-zone label as separate application data.

**Why precision matters**

If two events happen within one second, truncating fractional seconds can make them appear simultaneous. Ordering and cursor pagination must remain stable, often using an ID as a tie-breaker.

Test scheduled jobs, session expiration, daylight-saving boundaries where relevant, and accepted historical/future dates.

**A limit of the existing columns**

Having valid_from and valid_until columns does not prove the system saves all historical versions. That requires actual version-writing behavior.

**How to explain this slide aloud**

"We must preserve what dates mean, including open-ended validity, precision, and time zones. Matching type names is not enough."

Evidence: [tenant table definition](../backend/data-model-service/internal/tenantdb/postgres/indexing.go). Source: [MySQL temporal types](https://dev.mysql.com/doc/refman/8.4/en/datetime.html).

---

[Reading guide](README.md) | [Previous: slide 13](13-jsonb-arrays-and-types.md) | [Next: slide 15](15-text-comparison-and-keys.md)
