# Workflows

This document explains both the flat workflow endpoints and the structured workflow rule/condition/action endpoints.

## What A Workflow Is

In this service, a workflow is the post-decision action layer.

Rules answer:

- did a fraud condition match
- if it matched, how much score should it contribute

The decision engine then totals those rule scores and produces a decision outcome such as:

- `approve`
- `review`
- `block_and_review`
- `decline`

A workflow answers the next question:

- now that a decision happened, what should the platform do about it

That means workflows are not used to detect fraud conditions. They are used to define the operational response that should happen after evaluation.

## Where Workflows Fit In The End-To-End Flow

The runtime sequence is:

1. a scenario is selected for evaluation
2. the scenario trigger formula is evaluated
3. the scenario's rules are evaluated
4. matching rules contribute their `score_modifier` values
5. the service computes the final decision outcome from the total score and the iteration thresholds
6. the service creates any matching workflow execution records
7. a dispatcher later sends those workflow executions to the downstream workflow handler

This separation is important:

- rules determine risk
- decisions determine the final scenario outcome
- workflows determine the follow-up action

Without workflows, the engine can tell you that a record is risky, but it cannot express what the wider platform should do next.

## Why Workflows Exist

Workflows exist so that decisioning logic and operational actions do not get mixed together.

That gives a few practical benefits:

- you can change downstream actions without changing the fraud rule logic
- multiple different actions can be attached to the same scenario outcome
- action dispatch can fail or retry independently of the decision itself
- the decision record remains a clean audit of risk evaluation, while the workflow execution record becomes the audit of downstream actioning

Example:

- a rule says `amount > 20000`
- that rule contributes `score_modifier = 20`
- the final scenario outcome becomes `decline`
- a workflow attached to `decline` can then:
  - `create_case`
  - `add_tag`
  - `emit_event`

## What This Service Actually Stores

Workflow authoring definitions are not the same thing as workflow executions.

There are two layers:

- workflow definition or structured workflow rule
  - authored ahead of time on a scenario
  - describes when an action is eligible and what action should be sent
- workflow execution
  - created at decision time
  - represents one concrete action generated for one concrete decision

The execution record contains:

- the `decision_id`
- the `scenario_id`
- the selected `action_type`
- the `action_config`
- a dispatch status such as:
  - `pending_dispatch`
  - `dispatched`
  - `dispatch_failed`

So when a decision is made, the service does not directly perform the action itself. It first writes a workflow execution record, then the dispatch process sends that execution to a downstream workflow endpoint.

## What Workflow Actions Are Supported Right Now

This implementation does not allow arbitrary workflow action types.

The currently valid `action_type` values are:

- `create_case`
- `add_tag`
- `emit_event`

These values are validated by the workflow domain model. Requests using any other `action_type` fail validation.

### Meaning Of Each Action Type

`create_case`

- use when the decision should create an investigation or operations case
- typical fit for `review`, `block_and_review`, or `decline` outcomes

`add_tag`

- use when the result should label the affected entity or record in some way
- typical fit for enrichment or flagging flows where a later system consumes tags

`emit_event`

- use when another downstream service should react to a generic event payload
- typical fit for integrations, notifications, or platform automation outside this service

Important implementation note:

- this service validates and dispatches these action types
- it does not itself define the business meaning of every `action_config` field
- the downstream workflow receiver is responsible for interpreting the action payload

## What `action_config` Is For

Every workflow action stores an `action_config` JSON payload.

That payload is forwarded when the workflow execution is dispatched. It is the place to put downstream action parameters such as:

- a target URL override
- a case type
- a queue name
- a tag name
- an event name
- integration-specific metadata

This service treats `action_config` as opaque JSON for most purposes. It stores it, returns it, and sends it onward with the workflow execution.

## Dispatch Model

Workflow execution creation and workflow execution dispatch are separate steps.

At decision time:

- the decision engine evaluates the scenario
- it decides which workflows match
- it stores workflow execution rows with status `pending_dispatch`

Later, a dispatch process:

- loads pending workflow executions
- POSTs them to the configured workflow dispatcher endpoint
- marks each execution as either:
  - `dispatched`
  - `dispatch_failed`

This means a successful decision does not require the downstream workflow target to be immediately available in the same transaction.

## Flat Workflows Vs Structured Workflow Rules

This service supports two authoring styles.

### Flat workflows

