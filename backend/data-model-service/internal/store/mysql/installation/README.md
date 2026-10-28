# Data-model installation artifacts

## Shared exact-key dependency inspection

`NewExactKeyReadiness` binds a complete reviewed v001 artifact, optionally with
the shipped v002 delivery upgrade, to the exact installation SQL and dedicated
resolver definer. Partial lineages, substituted SQL/order/identities/definers and
unknown upgrades fail construction. The verifier owns a copy of the artifact.
Reference observations are still trusted review inputs, never learned from the
application target.

`VerifyOnConn` reuses the caller's pinned physical execution connection. It
verifies the complete applied migration history and every final installed shape,
then exact locked resolver-definer grants, absence of existing definer sessions,
and absence of triggers on either dictionary table. Missing history, dirty steps,
checksum differences, object drift, extra/missing grants, mandatory roles and
unlocked accounts fail closed. Performance Schema and privileged catalog visibility
are required; locking an account does not terminate an already connected session.
No inspection result is cached and no DDL, grant repair or gate mutation occurs.

The caller must retain its table execution lock and exclude shared installation
and privilege administration through subsequent publication. This method does
not acquire a database-wide lock or additional pool connection. Verification is
bounded by the reviewed 80/81-step artifact; representative inspection latency
and deployment resource budgets remain unmeasured.

`PhysicalExecutor.WithCreationReadiness` explicitly binds this inspector and a
record-installer policy to v2 publication. Unconfigured executors still reject v2.
The configured verifier checks all table/trigger definitions and journal entries,
this shared lineage, the locked record definer's exact grants and absence of live
sessions, and the inactive installer before invoking the atomic owner callback.
The table execution lock remains held through publication commit. This staged
path does not configure application startup or approve a production deployment.

