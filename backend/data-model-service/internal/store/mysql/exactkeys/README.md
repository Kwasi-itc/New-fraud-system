# Data-model exact-byte key dictionary

This package provides ordered SQL definitions for the data-model owner's private
dictionary. It addresses the unbounded TEXT unique domains in PostgreSQL metadata:
`core.tenants.external_key/schema_name`, table/field/link names and enum value/label
keys. `datamodel.ValidateObjectName` checks syntax, not a maximum length. No public
length limit is tightened, and no prefix or digest is treated as exact equality.

These definitions are not installed by application startup or the existing
migration command. The production metadata manifest, complete consumer triggers,
restricted migration lineage and grants remain required before activation.

## Contract

The identity is `(key_domain, scope_bytes, raw_value)` using byte equality. Domains
are fixed internal names (up to 64 bytes), not user input. Scope is empty for an
explicitly global domain, one network-order UUID for a tenant, or two concatenated
fixed-width UUIDs for tenant plus parent. Empty scope is not a fabricated tenant.
Variable-width tuple components must use a separately specified canonical encoding;
do not concatenate user strings or NULL sentinels into scope.

`data_model_resolve_exact_key` derives a SHA-256 bucket server-side, locks that one bucket,
then compares full `LONGBLOB` values through a current locking read. Equal bytes
reuse an internal UUID. Different bytes in the same bucket receive different UUIDs.
An owner UNIQUE key on the derived UUID enforces uniqueness in the chosen domain.
The UUID is internal identity only; existing public identifiers do not change.

The bucket has exactly one unique constraint: its composite primary key. Its no-op
upsert is safe only for that shape. The explicit locking read follows it. Neither
the digest nor its prefix is unique in the dictionary; the full-value comparison
and serialized allocation establish exact identity. Duplicate full values caused
by corruption fail the SELECT INTO rather than silently choosing one.

The bucket primary key uses at most 128 data bytes (64 domain + 32 scope + 32 hash),
with internal variable-length overhead. Dictionary lookup uses the same parts and
its 16-byte primary identity. Raw text is not an index key. This avoids applying the
InnoDB key-size budget to an unbounded user value, but does not eliminate packet,
row, memory or operational size limits. Those require source-data profiling.

The dictionary lookup uses `FOR SHARE` while the bucket retains its exclusive
lock. This is a current read even after an older repeatable-read snapshot, without
granting UPDATE on immutable dictionary rows. The resolver rejects sessions that
disable unique or foreign-key checks. Complete metadata triggers must preserve
that guard even for nullable branches which do not call the resolver.

## Mandatory enforcement boundary

1. Consumer BEFORE INSERT/UPDATE triggers derive key IDs from the row's actual
   value and constant domain/authorized scope. Ignore supplied derived IDs.
2. Triggers call the resolver within the consumer DML transaction. Do not grant
   standalone resolver execution to runtime callers: a CALL without an explicit
   transaction is not the allocation contract.
3. Runtime users have no dictionary read/write, routine EXECUTE, DDL or TRIGGER
   privileges. The trigger/routine definer is a separate locked account with only
   required table and routine privileges. Do not leave the installer as a broadly
   privileged production definer.
4. Preserve NULL uniqueness deliberately: nullable external values set the key to
   NULL; nonnullable domains must retain their NOT NULL requirement. Empty bytes
   are a real value, not NULL. Conditional dedupe domains need their own explicit
   empty/nonempty rule.
5. Tenant/parent identities must be immutable or undergo an explicitly authorized
   ownership transition. The dictionary does not perform application authorization
   or replace tenant-consistent foreign keys.
6. All tables participating in an allocation are InnoDB. Never commit inside the
   resolver. Owner failure/rollback must undo new dictionary and bucket rows.
   Use a stable lock order for multiple domains and retry whole owner transactions
   only under their existing idempotency/recovery contract after deadlock.

Owner triggers must use local OUT variables, not caller-controlled SQL session
variables. Routine definitions contain no dynamic SQL and no transaction control.
Install with strict SQL mode and schema-qualified names. Preserve binary equality
when converting UTF-8 metadata text into the raw key; normalization remains the
existing domain service's responsibility. A different hash function or scope
encoding requires coordinated rebuilding, not an online function edit.

Dictionary keys and buckets are retained after consumer deletion. Garbage
collection is deliberately absent until a safe reference/locking protocol exists;
manual deletion can break identity reuse. Restrict access and protect backups:
the retained raw keys may contain sensitive values and create a retention obligation.

## Verification boundary

The integration fixture uses the real definitions and a separate locked definer
with restricted grants. Representative tenant-scoped and global nullable consumer
triggers test long values, case/Unicode/trailing-byte differences, duplicate updates,
concurrent allocation, rollback, forged IDs and denied bypasses. A separate isolated
database replaces only the digest function with a constant to exercise real hash
collisions through the full resolver and owner constraints.

The fixture triggers are not production metadata triggers. Full tenant-composite
FK coverage, every column/key mapping, owner mutation integration, privileged
migration recovery, replication/failover and workload benchmarks remain separate
acceptance gates. No throughput improvement is claimed from these correctness tests.

## Authoritative constraints

- [MySQL BLOB semantics](https://dev.mysql.com/doc/refman/8.4/en/blob.html): binary
  values compare by bytes; strict-mode overflow handling must remain enabled.
- [Trigger restrictions](https://dev.mysql.com/doc/refman/8.4/en/trigger-syntax.html):
  OUT parameters are permitted, while trigger transaction control and dynamic SQL
  are restricted. These definitions use one enclosing owner transaction.
- [Routine security](https://dev.mysql.com/doc/refman/8.4/en/create-procedure.html):
  definer privileges and EXECUTE grants are part of the deployment contract.
- [Locking reads](https://dev.mysql.com/doc/refman/8.4/en/innodb-locking-reads.html):
  FOR SHARE requires SELECT; FOR UPDATE also needs a modifying/locking privilege.
  The bucket and dictionary therefore use different locking modes intentionally.
- [Stored-program restrictions](https://dev.mysql.com/doc/refman/8.4/en/stored-program-restrictions.html):
  replication and foreign-key cascades need separate consideration; cascades do not
  invoke triggers. The dictionary therefore does not rely on delete triggers for GC.
