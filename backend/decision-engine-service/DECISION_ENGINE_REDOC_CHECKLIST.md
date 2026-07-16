# Decision Engine Redoc Checklist

This checklist tracks the remaining work needed before the decision-engine OpenAPI and Redoc can be treated as complete for developer use.

## 1. Redoc and top-level docs

- [x] Add `GET /redoc`
- [x] Keep `GET /docs` and `GET /openapi.yaml`
- [x] Replace the long technical API intro with a short plain-English summary
- [x] Move `Health` lower in the tag list
- [x] Rewrite health descriptions in simpler language

## 2. Tag and route description quality

- [x] Improve scenario authoring tag description
- [x] Improve decision, workflow, screening, scoring, execution, platform-helper, and outbox route descriptions
- [x] Keep descriptions focused on what the endpoint does, when to use it, and any important caller-facing behavior
- [x] Do one final consistency pass to remove any remaining overly technical or repetitive wording

## 3. Field descriptions and examples

- [x] Add clearer field descriptions for the main scenario, rule, decision, workflow, screening, scoring, execution, helper, and outbox schemas
- [x] Clarify that `trigger_object_type` and similar `object_type` fields refer to the tenant data-model table name
- [x] Add realistic examples for core request and response schemas
- [x] Add examples for any newly documented routes or response envelopes that still look thin in Redoc

## 4. Query parameter coverage

- [x] Document decision filters on `GET /v1/tenants/{tenantId}/decisions`
- [x] Document rule-snooze filters
- [x] Document custom-list-entry filters
- [x] Document record-tag filters
- [x] Document IP-flag filters
- [x] Document outbox `limit`
- [x] Document scheduled-execution filters
- [x] Document async-decision-execution filters
- [x] Document publication-preparation `iteration_id`

## 5. Public route coverage still missing from OpenAPI

### Scenario authoring

- [x] `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/copy`
- [x] `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/rules/latest`
- [x] `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/ast-ai-description`
- [x] `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/generate-ast`
- [x] `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/metadata`
- [x] `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}`
- [x] `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/draft`
- [x] `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules/{ruleId}/ai-description`

### Test runs

- [x] `GET /v1/tenants/{tenantId}/test-runs/{testRunId}/decision-data-by-score`
- [x] `GET /v1/tenants/{tenantId}/test-runs/{testRunId}/data-by-rule-execution`

### Workflows

- [x] `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflows/{workflowId}`
- [x] `PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflows/{workflowId}`
- [x] `DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflows/{workflowId}`

### Screening configs

- [x] `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/screening-configs/{configId}`
- [x] `PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/screening-configs/{configId}`
- [x] `DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/screening-configs/{configId}`

### Scoring configs

- [x] `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/scoring-configs/{configId}`
- [x] `PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/scoring-configs/{configId}`
- [x] `DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/scoring-configs/{configId}`

### Decisions and execution

- [x] `POST /v1/tenants/{tenantId}/decisions`
- [x] `POST /v1/tenants/{tenantId}/decisions/all`
- [x] `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/scheduled-executions/{executionId}/retry`
- [x] `GET /v1/tenants/{tenantId}/async-decision-executions/{executionId}`
- [x] `POST /v1/tenants/{tenantId}/async-decision-executions/{executionId}/retry`

## 6. Recently added route coverage to polish

- [x] Add or improve examples for publication-preparation responses
- [x] Add or improve examples for execution status summary responses
- [x] Check that the new filtered decision-list route reads clearly in Redoc
- [x] Check that scheduled and async execution filter descriptions are consistent

## 7. Internal route decision

- [x] Decide whether `POST /internal/screening-status-updates` should appear in OpenAPI/Redoc
- [x] If yes, add a clearly marked internal section for it
- [x] If no, leave it intentionally undocumented and note that decision in service docs

## 8. Final verification

- [x] Compare `router.go` against `openapi.yaml` one more time after the missing routes are added
- [x] Verify `/openapi.yaml`, `/docs`, and `/redoc` locally
- [x] Check that non-technical readers can understand the top-level summaries
- [x] Check that examples shown in Redoc are realistic and not misleading
