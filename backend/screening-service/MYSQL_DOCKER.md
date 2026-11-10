# Local MySQL screening runtime

The application uses the shared MySQL `fraud` database. The existing screening
publication/intake schema remains the exact migration prefix. Native API and
worker entrypoints are separate from the existing PostgreSQL binaries.

The local API runs at `http://localhost:8085/docs`. The native API and worker use
restricted MySQL accounts and run as non-root containers. The retained runtime
smoke verifies an auth-issued session, request replay, a worker-persisted match,
and browser rendering. Consult the endpoint/worker checklists for test acceptance.

The default matcher uses only the synthetic names Alex Example, Morgan Sample
and Example Trading Limited. Responses/capabilities explicitly identify the
local development backend. It provides no sanctions-provider coverage.

Human routes require an auth-issued access JWT and current investigator/case
permissions. Legacy owner routes require the private operator header plus the
service bearer token. Internal integration routes use a distinct intake token.
Tenant registration/activation remains checked; the service is not bound to one
tenant. Keycloak keeps its existing storage.

Generated secrets/configuration stay in ignored `.local/mysql-dev`. The API
receives only auth's public signing key. Application roles use restricted MySQL
accounts and capped pools; neither role has DDL/root credentials. The installer
alone receives the root secret and target/reference sockets.

Disposable tests use `compose.mysql-test.yaml`, two independent MySQL 8.4.8
servers and a private PostgreSQL server, with no published database ports or
application data volume. Native worker polling derives durable work from domain
rows rather than importing PostgreSQL River jobs.

Historical data/queue transfer, real provider configuration, production
performance/recovery and frontend cutover require separate acceptance.

## Setup

From `backend/screening-service`, with the existing MySQL data-model, ingestion,
decision, auth and case setups installed:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/setup-mysql.ps1
```

The script preserves local credentials, builds the native image, verifies the
append-only owner migration against an independent MySQL 8.4.8 reference, and
starts only the screening API/worker. The application database is not recreated.
After the first screening installation, run the case-manager setup again to grant
its API the new narrow screening read contracts and deploy its integration code.
The API/worker each cap their pool at eight connections, adding a combined maximum
of 16 to the shared database budget of 100. This is a development configuration;
capacity and contention still require representative measurement.

For manual screening, authorize Swagger with an existing auth access JWT carrying
case/investigator permissions. Tenant identity comes from that session. Private
owner endpoints additionally require `X-Screening-Operator-Token` and the separate
service bearer credential. Internal intake uses its own credential. Local secret
files are generated under ignored `.local/mysql-dev`; never copy them into docs.

## Verification

From this service directory:

```powershell
docker compose -p screening-mysql-test -f compose.mysql-test.yaml run --rm test
node scripts/summarize-mysql-tests.mjs
node scripts/smoke-mysql.mjs
node scripts/update-mysql-status.mjs
```

The complete test script builds fresh PostgreSQL API/worker binaries for source
journeys as well as testing native MySQL. It requires zero skipped/missing tests,
race warnings or failures, plus vet. The HTTP report inventories 50 native mounted
operations, including documentation and the two local evidence capability routes.
The source router has 47 operations; Swagger displays 47 documented operations.
Positive HTTP coverage does not establish every business branch or production load.

The smoke creates one synthetic manual screening, checks idempotent replay and
waits for the actual Docker worker to persist its match. It uses the existing
development auth session without printing or saving the JWT. Set
`SCREENING_SESSION_FILE` to an alternative private opaque-session file if needed.
Browser verification requires the frontend Playwright dependencies and access to
Swagger's configured CDN. Tests use private tmpfs databases; release them with
`docker compose -p screening-mysql-test -f compose.mysql-test.yaml down` afterward.

## Worker and integration boundaries

Native workers poll durable pending rows and use fenced claims for dispatch,
continuous screening, dataset jobs and callback delivery. Provider/HTTP calls run
outside SQL transactions. Publication is reconciled through the decision owner's
SQL contract and durable projection queue. Dataset rescreening now advances in
50-object keyset pages. Each page commits its requeues and checkpoint together,
and the provider delta snapshot is fetched once and retained for recovery. Only
objects created by the job's cutoff are considered; current configuration, state
and provider are rechecked. Tests exercise 625 eligible objects, interrupted
work and expired claims. Representative production capacity remains unverified.

The existing decision private provider still owns screenings whose provider is
`local`. The standalone screening worker excludes those rows. Installing this
runtime does not reroute that dispatch traffic or replace its configured local
watchlists with the three-name synthetic catalog. A provider handover requires
separate end-to-end acceptance.

Case manual investigations, review/arrival callbacks and imported evidence use
authoritative screening contracts. The MySQL case `evidence-uploaded` callback
verifies the source publication, imports the file, verifies bytes/SHA-256 and
commits its audit, notification outbox and intake receipt atomically. Stable
event IDs and the case lock prevent duplicate files from callbacks or manual
imports. A missing case link or unfinished upload remains a visible retry.
Callback delivery uses stable event identities, expiring leases, retry/backoff
and terminal failure after 12 attempts. Replay one terminal callback explicitly:

```powershell
docker compose --env-file ../../.local/mysql-dev/compose.env -f docker-compose-mysql.yml run --rm --no-deps screening-worker /app/mysql-api --worker --tenant <tenant-uuid> --replay <event-uuid>
```

Replay is an operator action. Fix the receiver before replaying failed work;
receivers must deduplicate the stable event ID. Generic case outbound notification
publishing remains separately disabled until its publisher is configured.

The HTTP and worker integration fixtures use the installer's exact restricted
account policy and verify that DDL is denied. The policy grants the API only the
additional match deletion needed for retry and a no-op outbox identity update;
the worker can create continuous screening rows and lock the source view used by
decision reconciliation. No runtime account receives database-wide DDL grants.
