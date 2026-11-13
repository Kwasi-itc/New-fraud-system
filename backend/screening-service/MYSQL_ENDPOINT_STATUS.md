# Screening MySQL endpoint status

Source inventory: **49 mounted operations**, including documentation and health/metrics. **49 positively verified; 0 awaiting passing complete evidence.**

`[x]` requires successful native behavior in passing integration evidence. Code presence, expected rejection, synthetic data and a documented route do not establish production/provider acceptance.

## Endpoint checklist

- [x] `GET /healthz` - Successful native MySQL workflow evidenced.
- [x] `GET /readyz` - Successful native MySQL workflow evidenced.
- [x] `GET /metrics` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/service-info` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/screening-provider/catalog` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/screening-provider/freshness` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/tenants/:tenantId/dataset-update-jobs` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/dataset-update-jobs` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/tenants/:tenantId/dataset-update-jobs/:jobId` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/dataset-update-jobs/:jobId/retry` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/screenings` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/screenings/freeform-search` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/tenants/:tenantId/decisions/:decisionId/screenings` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/tenants/:tenantId/screenings/:screeningId` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/screenings/:screeningId/retry` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/screening-matches/:matchId/review` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/screening-matches/:matchId/comments` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/screening-matches/:matchId/enrich` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/screenings/:screeningId/files` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/screenings/:screeningId/file-uploads` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/tenants/:tenantId/screenings/:screeningId/files` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/tenants/:tenantId/screenings/:screeningId/files/:fileId` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/tenants/:tenantId/screenings/:screeningId/files/:fileId/download` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/tenants/:tenantId/screening-whitelist` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/screening-whitelist` - Successful native MySQL workflow evidenced.
- [x] `DELETE /v1/tenants/:tenantId/screening-whitelist` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/tenants/:tenantId/continuous-screening-configs` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/continuous-screening-configs` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/tenants/:tenantId/continuous-screening-configs/:configId` - Successful native MySQL workflow evidenced.
- [x] `PUT /v1/tenants/:tenantId/continuous-screening-configs/:configId` - Successful native MySQL workflow evidenced.
- [x] `DELETE /v1/tenants/:tenantId/continuous-screening-configs/:configId` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/tenants/:tenantId/continuous-screening-configs/:configId/monitored-objects` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/continuous-screening-configs/:configId/monitored-objects` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/tenants/:tenantId/monitored-objects/:monitoredObjectId` - Successful native MySQL workflow evidenced.
- [x] `DELETE /v1/tenants/:tenantId/monitored-objects/:monitoredObjectId` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/tenants/:tenantId/monitored-objects/:monitoredObjectId/requeue` - Successful native MySQL workflow evidenced.
- [x] `POST /internal/v1/tenants/:tenantId/decision-screenings` - Successful native MySQL workflow evidenced.
- [x] `GET /internal/screening-capabilities` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/manual-screenings` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/manual-screenings` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/manual-screenings/:id` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/investigator/tenants/:tenantId/cases/:caseId/review` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/investigator/tenants/:tenantId/cases/:caseId/review` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/investigator/tenants/:tenantId/cases/:caseId/exclusions` - Successful native MySQL workflow evidenced.
- [x] `GET /openapi.yaml` - Successful native MySQL workflow evidenced.
- [x] `GET /docs` - Successful native MySQL workflow evidenced.
- [x] `GET /redoc` - Successful native MySQL workflow evidenced.
- [x] `GET /v1/investigator/tenants/:tenantId/cases/:caseId/screenings/:screeningId/refinement` - Successful native MySQL workflow evidenced.
- [x] `POST /v1/investigator/tenants/:tenantId/cases/:caseId/screenings/:screeningId/refinement` - Successful native MySQL workflow evidenced.

## Verification and activation

- [x] Source endpoint inventory derived from the shared router and registration functions.
- [x] Complete screening suite, MySQL target/reference fixtures, PostgreSQL characterization, race detection and vet.
- [x] Authenticated positive workflows and unauthenticated/tenant/resource denial checks.
- [x] Live Docker, auth and rendered Swagger acceptance at http://localhost:8085/docs.
- [ ] Real sanctions-provider/data coverage and production workload acceptance (separate release work).

Retained full evidence: [report](evidence/mysql-integration/2026-11-09T23-15-03Z/report.json), 107 top-level tests / 211 test/subtest results.

Screening investigation increment: [acceptance](../../frontend/evidence/mysql-integration/2026-11-11T13-50-46.598Z/report.json), including the two refinement operations, review restrictions and scoped whitelist lifecycle. This supplements the earlier migration evidence; it does not rerun the complete suite.

The default development provider searches a small built-in synthetic catalog only. It does not query OpenSanctions. Human manual/investigator APIs require a current auth access JWT. Owner routes retain their separate private operator and service credentials; integration routes use a dedicated intake credential.

See [migration plan](MYSQL_PLAN.md), [worker checklist](MYSQL_WORKER_STATUS.md) and [Docker instructions](MYSQL_DOCKER.md).