Flat workflows are the simpler model.

They answer:

- if the final decision outcome is in this set of outcomes, trigger this one action

They are a good fit when you want direct outcome-to-action mapping such as:

- on `review`, `create_case`
- on `decline`, `emit_event`

### Structured workflow rules

Structured workflow rules are the richer model.

They let you define:

- a workflow rule with a priority
- one or more conditions
- one or more actions
- whether evaluation should continue to later workflow rules using `fallthrough`

They are a good fit when the workflow should depend on more than just the final decision outcome.

For example:

- if outcome is `decline` and a specific fraud rule hit, `create_case`
- if outcome is `review` and payload amount is greater than `20000`, `add_tag`
- if a first workflow rule matches and `fallthrough` is false, stop evaluating later workflow rules

## Structured Workflow Condition Functions

The currently supported structured condition functions are:

- `always`
- `never`
- `outcome_in`
- `rule_hit`
- `payload_evaluates`

### `always`

- always matches
- takes no meaningful params
- useful for unconditional actions inside a structured workflow rule

### `never`

- never matches
- takes no meaningful params
- mainly useful for placeholders or temporarily disabled branches

### `outcome_in`

- matches when the final decision outcome is in a provided list
- params are a JSON array of outcomes

Example params:

```json
["review", "decline"]
```

### `rule_hit`

- matches when one of the specified scenario rules hit during evaluation
- use when the downstream action should depend on a particular fraud rule, not just the final score band

Example params:

```json
{
  "rule_ids": ["rule-high-amount", "rule-merchant-risk"]
}
```

### `payload_evaluates`

- matches when an AST expression over the evaluated payload returns `true`
- use when workflow eligibility depends on payload facts that should not be baked into the scenario outcome alone

Example params:

```json
{
  "expression": {
    "function": "gt",
    "children": [
      {
        "name": "Payload",
        "children": [
          { "constant": "amount" }
        ]
      },
      { "constant": 20000 }
    ]
  }
}
```

This condition must validate to a boolean AST expression.

## `fallthrough` In Structured Workflow Rules

Structured workflow rules are evaluated by priority order.

When a workflow rule matches:

- its actions are turned into workflow executions
- then the engine checks `fallthrough`

If `fallthrough` is `false`:

- stop evaluating later structured workflow rules

If `fallthrough` is `true`:

- continue evaluating lower-priority structured workflow rules

Use `fallthrough = false` when the first matching workflow rule should win.

Use `fallthrough = true` when multiple workflow rules should be allowed to add multiple downstream actions for the same decision.

## Relationship To Screening And Scoring

Workflows are only one type of post-decision side effect in this service.

The decision engine can also create:

- workflow executions
- screening executions
- scoring requests
- outbox events

These are separate concepts.

Do not treat screening or scoring as workflow `action_type` values in this implementation.

Instead:

- workflows use `create_case`, `add_tag`, or `emit_event`
- screening uses screening configuration routes
- scoring uses scoring configuration routes

## How To Choose Between A Rule And A Workflow

Use a rule when you are deciding whether something is risky.

Use a workflow when you are deciding what to do after that risk has been assessed.

Use a scenario trigger when you are deciding whether the scenario should run at all.

Use the final decision thresholds when you are deciding how total risk score maps to `approve`, `review`, `block_and_review`, or `decline`.

## Example Mental Models

Simple mental model:

- trigger: should I run this scenario
- rule: did this fraud condition match
- decision: what is the final score and outcome
- workflow: what should happen next

Example 1:

- scenario evaluates a transaction
- rule: `amount > 20000`
- score modifier: `20`
- final outcome: `decline`
- flat workflow: on `decline`, `create_case`

Example 2:

- scenario evaluates a transaction
- multiple rules contribute score
- final outcome: `review`
- structured workflow condition: `outcome_in = ["review"]`
- structured workflow condition: `payload_evaluates amount > 20000`
- action: `add_tag`

Example 3:

- final outcome: `decline`
- structured workflow rule 1: `emit_event`, `fallthrough = true`
- structured workflow rule 2: `create_case`, `fallthrough = false`
- result: one decision can generate multiple workflow executions

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
  - valid values in this implementation are:
    - `create_case`
    - `add_tag`
    - `emit_event`
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
  - valid values in this implementation are:
    - `create_case`
    - `add_tag`
    - `emit_event`
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
