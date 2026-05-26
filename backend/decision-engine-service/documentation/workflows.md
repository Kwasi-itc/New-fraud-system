# Workflows

This document explains both the flat workflow endpoints and the structured workflow rule/condition/action endpoints.

## Endpoint Group

- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/workflows`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/workflows`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/workflows/reorder`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId`
- `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId`
- `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/reorder`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId`
- `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId`
- `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions`
- `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions/:conditionId`
- `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions/:conditionId`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions`
- `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions/:actionId`
- `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions/:actionId`
- `GET /v1/tenants/:tenantId/decisions/:decisionId/workflow-executions`

Primary files:

- [internal/httpapi/handlers/workflows.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/workflows.go)
- [internal/httpapi/handlers/workflow_rules.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/workflow_rules.go)

## Shared Parameters

- `tenantId`
  - tenant that owns the scenario, workflows, and workflow executions
- `scenarioId`
  - scenario where the workflows are authored
- `workflowId`
  - one flat workflow definition
- `ruleId`
  - one structured workflow rule
- `conditionId`
  - one condition attached to a structured workflow rule
- `actionId`
  - one action attached to a structured workflow rule
- `decisionId`
  - one decision whose workflow executions should be listed

## Reorder Requests

Both reorder endpoints accept an ordered array of ids.

Meaning:

- the caller sends the desired final order
- the service stores that order as display/order priority

## Execution Read

`GET /decisions/:decisionId/workflow-executions` returns the workflow execution records created after a decision was evaluated.

## Endpoint Detail

### Flat workflow endpoints

#### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/workflows`

What it does:

- lists flat workflow definitions for a scenario

How it should be used:

- legacy/simple workflow authoring screens
- inspect display order and action config

#### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/workflows`

What it does:

- creates one flat workflow definition

Request body fields:

- `name`
- `description`
- `allowed_outcomes`
  - outcomes that should trigger the workflow
- `action_type`
  - action to dispatch when matched
- `action_config`
  - action payload/configuration
- `active`
  - whether it is enabled

How it should be used:

- simple one-step post-decision automation

#### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/workflows/reorder`

What it does:

- changes flat workflow display order

How it should be used:

- keep scenario workflows in a deterministic authoring order

#### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId`

What it does:

- loads one flat workflow definition

#### `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId`

What it does:

- updates one flat workflow definition

#### `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId`

What it does:

- deletes one flat workflow definition

### Structured workflow rule endpoints

#### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules`

What it does:

- lists structured workflow rules for the scenario

How it should be used:

- richer rule/condition/action authoring flows

#### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules`

What it does:

- creates one structured workflow rule

Request body fields:

- `name`
  - rule name
- `fallthrough`
  - whether later workflow rules should continue after this one matches

#### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/reorder`

What it does:

- sets the evaluation priority order for structured workflow rules

#### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId`

What it does:

- loads one structured workflow rule including its conditions and actions

#### `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId`

What it does:

- updates the rule metadata such as name or fallthrough

#### `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId`

What it does:

- deletes one structured workflow rule and its child authoring elements

### Condition endpoints

#### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions`

What it does:

- adds a condition to a structured workflow rule

Request body fields:

- `function`
  - condition function name such as `always`, `outcome_in`, `rule_hit`, or `payload_evaluates`
- `params`
  - function-specific JSON parameters

How it should be used:

- express the logic that decides whether a workflow rule is eligible to fire

#### `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions/:conditionId`

What it does:

- updates one condition

#### `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions/:conditionId`

What it does:

- removes one condition

### Action endpoints

#### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions`

What it does:

- adds an action to a structured workflow rule

Request body fields:

- `action_type`
  - downstream action to perform
- `action_config`
  - JSON payload for that action

#### `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions/:actionId`

What it does:

- updates one workflow action

#### `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions/:actionId`

What it does:

- removes one workflow action

### Execution read endpoint

#### `GET /v1/tenants/:tenantId/decisions/:decisionId/workflow-executions`

What it does:

- lists workflow executions created for one decision

How it should be used:

- inspect which workflow action fired
- see whether dispatch completed or failed
