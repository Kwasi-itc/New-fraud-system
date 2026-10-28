# Durable initial-table owner workflow

These are explicit data-model owner components. PostgreSQL/River remain the
configured runtime. No HTTP handler, worker or startup path selects MySQL here.

## Allocation and command identity

`TableService.PrepareCreation` takes the original CreateTableInput, a retained
operation UUID, trusted actor UUID and an authorized TableCreationManager. The
`dm-table-creation/v1` hash binds tenant, actor and every original input string
before normalization. It excludes generated IDs/time. The separate existing
publication hash binds the allocated table UUID and absent-version condition.
Neither hash authenticates a caller; both managers require current authorization.

The manager authorizes before taking a connection and rechecks local permission
in the owner transaction. Matching receipt replay skips ID/time allocation and
metadata effects. A new operation allocates the table and two mandatory fields,
then atomically retains the snapshot/hashes and creates metadata plus immutable
physical mappings with a closed registry. The original owner Table and fields
are retained in `dm-table-creation-result/v1`. Application code must preserve that
format when domain models evolve; changing an old snapshot encoding requires an
explicit version, never silently rewriting stored output.

The receipt primary key is tenant/operation; tenant/table is also unique. An
INSERT duplicate is accepted only after exact retained actor/request verification.
A conflicting table allocation or changed command fails. Concurrent identical
requests return one retained allocation; losing candidates are discarded. No
unrelated table is serialized through a tenant parent mutation lock.

Preparation returns an allocation, **not a successful create result**. The gate
is closed and the operation may still need a reference plan or account provisioning.
Errors, including ambiguous commit, return no allocation. Retry the same operation
with current authorization on a healthy connection; retained evidence is read
before generating IDs. There is no automatic mutation retry or new-operation
substitution after uncertainty.

## Plan, execution and publication

Capture the complete typed three-object reference outside the application target
and outside any owner row locks. `TableCreationManager.BindPlan` reauthorizes,
locks the exact model table, verifies source-bound v2 SQL and the retained tenant,
table, field IDs and registered physical mappings, then calls `PrepareTx` on the
same owner transaction. Intent and barrier/epoch changes commit together. It
never performs DDL or network work under row locks. Matching binding replay keeps
the original operation and epoch.

The restricted temporary installer executes the complete table/trigger plan with
durable dirty/applied recovery. Deactivate it before readiness inspection as
specified in the installation contract. The configured publication manager holds
the table execution lock through full shared-lineage/shape/grant inspection and
`TableService.PublishPreparedCreation` audit/revision/contract publication.

`RecoverCreation` returns the retained original Table only when the published
intent and immutable ingestion-publication receipt agree on operation, table,
actor, hash and absent-version condition. The intent uses SQL NULL for absent
expected version; the publication receipt uses the established zero UUID. Pending
allocation/execution returns ErrModelUnavailable. A later metadata edit does not
change the original result; historical recovery does not reopen a newer barrier
or certify current physical data availability. Every recovery reauthorizes.

## Append-only receipt upgrade

V003 appends four steps to the frozen 81-step v002 lineage: receipt table and
INSERT/UPDATE/DELETE guards. V001/v002 source and checksums remain unchanged.
The receipt has a tenant FK, no cascading table FK, bounded original JSON, exact
UUID/hash keys, and an immutable timestamp. UPDATE cannot rewrite evidence and
DELETE is rejected pending an explicit retention protocol. This is intentional
retention, not an implemented cleanup/retirement policy.

A separate locked receipt definer has SELECT/TRIGGER only on this table. It does
not inherit the shared dictionary resolver identity or capabilities. The v003
dependency constructor validates all 85 source-bound steps and exact grants,
requires no existing definer sessions, and rejects additional receipt triggers.
Ordinary owner access needs SELECT/INSERT on receipts, not UPDATE/DELETE. Actual
production owner/grant manifests and credential deployment still require review.

The fixture-only builder accepts `-creation-receipt-definer-user` (at the selected
definer host), which includes v002 and v003. It emits the new artifact and separate
receipt-definer provisioning/grant SQL without overwriting existing paths. The
shipped `testdata/mysql-reference-fraud-v003` artifact is marked needs_review.
Production installation must verify the extra receipt policy explicitly, e.g.
through the existing reviewed account-policies installation/verification interface.

Remaining integration: production authorization/principal deployment; operational
reference capture and installer lifecycle orchestration; request/operation-key
transport and existing explicit transient-error API behavior (no new pending
success semantics are enabled); recovery scheduling; retirement and
receipt retention; runtime/consumer adoption; replication/failover and load tests.
Passing isolated owner workflow tests does not complete those module gates.