MySQL 8.4 requires `TRIGGER` on the target and `SET_ANY_DEFINER` to create a
trigger naming another existing account. `SET_ANY_DEFINER` is a global capability,
so it must not be smuggled into the ordinary object-only `AccountPolicy` contract.
With binary logging enabled, trigger creation also requires the documented
`log_bin_trust_function_creators` configuration or additional administrative
authority. This implementation never changes server settings. The disposable
Compose fixture explicitly uses trusted creators and ROW logging for restricted
installation tests. Production configuration review and replication/restore
acceptance remain required. See MySQL's
[trigger creation requirements](https://dev.mysql.com/doc/refman/8.4/en/create-trigger.html),
[definer rules](https://dev.mysql.com/doc/refman/8.4/en/stored-objects-security.html)
and [binary logging restrictions](https://dev.mysql.com/doc/refman/8.4/en/stored-programs-logging.html).

### Temporary record installer

`RecordInstallerPolicy.ActiveGrants` renders the exact temporary policy for one
registered record table: SELECT/CREATE/TRIGGER on that table, REFERENCES on the
dictionary, and narrowly scoped registry/intent/journal permissions needed by the
durable executor. MySQL 8.4.8 rejected column-only REFERENCES(id) in the real
creation test; table-level REFERENCES is required. No dictionary DML/SELECT,
record DML, routine EXECUTE, SUPER, SYSTEM_USER, ALLOW_NONEXISTENT_DEFINER or
GRANT OPTION is granted. The separate SET_ANY_DEFINER capability is global and
cannot be constrained to one definer; ordinary AccountPolicy remains object-only.

Deployment must isolate the installer credentials, provision existing locked
definers, review/apply the exact active grants, and inspect them using
VerifyActive before execution. With binary logging enabled, that inspection
requires trusted creators and ROW format. After execution, close installer
connections, lock the account and revoke SET_ANY_DEFINER. VerifyInactive requires
the remaining exact object grants, ACCOUNT LOCK and no surviving sessions before
publication. Any additional privilege or old session keeps readiness closed.
These helpers do not provision, unlock, revoke, terminate sessions or silently
repair accounts. Exclude concurrent account and shared-schema administration
through execution/publication; the table lock does not fence administrators.

Real-engine tests execute all three steps and replay as this restricted account,
reject missing REFERENCES/SET_ANY_DEFINER/TRIGGER, recover after explicit grant
restoration, and reject direct record/dictionary/routine bypasses. Publication
tests cover shared/record/consumer grant drift, active or overprivileged installers,
live sessions, malformed metadata, owner rollback and lost publication commit.
Production principal deployment and end-to-end owner preparation remain open.

## Structural artifacts

### Creation receipts (v003)

The separate 85-step `testdata/mysql-reference-fraud-v003` artifact preserves the
entire frozen v002 prefix and appends the immutable table-creation receipt table
and three guards. `ExtendCreationReceiptPlan` binds every appended step to source
and requires initially absent objects. A dedicated locked receipt definer receives
SELECT/TRIGGER only on this table; its grants never widen the shared resolver's
frozen policy. The fixture builder's `-creation-receipt-definer-user` option emits
separate account/grant SQL and source/checksum provenance marked needs_review.

Use `NewExactKeyReadinessWithCreationReceipts` to bind this lineage and both
definers. It verifies all 85 steps plus exact receipt grants, lock/session state
and the complete receipt trigger set. Older dependency constructors deliberately
reject v003. Provision and verify the receipt definer through the reviewed
account-policy workflow as well as the shared definer; generic migration verify
without that additional policy does not attest its privileges. See the
[durable owner workflow](../creation_workflow.md) for allocation, result recovery
and retention requirements. The artifact is not production deployment approval.

This package supplies the **frozen base structural lineage and explicit append-only
upgrades** to the migration runner. It is not an application startup installer or a completed
production deployment. All objects remain in the selected shared database.

`Statements(database, definer)` returns ordered versioned steps for exact keys,
metadata, delivery, display, physical registry, intents, step journals and guards.
The cyclic registry/active-intent foreign key is a separate ALTER with its own
before/after definition. Every function, procedure and trigger names a dedicated
definer explicitly; it cannot accidentally inherit the installer identity.

Capture complete `migrations.Definition` observations before and after each step
on a fresh isolated MySQL 8.4.8 reference server. `Plan` binds those observations
to the exact database, definer, SQL, IDs, order and intermediate object chain.
Serialize that plan as JSON and review it before supplying it to `migrate-mysql`.
Never capture the desired definition from an application target. Missing or
changed reference steps are rejected. Changing source does not upgrade an
installed lineage: retain the original reviewed artifact and append separately
reviewed upgrade steps. Current schema definitions must not overwrite installed
checksums. `ExtendPlan` appends reviewed upgrade steps while preserving the old
artifact, rejecting changed definition chains and adoption of unjournaled objects.

`cmd/build-mysql-reference` automates capture exclusively on the explicitly
configured disposable MySQL 8.4.8 Compose fixture. It creates new reference objects,
verifies the definer grants and cleans them up before writing a new artifact
directory. It refuses existing database/account/output names and other servers.
For an existing output parent directory, run inside the fixture:

```
go run ./cmd/build-mysql-reference -database fraud -definer-user data_model_guard -definer-host localhost -output /path/to/new-artifact
```

The [frozen reference artifact](../../../../testdata/mysql-reference-fraud-v001/plan.json)
uses the root Compose shared database name `fraud`, contains 80 steps and is marked
`needs_review` in provenance. The definer SQL and plan checksum accompany it. Tests
bind its bytes/SQL/grants to source and prove fresh installation/replay. Git
attributes preserve checksum-bound SQL and artifact bytes across platforms.
This is a concrete review input, not approval to deploy or rewrite old history.

`CreateDefiner` emits CREATE USER ACCOUNT LOCK without credentials or IF NOT
EXISTS. It refuses to silently adopt an existing principal. Provision the locked
account before objects; apply `DefinerGrants` after structural installation and
before allowing application traffic. Grants list individual owner tables and
routines, with no global/database-wide DML, DDL, DELETE or GRANT OPTION. BEFORE
triggers need UPDATE to assign dictionary keys; the dictionary routine receives
only the SELECT/INSERT/UPDATE privileges its statements need.

`VerifyDefiner` uses privileged installer visibility to require the account to
remain locked and SHOW GRANTS to equal the exact allowlist. Missing routine
permissions, excess permissions, roles and unlocked accounts fail closed. Run
under operational exclusion of concurrent privilege changes. This verifies only
this dedicated identity: it is not a complete runtime/DDL/consumer grant policy.
Account/grant provisioning is separate from the schema runner journal and is
not advertised as a transactional account installer.

`AccountPolicy`, `GrantStatements` and `VerifyAccountPolicy` support explicit
runtime/DDL object allowlists and reject extra privileges, roles and wrong account
lock states. Mandatory server roles are outside this direct-grant contract and
are rejected by both account and definer verification. Concrete production
principal policies and credential deployment remain required.

Column grants are represented by `ObjectGrant.Columns`, mapping SELECT, INSERT,
UPDATE or REFERENCES to explicit column identifiers. Table-wide and column grants
for the same privilege cannot coexist in a reviewed policy. Empty, duplicate,
invalid or excessive column lists fail validation; the total rendered policy is
bounded to 1 MiB, including USAGE. Grant rendering sorts columns without mutating
the input. Verification normalizes only column-list ordering in SHOW GRANTS;
additional columns, table-wide rights, other objects, roles, grant options and
missing privileges still fail closed. Revoking a table-wide privilege can also
remove its column grants: explicitly reapply and verify the reviewed policy after
such an administrative change. These checks never broaden privileges automatically.

`migrate-mysql -action verify -account-policies accounts.json` optionally verifies
additional reviewed accounts on the same pinned connection under the migration
lock, after complete schema-lineage and locked-definer verification. It performs
no account creation, GRANT, REVOKE or repair. Supply the usual plan, definer and
connection flags. With no account file, verification covers only the schema and
definer; output states the number of additional accounts inspected. An omitted
principal is not implicitly verified. Exclude concurrent privilege administration;
the migration lock does not serialize arbitrary external GRANT statements.

The JSON file has format `dm-account-policies/v1`, a `database` exactly matching the
migration plan, and 1..64 `accounts`. Each account has `user`, `host`, `locked` and
`objects`; each object has `kind`, `name`, `privileges` and optional `columns`.
For example, a locking-read policy entry is:

```json
{"kind":"TABLE","name":"data_model_index_jobs",
 "privileges":["SELECT"],"columns":{"UPDATE":["id"]}}
```

This example is not a complete worker role. The file and combined rendered grants
are bounded to 1 MiB; duplicate keys/accounts, unknown fields, unsupported grants,
trailing documents and excessive nesting are rejected. `-action validate` accepts
the same file for offline validation only. The `install-grants` action also
accepts it as described below. Other actions reject the flag rather than silently
ignoring it. Credentials remain in the deployment secret workflow.

## Installing reviewed account grants

`migrate-mysql -action install-grants -account-policies accounts.json` uses the
normal plan, definer, connection and TLS flags. It verifies the entire installed
schema lineage and locked-definer grants before installing any account grants,
using the same pinned connection under the migration lock. This explicit operator
action is never invoked by application startup.

The file must name existing, locked, dedicated accounts, distinct from the
installed guard definer. Provision accounts and credentials through the deployment
secret workflow first. Every policy is validated and every account inspected
before the first GRANT. Existing direct grants must be either complete matching
object grants or absent; an unexpected privilege, role, grant option, different
partial object grant, unlocked account or missing account rejects the batch.
This is installation/resumption, not automatic repair of arbitrary account drift.

The installer also requires enabled Performance Schema and visibility of its
threads table. Accounts with existing sessions are rejected: locking an account
does not terminate old sessions. The check conservatively rejects sessions with
the same username at any host. Operationally exclude concurrent account/privilege
administration throughout installation; the migration lock cannot serialize
unrelated administrator commands. Retain dedicated account ownership and review
any definer capabilities before assigning privileges.

GRANT statements commit independently. A failure reports the affected account and
statement, may leave a prefix applied, and never prints success. Replay the same
reviewed policy after resolving the reported failure; exact completed object
grants, including an acknowledged-by-server but lost-to-client GRANT, are accepted.
The installer never revokes unexpected permissions or changes an account lock or
credential. Full exact-account verification follows installation. Accounts stay
locked even on success; deployment authorization, unlocking and runtime readiness
remain separate requirements. Keep the reviewed policy file with deployment
evidence; this operation does not add a transactional account-history journal.

After provisioning the locked definer, applying the plan and applying its grant
file, `migrate-mysql -action verify -plan plan.json -definer-user data_model_guard
-definer-host localhost` (with the normal connection/TLS flags) requires the full
applied lineage, exact final object definitions and exact definer grants while
holding the migration lock. It performs no DDL or repair. Missing, partial, dirty
or drifted installations fail. A disposable CLI rehearsal rejected verification
before grants, passed after grants and replayed installation successfully.

Verification installs a reference lineage, rebuilds the disposable database,
applies the prefix before the cyclic FK, resumes the complete lineage and replays
it. Restricted login writes exercise the locked-definer guard and exact-key
routine while direct dictionary reads, journal writes, DDL and direct routine
calls are denied. The fixture also rejects broad grants, missing routine grants,
unlocked definers and immutable identity changes. No application database is used.

Remaining production gates: deployment-specific reviewed artifacts and upgrades,
preflight/backfill and remaining business invariants, complete installer/runtime/
DDL/consumer role matrix, credential lifecycle and operational installation
acceptance. Physical-model grants and readiness belong to the owner protocol.

## Guarded native record changes

The separate `DisplayOwnerPolicy` renders display settings/dependency mutation,
caption job production and immutable publication receipt insertion privileges.
It excludes physical DDL, dictionary access, gate/contract mutation and receipt
update/delete. Reservation locking requires column UPDATE on the immutable
tenant identity; installed identity guards remain mandatory. The restricted-login
acceptance runs after complete v003 installation and verified physical creation.
This profile does not complete the production principal matrix or enable runtime.

For guarded record-table native changes, `RecordInstallerPolicy.NativeChanges`
renders a temporary per-table ALTER/INDEX/CREATE/SELECT/TRIGGER policy and narrow
registry/journal grants. TRIGGER is required to inspect existing trigger
definitions; testing with the old native DDL account failed with MySQL 1227.
This profile never grants SET_ANY_DEFINER. Publication requires the exact account
policy, ACCOUNT LOCK and no surviving sessions. Initial creation uses the
separate profile with temporary SET_ANY_DEFINER, revoked before publication.
Neither profile widens long-lived runtime roles or provisions accounts itself.

## Delivery retry upgrade (v002)

`DeliveryRetryUpgradeStatement` supplies one additive ALTER for the delivery
`error_count` column. `ExtendDeliveryRetryPlan` binds isolated before/after
definitions to that exact SQL and appends it without rewriting any v001 step.
`Statements` and the frozen v001 artifact retain their original 80 steps. The
separate [v002 artifact](../../../../testdata/mysql-reference-fraud-v002/plan.json)
contains the same prefix followed by the upgrade, with separate SHA-256 provenance
and `needs_review` status. The fixture-only builder accepts
`-delivery-retry-upgrade` to reproduce the expanded lineage in a new directory.
The normal migration runner applies, verifies and replays this appended plan.

Existing rows deliberately receive SQL NULL (unknown history). A policy-aware
enqueue writes zero; neither migration nor adapter infers error history from
claims. The CHECK permits NULL or a nonnegative count no greater than the delivery
attempt. Before switching workers, drain old writers and explicitly translate or
retire existing deliveries. Unknown-history rows fail before policy-aware claim
or rescue. A reviewed translation must preserve River error count independently
of attempts, job identity, schedule and retained dedupe. That translation and
deployment procedure remain open; setting every existing count to zero is unsafe.

Column-restricted worker accounts need explicit SELECT/UPDATE on error_count and
INSERT permission for policy-aware enqueue. The upgrade does not broaden grants.
All worker/maintenance/redrive producers for a queue must use one reviewed policy
configuration; the legacy fixed-delay adapter remains for staged compatibility,
not a supported mixed-policy rollout. Production data preflight, upgrade-lock
duration and deployment acceptance remain required.

## Owner result upgrade (v004)

`OwnerResultUpgradeStatements` appends a tenant/table/publication-bound JSON
result table and three immutable/integrity triggers. Its separate locked definer
has only SELECT/TRIGGER on that table. `ExtendOwnerResultPlan` requires the v003
prefix and exact generated source SQL, IDs, kinds, names, order and absent-before
observations. The frozen v001-v003 history is preserved.

The isolated reference builder accepts `-owner-result-definer-user` together with
`-creation-receipt-definer-user`; all three dependency identities must differ.
The [89-step artifact](../../../../testdata/mysql-reference-fraud-v004/plan.json)
is source-bound with SHA-256 provenance and remains `needs_review`.
`NewExactKeyReadinessWithOwnerResults` verifies the complete applied lineage and
result guard account/trigger set under the existing execution lock. Older
constructors reject this expanded lineage. Account provisioning, production
policy review and activation remain separate gates.

Display result owners use `DisplayResultOwnerPolicy`: the existing narrow display
profile plus SELECT/INSERT on `data_model_owner_results`. UPDATE/DELETE are not
granted. Retained results cannot be deleted to unblock retirement; a reviewed
retention protocol is still required. Pre-v004 publication-only history has no
original settings result and is never backfilled from current settings.

## Presentation owner capabilities

`PresentationOwnerPolicy` is the explicit Table/Field presentation profile. The
metadata publication coordinator uses narrow column assignments and rejects
changes to caption, archive, identity and field enforcement before issuing its
UPDATE. Original Table/Field responses share the immutable v004 result table;
no additional migration or broader receipt privileges are required.

The profile includes bounded metadata/contract/gate reads, presentation-only
updates, audit/revision/result inserts and existing caption-job producer grants.
It grants no DDL, dictionary, contract/gate mutations or receipt UPDATE/DELETE.
Object grants do not replace tenant authorization or approve production account
composition. Runtime wiring and operational acceptance remain separate gates.
