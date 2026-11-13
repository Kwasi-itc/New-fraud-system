# Screening And Scoring

This document explains screening config CRUD, scoring config CRUD, execution/request inspection, and provider-result lifecycle updates.

## Screening rule findings and MySQL behavior (2026-11-11)

Reference: [setting up a screening rule](https://docs.checkmarble.com/docs/setting-up-a-sanction-check-rule)
and [reviewing a screening](https://docs.checkmarble.com/docs/reviewing-a-sanction-check).
The guide describes screening as part of decision evaluation, with a hit-driven
outcome, combined names, preprocessing, dataset selection and counterparty
whitelisting. The following checklist separates implemented behavior from parity
work. It does not establish real sanctions coverage.

- [x] Scenario Rules > Add offers a rule or screening configuration.
- [x] Ordered name mapping: `name_field` plus up to four
  `additional_name_fields`. Trim and join nonblank parts with one space before
  preprocessing. Combined names must contain 1–200 characters; nontext values,
  duplicate mappings and unavailable model fields are rejected.
- [x] Optional `forced_outcome`: `review`, `block_and_review`, or `decline`.
  An omitted value preserves asynchronous post-decision behavior. Inline rules
  run after the scenario trigger and their additional condition, independently
  of `outcomes`; that filter applies only to asynchronous configurations.
- [x] Inline hits raise the outcome before workflow and scoring routing.
  The numeric rule score stays unchanged. Severity is monotonic:
  `approve < review < block_and_review < decline`. Multiple hits use the most
  severe outcome; a screening cannot soften an existing decline.
- [x] One two-second context budget covers all inline rules for a decision.
  Provider failure, timeout or an incomplete response without any known match
  fails evaluation; no successful approval or decision write is produced.
  Provider work occurs before the write transaction. The local adapter makes
  no network requests; a future external adapter must honor this deadline.
- [x] Persist the exact owner-produced result snapshot and forced-outcome policy
  with the decision effect in its existing atomic transaction. Private intake
  validates the query digest and bounded result shape; screening workers replay
  this evidence instead of searching again. Normal receipt replay, claims,
  projection and case delivery remain in use.
- [x] Use the same screening-owned synthetic matcher for inline and background
  work. Authoring exposes its actual defaults: 80% and 50 results. Per-rule
  overrides support 0–100 and 1–50 respectively; zero is an explicit threshold.
- [x] Show the actual provider and its synthetic-data limitation in the editor.
  Gate unsupported identity, dataset and recognition settings by capability.
  Name-recognition copy explains its potential impact on decision latency.
- [x] Existing bounded additional conditions and name preprocessing contracts
  remain available. Custom-list removal and recognition are supported in the
  portable external-provider path, but are unavailable in native local MySQL.
- [ ] Organization-wide screening policy, inherited defaults and its settings
  editor. The reference's 70%/30 defaults are not the current synthetic defaults.
- [x] Counterparty text-field selector and audited suppression of the exact
  `(tenant, provider, counterparty ID, matched entity)` pair. Investigators can
  dismiss and whitelist, or remove that exclusion for future searches. Blank
  identifiers never enable suppression. Candidates and application receipts are
  retained. Inline evaluation captures the policy used at evaluation time;
  background replay does not reinterpret that saved result after policy changes.
- [x] Group the editor into global settings, counterparty ID, matching settings,
  preprocessing and review routing. Saving is explicitly live configuration.
- [x] Decision screening cards show rule names, timing and provider, with a
  currently authorized case link. Case evidence presents expandable candidates,
  similarity, datasets, identity properties and original search inputs.
- [x] Approval is rejected by the backend while screening is pending, failed,
  incomplete, archived or has unresolved/confirmed matches. The UI explains the
  restriction and leaves decline available. Match review also rejects partial
  and superseded sources. The machine decision score/outcome remains immutable.
- [x] Linked refinement requires a fresh, complete, actor-owned manual preview,
  expected source version and findings. Preview and apply are separate actions.
  Applying atomically creates an immutable receipt, case links, history and
  outbox event. It preserves the original source and does not approve a decision.
  Competing refinements cannot fork a source; an identical retry replays its receipt.
  Confirmed matches cannot be cleared by refinement. Applied descendants inherit
  the original counterparty whitelist scope without changing their saved query.
- [ ] Vehicle and Any-type matching with a capability-advertised provider contract.
  The current editor and local matcher support Person and Organization.
- [ ] Provider-backed dataset catalog and list-group selection. Existing dataset
  ID settings require a suitable external adapter; the local catalog rejects them.
- [ ] Native MySQL real-provider wiring, credentials, coverage, latency/load
  acceptance, organization hit limits and production close-case workflows.
  The new inline capability is explicitly a local development implementation.

Configuration JSON remains in the existing MySQL owner tables. The investigation
increment appends screening-owned refinement, counterparty exclusion, event and
application tables; it does not add a PostgreSQL migration. Install the screening
schema first, refresh the decision review read grants, and deploy screening API,
worker and the local private intake/provider before the decision evaluator and
frontend. The intake accepts the authored
`opensanctions` alias for the synthetic catalog. The legacy `local-development`
exact-watchlist traffic remains on its existing worker.
Old readers must not edit configurations containing fields they do not understand.
Local acceptance is recorded in `frontend/evidence/mysql-integration/`; focused
tests are `inline_screening_test.go`, `screening_combined_names_test.go`, and
`scenario-screening-inline.spec.ts`.

Verified local acceptance: Go race regressions passed for the affected decision
and screening packages; host and Docker production builds passed; 13 focused
browser tests passed. The real authenticated MySQL journey passed 12 suites and
69 recorded gateway requests, including unknown-field denial, combined-name
authoring, forced block with numeric score 0, no-hit approval and completed
captured-evidence projection. Raw execution inspection remains on the existing
loopback development API; the browser gateway still denies it.
Evidence: [live report](../../../frontend/evidence/mysql-integration/2026-11-11T11-53-58.331Z/report.json).
Two earlier failed acceptance reports are retained; the linked report records
the passing run after correcting the private intake/provider deployment.

The investigation increment is verified separately by
`mysql_review_refinement_test.go` against restricted native API/worker accounts
and the disposable MySQL target/reference pair. It covers private intake and
receipt replay, approval denial, partial-result rejection, tenant isolation,
worker and inline suppression, audited removal, stale versions, rejected reused
previews, history rollback, competing applies and final human review. Refinement
previews expire after 15 minutes; each original source permits at most 20 applied
refinements. Review reads are bounded to 100 sources/refinements per decision.
Oversized histories fail explicitly instead of silently clearing approval.

The focused screening browser suite passes 24 tests. The broader legacy case
workspace suite still has stale tab/selector expectations and is not claimed as a
clean regression run. Current live acceptance is linked from
`frontend/MYSQL_INTEGRATION.md` and `screening-service/MYSQL_ENDPOINT_STATUS.md`.

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
