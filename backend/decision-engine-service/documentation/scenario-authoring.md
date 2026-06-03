# Scenario Authoring

This document explains the scenario, iteration, publication, and rule authoring endpoints.

## Endpoint Group

- `GET /v1/tenants/:tenantId/scenarios`
- `POST /v1/tenants/:tenantId/scenarios`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId`
- `PUT /v1/tenants/:tenantId/scenarios/:scenarioId`
- `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/copy`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/rules/latest`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/iterations`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/metadata`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/draft`
- `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/commit`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/publications`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/publications`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/publications/preparation`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/publications/preparation`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules`
- `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId`
- `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId`

Primary files:

- [internal/httpapi/handlers/scenarios.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/scenarios.go)
- [internal/httpapi/handlers/publications.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/publications.go)
- [internal/httpapi/handlers/rules.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/rules.go)

## Shared Parameters

- `tenantId`
  - tenant that owns the scenarios and metadata
- `scenarioId`
  - one scenario definition under the tenant
- `iterationId`
  - one versioned draft or committed scenario iteration
- `ruleId`
  - one rule inside an iteration

## Parameter Meanings By Area

### Scenario routes

- `tenantId`
  - selects the tenant boundary for scenario authoring
- `scenarioId`
  - selects the specific scenario being read, updated, copied, or inspected

### Iteration routes

- `iterationId`
  - selects the exact scenario version being read, updated, committed, or cloned into a draft

### Publication routes

- `scenarioId`
  - the scenario whose live version state is being listed or changed

Publication request body meaning:

- action identifies whether the request is publishing or unpublishing
- iteration id identifies which committed iteration should become live

### Rule routes

- `ruleId`
  - the exact rule being updated or deleted

Rule request bodies carry the rule definition itself:

- name and description
- outcome
- condition AST / expression payload
- metadata needed by the evaluator

## Notes

- scenario copy now clones the structured workflow tree as well as scenario authoring state
- publication preparation endpoints use `data-model-service` readiness as the source of truth

## Endpoint Detail

### `GET /v1/tenants/:tenantId/scenarios`

What it does:

- lists all scenarios owned by the tenant

How it should be used:

- populate scenario inventory screens
- fetch ids before drilling into iterations, rules, or publications

### `POST /v1/tenants/:tenantId/scenarios`

What it does:

- creates a new scenario shell

Request body fields:

- `name`
  - human-readable scenario name
- `trigger_object_type`
  - object type this scenario is allowed to evaluate
  - in practice, this should match the tenant data-model table/object type name, such as `business`, `transaction`, or `user`
  - this is the same object type value that will appear in ingestion-triggered evaluation requests
  - field validation and runtime evaluation both use this object type as the scenario's base record type

Example:

- if the tenant data model contains a table or object type named `business`, then `trigger_object_type: "business"` means this scenario is authored against business records
- if the tenant data model contains a table or object type named `transaction`, then `trigger_object_type: "transaction"` means this scenario only evaluates transaction records

How it works:

- creates the scenario record
- does not automatically create a live iteration
- validates that the object type already exists in the tenant data model

How it should be used:

- first step when authoring a new decision flow
- choose the exact base record type the scenario should run against

### `GET /v1/tenants/:tenantId/scenarios/:scenarioId`

What it does:

- loads one scenario record

How it should be used:

- fetch scenario metadata before editing or evaluating it

### `PUT /v1/tenants/:tenantId/scenarios/:scenarioId`

What it does:

- updates scenario metadata

How it should be used:

- rename a scenario
- change the trigger object type when the scenario boundary itself needs to move

Important validation note:

- create and update both validate `trigger_object_type` against the current tenant model
- if the object type does not exist in `data-model-service`, the request fails instead of storing an invalid scenario shell

### `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId`

What it does:

- deletes one scenario

How it works:

- removes the scenario record
- dependent rows linked by foreign keys are deleted by cascade, including scenario iterations, rules, publications, decisions, test runs, workflow rows, screening configs, and scoring configs

How it should be used:

- remove a scenario that should no longer exist at all
- not for temporary disablement; use publication/live-version controls when you want to stop execution without removing authoring history

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/copy`

What it does:

- clones the scenario into a new scenario

Request body fields:

- `name`
  - name for the copied scenario

How it works:

- copies scenario metadata
- copies iterations/rules needed for authoring continuity
- copies structured workflows as part of the extraction work

How it should be used:

- fork an existing scenario into a variant
- start a new scenario from an established baseline

### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/rules/latest`

What it does:

- returns the rules from the latest relevant iteration view for the scenario

How it should be used:

- quick current-rules view without separately loading iteration history first

### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/iterations`

What it does:

- lists all scenario iterations

How it should be used:

- show draft/live history
- choose an iteration to inspect, commit, or publish

### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/metadata`

What it does:

