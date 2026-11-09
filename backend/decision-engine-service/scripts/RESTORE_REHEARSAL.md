# Isolated logical restore rehearsal

Run from the repository root with Docker available:

```powershell
./backend/decision-engine-service/scripts/test-restore.ps1 -EvidenceDirectory C:/temp/decision-restore-new
```

Add `-ReleaseChecks` to also run frozen-artifact validation, fresh and
115-to-140-to-145-to-current upgrade recovery, API/worker privilege boundaries,
and broken-relationship rejection before restoring. The runner fails on missing
required tests, failures or skips, and saves raw results with cleanup status.

The evidence directory must not already exist. The runner creates private disposable
MySQL servers with no published ports, uses generated fixture schemas, and removes
its containers and volumes on exit. It refuses an existing `.restore-rehearsal`
exchange directory; inspect a leftover directory before attempting another run.
No application database URL is accepted. Do not substitute application credentials
or server names into this test.

The test exercises this sequence:

1. Persist synthetic decision/effect/job data and an actual case workflow receipt.
2. Stop fixture writes and capture exact schema definitions, counts and checksums.
3. Install the same frozen owner migration plans on a new private server. Preserve
   original definer identities and install their required grants.
4. Prepare the offline empty target for importing original ledger and application
   rows. Temporarily remove only known guard triggers on that isolated target.
5. Take a consistent data-only dump and import it with binary mode enabled.
6. Recreate every original guard, verify all original owner migration ledgers,
   compare all captured definitions/counts/checksums, and check enabled constraints.
   Inspect every declared foreign-key relationship, including composite tenant
   keys, for imported orphans. This is an offline full-data check with a deadline;
   it is not a frequent readiness probe or a check of relationships absent from DDL.
7. Install restricted runtime grants separately. Reconcile and replay the retained
   receipt through the actual case HTTP handler; assert unchanged data and denied
   receipt deletion.

A full schema dump was found to change definition rendering in this fixture and
failed strict verification. Retain frozen migration artifacts; do not replace
expected definitions with whatever the restored server returns. MySQL documents
data-only dump options in [mysqldump](https://dev.mysql.com/doc/refman/8.4/en/mysqldump.html)
and line-ending preservation in [mysql binary mode](https://dev.mysql.com/doc/refman/8.4/en/mysql-command-options.html).

This is supporting release evidence, not an executable production restore plan.
Production needs reviewed immutable artifacts, full owner inventory and relationship
validation, protected backups, coordinated writer/worker fencing, separately restored
identities/grants, volume/timing measurements and the selected PITR/failover procedure.
Do not expose a target while restore-time guards are absent. Do not start target
workers before old writers are fenced and delivery receipts reconcile. PostgreSQL
remains the active runtime until all production gates are satisfied.
