# Frontend with the local MySQL services

For the unified platform, run `powershell -NoProfile -ExecutionPolicy Bypass -File docker/setup.ps1`, open **http://localhost:8088** and select **Continue to sign in**. Keycloak login and administration use `/identity/` on that same port. Use an existing Keycloak account provisioned by the auth service. Individual service ports stay inside Docker.

The frontend resolves its tenant from the current auth session. It does not require a tenant or a service credential in a browser environment variable. Backend URLs and mounted service credentials stay on the Next server. Data-model resource IDs without a tenant prefix are checked against the current tenant's metadata before forwarding.

An existing Keycloak account also needs an active MySQL platform membership.
Accounts from the earlier PostgreSQL setup are not automatically granted tenant
access. If sign-in returns to the login page with an access message, an operator
should check the account's exact provider identity and use the auth service's
[explicit provisioning procedure](../backend/auth-service/MYSQL_DOCKER.md#explicit-operator-provisioning)
with the intended existing tenant and roles. Do not change the account password
or grant access automatically during login.

For a new device, use the auth service's [portable user manifest and setup script](../backend/auth-service/MYSQL_DOCKER.md#portable-user-and-permission-setup)
to create the intended local users and memberships before signing in. Generate
fresh local credentials on that device rather than copying `.local/`.

| Module | Frontend gateway | Backend |
| --- | --- | --- |
| Authentication | `/api/auth/*` | auth-service:8087 |
| Data models | `/api/data-model/*` | data-model-service:8080 |
| Ingestion and records | `/api/ingestion/*` | ingestion-service:8081 |
| Decisions | `/api/decisions/*` | decision-engine-service:8082 |
| Workflows and scenario screening | `/api/workflows/*`, `/api/scenario-screening/*` | decision-engine-service:8082 |
| Case management and authorized evidence | `/api/cases/*`, `/api/case-evidence/*`, `/api/case-review/*` | case-manager-service:8086 and resource owners |
| Manual screening | `/api/screening-search/*` | screening-service:8085 |

For initial installation or a restarted Docker network, use the complete
[platform setup](../docker/mysql-dev/README.md#unified-frontend-and-backend):
`powershell -NoProfile -ExecutionPolicy Bypass -File docker/mysql-dev/setup-platform.ps1`.
It installs prerequisites on both database servers, reconciles local account
subnets and stops on the first error. To rebuild just the already installed
frontend integration, use:

```powershell
powershell -ExecutionPolicy Bypass -File frontend/scripts/setup-mysql.ps1
```

This separate development helper registers the fraud callback on the existing local Keycloak client, prepares ignored frontend settings, builds backend images, synchronizes grants on published physical tables, reloads the auth API and worker, and starts the frontend. It can publish individual development ports, including frontend port 3002; use `docker/setup.ps1` to return to the unified single-port setup. It does not reset application volumes or run business-work retries. Keycloak uses its dedicated `keycloak` database on the shared MySQL server; application data uses `fraud`.

The local table author grants the existing ingestion accounts `SELECT, INSERT, UPDATE` on each newly created physical table, and gives existing decision/case/data-model reader accounts `SELECT`. The operator sync paginates existing publications and does not grant privileges across the schema or create accounts. The local ingestion worker resolves each resource's actual published owner. Isolated runtimes retain their configured tenant/table scope.

The production frontend image uses Next standalone output, runs as the Node user with a read-only filesystem, and receives secrets through mounted files. `.local/`, provider credentials, signing keys, browser traces, and local environment files must remain untracked.

## Verification

- [x] Frontend production build and TypeScript checks.
- [x] Real Keycloak browser login with a secure HttpOnly session cookie.
- [x] Data-model creation, field creation, current-tenant checks, and foreign resource denial.
- [x] Ingestion runtime and HTTP unit checks.
- [x] Decision runtime, API and store unit checks.
- [x] Real MySQL data-model maintenance test, including missing retained reservations and uncertain commit acknowledgements.
- [x] CSV ingestion and worker completion through the authenticated frontend.
- [x] Publish a high-amount rule and verify approve/review outcomes through the frontend.
- [x] Case creation and authorized decision evidence through the frontend.
- [x] Workflow access, scenario-screening authoring, stale edit rejection, and safe screening summaries.
- [x] Manual screening through the current user's identity.
- [x] Lists overview with exact entry counts, without downloading every list entry.
- [x] Cross-origin write denial and logout replay checks against the live services.

The latest live acceptance run passed **10 suites and 47 gateway requests**. [Recorded results](evidence/mysql-integration/2026-11-10T13-20-22.737Z/report.json) include the synthetic tenant and provider-user cleanup status. The decisions screen also renders the published scenario using the owner's cursor API; page controls do not add SQL offset scans.

The Lists overview uses the MySQL list response's `entry_count`. The previous
unfiltered `/platform/custom-list-entries` request was invalid because that
endpoint requires an explicit `list_name`. Counts are now read with list metadata
in one authorized snapshot. The isolated MySQL regression verifies a 601-entry
list beyond the entry-download budget, empty lists, rename/delete updates, tenant
isolation, revoked membership, and a covering `de_entry_list` lookup on
`tenant_id, list_id`. The focused Lists browser regression also passes.

Run the live acceptance test from the repository root:

```powershell
node frontend/scripts/mysql-acceptance.mjs
```

It creates an isolated synthetic tenant and a temporary local Keycloak user, exercises the actual browser and Next gateways, deletes the provider user, and retains synthetic MySQL records and audit history. Reports under `frontend/evidence/mysql-integration/` contain operation/status metadata without credentials or response bodies.

The first broad browser run recorded 97 passes and 21 failures. Those results are not a clean final regression run: its fixtures and several selectors still reflected older tenant configuration, display controls, and case tabs. Updated gateway checks are tested separately; the broad suite must not be reported as fully passing until its remaining expectations are updated and rerun.

The updated tenant, gateway, decision-selection, and display-settings regression group passes **12/12 browser tests**. The pagination and legacy local-session checks pass **6/6 Node tests**.

This is a local development integration. Existing case AI review and explicit assignment-run limitations remain documented in the case service status files. Screening and other local provider adapters do not establish production provider readiness or production cutover approval.
