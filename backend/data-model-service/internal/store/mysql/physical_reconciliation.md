# Bounded physical recovery

`PhysicalExecutor.ReconcilePage` and the explicitly invoked
`reconcile-physical-mysql` command resume existing immutable intents. They do not
create operations, authorize an end user, verify publication grants, publish an
owner mutation or activate MySQL runtime. The operator must be authorized for the
explicit tenant and use a restricted recovery account on the shared database.

One indexed tenant/state/operation keyset query projects table and operation IDs,
with at most 1,001 rows for a page of at most 1,000 attempts. It closes selection
rows before execution; no selection transaction or row lock spans DDL. Operations
run sequentially under their own deadlines using the existing pinned connection,
table-specific named lock, immutable plan, journal and drift checks. A one-session
pool suffices. This does not establish representative production throughput.

The caller selects exactly one of prepared, applying, verifying or blocked. In
particular, blocked work is not retried automatically. An individual failure does
not suppress independent later work. Every failure is retained in the returned
joined error; the command emits partial JSON progress and exits nonzero. Each
result contains only table/operation identity and an outcome code. The error
stream is trusted operator diagnostics and may contain database error detail.

`awaiting_publication` means DDL execution reached verifying. The registry stays
closed. Other outcomes include lock_busy, schema_drift, journal_conflict,
data_precondition, operation_conflict, outcome_uncertain, deadline_exceeded,
cancelled and execution_failed. None grants permission to force-clean or adopt an
object. A connection failure can leave DDL applied; retry only through durable
inspection and the same execution lock.

`after` is the last **attempted** operation, including failures. Continue this
state's sweep from that cursor. Start a new sweep at the zero UUID to revisit busy
or failed operations and discover earlier insertions/state transitions. `exhausted`
describes the selected snapshot, never global completion. Cancellation stops new
attempts and retains the last attempted cursor. A scan failure returns no attempts
and must not be interpreted as an empty successful sweep.

Example (password supplied through DATA_MODEL_MYSQL_RECOVERY_PASSWORD):

```text
/app/reconcile-physical-mysql -address mysql.example:3306 -database fraud \
  -user data_model_recovery -tenant <tenant-uuid> -state applying \
  -limit 100 -operation-timeout 1m -timeout 10m -ca /secrets/mysql-ca.pem
```

TLS certificate verification is mandatory outside the explicitly enabled
disposable fixture. The command uses one connection and no unbounded retry loop.
It is packaged alongside the server, but is not an entrypoint or scheduler.
Scheduling, deployment grants, operator audit, owner publication, queue adoption,
former-primary fencing and full lifecycle/load acceptance remain outstanding.
