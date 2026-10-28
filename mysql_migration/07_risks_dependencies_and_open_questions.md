# Risks, dependencies and decisions

Severity reflects possible impact; likelihood is conditional on a naive migration, not a measured incident probability. All risks are OPEN until implementation evidence closes them. No production performance or data-integrity failure was inferred from source inspection alone.

| Risk | Severity / likelihood | Affected evidence | Mitigation and dependency |
|---|---|---|---|
| R-01 Partial DDL commits publish invalid model | Critical / high with direct port | PG-04–07; same tx schema manager | Recoverable intents, exact physical introspection, publish last; M-02/M-05/M-06 |
| R-02 Business commit loses queue job | Critical / high if River removed without replacement | PG-09/10/14, EnqueueTx | Atomic queue/outbox contract and crash/replay tests; M-03/M-04 |
| R-03 Tenant or permission leak | Critical / unquantified | PG-03/17/18/33/35; namespace mapping, identity functions, inbox checks | Tenant-composite FKs where required, trusted identity, owner contracts, cross-tenant tests; M-02/M-10/M-11 |
| R-04 Wrong conflict target / same-value row count | Critical / high with naive upsert | PG-08/09/16/28 | Explicit mutation outcomes, alternate-unique and concurrent insert tests; M-04/M-07/M-09 |
| R-05 Stale worker overwrites newer result or delivery | Critical / medium if claim is split incorrectly | PG-12–15; token/attempt fencing | Conditional claim/finish, source→queue locks, crash and delayed-provider schedules; M-03/M-09 |
| R-06 Exclusion/review fails or loses audit atomicity | Critical / certain for unchanged immediate FK order | PG-16/18 | Parent-first validated write design, immutable history/source guards; M-09/M-10/M-11 |
| R-07 Conditional uniqueness or text key drift | High / high with default schema translation | PG-05/16/19/25 | Generated active keys or explicit reservations, exact-value long-key contract; M-02/M-05 |
| R-08 Deployment points services at different DBs | Critical / plausible configuration drift | Root scale override omits auth/provider; service-local Compose uses legacy DBs (PG-02/32) | Render/audit all profiles and command paths together; M-12 |
| R-09 Changed isolation/deadlock behavior | High / unquantified | PG-11/12/17/19 | Explicit session isolation, lock graph, narrow indexed claims, bounded retries; M-04/M-14 |
| R-10 JSON/order/time/numeric drift changes decisions | Critical / unquantified | PG-20–26/34 | Differential corpus including nulls, precision, aggregate/filter/time boundaries and workflow order; M-07/M-08/M-14 |
| R-11 Assignment capacity/last-admin guard weakened | High / plausible if trigger/function omitted | PG-17/19 | Shared authority for every write path; concurrent revoke/reopen/assign tests; M-10/M-11 |
| R-12 Query/pool pressure regresses throughput | High / unknown | PG-02/27/32/34; dynamic tables and shared DB | Representative per-query plans and all-process budget, queue age/lock/I/O metrics; M-14 |
| R-13 Provider identities or sessions lost | High / unknown | PG-29, issuer/subject app mappings | Version-specific provider migration rehearsal and state coverage; M-11/M-13 |
| R-14 Runtime schema differs from repository | High / unknown | Untracked migrations and old historical versions | Live read-only catalog against frozen worktree; M-01 |
| R-15 False confidence from skipped/historical tests | High / plausible | PG-31, reverted prototype docs | Explicit executed/skipped reports, mandatory isolated MySQL integration; M-04/M-14 |
| R-16 Unsafe rollback after target writes | Critical / unknown | PG-09/14/30; data changes and external delivery | Retained source, fenced ownership, reverse delta/forward repair rehearsal; M-13/M-15 |
| R-17 Shared River schema/version incompatibility | High / unknown | PG-10; 0.26/0.31 modules, default namespace | Capture installed DDL and compatibility; replacement job mapping; M-01/M-03 |
| R-18 Existing failure-path gaps preserved accidentally | Medium–high / source-confirmed code, runtime likelihood unknown | Ingestion ignored validation-audit error; no-op enqueuer defaults; unreserved idempotency first read; unspecified tie order | Characterize current behavior, decide intended contract explicitly; no claim migration automatically repairs it; M-07/M-14 |

## Decisions required before implementation

| ID | Question / needed evidence | Owner role / blocking task |
|---|---|---|
| Q-01 | What business requirement justifies migration, and what p95/p99 latency, sustained throughput, queue age, error rate, RPO/RTO and downtime gates apply? No MySQL advantage has been measured. | Product + operations / M-01 |
| Q-02 | Exact MySQL patch/hosting, InnoDB settings, OS/case rules, collation, strict mode, UTC, isolation, TLS and driver? 8.4 is only the assessment baseline. | Architecture + DBA / M-02 |
| Q-03 | Physical name registry/prefix design versus larger tenant-table redesign; allowed ID lengths and exact-value long-key approach? | Data-model owner / M-02/M-05 |
| Q-04 | Queue implementation and owner; recurrence, retries, dedupe, cancellation, leases and old-backlog migration? | Worker owners / M-03 |
| Q-05 | Which SQL functions become MySQL routines versus owner application contracts, and how are direct writers constrained? | Identity, case, model and screening owners / M-06/M-09–M-11 |
| Q-06 | Is local Keycloak used in real deployment; what state must migrate; can it meet the one-database design with supported provider settings? | Identity + operations / M-11 |
| Q-07 | Actual data volumes, tenant/table counts, text lengths, IP formats, time infinity/ranges, blob sizes and schema drift? | DBA / M-01/M-13 |
| Q-08 | Are read/worker URLs separate pools on the same primary or replicas; what read-after-write guarantees are needed? | Operations + ingestion / M-01/M-12 |
| Q-09 | Which external CI/deployment/backup/integration jobs access PostgreSQL but are not checked into this repo? | Operations / M-01/M-12 |
| Q-10 | Freeze point for existing uncommitted work; are new manual/exclusion/authoring migrations applied anywhere? | Repository/service owners / M-01 |
| Q-11 | Post-cutover reverse replication/replay or forward repair policy, retention and abort authority? | Operations + business owner / M-13/M-15 |
| Q-12 | Should known baseline gaps be fixed before port, retained, or separately changed with explicit behavior tests? | Service owners / M-07/M-14 |

## Inspection limitations

Source discovery and critical-path tracing were performed; not every mechanically matched line was separately simulated. No running database, runtime configuration, query plan, benchmark, installed dependency source, provider data or private local secrets were inspected. Full live schema (including dynamic tables, River and provider DDL) is unresolved. Generated stress run JSON was enumerated/scanned where applicable but not treated as reproducible current results. This is a source assessment with an explicit verification backlog, not a claim of production migration readiness.
