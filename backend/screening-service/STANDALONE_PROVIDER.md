# Standalone screening provider

The provider can run with its own PostgreSQL catalog, machine-client credentials and API, without installing the fraud platform, Keycloak, cases, decisions or River. It uses the same source import, matching, publication and evidence code as the integrated screening service.

This is an independently deployable engineering delivery. It does **not** establish commercial source rights, production matching quality or production capacity. See [remaining acceptance gates](INTERNAL_PROVIDER_ACCEPTANCE.md) and [delivery checklist](STANDALONE_PROVIDER_PLAN.md).

## Start locally

From the repository root, with Node.js and Docker Compose installed:

```powershell
node docker/setup-provider.mjs
```

Open **http://localhost:8091/docs**. Swagger assets are bundled; the browser does not need a third-party CDN. `/openapi.yaml` is the API contract. `GET /healthz` checks the process; `GET /readyz` checks catalog connectivity/compatibility, not dataset freshness, approved rights or matching quality.

The command creates protected, ignored files under `.local/provider-dev`, builds `Dockerfile.provider` and starts the `screening-provider-dev` Compose project. It reuses existing passwords and refuses inconsistent credentials instead of replacing them. Its PostgreSQL volume is separate from the fraud database. The API binds to loopback; PostgreSQL has no host port. The catalog starts empty, with no client credentials or activated official sources.

Options: `--prepare-only`, `--skip-build`, `--port 8092`. On Linux, run as a regular user: setup uses that user's UID/GID so the non-root containers can read mode-0600 bind-mounted secrets. Windows uses the image's non-root UID. Docker Desktop controls Windows bind-mount access; keep the local directory in your user profile and out of version control.

For subsequent CLI commands in PowerShell:

```powershell
$providerCompose = @('compose', '--env-file', '.local/provider-dev/compose.env', '-p', 'screening-provider-dev', '-f', 'docker-compose.provider.yml')
docker @providerCompose ps
docker @providerCompose run --rm provider-admin -action status
```

Stop with `docker @providerCompose down`. This preserves the catalog volume. Do not add `-v` unless you intend to erase this provider installation. The setup script does not start or modify the fraud stack.

## Create a machine client

A namespace is a customer boundary, expressed as a UUID. It is independent of a fraud tenant in standalone mode. Clients receive an explicit list of operations and datasets; possessing a key alone does not grant all source data.

```powershell
$providerNamespace = [guid]::NewGuid().ToString()
$created = docker @providerCompose run --rm provider-admin -action create-client -name 'Sandbox integration' -namespace $providerNamespace -operations 'search,enrich,catalog,freshness,delta' -requests-per-minute 6000 -ttl 720h | ConvertFrom-Json
$providerClient = $created.client_id
$issued = docker @providerCompose run --rm provider-admin -action issue-key -client $providerClient -ttl 720h -key-file /output/sandbox-key.txt | ConvertFrom-Json
```

The token is written once to `.local/provider-dev/sandbox-key.txt`; it is never printed by the admin tool. The file must not already exist. PostgreSQL stores its SHA-256 digest, not the token. Client and key expiration both apply. Keep the token in a secret manager outside development; never put it in a URL, source control or a frontend bundle.

For a **fictional demonstration only**, explicitly publish the authored two-record fixture and grant this client access:

```powershell
docker @providerCompose run --rm provider-import -register -dataset standalone-demo -label 'Fictional demonstration watchlist' -file /input/controlled-watchlist.json
docker @providerCompose run --rm provider-admin -action grant-dataset -client $providerClient -scope global -dataset standalone-demo
```

This is neither an OFAC/UN designation list nor a PEP dataset. Publishing it does not enable any official connector. Global controlled fixture imports are private operator actions. The fraud platform's tenant-private list workflow still requires two different authorized administrators; there is no public provider HTTP shortcut for that workflow.

In Swagger, choose **Authorize** and enter the token from the protected file. The UI does not persist authorization across reloads. Alternatively:

```powershell
$providerHeaders = @{ Authorization = 'Bearer ' + (Get-Content -Raw .local/provider-dev/sandbox-key.txt).Trim() }
$providerBody = @{
  profile = 'name-fuzzy-v1'
  threshold = 85
  dataset_selection = @(@{ scope = 'global'; id = 'standalone-demo' })
  queries = @(@{ name = 'Ama Testpreson'; type = 'Person' })
  limit = 10
} | ConvertTo-Json -Depth 8
$providerResult = Invoke-RestMethod -Method Post -Uri http://localhost:8091/v1/search -Headers $providerHeaders -ContentType application/json -Body $providerBody
$providerResult.matches | Select-Object entity_id, name, score
```

