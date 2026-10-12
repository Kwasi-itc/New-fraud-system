# Legacy API authorization rollout

2026-10-08 follow-up: [Data Model platform authorization](DATA_MODEL_AUTH.md) now
implements human role/tenant/resource checks and enables Data Model auth in the
checked-in Compose configuration. The initial configuration-only increment and
route inventory below are retained as a dated baseline; other owners remain open.

AUTH-X13 first increment, 2026-10-08. Status: configuration safeguard tested;
broader endpoint authorization remains in progress.

## Implemented configuration boundary

Data Model, ingestion, decision and screening now default an omitted
`SERVICE_AUTH_MODE` to `token`. Startup rejects unknown modes and missing or
whitespace-only tokens in token mode. Middleware also rejects invalid configuration
when a router is constructed without the normal startup path. Token comparisons
use constant-time comparison; errors do not include credentials.

`SERVICE_AUTH_MODE=disabled` remains an explicit development opt-out, including
when supplied through a service's `.env`. The checked-in Compose configuration
and example files still select it explicitly. This increment does not secure
that deployment or add human/tenant authorization to legacy endpoints.

For standalone token-mode startup, supply `SERVICE_AUTH_TOKEN` through private
runtime configuration. Existing nonblank service tokens remain compatible.
Do not put the token in `NEXT_PUBLIC_*` variables or frontend build arguments.
Coordinated browser-proxy and downstream integration changes are required before
switching the development stack away from its explicit disabled mode.

## Ownership checks required for the next increment

| Route family | Required boundary before granting human access |
| --- | --- |
| Tenant-qualified routes | Resolve current platform identity; match path tenant to active membership; check action permission. Resource IDs nested under a tenant still require owner-side tenant consistency. |
| Data Model `/tables/:tableId/*` | Resolve the table's authoritative tenant before reading or changing fields/options/navigation. Validate referenced objects as well as the target. |
| Data Model `/fields/:fieldId/*`, `/enum-values/:enumValueId` | Resolve ownership through field/table relationships; do not trust an added caller-supplied tenant header. |
| Data Model `/links/:linkId`, `/pivots/:pivotId`, `/navigation-options/:navigationOptionId` | Resolve owner tenant and preserve tenant-consistent relationships in mutations. |
| Data Model `/index-jobs/:jobId/*` | Resolve job tenant; authorize reads and retry separately. |
| Ingestion `/upload-logs/:uploadLogId`, `/deferred-ingests/:deferredIngestId` | Resolve tenant from the stored resource; reject foreign IDs. |
| Tenant registry creation/list/provision and `/admin/*` | Decide platform-wide operator authority separately from ordinary tenant administration. Do not expose global tenant lists to a tenant-scoped principal. |
| Scenario iterations/publication/preparation | Separate authoring from publication permissions; HTTP method alone cannot identify the required capability. |
| Legacy screening review/comment/file operations | Preserve case/inbox/resource authority; these must not become an alternate path around investigator checks. |
| Owner callbacks and internal intake | Keep service principals separate; define permitted tenant/action scope and prevent use as human review authority. |
| Health, documentation, metrics, service-info and catalogs | Classify public/operator/tenant visibility explicitly; some routes reveal deployment details. Inspect separately from mutation routes. |

The Data Model `GET /v1/admin/reconcile` calls a reconcile handler; do not infer
read-only permission solely from GET. The case-specific verifier is not a general
platform-role verifier. Existing tenant/case roles retain their responsibilities
while AUTH-X14 defines the non-case capability map.

## Verification

Run `sh backend/auth-service/scripts/test-legacy-auth.sh` in a Go 1.25 environment.
The script runs configuration and HTTP package tests for all four services.
Database-backed suites are not enabled by this command; run them separately
against an isolated disposable database when adding tenant/resource enforcement.

Executed on 2026-10-08 in an isolated `golang:1.25.0` container: all eight app/HTTP
packages passed. No application database, live service configuration, frontend
build or full browser journey was exercised by this increment.

