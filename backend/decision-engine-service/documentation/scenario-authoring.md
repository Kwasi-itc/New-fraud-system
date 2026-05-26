# Scenario Authoring

This document explains the scenario, iteration, publication, and rule authoring endpoints.

## Endpoint Group

- `GET /v1/tenants/:tenantId/scenarios`
- `POST /v1/tenants/:tenantId/scenarios`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId`
- `PUT /v1/tenants/:tenantId/scenarios/:scenarioId`
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

How it works:

- creates the scenario record
- does not automatically create a live iteration

How it should be used:

- first step when authoring a new decision flow

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
- `score_review_threshold`
  - score at or above which a review outcome may begin
- `score_block_and_review_threshold`
  - score threshold for block-and-review behavior
- `score_decline_threshold`
  - score threshold for decline behavior
- `schedule`
  - optional schedule string for scheduled execution use cases

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