## HTTP contract

All five business endpoints require `Authorization: Bearer <provider credential>` over a trusted connection. No request tenant header can override the credential's namespace. TLS terminates at your deployment's trusted ingress; HTTP on loopback/the private Docker network is for local development.

| Endpoint | Permission | Behavior |
| --- | --- | --- |
| `POST /v1/search` | `search` | Synchronous matching against explicitly selected, entitled, published datasets. No scenario, decision, case or screening job is created. |
| `POST /v1/enrich` | `enrich` | Returns the permitted supplement for an exact record and retained generation. |
| `GET /v1/datasets` | `catalog` | Pages through entitled datasets and readiness. Optional repeated `dataset=global:id` or `dataset=tenant:id`, `limit`, `cursor`. |
| `GET /v1/freshness` | `freshness` | Requires explicit repeated `dataset` parameters. Returns publication/collection freshness, not a fresh external verification claim. |
| `GET /v1/deltas` | `delta` | Requires explicit datasets; pages through publication events, including rollback publications. It does not return the entire source catalog. |

Search profiles are `exact-name-v1`, `name-fuzzy-v1`, `name-token-v2` and `identifier-evidence-v1`. Fuzzy/identifier profiles require a threshold from 50 to 100. Entity types are the contract's `Person`, `Organization`, `Vessel` and `Aircraft`; they are not interchangeable with watchlist categories. Keep identifiers as strings so leading zeros are retained. See OpenAPI for query fields and identifier namespaces. A score is matching evidence, not the probability of a confirmed identity.

`matches[].payload.provenance` contains the exact enrichment request:

```json
{
  "dataset": {"scope": "global", "id": "standalone-demo"},
  "generation": "<UUID from search provenance>",
  "record_id": "controlled-person-001"
}
```

Search returns evidence `contract_version: 2`, a `provider_reference`, pinned dataset generations, matching explanations, `matches` and `partial`. API URL version `/v1` and source-evidence version `2` are separate contracts. A complete response with an empty `matches` array means no candidate met this particular query/profile/selection. **`partial: true`, a timeout or any error must never be treated as a clean no-match.** Source XML and private ingestion configuration are excluded from public responses.

Errors include 400 for invalid input/cursors, 401 for invalid/expired/revoked credentials, 403 for disallowed operations, 404 for unavailable datasets, 410 for unavailable historical record evidence, 429 for quota/concurrency limits, 503 for unavailable dependencies and 504 for an exceeded request deadline. Unauthorized datasets are not silently removed from a requested selection.

Limits: 32 KiB JSON request body; 10 selected datasets; 50 returned search matches; catalog/delta page size at most 500; 8 MiB response ceiling. The existing matcher also bounds query work and marks candidate overflow incomplete. The default API deadline is 2 seconds, pool and concurrency are each 8. Allowed API timeout is 10 ms–5 s. Quotas are per client per database minute, shared atomically across replicas; authenticated attempts for allowed operations consume budget, including failed requests. This is a protective request budget, not customer billing or a throughput guarantee.

Cursor authority includes client/key/revision/namespace/selection. Reuse the returned cursor with the same dataset selection. Grant changes or key/client revocation invalidate prior authority; another client cannot reuse a cursor.

## Rotate or revoke

Issue a replacement key to a **new** protected file, update the integrating secret, then revoke the old credential UUID returned during issuance:

```powershell
docker @providerCompose run --rm provider-admin -action issue-key -client $providerClient -ttl 720h -key-file /output/sandbox-key-next.txt
docker @providerCompose run --rm provider-admin -action revoke-key -client $providerClient -credential $issued.credential_id
docker @providerCompose run --rm provider-admin -action revoke-dataset -client $providerClient -scope global -dataset standalone-demo
docker @providerCompose run --rm provider-admin -action revoke-client -client $providerClient
```

The admin CLI requires the catalog owner connection and records client/key/grant mutations in `provider_api.admin_audit` with the PostgreSQL actor. It is not an exposed account-management API. Request logs include request/client IDs, operation, status and duration; not tokens, search names, query bodies or source XML. Use distinct administrator identities for attributable production administration.

