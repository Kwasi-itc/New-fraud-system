# Staged MySQL case workflow owner

This package supplies case-owned migration definitions and restricted grants for
decision workflow receipt delivery. The adapter is in `internal/store/mysql` and
uses the same receipt service and wire validation as the PostgreSQL case owner.
The opt-in receipt router uses the existing service-token and tenant-allowlist
middleware. No command or deployed runtime selects this adapter.

Supported actions are `create_case`, `add_to_case`, and
`add_to_case_if_possible`. Decision metadata is read from the decision owner's
tables in the same logical database. Case creation/reuse, decision membership,
case tags, audit events, pending case notifications and permanent intake receipts
commit atomically. Workflow execution and tenant/object locks are independent of
unrelated objects. Receipt replay preserves accepted cases after closure; a new
attempt cannot repeat the original business mutation. External workflow actions
and event delivery require their own real receiver contracts.

Install the identity owner and decision owner first. `CombinedStatements` retains
the existing `mysqlaccess` prefix and extends the **case_manager** migration ledger.
`Plan` requires reviewed pinned-engine before/after definitions. Runtime accounts
can read decision metadata but cannot modify it, rewrite receipts or delete their
proof. Trigger definers are explicit; `DefinerGrants` and `RuntimeGrants` have
different purposes. Reference observations in tests are not production artifacts.

The adapter deliberately exposes only workflow receipt operations. Full MySQL
case queues, investigator commands, assignment, screening links and maintenance
are separate case-service migration work. The case outbox is persisted here; this
does not implement its downstream publisher or claim remote delivery. Do not
activate the adapter alongside PostgreSQL case mutation authority.

Run `backend/decision-engine-service/scripts/test-case-workflow-receipts.sh` inside
the private Compose runner with both disposable databases. Tests use real owner
tables, the restricted case account, HTTP receipt delivery, source/target business
comparison, cancellation, late-write rollback, fresh/upgrade/repeat migration
checks and a subprocess crash after commit before acknowledgement. The decision
authorization/model setup remains a named fixture; this is not complete shared
application or production failover acceptance.
