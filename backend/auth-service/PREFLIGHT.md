# Existing identity inventory

Implemented first provider-independent increment: `cmd/identity-preflight`.
This is an operator tool, not a login service, directory API or account migration.
It reads existing attribution from the shared database and never creates an
account, assigns permissions, rewrites a subject or applies migrations.

## Run

From `backend/auth-service`, with Go 1.24.2 or compatible newer tooling:

```powershell
go run ./cmd/identity-preflight -list-sources
```

Set `AUTH_PREFLIGHT_DATABASE_URL` through your normal private environment/secret
mechanism to the intended shared database. Prefer an operator role with SELECT
only on `core.tenants` and the selected source table, and schema USAGE. This
command does not grant database access. Do not put credentials in command-line
arguments or commit reports to Git; subject identifiers can contain personal data.
TLS follows the supplied PostgreSQL configuration; use verified TLS remotely.

```powershell
go run ./cmd/identity-preflight -tenant <tenant-uuid> -source inbox_members -limit 200
go run ./cmd/identity-preflight -tenant <tenant-uuid> -source inbox_members -limit 200 -after <next_after>
```

Replace placeholders before running. Continue with the returned `next_after`
while `has_more` is true, retaining the same tenant/source. Repeat for each
source returned by `-list-sources` and each explicitly authorized tenant.
No database connection is made for listing sources or validating invalid inputs.

Each JSON page contains version, tenant ID/status, source, snapshot time,
references (`record_id`, exact nullable `subject`, classification), `has_more`
and optional `next_after`. Classification is intentionally conservative:

- `absent`: SQL NULL; can legitimately mean no assignee or a system event.
- `blank`: empty/whitespace value; preserved for review, not silently discarded.
- `noncanonical`: surrounding whitespace; exact original value retained.
- `unresolved`: every other value, including UUIDs and `legacy_import`. This
  does not mean a verified account exists or that the actor was a human.

## Source scope

The allowlist covers inbox users, current assignees, contributors, case/inbox
event actors, prior/new assignment targets, file uploaders, report creators and
completers, both owners' investigator review actors, screening comment authors
and whitelist reviewers. See [source definitions](internal/preflight/inventory.go).
No comments, report content, evidence, tokens or owner receipt payloads are read.

This is a typed-column inventory, not a recursive search through historical JSON
payloads or an external provider directory. Reviewed mapping, unresolved/conflict
aggregation, additional legacy JSON references and historical aliases remain
AUTH-M05/M06 work. Review receipts are immutable; never rewrite them merely to
make a new identity mapping convenient.

## Safety and completeness

- Connection defaults and each transaction are read-only. One connection, a
  repeatable-read snapshot per page, 1–1000 rows returned, one lookahead row,
  UUID keyset cursor, 5s connection timeout, 2s lock timeout, 30s statement/idle
  transaction limits and 45s overall command deadline bound each invocation.
- Missing tenant, table/column, grants, cancellation or timeout fails with nonzero
  exit and no successful partial page. DB error details/credentials are not printed.
  An empty source is distinguishable from an unavailable source.
- Page snapshots are independent. Pause relevant writers for a migration-grade
  census: a concurrently inserted random UUID can sort before an already-read
  cursor. Snapshot timestamps and paging do not prove cross-page consistency.
- UUID ordering uses existing owner indexes where the planner can use them;
  tenant filtering/sorting may still examine many rows. No owner indexes were
  changed. Measure query plans on representative volumes before a large census;
  bounded output is not a claim of production-scale query performance.
- The tool trusts operator DB privileges; `-tenant` limits the requested scan,
  but is not end-user authentication. Do not expose this executable as a public
  HTTP endpoint. Actual application data was not inventoried in this increment.

## Verification

```powershell
go test ./...
go vet ./...
```

For the real PostgreSQL test, set `AUTH_PREFLIGHT_TEST_DATABASE_URL` to a test
cluster connection whose role may create databases. The test creates a randomly
named disposable database, applies actual Data Model, ingestion, decision,
screening and case migrations, seeds fixtures there and drops only that generated
database. Without the variable the integration test is explicitly skipped.

2026-10-06 verification: `go test ./... -count=1 -v` passed, including
`TestPostgresInventory` (3.04s), and `go vet ./...` passed. Tests cover all 14
source schemas, exact actor preservation, null/synthetic/noncanonical IDs,
assignment history, multi-page results, tenant isolation, missing tenant/source,
cancellation, unsafe scope and configuration-error redaction. An initial
canceled-context connection failure was corrected before this passing run.
No deployment, provider login, migration reconciliation or representative load
acceptance is claimed.