## Source collection and deployment roles

`provider-source-admin`, `provider-import` and the optional `sources` profile package existing source tooling. Registering a connector does not approve rights or enable collection. Official source operation still requires the existing explicit registration, reviewed rights reference, enablement and entitlement gates described in [source documentation](INTERNAL_PROVIDER_SOURCES.md). No public API route can approve or publish sources. `source-admin -action health` reports standalone workflow support as false; it does not invent fraud workflow tables or receipts.

The API login inherits `screening_provider_reader`: catalog reads, credential digest reads, usage counters and cursor insertion only. It cannot change clients, grants, source data or publications. The collector inherits `screening_provider_collector`: source ingestion/publication/retention capabilities, with no API credential access. Migrations/admin use a separate owner secret. The optional collector login is provisioned by `docker @providerCompose --profile sources up -d provider-source-worker`; no source is automatically registered or activated.

Standalone bootstrap installs the curated source/private-list support DDL from canonical screening migrations 11–23 and records a normalized DDL checksum. It refuses an occupied application database and refuses unknown or changed bootstrap contracts. It is repeatable for this release, **not** a general future schema-upgrade engine. Back up before upgrades and supply a reviewed forward migration for changed releases. The regular fraud migration path remains independently tracked through migration 23.

Back up this product's PostgreSQL catalog and secret-manager configuration separately, including publication history, original source artifacts, approvals/audit, retained records and the database identity. API keys are not recoverable from digests; rotate lost credentials. Validate restores using the existing recovery checks; production PITR/restore timing remains an acceptance gate. Do not drop retained publications to make backups smaller. Usage-minute rows are operational counters; arrange an owner-controlled, bounded retention job after agreeing a retention policy. No automatic deletion policy or production alerting service is activated by this setup.

## Keep the fraud deployment integrated

The default remains `INTERNAL_PROVIDER_TRANSPORT=local`; existing user sessions, tenant roles, private-list approvals, cases, evidence and workers retain their behavior. Standalone credentials are separate from platform bearer tokens.

For an optional provider HTTP process **against the existing shared application catalog**:

1. Apply the normal screening migrations through 23; do not run standalone bootstrap there.
2. Use the private provider-admin tool with an explicitly mounted owner DSN to provision a `screening_provider_reader` login on that shared database. Keep its DSN in `PROVIDER_SHARED_READER_DSN_FILE`.
3. Create a machine client per participating tenant, using the real active tenant UUID as its namespace. Grant only datasets that tenant already has through `screening.source_grants`; machine entitlements add a restriction, not a bypass.
4. Mount a protected `PROVIDER_CLIENT_KEYS_FILE` with this format, using actual tokens instead of placeholders:

   ```json
   {"version":1,"clients":[{"namespace":"<tenant UUID>","credential":"<provider token>"}]}
   ```

5. Add `docker-compose.provider-integrated.yml` after your existing PostgreSQL Compose files. It adds the provider process and configures the screening API/worker and decision API/worker with explicit `INTERNAL_PROVIDER_TRANSPORT=http`, `PROVIDER_HTTP_URL` and the secret file. Review the merged Compose configuration before starting it; budget the additional eight provider connections across all replicas against the shared database limit.

Missing/malformed keyrings fail startup. A tenant without a configured client fails closed. The HTTP adapter does not follow redirects, retry searches or fall back to the local engine on failure. Decision-time screening happens before the decision mutation transaction; evidence and effects are saved atomically afterward. A provider failure/incomplete result returns an evaluation error without saving a successful decision or its effects. Existing manual screening still creates and processes platform workflow jobs.

This HTTP fraud integration deliberately uses the **same catalog metadata** as the platform for authorization, configuration validation, evidence persistence and monitoring. Connecting fraud workflows to a completely separate remote SaaS catalog requires a further metadata/monitoring integration; this release does not claim that topology. External clients can already use the independent product API directly without the fraud platform.

## Acceptance boundary

The standalone delivery does not change the frozen matcher/corpora or Phase 8 measurements. The existing name-quality acceptance and 500 TPS / 50 ms target remain unmet. Source rights, commercial redistribution, production security/image review, deployment capacity, TLS/ingress, PITR, alerting and accountable operational approvals still require the evidence/approvals in the acceptance register. Customer signup/billing, a hosted administration portal and a fraud-to-separate-SaaS-catalog bridge are not implemented features.
