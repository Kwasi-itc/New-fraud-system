# Staged screening publication storage

This package owns MySQL publication definitions, independently tracked in
`screening_schema_migrations` in the same logical database as decision objects.
It does not change the configured PostgreSQL screening service.

Install decision effects and their configuration/scenario parents first, then
apply `Plan` with definitions captured from an isolated MySQL 8.4.8 reference
installation. Never capture the destination's definitions to adopt drift.
`Statements` is reference/deployment tooling, not a request SQL interface.

Linked sources require explicit tenant, decision, scenario, configuration and
execution bindings. FKs establish tenant relationships; the insert trigger also
checks that the execution belongs to that exact decision/scenario/configuration.
Bindings are immutable. Manual sources have all four relationship columns NULL
and are excluded from both the read view and projection delivery. Legacy text
idempotency keys require explicit resolution before importing linked sources.

`screening_decision_source_v1` is an updatable MERGE view with invoker security.
It projects owner-generated summary JSON, not raw provider responses. Identity
and status changes cannot be smuggled through caller-supplied summary JSON.
The row payload is capped at 1 MiB and reference/error fields at 16 KiB each;
the decision consumer can impose a smaller combined byte limit. Source locks
taken through this view reach the source row. Consumer accounts still need
reviewed base-column and view grants; this package does not grant application
access automatically.

Source updates require exactly the next positive version. AFTER triggers enqueue
the current source version and reset retry state in the same transaction. A
delivery write failure rolls back the source update. The `mysqlpublication`
owner implements version-checked status/result/match updates and a bounded
projection drain. Completion replaces bounded match evidence and maintains
`match_count` atomically. These are staged APIs; the configured service remains
PostgreSQL.

The append-only intake extension retains one immutable payload hash per
tenant/execution, and one permanent receipt per full execution/attempt/redrive
tuple. `mysqlpublication.NewIntake` requires a service authorization callback and
an explicit provider-name allowlist. It commits the pending source, its required
projection delivery and the acceptance receipt together. Reconciliation reads
the original receipt; a changed payload conflicts even across domain attempts.
New attempts for an accepted execution retain its original source and never
restart a completed search. Imported sources without intake receipts are not
silently adopted. Receipt deletion and mutation are prohibited.

`decision-engine-service/internal/clients/screeningreceipt` implements the
decision receipt port through this local screening-owned mutation contract.
There is no caller-controlled destination URL or network call in the transaction.
Acceptance means durable screening intake, not external provider completion.
The existing source owner can subsequently publish processing/results. The
actual external-search worker, trusted runtime principals, restricted runtime
grants and full application composition remain separate prerequisites.

Intake locks one execution reservation, then its screening source; relationship
checks and projection enqueue occur under the same transaction. Reconciliation
uses the full indexed attempt tuple. A decision acknowledgement arriving after
source publication checks the matching source reference and records the receipt
without overwriting that newer projection. It can resume only an existing send
intent once the source owns the result.

Verification is in the decision module's
`TestMySQLScreeningOwnerPublicationBindingsAndView`, `TestScreeningReceiptOwner`
and `TestScreeningReceiptDispatchHandoff`, using actual owner plans in
generated disposable schemas. Restricted runtime identities, full provider
runtime integration, cross-engine parity and process recovery remain separate
checklist gates.
