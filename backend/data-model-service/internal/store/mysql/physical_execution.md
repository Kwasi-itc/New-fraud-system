# Physical DDL step execution and recovery

`PhysicalExecutor.ExecuteSteps` is staged internal execution, not an API success
path or a schema planner. It takes tenant/table/operation identity, resolves the
immutable registry and loads the bounded durable intent. A v1 `PhysicalDDLPlan`
contains 1..1,000 trusted owner-generated CREATE/ALTER table steps with complete
before/after definitions. SQL is never accepted from an API. Validation restricts
the declared object and qualified statement prefix to the registered table; this
is not a sandbox for arbitrary SQL. [Typed field/native-index planning](physical_planning.md)
now supplies supported operations; full table/lifecycle/unique-text planners remain
required. Typed NOT NULL additions carry an empty-table precondition to prevent
silent implicit value synthesis on populated tables.

One database/tenant/table-scoped named lock is held on the same dedicated pinned
connection used for every statement and inspection. A queue lease expiring cannot
release this lock. Server-local locks do not fence a former primary; deployment
must exclude old writers during failover. Consumers must adopt the registry gate;
this adapter cannot protect a reader that bypasses it.

Before each journal mutation, a short READ COMMITTED transaction locks registry
then intent, checks the active operation, expected epoch and closed barrier, and
rejects publication or replacement. The step journal records tenant/table/op,
contiguous ordinal, SHA-256 checksum, dirty/applied state and timestamps. Identity
and checksum are immutable. Journal reads are bounded and indexed. Row locks are
released before executing DDL. Commit/rollback/lock-cleanup failures propagate.

- A new step requires the exact before definition, then commits a dirty record.
- A dirty step matching before may execute; matching after is finalized without
  repeating DDL. Neither match means visible drift and a blocked intent.
- Each successful DDL is inspected before its journal row becomes applied.
- Completed replay verifies the final table definition, including columns,
  indexes, constraints and table options. Unjournaled existing objects are not
  adopted. Changed/gapped journal entries fail lineage validation.
- A lost DDL acknowledgement leaves the journal dirty. Recovery uses the same
  lock and actual shape, rather than treating connection failure as rollback.

Success means **steps executed and intent moved to verifying**. The registry stays
closed and unpublished. `WithVerifiedPhysical` separately checks complete applied
progress, actual schema, absence of unplanned triggers and explicit physical role
grants while retaining the DDL lock through an owner callback. Complete semantic type/collation/uniqueness rules,
operation-specific data preflight, atomic owner publication and barrier release
are not completed by this executor. SHOW CREATE observations must come from a
trusted planner/reference contract, never from accepting target drift. Mutable
definitions such as AUTO_INCREMENT need an explicit normalization contract before
support. Multi-object/routine changes need a separate reviewed execution plan.

Nine real-engine scenarios cover fresh/repeated execution, dirty-before and
dirty-after restart, unrecorded-object rejection, dropped-index drift, competing
executor exclusion, tenant isolation and actual lost CREATE/ALTER acknowledgements.
Wire faults consume a real successful server response before hiding it. Recovery
then completes without blindly repeating CREATE/ALTER. Unit tests reject foreign
objects/databases, invalid versions/definitions, unknown fields and excess input.
Full selected race regression and module-wide vet passed on MySQL 8.4.8.

Remaining: complete table/type-change/backfill/retirement planners and text indexes,
unique long-text enforcement, production role policies, owner publication,
scheduled reconciliation and queue wiring, failover/retirement
acceptance and representative lock-duration/load measurements. No runtime switch.

[Bounded recovery](physical_reconciliation.md) now supplies a tenant/state page
and an explicit operator command. Partial failures remain errors, cursors advance
only across attempted work, and successful recovery still awaits publication.

## Atomic publication after verified physical work

The unconfigured publication path accepts native v1 plans only. V2 requires
explicit `WithCreationReadiness` configuration with a source-bound shared
installation inspector and the exact temporary installer policy. Native v1 role
checks alone still return `ErrPhysicalPlanningUnsupported` for v2.

## Version 2 initial table creation

V2 is restricted to the generated initial table followed by its BEFORE INSERT
and BEFORE UPDATE exact-key triggers. It is not an arbitrary multi-object DDL
format. The immutable creation identity regenerates and checks every statement,
step identity, object and order. All before definitions must be absent; all after
definitions come from the isolated reference. V1 cannot carry a creation identity.
The executor checks the identity's tenant/table against its own scope before DDL.

The existing per-step journal, pinned connection and table execution lock apply
to all three objects. Before resuming, the executor verifies every object's state:
applied objects must match after, dirty objects must match before or after, and
unjournaled objects must match before. This checks the already-applied table and
earlier trigger even when recovery is currently at a later dirty trigger. It also
rejects unexpected triggers using a bounded information-schema query. At the end,
all three definitions and the complete trigger set are checked again. A dropped
or altered earlier object blocks recovery; the executor does not recreate it by
silently resetting its journal. Grant inspection is not implied by these checks.

