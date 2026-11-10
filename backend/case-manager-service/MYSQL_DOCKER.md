# Local MySQL case management

The case API uses the shared `fraud` MySQL database and listens on
`http://localhost:8086/docs`. It reuses the existing case service and handlers,
with native MySQL repositories. PostgreSQL entrypoints remain available.

## Start

First complete the existing data-model, ingestion, decision and auth MySQL
setups. Case management depends on their registered tenants, decision tables,
identity sessions and screening publication tables. Keycloak keeps its existing
storage; this setup does not migrate Keycloak.

From the repository root:

```powershell
.\backend\case-manager-service\scripts\setup-mysql.ps1
```

The script preserves generated credentials under ignored `.local/mysql-dev`,
reuses the auth **public** key, builds the API, verifies the case owner upgrade on
the independent MySQL reference, applies it to the shared database, provisions
restricted accounts, and starts the API and worker. Existing cases and workflow
receipts retain their identities. No historical PostgreSQL data is copied.

The API and worker run without root/database-owner credentials. Their pools are
limited to eight connections each, adding a maximum of 16 to the shared database
budget. API/worker CPU limits are 1/0.5, with 256 MiB per process.

The case owner extends its existing `case_manager` ledger. Its published access,
workflow and review DDL remains an exact prefix. The decision installer uses the
complete composition after installing screening prerequisites. A compatibility
trigger keeps the existing workflow provider's ensure-tag-membership upsert
working after a tag link has been removed.

The reference server is disposable, but its prerequisite owners must be present
when capturing an append-only upgrade. If it has been recreated, restore the
retained prerequisite migration artifacts on that independent server first.
The installer rejects missing or differing reference definitions; it never
adopts the application's observed schema as a replacement artifact.

## Authentication and tenants

Use an auth-issued **access JWT** in Swagger's `InvestigatorSession` authorization.
The existing auth `POST /internal/session` resolves an opaque session and issues
that JWT; it requires auth's proxy credential. An opaque session or proxy token
is not itself a case access JWT.

The token's tenant must match the URL. Current user, membership, session expiry,
revocation and authorization version are checked against MySQL. Users need
`case_manager_admin` or `case_investigator`; investigators also need inbox
membership. Inbox administration requires `case_manager_admin`.

The API supports every tenant allowed by the authenticated session. It does not
seed or bind the API to one tenant. Service integration requests use the separate
ignored `case-service-token.txt` credential. By default, their requested tenant
must be active in the identity registry. To restrict integrations further, set
`ServiceTenantMode` to `allowlist` and provide `ServiceTenants` in
`.local/mysql-dev/case-runtime/config.json`, then restart the case containers.

## Example workflow

Replace `{tenantId}` with the tenant in your access token. As a case administrator,
create an inbox with `POST /v1/tenants/{tenantId}/inboxes`:

```json
{"name":"High risk transfers"}
```

Use the returned inbox ID in `POST /v1/tenants/{tenantId}/cases`:

```json
{
  "inbox_id":"<returned inbox UUID>",
  "name":"Investigate a large transfer",
  "type":"decision",
  "decision_ids":[]
}
```

Decision IDs, when supplied, must be existing decisions in that tenant. Their
object/scenario metadata comes from the decision owner. Add a comment with
`POST /v1/tenants/{tenantId}/cases/{caseId}/comments`:

```json
{"comment":"Review source of funds and destination account history."}
```

Evidence follows start-upload, upload-content, finalize, then download. Uploads
accept supported MIME types and 1 byte–10 MiB. Report completion requires a title,
subject, narrative and activity start/end; report changes require the current
version. Raw storage keys are not accepted as uploaded evidence.

Queue and relationship endpoints support cursors. Full detail collections have
explicit bounds and reject overflow instead of silently truncating it. Use
`/cases/{caseId}/links/{kind}` for paginated events, decisions, screenings or files.
Explorer reads registered, published physical tables and active mapped fields;
archived/internal columns are excluded. Rerun setup after publishing additional
physical tables to refresh the API account's explicit SELECT grants.

## Workers and delivery

The native maintenance loop expires unfinished uploads, wakes snoozed cases and
assigns eligible cases within inbox/member capacity. Multiple workers coordinate
through short transactions and `SKIP LOCKED`; eligible domain rows survive a
restart without depending on PostgreSQL River jobs.

Case events and their pending notifications commit atomically. Inbox events have
their own outbox because the existing case outbox enforces a case-aggregate FK.
Delivery uses 60-second leases, fenced completion, stable event idempotency keys,
bounded retries and terminal failure after 12 failed attempts. Network calls take
place after the claim transaction commits.

`PublisherURL` defaults to empty. In that configuration delivery remains disabled
and notifications stay pending. A configured publisher receives a JSON delivery
envelope, an `Idempotency-Key` equal to the event UUID, and the separate generated
`case-publisher-token.txt` bearer credential. The recipient must deduplicate that
identity. A successful HTTP response acknowledges delivery to that recipient;
it does not establish completion of the recipient's business processing.

Operator replay preserves an event's idempotency key and audit history. With the
case compose configuration, run the worker binary using `--worker --replay
<event UUID> --tenant <tenant UUID>`; add `--inbox-outbox` for an inbox event. Only
terminal `sent`/`failed` records without a live lease can be replayed.

## Verification and remaining dependencies

Run the disposable fixtures, including PostgreSQL source characterization:

```powershell
docker compose -p case-mysql-test -f backend/case-manager-service/compose.mysql-test.yaml run --rm test
cd backend/case-manager-service
node scripts/summarize-mysql-tests.mjs
```

The test setup has no published database ports and uses temporary database
storage. It verifies independent server UUIDs and generated fixture database
names. Evidence includes the test inventory, race/vet results and successful HTTP
operation inventory. To release fixture resources afterward, run the same compose
project with `down`; retain named Go caches if you want faster subsequent runs.

With the application services running, verify auth, tenant isolation and rendered
Swagger using `node backend/case-manager-service/scripts/smoke-mysql.mjs` from the
repository root. It resolves the existing decision development credential through
auth and performs read-only case checks. Set `CASE_SESSION_FILE` to an alternative
private opaque-session file when needed. Swagger's UI assets require network
access to its configured CDN. To regenerate the status documents after passing
tests and smoke checks, run `node scripts/update-mysql-status.mjs` from this
service directory.

The endpoint and worker status files distinguish passing behavior from gaps.
Manual/continuous screening provenance and screening-file imports now use the
verified screening-owner contracts. Re-run case setup after screening installation
to grant the case API its narrow read access. MySQL evidence callbacks now verify
the owner's publication and actual bytes, then import evidence and a stable
receipt atomically. Repeated callbacks and manual imports return the same file.
Two MySQL routes still return `501`: AI case review and internal auto-assignment. Automatic
assignment itself runs in the maintenance worker. No throughput or latency target
is claimed by the behavioral tests. Production data transfer, deployment and
frontend cutover are separate work.

The PostgreSQL evidence callback keeps its existing `501` until its owner adapter
implements the same publication contract. MySQL callback acceptance does not
change that legacy deployment's behavior.
