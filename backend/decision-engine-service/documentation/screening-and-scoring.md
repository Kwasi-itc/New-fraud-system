# Screening And Scoring

This document explains screening config CRUD, scoring config CRUD, execution/request inspection, and provider-result lifecycle updates.

## Screening configuration access

Legacy `/v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs`
routes (including individual configuration routes) are operator-only. All five
operations require `X-Screening-Authoring-Token`, matching the private
`SCREENING_AUTHORING_TOKEN` environment variable, in addition to normal service
authentication. An absent credential disables these routes with403, even when
`SERVICE_AUTH_MODE=disabled`. A configured credential must have at least32 bytes
after trimming and must differ from `SERVICE_AUTH_TOKEN`.

Never put this credential in browser code, `NEXT_PUBLIC_*`, URLs or request logs.
Compose forwards it only to the decision engine. Existing operator clients must
be updated before rollout; no running environment was changed by this increment.
This is a privileged operator boundary, not tenant-scoped analyst authorization.
The user-facing configuration editor uses the separate private gateway described
below. Do not wire the browser API client to this operator route.

Execution inspection, callbacks and automatic worker consumption of saved
configurations are unchanged. Broader legacy execution-route authorization is a
separate open service-boundary task.

## User-authorized scenario editor

`/detection/{scenarioId}/screening` lists, creates, edits and disables name-based
Person/Organization configurations for OpenSanctions. The browser calls
`/api/scenario-screening/scenarios/{scenarioId}` (GET) or
`/api/scenario-screening/scenarios/{scenarioId}/configs/{configId}` (PUT).
The server proxy resolves the tenant from the platform session, checks mutation
origin and forwards to `/internal/screening-authoring/tenants/{tenantId}/scenarios/{scenarioId}`
and its `/configs/{configId}` PUT child.

Private requests use `Authorization: Bearer <SCREENING_AUTHORING_TOKEN>` and
`X-Screening-User`, `X-Screening-Session`, `X-Screening-Version` from the current
server session. The backend independently calls the existing identity-owner
platform and case authorization contracts. Writes require platform-admin and
case access, accessible tenant-owned inboxes and an existing scenario text field.
The gateway secret alone does not grant analyst permissions on this route.

PUT fields: `id`, `name`, `enabled`, `entity_type` (Person/Organization),
`name_field`, `outcomes` (approve/review/block_and_review/decline),
`inbox_id`, `updated_at` (null for create, exact saved version for edit).
Unknown fields and bodies over8192 bytes are rejected. Responses contain the
saved structured rule. GET returns `scenario_name`, `rules`, `fields` and
`other_rules`; the proxy adds session-derived tenant and edit visibility.
403 means current identity/inbox access denied;404 missing scenario;409 stale,
unsupported or invalidated mapping;500 returns a redacted retry message.

Only canonical name-mapping configs owned by this editor are editable. Other
provider settings are retained without rewriting. Limits are100 configs per
scenario and500 eligible fields; exceeding them fails explicitly. Configuration
and audit writes share a transaction; expected versions prevent stale overwrites.
An uncertain create response must be recovered by reloading, not creating another
ID. There is no destructive delete action.

Apply additive decision migration000023 before enabling the editor. It records
actor/config snapshots; its down migration refuses to remove populated history.
Both identity schema versions1/2 and case inbox schemas must already be present.
No external request occurs inside the save transaction.

This is **post-decision screening**, not a scoring rule: selected existing
decision outcomes trigger asynchronous screening. Saving does not establish
provider/worker availability. Advanced fields, provider health verification and
screening-driven changes to decision scores remain separate work.

## Endpoint Group

- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId`
- `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId`
- `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId`
- `GET /v1/tenants/:tenantId/decisions/:decisionId/screening-executions`
- `GET /v1/tenants/:tenantId/screening-executions/:executionId`
- `POST /v1/tenants/:tenantId/screening-executions/:executionId/status`
- `POST /v1/tenants/:tenantId/screening-executions/:executionId/retry`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId`
- `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId`
- `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId`
- `GET /v1/tenants/:tenantId/decisions/:decisionId/scoring-requests`
- `GET /v1/tenants/:tenantId/scoring-requests/:requestId`
- `POST /v1/tenants/:tenantId/scoring-requests/:requestId/status`
- `POST /v1/tenants/:tenantId/scoring-requests/:requestId/retry`

Primary files:

- [internal/httpapi/handlers/screening.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/screening.go)
- [internal/httpapi/handlers/scoring.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/scoring.go)

## Shared Parameters

- `tenantId`
  - tenant boundary for configs and provider execution state
- `scenarioId`
  - scenario that owns the config
- `configId`
  - screening or scoring config identifier
- `decisionId`
  - decision whose screening/scoring side effects are being inspected
- `executionId`
  - screening execution identifier
- `requestId`
  - scoring request identifier

## Request Meanings

### Config create/update

Screening config request fields:

- `name`
  - display/authoring name
- `allowed_outcomes`
  - decision outcomes that should trigger screening
- `provider`
  - upstream screening provider name or identifier