Real-engine tests cover thirteen creation recovery scenarios: fresh/replayed
execution; dirty-before and dirty-after for each trigger; unrecorded triggers;
earlier table/trigger drift; additional triggers; actual lost acknowledgements for
each trigger; fully-applied trigger drift; and tenant isolation. Reconciliation
uses the same checks and retains a closed/unpublished registry. Invalid-plan tests
reject substituted mappings, scope, principals, SQL, ordinals and definitions.

Execution itself never opens the readiness barrier. Separate publication tests
now execute/replay the table and both triggers using the restricted temporary
installer described in the installation contract. The earlier recovery fixture
still uses an administrator; neither fixture approves production deployment.
Runtime remains PostgreSQL/River.

The [shared dependency inspector](installation/README.md#shared-exact-key-dependency-inspection)
now binds supported installation artifacts to source and verifies their complete
applied lineage, final shapes and locked resolver grants on a caller-owned pinned
connection. It also rejects dictionary triggers and existing resolver-definer
sessions. The explicitly configured v2 publisher invokes it under the table
execution lock, checks the exact locked record-trigger definer and its sessions,
and requires the installer locked/disconnected with SET_ANY_DEFINER revoked.
All principals remain distinct; physical consumers cannot receive private
dictionary access. Missing/excess grants or any incomplete state prevents the
owner callback. Deployment must exclude shared installation and privilege changes
through publication; the table lock cannot fence arbitrary administrators.

After owner effects, the same transaction checks exactly the two mandatory
metadata fields and their immutable value mappings against the creation identity.
Extra fields/components, changed semantics and missing mappings roll back owner
effects and keep the gate closed. The owner transaction retains the execution
lock through the atomic contract/receipt/intent/readiness commit.

`TableService.PublishPreparedCreation` supplies a concrete owner finalizer for
an already prepared/executed operation. It validates the canonical original API
request and absent-publication condition, active tenant, original table attributes
and mandatory fields, then writes the create-table audit and tenant revision.
The manager authorizes every invocation, including historical receipt replay.
This method returns the original ingestion publication. The separate
[durable creation workflow](creation_workflow.md) now supplies owner allocation,
immutable original Table/field receipts, transaction-bound intent binding and
original Table-result recovery after publication. Operational reference/installer
orchestration and HTTP/worker wiring remain unfinished. These explicit methods
do not replace runtime Create.

Creation publication acceptance covers restricted execution and grant-denial
recovery, table/trigger/journal/shared-resolver drift, locked account/session
requirements, grant mismatches, owner metadata failures, audit/revision rollback,
authorization/revoked replay and actual lost-COMMIT acknowledgement/recovery.
Production policies, operational installation review, replication/restore and
representative load acceptance remain outstanding.

## Version 3 native changes with retained record guards

`BindRecordChanges` binds supported typed native ALTER operations to an existing
reviewed v2 creation or v3 change. The first before definition must equal that
lineage's last table definition. V3 retains the original creation identity and
both exact-key trigger observations; it does not reinstall or re-journal them.
It also stores the field/mapping snapshot and typed operation descriptors, so
decoding regenerates every SQL statement, step identity and empty-table
precondition. Unknown operations, substituted SQL, scope/mapping/definer changes,
missing guards and changed mandatory record semantics are rejected. V1/v2 reject
these additional fields; existing v1 behavior is unchanged.

Execution checks both retained trigger definitions and the exact trigger set
before any further ALTER, including dirty recovery, and again before verifying.
Lost ALTER acknowledgements use the same immutable per-step journal and actual
shape recovery as v1. Missing/changed/extra guards block work rather than being
silently recreated. Publication requires an existing expected published version,
the complete native step chain and shared/record-definer readiness under the same
execution lock. After owner effects, one bounded field/mapping query verifies the
entire desired snapshot (including typed nullability/unique changes) before the
contract is persisted; extra/missing/mismatched metadata rolls back all effects.

Trigger inspection itself requires TRIGGER privileges. The long-lived native
DDL role deliberately lacks that capability. `RecordInstallerPolicy.NativeChanges`
therefore supplies a separate temporary target SELECT/CREATE/ALTER/INDEX/TRIGGER
policy plus journal rights and dictionary REFERENCES. It has no SET_ANY_DEFINER,
record DML or routine EXECUTE. Actual restricted ALTER and replay are tested.
Close its sessions and lock it before publication; exact inactive policy and no
surviving sessions are mandatory. A creation installer profile cannot substitute
for a native-change profile. No publisher grants privileges or deactivates accounts.

This protocol covers supported native additions/indexes/nullability changes on
guarded record tables. Enum/unique-text/type-change/backfill/retirement protocols,
complete field/index owner orchestration and deployment/load acceptance remain
separate requirements. It does not authorize changing mandatory record columns
or dropping their envelope/key constraints.

`FieldService.PublishPreparedCreation` supplies the concrete native-field owner
finalizer. It binds the retained field UUID and original command using
`dm-prepared-field-creation/v1`, requires an exact prior publication version,
validates the active tenant/table and exact prepared field attributes, and writes
the create-field audit and tenant revision in the publication transaction. The
manager verifies the entire desired metadata/mapping snapshot after these effects.
Replay returns the retained ingestion publication without repeating effects and
requires current authorization. It does not return an original Field result or
implement durable field allocation; those and enum/unique-text creation remain
outstanding. PostgreSQL Create behavior and runtime wiring remain unchanged.

## Atomic owner publication contract

`PhysicalExecutor.PublishVerifiedOperation` joins physical verification with the
caller-owned publication transaction introduced in DM-01.3b.2d. It is an internal,
single-table owner adapter, not an API endpoint or enabled runtime backend.

The required authorization callback runs before acquiring a database connection
or execution lock, on every initial call and historical replay. It must authenticate
the actual caller, bind the request's actor kind/identity and authorize the selected
tenant/table. The adapter then checks the immutable intent's tenant/table/operation,
request hash and actor identity. Only absent/exact publication conditions are
accepted, and the condition must match the intent's expected version. An unconditioned
physical publication is rejected. The hash must come from the versioned canonical
owner command, not a client-supplied fingerprint or the ingestion payload alone.

For an unpublished intent, the existing full journal/shape/trigger/grant verifier
runs under the pinned table DDL lock. The same connection begins READ COMMITTED and
locks model table -> physical registry -> intent. It rechecks closed state, active
operation, epoch, expected version and verifying state. A receipt already present
without published intent is rejected rather than adopted as partial success.
Owner metadata/audit/revision/queue effects, ingestion contract, receipt, published
intent and reopened registry/version then commit together. Final conditional writes
reject changed gate/intent state. Any error rolls back all local effects. Archived
tables are rejected; archive/retirement require a protocol that leaves access closed.

The synchronous owner callback must validate the operation-specific business rules
and correspondence between planned physical schema and desired metadata. The adapter
verifies the trusted plan against actual DDL; it does not invent a missing owner
planner, prove semantic mappings absent from that plan, or supply missing audit/
revision/index effects. It permits no DDL, network calls, nested commit, registry/
intent manipulation or unrelated-table work inside the callback. Multi-table display
cleanup needs its own consistently ordered owner coordinator. Concurrent privilege
administration must remain excluded during verification/publication.

A commit or lock-cleanup failure returns no successful publication. After uncertain
commit, retry the exact request on a healthy connection with current authorization.
If intent is published, immutable receipt replay returns the original result without
rerunning effects or reopening a newer barrier. Historical replay does not attest to
current shape/grants or current record availability; consumers must still use
LockPublished with their expected version for every physical data transaction.
Missing receipts and changed requests remain errors. No automatic retry is performed.

Fourteen MySQL 8.4.8 scenarios verify initial publication, exact-version follow-up,
historical replay over a newer barrier, denied authorization including replay,
changed actor/hash, unverified intent, schema/grant drift, orphan receipt rejection,
archive rejection, owner/receipt/gate rollback and actual lost-COMMIT acknowledgement.
The single-connection pool and explicit IS_USED_LOCK check prove the owner transaction
retains the pinned execution lock. Healthy recovery waits for observed lock release,
not elapsed time as proof of fencing. Unit tests reject missing callbacks, cross-
database composition and invalid identities/conditions before any mutation.

Remaining: concrete platform authorization and business-service bindings; complete
canonical command and semantic planner coverage; multi-table display lifecycle;
production metadata/physical role deployment; server/worker/consumer wiring;
failover fencing and representative resource/load acceptance. Runtime is still
PostgreSQL/River. This adapter does not complete DM-01.3c by itself.

## Original physical Field responses

The prepared native creation, nullability and numeric uniqueness owner paths now
also expose typed Field results through `PhysicalFieldPublicationManager`.
`PublishPreparedCreationResult`, `PublishPreparedNullabilityResult` and
`PublishPreparedUniqueResult` reuse the same canonical owner validation and
business effects as the publication-returning entry points.

The internal result hook runs after the publication receipt exists and before
intent publication/readiness opening, on the same transaction and pinned
execution connection. It writes the v004 immutable Field response together with
owner effects and the gate transition. Result serialization/INSERT failure rolls
back those effects and keeps readiness closed; already applied DDL evidence is
retained. A retry resumes publication, not physical execution.

Historical replay rechecks current authorization and the durable physical command
identity under the execution lock, then reads the retained response on that same
connection. It never reruns owner effects, reconstructs a missing legacy response,
returns today's Field or opens a newer barrier. Wrong Field/tenant/table/kind and
oversized retained JSON fail closed. Ambiguous COMMIT returns no successful Field.

These methods do not allocate a stable field ID before preparation or install new
runtime entry points. Durable field allocation, remaining physical planners and
owners, operational orchestration, lifecycle/retention, API/consumer adoption and
reviewed deployment acceptance remain required.