- lists a lighter metadata-only view of iterations

How it should be used:

- timeline or selector UIs that do not need full trigger formula payloads

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations`

What it does:

- creates a new draft iteration for the scenario

How it works:

- increments versioning
- starts a new authoring draft under the scenario
- seeds the new draft with valid default values so it can be edited immediately:
  - `trigger_formula = {"constant": true}`
  - `score_review_threshold = 1`
  - `score_block_and_review_threshold = 10`
  - `score_decline_threshold = 20`

What those defaults mean:

- `trigger_formula = {"constant": true}`
  - a literal boolean `true` AST node
  - means "always run this scenario" until the author replaces it with a real trigger condition
- `score_review_threshold = 1`
  - the scenario enters the review band when total scenario score is `>= 1`
- `score_block_and_review_threshold = 10`
  - the scenario enters the stronger block-and-review band when total score is `>= 10`
- `score_decline_threshold = 20`
  - the scenario enters the decline band when total score is `>= 20`

Important note:

- these are bootstrap authoring defaults, not system-derived risk values
- authors are expected to replace them with scenario-specific logic and thresholds

How it should be used:

- begin a new revision of a scenario without mutating the existing live version

### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId`

What it does:

- fetches one full iteration including trigger and threshold fields

How it should be used:

- edit forms
- validation or commit preparation

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/draft`

What it does:

- creates a new draft from an existing iteration

How it works:

- copies the selected iteration into a fresh draft version
- intended for “branch from existing” authoring

How it should be used:

- when you want to tune an older or live iteration without editing it directly

### `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId`

What it does:

- updates one draft iteration

Request body fields:

- `trigger_formula`
  - the trigger AST/formula that decides whether evaluation should proceed
  - this is evaluated before the scenario's rules are scored
  - if it resolves to `false`, the scenario does not run for that record
  - `{"constant": true}` means "always run"
- `score_review_threshold`
  - score at or above which a review outcome may begin
  - this is the lowest review threshold
- `score_block_and_review_threshold`
  - score threshold for block-and-review behavior
  - this should be greater than or equal to `score_review_threshold`
- `score_decline_threshold`
  - score threshold for decline behavior
  - this should be greater than or equal to `score_block_and_review_threshold`
- `schedule`
  - optional schedule string for scheduled execution use cases

Threshold ordering rule:

- the service validates `score_review_threshold <= score_block_and_review_threshold <= score_decline_threshold`
- requests fail if the thresholds are out of order

How scenario score works:

- each rule has its own `score_modifier`
- when a rule hits, its `score_modifier` contributes to the scenario's total score
- the final total score is compared against the three thresholds above to determine which outcome band the scenario falls into

How it should be used:

- update trigger logic or thresholds on a draft iteration before validation/commit

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/commit`

What it does:

- marks a draft iteration as committed and ready for publication workflows

How it should be used:

- after validation passes and the scenario author wants to lock the version for publication

### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/publications`

What it does:

- lists publication history for the scenario

How it should be used:

- inspect which iterations were published or unpublished and when

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/publications`

What it does:

- records a publish or unpublish action

Request body fields:

- `action`
  - publication action, typically publish or unpublish
- `iteration_id`
  - committed iteration being made live or targeted by the action

How it works:

- for publish, the service updates the scenario live iteration pointer
- publication safety depends on preparation readiness

How it should be used:

- activate a committed iteration
- remove a live iteration from active use

### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/publications/preparation`

What it does:

- reports whether publication preparation is needed, started, or finished

How it works:

- inspects model/index readiness through `data-model-service`

How it should be used:

- check whether a scenario is safe to publish

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/publications/preparation`

What it does:

- starts or retriggers preparation work needed before publish

How it should be used:

- initiate readiness/index preparation when a scenario references model structures that must be prepared first

### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules`

What it does:

- lists rules for one iteration

How it should be used:

- load the full ordered rule list for a draft or committed iteration

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules`

What it does:

- creates a rule on one iteration

Request body fields:

- `display_order`
  - ordering position within the iteration
- `name`
  - rule name
- `description`
  - rule description
- `formula`
  - AST or formula payload for the rule logic
- `score_modifier`
  - score contribution applied when the rule hits
  - positive values increase risk score
  - the accumulated score across matching rules is what gets compared to the iteration thresholds
- `rule_group`
  - grouping/category identifier
- `snooze_group_id`
  - optional snooze bucket used by rule snoozes
- `stable_rule_id`
  - stable identity used across iteration copies/versioning

How it should be used:

- add a new rule to the currently authored iteration

### `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId`

What it does:

- updates one rule in the iteration

How it should be used:

- edit formula, ordering, scoring, or grouping metadata

### `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId`

What it does:

- removes one rule from the iteration

How it should be used:

- cleanly delete obsolete rule logic from a draft iteration
