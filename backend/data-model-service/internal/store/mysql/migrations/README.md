# Data-model MySQL migration runner

Status: verified runner and separate executable. The [installation package](../installation/README.md)
now binds the staged structural lineage to reference definitions and supplies
restricted definer provisioning/grant verification. Deployment-specific reviewed
artifacts, complete role policies and physical-model protocols remain open.
No runtime constructor calls this package. The PostgreSQL migrate command is
unchanged. This package uses `data_model_schema_migrations` in the selected shared
application database, not a separate service database.

## Reviewed plan

`Plan` contains an explicit database and 1..10,000 ordered `Step` values. Each
step has a stable ID, object kind/name, one complete CREATE/ALTER statement and
exact before/after definitions. Routine bodies are never split on semicolons.
Empty Before means absent. After must be nonempty. Drop operations and forced
version changes are intentionally unsupported; retirement requires its own
reader/data/rollback protocol.

Definitions are canonical JSON returned by `Definition` on a clean reference
installation of the pinned MySQL version. They include the complete SHOW CREATE
row: constraints, index definitions, table options, routine definer/security,
SQL mode and character-set context. Trigger creation timestamps are excluded.
Ship and review reference definitions with the SQL; never learn the desired
definition from a production target. Each SQL/definition is bounded to 1 MiB;
the executable bounds the whole artifact to 32 MiB. SHOW CREATE is a one-object
installer inspection, not an application request API.

Plans are trusted deployment code, not an SQL sandbox. Review every statement
for the intended owner, shared database, object, privileges and dependencies.
No user-provided SQL or physical identifier may reach this interface. The current
runner consumes plans built by the installation package; it does not install
roles. `metadata.Definitions` alone does not contain reference observations or
the complete deployment privilege policy.

## Journal and recovery

One database-scoped named lock is held on the same pinned connection for all
inspection, autocommit journal writes and DDL. This serializes only migration
installers, not application model mutations. It does not fence a former primary;
operational failover must fence old writers before running recovery.

The runner checks the ledger's complete table definition, a contiguous history,
ordered IDs and SHA-256 checksums binding the database and every step field.
Changed, reordered, missing and unknown history is rejected. Repeated-object
steps must form an exact definition chain. Before continuing, the latest applied
definition of every completed object is checked for drift.

For each new step, verify Before, persist a dirty journal row, execute DDL, verify
After, then mark applied. No metadata transaction encloses DDL. A dirty step
matching Before may execute again; one matching After is finalized without
replaying DDL. Anything else fails visibly. An unjournaled preexisting object is
not adopted, even when its shape matches After. Errors leave history available
for inspection; ambiguous journal/DDL acknowledgements are not success. There
is no automatic compensation, blind DDL retry, or force-clean command.

Exact definitions also detect legitimate changes such as AUTO_INCREMENT counters
and changed definers. Such mutable definitions need a separately reviewed
normalization/verification contract before use; the runner does not silently
strip them. Alterations involving multiple objects need explicit steps and
reviewed intermediate states. This is not the physical-model publication barrier.

## Executable

`cmd/migrate-mysql` is separately packaged as `/app/migrate-mysql`. Use `-help`
for explicit connection/TLS/deadline flags. The password is read from an environment
variable, never from a command-line value. Verified TLS is required outside the
explicit isolated `mysql:3306` test fixture. The command opens only the plan's
existing database, with a one-connection installer pool.

- `-action validate -plan reviewed-plan.json`: validate artifact structure without
  connecting; this does not prove SQL correctness or production readiness.
- `-action status ...`: verify ledger shape/checksums and print history without
  installing anything. Missing ledger returns empty history.
- `-action up ...`: apply or reconcile the reviewed plan, then print history.
- `-action verify ... -definer-user NAME -definer-host HOST`: require complete
  applied history, exact final definitions and the locked definer grant allowlist
  under the same migration lock. No DDL or dirty-state repair is performed.

Grant the installer only privileges needed by its reviewed steps and journal.
Runtime accounts must not mutate this journal or issue owner DDL. Complete role
installation and restricted-account bypass tests remain required before adoption.

## Verification

MySQL 8.4.8 tests cover fresh install, ALTER upgrade, replay, changed checksums,
short history, unjournaled objects, applied/dirty drift, failed-DDL restart,
post-DDL recovery, cancellation, competing installer locks and ledger tampering.
Separate tests cover views, procedures, functions and triggers. A wire fault
consumes a real successful DDL acknowledgement before hiding it: history remains
dirty, and a healthy runner inspects and finalizes without replaying CREATE.
CLI validation rejects malformed/unknown/trailing input and unsupported down.
Module-wide vet, selected race regression, command build and help passed.
