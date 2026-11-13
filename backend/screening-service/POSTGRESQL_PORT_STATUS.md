# Screening on PostgreSQL main

Verified 2026-11-13 UTC. The PostgreSQL changes extend `main` from base `a2d241a`. The MySQL branch and its data remain separate.

## Completed

- [x] Preserve the original working tree in an ignored backup and retained Git stash before switching branches.
- [x] Stop `fraud-mysql-dev` containers without deleting containers, volumes or data.
- [x] Reuse the existing shared PostgreSQL database and its original volume; do not import NovaPay/MySQL data.
- [x] Save a private PostgreSQL backup before applying owner migrations.
- [x] Retain the redesigned scenario screening editor, Add menu, screening entries in the rules list and read-only views.
- [x] Retain manual screening, saved search/history/report pages and decision/case investigation evidence.
- [x] Bring the required PostgreSQL owner repositories, migrations, authoring and worker contracts along with those frontend features.
- [x] Bind browser query state to the authenticated tenant/session and use server-side ingestion/decision proxies.
- [x] Start PostgreSQL APIs and workers with the frontend/identity gateway at http://localhost:8088.
- [x] Preserve existing identity keys and users; update the existing identity client's gateway callback allowlist.
- [x] Repair the decision OpenAPI duplicate key, verify unique mappings and correct gateway server prefixes, and bundle pinned Swagger UI assets locally.
- [x] Adapt the six INTERNAL_PROVIDER documents to PostgreSQL; keep the original migration-branch audit evidence explicitly historical.
- [x] Verify `.local/` is ignored, with no tracked private files or private credential matches in changed files.
- [x] Replace remaining dashboard/detection public tenant settings with the authenticated workspace tenant; deploy the rebuilt frontend on port 8088.
- [x] Add frontend readiness checks, wait for frontend health at gateway startup, bound local build workers and restart the decision worker after PostgreSQL becomes available.

Dashboard deployment follow-up, 2026-11-13: the running frontend image now matches
the rebuilt image, reports healthy, and contains no copy of the previous
`Set NEXT_PUBLIC_DATA_MODEL_TENANT_ID` dashboard error. All 19 focused browser
checks passed: eight workspace/dashboard tenant checks, two Data Model proxy
checks, four decision/case-link checks and five screening-list checks. Live
gateway health/login return 200; a signed-out dashboard redirects to login;
decision health returns 200 and auth readiness returns 204. IT Consortium's
PostgreSQL model/scenario/decision-count endpoints also returned 200.

The wider 25-test exploratory run found two decision-selection assertions that
expect a disabled toolbar when the existing UI hides it, and two display-settings
element lookup failures. Those four checks remain unresolved. The new tenant
test's independent request-context mistake was corrected and its authenticated
foreign-tenant request now passes the 403 assertion. The focused passing run is
not a claim that the entire frontend suite passes.

## Verification

| Check | Result | Scope |
| --- | --- | --- |
| Frontend production build | Passed | Compilation and TypeScript; standalone Docker frontend also built |
| Decision Go suite | 253 top-level / 400 test-subtest passes | Disposable PostgreSQL; one optional MMDB test skipped |
| Screening Go suite | 95 top-level / 199 test-subtest passes | Disposable PostgreSQL, freshly built decision/screening/case APIs and decision/screening workers; no test skips |
| Case Go suite | 90 top-level / 157 test-subtest passes | Disposable PostgreSQL including the real shared identity schema; no test skips |
| Follow-up OpenAPI/HTTP tests | Passed | New duplicate-YAML-key regression plus HTTP/handler packages |
| Screening browser contracts | 61 passed | Editor, rules list, read-only access, evidence, search/refinement, review, exclusions and reports using service fixtures |
| Isolated gateway transport contracts | Passed | All six API prefixes and identity path; method/body, escaped paths, credentials, redirects/cookies and failures |
| Real Swagger browser execution | Passed | Data model, ingestion, decisions and screening health requests through their gateway prefixes |
| Real service health | Passed | All six APIs; auth returns 204 |
| OIDC/login-start handshake | Passed | Public issuer, gateway redirect/callback and binding cookie; no user password login performed |
| Signed-out BFF authorization | Passed | Scenario authoring, decisions, ingestion and screening reject requests with 401 |
| Whitespace/private-material checks | Passed | No private material found in changed files |

The three complete Go runs contain 756 passing test/subtest results. Browser contract tests do not replace real database/worker tests, and neither establishes production throughput or real-source matching accuracy. See the [machine-readable report](evidence/postgresql-port/2026-11-13/report.json). Local raw test logs and backup manifests remain under ignored `.local/branch-switch/2026-11-13/`.

Test variables: `DECISION_TEST_DATABASE_URL`, `DECISION_ENGINE_TEST_DATABASE_URL`, `SCREENING_TEST_DATABASE_URL`, `CASE_MANAGER_TEST_DATABASE_URL`. Use an isolated disposable PostgreSQL server with CREATE DATABASE privileges; tests create and drop their own databases. Worker journeys require `DECISION_INTEGRATION_BINARY`, `DECISION_WORKER_INTEGRATION_BINARY`, `SCREENING_INTEGRATION_BINARY`, `SCREENING_WORKER_INTEGRATION_BINARY` and `CASE_MANAGER_INTEGRATION_BINARY` built from the current source. Never point destructive test setup at the application database.

## Remaining boundaries

Login follow-up, 2026-11-13: the existing `kaddo` binding still used the old
Keycloak issuer after the gateway URL changed. The auth-owned `rebind-issuer`
command moved that verified binding, retained the platform UUID and all tenant
memberships/roles, revoked previous sessions and recorded two tenant audit events.
The gateway no longer forces the old frontend demo tenant at login. All 60 auth
test results passed on disposable PostgreSQL, including repair rollback,
conflicting identities and concurrent retries. Provider credentials match and
fresh login initiation succeeds; user-password sign-in still needs a fresh retry.

- [ ] Implement PostgreSQL decision-time screening and forced outcomes. Current screening runs after a decision is saved, and the inline capability stays disabled.
- [ ] Implement the separate internal source collector, PostgreSQL source-data migrations and provider adapter from the [internal provider plan](INTERNAL_PROVIDER_PLAN.md).
- [ ] Implement and independently verify authoritative OFAC/UN/other source connectors. No new source or synthetic catalog was activated by this switch.
- [ ] Configure a compatible screening provider before running new live searches; unconfigured capabilities remain unavailable and stored history stays readable.
- [ ] Add PostgreSQL case/auth Swagger endpoints if required. Their APIs are routed, but main currently has no Swagger pages for those services.
- [ ] Validate full user sign-in with an existing PostgreSQL membership. MySQL-only users and NovaPay data were intentionally not copied.
- [ ] Measure representative real-provider latency, matching quality and production workload behavior before making performance claims.

Run/restart instructions and the data-preservation boundary are in [PostgreSQL gateway setup](../../docker/POSTGRES_GATEWAY.md).