Boundary cases cover omitted/unknown mode, missing/blank configured secret,
missing/wrong/malformed caller credential, valid service credential, explicit
development opt-out and response secret redaction. Configuration cases cover
the secure default, explicit token/disabled modes and explicit dotenv opt-out.

Before closing X13, complete the per-operation human/service/permission matrix,
implement the owner checks above, remove browser credential paths, verify direct
API denial and deploy/test the coordinated configuration. Neither passing these
tests nor the route inventory below establishes those guarantees.

## Legacy route inventory

Snapshot of literal routes registered on the legacy `v1`, `readRoutes` and
`internal` groups in the four primary routers. Every listed route currently uses
the service-token/explicit-disabled boundary. Required action permissions and
feature gates remain to be assigned under X14/X15. This inventory excludes
separately registered investigator routes and root health/docs/metrics routes;
those require their own matrix entries before X13 can close.

### data-model-service

| Method | Path | Registration |
| --- | --- | --- |
| POST | `/v1/tenants` | [router.go:196](../data-model-service/internal/httpapi/router.go#L196) |
| GET | `/v1/tenants` | [router.go:197](../data-model-service/internal/httpapi/router.go#L197) |
| GET | `/v1/tenants/:tenantId` | [router.go:198](../data-model-service/internal/httpapi/router.go#L198) |
| POST | `/v1/tenants/:tenantId/provision` | [router.go:199](../data-model-service/internal/httpapi/router.go#L199) |
| GET | `/v1/tenants/:tenantId/data-model` | [router.go:200](../data-model-service/internal/httpapi/router.go#L200) |
| GET | `/v1/tenants/:tenantId/data-model/export` | [router.go:201](../data-model-service/internal/httpapi/router.go#L201) |
| POST | `/v1/tenants/:tenantId/data-model/import` | [router.go:202](../data-model-service/internal/httpapi/router.go#L202) |
| GET | `/v1/tenants/:tenantId/tables` | [router.go:203](../data-model-service/internal/httpapi/router.go#L203) |
| POST | `/v1/tenants/:tenantId/tables` | [router.go:204](../data-model-service/internal/httpapi/router.go#L204) |
| GET | `/v1/tables/:tableId/fields` | [router.go:205](../data-model-service/internal/httpapi/router.go#L205) |
| POST | `/v1/tables/:tableId/fields` | [router.go:206](../data-model-service/internal/httpapi/router.go#L206) |
| GET | `/v1/fields/:fieldId/enum-values` | [router.go:207](../data-model-service/internal/httpapi/router.go#L207) |
| POST | `/v1/fields/:fieldId/enum-values` | [router.go:208](../data-model-service/internal/httpapi/router.go#L208) |
| GET | `/v1/tenants/:tenantId/links` | [router.go:209](../data-model-service/internal/httpapi/router.go#L209) |
| POST | `/v1/tenants/:tenantId/links` | [router.go:210](../data-model-service/internal/httpapi/router.go#L210) |
| GET | `/v1/tenants/:tenantId/pivots` | [router.go:211](../data-model-service/internal/httpapi/router.go#L211) |
| POST | `/v1/tenants/:tenantId/pivots` | [router.go:212](../data-model-service/internal/httpapi/router.go#L212) |
| GET | `/v1/tables/:tableId/options` | [router.go:213](../data-model-service/internal/httpapi/router.go#L213) |
| PUT | `/v1/tables/:tableId/options` | [router.go:214](../data-model-service/internal/httpapi/router.go#L214) |
| GET | `/v1/tables/:tableId/navigation-options` | [router.go:215](../data-model-service/internal/httpapi/router.go#L215) |
| POST | `/v1/tables/:tableId/navigation-options` | [router.go:216](../data-model-service/internal/httpapi/router.go#L216) |
| GET | `/v1/tenants/:tenantId/schema-change-log` | [router.go:217](../data-model-service/internal/httpapi/router.go#L217) |
| GET | `/v1/tenants/:tenantId/schema-migrations` | [router.go:218](../data-model-service/internal/httpapi/router.go#L218) |
| POST | `/v1/tenants/:tenantId/index-jobs` | [router.go:219](../data-model-service/internal/httpapi/router.go#L219) |
| GET | `/v1/tenants/:tenantId/index-jobs` | [router.go:220](../data-model-service/internal/httpapi/router.go#L220) |
| GET | `/v1/index-jobs/:jobId` | [router.go:221](../data-model-service/internal/httpapi/router.go#L221) |
| POST | `/v1/index-jobs/:jobId/retry` | [router.go:222](../data-model-service/internal/httpapi/router.go#L222) |
| PATCH | `/v1/tables/:tableId` | [router.go:223](../data-model-service/internal/httpapi/router.go#L223) |
| PATCH | `/v1/fields/:fieldId` | [router.go:224](../data-model-service/internal/httpapi/router.go#L224) |
| PATCH | `/v1/enum-values/:enumValueId` | [router.go:225](../data-model-service/internal/httpapi/router.go#L225) |
| DELETE | `/v1/tables/:tableId` | [router.go:226](../data-model-service/internal/httpapi/router.go#L226) |
| DELETE | `/v1/fields/:fieldId` | [router.go:227](../data-model-service/internal/httpapi/router.go#L227) |
| DELETE | `/v1/enum-values/:enumValueId` | [router.go:228](../data-model-service/internal/httpapi/router.go#L228) |
| DELETE | `/v1/links/:linkId` | [router.go:229](../data-model-service/internal/httpapi/router.go#L229) |
| DELETE | `/v1/pivots/:pivotId` | [router.go:230](../data-model-service/internal/httpapi/router.go#L230) |
| DELETE | `/v1/navigation-options/:navigationOptionId` | [router.go:231](../data-model-service/internal/httpapi/router.go#L231) |
| GET | `/v1/admin/reconcile` | [router.go:232](../data-model-service/internal/httpapi/router.go#L232) |

### ingestion-service

| Method | Path | Registration |
| --- | --- | --- |
| POST | `/v1/tenants/:tenantId/ingest/:objectType` | [router.go:144](../ingestion-service/internal/httpapi/router.go#L144) |
| PATCH | `/v1/tenants/:tenantId/ingest/:objectType` | [router.go:145](../ingestion-service/internal/httpapi/router.go#L145) |
| POST | `/v1/tenants/:tenantId/ingest/:objectType/batch` | [router.go:146](../ingestion-service/internal/httpapi/router.go#L146) |
| PATCH | `/v1/tenants/:tenantId/ingest/:objectType/batch` | [router.go:147](../ingestion-service/internal/httpapi/router.go#L147) |
| GET | `/v1/admin/read-metrics` | [router.go:148](../ingestion-service/internal/httpapi/router.go#L148) |
| GET | `/v1/admin/deferred-ingest-metrics` | [router.go:149](../ingestion-service/internal/httpapi/router.go#L149) |
| GET | `/v1/tenants/:tenantId/records/:objectType` | [router.go:151](../ingestion-service/internal/httpapi/router.go#L151) |
| GET | `/v1/tenants/:tenantId/records/:objectType/search` | [router.go:152](../ingestion-service/internal/httpapi/router.go#L152) |
| GET | `/v1/tenants/:tenantId/records/:objectType/:objectId` | [router.go:153](../ingestion-service/internal/httpapi/router.go#L153) |
| POST | `/v1/tenants/:tenantId/query/aggregate` | [router.go:154](../ingestion-service/internal/httpapi/router.go#L154) |
| POST | `/v1/tenants/:tenantId/ingest/:objectType/csv` | [router.go:155](../ingestion-service/internal/httpapi/router.go#L155) |
| GET | `/v1/tenants/:tenantId/ingest/:objectType/upload-logs` | [router.go:156](../ingestion-service/internal/httpapi/router.go#L156) |
| GET | `/v1/upload-logs/:uploadLogId` | [router.go:157](../ingestion-service/internal/httpapi/router.go#L157) |
| GET | `/v1/deferred-ingests/:deferredIngestId` | [router.go:158](../ingestion-service/internal/httpapi/router.go#L158) |

### decision-engine-service

| Method | Path | Registration |
| --- | --- | --- |
| GET | `/v1/admin/runtime-metrics` | [router.go:257](../decision-engine-service/internal/httpapi/router.go#L257) |
| GET | `/v1/service-info` | [router.go:258](../decision-engine-service/internal/httpapi/router.go#L258) |
| GET | `/v1/rule-functions` | [router.go:267](../decision-engine-service/internal/httpapi/router.go#L267) |
| GET | `/v1/tenants/:tenantId/scenarios` | [router.go:268](../decision-engine-service/internal/httpapi/router.go#L268) |
| POST | `/v1/tenants/:tenantId/scenarios` | [router.go:269](../decision-engine-service/internal/httpapi/router.go#L269) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId` | [router.go:270](../decision-engine-service/internal/httpapi/router.go#L270) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/editor-identifiers` | [router.go:271](../decision-engine-service/internal/httpapi/router.go#L271) |
| PUT | `/v1/tenants/:tenantId/scenarios/:scenarioId` | [router.go:272](../decision-engine-service/internal/httpapi/router.go#L272) |
| DELETE | `/v1/tenants/:tenantId/scenarios/:scenarioId` | [router.go:273](../decision-engine-service/internal/httpapi/router.go#L273) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/copy` | [router.go:274](../decision-engine-service/internal/httpapi/router.go#L274) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/rules/latest` | [router.go:275](../decision-engine-service/internal/httpapi/router.go#L275) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/ast-ai-description` | [router.go:276](../decision-engine-service/internal/httpapi/router.go#L276) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/generate-ast` | [router.go:277](../decision-engine-service/internal/httpapi/router.go#L277) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations` | [router.go:278](../decision-engine-service/internal/httpapi/router.go#L278) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations/metadata` | [router.go:279](../decision-engine-service/internal/httpapi/router.go#L279) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations` | [router.go:280](../decision-engine-service/internal/httpapi/router.go#L280) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId` | [router.go:281](../decision-engine-service/internal/httpapi/router.go#L281) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/draft` | [router.go:282](../decision-engine-service/internal/httpapi/router.go#L282) |
| PUT | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId` | [router.go:283](../decision-engine-service/internal/httpapi/router.go#L283) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/commit` | [router.go:284](../decision-engine-service/internal/httpapi/router.go#L284) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/deactivate` | [router.go:285](../decision-engine-service/internal/httpapi/router.go#L285) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/publications` | [router.go:286](../decision-engine-service/internal/httpapi/router.go#L286) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/publications` | [router.go:287](../decision-engine-service/internal/httpapi/router.go#L287) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/publications/preparation` | [router.go:288](../decision-engine-service/internal/httpapi/router.go#L288) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/publications/preparation` | [router.go:289](../decision-engine-service/internal/httpapi/router.go#L289) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules` | [router.go:290](../decision-engine-service/internal/httpapi/router.go#L290) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/rule-groups` | [router.go:291](../decision-engine-service/internal/httpapi/router.go#L291) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules` | [router.go:292](../decision-engine-service/internal/httpapi/router.go#L292) |
| PUT | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId` | [router.go:293](../decision-engine-service/internal/httpapi/router.go#L293) |
| DELETE | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId` | [router.go:294](../decision-engine-service/internal/httpapi/router.go#L294) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId/ai-description` | [router.go:295](../decision-engine-service/internal/httpapi/router.go#L295) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/validate` | [router.go:296](../decision-engine-service/internal/httpapi/router.go#L296) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/evaluate` | [router.go:297](../decision-engine-service/internal/httpapi/router.go#L297) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/decisions` | [router.go:298](../decision-engine-service/internal/httpapi/router.go#L298) |
| GET | `/v1/tenants/:tenantId/decisions/count` | [router.go:299](../decision-engine-service/internal/httpapi/router.go#L299) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/test-runs` | [router.go:300](../decision-engine-service/internal/httpapi/router.go#L300) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/test-runs` | [router.go:301](../decision-engine-service/internal/httpapi/router.go#L301) |
| GET | `/v1/tenants/:tenantId/test-runs/:testRunId` | [router.go:302](../decision-engine-service/internal/httpapi/router.go#L302) |
| POST | `/v1/tenants/:tenantId/test-runs/:testRunId/cancel` | [router.go:303](../decision-engine-service/internal/httpapi/router.go#L303) |
| GET | `/v1/tenants/:tenantId/test-runs/:testRunId/decision-data-by-score` | [router.go:304](../decision-engine-service/internal/httpapi/router.go#L304) |
| GET | `/v1/tenants/:tenantId/test-runs/:testRunId/data-by-rule-execution` | [router.go:305](../decision-engine-service/internal/httpapi/router.go#L305) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflows` | [router.go:306](../decision-engine-service/internal/httpapi/router.go#L306) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflows` | [router.go:307](../decision-engine-service/internal/httpapi/router.go#L307) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflows/reorder` | [router.go:308](../decision-engine-service/internal/httpapi/router.go#L308) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId` | [router.go:309](../decision-engine-service/internal/httpapi/router.go#L309) |
| PUT | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId` | [router.go:310](../decision-engine-service/internal/httpapi/router.go#L310) |
| DELETE | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId` | [router.go:311](../decision-engine-service/internal/httpapi/router.go#L311) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules` | [router.go:312](../decision-engine-service/internal/httpapi/router.go#L312) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules` | [router.go:313](../decision-engine-service/internal/httpapi/router.go#L313) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/reorder` | [router.go:314](../decision-engine-service/internal/httpapi/router.go#L314) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId` | [router.go:315](../decision-engine-service/internal/httpapi/router.go#L315) |
| PUT | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId` | [router.go:316](../decision-engine-service/internal/httpapi/router.go#L316) |
| DELETE | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId` | [router.go:317](../decision-engine-service/internal/httpapi/router.go#L317) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions` | [router.go:318](../decision-engine-service/internal/httpapi/router.go#L318) |
| PUT | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions/:conditionId` | [router.go:319](../decision-engine-service/internal/httpapi/router.go#L319) |
| DELETE | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions/:conditionId` | [router.go:320](../decision-engine-service/internal/httpapi/router.go#L320) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions` | [router.go:321](../decision-engine-service/internal/httpapi/router.go#L321) |
| PUT | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions/:actionId` | [router.go:322](../decision-engine-service/internal/httpapi/router.go#L322) |
| DELETE | `/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions/:actionId` | [router.go:323](../decision-engine-service/internal/httpapi/router.go#L323) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs` | [router.go:324](../decision-engine-service/internal/httpapi/router.go#L324) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs` | [router.go:325](../decision-engine-service/internal/httpapi/router.go#L325) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId` | [router.go:326](../decision-engine-service/internal/httpapi/router.go#L326) |
| PUT | `/v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId` | [router.go:327](../decision-engine-service/internal/httpapi/router.go#L327) |
| DELETE | `/v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId` | [router.go:328](../decision-engine-service/internal/httpapi/router.go#L328) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs` | [router.go:329](../decision-engine-service/internal/httpapi/router.go#L329) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs` | [router.go:330](../decision-engine-service/internal/httpapi/router.go#L330) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId` | [router.go:331](../decision-engine-service/internal/httpapi/router.go#L331) |
| PUT | `/v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId` | [router.go:332](../decision-engine-service/internal/httpapi/router.go#L332) |
| DELETE | `/v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId` | [router.go:333](../decision-engine-service/internal/httpapi/router.go#L333) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/rule-snoozes` | [router.go:334](../decision-engine-service/internal/httpapi/router.go#L334) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/rule-snoozes` | [router.go:335](../decision-engine-service/internal/httpapi/router.go#L335) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/recurring-schedule` | [router.go:336](../decision-engine-service/internal/httpapi/router.go#L336) |
| PUT | `/v1/tenants/:tenantId/scenarios/:scenarioId/recurring-schedule` | [router.go:337](../decision-engine-service/internal/httpapi/router.go#L337) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions` | [router.go:338](../decision-engine-service/internal/httpapi/router.go#L338) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions/status-summary` | [router.go:339](../decision-engine-service/internal/httpapi/router.go#L339) |
| GET | `/v1/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions/:executionId` | [router.go:340](../decision-engine-service/internal/httpapi/router.go#L340) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions/:executionId/retry` | [router.go:341](../decision-engine-service/internal/httpapi/router.go#L341) |
| POST | `/v1/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions` | [router.go:342](../decision-engine-service/internal/httpapi/router.go#L342) |
| GET | `/v1/tenants/:tenantId/platform/custom-lists` | [router.go:343](../decision-engine-service/internal/httpapi/router.go#L343) |
| POST | `/v1/tenants/:tenantId/platform/custom-lists` | [router.go:344](../decision-engine-service/internal/httpapi/router.go#L344) |
| GET | `/v1/tenants/:tenantId/platform/custom-lists/:listId` | [router.go:345](../decision-engine-service/internal/httpapi/router.go#L345) |
| PUT | `/v1/tenants/:tenantId/platform/custom-lists/:listId` | [router.go:346](../decision-engine-service/internal/httpapi/router.go#L346) |
| DELETE | `/v1/tenants/:tenantId/platform/custom-lists/:listId` | [router.go:347](../decision-engine-service/internal/httpapi/router.go#L347) |
| GET | `/v1/tenants/:tenantId/platform/custom-lists/:listId/entries` | [router.go:348](../decision-engine-service/internal/httpapi/router.go#L348) |
| POST | `/v1/tenants/:tenantId/platform/custom-lists/:listId/entries` | [router.go:349](../decision-engine-service/internal/httpapi/router.go#L349) |
| POST | `/v1/tenants/:tenantId/platform/custom-lists/:listId/entries/import` | [router.go:350](../decision-engine-service/internal/httpapi/router.go#L350) |
| PUT | `/v1/tenants/:tenantId/platform/custom-lists/:listId/entries/:entryId` | [router.go:351](../decision-engine-service/internal/httpapi/router.go#L351) |
| DELETE | `/v1/tenants/:tenantId/platform/custom-lists/:listId/entries/:entryId` | [router.go:352](../decision-engine-service/internal/httpapi/router.go#L352) |
| GET | `/v1/tenants/:tenantId/platform/custom-list-entries` | [router.go:353](../decision-engine-service/internal/httpapi/router.go#L353) |
| GET | `/v1/tenants/:tenantId/platform/record-tags` | [router.go:354](../decision-engine-service/internal/httpapi/router.go#L354) |
| POST | `/v1/tenants/:tenantId/platform/record-tags` | [router.go:355](../decision-engine-service/internal/httpapi/router.go#L355) |
| POST | `/v1/tenants/:tenantId/platform/risk-snapshots` | [router.go:356](../decision-engine-service/internal/httpapi/router.go#L356) |
| GET | `/v1/tenants/:tenantId/platform/ip-flags` | [router.go:357](../decision-engine-service/internal/httpapi/router.go#L357) |
| POST | `/v1/tenants/:tenantId/platform/ip-flags` | [router.go:358](../decision-engine-service/internal/httpapi/router.go#L358) |
| GET | `/v1/tenants/:tenantId/decisions/:decisionId` | [router.go:359](../decision-engine-service/internal/httpapi/router.go#L359) |
| GET | `/v1/tenants/:tenantId/decisions` | [router.go:360](../decision-engine-service/internal/httpapi/router.go#L360) |
| POST | `/v1/tenants/:tenantId/decisions` | [router.go:361](../decision-engine-service/internal/httpapi/router.go#L361) |
| POST | `/v1/tenants/:tenantId/decisions/all` | [router.go:362](../decision-engine-service/internal/httpapi/router.go#L362) |
| GET | `/v1/tenants/:tenantId/decisions/:decisionId/workflow-executions` | [router.go:363](../decision-engine-service/internal/httpapi/router.go#L363) |
| GET | `/v1/tenants/:tenantId/decisions/:decisionId/screening-executions` | [router.go:364](../decision-engine-service/internal/httpapi/router.go#L364) |
| GET | `/v1/tenants/:tenantId/screening-executions/:executionId` | [router.go:365](../decision-engine-service/internal/httpapi/router.go#L365) |
| POST | `/v1/tenants/:tenantId/screening-executions/:executionId/status` | [router.go:366](../decision-engine-service/internal/httpapi/router.go#L366) |
| POST | `/v1/tenants/:tenantId/screening-executions/:executionId/retry` | [router.go:367](../decision-engine-service/internal/httpapi/router.go#L367) |
| GET | `/v1/tenants/:tenantId/decisions/:decisionId/scoring-requests` | [router.go:368](../decision-engine-service/internal/httpapi/router.go#L368) |
| GET | `/v1/tenants/:tenantId/scoring-requests/:requestId` | [router.go:369](../decision-engine-service/internal/httpapi/router.go#L369) |
| POST | `/v1/tenants/:tenantId/scoring-requests/:requestId/status` | [router.go:370](../decision-engine-service/internal/httpapi/router.go#L370) |
| POST | `/v1/tenants/:tenantId/scoring-requests/:requestId/retry` | [router.go:371](../decision-engine-service/internal/httpapi/router.go#L371) |
| GET | `/v1/tenants/:tenantId/outbox-events` | [router.go:372](../decision-engine-service/internal/httpapi/router.go#L372) |
| GET | `/v1/tenants/:tenantId/async-decision-executions` | [router.go:373](../decision-engine-service/internal/httpapi/router.go#L373) |
| GET | `/v1/tenants/:tenantId/async-decision-executions/status-summary` | [router.go:374](../decision-engine-service/internal/httpapi/router.go#L374) |
| GET | `/v1/tenants/:tenantId/async-decision-executions/:executionId` | [router.go:375](../decision-engine-service/internal/httpapi/router.go#L375) |
| POST | `/v1/tenants/:tenantId/async-decision-executions/:executionId/retry` | [router.go:376](../decision-engine-service/internal/httpapi/router.go#L376) |
| POST | `/v1/tenants/:tenantId/async-decision-executions` | [router.go:377](../decision-engine-service/internal/httpapi/router.go#L377) |
| POST | `/v1/tenants/:tenantId/test-runs/:testRunId/evaluate` | [router.go:378](../decision-engine-service/internal/httpapi/router.go#L378) |
| POST | `/v1/tenants/:tenantId/ingestion-events/record-ingested` | [router.go:379](../decision-engine-service/internal/httpapi/router.go#L379) |
| POST | `/internal/screening-status-updates` | [router.go:386](../decision-engine-service/internal/httpapi/router.go#L386) |

### screening-service

| Method | Path | Registration |
| --- | --- | --- |
| GET | `/v1/service-info` | [router.go:139](../screening-service/internal/httpapi/router.go#L139) |
| GET | `/v1/screening-provider/catalog` | [router.go:155](../screening-service/internal/httpapi/router.go#L155) |
| GET | `/v1/screening-provider/freshness` | [router.go:156](../screening-service/internal/httpapi/router.go#L156) |
| GET | `/v1/tenants/:tenantId/dataset-update-jobs` | [router.go:157](../screening-service/internal/httpapi/router.go#L157) |
| POST | `/v1/tenants/:tenantId/dataset-update-jobs` | [router.go:158](../screening-service/internal/httpapi/router.go#L158) |
| GET | `/v1/tenants/:tenantId/dataset-update-jobs/:jobId` | [router.go:159](../screening-service/internal/httpapi/router.go#L159) |
| POST | `/v1/tenants/:tenantId/dataset-update-jobs/:jobId/retry` | [router.go:160](../screening-service/internal/httpapi/router.go#L160) |
| POST | `/v1/tenants/:tenantId/screenings` | [router.go:161](../screening-service/internal/httpapi/router.go#L161) |
| POST | `/v1/tenants/:tenantId/screenings/freeform-search` | [router.go:162](../screening-service/internal/httpapi/router.go#L162) |
| GET | `/v1/tenants/:tenantId/decisions/:decisionId/screenings` | [router.go:163](../screening-service/internal/httpapi/router.go#L163) |
| GET | `/v1/tenants/:tenantId/screenings/:screeningId` | [router.go:164](../screening-service/internal/httpapi/router.go#L164) |
| POST | `/v1/tenants/:tenantId/screenings/:screeningId/retry` | [router.go:165](../screening-service/internal/httpapi/router.go#L165) |
| POST | `/v1/tenants/:tenantId/screening-matches/:matchId/review` | [router.go:166](../screening-service/internal/httpapi/router.go#L166) |
| POST | `/v1/tenants/:tenantId/screening-matches/:matchId/comments` | [router.go:167](../screening-service/internal/httpapi/router.go#L167) |
| POST | `/v1/tenants/:tenantId/screening-matches/:matchId/enrich` | [router.go:168](../screening-service/internal/httpapi/router.go#L168) |
| POST | `/v1/tenants/:tenantId/screenings/:screeningId/files` | [router.go:169](../screening-service/internal/httpapi/router.go#L169) |
| POST | `/v1/tenants/:tenantId/screenings/:screeningId/file-uploads` | [router.go:170](../screening-service/internal/httpapi/router.go#L170) |
| GET | `/v1/tenants/:tenantId/screenings/:screeningId/files` | [router.go:171](../screening-service/internal/httpapi/router.go#L171) |
| GET | `/v1/tenants/:tenantId/screenings/:screeningId/files/:fileId` | [router.go:172](../screening-service/internal/httpapi/router.go#L172) |
| GET | `/v1/tenants/:tenantId/screenings/:screeningId/files/:fileId/download` | [router.go:173](../screening-service/internal/httpapi/router.go#L173) |
| GET | `/v1/tenants/:tenantId/screening-whitelist` | [router.go:174](../screening-service/internal/httpapi/router.go#L174) |
| POST | `/v1/tenants/:tenantId/screening-whitelist` | [router.go:175](../screening-service/internal/httpapi/router.go#L175) |
| DELETE | `/v1/tenants/:tenantId/screening-whitelist` | [router.go:176](../screening-service/internal/httpapi/router.go#L176) |
| GET | `/v1/tenants/:tenantId/continuous-screening-configs` | [router.go:177](../screening-service/internal/httpapi/router.go#L177) |
| POST | `/v1/tenants/:tenantId/continuous-screening-configs` | [router.go:178](../screening-service/internal/httpapi/router.go#L178) |
| GET | `/v1/tenants/:tenantId/continuous-screening-configs/:configId` | [router.go:179](../screening-service/internal/httpapi/router.go#L179) |
| PUT | `/v1/tenants/:tenantId/continuous-screening-configs/:configId` | [router.go:180](../screening-service/internal/httpapi/router.go#L180) |
| DELETE | `/v1/tenants/:tenantId/continuous-screening-configs/:configId` | [router.go:181](../screening-service/internal/httpapi/router.go#L181) |
| GET | `/v1/tenants/:tenantId/continuous-screening-configs/:configId/monitored-objects` | [router.go:182](../screening-service/internal/httpapi/router.go#L182) |
| POST | `/v1/tenants/:tenantId/continuous-screening-configs/:configId/monitored-objects` | [router.go:183](../screening-service/internal/httpapi/router.go#L183) |
| GET | `/v1/tenants/:tenantId/monitored-objects/:monitoredObjectId` | [router.go:184](../screening-service/internal/httpapi/router.go#L184) |
| DELETE | `/v1/tenants/:tenantId/monitored-objects/:monitoredObjectId` | [router.go:185](../screening-service/internal/httpapi/router.go#L185) |
| POST | `/v1/tenants/:tenantId/monitored-objects/:monitoredObjectId/requeue` | [router.go:186](../screening-service/internal/httpapi/router.go#L186) |
| POST | `/internal/v1/tenants/:tenantId/decision-screenings` | [router.go:193](../screening-service/internal/httpapi/router.go#L193) |