- `config_json`
  - provider-specific configuration payload
- `active`
  - whether the config participates in live execution

Scoring config request fields:

- `name`
- `allowed_outcomes`
- `ruleset_ref`
  - scoring ruleset or provider-side scoring reference
- `config_json`
- `active`

### Status update endpoints

Screening/scoring status update request fields:

- `status`
  - lifecycle state such as `pending`, `sent`, `completed`, or `failed`
- `provider_reference`
  - upstream provider job/request identifier
- `response_json`
  - provider result payload persisted by the service
- `last_error`
  - last provider-side or orchestration error text

### Retry endpoints

Retry means:

- clear the current provider-result failure state
- move the execution/request back to pending so the dispatch path can send it again

## Endpoint Detail

### Screening config endpoints

#### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs`

What it does:

- lists screening configurations attached to a scenario

#### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs`

What it does:

- creates a screening config for the scenario

How it should be used:

- declare when a decision should result in a screening request

#### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId`

What it does:

- loads one screening config

#### `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId`

What it does:

- updates one screening config

#### `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId`

What it does:

- deletes one screening config

### Screening execution endpoints

All four legacy execution routes below require `X-Screening-Authoring-Token`
matching the private `SCREENING_AUTHORING_TOKEN`, in addition to service bearer
 authentication when enabled. Missing/short configuration fails closed, including
local disabled-auth mode. Responses use `Cache-Control: private, no-store`.
These are privileged cross-tenant operator contracts, not analyst permissions.
The browser uses `/api/decision-screening/{decisionId}` and its session-derived
private backend summary route instead. Never place this credential in NEXT_PUBLIC
configuration. Existing operator scripts must explicitly supply it.

The screening worker callback `/internal/screening-status-updates` is unchanged;
it reconciles source-owned state. Do not replace it with the generic status route.
Analyst retry is not enabled by read access.


#### `GET /v1/tenants/:tenantId/decisions/:decisionId/screening-executions`

What it does:

- lists screening executions created as side effects of one decision

#### `GET /v1/tenants/:tenantId/screening-executions/:executionId`

What it does:

- loads one screening execution record including provider-state fields

#### `POST /v1/tenants/:tenantId/screening-executions/:executionId/status`

What it does:

- updates screening execution lifecycle and provider result data

How it should be used:

- provider callback handlers
- internal operational correction tools

#### `POST /v1/tenants/:tenantId/screening-executions/:executionId/retry`

What it does:

- resets and requeues a failed or previously sent screening execution

### Scoring config endpoints

#### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs`

What it does:

- lists scoring configs attached to a scenario

#### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs`

What it does:

- creates one scoring config

How it should be used:

- declare when a decision should generate a scoring request

#### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId`

What it does:

- loads one scoring config

#### `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId`

What it does:

- updates one scoring config

#### `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId`

What it does:

- deletes one scoring config

### Scoring request endpoints

#### `GET /v1/tenants/:tenantId/decisions/:decisionId/scoring-requests`

What it does:

- lists scoring requests created for one decision

#### `GET /v1/tenants/:tenantId/scoring-requests/:requestId`

What it does:

- loads one scoring request including provider-state fields

#### `POST /v1/tenants/:tenantId/scoring-requests/:requestId/status`

What it does:

- updates scoring request lifecycle and provider result data

#### `POST /v1/tenants/:tenantId/scoring-requests/:requestId/retry`

What it does:

- resets and requeues a failed or previously sent scoring request

## Scenario authoring extensions (2026-10-21)

The private session-derived authoring API accepts optional threshold (integer0-100),
result_limit (integer1-50), datasets (up to20 unique IDs), a bounded workflow condition
tree, minimum_name_length (0-200 Unicode characters), remove_digits, ignore_list_id,
use_ner and ner_ignore_classification. Existing optional identity field mappings remain.
Settings are canonicalized into existing config_json; unknown legacy settings remain
read-only. No migration is needed. False conditions/preprocessing skips create no
screening request. These options do not change the original decision score/outcome.

Ignore lists are tenant-owned existing custom lists, bounded to1000 entries/1000 bytes
per entry. Missing/deleted/oversized lists fail before decision persistence. List values
are read once per list per decision; subsequent list changes affect future executions.

Optional recognition requires SCREENING_NAME_RECOGNITION_URL and, when required by
that service, SCREENING_NAME_RECOGNITION_TOKEN on both API and decision worker. The
endpoint must use HTTPS except loopback HTTP tests. It receives POST {text} and returns
an array of {type,text}; timeout <=5s, body <=32KiB and <=20 matches. Secrets/endpoints
are never sent to the UI. Recognition failure returns safe502 before writes; no retry
is hidden inside the adapter. Capabilities indicate configuration, not health.
Recognized names supplement the original; unknown classifications are ignored unless
ner_ignore_classification selects Thing. Direct OpenSanctions is required; an explicit
persisted adapter requirement rejects generic worker drift before provider HTTP.

See docs/marble-screening-study/architecture/scenario-screening-completion.md at the
workspace root for acceptance evidence, target adaptations and remaining release gates.
